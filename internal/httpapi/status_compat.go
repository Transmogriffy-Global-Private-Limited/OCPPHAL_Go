package httpapi

import (
	"encoding/json"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/state"
)

const timeRFC3339NanoCompat = "2006-01-02T15:04:05.999999999Z07:00"

func legacyStatusPayload(cp *state.ChargerState, mode string, chargerID string) map[string]any {
	online := "Offline"
	if cp.Online && cp.HasError {
		online = "Online (with error)"
	} else if cp.Online {
		online = "Online"
	}

	payload := map[string]any{
		"status":                       cp.Status,
		"connectors":                   legacyConnectorPayload(cp.Connectors, mode),
		"online":                       online,
		"latest_message_received_time": cp.LastMessageTime.Format(timeRFC3339NanoCompat),
	}

	if mode == "all" {
		payload["last_message_received_time"] = cp.LastMessageTime.Format(timeRFC3339NanoCompat)
	}

	if mode == "specific" {
		payload["charger_id"] = chargerID
	}

	return payload
}

func legacyOfflineStatusPayload(mode string, chargerID string) map[string]any {
	payload := map[string]any{
		"status":                       "Offline",
		"connectors":                   map[string]any{},
		"online":                       "Offline",
		"latest_message_received_time": nil,
	}

	if mode == "all" {
		payload["last_message_received_time"] = nil
	}

	if mode == "specific" {
		payload["charger_id"] = chargerID
	}

	return payload
}

func legacyConnectorPayload(connectors any, mode string) map[string]any {
	raw, err := json.Marshal(connectors)
	if err != nil {
		return map[string]any{}
	}

	var decoded map[string]map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return map[string]any{}
	}

	out := make(map[string]any, len(decoded))

	for connectorID, connector := range decoded {
		item := make(map[string]any, len(connector)+6)

		for key, value := range connector {
			item[key] = value
		}

		lastMeter := firstPresent(connector, "last_meter_value", "latest_meter_value")
		lastConsumption := firstPresent(connector, "last_transaction_consumption_kwh", "latest_transaction_consumption_kwh")
		transactionID := firstPresent(connector, "transaction_id", "latest_transaction_id")

		item["last_meter_value"] = lastMeter
		item["latest_meter_value"] = lastMeter

		item["last_transaction_consumption_kwh"] = lastConsumption
		item["latest_transaction_consumption_kwh"] = lastConsumption

		item["transaction_id"] = transactionID
		item["latest_transaction_id"] = transactionID

		if _, ok := item["error_code"]; !ok {
			item["error_code"] = "NoError"
		}

		out[connectorID] = item
	}

	return out
}

func firstPresent(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}

	return nil
}
