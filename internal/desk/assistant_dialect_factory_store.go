package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

// The raw port accepts the selected serializer's complete bytes once. It never
// reserializes raw objects for storage or puts their bodies into the run ledger.
const assistantDialectDraftByteLimit = 8 << 20
const assistantDialectFinalByteLimit = 4 << 20

// The finite shape reader flattens the package's embedded identity/candidate.
// It preserves open report values and exact inner JSON strings. Nullable fields
// are the package's explicit null-bearing fields, never optional controls.
func readAssistantDialectFactoryShape(raw []byte, shape reflect.Type, nullable bool, version int) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if nullable {
			return nil
		}
		return errAssistantRunInvalid
	}
	for shape.Kind() == reflect.Pointer {
		shape = shape.Elem()
	}
	switch shape.Kind() {
	case reflect.Struct:
		fields, err := assistantTestObject(raw)
		if err != nil {
			return err
		}
		known := map[string]reflect.StructField{}
		var collect func(reflect.Type)
		collect = func(t reflect.Type) {
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				if f.Anonymous && f.Tag.Get("json") == "" {
					collect(f.Type)
					continue
				}
				name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
				known[name] = f
			}
		}
		collect(shape)
		// The package's current struct carries both versions for compatibility;
		// persisted objects have exactly one closed candidate shape.
		if shape == reflect.TypeOf(runcontext.DialectCandidate{}) || shape == reflect.TypeOf(runcontext.DialectReviewProposal{}) {
			if version == 1 {
				delete(known, "researchIndex")
			} else {
				delete(known, "researchDeclarations")
			}
		}
		for name, f := range known {
			value, ok := fields[name]
			if !ok {
				if name == "researchDeclarations" || !strings.Contains(f.Tag.Get("json"), ",omitempty") {
					return errAssistantRunInvalid
				}
				continue
			}
			null := name == "previousReceipt" || name == "dialectDigest" || name == "retainedRead" || name == "researchDeclarations" || name == "researchIndex" || shape == reflect.TypeOf(runcontext.DialectResearchRevision{}) && name == "fields" || shape == reflect.TypeOf(runcontext.DialectIndexRevision{}) && (name == "fields" || name == "digest" || name == "bytes")
			if err = readAssistantDialectFactoryShape(value, f.Type, null, version); err != nil {
				return err
			}
		}
		for name := range fields {
			if _, ok := known[name]; !ok {
				return errAssistantRunInvalid
			}
		}
	case reflect.Slice:
		rows, err := assistantChatArray(raw)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err = readAssistantDialectFactoryShape(row, shape.Elem(), false, version); err != nil {
				return err
			}
		}
	case reflect.Map:
		fields, err := assistantTestObject(raw)
		if err != nil {
			return err
		}
		for _, value := range fields {
			if err = readAssistantDialectFactoryShape(value, shape.Elem(), shape.Elem().Kind() == reflect.Interface, version); err != nil {
				return err
			}
		}
	case reflect.Interface:
		if !json.Valid(raw) {
			return errAssistantRunInvalid
		}
	default:
		if json.Unmarshal(raw, reflect.New(shape).Interface()) != nil {
			return errAssistantRunInvalid
		}
	}
	return nil
}

