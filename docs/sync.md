# Syncing

nerve changes a project only when you tell it to. Each direction has
one command:

- `nerve sync` brings the remote to this machine: the Brain, then every
  checkout this machine lists. `nerve sync --here` does the one around
  the working directory.
- `nerve push` brings this machine to the remote.
- At each session start, the [session report](#the-session-report) says
  what waits for a sync. It changes nothing.

## What sync does

- **Makes the Brain its remote's.** It fetches, then resets the Brain's
  branch and tracked files to its upstream, and says which local commits
  and changes it dropped. Untracked files stay, but for one where the
  remote has a file. Push first what you want to keep.
  - It skips this, and says why, when the Brain has no upstream or a
    detached HEAD, or its remote cannot be reached or no longer has the
    branch. It then carries the Brain as it is.
  - It never asks for a password: a remote that needs one is skipped.
- **Refuses a checkout** where git tracks `.claude/settings.local.json`,
  or does not ignore it: add `**/.claude/settings.local.json` to your
  global git ignore file. nerve never edits `.gitignore`.
- **Refuses a checkout** where `.claude`, `.claude/skills`,
  `.claude/agents` or `.claude/settings.local.json` is a link, or the
  repo's `info/exclude` or its folder, since nerve would write outside
  it. `nerve project rm` refuses it too.
- **Refuses a checkout** whose project lists a skill, agent or hook the
  Brain does not offer, a skill it offers both as its own and from a
  source, or a plugin ID that is not `<plugin>@<marketplace>`, as
  `nerve check` reports it.
- **Links** the project's skills into `.claude/skills/` and agents into
  `.claude/agents/`, with its own `nerve` skill, and keeps them out of
  git with a marked block in the repo's `info/exclude`. Own skills link
  from the Brain checkout, so edits are live; third-party skills from a
  cache at their pin.
  - A third-party skill that cannot be fetched, say offline, is
    reported and keeps the link it had. The rest of the project syncs.
  - If the block is damaged, say its end line is gone, sync refuses
    the checkout, before it links anything, until you fix it by hand.
- **Writes `.claude/settings.local.json`**: common settings, then the
  project's, then its plugins (`enabledPlugins`), hooks and the session
  report's hook, then the local overlay. Objects merge key by key,
  arrays are joined without repeats, and anything else in a later layer
  wins. Personal settings stay, as [below](#personal-settings). A file
  nerve makes is readable by you only, since the overlay may hold
  secrets.
- **Unlists a checkout that is gone**, on a full sync.
- **Reports what it did.**

A worktree is a checkout like any other: it gets nerve once
`nerve project add` lists it.

## Push

`nerve push [message]` commits everything in the Brain checkout, then
force-pushes it to its upstream.

- A change needs a message, as in `nerve push "add go-stack"`.
- It refuses a Brain that holds a setting no list allows, as `sync`
  does.
- The remote's commits this machine lacks are overwritten: sync first
  on a machine that may be behind.

## The session report

Every listed project's `settings.local.json` holds a `SessionStart`
hook that runs `nerve status --hook`. Each session in the project starts
with a short report: how the checkout stands against the Brain, how many
changes wait for `nerve sync`, drift, and problems.

- It changes nothing, and reads only this machine: commits on the
  remote count once a sync brings them.
- The report reaches you, not Claude's context.
- It never fails the session: an error is part of the report.

## Personal settings

A setting the Brain does not set, and nerve never wrote, is personal:
one the checkout had before nerve, say, or an approval Claude Code
adds to `permissions.allow`. nerve keeps it, reports it the first time
it sees it, and never removes it, `--discard` and `project rm`
included. To share it, move it into the Brain with `nerve setting set`
or `nerve setting add`.

A setting the checkout had before nerve first wrote its file, that the
Brain sets the same, is the Brain's from then on, but `project rm`
leaves it while it holds that value. A checkout that moved holds
nerve's links: there, every setting as the Brain sets it is nerve's. So
is a checkout whose state nerve cannot read: sync reports it, and starts
the state again.

## Drift

A setting nerve wrote or the Brain sets, changed in the checkout
since nerve last wrote it, by `/model` or `/config` say, that the Brain
disagrees with, is drift. nerve leaves it as it is, writes the rest,
and reports it at every sync and session start. A sync fails on drift.

A list is taken item by item: an item of the Brain's removed in the
checkout is drift of that one item, and the Brain's other changes to
the list still arrive.

An empty object, such as `"permissions": {}`, holds no setting, so it
never drifts. A key the project holds as a value, and the Brain as an
object, or the other way round, drifts as a whole.

To resolve drift:

- Move a value into the Brain with `nerve setting set <key> <value>`,
  or one item of a list with `nerve setting add <key> <item>`.
- Drop all the checkout's drift with `nerve sync --here --discard`.
  Personal settings stay.

## What leaves the Brain leaves the project

- A setting, skill or agent taken out of a project, or out of every
  project with `rm --all`, is removed from each checkout at its next
  sync. A key changed there since is drift, and kept.
- Only `skill rm` and `agent rm` take a skill or agent out of a
  project. A link that is gone, as after `git clean -x`, is made again
  at the next sync, and so is a settings file that is gone.
- A link you point elsewhere becomes yours: nerve leaves it alone, and
  does not link that item there.
- A link that already points where nerve would point it, as in a
  checkout that moved, becomes nerve's again.
- A project dropped from the Brain, by `nerve project rm` here or on
  another machine, is cleaned out of its checkouts: nerve's links, its
  exclude block, the settings and list items it wrote that were not
  changed since, and the folders it made that are left empty.
  `settings.local.json` stays, if only as `{}`: nerve never deletes it.

## The team's own

nerve never touches what it did not make. Where the team already has a
skill or agent of the same name, nerve does not link yours there, and
reports a problem: rename yours in the Brain to use it in that project.
`nerve status` shows nerve's links, the team's skills, and links you
made or pointed elsewhere, as yours.

## Notes

- Third-party skills and the nerve skill link into nerve's cache
  folder. If the system clears that cache, those links break until
  the next sync.
- The exclude block also hides from `git add` a new folder of the same
  name, in every worktree of the repo. Files the team already committed
  are not affected.
- Skills you upload to claude.ai reach every Claude Code session signed
  into that account. `syncClaudeAiSkills: false`, in your user settings
  or `common`, stops that.
