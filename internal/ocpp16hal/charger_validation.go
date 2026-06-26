package ocpp16hal

import (
	"context"
	"log/slog"
	"net/http"
)

type ChargerDirectory interface {
	IsKnownCharger(ctx context.Context, chargerID string) (bool, error)
}

var chargerDirectory ChargerDirectory
var chargerDirectoryLogger *slog.Logger

func SetChargerDirectory(directory ChargerDirectory, logger *slog.Logger) {
	chargerDirectory = directory
	chargerDirectoryLogger = logger
}

func validateIncomingCharger(chargePointID string, r *http.Request) bool {
	if chargerDirectory == nil {
		return true
	}

	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}

	ok, err := chargerDirectory.IsKnownCharger(ctx, chargePointID)
	if err != nil {
		if chargerDirectoryLogger != nil {
			chargerDirectoryLogger.Warn("charger directory validation failed", "charge_point_id", chargePointID, "error", err)
		}
		return false
	}

	if !ok && chargerDirectoryLogger != nil {
		chargerDirectoryLogger.Warn("rejected unknown charger", "charge_point_id", chargePointID)
	}

	return ok
}
