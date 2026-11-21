# Review of `feat/projects` at a47492f

Review seven: the whole branch, with the fixes made since 0ffecaa. main is a full rewrite away, so it was ignored.

## How it ran

- **Baseline, all green:**
  - `go test -race ./...` and `NERVE_OPEN_BUGS=1 go test ./...` pass.
  - `golangci-lint run` reports 0 issues, and `go vet` is clean.
- **Explore:** five Haiku explorers covered domain, app, infra (split in two), and cmd plus docs. They gave leads only.
- **Review:** five Sonnet reviewers, each in its own worktree on a47492f (checked).
  - They confirmed or refuted every lead and hunted freely.
  - They used the built binary on a temp HOME, with `GIT_CONFIG_GLOBAL=/dev/null` and no network.
- **Verify:** each finding below has one of these labels.
  - **Repro:** I reproduced it myself on a temp HOME.
  - **Code:** I confirmed it by reading the code.
  - **Code + reviewer:** I read the code, and the reviewer has a repro.
  - **Plausible:** reasoned, not reproduced.
- **Dropped:** anything refuted or unverified.
- **Fact-checked against the Claude Code docs:**
  - `systemMessage` is shown to the user.
  - `hookSpecificOutput.reloadSkills` exists for SessionStart.
  - `syncClaudeAiSkills` is honoured from local settings.
  - Every allowed settings key exists.
  - The hook format is right.
  - Symlinked skill folders are read.

## Decisions taken after the review

- **Keeping sensitive data out of the Brain is not nerve's job.** The feature goes, and so does its bug report (bug 1). This contradicts CLAUDE.md "Nothing sensitive in the Brain" and ADR 0005 "Keeping sensitive values out": a new ADR supersedes that section. The scope of the drop is still open.
- **nerve never deletes `settings.local.json`.** This settles bug 5.

The numbers below are kept from the first version, so bug 1 is missing.

## Outcome

The fixes went with a simplification, [ADR 0009](docs/adr/0009-explicit-verbs.md): nerve acts only when told. `sync` makes the Brain its remote's, `push` commits and force-pushes it, `update` moves pins, and the session-start hook (`status --hook`) only reports.

- **Fixed:**
  - 2: the pull is a fetch and a hard reset, so collisions take the remote's.
  - 3 and 4: a state nerve cannot read is reported, then dropped, and `project rm` cleans that checkout.
  - 5: `settings.local.json` is never deleted.
  - 6: all three frontmatter slips.
  - 7: hook mode keys on the command word, `--hook` goes with `status` only, the `--` conflict is checked, a report with nothing to say prints `{}`, and `--project --help` shows the help.
  - 10: all four messages; the upstream case by construction.
  - 11, `Merge` of nested lists, `PersonalKeys` names, and `fsync` before rename.
- **Gone with their features:** 1 (secret scanning), 8 (the pin line editor), 9 (the version), and the SKILL.md `version` gap.
- **Added to not-bugs.md:** the cache write through a link, `linkEnd`, and `adopt` on an unreadable `SKILL.md`.
- **Docs:** brain.md:105-107 rewritten; GLOSSARY.md has state, exclude block, sync, push, update and session report.
- **Tests added:**
  - a second sync writes nothing;
  - the generated hook command runs from a subfolder;
  - pull, push and update;
  - an unreadable state, and `{}` kept;
  - the error checks above.
- **Still open:**
  - a CLI test of `status --json` fields;
  - the weak scripts listed under Weak tests;
  - a randomized Reconcile property test.

## Bugs, by severity

### Medium

**2. A Brain pull fails, instead of skipping, when an untracked path and an upstream addition differ as file and folder.** (Repro)

- Repro: the upstream adds `x/file`, and the Brain has an untracked file `x`.
  - `nerve sync --here` prints `error: pull …: error: The following untracked working tree files would be overwritten by merge:` and exits 1.
  - The message stops at the colon, without the file's name.
  - Autosync gets the same text and exits 0.
