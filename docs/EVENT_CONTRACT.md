# Finance — event-bus integration contract

How other platform services integrate with `iag-finance` over Kafka: the events
it **consumes**, the events it **produces**, the envelope shape, delivery
guarantees (transactional outbox + at-least-once + idempotency), and ready-made
integration recipes.

For REST/BFF integration see [FRONTEND_INTEGRATION.md](./FRONTEND_INTEGRATION.md);
for correctness rules (idempotency keys, error model, periods) see
[INTEGRATION_CONTRACTS.md](./INTEGRATION_CONTRACTS.md); for currency see
[MULTICURRENCY.md](./MULTICURRENCY.md).

---

## 1. Envelope

Every event is a CloudEvents-compatible envelope. Domain data lives under `data`.

```json
{
  "id": "sale.completed:INV-1001",
  "type": "sale.completed",
  "time": "2026-06-16T09:30:00.123456789Z",
  "source": "iag.finance",
  "specversion": "1.0",
  "correlationId": "optional-trace-id",
  "data": { "amount": "1500.00", "currency": "UGX", "documentRef": "INV-1001" }
}
```

- **`id` is the idempotency key.** Producers set a stable id per business document
  (e.g. `sale.completed:INV-1001`), not a random UUID, so a redelivery is
  recognised as the same event. Finance dedupes on it (see §4).
- **`source`** is the emitting service (`iag.finance` for finance-produced events).
- **`time`** is RFC3339Nano UTC. The ERP payroll mirror uses it for out-of-order
  protection — emit a correct `time`.

---

## 2. Events finance CONSUMES

Enable with `ENABLE_CONSUMER=true`. Each topic has its own consumer group so
finance can be scaled and replayed independently.

| Topic | Consumer group | Event types | Effect in finance |
|-------|----------------|-------------|-------------------|
| `iag.finance` | `iag.finance.ledger` | `sale.completed`, `invoice.posted` | Books the GL entry (AR/Revenue or Expense/AP) and links the open item |
| `iag.fleet` | `iag.finance.fleet` | `fleet.fuel.recorded` | Books fuel expense / AP |
| `iag.supply-chain` | `iag.finance.supply-chain` | `scm.party.created`, `scm.party.updated` | Backfills `party_id` on AP open items (vendor portal linkage) |
| `iag.commercial` | `iag.finance.commercial` | `procurement.invoice.received`, `procurement.grn.posted`, `contracts.payment.authorized` | AP open item (and emits `invoice.posted`); GR/IR accrual at goods receipt |
| `iag.operations` | `iag.finance.erp` | `erp.employee.*`, `erp.leave.*` | Mirrors employees/leave for payroll prep ([PAYROLL_ERP_BOUNDARY.md](./PAYROLL_ERP_BOUNDARY.md)) |
| `iag.operations` | `iag.finance.warehouse` | `warehouse.asset.disposed` | Books gain/loss on disposal and de-recognises the fixed asset (system NBV when capitalised, else carried book value) |

### Required payload fields

| Event | Required `data` fields | Notes |
|-------|------------------------|-------|
| `sale.completed` | `amount`, `documentRef` | `currency` (default `UGX`), `customerRef` recommended. `documentRef` links the AR open item. |
| `invoice.posted` | `amount`, `documentRef` | `currency`, `vendorRef` recommended. |
| `fleet.fuel.recorded` | `amount` | `currency`, `documentRef`, `vehicleId` optional. |
| `procurement.invoice.received` | `documentRef`, `amount` | `vendorRef`, `currency`, `dueDate` (`YYYY-MM-DD`), `description` optional. Optional `poRef` clears the matching GR/IR accrual instead of double-booking expense; optional `vatAmount` (portion of the gross `amount`) is split to the VAT control account. Missing required fields → **permanent** error → DLQ. |
| `procurement.grn.posted` | `po_id`, `amount` | Books the GR/IR accrual `Dr expense / Cr GR-IR clearing` for the received value, keyed to the PO so the later invoice (carrying the same `poRef`) clears it. No-op without `po_id`/`amount`. Optional `currency` (default `UGX`). |
| `warehouse.asset.disposed` | `asset_tag`, `method`, `currency` | `proceeds` (number, default 0) and optional `book_value` used only when the asset is **not** in the FA subledger; when capitalised, system cost + accumulated depreciation are used instead. |
| `contracts.payment.authorized` | `paymentId`, `payable` (number) | `contractor`, `contractNumber` optional. `documentRef` is derived as `CT-PAY-<paymentId>`. |
| `scm.party.created` / `scm.party.updated` | party id + name/business id | Matched against `ap_open_items.vendor_ref`. |

Money in event payloads should be a **decimal string** (`"1500.75"`). The
contract-payment path tolerates a JSON number for `payable` but strings are safer.

