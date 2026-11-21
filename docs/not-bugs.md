# Not bugs

Behaviour a review may take for a bug, kept on purpose. A review lists what it finds here as ignored, not as a finding. Each item says why it stays; one that no longer holds goes back to being a finding.

## Git and the Brain

- **A global credential helper that opens a window is not stopped.** nerve stops git, ssh and askpass prompts; a helper with its own window is the user's choice.
- **`check` and `update` need HOME.** The machine adapter finds its folder at startup. A Brain with a source needs the cache folder anyway, and CI runners set HOME.

## Checkouts and links

- **A worktree of a bare repo is its own checkout.** A bare repo has no main checkout for its worktrees to belong to. Its exclude lines go to the bare repo's `info/exclude`. `TestLocateTakesAWorktreeOfABareRepoAsItsOwnCheckout` pins it.
- **After a move, `project rm` leaves an empty `.claude/skills`.** nerve cannot know it made that folder before the move.
- **In a moved checkout, every setting as the Brain sets it is nerve's**, so `project rm` takes out a value the checkout had before nerve, if it equals the Brain's. After a move nerve cannot tell the two apart, and leaving its own settings would leave its hooks running. sync.md says so.
- **A checkout path reached through a link** in a hand-written machine file is not found. `project add` lists the path git gives, with links followed.
- **A link is removed, then made again**, not swapped at once. A run that dies between the two leaves no link, and the next sync makes it.
- **Writes follow a link at `brain.toml`**, and make a missing folder it points into. The Brain is the user's own; only a checkout's files are refused as links.
- **The nerve skill is written through a link someone put at its `SKILL.md` in the cache.** The cache is the user's own, as the Brain is, and a fetched source holds no links.
- **A relative link target holding `..` is read as written**, not through the links on its way. Only a hand-made link at `brain.toml` or in nerve's folders can hold one.
- **After a move, an own skill whose `SKILL.md` cannot be read keeps its link at `project rm`.** nerve cannot tell its link from the user's; `check` reports the skill.
- **Changes to the exclude block are not reported.** sync.md says links are kept out of git by it, and it follows them.

## Machine files and runs

- **Two runs at once may race on the machine file**, and two `init`s on its creation. Runs at once are rare, and the next sync redoes what a lost write held.
- **The exclude lock waits with no bound.** Its holders only read state and write one file, and the lock dies with its process.
- **A run killed mid-write leaves a temp file beside the file.** A sweep needs a name only nerve makes: see the [backlog](backlog.md).
- **nerve makes `~/.config` 0700 when it is missing.** It holds this machine's files, which may hold secrets.
- **An existing `settings.local.json` keeps its mode.** Only a file nerve makes is for the user only.
- **`main.go` ignores the machine file's load error in `onMachine`.** Every use case loads it again and reports it.

## Settings

- **An overlay can add items to a Brain list, but not take one out.** Arrays join without repeats, as sync.md says.

## Sources

- **nerve looks for a skill in fixed places**, not the whole repo: the root, `skills/<name>/`, `<name>/`, or where `path` says.
- **A source's cache is trusted by its folder, and never evicted.** It is renamed into place whole, and named by its commit.
- **A fetch checks out the whole repo, under git's two-minute bound.** A repo too big for it fails each time and caches nothing.
- **`update` reads the whole diff before cutting it** at 256 KiB.

## Skills and frontmatter

- **YAML-only escapes such as `\0` and `\e`** come out as written in a frontmatter value.
- **nerve checks an agent's name and description, not their length.** It leaves an agent's limits to Claude Code.

## Docs and words

- **brain.md names `state/`, and sync.md names `info/exclude`.** A message tells the user to delete a file in the one, or to fix the other by hand.
- **Git's own words stay in git's sense**: upstream, the global git ignore file, the repo. So does the machine file's `path` key.
- **`nerve help` repeats what the README says of `--project`.** The usage is the CLI's own text, not a doc.

## Tests

- **The open-bug machinery has no users**: the `[bugs]` script condition, `NERVE_OPEN_BUGS`, `openBug`. CLAUDE.md's test rule relies on it.
- **The exclude lock test waits 200 ms.** Showing that a second run cannot get in needs a wait, and a working lock never fails it.
- **Some fakes never fail or follow links**, such as `Load`, `State` and `Locate`. No bug hides behind them; the adapters' own tests cover those paths.
