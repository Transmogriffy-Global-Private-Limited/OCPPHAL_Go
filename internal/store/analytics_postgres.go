package store

import (
	"context"
	"database/sql"
	"time"
)

func (s *PostgresStore) ChargerAnalytics(ctx context.Context, input AnalyticsInput) (*AnalyticsOutput, error) {
	chargerIDs, err := s.analyticsChargerIDs(ctx, input.ChargerID)
	if err != nil {
		return nil, err
	}

	if len(chargerIDs) == 0 && input.ChargerID != "" && input.ChargerID != "all" && input.ChargerID != "cumulative" {
		chargerIDs = []string{input.ChargerID}
	}

	output := &AnalyticsOutput{
		Analytics: make(map[string]ChargerAnalytics),
	}

	for _, chargerID := range chargerIDs {
		analytics, err := s.analyticsForCharger(ctx, chargerID, input.StartTime, input.EndTime)
		if err != nil {
			return nil, err
		}

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

func (s *PostgresStore) analyticsChargerIDs(ctx context.Context, chargerID string) ([]string, error) {
	if chargerID != "" && chargerID != "all" && chargerID != "cumulative" {
		return []string{chargerID}, nil
	}

	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT charger_id FROM transactions ORDER BY charger_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

func (s *PostgresStore) analyticsForCharger(ctx context.Context, chargerID string, inputStart *time.Time, inputEnd *time.Time) (ChargerAnalytics, error) {
	now := time.Now().UTC()

	start, end, err := s.analyticsWindow(ctx, chargerID, inputStart, inputEnd)
	if err != nil {
		return ChargerAnalytics{}, err
	}

	if start == nil {
		start = &now
	}
	if end == nil {
		end = &now
	}

	totalPossible := end.Sub(*start).Seconds()
	if totalPossible < 0 {
		totalPossible = 0
	}

	var totalTransactions sql.NullInt64
	var totalKWh sql.NullFloat64
	var occupiedSeconds sql.NullFloat64

	err = s.db.QueryRowContext(
		ctx,
		`SELECT
COUNT(*),
COALESCE(SUM(total_consumption), 0),
COALESCE(SUM(
CASE
WHEN stop_time IS NOT NULL THEN EXTRACT(EPOCH FROM (stop_time - start_time))
ELSE 0
END
), 0)
 FROM transactions
 WHERE charger_id = $1
   AND start_time >= $2
   AND start_time <= $3`,
		chargerID,
		*start,
		*end,
	).Scan(&totalTransactions, &totalKWh, &occupiedSeconds)
	if err != nil {
		return ChargerAnalytics{}, err
	}

	peak := make([]int64, 24)

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT EXTRACT(HOUR FROM start_time)::INT AS hour, COUNT(*)
 FROM transactions
 WHERE charger_id = $1
   AND start_time >= $2
   AND start_time <= $3
 GROUP BY hour`,
		chargerID,
		*start,
		*end,
	)
	if err != nil {
		return ChargerAnalytics{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var hour int
		var count int64
		if err := rows.Scan(&hour, &count); err != nil {
			return ChargerAnalytics{}, err
		}
		if hour >= 0 && hour < 24 {
			peak[hour] = count
		}
	}
	if err := rows.Err(); err != nil {
		return ChargerAnalytics{}, err
	}

	count := totalTransactions.Int64
	totalEnergy := totalKWh.Float64
	occupied := occupiedSeconds.Float64

	avgSeconds := 0.0
	if count > 0 {
		avgSeconds = occupied / float64(count)
	}

	occupancy := 0.0
	if totalPossible > 0 && occupied > 0 {
		occupancy = round3((occupied / totalPossible) * 100)
	}

	return ChargerAnalytics{
		ChargerID:                chargerID,
		Timestamp:                now,
		TotalUptime:              FormatDuration(0),
		UptimePercentage:         0,
		TotalTransactions:        count,
		TotalElectricityUsedKWh:  totalEnergy,
		OccupancyRatePercentage:  occupancy,
		AverageSessionDuration:   FormatDuration(avgSeconds),
		PeakUsageTimes:           PeakUsageTimes(peak),
		TotalTimeOccupied:        FormatDuration(occupied),
		TotalTimeOccupiedSeconds: occupied,
		TotalPossibleSeconds:     totalPossible,
		TotalUptimeSeconds:       0,
	}, nil
}

func (s *PostgresStore) analyticsWindow(ctx context.Context, chargerID string, inputStart *time.Time, inputEnd *time.Time) (*time.Time, *time.Time, error) {
	start := inputStart
	end := inputEnd

	if start != nil && end != nil {
		return start, end, nil
	}

	var minTime sql.NullTime
	var maxTime sql.NullTime

	err := s.db.QueryRowContext(
		ctx,
		`SELECT MIN(start_time), MAX(COALESCE(stop_time, start_time))
 FROM transactions
 WHERE charger_id = $1`,
		chargerID,
	).Scan(&minTime, &maxTime)
	if err != nil {
		return nil, nil, err
	}

	if start == nil && minTime.Valid {
		v := minTime.Time
		start = &v
	}
	if end == nil && maxTime.Valid {
		v := maxTime.Time
		end = &v
	}

	return start, end, nil
}
