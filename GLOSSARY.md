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
A third-party repo on GitHub, pinned to a commit, and the skills a Brain uses from it, from one or more of its folders.
_Avoid_: vendor, upstream

**Hook**:
A Claude Code hook defined once in a Brain by name, that projects take by listing it.

**Link**:
A symlink nerve makes in a project's `.claude/` to one skill folder or agent file; a link that is gone is made again at the next sync, and only `skill rm` or `agent rm` takes the item out.
_Avoid_: copy, install

**Pin**:
The exact commit a third-party skill is used at.
_Avoid_: version, lock

**Tip**:
The commit at the end of a source's default branch, where `nerve update` moves its pin.
_Avoid_: head, latest

**Project**:
An entry in a Brain naming what one codebase gets from it; nothing reaches a project it does not list.
_Avoid_: repo, workspace

**Checkout**:
A project's working tree on one machine, a git worktree included, linked to its project in the machine's own file, never in the Brain.
_Avoid_: clone, path

**Brain checkout**:
The Brain's own working tree on one machine, named in the machine file; sync makes it equal to its remote, and push sends it there; not a project's checkout.

**Manifest**:
`brain.json` at the top of a Brain: its sources, hooks, allowed settings, common settings, and each project's entry.
_Avoid_: config

**Machine file**:
`config.json` in nerve's config folder: this machine's Brain checkout and each checkout's project, never in the Brain.
_Avoid_: config, settings

**Sync**:
Bringing the remote to this machine: the Brain checkout made equal to its remote, then carried into each listed checkout; it drops the Brain's work not pushed only with `-f`.
_Avoid_: pull, install

**Push**:
Bringing this machine to the remote: everything in the Brain checkout committed, then pushed, refused when the remote has commits the Brain lacks unless `-f` overwrites them. A command that changes `brain.json` pushes that change alone, at once, after a refresh.
_Avoid_: publish, upload

**Refresh**:
Taking the remote's new commits into the Brain checkout by a fast-forward, before a command changes `brain.json`; it drops nothing, and refuses a Brain with changes to `brain.json` or commits its remote lacks.
_Avoid_: pull, fetch

**Update**:
Moving each source's pin to its tip, after showing how its skills changed, then pushing it and syncing the checkout here.
_Avoid_: upgrade, bump

**Dry run**:
`update --dry-run`: showing how each source's skills changed, from `brain.json` as this machine holds it, without moving a pin, pushing or syncing.
_Avoid_: preview

**State**:
What nerve last did to one checkout: its links, the settings it wrote, and what the file held before; kept in nerve's config folder, never in the project.
_Avoid_: cache, lock

**Exclude block**:
The marked lines nerve keeps in a repo's `info/exclude`, which hide its links from git.
_Avoid_: gitignore

**Common settings**:
Settings every listed project gets; the only thing a Brain gives to more than one project at once.
_Avoid_: base, defaults, global

**Allowed setting**:
A settings key a Brain may hold: one nerve allows on its own, or one the Brain's `allowedSettings` adds; any other stays in a local overlay.
_Avoid_: whitelist, safe setting

**Misfit**:
A value of a setting nerve allows that Claude Code would refuse there, such as text where it takes `true` or `false`; a Brain does not hold one.
_Avoid_: invalid setting, wrong type

**Session report**:
The session-start hook in every listed project: it says how the project stands against the Brain, its drift and problems, and changes nothing.
_Avoid_: autosync, auto-update

**Report**:
What a command says it did and found, one event at a time: links made or removed, settings set or revoked, drift, problems; the session report shows it to the user as one message.
_Avoid_: log, output

**Local overlay**:
A project's settings kept on one machine and never in the Brain: those the Brain does not allow.
_Avoid_: secrets file, private settings

**Drift**:
A setting, or one item of a list, that nerve wrote or the Brain sets, changed in a project since nerve last wrote it, and that the Brain disagrees with; nerve leaves it as is, fails a sync on it, and overwrites it only with `-f`.
_Avoid_: out of sync, divergence

**Personal setting**:
A setting, or one item of a list, in a project's file that the Brain does not set and nerve never wrote, such as one the project had before nerve or an approval Claude Code added; nerve keeps it and reports it once.
_Avoid_: drift, unmanaged setting
