# trips-api

A REST API for a small ride-hailing back office, written in Go. Users register, log in, manage their own drivers, and move trips through a fixed set of states. Drivers and trips belong to one account, and nobody else can see or change them.

Stack: Go, Gin, PostgreSQL, pgx, golang-migrate, bcrypt and JWT, with rate limiting from my own [rate-limiter](https://github.com/wilfex81/rate-limiter) module.

## Quick start

```bash
cp .env.example .env        # then set JWT_SECRET (openssl rand -hex 32) and the passwords
docker compose up --build -d
curl -s localhost:8080/readyz
```

Register, log in, and create a driver:

```bash
curl -s -X POST localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"ann@example.com","password":"correct horse"}'

TOKEN=$(curl -s -X POST localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"ann@example.com","password":"correct horse"}' | sed -E 's/.*"access_token":"([^"]+)".*/\1/')

curl -s -X POST localhost:8080/drivers -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Kamau","phone":"+254700000001"}'
```

Run it without Docker: start Postgres (`docker compose up -d db`), set `DATABASE_URL` and `JWT_SECRET`, then `go run ./cmd/api`.

## Configuration

Environment variables, read at startup. A missing required value stops the server with a clear message.

| Variable | Default | Meaning |
|----------|---------|---------|
| `DATABASE_URL` | required | Postgres connection URL |
| `JWT_SECRET` | required | Signing key, at least 32 bytes |
| `ADDR` | `:8080` | Address to listen on |
| `TOKEN_TTL` | `1h` | How long an access token lasts |
| `SHUTDOWN_TIMEOUT` | `5s` | How long in-flight requests get to finish on shutdown |
| `TRUSTED_PROXIES` | none | Comma separated CIDRs of proxies whose `X-Forwarded-For` is trusted |
| `TEST_DATABASE_URL` | none | Database for the integration tests, which skip when unset |

## Endpoints

| Method and path | Auth | Purpose |
|-----------------|------|---------|
| `GET /healthz` | no | The process is alive |
| `GET /readyz` | no | The database answers |
| `POST /auth/register` | no | Create an account |
| `POST /auth/login` | no | Get an access token |
| `GET /me` | yes | The logged in account |
| `POST /drivers` | yes | Create a driver (starts `offline`) |
| `GET /drivers` | yes | List your drivers, with `limit`, `cursor`, `status` |
| `GET /drivers/:id` | yes | One driver |
| `PUT /drivers/:id` | yes | Replace name, phone and status |
| `DELETE /drivers/:id` | yes | Delete a driver with no trips |
| `POST /trips` | yes | Create a trip (starts `requested`) |
| `GET /trips` | yes | List your trips, with `limit`, `cursor`, `status` |
| `GET /trips/:id` | yes | One trip |
| `POST /trips/:id/accept` | yes | Assign a driver: `{"driver_id": 1}` |
| `POST /trips/:id/start` | yes | Start an accepted trip |
| `POST /trips/:id/complete` | yes | Finish a trip in progress |
| `POST /trips/:id/cancel` | yes | Cancel a requested or accepted trip |

Send the token as `Authorization: Bearer <token>`.

### Trip rules

Trips change only through their action endpoints, never by setting a status directly.

| Action | From | To | Driver |
|--------|------|----|--------|
| accept | requested | accepted | assigned, becomes `on_trip` |
| start | accepted | in_progress | unchanged |
| complete | in_progress | completed | becomes `available` |
| cancel | requested or accepted | cancelled | freed if one was assigned |

Anything else returns `409 invalid_transition`. A driver must belong to you and be `available` to accept a trip.

### Pagination

Lists use cursor paging, ordered by id:

```
GET /drivers?limit=20&cursor=<opaque>&status=available
```

```json
{"data": [], "next_cursor": null}
```

`limit` defaults to 20 and is capped at 100. Pass `next_cursor` back as `cursor` to get the next page. It is `null` on the last page.

### Errors

Every error has the same shape:

```json
{"error": {"code": "validation_failed", "message": "request body failed validation", "fields": {"phone": "must be in international format, like +254700000001"}}}
```

| Status | Code | Meaning |
|--------|------|---------|
| 400 | `invalid_json`, `invalid_id`, `invalid_cursor` | The request could not be read |
| 401 | `unauthorized`, `invalid_credentials` | No valid token, or wrong email or password |
| 404 | `not_found`, `route_not_found` | Missing, or not yours |
| 405 | `method_not_allowed` | Wrong HTTP method for the route |
| 409 | `conflict`, `email_taken`, `in_use`, `invalid_transition`, `driver_unavailable` | The request is fine, but the current state does not allow it |
| 413 | `request_too_large` | Body over 1 MiB |
| 422 | `validation_failed` | The body was read but breaks a rule, with a `fields` map |
| 429 | `rate_limited` | Too many requests, see the `Retry-After` header |
| 500 | `internal_error` | Details are logged, never returned |

### Rate limits

| Routes | Counted by | Limit |
|--------|------------|-------|
| `/auth/*` | client address | 5 requests at once, then 1 every 10 seconds |
| `/me`, `/drivers`, `/trips` | logged in user | 60 requests at once, then 1 per second |

| Variable | Default | Meaning |
|----------|---------|---------|
| `AUTH_RATE_BURST` | `5` | Requests at once on `/auth/*`, per client address |
| `AUTH_RATE_PER_SEC` | `0.1` | Tokens added per second on `/auth/*` |
| `API_RATE_BURST` | `60` | Requests at once on the API, per user |
| `API_RATE_PER_SEC` | `1` | Tokens added per second on the API, per user |

Health checks are never limited. `X-Forwarded-For` is ignored unless the request comes from a proxy listed in `TRUSTED_PROXIES`, so a client cannot fake its address.

## How it works

```
cmd/api/                              startup, wiring, graceful shutdown
internal/config/                      settings from the environment
internal/httpapi/                     router, handlers, validation, error format, middleware
internal/auth/                        password hashing and tokens
internal/trips/                       the trip rules, with no database or HTTP
internal/store/                       all SQL, one file per resource
internal/adapter/postgres/migrations/ numbered .sql files, embedded in the binary
```

- **Layers.** Handlers know HTTP, the store knows SQL, and the trip rules know neither. Handlers depend on small interfaces, so handler tests run against in-memory fakes with no database.
- **Ownership lives in the query.** Every store method takes the owner id and puts it in the `WHERE` clause, so there is no way to read a record without saying whose it is. Another owner's record answers `404`, never `403`, so ids do not leak.
- **The database refuses bad data too.** A composite foreign key means a trip can only point at a driver of the same owner, and a check constraint requires accepted trips to have a driver, even if some future query is wrong.
- **State changes are transactions with row locks.** Accepting a trip locks the trip and then the driver (always in that order, to avoid deadlocks), checks the rules against what it just locked, and updates both rows together. Two requests racing for the same trip or the same driver cannot both win. The integration tests start those races on purpose.
- **Passwords and tokens.** Passwords are hashed with bcrypt. Tokens are HS256 JWTs that check signature, algorithm, issuer and expiry. Login answers the same way, and takes the same time, for an unknown email and a wrong password.
- **Migrations** run when the server starts. They are numbered files embedded in the binary.

## Tests

```bash
go vet ./...
go test -race ./...
```

The database tests need a real Postgres and skip themselves otherwise. Use a separate database, since the tests create and delete their own users:

```bash
docker compose exec db psql -U postgres -c 'create database "trip-api-test"'
TEST_DATABASE_URL='postgres://...@localhost:5433/trip-api-test?sslmode=disable' go test -race -count=1 ./...
```

CI runs `go vet`, then `go test -race` against a Postgres service, and builds the Docker image.

## Known limits

- **Access tokens only.** There is no refresh token and no way to revoke a token before it expires. The lifetime is short for that reason.
- **No email verification or password reset.** Registration reveals whether an email is taken.
- **Limits are per process.** Two instances each keep their own counters. Sharing them through Redis is solved in the rate-limiter module and not wired in here.
- **A distributed password guessing attack** that tries one account from many addresses is not covered. A second limit keyed by account would be the next layer.
- **Drivers can be set to any status by hand** with `PUT /drivers/:id`, even while on a trip.
- **No trip history.** An events table recording each transition would give an audit trail.
- **Migrations run at startup.** For several instances, run them as a separate deploy step instead.