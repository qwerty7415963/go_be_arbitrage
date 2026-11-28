# Security Hardening — API, Auth & VPS Contract v1.0

> **Status:** Proposed — awaiting approval  
> **Date:** 2026-11-28  
> **Canonical:** `go_be_arbitrage/plans/security-hardening/contract.md`  
> **Mirror:** `FE_arbitrage/plans/security-hardening/contract.md` (byte-identical)  
> **Change control:** thay đổi phải được duyệt, tăng version, sync mirror. Worker chỉ đọc file này.

## 1. Scope & locked decisions

**In scope**

- Đóng public admin/trading surface; tách public API và admin API.
- Admin API + Dokploy + SSH admin chỉ qua Tailscale.
- Refresh token sang HttpOnly cookie; access token giữ trong memory.
- Public registration dùng CAPTCHA + rate limit.
- Siết authorization các endpoint đang public.
- Traefik security headers, rate limit, access log; VPS firewall/port/secret hardening.
- Rotate toàn bộ secret bị phơi nhiễm.
- Test + rollout/rollback.

**Out of scope v1**

- Email verification (không có mail service).
- App-level MFA; Tailscale là lớp ngoài.
- Cloudflare Access/WAF/DDoS.
- Access-token revocation tức thời; chấp nhận cửa sổ 15 phút.
- DB TLS nội bộ; DB đang ở Docker network riêng.
- CI/CD supply-chain đầy đủ, image signing, encrypted offsite backup (Phase 2).
- Public SSH → Tailscale-only; v1 giữ key-only + fail2ban để không gãy deploy GitHub Actions.
- Admin UI mới; hiện FE không có trang admin.

**Locked decisions**

| #   | Decision                                                                                 |
| --- | ---------------------------------------------------------------------------------------- |
| 1   | Register public; CAPTCHA **Cloudflare Turnstile** + rate limit; không email verification |
| 2   | Admin/trading API chỉ qua Tailscale, listener riêng `:8081`                              |
| 3   | Dokploy chỉ truy cập qua Tailscale                                                       |
| 4   | Rotate JWT/admin/DB/Telegram secrets                                                     |
| 5   | Refresh token: `HttpOnly; Secure; SameSite=Lax` cookie; access token memory              |
| 6   | Public frontend/API vẫn qua Traefik 80/443; backend `:8080` không publish public         |
| 7   | CAPTCHA provider mặc định Turnstile; đổi sang hCaptcha phải sửa trước freeze             |

## 2. Verified baseline (2026-11-28)

- VPS `vmi3658206`, Ubuntu 24.04, public IP `167.86.127.90`, Tailscale IP `100.77.189.117`.
- Traefik/Dokploy đứng trước; TLS hợp lệ; HTTP → HTTPS 301.
- Public đang mở: `22`, `80`, `443`, `3000`, `5000`, `8080`.
- `3000`/`5000` đều là Dokploy admin panel (`<title>Dokploy</title>`), truy cập HTTP trực tiếp.
- Backend `:8080` publish trực tiếp, bypass Traefik.
- `3001`, `5434`, `6379` bind `0.0.0.0` nhưng hiện bị filter từ ngoài; không dựa vào may mắn.
- Traefik `api.insecure: true`; `websecure` routers có `middlewares: []`; không HSTS/CSP/access log; ACME email `test@localhost.com`.
- `/etc/dokploy` mode `777`; backend `.env` mode `644`; local users `ubuntu`, `deploy` đọc được.
- `deploy` có `sudo ALL` và thuộc group `docker` → root-equivalent.
- Secrets hiện tại: JWT 43 ký tự, admin password 11, DB password 11, Telegram token 46. Coi như đã lộ do quyền file.
- SSH: root key-only, password off, fail2ban active; 1.297 failed attempts, 30 banned.
- Backend `ARBITRAGE_MODE=release`, CORS exact `https://hyperscanai.xyz`, SIWE domain đúng, DB/Redis arbitrage không publish host.
- CORS origin lạ không nhận `Access-Control-Allow-Origin`; `Access-Control-Allow-Credentials: true` hiện thừa.
- Frontend/API thiếu HSTS/CSP/nosniff/frame; còn `X-Powered-By: Next.js`.

