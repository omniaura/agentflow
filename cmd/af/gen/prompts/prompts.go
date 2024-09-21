/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package prompts

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ditto-assistant/agentflow/pkg/assert"
	"github.com/ditto-assistant/agentflow/pkg/ast"
	"github.com/ditto-assistant/agentflow/pkg/gen/py"
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

			// Create output directory if it doesn't exist
			err = os.MkdirAll(DirOutput, os.ModePerm)
			assert.NoError(err)

			files, err := filepath.Glob(filepath.Join(DirInput, "*.af"))
			assert.NoError(err)
			for _, name := range files {
				file, err := os.Open(name)
				assert.NoError(err)
				defer file.Close()
				f, err := io.ReadAll(file)
				assert.NoError(err)

				fName := filepath.Base(file.Name())
				ff, err := ast.NewFile(fName, f)
				assert.NoError(err)

				for _, lang := range Langs {
					switch lang {
					case "py":
						outFileName := fmt.Sprintf("%s.py", ff.Name)
						outFilePath := filepath.Join(DirOutput, outFileName)
						outFile, err := os.OpenFile(outFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
						assert.NoError(err)
						defer outFile.Close()

						err = py.GenFile(outFile, ff)
						assert.NoError(err)
						slog.Info("Generated", "file", outFilePath)
					}
				}
			}
		},
	}
	return flags(cmd)
}
