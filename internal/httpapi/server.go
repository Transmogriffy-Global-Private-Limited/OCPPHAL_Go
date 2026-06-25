package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/config"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/ocpp16hal"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/state"
)

type Server struct {
	cfg      config.Config
	logger   *slog.Logger
	registry *state.Registry
	hal      *ocpp16hal.HAL
}

func NewServer(cfg config.Config, logger *slog.Logger, registry *state.Registry, hal *ocpp16hal.HAL) *Server {
	return &Server{
		cfg:      cfg,
		logger:   logger,
		registry: registry,
		hal:      hal,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/hello", s.hello)

	for _, path := range []string{
		"/api/status",
		"/api/start_transaction",
		"/api/stop_transaction",
		"/api/change_availability",
		"/api/change_configuration",
		"/api/clear_cache",
		"/api/unlock_connector",
		"/api/reset",
		"/api/get_configuration",
		"/api/get_diagnostics",
		"/api/update_firmware",
		"/api/trigger_message",
		"/api/charger_analytics",
		"/api/check_charger_inactivity",
	} {
		routePath := path
		mux.HandleFunc(routePath, func(w http.ResponseWriter, r *http.Request) {
			_ = routePath

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			s.requireAPIKey(http.HandlerFunc(s.api)).ServeHTTP(w, r)
		})
	}

	return s.withCORS(mux)
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIKey == "" || r.Header.Get("x-api-key") != s.cfg.APIKey {
			writeJSON(w, http.StatusForbidden, map[string]any{"detail": "Invalid API key"})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Helloo, this is the OCPP HAL API. It is running fine.",
	})
}

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	switch r.URL.Path {
	case "/api/status":
		s.status(w, r)
	case "/api/start_transaction":
		s.remoteStart(w, r)
	case "/api/stop_transaction":
		s.remoteStop(w, r)
	case "/api/change_availability":
		s.changeAvailability(w, r)
	case "/api/change_configuration":
		s.changeConfiguration(w, r)
	case "/api/clear_cache":
		s.clearCache(w, r)
	case "/api/unlock_connector":
		s.unlockConnector(w, r)
	case "/api/reset":
		s.reset(w, r)
	case "/api/get_configuration":
		s.getConfiguration(w, r)
	case "/api/get_diagnostics":
		s.getDiagnostics(w, r)
	case "/api/update_firmware":
		s.updateFirmware(w, r)
	case "/api/trigger_message":
		s.triggerMessage(w, r)
	default:
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"error": "route not implemented yet in ocpp-go rewrite",
			"path":  r.URL.Path,
		})
	}
}

type baseChargerRequest struct {
	UID           string `json:"uid"`
	ChargePointID string `json:"charge_point_id"`
	ClientID      string `json:"client_id"`
}

func (r baseChargerRequest) chargerID() string {
	if r.UID != "" {
		return r.UID
	}
	if r.ChargePointID != "" {
		return r.ChargePointID
	}
	return r.ClientID
}

type statusRequest struct {
	UID string `json:"uid"`
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	var req statusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Invalid JSON"})
		return
	}

	if req.UID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing uid"})
		return
	}

	if req.UID == "all" || req.UID == "all_online" {
		all := s.registry.SnapshotAll()
		resp := make(map[string]any, len(all))

		for chargerID, cp := range all {
			if req.UID == "all_online" && !cp.Online {
				continue
			}

			resp[chargerID] = statusPayload(cp)
		}

		writeJSON(w, http.StatusOK, resp)
		return
	}

	cp, ok := s.registry.Snapshot(req.UID)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":                       "Offline",
			"connectors":                   map[string]any{},
			"online":                       "Offline",
			"latest_message_received_time": nil,
		})
		return
	}

	writeJSON(w, http.StatusOK, statusPayload(cp))
}

func statusPayload(cp *state.ChargerState) map[string]any {
	online := "Offline"
	if cp.Online && cp.HasError {
		online = "Online (with error)"
	} else if cp.Online {
		online = "Online"
	}

	return map[string]any{
		"status":                       cp.Status,
		"connectors":                   cp.Connectors,
		"online":                       online,
		"latest_message_received_time": cp.LastMessageTime.Format(time.RFC3339Nano),
	}
}

type remoteStartRequest struct {
	baseChargerRequest
	IDTagSnake  string `json:"id_tag"`
	IDTagCamel  string `json:"idTag"`
	ConnectorID int    `json:"connector_id"`
	ConnectorId int    `json:"connectorId"`
}

func (r remoteStartRequest) idTag() string {
	if r.IDTagSnake != "" {
		return r.IDTagSnake
	}
	return r.IDTagCamel
}

func (r remoteStartRequest) connectorID() int {
	if r.ConnectorID > 0 {
		return r.ConnectorID
	}
	return r.ConnectorId
}

