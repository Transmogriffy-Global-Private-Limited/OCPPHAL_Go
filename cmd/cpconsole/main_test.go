package main

import (
	"testing"

	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
)

func TestParseStatusIsCaseInsensitive(t *testing.T) {
	got, ok := parseStatus("suspendedevse")
	if !ok || got != core.ChargePointStatusSuspendedEVSE {
		t.Fatalf("parseStatus() = %q, %t", got, ok)
	}
}

func TestParseErrorCodeRejectsUnknownCode(t *testing.T) {
	if _, ok := parseErrorCode("DefinitelyNotOCPP"); ok {
		t.Fatal("parseErrorCode accepted an unknown code")
	}
}

func TestParseStopReason(t *testing.T) {
	got, ok := parseStopReason("evdisconnected")
	if !ok || got != core.ReasonEVDisconnected {
		t.Fatalf("parseStopReason() = %q, %t", got, ok)
	}
}

func TestParseBoolPolicy(t *testing.T) {
	if value, ok := parseBoolPolicy("ACCEPT", "accept", "reject"); !ok || !value {
		t.Fatalf("parseBoolPolicy accept = %t, %t", value, ok)
	}
	if value, ok := parseBoolPolicy("reject", "accept", "reject"); !ok || value {
		t.Fatalf("parseBoolPolicy reject = %t, %t", value, ok)
	}
}

func TestNormalizeWebSocketURL(t *testing.T) {
	got, err := normalizeWebSocketURL(" wss://ocpp.example.com/base/ ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://ocpp.example.com/base" {
		t.Fatalf("normalizeWebSocketURL() = %q", got)
	}
	if _, err := normalizeWebSocketURL("https://ocpp.example.com"); err == nil {
		t.Fatal("normalizeWebSocketURL accepted an HTTP URL")
	}
}
