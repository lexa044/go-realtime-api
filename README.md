# realtime-api

Clean-architecture skeleton: Go REST API + WebSocket fan-out, backed by
MSSQL for persistence and Redis for cross-instance pub/sub.

## Layers

```
internal/domain              entities, enums, value objects, sentinel errors — no external imports
  entities.go                 Order, Event
  statuses.go                 OrderStatus enum (Pending, Shipped, Cancelled) + every future enum-like type
  money.go                    Money value object (amount + currency, validated construction)
  errors.go                   ErrOrderNotFound, ErrInvalidOrderStatus, ErrInvalidMoney

internal/dto                 wire-format types — decoupled from domain's internal representation
  request/                    PlaceOrderRequest, UpdateOrderRequest
  response/                   OrderResponse, ListOrdersResponse

internal/usecase              business logic + ports (interfaces)
  ports.go                     OrderRepository, EventPublisher, OrderService
  order_service.go

internal/adapter              implements the ports; the only layer that touches transport/storage libraries
  http/                        REST handlers (orders + auth), router, JWT middleware, domain<->dto mapper
  ws/                          websocket hub + client (implements the driving side)
  broker/                      Redis publisher/subscriber (implements the driven side)
  repository/                  MSSQL repositories — stored procedures only
  security/                    bcrypt password hashing + JWT issuance/verification

internal/infrastructure        config loading, MSSQL/Redis client construction (used only by cmd/api)
  config/
  db/

internal/contextutil            shared context.Context key definitions, so no package
                                 needs to import another package purely for a context key
```

Dependency rule: arrows point inward. `domain` imports nothing internal.
`usecase` imports only `domain`. Every `adapter/*` package points inward
toward `domain`/`usecase`/`contextutil`, never sideways into another
adapter — except `http`, which imports both `ws` (to wire the WS route)
and `security` (to verify JWTs the same way they were issued) — and never
into `infrastructure`. `cmd/api/main.go` is the only place that sees the
whole graph — it's the dependency injection root. Swapping MSSQL for
Postgres, or Redis for NATS, only touches the `adapter`/`infrastructure`
files that implement the relevant port.

## Auth

There is no self-serve registration endpoint — accounts are provisioned
directly against the database (see "Provisioning a user" below).

| Method | Path            | Auth required | Description                              |
|--------|-----------------|---------------|-------------------------------------------|
| POST   | `/auth/login`   | none          | `{username, password}` → token pair       |
| POST   | `/auth/refresh` | none          | `{refresh_token}` → new (rotated) token pair |
| POST   | `/auth/logout`  | none          | `{refresh_token}` → 204, always           |

Response shape for login/refresh:

```json
{
  "access_token": "eyJ...",
  "access_token_expires_at": "2026-08-09T12:15:00Z",
  "refresh_token": "9f2c...",
  "refresh_token_expires_at": "2026-08-16T12:00:00Z"
}
```

- **Access tokens** are short-lived HS256 JWTs (`ACCESS_TOKEN_TTL`,
  default 15m), verified by `AuthMiddleware` on every `/api/v1/*` and `/ws`
  request. The subject claim carries the user's ID.
- **Refresh tokens** are opaque random values (never JWTs), stored
  server-side as a SHA-256 hash in `RefreshTokens` — never the raw value —
  so a stolen database dump can't be replayed as a live token.
- **Rotation**: every `/auth/refresh` call revokes the presented token and
  issues a brand new pair, linked via `ReplacedBy`. A client should always
  use the newest refresh token; the old one stops working the moment it's
  used.
- **Reuse detection**: if an already-revoked refresh token is presented
  again, that's treated as a signal of theft — every outstanding token for
  that user is revoked, forcing a fresh login everywhere.
- **Login failures are deliberately generic**: unknown username, wrong
  password, and a disabled (`IsActive = 0`) account all return the same
  `401` with the same message, so a caller can't enumerate valid
  usernames by observing different error responses.
- **Logout is idempotent and silent**: it always returns `204`, whether
  the token was valid, unknown, or already revoked.

