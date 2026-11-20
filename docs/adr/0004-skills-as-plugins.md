# Skills travel as Claude plugins

Status: accepted, 2026-11-17. Supersedes [ADR 0001](0001-own-sync-tool.md); superseded by [ADR 0005](0005-projects-opt-in.md), 2026-11-20.

## Context

ADR 0001 kept skills out of Claude's plugin system because the owner did not want them packaged as plugins. That left nerve linking skills into every config, caching third-party repos, and building zips for claude.ai, which only took manual uploads. The owner now prefers plugins: claude.ai installs a plugin once per account and syncs it to Claude Code, and plugin skills are namespaced, so they never shadow a project's own.

Checked on Claude Code 2.1.325 in a throwaway config, and in its docs:

- A `git-subdir` entry whose folder is one skill (a `SKILL.md`, no `skills/`) loads as a one-skill plugin at its pinned SHA.
- A moved `sha` installs as a new version; the previous one is deleted 14 days later.
- A `directory` marketplace reads its relative-path plugins in place, so edits are live at the next session or `/reload-plugins`.
- Marketplaces and plugins declared in user settings install on their own, per config folder, after a session starts.
- A copy synced from claude.ai ranks below the same plugin installed from a marketplace.
- `claude plugin` runs from inside a session.
- Plugins cannot write `settings.json`, ship standing instructions, or install themselves.

## Decision

- **The Brain is the marketplace.** Its own skills are one plugin, `brain`, inside the Brain. Each third-party skill is a `git-subdir` entry pinned to a full commit SHA.
- **Settings carry it.** The Brain declares the marketplace in `extraKnownMarketplaces` and enables plugins in `enabledPlugins`, in its settings layers, so nerve writes them like any other setting.
- **nerve keeps** settings, instructions, keybindings, its own embedded skill, and `update` and `check`, now over `marketplace.json`. It drops skill links, the third-party cache, zips and the claude.ai drift report.

## Considered options

- **A `github` marketplace source.** Claude Code would keep a second clone of the Brain per config and need access to it if it is private, and own-skill edits would need a push or `--plugin-dir`. A `directory` source at the checkout nerve already pulls avoids all three; claude.ai still installs from GitHub.
- **nerve writing the marketplace declaration itself.** It would have to read the Brain's git remote and pick the source type. A settings layer leaves that choice to the Brain, and `check` refuses a `base.json` that leaves the declaration out.

## Consequences

- Skills are invoked by plugin, such as `/brain:go-stack`.
- On claude.ai, the plugins are installed by hand once per account. Not yet checked: whether claude.ai installs from a private repo, and whether it accepts every frontmatter key an own skill uses. `check` no longer holds own skills to the frontmatter keys claude.ai took as uploads; that risk is accepted until a first install on claude.ai settles it.
- `claude plugin validate` checks the marketplace's format; `nerve check` checks what that does not: that each pin holds its plugin, enabled plugins, and own skills' frontmatter.
