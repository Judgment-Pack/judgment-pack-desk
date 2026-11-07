//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import "syscall"

// privateWorkerFiles makes what the worker and the gateway it starts create
// owner-only. The gateway makes its store's folders 0755 and its files 0644
// under the inherited mask, inside the run folder Desk made 0700, and Desk's
// storage census refuses the installation over any folder or file in its
// config folder that is not private, so the next start was read-only. The
// worker is a process of its own that exists only to own one gateway, so the
// process-wide mask is the worker's alone.
func privateWorkerFiles() { syscall.Umask(0o077) }
