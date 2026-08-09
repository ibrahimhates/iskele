# Iskele — İlerleme Takibi (PROGRESS.md)

> **Bu dosya her commit'te güncellenir.** Kural: bir görev tamamlandığında `[ ]` → `[x]`,
> milestone bitince "Durum" sütunu güncellenir ve "Son Durum" bölümüne tek satır not yazılır.
> Bağlam sınırına yaklaşıldığında önce bu dosya + `DECISIONS.md` güncellenir, sonra devam edilir.

**Son güncelleme:** 2026-08-09 · **Aktif faz:** M7 (başlamayı bekliyor) · **Sürüm hedefi:** v0.1.0

---

## 0. Özet Tablo

| Faz | Ad | Durum | İlerleme | Not |
|---|---|---|---|---|
| — | Planlama | ✅ Bitti | 4/4 | PLAN/PROGRESS/DECISIONS/ACCEPTANCE üretildi |
| M0 | İskelet | ✅ Bitti | 14/14 | Servis ayakta, CI kuruldu, kapsam %84.3 |
| M1 | Docker katmanı + temel API | ✅ Bitti | 13/13 | 12 endpoint, offline istemci, kapsam %82.1 |
| M2 | Auth + DB | ✅ Bitti | 16/16 | RBAC matrisi tam test edildi, kapsam %79.8 |
| M3 | Frontend iskeleti | ✅ Bitti | 14/14 | Tek binary UI'ı sunuyor, 35 frontend testi |
| M4 | Container yönetimi (tam) | ✅ Bitti | 16/16 | Log/stats/console/inspect + toplu işlem + redeploy |
| M5 | Oluşturma sihirbazı + Image/Volume/Network | ✅ Bitti | 18/18 | 10 sekmeli sihirbaz, canlı preview, registry, task drawer |
| M6 | Dockerfile build | ✅ Bitti | 12/12 | Path browser, tar context, canlı build log, iptal, geçmiş + log arşivi |
| M7 | Compose stack | ⬜ Bekliyor | 0/15 | |
| M8 | App Catalog + Dashboard + Ayarlar | ⬜ Bekliyor | 0/18 | |
| M9 | Paketleme ve teslim | ⬜ Bekliyor | 0/16 | |

**Durum kodları:** ⬜ Bekliyor · 🟡 Devam ediyor · ✅ Bitti · 🔴 Bloke

### Build sağlığı (son çalıştırma: 2026-08-09, M6 sonu)

| Kontrol | Durum | Not |
|---|---|---|
| `make build` | ✅ | web build + go build; 14 MB tek binary |
| `gofmt -l` | ✅ | temiz |
| `make vet` | ✅ | — |
| `make test` | ✅ | 15 paket, hepsi yeşil (`-race`) |
| `golangci-lint run` | ✅ | 0 issue (v2.5.0) |
| Cross-compile | ✅ | amd64 / arm64 / armv7 |
| `govulncheck` | ⚠️ | yerelde proxy engelliyor (`vuln.go.dev` 403); CI'da koşuyor |
| `npm run lint` | ✅ | 0 uyarı (`--max-warnings 0`) |
| `npm run format:check` | ✅ | prettier temiz |
| `npm run build` | ✅ | tsc + vite, uyarısız |
| `npm run test` | ✅ | 10 dosya / 94 test |
| Frontend bundle | ✅ | index 82 kB gz · vendor 54 kB gz · charts/terminal ayrı chunk |
| Backend coverage | ✅ **%73.3** | hedef ≥ %60. `make test-cover`'ın yöntemiyle (`-coverpkg` ile tüm paketler) ölçüldü; paket-başına ölçüm %46.0 çünkü `internal/server/handlers` gibi paketler kendi testleriyle değil `internal/server` testleriyle kapsanıyor. `make test-cover` bu ortamda koşmuyor (D-042), komut elle çalıştırıldı. |

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

## M3 — Frontend iskeleti  ✅

