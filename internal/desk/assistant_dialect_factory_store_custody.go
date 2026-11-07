package desk

import (
	"reflect"
	"strings"
)

// The journal carries bounded references only. Checkpoints, proposals and reports
// remain exact raw objects in the existing project artifact repository.
type assistantDialectDraftIntent struct {
	Reference       retainedArtifactRef `json:"reference"`
	Receipt         string              `json:"receipt"`
	PackageDigest   string              `json:"packageDigest"`
	PreviousReceipt *string             `json:"previousReceipt"`
}
type assistantDialectDraftSlot struct {
	Intent  *assistantDialectDraftIntent `json:"intent,omitempty"`
	Receipt *assistantDialectDraftIntent `json:"receipt,omitempty"`
}
type assistantDialectDraftCustody struct {
	Version              int                       `json:"version"`
	HostOperationID      string                    `json:"hostOperationId"`
	ActionID             string                    `json:"actionId"`
	ReadOperationID      string                    `json:"readOperationId"`
	ResourceDigest       string                    `json:"resourceDigest"`
	AttemptID            string                    `json:"attemptId"`
	CandidateFingerprint string                    `json:"candidateFingerprint"`
	DraftFingerprint     string                    `json:"draftFingerprint"`
	Candidate            assistantDialectDraftSlot `json:"candidate"`
	Checked              assistantDialectDraftSlot `json:"checked"`
	Final                assistantDialectDraftSlot `json:"final"`
	Uncertainty          string                    `json:"uncertainty,omitempty"`
	Reconciled           bool                      `json:"reconciled,omitempty"`
}

func assistantDialectDraftSlotOf(held *assistantDialectDraftCustody, stage string) *assistantDialectDraftSlot {
	switch stage {
	case "candidate":
		return &held.Candidate
	case "checked":
		return &held.Checked
	case "final":
		return &held.Final
	}
	return nil
}
func assistantDialectDraftOf(run *assistantRunRecord, host string) *assistantDialectDraftCustody {
	for i := range run.DialectDrafts {
		if run.DialectDrafts[i].HostOperationID == host {
			return &run.DialectDrafts[i]
		}
	}
	return nil
}
func assistantDialectPackageDigest(v string) bool {
	return strings.HasPrefix(v, "sha256:") && assistantRunDigest(strings.TrimPrefix(v, "sha256:"))
}
func assistantDialectAttempt(v string) bool {
	return len(v) == 32 && assistantRunDigest(v+v)
}
func assistantDialectResource(id, pack string) string {
	return digestOf(assistantChatJSON(struct {
		ID      string `json:"id"`
		PackKey string `json:"packKey"`
	}{id, pack}))
}
func assistantDialectDraftReceipt(run assistantRunRecord, held assistantDialectDraftCustody, stage string, intent assistantDialectDraftIntent) string {
	prefix := "ddr-"
	if stage == "final" {
		prefix = "dpr-"
	}
	// No user-controlled delimiter concatenation: every identity is framed JSON.
	return prefix + digestOf(assistantChatJSON([]any{run.Binding.Project, run.ID, held.HostOperationID, held.ActionID, held.AttemptID, stage, intent.Reference, intent.PackageDigest, intent.PreviousReceipt}))
}

// Resolves only the saved admitted root and its exact earlier completed read.
// It is usable by original-byte validation, including historical imports; the
// live-only state checks belong to the effect path below.
func assistantDialectDraftRoot(run assistantRunRecord, host string) (assistantHostIntent, assistantDialectRead, error) {
	var in assistantHostIntent
	op := assistantDialectOperation(run, host)
	if op == nil || op.Kind != "host" || op.Name != "propose_dialect" || readAssistantHostReceipt(op.Intent, &in) != nil || !in.Root || in.SourceBinding != nil || in.ExternalScope != nil || in.DialectAction == nil || validateAssistantDialectIntent(run, *op, in) != nil || run.Binding.Owner.Kind != "chat" || run.Binding.Phase != "work" {
		return in, assistantDialectRead{}, errAssistantRunScope
	}
	a, err := readAssistantDialectArguments(op.Name, in.Arguments)
	if err != nil {
		return in, assistantDialectRead{}, err
	}
	before := assistantRunRecord{Binding: run.Binding}
	for _, earlier := range run.Operations {
		if earlier.ID == host {
			break
		}
		before.Operations = append(before.Operations, earlier)
	}
	readOp, read, err := assistantDialectReadOperation(before, op.Name, a)
	if err != nil || readOp.ID != in.DialectAction.ReadOperationID || !validAssistantDialectRead(read) || read.Recipe != nil || read.Variant != "" {
		return in, read, errAssistantRunScope
	}
	return in, read, nil
}

