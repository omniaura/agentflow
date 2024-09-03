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
	// Nodes are the nodes of the prompt.
	Nodes []Node
}

type NodeKind int

const (
	NodeKindUnset NodeKind = iota
	KindText
	KindVar
)

type Node struct {
	Kind NodeKind
	// KindText: text content or empty for newline
	// KindVar: name of variable
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
			if len(cp.Nodes) != 0 { // append to previous node if found
				last := len(cp.Nodes) - 1
				if cp.Nodes[last].Kind == KindText {
					cp.Nodes[last].Bytes = append(cp.Nodes[last].Bytes, l...)
				}

			} else {
				cp.Nodes = append(cp.Nodes, Node{
					Kind:  KindText,
					Bytes: l,
				})
			}
			continue
		}

		if bytes.HasPrefix(line, titleBytes) {
			// log.Trace().Bytes("title", title).Msg("found title")
			if len(cp.Title) != 0 { // It is the next prompt if we find a new title
				root.Prompts = append(root.Prompts, cp)
				cp = Prompt{}
			}
			cp.Title = line[len(titleBytes):]
			continue
		}

		for node := range getTextNodes(line) {
			if len(cp.Nodes) == 0 {
				cp.Nodes = append(cp.Nodes, node)
				continue
			}
			switch node.Kind {
			case KindVar:
				cp.Nodes = append(cp.Nodes, node)

			case KindText:
				last := len(cp.Nodes) - 1
				switch cp.Nodes[last].Kind {
				case KindText:
					cp.Nodes[last].Bytes = append(cp.Nodes[last].Bytes, node.Bytes...)
				case KindVar:
					cp.Nodes = append(cp.Nodes, node)
				}

			}
		}
	}
	root.Prompts = append(root.Prompts, cp)
	return
}

func getTextNodes(l []byte) iter.Seq[Node] {
	return func(yield func(Node) bool) {
		var node Node
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
					node = Node{Kind: KindText}
				default:
					node.Bytes = append(node.Bytes, b)

				}

			case KindText:
				// log.Trace().Str("b", string(b)).Msg("NodeKindText")
				if b == '<' &&
					len(l) > i &&
					l[i+1] == '!' { // start var
					if !yield(node) {
						return
					}
					node = Node{Kind: KindVar}
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
