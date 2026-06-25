package store

import "time"

type AnalyticsInput struct {
	ChargerID string
	StartTime *time.Time
	EndTime   *time.Time
}

type ChargerAnalytics struct {
	ChargerID                string    `json:"charger_id"`
	Timestamp                time.Time `json:"timestamp"`
	TotalUptime              string    `json:"total_uptime"`
	UptimePercentage         float64   `json:"uptime_percentage"`
	TotalTransactions        int64     `json:"total_transactions"`
	TotalElectricityUsedKWh  float64   `json:"total_electricity_used_kwh"`
	OccupancyRatePercentage  float64   `json:"occupancy_rate_percentage"`
	AverageSessionDuration   string    `json:"average_session_duration"`
	PeakUsageTimes           string    `json:"peak_usage_times"`
	TotalTimeOccupied        string    `json:"total_time_occupied,omitempty"`
	TotalTimeOccupiedSeconds float64   `json:"-"`
	TotalPossibleSeconds     float64   `json:"-"`
	TotalUptimeSeconds       float64   `json:"-"`
}

type AnalyticsOutput struct {
	Analytics map[string]ChargerAnalytics `json:"analytics,omitempty"`

	TotalUptime             string  `json:"total_uptime,omitempty"`
	TotalTransactions       int64   `json:"total_transactions,omitempty"`
	TotalElectricityUsedKWh float64 `json:"total_electricity_used_kwh,omitempty"`
	TotalTimeOccupied       string  `json:"total_time_occupied,omitempty"`

	TotalTimeOccupiedSeconds float64 `json:"-"`
	TotalUptimeSeconds       float64 `json:"-"`
}
