//go:build !linux

package desk

import (
	"context"
	"os"
	"os/exec"
)

// runtimeCommand builds the command that starts one `jpack mcp`.
//
// Plain, because there is nothing here to build it around: the working
// directory is set by `aimAtTheProject` immediately before the spawn.
func (s *Server) runtimeCommand(ctx context.Context) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, s.cfg.JpackBin, "mcp"), nil
}

// aimAtTheProject re-verifies the pathname and points the command at it.
//
// **Check-then-use, called last.** There is no portable way to hand a
// subprocess a working directory by descriptor — `fchdir` applies to this
// process, and changing this process's directory to serve one relay would
// change it for every other — so the pathname is checked by identity and a
// moved project refuses the relay rather than starting a runtime somewhere
// else. The window between this and the child's `chdir` is not closed by it,
// and the README says so.
func (s *Server) aimAtTheProject(cmd *exec.Cmd) error {
	dir, err := runtimeWorkingDirByPathname(s.projectDir, s.project.info)
	if err != nil {
		return err
	}
	cmd.Dir = dir
	return nil
}

// descriptorWorkingDir has no answer off Linux. `/proc/self/fd` is Linux's.
func (p *ProjectRoot) descriptorWorkingDir() (string, bool) { return "", false }

// sameDirectory reports whether a pathname still names the pinned directory.
//
// The fallback's whole content: one `Stat`, compared by identity against what
// was pinned. It is a function rather than two lines at the call site so that
// the rule has one spelling and one mutation row — the lesson `ownerOnlyFile`
// and `deskConfigUnmoved` both carry.
func sameDirectory(path string, pinned os.FileInfo) bool {
	found, err := os.Stat(path)
	return err == nil && os.SameFile(found, pinned)
}
