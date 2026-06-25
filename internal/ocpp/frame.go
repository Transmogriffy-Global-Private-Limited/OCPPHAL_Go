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

func CallResult(uniqueID string, payload any) ([]byte, error) {
return json.Marshal([]any{MessageTypeCallResult, uniqueID, payload})
}

func CallError(uniqueID string, code string, description string, details any) ([]byte, error) {
if details == nil {
details = map[string]any{}
}
return json.Marshal([]any{MessageTypeCallError, uniqueID, code, description, details})
}
