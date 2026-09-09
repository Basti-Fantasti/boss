// Package gitadapter provides embedded Git operations using go-git library.
// This file implements Git clone/update operations without requiring native Git installation.
package gitadapter

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/auth"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/go-git/go-billy/v5/memfs"
	gogitosfs "github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	cache2 "github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/transport"
	httpAuth "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// httpsAuth converts a Decision's credential into a go-git auth method.
// Returns nil when no credentials are needed.
func httpsAuth(d auth.Decision) transport.AuthMethod {
	if d.Credential.User == "" && d.Credential.Password == "" {
		return nil
	}
	return &httpAuth.BasicAuth{Username: d.Credential.User, Password: d.Credential.Password}
}

// scrubTokenFromURL removes embedded basic-auth credentials from an HTTP(S)
// URL. Older bossy versions persisted the GitLab CI job token into the cache's
// remote URL; this clears that secret from disk. Non-HTTP inputs and URLs
// without credentials are returned unchanged.
//
// Distinct from redactURLCredentials in git_native.go: that one masks
// credentials in free-form log and error text and must cope with the
// scheme-relative fragments git prints, so it is regex-based and lossy. This
// one rewrites a single well-formed URL that is about to be written back to
// disk, so it parses.
func scrubTokenFromURL(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "http") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

// scrubPersistedRemoteURLs clears any credential an older bossy baked into the
// cache's persisted remote URLs. Best effort: a cache whose config cannot be
// rewritten still fetches correctly, because every fetch supplies RemoteURL
// explicitly and so never reads the stored URL.
func scrubPersistedRemoteURLs(dep domain.Dependency, repository *git.Repository) {
	cfg, err := repository.Config()
	if err != nil {
		// Same posture as the SetConfig failure below: nothing the user can act
		// on, since a cache whose config cannot be read still fetches, but it
		// should not vanish without trace either.
		msg.Debug("Could not read cached config for %s: %s", dep.Repository, err)
		return
	}
	changed := false
	for _, remote := range cfg.Remotes {
		for i, u := range remote.URLs {
			if scrubbed := scrubTokenFromURL(u); scrubbed != u {
				remote.URLs[i] = scrubbed
				changed = true
			}
		}
	}
	if !changed {
		return
	}
	if err := repository.SetConfig(cfg); err != nil {
		msg.Debug("Could not scrub cached remote URL for %s: %s", dep.Repository, err)
	}
}

// CloneCacheEmbedded clones the dependency repository to the cache using the embedded git implementation.
func CloneCacheEmbedded(dep domain.Dependency, decision auth.Decision) (*git.Repository, error) {
	msg.Info("📥 Downloading dependency %s", dep.Repository)
	storageCache := makeStorageCacheNoCfg(dep)
	worktreeFileSystem := memfs.New()

	cloneOpts := &git.CloneOptions{
		URL:  decision.URL,
		Tags: git.AllTags,
		Auth: httpsAuth(decision),
	}

	if env.GetGitShallow() {
		msg.Debug("Using shallow clone for %s", dep.Repository)
		cloneOpts.Depth = 1
		// SingleBranch must stay false — see doClone for the reasoning.
		cloneOpts.SingleBranch = false
	}

	repository, err := git.Clone(storageCache, worktreeFileSystem, cloneOpts)
	if err != nil {
		_ = os.RemoveAll(filepath.Join(env.GetCacheDir(), dep.HashName()))
		if isCIPermissionError(err) {
			return nil, ciJobTokenError(dep.Repository, err)
		}
		return nil, err
	}
	if err := initSubmodules(dep, decision, repository); err != nil {
		return nil, err
	}
	return repository, nil
}

// UpdateCacheEmbedded updates the dependency repository in the cache using the embedded git implementation.
func UpdateCacheEmbedded(dep domain.Dependency, decision auth.Decision) (*git.Repository, error) {
	storageCache := makeStorageCacheNoCfg(dep)
	wtFs := memfs.New()

	repository, err := git.Open(storageCache, wtFs)
	if err != nil {
		msg.Warn("⚠️ Error to open cache of %s: %s", dep.Repository, err)
		var errRefresh error
		repository, errRefresh = refreshCopy(dep, decision)
		if errRefresh != nil {
			return nil, errRefresh
		}
	} else {
		worktree, _ := repository.Worktree()
		_ = worktree.Reset(&git.ResetOptions{
			Mode: git.HardReset,
		})
	}

	scrubPersistedRemoteURLs(dep, repository)

	// RemoteURL overrides whatever was persisted in the cache's .git/config at
	// clone time. This is what keeps a stale CI job token — or a changed host
	// protocol or credential — from being reused on a later run.
	err = repository.Fetch(&git.FetchOptions{
		Force:     true,
		Auth:      httpsAuth(decision),
		RemoteURL: decision.URL,
	})
	if err != nil && err.Error() != "already up-to-date" {
		msg.Debug("Error to fetch repository of %s: %s", dep.Repository, err)
	}
	if err := initSubmodules(dep, decision, repository); err != nil {
		return nil, err
	}
	return repository, nil
}

