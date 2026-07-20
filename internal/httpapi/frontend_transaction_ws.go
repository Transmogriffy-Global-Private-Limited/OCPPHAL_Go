package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/store"
)

var positiveDecimalTransactionID = regexp.MustCompile(`^[1-9][0-9]*$`)

type frontendTransactionRow struct {
	ID                 int64      `json:"id"`
	UUIDDB             string     `json:"uuiddb"`
	ChargerID          string     `json:"charger_id"`
	ConnectorID        int        `json:"connector_id"`
	MeterStart         float64    `json:"meter_start"`
	MeterStop          *float64   `json:"meter_stop"`
	TotalConsumption   *float64   `json:"total_consumption"`
	StartTime          time.Time  `json:"start_time"`
	StopTime           *time.Time `json:"stop_time"`
	IDTag              string     `json:"id_tag"`
	TransactionID      string     `json:"transaction_id"`
	IsSingleSession    bool       `json:"is_single_session"`
	MaxKWh             *float64   `json:"max_kwh"`
	LimitStopRequested bool       `json:"limit_stop_requested"`
}

type frontendTransactionSnapshot struct {
	Event       string                 `json:"event"`
	Status      string                 `json:"status"`
	Transaction frontendTransactionRow `json:"transaction"`
	ObservedAt  time.Time              `json:"observed_at"`
}

func (s *Server) frontendTransactionWebSocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	rawTransactionID := firstQueryValue(r, "transaction_id", "transactionId")
	idTag := firstQueryValue(r, "id_tag", "idTag")

	transactionID, err := parseFrontendTransactionID(rawTransactionID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"detail": "transaction_id must be a positive base-10 integer within the OCPP signed 32-bit range",
		})
		return
	}
	if idTag == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing id_tag"})
		return
	}
	if s.txStore == nil || s.txUpdates == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "Transaction stream unavailable"})
		return
	}

	lookupCtx, cancelLookup := context.WithTimeout(r.Context(), 5*time.Second)
	_, err = s.txStore.GetByTransactionIDAndIDTag(lookupCtx, transactionID, idTag)
	cancelLookup()
	if err != nil {
		s.writeTransactionLookupError(w, transactionID, err)
		return
	}

	updates, unsubscribe := s.txUpdates.Subscribe(transactionID)
	defer unsubscribe()

	conn, err := frontendWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("frontend transaction websocket upgrade failed", "transaction_id", transactionID, "error", err)
		return
	}
	defer conn.Close()

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	s.logger.Info("frontend transaction websocket connected", "transaction_id", transactionID)

	resyncTicker := time.NewTicker(30 * time.Second)
	defer resyncTicker.Stop()

	sendSnapshot := func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		tx, err := s.txStore.GetByTransactionIDAndIDTag(ctx, transactionID, idTag)
		if err != nil {
			s.logger.Warn("frontend transaction snapshot lookup failed", "transaction_id", transactionID, "error", err)
			return false
		}

		if err := conn.WriteJSON(newFrontendTransactionSnapshot(tx)); err != nil {
			s.logger.Info("frontend transaction websocket disconnected", "transaction_id", transactionID, "error", err)
			return false
		}
		return true
	}

	// Re-read after subscribing so an update between validation and upgrade
	// cannot be missed.
	if !sendSnapshot() {
		return
	}

	for {
		select {
		case <-closed:
			s.logger.Info("frontend transaction websocket disconnected", "transaction_id", transactionID)
			return
		case <-r.Context().Done():
			return
		case <-updates:
			if !sendSnapshot() {
				return
			}
		case <-resyncTicker.C:
			// Event notifications provide immediate updates. A slow periodic
			// re-read also covers external database repair and keeps the
			// frontend tunnel active without imposing a connection timeout.
			if !sendSnapshot() {
				return
			}
		}
	}
}

func firstQueryValue(r *http.Request, names ...string) string {
	for _, name := range names {
		if value := r.URL.Query().Get(name); value != "" {
			return value
		}
	}
	return ""
}

func parseFrontendTransactionID(raw string) (int64, error) {
	if !positiveDecimalTransactionID.MatchString(raw) {
		return 0, errors.New("invalid transaction ID")
	}

	transactionID, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || transactionID <= 0 {
		return 0, errors.New("invalid transaction ID")
	}
	return transactionID, nil
}

func (s *Server) writeTransactionLookupError(w http.ResponseWriter, transactionID int64, err error) {
	if errors.Is(err, store.ErrTransactionNotFound) {
		// Deliberately do not disclose whether the transaction ID exists with a
		// different idTag.
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "Transaction not found"})
		return
	}

	s.logger.Error("frontend transaction lookup failed", "transaction_id", transactionID, "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "Transaction lookup failed"})
}

func newFrontendTransactionSnapshot(tx *store.Transaction) frontendTransactionSnapshot {
	status := "RUNNING"
	if tx.StopTime != nil {
		status = "COMPLETED"
	}

	return frontendTransactionSnapshot{
		Event:  "transaction_snapshot",
		Status: status,
		Transaction: frontendTransactionRow{
			ID:                 tx.ID,
			UUIDDB:             tx.UUIDDB,
			ChargerID:          tx.ChargerID,
			ConnectorID:        tx.ConnectorID,
			MeterStart:         tx.MeterStart,
			MeterStop:          tx.MeterStop,
			TotalConsumption:   tx.TotalConsumption,
			StartTime:          tx.StartTime,
			StopTime:           tx.StopTime,
			IDTag:              tx.IDTag,
			TransactionID:      strconv.FormatInt(tx.TransactionID, 10),
			IsSingleSession:    tx.IsSingleSession,
			MaxKWh:             tx.MaxKWh,
			LimitStopRequested: tx.LimitStopRequested,
		},
		ObservedAt: time.Now().UTC(),
	}
}
