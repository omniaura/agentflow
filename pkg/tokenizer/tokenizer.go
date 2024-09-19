package tokenizer

import "bytes"

type Token struct {
	Start int
	End   int
	Kind  Kind
}
type Kind int

const (
	KindUnset = iota
	KindTitle
	KindText
	KindVar
)

var (
	cmdTitle = []byte(".title")
)

func Tokenize(input []byte) ([]Token, error) {
	var tokens []Token
	var ct Token
	startLine := true
	cmdStart := -1
	cmdEnd := cmdStart
	for i, b := range input {
		if startLine {
			switch b {
			case '<':
			}
			switch b {
			case '.':
				if ct.Kind != KindUnset {
					ct.End = i - 1
					tokens = append(tokens, ct)
				}
				cmdStart = i
			default:
				if ct.Kind == KindUnset {
					ct.Kind = KindText
					ct.Start = i
				}
			}
		}
		if cmdStart != -1 {
			switch b {
			case ' ':
				cmdEnd = i
				switch {
				case bytes.Equal(input[cmdStart:cmdEnd], cmdTitle):
					ct.Kind = KindTitle
					ct.Start = i
				}
			}
		}

		if b == '\n' {
			startLine = false
			switch ct.Kind {
			case KindTitle:
				ct.End = i
				tokens = append(tokens, ct)
				ct = Token{}
			}
		}
	}
	return tokens, nil
}
