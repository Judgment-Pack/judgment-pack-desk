package desk

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
)

// Bound once to an authorized workspace; methods take logical identities, never
// paths or credentials. Empty revision is create-only. Failed/uncertain writes
// return no successful snapshot and are not retried. Publication is a separate
// workflow: never call another store or provider from a retrying transaction.
type dialectDraftRepository interface {
	List(context.Context) ([]dialectDraftSummary, error)
	Get(context.Context, string) (dialectDraftReply, error)
	CompareAndSwap(context.Context, dialectDraftDocument, string) (dialectDraftReply, error)
}
type dialectDraftSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	UpdatedAt string `json:"updatedAt"`
}

var (
	errDialectDraftConflict = errors.New("dialect draft revision changed")
	errDialectDraftBusy     = errors.New("dialect draft storage busy")
	errDialectDraftCapacity = errors.New("Dialect draft storage is full. Export or archive this workspace before creating more drafts.")
)

const dialectDraftActiveLimit = 256
const dialectDraftTotalLimit = 4096

func (s *Server) draftRepository() dialectDraftRepository {
	return s.draftRepositoryWithAuthority(nil)
}

// Storage binding belongs to the repository owner. The callback checks only
// current authority and must never reacquire conversation/private-data locks.
// File adapters check after queued locks and immediately before writes; injected
// adapters keep the same pre/post observation fence without an implicit retry.
func (s *Server) draftRepositoryWithAuthority(authorize func(context.Context) error) dialectDraftRepository {
	if s.dialectDrafts != nil {
		if authorize == nil {
			return s.dialectDrafts
		}
		return authorizedDialectDraftRepository{s.dialectDrafts, authorize}
	}
	return fileDialectDraftRepository{open: s.openDialectDrafts, authorize: authorize}
}

type authorizedDialectDraftRepository struct {
	repository dialectDraftRepository
	authorize  func(context.Context) error
}

func (a authorizedDialectDraftRepository) List(ctx context.Context) ([]dialectDraftSummary, error) {
	if err := a.authorize(ctx); err != nil {
		return nil, err
	}
	out, err := a.repository.List(ctx)
	if err != nil {
		return nil, err
	}
	if err = a.authorize(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
func (a authorizedDialectDraftRepository) Get(ctx context.Context, id string) (dialectDraftReply, error) {
	if err := a.authorize(ctx); err != nil {
		return dialectDraftReply{}, err
	}
	out, err := a.repository.Get(ctx, id)
	if err != nil {
		return dialectDraftReply{}, err
	}
	if err = a.authorize(ctx); err != nil {
		return dialectDraftReply{}, err
	}
	return out, nil
}
func (a authorizedDialectDraftRepository) CompareAndSwap(ctx context.Context, d dialectDraftDocument, revision string) (dialectDraftReply, error) {
	if err := a.authorize(ctx); err != nil {
		return dialectDraftReply{}, err
	}
	out, err := a.repository.CompareAndSwap(ctx, d, revision)
	if err != nil {
		return dialectDraftReply{}, err
	}
	if err = a.authorize(ctx); err != nil {
		return dialectDraftReply{}, err
	}
	return out, nil
}
func draftLockError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.Join(errDialectDraftBusy, err)
}
func decodeDialectDraft(id string, raw []byte) (dialectDraftReply, error) {
	var result dialectDraftReply
	if len(raw) > dialectDraftLimit {
		return result, errors.New("The dialect draft is too large.")
	}
	if err := decodeDataJSON(raw, &result.Document); err != nil {
		return dialectDraftReply{}, err
	}
	d := result.Document
	if d.Version != 1 || d.ID != id || !dialectDraftID.MatchString(id) || d.SavedInputID != "" && (d.SavedInputID != dialectDraftPreparationID(d) || d.Discarded) {
		return dialectDraftReply{}, errors.New("Invalid dialect draft.")
	}
	if d.Origin != nil && (!dialectDraftKey.MatchString(d.Origin.ChatID) || !dialectDraftKey.MatchString(d.Origin.DraftID) || id != dialectDraftIdentity("", d.Origin)) {
		return dialectDraftReply{}, errors.New("Invalid dialect draft origin.")
	}
	if err := validateDialectDraft(d.Name, d.Recipe); err != nil {
		return dialectDraftReply{}, err
	}
	result.SHA256 = digestOf(raw)
	return result, nil
}
func encodeDialectDraft(d dialectDraftDocument) ([]byte, dialectDraftReply, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, dialectDraftReply{}, err
	}
	snapshot, err := decodeDialectDraft(d.ID, raw)
	return raw, snapshot, err
}
func validateDialectDraftTransition(before, next dialectDraftDocument) error {
	bad := errors.New("Retained draft identity or completed publication cannot be replaced.")
	if before.ID != next.ID || before.Version != next.Version || before.CreatedAt != next.CreatedAt || !sameDraftValue(before.Origin, next.Origin) {
		return bad
	}
	if before.Discarded || before.SavedInputID != "" {
		if !sameDraftValue(before, next) {
			return bad
		}
	}
	if next.Discarded || next.SavedInputID != "" {
		if before.Name != next.Name || !sameDraftValue(before.Recipe, next.Recipe) {
			return bad
		}
	}
	return nil
}
func sameDraftValue(a, b any) bool {
	x, e := json.Marshal(a)
	y, f := json.Marshal(b)
	return e == nil && f == nil && string(x) == string(y)
}
func draftSummaries(docs []dialectDraftDocument) []dialectDraftSummary {
	items := []dialectDraftSummary{}
	for _, d := range docs {
		if !d.Discarded && d.SavedInputID == "" {
			items = append(items, dialectDraftSummary{d.ID, d.Name, d.UpdatedAt})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt == items[j].UpdatedAt {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	return items
}
func draftCapacity(docs []dialectDraftDocument) error {
	active := 0
	for _, d := range docs {
		if !d.Discarded && d.SavedInputID == "" {
			active++
		}
	}
	if active >= dialectDraftActiveLimit || len(docs) >= dialectDraftTotalLimit {
		return errDialectDraftCapacity
	}
	return nil
}
