---
layout: default
title: Docker management panel for a single Linux host
description: >-
  Iskele is a self-hosted Docker management panel that runs as a systemd
  service on one Linux host. A single static Go binary with an embedded web UI
  — no container, no runtime dependencies — managing containers, images,
  volumes, networks and Compose stacks.
---

{: .lead }
Iskele manages Docker on one Linux host from a web UI that lives inside the
binary. It runs as a systemd service and talks to the Docker Engine API over
`/var/run/docker.sock`. There is no container to pull, no database to run and
no runtime dependency to install.

<p class="warn" markdown="1">
**Access to the Docker socket is equivalent to root on the host.** Anyone who
reaches Iskele's API can start a privileged container that mounts the host
filesystem. Keep <code>listen</code> on <code>127.0.0.1</code> behind a TLS
proxy and read the [security model](security-model.md) before exposing it.
</p>

## Install

Download the archive for your architecture from the
[releases page](https://github.com/ibrahimhates/iskele/releases), then:

```sh
tar xzf iskele_0.1.1_linux_amd64.tar.gz
sudo ./deploy/install.sh
```

The installer creates the `iskele` system user, adds it to the `docker` group,
lays out `/etc/iskele` and `/var/lib/iskele`, installs the systemd unit and
starts it. Running it again upgrades the binary and leaves the config, the
database and the secret key alone.

There are `.deb` and `.rpm` packages too. They install everything but do not
start the service — a package manager starting a root-equivalent panel because
somebody typed `apt install` is making a decision that is not its to make:

```sh
sudo dpkg -i iskele_0.1.1_linux_amd64.deb
sudo systemctl enable --now iskeled
```

Then open `http://127.0.0.1:8377` and create the first admin account. Until you
do, every route answers `409 NOT_INITIALIZED`: an installation that is running
but not yet configured does not expose Docker to whoever reaches the port
first.

Builds are published for `linux/amd64`, `linux/arm64` and `linux/armv7`, so a
Raspberry Pi 2 or newer is a target rather than an afterthought.

## What it does

- **Containers** — list with search, sort and multi-select; start, stop,
  restart, pause, kill, rename and remove, in bulk, with per-container results
  rather than one verdict for the batch. Redeploy pulls the image and recreates
  the container from its own definition, rolling back if the new one fails.
- **Live streams** — log streaming and an interactive `exec` console over
  WebSockets, plus CPU, memory and IO for every running container over one
  shared event stream.
- **Create wizard** — every `docker run` option across ten tabs, with a live
  preview of both the command and the API payload it becomes.
- **Compose stacks** — parsed with the same library the `docker compose` CLI
  uses and deployed over the engine socket, so no `docker compose` binary has
  to be installed. Projects started with the CLI on the same host are
  discovered and can be adopted.
- **Builds** — build an image from a Dockerfile on the host, with output
  streaming live and the build surviving the tab that started it.
- **Images, volumes, networks** — full CRUD and prune, with private registry
  credentials encrypted at rest.
- **App catalog** — twenty one-click templates, and your own alongside them.
- **Users and audit** — admin, operator and viewer roles, API tokens, optional
  TOTP two-factor, and every mutation recorded and exportable as CSV or JSON.

The UI ships in English and Turkish, light and dark.

## Documentation

| | |
|---|---|
| [Configuration](configuration.md) | Every setting, flag and environment variable |
| [Security model](security-model.md) | The trust boundaries, in detail |
| [Architecture](architecture.md) | How the pieces fit, and why |
| [Compose support](compose-support.md) | Which compose fields are supported |
| [Template schema](template-schema.md) | Writing a catalog template |
| [Development](development.md) | Building from source, and cutting a release |
| [Security policy](security.md) | The threat model and how to report a vulnerability |
| [Changelog](changelog.md) | What shipped, and the known limitations |

The API is specified in full in [`openapi.yaml`]({{ '/openapi.yaml' | relative_url }}).

## Why not a container

Running the panel that manages Docker *inside* Docker means the thing you reach
for when the engine is unhealthy is the thing that just went down with it.
Iskele stays up and reports the outage instead: `/health` keeps answering and
every engine-backed route returns `503 DOCKER_UNAVAILABLE` with the endpoint it
tried and the most likely fix.
