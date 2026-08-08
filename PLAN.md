# Iskele — Uygulama Planı (PLAN.md)

> Kaynak gereksinim dokümanı: [`PROMPT.md`](./PROMPT.md)
> Durum takibi: [`PROGRESS.md`](./PROGRESS.md) · Kararlar: [`DECISIONS.md`](./DECISIONS.md) · Kabul: [`ACCEPTANCE.md`](./ACCEPTANCE.md)
>
> Bu doküman uygulama başlamadan önce üretilmiştir ve M0–M9 boyunca **yaşayan doküman**dır.
> API yüzeyi veya veri modeli değişirse önce burası güncellenir, sonra kod yazılır.

---

## 1. Amaç ve Kapsam

**Iskele**, Linux host üzerinde `iskeled` adlı **native systemd servisi** olarak çalışan, tek statik binary
halinde dağıtılan bir Docker yönetim panelidir. Container / image / volume / network ve Compose stack
yönetimini web arayüzünden yapar.

### Kapsam içi (v0.1.0)
- Tek Docker endpoint (local unix socket) üzerinde tam kaynak yönetimi
- Canlı log, exec konsolu, canlı stats, canlı build log
- Dockerfile'dan build, Compose stack yönetimi, App Catalog (20 template)
- Kullanıcı/rol yönetimi, API token, audit log, TOTP 2FA
- systemd paketleme, install/uninstall script, 3 mimariye release

### Kapsam dışı (v0.1.0 sonrası)
- Çoklu/uzak Docker endpoint (TCP+TLS) — altyapı M9'da hazırlanır, UI tam akışı v0.2
- Swarm / Kubernetes desteği
- Container içi dosya yöneticisi (Files sekmesi) — M9'da "opsiyonel", zaman kalırsa
- Cluster / HA, çoklu sunucu ajanı

### Tasarım ilkeleri
1. **Tek binary, sıfır runtime bağımlılığı.** cgo yok, harici `docker` CLI çağrısı yok.
2. **Docker'a tek erişim noktası.** Tüm SDK çağrıları `internal/docker` arkasında, interface ile.
3. **Güvenlik varsayılanı kapalı.** `127.0.0.1` bind, path whitelist, privileged yalnız admin.
4. **Placeholder yok.** Her ekran, her buton, her endpoint gerçek çalışır (bkz. PROMPT §0.8).
5. **Her milestone yeşil biter.** `go build` + `go test` + `npm run build` + lint.

---

## 2. Teknoloji Yığını (SABİT)

| Katman | Seçim | Not |
|---|---|---|
| Dil (backend) | Go 1.25+ | `CGO_ENABLED=0`; Docker SDK bağımlılık ağacı 1.25 gerektiriyor (D-019) |
| Modül yolu | `github.com/ibrahimhates/iskele` | bkz. D-001 |
| Docker | `github.com/docker/docker v28.5.2` | resmi SDK, API negotiation açık (D-020) |
| Router | `github.com/go-chi/chi/v5` | + chi middleware |
| DB | `modernc.org/sqlite` | saf Go, cgo yok |
| Migration | Elle yazılmış, `embed.FS` ile gömülü sıralı SQL | bkz. D-004 |
| Log | `log/slog` (stdlib) + JSON/text handler | bkz. D-003 |
| Auth | `github.com/golang-jwt/jwt/v5` + `golang.org/x/crypto/argon2` | argon2id |
| TOTP | `github.com/pquerna/otp` | M8 |
| WebSocket | `github.com/coder/websocket` | bkz. D-005 |
| Compose | `github.com/compose-spec/compose-go/v2` | parse; uygulama Docker API ile |
| Host metrik | `github.com/shirou/gopsutil/v3` | CPU/RAM/disk |
| Config | `gopkg.in/yaml.v3` + flag + env | öncelik: flag > env > yaml > default |
| Frontend | React 18 + TypeScript + Vite 5 | |
| UI | Tailwind CSS + shadcn/ui (Radix) | dark mode varsayılan |
| Data | TanStack Query v5 + Zustand | |
| Form | react-hook-form + zod | |
| Terminal | xterm.js + addon-fit + addon-web-links | |
| Editör | monaco-editor (@monaco-editor/react) | Compose YAML |
| Grafik | Recharts | stats |
| i18n | react-i18next | TR/EN |
| Test (be) | stdlib `testing`, tablo testleri | testify eklenmedi (D-015) |
| Test (fe) | Vitest + @testing-library/react | |
| Lint | golangci-lint (errcheck, govet, staticcheck, gosec) + eslint + prettier | |
| Build | Makefile + GoReleaser | |
| CI/CD | GitHub Actions | |

**Cross-compile hedefleri:** `linux/amd64`, `linux/arm64`, `linux/arm/v7`.

---

## 3. Dosya Ağacı (hedef)

