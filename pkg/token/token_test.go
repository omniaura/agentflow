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
package token_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
	"github.com/omniaura/agentflow/pkg/logger"
	"github.com/omniaura/agentflow/pkg/token"
	"github.com/omniaura/agentflow/pkg/token/kind"
)

func TestMain(m *testing.M) {
	logger.SetupLevel(slog.LevelDebug)
	m.Run()
}

type TestCase struct {
	name string
	def  func() (in []byte, want token.Slice, wantErr error)
}

func (tc TestCase) Run(t *testing.T) {
	t.Run(tc.name, func(t *testing.T) {
		in, want, wantErr := tc.def()
		got, err := token.Tokenize(in)
		if wantErr != nil {
			require.EqualErr(t, wantErr, err)
		} else {
			require.NoError(t, err)
			if !want.Equal(got) {
				var sb strings.Builder
				sb.WriteString("tokens not equal\n")
				sb.WriteString("\x1b[1;37mINPUT:\x1b[0m\n")
				sb.Write(in)
				sb.WriteString("\n\x1b[1;37mWANT:\x1b[0m\n")
				sb.WriteString(want.Stringify(in))
				sb.WriteString("\n\x1b[1;37mGOT:\x1b[0m\n")
				sb.WriteString(got.Stringify(in))
				t.Error(sb.String())
			}
		}
	})
}

var (
	helloW         = []byte("hello world")
	helloMultiline = bytes.Join([][]byte{helloW, helloW, helloW}, []byte{'\n'})
)

func TestText(t *testing.T) {
	testcases := []TestCase{
		{
			name: "one line",
			def: func() ([]byte, token.Slice, error) {
				want := token.Slice{
					{
						Kind:  kind.Text,
						Start: 0,
						End:   len(helloW),
					},
				}
				return helloW, want, nil
			},
		},
		{
			name: "multi line",
			def: func() ([]byte, token.Slice, error) {
				want := token.Slice{
					{
						Kind:  kind.Text,
						Start: 0,
						End:   len(helloMultiline),
					},
				}
				return helloMultiline, want, nil
			},
		},
	}
	for _, tc := range testcases {
		tc.Run(t)
	}
}
func TestTitle(t *testing.T) {
	testcases := []TestCase{
		{
			name: "one title",
			def: func() ([]byte, token.Slice, error) {
				tCmd := []byte(".title ")
				title := []byte("hey prompt")
				line1 := append(tCmd, title...)
				line2 := helloW
				input := joinLines(line1, line2, line2)
				want := []token.T{
					{
						Kind:  kind.Title,
						Start: len(tCmd),
						End:   len(tCmd) + len(title),
					},
					{
						Kind:  kind.Text,
						Start: len(tCmd) + len(title) + 1, // newline omitted
						End:   len(input),
					},
				}
				return input, want, nil
			},
		},
		{
			name: "two titles",
			def: func() ([]byte, token.Slice, error) {
				tCmd := []byte(".title ")
				title1 := []byte("hey prompt")
				title2 := []byte("hey prompt 2")
				line1 := append(tCmd, title1...)
				line2 := append(tCmd, title2...)
				input := joinLines(line1, helloW, line2, helloW)
				want := []token.T{
					{
						Kind:  kind.Title,
						Start: len(tCmd),
						End:   len(tCmd) + len(title1),
					},
					{
						Kind:  kind.Text,
						Start: len(line1) + 1, // +1 for newline
						End:   len(line1) + 1 + len(helloW),
					},
					{
						Kind:  kind.Title,
						Start: len(line1) + 1 + len(helloW) + 1 + len(tCmd), // +1 for newline
						End:   len(line1) + 1 + len(helloW) + 1 + len(tCmd) + len(title2),
					},
					{
						Kind:  kind.Text,
						Start: len(line1) + 1 + len(helloW) + 1 + len(line2) + 1, // +1 for newline
						End:   len(input),
					},
				}
				return input, want, nil
			},
		},
	}

	for _, tc := range testcases {
		tc.Run(t)
	}
}

