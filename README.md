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
  http/                        REST handlers, router, JWT middleware, domain<->dto mapper
  ws/                          websocket hub + client (implements the driving side)
  broker/                      Redis publisher/subscriber (implements the driven side)
  repository/                  MSSQL repository — stored procedures only

internal/infrastructure        config loading, MSSQL/Redis client construction (used only by cmd/api)
  config/
  db/

internal/contextutil            shared context.Context key definitions, so no package
                                 needs to import another package purely for a context key
```

Dependency rule: arrows point inward. `domain` imports nothing internal.
`usecase` imports only `domain`. Every `adapter/*` package points inward
toward `domain`/`usecase`/`contextutil`, never sideways into another
adapter (except `http` importing `ws` to wire the WS route) and never into
`infrastructure`. `cmd/api/main.go` is the only place that sees the whole
graph — it's the dependency injection root. Swapping MSSQL for Postgres,
or Redis for NATS, only touches the `adapter`/`infrastructure` files that
implement the relevant port.

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

All routes under `/api/v1` and `/ws` require a JWT (see Auth below).

| Method | Path              | Description                                    |
|--------|-------------------|-------------------------------------------------|
| POST   | `/orders`         | Create an order                                 |
| GET    | `/orders`         | List orders, paged (`?page=&page_size=&customer_id=`) |
| GET    | `/orders/{id}`    | Get a single order                              |
| PUT    | `/orders/{id}`    | Replace CustomerID/Status/Total on an order     |
| DELETE | `/orders/{id}`    | Logical delete (sets `IsDeleted=1`, returns 204)|

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
      "created_at": "...", "updated_at": null, "is_deleted": false
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

`internal/adapter/repository/mssql_order_repository.go` never builds a SQL
string from request data — every method is a single `EXEC dbo.usp_...`
with named parameters. The procedures live in `db/init.sql`:

- `usp_Order_Create`
- `usp_Order_GetByID` — excludes soft-deleted rows
- `usp_Order_List` — pages with `OFFSET`/`FETCH`, returns `COUNT(*) OVER()`
  as a `TotalCount` column on every row so paging metadata comes back in
  the same round trip as the data
- `usp_Order_Update` — updates and re-selects in one call; a no-op (empty
  result set) if the order is missing or already deleted, which the Go
  layer treats as `domain.ErrOrderNotFound`
- `usp_Order_Delete` — logical delete only: flags `IsDeleted`, never
  removes the row, and returns `@@ROWCOUNT` so the caller can tell
  "deleted" apart from "already gone"

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
service (creates the `appdb` database and `dbo.Orders` table), then starts
the API on `:8080`.

Every REST call and the WS handshake require a JWT (`Authorization: Bearer
<token>` header, or `?token=` query param for the WS upgrade). Mint a dev
token with the included helper:

```
docker compose exec app sh -c 'echo no shell in scratch-style image'
# or, from the host with Go installed:
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
session (this one included) receives it.

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

## Tests

```
go test ./...
```

`internal/usecase/order_service_test.go` covers `OrderService` against
hand-rolled fakes for `OrderRepository` and `EventPublisher` — no database,
no Redis, no mocking framework. That's the payoff of routing everything
through ports: the business logic is testable in isolation.

## Notes / things to harden before production

- `AuthMiddleware` (`internal/adapter/http/middleware.go`) validates
  HS256-signed JWTs against a shared secret. Swap the `keyFunc` for RS256 +
  JWKS if you're fronting this with a real IdP (Auth0, Keycloak, Entra ID),
  and rotate `JWT_SECRET` out of plain env vars in production (use a
  secrets manager).
- `cmd/gentoken` is a **dev-only** convenience for minting test tokens.
  Don't ship it, and don't reuse its signing path for real user auth.
- MSSQL credentials and `JWT_SECRET` in `docker-compose.yml` are
  placeholders for local dev only — inject real secrets via your
  orchestrator (Compose `secrets:`, Kubernetes Secrets, etc.) elsewhere.
- `upgrader.CheckOrigin` in `adapter/ws/handler.go` currently allows all
  origins; restrict it to known origins.
- Redis Pub/Sub is fire-and-forget: a subscriber that's down misses
  messages. If you need replay/at-least-once delivery, swap `Publish`/
  `Subscribe` for Redis Streams + consumer groups (same port interfaces,
  different adapter implementation).
- Order creation and event publish aren't transactional; a crash between
  the two loses the notification but not the data. Use an outbox table +
  relay process if that gap matters.
- Ports (`OrderRepository`, `EventPublisher`, `Broadcaster`) make every
  layer mockable — unit-test `usecase` with fakes, no DB or Redis needed.
