package desk

import (
	"bytes"
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func assistantRunStorageValidationRead(t *testing.T, store assistantRunStore) []byte {
	t.Helper()
	data, err := store.server.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	raw, err := readPrivateData(data.root, store.name, assistantRunRecordLimit)
	assistantRunMust(t, err)
	return raw
}

func assistantRunStorageValidationCounts(t *testing.T) (*int, *[]bool) {
	t.Helper()
	full, output := new(int), new([]bool)
	testHookAssistantRunStorageValidation = func() { (*full)++ }
	testHookAssistantRunOutputValidation = func(associated bool) { *output = append(*output, associated) }
	t.Cleanup(func() {
		testHookAssistantRunStorageValidation = nil
		testHookAssistantRunOutputValidation = nil
	})
	return full, output
}

func TestAssistantRunStorageValidationSharedSuccessors(t *testing.T) {
	s, _, store, r, identity, start := assistantAuthoringValidationFixture(t)
	full, output := assistantRunStorageValidationCounts(t)
	_, err := s.readAssistantRunSource(r, start, identity, store.project)
	assistantRunMust(t, err)
	if *full != 1 {
		t.Fatal("cold source did not run the authoritative decoder", *full)
	}
	baseline := s.assistantRunValidation
	callbacks := 0
	_, err = store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
		callbacks++
		l.AuthoringCaptures[0].Cancelled = true
		return true, nil
	})
	assistantRunMust(t, err)
	if callbacks != 1 || *full != 1 || len(*output) != 1 || s.assistantRunValidation == baseline {
		t.Fatal("successor did not use/publish the composed proof", callbacks, *full, *output)
	}
	_, err = store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
		callbacks++
		if !l.AuthoringCaptures[0].Cancelled {
			t.Fatal("warm transaction saw the previous capture")
		}
		return false, nil
	})
	assistantRunMust(t, err)
	if _, err = s.readAssistantRunSource(r, start, identity, store.project); err == nil {
		t.Fatal("warm successor fact bypassed current cancellation")
	}
	if callbacks != 2 || *full != 1 || cap(s.assistantRunValidation.raw) != len(s.assistantRunValidation.raw) || len(s.assistantRunValidation.raw) > assistantRunRecordLimit {
		t.Fatal("repeated full validation or unbounded retained allocation", callbacks, *full)
	}
	if !bytes.Equal(s.assistantRunValidation.raw, assistantRunStorageValidationRead(t, store)) {
		t.Fatal("fact does not own exact current bytes")
	}
}

