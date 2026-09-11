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
	"sync"
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
		// --no-single-branch is required: git implies --single-branch with
		// --depth, which writes a refspec restricted to the default branch and
		// makes every other branch permanently unreachable. Not even
		// `fetch --unshallow` recovers them.
		args = append(args, "--depth", "1", "--no-single-branch")
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

	// Repair caches cloned by an older bossy that passed --single-branch: their
	// refspec is pinned to one branch, so `fetch --all` silently brings nothing
	// for every other branch.
	//
	// Best effort, like scrubPersistedRemoteURLs: the fetch below is the real
	// operation and fails loudly on its own, so a repair that could not be
	// applied is a debug note rather than a hard error.
	if err := repairRemoteRefspec(dirModule); err != nil {
		msg.Debug("Could not normalise refspec for %s: %s", dep.Repository, err)
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

// wildcardHeadsRefspec is the refspec every cache bossy manages must have:
// one that brings all branches, so a later checkout of any of them succeeds.
const wildcardHeadsRefspec = "+refs/heads/*:refs/remotes/origin/*"

// repairRemoteRefspec makes remote.origin.fetch cover refs/heads/* in the
// cache rooted at dirModule, rewriting it only when nothing already does.
//
// Read before write, for two reasons. `git config <key> <value>` refuses a
// multi-valued key outright -
//
//	warning: remote.origin.fetch has multiple values
//	error: cannot overwrite multiple values with a single value
//
// - leaving the config untouched, so an unconditional single-value write is a
// silent no-op on exactly the caches most likely to be unusual. And rewriting
// unconditionally would discard a refspec the user deliberately narrowed or
// extended: the old claim that this is "idempotent and harmless on healthy
// caches" only ever held for caches bossy cloned itself.
//
// The healthy case still costs one git invocation, the same as the
// unconditional write it replaces.
//
// The rewrite uses --replace-all because that is the only way to collapse a
// multi-valued key. A cache that carried extra refspecs alongside a restricted
// one - a refs/notes/* mapping, say - loses them, which is accepted: none of
// its values covered refs/heads/*, so it was broken for bossy's purposes and
// the wildcard is what makes it usable again.
//
// There is no embedded counterpart. getVersionsEmbedded asks for an explicit
// refs/*:refs/* refspec on every call, so a restricted stored refspec never
// reaches the go-git path.
func repairRemoteRefspec(dirModule string) error {
	existing, err := gitConfigValues(dirModule, "remote.origin.fetch")
	if err != nil {
		return err
	}
	for _, value := range existing {
		if strings.Contains(value, "refs/heads/*") {
			return nil
		}
	}

	cmd := exec.Command("git", "config", "--replace-all", "remote.origin.fetch", wildcardHeadsRefspec)
	cmd.Dir = dirModule
	return runCommand(cmd)
}

// gitConfigValues returns every value configured for key in the repository at
// dirModule. stdout is captured here rather than through runCommand, which
// routes it to msg.Debug and discards it.
//
// An unset key makes git exit 1 with no output; that is reported as no values
// rather than as an error, because a missing refspec is one of the broken
// states repairRemoteRefspec exists to fix.
func gitConfigValues(dirModule, key string) ([]string, error) {
	var stdout, stderr bytes.Buffer
	//nolint:gosec,nolintlint // key is a compile-time constant, not user input
	cmd := exec.Command("git", "config", "--get-all", key) // #nosec G204
	cmd.Dir = dirModule
	cmd.Env = gitSafeEnv()
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("git config --get-all %s: %w\nStderr: %s",
			key, err, redactURLCredentials(stderr.String()))
	}

	values := make([]string, 0)
	for _, line := range strings.Split(stdout.String(), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values, nil
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
// Distinct from scrubTokenFromURL in git_embedded.go, which removes rather
// than masks credentials, and only from a single well-formed URL on its way
// back to disk.
//
// This is defence in depth rather than a live leak. Everything in this file is
// reached only for TransportSSH, and the SSH URLs auth.Resolve builds are all
// scp-form "git@host:path", which carries no userinfo for this pattern to
// match. No auth layer puts a credential in the URL any more - the gitlab-ci
// layer carries its job token in Decision.Credential, and resolves to
// TransportHTTPS besides, so it never lands here. The guard stays because the
// invariant is one auth layer away from changing.
func redactURLCredentials(s string) string {
	return reURLCredentials.ReplaceAllString(s, "${1}//***@")
}

// batchModeOption is the only ssh option bossy imposes. It is what actually
// makes a native git operation fail fast: it turns off ssh's passphrase prompt
// and forces StrictHostKeyChecking to refuse rather than ask on first contact.
const batchModeOption = " -o BatchMode=yes"

// fallbackSSHProgram is used when ssh cannot be found on PATH. Naming it and
// letting the run fail with git's own message beats inventing a better error
// for a machine that has no ssh at all.
const fallbackSSHProgram = "ssh"

// resolveSSHProgram returns the ssh bossy should name in GIT_SSH_COMMAND.
//
// Naming a bare "ssh" is not the same as letting git find ssh itself, and on
// Windows the difference decides which binary runs. Git executes
// GIT_SSH_COMMAND through its bundled sh, whose PATH puts Git's own
// /usr/bin first, so "ssh" resolves to the ssh shipped inside Git for Windows
// no matter what the process PATH says. With no GIT_SSH_COMMAND set, git
// resolves ssh against the process PATH instead, which on most Windows
// machines finds C:\Windows\System32\OpenSSH first.
//
// Those two are different programs linked against different crypto libraries -
// Git's against OpenSSL, Microsoft's against LibreSSL - and they do not accept
// the same private keys. A key in the legacy PEM format loads under LibreSSL
// and is rejected by OpenSSL 3 with "error in libcrypto: unsupported", so
// merely setting GIT_SSH_COMMAND could turn a working checkout into
// "Permission denied (publickey)".
//
// Resolving ssh here, against the same PATH git would have used, keeps bossy's
// choice of binary identical to git's own while still applying BatchMode.
func resolveSSHProgram() string {
	path, err := exec.LookPath(fallbackSSHProgram)
	if err != nil {
		msg.Debug("ssh not found on PATH, leaving resolution to git: %v", err)
		return fallbackSSHProgram
	}
	return quoteSSHProgram(path)
}

// quoteSSHProgram renders a resolved ssh path for GIT_SSH_COMMAND, which git
// parses with shell quoting rules. Backslashes are escapes to that parser, so
// a Windows path has to be given with forward slashes, and the quotes cover
// the space in "C:/Program Files/...".
func quoteSSHProgram(path string) string {
	return `"` + strings.ReplaceAll(path, `\`, "/") + `"`
}

// configuredSSHCommand returns git's own core.sshCommand, or "" when none is
// configured or git cannot be run.
//
// GIT_SSH_COMMAND outranks core.sshCommand, so supplying a default without
// looking would override a wrapper the user deliberately configured - the very
// setup the native path exists to honour. The result is memoised because it is
// otherwise re-read once per dependency, and an ssh wrapper does not change
// mid-run.
//
//nolint:gochecknoglobals // memoised process-wide lookup; the value cannot change mid-run
var configuredSSHCommand = sync.OnceValue(func() string {
	out, err := exec.Command("git", "config", "--get", "core.sshCommand").Output()
	if err != nil {
		// Exit status 1 simply means "not set", which is the common case.
		return ""
	}
	return strings.TrimSpace(string(out))
})

// defaultSSHCommand builds the GIT_SSH_COMMAND bossy supplies when the caller
// set none: the user's configured ssh wrapper if there is one, otherwise the
// ssh git itself would have run, in both cases with BatchMode added.
func defaultSSHCommand() string {
	if configured := configuredSSHCommand(); configured != "" {
		return configured + batchModeOption
	}
	return resolveSSHProgram() + batchModeOption
}

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
	return filterGitEnv(os.Environ(), defaultSSHCommand())
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
func filterGitEnv(entries []string, sshCommand string) []string {
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
		out = append(out, "GIT_SSH_COMMAND="+sshCommand)
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
		// and would be mistaken for a raw-SHA pin. The same predicate filters
		// the embedded backend, so the two cannot drift apart.
		if !isBranchOrTagRef(name) {
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
