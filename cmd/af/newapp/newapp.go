// Package newapp is af new: it creates an agentic backend, ready to run.
package newapp

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/omniaura/agentflow/cmd/af/gen/prompts"
)

//go:embed template/*.tmpl
var templates embed.FS

var output io.Writer = os.Stdout

// CMD is af new.
func CMD() *cobra.Command {
	var module string
	cmd := &cobra.Command{
		Use:   "new <dir>",
		Short: "Create an agentic backend, ready to run",
		Long: `Create a Go module in dir that serves an agent over HTTP, with its
conversations in Postgres: an .af file declaring the agent and its tools,
the code af gen makes from it, and a main.go that runs a afthread.Worker
and serves afthread.Server's thread API.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			if module == "" {
				module = filepath.Base(filepath.Clean(dir))
			}
			if err := Create(dir, module); err != nil {
				return err
			}
			_, err := fmt.Fprintf(output, nextSteps, dir)
			return err
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "module path (default: the directory's name)")
	return cmd
}

const nextSteps = `Created %[1]s.

Next:
  cd %[1]s
  go get github.com/omniaura/agentflow@latest github.com/omniaura/agentflow/adapters/afthreadpg@latest
  go mod tidy
  export DATABASE_URL=postgres://... ANTHROPIC_API_KEY=... APP_TOKEN=$(openssl rand -hex 16)
  go run .

Then see README.md to talk to the agent.
`

// Create writes the backend into dir, which must be empty or missing,
// with module path module.
func Create(dir, module string) error {
	if !validModule.MatchString(module) {
		return fmt.Errorf("af new: %q is not a module path", module)
	}
	if err := emptyDir(dir); err != nil {
		return err
	}
	data := struct{ Module, Name string }{module, name(module)}
	entries, err := templates.ReadDir("template")
	if err != nil {
		return err
	}
	for _, e := range entries {
		t, err := template.ParseFS(templates, "template/"+e.Name())
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, data); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, strings.TrimSuffix(e.Name(), ".tmpl")), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	// Generate the agent's code now, so the module builds as it is.
	af := filepath.Join(dir, "assistant.af")
	src, err := os.ReadFile(af)
	if err != nil {
		return err
	}
	out, code, err := prompts.Render(af, src)
	if err != nil {
		return fmt.Errorf("af new: generate %s: %w", af, err)
	}
	return os.WriteFile(out, code, 0o644)
}

// validModule is a module path without spaces or quotes.
var validModule = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~/-]*$`)

// name is the module's last element, as the app calls itself.
func name(module string) string {
	return module[strings.LastIndex(module, "/")+1:]
}

func emptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return os.MkdirAll(dir, 0o755)
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("af new: %s is not empty", dir)
	}
	return nil
}