func refreshCopy(dep domain.Dependency, decision auth.Decision) (*git.Repository, error) {
	dir := filepath.Join(env.GetCacheDir(), dep.HashName())
	err := os.RemoveAll(dir)
	if err == nil {
		return CloneCacheEmbedded(dep, decision)
	}

	msg.Err("❌ Error on retry get refresh copy: %s", err)

	return nil, err
}

// makeStorageCacheNoCfg creates filesystem-backed storage for the dependency cache,
// creating the cache directory if it does not already exist.
func makeStorageCacheNoCfg(dep domain.Dependency) storage.Storer {
	cacheDir := filepath.Join(env.GetCacheDir(), dep.HashName())
	if err := os.MkdirAll(cacheDir, 0755); err != nil { //nolint:mnd // Standard directory permissions
		msg.Die("❌ Could not create %s: %s", cacheDir, err)
	}
	fs := gogitosfs.New(cacheDir)
	return filesystem.NewStorage(fs, cache2.NewObjectLRUDefault())
}

func CheckoutEmbedded(dep domain.Dependency, referenceName plumbing.ReferenceName) error {
	repository := GetRepository(dep)
	worktree, err := repository.Worktree()
	if err != nil {
		return err
	}
	return worktree.Checkout(&git.CheckoutOptions{
		Force:  true,
		Branch: referenceName,
	})
}

func CheckoutHashEmbedded(dep domain.Dependency, hash plumbing.Hash) error {
	repository := GetRepository(dep)
	if repository == nil {
		return fmt.Errorf("repository not found for %s", dep.Repository)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		return err
	}
	return worktree.Checkout(&git.CheckoutOptions{
		Hash:  hash,
		Force: true,
	})
}

// UnshallowFetchEmbedded performs a deepening fetch on the cached repository
// using the embedded go-git implementation. Depth: 0 instructs go-git to fetch
// the full history. NoErrAlreadyUpToDate is treated as success because the
// repository may already be complete (not shallow, or already deepened).
func UnshallowFetchEmbedded(dep domain.Dependency, decision auth.Decision) error {
	repository := GetRepository(dep)
	if repository == nil {
		return fmt.Errorf("repository not found for %s", dep.Repository)
	}
	// RemoteURL: see UpdateCacheEmbedded.
	err := repository.Fetch(&git.FetchOptions{
		Depth:     0,
		Force:     true,
		Tags:      git.AllTags,
		Auth:      httpsAuth(decision),
		RemoteURL: decision.URL,
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return err
	}
	return nil
}

func PullEmbedded(dep domain.Dependency, decision auth.Decision) error {
	repository := GetRepository(dep)
	worktree, err := repository.Worktree()
	if err != nil {
		return err
	}
	// RemoteURL: see UpdateCacheEmbedded.
	return worktree.Pull(&git.PullOptions{
		Force:     true,
		Auth:      httpsAuth(decision),
		RemoteURL: decision.URL,
	})
}

// isCIPermissionError reports whether err looks like a GitLab CI_JOB_TOKEN
// permission denial. Only meaningful when running inside a GitLab Runner.
func isCIPermissionError(err error) bool {
	if err == nil || os.Getenv("GITLAB_CI") != "true" {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "403") || strings.Contains(s, "Forbidden")
}

// ciJobTokenError wraps a CI permission error with the canonical fix
// pointer. The underlying error is preserved with %w for callers that
// want to inspect it.
func ciJobTokenError(repo string, cause error) error {
	return fmt.Errorf("CI_JOB_TOKEN denied access to %s.\n"+
		"Add the calling project to the dependency project's CI/CD job-token allowlist:\n"+
		"  Settings → CI/CD → Job token permissions\n"+
		"See docs/ci.md for details. Underlying error: %w", repo, cause)
}
