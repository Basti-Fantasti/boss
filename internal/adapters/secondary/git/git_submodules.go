package gitadapter

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// AdvanceSubmodulesToBranchTips advances every submodule of a dependency to the
// tip of its configured branch — what `git submodule update --remote` does — and
// returns the commits it settled on, keyed by submodule path.
//
// Floating submodules and a lock file pull in opposite directions, so the two
// halves are split: this resolves the tips once, and the caller records the
// result in bossy-lock.json. A later install replays those commits through
// CheckoutSubmodules instead of re-resolving, which is what keeps an install
// from a committed lock reproducible even though the declared intent floats.
//
// Only dependencies whose preset asks for it take this path; git's own default
// of honouring the superproject's recorded commits stays the default here.
func AdvanceSubmodulesToBranchTips(dep domain.Dependency) (map[string]string, error) {
	if useNativeSubmodules(dep) {
		if err := advanceSubmodulesNative(dep); err != nil {
			return nil, err
		}
		return SubmoduleCommits(dep)
	}
	return advanceSubmodulesEmbedded(dep)
}

// advanceSubmodulesNative shells out to the system git binary.
func advanceSubmodulesNative(dep domain.Dependency) error {
	cmd := exec.Command("git", "submodule", "update", "--init", "--recursive", "--remote")
	cmd.Dir = moduleDir(dep)

	if err := runCommand(cmd); err != nil {
		return fmt.Errorf("advance submodules of %s: %w", dep.Repository, err)
	}
	return nil
}

// advanceSubmodulesEmbedded does the same with go-git.
//
// go-git has no equivalent of --remote, so each submodule is fetched and then
// checked out at the tip of the branch .gitmodules names for it, defaulting to
// the submodule repository's own HEAD branch when none is named — which is what
// git itself does.
func advanceSubmodulesEmbedded(dep domain.Dependency) (map[string]string, error) {
	worktree, err := openWorktree(dep)
	if err != nil {
		return nil, err
	}
	resolved, err := advanceSubmodulesInWorktree(worktree)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dep.Repository, err)
	}
	return resolved, nil
}

// advanceSubmodulesInWorktree is advanceSubmodulesEmbedded's dependency-free
// half, so the go-git logic can be exercised against a plain repository rather
// than bossy's separate-git-dir cache layout.
func advanceSubmodulesInWorktree(worktree *goGit.Worktree) (map[string]string, error) {
	submodules, err := worktree.Submodules()
	if err != nil {
		return nil, fmt.Errorf("read submodules: %w", err)
	}

	resolved := map[string]string{}
	for _, sub := range submodules {
		hash, subErr := advanceOneSubmodule(sub)
		if subErr != nil {
			return nil, fmt.Errorf("advance submodule %s: %w", sub.Config().Path, subErr)
		}
		resolved[sub.Config().Path] = hash
	}
	return resolved, nil
}

// advanceOneSubmodule fetches a submodule and checks out its branch tip.
func advanceOneSubmodule(sub *goGit.Submodule) (string, error) {
	if err := sub.Init(); err != nil && !strings.Contains(err.Error(), "already initialized") {
		return "", err
	}
	if err := sub.Update(&goGit.SubmoduleUpdateOptions{Init: true}); err != nil &&
		!strings.Contains(err.Error(), "already up-to-date") {
		return "", err
	}

	repo, err := sub.Repository()
	if err != nil {
		return "", err
	}
	if fetchErr := repo.Fetch(&goGit.FetchOptions{Force: true, Tags: goGit.AllTags}); fetchErr != nil &&
		!strings.Contains(fetchErr.Error(), goGit.NoErrAlreadyUpToDate.Error()) {
		return "", fetchErr
	}

	target, err := submoduleTip(repo, sub.Config().Branch)
	if err != nil {
		return "", err
	}

	subWorktree, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	if err := subWorktree.Checkout(&goGit.CheckoutOptions{Hash: target, Force: true}); err != nil {
		return "", err
	}
	return target.String(), nil
}

