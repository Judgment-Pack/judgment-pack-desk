//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func completeEnrollmentSnapshot(t *testing.T, s *Server, id, key string) runcontext.DialectSnapshot {
	t.Helper()
	var snapshot runcontext.DialectSnapshot
	view, err := s.preparationRepository().TryRead(t.Context())
	assistantRunMust(t, err)
	err = s.withAssistantDialectCompleteInventory(t.Context(), view, id, func(source *assistantDialectCompleteSourceHold) error {
		project, e := s.acquireAssistantDialectCompleteProject(t.Context(), key)
		if e != nil {
			return e
		}
		snapshot, e = source.snapshot(t.Context(), project)
		return errorsJoinForCompleteTest(e, project.Close(t.Context()))
	})
	err = errorsJoinForCompleteTest(err, view.Close())
	assistantRunMust(t, err)
	return snapshot
}
func errorsJoinForCompleteTest(a, b error) error { return errors.Join(a, b) }

func completeEnrollmentRead(t *testing.T, s *Server, e *assistantRunExecution, saved inputPreparationReply, call string) (assistantRunRecord, assistantRunOperation, assistantDialectRead) {
	t.Helper()
	registry, err := e.assistantDialectTools()
	assistantRunMust(t, err)
	raw := assistantChatJSON(map[string]string{"id": saved.Document.ID, "packKey": "example"})
	assistantPolicyEvent(t, e, call, "read_dialect", raw)
	ctx := context.WithValue(runcontext.WithCallID(e.generationContext(t.Context(), 1), call), assistantHostRootKey{}, true)
	var result runcontext.ToolResult
	for _, tool := range registry.tools {
		if tool.Name == "read_dialect" {
			result, err = tool.Execute(ctx, raw)
		}
	}
	assistantRunMust(t, err)
	if result.Err() {
		t.Fatalf("actual read refused: %s writer=%v", result.Value().JSON(), e.writer.Err())
	}
	run, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	for _, op := range run.Operations {
		if op.Name == "read_dialect" {
			var receipt assistantHostResult
			assistantRunMust(t, readAssistantHostReceipt(op.Result, &receipt))
			if receipt.DialectRead != nil && op.State == "completed" {
				return run, op, *receipt.DialectRead
			}
		}
	}
	t.Fatal("completed actual read missing")
	return run, assistantRunOperation{}, assistantDialectRead{}
}
func completeEnrollmentExecution(t *testing.T) (*Server, *assistantRunExecution, inputPreparationReply) {
	t.Helper()
	s, _, e := assistantDialectAuthorityExecutionSetup(t, nil, nil)
	saved := assistantMappingSeed(t, s)
	e.canonicalMu.Lock()
	e.generation, e.lifecycle = 1, "running"
	e.canonicalMu.Unlock()
	return s, e, saved
}
func TestAssistantDialectCompleteEnrollmentActualReadReload(t *testing.T) {
	s, e, saved := completeEnrollmentExecution(t)
	run, op, read := completeEnrollmentRead(t, s, e, saved, "complete-read-only")
	if len(run.DomainOutputs) != 0 || len(run.DialectDrafts) != 0 || len(run.DialectReads) != 1 || run.DialectReads[0].Receipt == nil || run.DialectReads[0].ResearchIndex == nil || len(run.DialectReads[0].ResearchIndex.Revisions) != 0 || read.Complete == nil || read.Pack != nil {
		t.Fatal("no-proposal root/read reference not retained")
	}
	if bytes.Contains(read.View, []byte("packJson")) || bytes.Contains(read.View, []byte("researchDeclarations")) {
		t.Fatal("complete bytes repeated in selector")
	}
	raw, err := s.readAssistantDialectCompleteNativeRaw(t.Context(), read.Complete.Reference)
	assistantRunMust(t, err)
	v, err := readAssistantDialectCompleteRead(t.Context(), raw)
	assistantRunMust(t, err)
	if v.Snapshot.PackJSON != qualificationTestPack || v.Snapshot.RecordCount != 1 || v.Snapshot.DialectJSON == nil || v.Snapshot.ResearchIndex == nil || len(v.Inventory.Declarations) != 0 {
		t.Fatal("original full snapshot missing")
	}
	// Reload through the real strict storage reader and production evidence.
	data, err := s.openChatData()
	assistantRunMust(t, err)
	stored, err := readPrivateData(data.root, e.writer.lease.store.name, assistantRunRecordLimit)
	assistantRunMust(t, err)
	assistantRunMust(t, data.root.Close())
	ledger, err := decodeAssistantRunStorage(e.writer.lease.store.name, stored)
	assistantRunMust(t, err)
	var reloaded assistantRunRecord
	for _, r := range ledger.Runs {
		if r.ID == run.ID {
			reloaded = r
		}
	}
	// Older no-index complete-read history stays readable without being
	// silently upgraded into paged evidence. The original raw object is kept.
	var legacy assistantRunLedger
	assistantRunMust(t, json.Unmarshal(assistantChatJSON(ledger), &legacy))
	for ri := range legacy.Runs {
		if legacy.Runs[ri].ID != run.ID {
			continue
		}
		completeEnrollmentLegacyRoot(t, &legacy.Runs[ri])
	}
	_, err = decodeAssistantRunStorage(e.writer.lease.store.name, assistantChatJSON(legacy))
	assistantRunMust(t, err)
	original, err := (&assistantDialectCompletedReadEvidence{execution: e}).completeDialectSnapshot(t.Context(), reloaded, op, read)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(original, v.Snapshot) {
		t.Fatal("reload changed original full snapshot")
	}
	view, err := s.preparationRepository().TryRead(t.Context())
	assistantRunMust(t, err)
	before, current, err := (&assistantDialectCompletedReadEvidence{execution: e}).completeDialectSnapshotUsingView(t.Context(), reloaded, op, read, assistantDialectBorrowedPreparation{view})
	err = errorsJoinForCompleteTest(err, view.Close())
	assistantRunMust(t, err)
	if !reflect.DeepEqual(before, current) {
		t.Fatal("independent unchanged current reconstruction differs")
	}
	// A copied result/ref/receipt does not authenticate another actual read.
	bad := read
	copySelector := *read.Complete
	bad.Complete = &copySelector
	bad.Complete.ReadOperationID = "dialect-host-" + strings.Repeat("0", 32)
	if _, err = (&assistantDialectCompletedReadEvidence{execution: e}).completeDialectSnapshot(t.Context(), reloaded, op, bad); err == nil {
		t.Fatal("wrong original read identity accepted")
	}
	// Acknowledged missing raw is refusal, never empty current evidence/repair.
	assistantRunMust(t, s.root.Remove(filepath.Join(retainedArtifactDirectory, read.Complete.Reference.SHA256)))
	if _, err = (&assistantDialectCompletedReadEvidence{execution: e}).completeDialectSnapshot(t.Context(), reloaded, op, read); err == nil {
		t.Fatal("acknowledged missing raw accepted")
	}
}
func TestAssistantDialectCompleteEnrollmentSourceDerivedFields(t *testing.T) {
	s, e, saved := completeEnrollmentExecution(t)
	_ = e
	base := completeEnrollmentSnapshot(t, s, saved.Document.ID, "example")
	if len(base.Columns) != 6 || len(base.FactTargets) != 1 || base.FactTargets[0] != "/company/country" || len(base.EvidenceRequirements) != 1 || base.EvidenceRequirements[0] != "license" || len(base.Conversions) != 0 {
		t.Fatal("actual source fields not projected", base)
	}
	doc := saved.Document
	doc.Name = "Changed original name"
	doc.Records[0].Values["Country"] = "US"
	doc.Records = append(doc.Records, preparedInputRecord{ID: "row_second", Class: "asserted", Values: map[string]string{"New": "false"}})
	doc.Recipe.Qualification.Conversions = map[string]inputConversion{"/company/country": {Kind: "decimal"}}
	doc.Recipe.Qualification.Mapping.Case.Facts[0].Source = "/values/Amount"
	next, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
	assistantRunMust(t, err)
	changed := completeEnrollmentSnapshot(t, s, next.Document.ID, "example")
	if changed.Name != "Changed original name" || changed.RecordCount != 2 || changed.Conversions["/company/country"].Kind != "decimal" || reflect.DeepEqual(base.Columns, changed.Columns) || changed.Revision == base.Revision || changed.DialectJSON == nil || base.DialectJSON == nil || *changed.DialectJSON == *base.DialectJSON {
		t.Fatal("source changes not independently rebuilt")
	}
	pack := strings.Replace(qualificationTestPack, "/company/country", "/company/country", 1)
	// Change exact pack bytes without changing collection identity. All raw bytes
	// remain exact even when parsed fact/evidence inventories are unchanged.
	pack += "\n"
	assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "mapped.json"), []byte(pack), 0600))
	packChanged := completeEnrollmentSnapshot(t, s, next.Document.ID, "example")
	if packChanged.PackJSON != pack || packChanged.Revision != changed.Revision || reflect.DeepEqual(packChanged, changed) {
		t.Fatal("rooted exact pack change missed")
	}
	// Hand-authored declared target/evidence changes drive their own final fields.
	raw := []byte(`{"rule":{"op":"fact","path":"/company/new","value":true},"evidenceRequirements":[{"id":"new-proof"}]}`)
	assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "mapped.json"), raw, 0600))
	declared := completeEnrollmentSnapshot(t, s, next.Document.ID, "example")
	if reflect.DeepEqual(declared.FactTargets, changed.FactTargets) || !reflect.DeepEqual(declared.EvidenceRequirements, []string{"new-proof"}) {
		t.Fatal("source pack inventory not rebuilt")
	}
}
func TestAssistantDialectCompleteEnrollmentRawControls(t *testing.T) {
	s, e, saved := completeEnrollmentExecution(t)
	run, op, read := completeEnrollmentRead(t, s, e, saved, "complete-controls")
	for _, change := range []func(*assistantDialectCompleteReadCustody){
		func(h *assistantDialectCompleteReadCustody) { h.Intent.ResultSHA256 = strings.Repeat("f", 64) },
		func(h *assistantDialectCompleteReadCustody) { h.Revision = strings.Repeat("f", 64) },
		func(h *assistantDialectCompleteReadCustody) { h.PackDigest = "sha256:" + strings.Repeat("f", 64) },
		func(h *assistantDialectCompleteReadCustody) { h.MetadataSHA256 = strings.Repeat("f", 64) },
		func(h *assistantDialectCompleteReadCustody) { h.Receipt = nil },
	} {
		var bad assistantRunRecord
		assistantRunMust(t, json.Unmarshal(assistantChatJSON(run), &bad))
		change(&bad.DialectReads[0])
		if validateAssistantDialectCompleteReadCustody(bad) == nil {
			t.Fatal("altered original root custody accepted")
		}
	}
	var reencoded assistantRunRecord
	assistantRunMust(t, json.Unmarshal(assistantChatJSON(run), &reencoded))
	root := assistantDialectOperation(reencoded, op.ID)
	root.Intent = append(root.Intent, '\n')
	if validateAssistantDialectCompleteReadCustody(reencoded) == nil {
		t.Fatal("changed exact original root intent spelling accepted")
	}
	// No copied saved row can acquire a fresh live admission.
	before := assistantRunLedger{Runs: []assistantRunRecord{run}}
	var forged assistantRunLedger
	assistantRunMust(t, json.Unmarshal(assistantChatJSON(before), &forged))
	copyRow := forged.Runs[0].DialectReads[0]
	copyRow.ReadOperationID = "another-root"
	forged.Runs[0].DialectReads = append(forged.Runs[0].DialectReads, copyRow)
	if validateAssistantDialectArtifactOwnerTransition(before, forged, nil) == nil {
		t.Fatal("saved root row reminted admission")
	}
	if _, err := (&assistantDialectCompletedReadEvidence{}).completeDialectSnapshot(t.Context(), run, op, read); err == nil {
		t.Fatal("detached saved data became source owner")
	}
}

