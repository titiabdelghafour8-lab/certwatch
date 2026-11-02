# Contributing

Thanks for helping improve certwatch!

## Development setup

Requirements: Go 1.26+ and Docker.

```sh
make up                 # postgres + mailpit + server on :8080
make test               # unit tests
make test-integration   # unit + PostgreSQL integration tests
make lint               # golangci-lint
go run ./cmd/certwatch --api-url http://localhost:8080 signup --email you@example.com
```

Emails sent by the local server are visible at http://localhost:8025.

## Guidelines

- Read [docs/architecture.md](docs/architecture.md) first. Keep decision logic pure (`alerting`, `checker`) and I/O at the edges (`store`, `notify`, `api`).
- Every behaviour change comes with a test. Database behaviour is tested against real PostgreSQL, not mocks.
- Schema changes go in a **new** migration file. Released migrations are never edited.
- The JSON wire format (`internal/model`) and the webhook payload are public contracts. Add fields freely, but never rename or remove them within v1.
- Update `CHANGELOG.md` under *Unreleased*.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org) (`feat:`, `fix:`, `docs:`...). The release changelog is generated from them.

## Releasing (maintainers)

1. Move the *Unreleased* changelog entries under the new version.
2. `git tag vX.Y.Z && git push origin vX.Y.Z`. CI publishes the binaries and images.
