# Projects opt in; nothing is carried everywhere

Status: accepted, 2026-11-20; deleting a link and a name the team has superseded by [ADR 0007](0007-drop-guessing-features.md), and claude.ai as a special project by [ADR 0008](0008-drop-claude-ai-accounts.md); adding a project that already has settings amended 2026-11-21, to import nothing and keep them as personal settings, and keeping sensitive values out, to an allow-list. Supersedes [ADR 0004](0004-skills-as-plugins.md), and the user-level carrying of [ADR 0003](0003-generated-layered-settings.md); autosync, and keeping sensitive values out but for the allow-list, superseded by [ADR 0009](0009-explicit-verbs.md).

## Context

nerve carried the Brain at user level, so every skill, plugin, hook and denied tool reached every project. In practice:

- A skill is almost never wanted in every project. Most belong to one stack or one kind of work.
- In a team project, the team decides what is committed in `.claude/`, and teammates do not all want the same personal skills.
- What is carried at user level is hard to see, and hard for a project to undo. Deny rules from every level are combined, and a deny beats a project's allow. Hooks from every level all run. A personal skill shadows a project skill with the same name, and a plugin skill sits beside it as a near-duplicate.

Two rules follow:

- **Only where needed.** Send skills, settings and plugins only where they are needed, so context is not bloated and needed tools are not denied. Make every effect visible, explicit and hard to miss.
- **Nothing sensitive in the Brain.** No secrets, no paths, no repo remotes, no client or employer names, no account emails.

## Options

- **Keep user level, let projects opt out.** Every project pays for everything until someone notices. This breaks the first rule by default.
- **The team's committed `.claude/`.** Right for what the whole team wants, but it is the team's decision, and it forces one person's choices on everyone.
- **Per person, per project.** `.claude/settings.local.json` and links in `.claude/`, both kept out of git, recorded in the Brain.

## Decision

### The Brain lists projects

- `brain.toml` is the source of truth. Each project entry has a name you choose, and lists its skills, agents, hooks, plugins and settings. A project's files only reflect `brain.toml`.
- `common` holds settings only, applied to every listed project. Skills, agents, hooks and plugins reach a project only when its entry names them.
- A project is named, never located. This machine's file maps each checkout to a project name, and `nerve project add <name>` makes that link once per checkout. No remote or path enters the Brain.

### What nerve writes in a project

nerve writes only in listed projects, and nothing at user level:

- `.claude/settings.local.json`, built from `common`, then the project entry, then the project's local overlay. Plugins and hooks reach the project as entries in this file.
  - nerve refuses to write it while git tracks it or does not ignore it, and never edits `.gitignore`.
  - Claude Code adds `**/.claude/settings.local.json` to the global git excludes file the first time it writes the file itself. A file nerve creates gets no such rule.
- One link per skill in `.claude/skills/` and per agent in `.claude/agents/`, including the nerve skill, kept out of git through a marked block in the repo's `info/exclude`.
  - Own skills and agents link from the Brain checkout, so edits are live.
  - Third-party skills link from a cache at their pinned commit, and `nerve skill update` moves a pin after showing the upstream diff.
- Deleting a link removes that skill or agent from the project's entry. It is the one change that flows from a project back to `brain.toml`. Every other change in `settings.local.json` is drift, as in ADR 0003.
  - Links that are all gone at once, or gone with their folder, as after `git clean -x`, were not deleted one by one: nerve links them again and says so.
- When one of your skills or agents has the same name as one the team committed, nerve keeps the team's and links yours as `<owner>-<name>`, where `<owner>` comes from the Brain's git remote at sync time and is never stored. The link's folder name is the name you invoke it by.

### Autosync is explicit

- `autosync` is a key in `brain.toml`, on by default, and a project entry can turn it off. When it is on, nerve adds a `SessionStart` hook to the project's `settings.local.json` that runs `nerve sync --here --hook`, which prints its report as the hook's output. On is the default because the source is a git repo the user committed to: the hook only applies what was already reviewed.
- Every run hands the user a report as the hook's `systemMessage`. That is shown to the user and does not enter Claude's context. It says:
  - what changed (links made or removed, settings added, changed or revoked);
  - what was held back as drift;
  - whether the pull was skipped.

  When nothing changed, it is one line naming the Brain commit the project is at, and why the pull was skipped, if it was.
