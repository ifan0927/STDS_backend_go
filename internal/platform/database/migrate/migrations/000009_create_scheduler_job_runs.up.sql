CREATE TABLE IF NOT EXISTS scheduler_job_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_key VARCHAR(100) NOT NULL,
    window_key VARCHAR(100) NOT NULL,
    request_id VARCHAR(100),
    retry_count INTEGER NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL CHECK (status IN ('started', 'completed', 'failed', 'skipped')),
    message TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (job_key, window_key)
);

CREATE INDEX IF NOT EXISTS idx_scheduler_job_runs_job_window
    ON scheduler_job_runs (job_key, window_key);
