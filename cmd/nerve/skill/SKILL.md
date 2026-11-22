---
name: nerve
description: Tend this project's place in the user's Brain, the git repo of skills, agents, hooks, plugins and settings that nerve carries to the projects that list them. Use for /nerve, any change to Claude Code's settings, permissions or tools in a project, "use my <skill> here", "stop <setting> everywhere", "remember that in this project…", settings drift, the session report, pushing or syncing the Brain, or moving a skill pin.
---

# nerve

A **Brain** is one person's git repo: own skills and agents, third-party skills pinned to a commit, hooks, and settings. `brain.json` lists the projects, and what each one gets; a project gets nothing it does not list. The `nerve` CLI carries that into each project's `.claude/settings.local.json` and links in `.claude/skills/` and `.claude/agents/`, all kept out of git. You are its front end: run `nerve` and `git`, read what they print, and make every decision with the user. The CLI never asks; you do.

nerve acts only when told. `nerve sync` makes the Brain its remote's, then carries it into the project. `nerve push` sends the Brain to its remote. A command that changes `brain.json`, such as `nerve skill add`, takes the remote's first, then commits and pushes that change at once. `skill`, `agent`, `hook`, `plugin` and `setting` commands then sync this checkout. Each session starts with a report of what waits for a sync; it changes nothing.

## How to work

- Go straight to the result. When the user's words name it, such as "put these in my Brain" or "use X here", run the whole chain in one Bash call joined with `&&`, without stopping: `nerve push "add reviewing" && nerve skill add reviewing && nerve agent add code-reviewer impact-explorer`.
- Stop and ask only when:
  - their words fit more than one reading. They are not commands: "update my Brain" may mean moving pins, cleaning a list, or syncing. Read them in context, such as what you just offered, and name each option in plain words.
  - a command drops or overwrites: `-f`, `project rm`, `rm --all`. Say what it loses.
  - a value going into the Brain looks secret, or names the user or this machine. The Brain may be public, and nerve does not look for secrets: such a value belongs in the overlay.
- Never run a command that pushes, such as `update` without `--dry-run`, `setting`, `skill add`, `allow` or `project rm`, unless the user asked for that change.
- A line that needs action ends with the commands that resolve it, and an error names the commands that settle it. Offer those.

## Closing

End with nerve's outcome in a blockquote that opens with **nerve**, at most 3 lines. Say what is now true, and what the user must do, such as starting a new session. Do not list the commands you ran, commit SHAs, or how nerve works, unless asked.

> **nerve** · The `reviewing` skill and its 2 agents are in your Brain and linked here. Start a new session to use them.

Below it, as plain text, at most 2 short suggestions, when there are any. Run nothing a suggestion names until the user says yes.

## /nerve on its own

Run `nerve status` and summarize it in a few lines: the project, what waits for a sync, the team's own skills, drift, and Brain work not pushed. Change nothing. Outside a listed project, say in a few lines what nerve can do here, starting with `nerve project add`.

## The Brain

```text
brain.json          # allowedSettings, sources (pins), hooks, common.settings, projects
skills/<name>/      # own skills
agents/<name>.md    # own agents
hooks/<script>      # scripts hooks run, as {{brain}}/hooks/<script>
```

An entry of `projects` lists `skills`, `agents`, `hooks`, `plugins` and `settings`. `common.settings` reach every project; nothing else does. The project's settings are laid: common, then the project, then what nerve adds for plugins, hooks and the session report, then the local overlay, a file in nerve's config folder that never leaves the machine.

Never edit what nerve writes: `settings.local.json` and the links. Change the Brain through the CLI.

To use the Brain's own things here, find what it offers in the Brain, at the path `status` prints on its `brain` line: `skills/` and `agents/`, and in `brain.json` its `hooks` and the skills each entry of `sources` lists.

## Commands

A command that acts on one project, such as `skill add` or `setting set`, acts on the one around the working directory; `--project <name>` names another.

| Command | Use |
| --- | --- |
| `nerve status` | Where things stand: what waits for a sync, the team's own, drift. |
| `nerve sync --here` | Make the Brain its remote's, then carry it to this project. It refuses to drop Brain work not pushed; `-f` drops it, and overwrites drift: ask first. `nerve sync` does every checkout on the machine. |
| `nerve push [<message>]` | Commit everything in the Brain, such as a new own skill, then push it. With no message, nerve writes one. When the remote has commits too, it refuses; `-f` overwrites the remote's: ask first. |
| `nerve project add [<name>]` | List this checkout, a worktree included, as a project named after its repo or `<name>`, then sync it. Settings it already has stay: as personal settings, or as drift where the Brain sets them otherwise. |
| `nerve project ls` | The Brain's projects, and their checkouts on this machine. |
| `nerve project rm <name>` | Clean its checkouts here, and drop it from the Brain. Its overlay stays. Ask first. |
| `nerve skill add\|rm <name>...`, `agent …`, `hook …`, `plugin add\|rm <plugin>@<marketplace>...` | Change what the project lists, several names of one kind in one push, then sync this checkout. `rm --all` takes them out of every project. |
| `nerve setting set <key> <value>` | Value is JSON or text. `--common` for every project. A setting the Brain does not allow goes to the project's overlay on its own; with `--common`, nerve refuses it. A setting nerve allows takes only a value Claude Code takes there; the error says what fits. A key that holds a dot or `=` goes in double quotes: `enabledPlugins."x@y.z"`. |
| `nerve setting add <key> <value>` | Add one item to a list, such as `permissions.allow`. `--common` and the overlay as for `set`. |
| `nerve setting rm <key>[=<value>]` | Remove a key, or one item of a list, from the project and its overlay. `--common` from common; `--all` from common and every project. |
| `nerve allow add\|rm <key>...` | Add keys to `allowedSettings`, or take them out. |
| `nerve update [<repo>]` | Show each source's pin and tip, and the files each skill changed; `--diff` adds the whole diff. Then pin and push the tip, and sync this project. `--dry-run` shows the same, and moves no pin. |
| `nerve check` | Validate the Brain before pushing. |