### Provisioning a user

```
docker compose --profile tools run --rm seed                       # admin / ChangeMe123!
docker compose --profile tools run --rm seed bob 'Secret123!'       # custom username/password
```

`cmd/seeduser` hashes the password with the same bcrypt code the running
API uses to verify logins, then upserts the user via `usp_User_Upsert` —
re-running it against an existing username resets that user's password,
so it doubles as a "reset the dev password" tool. It has no HTTP surface
and is not part of the API binary; don't ship it.

### Rate limiting

`/auth/login`, `/auth/refresh`, and `/auth/logout` sit outside the JWT
gate by necessity — they're how a client gets a token in the first place —
which makes them the most exposed routes in the API. All three are
rate-limited per client IP, backed by Redis (`internal/adapter/ratelimit`)
rather than in-process counters, since the API is designed to run as
multiple horizontally-scaled instances: an in-memory limiter would give
each instance its own separate budget, which defeats the point. Every
instance sharing the same Redis means the limit is enforced across the
whole fleet, the same way the WS broadcast fan-out shares one Redis
channel.

- `LOGIN_RATE_LIMIT` / `LOGIN_RATE_WINDOW` (default `5` / `1m`) — the
  stricter budget, since `/auth/login` is a password-guessing target.
- `AUTH_RATE_LIMIT` / `AUTH_RATE_WINDOW` (default `20` / `1m`) — shared by
  `/auth/refresh` and `/auth/logout`. Both require possession of an
  already-issued, high-entropy (256-bit) refresh token, so brute-forcing
  isn't realistic; this budget exists mainly to bound abuse/DoS rather
  than guard against credential guessing.

Exceeding the limit returns `429 Too Many Requests` with a `Retry-After`
header (seconds until the window resets). If Redis is unreachable, the
limiter **fails open** — requests proceed and the error is logged —
because keeping the auth endpoints available matters more than rate
limiting continuing to function during a Redis outage.

`RateLimitMiddleware` (`internal/adapter/http/ratelimit_middleware.go`)
keys on `middleware.GetClientIP(r.Context())`, which reads whatever
`router.go`'s chosen `middleware.ClientIPFrom*` middleware resolved — chi
v5.3.0 deprecated `middleware.RealIP` over three disclosed CVEs (one rated
Critical) because it trusted `X-Forwarded-For`/`X-Real-IP`/`True-Client-IP`
unconditionally, regardless of whether the deployment actually sat behind
something that set them; the replacement requires picking one of four
explicit `ClientIPFrom*` middlewares, with **no safe default**. `router.go`
currently installs `ClientIPFromRemoteAddr` — correct only when this API is
directly reachable from the internet with no reverse proxy in front. If
you deploy behind one (Cloudflare, an ALB, nginx, ...), you **must** swap
that one line for `ClientIPFromHeader`, `ClientIPFromXFF`, or
`ClientIPFromXFFTrustedProxies` to match your actual topology — otherwise
the rate limiter either keys on the proxy's IP (useless — every client
shares one budget) or trusts a header any client can forge (spoofable).
See the comment above `r.Use(middleware.ClientIPFromRemoteAddr)` in
`router.go` for the full decision guide.

## Order attribution (`created_by` / `updated_by`)

Every write to an order records who made it:

- `PlaceOrder` sets both `CreatedBy` and `UpdatedBy` to the authenticated
  caller's user ID.
- `UpdateOrder` changes `UpdatedBy` only — `CreatedBy` never changes after
  insert.
- `DeleteOrder` (logical delete) also updates `UpdatedBy` — a delete is
  still a modification of the row.

Both columns are real foreign keys into `Users(ID)` (`FK_Orders_CreatedBy`,
`FK_Orders_UpdatedBy`), so an order can never reference a nonexistent
user. The acting user's ID is read from the verified JWT's subject claim
by `actorFromContext` in `internal/adapter/http/handler.go` — never
accepted from the request body — so a client can't forge who an action is
attributed to.

## Domain types

