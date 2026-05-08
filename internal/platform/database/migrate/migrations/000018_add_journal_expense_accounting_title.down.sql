DROP INDEX IF EXISTS idx_journal_logs_expense_accounting_title_id;

ALTER TABLE journal_logs
    DROP COLUMN IF EXISTS expense_accounting_title_name,
    DROP COLUMN IF EXISTS expense_accounting_title_code,
    DROP COLUMN IF EXISTS expense_accounting_title_id;
