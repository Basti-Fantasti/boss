// Package auth resolves the protocol, URL, and credentials bossy uses
// to clone or fetch a dependency. The resolution chain is documented in
// docs/superpowers/specs/2026-05-05-bossy-fork-design.md §2.
package auth

import (
	"fmt"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/pkg/env"
)

// Transport identifies which underlying git client bossy will use.
type Transport int

const (
	// TransportHTTPS clones over HTTPS using the embedded go-git client.
	TransportHTTPS Transport = iota
	// TransportSSH clones over SSH by shelling out to the system git binary.
	TransportSSH
)

// String returns the lowercase name of the transport ("https" or "ssh").
func (t Transport) String() string {
	switch t {
	case TransportSSH:
		return "ssh"
	case TransportHTTPS:
		return "https"
	default:
		return "unknown"
	}
}

// CredentialSpec carries optional HTTPS basic-auth credentials. Empty
// (zero value) means no credentials should be sent. SSH transports do
// not use this — credentials come from ssh-agent / ~/.ssh/config via
// the system git binary.
//
// Password is a live secret: the GitLab CI job token travels here rather
// than in Decision.URL, precisely so it is not persisted into the cache's
// remote URL. Nothing in Decision.URL's place masks it, so the String and
// GoString methods below do that job instead. Read the field directly when
// the value itself is needed; format the struct when it is not.
type CredentialSpec struct {
	User     string
	Password string
}

// String masks the password so a CredentialSpec cannot carry a token into a
// log line, a wrapped error or a test failure message. The user is left
// visible: it is not secret, and it is what identifies the resolution layer
// that produced the credential.
//
// fmt calls this for %v, %+v and %s, on the value itself and on the
// Credential field of a Decision printed with any of them.
func (c CredentialSpec) String() string {
	return "{User:" + c.User + " Password:" + maskSecret(c.Password) + "}"
}

// GoString covers %#v, which ignores String entirely and would otherwise dump
// the struct fields verbatim.
func (c CredentialSpec) GoString() string {
	return fmt.Sprintf("auth.CredentialSpec{User:%q, Password:%q}", c.User, maskSecret(c.Password))
}

// maskSecret renders a non-empty secret as a fixed placeholder. An empty
// password stays empty rather than becoming "***", so "no credential" and
// "credential withheld from the output" remain distinguishable.
func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	return "***"
}

// Decision is the output of Resolve: how bossy should clone the dep.
type Decision struct {
	URL        string
	Transport  Transport
	Credential CredentialSpec
	// Layer is the human-readable name of the resolution layer that
	// produced this decision, used in debug logs and error messages.
	Layer string
}

const defaultBareOrg = "github.com/hashload/"

// Resolve uses the live env.Configuration as the credential/host-config source.
// Most callers should use this. Tests use ResolveWith to inject doubles.
func Resolve(dep domain.Dependency) (Decision, error) {
	cfg := env.GlobalConfiguration()
	return ResolveWith(dep, cfg, cfg.HostProtocols)
}

// ResolveWith is the testable form of Resolve. It walks the precedence
// chain documented in the design spec §2.
func ResolveWith(dep domain.Dependency, store CredentialStore, hostCfg map[string]string) (Decision, error) {
	repo := dep.Repository
	parsed, err := ParseDepURL(repo)
	if err != nil {
		return Decision{}, err
	}

	// Bare names resolve against the default org and become host/path.
	if parsed.Kind == URLKindBare {
		parsed, err = ParseDepURL(defaultBareOrg + parsed.Path)
		if err != nil {
			return Decision{}, err
		}
	}

	// Layer 1: GitLab CI auto-detection.
	if d, ok := tryGitLabCI(parsed); ok {
		return d, nil
	}
	// Layer 2: env-var override.
	if d, ok := tryEnvVar(parsed); ok {
		return d, nil
	}
	// Layer 3: per-dep explicit URL.
	if parsed.Kind == URLKindSSH {
		return Decision{
			URL:       "git@" + parsed.Host + ":" + parsed.Path,
			Transport: TransportSSH,
			Layer:     "explicit",
		}, nil
	}
	if parsed.Kind == URLKindHTTPS {
		// Explicit HTTPS may still want stored creds applied.
		if d, ok := tryStored(parsed, store); ok {
			return d, nil
		}
		return Decision{
			URL:       "https://" + parsed.Host + "/" + parsed.Path,
			Transport: TransportHTTPS,
			Layer:     "explicit",
		}, nil
	}
	// Layer 4: per-host config.
	if d, ok := tryHostConfig(parsed, hostCfg); ok {
		return d, nil
	}
	// Layer 5: stored creds.
	if d, ok := tryStored(parsed, store); ok {
		return d, nil
	}
	// Layer 6: default HTTPS, no auth.
	return Decision{
		URL:       "https://" + parsed.Host + "/" + parsed.Path,
		Transport: TransportHTTPS,
		Layer:     "default",
	}, nil
}
