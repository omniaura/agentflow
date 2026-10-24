package gogen

import (
	"bytes"
	"encoding/json"
	"fmt"
	gotoken "go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/omniaura/agentflow/pkg/decl"
	"github.com/peyton-spencer/caseconv/bytcase"
)

// AftoolImport is the runtime package generated tool code imports.
const AftoolImport = "github.com/omniaura/agentflow/runtime/aftool"

// genTools writes the declarations for the file's .tool sections: per tool,
// an Args struct (plus one struct per nested object) and an *aftool.Def
// variable holding the name, description, JSON Schema and argument shape. It
// reports whether the code uses encoding/json.
func genTools(buf *bytes.Buffer, tools []*decl.Tool) (usesJSON bool) {
	for _, t := range tools {
		name := camel(t.Name)
		args := name + "Args"
		var structs bytes.Buffer
		if writeArgsStruct(&structs, args, t.Name, t.Params, true) {
			usesJSON = true
		}
		buf.Write(structs.Bytes())

		fmt.Fprintf(buf, "// %sTool is the %s tool. Its Decode method checks and decodes the\n", name, t.Name)
		fmt.Fprintf(buf, "// arguments of a call into %s.\n", args)
		fmt.Fprintf(buf, "var %sTool = &aftool.Def[%s]{\n", name, args)
		fmt.Fprintf(buf, "Name: %s,\n", strconv.Quote(t.Name))
		fmt.Fprintf(buf, "Description: %s,\n", goString(t.Description))
		fmt.Fprintf(buf, "Schema: []byte(%s),\n", goString(objectSchema(t.Params, "")))
		if t.MaxBytes > 0 {
			fmt.Fprintf(buf, "MaxBytes: %d,\n", t.MaxBytes)
		}
		buf.WriteString("Shape: ")
		writeObjectShape(buf, t.Params)
		buf.WriteString(",\n}\n\n")
		writeEnumConsts(buf, name, t.Name, t.Params)
	}
	return usesJSON
}

// writeEnumConsts writes a constant per enum value, named after the tool,
// the parameter path and the value, so callers can switch on them. An enum
// with a value that makes no Go identifier gets no constants.
func writeEnumConsts(buf *bytes.Buffer, prefix, tool string, params []*decl.Param) {
	for _, p := range params {
		name := prefix + camel(p.Name)
		if p.Type.Kind == decl.Object {
			writeEnumConsts(buf, name, tool, p.Children)
			continue
		}
		if p.Type.Kind != decl.Enum {
			continue
		}
		consts := make([]string, len(p.Type.Enum))
		for i, v := range p.Type.Enum {
			consts[i] = name + camel(v)
			if !gotoken.IsIdentifier(consts[i]) || slices.Contains(consts[:i], consts[i]) {
				consts = nil
				break
			}
		}
		if consts == nil {
			continue
		}
		fmt.Fprintf(buf, "// Values of the %s tool's %s parameter.\nconst (\n", tool, p.Name)
		for i, v := range p.Type.Enum {
			fmt.Fprintf(buf, "%s = %s\n", consts[i], strconv.Quote(v))
		}
		buf.WriteString(")\n\n")
	}
}

// ToolQuery is the name of the sqlc query a tool written in SQL runs:
// lookup_order runs LookupOrderTool. The suffix keeps it apart from a
// route's query of the same name, as sqlc queries share one namespace.
func ToolQuery(t *decl.Tool) string { return camel(t.Name) + "Tool" }

func camel(s string) string {
	return string(bytcase.ToCamel([]byte(strings.ReplaceAll(s, "-", "_"))))
}

// writeArgsStruct writes the struct named name for params, then the structs
// of its nested objects, and reports whether a field is a json.RawMessage.
func writeArgsStruct(buf *bytes.Buffer, name, tool string, params []*decl.Param, root bool) (usesJSON bool) {
	return writeStruct(buf, name, "are", "the arguments of the "+tool+" tool", params, root)
}

// writeStruct writes the struct named name for params, which make up what,
// then the structs of its nested objects. verb joins name and what in the
// root struct's doc comment.
func writeStruct(buf *bytes.Buffer, name, verb, what string, params []*decl.Param, root bool) (usesJSON bool) {
	if root {
		fmt.Fprintf(buf, "// %s %s %s.\n", name, verb, what)
	} else {
		fmt.Fprintf(buf, "// %s is an object in %s.\n", name, what)
	}
	fmt.Fprintf(buf, "type %s struct {\n", name)
	var nested bytes.Buffer
	for _, p := range params {
		if p.Description != "" {
			for _, line := range strings.Split(p.Description, "\n") {
				fmt.Fprintf(buf, "// %s\n", line)
			}
		}
		typ := ""
		switch p.Type.Kind {
		case decl.String, decl.Enum:
			typ = "string"
		case decl.Int:
			typ = "int"
		case decl.Float:
			typ = "float64"
		case decl.Bool:
			typ = "bool"
		case decl.Any:
			typ = "json.RawMessage"
			usesJSON = true
		case decl.Object:
			if len(p.Children) == 0 {
				typ = "map[string]any"
				break
			}
			typ = name + camel(p.Name)
			if writeStruct(&nested, typ, verb, what, p.Children, false) {
				usesJSON = true
			}
			if p.Optional && !p.Type.Array {
				typ = "*" + typ
			}
		}
		if p.Type.Array {
			typ = "[]" + typ
		}
		tag := p.Name
		if p.Optional {
			tag += ",omitempty"
		}
		fmt.Fprintf(buf, "%s %s `json:%q`\n", camel(p.Name), typ, tag)
	}
	buf.WriteString("}\n\n")
	buf.Write(nested.Bytes())
	return usesJSON
}

