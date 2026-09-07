//go:build !linux

package desk

import "os"

// descriptorWorkingDir has no answer off Linux.
//
// `/proc/self/fd` is Linux's. There is no portable way to give a subprocess a
// working directory by descriptor — `fchdir` applies to this process, and
// changing this process's directory to serve one relay would change it for
// every other — so the callers fall back to **checking the pathname
// immediately before each use** and refusing where it has moved. That is
// check-then-use and it is said to be: a rename in the window between the
// check and the `chdir` is not closed by it. The README says so too.
func (p *ProjectRoot) descriptorWorkingDir() (string, bool) { return "", false }

// sameDirectory reports whether a pathname still names the pinned directory.
//
// The fallback's whole content: one `Stat`, compared by identity against what
// was pinned. It is a function rather than two lines at each call site so that
// the rule has one spelling and one mutation row — the lesson `ownerOnlyFile`
// and `deskConfigUnmoved` both carry.
func sameDirectory(path string, pinned os.FileInfo) bool {
	found, err := os.Stat(path)
	return err == nil && os.SameFile(found, pinned)
}
