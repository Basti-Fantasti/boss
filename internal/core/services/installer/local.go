// Package installer provides local dependency installation.
package installer

import (
	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/basti-fantasti/bossy/utils/dcp"
)

// LocalInstall installs dependencies locally.
func LocalInstall(config env.ConfigProvider, options InstallOptions, pkg *domain.Package) {
	// TODO noSave
	EnsureDependency(pkg, options.Args)
	if err := DoInstall(config, options, pkg); err != nil {
		msg.Die("❌ %s", err)
	}
	dcp.InjectDpcs(pkg, pkg.Lock)
}
