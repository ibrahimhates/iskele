# Iskele — Claude Code Görev Promptu

> Bu dosyayı Claude Code oturumunun ilk mesajı olarak ver. Repo boşken başlat.

---

## 0. ÇALIŞMA MODU (ÖNCE BUNU OKU)

Bu bir **tek seferlik, uçtan uca teslim** görevidir. Aşağıdaki kurallara uy:

1. **Onay bekleme.** Plan onayı, dosya oluşturma onayı, "devam edeyim mi?" sorusu sorma. Belirsizlik varsa `DECISIONS.md` dosyasına varsayımı yaz, en makul seçeneği uygula, devam et.
2. **Önce plan, sonra kesintisiz uygulama.** İlk adımda `PLAN.md` üret (tüm milestone'lar, dosya ağacı, API yüzeyi, veri modeli). Sonra M0'dan M9'a kadar **durmadan** uygula.
3. **Her milestone sonunda:** `go build ./...` + `go test ./...` + `npm run build` yeşil olacak. Kırmızıysa düzeltmeden sonraki milestone'a geçme.
4. **Her milestone sonunda commit at.** Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`, `test:`). Milestone bitince `git push`.
5. **Durum takibi:** repo kökünde `PROGRESS.md` tut. Her milestone için `[ ]` / `[x]` ve tek satır not. Her commit'te güncelle.
6. **Bağlam sınırına yaklaşırsan:** `PROGRESS.md` + `DECISIONS.md`'i güncelle, kaldığın yerden devam et. Özet çıkarıp kullanıcıya dönme.
7. **İş bitti sayılma kriteri:** M9 tamamlanmadan ve `ACCEPTANCE.md` içindeki tüm maddeler işaretlenmeden görevi bitmiş sayma.
8. **Yasak:** placeholder fonksiyon (`// TODO: implement`), mock veri ile geçiştirilmiş endpoint, çalışmayan UI butonu. Yazdığın her şey gerçekten çalışacak.
9. Kullanıcıya sadece milestone tamamlandığında **2-3 satırlık** ilerleme notu yaz. Uzun anlatım yok.

---

## 1. PROJE TANIMI

**Iskele**, Linux sunucuda **native systemd servisi** olarak çalışan, Docker container/image/volume/network ve Docker Compose stack'lerini yöneten, tek binary halinde dağıtılan açık kaynak bir web yönetim panelidir.

**Kritik kısıt:** Uygulamanın kendisi container içinde çalışmaz. Host üzerinde `iskeled` adlı systemd servisi olarak çalışır ve `/var/run/docker.sock` üzerinden Docker Engine API ile konuşur.

**Hedef kullanıcı:** Tek sunucu ya da birkaç sunucu yöneten geliştirici / homelab kullanıcısı. Portainer'ın hafif, bağımlılıksız, native alternatifi.

**Lisans:** Apache-2.0.

---

## 2. TEKNOLOJİ SEÇİMLERİ (SABİT — DEĞİŞTİRME)

| Katman | Seçim | Gerekçe |
|---|---|---|
| Backend | Go 1.22+ | Tek statik binary, systemd'ye ideal, cgo'suz derleme |
| Docker erişimi | `github.com/docker/docker/client` (resmi SDK) | Shell'e `docker` komutu çağırmak **yasak** |
| HTTP router | `chi` v5 | Stdlib'e yakın, middleware zinciri |
| DB | SQLite — `modernc.org/sqlite` (saf Go, cgo yok) | Cross-compile edilebilir |
| Migration | `goose` veya elle yazılmış embedded SQL migration'lar | |
| Frontend | React 18 + TypeScript + Vite | |
| UI kit | Tailwind CSS + shadcn/ui | |
| State/data | TanStack Query + Zustand | |
| Terminal | xterm.js + WebSocket | exec ve log stream için |
| Auth | Kendi JWT implementasyonu (`golang-jwt/jwt/v5`) + argon2id parola hash | |
| Frontend gömme | `embed.FS` ile Go binary içine | Tek dosya dağıtımı |
| Build | Makefile + GoReleaser | |
| CI | GitHub Actions | |

**Cross-compile hedefleri:** `linux/amd64`, `linux/arm64`, `linux/armv7`.

---

## 3. MİMARİ

```
iskele/
├── cmd/
│   └── iskeled/main.go            # entrypoint, flag/config, graceful shutdown
├── internal/
│   ├── config/                    # YAML + env + flag birleşimi
│   ├── server/                    # HTTP server, router, middleware
│   │   ├── router.go
│   │   ├── middleware/            # auth, rate limit, logging, recover, CSRF
│   │   └── handlers/              # her kaynak için ayrı dosya
│   ├── docker/                    # Docker SDK sarmalayıcı (tek erişim noktası)
│   │   ├── client.go
│   │   ├── container.go
│   │   ├── image.go
│   │   ├── network.go
│   │   ├── volume.go
│   │   ├── build.go               # Dockerfile build + stream log
│   │   ├── exec.go                # WebSocket exec
│   │   └── events.go              # docker events -> internal event bus
│   ├── compose/                   # compose dosya parse/up/down/ps
│   ├── store/                     # SQLite repository katmanı
│   │   ├── migrations/
│   │   └── models.go
│   ├── auth/                      # kullanıcı, oturum, RBAC, API token
│   ├── templates/                 # tek tık app catalog (redis, postgres, ...)
│   ├── events/                    # pub/sub, SSE/WS yayını
│   ├── audit/                     # denetim kaydı
│   └── version/
├── web/                           # Vite projesi
│   ├── src/
│   └── dist/                      # embed edilir
├── deploy/
│   ├── iskeled.service            # systemd unit
│   ├── install.sh                 # kurulum scripti
│   └── uninstall.sh
├── docs/
├── .github/workflows/
├── Makefile
├── PLAN.md  PROGRESS.md  DECISIONS.md  ACCEPTANCE.md
├── README.md  CONTRIBUTING.md  SECURITY.md  LICENSE
└── go.mod
```

**Katman kuralı:** handler → service → docker/store. Handler içinde doğrudan Docker SDK çağrısı yok.

---

## 4. FONKSİYONEL GEREKSİNİMLER

### 4.1 Kimlik doğrulama ve yetki
- İlk açılışta **bootstrap ekranı**: admin kullanıcı + parola oluşturma (kurulum tamamlanana kadar başka endpoint çalışmaz).
- Parola: argon2id, min 12 karakter zorunluluğu.
- JWT access token (15 dk) + refresh token (7 gün, DB'de saklanan, revoke edilebilir).
- Roller: `admin` (her şey), `operator` (container start/stop/restart/log, deploy yok), `viewer` (salt okuma).
- API token üretimi (headless kullanım için, `Authorization: Bearer`), scope ve expiry ile.
- Opsiyonel TOTP 2FA (M8).
- Brute-force koruması: IP başına başarısız giriş limiti.

### 4.2 Container yönetimi
- Liste: durum, image, port map, CPU/RAM anlık kullanımı, uptime, health status, restart sayısı.
- Filtre + arama + etiketle gruplama, sıralama.
- Aksiyonlar: start / stop / restart / pause / unpause / kill / remove (force seçeneği) / rename.
- **Toplu işlem:** çoklu seçim ile aynı aksiyonu uygulama.
- Detay sayfası sekmeleri: Overview, Logs, Stats, Console, Inspect (ham JSON), Files (opsiyonel M9), Env, Mounts, Network.
- **Loglar:** WebSocket ile canlı stream, `tail` sayısı seçilebilir, timestamps toggle, metin araması, indirme.
- **Console:** xterm.js ile `docker exec -it` eşleniği. Shell seçimi (`/bin/sh`, `/bin/bash`), terminal resize (SIGWINCH) desteği.
- **Stats:** CPU %, memory kullanımı/limiti, network I/O, block I/O — canlı grafik (son 60 örnek, in-memory ring buffer).
- Container'dan **"redeploy"**: aynı config ile yeni image çekip yeniden yaratma (config'i inspect'ten türet).

### 4.3 Container oluşturma sihirbazı
Formda desteklenmesi zorunlu alanlar:
- Image + tag (registry arama/autocomplete opsiyonel), pull policy
- Container adı, restart policy, komut/entrypoint override, working dir, user
- Port mapping (host:container/proto, çoklu satır)
- Volume: bind mount (host path picker ile), named volume, tmpfs, read-only bayrağı
- Environment variables (satır satır + `.env` dosyası yapıştırma/import)
- Network seçimi, alias, statik IP, extra hosts
- Labels
- Kaynak limitleri: CPU limit, memory limit/reservation, pids limit
- Healthcheck: test komutu, interval, timeout, retries
- Devices, capabilities (add/drop), privileged, security-opt
- Log driver + opsiyonları
- **"Preview" paneli:** formun karşılığı olan `docker run ...` komutu ve API payload'ı canlı gösterilir.

### 4.4 Dockerfile'dan build
- Sunucudaki bir dizin yolu girilir (veya izinli dizinler arasından **path browser** ile seçilir).
- Dockerfile adı seçilebilir (`Dockerfile`, `Dockerfile.prod` ...).
- Build args, target stage (multi-stage), image tag, `--no-cache`, platform, pull bayrakları.
- Build çıktısı **canlı stream** (WebSocket), layer bazlı ilerleme, hata durumunda kırmızı vurgulama, iptal edilebilir.
- Build sonrası "bu image'dan container oluştur" kısayolu.
- Build geçmişi DB'de: kim, ne zaman, hangi dizin, sonuç, süre, log arşivi.

### 4.5 Compose stack yönetimi
- Stack kaynağı üç yoldan: (a) sunucudaki mevcut compose dosyası yolu, (b) UI'da editörle yazma (Monaco, YAML syntax + şema doğrulama), (c) Git repo URL'inden clone/pull.
- İşlemler: `up -d`, `down`, `restart`, `pull`, `stop`, `start`, `logs` (tüm servisler birleşik + servis bazlı), servis ölçekleme.
- `.env` dosyası yönetimi ve değişken interpolasyonu.
- Stack içindeki servisler tek ekranda durum tablosu olarak.
- Compose v2 spec desteği. **Uygulama:** `docker compose` CLI'yi çağırmak yerine öncelikle `compose-go` (`github.com/compose-spec/compose-go/v2`) ile parse edip Docker API'ye çevir. Bu çok karmaşıklaşırsa `docker compose` binary'sine düşüp bunu `DECISIONS.md`'e yaz — ama fallback'i kullanıcının görebileceği şekilde ayarda belirt.
- Değişiklikleri kaydetmeden önce **diff** göster.

### 4.6 App Catalog (tek tık deploy)
- `internal/templates/catalog/` altında **JSON template** dosyaları. Her template: ad, açıklama, kategori, ikon, image, alanlar (tip: text/number/password/select/bool/port/path/volume, default, required, validation regex, help metni), oluşturulacak container/compose tanımı.
- Kullanıcı formu doldurur → "Deploy" → container/stack ayağa kalkar.
- **Başlangıç için yazılacak template'ler (en az bunlar):**
  `redis`, `postgres`, `mysql`, `mariadb`, `mongodb`, `cloudflared` (tunnel token ile), `nginx`, `caddy`, `traefik`, `portainer_agent`, `uptime-kuma`, `n8n`, `vaultwarden`, `minio`, `rabbitmq`, `adminer`, `pgadmin`, `watchtower`, `gitea`, `wg-easy`.
- Parola tipindeki alanlar için "rastgele üret" butonu.
- Template şeması `docs/template-schema.md` içinde belgelenir; kullanıcı kendi template'ini `/etc/iskele/templates/` altına koyabilir (custom katalog).

### 4.7 Image / Volume / Network
- **Image:** liste (repo, tag, boyut, oluşturma tarihi, kullanan container sayısı), pull (registry auth destekli, ilerleme çubuğu), remove/force remove, prune, tag, history, inspect, export/save (opsiyonel).
- **Volume:** liste, oluştur (driver + opts), remove, prune, hangi container kullanıyor, boyut (mümkünse).
- **Network:** liste, oluştur (bridge/macvlan/overlay, subnet/gateway), remove, prune, container bağla/kopar, inspect.
- **Registry yönetimi:** özel registry ekleme (URL, kullanıcı adı, parola), kimlik bilgileri **şifrelenmiş** saklanır (AES-GCM, anahtar `/etc/iskele/secret.key` — 0600).

### 4.8 Sistem / Dashboard
- Ana sayfa: container sayıları (running/stopped/unhealthy), image/volume/network sayıları, disk kullanımı (`docker system df`), host CPU/RAM/disk (gopsutil), Docker Engine sürümü, uptime.
- `docker events` akışını dinleyip UI'ya canlı bildirim (toast + activity feed).
- Prune araçları (dangling image, stopped container, unused volume/network) — onay diyaloğu ile.
- **Audit log:** kim, ne zaman, hangi kaynağa, hangi işlemi yaptı. Filtrelenebilir, dışa aktarılabilir.

### 4.9 Ayarlar
- Kullanıcı yönetimi (CRUD, rol atama, parola sıfırlama).
- Docker socket yolu, TCP/TLS ile uzak Docker host tanımlama (çoklu endpoint, M9).
- Tema (light/dark/system), dil (TR/EN — i18n altyapısı `react-i18next`, tüm metinler dosyada).
- İzinli host dizin whitelist'i (bind mount ve Dockerfile build için — güvenlik açısından zorunlu).
- Log retention, event retention ayarları.

---

## 5. API TASARIMI

`/api/v1` prefix. JSON. Hata gövdesi standardı:
```json
{ "error": { "code": "CONTAINER_NOT_FOUND", "message": "...", "details": {} } }
```

Zorunlu endpoint grupları:
```
POST   /api/v1/auth/bootstrap        (yalnızca kurulum yapılmamışsa)
POST   /api/v1/auth/login
POST   /api/v1/auth/refresh
POST   /api/v1/auth/logout
GET    /api/v1/auth/me

GET    /api/v1/containers                  ?all=&filter=
POST   /api/v1/containers
GET    /api/v1/containers/{id}
DELETE /api/v1/containers/{id}             ?force=&volumes=
POST   /api/v1/containers/{id}/start|stop|restart|pause|unpause|kill|rename
POST   /api/v1/containers/{id}/redeploy
GET    /api/v1/containers/{id}/inspect
GET    /api/v1/containers/{id}/stats       (SSE)
WS     /api/v1/containers/{id}/logs
WS     /api/v1/containers/{id}/exec
POST   /api/v1/containers/batch            {ids:[], action:""}

GET    /api/v1/images ; POST /api/v1/images/pull (SSE) ; DELETE /api/v1/images/{id}
POST   /api/v1/images/prune
WS     /api/v1/build                       (Dockerfile build + log stream)
GET    /api/v1/builds ; GET /api/v1/builds/{id}/log

GET    /api/v1/volumes  POST  DELETE  POST /prune
GET    /api/v1/networks POST  DELETE  POST /prune  POST /{id}/connect|disconnect

GET    /api/v1/stacks ; POST /api/v1/stacks ; GET/PUT/DELETE /api/v1/stacks/{id}
POST   /api/v1/stacks/{id}/up|down|restart|pull
WS     /api/v1/stacks/{id}/logs

GET    /api/v1/templates ; POST /api/v1/templates/{id}/deploy

GET    /api/v1/system/info|df|events(SSE)
POST   /api/v1/system/prune
GET    /api/v1/audit

GET    /api/v1/fs/browse?path=            (whitelist içinde dizin listeleme)

GET    /api/v1/users  POST  PUT  DELETE
GET    /api/v1/settings  PUT
GET    /api/v1/health   /api/v1/version   (auth'suz)
```

OpenAPI 3.1 spec'i `docs/openapi.yaml` olarak üret ve endpoint'lerle senkron tut.

---

## 6. GÜVENLİK GEREKSİNİMLERİ (İHMAL EDİLEMEZ)

1. Docker socket erişimi = root eşdeğeri. `README.md` ve `SECURITY.md` içinde bu açıkça yazılacak.
2. Servis `iskele` adlı ayrı sistem kullanıcısı ile çalışır; kullanıcı `docker` grubuna eklenir. Root olarak çalıştırma **varsayılan değil**.
3. systemd unit'inde hardening: `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`, `ReadWritePaths=/var/lib/iskele /etc/iskele`, `RestrictAddressFamilies`, `MemoryDenyWriteExecute=yes`, `CapabilityBoundingSet=`.
4. Bind mount ve build path'leri **whitelist** dışına çıkamaz. Path traversal testleri yazılacak (`../`, symlink, absolute bypass).
5. `privileged: true` ve tehlikeli capability'ler yalnız `admin` rolüne açık, UI'da açık uyarı ile.
6. Tüm state-changing endpoint'lerde CSRF koruması (SameSite=Strict cookie + double submit token) ya da yalnız Bearer token kabulü.
7. Rate limiting: login endpoint'i sıkı, genel API gevşek.
8. Secret'lar (registry parolası, tunnel token) DB'de AES-GCM ile şifreli. Loglara secret yazılmaz — env değerleri audit log'da maskelenir.
9. Varsayılan bind adresi `127.0.0.1:8377`. Kullanıcı açıkça `0.0.0.0` yapmadıkça dışarı açılmaz; yaptığında UI ve log'da TLS/reverse-proxy uyarısı çıkar.
10. Opsiyonel yerleşik TLS (cert/key yolu) desteği.
11. Bağımlılıklar için `govulncheck` ve `npm audit` CI'da çalışır.
12. WebSocket bağlantılarında origin doğrulaması + token kontrolü.

---

## 7. UI/UX GEREKSİNİMLERİ

- Sol dikey sidebar: Dashboard, Containers, Stacks, Images, Volumes, Networks, App Catalog, Builds, Audit, Settings.
- Dark mode varsayılan; light mode toggle.
- Mobil uyumlu (sidebar collapse, tablolar kart görünümüne düşer).
- Tablolar: sunucu tarafı değil client-side filtre yeterli (tek sunucu ölçeği), sanallaştırma 500+ satırda.
- Yıkıcı işlemler (`remove`, `prune`, `down`) için container adını yazdırarak onaylatma.
- Uzun süren işlemler (pull, build, up) için global "task drawer": ilerleme, iptal, log.
- Klavye kısayolları: `/` arama, `g c` containers, `g s` stacks.
- Bağlantı kopunca "reconnecting" bandı; WebSocket otomatik yeniden bağlanma (exponential backoff).
- Hata mesajları teknik detayı gizlemez; Docker'ın döndürdüğü hata metni gösterilir.
- Boş durum (empty state) ekranları her liste için yazılır.

---

## 8. MILESTONE PLANI

Sırayla, durmadan uygula. Her biri bitince build + test + commit + push.

**M0 — İskelet**
`go mod init github.com/<owner>/iskele`, dizin yapısı, config yükleme (flag > env > `/etc/iskele/config.yaml`), zerolog/slog yapılandırılmış log, graceful shutdown, `/health` ve `/version`, Makefile, `.gitignore`, LICENSE, README taslağı, GitHub Actions (build + test + lint).

**M1 — Docker katmanı + temel API**
Docker client wrapper, container list/inspect/start/stop/restart/remove, image list, volume list, network list. Docker erişilemezse anlamlı hata. Unit testler (Docker SDK interface'i mock'lanır).

**M2 — Auth + DB**
SQLite şema + migration'lar, users/sessions/api_tokens/audit_logs tabloları, bootstrap akışı, JWT, RBAC middleware, audit yazımı, rate limit.

**M3 — Frontend iskeleti**
Vite + React + TS + Tailwind + shadcn/ui kurulumu, router, login/bootstrap ekranı, layout + sidebar, TanStack Query client, API tip tanımları (OpenAPI'den üretim tercih edilir), dark mode, i18n (TR/EN), `embed.FS` ile Go'ya gömme ve SPA fallback route.

**M4 — Container yönetimi (tam)**
Liste + detay + aksiyonlar + toplu işlem + WebSocket log stream + SSE stats + xterm.js exec konsolu + inspect JSON görüntüleyici.

**M5 — Oluşturma sihirbazı + Image/Volume/Network**
Container create formunun tüm alanları, `docker run` preview, image pull ilerleme akışı, registry auth ve şifreli saklama, volume/network CRUD ve prune ekranları.

**M6 — Dockerfile build**
Path browser (whitelist'li), build formu, canlı build log stream, iptal, build geçmişi ve log arşivi, "image'dan container oluştur" akışı.

**M7 — Compose stack**
compose-go parse, stack CRUD, Monaco YAML editör + şema doğrulama + diff, up/down/pull/restart, servis tablosu, birleşik log, `.env` yönetimi, Git repo'dan stack.

**M8 — App Catalog + Dashboard + Ayarlar**
Template motoru ve şeması, 20 template, deploy akışı, custom template dizini, dashboard metrikleri ve host istatistikleri, docker events canlı akışı, audit log ekranı, kullanıcı yönetimi, TOTP 2FA, ayarlar sayfası.

**M9 — Paketleme ve teslim**
`deploy/iskeled.service` (hardening ile), `install.sh` (kullanıcı oluştur, binary'yi `/usr/local/bin/`e koy, dizinleri hazırla, servisi enable+start), `uninstall.sh`, GoReleaser ile 3 mimariye release + `.deb`/`.rpm`, checksum, GitHub Actions release workflow, tam README (ekran görüntüsü yer tutucuları, kurulum, yapılandırma, reverse proxy örnekleri: nginx + Caddy + Traefik), `SECURITY.md`, `CONTRIBUTING.md`, `CHANGELOG.md`, `docs/` (openapi.yaml, template-schema.md, architecture.md), `ACCEPTANCE.md` doldurulup işaretlenir, `v0.1.0` tag.

---

## 9. TEST GEREKSİNİMLERİ

- Docker katmanı bir Go interface'i arkasında; handler testleri fake implementasyon kullanır.
- Auth: token üretimi/doğrulama/expiry/revoke, RBAC matrisi, brute-force limiti.
- Path whitelist: traversal ve symlink saldırı vektörleri.
- Compose parse: en az 5 gerçek compose dosyası fixture'ı.
- Template render: her template için "geçerli payload üretiyor mu" testi.
- Frontend: Vitest ile kritik bileşenler (form validasyonu, log viewer buffer).
- Hedef backend kapsam: %60+. Test coverage'ı CI'da raporla.
- Lint: `golangci-lint` (errcheck, govet, staticcheck, gosec) + `eslint` + `prettier`. CI'da zorunlu.

---

## 10. VARSAYILAN DEĞERLER

```yaml
# /etc/iskele/config.yaml
listen: "127.0.0.1:8377"
docker_host: "unix:///var/run/docker.sock"
data_dir: "/var/lib/iskele"
allowed_paths:
  - "/opt/stacks"
  - "/srv"
log_level: "info"
tls:
  enabled: false
  cert_file: ""
  key_file: ""
session:
  access_ttl: "15m"
  refresh_ttl: "168h"
```

---

## 11. ŞİMDİ BAŞLA

1. `PLAN.md`, `PROGRESS.md`, `DECISIONS.md`, `ACCEPTANCE.md` dosyalarını oluştur.
2. M0'dan başla.
3. M9 bitene kadar durma.
