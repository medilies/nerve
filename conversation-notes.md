# nerve: questions and answers

Notes from a conversation on 2026-11-16, checked against the code at commit `505eba1`.

## 1. What is this project?

**nerve** is a small Go command-line tool that keeps one person's Claude setup the same everywhere they use Claude: skills, settings, standing instructions and keybindings.

### The core idea: two repos

The reasoning is in [ADR 0002](docs/adr/0002-brain-and-nervous-system.md).

- **Brain**: a git repo you own that holds only content. That means your own skills, third-party skills pinned to exact commits, layered settings, instructions and hook scripts. Its manifest is `brain.toml`.
- **Nervous system**: this repo, the `nerve` binary plus one skill built into it. It knows the Brain's layout but has no opinions about what's in it.

Other terms (defined in [GLOSSARY.md](GLOSSARY.md)):

- A **surface** is one place skills are used: one Claude Code config folder (such as `~/.claude` or `~/.claude-personal`) or one claude.ai account.
- Each config follows a **profile**, such as `work` or `personal`.

### What `nerve sync` does

1. **Pulls the Brain**, fast-forward only.
2. **Updates itself.** It downloads the nerve release the Brain asks for, checks it against the release's checksums, and swaps its own binary for it. See [upgrade.go](cmd/nerve/upgrade.go) and [self.go](cmd/nerve/self.go).
3. **Links skills** into each config's `skills/` folder:
   - your own skills come straight from the Brain checkout, so edits work immediately;
   - third-party skills come from a cache at their pinned commit;
   - the nerve skill comes from the binary.

   It only removes links it made itself, and keeps a record of them in `.nerve.json` ([state.go](cmd/nerve/state.go)).
4. **Builds `settings.json`** by merging three layers: the Brain's `base`, then the config's profile, then a local overlay that never leaves the machine ([ADR 0003](docs/adr/0003-generated-layered-settings.md), [settings.go](cmd/nerve/settings.go)). If a key was changed inside Claude Code (with `/model`, for example) and the Brain disagrees, it reports that as **drift** and stops. It also refuses settings that may hold secrets.
5. **Instructions and keybindings:**
   - it keeps a marked import block at the top of each config's `CLAUDE.md` ([instructions.go](cmd/nerve/instructions.go));
   - it links `keybindings.json` back to the Brain ([keybindings.go](cmd/nerve/keybindings.go)).
6. **claude.ai:** there's no API for uploading personal skills, so it builds `dist/<name>.zip` files and reports which ones are missing or out of date on each account, for manual upload.

### Other commands

| Command | What it does |
| --- | --- |
| `init` | Writes `~/.config/nerve/config.toml`: where the Brain is and which profile each config follows ([machine.go](cmd/nerve/machine.go)) |
| `status` | Shows the Brain's location, each config's profile and nerve's version |
| `check` | Validates a Brain; meant for the Brain's CI |
| `update <repo> [sha]` | Shows the upstream changes and moves a third-party pin |
| `--json` | Works on any command and outputs JSON for scripts and agents ([report.go](cmd/nerve/report.go)) |

### The built-in skill

[cmd/nerve/skill/SKILL.md](cmd/nerve/skill/SKILL.md) is installed into every config. It lets you ask Claude in plain language to sync, move a pin, sort out drift, or "remember that I always…". Claude then picks the right file in the Brain, shows you the diff, and commits and syncs once you agree.

### Code and repo

- About 1,900 lines of Go in a single `main` package under [cmd/nerve/](cmd/nerve/), plus around 1,000 lines of tests.
- One dependency (`BurntSushi/toml`); Go 1.26.
- CI and releases are in [.github/workflows/](.github/workflows/). Pushing a `vX.Y.Z` tag builds Linux and macOS binaries along with `checksums.txt`.
- [ADR 0001](docs/adr/0001-own-sync-tool.md) explains why this is its own tool rather than the `skills` CLI or a Claude plugin marketplace.

## 2. Does it meet my specs?