- Settings the hook writes take effect in the same session, and so do skills it links: the hook asks Claude Code to rescan them (`reloadSkills`).
- Autosync never resolves drift: it is the CLI, run without a person. It leaves drifted keys as they are and reports them. The nerve skill resolves drift with the user.
- `/nerve` on its own changes nothing. It summarizes the current project's state, autosync included, or says what it can do.

### Revoking

- nerve records what it last wrote in each checkout. A key or array item that leaves `common` or a project entry is removed from that checkout's file at its next sync, unless it was changed there since, which is reported as drift. A skill or agent that leaves an entry loses its link.
- `rm --all` (`nerve setting rm <key>[=<value>] --all`, `nerve skill rm <name> --all`, and so on) removes it from every entry at once, and lists each project affected, so no project keeps it through an override.
- `nerve project rm <name>` takes nerve's links, exclude block, hook and keys out of a checkout and drops the entry.
- A revoke reaches a checkout at its next sync: its next session start with autosync on, or a manual `nerve sync` with it off. `/nerve` shows a checkout that is behind the Brain.

### claude.ai is a special project

```toml
[project.claude-ai-work]
surface = "claude.ai"
skills = ["research"]
```

- It takes skills only. One entry per account, named by a label, never by email.
- `sync` builds a zip per skill into nerve's cache, never into the Brain, and reports which zips changed since it last built them. Uploading and deleting on claude.ai stay manual, because there is no API for it.
- nerve does not read skills back from claude.ai. Claude Code downloads the account's skills into every session signed into it, at user level. Claude Code's `syncClaudeAiSkills: false` turns that off; `common` can carry it to listed projects.

### Adding a project that already has settings

`nerve project add` adds an empty entry, and records nothing of the checkout's existing `settings.local.json`. At the first sync, each existing key the Brain does not set is a personal setting: kept, reported once, and never removed, `--discard` included. A key the Brain sets to another value is drift. The user moves a personal setting into the Brain with `nerve setting set`, which sends anything sensitive to the overlay. `nerve project rm` strips only what nerve wrote, so these keys stay in the file.

A setting the Brain has no opinion on is not drift: the Brain does not disagree with it. Taking it as drift failed every sync by hand until the user moved or discarded it, and `--discard` deleted a value nerve never wrote.

An earlier version imported the file: it sorted each key into the entry, the overlay or nothing, against `common`. That guessed where each key belonged, could carry a value into the Brain unseen, and moved secrets into an overlay that `project rm` then deleted. The idea waits in the [backlog](../backlog.md).

### Keeping sensitive values out

- A Brain holds only allowed settings: a short list nerve allows, of settings that shape how a project works, and the keys `allowed-settings` in `brain.toml` adds. A key that may hold secrets (`env`, `headers`, `apiKeyHelper` and the like) is never allowed, at any depth. An allow-list fails closed: a new or unknown key, such as an MCP server's `env`, stays out until someone allows it, where a list of secret keys let it in until someone noticed.
- `check` and `sync` refuse any other setting, and absolute home paths, email addresses and token-like strings anywhere in `brain.toml`. `{{brain}}` stands in for the checkout path.
- Every value about to enter the Brain, from moving drift or from a request, goes through the same guard. The nerve skill shows it to the user, and anything identifying goes to the project's local overlay. That covers values a pattern cannot catch, such as a client host in a permission rule.

## Consequences

- No skill, agent, hook, plugin or setting from the Brain reaches a project that is not listed. Listing a project, or anything in it, is a reviewed commit.
- nerve no longer writes user-level settings, standing instructions or keybindings. Profiles go.
- A new machine links each checkout once with `nerve project add <name>`, since the Brain does not say where projects live.
- Local overlays stay on one machine. A sensitive value has to be set again on each machine.
- The plugin marketplace layout goes, and skills are invoked by name again (`/go-stack`).
- Claude Code writes approvals ("don't ask again") and project-local `/config` choices into `settings.local.json`. An approval is a personal setting, and a `/config` choice is drift when the Brain sets that key.
- The CLI is grouped by noun, like Docker's: `nerve project`, `skill`, `agent`, `hook`, `plugin` and `setting`.
