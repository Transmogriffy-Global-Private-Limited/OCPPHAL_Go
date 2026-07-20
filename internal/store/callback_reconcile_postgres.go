package store

import (
	"context"
	"database/sql"
)

func (s *PostgresStore) ListTransactionsMissingStartCallbacks(ctx context.Context, limit int) ([]*Transaction, error) {
	return s.listTransactionsMissingCallback(ctx, "start_transaction", false, limit)
}

func (s *PostgresStore) ListTransactionsMissingCompletedCallbacks(ctx context.Context, limit int) ([]*Transaction, error) {
	return s.listTransactionsMissingCallback(ctx, "completed_transaction", true, limit)
}

func (s *PostgresStore) listTransactionsMissingCallback(
	ctx context.Context,
	kind string,
	completedOnly bool,
	limit int,
) ([]*Transaction, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
	t.id,
	t.uuiddb,
	t.charger_id,
	t.connector_id,
	t.meter_start,
	t.meter_stop,
	t.total_consumption,
	t.start_time,
	t.stop_time,
	t.id_tag,
	t.transaction_id,
	t.is_single_session,
	t.max_kwh,
	t.limit_stop_requested
 FROM transactions t
 LEFT JOIN callback_outbox c
   ON c.dedupe_key = $1 || ':' || t.transaction_id::text
 WHERE c.id IS NULL
   AND ($2 = FALSE OR t.stop_time IS NOT NULL)
 ORDER BY t.id
 LIMIT $3`,
		kind,
		completedOnly,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*Transaction, 0)
	for rows.Next() {
		var tx Transaction
		var meterStop sql.NullFloat64
		var totalConsumption sql.NullFloat64
		var stopTime sql.NullTime
		var maxKWh sql.NullFloat64

		if err := rows.Scan(
			&tx.ID,
			&tx.UUIDDB,
			&tx.ChargerID,
			&tx.ConnectorID,
			&tx.MeterStart,
			&meterStop,
			&totalConsumption,
			&tx.StartTime,
			&stopTime,
			&tx.IDTag,
			&tx.TransactionID,
			&tx.IsSingleSession,
			&maxKWh,
			&tx.LimitStopRequested,
		); err != nil {
			return nil, err
		}

		if meterStop.Valid {
			tx.MeterStop = &meterStop.Float64
		}
		if totalConsumption.Valid {
			tx.TotalConsumption = &totalConsumption.Float64
		}
		if stopTime.Valid {
			tx.StopTime = &stopTime.Time
		}
		if maxKWh.Valid {
			tx.MaxKWh = &maxKWh.Float64
		}

		out = append(out, &tx)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}
