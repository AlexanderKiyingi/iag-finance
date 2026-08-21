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
- **`time` is the accounting date.** RFC3339Nano UTC, and the date the resulting
  journal entry is filed under — not the moment finance read the message. It is
  also the FX measurement date: foreign-currency amounts convert to base at the
  rate ruling on that day (IAS 21). The ERP payroll mirror additionally uses it
  for out-of-order protection. **Emit a correct `time`**; an envelope without one
  falls back to arrival, which is a cut-off error waiting to happen. See §4.1.

---

## 2. Events finance CONSUMES

Enable with `ENABLE_CONSUMER=true`. Each topic has its own consumer group so
finance can be scaled and replayed independently.

| Topic | Consumer group | Event types | Effect in finance |
|-------|----------------|-------------|-------------------|
| `iag.finance` | `iag.finance.ledger` | `sale.completed`, `invoice.posted` | Books the GL entry (AR/Revenue or Expense/AP) and links the open item |
| `iag.fleet` | `iag.finance.fleet` | `fleet.fuel.recorded` | Raises the fuel AP item; the journal follows from the `invoice.posted` it enqueues |
| `iag.payments` | `iag.finance.payments` | `payments.settled` | Books the disbursement `Dr 1050 Payments Clearing / Cr 1000 Cash` — or `Dr 1400 Inventory` for `coffee_payout`, which has no prior finance document |
| `iag.supply-chain` | `iag.finance.supply-chain` | `scm.party.created`, `scm.party.updated`, `scm.party.portal_linked`, `scm.farmer_payment.recorded` | Backfills `party_id` on AP open items, lands vendor-type parties in the vendor master, records portal user↔party links, and raises the **coffee payable** (§2.2) |
| `iag.commercial` | `iag.finance.commercial` | `procurement.invoice.received`, `procurement.grn.posted`, `contracts.payment.authorized` | AP open item (and emits `invoice.posted`); GR/IR accrual at goods receipt |
| `iag.commercial` | `iag.finance.vendor-sync` | `party.vendor.upserted` | Vendor master mesh — upserts the finance vendor keyed on the shared `party_id`. Own group so it advances independently of the AP inbox on the same topic. Events sourced `iag.finance` are skipped (loop guard) |
| `iag.operations` | `iag.finance.erp` | `erp.employee.created`, `erp.employee.updated`, `erp.employee.terminated`, `erp.leave.approved`, `erp.leave.rejected`, `erp.leave.cancelled`, `erp.leave.balance_changed`, `erp.employee.rate_changed`, `erp.payroll.run_posted` | Mirrors employees/leave for payroll prep ([PAYROLL_ERP_BOUNDARY.md](./PAYROLL_ERP_BOUNDARY.md)); measures the leave liability; **books the payroll journal** from a released run |
| `iag.operations` | `iag.finance.warehouse` | `warehouse.asset.disposed`, `warehouse.movement.posted` | Gain/loss on disposal and fixed-asset de-recognition (system NBV when capitalised, else carried book value); perpetual-inventory GL from valued stock movements |

`internal/consumer/handled.go` is the authoritative list, and
`TestEventContractDocumentsEveryHandledType` fails the build if a type or group
reaches production without a row above.

### Required payload fields

