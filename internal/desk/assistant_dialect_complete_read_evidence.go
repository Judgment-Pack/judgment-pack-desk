package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// The live execution supplies the actual original work/root owner, never a
// decoded journal's guessed namespace. Historical archive/import resolution is
// separately owned and must not construct this live capability.
type assistantDialectCompletedReadEvidence struct {
	execution  *assistantRunExecution
	projection *assistantDialectCompleteProjectionRead
}

func (f *assistantDialectCompletedReadEvidence) sourceOwner(ctx context.Context, run assistantRunRecord) (*Server, *os.File, error) {
	if f == nil || run.Historical {
		return nil, nil, errAssistantRunScope
	}
	if e := f.execution; e != nil && f.projection == nil {
		if e.service == nil || e.service.server == nil || e.writer == nil || e.writer.lease == nil || !reflect.DeepEqual(e.record.Binding, run.Binding) || e.record.ID != run.ID || e.writer.lease.file == nil || e.writer.lease.work == nil || e.writer.lease.store.project != run.Binding.Project || e.writer.lease.store.server != e.service.server {
			return nil, nil, errAssistantRunScope
		}
		return e.service.server, e.writer.lease.work, nil
	}
	if p := f.projection; p != nil && f.execution == nil && p.matches(run) {
		if err := p.Check(ctx); err != nil {
			return nil, nil, err
		}
		return p.server, p.work, nil
	}
	return nil, nil, errAssistantRunScope
}
func (f *assistantDialectCompletedReadEvidence) original(ctx context.Context, run assistantRunRecord, op assistantRunOperation, read assistantDialectRead) (out assistantDialectCompleteRead, err error) {
	s, _, err := f.sourceOwner(ctx, run)
	if err != nil || !validAssistantDialectCompleteReadSelector(read) {
		return out, errors.Join(errAssistantRunScope, err)
	}
	held := assistantDialectCompleteReadOf(&run, op.ID)
	actual, in, e := assistantDialectCompleteReadRoot(run, op.ID)
	if e != nil || actual.State != "completed" || !reflect.DeepEqual(actual, op) || held == nil || held.Receipt == nil || held.Uncertainty != "" || !assistantDialectCompleteSelectorMatches(read, *held) || held.Intent.Receipt != assistantDialectCompleteReadReceipt(run, *held, in) || !assistantRunDigest(run.DialectArtifactOwner) {
		return out, errAssistantRunScope
	}
	// Injected raw ownership needs its own lexical selected view under the
	// already-held source/queue order. Never fall back to native files.
	if s.retainedArtifacts != nil {
		return out, errAssistantDialectCompleteSourceOwner
	}
	if err = f.nativeProvenance(ctx, run); err != nil {
		return out, err
	}
	raw, err := s.readAssistantDialectCompleteNativeRaw(ctx, held.Intent.Reference)
	if err != nil {
		return out, err
	}
	out, err = readAssistantDialectCompleteRead(ctx, raw)
	if err != nil {
		return out, err
	}
	if held.ResearchIndex != nil && !reflect.DeepEqual(held.ResearchIndex, out.Snapshot.ResearchIndex) {
		return out, errAssistantRunScope
	}
	if out.Snapshot.ID != read.ID || out.Snapshot.Name != read.Name || out.Snapshot.Revision != read.Revision || out.Snapshot.PackKey != read.PackKey || "sha256:"+digestOf([]byte(out.Snapshot.PackJSON)) != read.Complete.PackDigest || !bytes.Equal(assistantDialectCompleteReadMetadata(out.Snapshot), read.View) {
		return out, errAssistantRunScope
	}
	result, err := out.result(ctx)
	if err != nil {
		return out, err
	}
	if digestOf(assistantDialectCompleteResultBytes(result)) != held.Intent.ResultSHA256 {
		return out, errAssistantRunScope
	}
	if err = f.nativeProvenance(ctx, run); err != nil {
		return out, err
	}
	return out, nil
}
func (f *assistantDialectCompletedReadEvidence) nativeProvenance(ctx context.Context, run assistantRunRecord) (err error) {
	s, workFile, err := f.sourceOwner(ctx, run)
	if err != nil {
		return err
	}
	if s.root == nil || s.assistant == nil {
		return errAssistantRunScope
	}
	metadata, err := s.openDialectArtifactSelectedMetadata(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, metadata.Close()) }()
	ownerRow, err := metadata.ownerRow(ctx, run.DialectArtifactOwner)
	if err != nil {
		return err
	}
	owner, err := readDialectArtifactOwnerRecord(ownerRow.raw)
	if err != nil {
		return err
	}
	locationRow, err := metadata.locationRow(ctx, ownerRow)
	if err != nil {
		return err
	}
	location, err := readDialectArtifactLocationRecord(locationRow.raw)
	if err != nil {
		return err
	}
	workRow, err := metadata.workRow(ctx, owner.Work.ID)
	if err != nil {
		return err
	}
	work, err := readDialectArtifactWorkDomainRecord(workRow.raw)
	if err != nil {
		return err
	}
	scope, err := metadata.scopeRow(ctx, dialectArtifactOwnerScope{Version: 1, Kind: "native", Owner: run.DialectArtifactOwner, Namespace: run.Binding.Project})
	if err != nil {
		return err
	}
	projectInfo, e1 := s.root.Stat(".")
	settingsInfo, e2 := s.assistant.root.Stat(".")
	workInfo, e3 := workFile.Stat()
	workEntry, e4 := s.assistant.root.Lstat(".data-work.lock")
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || !os.SameFile(workInfo, workEntry) || !sameDirectory(s.projectDir, projectInfo) || !sameDirectory(s.configDir, settingsInfo) || identityKey(projectInfo) != owner.FolderIdentity || location.FolderIdentity != identityKey(projectInfo) || work.ID != dialectArtifactWorkDomainID(identityKey(settingsInfo), identityKey(workInfo)) || owner.Work.ID != work.ID || location.WorkDomain != work.ID || work.FolderIdentity != identityKey(settingsInfo) || work.LockIdentity != identityKey(workInfo) {
		return errors.Join(errAssistantRunScope, e1, e2, e3, e4)
	}
	private, err := openDialectArtifactOwnerFolder(ctx, s.root, false, "complete-read-marker", ".desk-private")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "complete-read-marker", private.Close)) }()
	raw, err := readInjectedArtifactExactFile(ctx, private, dialectArtifactOwnerMarker, dialectArtifactOwnerMarkerBytes, "complete-read-marker")
	if err != nil {
		return err
	}
	marker, err := readDialectArtifactOwnerMarker(raw)
	if err != nil || marker.Owner != run.DialectArtifactOwner {
		return errors.Join(errAssistantRunScope, err)
	}
	return errors.Join(ownerRow.check(ctx), locationRow.check(ctx), workRow.check(ctx), scope.check(ctx), metadata.check(ctx))
}
func (f *assistantDialectCompletedReadEvidence) completeDialectSnapshot(ctx context.Context, run assistantRunRecord, op assistantRunOperation, read assistantDialectRead) (runcontext.DialectSnapshot, error) {
	v, err := f.original(ctx, run, op, read)
	return v.Snapshot, err
}
func (f *assistantDialectCompletedReadEvidence) acquireCurrent(ctx context.Context, run assistantRunRecord, op assistantRunOperation, read assistantDialectRead, reader inputPreparationReader) (source *assistantDialectCompleteSourceHold, project *assistantDialectCompleteProject, original, current runcontext.DialectSnapshot, err error) {
	v, err := f.original(ctx, run, op, read)
	if err != nil {
		return nil, nil, original, current, err
	}
	original = v.Snapshot
	if borrowed, ok := reader.(assistantDialectBorrowedPreparation); ok {
		reader = borrowed.view
	}
	if actual, ok := reader.(*assistantDialectCompleteProjectionRead); ok {
		if f.projection != actual || !actual.matches(run) {
			return nil, nil, original, current, errAssistantRunScope
		}
		reader = actual.view
	}
	s, _, err := f.sourceOwner(ctx, run)
	if err != nil {
		return nil, nil, original, current, err
	}
	defer func() {
		if err != nil {
			if project != nil {
				err = errors.Join(err, project.Close(ctx))
				project = nil
			}
			if source != nil {
				err = errors.Join(err, source.Close())
				source = nil
			}
		}
	}()
	source, err = s.acquireAssistantDialectCompleteInventory(ctx, reader, read.ID)
	if err != nil {
		return source, project, original, current, err
	}
	project, err = s.acquireAssistantDialectCompleteProject(ctx, read.PackKey)
	if err != nil {
		return source, project, original, current, err
	}
	current, err = source.snapshot(ctx, project)
	if err == nil && (project.pack.path != read.PackPath || project.declaration != read.Declaration) {
		err = errAssistantRunScope
	}
	return source, project, original, current, err
}
func (f *assistantDialectCompletedReadEvidence) completeDialectSnapshotUsingView(ctx context.Context, run assistantRunRecord, op assistantRunOperation, read assistantDialectRead, reader inputPreparationReader) (original, current runcontext.DialectSnapshot, err error) {
	source, project, original, current, err := f.acquireCurrent(ctx, run, op, read, reader)
	if err != nil {
		return original, current, err
	}
	defer func() { err = errors.Join(err, project.Close(ctx), source.Close()) }()
	return original, current, nil
}

