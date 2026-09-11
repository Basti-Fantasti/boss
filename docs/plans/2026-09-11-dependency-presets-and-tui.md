# Dependency Presets and Selection TUI — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let a developer pick dependencies from a curated catalog through a TUI instead of typing repository URLs, and manage that catalog from the CLI.

**Architecture:** Hexagonal, matching the existing layout. Pure catalog types in `internal/core/domain`; a `PresetStore` port with an on-disk adapter under `~/.bossy/presets/`; a `presets` service composing them; a `bubbletea` TUI as a primary adapter peer to `cli/`; a new `bossy add` verb that writes `bossy.json` and delegates to the existing installer service. One port extension (`ListRemoteRefs`) gives clone-free ref listing on both git backends.

**Tech Stack:** Go 1.24, cobra, bubbletea/bubbles/lipgloss, go-git v5, pterm (existing output), testify-free stdlib table tests.

**Design document:** `docs/plans/2026-09-11-dependency-presets-and-tui-design.md`

**Branch:** `feat/dependency-presets` off `bossy-fork`.

---

## Conventions for every task

- Lint needs the pinned toolchain on this machine:
  `GOTOOLCHAIN=go1.24.1 go tool golangci-lint run ./<path>/...`
- Tests: `go test ./<path>/... -race` (plain `go test` is fine under Go 1.27).
- Use the `git-config` skill before the first commit. No AI attribution in messages.
- Commit after every task. Each task must leave the tree green.

---

## Task 1: Catalog domain types

**Files:**
- Create: `internal/core/domain/preset.go`
- Test: `internal/core/domain/preset_test.go`

**Types:**

```go
type RefKind string

const (
    RefKindDefault RefKind = "default"
    RefKindBranch  RefKind = "branch"
    RefKindTag     RefKind = "tag"
    RefKindCommit  RefKind = "commit"
)

type PresetRef struct {
    Kind  RefKind `json:"kind"`
    Value string  `json:"value,omitempty"`
}

type Preset struct {
    ID          string    `json:"id"`
    Name        string    `json:"name,omitempty"`
    Description string    `json:"description,omitempty"`
    Repo        string    `json:"repo"`
    DefaultRef  PresetRef `json:"default_ref"`
    SearchPaths []string  `json:"searchpaths,omitempty"`
    Platforms   []string  `json:"platforms,omitempty"`
    Tags        []string  `json:"tags,omitempty"`
}

type Catalog struct {
    Schema   int       `json:"schema"`
    Source   string    `json:"source,omitempty"`
    SyncedAt time.Time `json:"synced_at,omitempty"`
    Presets  []Preset  `json:"presets"`
}
```

**Behaviour:**

- `PresetIDPattern = ^[A-Za-z][A-Za-z0-9._-]*$` in `pkg/consts`. Uppercase must be
  allowed — `config.toml` has `PYTHONENVIRONMENTDIR`.
- `PresetRef.Validate()`: `default` must carry an empty value; `branch` and `tag`
  must carry a non-empty one; `commit` must be exactly 40 hex digits. Reuse the
  same reasoning as `isFullHexSHA` in the git adapter: a short SHA is silently
  zero-padded by go-git into a plausible wrong hash.
- `Preset.Validate()`: id matches the pattern, repo non-empty, ref valid.
- `Catalog.Validate()`: schema is `1`, every preset valid, no duplicate ids.
- `Preset.Version()`: the value written into `bossy.json` — `>0.0.0` for
  `default`, otherwise `DefaultRef.Value`.
- `MergeCatalogs(remote, local Catalog) []Preset`: keyed on id, local shadows
  remote entirely, result sorted by id.

**Tests (table-driven):**

- `TestPresetRef_Validate` — each kind, valid and invalid, including a 7-char SHA.
- `TestPreset_Validate` — empty id, lowercase/uppercase ids, dotted id, empty repo.
- `TestCatalog_Validate` — duplicate ids rejected, wrong schema rejected.
- `TestPreset_Version` — the four-row mapping table from the design.
- `TestMergeCatalogs` — local shadows remote; remote-only and local-only survive;
  output sorted; a local entry never inherits remote fields.

**Commit:** `feat(domain): add dependency preset catalog types`

---

## Task 2: PresetStore port and on-disk adapter

**Files:**
- Create: `internal/core/ports/preset.go`
- Create: `internal/adapters/secondary/repository/preset.go`
- Test: `internal/adapters/secondary/repository/preset_test.go`

**Port:**

```go
type PresetStore interface {
    LoadLocal() (domain.Catalog, error)
    SaveLocal(domain.Catalog) error
    LoadRemote() (domain.Catalog, error)
    SaveRemote(domain.Catalog) error
}
```

**Adapter:** files at `<presetsDir>/local.json` and `<presetsDir>/remote.json`,
where `presetsDir` is injected (production: `filepath.Join(bossHome, "presets")`).

- A missing file returns an empty catalog with `Schema: 1` and no error. A fresh
  install has no catalog and that is not an error condition.
- A malformed file *is* an error, naming the path. Silently starting from empty
  would discard a catalog somebody spent effort on.