## 3. Target topology

```text
Internet
  :80/:443 -> Traefik
              -> hyperscanai.xyz        -> frontend :3000
              -> api.hyperscanai.xyz   -> public API :8080 (no admin/trading routes)

Tailnet 100.64.0.0/10
  -> 100.77.189.117:8081   -> admin API :8081 (JWT admin required)
  -> 100.77.189.117:3000   -> Dokploy
  -> 100.77.189.117:5000   -> Dokploy (remove duplicate if possible)
  -> SSH (Phase 2; v1 keep key-only public :22)
```

Rules:

- Public `:8080` không publish; chỉ Traefik gọi qua `dokploy-network` (`10.0.1.0/24`).
- Admin `:8081` publish nhưng firewall chỉ cho `tailscale0`; drop từ `eth0`.
- Admin API dùng HTTP trong Tailscale WireGuard; nếu cần browser admin UI sau này mới dùng Tailscale Serve HTTPS.
- Dokploy `:3000/:5000` firewall chỉ cho `tailscale0`.
- Public internet chỉ còn `80/443` (và `22` tạm thời).

## 4. API surface split

### 4.1 Public listener `:8080`

Chỉ đăng ký:

- `/health`, `/ready`
- `/v1/ping`
- `/v1/auth/register|login|refresh|wallet/nonce|wallet/verify`
- `/v1/auth/me|logout|change-password|wallet/*`
- `/v1/public/*`, `/v1/funding/arbitrage`
- `/v1/market` reads, `/v1/orderbook` reads/WS, `/v1/unified` reads/WS
- `/v1/traders` search/detail/read + `/v1/traders/ws`
- `/v1/trader-groups`, `/v1/trader-follows`, `/v1/telegram`
- `/v1/assets/logos/:assetId`
- `/v1/instruments` GET, `/v1/venue-instruments` GET

### 4.2 Admin listener `:8081` (Tailscale only)

Đăng ký:

- `/v1/admin/*` (traders, telegram/settings, perp-dexes, settings, asset logos)
- `/v1/venues` (toàn bộ)
- `/v1/instruments` POST/PUT/DELETE, `/v1/instruments/:id/trading`
- `/v1/venue-instruments` POST
- `/v1/orderbook/resync`
- `/v1/market/subscribe`
- `/v1/storage/*`
- `/v1/strategies/*`, `/v1/risk/*`, `/v1/executions/*`, `/v1/reconciliation/*`
- `/v1/admin/traders/*`
- `/swagger/*`

### 4.3 Rules

- Public listener **không đăng ký** admin routes → trả `404`, không trả `401` (không lộ route).
- Admin listener yêu cầu `JWT + RequireRole("admin")`; Tailscale không thay JWT.
- User role `user` gọi admin listener → `403`.
- Không có admin UI trong public frontend. Admin dùng curl/Postman/automation từ Tailscale.
- `/v1/storage/retention/cleanup` chỉ admin listener.
- `/swagger` chỉ admin listener.
- Instrument reads vẫn public; writes chỉ admin.
- WebSocket public giữ nguyên nhưng thêm origin allowlist, connection cap, rate limit.

## 5. Auth & session

### 5.1 JWT

- HS256 only; secret ≥ 32 bytes, production ≥ 64 bytes.
- Claims: `sub`, `role`, `tenant_id`, `iss=arbitrage-platform`, `aud=arbitrage-api`, `typ=access`, `jti`, `iat/nbf/exp`.
- Validate `WithValidMethods(HS256)`, `WithIssuer`, `WithAudience`, leeway 30s.
- Access TTL 15m.
- Xoá `GenerateTokenPair` (dead code tạo refresh JWT cùng claims).
- Rotate secret → revoke toàn bộ refresh tokens, buộc login lại.

### 5.2 Refresh cookie