```
iskele/
├── cmd/
│   └── iskeled/
│       └── main.go                     # flag parse, config, DI wiring, graceful shutdown
├── internal/
│   ├── config/
│   │   ├── config.go                   # Config struct, Load(), defaults
│   │   ├── config_test.go              # flag>env>yaml önceliği testi
│   │   └── validate.go                 # listen/path/tls doğrulama
│   ├── version/
│   │   └── version.go                  # ldflags: Version, Commit, BuildDate
│   ├── logging/
│   │   └── logging.go                  # slog kurulumu, request logger
│   ├── httpx/                          # ortak HTTP sözlüğü (D-014)
│   │   ├── errors.go                   # APIError, kod sabitleri
│   │   └── response.go                 # JSON yaz, hata gövdesi standardı, Handler tipi
│   ├── server/
│   │   ├── server.go                   # http.Server, TLS, shutdown
│   │   ├── router.go                   # tüm route tanımları
│   │   ├── spa.go                      # embed.FS + SPA fallback
│   │   ├── middleware/
│   │   │   ├── auth.go                 # JWT / API token doğrulama
│   │   │   ├── rbac.go                 # rol matrisi
│   │   │   ├── ratelimit.go            # token bucket, IP bazlı
│   │   │   ├── csrf.go                 # double submit
│   │   │   ├── logger.go
│   │   │   ├── recover.go
│   │   │   ├── requestid.go
│   │   │   └── securityheaders.go
│   │   └── handlers/
│   │       ├── auth.go        images.go       volumes.go
│   │       ├── containers.go  networks.go     stacks.go
│   │       ├── exec.go        logs.go         stats.go
│   │       ├── build.go       templates.go    system.go
│   │       ├── audit.go       users.go        settings.go
│   │       ├── fs.go          registries.go   health.go
│   │       └── *_test.go               # fake docker/service ile
│   ├── docker/
│   │   ├── client.go                   # Client interface + gerçek implementasyon
│   │   ├── container.go  image.go  network.go  volume.go
│   │   ├── build.go                    # build + log stream
│   │   ├── exec.go                     # exec attach, resize
│   │   ├── stats.go                    # stats stream + ring buffer
│   │   ├── events.go                   # docker events -> event bus
│   │   ├── system.go                   # info, df, prune
│   │   ├── types.go                    # UI'ya dönen sadeleştirilmiş DTO'lar
│   │   └── fake/fake.go                # testler için fake implementasyon
│   ├── service/
│   │   ├── container.go  image.go  volume.go  network.go
│   │   ├── build.go  stack.go  template.go  system.go
│   │   └── *_test.go
│   ├── compose/
│   │   ├── parse.go                    # compose-go ile parse + normalize
│   │   ├── convert.go                  # service -> container/network/volume spec
│   │   ├── up.go  down.go  logs.go
│   │   ├── diff.go                     # kaydetmeden önce diff
│   │   ├── env.go                      # .env interpolasyon
│   │   ├── git.go                      # clone/pull
│   │   └── testdata/*.yaml             # ≥5 gerçek compose fixture
│   ├── store/
│   │   ├── db.go                       # açılış, pragma, migrate
│   │   ├── models.go
│   │   ├── migrations/                 # 0001_init.sql ...
│   │   ├── users.go  sessions.go  tokens.go  audit.go
│   │   ├── stacks.go builds.go  settings.go  registries.go
│   │   └── *_test.go                   # :memory: DB ile
│   ├── auth/
│   │   ├── password.go                 # argon2id hash/verify
│   │   ├── jwt.go                      # issue/parse/validate
│   │   ├── session.go                  # refresh rotate/revoke
│   │   ├── apitoken.go                 # üretim, prefix, scope
│   │   ├── totp.go                     # M8
│   │   ├── bruteforce.go               # IP bazlı sayaç
│   │   └── *_test.go
│   ├── crypto/
│   │   ├── secretbox.go                # AES-GCM encrypt/decrypt
│   │   └── keyfile.go                  # /etc/iskele/secret.key (0600) üret/oku
│   ├── paths/
│   │   ├── whitelist.go                # allowed_paths doğrulama, symlink çözümü
│   │   ├── browse.go                   # dizin listeleme
│   │   └── whitelist_test.go           # traversal saldırı vektörleri
│   ├── templates/
│   │   ├── engine.go                   # şema doğrulama + render
│   │   ├── schema.go
│   │   ├── catalog/*.json              # 20 gömülü template
│   │   └── engine_test.go
│   ├── events/
│   │   ├── bus.go                      # in-memory pub/sub
│   │   └── sse.go                      # SSE yayını
│   ├── audit/
│   │   ├── audit.go                    # kayıt yazma, secret maskeleme
│   │   └── mask.go
│   └── tasks/
│       └── tasks.go                    # uzun süren iş kaydı (pull/build/up) + iptal
├── web/
│   ├── src/
│   │   ├── main.tsx  App.tsx  router.tsx
│   │   ├── api/                        # generated types + fetch client + ws/sse
│   │   ├── components/ui/              # shadcn
│   │   ├── components/                 # DataTable, LogViewer, Terminal, TaskDrawer...
│   │   ├── features/                   # containers/ stacks/ images/ ... (sayfa+hook)
│   │   ├── stores/                     # zustand: auth, ui, tasks
│   │   ├── locales/{tr,en}.json
│   │   └── lib/
│   ├── index.html  vite.config.ts  tailwind.config.ts  tsconfig.json
│   └── dist/                           # embed edilir (gitignore)
├── deploy/
│   ├── iskeled.service                 # systemd unit (hardened)
│   ├── config.example.yaml
│   ├── install.sh  uninstall.sh
│   └── reverse-proxy/{nginx,caddy,traefik}.example
├── docs/
│   ├── architecture.md  openapi.yaml  template-schema.md
│   ├── configuration.md  security-model.md  development.md
│   └── screenshots/
├── .github/workflows/{ci.yml,release.yml,codeql.yml}
├── Makefile  .goreleaser.yaml  .golangci.yml  .gitignore  .editorconfig
├── PLAN.md  PROGRESS.md  DECISIONS.md  ACCEPTANCE.md  PROMPT.md
├── README.md  CONTRIBUTING.md  SECURITY.md  CHANGELOG.md  LICENSE
└── go.mod  go.sum
```

