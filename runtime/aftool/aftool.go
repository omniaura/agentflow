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
	// Tool is the tool's name, or the output's.
	Tool string
	// What is what failed to decode: "arguments" (when empty) or
	// "output".
	What string
	// Path is the dotted path of the offending value; empty for the
	// arguments as a whole.
	Path string
	Msg  string
}

func (e *ArgsError) Error() string {
	what := e.What
	if what == "" {
		what = "arguments"
	}
	if e.Path == "" {
		return fmt.Sprintf("%s: invalid %s: %s", e.Tool, what, e.Msg)
	}
	return fmt.Sprintf("%s: invalid %s: %s: %s", e.Tool, what, e.Path, e.Msg)
}

// Decode checks arguments, the JSON a model sent for a call to the tool,
// against the declaration and decodes them. It rejects arguments over
// MaxBytes, trailing data, unknown or missing properties, null required
// values, values of the wrong type and values outside an enum. Any error is
// an *ArgsError.
func (d *Def[Args]) Decode(arguments string) (Args, error) {
	// An empty call is an empty object: some models omit "{}" for tools
	// with no required arguments.
	if strings.TrimSpace(arguments) == "" {
		arguments = "{}"
	}
	return decode[Args](checker{name: d.Name}, d.MaxBytes, arguments, d.Shape)
}

// decode checks input against shape and decodes it into a T.
func decode[T any](c checker, maxBytes int, input string, shape Shape) (T, error) {
	var out T
	limit := maxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	if len(input) > limit {
		return out, c.fail("", fmt.Sprintf("%d bytes, the limit is %d", len(input), limit))
	}
	dec := json.NewDecoder(strings.NewReader(input))
	dec.UseNumber()
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return out, c.fail("", "not valid JSON: "+err.Error())
	}
	if _, err := dec.Token(); err != io.EOF {
		return out, c.fail("", "unexpected data after the JSON object")
	}
	if err := c.check(raw, shape, ""); err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, c.fail("", err.Error())
	}
	return out, nil
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

// checker checks JSON against a Shape, reporting errors for the tool or
// output name.
type checker struct {
	name, what string
	// strict requires every declared property to be present, as strict
	// structured-output schemas do; optional ones may be null.
	strict bool
}

func (d checker) fail(path, msg string) error {
	return &ArgsError{Tool: d.name, What: d.what, Path: path, Msg: msg}
}

func (d checker) check(raw json.RawMessage, s Shape, path string) error {
	raw = bytes.TrimSpace(raw)
	if s.Kind != Any && isNull(raw) {
		return d.fail(path, "expected "+article(s.Kind)+", got null")
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
			if f.Required && (!ok || isNull(v)) {
				return d.fail(join(path, f.Name), "required")
			}
			if !ok && d.strict {
				return d.fail(join(path, f.Name), "missing; send null when there is no value")
			}
			// An optional property may be null: it decodes to the zero value.
			if !ok || isNull(v) {
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

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func article(k Kind) string {
	if k == Integer || k == Array || k == Object {
		return "an " + k.String()
	}
	return "a " + k.String()
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

// Output is a structured output whose JSON decodes into T: the shape a
// model must answer in, declared with .output in a .af file.
type Output[T any] struct {
	Name        string
	Description string
	// Schema is a strict JSON Schema of the output: every property is
	// required, optional ones are nullable, and no other properties are
	// allowed, as OpenAI's strict mode requires.
	Schema   json.RawMessage
	MaxBytes int
	Shape    Shape
	// Fields lists the fields with their types and descriptions, one per
	// line, for a prompt that explains the output to the model.
	Fields string
}

// Decode checks output, the JSON a model answered with, against the
// declaration and decodes it. Like the strict schema, it requires every
// field, allowing null for optional ones. Errors are *ArgsError, worded so the model
// can be asked to correct its answer.
func (o *Output[T]) Decode(output string) (T, error) {
	return decode[T](checker{name: o.Name, what: "output", strict: true}, o.MaxBytes, output, o.Shape)
}

// OpenAI returns the OpenAI response_format for the output:
// {"type":"json_schema","json_schema":{"name":…,"description":…,"schema":…,"strict":true}}.
func (o *Output[T]) OpenAI() json.RawMessage {
	var b bytes.Buffer
	b.WriteString(`{"type":"json_schema","json_schema":{"name":`)
	writeJSONString(&b, o.Name)
	if o.Description != "" {
		b.WriteString(`,"description":`)
		writeJSONString(&b, o.Description)
	}
	b.WriteString(`,"schema":`)
	b.Write(o.Schema)
	b.WriteString(`,"strict":true}}`)
	return b.Bytes()
}

// Anthropic returns the Anthropic Messages output format for the output,
// the value of the request's output_config.format:
// {"type":"json_schema","schema":…}.
func (o *Output[T]) Anthropic() json.RawMessage {
	var b bytes.Buffer
	b.WriteString(`{"type":"json_schema","schema":`)
	b.Write(o.Schema)
	b.WriteString(`}`)
	return b.Bytes()
}
