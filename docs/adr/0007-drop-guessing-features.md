# Drop write-back, owner-prefixed links and self-upgrade

Status: accepted, 2026-11-21. Supersedes, in [ADR 0005](0005-projects-opt-in.md), the bullets on deleting a link and on a name the team has.

## Context

Three reviews of the projects branch found about thirty bugs in a tool of some five thousand lines. Most came from three features:

- **Write-back.** Deleting a skill's or agent's link took the item out of the project in `brain.toml`. nerve had to guess intent from what was gone: a guard against mass loss, relinking after `git clean`, links moved with a checkout, links pointed elsewhere.
- **Owner-prefixed links.** Where the team had a skill or agent of the same name, nerve linked yours as `<owner>-<name>`, with the owner read from the Brain's git remote. A link you pointed elsewhere got a second, prefixed copy.
- **Self-upgrade.** On sync, nerve downloaded the release the Brain pins, checked it against `checksums.txt` and `nerve-checksums`, replaced itself and ran again. Inside the session-start hook, a download could outlast Claude Code's hook timeout.

Each saved a step that is already one command.

## Decision

- **Only `skill rm` and `agent rm` take an item out of a project.** A link that is gone is made again at the next sync. `brain.toml` changes only through commands, and every change in a project other than to its settings is undone or left alone, never written back.
- **A name the team has is not linked.** nerve skips that skill or agent in that checkout and reports a problem: rename yours in the Brain to use it there. A link that already points where nerve would point it, as in a checkout that moved, is nerve's.
- **nerve never replaces itself.** The `nerve` pin stays, and stays reviewed as [ADR 0002](0002-brain-and-nervous-system.md) asks. When a release other than the pin runs, sync reports it, and the user installs the pin. `nerve-checksums` goes.

## Consequences

- No intent is guessed from links: the mass-loss guard, `wrote-back` and `relinked` go.
- No owner is read from the Brain's remote.
- Sync makes no download of its own, so a hook run is bounded by the Brain's pull and the first fetch of a third-party skill.
- A team that clashes with one of your names costs a rename in the Brain.
- A Brain whose `brain.toml` sets `nerve-checksums` must drop the key, since nerve refuses keys it does not know.
