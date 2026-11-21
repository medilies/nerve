---
name: nerve
description: Tend this project's place in the user's Brain, the git repo of skills, agents, hooks, plugins and settings that nerve carries to the projects that list them. Use for /nerve, "use my <skill> here", "stop <setting> everywhere", "remember that in this project…", settings drift, the autosync report, or moving a skill pin.
---

# nerve

A **Brain** is one person's git repo: own skills and agents, third-party skills pinned to a commit, hooks, and settings. `brain.toml` lists the projects, and what each one gets; a project gets nothing it does not list. The `nerve` CLI carries that into each project's `.claude/settings.local.json` and links in `.claude/skills/` and `.claude/agents/`, all kept out of git. You are its front end: run `nerve` and `git`, read what they report, and make every decision with the user. The CLI never asks; you do.

## /nerve on its own

Run `nerve status --json` and summarize it in a few lines: the project, autosync, what is linked, the team's own skills, drift, and Brain changes not yet committed or pushed. Change nothing. Outside a listed project, say in a few lines what nerve can do here, starting with `nerve project add <name>`.

## The Brain

```text
brain.toml          # nerve pin, autosync, [[source]] pins, [hook.<name>], [common.settings], [project.<name>]
skills/<name>/      # own skills
agents/<name>.md    # own agents
hooks/<script>      # scripts hooks run, as {{brain}}/hooks/<script>
```

A project entry lists `skills`, `agents`, `hooks`, `plugins` and `settings`. `common.settings` reach every project; nothing else does. A `surface = "claude.ai"` project is a claude.ai account and takes skills only. The project's settings are laid: common, then the project, then what nerve adds for plugins, hooks and autosync, then the local overlay, a file in nerve's config folder that never leaves the machine.

Never edit what nerve writes: `settings.local.json` and the links. Change the Brain through the CLI, then sync.

To use the Brain's own things here, find what it offers in the Brain, at the path `status` gives (its `brain` event): `skills/` and `agents/`, and in `brain.toml` the `[hook.<name>]` tables and the skills each `[[source]]` lists. Then `nerve skill add` (or `agent`, `hook`) it.

## Commands

Always add `--json`, then tell the user what happened in plain words. A command that acts on one project, such as `skill add` or `setting set`, acts on the one around the working directory; `--project <name>` names another.

| Command | Use |
| --- | --- |
| `nerve status` | Where things stand. |
| `nerve sync --here` | Carry the Brain to this project. `nerve sync` does every checkout on the machine and builds claude.ai zips. |
| `nerve project add <name>` | List this checkout. Settings it already has stay, as personal settings. |
| `nerve project ls` | The Brain's projects, and their checkouts on this machine. |
| `nerve project rm <name>` | Clean its checkouts here, delete its overlay here, and drop it from the Brain. Ask first. |
| `nerve skill add\|rm <name>`, `agent …`, `hook …`, `plugin add\|rm <plugin>@<marketplace>` | Change what the project lists. `rm --all` takes it out of every project. |
| `nerve skill ls`, `agent ls`, `hook ls`, `plugin ls` | What the project lists and where each comes from; for skills and agents, the team's own and links the user made (`yours`) too. |
| `nerve setting set <key> <value>` | Value is JSON or text. `--common` for every project. A setting the Brain does not allow, or a sensitive value, goes to the overlay on its own; `allowed-settings` in `brain.toml` allows more keys, never one that may hold secrets. A key that holds a dot goes in double quotes: `enabledPlugins."x@y.z"`. |
| `nerve setting add <key> <value>` | Add one item to a list, such as `permissions.allow`. `--common` and the overlay as for `set`. |
| `nerve setting rm <key>[=<value>]` | Remove a key, or one item of a list, from the project and its overlay. `--common` from common; `--all` revokes it from common and every project. |
| `nerve setting ls` | Each setting and the layers that give it. |
| `nerve project autosync on\|off` | Sync at each session start. `--all` sets the Brain's default. |
| `nerve skill update <repo> [sha]` | Show how a source's skills changed since their pin, then move it. |
| `nerve check` | Validate the Brain before committing. |

A `[[source]]` or a `[hook.<name>]` has no command: add it to `brain.toml` by hand, then `nerve check`.

```toml
[[source]]                  # third-party skills
repo = "owner/name"         # on GitHub
sha = "<full 40-character commit>"
skills = ["grilling"]

[hook.go-fmt]               # a Claude Code hook
event = "PostToolUse"
matcher = "Edit|Write"      # optional
command = "{{brain}}/hooks/go-fmt.sh"   # {{brain}} is the Brain's path
```

## Acting on what nerve reports

- **`drift`**: Claude Code or the user changed a setting here that the Brain sets, and nerve left it as is. With `item`, it is one item of a list. For each, ask: keep it for this project (`setting set`, or `setting add` for an item), for every project (`--common`), or drop it. `nerve sync --here --discard` drops all of this checkout's drift at once: say so, and run it only after the user agrees to lose all of it. A value or key shown as `(hidden)` is sensitive: never ask the user to paste it.
- **`personal`**: a setting here the Brain does not set, such as an approval in `permissions.allow` or one the project had before nerve. nerve keeps it, and reports it once. Offer to share it (`setting set`, `setting add`, `--common`) only when it looks useful elsewhere; otherwise leave it.
- **`unlisted`**: this machine no longer lists a checkout: its folder is gone, or its project left the Brain or became a claude.ai account, as `detail` says. `nerve project add` lists it again.
- **`problem`**: propose a fix for each.
- **`diff`** from `skill update`: summarize what changed in the source, and call out anything that widens what a skill can do (new tools, scripts, network access) before the user commits the new pin.
- **`upload`** and **`unload`**: tell the user which zip to upload to which claude.ai account, or which skill to delete there. Neither you nor nerve can do it.
- **`pull-skipped`**: the Brain was not pulled. Say why, and offer to commit, pull, or check the remote.

## Changing the Brain

1. Prefer the CLI. Write a new own skill or agent in the Brain's `skills/` or `agents/`, then `nerve skill add` it.
2. Ask whether a change is for this project or every project, when it is unclear. Default to this project: nothing should reach a project that does not need it.
3. Nothing sensitive enters the Brain: no secrets, home paths, emails, client or employer names, or hosts that identify them. Show the user each value before it goes in; send anything identifying to the overlay.
4. Run `nerve check --json`, show the diff of the Brain, and ask to commit and push. On yes, follow the Brain's own commit rules (its README, CLAUDE.md or AGENTS.md win over this skill), push, and `nerve sync --here --json`.

## Left to the user

- Installing `nerve` on a machine, and `nerve init <brain>`.
- Uploading and deleting skills on claude.ai.
- Setting `syncClaudeAiSkills: false` in their user `settings.json`, if claude.ai's skills should not reach every project.
