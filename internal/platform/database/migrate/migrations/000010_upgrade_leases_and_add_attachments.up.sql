ALTER TABLE leases
    ADD COLUMN IF NOT EXISTS deposit_refund_amount INTEGER CHECK (deposit_refund_amount >= 0),
    ADD COLUMN IF NOT EXISTS deposit_deduction_amount INTEGER CHECK (deposit_deduction_amount >= 0);

ALTER TABLE leases
    DROP CONSTRAINT IF EXISTS leases_deposit_status_check;

ALTER TABLE leases
    ADD CONSTRAINT leases_deposit_status_check
    CHECK (deposit_status IN ('held', 'settled', 'written_off'));

UPDATE leases
SET deposit_status = CASE
    WHEN deposit_status IN ('refunded', 'deducted') THEN 'settled'
    ELSE deposit_status
END,
deposit_refund_amount = CASE
    WHEN deposit_status = 'refunded' AND deposit_refund_amount IS NULL THEN deposit_amount
    ELSE deposit_refund_amount
END,
deposit_deduction_amount = CASE
    WHEN deposit_status = 'deducted' AND deposit_deduction_amount IS NULL THEN deposit_amount
    ELSE deposit_deduction_amount
END;

CREATE TABLE IF NOT EXISTS attachment_upload_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    nonce VARCHAR(255) NOT NULL UNIQUE,
    object_path TEXT NOT NULL,
    issued_to UUID NOT NULL REFERENCES users(id),
    resource_type VARCHAR(50) NOT NULL,
    resource_id UUID NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_attachment_upload_tokens_expires_at
    ON attachment_upload_tokens (expires_at);

CREATE INDEX IF NOT EXISTS idx_attachment_upload_tokens_resource
    ON attachment_upload_tokens (resource_type, resource_id);

CREATE TABLE IF NOT EXISTS property_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id UUID NOT NULL REFERENCES properties(id),
    object_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    uploaded_by UUID REFERENCES users(id),
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_property_attachments_property_id
    ON property_attachments (property_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS room_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id UUID NOT NULL REFERENCES rooms(id),
    object_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    uploaded_by UUID REFERENCES users(id),
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_room_attachments_room_id
    ON room_attachments (room_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS tenant_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    object_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    uploaded_by UUID REFERENCES users(id),
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tenant_attachments_tenant_id
    ON tenant_attachments (tenant_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS lease_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lease_id UUID NOT NULL REFERENCES leases(id),
    object_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    uploaded_by UUID REFERENCES users(id),
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_lease_attachments_lease_id
    ON lease_attachments (lease_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS journal_log_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    journal_log_id UUID NOT NULL REFERENCES journal_logs(id),
    object_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    uploaded_by UUID REFERENCES users(id),
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_journal_log_attachments_journal_log_id
    ON journal_log_attachments (journal_log_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS repair_request_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repair_request_id UUID NOT NULL REFERENCES repair_requests(id),
    object_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    uploaded_by UUID REFERENCES users(id),
    sort_order INTEGER NOT NULL DEFAULT 0,
    photo_stage VARCHAR(20) CHECK (photo_stage IN ('before', 'after', 'other')),
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_repair_request_attachments_repair_request_id
    ON repair_request_attachments (repair_request_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_repair_request_attachments_repair_request_sort_order
    ON repair_request_attachments (repair_request_id, sort_order)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS bill_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bill_id UUID NOT NULL REFERENCES bills(id),
    object_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    uploaded_by UUID REFERENCES users(id),
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_bill_attachments_bill_id
    ON bill_attachments (bill_id)
    WHERE deleted_at IS NULL;
