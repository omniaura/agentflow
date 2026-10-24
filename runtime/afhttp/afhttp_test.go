package afhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/runtime/afhttp"
	"github.com/omniaura/go-kit/errs"
)

type scanned struct{ v string }

func (s *scanned) Scan(src any) error {
	str, ok := src.(string)
	if !ok || str == "bad" {
		return errors.New("bad")
	}
	s.v = str
	return nil
}

func TestParse(t *testing.T) {
	var (
		s   string
		b   bool
		i   int
		i8  int8
		i32 int32
		i64 int64
		u   uint
		u16 uint16
		f32 float32
		f64 float64
		ip  netip.Addr
		sc  scanned
	)
	for _, tt := range []struct {
		in string
		v  any
	}{
		{"x y", &s}, {"true", &b}, {"-3", &i}, {"-8", &i8}, {"32", &i32}, {"64", &i64},
		{"7", &u}, {"16", &u16}, {"1.5", &f32}, {"2.5", &f64}, {"10.0.0.1", &ip}, {"ok", &sc},
	} {
		if err := afhttp.Parse("p", tt.in, tt.v); err != nil {
			t.Fatalf("Parse(%q, %T): %v", tt.in, tt.v, err)
		}
	}
	if s != "x y" || !b || i != -3 || i8 != -8 || i32 != 32 || i64 != 64 || u != 7 || u16 != 16 || f32 != 1.5 || f64 != 2.5 || ip.String() != "10.0.0.1" || sc.v != "ok" {
		t.Fatal("wrong values")
	}
	for _, tt := range []struct {
		in string
		v  any
	}{{"300", &i8}, {"-1", &u}, {"x", &b}, {"1.5", &i}, {"bad", &sc}, {"::x", &ip}} {
		err := afhttp.Parse("p", tt.in, tt.v)
		var e *afhttp.ParseError
		if !errors.As(err, &e) || e.Error() != `invalid p "`+tt.in+`"` || e.Err == nil {
			t.Fatalf("Parse(%q, %T) = %v", tt.in, tt.v, err)
		}
	}
	var unsupported struct{}
	if err := afhttp.Parse("p", "x", &unsupported); err == nil || !strings.Contains(err.Error(), "cannot parse parameter p") {
		t.Fatalf("got %v", err)
	}
}

func TestParseQuery(t *testing.T) {
	r := httptest.NewRequest("GET", "/x?n=4&n=5&empty=", nil)
	n, missing := 1, 9
	empty := "unset"
	if err := afhttp.ParseQuery(r, "n", &n); err != nil || n != 4 {
		t.Fatalf("n = %d, %v", n, err)
	}
	if err := afhttp.ParseQuery(r, "missing", &missing); err != nil || missing != 9 {
		t.Fatalf("missing = %d, %v", missing, err)
	}
	if err := afhttp.ParseQuery(r, "empty", &empty); err != nil || empty != "" {
		t.Fatalf("empty = %q, %v", empty, err)
	}
}

func TestDecodeBody(t *testing.T) {
	type body struct {
		A string `json:"a"`
	}
	for _, tt := range []struct {
		in      string
		max     int64
		status  int
		problem string
	}{
		{`{"a":"x"}`, 0, 0, ""},
		{`{"a":"x","b":1}`, 0, 400, `unknown field "b"`},
		{`{"a":"x"} []`, 0, 400, "has unexpected data after the JSON object"},
		{``, 0, 400, "is empty; send a JSON object"},
		{`{"a":`, 0, 400, "unexpected EOF"},
		{`{"a":"` + strings.Repeat("x", 100) + `"}`, 50, 413, ""},
	} {
		var v body
		err := afhttp.DecodeBody(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader(tt.in)), tt.max, &v)
		if tt.status == 0 {
			if err != nil || v.A != "x" {
				t.Fatalf("%s: %+v, %v", tt.in, v, err)
			}
			continue
		}
		var e *errs.Error
		if !errors.As(err, &e) {
			t.Fatalf("%s: got %v", tt.in, err)
		}
		pub := e.Public()
		if pub.Status != tt.status || pub.Fields["body"] != tt.problem || pub.Action != errs.ActionFixInput {
			t.Fatalf("%s: got %+v, want %d %q", tt.in, pub, tt.status, tt.problem)
		}
		if tt.status == 413 && pub.Params["max_bytes"] != int64(50) {
			t.Fatalf("413 params = %v", pub.Params)
		}
	}
}

// failingBody returns data, then fails like a dropped connection.
type failingBody struct{ data string }

