# Development

| To | Do |
| --- | --- |
| Test | `go test ./...` |
| Lint | `golangci-lint run` (v2.14.0, as in CI): vet, the standard linters, gofmt, goimports, and depguard for the layers |
| Release | Tag `vX.Y.Z` on `main` and push the tag. [release.yml](../.github/workflows/release.yml) tests, builds Linux and macOS binaries, and publishes them with `checksums.txt`. |

A build without a release tag reports its version as `(devel)` or a Go pseudo-version, and `sync` never replaces it.

## Design

One bounded context in hexagonal layers ([ADR 0006](adr/0006-hexagonal-layers.md)):

```text
cmd/nerve/              flags, usage, wiring, the embedded skill; end-to-end tests
internal/brain/domain/  the Brain and its rules; the standard library only
internal/brain/app/     use cases, events, ports; the standard library and domain only
internal/brain/infra/   adapters: brain.toml, machine files, git, caches, links, release, reports
```

Dependencies point inward: `.golangci.yml`'s depguard rules refuse anything else in `domain` and `app`. A new concept takes the words of [GLOSSARY.md](../GLOSSARY.md).
