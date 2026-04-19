ALTER TABLE properties
    ALTER COLUMN electricity_unit_price TYPE NUMERIC(10,4)
    USING electricity_unit_price::NUMERIC(10,4);

ALTER TABLE bills
    ALTER COLUMN meter_unit_price TYPE NUMERIC(10,4)
    USING meter_unit_price::NUMERIC(10,4);