func (e *assistantRunExecution) resolveAssistantDialectCompleteRead(ctx context.Context, run assistantRunRecord, read assistantDialectRead) (assistantDialectCompleteRead, error) {
	if read.Complete == nil {
		return assistantDialectCompleteRead{}, errAssistantRunScope
	}
	op := assistantDialectOperation(run, read.Complete.ReadOperationID)
	if op == nil {
		return assistantDialectCompleteRead{}, errAssistantRunScope
	}
	return (&assistantDialectCompletedReadEvidence{execution: e}).original(ctx, run, *op, read)
}
func (e *assistantRunExecution) recheckAssistantDialectCompleteRead(ctx context.Context, read assistantDialectRead) (err error) {
	if err = e.recheckAssistantCritic(ctx); err != nil {
		return err
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return err
	}
	op := assistantDialectOperation(run, read.Complete.ReadOperationID)
	if op == nil {
		return errAssistantRunScope
	}
	view, err := e.service.server.preparationRepository().TryRead(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, view.Close()) }()
	original, current, err := (&assistantDialectCompletedReadEvidence{execution: e}).completeDialectSnapshotUsingView(ctx, run, *op, read, assistantDialectBorrowedPreparation{view})
	if err == nil && !reflect.DeepEqual(original, current) {
		err = errAssistantRunScope
	}
	return err
}

