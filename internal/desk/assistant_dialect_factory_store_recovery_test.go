package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

type assistantDialectFactoryObservationObjects struct {
	raw         []byte
	byReference map[retainedArtifactRef][]byte
	gets, puts  int
	missing     bool
}

func (o *assistantDialectFactoryObservationObjects) Get(_ context.Context, ref retainedArtifactRef) ([]byte, error) {
	o.gets++
	if o.byReference != nil {
		raw, ok := o.byReference[ref]
		if !ok {
			return nil, errors.New("missing exact fixture reference")
		}
		return append([]byte(nil), raw...), nil
	}
	if o.missing {
		return nil, errors.New("missing fixture")
	}
	return append([]byte(nil), o.raw...), nil
}
func (o *assistantDialectFactoryObservationObjects) Put(context.Context, []byte) (retainedArtifactRef, error) {
	o.puts++
	return retainedArtifactRef{}, errors.New("observation must never Put")
}

// Test-only trusted owner for a hand-authored complete read. No legacy view is
// interpreted as complete evidence and no production enrollment is implied.
type assistantDialectFactoryFixtureEvidence struct {
	run      string
	binding  assistantRunBinding
	readID   string
	snapshot runcontext.DialectSnapshot
	legacy   []runcontext.DialectResearchRevision
}

func (e assistantDialectFactoryFixtureEvidence) completeDialectSnapshot(_ context.Context, run assistantRunRecord, op assistantRunOperation, read assistantDialectRead) (runcontext.DialectSnapshot, error) {
	if run.ID != e.run || !reflect.DeepEqual(run.Binding, e.binding) || op.ID != e.readID || op.State != "completed" || read.ID != e.snapshot.ID || read.Revision != e.snapshot.Revision || read.PackKey != e.snapshot.PackKey || string(read.Pack) != e.snapshot.PackJSON {
		return runcontext.DialectSnapshot{}, errAssistantRunScope
	}
	return e.snapshot, nil
}
func (e assistantDialectFactoryFixtureEvidence) completeDialectLegacyDeclarations(ctx context.Context, run assistantRunRecord, op assistantRunOperation, read assistantDialectRead) ([]runcontext.DialectResearchRevision, error) {
	if _, err := e.completeDialectSnapshot(ctx, run, op, read); err != nil {
		return nil, err
	}
	return e.legacy, nil
}
func assistantDialectFactoryEvidenceFixture(run assistantRunRecord) assistantDialectFactoryFixtureEvidence {
	c := assistantDialectFactoryFixtureCandidate()
	return assistantDialectFactoryFixtureEvidence{run: run.ID, binding: run.Binding, readID: run.DialectDrafts[0].ReadOperationID, snapshot: runcontext.DialectSnapshot{ID: c.ID, Revision: c.Revision, PackKey: c.PackKey, PackJSON: c.PackJSON, Columns: c.Columns, ResearchIndex: c.ResearchIndex, RetainedRead: c.RetainedRead, Conversions: map[string]runcontext.DialectConversion{}, FactTargets: []string{"/company/country"}, EvidenceRequirements: []string{"license"}}}
}

