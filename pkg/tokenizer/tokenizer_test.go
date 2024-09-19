package tokenizer_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/ditto-assistant/agentflow/pkg/logger"
	"github.com/ditto-assistant/agentflow/pkg/tokenizer"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	logger.SetupLevel(zerolog.TraceLevel)
	m.Run()
}

type TestCase struct {
	name string
	def  func() (in []byte, want tokenizer.TokenSlice, wantErr error)
}

func (tc TestCase) Run(t *testing.T) {
	t.Run(tc.name, func(t *testing.T) {
		in, want, wantErr := tc.def()
		tokens, err := tokenizer.Tokenize(in)
		if wantErr != nil {
			require.Equal(t, wantErr, err)
		} else {
			require.NoError(t, err)
			if !want.Equal(tokens) {
				t.Errorf("tokens not equal\n\x1b[1;37mWANT:\x1b[0m\n%s\n\x1b[1;37mGOT:\x1b[0m\n%s", stringify(in, want), stringify(in, tokens))
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
			def: func() ([]byte, tokenizer.TokenSlice, error) {
				want := tokenizer.TokenSlice{
					{
						Kind:  tokenizer.KindText,
						Start: 0,
						End:   len(helloW),
					},
				}
				return helloW, want, nil
			},
		},
		{
			name: "multi line",
			def: func() ([]byte, tokenizer.TokenSlice, error) {
				want := tokenizer.TokenSlice{
					{
						Kind:  tokenizer.KindText,
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
			def: func() ([]byte, tokenizer.TokenSlice, error) {
				tCmd := []byte(".title ")
				title := []byte("hey prompt")
				line1 := append(tCmd, title...)
				line2 := helloW
				input := joinLines(line1, line2, line2)
				want := []tokenizer.Token{
					{
						Kind:  tokenizer.KindTitle,
						Start: len(tCmd),
						End:   len(tCmd) + len(title),
					},
					{
						Kind:  tokenizer.KindText,
						Start: len(tCmd) + len(title) + 1, // newline omitted
						End:   len(input),
					},
				}
				return input, want, nil
			},
		},
		{
			name: "two titles",
			def: func() ([]byte, tokenizer.TokenSlice, error) {
				tCmd := []byte(".title ")
				title1 := []byte("hey prompt")
				title2 := []byte("hey prompt 2")
				line1 := append(tCmd, title1...)
				line2 := append(tCmd, title2...)
				input := joinLines(line1, helloW, line2, helloW)
				want := []tokenizer.Token{
					{
						Kind:  tokenizer.KindTitle,
						Start: len(tCmd),
						End:   len(tCmd) + len(title1),
					},
					{
						Kind:  tokenizer.KindText,
						Start: len(line1) + 1, // +1 for newline
						End:   len(line1) + 1 + len(helloW),
					},
					{
						Kind:  tokenizer.KindTitle,
						Start: len(line1) + 1 + len(helloW) + 1 + len(tCmd), // +1 for newline
						End:   len(line1) + 1 + len(helloW) + 1 + len(tCmd) + len(title2),
					},
					{
						Kind:  tokenizer.KindText,
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
	varEnd := []byte(">")
	var1 := append(append(varStart, varName...), varEnd...)
	testcases := []TestCase{
		{
			name: "one var",
			def: func() ([]byte, tokenizer.TokenSlice, error) {
				want := tokenizer.TokenSlice{
					{
						Kind:  tokenizer.KindVar,
						Start: len(varStart),
						End:   len(var1) - len(varEnd),
					},
				}
				return var1, want, nil
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

func stringify(in []byte, tokens []tokenizer.Token) string {
	var buf strings.Builder
	for i, tok := range tokens {
		buf.WriteString(tok.Kind.String())
		buf.WriteString(": ")
		if tok.Start < 0 || tok.End > len(in) || tok.Start > tok.End {
			buf.WriteString("INVALID BOUNDS [")
			buf.WriteString(strconv.Itoa(tok.Start))
			buf.WriteString(":")
			buf.WriteString(strconv.Itoa(tok.End))
			buf.WriteString("]")
		} else {
			buf.Write(in[tok.Start:tok.End])
		}
		if i != len(tokens)-1 {
			buf.WriteRune('\n')
		}
	}
	return buf.String()
}
