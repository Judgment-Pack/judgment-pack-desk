package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
)

var errAssistantDialectReadRecoveryBudget = errors.New("Retained read observation is waiting for original storage admission.")

// Implemented only by the original B2 writer adapter, never by JSON or a
// repository/provider callback. The adapter must reserve the exact stage/target,
// replacement peak and settlement/control capacity BEFORE invoking this method.
// It retains the existing coordinator token and uncertain-write journal. Domain
// CAS/checks execute outside Q. See the staged quota join contract; no production
// default observer or path/JSON bypass is installed.
type assistantDialectReadRecoveryWriter interface {
	prepareOriginalDialectReadRecovery(context.Context) (*assistantDialectReadRecoveryStorageOperation, error)
	recognizeOriginalDialectReadRecovery(context.Context, *assistantDialectReadRecoveryGuard, assistantDialectReadRecovery, []byte, assistantDialectReadRecoveryRecord) error
	reserveOriginalDialectReadRecovery(context.Context, *assistantDialectReadRecoveryPin, assistantDialectReadRecoveryWriteIntent) (assistantDialectReadRecoveryWriteAdmission, error)
}

type assistantDialectReadRecoveryWriteAdmission interface {
	writeOriginalDialectReadRecovery(context.Context, *assistantDialectReadRecoveryGuard, assistantDialectReadRecoveryWriteIntent) error
	verifyOriginalDialectReadRecovery(context.Context, *assistantDialectReadRecoveryGuard) error
	completeOriginalDialectReadRecovery(context.Context, *assistantDialectReadRecoveryGuard) error
	Close() error
}

type assistantDialectReadRecoveryWriteIntent struct {
	Target, Stage                     string
	ExpectedAbsent                    bool
	ExpectedSHA256, OriginalRunSHA256 string
	Bytes                             []byte
}

func readAssistantDialectReadRecoveryLocked(ctx context.Context, root *os.Root, run assistantRunRecord) (assistantDialectReadRecovery, []byte, error) {
	name := assistantDialectReadRecoveryName(run.Binding.Project, run.ID)
	raw, err := readInjectedArtifactExactFile(ctx, root, name, assistantDialectReadRecoveryFileLimit, "read-recovery-record")
	if injectedArtifactOnlyNotExist(err) {
		return assistantDialectReadRecovery{Version: 1, Project: run.Binding.Project, RunID: run.ID, RunBindingSHA256: digestOf(assistantChatJSON(run.Binding)), Records: []assistantDialectReadRecoveryRecord{}}, nil, nil
	}
	if err != nil {
		return assistantDialectReadRecovery{}, nil, err
	}
	value, err := decodeAssistantDialectReadRecovery(name, raw)
	if err == nil {
		err = validateAssistantDialectReadRecoveryBound(value, run)
	}
	return value, raw, err
}

func assistantDialectReadRecoveryOf(value assistantDialectReadRecovery, operation string) *assistantDialectReadRecoveryRecord {
	for i := range value.Records {
		if value.Records[i].OperationID == operation {
			return &value.Records[i]
		}
	}
	return nil
}

func newAssistantDialectReadRecoveryRecord(run assistantRunRecord, operation, request string) (assistantDialectReadRecoveryRecord, error) {
	op, _, err := assistantDialectCompleteReadRoot(run, operation)
	if err != nil {
		return assistantDialectReadRecoveryRecord{}, err
	}
	row := assistantDialectReadRecoveryRecord{OperationID: operation, OriginalIntentSHA256: digestOf(op.Intent), BindingSHA256: digestOf(assistantChatJSON(run.Binding)), Owner: run.DialectArtifactOwner, FirstClassification: "not-reserved", Settlement: assistantDialectReadRecoverySettlement{State: "unresolved"}, LatestObservation: assistantDialectReadRecoveryObservation{Ordinal: 1, BaselineRunRevision: run.Revision, BaselineRunSHA256: digestOf(assistantChatJSON(run)), State: "unavailable", Reason: "owner-unavailable", RequestID: request}}
	if held := assistantDialectCompleteReadOf(&run, operation); held != nil {
		ref := held.Intent.Reference
		row.Reference, row.CustodyIntentSHA256, row.OriginalUncertainty = &ref, digestOf(assistantChatJSON(held.Intent)), held.Uncertainty
		row.FirstClassification = "reserved-unacknowledged"
		if held.Receipt != nil {
			row.FirstClassification = "acknowledged-result-unavailable"
		}
		if op.State == "completed" {
			row.FirstClassification = "completed-backing-unavailable"
		}
	}
	return row, nil
}