func TestAssistantRunStorageValidationSnapshots(t *testing.T) {
	for _, fixture := range []struct {
		name          string
		changed, warm bool
	}{
		{"baseline-cold", false, false},
		{"baseline-warm", false, true},
		{"changed-cold", true, false},
		{"changed-warm", true, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			changed := fixture.changed
			store, _, _, _, _, _ := assistantRunAdmitted(t)
			_, err := store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
				l.Runs[0].Checkpoint.OutputGuard = map[string]int64{"proposal": 1}
				op := assistantRunOp("retained")
				op.State = "not-started"
				l.Runs[0].Operations = []assistantRunOperation{op}
				return true, nil
			})
			assistantRunMust(t, err)
			if !fixture.warm {
				store.server.assistantRunValidation = nil
			}
			full, _ := assistantRunStorageValidationCounts(t)
			wantFull := 0
			if !fixture.warm {
				wantFull = 1
			}
			before := assistantRunStorageValidationRead(t, store)
			var retained *assistantRunLedger
			callbacks := 0
			got, err := store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
				callbacks++
				retained = l
				l.Runs[0].ProjectionIssue = "unsupported-unicode"
				l.Runs[0].Binding.SourceDigests[0] = digestOf([]byte("candidate source"))
				return changed, nil
			})
			assistantRunMust(t, err)
			if *full != wantFull {
				t.Fatal("wrong original-byte proof path", *full, wantFull)
			}
			after := assistantRunStorageValidationRead(t, store)
			if callbacks != 1 || (!changed && (!bytes.Equal(before, after) || got.Runs[0].ProjectionIssue != "")) || (changed && got.Runs[0].ProjectionIssue != "unsupported-unicode") {
				t.Fatal("wrong durable baseline or repeated callback", callbacks)
			}
			want := got.Runs[0].Binding.SourceDigests[0]
			wantIntent := bytes.Clone(got.Runs[0].Operations[0].Intent)
			retained.Runs[0].Operations[0].Intent[0] = '['
			retained.Runs[0].Checkpoint.OutputGuard["proposal"] = 99
			if !bytes.Equal(got.Runs[0].Operations[0].Intent, wantIntent) || got.Runs[0].Checkpoint.OutputGuard["proposal"] != 1 {
				t.Fatal("callback byte slice or map aliases the snapshot")
			}
			got.Runs[0].Operations[0].Intent[0] = 'x'
			got.Runs[0].Checkpoint.OutputGuard["proposal"] = 100
			if retained.Runs[0].Operations[0].Intent[0] != '[' || retained.Runs[0].Checkpoint.OutputGuard["proposal"] != 99 {
				t.Fatal("snapshot byte slice or map aliases the callback")
			}
			retained.Runs[0].Binding.SourceDigests[0] = "callback-alias"
			retained.Runs[0].Checkpoint.RequestKey = "callback-checkpoint"
			if got.Runs[0].Binding.SourceDigests[0] != want || got.Runs[0].Checkpoint.RequestKey == "callback-checkpoint" {
				t.Fatal("callback graph escaped through snapshot")
			}
			got.Runs[0].Binding.SourceDigests[0] = "returned-alias"
			got.Runs[0].Checkpoint.RequestKey = "returned-checkpoint"
			if retained.Runs[0].Binding.SourceDigests[0] != "callback-alias" || retained.Runs[0].Checkpoint.RequestKey != "callback-checkpoint" {
				t.Fatal("returned graph aliases callback graph")
			}
			next, err := store.transaction(t.Context(), func(*assistantRunLedger) (bool, error) { return false, nil })
			assistantRunMust(t, err)
			if *full != wantFull {
				t.Fatal("independent graphs repeated full storage validation", *full, wantFull)
			}
			if !bytes.Equal(next.Runs[0].Operations[0].Intent, wantIntent) || next.Runs[0].Checkpoint.OutputGuard["proposal"] != 1 || next.Runs[0].Binding.SourceDigests[0] != want || next.Runs[0].Checkpoint.RequestKey == "returned-checkpoint" || !bytes.Equal(after, assistantRunStorageValidationRead(t, store)) || !bytes.Equal(after, store.server.assistantRunValidation.raw) {
				t.Fatal("alias mutation changed disk, fact or future read")
			}
		})
	}
}

func TestAssistantRunStorageValidationMissingBaseline(t *testing.T) {
	store, _, _ := assistantRunFixture(t)
	callbacks := 0
	got, err := store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
		callbacks++
		l.Version = 9
		return false, nil
	})
	assistantRunMust(t, err)
	if got.Version != 1 || got.Project != store.project || got.Runs == nil || callbacks != 1 || store.server.assistantRunValidation != nil {
		t.Fatal("missing baseline was mutated, cached or replayed")
	}
	data, err := store.server.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	if _, err := data.root.Stat(store.name); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("false-return callback wrote a missing ledger", err)
	}
}

func TestAssistantRunStorageValidationFailuresDoNotPublish(t *testing.T) {
	for _, mode := range []string{"callback", "invalid", "cancel-true", "cancel-false", "before-rename", "readback-error", "readback-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			store, _, _, _, _, _ := assistantRunAdmitted(t)
			before := assistantRunStorageValidationRead(t, store)
			proof := store.server.assistantRunValidation
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			injected := errors.New("handwritten storage failure")
			callbacks, reads := 0, 0
			data, err := store.server.openChatData()
			assistantRunMust(t, err)
			defer data.root.Close()
			t.Cleanup(func() { testHookPrivateRead = nil })
			got, err := store.transaction(ctx, func(l *assistantRunLedger) (bool, error) {
				callbacks++
				l.Runs[0].ProjectionIssue = "unsupported-unicode"
				switch mode {
				case "callback":
					return false, injected
				case "invalid":
					l.Runs[0].Binding.Project = strings.Repeat("f", 64)
				case "cancel-true", "cancel-false":
					cancel()
				case "before-rename":
					// A directory in the target slot makes the real rename fail.
					assistantRunMust(t, data.root.Remove(store.name))
					assistantRunMust(t, data.root.Mkdir(store.name, 0700))
				case "readback-error", "readback-mismatch":
					testHookPrivateRead = func(name string) error {
						if name != store.name {
							return nil
						}
						reads++
						if mode == "readback-error" {
							return injected
						}
						// Run after the real rename, while readback has the file
						// open. Restore valid older bytes through that same inode.
						return os.WriteFile(filepath.Join(data.root.Name(), name), before, 0600)
					}
				}
				return mode != "cancel-false", nil
			})
			testHookPrivateRead = nil
			if err == nil || !reflect.DeepEqual(got, assistantRunLedger{}) || callbacks != 1 || store.server.assistantRunValidation != proof {
				t.Fatal("failed/uncertain transaction published or repeated", mode, err, callbacks)
			}
			if strings.HasPrefix(mode, "cancel-") && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
			if mode == "callback" || mode == "readback-error" {
				if !errors.Is(err, injected) {
					t.Fatal("lost underlying failure", err)
				}
			}
			if strings.HasPrefix(mode, "readback-") && reads != 1 {
				t.Fatal("write did not execute exactly one real readback", reads)
			}
			if mode == "before-rename" {
				assistantRunMust(t, data.root.Remove(store.name))
				assistantRunMust(t, writePrivateData(data.root, store.name, before))
			}
			exact := assistantRunStorageValidationRead(t, store)
			next, err := store.transaction(t.Context(), func(*assistantRunLedger) (bool, error) { return false, nil })
			assistantRunMust(t, err)
			if !bytes.Equal(exact, store.server.assistantRunValidation.raw) || next.Version != 1 {
				t.Fatal("next lookup did not validate actual post-failure bytes")
			}
		})
	}
}

