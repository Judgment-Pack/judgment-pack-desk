//go:build unix

package desk

import (
	"io/fs"
	"os"
	"syscall"
)

// custodyChecked reports whether ownership can be inspected on this platform.
//
// It is what stops the custody argument being made in prose on a platform
// where it is not enforced: where this is false the store reports that it
// cannot establish ownership, rather than proceeding as though it had.
const custodyChecked = true

// ownerOf answers the owning user id of an already-stat'ed entry.
func ownerOf(info fs.FileInfo) (uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Uid, true
}

// openNoFollow refuses to traverse a symbolic link as the final component.
//
// `os.Root` confines a symlink to the root; it does **not** refuse one. A link
// planted inside the credential directory therefore still resolves, which is
// precisely the arrangement this exists to defeat: the kernel refuses the open
// outright, with no window between the check and it.
const openNoFollow = syscall.O_NOFOLLOW

// connectionRefused is the error a closed port answers with, matched by value
// rather than by the sentence around it: a message is a moving target across
// releases and platforms, and a diagnostic that branched on English would drift.
const connectionRefused = syscall.ECONNREFUSED

// effectiveUser is the user this process actually acts as.
//
// `Geteuid`, not `Getuid`: a setuid binary acts as its effective user, and it
// is the effective user whose ability to open a file decides whether the key
// is confined.
//
// A var rather than a plain function for one reason: a test substitutes it, so
// that "a directory owned by somebody else is refused" can be shown to hold
// without the suite needing a second user account or root. Nothing else writes
// it, and the ownership rule it feeds is asserted through it.
var effectiveUser = func() uint32 { return uint32(os.Geteuid()) }
