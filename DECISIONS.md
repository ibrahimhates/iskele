# Iskele — Kararlar ve Varsayımlar (DECISIONS.md)

> `PROMPT.md` §0.1 gereği: belirsizlik olduğunda burada varsayım yazılır, en makul seçenek uygulanır
> ve durulmaz. Her karar kısa ADR formatındadır: **Bağlam → Karar → Gerekçe → Sonuç**.
>
> Durum: `Kabul` (uygulanacak/uygulandı) · `Öneri` (uygulama sırasında doğrulanacak) · `Değişti` (yerine yeni karar geçti)

---

## Başlangıç Kararları (planlama fazı)

### D-001 — Go modül yolu `github.com/ibrahimhates/iskele`
**Durum:** Kabul · **Faz:** M0
**Bağlam:** PROMPT'ta `github.com/<owner>/iskele` yazıyor, owner belirtilmemiş.
**Karar:** Repo remote'u `https://github.com/ibrahimhates/iskele` olduğundan modül yolu `github.com/ibrahimhates/iskele`.
**Sonuç:** Tüm import yolları bu prefix'i kullanır. Owner değişirse tek `go mod edit -module` + sed ile taşınır.

### D-002 — Servis adı `iskeled`, varsayılan port `8377`, veri dizini `/var/lib/iskele`
**Durum:** Kabul · **Faz:** M0
**Karar:** PROMPT §10'daki varsayılanlar birebir uygulanır: `listen: 127.0.0.1:8377`,
`docker_host: unix:///var/run/docker.sock`, `data_dir: /var/lib/iskele`, config `/etc/iskele/config.yaml`,
`allowed_paths: [/opt/stacks, /srv]`.
**Sonuç:** DB dosyası `/var/lib/iskele/iskele.db`, build logları `/var/lib/iskele/builds/`,
şifreleme anahtarı `/etc/iskele/secret.key` (0600).

### D-003 — Log kütüphanesi: stdlib `log/slog`
**Durum:** Kabul · **Faz:** M0
**Bağlam:** PROMPT "zerolog/slog" diyor, seçim serbest.
**Karar:** `log/slog` (Go 1.22 stdlib).
**Gerekçe:** Sıfır bağımlılık, tek binary hedefiyle uyumlu, yapılandırılmış log ihtiyacını karşılıyor.
**Sonuç:** `log_level` ayarı slog level'a eşlenir; `log_format: json|text` ek ayarı eklenir (varsayılan `text` tty'de, `json` servis altında).

### D-004 — Migration: elle yazılmış, `embed.FS` ile gömülü sıralı SQL
**Durum:** Kabul · **Faz:** M2
**Bağlam:** PROMPT "goose veya elle yazılmış embedded" diyor.
**Karar:** `internal/store/migrations/NNNN_ad.sql` dosyaları + ~80 satırlık runner.
**Gerekçe:** Bir bağımlılık daha az; goose'un cgo'suz sqlite ile sürücü kaydı ek karmaşıklık getiriyor.
**Sonuç:** `schema_migrations(version)` tablosu, tek transaction içinde sıralı uygulama, idempotent.

### D-005 — WebSocket kütüphanesi: `github.com/coder/websocket`
**Durum:** Kabul · **Faz:** M4
**Bağlam:** PROMPT kütüphane belirtmemiş. Adaylar: `gorilla/websocket`, `coder/websocket` (eski nhooyr).
**Karar:** `coder/websocket`.
**Gerekçe:** `context.Context` tabanlı API (graceful shutdown ve iptal ile doğrudan uyumlu), stdlib'e yakın, aktif bakım.
**Sonuç:** Exec için binary frame, kontrol mesajları için text/JSON frame kullanılır.

### D-006 — Compose: önce `compose-go` ile native uygulama, CLI fallback ayara bağlı
**Durum:** Kabul · **Faz:** M7 · **Risk:** R1
**Karar:** Birincil yol `compose-spec/compose-go/v2` ile parse → Docker API çağrıları. `docker compose`
binary'si **varsayılan olarak kullanılmaz**.
**Gerekçe:** PROMPT §2 "shell'e docker komutu çağırmak yasak" ve tek binary/bağımlılıksız hedefi.
**Sonuç:** Desteklenen compose alanları `docs/compose-support.md`'de matris olarak yayınlanır.
Desteklenmeyen bir alan sessizce yutulmaz; parse sırasında kullanıcıya uyarı listesi döner.
Native uygulama belirli bir alanda tıkanırsa, `compose.cli_fallback: true` ayarı ile (varsayılan `false`)
CLI'ya düşülür ve bu durum UI'da ve bu dosyada açıkça belirtilir.

### D-007 — Servis (business) katmanı `internal/service` olarak ayrılır
**Durum:** Kabul · **Faz:** M1
**Bağlam:** PROMPT §3 dosya ağacında ayrı `service/` yok ama §3 sonunda "handler → service → docker/store" kuralı var.
**Karar:** Kuralı sağlamak için `internal/service` paketi eklenir.
**Sonuç:** Ağaç PROMPT'takinin üst kümesi olur; handler'lar Docker SDK'yı hiç görmez.

