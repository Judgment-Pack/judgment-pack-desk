//go:build !linux

package codexbridge

import "os/exec"

// Other hosts require their own process and containment certification.
func prepareProcess(_ *exec.Cmd) error     { return ErrUnavailable }
func killProcessGroup(cmd *exec.Cmd) error { return cmd.Process.Kill() }
