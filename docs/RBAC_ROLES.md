# iag-finance — RBAC Role Model

Standard finance roles, organised to mirror the role structures of established
accounting systems (SAP S/4HANA, QuickBooks Online, Xero, Zoho Books) and to
enforce **segregation of duties (SoD)**.

## Design principles

1. **Least privilege by function.** Each role grants only what a job function
   needs. Day-to-day clerks are scoped to their sub-ledger (AR *or* AP *or*
   banking) rather than the whole ledger.
2. **Segregation of duties.** The person who *creates* a transaction is not the
   person who *posts/approves* it. Journals split into create vs post; payments
   above a threshold route through the tiered maker-checker workflow.
3. **Read-only auditor.** A dedicated view-everything / change-nothing role for
   auditors and reviewers.
4. **Backward compatible.** `finance.change_ledger` remains a superset that
   satisfies every write gate, so existing grants keep working. New granular
   codenames (`manage_ar`, `manage_ap`, `manage_banking`, `create_journal`,
   `post_journal`) only *narrow* a role when `change_ledger` is omitted.

## Permission taxonomy

`finance.<action>_<object>`. Actions: `view_*` (read), `change_*` (broad write),
`manage_*` (granular domain write), plus specific verbs (`post_journal`,
`reverse_journal`, `close_period`, `issue_invoice`, `collect_payment`,
`approve_tier{1,2,3}`, …).

New codenames added for module-scoped SoD:

| Codename | Gates |
|---|---|
| `finance.create_journal` | create draft journal entries (`POST /ledger/entries`, validate-posting) |
| `finance.post_journal` | post a draft to the GL (`POST /ledger/entries/:id/post`) — the *checker* half of maker-checker |
| `finance.manage_ar` | AR writes: AR items, receipts, credit/debit notes, late fees, billing invoices/receipts |
| `finance.manage_ap` | AP writes: AP items/bills, vendor payments, debit notes |
| `finance.manage_banking` | bank statements, reconciliation, match/confirm/reject, sync |

All five are registered as `granular(p)` gates — i.e. the route accepts the
narrow codename **or** `finance.change_ledger` — so nothing breaks for existing
`change_ledger` holders while narrow roles become expressible.

## Roles

| Role (group) | Mirrors | Grant summary |
|---|---|---|
| `finance-administrator` | QB **Company Admin**, Zoho **Admin**, SAP superuser | Everything: all sub-ledgers, journals create+post, close period, entities/consolidation, all approval tiers. |
| `finance-controller` | Xero **Adviser**, SAP **Controller**, Zoho **Admin (finance)** | Full accounting authority: journals create+post+reverse, CoA, close period + year-end, consolidation, FX/tax, all subledgers, **approve tier 1-3**. No user administration. |
| `finance-accountant` | Zoho **Accountant**, SAP **GL Accountant**, Xero **Standard**+journals | GL + journals (create **and** post), CoA, reconciliation, dimensions, fixed assets & all IFRS/IAS subledgers, budgets, reports. **No** period close, entities, or approvals. |
| `finance-ar-clerk` | QB **Customers & Sales**, SAP **AR Accountant**, Zoho **Sales** | Customers, sales invoices/recurring, receipts, credit notes, AR reports. **No** AP, banking, journals, or period close. |
| `finance-ap-clerk` | QB **Vendors & Purchases**, SAP **AP Accountant**, Zoho **Purchases** | Vendors, bills/AP items, vendor payments (entry), debit notes, AP reports. **No** AR, banking, or journals. Payments over threshold still need an approver. |
| `finance-cashier` | SAP **Cash Manager**, Xero **Standard (bank)** | Banking, bank reconciliation, payment intents, FX rates/revaluation. **No** AR/AP master or journals. |
| `finance-tax` | SAP **Tax Specialist**, Zoho **GST/Tax** | Tax codes, VAT/WHT/income & deferred tax, reverse-charge, URA EFRIS filing. Read ledger for context. |
| `finance-approver` | SAP **checker**, maker-checker approver | Read ledger + **approve tiers 1-3**. Holds no write grant (cannot also create what it approves). |
| `finance-auditor` | Xero **Read Only**, SAP **Auditor/Display**, QB **Reports only** | All `view_*` (ledger, operations, consolidated, payroll). Zero writes. |
| `finance-clerk` *(legacy)* | QB **Standard (limited)** | Broad ledger/operations data entry via `change_ledger`. Retained for backward compatibility; prefer the scoped clerk roles above. |

## Segregation-of-duties matrix (who can do what)

| Capability | ar-clerk | ap-clerk | cashier | accountant | controller | approver | auditor |
|---|:--:|:--:|:--:|:--:|:--:|:--:|:--:|
| Create sales invoice / receipt | ✔ | | | ✔ | ✔ | | |
| Create bill / vendor payment | | ✔ | | ✔ | ✔ | | |
| Bank reconciliation | | | ✔ | ✔ | ✔ | | |
| Create journal (draft) | | | | ✔ | ✔ | | |
| **Post** journal to GL | | | | ✔ | ✔ | | |
| Reverse posted journal | | | | | ✔ | | |
| Close period / year-end | | | | | ✔ | | |
| Approve high-value (tier 1-3) | | | | | ✔ | ✔ | |
| Manage entities / consolidation | | | | | ✔ | | |
| View reports & ledger | ✔(AR) | ✔(AP) | ✔ | ✔ | ✔ | ✔ | ✔ |

Notes:
- **Maker-checker on journals:** `finance-accountant`/`controller` hold both
  `create_journal` and `post_journal`. To enforce a strict maker≠checker split,
  grant a data-entry role `create_journal` only and reserve `post_journal` for a
  senior reviewer.
- **Payments maker-checker:** the tiered approval workflow (`approve_tier{1,2,3}`)
  already blocks self-approval and double-tier approval by the same actor; the
  `finance-approver` role deliberately holds no create grant.
- **Superuser / `superadmin` group** bypasses all gates (platform administration)
  — outside this finance role model.

Roles are seeded as auth groups (see `iag-authentication`
`internal/domain/finance_view.go` + `internal/auth/service.go`) from the finance
permission catalogue (`internal/models/permissions.go`). Admins may rename or
retune them; none are hard-coded into enforcement.
