# Iskele — İlerleme Takibi (PROGRESS.md)

> **Bu dosya her commit'te güncellenir.** Kural: bir görev tamamlandığında `[ ]` → `[x]`,
> milestone bitince "Durum" sütunu güncellenir ve "Son Durum" bölümüne tek satır not yazılır.
> Bağlam sınırına yaklaşıldığında önce bu dosya + `DECISIONS.md` güncellenir, sonra devam edilir.

**Son güncelleme:** 2026-08-07 · **Aktif faz:** M0 (başlamayı bekliyor) · **Sürüm hedefi:** v0.1.0

---

## 0. Özet Tablo

| Faz | Ad | Durum | İlerleme | Not |
|---|---|---|---|---|
| — | Planlama | ✅ Bitti | 4/4 | PLAN/PROGRESS/DECISIONS/ACCEPTANCE üretildi |
| M0 | İskelet | ⬜ Bekliyor | 0/14 | Başlama komutu bekleniyor |
| M1 | Docker katmanı + temel API | ⬜ Bekliyor | 0/13 | |
| M2 | Auth + DB | ⬜ Bekliyor | 0/16 | |
| M3 | Frontend iskeleti | ⬜ Bekliyor | 0/14 | |
| M4 | Container yönetimi (tam) | ⬜ Bekliyor | 0/16 | |
| M5 | Oluşturma sihirbazı + Image/Volume/Network | ⬜ Bekliyor | 0/18 | |
| M6 | Dockerfile build | ⬜ Bekliyor | 0/12 | |
| M7 | Compose stack | ⬜ Bekliyor | 0/15 | |
| M8 | App Catalog + Dashboard + Ayarlar | ⬜ Bekliyor | 0/18 | |
| M9 | Paketleme ve teslim | ⬜ Bekliyor | 0/16 | |

**Durum kodları:** ⬜ Bekliyor · 🟡 Devam ediyor · ✅ Bitti · 🔴 Bloke

### Build sağlığı (son çalıştırma)

| Kontrol | Durum | Zaman |
|---|---|---|
| `go build ./...` | — | — |
| `go vet ./...` | — | — |
| `go test ./...` | — | — |
| `golangci-lint run` | — | — |
| `npm run lint` | — | — |
| `npm run build` | — | — |
| Backend coverage | — | hedef ≥ %60 |

---

## Planlama (tamamlandı)

- [x] `PROMPT.md` okundu, gereksinimler çıkarıldı
- [x] `PLAN.md` — mimari, veri modeli, API yüzeyi, faz planı
- [x] `PROGRESS.md` — bu dosya
- [x] `DECISIONS.md` — başlangıç kararları (D-001…D-012)
- [x] `ACCEPTANCE.md` — kabul kriterleri listesi

---

## M0 — İskelet  ⬜

**Hedef:** derlenen, çalışan, CI'sı yeşil boş bir servis.