func writeObjectShape(buf *bytes.Buffer, params []*decl.Param) {
	buf.WriteString("aftool.Shape{Kind: aftool.Object, Fields: []aftool.Field{\n")
	for _, p := range params {
		fmt.Fprintf(buf, "{Name: %s, ", strconv.Quote(p.Name))
		if !p.Optional {
			buf.WriteString("Required: true, ")
		}
		buf.WriteString("Shape: ")
		writeShape(buf, p)
		buf.WriteString("},\n")
	}
	buf.WriteString("}}")
}

func writeShape(buf *bytes.Buffer, p *decl.Param) {
	if p.Type.Array {
		buf.WriteString("aftool.Shape{Kind: aftool.Array, Items: &")
		writeElemShape(buf, p)
		buf.WriteString("}")
		return
	}
	writeElemShape(buf, p)
}

func writeElemShape(buf *bytes.Buffer, p *decl.Param) {
	switch p.Type.Kind {
	case decl.String:
		buf.WriteString("aftool.Shape{Kind: aftool.String}")
	case decl.Enum:
		buf.WriteString("aftool.Shape{Kind: aftool.String, Enum: []string{")
		for i, v := range p.Type.Enum {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(strconv.Quote(v))
		}
		buf.WriteString("}}")
	case decl.Int:
		buf.WriteString("aftool.Shape{Kind: aftool.Integer}")
	case decl.Float:
		buf.WriteString("aftool.Shape{Kind: aftool.Number}")
	case decl.Bool:
		buf.WriteString("aftool.Shape{Kind: aftool.Boolean}")
	case decl.Any:
		buf.WriteString("aftool.Shape{Kind: aftool.Any}")
	case decl.Object:
		if len(p.Children) == 0 {
			buf.WriteString("aftool.Shape{Kind: aftool.Object}")
			return
		}
		writeObjectShape(buf, p.Children)
	}
}

// objectSchema renders the JSON Schema of an object with params as its
// properties, in declaration order. Declared objects reject properties they
// do not declare.
func objectSchema(params []*decl.Param, description string) string {
	var b strings.Builder
	b.WriteString(`{"type":"object"`)
	if description != "" {
		b.WriteString(`,"description":`)
		b.WriteString(jsonString(description))
	}
	b.WriteString(`,"properties":{`)
	var required []string
	for i, p := range params {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(jsonString(p.Name))
		b.WriteByte(':')
		b.WriteString(paramSchema(p))
		if !p.Optional {
			required = append(required, jsonString(p.Name))
		}
	}
	b.WriteString(`}`)
	if len(required) > 0 {
		b.WriteString(`,"required":[`)
		b.WriteString(strings.Join(required, ","))
		b.WriteString(`]`)
	}
	b.WriteString(`,"additionalProperties":false}`)
	return b.String()
}

func paramSchema(p *decl.Param) string {
	if !p.Type.Array {
		return elemSchema(p, p.Description)
	}
	var b strings.Builder
	b.WriteString(`{"type":"array"`)
	if p.Description != "" {
		b.WriteString(`,"description":`)
		b.WriteString(jsonString(p.Description))
	}
	b.WriteString(`,"items":`)
	b.WriteString(elemSchema(p, ""))
	b.WriteString(`}`)
	return b.String()
}

func elemSchema(p *decl.Param, description string) string {
	if p.Type.Kind == decl.Object && len(p.Children) > 0 {
		return objectSchema(p.Children, description)
	}
	var b strings.Builder
	b.WriteByte('{')
	typ := ""
	switch p.Type.Kind {
	case decl.String, decl.Enum:
		typ = "string"
	case decl.Int:
		typ = "integer"
	case decl.Float:
		typ = "number"
	case decl.Bool:
		typ = "boolean"
	case decl.Object:
		typ = "object"
	}
	if typ != "" {
		b.WriteString(`"type":`)
		b.WriteString(jsonString(typ))
	}
	if description != "" {
		if typ != "" {
			b.WriteByte(',')
		}
		b.WriteString(`"description":`)
		b.WriteString(jsonString(description))
	}
	if p.Type.Kind == decl.Enum {
		b.WriteString(`,"enum":[`)
		for i, v := range p.Type.Enum {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(jsonString(v))
		}
		b.WriteByte(']')
	}
	b.WriteByte('}')
	return b.String()
}

