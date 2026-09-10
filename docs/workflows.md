# Bossy workflows

Task-oriented recipes for everyday work. The command reference lives in
[`README.md`](../README.md), and CI authentication and runner setup in
[`ci.md`](ci.md).

## You cloned a project that already uses bossy

```sh
git clone git@github.com:acme/warehouse-app.git
cd warehouse-app
bossy install
```

`bossy install` with no arguments reads `bossy.json` and `bossy-lock.json`,
checks out every dependency at the commit recorded in the lock, populates
`modules/`, and wires the resulting source directories into the Delphi library
and browsing paths. That is the whole onboarding step; the IDE can be opened
straight afterwards.

### What belongs in source control

Commit `bossy.json` and `bossy-lock.json`. Both are inputs to that first
`bossy install`, and without the lock a colleague's build is only as
reproducible as upstream tags happen to be.

`modules/` is generated output. Ignore it:

```gitignore
modules/
```

`bossy init` writes `bossy.json` and nothing else, so the ignore rule is yours
to add. Do not copy this repository's own `.gitignore` into a consumer project:
it excludes `bossy.json` and `bossy-lock.json`, because in bossy's own tree
those are scratch files left behind by the Go test runs.

## Starting a new project

```sh
mkdir warehouse-app && cd warehouse-app
bossy init                  # add -q to skip the prompts and take defaults
bossy install horse@3.3.5
git add bossy.json bossy-lock.json
git commit -m "Add horse 3.3.5"
```

`bossy init` creates the manifest. `bossy install <pkg>@<version>` adds the
entry to `dependencies` and records the resolved commit in the lock. A bare
name expands to `github.com/hashload/<name>` and is matched case-insensitively;
the README lists the other accepted URL forms.

## Pinning a dependency to a branch

A branch name works anywhere a version string does, both in the manifest and on
the command line:

```json
{
  "dependencies": {
    "github.com/hashload/horse": "dev"
  }
}
```

```sh
bossy install horse@dev
```

Version strings are parsed as semver constraints first. `dev` is not a valid
constraint, so resolution falls back to an exact-name match against the
repository's ref list, which holds branches alongside tags. The failed parse is
still reported, so a successful branch install prints a warning:

```
⚠️ Installation Warnings:
   - horse: Version constraint 'dev' not supported: improper constraint: dev
```

It appears whenever bossy resolves the branch — on the first install and on
every `bossy update`. Replaying an existing lock entry skips resolution
entirely, so the routine `bossy install` a colleague runs is quiet.

`bossy dependencies -v` labels what a branch pin resolved to:

```
.
└── warehouse-app:
    └── horse@dev <- branch based
```

## Branch pins stay reproducible

Every install records the commit it actually checked out. With the manifest
above, `bossy install` produces:

```json
{
  "hash": "d41d8cd98f00b204e9800998ecf8427e",
  "updated": "2026-09-10T11:19:04+02:00",
  "installedModules": {
    "github.com/hashload/horse": {
      "name": "horse",
      "version": "dev",
      "commit": "77db588aa654e57b23bc416429be43c0613de2c3",
      "hash": "f3a6f17d826157b1ea20747f59165e42",
      "artifacts": {}
    }
  }
}
```

`version` records the label that was resolved, which for a branch pin is the
branch name (a `^3.0.0` range would record the tag it matched). `commit` is what
landed in `modules/horse`.

Now upstream moves: someone merges into `dev` and the branch tip advances past
`77db588`. A colleague clones the repository, gets your committed lock, and runs
`bossy install`. bossy takes the SHA from the lock, checks out `77db588` in
detached HEAD, and skips the pull, because a pinned commit has nothing to pull
towards. Their `modules/horse` matches yours commit for commit, and the lock
comes back unchanged. A CI runner behaves the same way, which is what makes a
branch-pinned dependency safe to build in a pipeline.

Picking up the new tip is a separate step:

```sh
bossy update horse
```

`bossy update` ignores the lock, re-resolves `dev` against the remote, checks
out the new tip and rewrites `commit`. Review the resulting lock diff and commit
it like any other change.

## Changing what a dependency points at

