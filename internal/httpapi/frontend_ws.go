package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var frontendWSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (s *Server) frontendWebSocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	uid := strings.TrimPrefix(r.URL.Path, "/frontend/ws/")
	uid = strings.Trim(uid, "/")

	if uid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"detail": "Missing uid",
		})
		return
	}

	conn, err := frontendWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("frontend websocket upgrade failed", "uid", uid, "error", err)
		return
	}
	defer conn.Close()

	s.logger.Info("frontend websocket connected", "uid", uid)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	send := func() bool {
		payload := s.frontendStatusPayload(uid)

		if err := conn.WriteJSON(payload); err != nil {
			s.logger.Info("frontend websocket disconnected", "uid", uid, "error", err)
			return false
		}

		return true
	}

	if !send() {
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}

func (s *Server) frontendStatusPayload(uid string) any {
	if uid == "all" || uid == "all_online" {
		all := s.registry.SnapshotAll()
		resp := make(map[string]any, len(all))

		for chargerID, cp := range all {
			if uid == "all_online" && !cp.Online {
				continue
			}

			resp[chargerID] = legacyStatusPayload(cp, uid, chargerID)
		}

		return resp
	}

	cp, ok := s.registry.Snapshot(uid)
	if !ok {
		return map[string]any{
			"status":                       "Offline",
			"connectors":                   map[string]any{},
			"online":                       "Offline",
			"latest_message_received_time": nil,
		}
	}

	return legacyStatusPayload(cp, "specific", uid)
}