- Production cookie: `__Host-refresh`; `HttpOnly; Secure; SameSite=Lax; Path=/`; không `Domain`; Max-Age 7d.
- Dev: `refresh_token`, `Secure=false`, chỉ khi `ARBITRAGE_MODE != release`.
- Login/Register/WalletVerify: set cookie; response chỉ trả `access_token`, `expires_at`, `user`; **không** trả `refresh_token`.
- Refresh: đọc cookie, rotate, set cookie mới, trả access token mới.
- Logout: đọc cookie, revoke family, clear cookie; trả 204 kể cả access token hết hạn.
- Password change: revoke toàn bộ refresh tokens.

### 5.3 Rotation & reuse

Migration `000043_security_refresh_family`:

```sql
ALTER TABLE refresh_tokens
  ADD COLUMN family_id UUID NOT NULL DEFAULT gen_random_uuid(),
  ADD COLUMN rotated_at TIMESTAMPTZ NULL,
  ADD COLUMN replaced_by_hash TEXT NULL;
CREATE INDEX idx_refresh_tokens_family ON refresh_tokens(family_id);
```

- Rotate atomic trong transaction:

```sql
UPDATE refresh_tokens
SET revoked = TRUE, rotated_at = NOW()
WHERE token_hash = $1 AND revoked = FALSE AND expires_at > NOW()
RETURNING id, user_id, family_id;
```

- Insert token mới cùng `family_id` trong cùng transaction.
- Token cũ bị dùng lại → revoke toàn bộ family, trả `401 AUTH-004`.
- Refresh token hash SHA-256; không lưu plaintext.
- Cleanup worker xoá expired/revoked.

### 5.4 CSRF

- Refresh/Logout yêu cầu `Origin` exact `https://hyperscanai.xyz` (dev `http://localhost:3000`).
- `Sec-Fetch-Site` phải `same-origin` hoặc `none`.
- `SameSite=Lax`; cookie `__Host-`; không dùng cookie cho state-changing API khác.
- CORS exact origin; `Access-Control-Allow-Credentials` bỏ trừ khi sau này có direct cookie API.
- Thêm `Vary: Origin`.

### 5.5 SIWE nonce

- Bỏ pattern `GetValidNonce` → verify signature → `MarkNonceUsed`.
- Thay bằng atomic:

```sql
UPDATE wallet_nonces
SET used = TRUE
WHERE nonce = $1 AND address = $2 AND chain_id = $3
  AND used = FALSE AND expires_at > NOW()
RETURNING id;
```

- Chỉ sau khi verify signature; update fail → reject.
- Không bỏ qua lỗi `MarkNonceUsed`.
- Áp dụng cho `VerifySignature` và `LinkWallet`.
- Test concurrent same nonce: đúng 1 request thành công.

### 5.6 Frontend auth

- Xoá `src/lib/token.ts`; không lưu access/refresh trong localStorage.
- Access token giữ trong memory (Zustand non-persist/module store).
- Khởi động: gọi `/auth/refresh` bằng cookie; nếu fail → unauthenticated.
- `apiClient`:
  - 401 → gọi refresh một lần, retry đúng một lần.
  - Refresh dùng `credentials: 'include'`, không body.
  - Dùng Web Locks + BroadcastChannel để tránh nhiều tab refresh đồng thời.
  - Không gửi refresh token trong body.
- Logout gọi API để clear cookie; clear memory.
- Xoá key cũ `access_token`, `refresh_token` khi boot.
- `AuthResponse` type bỏ `refresh_token`.

## 6. Public registration, CAPTCHA & rate limit

### 6.1 Turnstile

- FE: `NEXT_PUBLIC_TURNSTILE_SITE_KEY`; widget trên form register; gửi `captcha_token`.
- BE: `TURNSTILE_SECRET_KEY`; verify `https://challenges.cloudflare.com/turnstile/v0/siteverify`.
- Yêu cầu: `success=true`, `action="register"`, hostname thuộc `hyperscanai.xyz` (dev `localhost`).
- Timeout 3s; production fail-closed; dev bypass chỉ khi `ARBITRAGE_MODE != release` và `ARBITRAGE_CAPTCHA_ENABLED=false`.
- Không log token.
- Error mới: `AUTH-017 CAPTCHA_FAILED`.

### 6.2 Rate limit

