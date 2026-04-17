DROP TABLE IF EXISTS bill_attachments;
DROP TABLE IF EXISTS repair_request_attachments;
DROP TABLE IF EXISTS journal_log_attachments;
DROP TABLE IF EXISTS lease_attachments;
DROP TABLE IF EXISTS tenant_attachments;
DROP TABLE IF EXISTS room_attachments;
DROP TABLE IF EXISTS property_attachments;
DROP TABLE IF EXISTS attachment_upload_tokens;

ALTER TABLE leases
    DROP CONSTRAINT IF EXISTS leases_deposit_status_check;

ALTER TABLE leases
    ADD CONSTRAINT leases_deposit_status_check
    CHECK (deposit_status IN ('held', 'refunded', 'deducted', 'written_off'));

UPDATE leases
SET deposit_status = CASE
    WHEN deposit_status = 'settled' AND COALESCE(deposit_refund_amount, 0) > 0 AND COALESCE(deposit_deduction_amount, 0) = 0 THEN 'refunded'
    WHEN deposit_status = 'settled' AND COALESCE(deposit_deduction_amount, 0) > 0 AND COALESCE(deposit_refund_amount, 0) = 0 THEN 'deducted'
    WHEN deposit_status = 'settled' THEN 'deducted'
    ELSE deposit_status
END;

ALTER TABLE leases
    DROP COLUMN IF EXISTS deposit_deduction_amount,
    DROP COLUMN IF EXISTS deposit_refund_amount;
