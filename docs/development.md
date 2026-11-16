# Development

| To | Do |
| --- | --- |
| Test | `go test ./...` |
| Lint | `gofmt -l .` and `go vet ./...` |
| Release | Tag `vX.Y.Z` on `main` and push the tag. [release.yml](../.github/workflows/release.yml) tests, builds Linux and macOS binaries, and publishes them with `checksums.txt`. |

A build without a release tag reports its version as `(devel)` or a Go pseudo-version, and `sync` never replaces it.
