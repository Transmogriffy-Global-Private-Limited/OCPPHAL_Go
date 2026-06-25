package ocpp16hal

import (
	"context"
	"time"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/store"
)

func (h *HAL) ChargerAnalytics(ctx context.Context, chargerID string, startTime *time.Time, endTime *time.Time) (*store.AnalyticsOutput, error) {
	return h.store.ChargerAnalytics(ctx, store.AnalyticsInput{
		ChargerID: chargerID,
		StartTime: startTime,
		EndTime:   endTime,
	})
}
