package gogen

import (
	"bytes"
	"fmt"
	goast "go/ast"
	"go/format"
	"go/parser"
	gotoken "go/token"
	"io"
	"strconv"
	"strings"

	"github.com/omniaura/agentflow/cfg"
	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/gen"
	"github.com/omniaura/agentflow/pkg/gonames"
	"github.com/omniaura/agentflow/pkg/token/coarse"
	"github.com/peyton-spencer/caseconv"
	"github.com/peyton-spencer/caseconv/bytcase"
)

func hasLoops(f ast.File) bool {
	for _, p := range f.Prompts {
		for _, n := range p.Nodes {
			if n.Kind == coarse.LoopBlock {
				return true
			}
		}
	}
	return false
}

type loopScope struct {
	close, alias, local, typ string
	otherwise                bool
}

// genLoopFile emits ordinary Go range loops. Values are data, never template source.
func genLoopFile(w io.Writer, f ast.File, dir string) error {
	var declarations, methods bytes.Buffer
	usesFmt := false
	usesReflect := false
	for pi, p := range f.Prompts {
		name := string(bytcase.ToCamel([]byte(f.Name)))
		if p.Title.Kind != coarse.Unset {
			name = string(bytcase.ToCamel(p.Title.Get(f.Content)))
		} else if len(f.Prompts) > 1 {
			return gen.ErrMissingTitle.F("index: %d", pi)
		}
		fields := map[string]string{}
		aliases := map[string]bool{}
		var fieldOrder []string
		var scopes []loopScope
		types := map[string]string{}
		type pendingCondition struct{ marker, path, expr, fallback string }
		var pending []pendingCondition
		var body bytes.Buffer
		body.WriteString("var b strings.Builder\n")
		addField := func(path, typ string) error {
			if prev, ok := fields[path]; ok {
				if prev != typ {
					return fmt.Errorf("conflicting types for %s: %s and %s", path, prev, typ)
				}
				return nil
			}
			fields[path] = typ
			fieldOrder = append(fieldOrder, path)
			return nil
		}
		resolve := func(path, typ string) (string, error) {
			parts := strings.Split(path, ".")
			if !validValueType(typ) {
				return "", fmt.Errorf("invalid Go value type %q", typ)
			}
			for _, part := range parts {
				if !gotoken.IsIdentifier(part) || part == "_" {
					return "", fmt.Errorf("invalid variable path %q", path)
				}
			}
			for i := len(scopes) - 1; i >= 0; i-- {
				s := scopes[i]
				if s.alias == parts[0] {
					out := s.local
					for _, part := range parts[1:] {
						out += "." + string(bytcase.ToCamel([]byte(part)))
					}
					return out, nil
				}
			}
			if aliases[parts[0]] {
				return "", fmt.Errorf("loop alias %q is out of scope", parts[0])
			}
			if err := addField(path, typ); err != nil {
				return "", err
			}
			return varFieldAccess(bytes.Split([]byte(path), []byte("."))), nil
		}
		for ni, n := range p.Nodes {
			fail := func(err error) error { return &PosError{File: f.Name, Prompt: name, Offset: n.Start, Err: err} }
			switch n.Kind {
			case coarse.LoopBlock:
				parts := strings.Fields(string(n.Get(f.Content)))
				if len(parts) != 4 || parts[2] != "as" || !gotoken.IsIdentifier(parts[3]) || parts[3] == "_" {
					return fail(fmt.Errorf("expected <*path []Type as alias>"))
				}
				typ := parts[1]
				if !validSliceType(typ) {
					return fail(fmt.Errorf("loop requires a Go slice type, got %q", typ))
				}
				for _, s := range scopes {
					if s.alias == parts[3] {
						return fail(fmt.Errorf("loop alias %q shadows an active alias", parts[3]))
					}
				}
				expr, err := resolve(parts[0], typ)
				if err != nil {
					return fail(err)
				}
				local := fmt.Sprintf("item%d", ni)
				fmt.Fprintf(&body, "for _, %s := range %s {\n_ = %s\n", local, expr, local)
				aliases[parts[3]] = true
				scopes = append(scopes, loopScope{close: parts[0], alias: parts[3], local: local, typ: strings.TrimPrefix(typ, "[]")})
			case coarse.Var, coarse.OptionalBlock:
				vi := n.GetVar(f.Content, types)
				path := string(bytes.Join(vi.Path, []byte(".")))
				typ := vi.Type
				for i := len(scopes) - 1; i >= 0; i-- {
					if scopes[i].alias == path {
						typ = scopes[i].typ
						break
					}
				}
				expr, err := resolve(path, typ)
				if err != nil {
					return fail(err)
				}
				if n.Kind == coarse.Var {
					if typ == "string" {
						fmt.Fprintf(&body, "b.WriteString(%s)\n", expr)
					} else {
						usesFmt = true
						fmt.Fprintf(&body, "b.WriteString(fmt.Sprint(%s))\n", expr)
					}
				} else {
					condition := expr + " != \"\""
					if vi.Operator != "" {
						op, err := convertWordOperatorToGo(vi.Operator)
						if err != nil {
							return fail(err)
						}
						condition = expr + " " + op + " " + vi.Operand
					} else if typ == "bool" {
						condition = expr
					} else if strings.HasPrefix(typ, "[]") {
						condition = "len(" + expr + ") != 0"
					} else if typ == "int" || strings.HasPrefix(typ, "float") {
						condition = expr + " != 0"
					} else if typ != "string" {
						usesReflect = true
						condition = "!reflect.ValueOf(&" + expr + ").Elem().IsZero()"
					}
					if vi.Operator == "" && strings.HasPrefix(expr, "input.") {
						marker := fmt.Sprintf("__af_condition_%d__", ni)
						pending = append(pending, pendingCondition{marker, path, expr, condition})
						condition = marker
					}
					fmt.Fprintf(&body, "if %s {\n", condition)
					scopes = append(scopes, loopScope{close: path})
				}
			case coarse.ElseBlock:
				if len(scopes) == 0 || scopes[len(scopes)-1].alias != "" || scopes[len(scopes)-1].otherwise {
					return fail(fmt.Errorf("else requires an unmatched conditional"))
				}
				scopes[len(scopes)-1].otherwise = true
				body.WriteString("} else {\n")
			case coarse.EndTag:
				close := strings.TrimSpace(string(n.Get(f.Content)))
				if len(scopes) == 0 || scopes[len(scopes)-1].close != close {
					return fail(fmt.Errorf("unmatched closing tag %q", close))
				}
				scopes = scopes[:len(scopes)-1]
				body.WriteString("}\n")
			default:
				fmt.Fprintf(&body, "b.WriteString(%s)\n", strconv.Quote(string(n.Get(f.Content))))
			}
		}
		if len(scopes) > 0 {
			return fmt.Errorf("%s prompt %s: unclosed block %q", f.Name, name, scopes[len(scopes)-1].close)
		}
		// Reuse implicit-object inference for root inputs, excluding lexical aliases.
		var source strings.Builder
		for _, path := range fieldOrder {
			fmt.Fprintf(&source, "<!%s %s>", path, fields[path])
		}
		sf, err := ast.NewFile("inputs.af", []byte(source.String()))
		if err != nil {
			return err
		}
		var inputs ast.InputStruct
		if len(sf.Prompts) > 0 {
			inputs, err = sf.Prompts[0].GetInputs(sf.Content, caseconv.CaseCamel)
			if err != nil {
				return err
			}
		}
		// GetInputs retains declared types; the loop writer preserves arbitrary Go slices.
		writeLoopStruct(&declarations, name, inputs.TopLevel)
		renderedBody := body.String()
		for _, c := range pending {
			condition := c.fallback
			if isStructPath(bytes.Split([]byte(c.path), []byte(".")), inputs) {
				usesReflect = true
				condition = "!reflect.ValueOf(&" + c.expr + ").Elem().IsZero()"
			}
			renderedBody = strings.ReplaceAll(renderedBody, "if "+c.marker+" {\n", "if "+condition+" {\n")
		}
		fmt.Fprintf(&methods, "func (input *%s) String() string {\n%sreturn b.String()\n}\n", name, renderedBody)
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "// Code generated by agentflow v%s; DO NOT EDIT.\npackage %s\nimport (\"strings\"", cfg.Version, gonames.CleanPackageName(dir))
	if usesFmt {
		out.WriteString(";\"fmt\"")
	}
	if usesReflect {
		out.WriteString(";\"reflect\"")
	}
	out.WriteString(")\n")
	out.Write(declarations.Bytes())
	out.Write(methods.Bytes())
	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return fmt.Errorf("format loop template: %w", err)
	}
	_, err = w.Write(formatted)
	return err
}

