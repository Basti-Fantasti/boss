// Package preset provides the commands that manage the catalog of predefined
// dependencies a project can pick from.
package preset

import (
	"path/filepath"

	"github.com/basti-fantasti/bossy/internal/adapters/secondary/repository"
	"github.com/basti-fantasti/bossy/internal/core/services/presets"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/spf13/cobra"
)

// PresetsDirName is the directory under the bossy home holding the catalogs.
const PresetsDirName = "presets"

// Service builds the presets service against the user's bossy home.
//
// Exported so that `bossy add` composes the same catalog these commands edit,
// rather than growing a second way to reach it.
func Service() *presets.Service {
	return presets.New(repository.NewFilePresetStore(filepath.Join(env.GetBossHome(), PresetsDirName)))
}

// RegisterPresetCommand registers the preset command group.
func RegisterPresetCommand(root *cobra.Command) {
	presetCmd := &cobra.Command{
		Use:   "preset",
		Short: "Manage the catalog of predefined dependencies",
		Long: `Presets are a curated list of libraries a project can pick from, so a
dependency can be added by name instead of by repository URL.

Two catalogs are merged: the shared one pulled by 'bossy preset sync', and a
local one holding your own entries. A local entry always wins on an id
collision, which is how you override a shared entry without editing the shared
catalog.`,
	}

	root.AddCommand(presetCmd)
	presetCmd.AddCommand(listCmd())
	presetCmd.AddCommand(showCmd())
	presetCmd.AddCommand(addCmd())
	presetCmd.AddCommand(updateCmd())
	presetCmd.AddCommand(removeCmd())
	presetCmd.AddCommand(importCmd())
	presetCmd.AddCommand(syncCmd())
}
