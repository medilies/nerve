# Review of `feat/projects` (cd76636)

Scope: the whole branch: `internal/brain/{domain,app,infra}`, `cmd/nerve`, the docs, SKILL.md and ADR 0005. This is the third review. The fixes of the second (521ca08) were re-checked, and what they left or added is below.

Baseline: `go test -race ./...`, `go vet ./...` and `golangci-lint run` all pass. None of the bugs below is caught by CI.

How each finding was checked:

- **Repro**: run by me with the built binary on a temp HOME and XDG dirs, `GIT_CONFIG_GLOBAL=/dev/null`, no network, or with a throwaway test I then deleted.
- **Code + reviewer**: confirmed by reading the code, and reproduced by a review agent.
- **Plausible**: not reproduced.

Leads that were refuted are left out. Some are listed under "Checked and fine".

## Bugs

### High

#### 1. A broken or missing machine file makes sync pull the project's own repo (Repro)

- Where: `onMachine` ignores `machine.Load`'s error and wires a Brain at `""` ([main.go:191](cmd/nerve/main.go#L191)). `Sync` pulls before it loads the machine, and git runs in the working directory.
- Repro: give the project an upstream that is one commit ahead. Set `brain = "relative/path"` in `config.toml`, then run `nerve sync --here --hook` in the project.
  - It prints `nerve: pulled   the Brain to 3c0bbe2`, then the machine-file error.
  - The project went from 1 to 2 commits.
- With no machine file at all, `sync` reports a pull of `""` (`no pull  : …`).
- Effect: autosync runs in every session, so any bad or deleted machine file fast-forwards the user's project. ADR 0005: nerve writes only in listed projects.
- Fix: refuse a Brain at `""` in `wire`, or load the machine before the pull.

#### 2. `project rm` with a failed Brain save deletes the overlay and unlists the checkout (Repro)

