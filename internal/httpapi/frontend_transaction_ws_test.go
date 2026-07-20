package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/config"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/state"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/store"
)

func TestFrontendTransactionWebSocketStreamsPersistedLifecycle(t *testing.T) {
	baseStore := store.NewMemoryStore()
	updates := store.NewTransactionUpdates()
	txStore := store.WithTransactionUpdates(baseStore, updates)

	tx, err := txStore.CreateTransaction(context.Background(), store.CreateTransactionInput{
		ChargerID:   "CP-WS-1",
		ConnectorID: 1,
		MeterStart:  1000,
		IDTag:       "USER-WS-1",
	})
	if err != nil {
		t.Fatalf("create transaction: %v", err)
	}

	api := NewServer(
		config.Config{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		state.NewRegistry(),
		nil,
		txStore,
		updates,
	)
	server := httptest.NewServer(api.Routes())
	defer server.Close()

	values := url.Values{}
	values.Set("transaction_id", strconvFormatInt(tx.TransactionID))
	values.Set("id_tag", tx.IDTag)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/frontend/ws/transaction?" + values.Encode()

	conn, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if response != nil {
			defer response.Body.Close()
		}
		t.Fatalf("dial transaction websocket: %v", err)
	}
	defer conn.Close()

	snapshot := readTransactionSnapshot(t, conn)
	if snapshot.Status != "RUNNING" {
		t.Fatalf("initial status = %q, want RUNNING", snapshot.Status)
	}
	if snapshot.Transaction.TransactionID != strconvFormatInt(tx.TransactionID) {
		t.Fatalf("initial transaction ID = %q, want %d", snapshot.Transaction.TransactionID, tx.TransactionID)
	}

	if _, err := txStore.UpdateLiveMeter(context.Background(), store.UpdateLiveMeterInput{
		ChargerID:     tx.ChargerID,
		TransactionID: tx.TransactionID,
		MeterStop:     1600,
	}); err != nil {
		t.Fatalf("update live meter: %v", err)
	}

	snapshot = readTransactionSnapshot(t, conn)
	if snapshot.Status != "RUNNING" {
		t.Fatalf("meter update status = %q, want RUNNING", snapshot.Status)
	}
	if snapshot.Transaction.MeterStop == nil || *snapshot.Transaction.MeterStop != 1600 {
		t.Fatalf("meter stop = %v, want 1600", snapshot.Transaction.MeterStop)
	}
	if snapshot.Transaction.TotalConsumption == nil || *snapshot.Transaction.TotalConsumption != 0.6 {
		t.Fatalf("total consumption = %v, want 0.6", snapshot.Transaction.TotalConsumption)
	}

	if _, err := txStore.StopTransaction(context.Background(), store.StopTransactionInput{
		ChargerID:     tx.ChargerID,
		TransactionID: tx.TransactionID,
		MeterStop:     1750,
	}); err != nil {
		t.Fatalf("stop transaction: %v", err)
	}

	snapshot = readTransactionSnapshot(t, conn)
	if snapshot.Status != "COMPLETED" {
		t.Fatalf("completed status = %q, want COMPLETED", snapshot.Status)
	}
	if snapshot.Transaction.StopTime == nil {
		t.Fatal("completed snapshot has nil stop time")
	}
	if snapshot.Transaction.TotalConsumption == nil || *snapshot.Transaction.TotalConsumption != 0.75 {
		t.Fatalf("completed consumption = %v, want 0.75", snapshot.Transaction.TotalConsumption)
	}
}

func TestFrontendTransactionWebSocketRejectsMismatchedIDTag(t *testing.T) {
	baseStore := store.NewMemoryStore()
	updates := store.NewTransactionUpdates()
	txStore := store.WithTransactionUpdates(baseStore, updates)

	tx, err := txStore.CreateTransaction(context.Background(), store.CreateTransactionInput{
		ChargerID:   "CP-WS-2",
		ConnectorID: 1,
		MeterStart:  0,
		IDTag:       "OWNER",
	})
	if err != nil {
		t.Fatalf("create transaction: %v", err)
	}

	api := NewServer(
		config.Config{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		state.NewRegistry(),
		nil,
		txStore,
		updates,
	)
	server := httptest.NewServer(api.Routes())
	defer server.Close()

	values := url.Values{}
	values.Set("transaction_id", strconvFormatInt(tx.TransactionID))
	values.Set("id_tag", "NOT-THE-OWNER")
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/frontend/ws/transaction?" + values.Encode()

	conn, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("mismatched idTag unexpectedly upgraded")
	}
	if response == nil {
		t.Fatal("mismatched idTag returned no HTTP response")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("mismatched idTag status = %d, want 404", response.StatusCode)
	}
}

func TestParseFrontendTransactionID(t *testing.T) {
	for _, valid := range []string{"1", "2147483647"} {
		if _, err := parseFrontendTransactionID(valid); err != nil {
			t.Fatalf("valid transaction ID %q rejected: %v", valid, err)
		}
	}

	for _, invalid := range []string{"", "0", "-1", "+1", "01", "1.5", "2147483648"} {
		if _, err := parseFrontendTransactionID(invalid); err == nil {
			t.Fatalf("invalid transaction ID %q accepted", invalid)
		}
	}
}

func readTransactionSnapshot(t *testing.T, conn *websocket.Conn) frontendTransactionSnapshot {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set websocket read deadline: %v", err)
	}

	var snapshot frontendTransactionSnapshot
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatalf("read transaction snapshot: %v", err)
	}
	return snapshot
}

func strconvFormatInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