---

## 4. Katmanlı Mimari

```
HTTP / WS / SSE
      │
   handlers  ── sadece: parse, validate, authz kontrolü, response şekillendirme
      │
   service   ── iş kuralları, audit yazımı, event yayını, task kaydı
      ├──────► docker (Client interface)  ──► Docker Engine API
      ├──────► store  (repository)        ──► SQLite
      ├──────► compose / templates / paths
      └──────► crypto / events / tasks
```

**Kurallar**
- Handler içinde `docker/client` SDK tipi geçmez; yalnız `internal/docker` DTO'ları.
- `internal/docker` DB bilmez; `internal/store` Docker bilmez.
- Tüm dış çağrılar `context.Context` alır ve iptal edilebilir.
- Servis metodları `actor` (kullanıcı) bilgisini parametre olarak alır → audit tek noktadan yazılır.
- Paket döngüsü yok; `internal/service` en üst kompozisyon katmanı, `cmd/iskeled` DI wiring yapar.

### Eşzamanlılık modeli
- Docker events dinleyicisi tek goroutine → `events.Bus` → SSE aboneleri (fan-out, buffered channel, yavaş abone düşürülür).
- Stats: container başına tek stats stream, `sync.Map` ile paylaşılır; abone sayısı 0 olunca kapanır. Son 60 örnek in-memory ring buffer.
- Uzun işler (`pull`, `build`, `stack up`) `tasks.Manager` altında; her task'ın `context.CancelFunc`'ı saklanır → UI'dan iptal.
- Graceful shutdown: `SIGINT/SIGTERM` → root context cancel → HTTP `Shutdown(30s)` → aktif WS'lere close frame → DB close.

---

## 5. Veri Modeli (SQLite)

`PRAGMA journal_mode=WAL; foreign_keys=ON; busy_timeout=5000;`

| Tablo | Alanlar (özet) |
|---|---|
| `schema_migrations` | `version INTEGER PK`, `applied_at` |
| `users` | `id`, `username UNIQUE`, `password_hash`, `role`(admin/operator/viewer), `totp_secret_enc`, `totp_enabled`, `disabled`, `created_at`, `updated_at`, `last_login_at` |
| `sessions` | `id`, `user_id FK`, `refresh_hash UNIQUE`, `ip`, `user_agent`, `expires_at`, `revoked_at`, `created_at` |
| `api_tokens` | `id`, `user_id FK`, `name`, `prefix`, `token_hash UNIQUE`, `scopes`(csv), `expires_at`, `last_used_at`, `revoked_at`, `created_at` |
| `login_attempts` | `id`, `ip`, `username`, `success`, `created_at` (brute-force penceresi) |
| `audit_logs` | `id`, `user_id`, `username`, `action`, `resource_type`, `resource_id`, `result`(ok/error), `detail`(JSON, maskeli), `ip`, `user_agent`, `created_at` |
| `settings` | `key PK`, `value`(JSON), `updated_at` |
| `registries` | `id`, `name`, `url`, `username`, `password_enc`(AES-GCM), `created_at`, `updated_at` |
| `stacks` | `id`, `name UNIQUE`, `source`(file/editor/git), `path`, `git_url`, `git_ref`, `compose_content`, `env_content`, `status`, `last_error`, `last_deployed_at`, `created_by`, `created_at`, `updated_at` |
| `builds` | `id`, `user_id`, `context_path`, `dockerfile`, `tags`, `build_args`(JSON), `target`, `no_cache`, `platform`, `status`(running/success/failed/canceled), `duration_ms`, `log_path`, `created_at`, `finished_at` |
| `events` | `id`, `type`, `action`, `resource_type`, `resource_id`, `payload`(JSON), `created_at` (retention ile budanır) |

**İndeksler:** `audit_logs(created_at DESC)`, `audit_logs(user_id)`, `sessions(user_id)`,
`login_attempts(ip, created_at)`, `builds(created_at DESC)`, `events(created_at DESC)`.

**Retention:** `settings.audit_retention_days` (varsayılan 90), `settings.event_retention_days` (7).
Günlük tek goroutine ile budama.

**Bootstrap durumu:** `users` tablosu boşsa sistem *uninitialized* kabul edilir; `/auth/bootstrap`
dışındaki tüm `/api/v1` yolları `409 NOT_INITIALIZED` döner.

---

## 6. API Yüzeyi

Prefix `/api/v1`. Tüm hatalar:

```json
{ "error": { "code": "CONTAINER_NOT_FOUND", "message": "no such container: abc", "details": {} } }
```

### Hata kodları (başlangıç seti)
`BAD_REQUEST`, `VALIDATION_FAILED`, `UNAUTHORIZED`, `INVALID_CREDENTIALS`, `TOKEN_EXPIRED`,
`FORBIDDEN`, `NOT_INITIALIZED`, `ALREADY_INITIALIZED`, `RATE_LIMITED`, `CSRF_INVALID`,
`NOT_FOUND`, `CONTAINER_NOT_FOUND`, `IMAGE_NOT_FOUND`, `VOLUME_NOT_FOUND`, `NETWORK_NOT_FOUND`,
`STACK_NOT_FOUND`, `CONFLICT`, `PATH_NOT_ALLOWED`, `DOCKER_UNAVAILABLE`, `DOCKER_ERROR`,
`COMPOSE_PARSE_ERROR`, `BUILD_FAILED`, `TEMPLATE_INVALID`, `INTERNAL`.