- [x] `web/` — Vite 5 + React 18 + TS (strict) kurulumu
- [x] Tailwind + CSS değişkeni tabanlı tema (açık/koyu, `dark` sınıfı) — shadcn/ui yerine kendi bileşenleri (D-039)
- [x] eslint + prettier + vitest yapılandırması
- [x] `src/api` — fetch wrapper (401 → tek paylaşımlı refresh → tek tekrar), `ApiError` kod eşlemesi
- [x] `make gen-api` — OpenAPI'den TS tip üretimi + `conformance.ts` ile derleme zamanı uyum kanıtı (D-040)
- [x] Router + `ProtectedRoute` + izin bazlı gizleme (`useAuth().can()`)
- [x] Bootstrap ekranı (parola gücü ölçer, doğrulama)
- [x] Login ekranı (hata mesajları, 429 geri bildirimi, `NOT_INITIALIZED` yönlendirmesi)
- [x] AppShell: sidebar, topbar, kullanıcı menüsü, tema toggle, klavye kısayolları
- [x] TanStack Query client + varsayılan ayarlar
- [x] i18n (react-i18next) + `locales/tr.json` + `locales/en.json`
- [x] `internal/server/spa.go` — `embed.FS` + SPA fallback (`/api` hariç), hash'li varlıklara `immutable` cache
- [x] `make build` → web build + go build tek komutta
- [x] Vitest: fetch refresh akışı, `ProtectedRoute`, i18n anahtar eşitliği, parola gücü, biçimleyiciler

**Not:** Sidebar yalnızca yapılmış bölümleri listeliyor. Henüz gelmemiş bölümler için "yakında" sayfası
konulmadı; çalışmayan bir menü öğesi PROMPT'un "işlevsiz UI yok" kuralına aykırı (D-041).

**DoD:** ✅ `make build` çıktısı tek başına UI'ı sunuyor · CI'da `web` ve `bundle` job'ları eklendi · commit + push

---

## M4 — Container yönetimi (tam)  ✅

**M4-A — Docker katmanı**
- [x] pause/unpause/kill/rename engine çağrıları
- [x] `ContainerLogs` — 8 baytlık multiplex başlığı çözümü, TTY tespiti, 64 KB satır sınırı
- [x] `ContainerStats` — `docker stats` ile aynı CPU formülü, page cache düşülmüş bellek, 60 örneklik ring buffer
- [x] `Exec` / `ResizeExec` / `ExecExitCode`
- [x] `Events` — engine olay akışı

**M4-B — WS/SSE altyapısı**
- [x] WS/SSE ticket mekanizması (60 sn, tek kullanımlık) + origin doğrulaması
- [x] WS `/containers/{id}/logs` (tail, follow, timestamps, stdout/stderr)
- [x] WS `/containers/{id}/exec` (binary=stdin, text=resize, exit kodu raporu, audit kaydı)
- [x] SSE `/containers/{id}/stats` + `/system/events` (25 sn heartbeat)
- [x] SSE `/containers/stats` — tüm çalışan container'lar tek bağlantıda, ID etiketli (D-048)
- [x] `POST /containers/batch` — hiçbir hata döngüyü durdurmuyor, kısmi başarıda 207
- [x] `POST /containers/{id}/redeploy` — inspect'ten config türetme + pull + recreate + rollback

**M4-C — Arayüz**
- [x] Container liste: durum, image, port map, uptime, health, **canlı CPU/RAM** (tek paylaşımlı akış).
      Restart sayısı listede yok — engine'in liste API'si döndürmüyor (D-049), detay sekmesinde var.
