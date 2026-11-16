# Own sync tool instead of the `skills` CLI or a Claude plugin

Status: accepted, 2026-11-16

## Context

Skills reach four surfaces: two Claude Code configs and two claude.ai accounts. The repo must be the source of truth for all of them, third-party skills must be pinned to exact commits, and removing a skill from the repo must remove it everywhere.

## Options

- **The `skills` CLI** (vercel-labs/skills). It accepts `owner/repo#sha` and local paths, but its lockfile only restores project-scoped installs, not global ones. Sharing one copy between configs needed selecting an unused agent (`codex`). Pins, removal, zips and drift checks would all have been built around it anyway, and it sends install telemetry.
- **A Claude plugin marketplace.** It pins with `sha`, namespaces skills so they never shadow a project's own, and claude.ai can sync it from GitHub. Rejected: the owner does not want these skills packaged as a plugin.
- **An own Go tool** reading `skills.toml`.

## Decision

An own Go tool, `cmd/skills`. It links own skills from the checkout, fetches third-party skills at their pinned SHA, removes only links it made, builds claude.ai zips, and reports claude.ai drift.

## Consequences

- claude.ai stays a manual upload: there is no API for personal skills. The tool says what to upload.
- A personal skill still shadows a project's skill of the same name. Own skills defer to project instructions in text; third-party skills are left as upstream ships them.
- The tool is ours to maintain: a few hundred lines of Go with one dependency.
