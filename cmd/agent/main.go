package main

import (
	"fmt"
	"os"

	"github.com/echotools/nevr-agent/v4/internal/config"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	version    = "dev"
	cfg        *config.Config
	logger     *zap.Logger
	configFile string
	debugFlag  bool
	logLevel   string
	logFile    string
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "agent",
		Short:   "NEVR Agent - Tools for recording and processing EchoVR telemetry",
		Version: version,
		Long: `NEVR Agent is a suite of tools for recording session and player bone 
data from the EchoVR game engine HTTP API, converting between formats, and 
serving recorded data.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			var err error
			cfg, err = config.LoadConfig(configFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Override config with CLI flags (highest priority)
			if cmd.Flags().Changed("debug") {
				cfg.Debug = debugFlag
			}
			if cmd.Flags().Changed("log-level") {
				cfg.LogLevel = logLevel
			}
			if cmd.Flags().Changed("log-file") {
				cfg.LogFile = logFile
			}

			logger, err = cfg.NewLogger()
			if err != nil {
				return fmt.Errorf("failed to create logger: %w", err)
			}

			return nil
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if logger != nil {
				_ = logger.Sync()
			}
		},
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "config file (default is ./agent.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&debugFlag, "debug", "d", false, "enable debug logging")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVar(&logFile, "log-file", "", "log file path")

	// Define command groups
	mainGroup := &cobra.Group{
		ID:    "main",
		Title: "Main Commands",
	}
	rootCmd.AddGroup(mainGroup)

	// Add subcommands
	streamCmd := newAgentCommand()
	streamCmd.GroupID = "main"
	rootCmd.AddCommand(streamCmd)

	serveCmd := newAPIServerCommand()
	serveCmd.GroupID = "main"
	rootCmd.AddCommand(serveCmd)

	convertCmd := newConverterCommand()
	convertCmd.GroupID = "main"
	rootCmd.AddCommand(convertCmd)

	replayCmd := newReplayerCommand()
	replayCmd.GroupID = "main"
	rootCmd.AddCommand(replayCmd)

	showCmd := newDumpEventsCommand()
	showCmd.GroupID = "main"
	rootCmd.AddCommand(showCmd)

	rootCmd.AddCommand(newVersionCheckCommand())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
