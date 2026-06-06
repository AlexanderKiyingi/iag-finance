# URA EFRIS integration

Finance supports three EFRIS adapter modes (see `URA_EFRIS_MODE`):

| Mode | When | Use case |
|------|------|----------|
| `simulate` | `URA_EFRIS_SIMULATE=true` | Local dev — returns `SIM-{documentRef}` |
| `http` | `URA_EFRIS_BASE_URL` set | Partner middleware that exposes `/v1/invoices/fiscalise` |
| `ura_s2s` | `URA_EFRIS_MODE=ura_s2s` | Direct URA server-to-server (T109 upload) |

Default when nothing is configured: **stub** (submit fails with configuration hint).

## HTTP partner adapter

```env
URA_EFRIS_BASE_URL=https://efris-partner.example.com
URA_EFRIS_API_KEY=...
URA_EFRIS_TIN=1000123456
```

Posts JSON to `{BASE_URL}/v1/invoices/fiscalise`.

## URA S2S (live Uganda)

```env
URA_EFRIS_MODE=ura_s2s
URA_EFRIS_TIN=1000123456
URA_EFRIS_DEVICE_NO=YOUR_DEVICE
URA_EFRIS_BRANCH_ID=00
URA_EFRIS_S2S_URL=https://efrisws.ura.go.ug
URA_EFRIS_S2S_PATH=/efrisws/ws/ta/request
# Optional AES-128/192/256 key (hex or raw) for encrypted data field:
# URA_EFRIS_AES_KEY=
```

The adapter builds a T109 `globalInfo` envelope and posts the fiscal document payload. Production deployments must supply URA-issued device credentials and encryption keys per URA onboarding.

## API

```http
POST /api/v1/finance/v1/integrations/ura-efris/submit
{ "documentRef": "INV-2026-0042" }
```

Queues `efris_submissions`, loads the matching AR row for amount/customer, calls the active adapter, and stores `ura_receipt` / `adapter_mode`.
