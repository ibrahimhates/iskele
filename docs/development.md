# Development

Requirements: **Go 1.25+**, **Node 20+**. Docker is optional — most of the
codebase can be worked on without it, and the test suite never touches a real
engine.

## Getting started

```sh
git clone https://github.com/ibrahimhates/iskele
cd iskele
make build      # frontend + binary -> bin/iskeled
make run        # 127.0.0.1:8377, data in ./.data, debug logging
```

Frontend work with live reload takes two terminals:

```sh
make run        # the daemon
make web-dev    # Vite on :5173, proxying /api to the daemon
```

`make build-go` skips the frontend, for backend work on a machine without Node.
The binary then serves a short page instead of the UI, explaining how to build
the real one; the API is unaffected.

## Make targets

```sh
make help       # all of them
make build      # frontend + binary
make build-go   # binary only
make test       # go test -race
make test-cover # with coverage, as CI measures it
make check      # gofmt + vet + test
make lint       # golangci-lint
make vuln       # govulncheck
make web-test   # frontend suite
make web-lint   # eslint + prettier
make gen-api    # regenerate TypeScript types from docs/openapi.yaml
make run        # a local daemon with development defaults
```

## Before pushing

```sh
make check && make lint && make web-test && make web-lint
```

CI runs all of it plus a bundle check (the binary must actually serve its own
UI) and three cross-compiles.

Two failure modes are worth knowing in advance:

**API types drift.** `make gen-api` regenerates TypeScript from
`docs/openapi.yaml`; commit the result. Separately,
`web/src/api/conformance.ts` compares the hand-written types in `types.ts`
against the generated ones at compile time. Add a field to one and not the
other, and the frontend build fails — which is the point.

**Tests need `-race` and cgo.** `make test` handles this. `CGO_ENABLED=0
go test -race` silently cannot run the detector.

## Testing

The suite runs with no Docker daemon and no network. `internal/docker/fake`
implements the same `Client` interface as the real engine wrapper, so services
and handlers are exercised end to end against it.

What that buys: the security boundaries are covered by tests that run on every
push, rather than a manual checklist. The RBAC matrix, the path guard, the
privileged gate and the bootstrap-once rule are all pinned.

Conventions worth following:

- **Name the behaviour.** `TestRenderSkipsAnUnansweredOptionalBind`, not
  `TestRender3`.
- **Test against a standard where one exists.** The TOTP tests run RFC 6238's
  own vectors, because a test that only compares our output to itself would
  pass a wrong-but-consistent implementation.
- **Do not let timing decide.** If a test needs an operation to still be
  running, hold it — `fake.HoldBuilds()` exists for exactly that. A test that
  is red under load is a test nobody trusts.

`internal/server` is the slowest package by a wide margin: it builds a real
store and a real auth service per test, and argon2id is deliberately expensive.
Roughly five minutes under `-race`.

## Layout

See [`architecture.md`](architecture.md). The rule that matters most while
editing: **handlers do HTTP, services decide, `internal/docker` talks to the
engine.** A handler that imports the Docker SDK, or a service that writes a
status code, is in the wrong layer.

`internal/docker` and `internal/hostinfo` are the only packages allowed to
import the Docker SDK and gopsutil respectively.

## Changing the API

1. Edit `docs/openapi.yaml` first. It is the specification, not documentation
   written afterwards.
2. `make gen-api`.
3. Update `web/src/api/types.ts` and add a line to `conformance.ts`.
4. Implement the handler and the service.
5. Add the route to `internal/server/router.go` with the permission it needs.
6. Add it to the RBAC table in `internal/server/rbac_test.go`, or write a
   dedicated permission test.
7. Update the endpoint table in `README.md`.

## Decisions

`DECISIONS.md` records anything a future maintainer might reasonably want to
undo: context, decision, rationale, consequence. It is in Turkish, matching the
project's internal documents; code and comments are in English.

If a PR description contains "we should probably…", that is a decision.

## Cutting a release

1. Update `CHANGELOG.md` — move `[Unreleased]` into a version with a date.
2. Make sure `ACCEPTANCE.md` reflects reality.
3. Commit, tag, push:

```sh
git tag -a v0.1.0 -m "Iskele v0.1.0"
git push origin v0.1.0
```

The tag triggers `.github/workflows/release.yml`, which re-runs the tests
against the tagged commit — a tag can point at a commit no branch build ever
saw — and then runs GoReleaser: three architectures, `.tar.gz`, `.deb`,
`.rpm`, SHA-256 checksums and an SBOM per archive.

Try it without publishing:

```sh
go run github.com/goreleaser/goreleaser/v2@latest check
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
```

The `before` hook runs `make web` first, because the binary embeds `web/dist`.
A release built without it compiles cleanly and serves a blank page — which is
why the release workflow starts the binary and checks that the UI comes back.

**Artifacts are not signed.** A cosign keyless signature needs the workflow to
carry `id-token: write`, and a half-configured signing step produces a release
that fails at its last stage. The checksums file is published; verifying it
against the release page is the current story.

## Adding a catalog template

See [`template-schema.md`](template-schema.md). Put the JSON in
`internal/templates/catalog/`; the tests iterate every shipped template and
check that it validates and renders a legal container spec, so a broken one
fails the suite rather than the catalog screen.