func validateAssistantDialectDraftCustody(run assistantRunRecord) error {
	if err := validateAssistantDialectArtifactImport(run); err != nil {
		return err
	}
	if err := validateAssistantDialectCompleteReadCustody(run); err != nil {
		return err
	}
	if run.DialectArtifactOwner != "" && (!assistantRunDigest(run.DialectArtifactOwner) || len(run.DialectDrafts) == 0 && len(run.DialectReads) == 0) {
		return errAssistantRunInvalid
	}
	if len(run.DialectDrafts) > 16 {
		return errAssistantRunInvalid
	}
	hosts, actions, attempts, receipts := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, held := range run.DialectDrafts {
		in, read, err := assistantDialectDraftRoot(run, held.HostOperationID)
		if err != nil || held.Version != 1 || !assistantRunText(held.HostOperationID, 128) || !assistantRunText(held.ActionID, 80) || !assistantRunText(held.ReadOperationID, 128) || held.ActionID != in.DialectAction.ID || held.ReadOperationID != in.DialectAction.ReadOperationID || held.ResourceDigest != assistantDialectResource(read.ID, read.PackKey) || !assistantDialectAttempt(held.AttemptID) || !assistantDialectPackageDigest(held.CandidateFingerprint) || !assistantDialectPackageDigest(held.DraftFingerprint) || hosts[held.HostOperationID] || actions[held.ActionID] || attempts[held.AttemptID] || held.Candidate.Intent == nil {
			return errAssistantRunInvalid
		}
		hosts[held.HostOperationID], actions[held.ActionID], attempts[held.AttemptID] = true, true, true
		if held.Uncertainty != "" && !contains([]string{"storage-unacknowledged", "ack-mismatch", "cancelled-after-intent"}, held.Uncertainty) || held.Reconciled && held.Uncertainty != "" {
			return errAssistantRunInvalid
		}
		for _, stage := range []string{"candidate", "checked", "final"} {
			slot := assistantDialectDraftSlotOf(&held, stage)
			if slot.Receipt != nil && (slot.Intent == nil || !reflect.DeepEqual(slot.Receipt, slot.Intent)) {
				return errAssistantRunInvalid
			}
			if slot.Intent == nil {
				continue
			}
			i := slot.Intent
			limit := 8 << 20
			var previous *string
			if stage != "candidate" {
				prior := held.Candidate.Receipt
				if stage == "final" {
					prior, limit = held.Checked.Receipt, 4<<20
				}
				if prior == nil {
					return errAssistantRunInvalid
				}
				previous = &prior.Receipt
			}
			if !i.Reference.valid() || i.Reference.Bytes < 1 || i.Reference.Bytes > limit || !assistantDialectPackageDigest(i.PackageDigest) || !reflect.DeepEqual(i.PreviousReceipt, previous) || i.Receipt != assistantDialectDraftReceipt(run, held, stage, *i) || receipts[i.Receipt] || held.Reconciled && slot.Receipt == nil {
				return errAssistantRunInvalid
			}
			receipts[i.Receipt] = true
		}
	}
	return nil
}

// Closed typed controls, explicit nullable lineage and no scalar coercions.
// This is also invoked on original journal bytes before independent decoding.
func readAssistantDialectDraftCustody(raw []byte) error {
	return readAssistantDialectFactoryShape(raw, reflect.TypeOf([]assistantDialectDraftCustody{}), false, 1)
}

// A plain saved scalar cannot recreate the original concrete reservation hold.
// This independent value is latched before a mutation callback is entered.
type assistantDialectArtifactOwnerAdmission struct {
	owner     string
	runID     string
	binding   assistantRunBinding
	host      string
	stage     string
	expected  assistantDialectDraftIntent
	read      *assistantDialectCompleteReadCustody
	readStage string
}

func assistantDialectDraftOwnerMatches(run assistantRunRecord, owner string) bool {
	return assistantRunDigest(owner) && (run.DialectArtifactOwner == owner || run.DialectArtifactOwner == "" && len(run.DialectDrafts) == 0 && len(run.DialectReads) == 0)
}

