package domain

import (
	//nolint:gosec // MD5 used for dependency hashing, not security
	"crypto/md5"
	"encoding/hex"
	"io"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/basti-fantasti/bossy/pkg/msg"
)

var (
	reURLPrefix         = regexp.MustCompile(`^[^/^:]+`)
	reVersionMajorMinor = regexp.MustCompile(`(?m)^(.|)(\d+)\.(\d+)$`)
	reVersionMajor      = regexp.MustCompile(`(?m)^(.|)(\d+)$`)
	reDepName           = regexp.MustCompile(`[^/]+(:?/$|$)`)
)

// Dependency represents a package dependency.
type Dependency struct {
	Repository string
	version    string
}

// canonicalRepo returns the canonical "host/path" form of a repository
// reference, used for cache hashing so equivalent declarations
// (git@host:path[.git], host/path, https://host/path[.git]) collapse
// to one cache entry. Falls back to the input on malformed strings.
func canonicalRepo(repo string) string {
	s := strings.TrimSpace(repo)
	s = strings.TrimSuffix(s, ".git")
	if rest, ok := strings.CutPrefix(s, "git@"); ok {
		// git@host:path → host/path
		if idx := strings.Index(rest, ":"); idx > 0 {
			return rest[:idx] + "/" + rest[idx+1:]
		}
	}
	if rest, ok := strings.CutPrefix(s, "https://"); ok {
		return rest
	}
	if rest, ok := strings.CutPrefix(s, "http://"); ok {
		return rest
	}
	return s
}

// HashName returns the MD5 hash of the canonical repository name.
func (p *Dependency) HashName() string {
	//nolint:gosec // We are not using this for security purposes
	hash := md5.New()
	if _, err := io.WriteString(hash, strings.ToLower(canonicalRepo(p.Repository))); err != nil {
		msg.Warn("⚠️ Failed on write dependency hash")
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// GetVersion returns the version of the dependency.
func (p *Dependency) GetVersion() string {
	return p.version
}

// GetURLPrefix returns the provider prefix of the repository URL.
func (p *Dependency) GetURLPrefix() string {
	return reURLPrefix.FindString(p.Repository)
}

// ParseDependency creates a Dependency object from repository string and version info.
func ParseDependency(repo string, info string) Dependency {
	parsed := strings.Split(info, ":")
	dependency := Dependency{}
	dependency.Repository = repo
	dependency.version = parsed[0]
	if reVersionMajorMinor.MatchString(dependency.version) {
		msg.Debug("Current version for %s is not semantic (x.y.z), for comparison using %s -> %s",
			dependency.Repository, dependency.version, dependency.version+".0")
		dependency.version += ".0"
	}
	if reVersionMajor.MatchString(dependency.version) {
		msg.Debug("Current version for %s is not semantic (x.y.z), for comparison using %s -> %s",
			dependency.Repository, dependency.version, dependency.version+".0.0")
		dependency.version += ".0.0"
	}
	return dependency
}

// GetDependencies converts a map of dependencies to a slice of Dependency objects.
func GetDependencies(deps map[string]string) []Dependency {
	dependencies := make([]Dependency, 0)
	for repo, info := range deps {
		dependencies = append(dependencies, ParseDependency(repo, info))
	}
	return dependencies
}

// GetDependenciesNames returns a slice of dependency names.
func GetDependenciesNames(deps []Dependency) []string {
	var dependencies []string
	for _, info := range deps {
		dependencies = append(dependencies, info.Name())
	}
	return dependencies
}

// Name returns the name of the dependency extracted from the repository URL.
func (p *Dependency) Name() string {
	return reDepName.FindString(p.Repository)
}

// GetKey returns the normalized key for the dependency (lowercase repository).
func (p *Dependency) GetKey() string {
	return strings.ToLower(p.Repository)
}

// NeedsVersionUpdate checks if a version update is needed based on semver comparison.
func NeedsVersionUpdate(currentVersion, newVersion string) bool {
	parsedNew, err := semver.NewVersion(newVersion)
	if err != nil {
		return newVersion != currentVersion
	}

	parsedCurrent, err := semver.NewVersion(currentVersion)
	if err != nil {
		return newVersion != currentVersion
	}

	return parsedNew.GreaterThan(parsedCurrent)
}
