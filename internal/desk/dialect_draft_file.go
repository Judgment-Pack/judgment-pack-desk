package desk

import (
	"context"
	"errors"
	"os"
	"strings"
)

type fileDialectDraftRepository struct {
	open func(bool) (*os.Root, error)
	// Current authority only; never reacquires the conversation/private lock.
	authorize func(context.Context) error
}

// File transactions use the existing directory lock and bounded wait. The
// workflow lease is a different file; neither is a recursive storage transaction.
func (f fileDialectDraftRepository) locked(ctx context.Context, write bool, use func(*os.Root) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := f.open(write)
	if err != nil {
		return err
	}
	defer root.Close()
	lock, err := root.Open(".")
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = waitPrivateDataLock(ctx, lock, write); err != nil {
		return draftLockError(ctx, err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if f.authorize != nil {
		if err = f.authorize(ctx); err != nil {
			return err
		}
	}
	if err = use(root); err != nil {
		return err
	}
	if f.authorize != nil {
		return f.authorize(ctx)
	}
	return nil
}
func readDialectDraft(root *os.Root, id string) (dialectDraftReply, error) {
	raw, err := readPrivateData(root, id+".json", dialectDraftLimit)
	if err != nil {
		return dialectDraftReply{}, err
	}
	return decodeDialectDraft(id, raw)
}
func readDialectDrafts(ctx context.Context, root *os.Root) ([]dialectDraftDocument, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	docs := []dialectDraftDocument{}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !strings.HasSuffix(entry.Name(), ".json") || !dialectDraftID.MatchString(id) {
			continue
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		saved, err := readDialectDraft(root, id)
		if err != nil {
			return nil, err
		}
		docs = append(docs, saved.Document)
	}
	return docs, nil
}
func (f fileDialectDraftRepository) List(ctx context.Context) ([]dialectDraftSummary, error) {
	var docs []dialectDraftDocument
	missingRoot := false
	// Missing root is empty; a missing file observed during listing is unavailable.
	open := f.open
	f.open = func(create bool) (*os.Root, error) {
		root, err := open(create)
		missingRoot = errors.Is(err, os.ErrNotExist)
		return root, err
	}
	err := f.locked(ctx, false, func(root *os.Root) error { var err error; docs, err = readDialectDrafts(ctx, root); return err })
	if missingRoot {
		return []dialectDraftSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	return draftSummaries(docs), nil
}
func (f fileDialectDraftRepository) Get(ctx context.Context, id string) (saved dialectDraftReply, err error) {
	if !dialectDraftID.MatchString(id) {
		return saved, errors.New("Invalid dialect draft identity.")
	}
	err = f.locked(ctx, false, func(root *os.Root) error { var err error; saved, err = readDialectDraft(root, id); return err })
	if err != nil {
		return dialectDraftReply{}, err
	}
	return saved, nil
}
func (f fileDialectDraftRepository) CompareAndSwap(ctx context.Context, d dialectDraftDocument, revision string) (saved dialectDraftReply, err error) {
	raw, candidate, err := encodeDialectDraft(d)
	if err != nil {
		return saved, err
	}
	err = f.locked(ctx, true, func(root *os.Root) error {
		before, e := readDialectDraft(root, d.ID)
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if e == nil && (revision == "" || revision != before.SHA256) || errors.Is(e, os.ErrNotExist) && revision != "" {
			return errDialectDraftConflict
		}
		if e == nil {
			if e = validateDialectDraftTransition(before.Document, candidate.Document); e != nil {
				return e
			}
		} else {
			docs, e := readDialectDrafts(ctx, root)
			if e != nil {
				return e
			}
			if e = draftCapacity(docs); e != nil {
				return e
			}
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if f.authorize != nil {
			if e = f.authorize(ctx); e != nil {
				return e
			}
		}
		if e = writePrivateData(root, d.ID+".json", raw); e != nil {
			return e
		}
		saved = candidate
		return nil
	})
	if err != nil {
		return dialectDraftReply{}, err
	}
	return saved, nil
}

// captureRetentionDrafts runs under the caller's original project lease.
// It enumerates only this owner's existing folder, preserving checked absence.
func (s *Server) captureRetentionDrafts(ctx context.Context, c *storageRetentionCapture, o *storageBudgetOwner) (err error) {
	drafts, absent, e := storageRetentionOwnFolder(s.root, strings.Split(dialectDraftDir, "/"), "dialect drafts", o, "draft-ancestor-close")
	if e != nil {
		return e
	}
	if absent {
		c.Sources = append(c.Sources, storageRetentionSource{ID: digestOf([]byte("dialect-drafts")), Kind: "dialect-drafts", Revision: digestOf(nil), Complete: true, Reasons: []string{}, References: []storageRetentionReference{}})
		return nil
	}
	defer func() { err = errors.Join(err, drafts.Close()) }()
	lock, e := drafts.Open(".")
	if e != nil {
		return e
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	if e = lockPrivateData(lock, false); e != nil {
		return e
	}
	entries, e := storageRetentionEntries(drafts, o, "draft-entries-close")
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if entry.Name() == ".gitignore" {
			continue
		}
		raw, _, e := storageRetentionRead(drafts, entry.Name(), min(dialectDraftLimit, int(maxBackupData-c.Total)))
		if e != nil {
			return e
		}
		if e = c.charge(raw); e != nil {
			return e
		}
		c.Sources = append(c.Sources, storageRetentionUnknown("draft:"+entry.Name(), "dialect-draft", raw, "draft-recipe-origin-and-publication-closure-not-joined"))
	}
	return ctx.Err()
}
