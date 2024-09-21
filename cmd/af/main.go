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
package main

import (
	"context"

	"github.com/ditto-assistant/agentflow/cfg"
	"github.com/ditto-assistant/agentflow/cmd/af/gen"
	"github.com/ditto-assistant/agentflow/pkg/assert"
	"github.com/ditto-assistant/agentflow/pkg/logger"
	"github.com/spf13/cobra"
)

// Root represents the base command when called without any subcommands
var Root = &cobra.Command{
	Version:          "0.0.1",
	Use:              "agentflow",
	Short:            "agentflow",
	Long:             "agentflow",
	PersistentPreRun: func(cmd *cobra.Command, args []string) { logger.Setup() },
}

func main() {
	Root.PersistentFlags().StringVar(&cfg.LogLevel, "log", "debug", "Log level")

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	Root.AddCommand(gen.CMD())

	err := Root.ExecuteContext(ctx)
	assert.NoError(err)
}
