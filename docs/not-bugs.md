# Not bugs

Behaviour a review may take for a bug, kept on purpose. A review lists what it finds here as ignored, not as a finding. Each item says why it stays; one that no longer holds goes back to being a finding.

## Git and the Brain

- **A global credential helper that opens a window is not stopped.** nerve stops git, ssh and askpass prompts; a helper with its own window is the user's choice.
- **nerve does not check git's version.** The README names 2.31; an older git shows its own error. See the [backlog](backlog.md).
- **`check` and `update` need HOME.** The machine adapter finds its folder at startup. A Brain with a source needs the cache folder anyway, and CI runners set HOME.

## Checkouts and links

- **A worktree of a bare repo is its own checkout.** A bare repo has no main checkout for its worktrees to belong to. Its exclude lines go to the bare repo's `info/exclude`. `TestLocateTakesAWorktreeOfABareRepoAsItsOwnCheckout` pins it.
- **In a moved checkout, every setting as the Brain sets it is nerve's**, so `project rm` takes out a value the checkout had before nerve, if it equals the Brain's. After a move nerve cannot tell the two apart, and leaving its own settings would leave its hooks running. sync.md says so.
- **A checkout path reached through a link** in a hand-written machine file is not found. `project add` lists the path git gives, with links followed.
- **A link is removed, then made again**, not swapped at once. A run that dies between the two leaves no link, and the next sync makes it.
- **Writes follow a link at `brain.json`**, and make a missing folder it points into. The Brain is the user's own; only a checkout's files are refused as links.
- **The nerve skill is written through a link someone put at its `SKILL.md` in the cache.** The cache is the user's own, as the Brain is, and a fetched source holds no links.
- **A relative link target holding `..` is read as written**, not through the links on its way. Only a hand-made link at `brain.json` or in nerve's folders can hold one.
- **After a move, an own skill whose `SKILL.md` cannot be read keeps its link at `project rm`.** nerve cannot tell its link from the user's; `check` reports the skill.
- **Changes to the exclude block are not reported.** sync.md says links are kept out of git by it, and it follows them.
- **While any state file cannot be read, no exclude block loses a line.** nerve cannot tell which repo the file was for. The next sync of its tree replaces it; a state left by a checkout taken out of the machine file by hand stays until the user deletes it.
- **A gone worktree's lines stay in the exclude block** until a checkout of its repo syncs. Nothing else writes the block, and an extra line only hides a path from git.
- **Folders a sync makes on the way to a link get no event of their own.** The `linked` line names the path.
- **After a move, a checkout whose nerve skill link was deleted keeps its other links as the user's.** nerve's own link is its only sign of a past sync; without it, nerve cannot tell its links from the user's.
- **Links into a cleared cache break until the next sync.** The system owns the cache folder, and a sync fetches and links again. sync.md says so.
- **Links point outside the project**, so a container or sandbox that mounts only the project does not see the skills. Copies would lose live edits of own skills and need their own drift.
- **No Windows.** Links are symlinks, which Windows gives only with extra rights, and releases build for Linux and macOS. The README says so.

## Machine files and runs

- **Two runs at once may race on the machine file**, and two `init`s on its creation. Runs at once are rare, and the next sync redoes what a lost write held.
- **The exclude lock waits with no bound.** Its holders only read state and write one file, and the lock dies with its process.
- **A run killed mid-write leaves a temp file beside the file.** A sweep needs a name only nerve makes: see the [backlog](backlog.md).
- **nerve makes `~/.config` 0700 when it is missing.** It holds this machine's files, which may hold secrets.
- **An existing `settings.local.json` keeps its mode.** Only a file nerve makes is for the user only.
- **`main.go` ignores the machine file's load error in `onMachine`.** Every use case loads it again and reports it.
- **A local overlay left empty is deleted.** It is nerve's own file on this machine, not a project's settings, which nerve never deletes.
- **After a sync that kept a third-party skill at its old pin**, offline say, `status` shows the checkout synced. The sync reported the problem, and the next one relinks it.
- **A setting that goes to the local overlay still needs the Brain's remote.** Where a setting goes depends on the Brain's `allowedSettings`, read as the remote holds it.
- **`push` with no git identity leaves the Brain's files staged**, behind git's own message. Staging loses nothing, and the next `push` commits them.

