# Finance Frontend Integration Guide

Comprehensive guide for connecting a **Next.js** app to the finance backend.
Covers auth, gateway paths, the permission model, route catalog, vendor portal,
and cross-service billing identity.

For deployment-side env config see [PLATFORM_INTEGRATION.md](./PLATFORM_INTEGRATION.md)
(note: that doc's gateway-secret auth section is **deprecated** — this guide
reflects the current Bearer+JWKS cutover). For AR billing identity see
[BILLING_IDENTITY_CONTRACT.md](./BILLING_IDENTITY_CONTRACT.md).

---

## 1. Authentication

Finance runs in **platform Bearer+aud mode**. Every request — except health
probes — requires:

```
Authorization: Bearer <jwt>
```

The JWT must carry `aud=iag.finance`. The service verifies signatures locally
against the auth service's JWKS (refreshed periodically) — no callback to auth
on the request hot path.

### Two-hop audience (gateway + finance)

1. **Gateway** verifies the token includes `aud=iag.gateway`.
2. Gateway forwards `Authorization` verbatim (no injected identity headers).
3. **Finance** re-verifies with `aud=iag.finance`.

User tokens issued by auth embed both audiences via `USER_TOKEN_AUDIENCES`.

### Token acquisition

```
┌─────────┐  1. POST /api/v1/authentication/oauth/token  ┌──────────┐
│ Browser │ ────────── grant_type=password ─────────────▶│   Auth   │
│  (FE)   │                                              │ Service  │
│         │◀────── access_token, refresh_token ──────────│          │
└─────────┘                                              └──────────┘
     │  2. Authorization: Bearer <access_token>
     ▼
┌──────────────┐
│ iag-finance  │  (verifies JWT locally via cached JWKS)
└──────────────┘
```

**Frontend responsibilities:**
- Keep `access_token` in memory; refresh ~1 minute before its 15-minute TTL.
- On any 401 from finance, attempt refresh; on 401 from refresh, redirect to
  login.
- On 403, the call passed auth but the user lacks the specific permission —
  hide the UI control rather than retry.

### Common 401 / 403 causes

1. Token expired (refresh).
2. `aud` claim missing `iag.finance` or `iag.gateway` — re-login through auth.
3. Missing **`platform.access_finance`** at the gateway (403 before upstream).
4. Missing `finance.view_ledger` / `finance.change_ledger` (or operations
   aliases) on the route (403 from finance).
5. JWKS rotation in flight (transient, resolves within minutes).

---

## 2. Base URLs

| Environment | API base |
|---|---|
| Local direct | `http://localhost:3006/v1` |
| Local via gateway | `http://localhost:8080/api/v1/finance/v1` |
| Production | `https://iag-api-gateway-production.up.railway.app/api/v1/finance/v1` |

**Always go through the gateway in non-local environments.** It owns rate
limiting, CORS, request IDs, and routes `/api/v1/finance/*` to this service.

**Legacy alias (deprecated):** `/api/v1/accounts/v1/*` mirrors finance RBAC
but new clients should use `/api/v1/finance/v1/*`.

### Required frontend env vars

Copy [frontend.env.example](./frontend.env.example) to your Next.js app as
`.env.local` (local) or platform secrets (production).

```env
# Local (via gateway)
NEXT_PUBLIC_FINANCE_API_URL=http://localhost:8080/api/v1/finance/v1
NEXT_PUBLIC_AUTH_API_URL=http://localhost:8080/api/v1/authentication
NEXT_PUBLIC_GATEWAY_ORIGIN=http://localhost:8080
```

```env
# Production (Railway, via gateway)
NEXT_PUBLIC_FINANCE_API_URL=https://iag-api-gateway-production.up.railway.app/api/v1/finance/v1
NEXT_PUBLIC_AUTH_API_URL=https://iag-api-gateway-production.up.railway.app/api/v1/authentication
NEXT_PUBLIC_GATEWAY_ORIGIN=https://iag-api-gateway-production.up.railway.app
```

### CORS

Origins are configured via `CORS_ALLOW_ORIGIN` (comma-separated). Auth uses
the `Authorization` header — **no cookies**. Include your Next.js origin
(e.g. `http://localhost:3000`) in finance and gateway CORS allowlists.

---

## 3. Permission Model

Finance uses **Django-style `finance.*` codenames**. The catalog is registered
with iag-authentication at startup.

### 3.1 Core codenames

| Codename | Use |
|---|---|
| `finance.view_ledger` | CoA, journal entries, GL reports, AR/AP, invoices, banking, integrations |
| `finance.change_ledger` | Create/post ledger, AR/AP mutations, EFRIS/banking writes |
| `finance.view_operations` | Ops audit, inboxes, payroll mirror, prototype tables (read alias) |
| `finance.change_operations` | Append audit/table rows (write alias) |
| `finance.view_own_ap` | Vendor portal AP |
| `finance.view_own_payment` | Vendor portal payments |

**Catalog source:** [internal/models/permissions.go](../internal/models/permissions.go)

### 3.2 Gateway service gate

