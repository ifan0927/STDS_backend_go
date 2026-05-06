ALTER TABLE leases
    DROP CONSTRAINT IF EXISTS leases_rent_billing_cadence_check,
    DROP COLUMN IF EXISTS rent_billing_cadence;
