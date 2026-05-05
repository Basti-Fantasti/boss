# Bossy Fork Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fork `boss` into the standalone `bossy` binary with fixed SSH cloning for self-hosted GitLab and zero-config GitLab CI authentication via `CI_JOB_TOKEN`.

**Architecture:** New `internal/core/services/auth` package owns protocol/credential resolution as a precedence chain (GitLab CI > env-var > per-dep URL > per-host config > stored creds > default HTTPS). All git clone/fetch sites consult it. SSH transport always shells out to system `git` (hard-fail if missing). Existing `boss login`/embedded-SSH path is removed; user-level config migrates `~/.boss/` → `~/.bossy/` on first run.

**Tech Stack:** Go 1.24, cobra CLI, go-git (HTTPS only, after this work), system `git` (SSH path), pterm (output), Masterminds/semver. Tests use stdlib `testing` + `t.Setenv` + `t.TempDir`.

**Spec:** `docs/superpowers/specs/2026-05-05-bossy-fork-design.md`. Read it first.

**Working branch:** `bossy-fork-design` (already created with the spec commit). Create a fresh branch off main for implementation if you prefer.

**Verify environment first:** `go version` (must be 1.24.x), `git --version` (any 2.x), `cd /mnt/x/git_local/boss && go mod download && go build ./...` should succeed before starting Task 1.

---

## File map

**New files:**
- `pkg/consts/branding.go` — fork-identifying constants in one place
- `internal/core/services/auth/auth.go` — `Decision`, `Transport`, `CredentialSpec`, `Resolve()`
- `internal/core/services/auth/parse.go` — URL classification (bare/host-path/git@/https)
- `internal/core/services/auth/gitlab_ci.go` — layer 1
- `internal/core/services/auth/envvar.go` — layer 2
- `internal/core/services/auth/hostconfig.go` — layer 4
- `internal/core/services/auth/stored.go` — layer 5
- `internal/core/services/auth/*_test.go` — one per source file
- `internal/migrate/migrate.go` — `~/.boss` → `~/.bossy` and `boss.json` `:ssh` rewrite
- `internal/migrate/migrate_test.go`
- `internal/adapters/primary/cli/auth.go` — `bossy auth set/list/rm`
- `docs/ci.md` — GitLab CI documentation

