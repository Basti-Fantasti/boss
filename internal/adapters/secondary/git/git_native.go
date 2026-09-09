// Package gitadapter provides native Git command execution.
// This file implements Git clone/update operations using system Git commands.
package gitadapter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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
	// gitSafeEnv rather than os.Environ. The hang protection is the part with
	// standing value: every caller of runCommand is gated on TransportSSH, so
	// BatchMode keeps a first-contact host-key prompt or a passphrase prompt
	// from blocking a non-interactive run forever.
	//
	// The credential hygiene is defence in depth, not a fix for a live leak:
	// auth.Resolve only ever builds scp-form "git@host:path" URLs for SSH, and
	// the gitlab-ci layer that does embed a job token resolves to HTTPS, which
	// never reaches this runner. It costs nothing and holds the invariant if a
	// future auth layer produces an SSH URL carrying userinfo.
	cmd.Env = gitSafeEnv()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("command failed: %w\nStderr: %s", err, redactURLCredentials(stderrBuf.String()))
	}

	// Debug output is still output: it reaches the same logs the wrapped error
	// does, so it is redacted on the same terms.
	if stdoutBuf.Len() > 0 {
		msg.Debug("Command stdout: %s", redactURLCredentials(stdoutBuf.String()))
	}
	if stderrBuf.Len() > 0 {
		msg.Debug("Command stderr: %s", redactURLCredentials(stderrBuf.String()))
	}

	return nil
}

// reURLCredentials matches the userinfo component of a URL-like substring.
// The scheme is optional because git frequently echoes the scheme-relative
// form (//user:secret@host/path) in its diagnostics.
var reURLCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*:)?//[^/\s]+@`)

// redactURLCredentials replaces the userinfo component of any URL-like
// substring with "***". git anonymizes credentials on some transports but not
// all - file:// and ssh:// print them verbatim, and GIT_TRACE dumps the raw
// command line regardless - so stderr must be scrubbed before it is wrapped
// into an error that may reach a log.
//
// This is defence in depth rather than a live leak. Everything in this file is
// reached only for TransportSSH, and the SSH URLs auth.Resolve builds are all
// scp-form "git@host:path", which carries no userinfo for this pattern to
// match. The gitlab-ci layer that does bake a CI_JOB_TOKEN into the URL
// resolves to TransportHTTPS and so never lands here. The guard stays because
// the invariant is one auth layer away from changing.
func redactURLCredentials(s string) string {
	return reURLCredentials.ReplaceAllString(s, "${1}//***@")
}

// defaultSSHCommand is the ssh invocation used when the caller's environment
// does not already specify one. BatchMode=yes is the only thing that actually
// makes a native git operation fail fast: it turns off ssh's passphrase prompt
// and forces StrictHostKeyChecking to refuse rather than ask on first contact.
const defaultSSHCommand = "ssh -o BatchMode=yes"

// gitSafeEnv returns the process environment prepared for a native git
// invocation: tracing switches removed, and both prompt sources disabled.
//
// Tracing is dropped because GIT_TRACE and its relatives dump the full command
// line - including any credentials baked into the remote URL - to stderr,
// which is wrapped into returned errors and from there into logs.
//
// Prompting is disabled because ref listing and cloning run once per
// dependency during install, and a prompt nobody is there to answer blocks the
// whole run. The two prompt sources are not the same:
//
//   - GIT_TERMINAL_PROMPT=0 governs git's own credential prompt. That is the
//     HTTPS path, so it does nothing for the callers in this file, all of
//     which are gated on TransportSSH. It is set anyway because it is free and
//     correct for any future HTTPS caller.
//   - GIT_SSH_COMMAND is what matters here. The blocking prompts on the SSH
//     path come from the child ssh process - a key passphrase when no agent is
//     loaded, and StrictHostKeyChecking=ask on first contact with an internal
//     host - and both are read from the console, so an empty stdin does not
//     help. BatchMode=yes turns both into an immediate failure.
func gitSafeEnv() []string {
	return filterGitEnv(os.Environ())
}

// filterGitEnv is gitSafeEnv's pure half: it drops the tracing and prompting
// variables from entries, appends GIT_TERMINAL_PROMPT=0, and supplies
// defaultSSHCommand when the caller has not set GIT_SSH_COMMAND.
//
// A user-set GIT_SSH_COMMAND is preserved verbatim. Reading the developer's
// own ssh setup is the entire reason the native path exists, so bossy defaults
// that variable but never overrides it. The caveat is that an ssh wrapper
// configured through git's core.sshCommand is lower precedence than the
// environment variable and therefore is overridden; a user in that position
// can export GIT_SSH_COMMAND to get it back.
//
// GIT_TERMINAL_PROMPT is stripped before being appended rather than simply
// appended: execve passes duplicate names through unchanged and getenv returns
// the first match, so an inherited GIT_TERMINAL_PROMPT=1 would shadow the
// appended GIT_TERMINAL_PROMPT=0. Any refactor that routes this through a map
// must keep that property.
//
// What is deliberately not filtered: GIT_DIR, GIT_WORK_TREE, GIT_SSH_COMMAND
// and the global/system config variables. User configuration is trusted - the
// native path exists precisely so ~/.ssh/config and friends are honoured - so
// only the leak vectors (tracing) and the hang vectors (prompting) are
// stripped.
//
// Names are matched case-insensitively. Windows resolves environment variable
// names without regard to case and Git for Windows honours a lowercase
// git_trace, but os.Environ reports whatever casing was used to set the
// variable, so a case-sensitive match would pass the switch straight through
// to the child. That matters beyond stderr: GIT_TRACE2_EVENT writes to a file
// sink that redactURLCredentials never sees, so scrubbing stderr is not a
// backstop for it.
func filterGitEnv(entries []string) []string {
	out := make([]string, 0, len(entries)+2)
	hasSSHCommand := false
	for _, entry := range entries {
		name, _, _ := strings.Cut(entry, "=")
		name = strings.ToUpper(name)
		if strings.HasPrefix(name, "GIT_TRACE") {
			continue
		}
		switch name {
		case "GIT_CURL_VERBOSE", "GIT_TERMINAL_PROMPT":
			continue
		case "GIT_SSH_COMMAND":
			hasSSHCommand = true
		}
		out = append(out, entry)
	}
	out = append(out, "GIT_TERMINAL_PROMPT=0")
	if !hasSSHCommand {
		out = append(out, "GIT_SSH_COMMAND="+defaultSSHCommand)
	}
	return out
}

