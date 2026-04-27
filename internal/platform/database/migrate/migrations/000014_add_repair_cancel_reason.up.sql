ALTER TABLE repair_requests
    ADD COLUMN IF NOT EXISTS cancel_reason TEXT;
