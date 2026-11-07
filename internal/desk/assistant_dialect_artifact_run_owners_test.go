//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"strings"
	"testing"
)

func TestDialectArtifactRunOwnerSchema(t *testing.T) {
	run, _, _ := assistantDialectFactoryFixtureRun(t)
	run.DialectArtifactOwner = strings.Repeat("a", 64)
	ledger := assistantRunLedger{Version: 1, Project: run.Binding.Project, Runs: []assistantRunRecord{run}}
	name := "assistant-runs-" + ledger.Project + ".json"
	raw, _, err := prepareAssistantRunStorage(name, ledger)
	assistantRunMust(t, err)
	decoded, err := decodeAssistantRunStorage(name, raw)
	assistantRunMust(t, err)
	if decoded.Runs[0].DialectArtifactOwner != run.DialectArtifactOwner {
		t.Fatal("owned scalar lost")
	}
	member := []byte(`"dialectArtifactOwner":"` + run.DialectArtifactOwner + `"`)
	for label, value := range map[string]string{
		"null": `null`, "empty": `""`, "number": `1`, "object": `{}`, "array": `[]`,
		"short": `"` + strings.Repeat("a", 63) + `"`, "long": `"` + strings.Repeat("a", 65) + `"`,
		"uppercase": `"` + strings.Repeat("A", 64) + `"`, "nonhex": `"` + strings.Repeat("g", 64) + `"`,
		"escaped": `"\u0061` + strings.Repeat("a", 63) + `"`,
	} {
		t.Run(label, func(t *testing.T) {
			changed := bytes.Replace(raw, member, []byte(`"dialectArtifactOwner":`+value), 1)
			if bytes.Equal(raw, changed) {
				t.Fatal("mutation missed member")
			}
			if _, err := decodeAssistantRunStorage(name, changed); err == nil {
				t.Fatal("noncanonical owner accepted")
			}
		})
	}
	for label, replacement := range map[string]string{
		"duplicate": string(member) + "," + string(member),
		"case":      `"DialectArtifactOwner":"` + run.DialectArtifactOwner + `"`,
		"unknown":   string(member) + `,"dialectArtifactOwnerExtra":true`,
	} {
		t.Run(label, func(t *testing.T) {
			if _, err := decodeAssistantRunStorage(name, bytes.Replace(raw, member, []byte(replacement), 1)); err == nil {
				t.Fatal("member control accepted")
			}
		})
	}
	// Removing optional provenance preserves legacy decoding but cannot make
	// that run eligible for live retention against the original owner.
	legacy := bytes.Replace(raw, append(member, ','), nil, 1)
	old, err := decodeAssistantRunStorage(name, legacy)
	assistantRunMust(t, err)
	if old.Runs[0].DialectArtifactOwner != "" || assistantDialectDraftOwnerMatches(old.Runs[0], run.DialectArtifactOwner) {
		t.Fatal("legacy became executable")
	}
	if len(raw)-len(legacy) != 90 {
		t.Fatal("owner increment is not exactly 90 bytes")
	}
	ledger.Runs[0].DialectDrafts = nil
	if _, _, err := prepareAssistantRunStorage(name, ledger); err == nil {
		t.Fatal("owner-only record accepted")
	}
	ledger.Runs[0].DialectArtifactOwner = ""
	inline, _, err := prepareAssistantRunStorage(name, ledger)
	assistantRunMust(t, err)
	if bytes.Contains(inline, []byte(`"dialectArtifactOwner"`)) {
		t.Fatal("omission was not retained")
	}
	// Provenance never participates in receipt framing or binding bytes.
	i := *run.DialectDrafts[0].Candidate.Intent
	before, _ := json.Marshal(run.Binding)
	if assistantDialectDraftReceipt(run, run.DialectDrafts[0], "candidate", i) != i.Receipt {
		t.Fatal("receipt preimage changed")
	}
	run.DialectArtifactOwner = ""
	after, _ := json.Marshal(run.Binding)
	if !bytes.Equal(before, after) || assistantDialectDraftReceipt(run, run.DialectDrafts[0], "candidate", i) != i.Receipt {
		t.Fatal("provenance changed receipt or Binding")
	}
}

func TestDialectArtifactRunOwnerActualCapture(t *testing.T) {
	f := assistantDialectAuthoritySetup(t)
	if !assistantRunDigest(f.owner.enrollment.Owner) || f.owner.enrollment.Namespace != f.execution.record.Binding.Project {
		t.Fatal("missing actual enrollment")
	}
	project, err := f.server.root.Stat(".")
	assistantRunMust(t, err)
	workRoot, err := f.server.assistant.root.Stat(".")
	assistantRunMust(t, err)
	work, err := f.owner.lease.work.Stat()
	assistantRunMust(t, err)
	if f.owner.enrollment.ProjectIdentity != identityKey(project) || f.owner.enrollment.WorkIdentity != identityKey(workRoot) || f.owner.enrollment.WorkDomain != dialectArtifactWorkDomainID(identityKey(workRoot), identityKey(work)) {
		t.Fatal("enrollment was not joined to actual captured roots/FD")
	}
	before := assistantExecutionRecord(t, f.execution)
	if before.DialectArtifactOwner != "" || len(before.DialectDrafts) != 0 {
		t.Fatal("constructor persisted pre-enrollment owner")
	}
	f.reserve(t, false)
	saved := assistantExecutionRecord(t, f.execution)
	if saved.DialectArtifactOwner != f.owner.enrollment.Owner || saved.Revision != before.Revision+1 || saved.DialectDrafts[0].Candidate.Intent == nil {
		t.Fatal("owner and first intent were not atomic")
	}
	assistantDialectAssertReleased(t, f)
}

