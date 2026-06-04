# Finance service (`iag-finance`)

**Accounting and finance** for the IAG platform — general ledger, chart of accounts, AR/AP, tax (URA EFRIS), banking, and the `iag.finance` event consumer.

This is **not** end-user login accounts (see [`iag-authentication`](../authentication/) and future [`iag-accounts`](../accounts/)).

**Stack:** Go 1.23, Gin, PostgreSQL, Redis (optional), Kafka  
**Port (dev):** 3006  
**Database:** `finance` schema on `iag_platform`

## Run locally

```bash
pnpm infra:up
cd shared/services/finance
cp .env.example .env
go run ./cmd/server
```

Via gateway: `http://localhost:8080/api/v1/finance/v1/...`

## API (gateway prefix `/api/v1/finance`)

| Method | Upstream path | Description |
|--------|---------------|-------------|
| GET | `/v1/chart-of-accounts` | List CoA |
| POST | `/v1/chart-of-accounts` | Create account |
| GET | `/v1/ledger/entries` | List journal entries |
| POST | `/v1/ledger/entries` | Create draft entry |
| POST | `/v1/ledger/entries/:id/post` | Post entry |
| POST | `/v1/ledger/validate-posting` | Posting rules check |
| GET | `/v1/reports/trial-balance` | Trial balance (posted lines) |
| GET/POST | `/v1/ar/items` | AR open items |
| GET/POST | `/v1/ap/items` | AP open items |
| GET | `/v1/integrations/ura-efris` | EFRIS status (stub) |
| GET | `/v1/integrations/banking` | Banking status (stub) |
| GET/POST | `/v1/audit/events` | Hash-chain ops audit |
| GET | `/v1/inbox/bank-accounts` | Bank / cash positions |
| GET | `/v1/inbox/ap` | AP inbox (`ap_open_items`) |
| GET | `/v1/inbox/cherry-intake` | Cherry intake queue |
| GET/POST | `/v1/tables/:tableId/rows` | Legacy HTML rows (`seed_*` → use inbox APIs) |

**Legacy gateway paths:** `/api/v1/accounts/v1/*` still work and proxy to this service (same as `/api/v1/finance/v1/*`).

**Permissions:** `finance.view_ledger` / `finance.change_ledger` (and legacy `finance.view_operations` on write/read).

## Event bus

**Consumer** (`ENABLE_CONSUMER=true`):

| Topic | Group | Events |
|-------|-------|--------|
| `iag.finance` | `iag.finance.ledger` | `sale.completed`, `invoice.posted` |
| `iag.fleet` | `iag.finance.fleet` | `fleet.fuel.recorded` |
| `iag.supply-chain` | `iag.finance.supply-chain` | `scm.party.created`, `scm.party.updated` (AP `party_id` backfill) |

**Producer** (`ENABLE_EVENT_PUBLISH=true`, default on when Kafka is configured):

- `POST /v1/ar/items` → `sale.completed` on `iag.finance` (consumer books AR/revenue)
- `POST /v1/ap/items` → `invoice.posted` on `iag.finance` (consumer books expense/AP)

External services may also publish the same event types to `iag.finance`.

**Permissions:** registered at startup when `SERVICE_CLIENT_SECRET` is set. Mutating routes enforce `finance.change_*` / `finance.view_*` at the service (defense in depth with the gateway).

**Legacy code:** the `backend/` directory is an old prototype (`github.com/iag/finance-backend`); the runnable service is `cmd/server` only.

## Vendor portal

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/portal/ap` | AP open items for `?party_id=` (from linked supplier profile) |

SCM party events backfill `party_id` on `ap_open_items` where `vendor_ref` matches the party name or business id.

## Docs

- [Platform integration](./docs/PLATFORM_INTEGRATION.md)