### Endpoint tablosu

| Method | Yol | Rol | Milestone |
|---|---|---|---|
| GET | `/health`, `/version` | — (auth'suz) | M0 |
| POST | `/auth/bootstrap` | — (yalnız uninitialized) | M2 |
| POST | `/auth/login` `/auth/refresh` `/auth/logout` | — / self | M2 |
| GET | `/auth/me` | any | M2 |
| POST | `/auth/totp/setup` `/auth/totp/verify` `/auth/totp/disable` | self | M8 |
| GET | `/containers?all=&filter=` | viewer | M1 |
| POST | `/containers` | operator (bind ve privileged için ek kontrol) | M5 |
| GET | `/containers/{id}` `/containers/{id}/inspect` | viewer | M1 |
| DELETE | `/containers/{id}?force=&volumes=` | operator | M1 |
| POST | `/containers/{id}/{start\|stop\|restart\|pause\|unpause\|kill\|rename}` | operator | M1/M4 |
| POST | `/containers/{id}/redeploy` | operator | M4 |
| POST | `/containers/batch` `{ids:[],action}` | operator | M4 |
| POST | `/auth/ws-ticket` | any | M4 |
| GET | `/containers/{id}/stats` (SSE) | viewer (ticket) | M4 |
| GET | `/containers/stats` (SSE, hepsi tek bağlantıda) | viewer (ticket) | M4 |
| GET | `/system/events` (SSE) | viewer (ticket) | M4 |
| WS | `/containers/{id}/logs` | viewer (ticket) | M4 |
| WS | `/containers/{id}/exec` | operator (ticket) | M4 |
| GET | `/images` | viewer | M1 |
| GET | `/images/pull` (SSE, ticket) | operator | M5 |
| DELETE | `/images/{id}?force=&noprune=` | delete | M5 |
| POST | `/images/prune` | prune (admin) | M5 |
| POST | `/images/{id}/tag` | operator | M5 |
| GET | `/system/allowed-paths` | viewer | M5 |
| GET/POST/PUT/DELETE | `/registries` `/registries/{id}` | admin | M5 |
| GET | `/tasks` `/tasks/{id}` · POST `/tasks/{id}/cancel` | viewer / operator | M5 |
| GET | `/images/{id}/history` `/images/{id}/inspect` | viewer | M5 |
| WS | `/build` | admin | M6 |
| GET | `/builds` `/builds/{id}` `/builds/{id}/log` | viewer | M6 |
| POST | `/builds/{id}/cancel` | admin | M6 |
| GET/POST | `/volumes` · DELETE `/volumes/{name}` · POST `/volumes/prune` | viewer/operator/admin | M1/M5 |
| GET/POST | `/networks` · DELETE `/networks/{id}` · POST `/networks/prune` | viewer/operator/admin | M1/M5 |
| POST | `/networks/{id}/{connect\|disconnect}` | operator | M5 |
| GET/POST | `/stacks` · GET/PUT/DELETE `/stacks/{id}` | viewer/admin | M7 |
| POST | `/stacks/{id}/{up\|down\|restart\|pull\|stop\|start}` | operator | M7 |
| POST | `/stacks/{id}/diff` `/stacks/{id}/validate` `/stacks/{id}/scale` | operator | M7 |
| WS | `/stacks/{id}/logs` | viewer | M7 |
| GET | `/templates` `/templates/{id}` | viewer | M8 |
| POST | `/templates/{id}/deploy` | operator | M8 |
| GET | `/system/info` `/system/df` · SSE `/system/events` | viewer | M8 |
| POST | `/system/prune` | admin | M8 |
| GET | `/audit?actor=&action=&from=&to=&format=` | admin | M8 |
| GET | `/fs/browse?path=` | operator | M6 |
| GET/POST/PUT/DELETE | `/users`, `/users/{id}` | admin | M8 |
| GET/PUT | `/settings` | admin | M8 |
| GET/POST/DELETE | `/registries` | admin | M5 |
| GET | `/tasks` · POST `/tasks/{id}/cancel` | operator | M5 |

`operator*`: container create operator'a açık, ancak `privileged`, `cap_add`, `devices`,
`security-opt` ve host-path bind mount alanları **yalnız admin**.

`docs/openapi.yaml` her milestone sonunda endpoint'lerle senkron tutulur (ACCEPTANCE'ta kontrol maddesi var).

### Gerçek zamanlı kanal protokolleri

**WS `/containers/{id}/logs`** — query: `tail`, `follow`, `timestamps`, `stdout`, `stderr`
```jsonc
// server -> client
{"t":"log","s":"stdout","ts":"2026-01-01T10:00:00Z","m":"listening on :80"}
{"t":"err","code":"CONTAINER_NOT_FOUND","m":"..."}
{"t":"eof"}
```

**WS `/containers/{id}/exec`** — query: `cmd`, `tty`
- İstemci → sunucu: binary frame = stdin; text frame = kontrol `{"t":"resize","cols":120,"rows":40}`
- Sunucu → istemci: binary frame = stdout/stderr; `{"t":"exit","code":0}`

**WS `/build`** — ilk mesaj build isteği JSON'u; sonrası:
```jsonc
{"t":"stream","m":"Step 3/9 : RUN go build"}
{"t":"progress","id":"sha256:..","status":"Downloading","current":123,"total":456}
{"t":"error","m":"..."} | {"t":"done","image":"sha256:..","duration_ms":10321}
```