func TestAssistantDialectCompleteEnrollmentNullAndSelectedMapping(t *testing.T) {
	s, _, saved := completeEnrollmentExecution(t)
	path := filepath.Join(s.projectDir, ".desk-private", "input-preparations", saved.Document.ID+".json")
	raw, err := os.ReadFile(path)
	assistantRunMust(t, err)
	raw = bytes.Replace(raw, []byte(`"Empty":""`), []byte(`"Empty":"","OriginalNull":null`), 1)
	assistantRunMust(t, os.WriteFile(path, raw, 0600))
	snapshot := completeEnrollmentSnapshot(t, s, saved.Document.ID, "example")
	foundNull, foundEmpty := false, false
	for _, column := range snapshot.Columns {
		if column.Name == "OriginalNull" {
			foundNull = len(column.Samples) == 1 && column.Samples[0].ValueJSON == "null"
		}
		if column.Name == "Empty" {
			foundEmpty = len(column.Samples) == 1 && column.Samples[0].ValueJSON == `""`
		}
	}
	if !foundNull || !foundEmpty {
		t.Fatal("null/empty original cells collapsed")
	}
	// Handwritten selected case template; its original lexical token becomes
	// RetainedRead without selecting another occurrence or inventing a frame.
	saved, err = s.preparationRepository().Get(t.Context(), saved.Document.ID)
	assistantRunMust(t, err)
	doc := saved.Document
	selection := inputRetainedContract()
	profile := selection.Profile
	doc.Recipe.Qualification.ConversionContract = &selection
	doc.Recipe.Qualification.Mapping = inputRecordDialect{Version: 2, Profile: &profile, Sources: []json.RawMessage{}, Case: &inputCaseDialect{Facts: []inputDialectFact{{Target: "/company/country", Source: "/retained", Conversion: &inputRetainedConversion{ContractVersion: "1", Column: "Country", Kind: "text"}}}, Evidence: []inputDialectEvidence{{Requirement: "license", Source: "/retained", Availability: &inputRetainedAvailability{ContractVersion: "1", Column: "License"}}}}}
	doc.Recipe.Qualification.Conversions = map[string]inputConversion{"/company/country": {Kind: "text"}}
	selected, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
	assistantRunMust(t, err)
	current := completeEnrollmentSnapshot(t, s, selected.Document.ID, "example")
	if current.RetainedRead == nil || current.DialectJSON == nil || current.RetainedRead.DocumentJSON != *current.DialectJSON || current.RetainedRead.Occurrence.Kind != "case" || current.RetainedRead.Occurrence.Name != nil || current.RetainedRead.FramePointer != "/retained" {
		t.Fatal("selected occurrence/current template not source derived")
	}
}

