// Package afhttp is the runtime for routes declared with .route in .af
// files: strict request decoding, argument parsing, and JSON responses and
// errors. Errors are go-kit errs errors (github.com/omniaura/go-kit/errs):
// the client sees a factory's fixed message, code and fields, and the cause
// stays in the logs.
package afhttp

import (
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/omniaura/go-kit/errs"
)

// DefaultMaxBytes caps a request body when its .route section sets no
// .maxbytes.
const DefaultMaxBytes = 1 << 20

// The errors generated handlers fail with. Return them, or your own errs
// factories, from Auth and Prepare: WriteError writes any *errs.Error as it
// is.
var (
	ErrInvalidRequest = errs.NewFactory(http.StatusBadRequest, "invalid request", errs.WithCode("invalid_request"), errs.WithAction(errs.ActionFixInput))
	ErrTooLarge       = errs.NewFactory(http.StatusRequestEntityTooLarge, "request body is too large", errs.WithAction(errs.ActionFixInput))
	ErrUnauthorized   = errs.NewFactory(http.StatusUnauthorized, "unauthorized", errs.WithAction(errs.ActionSignIn))
	ErrNotFound       = errs.NewFactory(http.StatusNotFound, "not found")
	ErrConflict       = errs.NewFactory(http.StatusConflict, "already exists", errs.WithCode("already_exists"))
	ErrModelFailed    = errs.NewFactory(http.StatusBadGateway, "the model could not answer", errs.WithCode("model_failed"), errs.WithAction(errs.ActionRetry))
)

// Unauthorized wraps an authentication failure in ErrUnauthorized. An
// *errs.Error from the authenticator keeps its own status.
func Unauthorized(ctx context.Context, err error) *errs.Error {
	var e *errs.Error
	if errors.As(err, &e) {
		return errs.AsError(ctx, err)
	}
	return ErrUnauthorized.New(ctx).AddError(err)
}

// QueryFailed wraps a failed query: ErrConflict for a unique violation,
// and ErrInvalidRequest for a NOT NULL or CHECK violation, so constraints
// in the schema validate input. The field is the column, when the driver
// names it (pgx's
// PgError.ColumnName, lib/pq's Error.Column) or a CHECK constraint has
// Postgres's default name, <table>_<column>_check (or _check1, ...). An *errs.Error the
// queries return, such as a permission check in a wrapper around them,
// keeps its status; anything else is errs.Unknown, whose message says
// nothing of the cause.
func QueryFailed(ctx context.Context, err error) *errs.Error {
	var e *errs.Error
	if errors.As(err, &e) {
		return errs.AsError(ctx, err)
	}
	var state interface{ SQLState() string }
	if !errors.As(err, &state) {
		return errs.AsError(ctx, err)
	}
	code := state.SQLState()
	if code == "23505" {
		return ErrConflict.New(ctx).AddError(err)
	}
	problem, ok := invalidValues[code]
	if !ok {
		return errs.AsError(ctx, err)
	}
	out := ErrInvalidRequest.New(ctx).AddError(err)
	if col := rejectedColumn(state, code); col != "" {
		out.Field(col, problem)
	}
	return out
}

// invalidValues are the Postgres SQLSTATEs of a value a write's
// constraint rejects, with what a client is told is wrong with it. Data
// exceptions (class 22, such as a varchar(n) overflow) are left out: a read
// of stored data raises them too, and Postgres names no column for them.
// Bound a length with a CHECK instead.
var invalidValues = map[string]string{
	"23502": "is required", // not_null_violation
	"23514": "is invalid",  // check_violation
}

// rejectedColumn names the column a database error is about, from the
// driver error's fields, which are read by name so that afhttp needs no
// driver.
func rejectedColumn(err any, code string) string {
	if col := stringField(err, "ColumnName", "Column"); col != "" {
		return col
	}
	if code != "23514" {
		return ""
	}
	table := stringField(err, "TableName", "Table")
	constraint := stringField(err, "ConstraintName", "Constraint")
	// Postgres numbers a column's further checks: <table>_<column>_check1.
	constraint = strings.TrimRight(constraint, "0123456789")
	col, ok := strings.CutPrefix(constraint, table+"_")
	if col, found := strings.CutSuffix(col, "_check"); ok && table != "" && found && col != "" {
		return col
	}
	return ""
}

// stringField returns the first of the named string fields of the struct v
// points to that is set.
func stringField(v any, names ...string) string {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return ""
	}
	for _, n := range names {
		if f := rv.FieldByName(n); f.IsValid() && f.Kind() == reflect.String && f.String() != "" {
			return f.String()
		}
	}
	return ""
}

// ToolMessage is what a model is told of a failed tool call: the error's
// message and fields when it is the caller's fault (a 4xx), such as a
// value the database rejects, or "internal error".
func ToolMessage(e *errs.Error) string {
	p := e.Public()
	if p.Status >= 500 {
		return "internal error"
	}
	msg := p.Message
	for _, name := range slices.Sorted(maps.Keys(p.Fields)) {
		msg += "; " + name + " " + p.Fields[name]
	}
	return msg
}

