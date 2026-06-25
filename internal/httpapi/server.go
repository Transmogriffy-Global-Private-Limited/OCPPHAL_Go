package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/config"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/ocpp"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/session"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/state"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/store"
)

type Server struct {
	cfg      config.Config
	logger   *slog.Logger
	registry *state.Registry
	store    store.TransactionStore
	manager  *session.Manager
	upgrader websocket.Upgrader
}

func NewServer(cfg config.Config, logger *slog.Logger, registry *state.Registry, transactionStore store.TransactionStore) *Server {
	return &Server{
		cfg:      cfg,
		logger:   logger,
		registry: registry,
		store:    transactionStore,
		manager:  session.NewManager(registry, logger),
		upgrader: websocket.Upgrader{
			CheckOrigin:  func(r *http.Request) bool { return true },
			Subprotocols: []string{"ocpp1.6", "ocpp1.6j"},
		},
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/hello", s.hello)

	apiPaths := []string{
		"/api/change_availability",
		"/api/start_transaction",
		"/api/stop_transaction",
		"/api/change_configuration",
		"/api/clear_cache",
		"/api/unlock_connector",
		"/api/get_diagnostics",
		"/api/update_firmware",
		"/api/reset",
		"/api/get_configuration",
		"/api/status",
		"/api/trigger_message",
		"/api/charger_analytics",
		"/api/check_charger_inactivity",
	}

	for _, path := range apiPaths {
		routePath := path
		mux.HandleFunc(routePath, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				s.options(w, r)
				return
			}

			s.requireAPIKey(http.HandlerFunc(s.apiCompatibilityShell)).ServeHTTP(w, r)
		})
	}

	mux.HandleFunc("/frontend/ws/", s.frontendWebSocket)
	mux.HandleFunc("/", s.chargerWebSocket)

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
			writeJSON(w, http.StatusForbidden, map[string]any{
				"detail": "Invalid API key",
			})
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

func (s *Server) options(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) apiCompatibilityShell(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	switch r.URL.Path {
	case "/api/status":
		s.status(w, r)
	default:
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"error": "Go OCPPHAL compatibility shell is running, but this route is not implemented yet",
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
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"detail": "Invalid JSON",
		})
		return
	}

	if req.UID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"detail": "Missing uid",
		})
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

type remoteStartTransactionRequest struct {
	UID             string `json:"uid"`
	IDTag           string `json:"id_tag"`
	ConnectorID     int    `json:"connector_id"`
	IsSingleSession bool   `json:"is_single_session"`
}

type remoteStartTransactionResponse struct {
	Status string `json:"status"`
}

type remoteStartTransactionConf struct {
	Status string `json:"status"`
}