func TestAssistantDialectFactoryRecoveryObservationOnly(t *testing.T) {
	run, _, raw := assistantDialectFactoryFixtureRun(t)
	run.State = "interrupted"
	run.Operations[1].State = "uncertain"
	run.DialectDrafts[0].Uncertainty = "storage-unacknowledged"
	original := run.DialectDrafts[0]
	objects := &assistantDialectFactoryObservationObjects{raw: raw}
	held, err := observeAssistantDialectDraft(t.Context(), objects, assistantDialectFactoryEvidenceFixture(run), run, original)
	if err != nil || held.Candidate.Receipt == nil || !reflect.DeepEqual(held.Candidate.Receipt, held.Candidate.Intent) || held.Uncertainty != "" || !held.Reconciled {
		t.Fatalf("exact observation: %v", err)
	}
	if objects.puts != 0 || objects.gets != 1 || run.Operations[1].State != "uncertain" || run.DialectDrafts[0].Candidate.Receipt != nil {
		t.Fatal("inspection changed root custody or wrote raw object")
	}
	for label, owner := range map[string]*assistantDialectFactoryObservationObjects{"missing": {missing: true}, "mismatch": {raw: []byte(`{}`)}} {
		t.Run(label, func(t *testing.T) {
			if _, err := observeAssistantDialectDraft(t.Context(), owner, assistantDialectFactoryEvidenceFixture(run), run, original); err == nil {
				t.Fatal("unavailable object resolved")
			}
			if owner.puts != 0 || original.Candidate.Receipt != nil {
				t.Fatal("unknown object retried or uncertainty erased")
			}
		})
	}
	if _, err := observeAssistantDialectDraft(t.Context(), objects, nil, run, original); err == nil {
		t.Fatal("legacy read substituted for complete snapshot")
	}
	evidence := assistantDialectFactoryEvidenceFixture(run)
	evidence.snapshot.ResearchIndex = nil
	if _, err := observeAssistantDialectDraft(t.Context(), objects, evidence, run, original); err == nil {
		t.Fatal("original unavailable/empty inventory distinction lost")
	}
}

func TestAssistantDialectFactoryRecoveryFenceSurvivesSessionAndNonce(t *testing.T) {
	old, _, _ := assistantDialectFactoryFixtureRun(t)
	current, _, _ := assistantDialectFactoryFixtureRun(t)
	current.ID = "arun_" + strings.Repeat("d", 32)
	current.Binding.Session = strings.Repeat("e", 64)
	current.DialectDrafts = nil
	host := current.Operations[1].ID
	ledger := assistantRunLedger{Version: 1, Project: old.Binding.Project, Runs: []assistantRunRecord{old, current}}
	if assistantDialectDraftFence(ledger, current, host) == nil {
		t.Fatal("new run/session bypassed pending intent")
	}
	// Even a storage reconciliation cannot erase the original root's unknown
	// operation or manufacture permission for another attempt.
	intent := *ledger.Runs[0].DialectDrafts[0].Candidate.Intent
	ledger.Runs[0].DialectDrafts[0].Candidate.Receipt = &intent
	ledger.Runs[0].DialectDrafts[0].Reconciled = true
	ledger.Runs[0].Operations[1].State = "uncertain"
	if assistantDialectDraftFence(ledger, current, host) == nil {
		t.Fatal("reconciled object erased root uncertainty")
	}
	// Crash-before-intent remains fenced by the admitted dispatched root.
	ledger.Runs[0].DialectDrafts = nil
	if assistantDialectDraftFence(ledger, current, host) == nil {
		t.Fatal("absent manifest converted uncertain dispatch to fresh permission")
	}
	// A separately observed known refusal closes the root. It is not produced
	// by this reconciler and may permit an explicit new root/nonce later.
	ledger.Runs[0].Operations[1].State = "refused"
	if err := assistantDialectDraftFence(ledger, current, host); err != nil {
		t.Fatalf("known closed root remained unknown: %v", err)
	}
}

func TestAssistantDialectFactoryRecoveryRequiredPutCapability(t *testing.T) {
	store := &assistantDialectFactoryStore{objects: &assistantDialectFactoryObservationObjects{}}
	if _, _, _, err := store.current(t.Context()); err == nil {
		t.Fatal("inactive store accepted missing full effect authority")
	}
}

