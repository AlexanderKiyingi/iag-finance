-- Prevent duplicate bank feed imports on re-sync.

CREATE UNIQUE INDEX IF NOT EXISTS idx_bank_stmt_lines_external_ref
    ON bank_statement_lines (external_ref)
    WHERE external_ref IS NOT NULL AND external_ref <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_bank_tx_statement_line
    ON bank_transactions (statement_line_id)
    WHERE statement_line_id IS NOT NULL;
