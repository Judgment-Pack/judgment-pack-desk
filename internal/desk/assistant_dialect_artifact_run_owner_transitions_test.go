//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func assistantDialectOwnerExpected(t *testing.T, f *assistantDialectAuthorityFixture) assistantDialectDraftIntent {
	t.Helper()
	cp, _, err := readAssistantDialectFactoryDraft(f.raw)
	assistantRunMust(t, err)
	i := assistantDialectDraftIntent{Reference: artifactReference(f.raw), PackageDigest: cp.Digest}
	i.Receipt = assistantDialectDraftReceipt(assistantExecutionRecord(t, f.execution), f.seed, "candidate", i)
	return i
}
func assistantDialectOwnerAssign(f *assistantDialectAuthorityFixture, run *assistantRunRecord, intent assistantDialectDraftIntent) {
	held := f.seed
	held.Candidate.Intent = &intent
	run.DialectDrafts = []assistantDialectDraftCustody{held}
	run.DialectArtifactOwner = f.owner.enrollment.Owner
}

func TestDialectArtifactRunOwnerAtomicFirstIntent(t *testing.T) {
	for _, mode := range []string{"ordinary", "result", "owner-only", "intent-only", "new-preowned"} {
		t.Run(mode, func(t *testing.T) {
			f := assistantDialectAuthoritySetup(t)
			store := f.owner.lease.store
			before := assistantRunStorageValidationRead(t, store)
			intent := assistantDialectOwnerExpected(t, f)
			mutate := func(run *assistantRunRecord) (bool, error) {
				assistantDialectOwnerAssign(f, run, intent)
				if mode == "owner-only" {
					run.DialectDrafts = nil
				}
				if mode == "intent-only" {
					run.DialectArtifactOwner = ""
				}
				return true, nil
			}
			var err error
			if mode == "result" {
				err = f.execution.writer.change(f.ctx, true, mutate)
			} else {
				_, err = store.transaction(f.ctx, func(ledger *assistantRunLedger) (bool, error) {
					if mode == "new-preowned" {
						copy := ledger.Runs[0]
						copy.ID = "arun_" + strings.Repeat("d", 32)
						copy.AdmissionID = "another-admission"
						copy.DialectArtifactOwner = f.owner.enrollment.Owner
						ledger.Runs = append(ledger.Runs, copy)
						return true, nil
					}
					return mutate(&ledger.Runs[0])
				})
			}
			if err == nil || !bytes.Equal(before, assistantRunStorageValidationRead(t, store)) || len(f.owner.reservations) != 0 {
				t.Fatal("ordinary mutation admitted provenance", err)
			}
		})
	}
	for _, mode := range []string{"false", "error", "checked-first", "wrong-owner"} {
		t.Run(mode, func(t *testing.T) {
			f := assistantDialectAuthoritySetup(t)
			store := f.owner.lease.store
			before := assistantRunStorageValidationRead(t, store)
			intent := assistantDialectOwnerExpected(t, f)
			stage := "candidate"
			if mode == "checked-first" {
				stage = "checked"
			}
			err := f.execution.writer.changeDialectDraft(f.ctx, f.owner, stage, intent, func(run *assistantRunRecord) (bool, error) {
				assistantDialectOwnerAssign(f, run, intent)
				if mode == "wrong-owner" {
					run.DialectArtifactOwner = strings.Repeat("f", 64)
				}
				if mode == "false" {
					return false, nil
				}
				if mode == "error" {
					return false, errors.New("fixture callback failed")
				}
				return true, nil
			})
			if (mode == "false") != (err == nil) {
				t.Fatal("wrong callback disposition", err)
			}
			if !bytes.Equal(before, assistantRunStorageValidationRead(t, store)) || len(f.owner.reservations) != 0 {
				t.Fatal("refused callback leaked owner/intent/arm")
			}
			assistantDialectAssertReleased(t, f)
		})
	}
}

