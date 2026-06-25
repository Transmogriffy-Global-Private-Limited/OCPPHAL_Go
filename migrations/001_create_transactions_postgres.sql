CREATE TABLE IF NOT EXISTS transactions (
    id BIGSERIAL PRIMARY KEY,
    uuiddb TEXT NOT NULL,
    charger_id TEXT NOT NULL,
    connector_id INTEGER NOT NULL,
    meter_start DOUBLE PRECISION NOT NULL,
    meter_stop DOUBLE PRECISION NULL,
    total_consumption DOUBLE PRECISION NULL,
    start_time TIMESTAMPTZ NOT NULL,
    stop_time TIMESTAMPTZ NULL,
    id_tag TEXT NOT NULL,
    transaction_id BIGINT UNIQUE,
    is_single_session BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_transactions_charger_connector_idtag
ON transactions (charger_id, connector_id, id_tag);

CREATE INDEX IF NOT EXISTS idx_transactions_charger_transaction
ON transactions (charger_id, transaction_id);