func TestAssistantRunStorageValidationGlobalMutations(t *testing.T) {
	for _, mode := range []string{"duplicate-id", "duplicate-admission", "active-owner", "other-binding", "other-canonical", "capture-binding", "valid-multiple"} {
		t.Run(mode, func(t *testing.T) {
			store, _, _, _, _ := canonicalActivationAdmitted(t)
			before := assistantRunStorageValidationRead(t, store)
			proof := store.server.assistantRunValidation
			callbacks := 0
			got, err := store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
				callbacks++
				// Use independent typed storage decoding to avoid constructing
				// aliases between the original and the second run.
				var independent assistantRunLedger
				assistantRunMust(t, decodeDataJSON(before, &independent))
				other := independent.Runs[0]
				other.ID = "arun_" + strings.Repeat("b", 32)
				other.AdmissionID = "second-admission"
				other.Binding.Owner.ID = "second-owner"
				l.Runs = append(l.Runs, other)
				switch mode {
				case "duplicate-id":
					l.Runs[1].ID = l.Runs[0].ID
				case "duplicate-admission":
					l.Runs[1].AdmissionID = l.Runs[0].AdmissionID
				case "active-owner":
					l.Runs[1].Binding.Owner = l.Runs[0].Binding.Owner
				case "other-binding":
					l.Runs[1].Binding.Project = strings.Repeat("f", 64)
				case "other-canonical":
					l.Runs[1].Canonical.Head.Checkpoint[0] = '['
				case "capture-binding":
					l.Runs[1].Binding.Owner.Kind = "author-pack"
					l.Runs[1].Binding.Owner.ID = "author_" + strings.Repeat("c", 32)
				case "valid-multiple":
					l.Runs[0].ProjectionIssue = "unsupported-unicode"
					l.Runs[1].ProjectionIssue = "saved-chat-validation"
				}
				return true, nil
			})
			if callbacks != 1 {
				t.Fatal("callback replay", callbacks)
			}
			if mode == "valid-multiple" {
				assistantRunMust(t, err)
				if len(got.Runs) != 2 || got.Runs[0].ProjectionIssue != "unsupported-unicode" || got.Runs[1].ProjectionIssue != "saved-chat-validation" {
					t.Fatal("valid whole-ledger mutation lost")
				}
			} else if err == nil || !reflect.DeepEqual(got, assistantRunLedger{}) || store.server.assistantRunValidation != proof || !bytes.Equal(before, assistantRunStorageValidationRead(t, store)) {
				t.Fatal("mutation outside nominal run escaped complete validation", mode, err)
			}
		})
	}
}

