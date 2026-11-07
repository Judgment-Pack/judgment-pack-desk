package desk

import (
	"context"
	"net/http"
	"reflect"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Deliberately scans every retained run in the same project/chat/resource. A
// session or nonce filter here would turn recreation into retry permission.
func assistantDialectDraftFence(ledger assistantRunLedger, current assistantRunRecord, host string) error {
	in, read, err := assistantDialectDraftRoot(current, host)
	if err != nil || in.DialectAction == nil {
		return errAssistantRunScope
	}
	resource := assistantDialectResource(read.ID, read.PackKey)
	for _, run := range ledger.Runs {
		if run.Binding.Project != current.Binding.Project || run.Binding.Owner.Kind != current.Binding.Owner.Kind || run.Binding.Owner.ID != current.Binding.Owner.ID {
			continue
		}
		// Even a crash before the first storage intent must not disappear merely
		// because it left no manifest. The admitted dispatched root remains unknown.
		for _, op := range run.Operations {
			if run.ID == current.ID && op.ID == host || op.Kind != "host" || op.Name != "propose_dialect" || op.State != "dispatched" && op.State != "uncertain" {
				continue
			}
			_, earlierRead, err := assistantDialectDraftRoot(run, op.ID)
			if err != nil {
				return err
			}
			if assistantDialectResource(earlierRead.ID, earlierRead.PackKey) == resource {
				return errAssistantRunDispatch
			}
		}
		for _, held := range run.DialectDrafts {
			if run.ID == current.ID && held.HostOperationID == host || held.ResourceDigest != resource {
				continue
			}
			op := assistantDialectOperation(run, held.HostOperationID)
			if op == nil || op.State == "dispatched" || op.State == "uncertain" || held.Uncertainty != "" {
				return errAssistantRunDispatch
			}
			for _, stage := range []string{"candidate", "checked", "final"} {
				slot := assistantDialectDraftSlotOf(&held, stage)
				if slot.Intent != nil && slot.Receipt == nil {
					return errAssistantRunDispatch
				}
			}
		}
	}
	return nil
}

// Called by the eventual adapter when an acknowledgment is missing or fails the
// package echo checks. It accepts a finite disposition, never caught error text.
func (s *assistantDialectFactoryStore) markUncertain(ctx context.Context, reason string) error {
	if !contains([]string{"storage-unacknowledged", "ack-mismatch", "cancelled-after-intent"}, reason) {
		return errAssistantRunInvalid
	}
	observed, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.execution.writer.change(observed, true, func(run *assistantRunRecord) (bool, error) {
		held := assistantDialectDraftOf(run, s.host)
		if held == nil || held.Reconciled {
			return false, errAssistantRunInvalid
		}
		if held.Uncertainty != "" {
			return false, nil
		}
		held.Uncertainty = reason
		return true, nil
	})
}

// This capability is supplied by the trusted completed-read owner only after it
// has durably bound the complete package snapshot to this exact actual read.
// The legacy DialectRead.View is insufficient. There is intentionally no
// legacy View fallback. assistantDialectCompletedReadEvidence supplies the actual
// original raw producer; canonical factory port enrollment remains separately required.
type assistantDialectFactoryReadEvidence interface {
	completeDialectSnapshot(context.Context, assistantRunRecord, assistantRunOperation, assistantDialectRead) (runcontext.DialectSnapshot, error)
}

// The live effect capability authenticates the same durable original read AND
// returns the complete current collection/history and rooted pack/dialect
// inputs through this already-owned view for an explicit whole-snapshot equality
// check by the guard. Implementations must not reacquire
// preparation/private owners or call Authorize. A snapshot from legacy View,
// or an MVCC view allowing replacement, does not satisfy this contract.
// The production implementation independently rebuilds through the actual view;
// supported injected source custody remains an explicit enrollment dependency.
type assistantDialectFactoryOwnedReadEvidence interface {
	assistantDialectFactoryReadEvidence
	completeDialectSnapshotUsingView(context.Context, assistantRunRecord, assistantRunOperation, assistantDialectRead, inputPreparationReader) (original runcontext.DialectSnapshot, current runcontext.DialectSnapshot, err error)
}

func assistantDialectFactorySnapshotBound(snapshot runcontext.DialectSnapshot, read assistantDialectRead) bool {
	return snapshot.ID == read.ID && snapshot.Revision == read.Revision && snapshot.PackKey == read.PackKey && (read.Complete == nil && snapshot.PackJSON == string(read.Pack) || read.Complete != nil && "sha256:"+digestOf([]byte(snapshot.PackJSON)) == read.Complete.PackDigest) && snapshot.Columns != nil && snapshot.Conversions != nil && snapshot.FactTargets != nil && snapshot.EvidenceRequirements != nil
}

func assistantDialectFactoryBoundSnapshot(ctx context.Context, evidence assistantDialectFactoryReadEvidence, run assistantRunRecord, host string) (runcontext.DialectSnapshot, error) {
	var snapshot runcontext.DialectSnapshot
	if evidence == nil {
		return snapshot, errAssistantRunScope
	}
	in, read, err := assistantDialectDraftRoot(run, host)
	if err != nil {
		return snapshot, err
	}
	op := assistantDialectOperation(run, in.DialectAction.ReadOperationID)
	if op == nil || op.State != "completed" {
		return snapshot, errAssistantRunScope
	}
	snapshot, err = evidence.completeDialectSnapshot(ctx, run, *op, read)
	if err != nil {
		return snapshot, err
	}
	if !assistantDialectFactorySnapshotBound(snapshot, read) {
		return snapshot, errAssistantRunScope
	}
	return snapshot, nil
}

func assistantDialectFactorySnapshotIdentityMatches(snapshot runcontext.DialectSnapshot, c runcontext.DialectCandidate) bool {
	var dialectDigest *string
	if snapshot.DialectJSON != nil {
		digest := "sha256:" + digestOf([]byte(*snapshot.DialectJSON))
		dialectDigest = &digest
	}
	return snapshot.ID == c.ID && snapshot.Revision == c.Revision && snapshot.PackKey == c.PackKey && snapshot.PackJSON == c.PackJSON && c.PackDigest == "sha256:"+digestOf([]byte(snapshot.PackJSON)) && reflect.DeepEqual(dialectDigest, c.DialectDigest) && reflect.DeepEqual(snapshot.Columns, c.Columns) && reflect.DeepEqual(snapshot.RetainedRead, c.RetainedRead)
}

func assistantDialectFactorySnapshotMatches(snapshot runcontext.DialectSnapshot, c runcontext.DialectCandidate) bool {
	return assistantDialectFactorySnapshotIdentityMatches(snapshot, c) && c.ResearchDeclarations == nil && reflect.DeepEqual(snapshot.ResearchIndex, c.ResearchIndex)
}

// Legacy declarations must come from the same completed original read. The
// new top index cannot reconstruct their exact bytes or nil/empty distinction.
// This compatibility capability is observation-only; new effects require v2.
type assistantDialectFactoryLegacyReadEvidence interface {
	completeDialectLegacyDeclarations(context.Context, assistantRunRecord, assistantRunOperation, assistantDialectRead) ([]runcontext.DialectResearchRevision, error)
}

func assistantDialectFactoryHistoricalSnapshotMatches(ctx context.Context, evidence assistantDialectFactoryReadEvidence, run assistantRunRecord, host string, snapshot runcontext.DialectSnapshot, candidate runcontext.DialectCandidate, version int) bool {
	if version == 2 {
		return assistantDialectFactorySnapshotMatches(snapshot, candidate)
	}
	legacy, ok := evidence.(assistantDialectFactoryLegacyReadEvidence)
	if version != 1 || !ok || candidate.ResearchIndex != nil {
		return false
	}
	in, read, err := assistantDialectDraftRoot(run, host)
	if err != nil {
		return false
	}
	op := assistantDialectOperation(run, in.DialectAction.ReadOperationID)
	if op == nil || op.State != "completed" {
		return false
	}
	declarations, err := legacy.completeDialectLegacyDeclarations(ctx, run, *op, read)
	if err != nil || !reflect.DeepEqual(declarations, candidate.ResearchDeclarations) {
		return false
	}
	// Independently compare the original source identity. A current index does
	// not participate in the legacy declaration proof.
	return assistantDialectFactorySnapshotIdentityMatches(snapshot, candidate)
}

// Verify the original reserved objects only. Missing or corrupt objects return
// an error and preserve the fence; neither receipt nor emptiness is invented.
func observeAssistantDialectDraft(ctx context.Context, objects retainedArtifactRepository, evidence assistantDialectFactoryReadEvidence, run assistantRunRecord, held assistantDialectDraftCustody) (assistantDialectDraftCustody, error) {
	snapshot, err := assistantDialectFactoryBoundSnapshot(ctx, evidence, run, held.HostOperationID)
	if err != nil {
		return held, err
	}
	var initial *runcontext.DialectCandidate
	initialVersion := 0
	for _, stage := range []string{"candidate", "checked", "final"} {
		slot := assistantDialectDraftSlotOf(&held, stage)
		if slot.Intent == nil {
			continue
		}
		raw, err := objects.Get(ctx, slot.Intent.Reference)
		if err != nil {
			return held, err
		}
		if verifyRetainedArtifact(slot.Intent.Reference, raw) != nil {
			return held, errRetainedArtifactChanged
		}
		if stage == "final" {
			proposal, err := readAssistantDialectFactoryFinal(raw)
			if err != nil || proposal.Fingerprint != slot.Intent.PackageDigest || !assistantDialectFactoryHistoricalSnapshotMatches(ctx, evidence, run, held.HostOperationID, snapshot, proposal.DialectCandidate, proposal.Version) {
				return held, errAssistantRunInvalid
			}
			if held.Candidate.Receipt == nil || held.Checked.Receipt == nil || proposal.Draft != (runcontext.DialectDraftLineage{AttemptID: held.AttemptID, Fingerprint: held.DraftFingerprint, CandidateReceipt: held.Candidate.Receipt.Receipt, CheckedReceipt: held.Checked.Receipt.Receipt, CheckedDigest: held.Checked.Receipt.PackageDigest}) {
				return held, errAssistantRunInvalid
			}
			candidate := proposal.DialectCandidate
			if initial == nil || initialVersion != proposal.Version {
				return held, errAssistantRunInvalid
			}
			candidate.DocumentJSON = initial.DocumentJSON
			if !reflect.DeepEqual(candidate, *initial) {
				return held, errAssistantRunInvalid
			}
		} else {
			cp, fingerprint, err := readAssistantDialectFactoryDraft(raw)
			if err != nil || cp.Stage != stage || cp.Digest != slot.Intent.PackageDigest || !reflect.DeepEqual(cp.PreviousReceipt, slot.Intent.PreviousReceipt) || !assistantDialectFactoryDraftMatches(held, cp, fingerprint) || !assistantDialectFactoryHistoricalSnapshotMatches(ctx, evidence, run, held.HostOperationID, snapshot, cp.Candidate, cp.Version) {
				return held, errAssistantRunInvalid
			}
			if initial == nil {
				candidate := cp.Candidate
				initial = &candidate
				initialVersion = cp.Version
			} else if initialVersion != cp.Version || !reflect.DeepEqual(*initial, cp.Candidate) {
				return held, errAssistantRunInvalid
			}
		}
		intent := *slot.Intent
		slot.Receipt = &intent
	}
	held.Uncertainty, held.Reconciled = "", true
	return held, nil
}

// Private entry for the existing authorized retained-result inspection seam.
// It is not routed here. No active writer is advanced; no Put, Runtime, provider,
// save, grant, parent-result rewrite or operation replay occurs in this path.
func (s *Server) reconcileAssistantDialectDrafts(r *http.Request, id string, evidence assistantDialectFactoryReadEvidence) error {
	host := s.assistantRunService()
	host.mu.Lock()
	active := host.active[id] != nil
	host.mu.Unlock()
	if active {
		return errAssistantRunConflict
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		return err
	}
	store := s.assistantRuns()
	run, err := store.lookup(r.Context(), identity, id, "", false)
	if err != nil {
		return err
	}
	if !assistantRunTerminal(run.State) {
		return errAssistantRunConflict
	}
	// A saved owner is provenance, not a current repository selection. Slice6
	// supplies the source-bound resolver; neither owned nor legacy D5 may fall
	// back to this Server's current provider or synthesize an acknowledgment.
	if len(run.DialectDrafts) != 0 {
		return errRetainedArtifactAuthorityUnavailable
	}
	objects := s.artifactRepositoryWithAuthority(func(ctx context.Context) error { return s.authorizeAssistantRun(r.WithContext(ctx), run) })
	observed := make([]assistantDialectDraftCustody, len(run.DialectDrafts))
	for i, held := range run.DialectDrafts {
		observed[i], err = observeAssistantDialectDraft(r.Context(), objects, evidence, run, held)
		if err != nil {
			return err
		}
	}
	_, err = store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		current, err := assistantRunBound(ledger, run.Binding, id)
		if err != nil {
			return false, err
		}
		if current.Revision != run.Revision || !assistantRunTerminal(current.State) || !reflect.DeepEqual(current.DialectDrafts, run.DialectDrafts) {
			return false, errAssistantRunConflict
		}
		if err := s.authorizeAssistantRun(r, *current); err != nil {
			return false, err
		}
		if reflect.DeepEqual(current.DialectDrafts, observed) {
			return false, nil
		}
		current.DialectDrafts = observed
		current.Revision++
		return true, nil
	})
	return err
}
