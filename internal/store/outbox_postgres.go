package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

func (s *PostgresStore) UpdateTransactionMaxKWh(ctx context.Context, transactionID int64, maxKWh float64) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE transactions
 SET max_kwh = $1
 WHERE transaction_id = $2`,
		maxKWh,
		transactionID,
	)
	return err
}

func (s *PostgresStore) EnqueueCallback(ctx context.Context, input EnqueueCallbackInput) error {
	payload, err := json.Marshal(input.Payload)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO callback_outbox
(kind, dedupe_key, transaction_id, uuiddb, target_url, payload, max_retries)
 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
 ON CONFLICT (dedupe_key) DO NOTHING`,
		input.Kind,
		input.DedupeKey,
		input.TransactionID,
		input.UUIDDB,
		input.TargetURL,
		string(payload),
		input.MaxRetries,
	)

	return err
}

func (s *PostgresStore) ClaimDueCallbacks(ctx context.Context, limit int) ([]CallbackTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, kind, dedupe_key, transaction_id, uuiddb, target_url, payload, retries, max_retries
 FROM callback_outbox
 WHERE (status = 'pending' AND next_retry_at <= NOW())
    OR (status = 'processing' AND updated_at <= NOW() - INTERVAL '2 minutes')
 ORDER BY id
 LIMIT $1
 FOR UPDATE SKIP LOCKED`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []CallbackTask
	var ids []int64

	for rows.Next() {
		var task CallbackTask
		var transactionID sql.NullInt64

		if err := rows.Scan(
			&task.ID,
			&task.Kind,
			&task.DedupeKey,
			&transactionID,
			&task.UUIDDB,
			&task.TargetURL,
			&task.Payload,
			&task.Retries,
			&task.MaxRetries,
		); err != nil {
			return nil, err
		}

		if transactionID.Valid {
			v := transactionID.Int64
			task.TransactionID = &v
		}

		tasks = append(tasks, task)
		ids = append(ids, task.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, id := range ids {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE callback_outbox
 SET status = 'processing', updated_at = NOW()
 WHERE id = $1`,
			id,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return tasks, nil
}

func (s *PostgresStore) MarkCallbackSent(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE callback_outbox
 SET status = 'sent', sent_at = NOW(), updated_at = NOW(), last_error = NULL
 WHERE id = $1`,
		id,
	)
	return err
}

func (s *PostgresStore) MarkCallbackRetry(ctx context.Context, id int64, retries int, nextRetryAt time.Time, lastError string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE callback_outbox
 SET status = 'pending', retries = $2, next_retry_at = $3, last_error = $4, updated_at = NOW()
 WHERE id = $1`,
		id,
		retries,
		nextRetryAt,
		lastError,
	)
	return err
}

func (s *PostgresStore) MarkCallbackFatal(ctx context.Context, id int64, lastError string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE callback_outbox
 SET status = 'fatal', last_error = $2, updated_at = NOW()
 WHERE id = $1`,
		id,
		lastError,
	)
	return err
}
