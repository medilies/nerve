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
- **Verified claude.ai state.** Read what an account actually holds, rather than reporting which zips changed.
- **Sweeping a killed run's temp files.** A run killed mid-write leaves `.<file>.<random>` beside the file, such as one in the Brain that git shows as untracked. A sweep needs a name only nerve makes, so it never deletes a file of the user's.
- **Picking a source skill by folder.** A source with two folders of the skill's name, such as `skills/x` and `examples/x`, is refused as ambiguous. A `path` in `[[source]]` could pick one.
- **Autosync in `nerve hook ls`.** List nerve's own hook beside the Brain's, marked as nerve's.
- **Agents in the same session.** Check whether an agent linked by autosync is usable in the same session, as skills are with `reloadSkills`.
