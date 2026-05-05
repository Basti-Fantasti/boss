// Package config provides Git configuration commands.
package config

import (
	"strconv"

	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/spf13/cobra"
)

// registryGitCmd registers the git command.
func registryGitCmd(root *cobra.Command) {
	gitCmd := &cobra.Command{
		Use:   "git",
		Short: "Configure git-related behavior",
	}

	root.AddCommand(gitCmd)
	gitCmd.AddCommand(protocolCmd())
	gitCmd.AddCommand(shallowCmd())
}

func protocolCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "protocol <host> <ssh|https>",
		Short: "Set the default protocol for a host",
		Long: `Pin the default protocol bossy should use when cloning from <host>.
Overridden in CI by GITLAB_CI auto-detection and by BOSSY_AUTH_<HOST>.`,
		Args: cobra.ExactArgs(2),
		Run: func(_ *cobra.Command, args []string) {
			host, proto := args[0], args[1]
			if proto != "ssh" && proto != "https" {
				msg.Die("protocol must be 'ssh' or 'https'")
			}
			cfg := env.GlobalConfiguration()
			if cfg.HostProtocols == nil {
				cfg.HostProtocols = map[string]string{}
			}
			cfg.HostProtocols[host] = proto
			cfg.SaveConfiguration()
			msg.Success("✅ %s → %s", host, proto)
		},
	}
}

func shallowCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "shallow <true|false>",
		Short:     "Enable or disable shallow clones",
		Long:      "Enable or disable shallow clone (faster downloads, no history)",
		ValidArgs: []string{"true", "false"},
		Args: func(cmd *cobra.Command, args []string) error {
			err := cobra.OnlyValidArgs(cmd, args)
			if err == nil {
				err = cobra.ExactArgs(1)(cmd, args)
			}
			if err != nil {
				msg.Warn(err.Error())
				msg.Info("Current: %v\n\nValid args:\n\t%s\n",
					env.GlobalConfiguration().GitShallow,
					"true\n\tfalse")
				return err
			}
			return nil
		},
		Run: func(_ *cobra.Command, args []string) {
			v, err := strconv.ParseBool(args[0])
			if err != nil {
				msg.Die("expected true or false")
			}
			cfg := env.GlobalConfiguration()
			cfg.GitShallow = v
			cfg.SaveConfiguration()
			if cfg.GitShallow {
				msg.Info("Shallow clone enabled (faster, no git history)")
			} else {
				msg.Info("Shallow clone disabled (full git history)")
			}
		},
	}
}
