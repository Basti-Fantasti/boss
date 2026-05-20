package config

import (
	"errors"
	"fmt"
	"sort"

	"github.com/basti-fantasti/bossy/pkg/consts"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/spf13/cobra"
)

// reservedAliasNames lists alias names that cannot be redefined because
// they collide with well-known URL schemes / SSH user prefixes.
//
//nolint:gochecknoglobals // small immutable lookup table
var reservedAliasNames = map[string]struct{}{
	"git": {}, "http": {}, "https": {}, "ssh": {},
}

// aliasAction is the side-effect-free core of the alias command, returning
// an error rather than calling msg.Die so it is unit-testable.
type aliasAction struct {
	list  bool
	unset string
	args  []string
}

func (a aliasAction) run(cfg *env.Configuration) ([]string, error) {
	if cfg.Aliases == nil {
		cfg.Aliases = map[string]string{}
	}
	switch {
	case a.list:
		return listAliases(cfg), nil
	case a.unset != "":
		return unsetAlias(cfg, a.unset)
	default:
		return setAlias(cfg, a.args)
	}
}

func listAliases(cfg *env.Configuration) []string {
	if len(cfg.Aliases) == 0 {
		return []string{"No aliases configured"}
	}
	keys := make([]string, 0, len(cfg.Aliases))
	for k := range cfg.Aliases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s → %s", k, cfg.Aliases[k]))
	}
	return lines
}

func unsetAlias(cfg *env.Configuration, name string) ([]string, error) {
	if _, ok := cfg.Aliases[name]; !ok {
		return nil, fmt.Errorf("alias %q not set", name)
	}
	delete(cfg.Aliases, name)
	cfg.SaveConfiguration()
	return []string{fmt.Sprintf("✅ removed alias %q", name)}, nil
}

func setAlias(cfg *env.Configuration, args []string) ([]string, error) {
	if len(args) != 2 {
		return nil, errors.New("usage: bossy config alias <name> <host>")
	}
	name, host := args[0], args[1]
	if !consts.AliasNamePattern.MatchString(name) {
		return nil, fmt.Errorf("alias name %q invalid (must match %s)", name, consts.AliasNamePattern.String())
	}
	if _, reserved := reservedAliasNames[name]; reserved {
		return nil, fmt.Errorf("alias name %q is reserved", name)
	}
	if _, ok := cfg.HostProtocols[name]; ok {
		return nil, fmt.Errorf("alias name %q collides with a configured host protocol", name)
	}
	cfg.Aliases[name] = host
	cfg.SaveConfiguration()
	return []string{fmt.Sprintf("✅ %s → %s", name, host)}, nil
}

func aliasCmd() *cobra.Command {
	var list bool
	var unset string

	cmd := &cobra.Command{
		Use:   "alias <name> <host>",
		Short: "Manage host aliases for dependency shorthand (`<alias>:<path>`)",
		Long: `Define short aliases for git hosts so dependencies can be referenced
as <alias>:<path>. The alias is expanded at install time and the canonical
host/path form is stored in bossy.json (aliases never round-trip).`,
		Args: cobra.MaximumNArgs(2),
		Run: func(_ *cobra.Command, args []string) {
			action := aliasAction{list: list, unset: unset, args: args}
			lines, err := action.run(env.GlobalConfiguration())
			if err != nil {
				msg.Die("%s", err.Error())
				return
			}
			// Distinguish list output (Info) from set/unset confirmation (Success).
			if action.list || (action.unset == "" && len(args) == 0) {
				for _, l := range lines {
					msg.Info("%s", l)
				}
				return
			}
			for _, l := range lines {
				msg.Success("%s", l)
			}
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "List all configured aliases")
	cmd.Flags().StringVar(&unset, "unset", "", "Remove the named alias")
	return cmd
}
