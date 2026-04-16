CREATE TABLE IF NOT EXISTS tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    email VARCHAR(255) NOT NULL,
    phone VARCHAR(50),
    contacts JSONB NOT NULL DEFAULT '[]',
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_tenants_status
    ON tenants (status)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_tenants_email
    ON tenants (email)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS leases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    room_id UUID NOT NULL REFERENCES rooms(id),
    property_id UUID NOT NULL REFERENCES properties(id),
    rent_amount INTEGER NOT NULL CHECK (rent_amount > 0),
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'expired', 'terminated', 'force_terminated')),
    deposit_amount INTEGER NOT NULL CHECK (deposit_amount >= 0),
    deposit_status VARCHAR(20) NOT NULL DEFAULT 'held' CHECK (deposit_status IN ('held', 'refunded', 'deducted', 'written_off')),
    deposit_deduction_reason TEXT,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_leases_property_status
    ON leases (property_id, status)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_leases_tenant_id
    ON leases (tenant_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_leases_room_id
    ON leases (room_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_leases_end_date_status
    ON leases (end_date, status)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_leases_status
    ON leases (status)
    WHERE deleted_at IS NULL;
