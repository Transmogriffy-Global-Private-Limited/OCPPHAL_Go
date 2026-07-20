package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/config"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/store"
)

const (
	KindStartTransaction     = "start_transaction"
	KindCompletedTransaction = "completed_transaction"
)

type LimitStopper interface {
	CheckAndRequestLimitStop(ctx context.Context, chargerID string, transactionID int64)
}

type Manager struct {
	cfg    config.Config
	store  store.TransactionStore
	logger *slog.Logger
	client *http.Client

	limitStopper LimitStopper

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewManager(cfg config.Config, txStore store.TransactionStore, logger *slog.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		cfg:    cfg,
		store:  txStore,
		logger: logger,
		client: &http.Client{Timeout: 10 * time.Second},
		ctx:    ctx,
		cancel: cancel,
	}
}

func (m *Manager) SetLimitStopper(limitStopper LimitStopper) {
	m.limitStopper = limitStopper
}

func (m *Manager) Start() {
	m.wg.Add(1)
	go m.loop()
}

func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()
}

func (m *Manager) EnqueueStartTransaction(ctx context.Context, tx *store.Transaction) error {
	targetURL := m.cfg.MainCMSStartTxnHookURL
	if tx.IsSingleSession && m.cfg.SingleSessionStartTxnHookURL != "" {
		targetURL = m.cfg.SingleSessionStartTxnHookURL
	}

	if targetURL == "" {
		return errors.New("missing start transaction hook URL")
	}

	payload := map[string]any{
		"transactionid":     strconv.FormatInt(tx.TransactionID, 10),
		"userid":            tx.IDTag,
		"chargerid":         tx.ChargerID,
		"connectorid":       strconv.Itoa(tx.ConnectorID),
		"is_single_session": tx.IsSingleSession,
	}

	txID := tx.TransactionID

	return m.store.EnqueueCallback(ctx, store.EnqueueCallbackInput{
		Kind:          KindStartTransaction,
		DedupeKey:     fmt.Sprintf("%s:%d", KindStartTransaction, tx.TransactionID),
		TransactionID: &txID,
		UUIDDB:        tx.UUIDDB,
		TargetURL:     targetURL,
		Payload:       payload,
		MaxRetries:    6,
	})
}

func (m *Manager) EnqueueCompletedTransaction(ctx context.Context, tx *store.Transaction) error {
	targetURL := m.cfg.MainCMSCompletedTxnURL
	if tx.IsSingleSession && m.cfg.SingleSessionCompletedTxnURL != "" {
		targetURL = m.cfg.SingleSessionCompletedTxnURL
	}

	if targetURL == "" {
		return errors.New("missing completed transaction hook URL")
	}
	if tx.StopTime == nil {
		return errors.New("missing stop_time")
	}
	if tx.MeterStop == nil {
		return errors.New("missing meter_stop")
	}
	if tx.TotalConsumption == nil || *tx.TotalConsumption < 0 {
		return fmt.Errorf("invalid total_consumption: %v", tx.TotalConsumption)
	}

	payload := map[string]any{
		"sessionid":   strconv.FormatInt(tx.TransactionID, 10),
		"chargerid":   tx.ChargerID,
		"starttime":   tx.StartTime.Format(time.RFC3339Nano),
		"stoptime":    tx.StopTime.Format(time.RFC3339Nano),
		"userid":      tx.IDTag,
		"meterstart":  strconv.FormatFloat(tx.MeterStart, 'f', -1, 64),
		"meterstop":   strconv.FormatFloat(*tx.MeterStop, 'f', -1, 64),
		"consumedkwh": strconv.FormatFloat(*tx.TotalConsumption, 'f', -1, 64),
	}

	txID := tx.TransactionID

	return m.store.EnqueueCallback(ctx, store.EnqueueCallbackInput{
		Kind:          KindCompletedTransaction,
		DedupeKey:     fmt.Sprintf("%s:%d", KindCompletedTransaction, tx.TransactionID),
		TransactionID: &txID,
		UUIDDB:        tx.UUIDDB,
		TargetURL:     targetURL,
		Payload:       payload,
		MaxRetries:    10,
	})
}