- The reverse case, an untracked folder where a file arrives, fails the same way (reviewer's Go test).
- Effect: every sync, autosync included, stops before linking or writing anything for every project, until the user cleans the Brain by hand.
- Where: `inTheWay` in [brain_checkout.go:421-437](internal/brain/infra/brain_checkout.go#L421-L437) compares exact paths only.
- Breaks: sync.md, "untracked files stand where it would bring files" means skip and say why.
- Fix: also match an added path under an untracked folder, and an untracked file that is a parent of an added path. Or treat a failed `merge --ff-only` as a skip, with the whole message.

### Medium-low

**3. `project rm` skips a worktree whose state is unreadable, and says nothing.** (Repro)

- Repro:
  1. Sync the main checkout and a worktree.
  2. Write `{bad` into the worktree's state.
  3. Run `nerve project rm demo`.
- What happens:
  - It exits 0 and reports nothing about the worktree.
  - The worktree keeps its links to `mine` and `nerve`.
  - The shared `info/exclude` block is rebuilt without the worktree, so `git status` shows `?? .claude/` there.
  - The next `nerve sync` fails with "1 states cannot be read".
  - Once the state is deleted, `sync --here` in the worktree says it is not listed. Nothing ever cleans it.
- Where: [project.go:108](internal/brain/app/project.go#L108) drops the `unreadable` list that `Sync` reports at [sync.go:90-92](internal/brain/app/sync.go#L90-L92).
- Breaks: "every effect is visible and reported", and ADR 0005's clean-out of a dropped project.
- Fix: refuse `project rm` while a state is unreadable, as `sync` does.

**4. Following "delete it, then sync again" loses what nerve knew.** (Repro)

- Repro:
  1. The checkout had `{"model":"sonnet","foo":1}` before nerve.
  2. Sync with `--discard`.
  3. Corrupt the state, then `skill rm own`.
  4. Delete the state as told, then `sync --here`, then `project rm demo`.
- What is lost:
  - The `own` link stays forever, outside the exclude block, so git shows `?? .claude/`.
  - `model` is gone, where it should go back to `"sonnet"`.
  - The empty folders `.claude/` and `.claude/skills/` stay.
- not-bugs.md excuses the folders and the adoption only after a move. A deleted state is a different cause.
- Where: the message at [machine_files.go:220](internal/brain/infra/machine_files.go#L220) and at [sync.go:91](internal/brain/app/sync.go#L91).
- Breaks: "removes only what it made".
- Fix: pick one.
  - The message names the cost ("nerve then forgets what it made there: run `project rm` first if you mean to remove it").
  - Or `project rm` and `sync` adopt links by their target (the Brain or nerve's cache), as `adopt` already does for links the project lists.

### Low

**5. `project rm` deletes a `settings.local.json` that was `{}` before nerve.** (Repro)

- Where: [sync.go:549-555](internal/brain/app/sync.go#L549-L555) removes the file whenever nothing is left. The state does not record whether nerve made the file.
- Fix: record in `TreeState` that nerve made the file, and write `{}` back otherwise.

**6. A quoted frontmatter value starting with `>` or `|` is dropped.** (Repro)

- Repro: `description: "> quoted"` makes `nerve check` say `skills/q: no description`.
- Where: [skill.go:77-80](internal/brain/domain/skill.go#L77-L80) tests for a block marker after unquoting.
- Two more frontmatter slips (Code + reviewer):
  - `a<TAB># c` keeps the comment ([skill.go:143](internal/brain/domain/skill.go#L143)).
  - `"a\\/b"` comes out raw ([skill.go:155](internal/brain/domain/skill.go#L155)).

**7. Hook mode depends on word order, and has edge cases.**

- `nerve --hook sync --here` is not hook mode (Repro).
  - It prints text, has no deadline, and exits 1 on an error.
  - Where: [main.go:167](cmd/nerve/main.go#L167) needs `args[0] == "sync"`.
  - The generated hook uses the right order, so autosync itself is safe.
- Code + reviewer:
  - `sync --here --hook --json --` passes, because the conflict check is skipped after `--` ([main.go:90-91](cmd/nerve/main.go#L90-L91)).
  - `sync --hook --help` puts the whole usage in `systemMessage`.
  - A full `sync --hook` with no checkouts prints `{"systemMessage":"nerve: "}` ([report.go:93](internal/brain/infra/report.go#L93)).
  - `nerve --project --help` errors instead of showing help.
- Fix: hook mode is "`sync` is the first word and `--hook` is among the flags". Refuse `--hook` elsewhere, and give an empty report no message.

**8. `skill update` cannot find a pin in some valid `brain.toml` files.** (Code + reviewer)

- It fails with "no [[source]] … is pinned" in two cases:
  - The file starts with a UTF-8 BOM and `[[source]]` is on line 1. `sourceHeader` at [brain_checkout.go:301](internal/brain/infra/brain_checkout.go#L301) does not match the BOM.
  - The source is written as an inline array, `source = [{…}]`.
- `Load` accepts both.
- Fix: strip the BOM before matching. Say that only `[[source]]` tables can be moved.

**9. `nerve status` prints an empty version for a build by file path.** (Repro)

- Repro: `go build ./cmd/nerve/main.go`, then `nerve status`, prints a `nerve` line with no version after it.
- Where: [self.go:50-51](internal/brain/infra/self.go#L50-L51) returns an empty `Main.Version`, where it should fall back to `(devel)`.

**10. Messages that mislead.**

- `status` on a Brain with no commits and a remote shows a raw `fatal: ambiguous argument 'HEAD'` (Code + reviewer, [brain_checkout.go:476-480](internal/brain/infra/brain_checkout.go#L476-L480)).
- Any `@{upstream}` failure after a fetch is reported as "its upstream branch is not on the remote", even when the cause is a broken branch config (Code + reviewer, [brain_checkout.go:391-393](internal/brain/infra/brain_checkout.go#L391-L393)).
- `skill update`'s error for the old pin lacks the `repo@sha` and the way forward that the new pin's error has (Code + reviewer, [sources.go:56-58](internal/brain/infra/sources.go#L56-L58)).
- A team folder named `nerve` in `.claude/skills` gets "rename yours in the Brain to use it". The user cannot rename nerve's own skill (Repro).

**11. `Validate` returns a different error from run to run** when several names are bad. (Code + reviewer)

- Where: [manifest.go:153](internal/brain/domain/manifest.go#L153) and [:161](internal/brain/domain/manifest.go#L161) range over maps.
- Fix: use `SortedKeys`.

### Hardening (take each, or add it to not-bugs.md)

All are Code + reviewer unless marked.

- `Nerve.Skill` writes through a link planted at the cache's `SKILL.md`, because `writeFile` follows links. The cache does not need that; the Brain does.
- `linkEnd` cleans a relative target such as `sub/../x` without following links ([files.go:113-118](internal/brain/infra/files.go#L113-L118)).
- There is no `fsync` before the rename ([files.go:80-87](internal/brain/infra/files.go#L80-L87)).
- `PersonalKeys` names can collide, as with `{"a":[1],"a 1":true}`.
- Lists nested under a key the lower layer lacks are not de-duplicated in `Merge`. Harmless, since `Reconcile` de-duplicates them, but the comment at settings.go:67 over-promises.
- `adopt` skips an own skill whose `SKILL.md` cannot be read, so a moved checkout keeps that link after `project rm` (Plausible, [project.go:181](internal/brain/app/project.go#L181)). `fakeBrain.OwnSkill` returns a `Dir` on error where the real one does not.

## Spec violations

The bugs above break these rules:

| Rule | Where it is written | Broken by |
| --- | --- | --- |
| A pull skips, and says why, when untracked files are in the way | sync.md | 2 |
| Every effect is visible and reported | CLAUDE.md, ADR 0005 | 3 |
| Removes only what it made | CLAUDE.md, ADR 0005 | 4, 5 |

One more breaks CLAUDE.md's "a new concept gets an entry [in GLOSSARY.md] before it gets code" (Code):

- **state** and **exclude block** are user-facing words in brain.md and sync.md, and neither has an entry.

## Docs

- **SKILL.md:75 misses the common case** (Code).
  - It tells the agent how to handle `version` with a `target`: "never install it yourself".
  - But `sync` and autosync report a pin mismatch as a `problem` ("nerve vX runs, but the Brain pins vY: install vY", [sync.go:58-59](internal/brain/app/sync.go#L58-L59)). Only `status` emits `version`.
  - On the common path, the agent gets only "propose a fix".
  - Fix: name that `problem` in the bullet.
- **brain.md:105-107 is not true for every refusal** (Code).
  - It says "Each refusal of a setting says how to move it to a local overlay".
  - The refusals for `hooks` and `enabledPlugins` point to `plugin add` and `[hook.<name>]` instead ([manifest.go:297](internal/brain/domain/manifest.go#L297)).
  - Fix: say "says where it goes".
- **The fix for bug 4 needs a doc line.** brain.md "This machine" should say what deleting a state costs, or nothing, if the fix removes the cost.

## Tests

### Gaps on essential paths

- **No test of the pull collisions in bug 2.** The untracked-path test covers only a same-path file ([brain_checkout_test.go:259](internal/brain/infra/brain_checkout_test.go#L259)).
- **No test that a second sync writes nothing to the project.** The fakes count no writes, and no script checks mtimes. A regression that rewrote `settings.local.json` or `info/exclude` at every session would pass.
- **No test of bugs 3, 4 and 5:**
  - `RemoveProject` with an unreadable state;
  - deleting a state, then syncing;
  - a `{}` file before nerve.
- **The hook command nerve writes is never run.**
  - `autosync_hook_output` only greps it.
  - A script should take `command` from `settings.local.json` and run it from a subfolder. That covers quoting, `--here`, JSON on stdout and exit 0 together.
  - No script runs autosync on drift: a drift line and exit 0, while a plain `sync` fails.
- **Nothing checks that a sync after `autosync off` takes the hook out of an existing file.** The domain checks only that `Want` adds no hook ([settings_test.go:333](internal/brain/domain/settings_test.go#L333)).
- **No CLI test of `status --json`'s event fields**, though the nerve skill reads it first at every `/nerve`.
- **No `MovePin` tests** for a BOM, an inline array or CRLF.

### Weak tests

- These ignore the error of the call they test:
  - [settings_test.go:327](internal/brain/app/settings_test.go#L327) `_ = e.app.SetSetting(…)`;
  - [sync_test.go:797](internal/brain/app/sync_test.go#L797), and :619, :718 and :735.
- `TestReconcileProperties` ([settings_test.go:149-160](internal/brain/domain/settings_test.go#L149-L160)) draws from keys `a` to `d` only.
  - The domain reviewer ran 300k random trials over nested objects and lists of objects, and every property held.
  - A randomized version would be cheap to add.
- [self_test.go:124-127](internal/brain/infra/self_test.go#L124-L127) checks only that `Version("")` is not empty, so it missed bug 9.
- [files_test.go:141](internal/brain/infra/files_test.go#L141) uses a read-only folder, where the temp file is never made, so the cleanup path is never run.
- Scripts that assert too little:
  - `exit_code` checks only the first line of the usage.
  - `sync_refuses_unignored_settings` passes whenever `.claude` is missing.
  - `worktree` never checks the exclude block.
  - Most app tests match events by substring, so an extra or repeated event passes.

### Fakes

- No fake hides a confirmed bug.
- `fakeBrain.OwnSkill` returning a `Dir` on error is the only difference that touches a finding, the Plausible `adopt` one.

## Ignored (not-bugs.md)

The reviewers hit these and left them, as listed:

- an empty `.claude/skills` after a move;
- every setting as the Brain sets it is nerve's, in a moved checkout;
- a link removed, then made again;
- a Brain ahead of its remote says nothing;
- the exclude lock;
- skills looked for in fixed places;
- the source cache trusted by its folder;
- writes follow a link at `brain.toml`;
- the open-bug machinery with no users;
- fakes that never fail.

## Checked and fine

- **Main flow:**
  - Steps run: `init`, `project add`, `skill`/`agent`/`hook`/`plugin add`, `sync --here`.
  - A second sync writes nothing and prints one line.
  - Drift and personal settings are reported, and `setting set` resolves drift.
  - `skill rm` takes the link out.
  - `project rm` leaves the checkout as it was before nerve, `info/exclude` included.
- **Many working trees:** two checkouts, a worktree, a moved checkout, `--discard`, `rm --all` and `setting rm --all` all behave as documented. Autosync off and on touches only nerve's hook item, and leaves the user's own `SessionStart` hook.
- **Nothing written outside a listed checkout:** none found. A link the user points elsewhere is left alone.
- **Pull skips:**
  - Each case behaves as sync.md says, in text, `--json` and `--hook`:
    - uncommitted changes;
    - no upstream;
    - a detached HEAD;
    - an unreachable remote;
    - the branch gone from the remote;
    - a diverged Brain fails;
    - a remote asking for a password gets no prompt.
  - A Brain ahead of its upstream pulls nothing, and one behind pulls.
- **Sources:**
  - Skill lookup works at the root, `skills/<name>`, `<name>` and `path`, and a skill in two places is refused.
  - Links in a fetched repo arrive as plain files, and `path` with `..` is refused.
  - A sha not on the remote, or going offline, keeps the old link.
  - `skill update` shows the diff, moves the pin, then says `already-pinned`.
  - No `.tmp` folders are left.
- **The Reconcile/Strip/PersonalKeys properties** held over 300k random trials.
- **The Brain.Save round trip** held for 16 shapes.
- **Numbers:** int64 bounds are fine.
- **Hook mode:**
  - The generated command always prints JSON and exits 0.
  - Cases tried: no machine file, a broken one, a missing Brain, HOME unset, the working folder deleted, an unlisted checkout, a project gone from the Brain, stdout closed.
- **Docs agree with the binary:**
  - README usage, the SKILL.md command table and `nerve help` agree.
  - SKILL.md's event names and fields match the `--json` output.
  - The `config.toml` example matches what `init` writes.
  - The git 2.31 floor holds: `rev-parse --path-format=absolute` needs it.
- **ADRs:** no code contradicts an accepted ADR.
- **The Avoid words of GLOSSARY.md:** each use found is a different sense.

## Fix order

1. **Drop the sensitive-data feature**, with its ADR, once its scope is settled.
2. **Bug 2 (pull collisions).** Write a failing infra test for each direction, then widen `inTheWay`, or treat a failed fast-forward as a skip.
3. **Bugs 3 and 4 (unreadable or deleted state).**
   - `project rm` refuses while a state is unreadable.
   - Then choose: the message names the cost (and brain.md says so), or links are adopted by their target. Writing the test first will show which is simpler.
4. **Bug 5 (`{}` removed).** nerve never deletes `settings.local.json`; it writes what is left, `{}` included.
5. **SKILL.md's `version`/`problem` bullet, and brain.md:105-107.**
6. **Bug 7 (hook word order).** Then add the script that runs the generated hook command, which pins it.
7. **Low bugs 6, 8, 9, 10 and 11,** each with its test.
8. **Test gaps:** the second sync writes nothing; `autosync off` takes the hook out; arrays of tables through `Load`; `status --json` fields; the weak tests above.
9. **Hardening:** fix each item, or add it to not-bugs.md with its reason.
10. **GLOSSARY entries** for state and the exclude block.
