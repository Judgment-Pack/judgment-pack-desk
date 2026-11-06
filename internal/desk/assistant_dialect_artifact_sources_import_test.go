package desk

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func dialectArtifactImportedRunFixture(t *testing.T, availability string) assistantRunRecord {
	t.Helper()
	run, _, _ := assistantDialectFactoryFixtureRun(t)
	run.Historical, run.State = true, "interrupted"
	run.DialectArtifactOwner = strings.Repeat("d", 64)
	run.DialectArtifactImport = &assistantDialectArtifactImport{Version: 1, Slots: []assistantDialectArtifactImportSlot{{
		HostOperationID: run.DialectDrafts[0].HostOperationID, Stage: "candidate", Availability: availability,
	}}}
	return run
}

func cloneDialectArtifactImportedRun(t *testing.T, run assistantRunRecord) assistantRunRecord {
	t.Helper()
	var copied assistantRunRecord
	assistantRunMust(t, json.Unmarshal(assistantChatJSON(run), &copied))
	return copied
}

func TestDialectArtifactImportClosedRaw(t *testing.T) {
	good := []byte(`{"version":1,"slots":[{"hostOperationId":"root","stage":"candidate","availability":"missing"}]}`)
	assistantRunMust(t, readAssistantDialectArtifactImport(good))
	for name, bad := range map[string][]byte{
		"null":                []byte(`null`),
		"missing-version":     []byte(`{"slots":[]}`),
		"missing-slots":       []byte(`{"version":1}`),
		"slots-null":          []byte(`{"version":1,"slots":null}`),
		"slots-object":        []byte(`{"version":1,"slots":{}}`),
		"row-null":            []byte(`{"version":1,"slots":[null]}`),
		"row-missing-control": []byte(`{"version":1,"slots":[{"hostOperationId":"root","stage":"candidate"}]}`),
		"version-null":        bytes.Replace(good, []byte(`"version":1`), []byte(`"version":null`), 1),
		"fraction":            bytes.Replace(good, []byte(`"version":1`), []byte(`"version":1.0`), 1),
		"version-string":      bytes.Replace(good, []byte(`"version":1`), []byte(`"version":"1"`), 1),
		"duplicate":           bytes.Replace(good, []byte(`"stage":`), []byte(`"stage":"candidate","stage":`), 1),
		"case":                bytes.Replace(good, []byte(`"availability":`), []byte(`"Availability":`), 1),
		"unknown":             bytes.Replace(good, []byte(`"version":`), []byte(`"unknown":false,"version":`), 1),
		"control-null":        bytes.Replace(good, []byte(`"availability":"missing"`), []byte(`"availability":null`), 1),
		"coerced-control":     bytes.Replace(good, []byte(`"availability":"missing"`), []byte(`"availability":false`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if readAssistantDialectArtifactImport(bad) == nil {
				t.Fatal("accepted malformed import controls", string(bad))
			}
		})
	}
}

func TestDialectArtifactImportExactIntentCoverage(t *testing.T) {
	for _, availability := range []string{"present", "missing"} {
		t.Run(availability, func(t *testing.T) {
			run := dialectArtifactImportedRunFixture(t, availability)
			assistantRunMust(t, validateAssistantDialectArtifactImport(run))
			got, err := assistantDialectArtifactImportedAvailability(run, run.DialectDrafts[0].HostOperationID, "candidate")
			if err != nil || got != availability || run.DialectDrafts[0].Candidate.Receipt != nil || run.DialectDrafts[0].Reconciled {
				t.Fatal("import availability changed unknown custody", got, err)
			}
			if _, err := assistantDialectArtifactImportedAvailability(run, "other-root", "candidate"); err == nil {
				t.Fatal("foreign root was selected")
			}
		})
	}
	base := dialectArtifactImportedRunFixture(t, "missing")
	for name, mutate := range map[string]func(*assistantRunRecord){
		"version":     func(v *assistantRunRecord) { v.DialectArtifactImport.Version = 2 },
		"native":      func(v *assistantRunRecord) { v.Historical = false },
		"executable":  func(v *assistantRunRecord) { v.State = "running" },
		"ownerless":   func(v *assistantRunRecord) { v.DialectArtifactOwner = "" },
		"no-custody":  func(v *assistantRunRecord) { v.DialectDrafts = nil },
		"nil-slots":   func(v *assistantRunRecord) { v.DialectArtifactImport.Slots = nil },
		"empty-slots": func(v *assistantRunRecord) { v.DialectArtifactImport.Slots = []assistantDialectArtifactImportSlot{} },
		"extra-row": func(v *assistantRunRecord) {
			v.DialectArtifactImport.Slots = append(v.DialectArtifactImport.Slots, assistantDialectArtifactImportSlot{HostOperationID: "extra", Stage: "candidate", Availability: "present"})
		},
		"duplicate-row": func(v *assistantRunRecord) {
			v.DialectArtifactImport.Slots = append(v.DialectArtifactImport.Slots, v.DialectArtifactImport.Slots[0])
		},
		"wrong-root":       func(v *assistantRunRecord) { v.DialectArtifactImport.Slots[0].HostOperationID = "other" },
		"wrong-stage":      func(v *assistantRunRecord) { v.DialectArtifactImport.Slots[0].Stage = "final" },
		"wrong-case":       func(v *assistantRunRecord) { v.DialectArtifactImport.Slots[0].Stage = "Candidate" },
		"bad-availability": func(v *assistantRunRecord) { v.DialectArtifactImport.Slots[0].Availability = "absent" },
		"acknowledged-missing": func(v *assistantRunRecord) {
			in := *v.DialectDrafts[0].Candidate.Intent
			v.DialectDrafts[0].Candidate.Receipt = &in
		},
		"no-intent": func(v *assistantRunRecord) { v.DialectDrafts[0].Candidate.Intent = nil },
	} {
		t.Run(name, func(t *testing.T) {
			changed := cloneDialectArtifactImportedRun(t, base)
			mutate(&changed)
			if validateAssistantDialectArtifactImport(changed) == nil {
				t.Fatal("accepted changed import coverage")
			}
		})
	}
}

