# Changelog

All notable changes to the finance service (`iag-finance`). Format loosely
follows [Keep a Changelog](https://keepachangelog.com/). Dates are the cycle the
work merged; the service is not independently versioned (deployed from `main`).

> **Verification note:** the 2026-07 accounting build-out is verified by `go build`
> + `go test ./...` (compile, unit tests, route-gate and manifest-staleness
> guards). The DB round-trip (migrations applied against a live Postgres) is
> **unverified** in the build environment — validate against a running stack
> before relying on the new statements with production data.

## [Unreleased] — 2026-07 · IFRS/IAS build-out

Brought the general ledger up to IFRS/IAS parity for the Uganda operating context.
Every engine composes with the existing double-entry core, is idempotent on a
stable event key, and is fiscal-period-close guarded.

### Added — accounting standards

- **IFRS 16 — Leases** (migration `053`). Recognise a lease on balance sheet:
  `CreateLease` discounts the payments to present value (initial liability + ROU
  asset, `Dr 1600 / Cr 2500`) and precomputes the amortization schedule; a
  periodic run books interest, payment and straight-line ROU depreciation in one
  balanced entry. Accounts `1600/1610/2500/5320/5600`.
  `GET/POST /v1/leases`, `POST /v1/leases/run`. Perm `finance.manage_leases`.
- **IAS 1 — Prepaid-expense amortization** (migration `052`). Capitalise a
  prepayment (`Dr 1250 / Cr` funding) and amortize it straight-line to expense
  over the coverage periods. `GET/POST /v1/prepayments`,
  `POST /v1/prepayments/amortization-run`. Perm `finance.manage_prepayments`.
- **IAS 12 — Income taxes** (migration `054`). Current tax provision on a
  caller-supplied taxable profit at the corporate rate (Uganda 30% default,
  `Dr 5700 / Cr 2600`); deferred tax on a temporary difference tagged deductible
  (→ DTA `1700`) or taxable (→ DTL `2610`). `GET/POST /v1/income-tax/{runs,
  current-run,deferred}`. Perm `finance.manage_tax`.
- **IFRS 10 — Consolidation eliminations** (migration `055`). Report-time
  elimination of intra-group activity on consolidated statements. A posted entry
  with both its entity and `counterparty_entity_id` in the consolidation scope is
  intra-group and its lines are eliminated per account (nets IC receivables/
  payables + IC revenue/COGS at once). Per subsidiary: eliminate its equity,
  recognise non-controlling interest `(1−ownership)×equity`, remove the parent's
  Investment (`1800`) and carry the residual as goodwill (`1900`). Accounts
  `1800/1900/3200`; `entities.ownership_pct`. `GET /v1/consolidation/eliminations`,
  `PATCH /v1/entities/:id/ownership`, `counterpartyEntityId` on manual journal
  create. Consolidated Balance Sheet + P&L fold the eliminations in
  automatically. Perms `finance.view_consolidated` (read) + `finance.manage_entities`.
- **IFRS 15 — Revenue recognition** (migrations `046`, `048`, `050`). Deferred/
  accrued revenue, ratable + milestone schedules, and a recognition run; invoices
  and recurring invoices can carry a recognition spec that spreads their revenue.
  `GET/POST /v1/revenue/schedules`, `POST /v1/revenue/recognition-run`,
  `/v1/revenue/obligations/:id/satisfy`, `/v1/revenue/accrue`.
- **IFRS 9 — Expected credit loss** (migration `047`). ECL allowance provisioning
  run + write-off/recovery. `GET /v1/provisions/ecl`, `POST /v1/provisions/ecl-run`.
- **IAS 16 / IAS 36 — Impairment & revaluation** (migration `041`). Fixed-asset
  impairment to recoverable amount, reversal, and revaluation to a new carrying
  amount. `POST /v1/fixed-assets/{impair,reverse-impairment,revalue}`.
- **IAS 37 — Provisions** (migration `042`). Recognise/unwind/utilise/remeasure a
  liability provision (legal, warranty, decommissioning) with PV discounting.
  `GET /v1/provisions/liability`, `POST /v1/provisions/liability/*`.
- **Three-way match** (migrations `043`, `049`). GR/IR accrual with price and
  quantity variance detection (purchase price variance to `5150`).

### Changed

- **Ledger controls** (migration `044`). Opt-in per-account natural-side
  restriction (`chart_of_accounts.restrict_to_natural_side`) and a trigger scoping
  a journal line's cost-centre/project dimension to its entry's entity.
- **Reverse-charge VAT** (migration `045`). Self-assessed reverse charge on
  imported services. `POST /v1/tax/reverse-charge`.

### Fixed

- **Direct cash-flow statement** (migration `051`). Was hardcoded to identify
  "cash" as account `1000`, silently undercounting once cash moved through any
  other cash/bank account. Now data-driven on `chart_of_accounts.is_cash_equivalent`
  (seeded on `1000`), and excludes all cash-equivalent legs so transfers between
  two cash accounts net to zero.

### Notes

- New GL accounts are seeded via `SeedChartOfAccounts` in addition to their
  migrations, so behaviour is identical on a fresh database regardless of seed/
  migration order.
- Deferred within these standards (future work): post-acquisition NCI share of
  profit, goodwill impairment, unrealised-profit-in-inventory and fair-value
  step-ups (IFRS 10/3); compound/multi-component tax codes (IAS 12 sits on
  single-rate VAT). See `docs/GAP_REMEDIATION_ROADMAP.md`.