| Event | Required `data` fields | Notes |
|-------|------------------------|-------|
| `sale.completed` | `amount`, `documentRef` | `currency` (default `UGX`), `customerRef` recommended. `documentRef` links the AR open item. |
| `invoice.posted` | `amount`, `documentRef` | `currency`, `vendorRef` recommended. |
| `fleet.fuel.recorded` | `amount` | `currency`, `documentRef`, `vehicleId` optional. |
| `procurement.invoice.received` | `documentRef`, `amount` | `vendorRef`, `currency`, `dueDate` (`YYYY-MM-DD`), `description` optional. Optional `poRef` clears the matching GR/IR accrual instead of double-booking expense; optional `vatAmount` (portion of the gross `amount`) is split to the VAT control account. Missing required fields → **permanent** error → DLQ. |
| `procurement.grn.posted` | `po_id`, `amount` | **The only accounting event for a receipt against a PO** — see §2.1. Books the GR/IR accrual for the received value, keyed to the PO so the later invoice (carrying the same `poRef`) clears it. Optional `inventory_value` is the stockable portion of `amount`: that part debits `1400 Inventory`, the remainder `5000` expense. Omit it and everything expenses, as before. No-op without `po_id`/`amount`. Optional `currency` (default `UGX`). |
| `warehouse.asset.disposed` | `asset_tag`, `method`, `currency` | `proceeds` (number, default 0) and optional `book_value` used only when the asset is **not** in the FA subledger; when capitalised, system cost + accumulated depreciation are used instead. |
| `contracts.payment.authorized` | `paymentId`, `payable` (number) | `contractor`, `contractNumber`, `currency` optional — but send `currency`: without it the payable is booked as UGX and a wrongly-denominated payable is invisible once booked. `documentRef` is derived as `CT-PAY-<paymentId>`. |
| `scm.party.created` / `scm.party.updated` | `party_id`, `supplier_type` | `party_business_id`, `name` optional. Only `vendor`/`cooperative`/`farmer` types are acted on; `vendor` also lands in the finance vendor master. Matched against `ap_open_items.vendor_ref`. |
| `scm.party.portal_linked` | `platform_user_id`, `party_id` | `party_business_id`, `supplier_type` optional. Scopes the vendor/farmer portal to its own documents. |
| `party.vendor.upserted` | `party_id` | `code`, `name`, `email`, `phone`, `currency`, `status` optional. Upsert is naturally idempotent, so no `processed_events` row is written. |
| `payments.settled` | `instructionId`, `amountMinor` (integer), `currency` | Debits `1050` Payments Clearing (see §5a of INTEGRATION_CONTRACTS.md), and `coffee_payout` joins them once `COFFEE_PAYOUT_VIA_CLEARING=true` — see §2.2. `referenceNumber`, `partyBusinessId`, `originService`, `category`, `providerRef` optional. UGX has no subunit here, so `amountMinor` is the shilling amount. |
| `scm.farmer_payment.recorded` | `business_id`, `party_business_id`, `gross_ugx` | Raises the coffee payable `CHERRY-<business_id>` and capitalises the purchase (§2.2). `batch_business_id`, `kg`, `price_per_kg_ugx`, `currency` (default `UGX`) optional. A record with no positive gross — an advance taken before any coffee was priced — books nothing. |
| `warehouse.movement.posted` | `movement_type`, `total_cost` | Dormant by design: a movement with no/zero `total_cost` books nothing, so finance can run ahead of warehouse's costing engine. `source_doc_type` names the upstream document — `procurement_grn` means the delivery is already accrued from `procurement.grn.posted` and this movement books nothing (§2.1). `ref` and `currency` optional. Idempotent on the envelope id (the warehouse `movement_id`). |
| `erp.payroll.run_posted` | `run_ref`, `period` | Period totals only, never per-employee. `gross`, `paye`, `nssf_employee`, `nssf_employer`, `other_deductions`, `net`, `currency`, `employee_count`. A run that does not balance, or one dated into a closed period, is **permanent** → DLQ. |
| `erp.leave.balance_changed` | `employee_no`, `accrual_year` | `leave_type_code`, `balance_days`. Stores days, not money — valuing them is a separate deliberate act. |
| `erp.employee.rate_changed` | `employee_no`, `daily_rate` (positive), `effective_from` | `currency` (default `UGX`). The derived daily rate only — never gross or benefits. |
| `erp.employee.*` / `erp.leave.*` | `employee_no` (and `leave_request_id`, `starts_on`, `ends_on` for leave) | Mirror rows for payroll prep. `time` on the envelope drives out-of-order protection — emit a correct one. |

Money in event payloads should be a **decimal string** (`"1500.75"`). The
contract-payment path tolerates a JSON number for `payable` but strings are safer.

### 2.1 One delivery, one posting

A purchase-order delivery raises two events: `procurement.grn.posted` when the
goods receipt is posted, and `warehouse.movement.posted` for the same goods once
they are put away. **Only the first books the general ledger.**

