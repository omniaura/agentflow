package upgrade

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fake records commands and answers go list with versions in turn.
type fake struct {
	cmds     []string
	versions []string
	fail     string // A command prefix that fails.
}

func (f *fake) run(w io.Writer, dir, name string, args ...string) error {
	cmd := strings.Join(append([]string{name}, args...), " ")
	if strings.HasPrefix(cmd, "go list -m") {
		if len(f.versions) > 0 {
			fmt.Fprintln(w, f.versions[0])
			f.versions = f.versions[1:]
		}
		return nil
	}
	f.cmds = append(f.cmds, cmd)
	if f.fail != "" && strings.HasPrefix(cmd, f.fail) {
		return errors.New("exit status 1")
	}
	return nil
}

func module(t *testing.T, gomod string, files map[string]string) string {
	dir := t.TempDir()
	files["go.mod"] = gomod
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const pinned = "module example.com/m\n\ngo 1.26\n\ntool (\n\tgolang.org/x/tools/cmd/stringer\n\tgithub.com/omniaura/agentflow/cmd/af // af\n)\n"

func TestUpgradeRunsEveryStep(t *testing.T) {
	dir := module(t, pinned, map[string]string{
		"agentflow.json":                    `{"runtime":{"aftool":"internal/agentflow/aftool","afagent":"x/afagent"}}`,
		".agents/skills/agentflow/SKILL.md": "x",
	})
	f := &fake{versions: []string{"v0.12.1", "v0.14.0"}}
	var out strings.Builder
	if err := Upgrade(&out, filepath.Join(dir), "v0.14.0", f.run); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"go get -tool github.com/omniaura/agentflow/cmd/af@v0.14.0",
		"go tool af eject afagent aftool",
		"go tool af skills install",
		"go tool af gen prompts .",
		"go tool af check ./...",
	}
	if strings.Join(f.cmds, "\n") != strings.Join(want, "\n") {
		t.Errorf("commands:\n%s\nwant:\n%s", strings.Join(f.cmds, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(out.String(), "upgraded af v0.12.1 -> v0.14.0") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestUpgradeSkipsWhatIsNotThere(t *testing.T) {
	dir := module(t, "module m\n\ntool github.com/omniaura/agentflow/cmd/af\n", map[string]string{"pkg/a.go": "package pkg\n"})
	f := &fake{versions: []string{"v0.14.0", "v0.14.0"}}
	var out strings.Builder
	if err := Upgrade(&out, filepath.Join(dir, "pkg"), "latest", f.run); err != nil {
		t.Fatal(err)
	}
	want := "go get -tool github.com/omniaura/agentflow/cmd/af@latest\ngo tool af gen prompts .\ngo tool af check ./..."
	if strings.Join(f.cmds, "\n") != want {
		t.Errorf("commands:\n%s", strings.Join(f.cmds, "\n"))
	}
	if !strings.Contains(out.String(), "already at v0.14.0") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestUpgradeStopsAtAFailingStep(t *testing.T) {
	dir := module(t, pinned, map[string]string{"agentflow.json": `{"runtime":{"aftool":"internal/agentflow/aftool"}}`})
	f := &fake{fail: "go tool af eject"}
	err := Upgrade(io.Discard, dir, "latest", f.run)
	if err == nil || !strings.Contains(err.Error(), "resolve the conflict markers") {
		t.Fatalf("got %v", err)
	}
	if last := f.cmds[len(f.cmds)-1]; last != "go tool af eject aftool" {
		t.Errorf("ran past the failure: %v", f.cmds)
	}
}

func TestUpgradeNeedsAToolPin(t *testing.T) {
	dir := module(t, "module m\n\n// tool github.com/omniaura/agentflow/cmd/af\nrequire github.com/omniaura/agentflow v0.12.1\n", map[string]string{})
	f := &fake{}
	err := Upgrade(io.Discard, dir, "latest", f.run)
	if err == nil || !strings.Contains(err.Error(), "does not pin af as a tool") || len(f.cmds) != 0 {
		t.Fatalf("got %v after %v", err, f.cmds)
	}
}
