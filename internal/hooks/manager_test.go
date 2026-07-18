package hooks

import "testing"

func TestParseMaxKWhResponseAcceptsCMSFormats(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     float64
	}{
		{
			name:     "JSON number",
			response: `{"max_kwh":7.5}`,
			want:     7.5,
		},
		{
			name:     "quoted CMS decimal",
			response: `{"max_kwh":"7.50"}`,
			want:     7.5,
		},
		{
			name:     "quoted zero",
			response: `{"max_kwh":"0.00"}`,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMaxKWhResponse([]byte(tt.response))
			if err != nil {
				t.Fatalf("parse max_kwh response: %v", err)
			}
			if got != tt.want {
				t.Fatalf("max_kwh = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseMaxKWhResponseRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "missing",
			response: `{"message":"Charging started"}`,
		},
		{
			name:     "null",
			response: `{"max_kwh":null}`,
		},
		{
			name:     "non numeric string",
			response: `{"max_kwh":"not-a-number"}`,
		},
		{
			name:     "non finite string",
			response: `{"max_kwh":"NaN"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseMaxKWhResponse([]byte(tt.response)); err == nil {
				t.Fatal("expected invalid max_kwh response to be rejected")
			}
		})
	}
}
