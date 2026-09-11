package preset

import (
	"fmt"
	"strings"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/presets"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/spf13/cobra"
)

// catalogSource narrows a listing to one of the two catalogs.
type catalogSource string

const (
	sourceAll    catalogSource = ""
	sourceLocal  catalogSource = "local"
	sourceRemote catalogSource = "remote"
)

// listAction is the testable core of `preset list`.
type listAction struct {
	tag    string
	source catalogSource
}

func (a listAction) run(svc *presets.Service) ([]string, error) {
	items, err := a.collect(svc)
	if err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return []string{"No presets found. Run 'bossy preset sync' or add one with 'bossy preset add'."}, nil
	}

	// Both columns are measured rather than guessed: a branch name such as
	// "pr/utf8-console-output-v2" overruns any fixed width and pushes the
	// repository column out of alignment for every other row.
	idWidth, refWidth := 0, 0
	for _, p := range items {
		if len(p.ID) > idWidth {
			idWidth = len(p.ID)
		}
		if ref := p.DefaultRef.String(); len(ref) > refWidth {
			refWidth = len(ref)
		}
	}

	lines := make([]string, 0, len(items))
	for _, p := range items {
		lines = append(lines, fmt.Sprintf("%-*s  %-*s  %s", idWidth, p.ID, refWidth, p.DefaultRef.String(), p.Repo))
	}
	return lines, nil
}

// collect resolves the listing against the requested source and tag.
func (a listAction) collect(svc *presets.Service) ([]domain.Preset, error) {
	var (
		items []domain.Preset
		err   error
	)

	switch a.source {
	case sourceLocal:
		var catalog domain.Catalog
		if catalog, err = svc.Local(); err == nil {
			items = catalog.Presets
		}
	case sourceRemote:
		var catalog domain.Catalog
		if catalog, err = svc.Remote(); err == nil {
			items = catalog.Presets
		}
	case sourceAll:
		items, err = svc.All()
	default:
		return nil, fmt.Errorf("unknown source %q (expected local or remote)", a.source)
	}
	if err != nil {
		return nil, err
	}

	if a.tag == "" {
		return items, nil
	}
	filtered := make([]domain.Preset, 0, len(items))
	for _, p := range items {
		if p.HasTag(a.tag) {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// showAction is the testable core of `preset show`.
type showAction struct {
	id string
}

func (a showAction) run(svc *presets.Service) ([]string, error) {
	preset, found, err := svc.ByID(a.id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: %s", presets.ErrNotFound, a.id)
	}

	lines := []string{
		"id           " + preset.ID,
		"name         " + preset.DisplayName(),
		"repo         " + preset.Repo,
		"ref          " + preset.DefaultRef.String(),
		"version      " + preset.Version(),
	}
	if preset.Description != "" {
		lines = append(lines, "description  "+preset.Description)
	}
	if len(preset.SearchPaths) > 0 {
		lines = append(lines, "searchpaths  "+strings.Join(preset.SearchPaths, ", "))
	}
	if len(preset.Platforms) > 0 {
		lines = append(lines, "platforms    "+strings.Join(preset.Platforms, ", "))
	}
	if len(preset.Tags) > 0 {
		lines = append(lines, "tags         "+strings.Join(preset.Tags, ", "))
	}
	if preset.Submodules.IsRemote() {
		lines = append(lines, "submodules   remote (advanced to branch tips, resolved commits go in the lock)")
	}
	return lines, nil
}

func listCmd() *cobra.Command {
	var tag string
	var source string

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the presets available to this machine",
		Args:    cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			lines, err := listAction{tag: tag, source: catalogSource(source)}.run(Service())
			if err != nil {
				msg.Die("%s", err.Error())
				return
			}
			for _, l := range lines {
				msg.Info("%s", l)
			}
		},
	}
	cmd.Flags().StringVar(&tag, "tag", "", "only presets carrying this tag")
	cmd.Flags().StringVar(&source, "source", "", "restrict to one catalog: local or remote")
	return cmd
}

func showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show one preset in full",
		Args:  cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			lines, err := showAction{id: args[0]}.run(Service())
			if err != nil {
				msg.Die("%s", err.Error())
				return
			}
			for _, l := range lines {
				msg.Info("%s", l)
			}
		},
	}
}
