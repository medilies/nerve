# Review of `feat/projects`: open items

Scope: what is still open from the reviews of the branch, the fifth included ([review-feat-projects-0ffecaa.md](review-feat-projects-0ffecaa.md)), after the fixes made on top of 0ffecaa. Everything else they found is fixed, backlogged or went with ADR 0007.

How each item was checked:

- **Repro**: run with the built binary on a temp HOME and XDG dirs, with `GIT_CONFIG_GLOBAL=/dev/null` and no network.
- **Code**: confirmed by reading the code.
- **Plausible**: not reproduced.

## Bugs

### Low

- **A worktree of a bare repo is listed on its own** (Plausible).
  - `Locate` ([trees.go](internal/brain/infra/trees.go)) finds a main checkout only when the common git folder is named `.git`.
  - So `project add` in such a worktree lists that worktree, and `sync --here` in a second worktree says it is not listed.
  - Nothing is damaged: the exclude lines still go to the bare repo's `info/exclude`.
- **A few secrets still pass the sensitive patterns** (Code), such as `Authorization: Basic …` in a value, or `https://user@host` with no password.
  - A pattern for them would also flag ordinary text, such as "basic installation".

## Tests

Gaps:

- No `Locate` test for a bare repo's worktree, a separate git dir or a submodule.
- claude.ai has no end-to-end script. App and infra tests cover it.

## Kept on purpose

- **`check` and `skill update` need HOME** (Code). `env -i nerve check` fails on `$XDG_CONFIG_HOME`, as the machine and zip adapters work out their folders at startup. A Brain with a source needs the cache folder anyway, and CI runners set HOME.
- **`main.go` ignores the machine file's load error** in `onMachine` (Code). Latent only: every use case loads the machine file again and reports it.
- **Changes to the exclude block are not reported** (Code). sync.md says links are kept out of git by that block, and it follows them.
- The open-bug machinery (the `[bugs]` script condition, `NERVE_OPEN_BUGS`, `openBug`) has no users. CLAUDE.md's test rule relies on it, so it stays.
- brain.md still names `state/`: an unreadable state's message tells the user to delete a file there.
- sync.md still says links are kept out of git with a block in `info/exclude`: a damaged block is the user's to fix.

## Backlogged

- Sweeping the temp files of a killed run: the sweep deleted the user's files named like them, so it went. See [backlog.md](docs/backlog.md).
- Picking a source skill by folder, when two folders of a source have its name.

## Housekeeping

- The fixes on top of 0ffecaa have not been reviewed. CLAUDE.md asks for that in a fresh subagent.
