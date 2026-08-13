# Architecture

One process, one binary, one host. `iskeled` runs on the machine as a systemd
service, talks to the Docker Engine API over a unix socket, keeps its own state
in SQLite, and serves a React app it carries inside itself.

```
                    browser
                       │
                       │  HTTP + WebSocket + SSE
                       ▼
  ┌──────────────────────────────────────────────┐
  │ iskeled                                      │
  │                                              │
  │  server/          handlers, middleware, SPA  │
  │      │                                       │
  │  service/         decisions, audit, guards   │
  │      │                                       │
  │  ┌───┴────┬──────────┬──────────┐            │
  │  docker/  store/   compose/   templates/     │
  │  │        │                                  │
  └──┼────────┼──────────────────────────────────┘
     │        │
     ▼        ▼
  /var/run/  /var/lib/iskele/iskele.db
  docker.sock
```

## The layers, and what each may not do

**`server/`** speaks HTTP. Handlers decode a request, call one service method,
and turn the result into a status code and a body. A handler never imports the
Docker SDK and never makes a decision that could be tested without a request.

**`service/`** decides. This is where the path whitelist is enforced, the
privileged gate closes, the audit record is written and errors become sentinel
values a handler can map. A service never writes a status code — it returns an
error with enough shape that the handler can.

**`docker/`** is the only package that imports the Docker SDK. Everything above
it works against a `Client` interface, which is what makes the whole suite
runnable with no Docker daemon: `docker/fake` implements the same interface and
the tests never touch a real engine. `docker/offline` implements it too,
returning `DOCKER_UNAVAILABLE` for every call — which is why the panel still
starts and serves when Docker is down.

**`hostinfo/`** is the same idea for gopsutil: one package reads the
platform-specific files, everything else sees a plain struct.

**`store/`** is SQLite behind a repository per table. Migrations are embedded
SQL, applied in order at startup.

**`compose/`** parses compose files with the library the `docker compose` CLI
itself uses, then converts services into ordinary container specs. There is no
`docker compose` binary anywhere in the path.

**`templates/`** loads the catalog — twenty entries embedded in the binary,
plus anything in `/etc/iskele/templates` — and renders a template's answers
into a container spec.

## Why the layering is worth the indirection

Two properties fall out of it, and both are load-bearing:

**The suite runs anywhere.** No Docker, no network, no fixtures to seed. That
is not a testing nicety — it means the security boundaries (the path guard, the
privileged gate, the RBAC matrix) are covered by tests that run on every push,
rather than by a manual checklist somebody skips.

**Docker being down is an ordinary case.** `docker/offline` satisfies the same
interface, so the failure travels the normal path and arrives as a 503 with an
explanation. Nothing special-cases it, and nothing panics.

## Where things live

```
cmd/iskeled/        entrypoint: config, wiring, graceful shutdown, housekeeping
internal/
  config/           flag > env > YAML > default, then validate
  server/
    router.go       every route and the permission it needs
    middleware/     auth, RBAC, rate limit, CSRF, logging, recover
    handlers/       one file per resource
    spa.go          serves the embedded frontend; deep links get the shell
  service/          the business layer
  docker/           SDK wrapper, fake, offline
  compose/          parse, convert, diff, git, discovery
  templates/        catalog schema, renderer, the twenty entries
  store/            SQLite, migrations, one repository per table
  auth/             argon2id, JWT, API tokens, TOTP, brute-force limiter
  crypto/           master key, AES-GCM secret box
  audit/            the trail, with secret masking
  hostinfo/         gopsutil, fenced off
  systemd/          sd_notify
  httpx/            the shared HTTP vocabulary: errors, codes, JSON writing
  version/          build metadata
web/                React + TypeScript, embedded via go:embed
```

## Request path

A request for `POST /api/v1/containers/{id}/stop`:

1. **RequestID → Logger → Recover → SecurityHeaders.** In that order: the
   logger needs the id, and a panic must still be logged with its request.
2. **Rate limit**, then **initialization check** — before setup, every route
   but the auth endpoints answers `409 NOT_INITIALIZED`.
3. **Authenticate**: a JWT or an API token becomes an `Identity`, which carries
   the role. A disabled account fails here.
4. **CSRF guard** on state-changing requests.
5. **RequirePermission(operate)** — routes require permissions, not roles, and
   an unrecognised role has none.
6. **Handler** decodes and calls `service.Container.Stop`.
7. **Service** normalizes the id, calls the engine, and writes an audit record
   whether the call succeeded or failed.
8. **`docker/`** translates the SDK error into a classified `docker.Error`.
9. **Handler** maps that class onto a status code and the standard envelope.

## Streaming

Logs, the exec console and build output are WebSockets. Live stats, engine
events, image pulls and stack deploys are Server-Sent Events. Both face the
same problem: a browser cannot set an `Authorization` header on either. So
they take a single-use 60-second ticket from `POST /auth/ws-ticket` in the
query string — consumed on arrival, whether or not the permission check that
follows passes, so a rejected ticket cannot be retried elsewhere.

Two rules learned the hard way, both pinned by tests:

- **Work outlives its socket.** Closing the tab during a build stops the
  frames, not the build. The send context is deliberately not derived from the
  task context: cancelling a task ends the work, and the client still has to be
  told why.
- **The engine reports failures inside a 200.** A pull or a build streams
  errors in the body, so the reader drains *both* the event channel and the
  error channel before declaring success.

## State

SQLite in `data_dir`, WAL mode, one repository per table: users, sessions, API
tokens, login attempts, audit, registries, builds, stacks, settings.

Two things live outside it. Build logs are files in `data_dir/builds` — a
megabyte of output per row is not what a database is for. Stack working copies
(git clones, written compose files) are directories in `data_dir/stacks`.

The master key is a separate file, `secret_key_file`, mode 0600, which iskeled
refuses to start on if anyone else can read it. Registry passwords and TOTP
secrets are encrypted under it, so the database alone gives up neither.

## Frontend

Vite + React + TypeScript, built into `web/dist` and embedded with `go:embed`.
The binary serves it at `/`, on the same port as the API; unknown paths under
`/api` stay JSON, and everything else returns the shell so client-side routes
survive a reload.

The wire types are kept honest in two steps: `make gen-api` generates
TypeScript from `docs/openapi.yaml`, and `web/src/api/conformance.ts` compares
the hand-written types against the generated ones at compile time. A response
shape that changes in Go without changing the spec fails the frontend build.

## Shutdown

SIGTERM cancels the root context. The server stops accepting, drains in-flight
requests within its deadline, and closes what is left. Under systemd,
`STOPPING=1` goes out first, so a slow drain is not mistaken for the hang the
watchdog exists to catch.

Work bound to the process is reconciled at the *next* startup rather than
rescued at this one: a build or a deploy that was running when the process died
can never finish, so its row is closed with an explanation instead of being
left at "running" forever.