func (b *failingBody) Read(p []byte) (int, error) {
	if b.data == "" {
		return 0, errors.New("read tcp: connection reset by peer")
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

func TestDecodeBodyReadFailures(t *testing.T) {
	type body struct {
		A string `json:"a"`
	}
	for _, in := range []string{`{"a":"x"}`, `{"a":`, ``} {
		r := httptest.NewRequest("POST", "/", &failingBody{in})
		var e *errs.Error
		err := afhttp.DecodeBody(httptest.NewRecorder(), r, 0, &body{})
		if !errors.As(err, &e) || e.Public().Status != 500 {
			t.Errorf("%q then a read failure: got %v", in, err)
		}
	}
	// Trailing data past the limit is too large, not unexpected.
	err := afhttp.DecodeBody(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader(`{"a":"x"} `+strings.Repeat("1", 100))), 20, &body{})
	var e *errs.Error
	if !errors.As(err, &e) || e.Public().Status != 413 {
		t.Errorf("trailing data past the limit: got %v", err)
	}
}

type pgError struct{ code string }

func (e *pgError) Error() string    { return "ERROR: duplicate key (SQLSTATE " + e.code + ")" }
func (e *pgError) SQLState() string { return e.code }

func TestErrors(t *testing.T) {
	ctx := context.Background()
	cause := errors.New("db down")
	banned := errs.NewFactory(403, "banned", errs.WithCode("banned"))
	for _, tt := range []struct {
		err  error
		want errs.Public
	}{
		{afhttp.QueryFailed(ctx, cause), errs.Public{Status: 500, Code: "internal_error", Message: "unknown error"}},
		{afhttp.QueryFailed(ctx, fmt.Errorf("insert: %w", &pgError{"23505"})), errs.Public{Status: 409, Code: "already_exists", Message: "already exists"}},
		{afhttp.QueryFailed(ctx, &pgError{"23503"}), errs.Public{Status: 500, Code: "internal_error", Message: "unknown error"}},
		// A wrapper around the queries can fail with its own errs factory.
		{afhttp.QueryFailed(ctx, fmt.Errorf("list: %w", banned.New(ctx))), errs.Public{Status: 403, Code: "banned", Message: "banned"}},
		// Even when the cause it wraps is a unique violation.
		{afhttp.QueryFailed(ctx, banned.New(ctx).AddError(&pgError{"23505"})), errs.Public{Status: 403, Code: "banned", Message: "banned"}},
		{afhttp.ErrNotFound.New(ctx), errs.Public{Status: 404, Code: "not_found", Message: "not found"}},
		{afhttp.Unauthorized(ctx, cause), errs.Public{Status: 401, Code: "unauthorized", Message: "unauthorized", Action: errs.ActionSignIn}},
		// An authenticator's own *errs.Error keeps its status.
		{afhttp.Unauthorized(ctx, fmt.Errorf("check: %w", banned.New(ctx))), errs.Public{Status: 403, Code: "banned", Message: "banned"}},
		{&afhttp.ParseError{Name: "id", Value: "x", Err: cause}, errs.Public{Status: 400, Code: "invalid_request", Message: "invalid request", Action: errs.ActionFixInput, Fields: map[string]string{"id": `invalid value "x"`}}},
		{afhttp.ErrModelFailed.New(ctx).AddError(cause), errs.Public{Status: 502, Code: "model_failed", Message: "the model could not answer", Action: errs.ActionRetry, Retryable: true}},
		{cause, errs.Public{Status: 500, Code: "internal_error", Message: "unknown error"}},
		{nil, errs.Public{Status: 500, Code: "internal_error", Message: "unknown error"}},
	} {
		w := httptest.NewRecorder()
		afhttp.WriteError(w, httptest.NewRequest("GET", "/", nil), tt.err)
		var got errs.Public
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Ref == "" {
			t.Errorf("%v: no ref", tt.err)
		}
		got.Ref = ""
		if w.Code != tt.want.Status || !reflect.DeepEqual(got, tt.want) || w.Header().Get("Content-Type") != "application/json" {
			t.Errorf("%v: got %d %+v, want %+v", tt.err, w.Code, got, tt.want)
		}
		if strings.Contains(w.Body.String(), "db down") || strings.Contains(w.Body.String(), "SQLSTATE") {
			t.Errorf("%v: the cause reached the client: %s", tt.err, w.Body)
		}
	}
	if !errors.Is(afhttp.QueryFailed(ctx, cause), cause) {
		t.Fatal("QueryFailed does not wrap its cause")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r = r.WithContext(errs.WithRef(r.Context(), "req-1"))
	afhttp.WriteJSON(w, r, 200, func() {})
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), `"ref":"req-1"`) {
		t.Fatalf("unencodable value: %d %s", w.Code, w.Body)
	}
}
