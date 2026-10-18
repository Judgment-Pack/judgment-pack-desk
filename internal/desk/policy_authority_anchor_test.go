package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type policyAnchorFixture struct {
	anchor   *policyAuthorityAnchor
	ledger   *os.Root
	identity policyAuthorityAnchorIdentity
	original []byte
}

func newPolicyAnchorFixture(t *testing.T, enroll bool) *policyAnchorFixture {
	t.Helper()
	identity := policyAuthorityAnchorIdentity{InstallationID: "installation-123", WorkspaceID: "workspace-456"}
	anchor, err := newPolicyAuthorityAnchor(filepath.Join(t.TempDir(), "machine-authority"), identity)
	if err != nil {
		t.Fatal(err)
	}
	ledgerDirectory := t.TempDir()
	// Only this fixture's synthetic store is narrowed; production custody stays strict.
	if err := os.Chmod(ledgerDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	ledger, err := openPrivateDataRoot(ledgerDirectory, false)
	if err != nil {
		t.Fatal("fixture custody", err)
	}
	t.Cleanup(func() { ledger.Close() })
	f := &policyAnchorFixture{anchor: anchor, ledger: ledger, identity: identity, original: []byte("{\"revision\":1}\n")}
	if err := writePrivateData(ledger, "ledger.json", f.original); err != nil {
		t.Fatal(err)
	}
	if enroll {
		if err := f.withLedger(t, func(raw []byte) error { return anchor.Enroll(context.Background(), raw) }); err != nil {
			t.Fatal(err)
		}
	}
	return f
}
func (f *policyAnchorFixture) withLedger(t *testing.T, use func([]byte) error) error {
	t.Helper()
	lock, err := f.ledger.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = lockPrivateData(lock, true); err != nil {
		t.Fatal(err)
	}
	raw, err := readPrivateData(f.ledger, "ledger.json", policyLedgerLimit)
	if err != nil {
		t.Fatal(err)
	}
	return use(raw)
}
func policyAnchorRequireCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *policyAuthorityAnchorError
	if !errors.As(err, &failure) || failure.Code != "policy-authority-anchor-"+code {
		t.Fatalf("want %s, got %v", code, err)
	}
}
func (f *policyAnchorFixture) state(t *testing.T) policyAuthorityAnchorState {
	t.Helper()
	root, err := openPrivateDataRoot(f.anchor.directory, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	state, err := f.anchor.read(root)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
func (f *policyAnchorFixture) replaceState(t *testing.T, raw []byte) {
	t.Helper()
	root, err := openPrivateDataRoot(f.anchor.directory, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = writePrivateData(root, policyAuthorityAnchorName, raw); err != nil {
		t.Fatal(err)
	}
}
func TestPolicyAuthorityAnchorOrderedAdvanceNoopAndNoInputMutation(t *testing.T) {
	f := newPolicyAnchorFixture(t, true)
	writes := 0
	next := []byte("{\"revision\":2,\"rawNumber\":9007199254740993}\n")
	expected := bytes.Clone(next)
	var escaped *policyAuthorityAnchorHeld
	err := f.withLedger(t, func(raw []byte) error {
		original := bytes.Clone(raw)
		return f.anchor.WithCurrent(context.Background(), raw, func(held *policyAuthorityAnchorHeld) error {
			escaped = held
			before, err := held.Current()
			if err != nil || before.Generation != 1 || before.Classification != "current" {
				t.Fatal(before, err)
			}
			err = held.Advance(context.Background(), next, func(delivered []byte) error {
				writes++
				if !bytes.Equal(delivered, expected) {
					t.Fatal("changed ledger bytes")
				}
				// The callback can observe only the already-durable intent.
				state := f.state(t)
				if state.Intent == nil || state.Generation != 1 || state.Intent.NewDigest != digestOf(expected) {
					t.Fatal(state)
				}
				return writePrivateData(f.ledger, "ledger.json", delivered)
			})
			if !bytes.Equal(raw, original) {
				t.Fatal("current ledger input mutated")
			}
			return err
		})
	})
	if err != nil || writes != 1 || !bytes.Equal(next, expected) {
		t.Fatal(err, writes)
	}
	state := f.state(t)
	if state.Generation != 2 || state.Intent != nil || state.LedgerDigest != digestOf(next) {
		t.Fatal(state)
	}
	_, err = escaped.Current()
	policyAnchorRequireCode(t, err, "not-held")
	err = escaped.Advance(context.Background(), []byte("escaped"), func([]byte) error { t.Fatal("escaped callback invoked"); return nil })
	policyAnchorRequireCode(t, err, "not-held")
	err = f.withLedger(t, func(raw []byte) error {
		return f.anchor.WithCurrent(context.Background(), raw, func(held *policyAuthorityAnchorHeld) error {
			return held.Advance(context.Background(), raw, func([]byte) error { t.Fatal("same digest wrote ledger"); return nil })
		})
	})
	if err != nil || f.state(t).Generation != 2 {
		t.Fatal("no-op changed high water", err)
	}
	third := []byte("{\"revision\":3}\n")
	err = f.withLedger(t, func(raw []byte) error {
		return f.anchor.WithCurrent(context.Background(), raw, func(held *policyAuthorityAnchorHeld) error {
			return held.Advance(context.Background(), third, func(exact []byte) error { writes++; return writePrivateData(f.ledger, "ledger.json", exact) })
		})
	})
	if err != nil || writes != 2 || f.state(t).Generation != 3 {
		t.Fatal(err, writes)
	}
	// Restoring only the portable ledger cannot move the external high water back.
	if err := writePrivateData(f.ledger, "ledger.json", f.original); err != nil {
		t.Fatal(err)
	}
	err = f.withLedger(t, func(raw []byte) error {
		return f.anchor.WithCurrent(context.Background(), raw, func(*policyAuthorityAnchorHeld) error { t.Fatal("restored old ledger admitted"); return nil })
	})
	policyAnchorRequireCode(t, err, "mismatch")
	fresh, err := newPolicyAuthorityAnchor(f.anchor.directory, f.identity)
	if err != nil {
		t.Fatal(err)
	}
	policyAnchorRequireCode(t, fresh.Enroll(context.Background(), f.original), "exists")
	if f.state(t).Generation != 3 {
		t.Fatal("enrollment replay reset generation")
	}
}
func TestPolicyAuthorityAnchorPendingAndPersistenceEdges(t *testing.T) {
	cases := []struct {
		name          string
		phase         policyAuthorityAnchorWritePhase
		after         bool
		ledgerFailure string
		want          string
		callbacks     int
		persistCalls  int
	}{
		{"intent-before", policyAuthorityAnchorIntentWrite, false, "", "current", 0, 1},
		{"intent-after", policyAuthorityAnchorIntentWrite, true, "", "pending-old", 0, 1},
		{"ledger-before", "", false, "before", "pending-old", 1, 1},
		{"ledger-after", "", false, "after", "pending-new", 1, 1},
		{"final-before", policyAuthorityAnchorFinalWrite, false, "", "pending-new", 1, 2},
		{"final-after", policyAuthorityAnchorFinalWrite, true, "", "current", 1, 2},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			f := newPolicyAnchorFixture(t, true)
			next := []byte("{\"revision\":2}\n")
			writes, persists := 0, 0
			f.anchor.persistForTest = func(phase policyAuthorityAnchorWritePhase, root *os.Root, raw []byte) error {
				persists++
				if phase == tt.phase && !tt.after {
					return errors.New("secret path and content must not escape")
				}
				if err := writePrivateData(root, policyAuthorityAnchorName, raw); err != nil {
					return err
				}
				if phase == tt.phase && tt.after {
					return errors.New("uncertain after rename/fsync/read-back")
				}
				return nil
			}
			err := f.withLedger(t, func(raw []byte) error {
				return f.anchor.WithCurrent(context.Background(), raw, func(held *policyAuthorityAnchorHeld) error {
					// Ignoring the Advance error must not produce a successful outer call.
					_ = held.Advance(context.Background(), next, func(exact []byte) error {
						writes++
						if tt.ledgerFailure == "before" {
							return errors.New("ledger failed before writing")
						}
						if err := writePrivateData(f.ledger, "ledger.json", exact); err != nil {
							return err
						}
						if tt.ledgerFailure == "after" {
							return errors.New("ledger result uncertain after writing")
						}
						return nil
					})
					return nil
				})
			})
			policyAnchorRequireCode(t, err, "uncertain")
			if strings.Contains(err.Error(), "secret") || writes != tt.callbacks || persists != tt.persistCalls {
				t.Fatal(err, writes, persists)
			}
			policyAnchorRequireCode(t, f.anchor.WithCurrent(context.Background(), next, func(*policyAuthorityAnchorHeld) error { t.Fatal("poisoned instance admitted"); return nil }), "uncertain")
			state := f.state(t)
			raw, err := readPrivateData(f.ledger, "ledger.json", policyLedgerLimit)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := classifyPolicyAuthorityAnchor(state, f.identity, digestOf(raw))
			if err != nil || observed.Classification != tt.want {
				t.Fatal(observed, err, state)
			}
			fresh, err := newPolicyAuthorityAnchor(f.anchor.directory, f.identity)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			err = fresh.WithCurrent(context.Background(), raw, func(held *policyAuthorityAnchorHeld) error { called = true; _, err := held.Current(); return err })
			if strings.HasPrefix(tt.want, "pending-") {
				policyAnchorRequireCode(t, err, "pending")
				var failure *policyAuthorityAnchorError
				_ = errors.As(err, &failure)
				if failure.Classification != tt.want || called {
					t.Fatal(failure, called)
				}
			} else if err != nil || !called {
				t.Fatal("stable matched head not observable", err)
			}
			if tt.name == "final-after" && state.Generation != 2 {
				t.Fatal("uncertain final write was rolled back", state)
			}
			if tt.name == "intent-before" && state.Generation != 1 {
				t.Fatal(state)
			}
			policyAnchorRequireCode(t, fresh.Enroll(context.Background(), f.original), "exists")
			if writes != tt.callbacks || persists != tt.persistCalls {
				t.Fatal("recovery replayed or repaired persistence")
			}
		})
	}
}
func TestPolicyAuthorityAnchorRefusesMissingCorruptIdentityPendingAndLock(t *testing.T) {
	for _, scenario := range []string{"missing", "corrupt", "identity", "digest", "pending-old", "pending-new", "pending-other", "locked"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPolicyAnchorFixture(t, scenario != "missing")
			observed := bytes.Clone(f.original)
			var lock *os.File
			var root *os.Root
			switch scenario {
			case "corrupt":
				f.replaceState(t, []byte(`{"version":1,"version":1}`))
			case "identity":
				var err error
				f.anchor, err = newPolicyAuthorityAnchor(f.anchor.directory, policyAuthorityAnchorIdentity{InstallationID: "other-installation", WorkspaceID: f.identity.WorkspaceID})
				if err != nil {
					t.Fatal(err)
				}
			case "digest":
				observed = []byte("restored older ledger")
			case "pending-old", "pending-new", "pending-other":
				state := f.state(t)
				state.Intent = &policyAuthorityAnchorIntent{Generation: state.Generation + 1, OldDigest: state.LedgerDigest, NewDigest: digestOf([]byte("new ledger")), Nonce: strings.Repeat("a", 48)}
				raw, _ := json.Marshal(state)
				f.replaceState(t, raw)
				if scenario == "pending-new" {
					observed = []byte("new ledger")
				}
				if scenario == "pending-other" {
					observed = []byte("other ledger")
				}
			case "locked":
				var err error
				root, err = openPrivateDataRoot(f.anchor.directory, false)
				if err != nil {
					t.Fatal(err)
				}
				defer root.Close()
				lock, err = root.Open(".")
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
				if err = lockPrivateData(lock, true); err != nil {
					t.Fatal(err)
				}
			}
			callbacks := 0
			err := f.anchor.WithCurrent(context.Background(), observed, func(held *policyAuthorityAnchorHeld) error {
				callbacks++
				return held.Advance(context.Background(), []byte("next"), func([]byte) error { t.Fatal("refused callback wrote ledger"); return nil })
			})
			code := map[string]string{"missing": "missing", "corrupt": "integrity", "identity": "identity", "digest": "mismatch", "pending-old": "pending", "pending-new": "pending", "pending-other": "pending", "locked": "busy"}[scenario]
			policyAnchorRequireCode(t, err, code)
			if callbacks != 0 {
				t.Fatal("callback admitted", scenario)
			}
		})
	}
}
func TestPolicyAuthorityAnchorClosedJSONAndGeneration(t *testing.T) {
	state := policyAuthorityAnchorState{Version: 1, InstallationID: "install-1", WorkspaceID: "workspace-1", Generation: 1, LedgerDigest: strings.Repeat("a", 64), Intent: &policyAuthorityAnchorIntent{Generation: 2, OldDigest: strings.Repeat("a", 64), NewDigest: strings.Repeat("b", 64), Nonce: strings.Repeat("c", 48)}}
	raw, _ := json.Marshal(state)
	decoded, err := decodePolicyAuthorityAnchor(raw)
	if err != nil || decoded.Intent == nil {
		t.Fatal(decoded, err)
	}
	for _, field := range []string{"version", "installationId", "workspaceId", "generation", "ledgerDigest", "intent"} {
		for _, variant := range []string{"null", "case", "unknown", "missing"} {
			t.Run(field+"-"+variant, func(t *testing.T) {
				var object map[string]json.RawMessage
				_ = json.Unmarshal(raw, &object)
				switch variant {
				case "null":
					object[field] = json.RawMessage("null")
				case "case":
					value := object[field]
					delete(object, field)
					object[strings.ToUpper(field[:1])+field[1:]] = value
				case "unknown":
					object["extra"] = json.RawMessage("true")
				case "missing":
					delete(object, field)
				}
				data, _ := json.Marshal(object)
				_, err := decodePolicyAuthorityAnchor(data)
				if field == "intent" && variant == "missing" {
					if err != nil {
						t.Fatal("optional absent intent refused", err)
					}
				} else {
					policyAnchorRequireCode(t, err, "integrity")
				}
			})
		}
	}
	for _, field := range []string{"generation", "oldDigest", "newDigest", "nonce"} {
		for _, variant := range []string{"null", "case", "missing", "unknown"} {
			t.Run("intent-"+field+"-"+variant, func(t *testing.T) {
				var top map[string]json.RawMessage
				_ = json.Unmarshal(raw, &top)
				var object map[string]json.RawMessage
				_ = json.Unmarshal(top["intent"], &object)
				switch variant {
				case "null":
					object[field] = json.RawMessage("null")
				case "case":
					value := object[field]
					delete(object, field)
					object[strings.ToUpper(field[:1])+field[1:]] = value
				case "missing":
					delete(object, field)
				case "unknown":
					object["extra"] = json.RawMessage("true")
				}
				top["intent"], _ = json.Marshal(object)
				data, _ := json.Marshal(top)
				_, err := decodePolicyAuthorityAnchor(data)
				policyAnchorRequireCode(t, err, "integrity")
			})
		}
	}
	for _, data := range [][]byte{bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), bytes.Replace(raw, []byte(`"oldDigest":`), []byte(`"oldDigest":"`+strings.Repeat("a", 64)+`","oldDigest":`), 1), append(bytes.Clone(raw), []byte(` {}`)...), []byte("null"), []byte("[]"), bytes.Replace(raw, []byte(`"generation":1`), []byte(`"generation":1.0`), 1)} {
		_, err := decodePolicyAuthorityAnchor(data)
		policyAnchorRequireCode(t, err, "integrity")
	}
	for _, alter := range []func(*policyAuthorityAnchorState){func(s *policyAuthorityAnchorState) { s.Version = 2 }, func(s *policyAuthorityAnchorState) { s.Generation = 0 }, func(s *policyAuthorityAnchorState) { s.Generation = policyAuthorityAnchorMaxGeneration + 1 }, func(s *policyAuthorityAnchorState) { s.Intent.Generation = 1 }, func(s *policyAuthorityAnchorState) { s.Intent.OldDigest = strings.Repeat("d", 64) }, func(s *policyAuthorityAnchorState) { s.Intent.NewDigest = s.Intent.OldDigest }, func(s *policyAuthorityAnchorState) { s.Intent.Nonce = "short" }} {
		s := state
		p := *s.Intent
		s.Intent = &p
		alter(&s)
		policyAnchorRequireCode(t, validatePolicyAuthorityAnchorState(s), "integrity")
	}
	f := newPolicyAnchorFixture(t, true)
	limit := f.state(t)
	limit.Generation = policyAuthorityAnchorMaxGeneration
	data, _ := json.Marshal(limit)
	f.replaceState(t, data)
	err = f.withLedger(t, func(raw []byte) error {
		return f.anchor.WithCurrent(context.Background(), raw, func(h *policyAuthorityAnchorHeld) error {
			return h.Advance(context.Background(), []byte("next"), func([]byte) error { t.Fatal("exhausted generation wrote ledger"); return nil })
		})
	})
	policyAnchorRequireCode(t, err, "exhausted")
}
func TestPolicyAuthorityAnchorCanceledMutationAndEnrollmentFailure(t *testing.T) {
	for _, phase := range []string{"intent", "ledger", "argument-mutation", "enrollment-before", "enrollment-after", "enrollment-canceled"} {
		t.Run(phase, func(t *testing.T) {
			enrollment := strings.HasPrefix(phase, "enrollment-")
			f := newPolicyAnchorFixture(t, !enrollment)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			writes := 0
			f.anchor.persistForTest = func(stage policyAuthorityAnchorWritePhase, root *os.Root, raw []byte) error {
				if phase == "enrollment-before" {
					return errors.New("before enrollment")
				}
				if err := writePrivateData(root, policyAuthorityAnchorName, raw); err != nil {
					return err
				}
				if phase == "enrollment-after" {
					return errors.New("after enrollment")
				}
				if phase == "enrollment-canceled" {
					cancel()
				}
				if phase == "intent" && stage == policyAuthorityAnchorIntentWrite {
					cancel()
				}
				return nil
			}
			var err error
			if enrollment {
				err = f.anchor.Enroll(ctx, f.original)
			} else {
				next := []byte("new exact ledger")
				original := bytes.Clone(next)
				err = f.withLedger(t, func(raw []byte) error {
					return f.anchor.WithCurrent(ctx, raw, func(h *policyAuthorityAnchorHeld) error {
						return h.Advance(ctx, next, func(exact []byte) error {
							writes++
							if phase == "argument-mutation" {
								exact[0] = 'X'
								return nil
							}
							if err := writePrivateData(f.ledger, "ledger.json", exact); err != nil {
								return err
							}
							if phase == "ledger" {
								cancel()
							}
							return nil
						})
					})
				})
				if !bytes.Equal(next, original) {
					t.Fatal("caller input mutated")
				}
			}
			policyAnchorRequireCode(t, err, "uncertain")
			if enrollment {
				fresh, e := newPolicyAuthorityAnchor(f.anchor.directory, f.identity)
				if e != nil {
					t.Fatal(e)
				}
				if phase == "enrollment-after" || phase == "enrollment-canceled" {
					policyAnchorRequireCode(t, fresh.Enroll(context.Background(), f.original), "exists")
				} else {
					policyAnchorRequireCode(t, fresh.WithCurrent(context.Background(), f.original, func(*policyAuthorityAnchorHeld) error { t.Fatal("missing enrollment admitted"); return nil }), "missing")
				}
			} else {
				state := f.state(t)
				if state.Intent == nil {
					t.Fatal("lost pending write")
				}
				if phase == "intent" && writes != 0 {
					t.Fatal("canceled before ledger write still wrote")
				}
			}
		})
	}
}

