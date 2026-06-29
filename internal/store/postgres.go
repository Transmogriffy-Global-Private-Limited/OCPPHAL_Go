package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/config"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(cfg config.Config) (*PostgresStore, error) {
	dsn := strings.TrimSpace(cfg.DatabaseURL)
	if dsn == "" {
		dsn = postgresURL(cfg)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &PostgresStore{db: db}, nil
}

func postgresURL(cfg config.Config) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.DBUser, cfg.DBPassword),
		Host:   fmt.Sprintf("%s:%d", cfg.DBHost, cfg.DBPort),
		Path:   cfg.DBName,
	}
	q := u.Query()
	q.Set("sslmode", cfg.DBSSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *PostgresStore) CreateTransaction(ctx context.Context, input CreateTransactionInput) (*Transaction, error) {
	for attempts := 0; attempts < 100; attempts++ {
		tid := RandomTransactionID()
		uuiddb := NewUUIDString()
		startTime := time.Now().UTC()

		var rowID int64
		err := s.db.QueryRowContext(
			ctx,
			`INSERT INTO transactions
(uuiddb, charger_id, connector_id, meter_start, start_time, id_tag, transaction_id, is_single_session)
 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
 RETURNING id`,
			uuiddb,
			input.ChargerID,
			input.ConnectorID,
			input.MeterStart,
			startTime,
			input.IDTag,
			tid,
			input.IsSingleSession,
		).Scan(&rowID)

		if err != nil {
			if isDuplicate(err) {
				continue
			}
			return nil, err
		}

		return &Transaction{
			ID:              rowID,
			UUIDDB:          uuiddb,
			ChargerID:       input.ChargerID,
			ConnectorID:     input.ConnectorID,
			MeterStart:      input.MeterStart,
			StartTime:       startTime,
			IDTag:           input.IDTag,
			TransactionID:   tid,
			IsSingleSession: input.IsSingleSession,
		}, nil
	}

	return nil, errors.New("could not allocate unique transaction id after 100 attempts")
}

func (s *PostgresStore) UpdateLiveMeter(ctx context.Context, input UpdateLiveMeterInput) (*Transaction, error) {
	tx, err := s.GetByTransactionID(ctx, input.ChargerID, input.TransactionID)
	if err != nil {
		return nil, err
	}

	total := DeltaWh(tx.MeterStart, input.MeterStop) / 1000.0

	_, err = s.db.ExecContext(
		ctx,
		`UPDATE transactions
 SET meter_stop = $1, total_consumption = $2
 WHERE charger_id = $3 AND transaction_id = $4`,
		input.MeterStop,
		total,
		input.ChargerID,
		input.TransactionID,
	)
	if err != nil {
		return nil, err
	}

	tx.MeterStop = &input.MeterStop
	tx.TotalConsumption = &total
	return tx, nil
}

func (s *PostgresStore) StopTransaction(ctx context.Context, input StopTransactionInput) (*Transaction, error) {
	tx, err := s.GetByTransactionID(ctx, input.ChargerID, input.TransactionID)
	if err != nil {
		return nil, err
	}

	stopTime := time.Now().UTC()
	total := DeltaWh(tx.MeterStart, input.MeterStop) / 1000.0

	_, err = s.db.ExecContext(
		ctx,
		`UPDATE transactions
 SET meter_stop = $1, total_consumption = $2, stop_time = $3
 WHERE charger_id = $4 AND transaction_id = $5`,
		input.MeterStop,
		total,
		stopTime,
		input.ChargerID,
		input.TransactionID,
	)
	if err != nil {
		return nil, err
	}

	tx.MeterStop = &input.MeterStop
	tx.TotalConsumption = &total
	tx.StopTime = &stopTime
	return tx, nil
}

func (s *PostgresStore) GetByTransactionID(ctx context.Context, chargerID string, transactionID int64) (*Transaction, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT
id,
uuiddb,
charger_id,
connector_id,
meter_start,
meter_stop,
total_consumption,
start_time,
stop_time,
id_tag,
transaction_id,
is_single_session
 FROM transactions
 WHERE charger_id = $1 AND transaction_id = $2
 LIMIT 1`,
		chargerID,
		transactionID,
	)

	var tx Transaction
	var meterStop sql.NullFloat64
	var totalConsumption sql.NullFloat64
	var stopTime sql.NullTime

	err := row.Scan(
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
	)
	if err != nil {
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

	return &tx, nil
}

func (s *PostgresStore) ForceCloseTransaction(ctx context.Context, input ForceCloseTransactionInput) (*Transaction, error) {
	tx, err := s.GetByTransactionID(ctx, input.ChargerID, input.TransactionID)
	if err != nil {
		return nil, err
	}

	stopTime := time.Now().UTC()
	total := DeltaWh(tx.MeterStart, input.MeterStop) / 1000.0

	_, err = s.db.ExecContext(
		ctx,
		`UPDATE transactions
 SET meter_stop = $1, total_consumption = $2, stop_time = $3
 WHERE charger_id = $4
   AND transaction_id = $5
   AND stop_time IS NULL`,
		input.MeterStop,
		total,
		stopTime,
		input.ChargerID,
		input.TransactionID,
	)
	if err != nil {
		return nil, err
	}

	tx.MeterStop = &input.MeterStop
	tx.TotalConsumption = &total
	tx.StopTime = &stopTime

	return tx, nil
}

func (s *PostgresStore) ListOpenTransactionsByCharger(ctx context.Context, chargerID string) ([]*Transaction, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
id,
uuiddb,
charger_id,
connector_id,
meter_start,
meter_stop,
total_consumption,
start_time,
stop_time,
id_tag,
transaction_id,
is_single_session
 FROM transactions
 WHERE charger_id = $1
   AND stop_time IS NULL
   AND transaction_id IS NOT NULL
 ORDER BY start_time DESC`,
		chargerID,
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

		out = append(out, &tx)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func isDuplicate(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
