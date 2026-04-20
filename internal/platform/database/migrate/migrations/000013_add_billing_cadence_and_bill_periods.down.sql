ALTER TABLE bills
    DROP CONSTRAINT IF EXISTS bills_period_range_check,
    DROP COLUMN IF EXISTS period_end,
    DROP COLUMN IF EXISTS period_start;

ALTER TABLE leases
    DROP CONSTRAINT IF EXISTS leases_electricity_billing_cadence_check,
    DROP COLUMN IF EXISTS electricity_billing_cadence;

ALTER TABLE properties
    DROP CONSTRAINT IF EXISTS properties_default_electricity_billing_cadence_check,
    DROP COLUMN IF EXISTS default_electricity_billing_cadence;
