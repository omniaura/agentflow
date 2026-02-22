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
						Kind:  kind.TitleDirective,
						Start: 0,
						End:   6, // ".title"
					},
					{
						Kind:  kind.Whitespace,
						Start: 6,
						End:   7, // " "
					},
					{
						Kind:  kind.TitleText,
						Start: 7,
						End:   len(tCmd) + len(title), // "hey prompt"
					},
					{
						Kind:  kind.Whitespace,
						Start: len(tCmd) + len(title),
						End:   len(tCmd) + len(title) + 1, // newline
					},
					{
						Kind:  kind.Text,
						Start: len(tCmd) + len(title) + 1,
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
						Kind:  kind.TitleDirective,
						Start: 0,
						End:   6, // ".title"
					},
					{
						Kind:  kind.Whitespace,
						Start: 6,
						End:   7, // " "
					},
					{
						Kind:  kind.TitleText,
						Start: len(tCmd),
						End:   len(tCmd) + len(title1),
					},
					{
						Kind:  kind.Whitespace,
						Start: len(tCmd) + len(title1),
						End:   len(tCmd) + len(title1) + 1, // newline
					},
					{
						Kind:  kind.Text,
						Start: len(line1) + 1, // +1 for newline
						End:   len(line1) + 1 + len(helloW) + 1, // +1 for newline
					},
					{
						Kind:  kind.TitleDirective,
						Start: len(line1) + 1 + len(helloW) + 1,
						End:   len(line1) + 1 + len(helloW) + 1 + 6, // ".title"
					},
					{
						Kind:  kind.Whitespace,
						Start: len(line1) + 1 + len(helloW) + 1 + 6,
						End:   len(line1) + 1 + len(helloW) + 1 + 6 + 1, // " "
					},
					{
						Kind:  kind.TitleText,
						Start: len(line1) + 1 + len(helloW) + 1 + len(tCmd), // +1 for newline
						End:   len(line1) + 1 + len(helloW) + 1 + len(tCmd) + len(title2),
					},
					{
						Kind:  kind.Whitespace,
						Start: len(line1) + 1 + len(helloW) + 1 + len(tCmd) + len(title2),
						End:   len(line1) + 1 + len(helloW) + 1 + len(tCmd) + len(title2) + 1, // newline
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
				// <!var1> => OpenBracket, DirectiveVar, VarName, CloseBracket
				want := token.Slice{
					{Kind: kind.OpenBracket, Start: 0, End: 1},
					{Kind: kind.DirectiveVar, Start: 1, End: 2},
					{Kind: kind.VarName, Start: 2, End: 6},
					{Kind: kind.CloseBracket, Start: 6, End: 7},
				}
				return var1, want, nil
			},
		},
		{
			name: "start of text line",
			def: func() ([]byte, token.Slice, error) {
				// "<!var1> hello world"
				line := bytes.Join([][]byte{var1, helloW}, []byte{' '})
				want := token.Slice{
					{Kind: kind.OpenBracket, Start: 0, End: 1},
					{Kind: kind.DirectiveVar, Start: 1, End: 2},
					{Kind: kind.VarName, Start: 2, End: 6},
					{Kind: kind.CloseBracket, Start: 6, End: 7},
					{Kind: kind.Text, Start: len(var1), End: len(line)},
				}
				return line, want, nil
			},
		},
		{
			name: "end of text line",
			def: func() ([]byte, token.Slice, error) {
				// "hello world <!var1>"
				line := bytes.Join([][]byte{helloW, var1}, []byte{' '})
				want := token.Slice{
					{Kind: kind.Text, Start: 0, End: len(helloW) + 1},
					{Kind: kind.OpenBracket, Start: len(helloW) + 1, End: len(helloW) + 2},
					{Kind: kind.DirectiveVar, Start: len(helloW) + 2, End: len(helloW) + 3},
					{Kind: kind.VarName, Start: len(helloW) + 3, End: len(line) - 1},
					{Kind: kind.CloseBracket, Start: len(line) - 1, End: len(line)},
				}
				return line, want, nil
			},
		},
		{
			name: "start of multiline text",
			def: func() ([]byte, token.Slice, error) {
				// "<!var1> hello world"
				line := bytes.Join([][]byte{var1, helloW}, []byte{' '})
				want := token.Slice{
					{Kind: kind.OpenBracket, Start: 0, End: 1},
					{Kind: kind.DirectiveVar, Start: 1, End: 2},
					{Kind: kind.VarName, Start: 2, End: 6},
					{Kind: kind.CloseBracket, Start: 6, End: 7},
					{Kind: kind.Text, Start: len(var1), End: len(line)},
				}
				return line, want, nil
			},
		},
		{
			name: "end of multiline text",
			def: func() ([]byte, token.Slice, error) {
				// "hello world <!var1>"
				line := bytes.Join([][]byte{helloW, var1}, []byte{' '})
				want := token.Slice{
					{Kind: kind.Text, Start: 0, End: len(helloW) + 1},
					{Kind: kind.OpenBracket, Start: len(helloW) + 1, End: len(helloW) + 2},
					{Kind: kind.DirectiveVar, Start: len(helloW) + 2, End: len(helloW) + 3},
					{Kind: kind.VarName, Start: len(helloW) + 3, End: len(line) - 1},
					{Kind: kind.CloseBracket, Start: len(line) - 1, End: len(line)},
				}
				return line, want, nil
			},
		},
		{
			name: "start and end of multiline text",
			def: func() ([]byte, token.Slice, error) {
				// "<!var1> hello world <!var2>"
				line := bytes.Join([][]byte{var1, helloW, var2}, []byte{' '})
				v2Start := len(var1) + 1 + len(helloW) + 1 // after "<!var1> hello world "
				want := token.Slice{
					{Kind: kind.OpenBracket, Start: 0, End: 1},
					{Kind: kind.DirectiveVar, Start: 1, End: 2},
					{Kind: kind.VarName, Start: 2, End: 6},
					{Kind: kind.CloseBracket, Start: 6, End: 7},
					{Kind: kind.Text, Start: len(var1), End: v2Start},
					{Kind: kind.OpenBracket, Start: v2Start, End: v2Start + 1},
					{Kind: kind.DirectiveVar, Start: v2Start + 1, End: v2Start + 2},
					{Kind: kind.VarName, Start: v2Start + 2, End: v2Start + 2 + len(varName2)},
					{Kind: kind.CloseBracket, Start: len(line) - 1, End: len(line)},
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
	line1 := append(tCmd, title...)
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
				// ".title hey prompt\n<!var1> hello world"
				line2 := bytes.Join([][]byte{var1, helloW}, []byte{' '})
				line := joinLines(line1, line2)
				// line1 = ".title hey prompt" (len 17)
				// line2 = "<!var1> hello world" starts at offset 18
				want := token.Slice{
					{Kind: kind.TitleDirective, Start: 0, End: 6},
					{Kind: kind.Whitespace, Start: 6, End: 7},
					{Kind: kind.TitleText, Start: 7, End: len(line1)},
					{Kind: kind.Whitespace, Start: len(line1), End: len(line1) + 1}, // newline
					{Kind: kind.OpenBracket, Start: len(line1) + 1, End: len(line1) + 2},
					{Kind: kind.DirectiveVar, Start: len(line1) + 2, End: len(line1) + 3},
					{Kind: kind.VarName, Start: len(line1) + 3, End: len(line1) + 3 + len(varName)},
					{Kind: kind.CloseBracket, Start: len(line1) + 3 + len(varName), End: len(line1) + 3 + len(varName) + 1},
					{Kind: kind.Text, Start: len(line1) + 3 + len(varName) + 1, End: len(line)},
				}
				return line, want, nil
			},
		},
		{
			name: "two titles and var",
			def: func() ([]byte, token.Slice, error) {
				// ".title hey prompt\n<!var1> hello world <!var2>\n.title hey prompt 3\n<!camelVar1> say hello to the user"
				line2 := bytes.Join([][]byte{var1, helloW, var2}, []byte{' '})
				line3 := []byte(".title hey prompt 3")
				line4 := []byte("<!camelVar1> say hello to the user")
				line := joinLines(line1, line2, line3, line4)
				// Offsets:
				// line1: [0:17] ".title hey prompt"
				// \n at 17
				// line2: [18:44] "<!var1> hello world <!var2>"
				// \n at 44
				// line3: [45:64] ".title hey prompt 3"
				// \n at 64
				// line4: [65:99] "<!camelVar1> say hello to the user"
				l1End := len(line1)           // 17
				l2Start := l1End + 1          // 18
				l2End := l2Start + len(line2) // 44
				l3Start := l2End + 1          // 45
				l3End := l3Start + len(line3) // 64
				l4Start := l3End + 1          // 65

				// var2 starts at: l2Start + len(var1) + 1 + len(helloW) + 1
				v2Start := l2Start + len(var1) + 1 + len(helloW) + 1

				want := token.Slice{
					// .title hey prompt
					{Kind: kind.TitleDirective, Start: 0, End: 6},
					{Kind: kind.Whitespace, Start: 6, End: 7},
					{Kind: kind.TitleText, Start: 7, End: l1End},
					{Kind: kind.Whitespace, Start: l1End, End: l2Start}, // newline
					// <!var1>
					{Kind: kind.OpenBracket, Start: l2Start, End: l2Start + 1},
					{Kind: kind.DirectiveVar, Start: l2Start + 1, End: l2Start + 2},
					{Kind: kind.VarName, Start: l2Start + 2, End: l2Start + 2 + len(varName)},
					{Kind: kind.CloseBracket, Start: l2Start + 2 + len(varName), End: l2Start + 2 + len(varName) + 1},
					// " hello world "
					{Kind: kind.Text, Start: l2Start + len(var1), End: v2Start},
					// <!var2>
					{Kind: kind.OpenBracket, Start: v2Start, End: v2Start + 1},
					{Kind: kind.DirectiveVar, Start: v2Start + 1, End: v2Start + 2},
					{Kind: kind.VarName, Start: v2Start + 2, End: v2Start + 2 + len(varName2)},
					{Kind: kind.CloseBracket, Start: v2Start + 2 + len(varName2), End: l2End},
					// "\n" text between line2 and line3
					{Kind: kind.Text, Start: l2End, End: l2End + 1},
					// .title hey prompt 3
					{Kind: kind.TitleDirective, Start: l3Start, End: l3Start + 6},
					{Kind: kind.Whitespace, Start: l3Start + 6, End: l3Start + 7},
					{Kind: kind.TitleText, Start: l3Start + 7, End: l3End},
					{Kind: kind.Whitespace, Start: l3End, End: l4Start}, // newline
					// <!camelVar1>
					{Kind: kind.OpenBracket, Start: l4Start, End: l4Start + 1},
					{Kind: kind.DirectiveVar, Start: l4Start + 1, End: l4Start + 2},
					{Kind: kind.VarName, Start: l4Start + 2, End: l4Start + 11},
					{Kind: kind.CloseBracket, Start: l4Start + 11, End: l4Start + 12},
					// " say hello to the user"
					{Kind: kind.Text, Start: l4Start + 12, End: len(line)},
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
				// "<?optional>\nsome optional text\n</optional>"
				start := []byte("<?optional>")
				text := []byte("some optional text")
				end := []byte("</optional>")
				input := bytes.Join([][]byte{start, text, end}, []byte{'\n'})
				// <?optional> at [0:11]
				// \n at 11
				// text at [12:30]
				// \n at 30
				// </optional> at [31:42]
				want := token.Slice{
					// <?optional>
					{Kind: kind.OpenBracket, Start: 0, End: 1},
					{Kind: kind.DirectiveCond, Start: 1, End: 2},
					{Kind: kind.VarName, Start: 2, End: 10},
					{Kind: kind.CloseBracket, Start: 10, End: 11},
					// "\nsome optional text\n"
					{Kind: kind.Text, Start: 11, End: 31},
					// </optional>
					{Kind: kind.OpenBracket, Start: 31, End: 32},
					{Kind: kind.DirectiveEnd, Start: 32, End: 33},
					{Kind: kind.VarName, Start: 33, End: 41},
					{Kind: kind.CloseBracket, Start: 41, End: 42},
				}
				return input, want, nil
			},
		},
		{
			name: "optional block with variable",
			def: func() ([]byte, token.Slice, error) {
				// "<?block> Hello <!name>  how are you? </block>"
				start := []byte("<?block>")
				text1 := []byte("Hello")
				varTag := []byte("<!name>")
				text2 := []byte(" how are you?")
				end := []byte("</block>")
				input := bytes.Join([][]byte{start, text1, varTag, text2, end}, []byte{' '})
				// <?block> at [0:8]
				// " Hello " at [8:15]
				// <!name> at [15:22]
				// "  how are you? " at [22:37]
				// </block> at [37:45]
				want := token.Slice{
					// <?block>
					{Kind: kind.OpenBracket, Start: 0, End: 1},
					{Kind: kind.DirectiveCond, Start: 1, End: 2},
					{Kind: kind.VarName, Start: 2, End: 7},
					{Kind: kind.CloseBracket, Start: 7, End: 8},
					// " Hello "
					{Kind: kind.Text, Start: 8, End: 15},
					// <!name>
					{Kind: kind.OpenBracket, Start: 15, End: 16},
					{Kind: kind.DirectiveVar, Start: 16, End: 17},
					{Kind: kind.VarName, Start: 17, End: 21},
					{Kind: kind.CloseBracket, Start: 21, End: 22},
					// "  how are you? "
					{Kind: kind.Text, Start: 22, End: 37},
					// </block>
					{Kind: kind.OpenBracket, Start: 37, End: 38},
					{Kind: kind.DirectiveEnd, Start: 38, End: 39},
					{Kind: kind.VarName, Start: 39, End: 44},
					{Kind: kind.CloseBracket, Start: 44, End: 45},
				}
				return input, want, nil
			},
		},
		{
			name: "nested optional blocks",
			def: func() ([]byte, token.Slice, error) {
				// "<?outer>\nstart\n<?inner>\ninner text\n</inner>\nend\n</outer>"
				outer := []byte("<?outer>")
				text1 := []byte("start")
				inner := []byte("<?inner>")
				text2 := []byte("inner text")
				innerEnd := []byte("</inner>")
				text3 := []byte("end")
				outerEnd := []byte("</outer>")
				input := bytes.Join([][]byte{outer, text1, inner, text2, innerEnd, text3, outerEnd}, []byte{'\n'})
				// <?outer> at [0:8]
				// \nstart\n at [8:15]
				// <?inner> at [15:23]
				// \ninner text\n at [23:35]
				// </inner> at [35:43]
				// \nend\n at [43:48]
				// </outer> at [48:56]
				want := token.Slice{
					// <?outer>
					{Kind: kind.OpenBracket, Start: 0, End: 1},
					{Kind: kind.DirectiveCond, Start: 1, End: 2},
					{Kind: kind.VarName, Start: 2, End: 7},
					{Kind: kind.CloseBracket, Start: 7, End: 8},
					// "\nstart\n"
					{Kind: kind.Text, Start: 8, End: 15},
					// <?inner>
					{Kind: kind.OpenBracket, Start: 15, End: 16},
					{Kind: kind.DirectiveCond, Start: 16, End: 17},
					{Kind: kind.VarName, Start: 17, End: 22},
					{Kind: kind.CloseBracket, Start: 22, End: 23},
					// "\ninner text\n"
					{Kind: kind.Text, Start: 23, End: 35},
					// </inner>
					{Kind: kind.OpenBracket, Start: 35, End: 36},
					{Kind: kind.DirectiveEnd, Start: 36, End: 37},
					{Kind: kind.VarName, Start: 37, End: 42},
					{Kind: kind.CloseBracket, Start: 42, End: 43},
					// "\nend\n"
					{Kind: kind.Text, Start: 43, End: 48},
					// </outer>
					{Kind: kind.OpenBracket, Start: 48, End: 49},
					{Kind: kind.DirectiveEnd, Start: 49, End: 50},
					{Kind: kind.VarName, Start: 50, End: 55},
					{Kind: kind.CloseBracket, Start: 55, End: 56},
				}
				return input, want, nil
			},
		},
	}

	for _, tc := range testcases {
		tc.Run(t)
	}
}
