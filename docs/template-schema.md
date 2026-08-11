# Template schema

A template is one entry in Iskele's app catalog: a JSON file describing an
application, the questions to ask about it, and how the answers become a
container.

**A template is a form, not a script.** Rendering fills in a container
definition and hands it to the same service the create wizard uses, so every
template goes through the path whitelist and the privileged-option permission.
A catalog entry cannot do anything an operator could not do by hand.

Twenty templates ship inside the binary. Your own go in
`/etc/iskele/templates/*.json` — one file per template, and an id that matches
a shipped one replaces it, so pinning a version for a whole installation does
not mean forking the binary.

A custom file that will not load is reported in the catalog rather than
swallowed, and it does not take the rest of the catalog with it.

---

## A minimal template

```json
{
  "id": "hello",
  "title": "Hello",
  "category": "tools",
  "description": "Serves a page that says hello.",
  "image": "nginxdemos/hello:plain-text",
  "ports": [{ "host": "{{port}}", "container": 80 }],
  "fields": [
    { "name": "port", "label": "Host port", "type": "port", "default": "8080", "required": true }
  ],
  "notes": "Open http://localhost:{{port}}."
}
```

`{{port}}` is substituted with the answer to the field called `port`.
**A placeholder with no matching field is refused at load time** — it would
otherwise render as an empty string and produce a container that is subtly
wrong rather than obviously broken.

---

## Top level

| Key | Type | Required | Meaning |
|---|---|:--:|---|
| `id` | string | ✅ | Lowercase letters, digits, dash or underscore, up to 64 characters. It appears in a URL and in a label. |
| `title` | string | ✅ | What the catalog shows. |
| `category` | string | ✅ | Groups the catalog. The shipped set uses `database`, `networking` and `tools`. |
| `description` | string | | One sentence, in the operator's terms rather than the project's marketing. |
| `icon` | string | | A [lucide](https://lucide.dev) icon name. Names, not images, so the catalog works offline. |
| `website`, `documentation` | string | | Links to the project itself. |
| `keywords` | string[] | | Extra words the catalog search matches. |
| `image` | string | ✅ | The container image. Usually carries a `{{version}}` placeholder. |
| `command`, `entrypoint` | string[] | | Override the image's. An element that is entirely an unanswered placeholder is dropped rather than passed as an empty argument. |
| `env` | object | | Environment variables. Values may hold placeholders. |
| `ports` | array | | See *Ports*. |
| `mounts` | array | | See *Mounts*. |
| `labels` | object | | Extra labels. Iskele adds its own on top. |
| `restart` | string | | `no`, `always`, `unless-stopped` (the default) or `on-failure`. |
| `health_check` | object | | `test`, `interval`, `timeout`, `start_period`, `retries`. |
| `cap_add`, `devices`, `privileged`, `sysctls`, `network_mode` | | | See *Privileged options*. |
| `fields` | array | | The questions, in the order they are asked. |
| `notes` | string | | Shown after a successful deploy, with placeholders substituted. This is where the first-login instruction goes. |

`source` is set by the loader — `builtin` or `custom` — and ignored if a file
sets it.

An **unknown key is refused.** A typo in a key name would otherwise produce a
template that is quietly not what was written.

---

## Fields

```json
{
  "name": "password",
  "label": "Superuser password",
  "type": "password",
  "help": "Anything that reaches the port and knows this has full access.",
  "required": true,
  "generate": true,
  "generate_length": 32
}
```

| Key | Meaning |
|---|---|
| `name` | Lowercase letters, digits and underscore. This is what `{{name}}` substitutes. |
| `label` | The form label. Required. |
| `type` | One of the types below. |
| `help` | Shown under the input. This is where a template explains what a value is *for*, which is most of what makes a catalog usable. |
| `default` | Pre-fills the input. Refused on a password field. |
| `required` | An empty answer is refused. |
| `pattern` | A Go regular expression the answer must match. It must compile, or the template does not load. |
| `min`, `max` | Bound a number or a port. |
| `options` | The choices for a select: `[{"value": "...", "label": "..."}]`. |
| `generate` | The UI offers to generate a random value. |
| `generate_length` | How long a generated value should be. |

### Field types

| Type | Answer | Checked |
|---|---|---|
| `text` | A line of text | `pattern`, if set |
| `number` | An integer | `min`, `max` |
| `password` | A secret | **`default` is refused** — a shipped default password is a password everybody has |
| `select` | One of `options` | The answer must be one of them |
| `bool` | `"true"` or `"false"` | |
| `port` | A host port | 1–65535, plus `min`/`max` |
| `path` | A host path | Must be absolute. Checked against `allowed_paths` at deploy time, like any other bind source |
| `volume` | A named volume | Engine name rules. Empty means "name it after the container" |

Every bad answer is reported at once, not the first one: an operator filling in
a nine-field form should not have to submit it nine times.

---

## Ports

```json
"ports": [
  { "host": "{{port}}", "container": 5432, "protocol": "tcp" }
]
```

`host` is normally a placeholder — the port is the first thing an operator
changes. `protocol` defaults to `tcp`.

**A port whose host value resolves to nothing is not published.** That is how a
template offers a port without insisting on it: leave the field optional, and
an operator who does not answer it gets a container with that port closed.

---

## Mounts

```json
"mounts": [
  { "type": "volume", "source": "{{data_volume}}", "destination": "/var/lib/postgresql/data" },
  { "type": "bind", "source": "{{site_dir}}", "destination": "/usr/share/nginx/html", "read_only": true }
]
```

| Type | Source | Notes |
|---|---|---|
| `volume` | A volume name | Empty resolves to `<container>-data-<n>`, so a template can offer "where should this live" without making it mandatory |
| `bind` | An absolute host path | Required, and checked against `allowed_paths` at deploy time |

`tmpfs` is not allowed in a template: it would silently discard data the
operator believes they are keeping.

---

## Privileged options

A few real applications need more than a plain container. WireGuard needs
`NET_ADMIN` and two sysctls; Traefik and Watchtower need the Docker socket.
Templates may declare these:

| Key | Effect |
|---|---|
| `privileged` | `--privileged` |
| `cap_add` | Added capabilities |
| `devices` | Device mappings |
| `sysctls` | Kernel parameters inside the container's namespaces |
| `network_mode` | `host`, `none`, or a network name |

**Declaring them does not grant them.** The rendered definition goes through
the same permission gate as everything else, so deploying such a template
requires the `privileged` permission and is refused otherwise, naming the
option that stopped it. The catalog marks these entries in advance, so nobody
fills in a form they will not be allowed to submit.

Mounting the Docker socket deserves its own sentence: a container with
`/var/run/docker.sock` can start any other container, including a privileged
one, which makes it equivalent to root on the host. Iskele refuses the mount
unless that path is in `allowed_paths` — which it should not be unless you have
decided otherwise on purpose.

---

## What Iskele adds

Every container a template creates carries:

| Label | Meaning |
|---|---|
| `com.iskele.template` | The template's id |
| `com.iskele.managed` | Iskele created this |

The container's restart policy defaults to `unless-stopped`, which is what an
operator deploying a service from a catalog almost always means.

---

## Writing your own

1. Put the file in `/etc/iskele/templates/`, named whatever you like, with a
   `.json` extension.
2. Restart iskeled. The catalog is read at startup.
3. If the file does not load, the catalog page shows the path and the reason.

The error messages name the field: `redis: fields.password: a password field
must not carry a default` rather than "invalid template".
