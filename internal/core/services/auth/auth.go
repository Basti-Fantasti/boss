// Package auth resolves the protocol, URL, and credentials bossy uses
// to clone or fetch a dependency. The resolution chain is documented in
// docs/superpowers/specs/2026-05-05-bossy-fork-design.md §2.
package auth

import "github.com/basti-fantasti/bossy/internal/core/domain"

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
type CredentialSpec struct {
	User     string
	Password string
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

// Resolve walks the precedence chain and returns the Decision for the
// given dependency. Implementation is filled in by later tasks; this
// stub exists so call sites can be wired now.
func Resolve(dep domain.Dependency) (Decision, error) {
	return Decision{}, nil
}
