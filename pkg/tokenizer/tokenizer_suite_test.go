package tokenizer_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ditto-assistant/agentflow/pkg/logger"
	"github.com/ditto-assistant/agentflow/pkg/tokenizer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
)

func TestTokenizer(t *testing.T) {
	logger.SetupLevel(zerolog.TraceLevel)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tokenizer Suite")
}

var _ = Describe("tokenizer", func() {
	type TestCase struct {
		name string
		def  func() (in []byte, want []tokenizer.Token, wantErr error)
	}
	helloW := []byte("hello world")
	helloMultiline := bytes.Join([][]byte{helloW, helloW, helloW}, []byte{'\n'})

	testcases := []TestCase{
		{
			name: "one line",
			def: func() ([]byte, []tokenizer.Token, error) {
				want := []tokenizer.Token{
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
			def: func() ([]byte, []tokenizer.Token, error) {
				want := []tokenizer.Token{
					{
						Kind:  tokenizer.KindText,
						Start: 0,
						End:   len(helloMultiline),
					},
				}
				return helloMultiline, want, nil
			},
		},
		{
			name: "one title",
			def: func() ([]byte, []tokenizer.Token, error) {
				tCmd := []byte(".title ")
				title := []byte("hey prompt")
				line1 := append(tCmd, title...)
				line2 := helloW
				input := joinLines(line1, line2)
				want := []tokenizer.Token{
					{
						Kind:  tokenizer.KindTitle,
						Start: len(tCmd),
						End:   len(tCmd) + len(title),
					},
				}
				return input, want, nil
			},
		},
	}
	for _, tc := range testcases {
		It(tc.name, func() {
			in, want, wantErr := tc.def()
			tokens, err := tokenizer.Tokenize(in)
			GinkgoLogr.Info("result", "input", string(in), "tokens", stringify(in, tokens))
			if wantErr != nil {
				Expect(err).To(Equal(wantErr))
			} else {
				Expect(err).To(BeNil())
				Expect(tokens).To(Equal(want))
			}
		})
	}
})

func joinLines(in ...[]byte) []byte {
	return bytes.Join(in, []byte{'\n'})
}

func stringify(in []byte, tokens []tokenizer.Token) string {
	var buf strings.Builder
	for i, tok := range tokens {
		buf.WriteString(tok.Kind.String())
		buf.WriteString(": ")
		buf.Write(in[tok.Start:tok.End])
		if i != len(tokens)-1 {
			buf.WriteRune('\n')
		}
	}
	return buf.String()
}
