# Dependency presets and selection TUI — design

Date: 2026-09-11
Status: agreed, not yet implemented
Branch target: `bossy-fork`

## Problem

Adding a dependency today means typing its full repository key:

```
bossy install github.com/danieleteti/delphimvcframework@3.4.2-magnesium
```

That is fine for one or two. The GTR dev-PC and CI baseline is 28 repositories,
provisioned by `X:\git_local\delphi-install-scripts\00_checkout_delphilibs.bat`.
Nobody is going to retype 28 repository URLs, their branch pins and their
search-path restrictions for every new project, and getting one of them wrong is
silent — a dependency pinned to the wrong branch still builds.

What is missing is a list of the libraries that are *available*, so a project can
pick from it.

## Scope

Per-project only. `bossy add` writes the current project's `bossy.json` and
delegates to the existing installer, which produces `bossy-lock.json`.

`00_checkout_delphilibs.bat` keeps managing the shared `X:\Delphi_libs\D13` tree
and the BDS user directory. Bossy does not learn to provision a global library
tree. CI is unaffected: it runs `bossy install` against a committed `bossy.json`
as it does now, so the catalog is only ever read at authoring time on a developer
machine.

## What the batch file actually contains

28 repositories: 19 external over HTTPS (GitHub, one Bitbucket), 9 internal over
SSH (`git@gitlab.gtr.de`).

Three clone flavours:

- plain `git clone --recursive` (26 of them)
- `--recursive` followed by `git submodule update --init --recursive --remote`
  (TaurusTLS only)
- branch-pinned `git clone -b 8.0-patches` (ZeosLib only)

Updating is `git reset --hard HEAD && git pull`, so every repository sits at the
tip of its default branch. There is no pinning of any kind.

Three targets are outside the library tree, under
`%USERPROFILE%\Documents\Embarcadero\Studio\<ver>\`: `code_templates`, `imports`,
`gtr-controls`.

The second half of the story is `config.toml` in the same directory. It carries,
per library, a `path` list and a `compilerlist`. Those map onto bossy's
`searchpaths` field and `engines.platforms` almost one to one, and they are the
part that is genuinely tedious to reconstruct by hand.

## Catalog

### Storage

```
~/.bossy/presets/
  remote.json    # written only by `bossy preset sync`, never hand-edited
  local.json     # personal entries and overrides
```

Merged on load, keyed on `id`. A `local` entry shadows a remote entry with the
same id in full. There is no field-level merging: when the remote entry later
changes, a partial override would silently produce a third thing that nobody
wrote.

Two files rather than one so that `sync` can overwrite its own file wholesale
without risking local work.

### Schema

```json
{
  "schema": 1,
  "source": "gtr:delphi/libraries/bossy-presets",
  "synced_at": "2026-09-11T09:12:44Z",
  "presets": [
    {
      "id": "dmvcframework",
      "name": "DMVC Framework",
      "description": "REST/MVC framework for Delphi",
      "repo": "github.com/danieleteti/delphimvcframework",
      "default_ref": { "kind": "tag", "value": "3.4.2-magnesium" },
      "searchpaths": ["sources", "lib/dmustache", "lib/loggerpro", "contrib"],
      "platforms": ["Win32", "Win64", "Win64x", "Linux64"],
      "tags": ["web", "gtr-standard"]
    }
  ]
}
```

`repo` is written in bossy's existing dependency-key syntax, so host aliases
resolve: `gtr:delphi/libraries/opengl` expands through `bossy config alias`. The
catalog therefore carries no SSH-versus-HTTPS decision — that already lives in
`host_protocols`, per machine, where it belongs.

`default_ref.kind` is one of `branch`, `tag`, `commit`, `default` (server HEAD).

`searchpaths` and `platforms` come from `config.toml`. Ticking `dmvcframework`
writes the dependency *and* its search-path restriction, which is what keeps a
repository that ships samples and unit tests next to its library from putting
hundreds of directories on the Delphi search path.

`tags` drives the TUI filter and makes a non-interactive `--tag gtr-standard`
possible.

### Distribution

The catalog lives in a GTR git repository. `bossy preset sync` shallow-clones it
through the existing `ports.Git` implementation, reads `presets.json` from its
root, validates it and writes `remote.json`. Going through the port means SSH
aliases, the GitLab CI job-token path and the native/embedded backend switch all
work without new code.

`--source` is stored in the global configuration on first use, so later syncs
take no argument.

Adding a library to the shared catalog is a merge request against that
repository. That review step is wanted: an entry propagates to every dev PC on
the next sync.

## Commands

```
bossy preset list [--tag <t>] [--source local|remote]
bossy preset show <id>
bossy preset add <id> --repo <key> [--ref <kind>:<value>]
                      [--searchpath <p>]... [--platform <p>]...
                      [--tag <t>]... [--name <n>] [--description <d>]