### D-008 — WS/SSE yetkilendirme: kısa ömürlü "ticket"
**Durum:** Kabul · **Faz:** M4
**Bağlam:** Tarayıcı `WebSocket` API'si özel header gönderemez; SSE `EventSource` de gönderemez.
**Karar:** İstemci önce `POST /api/v1/auth/ws-ticket` ile 60 sn ömürlü, tek kullanımlık ticket alır;
WS/SSE bağlantısında `?ticket=` olarak gönderir. Ticket sunucu belleğinde tutulur, kullanılınca silinir.
**Ek:** `Origin` başlığı doğrulanır; ticket'ın rolü isteğin gerektirdiği rolü karşılamalıdır.
**Gerekçe:** Token'ı URL'de taşımak (log'a düşme riski) yerine tek kullanımlık ve kısa ömürlü belirteç.

### D-009 — Container "redeploy" davranışı
**Durum:** Kabul · **Faz:** M4
**Karar:** `inspect` çıktısından `Config` + `HostConfig` + `NetworkingConfig` türetilir → image `pull` →
eski container `stop` + `rename <ad>_old_<ts>` → yeni container aynı adla oluşturulur ve başlatılır →
başarılıysa eski container silinir, başarısızsa eski container geri adlandırılıp başlatılır (rollback).
**Sonuç:** Anonim volume'lar korunmaz; UI'da bu açıkça uyarı olarak gösterilir.

### D-010 — Stack keşfi ve sahiplik etiketleri
**Durum:** Kabul · **Faz:** M7
**Karar:** Iskele'nin yarattığı kaynaklar `com.iskele.stack=<ad>`, `com.iskele.service=<servis>`,
`com.iskele.managed=true` etiketleriyle işaretlenir. `com.docker.compose.project` etiketli mevcut
container'lar da "harici stack" olarak listelenir (salt okuma + up/down desteği).
**Sonuç:** Iskele dışında yaratılmış compose kurulumları da panelde görünür.

### D-011 — Frontend tipleri OpenAPI'den üretilir
**Durum:** Kabul · **Faz:** M3
**Karar:** `docs/openapi.yaml` tek doğruluk kaynağıdır; `make gen-api` ile `openapi-typescript`
kullanılarak `web/src/api/schema.d.ts` üretilir. Üretilen dosya repoya commit'lenir.
**Sonuç:** CI'da "üretilen tipler güncel mi" kontrolü yapılır (`git diff --exit-code`).

### D-012 — Sürüm ve dallanma
**Durum:** Kabul · **Faz:** M0
**Karar:** Geliştirme `claude/github-project-setup-guxyzz` dalında yapılır. Conventional Commits
(`feat|fix|chore|docs|test|refactor|build|ci`), her milestone sonunda push. Sürüm `v0.1.0`, SemVer.
**Sonuç:** `CHANGELOG.md` M9'da commit geçmişinden derlenir.

---

## M0 Sırasında Alınan Kararlar

### D-013 — Minimum Go sürümü 1.23 (PLAN'daki 1.22 yerine)
**Durum:** ~~Kabul~~ **Değişti** → D-019 (M1'de 1.25'e yükseltildi) · **Faz:** M0
**Bağlam:** PLAN §2'de "Go 1.22+" yazıyordu. `github.com/go-chi/chi/v5 v5.3.1` `go 1.23` gerektiriyor.
**Karar:** `go.mod` içinde `go 1.23`; CI matrisi `1.23` ve `1.24`. `toolchain` direktifi kaldırıldı ki
1.23 kurulu bir runner ek indirme yapmasın.
**Sonuç:** PLAN §2 ve README güncellendi. Bu, desteklenen dağıtımlarda sorun değil — binary statik
derlendiği için son kullanıcıda Go gerekmiyor.

### D-014 — Ortak HTTP sözlüğü için `internal/httpx` paketi
**Durum:** Kabul · **Faz:** M0
**Bağlam:** PLAN'da `errors.go` ve `response.go` `internal/server` altındaydı. Ancak `server` paketi
`handlers`'ı import ediyor; handler'lar da hata gövdesini yazmak için aynı yardımcılara ihtiyaç duyuyor
→ import döngüsü.
**Karar:** Standart hata zarfı, hata kodları, `WriteJSON`/`WriteError` ve hata dönebilen `Handler` tipi
`internal/httpx` paketine taşındı. Hem `server` hem `handlers` buradan import eder.
**Sonuç:** Katman kuralı korunuyor; sonraki milestone'larda tüm handler'lar `httpx.Handler` imzasını
kullanacak (`func(w, r) error`), böylece hata yazma boilerplate'i tekrarlanmıyor.

### D-015 — Test kütüphanesi: stdlib `testing`, testify yok
**Durum:** Kabul · **Faz:** M0
**Bağlam:** PLAN §2'de `testify/require` yazıyordu.
**Karar:** Yalnız stdlib `testing` + tablo testleri kullanılıyor; testify bağımlılığı `go mod tidy` ile düştü.
**Gerekçe:** M0'da tüm testler assertion helper'ı olmadan okunaklı yazılabildi; bağımlılık yüzeyini
küçük tutmak tek binary hedefiyle uyumlu.
**Sonuç:** İleride gerçekten gerekirse eklenir; şimdilik `go.mod` yalnız chi ve yaml.v3 içeriyor.

### D-016 — `listen` portu 0 kabul edilmez
**Durum:** Kabul · **Faz:** M0
**Bağlam:** Port 0 "boş port ata" demek; testlerde kullanışlı ama üretimde kullanıcı servisin hangi
porta bağlandığını bulamaz.
**Karar:** Config doğrulaması portu 1-65535 aralığına zorlar. Testler `server.New`'i doğrudan çağırarak
0 portunu kullanmaya devam edebilir (doğrulamadan geçmez).

### D-017 — Log formatı `auto` ve `log_format` ayarı
**Durum:** Kabul · **Faz:** M0
**Karar:** D-003'te öngörüldüğü gibi `log_format` ayarı eklendi: `auto` (varsayılan), `text`, `json`.
`auto`, çıktı bir terminal ise text, değilse JSON seçer — systemd altında otomatik yapılandırılmış log.

### D-018 — İstemci IP'si için proxy başlıkları yok sayılır
**Durum:** Kabul · **Faz:** M0
**Bağlam:** Rate limit ve brute-force koruması (M2) IP'ye göre çalışacak.
**Karar:** `middleware.ClientIP` yalnız `RemoteAddr` kullanır; `X-Forwarded-For` / `X-Real-IP`
**yok sayılır**, çünkü saldırgan kontrolündedir ve limitleri atlatmak için kullanılabilir.
**Sonuç:** Reverse proxy arkasında doğru istemci IP'si isteniyorsa, ileride yalnız güvenilen proxy
adresleri için açılan açık bir ayar eklenecek (v0.2). README'de belirtilecek.

---

## M1 Sırasında Alınan Kararlar

### D-019 — Minimum Go 1.25 (D-013'ü değiştirir)
**Durum:** Kabul · **Faz:** M1 · **Değiştirdiği karar:** D-013 (Go 1.23)
**Bağlam:** `github.com/docker/docker v28.5.2` bağımlılık ağacı `go.opentelemetry.io/otel v1.45`,
`otelhttp v0.70` ve `golang.org/x/sys v0.47` çekiyor; bunların hepsi `go >= 1.25` istiyor.
**Değerlendirilen alternatif:** Bu paketleri eski sürümlere pinlemek (otel v1.37, x/sys v0.4x).
Denendi ve çalıştı, ancak her `go mod tidy` sonrası tekrar kırılıyor ve bilerek eski —
dolayısıyla potansiyel olarak zafiyetli — bağımlılıklarla yaşamak demek. CI'da `govulncheck`
çalıştıran, Docker soketine erişen bir projede bu kabul edilemez.
**Karar:** `go.mod` içinde `go 1.25.0`; CI matrisi `1.25` ve `stable`.
**Sonuç:** PLAN §2, README ve CI güncellendi. Son kullanıcıyı etkilemez (statik binary).

### D-020 — `docker/docker` modülü `v28.5.2+incompatible` olarak sabitlendi
**Durum:** Kabul · **Faz:** M1
**Bağlam:** `github.com/docker/docker/client@latest` artık `github.com/moby/moby/client` olarak
yayınlanıyor; eski yol ile `go get` hata veriyor.
**Karar:** `go get github.com/docker/docker@v28.5.2+incompatible` ile ana modül çekiliyor,
import yolu `github.com/docker/docker/client` olarak kalıyor.
**Sonuç:** Moby'nin yeni modül yoluna geçiş ayrı bir iş olarak v0.2'ye bırakıldı.

### D-021 — Docker erişilemezken servis ayakta kalır (`Offline` istemci)
**Durum:** Kabul · **Faz:** M1 · **Kabul kriteri:** A12
**Bağlam:** Daemon çökmüşse, operatörün bunu öğrenmesi gereken yer tam da paneldir.
**Karar:** `docker.Offline(reason)` — her çağrısı `KindUnavailable` dönen bir `Client`
implementasyonu. Başlangıçta bağlantı kurulamazsa router bunu kullanır; `/health` ve `/version`
çalışmaya devam eder, Docker'a dayanan her route `503 DOCKER_UNAVAILABLE` döner.
**Sonuç:** Hata mesajı endpoint'i ve en olası çözümü (`docker` grubu üyeliği) içerir.

### D-022 — Engine hata metni olduğu gibi geçirilir
**Durum:** Kabul · **Faz:** M1 · **Gereksinim:** PROMPT §7
**Karar:** `docker.Error.Message` Docker'ın kendi metnini taşır ve yanıt gövdesine birebir yazılır.
Docker dışı hatalar (programlama hataları) ise opak `500 INTERNAL` olur — yalnız log'a düşer.
**Gerekçe:** Daemon güvenilen yerel bir bileşen; "container is running: stop the container before
removing or force remove" gibi mesajlar operatöre ne yapacağını söyleyen tek şey.

### D-023 — Bilinmeyen değerler için `-1` sentinel'i
**Durum:** Kabul · **Faz:** M1
**Bağlam:** `size_rw`, `size_root_fs`, volume `size`/`ref_count` engine tarafından yalnız istendiğinde
hesaplanır; hesaplanmadığında 0 dönmek "boş" ile "bilinmiyor"u karıştırır.
**Karar:** Hesaplanmamış sayısal alanlar `-1` döner. `docs/openapi.yaml`'da belgelenmiştir.

### D-024 — Liste endpoint'leri `{items, total}` zarfı kullanır
**Durum:** Kabul · **Faz:** M1
**Karar:** `GET /containers|images|volumes|networks` çıplak dizi yerine
`{"items":[...],"total":N}` döner; `items` hiçbir zaman `null` olmaz.
**Gerekçe:** İleride sayfalama metadata'sı eklemek için yer bırakır ve istemcide null kontrolü gerektirmez.

### D-025 — `/system/ping` M1'de eklendi (planda M8'di) ve daima 200 döner
**Durum:** Kabul · **Faz:** M1
**Karar:** `GET /system/ping` → `{"reachable":bool,"api_version":"...","error":"..."}`.
Daemon erişilemez olsa bile HTTP 200 döner.
**Gerekçe:** UI'nın bağlantı bandı (K8) bunu yoklayacak; erişilemez daemon bir *cevap*, başarısız
bir istek değil. `/system/info` ve `/system/df` ise normal 503 semantiğini korur.

### D-026 — Coverage `-coverpkg=./...` ile ölçülür
**Durum:** Kabul · **Faz:** M1
**Bağlam:** Handler'lar router testlerinden (farklı paket) geçiyor; varsayılan ölçüm bunu %3 gösteriyordu.
**Karar:** `make test-cover` ve CI `-coverpkg=./...` kullanır → paketler arası atıf doğru olur.

---

## M2 Sırasında Alınan Kararlar

### D-027 — Rol/izin matrisi: roller doğrudan değil, **izinler** üzerinden kontrol edilir
**Durum:** Kabul · **Faz:** M2 · **Kabul kriteri:** B7-B9
**Bağlam:** Route'larda `if role == "admin"` kontrolü, endpoint sayısı arttıkça dağınıklaşır ve
bir yerde unutulursa sessizce açık kalır.
**Karar:** 8 izin tanımlandı (`read, operate, create, delete, build, prune, privileged, admin`);
roller bu izinlerin kümesidir. Route'lar izin ister (`r.With(operate())`), rol değil.
**Sonuç:** Matris tek yerde (`middleware/rbac.go`) ve testte satır satır doğrulanıyor.
Bilinmeyen/bozuk bir rol **hiçbir izne sahip değildir** (fail-closed). `/auth/me` çağıranın izin
listesini döner; UI kullanamayacağı kontrolleri buna göre gizleyecek.

### D-028 — CSRF: çift-gönderim cookie yerine "yalnız Bearer" kabulü
**Durum:** Kabul · **Faz:** M2 · **Gereksinim:** PROMPT §6.6 (iki seçenekten biri)
**Bağlam:** Iskele'nin tarayıcı istemcisi access token'ı bellekte tutar ve `Authorization`
başlığıyla gönderir. Siteler arası bir form veya `<img>` bu başlığı **ayarlayamaz**.
**Karar:** Durum değiştiren her istek (a) `Bearer` token taşımak zorunda ve (b) `Origin` başlığı
varsa aynı origin olmalı. Cookie tabanlı oturum yok, dolayısıyla çift-gönderim token'ına gerek yok.
**Sonuç:** `Origin` göndermeyen istemciler (curl, CI) çalışır — CSRF onlar için geçerli bir tehdit
değil — ama yine de token sunmak zorundalar. `OriginAllowed` M4'te WebSocket handshake'inde tekrar
kullanılacak.

### D-029 — Brute-force limiti IP bazlı, kullanıcı adı bazlı değil
**Durum:** Kabul · **Faz:** M2 · **Kabul kriteri:** B11
**Bağlam:** Kullanıcı adına göre kilitlemek, bilinen bir hesabı kasten yanlış parolayla deneyerek
**kilitleme saldırısına** (account lockout DoS) açık hale getirir.
**Karar:** Sayaç ve kilit kaynak IP'ye göre; 15 dk pencerede 10 başarısızlık → 15 dk kilit.
**Başarılı giriş sayacı sıfırlar**, böylece iki kez yanlış yazıp sonra giren kullanıcı kilide
bir adım uzakta kalmaz. Kayıtlar DB'de tutulur (yeniden başlatma kilidi sıfırlamaz).
**Sonuç:** Ayrıca in-memory token bucket rate limit var: login/bootstrap 5/dk, genel API 120/dk.

### D-030 — `/auth/refresh` ve `/auth/status` sıkı login limitine tabi değil
**Durum:** Kabul · **Faz:** M2
**Bağlam:** Uçtan uca testte, birkaç sekmeli bir tarayıcının normal token yenilemesinde 429 alacağı
görüldü (login limiti 5/dk).
**Karar:** Bu iki endpoint genel limite (120/dk) alındı. Gerekçe: refresh token 32 byte rastgeledir,
kaba kuvvetle bulunamaz — tehdit modeli parola tahmini değil. `status` yalnız kurulumun yapılıp
yapılmadığını söyler.
**Sonuç:** `bootstrap` ve `login` sıkı limitte kalır.

### D-031 — Parola politikası: uzunluk önce, karakter sınıfı hafif
**Durum:** Kabul · **Faz:** M2 · **Kabul kriteri:** B3
**Karar:** Minimum 12 karakter (PROMPT §4.1) + en az 2 karakter sınıfı. Maksimum 1024 karakter.
**Gerekçe:** Uzun bir parola cümlesi, kısa ve karmaşık bir dizeden güçlüdür; sınıf kuralı yalnız
`aaaaaaaaaaaa` gibi aşikâr girdileri eler. Üst sınır, tek bir login denemesinin bellek-yoğun
argon2 hesabıyla DoS'a dönüşmesini engeller.
**Ek:** argon2id `t=3, m=64MiB, p=2`; parametreler PHC formatında hash ile birlikte saklanır,
`NeedsRehash` ile bir sonraki başarılı girişte otomatik yükseltilir.

### D-032 — Kullanıcı adı büyük/küçük harf duyarsız, görünen biçim korunur
**Durum:** Kabul · **Faz:** M2
**Karar:** `users.username_lower` sütunu UNIQUE; arama bunun üzerinden. `username` operatörün
yazdığı biçimi saklar.
**Sonuç:** "Admin" ve "admin" aynı hesap; iki ayrı hesap açılamaz.

### D-033 — Kimlik doğrulama hataları ayırt edilemez
**Durum:** Kabul · **Faz:** M2
**Karar:** "Kullanıcı yok" ve "parola yanlış" aynı `INVALID_CREDENTIALS` mesajını döner.
Kullanıcı bulunamadığında bile sabit bir sahte hash'e karşı doğrulama yapılır, böylece
**süre farkı** da hesap varlığını ele vermez.
**Sonuç:** Login formu hesap numaralandırma aracına dönüşmez.

### D-034 — Refresh token rotasyonu; iptal önce, üretim sonra
**Durum:** Kabul · **Faz:** M2 · **Kabul kriteri:** B5
**Karar:** Her `refresh` çağrısında eski oturum **önce** iptal edilir, sonra yeni çift üretilir.
**Gerekçe:** Üretim başarısız olursa eski token zaten ölmüştür — güvenli yönde hata.
Çalınan bir refresh token, gerçek kullanıcı bir kez yenilediği anda çalışmaz olur.
**Sonuç:** Token'lar DB'de yalnız SHA-256 hash'i olarak durur; veritabanı sızıntısı canlı oturum vermez.

### D-035 — Token iptali ve hesap durumu her istekte kontrol edilir
**Durum:** Kabul · **Faz:** M2 · **Kabul kriteri:** B14
**Bağlam:** JWT kendi başına geçmişe dair bir iddiadır; hesap o sırada devre dışı bırakılmış olabilir.
**Karar:** Her istekte kullanıcı DB'den okunur; `disabled` ise `403 ACCOUNT_DISABLED`, silinmişse `401`.
**Sonuç:** Devre dışı bırakma anında etkili olur, token süresinin dolmasını beklemez. Maliyeti tek
indeksli SELECT — tek sunucu ölçeğinde ihmal edilebilir.

### D-036 — Audit maskelemesi: önce maskele, sonra sunucu alanlarını ekle
**Durum:** Kabul · **Faz:** M2 · **Kabul kriteri:** C9
**Bağlam:** Maskeleme regex'i `token` içeren anahtarları gizliyor; bu, izleme için gereken
`api_token_id` alanını da (bir sır olmadığı halde) gizliyordu — test bunu yakaladı.
**Karar:** Kullanıcıdan gelen `detail` maskelenir, **sonra** sunucunun ürettiği güvenilir alanlar
(`api_token_id`) üstüne yazılır.
**Sonuç:** Sızmış bir API token'ının hangi işlemleri yaptığı izlenebilir kalır.

### D-037 — Master anahtar izinleri her açılışta doğrulanır
**Durum:** Kabul · **Faz:** M2 · **Kabul kriteri:** C8
**Karar:** `/etc/iskele/secret.key` yoksa 0600 ile üretilir; **varsa izinleri kontrol edilir** ve
grup/diğer okuyabiliyorsa servis başlamaz (`chmod 600 ...` ipucuyla).
**Gerekçe:** Başka bir yerel hesabın okuyabildiği anahtar hiçbir şeyi korumuyordur; bu bir uyarı
değil, başlatma hatasıdır. Anahtar `O_EXCL` ile yaratılır — eşzamanlı bir başlatma anahtarı ezmez.
**Ek:** Amaç bazlı alt anahtarlar (`Derive("jwt-signing")`, `Derive("secretbox")`) — birinin ele
geçmesi diğerini vermez.

### D-038 — SQLite: tek yazar bağlantısı, WAL, RFC3339Nano zaman damgaları
**Durum:** Kabul · **Faz:** M2
**Karar:** `SetMaxOpenConns(1)` ile yazımlar seri hale getirilir (SQLITE_BUSY tamamen elenir),
`journal_mode=WAL`, `synchronous=NORMAL`, `foreign_keys=ON`. Zaman damgaları RFC3339Nano UTC
**metin** olarak saklanır; sözlüksel sıralama = kronolojik sıralama, indeksler beklendiği gibi çalışır.
**Sonuç:** Tek sunucu ölçeğinde okuma performansı yeterli; basitlik kazanıyor.

---

## M3 + M4 Sırasında Alınan Kararlar

### D-039 — shadcn/ui yerine kendi bileşenleri
**Durum:** Kabul · **Faz:** M3
**Karar:** PLAN'da shadcn/ui yazıyordu; onun yerine Tailwind üstünde ~10 küçük bileşen elle yazıldı
(`Spinner`, `EmptyState`, `ErrorPanel`, `ConfirmDialog`, `PageHeader`, `StatCard`, `StateBadge`, `JsonViewer`).
**Gerekçe:** shadcn bir kütüphane değil, bir kopyala-yapıştır jeneratörüdür: Radix + CVA + tailwind-merge
bağımlılıklarını ve onlarca dosyayı projeye kalıcı olarak sokar. İhtiyacımız olan yüzey bunun onda biri.
Tema, CSS özel değişkenleriyle (`--bg`, `--fg`, `--accent`…) tanımlanıyor; koyu/açık tek sınıf değişimiyle geçiyor.
**Sonuç:** Frontend bağımlılık ağacı küçük kaldı, `vendor` chunk'ı 54 kB gz.

### D-040 — Elle yazılan TS tipleri + üretilen şema, derleme zamanında karşılaştırılıyor
**Durum:** Kabul · **Faz:** M3
**Karar:** `src/api/types.ts` elle yazılıyor (uygulamanın okuduğu tipler), `src/api/schema.d.ts`
`make gen-api` ile OpenAPI'den üretiliyor, `src/api/conformance.ts` ise ikisi arasındaki uyumu
tip düzeyinde iddia ediyor. CI ayrıca `gen:api` çıktısının commit'lenmiş dosyayla aynı olmasını şart koşuyor.
**Gerekçe:** Üretilen tipler doğrudan kullanılırsa arayüz kodu jeneratörün şekline (`components['schemas'][...]`)
bağlanır ve okunmaz hale gelir; sadece elle yazılırsa spec ile sessizce ayrışır. Bu kurulum ikisini de engelliyor:
ayrışma `npm run build`'i kırıyor.
**Sonuç:** İlk çalıştırmada iki gerçek hata çıktı — OpenAPI'de yinelenen `"409"` anahtarları (YAML'i geçersiz kılıyordu)
ve `health` alanının spec'te enum, TS'te `string` olması.

