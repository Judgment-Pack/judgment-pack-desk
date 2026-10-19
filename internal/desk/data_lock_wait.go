package desk

import (
	"context"
	"os"
	"time"
)

const privateDataLockWait = 5 * time.Second

// waitPrivateDataLock serializes short storage transactions. Waiting happens
// before any read or mutation; it never retries an operation. Ownership and
// admission leases must continue to use the nonblocking lockPrivateData.
func waitPrivateDataLock(ctx context.Context, file *os.File, exclusive bool) error {
	ctx, cancel := context.WithTimeout(ctx, privateDataLockWait)
	defer cancel()
	delay := 5 * time.Millisecond
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := lockPrivateData(file, exclusive)
		if err == nil || !lockHeld(err) {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < 100*time.Millisecond {
			delay *= 2
		}
	}
}
