// Package aftool is the runtime for tools declared with .tool in .af files.
//
// af gen turns each .tool section into a [Def]: the name, description and JSON
// Schema a model is shown, plus the shape the model's arguments are checked
// against before they are decoded into the tool's typed Args struct. The
// package has no dependencies, so it is safe to import from generated code and
// to eject into a repository with af eject.
package aftool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// DefaultMaxBytes caps the arguments a tool accepts when its .tool section
// sets no .maxbytes.
const DefaultMaxBytes = 64 << 10

// Kind is the JSON type of a field.
type Kind uint8

const (
	String Kind = iota + 1
	Integer
	Number
	Boolean
	Array
	Object
	// Any is a value of any JSON type, such as a free-form object.
	Any
)

func (k Kind) String() string {
	switch k {
	case String:
		return "string"
	case Integer:
		return "integer"
	case Number:
		return "number"
	case Boolean:
		return "boolean"
	case Array:
		return "array"
	case Object:
		return "object"
	case Any:
		return "any"
	}
	return fmt.Sprintf("Kind(%d)", k)
}

// Field is one declared property of an object.
type Field struct {
	Name     string
	Required bool
	Shape    Shape
}

// Shape describes a value the arguments must match.
type Shape struct {
	Kind Kind
	// Enum lists the allowed values of a String.
	Enum []string
	// Items is the element shape of an Array.
	Items *Shape
	// Fields are the properties of an Object. An Object with no Fields
	// accepts any properties.
	Fields []Field
}

// Def is the definition of one tool whose arguments decode into Args.
type Def[Args any] struct {
	Name        string
	Description string
	// Schema is the JSON Schema of the arguments, with properties in
	// declaration order.
	Schema   json.RawMessage
	MaxBytes int
	Shape    Shape
}

// ArgsError reports arguments that do not match a tool's declaration. Its
// message is written for the model, so it can be returned as the tool result
// and the model can correct its call.
type ArgsError struct {
	Tool string
	// Path is the dotted path of the offending value; empty for the
	// arguments as a whole.
	Path string
	Msg  string
}

func (e *ArgsError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("%s: invalid arguments: %s", e.Tool, e.Msg)
	}
	return fmt.Sprintf("%s: invalid arguments: %s: %s", e.Tool, e.Path, e.Msg)
}

// Decode checks arguments, the JSON a model sent for a call to the tool,
// against the declaration and decodes them. It rejects arguments over
// MaxBytes, trailing data, unknown or missing properties, null required
// values, values of the wrong type and values outside an enum. Any error is
// an *ArgsError.
func (d *Def[Args]) Decode(arguments string) (Args, error) {
	var args Args
	limit := d.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	if len(arguments) > limit {
		return args, d.fail("", fmt.Sprintf("arguments are %d bytes, the limit is %d", len(arguments), limit))
	}
	// An empty call is an empty object: some models omit "{}" for tools
	// with no required arguments.
	if strings.TrimSpace(arguments) == "" {
		arguments = "{}"
	}
	dec := json.NewDecoder(strings.NewReader(arguments))
	dec.UseNumber()
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return args, d.fail("", "not valid JSON: "+err.Error())
	}
	if _, err := dec.Token(); err != io.EOF {
		return args, d.fail("", "unexpected data after the JSON object")
	}
	if err := d.check(raw, d.Shape, ""); err != nil {
		return args, err
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, d.fail("", err.Error())
	}
	return args, nil
}

// OpenAI returns the tool in the OpenAI Chat Completions tools format:
// {"type":"function","function":{"name":…,"description":…,"parameters":…}}.
func (d *Def[Args]) OpenAI() json.RawMessage {
	var b bytes.Buffer
	b.WriteString(`{"type":"function","function":{"name":`)
	writeJSONString(&b, d.Name)
	b.WriteString(`,"description":`)
	writeJSONString(&b, d.Description)
	b.WriteString(`,"parameters":`)
	b.Write(d.Schema)
	b.WriteString(`}}`)
	return b.Bytes()
}

// Anthropic returns the tool in the Anthropic Messages tools format:
// {"name":…,"description":…,"input_schema":…}.
func (d *Def[Args]) Anthropic() json.RawMessage {
	var b bytes.Buffer
	b.WriteString(`{"name":`)
	writeJSONString(&b, d.Name)
	b.WriteString(`,"description":`)
	writeJSONString(&b, d.Description)
	b.WriteString(`,"input_schema":`)
	b.Write(d.Schema)
	b.WriteString(`}`)
	return b.Bytes()
}

func writeJSONString(b *bytes.Buffer, s string) {
	enc := json.NewEncoder(b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	b.Truncate(b.Len() - 1) // Encode appends a newline.
}

func (d *Def[Args]) fail(path, msg string) error {
	return &ArgsError{Tool: d.Name, Path: path, Msg: msg}
}

func (d *Def[Args]) check(raw json.RawMessage, s Shape, path string) error {
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		// null is the zero value; required properties reject it before
		// reaching here.
		return nil
	}
	switch s.Kind {
	case Any:
		return nil
	case String:
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return d.fail(path, "expected a string, got "+jsonType(raw))
		}
		if len(s.Enum) > 0 && !slices.Contains(s.Enum, v) {
			return d.fail(path, fmt.Sprintf("%q is not one of %s", v, strings.Join(s.Enum, ", ")))
		}
	case Integer:
		var n json.Number
		if !isNumber(raw) || json.Unmarshal(raw, &n) != nil {
			return d.fail(path, "expected an integer, got "+jsonType(raw))
		}
		if _, err := n.Int64(); err != nil {
			return d.fail(path, "expected an integer, got "+n.String())
		}
	case Number:
		var n json.Number
		if !isNumber(raw) || json.Unmarshal(raw, &n) != nil {
			return d.fail(path, "expected a number, got "+jsonType(raw))
		}
	case Boolean:
		var v bool
		if json.Unmarshal(raw, &v) != nil {
			return d.fail(path, "expected a boolean, got "+jsonType(raw))
		}
	case Array:
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return d.fail(path, "expected an array, got "+jsonType(raw))
		}
		if s.Items == nil {
			return nil
		}
		for i, item := range items {
			if err := d.check(item, *s.Items, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case Object:
		var props map[string]json.RawMessage
		if raw[0] != '{' || json.Unmarshal(raw, &props) != nil {
			return d.fail(path, "expected an object, got "+jsonType(raw))
		}
		if len(s.Fields) == 0 {
			return nil
		}
		for name := range props {
			if !slices.ContainsFunc(s.Fields, func(f Field) bool { return f.Name == name }) {
				return d.fail(join(path, name), "unknown property")
			}
		}
		for _, f := range s.Fields {
			v, ok := props[f.Name]
			if f.Required && (!ok || bytes.Equal(bytes.TrimSpace(v), []byte("null"))) {
				return d.fail(join(path, f.Name), "required")
			}
			if !ok {
				continue
			}
			if err := d.check(v, f.Shape, join(path, f.Name)); err != nil {
				return err
			}
		}
	default:
		return errors.New("aftool: unknown kind " + s.Kind.String())
	}
	return nil
}

// isNumber reports whether raw is a JSON number. json.Number alone would
// also accept a string such as "1".
func isNumber(raw json.RawMessage) bool {
	return raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9')
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func jsonType(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "nothing"
	}
	switch raw[0] {
	case '"':
		return "a string"
	case '{':
		return "an object"
	case '[':
		return "an array"
	case 't', 'f':
		return "a boolean"
	case 'n':
		return "null"
	}
	return "a number"
}
