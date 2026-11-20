# Plan: carry the Brain per project

Status: implemented in v0.3.0, 2026-11-20. The file names under Code predate the layers of [ADR 0006](../adr/0006-hexagonal-layers.md). Implements [ADR 0005](../adr/0005-projects-opt-in.md). Ideas left out are in the [backlog](../backlog.md).

## Brain layout

```text
brain.toml          # nerve pin, autosync, third-party sources, hooks, common settings, projects
skills/<name>/      # own skills
agents/<name>.md    # own agents
hooks/<script>      # scripts hooks run, as {{brain}}/hooks/<script>
```

```toml
nerve = "v0.3.0"
autosync = true

[[source]]
repo = "mattpocock/skills"
sha = "5863806dd4e15c63174bee4e7515a4256792ecfa"
skills = ["grilling", "code-review"]

[hook.go-fmt]
event = "PostToolUse"
matcher = "Edit|Write"
command = "{{brain}}/hooks/go-fmt.sh"

[common.settings]
syncClaudeAiSkills = false

[project.brain]               # the Brain is a project too
skills = []

[project.nerve]
skills = ["go-stack", "grilling"]
agents = ["reviewer"]
hooks = ["go-fmt"]
plugins = ["gopls-lsp@claude-plugins-official"]

[project.nerve.settings]
model = "opus"

[project.claude-ai-work]
surface = "claude.ai"
skills = ["research"]
```

- Settings are written as TOML and become JSON. They are merged by the existing rule: objects key by key, arrays joined without repeats, the project's scalars win. `{{brain}}` still becomes the checkout path.
- A plugin is written into the project's settings as `enabledPlugins`. A hook is written as a `hooks` entry.
- `check` refuses:
  - secret keys, home paths, emails and token-like strings anywhere;
  - unknown skills, agents and hooks, and unpinned sources;
  - anything but skills on a claude.ai project;
  - project overrides equal to `common`.

## This machine

The machine file is never in the Brain:

```toml
brain = "~/dev/brain"

[[checkout]]
path = "~/dev/nerve"
project = "nerve"
```

- Local overlays are per project: `~/.config/nerve/overlays/<project>.json`, laid last.
- State per checkout lives under `~/.config/nerve/state/`, never in the project. It holds the links nerve made, the settings it last wrote and the Brain commit it synced.

## Commands

The commands are grouped by noun, like Docker's.

- A command acts on the project around the working directory. nerve takes the repo's main checkout (`git rev-parse --git-common-dir`, so a worktree counts as its checkout) and looks it up in the machine file. `--project <name>` names a project instead. Outside a listed checkout, without `--project`, the command fails and says to run `nerve project add`.
- `rm --all` removes from every entry (and `common`), and lists the projects affected.
- Commands that edit the Brain change only `brain.toml` and overlays. They never commit.

| Command | Does |
| --- | --- |
| `nerve project add <name>` | Inside a repo: links the checkout to `<name>`, creating the entry if needed by importing the repo's `settings.local.json` (see ADR 0005). |
| `nerve project ls` | The projects, their checkouts on this machine, autosync, and the Brain commit each was last synced to. |
| `nerve project rm <name>` | Cleans its checkouts on this machine and drops the entry. |
| `nerve project autosync on\|off` | Sets `autosync` for the project, or the Brain's default with `--all`. |
| `nerve skill add\|rm\|ls <name>` | Adds a skill to the entry or removes it. `ls` shows yours, the team's, and clashes linked under a prefix. |
| `nerve skill update <repo> [sha]` | Shows the diff of each skill used from `repo`, then moves the pin. |
| `nerve agent add\|rm\|ls <name>` | The same for own agents. |
| `nerve hook add\|rm\|ls <name>` | Adds a hook defined in `brain.toml` to the entry, or removes it. |
| `nerve plugin add\|rm\|ls <plugin>@<marketplace>` | Adds a plugin to the entry, or removes it. |
| `nerve setting set <key> <value> [--common]` | Sets a value in the project's entry, or in `common`, through the sensitive-value guard. |
| `nerve setting rm <key>[=<value>]` | Removes a key, or one array item. |
| `nerve setting ls` | The project's effective settings, each with where it comes from: `common`, project or overlay. |
| `nerve sync [--here]` | Pulls the Brain, syncs every checkout in the machine file, and reports changed claude.ai zips. `--here` syncs only the checkout around the working directory: this is what autosync runs. |
| `nerve status` | Inside a repo: the project's links, settings, drift, autosync, and Brain commits not yet synced or pushed. Elsewhere: the Brain and the checkouts. |
| `nerve check` | Validates the Brain, for its CI. |
| `nerve init` | Writes the machine file. |

