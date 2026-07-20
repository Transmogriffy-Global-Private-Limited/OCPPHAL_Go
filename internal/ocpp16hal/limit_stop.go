package ocpp16hal

import (
	"context"
	"strings"
	"time"
)

const (
	limitStopMaxAttempts      = 6
	limitStopInitialRetry     = 2 * time.Second
	limitStopMaxRetry         = 30 * time.Second
	limitStopConfirmationWait = 30 * time.Second
	limitStopPollInterval     = 2 * time.Second
)

func (h *HAL) CheckAndRequestLimitStop(ctx context.Context, chargerID string, transactionID int64) {
	shouldStop, err := h.store.CheckAndMarkLimitStop(ctx, chargerID, transactionID)
	if err != nil {
		h.logger.Warn(
			"failed to check max_kwh limit",
			"charge_point_id", chargerID,
			"transaction_id", transactionID,
			"error", err,
		)
		return
	}

	if !shouldStop {
		return
	}

	h.logger.Info(
		"max_kwh limit crossed; sending RemoteStopTransaction",
		"charge_point_id", chargerID,
		"transaction_id", transactionID,
	)

	go h.requestLimitStopUntilClosed(chargerID, transactionID)
}

func (h *HAL) checkAndRemoteStopIfLimitExceeded(chargerID string, transactionID int64) {
	h.CheckAndRequestLimitStop(context.Background(), chargerID, transactionID)
}

func (h *HAL) requestLimitStopUntilClosed(chargerID string, transactionID int64) {
	delay := limitStopInitialRetry

	for attempt := 1; attempt <= limitStopMaxAttempts; attempt++ {
		closed, err := h.transactionClosed(chargerID, transactionID)
		if err == nil && closed {
			return
		}
		if err != nil {
			h.logger.Warn(
				"failed to verify transaction before limit stop",
				"charge_point_id", chargerID,
				"transaction_id", transactionID,
				"attempt", attempt,
				"error", err,
			)
		}

		callCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		status, stopErr := h.RemoteStopTransaction(callCtx, chargerID, int(transactionID))
		cancel()

		accepted := stopErr == nil && strings.EqualFold(strings.TrimSpace(status), "Accepted")
		if accepted {
			h.logger.Info(
				"limit RemoteStopTransaction accepted",
				"charge_point_id", chargerID,
				"transaction_id", transactionID,
				"attempt", attempt,
			)

			if h.waitForTransactionClosed(chargerID, transactionID, limitStopConfirmationWait) {
				return
			}

			h.logger.Warn(
				"charger did not report StopTransaction after accepted limit stop",
				"charge_point_id", chargerID,
				"transaction_id", transactionID,
				"attempt", attempt,
			)
		} else {
			h.logger.Warn(
				"limit RemoteStopTransaction was not accepted",
				"charge_point_id", chargerID,
				"transaction_id", transactionID,
				"attempt", attempt,
				"status", status,
				"error", stopErr,
			)
		}

		if attempt < limitStopMaxAttempts {
			time.Sleep(delay)
			delay *= 2
			if delay > limitStopMaxRetry {
				delay = limitStopMaxRetry
			}
		}
	}

	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.store.ReleaseLimitStopRequest(releaseCtx, chargerID, transactionID); err != nil {
		h.logger.Warn(
			"failed to release exhausted limit-stop claim",
			"charge_point_id", chargerID,
			"transaction_id", transactionID,
			"error", err,
		)
		return
	}

	h.logger.Error(
		"limit stop exhausted; released for a future meter-value retry",
		"charge_point_id", chargerID,
		"transaction_id", transactionID,
	)
}

func (h *HAL) waitForTransactionClosed(chargerID string, transactionID int64, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		closed, err := h.transactionClosed(chargerID, transactionID)
		if err == nil && closed {
			return true
		}
		time.Sleep(limitStopPollInterval)
	}
	return false
}

func (h *HAL) transactionClosed(chargerID string, transactionID int64) (bool, error) {
	tx, err := h.store.GetByTransactionID(context.Background(), chargerID, transactionID)
	if err != nil {
		return false, err
	}
	return tx.StopTime != nil, nil
}
