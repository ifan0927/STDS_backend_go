CREATE VIEW approved_brand_property_availability_v1 AS
SELECT
    p.id AS property_id,
    p.property_public_name,
    p.address,
    EXISTS (
        SELECT 1
        FROM rooms r
        WHERE r.property_id = p.id
          AND r.status = 'vacant'
          AND r.deleted_at IS NULL
    ) AS has_vacant_room
FROM properties p
WHERE p.deleted_at IS NULL;
