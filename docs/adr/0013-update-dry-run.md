# update --dry-run reads an update before it lands

Status: accepted, 2026-11-21. Amends, in [ADR 0010](0010-brain-toml-in-step.md), "`update` reaches the project before anyone reads its diff": `--dry-run` reads it first.

## Context

`core.md` says `update` looks for the latest commits of third-party skills and moves their pins in the Brain and the project. [ADR 0010](0010-brain-toml-in-step.md) has `update` show each diff, pin, push and sync in one run. So the diff of a source the user does not trust reaches the remote and the project before anyone reads it.

The other way, an `update` that only reports and a flag such as `--pin` that moves the pin, goes against `core.md`, where `update` moves pins.

## Decision

- **`nerve update` stays as it is**: it shows how each source's skills changed, then pins, pushes and syncs the checkout here.
- **`nerve update --dry-run`** shows the same, and moves no pin: no push, no sync, nothing in the Brain or a project. When a pin would move, it ends by saying none did. It still fills nerve's cache of sources, as any read of a source does.
- **A dry run reads `brain.json` as this machine holds it**, as `status` and `check` do. It does not take the remote's commits first, so the Brain's work not pushed never stops it. `core.md` says the remote and local `brain.json` always sync: a dry run changes neither, so they stay as in step as it found them.
- `--diff` works with `--dry-run`.

## Consequences

- The nerve skill can show the table of a dry run, and ask before running `nerve update`.
- `nerve update` reads the tip again. When the repo moved after the dry run, `update` shows that diff instead, and pins it: it pins what it shows, which may not be what the user read.
- A dry run on a Brain behind its remote shows the update from the older pins. `nerve sync` first makes it the remote's.
