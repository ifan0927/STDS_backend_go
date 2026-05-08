ALTER TABLE leases
    DROP CONSTRAINT IF EXISTS leases_starting_meter_reading_check,
    DROP COLUMN IF EXISTS starting_meter_reading;