func assistantDialectFactoryDecode(raw []byte, limit int, out any) error {
	if err := assistantDialectFactoryRawGuard(raw, limit); err != nil {
		return err
	}
	fields, err := assistantTestObject(raw)
	if err != nil {
		return err
	}
	var version int
	if json.Unmarshal(fields["version"], &version) != nil || version != 1 && version != 2 {
		return errAssistantRunInvalid
	}
	if err := readAssistantDialectFactoryShape(raw, reflect.TypeOf(out).Elem(), false, version); err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
func assistantDialectFactoryRawGuard(raw []byte, limit int) error {
	if len(raw) == 0 || len(raw) > limit || !validUTF8(raw) || jsonscalar.ValidateEscapes(raw) != nil {
		return errAssistantRunInvalid
	}
	return nil
}
func assistantDialectFactoryDigest(raw []byte) (string, error) {
	v, err := runcontext.ParseJSON(raw)
	if err != nil {
		return "", err
	}
	return "sha256:" + digestOf([]byte(runcontext.CanonicalJSON(v))), nil
}

// The raw candidate is the versioned identity. Marshaling the current Go
// type would drop v1's null/empty declarations and add researchIndex:null.
func assistantDialectFactoryRawCandidate(raw []byte, final bool) ([]byte, error) {
	fields, err := assistantTestObject(raw)
	if err != nil {
		return nil, err
	}
	if !final {
		return fields["candidate"], nil
	}
	for _, name := range []string{"draft", "version", "fingerprint", "mappingDigest", "status", "validation", "coverage", "preview"} {
		delete(fields, name)
	}
	return assistantChatEncodeObject(fields), nil
}

func readAssistantDialectFactoryDraft(raw []byte) (runcontext.DialectDraftCheckpoint, string, error) {
	var cp runcontext.DialectDraftCheckpoint
	if err := assistantDialectFactoryDecode(raw, assistantDialectDraftByteLimit, &cp); err != nil {
		return cp, "", err
	}
	if (cp.Version != 1 && cp.Version != 2) || !assistantDialectAttempt(cp.AttemptID) || !assistantDialectPackageDigest(cp.Fingerprint) || !assistantDialectPackageDigest(cp.Digest) || cp.Stage != "candidate" && cp.Stage != "checked" || cp.Stage == "candidate" && (cp.PreviousReceipt != nil || cp.Check != nil) || cp.Stage == "checked" && (cp.PreviousReceipt == nil || !assistantRunText(*cp.PreviousReceipt, 128) || cp.Check == nil || cp.Check.Results == nil || len(cp.Check.Results) > runcontext.DialectRuntimeResults) {
		return cp, "", errAssistantRunInvalid
	}
	if err := validateAssistantDialectFactoryCandidate(cp.Candidate); err != nil {
		return cp, "", err
	}
	fields, err := assistantTestObject(raw)
	if err != nil {
		return cp, "", err
	}
	// Count the selected serializer's complete candidate, including escapes;
	// json.Marshal of a decoded TS object would measure a different spelling.
	if len(fields["candidate"]) > runcontext.DialectCandidateBytes {
		return cp, "", errAssistantRunInvalid
	}
	delete(fields, "digest")
	digest, err := assistantDialectFactoryDigest(assistantChatEncodeObject(fields))
	if err != nil || digest != cp.Digest {
		return cp, "", errAssistantRunInvalid
	}
	candidateFingerprint, err := assistantDialectFactoryDigest(fields["candidate"])
	if err != nil {
		return cp, "", err
	}
	fingerprint, err := assistantDialectFactoryDigest(assistantChatJSON(map[string]any{"attemptId": cp.AttemptID, "candidate": json.RawMessage(fields["candidate"])}))
	if err != nil || fingerprint != cp.Fingerprint {
		return cp, "", errAssistantRunInvalid
	}
	if cp.Check != nil {
		for _, result := range cp.Check.Results {
			if !contains([]string{"dialect_validate", "dialect_check"}, result.Operation) || len(result.ResultJSON) > runcontext.DialectRuntimeBytes || !validUTF8([]byte(result.ResultJSON)) || jsonscalar.ValidateEscapes([]byte(result.ResultJSON)) != nil || !json.Valid([]byte(result.ResultJSON)) {
				return cp, "", errAssistantRunInvalid
			}
		}
		if cp.Check.Failure != nil && (!assistantRunText(cp.Check.Failure.Code, 128) || !assistantRunText(cp.Check.Failure.Message, 4096)) {
			return cp, "", errAssistantRunInvalid
		}
	}
	return cp, candidateFingerprint, nil
}

func validateAssistantDialectFactoryCandidate(c runcontext.DialectCandidate) error {
	if !assistantRunText(c.ID, 512) || !assistantRunText(c.Revision, 128) || !assistantRunText(c.PackKey, 512) || !assistantDialectPackageDigest(c.PackDigest) || c.DialectDigest != nil && !assistantDialectPackageDigest(*c.DialectDigest) || c.ConversionContract != runcontext.DialectRetainedColumnDraft() || len(c.DocumentJSON) > runcontext.DialectDocumentBytes || len(c.PackJSON) > runcontext.DialectPackBytes || c.Columns == nil || len(c.Columns) > runcontext.DialectColumns || c.Facts == nil || len(c.Facts) > runcontext.DialectBindings || c.Evidence == nil || len(c.Evidence) > runcontext.DialectBindings || c.Conversions == nil || strings.TrimSpace(c.Summary) == "" || len(c.Summary) > 4000 {
		return errAssistantRunInvalid
	}
	return nil
}

func assistantDialectFactoryDraftMatches(held assistantDialectDraftCustody, cp runcontext.DialectDraftCheckpoint, candidateFingerprint string) bool {
	return held.AttemptID == cp.AttemptID && held.DraftFingerprint == cp.Fingerprint && held.CandidateFingerprint == candidateFingerprint
}
func assistantDialectFactoryDraftAck(held assistantDialectDraftCustody, stage string) runcontext.DialectDraftRetention {
	i := assistantDialectDraftSlotOf(&held, stage).Receipt
	return runcontext.DialectDraftRetention{Fingerprint: held.DraftFingerprint, Stage: stage, Digest: i.PackageDigest, PreviousReceipt: i.PreviousReceipt, Receipt: i.Receipt, Status: "draft"}
}

// Created only by the host's later integration. No factory or route uses it in
// this private slice. The host snapshot enrollment remains a cutover prerequisite.
type assistantDialectFactoryStore struct {
	execution *assistantRunExecution
	host      string
	objects   retainedArtifactRepository
	evidence  assistantDialectFactoryReadEvidence
	put       assistantDialectFactoryPutAuthority
}

// Required owner capability, implemented by the concrete private owner in this inactive
// slice. Plain repository.Put or before/after callbacks cannot prove a live
// source/phase/executor fence after the artifact queue. The owner must hold its
// complete forward-order authority guard through exact publication/readback.
type assistantDialectFactoryPutAuthority interface {
	putDialectDraft(context.Context, retainedArtifactRef, []byte) (retainedArtifactRef, error)
}

func (s *assistantDialectFactoryStore) current(ctx context.Context) (assistantRunRecord, assistantHostIntent, assistantDialectRead, error) {
	e := s.execution
	if e == nil || e.writer == nil || s.objects == nil || s.evidence == nil || s.put == nil {
		return assistantRunRecord{}, assistantHostIntent{}, assistantDialectRead{}, errAssistantRunScope
	}
	if err := e.Authorize(ctx); err != nil {
		return assistantRunRecord{}, assistantHostIntent{}, assistantDialectRead{}, err
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return run, assistantHostIntent{}, assistantDialectRead{}, err
	}
	in, read, err := assistantDialectDraftRoot(run, s.host)
	op := assistantDialectOperation(run, s.host)
	if err != nil || run.Historical || assistantRunTerminal(run.State) || run.Checkpoint == nil || run.Checkpoint.Status != "running" || op == nil || op.State != "dispatched" {
		return run, in, read, errAssistantRunScope
	}
	owner, ok := s.put.(*assistantDialectDraftPutOwner)
	if !ok || !owner.original() || owner.factory != s || !assistantDialectDraftOwnerMatches(run, owner.enrollment.Owner) {
		return run, in, read, errAssistantRunScope
	}
	return run, in, read, nil
}

// The caller holds w.change's private journal lock. Re-read the captured graph
// independently, replace only this run, and test the full pending AND completed
// serialized journals before admitting an object effect.
func (s *assistantDialectFactoryStore) preflightLocked(ctx context.Context, run assistantRunRecord, stage string) error {
	store := s.execution.writer.lease.store
	data, err := store.server.openChatData()
	if err != nil {
		return err
	}
	defer data.root.Close()
	ledger, err := store.readLocked(ctx, data)
	if err != nil {
		return err
	}
	if err = assistantDialectDraftFence(ledger, run, s.host); err != nil {
		return err
	}
	for i := range ledger.Runs {
		if ledger.Runs[i].ID != run.ID {
			continue
		}
		run.Revision++ // w.change persists this same next revision.
		ledger.Runs[i] = run
		_, pending, err := prepareAssistantRunStorage(store.name, ledger)
		if err != nil {
			return err
		}
		completed := &pending.Runs[i]
		completed.Revision++ // The separate receipt CAS advances the revision again.
		slot := assistantDialectDraftSlotOf(assistantDialectDraftOf(completed, s.host), stage)
		intent := *slot.Intent
		slot.Receipt = &intent
		_, _, err = prepareAssistantRunStorage(store.name, pending)
		return err
	}
	return errAssistantRunInvalid
}

// retain reserves once, then writes once. A pending intent is never a retry
// token, including identical deliveries. Committed duplicates reread the object.
func (s *assistantDialectFactoryStore) retain(ctx context.Context, stage string, raw []byte, seed assistantDialectDraftCustody, packageDigest string, previous *string) (assistantDialectDraftCustody, error) {
	raw = bytes.Clone(raw)
	var saved assistantDialectDraftCustody
	run, _, _, err := s.current(ctx)
	if err != nil {
		return saved, err
	}
	expected := assistantDialectDraftIntent{Reference: artifactReference(raw), PackageDigest: packageDigest, PreviousReceipt: previous}
	expected.Receipt = assistantDialectDraftReceipt(run, seed, stage, expected)
	reserved := false
	var reservedRevision uint64
	owner, ok := s.put.(*assistantDialectDraftPutOwner)
	if !ok || !owner.original() || owner.factory != s {
		return saved, errAssistantRunScope
	}
	err = s.execution.writer.changeDialectDraft(ctx, owner, stage, expected, func(run *assistantRunRecord) (bool, error) {
		if run.Historical || assistantRunTerminal(run.State) || run.Checkpoint == nil || run.Checkpoint.Status != "running" {
			return false, errAssistantRunScope
		}
		op := assistantDialectOperation(*run, s.host)
		if op == nil || op.State != "dispatched" {
			return false, errAssistantRunScope
		}
		held := assistantDialectDraftOf(run, s.host)
		if held == nil {
			if stage != "candidate" {
				return false, errAssistantRunDispatch
			}
			run.DialectDrafts = append(run.DialectDrafts, seed)
			held = &run.DialectDrafts[len(run.DialectDrafts)-1]
		}
		if held.AttemptID != seed.AttemptID || held.DraftFingerprint != seed.DraftFingerprint || held.CandidateFingerprint != seed.CandidateFingerprint || held.Uncertainty != "" || held.Reconciled {
			return false, errAssistantRunDispatch
		}
		slot := assistantDialectDraftSlotOf(held, stage)
		if slot == nil {
			return false, errAssistantRunInvalid
		}
		if slot.Intent != nil {
			if !reflect.DeepEqual(*slot.Intent, expected) || slot.Receipt == nil {
				return false, errAssistantRunDispatch
			}
			saved = *held
			return false, nil
		}
		slot.Intent = &expected
		if run.DialectArtifactOwner == "" {
			run.DialectArtifactOwner = owner.enrollment.Owner
		}
		if err := s.preflightLocked(ctx, *run, stage); err != nil {
			return false, err
		}
		saved, reserved = *held, true
		reservedRevision = run.Revision + 1
		return true, nil
	})
	if err != nil {
		return saved, err
	}
	if reserved {
		// Only the original successful reservation may arm the concrete effect
		// owner. Independent durable readback must match the exact committed head.
		owner, ok := s.put.(*assistantDialectDraftPutOwner)
		if !ok || owner.execution != s.execution || owner.host != s.host {
			err = errAssistantRunScope
		} else {
			var committed assistantRunRecord
			committed, err = s.execution.writer.lease.store.Read(ctx, s.execution.record.Binding, s.execution.record.ID)
			if err == nil {
				err = owner.bindReservation(committed, stage, reservedRevision, expected)
			}
		}
		if err == nil {
			_, _, _, err = s.current(ctx)
		}
		if err == nil {
			var ref retainedArtifactRef
			ref, err = s.put.putDialectDraft(ctx, expected.Reference, raw)
			if err == nil && ref != expected.Reference {
				err = errRetainedArtifactChanged
			}
		}
	}
	var stored []byte
	if err == nil {
		stored, err = s.objects.Get(ctx, expected.Reference)
	}
	if err == nil && (!bytes.Equal(stored, raw) || verifyRetainedArtifact(expected.Reference, stored) != nil) {
		err = errRetainedArtifactChanged
	}
	if err != nil {
		_ = s.markUncertain(ctx, "storage-unacknowledged")
		return saved, err
	}
	if reserved {
		observed, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = s.execution.writer.change(observed, true, func(run *assistantRunRecord) (bool, error) {
			if run.DialectArtifactOwner != owner.enrollment.Owner {
				return false, errAssistantRunScope
			}
			held := assistantDialectDraftOf(run, s.host)
			if held == nil {
				return false, errAssistantRunInvalid
			}
			slot := assistantDialectDraftSlotOf(held, stage)
			if slot == nil || !reflect.DeepEqual(slot.Intent, &expected) {
				return false, errAssistantRunConflict
			}
			if slot.Receipt != nil {
				return false, nil
			}
			intent := expected
			slot.Receipt = &intent
			return true, nil
		})
	}
	if err == nil {
		_, _, _, err = s.current(ctx)
	}
	if err != nil {
		_ = s.markUncertain(ctx, "cancelled-after-intent")
		return saved, err
	}
	// The write primitive verified exact manifest bytes. Read the saved graph too;
	// never return a receipt merely held in the mutation callback's memory.
	run, err = s.execution.writer.lease.store.Read(ctx, s.execution.record.Binding, s.execution.record.ID)
	if err != nil {
		_ = s.markUncertain(ctx, "storage-unacknowledged")
		return saved, err
	}
	held := assistantDialectDraftOf(&run, s.host)
	if run.DialectArtifactOwner != owner.enrollment.Owner || held == nil || !reflect.DeepEqual(assistantDialectDraftSlotOf(held, stage).Receipt, &expected) || held.Uncertainty != "" {
		_ = s.markUncertain(ctx, "ack-mismatch")
		return saved, errAssistantRunConflict
	}
	return *held, nil
}

func (s *assistantDialectFactoryStore) retainDraft(ctx context.Context, raw []byte) (runcontext.DialectDraftRetention, error) {
	var ack runcontext.DialectDraftRetention
	cp, candidateFingerprint, err := readAssistantDialectFactoryDraft(raw)
	if err != nil {
		return ack, err
	}
	if cp.Version != 2 {
		return ack, errAssistantRunScope
	}
	run, in, read, err := s.current(ctx)
	if err != nil {
		return ack, err
	}
	snapshot, err := assistantDialectFactoryBoundSnapshot(ctx, s.evidence, run, s.host)
	if err != nil || !assistantDialectFactorySnapshotMatches(snapshot, cp.Candidate) {
		return ack, errAssistantRunScope
	}
	op := assistantDialectOperation(run, s.host)
	a, err := readAssistantDialectArguments(op.Name, in.Arguments)
	if err != nil || a.PackDigest != cp.Candidate.PackDigest || a.Summary != cp.Candidate.Summary || !assistantHostArgumentsMatch(assistantChatJSON(a.Facts), assistantChatJSON(cp.Candidate.Facts)) || !assistantHostArgumentsMatch(assistantChatJSON(a.Evidence), assistantChatJSON(cp.Candidate.Evidence)) {
		return ack, errAssistantRunScope
	}
	seed := assistantDialectDraftCustody{Version: 1, HostOperationID: s.host, ActionID: in.DialectAction.ID, ReadOperationID: in.DialectAction.ReadOperationID, ResourceDigest: assistantDialectResource(read.ID, read.PackKey), AttemptID: cp.AttemptID, CandidateFingerprint: candidateFingerprint, DraftFingerprint: cp.Fingerprint}
	if held := assistantDialectDraftOf(&run, s.host); held != nil {
		if !assistantDialectFactoryDraftMatches(*held, cp, candidateFingerprint) {
			return ack, errAssistantRunConflict
		}
		seed = *held
	}
	if cp.Stage == "checked" {
		if seed.Candidate.Receipt == nil || cp.PreviousReceipt == nil || *cp.PreviousReceipt != seed.Candidate.Receipt.Receipt {
			return ack, errAssistantRunDispatch
		}
		original, err := s.objects.Get(ctx, seed.Candidate.Receipt.Reference)
		if err != nil {
			return ack, err
		}
		if verifyRetainedArtifact(seed.Candidate.Receipt.Reference, original) != nil {
			return ack, errRetainedArtifactChanged
		}
		initial, _, err := readAssistantDialectFactoryDraft(original)
		if err != nil || initial.Version != cp.Version || initial.Stage != "candidate" || initial.Digest != seed.Candidate.Receipt.PackageDigest || !reflect.DeepEqual(initial.Candidate, cp.Candidate) {
			return ack, errAssistantRunConflict
		}
	}
	held, err := s.retain(ctx, cp.Stage, raw, seed, cp.Digest, cp.PreviousReceipt)
	if err != nil {
		return ack, err
	}
	return assistantDialectFactoryDraftAck(held, cp.Stage), nil
}

// The same pure final-object proof is required before live publication and
// historical observation. Raw bytes remain the artifact identity; this helper
// decodes only to validate the selected proposal, never to replace its spelling.
func readAssistantDialectFactoryFinal(raw []byte) (runcontext.DialectReviewProposal, error) {
	var proposal runcontext.DialectReviewProposal
	if err := assistantDialectFactoryDecode(raw, assistantDialectFinalByteLimit, &proposal); err != nil {
		return proposal, err
	}
	if (proposal.Version != 1 && proposal.Version != 2) || proposal.Status != "review-required" || !assistantDialectPackageDigest(proposal.MappingDigest) || validateAssistantDialectFactoryCandidate(proposal.DialectCandidate) != nil || proposal.Validation == nil || proposal.Coverage == nil || proposal.Preview.Fingerprint != proposal.Fingerprint {
		return proposal, errAssistantRunInvalid
	}
	candidateRaw, err := assistantDialectFactoryRawCandidate(raw, true)
	if err != nil {
		return proposal, err
	}
	fingerprint, err := assistantDialectFactoryDigest(candidateRaw)
	if err != nil || fingerprint != proposal.Fingerprint {
		return proposal, errAssistantRunInvalid
	}
	return proposal, nil
}

func (s *assistantDialectFactoryStore) retainFinal(ctx context.Context, raw []byte) (runcontext.DialectRetention, error) {
	var ack runcontext.DialectRetention
	proposal, err := readAssistantDialectFactoryFinal(raw)
	if err != nil {
		return ack, err
	}
	if proposal.Version != 2 {
		return ack, errAssistantRunScope
	}
	run, _, _, err := s.current(ctx)
	if err != nil {
		return ack, err
	}
	held := assistantDialectDraftOf(&run, s.host)
	if held == nil || held.Candidate.Receipt == nil || held.Checked.Receipt == nil {
		return ack, errAssistantRunDispatch
	}
	lineage := runcontext.DialectDraftLineage{AttemptID: held.AttemptID, Fingerprint: held.DraftFingerprint, CandidateReceipt: held.Candidate.Receipt.Receipt, CheckedReceipt: held.Checked.Receipt.Receipt, CheckedDigest: held.Checked.Receipt.PackageDigest}
	if proposal.Draft != lineage {
		return ack, errAssistantRunConflict
	}
	for _, stage := range []string{"candidate", "checked"} {
		slot := assistantDialectDraftSlotOf(held, stage)
		original, err := s.objects.Get(ctx, slot.Receipt.Reference)
		if err != nil {
			return ack, err
		}
		if verifyRetainedArtifact(slot.Receipt.Reference, original) != nil {
			return ack, errRetainedArtifactChanged
		}
		cp, initialFingerprint, err := readAssistantDialectFactoryDraft(original)
		if err != nil || cp.Version != proposal.Version || cp.Stage != stage || !reflect.DeepEqual(cp.PreviousReceipt, slot.Receipt.PreviousReceipt) || !assistantDialectFactoryDraftMatches(*held, cp, initialFingerprint) || cp.Digest != slot.Receipt.PackageDigest {
			return ack, errAssistantRunConflict
		}
		candidate := proposal.DialectCandidate
		candidate.DocumentJSON = cp.Candidate.DocumentJSON
		if !reflect.DeepEqual(candidate, cp.Candidate) {
			return ack, errAssistantRunConflict
		}
	}
	previous := held.Checked.Receipt.Receipt
	saved, err := s.retain(ctx, "final", raw, *held, proposal.Fingerprint, &previous)
	if err != nil {
		return ack, err
	}
	return runcontext.DialectRetention{Draft: lineage, Fingerprint: proposal.Fingerprint, Receipt: saved.Final.Receipt.Receipt, Status: "review-required"}, nil
}
