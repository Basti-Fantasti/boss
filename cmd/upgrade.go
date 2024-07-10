package cmd

import (
	"github.com/hashload/boss/msg"
	"github.com/spf13/cobra"
)

var preRelease bool

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade the client version",
	Example: `  Upgrade boss:
  boss upgrade

  Upgrade boss with pre-release:
  boss upgrade --dev`,
	RunE: func(cmd *cobra.Command, args []string) error {
		msg.Info("To avoid upgrading to main version,")
		msg.Info("Upgrade function has been disabled for this custom build!")
		msg.Info("To go back to main build, download the release from the original repo")
		return nil
	},
}

func init() {
	RootCmd.AddCommand(upgradeCmd)
	upgradeCmd.Flags().BoolVar(&preRelease, "dev", false, "pre-release")
}
