ALTER TABLE properties
    ADD COLUMN IF NOT EXISTS property_public_name VARCHAR(200);

UPDATE properties
SET property_public_name = name
WHERE property_public_name IS NULL;

ALTER TABLE properties
    ALTER COLUMN property_public_name SET NOT NULL,
    ADD CONSTRAINT properties_property_public_name_non_empty_check CHECK (btrim(property_public_name) <> '');