- [x] Filtre + arama + sıralama + 500+ satırda sanallaştırma
- [x] Çoklu seçim + toplu aksiyon (batch endpoint'i)
- [x] Aksiyonlar: start/stop/restart/pause/unpause/kill/rename/remove(force, volumes)
- [x] Yıkıcı işlem onayı (container adını yazdırma)
- [x] Detay sayfası 8 sekme (Overview/Logs/Stats/Console/Inspect/Env/Mounts/Network)
- [x] `LogViewer`: ring buffer, arama, stderr vurgusu, otomatik kaydırma kilidi, indirme
- [x] Stats sekmesi: CPU/mem/net/blk canlı grafikleri (Recharts)
- [x] `ConsolePanel`: xterm.js + fit + shell seçimi + resize
- [x] Inspect sekmesi: ham JSON görüntüleyici (arama, kopyala)
- [x] `ConnectionBanner` + üstel geri çekilmeli yeniden bağlanma (maks. 30 sn)

**Testler:** `internal/server/stream_test.go` (ticket, izin, WS/SSE handler'ları) · batch/redeploy servis testleri ·
`useLogStream.test.ts` (ring buffer sınırı, yeniden bağlanma, hata ve EOF davranışı) · `spa_test.go`

**DoD:** ✅ Docker soketi olmayan ortamda uçtan uca doğrulandı (bootstrap → ticket → batch → `DOCKER_UNAVAILABLE` zinciri);
gerçek container üzerinde log/stats/console doğrulaması Docker'lı bir makinede yapılmalı (bkz. Bloke Eden Konular) · commit + push

---

## M5 — Oluşturma sihirbazı + Image/Volume/Network  ✅

**M5-A — Docker katmanı**
- [x] `docker.ContainerSpec` — operatör diliyle tam container tanımı + `BuildCreateSpec` çevirisi
- [x] `PullImageProgress` (NDJSON ilerleme), `RemoveImage`, `PruneImages`, `TagImage`, `ImageHistory`, `InspectImageRaw`
- [x] `CreateVolume` / `InspectVolume` / `RemoveVolume` / `PruneVolumes`
- [x] `CreateNetwork` / `InspectNetwork` / `RemoveNetwork` / `PruneNetworks` / `Connect` / `Disconnect`
- [x] Fake ve offline istemciler yeni yüzeyin tamamını kapsıyor

**M5-B — Güvenlik ve kalıcılık**
- [x] `PathGuard` — bind mount'ları `allowed_paths`'e karşı doğruluyor; symlink çözüyor, bileşen bazlı karşılaştırıyor, boş liste = hepsini reddet
- [x] Privileged kapısı: `privileged`, `cap_add`, `devices`, `security_opt`, `sysctls`, `network=host`
- [x] `registries` tablosu + AES-GCM şifreli parola + `NormalizeRegistryServer` / `RegistryServerForImage`
- [x] Servisler: `Creator`, `Registry`, ve Image/Volume/Network mutasyonları (hepsi audit kayıtlı)

**M5-C — Uzun işler**
- [x] `TaskRegistry` — bellek içi, iptal edilebilir, 10 dk saklama, 200 görev tavanı
- [x] `GET /tasks`, `GET /tasks/{id}`, `POST /tasks/{id}/cancel`
- [x] Image pull bir task olarak kaydediliyor; katman bazlı ilerlemeden tek yüzde hesaplanıyor

**M5-D — API**
- [x] `POST /containers` (tam spec), `GET /system/allowed-paths`
- [x] `/images` pull(SSE)/remove/prune/tag/history/inspect
- [x] `/volumes` ve `/networks` tam CRUD + prune + connect/disconnect
- [x] `/registries` CRUD (admin-only), `/tasks`
- [x] `PATH_NOT_ALLOWED` hata kodu + `field` taşıyan 422; OpenAPI 60 endpoint'i kapsıyor

**M5-E — Oluşturma sihirbazı**
- [x] 10 sekme: Genel, Komut, Portlar, Volume'ler, Ortam, Ağ, Etiketler, Kaynaklar, Sağlık, Gelişmiş
- [x] `.env` yapıştırma (yorum, `export` öneki, tırnaklı değer, ilk `=`'de bölme)
- [x] Canlı **`docker run` komutu + API payload** önizlemesi — ikisi de gönderilen nesneden üretiliyor
- [x] Whitelist ihlali ve privileged seçenekler sunucuya gitmeden formda uyarılıyor

**M5-F — Kaynak ekranları**
- [x] Image: pull ilerleme çubuğu, katman geçmişi, inspect, tag, remove, prune
- [x] Volume: oluştur (driver), sil, prune, kullanım bilgisi
- [x] Network: oluştur (driver + subnet + internal), container bağla/çöz, sil, prune
- [x] Ayarlar altında registry CRUD (yalnız admin), parola asla geri dönmüyor
- [x] Global `TaskDrawer` — çalışan iş sayacı, ilerleme, iptal

**Testler:** `spec_test.go` (form→payload çevirisi, 20 vaka) · `paths_test.go` (whitelist, symlink kaçışı, traversal) ·
`create_test.go` servis + handler (privileged matrisi, whitelist reddi, audit) · `registries_test.go` (şifreleme
round-trip, parolanın sızmaması) · `tasks_test.go` · `preview.test.ts` + `state.test.ts` (35 frontend testi)

**DoD:** ✅ UI'dan sıfırdan container tanımlanıp gönderilebiliyor; whitelist dışı bind 403, bozuk alan 422,
privileged seçenek operator'a 403 dönüyor. Docker'lı bir makinede canlı oluşturma doğrulaması bekliyor · commit + push

---

## M6 — Dockerfile build  ✅

- [x] Yol whitelist'i — `EvalSymlinks` + bileşen bazlı kök kontrolü (`internal/service/paths.go`, M5'ten devralındı; ikinci bir uygulama yazılmadı → D-059)
- [x] `internal/service/browse.go` + `GET /fs/browse?path=`
- [x] Path browser UI bileşeni (whitelist kökleri, ileri/geri, dizin seçimi)
- [x] Build formu: dizin, Dockerfile adı, tag'ler, build args, label, target, no-cache, platform, pull
- [x] Tar context üretimi (`.dockerignore` desteği, negasyon + `**/`, boyut limiti, symlink takip edilmiyor)
- [x] `WS /build` — canlı log stream + adım/layer ilerleme + hata vurgusu
- [x] Build iptali (`POST /builds/{id}/cancel`) → `canceled` durumu
- [x] `builds` tablosu kayıt yaşam döngüsü (running→success/failed/canceled) + restart sonrası uzlaştırma
- [x] Log arşivi `<data_dir>/builds/<id>.log` + retention (log 30 gün, kayıt 180 gün)
- [x] `GET /builds`, `GET /builds/{id}`, `GET /builds/{id}/log`
- [x] Build geçmişi ekranı + log yeniden görüntüleme
- [x] "Bu image'dan container oluştur" kısayolu (M5 sihirbazına ön-dolgulu geçiş) — image listesi, build sonucu ve build geçmişinden

**Testler:** `browse_test.go` + `paths_test.go` (traversal/symlink tablosu, kök dışına çıkan bağ) ·
`context_test.go` (`.dockerignore` negasyon + `**/`, boyut limiti, symlink girdileri) · `build_test.go` servis
(kayıt yaşam döngüsü, iptal, log arşivi + retention, restart uzlaştırması) · `internal/server/build_test.go`
(gerçek WebSocket üzerinden uçtan uca build, izin matrisi, soket açılmadan önceki reddler, log replay) ·
`state.test.ts` + `useBuildStream.test.ts` (24 frontend testi)

**DoD:** ✅ Whitelist dışı dizin 403, eksik Dockerfile 422, context'ten kaçan Dockerfile 422 — hepsi soket
kabul edilmeden, canlı binary ile doğrulandı. Fake engine üzerinden build akıyor, iptal ediliyor, arşivlenen log
geri okunuyor. Gerçek bir engine ile build hâlâ elle koşulmalı (bkz. Bloke Eden Konular) · commit + push

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
| 2026-08-09 | M6 ✅ | Dockerfile build uçtan uca. En kritik karar `PathGuard`'ın yeniden kullanılması (D-059): gezinme, bind mount ve build context aynı güven sınırının üç yüzü, ikinci bir uygulama ikinci bir hata kaynağı olurdu. Build context bir `io.Pipe` üzerinden engine'e akıyor, diske ikinci kez yazılmıyor; `.dockerignore` negasyon ve `**/` ile destekleniyor; symlink'ler izlenmeden bağ olarak yazılıyor (D-061). Build, kendisini izleyen soketten uzun yaşıyor: sekme kapanınca yalnız frame gönderimi duruyor, kanallar sonuna kadar boşaltılıyor, log arşivleniyor ve kayıt kapanıyor (D-062). İki hata yol boyunca yakalandı: `TaskRegistry.Finish` task context'ini iptal ettiği için son `done` frame'i sessizce düşüyordu — gönderim context'i artık task'tan değil kökten türüyor; ve `/fs/browse` `read` izniyle açıktı, host dizinlerini sıralayan bir uç için fazla gevşek, `build`'e çekildi. `WS /build` isteği soket kabul edilmeden önce doğrulanıyor, bu yüzden whitelist dışı bir dizin veya eksik Dockerfile sıradan bir HTTP hatası olarak dönüyor (403/422). Lint 0 issue, coverage %73.3 (`-coverpkg`), 94 frontend testi. Docker soketi olmayan ortamda whitelist/doğrulama yüzeyi canlı binary ile doğrulandı; gerçek bir engine ile build hâlâ elle koşulmalı (G3/G4 🟡). |
| 2026-08-08 | M5 ✅ | Container oluşturma sihirbazı (10 sekme) ve tüm kaynak yönetimi. En kritik parça `PathGuard`: bind mount kaynakları `allowed_paths`'e karşı, symlink çözülerek ve bileşen bazlı karşılaştırılarak doğrulanıyor; boş whitelist hepsini reddediyor. Privileged seçenekler (`privileged`, `cap_add`, `devices`, `security_opt`, `sysctls`, `network=host`) ayrı bir izin kapısının arkasında ve hata mesajı hangisinin takıldığını söylüyor. Registry parolaları AES-GCM ile şifreli saklanıyor ve hiçbir yanıtta geri dönmüyor. Image pull SSE ile akıyor, bir task olarak kaydediliyor ve drawer'dan iptal edilebiliyor. Sihirbazın canlı `docker run` önizlemesi API'ye gidenle aynı nesneden üretiliyor, bu yüzden ayrışamıyor. Yol boyunca üç hata düzeltildi: pull akışında `done` olayı hata kanalıyla yarışıyordu, registry yanıtları sıfır zaman damgası (`0001-01-01`) yayıyordu, `Create` yazdığı zaman damgalarını çağırana bildirmiyordu. Lint 0 issue, coverage %60.4, 70 frontend testi. |
| 2026-08-08 | M3 + M4 ✅ | Frontend tek binary'ye gömüldü (`web/embed.go` + `internal/server/spa.go`): derin bağlantılar SPA kabuğunu, `/api` altındaki bilinmeyen yollar JSON 404'ü döndürüyor. Container yönetimi tam: liste + toplu işlem + 8 sekmeli detay, WS log/exec, SSE stats/events, redeploy. `make gen-api` OpenAPI'den TS tipi üretiyor, `conformance.ts` elle yazılan tiplerle spec'i derleme zamanında karşılaştırıyor. CI'ya `web` ve `bundle` job'ları eklendi. Üç gerçek hata yakalandı: `web/node_modules` altındaki üçüncü parti Go dosyası `./...`'a sızıyordu (D-042), `make test`/`make test-cover` `-race` ile CGO_ENABLED=0 yüzünden hiç koşamıyordu (D-043), `go.mod` tidy değildi. Lint 0 issue, coverage %71.9, 35 frontend testi. |
| 2026-08-08 | M2 ✅ | SQLite + migration'lar, argon2id, JWT + refresh rotasyonu, API token, brute-force limiti, 8 izinli RBAC matrisi, CSRF, rate limit, audit + maskeleme. M1'in tüm endpoint'leri koruma altında. Uçtan uca bootstrap→login→refresh zinciri doğrulandı. Lint 0 issue, coverage %79.8. |
| 2026-08-07 | Planlama | PLAN/PROGRESS/DECISIONS/ACCEPTANCE oluşturuldu. Kodlamaya başlama komutu bekleniyor. |
| 2026-08-07 | M1 ✅ | Docker katmanı: `Client` interface + SDK implementasyonu + fake + offline istemci. 12 yeni endpoint, OpenAPI spec'i, engine hatası → HTTP eşlemesi. Docker soketi olmayan ortamda uçtan uca doğrulandı. **Go minimumu 1.25'e yükseldi** (D-019, Docker SDK bağımlılık ağacı). Lint 0 issue, coverage %82.1. |
| 2026-08-07 | M0 ✅ | İskelet tamam: config zinciri, slog, chi router + 4 middleware, health/version, graceful shutdown, Makefile, CI (4 job). Uçtan uca doğrulandı: binary ayağa kalkıyor, `/api/v1/health` 200 dönüyor, SIGTERM ile temiz kapanıyor. Lint 0 issue, coverage %84.3. |

---

## Bloke Eden Konular

| Tarih | Konu | Durum |
|---|---|---|
| 2026-08-08 | Bu geliştirme ortamında Docker soketi yok. Log/stats/console yolları fake engine ve offline istemciyle test edildi; **gerçek bir container üzerinde canlı doğrulama yapılmadı.** | Bloke değil — kod yolları test altında, ama Docker'lı bir makinede elle bir tur atılması gerekiyor. |
| 2026-08-08 | `govulncheck` yerelde koşmuyor (`vuln.go.dev` proxy tarafından 403). | Bloke değil — CI'da koşuyor. |
| 2026-08-08 | `make test-cover` yerelde koşmuyor: bu ortamın Go araç zincirinde `covdata` yok. `make test` (`-race`, kapsamsız) yeşil. | Bloke değil — CI'da tam araç zinciri var. |
| 2026-08-09 | Gerçek bir engine ile Dockerfile build koşulmadı (soket yok). WS build akışı, iptal, log arşivi ve geçmiş fake engine ile uçtan uca test ediliyor; whitelist ve doğrulama reddleri canlı binary ile doğrulandı. | Bloke değil — G3/G4 🟡 olarak işaretli, Docker'lı bir makinede bir tur atılması gerekiyor. |
