# The Brain

A Brain is a git repo you own. nerve knows nothing about what is in it
beyond its layout:

```text
brain.toml          # the nerve release, autosync, third-party pins, hooks, common settings, projects
skills/<name>/      # own skills
agents/<name>.md    # own agents
hooks/<script>      # scripts hooks run, referenced as {{brain}}/hooks/<script>
```

Words used here are defined in [GLOSSARY.md](../GLOSSARY.md); the
reasoning is in [ADR 0005](adr/0005-projects-opt-in.md).

## brain.toml

```toml
nerve = "v0.3.0"                 # the release that carries this Brain
nerve-checksums = "9f86d0…"      # optional: SHA-256 of that release's checksums.txt
autosync = true                  # sync each project at session start; the default

[[source]]                       # third-party skills, pinned to a full commit
repo = "acme/skills"
sha = "5863806dd4e15c63174bee4e7515a4256792ecfa"
skills = ["grilling"]

[hook.go-fmt]                    # a hook projects can take by name
event = "PostToolUse"
matcher = "Edit|Write"
command = "{{brain}}/hooks/go-fmt.sh"

[common.settings]                # settings every listed project gets
syncClaudeAiSkills = false

[project.billing]                # what one project gets, and nothing else does
skills = ["go-stack", "grilling"]
agents = ["reviewer"]
hooks = ["go-fmt"]
plugins = ["gopls-lsp@claude-plugins-official"]

[project.billing.settings]
model = "opus"

[project.claude-ai-work]         # a claude.ai account: skills only
surface = "claude.ai"
skills = ["research"]
```

- **`nerve`** pins the release that carries the Brain. `sync` brings
  the installed nerve to it, checking the download against the
  release's `checksums.txt`, and that file against `nerve-checksums`
  when set. A development build is never replaced.
- **`[[source]]`** is a third-party repo on GitHub at a full commit,
  and the skills used from it. A skill is the folder of that name
  holding a `SKILL.md`, or the whole repo when its root `SKILL.md`
  names it. `nerve skill update <repo> [sha]` shows the upstream diff,
  then moves the pin.
- **`[hook.<name>]`** defines a Claude Code hook. `{{brain}}` in any
  value becomes the Brain's checkout path.
- **`[common.settings]`** holds the only settings that reach more than
  one project: every listed project gets them. Skills, agents, hooks
  and plugins are never common.
- **`[project.<name>]`** lists what one project gets. A name you choose,
  never a path or a remote. `autosync = false` turns autosync off for it.
- **`surface = "claude.ai"`** makes a project a claude.ai account,
  which takes skills only. See [Syncing](sync.md#claudeai).

nerve rewrites `brain.toml` in this layout when a command changes it,
so comments in it are not kept; `nerve skill update` only moves the pin.

## Nothing sensitive

A Brain may be public. `check` and `sync` refuse, anywhere in
`brain.toml`, secret-bearing settings such as `env` and `apiKeyHelper`,
home paths, emails and token-like strings. Nor does a Brain say where
a project lives, or name a claude.ai account by its email.

## This machine

`~/.config/nerve/config.toml`, written by `nerve init`, names the
Brain's checkout and each listed checkout's project. `nerve project
add` adds a checkout.

```toml
brain = "~/dev/brain"

[[checkout]]
path = "~/dev/billing"
project = "billing"
```

- **Local overlays**, `~/.config/nerve/overlays/<project>.json`, hold a
  project's settings that are sensitive or identifying. nerve puts them
  there on its own when it imports a project or sets a value. They
  never leave the machine, so set them again on each one.
- **State**, under `~/.config/nerve/state/`, is what nerve last did to
  each working tree, never kept in the project.

## Checking a Brain

`nerve check`, in the Brain or anywhere when this machine names one,
validates own skills and agents, each pin, what each project lists, and
the sensitive values above. In the Brain's CI, run the release it pins:

```sh
go run github.com/medilies/nerve/cmd/nerve@$(sed -n 's/^nerve = "\(.*\)"$/\1/p' brain.toml) check
```
