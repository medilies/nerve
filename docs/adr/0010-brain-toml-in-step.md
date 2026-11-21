# brain.toml in step with its remote

Status: accepted, 2026-11-21. Supersedes, in [ADR 0009](0009-explicit-verbs.md), "A change to the Brain's tracked files is lost at the next sync unless it is pushed" for `brain.toml`, push force-pushing unasked, the `--discard` flag, and `update` leaving the pin to push.

## Context

`core.md` is the reference for what nerve is. It asks that the remote Brain be the source of truth, that `brain.toml` here and on the remote always agree, that every conflict fail with a diff unless `-f` is given, and that nerve never delete a folder or settings file it leaves empty.

The eighth review found the costs of a local `brain.toml`:

- `project add` said to run `nerve sync --here`. That sync dropped the new entry, then unlisted the checkout.
- A sync that could not run had already reset the Brain.
- `update` reported a pin before saving it, and left it to push by hand.

## Decision

- **A command that changes `brain.toml` keeps it in step.** `project add` and `rm`, `skill`, `agent`, `hook` and `plugin` `add` and `rm`, `setting set`, `add` and `rm`, and `update`:
  1. bring the remote's commits first, by a fast-forward;
  2. refuse a Brain with changes to `brain.toml` or commits its remote lacks, or whose remote they cannot reach, and change nothing;
  3. commit `brain.toml` alone with a message saying what changed, and push it;
  4. if the push fails, put `brain.toml` back as the remote holds it: nothing changed. When the remote moved meanwhile, the command runs again.
- **`skill add` and `agent add`** refuse an own skill or agent the remote does not hold yet, so the remote's `brain.toml` never names what the remote lacks.
- **`nerve push`** stays for the rest of the Brain: own skills, agents and hook scripts. It no longer overwrites the remote unasked: it takes the remote's commits first when the Brain has none of its own, and otherwise refuses, naming them; `-f` overwrites them. It refuses a Brain with a problem `nerve check` reports, since every machine syncs what it pushes.
- **`update`** shows each diff, pins and pushes, then syncs the listed checkout around the working directory, carrying the Brain as it is: its other changes stay.
- **`-f`, `--force`** replaces `--discard`: at `sync` it drops the Brain's work not pushed and overwrites drift, and at `push` it overwrites the remote's commits. A refused `brain.toml` command takes no `-f`: `nerve push` or `nerve sync` settles the Brain first.
- **`sync` refuses to drop** the Brain's local commits, changes to tracked files, or untracked files in the way of the remote's, naming them; `-f` drops them. A sync that cannot run, or that would refuse the checkout `--here` names for a local reason, leaves the Brain as it is.
- **nerve never deletes a folder** it made, as it never deletes `settings.local.json`.
- **The event kinds `pulled` and `pull-skipped`** become `brain-synced` and `brain-not-synced`, as GLOSSARY.md avoids "pull".

## Consequences

- A Brain needs a remote and an upstream branch before any command can change `brain.toml`.
- Offline, nothing changes `brain.toml`. A setting that goes to the local overlay still needs the remote first, since the Brain decides where it goes.
- Each change is a commit on the remote. Its history says what changed, and when.
- Two machines that push their own files no longer overwrite each other silently: the second is refused until it syncs, or pushes with `-f`.
- `update` reaches the project before anyone reads its diff. To go back, set the old `sha` in `brain.toml`, then `nerve push`.
- Folders nerve made stay after `project rm`, empty or not.
