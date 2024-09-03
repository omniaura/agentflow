package ast

import (
	"bufio"
	"bytes"
	"io"
	"iter"
)

type File struct {
	Prompts []Prompt
}

type Prompt struct {
	// Title is the name of the prompt.
	Title []byte
	// TextNodes is the text of the prompt.
	TextNodes []TextNode
}

type NodeKind int

const (
	NodeKindUnset NodeKind = iota
	KindText
	KindVar
)

type TextNode struct {
	Kind NodeKind
	// NodeTypeText: text content or empty for newline
	// NodeTypeVar: name of variable
	Bytes []byte
}

func New(reader io.Reader) (root File, err error) {
	scanner := bufio.NewScanner(reader)
	var cp Prompt
	for lineNum := 0; scanner.Scan(); lineNum++ {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// rawline check
		if line[0] == '~' {
			l := line[1:]
			// log.Trace().Bytes("line", l).Msg("found rawline")
			// append to previous node if found
			if len(cp.TextNodes) != 0 {
				last := len(cp.TextNodes) - 1
				if cp.TextNodes[last].Kind == KindText {
					cp.TextNodes[last].Bytes = append(cp.TextNodes[last].Bytes, l...)
				}

			} else {
				cp.TextNodes = append(cp.TextNodes, TextNode{
					Kind:  KindText,
					Bytes: l,
				})
			}
			continue
		}

		if title := bytes.TrimPrefix(line, titleBytes); len(title) != len(line) {
			// log.Trace().Bytes("title", title).Msg("found title")
			// It is the next prompt if we find a new title
			if len(cp.Title) != 0 {
				root.Prompts = append(root.Prompts, cp)
				cp = Prompt{}
			}
			cp.Title = title
			continue
		}

		for node := range getTextNodes(line) {
			if len(cp.TextNodes) == 0 {
				cp.TextNodes = append(cp.TextNodes, node)
				continue
			}
			switch node.Kind {
			case KindVar:
				cp.TextNodes = append(cp.TextNodes, node)

			case KindText:
				last := len(cp.TextNodes) - 1
				switch cp.TextNodes[last].Kind {
				case KindText:
					cp.TextNodes[last].Bytes = append(cp.TextNodes[last].Bytes, node.Bytes...)
				case KindVar:
					cp.TextNodes = append(cp.TextNodes, node)
				}

			}
		}
	}
	root.Prompts = append(root.Prompts, cp)
	return
}

func getTextNodes(l []byte) iter.Seq[TextNode] {
	return func(yield func(TextNode) bool) {
		var node TextNode
		for i, b := range l {
			switch node.Kind {
			case KindVar:
				// log.Trace().Str("b", string(b)).Msg("NodeKindVar")
				switch b {
				case '!':
					continue
				case '>':
					if !yield(node) {
						return
					}
					node = TextNode{Kind: KindText}
				default:
					node.Bytes = append(node.Bytes, b)

				}

			case KindText:
				// log.Trace().Str("b", string(b)).Msg("NodeKindText")
				// start var
				if b == '<' &&
					len(l) > i &&
					l[i+1] == '!' {
					if !yield(node) {
						return
					}
					node = TextNode{Kind: KindVar}
					continue

				}
				node.Bytes = append(node.Bytes, b)

			// Could be first var or first text
			case NodeKindUnset:
				// log.Trace().Str("b", string(b)).Msg("NodeKindUnset")
				switch b {
				case '<':
					if len(l) > i && l[i+1] == '!' {
						node.Kind = KindVar
						continue
					}
				}
				node.Kind = KindText
				node.Bytes = append(node.Bytes, b)

			}
		}
		if node.Kind != NodeKindUnset {
			yield(node)
		}
	}
}

var (
	titleBytes = []byte(".title ")
)
