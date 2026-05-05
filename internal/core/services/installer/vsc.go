// Package installer provides version control system integration.
package installer

import (
	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/pkg/env"
)

// getConfigProvider returns the global configuration provider.
func getConfigProvider() env.ConfigProvider {
	return env.GlobalConfiguration()
}

// GetDependency fetches or updates a dependency in cache.
// Deprecated: Use DependencyManager directly for better testability.
func GetDependency(dep domain.Dependency) error {
	return NewDefaultDependencyManager(getConfigProvider()).GetDependency(dep)
}

// GetDependencyWithProgress fetches or updates a dependency with optional progress tracking.
// Deprecated: Use DependencyManager directly for better testability.
func GetDependencyWithProgress(dep domain.Dependency, progress *ProgressTracker) error {
	return NewDefaultDependencyManager(getConfigProvider()).GetDependencyWithProgress(dep, progress)
}
