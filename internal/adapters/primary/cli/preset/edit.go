package preset

import (
	"errors"
	"fmt"
	"strings"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/presets"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/spf13/cobra"
)

// editMode selects which mutation an editAction performs.
type editMode int

const (
	modeAdd editMode = iota
	modeUpdate
	modeRemove
)

// presetFields carries the flag values for add and update.
//
// The *Set fields record whether a flag was actually passed. update patches
// only what was given, and without them an omitted --repo would be
// indistinguishable from --repo="" and would wipe the stored value.
type presetFields struct {
	repo           string
	repoSet        bool
	ref            string
	refSet         bool
	name           string
	nameSet        bool
	description    string
	descriptionSet bool
	searchPaths    []string
	searchPathsSet bool
	platforms      []string
	platformsSet   bool
	tags           []string
	tagsSet        bool
}

// editAction is the testable core of add, update and rm.
type editAction struct {
	mode   editMode
	id     string
	fields presetFields
}

func (a editAction) run(svc *presets.Service) ([]string, error) {
	if a.mode == modeRemove {
		if err := svc.Remove(a.id); err != nil {
			return nil, err
		}
		return []string{fmt.Sprintf("✅ removed preset %q", a.id)}, nil
	}

	base, err := a.base(svc)
	if err != nil {
		return nil, err
	}

	preset, err := a.apply(base)
	if err != nil {
		return nil, err
	}

	if a.mode == modeAdd {
		if err := svc.Add(preset); err != nil {
			return nil, err
		}
		return []string{fmt.Sprintf("✅ added preset %q → %s", preset.ID, preset.Repo)}, nil
	}

	if err := svc.Update(preset); err != nil {
		return nil, err
	}
	return []string{fmt.Sprintf("✅ updated preset %q → %s", preset.ID, preset.Repo)}, nil
}

// base returns the preset an edit starts from.
//
// For update it is the merged entry, so patching an id that exists only in the
// synced catalog produces a local override carrying the shared entry's other
// fields rather than a half-empty one.
func (a editAction) base(svc *presets.Service) (domain.Preset, error) {
	if a.mode == modeAdd {
		return domain.Preset{ID: a.id, DefaultRef: domain.PresetRef{Kind: domain.RefKindDefault}}, nil
	}

	existing, found, err := svc.ByID(a.id)
	if err != nil {
		return domain.Preset{}, err
	}
	if !found {
		return domain.Preset{}, fmt.Errorf("%w: %s", presets.ErrNotFound, a.id)
	}
	existing.ID = a.id
	return existing, nil
}

// apply overlays the flag values onto a preset.
func (a editAction) apply(preset domain.Preset) (domain.Preset, error) {
	f := a.fields

	// On add every flag value is meaningful even when the *Set marker is
	// absent, because there is nothing to preserve.
	set := func(explicit bool) bool { return explicit || a.mode == modeAdd }

	if set(f.repoSet) && f.repo != "" {
		preset.Repo = f.repo
	}
	if set(f.nameSet) {
		preset.Name = f.name
	}
	if set(f.descriptionSet) {
		preset.Description = f.description
	}
	if set(f.searchPathsSet) && len(f.searchPaths) > 0 {
		preset.SearchPaths = f.searchPaths
	}
	if set(f.platformsSet) && len(f.platforms) > 0 {
		preset.Platforms = f.platforms
	}
	if set(f.tagsSet) && len(f.tags) > 0 {
		preset.Tags = f.tags
	}
	if set(f.refSet) && (f.ref != "" || a.mode == modeAdd) {
		ref, err := parseRefFlag(f.ref)
		if err != nil {
			return domain.Preset{}, err
		}
		preset.DefaultRef = ref
	}

	if err := preset.Validate(); err != nil {
		return domain.Preset{}, err
	}
	return preset, nil
}