Every proxied finance route also requires:

**`platform.access_finance`**

Superusers (`is_superuser` / `superadmin` group) bypass permission checks at
both gateway and service layers.

### 3.3 Staff vs vendor portal

| Surface | Gateway | Service permissions |
|---|---|---|
| Staff ledger UI | `platform.access_finance` + `finance.view_ledger` (GET) or `finance.change_ledger` (mutations) | Same |
| Vendor portal | Authenticated only at gateway | `finance.view_own_ap` or `finance.view_own_payment` |
| Admin monitoring | `requireAdmin` at gateway | Django admin (superuser / staff+admin) |

Portal party links are populated via SCM `scm.party.portal_linked` events.

---

## 4. App boot sequence (Next.js)

Finance has **no single bootstrap endpoint**. Recommended first calls after
login:

| Step | Endpoint | Purpose |
|---|---|---|
| 1 | `GET /api/v1/authentication/v1/users/me` | IAM profile, groups, permissions |
| 2 | `GET /v1/finance/summary` | Dashboard headline metrics |
| 3 | `GET /v1/chart-of-accounts` or `/v1/ar/items` | Primary workspace data |

For vendor portal apps:

| Step | Endpoint | Purpose |
|---|---|---|
| 1 | `GET /v1/portal/me` | Linked party profile |
| 2 | `GET /v1/portal/ap` | Scoped AP items |

---

## 5. Endpoint Catalog

All routes are prefixed with the base URL (§2). Permissions listed are
**service-level**; gateway also enforces `platform.access_finance`.

### 5.1 Public probes (no auth)

| Method | Path | Description |
|---|---|---|
| GET | `/health` | Liveness |
| GET | `/ready` | Readiness (Postgres + Redis) |

Gateway aliases: `/api/v1/finance/health`, `/api/v1/finance/ready`.

### 5.2 General ledger

| Method | Path | Permission |
|---|---|---|
| GET | `/chart-of-accounts` | `finance.view_ledger` |
| POST | `/chart-of-accounts` | `finance.change_ledger` |
| GET | `/ledger/entries` | `finance.view_ledger` |
| GET | `/ledger/entries/:id` | `finance.view_ledger` |
| POST | `/ledger/entries` | `finance.change_ledger` |
| POST | `/ledger/entries/:id/post` | `finance.change_ledger` |
| POST | `/ledger/validate-posting` | `finance.change_ledger` |
| GET | `/reports/trial-balance` | `finance.view_ledger` |
| GET | `/reports/ar-aging` | `finance.view_ledger` |
| GET | `/reports/profit-and-loss` | `finance.view_ledger` |
| GET | `/reports/balance-sheet` | `finance.view_ledger` |
| GET | `/finance/summary` | `finance.view_ledger` |

### 5.3 AR / AP

| Method | Path | Permission |
|---|---|---|
| GET/POST | `/ar/items` | `finance.view_ledger` / `finance.change_ledger` |
| POST/GET | `/ar/items/:id/payments` | `finance.change_ledger` / `finance.view_ledger` |
| GET | `/ar/items/:id/payment-link` | `finance.view_ledger` |
| GET | `/ar/invoices/:documentRef/pdf` | `finance.view_ledger` |
| GET | `/ar/customers/:customerRef/statement` | `finance.view_ledger` |
| POST | `/ar/credit-notes`, `/ar/debit-notes` | `finance.change_ledger` |
| GET/POST | `/ap/items` | `finance.view_ledger` / `finance.change_ledger` |
| POST/GET | `/ap/items/:id/payments` | `finance.change_ledger` / `finance.view_ledger` |
| POST | `/ap/credit-notes`, `/ap/debit-notes` | `finance.change_ledger` |
| GET | `/adjustments` | `finance.view_ledger` |

**AR create with billing identity:** prefer `orgId` + `billingIdentityId` on
`POST /ar/items` — see [BILLING_IDENTITY_CONTRACT.md](./BILLING_IDENTITY_CONTRACT.md).

### 5.4 Legacy invoices & banking shapes

| Method | Path | Permission |
|---|---|---|
| GET/POST | `/invoices` | `finance.view_ledger` / `finance.change_ledger` |
| GET | `/invoices/funnel` | `finance.view_ledger` |
| GET/PATCH/DELETE | `/invoices/:no` | per method |
| GET | `/banking/accounts`, `/banking/transactions` | `finance.view_ledger` |

### 5.5 Integrations

| Method | Path | Permission |
|---|---|---|
| GET | `/integrations/ura-efris` | `finance.view_ledger` |
| POST | `/integrations/ura-efris/submit` | `finance.change_ledger` |
| GET | `/integrations/banking` | `finance.view_ledger` |
| POST | `/integrations/banking/statements` | `finance.change_ledger` |
| GET | `/integrations/banking/statements/:id/lines` | `finance.view_ledger` |
| POST | `/integrations/banking/statements/:id/reconcile/auto` | `finance.change_ledger` |
| POST | `/integrations/banking/lines/:lineId/match` | `finance.change_ledger` |
| POST | `/integrations/banking/sync` | `finance.change_ledger` |

