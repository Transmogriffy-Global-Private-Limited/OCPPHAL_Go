package ocpp16hal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/state"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/store"
)

const heartbeatIntervalSeconds = 900

type HookSink interface {
	EnqueueStartTransaction(ctx context.Context, tx *store.Transaction) error
	EnqueueCompletedTransaction(ctx context.Context, tx *store.Transaction) error
}

type HAL struct {
	cs          ocpp16.CentralSystem
	registry    *state.Registry
	store       store.TransactionStore
	hooks       HookSink
	connections *connectionTracker
	logger      *slog.Logger
}

func New(registry *state.Registry, txStore store.TransactionStore, hookSink HookSink, logger *slog.Logger) *HAL {
	h := &HAL{
		cs:          ocpp16.NewCentralSystem(nil, nil),
		registry:    registry,
		store:       txStore,
		hooks:       hookSink,
		connections: newConnectionTracker(),
		logger:      logger,
	}

	h.cs.SetNewChargingStationValidationHandler(validateIncomingCharger)

	h.cs.SetNewChargePointHandler(func(chargePoint ocpp16.ChargePointConnection) {
		chargePointID := chargePoint.ID()
		connKey := connectionKey(chargePoint)

		current, previous := h.connections.register(chargePointID, connKey, fmt.Sprint(chargePoint.RemoteAddr()))

		if previous != nil {
			h.logger.Warn(
				"charge point reconnected; superseding previous connection",
				"charge_point_id", chargePointID,
				"previous_generation", previous.Generation,
				"previous_remote_addr", previous.RemoteAddr,
				"current_generation", current.Generation,
				"current_remote_addr", current.RemoteAddr,
			)
		} else {
			h.logger.Info(
				"charge point connected",
				"charge_point_id", chargePointID,
				"remote_addr", chargePoint.RemoteAddr(),
				"connection_generation", current.Generation,
			)
		}

		h.registry.Touch(chargePointID)
	})

	h.cs.SetChargePointDisconnectedHandler(func(chargePoint ocpp16.ChargePointConnection) {
		chargePointID := chargePoint.ID()
		connKey := connectionKey(chargePoint)

		isCurrent, current := h.connections.unregisterIfCurrent(chargePointID, connKey)
		if !isCurrent {
			if current != nil {
				h.logger.Info(
					"ignoring stale charge point disconnect",
					"charge_point_id", chargePointID,
					"remote_addr", chargePoint.RemoteAddr(),
					"current_generation", current.Generation,
					"current_remote_addr", current.RemoteAddr,
				)
			} else {
				h.logger.Info(
					"ignoring unknown charge point disconnect",
					"charge_point_id", chargePointID,
					"remote_addr", chargePoint.RemoteAddr(),
				)
			}
			return
		}

		h.logger.Info(
			"charge point disconnected",
			"charge_point_id", chargePointID,
			"remote_addr", chargePoint.RemoteAddr(),
			"connection_generation", current.Generation,
		)

		h.registry.MarkOffline(chargePointID)
	})

	h.cs.SetCoreHandler(h)
	h.cs.SetFirmwareManagementHandler(h)

	return h
}

func (h *HAL) Start(port int, path string) {
	h.logger.Info("starting ocpp-go central system", "port", port, "path", path)
	h.cs.Start(port, path)
}

func (h *HAL) Stop() {
	h.cs.Stop()
}

func (h *HAL) Errors() <-chan error {
	return h.cs.Errors()
}

