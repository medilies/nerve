# Generated, layered settings

Status: accepted, 2026-11-16

## Context

A Brain carries Claude Code settings to every config. Configs differ by kind of use (work and personal want different models, effort and hooks), and some settings are sensitive or identifying while a Brain may be public. Claude Code also rewrites `settings.json` itself, from `/model`, `/config` and similar.

## Options

- **Symlink one `settings.json`** from the Brain into every config. Edits made in Claude Code land in the Brain at once, but configs cannot differ, secrets would be committed, and an app rewrite may replace the link with a plain file.
- **A symlink per profile.** Configs can differ, but shared settings are copied into every profile and secrets still have nowhere to go.
- **Generate the file** from layers: the Brain's base, then the config's profile, then a local overlay that never leaves the machine.

## Decision

Generate `settings.json` for each config from base, profile and local overlay, later layers winning. Each `sync` compares the file with what it last wrote and reports keys changed in Claude Code as drift; the Nervous system's skill offers to move each into the right layer. `check` refuses known secret-bearing keys (`env`, `apiKeyHelper` and the like) in the Brain.

## Consequences

- Changes made in Claude Code do not reach the Brain until someone moves them; they are reported, not lost.
- The Nervous system owns a merge rule for settings and must record what it last wrote.
- Hook commands in the Brain cannot use fixed paths, as config dirs differ; the Nervous system fills in the Brain's path when it generates the file.
