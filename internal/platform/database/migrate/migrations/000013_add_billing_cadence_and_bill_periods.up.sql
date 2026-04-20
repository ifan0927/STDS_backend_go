ALTER TABLE properties
    ADD COLUMN default_electricity_billing_cadence VARCHAR(20) NOT NULL DEFAULT 'monthly',
    ADD CONSTRAINT properties_default_electricity_billing_cadence_check
        CHECK (default_electricity_billing_cadence IN ('monthly', 'bimonthly'));

ALTER TABLE leases
    ADD COLUMN electricity_billing_cadence VARCHAR(20) NOT NULL DEFAULT 'monthly',
    ADD CONSTRAINT leases_electricity_billing_cadence_check
        CHECK (electricity_billing_cadence IN ('monthly', 'bimonthly'));

ALTER TABLE bills
    ADD COLUMN period_start DATE,
    ADD COLUMN period_end DATE,
    ADD CONSTRAINT bills_period_range_check CHECK (period_start <= period_end);

UPDATE bills
SET period_start = date_trunc('month', due_date)::date,
    period_end = (date_trunc('month', due_date) + INTERVAL '1 month - 1 day')::date
WHERE period_start IS NULL
  AND period_end IS NULL;

ALTER TABLE bills
    ALTER COLUMN period_start SET NOT NULL,
    ALTER COLUMN period_end SET NOT NULL;
