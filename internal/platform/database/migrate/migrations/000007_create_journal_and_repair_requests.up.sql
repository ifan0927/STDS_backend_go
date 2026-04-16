CREATE TABLE IF NOT EXISTS journal_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id UUID NOT NULL REFERENCES properties(id),
    room_id UUID REFERENCES rooms(id),
    author_id UUID NOT NULL REFERENCES users(id),
    content TEXT NOT NULL,
    expense_amount INTEGER,
    expense_description TEXT,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_journal_logs_property_created_at
    ON journal_logs (property_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_journal_logs_room_id
    ON journal_logs (room_id)
    WHERE deleted_at IS NULL AND room_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS repair_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id UUID NOT NULL REFERENCES properties(id),
    room_id UUID NOT NULL REFERENCES rooms(id),
    submitted_by UUID NOT NULL REFERENCES users(id),
    assigned_to UUID REFERENCES users(id),
    title VARCHAR(200) NOT NULL,
    description TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted', 'assigned', 'in_progress', 'completed', 'cancelled')),
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    assigned_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_repair_requests_property_status
    ON repair_requests (property_id, status)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_repair_requests_room_id
    ON repair_requests (room_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_repair_requests_assigned_to
    ON repair_requests (assigned_to)
    WHERE deleted_at IS NULL AND assigned_to IS NOT NULL;