### D-041 — Yapılmamış bölümler için menü öğesi ve "yakında" sayfası yok
**Durum:** Kabul · **Faz:** M3
**Karar:** Stacks/Catalog/Builds/Audit menü öğeleri ve `ComingSoonPage` bileşeni kaldırıldı.
Bu bölümler kendi milestone'larında (M6–M8) menüye geri eklenecek.
**Gerekçe:** PROMPT §0: "işlevsiz UI butonu yok." Hiçbir şey yapmayan bir menü öğesi, ne kadar dürüstçe
"M7'de gelecek" dese de, tıklanabilir ve hiçbir şey yapmıyor.
**Sonuç:** Sidebar 6 öğe; hepsi çalışıyor.

### D-042 — Go paket listesi `web/node_modules`'ü dışlıyor
**Durum:** Kabul · **Faz:** M3
**Karar:** Makefile `PKGS = go list ./... | grep -v /node_modules/` tanımlıyor; `test`, `test-cover`,
`vet` ve `vuln` hedefleri joker yerine bu listeyi alıyor. `.golangci.yml` de aynı yolu dışlıyor.
**Gerekçe:** npm paketleri bazen kendi Go kaynaklarını taşıyor — `flatted` bunu yapıyor ve
`web/node_modules/flatted/golang/pkg/flatted` `./...`'a giriyordu. Frontend'in bir bağımlılığının
bizim derlememize, vet çıktımıza ve kapsam sayımıza karışması kabul edilemez.
**Sonuç:** `go list` çıktısı 15 paket + `web`; üçüncü parti kaynak yok.

