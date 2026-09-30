<p align="center">
  <source media="(prefers-color-scheme: dark)" srcset="images/lnk-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="images/lnk-light.png">
  <img alt="Project Logo" src="images/lnk-dark.png" width="128">
</p>

# lnk

[![Go Version](https://img.shields.io/github/go-mod/go-version/fuzzyporpoise/lnk)](https://go.dev/) [![License](https://img.shields.io/github/license/fuzzyporpoise/lnk)](./LICENSE) [![Build Status](https://img.shields.io/github/actions/workflow/status/fuzzyporpoise/lnk/ci.yml?branch=main)](https://github.com/fuzzyporpoise/lnk/actions)

**Lightweight git-native dotfiles management.**

Track dotfiles across machines with one command. Lnk moves files into a Git repo (defaults to `~/.config/lnk`; override with `--repo` flag or `LNK_HOME` / `LNK_REPO`), symlinks them back, and stays out of your way. Setup multiple host/scopes in addition to a common scope to separate work and personal settings.

## Quick Demo

```bash
lnk init                                       # create a local repo
lnk clone git@github.com:you/dotfiles.git      # clone a remote repo
lnk create ~/.vimrc ~/.bashrc ~/.gitconfig     # create empty files and track them
lnk create --dir ~/.config/awesome             # create empty directory and track it
lnk add ~/.vimrc ~/.bashrc ~/.gitconfig        # track existing files
lnk add --host work ~/.ssh/config              # per-machine config
lnk push                                       # push to remote
lnk update                                     # pull and restore symlinks
```

## Getting Started

### Install

```bash
curl -sSL https://raw.githubusercontent.com/fuzzyporpoise/lnk/main/install.sh | bash
```

Or grab a binary from [releases](https://github.com/fuzzyporpoise/lnk/releases), or build from source:

```bash
go install go.fuzzyporpoise.dev/lnk/v2@latest
```

*NOTE:*

Installing with Homebrew is not yet supported.

### Quick Start

1. **Initialize** on a new machine:

   ```bash
   lnk clone git@github.com:you/dotfiles.git --bootstrap
   lnk update
   lnk update --host $(hostname)
   ```

   That's it. Bootstrap runs automatically (with flag), symlinks get restored, you're working.

2. **Add** dotfiles on your daily machine:

   ```bash
   lnk add ~/.vimrc ~/.bashrc ~/.gitconfig
   ```

3. **Sync** changes:

   ```bash
   lnk push
   ```

## How it works

```bash
Before: ~/.vimrc (regular file)
After:  ~/.vimrc → ~/.config/lnk/.vimrc (symlink into git repo)
```

Common files live at the repo root (v1) or under `common.lnk/` (v2). Host-specific files go in `<hostname>.lnk/` subdirectories. A plain text `.lnk` file tracks what's managed — one path per line, no special format.

```bash
~/.config/lnk/
├── .lnkrepo               # version marker (v2)
├── .lnk.common            # tracked common files (v2)
├── .lnk.work              # tracked work-specific files
├── common.lnk/            # v2: common storage directory
│   ├── .vimrc
│   └── .gitconfig
└── work.lnk/              # host-specific storage
    └── .ssh/config
```

### Legacy Format

```bash
~/.config/lnk/
├── .lnk                   # tracked common files (v1)
├── .lnk.work              # tracked work-specific files          
├── .vimrc                 # v1: common files and directories in repo root.
├── .gitconfig
├── .config/               
│   └── ghostty
└── work.lnk/              # host-specific storage
    └── .ssh/config
```

## Features

### Create files and directories

```bash
lnk create ~/.vimrc ~/.bashrc             # create empty files and track them
lnk create --dir ~/.config/awesome        # create empty directory and track it
lnk create ~/.config/awesome/             # same as --dir (trailing slash)
lnk create --host work ~/.ssh/config      # create and track in host scope
```

All paths in one `create` invocation must be the same kind: either all files or all directories. Use `--dir` or a trailing slash to request directories.

### Add files

```bash
lnk add ~/.vimrc ~/.bashrc                # multiple at once
lnk add --host laptop ~/.ssh/config       # host-specific
```

### Move between scopes

```bash
lnk move ~/.ssh/config --to-common        # move to common scope
lnk move ~/.vimrc --to-host work          # move to host scope
```

### Sync

```bash
lnk status                                # full git status output
lnk status --color                        # colorized git status
lnk diff                                  # uncommitted changes
lnk diff --color                          # colorized diff
lnk commit -m "updated vim config"        # stage and commit - not needed after `lnk add`
lnk commit                                # commit with default message - not needed after `lnk add`
lnk push                                  # push existing commits
lnk pull                                  # pull repo changes
lnk restore                               # restore symlinks (no pull)
lnk update                                # pull + restore symlinks
lnk update --host work                    # include host-specific files
lnk restore --dry-run                     # preview what would be restored
```

`status` shows the full `git status` output. If no remote is configured, it prints `Remote not set` at the top. Use `--color` to enable colorized output (default is plain text).

### Remove

```bash
lnk remove ~/.vimrc                       # stop managing, restore file locally
lnk forget ~/.bashrc                      # stop tracking, keep stored repo copy
```

`remove` restores the file to its original location and removes it from the repo. `forget` keeps the stored copy in the repo but removes the symlink and tracking entry — useful for temporarily stopping management of a path.

### List

```bash
lnk list                                  # common files
lnk list --host work                      # host-specific
lnk list --all                            # all scopes
```

When listing all scopes, host profiles show `[active]` if at least one managed symlink exists on the current machine, or `[not installed]` otherwise. Common scope is always active.

### Health checks

```bash
lnk doctor                                # audit repo and profile health
lnk doctor --fix                          # apply safe automatic fixes
lnk doctor --fix --prune-empty            # also remove empty host scopes and project storage
lnk doctor --all                          # check all scopes
```

`lnk doctor` checks project scope as well as host/common scope: it reports orphaned project storage, broken project symlinks, and missing project checkouts using the machine-local project registry. Project issues are listed with severity and a suggested fix.

When restoring symlinks, if a real file exists at the target location (not a symlink), it will be renamed to `<path>.lnk-backup` to preserve your data before the symlink is created. Check for `.lnk-backup` files after running `restore`, `update`, or `doctor` if you expect them. The git hook path (`lnk hooks run ...`) is collision-safe and does not create `.lnk-backup` files; it reports collisions to stderr and leaves the real file in place.

### Format migration

```bash
lnk format                                # show current repo format
lnk format --v2                           # migrate to v2 format, i.e. common files under lnk.common
lnk format --v1                           # migrate back to v1 format
```

v2 aggregates common dotfiles under `common.lnk/` for cleaner repo organization. New repos are initialized as v2 by default. After the format, run a `lnk doctor --fix` as all your common symlinks will be broken.

### Bootstrap

Drop a `bootstrap.sh` in your dotfiles repo. Lnk runs it automatically on `lnk clone <url> --bootstrap`.

```bash
lnk clone <url>                           # clones repo locally, no bootstrap.sh
lnk clone <url> --bootstrap               # runs bootstrap.sh after clone
lnk bootstrap                             # run manually
```

### Project scope

Track project-local configuration files without committing them to the project's own git repository. Useful for `.crush/crush.json`, `.vscode/settings.json`, repo-specific shell aliases, or any file you want backed up in your dotfiles repo but not pushed upstream.

Project scope uses a `.lnkinclude` file inside the project root. Patterns follow `.gitignore` syntax, but a match means "include". Global patterns live in your lnk repo root (`.config/lnk/.lnkinclude`) and apply to every project; local patterns are project-specific and are evaluated after the global ones, so they can negate a global include with `!`.

```bash
# inside a git repository
lnk project init                          # create an empty .lnkinclude
lnk project add .crush/**                 # track all files under .crush/
lnk project add .vscode/settings.json     # track a single file
lnk project list                          # show effective global + local patterns
lnk project list --all                    # list stored projects and file counts
lnk project push                          # move matches to lnk storage and symlink back
lnk project sync                          # reconcile patterns, live files, and storage
lnk project sync --all                    # reconcile every stored project
lnk project sync --prune-deletions        # also drop storage for files deleted locally
lnk project cache --scan ~/code           # discover local checkouts and update the project registry
lnk project restore                       # recreate symlinks from storage
lnk project restore --dry-run             # preview what would be restored
lnk project pull                          # pull lnk repo and restore
lnk project untrack .crush/**             # remove a local pattern and restore its files
lnk project untrack --keep .crush/**      # remove a pattern but leave files managed
lnk project remove                        # stop managing the project, restore all files
lnk project forget                        # stop managing the project, keep stored files

# implicit detection in update/restore
lnk update                                # also restores any detected project scope
lnk restore                               # also restores any detected project scope
lnk update --no-project                   # skip automatic project-scope detection
lnk restore --no-project                  # skip automatic project-scope detection

# global patterns (apply to every project)
lnk project add --global AGENTS.md        # include AGENTS.md everywhere
lnk project add '!AGENTS.md'              # then exclude it in one project
lnk project untrack --global AGENTS.md    # remove the global pattern
```

Matched files are stored under `projects/<normalized-origin>/<path>/` in your lnk repo (derived from the project's origin remote) and symlinked back into the project. Existing files at symlink locations are backed up to `<path>.lnk-backup` during restore, just like host/common scope restores. The normalized origin is also the project's identity in lnk, so untrack a project before changing its remote (see [Notes and edge cases](#notes-and-edge-cases)).

Project checkouts are tracked in a machine-local registry at `$XDG_CACHE_HOME/lnk/registry.json`, falling back to `~/.cache/lnk/registry.json` and overridable with `LNK_REGISTRY`. The registry is updated automatically on `project push` and `project sync`, and is used by `project sync --all` and `lnk doctor` to find local projects without scanning `$HOME`. It lives outside the lnk repo so moving or renaming the repo does not orphan the mapping.

`lnk update` and `lnk restore` automatically detect project scope when they run inside a git repo that contains a `.lnkinclude` file. The project scope is restored alongside the common and host scopes, and a `(project scope: <id>)` message is printed to stderr. Use the global `--no-project` flag to skip automatic detection.

### Notes and edge cases

- **Global patterns are hand-managed** (or edited via `--global`): they apply to every project, so negate them per project with a local `!` pattern. Quote the `!` in your shell (`'!AGENTS.md'`) or zsh's history expansion will eat it before lnk sees it.
- **Files are tracked individually**, not as directory symlinks. A `.todo/` pattern matches every file under it, so new files are picked up by the next `project push`/`project sync`. This differs from `lnk add`, which symlinks a whole directory as one unit.
- **Files tracked by the project's own git are left alone.** If a match is committed upstream (a typical `AGENTS.md`), push/sync skip it with a warning to avoid replacing a committed file with a machine-local symlink; use `--force` to override.
- **The lnk repo protects itself.** Project commands refuse to run inside the lnk repository (or any clone of it) to prevent storing it inside its own storage.
- **Reconciliation is explicit for deletions.** `project sync` reports stored files whose live copies were deleted; they are only removed from storage with `--prune-deletions`.
- **The project registry is machine-local.** It is maintained automatically by `project push` and `project sync`, lives at `$XDG_CACHE_HOME/lnk/registry.json` (falling back to `~/.cache/lnk/registry.json`; override with `LNK_REGISTRY`), and is never synced. Use `project cache --scan <dir>` to populate or repair it on a new machine or after moving checkouts.
- **Project identity comes from `origin`.** Storage lives at `projects/<normalized-origin>/`, so switching remotes, renaming the repo, or moving a checkout that has no `origin` (its id is hashed from the local path) orphans the old storage directory. `project remove`, `project forget`, and `project sync` then report `no stored files for this project`, while the live symlinks still point at the old path and `lnk doctor` reports the leftovers as orphaned project storage. Untrack before changing the remote with `lnk project remove` (or `forget`); if the remote has already moved, point `origin` back at the old URL, run `lnk project remove`, then set the new URL and `lnk project push` to re-adopt.

### Hooks

Install opt-in git hooks so lnk restores symlinks automatically after git operations.

```bash
lnk hooks install                             # post-merge hook in ~/.config/lnk
lnk hooks install --project                   # post-checkout hook in current project repo
lnk hooks uninstall                           # remove lnk's post-merge hook
lnk hooks uninstall --project                 # remove lnk's post-checkout hook
```

The `post-merge` hook runs inside the lnk repo after a `git pull` and recreates any missing common-scope symlinks. The `post-checkout` hook runs inside a project repo after switching branches and recreates missing project-scope symlinks. Both hooks are collision-safe: if a real file occupies a symlink target, the hook reports the collision to stderr and leaves the file untouched (no `.lnk-backup`).

Hooks are installed as shell scripts that delegate to `lnk hooks run <hook-name>`, so they always use the same lnk binary that was present at install time.

## Man pages

Man pages are generated from the Cobra command tree and ship with release archives.

```bash
make man                                  # generate pages in man/
man man/lnk-project-push.1                # read a generated page
```

## Commands

| Command | What it does |
| --- | --- |
| `init` | Create or adopt a local lnk repo |
| `clone <url> [--bootstrap]` | Clone a remote lnk repo |
| `add [--host H] <path...>` | Track files (move to repo + symlink) |
| `create [--dir] [--host H] <path...>` | Create empty files or directories and track them |
| `move <path> (--to-common \| --to-host H)` | Move a tracked path between scopes |
| `remove [--host H] <path>` | Stop managing, restore file locally |
| `forget [--host H] <path>` | Stop tracking, keep stored repo copy |
| `list [--host H \| --all]` | Show tracked files by scope |
| `status [--color]` | Show full git status output |
| `diff [--color]` | Show uncommitted changes |
| `commit [-m message]` | Stage all changes and commit |
| `push` | Push existing commits |
| `pull` | Pull repo changes |
| `restore [--host H] [--dry-run] [--no-project]` | Restore symlinks without pulling (auto-detects project scope) |
| `update [--host H] [--no-project]` | Pull and restore the effective profile (auto-detects project scope) |
| `doctor [--host H \| --all] [--fix] [--prune-empty]` | Audit and fix repo health, including project scope |
| `format [--v1 \| --v2]` | Migrate repo format |
| `bootstrap` | Run bootstrap.sh explicitly |
| `project init` | Activate project scope in the current git repo |
| `project add <pattern...>` | Add patterns to the project's `.lnkinclude` |
| `project list` | Show effective project patterns |
| `project untrack [--keep] <pattern>` | Remove a pattern from the project's `.lnkinclude`, restoring its files unless `--keep` |
| `project push [--force]` | Move matching project files to lnk storage |
| `project sync [--all] [--dry-run] [--prune-deletions] [--force]` | Reconcile patterns, live files, and storage |
| `project cache --scan <dir>` | Discover local checkouts and update the project registry |
| `project restore [--dry-run] [--force]` | Recreate project symlinks from storage |
| `project pull [--force]` | Pull lnk repo and restore project symlinks |
| `project remove` | Stop managing the project: restore all files and delete storage |
| `project forget` | Stop managing the project but keep stored files |
| `hooks install [--project]` | Install lnk's git hooks |
| `hooks uninstall [--project]` | Remove lnk's git hooks |
| `hooks run <hook-name> [args...]` | Entry point used by installed git hook scripts |

## Global Options

Available with all commands:

| Option | Default | What it does |
| --- | --- | --- |
| `--repo <path>` | `~/.config/lnk` | Path to the lnk repository |
| `--no-project` | `false` | Skip automatic project-scope detection |

## Acknowledgements

This originally started off as a fork of [yarlson/lnk](https://github.com/yarlson/lnk) with a number of features that I wanted. It has since turned into a standalone version after I saw the plan to rewrite a v2 in Rust. I've cleaned up the legacy code and added some opinionated fixes along the way. This should™ be fully compatible with the original repos from yarlson's tool, but now stands alone. I can't guarantee backwards or cross compatibility going forward so use both at your own peril.

The idea of a project scope was born out of seeing [claytercek/offstage](https://github.com/claytercek/offstage). It felt like a good extension of what was already built out here, but I wanted to streamline it and have it fit with the intent I've curated here, namely a targeted working snapshot of my different machine profiles.

## Contributing

```bash
git clone https://github.com/fuzzyporpoise/lnk.git
cd lnk
make check    # fmt, vet, lint, test
```

## Contributors

<a href="https://github.com/fuzzyporpoise/lnk/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=fuzzyporpoise/lnk" />
</a>

## License

[MIT](LICENSE)
