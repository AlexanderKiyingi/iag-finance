# Finance — platform integration plan

Evolve `iag-finance` from a standalone prototype (`:8080`, `iag_finance` DB, no auth) into a shared platform service on **port 3006**, behind the **API gateway**, on **`iag_platform.finance`**, connected to **authentication**, **accounts**, and the **event bus**.

---

## Service boundaries (target state)

| Owns | Does not own |
|------|----------------|
| General ledger, chart of accounts, journal entries | End-user login profiles (→ **iag-authentication** / future **iag-accounts**) |
| AR/AP open items, trial balance | SCM farmer payments (→ **iag-supply-chain**) |
| URA EFRIS & banking adapters (stubs → real) | Full QuickBooks parity (reports, payroll, bank feeds) |
| Kafka **consumer** on `iag.finance` | |
| Hash-chained ops audit + prototype UI rows | |

**Event bus:** Finance **consumes** `sale.completed` and `invoice.posted` on `iag.finance`, and `fleet.fuel.recorded` on `iag.fleet`. Finance **publishes** `sale.completed` / `invoice.posted` when AR/AP open items are created via the API (`ENABLE_EVENT_PUBLISH`).

```mermaid
flowchart LR
  Client --> GW[API Gateway :8080]
  GW --> FIN[iag-finance :3006]
  FIN --> PG[(iag_platform.finance)]
  SCM[iag-supply-chain] --> K[iag.finance]
  FLT[iag-fleet] --> K
  FIN -.->|optional publish| K
  K --> FIN
  FIN --> AUTH[iag-authentication JWKS]
  GW --> AUTH
```

---

## Phase 1 — Platform substrate (config, DB, compose)

### 1.1 Environment variables

Align with `shared/services/accounts` (replace `HTTP_ADDR` with `PORT`).

| Variable | Dev (compose) | Production | Notes |
|----------|---------------|------------|--------|
| `SERVICE_NAME` | `finance` | `finance` | Logs / metrics |
| `ENVIRONMENT` | `development` | `production` | `development` \| `staging` \| `production` |
| `PORT` | `3006` | `3006` | **Railway:** set `PORT=3006`; listen on `0.0.0.0:$PORT` |
| `LOG_LEVEL` | `info` | `info` | |
| `DATABASE_URL` | see below | `postgresql://svc_finance:***@host:5432/iag_platform?sslmode=require` | Role `svc_finance`, schema `finance` |
| `AUTH_MODE` | `gateway` | `gateway` | `jwt` only for direct local debugging |
| `GATEWAY_INTERNAL_SECRET` | same as gateway | secret store | ≥16 chars; must match gateway |
| `JWT_ISSUER` | `http://localhost:3001` | public auth URL | |
| `JWKS_URL` | `http://authentication:3001/.well-known/jwks.json` | auth JWKS | For `AUTH_MODE=jwt` |
| `REDIS_URL` | `redis://redis:6379/1` | managed Redis | Optional: degrade audit cache if unset |
| `SEED_ON_STARTUP` | `true` | `false` | Demo `table_rows` seed; never in prod |
| `AUTO_MIGRATE` | `true` | `true` | Apply versioned SQL on startup |
| `ACCOUNTS_URL` | `http://accounts:3005` | internal URL | Health / future server-side GL calls |
| `KAFKA_BROKERS` | `redpanda:9092` | cluster | Phase 3+ |
| `KAFKA_CLIENT_ID` | `finance` | `finance` | Phase 3+ |
| `SHUTDOWN_TIMEOUT_SECONDS` | `15` | `15` | |

**Dev `DATABASE_URL` (schema via role `search_path`):**

```text
postgresql://svc_finance:iag_finance_dev@localhost:5432/iag_platform?sslmode=disable
```

Postgres role `svc_finance` is already defined in `deploy/postgres/init/02-service-roles.sh`.

### 1.2 `deploy/docker-compose.yml` — add `finance` service