**Modified files:**
- `go.mod` — module path
- All `*.go` files — import path updates (mechanical sed)
- `Makefile` — BINNAME, ldflags
- `internal/upgrade/upgrade.go` — org/repo constants
- `internal/upgrade/github.go` — asset name prefix (or wherever it's built)
- `pkg/env/configuration.go` — `Auth` struct simplification (remove SSH fields), config dir path
- `pkg/env/interfaces.go` — drop `GetAuth` from interface (auth package owns this now)
- `pkg/consts/consts.go` — `FolderBossHome` becomes `.bossy`; remove `GitProtocolSSH`
- `internal/core/domain/dependency.go` — `GetURL()` becomes thin formatter; remove `UseSSH` field, `:ssh` parsing
- `internal/core/services/installer/utils.go` — accept `git@host:path` and `https://` keys; fix URL bug
- `internal/core/services/installer/core.go` and `dependency_manager.go` — call `auth.Resolve` instead of `dep.GetURL`/`config.GetAuth`
- `internal/adapters/secondary/git/git.go` — dispatch on `Decision.Transport`, not config flag
- `internal/adapters/secondary/git/git_embedded.go` — accept `Decision`, drop direct config access for auth
- `internal/adapters/secondary/git/git_native.go` — same; also fix `where git` → POSIX-friendly check
- `internal/adapters/primary/cli/login.go` — **delete**, replaced by `auth.go`
- `internal/adapters/primary/cli/root.go` — register `auth` cmd, drop `login`/`logout`
- `internal/adapters/primary/cli/config/git.go` — drop `mode`, add `protocol <host> ssh|https`
- `.github/workflows/release.yml` — asset name prefix, repo references
- `.github/workflows/ci.yml` — module path references in test names if any
- `README.md` — auth section rewrite, add `docs/ci.md` link

**Deleted files:**
- `internal/adapters/primary/cli/login.go`

---

## Task 1: Module rename and binary rebrand

**Files:**
- Modify: `go.mod`
- Modify: every `*.go` file containing `github.com/hashload/boss`
- Modify: `Makefile` (`BINNAME`, ldflags target)

This is mechanical but sweeping. Do it first so all later tasks reference the new module.

- [ ] **Step 1: Verify clean tree**

```bash
cd /mnt/x/git_local/boss
git status   # must be clean (or only have the spec commit)
go build ./... && go test ./... -count=1
```

Expected: build and tests pass on the unmodified codebase.

- [ ] **Step 2: Rewrite `go.mod` module path**

Edit `go.mod` line 1:

```
module github.com/basti-fantasti/bossy
```

- [ ] **Step 3: Sweep import paths**

```bash
grep -rl "github.com/hashload/boss" --include="*.go" . \
  | xargs sed -i 's|github.com/hashload/boss|github.com/basti-fantasti/bossy|g'
```

Verify nothing remains:

```bash
grep -rn "github.com/hashload/boss" --include="*.go" .
```

Expected: no output.

- [ ] **Step 4: Update Makefile**

Edit `Makefile`:

```makefile
BINNAME     ?= bossy
```

And the two ldflags lines (currently lines 32 and 40-41):

```makefile
LDFLAGS += -X github.com/basti-fantasti/bossy/internal/version.version=${BINARY_VERSION}
LDFLAGS += -X github.com/basti-fantasti/bossy/internal/version.metadata=${VERSION_METADATA}
LDFLAGS += -X github.com/basti-fantasti/bossy/internal/version.gitCommit=${GIT_COMMIT}
```

- [ ] **Step 5: Build and test**

```bash
go mod tidy
go build ./...
go test ./... -count=1
```

Expected: green. If any test references `hashload/boss` in a fixture, fix it inline.

- [ ] **Step 6: Verify binary name**

```bash
make build
ls -la bin/bossy
./bin/bossy --version
```

Expected: `bin/bossy` exists; `--version` prints something.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor: rename module to github.com/basti-fantasti/bossy and binary to bossy"
```

---

## Task 2: Branding constants and self-update target

**Files:**
- Create: `pkg/consts/branding.go`
- Modify: `internal/upgrade/upgrade.go`
- Modify: `internal/upgrade/github.go` (asset-name builder lives there or in `upgrade.go` — verify)
- Modify: `pkg/consts/consts.go` (rename `FolderBossHome`, remove `GitProtocolSSH`)

- [ ] **Step 1: Create the branding constants file**

Create `pkg/consts/branding.go`:

```go
// Package consts — branding holds fork-identifying constants.
// Consolidated here to minimize the merge-conflict surface when
// cherry-picking changes from upstream HashLoad/boss.
package consts

const (
    // BinaryName is the user-facing name of the CLI binary.
    BinaryName = "bossy"

    // GithubOrganization is the GitHub org/user that hosts the fork's releases.
    GithubOrganization = "Basti-Fantasti"

    // GithubRepository is the fork's GitHub repository name (releases live here).
    GithubRepository = "bossy"

    // ReleaseAssetPrefix is the prefix for binary release artifact names.
    // Asset filenames are formatted as: <prefix>-<os>-<arch>.<ext>.
    ReleaseAssetPrefix = "bossy"

    // UserHomeDir is the per-user config directory under the user's home.
    // Migration from the legacy ".boss" directory happens on first run.
    UserHomeDir = ".bossy"

    // LegacyUserHomeDir is the upstream's per-user config directory.
    // Used only by the migrator to detect a one-time migration source.
    LegacyUserHomeDir = ".boss"
)
```

- [ ] **Step 2: Replace `FolderBossHome` references with `UserHomeDir`**

Edit `pkg/consts/consts.go`:

Replace the line:
```go
FolderBossHome = ".boss"
```
with: (delete it — superseded by `branding.go`)

Then sweep:
```bash
grep -rln "consts.FolderBossHome" --include="*.go" . \
  | xargs sed -i 's|consts.FolderBossHome|consts.UserHomeDir|g'
```

Verify:
```bash
grep -rn "FolderBossHome" --include="*.go" .   # should be empty
go build ./...
```

- [ ] **Step 3: Remove `GitProtocolSSH` constant**

Edit `pkg/consts/consts.go`, delete the line:
```go
GitProtocolSSH = "ssh"
```

This will break references — that's expected; we will fix them in Task 14 when we remove the `:ssh` suffix logic. For now, temporarily inline the constant where it's referenced:

```bash
grep -rln "consts.GitProtocolSSH" --include="*.go" . \
  | xargs sed -i 's|consts.GitProtocolSSH|"ssh"|g'
go build ./...
```

- [ ] **Step 4: Wire branding constants into the upgrade package**

Edit `internal/upgrade/upgrade.go`:

Replace:
```go
const (
    githubOrganization = "HashLoad"
    githubRepository   = "boss"
)
```
with:
```go
import (
    // ... existing imports ...
    "github.com/basti-fantasti/bossy/pkg/consts"
)

var (
    githubOrganization = consts.GithubOrganization
    githubRepository   = consts.GithubRepository
)
```

Edit `internal/upgrade/upgrade.go` `getAssetName()`:

Replace:
```go
return fmt.Sprintf("boss-%s-%s.%s", runtime.GOOS, runtime.GOARCH, ext)
```
with:
```go
return fmt.Sprintf("%s-%s-%s.%s", consts.ReleaseAssetPrefix, runtime.GOOS, runtime.GOARCH, ext)
```

- [ ] **Step 5: Update upgrade tests**

```bash
go test ./internal/upgrade/... -v
```

If any test asserts the literal string `"boss-..."`, update it to `"bossy-..."`.

- [ ] **Step 6: Build, lint, commit**

```bash
go build ./...
go test ./... -count=1
go tool golangci-lint run
git add -A
git commit -m "refactor: extract branding constants and point self-update at fork"
```

---

## Task 3: Auth package — types and skeleton

**Files:**
- Create: `internal/core/services/auth/auth.go`
- Create: `internal/core/services/auth/auth_test.go`

This task introduces the package and the types every layer references. No layer logic yet.

- [ ] **Step 1: Write the failing test**

Create `internal/core/services/auth/auth_test.go`:

```go
package auth

import "testing"

func TestTransportString(t *testing.T) {
    if TransportSSH.String() != "ssh" {
        t.Errorf("TransportSSH.String() = %q, want \"ssh\"", TransportSSH.String())
    }
    if TransportHTTPS.String() != "https" {
        t.Errorf("TransportHTTPS.String() = %q, want \"https\"", TransportHTTPS.String())
    }
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test ./internal/core/services/auth/... -v
```

Expected: build error, package does not exist yet.

- [ ] **Step 3: Create the package**

Create `internal/core/services/auth/auth.go`:

```go
// Package auth resolves the protocol, URL, and credentials bossy uses
// to clone or fetch a dependency. The resolution chain is documented in
// docs/superpowers/specs/2026-05-05-bossy-fork-design.md §2.
package auth

import "github.com/basti-fantasti/bossy/internal/core/domain"

// Transport identifies which underlying git client bossy will use.
type Transport int

const (
    // TransportHTTPS clones over HTTPS using the embedded go-git client.
    TransportHTTPS Transport = iota
    // TransportSSH clones over SSH by shelling out to the system git binary.
    TransportSSH
)

// String returns the lowercase name of the transport ("https" or "ssh").
func (t Transport) String() string {
    switch t {
    case TransportSSH:
        return "ssh"
    case TransportHTTPS:
        return "https"
    default:
        return "unknown"
    }
}

// CredentialSpec carries optional HTTPS basic-auth credentials. Empty
// (zero value) means no credentials should be sent. SSH transports do
// not use this — credentials come from ssh-agent / ~/.ssh/config via
// the system git binary.
type CredentialSpec struct {
    User     string
    Password string
}

// Decision is the output of Resolve: how bossy should clone the dep.
type Decision struct {
    URL        string
    Transport  Transport
    Credential CredentialSpec
    // Layer is the human-readable name of the resolution layer that
    // produced this decision, used in debug logs and error messages.
    Layer string
}

// Resolve walks the precedence chain and returns the Decision for the
// given dependency. Implementation is filled in by later tasks; this
// stub exists so call sites can be wired now.
func Resolve(dep domain.Dependency) (Decision, error) {
    return Decision{}, nil
}
```

- [ ] **Step 4: Run the test to confirm it passes**

```bash
go test ./internal/core/services/auth/... -v
```

Expected: `PASS: TestTransportString`.

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/auth/
git commit -m "feat(auth): add Decision/Transport types and package skeleton"
```

---

## Task 4: URL parsing — classify dep keys

**Files:**
- Create: `internal/core/services/auth/parse.go`
- Create: `internal/core/services/auth/parse_test.go`

The parser is a pure function. It must handle bare names, `host/path`, `git@host:path[.git]`, and `https://host/path[.git]`. It also produces a canonical `host/path` form used for cache hashing and as the input to URL rewriting.

- [ ] **Step 1: Write the failing tests**

Create `internal/core/services/auth/parse_test.go`:

```go
package auth

import "testing"

func TestParseDepURL(t *testing.T) {
    tests := []struct {
        name           string
        input          string
        wantKind       URLKind
        wantHost       string
        wantPath       string
        wantCanonical  string
    }{
        {
            name:          "bare name",
            input:         "horse",
            wantKind:      URLKindBare,
            wantHost:      "",
            wantPath:      "horse",
            wantCanonical: "horse",
        },
        {
            name:          "host slash path",
            input:         "gitlab.mydomain.com/group/repo",
            wantKind:      URLKindHostPath,
            wantHost:      "gitlab.mydomain.com",
            wantPath:      "group/repo",
            wantCanonical: "gitlab.mydomain.com/group/repo",
        },
        {
            name:          "git@ ssh form",
            input:         "git@gitlab.mydomain.com:group/repo.git",
            wantKind:      URLKindSSH,
            wantHost:      "gitlab.mydomain.com",
            wantPath:      "group/repo",
            wantCanonical: "gitlab.mydomain.com/group/repo",
        },
        {
            name:          "git@ ssh form without .git",
            input:         "git@gitlab.mydomain.com:group/repo",
            wantKind:      URLKindSSH,
            wantHost:      "gitlab.mydomain.com",
            wantPath:      "group/repo",
            wantCanonical: "gitlab.mydomain.com/group/repo",
        },
        {
            name:          "https url",
            input:         "https://github.com/HashLoad/horse.git",
            wantKind:      URLKindHTTPS,
            wantHost:      "github.com",
            wantPath:      "HashLoad/horse",
            wantCanonical: "github.com/HashLoad/horse",
        },
        {
            name:          "http url",
            input:         "http://gitlab.mydomain.com/group/repo",
            wantKind:      URLKindHTTPS,
            wantHost:      "gitlab.mydomain.com",
            wantPath:      "group/repo",
            wantCanonical: "gitlab.mydomain.com/group/repo",
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ParseDepURL(tt.input)
            if err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            if got.Kind != tt.wantKind {
                t.Errorf("Kind = %v, want %v", got.Kind, tt.wantKind)
            }
            if got.Host != tt.wantHost {
                t.Errorf("Host = %q, want %q", got.Host, tt.wantHost)
            }
            if got.Path != tt.wantPath {
                t.Errorf("Path = %q, want %q", got.Path, tt.wantPath)
            }
            if got.Canonical != tt.wantCanonical {
                t.Errorf("Canonical = %q, want %q", got.Canonical, tt.wantCanonical)
            }
        })
    }
}

func TestParseDepURL_Invalid(t *testing.T) {
    cases := []string{"", "git@no-colon-form", "https://"}
    for _, c := range cases {
        if _, err := ParseDepURL(c); err == nil {
            t.Errorf("ParseDepURL(%q) returned no error", c)
        }
    }
}
```

- [ ] **Step 2: Run test, expect failure**

```bash
go test ./internal/core/services/auth -run TestParseDepURL -v
```

Expected: build error (no `ParseDepURL`).

- [ ] **Step 3: Implement the parser**

Create `internal/core/services/auth/parse.go`:

```go
package auth

import (
    "errors"
    "regexp"
    "strings"
)

// URLKind enumerates the recognized forms of a dependency key.
type URLKind int

const (
    // URLKindBare is a name without a slash, resolved against the default org.
    URLKindBare URLKind = iota
    // URLKindHostPath is the canonical "host/path" form (the upstream default).
    URLKindHostPath
    // URLKindSSH is the explicit "git@host:path[.git]" SSH form.
    URLKindSSH
    // URLKindHTTPS is an explicit "http(s)://host/path[.git]" URL.
    URLKindHTTPS
)

// ParsedURL is the result of classifying a dependency key.
type ParsedURL struct {
    Kind      URLKind
    Host      string // empty for URLKindBare
    Path      string // owner/repo or just a name (bare)
    Canonical string // host/path or bare name; used for cache hashing
}

var (
    reGitAt = regexp.MustCompile(`^git@([^:]+):(.+?)(?:\.git)?$`)
    reHTTP  = regexp.MustCompile(`^https?://([^/]+)/(.+?)(?:\.git)?$`)
)

// ParseDepURL classifies a dependency key into one of the recognized forms.
// It returns an error if the input is empty or syntactically malformed.
func ParseDepURL(s string) (ParsedURL, error) {
    s = strings.TrimSpace(s)
    if s == "" {
        return ParsedURL{}, errors.New("empty dependency URL")
    }

    if m := reGitAt.FindStringSubmatch(s); m != nil {
        host, path := m[1], m[2]
        if host == "" || path == "" {
            return ParsedURL{}, errors.New("invalid SSH URL: missing host or path")
        }
        return ParsedURL{
            Kind:      URLKindSSH,
            Host:      host,
            Path:      path,
            Canonical: host + "/" + path,
        }, nil
    }

    if m := reHTTP.FindStringSubmatch(s); m != nil {
        host, path := m[1], m[2]
        if host == "" || path == "" {
            return ParsedURL{}, errors.New("invalid HTTPS URL: missing host or path")
        }
        return ParsedURL{
            Kind:      URLKindHTTPS,
            Host:      host,
            Path:      path,
            Canonical: host + "/" + path,
        }, nil
    }

    if strings.HasPrefix(s, "git@") {
        return ParsedURL{}, errors.New("invalid SSH URL: expected git@host:path form")
    }

    if strings.Contains(s, "/") {
        idx := strings.Index(s, "/")
        host := s[:idx]
        path := strings.TrimSuffix(s[idx+1:], ".git")
        if path == "" {
            return ParsedURL{}, errors.New("invalid host/path: missing path")
        }
        return ParsedURL{
            Kind:      URLKindHostPath,
            Host:      host,
            Path:      path,
            Canonical: host + "/" + path,
        }, nil
    }

    return ParsedURL{
        Kind:      URLKindBare,
        Path:      s,
        Canonical: s,
    }, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/core/services/auth -run TestParseDepURL -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/auth/parse.go internal/core/services/auth/parse_test.go
git commit -m "feat(auth): add dep URL classifier (bare/host-path/git@/https)"
```

---

## Task 5: GitLab CI auto-detection layer

**Files:**
- Create: `internal/core/services/auth/gitlab_ci.go`
- Create: `internal/core/services/auth/gitlab_ci_test.go`

Highest-priority layer. Reads `GITLAB_CI`, `CI_SERVER_HOST`, `CI_JOB_TOKEN` from env. Returns a Decision iff all three are present and the dep host matches.

- [ ] **Step 1: Write the failing tests**

Create `internal/core/services/auth/gitlab_ci_test.go`:

```go
package auth

import "testing"

func TestGitLabCI_Match(t *testing.T) {
    t.Setenv("GITLAB_CI", "true")
    t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
    t.Setenv("CI_JOB_TOKEN", "secret-token")

    parsed := ParsedURL{Kind: URLKindHostPath, Host: "gitlab.mydomain.com", Path: "group/repo", Canonical: "gitlab.mydomain.com/group/repo"}
    d, ok := tryGitLabCI(parsed)
    if !ok {
        t.Fatal("expected gitlab CI layer to match")
    }
    want := "https://gitlab-ci-token:secret-token@gitlab.mydomain.com/group/repo"
    if d.URL != want {
        t.Errorf("URL = %q, want %q", d.URL, want)
    }
    if d.Transport != TransportHTTPS {
        t.Errorf("Transport = %v, want HTTPS", d.Transport)
    }
    if d.Layer != "gitlab-ci" {
        t.Errorf("Layer = %q, want \"gitlab-ci\"", d.Layer)
    }
}

func TestGitLabCI_OverridesSSHForm(t *testing.T) {
    t.Setenv("GITLAB_CI", "true")
    t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
    t.Setenv("CI_JOB_TOKEN", "tok")
    parsed := ParsedURL{Kind: URLKindSSH, Host: "gitlab.mydomain.com", Path: "group/repo", Canonical: "gitlab.mydomain.com/group/repo"}
    d, ok := tryGitLabCI(parsed)
    if !ok {
        t.Fatal("expected gitlab CI to override SSH form")
    }
    if d.URL != "https://gitlab-ci-token:tok@gitlab.mydomain.com/group/repo" {
        t.Errorf("unexpected URL: %s", d.URL)
    }
}

func TestGitLabCI_NotInCI(t *testing.T) {
    t.Setenv("GITLAB_CI", "")
    parsed := ParsedURL{Kind: URLKindHostPath, Host: "gitlab.mydomain.com", Path: "g/r", Canonical: "gitlab.mydomain.com/g/r"}
    if _, ok := tryGitLabCI(parsed); ok {
        t.Fatal("should not match outside CI")
    }
}

func TestGitLabCI_HostMismatch(t *testing.T) {
    t.Setenv("GITLAB_CI", "true")
    t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
    t.Setenv("CI_JOB_TOKEN", "tok")
    parsed := ParsedURL{Kind: URLKindHostPath, Host: "github.com", Path: "x/y", Canonical: "github.com/x/y"}
    if _, ok := tryGitLabCI(parsed); ok {
        t.Fatal("should not match for non-CI-server host")
    }
}

func TestGitLabCI_MissingToken_FallsThrough(t *testing.T) {
    t.Setenv("GITLAB_CI", "true")
    t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
    t.Setenv("CI_JOB_TOKEN", "")
    parsed := ParsedURL{Kind: URLKindHostPath, Host: "gitlab.mydomain.com", Path: "g/r", Canonical: "gitlab.mydomain.com/g/r"}
    if _, ok := tryGitLabCI(parsed); ok {
        t.Fatal("missing token must skip layer, not produce malformed URL")
    }
}

func TestGitLabCI_BareDep_NotMatched(t *testing.T) {
    t.Setenv("GITLAB_CI", "true")
    t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
    t.Setenv("CI_JOB_TOKEN", "tok")
    parsed := ParsedURL{Kind: URLKindBare, Path: "horse", Canonical: "horse"}
    if _, ok := tryGitLabCI(parsed); ok {
        t.Fatal("bare deps have no host, must skip CI layer")
    }
}
```

- [ ] **Step 2: Run, expect failure**

```bash
go test ./internal/core/services/auth -run TestGitLabCI -v
```

Expected: build error (no `tryGitLabCI`).

- [ ] **Step 3: Implement**

Create `internal/core/services/auth/gitlab_ci.go`:

```go
package auth

import "os"

// tryGitLabCI implements the highest-priority resolution layer:
// when running under a GitLab Runner with a CI job token, rewrite
// any dep URL on the same GitLab instance to authenticated HTTPS.
//
// All three env vars (GITLAB_CI, CI_SERVER_HOST, CI_JOB_TOKEN) must
// be non-empty AND the dep host must match CI_SERVER_HOST. Missing
// any of them means we skip this layer rather than produce a
// half-formed URL.
func tryGitLabCI(parsed ParsedURL) (Decision, bool) {
    if os.Getenv("GITLAB_CI") != "true" {
        return Decision{}, false
    }
    serverHost := os.Getenv("CI_SERVER_HOST")
    token := os.Getenv("CI_JOB_TOKEN")
    if serverHost == "" || token == "" {
        return Decision{}, false
    }
    if parsed.Host == "" || parsed.Host != serverHost {
        return Decision{}, false
    }
    url := "https://gitlab-ci-token:" + token + "@" + parsed.Host + "/" + parsed.Path
    return Decision{
        URL:        url,
        Transport:  TransportHTTPS,
        Credential: CredentialSpec{}, // baked into URL
        Layer:      "gitlab-ci",
    }, true
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/core/services/auth -run TestGitLabCI -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/auth/gitlab_ci.go internal/core/services/auth/gitlab_ci_test.go
git commit -m "feat(auth): add GitLab CI auto-detection layer"
```

---

## Task 6: Generic env-var override layer

**Files:**
- Create: `internal/core/services/auth/envvar.go`
- Create: `internal/core/services/auth/envvar_test.go`

`BOSSY_AUTH_<HOST>` where `<HOST>` is the dep host upper-cased with `.` and `-` replaced by `_`. Value formats:

- `https-token:<user>:<token>` → HTTPS basic auth
- `ssh` → force system-git SSH, no creds (agent handles them)

- [ ] **Step 1: Write failing tests**

Create `internal/core/services/auth/envvar_test.go`:

```go
package auth

import "testing"

func TestEnvVarHTTPSToken(t *testing.T) {
    t.Setenv("BOSSY_AUTH_GITLAB_MYDOMAIN_COM", "https-token:gitlab-ci-token:abc123")
    parsed := ParsedURL{Kind: URLKindHostPath, Host: "gitlab.mydomain.com", Path: "g/r", Canonical: "gitlab.mydomain.com/g/r"}
    d, ok := tryEnvVar(parsed)
    if !ok {
        t.Fatal("expected env var layer to match")
    }
    if d.URL != "https://gitlab.mydomain.com/g/r" {
        t.Errorf("URL = %q", d.URL)
    }
    if d.Transport != TransportHTTPS {
        t.Errorf("Transport = %v", d.Transport)
    }
    if d.Credential.User != "gitlab-ci-token" || d.Credential.Password != "abc123" {
        t.Errorf("Credential = %+v", d.Credential)
    }
}

func TestEnvVarSSH(t *testing.T) {
    t.Setenv("BOSSY_AUTH_GITLAB_MYDOMAIN_COM", "ssh")
    parsed := ParsedURL{Kind: URLKindHostPath, Host: "gitlab.mydomain.com", Path: "g/r", Canonical: "gitlab.mydomain.com/g/r"}
    d, ok := tryEnvVar(parsed)
    if !ok {
        t.Fatal("expected env var layer to match")
    }
    if d.Transport != TransportSSH {
        t.Errorf("Transport = %v, want SSH", d.Transport)
    }
    if d.URL != "git@gitlab.mydomain.com:g/r" {
        t.Errorf("URL = %q", d.URL)
    }
}

func TestEnvVar_Missing(t *testing.T) {
    parsed := ParsedURL{Kind: URLKindHostPath, Host: "gitlab.mydomain.com", Path: "g/r"}
    if _, ok := tryEnvVar(parsed); ok {
        t.Fatal("expected no match when env unset")
    }
}

func TestEnvVar_BareDep_NoMatch(t *testing.T) {
    t.Setenv("BOSSY_AUTH_HORSE", "ssh")
    parsed := ParsedURL{Kind: URLKindBare, Path: "horse"}
    if _, ok := tryEnvVar(parsed); ok {
        t.Fatal("bare deps have no host")
    }
}

func TestEnvVarHostKey(t *testing.T) {
    cases := map[string]string{
        "gitlab.mydomain.com": "BOSSY_AUTH_GITLAB_MYDOMAIN_COM",
        "github.com":    "BOSSY_AUTH_GITHUB_COM",
        "git.example":   "BOSSY_AUTH_GIT_EXAMPLE",
    }
    for host, want := range cases {
        if got := envVarKey(host); got != want {
            t.Errorf("envVarKey(%q) = %q, want %q", host, got, want)
        }
    }
}
```

- [ ] **Step 2: Run, expect fail**

```bash
go test ./internal/core/services/auth -run TestEnvVar -v
```

- [ ] **Step 3: Implement**

Create `internal/core/services/auth/envvar.go`:

```go
package auth

import (
    "os"
    "strings"
)

// envVarKey returns the env var name bossy looks up for a given host.
// "gitlab.mydomain.com" → "BOSSY_AUTH_GITLAB_MYDOMAIN_COM".
func envVarKey(host string) string {
    upper := strings.ToUpper(host)
    upper = strings.ReplaceAll(upper, ".", "_")
    upper = strings.ReplaceAll(upper, "-", "_")
    return "BOSSY_AUTH_" + upper
}

// tryEnvVar implements layer 2: BOSSY_AUTH_<HOST> override.
//
// Supported formats:
//   https-token:<user>:<token>   → HTTPS basic auth
//   ssh                          → force SSH (system git uses agent/config)
func tryEnvVar(parsed ParsedURL) (Decision, bool) {
    if parsed.Host == "" {
        return Decision{}, false
    }
    raw := os.Getenv(envVarKey(parsed.Host))
    if raw == "" {
        return Decision{}, false
    }

    if raw == "ssh" {
        return Decision{
            URL:       "git@" + parsed.Host + ":" + parsed.Path,
            Transport: TransportSSH,
            Layer:     "envvar",
        }, true
    }

    if rest, ok := strings.CutPrefix(raw, "https-token:"); ok {
        idx := strings.Index(rest, ":")
        if idx <= 0 || idx == len(rest)-1 {
            return Decision{}, false
        }
        return Decision{
            URL:        "https://" + parsed.Host + "/" + parsed.Path,
            Transport:  TransportHTTPS,
            Credential: CredentialSpec{User: rest[:idx], Password: rest[idx+1:]},
            Layer:      "envvar",
        }, true
    }

    return Decision{}, false
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/core/services/auth -run TestEnvVar -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/auth/envvar.go internal/core/services/auth/envvar_test.go
git commit -m "feat(auth): add BOSSY_AUTH_<HOST> env var override layer"
```

---

## Task 7: Per-host config layer

**Files:**
- Modify: `pkg/env/configuration.go` — add `HostProtocols map[string]string` field
- Create: `internal/core/services/auth/hostconfig.go`
- Create: `internal/core/services/auth/hostconfig_test.go`

A user can pin a per-host default with `bossy config git protocol <host> ssh|https` (CLI added in a later task). This layer reads that config.

- [ ] **Step 1: Add config field**

Edit `pkg/env/configuration.go`. After the `GitShallow` field, add:

```go
HostProtocols map[string]string `json:"host_protocols,omitempty"`
```

Build:

```bash
go build ./...
```

- [ ] **Step 2: Write failing tests**

Create `internal/core/services/auth/hostconfig_test.go`:

```go
package auth

import "testing"

func TestHostConfig_SSH(t *testing.T) {
    cfg := map[string]string{"gitlab.mydomain.com": "ssh"}
    parsed := ParsedURL{Host: "gitlab.mydomain.com", Path: "g/r"}
    d, ok := tryHostConfig(parsed, cfg)
    if !ok {
        t.Fatal("expected match")
    }
    if d.Transport != TransportSSH {
        t.Errorf("Transport = %v", d.Transport)
    }
    if d.URL != "git@gitlab.mydomain.com:g/r" {
        t.Errorf("URL = %q", d.URL)
    }
}

func TestHostConfig_HTTPS(t *testing.T) {
    cfg := map[string]string{"github.com": "https"}
    parsed := ParsedURL{Host: "github.com", Path: "x/y"}
    d, ok := tryHostConfig(parsed, cfg)
    if !ok {
        t.Fatal("expected match")
    }
    if d.Transport != TransportHTTPS || d.URL != "https://github.com/x/y" {
        t.Errorf("got %+v", d)
    }
}

func TestHostConfig_Missing(t *testing.T) {
    parsed := ParsedURL{Host: "gitlab.mydomain.com", Path: "g/r"}
    if _, ok := tryHostConfig(parsed, nil); ok {
        t.Fatal("nil map must not match")
    }
    if _, ok := tryHostConfig(parsed, map[string]string{"other.host": "ssh"}); ok {
        t.Fatal("non-matching host must not match")
    }
}

func TestHostConfig_InvalidValue(t *testing.T) {
    parsed := ParsedURL{Host: "gitlab.mydomain.com", Path: "g/r"}
    if _, ok := tryHostConfig(parsed, map[string]string{"gitlab.mydomain.com": "bogus"}); ok {
        t.Fatal("invalid value must not match (caller falls through)")
    }
}
```

- [ ] **Step 3: Implement**

Create `internal/core/services/auth/hostconfig.go`:

```go
package auth

// tryHostConfig implements layer 4: per-host protocol config from
// ~/.bossy/config.json. The cfg map is host → "ssh"|"https" pairs as
// stored by `bossy config git protocol`.
func tryHostConfig(parsed ParsedURL, cfg map[string]string) (Decision, bool) {
    if parsed.Host == "" || cfg == nil {
        return Decision{}, false
    }
    proto, ok := cfg[parsed.Host]
    if !ok {
        return Decision{}, false
    }
    switch proto {
    case "ssh":
        return Decision{
            URL:       "git@" + parsed.Host + ":" + parsed.Path,
            Transport: TransportSSH,
            Layer:     "host-config",
        }, true
    case "https":
        return Decision{
            URL:       "https://" + parsed.Host + "/" + parsed.Path,
            Transport: TransportHTTPS,
            Layer:     "host-config",
        }, true
    default:
        return Decision{}, false
    }
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/core/services/auth -run TestHostConfig -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/env/configuration.go internal/core/services/auth/hostconfig.go internal/core/services/auth/hostconfig_test.go
git commit -m "feat(auth): add per-host protocol config layer"
```

---

## Task 8: Stored HTTPS credentials layer

**Files:**
- Create: `internal/core/services/auth/stored.go`
- Create: `internal/core/services/auth/stored_test.go`
- Modify: `pkg/env/configuration.go` — narrow `Auth` struct

The stored-creds layer only handles HTTPS. SSH credentials no longer exist as a concept (the system git binary owns that).

- [ ] **Step 1: Narrow `Auth` struct**

Edit `pkg/env/configuration.go`. Replace the `Auth` struct with:

```go
// Auth represents stored HTTPS basic-auth credentials for a given host.
// SSH credentials are no longer stored — they come from ssh-agent and
// ~/.ssh/config via the system git binary.
type Auth struct {
    User string `json:"user,omitempty"`
    Pass string `json:"pass,omitempty"`
}
```

Delete the now-unused methods on `Auth` if any reference removed fields (SSH key path, passphrase). Keep `GetUser`, `GetPassword`, `SetUser`, `SetPass` (the encrypted accessors). Delete `GetPassPhrase`, `SetPassPhrase`, and any reference to `Path`/`PassPhrase`/`UseSSH` fields.

Then delete the entire `GetAuth(repo string) transport.AuthMethod` method on `Configuration` (lines ~109–137 in the current file). Remove the now-unused imports (`transport`, `http`, `sshGit`, `ssh`).

Edit `pkg/env/interfaces.go`. Remove `GetAuth(repo string) transport.AuthMethod` from the `ConfigProvider` interface and the unused `transport` import.

Build:

```bash
go build ./...
```

This **will** break callers — that's expected. They're fixed in Tasks 9 (orchestrator) and 12 (git adapters). For now, fix any compilation error by stubbing the call sites with `// TODO(task-9): wire to auth.Resolve` returning `nil`. Concretely, in `internal/adapters/secondary/git/git.go` and `git_embedded.go`, replace `config.GetAuth(dep.GetURLPrefix())` with `nil` (those calls go away in Task 12). After this step the build must succeed even if behavior is degraded.

- [ ] **Step 2: Write failing tests for stored layer**

Create `internal/core/services/auth/stored_test.go`:

```go
package auth

import "testing"

// fakeStore is a test double for the credential store interface.
type fakeStore map[string]struct{ User, Pass string }

func (f fakeStore) GetHTTPSCredentials(host string) (user, pass string, ok bool) {
    c, present := f[host]
    if !present {
        return "", "", false
    }
    return c.User, c.Pass, true
}

func TestStored_Match(t *testing.T) {
    s := fakeStore{"gitlab.mydomain.com": {User: "alice", Pass: "hunter2"}}
    parsed := ParsedURL{Host: "gitlab.mydomain.com", Path: "g/r"}
    d, ok := tryStored(parsed, s)
    if !ok {
        t.Fatal("expected stored layer to match")
    }
    if d.URL != "https://gitlab.mydomain.com/g/r" || d.Transport != TransportHTTPS {
        t.Errorf("Decision = %+v", d)
    }
    if d.Credential.User != "alice" || d.Credential.Password != "hunter2" {
        t.Errorf("Credential = %+v", d.Credential)
    }
}

func TestStored_NoMatch(t *testing.T) {
    parsed := ParsedURL{Host: "gitlab.mydomain.com", Path: "g/r"}
    if _, ok := tryStored(parsed, fakeStore{}); ok {
        t.Fatal("expected no match")
    }
}

func TestStored_BareDep(t *testing.T) {
    parsed := ParsedURL{Kind: URLKindBare, Path: "horse"}
    s := fakeStore{"github.com": {User: "u", Pass: "p"}}
    if _, ok := tryStored(parsed, s); ok {
        t.Fatal("bare deps have no host; layer cannot match")
    }
}
```

- [ ] **Step 3: Implement**

Create `internal/core/services/auth/stored.go`:

```go
package auth

// CredentialStore returns stored HTTPS basic-auth credentials for a host.
// Implemented by env.Configuration in production and by test doubles
// in unit tests.
type CredentialStore interface {
    GetHTTPSCredentials(host string) (user, pass string, ok bool)
}

// tryStored implements layer 5: HTTPS credentials saved via `bossy auth set`.
func tryStored(parsed ParsedURL, store CredentialStore) (Decision, bool) {
    if parsed.Host == "" || store == nil {
        return Decision{}, false
    }
    user, pass, ok := store.GetHTTPSCredentials(parsed.Host)
    if !ok {
        return Decision{}, false
    }
    return Decision{
        URL:        "https://" + parsed.Host + "/" + parsed.Path,
        Transport:  TransportHTTPS,
        Credential: CredentialSpec{User: user, Password: pass},
        Layer:      "stored",
    }, true
}
```

- [ ] **Step 4: Implement `GetHTTPSCredentials` on Configuration**

Edit `pkg/env/configuration.go`. After the existing `Auth` struct methods, add:

```go
// GetHTTPSCredentials returns stored HTTPS basic-auth for a host, or
// (false) if none. Implements auth.CredentialStore.
func (c *Configuration) GetHTTPSCredentials(host string) (string, string, bool) {
    a, ok := c.Auth[host]
    if !ok || a == nil {
        return "", "", false
    }
    return a.GetUser(), a.GetPassword(), true
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/core/services/auth -run TestStored -v
go build ./...
```

Expected: PASS, build green.

- [ ] **Step 6: Commit**

```bash
git add pkg/env/ internal/core/services/auth/stored.go internal/core/services/auth/stored_test.go internal/adapters/secondary/git/
git commit -m "feat(auth): add stored HTTPS credentials layer; narrow Auth struct"
```

---

## Task 9: Resolve orchestrator

**Files:**
- Modify: `internal/core/services/auth/auth.go` — replace stub `Resolve`
- Modify: `internal/core/services/auth/auth_test.go` — add orchestrator tests

The orchestrator wires the layers in priority order. It also handles bare names (no host) by treating them as a `URLKindBare` that falls through every host-keyed layer to the default.

- [ ] **Step 1: Write failing test**

Append to `internal/core/services/auth/auth_test.go`:

```go
import (
    "testing"

    "github.com/basti-fantasti/bossy/internal/core/domain"
)

func TestResolve_DefaultHTTPS(t *testing.T) {
    // No env, no stored creds, no host config → default HTTPS, no auth.
    dep := domain.Dependency{Repository: "github.com/HashLoad/horse"}
    d, err := ResolveWith(dep, fakeStore{}, nil)
    if err != nil {
        t.Fatalf("err: %v", err)
    }
    if d.URL != "https://github.com/HashLoad/horse" {
        t.Errorf("URL = %q", d.URL)
    }
    if d.Transport != TransportHTTPS {
        t.Errorf("Transport = %v", d.Transport)
    }
    if d.Layer != "default" {
        t.Errorf("Layer = %q", d.Layer)
    }
}

func TestResolve_BareName_DefaultOrg(t *testing.T) {
    dep := domain.Dependency{Repository: "horse"}
    d, err := ResolveWith(dep, fakeStore{}, nil)
    if err != nil {
        t.Fatalf("err: %v", err)
    }
    if d.URL != "https://github.com/hashload/horse" {
        t.Errorf("URL = %q (bare names default to upstream org)", d.URL)
    }
}

func TestResolve_GitLabCI_BeatsExplicitSSH(t *testing.T) {
    t.Setenv("GITLAB_CI", "true")
    t.Setenv("CI_SERVER_HOST", "gitlab.mydomain.com")
    t.Setenv("CI_JOB_TOKEN", "tok")
    dep := domain.Dependency{Repository: "git@gitlab.mydomain.com:g/r.git"}
    d, err := ResolveWith(dep, fakeStore{}, nil)
    if err != nil {
        t.Fatalf("err: %v", err)
    }
    if d.Transport != TransportHTTPS {
        t.Errorf("CI must coerce SSH form to HTTPS; got %v", d.Transport)
    }
    if d.URL != "https://gitlab-ci-token:tok@gitlab.mydomain.com/g/r" {
        t.Errorf("URL = %q", d.URL)
    }
    if d.Layer != "gitlab-ci" {
        t.Errorf("Layer = %q", d.Layer)
    }
}

func TestResolve_ExplicitSSH_NoCI(t *testing.T) {
    dep := domain.Dependency{Repository: "git@gitlab.mydomain.com:g/r.git"}
    d, err := ResolveWith(dep, fakeStore{}, nil)
    if err != nil {
        t.Fatalf("err: %v", err)
    }
    if d.Transport != TransportSSH {
        t.Errorf("Transport = %v, want SSH", d.Transport)
    }
    if d.URL != "git@gitlab.mydomain.com:g/r" {
        t.Errorf("URL = %q", d.URL)
    }
    if d.Layer != "explicit" {
        t.Errorf("Layer = %q", d.Layer)
    }
}

func TestResolve_Stored_BeatsDefault(t *testing.T) {
    dep := domain.Dependency{Repository: "gitlab.mydomain.com/g/r"}
    store := fakeStore{"gitlab.mydomain.com": {User: "alice", Pass: "secret"}}
    d, err := ResolveWith(dep, store, nil)
    if err != nil {
        t.Fatalf("err: %v", err)
    }
    if d.Layer != "stored" || d.Credential.User != "alice" {
        t.Errorf("got %+v", d)
    }
}
```

- [ ] **Step 2: Replace the stub `Resolve` and add `ResolveWith`**

Edit `internal/core/services/auth/auth.go`. Replace `Resolve` with:

```go
const defaultBareOrg = "github.com/hashload/"

// Resolve uses the live env.Configuration as the credential/host-config source.
// Most callers should use this. Tests use ResolveWith to inject doubles.
func Resolve(dep domain.Dependency) (Decision, error) {
    cfg := env.GlobalConfiguration()
    return ResolveWith(dep, cfg, cfg.HostProtocols)
}

// ResolveWith is the testable form of Resolve. It walks the precedence
// chain documented in the design spec §2.
func ResolveWith(dep domain.Dependency, store CredentialStore, hostCfg map[string]string) (Decision, error) {
    repo := dep.Repository
    parsed, err := ParseDepURL(repo)
    if err != nil {
        return Decision{}, err
    }

    // Bare names resolve against the default org and become host/path.
    if parsed.Kind == URLKindBare {
        parsed, err = ParseDepURL(defaultBareOrg + parsed.Path)
        if err != nil {
            return Decision{}, err
        }
    }

    // Layer 1: GitLab CI auto-detection.
    if d, ok := tryGitLabCI(parsed); ok {
        return d, nil
    }
    // Layer 2: env-var override.
    if d, ok := tryEnvVar(parsed); ok {
        return d, nil
    }
    // Layer 3: per-dep explicit URL.
    if parsed.Kind == URLKindSSH {
        return Decision{
            URL:       "git@" + parsed.Host + ":" + parsed.Path,
            Transport: TransportSSH,
            Layer:     "explicit",
        }, nil
    }
    if parsed.Kind == URLKindHTTPS {
        // Explicit HTTPS may still want stored creds applied.
        if d, ok := tryStored(parsed, store); ok {
            return d, nil
        }
        return Decision{
            URL:       "https://" + parsed.Host + "/" + parsed.Path,
            Transport: TransportHTTPS,
            Layer:     "explicit",
        }, nil
    }
    // Layer 4: per-host config.
    if d, ok := tryHostConfig(parsed, hostCfg); ok {
        return d, nil
    }
    // Layer 5: stored creds.
    if d, ok := tryStored(parsed, store); ok {
        return d, nil
    }
    // Layer 6: default HTTPS, no auth.
    return Decision{
        URL:       "https://" + parsed.Host + "/" + parsed.Path,
        Transport: TransportHTTPS,
        Layer:     "default",
    }, nil
}
```

Add the `env` import:

```go
import (
    "github.com/basti-fantasti/bossy/internal/core/domain"
    "github.com/basti-fantasti/bossy/pkg/env"
)
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/core/services/auth -v
```

Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/core/services/auth/
git commit -m "feat(auth): wire layers into Resolve orchestrator"
```

---

## Task 10: Fix install URL parser bug

**Files:**
- Modify: `internal/core/services/installer/utils.go`
- Modify: `internal/core/services/installer/utils_test.go`

The current `EnsureDependency` mangles `git@host:repo.git` into a malformed key. Reuse the new `auth.ParseDepURL` for canonicalization. Bare names retain the default-org behavior.

- [ ] **Step 1: Add failing test**

Append to `internal/core/services/installer/utils_test.go`:

```go
func TestEnsureDependency_GitAtURL(t *testing.T) {
    pkg := domain.NewPackage()
    EnsureDependency(pkg, []string{"git@gitlab.mydomain.com:group/repo.git"})
    deps := pkg.Dependencies
    if _, ok := deps["git@gitlab.mydomain.com:group/repo"]; !ok {
        t.Errorf("expected dep key to be canonical SSH form without .git; got %v", deps)
    }
}

func TestEnsureDependency_HTTPSURL(t *testing.T) {
    pkg := domain.NewPackage()
    EnsureDependency(pkg, []string{"https://github.com/HashLoad/horse"})
    deps := pkg.Dependencies
    if _, ok := deps["github.com/HashLoad/horse"]; !ok {
        t.Errorf("expected canonical host/path key; got %v", deps)
    }
}
```

If `domain.NewPackage()` does not exist, use whatever zero-value constructor the package exposes; check `internal/core/domain/package.go` for the right idiom and adjust the test accordingly.

- [ ] **Step 2: Run, expect failure**

```bash
go test ./internal/core/services/installer -run TestEnsureDependency -v
```

- [ ] **Step 3: Update `EnsureDependency`**

Edit `internal/core/services/installer/utils.go`. Replace the body of `EnsureDependency`:

```go
import (
    "github.com/basti-fantasti/bossy/internal/core/services/auth"
    // existing imports …
)

func EnsureDependency(pkg *domain.Package, args []string) {
    for _, raw := range args {
        // First: handle the legacy bare-name → default-org expansion.
        expanded := ParseDependency(raw)

        // Then: classify the URL form. For SSH/HTTPS forms we keep the
        // explicit URL as the dep key (preserves protocol intent). For
        // host-path/bare we keep the canonical "host/path" form.
        parsed, err := auth.ParseDepURL(expanded)
        if err != nil {
            continue
        }
        var key string
        switch parsed.Kind {
        case auth.URLKindSSH:
            key = "git@" + parsed.Host + ":" + parsed.Path
        case auth.URLKindHTTPS, auth.URLKindHostPath:
            key = parsed.Canonical
        case auth.URLKindBare:
            key = parsed.Canonical
        }

        // Version handling: split on '@' to grab a tag/version if present.
        // The old regex was over-permissive; keep it simple.
        ver := consts.MinimalDependencyVersion
        if at := strings.LastIndex(raw, "@"); at > 0 && !strings.HasPrefix(raw, "git@") {
            // Skip the leading "git@" of SSH URLs when splitting on @.
            ver = raw[at+1:]
            // The key was already computed from `expanded`, which may
            // include the trailing version; strip it for safety.
        }
        pkg.AddDependency(key, ver)
    }
}
```

Delete the obsolete regex helpers (`reURLVersion`, `reHasMultiSlash`) if no longer referenced. Keep `reHasSlash` if `ParseDependency` still uses it. (`ParseDependency` continues to exist for the bare-name → default-org expansion.)

- [ ] **Step 4: Run tests**

```bash
go test ./internal/core/services/installer -v
go build ./...
```

Expected: all installer tests pass. If pre-existing `EnsureDependency` tests reference the old regex behavior with version-after-`@`, update them or pin the simpler new behavior.

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/installer/utils.go internal/core/services/installer/utils_test.go
git commit -m "fix(installer): accept git@host:path and https:// dep keys"
```

---

## Task 11: Domain Dependency cleanup

**Files:**
- Modify: `internal/core/domain/dependency.go`
- Modify: `internal/core/domain/dependency_test.go`

`GetURL()`, `SSHUrl()`, the `UseSSH` field, and the `:ssh` version-suffix parsing are all subsumed by the auth package. Trim them from the domain.

- [ ] **Step 1: Reduce `Dependency` struct**

Edit `internal/core/domain/dependency.go`:

Replace the struct:

```go
type Dependency struct {
    Repository string
    version    string
}
```

(Remove `UseSSH` field.)

Delete the methods: `GetURL`, `SSHUrl`. Keep `HashName`, `GetVersion`, `GetURLPrefix` (still used elsewhere — verify with grep), `Name`, `GetKey`.

In `ParseDependency`, drop the `:ssh` suffix logic:

```go
func ParseDependency(repo string, info string) Dependency {
    parsed := strings.Split(info, ":")
    dependency := Dependency{}
    dependency.Repository = repo
    dependency.version = parsed[0]
    if reVersionMajorMinor.MatchString(dependency.version) {
        dependency.version += ".0"
    }
    if reVersionMajor.MatchString(dependency.version) {
        dependency.version += ".0.0"
    }
    return dependency
}
```

Also delete the `consts` import if no longer used after removing the `GitProtocolSSH` reference. The `reSSHUrl` and `reHasHTTPS` regexps are now unused — delete them.

- [ ] **Step 2: Update domain tests**

Edit `internal/core/domain/dependency_test.go`. Delete tests for the removed functions (`TestGetURL*`, `TestSSHUrl*`, `:ssh` suffix tests). Keep tests for `Name`, `HashName`, `GetURLPrefix`.

- [ ] **Step 3: Build and run**

```bash
go build ./...
go test ./internal/core/domain -v
```

Expected: green. If there are call sites still referencing `dep.GetURL()` or `dep.UseSSH` (likely in `git_embedded.go`/`git_native.go`), they will fail to compile. **Fix them in Task 12 below**, but to keep the tree buildable temporarily, replace those call sites with `auth.Resolve(dep).URL` or comment them out as a TODO. Easier: just proceed straight to Task 12 within the same uncommitted state.

- [ ] **Step 4: Commit (if green) or proceed to Task 12**

If `go build ./...` is green:

```bash
git add internal/core/domain/
git commit -m "refactor(domain): remove URL/SSH logic now owned by auth package"
```

If it's not green because of git adapters, do not commit yet — keep the changes staged and continue to Task 12.

---

## Task 12: Wire auth into git adapters

**Files:**
- Modify: `internal/adapters/secondary/git/git.go`
- Modify: `internal/adapters/secondary/git/git_embedded.go`
- Modify: `internal/adapters/secondary/git/git_native.go`

Dispatch on `Decision.Transport` instead of `config.GetGitEmbedded()`. SSH → native (system git); HTTPS → embedded (go-git, with optional basic auth from `Decision.Credential`).

- [ ] **Step 1: Refactor the dispatcher**

Edit `internal/adapters/secondary/git/git.go`. Replace `CloneCache` and `UpdateCache`:

```go
import (
    // existing imports …
    "github.com/basti-fantasti/bossy/internal/core/services/auth"
)

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
```

Replace `Checkout` and `Pull` similarly — call `auth.Resolve` to pick transport.

- [ ] **Step 2: Update embedded git operations to take `Decision`**

Edit `internal/adapters/secondary/git/git_embedded.go`. Change signatures and use the URL/credential from `Decision` instead of `dep.GetURL()` and `config.GetAuth(...)`:

```go
import (
    // existing imports …
    "github.com/go-git/go-git/v5/plumbing/transport"
    httpAuth "github.com/go-git/go-git/v5/plumbing/transport/http"
    "github.com/basti-fantasti/bossy/internal/core/services/auth"
)

func httpsAuth(d auth.Decision) transport.AuthMethod {
    if d.Credential.User == "" && d.Credential.Password == "" {
        return nil
    }
    return &httpAuth.BasicAuth{Username: d.Credential.User, Password: d.Credential.Password}
}

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
        cloneOpts.Depth = 1
        cloneOpts.SingleBranch = true
    }

    repository, err := git.Clone(storageCache, worktreeFileSystem, cloneOpts)
    if err != nil {
        _ = os.RemoveAll(filepath.Join(env.GetCacheDir(), dep.HashName()))
        return nil, err
    }
    if err := initSubmodulesEmbedded(dep, decision, repository); err != nil {
        return nil, err
    }
    return repository, nil
}
```

Apply the same pattern to `UpdateCacheEmbedded`, `CheckoutEmbedded`, `PullEmbedded`. Replace any internal `initSubmodules` helper that took `config` to take `Decision` instead.

`makeStorageCacheNoCfg` is the existing `makeStorageCache` minus the unused `config` argument — rename in place.

- [ ] **Step 3: Update native git operations to take `Decision`**

Edit `internal/adapters/secondary/git/git_native.go`:

```go
func CloneCacheNative(dep domain.Dependency, decision auth.Decision) (*git2.Repository, error) {
    if err := requireGit(); err != nil {
        return nil, err
    }
    msg.Info("📥 Downloading dependency %s", dep.Repository)
    if err := doClone(dep, decision); err != nil {
        return nil, err
    }
    return GetRepository(dep), nil
}

// requireGit checks that the system git binary is on PATH. SSH transports
// require it; we hard-fail with a clear message if it's missing.
func requireGit() error {
    if _, err := exec.LookPath("git"); err != nil {
        return errors.New("SSH cloning requires `git` to be installed and on PATH. " +
            "Install git, or change the dependency to an HTTPS URL")
    }
    return nil
}

func doClone(dep domain.Dependency, decision auth.Decision) error {
    dirModule := filepath.Join(env.GetModulesDir(), dep.Name())
    dir := "--separate-git-dir=" + filepath.Join(env.GetCacheDir(), dep.HashName())

    _ = os.RemoveAll(dirModule)
    args := []string{"clone", dir}
    if env.GetGitShallow() {
        args = append(args, "--depth", "1", "--single-branch")
    }
    args = append(args, decision.URL, dirModule)

    cmd := exec.Command("git", args...) // #nosec G204
    if err := runCommand(cmd); err != nil {
        return err
    }
    if err := initSubmodulesNative(dep); err != nil {
        return err
    }
    _ = os.Remove(filepath.Join(dirModule, ".git"))
    return nil
}
```

Delete the old `checkHasGitClient` (POSIX-incompatible `where git`) — `requireGit` replaces it.

Apply the same `auth.Decision` parameter to `UpdateCacheNative`, `CheckoutNative`, `PullNative`.

- [ ] **Step 4: Update upstream callers in `installer/git_client.go`**

Find every call site that passed a `*env.Configuration` to `git.CloneCache` etc. and pass nil (the parameter is unused now) or remove the parameter entirely from the public function. Easier path: keep the parameter (for upstream cherry-pick stability) and ignore it inside.

```bash
grep -rn "git\.\(Clone\|Update\|Checkout\|Pull\)" --include="*.go" .
```

Verify each call still compiles.

- [ ] **Step 5: Build, test, commit**

```bash
go build ./...
go test ./... -count=1
go tool golangci-lint run
git add -A
git commit -m "refactor(git): dispatch on auth.Decision; system git for SSH, go-git for HTTPS"
```

---

## Task 13: Replace `boss login` with `bossy auth`

**Files:**
- Delete: `internal/adapters/primary/cli/login.go`
- Create: `internal/adapters/primary/cli/auth.go`
- Modify: `internal/adapters/primary/cli/root.go` — register the new command, drop the old

- [ ] **Step 1: Delete login.go**

```bash
rm internal/adapters/primary/cli/login.go
```

(There may be call-site references in `root.go` — fix in step 3.)

- [ ] **Step 2: Create the new `auth.go`**

Create `internal/adapters/primary/cli/auth.go`:

```go
// Package cli — auth command for managing stored HTTPS credentials.
// SSH credentials are no longer stored: ssh-agent and ~/.ssh/config
// supply them when bossy shells out to system git.
package cli

import (
    "fmt"

    "github.com/basti-fantasti/bossy/pkg/env"
    "github.com/basti-fantasti/bossy/pkg/msg"
    "github.com/pterm/pterm"
    "github.com/spf13/cobra"
)

func authCmdRegister(root *cobra.Command) {
    authCmd := &cobra.Command{
        Use:   "auth",
        Short: "Manage stored HTTPS credentials for private repositories",
        Long: `Manage HTTPS basic-auth credentials per host.

For SSH cloning, no command is needed — bossy invokes the system git
binary, which uses ssh-agent and ~/.ssh/config like a normal git clone.

For private HTTPS repositories, run:
  bossy auth set <host>          (prompts interactively)
  bossy auth set <host> -u USER -p PASS
  bossy auth list
  bossy auth rm <host>
`,
    }

    var user, pass string
    setCmd := &cobra.Command{
        Use:   "set <host>",
        Short: "Store HTTPS credentials for a host",
        Args:  cobra.ExactArgs(1),
        Run: func(_ *cobra.Command, args []string) {
            authSet(args[0], user, pass)
        },
    }
    setCmd.Flags().StringVarP(&user, "username", "u", "", "Username")
    setCmd.Flags().StringVarP(&pass, "password", "p", "", "Password or personal access token")

    listCmd := &cobra.Command{
        Use:   "list",
        Short: "List hosts with stored credentials",
        Run:   func(_ *cobra.Command, _ []string) { authList() },
    }

    rmCmd := &cobra.Command{
        Use:   "rm <host>",
        Short: "Remove stored credentials for a host",
        Args:  cobra.ExactArgs(1),
        Run:   func(_ *cobra.Command, args []string) { authRm(args[0]) },
    }

    authCmd.AddCommand(setCmd, listCmd, rmCmd)
    root.AddCommand(authCmd)

    // Migration stubs for the old commands.
    root.AddCommand(&cobra.Command{
        Use:    "login",
        Hidden: true,
        Run: func(_ *cobra.Command, _ []string) {
            msg.Die("`boss login` was removed in bossy. Use `bossy auth set <host>` for HTTPS, " +
                "or configure ssh-agent / ~/.ssh/config for SSH.")
        },
    })
    root.AddCommand(&cobra.Command{
        Use:    "logout",
        Hidden: true,
        Run: func(_ *cobra.Command, _ []string) {
            msg.Die("`boss logout` was removed in bossy. Use `bossy auth rm <host>`.")
        },
    })
}

func authSet(host, user, pass string) {
    cfg := env.GlobalConfiguration()
    if user == "" {
        u, err := pterm.DefaultInteractiveTextInput.Show("Username")
        if err != nil {
            msg.Die("input error: %v", err)
        }
        user = u
    }
    if pass == "" {
        p, err := pterm.DefaultInteractiveTextInput.WithMask("•").Show("Password")
        if err != nil {
            msg.Die("input error: %v", err)
        }
        pass = p
    }
    if cfg.Auth == nil {
        cfg.Auth = make(map[string]*env.Auth)
    }
    a := &env.Auth{}
    a.SetUser(user)
    a.SetPass(pass)
    cfg.Auth[host] = a
    cfg.SaveConfiguration()
    msg.Success("✅ Credentials stored for %s", host)
}

func authList() {
    cfg := env.GlobalConfiguration()
    if len(cfg.Auth) == 0 {
        fmt.Println("(no stored credentials)")
        return
    }
    for host := range cfg.Auth {
        fmt.Println(host)
    }
}

func authRm(host string) {
    cfg := env.GlobalConfiguration()
    if _, ok := cfg.Auth[host]; !ok {
        msg.Die("no credentials stored for %s", host)
    }
    delete(cfg.Auth, host)
    cfg.SaveConfiguration()
    msg.Success("✅ Credentials removed for %s", host)
}
```

- [ ] **Step 3: Update `root.go`**

Edit `internal/adapters/primary/cli/root.go`. Find the registration of the old login/logout (likely `loginCmdRegister(rootCmd)` or similar) and replace it with:

```go
authCmdRegister(rootCmd)
```

Build:

```bash
go build ./...
```

- [ ] **Step 4: Smoke test the CLI**

```bash
make build
./bin/bossy auth --help
./bin/bossy login   # expect: "boss login was removed..." with non-zero exit
./bin/bossy auth list
```

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat(cli): replace boss login/logout with bossy auth subcommands"
```

---

## Task 14: Replace `config git mode` with `config git protocol`

**Files:**
- Modify: `internal/adapters/primary/cli/config/git.go`
- Modify: `pkg/env/configuration.go` — drop `GitEmbedded` field (now obsolete)
- Modify: `pkg/env/interfaces.go` — drop `GetGitEmbedded` method

The protocol decision now drives transport choice; `git mode` is meaningless. Replace with a per-host protocol setter.

- [ ] **Step 1: Read the existing `config/git.go` to know what to replace**

```bash
cat internal/adapters/primary/cli/config/git.go
```

- [ ] **Step 2: Rewrite `config/git.go`**

Overwrite `internal/adapters/primary/cli/config/git.go`:

```go
package config

import (
    "fmt"
    "strconv"

    "github.com/basti-fantasti/bossy/pkg/env"
    "github.com/basti-fantasti/bossy/pkg/msg"
    "github.com/spf13/cobra"
)

func gitCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "git",
        Short: "Configure git-related behavior",
    }
    cmd.AddCommand(protocolCmd())
    cmd.AddCommand(shallowCmd())
    return cmd
}

func protocolCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "protocol <host> <ssh|https>",
        Short: "Set the default protocol for a host",
        Long: `Pin the default protocol bossy should use when cloning from <host>.
Overridden in CI by GITLAB_CI auto-detection and by BOSSY_AUTH_<HOST>.`,
        Args: cobra.ExactArgs(2),
        Run: func(_ *cobra.Command, args []string) {
            host, proto := args[0], args[1]
            if proto != "ssh" && proto != "https" {
                msg.Die("protocol must be 'ssh' or 'https'")
            }
            cfg := env.GlobalConfiguration()
            if cfg.HostProtocols == nil {
                cfg.HostProtocols = map[string]string{}
            }
            cfg.HostProtocols[host] = proto
            cfg.SaveConfiguration()
            msg.Success("✅ %s → %s", host, proto)
        },
    }
}

func shallowCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "shallow <true|false>",
        Short: "Enable or disable shallow clones",
        Args:  cobra.ExactArgs(1),
        Run: func(_ *cobra.Command, args []string) {
            v, err := strconv.ParseBool(args[0])
            if err != nil {
                msg.Die("expected true or false")
            }
            cfg := env.GlobalConfiguration()
            cfg.GitShallow = v
            cfg.SaveConfiguration()
            fmt.Printf("git shallow: %v\n", v)
        },
    }
}
```

If the parent `config.go` was registering an `gitModeCmd` or similar, drop it and add `gitCmd()` to the `config` parent command.

- [ ] **Step 3: Drop `GitEmbedded`**

Edit `pkg/env/configuration.go`:

Remove the `GitEmbedded bool ...` field. Search for and delete all references:

```bash
grep -rn "GitEmbedded\|GetGitEmbedded" --include="*.go" .
```

Edit `pkg/env/interfaces.go`: remove `GetGitEmbedded() bool` from the interface.

Update `internal/adapters/secondary/git/git.go` — its dispatch already uses `auth.Decision`, so the old `config.GetGitEmbedded()` branches should already be gone from Task 12. Confirm.

- [ ] **Step 4: Build, test**

```bash
go build ./...
go test ./... -count=1
go tool golangci-lint run
```

- [ ] **Step 5: Smoke test**

```bash
make build
./bin/bossy config git protocol gitlab.mydomain.com ssh
./bin/bossy config git shallow true
cat ~/.bossy/boss.cfg.json   # verify host_protocols and git_shallow set
```

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(cli): replace 'config git mode' with per-host 'config git protocol'"
```

