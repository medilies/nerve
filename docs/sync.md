# Syncing

`nerve sync` carries the Brain to every checkout this machine lists;
`nerve sync --here`, to the one around the working directory, a
worktree included.

## What sync does

- **Pulls the Brain**, fast-forward only. It skips the pull while
  tracked files have uncommitted changes or the checkout has no
  upstream, and fails if the checkout has diverged.
- **Brings nerve to the Brain's pin**, then runs the new binary in its
  place. If it cannot replace its own binary, it fails and says so:
  install the pinned release by hand.
- **Refuses a checkout** where git tracks `.claude/settings.local.json`,
  or does not ignore it. Claude Code adds
  `**/.claude/settings.local.json` to your global git ignore file the
  first time it writes one; nerve never edits `.gitignore`.
- **Refuses a checkout** whose project lists a skill, agent, hook or
  plugin the Brain does not offer, as `nerve check` reports it.
- **Links** the project's skills into `.claude/skills/` and agents into
  `.claude/agents/`, with its own `nerve` skill, and keeps them out of
  git with a marked block in the repo's `info/exclude`. Own skills link
  from the Brain checkout, so edits are live; third-party skills from a
  cache at their pin.
  - A repo's worktrees share its `info/exclude`, so the block lists the
    links of all of them.
  - If the block is damaged, say its end line is gone, sync refuses
    the checkout until you fix it by hand.
- **Writes `.claude/settings.local.json`**: common settings, then the
  project's, then its plugins (`enabledPlugins`), hooks and autosync
  hook, then the local overlay. Objects merge key by key, arrays are
  joined without repeats, and anything else in a later layer wins.
- **Reports what it did**, with sensitive values shown as `(hidden)`.
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

A key changed in the project since nerve last wrote it, by `/model` or
`/config` say, that the Brain disagrees with, is drift. nerve leaves it
as it is, writes the rest, and reports it at every sync until it is
resolved. A sync by hand fails on drift; autosync only reports it.

A list is taken item by item. An approval Claude Code adds to
`permissions.allow` is drift of that one item, and the Brain's other
changes to the list still arrive.

To resolve drift:

- Move a value into the Brain with `nerve setting set <key> <value>`,
  or one item of a list with `nerve setting add <key> <item>`.
- Drop what drifted with `nerve sync --here --discard`.

## What leaves the Brain leaves the project

- A setting, skill or agent taken out of a project, or out of every
  project with `rm --all`, is removed from each checkout at its next
  sync. A key changed there since is drift, and kept.
- Deleting a skill's or agent's link in `.claude/` takes it out of the
  project in `brain.toml`, at the next sync, which says to commit it.
  A link you point elsewhere becomes yours: nerve leaves it alone.
- A project dropped from the Brain, by `nerve project rm` here or on
  another machine, is cleaned out of its checkouts: nerve's links, its
  exclude block, and the settings and list items it wrote that were not
  changed since.

## The team's own

nerve never touches what it did not make. Where the team already has a
skill or agent of the same name, nerve links yours as `<owner>-<name>`,
the owner taken from the Brain's git remote. `nerve skill ls` shows
yours and the team's.

## claude.ai

A project with `surface = "claude.ai"` stands for an account. claude.ai
has no API for skills, so `nerve sync` builds a zip per skill in
`claude.ai/<project>/` under nerve's [cache folder](brain.md#this-machine),
and says which to upload, once each time one changes, and which skills
to delete on the account. A skill it cannot build is reported, and
never marked for deletion.

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
- `nerve-checksums` guards against a release replaced after you pinned
  it: `curl -sL https://github.com/medilies/nerve/releases/download/<tag>/checksums.txt | sha256sum`.
