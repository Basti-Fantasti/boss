package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/basti-fantasti/bossy/internal/adapters/primary/cli/preset"
	"github.com/basti-fantasti/bossy/internal/adapters/primary/tui"
	gitadapter "github.com/basti-fantasti/bossy/internal/adapters/secondary/git"
	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/installer"
	"github.com/basti-fantasti/bossy/internal/core/services/presets"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/basti-fantasti/bossy/pkg/pkgmanager"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// errNoTTY is returned when the picker was asked for without a terminal.
var errNoTTY = errors.New("bossy add needs an interactive terminal")

// addAction is the testable core of `bossy add`.
type addAction struct {
	ids []string
	tag string
	// interactive reports whether a terminal is attached. Injected so the
	// non-interactive refusal can be tested.
	interactive func() bool
	// pick opens the picker. Injected for the same reason.
	pick func(catalog []domain.Preset) ([]tui.Selection, error)
}

// resolve works out which dependencies to add.
func (a addAction) resolve(svc *presets.Service) ([]tui.Selection, error) {
	if len(a.ids) > 0 {
		return resolveByIDs(svc, a.ids)
	}
	if a.tag != "" {
		return resolveByTag(svc, a.tag)
	}

	// Without ids or a tag the picker is the only way to choose, and a picker
	// without a terminal blocks forever on a keystroke that never arrives —
	// in CI a hung job rather than a failed one.
	if !a.interactive() {
		return nil, fmt.Errorf(
			"%w; on a non-interactive shell pass preset ids or --tag <tag> instead", errNoTTY)
	}

	catalog, err := svc.All()
	if err != nil {
		return nil, err
	}
	if len(catalog) == 0 {
		return nil, errors.New(
			"the preset catalog is empty; run 'bossy preset sync' or add an entry with 'bossy preset add'")
	}

	pick := a.pick
	if pick == nil {
		pick = runPicker
	}
	return pick(catalog)
}

// runPicker opens the TUI wired to clone-free ref listing.
func runPicker(catalog []domain.Preset) ([]tui.Selection, error) {
	lister := func(p domain.Preset) ([]*plumbing.Reference, error) {
		return gitadapter.ListRemoteRefs(domain.Dependency{Repository: p.Repo})
	}
	return tui.Run(catalog, lister)
}

// resolveByIDs turns explicit preset ids into selections carrying each
// preset's catalog default reference.
func resolveByIDs(svc *presets.Service, ids []string) ([]tui.Selection, error) {
	selections := make([]tui.Selection, 0, len(ids))
	for _, id := range ids {
		found, ok, err := svc.ByID(id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, unknownPresetError(id)
		}
		selections = append(selections, tui.Selection{Preset: found, Ref: found.DefaultRef})
	}
	return sortSelections(selections), nil
}

// unknownPresetError explains an id the catalog does not carry.
//
// `add` used to be an alias for `install`, so an argument that looks like a
// repository key is most likely an old habit rather than a typo. Saying so
// beats telling the user to go read the catalog.
func unknownPresetError(id string) error {
	if looksLikeRepositoryKey(id) {
		return fmt.Errorf(
			"%q looks like a repository, not a preset id; use 'bossy install %s' "+
				"(bossy add takes preset ids — see 'bossy preset list')", id, id)
	}
	return fmt.Errorf("no preset named %q; run 'bossy preset list' to see what is available", id)
}

// looksLikeRepositoryKey reports whether an argument is a repository reference
// rather than a catalog id. Preset ids never carry a slash or a colon.
func looksLikeRepositoryKey(s string) bool {
	return strings.ContainsAny(s, "/:")
}

// resolveByTag turns a tag into selections.
//
// A tag matching nothing is an error rather than an empty install: it is
// almost always a typo, and silently doing nothing hides that.
func resolveByTag(svc *presets.Service, tag string) ([]tui.Selection, error) {
	matched, err := svc.ByTag(tag)
	if err != nil {
		return nil, err
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("no preset carries the tag %q; run 'bossy preset list' to see what is available", tag)
	}

	selections := make([]tui.Selection, 0, len(matched))
	for _, p := range matched {
		selections = append(selections, tui.Selection{Preset: p, Ref: p.DefaultRef})
	}
	return sortSelections(selections), nil
}