| Endpoint                   | Limit                                      |
| -------------------------- | ------------------------------------------ |
| `POST /auth/register`      | 3/giờ/IP + 10/ngày/IP; CAPTCHA mỗi request |
| `POST /auth/login`         | 5/phút/IP + 10/giờ/email; backoff          |
| `POST /auth/wallet/nonce`  | 10/phút/IP + 5/phút/address                |
| `POST /auth/wallet/verify` | 10/phút/IP + 5/phút/address                |
| `POST /auth/refresh`       | 30/phút/IP                                 |
| Public scanner/search      | 60/phút/IP (edge + app)                    |
| General authenticated API  | 120/phút/user                              |

- Key IP lấy từ trusted proxy; email key hash.
- In-memory cho 1 replica; interface sẵn sàng Redis sau.
- `Retry-After` đúng thời điểm reset.
- `X-Forwarded-For` chỉ tin từ `ARBITRAGE_TRUSTED_PROXIES=10.0.1.0/24`; admin listener trust none.
- Edge Traefik rate limit bổ sung; không thay app limit.

### 6.3 Password / account

- Password 12–128; zxcvbn score ≥ 3; reject email/username; bcrypt cost 12.
- Login error generic; register conflict `409` sau CAPTCHA/rate limit.
- New account role `user`; không admin/trading.
- Audit register/login success/failure, refresh reuse, logout, admin mutations.
- Không email verification (accepted risk); CAPTCHA/rate limit là lớp chống spam.

## 7. VPS & infrastructure

### 7.1 Ports / firewall

| Port                         | Public                   | Tailnet                |
| ---------------------------- | ------------------------ | ---------------------- |
| 80, 443                      | allow                    | allow                  |
| 22                           | key-only + fail2ban (v1) | allow                  |
| 8080 public API              | **deny**                 | deny (Traefik only)    |
| 8081 admin API               | **deny**                 | allow                  |
| 3000, 5000 Dokploy           | **deny**                 | allow                  |
| 3001, 5434, 6379 other stack | deny                     | deny; bind `127.0.0.1` |

- UFW default deny; remove allow `8080`, `3000`.
- Docker bypass UFW → thêm `DOCKER-USER` persistent:
  - allow `tailscale0` → `8081`, `3000`, `5000`, `22`
  - drop `eth0` → `8081`, `3000`, `5000`, `3001`, `5434`, `6379`
  - giữ `80/443` public; giữ Tailscale UDP `41641`.
- Test lại từ Internet và từ Tailscale sau reboot.

### 7.2 Traefik

- `api.insecure: false`; nếu cần dashboard, router riêng + auth + Tailscale IP allowlist.
- `acme.email` đổi sang email thật.
- Bật access log JSON; không log Authorization/Cookie.
- Middleware `security-headers` gắn cho frontend + API `websecure`:
  - HSTS `31536000; includeSubDomains; preload`
  - `X-Content-Type-Options: nosniff`
  - `X-Frame-Options: DENY`
  - `Referrer-Policy: strict-origin-when-cross-origin`
  - `Permissions-Policy: camera=(), microphone=(), geolocation=()`
  - remove `X-Powered-By`
- Middleware `rate-limit` gắn API (ví dụ average 30, burst 60).
- Không tạo public router cho admin API.

### 7.3 File permissions & secrets

- `/etc/dokploy` → `755`; `traefik/dynamic` → `755`; YAML `644`; `acme.json` `600`.
- Backend/frontend `.env` → `600`; code dir `755`.
- `ubuntu` khoá login hoặc `nologin`.
- `deploy` bỏ `sudo ALL` nếu có thể; tối thiểu rotate key, giới hạn Tailscale ACL; ghi nhận `deploy` là root-equivalent.
- Rotate:
  - JWT ≥ 64 ký tự; revoke all refresh tokens.
  - Admin password ≥ 24 ký tự; thêm CLI `admin reset-password` (không expose HTTP).
  - DB password ≥ 32 ký tự; `ALTER USER` + update env + restart.
  - Telegram bot token mới qua BotFather.
- Verify old JWT/refresh/admin/DB token fail sau rotate.

