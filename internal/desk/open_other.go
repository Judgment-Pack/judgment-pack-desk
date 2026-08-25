//go:build !unix

package desk

// openNonBlocking is zero where there are no FIFOs to block on. See
// open_unix.go for what it is for.
const openNonBlocking = 0
