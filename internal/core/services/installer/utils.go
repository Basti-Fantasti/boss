// Package installer provides utility functions for dependency management.
package installer

import (
	"regexp"
	"strings"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/auth"
	"github.com/basti-fantasti/bossy/pkg/consts"
	"github.com/basti-fantasti/bossy/pkg/env"
)

var (
	reHasSlash      = regexp.MustCompile(`(?m)(([?^/]).*)`)
	reHasMultiSlash = regexp.MustCompile(`(?m)([?^/].*)(([?^/]).*)`)
)

// EnsureDependency ensures that the dependencies are added to the package.
//
// An argument that carries no explicit "@version" must not overwrite a version
// already declared in bossy.json: `bossy update <dep>` names the dependency to
// update, it does not redeclare it. Only a genuinely new dependency picks up
// the minimal range, which is what `bossy install <newdep>` relies on.
func EnsureDependency(pkg *domain.Package, args []string) {
	for _, raw := range args {
		key, ver, explicit, ok := normalizeDepArg(raw)
		if !ok {
			continue
		}
		if !explicit {
			if hasDependency(pkg, key) {
				continue
			}
			ver = consts.MinimalDependencyVersion
		}
		pkg.AddDependency(key, ver)
	}
}

// hasDependency reports whether pkg already declares key, using the same
// case-insensitive comparison as Package.AddDependency so the two cannot
// disagree about whether an entry exists.
func hasDependency(pkg *domain.Package, key string) bool {
	for existing := range pkg.Dependencies {
		if strings.EqualFold(existing, key) {
			return true
		}
	}
	return false
}

// normalizeDepArg splits a user-supplied dependency argument into a canonical
// dep key (matching how it is stored in bossy.json) and its version suffix.
//
// explicit reports whether the argument actually carried an "@version"; when it
// is false, version is empty and callers must decide what a missing version
// means for them rather than being handed a silent default. Returns ok=false if
// the argument cannot be parsed.
func normalizeDepArg(raw string) (key, version string, explicit, ok bool) {
	// Defensive: reject alias-shaped prefixes ("name:...") where "name" matches
	// the alias-name pattern but is not actually a configured alias. Without
	// this guard, "unknown:foo/bar" would fall through to ParseDependency and
	// be misclassified. The check runs against raw input (before the version
	// suffix splitter) so "unknown:foo/bar@1.2.0" is rejected too.
	if colon := strings.Index(raw, ":"); colon > 0 && !strings.HasPrefix(raw, "git@") &&
		!strings.HasPrefix(raw[colon+1:], "//") &&
		!strings.Contains(raw[:colon], "/") && !strings.Contains(raw[:colon], ".") {
		prefix := raw[:colon]
		if consts.AliasNamePattern.MatchString(prefix) {
			if _, aliased := env.GlobalConfiguration().Aliases[prefix]; !aliased {
				return "", "", false, false
			}
		}
	}

	// Extract version suffix: "dep@version" but skip the leading "git@" of SSH URLs.
	// A trailing bare "@" is still stripped from the URL part, as it always was,
	// but carries no version and so does not count as explicit.
	urlPart := raw
	if !strings.HasPrefix(raw, "git@") {
		if at := strings.LastIndex(raw, "@"); at > 0 {
			version = raw[at+1:]
			explicit = version != ""
			urlPart = raw[:at]
		}
	}

	// Expand "<alias>:<path>" into the canonical SSH or host/path form before
	// the URL-prefix branch below, so an alias-prefixed argument is not
	// misclassified as a bare name or owner/repo.
	if expanded, expandedOK := tryExpandAlias(urlPart); expandedOK {
		urlPart = expanded
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
		return "", "", false, false
	}
	switch parsed.Kind {
	case auth.URLKindSSH:
		key = "git@" + parsed.Host + ":" + parsed.Path
	case auth.URLKindHTTPS, auth.URLKindHostPath, auth.URLKindBare:
		key = parsed.Canonical
	}
	return key, version, explicit, true
}

// NormalizeDepKey returns the canonical dep key for a user-supplied argument,
// matching what EnsureDependency would store in bossy.json. The version suffix
// is discarded.
func NormalizeDepKey(raw string) (string, bool) {
	key, _, _, ok := normalizeDepArg(raw)
	return key, ok
}

// tryExpandAlias rewrites "<alias>:<path>" into the canonical SSH or
// host/path form based on the user-config alias table and the host's
// configured protocol. Returns (expanded, true) on hit, ("", false)
// otherwise — including when the prefix is "git" / "http" / "https"
// (already a real URL form).
func tryExpandAlias(s string) (string, bool) {
	colon := strings.Index(s, ":")
	if colon <= 0 {
		return "", false
	}
	name := s[:colon]
	if !consts.AliasNamePattern.MatchString(name) {
		return "", false
	}
	// Reject real URL forms: "scheme://host/path" — the alias-name pattern
	// happily matches "http"/"https", so guard explicitly on the "//" that
	// follows the colon in a URL.
	if strings.HasPrefix(s[colon+1:], "//") {
		return "", false
	}
	cfg := env.GlobalConfiguration()
	host, ok := cfg.Aliases[name]
	if !ok {
		return "", false
	}
	path := strings.TrimSuffix(s[colon+1:], ".git")
	if path == "" {
		return "", false
	}
	if cfg.HostProtocols[host] == "ssh" {
		return "git@" + host + ":" + path, true
	}
	return host + "/" + path, true
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