// ListRefsNative enumerates the dependency's remote heads and tags using the
// system git binary. It is the SSH counterpart to the ref listing go-git does
// inside GetVersions: go-git's SSH transport does not read ~/.ssh/config and
// enforces strict known_hosts, so it cannot be relied on for internal hosts
// that use config aliases, custom keys or non-standard ports.
//
// The URL is passed explicitly, so no worktree, .git pointer or prior clone is
// required. stdout is captured here rather than through runCommand because
// runCommand routes stdout to msg.Debug and discards it; ls-remote itself
// writes nothing to stderr on success.
func ListRefsNative(dep domain.Dependency, decision auth.Decision) ([]*plumbing.Reference, error) {
	return listRefsNative(dep, decision, refListTimeout)
}

// refListTimeout bounds a single ref listing. Listing runs once per dependency
// and serially, and a failure is terminal for the install, so an unbounded
// hang would leave the user with no output at all rather than an error. The
// budget is generous: ls-remote transfers no objects, so anything approaching
// a minute means the host is unreachable rather than slow.
const refListTimeout = 60 * time.Second

// refListWaitDelay bounds how long Wait keeps waiting for the output pipes
// after the context has killed git. Killing git does not kill the ssh process
// it spawned, and that grandchild holds the write end of the stderr pipe, so
// without a delay Wait would block on it and defeat the timeout.
const refListWaitDelay = 5 * time.Second

// listRefsNative is ListRefsNative with an injectable timeout so the deadline
// can be exercised in tests without waiting out refListTimeout.
func listRefsNative(
	dep domain.Dependency,
	decision auth.Decision,
	timeout time.Duration,
) ([]*plumbing.Reference, error) {
	if err := requireGit(dep, hostFromURL(decision.URL)); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	//nolint:gosec,nolintlint // URL originates from auth.Resolve; not independently validated here
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", "--tags", decision.URL) // #nosec G204
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = gitSafeEnv()
	cmd.WaitDelay = refListWaitDelay

	err := cmd.Run()
	// Checked before err: a killed process reports a generic exit status, and
	// "timed out" is the diagnosis the user can act on.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("git ls-remote for %s timed out after %s (host unreachable or prompting for input)",
			dep.Repository, timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("git ls-remote failed for %s: %w\nStderr: %s",
			dep.Repository, err, redactURLCredentials(stderr.String()))
	}

	return parseLsRemote(stdout.String()), nil
}

// parseLsRemote turns `git ls-remote` stdout ("<sha>\t<refname>" per line) into
// plumbing references. Only refs/heads/* and refs/tags/* entries carrying a
// full 40-digit hex SHA are returned. Peeled annotated-tag entries
// ("refs/tags/x^{}") are dropped: the tag object itself is the ref bossy
// resolves against. Everything else is ignored — a bare HEAD line, the
// server-advertised refs/pull/* and refs/merge-requests/* namespaces, and
// --symref output, which arrives as three fields. Rejected lines are skipped
// rather than treated as fatal: a partially parseable listing is more useful
// than none.
func parseLsRemote(stdout string) []*plumbing.Reference {
	refs := make([]*plumbing.Reference, 0)
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		sha, name := fields[0], fields[1]
		// Only real branches and tags. Server-advertised refs such as
		// refs/pull/*, refs/merge-requests/* and a bare HEAD line all satisfy
		// installer's isHashRef sentinel (!IsTag && !IsBranch && !IsRemote)
		// and would be mistaken for a raw-SHA pin.
		if !strings.HasPrefix(name, "refs/heads/") && !strings.HasPrefix(name, "refs/tags/") {
			continue
		}
		if strings.HasSuffix(name, "^{}") {
			continue
		}
		if !isFullHexSHA(sha) {
			continue
		}
		refs = append(refs, plumbing.NewReferenceFromStrings(name, sha))
	}
	return refs
}

// isFullHexSHA reports whether s is exactly 40 lowercase-or-uppercase hex
// digits. plumbing.NewHash silently zero-pads anything shorter, turning a
// truncated SHA into a plausible-looking but wrong hash, so the length must be
// checked before the string is handed to go-git.
func isFullHexSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
