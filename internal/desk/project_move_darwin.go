package desk

import (
	"golang.org/x/sys/unix"
	"os"
)

func renameDirectoryNoReplace(source *os.File, from string, target *os.File, to string) error {
	return unix.RenameatxNp(int(source.Fd()), from, int(target.Fd()), to, unix.RENAME_EXCL)
}