// parseRefFlag turns the --ref flag into a reference.
//
// Accepted forms are "branch:<name>", "tag:<name>", "commit:<sha>" and a bare
// "default"; an empty flag also means default. Only the first colon separates
// kind from value, so a branch name containing a colon survives intact.
func parseRefFlag(input string) (domain.PresetRef, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || trimmed == string(domain.RefKindDefault) {
		return domain.PresetRef{Kind: domain.RefKindDefault}, nil
	}

	kind, value, found := strings.Cut(trimmed, ":")
	if !found {
		return domain.PresetRef{}, fmt.Errorf(
			"ref %q must be written as <kind>:<value>, e.g. tag:3.4.2-magnesium (or bare 'default')", input)
	}

	ref := domain.PresetRef{Kind: domain.RefKind(kind), Value: value}
	if err := ref.Validate(); err != nil {
		return domain.PresetRef{}, err
	}
	return ref, nil
}

// bindPresetFlags wires the shared add/update flags onto a command.
func bindPresetFlags(cmd *cobra.Command, f *presetFields) {
	cmd.Flags().StringVar(&f.repo, "repo", "", "repository key, e.g. github.com/owner/name or gtr:group/name")
	cmd.Flags().StringVar(&f.ref, "ref", "", "default reference: branch:<name>, tag:<name>, commit:<sha> or default")
	cmd.Flags().StringVar(&f.name, "name", "", "human-readable name")
	cmd.Flags().StringVar(&f.description, "description", "", "one-line description")
	cmd.Flags().StringSliceVar(&f.searchPaths, "searchpath", nil,
		"directory inside the dependency to put on the Delphi search path (repeatable)")
	cmd.Flags().StringSliceVar(&f.platforms, "platform", nil, "supported platform (repeatable)")
	cmd.Flags().StringSliceVar(&f.tags, "tag", nil, "tag used for filtering and 'bossy add --tag' (repeatable)")
}

// captureChangedFlags records which of the shared flags the user actually
// passed, so update can patch rather than overwrite.
func captureChangedFlags(cmd *cobra.Command, f *presetFields) {
	f.repoSet = cmd.Flags().Changed("repo")
	f.refSet = cmd.Flags().Changed("ref")
	f.nameSet = cmd.Flags().Changed("name")
	f.descriptionSet = cmd.Flags().Changed("description")
	f.searchPathsSet = cmd.Flags().Changed("searchpath")
	f.platformsSet = cmd.Flags().Changed("platform")
	f.tagsSet = cmd.Flags().Changed("tag")
}

// renderEdit prints the result of a mutation, or dies with a readable message.
func renderEdit(lines []string, err error) {
	if err != nil {
		switch {
		case errors.Is(err, presets.ErrExists):
			msg.Die("%s (use 'bossy preset update' to change it)", err.Error())
		case errors.Is(err, presets.ErrNotFound):
			msg.Die("%s", err.Error())
		default:
			msg.Die("%s", err.Error())
		}
		return
	}
	for _, l := range lines {
		msg.Success("%s", l)
	}
}

func addCmd() *cobra.Command {
	fields := presetFields{}

	cmd := &cobra.Command{
		Use:   "add <id>",
		Short: "Add a preset to your local catalog",
		Example: `  bossy preset add dmvcframework \
    --repo github.com/danieleteti/delphimvcframework \
    --ref tag:3.4.2-magnesium \
    --searchpath sources --searchpath lib/loggerpro \
    --tag gtr-standard`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			captureChangedFlags(cmd, &fields)
			renderEdit(editAction{mode: modeAdd, id: args[0], fields: fields}.run(Service()))
		},
	}
	bindPresetFlags(cmd, &fields)
	return cmd
}

func updateCmd() *cobra.Command {
	fields := presetFields{}

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Change a preset in your local catalog",
		Long: `Patches only the fields whose flags are given. An id that currently
exists only in the synced catalog is accepted and produces a local override.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			captureChangedFlags(cmd, &fields)
			renderEdit(editAction{mode: modeUpdate, id: args[0], fields: fields}.run(Service()))
		},
	}
	bindPresetFlags(cmd, &fields)
	return cmd
}

func removeCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rm <id>",
		Aliases: []string{"remove", "delete"},
		Short:   "Remove a preset from your local catalog",
		Long: `Removes a local entry. Entries from the synced catalog cannot be
removed here; the next sync would restore them.`,
		Args: cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			renderEdit(editAction{mode: modeRemove, id: args[0]}.run(Service()))
		},
	}
}