- **`OrderStatus`** (`internal/domain/statuses.go`) is a closed set —
  `Pending`, `Shipped`, `Cancelled` — constructed only via
  `ParseOrderStatus`, so an arbitrary string can never reach persistence as
  a status. Invalid values are rejected with `400` at the HTTP boundary,
  before they reach the usecase layer.
- **`Money`** (`internal/domain/money.go`) pairs an amount with a currency
  code and is constructed only via `NewMoney`, which rejects negative,
  NaN/Inf, or unsupported-currency values and rounds to 2 decimal places.
  Supported currencies: `USD`, `EUR`, `GBP` (extend the map as needed).

## API

All routes under `/api/v1` and `/ws` require a JWT access token
(`Authorization: Bearer <token>` header, or `?token=` query param for the
WS upgrade — see "Auth" above for how to get one).

| Method | Path              | Description                                    |
|--------|-------------------|-------------------------------------------------|
| POST   | `/orders`         | Create an order                                 |
| GET    | `/orders`         | List orders, paged (`?page=&page_size=&customer_id=`) |
| GET    | `/orders/{id}`    | Get a single order                              |
| PUT    | `/orders/{id}`    | Replace CustomerID/Status/Total on an order     |
| DELETE | `/orders/{id}`    | Logical delete (sets `IsDeleted=1`, returns 204)|
| GET    | `/users/me`       | Get the authenticated caller's own profile      |

`GET /users/me` returns whichever user the presented token's subject
claim names — there is no way to request another user's profile:

```json
{ "id": "...", "username": "admin", "is_active": true, "created_at": "..." }
```

Request body for `POST`/`PUT` (`currency` optional, defaults to `USD`):

```json
{ "customer_id": "cust-1", "status": "shipped", "total": 42.5, "currency": "USD" }
```

`GET /orders` response shape:

```json
{
  "data": [
    {
      "id": "...", "customer_id": "...", "status": "pending",
      "total": 42.5, "currency": "USD",
      "created_at": "...", "updated_at": null, "is_deleted": false,
      "created_by": "user-id-of-creator", "updated_by": "user-id-of-last-writer"
    }
  ],
  "page": 1,
  "page_size": 20,
  "total_count": 137
}
```

Response fields are defined in `internal/dto/response`, mapped from
`domain.Order` by `internal/adapter/http/mapper.go` — the wire format is
never `domain.Order` serialized directly, so a change to how `Money` or
`OrderStatus` are represented internally doesn't automatically change the
API contract.

`page` defaults to 1, `page_size` defaults to 20 and is capped at 200 —
both are clamped in `OrderService.ListOrders`, not just validated, so a
bad or missing value degrades to a sane default rather than erroring. An
invalid `status` or `total`/`currency` on `POST`/`PUT`, by contrast, is
rejected outright with `400` — `domain.ParseOrderStatus`/`domain.NewMoney`
run in the HTTP handler before the usecase is ever called.

Create, Update, and Delete each publish an event (`order.created`,
`order.updated`, `order.deleted`) to Redis, which every connected
websocket session receives — see "Data flow" below.

## Stored procedures, not dynamic SQL

`internal/adapter/repository/*.go` never builds a SQL string from request
data — every method is a single `EXEC dbo.usp_...` with named parameters.
The procedures live in `db/init.sql`:

**Orders**
- `usp_Order_Create` — now also takes `@CreatedBy`/`@UpdatedBy`
- `usp_Order_GetByID` — excludes soft-deleted rows
- `usp_Order_List` — pages with `OFFSET`/`FETCH`, returns `COUNT(*) OVER()`
  as a `TotalCount` column on every row so paging metadata comes back in
  the same round trip as the data
- `usp_Order_Update` — updates and re-selects in one call; a no-op (empty
  result set) if the order is missing or already deleted, which the Go
  layer treats as `domain.ErrOrderNotFound`; sets `@UpdatedBy`, never
  touches `CreatedBy`
- `usp_Order_Delete` — logical delete only: flags `IsDeleted`, sets
  `UpdatedBy`, never removes the row, and returns `@@ROWCOUNT` so the
  caller can tell "deleted" apart from "already gone"