```yaml
  finance:
    build:
      context: ../shared/services/finance
      dockerfile: Dockerfile
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      authentication:
        condition: service_healthy
    environment:
      ENVIRONMENT: ${FINANCE_ENVIRONMENT:-staging}
      PORT: "3006"
      DATABASE_URL: ${FINANCE_DATABASE_URL:-postgresql://svc_finance:${POSTGRES_SVC_FINANCE_PASSWORD:-iag_finance_dev}@postgres:5432/iag_platform}
      AUTH_MODE: ${FINANCE_AUTH_MODE:-gateway}
      GATEWAY_INTERNAL_SECRET: ${GATEWAY_INTERNAL_SECRET:-dev-gateway-secret-min-16-chars}
      JWT_ISSUER: ${JWT_ISSUER:-http://localhost:3001}
      JWKS_URL: ${JWKS_URL:-http://authentication:3001/.well-known/jwks.json}
      REDIS_URL: redis://redis:6379/1
      SEED_ON_STARTUP: ${FINANCE_SEED_ON_STARTUP:-true}
      AUTO_MIGRATE: "true"
      ACCOUNTS_URL: http://accounts:3005
    ports:
      - "3006:3006"
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:3006/ready"]
```

Also add to `postgres` service env: `POSTGRES_SVC_FINANCE_PASSWORD` (mirror other `POSTGRES_SVC_*` keys in compose).

Wire **api-gateway** `depends_on: finance` and:

```text
UPSTREAM_FINANCE: http://finance:3006
```

### 1.3 Root dev scripts

| File | Change |
|------|--------|
| `package.json` | `"dev:finance"`, `"infra:finance"` |
| `pnpm-workspace.yaml` | include `shared/services/finance` if packaged |
| `README.md` | Port table: Finance `3006` |
| `deploy/RAILWAY.md` | Row: Finance `3006` `/ready`, migrations always on startup |
| `deploy/.env.example` | `FINANCE_DATABASE_URL`, `POSTGRES_SVC_FINANCE_PASSWORD` |

### 1.4 Dockerfile

```dockerfile
ENV PORT=3006
EXPOSE 3006
HEALTHCHECK CMD wget -q -O /dev/null http://127.0.0.1:${PORT}/ready
```

Remove standalone `docker-compose.yml` use of `iag_finance` DB (keep file as optional local-only override or delete after compose works).

---

## Phase 2 — Schema migration (prototype → platform)

### 2.1 Problems today

- DB `iag_finance` + user `iag` — not `iag_platform` / `svc_finance`
- Migrations re-run all SQL every boot; `002_seed.sql` deletes/reinserts demo rows
- No `schema_migrations` versioning (unlike accounts)

### 2.2 Target migration runner

Copy the **accounts** pattern (`internal/db` or `internal/migrate`):

1. `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, ...)`
2. Apply `migrations/NNN_*.sql` in order once per version
3. `pg_advisory_xact_lock` during `Up` (same as procurement) for multi-replica deploys

### 2.3 SQL file plan

| File | Action |
|------|--------|
| `migrations/001_init.sql` | Same tables as today (`audit_events`, `table_rows`) in schema `finance` (implicit via role `search_path`) |
| `migrations/002_seed.sql` | **Remove from auto-migrate**; run only when `SEED_ON_STARTUP=true` (dev) |
| `migrations/003_integrations.sql` | `efris_submissions`, `bank_statements` stub tables (Phase 4) |
| `migrations/004_audit_immutable.sql` | Optional: prevent `UPDATE`/`DELETE` on `audit_events` |

### 2.4 Data migration (existing dev DB)

One-off if anyone used standalone compose:

```sql
-- Run as iag superuser on iag_platform
INSERT INTO finance.audit_events SELECT * FROM dblink(...iag_finance.audit_events...);
-- Or pg_dump/pg_restore into finance schema
```

Greenfield dev: drop volume `iag_pg_data` and rely on `deploy/postgres/init` + finance migrations.

### 2.5 Move integrations out of accounts (optional, Phase 4)

Today accounts exposes:

- `GET /v1/integrations/ura-efris`
- `GET /v1/integrations/banking`

**Plan:** Implement real adapters on finance; gateway deprecates accounts integration routes (301 → finance) or removes stubs after UI moves.

---

## Phase 3 — API gateway routes & auth

