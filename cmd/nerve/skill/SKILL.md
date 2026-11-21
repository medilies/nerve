---
name: nerve
description: Tend this project's place in the user's Brain, the git repo of skills, agents, hooks, plugins and settings that nerve carries to the projects that list them. Use for /nerve, "use my <skill> here", "stop <setting> everywhere", "remember that in this project…", settings drift, the session report, pushing or syncing the Brain, or moving a skill pin.
---

# nerve

A **Brain** is one person's git repo: own skills and agents, third-party skills pinned to a commit, hooks, and settings. `brain.toml` lists the projects, and what each one gets; a project gets nothing it does not list. The `nerve` CLI carries that into each project's `.claude/settings.local.json` and links in `.claude/skills/` and `.claude/agents/`, all kept out of git. You are its front end: run `nerve` and `git`, read what they report, and make every decision with the user. The CLI never asks; you do.

nerve acts only when told. `nerve sync` brings the remote here: it makes the Brain its remote's, then carries it into the project. It refuses to drop Brain work not pushed, unless `-f`. `nerve push` sends the Brain to its remote. A command that changes `brain.toml`, such as `nerve skill add`, takes the remote's first, then commits and pushes that change at once. Each session starts with a report of what waits for a sync; it changes nothing.

## /nerve on its own

Run `nerve status --json` and summarize it in a few lines: the project, what waits for a sync, what is linked, the team's own skills, drift, and Brain changes not yet pushed. Change nothing. Outside a listed project, say in a few lines what nerve can do here, starting with `nerve project add <name>`.

## The Brain

```text
brain.toml          # allowed-settings, [[source]] pins, [hook.<name>], [common.settings], [project.<name>]
skills/<name>/      # own skills
agents/<name>.md    # own agents
hooks/<script>      # scripts hooks run, as {{brain}}/hooks/<script>
```

A project entry lists `skills`, `agents`, `hooks`, `plugins` and `settings`. `common.settings` reach every project; nothing else does. The project's settings are laid: common, then the project, then what nerve adds for plugins, hooks and the session report, then the local overlay, a file in nerve's config folder that never leaves the machine.

Never edit what nerve writes: `settings.local.json` and the links. Change the Brain through the CLI, which pushes `brain.toml` itself, then sync.

To use the Brain's own things here, find what it offers in the Brain, at the path `status` gives (its `brain` event): `skills/` and `agents/`, and in `brain.toml` the `[hook.<name>]` tables and the skills each `[[source]]` lists. Then `nerve skill add` (or `agent`, `hook`) it.

## Commands

Always add `--json`, then tell the user what happened in plain words. A command that fails gives its reason in the document's `error`. A command that acts on one project, such as `skill add` or `setting set`, acts on the one around the working directory; `--project <name>` names another.

| Command | Use |
| --- | --- |
| `nerve status` | Where things stand: what waits for a sync, links, the team's own, drift. |
| `nerve sync --here` | Make the Brain its remote's, then carry it to this project. It refuses to drop Brain changes not pushed, naming them: offer `nerve push`, or `nerve sync --here -f` to drop them after the user agrees. `-f` also overwrites drift. `nerve sync` does every checkout on the machine. |
| `nerve push [<message>]` | Commit everything in the Brain, such as a new own skill, then push it. A change needs a message. It takes the remote's commits first when the Brain has none of its own; when it has some too, it refuses, and `-f` overwrites the remote's: ask first. |
| `nerve project add <name>` | List this checkout, a worktree included. Settings it already has stay: as personal settings, or as drift where the Brain sets them otherwise. |
| `nerve project ls` | The Brain's projects, and their checkouts on this machine. |
| `nerve project rm <name>` | Clean its checkouts here, and drop it from the Brain. Its overlay stays. Ask first. |
| `nerve skill add\|rm <name>`, `agent …`, `hook …`, `plugin add\|rm <plugin>@<marketplace>` | Change what the project lists. `rm --all` takes it out of every project. |
| `nerve setting set <key> <value>` | Value is JSON or text. `--common` for every project. A setting the Brain does not allow goes to the project's overlay on its own; with `--common`, nerve refuses it. `allowed-settings` in `brain.toml` allows more keys. A key that holds a dot or `=` goes in double quotes: `enabledPlugins."x@y.z"`. |
| `nerve setting add <key> <value>` | Add one item to a list, such as `permissions.allow`. `--common` and the overlay as for `set`. |
| `nerve setting rm <key>[=<value>]` | Remove a key, or one item of a list, from the project and its overlay. `--common` from common; `--all` revokes it from common and every project. |
| `nerve update [<repo>]` | Show how each source's skills changed at the source, pin and push the latest commit, then sync this project. Ask first. |
| `nerve check` | Validate the Brain before pushing. |

