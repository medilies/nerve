# nerve

The Nervous system for a Brain: it carries one person's skills to every place their agents run. A Brain is a git repo you own; nerve knows nothing about what is in it beyond its layout. Words used here are defined in [GLOSSARY.md](GLOSSARY.md).

Surfaces today: Claude Code configs, and claude.ai accounts by manual upload.

## Install

```sh
go install github.com/medilies/nerve/cmd/nerve@latest
```

## A Brain

```
skills.toml        # Claude Code configs, and third-party skills pinned to a commit
skills/<name>/     # own skills
```

```toml
configs = ["~/.claude"]

[[source]]
repo = "acme/skills"
sha = "5863806dd4e15c63174bee4e7515a4256792ecfa"   # full commit SHA
skills = ["grilling", "research"]
```

Run nerve from anywhere inside the Brain's checkout.

## Use

| To | Do |
| --- | --- |
| Install or refresh everything | `nerve sync` |
| Edit or add an own skill | Change `skills/<name>/`, then `sync`. Linked from the checkout, so edits are live at once. |
| Add a third-party skill | Add it to a `[[source]]` in `skills.toml` (a new source needs a full commit SHA), then `sync` |
| Move a pin | `nerve update <repo> [sha]`: shows the upstream diff, then rewrites the pin. Review it, commit, `sync`. |
| Remove a skill | Delete it from `skills.toml` or `skills/`, then `sync` |
| Validate (in the Brain's CI) | `nerve check` |

## What sync does

- Links every skill into `<config>/skills/` for each config: own skills from the checkout, third-party ones from a cache at their pin.
- Removes only links it made itself (listed in `<config>/skills/.skills-sync.json`), and refuses to replace a real folder. A link with a skill's name is replaced, whoever made it.
- Builds `dist/<name>.zip` in the Brain for every skill claude.ai accepts. A skill whose frontmatter claude.ai rejects is listed as Claude Code only.
- Reports drift on claude.ai. Each config downloads its account's claude.ai skills into `<config>/skills/synced/`, and sync checks that a copy there holds every file of the skill unchanged, printing `missing` or `stale` with the zip to upload.

claude.ai only takes skills uploaded by hand (Customize > Skills > Upload a skill). After a `sync` reports drift, upload the named zips to that account.

## Notes

- A personal skill always wins over a project's skill of the same name; nothing changes that. Own skills can defer to a project in their own text.
- Decisions: [docs/adr/](docs/adr/).
