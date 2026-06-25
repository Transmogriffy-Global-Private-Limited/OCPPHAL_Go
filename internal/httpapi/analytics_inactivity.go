package httpapi

import (
	"context"
	"net/http"
	"time"
)

type chargerAnalyticsRequest struct {
	UID       string `json:"uid"`
	ChargerID string `json:"charger_id"`
	UserID    string `json:"user_id"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

func (r chargerAnalyticsRequest) requestedChargerID() string {
	if r.ChargerID != "" {
		return r.ChargerID
	}
	if r.UID != "" {
		return r.UID
	}
	return "all"
}

func (s *Server) chargerAnalytics(w http.ResponseWriter, r *http.Request) {
	var req chargerAnalyticsRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	start, err := parseOptionalRFC3339(req.StartTime)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Invalid start_time"})
		return
	}

	end, err := parseOptionalRFC3339(req.EndTime)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Invalid end_time"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	result, err := s.hal.ChargerAnalytics(ctx, req.requestedChargerID(), start, end)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

type checkInactivityRequest struct {
	UID string `json:"uid"`
}

func (s *Server) checkChargerInactivity(w http.ResponseWriter, r *http.Request) {
	var req checkInactivityRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.UID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Missing uid"})
		return
	}

	result := s.registry.CheckInactivity(req.UID, 120*time.Second)
	writeJSON(w, http.StatusOK, result)
}

func parseOptionalRFC3339(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, err
	}

	return &t, nil
}
