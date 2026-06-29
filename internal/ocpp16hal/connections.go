package ocpp16hal

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
)

type connectionTracker struct {
	mu     sync.Mutex
	next   uint64
	active map[string]connectionRecord
}

type connectionRecord struct {
	ChargerID   string
	Key         string
	Generation  uint64
	RemoteAddr  string
	ConnectedAt time.Time
}

func newConnectionTracker() *connectionTracker {
	return &connectionTracker{
		active: map[string]connectionRecord{},
	}
}

func (t *connectionTracker) register(chargerID string, key string, remoteAddr string) (connectionRecord, *connectionRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.next++

	record := connectionRecord{
		ChargerID:   chargerID,
		Key:         key,
		Generation:  t.next,
		RemoteAddr:  remoteAddr,
		ConnectedAt: time.Now().UTC(),
	}

	var previous *connectionRecord
	if old, ok := t.active[chargerID]; ok {
		oldCopy := old
		previous = &oldCopy
	}

	t.active[chargerID] = record
	return record, previous
}

func (t *connectionTracker) unregisterIfCurrent(chargerID string, key string) (bool, *connectionRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()

	current, ok := t.active[chargerID]
	if !ok {
		return false, nil
	}

	currentCopy := current

	if current.Key != key {
		return false, &currentCopy
	}

	delete(t.active, chargerID)
	return true, &currentCopy
}

func connectionKey(chargePoint ocpp16.ChargePointConnection) string {
	value := reflect.ValueOf(chargePoint)
	if value.IsValid() {
		if value.Kind() == reflect.Interface && !value.IsNil() {
			value = value.Elem()
		}

		switch value.Kind() {
		case reflect.Ptr, reflect.Chan, reflect.Func, reflect.Map, reflect.Slice, reflect.UnsafePointer:
			if !value.IsNil() {
				return fmt.Sprintf("%s:%x", value.Type().String(), value.Pointer())
			}
		}
	}

	return fmt.Sprintf("%s:%s", chargePoint.ID(), chargePoint.RemoteAddr())
}
