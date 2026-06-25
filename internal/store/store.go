package store

import (
	"context"
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

type TransactionStore interface {
	CreateTransaction(ctx context.Context, input CreateTransactionInput) (*Transaction, error)
	UpdateLiveMeter(ctx context.Context, input UpdateLiveMeterInput) (*Transaction, error)
	StopTransaction(ctx context.Context, input StopTransactionInput) (*Transaction, error)
	GetByTransactionID(ctx context.Context, chargerID string, transactionID int64) (*Transaction, error)
}
