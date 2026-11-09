package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// This provisional file-source slice must not silently substitute disk for an
// injected collection or journal owner. Those owners need their own actual
// bounded-byte/nonblocking source custody, not an arbitrary detached Reply.
var errAssistantDialectCompleteSourceOwner = errors.New("owned complete Dialect source reading is unavailable")

type assistantDialectCompleteFile struct {
	root      *os.Root // borrowed selected parent
	file      *os.File
	info      os.FileInfo
	name      string
	raw       []byte
	reference retainedArtifactRef
	limit     int
}

func openAssistantDialectCompleteFile(ctx context.Context, root *os.Root, name string, limit int) (_ *assistantDialectCompleteFile, err error) {
	file, err := openDialectArtifactOwnerFile(root, name)
	if err != nil {
		return nil, err
	}
	f := &assistantDialectCompleteFile{root: root, file: file, name: name, limit: limit}
	defer func() {
		if err != nil {
			err = errors.Join(err, closeDialectArtifactOwner(ctx, "complete-source-file", file.Close))
		}
	}()
	f.info, err = file.Stat()
	if err == nil {
		f.raw, err = readBounded(file, limit)
		if err == nil {
			f.reference = artifactReference(f.raw)
		}
	}
	if err == nil {
		err = f.check(ctx)
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (f *assistantDialectCompleteFile) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entry, e1 := f.root.Lstat(f.name)
	held, e2 := f.file.Stat()
	if e1 != nil || e2 != nil || !os.SameFile(entry, f.info) || !os.SameFile(held, f.info) || ownerOnlyFile(f.name, entry.Mode()) != nil || ownedByUs(f.name, entry) != nil {
		return errors.Join(errAssistantDialectCompleteRead, e1, e2)
	}
	raw, err := readBounded(io.NewSectionReader(f.file, 0, int64(f.limit)+1), f.limit)
	if err != nil || verifyRetainedArtifact(f.reference, raw) != nil || f.raw != nil && !bytes.Equal(raw, f.raw) {
		return errors.Join(errAssistantDialectCompleteRead, err)
	}
	again, err := f.root.Lstat(f.name)
	if err != nil || !os.SameFile(again, f.info) {
		return errors.Join(errAssistantDialectCompleteRead, err)
	}
	return nil
}

type assistantDialectCompleteJournal struct {
	root *os.Root
	lock *os.File
	file *assistantDialectCompleteFile
	info os.FileInfo
	run  inputResearchRun
}

// Minted only while the actual preparation read view is live. Every file and
// directory exclusion belongs to this lexical call. Get is an independent
// decode of the held bounded original bytes, not a saved snapshot as current.
// No artifact/provider queue is entered here or by Check/Get/Close.
type assistantDialectCompleteSourceHold struct {
	self                         *assistantDialectCompleteSourceHold
	s                            *Server
	ctx                          context.Context
	view                         *fileInputPreparationRead // borrowed; never closed here
	collectionRoot, researchRoot *os.Root
	preparationLock              *os.File
	projectInfo                  os.FileInfo
	collection                   *assistantDialectCompleteFile
	edges                        []retainedArtifactInjectedEdge
	journals                     map[string]*assistantDialectCompleteJournal
	order                        []string
	saved                        inputPreparationReply
	inventory                    assistantDialectCompleteInventory
	closed                       bool
	closeErr                     error
}

// Call before artifact/provider custody, using the owning host's original
// preparation read. Holding v.mu only for pointer/pin checks permits supplied
// Get-only callbacks; callbacks cannot acquire another view through this API.
func (s *Server) withAssistantDialectCompleteInventory(ctx context.Context, reader inputPreparationReader, id string, use func(*assistantDialectCompleteSourceHold) error) (err error) {
	if use == nil {
		return errAssistantDialectCompleteSourceOwner
	}
	h, err := s.acquireAssistantDialectCompleteInventory(ctx, reader, id)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, h.Close()) }()
	if err = use(h); err != nil {
		return err
	}
	return h.Check(ctx)
}