// ParseError reports a path, query or tool argument that does not parse.
// Its message names the argument and value, and not the cause, so it is
// safe to show a client or a model.
type ParseError struct {
	Name, Value string
	Err         error
}

func (e *ParseError) Error() string { return fmt.Sprintf("invalid %s %q", e.Name, e.Value) }

func (e *ParseError) Unwrap() error { return e.Err }

// AsError returns err as an *errs.Error: a *ParseError becomes
// ErrInvalidRequest with a field for the argument, and any other error is
// errs.AsError's.
func AsError(ctx context.Context, err error) *errs.Error {
	var pe *ParseError
	if errors.As(err, &pe) {
		return ErrInvalidRequest.New(ctx).Field(pe.Name, fmt.Sprintf("invalid value %q", pe.Value)).AddError(err)
	}
	return errs.AsError(ctx, err)
}

// WriteError writes err with errs: its status, message, code and fields.
// An error that is not an *errs.Error is errs.Unknown, a 500.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		err = errors.New("afhttp: WriteError called with a nil error")
	}
	AsError(r.Context(), err).Abort(w)
}

// Map returns fn applied to each row: the response of a .map route with a
// :many query. It is never nil, so no rows respond [].
func Map[Row, T any](rows []Row, fn func(Row) T) []T {
	out := make([]T, len(rows))
	for i, row := range rows {
		out[i] = fn(row)
	}
	return out
}

// WriteJSON writes v as JSON with status, the response to r.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		errs.Unknown.New(r.Context()).AddError(err).Abort(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

// DecodeBody strictly decodes the JSON body of r into v: at most maxBytes
// (DefaultMaxBytes when 0), no unknown fields, no trailing data. Errors are
// ErrInvalidRequest, with the problem in the "body" field, or ErrTooLarge.
func DecodeBody(w http.ResponseWriter, r *http.Request, maxBytes int64, v any) error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	ctx := r.Context()
	invalid := func(problem string, err error) error {
		return ErrInvalidRequest.New(ctx).Field("body", problem).AddError(err)
	}
	body := &bodyReader{r: http.MaxBytesReader(w, r.Body, maxBytes)}
	// failed classifies the errors reading the body can cause, before the
	// JSON is blamed.
	failed := func(err error) error {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return ErrTooLarge.New(ctx).Param("max_bytes", maxBytes).AddError(err)
		}
		if body.err != nil {
			// The connection, not the JSON, failed.
			return errs.AsError(ctx, body.err)
		}
		return nil
	}
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if e := failed(err); e != nil {
			return e
		}
		if errors.Is(err, io.EOF) {
			return invalid("is empty; send a JSON object", err)
		}
		return invalid(strings.TrimPrefix(err.Error(), "json: "), err)
	}
	if _, err := dec.Token(); err != io.EOF {
		if e := failed(err); e != nil {
			return e
		}
		return invalid("has unexpected data after the JSON object", err)
	}
	return nil
}

// bodyReader records the error reading a request body fails with, other
// than its end or its size limit.
type bodyReader struct {
	r   io.Reader
	err error
}

func (b *bodyReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	var tooLarge *http.MaxBytesError
	if err != nil && err != io.EOF && !errors.As(err, &tooLarge) {
		b.err = err
	}
	return n, err
}

// Scanner is implemented by database types such as pgtype.Text and
// pgtype.UUID, which parse a string with Scan.
type Scanner interface {
	Scan(src any) error
}

// Parse parses s, the value of the path or query parameter name, into v,
// which must be a pointer. It supports strings, booleans, numbers, and types
// implementing encoding.TextUnmarshaler or Scanner. Errors are
// *ParseError, except for a v it can't parse into.
func Parse(name, s string, v any) error {
	var err error
	switch v := v.(type) {
	case *string:
		*v = s
	case *bool:
		*v, err = strconv.ParseBool(s)
	case *int:
		*v, err = strconv.Atoi(s)
	case *int8:
		err = parseInt(s, 8, v)
	case *int16:
		err = parseInt(s, 16, v)
	case *int32:
		err = parseInt(s, 32, v)
	case *int64:
		err = parseInt(s, 64, v)
	case *uint:
		err = parseUint(s, 0, v)
	case *uint8:
		err = parseUint(s, 8, v)
	case *uint16:
		err = parseUint(s, 16, v)
	case *uint32:
		err = parseUint(s, 32, v)
	case *uint64:
		err = parseUint(s, 64, v)
	case *float32:
		var f float64
		f, err = strconv.ParseFloat(s, 32)
		*v = float32(f)
	case *float64:
		*v, err = strconv.ParseFloat(s, 64)
	case encoding.TextUnmarshaler:
		err = v.UnmarshalText([]byte(s))
	case Scanner:
		err = v.Scan(s)
	default:
		return fmt.Errorf("afhttp: cannot parse parameter %s into %T", name, v)
	}
	if err != nil {
		return &ParseError{Name: name, Value: s, Err: err}
	}
	return nil
}

