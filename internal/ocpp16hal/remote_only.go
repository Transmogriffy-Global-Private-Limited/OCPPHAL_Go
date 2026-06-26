package ocpp16hal

import (
	"context"
	"strings"
	"sync"
	"time"
)

var remoteOnlySyncOnce sync.Map

func (h *HAL) scheduleRemoteOnlyConfigSync(chargePointID string) {
	if _, loaded := remoteOnlySyncOnce.LoadOrStore(chargePointID, true); loaded {
		return
	}

	go func() {
		time.Sleep(10 * time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		h.EnforceRemoteOnlyMode(ctx, chargePointID)
	}()
}

func (h *HAL) EnforceRemoteOnlyMode(ctx context.Context, chargePointID string) {
	desired := map[string]string{
		"HeartbeatInterval":          "15",
		"MeterValueSampleInterval":   "15",
		"AuthorizeRemoteTxRequests":  "true",
		"LocalAuthorizeOffline":      "false",
		"LocalPreAuthorize":          "false",
		"AuthorizationCacheEnabled":  "false",
		"AllowOfflineTxForUnknownId": "false",
		"StopTransactionOnInvalidId": "true",
		"ChargePointAuthEnable":      "true",
		"FreevendEnabled":            "false",
	}

	keys := make([]string, 0, len(desired))
	for key := range desired {
		keys = append(keys, key)
	}

	conf, err := h.GetConfiguration(ctx, chargePointID, keys)
	if err != nil {
		h.logger.Warn("remote-only config sync get_configuration failed", "charge_point_id", chargePointID, "error", err)
		return
	}

	current := map[string]string{}
	for _, item := range conf.ConfigurationKey {
		if item.Value == nil {
			current[item.Key] = ""
			continue
		}
		current[item.Key] = *item.Value
	}

	for key, want := range desired {
		got, ok := current[key]
		if !ok {
			h.logger.Debug("remote-only config key not present on charger", "charge_point_id", chargePointID, "key", key)
			continue
		}

		if strings.TrimSpace(got) == want {
			continue
		}

		status, err := h.ChangeConfiguration(ctx, chargePointID, key, want)
		if err != nil {
			h.logger.Warn("remote-only config change failed", "charge_point_id", chargePointID, "key", key, "value", want, "error", err)
			continue
		}

		h.logger.Info("remote-only config change sent", "charge_point_id", chargePointID, "key", key, "from", got, "to", want, "status", status)
	}
}
