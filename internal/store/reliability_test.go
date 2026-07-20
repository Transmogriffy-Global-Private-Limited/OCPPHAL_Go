package store

import (
	"context"
	"errors"
	"strconv"
	"testing"
)

func TestMemoryStoreTransactionLifecycleIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	tx, err := s.CreateTransaction(ctx, CreateTransactionInput{
		ChargerID:   "CP-1",
		ConnectorID: 1,
		MeterStart:  1000,
		IDTag:       "USER-1",
	})
	if err != nil {
		t.Fatalf("create transaction: %v", err)
	}

	missingStart, err := s.ListTransactionsMissingStartCallbacks(ctx, 10)
	if err != nil {
		t.Fatalf("list missing start callbacks: %v", err)
	}
	if len(missingStart) != 1 || missingStart[0].TransactionID != tx.TransactionID {
		t.Fatalf("missing start callbacks = %#v, want transaction %d", missingStart, tx.TransactionID)
	}

	txID := tx.TransactionID
	if err := s.EnqueueCallback(ctx, EnqueueCallbackInput{
		Kind:          KindStartTransactionStore,
		DedupeKey:     "start_transaction:" + strconv.FormatInt(tx.TransactionID, 10),
		TransactionID: &txID,
		UUIDDB:        tx.UUIDDB,
		TargetURL:     "http://cms/start",
		Payload:       map[string]any{"transactionid": tx.TransactionID},
		MaxRetries:    6,
	}); err != nil {
		t.Fatalf("enqueue start callback: %v", err)
	}

	missingStart, err = s.ListTransactionsMissingStartCallbacks(ctx, 10)
	if err != nil {
		t.Fatalf("list missing start callbacks after enqueue: %v", err)
	}
	if len(missingStart) != 0 {
		t.Fatalf("missing start callbacks after enqueue = %d, want 0", len(missingStart))
	}

	stopped, err := s.StopTransaction(ctx, StopTransactionInput{
		ChargerID:     "CP-1",
		TransactionID: tx.TransactionID,
		MeterStop:     2500,
	})
	if err != nil {
		t.Fatalf("stop transaction: %v", err)
	}
	if stopped.TotalConsumption == nil || *stopped.TotalConsumption != 1.5 {
		t.Fatalf("total consumption = %v, want 1.5", stopped.TotalConsumption)
	}
	firstStopTime := *stopped.StopTime

	retried, err := s.StopTransaction(ctx, StopTransactionInput{
		ChargerID:     "CP-1",
		TransactionID: tx.TransactionID,
		MeterStop:     9000,
	})
	if err != nil {
		t.Fatalf("retry stop transaction: %v", err)
	}
	if retried.TotalConsumption == nil || *retried.TotalConsumption != 1.5 {
		t.Fatalf("retried total consumption = %v, want original 1.5", retried.TotalConsumption)
	}
	if retried.StopTime == nil || !retried.StopTime.Equal(firstStopTime) {
		t.Fatalf("retried stop time = %v, want original %v", retried.StopTime, firstStopTime)
	}

	missingCompleted, err := s.ListTransactionsMissingCompletedCallbacks(ctx, 10)
	if err != nil {
		t.Fatalf("list missing completed callbacks: %v", err)
	}
	if len(missingCompleted) != 1 || missingCompleted[0].TransactionID != tx.TransactionID {
		t.Fatalf("missing completed callbacks = %#v, want transaction %d", missingCompleted, tx.TransactionID)
	}
}

func TestMemoryStoreLimitStopClaimCanBeReleased(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	tx, err := s.CreateTransaction(ctx, CreateTransactionInput{
		ChargerID:   "CP-LIMIT",
		ConnectorID: 1,
		MeterStart:  1000,
		IDTag:       "USER-1",
	})
	if err != nil {
		t.Fatalf("create transaction: %v", err)
	}
	if err := s.UpdateTransactionMaxKWh(ctx, tx.TransactionID, 1); err != nil {
		t.Fatalf("update max kWh: %v", err)
	}
	if _, err := s.UpdateLiveMeter(ctx, UpdateLiveMeterInput{
		ChargerID:     tx.ChargerID,
		TransactionID: tx.TransactionID,
		MeterStop:     2500,
	}); err != nil {
		t.Fatalf("update live meter: %v", err)
	}

	claimed, err := s.CheckAndMarkLimitStop(ctx, tx.ChargerID, tx.TransactionID)
	if err != nil || !claimed {
		t.Fatalf("first limit-stop claim = %v, %v; want true, nil", claimed, err)
	}
	claimed, err = s.CheckAndMarkLimitStop(ctx, tx.ChargerID, tx.TransactionID)
	if err != nil || claimed {
		t.Fatalf("second limit-stop claim = %v, %v; want false, nil", claimed, err)
	}

	if err := s.ReleaseLimitStopRequest(ctx, tx.ChargerID, tx.TransactionID); err != nil {
		t.Fatalf("release limit-stop claim: %v", err)
	}
	claimed, err = s.CheckAndMarkLimitStop(ctx, tx.ChargerID, tx.TransactionID)
	if err != nil || !claimed {
		t.Fatalf("reclaimed limit stop = %v, %v; want true, nil", claimed, err)
	}
}

func TestMemoryStoreFindsTransactionByExactIDTag(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	tx, err := s.CreateTransaction(ctx, CreateTransactionInput{
		ChargerID:   "CP-EXACT",
		ConnectorID: 1,
		MeterStart:  1000,
		IDTag:       "USER-EXACT",
	})
	if err != nil {
		t.Fatalf("create transaction: %v", err)
	}

	found, err := s.GetByTransactionIDAndIDTag(ctx, tx.TransactionID, "USER-EXACT")
	if err != nil {
		t.Fatalf("get exact transaction: %v", err)
	}
	if found.TransactionID != tx.TransactionID || found.IDTag != "USER-EXACT" {
		t.Fatalf("exact transaction = %#v, want transaction %d for USER-EXACT", found, tx.TransactionID)
	}

	_, err = s.GetByTransactionIDAndIDTag(ctx, tx.TransactionID, "OTHER-USER")
	if !errors.Is(err, ErrTransactionNotFound) {
		t.Fatalf("mismatched idTag error = %v, want ErrTransactionNotFound", err)
	}
}