func TestDialectArtifactRunOwnerEnrollmentCaptureReplacement(t *testing.T) {
	f := assistantDialectAuthoritySetup(t)
	before := assistantRunStorageValidationRead(t, f.owner.lease.store)
	original := f.owner.lease.work
	path := filepath.Join(f.server.configDir, ".data-work.lock")
	savedPath := path + ".fixture-original"
	var replacement *os.File
	reached := false
	testHookDialectArtifactOwnerStep = func(step string) error {
		if step != "factory-after-owner-enrollment" || reached {
			return nil
		}
		reached = true
		if err := os.Rename(path, savedPath); err != nil {
			return err
		}
		var err error
		replacement, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		if err = lockPrivateData(replacement, false); err != nil {
			return err
		}
		f.owner.lease.mu.Lock()
		f.owner.lease.work = replacement
		f.owner.lease.mu.Unlock()
		return nil
	}
	defer func() {
		testHookDialectArtifactOwnerStep = nil
		f.owner.lease.mu.Lock()
		f.owner.lease.work = original
		f.owner.lease.mu.Unlock()
		if reached {
			assistantRunMust(t, os.Rename(savedPath, path))
		}
		if replacement != nil {
			assistantRunMust(t, replacement.Close())
		}
	}()
	factory, err := newAssistantDialectFactoryStore(f.ctx, f.execution, f.owner.host, f.evidence)
	testHookDialectArtifactOwnerStep = nil
	if !reached || err == nil || factory != nil {
		t.Fatal("replacement after acknowledged enrollment became a new original", err, reached)
	}
	if !bytes.Equal(before, assistantRunStorageValidationRead(t, f.owner.lease.store)) {
		t.Fatal("capture replacement persisted an owner or intent")
	}
}

func TestDialectArtifactRunOwnerStages(t *testing.T) {
	for _, injected := range []bool{false, true} {
		t.Run(map[bool]string{false: "native", true: "injected"}[injected], func(t *testing.T) {
			var f *assistantDialectAuthorityFixture
			if injected {
				p, registry, _, _ := injectedArtifactProviderFixture(t)
				f = assistantDialectAuthoritySetupWithProvider(t, p, registry)
			} else {
				f = assistantDialectAuthoritySetup(t)
			}
			var candidate runcontext.DialectDraftCheckpoint
			assistantRunMust(t, json.Unmarshal(f.raw, &candidate))
			first, err := f.store.retainDraft(f.ctx, f.raw)
			assistantRunMust(t, err)
			previous := first.Receipt
			fields := map[string]any{"version": 2, "attemptId": candidate.AttemptID, "candidate": candidate.Candidate, "stage": "checked", "previousReceipt": &previous, "fingerprint": candidate.Fingerprint, "check": runcontext.DialectDraftCheck{Results: []runcontext.DialectDraftResult{{Operation: "dialect_validate", ResultJSON: `{"valid":true}`}}}}
			fields["digest"] = assistantDialectFactoryFixtureHash(t, fields)
			checkedRaw := assistantChatJSON(fields)
			checked, err := f.store.retainDraft(f.ctx, checkedRaw)
			assistantRunMust(t, err)
			fingerprint := assistantDialectFactoryFixtureHash(t, candidate.Candidate)
			proposal := runcontext.DialectReviewProposal{Draft: runcontext.DialectDraftLineage{AttemptID: candidate.AttemptID, Fingerprint: candidate.Fingerprint, CandidateReceipt: first.Receipt, CheckedReceipt: checked.Receipt, CheckedDigest: checked.Digest}, DialectCandidate: candidate.Candidate, Version: 2, Fingerprint: fingerprint, MappingDigest: "sha256:" + digestOf([]byte(candidate.Candidate.DocumentJSON)), Status: "review-required", Validation: map[string]any{}, Coverage: map[string]any{}, Preview: runcontext.DialectPreview{Fingerprint: fingerprint, RecordCount: 1, Issues: []runcontext.DialectPreviewIssue{}}}
			finalRaw := assistantChatJSON(proposal)
			final, err := f.store.retainFinal(f.ctx, finalRaw)
			assistantRunMust(t, err)
			if final.Receipt == "" {
				t.Fatal("final receipt missing")
			}
			run := assistantExecutionRecord(t, f.execution)
			if run.DialectArtifactOwner != f.owner.enrollment.Owner {
				t.Fatal("stages replaced provenance")
			}
			held := run.DialectDrafts[0]
			for i, stage := range []string{"candidate", "checked", "final"} {
				slot := assistantDialectDraftSlotOf(&held, stage)
				if slot.Receipt == nil || !reflect.DeepEqual(slot.Intent, slot.Receipt) {
					t.Fatal("stage receipt differs", stage)
				}
				got, err := f.owner.inspection.Get(f.ctx, slot.Receipt.Reference)
				assistantRunMust(t, err)
				if !bytes.Equal(got, [][]byte{f.raw, checkedRaw, finalRaw}[i]) {
					t.Fatal("stage raw spelling changed", stage)
				}
			}
			before := assistantRunStorageValidationRead(t, f.owner.lease.store)
			publications := 0
			testHookDialectArtifactOwnerStep = func(step string) error {
				if step == "injected-object.publish" {
					publications++
				}
				return nil
			}
			defer func() { testHookDialectArtifactOwnerStep = nil }()
			duplicate, err := f.store.retainFinal(f.ctx, finalRaw)
			testHookDialectArtifactOwnerStep = nil
			assistantRunMust(t, err)
			if !reflect.DeepEqual(duplicate, final) || publications != 0 || !bytes.Equal(before, assistantRunStorageValidationRead(t, f.owner.lease.store)) {
				t.Fatal("completed duplicate republished or revised custody")
			}
			assistantDialectAssertReleased(t, f)
		})
	}
}
