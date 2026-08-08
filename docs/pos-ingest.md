# POS ingest — posting till takings to the ledger

`POST /v1/pos/receipts`

There is no POS microservice on the platform, so till takings had no route to
the general ledger. Rather than stand one up to relay them, the POS backend
posts its receipts here.

This is the synchronous style of [ADR 0001](../../../../docs/adr/0001-finance-integration-style.md):
the caller knows a till sale is revenue, so it states the outcome rather than
publishing a fact for finance to interpret.

## Auth

Bearer token with `finance.ingest_pos`. It is a dedicated permission because
the caller is a machine account for the till backend, not a person with ledger
rights — a POS terminal should not be able to touch anything else in finance.

## Request

```json
{
  "terminalId": "TILL-01",
  "receipts": [
    {
      "reference": "POS-TILL01-000123",
      "type": "sale",
      "gross": "25000",
      "vat": "3814",
      "tender": "cash",
      "currency": "UGX"
    },
    {
      "reference": "POS-TILL01-000124",
      "type": "return",
      "gross": "5000",
      "vat": "763",
      "tender": "card"
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `reference` | The till's own receipt number. **Must be stable across retries** — it is the idempotency key. |
| `type` | `sale` (default) or `return`. A return posts the exact reverse. |
| `gross` | Total the customer paid, VAT included. Decimal string. |
| `vat` | Output VAT within `gross`. Omit or `0` to book the whole amount as revenue. |
| `tender` | `cash`, `card`, `mobile` / `momo` / `mobile_money`. Anything else is treated as cash. |
| `currency` | Defaults to the ledger's base currency when omitted. |

## Postings

A sale of 25,000 with 3,814 VAT, tendered in cash:

| Account | | Debit | Credit |
|---|---|---|---|
| 1000 | Cash | 25,000 | |
| 4000 | Revenue | | 21,186 |
| 2100 | Output VAT | | 3,814 |

Card and mobile money debit **1050 Payments Clearing** instead of 1000. The
money is not in the drawer — the acquirer owes it until settlement, and the
payments bridge relieves 1050 when `payments.settled` arrives. Booking these as
cash would overstate the till and leave the later settlement with nothing to
clear.

A return reverses every line of the equivalent sale.

## Response

```json
{
  "terminalId": "TILL-01",
  "posted": 1,
  "duplicates": 1,
  "rejected": 0,
  "results": [
    { "reference": "POS-TILL01-000123", "status": "posted", "entryId": "…" },
    { "reference": "POS-TILL01-000124", "status": "duplicate" }
  ]
}
```

`200` even when some receipts are rejected. The batch is deliberately **not
atomic**: one malformed receipt must not stop a day's takings reaching the
books, and a blanket error would hide the ones that did post. Read `results`
and resend only what was rejected.

## Retrying

Resending a whole batch is safe. Each receipt is booked through the ledger's
event-sourced path keyed on `reference`, so a receipt already booked comes back
as `duplicate` rather than posting twice. A till that loses its connection
mid-upload should simply send the batch again.

The corollary: **`reference` is a contract.** If the till changes how it derives
the reference, a resend books a second entry instead of colliding with the
first. Treat the format as append-only.

## What this does not do

- **No cash-session or daily-closing reconciliation.** Only individual receipts
  are posted. Variance between counted and expected cash is not modelled, so a
  drawer discrepancy will not appear in the ledger.
- **No cost of sale.** Revenue is recorded, inventory is not relieved — that
  would need the till to send line items and the platform to know which
  warehouse they came from.
- **No back-dating.** The ledger stamps its own posting date, so a batch
  uploaded late lands in the period it was received, not the period it was
  rung up in. Uploading across a period close needs thought before it is relied
  on.
