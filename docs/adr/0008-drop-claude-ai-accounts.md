# Drop claude.ai accounts

Status: accepted, 2026-11-21. Supersedes, in [ADR 0005](0005-projects-opt-in.md), "claude.ai is a special project".

## Context

A project with `surface = "claude.ai"` stood for a claude.ai account and took skills only. A full sync zipped each of its skills into nerve's cache, and said which zips to upload and which skills to delete. claude.ai has no API for skills, so the user did both by hand.

- **It carried skills everywhere.** A skill on claude.ai reaches every Claude Code session signed into the account, against [ADR 0005](0005-projects-opt-in.md)'s rule that nothing reaches a project it does not list.
- **It could not see the account.** nerve said "upload" once per change. A missed line left nerve and the account apart, with no sign of it.
- **It saved one command.** Zipping a skill folder is one `zip` command.
- **It cost a second kind of project.** Sync, project, items, settings, check and status each special-cased it, and it brought a port, an adapter, a record of the zips built, and two events. The fifth review found five bugs in it.

It was never released.

## Decision

nerve has one kind of project: a Claude Code checkout. claude.ai accounts are dropped, with the zips, the record of what was built, and the `upload` and `unload` events. They wait in the [backlog](../backlog.md) for an API that lets nerve upload skills and read what an account holds.

`syncClaudeAiSkills` stays an allowed setting: skills uploaded by hand still reach every session, and `common` can turn that off in listed projects.

## Consequences

- A Brain whose `brain.toml` sets `surface` must drop that project, since nerve refuses keys it does not know.
- Every project has sessions, so every project can take autosync, settings, agents, hooks and plugins.