Both used to. Each credited `2150 GR/IR Clearing` for the receipt value while the
vendor invoice cleared it once, so the cost was recognised twice — once as
expense, once as inventory — and 2150 kept a permanent credit. Neither entry is
unbalanced, so nothing downstream complained.

The GRN owns the posting because it is the event that exists for *every* PO
receipt, including purchases that never reach a warehouse, and because the
three-way match is already keyed to it by PO reference. Warehouse therefore
stamps `source_doc_type: "procurement_grn"` on movements raised from a GRN and
finance treats those as cost-neutral. Stock arriving by any other route — field
intake, a customer return, production output — carries no `source_doc_type` and
books from the movement exactly as before.

**Rollout order matters.** Finance's suppression and procurement's
`inventory_value` split must both be deployed *before* warehouse starts pricing
GRN-raised receipts, or the window between them double-counts. Warehouse
receipts drafted from a GRN were unpriced until now, which is the only reason
the defect had never fired in production.

### 2.2 Coffee purchases

A coffee purchase from a farmer reached the books only when iag-payments
settled, so between delivery and payment the ledger showed neither the stock nor
the money owed for it — the platform's core purchase was, in effect, cash-basis.

`scm.farmer_payment.recorded` fixes that. iag-supply-chain emits it when a
farmer payment record is created, which is the first moment the purchase is both
attributable to a farmer and measurable in money: `price_per_kg_ugx` is set there
and `gross_ugx` computed from it. Finance raises AP item `CHERRY-<business_id>`
and capitalises the gross — `Dr 1400 Inventory / Cr 2000 AP`. Cherry is stock, so
it becomes cost of sales when the coffee sells, not when it arrives.

The payout then settles that payable like any other disbursement, through
Payments Clearing. Because the two halves deploy separately this is gated on
`COFFEE_PAYOUT_VIA_CLEARING` (default off):

| Flag | `coffee_payout` books | Correct when |
|---|---|---|
| off (default) | `Dr 1400 Inventory / Cr 1000 Cash` | SCM is **not** emitting the payable |
| on | `Dr 1050 Clearing / Cr 1000 Cash` | SCM **is** emitting it |

Turn it on only once the payable is flowing. Too early and a payout reaches
neither inventory nor a payable; too late and the same coffee capitalises twice.

> **Known residual gap.** The payable arises when the purchase is *priced*, not
> when the cherry is physically delivered. `scm.intake.received` carries the
> per-farmer `kg` at delivery but no price, and SCM has no floating-price or
> price-settlement mechanism — `advance_ugx`/`balance_ugx` are two instalments of
> an already-fixed gross, not two prices. Accruing at delivery would therefore
> mean inventing a provisional price, which is a separate decision and a much
> larger piece of work. Until then, coffee delivered but not yet priced is
> unrecorded; the window is visible as intakes with no matching `CHERRY-*`
> payable.

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

### 4.1 Dating and late delivery

At-least-once delivery means an event can arrive long after the fact it reports —
a redelivery, a lagging partition, a DLQ replay. Finance dates the posting by
`envelope.time`, so a goods receipt dated 30 June books into June however late it
lands, and converts at June's rate.

When the transaction's own period has already been closed:

- the entry is filed into the **current open period** rather than refused —
  refusing would strand a real transaction behind a month-end close, and a DLQ
  is not a ledger;
- the **FX rate stays the transaction date's**, because that is the measurement
  date, not the filing date;
- the entry description is annotated `[dated YYYY-MM-DD; period closed, filed
  YYYY-MM-DD]`, so the reclassification is visible in the GL rather than only in
  a log line, and a warning is emitted.

If both the transaction's period **and** the current period are closed there is
nowhere honest to file it: the handler returns an error and the event retries.
Reopen a period to let it through.

An envelope with no `time` (or an unparseable one) is dated by arrival — the old
behaviour, kept only so emitters predating this contract still book.

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
| `KAFKA_PAYMENTS_TOPIC` | Disbursement settlements from iag-payments | `iag.payments` |
| `KAFKA_NOTIFICATIONS_TOPIC` | Notifications topic | `iag.notifications` |
| `KAFKA_DLQ_TOPIC` | Dead-letter topic | `iag.finance.dlq` |