### D-043 — Testler cgo ile, ürün binary'si cgo'suz derleniyor
**Durum:** Kabul · **Faz:** M3
**Karar:** Makefile ikiye ayrıldı: `GO := CGO_ENABLED=0 go` (derleme, cross-compile) ve
`GOTEST := CGO_ENABLED=1 go` (yalnız test hedefleri). CI'daki global `CGO_ENABLED=0` ortam değişkeni
kaldırıldı, cross-compile adımına taşındı.
**Gerekçe:** Yarış dedektörü C ile yazılmıştır; `CGO_ENABLED=0 go test -race` doğrudan
"`-race` requires cgo" ile ölüyordu. Yani `make test` ve `make test-cover` **hiç koşmuyordu** —
yeşil sanılan bir hedefti. Ürün binary'si statik kalmak zorunda olduğu için ayrım şart.
**Sonuç:** `make test` 15 paketi `-race` ile koşuyor; `make build` hâlâ CGO'suz statik binary üretiyor.

### D-044 — SPA fallback yalnız `/api` dışındaki yollara, varlıklara değil
**Durum:** Kabul · **Faz:** M3
**Karar:** `internal/server/spa.go`: `/api` ile başlayan yollar JSON 404 alır; diğerleri için dosya varsa
dosya, yoksa `index.html` döner. **İstisna:** `/assets/` altında bulunamayan bir dosya 404 alır,
`index.html` almaz. Hash'li varlıklar `immutable` bir yıl, `index.html` `no-cache`.
**Gerekçe:** Eski bir kabuk artık var olmayan bir bundle'ı isterse, ona HTML dönmek tarayıcıda
"JavaScript'te sözdizimi hatası" olarak görünür ve hatayı tamamen yanlış yere işaret eder.
`index.html` hash'li dosya adlarını taşıdığı için kendisi asla cache'lenemez.
**Sonuç:** Binary tek başına UI'ı sunuyor; `curl /containers/abc` kabuğu, `curl /api/v1/nope` JSON'u döndürüyor.

### D-045 — Frontend'siz `go build` çalışmaya devam ediyor
**Durum:** Kabul · **Faz:** M3
**Karar:** `web/dist/.gitkeep` commit'leniyor ve `//go:embed all:dist` boş bir ağacı da kabul ediyor.
`web.Bundled()` `index.html`'in varlığına bakıyor; yoksa sunucu, API'nin çalıştığını ve `make build`
gerektiğini söyleyen bir sayfa döndürüyor.
**Gerekçe:** `go build ./cmd/iskeled`, Node kurulu olmayan bir makinede de çalışmalı; aksi halde
backend üzerinde çalışan biri frontend araç zincirini kurmak zorunda kalır. Alternatif — embed'i
build tag'i arkasına almak — iki farklı derleme yolu demekti.
**Sonuç:** CI'nın `go` job'ı Node'suz `make build-go` koşuyor, `bundle` job'ı ikisini birden.

### D-046 — Ticket, izin kontrolü başarısız olsa bile tüketilir
**Durum:** Kabul · **Faz:** M4
**Karar:** `redeemTicket` önce ticket'ı siler, sonra izni kontrol eder.
**Gerekçe:** Ticket tanımı gereği tek kullanımlıktır. Reddedilen bir ticket'ı hayatta bırakmak,
onu başka bir endpoint'e karşı tekrar denemeye izin verir.
**Sonuç:** Yanlış izinle gelen bir istek 403 alır ve ticket'ı da kaybeder.

### D-047 — Redeploy: silmeden önce park et, hata olursa geri al
**Durum:** Kabul · **Faz:** M4
**Karar:** Eski container `_old_<ts>` adına yeniden adlandırılır, yenisi oluşturulup başlatılır,
ancak o zaman eskisi silinir. Herhangi bir adımda hata olursa eski container kendi adına döndürülüp
başlatılır ve sonuç `rolled_back: true` ile raporlanır.
**Gerekçe:** Önce silip sonra oluşturmak, oluşturma başarısız olduğunda operatörü container'sız bırakır.
**Sonuç:** Atomik değil (ikisinin de servis vermediği bir pencere var) ve bu OpenAPI'de açıkça yazıyor.
Yalnızca engine'in inspect çıktısında ifade edilebilen ayarlar taşınır.

### D-048 — Liste görünümündeki CPU/RAM tek bir çoğullanmış akıştan geliyor
**Durum:** Kabul · **Faz:** M4
**Karar:** `GET /containers/stats` (SSE) çalışan tüm container'ları tek bağlantıda yayınlıyor;
her örnek ait olduğu container'ın ID'siyle etiketleniyor. Sunucu tarafında `statsMux` her container
için bir engine akışı tutuyor ve 10 saniyede bir listeyi yeniden tarayarak yeni başlayanları ekliyor,
duranları düşürüyor.
**Gerekçe:** Satır başına bir SSE bağlantısı, tarayıcının origin başına ~6 bağlantı sınırına
altıncı container'da toslar. Engine olaylarını dinlemek yerine periyodik tarama seçildi: tarama
tek ucuz bir çağrı, kaçan bir olay ise satırı kalıcı olarak boş bırakır.
**Sonuç:** Liste 500 container'da da tek bağlantı kullanıyor. Bir container'ın akışı hata verirse
yalnızca o satır boş kalıyor; akışın tamamı yalnızca daemon erişilemezse kapanıyor.

### D-049 — Restart sayısı listede değil, yalnızca detayda
**Durum:** Kabul (kapsam daraltma) · **Faz:** M4
**Karar:** ACCEPTANCE D1 listede "restart sayısı" istiyor; gösterilmiyor. Detay sayfasının
Overview sekmesinde var.
**Gerekçe:** Engine'in liste API'si (`docker ps` eşleniği) `RestartCount` döndürmüyor; yalnızca
`inspect` döndürüyor. Listede göstermek, her sayfa yüklemesinde container sayısı kadar `inspect`
çağrısı demek — 500 container'lı bir hostta 500 çağrı. Her zaman `—` yazan bir sütun ise
hiç olmamasından kötü.
**Sonuç:** ACCEPTANCE D1 bu gerekçeyle kısmi (🟡) işaretlendi; M8'de dashboard'a "en çok yeniden
başlayan container'lar" olarak, tek seferlik ve isteğe bağlı bir sorguyla gelebilir.

---

## M5 Sırasında Alınan Kararlar

### D-050 — İki ayrı container tanımı: `CreateSpec` (motor) ve `ContainerSpec` (operatör)
**Durum:** Kabul · **Faz:** M5
**Karar:** `docker.CreateSpec` SDK yapılarını taşımaya devam ediyor (redeploy bir container'ı byte byte
yeniden üretebilsin diye). Sihirbazın gönderdiği ise `docker.ContainerSpec`: düz, JSON dostu, operatörün
tanıdığı terimlerle. Çeviri `BuildCreateSpec` içinde, yani `internal/docker` sınırının içinde.
**Gerekçe:** Servis katmanı doğrulamayı (whitelist, privileged) SDK tiplerine bakarak yapamaz — o zaman
SDK dışarı sızardı. Tek bir tip kullanmak ise ya redeploy'u bozar ya da formu SDK'nın şekline bağlardı.
**Sonuç:** SDK hâlâ tek pakette. `BuildCreateSpec` alan alan doğruluyor, bu yüzden bozuk bir port
"400 Bad Request" yerine "ports: container port 70000 is outside 1-65535" olarak dönüyor.

### D-051 — Bind mount doğrulaması: symlink çöz, bileşen karşılaştır, boşsa reddet
**Durum:** Kabul · **Faz:** M5
**Karar:** `PathGuard.Check` yolu temizliyor, `EvalSymlinks` ile çözüyor, sonra `filepath.Rel` ile
bileşen bazlı karşılaştırıyor. Henüz var olmayan bir yol (engine oluşturacaksa) kabul ediliyor.
`allowed_paths` boşsa **her** bind mount reddediliyor.
**Gerekçe:** Bind mount, container'dan host root'una en kısa yol. Üç saldırı da gerçek: `..` ile çıkış
(Clean çözer), `/srv-other` gibi önek çakışması (string karşılaştırma yakalamaz), ve izinli kök içine
konmuş bir symlink (yalnız çözerek yakalanır). Boş liste "her şey serbest" demek olsaydı, bir
yapılandırma hatası hostu verirdi.
**Sonuç:** `paths_test.go` üç saldırıyı da pinliyor. Named volume ve tmpfs host yoluna dokunmadığı için
kontrol dışında — aksi halde sihirbaz kullanılamaz olurdu.

### D-052 — Privileged seçenekler tek bir kapı arkasında, hata hangisini söylüyor
**Durum:** Kabul · **Faz:** M5
**Karar:** `privileged`, `cap_add`, `devices`, `security_opt`, `sysctls` ve `network=host`
`privileged` iznini gerektiriyor. `cap_drop` gerektirmiyor. Reddedilen istek 403 ile birlikte
`details.options` içinde takılan seçeneklerin tamamını döndürüyor.
**Gerekçe:** Her biri bir yapılandırmada container'dan host root'una çıkış yolu. `cap_drop` ise tam
tersi — container'ı daraltıyor, kapıya koymak yalnızca güvenli yapılandırmayı zorlaştırırdı.
Hangi seçeneğin takıldığını söylememek operatörü tek tek denemeye zorlar.
**Sonuç:** Sihirbaz aynı listeyi istemci tarafında da hesaplıyor, böylece uyarı gönderim öncesi çıkıyor;
otorite yine sunucuda.