func TestAssistantDialectCompleteEnrollmentOriginalInventorySnapshot(t *testing.T) {
	var originals []runcontext.DialectSnapshot
	for _, count := range []int{1, 2} {
		s, saved := completeReadSourceFixture(t, count, 1, 20)
		assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "jpack.json"), []byte(`{"packs":{"example":{"path":"mapped.json"}}}`), 0600))
		assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "mapped.json"), []byte(qualificationTestPack), 0600))
		snapshot := completeEnrollmentSnapshot(t, s, saved.Document.ID, "example")
		if snapshot.ResearchIndex == nil || len(snapshot.ResearchIndex.Revisions) != count || snapshot.RecordCount != int64(count) {
			t.Fatal("original inventory replaced by current recipe")
		}
		if count == 1 {
			found := false
			for _, column := range snapshot.Columns {
				if strings.HasPrefix(column.Name, "fa") {
					found = column.Research != nil && column.Research.Revision == snapshot.ResearchIndex.Revisions[0].Revision
				}
			}
			if !found {
				t.Fatal("single original declaration not source derived")
			}
		}
		originals = append(originals, snapshot)
	}
	if reflect.DeepEqual(originals[0].ResearchIndex, originals[1].ResearchIndex) {
		t.Fatal("different complete source inventory reconstructed unchanged")
	}
}

