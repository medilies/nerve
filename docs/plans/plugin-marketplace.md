# Plan: carry skills through a Claude plugin marketplace

Status: proposed, 2026-11-17. If the spike passes, an ADR replaces [ADR 0001](../adr/0001-own-sync-tool.md).

## Goal

Skills reach every Claude Code config and claude.ai account through Claude's own plugin system instead of nerve's links and zips. nerve keeps what plugins cannot do: settings, standing instructions, keybindings, and reviewed pin updates.

## Why

- **claude.ai without manual uploads.** A plugin installed on claude.ai syncs to every Claude Code signed in with that account, and marketplaces added from claude.ai auto-update. This replaces building zips, uploading them by hand and reporting claude.ai drift.
- **Native installs.** A marketplace declared in user settings is cloned on a new machine, and its enabled plugins are downloaded without any nerve code.
- **No shadowing.** Plugin skills are namespaced (`/brain:go-stack`), so they never override a project's own skill of the same name.
- **Less code to keep.** Linking skills, tracking links in `.nerve.json`, the third-party cache, zips and claude.ai drift all go.

## What plugins cannot do

Checked against the Claude Code plugin, marketplace and settings docs:

- **Write `settings.json`.** The plugin manifest has no settings key, so nothing sets `model`, `permissions`, `statusLine` or keybindings.
- **Ship standing instructions.** "A `CLAUDE.md` at the plugin root isn't loaded as context."
- **Install themselves.** `extraKnownMarketplaces` and `enabledPlugins` live in `settings.json`, so something must write them first.
- **Carry a binary to claude.ai.** claude.ai refuses a plugin with a top-level `bin/`.

That is why nerve stays a binary.

## Two repos

This is the same split as [ADR 0002](../adr/0002-brain-and-nervous-system.md): content in one repo, the tool in the other.

| Repo | Holds | Plugin role |
| --- | --- | --- |
| **Brain** (one per person, content only) | own skills, third-party pins, settings, instructions, hooks, `brain.toml` | The marketplace: `marketplace.json` lists the own-skills plugin and the pinned third-party entries |
| **nerve** (this repo, the tool) | the binary and its one skill | None. The skill stays embedded in the binary and installed by `sync`, so it always matches the binary and is never namespaced. |

The own-skills plugin stays inside the Brain, not in a repo of its own:

- one commit can change a skill, its pin and the profile that enables it;
- a relative-path source (`./plugins/brain`) needs no pin to bump on every edit;
- the spec says all skills live in one repo.

## Brain layout

```
.claude-plugin/marketplace.json   # plugins and their pins (replaces [[source]])
plugins/brain/skills/<name>/      # own skills, as one plugin
settings/base.json                # now also enables plugins
settings/<profile>.json           # profiles choose which plugins they get
instructions/base.md
instructions/<profile>.md
keybindings.json
hooks/
brain.toml                        # nerve pin and profiles only
```

```json
{
  "name": "brain",
  "plugins": [
    { "name": "brain", "source": "./plugins/brain" },
    {
      "name": "grilling",
      "source": {
        "source": "git-subdir",
        "url": "https://github.com/mattpocock/skills",
        "path": "…",
        "sha": "5863806dd4e15c63174bee4e7515a4256792ecfa"
      }
    }
  ]
}
```

## Lifecycle

**New machine**

1. Install `nerve` by hand.
2. Run `nerve init`.
3. Run `nerve sync`. It writes each config's settings, now including:

   ```json
   "extraKnownMarketplaces": { "brain": { "source": { "source": "github", "repo": "<owner>/brain" } } },
   "enabledPlugins": { "brain@brain": true, "grilling@brain": true }
   ```

4. Open Claude Code. It clones the marketplace and downloads the enabled plugins in the background after the session starts, so the first session may lack them.

This happens once per config folder, because plugins are stored per config directory.

**Edit an own skill**

- While writing it, run `claude --plugin-dir <brain>/plugins/brain`. An enabled `--plugin-dir` plugin outranks the installed copy, so edits are live.
- To roll it out, commit and push. Machines pick it up when the `brain` marketplace refreshes. nerve sets `autoUpdate: true` on that one marketplace, or the change is pulled now with `claude plugin marketplace update brain`.