### D-053 — Registry parolaları şifreli saklanıyor, hiçbir yanıtta dönmüyor
**Durum:** Kabul · **Faz:** M5
**Karar:** Parola `SecretBox` (AES-256-GCM, master anahtardan türetilmiş) ile şifrelenip saklanıyor.
`store.Registry.Password` `json:"-"` etiketli; API yalnız `has_password` söylüyor. Güncellemede boş
parola "saklı olanı koru" anlamına geliyor. Audit kaydına parola hiç yazılmıyor.
**Gerekçe:** Anahtar dosyası olmadan sızan bir veritabanı özel registry'ye erişim vermemeli.
UI parolayı hiç görmediği için geri gönderemez; boş alanı "sil" saymak her düzenlemede kimliği silerdi.
**Sonuç:** İki test bunu pinliyor: biri API yanıtlarının parolayı taşımadığını, diğeri veritabanı
satırının düz metin içermediğini doğruluyor.

### D-054 — Görevler bellekte, veritabanında değil
**Durum:** Kabul · **Faz:** M5
**Karar:** `TaskRegistry` bellek içi. Biten görevler 10 dakika, en fazla 200 görev saklanıyor.
**Gerekçe:** Bir görev, onu çalıştıran daemon yaşadığı sürece anlamlı. iskeled yeniden başladığında
her pull zaten iptal oluyor; kalıcı kayıt yalnızca asla bitemeyecek satırlar üretirdi.
**Sonuç:** M6'da build'ler aynı kayda girecek. Kalıcı bir geçmiş gerekirse audit log zaten var.

### D-055 — Pull ilerlemesi sunucuda toplanıyor
**Durum:** Kabul · **Faz:** M5
**Karar:** Engine katman katman rapor veriyor ve hiçbir zaman toplam vermiyor. Sunucu her katmanın son
figürünü tutup topluyor ve tek bir yüzde yayınlıyor; hiçbir katman boyut bildirmemişken `-1`.
**Gerekçe:** Tek bir ilerleme çubuğu ancak böyle var olabilir. Boyut bildirmeyen katmanı sıfır saymak
çubuğu geri götürürdü; toplamak yerine biriktirmek ilk katmanda 100'ü aşardı.
**Sonuç:** `pullprogress_test.go` bu üç durumu da pinliyor.

### D-056 — Pull akışı `done` demeden önce iki kanalı da boşaltıyor
**Durum:** Kabul (hata düzeltmesi) · **Faz:** M5
**Karar:** SSE döngüsü `events` kapandığında hemen başarı ilan etmiyor; `events` ve `errs` **ikisi de**
kapanana kadar sürüyor.
**Gerekçe:** Engine başarısız bir pull'u 200 yanıtın *içinde* raporluyor, yani hata son ilerleme
satırıyla aynı anda geliyor. `select` kapanan `events`'i önce seçtiğinde başarısız bir pull "done"
olarak bildiriliyordu. Test yazılırken yakalandı.
**Sonuç:** Katman düzeyindeki hata olayı da ayrıca kontrol ediliyor, böylece hata iki yoldan da yakalanıyor.

### D-057 — Sıfır zaman damgası yayınlanmıyor
**Durum:** Kabul (hata düzeltmesi) · **Faz:** M5
**Karar:** `store.Registry` kendi `MarshalJSON`'ını uyguluyor ve hiç kullanılmamış `last_used_at`
alanını çıkarıyor. `Create` ve `Update` yazdıkları zaman damgalarını çağıranın kopyasına basıyor.
**Gerekçe:** `omitempty` bir struct'a uygulanmıyor, bu yüzden sıfır `time.Time` `"0001-01-01T00:00:00Z"`
olarak gidiyordu — arayüz bunu "2000 yıl önce" diye gösterir. `Create` değer alıyordu, dolayısıyla
oluşturma yanıtı da sıfır zaman taşıyordu. İkisi de uçtan uca doğrulamada görüldü.
**Sonuç:** `APIToken.LastUsedAt` aynı desende ama bugün hiçbir yanıtta görünmüyor; M8'de token listesi
gelince aynı düzeltme oraya da gerekecek.

### D-058 — Wizard önizlemesi gönderilen nesneden üretiliyor
**Durum:** Kabul · **Faz:** M5
**Karar:** Hem `docker run` komutu hem API payload'ı, POST edilen `ContainerSpec`'ten render ediliyor.
Komut, kabuğun dokunacağı argümanları POSIX tek tırnak (`'\''` kaçışıyla) ile alıntılıyor.
**Gerekçe:** Önizleme ayrı bir açıklama olsaydı formdan sapabilir ve operatöre yalan söyleyebilirdi.
Aynı nesneden üretilince sapması imkânsız. Alıntılama doğru olmazsa terminale yapıştırılan komut
gösterilenden başka bir container yaratır.
**Sonuç:** `preview.test.ts` 14 vaka ile pinliyor; boşluk, kesme işareti ve `?` içeren değerler dahil.

---

## M6 Sırasında Alınan Kararlar

### D-059 — Ayrı bir `internal/paths` paketi yazılmadı
**Durum:** Kabul · **Faz:** M6
**Bağlam:** PROGRESS M6 için `internal/paths/whitelist.go` öngörüyordu; ama M5'te bind mount kaynakları
için yazılan `service.PathGuard` zaten `EvalSymlinks` + bileşen bazlı kök karşılaştırması yapıyor.
**Karar:** Gezinme, bind mount ve build context aynı `PathGuard`'ı kullanıyor.
**Gerekçe:** Üçü de tek bir güven sınırının farklı yüzleri. İkinci bir uygulama, yanlış yazılabilecek
ikinci bir yer demekti; bir güvenlik kontrolünün iki kopyası er ya da geç ayrışır.
**Sonuç:** `internal/service/paths.go` tek doğruluk kaynağı. Traversal/symlink tablo testleri hem
mount hem browse tarafını kapsıyor.

### D-060 — Build context boru üzerinden akıyor, diske ikinci kez yazılmıyor
**Durum:** Kabul · **Faz:** M6
**Karar:** `WriteBuildContext` tar'ı bir `io.Pipe`'a yazıyor, engine de aynı borudan okuyor.
**Gerekçe:** Önce geçici bir tar dosyası üretmek, birkaç gigabaytlık bir ağacı diskte iki kere
tutmak demekti; `/var/lib` dolduğunda build değil daemon ölür.
**Sonuç:** Boyut limiti (`DefaultMaxContextBytes`, 512 MiB) akış sırasında uygulanıyor; aşıldığında
boru hata ile kapanıyor ve engine kısmi bir tar görmek yerine hatayı alıyor.

### D-061 — Symlink'ler context'e bağ olarak giriyor, izlenmiyor
**Durum:** Kabul · **Faz:** M6
**Karar:** Tar'a symlink girdisi olarak yazılıyor; hedefi okunmuyor.
**Gerekçe:** İzlemek, whitelist dışındaki bir dosyanın (`/etc/shadow`) context'e kopyalanması demekti.
Docker CLI de aynısını yapıyor, dolayısıyla davranış operatörün beklediğiyle aynı.
**Sonuç:** Kök dışına işaret eden bir bağ, image içinde kırık bir bağ olur — sızıntı değil.

### D-062 — Build, kendisini izleyen soketten uzun yaşıyor
**Durum:** Kabul · **Faz:** M6
**Bağlam:** Sekmeyi kapatmak, "bu build'i durdur" demek değil.
**Karar:** Build `context.WithoutCancel(r.Context())` üzerine kurulu bir task'ta çalışıyor; soket
koptuğunda yalnız frame gönderimi duruyor, kanallar sonuna kadar boşaltılıyor.
**Gerekçe:** Yarıda kesilen bir build ne image üretir ne de log arşivler; kayıt "running"de asılı kalır.
**Sonuç:** İptal yalnız `POST /builds/{id}/cancel` ile oluyor. Task, build'in kendi id'siyle
kaydediliyor (`TaskRegistry.StartWithID`), bu yüzden ikinci bir tanımlayıcı taşımaya gerek yok.
2 saatlik zaman aşımı, yavaş değil takılmış bir build'i sınırlıyor.

### D-063 — Restart'ta "running" kalan build'ler uzlaştırılıyor
**Durum:** Kabul · **Faz:** M6
**Karar:** `Builder.ReconcileRunning` açılışta bu satırları "canceled" olarak kapatıyor.
**Gerekçe:** Build, onu başlatan daemon'a bağlı; süreç ölünce engine isteği de ölüyor. Kayıt kendi
başına asla bitemez, sonsuza dek "running" görünür.
**Sonuç:** Açıklama mesajı ne olduğunu söylüyor: "iskeled restarted while this build was running".

### D-064 — Log dosyası 30, kayıt 180 gün duruyor
**Durum:** Kabul · **Faz:** M6
**Karar:** `PruneLogs` yalnız dosyayı siliyor ve `log_archived`'ı düşürüyor; satırı `DeleteOlderThan`
çok daha sonra siliyor.
**Gerekçe:** "Bu build ne zaman oldu, ne üretti" bilgisi ucuz; megabaytlarca çıktı değil.
**Sonuç:** UI, `log_archived` false ise "çıktıyı göster" düğmesini hiç göstermiyor; endpoint yine de
410 döndürüyor.

---

## M7 Sırasında Alınan Kararlar

### D-065 — Interpolasyon yalnız stack'in kendi `.env`'ini görüyor
**Durum:** Kabul · **Faz:** M7
**Bağlam:** `docker compose` CLI'ı `${VAR}` için kabuğun ortamını da okur. iskeled'in ortamı ise kabuk değil:
secret key yolu, veritabanı yolu ve unit dosyasının verdiği ne varsa orada.
**Karar:** Interpolasyon kaynağı yalnız stack'in `.env` içeriği. Servis `environment:` bloğundaki değersiz
girdiler (`- FROM_HOST`) de düşürülüyor — onlar da "çağıranın ortamından kopyala" demek.
**Gerekçe:** Aksi hâlde "bu stack'i ayağa kaldır", "bana daemon'ın ortamını yazdır"a dönüşürdü.
**Sonuç:** `${VAR:-default}` çalışıyor, `${VAR:?mesaj}` deploy'u reddediyor, `${VAR}` boş kalırsa **uyarı**
üretiliyor. Davranış `docs/compose-support.md`'de açıkça yazılı.