---

## Task 15: Config migration

**Files:**
- Create: `internal/migrate/migrate.go`
- Create: `internal/migrate/migrate_test.go`
- Modify: `internal/adapters/primary/cli/root.go` or wherever startup runs — call migrator early

Two parallel migrations, both idempotent:

1. `~/.boss/` directory → `~/.bossy/` directory (copy).
2. `boss.json` in the current project: rewrite any version string containing `:ssh` to a `git@host:path` key + version without the suffix.

- [ ] **Step 1: Write failing tests**

Create `internal/migrate/migrate_test.go`:

```go
package migrate

import (
    "os"
    "path/filepath"
    "testing"
)

func TestMigrateHome_Copies(t *testing.T) {
    home := t.TempDir()
    legacy := filepath.Join(home, ".boss")
    if err := os.MkdirAll(legacy, 0700); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(legacy, "boss.cfg.json"), []byte("{}"), 0600); err != nil {
        t.Fatal(err)
    }

    moved, err := MigrateHome(home)
    if err != nil {
        t.Fatal(err)
    }
    if !moved {
        t.Fatal("expected migration to run")
    }
    if _, err := os.Stat(filepath.Join(home, ".bossy", "boss.cfg.json")); err != nil {
        t.Fatalf("expected file copied to .bossy: %v", err)
    }
}

func TestMigrateHome_Idempotent(t *testing.T) {
    home := t.TempDir()
    if err := os.MkdirAll(filepath.Join(home, ".bossy"), 0700); err != nil {
        t.Fatal(err)
    }
    moved, err := MigrateHome(home)
    if err != nil {
        t.Fatal(err)
    }
    if moved {
        t.Fatal("expected no migration when target already exists")
    }
}

func TestMigrateHome_NoLegacy(t *testing.T) {
    home := t.TempDir()
    moved, err := MigrateHome(home)
    if err != nil {
        t.Fatal(err)
    }
    if moved {
        t.Fatal("nothing to migrate")
    }
}

func TestMigrateBossJSON_RewritesSSHSuffix(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "boss.json")
    input := `{
        "name":"app",
        "version":"1.0.0",
        "dependencies": {
            "gitlab.mydomain.com/g/r": "*:ssh",
            "horse": "^1.0.0"
        }
    }`
    if err := os.WriteFile(path, []byte(input), 0600); err != nil {
        t.Fatal(err)
    }
    changed, err := MigrateBossJSON(path)
    if err != nil {
        t.Fatal(err)
    }
    if !changed {
        t.Fatal("expected rewrite")
    }
    out, _ := os.ReadFile(path)
    s := string(out)
    if !contains(s, `"git@gitlab.mydomain.com:g/r"`) {
        t.Errorf("expected SSH key rewrite; got %s", s)
    }
    if contains(s, ":ssh") {
        t.Errorf(":ssh suffix not stripped: %s", s)
    }
    if !contains(s, `"horse"`) {
        t.Errorf("non-affected dep dropped: %s", s)
    }
}

func TestMigrateBossJSON_NoChange(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "boss.json")
    input := `{"name":"x","version":"1","dependencies":{"horse":"*"}}`
    if err := os.WriteFile(path, []byte(input), 0600); err != nil {
        t.Fatal(err)
    }
    changed, err := MigrateBossJSON(path)
    if err != nil {
        t.Fatal(err)
    }
    if changed {
        t.Fatal("expected no change")
    }
}

func contains(s, sub string) bool {
    for i := 0; i+len(sub) <= len(s); i++ {
        if s[i:i+len(sub)] == sub {
            return true
        }
    }
    return false
}
```

