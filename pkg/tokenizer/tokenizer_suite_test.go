package tokenizer_test

import (
	"testing"

	"github.com/ditto-assistant/agentflow/pkg/tokenizer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTokenizer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tokenizer Suite")
}

var _ = Describe("tokenizer", func() {
	type TestCase struct {
		name    string
		input   []byte
		want    []tokenizer.Token
		wantErr error
	}
	helloW := []byte("hello world")
	testcases := []TestCase{
		{
			name:  "one line",
			input: helloW,
			want: []tokenizer.Token{
				{
					Kind:  tokenizer.KindText,
					Start: 0,
					End:   len(helloW) - 1,
				},
			},
		},
	}
	for _, tc := range testcases {
		It(tc.name, func() {
			tokens, err := tokenizer.Tokenize(tc.input)
			if tc.wantErr != nil {
				Expect(err).To(Equal(tc.wantErr))
			} else {
				Expect(err).To(BeNil())
				Expect(tokens).To(Equal(tc.want))
			}
		})
	}
})
