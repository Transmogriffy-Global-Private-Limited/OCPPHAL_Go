package hooks

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/config"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/store"
)

func TestParseMaxKWhResponseAcceptsCMSFormats(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     float64
	}{
		{
			name:     "JSON number",
			response: `{"max_kwh":7.5}`,
			want:     7.5,
		},
		{
			name:     "quoted CMS decimal",
			response: `{"max_kwh":"7.50"}`,
			want:     7.5,
		},
		{
			name:     "quoted zero",
			response: `{"max_kwh":"0.00"}`,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMaxKWhResponse([]byte(tt.response))
			if err != nil {
				t.Fatalf("parse max_kwh response: %v", err)
			}
			if got != tt.want {
				t.Fatalf("max_kwh = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseMaxKWhResponseRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "missing",
			response: `{"message":"Charging started"}`,
		},
		{
			name:     "null",
			response: `{"max_kwh":null}`,
		},
		{
			name:     "non numeric string",
			response: `{"max_kwh":"not-a-number"}`,
		},
		{
			name:     "non finite string",
			response: `{"max_kwh":"NaN"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseMaxKWhResponse([]byte(tt.response)); err == nil {
				t.Fatal("expected invalid max_kwh response to be rejected")
			}
		})
	}
}

func TestStartCallbackPreservesLargeTransactionIDAsDecimalString(t *testing.T) {
	const transactionID = int64(1037615263)

	memoryStore := store.NewMemoryStore()
	manager := NewManager(
		config.Config{MainCMSStartTxnHookURL: "http://cms/start"},
		memoryStore,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	normalized, err := normalizeStartCallbackPayload(json.RawMessage(`{"transactionid":1037615263}`))
	if err != nil {
		t.Fatalf("normalize legacy numeric callback: %v", err)
	}
	var normalizedBody struct {
		TransactionID string `json:"transactionid"`
	}
	if err := json.Unmarshal(normalized, &normalizedBody); err != nil {
		t.Fatalf("decode normalized callback: %v", err)
	}
	if normalizedBody.TransactionID != "1037615263" {
		t.Fatalf("legacy transactionid = %q, want %q", normalizedBody.TransactionID, "1037615263")
	}

	tx := &store.Transaction{
		TransactionID: transactionID,
		ChargerID:     "CP-1",
		ConnectorID:   1,
		IDTag:         "USER-1",
	}
	if err := manager.EnqueueStartTransaction(t.Context(), tx); err != nil {
		t.Fatalf("enqueue start callback: %v", err)
	}
	tasks, err := memoryStore.ClaimDueCallbacks(t.Context(), 1)
	if err != nil {
		t.Fatalf("claim start callback: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("claimed callback count = %d, want 1", len(tasks))
	}
	var queuedBody struct {
		TransactionID string `json:"transactionid"`
	}
	if err := json.Unmarshal(tasks[0].Payload, &queuedBody); err != nil {
		t.Fatalf("decode queued callback: %v", err)
	}
	if queuedBody.TransactionID != "1037615263" {
		t.Fatalf("queued transactionid = %q, want %q", queuedBody.TransactionID, "1037615263")
	}
}
