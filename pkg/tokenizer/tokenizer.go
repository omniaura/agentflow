package tokenizer

import "bytes"

type TokenSlice []Token

func (t TokenSlice) Equal(o TokenSlice) bool {
	if len(t) != len(o) {
		return false
	}
	for i, tok := range t {
		if tok != o[i] {
			return false
		}
	}
	return true
}

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
	KindRawBlock
)

func (k Kind) String() string {
	switch k {
	case KindTitle:
		return "title"
	case KindText:
		return "text"
	case KindVar:
		return "var"
	case KindUnset:
		return "unset"
	}
	return "unknown"
}

var (
	cmdTitle = []byte(".title")
)

func Tokenize(input []byte) (TokenSlice, error) {
	var tokens []Token
	var ct Token
	startLine := true
	cmdStart := -1
	cmdEnd := cmdStart
	for i, b := range input {
		if startLine {
			startLine = false
			switch b {
			case '~':
			case '.':
				if ct.Kind != KindUnset {
					ct.End = i - 1
					tokens = append(tokens, ct)
				}
				cmdStart = i
			case '<':
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
					ct.Start = i + 1
				}
			}
		}

		if ct.Kind == KindVar && b == '>' {
			ct.End = i
			tokens = append(tokens, ct)
			ct = Token{}
		}
		if b == '<' && len(input) > i && input[i+1] == '!' {
			if ct.Kind != KindUnset {
				ct.End = i - 1
				tokens = append(tokens, ct)
			}
			ct.Kind = KindVar
			ct.Start = i + 2

		}

		if b == '\n' {
			startLine = true
			switch ct.Kind {
			case KindTitle:
				ct.End = i
				tokens = append(tokens, ct)
				ct = Token{}
			}
		}
		if i == len(input)-1 && ct.Kind != KindUnset {
			ct.End = i + 1
			tokens = append(tokens, ct)
		}
	}
	return tokens, nil
}