### D-066 — Parser'ın kendi uyarıları yakalanıyor, stderr'e sızmıyor
**Durum:** Kabul · **Faz:** M7
**Bağlam:** compose-go, "değişken atanmamış, boş string kullanılıyor" gibi düzeltmelerini dönüş değeriyle değil
global logrus ile bildiriyor.
**Karar:** Parse sırasında logrus'a geçici bir hook takılıyor, çıktı `io.Discard`'a alınıyor ve uyarılar
`[]Warning` olarak dönüyor. Parse, global hook yüzünden bir mutex ile seri hâle getiriliyor.
**Gerekçe:** JSON log yazan bir daemon'ın araya düz metin satırı sıkıştırması bir sorun; asıl sorun ise
`${DB_PASSWORD}` boş kaldığında operatörün bunu hiç duymaması. Parse, insanın yazdığı bir belge üzerinde
milisaniyelik iş; mutex'in maliyeti tasarım yapmaya değmez.
**Sonuç:** Obsolete `version:` uyarısı eleniyor — her dosyada var ve eyleme dönüşmeyen uyarı, uyarıları
görmezden gelmeyi öğretir.

### D-067 — Değişmeyen servis yerinde bırakılıyor (config-hash)
**Durum:** Kabul · **Faz:** M7
**Karar:** Her container, oluşturulduğu tanımın SHA-256 özetiyle etiketleniyor
(`com.docker.compose.config-hash`, compose'un kendi etiketi). `up`, özet aynıysa ve container çalışıyorsa ona
dokunmuyor.
**Gerekçe:** Her deploy'da her şeyi yeniden yaratmak, komşusunun image etiketi değişti diye veritabanını
yeniden başlatmak demek. Özet isim ve replica etiketlerini dışlıyor; yoksa aynı servisin iki kopyası her
seferinde farklı özetlenir ve ikisi de yeniden yaratılırdı.
**Sonuç:** CLI ile ayağa kaldırılmış bir stack de aynı ölçüyle değerlendiriliyor, çünkü etiket ortak.

### D-068 — Compose dosyası privileged kapısını ve whitelist'i aşamıyor
**Durum:** Kabul · **Faz:** M7
**Karar:** `privileged: true`, `cap_add`, `devices`, `security_opt`, `sysctls` YAML'dan geldiğinde de aynı izin
kapısından geçiyor; bind mount kaynakları ve build context'leri aynı `PathGuard`'dan.
**Gerekçe:** Bir compose dosyası, kutuyu işaretlemekle aynı istektir. İkisinin farklı davranması, sihirbazdaki
kapıyı anlamsız kılardı.
**Sonuç:** Reddedilen deploy hiçbir şey yaratmadan duruyor ve **hangi servis, hangi alan** olduğunu listeliyor —
operatör birini düzeltip yeniden deneyerek diğerini keşfetmiyor.

### D-069 — Git için `git` binary'si çalıştırılıyor, gömülü implementasyon değil
**Durum:** Kabul · **Faz:** M7
**Karar:** Klonlama `exec.Command("git", ...)` ile, `--depth 1`, `GIT_TERMINAL_PROMPT=0` ve 5 dakikalık zaman
aşımıyla. Binary yoksa hata bunu açıkça söylüyor.
**Gerekçe:** go-git birkaç megabaytlık bir bağımlılık; repodan deploy eden her makinede `git` zaten var.
**Sonuç:** URL doğrulaması bu kararın bedeli: `ext::` git'e keyfî komut çalıştırtır, tire ile başlayan bir URL
seçenek olarak okunur. İkisi de reddediliyor, `file://` ve yerel yollar da öyle. `git_test.go` bunları pinliyor.

### D-070 — Monaco gömülü ve budanmış, editör sayfası tembel yükleniyor
**Durum:** Kabul · **Faz:** M7
**Bağlam:** `@monaco-editor/react` varsayılan olarak Monaco'yu CDN'den çekiyor.
**Karar:** Monaco pakete gömülüyor; `monaco-editor` barrel'ı yerine yalnız `editor.api` + yaml/ini dil kayıtları
+ sayılı contrib içe aktarılıyor; editör sayfası `React.lazy` ile ayrı chunk'ta.
**Gerekçe:** Kendi arayüzünü sunan tek binary, internete çıkışı olmayan makinelerde çalışacak. Barrel 4 MB'lık bir
chunk üretiyordu (Solidity, PowerShell, TypeScript derleyicisi dahil); budanmış hâli 2.7 MB ve artık **ilk açılışta
indirilmiyor** — index chunk 539 kB'den 345 kB'ye düştü.
**Sonuç:** Şema doğrulaması istemcide yok; `POST /stacks/validate` deploy'un çalıştırdığı kontrollerin aynısını
çalıştırıyor. İkinci ve daha zayıf bir doğrulayıcı, karar veren doğrulayıcıyla çelişirdi.

### D-071 — Stack okuma, Docker'a bağlı değil
**Durum:** Kabul · **Faz:** M7
**Bağlam:** `GET /stacks/{id}` container listesi için engine'e gidiyordu; engine kapalıyken tüm istek 503 dönüyordu.
**Karar:** Engine'e ulaşılamazsa tanım yine dönüyor, canlı durum boş kalıyor ve `engine_error` alanı nedeni
söylüyor.
**Gerekçe:** Bir stack okuma isteği önce bir Docker işlemi değil: compose dosyası, servisleri ve uyarıları
engine olmadan da bilinebilir. Docker'ı çökmüş bir operatörün, düzeltmek üzere olduğu dosyayı okuyamaması saçma.
**Sonuç:** Arayüz sarı bir bant gösteriyor; `stack_test.go` bunu pinliyor. Uçtan uca doğrulamada da bu yoldan
geçildi (bu ortamda Docker yok).

### D-072 — Template'ler betik değil form
**Durum:** Kabul · **Faz:** M8
**Bağlam:** Katalog girdilerinin "kurulum betiği" çalıştırması, panelin tüm güvenlik kapılarını atlatmanın en kısa yolu olurdu.
**Karar:** Bir template JSON şemasıyla sınırlı bir sorular kümesi + bir container tarifi. Render sonucu sıradan bir
`ContainerSpec`; oradan sonra `PathGuard`, privileged kapısı ve RBAC aynen işliyor. Şema `DisallowUnknownFields` ile
okunuyor; bind mount dışında host'a değen bir alan yok.
**Gerekçe:** Katalog, `/etc/iskele/templates` altına dosya bırakabilen herkesin panelin yetkilerini devralabildiği bir
uzantı noktası olmamalı. Yanlış cevapların hepsi tek seferde `details.fields` altında dönüyor — formu üç kez
göndertmek yerine.

### D-073 — Host metrikleri tek pakette ve "elden geldiğince"
**Durum:** Kabul · **Faz:** M8
**Bağlam:** gopsutil platforma özel dosya/syscall okuyor; container içinde koşan bir daemon'da bunların bir kısmı yok
(swap yok, load average yok, `/proc` kısıtlı).
**Karar:** gopsutil'i yalnız `internal/hostinfo` içe aktarıyor — `internal/docker`'ın SDK için oynadığı rolün aynısı.
Okumalar tek tek başarısız olabiliyor; başarısız olan `errors` listesine yazılıyor, istek yine 200 dönüyor.
CPU yüzdesi paket-düzeyi global yerine `Collector` içinde tutulan önceki örneğe göre hesaplanıyor; ilk okuma
karşılaştıracak bir şey bulamadığı için `-1` diyor.
**Gerekçe:** Altı sayıdan biri okunamadı diye boş kalan bir panel, beş sayı gösterenden kötü. Global durum ise farklı
hızlarda yoklayan iki sekmenin birbirinin ölçümünü bozması demekti.
**Sonuç:** `GET /system/host` Docker kapalıyken de çalışıyor — `engine` alanı düşüyor, makinenin kendi sayıları
duruyor. Panelin en çok işe yaradığı an tam olarak orası.

### D-074 — Activity feed engine olaylarıdır, audit değil
**Durum:** Kabul · **Faz:** M8
**Bağlam:** Dashboard'daki akış ya panelin kendi audit kaydından ya da engine'in olay akışından beslenebilirdi.
**Karar:** `SSE /system/events` — yani makinede olan biten. SSH'tan durdurulan bir container, kendini yeniden
başlatan bir servis ve panelden yapılan işlem aynı listede.
**Gerekçe:** Dashboard "bu makine ne durumda?" sorusunu yanıtlıyor, "bu panelden kim ne yaptı?" sorusunu değil;
ikincisi audit ekranının işi (M8-F). Akış aynı zamanda sayımların bayatladığını haber veriyor: her olay sonrası
sorgular 1 sn'lik debounce ile tazeleniyor — `docker compose up` düzinelerce olay üretiyor, her biri için dört liste
çekmek engine'i en meşgul anında dövmek olurdu.
**Sonuç:** Ticket tek kullanımlık olduğu için tarayıcının kendi yeniden bağlanması yetmiyor; hook kopmayı görünce
yeni ticket alıp üstel geri çekilmeyle yeniden bağlanıyor.

### D-075 — TOTP elde yazıldı, RFC vektörleriyle sabitlendi
**Durum:** Kabul · **Faz:** M8
**Bağlam:** İki adımlı doğrulama için bir kütüphane çekmek ya da RFC 6238'i uygulamak arasında seçim vardı.
**Karar:** `internal/auth/totp.go` (~150 satır) elde yazıldı; testler RFC 6238'in kendi test vektörlerini koşuyor.
Parametreler seçim değil, dayatma: SHA-1, 6 hane, 30 sn. Doğrulayıcı uygulamaların tamamı bunu varsayıyor, başka
bir şey seçen sunucu kodları çalışmayan sunucu olur. Kod karşılaştırması `subtle.ConstantTimeCompare` ile ve
erken çıkışsız — hangi adımın tuttuğu zamanlamadan okunamıyor.
**Gerekçe:** Kendi çıktısını kendisiyle karşılaştıran bir test, yanlış ama tutarlı bir algoritmayı da geçirirdi;
RFC vektörleri bunun TOTP olduğunu kanıtlıyor. Bağımlılık yüzeyi de artmıyor.
**Sonuç:** Gizli anahtar AES-GCM ile şifreli saklanıyor (registry parolalarıyla aynı kutu). Secret key yoksa
iki adımlı "kullanılamaz" diyor — açık olan bir hesap giriş yapamıyor. Ters yönde başarısız olmak, anahtarı
kaybetmeyi ikinci faktörü atlamanın yoluna çevirirdi.

### D-076 — Son yönetici koruması, "kendi hesabına dokunma" kuralının yerini aldı
**Durum:** Kabul · **Faz:** M8
**Bağlam:** İlk yazımda iki ayrı kapı vardı: kendi rolünü/etkinliğini değiştirememek ve son admin'i düşürememek.
İkincisi hiç tetiklenemiyordu — değişikliği yapan zaten bir admin olduğuna göre, hedef kendisi değilse başka bir
admin her zaman var demektir; hedef kendisiyse ilk kapı önce kapanıyordu.
**Karar:** Korunan değişmez tek: **paneli yönetebilecek etkin hesapsız bırakma**. Kendi hesabı da bu kapsamda —
devretmek meşru bir iş, ama ancak devralacak biri varken. Ayrıca kendi hesabını *silmek* yasak (`SelfDelete`):
demote'un aksine kimsenin kasten yaptığı bir hamle değil ve yapacak başka admin hep var.
**Gerekçe:** Ulaşılamayan bir kontrol, kontrol değil; test edilemeyen bir garanti de garanti değil. Devredemeyen
bir admin ise gerçek bir kısıt: yerine birini atayıp çekilmek isteyen operatörün önü kapalıydı.
**Sonuç:** Devre dışı bir admin sayıma girmiyor — giriş yapamayan hesap kimseyi yönetemez. Parola sıfırlama ve
devre dışı bırakma o hesabın tüm oturumlarını kapatıyor; tarayıcısında geçerli token duran devre dışı hesap
devre dışı değildir.

### D-077 — Audit ekranı, eksik olanı ortaya çıkardı: container yaşam döngüsü hiç kaydedilmiyordu
**Durum:** Kabul · **Faz:** M8
**Bağlam:** Denetim ekranı yazılırken testler start/stop/restart/pause/kill/rename/remove için tek bir kayıt
bulamadı. M2 auth'u, M5 image/volume/network ve create'i kaydediyordu; M1'den kalan container yaşam döngüsü
handler'ları hiç geri dönülüp bağlanmamıştı. `Container` servisinin `recorder` alanı vardı ve kullanılmıyordu.
**Karar:** Yaşam döngüsünün tamamı kaydediliyor — başarısız denemeler dahil. Kayıtsız ham işlemler
küçük harfli (`start`, `stop`, `remove`…) olarak ayrıldı; toplu işlem (Batch) container başına kendi kaydını
yazdığı için bunları çağırıyor.
**Gerekçe:** "Veritabanımı kim durdurdu?" sorusunu yanıtlayamayan bir denetim kaydının üstüne ekran koymak,
olmayan bir güvenceyi varmış gibi göstermek olurdu. Reddedilen bir işlem de en az başarılı olan kadar kayda
değer: "kim silmeye çalıştı da silemedi" tam olarak logdan beklenen cevaptır.
**Sonuç:** Aynı işlemin iki kez kaydedilmemesi önemliydi — iki kayıt iki işlem gibi okunur ve sayan operatör
yanlış sayar. Ayrım bu yüzden dışa açık (kayıtlı) / paket içi (kayıtsız) olarak yapıldı.

### D-078 — Denetim kaydı API üzerinden salt-okunur
**Durum:** Kabul · **Faz:** M8
**Bağlam:** Ekranla birlikte "eski kayıtları temizle" düğmesi eklemek doğal görünüyordu.
**Karar:** `/audit` altında yalnız GET var: liste, facet'ler ve dışa aktarma. Kayıt silen ya da düzenleyen
hiçbir uç yok. Bir test bunu pinliyor. Kayıtların yaşlanarak düşmesi store'un işi
(`AuditRepo.DeleteBefore`) ve bir retention ayarına bağlanacak — **bu ayar henüz yok**, yani şu an kayıt
tablosu yalnızca büyüyor. Retention M8-G'de ayarlar sayfasıyla birlikte geliyor.
**Gerekçe:** Yöneticinin yeniden yazabildiği bir denetim kaydı denetim kaydı değildir. Diski dolan operatörün
ihtiyacı retention ayarıdır; tek tek satır silmek için bir gerekçe yok.
**Sonuç:** Dışa aktarma ekrandaki filtrenin aynısını alıyor (limit/offset hariç: dışa aktarma tüm sonuçtur) ve
satırlar 500'lük gruplar hâlinde okunup yazılıyor — yoğun bir makinenin bir yılı belleğe sığmak zorunda değil.
Tarayıcı tarafında indirme `Authorization` başlığı gerektirdiği için `<a href>` değil, kimlikli bir `fetch` +
object URL ile yapılıyor.

### D-079 — Prune'lar admin'e ait ve her biri kendi kuralını söylüyor
**Durum:** Kabul · **Faz:** M8
**Bağlam:** Dört prune ucu var ve "kullanılmayan" her biri için başka bir şey demek.
**Karar:** Hepsi `prune` izninin (yalnız admin) arkasında. Onay kutusu engine'in gerçek kuralını yazıyor;
volume prune ayrıca adının yazılmasını istiyor. Image prune yalnız dangling katmanları alıyor — `all`
etiketli image'ları da silerdi ve bu, aynı düğmenin arkasına saklanacak bir karar değil.
**Gerekçe:** Prune, kimsenin tek tek adını vermediği nesneleri siler. Volume dışındakiler yeniden üretilebilir;
volume birinin veritabanıdır ve başka hiçbir yerden geri gelmez.
**Sonuç:** `POST /containers/prune` bu fazda eklendi (docker katmanında yoktu). Sonuç toast olarak bildiriliyor:
kaç nesne gitti, ne kadar yer açıldı.

### D-080 — Soket yolu ve yol beyaz listesi ayarlar ekranından değiştirilemez
**Durum:** Kabul · **Faz:** M8
**Bağlam:** PROMPT ayarlar sayfasında "socket yolu, whitelist, retention" istiyor. Retention'ı çalışma zamanında
değiştirmek doğal; diğer ikisi değil.
**Karar:** `docker_host` ve `allowed_paths` ekranda **salt-okunur** gösteriliyor, yanlarında hangi dosyadan
geldikleri yazıyor. Değiştirmek config dosyasını düzenleyip servisi yeniden başlatmak demek. `PUT /settings`
bilinmeyen alanı reddediyor (400), yani whitelist'i ayarladığını sanan bir istemciye "ayarlamadın" deniyor —
hiçbir şey yapmayan bir 200 dönmek yerine.
**Gerekçe:** Bunlar açılış anında kurulan güvenlik sınırları. `allowed_paths`'i tarayıcıdan genişletebilen bir
yönetici, tüm dosya sistemini bir container'a bağlamaya tek istek uzaktadır — panel yöneticiliğinden host
root'una giden bir yol açılırdı. Dosyayı düzenlemek bilinçli bir eylemdir ve kendi izini bırakır (dosyanın mtime'ı).
**Sonuç:** Ekran yine de bu değerleri *gösteriyor*: "bu panel host'un ne kadarına erişebiliyor" sorusunun cevabı
operatörün görmesi gereken bir şey. Boş whitelist ayrıca sarı uyarıyla işaretleniyor — her bind mount'u reddeden
bir yapılandırma kasıtlı olmayabilir.

