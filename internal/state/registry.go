package state

import (
	"strconv"
	"sync"
	"time"
)

type ConnectorState struct {
	Status                        string   `json:"status"`
	LastMeterValue                *float64 `json:"last_meter_value,omitempty"`
	LastTransactionConsumptionKWh float64  `json:"last_transaction_consumption_kwh"`
	ErrorCode                     string   `json:"error_code"`
	TransactionID                 *int64   `json:"transaction_id,omitempty"`
	LastMeterReceived             *string  `json:"last_meter_received,omitempty"`
}

type ChargerState struct {
	ID              string                    `json:"-"`
	Online          bool                      `json:"-"`
	HasError        bool                      `json:"-"`
	Status          string                    `json:"status"`
	Connectors      map[string]ConnectorState `json:"connectors"`
	LastMessageTime time.Time                 `json:"-"`
}

type Registry struct {
	mu       sync.RWMutex
	chargers map[string]*ChargerState
}

func NewRegistry() *Registry {
	return &Registry{chargers: make(map[string]*ChargerState)}
}

func (r *Registry) Touch(chargerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cp := r.ensureLocked(chargerID)
	cp.Online = true
	cp.LastMessageTime = time.Now().UTC()
}

func (r *Registry) MarkOffline(chargerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cp := r.chargers[chargerID]; cp != nil {
		cp.Online = false
		cp.LastMessageTime = time.Now().UTC()
	}
}

func (r *Registry) ApplyStatusNotification(chargerID string, connectorID int, status string, errorCode string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cp := r.ensureLocked(chargerID)
	cp.Online = true
	cp.LastMessageTime = time.Now().UTC()

	key := connectorKey(connectorID)
	conn := cp.Connectors[key]

	if conn.Status == "" {
		conn.Status = "Unknown"
	}
	if conn.ErrorCode == "" {
		conn.ErrorCode = "NoError"
	}

	if status != "" {
		conn.Status = status
	}
	if errorCode != "" {
		conn.ErrorCode = errorCode
	}

	connectorHasError := conn.ErrorCode != "" && conn.ErrorCode != "NoError"
	cp.HasError = connectorHasError
	cp.Connectors[key] = conn
}

func (r *Registry) ApplyStartTransaction(chargerID string, connectorID int, transactionID int64, meterStart float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cp := r.ensureLocked(chargerID)
	cp.Online = true
	cp.Status = "Active"
	cp.LastMessageTime = time.Now().UTC()

	key := connectorKey(connectorID)
	conn := cp.Connectors[key]
	if conn.ErrorCode == "" {
		conn.ErrorCode = "NoError"
	}

	conn.Status = "Charging"
	meterCopy := meterStart
	txCopy := transactionID
	conn.LastMeterValue = &meterCopy
	conn.TransactionID = &txCopy
	now := time.Now().UTC().Format(time.RFC3339Nano)
	conn.LastMeterReceived = &now

	cp.Connectors[key] = conn
}

func (r *Registry) ApplyMeterValue(chargerID string, connectorID int, transactionID *int64, meterValueWh float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cp := r.ensureLocked(chargerID)
	cp.Online = true
	cp.LastMessageTime = time.Now().UTC()

	key := connectorKey(connectorID)
	conn := cp.Connectors[key]
	if conn.Status == "" {
		conn.Status = "Unknown"
	}
	if conn.ErrorCode == "" {
		conn.ErrorCode = "NoError"
	}

	if conn.LastMeterValue != nil {
		conn.LastTransactionConsumptionKWh = deltaWh(*conn.LastMeterValue, meterValueWh) / 1000.0
	}

	meterCopy := meterValueWh
	conn.LastMeterValue = &meterCopy

	if transactionID != nil {
		txCopy := *transactionID
		conn.TransactionID = &txCopy
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	conn.LastMeterReceived = &now

	cp.Connectors[key] = conn
}

func (r *Registry) ApplyStopTransaction(chargerID string, connectorID int, meterStop float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cp := r.ensureLocked(chargerID)
	cp.Online = true
	cp.Status = "Inactive"
	cp.LastMessageTime = time.Now().UTC()

	key := connectorKey(connectorID)
	conn := cp.Connectors[key]
	if conn.ErrorCode == "" {
		conn.ErrorCode = "NoError"
	}

	if conn.LastMeterValue != nil {
		conn.LastTransactionConsumptionKWh = deltaWh(*conn.LastMeterValue, meterStop) / 1000.0
	}

	conn.Status = "Available"
	meterCopy := meterStop
	conn.LastMeterValue = &meterCopy
	conn.TransactionID = nil
	now := time.Now().UTC().Format(time.RFC3339Nano)
	conn.LastMeterReceived = &now

	cp.Connectors[key] = conn
}

func (r *Registry) FindConnectorByTransactionID(chargerID string, transactionID int64) (int, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cp := r.chargers[chargerID]
	if cp == nil {
		return 0, false
	}

	for connectorID, conn := range cp.Connectors {
		if conn.TransactionID != nil && *conn.TransactionID == transactionID {
			id, err := strconv.Atoi(connectorID)
			if err == nil {
				return id, true
			}
		}
	}

	return 0, false
}

func (r *Registry) Snapshot(chargerID string) (*ChargerState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cp := r.chargers[chargerID]
	if cp == nil {
		return nil, false
	}
	return clone(cp), true
}

func (r *Registry) SnapshotAll() map[string]*ChargerState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]*ChargerState, len(r.chargers))
	for id, cp := range r.chargers {
		out[id] = clone(cp)
	}
	return out
}

func (r *Registry) ensureLocked(chargerID string) *ChargerState {
	cp := r.chargers[chargerID]
	if cp == nil {
		cp = &ChargerState{
			ID:              chargerID,
			Online:          true,
			Status:          "Inactive",
			Connectors:      make(map[string]ConnectorState),
			LastMessageTime: time.Now().UTC(),
		}
		r.chargers[chargerID] = cp
	}
	return cp
}

func clone(cp *ChargerState) *ChargerState {
	copyCP := *cp
	copyCP.Connectors = make(map[string]ConnectorState, len(cp.Connectors))
	for k, v := range cp.Connectors {
		copyCP.Connectors[k] = v
	}
	return &copyCP
}

func connectorKey(connectorID int) string {
	return strconv.Itoa(connectorID)
}

func deltaWh(previous float64, current float64) float64 {
	const rollover = 4294967295.0

	delta := current - previous
	if delta >= 0 {
		return delta
	}

	return current + (rollover - previous) + 1
}
