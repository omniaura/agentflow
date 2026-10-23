// Package vendored writes files that af copies into a repository, such as
// agent skills, so they can be upgraded later without losing local edits.
//
// A vendored file carries a stamp line holding the af version that wrote it
// and a hash of its contents. On upgrade, a file whose hash still matches was
// not edited and is replaced; an edited file is left alone unless forced.
package vendored

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Marker starts a stamp. A template holds Marker followed by Placeholder on
// one line, wherever that file format allows it, such as a YAML front matter
// field or a comment.
const (
	Marker      = "agentflow-vendored: "
	Placeholder = "STAMP"
)

// Stamp fills in the stamp of template with version and the hash of the
// template.
func Stamp(template []byte, version string) ([]byte, error) {
	if bytes.Count(template, []byte(Marker+Placeholder)) != 1 {
		return nil, errors.New("vendored: template needs exactly one " + Marker + Placeholder)
	}
	stamp := fmt.Sprintf("v%s sha256=%s", strings.TrimPrefix(version, "v"), sum(template))
	return bytes.Replace(template, []byte(Marker+Placeholder), []byte(Marker+stamp), 1), nil
}

// Info describes the stamp of a vendored file.
type Info struct {
	Version string
	// Edited reports whether the file changed after it was stamped.
	Edited bool
}

// Read reads the stamp of content. ok is false when there is none.
func Read(content []byte) (info Info, ok bool) {
	i := bytes.Index(content, []byte(Marker))
	if i < 0 {
		return info, false
	}
	start := i + len(Marker)
	end := bytes.IndexByte(content[start:], '\n')
	if end < 0 {
		end = len(content)
	} else {
		end += start
	}
	fields := strings.Fields(string(content[start:end]))
	if len(fields) != 2 || !strings.HasPrefix(fields[1], "sha256=") {
		return info, false
	}
	info.Version = strings.TrimPrefix(fields[0], "v")
	// Keep whatever followed the stamp on its line, such as a closing quote.
	rest := strings.TrimPrefix(string(content[start:end]), strings.Join(fields, " "))
	template := append(append(append([]byte{}, content[:start]...), Placeholder+rest...), content[end:]...)
	info.Edited = sum(template) != strings.TrimPrefix(fields[1], "sha256=")
	return info, true
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Result is what Write did to a file.
type Result int

const (
	Created Result = iota + 1
	Unchanged
	Upgraded
	// Kept means the file was edited, or not written by af, and was left
	// alone.
	Kept
	Overwritten
)

func (r Result) String() string {
	switch r {
	case Created:
		return "created"
	case Unchanged:
		return "unchanged"
	case Upgraded:
		return "upgraded"
	case Kept:
		return "kept"
	case Overwritten:
		return "overwritten"
	}
	return fmt.Sprintf("Result(%d)", int(r))
}

// Write writes the stamped content to path. It replaces an existing file
// only when that file is an unedited vendored file, or when force is set.
func Write(path string, content []byte, force bool) (Result, error) {
	old, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return 0, err
		}
		return Created, os.WriteFile(path, content, 0o644)
	case err != nil:
		return 0, err
	case bytes.Equal(old, content):
		return Unchanged, nil
	}
	result := Overwritten
	if info, ok := Read(old); ok && !info.Edited {
		result = Upgraded
	} else if !force {
		return Kept, nil
	}
	return result, os.WriteFile(path, content, 0o644)
}
