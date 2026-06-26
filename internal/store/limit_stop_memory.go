package store

import "context"

func (s *MemoryStore) CheckAndMarkLimitStop(ctx context.Context, chargerID string, transactionID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx := s.transactions[transactionID]
	if tx == nil || tx.ChargerID != chargerID {
		return false, nil
	}

	if tx.LimitStopRequested {
		return false, nil
	}

	if tx.MaxKWh == nil || tx.TotalConsumption == nil {
		return false, nil
	}

	if *tx.TotalConsumption < *tx.MaxKWh {
		return false, nil
	}

	tx.LimitStopRequested = true
	return true, nil
}