func (s *Server) remoteStart(w http.ResponseWriter, r *http.Request) {
	var req remoteStartRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	chargerID := req.chargerID()
	idTag := req.idTag()
	connectorID := req.connectorID()

	if chargerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing uid/charge_point_id"})
		return
	}
	if idTag == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing id_tag/idTag"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	status, err := s.hal.RemoteStartTransaction(ctx, chargerID, idTag, connectorID)
	if err != nil {
		s.writeRemoteError(w, "remote start failed", chargerID, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

type remoteStopRequest struct {
	baseChargerRequest
	TransactionID int `json:"transaction_id"`
	TransactionId int `json:"transactionId"`
}

func (r remoteStopRequest) transactionID() int {
	if r.TransactionID > 0 {
		return r.TransactionID
	}
	return r.TransactionId
}

func (s *Server) remoteStop(w http.ResponseWriter, r *http.Request) {
	var req remoteStopRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	chargerID := req.chargerID()
	transactionID := req.transactionID()

	if chargerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing uid/charge_point_id"})
		return
	}
	if transactionID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Invalid transaction_id/transactionId"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	status, err := s.hal.RemoteStopTransaction(ctx, chargerID, transactionID)
	if err != nil {
		s.writeRemoteError(w, "remote stop failed", chargerID, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

type changeAvailabilityRequest struct {
	baseChargerRequest
	ConnectorID      int    `json:"connector_id"`
	ConnectorId      int    `json:"connectorId"`
	Type             string `json:"type"`
	AvailabilityType string `json:"availability_type"`
}

func (r changeAvailabilityRequest) connectorID() int {
	if r.ConnectorID > 0 {
		return r.ConnectorID
	}
	return r.ConnectorId
}

func (r changeAvailabilityRequest) availabilityType() string {
	if r.Type != "" {
		return r.Type
	}
	return r.AvailabilityType
}

func (s *Server) changeAvailability(w http.ResponseWriter, r *http.Request) {
	var req changeAvailabilityRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	chargerID := req.chargerID()
	if chargerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing uid/charge_point_id"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	status, err := s.hal.ChangeAvailability(ctx, chargerID, req.connectorID(), req.availabilityType())
	if err != nil {
		s.writeRemoteError(w, "change availability failed", chargerID, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

type changeConfigurationRequest struct {
	baseChargerRequest
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (s *Server) changeConfiguration(w http.ResponseWriter, r *http.Request) {
	var req changeConfigurationRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	chargerID := req.chargerID()
	if chargerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing uid/charge_point_id"})
		return
	}
	if req.Key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing key"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	status, err := s.hal.ChangeConfiguration(ctx, chargerID, req.Key, req.Value)
	if err != nil {
		s.writeRemoteError(w, "change configuration failed", chargerID, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) clearCache(w http.ResponseWriter, r *http.Request) {
	var req baseChargerRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	chargerID := req.chargerID()
	if chargerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing uid/charge_point_id"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	status, err := s.hal.ClearCache(ctx, chargerID)
	if err != nil {
		s.writeRemoteError(w, "clear cache failed", chargerID, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

type unlockConnectorRequest struct {
	baseChargerRequest
	ConnectorID int `json:"connector_id"`
	ConnectorId int `json:"connectorId"`
}

func (r unlockConnectorRequest) connectorID() int {
	if r.ConnectorID > 0 {
		return r.ConnectorID
	}
	return r.ConnectorId
}

func (s *Server) unlockConnector(w http.ResponseWriter, r *http.Request) {
	var req unlockConnectorRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	chargerID := req.chargerID()
	connectorID := req.connectorID()

	if chargerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing uid/charge_point_id"})
		return
	}
	if connectorID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Invalid connector_id/connectorId"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	status, err := s.hal.UnlockConnector(ctx, chargerID, connectorID)
	if err != nil {
		s.writeRemoteError(w, "unlock connector failed", chargerID, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

type resetRequest struct {
	baseChargerRequest
	Type      string `json:"type"`
	ResetType string `json:"reset_type"`
}

func (r resetRequest) resetType() string {
	if r.Type != "" {
		return r.Type
	}
	return r.ResetType
}

func (s *Server) reset(w http.ResponseWriter, r *http.Request) {
	var req resetRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	chargerID := req.chargerID()
	resetType := req.resetType()
	if resetType == "" {
		resetType = "Soft"
	}

	if chargerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing uid/charge_point_id"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	status, err := s.hal.Reset(ctx, chargerID, resetType)
	if err != nil {
		s.writeRemoteError(w, "reset failed", chargerID, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

type getConfigurationRequest struct {
	baseChargerRequest
	Key               []string `json:"key"`
	Keys              []string `json:"keys"`
	ConfigurationKeys []string `json:"configuration_keys"`
}

func (r getConfigurationRequest) keys() []string {
	if len(r.Key) > 0 {
		return r.Key
	}
	if len(r.Keys) > 0 {
		return r.Keys
	}
	return r.ConfigurationKeys
}

func (s *Server) getConfiguration(w http.ResponseWriter, r *http.Request) {
	var req getConfigurationRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	chargerID := req.chargerID()
	if chargerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing uid/charge_point_id"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	conf, err := s.hal.GetConfiguration(ctx, chargerID, req.keys())
	if err != nil {
		s.writeRemoteError(w, "get configuration failed", chargerID, err)
		return
	}

	writeJSON(w, http.StatusOK, conf)
}

func (s *Server) writeRemoteError(w http.ResponseWriter, msg string, chargerID string, err error) {
	s.logger.Warn(msg, "charger_id", chargerID, "error", err)
	writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Invalid JSON"})
		return false
	}

	return true
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, x-api-key, Access-Control-Allow-Origin")
}

func writeJSON(w http.ResponseWriter, statusCode int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(body)
}
