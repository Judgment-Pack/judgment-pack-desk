//go:build !linux && !darwin

package desk

import (
	"errors"
	"os"
)

func checkMoveFilesystem(os.FileInfo, os.FileInfo) error {
	return errors.New("Safe project moves are not supported on this platform.")
}
func renameProjectNoReplace(*Server, string, string) error {
	return errors.New("Safe project moves are not supported on this platform.")
}
