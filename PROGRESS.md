# Iskele — İlerleme Takibi (PROGRESS.md)

> **Bu dosya her commit'te güncellenir.** Kural: bir görev tamamlandığında `[ ]` → `[x]`,
> milestone bitince "Durum" sütunu güncellenir ve "Son Durum" bölümüne tek satır not yazılır.
> Bağlam sınırına yaklaşıldığında önce bu dosya + `DECISIONS.md` güncellenir, sonra devam edilir.

**Son güncelleme:** 2026-08-08 · **Aktif faz:** M3 (başlamayı bekliyor) · **Sürüm hedefi:** v0.1.0

---

## 0. Özet Tablo

| Faz | Ad | Durum | İlerleme | Not |
|---|---|---|---|---|
| — | Planlama | ✅ Bitti | 4/4 | PLAN/PROGRESS/DECISIONS/ACCEPTANCE üretildi |
| M0 | İskelet | ✅ Bitti | 14/14 | Servis ayakta, CI kuruldu, kapsam %84.3 |
| M1 | Docker katmanı + temel API | ✅ Bitti | 13/13 | 12 endpoint, offline istemci, kapsam %82.1 |
| M2 | Auth + DB | ✅ Bitti | 16/16 | RBAC matrisi tam test edildi, kapsam %79.8 |
| M3 | Frontend iskeleti | ⬜ Bekliyor | 0/14 | |
| M4 | Container yönetimi (tam) | ⬜ Bekliyor | 0/16 | |
| M5 | Oluşturma sihirbazı + Image/Volume/Network | ⬜ Bekliyor | 0/18 | |
| M6 | Dockerfile build | ⬜ Bekliyor | 0/12 | |
| M7 | Compose stack | ⬜ Bekliyor | 0/15 | |
| M8 | App Catalog + Dashboard + Ayarlar | ⬜ Bekliyor | 0/18 | |
| M9 | Paketleme ve teslim | ⬜ Bekliyor | 0/16 | |

**Durum kodları:** ⬜ Bekliyor · 🟡 Devam ediyor · ✅ Bitti · 🔴 Bloke

### Build sağlığı (son çalıştırma: 2026-08-08, M2 sonu)

| Kontrol | Durum | Not |
|---|---|---|
| `go build ./...` | ✅ | — |
| `gofmt -l` | ✅ | temiz |
| `go vet ./...` | ✅ | — |
| `go test -race ./...` | ✅ | 14 paket, hepsi yeşil |
| `golangci-lint run` | ✅ | 0 issue (v2.5.0) |
| Cross-compile | ✅ | amd64 / arm64 / armv7 |
| `govulncheck` | ⚠️ | yerelde proxy engelliyor (`vuln.go.dev` 403); CI'da koşuyor |
| `npm run lint` | — | M3'te devreye girer |
| `npm run build` | — | M3'te devreye girer |
| Backend coverage | ✅ **%79.8** | hedef ≥ %60 (`-coverpkg=./...`) |

---

## Planlama (tamamlandı)

- [x] `PROMPT.md` okundu, gereksinimler çıkarıldı
- [x] `PLAN.md` — mimari, veri modeli, API yüzeyi, faz planı
- [x] `PROGRESS.md` — bu dosya
- [x] `DECISIONS.md` — başlangıç kararları (D-001…D-012)
- [x] `ACCEPTANCE.md` — kabul kriterleri listesi

---

## M0 — İskelet  ✅

**Hedef:** derlenen, çalışan, CI'sı yeşil boş bir servis.

