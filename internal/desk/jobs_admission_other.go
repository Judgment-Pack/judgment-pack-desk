//go:build !linux && !darwin

package desk

import (
	"net"
	"os"
)

func jobsAuthoritySocket() (net.Conn, *os.File, error) { return nil, nil, policyMachineRefusal() }
