package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Private data uses the existing ownership rules, but owns no credential files.
// Walk through pinned parents and compare each opened directory with its entry.
func openPrivateDataRoot(dir string, create bool) (*os.Root, error) {
	if !custodyChecked || !filepath.IsAbs(dir) || strings.ContainsAny(dir, "\x00\r\n") {
		return nil, errors.New("chat storage requires an absolute path on a platform with file ownership checks")
	}
	parts := splitPath(dir)
	root, err := os.OpenRoot(parts[0])
	if err != nil {
		return nil, err
	}
	label := parts[0]
	for index, part := range parts[1:] {
		label = filepath.Join(label, part)
		info, err := root.Lstat(part)
		if errors.Is(err, os.ErrNotExist) && create {
			if err = root.Mkdir(part, custodyDirMode); err != nil && !errors.Is(err, os.ErrExist) {
				root.Close()
				return nil, err
			}
			if err = syncPrivateDirectory(root); err != nil {
				root.Close()
				return nil, err
			}
			info, err = root.Lstat(part)
		}
		last := index == len(parts)-2
		if err == nil {
			err = safeDirectory(label, info, last)
		}
		if err != nil {
			root.Close()
			return nil, err
		}
		if last && needsNarrowing(info.Mode()) {
			root.Close()
			return nil, fmt.Errorf("chat storage %s must be private (directory permissions 0700); choose a new folder or correct its permissions", label)
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			root.Close()
			return nil, err
		}
		opened, err := next.Stat(".")
		if err == nil && !os.SameFile(info, opened) {
			err = errors.New("chat storage changed while being opened")
		}
		if err == nil {
			err = safeDirectory(label, opened, last)
		}
		if err == nil && last && needsNarrowing(opened.Mode()) {
			err = errors.New("chat storage permissions changed while being opened")
		}
		root.Close()
		if err != nil {
			next.Close()
			return nil, err
		}
		root = next
	}
	if len(parts) < 2 {
		root.Close()
		return nil, errors.New("choose a private data folder, not the filesystem root")
	}
	return root, nil
}

func readPrivateData(root *os.Root, name string, limit int) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if err = ownerOnlyFile(name, info.Mode()); err != nil {
		return nil, withCode(CodeForbidden, err)
	}
	if err = ownedByUs(name, info); err != nil {
		return nil, withCode(CodeForbidden, err)
	}
	file, err := root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, withCode(CodeForbidden, errors.New("private data changed while being opened"))
	}
	if err = ownerOnlyFile(name, opened.Mode()); err != nil {
		return nil, withCode(CodeForbidden, err)
	}
	if err = ownedByUs(name, opened); err != nil {
		return nil, withCode(CodeForbidden, err)
	}
	data, err := readBounded(file, limit)
	if err != nil {
		return nil, withCode(CodeTooLarge, fmt.Errorf("%s exceeds its %d-byte storage limit", name, limit))
	}
	return data, nil
}

func writePrivateData(root *os.Root, name string, data []byte) error {
	file, stage, err := newDataStage(root)
	if err != nil {
		return err
	}
	defer root.Remove(stage)
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Chmod(custodyFileMode); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = root.Rename(stage, name); err != nil {
		return err
	}
	if err = syncPrivateDirectory(root); err != nil {
		return err
	}
	read, err := readPrivateData(root, name, len(data))
	if err != nil {
		return err
	}
	if !bytes.Equal(read, data) {
		return withCode(CodeStale, errors.New("private data changed after writing; reload before continuing"))
	}
	return nil
}

func decodeDataJSON(data []byte, into any) error {
	if !validUTF8(data) {
		return errors.New("private storage settings must be UTF-8")
	}
	if _, _, err := topLevelMembers(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("expected one JSON document")
	}
	return nil
}

// The lock file is stable and never renamed or removed. Every cooperating Desk
// sharing this configuration root serializes storage changes through this inode.
func (s *Server) privateDataLock(exclusive bool) (*os.File, error) {
	return s.privateDataFileLock(".data.lock", exclusive)
}

func (s *Server) privateDataFileLock(name string, exclusive bool) (*os.File, error) {
	if !s.assistant.usable() {
		return nil, s.assistant.problem
	}
	file, err := s.assistant.root.OpenFile(name, os.O_CREATE|os.O_RDWR|openNoFollow|openNonBlocking, custodyFileMode)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil {
		err = ownerOnlyFile("private data lock", info.Mode())
	}
	if err == nil {
		err = ownedByUs("private data lock", info)
	}
	if err == nil {
		err = lockPrivateData(file, exclusive)
	}
	if err != nil {
		file.Close()
		return nil, withCode(CodeStale, fmt.Errorf("chat storage is busy or unavailable; finish active work and retry: %w", err))
	}
	return file, nil
}

func syncPrivateDirectory(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Claim an empty store once; a second configuration must never replace its owner.
func claimPrivateData(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|openNoFollow, custodyFileMode)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return syncPrivateDirectory(root)
}

func checkPrivateDataPath(store *chatDataStore) error {
	if store.legacy {
		return nil
	}
	current, err := openPrivateDataRoot(store.location.Path, false)
	if err != nil {
		return err
	}
	defer current.Close()
	before, err := store.root.Stat(".")
	if err != nil {
		return err
	}
	after, err := current.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) {
		return errors.New("chat storage folder changed during the operation")
	}
	return nil
}

func newDataStage(root *os.Root) (*os.File, string, error) {
	token, err := NewToken()
	if err != nil {
		return nil, "", err
	}
	name := ".data-" + token
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|openNoFollow, custodyFileMode)
	return file, name, err
}
