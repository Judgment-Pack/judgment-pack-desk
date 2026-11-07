package storagebudget

import (
	"os"
	"syscall"
	"time"
)

// ObserveHeld samples one already-validated root descriptor. It establishes no
// root ownership or reservation and does not scan file contents. B2 supplies the
// owner-verified filesystem identity/generation separately.
func ObserveHeld(root *os.Root, f Filesystem) (Observation, error) {
	o := Observation{Filesystem: f.ID, Generation: f.Generation}
	if root == nil || f.ID.zero() || f.Generation == 0 {
		return o, ErrAuthority
	}
	held, e := root.Open(".")
	if e != nil {
		return o, ErrUnknown
	}
	var stat syscall.Statfs_t
	e = syscall.Fstatfs(int(held.Fd()), &stat)
	ce := held.Close()
	if e != nil || ce != nil || stat.Bsize <= 0 {
		return o, ErrUnknown
	}
	block := uint64(stat.Bsize)
	if uint64(stat.Bavail) > ^uint64(0)/block {
		return o, ErrBounds
	}
	o.AvailableBytes = uint64(stat.Bavail) * block
	o.AvailableInodes = uint64(stat.Ffree)
	o.At = time.Now()
	o.Known = true
	return o, nil
}
