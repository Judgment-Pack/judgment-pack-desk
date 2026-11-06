package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const dialectDraftDir = ".desk-private/dialect-drafts"

func (s *Server) openDialectDrafts(create bool) (*os.Root, error) {
	if create && s.storageEnrollment != nil {
		if err := s.ensureDialectDraftEnrollment(context.Background()); err != nil {
			return nil, err
		}
		root, _, err := openOwnFolder(s.root, strings.Split(dialectDraftDir, "/"), false, "dialect drafts")
		return root, err
	}
	if create && s.storageReadOnly {
		return nil, errStorageEnrollmentUnresolved
	}
	root, made, err := openOwnFolder(s.root, strings.Split(dialectDraftDir, "/"), create, "dialect drafts")
	if err == nil && made {
		if err = storageCreateDraftIgnore(root); err != nil {
			root.Close()
		}
	}
	return root, err
}

// This local workflow lease orders promotion against edits/discard and chat
// retention. It is separate from short repository transactions. A cloud backend
// also needs distributed admission; CAS alone cannot replace this lease.
func (s *Server) lockDialectDraftWorkflow(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ensureDialectDraftEnrollment(ctx); err != nil {
		return nil, err
	}
	root, err := s.openDialectDrafts(s.storageEnrollment == nil)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var lock *os.File
	if s.storageEnrollment != nil {
		named, e := storageOpenFile(root, "workflow.lock", 0)
		lock, err = named.file, e
	} else {
		lock, err = storageCreateDraftWorkflowLock(root)
	}
	if err != nil {
		return nil, err
	}
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		lock.Close()
		return nil, draftLockError(ctx, err)
	}
	return func() { lock.Close() }, nil
}

// All cooperating creators enter the same actual draft-root window, including
// a configuration that found the directory already present. Existing content
// remains ordinary data; this lease is exclusion only and grants no Q promise.
func storageCreateDraftIgnore(root *os.Root) (err error) {
	lease, err := storageCreatorAcquire(root)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
	return writePrivateData(root, ".gitignore", []byte("*\n"))
}
func storageCreateDraftWorkflowLock(root *os.Root) (out *os.File, err error) {
	lease, err := storageCreatorAcquire(root)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, lease.Close())
		if err != nil && out != nil {
			err = errors.Join(err, out.Close())
			out = nil
		}
	}()
	file, err := root.OpenFile("workflow.lock", os.O_CREATE|os.O_RDWR|openNoFollow|openNonBlocking, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	named, e := root.Lstat("workflow.lock")
	if err != nil || e != nil || !storagePrivate(info, false) || info.Size() != 0 || !os.SameFile(info, named) {
		return nil, errors.Join(errStorageCustody, err, e, file.Close())
	}
	return file, nil
}

// Storage planning uses the owner's exact path derivation; it grants no read or
// creation authority and leaves the caller's original custody checks in place.
func dialectDraftStoragePath(project string) string {
	return filepath.Join(project, dialectDraftDir)
}

// The caller still validates the absolute path, admitted local kind and target.
// This is the owner's exact existing suffix relation, not creation authority.
func dialectDraftStoragePathSelected(path string) bool {
	return strings.HasSuffix(path, "/"+dialectDraftDir)
}