func (e *assistantRunExecution) readAssistantDialectCompletePage(ctx context.Context, a assistantDialectArguments, paging *assistantDialectCompleteReadPaging) (runcontext.ToolResult, *assistantDialectCompletePageReceipt, error) {
	if a.Cursor == nil {
		return runcontext.ToolResult{}, nil, errAssistantRunScope
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return runcontext.ToolResult{}, nil, err
	}
	read, err := paging.selected(ctx, e, a)
	if err != nil {
		return runcontext.ToolResult{}, nil, err
	}
	if err = e.recheckAssistantDialectCompleteRead(ctx, read); err != nil {
		return runcontext.ToolResult{}, nil, err
	}
	held := assistantDialectCompleteReadOf(&run, read.Complete.ReadOperationID)
	if held == nil || held.ResearchIndex == nil {
		return runcontext.ToolResult{}, nil, errAssistantRunScope
	}
	evidence, err := e.resolveAssistantDialectCompleteRead(ctx, run, read)
	if err != nil {
		return runcontext.ToolResult{}, nil, err
	}
	tools := runcontext.NewDialectTools(runcontext.DialectAuthoringPorts{Read: func(context.Context, runcontext.DialectReadRequest) (runcontext.DialectSnapshot, error) {
		return evidence.Snapshot, nil
	}, ReadFieldIndex: evidence.fieldIndex, ReadDeclarations: evidence.declarations})
	initial := assistantChatJSON(runcontext.DialectReadRequest{ID: read.ID, PackKey: read.PackKey})
	first, err := tools[0].Execute(ctx, initial)
	if err != nil || first.Err() {
		return runcontext.ToolResult{}, nil, errors.Join(errAssistantRunScope, err)
	}
	result, err := tools[0].Execute(ctx, assistantChatJSON(map[string]any{"id": read.ID, "packKey": read.PackKey, "cursor": *a.Cursor}))
	if err == nil && !result.Err() {
		err = e.recheckAssistantDialectCompleteRead(ctx, read)
	}
	if err != nil || result.Err() {
		return result, nil, err
	}
	page, err := assistantDialectCompletePageValue(result.Value().JSON())
	if err != nil {
		return runcontext.ToolResult{}, nil, err
	}
	receipt := &assistantDialectCompletePageReceipt{Version: 1, ReadOperationID: read.Complete.ReadOperationID, FieldIndexes: []assistantDialectCompleteFieldProof{}}
	start := 0
	for at, row := range held.ResearchIndex.Revisions {
		if row.Fields == nil {
			continue
		}
		end := start + *row.Fields
		if start < page.Cursor+len(page.Declarations) && end > page.Cursor {
			proof, e := evidence.fieldIndex(ctx, runcontext.DialectFieldIndexRequest{ID: read.ID, PackKey: read.PackKey, Revision: read.Revision, InventoryDigest: held.ResearchIndex.Digest, Entry: at})
			if e != nil {
				return runcontext.ToolResult{}, nil, e
			}
			receipt.FieldIndexes = append(receipt.FieldIndexes, assistantDialectCompleteFieldProof{Entry: proof.Entry, FieldIndex: proof.FieldIndex})
		}
		start = end
	}
	return result, receipt, nil
}

