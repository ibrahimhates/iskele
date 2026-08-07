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

## Uygulama Sırasında Doğrulanacak Varsayımlar

### A-001 — Docker minimum API sürümü 1.41 (Docker 20.10+)
Negotiation açık; daha düşük sürümde anlamlı hata verilir. M1'de doğrulanır.

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

_(Henüz yok.)_

---

## Açık Sorular (kullanıcıya sorulmadan varsayımla ilerlenecek)

| # | Soru | Varsayılan davranış |
|---|---|---|
| Q1 | Panel için varsayılan dil TR mi EN mi? | Tarayıcı diline göre otomatik, düşerse `en`; ayarlardan değiştirilebilir |
| Q2 | Telemetri / güncelleme kontrolü olacak mı? | Hayır. Hiçbir dış çağrı yapılmaz (registry pull hariç) |
| Q3 | Varsayılan tema? | Dark (PROMPT §7) |
| Q4 | Docker olmayan makinede panel açılsın mı? | Evet — açılır, Docker'a bağlı ekranlarda `DOCKER_UNAVAILABLE` banner'ı gösterilir |
