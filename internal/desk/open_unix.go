//go:build unix

package desk

import "syscall"

// openNonBlocking is added to the flags of every file the API opens.
//
// Opening a FIFO for reading blocks until a writer arrives, and the block
// happens inside `open(2)` — before any code can look at the mode and refuse.
// A handler that opened one would hang until the process died, and the type
// check placed after the open would never run. O_NONBLOCK makes the open
// return immediately, which is what lets the regular-file check be the thing
// that decides.
const openNonBlocking = syscall.O_NONBLOCK