// Package read state is populated only by the original actual producer. Page
// selection never scans a journal for the latest plausible matching read.
// The selected compact reference must still authenticate its completed root
// and original raw source; this ephemeral state confers no durable authority.
type assistantDialectCompleteReadPaging struct {
	self      *assistantDialectCompleteReadPaging
	execution *assistantRunExecution
	mu        sync.Mutex
	entries   map[string]struct {
		read       assistantDialectRead
		generation uint64
	}
	order []string
}

func newAssistantDialectCompleteReadPaging(e *assistantRunExecution) *assistantDialectCompleteReadPaging {
	p := &assistantDialectCompleteReadPaging{execution: e, entries: map[string]struct {
		read       assistantDialectRead
		generation uint64
	}{}}
	p.self = p
	return p
}
func (p *assistantDialectCompleteReadPaging) bind(o *assistantDialectCompleteReadOwner) error {
	if p == nil || p.self != p || !o.original() || !o.acknowledged || o.execution != p.execution || o.read.Complete == nil {
		return errAssistantRunScope
	}
	key := string(assistantChatJSON([]string{o.read.ID, o.read.PackKey}))
	var read assistantDialectRead
	if err := json.Unmarshal(assistantChatJSON(o.read), &read); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, keyWas := range p.order {
		if keyWas == key {
			p.order = append(p.order[:i], p.order[i+1:]...)
			break
		}
	}
	p.entries[key] = struct {
		read       assistantDialectRead
		generation uint64
	}{read, o.generation}
	p.order = append(p.order, key)
	if len(p.order) > runcontext.DialectRememberedReads {
		delete(p.entries, p.order[0])
		p.order = p.order[1:]
	}
	return nil
}
func (p *assistantDialectCompleteReadPaging) selected(ctx context.Context, e *assistantRunExecution, a assistantDialectArguments) (assistantDialectRead, error) {
	if p == nil || p.self != p || p.execution != e {
		return assistantDialectRead{}, errAssistantRunScope
	}
	generation, ok := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration)
	p.mu.Lock()
	defer p.mu.Unlock()
	basis, found := p.entries[string(assistantChatJSON([]string{a.ID, a.PackKey}))]
	if !ok || !found || generation.owner != e || generation.finalizer || generation.number != basis.generation {
		return assistantDialectRead{}, errAssistantRunScope
	}
	var read assistantDialectRead
	if err := json.Unmarshal(assistantChatJSON(basis.read), &read); err != nil {
		return read, err
	}
	return read, nil
}

