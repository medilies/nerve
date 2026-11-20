# nerve

The Nervous system for a Brain: it carries one person's skills, agents, hooks, plugins and settings to the projects that list them, and nowhere else. A Brain is a git repo you own; nerve knows nothing about what is in it beyond its layout. Words used here are defined in [GLOSSARY.md](GLOSSARY.md), and the reasoning is in [ADR 0005](docs/adr/0005-projects-opt-in.md).

## Install

Download the binary for your system from the [releases](https://github.com/medilies/nerve/releases) onto your `PATH` as `nerve`, or:

```sh
go install github.com/medilies/nerve/cmd/nerve@latest
```

Then, on each machine, name the Brain's checkout once:

```sh
nerve init ~/dev/brain
```

and in each project you want to carry the Brain to, `nerve project add <name>`, then `nerve sync --here`. From then on, use nerve through its skill: `/nerve` in a listed project.

## A Brain

```text
brain.toml          # the nerve release, autosync, third-party pins, hooks, common settings, projects
skills/<name>/      # own skills
agents/<name>.md    # own agents
hooks/<script>      # scripts hooks run, referenced as {{brain}}/hooks/<script>
```

```toml
nerve = "v0.3.0"                 # the release that carries this Brain
nerve-checksums = "9f86d0…"      # optional: SHA-256 of that release's checksums.txt
autosync = true                  # sync each project at session start; the default

[[source]]                       # third-party skills, pinned to a full commit
repo = "acme/skills"
sha = "5863806dd4e15c63174bee4e7515a4256792ecfa"
skills = ["grilling"]

[hook.go-fmt]                    # a hook projects can take by name
event = "PostToolUse"
matcher = "Edit|Write"
command = "{{brain}}/hooks/go-fmt.sh"

[common.settings]                # settings every listed project gets
syncClaudeAiSkills = false

[project.billing]                # what one project gets, and nothing else does
skills = ["go-stack", "grilling"]
agents = ["reviewer"]
hooks = ["go-fmt"]
plugins = ["gopls-lsp@claude-plugins-official"]

[project.billing.settings]
model = "opus"

[project.claude-ai-work]         # a claude.ai account: skills only
surface = "claude.ai"
skills = ["research"]
```

nerve rewrites `brain.toml` in this layout when a command changes it, so comments in it are not kept; `skill update` only moves the pin.

Nothing sensitive goes in the Brain: no secrets, home paths, emails, or names of clients and employers. A project is named, never located: where it is checked out is this machine's business.

## This machine

`~/.config/nerve/config.toml` names the Brain's checkout and each listed checkout's project:

```toml
brain = "~/dev/brain"

[[checkout]]
path = "~/dev/billing"
project = "billing"
```

Settings that are sensitive or identifying go in a local overlay, `~/.config/nerve/overlays/<project>.json`. nerve puts them there on its own when it imports a project or sets a value.

## Use

A command acts on the project around the working directory, a worktree included; `--project <name>` names another. Commands that change the Brain edit `brain.toml` and never commit: review, commit and push, then sync.

| To | Do |
| --- | --- |
| Carry the Brain to a project | `nerve project add <name>` in its checkout, then `nerve sync --here`. Its existing `settings.local.json` is recorded, so it behaves as before. |
| Use a skill, agent, hook or plugin in a project | `nerve skill add <name>`, `agent add`, `hook add`, `plugin add <plugin>@<marketplace>` |
| Take one out | `nerve skill rm <name>`, or delete its link in `.claude/skills/`: the next sync drops it from the project in `brain.toml` |
| Take one out of every project | `nerve skill rm <name> --all` |
| Change a setting | `nerve setting set <key> <value>` (`--common` for every project); value is JSON or text |
| Revoke a setting everywhere | `nerve setting rm <key>[=<item>] --all` |
| See the project's settings and where each comes from | `nerve setting ls` |
| Edit or add an own skill | Change `skills/<name>/`: linked from the checkout, so live at once |
| Move a third-party pin | `nerve skill update <repo> [sha]`: shows the upstream diff, then moves the pin |
| Turn autosync off | `nerve project autosync off`, or `--all` for the Brain's default |
| Drop a project | `nerve project rm <name>`: cleans its checkouts here; other machines clean theirs at their next sync |
| Use skills on claude.ai | A `surface = "claude.ai"` project per account. `nerve sync` builds the zips and says which to upload or delete. |
| See where things stand | `nerve status` |
| Validate (in the Brain's CI) | `go run github.com/medilies/nerve/cmd/nerve@$(sed -n 's/^nerve = "\(.*\)"$/\1/p' brain.toml) check` |
| Read results from a script or agent | Add `--json` to any command: one JSON document with its `events` and `error` |

## What sync does

`nerve sync` carries the Brain to every checkout this machine lists; `nerve sync --here`, to the one around the working directory.

- Pulls the Brain first, fast-forward only. It skips the pull while tracked files have uncommitted changes or the checkout has no upstream, and fails if the checkout has diverged.
- Brings nerve itself to the Brain's `nerve` release: it downloads that release, checks it against the release's `checksums.txt` (and that file against `nerve-checksums`, if set), replaces itself and runs the new binary in its place. A development build is kept.
- Refuses a checkout where git tracks `.claude/settings.local.json`, or does not ignore it. Claude Code adds `**/.claude/settings.local.json` to your global git ignore file the first time it writes one; nerve never edits `.gitignore`.
- Links the project's skills into `.claude/skills/` and agents into `.claude/agents/`, with its own `nerve` skill, and keeps them out of git with a marked block in the repo's `info/exclude`. Own skills link from the Brain checkout; third-party skills from a cache at their pin. Where the team already has a skill of that name, it links yours as `<owner>-<name>`, the owner taken from the Brain's git remote.
- Drops from the project, in `brain.toml`, a skill or agent whose link was deleted.
- Writes `.claude/settings.local.json`: common settings, then the project's, then its plugins (`enabledPlugins`), hooks, and the autosync hook, then the local overlay. Objects merge key by key, arrays are joined without repeats, and anything else in a later layer wins. `{{brain}}` becomes the Brain's checkout path.
- Leaves a key as it is when it was changed in the project since nerve last wrote it (by an approval, `/model` or `/config`, say) and the Brain disagrees. It reports it as drift: move the change into the Brain, or `sync --discard`.
- Removes what nerve wrote for a project that left the Brain, and keeps any key changed since.
- Builds a zip per skill of each claude.ai project in nerve's cache, and reports those that changed since the last build.

## Autosync

With autosync on, the project's `settings.local.json` holds a `SessionStart` hook that runs `nerve sync --here --hook`. Each session in the project then starts in step with the Brain, and hands you a report: what it linked or removed, which settings it set or revoked, what it held back as drift, and whether it skipped the pull. When nothing changed, it is one line. The report reaches you, not Claude's context. When it links a skill, Claude Code rescans skills, so the skill is usable in that session.

## Notes

- `check` and `sync` refuse sensitive values in `brain.toml`: secret-bearing settings such as `env` and `apiKeyHelper`, home paths, emails and token-like strings.
- Skills uploaded to claude.ai reach every Claude Code session signed into that account. `syncClaudeAiSkills: false`, in your user settings or `common`, stops that.
- Third-party skills and the nerve skill link into nerve's folder in the user cache. If the system clears that cache, those links break until the next sync.
- `nerve-checksums` guards against a release replaced after you pinned it: `curl -sL https://github.com/medilies/nerve/releases/download/<tag>/checksums.txt | sha256sum`.
- Ideas not taken up yet: [docs/backlog.md](docs/backlog.md). Decisions: [docs/adr/](docs/adr/).
