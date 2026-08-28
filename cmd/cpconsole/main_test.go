package main

import (
	"sync"
	"testing"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"
)

func TestParseStatusIsCaseInsensitive(t *testing.T) {
	got, ok := parseStatus("suspendedevse")
	if !ok || got != core.ChargePointStatusSuspendedEVSE {
		t.Fatalf("parseStatus() = %q, %t", got, ok)
	}
}

func TestParseErrorCodeRejectsUnknownCode(t *testing.T) {
	if _, ok := parseErrorCode("DefinitelyNotOCPP"); ok {
		t.Fatal("parseErrorCode accepted an unknown code")
	}
}

func TestParseStopReason(t *testing.T) {
	got, ok := parseStopReason("evdisconnected")
	if !ok || got != core.ReasonEVDisconnected {
		t.Fatalf("parseStopReason() = %q, %t", got, ok)
	}
}

func TestParseBoolPolicy(t *testing.T) {
	if value, ok := parseBoolPolicy("ACCEPT", "accept", "reject"); !ok || !value {
		t.Fatalf("parseBoolPolicy accept = %t, %t", value, ok)
	}
	if value, ok := parseBoolPolicy("reject", "accept", "reject"); !ok || value {
		t.Fatalf("parseBoolPolicy reject = %t, %t", value, ok)
	}
}

func TestNormalizeWebSocketURL(t *testing.T) {
	got, err := normalizeWebSocketURL(" wss://ocpp.example.com/base/ ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://ocpp.example.com/base" {
		t.Fatalf("normalizeWebSocketURL() = %q", got)
	}
	if _, err := normalizeWebSocketURL("https://ocpp.example.com"); err == nil {
		t.Fatal("normalizeWebSocketURL accepted an HTTP URL")
	}
}

func TestAcceptedRemoteStopCompletesOnceAndReturnsAvailable(t *testing.T) {
	s := newRemoteStopTestSimulator()
	statuses := make(chan core.ChargePointStatus, 5)
	stopEntered := make(chan struct{})
	allowStop := make(chan struct{})
	var stopCalls int
	s.stopTransactionFunc = func(_ int, transactionID int, _ string, reason core.Reason) (*core.StopTransactionConfirmation, error) {
		stopCalls++
		if transactionID != 1786889261 || reason != core.ReasonRemote {
			return nil, errTestStop
		}
		close(stopEntered)
		<-allowStop
		return core.NewStopTransactionConfirmation(), nil
	}
	s.statusFunc = func(status core.ChargePointStatus, _ core.ChargePointErrorCode) error {
		statuses <- status
		return nil
	}

	request := &core.RemoteStopTransactionRequest{TransactionId: 1786889261}
	confirmation, err := s.OnRemoteStopTransaction(request)
	if err != nil || confirmation.Status != types.RemoteStartStopStatusAccepted {
		t.Fatalf("first RemoteStopTransaction = %#v, %v", confirmation, err)
	}
	select {
	case <-stopEntered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the claimed stop")
	}
	duplicate, err := s.OnRemoteStopTransaction(request)
	if err != nil || duplicate.Status != types.RemoteStartStopStatusAccepted {
		t.Fatalf("duplicate RemoteStopTransaction = %#v, %v", duplicate, err)
	}
	close(allowStop)

	want := []core.ChargePointStatus{core.ChargePointStatusFinishing, core.ChargePointStatusAvailable}
	for _, expected := range want {
		select {
		case got := <-statuses:
			if got != expected {
				t.Fatalf("status ordering = %s, want %s", got, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s", expected)
		}
	}

	s.mu.RLock()
	transaction, stopping, status := s.transaction, s.stoppingTransaction, s.status
	s.mu.RUnlock()
	if stopCalls != 1 || transaction != 0 || stopping != 0 || status != core.ChargePointStatusAvailable {
		t.Fatalf("stopCalls=%d transaction=%d stopping=%d status=%s", stopCalls, transaction, stopping, status)
	}
	s.startTransactionFunc = func(connectorID int, idTag string, meterStart int) (*core.StartTransactionConfirmation, error) {
		if connectorID != 1 || idTag != "next-tag" || meterStart != 100000 {
			return nil, errTestStart
		}
		return core.NewStartTransactionConfirmation(&types.IdTagInfo{Status: types.AuthorizationStatusAccepted}, 1786889262), nil
	}
	if err := s.startTransaction("next-tag", true); err != nil {
		t.Fatalf("new transaction after remote completion: %v", err)
	}
	if !s.hasTransaction() {
		t.Fatal("new transaction was not admitted after remote completion")
	}
}

func TestAcceptedRemoteStopDoesNotRemainFinishingWhenFinishingNotificationFails(t *testing.T) {
	s := newRemoteStopTestSimulator()
	var mu sync.Mutex
	statuses := make([]core.ChargePointStatus, 0, 2)
	s.stopTransactionFunc = func(_ int, _ int, _ string, _ core.Reason) (*core.StopTransactionConfirmation, error) {
		return core.NewStopTransactionConfirmation(), nil
	}
	s.statusFunc = func(status core.ChargePointStatus, _ core.ChargePointErrorCode) error {
		mu.Lock()
		statuses = append(statuses, status)
		mu.Unlock()
		if status == core.ChargePointStatusFinishing {
			return errTestStatus
		}
		return nil
	}

	confirmation, err := s.OnRemoteStopTransaction(&core.RemoteStopTransactionRequest{TransactionId: 1786889261})
	if err != nil || confirmation.Status != types.RemoteStartStopStatusAccepted {
		t.Fatalf("RemoteStopTransaction = %#v, %v", confirmation, err)
	}
	time.Sleep(50 * time.Millisecond)
	s.mu.RLock()
	status, transaction := s.status, s.transaction
	s.mu.RUnlock()
	if status != core.ChargePointStatusAvailable || transaction != 0 {
		t.Fatalf("status=%s transaction=%d, want Available and cleared", status, transaction)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(statuses) != 2 || statuses[0] != core.ChargePointStatusFinishing || statuses[1] != core.ChargePointStatusAvailable {
		t.Fatalf("statuses=%v, want Finishing then Available", statuses)
	}
}

var (
	errTestStatus = &testError{"status failed"}
	errTestStop   = &testError{"unexpected stop request"}
	errTestStart  = &testError{"unexpected start request"}
)

type testError struct{ message string }

func (e *testError) Error() string { return e.message }

func newRemoteStopTestSimulator() *simulator {
	s := newSimulator("test-cp", "model", "vendor", 1, 100000, 230, 35)
	s.transaction = 1786889261
	s.idTag = "test-tag"
	s.status = core.ChargePointStatusCharging
	return s
}
