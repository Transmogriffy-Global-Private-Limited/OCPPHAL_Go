package ocpp16hal

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/store"
)

var bootRecoveryRunning sync.Map

const (
	bootRecoveryDelay       = 10 * time.Second
	bootRecoveryMaxRuntime  = 5 * time.Minute
	bootRecoveryCallTimeout = 25 * time.Second
)

func (h *HAL) scheduleBootRecovery(chargerID string) {
	if _, loaded := bootRecoveryRunning.LoadOrStore(chargerID, struct{}{}); loaded {
		h.logger.Info("boot recovery already running; skipping duplicate", "charge_point_id", chargerID)
		return
	}

	listCtx, listCancel := context.WithTimeout(context.Background(), bootRecoveryCallTimeout)
	openTxs, err := h.store.ListOpenTransactionsByCharger(listCtx, chargerID)
	listCancel()
	if err != nil {
		bootRecoveryRunning.Delete(chargerID)
		h.logger.Warn("failed to snapshot open transactions for boot recovery", "charge_point_id", chargerID, "error", err)
		return
	}
	if len(openTxs) == 0 {
		bootRecoveryRunning.Delete(chargerID)
		h.logger.Info("boot recovery found no pre-existing open transactions", "charge_point_id", chargerID)
		return
	}

	go func() {
		defer bootRecoveryRunning.Delete(chargerID)

		time.Sleep(bootRecoveryDelay)

		ctx, cancel := context.WithTimeout(context.Background(), bootRecoveryMaxRuntime)
		defer cancel()

		h.recoverOpenTransactions(ctx, chargerID, openTxs)
	}()
}

func (h *HAL) recoverOpenTransactions(ctx context.Context, chargerID string, bootOpenTxs []*store.Transaction) {
	h.logger.Warn(
		"boot recovery found pre-existing open transactions",
		"charge_point_id", chargerID,
		"open_transaction_count", len(bootOpenTxs),
	)

	for _, bootTx := range bootOpenTxs {
		if bootTx == nil {
			continue
		}

		tx, err := h.store.GetByTransactionID(ctx, chargerID, bootTx.TransactionID)
		if err != nil {
			h.logger.Warn(
				"failed to refresh boot recovery transaction",
				"charge_point_id", chargerID,
				"transaction_id", bootTx.TransactionID,
				"error", err,
			)
			continue
		}
		if tx == nil {
			continue
		}
		if tx.StopTime != nil {
			continue
		}

		if h.isGhostAvailableTransaction(tx) {
			h.forceCloseGhostTransaction(ctx, tx)
			continue
		}

		h.hydrateOpenTransaction(tx)
		h.remoteStopAndUnlockWithRetry(ctx, tx)
	}
}

func (h *HAL) isGhostAvailableTransaction(tx *store.Transaction) bool {
	snapshot, ok := h.registry.Snapshot(tx.ChargerID)
	if !ok || snapshot == nil {
		return false
	}

	conn, ok := snapshot.Connectors[connectorIDString(tx.ConnectorID)]
	if !ok {
		return false
	}

	if !strings.EqualFold(strings.TrimSpace(conn.Status), "Available") {
		return false
	}

	return conn.TransactionID == nil || *conn.TransactionID == tx.TransactionID
}

func (h *HAL) forceCloseGhostTransaction(ctx context.Context, tx *store.Transaction) {
	meterStop := tx.MeterStart
	if tx.MeterStop != nil {
		meterStop = *tx.MeterStop
	}

	closed, err := h.store.ForceCloseTransaction(ctx, store.ForceCloseTransactionInput{
		ChargerID:     tx.ChargerID,
		TransactionID: tx.TransactionID,
		MeterStop:     meterStop,
		Reason:        "boot_recovery_ghost_available",
	})
	if err != nil {
		h.logger.Warn(
			"failed to force-close ghost transaction during boot recovery",
			"charge_point_id", tx.ChargerID,
			"transaction_id", tx.TransactionID,
			"error", err,
		)
		return
	}

	h.registry.ApplyStopTransaction(tx.ChargerID, tx.ConnectorID, meterStop)

	if h.hooks != nil {
		if err := h.hooks.EnqueueCompletedTransaction(ctx, closed); err != nil {
			h.logger.Warn(
				"failed to enqueue recovered ghost transaction hook",
				"charge_point_id", tx.ChargerID,
				"transaction_id", tx.TransactionID,
				"error", err,
			)
		}
	}

	h.logger.Warn(
		"force-closed ghost transaction during boot recovery",
		"charge_point_id", tx.ChargerID,
		"connector_id", tx.ConnectorID,
		"transaction_id", tx.TransactionID,
	)
}

func (h *HAL) hydrateOpenTransaction(tx *store.Transaction) {
	h.registry.ApplyStartTransaction(
		tx.ChargerID,
		tx.ConnectorID,
		tx.TransactionID,
		tx.MeterStart,
	)

	if tx.MeterStop != nil {
		txID := tx.TransactionID
		h.registry.ApplyMeterValue(
			tx.ChargerID,
			tx.ConnectorID,
			&txID,
			*tx.MeterStop,
		)
	}
}

func (h *HAL) remoteStopAndUnlockWithRetry(ctx context.Context, tx *store.Transaction) {
	delay := 2 * time.Second

	for {
		if ctx.Err() != nil {
			h.logger.Warn(
				"boot recovery timed out before RemoteStop/Unlock succeeded",
				"charge_point_id", tx.ChargerID,
				"connector_id", tx.ConnectorID,
				"transaction_id", tx.TransactionID,
			)
			return
		}

		callCtx, cancel := context.WithTimeout(ctx, bootRecoveryCallTimeout)
		stopStatus, stopErr := h.RemoteStopTransaction(callCtx, tx.ChargerID, int(tx.TransactionID))
		cancel()

		if stopErr == nil {
			h.logger.Info(
				"boot recovery RemoteStopTransaction completed",
				"charge_point_id", tx.ChargerID,
				"connector_id", tx.ConnectorID,
				"transaction_id", tx.TransactionID,
				"status", stopStatus,
			)

			unlockCtx, unlockCancel := context.WithTimeout(ctx, bootRecoveryCallTimeout)
			unlockStatus, unlockErr := h.UnlockConnector(unlockCtx, tx.ChargerID, tx.ConnectorID)
			unlockCancel()

			if unlockErr == nil {
				h.logger.Info(
					"boot recovery UnlockConnector completed",
					"charge_point_id", tx.ChargerID,
					"connector_id", tx.ConnectorID,
					"transaction_id", tx.TransactionID,
					"status", unlockStatus,
				)
				return
			}

			h.logger.Warn(
				"boot recovery UnlockConnector failed",
				"charge_point_id", tx.ChargerID,
				"connector_id", tx.ConnectorID,
				"transaction_id", tx.TransactionID,
				"error", unlockErr,
			)
		} else {
			h.logger.Warn(
				"boot recovery RemoteStopTransaction failed",
				"charge_point_id", tx.ChargerID,
				"connector_id", tx.ConnectorID,
				"transaction_id", tx.TransactionID,
				"error", stopErr,
			)
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			continue
		case <-timer.C:
		}

		delay *= 2
		if delay > time.Minute {
			delay = time.Minute
		}
	}
}

func connectorIDString(connectorID int) string {
	return strconvItoa(connectorID)
}
