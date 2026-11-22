# Drift never fails a command

Status: accepted, 2026-11-22. Amends [ADR 0014](0014-listing-commands-sync-here.md), whose listing commands exited 1 on drift.

## Context

A sync failed on drift, and so did every command that runs one: `project add`, `update`, and since ADR 0014 the listing commands. Their change was pushed, yet they exited 1. The nerve skill runs one request as one `&&` chain, so drift in the checkout stopped the chain halfway. Drift is not an error: nerve did what it was told, left the change, and reports it with the commands that resolve it.

A listing command also lost to drift on the very setting it named: `nerve setting set model opus` pushed opus, and the sync left the `sonnet` someone had set by hand.

## Decision

- **Drift never fails a command**, `sync` included. Each drift line still ends with the commands that keep or drop it. A command fails only when nerve could not do what it was told.
- **What a command names wins here.** The setting a listing command names, or a plugin's setting, is written in the checkout it syncs, even where it drifts. Other drift stays.

## Consequences

- `nerve sync --here && …` runs on when the checkout drifts.
- A script that read a sync's exit code to find drift reads `status` instead.
- Setting a key to the value the Brain holds drops its drift here: a drop of one key, where `-f` drops all.