**Users**
- `usp_User_GetByUsername` — returns the row regardless of `IsActive`, so
  `AuthService` can reject an inactive account with the same generic error
  as a wrong password
- `usp_User_GetByID` — backs `GET /users/me`, since the JWT carries a
  user ID (subject claim), not a username
- `usp_User_Upsert` — insert-or-reset-password by username; used only by
  `cmd/seeduser`, never by request handling

**RefreshTokens**
- `usp_RefreshToken_Create`
- `usp_RefreshToken_GetByTokenHash` — deliberately returns revoked tokens
  too, so `AuthService.Refresh` can detect reuse rather than just seeing
  "not found"
- `usp_RefreshToken_Revoke` — sets `RevokedAt` and optionally `ReplacedBy`
  (the rotation chain link)
- `usp_RefreshToken_RevokeAllForUser` — the reuse-detection response

The `Orders` table carries a `CurrencyCode` column alongside `Total`, one
component of `domain.Money` each; the repository's `scanOrder` helper
reassembles them into a validated `Money` (and `Status` into a validated
`OrderStatus`) via the same constructors the rest of the app uses, so a row
that somehow contains an invalid value surfaces as an error rather than
entering the domain unchecked.

This keeps query plans stable and reusable, lets a DBA tune or audit data
access independently of application deploys, and limits the SQL injection
surface to well-typed, named parameters rather than string concatenation.

## Data flow

1. Client `POST /api/v1/orders` → `OrderHandler` → `OrderService.PlaceOrder`
2. Service writes to MSSQL via `OrderRepository`, then calls
   `EventPublisher.Publish` (→ `redis.Publish` under the hood)
3. Every API instance runs a `broker.Subscriber` on that same Redis channel
4. Each instance's subscriber calls `Hub.Broadcast` with the raw payload
5. Each `Hub` fans the message out to its own locally-connected clients

Because step 3 happens on every instance, one publish reaches every
connected client cluster-wide — no direct instance-to-instance coupling,
no sticky sessions required.

## Running with Docker Compose

```
docker compose up --build
```

This starts MSSQL, Redis, runs `db/init.sql` once via a one-shot `migrate`
service (creates `appdb` and all tables/procedures), then starts the API
on `:8080`. No user exists yet — provision one:

```
docker compose --profile tools run --rm seed
```

Then log in to get a token pair:

```
curl -X POST http://localhost:8080/auth/login \
     -H "Content-Type: application/json" \
     -d '{"username":"admin","password":"ChangeMe123!"}'
```

Or, for quick manual testing without going through login at all, mint a
token directly (bypasses password auth entirely — dev only):

```
JWT_SECRET=dev-secret-change-me go run ./cmd/gentoken user-123
```

Then:

```
curl -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"customer_id":"cust-1","total":42.5}' \
     http://localhost:8080/api/v1/orders

# in another terminal, e.g. with websocat:
websocat "ws://localhost:8080/ws?token=$TOKEN"
```

Placing an order publishes an event to Redis; every connected websocket
session (this one included) receives it. The order's `created_by`/
`updated_by` will be whichever user ID the token's subject claim carries
(`user-123` if minted via `gentoken`, or the real user ID if logged in via
`/auth/login`).

To refresh a token pair before the access token expires:

```
curl -X POST http://localhost:8080/auth/refresh \
     -H "Content-Type: application/json" \
     -d "{\"refresh_token\":\"$REFRESH_TOKEN\"}"
```

Note the refresh token returned here replaces the one you presented — the
old one is now revoked and can't be used again.

Fetch your own profile:

```
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/users/me
```

And if you exceed `LOGIN_RATE_LIMIT` attempts within `LOGIN_RATE_WINDOW`
against `/auth/login`, you'll get `429 Too Many Requests` with a
`Retry-After` header until the window resets.

## Running locally without Docker

Either export the required variables directly, or copy `.env.example` to
`.env` and edit it — `config.Load()` reads a local `.env` file if present
(real environment variables always win over it):

