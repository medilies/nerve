# JSON for brain.json and the machine file

Status: accepted, 2026-11-21. Replaces `brain.toml` with `brain.json`, and `config.toml` with `config.json`.

## Context

Claude Code's settings are JSON. The Brain held them in TOML, so nerve turned each value from one into the other and back:

- TOML has no null, and no whole number beyond 64 bits, which JSON has.
- JSON has no inf, nan or dates, which TOML has.
- `1`, `1.0` and `1e0` had to compare equal across both.
- A value given on the command line as JSON was written back as TOML: `model '{"a":1}'` became `model = {a = 1}`.

Each needed code, and several were bugs the reviews found. How a value is written should be the user's, not nerve's.

## Decision

- **The manifest is `brain.json`**, a JSON object at the Brain's top. Its keys follow Claude Code's own: `allowedSettings`, `sources`, `hooks`, `common` and `projects`. Settings sit in it as they would in `settings.json`, and nerve never converts a value.
- **The machine file is `config.json`**, with `brain` and `checkouts`.
- **Null stays out of the settings a Brain holds**: `nerve setting rm` removes a setting.
- No migration: before 1.0, a Brain is converted once by hand.

## Consequences

- A setting copied from a `settings.json` goes into the Brain as it is.
- `brain.json` holds no comments, as `settings.json` holds none.
- nerve still rewrites the manifest in its own layout when a command changes it.
