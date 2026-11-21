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
  plugins and settings it lists. Only the few common settings reach
  every project, so no context is wasted and no tool a project needs is
  denied.
- **Yours, not the team's.** Everything lands in
  `.claude/settings.local.json` and in links git never sees. Teammates
  see none of it, and the team's own `.claude/` stays theirs.
- **One source of truth.** `brain.json` says what each project gets.
  A command that changes it takes the remote's first, then pushes the
  change as one commit, so every machine reads the same file.
- **Explicit.** nerve changes a project only when you sync, update or
  remove it. Each session starts with a report of what waits for a
  sync, and of drift.
- **Revoke everywhere.** Take a skill or a denied tool out of every
  project with one command; each checkout drops it at its next sync.
- **Nothing lost.** A setting the Brain does not set, one the checkout
  had before nerve or an approval, is kept. One the Brain sets that
  Claude Code changed is drift: a sync fails on it, and `-f` overwrites
  it. A link that goes missing is made again.
- **Pinned.** Third-party skills sit at a commit. `nerve update` shows
  how they changed as it moves the pin, then syncs the project here.
- **Only allowed settings in the Brain.** It holds the settings nerve
  allows, and those you allow. Any other setting goes to a local overlay
  that never leaves the machine. What you allow is yours to keep free of
  secrets.
- **Ask Claude.** In a listed project, `/nerve` says where things stand,
  and does the rest with you: "use my Go stack here".

## A Brain

A git repo you own: `brain.json`, with your own skills, agents and hook
scripts beside it. `brain.json` says what each project gets:

```json
{
  "projects": {
    "billing": {
      "skills": ["go-stack"],
      "plugins": ["gopls-lsp@claude-plugins-official"],
      "settings": {"model": "opus"}
    }
  }
}
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

nerve needs git 2.31 or later. Your Brain is a git repo with a
`brain.json` at its root, empty to begin with, pushed to a remote that
its branch tracks (`git push -u`). Name its checkout once:

```sh
nerve init ~/dev/brain
```

## Usage

```sh
nerve project add billing            # list this checkout as project billing
nerve push "add go-stack"            # commit the Brain's own files, such as a new skill, and push them
nerve skill add go-stack             # give it a skill; agent, hook and plugin work the same
nerve setting set model opus         # a setting for it; --common for every project
nerve setting add permissions.allow "Bash(make test)"   # one more item in a list
nerve sync --here                    # make the Brain its remote's, then carry it to this checkout
nerve status                         # where it stands: what waits for a sync, links, drift
nerve skill rm go-stack --all        # take a skill out of every project
nerve setting rm permissions.deny=Cron --all   # revoke one denied tool everywhere
nerve update                         # show how third-party skills changed, move their pins, sync here
nerve sync                           # every checkout on this machine
nerve check                          # validate the Brain, as its CI does
```

A command that acts on one project, such as `skill add` or
`setting set`, acts on the one around the working directory;
`--project <name>` names another. `nerve push` sends what other
commands do not, such as a new own skill, and `nerve sync` refuses to
drop what was not pushed, unless `-f`. `--json` prints one JSON
document of what happened. A settings key that holds a dot goes in
double quotes: `enabledPlugins."x@y.z"`. `nerve help` lists every
command.

## Documentation

- [The Brain](docs/brain.md): `brain.json`, projects, sources, hooks, common settings, this machine, overlays
- [Syncing](docs/sync.md): what sync writes, push, the session report, drift, revoking, the team's own
- [Glossary](GLOSSARY.md): the words nerve uses
- [Decisions](docs/adr/) and [what is left out](docs/backlog.md)
- [Development](docs/development.md): building, testing, architecture
