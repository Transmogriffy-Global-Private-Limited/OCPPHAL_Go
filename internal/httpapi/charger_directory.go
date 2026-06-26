package httpapi

import "context"

type ChargerDirectory interface {
	IsKnownCharger(ctx context.Context, chargerID string) (bool, error)
	KnownChargerIDs(ctx context.Context) ([]string, error)
}

var chargerDirectory ChargerDirectory

func SetChargerDirectory(directory ChargerDirectory) {
	chargerDirectory = directory
}

func isKnownCharger(ctx context.Context, chargerID string) (bool, error) {
	if chargerDirectory == nil {
		return true, nil
	}
	return chargerDirectory.IsKnownCharger(ctx, chargerID)
}

func knownChargerIDs(ctx context.Context) ([]string, error) {
	if chargerDirectory == nil {
		return nil, nil
	}
	return chargerDirectory.KnownChargerIDs(ctx)
}