func jsonString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

// goString returns s as a Go string literal: a raw string when s can be one,
// so long descriptions stay readable in generated code.
func goString(s string) string {
	if strings.ContainsAny(s, "`\r") || !strconv.CanBackquote(strings.ReplaceAll(s, "\n", "")) {
		return strconv.Quote(s)
	}
	return "`" + s + "`"
}

// genOutputs writes the declarations for the file's .output sections: per
// output, a <Name>Output struct (plus one struct per nested object) and a
// <Name>Schema *aftool.Output with a strict JSON Schema.
func genOutputs(buf *bytes.Buffer, outputs []*decl.Tool) {
	for _, o := range outputs {
		name := camel(o.Name)
		typ := name + "Output"
		writeStruct(buf, typ, "is", "the "+o.Name+" structured output", o.Params, true)
		fmt.Fprintf(buf, "// %sSchema is the %s structured output. Send its OpenAI or\n", name, o.Name)
		fmt.Fprintf(buf, "// Anthropic format with a request, and Decode the model's answer into\n// %s.\n", typ)
		fmt.Fprintf(buf, "var %sSchema = &aftool.Output[%s]{\n", name, typ)
		fmt.Fprintf(buf, "Name: %s,\n", strconv.Quote(o.Name))
		fmt.Fprintf(buf, "Description: %s,\n", goString(o.Description))
		fmt.Fprintf(buf, "Schema: []byte(%s),\n", goString(strictObjectSchema(o.Params, "")))
		if o.MaxBytes > 0 {
			fmt.Fprintf(buf, "MaxBytes: %d,\n", o.MaxBytes)
		}
		buf.WriteString("Shape: ")
		writeObjectShape(buf, o.Params)
		buf.WriteString(",\n")
		fmt.Fprintf(buf, "Fields: %s,\n", goString(fieldsText(o.Params, "")))
		buf.WriteString("}\n\n")
		writeEnumConsts(buf, name, o.Name, o.Params)
	}
}

// strictObjectSchema renders an object schema for strict structured
// outputs: every property is required, and optional ones may be null.
func strictObjectSchema(params []*decl.Param, description string) string {
	var b strings.Builder
	b.WriteString(`{"type":"object"`)
	if description != "" {
		b.WriteString(`,"description":`)
		b.WriteString(jsonString(description))
	}
	b.WriteString(`,"properties":{`)
	var required []string
	for i, p := range params {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(jsonString(p.Name))
		b.WriteByte(':')
		b.WriteString(strictParamSchema(p))
		required = append(required, jsonString(p.Name))
	}
	b.WriteString(`},"required":[`)
	b.WriteString(strings.Join(required, ","))
	b.WriteString(`],"additionalProperties":false}`)
	return b.String()
}

func strictParamSchema(p *decl.Param) string {
	var s string
	switch {
	case p.Type.Array:
		var b strings.Builder
		b.WriteString(`{"type":"array"`)
		if p.Description != "" {
			b.WriteString(`,"description":`)
			b.WriteString(jsonString(p.Description))
		}
		b.WriteString(`,"items":`)
		b.WriteString(strictElemSchema(p, ""))
		b.WriteString(`}`)
		s = b.String()
	default:
		s = strictElemSchema(p, p.Description)
	}
	if !p.Optional {
		return s
	}
	// Strict mode has no optional properties: an optional one is required
	// and may be null.
	return `{"anyOf":[` + s + `,{"type":"null"}]}`
}

func strictElemSchema(p *decl.Param, description string) string {
	if p.Type.Kind == decl.Object {
		return strictObjectSchema(p.Children, description)
	}
	return elemSchema(p, description)
}

// fieldsText describes params one per line, for a prompt:
//
//   - "reason" (one of "damaged", "late"): Why the customer wants a refund.
func fieldsText(params []*decl.Param, prefix string) string {
	var b strings.Builder
	for _, p := range params {
		path := p.Name
		if prefix != "" {
			path = prefix + "." + p.Name
		}
		typ := ""
		switch p.Type.Kind {
		case decl.String:
			typ = "string"
		case decl.Int:
			typ = "integer"
		case decl.Float:
			typ = "number"
		case decl.Bool:
			typ = "true or false"
		case decl.Object:
			typ = "object"
		case decl.Enum:
			quoted := make([]string, len(p.Type.Enum))
			for i, v := range p.Type.Enum {
				quoted[i] = strconv.Quote(v)
			}
			typ = "one of " + strings.Join(quoted, ", ")
		}
		if p.Type.Array {
			typ = "list of " + typ
		}
		if p.Optional {
			typ += ", or null"
		}
		fmt.Fprintf(&b, "- %q (%s)", path, typ)
		if p.Description != "" {
			b.WriteString(": " + p.Description)
		}
		b.WriteString("\n")
		if p.Type.Kind == decl.Object {
			childPrefix := path
			if p.Type.Array {
				childPrefix += "[]"
			}
			b.WriteString(fieldsText(p.Children, childPrefix))
		}
	}
	return b.String()
}
