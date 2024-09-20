package ast

import (
	"fmt"

	"github.com/ditto-assistant/agentflow/pkg/token"
)

type File struct {
	Name    string
	Content []byte
	Prompts []Prompt
}

type Prompt struct {
	// Title is the name of the prompt.
	Title token.T
	// Nodes are the nodes of the prompt.
	Nodes token.Slice
}

func NewFile(name string, content []byte) (f File, err error) {
	tokens, err := token.Tokenize(content)
	if err != nil {
		return
	}
	fmt.Println(tokens.Stringify(content))
	f.Name = name
	f.Content = content
	return
}
