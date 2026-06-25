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
		"/api/get_diagnostics",
		"/api/update_firmware",
		"/api/reset",
		"/api/get_configuration",
		"/api/trigger_message",
		"/api/charger_analytics",
		"/api/check_charger_inactivity",
	} {
		routePath := path
		mux.HandleFunc(routePath, func(w http.ResponseWriter, r *http.Request) {
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
	default:
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"error": "route not implemented yet in ocpp-go rewrite",
			"path":  r.URL.Path,
		})
	}
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
	UID           string `json:"uid"`
	ChargePointID string `json:"charge_point_id"`
	IDTagSnake    string `json:"id_tag"`
	IDTagCamel    string `json:"idTag"`
	ConnectorID   int    `json:"connector_id"`
	ConnectorId   int    `json:"connectorId"`
}

func (r remoteStartRequest) chargerID() string {
	if r.UID != "" {
		return r.UID
	}
	return r.ChargePointID
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Invalid JSON"})
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
		s.logger.Warn("remote start failed", "charger_id", chargerID, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

type remoteStopRequest struct {
	UID           string `json:"uid"`
	ChargePointID string `json:"charge_point_id"`
	TransactionID int    `json:"transaction_id"`
	TransactionId int    `json:"transactionId"`
}

func (r remoteStopRequest) chargerID() string {
	if r.UID != "" {
		return r.UID
	}
	return r.ChargePointID
}

func (r remoteStopRequest) transactionID() int {
	if r.TransactionID > 0 {
		return r.TransactionID
	}
	return r.TransactionId
}

func (s *Server) remoteStop(w http.ResponseWriter, r *http.Request) {
	var req remoteStopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Invalid JSON"})
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
		s.logger.Warn("remote stop failed", "charger_id", chargerID, "transaction_id", transactionID, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": status})
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