Syncing one checkout:

1. Refuse if git tracks `settings.local.json` (`git ls-files`) or does not ignore it (`git check-ignore`).
2. Write the file, or report drift and leave the drifted keys alone. Keys and items that nerve wrote before and that left the Brain are removed.
3. Make the links and the `info/exclude` block. Add the `SessionStart` hook when autosync is on, and remove it when it is off.
4. A link that was deleted removes its skill or agent from the entry, and is reported.
5. Hand over the report, as the hook's `systemMessage` when run by autosync. When it linked a skill, the hook's output also sets `hookSpecificOutput.reloadSkills`, so the skill is usable in that session.

## The nerve skill

It is linked into every listed project and nowhere else.

- `/nerve` with no text runs `nerve status --json` and summarizes. It changes nothing.
- "Use my Go stack here" → `nerve skill add`, show the diff, then commit, push and `sync --here` once the user agrees.
- "Stop denying NotebookEdit everywhere" → `nerve setting rm permissions.deny=NotebookEdit --all`, list the projects affected, then the diff and the commit.
- Drift: for each key, offer to move it into the project entry or `common`, or to drop it.
- Before anything enters the Brain, it shows each value and sends anything identifying to the overlay.

## Code

| Keeps | Drops | Adds |
| --- | --- | --- |
| pull, self-pinning, `check`, `--json` | `marketplace.go` and its `check` rules | projects, `common`, hooks, `autosync` and `surface` in the manifest |
| the settings merge, drift, overlays and the secret guard, now per project | profiles, `[[config]]`, instructions, keybindings | checkouts in the machine file |
| linking with refusal of real folders, fetch and cache (from `505eba1`) | the nerve skill at user level | the noun-grouped CLI and `sync --here` |
| `update` over `[[source]]` (now `skill update`), and zips (from `505eba1`), moved to the cache | claude.ai drift read from `skills/synced/`; the flat `update` | the `info/exclude` block via `git rev-parse --git-path info/exclude`, so worktrees work |
| | | the wider sensitive-value guard, the ignore and tracked checks, write-back of deleted links, the clash prefix, the autosync report |

Release as `v0.3.0`, a breaking change.

## Checked

On Claude Code 2.1.325 with `claude -p` in a throwaway repo, and in the docs (code.claude.com: skills, hooks, settings):

| # | Question | Answer |
| --- | --- | --- |
| 12 | Symlinked skill folders in `.claude/skills/`? | Loaded. |
| 13 | Does what a `SessionStart` hook writes apply in the same session? | Settings do: a deny the hook wrote blocked the command in that session. A skill it linked does when the hook outputs `reloadSkills: true` inside `hookSpecificOutput` (placed at the top level, it is ignored). Agents were not checked. |
| 14 | A report to the user, kept out of Claude's context? | `systemMessage`: it reached the output stream and Claude did not see it. Plain stdout and `additionalContext` go into Claude's context. |
| 15 | Does `git check-ignore` follow the global excludes file and nested `.gitignore` files? | Yes, and `info/exclude`. It reports a tracked file as not ignored, so nerve checks `git ls-files` too. |
| 16 | What does Claude Code write into `settings.local.json`? | Documented: `permissions.allow` from "don't ask again", and project-local `/config` choices. It adds `**/.claude/settings.local.json` to the global excludes file the first time it writes the file. |
| 17 | Can claude.ai's synced skills be turned off? | `syncClaudeAiSkills: false`, in user or local settings, not in a team's `.claude/settings.json`. |
| 11 | Which name invokes a skill linked under another folder name? | The folder name: `medilies-renamed` with `name: renamed` is listed and invoked as `medilies-renamed`. |

## Migration

1. Move `plugins/brain/skills/*` back to `skills/`. Turn the `git-subdir` entries back into `[[source]]`. Delete `.claude-plugin/`, `settings/`, `instructions/` and `keybindings.json`.
2. Run `nerve project add` in each project in use, and add one claude.ai project per account.
3. `nerve sync` on each machine.
4. Clean each config by hand: the generated `settings.json`, the `brain` marketplace and its plugins, the nerve block in `CLAUDE.md`, the keybindings link and the nerve skill link.
5. Bring each claude.ai account in line with the zips `sync` reports.
6. Set `syncClaudeAiSkills: false` by hand in each config's user `settings.json`, so claude.ai's skills stop reaching projects nerve does not list. nerve no longer writes user settings.
