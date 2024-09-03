package ast_test

import (
	"bytes"
	"testing"

	"github.com/ditto-assistant/agentflow/pkg/ast"
	"github.com/ditto-assistant/agentflow/pkg/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
)

func TestAst(t *testing.T) {
	logger.SetupLevel(zerolog.TraceLevel)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ast Suite")
}

var _ = Describe("Ast", func() {
	testcases := []struct {
		name    string
		input   []byte
		want    ast.File
		wantErr error
	}{
		{
			name:  "simple prompt no variables",
			input: []byte("Hello World"),
			want: ast.File{
				Prompts: []ast.Prompt{
					{
						Nodes: []ast.Node{
							{
								Kind:  ast.KindText,
								Bytes: []byte("Hello World")},
						},
					},
				},
			},
		},
		{
			name: "2 prompts with titles",
			input: []byte(`
.title hello world
Hello World

.title goodbye world
Goodbye World`),
			want: ast.File{
				Prompts: []ast.Prompt{
					{
						Title: []byte("hello world"),
						Nodes: []ast.Node{
							{
								Kind:  ast.KindText,
								Bytes: []byte("Hello World"),
							},
						},
					},
					{
						Title: []byte("goodbye world"),
						Nodes: []ast.Node{
							{
								Kind:  ast.KindText,
								Bytes: []byte("Goodbye World"),
							},
						},
					},
				},
			},
		},
		{
			name: "2 prompts with vars",
			input: []byte(`
.title hello name
Hello, <!name>!

.title goodbye name
Goodbye, <!name>!`),
			want: ast.File{
				Prompts: []ast.Prompt{
					{
						Title: []byte("hello name"),
						Nodes: []ast.Node{
							{Kind: ast.KindText, Bytes: []byte("Hello, ")},
							{Kind: ast.KindVar, Bytes: []byte("name")},
							{Kind: ast.KindText, Bytes: []byte("!")},
						},
					},
					{
						Title: []byte("goodbye name"),
						Nodes: []ast.Node{
							{Kind: ast.KindText, Bytes: []byte("Goodbye, ")},
							{Kind: ast.KindVar, Bytes: []byte("name")},
							{Kind: ast.KindText, Bytes: []byte("!")},
						},
					},
				},
			},
		},
	}

	Context("Should create a new ast.File", func() {
		for _, tc := range testcases {
			It(tc.name, func() {
				reader := bytes.NewReader(tc.input)
				a, err := ast.New(reader)
				if tc.wantErr != nil {
					Expect(err).To(Equal(tc.wantErr))
				} else {
					Expect(err).To(BeNil())
				}
				Expect(a).To(Equal(tc.want))
			})
		}
	})
})
