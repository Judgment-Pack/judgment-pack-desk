package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestAssistantRunReadLockedValidationEquivalence(t *testing.T) {
	store, _, _, _, _ := canonicalActivationAdmitted(t)
	original := assistantRunRaw(t, store)
	lock, err := store.server.privateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	defer lock.Close()
	data, err := store.server.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	for _, mode := range []string{"valid", "version", "project", "binding-project", "binding-request", "outer-state", "checkpoint-state", "canonical-required", "canonical-head", "canonical-envelope", "canonical-projection", "canonical-cursor", "duplicate-member", "unknown-member", "case-member", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			var changed assistantRunLedger
			assistantRunMust(t, json.Unmarshal(original, &changed))
			run := &changed.Runs[0]
			switch mode {
			case "version":
				changed.Version = 2
			case "project":
				changed.Project = strings.Repeat("f", 64)
			case "binding-project":
				run.Binding.Project = strings.Repeat("f", 64)
			case "binding-request":
				run.Binding.ExecutionKey = strings.Repeat("f", 64)
			case "outer-state":
				run.State = "unrecognized"
			case "checkpoint-state":
				run.Checkpoint.Status = "unrecognized"
			case "canonical-required":
				run.CanonicalRequired = false
			case "canonical-head":
				run.Canonical.Head.Checkpoint = []byte(`{"broken":true}`)
			case "canonical-envelope":
				run.Canonical.Events = append(run.Canonical.Events, assistantCanonicalEvent{Cursor: "bad-envelope", Bytes: []byte(`{"version":1,"scope":{"execution":"other","role":"author"},"event":{"type":"tool_call","name":7}}`)})
				run.Canonical.Head.Cursor = "bad-envelope"
				run.Canonical.Commits[len(run.Canonical.Commits)-1].Cursor = "bad-envelope"
			case "canonical-projection":
				run.Checkpoint.ID = "different-execution"
			case "canonical-cursor":
				run.Canonical.Head.Cursor = "absent-cursor"
			}
			raw := assistantChatJSON(changed)
			switch mode {
			case "duplicate-member":
				raw = bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1)
			case "unknown-member":
				raw = bytes.Replace(raw, []byte(`"version":1`), []byte(`"extra":true,"version":1`), 1)
			case "case-member":
				raw = bytes.Replace(raw, []byte(`"version":1`), []byte(`"Version":1`), 1)
			case "truncated":
				raw = raw[:len(raw)-1]
			}
			assistantRunMust(t, writePrivateData(data.root, store.name, raw))
			decoded, decodeErr := decodeAssistantRunStorage(store.name, raw)
			// This is the previous read path's additional validation. Neither it
			// nor readLocked may admit any of these handwritten corruptions.
			previousErr := decodeErr
			if previousErr == nil {
				previousErr = validateAssistantRunLedger(decoded, store.project)
			}
			actual, actualErr := store.readLocked(t.Context(), data)
			if mode == "valid" {
				assistantRunMust(t, previousErr)
				assistantRunMust(t, actualErr)
				if !reflect.DeepEqual(actual, decoded) {
					t.Fatal("read changed the validated snapshot")
				}
			} else if previousErr == nil || actualErr == nil {
				t.Fatal("corrupt snapshot admitted", mode, previousErr, actualErr)
			}
			after, err := readPrivateData(data.root, store.name, assistantRunRecordLimit)
			assistantRunMust(t, err)
			if !bytes.Equal(after, raw) {
				t.Fatal("read rewrote source bytes")
			}
		})
	}
	assistantRunMust(t, writePrivateData(data.root, store.name, original))
	for _, mode := range []string{"filename", "store-project", "filename-project"} {
		t.Run(mode, func(t *testing.T) {
			other := store
			switch mode {
			case "filename":
				other.name = "assistant-runs-invalid.json"
			case "store-project":
				other.project = strings.Repeat("f", 64)
			case "filename-project":
				other.project = strings.Repeat("f", 64)
				other.name = "assistant-runs-" + other.project + ".json"
			}
			if _, err := other.readLocked(t.Context(), data); !errors.Is(err, errAssistantRunScope) {
				t.Fatal("foreign store namespace admitted", mode, err)
			}
		})
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.readLocked(canceled, data); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled read admitted", err)
	}
}

func TestAssistantRunReadLockedMissingLedger(t *testing.T) {
	store, _, _ := assistantRunFixture(t)
	lock, err := store.server.privateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	defer lock.Close()
	data, err := store.server.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	ledger, err := store.readLocked(t.Context(), data)
	assistantRunMust(t, err)
	if ledger.Version != 1 || ledger.Project != store.project || ledger.Runs == nil || len(ledger.Runs) != 0 {
		t.Fatal("invalid default ledger")
	}
	if _, err = data.root.Stat(store.name); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created a ledger", err)
	}
	assistantRunMust(t, validateAssistantRunLedger(ledger, store.project))
}