- [x] `go mod init github.com/ibrahimhates/iskele` + temel bağımlılıklar (chi v5, yaml.v3)
- [x] Dizin ağacı (`cmd/`, `internal/`, `web/`, `deploy/`, `docs/`, `.github/`)
- [x] `internal/version` — ldflags ile Version/Commit/BuildDate + `debug.ReadBuildInfo` fallback
- [x] `internal/config` — struct, varsayılanlar, YAML yükleme (bilinmeyen anahtar reddedilir)
- [x] `internal/config` — flag > env > yaml öncelik zinciri (12 env değişkeni)
- [x] `internal/config/validate.go` — listen/docker_host/path/tls/ttl doğrulama, tüm hatalar tek seferde
- [x] `internal/logging` — slog kurulumu (level, auto/text/json), debug'da source
- [x] `internal/server` — http.Server, TLS opsiyonu, graceful shutdown (30 sn) + force close
- [x] `internal/server/router.go` + requestid/logger/recover/securityheaders middleware
- [x] `GET /api/v1/health` ve `GET /api/v1/version` (auth'suz)
- [x] `cmd/iskeled/main.go` — wiring, SIGINT/SIGTERM, public bind uyarısı
- [x] `Makefile` (build/run/test/test-cover/lint/fmt/fmt-check/vuln/clean/check) + `.gitignore` + `.editorconfig` + `.golangci.yml` (v2)
- [x] `LICENSE` (Apache-2.0) + README taslağı + `deploy/config.example.yaml`
- [x] `.github/workflows/ci.yml` — go matrix + lint + govulncheck + cross-compile (amd64/arm64/armv7)

**Ek olarak yapıldı:** `internal/httpx` paketi (standart hata gövdesi, hata kodları, `Handler` tipi) — handler↔server import döngüsünü kırmak için (D-014).

**Testler:** 8 paket · config öncelik zinciri ve 18 doğrulama vakası · logging seviye/format · request ID hostile header vektörleri · panic recovery · security header'lar · router 404/405 · graceful shutdown (in-flight istek tamamlanıyor) · port çakışması · health/version
**DoD:** ✅ `make build` binary üretir · ✅ `curl :8377/api/v1/health` → `{"status":"ok","uptime":"1s"}` · ✅ lint 0 issue · ✅ coverage %84.3 · ✅ commit + push

---

## M1 — Docker katmanı + temel API  ✅

- [x] `internal/docker/client.go` — `Client` interface (14 metot), `Connect` + versiyon negotiation
- [x] SDK implementasyonu (`docker/docker v28.5.2`) + ping/erişim kontrolü + 5 sn bağlantı zaman aşımı
- [x] `DOCKER_UNAVAILABLE` hatası + "docker grubu" ipuçlu mesaj + `Offline` istemci (D-021)
- [x] `container.go` — list / inspect / inspect-raw / start / stop / restart / remove
- [x] `image.go` — list (dangling tri-state) · `volume.go` — list · `network.go` — list
- [x] `system.go` — info / df (reclaimable hesabıyla)
- [x] `types.go` — 14 UI DTO'su + dönüşüm fonksiyonları (health parse, port binding, -1 sentinel)
- [x] `internal/docker/fake` — çağrı kaydı + operasyon bazlı hata enjeksiyonu + engine semantiği
- [x] `internal/service` — container, image, volume, network, system servisleri
- [x] `internal/httpx` + `handlers/errors.go` — engine hatası → HTTP kod eşlemesi (7 sınıf)
- [x] Handler'lar: containers (7 route), images, volumes, networks, system (ping/info/df)
- [x] Handler testleri — 200 / 400 / 404 / 409 / 500 / 502 / 503 yolları
- [x] `docs/openapi.yaml` — 14 path, 14 şema, tüm `$ref`'ler doğrulandı

**Ek olarak yapıldı:** `GET /system/ping` (planda M8'di) — UI'nın bağlantı bandı için, daima 200 (D-025).

**Testler:** engine hata sınıflandırması (8 vaka) · DTO dönüşümleri (container/detail/image/volume/network) ·
health status parse · docker zaman damgası sentinel'i · offline istemcinin 13 metodu · fake'in engine
sözleşmesine uyumu (+ race testi) · servis katmanı (boş ID reddi, opsiyon iletimi, force/volumes) ·
handler'lar (filtre iletimi, tri-state dangling, verbatim inspect, boş listeler `[]`, hata eşlemesi)

**DoD:** ✅ Docker'sız ortamda servis ayakta, her engine route'u `503 DOCKER_UNAVAILABLE` + eyleme
dönüştürülebilir mesaj döndürüyor (uçtan uca `curl` ile doğrulandı) · ✅ lint 0 issue · ✅ coverage %82.1 ·
✅ 3 mimariye cross-compile · ✅ commit + push

---

## M2 — Auth + DB  ✅

- [x] `internal/store/db.go` — modernc sqlite, WAL, `foreign_keys`, tek yazar bağlantısı (D-038)
- [x] `internal/store/migrations/0001_init.sql` — 6 tablo + 10 indeks + rol CHECK kısıtı
- [x] Migration runner (`embed.FS`, sıralı, transaction başına bir migration, idempotent)
- [x] `internal/crypto` — `secret.key` 0600 üretimi + **her açılışta izin doğrulaması** (D-037), AES-256-GCM, amaç bazlı alt anahtar
- [x] `internal/auth/password.go` — argon2id (t=3, m=64MiB, p=2), PHC formatı, min 12 karakter, `NeedsRehash`
- [x] `internal/auth/jwt.go` — HS256, algoritma/issuer pinlemesi, rol doğrulaması
- [x] `internal/auth/token.go` — refresh token üret/hash, `isk_<prefix>_<secret>` API token formatı
- [x] `internal/auth/bruteforce.go` — IP bazlı sayaç + kilit, başarı sayacı sıfırlar (D-029)
- [x] Bootstrap akışı + `NOT_INITIALIZED` kapısı (kurulum bitene kadar tüm API kapalı)
- [x] Handler'lar: status / bootstrap / login / refresh / logout / me
- [x] Middleware: auth (JWT + API token), rbac (8 izin, D-027), ratelimit (token bucket), csrf (D-028)
- [x] `internal/audit` — kayıt yazma + iç içe yapılarda secret maskeleme, iptal edilen istekte bile yazar
- [x] M1 endpoint'lerinin tamamı izin koruması altına alındı
- [x] Store repository'leri: users, sessions, tokens, audit, logins, settings
- [x] Testler: parola politikası ve hash, JWT (expiry/imza/alg-none/tampering/issuer/bilinmeyen rol), refresh rotasyonu + replay, **tam RBAC matrisi (14 route × 3 rol)**, brute-force, migration idempotency, maskeleme, rate limit, CSRF
- [x] `docs/openapi.yaml` — auth endpoint'leri, güvenlik şeması, yeni hata kodları

**Testler:** 6 yeni test dosyası · hesap numaralandırma karşıtı testler (aynı mesaj + sabit süre) ·
devre dışı/silinmiş hesabın mevcut token'ı reddedilmesi · API token ile kimlik doğrulama ve iptali ·
anahtar dosyası izinleri · AES-GCM kurcalama tespiti · audit'te sır sızıntısı kontrolü

**DoD:** ✅ Uçtan uca doğrulandı: kurulmamış sistem `409 NOT_INITIALIZED` → bootstrap → ikinci bootstrap
`409` → token'sız `401` → token'la `503 DOCKER_UNAVAILABLE` → refresh rotasyonu → eski token `401`.
Anahtar dosyası 0600, audit tablosunda `auth.bootstrap` ve `auth.refresh` kayıtları var.
✅ lint 0 issue · ✅ coverage %79.8 · ✅ commit + push

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
| 2026-08-08 | M2 ✅ | SQLite + migration'lar, argon2id, JWT + refresh rotasyonu, API token, brute-force limiti, 8 izinli RBAC matrisi, CSRF, rate limit, audit + maskeleme. M1'in tüm endpoint'leri koruma altında. Uçtan uca bootstrap→login→refresh zinciri doğrulandı. Lint 0 issue, coverage %79.8. |
| 2026-08-07 | Planlama | PLAN/PROGRESS/DECISIONS/ACCEPTANCE oluşturuldu. Kodlamaya başlama komutu bekleniyor. |
| 2026-08-07 | M1 ✅ | Docker katmanı: `Client` interface + SDK implementasyonu + fake + offline istemci. 12 yeni endpoint, OpenAPI spec'i, engine hatası → HTTP eşlemesi. Docker soketi olmayan ortamda uçtan uca doğrulandı. **Go minimumu 1.25'e yükseldi** (D-019, Docker SDK bağımlılık ağacı). Lint 0 issue, coverage %82.1. |
| 2026-08-07 | M0 ✅ | İskelet tamam: config zinciri, slog, chi router + 4 middleware, health/version, graceful shutdown, Makefile, CI (4 job). Uçtan uca doğrulandı: binary ayağa kalkıyor, `/api/v1/health` 200 dönüyor, SIGTERM ile temiz kapanıyor. Lint 0 issue, coverage %84.3. |

---

## Bloke Eden Konular

_(Şu an yok. Bir konu bloke ederse buraya tarih + açıklama + gereken karar yazılır ve `DECISIONS.md`'e bağlanır.)_
