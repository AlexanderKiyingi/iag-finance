# Finance — REST integration contracts

Correctness rules an integrator must respect when calling the finance REST API:
idempotency, the error model, the journal lifecycle, fiscal-period controls,
reversals, period-bounded reports, and the audit chain.

Pairs with the route catalog in [FRONTEND_INTEGRATION.md](./FRONTEND_INTEGRATION.md),
the event contract in [EVENT_CONTRACT.md](./EVENT_CONTRACT.md), and FX rules in
[MULTICURRENCY.md](./MULTICURRENCY.md). All paths are behind the gateway prefix
`/api/v1/finance` and require `Authorization: Bearer <jwt>`.

---

## 1. Idempotency

| Operation | Idempotency mechanism |
|-----------|-----------------------|
| **Payments** (`POST /ar|ap/items/:id/payments`) | A stable key is **required**: send an `Idempotency-Key` header *or* a `paymentRef` in the body. Omitting both → **400**. A retry with the same key is a no-op and returns the existing payment. |
| **Event-driven booking** | Idempotent on `envelope.id` (see EVENT_CONTRACT §4). |
| **Adjustments / EFRIS submit** | Idempotent on the document ref (re-submitting an acknowledged EFRIS doc returns the prior receipt, **200**, no second URA call). |

**Rule:** generate a stable `Idempotency-Key`/`paymentRef` on the client and reuse
it across retries. Never let the server mint a fresh one per attempt.

---

## 2. Error model

Errors use a consistent JSON body:

```json
{ "error": { "code": "unprocessable_entity", "message": "payment exceeds open balance" } }
```

| Status | When | Example |
|--------|------|---------|
| `400` | Malformed request / missing idempotency key | bad JSON, no `Idempotency-Key`/`paymentRef` |
| `401` | Missing/invalid Bearer token | |
| `403` | Authenticated but lacks permission | needs `finance.change_ledger` |
| `404` | Resource not found | open item / journal entry / document |
| `409` | Conflict | duplicate document ref; audit chain `verify` failed |
| `422` | Business-rule rejection | unbalanced/closed-period post, over-payment, currency mismatch, credit note exceeds balance, period has drafts |
| `502` | Upstream adapter failure | bank feed / EFRIS gateway |
| `503` | Dependency unavailable | bank feed not configured; Postgres down (readiness) |

Treat `422` as "your input was understood but violates an accounting rule" — do
not retry without changing the request.

---

## 3. Journal entry lifecycle

```
draft ──post──▶ posted ──reverse──▶ reversed
                  │
                  └── (never edited in place)
```

- **Create** `POST /v1/ledger/entries` → `draft`. Debits must equal credits or
  the create is rejected.
- **Post** `POST /v1/ledger/entries/:id/post` → `posted`. Posting is guarded and
  single-shot: a concurrent double-post is rejected (`422`), and the entry's
  **accounting period** must be open. Balance is enforced at the database level.
- **Reverse** `POST /v1/ledger/entries/:id/reverse` (body: `{ "reason": "..." }`)
  → posts a mirror-image entry and marks the original `reversed`. This is the
  **only** way to undo a posted entry — posted entries are immutable. Reversing a
  non-posted entry → `422`; unknown entry → `404`.

The acting user is taken from the JWT for the audit trail (§6) — you cannot set
the actor from the body.

---

## 4. Fiscal periods

Period control keys off each entry's **`accountingDate`** (an optional field on
create; defaults to today), not wall-clock posting time.

| Endpoint | Effect |
|----------|--------|
| `GET /v1/ledger/periods` | List periods with open/closed status |
| `POST /v1/ledger/periods/:period/close` | Close `YYYY-MM`. **Refuses (422)** if any draft entry is dated in that period. Blocks further posting into it. |
| `POST /v1/ledger/periods/:period/reopen` | Reopen a closed month |
| `POST /v1/ledger/year-end/:year/close` | Year-end close (rolls P&L into retained earnings) |

Posting into a closed period → `422` (`accounting period is closed`). To back-date
a correction into a closed month, reopen → post → re-close.

---

## 4a. Fixed-asset subledger

Keyed to the warehouse asset tag (`assetRef`); capitalisation and depreciation post to the GL and respect the period close.

| Endpoint | Effect |
|----------|--------|
| `GET /v1/fixed-assets` | List capitalised assets (cost, accumulated depreciation, NBV) |
| `POST /v1/fixed-assets` | Capitalise; posts `Dr 1500 Fixed Assets / Cr <expense, default 5000>` for cost as of `inServiceDate` (skip the reclass with `recordOnly`). **422** if that period is closed. |
| `POST /v1/fixed-assets/depreciation/run?period=YYYY-MM` | Straight-line monthly run; idempotent and incremental per (asset, period); posts one `Dr 5300 Depreciation Expense / Cr 1510 Accumulated Depreciation`. **422** if the period is closed. |

Disposal de-recognition is event-driven (`warehouse.asset.disposed`): when the asset is capitalised, cost + accumulated depreciation are reversed and the gain/loss booked, against the system carrying amount; otherwise the carried book value is used. See [EVENT_CONTRACT.md](./EVENT_CONTRACT.md).

---

## 5. Period-bounded reports

Reports accept accounting-date query params and aggregate **posted** entries in
the **base currency** (see MULTICURRENCY.md):

| Report | Params |
|--------|--------|
| `GET /v1/reports/trial-balance` | `?from=YYYY-MM-DD&to=YYYY-MM-DD` (range) + `balanced` flag in response |
| `GET /v1/reports/profit-and-loss` | `?from=&to=` (period) |
| `GET /v1/reports/balance-sheet` | `?asOf=YYYY-MM-DD` (point-in-time; `?to=` accepted as alias) |
| `GET /v1/reports/gl-account/:code` | `?from=&to=` — per-account postings with running base balance |
| `GET /v1/reports/ar-aging`, `/ap-aging` | open AR/AP buckets, base currency |

Omitting the params returns the unbounded (inception-to-today) view. The trial
balance response includes `totalDebit`, `totalCredit`, and a `balanced` boolean.

---

## 6. Tamper-evident audit chain

Mutating GL operations (posting, payments, adjustments, reversals) append to a
SHA-256 hash chain, attributed to the authenticated principal.

| Endpoint | Purpose |
|----------|---------|
| `GET /v1/audit/events` | List recent chain entries |
| `GET /v1/audit/events/verify` | Recompute the chain; `200` `{valid:true}` or `409` `{valid:false, brokenAt, reason}` |
| `POST /v1/audit/events` | Append an ops event (actor is taken from the JWT, **not** the body) |

Use `verify` in monitoring/compliance checks to detect tampering: any in-place
edit, deletion, or reorder of a past entry breaks the chain.

---

## 7. Permissions quick reference

| Capability | Permission(s) |
|------------|---------------|
| Read GL / reports / FX rates / fixed assets | `finance.view_ledger` (or legacy `finance.view_operations`) |
| Post / pay / adjust / reverse / set FX rates / close periods / register assets / run depreciation | `finance.change_ledger` (or legacy `finance.change_operations`) |
| Vendor portal (own AP) | `finance.view_own_ap` / `finance.view_own_payment` |
| Admin audit/monitoring | admin role |

Permissions are enforced at the service (defence in depth with the gateway).
