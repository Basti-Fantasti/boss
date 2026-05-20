// Package gitadapter provides native Git command execution.
// This file implements Git clone/update operations using system Git commands.
package gitadapter

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/auth"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	git2 "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// requireGit checks that the system git binary is on PATH. SSH transports
// require it; we hard-fail with a clear message that includes dep and host context.
func requireGit(dep domain.Dependency, host string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf(
			"SSH cloning requires `git` to be installed and on PATH.\n"+
				"Dependency %s resolved to SSH transport (host: %s).\n"+
				"Either install git, or change the dependency to an HTTPS URL",
			dep.Name(), host,
		)
	}
	return nil
}

// hostFromURL extracts the hostname from a URL string. Falls back to the raw
// URL if parsing fails (should not happen for well-formed decision URLs).
func hostFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		// SSH URLs like git@github.com:owner/repo are not standard; extract
		// the host between "@" and ":".
		if at := len("git@"); at < len(rawURL) {
			s := rawURL[at:]
			if colon := findByte(s, ':'); colon >= 0 {
				return s[:colon]
			}
		}
		return rawURL
	}
	return u.Hostname()
}

func findByte(s string, b byte) int {
	for i := range len(s) {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// CloneCacheNative clones the dependency repository to the cache using the native git client.
func CloneCacheNative(dep domain.Dependency, decision auth.Decision) (*git2.Repository, error) {
	if err := requireGit(dep, hostFromURL(decision.URL)); err != nil {
		return nil, err
	}
	msg.Info("📥 Downloading dependency %s", dep.Repository)
	if err := doClone(dep, decision); err != nil {
		return nil, err
	}
	return GetRepository(dep), nil
}

// UpdateCacheNative updates the dependency repository in the cache using the native git client.
func UpdateCacheNative(dep domain.Dependency, decision auth.Decision) (*git2.Repository, error) {
	if err := requireGit(dep, hostFromURL(decision.URL)); err != nil {
		return nil, err
	}
	if err := getWrapperFetch(dep); err != nil {
		return nil, err
	}
	return GetRepository(dep), nil
}

func doClone(dep domain.Dependency, decision auth.Decision) error {
	dirModule := filepath.Join(env.GetModulesDir(), dep.Name())
	dir := "--separate-git-dir=" + filepath.Join(env.GetCacheDir(), dep.HashName())

	err := os.RemoveAll(dirModule)
	if err != nil && !os.IsNotExist(err) {
		msg.Debug("Failed to remove module directory: %v", err)
	}
	err = os.Remove(dirModule)
	if err != nil && !os.IsNotExist(err) {
		msg.Debug("Failed to remove module file: %v", err)
	}

	args := []string{"clone", dir}

	if env.GetGitShallow() {
		msg.Debug("Using shallow clone for %s", dep.Repository)
		args = append(args, "--depth", "1", "--single-branch")
	}

	args = append(args, decision.URL, dirModule)

	//nolint:gosec,nolintlint // Git command with controlled and validated repository URL
	cmd := exec.Command("git", args...) // #nosec G204 -- Controlled git clone command

	if err = runCommand(cmd); err != nil {
		return err
	}
	if err := initSubmodulesNative(dep); err != nil {
		return err
	}

	_ = os.Remove(filepath.Join(dirModule, ".git"))
	return nil
}

func writeDotGitFile(dep domain.Dependency) {
	mask := fmt.Sprintf("gitdir: %s\n", filepath.Join(env.GetCacheDir(), dep.HashName()))
	path := filepath.Join(env.GetModulesDir(), dep.Name(), ".git")
	_ = os.WriteFile(path, []byte(mask), 0600)
}

func getWrapperFetch(dep domain.Dependency) error {
	dirModule := filepath.Join(env.GetModulesDir(), dep.Name())

	if _, err := os.Stat(dirModule); os.IsNotExist(err) {
		err = os.MkdirAll(dirModule, 0600)
		if err != nil {
			return fmt.Errorf("failed to create module directory: %w", err)
		}
	}

	writeDotGitFile(dep)
	cmdReset := exec.Command("git", "reset", "--hard")
	cmdReset.Dir = dirModule
	if err := runCommand(cmdReset); err != nil {
		return err
	}

	cmd := exec.Command("git", "fetch", "--all")
	cmd.Dir = dirModule

	if err := runCommand(cmd); err != nil {
		return err
	}

	if err := initSubmodulesNative(dep); err != nil {
		return err
	}

	_ = os.Remove(filepath.Join(dirModule, ".git"))
	return nil
}

func initSubmodulesNative(dep domain.Dependency) error {
	dirModule := filepath.Join(env.GetModulesDir(), dep.Name())
	cmd := exec.Command("git", "submodule", "update", "--init", "--recursive")
	cmd.Dir = dirModule

	if err := runCommand(cmd); err != nil {
		return err
	}
	return nil
}

func CheckoutNative(dep domain.Dependency, decision auth.Decision, referenceName plumbing.ReferenceName) error {
	if err := requireGit(dep, hostFromURL(decision.URL)); err != nil {
		return err
	}
	dirModule := filepath.Join(env.GetModulesDir(), dep.Name())
	// doClone/getWrapperFetch leave dirModule without a .git pointer; restore it
	// so the git binary can locate the separate gitdir in the cache.
	writeDotGitFile(dep)
	//nolint:gosec,nolintlint // Git command with controlled repository reference
	cmd := exec.Command("git", "checkout", "-f", referenceName.Short()) // #nosec G204 -- Controlled git checkout command
	cmd.Dir = dirModule
	err := runCommand(cmd)
	_ = os.Remove(filepath.Join(dirModule, ".git"))
	return err
}

func CheckoutHashNative(dep domain.Dependency, decision auth.Decision, hash plumbing.Hash) error {
	if err := requireGit(dep, hostFromURL(decision.URL)); err != nil {
		return err
	}
	dirModule := filepath.Join(env.GetModulesDir(), dep.Name())
	// doClone/getWrapperFetch leave dirModule without a .git pointer; restore it
	// so the git binary can locate the separate gitdir in the cache.
	writeDotGitFile(dep)
	//nolint:gosec,nolintlint // Git command with controlled commit hash
	cmd := exec.Command("git", "checkout", "--detach", hash.String()) // #nosec G204 -- Controlled git checkout command
	cmd.Dir = dirModule
	err := runCommand(cmd)
	_ = os.Remove(filepath.Join(dirModule, ".git"))
	return err
}

// UnshallowFetchNative runs `git fetch --unshallow` against the cached repo.
// If the repository is already complete, git exits non-zero with a "--unshallow
// on a complete repository" message; that case is treated as success. Other
// failures propagate.
func UnshallowFetchNative(dep domain.Dependency, decision auth.Decision) error {
	if err := requireGit(dep, hostFromURL(decision.URL)); err != nil {
		return err
	}
	dirModule := filepath.Join(env.GetModulesDir(), dep.Name())
	writeDotGitFile(dep)
	cmd := exec.Command("git", "fetch", "--unshallow")
	cmd.Dir = dirModule
	err := runCommand(cmd)
	_ = os.Remove(filepath.Join(dirModule, ".git"))
	if err != nil && isAlreadyCompleteRepoErr(err) {
		return nil
	}
	return err
}

// isAlreadyCompleteRepoErr reports whether err is the benign native-git failure
// emitted when `--unshallow` is invoked on a repository that already has the
// full history. The wording has been stable across git versions; we match on
// the canonical substring.
func isAlreadyCompleteRepoErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "--unshallow on a complete repository")
}

func PullNative(dep domain.Dependency, decision auth.Decision) error {
	if err := requireGit(dep, hostFromURL(decision.URL)); err != nil {
		return err
	}
	dirModule := filepath.Join(env.GetModulesDir(), dep.Name())
	writeDotGitFile(dep)
	cmd := exec.Command("git", "pull", "--force")
	cmd.Dir = dirModule
	err := runCommand(cmd)
	_ = os.Remove(filepath.Join(dirModule, ".git"))
	return err
}

func runCommand(cmd *exec.Cmd) error {
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer

	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("command failed: %w\nStderr: %s", err, stderrBuf.String())
	}

	if stdoutBuf.Len() > 0 {
		msg.Debug("Command stdout: %s", stdoutBuf.String())
	}
	if stderrBuf.Len() > 0 {
		msg.Debug("Command stderr: %s", stderrBuf.String())
	}

	return nil
}
