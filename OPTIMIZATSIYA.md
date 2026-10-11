# KRADCHAT — To'liq Optimizatsiya Hisoboti (OLDIN → KEYIN)

> Sana: 2026-09-23
> Qamrov: backend (Go) + frontend (Go→WASM) + xavfsizlik + DevOps
> Verifikatsiya: `go vet` ✅ · `go build ./...` ✅ · `go test ./...` (barcha paketlar `ok`) ✅ · WASM build ✅ · `gofmt` ✅

---

## 1. Server entrypoint (`cmd/server/main.go`)

| OLDIN | KEYIN | Nima uchun yaxshiroq |
|---|---|---|
| Faqat `ReadHeaderTimeout` bor edi | `ReadTimeout: 30s` + `WriteTimeout: 30s` qo'shildi | Sekin-body Slowloris va sekin o'qiyotgan client hujumlari yopildi |
| `LOG_LEVEL` env o'qilar, lekin hardcode `LevelInfo` ishlatilardi | `parseLogLevel()` — `debug/warn/error/info` haqiqatdan ishlaydi | Dead setting yo'qoldi, prod'da log shovqini kamayadi |
| `os.Exit(1)` goroutine ichida — `defer db.Close()/svc.Stop()` ishlamay qolardi | Server xatosi kanal orqali `main` ga uzatiladi, graceful shutdown bilan yopiladi | Flush qilinmagan xabarlar va yopilmagan ulanishlar qolmaydi |

## 2. Konfiguratsiya (`internal/config/config.go`)

| OLDIN | KEYIN | Nima uchun yaxshiroq |
|---|---|---|
| Postgres DSN'da `DB_PORT` ishlatilmasdi (har doim 5432) | `port=` DSN'ga qo'shildi, default 5432 | `DB_PORT=5433` endi ishlaydi |
| MySQL parol `user:pass@...` qo'lda yig'ilardi — `: @ / ?` parolni buzardi | `mysql.Config.FormatDSN()` orqali xavfsiz format | Maxsus belgili parollar bilan ulanish ishonchli |
| JWT minimum 16 belgi | Minimum **32 belgi** (HS256 uchun 256-bit) | Brute-force chegarasi 2 baravar oshdi |
| Noto'g'ri env (`PORT=abc`, `RATE=xyz`) jimgina default'ga tushardi | `slog.Warn` yoziladi | Operator xatosi seziladi, sirli sozlama yo'q |

## 3. Repository qatlami (`internal/repository/`)

