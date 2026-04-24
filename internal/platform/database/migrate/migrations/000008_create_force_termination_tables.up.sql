CREATE TABLE IF NOT EXISTS force_terminations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lease_id UUID NOT NULL REFERENCES leases(id),
    initiated_by UUID NOT NULL REFERENCES users(id),
    reason TEXT NOT NULL,
    deposit_handling VARCHAR(20) NOT NULL CHECK (deposit_handling IN ('write_off', 'keep_held')),
    status VARCHAR(20) NOT NULL DEFAULT 'in_progress' CHECK (status IN ('in_progress', 'completed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_force_terminations_status
    ON force_terminations (status);

CREATE INDEX IF NOT EXISTS idx_force_terminations_lease_id
    ON force_terminations (lease_id);

CREATE TABLE IF NOT EXISTS force_termination_bills (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    force_termination_id UUID NOT NULL REFERENCES force_terminations(id),
    bill_id UUID NOT NULL REFERENCES bills(id),
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_force_termination_bills_ft_status
    ON force_termination_bills (force_termination_id, status);

CREATE INDEX IF NOT EXISTS idx_force_termination_bills_bill_id
    ON force_termination_bills (bill_id);
