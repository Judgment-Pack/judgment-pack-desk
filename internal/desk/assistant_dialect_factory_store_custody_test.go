package desk

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// A complete hand-authored saved root/read and manifest. No Runtime, model,
// provider, command discovery or live application fixture is required.
func assistantDialectFactoryFixtureRun(t *testing.T) (assistantRunRecord, assistantDialectRead, []byte) {
	t.Helper()
	candidate := assistantDialectFactoryFixtureCandidate()
	hash := strings.Repeat("b", 64)
	binding := assistantRunBinding{Principal: hash, Session: hash, Policy: hash, Project: hash, Owner: assistantRunOwner{Kind: "chat", ID: "chat", Revision: hash}, RequestID: "turn", RequestDigest: hash, SourceDigests: []string{}, ConnectionID: "legacy-api", ConnectionRevision: hash, ProfileRevision: hash, Model: "fixture", Phase: "work", Unit: "provider-managed", ExecutionKey: hash}
	run := assistantRunRecord{ID: "arun_" + strings.Repeat("c", 32), Revision: 1, Binding: binding, AdmissionID: "admission", BodyDigest: hash, State: "running", Operations: []assistantRunOperation{}, Events: []assistantRunEvent{}}
	facts, err := assistantDialectFacts([]byte(candidate.PackJSON))
	if err != nil {
		t.Fatal(err)
	}
	var pack map[string]json.RawMessage
	if err = json.Unmarshal([]byte(candidate.PackJSON), &pack); err != nil {
		t.Fatal(err)
	}
	read := assistantDialectRead{ID: candidate.ID, Name: "Example", Revision: candidate.Revision, RecipeVersion: 1, PackKey: candidate.PackKey, PackPath: "mapped.json", Declaration: hash, Pack: []byte(candidate.PackJSON)}
	read.View = assistantChatJSON(map[string]any{"id": read.ID, "name": read.Name, "revision": read.Revision, "recipeVersion": read.RecipeVersion, "guidance": "fixture", "packKey": read.PackKey, "packDigest": candidate.PackDigest, "pack": candidate.PackJSON, "facts": facts, "evidenceRequirements": pack["evidenceRequirements"], "recordCount": 1, "columns": []assistantDialectColumnView{{Name: "Country", Samples: []assistantDialectSample{{Value: "CA", Origin: "asserted"}}}}, "current": nil})
	readArgs := assistantChatJSON(map[string]any{"id": candidate.ID, "packKey": candidate.PackKey})
	readIn := assistantHostIntent{CallID: "read-call", Root: true, Arguments: readArgs}
	readTool, err := assistantHostJSON(json.RawMessage(read.View))
	if err != nil {
		t.Fatal(err)
	}
	readOp := assistantRunOperation{ID: assistantDialectOperationID(readIn), CallID: "read-host", Kind: "host", Name: "read_dialect", Intent: assistantChatJSON(readIn), State: "completed", Result: assistantChatJSON(assistantHostResult{Result: readTool.Value().JSON(), DialectRead: &read})}
	rootArgs := assistantChatJSON(map[string]any{"id": candidate.ID, "revision": candidate.Revision, "packKey": candidate.PackKey, "packDigest": candidate.PackDigest, "facts": candidate.Facts, "evidence": candidate.Evidence, "summary": candidate.Summary})
	rootIn := assistantHostIntent{CallID: "propose-call", Root: true, Arguments: rootArgs}
	rootIn.DialectAction = &assistantDialectActionMetadata{ID: assistantDialectActionID(binding, rootIn), CreatedAt: "2026-01-01T00:00:00Z", AfterTurnID: binding.RequestID, ReadOperationID: readOp.ID}
	root := assistantRunOperation{ID: assistantDialectOperationID(rootIn), CallID: "propose-host", Kind: "host", Name: "propose_dialect", Intent: assistantChatJSON(rootIn), State: "dispatched"}
	run.Operations = []assistantRunOperation{readOp, root}
	for i, row := range []struct {
		in   assistantHostIntent
		name string
	}{{readIn, "read_dialect"}, {rootIn, "propose_dialect"}} {
		call := row.in.CallID
		run.Events = append(run.Events, assistantRunEvent{ID: []string{"read-event", "propose-event"}[i], Event: contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: row.name, Args: row.in.Arguments}}})
	}
	cp, raw := assistantDialectFactoryFixtureDraft(t, "candidate", nil)
	held := assistantDialectDraftCustody{Version: 1, HostOperationID: root.ID, ActionID: rootIn.DialectAction.ID, ReadOperationID: readOp.ID, ResourceDigest: assistantDialectResource(read.ID, read.PackKey), AttemptID: cp.AttemptID, CandidateFingerprint: assistantDialectFactoryFixtureHash(t, cp.Candidate), DraftFingerprint: cp.Fingerprint}
	intent := assistantDialectDraftIntent{Reference: artifactReference(raw), PackageDigest: cp.Digest}
	intent.Receipt = "ddr-" + digestOf(assistantChatJSON([]any{binding.Project, run.ID, root.ID, held.ActionID, held.AttemptID, "candidate", intent.Reference, intent.PackageDigest, intent.PreviousReceipt}))
	held.Candidate.Intent = &intent
	run.DialectDrafts = []assistantDialectDraftCustody{held}
	return run, read, raw
}

