# KRADCHAT — To'liq Arxitektura

> Author: **Tohirbek Rahmatullayev**
> Version: 1.1 (2026-09-23)
> Status: Implementation
> Dizayn: `kradchat_design.md` · Logo: `kradchat.png`
> Reviziya tarixi: v1.1 — kod bilan sinxronlash (sqlite, persist-then-broadcast,
> uploads registry, files API, WS revocation, hash-routing, JWT-32, qatlam qoidalari).
> Oldingi tahlil va optimizatsiyalar: `OPTIMIZATSIYA.md`.

---

## 1. Maqsad

KRADCHAT — real-time (WebSocket) xavfsiz chat platformasi. Backend Go,
frontend Go→WASM, baza PostgreSQL/MySQL/**SQLite** (dialekt abstraktsiyasi orqali;
joriy dev/default — SQLite, prod-scale tavsiya — PostgreSQL).
Maqsad: <100ms xabar yetkazish, juda yuqori xavfsizlik, **yo'qolmaydigan**
xabarlar (**persist-then-broadcast**), to'liq media yordam (rasm/video),
2FA, qurilma tracking, emoji (iOS uslub), chat fonlari, @username/$id mention,
fayl registry va "Fayllarim" boshqaruvi.

---

## 2. Qatlamlar (Layered Architecture)

```
Request (REST/WS)
      │
      ▼
┌───────────────────┐
│ Handler (HTTP/WS) │  -- tekshiradi, DTO, JSON
└───────────────────┘
      │
      ▼
┌───────────────────┐
│ Service (Biznes)  │  -- logika, 2FA, upload, chat broadcast
└───────────────────┘
      │
      ▼
┌───────────────────┐
│ Repository (DB)   │  -- PostgreSQL/MySQL/MariaDB (dialekt interfeysi)
└───────────────────┘
      │
      ▼
┌───────────────────┐
│ Database (Pool)   │  -- pgx / go-sql-driver
└───────────────────┘
```

Qo'shimcha (mustaqil):
- `internal/ws` — Hub/Client/Room (in-memory, Redis siz)
- `internal/security` — sanitizer, validator, dedup, flood
- `internal/emoji` — emoji metadata (faqat WASM frontend ishlatadi)
- `web/src` — frontend Go manbasi (GOOS=js GOARCH=wasm)
- Har paketda `doc.go` — qatlam mas'uliyati va bog'lanish qoidalari (20-bo'lim).

---

## 2a. Qatlam qoidalari (majburiy)

```
Tashqi                                     Ichki
handler → service → repository → database
   ↓          ↓           ↓
middleware  ws (bus)   config/model/security/emoji (leaf)
```

1. Bog'lanish faqat **tashqaridan ichkariga**. Teskari yo'nalish
   (service→handler, repository→service) taqiqlanadi.
2. `handler` repository'ga biznes chaqiruv qilmaydi — faqat xato-sentinel
   mapping (`writeServiceErr`). `NewRouter` repo qabul qilmaydi.
3. `model/security/config/emoji` — leaf: ichki paket import qilmaydi.
4. `ws.Hub` — on-process message bus (shared kernel istisno); klasterda almashtiriladi.
5. `main.go` — yagona composition root (`run(ctx)`); Stop'lar idempotent.
6. Yangi qo'l yozma fayl >400 qator bo'lsa — mavzuga bo'linadi
   (namuna: `auth_service` → `auth_service/pending/device/directory`).

---

## 3. Loyiha tuzilishi

```
kradchat/
├── ARCHITECTURE.md
├── kradchat_design.md
├── kradchat.png                    ← logo (asl)
├── go.mod / go.sum
├── .env.example / .env
├── cmd/
│   └── server/
│       └── main.go                 ← kirish nuqtasi (composition root: run(ctx))
├── internal/
│   ├── config/config.go + doc.go   ← .env + dialekt (JWT min 32)
│   ├── database/
│   │   ├── database.go + doc.go    ← ulanish, pool, retry
│   │   ├── migrate.go              ← dialekt bo'yicha migratsiya (3 schema)
│   │   └── migrations/
│   │       ├── postgres.sql
│   │       └── mysql.sql
│   ├── model/ (+ doc.go)
│   │   ├── user.go
│   │   ├── message.go
│   │   ├── device.go
│   │   ├── session.go
│   │   ├── twofa.go
│   │   ├── background.go
│   │   ├── file.go                 ← uploads registry yozuvi
│   │   └── dto.go                  ← API DTO
│   ├── repository/ (+ doc.go)
│   │   ├── repository.go           ← interfeyslar (+ FileRepo)
│   │   ├── user_repo.go
│   │   ├── message_repo.go
│   │   ├── device_repo.go
│   │   ├── session_repo.go
│   │   ├── twofa_repo.go
│   │   ├── file_repo.go            ← uploads registry
│   │   ├── background_repo.go
│   │   └── scantime.go             ← driver vaqt farqlari (ScanTime)
│   ├── security/
│   │   ├── sanitizer.go            ← XSS tozalash
│   │   ├── validator.go            ← input validatsiya
│   │   ├── dedup.go                ← takrorlanmas xabar
│   │   └── flood.go                ← token bucket
│   ├── service/ (+ doc.go)
│   │   ├── auth_service.go         ← register/login/refresh (core)
│   │   ├── auth_pending.go         ← 2FA pending store
│   │   ├── auth_device.go          ← device tracking
│   │   ├── auth_directory.go       ← search/conversations/profile
│   │   ├── twofa_service.go
│   │   ├── chat_service.go         ← persist-then-broadcast + claimFile
│   │   ├── upload_service.go       ← chunked + registry + orphan GC
│   │   ├── background_service.go
│   │   └── presence_service.go     ← online status + devices (copy-on-write)
│   ├── handler/ (+ doc.go)
│   │   ├── auth_handler.go         ← register/login/2FA/password/devices/avatar
│   │   ├── upload_handler.go       ← chunk upload + cancel + files CRUD
│   │   ├── block_handler.go        ← bloklash
│   │   ├── background_handler.go
│   │   ├── ws_handler.go           ← WS + revocation + rate-limit
│   │   └── router.go               ← composition (static hardening included)
│   ├── middleware/ (+ doc.go)
│   │   ├── auth_mw.go              ← JWT access (Bearer; /ws query)
│   │   ├── ratelimit_mw.go         ← IP bucket + PeriodicGC
│   │   ├── security_mw.go          ← headers, CORS, origin (+HSTS)
│   │   └── logging_mw.go           ← access log + ClientIP (trusted proxy)
│   └── ws/ (+ doc.go)
│       ├── hub.go                  ← in-memory xonalar (+room GC, byDevice index)
│       ├── client.go
│       ├── room.go
│       ├── protocol.go             ← xabar turlari
│       └── message.go
├── migrations/                     ← root manba SQL (documentation)
│   └── 001_init.sql
├── build.ps1 / build.sh           ← backend + WASM build (`-Release`: `-s -w`)
├── web/
│   ├── index.html                  ← SPA shell (toast aria-live)
│   ├── wasm/
│   │   ├── app.wasm                ← GOOS=js GOARCH=wasm build (`?v=` cache-bust)
│   │   ├── wasm_exec.js            ← GOROOT'dan nusxa
│   │   └── boot.js                 ← yuklovchi (textContent xato ko'rsatish)
│   ├── src/                        ← FRONTEND GO MANBASI (flat package main)
│   │   ├── main.go                 ← boot + forceLogin + hash-tiklash
│   │   ├── dom.go                  ← syscall/js o'ram (setAttribute, textContent)
│   │   ├── api.go                  ← fetch + refresh retry + session
│   │   ├── ws.go                   ← reconnect + offline navbat + refresh
│   │   ├── store.go                ← global holat (CurRoom hash'ga sinxron)
│   │   ├── auth.go / screens.go    ← login/register/2FA/settings/devices/backgrounds
│   │   ├── files.go                ← Fayllarim ekrani
│   │   ├── chat.go                 ← xona, xabarlar, composer (XSS-escape)
│   │   ├── conversations.go / search.go / profile.go
│   │   ├── upload.go               ← chunked + cancel/retry + drop/paste
│   │   ├── emoji_picker.go / notifications.go / toast.go
│   └── static/
│       ├── style.css               ← dizayn tizimi (v2.0)
│       ├── logo.png
│       ├── backgrounds/            ← auto-scan (kategoriya = papka nomi)
│       └── uploads/                ← UUID fayllar (listing 404, immutable kesh)
│           ├── 2026/ / avatars/ / tmp/ (bloklangan)
└── OPTIMIZATSIYA.md                ← optimizatsiya tarixi (OLDIN → KEYIN)
```

---

## 4. Database

### 4.1 Dialektlar

Interfeys: `repository.Repositories` (interfeyslar to'plami).
To'rt dialekt to'liq qo'llab-quvvatlanadi:

| Dialekt | Driver | UUID | JSON | Tavsiya |
|---------|--------|------|------|---------|
| postgres | `jackc/pgx/v5` | `gen_random_uuid()` | `jsonb` | **Asosiy** — katta data (MVCC, partition, GIN) |
| mysql | `go-sql-driver/mysql` | `UUID()` (client) | `json` | MariaDB bilan mos |
| mariadb | `go-sql-driver/mysql` | `UUID()` (client) | `json` | mysql aliastan |
| sqlite | `modernc.org/sqlite` | `uuid.NewString()` | `json` (TEXT) | **Dev/default** — WAL + busy_timeout |

`.env` → `DB_DRIVER=postgres|mysql|mariadb|sqlite`.

### 4.2 Jadvallar (PostgreSQL ko'rinishi)

**users**
- id UUID PK · username UNIQUE · display_name · password_hash
- avatar_url · hide_username BOOL · show_last_seen BOOL · show_online_status BOOL
- notifications_enabled BOOL · twofa_enabled BOOL · twofa_secret TEXT (enc)
- last_seen TIMESTAMPTZ · created_at TIMESTAMPTZ

**messages** (partitionable)
- id BIGSERIAL PK · user_id FK · room · content TEXT · msg_type
- file_url · preview_url · file_size · client_msg_id UUID UNIQUE (DEDUP)
- reply_to · mentions UUID[] · created_at TIMESTAMPTZ

**devices**
- id · user_id FK · ip INET · user_agent · device_type · browser · os
- fingerprint · is_active · last_seen

**sessions**
- id · user_id FK · device_id FK · refresh_hash · ip · user_agent
- expires_at · revoked_at

**password_change_log**
- id · user_id · ip · user_agent · created_at

**chat_backgrounds**
- id · name · category · file_path UNIQUE · is_active · created_at

**user_backgrounds**
- user_id · background_id · room · applied_at (PK: user_id, room)

**uploads** (fayl registry — egalik, Fayllarim, o'chirish, orphan GC)
- id UUID PK · upload_id · owner_id FK · filename · mime · size
- url · preview_url · kind (chat|avatar) · created_at

**Indexes**: room+created_at DESC, client_msg_id UNIQUE, devices.user_id, sessions.user_id,
uploads.owner_id, uploads.url.

### 4.3 Migratsiyalar

`migrate.go` ichidagi 3 schema-string (postgres/mysql/sqlite).
Har startda idempotent: CREATE TABLE IF NOT EXISTS; eski bazalarga
`ensureColumns` orqali yetkaziladi. Versiyalash yo'q — yangi jadval
qo'shilsa 3 schema'ga birdan yoziladi.

---

## 5. Auth

### 5.1 Register
POST `/api/auth/register` {username, password, display_name}
- username: 3–50, `[a-zA-Z0-9_.]`; password: 8–72, bcrypt(cost=12)
- javob: 201 + JWT access + refresh (avto-login)

### 5.2 Login
POST `/api/auth/login` {username, password}
- haqiqiy → `{status:"ok"|"2fa_required", pending_session}`
- 2FA yoqilgan bo'lsa: `status:"2fa_required"` + pending_session (15 daq).
  Device verify bosqichida yaratiladi (device-exhaustion DoS oldi).
- aks holda: device (UA+IP fingerprint) + JWT beriladi

### 5.3 2FA verify
POST `/api/auth/twofa/verify` {pending_session, code}
- TOTP verify (30s window, ±1 drift) → JWT beriladi

### 5.4 Refresh
POST `/api/auth/refresh` → yangi access+refresh.
Rotation tartibi: AVVAL yangi session → KEYIN eski revoke (crash'da logout yo'q).
O'chirilgan device'dagi session jonlanmaydi. Reuse'da butun oila revoke.

### 5.5 Password change
POST `/api/auth/password` (auth) {old_password, new_password}
- parol o'zgarishi → `password_change_log` + barcha sessions revoke +
  WS broadcast `force_logout` → barcha qurilmalarda <1s chiqish
- WS revocation: parol o'zgarishidan OLDIN berilgan access token bilan
  qayta ulanish rad (`CheckWSAccess` + `LatestPasswordChange`); terminated
  device bilan ham ulanib bo'lmaydi

### 5.6 2FA boshqaruv
- POST `/api/auth/twofa/setup` (auth) → {secret, otpauth_uri, qr_data_url}
- POST `/api/auth/twofa/enable` {code} → yoqiladi
- POST `/api/auth/twofa/disable` {password, code} → o'chiriladi

### 5.7 JWT
- Access: 15 daq (HS256, secret env **min 32 belgi**), Refresh: 7 kun (hash DB da)
- Claims: `sub` (user_id), `name`, `dev` (device_id), `iat`, `exp`
- `middleware.Auth` faqat `ValidateAccess` interfeysiga tayanadi

### 5.8 Sessions/qurilmalar
- Har login → device record (IP, UA, fingerprint) + session
- GET `/api/devices` — ro'yxat; DELETE `/api/devices/{id}` — tugatish (+WS kick)

---

## 6. 2FA (TOTP — RFC 6238)

Kutubxona: `github.com/pquerna/otp/totp`.
- Secret: 32 bayt random, Base32. QR: `otpauth://totp/KRADCHAT:username?secret=...&issuer=KRADCHAT`
- Verify: `totp.Validate(code, secret)` — server taymer bilan.
- Yoqishda muvaffaqiyatli 2 marta verify talab (sozlamalardagi katolikni oldini olish).

---

## 7. WebSocket (in-memory, Redis siz)

### 7.1 Komponentlar
- **Hub**: `rooms map[string]*Room` (bo'sh non-global xonalar GC),
  `users map[UserID]map[*Client]`, `byDevice` O(1) index.
- **Room**: `clients` set, snapshot-broadcast (lock ushlanmaydi).
- **Client**: `conn`, `send chan []byte` (buffered 256), ping/pong,
  read/write pump; tozalash yagona mexanizm — `Done()` kanali.

### 7.2 Latency <100ms (persist-then-broadcast)
- Xabar: validatsiya → **sync DB Insert (3s timeout)** → real ID bilan broadcast →
  har client `send` chan → write pump → socket. DB yozish 5–20ms.
- Fallback: DB o'lik bo'lsagina async batch (best-effort).
- Reconnect: `?token=` + revocation tekshiruvi → dedup `client_msg_id` uchun
  server xabarni DB dan tekshirib `idempotent` javob beradi.

### 7.3 Protokol (JSON)

Client→Server:
```
{type:"auth", token}
{type:"msg", room, content, client_msg_id, msg_type, reply_to, mentions[]}
{type:"typing", room, on}
{type:"bg", room, background_id}
```

Server→Client:
```
{type:"ready", user, rooms, online}
{type:"msg", message{...}, idempotent:bool}
{type:"history", room, messages[], cursor}
{type:"online", users:[{id,username,devices,last_seen}]}
{type:"presence", user_id, online, devices}
{type:"typing", room, user, on}
{type:"bg_list", backgrounds[]}
{type:"bg_set", room, background_id}
{type:"notif", type, title, body}
{type:"force_logout", reason}
{type:"kick", device_id}
{type:"error", code, message}
```

### 7.4 Dedup (1 xabar 1 marta)
- Client har xabar uchun `client_msg_id` (UUID v4) generatsiya qiladi.
- Server: in-memory `DedupStore` (fail-open: to'lganda eng eskilar evict) +
  DB UNIQUE ustun; konflikt → idempotent `duplicate` javob.
- Xabar real ID bilan broadcast qilinadi; saqlashdan keyin `ack` keladi
  (optimistik node ID bog'lashi uchun).

### 7.5 Flood himoya
- Per-user token bucket: burst 10 (`WS_MSG_BURST` ishlaydi) + 60 msg/min refill;
  typing/history/delete/bg alohida limitlar + daqiqalik GC.
- Katta content (64KB, `WS_MAX_MESSAGE_SIZE` config) blok. Slow consumer uziladi.

---

## 8. Xavfsizlik (yuqori daraja)

1. **Rate limit**: IP (token bucket, 120 req/min; static istisno) + user (WS).
2. **XSS**: `sanitizer.Clean()` — allowlist + code-block escape; frontend
   `textContent` + `html.EscapeString` (chat header, 2FA); `boot.js` textContent.
3. **Code block**: ` ``` ` bloklar `pre > code` da monospace (eskap bilan).
4. **SQL injection**: parameterized query, hech qachon string interp.
5. **Fayl yuklash**: magic bytes whitelist, hajm ≤50MB, stream merge (OOM yo'q),
   atomik chunk, preview guard (8000px/30MP, `imaging` pure-Go), egalik
   (`claimFile` → 403), registry + orphan GC.
6. **Static serve**: katalog listing 404, `uploads/` immutable + noindex,
   xavfli kengaytma `attachment`, `tmp/` bloklangan.
7. **Headers**: nosniff, DENY, no-referrer, **HSTS**, CSP (img-src 'self' data: blob:),
   COOP/COEP, `Vary: Origin` (deprecated X-XSS-Protection olib tashlangan).
8. **CORS**: origin allowlist + host-moslik (LAN ishlaydi), credentials strict, OPTIONS preflight.
9. **CSRF**: token body/header'da (cookieauth yo'q); WS token faqat `/ws` query'da.
10. **Parol**: bcrypt cost=12 (10–15 clamp); kuchlilik tekshiruvi.
11. **Session hijack**: refresh rotation (create-before-revoke), reuse'da oila revoke,
    device fingerprint (UA+IP/24), terminated device + password-change revocation.
12. **Logging**: access log (URL query yozilmaydi — token sizmasi yo'q), `LOG_LEVEL` ishlaydi.

---

## 9. Media (upload + files)

- POST `/api/upload?...` (auth, `limitBody 3MB`): raw chunk; 1-chunk `filename`+`total`
  shart; keyingi chunklar `upload_id` shart. Server `uploads/tmp/<id>/` ga atomik
  yig'adi (tmp+rename, `seen` faqat muvaffaqiyatdan keyin); oxirgi chunkda:
  stream merge → magic bytes → preview → `uploads/<YYYY>/<uuid>.<ext>`.
- Javob: `{url, preview_url, size, mime, upload_id, complete, progress}`.
- Har tugallangan fayl `uploads` registry'ga yoziladi (owner, kind chat|avatar).
- `DELETE /api/upload/{id}` — bekor qilish; `GET /api/files` — Fayllarim;
  `DELETE /api/files/{id}` — faqat egasi o'chiradi (disk + DB).
- Xabar o'chsa fayli ham ketadi (referenssiz bo'lsa); yetim fayllar soatlik GC.
- Client: dumaloq progress + ✕ bekor + retry + pre-check (50MB/ext);
  drag&drop/paste; offline navbat (typing'dan tashqari, cap 50).

---

## 10. Emoji (iOS uslub)

- `internal/emoji/emoji.go` — metadata `{name, category, unicode, svg}`,
  qidiruv (`Search(q)`), shortcodes. Backendda ishlatilmaydi — faqat WASM.
- Frontend `emoji_picker.go` — kategoriya tablar + qidiruv (120 limit).

---

## 11. Chat fonlari (auto-scan)

- `background_service` har 30s (`BACKGROUND_POLL`) `web/static/backgrounds/**` skanerlaydi.
- Yangi fayl topilsa → DB `chat_backgrounds` insert, kategoriya katalog nomidan,
  WS `bg_list` global xonaga **bir marta** broadcast qilinadi.
- User: `backgrounds → tanlash` → `{type:"bg", room, id}` → `user_backgrounds`;
  qayta ulanganda restore. `"none"` — o'chirish.

---

## 12. Presence / Qurilmalar

- WS ulanishda: `MarkOnline` → snapshot (copy-on-write, race yo'q).
- Uzilganda: avval `hub.Disconnect` (to'g'ri count), keyin `MarkOffline`;
  boshqa qurilma qolsa faqat devices kamayadi; oxirgisi uzilsa `last_seen` DB ga.
- `ready` — onlayn ro'yxat + fon katalogi; presence o'zgarishlar 150ms batch.
- Devices ekrani: ro'yxat (type, browser, OS, IP, last_seen) + terminate
  (DB deactivate + session revoke + WS `kick`).

---

## 13. Sozlamalar

| Sozlama | Joy | Ta'sir |
|---------|-----|--------|
| Notifications | settings | WS notif yuborilmaydi (server filter) |
| Last seen | settings | boshqalarga ko'rinmaydi |
| Online status | settings | presence ro'yxatda yashirin |
| Hide username | settings | faqat `$id` bilan topiladi |
| Background | settings | har room uchun |
| 2FA | settings | enable/disable |
| Password | settings | o'zgartiradi → force logout |

---

## 14. Frontend (Go → WASM)

- `GOOS=js GOARCH=wasm go build -ldflags "-s -w" -o web/wasm/app.wasm ./web/src`
- `wasm_exec.js` — GOROOT'dan nusxa; `boot.js` — yuklovchi (`?v=` cache-bust).
- `dom.go` — `syscall/js` o'ram: `Q/El/SetText(textContent)/Attr(setAttribute)/On`.
- `store.go` — global holat (token, user, rooms, online, bg, `CurRoom`).
- `ws.go` — reconnect (exp backoff, cap 15s) + token refresh + offline navbat.
- Screens: login → twofa → chat → search/files/backgrounds/devices/settings.
- Routing: `location.hash` (`#/chat/<room>`) — reload'da tiklanadi, share bo'ladi.
- Media viewer (`viewer.go`): rasm/video/musiqa ilova ichida ochiladi
  (xom static URL o'rniga); ‹ › navigatsiya, Esc/yubka bilan yopish, yuklab olish.
- Audio player: Telegram-uslub (play/pause, seek, vaqt, yuklab olish, inline rename).
- Upload: 100% da AVTO-yuborish yo'q — kutish holati (➤ Yuborish / ✕ Bekor).
- Xavfsizlik: `textContent` izchil, header interpolatsiya escape, `safeFileURL`,
  token `localStorage` (XSS = session-o'g'irlash — shuning uchun XSS qat'iy).

### 14.1 Dizayn integratsiyasi (kradchat_design.md)
- Tokens → CSS `:root` (yuqoridagi jadval).
- Glass Card, Gradient, Shadow — kartalar/butonlar.
- Orbitron (logo/display), Inter (body), Oxanium (subhead).
- Login hero: logo + "Talk to the Future" + CTA + aurora/grid/particle bg.
- Animatsiyalar 200–300ms ease-out; glow pulse; logo glitch (loading).
- Radius: xs8 sm12 md16 lg20.

---

## 15. Kutubxonalar

| Kutubxona | Qaerda |
|-----------|--------|
| gorilla/websocket | ws (arxivlangan upstream — kelajakda coder/websocket) |
| golang-jwt/jwt/v5 | auth |
| x/crypto/bcrypt | auth |
| jackc/pgx/v5 | postgres repo |
| go-sql-driver/mysql | mysql/mariadb repo + DSN formatlash |
| modernc.org/sqlite | sqlite repo (pure-Go) |
| pquerna/otp | twofa (TOTP+QR) |
| google/uuid | client_msg_id, id |
| joho/godotenv | config |
| disintegration/imaging | rasm preview (pure-Go) |
| skip2/go-qrcode | 2FA QR |

`database/sql` ustidan uchala driver bitta API orqali; placeholder'lar
`mysqlToPostgres` bilan konvertatsiya qilinadi.

---

## 16. API ro'yxati

```
POST /api/auth/register
POST /api/auth/login
POST /api/auth/twofa/verify
POST /api/auth/refresh
POST /api/auth/password
POST /api/auth/logout
POST /api/auth/twofa/setup        (auth)
POST /api/auth/twofa/enable       (auth)
POST /api/auth/twofa/disable      (auth)
GET  /api/me                      (auth)
PATCH /api/me                     (auth)
POST /api/me/avatar               (auth, 6MB)
GET  /api/users/search?q=         (auth)
GET  /api/conversations           (auth)
POST /api/users/{id}/block        (auth)
DELETE /api/users/{id}/block      (auth)
GET  /api/devices                 (auth)
DELETE /api/devices/{id}          (auth)
GET  /api/backgrounds             (auth)
POST /api/backgrounds/set         (auth)
POST /api/upload                  (auth, chunk, 3MB body)
GET  /api/upload/{id}/status      (auth)
DELETE /api/upload/{id}           (auth, bekor qilish)
GET  /api/files?limit&offset      (auth, Fayllarim)
DELETE /api/files/{id}            (auth, faqat egasi)
PATCH /api/files/{id}             (auth, rename — Fayllarim)
PATCH /api/files/by-url           (auth, rename — chat)
WS   /ws?token=...                (revocation + rate-limit)
GET  /                             → index.html (SPA fallback)
GET  /wasm/*                       → no-cache frontend modul
GET  /static/*                     → uploads immutable+noindex, listing 404
```

---

## 17. Ishga tushirish

```bash
# 1) .env sozlash (DB_DRIVER=sqlite|postgres|mysql, JWT_SECRET min 32 belgi)
# 2) Agar postgres: CREATE DATABASE kradchat;
# 3) To'liq build (backend + WASM Release)
powershell -File build.ps1 -Release   # Windows
./build.sh                            # Linux/macOS (ichida -s -w)
# 4) Ishga tushirish
./kradchat.exe                        # yoki ./kradchat
# 5) http://localhost:8080
```

---

## 18. Test strategiyasi

- `internal/security`: sanitizer/validator/dedup/flood unit testlari.
- `internal/service`: upload (registry/cancel/orphan GC — stub FileRepo),
  twofa flow; auth device/pending mantiq.
- `internal/ws`: hub/room test (fake conn); `internal/handler`: ws lifecycle.
- `internal/config/middleware/model/emoji`: unit testlar.
- WASM: kompilyatsiya tekshiruvi (`GOOS=js GOARCH=wasm`, `-s -w`).
- E2E: DB o'rnatilganda — register→login→ws msg→presence.

---

## 19. Migratsiya yo'li (kelajak)

- `messages` partition (oylik) postgres; MySQL `PARTITION BY RANGE`.
- Redis o'rniga in-memory + future cluster: `hub` ni `raft` holatga o'tkazish
  (`Broadcaster` interfeysi orqali — hozir concrete `*ws.Hub`).
- 2FA recovery codes (10× hashed single-use), TOTP secret at-rest shifrlash (AES-GCM).
- `backgrounds` fayl watch (fsnotify) 30s poll o'rniga.
- `gorilla/websocket` → `coder/websocket` (upstream arxivlangan).
- Multi-instance: pending-2FA va presence uchun markaziy store (Redis).

---

## 20. Lifecycle va konvensiyalar

```
main.run(ctx)
 ├── svc.Start(ctx)      → Background, Presence, Blocks, Upload GC (fon goroutinalar)
 ├── router.StartGC()    → rate limiter + WS limiter tozalash
 └── defer teskari tartibda: router.StopGC → svc.Stop → db.Close
```

- Barcha `Stop()` idempotent (`sync.Once` yoki guard): double-stop panic yo'q.
- ctx-bog'langan fon ishlar `ctx.Done()` da to'xtaydi; kanal-bog'langanlar
  `Stop()` da. `run()` qaytishda child cancel chaqiriladi.
- Xato arxitekturasi: sentinel error'lar service/repository'da;
  HTTP mapping faqat `writeServiceErr` da markazlashgan.
- Konvensiya: qo'l yozma fayl ~400 qatordan oshsa mavzuga bo'linadi;
  har paketda `doc.go` (qatlam + qoidalar) majburiy.
