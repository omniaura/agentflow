// Package upgrade implements af upgrade, which moves a module to a new af
// version: the pinned tool, the ejected runtime, the installed skills and
// the generated code.
package upgrade

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/omniaura/agentflow/pkg/eject"
	"github.com/omniaura/agentflow/pkg/sqlcgo"
	"github.com/spf13/cobra"
)

// Tool is the package go.mod pins to run af as a Go tool.
const Tool = eject.Module + "/cmd/af"

// Runner runs a command in dir, writing its output to w.
type Runner func(w io.Writer, dir, name string, args ...string) error

func run(w io.Writer, dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, w, w
	return cmd.Run()
}

// skillFiles are where af skills install puts the agentflow skill.
var skillFiles = []string{".agents/skills/agentflow/SKILL.md", ".claude/skills/agentflow/SKILL.md"}

func CMD() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade [version]",
		Short: "Upgrade this module to a new af version",
		Long: `Upgrade the module in the current directory to af version (default latest).

The module must pin af as a Go tool (go get -tool ` + Tool + `).
af upgrade moves the pin, then runs the new af to:

  1. upgrade the runtime packages listed in agentflow.json (af eject),
     merging your edits to them,
  2. upgrade the installed agent skills (af skills install),
  3. regenerate every _af.go (af gen prompts .), and
  4. check that the generated code is current (af check ./...).

It stops at the first step that fails. Merge conflicts in ejected files are
left marked; resolve them, then run af upgrade again to finish.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			version := "latest"
			if len(args) == 1 {
				version = args[0]
			}
			return Upgrade(cmd.OutOrStdout(), ".", version, run)
		},
	}
}

// Upgrade upgrades the module containing dir to af version, running
// commands with r.
func Upgrade(w io.Writer, dir, version string, r Runner) error {
	root, _, err := sqlcgo.ModuleRoot(dir)
	if err != nil {
		return err
	}
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return err
	}
	if !pinsTool(gomod) {
		return fmt.Errorf("go.mod does not pin af as a tool; run go get -tool %s@%s first, or install af %s and run af gen and af eject yourself", Tool, version, version)
	}
	step := func(what string, name string, args ...string) error {
		fmt.Fprintf(w, "==> %s: %s %s\n", what, name, strings.Join(args, " "))
		if err := r(w, root, name, args...); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		return nil
	}
	before := moduleVersion(root, r)
	if err := step("pin af", "go", "get", "-tool", Tool+"@"+version); err != nil {
		return err
	}
	after := moduleVersion(root, r)

	// From here on, run the af the module now pins.
	cfg, err := eject.ReadConfig(root)
	if err != nil {
		return err
	}
	if len(cfg.Runtime) > 0 {
		pkgs := make([]string, 0, len(cfg.Runtime))
		for pkg := range cfg.Runtime {
			pkgs = append(pkgs, pkg)
		}
		sort.Strings(pkgs)
		if err := step("upgrade the ejected runtime", "go", append([]string{"tool", "af", "eject"}, pkgs...)...); err != nil {
			return fmt.Errorf("%w\nresolve the conflict markers in the ejected files, then run af upgrade again", err)
		}
	}
	for _, f := range skillFiles {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			if err := step("upgrade the agent skills", "go", "tool", "af", "skills", "install"); err != nil {
				return err
			}
			break
		}
	}
	if err := step("regenerate", "go", "tool", "af", "gen", "prompts", "."); err != nil {
		return err
	}
	if err := step("check", "go", "tool", "af", "check", "./..."); err != nil {
		return err
	}
	switch {
	case before != "" && before == after:
		fmt.Fprintf(w, "af is already at %s; the runtime, skills and generated code are current\n", after)
	case before != "" && after != "":
		fmt.Fprintf(w, "upgraded af %s -> %s; review and commit the changes\n", before, after)
	default:
		fmt.Fprintln(w, "upgraded af; review and commit the changes")
	}
	return nil
}

// pinsTool reports whether go.mod has a tool directive for af.
func pinsTool(gomod []byte) bool {
	inBlock := false
	for _, line := range strings.Split(string(gomod), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "tool (":
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock && line == Tool, line == "tool "+Tool:
			return true
		}
	}
	return false
}

// moduleVersion returns the agentflow version the module requires, or ""
// if it can't tell.
func moduleVersion(root string, r Runner) string {
	var out bytes.Buffer
	if err := r(&out, root, "go", "list", "-m", "-f", "{{.Version}}", eject.Module); err != nil {
		return ""
	}
	v := strings.TrimSpace(out.String())
	if v == "" || strings.ContainsAny(v, " \n") {
		return ""
	}
	return v
}
