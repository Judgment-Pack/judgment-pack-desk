package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Handwritten custody fixture adapted to the actual enrolled namespace. The
// receipt framing remains production framing over those explicit test inputs;
// no fixture is captured from a factory/check/model execution.
func selectedTransferRunFixture(t *testing.T, enrolled dialectArtifactOwnerEnrollment, acknowledged bool) (assistantRunRecord, []byte) {
	t.Helper()
	run, _, raw := assistantDialectFactoryFixtureRun(t)
	run.Binding.Project = enrolled.Namespace
	run.DialectArtifactOwner = enrolled.Owner
	held := &run.DialectDrafts[0]
	held.Candidate.Intent.Receipt = assistantDialectDraftReceipt(run, *held, "candidate", *held.Candidate.Intent)
	if acknowledged {
		receipt := *held.Candidate.Intent
		held.Candidate.Receipt = &receipt
	}
	return run, raw
}

func selectedTransferSaveRun(t *testing.T, s *Server, run assistantRunRecord) {
	t.Helper()
	ledger := assistantRunLedger{Version: 1, Project: run.Binding.Project, Runs: []assistantRunRecord{run}}
	name := "assistant-runs-" + run.Binding.Project + ".json"
	raw, _, err := prepareAssistantRunStorage(name, ledger)
	assistantRunMust(t, err)
	data, err := s.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	assistantRunMust(t, writePrivateData(data.root, name, raw))
}

func selectedTransferCurrentWitness(t *testing.T, s *Server) *os.File {
	t.Helper()
	file, err := s.assistant.root.OpenFile(".data-work.lock", os.O_RDWR|openNoFollow|openNonBlocking, 0)
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func TestDialectArtifactSelectedTransferNativeExactReadAndQueue(t *testing.T) {
	s, request := dialectArtifactOwnerSetup(t)
	enrolled, err := s.enrollDialectArtifactOwner(request)
	assistantRunMust(t, err)
	run, raw := selectedTransferRunFixture(t, enrolled, true)
	selectedTransferSaveRun(t, s, run)
	_, err = retainedArtifactsForProject(s.root).Put(t.Context(), raw)
	assistantRunMust(t, err)
	witness := selectedTransferCurrentWitness(t, s)
	var escaped retainedArtifactTransferView
	err = s.withSelectedDialectArtifactSources(request, witness, func(sources *dialectArtifactSelectedSources) error {
		if len(sources.initial.slots) != 1 {
			return errors.New("wrong exact slot selection")
		}
		slot := sources.initial.slots[0]
		return sources.withExactSource(slot, func(view retainedArtifactTransferView) error {
			escaped = view
			got, err := view.GetExact(t.Context(), slot.ref)
			if err != nil || !bytes.Equal(got, raw) {
				return errors.New("selected native exact bytes were unavailable")
			}
			if got, err := view.GetExact(t.Context(), artifactReference([]byte("foreign digest"))); err == nil || got != nil {
				return errors.New("view read a foreign digest")
			}
			if view.PublishExact(t.Context(), slot.ref, raw) == nil {
				return errors.New("read view published")
			}
			if err := sources.withExactSource(slot, func(retainedArtifactTransferView) error { return nil }); !errors.Is(err, errRetainedArtifactBusy) {
				return errors.New("callback reentered host admission under a queue")
			}
			copy := *view.(*retainedArtifactNativeTransferView)
			if copy.Close() == nil {
				return errors.New("copied view closed the original queue")
			}
			return nil
		})
	})
	assistantRunMust(t, err)
	if got, err := escaped.GetExact(t.Context(), run.DialectDrafts[0].Candidate.Intent.Reference); err == nil || got != nil {
		t.Fatal("escaped view outlived source custody")
	}
	root, err := s.openRetainedArtifacts(false)
	assistantRunMust(t, err)
	defer root.Close()
	queue := injectedArtifactQueue(t, root)
	defer queue.Close()
	err = s.withSelectedDialectArtifactSources(request, witness, func(sources *dialectArtifactSelectedSources) error {
		return sources.withExactSource(sources.initial.slots[0], func(retainedArtifactTransferView) error { return nil })
	})
	if !errors.Is(err, errRetainedArtifactBusy) {
		t.Fatal("actual native queue was not try-only", err)
	}
}

func TestDialectArtifactSelectedTransferUnknownBeforeFirstPut(t *testing.T) {
	for _, acknowledged := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "acknowledged"}[acknowledged], func(t *testing.T) {
			s, request := dialectArtifactOwnerSetup(t)
			enrolled, err := s.enrollDialectArtifactOwner(request)
			assistantRunMust(t, err)
			run, _ := selectedTransferRunFixture(t, enrolled, acknowledged)
			selectedTransferSaveRun(t, s, run)
			witness := selectedTransferCurrentWitness(t, s)
			err = s.withSelectedDialectArtifactSources(request, witness, func(sources *dialectArtifactSelectedSources) error {
				return sources.withExactSource(sources.initial.slots[0], func(view retainedArtifactTransferView) error {
					got, err := view.GetExact(t.Context(), sources.initial.slots[0].ref)
					if got != nil {
						return errors.New("absent unknown body synthesized")
					}
					return err
				})
			})
			if acknowledged && injectedArtifactOnlyNotExist(err) {
				t.Fatal("acknowledged missing payload became exportable absence", err)
			}
			if !acknowledged && !injectedArtifactOnlyNotExist(err) {
				t.Fatal("unknown before first Put lost authenticated absence", err)
			}
			if _, err := os.Lstat(filepath.Join(s.projectDir, ".desk-private", "retained-artifacts")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("source read created an artifact directory", err)
			}
		})
	}
}