func TestDialectArtifactImportStorageRoundTripAndRawGuard(t *testing.T) {
	run := dialectArtifactImportedRunFixture(t, "missing")
	ledger := assistantRunLedger{Version: 1, Project: run.Binding.Project, Runs: []assistantRunRecord{run}}
	name := "assistant-runs-" + ledger.Project + ".json"
	raw, saved, err := prepareAssistantRunStorage(name, ledger)
	assistantRunMust(t, err)
	decoded, err := decodeAssistantRunStorage(name, raw)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(decoded.Runs[0].DialectArtifactImport, run.DialectArtifactImport) ||
		!reflect.DeepEqual(decoded.Runs[0].DialectDrafts, run.DialectDrafts) ||
		!reflect.DeepEqual(saved.Runs[0].Operations, run.Operations) {
		t.Fatal("historical import rewrote immutable inner evidence")
	}
	for name, mutate := range map[string]func([]byte) []byte{
		"null": func(v []byte) []byte {
			return bytes.Replace(v, assistantChatJSON(run.DialectArtifactImport), []byte(`null`), 1)
		},
		"case": func(v []byte) []byte {
			return bytes.Replace(v, []byte(`"dialectArtifactImport":`), []byte(`"DialectArtifactImport":`), 1)
		},
		"duplicate": func(v []byte) []byte {
			return bytes.Replace(v, []byte(`"dialectArtifactImport":`), append(append([]byte(`"dialectArtifactImport":`), assistantChatJSON(run.DialectArtifactImport)...), []byte(`,"dialectArtifactImport":`)...), 1)
		},
		"partial": func(v []byte) []byte {
			return bytes.Replace(v, assistantChatJSON(run.DialectArtifactImport), []byte(`{"version":1,"slots":[]}`), 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := mutate(bytes.Clone(raw))
			if bytes.Equal(changed, raw) {
				t.Fatal("mutation did not change fixture")
			}
			if _, err := decodeAssistantRunStorage("assistant-runs-"+ledger.Project+".json", changed); err == nil {
				t.Fatal("original storage accepted malformed import metadata")
			}
		})
	}
	legacy, _, _ := assistantDialectFactoryFixtureRun(t)
	legacyLedger := assistantRunLedger{Version: 1, Project: legacy.Binding.Project, Runs: []assistantRunRecord{legacy}}
	_, _, err = prepareAssistantRunStorage(name, legacyLedger)
	assistantRunMust(t, err)
}

func TestDialectArtifactImportOrdinaryTransitionIsImmutable(t *testing.T) {
	base := dialectArtifactImportedRunFixture(t, "missing")
	before := assistantRunLedger{Version: 1, Project: base.Binding.Project, Runs: []assistantRunRecord{base}}
	assistantRunMust(t, validateAssistantDialectArtifactOwnerTransition(before, before, nil))
	for name, mutate := range map[string]func(*assistantRunRecord){
		"drop":         func(v *assistantRunRecord) { v.DialectArtifactImport = nil },
		"availability": func(v *assistantRunRecord) { v.DialectArtifactImport.Slots[0].Availability = "present" },
		"owner":        func(v *assistantRunRecord) { v.DialectArtifactOwner = strings.Repeat("e", 64) },
		"historical":   func(v *assistantRunRecord) { v.Historical = false },
		"executable":   func(v *assistantRunRecord) { v.State = "running" },
		"receipt": func(v *assistantRunRecord) {
			in := *v.DialectDrafts[0].Candidate.Intent
			v.DialectDrafts[0].Candidate.Receipt = &in
		},
		"reconciled": func(v *assistantRunRecord) { v.DialectDrafts[0].Reconciled = true },
	} {
		t.Run(name, func(t *testing.T) {
			changed := cloneDialectArtifactImportedRun(t, base)
			mutate(&changed)
			after := assistantRunLedger{Version: 1, Project: before.Project, Runs: []assistantRunRecord{changed}}
			if validateAssistantDialectArtifactOwnerTransition(before, after, nil) == nil {
				t.Fatal("ordinary writer changed imported historical provenance")
			}
		})
	}
	without := cloneDialectArtifactImportedRun(t, base)
	without.DialectArtifactImport = nil
	if validateAssistantDialectArtifactOwnerTransition(assistantRunLedger{Runs: []assistantRunRecord{without}}, before, nil) == nil ||
		validateAssistantDialectArtifactOwnerTransition(assistantRunLedger{}, before, nil) == nil ||
		validateAssistantDialectArtifactOwnerTransition(before, assistantRunLedger{}, nil) == nil {
		t.Fatal("ordinary writer added import provenance or deleted an owned run")
	}
}
