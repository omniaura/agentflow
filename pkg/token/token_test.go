package token_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ditto-assistant/agentflow/pkg/assert/require"
	"github.com/ditto-assistant/agentflow/pkg/logger"
	"github.com/ditto-assistant/agentflow/pkg/token"
	"github.com/rs/zerolog"
)

func TestMain(m *testing.M) {
	logger.SetupLevel(zerolog.TraceLevel)
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
						Kind:  token.KindText,
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
						Kind:  token.KindText,
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
						Kind:  token.KindTitle,
						Start: len(tCmd),
						End:   len(tCmd) + len(title),
					},
					{
						Kind:  token.KindText,
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
						Kind:  token.KindTitle,
						Start: len(tCmd),
						End:   len(tCmd) + len(title1),
					},
					{
						Kind:  token.KindText,
						Start: len(line1) + 1, // +1 for newline
						End:   len(line1) + 1 + len(helloW),
					},
					{
						Kind:  token.KindTitle,
						Start: len(line1) + 1 + len(helloW) + 1 + len(tCmd), // +1 for newline
						End:   len(line1) + 1 + len(helloW) + 1 + len(tCmd) + len(title2),
					},
					{
						Kind:  token.KindText,
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
						Kind:  token.KindVar,
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
						Kind:  token.KindVar,
						Start: len(varStart),
						End:   len(var1) - len(varEnd),
					},
					{
						Kind:  token.KindText,
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
						Kind:  token.KindText,
						Start: 0,
						End:   len(helloW) + 1,
					},
					{
						Kind:  token.KindVar,
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
						Kind:  token.KindVar,
						Start: len(varStart),
						End:   len(var1) - len(varEnd),
					},
					{
						Kind:  token.KindText,
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
						Kind:  token.KindText,
						Start: 0,
						End:   len(helloW) + 1,
					},
					{
						Kind:  token.KindVar,
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
						Kind:  token.KindVar,
						Start: len(varStart),
						End:   len(var1) - len(varEnd),
					},
					{
						Kind:  token.KindText,
						Start: len(var1),
						End:   len(line) - len(var1),
					},
					{
						Kind:  token.KindVar,
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
						Kind:  token.KindTitle,
						Start: 7,
						End:   len(line1),
					},
					{
						Kind:  token.KindVar,
						Start: len(line1) + 3,
						End:   len(line1) + 3 + len(varName),
					},
					{
						Kind:  token.KindText,
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
						Kind:  token.KindTitle,
						Start: 7,
						End:   len(line1),
					},
					{
						Kind:  token.KindVar,
						Start: len(line1) + 3,
						End:   len(line1) + 3 + len(varName),
					},
					{
						Kind:  token.KindText,
						Start: len(line1) + 3 + len(varName) + 1,
						End:   len(line1) + 3 + len(varName) + 1 + len(helloW) + 2,
					},
					{
						Kind:  token.KindVar,
						Start: len(line1) + 3 + len(varName) + 1 + len(helloW) + 2 + 2,
						End:   len(line1) + 3 + len(varName) + 1 + len(helloW) + 2 + 2 + len(varName2),
					},
					{token.KindTitle, 53, 65},
					{token.KindVar, 68, 77},
					{token.KindText, 78, 100},
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