---

## 3. Events finance PRODUCES

Enabled when Kafka is configured and `ENABLE_EVENT_PUBLISH=true` (default on with
brokers). All are emitted on the finance topic (`KAFKA_TOPIC`, logically
`iag.finance`) with `source = iag.finance`.

| Event type | Emitted when | `id` (idempotency key) | `data` |
|------------|--------------|------------------------|--------|
| `sale.completed` | `POST /v1/ar/items` (and legacy invoice create) | `sale.completed:<documentRef>` | `amount`, `currency`, `customerRef`, `documentRef` |
| `invoice.posted` | `POST /v1/ap/items`, and the procurement-invoice consumer | `invoice.posted:<documentRef>` | `amount`, `currency`, `vendorRef`, `documentRef` (and `poRef`/`vatAmount` passed through from `procurement.invoice.received`) |
| `finance.payment.made` | `POST /v1/ar/items/:id/payments` and `.../ap/...` | `finance.payment.made:<direction>:<openItemId>:<paymentRef>` | `direction` (`ar`/`ap`), `openItemId`, `amount`, `currency`, `paymentRef` |
| `finance.efris.submitted` | EFRIS submission acknowledged by URA | `finance.efris.submitted:<documentRef>` | `documentRef`, `uraReceipt` |
| `notification.requested` | e.g. invoice-ready email | — | on `iag.notifications`; channel/recipient/templateId/variables |

The partition key is the business id (documentRef / openItemId), so all events
for one document are ordered.

> Note: `sale.completed` and `invoice.posted` are both consumed **and** produced
> by finance. The REST create endpoints publish them; the `iag.finance.ledger`
> consumer books them. This lets external services book finance GL by publishing
> the same event types directly to `iag.finance`.

---

## 4. Delivery guarantees

### Producer — transactional outbox
Finance never publishes fire-and-forget. The event row is written to the
`event_outbox` table **inside the same database transaction** as the state change
(AR/AP item creation, payment, etc.). A relay worker then delivers it:

- The state change and its event commit atomically — a broker outage can never
  leave (say) an AP item without its `invoice.posted`.
- The relay polls unpublished rows (~5s), publishes with retry, and marks them
  sent. Delivery is **at-least-once** — consumers must be idempotent.
- Outbox rows are unique on `event_id`, so re-enqueueing the same event is a no-op.

### Consumer — at-least-once + idempotent
- Offsets are committed only **after** successful processing; transient failures
  retry with exponential backoff; decode/permanent failures go to the **DLQ**
  (`KAFKA_DLQ_TOPIC`, default `iag.finance.dlq`) instead of poison-looping.
- Idempotency is enforced in the database: a `processed_events` row plus a
  **unique** `journal_entries.source_event_id`. Redelivering `sale.completed:INV-1`
  books exactly one journal entry; the second attempt returns the existing one.

**What integrators must do:** make your own consumers idempotent on `envelope.id`,
and emit a **stable** `id` per document so finance's dedupe works.

---

## 5. Integration recipes

**Book revenue in finance from your service** — publish to `iag.finance`:
```json
{ "id": "sale.completed:INV-1001", "type": "sale.completed", "source": "iag.sales",
  "time": "2026-06-16T09:30:00Z",
  "data": { "amount": "1500.00", "currency": "UGX", "customerRef": "CUST-7", "documentRef": "INV-1001" } }
```
Finance books `Dr AR / Cr Revenue` once, idempotent on the id.

**React to a finance payment** — subscribe to `iag.finance`, filter
`type == "finance.payment.made"`, dedupe on `id`, and (e.g.) close your sales
order when `direction == "ar"`.

**React to URA fiscalisation** — subscribe to `finance.efris.submitted` for the
`uraReceipt` once an invoice is acknowledged.

---

## 6. Configuration

| Env | Purpose | Default |
|-----|---------|---------|
| `ENABLE_CONSUMER` | Run the Kafka consumers | `true` in prod/staging |
| `ENABLE_EVENT_PUBLISH` | Publish finance domain events (via outbox) | on when brokers set |
| `KAFKA_BROKERS` | Broker list | — |
| `KAFKA_TOPIC` | Finance topic (consume + produce) | `iag.finance` |
| `KAFKA_GROUP_ID` | Ledger consumer group | `iag.finance.ledger` |
| `KAFKA_SUPPLY_CHAIN_TOPIC` / `KAFKA_COMMERCIAL_TOPIC` / `KAFKA_OPERATIONS_TOPIC` | Cross-domain topics | `iag.supply-chain` / `iag.commercial` / `iag.operations` |
| `KAFKA_NOTIFICATIONS_TOPIC` | Notifications topic | `iag.notifications` |
| `KAFKA_DLQ_TOPIC` | Dead-letter topic | `iag.finance.dlq` |
