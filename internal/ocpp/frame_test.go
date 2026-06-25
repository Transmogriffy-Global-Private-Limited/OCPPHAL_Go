package ocpp

import "testing"

func TestParseCall(t *testing.T) {
raw := []byte(`[2,"abc123","Heartbeat",{}]`)

call, err := ParseCall(raw)
if err != nil {
t.Fatal(err)
}

if call.UniqueID != "abc123" {
t.Fatalf("UniqueID = %q, want %q", call.UniqueID, "abc123")
}

if call.Action != "Heartbeat" {
t.Fatalf("Action = %q, want %q", call.Action, "Heartbeat")
}
}

func TestCallResult(t *testing.T) {
raw, err := CallResult("abc123", map[string]string{"status": "Accepted"})
if err != nil {
t.Fatal(err)
}

want := `[3,"abc123",{"status":"Accepted"}]`
if string(raw) != want {
t.Fatalf("CallResult = %s, want %s", raw, want)
}
}
