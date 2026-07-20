package store

import (
	"context"
	"fmt"
)

func (s *MemoryStore) ListTransactionsMissingStartCallbacks(ctx context.Context, limit int) ([]*Transaction, error) {
	return s.listTransactionsMissingCallback(KindStartTransactionStore, false, limit), nil
}

func (s *MemoryStore) ListTransactionsMissingCompletedCallbacks(ctx context.Context, limit int) ([]*Transaction, error) {
	return s.listTransactionsMissingCallback(KindCompletedTransactionStore, true, limit), nil
}

const (
	KindStartTransactionStore     = "start_transaction"
	KindCompletedTransactionStore = "completed_transaction"
)

func (s *MemoryStore) listTransactionsMissingCallback(kind string, completedOnly bool, limit int) []*Transaction {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*Transaction, 0)
	for _, tx := range s.transactions {
		if len(out) >= limit {
			break
		}
		if tx == nil || (completedOnly && tx.StopTime == nil) {
			continue
		}

		dedupeKey := fmt.Sprintf("%s:%d", kind, tx.TransactionID)
		found := false
		for _, callback := range s.outbox {
			if callback != nil && callback.DedupeKey == dedupeKey {
				found = true
				break
			}
		}
		if !found {
			out = append(out, cloneTransaction(tx))
		}
	}

	return out
}
