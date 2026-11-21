# Development

| To | Do |
| --- | --- |
| Test | `go test ./...`; one CLI script: `go test ./cmd/nerve -run TestScripts/<name>` |
| Run the tests of open bugs | `NERVE_OPEN_BUGS=1 go test ./...`: each of them fails until its bug is fixed |
| Lint | `golangci-lint run` (v2.14.0, as in CI): vet, the standard linters, gofmt, goimports, and depguard for the layers |
| Release | Tag `vX.Y.Z` on `main` and push the tag. [release.yml](../.github/workflows/release.yml) tests, builds Linux and macOS binaries, and publishes them with `checksums.txt`. |

A build without a release tag reports its version as `(devel)` or a Go pseudo-version, and `sync` says nothing of the pin to it.

## Design

One bounded context in hexagonal layers ([ADR 0006](adr/0006-hexagonal-layers.md)):

```text
cmd/nerve/              flags, usage, wiring, the embedded skill; end-to-end tests
internal/brain/domain/  the Brain and its rules; the standard library only
internal/brain/app/     use cases, events, ports; the standard library and domain only
internal/brain/infra/   adapters: brain.toml, machine files, git, caches, links, reports
```

Dependencies point inward: `.golangci.yml`'s depguard rules refuse anything else in `domain` and `app`. A new concept takes the words of [GLOSSARY.md](../GLOSSARY.md).

Tests sit next to the code they test:

- `domain`: the rules, on plain values.
- `app`: the use cases, on the in-memory ports in `fakes_test.go`.
- `infra`: the adapters, on a temp HOME with real files, git and a local HTTP server.
- `cmd/nerve`: the CLI end to end, as [testscript](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript) scripts in `testdata/script/`, one per behaviour. Each runs the real `nerve` and git on a temp HOME holding a Brain at `brain` and a project checkout at `proj`. `script_test.go` sets it up, sets `$NERVE_CONFIG` and `$NERVE_CACHE` to nerve's own folders, and adds the commands `link`, `perm` and `replace`.

A test of a bug not fixed yet asserts the right behaviour, and is skipped:

- in Go, it starts with `openBug(t, "<the wrong behaviour>")`, a helper in the package's `bugs_test.go`, added with its first open bug and deleted with its last;
- in a script, with `[!bugs] skip 'open bug: <the wrong behaviour>'`, and its file name starts with `bug_`.

Fixing the bug deletes that line, so the test must pass.
