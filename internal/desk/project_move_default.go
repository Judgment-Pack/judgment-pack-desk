package desk

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) defaultProjectDestination() string {
	name := filepath.Base(s.projectDir)
	if name == "." || name == string(filepath.Separator) {
		name = "project"
	}
	return filepath.Join(deskApplicationDataPath(), "projects", name)
}

func relativeMoveDestination(parent, destination string) string {
	// Both paths are absolute and parent is an ancestor selected below.
	relative, _ := filepath.Rel(parent, destination)
	return relative
}

// Review is read-only. Only the server's exact default destination may have
// missing parents; custom destinations keep their existing-parent contract.
func (s *Server) projectDestinationParent(destination string) (*ProjectRoot, error) {
	path := filepath.Dir(destination)
	for {
		if _, err := os.Lstat(path); err == nil {
			parent, err := OpenProjectRoot(path)
			if err != nil {
				return nil, err
			}
			if parent.dir != path {
				parent.Close()
				return nil, errors.New("Use the resolved destination path without symbolic links.")
			}
			return parent, nil
		} else if !errors.Is(err, os.ErrNotExist) || destination != s.defaultProjectDestination() || path == filepath.Dir(path) {
			return nil, errors.New("The destination parent folder must already exist and be accessible.")
		}
		path = filepath.Dir(path)
	}
}

// Confirmation reopens the reviewed ancestor and creates only the missing
// default parents, one entry at a time through pinned directory handles. An
// entry that appears after review is a conflict, never a merge or a symlink.
func prepareProjectDestination(preview projectMovePreview) (string, error) {
	if !preview.CreateParent {
		return preview.parentIdentity, nil
	}
	parent, err := OpenProjectRoot(preview.parentPath)
	if err != nil {
		return "", err
	}
	defer parent.Close()
	if parent.dir != preview.parentPath || identityKey(parent.info) != preview.parentIdentity {
		return "", errors.New("The destination parent changed. Review the move again.")
	}
	if err = safeMoveParent(parent.info); err != nil {
		return "", err
	}
	root, err := parent.own.root.OpenRoot(".")
	if err != nil {
		return "", err
	}
	defer func() { root.Close() }()
	path := preview.parentPath
	parts := strings.Split(relativeMoveDestination(path, filepath.Dir(preview.Destination)), string(filepath.Separator))
	for _, part := range parts {
		if err = root.Mkdir(part, 0700); err != nil {
			return "", errors.New("The destination parent could not be created or changed after review. Review the move again.")
		}
		if err = syncPrivateDirectory(root); err != nil {
			return "", err
		}
		next, _, err := openOwnFolder(root, []string{part}, false, "project folders")
		if err != nil {
			return "", err
		}
		root.Close()
		root = next
		path = filepath.Join(path, part)
		held, err := root.Stat(".")
		if err != nil || !sameDirectory(path, held) {
			return "", errors.New("The destination parent changed. Review the move again.")
		}
	}
	held, err := root.Stat(".")
	if err != nil {
		return "", err
	}
	return identityKey(held), nil
}
