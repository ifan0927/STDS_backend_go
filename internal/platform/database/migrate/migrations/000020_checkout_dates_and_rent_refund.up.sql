ALTER TABLE leases
    ADD COLUMN IF NOT EXISTS actual_move_out_date DATE;

ALTER TABLE accounting_entries
    DROP CONSTRAINT IF EXISTS accounting_entries_category_check,
    ADD CONSTRAINT accounting_entries_category_check CHECK (category IN ('rent_payment', 'electricity_payment', 'deposit_refund', 'deposit_deduction', 'journal_expense', 'rent_refund'));

ALTER TABLE monthly_snapshot_entries
    DROP CONSTRAINT IF EXISTS monthly_snapshot_entries_category_check,
    ADD CONSTRAINT monthly_snapshot_entries_category_check CHECK (category IN ('rent_payment', 'electricity_payment', 'deposit_refund', 'deposit_deduction', 'journal_expense', 'rent_refund'));