func TestAssistantDialectCompleteEnrollmentExactOriginalPages(t *testing.T) {
	s, e, saved := completeEnrollmentExecution(t)
	doc := saved.Document
	doc.Records = []preparedInputRecord{}
	doc.Recipes = []inputRecipeRevision{}
	research, err := s.inputResearchRoot(true)
	assistantRunMust(t, err)
	for i, name := range []string{"score", "rating"} {
		recipe := researchRecipeFixture()
		recipe.Fields = []inputResearchField{{Name: name, Description: "Original " + name + " <&>", Type: "text"}}
		runID := "research_" + strings.Repeat([]string{"1", "2"}[i], 32)
		journal := inputResearchRun{Version: 1, ID: runID, PreparationID: saved.Document.ID, Revision: saved.SHA256, RecipeVersion: i + 1, Recipe: recipe, State: "completed", CreatedAt: saved.Document.CreatedAt, UpdatedAt: saved.Document.UpdatedAt, Intent: "Retain original inventory", Seeds: []string{}, Companies: []inputResearchEntity{}, Records: []inputResearchEntity{}, Sources: []inputResearchSource{}, Attempts: []inputResearchAttempt{}}
		dir, err := openInputResearch(research, runID, true)
		assistantRunMust(t, err)
		assistantRunMust(t, (fileResearchJournal{root: dir, id: runID}).Create(t.Context(), journal))
		assistantRunMust(t, dir.Close())
		doc.Recipe.Research = &recipe
		doc.RecipeVersion = i + 1
		doc.Recipes = append(doc.Recipes, inputRecipeRevision{Version: i + 1, Name: doc.Name, Recipe: doc.Recipe, At: doc.CreatedAt})
		record := saved.Document.Records[0]
		record.ID = "row_" + strings.Repeat([]string{"1", "2"}[i], 64)
		record.Class, record.SourceID = "generated", runID
		record.Research = &inputResearchRecord{RunID: runID, RecipeVersion: i + 1}
		doc.Records = append(doc.Records, record)
	}
	assistantRunMust(t, research.Close())
	saved, err = s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
	assistantRunMust(t, err)
	registry, err := e.assistantDialectTools()
	assistantRunMust(t, err)
	var readTool runcontext.HostTool
	for _, tool := range registry.tools {
		if tool.Name == "read_dialect" {
			readTool = tool
		}
	}
	invoke := func(call string, args map[string]any) runcontext.ToolResult {
		t.Helper()
		raw := assistantChatJSON(args)
		assistantPolicyEvent(t, e, call, "read_dialect", raw)
		ctx := context.WithValue(runcontext.WithCallID(e.generationContext(t.Context(), 1), call), assistantHostRootKey{}, true)
		result, e := readTool.Execute(ctx, raw)
		assistantRunMust(t, e)
		return result
	}
	args := map[string]any{"id": saved.Document.ID, "packKey": "example", "cursor": 0}
	if result := invoke("page-before-root", args); !result.Err() {
		t.Fatal("page without actual original read accepted")
	}
	delete(args, "cursor")
	if result := invoke("page-root", args); result.Err() {
		t.Fatal("original read failed", string(result.Value().JSON()))
	}
	args["cursor"] = 0
	page := invoke("page-zero", args)
	if page.Err() || !bytes.Contains(page.Value().JSON(), []byte("fieldJson")) {
		t.Fatal("exact original page unavailable", string(page.Value().JSON()))
	}
	run, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	if len(run.DialectReads) != 1 || run.DialectReads[0].Receipt == nil || run.DialectReads[0].ResearchIndex == nil || len(run.DomainOutputs) != 0 {
		t.Fatal("page minted another root receipt or proposal")
	}
	completeEnrollmentPageAssertions(t, s, e, run, page)
	args["cursor"] = 1
	if tail := invoke("page-one", args); tail.Err() {
		t.Fatal("second revision page failed", tail.Value().Text())
	}
	args["cursor"] = 2
	if result := invoke("page-after-end", args); !result.Err() {
		t.Fatal("cursor past exact inventory accepted")
	}
	args["cursor"] = 0
	args["id"] = "prep_" + strings.Repeat("f", 32)
	if result := invoke("page-foreign", args); !result.Err() {
		t.Fatal("foreign resource selected previous raw")
	}
	args["id"] = saved.Document.ID
	changed := saved.Document
	changed.Name = "Changed after original read"
	_, err = s.preparationRepository().CompareAndSwap(t.Context(), changed, saved.SHA256)
	assistantRunMust(t, err)
	if result := invoke("page-stale", args); !result.Err() {
		t.Fatal("changed source paged old raw as current")
	}
	for _, cursor := range []string{"null", "-1", "1.5", "1000001", "\"0\""} {
		raw := []byte(`{"id":"` + saved.Document.ID + `","packKey":"example","cursor":` + cursor + `}`)
		if _, err := readAssistantDialectArguments("read_dialect", raw); err == nil {
			t.Fatal("malformed/outside cursor accepted", cursor)
		}
	}
}

