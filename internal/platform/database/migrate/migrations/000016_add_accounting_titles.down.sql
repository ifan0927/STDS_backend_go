DROP INDEX IF EXISTS idx_legacy_accounting_title_mappings_title_id;
DROP INDEX IF EXISTS idx_monthly_snapshot_entries_accounting_title_id;
DROP INDEX IF EXISTS idx_accounting_entries_accounting_title_id;

ALTER TABLE monthly_snapshot_entries
    DROP COLUMN IF EXISTS accounting_title_name,
    DROP COLUMN IF EXISTS accounting_title_code,
    DROP COLUMN IF EXISTS accounting_title_id;

ALTER TABLE accounting_entries
    DROP COLUMN IF EXISTS accounting_title_name,
    DROP COLUMN IF EXISTS accounting_title_code,
    DROP COLUMN IF EXISTS accounting_title_id;

DROP TABLE IF EXISTS legacy_accounting_title_mappings;
DROP TABLE IF EXISTS accounting_titles;
