package afhttp_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/runtime/afhttp"
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
		var e *afhttp.Error
		if !errors.As(err, &e) || e.Status != 400 || e.Msg != `invalid p "`+tt.in+`"` {
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
		in     string
		max    int64
		status int
		msg    string
	}{
		{`{"a":"x"}`, 0, 0, ""},
		{`{"a":"x","b":1}`, 0, 400, `invalid request body: unknown field "b"`},
		{`{"a":"x"} []`, 0, 400, "unexpected data after the JSON object"},
		{``, 0, 400, "request body is empty"},
		{`{"a":`, 0, 400, "invalid request body"},
		{`{"a":"` + strings.Repeat("x", 100) + `"}`, 50, 413, "request body is over 50 bytes"},
	} {
		var v body
		err := afhttp.DecodeBody(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader(tt.in)), tt.max, &v)
		if tt.status == 0 {
			if err != nil || v.A != "x" {
				t.Fatalf("%s: %+v, %v", tt.in, v, err)
			}
			continue
		}
		var e *afhttp.Error
		if !errors.As(err, &e) || e.Status != tt.status || !strings.Contains(e.Msg, tt.msg) {
			t.Fatalf("%s: got %v, want %d %s", tt.in, err, tt.status, tt.msg)
		}
	}
}

func TestErrors(t *testing.T) {
	cause := errors.New("db down")
	for _, tt := range []struct {
		err    error
		status int
		body   string
	}{
		{afhttp.Internal(cause), 500, `{"error":"internal error"}`},
		{afhttp.NotFound(nil), 404, `{"error":"not found"}`},
		{afhttp.Unauthorized(cause), 401, `{"error":"unauthorized"}`},
		// An authenticator's own *Error keeps its status.
		{afhttp.Unauthorized(afhttp.Errorf(403, "banned")), 403, `{"error":"banned"}`},
		{afhttp.Internal(afhttp.Errorf(409, "taken")), 409, `{"error":"taken"}`},
		{cause, 500, `{"error":"internal error"}`},
	} {
		w := httptest.NewRecorder()
		afhttp.WriteError(w, httptest.NewRequest("GET", "/", nil), tt.err)
		if w.Code != tt.status || strings.TrimSpace(w.Body.String()) != tt.body || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%v: got %d %s", tt.err, w.Code, w.Body)
		}
	}
	if !errors.Is(afhttp.Internal(cause), cause) {
		t.Fatal("Internal does not wrap its cause")
	}
	w := httptest.NewRecorder()
	afhttp.WriteJSON(w, 200, func() {})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unencodable value: %d", w.Code)
	}
}
