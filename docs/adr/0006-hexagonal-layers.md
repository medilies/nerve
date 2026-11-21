# One bounded context in hexagonal layers, held by depguard

Status: accepted, 2026-11-20. The release upgrade adapter went with [ADR 0007](0007-drop-guessing-features.md), the zips with [ADR 0008](0008-drop-claude-ai-accounts.md), and the import of a project's settings with the amendment of [ADR 0005](0005-projects-opt-in.md). `brain.toml` became `brain.json` with [ADR 0011](0011-json-files.md).

## Context

nerve was one `main` package. Its rules (what a project gets, how settings are laid and reconciled, what must never enter the Brain) sat beside TOML decoding, git calls, symlinks and HTTP downloads, so any file could reach anything. ADR 0005 made those rules the heart of the tool, and they are the part most worth keeping clear of the rest.

## Options

- **Keep one package.** Least code, but nothing stops the rules from drifting into the adapters.
- **Packages by technical concern** (`toml`, `git`, `settings`). Splits files, not dependencies: the rules still import whatever they need.
- **One bounded context in hexagonal layers**, as in weaver: `domain`, `app` and `infra`, dependencies pointing inward, checked by the linter.

## Decision

- **One bounded context, `internal/brain`**: carrying a Brain to the projects that list what they need. Its words are those of [GLOSSARY.md](../../GLOSSARY.md).
- **`domain`**: the Brain and its rules: the manifest and projects, settings layering, reconciliation and drift, the import of a project's settings, sensitive values, frontmatter. It knows no file format, git or filesystem.
- **`app`**: the use cases (sync, project, skill/agent/hook/plugin, setting, status, check, pin update), the events they report, and the ports they need: the Brain checkout, sources, this machine's files, the working trees, nerve itself, zips.
- **`infra`**: every adapter: `brain.toml` and the machine file, JSON settings, git, the source and zip caches, links, the release upgrade, and the text, JSON and hook reports.
- **`cmd/nerve`**: flags, usage, wiring, and the embedded skill. The end-to-end tests stay here, driving the CLI.
- **depguard** in `.golangci.yml` allows `domain` only the standard library, and `app` only that and `domain`. CI runs it.

## Consequences

- A rule changes in `domain` without touching an adapter, and an adapter changes without touching a rule.
- A new adapter needs a port in `app` first; a new dependency in `domain` or `app` fails the lint.
- The manifest has a domain model and a TOML shape in `infra`, mapped both ways.
- Go's import cycles already forbid `domain` or `app` importing `infra`; depguard adds the third-party libraries.
