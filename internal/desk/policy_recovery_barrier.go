package desk

import "errors"

// Recovery extends the closed private ledger. Older readers refuse its unknown
// fields rather than silently turning archived grants into live authority.
type policyRecoverySnapshot struct {
	SnapshotID    string      `json:"snapshotId"`
	ArchiveDigest string      `json:"archiveDigest"`
	LedgerDigest  string      `json:"ledgerDigest"`
	At            string      `json:"at"`
	Actor         policyActor `json:"actor"`
}
type policyRecoveryBoundary struct {
	SnapshotID            string `json:"snapshotId"`
	RestoredAt            string `json:"restoredAt"`
	PolicySequence        int    `json:"policySequence"`
	SourceConsentRequired bool   `json:"sourceConsentRequired"`
}
type policyRecoverySource struct {
	SnapshotID         string `json:"snapshotId"`
	ConsentOperationID string `json:"consentOperationId"`
}
type policyRecoveryView struct {
	SnapshotID            string `json:"snapshotId"`
	RestoredAt            string `json:"restoredAt"`
	CurrentnessUnknown    bool   `json:"currentnessUnknown"`
	FreshReviewRequired   bool   `json:"freshReviewRequired"`
	SourceConsentRequired bool   `json:"sourceConsentRequired"`
}

func policyRecoveryProjection(record policyStoredRecord, stream *policySourceAuthorityStream) *policyRecoveryView {
	if record.Recovery == nil {
		return nil
	}
	return &policyRecoveryView{SnapshotID: record.Recovery.SnapshotID, RestoredAt: record.Recovery.RestoredAt, CurrentnessUnknown: true, FreshReviewRequired: policyAuthorityCurrentReview(record, stream) == nil, SourceConsentRequired: record.Recovery.SourceConsentRequired}
}
func policyRecoveryReviewBoundary(record policyStoredRecord, boundary int) int {
	if record.Recovery != nil && record.Recovery.PolicySequence > boundary {
		return record.Recovery.PolicySequence
	}
	return boundary
}
func policyRecoverySourceHeld(source *policyConnectedSource) bool {
	return source != nil && source.Recovery != nil && source.Recovery.ConsentOperationID == ""
}
func validatePolicyRecovery(ledger policyLedger) error {
	bad := errors.New("Policy recovery identity is inconsistent")
	if len(ledger.Recoveries) > 64 {
		return bad
	}
	known := map[string]policyRecoverySnapshot{}
	for _, r := range ledger.Recoveries {
		if !retainedArtifactDigest.MatchString(r.SnapshotID) || known[r.SnapshotID].SnapshotID != "" || !retainedArtifactDigest.MatchString(r.ArchiveDigest) || !retainedArtifactDigest.MatchString(r.LedgerDigest) || !validPolicyTime(r.At) || !validPolicyActor(r.Actor) {
			return bad
		}
		known[r.SnapshotID] = r
	}
	for id, record := range ledger.Policies {
		if r := record.Recovery; r != nil {
			snapshot, ok := known[r.SnapshotID]
			if !ok || r.RestoredAt != snapshot.At || r.PolicySequence < 1 || r.PolicySequence > policyEventCount(record) {
				return bad
			}
			source := ledger.Sources[id]
			if r.SourceConsentRequired && (source == nil || source.Recovery == nil || source.Recovery.SnapshotID != r.SnapshotID || source.Recovery.ConsentOperationID != "") {
				return bad
			}
		}
	}
	for id, source := range ledger.Sources {
		if source.Recovery != nil {
			r := source.Recovery
			record := ledger.Policies[id]
			if record.Recovery == nil || r.SnapshotID != record.Recovery.SnapshotID || record.Recovery.SourceConsentRequired != (r.ConsentOperationID == "") || r.ConsentOperationID != "" && r.ConsentOperationID != source.Grant.OperationID {
				return bad
			}
		}
	}
	return nil
}
func restorePolicyRecoveryBarrier(ledger *policyLedger, snapshot policyRecoverySnapshot) error {
	if len(ledger.Recoveries) >= 64 {
		return policyArchiveCapacity()
	}
	ledger.Recoveries = append(ledger.Recoveries, snapshot)
	for id, record := range ledger.Adoptions {
		record.Recovery = &policyCoverageRecovery{SnapshotID: snapshot.SnapshotID, RestoredAt: snapshot.At, Sequence: len(record.Reviews)}
		ledger.Adoptions[id] = record
	}
	for id, record := range ledger.Coverages {
		record.Recovery = &policyCoverageRecovery{SnapshotID: snapshot.SnapshotID, RestoredAt: snapshot.At, Sequence: len(record.Candidates) + len(record.Reviews)}
		ledger.Coverages[id] = record
	}
	for id, record := range ledger.Policies {
		record.Recovery = &policyRecoveryBoundary{SnapshotID: snapshot.SnapshotID, RestoredAt: snapshot.At, PolicySequence: policyEventCount(record), SourceConsentRequired: ledger.Sources[id] != nil}
		ledger.Policies[id] = record
		if source := ledger.Sources[id]; source != nil {
			source.Recovery = &policyRecoverySource{SnapshotID: snapshot.SnapshotID}
		}
	}
	return validatePolicyLedger(*ledger)
}
