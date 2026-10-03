//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package desk

import (
	"context"
	"os"
	"time"
)

// lockAuditShared takes no lock on this build. The runtime takes LockFileEx's
// byte-range lock on Windows, and none on the other systems this build
// covers; Desk takes neither, so a download here is refused rather than made
// from a size that may fall inside a write.
func lockAuditShared(context.Context, *os.File, time.Duration) (func(), error) {
	return nil, errAuditNoLock
}