func TestPolicyAuthorityAnchorPanicClosesHeldAndLaterFailureIsUncertain(t *testing.T) {
	f := newPolicyAnchorFixture(t, true)
	var escaped *policyAuthorityAnchorHeld
	panicValue := "controlled validation panic"
	recovered := false
	func() {
		defer func() {
			if value := recover(); value != panicValue {
				t.Fatalf("unexpected panic %v", value)
			} else {
				recovered = true
			}
		}()
		_ = f.withLedger(t, func(raw []byte) error {
			return f.anchor.WithCurrent(context.Background(), raw, func(h *policyAuthorityAnchorHeld) error { escaped = h; panic(panicValue) })
		})
	}()
	if !recovered {
		t.Fatal("callback did not panic")
	}
	_, err := escaped.Current()
	policyAnchorRequireCode(t, err, "not-held")
	err = escaped.Advance(context.Background(), []byte("escaped"), func([]byte) error { t.Fatal("escaped panic handle wrote"); return nil })
	policyAnchorRequireCode(t, err, "not-held")
	writes := 0
	next := []byte("committed exact next ledger")
	err = f.withLedger(t, func(raw []byte) error {
		return f.anchor.WithCurrent(context.Background(), raw, func(h *policyAuthorityAnchorHeld) error {
			if err := h.Advance(context.Background(), next, func(exact []byte) error { writes++; return writePrivateData(f.ledger, "ledger.json", exact) }); err != nil {
				t.Fatal(err)
			}
			second := h.Advance(context.Background(), next, func([]byte) error { t.Fatal("second advance invoked callback"); return nil })
			policyAnchorRequireCode(t, second, "uncertain")
			return nil
		})
	})
	policyAnchorRequireCode(t, err, "uncertain")
	if writes != 1 || f.state(t).Generation != 2 {
		t.Fatal("successful first advance was lost", writes)
	}
	policyAnchorRequireCode(t, f.anchor.WithCurrent(context.Background(), next, func(*policyAuthorityAnchorHeld) error { t.Fatal("poisoned object admitted"); return nil }), "uncertain")
}