func (s *Server) acquireAssistantDialectCompleteInventory(ctx context.Context, reader inputPreparationReader, id string) (_ *assistantDialectCompleteSourceHold, err error) {
	view, ok := reader.(*fileInputPreparationRead)
	if !ok || view == nil || s == nil || s.root == nil || s.inputPreparations != nil || s.researchJournals != nil || !inputPreparationID.MatchString(id) {
		return nil, errAssistantDialectCompleteSourceOwner
	}
	h := &assistantDialectCompleteSourceHold{s: s, ctx: ctx, view: view, journals: map[string]*assistantDialectCompleteJournal{}}
	h.self = h
	h.projectInfo, err = s.root.Stat(".")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, h.Close())
		}
	}()
	h.collectionRoot, err = openDialectArtifactOwnerFolder(ctx, s.root, false, "complete-collection", ".desk-private", "input-preparations")
	if err != nil {
		return nil, err
	}
	h.edges, err = pinInjectedArtifactHeldEdges(ctx, s.root, h.collectionRoot, ".desk-private", "input-preparations")
	if err != nil {
		return nil, err
	}
	if err = h.checkPreparationView(); err != nil {
		return nil, err
	}
	h.preparationLock, err = h.collectionRoot.Open(".")
	if err != nil {
		return nil, err
	}
	// Same directory inode as fileInputPreparationRead/openTransaction and
	// every file repository CAS: no created surrogate lock can qualify.
	if err = lockPrivateData(h.preparationLock, false); err != nil {
		return nil, err
	}
	h.collection, err = openAssistantDialectCompleteFile(ctx, h.collectionRoot, id+".json", inputPreparationLimit)
	if err != nil {
		return nil, err
	}
	h.saved, err = decodeInputPreparation(id, h.collection.raw)
	if err != nil {
		return nil, err
	}
	for _, record := range h.saved.Document.Records {
		if record.Class != "generated" {
			continue
		}
		if record.Research == nil || !inputResearchID.MatchString(record.Research.RunID) {
			return nil, errAssistantDialectCompleteRead
		}
		if _, exists := h.journals[record.Research.RunID]; !exists {
			h.journals[record.Research.RunID] = nil
			h.order = append(h.order, record.Research.RunID)
		}
	}
	sort.Strings(h.order)
	if len(h.order) != 0 {
		h.researchRoot, err = openDialectArtifactOwnerFolder(ctx, s.root, false, "complete-research", ".desk-private", "input-research")
		if err != nil {
			return nil, err
		}
		more, e := pinInjectedArtifactHeldEdges(ctx, s.root, h.researchRoot, ".desk-private", "input-research")
		if e != nil {
			return nil, e
		}
		if err = mergeInjectedArtifactEdges(&h.edges, more); err != nil {
			return nil, err
		}
	}
	// All existing fileResearchJournal writers lock the run directory EX.
	// Acquire shared NB exclusions in stable selected order; never create a
	// missing source or wait while holding preparation/source custody.
	for _, runID := range h.order {
		j := &assistantDialectCompleteJournal{}
		h.journals[runID] = j
		j.root, err = openDialectArtifactOwnerFolder(ctx, h.researchRoot, false, "complete-journal", runID)
		if err != nil {
			return nil, err
		}
		j.info, err = j.root.Stat(".")
		if err != nil {
			return nil, err
		}
		j.lock, err = j.root.Open(".")
		if err != nil {
			return nil, err
		}
		if err = lockPrivateData(j.lock, false); err != nil {
			return nil, err
		}
		j.file, err = openAssistantDialectCompleteFile(ctx, j.root, "record.json", inputResearchRecordLimit)
		if err != nil {
			return nil, err
		}
		decoded, e := decodeResearchJournal(j.file.raw, runID)
		if e != nil {
			return nil, e
		}
		if !assistantRunDigest(decoded.Revision) {
			return nil, errAssistantDialectCompleteRead
		}
		// Keep only the original joined facts, not up to16MiB of unrelated
		// journal operations for each selected record. Original exact file
		// bytes remain pinned by FD plus SHA/length and are never rewritten.
		var fields []inputResearchField
		if decoded.Recipe.Fields != nil {
			fields = make([]inputResearchField, len(decoded.Recipe.Fields))
			for i, field := range decoded.Recipe.Fields {
				fields[i] = inputResearchField{Name: strings.Clone(field.Name), Description: strings.Clone(field.Description), Type: strings.Clone(field.Type)}
			}
		}
		j.run = inputResearchRun{ID: decoded.ID, PreparationID: decoded.PreparationID, Revision: decoded.Revision, RecipeVersion: decoded.RecipeVersion, Recipe: inputResearchRecipe{Fields: fields}}
		j.file.raw = nil
	}
	h.inventory = assistantDialectCompleteInventory{Declarations: []runcontext.DialectResearchRevision{}, Sources: []assistantDialectCompleteSource{}}
	revisions := map[int]int{}
	for _, record := range h.saved.Document.Records {
		if record.Class != "generated" {
			continue
		}
		j := h.journals[record.Research.RunID]
		history, e := resolveRetainedResearchHistory(h.saved.Document, record, j.run)
		if e != nil {
			return nil, e
		}
		// Valid authoritative producer/admission uses original collection SHA.
		// Do not hash, truncate or promote malformed known provenance to unknown.
		if !assistantRunDigest(history.RunRevision) {
			return nil, errAssistantDialectCompleteRead
		}
		at, exists := revisions[history.RecipeVersion]
		var fields []runcontext.DialectResearchField
		if history.Fields != nil {
			fields = []runcontext.DialectResearchField{}
			for _, field := range history.Fields {
				raw, e := assistantDialectCompleteJSON(field)
				if e != nil {
					return nil, e
				}
				fields = append(fields, runcontext.DialectResearchField{Name: field.Name, FieldJSON: string(raw)})
			}
		}
		if exists {
			before, _ := json.Marshal(h.inventory.Declarations[at].Fields)
			after, _ := json.Marshal(fields)
			if !bytes.Equal(before, after) {
				return nil, errAssistantDialectCompleteRead
			}
		} else {
			at = len(h.inventory.Declarations)
			revisions[history.RecipeVersion] = at
			// Frame the collection/version identity; original per-record run
			// identity and exact revision digest remain separately preserved.
			label := assistantDialectCompleteRevision(h.saved.Document.ID, history.RecipeVersion)
			h.inventory.Declarations = append(h.inventory.Declarations, runcontext.DialectResearchRevision{Revision: label, Fields: fields})
		}
		revision := history.RunRevision
		h.inventory.Sources = append(h.inventory.Sources, assistantDialectCompleteSource{RecordID: history.RecordID, RunID: history.RunID, RunRevision: &revision, RecipeVersion: history.RecipeVersion, Availability: "known", Inventory: at})
	}
	if err = h.inventory.validate(); err != nil {
		return nil, err
	}
	if err = h.Check(ctx); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *assistantDialectCompleteSourceHold) checkPreparationView() error {
	h.view.mu.Lock()
	defer h.view.mu.Unlock()
	if h.view.root == nil || h.view.lock == nil || h.view.ctx.Err() != nil {
		return errInputPreparationReadClosed
	}
	selected, e1 := h.collectionRoot.Stat(".")
	borrowed, e2 := h.view.root.Stat(".")
	lock, e3 := h.view.lock.Stat()
	if e1 != nil || e2 != nil || e3 != nil || !os.SameFile(selected, borrowed) || !os.SameFile(selected, lock) {
		return errors.Join(errAssistantDialectCompleteRead, e1, e2, e3)
	}
	return nil
}

