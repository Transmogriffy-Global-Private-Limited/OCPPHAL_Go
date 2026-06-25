package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"
)

type smokeHandler struct {
	remoteStartCh chan *core.RemoteStartTransactionRequest
	remoteStopCh  chan *core.RemoteStopTransactionRequest
}

func newSmokeHandler() *smokeHandler {
	return &smokeHandler{
		remoteStartCh: make(chan *core.RemoteStartTransactionRequest, 1),
		remoteStopCh:  make(chan *core.RemoteStopTransactionRequest, 1),
	}
}

func (h *smokeHandler) OnChangeAvailability(request *core.ChangeAvailabilityRequest) (*core.ChangeAvailabilityConfirmation, error) {
	fmt.Println("ChargePoint received ChangeAvailability")
	return core.NewChangeAvailabilityConfirmation(core.AvailabilityStatusAccepted), nil
}

func (h *smokeHandler) OnChangeConfiguration(request *core.ChangeConfigurationRequest) (*core.ChangeConfigurationConfirmation, error) {
	fmt.Println("ChargePoint received ChangeConfiguration:", request.Key, request.Value)
	return core.NewChangeConfigurationConfirmation(core.ConfigurationStatusAccepted), nil
}

func (h *smokeHandler) OnClearCache(request *core.ClearCacheRequest) (*core.ClearCacheConfirmation, error) {
	fmt.Println("ChargePoint received ClearCache")
	return core.NewClearCacheConfirmation(core.ClearCacheStatusAccepted), nil
}

func (h *smokeHandler) OnDataTransfer(request *core.DataTransferRequest) (*core.DataTransferConfirmation, error) {
	return core.NewDataTransferConfirmation(core.DataTransferStatusAccepted), nil
}

func (h *smokeHandler) OnGetConfiguration(request *core.GetConfigurationRequest) (*core.GetConfigurationConfirmation, error) {
	fmt.Println("ChargePoint received GetConfiguration")
	return core.NewGetConfigurationConfirmation([]core.ConfigurationKey{}), nil
}

func (h *smokeHandler) OnRemoteStartTransaction(request *core.RemoteStartTransactionRequest) (*core.RemoteStartTransactionConfirmation, error) {
	fmt.Println("ChargePoint received RemoteStartTransaction:", "idTag:", request.IdTag)

	select {
	case h.remoteStartCh <- request:
	default:
	}

	return core.NewRemoteStartTransactionConfirmation(types.RemoteStartStopStatusAccepted), nil
}

func (h *smokeHandler) OnRemoteStopTransaction(request *core.RemoteStopTransactionRequest) (*core.RemoteStopTransactionConfirmation, error) {
	fmt.Println("ChargePoint received RemoteStopTransaction:", "transactionId:", request.TransactionId)

	select {
	case h.remoteStopCh <- request:
	default:
	}

	return core.NewRemoteStopTransactionConfirmation(types.RemoteStartStopStatusAccepted), nil
}

func (h *smokeHandler) OnReset(request *core.ResetRequest) (*core.ResetConfirmation, error) {
	fmt.Println("ChargePoint received Reset")
	return core.NewResetConfirmation(core.ResetStatusAccepted), nil
}

func (h *smokeHandler) OnUnlockConnector(request *core.UnlockConnectorRequest) (*core.UnlockConnectorConfirmation, error) {
	fmt.Println("ChargePoint received UnlockConnector")
	return core.NewUnlockConnectorConfirmation(core.UnlockStatusUnlocked), nil
}

