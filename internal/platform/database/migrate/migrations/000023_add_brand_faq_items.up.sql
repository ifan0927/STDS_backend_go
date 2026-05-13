CREATE TABLE brand_faq_items (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question   TEXT        NOT NULL CHECK (btrim(question) <> ''),
    answer     TEXT        NOT NULL CHECK (btrim(answer) <> ''),
    sort_order INTEGER     NOT NULL DEFAULT 0,
    is_active  BOOLEAN     NOT NULL DEFAULT true,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    version    INTEGER     NOT NULL DEFAULT 1
);

CREATE INDEX idx_brand_faq_items_sort_order
    ON brand_faq_items (sort_order)
    WHERE deleted_at IS NULL;

CREATE VIEW approved_brand_faq_items_v1 AS
SELECT
    question,
    answer,
    sort_order
FROM brand_faq_items
WHERE is_active = true
  AND deleted_at IS NULL
ORDER BY sort_order;
