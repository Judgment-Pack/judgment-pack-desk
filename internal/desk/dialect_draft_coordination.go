package desk

import (
	"context"
	"os"
	"strings"
)

const dialectDraftDir = ".desk-private/dialect-drafts"

func (s *Server) openDialectDrafts(create bool) (*os.Root, error) {
	root, made, err := openOwnFolder(s.root, strings.Split(dialectDraftDir, "/"), create, "dialect drafts")
	if err == nil && made {
		if err = writePrivateData(root, ".gitignore", []byte("*\n")); err != nil {
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
	root, err := s.openDialectDrafts(true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	lock, err := root.OpenFile("workflow.lock", os.O_CREATE|os.O_RDWR|openNoFollow|openNonBlocking, 0600)
	if err != nil {
		return nil, err
	}
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		lock.Close()
		return nil, draftLockError(ctx, err)
	}
	return func() { lock.Close() }, nil
}
