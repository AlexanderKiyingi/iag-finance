# Deprecated — do not use

This directory is a **legacy prototype** for an early finance Next.js UI. It is **not** the production `iag-finance` service.

| Use instead | Path |
|-------------|------|
| Production finance API | [`../cmd/server`](../cmd/server) (port **3006**) |
| Gateway prefix | `/api/v1/finance/v1/...` |
| Database schema | [`../internal/db/migrations`](../internal/db/migrations) |

## Why quarantined

- Separate `go.mod`, file/JSON persistence, and budget/inventory stubs that are **not shipped**.
- Routes mirror an old frontend contract; they are **not wired** into the platform gateway or k8s manifests.
- Conflicts with the real GL/AR/AP/EFRIS implementation under `internal/`.

## If you need finance APIs

Run the production service:

```bash
cd shared/services/finance
go run ./cmd/server
```

See [`../README.md`](../README.md) for the current API surface.
