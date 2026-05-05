// Package consts — branding holds fork-identifying constants.
// Consolidated here to minimize the merge-conflict surface when
// cherry-picking changes from upstream HashLoad/boss.
package consts

const (
	// BinaryName is the user-facing name of the CLI binary.
	BinaryName = "bossy"

	// GithubOrganization is the GitHub org/user that hosts the fork's releases.
	GithubOrganization = "Basti-Fantasti"

	// GithubRepository is the fork's GitHub repository name (releases live here).
	GithubRepository = "bossy"

	// ReleaseAssetPrefix is the prefix for binary release artifact names.
	// Asset filenames are formatted as: <prefix>-<os>-<arch>.<ext>.
	ReleaseAssetPrefix = "bossy"

	// UserHomeDir is the per-user config directory under the user's home.
	// Migration from the legacy ".boss" directory happens on first run.
	UserHomeDir = ".bossy"

	// LegacyUserHomeDir is the upstream's per-user config directory.
	// Used only by the migrator to detect a one-time migration source.
	LegacyUserHomeDir = ".boss"
)
