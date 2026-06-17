# Finance — multi-currency (FX) integration

How `iag-finance` handles foreign currencies: the base/reporting currency,
exchange rates, what currency API amounts are in, how reports aggregate, and the
rules integrators must follow.

---

## 1. Model

- **Base (reporting) currency** — configured by `BASE_CURRENCY` (default `UGX`).
  All financial reports are expressed in this currency.
- **Transaction currency** — the currency a document/payment is actually in
  (e.g. `USD`). Open items and journal lines carry their own currency.
- **Historical-rate method with FX gain/loss** — each document is booked at one
  FX rate captured at creation (`ar/ap_open_items.fx_rate`). On settlement the
  cash leg is valued at the **payment-date** rate and the clearing leg at the
  **document** rate; the base residual is posted as **realized FX gain/loss**
  (`7200`/`7210`). Open foreign balances can be **revalued** at period end
  (`7220`/`2900`, auto-reversed). The general-ledger balance invariant is enforced
  in **base** currency, so these mixed-currency entries are valid.

Every journal line stores both the transaction amount (`debit`/`credit`,
`currency`) and its base-currency equivalent (`debit_base`/`credit_base` =
nominal × rate). Because all lines of an entry share one rate, base debits always
equal base credits — the balanced-entry guarantee holds in base too.

---

## 2. Exchange rates API

Rates are currency→base, effective from a date (latest on/before the lookup date
wins). A base-currency rate is always `1` and need not be configured.

```http
GET  /api/v1/finance/v1/fx/rates            # list recent rates + baseCurrency
POST /api/v1/finance/v1/fx/rates            # upsert a rate (finance.change_ledger)
POST /api/v1/finance/v1/fx/revalue?period=YYYY-MM   # period-end revaluation (idempotent)
```

```jsonc
// POST body
{ "currency": "USD", "rate": "3700.50", "asOfDate": "2026-06-01" }  // asOfDate optional → today
```
```jsonc
// GET response
{ "baseCurrency": "UGX",
  "rates": [ { "currency": "USD", "baseCurrency": "UGX", "rate": "3700.50000000", "asOfDate": "2026-06-01" } ] }
```

`rate` must be a positive decimal. If **no** rate is configured for a foreign
currency, finance degrades to `1:1` rather than blocking the write — so configure
rates before transacting in a new currency, or base reports will be wrong.

---

## 3. What currency are amounts in?

| Surface | Currency |
|---------|----------|
| `amount` on AR/AP open items, payments, adjustments (request + response) | **Transaction currency** (the item's `currency`) |
| Journal line `debit`/`credit` | Transaction currency (line `currency`) |
| Reports: trial balance, P&L, balance sheet, GL account detail, AR/AP aging, `finance/summary` | **Base currency** (converted via the stored rate) |

So a USD invoice is created and paid in USD, but appears in the trial balance and
balance sheet converted to UGX.

---

## 4. Rules for integrators

1. **Create the rate first.** Before posting/booking in a non-base currency,
   `POST /fx/rates` for that currency. The document captures the rate as-of its
   creation day.
2. **Pay in the document's currency.** A payment whose `currency` differs from the
   open item's currency is rejected with **422** (`payment currency must match the
   open item currency`). Cross-currency settlement is not auto-converted.
3. **Set `currency` on event payloads.** `sale.completed` / `invoice.posted`
   should carry `currency`; finance converts at the event-date rate. Omitted →
   base currency assumed.
4. **Don't sum mixed-currency `amount` fields yourself.** Open-item `amount` is in
   the item's own currency. For cross-currency totals, use the reports (already in
   base) — e.g. `finance/summary`, `reports/ar-aging`.

---

## 5. Worked example

```
BASE_CURRENCY = UGX, rate USD→UGX = 3700 (as of 2026-06-01)

POST /ar/items { customerRef, documentRef: "INV-USD-1", amount: "10.00", currency: "USD" }
  → ar_open_items.fx_rate = 3700 captured

sale.completed:INV-USD-1 booked:
  journal_lines: Dr 1100 AR  10 USD  (debit_base 37000)
                 Cr 4000 Rev 10 USD  (credit_base 37000)

POST /ar/items/:id/payments { amount: "10.00", currency: "USD", paymentRef: "RCT-1" }
  → books Dr Cash / Cr AR at the SAME 3700 rate → AR base clears to 0 exactly

GET /reports/trial-balance → AR and Cash shown in UGX (×3700)
```

---

## 6. Supported & limitations

- **Realized FX gain/loss** — recognised on settlement to `7200`/`7210`.
- **Unrealized FX (period-end revaluation)** — `POST /fx/revalue?period=` revalues
  open foreign AR/AP to the period-end rate (`7220`/`2900`) and auto-reverses on
  the first of the next period. Idempotent per period; re-run after a rate
  correction requires reversing the prior run first. Currencies with no
  period-end rate are skipped.
- **Cross-currency payments** (pay a USD invoice in EUR) — still **rejected**, not
  converted; pay in the document's currency.
