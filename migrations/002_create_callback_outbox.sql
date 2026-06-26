ALTER TABLE transactions
ADD COLUMN IF NOT EXISTS max_kwh DOUBLE PRECISION NULL;

CREATE TABLE IF NOT EXISTS callback_outbox (
    id BIGSERIAL PRIMARY KEY,
    kind TEXT NOT NULL,
    dedupe_key TEXT NOT NULL UNIQUE,
    transaction_id BIGINT NULL,
    uuiddb TEXT NULL,
    target_url TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    retries INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 10,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_callback_outbox_due
ON callback_outbox (status, next_retry_at, id);

CREATE INDEX IF NOT EXISTS idx_callback_outbox_transaction
ON callback_outbox (transaction_id);
