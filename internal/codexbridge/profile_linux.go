package codexbridge

import (
	"os"
	"syscall"
)

const noFollow = syscall.O_NOFOLLOW

func privateInfo(info os.FileInfo, directory bool) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0077 != 0 {
		return false
	}
	if directory {
		return info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0700 == 0700
	}
	return info.Mode().IsRegular() && stat.Nlink == 1 && info.Mode().Perm()&0600 == 0600
}
func lockProfile(root *os.Root) (*os.File, error) {
	f, err := root.OpenFile("lease", os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, ErrProfile
	}
	info, err := f.Stat()
	if err != nil || !privateInfo(info, false) {
		f.Close()
		return nil, ErrProfile
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, ErrBusy
		}
		return nil, ErrProfile
	}
	return f, nil
}

func privateParent(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && info.IsDir() && info.Mode().Perm()&0022 == 0
}
