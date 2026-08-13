# Compose support in Iskele

Iskele parses Compose files with [compose-go](https://github.com/compose-spec/compose-go),
the same library the `docker compose` CLI uses, and then creates the containers
itself over the engine socket. It does not shell out to `docker compose`: a
panel that needs a CLI installed, at a matching version, is a panel that breaks
on somebody else's machine.

That means a file which loads in the CLI loads here. It does **not** mean every
field does the same thing — Iskele creates plain containers, so anything that
only exists in Swarm has nowhere to go.

Nothing is dropped silently. Every field in the "ignored" column below produces
a warning on `validate` and on deploy, naming the service and the field.

---

## Service fields

### Applied

| Field | Notes |
|---|---|
| `image` | Pulled according to `pull_policy`. |
| `build` | Built with Iskele's own builder. The context must be inside `allowed_paths`, exactly as a manual build is. Produces `<stack>-<service>:latest` when the service names no image. |
| `command`, `entrypoint` | Both list and string forms. |
| `container_name` | Pins the name. Cannot be combined with replicas. |
| `environment` | Entries with a value. See *Environment* below for the ones without. |
| `env_file` | Resolved at parse time into `environment`. |
| `ports` | Including host IP, protocol and ranges. A range produces a warning, because it publishes more than it looks like. |
| `expose` | Recorded by the engine; publishes nothing, which is what `expose` means. |
| `volumes` | Binds, named volumes and tmpfs, with `:ro` and propagation. Bind sources go through `allowed_paths`. |
| `tmpfs` | Shorthand form, with size. |
| `networks` | Aliases, `ipv4_address`, `ipv6_address`. Every service also gets its own name as an alias on each network. |
| `network_mode` | `host`, `none`, `bridge`, `container:<id>`, `service:<name>` pass through to the engine. |
| `depends_on` | Determines start order. Both the list and the conditional (`service_healthy`) forms parse; ordering is what Iskele acts on. |
| `restart` | `no`, `always`, `unless-stopped`, `on-failure[:n]`. |
| `healthcheck` | Test, interval, timeout, retries, start period, and `disable`. |
| `deploy.replicas` | Runs the service more than once, named `<stack>-<service>-<n>`. |
| `deploy.resources` | CPU, memory and pids limits and reservations. |
| `deploy.restart_policy.condition` | Used when the service sets no top-level `restart`. |
| `cpus`, `cpu_shares`, `cpuset`, `mem_limit`, `mem_reservation`, `memswap_limit`, `pids_limit`, `shm_size` | The pre-`deploy` limit fields. `deploy.resources` wins when both are set. |
| `user`, `working_dir`, `hostname`, `domainname` | |
| `tty`, `stdin_open`, `init` | |
| `labels` | Merged with Iskele's own stack labels. |
| `logging` / `log_driver` + `log_opt` | |
| `dns`, `dns_search`, `dns_opt`, `extra_hosts`, `mac_address` | |
| `read_only` | |
| `privileged`, `cap_add`, `cap_drop`, `security_opt`, `devices`, `sysctls` | Applied — but only for a caller with the `privileged` permission. A compose file is not a way around that gate; the deploy is refused with the option named. |
| `pull_policy` | `always`, `never`, `missing`/`if_not_present`. `build` warns and behaves as `missing`. |
| `profiles` | Parsed, but every service is deployed. A silently reduced deployment is worse than an explicit one. |
| `extends` | Resolved at parse time; the merged result is what deploys. |
| `scale` | Same as `deploy.replicas`. Setting both to different values is an error, as in the CLI. |

### Parsed and ignored (each produces a warning)

| Field | Why | What to do instead |
|---|---|---|
| `configs` | Swarm-only. | Bind mount the file, or pass it as an environment variable. |
| `secrets` | Swarm-only. | Same. Iskele's registry credentials are encrypted at rest; application secrets belong in the stack's `.env`. |
| `deploy.mode: global` | Swarm-only. | The service runs once. |
| `deploy.placement` | Swarm-only. | — |
| `deploy.update_config`, `deploy.rollback_config` | Swarm-only rolling updates. | Redeploy the stack. |
| `develop.watch` | A compose CLI feature — it needs a process watching your filesystem. | Rebuild and redeploy. |
| `links`, `external_links` | Legacy. | Services on the same network already reach each other by name. |
| `volumes_from` | Legacy. | Mount the same named volume in both services. |
| `credential_spec` | Windows containers only. | — |
| `models`, `provider` | Model runners and provider services. | — |
| `pre_start`, `post_start`, `pre_stop` | Compose CLI lifecycle hooks. | — |

---

## Project fields

| Field | Status |
|---|---|
| `services` | Applied. |
| `networks` | Created, namespaced `<stack>_<key>`, unless `external: true` or an explicit `name:`. Driver, `internal`, `attachable`, IPv6, IPAM subnets and driver options are applied. |
| `volumes` | Created, namespaced the same way. Driver, driver options and labels are applied. |
| `name` | Overridden by the stack's own name — that is what labels the containers. |
| `include` | Resolved by the loader, from paths inside `allowed_paths`. |
| `configs`, `secrets` | Parsed; warned about; not created. |
| `version` | Obsolete and ignored, as everywhere else. No warning: it is on nearly every file in the world, and a warning nobody can act on teaches people to ignore warnings. |

---

## Environment and interpolation

`${VARIABLE}` in a compose file resolves against **the stack's own `.env` and
nothing else.**

The CLI also reads your shell's environment. iskeled's environment is not your
shell — it holds the daemon's secret key path, its database path and whatever
the unit file sets. A compose file that could read `${...}` out of that would
turn "deploy this stack" into "print me the daemon's environment". So it cannot.

The consequences are worth stating plainly:

- `${VAR:-default}` works; the default is used.
- `${VAR:?message}` refuses the deploy when `VAR` is not in the stack's `.env`,
  which is what it is for.
- `${VAR}` with nothing set becomes the empty string **and produces a warning
  naming the variable.** A database whose password quietly became `""` is worth
  a line of text.
- In a service's `environment:`, a bare `- FROM_HOST` (a key with no value) asks
  compose to copy that variable from the caller's environment. Iskele drops it,
  for the same reason. Give it a value in the stack's `.env` and write
  `FROM_HOST: ${FROM_HOST}`.

---

## Paths

Every host path a compose file names — a bind mount source, a build context, an
`include` — is checked against `allowed_paths` before anything is read, with
symlinks resolved. It is the same check the create wizard's bind mounts go
through, because it is the same trust boundary.

Relative paths (`./data:/data`) resolve against the stack's working directory.
A stack written in the editor has no directory of its own outside Iskele's data
directory, so relative binds in one will normally be refused: name an absolute
path inside `allowed_paths`, or use a named volume.

---

## Naming

Iskele names things the way the CLI does, so a stack deployed here looks
familiar in `docker ps` and can be cleaned up without this panel:

| Thing | Name |
|---|---|
| Container | `<stack>-<service>-<n>` |
| Network | `<stack>_<key>` |
| Volume | `<stack>_<key>` |

Every container, network and volume carries both sets of labels:

| Label | Meaning |
|---|---|
| `com.iskele.stack` | The stack's name. |
| `com.iskele.service` | The service's name. |
| `com.iskele.managed` | Iskele created this and may remove it. |
| `com.docker.compose.project` | Same as `com.iskele.stack`, so the CLI recognizes it. |
| `com.docker.compose.service` | Same as `com.iskele.service`. |

The compose labels are why a stack started with `docker compose up` on the same
host shows up in Iskele's stack list without being imported.
