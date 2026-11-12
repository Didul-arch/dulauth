# PRD — dul-auth

| | |
|---|---|
| **Status** | Draft v0.1 |
| **Tanggal** | 12 November 2026 |
| **Pemilik** | didul |
| **Repo** | `dul-auth` (terpisah dari `my-porto-gw`) |
| **Domain target** | `auth.didul.site` |

---

## 1. Ringkasan

dul-auth adalah **identity provider** milik sendiri yang ditulis dengan Go dan berbicara protokol **OpenID Connect (OIDC)**. Fungsinya mirip "Login with Google", tapi versi pribadi. Aplikasi lain (porto sekarang, cuit nanti) tidak menyimpan password. Mereka cukup mengarahkan user ke dul-auth, lalu menerima token yang membuktikan siapa user tersebut.

Tahap pertama (MVP) hanya punya satu pengguna (admin) dan satu aplikasi klien (porto). Tujuannya mengunci dashboard `/dudul` dan endpoint yang mengubah data.

## 2. Latar belakang

- Porto sudah punya dashboard admin `/dudul` dan endpoint `POST`/`PUT`/`DELETE` untuk projects dan blogs. Semuanya **tanpa autentikasi**, jadi belum aman di-deploy publik.
- Akan ada service lain (cuit, mini twitter) yang memakai **user yang sama**. Kalau login dibuat di dalam porto, cuit harus menduplikasinya atau bergantung pada porto.
- Memisahkan identitas ke service sendiri adalah batas domain yang jelas: "siapa kamu" terpisah dari "apa yang kamu tulis".

## 3. Tujuan dan non-tujuan

### Tujuan (MVP)

1. Admin bisa login ke porto lewat dul-auth dengan email dan password.
2. `/dudul` dan semua endpoint mutasi di porto menolak request tanpa sesi admin yang valid.
3. dul-auth mengikuti OIDC standar (discovery, authorization code + PKCE, ID token, JWKS), sehingga client berikutnya cukup didaftarkan tanpa mengubah kode dul-auth.
4. Protokol dan kriptografi ditangani library tersertifikasi, bukan ditulis sendiri.

### Non-tujuan (MVP)

- Pendaftaran user publik
- Login dengan GitHub/Google
- Refresh token
- Halaman consent ("izinkan aplikasi X mengakses...")
- Pendaftaran client lewat UI atau API
- Rotasi signing key otomatis
- MFA/2FA
- Reset password lewat email
- SSO session lintas aplikasi (login sekali untuk porto dan cuit)