**SSE `/containers/{id}/stats`** — `event: stats`, veri: `{cpu_pct, mem_used, mem_limit, mem_pct, net_rx, net_tx, blk_r, blk_w, pids, ts}`
**SSE `/system/events`** — `event: docker` (container/image/volume/network aksiyonları), `event: ping` (25 sn heartbeat)

**Yetkilendirme:** WS/SSE'de `Authorization` header taşınamadığı durumlar için tek kullanımlık,
60 sn ömürlü `ticket` query parametresi kullanılır (bkz. D-008). `Origin` doğrulaması zorunlu.

---

## 7. Güvenlik Mimarisi

| Konu | Uygulama |
|---|---|
| Parola | argon2id — `t=3, m=64MiB, p=2, salt=16B, key=32B`; min 12 karakter, zxcvbn benzeri basit güç kontrolü |
| Access token | JWT HS256, 15 dk, claim: `sub, role, jti, exp, iat`; imza anahtarı `/etc/iskele/secret.key`'den türetilir |
| Refresh token | 32 byte rastgele, DB'de SHA-256 hash; **rotasyon** (her kullanımda yenisi, eskisi revoke) |
| API token | `isk_<prefix>_<secret>` formatı; DB'de yalnız hash; scope + expiry |
| CSRF | Cookie tabanlı oturumda SameSite=Strict + double-submit token; Bearer kullanımında muaf |
| Rate limit | login: 5/dk/IP + 10 başarısızda 15 dk kilit; genel API: 100/dk/IP; WS handshake: 20/dk/IP |
| Path whitelist | `filepath.EvalSymlinks` sonrası prefix kontrolü; `..`, symlink, absolute bypass testleri |
| Secret şifreleme | AES-256-GCM, anahtar `/etc/iskele/secret.key` (0600, yoksa üretilir) |
| Audit maskeleme | env/labels içinde `PASS|TOKEN|SECRET|KEY|CREDENTIAL` regex'i eşleşen değerler `***` |
| Bind adresi | varsayılan `127.0.0.1:8377`; `0.0.0.0` seçilirse startup log'unda ve UI'da kalıcı uyarı |
| TLS | opsiyonel yerleşik (cert/key yolu), aksi halde reverse proxy örnekleri |
| HTTP başlıkları | `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy`, CSP (self + ws) |
| systemd | `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `ReadWritePaths`, `RestrictAddressFamilies`, `MemoryDenyWriteExecute`, boş `CapabilityBoundingSet` |
| Servis kullanıcısı | `iskele` sistem kullanıcısı, `docker` grubunda; root varsayılan değil |
| CI güvenlik | `govulncheck`, `gosec`, `npm audit`, CodeQL |

**RBAC matrisi** — route'lar rol değil **izin** ister (D-027); matris
`internal/server/middleware/rbac.go` içinde tek tablodur ve testte satır satır kapsanır:

| Aksiyon sınıfı | viewer | operator | admin |
|---|:--:|:--:|:--:|
| Okuma (list/inspect/logs/stats) | ✅ | ✅ | ✅ |
| start/stop/restart/pause/kill | ❌ | ✅ | ✅ |
| create/remove container, volume, network | ❌ | ✅ | ✅ |
| image pull/remove, stack up/down, template deploy | ❌ | ✅ | ✅ |
| build, prune, privileged/cap/device/host-bind | ❌ | ❌ | ✅ |
| users, settings, registries, audit | ❌ | ❌ | ✅ |

---

## 8. Frontend Yapısı

**Route ağacı**
```
/bootstrap                    (yalnız uninitialized)
/login
/                             → /dashboard
/dashboard
/containers                   /containers/:id  (overview|logs|stats|console|inspect|env|mounts|network)
/containers/new               (sihirbaz — M5)
/stacks                       /stacks/:id  /stacks/new                       (M7)
/images  /volumes  /networks
/catalog                      /catalog/:templateId                           (M8)
/builds                       /builds/:id                                    (M6)
/audit                                                                       (M8)
/settings                     (general|users|registries|paths|retention|appearance|about)
```

Route'lar ve sidebar öğeleri kendi milestone'larında ekleniyor; yapılmamış bir bölüm için
menü öğesi konmuyor (D-041).

**Ortak bileşenler:** container tablosu (filtre/sıralama/çoklu seçim/sanallaştırma 500+),
`LogViewer` (ring buffer, arama, indir, otomatik kaydır kilidi), `ConsolePanel` (xterm+fit+resize),
`ConfirmDialog` (ad yazdırarak onay), `EmptyState`, `StatCard`, `JsonViewer`, `ConnectionBanner`.
`TaskDrawer` M5'te, `CommandPreview` M5'te geliyor.
shadcn/ui yerine bu bileşenler elle yazıldı (D-039).

**State**
- Sunucu verisi: TanStack Query (`staleTime` 5 sn, liste ekranlarında 5 sn `refetchInterval`, docker event geldiğinde invalidate).
- İstemci state: Zustand — `authStore` (token, user, role), `uiStore` (tema, dil, sidebar), `taskStore` (aktif işler).
- WS/SSE: `useWebSocket` hook'u, exponential backoff (1s→30s), `ReconnectingBanner` bağlantı durumuna bağlı.

**Klavye kısayolları:** `/` arama, `g c`/`g i`/`g v`/`g n`/`g d` navigasyon, `Esc` modal kapat.
(`g s` stacks ile birlikte M7'de gelir.) Metin alanı ve terminal içindeyken kısayollar susar.
**Erişilebilirlik:** tüm ikon butonlarda `aria-label`, odak halkaları korunur.
**i18n:** hard-coded metin yasak; `locales/tr.json` ve `locales/en.json` anahtar bazında eşit.

---

## 9. Faz (Milestone) Planı

Her milestone'un **Definition of Done (DoD)** ortak maddeleri:
- `go build ./... && go vet ./... && go test ./...` yeşil
- `golangci-lint run` temiz · `npm run lint && npm run build` yeşil (M3'ten sonra)
- `PROGRESS.md` güncel, `DECISIONS.md`'e o fazda alınan kararlar eklenmiş
- `docs/openapi.yaml` eklenen endpoint'lerle senkron (M2'den sonra)
- Conventional Commit + `git push`

---

### M0 — İskelet
**Kapsam:** `go mod init github.com/ibrahimhates/iskele`; dizin ağacı; `internal/config` (flag > env > `/etc/iskele/config.yaml` > default) ve doğrulama; `internal/logging` (slog, level/format ayarlı); `internal/version` (ldflags); `cmd/iskeled` graceful shutdown; `/api/v1/health`, `/api/v1/version`; chi router + recover/requestid/logger/securityheaders middleware; `Makefile` (build/test/lint/run/web/clean/release-snapshot); `.gitignore`, `.editorconfig`, `.golangci.yml`, `LICENSE` (Apache-2.0), README taslağı; GitHub Actions `ci.yml` (Go build+test+lint, matrix go1.22/1.23).
**Çıktılar:** çalışan `iskeled` binary, `curl :8377/api/v1/health` → `{"status":"ok"}`.
**Testler:** config önceliği, config doğrulama hataları, health handler.
**DoD ek:** `make build` binary üretir; CI ilk kez yeşil.
**Risk:** yok. **Tahmini commit:** 3–5.

---

### M1 — Docker katmanı + temel API
**Kapsam:** `internal/docker/client.go` — `Client` interface (Container/Image/Volume/Network/System/Events alt kümeleri) ve SDK implementasyonu; API version negotiation; socket erişilemezse `DOCKER_UNAVAILABLE` + kullanıcıya "iskele kullanıcısı docker grubunda mı?" ipucu; container list/inspect/start/stop/restart/remove; image/volume/network list; UI DTO'ları (`types.go`); `internal/service` ilk servisleri; handler'lar; `internal/docker/fake` ile handler testleri.
**Çıktılar:** `GET /api/v1/containers` gerçek Docker'dan veri döner (auth henüz yok, M2'de kapatılır).
**Testler:** fake Docker ile her handler için 200/404/500 yolları; DTO dönüşüm testleri.
**Risk:** SDK sürüm/API uyumu → negotiation + minimum API 1.41 ile azaltılır.
**Tahmini commit:** 5–8.

---

### M2 — Auth + DB
**Kapsam:** `internal/store` (modernc sqlite, WAL, embedded migration runner); `0001_init.sql` (tüm tablolar); `internal/crypto` (keyfile + AES-GCM); `internal/auth` (argon2id, JWT, refresh rotasyonu, API token, brute-force); bootstrap akışı ve `NOT_INITIALIZED` kapısı; auth + rbac + ratelimit + csrf middleware; `internal/audit` + maskeleme; M1 endpoint'lerinin rol koruması altına alınması.
**Çıktılar:** bootstrap → login → korumalı endpoint zinciri uçtan uca çalışır.
**Testler:** parola hash/verify, JWT expiry/imza, refresh rotate+revoke, API token scope, RBAC matrisinin **tamamı**, brute-force kilidi, migration idempotency, audit maskeleme.
**Risk:** cgo'suz sqlite performansı → WAL + busy_timeout + tek yazar.
**Tahmini commit:** 6–9.

---

### M3 — Frontend iskeleti  ✅
**Kapsam:** Vite+React+TS+Tailwind kurulumu (shadcn yerine kendi bileşenleri, D-039); `web/` tooling (eslint, prettier, vitest); router ve korumalı route'lar; bootstrap ve login ekranları; AppShell + sidebar + topbar + tema toggle; TanStack Query client + fetch wrapper (401→refresh→retry); OpenAPI'den TS tip üretimi (`make gen-api`); i18n TR/EN; `embed.FS` ile `web/dist` gömme + SPA fallback (`/api` hariç tüm yollar `index.html`); `make build` frontend'i de derler.
**Çıktılar:** tek binary çalıştırıldığında tarayıcıda login ekranı gelir.
**Testler:** Vitest — fetch wrapper refresh akışı, protected route yönlendirmesi, i18n anahtar eşitliği testi.
**Risk (gerçekleşti, çözüldü):** embed edilecek `dist` yoksa Go derlemesi kırılır → `web/dist/.gitkeep`
commit'lendi ve `//go:embed all:dist` boş ağacı kabul ediyor; `web.Bundled()` false ise sunucu
`make build` gerektiğini söyleyen bir sayfa döndürüyor (D-045).
**Tahmini commit:** 6–9.

