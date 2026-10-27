// Package eject implements af eject.
package eject

import (
	"fmt"
	"strings"

	"github.com/omniaura/agentflow/cfg"
	"github.com/omniaura/agentflow/pkg/eject"
	"github.com/omniaura/agentflow/runtime"
	"github.com/spf13/cobra"
)

func CMD() *cobra.Command {
	opts := eject.Options{}
	cmd := &cobra.Command{
		Use:   "eject [package...]",
		Short: "Copy the runtime packages into this module, or upgrade the copies",
		Long: `Copy agentflow's runtime packages (` + strings.Join(runtime.Packages, ", ") + `) into this module, so
generated code imports your copy instead of github.com/omniaura/agentflow. The
copy is yours to edit. With no arguments, eject copies every runtime package.
The optional packages (` + strings.Join(runtime.Adapters, ", ") + `) are copied only when named.

Run it again after upgrading af. Files you have not edited are replaced. Files
you have edited are merged three ways with the version they came from, and any
conflicts are left marked in the file. --force replaces edited files instead.

af eject records the copies in agentflow.json at the module root. Run af gen
afterwards to point generated code at them.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				args = runtime.Packages
			}
			opts.Version = cfg.Version
			results, err := eject.Eject(".", args, opts)
			conflicts := 0
			for _, r := range results {
				line := fmt.Sprintf("%-11s %s", r.Action, r.Path)
				if r.Note != "" {
					line += " (" + r.Note + ")"
				}
				fmt.Fprintln(cmd.OutOrStdout(), line)
				if r.Action == "conflict" {
					conflicts++
				}
			}
			if err != nil {
				return err
			}
			if conflicts > 0 {
				return fmt.Errorf("%d file(s) have merge conflicts to resolve", conflicts)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "run af gen to point generated code at the copies")
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Dir, "dir", eject.DefaultDir, "Directory for newly ejected packages, relative to the module root")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Replace edited files instead of merging them")
	return cmd
}
