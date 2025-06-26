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
	"runtime"
	"strings"
	"testing"
)

type Equaler[T any] interface {
	Equal(actual T) bool
}

func EqualErr(t *testing.T, want, got error) {
	if !errors.Is(want, got) {
		// Get caller info for clickable file:line
		_, file, line, ok := runtime.Caller(1)
		if ok {
			t.Fatalf("%s:%d: expected %v, got %v", file, line, want, got)
		} else {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func Equal[T Equaler[T]](t *testing.T, want, got T) {
	if !want.Equal(got) {
		var sb strings.Builder
		// Get caller info for clickable file:line
		_, file, line, ok := runtime.Caller(1)
		if ok {
			sb.WriteString(fmt.Sprintf("%s:%d: ", file, line))
		}
		sb.WriteString("Should equal:\n")
		WantGotDiff(&sb, want, got)
		t.Fatal(sb.String())
	}
}

func NotEqual[T Equaler[T]](t *testing.T, want, got T) {
	if want.Equal(got) {
		var sb strings.Builder
		// Get caller info for clickable file:line
		_, file, line, ok := runtime.Caller(1)
		if ok {
			sb.WriteString(fmt.Sprintf("%s:%d: ", file, line))
		}
		sb.WriteString("Should not equal:\n")
		WantGotDiff(&sb, want, got)
		t.Fatal(sb.String())
	}
}

func NoError(t *testing.T, err error) {
	if err != nil {
		// Get caller info for clickable file:line
		_, file, line, ok := runtime.Caller(1)
		if ok {
			t.Fatalf("%s:%d: expected no error, got %v", file, line, err)
		} else {
			t.Fatalf("expected no error, got %v", err)
		}
	}
}

// makeWhitespaceVisible replaces whitespace characters with visible representations
func makeWhitespaceVisible(s string) string {
	s = strings.ReplaceAll(s, " ", "·")
	s = strings.ReplaceAll(s, "\t", "→")
	s = strings.ReplaceAll(s, "\n", "↵\n")
	s = strings.ReplaceAll(s, "\r", "⤴")
	return s
}

// WantGotDiff shows a diff-style comparison with visible whitespace
func WantGotDiff(sb *strings.Builder, want, got any) {
	var wantStr, gotStr string
	if s, ok := want.(fmt.Stringer); ok {
		wantStr = s.String()
	} else {
		wantStr = fmt.Sprintf("%+v", want)
	}
	if s, ok := got.(fmt.Stringer); ok {
		gotStr = s.String()
	} else {
		gotStr = fmt.Sprintf("%+v", got)
	}

	wantLines := strings.Split(wantStr, "\n")
	gotLines := strings.Split(gotStr, "\n")

	sb.WriteString("\n\x1b[1mDIFF:\x1b[0m\n")

	maxLines := len(wantLines)
	if len(gotLines) > maxLines {
		maxLines = len(gotLines)
	}

	for i := 0; i < maxLines; i++ {
		var wantLine, gotLine string
		if i < len(wantLines) {
			wantLine = wantLines[i]
		}
		if i < len(gotLines) {
			gotLine = gotLines[i]
		}

		if wantLine == gotLine {
			sb.WriteString(fmt.Sprintf("  %s\n", makeWhitespaceVisible(wantLine)))
		} else {
			if wantLine != "" {
				sb.WriteString(fmt.Sprintf("\x1b[31m- %s\x1b[0m\n", makeWhitespaceVisible(wantLine)))
			}
			if gotLine != "" {
				sb.WriteString(fmt.Sprintf("\x1b[32m+ %s\x1b[0m\n", makeWhitespaceVisible(gotLine)))
			}
		}
	}
}

func WantGot(sb *strings.Builder, want, got any) {
	sb.WriteString("\x1b[1mWANT:\x1b[0m\n")
	if s, ok := want.(fmt.Stringer); ok {
		sb.WriteString(s.String())
	} else {
		fmt.Fprintf(sb, "%+v", want)
	}
	sb.WriteString("\n\x1b[1mGOT:\x1b[0m\n")
	if s, ok := got.(fmt.Stringer); ok {
		sb.WriteString(s.String())
	} else {
		fmt.Fprintf(sb, "%+v", got)
	}
}

func WantGotBoldQuotes(sb *strings.Builder, want, got any) {
	sb.WriteString("\x1b[1mWANT:\x1b[0m\n")
	sb.WriteString("\x1b[1m|\x1b[0m")
	if s, ok := want.(fmt.Stringer); ok {
		sb.WriteString(s.String())
	} else {
		fmt.Fprintf(sb, "%+v", want)
	}
	sb.WriteString("\x1b[1m|\x1b[0m")
	sb.WriteString("\n\x1b[1mGOT:\x1b[0m\n")
	sb.WriteString("\x1b[1m|\x1b[0m")
	if s, ok := got.(fmt.Stringer); ok {
		sb.WriteString(s.String())
	} else {
		fmt.Fprintf(sb, "%+v", got)
	}
	sb.WriteString("\x1b[1m|\x1b[0m")
}
