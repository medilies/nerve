# project add names and syncs its checkout

Status: accepted, 2026-11-21. Amends, in [ADR 0005](0005-projects-opt-in.md), "`nerve project add <name>` makes that link once per checkout": the name is optional.

## Context

Listing a project took two commands and a name the user had typed already, in the folder they stood in:

```sh
nerve project add my-repo
nerve sync --here
```

A checkout listed but not synced has nothing of the Brain, not even the nerve skill or the session report.

## Decision

- **`project add` with no name** names the project after the repo's folder: the folder that holds the git folder its worktrees share. A worktree joins its repo's project. A folder name that is not a project name is refused, asking for one.
- **`project add` syncs the checkout** it lists, as `update` syncs the checkout around the working directory ([ADR 0010](0010-brain-toml-in-step.md)). The Brain is already its remote's, so the sync carries it as it is.
- **A checkout sync would refuse, `project add` refuses** before it changes the Brain or this machine's file.
- An already listed checkout is synced again.

## Consequences

- One command makes a project of the folder the user stands in.
- Settings the checkout had are reported as personal at `project add`, not at a later sync. A key the Brain sets later is the Brain's from then on, as for any personal setting.
- Two repos in folders of the same name, on one machine or two, join one project unless the user names one.
