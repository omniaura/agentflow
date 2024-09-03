/*
Copyright © 2024 Ditto AI peyton@heyditto.ai

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
package gen

import (
	"fmt"

	"github.com/ditto-assistant/agentflow/cmd/af/gen/prompts"
	"github.com/spf13/cobra"
)

func CMD() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("generate called")
		},
	}
	cmd.AddCommand(prompts.CMD())
	return cmd
}