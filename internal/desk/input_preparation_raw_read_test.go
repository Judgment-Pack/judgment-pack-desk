package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func preparationRawFixtureBytes(t *testing.T) (string, []byte) {
	t.Helper()
	doc := repositoryDocument(81)
	doc.Records[0].Values["count"] = "9007199254740993"
	raw, err := json.MarshalIndent(doc, "", "  ")
	storageTestMust(t, err)
	raw = append([]byte(" \n"), append(raw, '\n', '\t')...)
	return doc.ID, raw
}
func preparationRawFileFixture(t *testing.T, id string, raw []byte) (*Server, fileInputPreparationRepository, string) {
	t.Helper()
	dir := t.TempDir()
	storageTestMust(t, os.MkdirAll(filepath.Join(dir, inputPreparationDir), 0700))
	if raw != nil {
		storageTestMust(t, os.WriteFile(filepath.Join(dir, inputPreparationDir, id+".json"), raw, 0600))
	}
	root, err := os.OpenRoot(dir)
	storageTestMust(t, err)
	t.Cleanup(func() { storageTestMust(t, root.Close()) })
	s := &Server{root: root}
	return s, s.preparationRepository().(fileInputPreparationRepository), dir
}

func TestInputPreparationRawFileView(t *testing.T) {
	for _, name := range []string{"exact-format-and-large-decimal", "detached-bytes", "stale-revision", "missing-record", "corrupt-record", "oversize-record", "cancelled-view", "closed-view", "writer-exclusion"} {
		t.Run(name, func(t *testing.T) {
			id, raw := preparationRawFixtureBytes(t)
			_, repo, dir := preparationRawFileFixture(t, id, raw)
			if name == "missing-record" {
				storageTestMust(t, os.Remove(filepath.Join(dir, inputPreparationDir, id+".json")))
			}
			if name == "corrupt-record" {
				storageTestMust(t, os.WriteFile(filepath.Join(dir, inputPreparationDir, id+".json"), []byte("{broken"), 0600))
			}
			if name == "oversize-record" {
				storageTestMust(t, os.Truncate(filepath.Join(dir, inputPreparationDir, id+".json"), inputPreparationLimit+1))
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			view, err := tryInputPreparationRawRead(ctx, repo)
			storageTestMust(t, err)
			defer view.Close()
			if name == "cancelled-view" {
				cancel()
			}
			if name == "closed-view" {
				storageTestMust(t, view.Close())
			}
			revision := digestOf(raw)
			if name == "stale-revision" {
				revision = strings.Repeat("0", 64)
			}
			got, err := readInputPreparationRaw(ctx, view, id, revision)
			switch name {
			case "missing-record":
				if !errors.Is(err, os.ErrNotExist) || got.Bytes != nil {
					t.Fatal("missing bytes were supplied", got, err)
				}
			case "corrupt-record", "oversize-record":
				if err == nil || got.Bytes != nil {
					t.Fatal("invalid source accepted", got, err)
				}
			case "stale-revision":
				if !errors.Is(err, errInputPreparationConflict) || got.Bytes != nil {
					t.Fatal("changed revision accepted", got, err)
				}
			case "cancelled-view":
				if !errors.Is(err, context.Canceled) || got.Bytes != nil {
					t.Fatal("cancelled read accepted", got, err)
				}
			case "closed-view":
				if !errors.Is(err, errInputPreparationReadClosed) || got.Bytes != nil {
					t.Fatal("closed read accepted", got, err)
				}
			default:
				storageTestMust(t, err)
				if got.ID != id || got.Revision != digestOf(raw) || !bytes.Equal(got.Bytes, raw) {
					t.Fatal("original bytes/revision changed")
				}
				if name == "detached-bytes" {
					got.Bytes[0] = 'x'
					again, e := readInputPreparationRaw(ctx, view, id, revision)
					storageTestMust(t, e)
					if !bytes.Equal(again.Bytes, raw) {
						t.Fatal("caller mutation reached original")
					}
				}
				if name == "writer-exclusion" {
					tx, e := repo.openTransaction(ctx, true, false)
					if tx != nil {
						tx.Close()
						t.Fatal("writer entered guarded raw view")
					}
					if !errors.Is(e, errInputPreparationBusy) {
						t.Fatal(e)
					}
					storageTestMust(t, view.Close())
					tx, e = repo.openTransaction(ctx, true, false)
					storageTestMust(t, e)
					storageTestMust(t, tx.Close())
				}
			}
			if name != "closed-view" {
				storageTestMust(t, view.Close())
			}
		})
	}
}

func TestInputPreparationRawCheckedOpening(t *testing.T) {
	for _, name := range []string{"missing-store", "cancelled-before-open", "ancestor-close", "missing-child-close", "file-close", "view-close"} {
		t.Run(name, func(t *testing.T) {
			id, raw := preparationRawFixtureBytes(t)
			s, repo, dir := preparationRawFileFixture(t, id, raw)
			owner := &storageBudgetOwner{}
			phase := ""
			switch name {
			case "missing-store":
				storageTestMust(t, os.RemoveAll(filepath.Join(dir, inputPreparationDir)))
			case "ancestor-close":
				phase = "preparation-ancestor-close"
			case "missing-child-close":
				phase = "preparation-ancestor-close"
				storageTestMust(t, os.RemoveAll(filepath.Join(dir, inputPreparationDir)))
			case "file-close":
				phase = "preparation-file-close"
			case "view-close":
				phase = "preparation-view-close"
			}
			if phase != "" {
				owner.fault = &storageBudgetFault{Name: "retention-read", Phase: phase}
			}
			// Exercise the same checked opener as the default adapter with only its
			// existing test-fault target supplied; no startup/native authority is forged.
			repo.rawOwner = owner
			repo.rawOpen = func() (*os.Root, bool, error) {
				return storageRetentionOwnFolder(s.root, strings.Split(inputPreparationDir, "/"), "input preparations", owner, "preparation-ancestor-close")
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if name == "cancelled-before-open" {
				cancel()
			}
			view, err := tryInputPreparationRawRead(ctx, repo)
			switch name {
			case "missing-store":
				if view != nil || !errors.Is(err, errInputPreparationStoreMissing) || !errors.Is(err, os.ErrNotExist) {
					t.Fatal(view, err)
				}
				if _, e := os.Stat(filepath.Join(dir, inputPreparationDir)); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("read created store", e)
				}
			case "cancelled-before-open":
				if view != nil || !errors.Is(err, context.Canceled) {
					t.Fatal(view, err)
				}
			case "ancestor-close", "missing-child-close":
				if view != nil || !errors.Is(err, errStorageOwnerRecovery) || errors.Is(err, errInputPreparationStoreMissing) || owner.fault != nil {
					t.Fatal("cleanup failure became absence", view, err)
				}
				if name == "missing-child-close" && !errors.Is(err, os.ErrNotExist) {
					t.Fatal("missing operand lost", err)
				}
			default:
				storageTestMust(t, err)
				got, e := readInputPreparationRaw(ctx, view, id, digestOf(raw))
				if name == "file-close" {
					if got.Bytes != nil || !errors.Is(e, errStorageOwnerRecovery) || owner.fault != nil {
						t.Fatal("file Close failure hidden", got, e)
					}
				} else {
					storageTestMust(t, e)
				}
				e = view.Close()
				if name == "view-close" {
					if !errors.Is(e, errStorageOwnerRecovery) || owner.fault != nil {
						t.Fatal("view Close failure hidden", e)
					}
				} else {
					storageTestMust(t, e)
				}
				// The real descriptors are released even when their checked result fails.
				tx, e := repo.openTransaction(t.Context(), true, false)
				storageTestMust(t, e)
				storageTestMust(t, tx.Close())
				if _, e = view.GetRaw(id); !errors.Is(e, errInputPreparationReadClosed) {
					t.Fatal("closed view survived", e)
				}
			}
			if _, e := s.root.Stat("."); e != nil {
				t.Fatal("borrowed project closed", e)
			}
		})
	}
}

// This optional test provider reuses the existing memory repository's SAME
// acquired read view and exclusion. It does not serialize its typed Get.
type memoryRawPreparationRepository struct{ *memoryPreparationRepository }

func (m *memoryRawPreparationRepository) TryRawRead(ctx context.Context) (inputPreparationRawRead, error) {
	v, err := m.TryRead(ctx)
	if err != nil {
		return nil, err
	}
	return &memoryRawPreparationRead{memoryPreparationRead: v.(*memoryPreparationRead)}, nil
}

type memoryRawPreparationRead struct{ *memoryPreparationRead }

func (v *memoryRawPreparationRead) GetRaw(id string) (inputPreparationRawSnapshot, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return inputPreparationRawSnapshot{}, errInputPreparationReadClosed
	}
	if err := v.ctx.Err(); err != nil {
		return inputPreparationRawSnapshot{}, err
	}
	raw, ok := v.owner.data[id]
	if !ok {
		return inputPreparationRawSnapshot{}, os.ErrNotExist
	}
	return validateInputPreparationRaw(id, "", inputPreparationRawSnapshot{ID: id, Revision: digestOf(raw), Bytes: raw})
}
func TestInputPreparationInjectedRawCapability(t *testing.T) {
	for _, name := range []string{"unsupported-no-fallback", "exact-and-detached", "same-view-exclusion", "cancelled", "closed"} {
		t.Run(name, func(t *testing.T) {
			id, raw := preparationRawFixtureBytes(t)
			memory := &memoryPreparationRepository{data: map[string][]byte{id: raw}}
			if name == "unsupported-no-fallback" {
				view, err := tryInputPreparationRawRead(t.Context(), memory)
				if view != nil || !errors.Is(err, errInputPreparationRawUnavailable) {
					t.Fatal(view, err)
				}
				return
			}
			repo := &memoryRawPreparationRepository{memory}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			view, err := tryInputPreparationRawRead(ctx, repo)
			storageTestMust(t, err)
			defer view.Close()
			if name == "same-view-exclusion" {
				if e := memory.acquire(ctx, false); !errors.Is(e, errInputPreparationBusy) {
					if e == nil {
						memory.mu.Unlock()
					}
					t.Fatal("injected writer entered", e)
				}
			}
			if name == "cancelled" {
				cancel()
			}
			if name == "closed" {
				storageTestMust(t, view.Close())
			}
			got, err := readInputPreparationRaw(ctx, view, id, digestOf(raw))
			if name == "cancelled" {
				if !errors.Is(err, context.Canceled) || got.Bytes != nil {
					t.Fatal(got, err)
				}
			} else if name == "closed" {
				if !errors.Is(err, errInputPreparationReadClosed) || got.Bytes != nil {
					t.Fatal(got, err)
				}
			} else {
				storageTestMust(t, err)
				if !bytes.Equal(raw, got.Bytes) || got.Revision != digestOf(raw) {
					t.Fatal("injected raw changed")
				}
				got.Bytes[0] = 'x'
				if digestOf(memory.data[id]) != got.Revision {
					t.Fatal("injected caller alias")
				}
			}
			storageTestMust(t, view.Close())
			storageTestMust(t, memory.acquire(t.Context(), false))
			memory.mu.Unlock()
		})
	}
}