Mostly yes. Seven of the eleven points are fully met. Four are only partly met: per-project stacks, identical settings everywhere, propagating updates, and protecting sensitive info.

| Spec | Met? | How, or what's missing |
| --- | --- | --- |
| All skills in one repo: custom ones with their content, third-party ones referenced | ✅ | Own skills live in `skills/<name>/` with their content. Third-party skills are only listed in `[[source]]` in `brain.toml`. |
| A lock file for skills | ✅ | `brain.toml` is both the list and the lock. It requires a full 40-character commit SHA ([manifest.go:35](cmd/nerve/manifest.go#L35)), and only `nerve update` moves a pin, after showing the diff. Third-party sources can only come from GitHub ([fetch.go:13](cmd/nerve/fetch.go#L13)). |
| My settings too | ✅ | Settings are built from base, then profile, then local overlay. Keybindings are linked from the Brain. |
| Only Claude | ✅ | Claude Code configs and claude.ai, nothing else. |
| Saves habits, techniques, preferences and stacks to use per project | ⚠️ Partly | Habits go in `instructions/*.md` and load in every session. Techniques and stacks are own skills (like `go-stack`), and Claude picks one when its description matches the task. nerve has no idea of a project, so "project X uses stack Y" only works through skill descriptions. |
| Same AI settings on every machine and project | ⚠️ Mostly | The same everywhere once `sync` has run on each machine. But a project's own `.claude/settings.json`, `CLAUDE.md` or skills still win over user-level ones, and nerve can't stop that. Local overlays also differ per machine, by design. |
| Everything applied at user level | ✅ | It only writes to the config folders, `~/.config/nerve`, the user cache, the Brain's `dist/`, and its own binary. |
| Doesn't touch the git tree of existing projects | ✅ | `sync` ignores the folder it's run from. `check` and `update` look upward for a `brain.toml`, so they only ever act on a Brain ([main.go:84](cmd/nerve/main.go#L84)). |
| Updates are saved and spread | ⚠️ Partly | On the same machine, edits to own skills, instructions and keybindings take effect immediately because they're linked. Other machines only get changes when `nerve sync` runs there; nothing runs it automatically. Settings changed inside Claude Code (`/model`, `/config`) are reported as drift, not saved. claude.ai still needs manual uploads. |
| Tool or skill usable from any project or folder | ✅ | The binary is on `PATH`, the skill is installed at user level, and `sync` always uses the Brain set in the machine file. |
| Others can use it without getting my opinions | ✅ | It ships only its own skill. [SKILL.md](cmd/nerve/skill/SKILL.md) tells Claude to follow the Brain's own rules for commits and layout. The only hardcoded owner-specific thing is the tool's own release URL. |
| Doesn't expose my sensitive info | ⚠️ Partly | `check` and `sync` refuse 5 settings keys in the Brain (`env`, `apiKeyHelper`, `awsAuthRefresh`, `awsCredentialExport`, `otelHeadersHelper`), and the local overlay keeps per-machine values out. Nothing scans other setting values, hook scripts, instructions or skills for tokens or identifying details; that rests on one line in the skill asking Claude to check. If a Brain's `.gitignore` doesn't ignore `/dist/`, the claude.ai zips can get committed, and `check` doesn't catch it. |

### Gaps worth closing, most important first

1. **Automatic spreading.** Add a `SessionStart` hook to `settings/base.json` that runs `nerve sync`. Every machine then catches up when a session starts, and nerve stays free of opinions.
2. **Secret scanning.** Have `check` scan everything in the Brain for token-like strings, not just the 5 setting keys, and warn when `.gitignore` doesn't ignore `/dist/`.
3. **Per-project stacks**, if they should be pinned to a project rather than chosen by description. That would need a project-side reference, which goes against "doesn't touch project trees". Clear skill descriptions plus instructions is probably the right trade-off.

## 3. Can third-party skills be updated?

Yes, with `nerve update <repo> [sha]` ([commands.go:41](cmd/nerve/commands.go#L41)):

```sh
nerve update acme/skills            # move to the newest commit upstream
nerve update acme/skills <full-sha> # move to a specific commit
```

What it does:

1. **Finds the target commit.** Without one, it uses the newest commit on the upstream default branch. A given SHA must be the full 40 characters.
2. **Fetches both commits**, the current pin and the new one.
3. **Shows a diff for each skill** used from that repo. If one of those skills no longer exists at the new commit, it stops and says to remove it from `brain.toml` first.
4. **Changes the pin** in `brain.toml`, editing only the SHA.

It doesn't commit or sync. Afterwards:

1. Review the diff.
2. Commit `brain.toml` and push.
3. Run `nerve sync`. That machine now uses the new version; other machines get it at their next sync.

Through the skill, saying "update the grilling skill" makes Claude run the command, summarise the upstream changes, and point out anything that gives the skill more reach (new tools, scripts or network access). It only commits and syncs after you agree.

Limits:

- One source repo at a time; there's no "update everything" command.
- Every skill used from a repo moves together, because the pin belongs to the repo, not to each skill. To keep one skill on an older commit, list the same repo twice in `brain.toml` with different SHAs. Don't count on `update` with that setup: it looks up the source by repo name, so with two entries it picks the last one, and it refuses if both entries have the same SHA.
- Sources can only come from GitHub.

## 4. Will updating `grilling` bring the latest from Matt Pocock?

This machine's Brain is `/home/ilies/dev/skills` (configs `~/.claude-personal` with profile `personal` and `~/.claude` with profile `work`; nerve `v0.1.0`). Its `brain.toml` has two sources:

| Source | Pin | Skills |
| --- | --- | --- |
| `mattpocock/skills` | `5863806dd4e15c63174bee4e7515a4256792ecfa` | `code-review`, `domain-modeling`, `grill-with-docs`, `grilling`, `prototype`, `research`, `setup-matt-pocock-skills`, `wayfinder` |
| `microsoft/tui-test` | `84b741eca41d19d321feeace5a8ea435e19e3c1b` | `tui-test` |

Yes: `nerve update mattpocock/skills` moves `grilling` to the newest commit on Matt Pocock's default branch.

On 2026-11-16 it would have done nothing: `git ls-remote` showed upstream `HEAD` was `5863806…`, the same as the pin. That was already the latest version.

When upstream does change, updating `grilling` also updates the other seven skills from that repo, and the diff covers all eight. To move only `grilling`, give it its own `[[source]]` entry with its own SHA.

## 5. Is that the standard way to update skills?

No. There's no single standard; nerve's way is its own.

| Approach | How updates work | Pinned to a commit? |
| --- | --- | --- |
| **Copy the folder by hand** into `~/.claude/skills/` | Copy it again | No, and it's hard to tell which version you have |
| **Claude Code plugin marketplaces**, the official way to share skills | `/plugin` updates a plugin from its marketplace, and marketplaces can auto-update | Possible: a marketplace entry can pin a `sha` |
| **The `skills` CLI** (vercel-labs) | Install again or update through the CLI | Accepts `owner/repo#sha`, but its lockfile only restores project-level installs, not user-level ones |
| **nerve** | `nerve update <repo>` shows the upstream diff and moves the SHA in `brain.toml`; then commit and `sync` | Always pinned to a full SHA, and every update is a reviewed git commit |

Plugin marketplaces are the closest thing to a standard. [ADR 0001](docs/adr/0001-own-sync-tool.md) records why nerve uses neither of the other tools:

- Plugins were rejected because the owner didn't want these skills packaged as a plugin.
- The `skills` CLI was rejected because its lockfile doesn't cover user-level installs, it sends install telemetry, and pins, removal, zips and drift checks would have needed building around it anyway.

nerve's approach is the same as pinning code dependencies, like `go.mod` or a GitHub Action pinned to a SHA: nothing changes until you look at the diff and decide. That fits skills well, because a skill's text controls what Claude does, so an unreviewed upstream change counts as a supply-chain risk. The cost is that updates never happen on their own: `update` runs once per source repo.
