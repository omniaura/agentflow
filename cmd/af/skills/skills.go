// Package skills implements af skills, which installs the agent skills that
// teach coding agents to write .af files.
package skills

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/omniaura/agentflow/cfg"
	"github.com/omniaura/agentflow/pkg/vendored"
	"github.com/spf13/cobra"
)

// FS holds the skills, one directory per skill.
//
//go:embed agentflow
var FS embed.FS

type options struct {
	dirs  []string
	force bool
}

func CMD() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Install agent skills for writing .af files",
		Long: `Install the agent skills that teach coding agents (Claude Code, Codex and
others that read SKILL.md files) to write and check .af files.`,
	}
	cmd.AddCommand(installCMD(), printCMD())
	return cmd
}

func installCMD() *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install or upgrade the skills in this repository",
		Long: `Install the skills into .agents/skills, and into .claude/skills when the
repository has a .claude directory that does not already share .agents/skills.

Running it again upgrades the skills to this af version. A skill file you have
edited is kept unless you pass --force.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := opts.dirs
			if len(dirs) == 0 {
				dirs = defaultDirs()
			}
			return install(cmd.OutOrStdout(), dirs, opts.force)
		},
	}
	cmd.Flags().StringSliceVar(&opts.dirs, "dir", nil, "Skills directory to install into (repeatable; default .agents/skills, plus .claude/skills)")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Overwrite skill files that were edited")
	return cmd
}

func printCMD() *cobra.Command {
	return &cobra.Command{
		Use:   "print [skill]",
		Short: "Print a skill's SKILL.md",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "agentflow"
			if len(args) == 1 {
				name = args[0]
			}
			b, err := Render(name + "/SKILL.md")
			if err != nil {
				return fmt.Errorf("unknown skill %q", name)
			}
			_, err = cmd.OutOrStdout().Write(b)
			return err
		},
	}
}

func defaultDirs() []string {
	dirs := []string{filepath.Join(".agents", "skills")}
	if fi, err := os.Stat(".claude"); err != nil || !fi.IsDir() {
		return dirs
	}
	claude := filepath.Join(".claude", "skills")
	a, errA := filepath.EvalSymlinks(dirs[0])
	c, errC := filepath.EvalSymlinks(claude)
	if errA == nil && errC == nil && a == c {
		return dirs
	}
	return append(dirs, claude)
}

// Render returns the embedded file at path, stamped with the af version.
func Render(path string) ([]byte, error) {
	b, err := FS.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(string(b), vendored.Marker) {
		return b, nil
	}
	return vendored.Stamp(b, cfg.Version)
}

func install(w io.Writer, dirs []string, force bool) error {
	kept := 0
	for _, dir := range dirs {
		err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			content, err := Render(path)
			if err != nil {
				return err
			}
			dst := filepath.Join(dir, filepath.FromSlash(path))
			result, err := vendored.Write(dst, content, force)
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "%s %s\n", result, dst)
			if result == vendored.Kept {
				kept++
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if kept > 0 {
		fmt.Fprintf(w, "%d edited file(s) kept; rerun with --force to replace them\n", kept)
	}
	return nil
}