func TestAssistantRunStorageValidationRawRefusalsAndRestoration(t *testing.T) {
	for _, mode := range []string{"outer-unknown", "binding-unknown", "nested-closed", "checkpoint-missing", "checkpoint-null", "case", "duplicate", "canonical", "extra", "utf8", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			store, _, _, _, _ := canonicalActivationAdmitted(t)
			original := assistantRunStorageValidationRead(t, store)
			proof := store.server.assistantRunValidation
			var top map[string]json.RawMessage
			assistantRunMust(t, json.Unmarshal(original, &top))
			var runs []map[string]json.RawMessage
			assistantRunMust(t, json.Unmarshal(top["runs"], &runs))
			var checkpoint map[string]json.RawMessage
			assistantRunMust(t, json.Unmarshal(runs[0]["checkpoint"], &checkpoint))
			switch mode {
			case "outer-unknown":
				top["unknown"] = []byte("true")
			case "binding-unknown":
				var binding map[string]json.RawMessage
				assistantRunMust(t, json.Unmarshal(runs[0]["binding"], &binding))
				binding["unknown"] = []byte("true")
				runs[0]["binding"] = assistantChatJSON(binding)
			case "nested-closed":
				checkpoint["context"] = []byte(`{"scope":"same","entries":[],"unknown":true}`)
			case "checkpoint-missing":
				delete(checkpoint, "used")
			case "checkpoint-null":
				checkpoint["used"] = []byte("null")
			case "canonical":
				var canonical assistantCanonicalJournal
				assistantRunMust(t, json.Unmarshal(runs[0]["canonical"], &canonical))
				canonical.Head.Checkpoint[0] = '['
				runs[0]["canonical"] = assistantChatJSON(canonical)
			}
			runs[0]["checkpoint"] = assistantChatJSON(checkpoint)
			top["runs"] = assistantChatJSON(runs)
			raw := assistantChatJSON(top)
			switch mode {
			case "case":
				raw = bytes.Replace(original, []byte(`"used":`), []byte(`"Used":`), 1)
			case "duplicate":
				raw = bytes.Replace(original, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1)
			case "extra":
				raw = append(bytes.Clone(original), []byte(` {}`)...)
			case "utf8":
				raw = bytes.Replace(original, []byte(store.project), append([]byte{0xff}, []byte(store.project[1:])...), 1)
			case "oversized":
				raw = append(bytes.Clone(original), bytes.Repeat([]byte(" "), assistantRunRecordLimit)...)
			}
			if bytes.Equal(raw, original) {
				t.Fatal("handwritten mutation did not change input")
			}
			if _, err := decodeAssistantRunStorage(store.name, raw); err == nil {
				t.Fatal("authoritative decoder accepted corruption", mode)
			}
			data, err := store.server.openChatData()
			assistantRunMust(t, err)
			defer data.root.Close()
			assistantRunMust(t, writePrivateData(data.root, store.name, raw))
			for i := 0; i < 2; i++ {
				callbacks := 0
				got, err := store.transaction(t.Context(), func(*assistantRunLedger) (bool, error) { callbacks++; return false, nil })
				if err == nil || callbacks != 0 || !reflect.DeepEqual(got, assistantRunLedger{}) || store.server.assistantRunValidation != proof {
					t.Fatal("warm fact admitted or cached unknown bytes", mode, err)
				}
			}
			assistantRunMust(t, writePrivateData(data.root, store.name, original))
			_, err = store.transaction(t.Context(), func(*assistantRunLedger) (bool, error) { return false, nil })
			assistantRunMust(t, err)
			if store.server.assistantRunValidation != proof {
				t.Fatal("exact restoration failed to reuse the valid fact")
			}
		})
	}
}

func TestAssistantRunStorageValidationOtherServerAndUnlockedRead(t *testing.T) {
	store, _, _, _, _, _ := assistantRunAdmitted(t)
	var baseline assistantRunLedger
	assistantRunMust(t, decodeDataJSON(assistantRunStorageValidationRead(t, store), &baseline))
	s := store.server
	other := &Server{cfg: s.cfg, configDir: s.configDir, projectDir: s.projectDir, assistant: s.assistant}
	otherStore := other.assistantRuns()
	_, err := otherStore.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
		l.Runs[0].ProjectionIssue = "saved-chat-validation"
		l.Runs[0].Revision++
		return true, nil
	})
	assistantRunMust(t, err)
	full, _ := assistantRunStorageValidationCounts(t)
	got, err := store.transaction(t.Context(), func(*assistantRunLedger) (bool, error) { return false, nil })
	assistantRunMust(t, err)
	if *full != 1 || got.Runs[0].ProjectionIssue != "saved-chat-validation" || got.Runs[0].Revision != baseline.Runs[0].Revision+1 {
		t.Fatal("fresh read reused another server's old bytes", *full)
	}
	proof := s.assistantRunValidation
	_, err = store.readSnapshot(t.Context())
	assistantRunMust(t, err)
	if *full != 2 || s.assistantRunValidation != proof {
		t.Fatal("unlocked read looked up or published a storage fact", *full)
	}
}

