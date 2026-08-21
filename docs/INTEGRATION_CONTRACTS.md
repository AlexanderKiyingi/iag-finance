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

**Event-driven postings** follow the same period control but resolve their date
differently: they are dated by the event's `time`, and when that period is closed
they file into the current open period with the deferral recorded on the entry
description, rather than being refused. See
[EVENT_CONTRACT.md §4.1](./EVENT_CONTRACT.md#41-dating-and-late-delivery).

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
| `GET /v1/reports/control-reconciliation` | GL vs subledger for AR (1100), AP (2000), GR/IR (2150) and Payments Clearing (1050); `difference` of 0 is reconciled |

### 5a. Payments Clearing worklist

Operational disbursements settled by iag-payments land in **1050 Payments
Clearing** rather than being classified on arrival — a settlement does not say
whether it paid a vendor invoice, a payroll run, a loan or a claim, and guessing
would either misclassify it or double-book against finance's own AP and payroll
paths. These endpoints are how it gets emptied.

| Endpoint | Effect |
|----------|--------|
| `GET /v1/payments-clearing?status=open` | The worklist: settled disbursements awaiting a document, with `ageDays`. `status=cleared` or `status=` (all) for audit. Gate `finance.view_ledger`. |
| `POST /v1/payments-clearing/:id/clear` | Record the document this disbursement paid — body `{"documentRef":"..."}`, required. **404** if unknown or already cleared, so a double submission never overwrites the first match. Gate `finance.manage_banking`. |

Clearing records the match; it posts no journal. Reclassifying money out of 1050
into its final account is a ledger act with its own gates and period control, so
working the queue cannot move the general ledger as a side effect.

A clearing account is healthy when it *turns over*, not when its balance is
small. Watch `ageDays` on the open list and the `1050` row of the control
reconciliation together.

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

> **Re-baseline required once, on upgrade.** Chain hashes were computed over a
> timestamp at Go's clock precision, which is finer than the microseconds
> Postgres stores — so the recomputed hash never matched the stored one and
> `verify` answered `409` on an intact chain. Appends now hash the truncated
> timestamp and verify cleanly. Rows written *before* the fix still fail, because
> their digests cover a timestamp the database cannot return. Verify will report
> the first such row as `brokenAt`; treat that as the known baseline, not as
> tampering, and archive-then-truncate `audit_events` to start a clean chain if
> you need `verify` to be green.

---

## 7. Permissions

Enforced at the service (defence in depth with the gateway). Superuser bypasses
all checks.

**Baseline:**

| Capability | Permission |
|------------|------------|
| Read GL / reports / masters | `finance.view_ledger` |
| Routine journal create/post/pay/adjust | `finance.change_ledger` |
| Vendor portal (own AP) | `finance.view_own_ap` / `finance.view_own_payment` |
| Admin audit/monitoring | admin role |

**Granular capability permissions (separation of duties).** Each sensitive write
is gated by a specific permission **OR** `finance.change_ledger` — so existing
`change_ledger` grants keep working, and you enforce SoD by *removing*
`change_ledger` from a role and granting only the narrow permissions:

| Action | Permission |
|--------|------------|
| Modify chart of accounts | `finance.manage_coa` |
| Reverse a posted entry | `finance.reverse_journal` |
| Close/reopen periods, year-end | `finance.close_period` |
| Fixed assets + depreciation | `finance.run_depreciation` |
| Exchange rates + FX revaluation | `finance.manage_fx` |
| Tax codes | `finance.manage_tax` |
| Submit to URA EFRIS | `finance.submit_efris` |
| Create entities | `finance.manage_entities` |
| Set budgets | `finance.manage_budgets` |
| Projects / cost centres | `finance.manage_dimensions` |
| Create/issue invoices, recurring | `finance.issue_invoice` |
| Payment intents (collect) | `finance.collect_payment` |
| Approve at a tier | `finance.approve_tier1/2/3` (requester ≠ approver; distinct approver per tier) |

**Scoped reads / multi-entity:**

| Capability | Permission |
|------------|------------|
| Select a non-default entity (`X-Entity-Id`) | `finance.cross_entity` (else 403) |
| Consolidated (`?consolidated=true`) reports | `finance.view_consolidated` (else the flag is ignored → single-entity scope) |
| Payroll mirror data | `finance.view_payroll` |

> Limitation: `finance.cross_entity` is all-or-nothing across non-default
> entities. **Per-entity** authorization (which specific entities a user may
> access) needs an auth-service token claim listing them — checked in
> `EntityContext` once available.
