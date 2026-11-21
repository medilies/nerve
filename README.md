# nerve

[![CI](https://github.com/medilies/nerve/actions/workflows/ci.yml/badge.svg)](https://github.com/medilies/nerve/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/medilies/nerve)](https://github.com/medilies/nerve/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/medilies/nerve)](go.mod)
[![License: MIT](https://img.shields.io/github/license/medilies/nerve)](LICENSE)

Your Claude setup, only where it's needed.

nerve carries your Brain, a git repo of your skills, agents, hooks and
settings, into the projects that list what they need from it, and
nowhere else.

- **Only where needed.** A project gets the skills, agents, hooks,
  plugins and settings it lists. Nothing reaches every project, so no
  context is wasted and no tool a project needs is denied.
- **Yours, not the team's.** Everything lands in
  `.claude/settings.local.json` and in links git never sees. Teammates
  see none of it, and the team's own `.claude/` stays theirs.
- **One source of truth.** `brain.toml` says what each project gets.
  A project only reflects it, and every change is a commit you review.
- **In step at every session.** Autosync brings a project up to date
  when a session starts, and tells you what changed.
- **Revoke everywhere.** Take a skill or a denied tool out of every
  project with one command; each checkout drops it at its next sync.
- **Nothing lost.** A setting Claude Code changed is reported as drift,
  never overwritten. A link that goes missing is made again.
- **Pinned and reviewed.** Third-party skills sit at a commit, and move
  only after you read how they changed.
- **Nothing sensitive in the Brain.** Secrets, home paths and emails
  are refused, and go to a local overlay that never leaves the machine.
- **Ask Claude.** In a listed project, `/nerve` says where things stand,
  and does the rest with you: "use my Go stack here".

## A Brain

A git repo you own: `brain.toml`, with your own skills, agents and hook
scripts beside it. `brain.toml` says what each project gets:

```toml
[project.billing]
skills = ["go-stack"]
plugins = ["gopls-lsp@claude-plugins-official"]

[project.billing.settings]
model = "opus"
```

[The Brain](docs/brain.md) has the whole layout and every key.

## Install

### Linux and macOS

[![Linux](https://img.shields.io/badge/Linux-amd64%20%7C%20arm64-FCC624?logo=linux&logoColor=black)](https://github.com/medilies/nerve/releases/latest)
[![macOS](https://img.shields.io/badge/macOS-Intel%20%7C%20Apple%20silicon-000000?logo=apple&logoColor=white)](https://github.com/medilies/nerve/releases/latest)

Download a binary from the
[latest release](https://github.com/medilies/nerve/releases/latest)
onto your `PATH` as `nerve`.

### From source

![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)

```sh
go install github.com/medilies/nerve/cmd/nerve@latest   # puts nerve in ~/go/bin
```

### Then, on each machine

nerve needs git. Name your Brain's checkout once:

```sh
nerve init ~/dev/brain
```

From then on, `sync` keeps nerve at the release the Brain pins.

## Usage

```sh
nerve project add billing            # list this checkout as project billing, keeping its settings.local.json
nerve skill add go-stack             # give it a skill; agent, hook and plugin work the same
nerve setting set model opus         # a setting for it; --common for every project
nerve setting add permissions.allow "Bash(make test)"   # one more item in a list
nerve sync --here                    # carry the Brain to it; autosync does this at each session start
nerve status                         # where it stands: links, the team's skills, drift
nerve skill rm grilling --all        # take a skill out of every project
nerve setting rm permissions.deny=Cron --all   # revoke one denied tool everywhere
nerve skill update acme/skills       # read how its skills changed, then move the pin
nerve project autosync off           # no sync at session start for this project
nerve sync                           # every checkout on this machine, and the claude.ai zips
nerve check                          # validate the Brain, as its CI does
```

A command that acts on one project, such as `skill add` or
`setting set`, acts on the one around the working directory;
`--project <name>` names another. Commands that change the Brain edit
`brain.toml` and never commit. `--json` prints one JSON document of
what happened. A settings key that holds a dot goes in double quotes:
`enabledPlugins."x@y.z"`. `nerve help` lists every command.

## Documentation

- [The Brain](docs/brain.md): `brain.toml`, projects, sources, hooks, common settings, this machine, overlays
- [Syncing](docs/sync.md): what sync writes, autosync, drift, revoking, the team's own, claude.ai
- [Glossary](GLOSSARY.md): the words nerve uses
- [Decisions](docs/adr/) and [what is left out](docs/backlog.md)
- [Development](docs/development.md): building, testing, architecture
