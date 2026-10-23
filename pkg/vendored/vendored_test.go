package vendored

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const template = "---\nname: x\nmetadata:\n  agentflow-vendored: STAMP\n---\nbody\n"

func TestStampRoundTrip(t *testing.T) {
	b, err := Stamp([]byte(template), "0.13.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "agentflow-vendored: v0.13.0 sha256=") {
		t.Fatalf("stamped = %q", b)
	}
	info, ok := Read(b)
	if !ok || info.Version != "0.13.0" || info.Edited {
		t.Fatalf("Read = %+v, %v", info, ok)
	}
	edited := strings.Replace(string(b), "body", "my body", 1)
	if info, ok := Read([]byte(edited)); !ok || !info.Edited {
		t.Fatalf("edited file reads as %+v, %v", info, ok)
	}
	if _, ok := Read([]byte("no stamp\n")); ok {
		t.Fatal("Read found a stamp in an unstamped file")
	}
	if _, err := Stamp([]byte("no placeholder"), "1"); err == nil {
		t.Fatal("Stamp accepted a template without a placeholder")
	}
}

func TestWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "SKILL.md")
	v1, _ := Stamp([]byte(template), "1.0.0")
	v2, _ := Stamp([]byte(strings.Replace(template, "body", "new body", 1)), "1.1.0")
	steps := []struct {
		content []byte
		force   bool
		want    Result
		edit    string
	}{
		{v1, false, Created, ""},
		{v1, false, Unchanged, ""},
		{v2, false, Upgraded, ""},
		{v1, false, Kept, "local edit\n"},
		{v1, true, Overwritten, ""},
	}
	for i, s := range steps {
		if s.edit != "" {
			f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			f.WriteString(s.edit)
			f.Close()
		}
		got, err := Write(path, s.content, s.force)
		if err != nil || got != s.want {
			t.Fatalf("step %d: got %v, %v; want %v", i, got, err, s.want)
		}
		b, _ := os.ReadFile(path)
		if s.want != Kept && string(b) != string(s.content) {
			t.Fatalf("step %d: file = %q", i, b)
		}
		if s.want == Kept && !strings.HasSuffix(string(b), s.edit) {
			t.Fatalf("step %d: edit lost", i)
		}
	}
	// A file af did not write is kept too.
	other := filepath.Join(t.TempDir(), "SKILL.md")
	os.WriteFile(other, []byte("mine\n"), 0o644)
	if got, _ := Write(other, v1, false); got != Kept {
		t.Fatalf("got %v for a foreign file", got)
	}
}
