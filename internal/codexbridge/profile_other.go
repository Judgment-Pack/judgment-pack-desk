//go:build !linux

package codexbridge

import "os"

// Profile custody is certified only with the Linux process boundary.
const noFollow = 0

func privateInfo(_ os.FileInfo, _ bool) bool   { return false }
func lockProfile(_ *os.Root) (*os.File, error) { return nil, ErrUnavailable }

func privateParent(_ os.FileInfo) bool { return false }
