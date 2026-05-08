ALTER TABLE monthly_snapshot_entries
    DROP COLUMN IF EXISTS display_note,
    DROP COLUMN IF EXISTS period_label,
    DROP COLUMN IF EXISTS tenant_label,
    DROP COLUMN IF EXISTS room_label,
    DROP COLUMN IF EXISTS source_date;

ALTER TABLE accounting_entries
    DROP COLUMN IF EXISTS display_note,
    DROP COLUMN IF EXISTS period_label,
    DROP COLUMN IF EXISTS tenant_label,
    DROP COLUMN IF EXISTS room_label,
    DROP COLUMN IF EXISTS source_date;
