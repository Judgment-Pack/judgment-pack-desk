//go:build linux || darwin

package desk

import (
	"net"
	"os"
	"syscall"
)

// Protect BOTH raw ends against every concurrent Go fork until CLOEXEC is set.
// ExtraFiles is the only operation that may intentionally pass the child end.
func jobsAuthoritySocket() (net.Conn, *os.File, error) {
	syscall.ForkLock.RLock()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err == nil {
		syscall.CloseOnExec(fds[0])
		syscall.CloseOnExec(fds[1])
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, nil, err
	}
	host := os.NewFile(uintptr(fds[0]), "manual-authority-host")
	child := os.NewFile(uintptr(fds[1]), "manual-authority-child")
	conn, err := net.FileConn(host)
	host.Close()
	if err != nil {
		child.Close()
		return nil, nil, err
	}
	return conn, child, nil
}
