package ocpp16hal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp1.6/firmware"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/remotetrigger"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"
)

func (h *HAL) GetDiagnostics(
	ctx context.Context,
	chargerID string,
	location string,
	startTime string,
	stopTime string,
	retries *int,
	retryInterval *int,
) (*firmware.GetDiagnosticsConfirmation, error) {
	props := []func(*firmware.GetDiagnosticsRequest){}

	if retries != nil {
		v := *retries
		props = append(props, func(req *firmware.GetDiagnosticsRequest) {
			req.Retries = &v
		})
	}

	if retryInterval != nil {
		v := *retryInterval
		props = append(props, func(req *firmware.GetDiagnosticsRequest) {
			req.RetryInterval = &v
		})
	}

	start, err := parseOptionalDateTime(startTime)
	if err != nil {
		return nil, fmt.Errorf("invalid start_time: %w", err)
	}
	if start != nil {
		props = append(props, func(req *firmware.GetDiagnosticsRequest) {
			req.StartTime = start
		})
	}

	stop, err := parseOptionalDateTime(stopTime)
	if err != nil {
		return nil, fmt.Errorf("invalid stop_time: %w", err)
	}
	if stop != nil {
		props = append(props, func(req *firmware.GetDiagnosticsRequest) {
			req.StopTime = stop
		})
	}

	resultCh := make(chan struct {
		conf *firmware.GetDiagnosticsConfirmation
		err  error
	}, 1)

	err = h.cs.GetDiagnostics(
		chargerID,
		func(conf *firmware.GetDiagnosticsConfirmation, err error) {
			resultCh <- struct {
				conf *firmware.GetDiagnosticsConfirmation
				err  error
			}{conf, err}
		},
		location,
		props...,
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
			return nil, errors.New("nil GetDiagnostics confirmation")
		}
		return result.conf, nil

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *HAL) UpdateFirmware(
	ctx context.Context,
	chargerID string,
	location string,
	retrieveDateRaw string,
	retries *int,
	retryInterval *int,
) error {
	retrieveDate, err := parseRequiredDateTime(retrieveDateRaw)
	if err != nil {
		return fmt.Errorf("invalid retrieve_date: %w", err)
	}

	props := []func(*firmware.UpdateFirmwareRequest){}

	if retries != nil {
		v := *retries
		props = append(props, func(req *firmware.UpdateFirmwareRequest) {
			req.Retries = &v
		})
	}

	if retryInterval != nil {
		v := *retryInterval
		props = append(props, func(req *firmware.UpdateFirmwareRequest) {
			req.RetryInterval = &v
		})
	}

	resultCh := make(chan error, 1)

	err = h.cs.UpdateFirmware(
		chargerID,
		func(conf *firmware.UpdateFirmwareConfirmation, err error) {
			if err != nil {
				resultCh <- err
				return
			}
			if conf == nil {
				resultCh <- errors.New("nil UpdateFirmware confirmation")
				return
			}
			resultCh <- nil
		},
		location,
		retrieveDate,
		props...,
	)
	if err != nil {
		return err
	}

	select {
	case err := <-resultCh:
		return err

	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *HAL) TriggerMessage(ctx context.Context, chargerID string, requestedMessage string, connectorID int) (string, error) {
	props := []func(*remotetrigger.TriggerMessageRequest){}

	if connectorID > 0 {
		v := connectorID
		props = append(props, func(req *remotetrigger.TriggerMessageRequest) {
			req.ConnectorId = &v
		})
	}

	resultCh := make(chan struct {
		status string
		err    error
	}, 1)

	err := h.cs.TriggerMessage(
		chargerID,
		func(conf *remotetrigger.TriggerMessageConfirmation, err error) {
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
				}{"", errors.New("nil TriggerMessage confirmation")}
				return
			}
			resultCh <- struct {
				status string
				err    error
			}{string(conf.Status), nil}
		},
		remotetrigger.MessageTrigger(requestedMessage),
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

func (h *HAL) OnDiagnosticsStatusNotification(chargePointID string, request *firmware.DiagnosticsStatusNotificationRequest) (*firmware.DiagnosticsStatusNotificationConfirmation, error) {
	h.registry.Touch(chargePointID)
	h.logger.Info("diagnostics status notification", "charge_point_id", chargePointID, "status", request.Status)
	return firmware.NewDiagnosticsStatusNotificationConfirmation(), nil
}

func (h *HAL) OnFirmwareStatusNotification(chargePointID string, request *firmware.FirmwareStatusNotificationRequest) (*firmware.FirmwareStatusNotificationConfirmation, error) {
	h.registry.Touch(chargePointID)
	h.logger.Info("firmware status notification", "charge_point_id", chargePointID, "status", request.Status)
	return firmware.NewFirmwareStatusNotificationConfirmation(), nil
}

func parseOptionalDateTime(raw string) (*types.DateTime, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, err
	}

	return types.NewDateTime(t), nil
}

func parseRequiredDateTime(raw string) (*types.DateTime, error) {
	dt, err := parseOptionalDateTime(raw)
	if err != nil {
		return nil, err
	}
	if dt == nil {
		return nil, errors.New("missing datetime")
	}
	return dt, nil
}