// sortSelections orders by preset id so a manifest is written the same way on
// every run and diffs stay readable.
func sortSelections(selections []tui.Selection) []tui.Selection {
	sort.Slice(selections, func(i, j int) bool {
		return strings.ToLower(selections[i].Preset.ID) < strings.ToLower(selections[j].Preset.ID)
	})
	return selections
}

// applyToManifest merges each preset's search-path restriction into bossy.json.
//
// This is the part the installer cannot do on its own: most Delphi libraries
// ship no bossy.json, so without the catalog bossy would walk the whole
// dependency tree and put every directory holding source on the IDE search
// path. A preset with no search paths writes no restriction — an empty list
// would restrict the dependency to nothing rather than leaving it unrestricted.
func applyToManifest(pkg *domain.Package, selections []tui.Selection) {
	for _, sel := range selections {
		if len(sel.Preset.SearchPaths) == 0 {
			continue
		}
		if pkg.SearchPaths == nil {
			pkg.SearchPaths = map[string][]string{}
		}
		pkg.SearchPaths[sel.Preset.Repo] = sel.Preset.SearchPaths
	}
}

// installArgs renders the selections as `<repo>@<version>` arguments for the
// existing installer, so catalog-driven installs take the same code path as
// `bossy install <pkg>@<version>` rather than a second one.
func installArgs(selections []tui.Selection) []string {
	args := make([]string, 0, len(selections))
	for _, sel := range selections {
		args = append(args, sel.Preset.Repo+"@"+domain.VersionForRef(sel.Ref))
	}
	return args
}

// warnUnverified reports selections whose refs could not be listed.
func warnUnverified(selections []tui.Selection) {
	for _, sel := range selections {
		if sel.Unverified {
			msg.Warn("⚠️ %s: refs could not be listed, installing the catalog default %s unverified",
				sel.Preset.ID, sel.Ref.String())
		}
	}
}

// stdoutIsTerminal reports whether stdout is an interactive terminal.
func stdoutIsTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

// runAdd resolves the selections, writes the manifest and installs.
func runAdd(action addAction, compiler, platform string, strict bool) {
	selections, err := action.resolve(preset.Service())
	if err != nil {
		if errors.Is(err, tui.ErrCancelled) {
			msg.Info("Nothing added.")
			return
		}
		msg.Die("%s", err.Error())
		return
	}
	if len(selections) == 0 {
		msg.Info("Nothing selected.")
		return
	}

	pkg, err := pkgmanager.LoadPackage()
	if err != nil {
		msg.Die("❌ Failed to load %s: %s", "bossy.json", err.Error())
		return
	}

	applyToManifest(pkg, selections)
	if err := pkgmanager.SavePackageCurrent(pkg); err != nil {
		msg.Die("❌ Failed to save bossy.json: %s", err.Error())
		return
	}

	warnUnverified(selections)

	installer.InstallModules(installer.InstallOptions{
		Args:          installArgs(selections),
		LockedVersion: true,
		Compiler:      compiler,
		Platform:      platform,
		Strict:        strict,
	})
}

// addCmdRegister registers the add command.
func addCmdRegister(root *cobra.Command) {
	var tag string
	var compilerVersion string
	var platform string
	var strict bool

	cmd := &cobra.Command{
		Use:   "add [id...]",
		Short: "Add dependencies from the preset catalog",
		Long: `Opens an interactive picker listing the dependencies in your preset
catalog, and writes the ones you choose into bossy.json together with their
search-path restrictions, then installs them.

Pass preset ids or --tag to skip the picker, which is what a non-interactive
shell must do.`,
		Example: `  Pick interactively:
  bossy add

  Add a whole tagged baseline:
  bossy add --tag gtr-standard

  Add named presets:
  bossy add dmvcframework zeoslib`,
		Run: func(_ *cobra.Command, args []string) {
			action := addAction{ids: args, tag: tag, interactive: stdoutIsTerminal}
			runAdd(action, compilerVersion, platform, strict)
		},
	}

	root.AddCommand(cmd)
	cmd.Flags().StringVar(&tag, "tag", "", "add every preset carrying this tag, without the picker")
	cmd.Flags().StringVar(&compilerVersion, "compiler", "", "compiler version to use")
	cmd.Flags().StringVar(&platform, "platform", "", "platform to use (e.g., Win32, Win64)")
	cmd.Flags().BoolVar(&strict, "strict", false, "strict mode for compiler selection")
}
