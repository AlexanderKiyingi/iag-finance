# Finance gap-remediation roadmap (Manager-parity)

Status of the Manager.io-benchmark gaps after the 2026-07 remediation. The
**Done** set shipped this cycle. The **Blocked** set is implementation-ready but
gated on an infra decision, a cross-service change, or the finaceiag thin-client
refactor settling — each is specced below so it can be executed in one sitting
once unblocked. The **Declined** set is intentionally not built.

---

## Done (shipped)
Cash-flow & budget-vs-actual reports · customer statements · GL account
drill-down · audit history · invoice PDF · period close/reopen + year-end +
depreciation run (period close = the lock-date) · recurring invoices ·
control-account reconciliation (`/reports/control-reconciliation`) ·
sales-by-item · statement of changes in equity · opening balances ·
**email invoices** (`POST /ar/invoices/:documentRef/email` → notifications).

## Declined (intentional)
- **Multi-component / compound tax codes** — single-rate VAT (STD18/ZERO/EXEMPT,
  migration 029) covers the Uganda case. Revisit only if a compound tax (e.g.
  VAT + a separate levy on the same line) becomes a real requirement.
- **Invoice themes** — marginal; only worth it with a frontend theme picker.
  If wanted: add a `?theme=` param to `GET /ar/invoices/:ref/pdf` selecting
  between layouts in `internal/pdf/`, plus a company-level default setting.

---

## Blocked 1 — Attachments (needs object-store provisioning)

**Why blocked:** finance has no object-store config (`internal/config/config.go`
has no S3 bucket/creds), no S3 SDK dependency, and the reusable presign pattern
lives in the contract-management service, which is **not checked out** in this
tree. Building it blind would add a heavy dependency for an inert feature.

**Decision needed from you:** bucket name, region, credentials source
(IAM role vs static keys), retention/ops budget.

**Then implement (config-gated, degrades like notifications):**
1. Dependency: `github.com/aws/aws-sdk-go-v2` (s3 + s3/presigned) in `go.mod`.
2. Config: `S3_BUCKET`, `S3_REGION`, `S3_ENDPOINT` (for MinIO), credentials via
   the default chain. Add `AttachmentsEnabled()` on config (true iff bucket set)
   — every endpoint 503s cleanly when unset, exactly like `NotificationsEnabled()`.
3. Migration `04x_attachments.sql`:
   ```sql
   CREATE TABLE attachments (
     id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
     entity_id UUID NOT NULL DEFAULT '…0001' REFERENCES entities(id),
     resource_type TEXT NOT NULL,   -- 'invoice' | 'bill' | 'journal_entry' | …
     resource_id   TEXT NOT NULL,
     object_key    TEXT NOT NULL,   -- s3 key: entity/resource_type/resource_id/uuid-filename
     filename      TEXT NOT NULL,
     content_type  TEXT NOT NULL,
     size_bytes    BIGINT NOT NULL DEFAULT 0,
     uploaded_by   TEXT,
     created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
   );
   CREATE INDEX ON attachments (entity_id, resource_type, resource_id);
   ```
4. Endpoints (`internal/handlers/attachments_api.go`):
   - `POST /v1/attachments/presign` → `{resourceType,resourceId,filename,contentType}` → returns a presigned PUT URL + the pending object key. Gate `finance.change_ledger`.
   - `POST /v1/attachments` → confirm upload, insert the row. Gate `finance.change_ledger`.
   - `GET  /v1/attachments?resourceType=&resourceId=` → list (ledgerRead).
   - `GET  /v1/attachments/:id/download` → presigned GET URL (ledgerRead).
   - `DELETE /v1/attachments/:id` → delete row + object. Gate `finance.change_ledger`.
5. Frontend: an "Attachments" section on `RecordDetailScreen` (presign → PUT →
   confirm → list), reusing the `SideDrawer` pattern.

Effort once unblocked: ~1 day. No accounting risk (metadata only).

---

## Blocked 2 — Custom fields (defer until the finaceiag refactor settles)

**Why blocked:** buildable in finance, but it's cross-cutting (schema + every
entity's create/update + a dynamic form) and finaceiag is mid thin-client
refactor — landing it now tangles repeatedly (it did twice in the last session).

**Then implement:**
1. Migration: `custom_field_defs (id, entity_id, resource_type, key, label, type[text|number|date|select], options JSONB, sort_order, active)` +
   a `custom JSONB NOT NULL DEFAULT '{}'` column on the target tables
   (`invoices`, `ap_open_items`, `journal_entries` to start — not all at once).
2. Endpoints: `GET/POST /v1/custom-fields` (defs, gate `finance.manage_dimensions`);
   create/update handlers merge a validated `custom` object onto the record.
3. Frontend: `FormScreen` fetches defs for the module and renders extra inputs;
   `RecordDetailScreen` shows them; include in CSV export.

Effort once unblocked: ~1.5 days. Scope to invoices first, expand per demand.

---

## Blocked 3 — Perpetual inventory (cross-service; warehouse-owned)

**Why blocked:** by design (see `032_inventory_account.sql`), inventory valuation
lives in **iag-warehouse**, not finance. Finance only books the GL from warehouse
events. This spans warehouse + finance + procurement and is its own project.
Control account `1400 Inventory` and `2150 GR/IR Clearing` already exist; there
is no `5000 COGS` account yet.

**Cross-service design:**
1. **iag-warehouse** owns item master, quantities, and weighted-average cost;
   on stock receipt/issue/adjustment it emits events (extend
   `docs/EVENT_CONTRACT.md`):
   - `warehouse.stock.received {itemRef, qty, unitCost, totalCost, ref}`
   - `warehouse.stock.issued   {itemRef, qty, avgCost, totalCost, ref}` (e.g. on sale/dispatch)
   - `warehouse.stock.adjusted {itemRef, deltaQty, deltaValue, reason, ref}`
2. **finance** consumes them (existing `internal/consumer`) and books, idempotent
   on `ref`:
   - received  → Dr 1400 Inventory / Cr 2150 GR/IR
   - issued    → Dr 5000 COGS       / Cr 1400 Inventory   (needs a new `5000 COGS` COA seed)
   - adjusted  → Dr/Cr 1400 vs a shrinkage/gain expense account
3. **procurement** switches GR/IR from periodic expense to inventory
   capitalisation so the GR/IR clearing nets against supplier bills.

First finance-side step (safe, no behaviour change): seed `5000 COGS` and add the
consumer handlers behind a feature flag, dormant until warehouse emits the events.

Effort: multi-day, cross-team. Do NOT build a finance-local inventory master —
it would duplicate/contradict warehouse's role.
