# nerve

The Nervous system for a Brain: it carries one person's skills to every place their agents run. A Brain is a git repo you own; nerve knows nothing about what is in it beyond its layout. Words used here are defined in [GLOSSARY.md](GLOSSARY.md).

Surfaces today: Claude Code configs, and claude.ai accounts by manual upload.

## Install

Download the binary for your system from the [releases](https://github.com/medilies/nerve/releases) onto your `PATH` as `nerve`, or:

```sh
go install github.com/medilies/nerve/cmd/nerve@latest
```

Then, on each machine, write its machine file once with `nerve init` (see [This machine](#this-machine)) and run `nerve sync`. From then on, use nerve through its skill.

## The nerve skill

`sync` installs a `nerve` skill into every config, carried by the binary so the two always match. Through it you ask Claude to sync, check or move a pin, to sort out settings drift, or to remember something ("remember that I always…"): it picks the right place in the Brain, shows the diff, and commits, pushes and syncs once you agree. It is never zipped for claude.ai, which has neither the CLI nor the Brain.

## A Brain

```
brain.toml               # the nerve release, profiles, and third-party skills pinned to a commit
.gitignore               # /dist/: the zips sync builds for claude.ai
skills/<name>/           # own skills
settings/base.json       # Claude Code settings for every config
settings/<profile>.json  # settings for configs that follow that profile
instructions/base.md     # standing instructions for every config
instructions/<profile>.md
keybindings.json         # Claude Code keybindings for every config
hooks/                   # any scripts settings run, referenced as {{brain}}/hooks/<script>
```

```toml
nerve = "v0.1.0"                 # the release that carries this Brain
nerve-checksums = "9f86d0…"      # optional: SHA-256 of that release's checksums.txt
profiles = ["personal", "work"]

[[source]]
repo = "acme/skills"
sha = "5863806dd4e15c63174bee4e7515a4256792ecfa"   # full commit SHA
skills = ["grilling", "research"]
```

## This machine

Each machine says where its Brain is checked out and which profile each Claude Code config follows, in `~/.config/nerve/config.toml`. Write it once:

```sh
nerve init ~/dev/brain ~/.claude=work ~/.claude-personal=personal
```

Settings that are sensitive or identifying never go in the Brain: put them in a local overlay, `~/.config/nerve/overlays/<profile>.json` (`default.json` for configs that follow no profile).

`sync` carries the machine's Brain. `check` and `update` work on the Brain around the working directory, else on the machine's.

## Use

| To | Do |
| --- | --- |
| Install or refresh everything | `nerve sync` |
| See where the Brain is, each config's profile, and nerve's version | `nerve status` |
| Edit or add an own skill | Change `skills/<name>/`, then `sync`. Linked from the checkout, so edits are live at once. |
| Add a third-party skill | Add it to a `[[source]]` in `brain.toml` (a new source needs a full commit SHA), then `sync` |
| Move a pin | `nerve update <repo> [sha]`: shows the upstream diff, then rewrites the pin. Review it, commit, `sync`. |
| Remove a skill | Delete it from `brain.toml` or `skills/`, then `sync` |
| Change standing instructions | Edit `instructions/base.md` or `instructions/<profile>.md`: imported, so live at the next session |
| Change a setting | Edit `settings/base.json`, `settings/<profile>.json` or the local overlay, then `sync` |
| Drop settings changed in Claude Code | `nerve sync --discard` |
| Move to a new nerve release | Change `nerve` in `brain.toml`, commit, `sync` |
| Validate (in the Brain's CI) | `go run github.com/medilies/nerve/cmd/nerve@$(sed -n 's/^nerve = "\(.*\)"$/\1/p' brain.toml) check` |
| Read results from a script or agent | Add `--json` to any command: one JSON document with its `events` and `error` |

## What sync does

- Pulls the Brain first, fast-forward only. It skips the pull while tracked files have uncommitted changes or the checkout has no upstream, and fails if the checkout has diverged.
- Brings nerve itself to the Brain's `nerve` release: it downloads that release, checks it against the release's `checksums.txt` (and that file against `nerve-checksums`, if set), replaces itself and runs the new binary in its place. A development build (built from source, not a release) is kept. If it cannot replace its own binary, it fails and says so: install the pinned release by hand.
- Links every skill into `<config>/skills/` for each config in the machine file: own skills from the checkout, third-party ones from a cache at their pin, and the nerve skill from the binary.
- Removes only links it made itself (recorded in `<config>/.nerve.json`), and refuses to replace a real folder. A link with a skill's name is replaced, whoever made it.
- Links `<config>/keybindings.json` to the Brain's, so edits made from Claude Code land in the Brain. It refuses to replace a real file.
- Keeps a marked block at the top of `<config>/CLAUDE.md` that imports `instructions/base.md` and the config's profile instructions. The rest of that file is the machine's own and left alone.
- Writes `<config>/settings.json` from `settings/base.json`, then the config's profile, then the local overlay. Objects merge key by key, arrays are joined without repeats, and anything else in a later layer wins. `{{brain}}` in any value becomes the Brain's checkout path, for hook and status line scripts kept in the Brain.
- Leaves `settings.json` alone when a key was changed outside the Brain since nerve last wrote it (by `/model` or `/config`, say) and the Brain disagrees. It prints each such key as drift and fails: move the change into a layer, or `sync --discard`.
- Builds `dist/<name>.zip` in the Brain for every skill claude.ai accepts. A skill whose frontmatter claude.ai rejects, or that holds a link to a folder, is listed as Claude Code only.
- Reports drift on claude.ai. Each config downloads its account's claude.ai skills into `<config>/skills/synced/`, and sync checks that a copy there holds every file of the skill unchanged, printing `missing` or `stale` with the zip to upload.

claude.ai only takes skills uploaded by hand (Customize > Skills > Upload a skill). After a `sync` reports drift, upload the named zips to that account.

## Notes

- `check` and `sync` refuse settings that may hold secrets, such as `env` and `apiKeyHelper`, in the Brain.

- Third-party skills and the nerve skill are linked from nerve's folder in the user cache. If the system clears that cache, those links break until the next `sync`.
- `nerve-checksums` guards against a release replaced after you pinned it: `curl -sL https://github.com/medilies/nerve/releases/download/<tag>/checksums.txt | sha256sum`.
- A personal skill always wins over a project's skill of the same name; nothing changes that. Own skills can defer to a project in their own text.
- Decisions: [docs/adr/](docs/adr/).