```
cp .env.example .env
# edit .env with real values, then:
go run ./cmd/api
```

`MSSQL_DSN` and `JWT_SECRET` are required; `Load()` returns an error and
the process exits with a clear message if either is missing.
`ACCESS_TOKEN_TTL`/`REFRESH_TOKEN_TTL` default to `15m`/`168h` if unset,
parsed with `time.ParseDuration` (so `"15m"`, `"1h30m"`, `"168h"` etc. —
not raw numbers).

## Tests

```
go test ./...
```

`internal/usecase/order_service_test.go`, `auth_service_test.go`, and
`user_service_test.go` cover `OrderService`, `AuthService`, and
`UserService` against hand-rolled fakes for every port (`OrderRepository`,
`UserRepository`, `RefreshTokenRepository`, `PasswordHasher`,
`TokenIssuer`, `EventPublisher`) — no database, no Redis, no mocking
framework. `internal/adapter/http/ratelimit_middleware_test.go` covers
`RateLimitMiddleware` the same way, against a fake `RateLimiter`, so the
allow/deny/fail-open logic is verified without a real Redis instance.
That's the payoff of routing everything through ports: the business logic
— including refresh-token rotation, reuse detection, and rate-limit
behavior — is testable in isolation.

## Notes / things to harden before production

- `AuthMiddleware` (`internal/adapter/http/middleware.go`) verifies
  HS256-signed JWTs via `adapter/security.VerifyAccessToken`, against a
  shared secret. Swap the whole `security` package for RS256 + JWKS if
  you're fronting this with a real IdP (Auth0, Keycloak, Entra ID), and
  rotate `JWT_SECRET` out of plain env vars in production (use a secrets
  manager).
- `cmd/gentoken` is a **dev-only** convenience for minting test tokens —
  it accepts any user ID with no password check at all. It signs through
  the same `security.JWTTokenIssuer` the API uses, so tokens it produces
  are indistinguishable from real ones; don't ship it or expose it.
- `cmd/seeduser` is similarly **dev-only**: it's the only way to create a
  user, since there's no self-serve registration endpoint. Don't ship it.
- MSSQL credentials, `JWT_SECRET` in `docker-compose.yml`, and the
  `seed` service's default `admin`/`ChangeMe123!` are placeholders for
  local dev only — inject real secrets via your orchestrator (Compose
  `secrets:`, Kubernetes Secrets, etc.) elsewhere, and never run `seed`
  with its defaults against anything but a throwaway dev database.
- `upgrader.CheckOrigin` in `adapter/ws/handler.go` currently allows all
  origins; restrict it to known origins.
- The rate limiter's trustworthiness depends entirely on `router.go`
  installing the *correct* `middleware.ClientIPFrom*` for your actual
  deployment topology (see "Rate limiting" above) — the default,
  `ClientIPFromRemoteAddr`, is only correct with no reverse proxy in
  front. Deploying behind one without swapping it defeats the rate
  limiter (every client shares the proxy's IP as one budget). It also
  fails open on a Redis outage, which is a deliberate availability
  trade-off, not an oversight.
- Redis Pub/Sub is fire-and-forget: a subscriber that's down misses
  messages. If you need replay/at-least-once delivery, swap `Publish`/
  `Subscribe` for Redis Streams + consumer groups (same port interfaces,
  different adapter implementation).
- Order creation and event publish aren't transactional; a crash between
  the two loses the notification but not the data. Use an outbox table +
  relay process if that gap matters. The same is true of refresh token
  issuance vs. persistence.
- `RefreshTokens` rows are never purged — every login/refresh inserts a
  new one, and revoked/expired rows just accumulate. Fine early on, but
  add a periodic cleanup job before this runs unattended for months.
- Ports (`OrderRepository`, `UserRepository`, `RefreshTokenRepository`,
  `PasswordHasher`, `TokenIssuer`, `EventPublisher`, `Broadcaster`,
  `RateLimiter`) make every layer mockable — unit-test `usecase`/
  middleware with fakes, no DB, Redis, or real crypto needed.
