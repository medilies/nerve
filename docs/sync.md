# Syncing

`nerve sync` carries the Brain to every checkout this machine lists,
and to the worktrees of them it reached before; `nerve sync --here`, to
the one around the working directory, a worktree included.

## What sync does

- **Pulls the Brain**, fast-forward only. It skips the pull, and says
  why, while tracked files have uncommitted changes, the checkout has
  no upstream or a detached HEAD, or its remote cannot be reached. It
  fails if the checkout has diverged.
  - It never asks for a password: a remote that needs one is skipped.
- **Checks nerve against the Brain's pin**, and reports a release that
  differs: install the pinned one. It never replaces itself.
- **Refuses a checkout** where git tracks `.claude/settings.local.json`,
  or does not ignore it: add `**/.claude/settings.local.json` to your
  global git ignore file. nerve never edits `.gitignore`.
- **Refuses a checkout** where `.claude`, `.claude/skills`,
  `.claude/agents` or `.claude/settings.local.json` is a link, since
  nerve would write outside it. `nerve project rm` refuses it too.
- **Refuses a checkout** whose project lists a skill, agent, hook or
  plugin the Brain does not offer, as `nerve check` reports it.
- **Links** the project's skills into `.claude/skills/` and agents into
  `.claude/agents/`, with its own `nerve` skill, and keeps them out of
  git with a marked block in the repo's `info/exclude`. Own skills link
  from the Brain checkout, so edits are live; third-party skills from a
  cache at their pin.
  - A third-party skill that cannot be fetched, say offline, is
    reported and keeps the link it had. The rest of the project syncs.
  - A repo's worktrees share its `info/exclude`, so the block lists the
    links of all of them.
  - If the block is damaged, say its end line is gone, sync refuses
    the checkout, before it links anything, until you fix it by hand.
- **Writes `.claude/settings.local.json`**: common settings, then the
  project's, then its plugins (`enabledPlugins`), hooks and autosync
  hook, then the local overlay. Objects merge key by key, arrays are
  joined without repeats, and anything else in a later layer wins.
- **Unlists a checkout that is gone**, on a full sync, and forgets a
  worktree that is. A worktree git cannot locate is left as it is, and
  reported, while its project is in the Brain.
- **Reports what it did**, with secrets and emails shown as `(hidden)`.
- **Builds the claude.ai zips**, on a full sync only. See
  [below](#claudeai).

## Autosync

With autosync on, the project's `settings.local.json` holds a
`SessionStart` hook that runs `nerve sync --here --hook`. Each session
in the project then starts in step with the Brain, and hands you a
report: what it linked or removed, which settings it set or revoked,
what it held back as drift, and whether it skipped the pull. When
nothing changed, it is one line, which also says why the pull was
skipped, if it was.

- The report reaches you, not Claude's context.
- When it links a skill, Claude Code rescans skills, so the skill is
  usable in that session.
- It never fails the session: an error is part of the report.
- `nerve project autosync off`, or `--all` for the Brain's default,
  takes the hook out at the next sync.

## Drift

A key the Brain disagrees with, that the checkout had before nerve
first wrote it, or that changed since, by `/model` or `/config` say, is
drift. nerve leaves it
as it is, writes the rest, and reports it at every sync until it is
resolved. A sync by hand fails on drift; autosync only reports it.

A list is taken item by item. An approval Claude Code adds to
`permissions.allow` is drift of that one item, and the Brain's other
changes to the list still arrive.

An empty object, such as `"permissions": {}`, holds no setting, so it
never drifts. A key the project holds as a value, and the Brain as an
object, or the other way round, drifts as a whole.

To resolve drift:

- Move a value into the Brain with `nerve setting set <key> <value>`,
  or one item of a list with `nerve setting add <key> <item>`.
- Drop what drifted with `nerve sync --here --discard`.

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

## The team's own

nerve never touches what it did not make. Where the team already has a
skill or agent of the same name, nerve does not link yours there, and
reports a problem: rename yours in the Brain to use it in that project.
`nerve skill ls` shows nerve's links, the team's skills, and links you
made or pointed elsewhere, as yours.

## claude.ai

A project with `surface = "claude.ai"` stands for an account. claude.ai
has no API for skills, so `nerve sync` builds a zip per skill in
`claude.ai/<project>/` under nerve's [cache folder](brain.md#this-machine),
and says which to upload, once each time one changes, and which skills
to delete on the account. A skill it cannot build is reported, and
never marked for deletion. A link in a skill is zipped as the file it
points to in the skill; one to a folder, to nothing, or out of the
skill is refused, so nothing outside the skill reaches a zip.

Skills on claude.ai reach every Claude Code session signed into that
account. `syncClaudeAiSkills: false`, in your user settings or
`common`, stops that.

## Notes

- Third-party skills and the nerve skill link into nerve's cache
  folder. If the system clears that cache, those links break until
  the next sync.
- The exclude block also hides from `git add` a new folder of the same
  name, in every worktree of the repo. Files the team already committed
  are not affected.
