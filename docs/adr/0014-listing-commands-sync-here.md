# Listing commands sync the checkout here

Status: accepted, 2026-11-22. Amends [ADR 0009](0009-explicit-verbs.md), "A project is in step only after `nerve sync`", as [ADR 0012](0012-project-add-syncs.md) did for `project add`.

## Context

Using an own skill and its two agents in the project at hand took five commands:

```sh
nerve push "add reviewing"
nerve skill add reviewing
nerve agent add code-reviewer
nerve agent add impact-explorer
nerve sync --here
```

That meant three pushes of `brain.json`, and a sync the user had already asked for by naming the project. Until the sync, the project did not have what it had just been given. The nerve skill made the agent stop and ask before each step. [core.md](../../core.md) asks for commands that are useful with few calls.

## Decision

- **A listing command syncs the checkout here.** `skill`, `agent`, `hook` and `plugin` `add` and `rm`, and `setting set`, `add` and `rm`, sync the listed checkout around the working directory once they succeed. They do this when the change reaches that checkout: its own project, or every project with `--common` or `--all`. The Brain is its remote's already, so they carry it as it is, as `project add` and `update` do.
- **They refuse up front** a checkout that a sync would refuse, before they change anything.
- **Other checkouts wait** for their own `nerve sync`, and their session report shows them behind. Outside a listed checkout, or with `--project` naming another project, a listing command only pushes.
- **Several names, one push.** `add` and `rm` take one or more names of one kind, in one commit, one push and one sync. If one name is refused, the command changes nothing. Kinds are not mixed in one command.
- **`push` stays apart.** `skill add` still refuses an own skill that the remote lacks. Folding `push` in would commit everything else in the Brain too.

## Consequences

- "Use X here" is one command, or `push` then `add` for a new own skill.
- A listing command that succeeds syncs even when it changed nothing, such as an item already listed. A checkout behind the Brain then catches up.
- When the checkout here drifts, a listing command exits 1 after its change is pushed, as `update` does, since the sync it runs reports the drift.