func (h *HAL) RemoteStartTransaction(ctx context.Context, chargerID string, idTag string, connectorID int) (string, error) {
	resultCh := make(chan struct {
		status string
		err    error
	}, 1)

	props := []func(*core.RemoteStartTransactionRequest){}
	if connectorID > 0 {
		connectorCopy := connectorID
		props = append(props, func(req *core.RemoteStartTransactionRequest) {
			req.ConnectorId = &connectorCopy
		})
	}

	err := h.cs.RemoteStartTransaction(
		chargerID,
		func(conf *core.RemoteStartTransactionConfirmation, err error) {
			if err != nil {
				resultCh <- struct {
					status string
					err    error
				}{"", err}
				return
			}

			if conf == nil {
				resultCh <- struct {
					status string
					err    error
				}{"", errors.New("nil RemoteStartTransaction confirmation")}
				return
			}

			resultCh <- struct {
				status string
				err    error
			}{string(conf.Status), nil}
		},
		idTag,
		props...,
	)
	if err != nil {
		return "", err
	}

	select {
	case result := <-resultCh:
		return result.status, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (h *HAL) RemoteStopTransaction(ctx context.Context, chargerID string, transactionID int) (string, error) {
	resultCh := make(chan struct {
		status string
		err    error
	}, 1)

	err := h.cs.RemoteStopTransaction(
		chargerID,
		func(conf *core.RemoteStopTransactionConfirmation, err error) {
			if err != nil {
				resultCh <- struct {
					status string
					err    error
				}{"", err}
				return
			}

			if conf == nil {
				resultCh <- struct {
					status string
					err    error
				}{"", errors.New("nil RemoteStopTransaction confirmation")}
				return
			}

			resultCh <- struct {
				status string
				err    error
			}{string(conf.Status), nil}
		},
		transactionID,
	)
	if err != nil {
		return "", err
	}

	select {
	case result := <-resultCh:
		return result.status, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (h *HAL) ChangeAvailability(ctx context.Context, chargerID string, connectorID int, availabilityType string) (string, error) {
	var t core.AvailabilityType
	switch strings.ToLower(strings.TrimSpace(availabilityType)) {
	case "operative":
		t = core.AvailabilityTypeOperative
	case "inoperative":
		t = core.AvailabilityTypeInoperative
	default:
		return "", fmt.Errorf("invalid availability type %q", availabilityType)
	}

	resultCh := make(chan struct {
		status string
		err    error
	}, 1)

	err := h.cs.ChangeAvailability(
		chargerID,
		func(conf *core.ChangeAvailabilityConfirmation, err error) {
			if err != nil {
				resultCh <- struct {
					status string
					err    error
				}{"", err}
				return
			}
			if conf == nil {
				resultCh <- struct {
					status string
					err    error
				}{"", errors.New("nil ChangeAvailability confirmation")}
				return
			}
			resultCh <- struct {
				status string
				err    error
			}{string(conf.Status), nil}
		},
		connectorID,
		t,
	)
	if err != nil {
		return "", err
	}

	select {
	case result := <-resultCh:
		return result.status, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (h *HAL) ChangeConfiguration(ctx context.Context, chargerID string, key string, value string) (string, error) {
	resultCh := make(chan struct {
		status string
		err    error
	}, 1)

	err := h.cs.ChangeConfiguration(
		chargerID,
		func(conf *core.ChangeConfigurationConfirmation, err error) {
			if err != nil {
				resultCh <- struct {
					status string
					err    error
				}{"", err}
				return
			}
			if conf == nil {
				resultCh <- struct {
					status string
					err    error
				}{"", errors.New("nil ChangeConfiguration confirmation")}
				return
			}
			resultCh <- struct {
				status string
				err    error
			}{string(conf.Status), nil}
		},
		key,
		value,
	)
	if err != nil {
		return "", err
	}

	select {
	case result := <-resultCh:
		return result.status, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (h *HAL) ClearCache(ctx context.Context, chargerID string) (string, error) {
	resultCh := make(chan struct {
		status string
		err    error
	}, 1)

	err := h.cs.ClearCache(
		chargerID,
		func(conf *core.ClearCacheConfirmation, err error) {
			if err != nil {
				resultCh <- struct {
					status string
					err    error
				}{"", err}
				return
			}
			if conf == nil {
				resultCh <- struct {
					status string
					err    error
				}{"", errors.New("nil ClearCache confirmation")}
				return
			}
			resultCh <- struct {
				status string
				err    error
			}{string(conf.Status), nil}
		},
	)
	if err != nil {
		return "", err
	}

	select {
	case result := <-resultCh:
		return result.status, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (h *HAL) UnlockConnector(ctx context.Context, chargerID string, connectorID int) (string, error) {
	resultCh := make(chan struct {
		status string
		err    error
	}, 1)

	err := h.cs.UnlockConnector(
		chargerID,
		func(conf *core.UnlockConnectorConfirmation, err error) {
			if err != nil {
				resultCh <- struct {
					status string
					err    error
				}{"", err}
				return
			}
			if conf == nil {
				resultCh <- struct {
					status string
					err    error
				}{"", errors.New("nil UnlockConnector confirmation")}
				return
			}
			resultCh <- struct {
				status string
				err    error
			}{string(conf.Status), nil}
		},
		connectorID,
	)
	if err != nil {
		return "", err
	}

	select {
	case result := <-resultCh:
		return result.status, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (h *HAL) Reset(ctx context.Context, chargerID string, resetType string) (string, error) {
	var t core.ResetType
	switch strings.ToLower(strings.TrimSpace(resetType)) {
	case "soft":
		t = core.ResetTypeSoft
	case "hard":
		t = core.ResetTypeHard
	default:
		return "", fmt.Errorf("invalid reset type %q", resetType)
	}

	resultCh := make(chan struct {
		status string
		err    error
	}, 1)

	err := h.cs.Reset(
		chargerID,
		func(conf *core.ResetConfirmation, err error) {
			if err != nil {
				resultCh <- struct {
					status string
					err    error
				}{"", err}
				return
			}
			if conf == nil {
				resultCh <- struct {
					status string
					err    error
				}{"", errors.New("nil Reset confirmation")}
				return
			}
			resultCh <- struct {
				status string
				err    error
			}{string(conf.Status), nil}
		},
		t,
	)
	if err != nil {
		return "", err
	}

	select {
	case result := <-resultCh:
		return result.status, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (h *HAL) GetConfiguration(ctx context.Context, chargerID string, keys []string) (*core.GetConfigurationConfirmation, error) {
	resultCh := make(chan struct {
		conf *core.GetConfigurationConfirmation
		err  error
	}, 1)

	err := h.cs.GetConfiguration(
		chargerID,
		func(conf *core.GetConfigurationConfirmation, err error) {
			resultCh <- struct {
				conf *core.GetConfigurationConfirmation
				err  error
			}{conf, err}
		},
		keys,
	)
	if err != nil {
		return nil, err
	}

	select {
	case result := <-resultCh:
		if result.err != nil {
			return nil, result.err
		}
		if result.conf == nil {
			return nil, errors.New("nil GetConfiguration confirmation")
		}
		return result.conf, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *HAL) OnAuthorize(chargePointID string, request *core.AuthorizeRequest) (*core.AuthorizeConfirmation, error) {
	h.registry.Touch(chargePointID)
	return core.NewAuthorizationConfirmation(types.NewIdTagInfo(types.AuthorizationStatusAccepted)), nil
}

func (h *HAL) OnBootNotification(chargePointID string, request *core.BootNotificationRequest) (*core.BootNotificationConfirmation, error) {
	h.registry.Touch(chargePointID)
	h.scheduleRemoteOnlyConfigSync(chargePointID)
	h.scheduleBootRecovery(chargePointID)

	return core.NewBootNotificationConfirmation(
		types.NewDateTime(time.Now().UTC()),
		heartbeatIntervalSeconds,
		core.RegistrationStatusAccepted,
	), nil
}

func (h *HAL) OnDataTransfer(chargePointID string, request *core.DataTransferRequest) (*core.DataTransferConfirmation, error) {
	h.registry.Touch(chargePointID)
	return core.NewDataTransferConfirmation(core.DataTransferStatusAccepted), nil
}

func (h *HAL) OnHeartbeat(chargePointID string, request *core.HeartbeatRequest) (*core.HeartbeatConfirmation, error) {
	h.registry.Touch(chargePointID)
	return core.NewHeartbeatConfirmation(types.NewDateTime(time.Now().UTC())), nil
}

func (h *HAL) OnStatusNotification(chargePointID string, request *core.StatusNotificationRequest) (*core.StatusNotificationConfirmation, error) {
	h.registry.ApplyStatusNotification(
		chargePointID,
		request.ConnectorId,
		string(request.Status),
		string(request.ErrorCode),
	)

	return core.NewStatusNotificationConfirmation(), nil
}

func (h *HAL) OnStartTransaction(chargePointID string, request *core.StartTransactionRequest) (*core.StartTransactionConfirmation, error) {
	h.registry.Touch(chargePointID)

	openTransactions, err := h.store.ListOpenTransactionsByCharger(context.Background(), chargePointID)
	if err != nil {
		h.logger.Error("failed to check existing transactions", "charge_point_id", chargePointID, "error", err)
		return nil, fmt.Errorf("check existing transactions: %w", err)
	}

	for _, existing := range openTransactions {
		if existing == nil || existing.ConnectorID != request.ConnectorId {
			continue
		}

		sameStart := existing.IDTag == request.IdTag &&
			existing.MeterStart == float64(request.MeterStart)
		if !sameStart {
			h.logger.Warn(
				"rejecting overlapping transaction on connector",
				"charge_point_id", chargePointID,
				"connector_id", request.ConnectorId,
				"existing_transaction_id", existing.TransactionID,
			)
			return core.NewStartTransactionConfirmation(
				types.NewIdTagInfo(types.AuthorizationStatusBlocked),
				0,
			), nil
		}

		h.registry.ApplyStartTransaction(
			chargePointID,
			request.ConnectorId,
			existing.TransactionID,
			float64(request.MeterStart),
		)

		if h.hooks != nil {
			if err := h.hooks.EnqueueStartTransaction(context.Background(), existing); err != nil {
				return nil, fmt.Errorf("enqueue retried start transaction callback: %w", err)
			}
		}

		return core.NewStartTransactionConfirmation(
			types.NewIdTagInfo(types.AuthorizationStatusAccepted),
			int(existing.TransactionID),
		), nil
	}

	tx, err := h.store.CreateTransaction(context.Background(), store.CreateTransactionInput{
		ChargerID:       chargePointID,
		ConnectorID:     request.ConnectorId,
		MeterStart:      float64(request.MeterStart),
		IDTag:           request.IdTag,
		IsSingleSession: h.consumePendingSingleSession(chargePointID, request.ConnectorId, request.IdTag),
	})
	if err != nil {
		h.logger.Error("failed to create transaction", "charge_point_id", chargePointID, "error", err)
		return core.NewStartTransactionConfirmation(types.NewIdTagInfo(types.AuthorizationStatusBlocked), 0), nil
	}

	h.registry.ApplyStartTransaction(
		chargePointID,
		request.ConnectorId,
		tx.TransactionID,
		float64(request.MeterStart),
	)

	if h.hooks != nil {
		if err := h.hooks.EnqueueStartTransaction(context.Background(), tx); err != nil {
			h.logger.Error("failed to enqueue start transaction hook", "charge_point_id", chargePointID, "transaction_id", tx.TransactionID, "error", err)
			return nil, fmt.Errorf("enqueue start transaction callback: %w", err)
		}
	}

	return core.NewStartTransactionConfirmation(
		types.NewIdTagInfo(types.AuthorizationStatusAccepted),
		int(tx.TransactionID),
	), nil
}

func (h *HAL) OnMeterValues(chargePointID string, request *core.MeterValuesRequest) (*core.MeterValuesConfirmation, error) {
	h.registry.Touch(chargePointID)

	meterValueWh, ok := extractMeterValueWh(request)
	if ok {
		var txID64 *int64

		if request.TransactionId != nil && *request.TransactionId > 0 {
			v := int64(*request.TransactionId)
			txID64 = &v
		} else if v, found := h.registry.TransactionIDForConnector(chargePointID, request.ConnectorId); found {
			txID64 = &v
		}

		h.registry.ApplyMeterValue(chargePointID, request.ConnectorId, txID64, meterValueWh)

		if txID64 != nil {
			if _, err := h.store.UpdateLiveMeter(context.Background(), store.UpdateLiveMeterInput{
				ChargerID:     chargePointID,
				TransactionID: *txID64,
				MeterStop:     meterValueWh,
			}); err != nil {
				h.logger.Warn(
					"failed to update live meter",
					"charge_point_id", chargePointID,
					"transaction_id", *txID64,
					"error", err,
				)
			} else {
				h.checkAndRemoteStopIfLimitExceeded(chargePointID, *txID64)
			}
		}
	}

	return core.NewMeterValuesConfirmation(), nil
}

func (h *HAL) OnStopTransaction(chargePointID string, request *core.StopTransactionRequest) (*core.StopTransactionConfirmation, error) {
	h.registry.Touch(chargePointID)

	txID := int64(request.TransactionId)

	tx, err := h.store.StopTransaction(context.Background(), store.StopTransactionInput{
		ChargerID:     chargePointID,
		TransactionID: txID,
		MeterStop:     float64(request.MeterStop),
	})
	if err != nil {
		h.logger.Error(
			"failed to stop transaction",
			"charge_point_id", chargePointID,
			"transaction_id", txID,
			"error", err,
		)
		return nil, fmt.Errorf("persist stop transaction: %w", err)
	}

	if h.hooks != nil {
		if err := h.hooks.EnqueueCompletedTransaction(context.Background(), tx); err != nil {
			h.logger.Error("failed to enqueue completed transaction hook", "charge_point_id", chargePointID, "transaction_id", tx.TransactionID, "error", err)
			return nil, fmt.Errorf("enqueue completed transaction callback: %w", err)
		}
	}

	h.registry.ApplyStopTransaction(chargePointID, tx.ConnectorID, float64(request.MeterStop))

	return core.NewStopTransactionConfirmation(), nil
}

func extractMeterValueWh(request *core.MeterValuesRequest) (float64, bool) {
	for i := len(request.MeterValue) - 1; i >= 0; i-- {
		mv := request.MeterValue[i]
		if len(mv.SampledValue) == 0 {
			continue
		}

		selected := mv.SampledValue[0]

		for _, sample := range mv.SampledValue {
			if strings.EqualFold(string(sample.Measurand), "Energy.Active.Import.Register") {
				selected = sample
				break
			}
		}

		value, err := strconv.ParseFloat(strings.TrimSpace(selected.Value), 64)
		if err != nil {
			continue
		}

		unit := strings.ToLower(strings.TrimSpace(string(selected.Unit)))
		if unit == "kwh" || unit == "kilowatthour" {
			value *= 1000.0
		}

		return value, true
	}

	return 0, false
}

func ConnectedURL(host string, port int, chargerID string) string {
	return fmt.Sprintf("ws://%s:%d/%s", host, port, chargerID)
}