Semua di atas tercatat di [§13 Rencana lanjutan](#13-rencana-lanjutan).

## 4. Pengguna

| Persona | Kebutuhan |
|---|---|
| **Admin (didul)** | Login sekali, mengelola projects/blogs di `/dudul`, logout. |
| **Pengunjung porto** | Tidak terdampak. Halaman publik tetap terbuka tanpa login. |
| **Developer service baru (cuit)** | Mendaftarkan client baru dan memverifikasi token tanpa mengubah dul-auth. |

## 5. User stories

- **US-1** Sebagai admin, saat membuka `/dudul` tanpa login, aku diarahkan ke halaman login dul-auth, lalu kembali ke `/dudul` setelah berhasil.
- **US-2** Sebagai admin, kalau password salah, aku melihat pesan error umum yang tidak membocorkan apakah email terdaftar.
- **US-3** Sebagai admin, aku bisa logout, dan setelah itu `/dudul` meminta login lagi.
- **US-4** Sebagai penyerang, kalau aku memanggil `POST /api/projects` tanpa sesi, aku mendapat `401`.
- **US-5** Sebagai penyerang, kalau aku mencoba menebak password berkali-kali, aku diperlambat atau diblokir sementara.
- **US-6** Sebagai developer cuit, aku cukup membaca `/.well-known/openid-configuration` untuk tahu semua endpoint dan kunci publik.

## 6. Alur utama

Authorization Code Flow dengan PKCE:

```mermaid
sequenceDiagram
    autonumber
    actor A as Admin (browser)
    participant P as porto (SvelteKit)
    participant D as dul-auth (Go)

    A->>P: GET /dudul
    P->>P: tidak ada sesi → buat state + code_verifier, simpan di cookie
    P-->>A: 302 ke D /authorize?client_id=porto&code_challenge=...&state=...
    A->>D: GET /authorize
    D-->>A: 302 ke /login?id=<authRequestId>
    A->>D: POST /login (email, password, id)
    D->>D: cek password (bcrypt), tandai authRequest selesai
    D-->>A: 302 ke porto /auth/callback?code=...&state=...
    A->>P: GET /auth/callback
    P->>P: cocokkan state dengan cookie
    P->>D: POST /oauth/token (code + code_verifier)
    D-->>P: id_token + access_token
    P->>D: GET /keys (JWKS, di-cache)
    P->>P: verifikasi tanda tangan id_token, iss, aud, exp
    P-->>A: set cookie sesi porto, 302 ke /dudul
```

Istilah:
- **state**: nilai acak untuk mencegah CSRF di callback.
- **PKCE** (`code_verifier`/`code_challenge`): membuktikan bahwa yang menukar code adalah pihak yang memulai login, walaupun code bocor di URL.
- **JWKS**: daftar kunci publik dul-auth. Porto memakainya untuk memverifikasi tanda tangan token tanpa perlu bertanya ke dul-auth setiap request.

## 7. Kebutuhan fungsional — dul-auth

### 7.1 Endpoint

| Method | Path | Sumber | Keterangan |
|---|---|---|---|
| GET | `/healthz` | sendiri | `200 ok`, untuk Docker healthcheck |
| GET | `/.well-known/openid-configuration` | library | Discovery document |
| GET | `/authorize` | library | Mulai alur login |
| GET | `/login` | sendiri | Form email dan password |
| POST | `/login` | sendiri | Proses login |
| POST | `/oauth/token` | library | Tukar code dengan token |
| GET | `/keys` | library | JWKS |
| GET | `/userinfo` | library | Klaim user dari access token |
| GET | `/end_session` | library | Logout dan redirect kembali ke client |

### 7.2 Aturan

| ID | Kebutuhan |
|---|---|
| FR-1 | Hanya **authorization code flow**. Implicit dan password grant ditolak. |
| FR-2 | **PKCE `S256` wajib** untuk semua client. `plain` ditolak. |
| FR-3 | `redirect_uri` harus **sama persis** dengan salah satu yang terdaftar untuk client itu. Tidak ada wildcard. |
| FR-4 | Authorization code berlaku **maksimal 60 detik** dan **hanya bisa dipakai sekali**. |
| FR-5 | ID token dan access token berbentuk JWT bertanda tangan **RS256**, berlaku **1 jam**. Header `kid` sesuai dengan kunci di JWKS. |
| FR-6 | Klaim ID token minimal: `iss`, `sub` (UUID user), `aud` (client_id), `exp`, `iat`, `email`. |
| FR-7 | Scope yang didukung: `openid`, `email`. Scope lain diabaikan. |
| FR-8 | Client terdaftar secara **statis lewat konfigurasi** (env/file), bukan tabel database. |
| FR-9 | Password disimpan sebagai hash **bcrypt cost 12**. Password mentah tidak pernah ditulis ke log atau DB. |
| FR-10 | Login gagal membalas pesan yang sama untuk "email tidak ada" dan "password salah". |
| FR-11 | Login dibatasi **5 percobaan per menit per kombinasi IP + email**. Melebihi batas → `429`. |
| FR-12 | Admin pertama dibuat lewat perintah CLI (`dul-auth create-user --email ...`, password dibaca dari stdin), bukan lewat endpoint HTTP. |
| FR-13 | Form login hanya valid untuk `authRequestId` yang ada dan belum selesai. |

## 8. Kebutuhan fungsional — integrasi porto

| ID | Kebutuhan |
|---|---|
| PI-1 | Route baru: `/auth/login`, `/auth/callback`, `/auth/logout`. |
| PI-2 | `state` dan `code_verifier` disimpan di cookie `httpOnly`, `Secure`, `SameSite=Lax`, berlaku 10 menit, dan dihapus setelah callback. |
| PI-3 | ID token diverifikasi: tanda tangan (JWKS), `iss`, `aud = porto`, `exp`. Library: `jose` (Node). |
| PI-4 | Setelah verifikasi, porto membuat **cookie sesi sendiri** (`httpOnly`, `Secure`, `SameSite=Lax`, ditandatangani, berlaku 8 jam) berisi `sub` dan `email`. Token dari dul-auth tidak dikirim ke browser. |
| PI-5 | `hooks.server.ts`: `/dudul/**` tanpa sesi → redirect ke `/auth/login`. `POST`/`PUT`/`DELETE` ke `/api/**` tanpa sesi → `401`. `GET` publik tetap terbuka. |
| PI-6 | Hanya `sub` yang tercantum di env `ADMIN_SUBS` yang dianggap admin. User lain yang berhasil login tetap mendapat `403`. |
| PI-7 | Logout menghapus cookie sesi porto lalu redirect ke `end_session` dul-auth. |

## 9. Model data

Database `dulauth` di instance Postgres yang sama dengan porto, dengan **database dan user DB sendiri**. Porto tidak punya akses ke database ini.

```sql
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Auth request dan authorization code yang sedang berjalan disimpan **di memori proses**. Restart server membuat login yang sedang berlangsung gagal, dan user cukup mengulang. Ini diterima untuk MVP dengan satu instance.

## 10. Konfigurasi

| Env | Contoh | Keterangan |
|---|---|---|
| `PORT` | `8080` | |
| `ISSUER` | `https://auth.didul.site` | Harus sama persis dengan `iss` di token |
| `DATABASE_URL` | `postgres://dulauth:...@postgres:5432/dulauth` | |
| `SIGNING_KEY_PATH` | `/run/secrets/signing.pem` | RSA 2048 privat, PEM |
| `CLIENTS_FILE` | `/etc/dul-auth/clients.json` | Daftar client statis |

Contoh `clients.json`:

```json
[
  {
    "id": "porto",
    "redirect_uris": [
      "https://didul.site/auth/callback",
      "http://localhost:3000/auth/callback"
    ],
    "post_logout_redirect_uris": ["https://didul.site/", "http://localhost:3000/"]
  }
]
```

Porto adalah **public client** (PKCE tanpa client secret). Alasannya, kalau suatu saat ada client tanpa backend, polanya sama. Kalau ingin lebih ketat, porto bisa diubah menjadi confidential client dengan secret karena token exchange terjadi di server.

## 11. Kebutuhan non-fungsional

| Area | Kebutuhan |
|---|---|
| **Keamanan** | HTTPS di production (TLS di Caddy). Semua cookie `httpOnly` + `Secure` + `SameSite=Lax`. Header `X-Frame-Options: DENY` di halaman login. Tidak ada secret di repo atau image. |
| **Library** | Protokol OIDC memakai `github.com/zitadel/oidc/v3` (paket `op`), yang sudah tersertifikasi OpenID. Hash memakai `golang.org/x/crypto/bcrypt`. |
| **Logging** | `log/slog` dengan format JSON. Login berhasil dan gagal dicatat dengan email dan IP, **tanpa password dan tanpa token**. |
| **Deploy** | Docker multi-stage, image akhir `distroless` atau `scratch`. Di belakang Caddy di VPS. Healthcheck ke `/healthz`. |
| **Test** | Unit test untuk hash password, rate limit, dan validasi client. Satu test end-to-end alur login dengan `httptest`. |
| **Ketersediaan** | Satu instance. Kalau dul-auth mati, halaman publik porto tetap jalan dan hanya `/dudul` yang tidak bisa diakses. |

## 12. Milestone dan kriteria selesai

| # | Milestone | Selesai jika |
|---|---|---|
| M1 | Server kosong | `GET /healthz` → `200 ok`, method lain → `405` |
| M2 | Database dan user | Migration `users` jalan; `dul-auth create-user` membuat admin dengan hash bcrypt |
| M3 | OIDC provider | `/.well-known/openid-configuration` dan `/keys` valid; diuji dengan `curl` |
| M4 | Halaman login | Alur `/authorize` → `/login` → redirect ke `redirect_uri` membawa `code`; salah password → pesan umum; percobaan ke-6 dalam semenit → `429` |
| M5 | Token | `POST /oauth/token` dengan code dan verifier yang benar → ID token valid; code dipakai dua kali → ditolak |
| M6 | Integrasi porto | US-1 sampai US-4 lolos di localhost |
| M7 | Deploy | `auth.didul.site` hidup lewat Caddy; porto production memakai dul-auth; `/dudul` terkunci |

## 13. Rencana lanjutan

Urutan kasarnya, dikerjakan **saat dibutuhkan**, bukan sebelumnya:

1. **Sesi SSO di dul-auth**: login sekali untuk porto dan cuit. Dibutuhkan saat cuit ada.
2. **Pendaftaran user publik**: dibutuhkan saat cuit terbuka untuk orang lain.
3. **Login dengan GitHub**: dul-auth menjadi client GitHub, dan callback GitHub hanya ke dul-auth.
4. **Tabel `clients` di DB**: dibutuhkan saat client lebih dari 2–3.
5. **Refresh token**: dibutuhkan kalau sesi 1 jam terasa mengganggu.
6. **Rotasi signing key**: JWKS memuat kunci lama dan baru sekaligus selama masa transisi.
7. **MFA (TOTP)**.
8. **Auth request di Redis atau Postgres**: dibutuhkan kalau dul-auth dijalankan lebih dari satu instance.

## 14. Risiko dan pertanyaan terbuka

| Risiko / pertanyaan | Mitigasi / keputusan |
|---|---|
| Salah implementasi protokol membuka celah | Protokol diserahkan ke `zitadel/oidc`; kita hanya menulis storage, login, dan konfigurasi. |
| Signing key bocor | Disimpan di luar repo dan image, di-mount sebagai secret. Rotasi manual didokumentasikan. |
| dul-auth down berarti admin tidak bisa login | Diterima untuk MVP; halaman publik porto tidak terdampak. |
| Interface storage `zitadel/oidc` cukup besar | Mulai dari contoh storage in-memory di repo library, lalu ganti bagian user dengan Postgres. |
| Porto sebaiknya public atau confidential client? | **Terbuka.** Default PRD: public + PKCE. Diputuskan di M6. |
| Durasi sesi porto 8 jam terlalu lama atau singkat? | **Terbuka.** Mulai 8 jam, sesuaikan setelah dipakai. |

## 15. Glosarium

- **Identity provider (IdP)**: service yang menjawab "siapa user ini" untuk aplikasi lain.
- **OIDC**: protokol standar di atas OAuth 2.0 untuk login.
- **Client**: aplikasi yang meminta login ke IdP. Di sini porto, nanti cuit.
- **Authorization code**: tiket sekali pakai berumur pendek yang ditukar dengan token.
- **ID token**: JWT berisi identitas user, ditandatangani IdP.
- **JWT**: token berbentuk `header.payload.signature` yang bisa diverifikasi dengan kunci publik.
- **JWKS**: endpoint berisi kunci publik IdP.
- **PKCE**: pengaman tambahan agar code yang bocor tidak bisa ditukar oleh pihak lain.
- **bcrypt**: algoritma hash password yang sengaja dibuat lambat agar sulit ditebak.
