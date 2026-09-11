package gitadapter

import (
	"fmt"
	"os/exec"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/auth"
	"github.com/basti-fantasti/bossy/pkg/msg"
	goGit "github.com/go-git/go-git/v5"
)

// CloneInto makes a shallow clone of a repository into an arbitrary directory.
//
// The CloneCache family cannot serve this: they clone a *dependency* into the
// per-user cache keyed on its hash, wire up a separate git dir and initialise
// submodules. The preset catalog is not a dependency — it is one small JSON
// file read once and thrown away — so it gets a plain checkout in a temporary
// directory instead of an entry in the dependency cache.
//
// Depth is always 1. Nothing reads the catalog repository's history, and the
// single-branch refspec restriction that makes shallow clones unsafe for
// dependencies is harmless here because only the default branch is ever read.
func CloneInto(dep domain.Dependency, destDir string) error {
	decision, err := auth.Resolve(dep)
	if err != nil {
		return fmt.Errorf("resolve auth for %s: %w", dep.Repository, err)
	}

	if decision.Transport == auth.TransportSSH {
		return cloneIntoNative(dep, decision, destDir)
	}
	return cloneIntoEmbedded(dep, decision, destDir)
}

// cloneIntoNative shells out to the system git binary, which is what makes
// ~/.ssh/config aliases, custom keys and non-standard ports work.
func cloneIntoNative(dep domain.Dependency, decision auth.Decision, destDir string) error {
	if err := requireGit(dep, hostFromURL(decision.URL)); err != nil {
		return err
	}

	args := []string{"clone", "--depth", "1", decision.URL, destDir}
	// runCommand applies gitSafeEnv and redacts credentials out of the output.
	cmd := exec.Command("git", args...) // #nosec G204 -- URL resolved by auth.Resolve, destination created by the caller

	if err := runCommand(cmd); err != nil {
		return fmt.Errorf("clone %s: %w", dep.Repository, err)
	}
	return nil
}

// cloneIntoEmbedded clones over HTTP(S) with go-git.
func cloneIntoEmbedded(dep domain.Dependency, decision auth.Decision, destDir string) error {
	msg.Debug("Cloning %s into %s", dep.Repository, destDir)

	_, err := goGit.PlainClone(destDir, false, &goGit.CloneOptions{
		URL:          decision.URL,
		Auth:         httpsAuth(decision),
		Depth:        1,
		SingleBranch: true,
	})
	if err != nil {
		return fmt.Errorf("clone %s: %w", dep.Repository, err)
	}
	return nil
}
