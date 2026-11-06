//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDialectArtifactRunOwnerHistory(t *testing.T) {
	run, _, _ := assistantDialectFactoryFixtureRun(t)
	run.DialectArtifactOwner = strings.Repeat("f", 64) // Detached data, no live owner.
	ledger := assistantRunLedger{Version: 1, Project: run.Binding.Project, Runs: []assistantRunRecord{run}}
	name := "assistant-runs-" + ledger.Project + ".json"
	raw, err := json.Marshal(ledger)
	assistantRunMust(t, err)
	_, err = decodeAssistantRunStorage(name, raw)
	assistantRunMust(t, err)
	open := func() *chatDataStore {
		root, err := os.OpenRoot(t.TempDir())
		assistantRunMust(t, err)
		t.Cleanup(func() { root.Close() })
		return &chatDataStore{root: root}
	}
	source, copiedStore, restored := open(), open(), open()
	assistantRunMust(t, writePrivateData(source.root, name, raw))
	var copied []string
	assistantRunMust(t, copyChatData(t.Context(), source, copiedStore, &copied))
	copiedRaw, err := readPrivateData(copiedStore.root, name, assistantRunRecordLimit)
	assistantRunMust(t, err)
	if !bytes.Equal(copiedRaw, raw) {
		t.Fatal("history copy changed original run bytes")
	}
	file, err := os.Create(filepath.Join(t.TempDir(), "history.zip"))
	assistantRunMust(t, err)
	defer file.Close()
	assistantRunMust(t, buildChatBackup(t.Context(), source, file))
	_, manifest, err := validateChatBackup(file)
	assistantRunMust(t, err)
	found := false
	for _, entry := range manifest.Files {
		if entry.Name == name {
			found = true
			if entry.Bytes != int64(len(raw)) || entry.SHA256 != digestOf(raw) {
				t.Fatal("backup checksum changed provenance")
			}
		}
	}
	if !found {
		t.Fatal("run entry missing from backup")
	}
	copied = nil
	assistantRunMust(t, copyChatBackup(t.Context(), file, restored, &copied))
	restoredRaw, err := readPrivateData(restored.root, name, assistantRunRecordLimit)
	assistantRunMust(t, err)
	history, err := decodeAssistantRunStorage(name, restoredRaw)
	assistantRunMust(t, err)
	got := history.Runs[0]
	if got.DialectArtifactOwner != run.DialectArtifactOwner || !got.Historical || got.State != "interrupted" || !reflect.DeepEqual(got.Binding, run.Binding) || !reflect.DeepEqual(got.DialectDrafts, run.DialectDrafts) {
		t.Fatal("historical import changed provenance/receipt or retained dispatch")
	}
	for i, op := range got.Operations {
		if !bytes.Equal(op.Intent, run.Operations[i].Intent) || !bytes.Equal(op.Result, run.Operations[i].Result) {
			t.Fatal("historical import reserialized operation/result bytes")
		}
	}
	if got.Operations[1].State != "uncertain" {
		t.Fatal("imported root remained dispatchable")
	}
	// These detached data-only paths have no Server/provider registration or
	// original owner to reconstruct. Their owner strings are never used to Get.
}

func TestDialectArtifactRunOwnerHistoryCacheAndStop(t *testing.T) {
	f := assistantDialectAuthoritySetup(t)
	f.reserve(t, false)
	store := f.owner.lease.store
	one, err := store.Read(f.ctx, f.owner.binding, f.owner.runID)
	assistantRunMust(t, err)
	one.DialectArtifactOwner = strings.Repeat("e", 64)
	one.DialectDrafts[0].Candidate.Intent.Receipt = "foreign"
	two, err := store.Read(f.ctx, f.owner.binding, f.owner.runID)
	assistantRunMust(t, err)
	if two.DialectArtifactOwner != f.owner.enrollment.Owner || two.DialectDrafts[0].Candidate.Intent.Receipt == "foreign" {
		t.Fatal("snapshot alias changed saved provenance")
	}
	assistantRunMust(t, f.execution.writer.Stop(f.ctx))
	stopped, err := store.Read(f.ctx, f.owner.binding, f.owner.runID)
	assistantRunMust(t, err)
	if stopped.DialectArtifactOwner != f.owner.enrollment.Owner || !assistantRunTerminal(stopped.State) || stopped.DialectDrafts[0].Candidate.Receipt != nil {
		t.Fatal("Stop erased provenance or invented receipt")
	}
	before := assistantRunStorageValidationRead(t, store)
	_, err = store.transaction(f.ctx, func(ledger *assistantRunLedger) (bool, error) { return interruptAssistantRuns(ledger, false), nil })
	assistantRunMust(t, err)
	if !bytes.Equal(before, assistantRunStorageValidationRead(t, store)) {
		t.Fatal("already stopped recovery changed provenance")
	}
}

func TestDialectArtifactRunOwnerHistoryLegacyTransitions(t *testing.T) {
	run, _, _ := assistantDialectFactoryFixtureRun(t)
	before := assistantRunLedger{Version: 1, Project: run.Binding.Project, Runs: []assistantRunRecord{run}}
	clone := func() assistantRunLedger {
		var out assistantRunLedger
		assistantRunMust(t, json.Unmarshal(assistantChatJSON(before), &out))
		return out
	}
	for _, mode := range []string{"owner", "receipt", "reconciled", "new-intent"} {
		t.Run(mode, func(t *testing.T) {
			after := clone()
			held := &after.Runs[0].DialectDrafts[0]
			switch mode {
			case "owner":
				after.Runs[0].DialectArtifactOwner = strings.Repeat("a", 64)
			case "receipt":
				intent := *held.Candidate.Intent
				held.Candidate.Receipt = &intent
			case "reconciled":
				held.Reconciled = true
			case "new-intent":
				held.Checked.Intent = held.Candidate.Intent
			}
			if validateAssistantDialectArtifactOwnerTransition(before, after, nil) == nil {
				t.Fatal("legacy data gained live ownership/acknowledgment")
			}
		})
	}
	after := clone()
	after.Runs[0].DialectDrafts[0].Uncertainty = "storage-unacknowledged"
	assistantRunMust(t, validateAssistantDialectArtifactOwnerTransition(before, after, nil))
	interruptAssistantRuns(&after, true)
	assistantRunMust(t, validateAssistantDialectArtifactOwnerTransition(before, after, nil))
}
