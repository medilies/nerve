# Text output for agents

Status: accepted, 2026-11-22.

## Context

[core.md](../../core.md) hides the CLI's complexity behind the nerve skill, "that is why good output is needed". The skill had the agent add `--json` to every command. On one project with 7 links, `nerve status --json` printed 61 lines and 1,419 bytes, and the text printed 10 lines and 535 bytes for the same events. Both listed every link, so they grew with the Brain. The skill then mapped each event to the command that resolves it, in a long section of its own.

## Decision

- **Text is the agent's interface.** `--json` goes. `--hook` stays, for the session report.
- **Quiet when in order.** `status` no longer lists links that are as they should be: the checkout line, the project's counts and the lines that need action say it. `sync` already prints only what it made, removed or set.
- **A line that needs action ends with its fix.** The commands that resolve it follow a colon, as errors already do. Examples are a checkout behind the Brain (`nerve sync --here`), Brain work not pushed (`nerve push`), and a drift (the command that keeps it, and the one that drops all drift). The session report shows the same lines to the user.
- **`update`'s `changed` line names the skill's folder at the tip**, where the agent reads a new script before the pin moves.

## Consequences

- An agent reads less, and runs what the line names instead of looking it up in the skill.
- Scripts that read `--json` must read text. Its lines have a fixed first column, which is no contract either.
- A full list of links is in `.claude/` and the project's entry, not in `status`.