func main() {
	clientID := env("CLIENT_ID", "CP-REST-CORE-001")
	centralSystemURL := env("CENTRAL_SYSTEM_URL", "ws://127.0.0.1:18081")
	restBaseURL := env("REST_BASE_URL", "http://127.0.0.1:18080")
	apiKey := env("API_KEY", "testkey")

	handler := newSmokeHandler()
	cp := ocpp16.NewChargePoint(clientID, nil, nil)
	cp.SetCoreHandler(handler)

	if err := cp.Start(centralSystemURL); err != nil {
		log.Fatalf("connect charge point: %v", err)
	}
	defer cp.Stop()

	waitUntilConnected(cp, 5*time.Second)

	fmt.Println("connected:", cp.IsConnected())

	bootConf, err := cp.BootNotification("SmokeModel", "SmokeVendor")
	if err != nil {
		log.Fatalf("BootNotification failed: %v", err)
	}
	fmt.Println("BootNotification:", bootConf.Status, "interval:", bootConf.Interval)

	_, err = cp.StatusNotification(1, core.NoError, core.ChargePointStatusAvailable)
	if err != nil {
		log.Fatalf("StatusNotification Available failed: %v", err)
	}
	fmt.Println("StatusNotification: Available")

	fmt.Println("REST /api/change_availability:", postJSON(
		restBaseURL+"/api/change_availability",
		apiKey,
		map[string]any{
			"uid":          clientID,
			"connector_id": 1,
			"type":         "Operative",
		},
	))

	fmt.Println("REST /api/change_configuration:", postJSON(
		restBaseURL+"/api/change_configuration",
		apiKey,
		map[string]any{
			"uid":   clientID,
			"key":   "HeartbeatInterval",
			"value": "900",
		},
	))

	fmt.Println("REST /api/clear_cache:", postJSON(
		restBaseURL+"/api/clear_cache",
		apiKey,
		map[string]any{
			"uid": clientID,
		},
	))

	fmt.Println("REST /api/unlock_connector:", postJSON(
		restBaseURL+"/api/unlock_connector",
		apiKey,
		map[string]any{
			"uid":          clientID,
			"connector_id": 1,
		},
	))

	fmt.Println("REST /api/reset:", postJSON(
		restBaseURL+"/api/reset",
		apiKey,
		map[string]any{
			"uid":  clientID,
			"type": "Soft",
		},
	))

	fmt.Println("REST /api/get_configuration:", postJSON(
		restBaseURL+"/api/get_configuration",
		apiKey,
		map[string]any{
			"uid": clientID,
			"key": []string{"HeartbeatInterval"},
		},
	))

	remoteStartStatus := postJSON(
		restBaseURL+"/api/start_transaction",
		apiKey,
		map[string]any{
			"uid":          clientID,
			"id_tag":       "USER001",
			"connector_id": 1,
		},
	)
	fmt.Println("REST /api/start_transaction:", remoteStartStatus)

	remoteStartReq := waitRemoteStart(handler.remoteStartCh, 5*time.Second)
	connectorID := 1
	if remoteStartReq.ConnectorId != nil && *remoteStartReq.ConnectorId > 0 {
		connectorID = *remoteStartReq.ConnectorId
	}

	startConf, err := cp.StartTransaction(connectorID, remoteStartReq.IdTag, 1000, types.Now())
	if err != nil {
		log.Fatalf("StartTransaction failed: %v", err)
	}
	if startConf == nil {
		log.Fatalf("StartTransaction returned nil confirmation")
	}

	transactionID := startConf.TransactionId
	fmt.Println("StartTransaction:", startConf.IdTagInfo.Status, "transactionId:", transactionID)

	_, err = cp.StatusNotification(connectorID, core.NoError, core.ChargePointStatusCharging)
	if err != nil {
		log.Fatalf("StatusNotification Charging failed: %v", err)
	}
	fmt.Println("StatusNotification: Charging")

	meterValues := []types.MeterValue{
		{
			Timestamp: types.Now(),
			SampledValue: []types.SampledValue{
				{
					Value:     "2.500",
					Measurand: types.MeasurandEnergyActiveImportRegister,
					Unit:      types.UnitOfMeasureKWh,
				},
			},
		},
	}

	_, err = cp.MeterValues(
		connectorID,
		meterValues,
		func(request *core.MeterValuesRequest) {
			request.TransactionId = &transactionID
		},
	)
	if err != nil {
		log.Fatalf("MeterValues failed: %v", err)
	}
	fmt.Println("MeterValues: 2.500 kWh")

	remoteStopStatus := postJSON(
		restBaseURL+"/api/stop_transaction",
		apiKey,
		map[string]any{
			"uid":            clientID,
			"transaction_id": transactionID,
		},
	)
	fmt.Println("REST /api/stop_transaction:", remoteStopStatus)

	remoteStopReq := waitRemoteStop(handler.remoteStopCh, 5*time.Second)
	if remoteStopReq.TransactionId != transactionID {
		log.Fatalf("RemoteStop transaction mismatch: got %d want %d", remoteStopReq.TransactionId, transactionID)
	}

	stopConf, err := cp.StopTransaction(3500, types.Now(), transactionID)
	if err != nil {
		log.Fatalf("StopTransaction failed: %v", err)
	}

	stopStatus := "Accepted"
	if stopConf != nil && stopConf.IdTagInfo != nil {
		stopStatus = string(stopConf.IdTagInfo.Status)
	}
	fmt.Println("StopTransaction:", stopStatus)

	_, err = cp.StatusNotification(connectorID, core.NoError, core.ChargePointStatusAvailable)
	if err != nil {
		log.Fatalf("StatusNotification Available after stop failed: %v", err)
	}
	fmt.Println("StatusNotification: Available after stop")

	fmt.Println("REST core outbound smoke complete")
}

func waitRemoteStart(ch <-chan *core.RemoteStartTransactionRequest, timeout time.Duration) *core.RemoteStartTransactionRequest {
	select {
	case req := <-ch:
		return req
	case <-time.After(timeout):
		log.Fatalf("timed out waiting for RemoteStartTransaction")
	}
	return nil
}

func waitRemoteStop(ch <-chan *core.RemoteStopTransactionRequest, timeout time.Duration) *core.RemoteStopTransactionRequest {
	select {
	case req := <-ch:
		return req
	case <-time.After(timeout):
		log.Fatalf("timed out waiting for RemoteStopTransaction")
	}
	return nil
}

func postJSON(url string, apiKey string, body any) string {
	raw, err := json.Marshal(body)
	if err != nil {
		log.Fatalf("marshal %s: %v", url, err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		log.Fatalf("new request %s: %v", url, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("POST %s failed: %v", url, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Fatalf("POST %s returned %s: %s", url, resp.Status, string(respBody))
	}

	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return string(respBody)
	}

	if status, ok := parsed["status"].(string); ok {
		return status
	}

	return string(respBody)
}

func waitUntilConnected(cp ocpp16.ChargePoint, timeout time.Duration) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if cp.IsConnected() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	log.Fatalf("charge point did not connect within %s", timeout)
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