// Hand-authored three-stage custody. Every mutation gets its own exact raw ref
// and deterministic intent, so refusal must come from the final-object proof,
// rather than an unrelated raw hash or lineage mismatch.
func assistantDialectFactoryFinalObservationFixture(t *testing.T, mutate func(*runcontext.DialectReviewProposal)) (assistantRunRecord, *assistantDialectFactoryObservationObjects, []byte) {
	t.Helper()
	run, _, candidateRaw := assistantDialectFactoryFixtureRun(t)
	run.State, run.Operations[1].State = "interrupted", "uncertain"
	held := &run.DialectDrafts[0]
	held.Uncertainty = "storage-unacknowledged"
	candidateIntent := *held.Candidate.Intent
	held.Candidate.Receipt = &candidateIntent
	previous := candidateIntent.Receipt
	checked, checkedRaw := assistantDialectFactoryFixtureDraft(t, "checked", &previous)
	checkedIntent := assistantDialectDraftIntent{Reference: artifactReference(checkedRaw), PackageDigest: checked.Digest, PreviousReceipt: &previous}
	checkedIntent.Receipt = "ddr-" + digestOf(assistantChatJSON([]any{run.Binding.Project, run.ID, held.HostOperationID, held.ActionID, held.AttemptID, "checked", checkedIntent.Reference, checkedIntent.PackageDigest, checkedIntent.PreviousReceipt}))
	held.Checked.Intent, held.Checked.Receipt = &checkedIntent, &checkedIntent
	candidate := assistantDialectFactoryFixtureCandidate()
	fingerprint := assistantDialectFactoryFixtureHash(t, candidate)
	proposal := runcontext.DialectReviewProposal{
		Draft:            runcontext.DialectDraftLineage{AttemptID: held.AttemptID, Fingerprint: held.DraftFingerprint, CandidateReceipt: candidateIntent.Receipt, CheckedReceipt: checkedIntent.Receipt, CheckedDigest: checkedIntent.PackageDigest},
		DialectCandidate: candidate, Version: 2, Fingerprint: fingerprint,
		MappingDigest: "sha256:" + digestOf([]byte(candidate.DocumentJSON)), Status: "review-required",
		Validation: map[string]any{}, Coverage: map[string]any{},
		Preview: runcontext.DialectPreview{Fingerprint: fingerprint, RecordCount: 1, Issues: []runcontext.DialectPreviewIssue{}},
	}
	if mutate != nil {
		mutate(&proposal)
	}
	finalRaw, err := json.Marshal(proposal)
	assistantRunMust(t, err)
	checkedReceipt := checkedIntent.Receipt
	finalIntent := assistantDialectDraftIntent{Reference: artifactReference(finalRaw), PackageDigest: fingerprint, PreviousReceipt: &checkedReceipt}
	finalIntent.Receipt = "dpr-" + digestOf(assistantChatJSON([]any{run.Binding.Project, run.ID, held.HostOperationID, held.ActionID, held.AttemptID, "final", finalIntent.Reference, finalIntent.PackageDigest, finalIntent.PreviousReceipt}))
	held.Final.Intent = &finalIntent
	objects := &assistantDialectFactoryObservationObjects{byReference: map[retainedArtifactRef][]byte{candidateIntent.Reference: candidateRaw, checkedIntent.Reference: checkedRaw, finalIntent.Reference: finalRaw}}
	return run, objects, finalRaw
}

func TestAssistantDialectFactoryRecoveryFinalProposalProof(t *testing.T) {
	for _, mode := range []string{"valid", "mapping-empty", "mapping-malformed", "preview-fingerprint"} {
		t.Run(mode, func(t *testing.T) {
			run, objects, raw := assistantDialectFactoryFinalObservationFixture(t, func(proposal *runcontext.DialectReviewProposal) {
				switch mode {
				case "mapping-empty":
					proposal.MappingDigest = ""
				case "mapping-malformed":
					proposal.MappingDigest = "sha256:malformed"
				case "preview-fingerprint":
					proposal.Preview.Fingerprint = "sha256:" + strings.Repeat("0", 64)
				}
			})
			ledger := assistantRunLedger{Version: 1, Project: run.Binding.Project, Runs: []assistantRunRecord{run}}
			name := "assistant-runs-" + ledger.Project + ".json"
			// Persisted custody alone cannot establish final proposal validity.
			encoded, _, err := prepareAssistantRunStorage(name, ledger)
			assistantRunMust(t, err)
			_, err = decodeAssistantRunStorage(name, encoded)
			assistantRunMust(t, err)
			original := run.DialectDrafts[0]
			observed, err := observeAssistantDialectDraft(t.Context(), objects, assistantDialectFactoryEvidenceFixture(run), run, original)
			if mode == "valid" {
				assistantRunMust(t, err)
				if !observed.Reconciled || observed.Uncertainty != "" || observed.Final.Receipt == nil || !reflect.DeepEqual(observed.Final.Intent, observed.Final.Receipt) {
					t.Fatal("valid three-stage evidence was not reconciled")
				}
				ledger.Runs[0].DialectDrafts = []assistantDialectDraftCustody{observed}
				encoded, _, err = prepareAssistantRunStorage(name, ledger)
				assistantRunMust(t, err)
				_, err = decodeAssistantRunStorage(name, encoded)
				assistantRunMust(t, err)
			} else {
				if err == nil || !reflect.DeepEqual(observed, original) {
					t.Fatal("malformed final evidence was certified", err)
				}
				// Both live entry points must reject the same exact raw object
				// before they need an execution, current source or effect owner.
				if _, err = (&assistantDialectFactoryStore{}).retainFinal(t.Context(), raw); err == nil {
					t.Fatal("live final retention accepted malformed proposal")
				}
				if _, err = readAssistantDialectDraftPublication(raw); err == nil {
					t.Fatal("live effect reader accepted malformed proposal")
				}
			}
			if objects.puts != 0 || objects.gets != 3 || !bytes.Equal(objects.byReference[original.Final.Intent.Reference], raw) || run.Operations[1].State != "uncertain" || run.DialectDrafts[0].Final.Receipt != nil || run.DialectDrafts[0].Uncertainty != "storage-unacknowledged" {
				t.Fatal("observation wrote, replayed or altered original custody")
			}
		})
	}
}

