package store

import (
	"context"
	"math"
	"sort"
	"strconv"
	"time"
)

func (s *MemoryStore) ChargerAnalytics(ctx context.Context, input AnalyticsInput) (*AnalyticsOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	chargerIDs := map[string]bool{}

	for _, tx := range s.transactions {
		if input.ChargerID == "" || input.ChargerID == "all" || input.ChargerID == "cumulative" || tx.ChargerID == input.ChargerID {
			chargerIDs[tx.ChargerID] = true
		}
	}

	if len(chargerIDs) == 0 && input.ChargerID != "" && input.ChargerID != "all" && input.ChargerID != "cumulative" {
		chargerIDs[input.ChargerID] = true
	}

	output := &AnalyticsOutput{
		Analytics: make(map[string]ChargerAnalytics),
	}

	for chargerID := range chargerIDs {
		analytics := computeMemoryAnalytics(chargerID, input, s.transactions)
		output.Analytics[chargerID] = analytics

		output.TotalTransactions += analytics.TotalTransactions
		output.TotalElectricityUsedKWh += analytics.TotalElectricityUsedKWh
		output.TotalTimeOccupiedSeconds += analytics.TotalTimeOccupiedSeconds
		output.TotalUptimeSeconds += analytics.TotalUptimeSeconds
	}

	output.TotalUptime = FormatDuration(output.TotalUptimeSeconds)
	output.TotalTimeOccupied = FormatDuration(output.TotalTimeOccupiedSeconds)

	if input.ChargerID == "cumulative" {
		output.Analytics = nil
	}

	return output, nil
}

func computeMemoryAnalytics(chargerID string, input AnalyticsInput, transactions map[int64]*Transaction) ChargerAnalytics {
	now := time.Now().UTC()
	start := input.StartTime
	end := input.EndTime

	if start == nil || end == nil {
		var minTime *time.Time
		var maxTime *time.Time

		for _, tx := range transactions {
			if tx.ChargerID != chargerID {
				continue
			}

			st := tx.StartTime
			if minTime == nil || st.Before(*minTime) {
				v := st
				minTime = &v
			}

			et := st
			if tx.StopTime != nil {
				et = *tx.StopTime
			}
			if maxTime == nil || et.After(*maxTime) {
				v := et
				maxTime = &v
			}
		}

		if start == nil {
			start = minTime
		}
		if end == nil {
			end = maxTime
		}
	}

	if start == nil {
		v := now
		start = &v
	}
	if end == nil {
		v := now
		end = &v
	}

	totalPossible := math.Max(0, end.Sub(*start).Seconds())
	peak := make([]int64, 24)

	var count int64
	var totalKWh float64
	var occupiedSeconds float64

	for _, tx := range transactions {
		if tx.ChargerID != chargerID {
			continue
		}
		if tx.StartTime.Before(*start) || tx.StartTime.After(*end) {
			continue
		}

		count++

		if tx.TotalConsumption != nil {
			totalKWh += *tx.TotalConsumption
		}

		if tx.StopTime != nil {
			occupiedSeconds += tx.StopTime.Sub(tx.StartTime).Seconds()
		}

		peak[tx.StartTime.Hour()]++
	}

	avgSeconds := 0.0
	if count > 0 {
		avgSeconds = occupiedSeconds / float64(count)
	}

	occupancy := 0.0
	if totalPossible > 0 && occupiedSeconds > 0 {
		occupancy = round3((occupiedSeconds / totalPossible) * 100)
	}

	return ChargerAnalytics{
		ChargerID:                chargerID,
		Timestamp:                now,
		TotalUptime:              FormatDuration(0),
		UptimePercentage:         0,
		TotalTransactions:        count,
		TotalElectricityUsedKWh:  totalKWh,
		OccupancyRatePercentage:  occupancy,
		AverageSessionDuration:   FormatDuration(avgSeconds),
		PeakUsageTimes:           PeakUsageTimes(peak),
		TotalTimeOccupied:        FormatDuration(occupiedSeconds),
		TotalTimeOccupiedSeconds: occupiedSeconds,
		TotalPossibleSeconds:     totalPossible,
		TotalUptimeSeconds:       0,
	}
}

func PeakUsageTimes(peak []int64) string {
	if len(peak) != 24 {
		return "No data available"
	}

	var maxCount int64
	for _, count := range peak {
		if count > maxCount {
			maxCount = count
		}
	}

	if maxCount == 0 {
		return "No peak usage times - charger was not used during this period."
	}

	hours := []string{}
	for hour, count := range peak {
		if count == maxCount {
			hours = append(hours, formatPeakHour(hour))
		}
	}

	sort.Strings(hours)

	out := ""
	for i, hour := range hours {
		if i > 0 {
			out += ", "
		}
		out += hour
	}
	return out
}

func formatPeakHour(hour int) string {
	return time.Date(2000, 1, 1, hour, 0, 0, 0, time.UTC).Format("15:04") + " - " +
		time.Date(2000, 1, 1, hour+1, 0, 0, 0, time.UTC).Format("15:04")
}

func FormatDuration(secondsFloat float64) string {
	seconds := int64(math.Round(secondsFloat))
	if seconds < 0 {
		seconds = 0
	}

	years := seconds / 31536000
	seconds %= 31536000

	days := seconds / 86400
	seconds %= 86400

	hours := seconds / 3600
	seconds %= 3600

	minutes := seconds / 60
	seconds %= 60

	parts := []struct {
		value int64
		name  string
	}{
		{years, "years"},
		{days, "days"},
		{hours, "hours"},
		{minutes, "minutes"},
		{seconds, "seconds"},
	}

	out := ""
	for _, part := range parts {
		if part.value <= 0 {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += strconvI64(part.value) + " " + part.name
	}

	if out == "" {
		return "0 seconds"
	}

	return out
}

func strconvI64(v int64) string {
	return strconv.FormatInt(v, 10)
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}
