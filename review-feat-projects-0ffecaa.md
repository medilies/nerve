# Review of `feat/projects` (0ffecaa)

Status: fixed on top of 0ffecaa, apart from what [review-feat-projects.md](review-feat-projects.md) lists as open, kept or backlogged.

Scope: the whole branch at 0ffecaa (52 commits on main). This is the fifth review. The items still open in [review-feat-projects.md](review-feat-projects.md) are not repeated here.

Baseline: `go test -race ./...`, `go vet ./...`, `golangci-lint run` (0 issues) and `NERVE_OPEN_BUGS=1 go test ./...` all pass. CI catches none of the bugs below.

How each finding was checked:

- **Repro**: I ran it with the built binary on a temp HOME and XDG dirs, with `GIT_CONFIG_GLOBAL=/dev/null` and no network.
- **Code**: I confirmed it by reading the code.
- **Code + reviewer**: I confirmed it by reading the code, and a review agent reproduced it.
- **Plausible**: not reproduced, or the harm depends on a setup nobody tried.

Refuted leads are left out. A few are listed under "Checked and fine".

## Bugs

### High

#### 1. Every write deletes the user's dotfiles named like its temp files (Repro)

- Where: `sweepTemps` in [files.go:82-84](internal/brain/infra/files.go#L82-L84), called by `writeFile` on every write.
  - It deletes any regular file `.<base>.*` older than an hour beside the file it writes.
- Repro: a synced project, then two files, both dated two hours ago:
  - `proj/.claude/.settings.local.json.bak`;
  - `brain/.brain.toml.orig`.
  - Then `setting set model sonnet` and `sync --here`.
  - Both files are gone. A fresh `.settings.local.json.new` stays.
- Same rule: vim's `.brain.toml.swp` goes while the user edits `brain.toml` (the reviewer reproduced this).
- The glob does not escape the folder (Code + reviewer).
  - With a project folder named `app[12]`, a write there deleted `app2/.claude/.settings.local.json.bak`.
  - The folder's own stale temp stayed.
- Breaks CLAUDE.md "It removes only what it made".
- Test: [files_test.go:98](internal/brain/infra/files_test.go#L98) only checks that `.settings.local.json.abc123` goes, never that a user's file stays.
- Fix:
  - Give temps a marker, for example `.<base>.nerve-tmp-<rand>`, and sweep only that shape. `bak` and `orig` are valid base36, so matching base36 is not enough.
  - List the folder with `os.ReadDir` instead of `Glob`.

#### 2. A secret inside a list of objects enters the Brain, and `check` passes it (Repro)

- Where:
  - `Leaves` ([settings.go](internal/brain/domain/settings.go)) does not go into lists.
  - `SettingRefusal` and `Sensitive` then check nested keys only against the token, email and home patterns, never against the secret key names.
- Repro:
  - `allowed-settings = ["foo2"]`, and the project sets `foo2 = [{ apiKey = "abc123", env = { X = "hunter2" } }]`.
  - `nerve check` prints `no problems`, exit 0.
  - `setting set foo2 '[{"password":"pw2"}]'` writes `password = "pw2"` into `brain.toml`.
- Breaks [brain.md:93](docs/brain.md#L93): "A key that may hold secrets is never allowed, at any depth". `check` is the Brain's CI guard.
- Needs an allowed key that holds a list of objects. None of nerve's own allowed keys does.
- Fix: run `secretKey` on every map key at any depth, lists included. Same root cause as bug 3.

### Medium

#### 3. A secret is printed when a setting changes between a scalar and an object (Repro)

- Where: `ShowValue` and `secret` ([settings.go](internal/brain/domain/settings.go)) test only the leaf key. An object value is shown whole.
- Repro:
  - `setting set cfg '"off"'`, then `sync --here`.
  - `setting rm cfg`, then `setting set cfg.dbPassword hunter2`. This goes to the overlay, correctly.
  - `sync --here` prints `set      cfg = {"dbPassword":"hunter2"} (was "off")`.
- The report reaches Claude through the autosync hook.
- Breaks [sync.md:41](docs/sync.md#L41): "Reports what it did, with secrets and emails shown as `(hidden)`".
- Related (Code + reviewer): `PersonalKeys` names two sensitive personal items alike, for example `permissions.allow (hidden)`. After the first one is recorded, a second one is never reported.
- Fix: hide a value whose nested keys or strings are sensitive, using the walk from bug 2. Give each hidden personal item a distinct name, such as a short hash.

#### 4. `skill add` and `agent add` accept a name that `brain.toml` refuses, which breaks every later command (Repro)

- Where: `AddItem` ([items.go:15-48](internal/brain/app/items.go#L15-L48)) checks only that the name exists. `Validate` applies `NamePattern` at the next load. `BrainCheckout.Save` does not validate.
- Repro, with `agents/Reviewer.md` in the Brain:
  - `agent add Reviewer` prints `added    agent Reviewer to project app`, exit 0.
  - Then `status`, `agent rm Reviewer` and `check` all fail with `brain.toml: project app: agent "Reviewer" must be lowercase words joined by hyphens`.
  - `skill add foo/` (a trailing slash, as tab completion adds) does the same.
  - `skill add ../x` works too when `brain/x/SKILL.md` exists (Code + reviewer).
- The only way out is to edit `brain.toml` by hand.
- Hidden by the fakes: `fakeBrain.Save` never validates, and `OwnSkill` and `AgentFile` are map lookups.
- Fix: refuse a name that does not match `NamePattern` in `AddItem`. Validate in `Brain.Save` too, and in the fake.

#### 5. `project rm` deletes a `settings.local.json` that was there before nerve (Repro)

- Where:
  - At the first sync, `Reconcile` ([settings.go:465](internal/brain/domain/settings.go#L465)) records a key as nerve's when it already equals the Brain's value (`Same(have, goal, k)`).
  - Cleaning ([sync.go:526-531](internal/brain/app/sync.go#L526-L531)) then removes the file once nothing else is left.
- Repro:
  - `setting set --common model sonnet`.
  - Write `{"model":"sonnet"}` to `.claude/settings.local.json`.
  - `sync --here` prints no `personal` line.
  - `project rm app` prints `removed  …/proj/.claude/settings.local.json`.
- Breaks "removes only what it made", and sync.md's promise that what the checkout had before nerve stays.
- Fix: at the first sync, report a key that is already equal as personal. Or record a key as nerve's only when nerve wrote it.

#### 6. An empty list is written as `null` (Repro)

- Where: `distinct` ([settings.go:724-731](internal/brain/domain/settings.go#L724-L731)) starts from `var out []any`, so `[]` becomes a nil slice.
- Repro:
  - `setting set foo '[]'` writes `"foo": null` to the overlay. Sync then writes `"foo": null` to `settings.local.json`.
  - `setting set permissions.deny '[]'` stores `deny = []`, but the report says `set      permissions.deny = null`.
- This gets past the CLI's own refusal: `setting set statusLine null` gives `cannot hold null: to remove a setting, use setting rm`.
- Fix: `make([]any, 0, len(list))`, and a test that `SetPath` of `[]any{}` stays an empty list.

#### 7. A claude.ai zip takes in dotfiles and ignored files (Code + reviewer)

- Where: the walk in `Zip` ([zips.go](internal/brain/infra/zips.go)) skips only a folder named `.git`.
- The reviewer zipped a skill folder holding `.env`, `node_modules/` and a `.git` gitfile. All three went into the zip.
- A gitignored `.env` in an own skill's folder goes into the zip that the user uploads to claude.ai.
- Same walk (Code + reviewer): a FIFO in the folder makes `os.ReadFile` block, so `sync` hangs.
- Fix: zip only regular files, and skip dotfiles. Better still, zip only what `git ls-files` lists.

#### 8. Overlays, which hold secrets, are readable by every user at the default umask (Repro)

- With umask 022 or 002, `setting set env.API_TOKEN sk-secret123` prints `env may hold secrets`. The overlay is then mode 644 in a 755 folder. The machine file and the states are 644 as well.
- This was chosen in 259842c ("create new files as the umask says"). [overlay_follows_umask.txtar](cmd/nerve/testdata/script/overlay_follows_umask.txtar) checks only that umask 077 gives 600.
- No doc or ADR decides it. Tools that keep credentials usually write 0600 whatever the umask.
- Fix: write overlays as 0600 in 0700 folders, and update the script. Or record in an ADR why the umask is enough.

### Low

- **A full sync skips a worktree whose state cannot be read, and says nothing** (Repro).
  - Setup: `wt2` is synced, then its state file is made `{`, then a skill is added.
  - `nerve sync` exits 0 and prints only the main checkout's lines. `wt2` never gets the skill.
  - `sync --here` in `wt2` does report the state.
  - Cause: `States` ([machine_files.go:250-261](internal/brain/infra/machine_files.go#L250-L261)) skips unreadable states. `fakeMachine.States` never does, so no test sees it.
- **A path holding `[`, `*` or `?` hides files from `Glob`** (Repro).
  - With the Brain under `…/g[1]/brain`, `check` prints `no problems` despite a broken agent. Under `…/g2/brain` it prints `problem  agents/broken.md: …`.
  - Other uses: `Agents` ([brain_checkout.go:467](internal/brain/infra/brain_checkout.go#L467)), `States` ([machine_files.go:245](internal/brain/infra/machine_files.go#L245)) and `sweepTemps`.
  - Use `os.ReadDir` instead.
- **An overlay value silently overrides a Brain value set later** (Repro).
  - `setting set statusLine.command x` goes to the overlay. Then `statusLine` is allowed, and `setting set statusLine.command y` prints `set      statusLine.command = "y"`.
  - Sync still writes `x`. `setting ls` hints at it with `(project + local overlay)`.
  - Fix: say that the overlay overrides the new value, or drop the overlay's key.
- **`-inf` and `-nan` are taken as values** (Repro).
  - `setting set cleanupPeriodDays -inf` stores the text `"-inf"`. `isNumber` ([main.go:130](cmd/nerve/main.go#L130)) accepts what `ParseFloat` accepts.
- **The overlay hint is wrong for `enabledPlugins` and `hooks`** (Repro).
  - `setting set enabledPlugins.x true` says `list it in allowed-settings to share it`, but `check` refuses those two keys in `allowed-settings`.
  - Name `plugin add` and `[hook.<name>]` instead.
- **`State` does not count unpushed commits on a branch with no upstream** (Code + reviewer).
  - [brain_checkout.go:416](internal/brain/infra/brain_checkout.go#L416). It also says "1 commits".
- **Pull can misreport why it skipped** (Code + reviewer):
  - A missing tracking ref reads `no upstream branch` ([brain_checkout.go:362](internal/brain/infra/brain_checkout.go#L362)). Since the check comes before the fetch, it never fixes itself.
  - Every fetch failure reads `could not reach its remote` ([:372](internal/brain/infra/brain_checkout.go#L372)), and git's reason is dropped.
- **A zip that cannot be removed is forgotten** (Code).
  - [sync.go:591-594](internal/brain/app/sync.go#L591-L594): `built` drops the skill even when `Remove` fails, so no later sync tries again.
- **`skill add` accepts an own skill whose `SKILL.md` is broken** (Code + reviewer).
  - `offers` drops the `OwnSkill` error ([lookup.go:97](internal/brain/app/lookup.go#L97)). The problem shows only at sync.
- **An own skill silently wins over a source's skill of the same name in sync** (Code + reviewer).
  - Only `check` reports it. No doc says that own wins.
- **Control characters in a team folder name reach the terminal** (Code + reviewer).
  - A team skill named `evil\033[2Jx` is printed raw by `skill ls` and `status` ([report.go:37](internal/brain/infra/report.go#L37)). The skill diff already cleans such characters.
- **The user's global git filters run on a source's checkout** (Code + reviewer, harm Plausible).
  - [sources.go:114-120](internal/brain/infra/sources.go#L114-L120). A smudge filter ran once per file. `core.autocrlf` or git-lfs would change the cached skill.
  - Fix: write `* -filter -text` to the temp repo's `info/attributes`.
- **Nits** (Code + reviewer):
  - A multi-line quoted frontmatter value keeps its quotes, and a `: value` line gives an empty key ([skill.go](internal/brain/domain/skill.go)).
  - `Reconcile` is not idempotent when the Brain lists an item twice.
  - The skill diff lets U+2028 and U+2029 through.
  - The error `apiKeyHelper apiKeyHelper may hold secrets` names the key twice.
- **Plausible**:
  - A worktree of a bare repo is listed on its own, not reached through a main checkout ([trees.go:32-34](internal/brain/infra/trees.go#L32-L34)).
  - A checkout path that is a subfolder of its repo, which only a hand edit makes, gets exclude lines anchored at the repo root.

## Spec violations

- **brain.md and GLOSSARY: overlays hold `null`, "nerve puts them there on its own"** (Repro).
  - [brain.md:138-141](docs/brain.md#L138-L141). `setting set statusLine null` is refused instead.
  - Either send `null` to the overlay, or say that only a hand edit puts it there.
- **SKILL.md and brain.md: a not-allowed or sensitive value with `--common` "goes to the overlay on its own"** (Repro).
  - [SKILL.md:42-43](cmd/nerve/skill/SKILL.md#L42-L43) and [brain.md:108](docs/brain.md#L108).
  - The code refuses it, with a correct message: `set it per project, where it goes to the local overlay`.
  - The skill will promise users something that does not happen. Say that the overlay is per project only.
- **sync.md:41 and brain.md:93**: broken by bugs 3 and 2.
- **CLAUDE.md "removes only what it made"**: broken by bugs 1 and 5.
- **CLAUDE.md "every effect is visible and reported"** (Code): writes to the exclude block in `info/exclude` have no event ([sync.go:348-366](internal/brain/app/sync.go#L348-L366)). Either report them, or have sync.md say that the block follows the links.
- **ADR status lines** (Code):
  - [ADR 0002](docs/adr/0002-brain-and-nervous-system.md#L17) still says that `sync` brings `nerve` to the pin.
  - [ADR 0006](docs/adr/0006-hexagonal-layers.md#L18-L20) still lists "the release upgrade" adapter and "the import of a project's settings".
  - ADR 0007 and ADR 0005's amendment dropped them, and neither ADR has a status line saying so.
- **ADR 0006, Plausible**: `cmd/nerve` decides which Brain applies (`onBrainHere`, `brainAround`, `inCheckout`, [main.go:223-270](cmd/nerve/main.go#L223-L270)). That is a rule, not wiring.

## Docs

- [sync.md:59](docs/sync.md#L59) gives the reason for a workaround: "so Claude Code does not cancel it first".
- sync.md says a project can list a plugin "the Brain does not offer". The Brain offers no plugins. Only a malformed ID is refused.
- The error about an unignored `settings.local.json` suggests "or to the repo's .gitignore" ([trees.go:108](internal/brain/infra/trees.go#L108)). That is a team file, which goes against ADR 0005's "yours, not the team's". Drop that part.
- No doc gives the overlay's file mode (bug 8).
- Personal keys are re-sorted, and duplicate keys merge into one. sync.md promises only to keep personal settings, so this is a nit. Add one sentence, or accept it.
- `review-feat-projects*.md` sit at the repo root, but CLAUDE.md's list of records doesn't include them. In the working tree, `review-feat-projects.md` is modified and `review-feat-projects-68e612e.md` is deleted, both uncommitted.

## Tests

Gaps on paths that matter:

- No test that a user's file beside a written file survives (bug 1).
- No test of `AddItem` with an uppercase or path-shaped name (bug 4).
- No test that a nested secret is hidden in a whole-object change, or refused inside a list of objects (bugs 2 and 3).
- No test of `SetPath` or `ParseValue` with `[]` (bug 6).
- No test of a key that predates nerve and equals the Brain's value, followed by `project rm` (bug 5).
- No test that two sensitive personal items get distinct names.
- The "N commits not pushed" line has no test anywhere.
- No `Locate` test for a bare repo's worktree or a separate git dir.
- No negative tests for the sensitive patterns. These pass today: lowercase `bearer`, `privateKey`, `authorization`, `cookie`. Decide which of them brain.md's list covers.
- claude.ai has no end-to-end script. App and infra tests cover it, and the reviewer found it working with the binary, so this gap is minor.

Weak tests:

- [zips_test.go:78](internal/brain/infra/zips_test.go#L78): `bytes.Contains(zip, "TOPSECRET")` runs on the nil zip returned with the error, so it can never fail.
- [project_rm.txtar:7](cmd/nerve/testdata/script/project_rm.txtar#L7): `! stdout imported` checks a word from the removed settings import.
- `drift.txtar:22`: `! stdout 'permissions.allow'` passes whatever sync prints.

Fakes in [fakes_test.go](internal/brain/app/fakes_test.go) that hide bugs found here:

- `fakeBrain.Save` never validates, and `OwnSkill` and `AgentFile` look names up in a map (bug 4).
- `fakeMachine.States` never skips an unreadable state (the silent worktree skip).

## Checked and fine

- Exit codes, stdout vs stderr, `--json` (events, plus `error` only on failure), `--project`, `--` and `--hook` (always exit 0, with a `systemMessage`).
- Every command in README, brain.md, sync.md and SKILL.md ran as shown.
- A release mismatch is a `problem` event during sync. Only `status` emits `version`, and SKILL.md says no more than that.
- The `openBug` helper is missing on purpose: development.md says it is added with the first open bug.
- ADR 0007's "only `skill rm` and `agent rm`" is about links. `hook rm` and `plugin rm` don't contradict it.
- GLOSSARY Avoid words: no real violation (`version`, `target`, `path` and `upstream` keep their own senses).
- `project rm` reports unlisting (`cleaned … project app removed`). A missing checkout is unlisted, and a sibling's sync drops its exclude lines.
- A malformed `settings.local.json` stops `project rm` with an error that names the file.
- The allow-list compares key paths (`LeafKey` joins with `\x1f`), not dotted text.
- Project, repo and sha names are validated, so a cache or overlay path cannot leave its folder.
- A JSON settings file holding `null` is read as empty, and nothing is lost. A CRLF `info/exclude` is rewritten once, then left alone.
- Property checks by the domain reviewer: `Reconcile` gave no changes on a second run over 200k cases (apart from the duplicate-item nit); `Strip` equals `Personal`; `ParseKey` and `Dotted` round-trip; `ParseFrontmatter` never panicked.

## Fix order

1. Bug 1: mark nerve's temp files, sweep only those, and use `ReadDir` (this also fixes the `Glob` bug).
2. Bugs 2 and 3: one sensitive walk at any depth, used by `SettingRefusal`, `Sensitive` and `ShowValue`. Give hidden personal items distinct names.
3. Bug 4: validate names in `AddItem` and `Brain.Save`, and in the fake.
4. Bug 6: `distinct` returns a non-nil slice.
5. Bug 5: report a key that predates nerve as personal, even when it equals the Brain's value.
6. Bug 7: zip only regular, non-dot files, which also fixes the FIFO hang.
7. Bug 8: decide the overlay's mode (0600 in 0700, or an ADR).
8. Docs in the same commits: brain.md and GLOSSARY on `null`, SKILL.md and brain.md on `--common`, and the status lines of ADR 0002 and 0006.
9. Low items: the unreadable worktree state, the overlay override, the overlay hint, `-inf`, the Pull messages, the forgotten zip, and the rest.
10. The test gaps and weak tests above, each with the fix it guards. Start with the fakes.
