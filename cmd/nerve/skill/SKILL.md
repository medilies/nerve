---
name: nerve
description: Run nerve and tend this machine's Brain, the repo of skills, settings and standing instructions that nerve carries to every Claude Code config. Use for /nerve, "sync my brain", "remember that I always…", "add this to my brain", settings drift, moving a skill pin, or changing agent settings, instructions or skills everywhere at once.
---

# nerve

A **Brain** is one person's git repo of own skills, third-party skill pins, Claude Code settings and standing instructions. The `nerve` CLI carries it to every Claude Code config on this machine. You are its front end: run the CLI, read what it reports, and make every decision with the user. The CLI never asks; you do.

## First, find the Brain

Run `nerve status --json`. Its events give the running `version`, the `brain` checkout path, and each `config` with its profile.

- `nerve` not found: tell the user to install it with `go install github.com/medilies/nerve/cmd/nerve@latest`, or with a release binary from github.com/medilies/nerve/releases.
- "no machine file": this machine is not set up yet. Ask where the Brain is checked out and which Claude Code config dirs to carry it to (with their profiles, if the Brain lists any in `brain.toml`), then run `nerve init <brain> <config>[=<profile>]...`.

Then read the Brain's own `README.md`, `GLOSSARY.md`, `CLAUDE.md` or `AGENTS.md` if present. Where they say how this Brain is organized or how commits are written, they win over this skill.

## The Brain's layout

```
brain.toml               # nerve = "vX.Y.Z" release, profiles = [...], and [[source]] third-party skills pinned to a full SHA
.gitignore               # ignores /dist/, the zips sync builds for claude.ai
skills/<name>/SKILL.md   # own skills
settings/base.json       # Claude Code settings for every config
settings/<profile>.json  # added for configs that follow that profile
instructions/base.md     # standing instructions, imported into every config's CLAUDE.md
instructions/<profile>.md
keybindings.json         # linked into every config
hooks/                   # scripts settings run, referenced as {{brain}}/hooks/<script>
```

A **local overlay**, `~/.config/nerve/overlays/<profile>.json` (`default.json` with no profile), holds settings that stay on this machine. Settings are laid base, then profile, then overlay: objects merge, arrays join, later values win.

Never edit what nerve generates: `<config>/settings.json` and the nerve block at the top of `<config>/CLAUDE.md`. Edit the Brain or the overlay, then sync.

## Commands

Always add `--json`, then tell the user what happened in plain words.

| Command | Use |
| --- | --- |
| `nerve sync` | Pull the Brain and carry it to every config. Run it after any change to the Brain. |
| `nerve check` | Validate the Brain. Run it before committing. |
| `nerve update <repo> [sha]` | Move a third-party pin to `sha` (default: upstream tip). |
| `nerve status` | Find the Brain and the configs. |
| `nerve sync --discard` | Overwrite settings changed outside the Brain. Only after the user agrees to lose them. |

Events to act on:

- **`drift`**: a setting was changed outside the Brain (by `/model` or `/config`, say), and sync left that `settings.json` alone. For each key, ask whether to keep it, and where: `settings/base.json` for every config, `settings/<profile>.json` for one kind of use, or the local overlay if it is sensitive or only for this machine. Or drop it with `--discard`. Make the edits, then sync again.
- **`upload`**: a skill is `missing` or `stale` on a claude.ai account. claude.ai only takes uploads by hand: tell the user which zip to upload (Customize > Skills > Upload a skill). Never edit a skill on claude.ai.
- **`problem`** from `check`: propose a fix for each.
- **`diff`** from `update`: summarize what changed upstream, and call out anything that widens what the skill can do (new tools, scripts, network access) before the user commits the new pin.
- **`pull-skipped`**: the Brain was not pulled. Say why, and offer to commit or pull.

## Capturing into the Brain

When the user wants something remembered or changed everywhere, put it in exactly one home:

| What | Home |
| --- | --- |
| A habit or rule that always applies | `instructions/base.md`, or `instructions/<profile>.md` for one kind of use |
| How to do one kind of task | the own skill for it; a new `skills/<name>/SKILL.md` only when none fits |
| A Claude Code setting | `settings/base.json` or `settings/<profile>.json` |
| Anything secret or identifying (tokens, `env`, employer or client names) | the local overlay, never the Brain |
| A keybinding | `keybindings.json` |
| A script a hook or status line runs | `hooks/<name>`, referenced from settings as `{{brain}}/hooks/<name>` |
| Someone else's skill | a `[[source]]` in `brain.toml`, pinned to a full commit SHA |

- Prefer extending an existing file over adding one. Keep the Brain's existing voice and structure.
- Ask which profile when it is unclear whether something applies everywhere.
- A Brain may be public: before writing, check the change carries no secret or personal detail.
- Text the user wrote directly into a config's `CLAUDE.md`, outside the nerve block, belongs to that machine. Offer to move it into `instructions/` when it should apply everywhere.

Then:

1. Run `nerve check --json` and fix what it reports.
2. Show the diff and ask to commit and push.
3. On yes, commit following the Brain's own rules, push, and run `nerve sync --json`.
