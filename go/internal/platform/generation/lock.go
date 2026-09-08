package generation

import (
	"context"

	"gorm.io/gorm"
)

// AdmissionLockKey serializes generation admission with generation dispatch
// reservations. Keep this key in the shared package so worker claims and the
// durable dispatcher cannot accidentally use different advisory locks.
const AdmissionLockKey int64 = 42630001

// LockAdmission holds the transaction-scoped generation admission lock.
// Callers must already be inside a database transaction and keep the lock
// until their running/reservation decision has been written.
func LockAdmission(ctx context.Context, tx *gorm.DB) error {
	return tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(?)", AdmissionLockKey).Error
}
