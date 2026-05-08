ALTER TABLE accounting_entries
    ADD COLUMN IF NOT EXISTS source_date DATE,
    ADD COLUMN IF NOT EXISTS room_label TEXT,
    ADD COLUMN IF NOT EXISTS tenant_label TEXT,
    ADD COLUMN IF NOT EXISTS period_label TEXT,
    ADD COLUMN IF NOT EXISTS display_note TEXT;

ALTER TABLE monthly_snapshot_entries
    ADD COLUMN IF NOT EXISTS source_date DATE,
    ADD COLUMN IF NOT EXISTS room_label TEXT,
    ADD COLUMN IF NOT EXISTS tenant_label TEXT,
    ADD COLUMN IF NOT EXISTS period_label TEXT,
    ADD COLUMN IF NOT EXISTS display_note TEXT;
