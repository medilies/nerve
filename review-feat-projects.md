# Review of `feat/projects`: open items

Scope: what is still open from the five reviews of the branch, after the fixes made on top of 0ffecaa (`git log 0ffecaa..`). Everything else they found is fixed, backlogged, or went with ADR 0007 or ADR 0008.

## Kept on purpose

- **A worktree of a bare repo is its own checkout.** A bare repo has no main checkout for its worktrees to belong to, so `project add` lists each worktree it runs in. Its exclude lines go to the bare repo's `info/exclude`. `TestLocateTakesAWorktreeOfABareRepoAsItsOwnCheckout` pins it.
- **A URL with a user and no password passes the sensitive patterns**, such as `https://user@host`. A pattern for it would flag `ssh://git@github.com` too. An email is refused as one.
- **`check` and `skill update` need HOME.** `env -i nerve check` fails on `$XDG_CONFIG_HOME`, as the machine adapter works out its folder at startup. A Brain with a source needs the cache folder anyway, and CI runners set HOME.
- **`main.go` ignores the machine file's load error** in `onMachine`. Every use case loads the machine file again and reports it.
- **Changes to the exclude block are not reported.** sync.md says links are kept out of git by that block, and it follows them.
- The open-bug machinery (the `[bugs]` script condition, `NERVE_OPEN_BUGS`, `openBug`) has no users. CLAUDE.md's test rule relies on it, so it stays.
- brain.md still names `state/`: an unreadable state's message tells the user to delete a file there.
- sync.md still says links are kept out of git with a block in `info/exclude`: a damaged block is the user's to fix.

## Dropped or backlogged

- Sweeping the temp files of a killed run: the sweep deleted the user's files named like them. See [backlog.md](docs/backlog.md).
- claude.ai accounts: [ADR 0008](docs/adr/0008-drop-claude-ai-accounts.md).
- Searching a whole source repo for a skill: nerve looks in fixed places, and `path` in `[[source]]` names another.

## Housekeeping

- The fixes on top of 0ffecaa have not been reviewed. CLAUDE.md asks for that in a fresh subagent.
