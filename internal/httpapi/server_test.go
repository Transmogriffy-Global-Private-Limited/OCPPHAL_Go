package httpapi

import (
	"encoding/json"
	"testing"
)

func TestRemoteStartRequestAcceptsCompatibleConnectorIDs(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		connectorID int
	}{
		{
			name:        "snake case number",
			payload:     `{"uid":"charger-1","id_tag":"user-1","connector_id":1}`,
			connectorID: 1,
		},
		{
			name:        "snake case numeric string",
			payload:     `{"uid":"charger-1","id_tag":"user-1","connector_id":"2"}`,
			connectorID: 2,
		},
		{
			name:        "camel case numeric string",
			payload:     `{"uid":"charger-1","idTag":"user-1","connectorId":"3","isSingleSession":"true"}`,
			connectorID: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var request remoteStartRequest
			if err := json.Unmarshal([]byte(tt.payload), &request); err != nil {
				t.Fatalf("decode remote-start request: %v", err)
			}

			if got := request.connectorID(); got != tt.connectorID {
				t.Fatalf("connector ID = %d, want %d", got, tt.connectorID)
			}
			if tt.name == "camel case numeric string" && !request.isSingleSession() {
				t.Fatal("quoted single-session boolean was not accepted")
			}
		})
	}
}

func TestCMSRequestIntegersAcceptNumericStrings(t *testing.T) {
	t.Run("remote stop transaction", func(t *testing.T) {
		var request remoteStopRequest
		decodeTestRequest(t, `{"transaction_id":"101"}`, &request)
		if got := request.transactionID(); got != 101 {
			t.Fatalf("transaction ID = %d, want 101", got)
		}
	})

	t.Run("change availability connector", func(t *testing.T) {
		var request changeAvailabilityRequest
		decodeTestRequest(t, `{"connector_id":"2"}`, &request)
		if got := request.connectorID(); got != 2 {
			t.Fatalf("connector ID = %d, want 2", got)
		}
	})

	t.Run("unlock connector", func(t *testing.T) {
		var request unlockConnectorRequest
		decodeTestRequest(t, `{"connectorId":"3"}`, &request)
		if got := request.connectorID(); got != 3 {
			t.Fatalf("connector ID = %d, want 3", got)
		}
	})

	t.Run("diagnostics retry values", func(t *testing.T) {
		var request getDiagnosticsRequest
		decodeTestRequest(t, `{"retries":"4","retry_interval":"30"}`, &request)
		if request.Retries == nil || int(*request.Retries) != 4 {
			t.Fatalf("retries = %v, want 4", request.Retries)
		}
		if request.RetryInterval == nil || int(*request.RetryInterval) != 30 {
			t.Fatalf("retry interval = %v, want 30", request.RetryInterval)
		}
	})

	t.Run("firmware retry values", func(t *testing.T) {
		var request updateFirmwareRequest
		decodeTestRequest(t, `{"retries":"5","retry_interval":"60"}`, &request)
		if request.Retries == nil || int(*request.Retries) != 5 {
			t.Fatalf("retries = %v, want 5", request.Retries)
		}
		if request.RetryInterval == nil || int(*request.RetryInterval) != 60 {
			t.Fatalf("retry interval = %v, want 60", request.RetryInterval)
		}
	})

	t.Run("trigger message connector", func(t *testing.T) {
		var request triggerMessageRequest
		decodeTestRequest(t, `{"connector_id":"6"}`, &request)
		if got := request.connectorID(); got != 6 {
			t.Fatalf("connector ID = %d, want 6", got)
		}
	})
}

func TestRemoteStartRequestRejectsNonNumericConnectorID(t *testing.T) {
	var request remoteStartRequest
	err := json.Unmarshal(
		[]byte(`{"uid":"charger-1","id_tag":"user-1","connector_id":"first"}`),
		&request,
	)
	if err == nil {
		t.Fatal("expected non-numeric connector ID to be rejected")
	}
}

func decodeTestRequest(t *testing.T, payload string, dst any) {
	t.Helper()
	if err := json.Unmarshal([]byte(payload), dst); err != nil {
		t.Fatalf("decode request: %v", err)
	}
}