## Settings

- **Sync prints personal and drifted values as they are**, in its lines and the commands that keep them. They are the user's own, in their own checkout, and the session report shows them only to the user.
- **A hook's event is not checked.** Claude Code adds events; a list in nerve would refuse a new one. See the [backlog](backlog.md).
- **An overlay can add items to a Brain list, but not take one out.** Arrays join without repeats, as sync.md says.
- **Keeping drift of an overlay's setting the Brain allows now sends it to the Brain.** The drift line names `nerve setting set`, which puts an allowed key in the Brain and drops it from the overlay, as it always does. The nerve skill asks before a value that looks secret goes into the Brain.
- **A suggested `nerve allow add` names the setting's own key**, such as `mcpServers.db.headers.X-Api-Key`, not a wider one. Only where needed: allowing more is the user's choice.

## Sources

- **`push` checks every pin, so a source it cannot fetch, offline say, blocks it.** Every machine syncs what it pushes; the check names the source.

- **nerve looks for a skill in fixed places**, not the whole repo: the root, `skills/<name>/`, `<name>/`, or where `path` says.
- **A source's cache is trusted by its folder, and never evicted**, nor are the lock files beside it. It is renamed into place whole, and named by its commit. Fetches of one commit take turns. Only `update` replaces a download with no date beside it, once the new one is whole; a skill read through a link at that moment may miss its files.
- **The cache needs file locks**, as the exclude lock does. On a filesystem without them, such as NFS without its lock service, fetching a source fails.
- **`update --dry-run` reads `brain.json` as this machine holds it**, even behind its remote. It moves nothing, so the Brain's work not pushed never stops it; `nerve sync` first makes it the remote's. See [ADR 0013](adr/0013-update-dry-run.md).
- **`update` shows commit dates in UTC.** A date then reads the same on every machine, whatever its time zone.
- **A fetch checks out the whole repo, under git's two-minute bound.** A repo too big for it fails each time and caches nothing.
- **`update` reads the whole diff before cutting it** at 256 KiB.

## Skills and frontmatter

- **An own skill folder may be a link, even to a folder outside the Brain.** `check` reads it as it is; other machines get the link as git holds it.

- **YAML-only escapes such as `\0` and `\e`** come out as written in a frontmatter value.
- **nerve checks an agent's name and description, not their length.** It leaves an agent's limits to Claude Code.

## Docs and words

- **brain.md names `state/`, and sync.md names `info/exclude`.** A message tells the user to delete a file in the one, or to fix the other by hand.
- **Git's own words stay in git's sense**: upstream, the global git ignore file, the repo. So does the machine file's `path` key.
- **`nerve help` repeats what the README says of `--project`.** The usage is the CLI's own text, not a doc.
- **brain.md lists the allowed settings in full.** Users need the list where they write `brain.json`; a review checks it against `nerveAllows`.
- **The nerve skill names each event an agent acts on.** It says what to do with each one, which no other doc does; a review checks it against the event kinds.
- **The report shows private-use characters as they are.** It makes control characters and invisible format characters visible, which can hide or rewrite text; private-use ones cannot.

## Tests

- **The open-bug machinery has no users**: the `[bugs]` script condition, `NERVE_OPEN_BUGS`, `openBug`. CLAUDE.md's test rule relies on it.
- **The exclude lock and fetch lock tests wait 200 ms.** Showing that a second run cannot get in needs a wait, and a working lock never fails it.
- **Some fakes never fail or follow links**, such as `Load`, `State` and `Locate`. No bug hides behind them; the adapters' own tests cover those paths.
