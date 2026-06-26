package store

import (
	"context"
	"encoding/json"
	"time"
)

type Transaction struct {
	ID               int64
	UUIDDB           string
	ChargerID        string
	ConnectorID      int
	MeterStart       float64
	MeterStop        *float64
	TotalConsumption *float64
	StartTime        time.Time
	StopTime         *time.Time
	IDTag            string
	TransactionID    int64
	IsSingleSession  bool
	MaxKWh           *float64
}

type CreateTransactionInput struct {
	ChargerID       string
	ConnectorID     int
	MeterStart      float64
	IDTag           string
	IsSingleSession bool
}

type UpdateLiveMeterInput struct {
	ChargerID     string
	TransactionID int64
	MeterStop     float64
}

type StopTransactionInput struct {
	ChargerID     string
	TransactionID int64
	MeterStop     float64
}

type EnqueueCallbackInput struct {
	Kind          string
	DedupeKey     string
	TransactionID *int64
	UUIDDB        string
	TargetURL     string
	Payload       map[string]any
	MaxRetries    int
}

type CallbackTask struct {
	ID            int64
	Kind          string
	DedupeKey     string
	TransactionID *int64
	UUIDDB        string
	TargetURL     string
	Payload       json.RawMessage
	Retries       int
	MaxRetries    int
}

type TransactionStore interface {
	CreateTransaction(ctx context.Context, input CreateTransactionInput) (*Transaction, error)
	UpdateLiveMeter(ctx context.Context, input UpdateLiveMeterInput) (*Transaction, error)
	StopTransaction(ctx context.Context, input StopTransactionInput) (*Transaction, error)
	GetByTransactionID(ctx context.Context, chargerID string, transactionID int64) (*Transaction, error)
	UpdateTransactionMaxKWh(ctx context.Context, transactionID int64, maxKWh float64) error

	ChargerAnalytics(ctx context.Context, input AnalyticsInput) (*AnalyticsOutput, error)

	EnqueueCallback(ctx context.Context, input EnqueueCallbackInput) error
	ClaimDueCallbacks(ctx context.Context, limit int) ([]CallbackTask, error)
	MarkCallbackSent(ctx context.Context, id int64) error
	MarkCallbackRetry(ctx context.Context, id int64, retries int, nextRetryAt time.Time, lastError string) error
	MarkCallbackFatal(ctx context.Context, id int64, lastError string) error
}