### D-081 — Retention ayarı her süpürmede okunuyor, açılışta değil
**Durum:** Kabul · **Faz:** M8
**Bağlam:** M8-F'te denetim kaydının "yaşlanarak düşmesi" doküman edilmişti ama `DeleteBefore` hiçbir yerden
çağrılmıyordu; ayar da yoktu. O dokümanı düzeltip işi buraya bıraktım.
**Karar:** `audit_retention_days` settings tablosunda; 0 = sonsuza kadar sakla ve bu varsayılan. Günlük
housekeeping döngüsü ayarı **her tick'te** yeniden okuyor.
**Gerekçe:** Ayarı değiştiren yönetici bir sonraki süpürmenin buna uymasını bekler, bir sonraki yeniden
başlatmanın değil. Varsayılanın "sakla" olması da bilinçli: ayarlar sayfasını hiç açmadığı için birinin denetim
geçmişini silmek, onun adına verilecek yanlış karardır.
**Sonuç:** `retention > 0` kapısı testle sabitlendi — sıfırın "şu andan öncekini sil"e dönüşmesi bu işin
bozulma biçimidir.

### D-082 — Fake build'ler test tarafından tutulabiliyor
**Durum:** Kabul · **Faz:** M8
**Bağlam:** `TestCancelStopsARunningBuild` tam suite ağır yük altında koşarken düştü: fake tüm build olaylarını
testin bir sonraki satırından önce oynatıp bitirmişti, `Cancel` de "bu build zaten başarılı" dedi.
**Karar:** Fake'e `HoldBuilds()` eklendi; build ilk olaydan önce, test bırakana ya da context iptal edilene kadar
bekliyor. İptal testi build'i tutuyor, sonra iptal ediyor.
**Gerekçe:** Test yanlış bir şey iddia etmiyordu; iddiasının ön koşulunu (build'in hâlâ koşuyor olması)
zamanlamaya bırakmıştı. "Yük altında bazen kırmızı" bir test, kırmızıyken bakılmayan bir testtir.
**Sonuç:** Dört çekirdek meşgulken 10 kez üst üste geçtiği doğrulandı. Üretim kodunda değişiklik yok — kusur
testin kendisindeydi.

### D-083 — sd_notify elde yazıldı; unit dosyası daemon'ın gerçekte yaptığına uyduruldu
**Durum:** Kabul · **Faz:** M9
**Bağlam:** systemd unit'ini yazarken `Type=notify-reload` ve `ExecReload=/bin/kill -HUP $MAINPID` koymuştum.
İkisi de yanlıştı: daemon sd_notify göndermiyordu (systemd READY=1 bekleyip zaman aşımına düşerdi) ve SIGHUP'ın
varsayılan davranışı süreci sonlandırmak — yani "reload" servisi öldürürdü.
**Karar:** Unit'i kısmak yerine eksik tamamlandı. `internal/systemd` (~150 satır, bağımlılıksız) READY=1,
STOPPING=1, STATUS ve WATCHDOG gönderiyor; unit `Type=notify` + `WatchdogSec=60s`; `ExecReload` yok.
`NOTIFY_SOCKET` yoksa her çağrı sessizce hiçbir şey yapmıyor, yani elle başlatılan daemon eskisi gibi davranıyor.
**Gerekçe:** Protokol tek bir datagram; bunun için bağımlılık çekmek, yerine koyduğu şeyden fazla kod denetlemek
demekti. Watchdog kasten Docker'a bakmıyor: Docker hıçkırdığında iskeled'i yeniden başlatmak, "Docker kapalı"
diyebilmek için ayakta kalan servisin varlık sebebinin tam tersi olurdu. Ping yalnız sürecin kilitlenmediğini
kanıtlar — bir watchdog'un gerçekten söyleyebileceği tek şey de budur.
**Sonuç:** `Type=notify` sayesinde bu unit'ten sonra sıralanan birimler, kabul etmeyen bir sokete karşı
başlamıyor. STOPPING=1, yavaş bir drain'in watchdog'un yakalamak için var olduğu takılmayla karıştırılmasını
önlüyor.

### D-084 — OpenAPI ↔ router senkronu gözle değil testle tutuluyor
**Durum:** Kabul · **Faz:** M9
**Bağlam:** ACCEPTANCE M7 "openapi.yaml tüm endpoint'lerle senkron" diyordu ve doğrulaması "CI" idi — ama böyle
bir kontrol yoktu; senkron olduğuna elle bakılıyordu.
**Karar:** `chi.Walk` ile mount edilmiş her route çıkarılıp spec'teki path'lerle iki yönlü karşılaştırılıyor.
Üçüncü bir test de yürüyüşün boş dönmediğini kontrol ediyor — aksi hâlde ilk ikisi hiçbir şeyi denetlemeden
geçerdi.
**Gerekçe:** Spec bu projede sonradan yazılan bir doküman değil, arayüzün üretildiği kaynak. Belgelenmemiş bir
route hiçbir istemcinin bilmediği bir route'tur; var olmayan bir belgelenmiş route ise daha kötüsüdür — tutulmayan
bir söz. İkisi de kod okuyarak değil, koşarak yakalanmalı.

### D-085 — Panelin korumasız olduğu ekranda da söyleniyor
**Durum:** Kabul · **Faz:** M9
**Bağlam:** loopback dışında ve TLS'siz dinlerken daemon açılışta uyarı basıyordu. ACCEPTANCE C1 ayrıca UI'da
uyarı istiyordu ve bu yoktu.
**Karar:** Ayarlar → Kurulum panelinde, `listen` loopback değilse ve TLS kapalıysa sarı uyarı.
**Gerekçe:** Açılış logunu kimse yeniden okumuyor. `listen`'i aylar önce değiştiren operatöre, o adresi gösteren
ekranda söylemek tek işe yarar an. Loopback olmayan bir hostname "belki loopback'e çözülüyordur" diye geçilmiyor:
root eşdeğeri bir API hakkında yanılmanın yanlış yönü budur.

### D-086 — Docker SDK'sının daemon tarafı açıkları gerekçesiyle listeleniyor, govulncheck kapatılmıyor
**Durum:** Kabul · **Faz:** M9
**Bağlam:** CI'daki `govulncheck` adımı iki açıkla kırmızıya döndü: GO-2026-4887 (CVE-2026-34040, AuthZ
plugin bypass) ve GO-2026-4883 (CVE-2026-33997, legacy plugin privilege doğrulamasında off-by-one). İkisi de
`github.com/docker/docker`'ın **tüm** sürümlerini etkili sayıyor ve o modül yolunda düzeltilmiş sürüm yok —
düzeltme `github.com/moby/moby/v2 v2.0.0-beta.8`'de, yani başka bir modül yolunda. Modül tek repo olduğu için
client'ı linkleyen herkes daemon açıklarını da üstleniyor; govulncheck'in izleri de zaten ağırlıklı olarak
`init` çağrıları.
**Karar:** Üç seçenek vardı: (a) `moby/v2`'ye geçmek, (b) adımı kaldırmak/`|| true` yapmak, (c) gerekçeli
istisna listesi. (a) beta bir modüle v0.1.0'da bağlanmak demekti ve SDK yüzeyi baştan taşınacaktı; (b) tarama
varmış gibi görünüp hiçbir şey taramamak olurdu. (c) seçildi: `scripts/vulncheck` govulncheck'i JSON modunda
kendisi çalıştırıyor, yalnız **symbol seviyesindeki** bulguları sayıyor ve iki ID'yi yanlarına yazılan
değerlendirmeyle geçiriyor.
**Gerekçe:** Her iki açık da daemon kodunda: biri engine'in authorization-plugin middleware'i, diğeri legacy
plugin kurulum yolu. iskeled ne authz plugin çalıştırıyor ne de plugin API'si açıyor; ikisi de derlenen
yüzeyimizde değil. Liste körelmesin diye üç kırılma noktası var: listede olmayan çağrılan bir açık, artık
**düzeltilmiş sürümü olan** bir istisna (yani gerekçe geçersiz, yapılacak şey yükseltmek) ve taramanın artık
raporlamadığı ölü bir istisna. Filtrenin kendisi test edilmiş — her şeyi geçiren bir filtre, taramasız CI'dan
daha kötüdür, çünkü yeşil görünür.
**Sonuç:** `make vuln` artık `go run` üzerinden geçmiyor: `go run` her hatayı 1'e indirgiyor ve "açık bulundu"
(3) ile "veritabanına ulaşılamadı" ayırt edilemiyordu. Araç govulncheck'i geçici bir GOBIN'e kurup çalıştırıyor,
çıkış kodu 0 veya 3 değilse tarama başarısız sayılıyor. moby/v2 stabilleştiğinde geçiş ayrı bir iş olarak
değerlendirilecek; o gün geldiğinde bu iki istisna "düzeltilmiş sürüm var" kuralıyla kendiliğinden CI'ı kıracak.

