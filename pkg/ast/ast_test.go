package ast_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/ditto-assistant/agentflow/pkg/assert/require"
	"github.com/ditto-assistant/agentflow/pkg/ast"
	"github.com/ditto-assistant/agentflow/pkg/token"
)

type FileTestCase struct {
	filename string
	def      func() (in []byte, want ast.File, wantErr error)
}

func (tc FileTestCase) Run(t *testing.T) {
	t.Run(tc.filename, func(t *testing.T) {
		in, want, wantErr := tc.def()
		got, err := ast.NewFile(tc.filename, in)
		if wantErr != nil {
			require.EqualErr(t, wantErr, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, want, got)
		}
	})
}

var (
	singlePrompt = []byte("say hi to ditto")
	// multiPrompt  = []byte("say hi to ditto\nsay hi to ditto")
)

func promptWithTitle(title, content string) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, ".title %s\n%s", title, content)
	return buf.Bytes()
}

func TestNewFile(t *testing.T) {
	testcases := []FileTestCase{
		{
			filename: "empty.af",
			def: func() ([]byte, ast.File, error) {
				return []byte(""), ast.File{
					Name: "empty",
				}, nil
			},
		},
		{
			filename: "single_prompt.af",
			def: func() ([]byte, ast.File, error) {
				return singlePrompt, ast.File{
					Name:    "single_prompt",
					Content: singlePrompt,
					Prompts: []ast.Prompt{
						{
							Nodes: token.Slice{
								{Kind: token.KindText, Start: 0, End: len(singlePrompt)},
							},
						},
					},
				}, nil
			},
		},
		{
			filename: "2_prompts.af",
			def: func() ([]byte, ast.File, error) {
				content1 := "content 1"
				prompt1 := promptWithTitle("prompt 1", content1)
				content2 := "content 2"
				prompt2 := promptWithTitle("prompt 2", content2)
				content := joinLines(prompt1, prompt2)
				return content, ast.File{
					Name:    "2_prompts",
					Content: content,
					Prompts: []ast.Prompt{
						{
							Title: token.T{Kind: token.KindTitle, Start: 7, End: 15},
							Nodes: token.Slice{
								{Kind: token.KindText, Start: 16, End: 25},
							},
						},
						{
							Title: token.T{Kind: token.KindTitle, Start: 33, End: 41},
							Nodes: token.Slice{
								{Kind: token.KindText, Start: 42, End: 51},
							},
						},
					},
				}, nil
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