func TestAssistantDialectCompleteEnrollmentRootedInputCurrentness(t *testing.T) {
	s, _, saved := completeEnrollmentExecution(t)
	project, err := s.acquireAssistantDialectCompleteProject(t.Context(), "example")
	assistantRunMust(t, err)
	assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "mapped.json"), []byte(qualificationTestPack+"\n"), 0600))
	if project.Check(t.Context()) == nil {
		t.Fatal("changed held project input remained current")
	}
	assistantRunMust(t, project.Close(t.Context()))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "jpack.json"), []byte(`{"packs":{"example":{"path":"mapped.json"},"other":{"path":"mapped.json"}}}`), 0600))
	other := completeEnrollmentSnapshot(t, s, saved.Document.ID, "other")
	if other.ID != saved.Document.ID || other.PackKey != "other" || other.PackJSON != qualificationTestPack+"\n" {
		t.Fatal("selected root pack identity not source derived")
	}
}

func TestAssistantDialectCompleteEnrollmentReadbackCloseUncertain(t *testing.T) {
	s, e, saved := completeEnrollmentExecution(t)
	registry, err := e.assistantDialectTools()
	assistantRunMust(t, err)
	var tool runcontext.HostTool
	for _, candidate := range registry.tools {
		if candidate.Name == "read_dialect" {
			tool = candidate
		}
	}
	failed := false
	testHookDialectArtifactOwnerStep = func(step string) error {
		if step == "complete-read-raw.file-close" && !failed {
			failed = true
			return errAssistantDialectCompleteRead
		}
		return nil
	}
	t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
	invoke := func(call string) runcontext.ToolResult {
		t.Helper()
		raw := assistantChatJSON(map[string]string{"id": saved.Document.ID, "packKey": "example"})
		assistantPolicyEvent(t, e, call, "read_dialect", raw)
		ctx := context.WithValue(runcontext.WithCallID(e.generationContext(t.Context(), 1), call), assistantHostRootKey{}, true)
		result, err := tool.Execute(ctx, raw)
		assistantRunMust(t, err)
		return result
	}
	if result := invoke("read-close-uncertain"); !result.Err() {
		t.Fatal("readback Close uncertainty acknowledged")
	}
	testHookDialectArtifactOwnerStep = nil
	run, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	if !failed || len(run.DialectReads) != 1 || run.DialectReads[0].Receipt != nil {
		t.Fatal("uncertain original intent not retained")
	}
	ref := run.DialectReads[0].Intent.Reference
	raw, err := s.readAssistantDialectCompleteNativeRaw(t.Context(), ref)
	assistantRunMust(t, err)
	if len(raw) == 0 {
		t.Fatal("uncertain exact raw was removed")
	}
	if result := invoke("read-after-uncertain"); !result.Err() {
		t.Fatal("later read promoted/repaired uncertain original")
	}
	after, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	if len(after.DialectReads) != 1 || after.DialectReads[0].Receipt != nil {
		t.Fatal("uncertainty became authority")
	}
}

