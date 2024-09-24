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
package main

import (
	"context"

	"github.com/omniaura/agentflow/cfg"
	"github.com/omniaura/agentflow/cmd/af/gen"
	"github.com/omniaura/agentflow/pkg/assert"
	"github.com/omniaura/agentflow/pkg/logger"
	"github.com/spf13/cobra"
)

var Root = &cobra.Command{
	Version:          "v0.1.5", // This line will be updated by the sync-version script
	Use:              "af",
	Short:            "AgentFlow CLI",
	Long:             "AgentFlow is a CLI for bootstrapping AI agents.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) { logger.Setup() },
}

func main() {
	Root.PersistentFlags().StringVar(&cfg.FlagLogLevel, "log", "debug", "Log level")

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	Root.AddCommand(gen.CMD())

	err := Root.ExecuteContext(ctx)
	assert.NoError(err)
}
