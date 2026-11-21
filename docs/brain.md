# The Brain

A Brain is a git repo you own, pushed to a remote its branch tracks.
nerve knows nothing about what is in it beyond its layout:

```text
brain.json          # allowed settings, third-party pins, hooks, common settings, projects
skills/<name>/      # own skills
agents/<name>.md    # own agents
hooks/<script>      # scripts hooks run, referenced as {{brain}}/hooks/<script>
```

Words used here are defined in [GLOSSARY.md](../GLOSSARY.md); the
reasoning is in [ADR 0005](adr/0005-projects-opt-in.md),
[ADR 0007](adr/0007-drop-guessing-features.md) and
[ADR 0009](adr/0009-explicit-verbs.md).

## brain.json

```json
{
  "allowedSettings": ["statusLine"],
  "sources": [
    {
      "repo": "acme/skills",
      "sha": "5863806dd4e15c63174bee4e7515a4256792ecfa",
      "skills": ["grilling"]
    }
  ],
  "hooks": {
    "go-fmt": {
      "event": "PostToolUse",
      "matcher": "Edit|Write",
      "command": "{{brain}}/hooks/go-fmt.sh"
    }
  },
  "common": {
    "settings": {"syncClaudeAiSkills": false}
  },
  "projects": {
    "billing": {
      "skills": ["go-stack", "grilling"],
      "agents": ["reviewer"],
      "hooks": ["go-fmt"],
      "plugins": ["gopls-lsp@claude-plugins-official"],
      "settings": {"model": "opus"}
    }
  }
}
```

- **`allowedSettings`** lists settings keys the Brain may hold beyond
  those nerve allows. `nerve allow add` and `nerve allow rm` change it.
  See [Allowed settings](#allowed-settings).
- **`sources`** lists third-party repos on GitHub, each at a full commit,
  and the skills used from it. Each repo has one entry. A link in a
  source arrives as a plain file holding its target. Skill, agent, hook
  and project names are lowercase words of letters and digits, joined
  by hyphens.
  - A skill is the whole repo when its root `SKILL.md` names it, else
    the folder `skills/<name>/` or `<name>/` holding a `SKILL.md`, in
    one of them only.
  - `path` names another folder to look in instead, such as
    `"path": "plugins/x/skills"`.
  - All of an entry's skills come from that one folder. A repo that
    keeps skills in two folders, such as `skills/engineering/` and
    `skills/productivity/`, gives skills from only one of them.
  - `nerve update [<repo>]` shows each source's pin and the tip of its
    default branch, with the date of each, and the files each of its
    skills changed, with their lines added and removed. `--diff` shows
    the whole diff too. Then it pins and pushes the tip, and syncs the
    listed checkout around the working directory.
- **`hooks`** defines Claude Code hooks by name. `{{brain}}` in any
  value becomes the Brain's checkout path.
- **`common.settings`** holds the only settings that reach more than
  one project: every listed project gets them. Skills, agents, hooks
  and plugins are never common.
- **`projects`** lists what each project gets, each item once, under a
  name you choose, never a path or a remote.

Settings are written as in Claude Code's `settings.json`, and nerve
keeps each value as it is. nerve rewrites `brain.json` in this layout
when a command changes it, `update` included, and pushes the change at
once, as [Push](sync.md#push) says.

nerve refuses `null` in settings: `nerve setting rm` removes one.

## Allowed settings

A Brain holds only allowed settings, and everything under them:

- those nerve allows, each holding only what Claude Code takes there:

  | Setting | Holds |
  |---|---|
  | `alwaysThinkingEnabled`, `disableBundledSkills`, `disableWorkflows`, `enableAllProjectMcpServers`, `enableArtifact`, `spinnerTipsEnabled`, `syncClaudeAiSkills` | `true` or `false` |
  | `model`, `outputStyle` | text |
  | `effortLevel` | `"low"`, `"medium"`, `"high"` or `"xhigh"` |
  | `modelSettings` | an object per model, such as `claude-opus-5-5`, whose `effortLevel` is one of those levels |
  | `cleanupPeriodDays` | a whole number, at least 1 |
  | `permissions.allow`, `permissions.ask`, `permissions.deny`, `enabledMcpjsonServers`, `disabledMcpjsonServers` | a list of text |
  | `permissions.defaultMode` | `"default"`, `"acceptEdits"`, `"plan"`, `"dontAsk"` or `"manual"`; Claude Code takes `"auto"` and `"bypassPermissions"` only from `~/.claude/settings.json` |
  | `attribution` | `false`, or `commit` and `pr` as text and `sessionUrl` as `true` or `false` |

- those `allowedSettings` lists, such as `"statusLine"` or
  `'extraKnownMarketplaces."acme.tools"'`. nerve does not check what
  they hold.
  - `nerve allow add <key>...` adds keys. It refuses a key nerve allows
    itself.
  - `nerve allow rm <key>...` takes keys out. It refuses one whose
    settings the Brain still holds: `nerve setting rm` them first.
  - `nerve check` notes each key it lists that nerve allows itself,
    without failing.

`hooks` and `enabledPlugins` are never allowed: nerve writes them from
`hooks` and `plugins`, and `allowedSettings` cannot list them.

`check`, `sync` and `push` refuse any other setting, and a value of
one nerve allows that does not fit, as `setting set` and `setting add`
do.
`setting set` and `setting add` put it in the project's local overlay
instead; with `--common`, they refuse it.

What an allowed setting holds is yours to judge: nerve does not look for
secrets in it. A Brain may be public, so keep secrets, home paths and
anything identifying in a local overlay.

## This machine

nerve keeps this machine's files in its config folder:

- Linux: `~/.config/nerve`, or `$XDG_CONFIG_HOME/nerve` when set.
- macOS: `~/Library/Application Support/nerve`.

Fetched skills go in its cache folder:
`~/.cache/nerve` (or `$XDG_CACHE_HOME/nerve`) on Linux,
`~/Library/Caches/nerve` on macOS.

`config.json`, written by `nerve init`, names the Brain's checkout, a
git repo, and each listed checkout's project. `nerve project add` adds a
checkout. nerve rewrites the file when it changes it.

```json
{
  "brain": "~/dev/brain",
  "checkouts": [
    {
      "path": "~/dev/billing",
      "project": "billing"
    }
  ]
}
```

- **Local overlays**, `overlays/<project>.json`, hold a project's
  settings the Brain does not allow. nerve puts them there on its own
  when you set one. You may also write in one what the Brain refuses, such as
  `null`. They never leave the machine, so set them again on each
  one.
  - Setting a key in the Brain drops the overlay's value for it, which
    would win: with `--common`, from every overlay on this machine.
  - `nerve setting rm` removes a key from the overlay too.
  - `nerve project rm` keeps the project's overlay, and says where it
    is: delete it once you no longer need what it holds.
- **State**, under `state/`, is what nerve last did to each working
  tree, never kept in the project.

These files are readable by you only.

## Checking a Brain

`nerve check`, in the Brain (a git repo with `brain.json` at its top)
or anywhere when this machine names one, validates own skills and
agents, each pin, what each project lists, and the allowed settings
above. In a listed checkout, it checks this machine's Brain, even when
the project has a `brain.json` of its own.
In the Brain's CI, run a release of nerve:

```sh
go run github.com/medilies/nerve/cmd/nerve@latest check
```
