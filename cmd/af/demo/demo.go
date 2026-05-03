/*
Copyright © 2024 Omni Aura peyton@omniaura.co

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package demo

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/omniaura/agentflow/pkg/assert"
	"github.com/spf13/cobra"
)

var output io.Writer = os.Stdout

func CMD() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Create AgentFlow demo projects",
	}
	cmd.AddCommand(initCMD())
	return cmd
}

func initCMD() *cobra.Command {
	return &cobra.Command{
		Use:   "init <dir>",
		Short: "Create a runnable AgentFlow demo project",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			dir := args[0]
			assert.NoError(initDemo(dir))
			_, err := fmt.Fprintf(output, `Created AgentFlow demo in %s

Next:
  cd %s
  af gen prompts --dir prompts
  go run .

Editor:
  Open this folder in VS Code and install the AgentFlow extension for .af highlighting.
`, dir, dir)
			assert.NoError(err)
		},
	}
}

func initDemo(dir string) error {
	if err := ensureWritableDemoDir(dir); err != nil {
		return err
	}

	files := map[string]string{
		"go.mod":               goModContent,
		"README.md":            readmeContent,
		"main.go":              mainGoContent,
		"prompts/assistant.af": assistantAFContent,
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return err
		}
	}

	return nil
}

func ensureWritableDemoDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dir, 0755)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("demo path %q already exists and is not a directory", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("demo directory %q already exists and is not empty", dir)
	}

	return nil
}

const goModContent = `module agentflow-demo

go 1.25.0
`

const readmeContent = "# AgentFlow Demo\n" + `

This project demonstrates AgentFlow prompt templates with titles, typed variables, nested variables, conditionals, and comparisons.

## Run

` + "```sh" + `
af gen prompts --dir prompts
go run .
` + "```" + `

Open ` + "`prompts/assistant.af`" + ` in VS Code with the AgentFlow extension installed for syntax highlighting.
`

const mainGoContent = `package main

import (
	"fmt"

	"agentflow-demo/prompts"
)

func main() {
	briefing := prompts.SystemBriefing{}
	briefing.Agent.Name = "Ada"
	briefing.Workspace.Name = "AgentFlow Demo"
	briefing.Workspace.Production = false

	fmt.Println(briefing.String())
}
`

const assistantAFContent = `.title System Briefing
You are <!agent.name>, an AI assistant for <!workspace.name>.

<?workspace.production bool>
Treat this as a production environment. Be careful with destructive operations.
<else>
This is a sandbox environment. Prefer fast iteration.
</workspace.production>

.title Task Handoff
User: <!user.name>
Plan depth: <!plan.depth int>
Confidence target: <!confidence float64>

<?plan.depth gte 3>
Provide a structured plan before editing files.
<else>
Keep the response direct and execute the smallest correct change.
</plan.depth>

<?user.premium bool>
Use priority model routing for <!user.name>.
</user.premium>

.title Memory Summary
Project: <!project.name>
Owner: <!project.owner.name>
Open issues: <!project.open_issues int>

<?project.open_issues gt 0>
Mention the highest-risk open issue before implementation.
<else>
No known blockers.
</project.open_issues>
`
