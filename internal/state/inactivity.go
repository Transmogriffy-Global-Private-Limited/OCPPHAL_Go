package state

import (
	"time"
)

type InactivityResult struct {
	ChargerID                 string  `json:"charger_id"`
	Found                     bool    `json:"found"`
	Inactive                  bool    `json:"inactive"`
	Online                    string  `json:"online"`
	Status                    string  `json:"status"`
	InactiveSeconds           float64 `json:"inactive_seconds"`
	InactivityLimitSeconds    float64 `json:"inactivity_limit_seconds"`
	LatestMessageReceivedTime *string `json:"latest_message_received_time"`
	Action                    string  `json:"action"`
}

func (r *Registry) CheckInactivity(chargerID string, limit time.Duration) InactivityResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	cp := r.chargers[chargerID]
	if cp == nil {
		return InactivityResult{
			ChargerID:              chargerID,
			Found:                  false,
			Inactive:               true,
			Online:                 "Offline",
			Status:                 "Offline",
			InactivityLimitSeconds: limit.Seconds(),
			Action:                 "charger_not_found",
		}
	}

	var latest *string
	if !cp.LastMessageTime.IsZero() {
		v := cp.LastMessageTime.Format(time.RFC3339Nano)
		latest = &v
	}

	inactiveSeconds := time.Since(cp.LastMessageTime).Seconds()
	if cp.LastMessageTime.IsZero() {
		inactiveSeconds = 0
	}

	online := "Offline"
	if cp.Online && cp.HasError {
		online = "Online (with error)"
	} else if cp.Online {
		online = "Online"
	}

	result := InactivityResult{
		ChargerID:                 chargerID,
		Found:                     true,
		Inactive:                  false,
		Online:                    online,
		Status:                    cp.Status,
		InactiveSeconds:           inactiveSeconds,
		InactivityLimitSeconds:    limit.Seconds(),
		LatestMessageReceivedTime: latest,
		Action:                    "no_change",
	}

	if cp.LastMessageTime.IsZero() {
		result.Action = "no_messages"
		return result
	}

	if cp.Online && inactiveSeconds >= limit.Seconds() {
		cp.Online = false
		cp.Status = "Offline"

		result.Inactive = true
		result.Online = "Offline"
		result.Status = "Offline"
		result.Action = "marked_offline"
		return result
	}

	if !cp.Online && inactiveSeconds >= limit.Seconds() {
		result.Inactive = true
		result.Online = "Offline"
		result.Action = "already_offline"
	}

	return result
}
