DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM transactions
        WHERE transaction_id IS NULL
    ) THEN
        RAISE EXCEPTION 'cannot require transaction_id: transactions with NULL transaction_id exist';
    END IF;
END
$$;

ALTER TABLE transactions
ALTER COLUMN transaction_id SET NOT NULL;