func validSliceType(s string) bool {
	e, err := parser.ParseExpr(s)
	if err != nil {
		return false
	}
	a, ok := e.(*goast.ArrayType)
	return ok && a.Len == nil && validElementType(a.Elt)
}
func validElementType(e goast.Expr) bool {
	switch t := e.(type) {
	case *goast.Ident:
		return t.Name != "_"
	case *goast.StarExpr:
		return validElementType(t.X)
	case *goast.ArrayType:
		return t.Len == nil && validElementType(t.Elt)
	default:
		return false
	}
}
func writeLoopStruct(b *bytes.Buffer, name string, nodes []ast.InputNode) {
	fmt.Fprintf(b, "type %s struct {\n", name)
	writeLoopFields(b, nodes)
	b.WriteString("}\n")
}
func writeLoopFields(b *bytes.Buffer, nodes []ast.InputNode) {
	for _, n := range nodes {
		fmt.Fprintf(b, "%s ", n.Name)
		if len(n.Subnodes) > 0 {
			b.WriteString("struct {\n")
			writeLoopFields(b, n.Subnodes)
			b.WriteString("}\n")
		} else {
			typ := n.Type
			if typ == "" {
				typ = "string"
			}
			fmt.Fprintln(b, typ)
		}
	}
}

func validValueType(s string) bool {
	e, err := parser.ParseExpr(s)
	return err == nil && validElementType(e)
}

// PosError reports a generation failure at a byte offset in a prompt's .af source.
type PosError struct {
	File   string
	Prompt string
	Offset int
	Err    error
}

func (e *PosError) Error() string {
	return fmt.Sprintf("%s prompt %s byte %d: %v", e.File, e.Prompt, e.Offset, e.Err)
}

func (e *PosError) Unwrap() error { return e.Err }