// This is a handwritten source/request/citation fixture, not a recorded model or
// application result. A nil excerpt is the reviewed canonical citation shape.
func assistantRunStorageValidationCreation(t *testing.T) (assistantRunStore, assistantRunLedger) {
	t.Helper()
	store, _, _, _, _, _ := assistantRunAdmitted(t)
	raw := assistantRunStorageValidationRead(t, store)
	var ledger assistantRunLedger
	assistantRunMust(t, decodeDataJSON(raw, &ledger))
	run := &ledger.Runs[0]
	location := "attachment:11111111-1111-1111-1111-111111111111/sha256:" + strings.Repeat("a", 64) + "/page/1"
	candidate := `{"sources":[{"id":"source-one","locator":{"value":"https://example.invalid/report"},"citation":{"location":"` + location + `","excerpt":"Retained exact quote."}}]}`
	source := []byte(`{"checkpoint":{"state":{"candidates":[{"text":` + string(assistantChatJSON(candidate)) + `}],"turns":[],"cases":[],"unknowns":[],"expectationIssues":[]}}}`)
	request := []byte(`{"creation":{"kind":"check","candidateDigest":"` + digestOf([]byte(candidate)) + `"}}`)
	run.Binding.SourceDigests = []string{digestOf(source)}
	run.Binding.RequestDigest = digestOf(request)
	input, err := compileAssistantCreationInput(assistantRunSource{Original: source, Request: request, Binding: run.Binding})
	assistantRunMust(t, err)
	proofs := []assistantCreationSourceProof{{Location: location, AttachmentDigest: strings.Repeat("a", 64), GatewayDigest: strings.Repeat("b", 64)}}
	input.Scope.EvidenceDigest = digestOf(assistantChatJSON(proofs))
	run.Creation = &assistantCreationReceipt{
		Source: source, Request: request, Action: "check", Scope: input.Scope, Candidate: candidate,
		Unknowns: []string{}, Cases: []assistantCreationCase{}, Issues: []assistantCreationIssue{}, Sources: proofs,
		Citations: []assistantCreationCitation{{SourceID: "source-one", Location: location, URL: "https://example.invalid/report", Quote: "Retained exact quote.", Traced: true}},
	}
	assistantRunMust(t, validateAssistantRunLedger(ledger, store.project))
	return store, ledger
}

type assistantRunStorageValidationNull string

func (assistantRunStorageValidationNull) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

type assistantRunStorageValidationMutator struct {
	ledger *assistantRunLedger
	calls  *int
}

func (m assistantRunStorageValidationMutator) MarshalJSON() ([]byte, error) {
	(*m.calls)++
	// This runs inside V(C), after the outer state check. Replacing the dynamic
	// value defeats an eligibility check deferred until after validation/marshal.
	m.ledger.Runs[0].State = "invalid-after-validation"
	m.ledger.Runs[0].Creation.Citations[0].ExcerptID = "self-replaced"
	return []byte("null"), nil
}