---

### M4 — Container yönetimi (tam)  ✅
**Kapsam:** Liste (durum, image, port, canlı CPU/RAM, uptime, health; filtre/arama/sıralama —
restart sayısı listede yok, gerekçesi D-049); toplu seçim + batch aksiyon; detay sayfası sekmeleri (Overview/Logs/Stats/Console/Inspect/Env/Mounts/Network); WS log stream (tail, timestamps, arama, indir); SSE stats + Recharts canlı grafik + ring buffer; xterm.js exec (shell seçimi, resize); redeploy (inspect'ten config türetip yeni image ile yeniden yarat); yıkıcı işlem onay diyaloğu; `ConnectionBanner`.
**Testler:** log/exec/stats handler'ları fake Docker ile; redeploy config türetme birim testi; LogViewer buffer ve Terminal resize Vitest testleri.
**Risk:** exec/TTY akışı → küçük entegrasyon testi + manuel doğrulama, `docker exec` semantiği ile karşılaştırma.
**Tahmini commit:** 8–12.

---

### M5 — Oluşturma sihirbazı + Image/Volume/Network  ✅
**Kapsam:** Container create sihirbazı — PROMPT §4.3'teki **tüm** alanlar (image/tag/pull policy, ad, restart policy, cmd/entrypoint, workdir, user, port map, bind/named/tmpfs volume + read-only, env satır satır + `.env` yapıştırma, network/alias/statik IP/extra hosts, labels, CPU/mem/pids limitleri, healthcheck, devices, cap add/drop, privileged, security-opt, log driver); canlı **`docker run` preview + API payload** paneli; image pull SSE ilerleme; registry CRUD + şifreli kimlik; image remove/prune/tag/history/inspect; volume ve network CRUD + prune + connect/disconnect; global TaskDrawer + iptal.
**Testler:** form→API payload dönüşümü (birim), whitelist ihlali reddi, registry şifreleme round-trip, zod şema testleri.
**Not (gerçekleşen):** zod eklenmedi. Doğrulama iki yerde: formu API'nin aldığı nesneye çeviren saf bir fonksiyon
(`buildSpec`, birim testli) ve sunucudaki `BuildCreateSpec` — ki otorite odur. Araya üçüncü bir şema koymak,
üçünün ayrışabileceği bir yüzey daha yaratırdı (D-050).
**Risk (gerçekleşti, çözüldü):** form karmaşıklığı → alanlar 10 sekmeye ayrıldı, canlı `docker run` önizlemesi
gönderilecek nesneden üretiliyor (D-058), whitelist ve privileged uyarıları gönderim öncesi çıkıyor.
**Tahmini commit:** 10–14.

---

### M6 — Dockerfile build
**Kapsam:** `/fs/browse` path browser (whitelist'li, symlink güvenli); build formu (dizin, Dockerfile adı, tag'ler, build args, target, no-cache, platform, pull); tar context üretimi (`.dockerignore` desteklenir); WS canlı build log + layer ilerleme + hata vurgusu + **iptal**; build geçmişi DB'de (kim/ne zaman/hangi dizin/sonuç/süre) + log arşivi (`/var/lib/iskele/builds/<id>.log`, retention'lı); "bu image'dan container oluştur" kısayolu (M5 sihirbazına image ön-dolgulu geçiş).
**Testler:** whitelist traversal/symlink saldırı vektörleri (tablo testi), tar context üretimi, build kayıt yaşam döngüsü, iptal → `canceled` durumu.
**Risk:** büyük context tar'ı → boyut limiti ayarı + akış (streaming) tar.
**Tahmini commit:** 6–9.