bossy preset update <id> [same flags]      # patches only what is passed
bossy preset rm <id>
bossy preset sync [--source <repo-key>]
bossy preset import <path|url>             # merge a catalog into local.json
```

`add`, `update` and `rm` write `local.json` only. The remote catalog is
read-only on the client.

```
bossy add                       # opens the TUI
bossy add --tag gtr-standard    # non-interactive, by tag
bossy add <id> [<id>...]        # non-interactive, by id
```

A new verb rather than an overload of `install`, because `install` takes
repository URLs and `add` takes catalog ids. Conflating them makes
`bossy add dmvcframework` ambiguous the moment someone has a directory of that
name.

`bossy add` edits `bossy.json` and then calls the existing installer service.
There is no second install path.

## TUI

### Layout

```
┌ bossy add — select dependencies ─────────────────────┐
│ / dmvc▊                             3 of 28 shown    │
│                                                      │
│ [x] delphimvcframework     tag 3.4.2-magnesium       │
│ [ ] delphiredisclient      branch main               │
│ [x] delphi-neon            branch main               │
│ [ ] spring4d               branch master             │
│ [x] zeoslib                branch 8.0-patches        │
│ [ ] TaurusTLS              branch main  (+submods)   │
├──────────────────────────────────────────────────────┤
│ delphimvcframework                                   │
│ github.com/danieleteti/delphimvcframework            │
│                                                      │
│ ref  (•) tag  ( ) branch  ( ) commit  ( ) default    │
│      ‹ 3.4.2-magnesium ›          [26 tags]          │
│ pin  [x] freeze resolved SHA into bossy.json         │
│ paths sources, lib/dmustache, lib/loggerpro, …       │
└──────────────────────────────────────────────────────┘
  space toggle · enter edit ref · / filter · ^s write
```

One screen: filterable checkbox list above, live detail pane for the highlighted
entry below. Enter edits the ref in place.

### Placement

`internal/adapters/primary/tui/`, a primary adapter and peer to `cli/`. It holds
no business logic — it reads the merged catalog from the `presets` service and
hands the chosen set to the `installer` service. `cli/add.go` decides between TTY
and flags and calls one or the other.

### Library

`bubbletea` + `bubbles` + `lipgloss`. `pterm`, already a dependency, has an
interactive multiselect but no filter and no detail pane, so it cannot express
this layout. Bubbletea's message loop also composes with the asynchronous
ref-listing below, and its `Update` is a pure function over model and message,
so the state machine is table-testable without a pty.

### Ref listing needs one port extension

Ref listing exists but is not clone-free on both transports.
`GetVersions` (`internal/adapters/secondary/git/git.go:85`) sends SSH
dependencies to `ListRefsNative`, which shells out to `git ls-remote` and needs
no clone, but sends HTTPS dependencies to `getVersionsEmbedded`, which requires
a `*git.Repository`. The TUI must not clone 28 repositories to show a tag list.

Add to the `GitRepository` port:

```go
// ListRemoteRefs enumerates a dependency's remote heads and tags without
// requiring a local clone.
ListRemoteRefs(dep domain.Dependency) ([]*plumbing.Reference, error)
```

Native implementation reuses `ls-remote` and the hardened `parseLsRemote`.
Embedded implementation uses `git.NewRemote(memory.NewStorage(), …).List()`.
Per the repository convention, both backends get it.

### Model

```
rootModel
 ├ listModel    filter, cursor, checkbox state
 ├ detailModel  ref kind radio, ref value picker, freeze checkbox
 └ refCache     map[presetID][]ref   (session-scoped)