func TestVar(t *testing.T) {
	varStart := []byte("<!")
	varName := []byte("var1")
	varName2 := []byte("var2")
	varEnd := []byte(">")
	var1 := append(append(varStart, varName...), varEnd...)
	var2 := append(append(varStart, varName2...), varEnd...)
	testcases := []TestCase{
		{
			name: "one",
			def: func() ([]byte, token.Slice, error) {
				want := token.Slice{
					{
						Kind:  kind.Var,
						Start: len(varStart),
						End:   len(var1) - len(varEnd),
					},
				}
				return var1, want, nil
			},
		},
		{
			name: "start of text line",
			def: func() ([]byte, token.Slice, error) {
				line := bytes.Join([][]byte{var1, helloW}, []byte{' '})
				want := token.Slice{
					{
						Kind:  kind.Var,
						Start: len(varStart),
						End:   len(var1) - len(varEnd),
					},
					{
						Kind:  kind.Text,
						Start: len(var1),
						End:   len(line),
					},
				}
				return line, want, nil
			},
		},
		{
			name: "end of text line",
			def: func() ([]byte, token.Slice, error) {
				line := bytes.Join([][]byte{helloW, var1}, []byte{' '})
				want := token.Slice{
					{
						Kind:  kind.Text,
						Start: 0,
						End:   len(helloW) + 1,
					},
					{
						Kind:  kind.Var,
						Start: len(helloW) + 3,
						End:   len(line) - 1,
					},
				}
				return line, want, nil
			},
		},
		{
			name: "start of multiline text",
			def: func() ([]byte, token.Slice, error) {
				line := bytes.Join([][]byte{var1, helloW}, []byte{' '})
				want := token.Slice{
					{
						Kind:  kind.Var,
						Start: len(varStart),
						End:   len(var1) - len(varEnd),
					},
					{
						Kind:  kind.Text,
						Start: len(var1),
						End:   len(line),
					},
				}
				return line, want, nil
			},
		},
		{
			name: "end of multiline text",
			def: func() ([]byte, token.Slice, error) {
				line := bytes.Join([][]byte{helloW, var1}, []byte{' '})
				want := token.Slice{
					{
						Kind:  kind.Text,
						Start: 0,
						End:   len(helloW) + 1,
					},
					{
						Kind:  kind.Var,
						Start: len(helloW) + 3,
						End:   len(line) - 1,
					},
				}
				return line, want, nil
			},
		},
		{
			name: "start and end of multiline text",
			def: func() ([]byte, token.Slice, error) {
				line := bytes.Join([][]byte{var1, helloW, var2}, []byte{' '})
				want := token.Slice{
					{
						Kind:  kind.Var,
						Start: len(varStart),
						End:   len(var1) - len(varEnd),
					},
					{
						Kind:  kind.Text,
						Start: len(var1),
						End:   len(line) - len(var1),
					},
					{
						Kind:  kind.Var,
						Start: len(line) - len(var1) + 2,
						End:   len(line) - 1,
					},
				}
				return line, want, nil
			},
		},
	}
	for _, tc := range testcases {
		tc.Run(t)
	}
}

