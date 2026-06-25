package store

import (
	"context"
	"errors"
	"sync"
	"time"
)

type MemoryStore struct {
	mu           sync.Mutex
	nextRowID    int64
	transactions map[int64]*Transaction
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		nextRowID:    1,
		transactions: make(map[int64]*Transaction),
	}
}

func (s *MemoryStore) CreateTransaction(ctx context.Context, input CreateTransactionInput) (*Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for attempts := 0; attempts < 100; attempts++ {
		tid := RandomTransactionID()
		if _, exists := s.transactions[tid]; exists {
			continue
		}

		tx := &Transaction{
			ID:              s.nextRowID,
			UUIDDB:          NewUUIDString(),
			ChargerID:       input.ChargerID,
			ConnectorID:     input.ConnectorID,
			MeterStart:      input.MeterStart,
			StartTime:       time.Now().UTC(),
			IDTag:           input.IDTag,
			TransactionID:   tid,
			IsSingleSession: input.IsSingleSession,
		}

		s.nextRowID++
		s.transactions[tid] = tx
		return cloneTransaction(tx), nil
	}

	return nil, errors.New("could not allocate unique transaction id after 100 attempts")
}

func (s *MemoryStore) UpdateLiveMeter(ctx context.Context, input UpdateLiveMeterInput) (*Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx := s.transactions[input.TransactionID]
	if tx == nil || tx.ChargerID != input.ChargerID {
		return nil, errors.New("transaction not found")
	}

	tx.MeterStop = floatPtr(input.MeterStop)
	total := DeltaWh(tx.MeterStart, input.MeterStop) / 1000.0
	tx.TotalConsumption = floatPtr(total)
	return cloneTransaction(tx), nil
}

func (s *MemoryStore) StopTransaction(ctx context.Context, input StopTransactionInput) (*Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx := s.transactions[input.TransactionID]
	if tx == nil || tx.ChargerID != input.ChargerID {
		return nil, errors.New("transaction not found")
	}

	now := time.Now().UTC()
	tx.MeterStop = floatPtr(input.MeterStop)
	total := DeltaWh(tx.MeterStart, input.MeterStop) / 1000.0
	tx.TotalConsumption = floatPtr(total)
	tx.StopTime = &now
	return cloneTransaction(tx), nil
}

func (s *MemoryStore) GetByTransactionID(ctx context.Context, chargerID string, transactionID int64) (*Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx := s.transactions[transactionID]
	if tx == nil || tx.ChargerID != chargerID {
		return nil, errors.New("transaction not found")
	}
	return cloneTransaction(tx), nil
}

func cloneTransaction(tx *Transaction) *Transaction {
	if tx == nil {
		return nil
	}

	copyTx := *tx

	if tx.MeterStop != nil {
		v := *tx.MeterStop
		copyTx.MeterStop = &v
	}
	if tx.TotalConsumption != nil {
		v := *tx.TotalConsumption
		copyTx.TotalConsumption = &v
	}
	if tx.StopTime != nil {
		v := *tx.StopTime
		copyTx.StopTime = &v
	}

	return &copyTx
}