---

## Uygulama Sırasında Doğrulanacak Varsayımlar

### A-001 — Docker minimum API sürümü 1.41 (Docker 20.10+) — **M1'de doğrulandı**
`client.WithAPIVersionNegotiation()` açık; `docker.MinimumAPIVersion` sabiti 1.41.
Daemon erişilemezse `KindUnavailable` + `docker` grubu ipucu ile anlamlı hata verilir.

### A-002 — Health/version endpoint'leri auth'suz kalır
`/api/v1/health` yalnız `{"status":"ok"}` döner, iç durum sızdırmaz. `/api/v1/version` sürüm + commit döner.
Docker engine sürümü gibi bilgiler yalnız auth'lu `/system/info` altında.

### A-003 — "Files" sekmesi (container içi dosya yöneticisi) opsiyoneldir
PROMPT §4.2 "opsiyonel M9" diyor. M9'da diğer teslim maddeleri bittiğinde ele alınır; girilmezse
`ACCEPTANCE.md`'de "kapsam dışı" olarak işaretlenir ve README'de belirtilir.

### A-004 — Uzak Docker host (TCP/TLS) M9'da yapılandırma düzeyinde
Config'te `docker_host` zaten değiştirilebilir olduğundan TCP+TLS bağlantısı desteklenir; çoklu
endpoint seçimi UI'ı v0.2'ye bırakılır (PLAN §1 kapsam dışı).

### A-005 — Image "export/save" opsiyonel
PROMPT §4.7'de opsiyonel. Zaman kalırsa M5'te, kalmazsa v0.2.

---

## Değişen / İptal Edilen Kararlar

| Karar | Ne oldu | Yerine |
|---|---|---|
| D-013 (Go 1.23) | Docker SDK bağımlılık ağacı Go 1.25 gerektiriyor | D-019 |

---

## Açık Sorular (kullanıcıya sorulmadan varsayımla ilerlenecek)

| # | Soru | Varsayılan davranış |
|---|---|---|
| Q1 | Panel için varsayılan dil TR mi EN mi? | Tarayıcı diline göre otomatik, düşerse `en`; ayarlardan değiştirilebilir |
| Q2 | Telemetri / güncelleme kontrolü olacak mı? | Hayır. Hiçbir dış çağrı yapılmaz (registry pull hariç) |
| Q3 | Varsayılan tema? | Dark (PROMPT §7) |
| Q4 | Docker olmayan makinede panel açılsın mı? | Evet — açılır, Docker'a bağlı ekranlarda `DOCKER_UNAVAILABLE` banner'ı gösterilir |
