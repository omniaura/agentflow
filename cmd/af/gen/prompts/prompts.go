/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package prompts

import (
	"io"
	"os"

	"github.com/ditto-assistant/agentflow/pkg/assert"
	"github.com/ditto-assistant/agentflow/pkg/iterfs"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	DirInput  string
	DirOutput string
	Langs     []string
)

func flags(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().StringVarP(&DirInput,
		"input", "i", "prompts", "Directory to read prompt files from")
	cmd.Flags().StringVarP(&DirOutput,
		"output", "o", "prompts", "Directory to write generated prompts to")
	cmd.Flags().StringArrayVarP(&Langs,
		"langs", "l", []string{"py"}, "Languages to generate prompts for")
	return cmd
}

func CMD() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompts",
		Short: "A brief description of your command",
		Run: func(cmd *cobra.Command, args []string) {
			inFile, err := os.Open(DirInput)
			assert.NoError(err)
			defer inFile.Close()
			assert.NoError(err)
			for name := range iterfs.NewDir(inFile) {
				file, err := os.Open(name)
				assert.NoError(err)
				defer file.Close()
				f, err := io.ReadAll(file)
				assert.NoError(err)
				log.Debug().
					Bytes("file", f).
					Str("name", name).
					Msg("Opened file")
			}
		},
	}
	return flags(cmd)
}