func (h *assistantDialectCompleteSourceHold) Check(ctx context.Context) error {
	if h == nil || h.self != h || h.closed || ctx.Err() != nil || h.ctx.Err() != nil {
		return errAssistantDialectCompleteRead
	}
	if !sameDirectory(h.s.projectDir, h.projectInfo) {
		return errAssistantDialectCompleteRead
	}
	if err := h.checkPreparationView(); err != nil {
		return err
	}
	if err := checkInjectedArtifactEdges(ctx, h.edges); err != nil {
		return err
	}
	if h.collection != nil {
		if err := h.collection.check(ctx); err != nil {
			return err
		}
	}
	for _, id := range h.order {
		j := h.journals[id]
		if j == nil || j.root == nil || j.file == nil || j.lock == nil {
			return errAssistantDialectCompleteRead
		}
		entry, e1 := h.researchRoot.Lstat(id)
		held, e2 := j.root.Stat(".")
		lock, e3 := j.lock.Stat()
		if e1 != nil || e2 != nil || e3 != nil || !os.SameFile(entry, j.info) || !os.SameFile(held, j.info) || !os.SameFile(lock, j.info) {
			return errors.Join(errAssistantDialectCompleteRead, e1, e2, e3)
		}
		if err := j.file.check(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (h *assistantDialectCompleteSourceHold) Get(id string) (inputPreparationReply, error) {
	if id != h.saved.Document.ID {
		return inputPreparationReply{}, errAssistantDialectCompleteRead
	}
	if err := h.Check(h.ctx); err != nil {
		return inputPreparationReply{}, err
	}
	raw, err := readBounded(io.NewSectionReader(h.collection.file, 0, int64(inputPreparationLimit)+1), inputPreparationLimit)
	if err != nil {
		return inputPreparationReply{}, err
	}
	if !bytes.Equal(raw, h.collection.raw) {
		return inputPreparationReply{}, errAssistantDialectCompleteRead
	}
	if err = h.Check(h.ctx); err != nil {
		return inputPreparationReply{}, err
	}
	return decodeInputPreparation(id, raw)
}

func (h *assistantDialectCompleteSourceHold) encode(ctx context.Context, snapshot runcontext.DialectSnapshot) ([]byte, error) {
	if err := h.Check(ctx); err != nil {
		return nil, err
	}
	if snapshot.ID != h.saved.Document.ID || snapshot.Revision != h.saved.SHA256 || snapshot.RecordCount != int64(len(h.saved.Document.Records)) {
		return nil, errAssistantDialectCompleteRead
	}
	raw, err := encodeAssistantDialectCompleteRead(ctx, snapshot, h.inventory)
	if err != nil {
		return nil, err
	}
	if err = h.Check(ctx); err != nil {
		return nil, err
	}
	return raw, nil
}

func (h *assistantDialectCompleteSourceHold) Close() error {
	if h == nil {
		return nil
	}
	if h.self != h {
		return errAssistantDialectCompleteRead
	}
	if h.closed {
		return h.closeErr
	}
	h.closed = true
	closeOne := func(label string, close func() error) {
		h.closeErr = errors.Join(h.closeErr, closeDialectArtifactOwner(h.ctx, label, close))
	}
	for i := len(h.order) - 1; i >= 0; i-- {
		j := h.journals[h.order[i]]
		if j == nil {
			continue
		}
		if j.file != nil {
			closeOne("complete-journal-file", j.file.file.Close)
		}
		if j.lock != nil {
			closeOne("complete-journal-lock", j.lock.Close)
		}
		if j.root != nil {
			closeOne("complete-journal-root", j.root.Close)
		}
	}
	if h.collection != nil {
		closeOne("complete-collection-file", h.collection.file.Close)
	}
	if h.preparationLock != nil {
		closeOne("complete-preparation-lock", h.preparationLock.Close)
	}
	if h.researchRoot != nil {
		closeOne("complete-research-root", h.researchRoot.Close)
	}
	if h.collectionRoot != nil {
		closeOne("complete-collection-root", h.collectionRoot.Close)
	}
	return h.closeErr
}
