ALTER TABLE properties
    DROP CONSTRAINT IF EXISTS properties_property_public_name_non_empty_check,
    DROP COLUMN IF EXISTS property_public_name;