// A snapshot-only proof cannot stand in for the removed complete legacy bytes.
type assistantDialectFactorySnapshotOnlyEvidence struct {
	assistantDialectFactoryReadEvidence
}

func TestAssistantDialectFactoryLegacyOriginalInventoryProof(t *testing.T) {
	for _, declaration := range []string{"null", "[]"} {
		t.Run(declaration, func(t *testing.T) {
			run, _, _ := assistantDialectFactoryFixtureRun(t)
			cp, raw, _ := assistantDialectFactoryLegacyFixture(t, declaration)
			held := &run.DialectDrafts[0]
			held.CandidateFingerprint = assistantDialectFactoryFixtureHash(t, json.RawMessage(mustAssistantDialectCandidateRaw(t, raw)))
			held.DraftFingerprint = cp.Fingerprint
			held.Candidate.Intent.Reference = artifactReference(raw)
			held.Candidate.Intent.PackageDigest = cp.Digest
			// The immutable intent receipt binds the exact original raw object.
			held.Candidate.Intent.Receipt = "ddr-" + digestOf(assistantChatJSON([]any{run.Binding.Project, run.ID, held.HostOperationID, held.ActionID, held.AttemptID, "candidate", held.Candidate.Intent.Reference, cp.Digest, nil}))
			evidence := assistantDialectFactoryEvidenceFixture(run)
			evidence.snapshot.Columns = cp.Candidate.Columns
			evidence.legacy = cp.Candidate.ResearchDeclarations
			objects := &assistantDialectFactoryObservationObjects{raw: raw}
			if _, err := observeAssistantDialectDraft(t.Context(), objects, evidence, run, *held); err != nil {
				t.Fatal("original legacy read refused", err)
			}
			if _, err := observeAssistantDialectDraft(t.Context(), objects, assistantDialectFactorySnapshotOnlyEvidence{evidence}, run, *held); err == nil {
				t.Fatal("current index substituted for legacy original bytes")
			}
			if declaration == "null" {
				evidence.legacy = []runcontext.DialectResearchRevision{}
			} else {
				evidence.legacy = nil
			}
			if _, err := observeAssistantDialectDraft(t.Context(), objects, evidence, run, *held); err == nil {
				t.Fatal("legacy original null/empty evidence collapsed")
			}
			if objects.puts != 0 || held.Candidate.Receipt != nil {
				t.Fatal("legacy inspection wrote or rewrote custody")
			}
		})
	}
}
func mustAssistantDialectCandidateRaw(t *testing.T, raw []byte) []byte {
	t.Helper()
	candidate, err := assistantDialectFactoryRawCandidate(raw, false)
	assistantRunMust(t, err)
	return candidate
}
