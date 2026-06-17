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
| POST | `/v1/ledger/entries/:id/reverse` | Reverse a posted entry (mirror-image; original → `reversed`) |
| POST | `/v1/ledger/validate-posting` | Posting rules check |
| GET | `/v1/ledger/periods` | List fiscal periods (open/closed) |
| POST | `/v1/ledger/periods/:period/close` \| `/reopen` | Close / reopen `YYYY-MM` |
| GET/POST | `/v1/fx/rates` | List / upsert exchange rates (base `BASE_CURRENCY`, default UGX) |
| POST | `/v1/fx/revalue?period=YYYY-MM` | Period-end FX revaluation of open foreign AR/AP (realized FX books on settlement) |
| GET | `/v1/reports/trial-balance` | Trial balance (posted, base currency; `?from=&to=`, `balanced` flag) |
| GET | `/v1/reports/ap-aging` | AP aging buckets |
| GET | `/v1/reports/gl-account/:code` | GL account detail (`?from=&to=`, running base balance) |
| GET | `/v1/audit/events/verify` | Verify the audit hash-chain |
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

## Accounting features (QuickBooks/Zoho/Tally parity)

| Area | Key endpoints |
|------|---------------|
| **Multi-currency FX** | `GET/POST /v1/fx/rates`, `POST /v1/fx/revalue?period=` (realized FX on settlement; unrealized revaluation) |
| **VAT/GST** | `GET/POST /v1/tax-codes`, `GET /v1/reports/vat-return` (output − input VAT) |
| **Multi-entity** | `GET/POST /v1/entities`; select with `X-Entity-Id` header; statements accept `?consolidated=true` |
| **Budgeting** | `POST /v1/budgets`, `GET /v1/reports/budget-vs-actual` |
| **Reports** | `GET /v1/reports/cash-flow`, `/profit-and-loss`, `/balance-sheet`, `/trial-balance`, `/gl-account/:code`, `/ap-aging` |
| **Invoicing** | `GET/POST /v1/billing/invoices`, `POST /v1/billing/invoices/:id/issue` (→ AR + GL) |
| **Recurring** | `GET/POST /v1/billing/recurring` (worker generates + issues on cadence) |
| **Payments** | `POST /v1/payments/intents`, `POST /v1/payments/intents/:id/confirm` (provider-agnostic; Manual provider built in) |
| **Job costing** | `GET/POST /v1/projects`, `/cost-centers`, `GET /v1/projects/:id/profit-and-loss` |

All GL mutations chain into the audit hash-chain and are entity-aware. Amounts on
documents are transaction-currency; reports aggregate in `BASE_CURRENCY` (default UGX).

## Event bus

**Consumer** (`ENABLE_CONSUMER=true`):

| Topic | Group | Events |
|-------|-------|--------|
| `iag.finance` | `iag.finance.ledger` | `sale.completed`, `invoice.posted` |
| `iag.fleet` | `iag.finance.fleet` | `fleet.fuel.recorded` |
| `iag.supply-chain` | `iag.finance.supply-chain` | `scm.party.created`, `scm.party.updated` (AP `party_id` backfill) |
| `iag.commercial` | `iag.finance.commercial` | `procurement.invoice.received`, `contracts.payment.authorized` → AP |
| `iag.operations` | `iag.finance.erp` | `erp.employee.*`, `erp.leave.*` → payroll mirror ([`docs/PAYROLL_ERP_BOUNDARY.md`](docs/PAYROLL_ERP_BOUNDARY.md)) |
| `iag.operations` | `iag.finance.warehouse` | `warehouse.asset.disposed` → retire fixed asset |

**Payroll prep APIs** (mirror from ERP events, not source of truth):

- `GET /v1/payroll/employees`
- `GET /v1/payroll/leave-accruals`

**Producer** (`ENABLE_EVENT_PUBLISH=true`, default on when Kafka is configured) — all via a **transactional outbox** (event written in the same DB tx as the state change; relay worker delivers at-least-once):

- `POST /v1/ar/items` → `sale.completed` on `iag.finance` (consumer books AR/revenue)
- `POST /v1/ap/items` → `invoice.posted` on `iag.finance` (consumer books expense/AP)
- AR/AP payments → `finance.payment.made` on `iag.finance`
- EFRIS acknowledged → `finance.efris.submitted` on `iag.finance`

External services may also publish the same event types to `iag.finance`. See the
full **[event contract](docs/EVENT_CONTRACT.md)** (payloads, idempotency keys, delivery guarantees).

**Permissions:** registered at startup when `SERVICE_CLIENT_SECRET` is set. Mutating routes enforce `finance.change_*` / `finance.view_*` at the service (defense in depth with the gateway).

**Legacy code:** `backend/` is quarantined — see [`backend/DEPRECATED.md`](backend/DEPRECATED.md). Runnable service: `cmd/server` only.

**Billing identity:** AR `customerRef` ↔ Users `financeCustomerRef` — see [`docs/BILLING_IDENTITY_CONTRACT.md`](docs/BILLING_IDENTITY_CONTRACT.md).

## Vendor portal

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/portal/ap` | AP open items for `?party_id=` (from linked supplier profile) |

SCM party events backfill `party_id` on `ap_open_items` where `vendor_ref` matches the party name or business id.

## Docs

**Integration:**
- [Frontend integration (Next.js)](./docs/FRONTEND_INTEGRATION.md) — env template: [docs/frontend.env.example](./docs/frontend.env.example)
- [Event-bus contract](./docs/EVENT_CONTRACT.md) — consumed/produced events, outbox, idempotency
- [REST integration contracts](./docs/INTEGRATION_CONTRACTS.md) — idempotency, error model, periods, reversal, audit
- [Multi-currency (FX)](./docs/MULTICURRENCY.md) — base currency, rates API, base reporting
- [Billing identity contract](./docs/BILLING_IDENTITY_CONTRACT.md)

**Platform / reference:**
- [Platform integration](./docs/PLATFORM_INTEGRATION.md)
- [Payroll ↔ ERP boundary](./docs/PAYROLL_ERP_BOUNDARY.md)
- [URA EFRIS](./docs/URA_EFRIS.md)
- [OpenAPI](./docs/openapi.yaml)
