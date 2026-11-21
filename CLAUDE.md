# Project rules

## Design

- One bounded context in hexagonal layers: `internal/brain/{domain,app,infra}` plus a thin `cmd/nerve` layer ([ADR 0006](docs/adr/0006-hexagonal-layers.md)). `domain` imports only the standard library, and `app` only that and `domain`; enforced by the `depguard` allow-lists in `.golangci.yml`.
- `domain` holds the Brain and its rules. `app` holds the use cases, the events they report and the ports they need. `infra` holds every adapter, such as `brain.json`, the machine file, git, caches, links and reports. A new adapter needs a port in `app` first.
- Minimalism: add only what is needed now. No speculative abstractions, unused dependencies or placeholder files. Ideas wait in `docs/backlog.md`.
- Only where needed ([ADR 0005](docs/adr/0005-projects-opt-in.md)): nerve writes only in listed projects, never at user level, and only what their entry names. Every effect is visible and reported. It removes only what it made, and never edits `.gitignore`.
- [core.md](core.md) is the reference for what nerve is. Where an ADR disagrees with it, a new ADR follows core.md.
- Explicit verbs ([ADR 0009](docs/adr/0009-explicit-verbs.md), [ADR 0010](docs/adr/0010-brain-toml-in-step.md)): nerve changes the Brain, its remote or a project only when a command says so; otherwise it reports. `sync` brings the remote here, `push` sends the Brain there, `update` moves pins and syncs here. A command that changes `brain.json` pushes it at once.
- nerve writes nothing of this machine into the Brain: no checkout paths or repo remotes, which stay in this machine's files. What an allowed setting holds is the user's to judge.

## Ubiquitous language

Code, `brain.json`, the CLI, the nerve skill, docs and commits use the words of [GLOSSARY.md](GLOSSARY.md), and respect its _Avoid_ notes. A new concept gets an entry there before it gets code. When code and user-facing words disagree, the code is renamed.

If your output contradicts an ADR in `docs/adr/`, say so explicitly rather than silently overriding it.

## Environment

- `go.mod` is the source of truth for the Go version (`go 1.26.0`). CI reads it through setup-go's `go-version-file`.
- No database. The Brain is a git repo with `brain.json` at its root. This machine's file, local overlays and state live under `os.UserConfigDir()/nerve/`, read and written through the ports in `app`.

## Tests

- Test first: write the failing test before the code, for features as well as bugs, and see it fail before making it pass.
- Meaningful tests over coverage: test behaviour through public APIs, one reason to fail per test, descriptive names. No coverage targets.
- Tests sit next to the code they test. `domain` tests use plain values, and `app` tests use the in-memory fakes in `fakes_test.go`. `infra` tests may touch disk, git and a local HTTP server, since they test the adapters themselves. `cmd/nerve` tests the CLI end to end with testscript scripts in `testdata/script/`.
- A test of an open bug is skipped with `openBug` (Go) or `[!bugs] skip` (script), and fails under `NERVE_OPEN_BUGS=1`. A fix deletes that line in the same commit.
- Tests never touch the user's real HOME, git config, Brain or network. `infra` tests run in the temp HOME of `env_test.go`.
- Never run `go test` with `-v`, because its output floods the context. A failure prints without it, and `-run` narrows it down.

## Docs

- A change updates, in the same commit, every doc it makes wrong:
  - the README, `docs/brain.md` and `docs/sync.md`
  - the embedded nerve skill (`cmd/nerve/skill/SKILL.md`) and `nerve help`
  - `GLOSSARY.md`, `docs/development.md`, `docs/backlog.md` and `docs/not-bugs.md`
  - this file
- Before committing, grep the docs for each command, flag, `brain.json` key, file and identifier the change renames or removes.
- Each fact lives in one doc, and the others link to it:
  - Users: the README (what nerve is, install, usage), `brain.md` (`brain.json`, this machine, checking a Brain), `sync.md` (what sync and push do, the session report, drift).
  - Agents in users' projects: the nerve skill.
  - Contributors: `development.md` (commands, design, releasing), `backlog.md` (ideas left out), `not-bugs.md` (behaviour kept on purpose).
  - Agents here: this file.
- ADRs in `docs/adr/`, plans in `docs/plans/`, `conversation-notes.md` and `review-feat-projects.md` are records. They aren't updated, apart from a status line.
- Never list in full what the code lists (use cases, ports, adapters, events). Name a few as examples, so the list can't go stale.
- User docs say what nerve does, not how. Leave out internals, the reasons behind workarounds, and history. History is in git, and decisions are in ADRs.
- Output shown in a doc is copied from a real run on a temp HOME, never written by hand.
- Short sentences, and lists over long paragraphs.
- A review checks the docs a change touches against these rules.

## Workflow

- At the end of each iteration of work, run the tests and the lint (commands in `docs/development.md`), and fix what fails before going on.
- At the end of each change, ask: review then commit / commit then review / commit without review.
- Reviews always run in a fresh subagent that has not seen the work, never in the main context.
- A review reports what `docs/not-bugs.md` lists as ignored, not as findings. Each finding is then fixed, dropped with the feature that causes it, or added there.
- Commits follow Conventional Commits v1.0.0.
- Use a Mermaid diagram (or an inline visual) when an explanation would otherwise be long.

## Commands

The test, lint and release commands are in `docs/development.md`.

Navigate Go code with gopls through the LSP tool: definitions, references, implementers of a port, callers. Grep for text, docs and `brain.json`, which gopls doesn't read. A rename still greps the docs for the old word.
