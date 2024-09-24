/*
Copyright © 2024 Omni Aura peyton@omniaura.co

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
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
		WantGot(&sb, want, got)
		t.Fatal(sb.String())
	}
}

func NotEqual[T Equaler[T]](t *testing.T, want, got T) {
	if want.Equal(got) {
		var sb strings.Builder
		sb.WriteString("Should not equal:\n")
		WantGot(&sb, want, got)
		t.Fatal(sb.String())
	}
}

func NoError(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func WantGot(sb *strings.Builder, want, got any) {
	sb.WriteString("\x1b[1mWANT:\x1b[0m\n")
	if s, ok := want.(fmt.Stringer); ok {
		sb.WriteString(s.String())
	} else {
		sb.WriteString(fmt.Sprintf("%+v", want))
	}
	sb.WriteString("\n\x1b[1mGOT:\x1b[0m\n")
	if s, ok := got.(fmt.Stringer); ok {
		sb.WriteString(s.String())
	} else {
		sb.WriteString(fmt.Sprintf("%+v", got))
	}
}

func WantGotBoldQuotes(sb *strings.Builder, want, got any) {
	sb.WriteString("\x1b[1mWANT:\x1b[0m\n")
	sb.WriteString("\x1b[1m|\x1b[0m")
	if s, ok := want.(fmt.Stringer); ok {
		sb.WriteString(s.String())
	} else {
		sb.WriteString(fmt.Sprintf("%+v", want))
	}
	sb.WriteString("\x1b[1m|\x1b[0m")
	sb.WriteString("\n\x1b[1mGOT:\x1b[0m\n")
	sb.WriteString("\x1b[1m|\x1b[0m")
	if s, ok := got.(fmt.Stringer); ok {
		sb.WriteString(s.String())
	} else {
		sb.WriteString(fmt.Sprintf("%+v", got))
	}
	sb.WriteString("\x1b[1m|\x1b[0m")
}
