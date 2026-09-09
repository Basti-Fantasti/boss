//nolint:testpackage // Shared helpers for the internal git adapter tests
package gitadapter

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hermeticGitEnv returns an environment that detaches git from the user's
// global and system configuration and drops the repository-selecting
// variables. Every test in this package that shells out to git needs the same
// isolation: an ambient GIT_DIR would redirect a clone, a global
// remote.origin.fetch or clone.* setting would make the refspec assertions
// read the developer's config rather than the flags under test, a global
// core.hooksPath (which the pre-commit framework installs, and this project
// pins pre-commit) would make `git commit` run an ambient hook, and a global
// commit.gpgsign could block on a pinentry prompt.
//
// The drop-list is deliberate rather than exhaustive: GIT_COMMON_DIR,
// GIT_OBJECT_DIRECTORY, GIT_CEILING_DIRECTORIES and GIT_CONFIG_COUNT /
// GIT_CONFIG_KEY_* all currently survive it, which is adequate for the
// assertions these tests make. Harden here, not in a second copy.
func hermeticGitEnv(t *testing.T) []string {
	t.Helper()

	// A path that does not exist: git >= 2.32 treats an unreadable
	// GIT_CONFIG_GLOBAL/GIT_CONFIG_SYSTEM as "no such config file".
	noConfig := filepath.Join(t.TempDir(), "absent-gitconfig")

	// Drop the repository-selecting variables outright; git treats an empty
	// GIT_DIR as set, so blanking them is not enough.
	gitEnv := make([]string, 0, len(os.Environ())+6)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE":
			continue
		}
		gitEnv = append(gitEnv, entry)
	}
	return append(gitEnv,
		"GIT_CONFIG_GLOBAL="+noConfig,
		"GIT_CONFIG_SYSTEM="+noConfig,
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
}

// runGit runs git in dir with the hermetic environment and returns trimmed
// stdout, failing the test if git exits non-zero. An empty dir inherits the
// test process's working directory, which is what the callers that pass
// --git-dir or an explicit destination path want.
func runGit(t *testing.T, gitEnv []string, dir string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\n%s%s", args, err, stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}
