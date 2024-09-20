package require

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type Equaler[T any] interface {
	Equal(actual T) bool
}

func EqualErr(t *testing.T, want, got error) {
	if !errors.Is(want, got) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func Equal[T Equaler[T]](t *testing.T, want, got T) {
	if !want.Equal(got) {
		var sb strings.Builder
		sb.WriteString("Should equal:\n")
		wantGot(&sb, want, got)
		t.Fatal(sb.String())
	}
}

func NotEqual[T Equaler[T]](t *testing.T, want, got T) {
	if want.Equal(got) {
		var sb strings.Builder
		sb.WriteString("Should not equal:\n")
		wantGot(&sb, want, got)
		t.Fatal(sb.String())
	}
}

func NoError(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func wantGot(sb *strings.Builder, want, got any) {
	sb.WriteString("\x1b[1mWANT:\x1b[0m ")
	if s, ok := want.(fmt.Stringer); ok {
		sb.WriteString(s.String())
	} else {
		sb.WriteString(fmt.Sprintf("%+v", want))
	}
	sb.WriteString("\n\x1b[1mGOT:\x1b[0m  ")
	if s, ok := got.(fmt.Stringer); ok {
		sb.WriteString(s.String())
	} else {
		sb.WriteString(fmt.Sprintf("%+v", got))
	}
}
