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
| POST | `/v1/ar/items/:id/payments` | Apply customer receipt (Cash/AR) |
| GET/POST | `/v1/ap/items` | AP open items |
| POST | `/v1/ap/items/:id/payments` | Apply vendor disbursement (AP/Cash) |
| GET | `/v1/reports/ar-aging` | AR aging buckets |
| GET | `/v1/reports/profit-and-loss` | P&L by revenue/expense accounts |
| GET | `/v1/reports/balance-sheet` | Assets, liabilities, equity |
| GET | `/v1/finance/summary` | AR balance summary (BFF / DMS) |
| GET/POST/PATCH/DELETE | `/v1/invoices` | Legacy invoice CRUD (maps to AR open items) |
| GET | `/v1/invoices/funnel` | Sales funnel (overdue / open / paid) |
| GET | `/v1/banking/accounts` | Bank accounts (legacy shape) |
| GET | `/v1/banking/transactions` | Bank transactions |
| POST | `/v1/integrations/banking/sync` | Pull bank feed via HTTP adapter |
| GET | `/v1/integrations/ura-efris` | EFRIS status + submission counts |
| POST | `/v1/integrations/ura-efris/submit` | Queue EFRIS submission |
| GET | `/v1/integrations/banking` | Banking status + statement counts |
| POST | `/v1/integrations/banking/statements` | Import bank statement metadata |
| GET | `/v1/integrations/banking/statements/:id/lines` | List statement lines + match status |
| POST | `/v1/integrations/banking/lines/:lineId/match` | Match line to AR/AP document |
| POST | `/v1/integrations/banking/statements/:id/reconcile/auto` | Auto-match lines by ref/amount |
| GET | `/v1/ar/customers/:customerRef/statement` | Customer AR statement |
| GET | `/v1/ar/invoices/:documentRef/pdf` | Invoice PDF |
| GET | `/v1/ar/items/:id/payment-link` | Payment link for open invoice |
| POST | `/v1/ar/credit-notes` / `/v1/ar/debit-notes` | AR credit/debit notes |
| POST | `/v1/ap/credit-notes` / `/v1/ap/debit-notes` | AP credit/debit notes |
| GET | `/v1/adjustments` | List credit/debit notes |
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
| `iag.commercial` | `iag.finance.commercial` | `procurement.invoice.received` → AP inbox |
| `iag.operations` | `iag.finance.erp` | `erp.employee.*`, `erp.leave.*` → payroll mirror ([`docs/PAYROLL_ERP_BOUNDARY.md`](docs/PAYROLL_ERP_BOUNDARY.md)) |

**Payroll prep APIs** (mirror from ERP events, not source of truth):

- `GET /v1/payroll/employees`
- `GET /v1/payroll/leave-accruals`

**Producer** (`ENABLE_EVENT_PUBLISH=true`, default on when Kafka is configured):

- `POST /v1/ar/items` → `sale.completed` on `iag.finance` (consumer books AR/revenue)
- `POST /v1/ap/items` → `invoice.posted` on `iag.finance` (consumer books expense/AP)

External services may also publish the same event types to `iag.finance`.

**Permissions:** registered at startup when `SERVICE_CLIENT_SECRET` is set. Mutating routes enforce `finance.change_*` / `finance.view_*` at the service (defense in depth with the gateway).

**Legacy code:** `backend/` is quarantined — see [`backend/DEPRECATED.md`](backend/DEPRECATED.md). Runnable service: `cmd/server` only.

**Billing identity:** AR `customerRef` ↔ Users `financeCustomerRef` — see [`docs/BILLING_IDENTITY_CONTRACT.md`](docs/BILLING_IDENTITY_CONTRACT.md).

## Vendor portal

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/portal/ap` | AP open items for `?party_id=` (from linked supplier profile) |

SCM party events backfill `party_id` on `ap_open_items` where `vendor_ref` matches the party name or business id.

## Docs

- [Frontend integration (Next.js)](./docs/FRONTEND_INTEGRATION.md) — env template: [docs/frontend.env.example](./docs/frontend.env.example)
- [Platform integration](./docs/PLATFORM_INTEGRATION.md)
- [Billing identity contract](./docs/BILLING_IDENTITY_CONTRACT.md)
- [URA EFRIS](./docs/URA_EFRIS.md)
- [OpenAPI](./docs/openapi.yaml)
