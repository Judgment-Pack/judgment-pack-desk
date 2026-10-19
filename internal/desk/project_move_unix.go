//go:build linux || darwin

package desk

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func checkMoveFilesystem(source, destination os.FileInfo) error {
	a, aok := source.Sys().(*syscall.Stat_t)
	b, bok := destination.Sys().(*syscall.Stat_t)
	if !aok || !bok {
		return errors.New("This platform cannot safely move project folders.")
	}
	if a.Dev != b.Dev {
		return errors.New("Choose a destination on the same filesystem. Cross-device moves are not supported.")
	}
	return nil
}
func renameProjectNoReplace(s *Server, destination, parentIdentity string) error {
	source, err := OpenProjectRoot(filepath.Dir(s.projectDir))
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := OpenProjectRoot(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer target.Close()
	if err = safeMoveParent(source.info); err != nil {
		return err
	}
	if err = safeMoveParent(target.info); err != nil {
		return err
	}
	if identityKey(target.info) != parentIdentity {
		return errors.New("The destination parent changed. Review the move again.")
	}
	if source.dir != filepath.Dir(s.projectDir) || target.dir != filepath.Dir(destination) {
		return errors.New("A parent folder changed. Review the move again.")
	}
	before, err := source.own.root.Lstat(filepath.Base(s.projectDir))
	if err != nil {
		return err
	}
	held, err := s.root.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(before, held) {
		return errors.New("The source folder changed. Nothing was moved.")
	}
	if err = checkMoveFilesystem(held, target.info); err != nil {
		return err
	}
	// Pin both parents through the syscall. No os.Rename fallback: it would
	// replace a destination which appeared after Review move.
	if err = renameDirectoryNoReplace(source.own.dirFile, filepath.Base(s.projectDir), target.own.dirFile, filepath.Base(destination)); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("The destination appeared after review. Nothing was replaced; choose an unused folder.")
		}
		if errors.Is(err, syscall.EXDEV) {
			return errors.New("Choose a destination on the same filesystem. Cross-device moves are not supported.")
		}
		if errors.Is(err, syscall.ENOSYS) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EINVAL) {
			return errors.New("This filesystem does not support safe project moves. Nothing was moved.")
		}
		return err
	}
	// The rename has landed even if durability checks fail; set this BEFORE sync.
	s.markProjectMoved(destination)
	if err = source.own.dirFile.Sync(); err != nil {
		return err
	}
	if err = target.own.dirFile.Sync(); err != nil {
		return err
	}
	if !sameDirectory(destination, held) {
		return errors.New("The destination path changed after the move. Recovery information was kept.")
	}
	return nil
}
