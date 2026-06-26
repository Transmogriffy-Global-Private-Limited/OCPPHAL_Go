package store

import (
	"context"
	"database/sql"
)

func (s *PostgresStore) CheckAndMarkLimitStop(ctx context.Context, chargerID string, transactionID int64) (bool, error) {
	var returnedID int64

	err := s.db.QueryRowContext(
		ctx,
		`UPDATE transactions
 SET limit_stop_requested = TRUE
 WHERE charger_id = $1
   AND transaction_id = $2
   AND limit_stop_requested = FALSE
   AND max_kwh IS NOT NULL
   AND total_consumption IS NOT NULL
   AND total_consumption >= max_kwh
 RETURNING transaction_id`,
		chargerID,
		transactionID,
	).Scan(&returnedID)

	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}

	return true, nil
}
