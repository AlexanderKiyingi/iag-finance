# Billing identity → AR customer reference contract

Finance AR open items are keyed by `customer_ref`. That value must align with the **billing identity** maintained in **iag-users** (`shared/services/accounts`).

## Canonical field

| Service | Field | Storage |
|---------|-------|---------|
| Users (billing identity) | `financeCustomerRef` | `billing_identities.finance_customer_ref` |
| Finance (AR) | `customerRef` | `ar_open_items.customer_ref` |

When both are set, they **must match**. Finance resolves `financeCustomerRef` from Users when creating AR via `billingIdentityId` + `orgId`.

## Creating AR from billing identity

```http
POST /api/v1/finance/v1/ar/items
Content-Type: application/json

{
  "orgId": "550e8400-e29b-41d4-a716-446655440000",
  "billingIdentityId": "660e8400-e29b-41d4-a716-446655440001",
  "documentRef": "INV-2026-0042",
  "amount": "1500000.00",
  "currency": "UGX",
  "dueDate": "2026-07-01"
}
```

Resolution order:

1. If `billingIdentityId` and `orgId` are present, finance calls Users  
   `GET /v1/orgs/{orgId}/billing-identities/{billingIdentityId}` (service account, `users.read_billing`).
2. If `financeCustomerRef` is set on the billing identity, that becomes `customerRef`.
3. If `financeCustomerRef` is empty, finance derives a stable slug from `legalName` (uppercase, non-alphanumeric → `-`).
4. If `customerRef` is sent explicitly, it must equal the resolved value when billing identity is also supplied.
5. At least one of `customerRef` or (`orgId` + `billingIdentityId`) is required.

## Direct customer reference (legacy / integrations)

```json
{ "customerRef": "IAG-COFFEE-LTD", "documentRef": "INV-001", "amount": "100" }
```

Allowed when no billing identity is supplied (manual AR, migrations, external systems).

## Metadata persisted on AR rows

When resolved via billing identity, finance stores:

- `billing_org_id` — org UUID from Users
- `billing_identity_id` — billing identity UUID

These support statements, PDFs, and audit; they do not replace `customer_ref` as the join key.

## Events

Users publishes `users.billing_identity.created` / `updated` with `financeCustomerRef` in the payload. Downstream services should use the API or consume events; finance does **not** auto-sync AR rows when billing identity changes.

## Permissions

| Caller | Users permission | Finance permission |
|--------|------------------|-------------------|
| Service account (finance → users) | `users.read_billing` | — |
| Human / BFF creating AR | — | `finance.change_ledger` |

Configure `USERS_API_URL` and `SERVICE_CLIENT_SECRET` on finance for server-side resolution.
