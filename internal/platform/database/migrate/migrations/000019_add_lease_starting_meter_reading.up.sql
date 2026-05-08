ALTER TABLE leases
    ADD COLUMN starting_meter_reading INTEGER,
    ADD CONSTRAINT leases_starting_meter_reading_check
        CHECK (starting_meter_reading IS NULL OR starting_meter_reading >= 0);