func TestPolicyAuthorityAnchorConcurrentAcquisitionFailsWithoutCallback(t *testing.T) {
	f := newPolicyAnchorFixture(t, true)
	second, err := newPolicyAuthorityAnchor(f.anchor.directory, f.identity)
	if err != nil {
		t.Fatal(err)
	}
	err = f.withLedger(t, func(raw []byte) error {
		entered, release := make(chan struct{}), make(chan struct{})
		completed := make(chan error, 1)
		go func() {
			completed <- f.anchor.WithCurrent(context.Background(), raw, func(*policyAuthorityAnchorHeld) error { close(entered); <-release; return nil })
		}()
		<-entered
		denied := second.WithCurrent(context.Background(), raw, func(*policyAuthorityAnchorHeld) error { t.Error("concurrent anchor callback admitted"); return nil })
		close(release)
		firstErr := <-completed
		if firstErr != nil {
			t.Fatal(firstErr)
		}
		policyAnchorRequireCode(t, denied, "busy")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPolicyAuthorityAnchorConfigurationDoesNotEnroll(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "absent-machine-authority")
	identity := policyAuthorityAnchorIdentity{InstallationID: "installation-1", WorkspaceID: "workspace-1"}
	for _, input := range []struct {
		directory string
		identity  policyAuthorityAnchorIdentity
	}{
		{"relative", identity}, {string(filepath.Separator), identity}, {directory + string(filepath.Separator) + "..", identity}, {directory + "\n", identity}, {directory, policyAuthorityAnchorIdentity{InstallationID: "", WorkspaceID: "workspace-1"}},
	} {
		_, err := newPolicyAuthorityAnchor(input.directory, input.identity)
		policyAnchorRequireCode(t, err, "invalid-input")
	}
	if _, err := newPolicyAuthorityAnchor(directory, identity); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("constructor touched trusted path", err)
	}
}

func TestPolicyAuthorityAnchorRecoveredInnerPanicRemainsUncertain(t *testing.T) {
	for _, edge := range []string{"intent-persistence", "ledger-writer"} {
		t.Run(edge, func(t *testing.T) {
			f := newPolicyAnchorFixture(t, true)
			next := []byte("durable new ledger")
			panicValue := "controlled inner write panic"
			writerCalls := 0
			if edge == "intent-persistence" {
				f.anchor.persistForTest = func(phase policyAuthorityAnchorWritePhase, root *os.Root, raw []byte) error {
					if err := writePrivateData(root, policyAuthorityAnchorName, raw); err != nil {
						return err
					}
					if phase == policyAuthorityAnchorIntentWrite {
						panic(panicValue)
					}
					t.Error("panic recovery attempted later persistence")
					return nil
				}
			}
			err := f.withLedger(t, func(raw []byte) error {
				return f.anchor.WithCurrent(context.Background(), raw, func(h *policyAuthorityAnchorHeld) error {
					recovered := false
					func() {
						defer func() {
							if value := recover(); value != panicValue {
								t.Errorf("original panic changed: %v", value)
							} else {
								recovered = true
							}
						}()
						_ = h.Advance(context.Background(), next, func(exact []byte) error {
							writerCalls++
							if err := writePrivateData(f.ledger, "ledger.json", exact); err != nil {
								return err
							}
							panic(panicValue)
						})
					}()
					if !recovered {
						t.Error("write panic did not propagate")
					}
					_, currentErr := h.Current()
					policyAnchorRequireCode(t, currentErr, "not-held")
					nextErr := h.Advance(context.Background(), next, func([]byte) error { t.Error("recovered panic replayed writer"); return nil })
					policyAnchorRequireCode(t, nextErr, "not-held")
					return nil
				})
			})
			policyAnchorRequireCode(t, err, "uncertain")
			if edge == "intent-persistence" && writerCalls != 0 || edge == "ledger-writer" && writerCalls != 1 {
				t.Fatal(writerCalls)
			}
			state := f.state(t)
			if state.Intent == nil {
				t.Fatal("panic lost durable intent")
			}
			actual, readErr := readPrivateData(f.ledger, "ledger.json", policyLedgerLimit)
			if readErr != nil {
				t.Fatal(readErr)
			}
			observed, readErr := classifyPolicyAuthorityAnchor(state, f.identity, digestOf(actual))
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := "pending-old"
			if edge == "ledger-writer" {
				want = "pending-new"
			}
			if observed.Classification != want {
				t.Fatal(observed)
			}
			policyAnchorRequireCode(t, f.anchor.WithCurrent(context.Background(), actual, func(*policyAuthorityAnchorHeld) error { t.Error("poisoned panic instance admitted"); return nil }), "uncertain")
		})
	}
}