type preparationRawResultView struct {
	raw                     inputPreparationRawSnapshot
	err, closeErr           error
	gets, typedGets, closes int
}

func (v *preparationRawResultView) Get(string) (inputPreparationReply, error) {
	v.typedGets++
	return inputPreparationReply{}, os.ErrInvalid
}
func (v *preparationRawResultView) GetRaw(string) (inputPreparationRawSnapshot, error) {
	v.gets++
	return v.raw, v.err
}
func (v *preparationRawResultView) Close() error { v.closes++; return v.closeErr }

type preparationRawResultRepository struct {
	inputPreparationRepository
	view   inputPreparationRawRead
	err    error
	cancel context.CancelFunc
}

func (r preparationRawResultRepository) TryRawRead(context.Context) (inputPreparationRawRead, error) {
	if r.cancel != nil {
		r.cancel()
	}
	return r.view, r.err
}
func TestInputPreparationRawProviderRefusals(t *testing.T) {
	for _, name := range []string{"wrong-id", "wrong-digest", "corrupt", "oversize", "stale", "invalid-request-id", "invalid-request-revision", "provider-error", "nil-view", "acquire-error-close", "cancel-after-acquire"} {
		t.Run(name, func(t *testing.T) {
			id, raw := preparationRawFixtureBytes(t)
			v := &preparationRawResultView{raw: inputPreparationRawSnapshot{ID: id, Revision: digestOf(raw), Bytes: raw}}
			requestID, revision := id, digestOf(raw)
			switch name {
			case "wrong-id":
				v.raw.ID = repositoryDocument(82).ID
			case "wrong-digest":
				v.raw.Revision = strings.Repeat("0", 64)
			case "corrupt":
				v.raw.Bytes = []byte("null")
				v.raw.Revision = digestOf(v.raw.Bytes)
				revision = ""
			case "oversize":
				v.raw.Bytes = make([]byte, inputPreparationLimit+1)
				v.raw.Revision = digestOf(v.raw.Bytes)
				revision = ""
			case "stale":
				revision = strings.Repeat("0", 64)
			case "invalid-request-id":
				requestID = "../collection"
			case "invalid-request-revision":
				revision = "bad"
			case "provider-error":
				v.err = os.ErrPermission
			}
			if name == "nil-view" || name == "acquire-error-close" || name == "cancel-after-acquire" {
				repo := preparationRawResultRepository{view: v}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if name == "nil-view" {
					repo.view = nil
				}
				if name == "acquire-error-close" {
					repo.err = os.ErrPermission
					v.closeErr = os.ErrInvalid
				}
				if name == "cancel-after-acquire" {
					repo.cancel = cancel
				}
				view, err := tryInputPreparationRawRead(ctx, repo)
				if view != nil || err == nil {
					t.Fatal("invalid acquisition accepted", view, err)
				}
				if name == "nil-view" {
					if !errors.Is(err, errInputPreparationRawUnavailable) {
						t.Fatal(err)
					}
				} else {
					if v.closes != 1 {
						t.Fatal("failed acquisition did not close once", v.closes)
					}
					if name == "acquire-error-close" && (!errors.Is(err, os.ErrPermission) || !errors.Is(err, os.ErrInvalid)) {
						t.Fatal("Close/original error lost", err)
					}
					if name == "cancel-after-acquire" && !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
				}
				return
			}
			got, err := readInputPreparationRaw(t.Context(), v, requestID, revision)
			if err == nil || got.Bytes != nil || v.typedGets != 0 {
				t.Fatal("provider refusal bypassed", got, err, v.typedGets)
			}
			if name == "invalid-request-id" || name == "invalid-request-revision" {
				if v.gets != 0 {
					t.Fatal("invalid request reached provider")
				}
			} else if v.gets != 1 {
				t.Fatal("provider call repeated", v.gets)
			}
			if name == "stale" && !errors.Is(err, errInputPreparationConflict) {
				t.Fatal(err)
			}
			if name == "provider-error" && !errors.Is(err, os.ErrPermission) {
				t.Fatal(err)
			}
		})
	}
}