| Goal | Command |
|---|---|
| Install exactly what the lock records | `bossy install` |
| Add a dependency at a tag | `bossy install horse@3.3.5` |
| Add a dependency at a branch | `bossy install horse@dev` |
| Move an existing dependency to another branch | `bossy update horse@dev` |
| Move to the latest head of the branch it is already on | `bossy update horse` |
| Return to a pinned tag from a branch | `bossy update horse@3.3.5` |
| Re-resolve everything the manifest declares | `bossy update` |

Each of the `update` forms rewrites both `bossy.json` (when a version is given)
and the `commit` in `bossy-lock.json`, so a switch is one command:

```sh
$ bossy update horse@dev
$ grep hashload bossy.json
        "github.com/hashload/horse": "dev"
$ grep -E '"version"|commit' bossy-lock.json
            "version": "dev",
            "commit": "77db588aa654e57b23bc416429be43c0613de2c3",
```

Use `update`, not `install`, to move a pin. `bossy install horse@dev` on a
dependency that is already in the lock rewrites the manifest entry but leaves
the lock and the checkout where they were, since `install` exists to replay the
lock. The result is a manifest and a lock that disagree, and a `modules/` tree
that still holds the old commit.

`bossy update <dep>` without a version leaves the declared version alone and
only re-resolves it, so pointing an update at a branch-pinned dependency will
not silently convert it into a tag range.

### Selecting interactively

`bossy update --select` opens a checklist of the dependencies in `bossy.json`,
each annotated with its state (`up to date`, `not installed`, or
`<locked> → <declared>`):

```
Select dependencies to update (Space to select, Enter to confirm)::
> [✗] horse (up to date)
enter: select | tab: confirm | left: none | right: all| type to filter
```

Selecting a dependency does the same work as naming it on the command line: the
version constraint is re-resolved against the remote and the lock is rewritten.
Dependencies you leave unticked are not touched.

Read the annotations with care. They compare version strings, not commits: a
branch pin shows `up to date` whenever the branch name in `bossy.json` matches
the one in the lock, however far the tip has moved since. Only tag ranges get a
label that means anything. When you already know which dependency you are after,
name it instead:

```sh
bossy update horse
bossy update horse jhonson       # several at once
```

## Pinning to an exact commit

A raw commit SHA (7–40 hex characters) is accepted as a version and is checked
out verbatim, with no resolution and no pull:

```json
{
  "dependencies": {
    "github.com/hashload/horse": "4e99bebc836f4cf4a943bf061158ca061f0a5a2d"
  }
}
```

```sh
bossy install
```

Both `version` and `commit` in the lock end up holding the SHA, and since there
is nothing to re-resolve, `bossy update` leaves it alone. This is the
right pin for a dependency you need frozen against a specific upstream state,
such as while waiting for a fix to be tagged. With shallow clone enabled, a SHA
older than the shallow cut-off triggers an automatic deepening fetch on first
use.

## Removing a dependency

```sh
bossy uninstall horse
```

The entry is dropped from `bossy.json`, `modules/horse` is deleted and the lock
entry goes with it, including when it was the only dependency left. Commit both
files.

## When a dependency brings its samples along

`bossy install` puts every directory of a dependency that holds source onto
the compiler search path. For most Delphi libraries — a folder of `.pas`
files — that is exactly right.

It goes wrong for a repository that ships its library next to demo projects.
delphimvcframework is the case you will meet first: one `sources` directory
against roughly 190 directories of samples, unit tests and vendored demos,
all of which land in your `.dproj`.

The symptom is a build that fails before compiling anything:

```
MSB6003: The specified task executable "dcc" could not be run.
The filename or extension is too long
```

MSBuild passes the search path to `dcc` on the command line, and Windows caps
a command line at 32767 characters. A single dependency can spend that on its
own.

List the directories that hold the library:

```json
{
  "searchpaths": {
    "github.com/danieleteti/delphimvcframework": ["sources", "lib/loggerpro"]
  }
}
```

Then re-run `bossy install` and commit the rewritten `.dproj`. Paths are
relative to `modules/<name>` and still walked recursively, so listing a root
picks up its subdirectories.

Worth checking even when the build succeeds: a samples directory on the
search path can shadow one of your own units with a same-named demo unit, and
that failure is far harder to read than the one above.
