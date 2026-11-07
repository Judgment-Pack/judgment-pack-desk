//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package desk

// privateWorkerFiles has no process mask to set on this platform.
func privateWorkerFiles() {}