func TestDialectArtifactSelectedTransferImportedMissingBeatsAmbientHash(t *testing.T) {
	s, request := dialectArtifactOwnerSetup(t)
	enrolled, err := s.enrollDialectArtifactOwner(request)
	assistantRunMust(t, err)
	run, raw := selectedTransferRunFixture(t, enrolled, false)
	run.Historical, run.State = true, "interrupted"
	run.DialectArtifactImport = &assistantDialectArtifactImport{Version: 1, Slots: []assistantDialectArtifactImportSlot{{HostOperationID: run.DialectDrafts[0].HostOperationID, Stage: "candidate", Availability: "missing"}}}
	// The destination producer is a later integration. Install the exact
	// handwritten closed scope here solely as source-reader coverage; no native
	// scope is manufactured for this imported namespace or used as fallback.
	scope := dialectArtifactOwnerScope{Version: 1, Kind: "import", Owner: enrolled.Owner, Namespace: enrolled.Namespace, RunID: run.ID}
	path := dialectArtifactOwnerTablePath(s, "imports", scope.Namespace, scope.RunID)
	assistantRunMust(t, os.MkdirAll(path, 0700))
	assistantRunMust(t, os.WriteFile(filepath.Join(path, scope.Owner+".json"), assistantChatJSON(scope), 0600))
	selectedTransferSaveRun(t, s, run)
	_, err = retainedArtifactsForProject(s.root).Put(t.Context(), raw)
	assistantRunMust(t, err)
	witness := selectedTransferCurrentWitness(t, s)
	err = s.withSelectedDialectArtifactSources(request, witness, func(sources *dialectArtifactSelectedSources) error {
		slot := sources.initial.slots[0]
		if !slot.missing || slot.scope.Kind != "import" {
			return errors.New("import provenance was discarded")
		}
		return sources.withExactSource(slot, func(view retainedArtifactTransferView) error {
			got, err := view.GetExact(t.Context(), slot.ref)
			if got != nil {
				return errors.New("ambient hash filled imported missing slot")
			}
			return err
		})
	})
	if !injectedArtifactOnlyNotExist(err) {
		t.Fatal("exact imported missing distinction was lost", err)
	}
}

func TestDialectArtifactSelectedTransferCurrentWitnessAlreadyLockedRefuses(t *testing.T) {
	s, request := dialectArtifactOwnerSetup(t)
	outer, err := s.privateDataFileLock(".data-work.lock", true)
	assistantRunMust(t, err)
	defer outer.Close()
	called := false
	err = s.withSelectedDialectArtifactSources(request, outer, func(*dialectArtifactSelectedSources) error { called = true; return nil })
	if !errors.Is(err, errRetainedArtifactBusy) || called {
		t.Fatal("already-flocked witness was mixed with selected-owned mode", err)
	}
	probe, err := s.assistant.root.OpenFile(".data-work.lock", os.O_RDWR, 0)
	assistantRunMust(t, err)
	defer probe.Close()
	if err := lockPrivateData(probe, true); !lockHeld(err) {
		t.Fatal("failed selected admission released the outer flock", err)
	}
}