---

### M7 — Compose stack
**Kapsam:** `compose-go/v2` ile parse + `.env` interpolasyon; service→container/network/volume dönüşümü ve `up -d` (bağımlılık sırası, `depends_on`), `down`, `stop/start`, `restart`, `pull`, `scale`; stack CRUD (dosya yolu / editör / Git repo); Monaco YAML editör + şema doğrulama + kaydetmeden önce **diff**; servis durum tablosu; birleşik ve servis bazlı WS log; Iskele etiketleme (`com.iskele.stack`, `com.iskele.service`) ile stack keşfi.
**Testler:** ≥5 gerçek compose fixture parse; interpolasyon; diff üretimi; dönüşüm birim testleri (port/volume/network/healthcheck/deploy limits).
**Risk (en yüksek):** compose-spec kapsamının genişliği. **Azaltım:** desteklenen alan matrisi `docs/` içinde yayınlanır; desteklenmeyen alan sessizce yutulmaz, kullanıcıya uyarı olarak gösterilir. Aşırı karmaşıklaşırsa `docker compose` binary fallback'i ayardan açılabilir şekilde eklenir ve `DECISIONS.md`'e yazılır (bkz. D-006).
**Tahmini commit:** 10–15.

---

### M8 — App Catalog + Dashboard + Ayarlar
**Kapsam:** Template motoru + JSON şema (`docs/template-schema.md`) + doğrulama; **20 template** (`redis, postgres, mysql, mariadb, mongodb, cloudflared, nginx, caddy, traefik, portainer_agent, uptime-kuma, n8n, vaultwarden, minio, rabbitmq, adminer, pgadmin, watchtower, gitea, wg-easy`); parola alanları için "rastgele üret"; custom katalog `/etc/iskele/templates/`; deploy akışı (container veya stack); Dashboard (container/image/volume/network sayıları, `system df`, gopsutil host CPU/RAM/disk, engine sürümü, uptime); docker events → toast + activity feed; prune araçları; audit log ekranı (filtre + CSV/JSON export); kullanıcı yönetimi CRUD; TOTP 2FA; ayarlar sayfası (socket yolu, whitelist, retention, tema, dil).
**Testler:** her template için "geçerli payload üretir" tablo testi; şema doğrulama negatif testleri; dashboard aggregate servis testi; TOTP setup/verify/disable.
**Tahmini commit:** 10–14.

