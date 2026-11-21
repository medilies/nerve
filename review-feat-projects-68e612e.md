# Review of `feat/projects` (68e612e)

Scope: the whole branch. This is the fourth review. Its starting point was the third review at cd76636 ([review-feat-projects.md](review-feat-projects.md)), and it covers the 16 commits since, including ADR 0007, which dropped write-back, owner-prefixed links and self-upgrade.

Baseline: `go test -race ./...`, `go vet ./...` and `golangci-lint run` (v2.14.0) all pass. None of the bugs below is caught by CI.

How each finding was checked:

- **Repro**: I ran it with the built binary on a temp HOME and XDG dirs, with `GIT_CONFIG_GLOBAL=/dev/null` and no network.
- **Code**: confirmed by reading the code.
- **Code + reviewer**: confirmed by reading the code, and reproduced by a review agent.
- **Plausible**: not reproduced.

Refuted leads are left out. A few of them are listed under "Checked and fine".

## Bugs

### High

#### 1. `project rm` destroys settings the user had before nerve, secrets included (Repro)

- Where:
  - `project add` takes the user's existing settings: sensitive ones go to the overlay, the others to the Brain ([project.go:45](internal/brain/app/project.go#L45)).
  - From then on, nerve treats every one of them as a setting it wrote.
  - `project rm` then cleans the file ([project.go:156](internal/brain/app/project.go#L156)) and deletes the overlay ([project.go:173](internal/brain/app/project.go#L173)).
- Repro: `.claude/settings.local.json` holds `{"env":{"API_TOKEN":"secret123"},"model":"opus"}` before nerve. Run `project add app`, `sync --here`, then `project rm app`.
  - It prints `removed …/settings.local.json` and `removed …/overlays/app.json`.
  - The token now exists nowhere.
- Effect: the user's own file, which nerve did not make, is gone. Its secret is lost for good, and the other values survive only in brain.toml's git history.
- Specs:
  - sync.md:92 cleans only "the settings and list items it wrote".
  - sync.md:97: "nerve never touches what it did not make".
  - ADR 0005: "removes only what it made".
- Fix: on `project rm`, write the overlay's values (and, ideally, what was imported) back into the file before deleting the overlay. At least, keep the overlay and say so.

#### 2. Secrets under nested keys reach the Brain, and reports print them (Repro)

- Where: `sensitive` checks `SecretKeys` against the first key only ([sensitive.go:32](internal/brain/domain/sensitive.go#L32)).
- Repro:
  - `setting set mcpServers.db.env.DB_PASSWORD hunter2hunter2` writes `[project.app.settings.mcpServers.db.env] DB_PASSWORD = "hunter2hunter2"` to brain.toml.
  - `project add` imports a pre-existing `mcpServers.db.headers.X-Api-Key = "abc123def456"` into brain.toml in the same way.
  - `check` then prints `no problems`.
  - The reviewer saw the same with `sandbox.env.TOKEN` (`--common`) and `pluginConfigs.x.options.apiKey`.
- Effect:
  - A Brain "may be public" (brain.md:76). Any `env` or `headers` below the top level is committed in clear.
  - Sync prints them too (`set mcpServers.db.env.DB_PASSWORD = "hunter2hunter2"`), while sync.md:36 says secrets are shown as `(hidden)`.
- Fix: match `SecretKeys`, plus `headers` and key names like `*token*`, `*secret*`, `*password*` and `apiKey`, at any depth of the leaf path.

### Medium

#### 3. A git failure during a full sync wipes every worktree, with a false reason (Repro)

- Where:
  - A known worktree that `here` cannot place gets project `""` ([sync.go:180](internal/brain/app/sync.go#L180)).
  - It is then cleaned as "no checkout this machine lists holds it" ([sync.go:97](internal/brain/app/sync.go#L97)).
  - The state's own `Project` is never consulted.
- Repro: a listed checkout `proj` and a synced worktree `wt`. Then `PATH=/nonexistent nerve sync`:
  - It prints `problem …/proj: not in a git repo: exec: "git": executable file not found`.
  - Then `removed …/wt/.claude/skills/nerve`, `removed …/wt/.claude/settings.local.json` and `cleaned …/wt: no checkout this machine lists holds it`.
  - `wt/.claude` is gone. The listed checkout is only failed, not cleaned, so the two are treated differently.
  - The same run says `no pull …: detached HEAD`, which is also false.
- Effect: any transient git failure clears every worktree, including "dubious ownership", a broken git upgrade, or a PATH without git in a hook. A `sync --here` in each worktree recovers.
- Fix: when Locate fails and `st.Project` is still in the Brain, report a problem and keep the state, as is done for a listed checkout.

#### 4. `project add` imports nerve's own autosync hook into brain.toml (Repro)

- Repro: sync a checkout, `mv` it, run a full sync (`unlisted …`), then `project add other` in the moved folder.
  - It prints `imported hooks.SessionStart: recorded in the project`.
  - brain.toml gets `command = "<this machine's path>/nerve sync --here --hook"`.
  - `project autosync off` then has no effect. Both the report and the file keep the hook.
- Effect:
  - A machine path enters the Brain, against CLAUDE.md "no … paths". When it is under `/home`, it goes to the overlay instead.
  - On another machine, the path may not exist, so the hook fails at every session start.
  - The same import also happens for any checkout synced before, once its state is gone.
- Fix: `ImportSettings` ([settings.go:584](internal/brain/domain/settings.go#L584)) skips the generated hook. Compare against `Generated`'s value.

#### 5. A symlinked `.claude/skills` makes nerve write outside the project, and git sees the links (Repro)

- Where: `Link` runs `MkdirAll` and `Symlink` through a linked parent ([trees.go:55-65](internal/brain/infra/trees.go#L55-L65)).
- Repro: `.claude/skills -> $HOME/outside`, then `sync --here`.
  - It prints `linked …/proj/.claude/skills/x`, with rc 0.
  - The links are in `$HOME/outside`.
  - `git status --porcelain` prints `?? .claude/`, because the exclude block's `/.claude/skills/x` does not match through a link.
- Effect:
  - ADR 0005: "writes only in listed projects".
  - The team can commit the links.
  - Pointed at `brain/skills`, nerve would write into the Brain.
- Fix: refuse the tree when `.claude/skills` or `.claude/agents` is a link. `.claude` itself is already stopped by `CheckIgnored`.

#### 6. A symlinked `settings.local.json`: nerve writes through it, and cleaning leaves its hook there (Repro)

- Where:
  - `writeFile` follows the link to its end ([files.go:75](internal/brain/infra/files.go#L75)).
  - The clean removes only the link.
- Repro: `.claude/settings.local.json -> ~/dot/settings.json`, then sync and `project rm app`.
  - It prints `removed …/settings.local.json`.
  - `dot/settings.json` still holds nerve's `SessionStart` hook.
- Effect:
  - If the link points at `~/.claude/settings.json`, nerve writes at user level and leaves its hook there for good. ADR 0005 says never at user level.
  - The report claims a removal that did not happen to the content.
- Fix, either:
  - refuse a linked settings file, as is done for a linked `.claude`;
  - or clean through `linkEnd`, and never remove the link.

#### 7. One unreadable state file stops every sync on the machine (Repro)

- Where:
  - `States` refuses any unreadable file that matches the name pattern ([machine_files.go:258](internal/brain/infra/machine_files.go#L258)).
  - Every sync calls it, for the exclude block ([sync.go:309](internal/brain/app/sync.go#L309)) and for the trees ([sync.go:166](internal/brain/app/sync.go#L166)).
  - So does `project rm` ([project.go:132](internal/brain/app/project.go#L132)).
- Repro: truncate proj's state to 0 bytes.
  - `sync --here` in proj prints `problem …/proj: …/state/4ea1….json: unexpected end of JSON input`. That message has no hint.
  - `sync --here` in an unrelated checkout fails too: `…is not a state nerve can read: delete it, then sync the tree it was for`.
  - Both end with `1 of 1 working trees did not sync`.
- Effect: every session start, in every project, shows the problem until the user finds and deletes the file. This is what is left of review 3's #10.
- Fix: skip an unreadable state with a problem that names its file. Only its own tree treats it as missing.

#### 8. A link the user repointed is reported as the team's, every sync (Repro)

- Where: [sync.go:388](internal/brain/app/sync.go#L388) and [items.go:141](internal/brain/app/items.go#L141) treat any link nerve did not make as "the team has it".
- Repro: `ln -sfn ~/elsewhere .claude/skills/nerve`, then sync twice.
  - Both runs print `problem …/skills/nerve: the team has it, so skill nerve is not linked here: rename yours in the Brain to use it`.
  - `skill ls` shows `skill nerve the team's`.
- Effect:
  - sync.md:86 says a repointed link "becomes yours: nerve leaves it alone". It says nothing of a problem at every session start.
  - The advice is wrong. Nobody can rename nerve's own skill, and the team has nothing.
- Fix: tell a link apart from a folder or file. Report a repointed link once as "yours", or not at all, and label it that way in `skill ls`.

#### 9. One unreachable third-party source blocks the whole project (Repro)

- Where: the first skill that fails to resolve returns before any link or settings write ([sync.go:237-241](internal/brain/app/sync.go#L237-L241)).
- Repro: a project lists own skill `own` and source skill `good` at a pin that can't be fetched. `sync --here --hook` prints `problem …: fetch acme/up@1111111: fatal: … does not appear to be a git repository`, and `.claude` is never created.
- Effect:
  - A first sync offline, or a deleted upstream, gives the project nothing: no own skills, no settings, and no autosync hook.
  - sync.md doesn't say this can happen.
- Fix: report that skill as a problem and go on with the rest.

#### 10. Whole numbers above int64 are rewritten, even keys only the user has (Code + reviewer, partly Repro)

- Where: `Numbers` falls back to float64 ([settings.go:667](internal/brain/domain/settings.go#L667)). Two comments say "keep every digit": [settings.go:667](internal/brain/domain/settings.go#L667) and [files.go:96](internal/brain/infra/files.go#L96).
- My repro:
  - `project add` on `"uid":18446744073709551615` records `uid = 1.8446744073709552e+19`.
  - After a sync, the file holds `"uid": 18446744073709552000`.
- The reviewer's repro: the same key, never in the Brain, is rewritten by a sync that only writes another key.
- Effect: silent corruption of a value nerve does not own.
- Fix: keep `json.Number` (or the literal) for what doesn't fit, or refuse it. `TestParseValueReadsAWholeNumberBeyondInt64AsAFloat` pins the loss today.

#### 11. The sensitive check misses common tokens and a home path in a plugin ID (Repro)

- `plugin add x@/home/bob/market` prints `added`, and `check` prints `no problems`.
  - `homePattern` doesn't match after `@` ([sensitive.go:16](internal/brain/domain/sensitive.go#L16)).
  - brain.md:78-88 says `plugin add` refuses home paths in plugin IDs.
- `project add` imports `permissions.allow` entries holding `Bearer abcDEF123456789` into brain.toml (Repro).
- The reviewer also saw `sk_live_…`, `hf_…` and `npm_…` pass.
- Fix:
  - Add `@` to the anchor set.
  - Add `Bearer \S{8,}`, `sk_(live|test)_`, `hf_`, `npm_`, `ya29.`, `SG.`.

#### 12. `status` misses a new skill not yet committed (Repro)

- Where: `State` runs `git status --untracked-files=no` ([brain_checkout.go:367](internal/brain/infra/brain_checkout.go#L367)).
- Repro: in a clean, committed Brain, add `skills/newone/SKILL.md`. `status` prints a plain `brain …/brain`, while `git status` shows `?? skills/`.
- Spec: SKILL.md:12 says status reports "Brain changes not yet committed", and its "Changing the Brain" step 1 is exactly this case.
- Fix: count untracked files in `State`. The pull's skip rule (tracked files only) can stay.

#### 13. `check` aborts on one odd skill folder, and skips a linked one (Repro)

- Where: `OwnSkills` uses `e.IsDir()` and fails on the first unreadable folder ([brain_checkout.go:407](internal/brain/infra/brain_checkout.go#L407)).
- Repro:
  - `skills/notes/` with no SKILL.md makes check print `error: skill notes: open …/SKILL.md: no such file or directory`, with rc 1 and no problem list.
  - With `skills/linked -> ../real/linked`, whose SKILL.md has no description, check prints `no problems`. Sync links it, and `OwnSkill` uses Stat.
- Effect: `check` is the Brain's CI gate (brain.md:127). It is blind to linked skills, and a stray folder hides every other problem.
- Fix: Stat each entry, and turn a skill that can't be read into a problem.

### Low

- **A moved checkout, `project add`, then `project rm` before any sync** (Code + reviewer).
  - `project rm` prints `cleaned …/proj2`, but the links and the hook stay. Only state finds links ([project.go:150-158](internal/brain/app/project.go#L150-L158)).
  - The exclude block is gone, so git shows `?? .claude/skills/…`.
- **`skill update` with two `[[source]]` at one sha** (Code + reviewer).
  - It prints the diff, then `two [[source]] tables are pinned at …` ([brain_checkout.go:300](internal/brain/infra/brain_checkout.go#L300)). A fork and its upstream at one commit can never move.
  - A repo listed twice: only its first table is ever diffed or moved (`UpdatePin`, [items.go:181](internal/brain/app/items.go#L181)). `check` accepts both cases.
  - Fix: key `MovePin` on the repo.
- **A diverged Brain dumps 11 lines of `hint:`** (Repro).
  - The failure is documented (sync.md:12), but in autosync the whole systemMessage is git's advice.
  - Use `firstLine` with an instruction ("rebase the Brain on its upstream"), as is done elsewhere.
- **Wrong advice for an unlisted checkout** (Repro). `… is not a listed checkout: run nerve project add <name> in it, or pass --project` ([lookup.go:50](internal/brain/app/lookup.go#L50)), but `sync --here --project app` prints `--project does not go with this command`. Autosync shows this in every moved checkout.
- **A hook run is not bounded below Claude Code's hook timeout** (Code; the limit itself is not measured).
  - No `timeout` is written on the hook entry.
  - A first source fetch is 3 git calls of up to 2 minutes each (`gitTimeout`). Only the pull's fetch has 30 s.
  - ADR 0007's "bounded by" is true, but the bound is above the hook's limit.
- **A project switched to `surface = "claude.ai"`** is unlisted with no `unlisted` event, and cleaned as `project app left the Brain` ([sync.go:95](internal/brain/app/sync.go#L95)) (Code + reviewer).
- **An adopted link is reported as `linked`** once after a move. A standing problem keeps the summary at `synced` instead of `up to date` at every run ([sync.go:277](internal/brain/app/sync.go#L277)) (Code + reviewer).
- **`skill ls` lists any entry of `.claude/skills`** as a team skill, a README included ([items.go:165](internal/brain/app/items.go#L165)) (Code + reviewer). Agents are already filtered.
- **Third-party sources** (Code + reviewer):
  - A link inside a skill folder (`leak.txt -> /etc/passwd`) is carried into the project. `skill update` shows it, and zips refuse it.
  - The `skill update` diff keeps bidi controls (U+202E) and has no size cap. This diff is the review gate for untrusted code.
  - A killed fetch leaves `<sha>.tmp-*` in the cache, never swept.
- **File writes** (Code, and Code + reviewer):
  - A rewrite of an existing 0600 overlay or state goes through a temp file at 0644 minus umask, chmod'ed only after the write ([files.go:43-52](internal/brain/infra/files.go#L43-L52)).
  - A killed run leaves `.settings.local.json.<rand>`, which git shows.
- **`main.go:191` drops `machine.Load`'s error** (Code). Every use case loads the machine first today, so this is latent only. Return the error.
- **Hook mode on a parse error** (Code + reviewer):
  - `sync --hook --project` exits 1 with text on stderr.
  - `sync --hook extra` puts the whole usage in `systemMessage`.
  - `sync --hook --json` silently drops `--json`.
- **Single-dash words are values** (Code + reviewer). `setting set -x v` stores the key `-x`, so a typo like `-all` becomes a name.
- **`check` and `skill update` need HOME** (Code + reviewer). `env -i nerve check` fails on `$XDG_CONFIG_HOME`, which matters for bare CI containers. `wire` builds the machine paths even when they aren't needed.
- **The `no brain.toml in this directory or above` message** shows when a brain.toml exists but no `.git` does ([main.go:208](cmd/nerve/main.go#L208)) (Code + reviewer).
- **Sensitive detail** (Code + reviewer):
  - The reason depends on map order (email, token or home path across runs).
  - `/root-tools/x` is flagged.
  - `/mnt/c/Users/…` and `/var/home/…` are missed.
- **Frontmatter** (Code + reviewer):
  - Escapes in quoted values are not decoded.
  - An opening `--- ` with trailing spaces is refused, while the closing one is accepted.
  - A duplicate key silently wins.
- **`OverridesOfCommon` counts `{}`** ([settings.go:641](internal/brain/domain/settings.go#L641)), so `check` reports an empty object as "equals common" (Code + reviewer).
- **`ZipCache.Remove` swallows its error**, while `unload` is reported (Code + reviewer).
- **A listed checkout whose `.git` was removed** fails every full sync with `not in a git repo`, and names no fix. This is what is left of review 3's #6 (Code + reviewer).
- **`git --path-format=absolute` needs git 2.31 or later** ([trees.go:26](internal/brain/infra/trees.go#L26)). No doc states a minimum (Code).
- **No lock on the shared exclude block** between concurrent worktree syncs (Plausible).

## Specs not met

- **ADR 0005 / sync.md:92 and :97, "removes only what it made", "never touches what it did not make"**: bugs 1 and 6.
- **ADR 0005, "writes only in listed projects", "never at user level"**: bugs 5 and 6.
- **CLAUDE.md and brain.md:76-88, "nothing sensitive in the Brain"**:
  - bug 2 (nested secret keys);
  - bug 4 (a machine path);
  - bug 11 (tokens, and a plugin ID with a home path).
- **sync.md:36, "secrets … shown as (hidden)"**: bug 2 prints them.
- **sync.md:86, "a link you point elsewhere becomes yours: nerve leaves it alone"**: bug 8.
- **SKILL.md:12, status reports "Brain changes not yet committed"**: bug 12.
- **brain.md:127, check "validates own skills"**: bug 13.
- **The comments "keep every digit"** ([settings.go:667](internal/brain/domain/settings.go#L667), [files.go:96](internal/brain/infra/files.go#L96)): bug 10.
- **Design tension, documented, to confirm.** A key the Brain never mentions (`outputStyle` set by `/config`) is drift. Every sync by hand then exits 1, and `--discard` deletes it (sync.md:60-62). That is hard to square with "removes only what it made". The Brain doesn't disagree: it has no opinion.

## Docs

- **Stale after ADR 0007**:
  - [README.md:78](README.md#L78), "From then on, `sync` keeps nerve at the release the Brain pins". It contradicts sync.md:15 and brain.md:51. Drop the line, or link to brain.md.
  - [GLOSSARY.md:12](GLOSSARY.md#L12), "and write changes back into it".
  - [development.md:20](docs/development.md#L20) "release" in the infra listing.
  - [files.go:2-3](internal/brain/infra/files.go#L2-L3) "its own release".
- **[README.md:14-16](README.md#L14-L16), "Nothing reaches every project"**: common settings do (GLOSSARY, brain.md). This is still open from review 3.
- **SKILL.md**:
  - The drift option "only on this machine (the overlay)" names no command, since `setting set` sends only sensitive values there. Name the file, or drop the option.
  - `--discard` reads as per key, but it drops all drift in the project.
  - It has no rule for `cleaned`, `version` (from `status`), `pinned` or `already-pinned`. "Left to the user" doesn't cover installing the pinned release.
- **`status` tells a development build to install the pin** (`… (the Brain pins v0.3.0: install it)`), while brain.md:52 and development.md:10 say nerve "says nothing to a development build". Only sync is silent.
- **README** never says the Brain must exist before `nerve init`. A new user gets `brain.toml: open …: no such file`.
- **Glossary**:
  - "the Brain's checkout" (main.go:22, README.md:72, brain.md:60 and :104, sync.md:24) uses Checkout, which GLOSSARY defines as a project's working tree.
  - "machine file" appears in CLI errors but has no entry.
  - Both are still open from review 3.
- **"How" in user docs** (still open): the exclude-block mechanics (sync.md:22-29), and `state/` (brain.md:122-123).
- **The open-bug machinery has no users**:
  - the `[bugs]` condition ([script_test.go:105-113](cmd/nerve/script_test.go#L105-L113));
  - `NERVE_OPEN_BUGS` (development.md:6);
  - the `openBug` text (development.md:32-35, CLAUDE.md:27).
  - Keep the rule if it is a process, but the dead condition goes against "no speculative abstractions".
- **setupWorld's Brain remote** `git@github.com:me/brain.git` ([script_test.go:79](cmd/nerve/script_test.go#L79)) only served owner prefixes. It is vestigial now.

## Tests

Essential behaviour with no test:

- `project rm` after `project add` imported the user's own settings and a secret (bug 1).
- Secret keys and token formats at any depth, through `setting set`, `project add` and `check` (bugs 2 and 11). `TestSensitiveLeavesOrdinaryValuesAlone` still has 5 values.
- A full sync where Locate fails for a worktree whose project is still listed (bug 3). The lost-worktree tests only cover a project that left.
- `project add` of a checkout that holds nerve's generated hook (bug 4).
- A symlinked `.claude/skills` or `settings.local.json` through link and clean (bugs 5 and 6).
- The blast radius of a corrupt state: another tree's sync (bug 7).
- The events and `skill ls` for a repointed link, and whether they repeat (bug 8).
- A source that can't be fetched on a first sync (bug 9).
- A user-only number above int64 surviving a sync (bug 10).
- `status` with an untracked skill, and `check` with a stray or linked skill folder (bugs 12 and 13).
- `MovePin` with two tables at one sha, and `UpdatePin` with a repo listed twice.
- Adoption of a moved checkout's links at the app level, and `project rm` before the first sync.
- End to end: a successful `skill update`, hook-mode parse errors, `init` errors, and a successful `pulled`.
- Timeouts, and a failed or killed fetch leaving no tmp.

Weak tests:

- `TestParseValueReadsAWholeNumberBeyondInt64AsAFloat` ([settings_test.go:487](internal/brain/domain/settings_test.go#L487)) pins the digit loss as correct.
- [check_in_listed_checkout.txtar](cmd/nerve/testdata/script/check_in_listed_checkout.txtar) passes for the wrong reason.
  - Its brain.toml is in `proj/config`, which has no `.git`, so `brainAround` never takes it.
  - The reviewer replaced `!inCheckout(m, wd)` with `true` and every script still passed.
  - Put the brain.toml at proj's top.
- `TestSyncLeavesALinkSomeonePointedElsewhere` ([sync_test.go:515](internal/brain/app/sync_test.go#L515)) checks only that the link is kept. Bug 8 passes it.
- `TestStatesRefuseAStateFileTheyCannotRead` pins the refusal, not its scope (bug 7).
- `TestJSONReportIsOneDocumentOfEventsAndTheError` never encodes an event.
- `! stdout 'pulled'` in `sync_with_broken_machine_file_pulls_nothing.txtar`, and `! stdout 'pulled|no pull'` in `sync_without_…`, pass vacuously. The commit count is the real check.
- `TestZipKeepsWhatAFileMayRun` doesn't check that a plain file stays 0644.
- `TestDiffShowsOnlyTheSourcesTextAsItIs` covers ESC and BEL only, not bidi or size.

Fakes in [fakes_test.go](internal/brain/app/fakes_test.go) that hide bugs:

- `fakeBrain.MovePin` moves every source at the old sha, so the real "two tables" refusal can't show.
- `fakeBrain.OwnSkill` never returns `(skill, true, err)`, so a malformed own skill is untested.
- `fakeBrain.Pull` only counts.
- `fakeMachine.saveErr` is shared by `Save`, `SaveOverlay` and `DropOverlay`, and `stateErr` by `SaveState`, `DropState` and `SaveBuilt`. A test cannot fail one alone.
- `fakeTrees.Remove` and `ReadSettings` cannot fail. `Link` has no model of a linked parent folder (bug 5).
- `fakeZips.Remove` cannot fail, and `Has` reads a map.

## Checked and fine

- Review 3's fixes hold:
  - #1: sync loads the machine before the pull, and a broken or missing machine file pulls nothing.
  - #2: `project rm` saves before it drops the overlay.
  - #3: no `.git` in the source cache.
  - #8: nerve's paths are shown.
  - #9: the home rule is anchored, and `/root`, JWT, PEM, `AIza` and credentials in URLs are caught.
  - #10: stray names in `state/` are ignored.
  - #11: empty, blank and BOM settings files are read.
  - #12: new files follow the umask.
  - #14: a comment holding the sha no longer breaks `skill update`.
  - #15: `status --project` shows each checkout.
  - The Low items:
    - `--json` on a bad flag;
    - a stray brain.toml;
    - `project rm ../../x`;
    - zips of a linked folder, and exec bits;
    - the hook keeps the link it ran through;
    - a symlinked HOME;
    - `SetPath` under a scalar;
    - frontmatter BOM and quoted keys;
    - `Describe` and `{}`.
- ADR 0007:
  - No write-back, mass-loss, owner, upgrade or `nerve-checksums` code is left.
  - A deleted link is made again once, then sync is quiet.
  - A name the team has is not linked.
  - A link pointing where nerve would point it is adopted.
  - `project rm` leaves a repointed link alone.
- Hook mode exits 0 with one JSON document for these pulls: diverged, dirty, detached, no upstream, unreachable remote, `index.lock`, and a pulled broken brain.toml.
- brain.toml round-trips every documented key, including keys like `"[weird"`, multi-line strings, nested arrays and empty tables. `MovePin` keeps comments and CRLF.
- There is no argument injection into git: repo and sha are validated, and `--` comes before URLs.
- Reconcile has no loss path across a dozen sequences of edits, Brain changes, list items, type flips and `--discard`. Each ends in drift or an explicit `set`.
- The exclude block handles:
  - CRLF, a missing final newline and a missing `info/`;
  - worktrees, a submodule and `--separate-git-dir`;
  - a damaged block, which is refused before any link.
- A `.claude` that is itself a link is stopped by `CheckIgnored`. `Remove` is guarded by `ours()`.
- A failed `AddProject` save keeps the overlay, and a retry imports cleanly.
- claude.ai projects get no hook or settings. A zip is reported once per change, and a skill that can't be built keeps its old zip.
- Every command shown in the README, `nerve help`, brain.md and SKILL.md runs as documented, from subfolders too. Every event kind named in SKILL.md exists.

## Fix order

1. **Data and secrets:**
   - `project rm` keeps what the user had: write the overlay and the imported values back, or keep the overlay (1).
   - Check secret keys at every depth, and add the token formats and `@` (2, 11).
   - Don't import nerve's own hook (4).
2. **Writes outside the project:**
   - Refuse linked `.claude/skills`, `.claude/agents` and `settings.local.json`, or clean through the link (5, 6).
3. **Syncs that wipe or keep failing:**
   - Keep a worktree whose project is still listed when git fails (3).
   - Skip an unreadable state (7).
   - Treat a source that can't be fetched as one skill's problem (9).
   - Give a repointed link its own case (8).
4. **Correctness:**
   - Big numbers (10).
   - `status` and untracked files (12).
   - `check` on odd own-skill folders (13).
   - `MovePin` by repo.
   - The unlisted-checkout advice.
   - Trim the diverged-pull message.
   - Bound the hook run, or write a `timeout`.
5. **The rest of Low, then the specs, docs and SKILL.md.**
6. **Tests:**
   - Fix the fakes first (`MovePin`, `OwnSkill`, split error knobs), so the app tests can see bugs 3, 7 and the `MovePin` refusal.
   - Then write each missing test before its fix, so it fails first.
