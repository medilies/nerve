# Backlog

Ideas left out of [ADR 0005](adr/0005-projects-opt-in.md) to keep it minimal. Take one up only when it is needed.

- **Bundles.** Named sets of skills, agents, hooks, plugins and settings that a project includes, such as `go`, so Go projects do not repeat the same list.
- **Pinned plugin marketplaces.** Pin a plugin's marketplace to a commit the way skills are pinned, so plugin updates are reviewed too.
- **Third-party agents.** Agents from other repos at a pinned commit, like third-party skills.
- **Per-project standing instructions.** Instructions carried to one project, without editing its `CLAUDE.md`.
- **User-level opt-in.** Carrying a few things to every config again (keybindings, a status line), explicitly listed.
- **`nerve skill new`.** Create an own skill from the CLI. Until then, the nerve skill writes it.
- **Deleting from the Brain.** A stronger verb than `rm` (such as `wipe` or `prune`) that deletes an own skill or agent, or a third-party source, refused while a project uses it. Until then, delete the folder or entry by hand.
- **Importing a checkout's settings.** `nerve project add` records the settings the checkout already has, so the user need not move each drifted key by hand. The challenges, which made the first version go:
  - Each key must go to the right place: the project, `common`, the overlay, or nowhere because `common` gives it. Lists that share items with `common` have to be split.
  - Nothing may reach the Brain unseen: each key needs the same checks, and the same review by the user, as `setting set`.
  - nerve's own hook, written by an earlier sync, must not be imported.
  - What moves to the overlay must come back to the file when the project is removed, or a secret is lost.
- **A drift resolver.** Open drift in a diff tool to move each key into the project, `common` or the overlay, rather than going through it with the nerve skill.
- **claude.ai accounts.** A project that stands for a claude.ai account and takes skills only: sync zips each one, and says which to upload and which to delete. Worth it once claude.ai has an API for skills. The challenges, which made the first version go ([ADR 0008](adr/0008-drop-claude-ai-accounts.md)):
  - With no API, uploading and deleting stay manual, and nerve cannot see what an account holds. A missed "upload" leaves the account and nerve apart, with no sign of it.
  - A skill on claude.ai reaches every Claude Code session signed into the account, against "only where needed". `syncClaudeAiSkills: false` has to turn that off.
  - It is a second kind of project, with no sessions: no session report, settings, agents, hooks or plugins. Every use case has to tell it apart.
  - A zip must hold the skill and nothing else: no link out of it or to a folder, no dot file such as `.env`, and no file that is not a regular one, such as a pipe, whose read never ends. The same files must give the same bytes, to tell a change.
  - claude.ai refuses frontmatter keys other than `name`, `description`, `license`, `compatibility`, `metadata` and `allowed-tools`.
  - The zip of a skill taken out must go, and one that cannot be removed must stay known, or it is uploaded again by mistake.
  - The project names the account, never by its email.
- **Sweeping a killed run's temp files.** A run killed mid-write leaves `.<file>.<random>` beside the file, such as one in the Brain that git shows as untracked. A sweep needs a name only nerve makes, so it never deletes a file of the user's.
- **The remote in the session report.** Fetch at session start, to say when the Brain's remote is ahead. Today the report reads only this machine.
- **Reading an update before it lands.** `nerve update` pushes the new pin and syncs at once ([ADR 0010](adr/0010-brain-toml-in-step.md)). A dry run would show the diff first, for a source the user does not trust.
- **Checking a hook's event.** Refuse a `[hook.<name>]` whose event Claude Code does not have. A list kept in nerve goes stale as Claude Code adds events, and would then refuse a valid one.
- **Checking git's version.** Say "nerve needs git 2.31" rather than show git's own error on an older git.
