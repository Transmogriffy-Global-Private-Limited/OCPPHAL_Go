package httpapi

import (
	"context"
	"net/http"
	"time"
)

type getDiagnosticsRequest struct {
	baseChargerRequest
	Location      string `json:"location"`
	StartTime     string `json:"start_time"`
	StopTime      string `json:"stop_time"`
	Retries       *int   `json:"retries"`
	RetryInterval *int   `json:"retry_interval"`
}

func (s *Server) getDiagnostics(w http.ResponseWriter, r *http.Request) {
	var req getDiagnosticsRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	chargerID := req.chargerID()
	if chargerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing uid/charge_point_id"})
		return
	}
	if req.Location == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing location"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	conf, err := s.hal.GetDiagnostics(
		ctx,
		chargerID,
		req.Location,
		req.StartTime,
		req.StopTime,
		req.Retries,
		req.RetryInterval,
	)
	if err != nil {
		s.writeRemoteError(w, "get diagnostics failed", chargerID, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "Accepted",
		"file_name": conf.FileName,
	})
}

type updateFirmwareRequest struct {
	baseChargerRequest
	Location      string `json:"location"`
	RetrieveDate  string `json:"retrieve_date"`
	Retries       *int   `json:"retries"`
	RetryInterval *int   `json:"retry_interval"`
}

func (s *Server) updateFirmware(w http.ResponseWriter, r *http.Request) {
	var req updateFirmwareRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	chargerID := req.chargerID()
	if chargerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing uid/charge_point_id"})
		return
	}
	if req.Location == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing location"})
		return
	}
	if req.RetrieveDate == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing retrieve_date"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	if err := s.hal.UpdateFirmware(
		ctx,
		chargerID,
		req.Location,
		req.RetrieveDate,
		req.Retries,
		req.RetryInterval,
	); err != nil {
		s.writeRemoteError(w, "update firmware failed", chargerID, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "Accepted"})
}

type triggerMessageRequest struct {
	baseChargerRequest
	RequestedMessage string `json:"requested_message"`
	ConnectorID      int    `json:"connector_id"`
	ConnectorId      int    `json:"connectorId"`
}

func (r triggerMessageRequest) connectorID() int {
	if r.ConnectorID > 0 {
		return r.ConnectorID
	}
	return r.ConnectorId
}

func (s *Server) triggerMessage(w http.ResponseWriter, r *http.Request) {
	var req triggerMessageRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	chargerID := req.chargerID()
	if chargerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing uid/charge_point_id"})
		return
	}
	if req.RequestedMessage == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing requested_message"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	status, err := s.hal.TriggerMessage(ctx, chargerID, req.RequestedMessage, req.connectorID())
	if err != nil {
		s.writeRemoteError(w, "trigger message failed", chargerID, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": status,
		"latest_message": map[string]any{
			"error": "latest charger-to-CMS message persistence not implemented yet",
		},
	})
}
