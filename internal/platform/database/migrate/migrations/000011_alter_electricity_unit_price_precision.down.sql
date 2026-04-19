ALTER TABLE bills
    ALTER COLUMN meter_unit_price TYPE INTEGER
    USING ROUND(meter_unit_price)::INTEGER;

ALTER TABLE properties
    ALTER COLUMN electricity_unit_price TYPE INTEGER
    USING ROUND(electricity_unit_price)::INTEGER;