// submoduleTip resolves the commit a submodule should advance to.
func submoduleTip(repo *goGit.Repository, branch string) (plumbing.Hash, error) {
	if branch != "" && branch != "." {
		ref, err := repo.Reference(plumbing.NewRemoteReferenceName(goGit.DefaultRemoteName, branch), true)
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("resolve branch %q: %w", branch, err)
		}
		return ref.Hash(), nil
	}

	// No branch named in .gitmodules: git tracks the submodule's default
	// branch, which the remote advertises as HEAD.
	ref, err := repo.Reference(plumbing.NewRemoteHEADReferenceName(goGit.DefaultRemoteName), true)
	if err == nil {
		return ref.Hash(), nil
	}

	head, headErr := repo.Head()
	if headErr != nil {
		return plumbing.ZeroHash, fmt.Errorf("resolve submodule default branch: %w", headErr)
	}
	return head.Hash(), nil
}

// CheckoutSubmodules puts each named submodule at a recorded commit.
//
// This is the replay half of the remote policy: an install working from a lock
// reproduces the commits the resolving install settled on rather than chasing
// the branch tips again.
func CheckoutSubmodules(dep domain.Dependency, commits map[string]string) error {
	if len(commits) == 0 {
		return nil
	}

	// bossy-lock.json is a file in the repository, so its contents are
	// attacker-influenced in any project you did not write yourself. Every
	// commit reaches a git command line, so each is checked to be a full hex
	// SHA before it gets there — a value like "--upload-pack=..." would
	// otherwise be handed to git as an option.
	for path, commit := range commits {
		if !isFullHexSHA(commit) {
			return fmt.Errorf("locked submodule %s of %s has an invalid commit %q",
				path, dep.Repository, commit)
		}
	}

	if useNativeSubmodules(dep) {
		return checkoutSubmodulesNative(dep, commits)
	}
	return checkoutSubmodulesEmbedded(dep, commits)
}

// checkoutSubmodulesNative checks each submodule out with the git binary.
func checkoutSubmodulesNative(dep domain.Dependency, commits map[string]string) error {
	// Initialise first: a fresh clone has empty submodule directories, and
	// there is nothing to check out inside them until git has populated them.
	if err := initSubmodulesNative(dep); err != nil {
		return err
	}

	for _, path := range sortedKeys(commits) {
		// #nosec G204 -- CheckoutSubmodules rejects anything but a full hex SHA
		cmd := exec.Command("git", "checkout", "--detach", commits[path])
		cmd.Dir = filepath.Join(moduleDir(dep), filepath.FromSlash(path))
		if err := runCommand(cmd); err != nil {
			return fmt.Errorf("checkout submodule %s of %s at %s: %w", path, dep.Repository, commits[path], err)
		}
	}
	return nil
}

// checkoutSubmodulesEmbedded does the same with go-git.
func checkoutSubmodulesEmbedded(dep domain.Dependency, commits map[string]string) error {
	worktree, err := openWorktree(dep)
	if err != nil {
		return err
	}
	if err := checkoutSubmodulesInWorktree(worktree, commits); err != nil {
		return fmt.Errorf("%s: %w", dep.Repository, err)
	}
	return nil
}

// checkoutSubmodulesInWorktree is checkoutSubmodulesEmbedded's dependency-free
// half. See advanceSubmodulesInWorktree.
func checkoutSubmodulesInWorktree(worktree *goGit.Worktree, commits map[string]string) error {
	submodules, err := worktree.Submodules()
	if err != nil {
		return fmt.Errorf("read submodules: %w", err)
	}

	for _, sub := range submodules {
		want, named := commits[sub.Config().Path]
		if !named {
			continue
		}
		if err := checkoutOneSubmodule(sub, want); err != nil {
			return fmt.Errorf("checkout submodule %s at %s: %w", sub.Config().Path, want, err)
		}
	}
	return nil
}

