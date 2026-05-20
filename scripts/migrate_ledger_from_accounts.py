#!/usr/bin/env python3
"""One-off: port ledger packages from shared/services/accounts into finance."""
from __future__ import annotations

import pathlib
import shutil

ROOT = pathlib.Path(__file__).resolve().parents[2]
ACC = ROOT / "accounts" / "internal"
FIN = ROOT / "finance" / "internal"

REPL = [
    ("github.com/alvor-technologies/iag-accounts", "github.com/iag-finance/backend"),
]


def rewrite_go(path: pathlib.Path, extra: list[tuple[str, str]] | None = None) -> None:
    text = path.read_text(encoding="utf-8")
    for a, b in (REPL + (extra or [])):
        text = text.replace(a, b)
    path.write_text(text, encoding="utf-8")


def main() -> None:
    # hash-chain audit -> chainaudit
    audit_dir = FIN / "audit"
    chain_dir = FIN / "chainaudit"
    if audit_dir.exists():
        if chain_dir.exists():
            shutil.rmtree(chain_dir)
        shutil.move(str(audit_dir), str(chain_dir))
        for f in chain_dir.glob("*.go"):
            rewrite_go(f, [("package audit", "package chainaudit")])

    for name in ("domain", "repository", "consumer", "integrations", "authz"):
        dst = FIN / name
        if dst.exists():
            shutil.rmtree(dst)
        shutil.copytree(ACC / name, dst)

    dst_auditlog = FIN / "auditlog"
    if dst_auditlog.exists():
        shutil.rmtree(dst_auditlog)
    shutil.copytree(ACC / "audit", dst_auditlog)
    for f in dst_auditlog.rglob("*.go"):
        rewrite_go(f, [("package audit", "package auditlog")])

    dst_ledger = FIN / "ledger"
    dst_ledger.mkdir(exist_ok=True)
    for name in ("service.go", "service_test.go"):
        shutil.copy2(ACC / "ledger" / name, dst_ledger / name)
        rewrite_go(dst_ledger / name, [("internal/audit", "internal/auditlog")])

    fin_middleware = FIN / "middleware"
    for name in ("audit.go", "admin.go"):
        shutil.copy2(ACC / "middleware" / name, fin_middleware / name)
        rewrite_go(fin_middleware / name, [("internal/audit", "internal/auditlog")])

    fin_handlers = FIN / "handlers"
    for name in ("audit.go", "monitoring.go", "context.go"):
        shutil.copy2(ACC / "handlers" / name, fin_handlers / name)
    shutil.copy2(ACC / "handlers" / "handlers.go", fin_handlers / "ledger_api.go")

    mig = FIN / "db" / "migrations"
    shutil.copy2(ACC / "db" / "migrations" / "001_init.sql", mig / "002_ledger.sql")
    shutil.copy2(ACC / "db" / "migrations" / "003_journal_entry_seq.sql", mig / "003_journal_entry_seq.sql")
    audit_sql = (ACC / "db" / "migrations" / "002_audit_log.sql").read_text(encoding="utf-8")
    audit_sql = audit_sql.replace("accounts_audit_log", "finance_audit_log")
    audit_sql = audit_sql.replace("idx_accounts_audit", "idx_finance_audit")
    (mig / "004_audit_log.sql").write_text(audit_sql, encoding="utf-8")

    for f in FIN.rglob("*.go"):
        if "chainaudit" in f.parts:
            continue
        rewrite_go(f, [("internal/audit", "internal/auditlog")])

    # service name in ledger API
    lap = fin_handlers / "ledger_api.go"
    t = lap.read_text(encoding="utf-8")
    t = t.replace('"service":   "accounts"', '"service":   "finance"')
    t = t.replace('"service":  "accounts"', '"service":  "finance"')
    t = t.replace('"service": "accounts"', '"service": "finance"')
    lap.write_text(t, encoding="utf-8")

    print("migrate_ledger_from_accounts: ok")


if __name__ == "__main__":
    main()
