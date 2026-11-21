# Syncing

nerve changes a project only when you tell it to. Each direction has
one command:

- `nerve sync` brings the remote to this machine: the Brain, then every
  checkout this machine lists. `nerve sync --here` does the one around
  the working directory.
- `nerve push` brings this machine to the remote.
- `nerve project add` lists the checkout around the working directory,
  then syncs it.
- A command that changes `brain.json`, such as `nerve skill add`, keeps
  it in step: see [Push](#push).
- At each session start, the [session report](#the-session-report) says
  what waits for a sync. It changes nothing.

## What sync does

- **Makes the Brain its remote's.** It fetches, then resets the Brain's
  branch and tracked files to its upstream. Untracked files stay.
  - It refuses when that would drop local commits, changes to tracked
    files, or untracked files where the remote has one, naming a few of
    each. Push them first, or drop them with `nerve sync -f`.
  - It skips this, and says why, when the Brain is not a git repo, or has
    no upstream or a detached HEAD, or its remote cannot be reached or no
    longer has the branch. It then carries the Brain as it is.
  - It never asks for a password: a remote that needs one is skipped.
  - A sync that cannot run, such as `--here` outside a listed checkout,
    or that would refuse the checkout `--here` names, leaves the Brain
    as it is.
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
  wins. Personal settings stay, as [below](#personal-settings). nerve
  writes the file in its own form: keys sorted, and numbers as JSON
  writes them. A file nerve makes is readable by you only, since the
  overlay may hold secrets.
- **Unlists a checkout** that is gone, on a full sync, or whose project
  left the Brain.
- **Reports what it did.**

A worktree is a checkout like any other: it gets nerve once
`nerve project add` lists it.

## Push

A command that changes `brain.json` pushes it at once: `project add`
and `rm`, `skill`, `agent`, `hook` and `plugin` `add` and `rm`,
`setting set`, `add` and `rm`, and `update`.

- It first takes the remote's new commits, so it changes `brain.json` as
  the remote holds it.
- It refuses, and changes nothing, when:
  - `brain.json` has changes, which it shows, or the Brain has commits
    its remote lacks;
  - the remote's commits change files changed here, which it names;
  - the remote cannot be reached, or does not have the branch yet:
    `nerve push` makes it there.
- `skill add` and `agent add` refuse an own skill or agent the remote
  does not hold yet: push it first.
- It commits `brain.json` alone, with a message saying what changed. If
  the push fails, it changes nothing; when the remote moved meanwhile, it
  says so: run it again.

`nerve push [message]` sends the rest, such as a new own skill: it
commits everything in the Brain checkout, then pushes it to its
upstream.

- A change needs a message, as in `nerve push "add go-stack"`.
- It refuses a Brain with a problem `nerve check` reports, since every
  machine syncs what it pushes.
- It takes the remote's commits first when this Brain has none of its
  own. When it has some too, it refuses, naming the remote's:
  `nerve sync -f` takes them, dropping what was not pushed here;
  `nerve push -f` overwrites them.

## The session report

Every listed project's `settings.local.json` holds a `SessionStart`
hook that runs `nerve status --hook`. Each session in the project starts
with a short report: what the Brain has not pushed, how the checkout
stands against the Brain, how many changes wait for `nerve sync`, drift,
and problems.

- It changes nothing, and reads only this machine: commits on the
  remote count once a sync brings them.
- The report reaches you, not Claude's context.
- It never fails the session: an error is part of the report.

## Personal settings

A setting the Brain does not set, and nerve never wrote, is personal:
one the checkout had before nerve, say, or an approval Claude Code
adds to `permissions.allow`. nerve keeps it, reports it the first time
it sees it, and never removes it while the Brain does not set it, `-f`
and `project rm` included. Once the Brain sets the key, it is the
Brain's, as below. To share it, move it into the Brain with `nerve setting set`
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
and reports it at every sync and session start. A sync fails on drift,
naming each value here and in the Brain.

A list is taken item by item: an item of the Brain's removed in the
checkout is drift of that one item, and the Brain's other changes to
the list still arrive.

An empty object, such as `"permissions": {}`, holds no setting, so it
never drifts. A key the project holds as a value, and the Brain as an
object, or the other way round, drifts as a whole.

To resolve drift:

- Move a value into the Brain with `nerve setting set <key> <value>`,
  or one item of a list with `nerve setting add <key> <item>`.
- Overwrite all the checkout's drift with `nerve sync --here -f`, or
  every checkout's with `nerve sync -f`. Personal settings stay.

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
  exclude block, and the settings and list items it wrote that were not
  changed since. Folders stay, even empty, and so does
  `settings.local.json`, if only as `{}`: nerve never deletes either.

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