func TestDialectArtifactRunOwnerReservationCustody(t *testing.T) {
	t.Run("held-through-atomic-write", func(t *testing.T) {
		f := assistantDialectAuthoritySetup(t)
		intent := assistantDialectOwnerExpected(t, f)
		callbacks := 0
		assistantRunMust(t, f.execution.writer.changeDialectDraft(f.ctx, f.owner, "candidate", intent, func(run *assistantRunRecord) (bool, error) {
			callbacks++
			for name, mu := range map[string]*sync.Mutex{"writes": f.server.writes, "canonical": &f.execution.canonicalMu, "writer": &f.owner.writer.mu, "lease": &f.owner.lease.mu} {
				if mu.TryLock() {
					mu.Unlock()
					t.Fatalf("%s custody missing", name)
				}
			}
			lock, err := f.server.tryPrivateDataLockContext(f.ctx, true)
			if err == nil {
				lock.Close()
				t.Fatal("private-data custody missing")
			}
			// Reservations do not add a preparation view or invoke a provider.
			view, err := f.server.preparationRepository().TryRead(f.ctx)
			assistantRunMust(t, err)
			assistantRunMust(t, view.Close())
			assistantDialectOwnerAssign(f, run, intent)
			return true, nil
		}))
		if callbacks != 1 || len(f.owner.reservations) != 0 {
			t.Fatal("callback repeated or effect armed before readback")
		}
		run := assistantExecutionRecord(t, f.execution)
		if run.DialectArtifactOwner != f.owner.enrollment.Owner || run.DialectDrafts[0].Candidate.Receipt != nil {
			t.Fatal("atomic saved head differs")
		}
		assistantDialectAssertReleased(t, f)
	})
	for _, name := range []string{"writes", "canonical", "writer", "lease", "private"} {
		t.Run("contended-"+name, func(t *testing.T) {
			f := assistantDialectAuthoritySetup(t)
			intent := assistantDialectOwnerExpected(t, f)
			before := assistantRunStorageValidationRead(t, f.owner.lease.store)
			var release func()
			if name == "private" {
				lock, err := f.server.tryPrivateDataLockContext(f.ctx, true)
				assistantRunMust(t, err)
				release = func() { assistantRunMust(t, lock.Close()) }
			} else {
				mu := map[string]*sync.Mutex{"writes": f.server.writes, "canonical": &f.execution.canonicalMu, "writer": &f.owner.writer.mu, "lease": &f.owner.lease.mu}[name]
				mu.Lock()
				release = mu.Unlock
			}
			called := false
			err := f.execution.writer.changeDialectDraft(f.ctx, f.owner, "candidate", intent, func(*assistantRunRecord) (bool, error) { called = true; return true, nil })
			release()
			if err == nil || called || f.owner.writer.Err() != nil || !bytes.Equal(before, assistantRunStorageValidationRead(t, f.owner.lease.store)) {
				t.Fatal("contention admitted mutation or latched unowned failure", err)
			}
			assistantDialectAssertReleased(t, f)
		})
	}
}

func TestDialectArtifactRunOwnerImmutableAndBounds(t *testing.T) {
	for _, mode := range []string{"clear", "replace", "drop"} {
		t.Run(mode, func(t *testing.T) {
			f := assistantDialectAuthoritySetup(t)
			f.reserve(t, false)
			store := f.owner.lease.store
			before := assistantRunStorageValidationRead(t, store)
			_, err := store.transaction(f.ctx, func(ledger *assistantRunLedger) (bool, error) {
				switch mode {
				case "clear":
					ledger.Runs[0].DialectArtifactOwner = ""
				case "replace":
					ledger.Runs[0].DialectArtifactOwner = strings.Repeat("e", 64)
				case "drop":
					ledger.Runs = nil
				}
				return true, nil
			})
			if err == nil || !bytes.Equal(before, assistantRunStorageValidationRead(t, store)) {
				t.Fatal("owned run provenance mutated", err)
			}
		})
	}
	t.Run("physical-work-replacement", func(t *testing.T) {
		f := assistantDialectAuthoritySetup(t)
		before := assistantRunStorageValidationRead(t, f.owner.lease.store)
		path := filepath.Join(f.server.configDir, ".data-work.lock")
		original := path + ".fixture-original"
		assistantRunMust(t, os.Rename(path, original))
		defer func() { assistantRunMust(t, os.Rename(original, path)) }()
		assistantRunMust(t, os.WriteFile(path, nil, 0600))
		if _, err := f.store.retainDraft(f.ctx, f.raw); err == nil {
			t.Fatal("replacement work entry accepted")
		}
		if !bytes.Equal(before, assistantRunStorageValidationRead(t, f.owner.lease.store)) || len(f.owner.reservations) != 0 {
			t.Fatal("physical replacement persisted provenance")
		}
	})
}

