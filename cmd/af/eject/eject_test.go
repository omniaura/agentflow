package eject

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/cmd/af/gen/prompts"
)

// TestEjectedModuleBuilds ejects the runtime into a module with no
// agentflow dependency, generates a tool against it, and runs it.
func TestEjectedModuleBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go tool")
	}
	root := t.TempDir()
	write := func(name, body string) {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/ej\n\ngo 1.26\n")
	write("agent/tools.af", ".tool ping\n.param msg? What to echo.\nCheck the service.\n")
	write("agent/ping_test.go", `package agent

import "testing"

func TestPing(t *testing.T) {
	if _, err := PingTool.Decode(`+"`"+`{"msg":3}`+"`"+`); err == nil || err.Error() != "ping: invalid arguments: msg: expected a string, got a number" {
		t.Fatal(err)
	}
}
`)
	t.Chdir(root)
	var out bytes.Buffer
	cmd := CMD()
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "created     "+filepath.Join(root, "internal", "agentflow", "aftool", "aftool.go")) {
		t.Fatalf("output:\n%s", out.String())
	}
	_, code, err := prompts.Render(filepath.Join(root, "agent", "tools.af"), []byte(".tool ping\n.param msg? What to echo.\nCheck the service.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(code, []byte(`"example.com/ej/internal/agentflow/aftool"`)) {
		t.Fatalf("generated code does not import the copy:\n%s", code)
	}
	write("agent/tools_af.go", string(code))
	test := exec.Command("go", "test", "./...")
	test.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	if b, err := test.CombinedOutput(); err != nil {
		t.Fatalf("go test: %v\n%s", err, b)
	}
}
