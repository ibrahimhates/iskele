# Contributing

Thanks for wanting to help. This is a small project; the rules below exist to
keep it reviewable, not to make anyone jump through hoops.

## Before a large change

Open an issue first if you are planning something big. A pull request that
rewrites a subsystem is a lot of your time to spend before finding out the
maintainers wanted it done differently. Small fixes need no ceremony — send
them.

## Development setup

Requirements: **Go 1.25+** and **Node 20+**.

```sh
git clone https://github.com/ibrahimhates/iskele
cd iskele
make build      # frontend + binary -> bin/iskeled
make run        # 127.0.0.1:8377, data in ./.data, debug logging
```

Working on the frontend with live reload takes two terminals:

```sh
make run        # the daemon
make web-dev    # Vite on :5173, proxying /api to the daemon
```

You do not need Docker installed to work on most of this. Without it the
daemon starts, the UI loads, and every engine-backed route answers
`503 DOCKER_UNAVAILABLE` — which is itself a path worth testing. The test
suite uses a fake engine and never touches a real one.

`make build-go` skips the frontend entirely, for backend work on a machine
without Node.

## Before you push

```sh
make check      # gofmt + vet + go test -race
make lint       # golangci-lint
make web-test   # frontend tests
make web-lint   # eslint + prettier
```

Or `make help` for everything.

Two of these catch mistakes that are easy to make and annoying to find:

- **`make gen-api`** regenerates the TypeScript wire types from
  `docs/openapi.yaml`. If you changed a response shape, run it and commit the
  result. CI fails when the committed types do not match the spec.
- **`web/src/api/conformance.ts`** compares the hand-written types in
  `types.ts` against the generated ones at compile time. If you add a field to
  one, add it to the other, or the frontend build fails.

## What the code looks like

Read a neighbouring file first; matching what is there beats matching this
list.

**Architecture.** Handlers do HTTP, services do decisions, `internal/docker`
does the engine. A handler never imports the Docker SDK, and a service never
writes a status code. `internal/docker` is the only package that imports the
Docker SDK, and `internal/hostinfo` is the only one that imports gopsutil —
for the same reason.

**Comments explain why, not what.** `// increment i` is noise. A comment
saying why a build's send context is deliberately not derived from its task
context is the difference between a maintainer keeping the behaviour and
"simplifying" it back into a bug.

**Errors say what to do.** `"cannot reach the Docker daemon at
unix:///var/run/docker.sock; check that Docker is running and that iskeled's
user is in the docker group"` beats `"connection refused"`. The person reading
it is trying to fix something.

**Tests describe behaviour.** `TestRenderSkipsAnUnansweredOptionalBind` says
what is guaranteed; `TestRender3` does not. A test that only compares our
output to itself proves nothing — where a standard exists, test against it
(the TOTP tests run RFC 6238's own vectors).

**No placeholders.** No `// TODO: implement`, no endpoint returning mock data,
no button that does nothing. If it is in the UI it works, or it is not in the
UI yet.

## Commits

[Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`,
`chore:`, `docs:`, `test:`, `refactor:`.

Write the body for someone reading `git log` in a year, wondering why. What
changed is in the diff; why it changed is only here.

## Decisions

Anything a future maintainer might reasonably want to undo goes in
[`DECISIONS.md`](DECISIONS.md) — context, decision, rationale, consequence. It
is in Turkish, matching the rest of the project's internal documents; the code
and its comments are in English.

If you find yourself writing "we should probably…" in a PR description, that is
a decision, and it belongs there.

## Pull requests

- One subject per PR.
- CI must be green: Go tests on two versions, golangci-lint, govulncheck, the
  frontend suite, a single-binary bundle check and three cross-compiles.
- Say what you tested by hand, especially anything needing a real Docker
  daemon. The suite cannot cover that, and saying "not tested against a live
  engine" is far more useful than silence.

## Security

Do not open a public issue for a vulnerability. See
[`SECURITY.md`](SECURITY.md).

## License

Contributions are licensed under [Apache-2.0](LICENSE), like the rest of the
project.
