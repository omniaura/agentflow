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
package token

import (
	"bytes"
	"log/slog"
	"strconv"
	"strings"

	"github.com/peyton-spencer/caseconv/bytcase"
)

type Slice []T

func (t Slice) Equal(o Slice) bool {
	if len(t) != len(o) {
		return false
	}
	for i, tok := range t {
		if tok != o[i] {
			return false
		}
	}
	return true
}

type T struct {
	Kind  Kind
	Start int
	End   int
}

func (t T) Get(in []byte) []byte {
	return in[t.Start:t.End]
}

func (t T) GetWrap(in []byte, left, right byte) []byte {
	out := make([]byte, 0, len(in)+2)
	out = append(out, left)
	out = append(out, in[t.Start:t.End]...)
	out = append(out, right)
	return out
}

func (t T) GetWrapLL(in []byte, left []byte, right byte) []byte {
	out := make([]byte, 0, len(in)+len(left)+1)
	out = append(out, left...)
	out = append(out, in[t.Start:t.End]...)
	out = append(out, right)
	return out
}

func (t T) GetJSFmtVar(in []byte) []byte {
	out := make([]byte, 0, len(in)+3)
	out = append(out, '$', '{')
	out = append(out, bytcase.ToLowerCamel(in[t.Start:t.End])...)
	out = append(out, '}')
	return out
}

type Kind int

// TODO: add KindDoc, KindVarDoc
// TODO: add .var var predeclare optional; sets the types
// example:
//
// .title say hello to your new friends
// .var names string list join="\n"
// Please say hello to:
// <!names>
//
// INPUT:
// Joe,Mary,Jane
//
// OUTPUT:
// Please say hello to:
// Joe
// Mary
// Jane

const (
	KindUnset = iota
	KindTitle
	KindText
	// TODO: add var parameters
	// such as:
	// <!name string>
	// <!age int>
	// <!is_admin bool>
	// <!created_at datetime>
	// <!meeting_time time>
	// <!meeting_date date>
	// <!any_data any>
	// <!todos string list join="\n">
	// <!weights float32 list join=",">
	// <!flags bool list join="," start="[" end="]">
	// <!names join="\n">
	KindVar
	KindRawBlock
)

func (k Kind) String() string {
	switch k {
	case KindTitle:
		return "title"
	case KindText:
		return "text"
	case KindVar:
		return "var"
	case KindUnset:
		return "unset"
	}
	return "unknown"
}

var (
	cmdTitle = []byte(".title")
)

func Tokenize(input []byte) (Slice, error) {
	var tokens []T
	var ct T
	startLine := true
	cmdStart := -1
	cmdEnd := cmdStart
	for i, b := range input {
		if startLine {
			startLine = false
			switch b {
			case '~':
			case '.':
				cmdStart = i
				if ct.Kind != KindUnset {
					// trim newlines before the directive
					// sub := 1
					// for input[i-sub] == '\n' {
					// 	sub++
					// }
					// ct.End = i - sub + 1
					if input[i-1] == '\n' {
						if input[i-2] == '\n' {
							ct.End = i - 2
						} else {
							ct.End = i - 1
						}
					} else {
						ct.End = i
					}
					tokens = append(tokens, ct)
					// log.Trace().Msgf("command start: %+v", ct)
				} else {
					// log.Trace().Msgf("kind was previously set: %+v", ct)
				}
			case '<':
			case '\n':
			default:
			}
		}

		if cmdStart != -1 {
			switch b {
			case ' ':
				cmdEnd = i
				switch {
				case bytes.Equal(input[cmdStart:cmdEnd], cmdTitle):
					ct.Kind = KindTitle
					ct.Start = i + 1
				}
			}
		}

		if ct.Kind == KindVar && b == '>' {
			ct.End = i
			tokens = append(tokens, ct)
			ct = T{}
			continue
		}
		if b == '<' && len(input) > i && input[i+1] == '!' {
			if ct.Kind != KindUnset {
				ct.End = i
				tokens = append(tokens, ct)
			}
			ct.Kind = KindVar
			ct.Start = i + 2

		}

		if b == '\n' {
			startLine = true
			switch ct.Kind {
			case KindText:

			case KindTitle:
				ct.End = i
				tokens = append(tokens, ct)
				ct = T{}
				continue
			case KindUnset:
				if len(input) > i+1 {
					switch input[i+1] {
					case '.':
						slog.Debug("ignoring text node", "token", ct.Stringify(input))
						continue
					}
				}
			}
		}

		if ct.Kind == KindUnset {
			ct.Kind = KindText
			ct.Start = i
		}

		if i == len(input)-1 && ct.Kind != KindUnset {
			ct.End = i + 1
			// slog.Debug("end of input, adding token", "token", ct.Stringify(input))

			tokens = append(tokens, ct)
		}
	}
	return tokens, nil
}

func (t T) Stringify(in []byte) string {
	var buf strings.Builder
	buf.Grow(len(in) + 100)
	buf.WriteString(t.Kind.String())
	buf.WriteString(":\t[")
	buf.WriteString(strconv.Itoa(t.Start))
	buf.WriteString(":")
	buf.WriteString(strconv.Itoa(t.End))
	buf.WriteString("]\t")
	if t.Start < 0 || t.End > len(in) || t.Start > t.End {
		buf.WriteString("INVALID BOUNDS")
	} else {
		buf.WriteString("\"")
		buf.Write(in[t.Start:t.End])
		buf.WriteString("\"")
	}
	return buf.String()
}

func (s Slice) Stringify(in []byte) string {
	if len(in) == 0 {
		return "no content"
	}
	if len(s) == 0 {
		return "no tokens"
	}
	var buf strings.Builder
	buf.Grow(len(in) + 100)
	for i, tok := range s {
		buf.WriteString(tok.Stringify(in))
		if i != len(s)-1 {
			buf.WriteRune('\n')
		}
	}
	return buf.String()
}
