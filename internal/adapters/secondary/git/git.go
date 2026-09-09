// Package gitadapter provides Git operations for cloning and updating dependency repositories.
// It supports both embedded (go-git) and native Git implementations.
package gitadapter

import (
	"fmt"
	"path/filepath"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/auth"
	"github.com/basti-fantasti/bossy/pkg/consts"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/go-git/go-billy/v5/osfs"
	goGit "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
)

// CloneCache clones the dependency repository to the cache.
// The config parameter is retained for upstream cherry-pick stability but is unused;
// transport selection is driven entirely by auth.Resolve.
func CloneCache(_ env.ConfigProvider, dep domain.Dependency) (*goGit.Repository, error) {
	decision, err := auth.Resolve(dep)
	if err != nil {
		return nil, err
	}
	if decision.Transport == auth.TransportSSH {
		return CloneCacheNative(dep, decision)
	}
	return CloneCacheEmbedded(dep, decision)
}

// UpdateCache updates the dependency repository in the cache.
// The config parameter is retained for upstream cherry-pick stability but is unused;
// transport selection is driven entirely by auth.Resolve.
func UpdateCache(_ env.ConfigProvider, dep domain.Dependency) (*goGit.Repository, error) {
	decision, err := auth.Resolve(dep)
	if err != nil {
		return nil, err
	}
	if decision.Transport == auth.TransportSSH {
		return UpdateCacheNative(dep, decision)
	}
	return UpdateCacheEmbedded(dep, decision)
}

func initSubmodules(_ domain.Dependency, decision auth.Decision, repository *goGit.Repository) error {
	worktree, err := repository.Worktree()
	if err != nil {
		return err
	}
	submodules, err := worktree.Submodules()
	if err != nil {
		return err
	}

	err = submodules.Update(&goGit.SubmoduleUpdateOptions{
		Init:              true,
		RecurseSubmodules: goGit.DefaultSubmoduleRecursionDepth,
		Auth:              httpsAuth(decision),
	})
	if err != nil {
		return err
	}
	return nil
}

// GetMain returns the main branch of the repository.
func GetMain(repository *goGit.Repository) (*gitConfig.Branch, error) {
	branch, err := repository.Branch(consts.GitBranchMain)
	if err != nil {
		branch, err = repository.Branch(consts.GitBranchMaster)
	}
	return branch, err
}

// GetVersions returns all versions (tags and branches) of the repository.
// SSH dependencies are listed through the system git binary; HTTPS
// dependencies keep the embedded go-git path.
//
// A listing failure is returned, never swallowed: an empty result is
// indistinguishable from "no matching version" to the caller, which would
// silently fall back to the main branch.
func GetVersions(
	cfg env.ConfigProvider,
	repository *goGit.Repository,
	dep domain.Dependency,
) ([]*plumbing.Reference, error) {
	decision, err := auth.Resolve(dep)
	if err != nil {
		return nil, fmt.Errorf("resolve auth for %s: %w", dep.Repository, err)
	}

	if decision.Transport == auth.TransportSSH {
		return ListRefsNative(dep, decision)
	}

	return getVersionsEmbedded(cfg, repository, dep, decision), nil
}

// getVersionsEmbedded is the go-git ref listing used for HTTPS dependencies.
// It mirrors the remote's refs into the cache so that branches appear under
// refs/heads/* and are visible to repository.Branches().
func getVersionsEmbedded(
	_ env.ConfigProvider,
	repository *goGit.Repository,
	dep domain.Dependency,
	decision auth.Decision,
) []*plumbing.Reference {
	var result = make([]*plumbing.Reference, 0)

	err := repository.Fetch(&goGit.FetchOptions{
		Force: true,
		Prune: true,
		Auth:  httpsAuth(decision),
		RefSpecs: []gitConfig.RefSpec{
			"refs/*:refs/*",
			"HEAD:refs/heads/HEAD",
		},
	})

	if err != nil {
		msg.Warn("⚠️ Fail to fetch repository %s: %s", dep.Repository, err)
	}

	tags, err := repository.Tags()
	if err != nil {
		msg.Err("❌ Fail to retrieve versions: %v", err)
	} else {
		err = tags.ForEach(func(reference *plumbing.Reference) error {
			result = append(result, reference)
			return nil
		})
		if err != nil {
			msg.Err("❌ Fail to retrieve versions: %v", err)
		}
	}

	branches, err := repository.Branches()
	if err != nil {
		msg.Err("❌ Fail to retrieve branches: %v", err)
	} else {
		err = branches.ForEach(func(reference *plumbing.Reference) error {
			result = append(result, reference)
			return nil
		})
		if err != nil {
			msg.Err("❌ Fail to retrieve branches: %v", err)
		}
	}

	return result
}

func GetTagsShortName(repository *goGit.Repository) []string {
	tags, _ := repository.Tags()
	var result = []string{}
	_ = tags.ForEach(func(reference *plumbing.Reference) error {
		result = append(result, reference.Name().Short())
		return nil
	})
	return result
}

func GetByTag(repository *goGit.Repository, shortName string) *plumbing.Reference {
	tags, _ := repository.Tags()

	for {
		if reference, err := tags.Next(); err == nil {
			if reference.Name().Short() == shortName {
				return reference
			}
		} else {
			return nil
		}
	}
}

func GetRepository(dep domain.Dependency) *goGit.Repository {
	// GetRepository is used in places where we already have a cloned repo
	// So we don't need config for EnsureCacheDir check
	cache := makeStorageCacheWithoutEnsure(dep)
	dir := osfs.New(filepath.Join(env.GetModulesDir(), dep.Name()))
	repository, err := goGit.Open(cache, dir)
	if err != nil {
		msg.Err("❌ Error on open repository %s: %s", dep.Repository, err)
	}

	return repository
}

func Checkout(_ env.ConfigProvider, dep domain.Dependency, referenceName plumbing.ReferenceName) error {
	decision, err := auth.Resolve(dep)
	if err != nil {
		return err
	}
	if decision.Transport == auth.TransportSSH {
		return CheckoutNative(dep, decision, referenceName)
	}
	return CheckoutEmbedded(dep, referenceName)
}

// CheckoutHash checks out a specific commit SHA in detached-HEAD state.
// Returns an error if the SHA is not present locally; callers should
// trigger a deepening fetch and retry on "object not found".
func CheckoutHash(_ env.ConfigProvider, dep domain.Dependency, hash plumbing.Hash) error {
	decision, err := auth.Resolve(dep)
	if err != nil {
		return err
	}
	if decision.Transport == auth.TransportSSH {
		return CheckoutHashNative(dep, decision, hash)
	}
	return CheckoutHashEmbedded(dep, hash)
}

// UnshallowFetch performs a deepening fetch on the dependency repository so that
// commit history previously omitted by a shallow clone becomes available. It is
// the recovery path used when a pinned SHA cannot be checked out because the
// object is missing locally. The fetch is a no-op on a complete repository.
func UnshallowFetch(_ env.ConfigProvider, dep domain.Dependency) error {
	decision, err := auth.Resolve(dep)
	if err != nil {
		return err
	}
	if decision.Transport == auth.TransportSSH {
		return UnshallowFetchNative(dep, decision)
	}
	return UnshallowFetchEmbedded(dep, decision)
}

func Pull(_ env.ConfigProvider, dep domain.Dependency) error {
	decision, err := auth.Resolve(dep)
	if err != nil {
		return err
	}
	if decision.Transport == auth.TransportSSH {
		return PullNative(dep, decision)
	}
	return PullEmbedded(dep, decision)
}
