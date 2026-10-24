package skills

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/cmd/af/gen/prompts"
	"github.com/omniaura/agentflow/pkg/lint"
	"github.com/omniaura/agentflow/pkg/vendored"
)

var frontMatter = regexp.MustCompile(`(?s)^---\nname: ([a-z0-9-]+)\ndescription: "([^"\n]+)"\n.*?\n---\n`)

func TestSkillsAreWellFormed(t *testing.T) {
	n := 0
	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.Name() != "SKILL.md" {
			return err
		}
		n++
		b, err := Render(path)
		if err != nil {
			t.Fatal(err)
		}
		m := frontMatter.FindSubmatch(b)
		if m == nil {
			t.Fatalf("%s: missing name/description front matter", path)
		}
		if dir := filepath.Base(filepath.Dir(path)); string(m[1]) != dir {
			t.Fatalf("%s: name %q does not match directory %q", path, m[1], dir)
		}
		if len(m[2]) > 1024 {
			t.Fatalf("%s: description is %d chars, the limit is 1024", path, len(m[2]))
		}
		if info, ok := vendored.Read(b); !ok || info.Edited {
			t.Fatalf("%s: bad stamp %+v %v", path, info, ok)
		}
		return nil
	})
	if err != nil || n == 0 {
		t.Fatalf("found %d skills, err %v", n, err)
	}
}

// Every ```af example in a skill must lint clean and generate, so the skills
// never teach syntax af rejects.
func TestSkillExamplesGenerate(t *testing.T) {
	b, err := FS.ReadFile("agentflow/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```af\n(.*?)```").FindAllSubmatch(b, -1)
	if len(blocks) < 3 {
		t.Fatalf("found %d af examples", len(blocks))
	}
	for i, m := range blocks {
		src := m[1]
		ds, err := lint.Lint("example.af", src)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range ds {
			t.Errorf("example %d: %s:%d:%d: %s %s", i, d.File, d.Line, d.Column, d.Code, d.Message)
		}
		name := filepath.Join(t.TempDir(), "example.af")
		switch {
		case bytes.Contains(src, []byte("\n.sql ")):
			// Inline queries must be ones sqlc generated: those of
			// examples/routes/assistant.af. Render writes nothing.
			name = filepath.Join("..", "..", "..", "examples", "routes", "assistant.af")
		case bytes.Contains(src, []byte(".route ")):
			// Route examples use the todo queries of examples/routes.
			name = filepath.Join("..", "..", "..", "examples", "routes", "skill_example.af")
		}
		if _, _, err := prompts.Render(name, src); err != nil {
			t.Errorf("example %d does not generate: %v\n%s", i, err, src)
		}
	}
}

func TestInstall(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Mkdir(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := install(&out, defaultDirs(), false); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".agents/skills/agentflow/SKILL.md", ".claude/skills/agentflow/SKILL.md", ".agents/skills/agentflow-migrate/SKILL.md", ".claude/skills/agentflow-migrate/SKILL.md"} {
		if !strings.Contains(out.String(), "created "+filepath.FromSlash(p)) {
			t.Fatalf("output %q is missing %s", out.String(), p)
		}
	}

	// Sharing one directory through a symlink installs once.
	os.RemoveAll(".claude/skills")
	if err := os.Symlink(filepath.Join("..", ".agents", "skills"), ".claude/skills"); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if dirs := defaultDirs(); len(dirs) != 1 {
		t.Fatalf("dirs = %v, want only .agents/skills", dirs)
	}
}