func parseInt[T int8 | int16 | int32 | int64](s string, bits int, v *T) error {
	n, err := strconv.ParseInt(s, 10, bits)
	*v = T(n)
	return err
}

func parseUint[T uint | uint8 | uint16 | uint32 | uint64](s string, bits int, v *T) error {
	n, err := strconv.ParseUint(s, 10, bits)
	*v = T(n)
	return err
}

// ParseQuery parses the query parameter name of r into v when it is
// present, and leaves v at its zero value otherwise.
func ParseQuery(r *http.Request, name string, v any) error {
	values, ok := r.URL.Query()[name]
	if !ok || len(values) == 0 {
		return nil
	}
	return Parse(name, values[0], v)
}

// WantsEventStream reports whether the client asked for a server-sent
// event stream with Accept: text/event-stream, and didn't refuse it with
// q=0.
func WantsEventStream(r *http.Request) bool {
	for _, v := range r.Header.Values("Accept") {
		for _, part := range strings.Split(v, ",") {
			mt, params, _ := strings.Cut(strings.TrimSpace(part), ";")
			if strings.EqualFold(strings.TrimSpace(mt), "text/event-stream") && quality(params) > 0 {
				return true
			}
		}
	}
	return false
}

// quality is the q parameter of a media range's params: 1 when absent, 0
// when malformed.
func quality(params string) float64 {
	for _, p := range strings.Split(params, ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(p), "=")
		if strings.EqualFold(strings.TrimSpace(name), "q") {
			q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || q < 0 || q > 1 {
				return 0
			}
			return q
		}
	}
	return 1
}

// EventStream writes server-sent events. Its methods are safe to call from
// several goroutines.
type EventStream struct {
	// WriteTimeout, when set, bounds each event's write, so a client that
	// stops reading can't block the sender for longer. After a write
	// fails, the stream writes nothing more and Send returns that error.
	// It needs a ResponseWriter that can set write deadlines; middleware
	// that wraps one should implement Unwrap, as http.ResponseController
	// expects. Writers that can't, such as a recorder, write without one.
	WriteTimeout time.Duration

	mu     sync.Mutex
	w      http.ResponseWriter
	rc     *http.ResponseController
	broken error
}

// NewEventStream starts a 200 text/event-stream response on w.
func NewEventStream(w http.ResponseWriter) *EventStream {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	// Ask proxies such as nginx not to buffer the stream.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	s := &EventStream{w: w, rc: http.NewResponseController(w)}
	s.rc.Flush()
	return s
}

// Send writes one event, with data encoded as JSON, and flushes it.
func (s *EventStream) Send(event string, data any) error {
	return s.SendEvent("", event, data)
}

// SendEvent is Send with an event ID, which a client that reconnects sends
// back in Last-Event-ID to resume after it. An empty id sends none.
func (s *EventStream) SendEvent(id, event string, data any) error {
	if strings.ContainsAny(id, "\r\n") {
		return fmt.Errorf("afhttp: event id %q has a line break", id)
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken != nil {
		return s.broken
	}
	if s.WriteTimeout > 0 {
		// Writers that can't time out, such as a recorder, never block.
		if err := s.rc.SetWriteDeadline(time.Now().Add(s.WriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			s.broken = err
			return err
		}
	}
	var frame string
	if id != "" {
		frame = "id: " + id + "\n"
	}
	if _, err := fmt.Fprintf(s.w, "%sevent: %s\ndata: %s\n\n", frame, event, b); err != nil {
		s.broken = err
		return err
	}
	if err := s.rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.broken = err
		return err
	}
	if s.WriteTimeout > 0 {
		// The connection may serve another request after this one: leave
		// it no deadline.
		if err := s.rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
			s.broken = err
			return err
		}
	}
	return nil
}

// ErrStreamClosed is what Send returns after Close.
var ErrStreamClosed = errors.New("afhttp: the event stream is closed")

// Close ends the stream: it fails a write in progress, waits for it, and
// Send writes nothing after it. Call it before the handler returns when other
// goroutines may still send, since the ResponseWriter is done then. Failing
// the write takes a ResponseWriter that can set write deadlines, as
// WriteTimeout does; with one that can't, Close waits for the write.
func (s *EventStream) Close() {
	// A write stalled on a client that stopped reading would hold the lock;
	// this fails it now.
	_ = s.rc.SetWriteDeadline(time.Now())
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken == nil {
		s.broken = ErrStreamClosed
	}
	// The response may still end well, and the connection serve another.
	_ = s.rc.SetWriteDeadline(time.Time{})
}

// SendError writes an error event whose data is the client view of err,
// as WriteError would write it, and logs err with errs.
func (s *EventStream) SendError(ctx context.Context, err error) error {
	return s.Send("error", AsError(ctx, err).Emit("sse"))
}
