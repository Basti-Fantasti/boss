package auth

import (
	"errors"
	"regexp"
	"strings"
)

// URLKind enumerates the recognized forms of a dependency key.
type URLKind int

const (
	// URLKindBare is a name without a slash, resolved against the default org.
	URLKindBare URLKind = iota
	// URLKindHostPath is the canonical "host/path" form (the upstream default).
	URLKindHostPath
	// URLKindSSH is the explicit "git@host:path[.git]" SSH form.
	URLKindSSH
	// URLKindHTTPS is an explicit "http(s)://host/path[.git]" URL.
	URLKindHTTPS
)

// ParsedURL is the result of classifying a dependency key.
type ParsedURL struct {
	Kind      URLKind
	Host      string // empty for URLKindBare
	Path      string // owner/repo or just a name (bare)
	Canonical string // host/path or bare name; used for cache hashing
}

var (
	reGitAt = regexp.MustCompile(`^git@([^:]+):(.+?)(?:\.git)?$`)
	reHTTP  = regexp.MustCompile(`^https?://([^/]+)/(.+?)(?:\.git)?$`)
)

// ParseDepURL classifies a dependency key into one of the recognized forms.
// It returns an error if the input is empty or syntactically malformed.
func ParseDepURL(s string) (ParsedURL, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ParsedURL{}, errors.New("empty dependency URL")
	}

	if m := reGitAt.FindStringSubmatch(s); m != nil {
		host, path := m[1], m[2]
		if host == "" || path == "" {
			return ParsedURL{}, errors.New("invalid SSH URL: missing host or path")
		}
		return ParsedURL{
			Kind:      URLKindSSH,
			Host:      host,
			Path:      path,
			Canonical: host + "/" + path,
		}, nil
	}

	if m := reHTTP.FindStringSubmatch(s); m != nil {
		host, path := m[1], m[2]
		if host == "" || path == "" {
			return ParsedURL{}, errors.New("invalid HTTPS URL: missing host or path")
		}
		return ParsedURL{
			Kind:      URLKindHTTPS,
			Host:      host,
			Path:      path,
			Canonical: host + "/" + path,
		}, nil
	}

	if strings.HasPrefix(s, "git@") {
		return ParsedURL{}, errors.New("invalid SSH URL: expected git@host:path form")
	}

	// Reject any remaining scheme-bearing input (e.g. "https://") that did
	// not match the http regex — avoids misclassifying it as host/path.
	if strings.Contains(s, "://") {
		return ParsedURL{}, errors.New("invalid URL: scheme present but malformed")
	}

	if strings.Contains(s, "/") {
		idx := strings.Index(s, "/")
		host := s[:idx]
		path := strings.TrimSuffix(s[idx+1:], ".git")
		if path == "" {
			return ParsedURL{}, errors.New("invalid host/path: missing path")
		}
		return ParsedURL{
			Kind:      URLKindHostPath,
			Host:      host,
			Path:      path,
			Canonical: host + "/" + path,
		}, nil
	}

	return ParsedURL{
		Kind:      URLKindBare,
		Path:      s,
		Canonical: s,
	}, nil
}
