# Iskele — Kabul Kriterleri (ACCEPTANCE.md)

> **Görev, bu dosyadaki tüm maddeler işaretlenmeden bitmiş sayılmaz** (`PROMPT.md` §0.7).
> Her madde ya `[x]` işaretlenir ya da gerekçesiyle **Kapsam Dışı** tablosuna taşınır.
> "Doğrulama" sütunu, maddenin nasıl kanıtlandığını söyler: `test` (otomatik test), `manuel` (elle koşulan senaryo),
> `CI` (pipeline çıktısı), `gözle` (UI incelemesi).

**Sürüm:** v0.1.0 · **Toplam madde:** 120 · **İşaretli:** 26 (M2 sonu)

---

## A. Derleme, Dağıtım ve Çalıştırma

| # | Kriter | Doğrulama | Faz | ✔ |
|---|---|---|---|:--:|
| A1 | `make build` tek statik binary üretir (`CGO_ENABLED=0`) | CI | M0 | [x] |
| A2 | `go build ./...` ve `go vet ./...` temiz | CI | tüm | [ ] |
| A3 | `go test ./...` yeşil, `-race` ile de geçer | CI | tüm | [ ] |
| A4 | `golangci-lint run` (errcheck, govet, staticcheck, gosec) temiz | CI | tüm | [ ] |
| A5 | `npm run lint` ve `npm run build` yeşil | CI | M3+ | [ ] |
| A6 | Binary `linux/amd64`, `linux/arm64`, `linux/armv7` için cross-compile edilir | CI | M9 | [ ] |
| A7 | Frontend `embed.FS` ile binary'ye gömülüdür; harici dosya gerekmez | manuel | M3 | [ ] |
| A8 | `iskeled --help` tüm flag'leri açıklar; `--version` sürüm/commit/tarih basar | manuel | M0 | [x] |
| A9 | Config önceliği flag > env > `/etc/iskele/config.yaml` > varsayılan | test | M0 | [x] |
| A10 | Hatalı config anlamlı hata mesajıyla çıkış yapar (panic yok) | test | M0 | [x] |
| A11 | `SIGTERM` ile graceful shutdown: aktif istekler tamamlanır, WS'ler kapanır, DB kapanır | manuel | M0 | [x] |
| A12 | Docker erişilemezken servis açılır, UI `DOCKER_UNAVAILABLE` uyarısı gösterir | manuel | M1 | [x] |
| A13 | `install.sh` temiz bir Linux VM'de kurulumu uçtan uca tamamlar (idempotent) | manuel | M9 | [ ] |
| A14 | `uninstall.sh` servisi ve dosyaları temizler; `--purge` veriyi de siler | manuel | M9 | [ ] |
| A15 | `systemctl status iskeled` aktif; reboot sonrası otomatik başlar | manuel | M9 | [ ] |
| A16 | GoReleaser `.deb`, `.rpm`, arşivler ve `checksums.txt` üretir | CI | M9 | [ ] |

## B. Kimlik Doğrulama ve Yetkilendirme

| # | Kriter | Doğrulama | Faz | ✔ |
|---|---|---|---|:--:|
| B1 | Kurulmamış sistemde yalnız `/auth/bootstrap` çalışır, diğerleri `NOT_INITIALIZED` döner | test | M2 | [x] |
| B2 | Bootstrap yalnız bir kez çalışır; ikinci deneme `ALREADY_INITIALIZED` | test | M2 | [x] |
| B3 | Parola argon2id ile hash'lenir; 12 karakterden kısa parola reddedilir | test | M2 | [x] |
| B4 | Access token 15 dk, refresh token 7 gün; süresi dolan token reddedilir | test | M2 | [x] |
| B5 | Refresh token rotasyonlu; kullanılan token tekrar kullanılamaz | test | M2 | [x] |
| B6 | Logout refresh token'ı revoke eder; sonraki refresh `UNAUTHORIZED` | test | M2 | [x] |
| B7 | `admin`/`operator`/`viewer` rol matrisinin tamamı test edilir | test | M2 | [x] |
| B8 | `viewer` hiçbir state-changing endpoint'i çağıramaz (403) | test | M2 | [x] |
| B9 | `operator` build, prune, users, settings çağıramaz (403) | test | M2 | [x] |
| B10 | API token `Authorization: Bearer` ile çalışır; scope ve expiry uygulanır | test | M2 | [x] |
| B11 | Başarısız girişte IP bazlı limit devreye girer, kilit süresi uygulanır | test | M2 | [x] |
| B12 | TOTP 2FA kurulabilir, doğrulanır, devre dışı bırakılabilir; login akışına girer | test | M8 | [ ] |
| B13 | Kullanıcı CRUD, rol atama, parola sıfırlama, devre dışı bırakma çalışır | manuel | M8 | [ ] |
| B14 | Devre dışı kullanıcının mevcut oturumları geçersiz olur | test | M8 | [x] |