When a command that changes `brain.json` refuses, it changes nothing, and its error names the commands that settle it. Run one with `-f` only after the user agrees to lose what it drops. When the remote cannot be reached, or the Brain has no upstream, say so: neither push nor sync helps until it is fixed. When a push failed and the error says the remote moved, run the command again; otherwise show git's reason.

## Reading what nerve prints

- **`drift`**: a setting the Brain sets, changed here, and left as is. It never fails a command. Ask: keep it (the command the line names), for every project where that is not already what the command does (with `--common`), or drop it. `nerve sync --here -f` drops all of this checkout's drift; setting the key again, even to the Brain's value, drops its drift alone.
- **`personal`**: a setting the Brain does not set, such as an approval. nerve keeps it. Offer to share it only when it looks useful elsewhere.
- **`problem`**: propose a fix for each.
- **`note`**: worth knowing, but not wrong. Offer the command it names.
- **`synced   the Brain to …, dropping …`**: `sync -f` dropped Brain work not pushed. Say so plainly.
- **`no sync`**: the Brain was not made its remote's. Say why, and offer to set its upstream or check the remote.
- **`overlay`**: a setting went to the local overlay, which never leaves the machine, for the reason the line gives.
- **`kept`**: nerve left a file it made, such as the overlay of a removed project, which may hold a secret. Say where it is; delete it only when the user asks.
- **`tip`**, **`changed`** and **`same`** from `update`: show one table, a row per skill:

  | Skill | Pinned (commit, date) | Tip (commit, date) | Summary |
  | --- | --- | --- | --- |

  - Each `tip` line gives a source's pin and tip, with their dates.
  - For a `changed` skill, summarize its files in a few words. For a `same` one, write "no change".
  - When a file may widen what the skill can do, such as a new script or tool, read it in the folder the `changed` line names, and say so in the row.
  - To read an update before it lands, run `nerve update --dry-run`, show the table, and ask. On yes, run `nerve update`. When its table differs, the repo moved since, and the newer tip is pinned already: show the new table, and offer to set the old `sha` back.
  - Add `--diff` only when the user asks to read the changes.
  - To undo an update, set the old `sha` in `brain.json`, then `nerve push`.

## Changing the Brain

To change what Claude Code does, find the setting made for it first: know Claude Code's settings, or search its settings docs. `enableArtifact: false` turns artifacts off, for one. A permission rule on a tool's name is the fallback, when no setting does it. A key nerve does not allow needs `nerve allow add` before `--common` takes it. nerve checks the value of a setting it allows, but not of one `allowedSettings` lists: check that value against Claude Code's settings docs first.

- Ask whether a change is for this project or every project, when it is unclear. Default to this project.
- A new own skill or agent: write it in the Brain's `skills/` or `agents/`, then `nerve check && nerve push "<message>" && nerve skill add <name>`. The message follows the Brain's own commit rules: its README, CLAUDE.md or AGENTS.md win over this skill.
- An entry of `sources` or `hooks` has no command: add it to `brain.json` by hand, then `nerve check && nerve push "<message>" && nerve sync --here`.

```json
{
  "sources": [
    {
      "repo": "owner/name",
      "sha": "<full 40-character commit>",
      "path": "plugins/x/skills",
      "skills": ["grilling"]
    }
  ],
  "hooks": {
    "go-fmt": {
      "event": "PostToolUse",
      "matcher": "Edit|Write",
      "command": "{{brain}}/hooks/go-fmt.sh"
    }
  }
}
```

A source's `repo` is on GitHub; `path`, optional, is where its skills are when not at the root, `skills/` or `<name>/`. A repo with skills in several folders has one entry per `path`, all at the same `sha`. A hook's `matcher` is optional, and `{{brain}}` is the Brain's path.

## Suggesting, unasked

After `nerve skill add`, read `.claude/skills/<name>/SKILL.md`. When it loads or names other skills the project lacks, such as "call the Skill tool for grilling", suggest each one below the closing blockquote, and say where it comes from: the Brain, or another folder of its source's repo, which the Brain does not list yet.

## Left to the user

- Installing `nerve` on a machine, and `nerve init <brain>`.
- Setting `syncClaudeAiSkills: false` in their user `settings.json`, if claude.ai's skills should not reach every project.
