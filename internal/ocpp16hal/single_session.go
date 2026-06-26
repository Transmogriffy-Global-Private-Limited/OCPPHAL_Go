package ocpp16hal

import (
	"context"
	"sync"
)

type pendingRemoteStartKey struct {
	chargerID   string
	connectorID int
	idTag       string
}

var (
	pendingRemoteStartMu             sync.Mutex
	pendingRemoteStartSingleSessions = map[pendingRemoteStartKey]bool{}
)

func (h *HAL) RemoteStartTransactionWithOptions(
	ctx context.Context,
	chargerID string,
	idTag string,
	connectorID int,
	isSingleSession bool,
) (string, error) {
	if isSingleSession {
		h.recordPendingSingleSession(chargerID, connectorID, idTag)
	}

	status, err := h.RemoteStartTransaction(ctx, chargerID, idTag, connectorID)
	if err != nil && isSingleSession {
		h.clearPendingSingleSession(chargerID, connectorID, idTag)
		return "", err
	}

	return status, nil
}

func (h *HAL) recordPendingSingleSession(chargerID string, connectorID int, idTag string) {
	pendingRemoteStartMu.Lock()
	defer pendingRemoteStartMu.Unlock()

	pendingRemoteStartSingleSessions[pendingRemoteStartKey{
		chargerID:   chargerID,
		connectorID: connectorID,
		idTag:       idTag,
	}] = true
}

func (h *HAL) clearPendingSingleSession(chargerID string, connectorID int, idTag string) {
	pendingRemoteStartMu.Lock()
	defer pendingRemoteStartMu.Unlock()

	delete(pendingRemoteStartSingleSessions, pendingRemoteStartKey{
		chargerID:   chargerID,
		connectorID: connectorID,
		idTag:       idTag,
	})
}

func (h *HAL) consumePendingSingleSession(chargerID string, connectorID int, idTag string) bool {
	pendingRemoteStartMu.Lock()
	defer pendingRemoteStartMu.Unlock()

	key := pendingRemoteStartKey{
		chargerID:   chargerID,
		connectorID: connectorID,
		idTag:       idTag,
	}

	if pendingRemoteStartSingleSessions[key] {
		delete(pendingRemoteStartSingleSessions, key)
		return true
	}

	// Fallback for chargers that StartTransaction on connector 0/unknown despite remote-start connector.
	for candidate := range pendingRemoteStartSingleSessions {
		if candidate.chargerID == chargerID && candidate.idTag == idTag {
			delete(pendingRemoteStartSingleSessions, candidate)
			return true
		}
	}

	return false
}
