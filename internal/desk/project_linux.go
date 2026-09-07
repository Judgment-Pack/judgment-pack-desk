//go:build linux

package desk

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// runtimeShellName is the POSIX shell the trampoline runs in.
//
// A variable so a test can point it at a name that is not there and see the
// refusal a host without a shell would get.
var runtimeShellName = "sh"

// lookRuntimeShell finds it. A variable for the same reason.
var lookRuntimeShell = exec.LookPath

// runtimeTrampoline changes into the project **through the inherited
// descriptor** and then becomes the runtime.
//
// # Why a shell at all
//
// Round 3 set `cmd.Dir` to `/proc/self/fd/N` on the *parent's* descriptor
// number, and it worked — because Go's current Linux implementation happens to
// `chdir` before it shuffles descriptors, and because a descriptor that is in
// no `ExtraFiles` happens to survive the fork. Round 4 was right that neither
// is the `os/exec` contract; this module supports Go 1.25 and newer, and an
// ordering change inside the standard library would give a failed launch or a
// child in the wrong directory.
//
// So nothing internal is relied on. What `os/exec` **does** promise is that
// `ExtraFiles[0]` is descriptor **3** in the child, so `3` is a number this
// desk knows rather than one it inferred, and the `cd` happens in the child
// after that promise has been kept.
//
//   - `cd /proc/self/fd/3/.` resolves through the inherited open description,
//     which names the pinned directory however it is called by then. The
//     trailing `/.` makes the kernel resolve the `/proc` symlink to the
//     directory itself.
//   - `exec 3<&-` closes the descriptor **before** the runtime is executed, so
//     what the runtime inherits is a working directory and not a capability.
//   - `exec "$0" "$@"` replaces the shell, so there is no extra process in the
//     tree and signals and exit status reach the runtime unchanged.
//
// The cost is a dependency on a POSIX `sh` being present, which is stated in
// the README and refused by name at spawn where it is not.
const runtimeTrampoline = `cd /proc/self/fd/3/. && exec 3<&- && exec "$0" "$@"`

// runtimeCommand builds the command that starts one `jpack mcp`.
//
// The runtime binary is resolved here rather than left to the shell so that a
// missing one is this desk's own sentence at the same place it always was.
func (s *Server) runtimeCommand(ctx context.Context) (*exec.Cmd, error) {
	dirFile, ok := s.projectDescriptor()
	if !ok {
		return nil, fmt.Errorf("this desk holds no descriptor for its project")
	}
	shell, err := lookRuntimeShell(runtimeShellName)
	if err != nil {
		return nil, fmt.Errorf(
			"no %s to change into the project with, so no runtime was started: %w",
			runtimeShellName, err)
	}
	binary, err := exec.LookPath(s.cfg.JpackBin)
	if err != nil {
		return nil, fmt.Errorf("the runtime %q could not be found: %w", s.cfg.JpackBin, err)
	}
	cmd := exec.CommandContext(ctx, shell, "-c", runtimeTrampoline, binary, "mcp")
	// **The documented contract**: this becomes descriptor 3 in the child,
	// with close-on-exec cleared for it there and nowhere else.
	cmd.ExtraFiles = []*os.File{dirFile}
	return cmd, nil
}

// aimAtTheProject has nothing left to do on Linux: the command already carries
// the descriptor, and the change of directory happens in the child.
func (s *Server) aimAtTheProject(*exec.Cmd) error { return nil }

// projectDescriptor is the pinned directory as an ordinary file.
func (s *Server) projectDescriptor() (*os.File, bool) {
	if s.project == nil || s.project.own == nil || s.project.own.dirFile == nil {
		return nil, false
	}
	return s.project.own.dirFile, true
}

// descriptorWorkingDir is a **path that resolves through this descriptor**
// rather than through the project's name.
//
// It is the watcher's answer — inotify takes a path — and this desk's own way
// of saying which directory a runtime would start in. The number here is this
// process's; the child gets its own, and gets it by the contract above.
//
// The trailing `/.` is load-bearing: `/proc/self/fd/N` is itself a symbolic
// link, and `filepath.WalkDir` does not follow one handed to it as the walk
// root — so the watcher would have installed no watches and refused to start.
func (p *ProjectRoot) descriptorWorkingDir() (string, bool) {
	if p == nil || p.own == nil || p.own.dirFile == nil {
		return "", false
	}
	return "/proc/self/fd/" + strconv.Itoa(int(p.own.dirFile.Fd())) + "/.", true
}

// sameDirectory reports whether a pathname still names the pinned directory.
//
// Unused on Linux — the descriptor answers there and nothing has to check —
// and declared here so the fallback has one spelling on every platform. See
// `project_other.go`.
func sameDirectory(path string, pinned os.FileInfo) bool {
	found, err := os.Stat(path)
	return err == nil && os.SameFile(found, pinned)
}