- [ ] **Step 2: Run, expect failure**

```bash
go test ./internal/migrate -v
```

Expected: build error.

- [ ] **Step 3: Implement**

Create `internal/migrate/migrate.go`:

```go
// Package migrate handles one-shot upgrades from upstream `boss` state
// to the renamed `bossy` layout.
package migrate

import (
    "encoding/json"
    "io"
    "os"
    "path/filepath"
    "strings"
)

// MigrateHome copies $home/.boss to $home/.bossy if the legacy dir exists
// and the new dir does not. Returns true if migration ran.
func MigrateHome(home string) (bool, error) {
    legacy := filepath.Join(home, ".boss")
    target := filepath.Join(home, ".bossy")
    if _, err := os.Stat(target); err == nil {
        return false, nil
    }
    if _, err := os.Stat(legacy); os.IsNotExist(err) {
        return false, nil
    }
    if err := copyTree(legacy, target); err != nil {
        return false, err
    }
    return true, nil
}

func copyTree(src, dst string) error {
    return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        rel, err := filepath.Rel(src, path)
        if err != nil {
            return err
        }
        out := filepath.Join(dst, rel)
        if info.IsDir() {
            return os.MkdirAll(out, info.Mode())
        }
        in, err := os.Open(path) // #nosec G304 -- migrating known config tree
        if err != nil {
            return err
        }
        defer in.Close()
        if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
            return err
        }
        f, err := os.Create(out) // #nosec G304
        if err != nil {
            return err
        }
        defer f.Close()
        _, err = io.Copy(f, in)
        return err
    })
}

// MigrateBossJSON rewrites legacy `:ssh` version-suffix entries in a
// boss.json file to canonical SSH-form keys. Returns true if the file
// was modified. Idempotent.
func MigrateBossJSON(path string) (bool, error) {
    raw, err := os.ReadFile(path) // #nosec G304
    if err != nil {
        if os.IsNotExist(err) {
            return false, nil
        }
        return false, err
    }
    var doc map[string]json.RawMessage
    if err := json.Unmarshal(raw, &doc); err != nil {
        return false, err
    }
    depsRaw, ok := doc["dependencies"]
    if !ok {
        return false, nil
    }
    var deps map[string]string
    if err := json.Unmarshal(depsRaw, &deps); err != nil {
        return false, err
    }
    changed := false
    newDeps := make(map[string]string, len(deps))
    for k, v := range deps {
        if strings.Contains(v, ":ssh") {
            // Strip the `:ssh` suffix (and any trailing colon) from the
            // version string; convert the `host/path` key to `git@host:path`.
            cleanedVer := strings.TrimSuffix(strings.ReplaceAll(v, ":ssh", ""), ":")
            if cleanedVer == "" {
                cleanedVer = "*"
            }
            host, p, ok := strings.Cut(k, "/")
            if ok {
                newKey := "git@" + host + ":" + p
                newDeps[newKey] = cleanedVer
                changed = true
                continue
            }
        }
        newDeps[k] = v
    }
    if !changed {
        return false, nil
    }
    newDepsRaw, err := json.MarshalIndent(newDeps, "", "  ")
    if err != nil {
        return false, err
    }
    doc["dependencies"] = newDepsRaw
    out, err := json.MarshalIndent(doc, "", "  ")
    if err != nil {
        return false, err
    }
    return true, os.WriteFile(path, out, 0600)
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/migrate -v
```

