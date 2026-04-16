CREATE TABLE IF NOT EXISTS property_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id UUID NOT NULL UNIQUE REFERENCES properties(id),
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version INTEGER NOT NULL DEFAULT 1
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_property_accounts_property_id
    ON property_accounts (property_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS accounting_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    property_account_id UUID NOT NULL REFERENCES property_accounts(id),
    category VARCHAR(50) NOT NULL CHECK (category IN ('rent_payment', 'electricity_payment', 'deposit_refund', 'deposit_deduction', 'journal_expense')),
    amount INTEGER NOT NULL,
    description TEXT,
    source_ref JSONB NOT NULL,
    year SMALLINT NOT NULL,
    month SMALLINT NOT NULL CHECK (month BETWEEN 1 AND 12),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_accounting_entries_account_year_month
    ON accounting_entries (property_account_id, year, month);

CREATE TABLE IF NOT EXISTS monthly_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id UUID NOT NULL REFERENCES properties(id),
    year SMALLINT NOT NULL,
    month SMALLINT NOT NULL CHECK (month BETWEEN 1 AND 12),
    total_income INTEGER NOT NULL DEFAULT 0,
    total_expense INTEGER NOT NULL DEFAULT 0,
    net INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (property_id, year, month)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_monthly_snapshots_property_year_month
    ON monthly_snapshots (property_id, year, month);

CREATE TABLE IF NOT EXISTS monthly_snapshot_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_id UUID NOT NULL REFERENCES monthly_snapshots(id),
    category VARCHAR(50) NOT NULL CHECK (category IN ('rent_payment', 'electricity_payment', 'deposit_refund', 'deposit_deduction', 'journal_expense')),
    description TEXT,
    amount INTEGER NOT NULL,
    source_ref JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_monthly_snapshot_entries_snapshot_category
    ON monthly_snapshot_entries (snapshot_id, category);
