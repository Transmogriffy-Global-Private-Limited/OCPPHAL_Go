ALTER TABLE transactions
ADD COLUMN IF NOT EXISTS limit_stop_requested BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_transactions_limit_stop
ON transactions (charger_id, transaction_id, limit_stop_requested);