### 7.4 Dokploy / SSH

- Dokploy chỉ truy cập `http://100.77.189.117:3000`/`:5000` trong Tailscale; xoá public ports.
- Nếu `5000` là duplicate, remove.
- SSH v1: giữ public key-only + fail2ban; `X11Forwarding no`, `AllowTcpForwarding no`, `MaxAuthTries 3`.
- Phase 2: SSH chỉ Tailscale + GitHub Actions qua Tailscale.

### 7.5 Repo/deploy config

- `docker-compose.production.yml` align `ARBITRAGE_*`, bỏ secrets khỏi compose, bỏ public `8080`.
- Health check deploy đổi từ `localhost:8080` sang `https://api.hyperscanai.xyz/health` hoặc `docker exec`.
- Dokploy env cập nhật; không còn secret world-readable.
- `traefik` dynamic config cập nhật headers/rate limit/access log.

## 8. Configuration

**Backend mới**

```env
ARBITRAGE_MODE=release
ARBITRAGE_HOST=0.0.0.0
ARBITRAGE_PORT=8080
ARBITRAGE_ADMIN_HOST=0.0.0.0
ARBITRAGE_ADMIN_PORT=8081
ARBITRAGE_TRUSTED_PROXIES=10.0.1.0/24
ARBITRAGE_PUBLIC_ORIGIN=https://hyperscanai.xyz
ARBITRAGE_CORS_ORIGINS=https://hyperscanai.xyz
ARBITRAGE_COOKIE_SECURE=true
ARBITRAGE_COOKIE_NAME=__Host-refresh
ARBITRAGE_REFRESH_EXPIRATION=168h
ARBITRAGE_CAPTCHA_ENABLED=true
TURNSTILE_SECRET_KEY=<secret>
TURNSTILE_EXPECTED_HOSTNAMES=hyperscanai.xyz
ARBITRAGE_JWT_SECRET=<64+ random>
ARBITRAGE_ADMIN_PASSWORD=<24+ random>
ARBITRAGE_DB_PASSWORD=<32+ random>
```

**Frontend mới**

```env
NEXT_PUBLIC_TURNSTILE_SITE_KEY=<public>
NEXT_PUBLIC_API_BASE_URL=
NEXT_PUBLIC_WS_URL=wss://api.hyperscanai.xyz
BACKEND_URL=https://api.hyperscanai.xyz
```

Fail-closed ở `release`:

- admin listener bật, JWT ≥ 32 (khuyến nghị ≥ 64), CORS không wildcard, cookie Secure, CAPTCHA bật, SIWE domain không `localhost`, Turnstile secret tồn tại.

## 9. Headers / CSP / CORS

- Traefik: HSTS, nosniff, frame deny, referrer, permissions; remove `X-Powered-By`.
- Next:
  - `poweredByHeader: false`.
  - CSP nonce qua `src/proxy.ts`; gắn nonce cho theme inline script.
  - `frame-ancestors 'none'`, `object-src 'none'`, `base-uri 'self'`, `form-action 'self'`.
  - `script-src` allow Turnstile `https://challenges.cloudflare.com`.
  - `style-src`/`font-src` allow `https://api.fontshare.com` hoặc self-host font.
  - `img-src 'self' data: blob:`; `connect-src 'self' wss://api.hyperscanai.xyz https://challenges.cloudflare.com` + WalletConnect/RPC allowlist.
  - `frame-src` Turnstile + WalletConnect verify.
- API: HSTS, nosniff, frame deny, referrer; không cần full CSP.
- `NEXT_LOCALE` thêm `Secure`.
- CORS exact origin; `Vary: Origin`; không credentials nếu không direct cookie API.

## 10. Testing requirements

**Go unit**

- JWT: none/HS512/wrong iss/aud/expired; secret length.
- Cookie flags; refresh rotation; reuse revokes family.
- Nonce concurrent consume.
- CAPTCHA success/fail/timeout/hostname/action.
- Rate limiter XFF spoof/trusted proxy/Retry-After.
- Authorization matrix: public 404 admin paths; admin 401/403; user 403.
- Storage cleanup/instrument writes admin-only.

