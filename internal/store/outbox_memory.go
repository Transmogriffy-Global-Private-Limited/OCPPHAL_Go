package store

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"
)

var memoryOutboxID int64

func (s *MemoryStore) UpdateTransactionMaxKWh(ctx context.Context, transactionID int64, maxKWh float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx := s.transactions[transactionID]
	if tx == nil {
		return nil
	}

	tx.MaxKWh = &maxKWh
	return nil
}

func (s *MemoryStore) EnqueueCallback(ctx context.Context, input EnqueueCallbackInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.outbox == nil {
		s.outbox = make(map[int64]*memoryCallback)
	}

	for _, existing := range s.outbox {
		if existing.DedupeKey == input.DedupeKey {
			return nil
		}
	}

	payload, err := json.Marshal(input.Payload)
	if err != nil {
		return err
	}

	id := atomic.AddInt64(&memoryOutboxID, 1)

	s.outbox[id] = &memoryCallback{
		ID:            id,
		Kind:          input.Kind,
		DedupeKey:     input.DedupeKey,
		TransactionID: input.TransactionID,
		UUIDDB:        input.UUIDDB,
		TargetURL:     input.TargetURL,
		Payload:       payload,
		Status:        "pending",
		Retries:       0,
		MaxRetries:    input.MaxRetries,
		NextRetryAt:   time.Now().UTC(),
	}

	return nil
}

func (s *MemoryStore) ClaimDueCallbacks(ctx context.Context, limit int) ([]CallbackTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	var tasks []CallbackTask

	for _, item := range s.outbox {
		if len(tasks) >= limit {
			break
		}

		if item.Status != "pending" || item.NextRetryAt.After(now) {
			continue
		}

		item.Status = "processing"

		tasks = append(tasks, CallbackTask{
			ID:            item.ID,
			Kind:          item.Kind,
			DedupeKey:     item.DedupeKey,
			TransactionID: item.TransactionID,
			UUIDDB:        item.UUIDDB,
			TargetURL:     item.TargetURL,
			Payload:       item.Payload,
			Retries:       item.Retries,
			MaxRetries:    item.MaxRetries,
		})
	}

	return tasks, nil
}

func (s *MemoryStore) MarkCallbackSent(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if item := s.outbox[id]; item != nil {
		item.Status = "sent"
	}

	return nil
}

func (s *MemoryStore) MarkCallbackRetry(ctx context.Context, id int64, retries int, nextRetryAt time.Time, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if item := s.outbox[id]; item != nil {
		item.Status = "pending"
		item.Retries = retries
		item.NextRetryAt = nextRetryAt
		item.LastError = lastError
	}

	return nil
}

func (s *MemoryStore) MarkCallbackFatal(ctx context.Context, id int64, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if item := s.outbox[id]; item != nil {
		item.Status = "fatal"
		item.LastError = lastError
	}

	return nil
}