Expected: PASS.

- [ ] **Step 5: Wire migration into startup**

Find the startup hook in `internal/adapters/primary/cli/root.go` (likely a `cobra.OnInitialize` or `PersistentPreRun`). Add at the very top:

```go
import (
    // existing …
    "os"
    homedir "github.com/mitchellh/go-homedir"
    "github.com/basti-fantasti/bossy/internal/migrate"
)

// inside the init function or PersistentPreRun:
if home, err := homedir.Dir(); err == nil {
    if moved, _ := migrate.MigrateHome(home); moved {
        msg.Info("ℹ️  Migrated ~/.boss to ~/.bossy")
    }
}
if cwd, err := os.Getwd(); err == nil {
    if changed, _ := migrate.MigrateBossJSON(filepath.Join(cwd, "boss.json")); changed {
        msg.Info("ℹ️  Migrated boss.json (:ssh suffix → SSH URL keys)")
    }
}
```

- [ ] **Step 6: Build, smoke**

```bash
go build ./...
make build
./bin/bossy --version   # should not error even if no migration applies
```

- [ ] **Step 7: Commit**

```bash
git add internal/migrate/ internal/adapters/primary/cli/root.go
git commit -m "feat(migrate): one-shot ~/.boss → ~/.bossy and boss.json :ssh rewrite"
```