**Go handler/integration**

- Register no CAPTCHA → 400 AUTH-017; invalid → 400; rate limit → 429; valid → 201 + cookie.
- Login sets cookie; refresh reads cookie; logout clears.
- Migration 000043; concurrent refresh only one succeeds.
- Audit events written.
- Public listener does not register admin routes.

**FE**

- No token in localStorage.
- Initialize refresh; reload session; 401 retry once; no infinite loop.
- Turnstile widget + AUTH-017 reset.
- 429/Retry-After messaging.
- Logout clears state; multi-tab sync.
- Existing tests pass.

**Infra acceptance**

- External scan: only `80/443` (+ `22` v1).
- `8080/8081/3000/5000/3001/5434/6379` closed from Internet.
- Admin `8081` works from Tailscale; timeout from Internet.
- Headers present; no `X-Powered-By`; CORS evil origin no ACAO.
- `/etc/dokploy` 755; `.env` 600; old secrets fail.
- Traefik `api.insecure=false`.

## 11. Acceptance criteria

1. Public API không còn admin/trading routes; public request trả 404.
2. Admin/trading chỉ truy cập được từ Tailscale, vẫn yêu cầu JWT admin.
3. Dokploy không còn public; chỉ Tailscale.
4. `8080` không publish public; public API chỉ qua Traefik 443.
5. Refresh cookie đúng flags; không còn refresh token trong JSON/localStorage.
6. Refresh rotation atomic; reuse bị revoke family.
7. SIWE nonce atomic; concurrent replay thất bại.
8. Register bắt buộc CAPTCHA + rate limit; login/wallet/refresh có rate limit.
9. JWT validation chặt; secret/admin/DB/Telegram đã rotate.
10. `/etc/dokploy` 755; `.env` 600; không còn secret world-readable.
11. HSTS/CSP/nosniff/frame/referrer/permissions có trên frontend; API có HSTS/nosniff/frame.
12. CORS chỉ exact origin; XFF spoof không bypass rate limit.
13. Public registration vẫn hoạt động; user mới không vào admin/trading.
14. Unit/handler/integration/FE tests pass; external scan đúng.
15. Audit log ghi nhận register/login/admin mutations/refresh reuse.

## 12. Rollout & rollback

**Rollout**

1. Freeze contract; backup DB + VPS snapshot.
2. Generate secrets mới; chuẩn bị image BE/FE.
3. Maintenance window: migrate `000043`, deploy BE + FE cùng lúc, cập nhật Dokploy env.
4. Rotate JWT/admin/DB/Telegram; revoke refresh tokens; restart; verify.
5. Apply Traefik headers/rate limit/access log; `api.insecure=false`.
6. Apply file perms + UFW/DOCKER-USER; remove public ports; verify from Internet + Tailscale.
7. Monitor 24h: 401/403/429, CAPTCHA fail, refresh reuse, firewall drops.

**Rollback**

- DB migration additive; old code ignores new columns.
- Rollback cả BE + FE cùng phiên bản; cookie cũ bị clear, user login lại.
- Nếu admin listener lỗi, tạm thời mở `8080` nội bộ/Tailscale (không public) và rollback image.
- Không rollback firewall về trạng thái public.

## 13. Known limitations / deferred

- Không email verification; tài khoản rác vẫn có thể nếu vượt CAPTCHA/rate limit.
- Không app MFA; Tailscale ACL là lớp ngoài. Admin API dùng HTTP trong WireGuard.
- Không revoke access token tức thời; cửa sổ 15 phút.
- Không Cloudflare/WAF/DDoS.
- Không DB TLS; DB chỉ trong Docker network.
- Public SSH vẫn key-only; `deploy` root-equivalent.
- CI/CD supply-chain, image signing, encrypted offsite backup: Phase 2.
- Stack `deploy-*` trên cùng VPS không thuộc repo này; vẫn cần bind loopback/Redis auth.

## 14. Changelog

- v1.0 (2026-11-28): initial proposed contract; public register + Turnstile/rate limit; Tailscale-only admin/Dokploy; cookie auth; VPS P0 hardening.
