# nerve

A Brain, and the Nervous system that carries it to each place an agent runs.

## Language

**Brain**:
One person's repo of everything that shapes how their agents work: own skills and agents, third-party pins, hooks, settings, and the projects that use them.
_Avoid_: config, dotfiles, kit, agentic brain

**Nervous system**:
The tool and its skill that carry any Brain to the projects that list what they need from it, and change it through their commands; it knows nothing about whose Brain it carries.
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

**Marketplace**:
Where a Claude Code plugin comes from; a plugin is named `<plugin>@<marketplace>`.
_Avoid_: registry, store

**Source**:
A third-party repo on GitHub, pinned to a commit, and the skills a Brain uses from it.
_Avoid_: vendor, upstream

**Hook**:
A Claude Code hook defined once in a Brain by name, that projects take by listing it.

**Link**:
A symlink nerve makes in a project's `.claude/` to one skill folder or agent file; a link that is gone is made again at the next sync, and only `skill rm` or `agent rm` takes the item out.
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

**Brain checkout**:
The Brain's own working tree on one machine, named in the machine file, from which nerve reads and pulls; not a project's checkout.

**Machine file**:
`config.toml` in nerve's config folder: this machine's Brain checkout and each checkout's project, never in the Brain.
_Avoid_: config, settings

**Working tree**:
Where nerve writes in a project: a checkout, or a git worktree of one, which shares the checkout's project and `info/exclude`.
_Avoid_: workspace

**Common settings**:
Settings every listed project gets; the only thing a Brain gives to more than one project at once.
_Avoid_: base, defaults, global

**Allowed setting**:
A settings key a Brain may hold: one nerve allows on its own, or one the Brain's `allowed-settings` adds; any other stays in a local overlay, and a key that may hold secrets is never allowed.
_Avoid_: whitelist, safe setting

**Surface**:
Where a project is used: a Claude Code checkout, or a claude.ai account.
_Avoid_: target, platform

**Autosync**:
The session-start hook that brings a project in step with its Brain and hands the user a report.
_Avoid_: auto-update

**Report**:
What a command says it did and found, one event at a time: links made or removed, settings set or revoked, drift, problems; autosync shows it to the user as one message.
_Avoid_: log, output

**Local overlay**:
A project's settings kept on one machine and never in the Brain, for anything sensitive or identifying, or that `brain.toml` cannot hold.
_Avoid_: secrets file, private settings

**Drift**:
A setting, or one item of a list, that nerve wrote or the Brain sets, changed in a project since nerve last wrote it, and that the Brain disagrees with; nerve leaves it as is and reports it at every sync.
_Avoid_: out of sync, divergence

**Personal setting**:
A setting, or one item of a list, in a project's file that the Brain does not set and nerve never wrote, such as one the project had before nerve or an approval Claude Code added; nerve keeps it and reports it once.
_Avoid_: drift, unmanaged setting
