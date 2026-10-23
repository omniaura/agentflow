// Package survey implements af survey.
package survey

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/omniaura/agentflow/pkg/survey"
	"github.com/spf13/cobra"
)

func CMD() *cobra.Command {
	var asJSON bool
	var top int
	cmd := &cobra.Command{
		Use:   "survey [dir]",
		Short: "Estimate how much of a Go module could be defined in .af files",
		Long: `Survey a Go module for hand-written code agentflow could generate: LLM tool
schemas (.tool), structured-output schemas (.output), HTTP handlers that only
serve one sqlc query (.route), and long prompt strings (.title), next to the code
agentflow and sqlc already generate.

The numbers come from syntactic heuristics: an estimate to steer migration,
not a measurement.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			r, err := survey.Run(dir)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(r)
			}
			write(cmd.OutOrStdout(), r, top)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the full report as JSON")
	cmd.Flags().IntVar(&top, "top", 5, "Show the largest sites of each category")
	return cmd
}

func pct(n, of int) string {
	if of == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(of))
}

func write(w io.Writer, r *survey.Report, top int) {
	fmt.Fprintf(w, "Go lines (non-test): %d, hand-written: %d, .af lines: %d\n\n", r.Lines, r.HandWritten, r.AFLines)
	fmt.Fprintf(w, "%-34s %7s %8s %s\n", "already generated", "files", "lines", "of all")
	for _, c := range []survey.Category{survey.GenAgentflow, survey.GenSqlc, survey.GenOther} {
		fmt.Fprintf(w, "  %-32s %7d %8d %s\n", c, len(r.Sites[c]), r.LinesOf(c), pct(r.LinesOf(c), r.Lines))
	}
	directive := map[survey.Category]string{survey.Tools: ".tool", survey.Outputs: ".output", survey.Routes: ".route", survey.Prompts: ".title"}
	fmt.Fprintf(w, "\n%-34s %7s %8s %s\n", "candidates for .af", "sites", "lines", "of hand-written")
	total := 0
	for _, c := range survey.Candidates {
		n := r.LinesOf(c)
		total += n
		fmt.Fprintf(w, "  %-32s %7d %8d %s\n", string(c)+" ("+directive[c]+")", len(r.Sites[c]), n, pct(n, r.HandWritten))
	}
	fmt.Fprintf(w, "  %-32s %7s %8d %s\n", "total", "", total, pct(total, r.HandWritten))
	if top <= 0 {
		return
	}
	for _, c := range survey.Candidates {
		sites := r.Sites[c]
		if len(sites) == 0 {
			continue
		}
		fmt.Fprintf(w, "\nlargest %s:\n", c)
		for _, s := range sites[:min(top, len(sites))] {
			name := ""
			if s.Name != "" {
				name = " " + s.Name
			}
			fmt.Fprintf(w, "  %s:%d%s (%d lines)\n", s.File, s.Line, name, s.Lines)
		}
	}
}