func TestAssistantDialectCompleteEnrollmentPlanReadNoProposalAuthority(t *testing.T) {
	checked := make(chan *assistantRunExecution, 1)
	s, _, e := assistantExecutionFixtureSource(t, "http://127.0.0.1:1", "plan", nil, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
		s.testAssistantRunMonitorChecked = func(ctx context.Context, e *assistantRunExecution) {
			select {
			case checked <- e:
			case <-ctx.Done():
				return
			}
			<-ctx.Done()
		}
		return start
	})
	select {
	case got := <-checked:
		if got != e {
			t.Fatal("foreign monitored run")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("monitor did not release private owner")
	}
	saved := assistantMappingSeed(t, s)
	e.canonicalMu.Lock()
	e.generation, e.lifecycle = 1, "running"
	e.canonicalMu.Unlock()
	run, _, read := completeEnrollmentRead(t, s, e, saved, "plan-complete-read")
	if run.Binding.Phase != "plan" || len(run.DialectReads) != 1 || run.DialectReads[0].ResearchIndex == nil || read.Complete == nil {
		t.Fatal("actual Plan read lost original phase")
	}
	registry, err := e.assistantDialectTools()
	assistantRunMust(t, err)
	raw := []byte(assistantMappingArguments(saved, "propose_dialect"))
	assistantPolicyEvent(t, e, "plan-cannot-propose", "propose_dialect", raw)
	ctx := context.WithValue(runcontext.WithCallID(e.generationContext(t.Context(), 1), "plan-cannot-propose"), assistantHostRootKey{}, true)
	for _, tool := range registry.tools {
		if tool.Name == "propose_dialect" {
			result, err := tool.Execute(ctx, raw)
			assistantRunMust(t, err)
			if !result.Err() {
				t.Fatal("Plan raw receipt authorized proposal")
			}
		}
	}
	after, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	if len(after.DialectDrafts) != 0 || len(after.DomainOutputs) != 0 {
		t.Fatal("Plan produced work output")
	}
}