**Add a third-party skill**

1. Add a `marketplace.json` entry with a full `sha`.
2. Enable it in a settings layer.
3. Commit, push, `nerve sync`.

**Move a pin**

1. `nerve update <repo>` shows the upstream diff and rewrites the `sha` in `marketplace.json`.
2. Review, commit, push.
3. Each machine refreshes. The new commit SHA becomes the plugin's version, so it gets a new cache folder; the old one is deleted 14 days later.

Auto-update stays off for third-party entries.

**Remove a skill**

Delete the entry and its `enabledPlugins` line, then commit, push and `nerve sync`.

**Settings, instructions, keybindings**

Unchanged: layered settings with drift detection, local overlays, the secret guard, the `CLAUDE.md` import block and the keybindings link.

**claude.ai, once per account**

1. Add the Brain repo on claude.ai and install the `brain` plugin.
2. It syncs to Claude Code on sign-in.

Where the same plugin is also installed from the marketplace, the marketplace copy wins: claude.ai-synced plugins rank last, so they never show up twice. Configs signed in with an API key or Bedrock get no claude.ai sync, but still have the marketplace copy.

**By prompt**

The nerve skill still drives every step on the machine through `nerve`, `git` and the `claude plugin` CLI. Some steps stay manual:

- installing the plugin on claude.ai;
- starting a session with `--plugin-dir`;
- the first install of `nerve` on a new machine.

## Changes to nerve

| Keeps | Drops | Adds |
| --- | --- | --- |
| settings layers, drift, overlays, secret guard | linking skills and `.nerve.json` link tracking | writes `extraKnownMarketplaces` and `enabledPlugins` |
| `CLAUDE.md` block, keybindings | third-party skill cache | `update` edits `marketplace.json` |
| `update` with diff review | `dist/` zips, claude.ai drift report | `check` validates `marketplace.json` (full SHAs, enabled plugins exist) |
| self-pinning, `check`, `--json`, the embedded skill | `[[source]]` in `brain.toml` | the skill learns the new layout |

## Spike: run before anything else

Build a throwaway marketplace with one own-skills plugin and one `git-subdir` entry pinned into `mattpocock/skills`. Install it in both configs and on one claude.ai account.

| # | Question | Passes if |
| --- | --- | --- |
| 1 | Does a `git-subdir` entry load one skill from a repo that is not a plugin (`SKILL.md` with no `skills/` folder)? | The skill loads at the pinned SHA. **If not, the plan needs rethinking**: the only fallback is taking the whole repo. |
| 2 | Does a relative-path plugin get a new version when the Brain gets a new commit? | Pushing an edit and refreshing the marketplace updates the cached copy |
| 3 | Does claude.ai install from a private repo? | It does, or the Brain is acceptable as a public repo |
| 4 | Does a claude.ai-synced copy show up twice next to the marketplace copy? | Only one appears |
| 5 | Do declared marketplaces install on their own in a second config folder? | `~/.claude` and `~/.claude-personal` both get the plugins from settings alone |
| 6 | Does a new or updated plugin load mid-session? | Record what is needed: a reload or a new session |
| 7 | Does `claude plugin …` work when Claude runs it from inside a session? | The prompt-driven flow works |
| 8 | Is the namespacing acceptable (`/brain:go-stack`, `/grilling:grilling`)? | The owner accepts it. ADR 0001 rejected plugins on this preference. |

## Rollout once the spike passes

1. Write an ADR that replaces 0001, giving the spike's results as the reason.
2. In nerve:
   - add the `marketplace.json` model and the settings it writes;
   - move `update` to `marketplace.json`;
   - add the new `check` rules.

   Release as `v0.2.0`.
3. In the Brain:
   - move own skills to `plugins/brain/skills/`;
   - turn `[[source]]` into marketplace entries;
   - enable plugins in the settings layers;
   - pin nerve `v0.2.0`.
4. `nerve sync` on each machine, then remove the old skill links nerve made, using `.nerve.json`.
5. Install the plugin once on each claude.ai account, then delete the uploaded skills there.
6. Remove the dropped code, the `dist/` ignore rule, and the README sections about zips and claude.ai drift. Update the glossary: a "Pin" can now live in `marketplace.json`.