---

## Task 16: Failure messages — git missing, GitLab 403

**Files:**
- Modify: `internal/adapters/secondary/git/git_native.go` — already done in Task 12 step 3, verify
- Modify: `internal/adapters/secondary/git/git_embedded.go` — add 403 detection on clone error

When go-git returns a 403 during clone and we are running under GitLab CI, surface the canonical fix.

- [ ] **Step 1: Add error wrapping in `CloneCacheEmbedded`**

Edit `internal/adapters/secondary/git/git_embedded.go`. After the `git.Clone(...)` call:

```go
import (
    // existing …
    "os"
    "strings"
)

repository, err := git.Clone(storageCache, worktreeFileSystem, cloneOpts)
if err != nil {
    _ = os.RemoveAll(filepath.Join(env.GetCacheDir(), dep.HashName()))
    if isCIPermissionError(err) {
        return nil, ciJobTokenError(dep.Repository, err)
    }
    return nil, err
}
```

Add the helpers at the bottom of the file:

```go
func isCIPermissionError(err error) bool {
    if err == nil || os.Getenv("GITLAB_CI") != "true" {
        return false
    }
    s := err.Error()
    return strings.Contains(s, "403") || strings.Contains(s, "Forbidden")
}

func ciJobTokenError(repo string, cause error) error {
    return fmt.Errorf("CI_JOB_TOKEN denied access to %s.\n"+
        "Add the calling project to the dependency project's CI/CD job-token allowlist:\n"+
        "  Settings → CI/CD → Job token permissions\n"+
        "See docs/ci.md for details. Underlying error: %w", repo, cause)
}
```