---

### M9 — Paketleme ve teslim
**Kapsam:** `deploy/iskeled.service` (tam hardening); `install.sh` (kullanıcı+grup, dizinler, `secret.key` 0600, binary `/usr/local/bin/`, config, enable+start, idempotent); `uninstall.sh` (`--purge` seçeneği); `.goreleaser.yaml` → amd64/arm64/armv7 + `.deb`/`.rpm` + checksum + SBOM; `release.yml` (tag push → release); tam README (özellikler, kurulum, güvenlik uyarısı, yapılandırma, ekran görüntüsü yer tutucuları, nginx/Caddy/Traefik reverse proxy örnekleri); `SECURITY.md`, `CONTRIBUTING.md`, `CHANGELOG.md`; `docs/` (openapi.yaml final, template-schema.md, architecture.md, configuration.md, security-model.md); `ACCEPTANCE.md` tüm maddeleri doğrulanıp işaretlenir; `v0.1.0` tag.
**DoD ek:** temiz bir Linux VM'de `install.sh` ile kurulum → tarayıcıdan bootstrap → container start/stop akışı manuel doğrulanır.
**Tahmini commit:** 6–10.

---

## 10. Test Stratejisi

| Alan | Yaklaşım | Hedef |
|---|---|---|
| `internal/docker` | Interface + `fake` implementasyon | dönüşüm mantığı %80 |
| `internal/service` | fake docker + `:memory:` store | %70 |
| `internal/server/handlers` | `httptest` + fake servis | mutlu yol + 4xx/5xx |
| `internal/auth` | tablo testleri (RBAC matrisi tam kapsanır) | %85 |
| `internal/paths` | traversal/symlink saldırı vektörleri | %90 |
| `internal/compose` | ≥5 gerçek fixture, golden dosya karşılaştırma | %70 |
| `internal/templates` | 20 template × geçerli payload | %100 template |
| Frontend | Vitest: form validasyon, log buffer, fetch refresh, i18n anahtar eşitliği | kritik bileşenler |
| **Toplam backend** | CI'da `-coverprofile`, rapor artifact | **≥ %60** |

Docker gerektiren entegrasyon testleri `//go:build integration` etiketi arkasında; CI'da opsiyonel job.

---

## 11. Build & Release Hattı

**Makefile hedefleri:** `build` (web + go), `web`, `run`, `dev`, `test`, `test-cover`, `lint`,
`gen-api`, `fmt`, `vuln`, `clean`, `release-snapshot`, `install-local`.

**ldflags:** `-s -w -X .../internal/version.Version=... .Commit=... .BuildDate=...`

**CI (`ci.yml`):** `go` job (matrix 1.25/stable: tidy kontrolü, gofmt, vet, build, `go test -race -coverpkg=./...`, coverage özeti) · `lint` job (golangci-lint v2.5.0) · `vuln` job (govulncheck) · `cross-compile` job (amd64/arm64/armv7). M3'ten sonra `web` job eklenir: `npm ci`, `npm run lint`, `npm run test`, `npm run build`.

**Release (`release.yml`):** `v*` tag → GoReleaser → 3 mimari arşiv + `.deb`/`.rpm` + `checksums.txt`
+ CHANGELOG'dan release notu.

---

## 12. Riskler ve Azaltımlar

| # | Risk | Etki | Azaltım |
|---|---|---|---|
| R1 | Compose spec kapsamı çok geniş (M7) | Yüksek | Desteklenen alan matrisi yayınla; desteklenmeyen alanda görünür uyarı; gerekirse CLI fallback (D-006) |
| R2 | Exec/TTY akışı ve resize semantiği | Orta | Erken prototip (M4 başı), `docker exec` ile davranış karşılaştırması |
| R3 | Docker API sürüm farklılıkları | Orta | `client.WithAPIVersionNegotiation()`, min API 1.41, sürüm bilgisini dashboard'da göster |
| R4 | Frontend `dist` embed'i ile Go derlemesinin bağı | Orta | `web/dist` placeholder + `make build` sıralaması + CI'da doğrulama |
| R5 | Path whitelist bypass (güvenlik) | Yüksek | `EvalSymlinks` sonrası kontrol, saldırı vektörü tablo testleri, admin-only build |
| R6 | SQLite eşzamanlı yazım kilitleri | Düşük | WAL + busy_timeout + kısa transaction |
| R7 | Bağlam/kapsam kayması, 20 template'in yükü | Orta | Template'ler tek şema + tek test tablosu; PROGRESS.md ile takip |
| R8 | Bellek: stats/log stream sızıntısı | Orta | Abone sayacı ile stream kapatma, ring buffer sabit boyut, `-race` testleri |

---

## 13. Sözlük

- **Stack:** Bir compose dosyasıyla yönetilen container grubu; `com.iskele.stack` etiketi ile işaretlenir.
- **Task:** Uzun süren iş (pull/build/up); `tasks.Manager`'da kayıtlı, iptal edilebilir, UI'da drawer'da görünür.
- **Template:** App Catalog'da tek tık deploy edilen JSON tanım.
- **Ticket:** WS/SSE bağlantıları için kısa ömürlü tek kullanımlık yetki belirteci.
- **Whitelist:** `config.allowed_paths` — bind mount ve build context'in çıkamayacağı dizin kümesi.