- Where: [project.go:161](internal/brain/app/project.go#L161) drops the overlay, and only then saves the Brain at [project.go:167](internal/brain/app/project.go#L167).
- Repro: `setting set env.TOKEN sk-…` (goes to the overlay), make the Brain read-only, then `project rm app`.
  - It prints `cleaned …`, then `removed …/overlays/app.json`, then `error: open …/brain/.brain.toml.…: permission denied`.
  - `project.app` is still in brain.toml. The overlay is gone and the checkout is unlisted.
- Effect: the overlay's secrets exist nowhere else. A retry cannot bring them back, and the report claims a removal that did not happen.
- Fix: save the Brain first, then the machine file, then delete the overlay. A re-run already handles a project that is gone from the Brain but still has an overlay.

#### 3. A skill that is a whole repo carries `.git` into projects and into the `skill update` diff (Repro)

- Where: `fetch` runs `git init` in the cache folder ([sources.go:98](internal/brain/infra/sources.go#L98)). When the root `SKILL.md` names the skill, that folder is the skill. `Diff` compares the two cache roots ([sources.go:66](internal/brain/infra/sources.go#L66)).
- Repro: a source repo whose root holds the skill.
  - The project's link lists `. .. .git SKILL.md`.
  - `skill update` between two commits printed 26 lines naming `.git/` (FETCH_HEAD, index, objects).
- Effect: `skill update` is the review gate for third-party code, and here the real change is buried under git internals. `.git` also reaches the project, and the claude.ai zip skips it only because `Zip` drops `.git` folders.
- Fix: remove `.git` from the fetched tree before the rename.

### Medium

#### 4. Every skill link deleted while one agent link remains writes back every skill (Repro)

- Where: the mass-loss guard counts all links of both folders together ([sync.go:368-369](internal/brain/app/sync.go#L368-L369)).
- Repro: a project with skills a and b and agent rev. Delete `.claude/skills/{a,b,nerve}`, as `rm .claude/skills/*` does, then sync. It prints `dropped skill a …` and `dropped skill b …`, and the `skills` line is gone from brain.toml.
- Effect: this is the loss the guard exists to stop, as in the second review's bug 1. The agent link alone defeats it.
- Fix: apply the rule per folder: if every link nerve made in `.claude/skills/` (or `agents/`) is gone, relink. `TestDeletingALinkDropsItFromTheProject` deletes nerve's own link along with go-stack, so it must change with the rule.

#### 5. A moved checkout leaves nerve's link behind, visible to git, and links a second one (Repro)

- Repro, with the Brain's remote at `git@github.com:me/brain.git`:
  1. Sync the project, `mv proj proj2`, and run a full sync. It prints `unlisted …/proj`.
  2. `project add app` in proj2, then `sync --here`. It prints `linked …/proj2/.claude/skills/me-nerve`.
  3. `git status` shows `?? .claude/`.
- Effect: the old `nerve` link is no longer in any state or exclude block. The team's repo can then commit it.
- Fix:
  - On `project add` or sync, adopt a link in nerve's places whose target is the Brain or nerve's cache.
  - At least, document "move = `project rm`, then `project add`" in sync.md.

#### 6. A worktree git cannot locate is never cleaned, and fails every sync (Repro)

- Where: [sync.go:496](internal/brain/app/sync.go#L496) returns the error while the repo folder exists.
- Repro: a worktree synced with `--here`, its `.git` file replaced with `gitdir: /nowhere`, then an emptied brain.toml.
  - Every full sync prints `problem …/wt: not in a git repo: fatal: not a git repository: /nowhere`.
  - Then `error: 1 of 1 working trees did not sync`.
- The same happens after `git worktree prune`, or with a "dubious ownership" error.
- Fix: report what was left in the exclude block, then go on to the settings, folders and state, so the tree is cleaned.

#### 7. Autosync can outlast Claude Code's hook timeout (Code + reviewer)

- Where:
  - A third-party skill not yet cached is fetched with the 2-minute `gitTimeout` ([files.go:183](internal/brain/infra/files.go#L183)). Only the pull's fetch uses `pullTimeout` (30 s).
  - The release download, in `Upgrade` at the start of every sync, has a 5-minute client timeout ([self.go:40](internal/brain/infra/self.go#L40)).
- Effect:
  - The SessionStart hook nerve writes sets no `timeout`, so Claude Code's default applies. The run is killed, and the report and the state save are lost.
  - A killed fetch also leaves `<sha>.tmp-*` in the cache.
- Not timed out by hand. Plausible as to the exact limit.
- Fix: bound every network step of a hook run below the hook timeout, or write an explicit `timeout` on the hook entry.

#### 8. Nerve's own paths are reported as `(hidden)` (Repro)

- Where: `ShowValue` runs the home-path rule on values that `Want` already filled ([settings.go:343](internal/brain/domain/settings.go#L343)).
- Repro: the binary at `…/home/u/nerve`. `sync --here` prints `set      hooks.SessionStart = (hidden)`.
- Effect:
  - On any Linux or macOS machine, the autosync hook and every `{{brain}}` hook show as `(hidden)` in each report.
  - SKILL.md tells Claude that `(hidden)` is sensitive and never to ask for it.
- Fix: show values from the unfilled layers, or leave out the home-path rule when showing (keep tokens and emails).

#### 9. The home-path rule refuses ordinary project paths (Repro)

- Where: `homePattern` is unanchored ([sensitive.go:15](internal/brain/domain/sensitive.go#L15)).
- Repro:
  - `setting add permissions.allow "Edit(app/home/page.tsx)"` prints `overlay  permissions.allow set in …: holds a home path`, and brain.toml does not get it.
  - `Read(./src/home/**)` is refused too.
- Effect: a web app's `home` route can't be allowed in the Brain, and `check` refuses such a rule written by hand.
- Fix: match `/home/` and `/Users/` only at the start of a path (start of string, or after a space, quote, `(`, `=` or `:`).

#### 10. One stray file in `state/` stops every sync (Repro)

- Where: `States` refuses any `state/*.json` that is not a state ([machine_files.go:247](internal/brain/infra/machine_files.go#L247)).
- Repro: `echo '{}' > state/stray.json`, then sync. It prints `problem …/proj: …/state/stray.json is not a state nerve can read: delete it, then sync the tree it was for (<nil>)`.
- Effect: no project syncs until the file is deleted. The message blames the tree, and shows `(<nil>)`.
- Fix: read only files named like a state (16 hex digits). Name a corrupt one without blaming the tree.

#### 11. An empty settings file fails its tree at every session start (Repro)

- Where: `readObject` refuses 0 bytes ([files.go:91](internal/brain/infra/files.go#L91)), while it reads `null` as `{}`.
- Repro: `: > .claude/settings.local.json`, then sync. It prints `problem …: …/settings.local.json: unexpected end of JSON input`.
- A BOM before `{}` fails the same way.
- Fix: read blank content, and a leading BOM, as `{}`.

#### 12. An overlay is written world-readable under umask 077 (Repro)

- Where: `writeFile` gives a new file mode 0644, whatever the umask ([files.go:33](internal/brain/infra/files.go#L33)).
- Repro: `(umask 077; nerve setting set env.T x)` leaves `overlays/app.json` at mode 644.
- Effect: overlays hold the secrets nerve keeps out of the Brain. The old `os.WriteFile` honoured the umask.
- Fix: create new files with 0644 minus the umask, or 0600 for overlays and the machine's files.

#### 13. A link the user points elsewhere is shown as the team's, and nerve links a second copy (Repro)

- Repro: the user points `.claude/skills/a` at another skill, then syncs.
  - It prints `linked …/.claude/skills/me-a -> …/brain/skills/a`.
  - `skill ls` then shows `skill a own, linked as .claude/skills/me-a` and `skill a the team's`.
- sync.md:87 says "A link you point elsewhere becomes yours: nerve leaves it alone". The project now holds two skills named after `a`, and the user's own link is labelled the team's.
- With no owner in the remote, the tree is refused at every sync.
- Fix, either:
  - Do not link a name whose link the user repointed, and say so in `skill ls`.
  - Or document the second link.

#### 14. `skill update` fails when the sha appears twice in brain.toml (Repro)

- Where: `MovePin` replaces the sha only when it appears exactly once ([brain_checkout.go:282](internal/brain/infra/brain_checkout.go#L282)).
- Repro: a comment `# was <sha>` in brain.toml. `skill update` prints `error: brain.toml: pin … must appear exactly once`.
- The same happens with two sources at one commit. `UpdatePin` also moves only the first source of a repo listed twice.
- Fix: replace the `sha` line inside the `[[source]]` table of that repo.

#### 15. `status --project` shows only the last checkout (Repro)

- Where: the loop keeps overwriting `tree` ([status.go:37](internal/brain/app/status.go#L37)).
- Repro: two checkouts of app. `status --project app` prints only `checkout …/p2 (app): never synced`.
- Fix: report each checkout.

### Low

- **Reported before saved** (Repro and Code + reviewer):
  - With a read-only Brain, `skill rm a` prints `dropped  skill a from project app`, then the save error ([items.go:78](internal/brain/app/items.go#L78)).
  - The same pattern appears in:
    - `AddProject`'s `imported` events;
    - reconcile's `set` events before `WriteSettings`;
    - `unlisted` before the machine file is saved.
  - `RemoveSetting` already reports only after it saves. Apply that rule everywhere.
- **`project rm` takes a path** (Repro). `project rm ../../victim` removes `~/.config/victim.json`, because `overlayPath` ([machine_files.go:147](internal/brain/infra/machine_files.go#L147)) joins the name unchecked. Check the name with `NamePattern`.
- **`--json` with a bad flag prints text** (Repro). `nerve --json sync --bogus` prints usage text and exits 1, because the mode is chosen only after a successful parse ([main.go:145](cmd/nerve/main.go#L145)). The usage and README promise one JSON document "with any command".
- **A stray brain.toml above an unlisted repo is taken as the Brain** (Repro). `check` in `~/x`, below `~/brain.toml` (`nerve = "v9.9.9"`), prints `no problems`. `skill update` would edit that file. See `onBrainHere` ([main.go:198](cmd/nerve/main.go#L198)).
- **The hook command is the binary's resolved path** (Repro).
  - With `bin/nerve -> v1/nerve`, the hook runs `…/v1/nerve sync --here --hook`. `Command` reuses `Release.Path` ([self.go:46](internal/brain/infra/self.go#L46)).
  - Removing a versioned install (Homebrew's Cellar, for example) breaks autosync until a manual sync.
  - The reviewer's "stale second hook" did not reproduce: the hook was replaced cleanly.
- **Whole numbers above int64 lose digits** (Repro). `ParseValue("12345678901234567890")` shows as `12345678901234567000`, and any sync that writes a file rounds it the same way. The comment on `Numbers` ([settings.go:645](internal/brain/domain/settings.go#L645)) says "keeps every digit".
- **Sensitive values it misses** (Repro). `/root/.ssh/id_rsa`, a JWT and `-----BEGIN PRIVATE KEY-----` all pass. Reviewer: `postgres://u:pw@host` and Google `AIza` keys pass too, and so does `env` nested under `mcpServers` when its value is not token-shaped.
- **`setting set` under a scalar replaces it silently** (Repro). After `k.a = 2`, `setting set k.a.b 2` prints `set k.a.b = 2`, and brain.toml now holds `[project.app.settings.k.a] b = 2`. `SetPath` ([settings.go:691](internal/brain/domain/settings.go#L691)) does not refuse, while `AddPath` does.
- **Frontmatter** (Repro):
  - A UTF-8 BOM gives `SKILL.md has no frontmatter` ([skill.go:61](internal/brain/domain/skill.go#L61)).
  - A quoted key (`"name": x`) is read as `"name"` with its quotes, so `name` is empty and the skill reports a name mismatch.
- **`Describe` counts `{}` as a setting** (Repro). `x: {}` gives `autosync on, 1 setting` ([manifest.go:172](internal/brain/domain/manifest.go#L172)), while sync.md says an empty object holds no setting.
- **Zips** (Code + reviewer):
  - A skill whose folder is itself a link is refused with `. links to a folder`.
  - Executable bits are lost, since every entry is stored as `-rw-rw-rw-`.
- **A project turned into a claude.ai account** is cleaned with the reason `project app left the Brain` (Code + reviewer).
- **A symlinked HOME** makes new paths in the machine file absolute rather than `~/…` (Code + reviewer).
- **`.claude/agents`** lists any file there, `README.txt` included, as the team's agent (Code).
- **`setting set permissions.allow '["a","a"]'`** stores the repeat in brain.toml (Code + reviewer).
- **Two session starts at once in worktrees of one repo** can drop each other's lines from the shared exclude block, since there is no lock (Plausible).
- **A skill name found in two folders of a source** (`skills/g` and `examples/g`) is "ambiguous", and the user has no way to pick (Plausible).

## Specs not met

- **ADR 0005, "writes only in listed projects"**: bug 1.
- **ADR 0005 and brain.md, "nothing sensitive in the Brain" and overlays that "never leave the machine"**:
  - Bug 2 loses the overlay.
  - Bug 12 leaves it readable by others.
  - brain.md says nerve refuses "home paths", but it misses `/root` and refuses `app/home/` (bug 9).
- **sync.md:87, "A link you point elsewhere becomes yours: nerve leaves it alone"**: bug 13.
- **sync.md, "An empty object … holds no setting"**: `Describe` counts it.
- **README.md:15, "Nothing reaches every project"**: common settings reach every listed project.
- **main.go:54 and README, "--json, with any command, prints one JSON document"**: false for a bad flag.
- **SKILL.md:66**:
  - `(hidden)` is said to mean sensitive, but nerve's own paths are hidden (bug 8).
  - "only on this machine (the overlay)" names no command: `setting set` puts only sensitive values there.
  - `--discard` drops every drifted key, not the one asked about. The skill should first move the kept ones into the Brain.
  - No rule for `upgraded`, `cleaned` or `pinned`.
- **brain.md, `check` "in the Brain or anywhere when this machine names one"**: an unrelated brain.toml above the working directory wins.

## Docs

- **"How" in user docs:**
  - sync.md explains the `info/exclude` block. Users need it only to fix a damaged block.
  - brain.md describes `state/`.
- **`nerve-checksums` is split.** The `curl … | sha256sum` recipe sits in sync.md's notes, but brain.md defines the key.
- **Glossary:**
  - "the Brain's checkout" (usage for `init`, brain.md) uses Checkout, which GLOSSARY defines as a project's working tree.
  - "machine file" appears in CLI errors but has no glossary entry.
- **Missing user facts:**
  - Moving a checkout (see bug 5).
  - A write-back from a worktree takes the item out of every checkout of the project.

## Tests

Essential behaviour with no test:

- Sync, or any command, with a broken or missing machine file run inside a project repo (bug 1).
- `project rm` with a failing Brain save, and the overlay kept (bug 2).
- `Diff` and `fetch` of a skill that is the whole repo (bug 3).
- Mass loss with both skills and agents (bug 4).
- A moved checkout (bug 5), and a worktree git cannot locate (bug 6).
- `ShowValue` on a filled `{{brain}}` or autosync path (bug 8).
- Sensitive false positives such as `app/home/`, and misses such as `/root` or a PEM block (bug 9). `TestSensitiveLeavesOrdinaryValuesAlone` has 5 trivial values.
- A `{}` or oddly named file in `state/` (bug 10).
- readObject on an empty, blank or BOM file (bug 11).
- File modes under a strict umask (bug 12).
- `status --project` with two checkouts (bug 15).
- Any save failing in `RemoveItem`, `AddProject` or `UpdatePin`, and `WriteSettings` failing.
- `--json` with a bad flag; `init` errors.

Weak tests:

- `TestEveryUsageLineRuns` ([nerve_test.go:230](cmd/nerve/nerve_test.go#L230)) only checks that a row's first word is in the usage, so a usage line can vanish without failing.
- `TestGitNeverRunsAnAskpassHelper` ([files_test.go:136](internal/brain/infra/files_test.go#L136)) sets only the env vars nerve strips. Removing `-c core.askPass=` would not fail it. The reviewer showed that a `core.askPass` in config is what the flag stops.
- `TestDeletingALinkDropsItFromTheProject` ([sync_test.go:154](internal/brain/app/sync_test.go#L154)) deletes nerve's own link too, so it doesn't isolate one deleted link.
- `TestSyncLeavesALinkSomeonePointedElsewhere` ([sync_test.go:589](internal/brain/app/sync_test.go#L589)) asserts the second link that contradicts sync.md.
- `TestStatesRefuseAStateFileTheyCannotRead` ([machine_files_test.go:56](internal/brain/infra/machine_files_test.go#L56)) pins the over-strict behaviour of bug 10.
- `TestReconcileProperties` ([settings_test.go:212](internal/brain/domain/settings_test.go#L212)) uses flat keys only. A reviewer's nested fuzz of about 4.9k cases found nothing new, but the test should cover nesting.

Fakes in [fakes_test.go](internal/brain/app/fakes_test.go) that hide bugs:

- `fakeTrees` has no error injection: `Link`, `Remove`, `WriteSettings` and `ReadSettings` never fail. Its `Locate` cannot fail for a listed tree (bug 6).
- `fakeMachine.saveErr` is shared by the machine file, `SaveOverlay` and `DropOverlay`, so a test cannot fail one alone.
- `fakeSelf.Upgrade` and `fakeBrain.Pull` do nothing, so the pull-before-load order of bug 1 can't show.
- `fakeZips.Has` checks a map, while `ZipCache.Has` checks the disk.

## Checked and fine

- The second review's fixes hold:
  - git clean relinks.
  - A failed write-back save is retried.
  - An unreachable remote is skipped.
  - Zips refuse links out of the skill.
  - Identifying keys and plugin IDs go to the overlay or are refused.
  - `(hidden)` is used in `setting rm`.
  - HTML escapes and digits within int64 are kept.
  - A damaged exclude block refuses before any link.
  - Gone checkouts are unlisted.
  - Worktrees are reached by a full sync.
  - `check` in a listed checkout uses the machine's Brain.
  - No askpass helper runs, a `core.askPass` in config included.
- The TOML round trip holds with quoted keys (`"x@y.z"`, `""`, `q"q`, `a b`), arrays of tables (hooks), empty tables and arrays, strings that look like headers, and whole floats of 1e19 and above.
- `ParseKey`, `Dotted` and `CutItem` round-trip 256 keys holding `. " \ = space #`.
- Reconcile, on a nested fuzz of about 4.9k file, last-write and Brain triples: no Strip leftover that is not drift, and no change on a second run beyond a `{}` nested in a whole key.
- A user list with repeats is collapsed once, then stays as is.
- A state is found under one hash whether its checkout is reached through a link or its real path.
- `ownerOf` on scp, ssh with port, https with credentials, local paths and odd names.
- Autosync prints one hook document and exits 0 for:
  - an unlisted folder;
  - no machine file;
  - a broken brain.toml;
  - a missing Brain;
  - `--hook --json`;
  - `--hook --project`.
- Every README usage example runs as documented. Flags before the command work. `init` twice and `init` on a folder with no brain.toml fail clearly.
- The claude.ai zip flow: upload, unchanged, delete, and a skill that can't be built keeping its old zip.
- Upgrade:
  - development builds are left alone;
  - a re-run that reports another version is refused;
  - checksums are checked before the rename;
  - a read-only folder gets an actionable error;
  - the temp file is removed on failure.
- `NERVE_*` variables reach only the re-run nerve and its git, never a later session.
- The SKILL.md event kinds all exist, and `rm --all`, overlay removal and `project rm` behave as it says.
- A malformed `NERVE_EARLIER_EVENTS` is ignored.

## Fix order

Fix by pattern, not one bug at a time: one ordering rule (load, save, then act, then report), one leniency rule (lenient on files people edit, strict on files nerve writes), and fakes that can fail. Each pattern's tests are already written, and skipped until fixed (`NERVE_OPEN_BUGS=1` runs them).

1. **Damage outside nerve's own files:**
   - Refuse a Brain at `""`, and load the machine before the pull (1).
   - Save the Brain before deleting the overlay in `project rm` (2).
   - Drop `.git` from fetched sources (3).
   - Create overlays with the umask honoured (12).
   - Check the `project rm` name (Low).
2. **Write-back and links:**
   - Apply the mass-loss rule per folder (4).
   - Adopt or document a moved checkout's links (5).
   - Decide how a repointed link is treated, and fix sync.md or the code (13).
3. **Syncs that keep failing:**
   - Clean a worktree git cannot locate (6).
   - Ignore stray files in `state/` (10).
   - Read an empty settings file as `{}` (11).
   - Bound every network step of a hook run (7).
4. **Reports and the sensitive check:**
   - Show nerve's own paths (8).
   - Anchor the home-path rule, and add `/root`, JWT and PEM (9 and Low).
   - Report only after saving (Low).
   - `status --project` (15) and `MovePin` (14).
   - `--json` on a bad flag (Low).
5. **The rest of Low, then the specs, docs and SKILL.md gaps.** Then the missing tests, weak tests and fakes above. Write each test first, so it fails before its fix.