- [ ] `go mod init github.com/ibrahimhates/iskele` + temel bağımlılıklar
- [ ] Dizin ağacı (`cmd/`, `internal/`, `web/`, `deploy/`, `docs/`, `.github/`)
- [ ] `internal/version` — ldflags ile Version/Commit/BuildDate
- [ ] `internal/config` — struct, varsayılanlar, YAML yükleme
- [ ] `internal/config` — flag > env > yaml öncelik zinciri
- [ ] `internal/config/validate.go` — listen/path/tls doğrulama + anlamlı hatalar
- [ ] `internal/logging` — slog kurulumu (level, json/text), request logger
- [ ] `internal/server` — http.Server, TLS opsiyonu, graceful shutdown (30 sn)
- [ ] `internal/server/router.go` + recover/requestid/logger/securityheaders middleware
- [ ] `GET /api/v1/health` ve `GET /api/v1/version` (auth'suz)
- [ ] `cmd/iskeled/main.go` — wiring, SIGINT/SIGTERM
- [ ] `Makefile` (build/run/test/lint/fmt/clean) + `.gitignore` + `.editorconfig` + `.golangci.yml`
- [ ] `LICENSE` (Apache-2.0) + README taslağı + `deploy/config.example.yaml`
- [ ] `.github/workflows/ci.yml` — build + test + lint, ilk yeşil koşu

**Testler:** config önceliği · config doğrulama hataları · health/version handler
**DoD:** `make build` binary üretir · `curl :8377/api/v1/health` → `{"status":"ok"}` · CI yeşil · commit + push

---

## M1 — Docker katmanı + temel API  ⬜

- [ ] `internal/docker/client.go` — `Client` interface tanımı (tüm alt kümeler)
- [ ] SDK implementasyonu + API version negotiation + ping/erişim kontrolü
- [ ] `DOCKER_UNAVAILABLE` hatası + "docker grubu" ipuçlu mesaj
- [ ] `container.go` — list / inspect / start / stop / restart / remove
- [ ] `image.go` — list · `volume.go` — list · `network.go` — list
- [ ] `system.go` — info / df
- [ ] `types.go` — UI DTO'ları ve dönüşüm fonksiyonları
- [ ] `internal/docker/fake` — testler için tam fake implementasyon
- [ ] `internal/service/container.go` (+ image/volume/network servisleri)
- [ ] `internal/server/errors.go` + `response.go` — standart hata gövdesi
- [ ] Handler'lar: containers, images, volumes, networks
- [ ] Handler testleri (200 / 404 / 500 yolları)
- [ ] `docs/openapi.yaml` — ilk sürüm (M1 endpoint'leri)

**DoD:** gerçek Docker'a bağlı `GET /api/v1/containers` doğru veri döner · testler yeşil · commit + push

---

## M2 — Auth + DB  ⬜

- [ ] `internal/store/db.go` — modernc sqlite, WAL, pragma, açılış
- [ ] `internal/store/migrations/0001_init.sql` — tüm tablolar + indeksler
- [ ] Migration runner (`embed.FS`, sıralı, idempotent, `schema_migrations`)
- [ ] `internal/crypto` — `secret.key` üretimi (0600) + AES-GCM encrypt/decrypt
- [ ] `internal/auth/password.go` — argon2id hash/verify + min 12 karakter kuralı
- [ ] `internal/auth/jwt.go` — access token issue/parse/validate
- [ ] `internal/auth/session.go` — refresh token üret/rotate/revoke
- [ ] `internal/auth/apitoken.go` — `isk_` formatı, scope, expiry, hash
- [ ] `internal/auth/bruteforce.go` — IP+kullanıcı bazlı sayaç ve kilit
- [ ] Bootstrap akışı + `NOT_INITIALIZED` kapısı (diğer tüm endpoint'ler kapalı)
- [ ] Handler'lar: bootstrap / login / refresh / logout / me
- [ ] Middleware: auth (JWT + API token), rbac (rol matrisi), ratelimit, csrf
- [ ] `internal/audit` — kayıt yazma + secret maskeleme
- [ ] M1 endpoint'lerinin rol koruması altına alınması
- [ ] Store repository'leri: users, sessions, tokens, audit, settings
- [ ] Testler: hash, JWT expiry/imza, refresh rotate+revoke, **tam RBAC matrisi**, brute-force, migration, maskeleme

**DoD:** bootstrap → login → korumalı endpoint zinciri uçtan uca çalışır · commit + push

---

## M3 — Frontend iskeleti  ⬜

- [ ] `web/` — Vite + React 18 + TS kurulumu
- [ ] Tailwind + shadcn/ui + tema değişkenleri (dark varsayılan)
- [ ] eslint + prettier + vitest yapılandırması
- [ ] `src/api` — fetch wrapper (401 → refresh → retry), hata tipi eşlemesi
- [ ] `make gen-api` — OpenAPI'den TS tip üretimi
- [ ] Router + `ProtectedRoute` + rol bazlı gizleme
- [ ] Bootstrap ekranı (parola gücü, doğrulama)
- [ ] Login ekranı (hata mesajları, rate limit geri bildirimi)
- [ ] AppShell: sidebar (10 bölüm), topbar, kullanıcı menüsü, tema toggle
- [ ] TanStack Query client + varsayılan ayarlar
- [ ] i18n (react-i18next) + `locales/tr.json` + `locales/en.json`
- [ ] `internal/server/spa.go` — `embed.FS` + SPA fallback (`/api` hariç)
- [ ] `make build` → web build + go build tek komutta
- [ ] Vitest: fetch refresh akışı, protected route, i18n anahtar eşitliği

**DoD:** tek binary çalıştırılınca tarayıcıda login/bootstrap ekranı gelir · CI'da web adımları yeşil · commit + push

---

## M4 — Container yönetimi (tam)  ⬜

- [ ] Container liste: durum, image, port map, CPU/RAM, uptime, health, restart sayısı
- [ ] Filtre + arama + etikete göre gruplama + sıralama + sanallaştırma (500+)
- [ ] Çoklu seçim + `POST /containers/batch` (toplu aksiyon)
- [ ] Aksiyonlar: start/stop/restart/pause/unpause/kill/rename/remove(force, volumes)
- [ ] Yıkıcı işlem onayı (container adını yazdırma)
- [ ] Detay sayfası sekme iskeleti (Overview/Logs/Stats/Console/Inspect/Env/Mounts/Network)
- [ ] WS `/containers/{id}/logs` backend (tail, follow, timestamps, stdout/stderr)
- [ ] `LogViewer` bileşeni: ring buffer, ANSI, arama, otomatik kaydırma kilidi, indir
- [ ] SSE `/containers/{id}/stats` backend + in-memory ring buffer (60 örnek)
- [ ] Stats sekmesi: CPU/mem/net/blk canlı grafikler (Recharts)
- [ ] WS `/containers/{id}/exec` backend (attach, tty, resize)
- [ ] `TerminalPane`: xterm.js + fit + shell seçimi + resize
- [ ] Inspect sekmesi: ham JSON görüntüleyici (katlanabilir, arama, kopyala)
- [ ] `POST /containers/{id}/redeploy` — inspect'ten config türetme + pull + recreate
- [ ] WS/SSE ticket mekanizması + origin doğrulaması
- [ ] `ReconnectingBanner` + exponential backoff yeniden bağlanma

**Testler:** log/exec/stats handler'ları (fake) · redeploy config türetme · LogViewer buffer · Terminal resize
**DoD:** gerçek bir container üzerinde log akar, konsol açılır, stats grafiği çizer · commit + push

---

## M5 — Oluşturma sihirbazı + Image/Volume/Network  ⬜

- [ ] `POST /containers` — tam create spec (host config dahil)
- [ ] Sihirbaz sekme 1: Image + tag + pull policy + ad + restart policy
- [ ] Sihirbaz sekme 2: Komut/entrypoint/workdir/user
- [ ] Sihirbaz sekme 3: Port mapping (çoklu satır, proto)
- [ ] Sihirbaz sekme 4: Volumes (bind + path picker, named, tmpfs, read-only)
- [ ] Sihirbaz sekme 5: Env (satır satır + `.env` yapıştır/import)
- [ ] Sihirbaz sekme 6: Network (seçim, alias, statik IP, extra hosts)
- [ ] Sihirbaz sekme 7: Labels
- [ ] Sihirbaz sekme 8: Kaynaklar (CPU, memory limit/reservation, pids)
- [ ] Sihirbaz sekme 9: Healthcheck (test, interval, timeout, retries, start period)
- [ ] Sihirbaz sekme 10: Gelişmiş (devices, cap add/drop, privileged, security-opt, log driver) — **admin-only alanlar işaretli**
- [ ] Canlı **Preview** paneli: `docker run ...` komutu + API payload
- [ ] `POST /images/pull` SSE + ilerleme çubuğu + katman bazlı durum
- [ ] Registry CRUD + AES-GCM şifreli kimlik + pull'da auth kullanımı
- [ ] Image ekranı: liste, remove/force, prune, tag, history, inspect, kullanan container sayısı
- [ ] Volume ekranı: liste, oluştur (driver+opts), remove, prune, kullanım bilgisi
- [ ] Network ekranı: liste, oluştur (bridge/macvlan/overlay, subnet/gateway), remove, prune, connect/disconnect, inspect
- [ ] Global `TaskDrawer` + `GET /tasks` + iptal

**Testler:** form→payload dönüşümü · zod şemaları · whitelist ihlali reddi · registry şifreleme round-trip
**DoD:** UI'dan sıfırdan çalışan container yaratılabiliyor · commit + push

---

## M6 — Dockerfile build  ⬜

- [ ] `internal/paths/whitelist.go` — `EvalSymlinks` + prefix kontrolü
- [ ] `internal/paths/browse.go` + `GET /fs/browse?path=`
- [ ] Path browser UI bileşeni (whitelist kökleri, ileri/geri, dizin seçimi)
- [ ] Build formu: dizin, Dockerfile adı, tag'ler, build args, target, no-cache, platform, pull
- [ ] Tar context üretimi (`.dockerignore` desteği, boyut limiti)
- [ ] `WS /build` — canlı log stream + layer ilerleme + hata vurgusu
- [ ] Build iptali (`POST /builds/{id}/cancel`) → `canceled` durumu
- [ ] `builds` tablosu kayıt yaşam döngüsü (running→success/failed/canceled)
- [ ] Log arşivi `/var/lib/iskele/builds/<id>.log` + retention
- [ ] `GET /builds`, `GET /builds/{id}`, `GET /builds/{id}/log`
- [ ] Build geçmişi ekranı + log yeniden görüntüleme
- [ ] "Bu image'dan container oluştur" kısayolu (M5 sihirbazına ön-dolgulu geçiş)

**Testler:** traversal/symlink saldırı vektörleri (tablo) · tar context · build kaydı · iptal
**DoD:** gerçek bir dizinden build çalışır, log akar, iptal edilebilir · commit + push

---

## M7 — Compose stack  ⬜

- [ ] `internal/compose/parse.go` — compose-go v2 parse + normalize
- [ ] `.env` yükleme + değişken interpolasyon
- [ ] `convert.go` — service → container/network/volume spec (portlar, volume, healthcheck, limitler, depends_on)
- [ ] Desteklenen alan matrisi (`docs/compose-support.md`) + desteklenmeyen alan uyarısı
- [ ] `up.go` — bağımlılık sıralı `up -d`, Iskele etiketleri (`com.iskele.stack`, `.service`)
- [ ] `down.go`, `stop/start`, `restart`, `pull`, `scale`
- [ ] `diff.go` — kaydetmeden önce diff üretimi
- [ ] `git.go` — repo clone/pull (ref seçimi)
- [ ] Stack CRUD API (`/stacks`, `/stacks/{id}`) + kaynak tipleri (file/editor/git)
- [ ] `WS /stacks/{id}/logs` — birleşik + servis bazlı
- [ ] Stacks liste ekranı + servis durum tablosu
- [ ] Monaco YAML editör + şema doğrulama + hata işaretleri
- [ ] Diff görünümü (kaydetmeden önce)
- [ ] `.env` yönetimi ekranı
- [ ] Mevcut compose ile başlatılmış container'ların stack olarak keşfi

**Testler:** ≥5 gerçek compose fixture · interpolasyon · diff · dönüşüm birim testleri
**DoD:** UI'dan yazılan bir compose stack ayağa kalkar ve logları akar · commit + push

---

## M8 — App Catalog + Dashboard + Ayarlar  ⬜

- [ ] Template JSON şeması + `internal/templates/schema.go` doğrulama
- [ ] Template motoru (render → container/stack payload)
- [ ] `docs/template-schema.md`
- [ ] 20 template: redis, postgres, mysql, mariadb, mongodb
- [ ] 20 template: cloudflared, nginx, caddy, traefik, portainer_agent
- [ ] 20 template: uptime-kuma, n8n, vaultwarden, minio, rabbitmq
- [ ] 20 template: adminer, pgadmin, watchtower, gitea, wg-easy
- [ ] Custom katalog dizini `/etc/iskele/templates/` yükleme
- [ ] Catalog UI: kategori/arama/ikon, dinamik form, parola "rastgele üret", deploy akışı
- [ ] Dashboard: container/image/volume/network sayıları + `system df`
- [ ] Dashboard: gopsutil host CPU/RAM/disk + engine sürümü + uptime
- [ ] `SSE /system/events` — docker events akışı
- [ ] Toast bildirimleri + activity feed
- [ ] Prune araçları (dangling image, stopped container, unused volume/network) + onay
- [ ] Audit log ekranı: filtre (aktör/aksiyon/tarih) + CSV/JSON export
- [ ] Kullanıcı yönetimi: CRUD, rol atama, parola sıfırlama, devre dışı bırakma
- [ ] TOTP 2FA: setup (QR), verify, disable, login akışına entegrasyon
- [ ] Ayarlar sayfası: socket yolu, whitelist, retention, tema, dil, bind uyarısı

**Testler:** her template için geçerli payload · şema negatif testleri · dashboard aggregate · TOTP
**DoD:** katalogdan tek tıkla redis + postgres deploy edilir · commit + push

---

## M9 — Paketleme ve teslim  ⬜

- [ ] `deploy/iskeled.service` — tam systemd hardening
- [ ] `deploy/install.sh` — kullanıcı/grup, dizinler, secret.key, binary, config, enable+start (idempotent)
- [ ] `deploy/uninstall.sh` — `--purge` seçeneği ile
- [ ] `deploy/reverse-proxy/` — nginx, Caddy, Traefik örnekleri (WS upgrade dahil)
- [ ] `.goreleaser.yaml` — amd64/arm64/armv7 + `.deb`/`.rpm` + checksum + SBOM
- [ ] `.github/workflows/release.yml` — tag → release
- [ ] `.github/workflows/codeql.yml` + CI'da `govulncheck` ve `npm audit`
- [ ] README (tam): özellikler, kurulum, güvenlik uyarısı, yapılandırma, ekran görüntüsü yer tutucuları
- [ ] `SECURITY.md` — tehdit modeli, socket=root uyarısı, bildirim süreci
- [ ] `CONTRIBUTING.md` — geliştirme kurulumu, commit kuralları, PR süreci
- [ ] `CHANGELOG.md` — v0.1.0
- [ ] `docs/architecture.md`
- [ ] `docs/openapi.yaml` — final, tüm endpoint'lerle senkron
- [ ] `docs/configuration.md` + `docs/security-model.md` + `docs/development.md`
- [ ] Temiz VM'de kurulum → bootstrap → container start/stop manuel doğrulaması
- [ ] `ACCEPTANCE.md` tüm maddeleri doğrulandı ve işaretlendi → `v0.1.0` tag

**DoD:** release artifact'ları üretildi, ACCEPTANCE tamamen yeşil · commit + push + tag

---

## Son Durum

| Tarih | Faz | Not |
|---|---|---|
| 2026-08-07 | Planlama | PLAN/PROGRESS/DECISIONS/ACCEPTANCE oluşturuldu. Kodlamaya başlama komutu bekleniyor. |

---

## Bloke Eden Konular

_(Şu an yok. Bir konu bloke ederse buraya tarih + açıklama + gereken karar yazılır ve `DECISIONS.md`'e bağlanır.)_
