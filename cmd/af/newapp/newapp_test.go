package newapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/omniaura/agentflow/cmd/af/gen/prompts"
)

func TestCreate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	if err := Create(dir, "example.com/acme/support-bot"); err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := read("go.mod"); !strings.HasPrefix(got, "module example.com/acme/support-bot\n") {
		t.Errorf("go.mod = %q", got)
	}
	if got := read("assistant.af"); !strings.Contains(got, "You are the assistant of support-bot.") {
		t.Errorf("assistant.af = %q", got)
	}
	// The agent's code is generated already.
	if got := read("assistant_af.go"); !strings.Contains(got, "type AssistantAgent struct") || !strings.Contains(got, "func (a *AssistantAgent) ForThread(") {
		t.Errorf("assistant_af.go lacks the agent")
	}
	for _, name := range []string{"main.go", "tools.go", "README.md"} {
		if read(name) == "" {
			t.Errorf("%s is empty", name)
		}
	}
	if err := Create(dir, "x"); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("Create into a full directory: %v", err)
	}
	for _, bad := range []string{`bad "module"`, "example.com/acme/", "example.com//bot", "/bot"} {
		if err := Create(t.TempDir(), bad); err == nil {
			t.Errorf("Create with module path %q succeeded", bad)
		}
	}
}

// A failed Create leaves the directory as it found it, so a retry works.
func TestCreateFailureCleansUp(t *testing.T) {
	render = func(string, []byte) (string, []byte, error) { return "", nil, errors.New("boom") }
	defer func() { render = prompts.Render }()
	missing := filepath.Join(t.TempDir(), "app")
	if err := Create(missing, "app"); err == nil {
		t.Fatal("Create succeeded")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("the directory Create made is still there: %v", err)
	}
	empty := t.TempDir()
	if err := Create(empty, "app"); err == nil {
		t.Fatal("Create succeeded")
	}
	if entries, _ := os.ReadDir(empty); len(entries) != 0 {
		t.Errorf("files left behind: %v", entries)
	}
	render = prompts.Render
	if err := Create(empty, "app"); err != nil {
		t.Errorf("retry: %v", err)
	}
}

// TestScaffoldRuns builds a new app against this checkout and, with a
// database, runs it: a client creates a thread, sends a message and reads
// it back. The model is unreachable, so the message fails, and the server
// keeps serving. Set AF_TEST_SCAFFOLD=1 to run it; it downloads modules.
func TestScaffoldRuns(t *testing.T) {
	if os.Getenv("AF_TEST_SCAFFOLD") == "" {
		t.Skip("set AF_TEST_SCAFFOLD=1 to build a new app")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Join(filepath.Dir(file), "..", "..", "..")
	dir := filepath.Join(t.TempDir(), "app")
	if err := Create(dir, "example.com/app"); err != nil {
		t.Fatal(err)
	}
	gomod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	gomod = fmt.Appendf(gomod, "\nreplace github.com/omniaura/agentflow => %s\n\nreplace github.com/omniaura/agentflow/adapters/afthreadpg => %s\n", repo, filepath.Join(repo, "adapters", "afthreadpg"))
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), gomod, 0o644); err != nil {
		t.Fatal(err)
	}
	gocmd := func(args ...string) {
		t.Helper()
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	gocmd("get", "github.com/omniaura/agentflow", "github.com/omniaura/agentflow/adapters/afthreadpg")
	gocmd("mod", "tidy")
	gocmd("vet", ".")
	gocmd("build", "-o", "app", ".")

	url := os.Getenv("AF_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("built; set AF_TEST_DATABASE_URL to run it")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := exec.CommandContext(ctx, filepath.Join(dir, "app"))
	app.Env = append(os.Environ(), "DATABASE_URL="+url, fmt.Sprint("PORT=", port), "APP_TOKEN=secret",
		"ANTHROPIC_API_KEY=test", "ANTHROPIC_BASE_URL=http://127.0.0.1:1")
	var logs strings.Builder
	app.Stdout, app.Stderr = &logs, &logs
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if t.Failed() {
			t.Logf("app logs:\n%s", logs.String())
		}
	}()
	base := fmt.Sprintf("http://127.0.0.1:%d/threads", port)
	call := func(method, path, body string) (int, map[string]any) {
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, nil
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	var thread map[string]any
	for start := time.Now(); time.Since(start) < 30*time.Second; time.Sleep(100 * time.Millisecond) {
		var code int
		if code, thread = call("POST", "", `{"agent":"assistant"}`); code == http.StatusCreated {
			break
		}
	}
	id, _ := thread["id"].(string)
	if id == "" {
		t.Fatalf("no thread created: %v", thread)
	}
	if code, sent := call("POST", "/"+id+"/messages", `{"message":"hi"}`); code != http.StatusAccepted {
		t.Fatalf("send: %d %v", code, sent)
	}
	// The model can't be reached: the run fails, and the server goes on.
	for start := time.Now(); ; time.Sleep(200 * time.Millisecond) {
		_, runs := call("GET", "/"+id+"/runs", "")
		if list, _ := runs["runs"].([]any); len(list) == 1 && list[0].(map[string]any)["status"] == "failed" {
			break
		}
		if time.Since(start) > 30*time.Second {
			t.Fatalf("the run didn't fail: %v", runs)
		}
	}
	if code, msgs := call("GET", "/"+id+"/messages", ""); code != 200 || fmt.Sprint(msgs["messages"]) != "[map[content:hi role:user]]" {
		t.Errorf("messages: %d %v", code, msgs)
	}
	// SIGTERM shuts the server down cleanly.
	app.Process.Signal(syscall.SIGTERM)
	exited := make(chan error, 1)
	go func() { exited <- app.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			t.Errorf("exit after SIGTERM: %v", err)
		}
	case <-time.After(time.Minute):
		app.Process.Kill()
		t.Error("the app didn't exit after SIGTERM")
	}
}
