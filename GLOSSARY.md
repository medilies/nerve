# nerve

A Brain, and the Nervous system that carries it to each place an agent runs.

## Language

**Brain**:
One person's repo of everything that shapes how their agents work: own skills and agents, third-party pins, hooks, settings, and the projects that use them.
_Avoid_: config, dotfiles, kit, agentic brain

**Nervous system**:
The tool and its skill that carry any Brain to the projects that list what they need from it, and write changes back into it; it knows nothing about whose Brain it carries.
_Avoid_: sync tool, sync script, engine

**Skill**:
A folder of instructions (`SKILL.md` plus optional files) that an agent loads when a task matches its description.

**Own skill**:
A skill whose content is written and kept in a Brain.
_Avoid_: my skill, custom skill, local skill

**Third-party skill**:
A skill written in someone else's repo and used in a Brain at a pinned commit.
_Avoid_: external skill, vendored skill

**Agent**:
A subagent definition (`agents/<name>.md`) kept in a Brain and linked into the projects that list it.

**Plugin**:
A Claude Code plugin from a marketplace, turned on in the projects that list it.
_Avoid_: package, bundle

**Source**:
A third-party repo on GitHub, pinned to a commit, and the skills a Brain uses from it.
_Avoid_: vendor, upstream

**Hook**:
A Claude Code hook defined once in a Brain by name, that projects take by listing it.

**Link**:
A symlink nerve makes in a project's `.claude/` to one skill folder or agent file; deleting it takes the item out of the project.
_Avoid_: copy, install

**Pin**:
The exact commit a third-party skill, or the Nervous system itself, is used at.
_Avoid_: version, lock

**Project**:
An entry in a Brain naming what one codebase, or one claude.ai account, gets from it; nothing reaches a project it does not list.
_Avoid_: repo, workspace

**Checkout**:
A project's working tree on one machine, linked to its project in the machine's own file, never in the Brain.
_Avoid_: clone, path

**Working tree**:
Where nerve writes in a project: a checkout, or a git worktree of one, which shares the checkout's project and `info/exclude`.
_Avoid_: workspace

**Common settings**:
Settings every listed project gets; the only thing a Brain gives to more than one project at once.
_Avoid_: base, defaults, global

**Surface**:
Where a project is used: a Claude Code checkout, or a claude.ai account.
_Avoid_: target, platform

**Autosync**:
The session-start hook that brings a project in step with its Brain and hands the user a report.
_Avoid_: auto-update

**Local overlay**:
A project's settings kept on one machine and never in the Brain, for anything sensitive or identifying.
_Avoid_: secrets file, private settings

**Drift**:
A setting, or one item of a list, changed in a project since nerve last wrote it, that the Brain disagrees with; nerve leaves it as is and reports it.
_Avoid_: out of sync, divergence
