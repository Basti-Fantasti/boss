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
		key, ver, ok := normalizeDepArg(raw)
		if !ok {
			continue
		}
		pkg.AddDependency(key, ver)
	}
}

// normalizeDepArg splits a user-supplied dependency argument into a canonical
// dep key (matching how it is stored in bossy.json) and an optional version
// suffix. Returns ok=false if the argument cannot be parsed.
func normalizeDepArg(raw string) (key, version string, ok bool) {
	// Extract version suffix: "dep@version" but skip the leading "git@" of SSH URLs.
	version = consts.MinimalDependencyVersion
	urlPart := raw
	if !strings.HasPrefix(raw, "git@") {
		if at := strings.LastIndex(raw, "@"); at > 0 {
			version = raw[at+1:]
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

	parsed, err := auth.ParseDepURL(toparse)
	if err != nil {
		return "", "", false
	}
	switch parsed.Kind {
	case auth.URLKindSSH:
		key = "git@" + parsed.Host + ":" + parsed.Path
	case auth.URLKindHTTPS, auth.URLKindHostPath, auth.URLKindBare:
		key = parsed.Canonical
	}
	return key, version, true
}

// NormalizeDepKey returns the canonical dep key for a user-supplied argument,
// matching what EnsureDependency would store in bossy.json. The version suffix
// is discarded.
func NormalizeDepKey(raw string) (string, bool) {
	key, _, ok := normalizeDepArg(raw)
	return key, ok
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