// Pages are observations of one earlier acknowledged read, never new roots or
// proposal selectors. The root owns its top index once; a page carries only the
// touched revision field indexes, which authenticate the exact declaration bytes
// without source I/O during saved-data validation.
type assistantDialectCompletePageReceipt struct {
	Version         int                                  `json:"version"`
	ReadOperationID string                               `json:"readOperationId"`
	FieldIndexes    []assistantDialectCompleteFieldProof `json:"fieldIndexes"`
}
type assistantDialectCompleteFieldProof struct {
	Entry      int                          `json:"entry"`
	FieldIndex runcontext.DialectFieldIndex `json:"fieldIndex"`
}
type assistantDialectCompletePage struct {
	ID              string                          `json:"id"`
	Revision        string                          `json:"revision"`
	PackKey         string                          `json:"packKey"`
	InventoryDigest string                          `json:"inventoryDigest"`
	Cursor          int                             `json:"cursor"`
	Declarations    []runcontext.DialectDeclaration `json:"declarations"`
	NextCursor      *int                            `json:"nextCursor"`
}

func assistantDialectCompleteCanonical(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	parsed, err := runcontext.ParseJSON(raw)
	if err != nil {
		return "", err
	}
	return runcontext.CanonicalJSON(parsed), nil
}
func assistantDialectCompletePayload(raw []byte) ([]byte, error) {
	value, err := runcontext.ParseJSON(raw)
	if err != nil {
		return nil, errAssistantRunInvalid
	}
	tool := runcontext.ReadToolResult(value)
	if tool.Err() || len(tool.Content) != 1 {
		return nil, errAssistantRunInvalid
	}
	text := tool.Content[0].Text()
	exact := runcontext.ResultOf([]runcontext.Content{runcontext.TextContent(text)}, nil)
	if len(text) > runcontext.DialectResultBytes || !bytes.Equal(raw, exact.Value().JSON()) {
		return nil, errAssistantRunInvalid
	}
	return []byte(text), nil
}
func assistantDialectCompletePageValue(raw []byte) (assistantDialectCompletePage, error) {
	var page assistantDialectCompletePage
	payload, err := assistantDialectCompletePayload(raw)
	if err != nil {
		return page, err
	}
	fields, err := assistantDialectCompleteObject(payload, []string{"id", "revision", "packKey", "inventoryDigest", "cursor", "declarations", "nextCursor"})
	if err != nil || bytes.Equal(fields["cursor"], []byte("null")) || readAssistantDialectCustody(fields["declarations"], reflect.TypeOf(page.Declarations)) != nil || json.Unmarshal(payload, &page) != nil || page.Declarations == nil || len(page.Declarations) == 0 || len(page.Declarations) > runcontext.DialectPageDeclarations {
		return page, errAssistantRunInvalid
	}
	// The package's byte guard applies to the port page before nextCursor.
	port := runcontext.DialectDeclarationPage{ID: page.ID, PackKey: page.PackKey, Revision: page.Revision, InventoryDigest: page.InventoryDigest, Cursor: page.Cursor, Declarations: page.Declarations}
	canonical, err := assistantDialectCompleteCanonical(port)
	if err != nil || len(canonical) > runcontext.DialectPageBytes {
		return page, errAssistantRunInvalid
	}
	return page, nil
}
func validateAssistantDialectCompleteIndex(index *runcontext.DialectResearchIndex) error {
	if index == nil || index.Revisions == nil || len(index.Revisions) > inputRecordLimit || !assistantDialectPackageDigest(index.Digest) {
		return errAssistantRunInvalid
	}
	canonical, err := assistantDialectCompleteCanonical(index)
	if err != nil || len(canonical) > runcontext.DialectIndexBytes {
		return errAssistantRunInvalid
	}
	canonical, err = assistantDialectCompleteCanonical(index.Revisions)
	if err != nil || "sha256:"+digestOf([]byte(canonical)) != index.Digest {
		return errAssistantRunInvalid
	}
	seen := map[string]bool{}
	for _, row := range index.Revisions {
		if !assistantDialectPackageDigest(row.Revision) || seen[row.Revision] {
			return errAssistantRunInvalid
		}
		seen[row.Revision] = true
		if row.Fields == nil {
			if row.Digest != nil || row.Bytes != nil {
				return errAssistantRunInvalid
			}
			continue
		}
		if *row.Fields < 0 || *row.Fields > 30 || row.Digest == nil || !assistantDialectPackageDigest(*row.Digest) || row.Bytes == nil || *row.Bytes < 0 || *row.Bytes > runcontext.DialectPageBytes {
			return errAssistantRunInvalid
		}
		if *row.Fields == 0 {
			empty := runcontext.DialectFieldIndex{Revision: row.Revision, Fields: []runcontext.DialectIndexField{}}
			canonical, err := assistantDialectCompleteCanonical(empty)
			if err != nil || len(canonical) != *row.Bytes || "sha256:"+digestOf([]byte(canonical)) != *row.Digest {
				return errAssistantRunInvalid
			}
		}
	}
	return nil
}
func validateAssistantDialectCompleteIndexResult(index *runcontext.DialectResearchIndex, raw []byte) error {
	if validateAssistantDialectCompleteIndex(index) != nil {
		return errAssistantRunInvalid
	}
	payload, err := assistantDialectCompletePayload(raw)
	if err != nil {
		return err
	}
	fields, err := assistantTestObject(payload)
	if err != nil {
		return err
	}
	inventory, err := assistantDialectCompleteObject(fields["researchInventory"], []string{"digest", "declarations", "revisions"})
	if err != nil {
		return err
	}
	var summary struct {
		Digest       string `json:"digest"`
		Declarations int    `json:"declarations"`
		Revisions    []struct {
			Revision string `json:"revision"`
			Fields   *int   `json:"fields"`
		} `json:"revisions"`
	}
	if json.Unmarshal(fields["researchInventory"], &summary) != nil || summary.Digest != index.Digest || len(summary.Revisions) != len(index.Revisions) || summary.Revisions == nil || bytes.Equal(inventory["declarations"], []byte("null")) {
		return errAssistantRunInvalid
	}
	rows, err := assistantChatArray(inventory["revisions"])
	if err != nil {
		return err
	}
	total := 0
	for at, row := range index.Revisions {
		if _, err := assistantDialectCompleteObject(rows[at], []string{"revision", "fields"}); err != nil {
			return err
		}
		if summary.Revisions[at].Revision != row.Revision || !reflect.DeepEqual(summary.Revisions[at].Fields, row.Fields) {
			return errAssistantRunInvalid
		}
		if row.Fields != nil {
			total += *row.Fields
		}
	}
	if total != summary.Declarations {
		return errAssistantRunInvalid
	}
	return nil
}
func validateAssistantDialectCompletePageReceipt(run assistantRunRecord, op assistantRunOperation, in assistantHostIntent, result assistantHostResult) error {
	proof := result.DialectPage
	if proof == nil {
		return nil
	}
	if op.Kind != "host" || op.Name != "read_dialect" || op.State != "completed" || !in.Root || proof.Version != 1 || result.DialectRead != nil || result.Tests != nil || result.Read != nil || result.PolicySource != nil || result.ExternalReadEvidence != nil || result.ExternalControl != nil || result.ExternalFailure != nil {
		return errAssistantRunInvalid
	}
	rawFields, rawErr := assistantTestObject(op.Result)
	if rawErr != nil || assistantDialectCompleteShape(rawFields["dialectPage"], reflect.TypeOf(*proof), false) != nil {
		return errAssistantRunInvalid
	}
	args, err := readAssistantDialectArguments(op.Name, in.Arguments)
	if err != nil || args.Cursor == nil {
		return errAssistantRunInvalid
	}
	root, originalIntent, err := assistantDialectCompleteReadRoot(run, proof.ReadOperationID)
	held := assistantDialectCompleteReadOf(&run, proof.ReadOperationID)
	if err != nil || root.State != "completed" || held == nil || held.Receipt == nil || held.Uncertainty != "" || held.ResearchIndex == nil || !reflect.DeepEqual(held.Receipt, &held.Intent) || held.Intent.Receipt != assistantDialectCompleteReadReceipt(run, *held, originalIntent) {
		return errAssistantRunInvalid
	}
	rootAt, pageAt := -1, -1
	for at, row := range run.Operations {
		if row.ID == root.ID {
			rootAt = at
		}
		if row.ID == op.ID {
			pageAt = at
		}
	}
	if rootAt < 0 || pageAt <= rootAt {
		return errAssistantRunInvalid
	}
	var original assistantHostResult
	if readAssistantHostReceipt(root.Result, &original) != nil || original.DialectRead == nil || original.DialectPage != nil || !assistantDialectCompleteSelectorMatches(*original.DialectRead, *held) || digestOf(original.Result) != held.Intent.ResultSHA256 || validateAssistantDialectReadReceipt(root, originalIntent, original) != nil || validateAssistantDialectCompleteIndexResult(held.ResearchIndex, original.Result) != nil {
		return errAssistantRunInvalid
	}
	page, err := assistantDialectCompletePageValue(result.Result)
	if err != nil || args.ID != held.InputID || args.PackKey != held.PackKey || page.ID != held.InputID || page.PackKey != held.PackKey || page.Revision != held.Revision || page.InventoryDigest != held.ResearchIndex.Digest || page.Cursor != *args.Cursor {
		return errAssistantRunInvalid
	}
	if proof.FieldIndexes == nil || len(proof.FieldIndexes) > runcontext.DialectPageDeclarations {
		return errAssistantRunInvalid
	}
	// Even with two partially covered boundary revisions, a page needs at
	// most 1,058 original field hashes. Keep aggregate proof bytes bounded too.
	canonical, err := assistantDialectCompleteCanonical(proof.FieldIndexes)
	if err != nil || len(canonical) > runcontext.DialectPageBytes {
		return errAssistantRunInvalid
	}
	start, proofAt, declarationAt := 0, 0, 0
	for at, entry := range held.ResearchIndex.Revisions {
		if entry.Fields == nil {
			continue
		}
		end := start + *entry.Fields
		if start < page.Cursor+len(page.Declarations) && end > page.Cursor {
			if proofAt >= len(proof.FieldIndexes) {
				return errAssistantRunInvalid
			}
			fieldPage := proof.FieldIndexes[proofAt]
			proofAt++
			if fieldPage.Entry != at || fieldPage.FieldIndex.Revision != entry.Revision || fieldPage.FieldIndex.Fields == nil || len(fieldPage.FieldIndex.Fields) != *entry.Fields {
				return errAssistantRunInvalid
			}
			canonical, err := assistantDialectCompleteCanonical(fieldPage)
			if err != nil || len(canonical) > runcontext.DialectPageBytes {
				return errAssistantRunInvalid
			}
			canonical, err = assistantDialectCompleteCanonical(fieldPage.FieldIndex)
			if err != nil || len(canonical) != *entry.Bytes || "sha256:"+digestOf([]byte(canonical)) != *entry.Digest {
				return errAssistantRunInvalid
			}
			seen := map[string]bool{}
			for _, field := range fieldPage.FieldIndex.Fields {
				if !researchFieldName.MatchString(field.Name) || seen[field.Name] || !assistantDialectPackageDigest(field.Digest) {
					return errAssistantRunInvalid
				}
				seen[field.Name] = true
			}
			for pos := max(start, page.Cursor); pos < min(end, page.Cursor+len(page.Declarations)); pos++ {
				field := fieldPage.FieldIndex.Fields[pos-start]
				declaration := page.Declarations[declarationAt]
				declarationAt++
				if declaration.Revision != entry.Revision || declaration.Name != field.Name || "sha256:"+digestOf([]byte(declaration.FieldJSON)) != field.Digest {
					return errAssistantRunInvalid
				}
			}
		}
		start = end
	}
	if proofAt != len(proof.FieldIndexes) || declarationAt != len(page.Declarations) || page.Cursor < 0 || page.Cursor >= start || page.Cursor+len(page.Declarations) > start {
		return errAssistantRunInvalid
	}
	next := page.Cursor + len(page.Declarations)
	if next == start && page.NextCursor != nil || next < start && (page.NextCursor == nil || *page.NextCursor != next) {
		return errAssistantRunInvalid
	}
	return nil
}