// This pure function validates only the separate observation transition. It can
// neither persist the sidecar nor confer the original read's admission.
func prepareAssistantDialectReadRecovery(prior assistantDialectReadRecovery, run assistantRunRecord, row assistantDialectReadRecoveryRecord) (assistantDialectReadRecovery, []byte, error) {
	if prior.Revision == math.MaxUint64 || validateAssistantDialectReadRecoveryBound(prior, run) != nil || validateAssistantDialectReadRecoveryRecord(row) != nil {
		return assistantDialectReadRecovery{}, nil, errAssistantRunScope
	}
	var next assistantDialectReadRecovery
	if json.Unmarshal(assistantChatJSON(prior), &next) != nil {
		return next, nil, errAssistantRunInvalid
	}
	was := assistantDialectReadRecoveryOf(prior, row.OperationID)
	if was != nil {
		if !sameAssistantDialectReadRecoveryOriginal(*was, row) || was.LatestObservation.Ordinal == math.MaxUint64 || row.LatestObservation.Ordinal != was.LatestObservation.Ordinal+1 || !reflect.DeepEqual(was.Settlement, row.Settlement) {
			return next, nil, errAssistantRunScope
		}
		*assistantDialectReadRecoveryOf(next, row.OperationID) = row
	} else {
		if row.LatestObservation.Ordinal != 1 || row.Settlement.State != "unresolved" {
			return next, nil, errAssistantRunScope
		}
		next.Records = append(next.Records, row)
	}
	next.Revision++
	name := assistantDialectReadRecoveryName(run.Binding.Project, run.ID)
	raw, err := json.Marshal(next)
	if err != nil {
		return next, nil, err
	}
	decoded, err := decodeAssistantDialectReadRecovery(name, raw)
	if err != nil || validateAssistantDialectReadRecoveryBound(decoded, run) != nil {
		return next, nil, errors.Join(errAssistantRunScope, err)
	}
	return decoded, raw, nil
}

func prepareAssistantDialectReadRecoveryWrite(prior assistantDialectReadRecovery, priorRaw []byte, run assistantRunRecord, row assistantDialectReadRecoveryRecord) (assistantDialectReadRecoveryWriteIntent, error) {
	_, raw, err := prepareAssistantDialectReadRecovery(prior, run, row)
	if err != nil {
		return assistantDialectReadRecoveryWriteIntent{}, err
	}
	intent := assistantDialectReadRecoveryWriteIntent{Target: assistantDialectReadRecoveryName(run.Binding.Project, run.ID), ExpectedAbsent: priorRaw == nil, ExpectedSHA256: digestOf(priorRaw), OriginalRunSHA256: digestOf(assistantChatJSON(run)), Bytes: bytes.Clone(raw)}
	intent.Stage = ".data-read-recovery-" + digestOf(assistantChatJSON([]any{intent.Target, prior.Revision, row.LatestObservation.RequestID, digestOf(raw)}))
	return intent, nil
}

func (g *assistantDialectReadRecoveryGuard) commit(ctx context.Context, prior assistantDialectReadRecovery, priorRaw []byte, row assistantDialectReadRecoveryRecord, intent assistantDialectReadRecoveryWriteIntent, admission assistantDialectReadRecoveryWriteAdmission) (assistantDialectReadRecovery, error) {
	if err := g.Check(ctx); err != nil {
		return assistantDialectReadRecovery{}, err
	}
	if admission == nil {
		return assistantDialectReadRecovery{}, errAssistantDialectReadRecoveryBudget
	}
	current, currentRaw, err := readAssistantDialectReadRecoveryLocked(ctx, g.data.root, g.run)
	if err != nil || !bytes.Equal(priorRaw, currentRaw) || !reflect.DeepEqual(current, prior) {
		return assistantDialectReadRecovery{}, errors.Join(errAssistantRunConflict, err)
	}
	ledger, err := g.pin.lease.store.readLocked(ctx, g.data)
	if err != nil {
		return assistantDialectReadRecovery{}, err
	}
	run, err := assistantRunBound(&ledger, g.run.Binding, g.run.ID)
	if err != nil || !reflect.DeepEqual(*run, g.run) {
		return assistantDialectReadRecovery{}, errors.Join(errAssistantRunConflict, err)
	}
	next, raw, err := prepareAssistantDialectReadRecovery(prior, g.run, row)
	if err != nil {
		return assistantDialectReadRecovery{}, err
	}
	expected, err := prepareAssistantDialectReadRecoveryWrite(prior, priorRaw, g.run, row)
	if err != nil || !reflect.DeepEqual(intent, expected) {
		return assistantDialectReadRecovery{}, errors.Join(errAssistantRunScope, err)
	}
	if err = g.Check(ctx); err != nil {
		return assistantDialectReadRecovery{}, err
	}
	sealedRun := bytes.Clone(assistantChatJSON(g.run))
	if err = admission.writeOriginalDialectReadRecovery(ctx, g, intent); err != nil {
		return assistantDialectReadRecovery{}, err
	}
	currentLedger, err := g.pin.lease.store.readLocked(ctx, g.data)
	if err != nil {
		return assistantDialectReadRecovery{}, err
	}
	original, err := assistantRunBound(&currentLedger, g.pin.run.Binding, g.pin.run.ID)
	if err != nil || !bytes.Equal(assistantChatJSON(*original), sealedRun) || digestOf(sealedRun) != intent.OriginalRunSHA256 {
		return assistantDialectReadRecovery{}, errors.Join(errAssistantRunScope, err)
	}
	// Writer success is insufficient without this same owner's exact readback.
	saved, savedRaw, err := readAssistantDialectReadRecoveryLocked(ctx, g.data.root, g.run)
	if err != nil || !bytes.Equal(savedRaw, raw) || !reflect.DeepEqual(saved, next) {
		return assistantDialectReadRecovery{}, errors.Join(errAssistantRunConflict, err)
	}
	if err = g.Check(ctx); err != nil {
		return assistantDialectReadRecovery{}, err
	}
	if err = admission.verifyOriginalDialectReadRecovery(ctx, g); err != nil {
		return assistantDialectReadRecovery{}, err
	}
	return saved, nil
}