func completeEnrollmentPageAssertions(t *testing.T, s *Server, e *assistantRunExecution, run assistantRunRecord, page runcontext.ToolResult) {
	t.Helper()
	var pageOp assistantRunOperation
	var receipt assistantHostResult
	for _, op := range run.Operations {
		if op.Kind != "host" || op.Name != "read_dialect" {
			continue
		}
		var in assistantHostIntent
		assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
		if op.Name == "read_dialect" && bytes.Contains(in.Arguments, []byte(`"cursor":0`)) && op.State == "completed" {
			pageOp = op
			assistantRunMust(t, readAssistantHostReceipt(op.Result, &receipt))
		}
	}
	if pageOp.ID == "" || receipt.DialectPage == nil || receipt.DialectRead != nil || len(receipt.DialectPage.FieldIndexes) != 2 || !bytes.Equal(receipt.Result, page.Value().JSON()) {
		t.Fatal("durable crossing-revision page proof missing")
	}
	parsed, err := assistantDialectCompletePageValue(receipt.Result)
	assistantRunMust(t, err)
	if len(parsed.Declarations) != 2 || parsed.Declarations[0].Name != "score" || parsed.Declarations[1].Name != "rating" || parsed.NextCursor != nil {
		t.Fatal("original ordered declarations changed")
	}
	root := assistantDialectOperation(run, receipt.DialectPage.ReadOperationID)
	if root == nil {
		t.Fatal("page root missing")
	}
	selected, _, err := assistantDialectReadOperation(run, "propose_dialect", assistantDialectArguments{ID: parsed.ID, PackKey: parsed.PackKey})
	assistantRunMust(t, err)
	if selected.ID != root.ID {
		t.Fatal("page became proposal root")
	}
	// Repeated observation reuses the persisted exact page, without minting a
	// second root or selecting another registry's latest matching observation.
	registry, err := e.assistantDialectTools()
	assistantRunMust(t, err)
	var in assistantHostIntent
	assistantRunMust(t, readAssistantHostReceipt(pageOp.Intent, &in))
	ctx := context.WithValue(runcontext.WithCallID(e.generationContext(t.Context(), 1), in.CallID), assistantHostRootKey{}, true)
	for _, tool := range registry.tools {
		if tool.Name == "read_dialect" {
			replayed, err := tool.Execute(ctx, in.Arguments)
			assistantRunMust(t, err)
			if replayed.Err() || !bytes.Equal(replayed.Value().JSON(), receipt.Result) {
				t.Fatal("page exact replay changed")
			}
		}
	}
	// A new call cannot borrow another registry or generation's source-minted
	// selection even when the same resource has a valid stored root.
	for _, generation := range []uint64{1, 2} {
		call := "new-registry-page"
		if generation == 2 {
			call = "foreign-generation-page"
		}
		assistantPolicyEvent(t, e, call, "read_dialect", in.Arguments)
		ctx := context.WithValue(runcontext.WithCallID(e.generationContext(t.Context(), generation), call), assistantHostRootKey{}, true)
		for _, tool := range registry.tools {
			if tool.Name == "read_dialect" {
				result, err := tool.Execute(ctx, in.Arguments)
				assistantRunMust(t, err)
				if !result.Err() {
					t.Fatal("borrowed registry/generation page selection accepted")
				}
			}
		}
	}
	data, err := s.openChatData()
	assistantRunMust(t, err)
	stored, err := readPrivateData(data.root, e.writer.lease.store.name, assistantRunRecordLimit)
	assistantRunMust(t, err)
	assistantRunMust(t, data.root.Close())
	ledger, err := decodeAssistantRunStorage(e.writer.lease.store.name, stored)
	assistantRunMust(t, err)
	mutations := map[string]func(*assistantHostResult){
		"missing receipt": func(r *assistantHostResult) { r.DialectPage = nil },
		"wrong root":      func(r *assistantHostResult) { r.DialectPage.ReadOperationID = pageOp.ID },
		"mixed root": func(r *assistantHostResult) {
			var original assistantHostResult
			assistantRunMust(t, readAssistantHostReceipt(root.Result, &original))
			r.DialectRead = original.DialectRead
		},
		"missing proof":           func(r *assistantHostResult) { r.DialectPage.FieldIndexes = nil },
		"wrong proof entry":       func(r *assistantHostResult) { r.DialectPage.FieldIndexes[0].Entry++ },
		"changed canonical proof": func(r *assistantHostResult) { r.DialectPage.FieldIndexes[0].FieldIndex.Fields[0].Name = "altered" },
		"changed proof hash": func(r *assistantHostResult) {
			r.DialectPage.FieldIndexes[0].FieldIndex.Fields[0].Digest = "sha256:" + strings.Repeat("f", 64)
		},
	}
	for name, alter := range map[string]func(*assistantDialectCompletePage){
		"valid shaped declaration tamper": func(p *assistantDialectCompletePage) {
			p.Declarations[0].FieldJSON = `{"name":"score","description":"Tampered valid data","type":"text"}`
		},
		"declaration name": func(p *assistantDialectCompletePage) { p.Declarations[0].Name = "other" },
		"cursor":           func(p *assistantDialectCompletePage) { p.Cursor = 1 },
		"next cursor":      func(p *assistantDialectCompletePage) { v := 1; p.NextCursor = &v },
		"inventory digest": func(p *assistantDialectCompletePage) { p.InventoryDigest = "sha256:" + strings.Repeat("f", 64) },
	} {
		mutations[name] = func(r *assistantHostResult) {
			p, err := assistantDialectCompletePageValue(r.Result)
			assistantRunMust(t, err)
			alter(&p)
			r.Result = runcontext.ResultOf([]runcontext.Content{runcontext.TextContent(string(assistantChatJSON(p)))}, nil).Value().JSON()
		}
	}
	for name, alter := range mutations {
		t.Run(name, func(t *testing.T) {
			var bad assistantRunLedger
			assistantRunMust(t, json.Unmarshal(assistantChatJSON(ledger), &bad))
			for ri := range bad.Runs {
				for oi := range bad.Runs[ri].Operations {
					op := &bad.Runs[ri].Operations[oi]
					if op.ID != pageOp.ID {
						continue
					}
					var changed assistantHostResult
					assistantRunMust(t, readAssistantHostReceipt(op.Result, &changed))
					alter(&changed)
					op.Result = assistantChatJSON(changed)
				}
			}
			if _, err := decodeAssistantRunStorage(e.writer.lease.store.name, assistantChatJSON(bad)); err == nil {
				t.Fatal("tampered page accepted by strict saved-data decoder")
			}
		})
	}
	// A legacy root remains a readable observation, but cannot authenticate a
	// page even when its old-format receipt and selector are internally valid.
	var legacy assistantRunLedger
	assistantRunMust(t, json.Unmarshal(assistantChatJSON(ledger), &legacy))
	for ri := range legacy.Runs {
		if legacy.Runs[ri].ID == run.ID {
			completeEnrollmentLegacyRoot(t, &legacy.Runs[ri])
		}
	}
	if _, err := decodeAssistantRunStorage(e.writer.lease.store.name, assistantChatJSON(legacy)); err == nil {
		t.Fatal("legacy no-index root silently upgraded to page proof")
	}
	// The optional proof is closed even when its typed DTO could fill defaults.
	for _, replacement := range []string{`"fieldIndexes":null`, `"fieldIndexes":[],"unexpected":1`} {
		changed := receipt
		changed.DialectPage = &assistantDialectCompletePageReceipt{Version: 1, ReadOperationID: root.ID, FieldIndexes: []assistantDialectCompleteFieldProof{}}
		raw := bytes.Replace(assistantChatJSON(changed), []byte(`"fieldIndexes":[]`), []byte(replacement), 1)
		var typed assistantHostResult
		if readAssistantHostReceipt(raw, &typed) == nil {
			op := pageOp
			op.Result = raw
			if validateAssistantDialectCompletePageReceipt(run, op, in, typed) == nil {
				t.Fatal("open or null proof accepted")
			}
		}
	}
}

func completeEnrollmentLegacyRoot(t *testing.T, run *assistantRunRecord) {
	t.Helper()
	held := &run.DialectReads[0]
	held.ResearchIndex = nil
	root := assistantDialectOperation(*run, held.ReadOperationID)
	var in assistantHostIntent
	assistantRunMust(t, readAssistantHostReceipt(root.Intent, &in))
	held.Intent.Receipt = assistantDialectCompleteReadReceipt(*run, *held, in)
	ack := held.Intent
	held.Receipt = &ack
	var result assistantHostResult
	assistantRunMust(t, readAssistantHostReceipt(root.Result, &result))
	result.DialectRead.Complete.Receipt = held.Intent.Receipt
	root.Result = assistantChatJSON(result)
}