// Validate provenance changes at the one live persistence boundary. Parsing an
// owned or legacy ledger is separate and never calls this admission function.
func validateAssistantDialectArtifactOwnerTransition(before, after assistantRunLedger, admission *assistantDialectArtifactOwnerAdmission) error {
	old := make(map[string]assistantRunRecord, len(before.Runs))
	for _, run := range before.Runs {
		old[run.ID] = run
	}
	for _, run := range after.Runs {
		prior, exists := old[run.ID]
		delete(old, run.ID)
		if !exists {
			if err := validateAssistantDialectArtifactImportTransition(nil, run); err != nil {
				return err
			}
			if run.DialectArtifactOwner != "" || len(run.DialectDrafts) != 0 || len(run.DialectReads) != 0 {
				return errAssistantRunScope
			}
			continue
		}
		if err := validateAssistantDialectArtifactImportTransition(&prior, run); err != nil {
			return err
		}
		if prior.DialectArtifactOwner != "" && run.DialectArtifactOwner != prior.DialectArtifactOwner {
			return errAssistantRunScope
		}
		if prior.DialectArtifactOwner == "" && len(prior.DialectDrafts) != 0 {
			// Legacy data may retain uncertainty/lifecycle, but cannot acquire
			// fresh storage intents, acknowledgments, or a guessed owner.
			if run.DialectArtifactOwner != "" || len(prior.DialectDrafts) != len(run.DialectDrafts) || !reflect.DeepEqual(prior.DialectReads, run.DialectReads) {
				return errAssistantRunScope
			}
			for i, held := range prior.DialectDrafts {
				changed := run.DialectDrafts[i]
				changed.Uncertainty = held.Uncertainty
				if !reflect.DeepEqual(held, changed) {
					return errAssistantRunScope
				}
			}
			continue
		}
		assigned := prior.DialectArtifactOwner == "" && run.DialectArtifactOwner != ""
		admitted := admission != nil && admission.runID == run.ID && admission.owner == run.DialectArtifactOwner && assistantRunDigest(admission.owner) && reflect.DeepEqual(admission.binding, prior.Binding) && reflect.DeepEqual(admission.binding, run.Binding)
		readChange, err := validateAssistantDialectCompleteReadTransition(prior, run, admission)
		if err != nil {
			return err
		}
		if readChange && !admitted {
			return errAssistantRunScope
		}
		newIntent := false
		for _, priorHeld := range prior.DialectDrafts {
			held := assistantDialectDraftOf(&run, priorHeld.HostOperationID)
			if held == nil {
				return errAssistantRunScope
			}
			for _, stage := range []string{"candidate", "checked", "final"} {
				was, now := assistantDialectDraftSlotOf(&priorHeld, stage), assistantDialectDraftSlotOf(held, stage)
				if was.Intent != nil && !reflect.DeepEqual(was.Intent, now.Intent) {
					return errAssistantRunScope
				}
			}
		}
		for _, held := range run.DialectDrafts {
			priorHeld := assistantDialectDraftOf(&prior, held.HostOperationID)
			for _, stage := range []string{"candidate", "checked", "final"} {
				slot := assistantDialectDraftSlotOf(&held, stage)
				if slot.Intent == nil || priorHeld != nil && assistantDialectDraftSlotOf(priorHeld, stage).Intent != nil {
					continue
				}
				if !admitted || held.HostOperationID != admission.host || stage != admission.stage || !reflect.DeepEqual(slot.Intent, &admission.expected) || slot.Receipt != nil || held.Uncertainty != "" || held.Reconciled {
					return errAssistantRunScope
				}
				if priorHeld == nil && stage != "candidate" {
					return errAssistantRunScope
				}
				in, _, err := assistantDialectDraftRoot(run, held.HostOperationID)
				if err != nil || held.ActionID != in.DialectAction.ID || slot.Intent.Receipt != assistantDialectDraftReceipt(run, held, stage, *slot.Intent) {
					return errAssistantRunScope
				}
				newIntent = true
			}
		}
		if assigned && (!admitted || !(newIntent && admission.stage == "candidate" && len(prior.DialectDrafts) == 0 && len(prior.DialectReads) == 0 || readChange && admission.readStage == "intent" && len(prior.DialectDrafts) == 0 && len(prior.DialectReads) == 0)) {
			return errAssistantRunScope
		}
		if (newIntent || readChange) && run.DialectArtifactOwner == "" {
			return errAssistantRunScope
		}
	}
	for _, run := range old {
		if run.DialectArtifactOwner != "" || len(run.DialectDrafts) != 0 || len(run.DialectReads) != 0 {
			return errAssistantRunScope
		}
	}
	return nil
}
