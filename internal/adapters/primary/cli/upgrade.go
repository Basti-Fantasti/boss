// Package cli implements Boss CLI commands.
package cli

import (
	"github.com/basti-fantasti/bossy/internal/upgrade"
	"github.com/spf13/cobra"
)

// upgradeCmdRegister registers the upgrade command.
func upgradeCmdRegister(root *cobra.Command) {
	var preRelease bool

	var upgradeCmd = &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the client version",
		Example: `  Upgrade bossy:
  bossy upgrade

  Upgrade bossy with pre-release:
  bossy upgrade --dev`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return upgrade.BossUpgrade(preRelease)
		},
	}

	root.AddCommand(upgradeCmd)
	upgradeCmd.Flags().BoolVar(&preRelease, "dev", false, "pre-release")
}
