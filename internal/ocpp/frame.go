package ocpp

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	MessageTypeCall       = 2
	MessageTypeCallResult = 3
	MessageTypeCallError  = 4
)

type Call struct {
	UniqueID string
	Action   string
	Payload  json.RawMessage
}

type CallResultFrame struct {
	UniqueID string
	Payload  json.RawMessage
}

type CallErrorFrame struct {
	UniqueID    string
	ErrorCode   string
	Description string
	Details     json.RawMessage
}

func MessageType(raw []byte) (int, error) {
	var frame []json.RawMessage
	if err := json.Unmarshal(raw, &frame); err != nil {
		return 0, err
	}

	if len(frame) == 0 {
		return 0, errors.New("empty OCPP frame")
	}

	var messageType int
	if err := json.Unmarshal(frame[0], &messageType); err != nil {
		return 0, err
	}

	return messageType, nil
}

func ParseCall(raw []byte) (*Call, error) {
	var frame []json.RawMessage
	if err := json.Unmarshal(raw, &frame); err != nil {
		return nil, err
	}

	if len(frame) != 4 {
		return nil, fmt.Errorf("expected OCPP CALL frame length 4, got %d", len(frame))
	}

	var messageType int
	if err := json.Unmarshal(frame[0], &messageType); err != nil {
		return nil, err
	}

	if messageType != MessageTypeCall {
		return nil, fmt.Errorf("expected OCPP message type 2 CALL, got %d", messageType)
	}

	var uniqueID string
	if err := json.Unmarshal(frame[1], &uniqueID); err != nil {
		return nil, err
	}
	if uniqueID == "" {
		return nil, errors.New("empty unique id")
	}

	var action string
	if err := json.Unmarshal(frame[2], &action); err != nil {
		return nil, err
	}
	if action == "" {
		return nil, errors.New("empty action")
	}

	return &Call{
		UniqueID: uniqueID,
		Action:   action,
		Payload:  frame[3],
	}, nil
}

func ParseCallResult(raw []byte) (*CallResultFrame, error) {
	var frame []json.RawMessage
	if err := json.Unmarshal(raw, &frame); err != nil {
		return nil, err
	}

	if len(frame) != 3 {
		return nil, fmt.Errorf("expected OCPP CALLRESULT frame length 3, got %d", len(frame))
	}

	var messageType int
	if err := json.Unmarshal(frame[0], &messageType); err != nil {
		return nil, err
	}

	if messageType != MessageTypeCallResult {
		return nil, fmt.Errorf("expected OCPP message type 3 CALLRESULT, got %d", messageType)
	}

	var uniqueID string
	if err := json.Unmarshal(frame[1], &uniqueID); err != nil {
		return nil, err
	}
	if uniqueID == "" {
		return nil, errors.New("empty unique id")
	}

	return &CallResultFrame{
		UniqueID: uniqueID,
		Payload:  frame[2],
	}, nil
}

func ParseCallError(raw []byte) (*CallErrorFrame, error) {
	var frame []json.RawMessage
	if err := json.Unmarshal(raw, &frame); err != nil {
		return nil, err
	}

	if len(frame) != 5 {
		return nil, fmt.Errorf("expected OCPP CALLERROR frame length 5, got %d", len(frame))
	}

	var messageType int
	if err := json.Unmarshal(frame[0], &messageType); err != nil {
		return nil, err
	}

	if messageType != MessageTypeCallError {
		return nil, fmt.Errorf("expected OCPP message type 4 CALLERROR, got %d", messageType)
	}

	var uniqueID string
	if err := json.Unmarshal(frame[1], &uniqueID); err != nil {
		return nil, err
	}

	var errorCode string
	if err := json.Unmarshal(frame[2], &errorCode); err != nil {
		return nil, err
	}

	var description string
	if err := json.Unmarshal(frame[3], &description); err != nil {
		return nil, err
	}

	return &CallErrorFrame{
		UniqueID:    uniqueID,
		ErrorCode:   errorCode,
		Description: description,
		Details:     frame[4],
	}, nil
}

func CallFrame(uniqueID string, action string, payload any) ([]byte, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	return json.Marshal([]any{MessageTypeCall, uniqueID, action, payload})
}

func CallResult(uniqueID string, payload any) ([]byte, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	return json.Marshal([]any{MessageTypeCallResult, uniqueID, payload})
}

func CallError(uniqueID string, code string, description string, details any) ([]byte, error) {
	if details == nil {
		details = map[string]any{}
	}
	return json.Marshal([]any{MessageTypeCallError, uniqueID, code, description, details})
}