```

Opening the detail pane for a preset whose refs are not cached fires a `tea.Cmd`
calling `ListRemoteRefs` under a timeout. The pane shows `loading refs…` and the
list stays usable. Listing is lazy and per-entry — never eager across the
catalog.

A listing failure is shown inline on that entry. It does not tear down the TUI,
and it does not fall back to a default branch: the entry keeps its catalog
default and is marked as unverified. Silent fallback to the main branch is the
exact failure mode fixed in `dda87ee`, and it must not come back through a new
door.

### Non-TTY

`bossy add` on a terminal that is not interactive (detectable with the existing
`go-isatty` dependency) exits with an error naming `--tag` and `<id>`, rather
than hanging a CI job on a keystroke that will never arrive.

## Write path

`bossy add` writes `bossy.json`, then calls the installer, which produces
`bossy-lock.json` as today.

| detail-pane choice | `dependencies` value |
|---|---|
| default | `>0.0.0` |
| branch `8.0-patches` | `8.0-patches` |
| tag `3.4.2-magnesium` | `3.4.2-magnesium` |
| commit | the 40-hex SHA |

The preset's `searchpaths` are merged into the manifest's `searchpaths` map
under the module key.

The `freeze` checkbox controls what goes into `bossy.json`, not the lock. The
lock records the resolved commit in every case. With freeze on, the chosen
tag or branch is resolved once and written as a literal SHA, so `bossy update`
cannot move it. With freeze off, the declared ref stays symbolic and `update`
may advance it.

## Seeding the catalog

A one-shot generator reads `00_checkout_delphilibs.bat` for the repositories and
`config.toml` for `path` and `compilerlist`, joins them on `lib_path`, and emits
`presets.json`. It is a uv script living in the catalog repository, not in the Go
tree, and it is throwaway: its output is reviewed by hand once and committed.

Review is not optional, because the two files disagree. `config.toml` writes
`ZeosLib-8.0-patches` where the batch file writes `zeoslib-8.0-patches`, and
bossy will install the module as `zeoslib`. Several `ident` values in
`config.toml` (`dmvcfw`, `bettertm`, `PYTHONENVIRONMENTDIR`) are unrelated to the
repository name and have to be mapped by hand.

## Testing

- `domain`: catalog merge precedence, schema validation, unknown-field handling.
- `services/presets`: `local.json` CRUD, sync validation, merge against a
  synthetic remote.
- `tui`: `rootModel.Update()` as a table test over synthetic `tea.KeyMsg` values,
  including the ref-listing failure path.
- `cli`: `--tag` and `<id>` non-interactive paths through the existing
  `cmd_test.go` harness, plus the non-TTY refusal.

## Out of scope

Global library-tree provisioning, `CompInstall` package registration, catalog
self-versioning.

---

## Known gaps in current bossy, for a follow-up session

These are things the 28 repositories will hit that this design does not fix.
None of them block the catalog and TUI work; they are recorded so the next
session does not rediscover them.

### 1. TaurusTLS and `submodule update --remote`

The batch file gives TaurusTLS special treatment (`:ProcessExt`):

```
git clone --recursive <url> <dir>
git submodule update --init --recursive --remote
```

Bossy does `--init --recursive` without `--remote`, in both backends
(`git_native.go:261`, `git.go:48`). The difference is real: without `--remote`,
submodules are checked out at the gitlink commit the superproject records;
with it, each submodule is advanced to the tip of its configured branch.
TaurusTLS carries Indy as a submodule, and the batch file's author evidently
wanted Indy's branch tip rather than the recorded commit.

So a TaurusTLS installed by bossy today will not match one installed by the
batch file, and may build against a different Indy.

Note that this is not a straightforward bug to close, because `--remote` is
directly opposed to what the lock file is for. A submodule that silently
advances to a branch tip makes an install non-reproducible, which is the
property the SHA-pinning work spent effort establishing. The two candidate
resolutions:

- **Do not replicate it.** Treat the recorded gitlink commit as correct and
  document that bossy installs differ from the batch script here. If TaurusTLS
  genuinely needs a newer Indy, the fix belongs upstream in TaurusTLS, by
  bumping its gitlink.
- **Replicate it behind an explicit opt-in**, e.g. a preset field
  `submodules: "remote"`, and record the resolved submodule SHAs in
  `bossy-lock.json` so the result stays reproducible even though the declared
  intent floats.

The second is more work but preserves the lock's guarantee. Decide before
putting TaurusTLS in the catalog; until then its preset should carry a warning
in `description`.

### 2. The three BDS-user-directory repositories

`code_templates`, `imports` and `gtr-controls` install into
`%USERPROFILE%\Documents\Embarcadero\Studio\<ver>\`, and `config.toml` gives
`gtr-controls` a search path rooted at `$(BDSUSERDIR)`. A per-project
`modules/<name>` checkout cannot express that. These three do not belong in the
catalog at all under the per-project scope, and the seeding script should skip
them rather than emit broken entries.

### 3. Search paths that only exist after a build

`config.toml` gives spring4d:

```
$(spring4d)\Library\Delphi$(delphi_main_version)\$(Platform)
```

Those directories hold compiled output, not source, and do not exist in a fresh
clone. Bossy's `searchpaths` entries are plain relative directories walked
recursively, with no `$(Platform)` or Delphi-version expansion. A literal
translation produces a preset pointing at nothing.

Two consequences to work out: whether bossy should expand IDE macros inside
`searchpaths` at all, and what a preset for a dependency that must be compiled
before its paths are valid should look like. Naming `Library` alone and letting
the recursive walk find everything is the cheap workaround, but it puts
wrong-platform DCUs on the search path.

### 4. IDE plugins are not libraries

`DDevExtensions` and `EditInVsCodeDelphiPlugin` are IDE plugins. They are built
and registered into the IDE, never placed on a project's search path. Like the
BDS-user-directory repositories, they should not become catalog entries.

That leaves roughly 23 of the 28 as genuine per-project candidates.

### 5. Disk cost of a 28-dependency project

`modules/<name>` is a real git checkout per dependency per project, not a link
into the per-user cache. GLXEngine and spring4d are large. A developer with
several projects each declaring twenty-odd dependencies will notice. Worth
measuring before recommending that every project declare the full baseline;
`git worktree` or a junction into the cache would be the direction if it turns
out to matter.

### 6. Bitbucket is untested

spring4d comes from `bitbucket.org`. There is no Bitbucket-specific code path —
it goes through the generic HTTPS handling, which should be fine — but no test
covers it and nobody has run it. Verify before the preset ships.
