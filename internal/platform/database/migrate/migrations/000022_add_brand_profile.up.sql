CREATE TABLE brand_profiles (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    singleton_key   BOOLEAN     NOT NULL DEFAULT true UNIQUE CHECK (singleton_key),
    brand_name      VARCHAR(200) NOT NULL CHECK (btrim(brand_name) <> ''),
    contact_phone   VARCHAR(50),
    contact_email   VARCHAR(255),
    contact_address TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    version         INTEGER     NOT NULL DEFAULT 1
);

CREATE VIEW approved_brand_profile_v1 AS
SELECT
    brand_name,
    contact_phone,
    contact_email,
    contact_address,
    updated_at
FROM brand_profiles;
