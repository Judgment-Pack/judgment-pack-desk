package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const policyRecoveryJournalName = "policy-recovery-operations.json"
const policyRecoveryResultName = "recovery-result.json"

type policyRecoveryOperation struct {
	Request             policyRecoveryRequest `json:"request"`
	Result              policyRecoveryResult  `json:"result"`
	DestinationIdentity string                `json:"destinationIdentity"`
}
type policyRecoveryJournal struct {
	Version    int                                `json:"version"`
	Operations map[string]policyRecoveryOperation `json:"operations"`
}

func validPolicyRecoveryRequest(q policyRecoveryRequest) bool {
	return policyOperationIDForm.MatchString(q.OperationID) && retainedArtifactDigest.MatchString(q.ProjectID) && retainedArtifactDigest.MatchString(q.ArchiveDigest) && q.ArchiveBytes > 0 && q.ArchiveBytes <= policyArchiveLimit && retainedArtifactDigest.MatchString(q.LedgerDigest) && validProjectHistoryPath(q.Destination) && len(q.Destination) <= 2048 && retainedArtifactDigest.MatchString(q.DestinationRevision)
}
func policyRecoveryRequestDigest(q policyRecoveryRequest, actor policyActor) string {
	raw, _ := json.Marshal(struct {
		Request policyRecoveryRequest
		Actor   policyActor
	}{q, actor})
	return digestOf(raw)
}
func policyRecoveryOperationKey(project, operation string) string { return project + "." + operation }
func validPolicyRecoveryOperation(op policyRecoveryOperation) bool {
	q, r := op.Request, op.Result
	x := r.Receipt
	return validPolicyRecoveryRequest(q) && r.Delivery == "committed" && r.OutcomeKind == "policy-recovery" && r.ProjectID == q.ProjectID && r.RestartRequired && r.Authority == recoveryAuthority() && x.Kind == "policy-recovery-restore" && x.OperationID == q.OperationID && x.ProjectID == q.ProjectID && x.ArchiveDigest == q.ArchiveDigest && x.ArchiveBytes == q.ArchiveBytes && x.LedgerDigest == q.LedgerDigest && x.Destination == q.Destination && x.DestinationRevision == q.DestinationRevision && retainedArtifactDigest.MatchString(x.SnapshotID) && validPolicyTime(x.At) && validPolicyActor(x.Actor) && x.RequestDigest == policyRecoveryRequestDigest(q, x.Actor) && op.DestinationIdentity != ""
}
func (s *Server) loadPolicyRecoveryJournal() (policyRecoveryJournal, error) {
	j := policyRecoveryJournal{Version: 1, Operations: map[string]policyRecoveryOperation{}}
	raw, err := readPrivateData(s.assistant.root, policyRecoveryJournalName, policyLedgerLimit)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return j, err
	}
	if rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &j) != nil || j.Version != 1 || j.Operations == nil || len(j.Operations) > policyOperationLimit {
		return j, errors.New("recovery operation journal integrity failed")
	}
	for key, op := range j.Operations {
		if !validPolicyRecoveryOperation(op) || key != policyRecoveryOperationKey(op.Request.ProjectID, op.Request.OperationID) {
			return j, errors.New("recovery operation journal identity failed")
		}
	}
	return j, nil
}
func policyRecoveryUnresolved() error {
	f := policyRefusal(409, "recovery-unresolved", "The restore outcome is not confirmed. Keep its operation ID and use read-only recovery; do not repeat the restore.")
	f.Uncertain = true
	return f
}
func (s *Server) recoverPolicyDestination(op policyRecoveryOperation) (policyRecoveryResult, error) {
	if !validPolicyRecoveryOperation(op) {
		return policyRecoveryResult{}, policyRecoveryUnresolved()
	}
	// Only the source-owned journal supplies a path. Neither URL nor uploaded
	// archive can turn recovery into a general filesystem read capability.
	root, err := openPrivateDataRoot(op.Request.Destination, false)
	if err != nil {
		return policyRecoveryResult{}, policyRecoveryUnresolved()
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil || identityKey(info) != op.DestinationIdentity {
		return policyRecoveryResult{}, policyRecoveryUnresolved()
	}
	policies, _, err := openOwnFolder(root, strings.Split(policyDirectory, "/"), false, "policies")
	if err != nil {
		return policyRecoveryResult{}, policyRecoveryUnresolved()
	}
	defer policies.Close()
	raw, err := readPrivateData(policies, policyRecoveryResultName, 16<<10)
	want, _ := json.Marshal(op.Result)
	if err != nil || !bytes.Equal(raw, want) {
		return policyRecoveryResult{}, policyRecoveryUnresolved()
	}
	r := op.Result
	r.Delivery = "recovered"
	return r, nil
}
func (s *Server) readPolicyRecoveryOperation(ctx context.Context, projectID, id string, request *policyRecoveryOperation) (policyRecoveryResult, error) {
	lock, err := s.openPrivateDataFileLock(ctx, ".policy-recovery-operations.lock", true, true)
	if err != nil {
		return policyRecoveryResult{}, policyRecoveryUnresolved()
	}
	defer lock.Close()
	j, err := s.loadPolicyRecoveryJournal()
	if err != nil {
		return policyRecoveryResult{}, policyRecoveryUnresolved()
	}
	op, ok := j.Operations[policyRecoveryOperationKey(projectID, id)]
	if !ok {
		return policyRecoveryResult{}, policyRefusal(404, "not-found", "No completed Policy restore is available. The original operation may still be in progress.")
	}
	if request != nil && op.Result.Receipt.RequestDigest != request.Result.Receipt.RequestDigest {
		return policyRecoveryResult{}, policyRefusal(409, "idempotency-conflict", "This restore operation ID already belongs to a different reviewed request.")
	}
	return s.recoverPolicyDestination(op)
}

var testPolicyRecoveryBeforePublish func() error
var testPolicyRecoveryAfterPublish func() error

func (s *Server) restorePolicyArchive(ctx context.Context, projectID string, actor policyActor, q policyRecoveryRequest, file *os.File, authorize func(context.Context) error) (policyRecoveryResult, error) {
	var empty policyRecoveryResult
	if err := authorize(ctx); err != nil {
		return empty, err
	}
	requestDigest := policyRecoveryRequestDigest(q, actor)
	probe := policyRecoveryOperation{Result: policyRecoveryResult{Receipt: policyRecoveryReceipt{RequestDigest: requestDigest}}}
	if recovered, err := s.readPolicyRecoveryOperation(ctx, projectID, q.OperationID, &probe); err == nil {
		return recovered, nil
	} else {
		var f *policyFailure
		if !errors.As(err, &f) || f.Code != "not-found" {
			return empty, err
		}
	}
	cut, manifest, reader, err := readPolicyArchive(ctx, file)
	if err != nil {
		return empty, policyBeforeCommitFailure(err)
	}
	if manifest.Ledger.SHA256 != q.LedgerDigest {
		return empty, policyRefusal(400, "archive-integrity", "The archive ledger differs from the reviewed ledger.")
	}
	parent, revision, err := s.policyRecoveryDestination(q.Destination)
	if err != nil {
		return empty, policyBeforeCommitFailure(err)
	}
	defer parent.Close()
	if revision != q.DestinationRevision {
		return empty, policyRefusal(409, "destination-conflict", "The destination parent changed. Inspect the destination again.")
	}
	token, err := NewToken()
	if err != nil {
		return empty, policyBeforeCommitFailure(err)
	}
	stageName := ".policy-recovery-" + token
	if err = parent.own.root.Mkdir(stageName, custodyDirMode); err != nil {
		return empty, policyBeforeCommitFailure(err)
	}
	published := false
	defer func() {
		if !published {
			_ = parent.own.root.RemoveAll(stageName)
		}
	}()
	stage, err := parent.own.root.OpenRoot(stageName)
	if err != nil {
		return empty, policyBeforeCommitFailure(err)
	}
	defer stage.Close()
	stageInfo, err := stage.Stat(".")
	if err != nil {
		return empty, policyBeforeCommitFailure(err)
	}
	snapshot := policyRecoverySnapshot{SnapshotID: digestOf([]byte(token)), ArchiveDigest: q.ArchiveDigest, LedgerDigest: q.LedgerDigest, At: time.Now().UTC().Format("2006-01-02T15:04:05Z"), Actor: actor}
	if err = restorePolicyRecoveryBarrier(&cut.Ledger, snapshot); err != nil {
		return empty, policyBeforeCommitFailure(err)
	}
	result := policyRecoveryResult{Delivery: "committed", OutcomeKind: "policy-recovery", ProjectID: projectID, RestartRequired: true, Authority: recoveryAuthority(), Receipt: policyRecoveryReceipt{Kind: "policy-recovery-restore", OperationID: q.OperationID, RequestDigest: requestDigest, ProjectID: projectID, ArchiveDigest: q.ArchiveDigest, ArchiveBytes: q.ArchiveBytes, LedgerDigest: q.LedgerDigest, Destination: q.Destination, DestinationRevision: q.DestinationRevision, SnapshotID: snapshot.SnapshotID, At: snapshot.At, Actor: actor}}
	op := policyRecoveryOperation{Request: q, Result: result, DestinationIdentity: identityKey(stageInfo)}
	if !validPolicyRecoveryOperation(op) {
		return empty, policyBeforeCommitFailure(errors.New("invalid recovery outcome"))
	}
	if err = writePolicyRecoveryProject(ctx, stage, cut, reader, result); err != nil {
		return empty, policyBeforeCommitFailure(err)
	}
	if testPolicyRecoveryBeforePublish != nil {
		if err = testPolicyRecoveryBeforePublish(); err != nil {
			return empty, policyBeforeCommitFailure(err)
		}
	}
	if err = authorize(ctx); err != nil {
		return empty, err
	}
	if current, err := s.policyRecoveryProjectID(); err != nil || current != projectID {
		return empty, policyRefusal(409, "recovery-conflict", "The current project identity changed. Nothing was restored.")
	}
	// Serialize operation admission through publication, allowing a lost reply to
	// recover the exact marker rather than creating another destination.
	lock, err := s.openPrivateDataFileLock(ctx, ".policy-recovery-operations.lock", true, true)
	if err != nil {
		return empty, policyBeforeCommitFailure(err)
	}
	defer lock.Close()
	j, err := s.loadPolicyRecoveryJournal()
	if err != nil {
		return empty, policyRecoveryUnresolved()
	}
	key := policyRecoveryOperationKey(projectID, q.OperationID)
	if previous, ok := j.Operations[key]; ok {
		if previous.Result.Receipt.RequestDigest != requestDigest {
			return empty, policyRefusal(409, "idempotency-conflict", "This restore operation ID belongs to another request.")
		}
		return s.recoverPolicyDestination(previous)
	}
	if len(j.Operations) >= policyOperationLimit {
		return empty, policyRefusal(409, "storage-capacity", "Policy recovery operation history has reached its supported capacity.")
	}
	// Recheck the pinned parent/name and absence immediately before recording an
	// intent. Publication still uses no-replace against destination races.
	check, latest, err := s.policyRecoveryDestination(q.Destination)
	if err != nil {
		return empty, policyBeforeCommitFailure(err)
	}
	check.Close()
	if latest != q.DestinationRevision {
		return empty, policyRefusal(409, "destination-conflict", "The destination parent changed. Inspect it again.")
	}
	if err = authorize(ctx); err != nil {
		return empty, err
	}
	j.Operations[key] = op
	raw, _ := json.Marshal(j)
	if len(raw) > policyLedgerLimit {
		return empty, policyArchiveCapacity()
	}
	if err = writePrivateData(s.assistant.root, policyRecoveryJournalName, raw); err != nil {
		return empty, policyRecoveryUnresolved()
	}
	if err = publishPolicyRecoveryDirectory(parent, stageName, filepath.Base(q.Destination)); err != nil {
		return empty, policyRecoveryUnresolved()
	}
	published = true
	if testPolicyRecoveryAfterPublish != nil {
		if err = testPolicyRecoveryAfterPublish(); err != nil {
			return empty, policyRecoveryUnresolved()
		}
	}
	if err = parent.own.dirFile.Sync(); err != nil {
		return empty, policyRecoveryUnresolved()
	}
	if _, err = s.recoverPolicyDestination(op); err != nil {
		return empty, err
	}
	return result, nil
}
func writePolicyRecoveryProject(ctx context.Context, root *os.Root, cut policyArchiveCut, reader policyArchiveReader, result policyRecoveryResult) error {
	initial := &Server{root: root}
	artifacts := fileRetainedArtifactRepository{open: initial.openRetainedArtifacts}
	for _, ref := range cut.Refs {
		raw, err := reader.Get(ctx, ref)
		if err != nil {
			return err
		}
		got, err := artifacts.Put(ctx, raw)
		if err != nil || got != ref {
			if err != nil {
				return err
			}
			return policyArchiveInvalid()
		}
	}
	policies, err := initial.openPolicies(true)
	if err != nil {
		return err
	}
	defer policies.Close()
	raw, _ := json.Marshal(cut.Ledger)
	if len(raw) > policyLedgerLimit {
		return policyArchiveCapacity()
	}
	if err = writePrivateData(policies, "ledger.json", raw); err != nil {
		return err
	}
	raw, _ = json.Marshal(result)
	if err = writePrivateData(policies, policyRecoveryResultName, raw); err != nil {
		return err
	}
	// A host-authored empty standalone project. No Runtime/provider check, source
	// configuration, identity, signing key, startup selection or chat pointer.
	for name, body := range map[string]string{runtimeConfigName: gatedDeskConfig, projectConfigName: "{\"deskConfigVersion\":1}\n", ".gitignore": ".desk-private/\n"} {
		if err = writePrivateData(root, name, []byte(body)); err != nil {
			return err
		}
	}
	private, err := root.Open(".desk-private")
	if err != nil {
		return err
	}
	err = private.Sync()
	private.Close()
	if err != nil {
		return err
	}
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
