package preset

import (
	"fmt"
	"strings"

	gitadapter "github.com/basti-fantasti/bossy/internal/adapters/secondary/git"
	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/presets"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/spf13/cobra"
)

// gitCloner adapts the git adapter to the presets service's Cloner, which is
// what keeps the service free of adapter imports.
func gitCloner(repoKey, destDir string) error {
	return gitadapter.CloneInto(domain.Dependency{Repository: repoKey}, destDir)
}

// syncAction is the testable core of `preset sync`.
type syncAction struct {
	source string
	clone  presets.Cloner
	// cfg is the configuration that remembers the source between runs.
	cfg *env.Configuration
}

func (a syncAction) run(svc *presets.Service) ([]string, error) {
	source := strings.TrimSpace(a.source)
	if source == "" {
		source = a.cfg.PresetSource
	}

	catalog, err := svc.Sync(source, a.clone)
	if err != nil {
		return nil, err
	}

	// Remembered only after a successful sync, so a typo does not become the
	// stored default.
	if a.cfg.PresetSource != source {
		a.cfg.PresetSource = source
		a.cfg.SaveConfiguration()
	}

	return []string{fmt.Sprintf("✅ synced %d preset(s) from %s", len(catalog.Presets), source)}, nil
}

func syncCmd() *cobra.Command {
	var source string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Pull the shared catalog from its git repository",
		Long: `Clones the catalog repository, reads presets.json from its root and
replaces the synced catalog. Your local catalog is untouched.

The repository is remembered after the first successful sync, so later runs
need no --source.`,
		Example: `  bossy preset sync --source gtr:delphi/libraries/bossy-presets
  bossy preset sync`,
		Args: cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			action := syncAction{source: source, clone: gitCloner, cfg: env.GlobalConfiguration()}
			lines, err := action.run(Service())
			if err != nil {
				msg.Die("%s", err.Error())
				return
			}
			for _, l := range lines {
				msg.Success("%s", l)
			}
		},
	}
	cmd.Flags().StringVar(&source, "source", "",
		"catalog repository key, e.g. gtr:delphi/libraries/bossy-presets (remembered after the first sync)")
	return cmd
}
