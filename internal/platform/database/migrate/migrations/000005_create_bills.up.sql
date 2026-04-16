CREATE TABLE IF NOT EXISTS bills (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lease_id UUID NOT NULL REFERENCES leases(id),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    room_id UUID NOT NULL REFERENCES rooms(id),
    property_id UUID NOT NULL REFERENCES properties(id),
    type VARCHAR(20) NOT NULL CHECK (type IN ('rent', 'electricity')),
    amount INTEGER,
    due_date DATE NOT NULL,
    status VARCHAR(30) NOT NULL CHECK (status IN ('pending_meter', 'pending_payment', 'paid', 'overdue', 'voided', 'written_off')),
    payment_method VARCHAR(20) CHECK (payment_method IN ('cash', 'transfer', 'other')),
    paid_at TIMESTAMPTZ,
    paid_amount INTEGER,
    meter_previous_reading INTEGER,
    meter_current_reading INTEGER,
    meter_unit_price INTEGER,
    meter_recorded_at TIMESTAMPTZ,
    written_off_reason TEXT,
    overdue_notice_count INTEGER NOT NULL DEFAULT 0,
    source_ref JSONB,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_bills_property_status_due_date
    ON bills (property_id, status, due_date)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_bills_lease_id
    ON bills (lease_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_bills_tenant_status
    ON bills (tenant_id, status)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_bills_status_due_date
    ON bills (status, due_date)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_bills_property_type_due_date
    ON bills (property_id, type, due_date)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_bills_room_type_due_date
    ON bills (room_id, type, due_date)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_bills_status_overdue_notice_count
    ON bills (status, overdue_notice_count)
    WHERE deleted_at IS NULL;
