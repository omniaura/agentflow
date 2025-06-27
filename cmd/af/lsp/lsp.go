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
package lsp

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/omniaura/agentflow/pkg/lsp"
	"github.com/spf13/cobra"
)

var (
	// Mode controls how the LSP server communicates
	// "stdio" uses stdin/stdout, "tcp" uses TCP on specified port
	Mode string
	// Port is the TCP port to listen on when mode is "tcp"
	Port int
	// Debug enables debug logging
	Debug bool
)

// CMD creates the lsp command
func CMD() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "Start the AgentFlow Language Server Protocol (LSP) server",
		Long: `Start the AgentFlow LSP server to provide language support for .af files.

The LSP server provides:
- Syntax highlighting
- Error diagnostics
- Auto-completion
- Go-to-definition
- Hover information
- Document outline`,
		Run: func(cmd *cobra.Command, args []string) {
			start := time.Now()

			// Set up panic recovery
			defer func() {
				if r := recover(); r != nil {
					slog.Error("LSP server panic recovered",
						"panic", r,
						"stack", string(debug.Stack()),
						"uptime", time.Since(start))
					os.Exit(1)
				}
			}()

			// Configure logging level based on debug flag
			if Debug {
				slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
				slog.Info("Debug logging enabled")
			} else {
				slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
			}

			// Set up signal handling
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)

			go func() {
				sig := <-sigChan
				slog.Info("Received signal, shutting down gracefully",
					"signal", sig.String(),
					"uptime", time.Since(start))
				os.Exit(0)
			}()

			slog.Info("Starting AgentFlow LSP server",
				"mode", Mode,
				"port", Port,
				"debug", Debug,
				"pid", os.Getpid(),
				"goVersion", runtime.Version(),
				"numCPU", runtime.NumCPU())

			if Debug {
				slog.Debug("Command arguments", "args", args)
				slog.Debug("Environment details",
					"GOOS", runtime.GOOS,
					"GOARCH", runtime.GOARCH,
					"workingDir", getWorkingDir())

				// Log memory stats
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				slog.Debug("Initial memory stats",
					"allocMB", bToMb(m.Alloc),
					"totalAllocMB", bToMb(m.TotalAlloc),
					"sysMB", bToMb(m.Sys),
					"numGC", m.NumGC)
			}

			server := lsp.NewServer(Debug)

			serverCreateDuration := time.Since(start)
			slog.Info("LSP server instance created", "duration", serverCreateDuration)

			switch Mode {
			case "stdio":
				slog.Info("Starting AgentFlow LSP server", "mode", "stdio")
				if Debug {
					slog.Debug("Server will communicate via stdin/stdout")
				}

				runStart := time.Now()

				// Wrap the server.RunStdio call with additional error context
				func() {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("Panic in RunStdio",
								"panic", r,
								"stack", string(debug.Stack()),
								"mode", "stdio",
								"uptime", time.Since(start),
								"runDuration", time.Since(runStart))
							os.Exit(1)
						}
					}()

					slog.Info("About to call server.RunStdio()")
					err := server.RunStdio()
					slog.Info("server.RunStdio() returned",
						"error", err,
						"runDuration", time.Since(runStart))

					if err != nil {
						slog.Error("LSP server error",
							"mode", "stdio",
							"error", err,
							"errorType", fmt.Sprintf("%T", err),
							"uptime", time.Since(start),
							"runDuration", time.Since(runStart))
						os.Exit(1)
					}
				}()

			case "tcp":
				address := fmt.Sprintf(":%d", Port)
				slog.Info("Starting AgentFlow LSP server",
					"mode", "tcp",
					"port", Port,
					"address", address)

				if Debug {
					slog.Debug("Server will listen on TCP",
						"address", address,
						"port", Port)
				}

				runStart := time.Now()

				// Wrap the server.RunTCP call with additional error context
				func() {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("Panic in RunTCP",
								"panic", r,
								"stack", string(debug.Stack()),
								"mode", "tcp",
								"address", address,
								"uptime", time.Since(start),
								"runDuration", time.Since(runStart))
							os.Exit(1)
						}
					}()

					slog.Info("About to call server.RunTCP()", "address", address)
					err := server.RunTCP(address)
					slog.Info("server.RunTCP() returned",
						"error", err,
						"address", address,
						"runDuration", time.Since(runStart))

					if err != nil {
						slog.Error("LSP server error",
							"mode", "tcp",
							"address", address,
							"error", err,
							"errorType", fmt.Sprintf("%T", err),
							"uptime", time.Since(start),
							"runDuration", time.Since(runStart))
						os.Exit(1)
					}
				}()

			default:
				slog.Error("Invalid mode",
					"mode", Mode,
					"validModes", []string{"stdio", "tcp"})
				fmt.Fprintf(os.Stderr, "Invalid mode: %s. Use 'stdio' or 'tcp'\n", Mode)
				os.Exit(1)
			}

			// This should never be reached for stdio mode, but for TCP mode
			// if the server shuts down gracefully
			totalUptime := time.Since(start)
			slog.Info("LSP server shutdown complete",
				"mode", Mode,
				"totalUptime", totalUptime)
		},
	}

	cmd.Flags().StringVar(&Mode, "mode", "stdio", "Communication mode: 'stdio' or 'tcp'")
	cmd.Flags().IntVar(&Port, "port", 4389, "TCP port to listen on (when mode is 'tcp')")
	cmd.Flags().BoolVar(&Debug, "debug", false, "Enable debug logging")
	return cmd
}

// getWorkingDir safely gets the current working directory
func getWorkingDir() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "unknown"
}

// bToMb converts bytes to megabytes
func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}