### 3.1 Upstream route (`shared/services/api-gateway/src/routes.ts`)

```typescript
"/api/v1/finance": {
  upstream: upstream("UPSTREAM_FINANCE", "http://127.0.0.1:3006"),
  prefix: "/api/v1/finance",
  rewritePrefix: "/",
},
```

### 3.2 Route policies (`policies.ts`)

Add (order: specific before broad):

```typescript
{ prefix: "/api/v1/finance/health", public: true },
{ prefix: "/api/v1/finance/ready", public: true },
{ prefix: "/api/v1/finance/v1/admin", requireAdmin: true },
{
  prefix: "/api/v1/finance/v1",
  methods: ["POST", "PUT", "PATCH", "DELETE"],
  permissions: ["finance.change_operations"],
},
{
  prefix: "/api/v1/finance/v1",
  permissions: ["finance.view_operations"],
},
```

Update `policies.coverage.test.ts` proxied paths.

### 3.3 Finance HTTP path layout

**Refactor** internal routes from `/api/v1/...` → `/v1/...` (match accounts).

| Public (via gateway) | Finance upstream |
|--------------------|------------------|
| `GET /api/v1/finance/health` | `GET /health` |
| `GET /api/v1/finance/ready` | `GET /ready` |
| `GET /api/v1/finance/v1/audit/events` | `GET /v1/audit/events` |
| `POST /api/v1/finance/v1/audit/events` | `POST /v1/audit/events` |
| `GET /api/v1/finance/v1/tables/:id/rows` | `GET /v1/tables/:tableId/rows` |
| `POST /api/v1/finance/v1/ledger/validate-posting` | `POST /v1/ledger/validate-posting` |
| `GET /api/v1/finance/v1/integrations/ura-efris` | `GET /v1/integrations/ura-efris` |
| `GET /api/v1/finance/v1/integrations/banking` | `GET /v1/integrations/banking` |

### 3.4 Finance middleware (implement in Go)

Mirror `shared/services/accounts/internal/middleware`:

1. **`PlatformAuth`** — require `X-IAG-Gateway-Secret` when `AUTH_MODE=gateway`; skip `/health`, `/ready`
2. **`Principal`** — read `X-IAG-User-Id`, `X-IAG-Email`, `X-IAG-Roles`, `X-IAG-Permissions`
3. **`RequirePermission`** — per-route checks (or rely on gateway-only enforcement in Phase 3a)

Register health routes **before** auth middleware (fleet pattern).

### 3.5 Authentication — new permissions

Seed in **iag-authentication** (not finance):

| Codename | Purpose |
|----------|---------|
| `finance.view_operations` | Read audit, tables, integration status |
| `finance.change_operations` | Append audit, table rows, submit EFRIS |
| `finance.view_ledger` | *(optional)* call accounts read APIs via BFF |
| `finance.change_ledger` | *(optional)* restricted; prefer accounts directly |

Existing SCM permissions (`finance.view_farmerpayment`) stay on supply-chain routes only.

Default groups: add codenames to **Finance** group in auth seed (parallel to SCM `Finance` group).

---

## Phase 4 — Inter-service behavior

### 4.1 Accounts (HTTP)

| Use case | Pattern |
|----------|---------|
| Posting validation (rules only) | Stay in finance `validate-posting` short-term |
| Real journal post | Client calls `/api/v1/accounts/v1/ledger/entries` or finance BFF proxies with user token |
| CoA balances | `GET ACCOUNTS_URL/v1/chart-of-accounts` server-side with service account (Phase 4+) |

Finance `ACCOUNTS_URL` used for `/ready` dependency check (optional: `"accounts": true` in ready JSON).

### 4.2 Kafka (publish, Phase 4+)

```text
Topic:     iag.finance
Client ID: finance
Group ID:  (none — producer only in v1)
```

Example new event types in `packages/events/src/index.ts`:

```typescript
export const financeOpsEventTypes = {
  invoiceApproved: "finance.invoice.approved",
  efrisSubmitted: "finance.efris.submitted",
} as const;
```

Accounts consumer: ignore unknown types (already does); add handlers when finance publishes bookable events.

### 4.3 Domain services

