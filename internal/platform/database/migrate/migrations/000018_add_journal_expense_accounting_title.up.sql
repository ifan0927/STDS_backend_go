ALTER TABLE journal_logs
    ADD COLUMN IF NOT EXISTS expense_accounting_title_id UUID REFERENCES accounting_titles(id),
    ADD COLUMN IF NOT EXISTS expense_accounting_title_code VARCHAR(20),
    ADD COLUMN IF NOT EXISTS expense_accounting_title_name VARCHAR(100);

UPDATE journal_logs jl
SET
    expense_accounting_title_id = ae.accounting_title_id,
    expense_accounting_title_code = ae.accounting_title_code,
    expense_accounting_title_name = ae.accounting_title_name
FROM accounting_entries ae
WHERE jl.expense_amount IS NOT NULL
  AND ae.category = 'journal_expense'
  AND ae.source_ref->>'journal_log_id' = jl.id::text
  AND ae.accounting_title_id IS NOT NULL
  AND (
      jl.expense_accounting_title_id IS NULL
      OR jl.expense_accounting_title_code IS NULL
      OR jl.expense_accounting_title_name IS NULL
  );

UPDATE journal_logs jl
SET
    expense_accounting_title_id = at.id,
    expense_accounting_title_code = at.code,
    expense_accounting_title_name = at.name
FROM accounting_titles at
WHERE jl.expense_amount IS NOT NULL
  AND at.code = '6681'
  AND (
      jl.expense_accounting_title_id IS NULL
      OR jl.expense_accounting_title_code IS NULL
      OR jl.expense_accounting_title_name IS NULL
  );

CREATE INDEX IF NOT EXISTS idx_journal_logs_expense_accounting_title_id
    ON journal_logs (expense_accounting_title_id);
