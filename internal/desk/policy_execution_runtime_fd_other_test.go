//go:build !linux

package desk

import (
	"os"
	"testing"
)

func observeManualRuntimeFD(t *testing.T, _ string, _ *runnerExecutorOwnership, _ *os.File, _ []os.FileInfo) {
	t.Helper()
	t.Log("live /proc descriptor observation is Linux-only; actual owned-run fixture still executes")
}