- Save creates the directory, marshals with `MarshalIndent` and a tab, matching
  `Configuration.SaveConfiguration`.

**Tests** using `t.TempDir()`:

- `TestPresetStore_LoadMissingReturnsEmpty`
- `TestPresetStore_RoundTrip` — save then load yields an equal catalog.
- `TestPresetStore_MalformedFileErrors` — write `{`, expect an error mentioning the path.
- `TestPresetStore_LocalAndRemoteAreIndependent` — saving local leaves remote untouched.

**Commit:** `feat(repository): persist preset catalogs under the bossy home`

---

## Task 3: presets service

**Files:**
- Create: `internal/core/services/presets/presets.go`
- Test: `internal/core/services/presets/presets_test.go`

**API:**

```go
type Service struct{ store ports.PresetStore }

func New(store ports.PresetStore) *Service

func (s *Service) All() ([]domain.Preset, error)                 // merged, sorted
func (s *Service) ByID(id string) (domain.Preset, bool, error)
func (s *Service) ByTag(tag string) ([]domain.Preset, error)     // sorted
func (s *Service) Add(p domain.Preset) error                     // errors if id exists locally
func (s *Service) Update(p domain.Preset) error                  // errors if id absent locally
func (s *Service) Remove(id string) error
func (s *Service) ImportLocal(c domain.Catalog) (added, replaced int, err error)
func (s *Service) ReplaceRemote(c domain.Catalog) error
```

`Add` and `Update` validate before writing. `Update` on an id that exists only in
the remote catalog is allowed and creates a local override — that is the
documented way to shadow a shared entry.

**Tests** against an in-memory fake store (defined in the test file):

- `TestService_AllMergesAndSorts`
- `TestService_AddRejectsDuplicate`
- `TestService_AddRejectsInvalid`
- `TestService_UpdateCreatesLocalOverrideForRemoteEntry`
- `TestService_RemoveUnknownErrors`
- `TestService_ByTagFiltersAcrossBothCatalogs`
- `TestService_ImportLocalCountsAddedAndReplaced`

**Commit:** `feat(presets): add catalog service with local overrides`

---

## Task 4: clone-free remote ref listing

**Files:**
- Modify: `internal/adapters/secondary/git/git.go` (add `ListRemoteRefs` dispatch)
- Modify: `internal/adapters/secondary/git/git_native.go` (reuse `ListRefsNative`)
- Modify: `internal/adapters/secondary/git/git_embedded.go` (add `listRemoteRefsEmbedded`)
- Modify: `internal/core/ports/git.go` — only if the `GitRepository` interface is
  actually implemented somewhere; check first with
  `grep -rn "ports.GitRepository" --include=*.go .` and skip the interface change
  if it is currently unused.
- Test: `internal/adapters/secondary/git/git_remote_refs_test.go`

**Why:** `GetVersions` (`git.go:85`) is clone-free for SSH but routes HTTPS
through `getVersionsEmbedded`, which needs a `*git.Repository`. The TUI must not
clone 28 repositories to populate a tag list.

**Embedded implementation:**

```go
remote := goGit.NewRemote(memory.NewStorage(), &config.RemoteConfig{
    Name: "origin",
    URLs: []string{url},
})
refs, err := remote.List(&goGit.ListOptions{Auth: auth})
```

Filter to `refs/heads/` and `refs/tags/` so both backends return the same shape.
`ListRefsNative` already does this through `parseLsRemote`'s allowlist.

**Tests** — no network. Create a real repository in `t.TempDir()` with the `git`
binary (skip the test if `git` is absent, as the existing ls-remote tests do),
give it two branches and a tag, then point both backends at its path:

- `TestListRemoteRefs_EmbeddedSeesBranchesAndTags`
- `TestListRemoteRefs_EmbeddedFiltersNonBranchTagRefs` — create
  `refs/pull/1/head` and assert it is absent.
- `TestListRemoteRefs_UnreachableRemoteErrors` — a failure must be returned, never
  an empty slice. An empty result is indistinguishable from "no refs" and would
  silently fall back to the main branch, which is the bug fixed in `dda87ee`.

**Commit:** `feat(git): list remote refs without cloning on both backends`

---

## Task 5: `bossy preset` command group

**Files:**
- Create: `internal/adapters/primary/cli/preset/preset.go` (group + DI)
- Create: `internal/adapters/primary/cli/preset/list.go` (`list`, `show`)
- Create: `internal/adapters/primary/cli/preset/edit.go` (`add`, `update`, `rm`)
- Create: `internal/adapters/primary/cli/preset/importcmd.go` (`import`)
- Modify: `internal/adapters/primary/cli/root.go` — register the group
- Test: `internal/adapters/primary/cli/preset/edit_test.go`

Follow the shape of `cli/config/alias.go`: an `action` struct with a `run` method
returning `([]string, error)` so it is testable, and a thin cobra `Run` that
renders through `pkg/msg` and calls `msg.Die` on error.