func TestDialectArtifactSelectedTransferForeignInjectedRegistration(t *testing.T) {
	viewer, request := dialectArtifactOwnerSetup(t)
	p := selectedProviderFixture(t, strings.Repeat("a", 64))
	q := selectedProviderFixture(t, strings.Repeat("b", 64))
	registry, err := newRetainedArtifactProviderRegistry(p, q)
	assistantRunMust(t, err)
	injectedArtifactAttach(t, viewer, p, registry)
	foreign, foreignRequest := dialectArtifactOwnerSetup(t)
	foreign.cfg.parent = viewer
	injectedArtifactAttach(t, foreign, q, registry)
	enrolled, err := foreign.enrollDialectArtifactOwner(foreignRequest)
	assistantRunMust(t, err)
	run, raw := selectedTransferRunFixture(t, enrolled, false)
	injectedArtifactPublish(t, foreign, foreignRequest, raw)
	// The selected whole-history file is exact validated original bytes; its
	// original provider/source provenance still comes from real enrollment.
	selectedTransferSaveRun(t, viewer, run)
	witness := selectedTransferCurrentWitness(t, viewer)
	err = viewer.withSelectedDialectArtifactSources(request, witness, func(sources *dialectArtifactSelectedSources) error {
		slot := sources.initial.slots[0]
		if sources.scopes[slot.scope].injected.registration != registry.byID[q.id] {
			return errors.New("ambient provider substituted selected provider")
		}
		return sources.withExactSource(slot, func(view retainedArtifactTransferView) error {
			got, err := view.GetExact(t.Context(), slot.ref)
			if err != nil || !bytes.Equal(got, raw) {
				return errors.New("selected provider exact bytes unavailable")
			}
			guard := sources.authority
			copy := *guard
			if copy.Close() == nil {
				return errors.New("copied selected guard ended original auth pins")
			}
			if guard.Check(t.Context()) != nil {
				return errors.New("copied guard disturbed original auth pins")
			}
			return nil
		})
	})
	assistantRunMust(t, err)
	// Neither exported history nor read-only source acquisition rewrites the
	// original owner string, receipts or unknown/reconciled state.
	data, err := viewer.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	saved, err := readPrivateData(data.root, "assistant-runs-"+run.Binding.Project+".json", assistantRunRecordLimit)
	assistantRunMust(t, err)
	var ledger assistantRunLedger
	assistantRunMust(t, json.Unmarshal(saved, &ledger))
	if ledger.Runs[0].DialectArtifactOwner != enrolled.Owner || ledger.Runs[0].DialectDrafts[0].Candidate.Receipt != nil || ledger.Runs[0].DialectDrafts[0].Reconciled {
		t.Fatal("selected read reconciled historical custody")
	}
}

func TestDialectArtifactSelectedHistoricalUsesRetainedLookupScope(t *testing.T) {
	s, request := dialectArtifactOwnerSetup(t)
	enrolled, err := s.enrollDialectArtifactOwner(request)
	assistantRunMust(t, err)
	run, raw := selectedTransferRunFixture(t, enrolled, false)
	identity, _, err := s.assistantRunIdentity(request)
	assistantRunMust(t, err)
	run.Binding.Principal = identity.Principal
	run.Historical, run.State = true, "interrupted"
	selectedTransferSaveRun(t, s, run)
	_, err = retainedArtifactsForProject(s.root).Put(t.Context(), raw)
	assistantRunMust(t, err)
	if s.authorizeAssistantRun(request, run) == nil {
		t.Fatal("fixture retained the current original-session execution scope")
	}
	witness := selectedTransferCurrentWitness(t, s)
	called := false
	err = s.withSelectedDialectArtifactRunSources(request, witness, run.ID, run.DialectDrafts[0].HostOperationID, "candidate", func(sources *dialectArtifactSelectedSources) error {
		called = true
		if len(sources.initial.files) != 1 || len(sources.initial.slots) != 1 {
			return errors.New("historical inspection selected unrelated files or roots")
		}
		return sources.withExactSource(sources.initial.slots[0], func(view retainedArtifactTransferView) error {
			got, err := view.GetExact(t.Context(), sources.initial.slots[0].ref)
			if err != nil || !bytes.Equal(got, raw) {
				return errors.New("historical source required original live session/factory")
			}
			return nil
		})
	})
	assistantRunMust(t, err)
	if !called {
		t.Fatal("authorized historical lookup did not reach its exact source")
	}
	for _, mutate := range []func(*assistantRunRecord){
		func(v *assistantRunRecord) { v.Binding.Principal = strings.Repeat("f", 64) },
		func(v *assistantRunRecord) { v.Historical = false; v.State = "running" },
	} {
		changed := run
		mutate(&changed)
		selectedTransferSaveRun(t, s, changed)
		called = false
		err = s.withSelectedDialectArtifactRunSources(request, witness, changed.ID, changed.DialectDrafts[0].HostOperationID, "candidate", func(*dialectArtifactSelectedSources) error { called = true; return nil })
		if err == nil || called {
			t.Fatal("historical lookup bypassed retained principal/state scope", err)
		}
	}
}