func TestCombined(t *testing.T) {
	tCmd := []byte(".title ")
	title := []byte("hey prompt")
	// title2 := []byte("hey prompt 2")
	line1 := append(tCmd, title...)
	// line2 := helloW
	// input := joinLines(line1, line2, line2)
	varStart := []byte("<!")
	varName := []byte("var1")
	varName2 := []byte("var2")
	varEnd := []byte(">")
	var1 := append(append(varStart, varName...), varEnd...)
	var2 := append(append(varStart, varName2...), varEnd...)
	testcases := []TestCase{
		{
			name: "title and var",
			def: func() ([]byte, token.Slice, error) {
				line2 := bytes.Join([][]byte{var1, helloW}, []byte{' '})
				line := joinLines(line1, line2)
				want := token.Slice{
					{
						Kind:  kind.Title,
						Start: 7,
						End:   len(line1),
					},
					{
						Kind:  kind.Var,
						Start: len(line1) + 3,
						End:   len(line1) + 3 + len(varName),
					},
					{
						Kind:  kind.Text,
						Start: len(line1) + 3 + len(varName) + 1,
						End:   len(line),
					},
				}
				return line, want, nil
			},
		},
		{
			name: "two titles and var",
			def: func() ([]byte, token.Slice, error) {
				line2 := bytes.Join([][]byte{var1, helloW, var2}, []byte{' '})
				line3 := []byte(".title hey prompt 3")
				line4 := []byte("<!camelVar1> say hello to the user")
				line := joinLines(line1, line2, line3, line4)
				want := token.Slice{
					{
						Kind:  kind.Title,
						Start: 7,
						End:   len(line1),
					},
					{
						Kind:  kind.Var,
						Start: len(line1) + 3,
						End:   len(line1) + 3 + len(varName),
					},
					{
						Kind:  kind.Text,
						Start: len(line1) + 3 + len(varName) + 1,
						End:   len(line1) + 3 + len(varName) + 1 + len(helloW) + 2,
					},
					{
						Kind:  kind.Var,
						Start: len(line1) + 3 + len(varName) + 1 + len(helloW) + 2 + 2,
						End:   len(line1) + 3 + len(varName) + 1 + len(helloW) + 2 + 2 + len(varName2),
					},
					{kind.Title, 53, 65},
					{kind.Var, 68, 77},
					{kind.Text, 78, 100},
				}
				return line, want, nil
			},
		},
	}
	for _, tc := range testcases {
		tc.Run(t)
	}
}

func joinLines(in ...[]byte) []byte {
	return bytes.Join(in, []byte{'\n'})
}

func TestOptionalBlock(t *testing.T) {
	testcases := []TestCase{
		{
			name: "simple optional block",
			def: func() ([]byte, token.Slice, error) {
				start := []byte("<?optional>")
				text := []byte("some optional text")
				end := []byte("</optional>")
				input := bytes.Join([][]byte{start, text, end}, []byte{'\n'})
				want := token.Slice{
					{
						Kind:  kind.OptionalBlock,
						Start: 2,
						End:   10,
					},
					{
						Kind:  kind.Text,
						Start: 11,
						End:   31,
					},
					{
						Kind:  kind.EndTag,
						Start: 33,
						End:   41,
					},
				}
				return input, want, nil
			},
		},
		{
			name: "optional block with variable",
			def: func() ([]byte, token.Slice, error) {
				start := []byte("<?block>")
				text1 := []byte("Hello")
				varStart := []byte("<!name>")
				text2 := []byte(" how are you?")
				end := []byte("</block>")
				input := bytes.Join([][]byte{start, text1, varStart, text2, end}, []byte{' '})
				want := token.Slice{
					{
						Kind:  kind.OptionalBlock,
						Start: 2,
						End:   7,
					},
					{
						Kind:  kind.Text,
						Start: 8,
						End:   15,
					},
					{
						Kind:  kind.Var,
						Start: 17,
						End:   21,
					},
					{
						Kind:  kind.Text,
						Start: 22,
						End:   37,
					},
					{
						Kind:  kind.EndTag,
						Start: 39,
						End:   44,
					},
				}
				return input, want, nil
			},
		},
		{
			name: "nested optional blocks",
			def: func() ([]byte, token.Slice, error) {
				outer := []byte("<?outer>")
				text1 := []byte("start")
				inner := []byte("<?inner>")
				text2 := []byte("inner text")
				innerEnd := []byte("</inner>")
				text3 := []byte("end")
				outerEnd := []byte("</outer>")
				input := bytes.Join([][]byte{outer, text1, inner, text2, innerEnd, text3, outerEnd}, []byte{'\n'})
				want := token.Slice{
					{
						Kind:  kind.OptionalBlock,
						Start: 2,
						End:   7,
					},
					{
						Kind:  kind.Text,
						Start: 8,
						End:   15,
					},
					{
						Kind:  kind.OptionalBlock,
						Start: 17,
						End:   22,
					},
					{
						Kind:  kind.Text,
						Start: 23,
						End:   35,
					},
					{
						Kind:  kind.EndTag,
						Start: 37,
						End:   42,
					},
					{
						Kind:  kind.Text,
						Start: 43,
						End:   48,
					},
					{
						Kind:  kind.EndTag,
						Start: 50,
						End:   55,
					},
				}
				return input, want, nil
			},
		},
	}

	for _, tc := range testcases {
		tc.Run(t)
	}
}