func TestAssistantDialectFactoryCustodyOriginalBytesAndSemanticOwners(t *testing.T) {
	run, _, _ := assistantDialectFactoryFixtureRun(t)
	ledger := assistantRunLedger{Version: 1, Project: run.Binding.Project, Runs: []assistantRunRecord{run}}
	name := "assistant-runs-" + ledger.Project + ".json"
	raw, _, err := prepareAssistantRunStorage(name, ledger)
	if err != nil {
		t.Fatalf("hand-authored new record: %v", err)
	}
	decoded, err := decodeAssistantRunStorage(name, raw)
	if err != nil || !reflect.DeepEqual(decoded, ledger) {
		t.Fatalf("independent full decode: %v", err)
	}
	old := ledger
	old.Runs = append([]assistantRunRecord(nil), ledger.Runs...)
	old.Runs[0].DialectDrafts = nil
	oldRaw, _, err := prepareAssistantRunStorage(name, old)
	if err != nil || bytes.Contains(oldRaw, []byte(`"dialectDrafts"`)) {
		t.Fatalf("old record compatibility: %v", err)
	}
	for label, mutate := range map[string]func(*assistantRunRecord){
		"orphan":         func(r *assistantRunRecord) { r.DialectDrafts[0].HostOperationID = "absent" },
		"wrong read":     func(r *assistantRunRecord) { r.DialectDrafts[0].ReadOperationID = "absent" },
		"foreign action": func(r *assistantRunRecord) { r.DialectDrafts[0].ActionID = "foreign" },
		"wrong resource": func(r *assistantRunRecord) { r.DialectDrafts[0].ResourceDigest = strings.Repeat("e", 64) },
		"wrong phase":    func(r *assistantRunRecord) { r.Binding.Phase = "plan" },
		"child": func(r *assistantRunRecord) {
			var in assistantHostIntent
			_ = json.Unmarshal(r.Operations[1].Intent, &in)
			in.Root = false
			r.Operations[1].Intent = assistantChatJSON(in)
		},
		"receipt rebind": func(r *assistantRunRecord) {
			r.DialectDrafts[0].Candidate.Intent.Receipt = "ddr-" + strings.Repeat("0", 64)
		},
		"order": func(r *assistantRunRecord) { r.DialectDrafts[0].Checked = r.DialectDrafts[0].Candidate },
	} {
		t.Run(label, func(t *testing.T) {
			var changed assistantRunLedger
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			mutate(&changed.Runs[0])
			if validateAssistantDialectDraftCustody(changed.Runs[0]) == nil {
				t.Fatal("typed custody admitted malformed record")
			}
			if _, _, err := prepareAssistantRunStorage(name, changed); err == nil {
				t.Fatal("composed output proof suppressed semantic validation")
			}
			if _, err := decodeAssistantRunStorage(name, assistantChatJSON(changed)); err == nil {
				t.Fatal("independent full decode suppressed semantic validation")
			}
		})
	}
	// Original-byte missing/null/case controls must fail even when the Go zero
	// value would look valid, before the composed R/V association is considered.
	for label, bad := range map[string][]byte{
		"null manifest":    bytes.Replace(raw, []byte(`"dialectDrafts":[`), []byte(`"dialectDrafts":null,"unused":[`), 1),
		"null version":     bytes.Replace(raw, []byte(`"version":1,"hostOperationId"`), []byte(`"version":null,"hostOperationId"`), 1),
		"missing previous": bytes.Replace(raw, []byte(`,"previousReceipt":null`), nil, 1),
		"case member":      bytes.Replace(raw, []byte(`"resourceDigest":`), []byte(`"ResourceDigest":`), 1),
	} {
		t.Run(label, func(t *testing.T) {
			if bytes.Equal(bad, raw) {
				t.Fatal("mutation did not apply")
			}
			if _, err := decodeAssistantRunStorage(name, bad); err == nil {
				t.Fatal("malformed original controls accepted")
			}
		})
	}
}

func TestAssistantDialectFactoryCustodyBoundedManifest(t *testing.T) {
	run, _, raw := assistantDialectFactoryFixtureRun(t)
	held := run.DialectDrafts[0]
	for _, stage := range []string{"candidate", "checked", "final"} {
		slot := assistantDialectDraftSlotOf(&held, stage)
		intent := assistantDialectDraftIntent{Reference: retainedArtifactRef{SHA256: strings.Repeat("f", 64), Bytes: assistantDialectDraftByteLimit}, PackageDigest: "sha256:" + strings.Repeat("f", 64)}
		if stage != "candidate" {
			previous := "ddr-" + strings.Repeat("f", 64)
			intent.PreviousReceipt = &previous
		}
		intent.Receipt = assistantDialectDraftReceipt(run, held, stage, intent)
		slot.Intent, slot.Receipt = &intent, &intent
	}
	held.HostOperationID, held.ReadOperationID = strings.Repeat("h", 128), strings.Repeat("r", 128)
	held.ActionID = strings.Repeat("a", 80)
	held.Uncertainty = "cancelled-after-intent"
	manifest, err := json.Marshal(held)
	if err != nil || len(manifest) > 4096 {
		t.Fatalf("finite manifest shape %d: %v", len(manifest), err)
	}
	if bytes.Contains(manifest, raw) || bytes.Contains(manifest, []byte("documentJson")) || bytes.Contains(manifest, []byte("resultJson")) {
		t.Fatal("payload body entered ledger")
	}
	t.Logf("finite schema envelope manifest bytes=%d raw checkpoint bytes=%d sha256=%s; 16-action/128-run products are conservative counts, not reachable-domain proof", len(manifest), len(raw), digestOf(raw))
}