func TestRetentionDraftOwnerCapture(t *testing.T) {
	for _, name := range []string{"exact-charged-and-protected", "missing-not-created", "ancestor-close", "entries-close"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			root, err := os.OpenRoot(dir)
			storageTestMust(t, err)
			defer root.Close()
			s := &Server{root: root}
			owner := &storageBudgetOwner{}
			doc := repositoryDraft(83)
			raw, err := json.MarshalIndent(doc, "", "  ")
			storageTestMust(t, err)
			if name != "missing-not-created" {
				storageTestMust(t, os.MkdirAll(filepath.Join(dir, dialectDraftDir), 0700))
				storageTestMust(t, os.WriteFile(filepath.Join(dir, dialectDraftDir, doc.ID+".json"), raw, 0600))
			}
			if name == "ancestor-close" || name == "entries-close" {
				owner.fault = &storageBudgetFault{Name: "retention-read", Phase: "draft-" + strings.TrimSuffix(name, "-close") + "-close"}
			}
			var capture storageRetentionCapture
			err = s.captureRetentionDrafts(t.Context(), &capture, owner)
			if name == "ancestor-close" || name == "entries-close" {
				if !errors.Is(err, errStorageOwnerRecovery) || owner.fault != nil {
					t.Fatal("owner Close error hidden", err)
				}
				return
			}
			storageTestMust(t, err)
			if len(capture.Sources) != 1 {
				t.Fatal(capture.Sources)
			}
			if name == "missing-not-created" {
				if !capture.Sources[0].Complete || capture.Total != 0 {
					t.Fatal(capture)
				}
				if _, e := os.Stat(filepath.Join(dir, dialectDraftDir)); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("capture created store", e)
				}
			} else {
				if capture.Total != uint64(len(raw)) || capture.Sources[0].Complete || capture.Sources[0].Revision != digestOf(raw) {
					t.Fatal("draft bytes relabeled as closed references", capture)
				}
				got, e := os.ReadFile(filepath.Join(dir, dialectDraftDir, doc.ID+".json"))
				storageTestMust(t, e)
				if !bytes.Equal(got, raw) {
					t.Fatal("draft bytes changed")
				}
			}
		})
	}
}
func TestDialectDraftOwnerStoragePath(t *testing.T) {
	for _, tc := range []struct{ name, path string }{{"plain", "project"}, {"spaces", "project with spaces"}, {"trailing-separator", "project/"}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dialectDraftStoragePath(tc.path); got != filepath.Join(tc.path, ".desk-private", "dialect-drafts") {
				t.Fatal(got)
			}
		})
	}
}
