# DEPRECATED — legacy finance prototype

> **Do not use.** See [`DEPRECATED.md`](DEPRECATED.md). Production finance is [`../cmd/server`](../cmd/server).

---

# IAG Finance API (Go + Gin) — legacy

Early REST prototype for a finance Next.js app. Routes mirror `finance/lib/fin/api/endpoints.ts`.

## Quick start (file persistence)

```bash
cd finance/backend
go mod tidy
go build -o bin/finance-api .
PORT=8082 ./bin/finance-api
```

```bash
chmod +x scripts/smoke_test.sh
./scripts/smoke_test.sh http://localhost:8082/v1
```

State persists to `data/finance-state.json` (override with `DATA_PATH`).

## Production stack (Postgres + Redis + JWT)

```bash
docker compose up -d
```

Copy `.env.example` and set:

```env
DATABASE_URL=postgres://fin:fin@localhost:5434/finance?sslmode=disable
REDIS_URL=redis://localhost:6381/0
JWT_SECRET=change-me-in-production
CORS_ALLOWED_ORIGINS=http://localhost:3000
```

Postgres stores the full app snapshot in `finance_app_state` (JSONB) and bcrypt-hashed accounts in `auth_accounts`. Redis stores refresh token rotation when enabled.

## Connect the Next.js frontend

`finance/.env.local`:

```env
NEXT_PUBLIC_FIN_USE_BACKEND=true
NEXT_PUBLIC_FIN_DATA_MODE=api
NEXT_PUBLIC_FIN_API_URL=http://localhost:8082/v1
```

Demo logins (file or Postgres seed):

| Email | Password | Role |
|-------|----------|------|
| finance@iag.africa | Finance123! | finance_admin |
| kassim@iagcoffee.com | Cfo123! | finance_manager |
| viewer@iag.africa | Viewer123! | finance_viewer |

## Endpoint map

| Feature | Methods |
|---------|---------|
| Health | `GET /health`, `GET /ready` |
| Bootstrap | `GET /bootstrap` |
| Auth | `GET /auth/session`, `GET /auth/accounts` (dev), `POST /auth/login`, `POST /auth/refresh`, `POST /auth/logout`, `PATCH /auth/session` |
| Demo reset | `POST /demo/reset` (when `ALLOW_DEMO_RESET`) |
| Dashboard | `GET /dashboard`, `GET /updates` |
| Sales / invoices | `GET/POST /invoices`, `GET/PATCH/DELETE /invoices/:no`, `GET /invoices/funnel` |
| Banking | `GET /banking/accounts`, `GET /banking/transactions` |
| Fixed assets | `GET /assets`, `GET /assets/:tag` |
| Approvals | `GET /approvals`, `GET /approvals/:id`, `PATCH /approvals/:id` |
| Audit | `GET /audit`, `POST /audit` |
| Modules | `GET /expenses`, `/workers`, `/users`, `/notifications`, `/budgets`, `/journals`, `/inventory`, `/taxes` |
| Settings | `GET /settings`, `PATCH /settings` |

All routes are also available under `/v1/...`.

When `JWT_SECRET` is set, protected routes require `Authorization: Bearer <accessToken>`. `/bootstrap`, `/auth/login`, and `/auth/refresh` stay public.

## RBAC

Roles: `finance_admin`, `finance_manager`, `finance_viewer`. Mutating handlers call `RequirePermission` (see `internal/models/permissions.go`).

## Tests

```bash
go test ./...
```
