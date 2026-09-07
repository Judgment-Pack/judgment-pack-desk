//go:build linux

package desk

import (
	"os"
	"strconv"
)

// descriptorWorkingDir is a **path that resolves through this descriptor**
// rather than through the project's name.
//
// # Why `/proc/self/fd`, and why it is not a pathname step
//
// Two consumers of the project cannot be made to go through `os.Root`: a
// subprocess's working directory, which the kernel sets with `chdir`, and an
// inotify watch, which the kernel takes by path. Round 3 found both still
// addressed by the resolved *spelling* — so a rename-and-replace at that
// spelling left every new `jpack mcp` judging one tree while the file API
// edited another, which is the whole property the pinned descriptor was
// supposed to establish.
//
// `/proc/self/fd/N` is the way out on Linux: the kernel resolves it to the
// **open file description**, not to a name, so it names the directory this
// desk pinned however that directory is called afterwards or whether it is
// called anything at all.
//
// # The subprocess case, and why nothing is inherited
//
// `os/exec` applies `Dir` with a `chdir` in the forked child, and it does so
// **before** the descriptor shuffle — so the child's table is still a copy of
// this process's at that moment and `N` means here what it means there. The
// descriptor is close-on-exec and is not listed in `ExtraFiles`, so the child
// changes into the directory and then the descriptor is gone: the runtime
// inherits a working directory and not a capability.
// # The trailing `/.`, which is not decoration
//
// `/proc/self/fd/N` is itself a **symbolic link**, and `filepath.WalkDir` does
// not follow one handed to it as the walk root — so the file watcher, which
// walks its root to install inotify watches, would have installed none and
// refused to start. A trailing `/.` makes the kernel resolve the link and
// answer the directory, at both use sites: `chdir` takes it, `Lstat` takes it,
// and `filepath.Rel` and `filepath.Join` clean it away.
func (p *ProjectRoot) descriptorWorkingDir() (string, bool) {
	if p == nil || p.own == nil || p.own.dirFile == nil {
		return "", false
	}
	return "/proc/self/fd/" + strconv.Itoa(int(p.own.dirFile.Fd())) + "/.", true
}

// sameDirectory reports whether a pathname still names the pinned directory.
//
// Unused on Linux — `descriptorWorkingDir` answers there and nothing has to
// check — and declared here so the fallback below has one spelling on every
// platform. See `project_other.go`.
func sameDirectory(path string, pinned os.FileInfo) bool {
	found, err := os.Stat(path)
	return err == nil && os.SameFile(found, pinned)
}