func TestAssistantRunStorageValidationOutputAssociation(t *testing.T) {
	for _, mode := range []string{"equal", "ledger-equal", "optional-empty", "raw-formatting", "open-value", "nullable-union", "custom-null", "raw-null"} {
		t.Run(mode, func(t *testing.T) {
			store, _, _ := assistantRunFixture(t)
			ledger := assistantRunLedger{Version: 1, Project: store.project, Runs: []assistantRunRecord{}}
			wantAssociated := mode == "equal" || mode == "ledger-equal"
			switch mode {
			case "optional-empty":
				ledger.AuthoringCaptures = []assistantAuthoringCapture{}
			case "ledger-equal", "raw-formatting", "open-value", "nullable-union":
				admitted, _, _, _, _, _ := assistantRunAdmitted(t)
				store = admitted
				assistantRunMust(t, decodeDataJSON(assistantRunStorageValidationRead(t, store), &ledger))
				switch mode {
				case "raw-formatting":
					ledger.Runs[0].Events = []assistantRunEvent{{ID: "formatted", Event: contract.RunEvent{Proposal: &contract.RunEventProposal{Type: "proposal", Document: map[string]json.RawMessage{"exact": []byte(`[ 1, null, false ]`)}, Unknowns: []string{}}}}}
				case "open-value":
					ledger.Runs[0].Checkpoint.Operations = append(ledger.Runs[0].Checkpoint.Operations, contract.CheckpointOperation{ID: "3f71c7b0-7df4-4eb6-96f3-f3a1390dc3a0", Name: "read_exact", Args: []byte(`{}`), Status: "complete", Result: &contract.ToolResultMCPToolResult{AdditionalMembers: map[string]json.RawMessage{"openData": []byte(`{ "x": 1 }`)}}})
				case "nullable-union":
					question := assistantContinuationQuestion()
					// The valid wire null restores Set=true on independent decode.
					question.UserQuestion.Question.Recommendation.Set = false
					ledger.Runs[0].Events = []assistantRunEvent{{ID: "question", Event: question}}
				}
			case "custom-null", "raw-null":
				store, ledger = assistantRunStorageValidationCreation(t)
				if mode == "custom-null" {
					ledger.Runs[0].Creation.Citations[0].ExcerptID = assistantRunStorageValidationNull("named string marshaler")
				} else {
					ledger.Runs[0].Creation.Citations[0].ExcerptID = json.RawMessage("null")
				}
			}
			_, output := assistantRunStorageValidationCounts(t)
			raw, decoded, err := prepareAssistantRunStorage(store.name, ledger)
			assistantRunMust(t, err)
			if len(*output) != 1 || (*output)[0] != wantAssociated {
				t.Fatal("wrong guarded output path", mode, *output)
			}
			full, err := decodeAssistantRunStorage(store.name, raw)
			assistantRunMust(t, err)
			if !reflect.DeepEqual(decoded, full) {
				t.Fatal("composed proof differs from authoritative full decode")
			}
			if (mode == "custom-null" || mode == "raw-null") && decoded.Runs[0].Creation.Citations[0].ExcerptID != nil {
				t.Fatal("benign dynamic marshaler changed accepted value")
			}
		})
	}
}

func TestAssistantRunStorageValidationMarshalLatch(t *testing.T) {
	store, ledger := assistantRunStorageValidationCreation(t)
	// Persist the handwritten valid receipt first, so the adversarial callback
	// tests the real transaction and leaves a meaningful valid baseline fact.
	_, err := store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) { *l = ledger; return true, nil })
	assistantRunMust(t, err)
	before := assistantRunStorageValidationRead(t, store)
	proof := store.server.assistantRunValidation
	calls, callbacks := 0, 0
	_, output := assistantRunStorageValidationCounts(t)
	got, err := store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
		callbacks++
		l.Runs[0].Creation.Citations[0].ExcerptID = assistantRunStorageValidationMutator{ledger: l, calls: &calls}
		return true, nil
	})
	if err == nil || !reflect.DeepEqual(got, assistantRunLedger{}) || calls != 1 || callbacks != 1 || len(*output) != 1 || (*output)[0] || store.server.assistantRunValidation != proof || !bytes.Equal(before, assistantRunStorageValidationRead(t, store)) {
		t.Fatal("self-replacing marshaler bypassed the pre-validation latch", err, calls, callbacks, *output)
	}
	// Ineligible values do not introduce a new refusal: a benign custom value
	// producing valid storage succeeds through the full decoded fallback.
	got, err = store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
		l.Runs[0].Creation.Citations[0].ExcerptID = assistantRunStorageValidationNull("valid")
		return true, nil
	})
	assistantRunMust(t, err)
	if got.Runs[0].Creation.Citations[0].ExcerptID != nil || len(*output) != 2 || (*output)[1] {
		t.Fatal("valid ineligible value did not take the independent fallback", *output)
	}
}

