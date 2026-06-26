package ocpp16hal

import (
	"context"
	"time"
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

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()

		status, err := h.RemoteStopTransaction(ctx, chargerID, int(transactionID))
		if err != nil {
			h.logger.Warn(
				"limit RemoteStopTransaction failed",
				"charge_point_id", chargerID,
				"transaction_id", transactionID,
				"error", err,
			)
			return
		}

		h.logger.Info(
			"limit RemoteStopTransaction completed",
			"charge_point_id", chargerID,
			"transaction_id", transactionID,
			"status", status,
		)
	}()
}

func (h *HAL) checkAndRemoteStopIfLimitExceeded(chargerID string, transactionID int64) {
	h.CheckAndRequestLimitStop(context.Background(), chargerID, transactionID)
}
