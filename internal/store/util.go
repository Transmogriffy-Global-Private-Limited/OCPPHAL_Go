package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	mathrand "math/rand"
	"time"
)

const maxTransactionID = int64(2147483647)

func NewUUIDString() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80

		return fmt.Sprintf(
			"%s-%s-%s-%s-%s",
			hex.EncodeToString(b[0:4]),
			hex.EncodeToString(b[4:6]),
			hex.EncodeToString(b[6:8]),
			hex.EncodeToString(b[8:10]),
			hex.EncodeToString(b[10:16]),
		)
	}

	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), mathrand.Int63())
}

func RandomTransactionID() int64 {
	return mathrand.Int63n(maxTransactionID) + 1
}

func DeltaWh(previous float64, current float64) float64 {
	const rollover = 4294967295.0

	delta := current - previous
	if delta >= 0 {
		return delta
	}

	return current + (rollover - previous) + 1
}

func floatPtr(v float64) *float64 { return &v }