A `[[source]]` or a `[hook.<name>]` has no command: add it to `brain.toml` by hand, `nerve check`, then `nerve push`.

A command that changes `brain.toml` refuses, and changes nothing, when:

- `brain.toml` has changes not pushed (a `diff` event shows them), or the Brain has commits its remote lacks: offer `nerve push` to keep them, or `nerve sync -f` to drop them. When the error says the remote has commits too, `nerve push` refuses: offer `nerve sync -f`, or `nerve push -f` to overwrite the remote, after the user agrees to lose its commits.
- the remote's commits change files changed here: name them, and offer to push or drop those changes first.
- the remote does not have the Brain's branch yet: `nerve push` makes it there.
- the remote cannot be reached, or the Brain has no upstream or a detached HEAD: say so; neither push nor sync helps until it is fixed.
- an own skill or agent is not on the remote yet, as it is here: `nerve push` it, then add it.
- the push failed: when the error says the remote moved, run the command again; otherwise show git's reason, and do not retry.

```toml
[[source]]                  # third-party skills
repo = "owner/name"         # on GitHub
sha = "<full 40-character commit>"
path = "plugins/x/skills"   # optional: where its skills are, when not at the root, skills/ or <name>/
skills = ["grilling"]

[hook.go-fmt]               # a Claude Code hook
event = "PostToolUse"
matcher = "Edit|Write"      # optional
command = "{{brain}}/hooks/go-fmt.sh"   # {{brain}} is the Brain's path
```

## Acting on what nerve reports

- **`checkout`** with changes waiting: the project is behind the Brain. Offer `nerve sync --here`.
- **`drift`**: Claude Code or the user changed a setting here that the Brain sets, and nerve left it as is. With `item`, it is one item of a list. With `detail` `local overlay`, the local overlay sets it, and `setting set` keeps it there. For each, ask: keep it for this project (`setting set`, or `setting add` for an item), for every project (`--common`), or drop it. `nerve sync --here -f` overwrites all of this checkout's drift at once: say so, and run it only after the user agrees to lose all of it.
- **`personal`**: a setting here the Brain does not set, such as an approval in `permissions.allow` or one the project had before nerve. nerve keeps it, and reports it once. Offer to share it (`setting set`, `setting add`, `--common`) only when it looks useful elsewhere; otherwise leave it.
- **`unlisted`**: this machine no longer lists a checkout: its folder is gone, or its project left the Brain, as `detail` says. `nerve project add` lists it again.
- **`problem`**: propose a fix for each.
- **`brain-synced`** with a `detail`: `sync -f` dropped local commits, changes or untracked files in the Brain, naming a few. Say so plainly.
- **`brain-not-synced`**: the Brain was not made its remote's. Say why, and offer to set its upstream or check the remote.
- **`pushed`**: the Brain's remote now holds this machine's Brain; `detail` is the commit's message.
- **`overlay`**: a setting went to the project's local overlay, which never leaves the machine, for the reason `detail` gives.
- **`diff`** from `update`: summarize what changed in the source, and call out anything that widens what a skill can do (new tools, scripts, network access). The pin is pushed already: to go back, set the old `sha` in `brain.toml`, then `nerve push`.
- **`cleaned`**: nerve took its links and settings out of a checkout, for the reason `detail` gives. Say so; nothing to do.
- **`kept`**: nerve left a file it made, such as the local overlay of a removed project, which may hold a secret. Say where it is; delete it only when the user asks.
- **`pinned`** and **`already-pinned`**: `update` moved and pushed a source's pin, or found it there already.

## Changing the Brain

1. Prefer the CLI. Write a new own skill or agent in the Brain's `skills/` or `agents/`, `nerve check --json`, then `nerve push --json "<message>"` it, as in step 4, before `nerve skill add` (or `agent add`) puts it in `brain.toml`.
2. Ask whether a change is for this project or every project, when it is unclear. Default to this project: nothing should reach a project that does not need it.
3. Show the user each value before it goes in: the Brain may be public, and nerve does not look for secrets. Anything identifying belongs in the overlay.
4. After a change to the Brain's own files, such as a new skill, run `nerve check --json`, show the diff of the Brain, and ask to push. On yes, `nerve push --json "<message>"`, with a message that follows the Brain's own commit rules (its README, CLAUDE.md or AGENTS.md win over this skill).
5. Then `nerve sync --here --json`.

## Left to the user

- Installing `nerve` on a machine, and `nerve init <brain>`.
- Setting `syncClaudeAiSkills: false` in their user `settings.json`, if claude.ai's skills should not reach every project.
