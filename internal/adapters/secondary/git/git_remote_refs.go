package gitadapter

import (
	"fmt"
	"strings"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/auth"
	goGit "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Ref namespaces a dependency may be pinned to.
const (
	refsHeadsPrefix = "refs/heads/"
	refsTagsPrefix  = "refs/tags/"
)

// ListRemoteRefs enumerates a dependency's remote branches and tags without
// cloning it.
//
// GetVersions cannot serve this. It is clone-free for SSH dependencies, which
// go through ls-remote, but routes HTTPS dependencies through
// getVersionsEmbedded, which fetches into an existing *git.Repository. The
// dependency picker offers a ref list for whichever entry the cursor is on, and
// cloning whole repositories to render a list is not a workable trade.
//
// A listing failure is returned, never reported as an empty slice: the caller
// cannot tell "no refs" from "could not reach the host", and would fall back to
// the main branch — the silent wrong-branch bug removed in dda87ee.
func ListRemoteRefs(dep domain.Dependency) ([]*plumbing.Reference, error) {
	decision, err := auth.Resolve(dep)
	if err != nil {
		return nil, fmt.Errorf("resolve auth for %s: %w", dep.Repository, err)
	}

	if decision.Transport == auth.TransportSSH {
		return ListRefsNative(dep, decision)
	}

	return listRemoteRefsEmbedded(dep, decision)
}

// listRemoteRefsEmbedded lists refs over HTTP(S) using go-git against an
// in-memory storer, so nothing is written to disk.
//
// The result is filtered to the same refs/heads + refs/tags allowlist
// parseLsRemote applies, so both backends return the same shape. Without it the
// server-advertised namespaces (refs/pull/*, refs/merge-requests/*,
// refs/keep-around/*) would be offered as pins, and every one of them satisfies
// the installer's raw-SHA sentinel.
func listRemoteRefsEmbedded(dep domain.Dependency, decision auth.Decision) ([]*plumbing.Reference, error) {
	remote := goGit.NewRemote(memory.NewStorage(), &gitConfig.RemoteConfig{
		Name: goGit.DefaultRemoteName,
		URLs: []string{decision.URL},
	})

	refs, err := remote.List(&goGit.ListOptions{Auth: httpsAuth(decision)})
	if err != nil {
		// A repository with no commits advertises nothing. That is an honest
		// empty result, not a failure to reach the host.
		if isEmptyRemote(err) {
			return []*plumbing.Reference{}, nil
		}
		return nil, fmt.Errorf("list refs for %s: %w", dep.Repository, err)
	}

	out := make([]*plumbing.Reference, 0, len(refs))
	for _, ref := range refs {
		if !isBranchOrTagRef(ref.Name().String()) {
			continue
		}
		out = append(out, ref)
	}

	return out, nil
}

// isBranchOrTagRef reports whether a full ref name is a real branch or tag.
//
// Peeled annotated-tag entries ("refs/tags/x^{}") are excluded: the tag object
// itself is the ref bossy resolves against.
func isBranchOrTagRef(name string) bool {
	if strings.HasSuffix(name, "^{}") {
		return false
	}
	return strings.HasPrefix(name, refsHeadsPrefix) || strings.HasPrefix(name, refsTagsPrefix)
}

// isEmptyRemote reports whether a listing error means the remote simply has no
// refs yet.
func isEmptyRemote(err error) bool {
	return err == transport.ErrEmptyRemoteRepository || //nolint:errorlint // go-git compares this sentinel by identity
		strings.Contains(err.Error(), transport.ErrEmptyRemoteRepository.Error())
}
