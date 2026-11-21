# Explicit verbs: nerve acts only when told

Status: accepted, 2026-11-21. Supersedes, in [ADR 0005](0005-projects-opt-in.md), "Autosync is explicit" and "Keeping sensitive values out" but for the allow-list; the `nerve` pin of [ADR 0002](0002-brain-and-nervous-system.md) and [ADR 0007](0007-drop-guessing-features.md).

## Context

The core is simple: a Brain, carried only into the projects that list what they need. Around it the branch grew guards and automation, and the seventh review found most of its bugs there:

- **An implicit pull.** Every sync, each session start included, pulled the Brain. It had eight cases to skip or fail on, and still failed where git would not fast-forward.
- **Autosync wrote at each session start.** A session could link, write settings or pull before anyone asked.
- **Secret scanning.** Patterns for tokens, emails, home paths and keys named like secrets. They missed real secrets, such as `curl -u user:pass`, refused harmless values, and hid values in reports.
- **Features beside the core.** The release pin, the autosync switch, the `ls` commands, tracking worktrees a sync reached, and a refusal of trees whose state nerve cannot read.

## Decision

nerve does nothing on its own. It acts when a command says so, and otherwise only reports. Each direction has one verb:

- **`nerve sync`** brings the remote to this machine. It makes the Brain checkout equal to its upstream, dropping local commits and changes to tracked files, and says what it dropped. Then it carries the Brain into each listed checkout. Untracked files in the Brain stay.
- **`nerve push [message]`** brings this machine to the remote. It commits everything in the Brain checkout, then force-pushes it to its upstream.
- **`nerve update [repo]`** finds each source's latest commit, shows how its skills changed, and moves the pin.

Around them:

- **The session-start hook only reports**: how the project stands against the Brain, drift and problems. It never writes. Every listed project has it, with no switch.
- **Keeping secrets out of the Brain is the user's job.** nerve keeps the allow-list of settings, which says what a Brain carries. Any other key goes to the local overlay. Value scanning, secret key names and hidden values go.
- **A worktree is a checkout.** It gets nerve when `project add` lists it, like any other.
- **A state nerve cannot read is reported, then replaced.** The tree is treated as one nerve has no state for, as a checkout that moved.
- **nerve never deletes `settings.local.json`.** Cleaning a project writes what is left, `{}` included.
- **Dropped:** the `nerve` release pin and the version report, `project autosync`, and the `ls` commands but `project ls`. `status` shows what a project lists.

## Consequences

- A change to the Brain's tracked files is lost at the next sync unless it is pushed. A new skill not yet committed stays, since it is untracked.
- Two machines that push without syncing first overwrite each other's commits.
- A secret written into the Brain reaches its remote. The nerve skill still shows the user each value before it enters the Brain.
- A project is in step only after `nerve sync`. Sessions say when it is not.
- `brain.toml` keys `nerve` and `autosync` are refused, as nerve refuses keys it does not know.
