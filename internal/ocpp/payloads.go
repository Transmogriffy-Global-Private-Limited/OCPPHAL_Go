package ocpp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type StartTransactionPayload struct {
	ConnectorID int
	IDTag       string
	MeterStart  float64
}

type StopTransactionPayload struct {
	ConnectorID   *int
	TransactionID int64
	MeterStop     float64
}

type StatusNotificationPayload struct {
	ConnectorID   int
	Status        string
	ErrorCode     string
	TransactionID *int64
}

type MeterValuesPayload struct {
	ConnectorID   int
	TransactionID *int64
	MeterValueWh  *float64
}

func ParseStartTransactionPayload(raw json.RawMessage) (*StartTransactionPayload, error) {
	body, err := object(raw)
	if err != nil {
		return nil, err
	}

	connectorID, ok := intFrom(body, "connectorId", "connector_id")
	if !ok {
		return nil, fmt.Errorf("missing connectorId")
	}

	idTag := stringFrom(body, "idTag", "id_tag")
	if idTag == "" {
		return nil, fmt.Errorf("missing idTag")
	}

	meterStart, ok := floatFrom(body, "meterStart", "meter_start")
	if !ok {
		return nil, fmt.Errorf("missing meterStart")
	}

	return &StartTransactionPayload{
		ConnectorID: int(connectorID),
		IDTag:       idTag,
		MeterStart:  meterStart,
	}, nil
}

func ParseStopTransactionPayload(raw json.RawMessage) (*StopTransactionPayload, error) {
	body, err := object(raw)
	if err != nil {
		return nil, err
	}

	transactionID, ok := intFrom(body, "transactionId", "transaction_id")
	if !ok {
		return nil, fmt.Errorf("missing transactionId")
	}

	meterStop, ok := floatFrom(body, "meterStop", "meter_stop")
	if !ok {
		return nil, fmt.Errorf("missing meterStop")
	}

	var connectorID *int
	if value, ok := intFrom(body, "connectorId", "connector_id"); ok {
		v := int(value)
		connectorID = &v
	}

	return &StopTransactionPayload{
		ConnectorID:   connectorID,
		TransactionID: transactionID,
		MeterStop:     meterStop,
	}, nil
}

func ParseStatusNotificationPayload(raw json.RawMessage) (*StatusNotificationPayload, error) {
	body, err := object(raw)
	if err != nil {
		return nil, err
	}

	connectorID, ok := intFrom(body, "connectorId", "connector_id")
	if !ok {
		return nil, fmt.Errorf("missing connectorId")
	}

	status := stringFrom(body, "status")
	errorCode := stringFrom(body, "errorCode", "error_code")
	if errorCode == "" {
		errorCode = "NoError"
	}

	tx := intPtrFrom(body, "transactionId", "transaction_id")

	return &StatusNotificationPayload{
		ConnectorID:   int(connectorID),
		Status:        status,
		ErrorCode:     errorCode,
		TransactionID: tx,
	}, nil
}

func ParseMeterValuesPayload(raw json.RawMessage) (*MeterValuesPayload, error) {
	body, err := object(raw)
	if err != nil {
		return nil, err
	}

	connectorID, ok := intFrom(body, "connectorId", "connector_id")
	if !ok {
		return nil, fmt.Errorf("missing connectorId")
	}

	tx := intPtrFrom(body, "transactionId", "transaction_id")

	meterArrayRaw, ok := rawFrom(body, "meterValue", "meter_value")
	if !ok {
		return &MeterValuesPayload{
			ConnectorID:   int(connectorID),
			TransactionID: tx,
		}, nil
	}

	var meterEntries []map[string]json.RawMessage
	if err := json.Unmarshal(meterArrayRaw, &meterEntries); err != nil {
		return nil, fmt.Errorf("invalid meterValue: %w", err)
	}

	var chosen *float64

	for i := len(meterEntries) - 1; i >= 0; i-- {
		sampledRaw, ok := rawFrom(meterEntries[i], "sampledValue", "sampled_value")
		if !ok {
			continue
		}

		var sampledValues []map[string]json.RawMessage
		if err := json.Unmarshal(sampledRaw, &sampledValues); err != nil {
			return nil, fmt.Errorf("invalid sampledValue: %w", err)
		}

		if len(sampledValues) == 0 {
			continue
		}

		selected := sampledValues[0]

		for _, candidate := range sampledValues {
			measurand := stringFrom(candidate, "measurand")
			if strings.EqualFold(measurand, "Energy.Active.Import.Register") {
				selected = candidate
				break
			}
		}

		value, ok := floatFrom(selected, "value")
		if !ok {
			continue
		}

		unit := strings.ToLower(strings.TrimSpace(firstNonEmpty(
			stringFrom(selected, "unit"),
			stringFrom(selected, "unitOfMeasure"),
			stringFrom(selected, "unit_of_measure"),
		)))

		if unit == "kwh" || unit == "kilowatthour" {
			value *= 1000.0
		}

		valueCopy := value
		chosen = &valueCopy
		break
	}

	return &MeterValuesPayload{
		ConnectorID:   int(connectorID),
		TransactionID: tx,
		MeterValueWh:  chosen,
	}, nil
}

func object(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return body, nil
}

func rawFrom(body map[string]json.RawMessage, keys ...string) (json.RawMessage, bool) {
	for _, key := range keys {
		if raw, ok := body[key]; ok {
			return raw, true
		}
	}
	return nil, false
}

func stringFrom(body map[string]json.RawMessage, keys ...string) string {
	raw, ok := rawFrom(body, keys...)
	if !ok {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String()
	}

	return ""
}

func intFrom(body map[string]json.RawMessage, keys ...string) (int64, bool) {
	raw, ok := rawFrom(body, keys...)
	if !ok {
		return 0, false
	}

	var i int64
	if err := json.Unmarshal(raw, &i); err == nil {
		return i, true
	}

	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int64(f), true
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		parsed, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err == nil {
			return parsed, true
		}
	}

	return 0, false
}

func intPtrFrom(body map[string]json.RawMessage, keys ...string) *int64 {
	value, ok := intFrom(body, keys...)
	if !ok {
		return nil
	}
	return &value
}

func floatFrom(body map[string]json.RawMessage, keys ...string) (float64, bool) {
	raw, ok := rawFrom(body, keys...)
	if !ok {
		return 0, false
	}

	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, true
	}

	var i int64
	if err := json.Unmarshal(raw, &i); err == nil {
		return float64(i), true
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err == nil {
			return parsed, true
		}
	}

	return 0, false
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.EqualFold(strings.TrimSpace(string(raw)), "null")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