func TestDialectArtifactSelectedHistoricalDoesNotDiscoverOtherRunOwners(t *testing.T) {
	s, request := dialectArtifactOwnerSetup(t)
	enrolled, err := s.enrollDialectArtifactOwner(request)
	assistantRunMust(t, err)
	run, raw := selectedTransferRunFixture(t, enrolled, false)
	identity, _, err := s.assistantRunIdentity(request)
	assistantRunMust(t, err)
	run.Binding.Principal = identity.Principal
	run.Historical, run.State = true, "interrupted"
	other := cloneDialectArtifactImportedRun(t, run)
	other.ID = "arun_" + strings.Repeat("e", 32)
	other.AdmissionID = "other-admission"
	other.DialectArtifactOwner = strings.Repeat("f", 64)
	other.DialectDrafts[0].Candidate.Intent.Receipt = assistantDialectDraftReceipt(other, other.DialectDrafts[0], "candidate", *other.DialectDrafts[0].Candidate.Intent)
	ledger := assistantRunLedger{Version: 1, Project: enrolled.Namespace, Runs: []assistantRunRecord{run, other}}
	name := "assistant-runs-" + enrolled.Namespace + ".json"
	file, _, err := prepareAssistantRunStorage(name, ledger)
	assistantRunMust(t, err)
	data, err := s.openChatData()
	assistantRunMust(t, err)
	assistantRunMust(t, writePrivateData(data.root, name, file))
	assistantRunMust(t, data.root.Close())
	_, err = retainedArtifactsForProject(s.root).Put(t.Context(), raw)
	assistantRunMust(t, err)
	witness := selectedTransferCurrentWitness(t, s)
	err = s.withSelectedDialectArtifactRunSources(request, witness, run.ID, run.DialectDrafts[0].HostOperationID, "candidate", func(sources *dialectArtifactSelectedSources) error {
		if len(sources.initial.slots) != 1 || sources.initial.slots[0].scope.Owner != enrolled.Owner {
			return errors.New("unrelated retained owner selected")
		}
		return sources.withExactSource(sources.initial.slots[0], func(view retainedArtifactTransferView) error {
			_, err := view.GetExact(t.Context(), sources.initial.slots[0].ref)
			return err
		})
	})
	assistantRunMust(t, err)
	if err := s.withSelectedDialectArtifactSources(request, witness, func(*dialectArtifactSelectedSources) error { return nil }); err == nil {
		t.Fatal("whole-store discovery ignored an unavailable selected owner")
	}
}

func TestDialectArtifactSelectedTransferMissingHeavyDeclaredBytesDoNotNarrowArchive(t *testing.T) {
	s, request := dialectArtifactOwnerSetup(t)
	p, registry, _, _ := injectedArtifactProviderFixture(t)
	injectedArtifactAttach(t, s, p, registry)
	enrolled, err := s.enrollDialectArtifactOwner(request)
	assistantRunMust(t, err)
	ledger := assistantRunLedger{Version: 1, Project: enrolled.Namespace, Runs: []assistantRunRecord{}}
	for i := 1; i <= 17; i++ {
		run, _ := selectedTransferRunFixture(t, enrolled, false)
		run.ID = fmt.Sprintf("arun_%032x", i)
		run.AdmissionID = fmt.Sprintf("missing-%d", i)
		run.State = "interrupted"
		held := &run.DialectDrafts[0]
		held.Candidate.Intent.Reference = retainedArtifactRef{SHA256: fmt.Sprintf("%064x", i), Bytes: 8 << 20}
		held.Candidate.Intent.Receipt = assistantDialectDraftReceipt(run, *held, "candidate", *held.Candidate.Intent)
		ledger.Runs = append(ledger.Runs, run)
	}
	name := "assistant-runs-" + enrolled.Namespace + ".json"
	raw, _, err := prepareAssistantRunStorage(name, ledger)
	assistantRunMust(t, err)
	data, err := s.openChatData()
	assistantRunMust(t, err)
	assistantRunMust(t, writePrivateData(data.root, name, raw))
	assistantRunMust(t, data.root.Close())
	witness := selectedTransferCurrentWitness(t, s)
	missing, total := 0, 0
	err = s.withSelectedDialectArtifactSources(request, witness, func(sources *dialectArtifactSelectedSources) error {
		for _, slot := range sources.initial.slots {
			total += slot.ref.Bytes
			err := sources.withExactSource(slot, func(view retainedArtifactTransferView) error {
				body, err := view.GetExact(t.Context(), slot.ref)
				if body != nil {
					return errors.New("unknown missing slot acquired a fabricated body")
				}
				return err
			})
			if !injectedArtifactOnlyNotExist(err) {
				return err
			}
			missing++
		}
		return nil
	})
	assistantRunMust(t, err)
	if missing != 17 || total <= maxBackupData {
		t.Fatal("missing-heavy witness did not exceed the declared-size sum", missing, total)
	}
}