## C. Güvenlik

| # | Kriter | Doğrulama | Faz | ✔ |
|---|---|---|---|:--:|
| C1 | Varsayılan bind `127.0.0.1:8377`; `0.0.0.0` seçilirse log ve UI'da TLS/proxy uyarısı | manuel | M0/M3 | [ ] |
| C2 | Bind mount ve build path'i `allowed_paths` dışına çıkamaz | test | M6 | [ ] |
| C3 | Path traversal (`../`), symlink ve absolute bypass vektörleri reddedilir | test | M6 | [ ] |
| C4 | `privileged`, cap add, devices, security-opt, host-bind yalnız `admin`; UI'da uyarı | test+gözle | M5 | [ ] |
| C5 | State-changing endpoint'lerde CSRF koruması (cookie akışı) veya yalnız Bearer kabulü | test | M2 | [x] |
| C6 | Login endpoint'i sıkı, genel API gevşek rate limit uygular | test | M2 | [x] |
| C7 | Registry parolaları ve tunnel token'ları DB'de AES-GCM ile şifreli | test | M5 | [ ] |
| C8 | `/etc/iskele/secret.key` 0600 izinle üretilir; yoksa oluşturulur | test | M2 | [x] |
| C9 | Audit log ve uygulama loglarında secret değerler maskelenir | test | M2 | [x] |
| C10 | WebSocket bağlantılarında `Origin` doğrulaması ve ticket kontrolü yapılır | test | M4 | [ ] |
| C11 | systemd unit hardening direktiflerinin tamamı mevcut (`NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `ReadWritePaths`, `RestrictAddressFamilies`, `MemoryDenyWriteExecute`, boş `CapabilityBoundingSet`) | gözle | M9 | [ ] |
| C12 | Servis `iskele` sistem kullanıcısıyla çalışır (root varsayılan değil), `docker` grubunda | manuel | M9 | [ ] |
| C13 | `README.md` ve `SECURITY.md` "docker socket = root eşdeğeri" uyarısını açıkça içerir | gözle | M9 | [ ] |
| C14 | CI'da `govulncheck` ve `npm audit` çalışır | CI | M9 | [ ] |
| C15 | Güvenlik başlıkları (CSP, X-Frame-Options, nosniff, Referrer-Policy) yanıtlarda mevcut | test | M0 | [x] |
| C16 | Opsiyonel yerleşik TLS (cert/key) çalışır | manuel | M9 | [ ] |

## D. Container Yönetimi

| # | Kriter | Doğrulama | Faz | ✔ |
|---|---|---|---|:--:|
| D1 | Liste: durum, image, port map, CPU/RAM, uptime, health, restart sayısı gösterilir | gözle | M4 | [ ] |
| D2 | Filtre, arama, etiketle gruplama, sıralama çalışır | gözle | M4 | [ ] |
| D3 | start / stop / restart / pause / unpause / kill / rename / remove(force, volumes) çalışır | manuel | M4 | [ ] |
| D4 | Çoklu seçimle toplu aksiyon uygulanır, kısmi hata raporlanır | manuel | M4 | [ ] |
| D5 | Detay sekmeleri: Overview, Logs, Stats, Console, Inspect, Env, Mounts, Network | gözle | M4 | [ ] |
| D6 | Canlı log akışı çalışır; tail sayısı, timestamps toggle, arama, indirme mevcut | manuel | M4 | [ ] |
| D7 | Console `docker exec -it` eşleniğidir; shell seçimi ve resize (SIGWINCH) çalışır | manuel | M4 | [ ] |
| D8 | Stats: CPU %, mem kullanım/limit, net I/O, blk I/O canlı grafik (son 60 örnek) | gözle | M4 | [ ] |
| D9 | Redeploy: yeni image çekip aynı config ile yeniden yaratır; hata olursa rollback | manuel | M4 | [ ] |
| D10 | Inspect sekmesi ham JSON'u okunur biçimde gösterir | gözle | M4 | [ ] |

## E. Container Oluşturma Sihirbazı

| # | Kriter | Doğrulama | Faz | ✔ |
|---|---|---|---|:--:|
| E1 | Image + tag + pull policy, ad, restart policy alanları | gözle | M5 | [ ] |
| E2 | Komut/entrypoint override, working dir, user | gözle | M5 | [ ] |
| E3 | Port mapping (host:container/proto, çoklu satır) | test | M5 | [ ] |
| E4 | Volume: bind (path picker ile), named volume, tmpfs, read-only | test | M5 | [ ] |
| E5 | Env: satır satır giriş + `.env` yapıştırma/import | test | M5 | [ ] |
| E6 | Network seçimi, alias, statik IP, extra hosts | test | M5 | [ ] |
| E7 | Labels | test | M5 | [ ] |
| E8 | Kaynak limitleri: CPU, memory limit/reservation, pids limit | test | M5 | [ ] |
| E9 | Healthcheck: test komutu, interval, timeout, retries | test | M5 | [ ] |
| E10 | Devices, capabilities (add/drop), privileged, security-opt | test | M5 | [ ] |
| E11 | Log driver + opsiyonları | test | M5 | [ ] |
| E12 | Preview paneli canlı `docker run ...` komutu ve API payload'ını gösterir | gözle | M5 | [ ] |
| E13 | Sihirbazla oluşturulan container gerçekten ayağa kalkar | manuel | M5 | [ ] |

## F. Image / Volume / Network / Registry

| # | Kriter | Doğrulama | Faz | ✔ |
|---|---|---|---|:--:|
| F1 | Image listesi: repo, tag, boyut, tarih, kullanan container sayısı | gözle | M5 | [ ] |
| F2 | Image pull ilerleme çubuğuyla akar; registry auth desteklenir | manuel | M5 | [ ] |
| F3 | Image remove/force, prune, tag, history, inspect çalışır | manuel | M5 | [ ] |
| F4 | Volume: liste, oluştur (driver+opts), remove, prune, kullanan container | manuel | M5 | [ ] |
| F5 | Network: liste, oluştur (bridge/macvlan/overlay, subnet/gateway), remove, prune, connect/disconnect, inspect | manuel | M5 | [ ] |
| F6 | Özel registry ekleme/silme; kimlik bilgileri şifreli saklanır ve pull'da kullanılır | test | M5 | [ ] |

## G. Dockerfile Build

| # | Kriter | Doğrulama | Faz | ✔ |
|---|---|---|---|:--:|
| G1 | Path browser yalnız whitelist içindeki dizinleri gezdirir | test | M6 | [ ] |
| G2 | Dockerfile adı seçilebilir (`Dockerfile`, `Dockerfile.prod`, …) | gözle | M6 | [ ] |
| G3 | Build args, target stage, image tag, `--no-cache`, platform, pull bayrakları uygulanır | manuel | M6 | [ ] |
| G4 | Build çıktısı canlı stream olur; layer ilerlemesi görünür; hata kırmızı vurgulanır | gözle | M6 | [ ] |
| G5 | Devam eden build iptal edilebilir ve `canceled` olarak kaydedilir | manuel | M6 | [ ] |
| G6 | Build geçmişi DB'de: kim, ne zaman, hangi dizin, sonuç, süre, log arşivi | test | M6 | [ ] |
| G7 | "Bu image'dan container oluştur" kısayolu sihirbaza ön-dolgulu geçer | gözle | M6 | [ ] |

## H. Compose Stack

| # | Kriter | Doğrulama | Faz | ✔ |
|---|---|---|---|:--:|
| H1 | Stack kaynağı: sunucudaki dosya yolu | manuel | M7 | [ ] |
| H2 | Stack kaynağı: UI'da Monaco editör (YAML syntax + şema doğrulama) | gözle | M7 | [ ] |
| H3 | Stack kaynağı: Git repo URL'inden clone/pull | manuel | M7 | [ ] |
| H4 | `up -d`, `down`, `restart`, `pull`, `stop`, `start` çalışır | manuel | M7 | [ ] |
| H5 | Servis ölçekleme (scale) çalışır | manuel | M7 | [ ] |
| H6 | Birleşik ve servis bazlı log akışı çalışır | manuel | M7 | [ ] |
| H7 | `.env` dosyası yönetimi ve değişken interpolasyonu doğru | test | M7 | [ ] |
| H8 | Stack servisleri tek ekranda durum tablosu olarak listelenir | gözle | M7 | [ ] |
| H9 | Kaydetmeden önce diff gösterilir | gözle | M7 | [ ] |
| H10 | ≥5 gerçek compose dosyası fixture'ı parse testinden geçer | test | M7 | [ ] |
| H11 | Desteklenmeyen compose alanları sessizce yutulmaz, kullanıcıya uyarı olarak gösterilir | gözle | M7 | [ ] |
| H12 | CLI fallback kullanılıyorsa `DECISIONS.md`'de yazılı ve ayarda görünür | gözle | M7 | [ ] |

## I. App Catalog

| # | Kriter | Doğrulama | Faz | ✔ |
|---|---|---|---|:--:|
| I1 | Template JSON şeması `docs/template-schema.md`'de belgelenmiş | gözle | M8 | [ ] |
| I2 | 20 template mevcut: redis, postgres, mysql, mariadb, mongodb, cloudflared, nginx, caddy, traefik, portainer_agent, uptime-kuma, n8n, vaultwarden, minio, rabbitmq, adminer, pgadmin, watchtower, gitea, wg-easy | test | M8 | [ ] |
| I3 | Her template için "geçerli payload üretir" testi var | test | M8 | [ ] |
| I4 | Alan tipleri desteklenir: text, number, password, select, bool, port, path, volume | test | M8 | [ ] |
| I5 | Alan doğrulama (required, regex, help metni) çalışır | test | M8 | [ ] |
| I6 | Parola alanlarında "rastgele üret" butonu çalışır | gözle | M8 | [ ] |
| I7 | `/etc/iskele/templates/` altındaki custom template'ler yüklenir | manuel | M8 | [ ] |
| I8 | Katalogdan deploy edilen container/stack gerçekten ayağa kalkar | manuel | M8 | [ ] |

## J. Dashboard, Sistem ve Audit

| # | Kriter | Doğrulama | Faz | ✔ |
|---|---|---|---|:--:|
| J1 | Dashboard container sayıları (running/stopped/unhealthy) doğru | gözle | M8 | [ ] |
| J2 | Image/volume/network sayıları ve `docker system df` disk kullanımı gösterilir | gözle | M8 | [ ] |
| J3 | Host CPU/RAM/disk (gopsutil), Docker Engine sürümü, uptime gösterilir | gözle | M8 | [ ] |
| J4 | `docker events` akışı UI'ya canlı bildirim (toast + activity feed) olarak düşer | manuel | M8 | [ ] |
| J5 | Prune araçları (dangling image, stopped container, unused volume/network) onay diyaloğuyla çalışır | manuel | M8 | [ ] |
| J6 | Audit log kim/ne zaman/hangi kaynak/hangi işlem bilgisini kaydeder | test | M2 | [x] |
| J7 | Audit log filtrelenebilir ve dışa aktarılabilir (CSV/JSON) | manuel | M8 | [ ] |
| J8 | Log ve event retention ayarları uygulanır (budama çalışır) | test | M8 | [ ] |

## K. UI / UX

| # | Kriter | Doğrulama | Faz | ✔ |
|---|---|---|---|:--:|
| K1 | Sidebar: Dashboard, Containers, Stacks, Images, Volumes, Networks, App Catalog, Builds, Audit, Settings | gözle | M3 | [ ] |
| K2 | Dark mode varsayılan; light/system toggle çalışır ve kalıcıdır | gözle | M3 | [ ] |
| K3 | Mobil uyum: sidebar collapse, tablolar kart görünümüne düşer | gözle | M4 | [ ] |
| K4 | 500+ satırda tablo sanallaştırması devreye girer | gözle | M4 | [ ] |
| K5 | Yıkıcı işlemler (remove, prune, down) kaynak adı yazdırılarak onaylanır | gözle | M4 | [ ] |
| K6 | Uzun işler için global task drawer: ilerleme, iptal, log | gözle | M5 | [ ] |
| K7 | Klavye kısayolları: `/` arama, `g c` containers, `g s` stacks | manuel | M4 | [ ] |
| K8 | Bağlantı kopunca "reconnecting" bandı çıkar; WS exponential backoff ile yeniden bağlanır | manuel | M4 | [ ] |
| K9 | Hata mesajları Docker'ın döndürdüğü metni gizlemeden gösterir | gözle | M4 | [ ] |
| K10 | Her liste ekranında empty state tasarımı var | gözle | M8 | [ ] |
| K11 | i18n: TR ve EN tam; hard-coded metin yok; anahtar eşitliği testi geçer | test | M3 | [ ] |
| K12 | Erişilebilirlik: ikon butonlarda `aria-label`, klavye ile tam gezinme | gözle | M8 | [ ] |

## L. Test ve Kalite

| # | Kriter | Doğrulama | Faz | ✔ |
|---|---|---|---|:--:|
| L1 | Docker katmanı interface arkasında; handler testleri fake kullanır | test | M1 | [x] |
| L2 | Auth testleri: üretim, doğrulama, expiry, revoke, RBAC matrisi, brute-force | test | M2 | [ ] |
| L3 | Path whitelist: traversal ve symlink saldırı vektörleri test edilir | test | M6 | [ ] |
| L4 | Compose parse: en az 5 gerçek fixture | test | M7 | [ ] |
| L5 | Template render: her template için geçerli payload testi | test | M8 | [ ] |
| L6 | Frontend Vitest: form validasyonu ve log viewer buffer testleri | test | M4 | [ ] |
| L7 | Backend test coverage ≥ %60 ve CI'da raporlanır | CI | M9 | [ ] |
| L8 | CI'da lint (golangci-lint + eslint + prettier) zorunlu ve yeşil | CI | M9 | [ ] |

## M. Dokümantasyon ve Teslim

| # | Kriter | Doğrulama | Faz | ✔ |
|---|---|---|---|:--:|
| M1 | `README.md`: özellikler, kurulum, güvenlik uyarısı, yapılandırma, ekran görüntüsü yer tutucuları | gözle | M9 | [ ] |
| M2 | README'de nginx, Caddy ve Traefik reverse proxy örnekleri (WS upgrade dahil) | gözle | M9 | [ ] |
| M3 | `SECURITY.md`: tehdit modeli, socket=root uyarısı, zafiyet bildirim süreci | gözle | M9 | [ ] |
| M4 | `CONTRIBUTING.md`: geliştirme kurulumu, commit kuralları, PR süreci | gözle | M9 | [ ] |
| M5 | `CHANGELOG.md`: v0.1.0 girdisi | gözle | M9 | [ ] |
| M6 | `LICENSE`: Apache-2.0 | gözle | M0 | [x] |
| M7 | `docs/openapi.yaml` tüm endpoint'lerle senkron ve doğrulanabilir (lint'ten geçer) | CI | M9 | [ ] |
| M8 | `docs/architecture.md`, `docs/template-schema.md`, `docs/configuration.md`, `docs/security-model.md` mevcut | gözle | M9 | [ ] |
| M9 | `PLAN.md`, `PROGRESS.md`, `DECISIONS.md` son duruma göre güncel | gözle | M9 | [ ] |
| M10 | `v0.1.0` tag'i atıldı ve release artifact'ları yayınlandı | CI | M9 | [ ] |
| M11 | Placeholder yok: `// TODO: implement`, mock endpoint, çalışmayan buton bulunmuyor | manuel | M9 | [ ] |

---

## Kapsam Dışı (gerekçeli)

| Madde | Gerekçe | Karar |
|---|---|---|
| Container içi dosya yöneticisi (Files sekmesi) | PROMPT §4.2'de "opsiyonel M9" | A-003 |
| Çoklu/uzak Docker endpoint UI'ı | v0.2 kapsamı; config düzeyinde TCP/TLS destekleniyor | A-004 |
| Image export/save | PROMPT §4.7'de opsiyonel | A-005 |

_(Bir madde buraya taşınırsa üstteki tablosunda `[~]` ile işaretlenir ve bu tabloya gerekçesi yazılır.)_