// Mutates only while the existing dynamic citation marshaler runs. Final owner
// scalars (including legacy omission) are valid to the typed decoder, so the
// independent central transition check must reject changed saved provenance.
// Dropping the outer ledger slice only changes its header; preparation already
// holds a value copy and must still persist the original run and owner.
type assistantDialectOwnerMarshalMutation struct {
	run    *assistantRunRecord
	ledger *assistantRunLedger
	mode   string
	calls  *int
}

func (m assistantDialectOwnerMarshalMutation) MarshalJSON() ([]byte, error) {
	(*m.calls)++
	switch m.mode {
	case "clear":
		m.run.DialectArtifactOwner = ""
	case "replace", "assign":
		m.run.DialectArtifactOwner = strings.Repeat("e", 64)
	case "drop":
		m.ledger.Runs = nil
	}
	m.run.Creation.Citations[0].ExcerptID = nil
	return []byte("null"), nil
}

func TestDialectArtifactRunOwnerPreparedOutput(t *testing.T) {
	for _, mode := range []string{"clear", "replace", "assign", "drop"} {
		t.Run(mode, func(t *testing.T) {
			store, ledger := assistantRunStorageValidationCreation(t)
			run := &ledger.Runs[0]
			template, _, _ := assistantDialectFactoryFixtureRun(t)
			run.Operations, run.Events = template.Operations, template.Events
			var root assistantHostIntent
			assistantRunMust(t, readAssistantHostReceipt(run.Operations[1].Intent, &root))
			root.DialectAction.ID = assistantDialectActionID(run.Binding, root)
			root.DialectAction.AfterTurnID = run.Binding.RequestID
			run.Operations[1].Intent = assistantChatJSON(root)
			held := template.DialectDrafts[0]
			held.ActionID = root.DialectAction.ID
			i := *held.Candidate.Intent
			i.Receipt = assistantDialectDraftReceipt(*run, held, "candidate", i)
			held.Candidate.Intent = &i
			run.DialectDrafts = []assistantDialectDraftCustody{held}
			if mode != "assign" {
				run.DialectArtifactOwner = strings.Repeat("a", 64)
			}
			baseline, _, err := prepareAssistantRunStorage(store.name, ledger)
			assistantRunMust(t, err)
			// Install the detached handwritten legacy/owned parsing fixture;
			// an ordinary live mutation cannot mint its initial owner.
			data, err := store.server.openChatData()
			assistantRunMust(t, err)
			assistantRunMust(t, writePrivateData(data.root, store.name, baseline))
			assistantRunMust(t, data.root.Close())
			calls := 0
			var callbackLedger *assistantRunLedger
			saved, err := store.transaction(t.Context(), func(current *assistantRunLedger) (bool, error) {
				callbackLedger = current
				r := &current.Runs[0]
				r.Creation.Citations[0].ExcerptID = assistantDialectOwnerMarshalMutation{run: r, ledger: current, mode: mode, calls: &calls}
				return true, nil
			})
			if calls != 1 || !bytes.Equal(baseline, assistantRunStorageValidationRead(t, store)) {
				t.Fatal("prepared output changed provenance", err, calls)
			}
			if mode == "drop" {
				// A slice-header mutation after preparation's value copy does
				// not drop a saved run. Real callback drops are refused by
				// TestDialectArtifactRunOwnerImmutableAndBounds/drop.
				if err != nil || callbackLedger == nil || callbackLedger.Runs != nil || len(saved.Runs) != 1 || saved.Runs[0].ID != run.ID || saved.Runs[0].DialectArtifactOwner != run.DialectArtifactOwner {
					t.Fatal("outer slice drop changed the persisted run or owner", err)
				}
			} else if err == nil {
				t.Fatal("prepared output admitted changed provenance")
			}
		})
	}
	t.Run("original-owner-capture-changes-during-prepare", func(t *testing.T) {
		f := assistantDialectAuthoritySetup(t)
		intent := assistantDialectOwnerExpected(t, f)
		before := assistantRunStorageValidationRead(t, f.owner.lease.store)
		original := f.owner.enrollment
		testHookAssistantRunOutputValidation = func(bool) { f.owner.enrollment.Owner = strings.Repeat("e", 64) }
		defer func() { testHookAssistantRunOutputValidation = nil; f.owner.enrollment = original }()
		err := f.execution.writer.changeDialectDraft(f.ctx, f.owner, "candidate", intent, func(run *assistantRunRecord) (bool, error) {
			assistantDialectOwnerAssign(f, run, intent)
			return true, nil
		})
		testHookAssistantRunOutputValidation = nil
		f.owner.enrollment = original
		if err == nil || !bytes.Equal(before, assistantRunStorageValidationRead(t, f.owner.lease.store)) || len(f.owner.reservations) != 0 {
			t.Fatal("mutable capture redefined admission", err)
		}
		assistantDialectAssertReleased(t, f)
	})
}

