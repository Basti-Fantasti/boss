// Package installer provides utility functions for dependency management.
package installer

import (
	"regexp"
	"strings"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/auth"
	"github.com/basti-fantasti/bossy/pkg/consts"
)

var (
	reHasSlash      = regexp.MustCompile(`(?m)(([?^/]).*)`)
	reHasMultiSlash = regexp.MustCompile(`(?m)([?^/].*)(([?^/]).*)`)
)

// EnsureDependency ensures that the dependencies are added to the package.
func EnsureDependency(pkg *domain.Package, args []string) {
	for _, raw := range args {
		// Extract version suffix: "dep@version" but skip the leading "git@" of SSH URLs.
		ver := consts.MinimalDependencyVersion
		urlPart := raw
		if !strings.HasPrefix(raw, "git@") {
			if at := strings.LastIndex(raw, "@"); at > 0 {
				ver = raw[at+1:]
				urlPart = raw[:at]
			}
		}

		// For SSH and HTTPS URLs, use auth.ParseDepURL directly — they are
		// already fully specified and ParseDependency would mangle them.
		// For everything else, apply the legacy bare-name → default-org expansion first.
		var toparse string
		if strings.HasPrefix(urlPart, "git@") || strings.HasPrefix(urlPart, "http://") || strings.HasPrefix(urlPart, "https://") {
			toparse = urlPart
		} else {
			toparse = ParseDependency(urlPart)
		}

		// Classify the URL form and derive the canonical dep key.
		parsed, err := auth.ParseDepURL(toparse)
		if err != nil {
			continue
		}
		var key string
		switch parsed.Kind {
		case auth.URLKindSSH:
			key = "git@" + parsed.Host + ":" + parsed.Path
		case auth.URLKindHTTPS, auth.URLKindHostPath, auth.URLKindBare:
			key = parsed.Canonical
		}

		pkg.AddDependency(key, ver)
	}
}

// ParseDependency parses the dependency name and returns the full URL if needed.
func ParseDependency(dependencyName string) string {
	if !reHasSlash.MatchString(dependencyName) {
		return "github.com/hashload/" + dependencyName
	}
	if !reHasMultiSlash.MatchString(dependencyName) {
		return "github.com/" + dependencyName
	}
	return dependencyName
}
