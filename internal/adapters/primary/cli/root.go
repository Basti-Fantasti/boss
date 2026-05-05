// Package cli provides the command-line interface for Boss package manager.
// It implements commands for dependency management, build operations, and configuration.
package cli

import (
	"os"
	"path/filepath"

	"github.com/basti-fantasti/bossy/internal/adapters/primary/cli/config"
	"github.com/basti-fantasti/bossy/internal/core/services/gc"
	"github.com/basti-fantasti/bossy/internal/migrate"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/basti-fantasti/bossy/setup"
	homedir "github.com/mitchellh/go-homedir"

	"github.com/spf13/cobra"
)

// runMigrations runs one-shot idempotent migrations before any command executes.
// MigrateHome copies ~/.boss → ~/.bossy so subsequent runs find the config in
// the new location. MigrateBossJSON rewrites :ssh version suffixes to SSH URL
// keys in the project boss.json. Both must run before setup.Initialize so that
// any newly migrated files are visible to the rest of startup.
func runMigrations() {
	if home, err := homedir.Dir(); err == nil {
		if moved, _ := migrate.MigrateHome(home); moved {
			msg.Info("ℹ️  Migrated ~/.boss to ~/.bossy")
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if changed, _ := migrate.MigrateBossJSON(filepath.Join(cwd, "boss.json")); changed {
			msg.Info("ℹ️  Migrated boss.json (:ssh suffix → SSH URL keys)")
		}
	}
}

// Execute executes the root command.
func Execute() error {
	runMigrations()
	var versionPrint bool
	var global bool
	var debug bool

	var root = &cobra.Command{
		Use:   "boss",
		Short: "Dependency Manager for Delphi",
		Long:  "Dependency Manager for Delphi",
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			if debug {
				msg.LogLevel(msg.DEBUG)
				msg.Debug("Debug mode enabled")
			}

			env.SetGlobal(global)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if versionPrint {
				printVersion()
			} else {
				return cmd.Help()
			}
			return nil
		},
	}

	root.PersistentFlags().BoolVarP(&global, "global", "g", false, "global environment")
	root.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "debug")
	root.Flags().BoolVarP(&versionPrint, "version", "v", false, "show cli version")

	setup.Initialize()

	config.RegisterConfigCommand(root)
	authCmdRegister(root)
	initCmdRegister(root)
	installCmdRegister(root)
	runCmdRegister(root)
	uninstallCmdRegister(root)
	updateCmdRegister(root)
	upgradeCmdRegister(root)
	dependenciesCmdRegister(root)
	versionCmdRegister(root)

	if err := gc.RunGC(false); err != nil {
		return err
	}

	config.RegisterCmd(root)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}

	return nil
}