func TestDialectArtifactRunOwnerReservationUncertainty(t *testing.T) {
	for _, mode := range []string{"cancel", "readback", "data-close", "final-check"} {
		t.Run(mode, func(t *testing.T) {
			f := assistantDialectAuthoritySetup(t)
			intent := assistantDialectOwnerExpected(t, f)
			before := assistantRunStorageValidationRead(t, f.owner.lease.store)
			failure := errors.New("handwritten reservation uncertainty")
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			defer func() {
				testHookPrivateRead = nil
				testHookDialectArtifactOwnerStep = nil
				testHookAssistantRunOutputValidation = nil
			}()
			err := f.execution.writer.changeDialectDraft(ctx, f.owner, "candidate", intent, func(run *assistantRunRecord) (bool, error) {
				assistantDialectOwnerAssign(f, run, intent)
				switch mode {
				case "cancel":
					cancel()
				case "readback":
					testHookPrivateRead = func(name string) error {
						if name == f.owner.lease.store.name {
							return failure
						}
						return nil
					}
				case "data-close":
					testHookDialectArtifactOwnerStep = func(step string) error {
						if step == "factory-data.close" {
							return failure
						}
						return nil
					}
				case "final-check":
					// Trigger after the known successful readback, before final
					// cleanup authorization. The file is still one complete head.
					testHookPrivateRead = func(name string) error {
						if name == f.owner.lease.store.name {
							cancel()
						}
						return nil
					}
				}
				return true, nil
			})
			testHookPrivateRead, testHookDialectArtifactOwnerStep = nil, nil
			if err == nil || len(f.owner.reservations) != 0 {
				t.Fatal("uncertain reservation armed raw effect", err)
			}
			saved := assistantRunStorageValidationRead(t, f.owner.lease.store)
			if mode == "cancel" {
				if !bytes.Equal(before, saved) {
					t.Fatal("cancelled pre-write changed baseline")
				}
			} else {
				var ledger assistantRunLedger
				assistantRunMust(t, decodeDataJSON(saved, &ledger))
				run := ledger.Runs[0]
				if run.DialectArtifactOwner != f.owner.enrollment.Owner || run.DialectDrafts[0].Candidate.Intent == nil || run.DialectDrafts[0].Candidate.Receipt != nil {
					t.Fatal("uncertainty left an incomplete owner/intent head")
				}
			}
			if _, err := f.owner.putDialectDraft(f.ctx, artifactReference(f.raw), f.raw); err == nil {
				t.Fatal("unarmed uncertainty reached raw Put")
			}
			assistantDialectAssertReleased(t, f)
		})
	}
}