(Add `"fmt"` import if not present.)

- [ ] **Step 2: Test by inspection**

There is no clean unit test for go-git's 403 path without a mocked transport; rely on the integration test in Task 19.

- [ ] **Step 3: Commit**

```bash
git add internal/adapters/secondary/git/git_embedded.go
git commit -m "feat(git): surface CI_JOB_TOKEN allowlist hint on 403 in CI"
```

---

## Task 17: Update self-update tests and release workflow

**Files:**
- Modify: `internal/upgrade/github_test.go` (asset-name expectations, if any)
- Modify: `.github/workflows/release.yml`

- [ ] **Step 1: Inspect release workflow**

```bash
cat .github/workflows/release.yml
```

- [ ] **Step 2: Update asset-name producing steps**

Replace any step that produces `boss-<os>-<arch>.{zip,tar.gz}` with `bossy-<os>-<arch>.{zip,tar.gz}`. The Makefile `dist` target uses `_dist/{{.OS}}-{{.Arch}}/$(BINNAME)` — that already follows `BINNAME` so step 1 of Task 1 already covered it. The packaging line `tar -zcf boss-{}.tar.gz {}` needs updating:

Edit `Makefile` `dist` target:

```makefile
$(DIST_DIRS) tar -zcf bossy-{}.tar.gz {} \; && \
$(DIST_DIRS) zip -r bossy-{}.zip {} \; \
```

- [ ] **Step 3: Update workflow repo references**

In `.github/workflows/release.yml`, any hardcoded `hashload/boss` → `Basti-Fantasti/bossy`. Owner-derived references that use `${{ github.repository }}` need no change.

- [ ] **Step 4: Smoke build**

