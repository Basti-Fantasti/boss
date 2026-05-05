// Package cli — auth command for managing stored HTTPS credentials.
// SSH credentials are no longer stored: ssh-agent and ~/.ssh/config
// supply them when bossy shells out to system git.
package cli

import (
	"fmt"

	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

func authCmdRegister(root *cobra.Command) {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage stored HTTPS credentials for private repositories",
		Long: `Manage HTTPS basic-auth credentials per host.

For SSH cloning, no command is needed — bossy invokes the system git
binary, which uses ssh-agent and ~/.ssh/config like a normal git clone.

For private HTTPS repositories, run:
  bossy auth set <host>          (prompts interactively)
  bossy auth set <host> -u USER -p PASS
  bossy auth list
  bossy auth rm <host>
`,
	}

	var user, pass string
	setCmd := &cobra.Command{
		Use:   "set <host>",
		Short: "Store HTTPS credentials for a host",
		Args:  cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			authSet(args[0], user, pass)
		},
	}
	setCmd.Flags().StringVarP(&user, "username", "u", "", "Username")
	setCmd.Flags().StringVarP(&pass, "password", "p", "", "Password or personal access token")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List hosts with stored credentials",
		Run:   func(_ *cobra.Command, _ []string) { authList() },
	}

	rmCmd := &cobra.Command{
		Use:   "rm <host>",
		Short: "Remove stored credentials for a host",
		Args:  cobra.ExactArgs(1),
		Run:   func(_ *cobra.Command, args []string) { authRm(args[0]) },
	}

	authCmd.AddCommand(setCmd, listCmd, rmCmd)
	root.AddCommand(authCmd)

	// Migration stubs for the old commands.
	root.AddCommand(&cobra.Command{
		Use:    "login",
		Hidden: true,
		Run: func(_ *cobra.Command, _ []string) {
			msg.Die("`boss login` was removed in bossy. Use `bossy auth set <host>` for HTTPS, " +
				"or configure ssh-agent / ~/.ssh/config for SSH.")
		},
	})
	root.AddCommand(&cobra.Command{
		Use:    "logout",
		Hidden: true,
		Run: func(_ *cobra.Command, _ []string) {
			msg.Die("`boss logout` was removed in bossy. Use `bossy auth rm <host>`.")
		},
	})
}

func authSet(host, user, pass string) {
	cfg := env.GlobalConfiguration()
	if user == "" {
		u, err := pterm.DefaultInteractiveTextInput.Show("Username")
		if err != nil {
			msg.Die("input error: %v", err)
		}
		user = u
	}
	if pass == "" {
		p, err := pterm.DefaultInteractiveTextInput.WithMask("•").Show("Password")
		if err != nil {
			msg.Die("input error: %v", err)
		}
		pass = p
	}
	if cfg.Auth == nil {
		cfg.Auth = make(map[string]*env.Auth)
	}
	a := &env.Auth{}
	a.SetUser(user)
	a.SetPass(pass)
	cfg.Auth[host] = a
	cfg.SaveConfiguration()
	msg.Success("Credentials stored for %s", host)
}

func authList() {
	cfg := env.GlobalConfiguration()
	if len(cfg.Auth) == 0 {
		fmt.Println("(no stored credentials)")
		return
	}
	for host := range cfg.Auth {
		fmt.Println(host)
	}
}

func authRm(host string) {
	cfg := env.GlobalConfiguration()
	if _, ok := cfg.Auth[host]; !ok {
		msg.Die("no credentials stored for %s", host)
	}
	delete(cfg.Auth, host)
	cfg.SaveConfiguration()
	msg.Success("Credentials removed for %s", host)
}
