package store

import (
	"context"
	"sync"
)

// TransactionUpdates fans persisted transaction changes out to interested
// frontend streams. Notifications carry no transaction data; subscribers
// re-read the durable store so the database remains the source of truth.
type TransactionUpdates struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[int64]map[uint64]chan struct{}
}

func NewTransactionUpdates() *TransactionUpdates {
	return &TransactionUpdates{
		subscribers: make(map[int64]map[uint64]chan struct{}),
	}
}

func (u *TransactionUpdates) Subscribe(transactionID int64) (<-chan struct{}, func()) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.nextID++
	subscriptionID := u.nextID
	ch := make(chan struct{}, 1)

	if u.subscribers[transactionID] == nil {
		u.subscribers[transactionID] = make(map[uint64]chan struct{})
	}
	u.subscribers[transactionID][subscriptionID] = ch

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			u.mu.Lock()
			defer u.mu.Unlock()

			delete(u.subscribers[transactionID], subscriptionID)
			if len(u.subscribers[transactionID]) == 0 {
				delete(u.subscribers, transactionID)
			}
		})
	}

	return ch, cancel
}

func (u *TransactionUpdates) Publish(transactionID int64) {
	u.mu.Lock()
	defer u.mu.Unlock()

	for _, ch := range u.subscribers[transactionID] {
		select {
		case ch <- struct{}{}:
		default:
			// A pending signal already tells the subscriber to fetch the latest
			// persisted snapshot, so duplicate signals can be coalesced safely.
		}
	}
}

type observableTransactionStore struct {
	TransactionStore
	updates *TransactionUpdates
}

func WithTransactionUpdates(inner TransactionStore, updates *TransactionUpdates) TransactionStore {
	return &observableTransactionStore{
		TransactionStore: inner,
		updates:          updates,
	}
}

func (s *observableTransactionStore) CreateTransaction(ctx context.Context, input CreateTransactionInput) (*Transaction, error) {
	tx, err := s.TransactionStore.CreateTransaction(ctx, input)
	if err == nil {
		s.updates.Publish(tx.TransactionID)
	}
	return tx, err
}

func (s *observableTransactionStore) UpdateLiveMeter(ctx context.Context, input UpdateLiveMeterInput) (*Transaction, error) {
	tx, err := s.TransactionStore.UpdateLiveMeter(ctx, input)
	if err == nil {
		s.updates.Publish(input.TransactionID)
	}
	return tx, err
}

func (s *observableTransactionStore) StopTransaction(ctx context.Context, input StopTransactionInput) (*Transaction, error) {
	tx, err := s.TransactionStore.StopTransaction(ctx, input)
	if err == nil {
		s.updates.Publish(input.TransactionID)
	}
	return tx, err
}

func (s *observableTransactionStore) ForceCloseTransaction(ctx context.Context, input ForceCloseTransactionInput) (*Transaction, error) {
	tx, err := s.TransactionStore.ForceCloseTransaction(ctx, input)
	if err == nil {
		s.updates.Publish(input.TransactionID)
	}
	return tx, err
}

func (s *observableTransactionStore) UpdateTransactionMaxKWh(ctx context.Context, transactionID int64, maxKWh float64) error {
	err := s.TransactionStore.UpdateTransactionMaxKWh(ctx, transactionID, maxKWh)
	if err == nil {
		s.updates.Publish(transactionID)
	}
	return err
}

func (s *observableTransactionStore) CheckAndMarkLimitStop(ctx context.Context, chargerID string, transactionID int64) (bool, error) {
	claimed, err := s.TransactionStore.CheckAndMarkLimitStop(ctx, chargerID, transactionID)
	if err == nil && claimed {
		s.updates.Publish(transactionID)
	}
	return claimed, err
}

func (s *observableTransactionStore) ReleaseLimitStopRequest(ctx context.Context, chargerID string, transactionID int64) error {
	err := s.TransactionStore.ReleaseLimitStopRequest(ctx, chargerID, transactionID)
	if err == nil {
		s.updates.Publish(transactionID)
	}
	return err
}