// checkoutOneSubmodule puts a single submodule at a commit.
func checkoutOneSubmodule(sub *goGit.Submodule, commit string) error {
	if err := sub.Update(&goGit.SubmoduleUpdateOptions{Init: true}); err != nil &&
		!strings.Contains(err.Error(), "already up-to-date") {
		return err
	}

	repo, err := sub.Repository()
	if err != nil {
		return err
	}
	subWorktree, err := repo.Worktree()
	if err != nil {
		return err
	}
	return subWorktree.Checkout(&goGit.CheckoutOptions{Hash: plumbing.NewHash(commit), Force: true})
}

// SubmoduleCommits reports the commit each submodule currently sits at.
func SubmoduleCommits(dep domain.Dependency) (map[string]string, error) {
	if !useNativeSubmodules(dep) {
		return submoduleCommitsEmbedded(dep)
	}

	cmd := exec.Command("git", "submodule", "status", "--recursive")
	cmd.Dir = moduleDir(dep)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read submodule status of %s: %w", dep.Repository, err)
	}
	return parseSubmoduleStatus(string(out)), nil
}

// submoduleCommitsEmbedded reads submodule HEADs through go-git.
func submoduleCommitsEmbedded(dep domain.Dependency) (map[string]string, error) {
	worktree, err := openWorktree(dep)
	if err != nil {
		return nil, err
	}
	commits, err := submoduleCommitsInWorktree(worktree)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dep.Repository, err)
	}
	return commits, nil
}

// submoduleCommitsInWorktree is submoduleCommitsEmbedded's dependency-free
// half. See advanceSubmodulesInWorktree.
func submoduleCommitsInWorktree(worktree *goGit.Worktree) (map[string]string, error) {
	submodules, err := worktree.Submodules()
	if err != nil {
		return nil, fmt.Errorf("read submodules: %w", err)
	}

	commits := map[string]string{}
	for _, sub := range submodules {
		status, statusErr := sub.Status()
		if statusErr != nil {
			continue
		}
		if status.Current.IsZero() {
			continue
		}
		commits[sub.Config().Path] = status.Current.String()
	}
	return commits, nil
}

// parseSubmoduleStatus turns `git submodule status --recursive` output into a
// path-to-commit map.
//
// Each line is "<status><sha> <path> (<describe>)", where status is a space, -,
// + or U. Lines whose SHA is not a full 40 hex digits are skipped rather than
// recorded: plumbing.NewHash zero-pads a short value into a well-formed but
// wrong commit, and a wrong commit in the lock is worse than a missing one.
func parseSubmoduleStatus(stdout string) map[string]string {
	commits := map[string]string{}
	for _, line := range strings.Split(stdout, "\n") {
		trimmed := strings.TrimLeft(strings.TrimRight(line, "\r"), " -+U")
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		sha, path := fields[0], fields[1]
		if !isFullHexSHA(sha) {
			continue
		}
		commits[filepath.ToSlash(path)] = sha
	}
	return commits
}

// useNativeSubmodules reports whether submodule work should go through the git
// binary. It follows the same rule as the rest of the adapter: the binary when
// it is available, go-git otherwise.
func useNativeSubmodules(dep domain.Dependency) bool {
	if _, err := exec.LookPath("git"); err != nil {
		msg.Debug("git binary not on PATH; using go-git for submodules of %s", dep.Repository)
		return false
	}
	return true
}

// moduleDir is where a dependency's worktree lives.
func moduleDir(dep domain.Dependency) string {
	return filepath.Join(env.GetModulesDir(), dep.Name())
}

// openWorktree opens a dependency's worktree.
func openWorktree(dep domain.Dependency) (*goGit.Worktree, error) {
	repository, err := TryGetRepository(dep)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dep.Repository, err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		return nil, fmt.Errorf("open worktree of %s: %w", dep.Repository, err)
	}
	return worktree, nil
}

// sortedKeys orders map keys so submodules are processed deterministically.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