### 5.6 Operations inboxes & payroll mirror

| Method | Path | Permission |
|---|---|---|
| GET | `/inbox/bank-accounts` | `finance.view_operations` |
| GET | `/inbox/ap` | `finance.view_operations` |
| GET | `/inbox/cherry-intake` | `finance.view_operations` |
| GET | `/payroll/employees` | `finance.view_operations` |
| GET | `/payroll/leave-accruals` | `finance.view_operations` |

### 5.7 Vendor portal

| Method | Path | Permission |
|---|---|---|
| GET | `/portal/me` | `finance.view_own_ap` or `finance.view_own_payment` |
| GET | `/portal/ap` | `finance.view_own_ap` or `finance.view_own_payment` |

### 5.8 Ops audit / legacy tables

| Method | Path | Permission |
|---|---|---|
| GET/POST | `/audit/events` | `finance.view_operations` / `finance.change_operations` |
| GET/POST | `/tables/:tableId/rows` | `finance.view_operations` / `finance.change_operations` |

> `seed_*` table IDs return **410 Gone** with a `migrateTo` hint pointing at
> inbox APIs.

### 5.9 Admin (staff)

| Method | Path | Notes |
|---|---|---|
| GET | `/admin/audit`, `/admin/audit/:id` | Django admin |
| GET | `/admin/monitoring/summary` | Django admin |
| GET | `/admin/monitoring/activity` | Django admin |
| GET | `/admin/monitoring/ledger` | Django admin |

Use iag-authentication admin APIs for IAM; these routes are finance ops only.

---

## 6. Error Conventions

### Standard envelope (most routes)

```json
{
  "error": {
    "code": "FORBIDDEN",
    "message": "permission denied"
  },
  "required_permission": ["finance.view_ledger", "finance.view_operations"]
}
```

| Status | Meaning | Frontend action |
|---|---|---|
| 400 | Bad request / validation | Show inline field error |
| 401 | Missing / invalid / expired token | Refresh; on second 401, re-login |
| 403 | Permission denied | Hide control; show toast |
| 404 | Resource not found | Re-fetch or soft-delete UX |
| 409 | Conflict | Re-fetch and retry |
| 410 | Deprecated table/API | Follow `migrateTo` hint |
| 422 | Domain validation (`validate-posting`) | `{ "ok": false, "issues": [...] }` |
| 429 | Rate limit | Backoff |
| 500 | Server error | Generic toast + retry |
| 503 | DB/Redis down | Maintenance banner |

Gateway errors add `required_all_permissions: ["platform.access_finance"]`
when the service gate fails.

---

## 7. Next.js integration patterns

### Fetch helper

```ts
const base = process.env.NEXT_PUBLIC_FINANCE_API_URL!;

export async function financeFetch<T>(
  path: string,
  token: string,
  init?: RequestInit,
): Promise<T> {
  const res = await fetch(`${base}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
      ...init?.headers,
    },
  });
  if (!res.ok) throw new Error(`Finance ${res.status}: ${await res.text()}`);
  return res.json() as Promise<T>;
}
```

### UI gating

At login, read `permissions` from `GET /api/v1/authentication/v1/users/me`.
Gate ledger screens on `finance.view_ledger`; mutations on
`finance.change_ledger`. Portal apps gate on `finance.view_own_ap`.

### Async GL side effects

`POST /ar/items` and `POST /ap/items` may publish Kafka events; GL booking
can be asynchronous when the consumer is enabled. Poll ledger endpoints or
use the notifications service for completion signals — finance has no SSE.

---

## 8. Quickstart Checklist

- [ ] Set `NEXT_PUBLIC_FINANCE_API_URL` and `NEXT_PUBLIC_AUTH_API_URL` (§2).
- [ ] Implement OAuth password-grant login against the auth service.
- [ ] Store access token in memory; set up silent refresh (§1).
- [ ] Confirm JWT `aud` includes `iag.gateway` and `iag.finance`.
- [ ] Confirm user holds `platform.access_finance` (or is superadmin).
- [ ] On app load, call `/finance/summary` + primary list endpoint (§4).
- [ ] For AR create, wire billing identity per BILLING_IDENTITY_CONTRACT.
- [ ] Handle 401 → refresh, 403 → hide control (§6).
- [ ] Vendor portal: start with `/portal/me` then `/portal/ap`.

---

## See Also

- [README.md](../README.md) — route summary and events
- [openapi.yaml](./openapi.yaml) — partial OpenAPI (ledger/AR/AP subset)
- [BILLING_IDENTITY_CONTRACT.md](./BILLING_IDENTITY_CONTRACT.md)
- [PAYROLL_ERP_BOUNDARY.md](./PAYROLL_ERP_BOUNDARY.md)
- [URA_EFRIS.md](./URA_EFRIS.md)
- [docs/RBAC.md](../../../../docs/RBAC.md) — platform `platform.access_*` gates
- Auth service `/oauth/token` — [shared/services/authentication](../)