| Service | Integration |
|---------|-------------|
| **iag-supply-chain** | Farmer payments remain SCM; finance UI may link via gateway |
| **iag-fleet** | Keeps publishing `fleet.fuel.recorded` → accounts |
| **iag-procurement** | Future: `procurement.invoice.received` → finance AP inbox |
| **iag-notifications** | Finance publishes `notification.requested` on approval workflows |

### 4.4 Replace HTML `table_rows` (Phase 5 — data model)

Introduce normalized tables; keep `table_rows` as read-only legacy or drop after UI migration:

- `ap_invoices`, `bank_accounts`, `cherry_intake_lines`, `chart_of_accounts_snapshot`

---

## Phase 5 — CI, observability, Railway

| Item | Action |
|------|--------|
| CI | `.github/workflows/ci.yml` in finance + meta-repo job: Postgres `finance` schema, `go test ./...`, `docker build` |
| Logs | Structured JSON (`slog`) with `service=finance` |
| Metrics | `/ready` exposes postgres + redis + optional accounts ping |
| Railway | Service `iag-finance`, `PORT=3006`, attach Postgres plugin URL with `svc_finance` user |
| K8s | `deploy/kubernetes/finance/` (copy accounts manifests) |

---

## Implementation checklist (ordered)

### Wave A — runnable on platform

- [x] `config.Load()`: `PORT`, `ENVIRONMENT`, `AUTH_MODE`, `GATEWAY_INTERNAL_SECRET`, `AUTO_MIGRATE`, `SEED_ON_STARTUP`
- [x] Versioned migrations + `svc_finance` on `iag_platform`
- [x] Listen on `:PORT`; Dockerfile `ENV PORT=3006`
- [x] Gateway `routes.ts` + `policies.ts` + `UPSTREAM_FINANCE`
- [x] `deploy/docker-compose.yml` finance service + postgres password env
- [x] Platform auth middleware; routes `/v1/*`
- [x] Update `config/.env.example`, `README.md`, `deploy/RAILWAY.md`

### Wave B — RBAC & hardening

- [x] Auth seed: `finance.view_operations`, `finance.change_operations`
- [x] CORS: `CORS_ALLOW_ORIGIN` (not `*`)
- [x] Redis optional at startup (audit works without cache)
- [x] `pnpm dev:finance` + CI job

### Wave C — product split (ongoing)

- [x] Move URA/banking stubs from accounts → finance handlers
- [x] Kafka producer for finance workflow events (`notification.requested`, `finance.*`)
- [x] Normalized schema; deprecate `table_rows` HTML storage (inbox APIs + 410 on `seed_*`)
- [x] `docs/planning/SHARED_SERVICES.md` row for Finance service #8

---

## Smoke test (after Wave A)

```bash
pnpm infra:up   # includes finance + gateway
curl -s http://localhost:8080/api/v1/finance/ready
curl -s -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/finance/v1/audit/events
```

Direct (dev only, `AUTH_MODE=jwt`):

```bash
curl -s http://localhost:3006/ready
```

---

## Risk register

| Risk | Mitigation |
|------|------------|
| Duplicate ledger logic in finance vs accounts | Finance never writes `journal_entries`; accounts owns GL |
| `iag.finance` topic confusion | Document: **accounts consumes**, finance may **publish** later |
| Seed migration on every deploy | `SEED_ON_STARTUP=false` in prod; versioned migrations only |
| Gateway 401 on `/ready` | Public policy on `/api/v1/finance/ready` |
| SCM `finance.*` permission name clash | SCM permissions stay on supply-chain routes; finance uses `finance.view_operations` |

---

## References

- Accounts integration: [`shared/services/accounts/README.md`](../../accounts/README.md)
- Fleet gateway pattern: [`services/operations/fleet/docs/PLATFORM_INTEGRATION.md`](../../../../services/operations/fleet/docs/PLATFORM_INTEGRATION.md)
- Postgres roles: [`deploy/postgres/README.md`](../../../../deploy/postgres/README.md)
- Gateway routes: [`shared/services/api-gateway/src/routes.ts`](../../api-gateway/src/routes.ts)
