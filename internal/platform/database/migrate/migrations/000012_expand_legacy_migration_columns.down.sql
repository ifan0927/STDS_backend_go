ALTER TABLE leases
    DROP COLUMN IF EXISTS settlement_detail,
    DROP COLUMN IF EXISTS termination_reason,
    DROP COLUMN IF EXISTS notes;

ALTER TABLE tenants
    DROP COLUMN IF EXISTS occupation,
    DROP COLUMN IF EXISTS address,
    DROP COLUMN IF EXISTS national_id,
    DROP COLUMN IF EXISTS birth_date,
    ALTER COLUMN email SET NOT NULL;

ALTER TABLE rooms
    DROP COLUMN IF EXISTS zone,
    DROP COLUMN IF EXISTS notes,
    DROP COLUMN IF EXISTS default_rent_amount,
    DROP COLUMN IF EXISTS facilities,
    DROP COLUMN IF EXISTS room_type,
    DROP COLUMN IF EXISTS floor,
    DROP COLUMN IF EXISTS size;

ALTER TABLE properties
    DROP COLUMN IF EXISTS facilities,
    DROP COLUMN IF EXISTS notes,
    DROP COLUMN IF EXISTS contact_email,
    DROP COLUMN IF EXISTS contact_phone,
    DROP COLUMN IF EXISTS subtitle,
    ALTER COLUMN electricity_unit_price SET NOT NULL;