```bash
make build
make test
```

- [ ] **Step 5: Commit**

```bash
git add Makefile .github/workflows/
git commit -m "build: rename release artifacts to bossy-*; update workflow refs"
```

---

## Task 18: Documentation — README

**Files:**
- Modify: `README.md`

Rewrite the auth section. Remove the old `Login`, `Logout`, and `git mode` documentation. Add `Authentication` and link to `docs/ci.md`.

- [ ] **Step 1: Replace auth section**

Find the existing `### > Login` section in `README.md` and replace it (and the `### > Logout` section that follows) with:

````markdown
### > Authentication

Bossy uses different authentication mechanisms depending on transport and
context:

- **Public repositories** (e.g. `github.com/HashLoad/horse`): no setup needed.
- **Private SSH repositories** (`git@gitlab.mydomain.com:foo/bar.git`): bossy invokes
  the system `git` binary. Configure `ssh-agent`, `~/.ssh/config`, and your
  SSH key the way you would for any other git workflow. No `bossy auth`
  command is required for SSH.
- **Private HTTPS repositories**: store basic-auth credentials per host:
  ```sh
  bossy auth set gitlab.mydomain.com              # interactive
  bossy auth set gitlab.mydomain.com -u user -p pat
  bossy auth list
  bossy auth rm gitlab.mydomain.com
  ```
- **CI/CD environments**: bossy auto-detects GitLab CI via `GITLAB_CI`,
  `CI_SERVER_HOST`, and `CI_JOB_TOKEN`. No setup needed in the runner.
  See [`docs/ci.md`](docs/ci.md).

For non-GitLab CI runners or one-off overrides:
```sh
export BOSSY_AUTH_GITLAB_MYDOMAIN_COM=https-token:gitlab-ci-token:$CI_JOB_TOKEN
# or, force SSH for a host:
export BOSSY_AUTH_GITLAB_MYDOMAIN_COM=ssh
```

To pin a default protocol per host:
```sh
bossy config git protocol gitlab.mydomain.com ssh
bossy config git protocol github.com https
```

### > Dependency URL forms

In `boss.json` and on the `bossy install` command line, you can use any of:

- Bare name → defaults to `github.com/hashload/<name>` (e.g. `horse`).
- `host/owner/repo`
- `git@host:owner/repo[.git]` — pins the dep to SSH.
- `https://host/owner/repo[.git]` — pins the dep to HTTPS.
````

Remove the old `### > Git Client` section (the `embedded` vs `native` doc) entirely. Keep the `### > Shallow Clone` section.

- [ ] **Step 2: Spot-check rendering**

```bash
grep -n "boss login\|boss logout\|boss config git mode" README.md
```

Expected: no remaining references.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: rewrite auth section, remove embedded/native git mode"
```

---

## Task 19: Documentation — `docs/ci.md`

**Files:**
- Create: `docs/ci.md`

This is the file the user explicitly asked to be well-documented, especially the GitLab job-token allowlist troubleshooting.

- [ ] **Step 1: Create `docs/ci.md`**

Create with:

````markdown
# Bossy in CI/CD

This document covers running `bossy install` inside a CI runner. The
common case — GitLab Runner pulling private dependencies from the same
GitLab instance — needs zero configuration.

## GitLab CI: zero-config

A minimal `.gitlab-ci.yml` that builds a Delphi project with private
dependencies on the same GitLab instance:

```yaml
build:
  image: registry.mydomain.com/devops/delphi-bossy:latest   # contains git, bossy, Delphi
  script:
    - bossy install
    - bossy run build
```

That's the entire CI auth story. No `before_script` SSH dance, no key
management, no extra env vars to set.

### How it works

GitLab Runner injects three env vars into every job:
- `GITLAB_CI=true`
- `CI_SERVER_HOST=<your gitlab host>`
- `CI_JOB_TOKEN=<short-lived token>`

When `bossy install` resolves a dependency whose host matches
`CI_SERVER_HOST`, it transparently rewrites the clone URL to
`https://gitlab-ci-token:${CI_JOB_TOKEN}@<host>/<path>` and clones over
HTTPS. This applies regardless of how the dependency was declared in
`boss.json`:

| Declared as | In CI, becomes |
|---|---|
| `git@gitlab.mydomain.com:foo/bar.git` | `https://gitlab-ci-token:TOKEN@gitlab.mydomain.com/foo/bar` |
| `gitlab.mydomain.com/foo/bar` | `https://gitlab-ci-token:TOKEN@gitlab.mydomain.com/foo/bar` |
| `https://gitlab.mydomain.com/foo/bar` | `https://gitlab-ci-token:TOKEN@gitlab.mydomain.com/foo/bar` |

So the same `boss.json` works for local SSH development and CI HTTPS
without any branching logic.

Public dependencies (e.g. `github.com/HashLoad/horse`) clone over plain
HTTPS — `CI_JOB_TOKEN` is not used for them.

## Troubleshooting

### `403 Forbidden` or "remote rejected" on a real repo

This is the common gotcha. Symptom:

```
❌ CI_JOB_TOKEN denied access to gitlab.mydomain.com/devops/some-lib.
   Add the calling project to the dependency project's CI/CD job-token allowlist:
     Settings → CI/CD → Job token permissions
```

**Cause:** Since GitLab 15, `CI_JOB_TOKEN` is scoped. A job in
**project A** can only clone **project B** if **project B has explicitly
added project A** to its CI/CD job-token allowlist. By default the
allowlist contains only the project itself.

**Fix:**

1. Open project B (the *dependency* repo, the one being cloned) in GitLab.
2. Go to **Settings → CI/CD → Job token permissions**.
3. Under **"Allow access to this project with a CI_JOB_TOKEN"**, add
   project A (the *consumer* repo, the one whose CI is failing) to the
   allowlist.
4. Save.
5. Re-run the failing pipeline.

If you have a monorepo of N internal libraries pulled by M apps, each
library needs each app added once — bookkeeping but a one-time cost.

If your security model allows it, GitLab also offers a per-project
toggle to allow access from any project under the same group. Use with
care: it removes the per-pair scoping.

GitLab docs: <https://docs.gitlab.com/ee/ci/jobs/ci_job_token.html>

### `SSH cloning requires git to be installed and on PATH`

The runner image does not have `git`. Install it (`apt-get install -y git`
in a Debian-based image, `apk add git` in Alpine) or switch the
dependency to an HTTPS URL.

### `no credentials configured for <host>` (outside CI)

Means none of the auth layers matched and the clone is failing. Either:
- Run `bossy auth set <host>` for HTTPS basic auth, or
- `bossy config git protocol <host> ssh` and ensure ssh-agent has your key.

## Non-GitLab CI runners

For Jenkins, GitHub Actions, Drone, etc., use the generic env-var
escape hatch:

```sh
# HTTPS with a token (recommended for CI)
export BOSSY_AUTH_GITLAB_MYDOMAIN_COM=https-token:gitlab-ci-token:$MY_CI_TOKEN

# Or force SSH (you'll need ssh-agent loaded too)
export BOSSY_AUTH_GITLAB_MYDOMAIN_COM=ssh
```

The env var name is `BOSSY_AUTH_<HOST>` with `.` and `-` in the host
replaced by `_` and the whole thing upper-cased. Examples:

| Host | Env var |
|---|---|
| `gitlab.mydomain.com` | `BOSSY_AUTH_GITLAB_MYDOMAIN_COM` |
| `github.com` | `BOSSY_AUTH_GITHUB_COM` |
| `git.example-corp.io` | `BOSSY_AUTH_GIT_EXAMPLE_CORP_IO` |

## Runner image checklist

Whatever base image your runners use, ensure it has:

- `git` (≥ 2.x) — required for SSH transports and recommended for HTTPS.
- `bossy` binary on PATH.
- The Delphi compiler if you build Delphi projects.
- **Not required:** `ssh-agent`, SSH keys, `~/.ssh/config` — none of
  these are needed for the GitLab CI auto-detection path. Add them only
  if you need to clone from a non-GitLab SSH host.
````

- [ ] **Step 2: Link from README**

Add a "CI/CD" line to the README's Authentication section if not already present (the Task-18 README rewrite already includes the link). Verify:

```bash
grep -n "docs/ci.md" README.md
```

- [ ] **Step 3: Commit**

```bash
git add docs/ci.md
git commit -m "docs: add docs/ci.md with GitLab CI guide and 403 troubleshooting"
```

---

## Task 20: Final verification and integration smoke

- [ ] **Step 1: Full lint & test suite**

```bash
go mod tidy
go build ./...
go test ./... -count=1 -race
go tool golangci-lint run
```

Expected: all green.

- [ ] **Step 2: Build and smoke the binary**

```bash
make build
./bin/bossy --version
./bin/bossy --help
./bin/bossy auth --help
./bin/bossy config git protocol --help
./bin/bossy login    # expect non-zero with migration hint
./bin/bossy install --help
```

- [ ] **Step 3: Manual GitLab CI simulation (if a test repo is available)**

Against a real internal repo on `gitlab.mydomain.com`:

```bash
# Local dev path: SSH via system git + ssh-agent
./bin/bossy install git@gitlab.mydomain.com:<group>/<test-repo>.git

# Simulated CI path: HTTPS via CI_JOB_TOKEN
GITLAB_CI=true \
CI_SERVER_HOST=gitlab.mydomain.com \
CI_JOB_TOKEN=<a token with read access> \
./bin/bossy install gitlab.mydomain.com/<group>/<test-repo>
```

Expected: both succeed; `bossy install` reports the dep installed.

- [ ] **Step 4: Verify upstream cherry-pick surface**

```bash
git diff master --stat | sort -k1
```

Sanity-check that the bulk of changes lie in:
- `internal/core/services/auth/` (new, no upstream conflict surface)
- `internal/migrate/` (new)
- `pkg/consts/branding.go` (new — single conflict point for trivial upstream version bumps)
- The mechanical `hashload/boss` → `basti-fantasti/bossy` import sweep (touches every file but with the same one-line change)

- [ ] **Step 5: Final commit and push**

```bash
git status   # should be clean
git log --oneline master..HEAD   # review your task commits
git push -u origin bossy-fork-design
```

- [ ] **Step 6: Open the PR**

Use `gh pr create` with the spec linked from the description.

---

## Self-review notes

Spec coverage check:
- ✅ Rebrand (binary, module, asset, config dir): Tasks 1, 2, 17
- ✅ Self-update target: Task 2
- ✅ Auth chain: Tasks 3–9
- ✅ SSH via system git: Task 12
- ✅ `git@host:repo` URL parser fix: Task 10
- ✅ CLI removal of `boss login`, addition of `bossy auth`: Task 13
- ✅ `config git mode` removal, `protocol` addition: Task 14
- ✅ Config & manifest migration: Task 15
- ✅ 403 + missing-git error messages: Tasks 12, 16
- ✅ README and docs/ci.md (with the user-emphasized GitLab allowlist troubleshooting): Tasks 18, 19
- ✅ Cherry-pick hygiene: branding consts file, isolated auth package, preserved adapter signatures