func (m *Manager) loop() {
	defer m.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	m.logger.Info("callback outbox worker started")

	for {
		m.processOnce(m.ctx)

		select {
		case <-m.ctx.Done():
			m.logger.Info("callback outbox worker stopped")
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) processOnce(ctx context.Context) {
	m.reconcileMissingCallbacks(ctx)

	tasks, err := m.store.ClaimDueCallbacks(ctx, 10)
	if err != nil {
		m.logger.Warn("failed to claim callback outbox tasks", "error", err)
		return
	}

	for _, task := range tasks {
		m.processTask(ctx, task)
	}
}

func (m *Manager) reconcileMissingCallbacks(ctx context.Context) {
	startTransactions, err := m.store.ListTransactionsMissingStartCallbacks(ctx, 25)
	if err != nil {
		m.logger.Warn("failed to reconcile missing start callbacks", "error", err)
	} else {
		for _, tx := range startTransactions {
			if err := m.EnqueueStartTransaction(ctx, tx); err != nil {
				m.logger.Warn(
					"failed to recover missing start callback",
					"transaction_id", tx.TransactionID,
					"error", err,
				)
			}
		}
	}

	completedTransactions, err := m.store.ListTransactionsMissingCompletedCallbacks(ctx, 25)
	if err != nil {
		m.logger.Warn("failed to reconcile missing completed callbacks", "error", err)
		return
	}

	for _, tx := range completedTransactions {
		if err := m.EnqueueCompletedTransaction(ctx, tx); err != nil {
			m.logger.Warn(
				"failed to recover missing completed callback",
				"transaction_id", tx.TransactionID,
				"error", err,
			)
		}
	}
}

func (m *Manager) processTask(ctx context.Context, task store.CallbackTask) {
	result, err := m.postTask(ctx, task)
	if err == nil && result.fatal == "" {
		if markErr := m.store.MarkCallbackSent(ctx, task.ID); markErr != nil {
			m.logger.Warn("failed to mark callback sent", "task_id", task.ID, "error", markErr)
		}
		return
	}

	if result.fatal != "" {
		if markErr := m.store.MarkCallbackFatal(ctx, task.ID, result.fatal); markErr != nil {
			m.logger.Warn("failed to mark callback fatal", "task_id", task.ID, "error", markErr)
		}
		return
	}

	retries := task.Retries + 1
	if retries >= task.MaxRetries {
		_ = m.store.MarkCallbackFatal(ctx, task.ID, err.Error())
		return
	}

	backoff := retryBase(task.Kind) * time.Duration(1<<(retries-1))
	nextRetry := time.Now().UTC().Add(backoff)

	if markErr := m.store.MarkCallbackRetry(ctx, task.ID, retries, nextRetry, err.Error()); markErr != nil {
		m.logger.Warn("failed to mark callback retry", "task_id", task.ID, "error", markErr)
	}
}

type postResult struct {
	fatal string
}

func (m *Manager) postTask(ctx context.Context, task store.CallbackTask) (postResult, error) {
	payload := task.Payload

	if task.Kind == KindStartTransaction {
		raw, err := normalizeStartCallbackPayload(task.Payload)
		if err != nil {
			return postResult{fatal: err.Error()}, nil
		}
		payload = raw
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, task.TargetURL, bytes.NewReader(payload))
	if err != nil {
		return postResult{fatal: err.Error()}, nil
	}

	req.Header.Set("Content-Type", "application/json")
	if m.cfg.APIAuthKey != "" {
		req.Header.Set("apiauthkey", m.cfg.APIAuthKey)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return postResult{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return postResult{}, fmt.Errorf("read callback response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return postResult{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if task.Kind == KindStartTransaction {
		maxKWh, err := parseMaxKWhResponse(respBody)
		if err != nil {
			return postResult{}, err
		}

		if task.TransactionID != nil {
			if err := m.store.UpdateTransactionMaxKWh(ctx, *task.TransactionID, maxKWh); err != nil {
				return postResult{}, err
			}

			chargerID := extractChargerID(task.Payload)
			if chargerID != "" && m.limitStopper != nil {
				m.limitStopper.CheckAndRequestLimitStop(context.Background(), chargerID, *task.TransactionID)
			}
		}
	}

	return postResult{}, nil
}

func normalizeStartCallbackPayload(payload json.RawMessage) ([]byte, error) {
	var body map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		return nil, err
	}

	if txID, ok := body["transactionid"]; ok {
		body["transactionid"] = fmt.Sprint(txID)
	}

	return json.Marshal(body)
}

type flexibleFloat64 float64

func (f *flexibleFloat64) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if strings.HasPrefix(raw, `"`) {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		raw = strings.TrimSpace(value)
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fmt.Errorf("invalid decimal %q: %w", raw, err)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("invalid non-finite decimal %q", raw)
	}

	*f = flexibleFloat64(value)
	return nil
}

func parseMaxKWhResponse(respBody []byte) (float64, error) {
	var parsed struct {
		MaxKWh *flexibleFloat64 `json:"max_kwh"`
	}

	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return 0, errors.New("invalid JSON response: " + err.Error())
	}
	if parsed.MaxKWh == nil {
		return 0, errors.New("missing max_kwh in response: " + string(respBody))
	}

	return float64(*parsed.MaxKWh), nil
}

func extractChargerID(payload json.RawMessage) string {
	var body struct {
		ChargerID string `json:"chargerid"`
	}

	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}

	return body.ChargerID
}

func retryBase(kind string) time.Duration {
	if kind == KindStartTransaction {
		return 30 * time.Second
	}
	return 60 * time.Second
}
