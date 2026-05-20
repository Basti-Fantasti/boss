package gitadapter_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	gitadapter "github.com/basti-fantasti/bossy/internal/adapters/secondary/git"
	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/pkg/consts"
	"github.com/go-git/go-billy/v5/osfs"
	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	cache2 "github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// fixtureSig returns a deterministic signature for fixture commits.
func fixtureSig() object.Signature {
	return object.Signature{
		Name:  "Test",
		Email: "test@example.com",
		When:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// buildFixtureRepo creates an on-disk go-git repository whose cache storer and
// worktree layout exactly mirror what GetRepository expects: cache storer rooted
// at $BOSS_HOME/cache/<dep.HashName()>, worktree at <cwd>/modules/<dep.Name()>.
// It seeds the worktree with two commits and returns the resulting commit hashes
// in chronological order (A first, then B).
func buildFixtureRepo(t *testing.T, dep domain.Dependency) (plumbing.Hash, plumbing.Hash) {
	t.Helper()

	cacheDir := filepath.Join(os.Getenv("BOSS_HOME"), "cache", dep.HashName())
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	workDir := filepath.Join(consts.FolderDependencies, dep.Name())
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	storer := filesystem.NewStorage(osfs.New(cacheDir), cache2.NewObjectLRUDefault())
	repo, err := goGit.Init(storer, osfs.New(workDir))
	if err != nil {
		t.Fatalf("git init: %v", err)
	}
	// Ensure HEAD points at refs/heads/master so the worktree can read it back.
	if errSet := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.Master)); errSet != nil {
		t.Fatalf("set HEAD: %v", errSet)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}

	writeAndCommit := func(name, content string) plumbing.Hash {
		path := filepath.Join(workDir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		if _, err := wt.Add(name); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
		h, err := wt.Commit("commit "+name, &goGit.CommitOptions{Author: &(object.Signature{
			Name: fixtureSig().Name, Email: fixtureSig().Email, When: fixtureSig().When,
		})})
		if err != nil {
			t.Fatalf("commit %s: %v", name, err)
		}
		return h
	}

	hashA := writeAndCommit("a.txt", "alpha")
	hashB := writeAndCommit("b.txt", "beta")

	// Sanity: tag v1.0.0 at A, v1.1.0 at B — mirrors a realistic upstream.
	if _, err := repo.CreateTag("v1.0.0", hashA, nil); err != nil {
		t.Fatalf("tag v1.0.0: %v", err)
	}
	if _, err := repo.CreateTag("v1.1.0", hashB, nil); err != nil {
		t.Fatalf("tag v1.1.0: %v", err)
	}

	return hashA, hashB
}

// withFixtureEnv isolates BOSS_HOME and cwd to a fresh temp dir for one test.
func withFixtureEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("BOSS_HOME", home)
	t.Chdir(project)
}

// TestCheckoutHashEmbedded_RoundTrip exercises the SHA-pinning checkout primitive
// against a real on-disk multi-commit fixture: it verifies that a pinned hash
// lands the worktree on exactly that commit, that switching between commits is
// possible in either direction, and that an unknown SHA fails with the sentinel
// error the installer's isObjectNotFound classifier recognizes.
func TestCheckoutHashEmbedded_RoundTrip(t *testing.T) {
	withFixtureEnv(t)

	dep := domain.Dependency{Repository: "https://example.com/owner/fixture-repo"}
	hashA, hashB := buildFixtureRepo(t, dep)

	if hashA == hashB {
		t.Fatalf("fixture produced identical hashes: %s", hashA)
	}

	// HEAD currently points at B (last commit). Confirm baseline.
	repo := gitadapter.GetRepository(dep)
	if repo == nil {
		t.Fatalf("GetRepository returned nil — env wiring is wrong")
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head.Hash() != hashB {
		t.Fatalf("baseline HEAD = %s, want %s", head.Hash(), hashB)
	}

	// Pin to A → HEAD must move to A.
	if errCo := gitadapter.CheckoutHashEmbedded(dep, hashA); errCo != nil {
		t.Fatalf("checkout A: %v", errCo)
	}
	head, err = gitadapter.GetRepository(dep).Head()
	if err != nil {
		t.Fatalf("head after A: %v", err)
	}
	if head.Hash() != hashA {
		t.Fatalf("after checkout A: HEAD = %s, want %s", head.Hash(), hashA)
	}

	// Pin back to B → HEAD must move forward to B again.
	if errCo := gitadapter.CheckoutHashEmbedded(dep, hashB); errCo != nil {
		t.Fatalf("checkout B: %v", errCo)
	}
	head, err = gitadapter.GetRepository(dep).Head()
	if err != nil {
		t.Fatalf("head after B: %v", err)
	}
	if head.Hash() != hashB {
		t.Fatalf("after checkout B: HEAD = %s, want %s", head.Hash(), hashB)
	}

	// Pin to a SHA the repo does not contain → must error, and the error must be
	// classifiable by the same logic the installer uses (errors.Is against
	// plumbing.ErrObjectNotFound). This guards against silent fallback.
	bogus := plumbing.NewHash("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	err = gitadapter.CheckoutHashEmbedded(dep, bogus)
	if err == nil {
		t.Fatalf("checkout of bogus SHA must fail, got nil")
	}
	if !errors.Is(err, plumbing.ErrObjectNotFound) {
		t.Fatalf("bogus SHA error = %v, want errors.Is(plumbing.ErrObjectNotFound)", err)
	}

	// HEAD must NOT have moved to the bogus value on failure.
	head, err = gitadapter.GetRepository(dep).Head()
	if err != nil {
		t.Fatalf("head after failed bogus checkout: %v", err)
	}
	if head.Hash() == bogus {
		t.Fatalf("HEAD was advanced to bogus SHA despite checkout error")
	}
}