func (s *Server) remoteStartTransaction(w http.ResponseWriter, r *http.Request) {
	var req remoteStartTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"detail": "Invalid JSON",
		})
		return
	}

	if req.UID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"detail": "Missing uid",
		})
		return
	}

	if req.IDTag == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"detail": "Missing id_tag",
		})
		return
	}

	if req.ConnectorID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"detail": "Invalid connector_id",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	payload := map[string]any{
		"idTag":       req.IDTag,
		"connectorId": req.ConnectorID,
	}

	raw, err := s.manager.Call(ctx, req.UID, "RemoteStartTransaction", payload)
	if err != nil {
		s.logger.Warn("RemoteStartTransaction failed", "charger_id", req.UID, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": err.Error(),
		})
		return
	}

	var conf remoteStartTransactionConf
	if err := json.Unmarshal(raw, &conf); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": "Invalid RemoteStartTransaction response from charger",
		})
		return
	}

	if conf.Status == "" {
		conf.Status = "Unknown"
	}

	writeJSON(w, http.StatusOK, remoteStartTransactionResponse{
		Status: conf.Status,
	})
}
func (s *Server) frontendWebSocket(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimPrefix(r.URL.Path, "/frontend/ws/")
	if uid == "" || strings.Contains(uid, "/") {
		http.NotFound(w, r)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("frontend websocket upgrade failed", "uid", uid, "error", err)
		return
	}
	defer conn.Close()

	_ = conn.WriteJSON(map[string]string{
		"charger_id": uid,
		"status":     "Offline",
	})

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (s *Server) chargerWebSocket(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/frontend/ws/") {
		http.NotFound(w, r)
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	chargerID := parts[0]
	serialNumber := ""
	if len(parts) == 2 {
		serialNumber = parts[1]
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("charger websocket upgrade failed", "charger_id", chargerID, "error", err)
		return
	}
	defer conn.Close()

	s.manager.Handle(conn, chargerID, serialNumber, s.handleOCPPCall)
}

func (s *Server) handleOCPPCall(sess *session.Session, chargerID string, call *ocpp.Call) {
	now := time.Now().UTC()

	switch call.Action {
	case "BootNotification":
		sess.WriteCallResult(call.UniqueID, map[string]any{
			"currentTime": now.Format(time.RFC3339),
			"interval":    900,
			"status":      "Accepted",
		})

	case "Authorize":
		sess.WriteCallResult(call.UniqueID, map[string]any{
			"idTagInfo": map[string]string{
				"status": "Accepted",
			},
		})

	case "Heartbeat":
		sess.WriteCallResult(call.UniqueID, map[string]string{
			"currentTime": now.Format(time.RFC3339),
		})

	case "StartTransaction":
		payload, err := ocpp.ParseStartTransactionPayload(call.Payload)
		if err != nil {
			sess.WriteCallError(call.UniqueID, "FormationViolation", err.Error())
			return
		}

		tx, err := s.store.CreateTransaction(context.Background(), store.CreateTransactionInput{
			ChargerID:       chargerID,
			ConnectorID:     payload.ConnectorID,
			MeterStart:      payload.MeterStart,
			IDTag:           payload.IDTag,
			IsSingleSession: false,
		})
		if err != nil {
			s.logger.Error("failed to create transaction", "charger_id", chargerID, "error", err)
			sess.WriteCallResult(call.UniqueID, map[string]any{
				"transactionId": 0,
				"idTagInfo": map[string]string{
					"status": "Rejected",
				},
			})
			return
		}

		s.registry.ApplyStartTransaction(chargerID, payload.ConnectorID, tx.TransactionID, payload.MeterStart)

		sess.WriteCallResult(call.UniqueID, map[string]any{
			"transactionId": tx.TransactionID,
			"idTagInfo": map[string]string{
				"status": "Accepted",
			},
		})

	case "StopTransaction":
		payload, err := ocpp.ParseStopTransactionPayload(call.Payload)
		if err != nil {
			sess.WriteCallError(call.UniqueID, "FormationViolation", err.Error())
			return
		}

		connectorID := 0
		if payload.ConnectorID != nil {
			connectorID = *payload.ConnectorID
		} else if foundConnectorID, ok := s.registry.FindConnectorByTransactionID(chargerID, payload.TransactionID); ok {
			connectorID = foundConnectorID
		}

		if connectorID == 0 {
			s.logger.Error("failed to resolve connector for StopTransaction", "charger_id", chargerID, "transaction_id", payload.TransactionID)
			sess.WriteCallResult(call.UniqueID, map[string]any{
				"idTagInfo": map[string]string{
					"status": "Rejected",
				},
			})
			return
		}

		if _, err := s.store.StopTransaction(context.Background(), store.StopTransactionInput{
			ChargerID:     chargerID,
			TransactionID: payload.TransactionID,
			MeterStop:     payload.MeterStop,
		}); err != nil {
			s.logger.Error("failed to stop transaction", "charger_id", chargerID, "transaction_id", payload.TransactionID, "error", err)
		}

		s.registry.ApplyStopTransaction(chargerID, connectorID, payload.MeterStop)

		sess.WriteCallResult(call.UniqueID, map[string]any{
			"idTagInfo": map[string]string{
				"status": "Accepted",
			},
		})

	case "StatusNotification":
		payload, err := ocpp.ParseStatusNotificationPayload(call.Payload)
		if err != nil {
			sess.WriteCallError(call.UniqueID, "FormationViolation", err.Error())
			return
		}

		s.registry.ApplyStatusNotification(
			chargerID,
			payload.ConnectorID,
			payload.Status,
			payload.ErrorCode,
			payload.TransactionID,
		)

		sess.WriteCallResult(call.UniqueID, map[string]any{})

	case "MeterValues":
		payload, err := ocpp.ParseMeterValuesPayload(call.Payload)
		if err != nil {
			sess.WriteCallError(call.UniqueID, "FormationViolation", err.Error())
			return
		}

		if payload.MeterValueWh != nil {
			s.registry.ApplyMeterValue(
				chargerID,
				payload.ConnectorID,
				payload.TransactionID,
				*payload.MeterValueWh,
			)

			if payload.TransactionID != nil {
				if _, err := s.store.UpdateLiveMeter(context.Background(), store.UpdateLiveMeterInput{
					ChargerID:     chargerID,
					TransactionID: *payload.TransactionID,
					MeterStop:     *payload.MeterValueWh,
				}); err != nil {
					s.logger.Warn("failed to update live meter in transaction store", "charger_id", chargerID, "transaction_id", *payload.TransactionID, "error", err)
				}
			}
		}

		sess.WriteCallResult(call.UniqueID, map[string]any{})

	case "DiagnosticsStatusNotification":
		sess.WriteCallResult(call.UniqueID, map[string]any{})

	case "FirmwareStatusNotification":
		sess.WriteCallResult(call.UniqueID, map[string]any{})

	default:
		sess.WriteCallError(call.UniqueID, "NotImplemented", "Go OCPP core scaffolded, action not implemented yet")
	}
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, x-api-key, Access-Control-Allow-Origin")
}

func writeJSON(w http.ResponseWriter, statusCode int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(body)
}
