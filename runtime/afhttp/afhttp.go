// Package afhttp is the runtime for routes declared with .route in .af
// files: strict request decoding, argument parsing, and JSON responses and
// errors. It has no dependencies, so it is safe to import from generated
// code and to eject into a repository.
package afhttp

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// DefaultMaxBytes caps a request body when its .route section sets no
// .maxbytes.
const DefaultMaxBytes = 1 << 20

// Error is an error with the HTTP status it should be reported with.
type Error struct {
	Status int
	// Msg is safe to show to the client.
	Msg string
	// Err is the cause, for logs.
	Err error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Msg
	}
	return e.Msg + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// Errorf returns an *Error with status and a client-safe message.
func Errorf(status int, format string, args ...any) *Error {
	return &Error{Status: status, Msg: fmt.Sprintf(format, args...)}
}

// Unauthorized wraps an authentication failure. An *Error from the
// authenticator keeps its own status.
func Unauthorized(err error) error {
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	return &Error{Status: http.StatusUnauthorized, Msg: "unauthorized", Err: err}
}

// NotFound is the error for a query that found nothing.
func NotFound(err error) error {
	return &Error{Status: http.StatusNotFound, Msg: "not found", Err: err}
}

// Internal wraps a query failure. The client sees only a generic message.
func Internal(err error) error {
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	return &Error{Status: http.StatusInternalServerError, Msg: "internal error", Err: err}
}

// WriteError writes err as {"error": msg} with its status: the status of an
// *Error, or 500.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var e *Error
	if !errors.As(err, &e) {
		e = &Error{Status: http.StatusInternalServerError, Msg: "internal error", Err: err}
	}
	WriteJSON(w, e.Status, map[string]string{"error": e.Msg})
}

// WriteJSON writes v as JSON with status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"internal error"}`)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

// DecodeBody strictly decodes the JSON body of r into v: at most maxBytes
// (DefaultMaxBytes when 0), no unknown fields, no trailing data. Errors are
// *Error with status 400 or 413.
func DecodeBody(w http.ResponseWriter, r *http.Request, maxBytes int64, v any) error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			return &Error{Status: http.StatusRequestEntityTooLarge, Msg: fmt.Sprintf("request body is over %d bytes", maxBytes), Err: err}
		case errors.Is(err, io.EOF):
			return &Error{Status: http.StatusBadRequest, Msg: "request body is empty; send a JSON object", Err: err}
		}
		return &Error{Status: http.StatusBadRequest, Msg: "invalid request body: " + strings.TrimPrefix(err.Error(), "json: "), Err: err}
	}
	if _, err := dec.Token(); err != io.EOF {
		return &Error{Status: http.StatusBadRequest, Msg: "invalid request body: unexpected data after the JSON object"}
	}
	return nil
}

// Scanner is implemented by database types such as pgtype.Text and
// pgtype.UUID, which parse a string with Scan.
type Scanner interface {
	Scan(src any) error
}

// Parse parses s, the value of the path or query parameter name, into v,
// which must be a pointer. It supports strings, booleans, numbers, and types
// implementing encoding.TextUnmarshaler or Scanner. Errors are *Error with
// status 400.
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
		return &Error{Status: http.StatusInternalServerError, Msg: "internal error", Err: fmt.Errorf("afhttp: cannot parse parameter %s into %T", name, v)}
	}
	if err != nil {
		return &Error{Status: http.StatusBadRequest, Msg: fmt.Sprintf("invalid %s %q", name, s), Err: err}
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
