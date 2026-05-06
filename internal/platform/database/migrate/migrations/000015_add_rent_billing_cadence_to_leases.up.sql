ALTER TABLE leases
    ADD COLUMN IF NOT EXISTS rent_billing_cadence VARCHAR(20) NOT NULL DEFAULT 'monthly',
    ADD CONSTRAINT leases_rent_billing_cadence_check
        CHECK (rent_billing_cadence IN ('monthly', 'quarterly', 'semiannual', 'annual'));
