# nerve

The Nervous system for a Brain: it carries one person's skills to every place their agents run. A Brain is a git repo you own; nerve knows nothing about what is in it beyond its layout. Words used here are defined in [GLOSSARY.md](GLOSSARY.md).

Surfaces today: Claude Code configs, and claude.ai accounts by manual upload.

## Install

```sh
go install github.com/medilies/nerve/cmd/nerve@latest
```

## A Brain

```
brain.toml               # profiles, and third-party skills pinned to a commit
skills/<name>/           # own skills
settings/base.json       # Claude Code settings for every config
settings/<profile>.json  # settings for configs that follow that profile
instructions/base.md     # standing instructions for every config
instructions/<profile>.md
keybindings.json         # Claude Code keybindings for every config
hooks/                   # any scripts settings run, referenced as {{brain}}/hooks/<script>
```

```toml
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
| Validate (in the Brain's CI) | `nerve check` |
| Read results from a script or agent | Add `--json` to any command: one JSON document with its `events` and `error` |

## What sync does

- Links every skill into `<config>/skills/` for each config in the machine file: own skills from the checkout, third-party ones from a cache at their pin.
- Removes only links it made itself (recorded in `<config>/.nerve.json`), and refuses to replace a real folder. A link with a skill's name is replaced, whoever made it.
- Links `<config>/keybindings.json` to the Brain's, so edits made from Claude Code land in the Brain. It refuses to replace a real file.
- Keeps a marked block at the top of `<config>/CLAUDE.md` that imports `instructions/base.md` and the config's profile instructions. The rest of that file is the machine's own and left alone.
- Writes `<config>/settings.json` from `settings/base.json`, then the config's profile, then the local overlay. Objects merge key by key, arrays are joined without repeats, and anything else in a later layer wins. `{{brain}}` in any value becomes the Brain's checkout path, for hook and status line scripts kept in the Brain.
- Leaves `settings.json` alone when a key was changed outside the Brain since nerve last wrote it (by `/model` or `/config`, say) and the Brain disagrees. It prints each such key as drift and fails: move the change into a layer, or `sync --discard`.
- Builds `dist/<name>.zip` in the Brain for every skill claude.ai accepts. A skill whose frontmatter claude.ai rejects is listed as Claude Code only.
- Reports drift on claude.ai. Each config downloads its account's claude.ai skills into `<config>/skills/synced/`, and sync checks that a copy there holds every file of the skill unchanged, printing `missing` or `stale` with the zip to upload.

claude.ai only takes skills uploaded by hand (Customize > Skills > Upload a skill). After a `sync` reports drift, upload the named zips to that account.

## Notes

- `check` refuses settings that may hold secrets, such as `env` and `apiKeyHelper`, in the Brain.

- A personal skill always wins over a project's skill of the same name; nothing changes that. Own skills can defer to a project in their own text.
- Decisions: [docs/adr/](docs/adr/).