| OLDIN | KEYIN | Nima uchun yaxshiroq |
|---|---|---|
| `block_repo.go`: MySQL shoxobchasi `INSERT OR IGNORE` (SQLite sintaksisi) — MySQL'da har block **sintaksis xato** berardi | Dialektga ajratildi: postgres `ON CONFLICT`, sqlite `OR IGNORE`, mysql `INSERT IGNORE` | Bloklash uchala bazada ishlaydi |
| `message_repo.go` `Insert` non-postgres'da `m.ID` o'rnatmasdi → ack `ID=0` | `LastInsertId` + fallback `SELECT by client_msg_id` | Retry/ack yo'li har doim real ID oladi |
| `Update/SetVerified/UpdatePassword/Deactivate/MarkDeleted` — 0 qatorga tegsa ham `nil` (silent "ok") | `RowsAffected == 0` → `ErrNotFound` | O'zganing ID'siga operatsiya endi "muvaffaqiyatli" ko'rinmaydi |
| `History/Devices/Backgrounds` da `rows.Err()` tekshirilmasdi | Tekshiriladi | Kesilgan ro'yxat jimgina qaytmaydi |
| `Conversations`: `deleted_at` filtrsiz, limitsiz, leading-wildcard LIKE | `deleted_at IS NULL` filtri + `LIMIT 100` | O'chirilgan xabar "oxirgi" bo'lmaydi, katta DB'da tez |
| `wrapErr` faqat 3 ta matnni tanirdi | `23505/1062/unique_/already exists` kodlari ham | Xato → to'g'ri HTTP kod (409/404, 500 emas) |
| `database.go` `kindOf` `*Error` type-assert (o'ralgan xatoda ishlamasdi) | `errors.As` | `fmt.Errorf("...: %w")` bilan o'ralgan xatolar ham to'g'ri tasniflanadi |

## 4. Auth va 2FA (`auth_service.go`, `twofa_service.go`)

| OLDIN | KEYIN | Nima uchun yaxshiroq |
|---|---|---|
| Device `Create` xatosi yutilib `deviceID=""` bilan davom etardi | Xato qaytariladi (`return nil, err`) | `device_id=NULL` buzuq sessionlar yo'q |
| Login'da device 2FA'dan **oldin** yaratilardi — parolni bilgan hujumchi `maxDevices=10` ni to'ldirib DoS qilardi | 2FA yoqilgan bo'lsa pending `deviceID=""` bilan saqlanadi, device verify'da yaratiladi | Device-exhaustion hujumi yopildi |
| Refresh: avval `Revoke` → keyin `Create` (oradagi crash = logout); parallel double-refresh race; o'chirilgan device tekshirilmasdi | Avval `Create` → keyin `Revoke`; device active tekshiruvi | Crash'da logout yo'q, terminated device jonlanmaydi |
| `pendingStore` to'lganda **tasodifiy** kalit o'chardi | Eng eski (ExpiresAt minimal) kalit o'chadi | Begunoh sessiyalar siqib chiqarilmaydi |
| Fingerprint faqat `sha256(UA)` — bir xil brauzer = bir fingerprint | `sha256(UA + IP/24)` | Qurilma audit izi ishonchli |
| `Conversations` N+1 (50 DM = 100+ so'rov), `SearchUser` snapshot 2 marta | User keshi + bitta snapshot chaqiruvi | DB yuki ~2 baravar kamaydi |
| `TwoFA.Enable` allaqachon yoqilgan bo'lsa kodsiz `nil` (soxta "muvaffaqiyat") | `ErrTwoFAAlreadyEnabled` | Chalkash "ok" yo'q |
| `TwoFA.Disable` avval TOTP → keyin parol (kod-oracle) | Avval parol → keyin kod | Token o'g'irlangan hujumchi kodni paypaslay olmaydi |
| WS ulanishda revocation yo'q edi (15 daqiqalik qayta-kirish oynasi) | `CheckWSAccess`: deactivated device + parol o'zgarishidan oldingi token rad (`LatestPasswordChange`) | Parol o'zgargandan/terminate'dan keyin eski token bilan kirib bo'lmaydi |

## 5. Chat xizmati (`chat_service.go`)

| OLDIN | KEYIN | Nima uchun yaxshiroq |
|---|---|---|
| **Broadcast-before-persist**: xabar avval efirga, keyin 300ms batch'da DB'ga — crash'da "ko'ringan lekin saqlanmagan" phantom xabar, history race, `ID=0` oynasi | **Persist-then-broadcast** (3s timeout, real ID bilan efir; DB o'lik bo'lsagina best-effort fallback) | Xabar yo'qolishi yo'q; delete/reply darhol ishlaydi; latency baribir <100ms (DB yozish 5–20ms) |
| Mention UUID/existence tekshirilmasdi, `FileSize` manfiy bo'lishi mumkin edi | `filterValidMentions` (UUID format + self + dedup) + `FileSize` chegara (`ErrFileTooBig`) | Soxta mention spam va hajm spoofing kamaydi |
| `flush` retry eskirgan ctx'da — hammesi soxta fail | Har retry'ga yangi 5s ctx; saqlanganlar o'tkazib yuboriladi | Retry ishonchli |
| `notifyReply/ToPayload` — `context.Background()` cheksiz | 3s timeout | Sekin DB head-of-line blocking qilmaydi |
| `Stop()` ikki marta chaqirilsa panic (`close` of closed channel) | `sync.Once` guard | Graceful shutdown xavfsiz |

## 6. Upload (`upload_service.go`, `upload_handler.go`)

| OLDIN | KEYIN | Nima uchun yaxshiroq |
|---|---|---|
| `merge` butun faylni RAM'da yig'ardi (50MB × N parallel = **OOM**) | Stream merge (`ioCopyCapped`, 64KB bufer, cap nazorati) | Xotira sarfi O(1) — OOM DoS yopildi |
| `seen[index]=true` diskka yozishdan **oldin** qo'yilardi — yozish yiqilsa retry "qabul qilindi" deb faylsiz qaytardi → **buzuq fayl** | Atomik yozish (tmp + rename), `seen` faqat muvaffaqiyatdan keyin; poygada double-count yo'q; `merging` flag (double-merge yo'q) | Corrupt merge imkonsiz |
| Yetishmayotgan chunk tekshirilmasdi | `0..Total-1` majburiy, yetishmasa `missing chunk` xato | Buzyuq fayl qabul qilinmaydi |
| `Status(id)` egalik tekshirmasdi | `StatusFor(ownerID, id)` — handler userID uzatadi | Begona upload progress'ini ko'rib bo'lmaydi |
| `POST /api/upload` da `limitBody` yo'q edi | `limitBody(3MB)` | Izchillik + 2MB chunk chegarasiga mos |

## 7. Presence (`presence_service.go`)

| OLDIN | KEYIN | Nima uchun yaxshiroq |
|---|---|---|
| `UserSnapshot` saqlangan pointer'ni to'g'ridan-to'g'ri qaytarardi, writer uni mutatsiya qilardi → **`go run -race` da data race** | Hamma joyda copy-on-write + klon qaytarish (`MarkOffline`, `SetSnapshotMetadata`, `UserSnapshot`) | Race detector toza, tasodifiy panic/buzilish yo'q |

## 8. WebSocket (`ws/hub.go`, `client.go`, `room.go`, `ws_handler.go`)

| OLDIN | KEYIN | Nima uchun yaxshiroq |
|---|---|---|
| `Disconnect` `c.rooms` ni lock'siz o'qirdi, `Join` lock ostida yozardi → concurrent map **panic riski** | Snapshot client-lock ostida, tozalash hub-lock ostida | Panic imkonsiz, lock tartibi izchil |
| `Hub.rooms` hech tozalanmasdi (memory leak); `ClientByDeviceID` O(N) scan | Bo'sh non-global xonalar o'chadi; `byDevice` O(1) index | Uzoq uptime'da xotira barqaror |
| `Room.broadcast` RLock ostida yuborardi | Snapshot olib yuboradi (`broadcastFiltered` bilan izchil) | Slow-consumer `Close` paytida lock ushlanmaydi |
| `WS_MSG_BURST` / `WS_MAX_MESSAGE_SIZE` dead setting (burst=60 o'tardi) | `NewRateLimiterBurst` + `SetMaxMessageSize` — config haqiqatdan ishlaydi | E'lon qilingan limit = real limit |
| WS limiter'larda GC yo'q (kalitlar cheksiz o'sardi) | `SweepLimiters` har daqiqada (router orqali) | Xotira o'sishi to'xtadi |
| Dispatch `context.Background()` cheksiz, sinxron | 5s timeout'li ctx | Sekin DB bitta client'ni cheksiz bloklamaydi |

## 9. Xavfsizlik middleware/router (`security_mw.go`, `router.go`, `dedup.go`, `flood.go`)

| OLDIN | KEYIN | Nima uchun yaxshiroq |
|---|---|---|
| `DedupStore.Seen` har chaqiruvda to'liq map scan (200k key = har xabarda 200k iteratsiya, CPU DoS); to'lganda legit xabar "duplicate" deb **tashlanardi** | Prune har 64-chaqiruv/30s; to'lganda fail-OPEN + eng eskilarni evict (test yangilandi) | CPU DoS yopildi, xabar yo'qolmaydi |
| HSTS yo'q; deprecated `X-XSS-Protection`; CORS'da `Vary: Origin` yo'q | HSTS (`max-age=31536000`), `Vary: Origin`, deprecated header olib tashlandi | TLS downgrade va cache-zaharlanish himoyasi |
| `background broadcast` — global + unicast (har client `bg_list` ni **2 marta** olardi) | Faqat global broadcast | Trafik 2× kam, dublikat event yo'q |
| SPA deep-link'lar 404 edi | `/api|/ws|/static|/wasm` dan boshqa yo'llar `index.html` ga | Hash-route'lar reload'da ishlaydi |

## 10. Frontend WASM (`web/src/`, `index.html`, `boot.js`)

| OLDIN | KEYIN | Nima uchun yaxshiroq |
|---|---|---|
| `chatLayout` begona `display_name` ni escape'siz `innerHTML` ga qo'yardi → **XSS** | `html.EscapeString` (name/sub/title) | Stored-XSS teshigi yopildi |
| 2FA secret/QR escape'siz | Secret escape + QR `data:image/` validatsiya | Quote-break imkonsiz |
| Avatar `url('...')` validatsiyasiz | `safeAvatarURL` (faqat `/static/uploads` yoki `data:image/`) | CSS-break/deface yo'q |
| `sendWS` ulanmagan bo'lsa jim `return` — xabarlar izsiz yo'qolardi | Offline navbat (cap 50, typing'dan tashqari) + `onopen`'da flush; composer "Navbatda" toasti | Reconnect'da yozilganlar ketadi |
| Routing yo'q — reload DM'ni o'ldirardi | `#/chat/<room>` hash + `roomFromHash` tiklash | Deep-link/share/reload ishlaydi |
| `loadSession` JS `null` → Go `"null"` string (ortiqcha `/api/me`) | `storageString` null/undefined filtri | Kirish oqimi toza |
| `Attr()` property yozardi (atribut emas) | `setAttribute` | Semantika to'g'ri (`disabled`, `href`, `src`…) |
| WS `setTimeout` `js.Func` release'siz | `defer timerFn.Release()` | Uzoq sessiyada leak kamaydi |
| `user-scalable=no`, toast'da `aria-live` yo'q, `msg-input` role'siz | Zoom ruxsat, `aria-live="polite"`, `role="textbox"` + `aria-label` | A11y yaxshilandi |
| `boot.js` xato innerHTML'da + `?v=29` qo'lda | textContent render + `?v=30` | Xato XSS'i yo'q, yangi WASM keshdan keladi |

## 11. DevOps

| OLDIN | KEYIN |
|---|---|
| `*.log/*.out/*.err/lanip.txt` ignore'siz — tasodifiy commit riski | `.gitignore` ga qo'shildi |
| `.env.example` JWT "min-16" | "min-32" + 32+ belgili namuna |
| `build.sh` WASM'da `-ldflags` yo'q | `-ldflags "-s -w"` (build.ps1 `-Release` bilan izchil) |

---

## 12. Fayl tizimi — tubdan qayta ishlash (File Access v2)

Oldin fayllar "egaliksiz" edi: kim yuklagani DB'da yo'q, o'chirilgan xabar fayli diskda abadiy qolardi, `/static/uploads/2026/` katalog listing ochiq edi, upload'ni bekor qilib bo'lmasdi.

### 12.1. `uploads` registry (DB)

| OLDIN | KEYIN |
|---|---|
| Fayl haqida DB'da hech narsa — faqat diskdagi UUID nom | `uploads` jadvali (3 dialekt: postgres/mysql/sqlite): `id, upload_id, owner_id, filename, mime, size, url, preview_url, kind, created_at` + `owner/url` indekslari |
| — | `FileRepo`: `CreateFile/FileByID/FileByURL/FilesByOwner/DeleteFile/IsReferenced` (uploads + xabarlar + avatarlar bo'yicha) |
| Chunk/avatar tugagach iz qolmasdi | Har tugallangan upload + har avatar registry'ga yoziladi (`recordFile`, best-effort) |

### 12.2. Egalik va avtomatik tozalash

| OLDIN | KEYIN |
|---|---|
| Begona user fayliga havola qo'yish mumkin edi (past-risk IDOR) | `claimFile`: registry'da birovning fayli bo'lsa → `403 not_own_file`; yozuv bo'lmasa yuboruvchi nomiga ochiladi |
| Xabar o'chsa fayl diskda qolardi (disk leak) | `cleanupMessageFiles`: boshqa hech kim ishlatmasa registry yozuv + disk (fayl va preview) o'chadi |
| Yetim fayllar (yuklanib, xabarga qo'shilmagan) abadiy yotardi | `StartOrphanGC` (har soatda): 24 soatdan eski + hech kim referens qilmagan fayllar o'chadi; `tmp/` va `backgrounds/` tegilmaydi |
| `Status(id)` egaliksiz edi | `StatusFor(ownerID, id)` |

### 12.3. API

| OLDIN | KEYIN |
|---|---|
| `POST /api/upload`, `GET /api/upload/{id}/status` — xolos | Yangi: `DELETE /api/upload/{id}` (bekor qilish, tmp tozalaydi), `GET /api/files?limit&offset` (Fayllarim), `DELETE /api/files/{id}` (faqat egasi; begona/boshqa → 404) |
| `filename` yo'q/invalid so'rovlar sessiya ochardi | `filename` majburiy, `index>0` da `upload_id` shart, `total` 1–2048 — erta 400 |
| Xato mapping'da `not_own_file` yo'q edi | 403; `security.ErrFileTooBig` → 413 |

### 12.4. Statik serve hardening (`/static/`)

| OLDIN | KEYIN |
|---|---|
| Katalog listing ochiq — `/static/uploads/2026/` dagi nomlarni sanab bo'lardi | `noListDir`: katalog so'rovi 404 |
| Hamma static `no-cache` | `uploads/` → `immutable, max-age=1y` (UUID nomlar) + `X-Robots-Tag: noindex`; qolganiga `no-cache` — tezlik + qidiruv tizimidan yashirish |

### 12.5. Frontend

| OLDIN | KEYIN |
|---|---|
| Progress'da bekor qilish yo'q; tarmoq miltillasa upload o'lardi; 50MB serverda bilinardi | ✕ bekor tugma (server cancel chaqiradi), har chunk'da 1 retry (400ms), client-side pre-check (50MB + kengaytma allowlist) |
| Faqat "📎 → bitta fayl" | Drag&drop (chat maydoniga tashlash) + paste (skrinshot yopishtirish) + 3 tagacha ko'p tanlash |
| `img/video` src validatsiyasiz; yuklab olish linki yo'q | `safeFileURL` (faqat `/static/uploads`), `loading="lazy"`, ⬇ download havolasi |
| Fayllarni boshqarish UI yo'q | **📁 Fayllarim** ekrani (5 ta layout'ning nav'ida): ro'yxat + ochish + ikki bosqichli o'chirish |
| `uploadDone` `js.Func` leak | `Release` bilan |

### 12.6. Yangi testlar

`TestUploadRecordsRegistry` (registry yozuvi), `TestUploadCancel` (bekor + begona rad), `TestSweepOrphans` (yetim o'chadi, referens/yangi qoladi), `TestRenameFile` + `TestRenameFileByURLLegacyClaim`, `TestRefreshGraceToleratesRace` + `TestRefreshOldReuseNukes` — barchasi yashil. E2E (Go skript): register → 3-chunk upload → registry → WS send (real ID) → history → rename (ext saqlanadi) → delete → fayl avto-tozalash (DB+disk) → user cleanup — **E2E-ALL-PASS**.

---

## 14. Musiqa player + viewer + qo'lda yuborish (foydalanuvchi xatolari bo'yicha)

### 14.1. Muammo: musiqa txt'dek ko'rinardi, player yo'q edi
- Sabab: `renderFileMsg` da audio shoxobchasi yo'q edi (faqat image/video).
- Yechim: Telegram-uslub player — ▶/⏸ tugma, bosib o'tkazish (seek), vaqt
  (`0:00 / 2:40`), ⬇ yuklab olish (ilova ichida, `download` atribut),
  ✏️ inline rename; bir vaqtda bitta trek chaladi (boshqasi to'xtaydi).
- Render: `m.Type=="file"` + `.mp3/.wav/.ogg` suffiksi → player; eski xabarlar
  ham avtomatik player bo'ladi (qayta yuborish shart emas).

### 14.2. Muammo: 100% da fayl o'zi yuborilib ketardi
- Sabab: upload tugashi bilan `sendFileMessage` chaqirilardi.
- Yechim: kutish holati — "Tayyor ✓" + **➤ Yuborish** / **✕ Bekor**.
  Yuborilmagan fayl 24 soatda orphan GC tozalaydi. Hech qanday avto-yuborish yo'q.

### 14.3. Fayl xom URL'da ochilardi (tashqi ko'rinish)
- Yechim: in-app media viewer — rasm kattalashadi, video autoplay+controls,
  musiqa player bilan; **‹ ›** oldingi/keyingi, yuklab olish tugmasi,
  Esc/orqa fon bosish — yopish. Rasm bosish yoki ⛶ tugma ochadi.

### 14.4. Fayl nomini o'zgartirish
- `PATCH /api/files/{id}` (Fayllarim) va `PATCH /api/files/by-url` (chat):
  faqat egasi; kengaytma saqlanadi (`qoshiq` → `qoshiq.mp3`); xabar matni ham
  yangilanadi; legacy fayllar (registry'siz) xabar-egalik bilan claim qilinadi.
- UI: chatda ✏️ → inline input (Enter saqlaydi, Esc bekor); Fayllarim'da ✏️.

### 14.5. Refresh poygasi (login ekraniga tushib qolish)
- Sabab: boot + WS reconnect bir xil refresh'ni parallel ishlatdi → reuse
  detektori barcha sessiyani o'chirdi.
- Yechim: server 30s grace (poyga → yangi juftlik, o'g'irlik → nuke) +
  frontend refresh mutex. Ikkala holat test bilan qoplangan.

---

## 13. Arxitektura sifati (v1.1)

### 13.1. Audit xulosasi

Paket grafigi (`go list`) tekshirildi: bog'lanishlar **tashqaridan ichkariga**
(`handler → service → repository → database`; `model/security/config/emoji` leaf) —
sikl va teskari bog'lanish yo'q. `middleware → service.AuthService` faqat
`ValidateAccess` uchun (ichkariga yo'nalish — to'g'ri), arxitektura qoidalari
`ARCHITECTURE.md §2a` va paket `doc.go` larda hujjatlandi.

### 13.2. O'lik kod o'chirildi

| O'chirilgan | Sabab |
|---|---|
| `DeviceRepo.Touch` + impl | 0 chaqiruvchi |
| `SessionRepo.RevokeAllExcept` + impl | 0 chaqiruvchi |
| `middleware.OptionalAuth` | 0 chaqiruvchi |
| `Client.onDisconnect` + `SetOnDisconnect` | `Done()` yagona mexanizm qoldi |
| `var _ = model.RoomGlobal` hack'lar (3 ta) | keraksiz import saqlovchilar |
| `NewRouter` dagi ishlatilmaydigan `repo` param | composition tozalandi |

### 13.3. Composition root (`cmd/server/main.go`)

`main()` dan `run(ctx) error` ajratildi: wiring + signal + graceful shutdown
alohida; xatoda defer'lar (`db.Close`, `svc.Stop`, `StopGC`) kafolatli ishlaydi;
qaytishda child cancel — ctx-bog'langan fon ishlar oqmaydi.

### 13.4. Lifecycle simmetriya

`Presence.Stop`, `Background.Stop` endi idempotent (`sync.Once`;
`ChatService`, `Router.StopGC`, `PeriodicGC.Stop` da bor edi).
`Services.Start/Stop` tartibi hujjatlandi (§20).

### 13.5. Fayl o'lchami

`auth_service.go` 799 qator → 4 mavzuli fayl: `auth_service` (381, core),
`auth_directory` (204, search/conversations/profile), `auth_device` (154),
`auth_pending` (96). API o'zgarmadi — faqat ko'chirish.

### 13.6. Hujjat sinxroni

`ARCHITECTURE.md` v1.1: sqlite dialekt, `uploads` jadvali, persist-then-broadcast,
fail-open dedup, files API, WS revocation, hash-routing, JWT-32, real fayl daraxti
(`web/src` flat, `files.go`, `boot.js`), kutubxonalar (`imaging`, sqlite;
`bimg`/`lib/pq`/`dialect.go` olib tashlandi — ular kodda yo'q), to'liq API ro'yxat,
ishonchli `build.ps1 -Release` yo'riqnomasi, §20 lifecycle/konvensiyalar.
Har paketda `doc.go` (qatlam + qoidalar).

---

## ⚠️ Migratsiya eslatmalari (muhim)

1. **JWT_SECRET 16–31 belgi bo'lsa** — 32+ belgiga uzaytirish shart (yangi talab, aks holda server ishga tushmaydi).
2. **`.env` git tarixida qolgan** (`31588c2`) — `git filter-repo` bilan tozalab, `JWT_SECRET` va `DB_PASSWORD` ni almashtiring. Bu kod bilan emas, tarix bilan bog'liq — qo'lda qilinadi.
3. ~~**Keyingi bosqich**: TOTP secret at-rest shifrlash (AES-GCM)~~ — **bajarildi** (§15 H5: `repository/secretcrypt.go`, legacy-plaintext fallback bilan).
4. **JWT_SECRET almashsa** — TOTP secret'lar eski kalitda qoladi (fail-closed, 2FA login ishlamaydi). Kalit almashtirishdan oldin userlarga 2FA'ni o'chirib-yoqishni ayting yoki DB'dagi `twofa_secret` (`v1.` prefiksli) yozuvlarni qayta generatsiya qiling.

---

## 15. Backend + xavfsizlik chuqur audit (2026-09-24)

3 yo'nalish, 47 topilma (High 7, Medium 13, Low 17, Info 10). Real nosozliklar fix qilindi, dizayn qarorlari hujjatlandi.

### Auth/sessiya (High)
- **Refresh reuse-grace oilani saqlab qolardi** → grace ichidagi qayta ishlatish ham `RevokeAllForUser` + yangi juftlik (`auth_service.go` Refresh). Test: `TestRefreshGraceReuseNukesButMints`.
- **2FA brute-force fresh-pending bilan** (login→5 kod→login...) → per-akkaunt limit (20/15min, `auth_pending.go` user2FAFails). Test: `TestTwoFAUserLevelLockout`.
- **TOTP replay** (bir kod ikki marta) → 100s used-code kesh. Test: `TestTOTPReplayRejected`.
- **Login per-account throttle yo'q** → `loginLimiter` 10/min (`ErrRateLimited` → 429). Test: `TestLoginPerUserThrottle`.
- **Terminate qilingan device qayta jonlanardi** → inactive device'lar reaktivatsiya qilinmaydi (`auth_device.go`).
- **Rotation rollback yo'q** → eski revoke xatoda yangi sessiya ham revoke (`issueTokensWithSession`).
- **TOTP plaintext** → AES-256-GCM at-rest (`repository/secretcrypt.go`, kalit JWT secret'dan; legacy fallback; buzilgan yozuv fail-closed).

### Upload/fayl (High + Medium)
- **H1 `IsReferenced` placeholder mismatch** (sqlite'da jonli fayllar o'chib ketardi) → argumentlar to'g'irlandi + `ErrNoRows` dan boshqa xato qaytariladi (`file_repo.go`). Test: `TestIsReferencedSQLite` (real sqlite).
- **M3 WAV slice panic** (4–11 bayt `RIFF`) → `len>=12` guard; **M15 MP3 frame-sync** hech qachon teng emasdi → `b[0]==0xFF && b[1]&0xE0==0xE0` (`upload_service.go` sniffMime). Test: `TestSniffMime` keyslar.
- **M1/M2 piksel cap** (decompression bomb): saqlashda 12000px/50MP, avatarda 4000px/16MP (header-only `DecodeConfig`).
- **M5 preview semafor** (2 slot, bo'sh bo'lmasa skip), **M4 sessiya kvota** (5/owner + eng eski eviction, `ErrTooManyUploads` → 429).
- **M6 phantom registry** (diskda yo'q URL ga yozuv) → `claimFile` disk tekshiradi; **M8 DB mime ishonchi** → `SaveFileToMy` har doim diskdan sniff; **M11 avatar delete** → `avatar_url` tozalanadi.

### WS/DB (Medium + Low)
- **F3 typing / F15 deleted / F4 history** blok filtrisiz edi → `SkipBlockedBy`/`BlockedByViewer` bilan bir xil semantika (`chat_service.go`, `ws_handler.go`).
- **F2 presence** blok va `show_online_status`'ni hurmat qilmaydi → `AnnouncePresenceFiltered` + `OnlineUsersFor` + 60s pref kesh (`presence_service.go`, `hub.go`). Testlar: `TestPresenceRespectsBlocks`, `TestPresenceRespectsShowOnline`.
- **F8 N+1 reply** → `ByIDs` batch + `ToPayloads` (`message_repo.go`, `chat_service.go`). Test: `TestByIDsBatch`.
- **F9 offset cap** (10000), **F10 limiter fail-closed** → eng eski bucket eviction, **F12 origin scheme** (faqat http/https), **F6 noma'lum freym limiter** (20/min).

### Dizayn qarorlari (kod o'zgarmadi)
- **F1 xona ACL yo'q** — ataylab: xonalar ochiq (IRC-uslub, shareable `#/chat/<room>`), DM'lar qat'iy (`IsDMRoomMember`). ACL qo'shish mahsulotni buzadi.
- **M4 account ban ustuni yo'q** — admin roli yo'q (kim ban qiladi?); buzilgan akkaunt uchun: parol reset + `RevokeAllForUser` + device terminate (mavjud).
- **H2 access-JWT 15min oyna** (logout'dan keyin): WS `CheckWSAccess` bilan yopilgan; HTTP blacklist request başına DB o'qishni talab qiladi (perf) — qabul qilingan trade-off.
- **H6 localStorage token** — XSS bo'lsa o'g'irlanadi; mitigatsiya: CSP (`script-src 'self'`), `nosniff`, Telegram-uslub inline yo'q. HttpOnly cookie SPA/WASM oqimini buzadi — qabul qilingan.
- **L1 register 409 enumeration** — standart UX; login generic (`invalid_credentials` + dummy bcrypt).

## ✅ Verifikatsiya natijasi

```
go vet ./...   → toza
go build ./... → toza
go test ./...  → 7 paket ok (config, emoji, handler, middleware, model, security, service, ws)
WASM (GOOS=js GOARCH=wasm, -ldflags "-s -w") → yig'iladi
gofmt → tozalandi
```