func TestAssistantRunStorageValidationMarshalTypeGraph(t *testing.T) {
	// Reviewed against Agent v0.11.0 contract_gen.go's object/union templates
	// and support.go's Nullable/encodeObject. These hooks only read their input.
	// Agent v0.13.2's RunPrincipal family was also reviewed: its concrete
	// object encoders and finite Software union only read their input; the Raw
	// branch returns retained bytes without changing them.
	// This is an explicit concrete inventory, not permission for a package. A
	// component update must audit hook bodies again before changing this list.
	const reviewedContractHooks = `
Checkpoint
CheckpointAcceptedPlan
CheckpointAcceptedPlanReferencesItem
CheckpointAcceptedPlanValidatedReferencesItem
CheckpointApproval
CheckpointApprovalDecision
CheckpointArtifactObservation
CheckpointChildUsageItem
CheckpointContext
CheckpointCritic
CheckpointDraft
CheckpointDraftDocument
CheckpointDurableBook
CheckpointDurableBookChildrenItem
CheckpointDurableBookPendingHandoff
CheckpointDurableDispatch
CheckpointDurableObservation
CheckpointDurableScope
CheckpointFailoverItem
CheckpointFailoverItemHistoryItem
CheckpointGrant
CheckpointHandoff
CheckpointNativePlanInvocation
CheckpointNativePlanPause
CheckpointOperation
CheckpointPendingDeparture
CheckpointPendingDepartureCandidate
CheckpointPlanChange
CheckpointPlanDecision
CheckpointPlanDeparture
CheckpointPlanDepartureRecord
CheckpointPlanOrigin
CheckpointPlanRefusal
CheckpointPlanRevision
CheckpointPlanState
CheckpointRepeatExecution
CheckpointRepeatObservation
CheckpointRepeatTools
CheckpointRetainedEntry
CheckpointRuntimeApprovalBinding
CheckpointRuntimeExecutionOwner
CheckpointSourceIdentity
CheckpointStepEvidence
CheckpointStepEvidenceArtifactsItem
CheckpointTaskChildBinding
CheckpointTaskChildResult
CheckpointTaskCompletionEvidence
CheckpointTaskCoordinatorBinding
CheckpointTaskEnvelopeBinding
CheckpointTaskExecutionEnvelope
CheckpointTaskPendingDelegation
CheckpointTaskSchedule
CheckpointTaskScheduleBinding
CheckpointTaskStep
CheckpointTaskStepClaim
CheckpointTaskStepRow
CheckpointTaskStepShare
DelegatedTask
DelegatedTaskAnalyzeFinding
DelegatedTaskAnalyzeReport
DelegatedTaskAnswer
DelegatedTaskAnswerTask
DelegatedTaskAnswerTaskUsed
DelegatedTaskAuthorTestFinding
DelegatedTaskAuthorTestReport
DelegatedTaskBudget
DelegatedTaskCitation
DelegatedTaskOpenQuestion
DelegatedTaskReport
DelegatedTaskRequest
DelegatedTaskResearchFinding
DelegatedTaskResearchReport
DelegatedTaskResultCursor
DelegatedTaskRetried
DelegatedTaskReviewFinding
DelegatedTaskReviewReport
DelegatedTaskStepsItem
DelegatedTaskTaskRequest
Failure
Memory
MemorySource
Nullable[string]
PolicyMember
Provenance
ProvenanceBatchFigures
ProvenanceChildFigures
ProvenanceCompactionReport
ProvenanceCompactionSummary
ProvenanceCompletion
ProvenanceCriticFigures
ProvenanceCrossProvider
ProvenanceDelegationReport
ProvenanceDeliveredSteer
ProvenanceEdit
ProvenanceFiguresTotal
ProvenanceIdentityFigures
ProvenanceMemories
ProvenanceMemoryRecall
ProvenanceParentFigures
ProvenancePromptDigest
ProvenanceProvider
ProvenanceQueuedSteer
ProvenanceRecordFigures
ProvenanceSkillLoad
ProvenanceSourceFigures
ProvenanceSteerReport
ProvenanceTokenFigures
Question
QuestionOption
RunEvent
RunEventApprovalRequested
RunEventApprovalRequestedPlan
RunEventBudgetPaused
RunEventCaseComparison
RunEventCaseOutcome
RunEventCaseOutcomeOption1
RunEventCaseOutcomeOption2
RunEventCompacted
RunEventConnectionSwitched
RunEventCriticDecided
RunEventCritique
RunEventCritiqueChecksItem
RunEventDelegatedTask
RunEventDelegationRequested
RunEventEditApplied
RunEventEditRefused
RunEventEnd
RunEventError
RunEventExecutionCheckpoint
RunEventExportWritten
RunEventGuardrail
RunEventMemoryProposed
RunEventMemoryRecalled
RunEventMessage
RunEventMessageProgress
RunEventOutputGuarded
RunEventPlanCompletionRefused
RunEventPlanDeparted
RunEventPlanDriftDecided
RunEventPlanRevised
RunEventPolicyResolved
RunEventProposal
RunEventProposalCritique
RunEventProposalNote
RunEventReasoning
RunEventRecorded
RunEventRepeatDetected
RunEventResponseRecovery
RunEventRetry
RunEventRouteDecided
RunEventRouteWidened
RunEventSkillLoaded
RunEventSkillUnloaded
RunEventSteered
RunEventThinkingUnavailable
RunEventUsage
RunEventUserQuestion
RunPrincipal
RunPrincipalActor
RunPrincipalDelegation
RunPrincipalPrincipalReference
RunPrincipalRefusal
RunPrincipalSoftware
RunPrincipalSoftwareOption1
RunPrincipalSoftwareOption2
SubagentRecord
SubagentRecordSource
ToolCall
ToolCallPresentation
ToolOfferIdentity
ToolOfferLoadedSkill
ToolOfferSourceRecord
ToolResult
ToolResultImagePart
ToolResultMCPToolResult
ToolResultMCPToolResultContentItem
ToolResultMCPToolResultContentItemOption1
TurnRoute
TurnRouteClassifierUsage_
TurnRouteClassifier_
TurnRouteOperations
TurnRouteRefusalsItem
TurnRouteTools
TurnRouteWidenedInvocation
Usage
UsageBudget
UsageBudgetAmounts
UsageBudgetEntry
UsageBudgetReceipt
UsageBudgetRefusal
UsageBudgetReservation
UsageBudgetScope
UsageRow
UsageTokens
`
	contractPackage := reflect.TypeOf(contract.Checkpoint{}).PkgPath()
	type hooks struct{ valueJSON, pointerJSON, valueText, pointerText, valueZero, pointerZero bool }
	jsonHook := reflect.TypeFor[json.Marshaler]()
	textHook := reflect.TypeFor[encoding.TextMarshaler]()
	zeroHook := reflect.TypeFor[interface{ IsZero() bool }]()
	expected := map[string]hooks{"encoding/json.RawMessage": {valueJSON: true, pointerJSON: true}}
	for _, name := range strings.Fields(reviewedContractHooks) {
		entry := hooks{valueJSON: true, pointerJSON: true}
		if name == "Nullable[string]" {
			entry.valueZero, entry.pointerZero = true, true
		}
		expected[contractPackage+"."+name] = entry
	}
	seen, actual := map[reflect.Type]bool{}, map[string]hooks{}
	interfaces := []string{}
	var walk func(reflect.Type, string)
	walk = func(shape reflect.Type, path string) {
		if shape.Kind() == reflect.Interface {
			interfaces = append(interfaces, path)
			return
		}
		if seen[shape] {
			return
		}
		seen[shape] = true
		if shape.Kind() == reflect.Pointer {
			walk(shape.Elem(), path+"*")
			return
		}
		pointer := reflect.PointerTo(shape)
		entry := hooks{shape.Implements(jsonHook), pointer.Implements(jsonHook), shape.Implements(textHook), pointer.Implements(textHook), shape.Implements(zeroHook), pointer.Implements(zeroHook)}
		if entry != (hooks{}) {
			actual[shape.PkgPath()+"."+shape.Name()] = entry
		}
		switch shape.Kind() {
		case reflect.Struct:
			for i := 0; i < shape.NumField(); i++ {
				field := shape.Field(i)
				walk(field.Type, shape.PkgPath()+"."+shape.Name()+"."+field.Name)
			}
		case reflect.Map:
			walk(shape.Key(), path+"{key}")
			walk(shape.Elem(), path+"{value}")
		case reflect.Slice, reflect.Array:
			walk(shape.Elem(), path+"[]")
		case reflect.Func, reflect.Chan, reflect.UnsafePointer:
			t.Errorf("new unreviewed reachable field %s: %s", path, shape)
		}
	}
	walk(reflect.TypeOf(assistantRunLedger{}), "ledger")
	sort.Strings(interfaces)
	wantInterfaces := []string{reflect.TypeOf(assistantCreationCitation{}).PkgPath() + ".assistantCreationCitation.ExcerptID"}
	if !reflect.DeepEqual(interfaces, wantInterfaces) {
		t.Fatalf("dynamic field inventory changed; audit eligibility: got %v, want %v", interfaces, wantInterfaces)
	}
	if !reflect.DeepEqual(actual, expected) {
		for name, entry := range actual {
			if want, ok := expected[name]; !ok || entry != want {
				t.Errorf("unreviewed reachable hook %s: %+v (reviewed %+v)", name, entry, want)
			}
		}
		for name := range expected {
			if _, ok := actual[name]; !ok {
				t.Errorf("reviewed hook no longer reachable: %s", name)
			}
		}
	}
}