Flags for `add`/`update`: `--repo`, `--ref <kind>:<value>`, repeatable
`--searchpath`, `--platform`, `--tag`, plus `--name`, `--description`.
`--ref` parsing: `tag:3.4.2-magnesium`, `branch:main`, `commit:<sha>`, or bare
`default`.

**Tests:** `--ref` parsing (valid forms and three malformed ones), `add` then
`list` round-trip against a fake store, `rm` of an unknown id.

**Commit:** `feat(cli): add bossy preset command group`

---

## Task 6: `bossy preset sync`

**Files:**
- Create: `internal/core/services/presets/sync.go`
- Create: `internal/adapters/primary/cli/preset/sync.go`
- Modify: `pkg/env/configuration.go` — add `PresetSource string \`json:"preset_source,omitempty"\``
- Test: `internal/core/services/presets/sync_test.go`

Clone the catalog repository into a temp directory through the existing git
adapter, read `presets.json` from its root, validate, stamp `Source` and
`SyncedAt`, then `ReplaceRemote`.

Validation happens **before** the write. A catalog repository with one bad entry
must not destroy the working `remote.json` already on disk.

The service takes a `func(repoKey, destDir string) error` clone function so the
test can supply a local fixture directory instead of reaching the network.

**Tests:**

- `TestSync_WritesRemoteCatalog`
- `TestSync_InvalidCatalogLeavesExistingRemoteIntact`
- `TestSync_MissingPresetsJSONErrors`

**Commit:** `feat(presets): sync the shared catalog from a git repository`

---

## Task 7: TUI

**Files:**
- Create: `internal/adapters/primary/tui/model.go` (root model, messages, `Update`)
- Create: `internal/adapters/primary/tui/view.go` (rendering only)
- Create: `internal/adapters/primary/tui/keys.go`
- Create: `internal/adapters/primary/tui/run.go` (`Run(presets, refLister) ([]Selection, error)`)
- Test: `internal/adapters/primary/tui/model_test.go`
- Modify: `go.mod` — add bubbletea, bubbles, lipgloss

**Model:**

```go
type rootModel struct {
    presets  []domain.Preset
    filter   string
    filtered []int          // indices into presets
    cursor   int
    selected map[string]Selection
    focus    focusArea      // focusList | focusDetail
    refs     map[string]refState
    lister   RefLister
    err      string
    done     bool
    cancelled bool
}

type Selection struct {
    Preset domain.Preset
    Ref    domain.PresetRef
    Freeze bool
}
```

`Update` must stay a pure function of `(model, tea.Msg)` — no terminal calls, no
I/O. Ref fetching is a `tea.Cmd` returning `refsLoadedMsg` or `refsFailedMsg`.
All rendering lives in `view.go`.

Keys: `space` toggle, `enter` focus detail, `esc` back to list, `/` filter,
`ctrl+s` confirm, `ctrl+c`/`q` cancel.

A `refsFailedMsg` sets an inline error on that entry and leaves the selection at
its catalog default, marked unverified. It must not exit the TUI and must not
substitute a default branch.

**Tests** — drive `Update` directly with synthetic messages, no pty:

- `TestUpdate_SpaceTogglesSelection`
- `TestUpdate_FilterNarrowsList`
- `TestUpdate_FilterResetsCursorIntoRange` — filter down to one entry with the
  cursor at index 5 and assert the cursor is still valid.
- `TestUpdate_EnterRequestsRefsOnlyOnce` — second enter on the same preset issues
  no command.
- `TestUpdate_RefsFailedKeepsDefaultAndFlagsUnverified`
- `TestUpdate_CtrlSProducesSelections`
- `TestUpdate_CtrlCCancels`

**Commit:** `feat(tui): add the dependency selection interface`

---

## Task 8: `bossy add`

**Files:**
- Create: `internal/adapters/primary/cli/add.go`
- Test: `internal/adapters/primary/cli/add_test.go`

Behaviour:

1. Load the merged catalog through the presets service.
2. If ids or `--tag` are given, resolve non-interactively.
3. Otherwise require a TTY (`isatty`, already a dependency) and run the TUI.
   Without a TTY, exit with an error naming `--tag` and `<id>` rather than
   blocking on a keystroke that will never arrive in CI.
4. For each selection, `pkg.AddDependency(repo, version)` and merge the preset's
   `searchpaths` into the manifest under the module key.
5. Save `bossy.json`, then call the existing installer service. No second install
   path.

**Tests:**

- `TestAdd_UnknownIDErrors`
- `TestAdd_TagSelectsExpectedPresets`
- `TestAdd_WritesSearchPathsIntoManifest`
- `TestAdd_NonTTYWithoutArgsErrors`

**Commit:** `feat(cli): add bossy add for catalog-driven dependency selection`

---

## Task 9: documentation

**Files:**
- Modify: `CHANGELOG.md` — an `### Added` block under `Unreleased`
- Modify: `README.md` — a short section on the catalog and `bossy add`

**Commit:** `docs: document the preset catalog and bossy add`

---

## Deliberately not in this plan

Global library-tree provisioning, `CompInstall` package registration, catalog
self-versioning, and the six known gaps recorded in the design document's final
section. Those are a separate conversation once this lands.
