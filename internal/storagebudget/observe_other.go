//go:build !linux

package storagebudget

import "os"

// Lock availability alone is not filesystem measurement certification.
func ObserveHeld(*os.Root, Filesystem) (Observation, error) { return Observation{}, ErrUnsupported }
