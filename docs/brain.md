# The Brain

A Brain is a git repo you own. nerve knows nothing about what is in it
beyond its layout:

```text
brain.toml          # the nerve release, autosync, allowed settings, third-party pins, hooks, common settings, projects
skills/<name>/      # own skills
agents/<name>.md    # own agents
hooks/<script>      # scripts hooks run, referenced as {{brain}}/hooks/<script>
```

Words used here are defined in [GLOSSARY.md](../GLOSSARY.md); the
reasoning is in [ADR 0005](adr/0005-projects-opt-in.md) and
[ADR 0007](adr/0007-drop-guessing-features.md).

## brain.toml

```toml
nerve = "v0.3.0"                  # the release that carries this Brain
autosync = true                   # sync each project at session start; the default
allowed-settings = ["statusLine"] # settings keys this Brain may hold beyond nerve's own

[[source]]                        # third-party skills, pinned to a full commit
repo = "acme/skills"
sha = "5863806dd4e15c63174bee4e7515a4256792ecfa"
skills = ["grilling"]

[hook.go-fmt]                     # a hook projects can take by name
event = "PostToolUse"
matcher = "Edit|Write"
command = "{{brain}}/hooks/go-fmt.sh"

[common.settings]                 # settings every listed project gets
syncClaudeAiSkills = false

[project.billing]                 # what one project gets, and nothing else does
skills = ["go-stack", "grilling"]
agents = ["reviewer"]
hooks = ["go-fmt"]
plugins = ["gopls-lsp@claude-plugins-official"]

[project.billing.settings]
model = "opus"
```

- **`nerve`** pins the release that carries the Brain. When another
  release runs, `sync` reports it: install the pinned one. nerve never
  replaces itself, and says nothing to a development build.
- **`allowed-settings`** lists settings keys the Brain may hold beyond
  those nerve allows. See [Nothing sensitive](#nothing-sensitive).
- **`[[source]]`** is a third-party repo on GitHub at a full commit,
  and the skills used from it. Each repo has one table. A link in a
  source arrives as a plain file holding its target. Skill, agent, hook
  and project names are lowercase words joined by hyphens.
  - A skill is the whole repo when its root `SKILL.md` names it, else
    the folder `skills/<name>/` or `<name>/` holding a `SKILL.md`, in
    one of them only.
  - `path` names another folder to look in instead, such as
    `path = "plugins/x/skills"`.
  - `nerve skill update <repo> [sha]` shows how the source's skills
    changed, then moves the pin.
- **`[hook.<name>]`** defines a Claude Code hook. `{{brain}}` in any
  value becomes the Brain's checkout path.
- **`[common.settings]`** holds the only settings that reach more than
  one project: every listed project gets them. Skills, agents, hooks
  and plugins are never common.
- **`[project.<name>]`** lists what one project gets. A name you choose,
  never a path or a remote. `autosync = false` turns autosync off for it.

nerve rewrites `brain.toml` in this layout when a command changes it,
so comments in it are not kept; `nerve skill update` only moves the pin.

nerve refuses `inf`, `nan`, dates and whole numbers beyond 64 bits
in settings.

## Nothing sensitive

A Brain may be public, so it holds only allowed settings, and
everything under them:

- those nerve allows: `alwaysThinkingEnabled`, `attribution`,
  `cleanupPeriodDays`, `disabledMcpjsonServers`,
  `enableAllProjectMcpServers`, `enabledMcpjsonServers`, `model`,
  `outputStyle`, `permissions.allow`, `permissions.ask`,
  `permissions.defaultMode`, `permissions.deny`, `spinnerTipsEnabled`
  and `syncClaudeAiSkills`;
- those `allowed-settings` lists, such as `"statusLine"` or
  `'extraKnownMarketplaces."acme.tools"'`.

A key that may hold secrets is never allowed, at any depth, in lists
too: `env`, `headers`, `apiKeyHelper` and the like, or a key named for a
token, secret, password, API, private, access or session key,
credential, cookie or authorization. Nor are `hooks` and
`enabledPlugins`, which nerve writes from `[hook.<name>]` and
`plugins`. `allowed-settings` cannot list them.

`check` and `sync` refuse any other setting. They also refuse home
paths (`/home/…`, `/Users/…`, `/root`), emails, token-like strings,
private keys, basic auth and credentials in URLs anywhere in
`brain.toml`: in
values, settings keys, hook matchers and commands, plugin IDs, and
names. Each refusal names the commands that move the value to a local
overlay.

The commands that write the Brain keep them out:

- `setting set` and `setting add` put a setting that is not allowed,
  or whose value is sensitive, in the project's local overlay instead.
  With `--common`, they refuse it.
- `plugin add` refuses such a plugin ID, and says how to enable it on
  this machine only.

Nor does a Brain say where a project lives.

## This machine

nerve keeps this machine's files in its config folder:

- Linux: `~/.config/nerve`, or `$XDG_CONFIG_HOME/nerve` when set.
- macOS: `~/Library/Application Support/nerve`.

Fetched skills go in its cache folder:
`~/.cache/nerve` (or `$XDG_CACHE_HOME/nerve`) on Linux,
`~/Library/Caches/nerve` on macOS.

`config.toml`, written by `nerve init`, names the Brain's checkout and
each listed checkout's project. `nerve project add` adds a checkout.

```toml
brain = "~/dev/brain"

[[checkout]]
path = "~/dev/billing"
project = "billing"
```

- **Local overlays**, `overlays/<project>.json`, hold a project's
  settings that are not allowed in a Brain, or are sensitive or
  identifying. nerve puts them there on its own when you set such a
  value. You may also write in one what `brain.toml` cannot hold, such
  as `null`. They never leave the machine, so set them again on each
  one.
  - Setting a key in the Brain drops the overlay's value for it, which
    would win.
  - `nerve setting rm` removes a key from the overlay too.
  - `nerve project rm` keeps the project's overlay, and says where it
    is: delete it once you no longer need what it holds.
- **State**, under `state/`, is what nerve last did to each working
  tree, never kept in the project.

These files are readable by you only.

## Checking a Brain

`nerve check`, in the Brain (a git repo with `brain.toml` at its top)
or anywhere when this machine names one, validates own skills and
agents, each pin, what each project lists, and the sensitive values
above. In a listed checkout, it checks this
machine's Brain, even when the project has a `brain.toml` of its own.
In the Brain's CI, run the release it pins:

```sh
go run github.com/medilies/nerve/cmd/nerve@$(sed -n 's/^nerve = "\(.*\)"$/\1/p' brain.toml) check
```
