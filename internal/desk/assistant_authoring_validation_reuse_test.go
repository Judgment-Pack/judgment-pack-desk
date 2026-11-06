package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func assistantAuthoringValidationFixture(t *testing.T) (*Server, string, assistantRunStore, *http.Request, assistantRunBinding, assistantRunStart) {
	t.Helper()
	s, token := assistantAuthoringFixture(t, "https://example.invalid")
	ref := assistantAuthoringCaptureFixture(t, s, token, assistantAuthoringRequestFixture("validation-facts", "author-pack"))
	store := s.assistantRuns()
	// Admission writes already establish the shared fact; start cold deliberately.
	s.assistantRunValidation = nil
	r := assistantProtocolRequest(token)
	identity, _, err := s.assistantRunIdentity(r)
	assistantRunMust(t, err)
	return s, token, store, r, identity, *ref.Start
}

func assistantAuthoringCountFullValidation(t *testing.T) *atomic.Int32 {
	t.Helper()
	calls := new(atomic.Int32)
	testHookAssistantAuthoringValidation = func() { calls.Add(1) }
	t.Cleanup(func() { testHookAssistantAuthoringValidation = nil })
	return calls
}

func TestAssistantAuthoringValidationReuseOwnsBytesAndKeepsTransactions(t *testing.T) {
	s, _, store, r, identity, start := assistantAuthoringValidationFixture(t)
	calls := assistantAuthoringCountFullValidation(t)
	first, err := s.readAssistantRunSource(r, start, identity, store.project)
	assistantRunMust(t, err)
	if calls.Load() != 1 {
		t.Fatal("cold read did not fully validate exactly once", calls.Load())
	}
	originalRequest, originalSnapshot := bytes.Clone(first.Request), bytes.Clone(first.Original)
	for i := 0; i < 3; i++ {
		next, err := s.readAssistantRunSource(r, start, identity, store.project)
		assistantRunMust(t, err)
		if !assistantRunFreshSourceEqual(first, next) {
			t.Fatal("warm read changed source facts")
		}
		next.Request[0], next.Original[0] = 'x', 'x'
	}
	if calls.Load() != 1 {
		t.Fatal("identical current bytes repeated full validation", calls.Load())
	}
	if !bytes.Equal(first.Request, originalRequest) || !bytes.Equal(first.Original, originalSnapshot) {
		t.Fatal("returned source payloads alias each other")
	}
	lock, err := s.privateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	defer lock.Close()
	cold, err := s.resolveAssistantRun(r, start, identity, store.project)
	assistantRunMust(t, err)
	assistantRunMust(t, lock.Close())
	if !assistantRunFreshSourceEqual(first, cold) || calls.Load() != 2 {
		t.Fatal("freshCAS resolver reused read-only validation", calls.Load())
	}
	callbacks := 0
	_, err = store.transaction(t.Context(), func(*assistantRunLedger) (bool, error) { callbacks++; return false, nil })
	assistantRunMust(t, err)
	if callbacks != 1 {
		t.Fatal("transaction callback repeated", callbacks)
	}
	if len(s.assistantRunValidation.raw) > assistantRunRecordLimit || cap(s.assistantRunValidation.raw) != len(s.assistantRunValidation.raw) {
		t.Fatal("retained validation bytes exceed the single-entry bound")
	}
	if !bytes.Equal(s.assistantRunValidation.raw, assistantRunRaw(t, store)) {
		t.Fatal("caller mutation changed retained validation bytes")
	}
}

func TestAssistantAuthoringValidationReuseCurrentAuthority(t *testing.T) {
	for _, mode := range []string{"saved-bytes", "declaration", "profile", "connection", "session", "identity", "reference", "capture-cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s, token, store, r, identity, start := assistantAuthoringValidationFixture(t)
			calls := assistantAuthoringCountFullValidation(t)
			_, err := s.readAssistantRunSource(r, start, identity, store.project)
			assistantRunMust(t, err)
			switch mode {
			case "saved-bytes":
				writeProjectFile(t, s.projectDir, "one.pack.json", reviewPack+"\n")
			case "declaration":
				writeProjectFile(t, s.projectDir, "jpack.json", "{\"packs\":{\"one\":{\"path\":\"one.pack.json\",\"description\":\"changed\"}}}")
			case "profile":
				writeProjectFile(t, s.projectDir, assistantProfilePath, "{\"profileVersion\":1,\"api\":{\"inherit\":true},\"critic\":\"on\"}")
			case "connection":
				registry, err := s.readAIRegistry()
				assistantRunMust(t, err)
				registry.Connections[0].Assistant.Endpoint.URL = "https://changed.invalid"
				raw, err := encodeAIRegistry(registry.aiRegistry)
				assistantRunMust(t, err)
				assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
			case "session":
				s.sessions.revoke(token)
			case "identity":
				identity.Policy = strings.Repeat("f", 64)
			case "reference":
				start.RequestID = "different-request"
			case "capture-cancelled":
				_, err := store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) { l.AuthoringCaptures[0].Cancelled = true; return true, nil })
				assistantRunMust(t, err)
			}
			got, err := s.readAssistantRunSource(r, start, identity, store.project)
			if err == nil || !reflect.DeepEqual(got, assistantRunSource{}) {
				t.Fatal("validation fact became current authority", mode, err)
			}
			// A successful changed transaction has already proven its exact
			// output. Cancellation still refuses using current capture state.
			want := int32(1)
			if calls.Load() != want {
				t.Fatal("wrong exact-byte hit/miss behavior", mode, calls.Load(), want)
			}
		})
	}
}

func TestAssistantAuthoringValidationReuseQueuedCurrentness(t *testing.T) {
	for _, mode := range []string{"saved-bytes", "session", "request-cancelled", "capture-cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s, token, store, r, identity, start := assistantAuthoringValidationFixture(t)
			_, err := s.readAssistantRunSource(r, start, identity, store.project)
			assistantRunMust(t, err)
			lock, err := s.privateDataLockContext(t.Context(), true)
			assistantRunMust(t, err)
			defer lock.Close()
			data, err := s.openChatData()
			assistantRunMust(t, err)
			defer data.root.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			type answer struct {
				source assistantRunSource
				err    error
			}
			done := make(chan answer, 1)
			go func() {
				got, err := s.readAssistantRunSource(r.WithContext(ctx), start, identity, store.project)
				done <- answer{got, err}
			}()
			deadline := time.NewTimer(3 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
		waiting:
			for {
				s.privateDataTurns.mu.Lock()
				n := len(s.privateDataTurns.waiters)
				s.privateDataTurns.mu.Unlock()
				if n >= 2 {
					break waiting
				}
				select {
				case got := <-done:
					t.Fatal("reader escaped held custody", got.err)
				case <-deadline.C:
					t.Fatal("reader did not queue")
				case <-ticker.C:
				}
			}
			switch mode {
			case "saved-bytes":
				writeProjectFile(t, s.projectDir, "one.pack.json", reviewPack+"\n")
			case "session":
				s.sessions.revoke(token)
			case "request-cancelled":
				cancel()
			case "capture-cancelled":
				raw, err := readPrivateData(data.root, store.name, assistantRunRecordLimit)
				assistantRunMust(t, err)
				var ledger assistantRunLedger
				assistantRunMust(t, json.Unmarshal(raw, &ledger))
				ledger.AuthoringCaptures[0].Cancelled = true
				raw, err = json.Marshal(ledger)
				assistantRunMust(t, err)
				assistantRunMust(t, writePrivateData(data.root, store.name, raw))
			}
			assistantRunMust(t, lock.Close())
			select {
			case got := <-done:
				if got.err == nil || !reflect.DeepEqual(got.source, assistantRunSource{}) {
					t.Fatal("queued read used stale authority", mode, got.err)
				}
				if mode == "request-cancelled" && !errors.Is(got.err, context.Canceled) {
					t.Fatal("cancellation lost", got.err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("queued read did not settle")
			}
		})
	}
}

func TestAssistantAuthoringValidationReuseUnseenJournalAndRestore(t *testing.T) {
	store, _, _, _, _ := canonicalActivationAdmitted(t)
	s := store.server
	s.assistantRunValidation = nil // Exercise a cold historical journal read.
	calls := assistantAuthoringCountFullValidation(t)
	lock, err := s.privateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	defer lock.Close()
	data, err := s.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	original, err := readPrivateData(data.root, store.name, assistantRunRecordLimit)
	assistantRunMust(t, err)
	_, err = s.readAssistantAuthoringCapturesLocked(t.Context(), data, store.project)
	assistantRunMust(t, err)
	_, err = s.readAssistantAuthoringCapturesLocked(t.Context(), data, store.project)
	assistantRunMust(t, err)
	if calls.Load() != 1 {
		t.Fatal("warm canonical ledger was not reused", calls.Load())
	}
	var ledger assistantRunLedger
	assistantRunMust(t, json.Unmarshal(original, &ledger))
	// Handwritten same-size corruption of original canonical checkpoint bytes.
	ledger.Runs[0].Canonical.Head.Checkpoint[0] = '['
	tampered, err := json.Marshal(ledger)
	assistantRunMust(t, err)
	if len(tampered) != len(original) || bytes.Equal(tampered, original) {
		t.Fatal("tamper must change bytes without changing size")
	}
	assistantRunMust(t, writePrivateData(data.root, store.name, tampered))
	for i := 0; i < 2; i++ {
		captures, err := s.readAssistantAuthoringCapturesLocked(t.Context(), data, store.project)
		if err == nil || captures != nil {
			t.Fatal("unseen canonical corruption reused validation")
		}
	}
	if calls.Load() != 3 {
		t.Fatal("invalid content was cached or skipped", calls.Load())
	}
	if !bytes.Equal(s.assistantRunValidation.raw, original) {
		t.Fatal("failed validation replaced the fact")
	}
	assistantRunMust(t, writePrivateData(data.root, store.name, original))
	_, err = s.readAssistantAuthoringCapturesLocked(t.Context(), data, store.project)
	assistantRunMust(t, err)
	if calls.Load() != 3 {
		t.Fatal("exact valid restoration could not reuse parsing fact", calls.Load())
	}
}

func TestAssistantAuthoringValidationReuseRootedScope(t *testing.T) {
	s, _, store, r, identity, start := assistantAuthoringValidationFixture(t)
	calls := assistantAuthoringCountFullValidation(t)
	_, err := s.readAssistantRunSource(r, start, identity, store.project)
	assistantRunMust(t, err)
	lock, err := s.privateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	defer lock.Close()
	data, err := s.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	root, err := data.root.Stat(".")
	assistantRunMust(t, err)
	proof := s.assistantRunValidation
	for _, mode := range []string{"revision", "path", "project", "filename", "raw"} {
		held := *data
		project, name, raw := store.project, store.name, bytes.Clone(proof.raw)
		switch mode {
		case "revision":
			held.revision += "changed"
		case "path":
			held.location.Path += "-other"
		case "project":
			project = strings.Repeat("f", 64)
		case "filename":
			name = "other.json"
		case "raw":
			raw[len(raw)-1] ^= 1
		}
		if proof.matches(root, &held, project, name, raw) {
			t.Fatal("validation key ignored changed metadata/bytes", mode)
		}
	}
	other, err := s.openChatDataAt(dataLocation{Version: 1, Path: filepath.Join(t.TempDir(), "other-data")}, "other-revision", true)
	assistantRunMust(t, err)
	defer other.root.Close()
	otherRoot, err := other.root.Stat(".")
	assistantRunMust(t, err)
	if proof.matches(otherRoot, data, store.project, store.name, proof.raw) {
		t.Fatal("different rooted inode reused proof")
	}
	assistantRunMust(t, writePrivateData(other.root, store.name, proof.raw))
	_, err = s.readAssistantAuthoringCapturesLocked(t.Context(), other, store.project)
	assistantRunMust(t, err)
	if calls.Load() != 2 {
		t.Fatal("alternate root skipped full validation", calls.Load())
	}
	foreign := strings.Repeat("f", 64)
	assistantRunMust(t, writePrivateData(other.root, "assistant-runs-"+foreign+".json", proof.raw))
	if _, err := s.readAssistantAuthoringCapturesLocked(t.Context(), other, foreign); !errors.Is(err, errAssistantRunScope) {
		t.Fatal("foreign project reused validation", err)
	}
	if calls.Load() != 2 {
		t.Fatal("foreign scope reached decoder", calls.Load())
	}
}

func TestAssistantAuthoringValidationReusePrivateFileGuards(t *testing.T) {
	for _, mode := range []string{"missing", "permissions", "symlink", "malformed", "capture-request"} {
		t.Run(mode, func(t *testing.T) {
			s, _, store, r, identity, start := assistantAuthoringValidationFixture(t)
			calls := assistantAuthoringCountFullValidation(t)
			_, err := s.readAssistantRunSource(r, start, identity, store.project)
			assistantRunMust(t, err)
			data, err := s.openChatData()
			assistantRunMust(t, err)
			path := filepath.Join(data.root.Name(), store.name)
			assistantRunMust(t, data.root.Close())
			switch mode {
			case "missing":
				assistantRunMust(t, os.Remove(path))
			case "permissions":
				assistantRunMust(t, os.Chmod(path, 0644))
				defer os.Chmod(path, 0600)
			case "symlink":
				target := filepath.Join(t.TempDir(), "ledger")
				assistantRunMust(t, os.WriteFile(target, s.assistantRunValidation.raw, 0600))
				assistantRunMust(t, os.Remove(path))
				assistantRunMust(t, os.Symlink(target, path))
			case "malformed":
				assistantRunMust(t, os.WriteFile(path, []byte("{\"version\":1"), 0600))
			case "capture-request":
				var ledger assistantRunLedger
				assistantRunMust(t, json.Unmarshal(s.assistantRunValidation.raw, &ledger))
				ledger.AuthoringCaptures[0].Request[0] = '['
				raw, err := json.Marshal(ledger)
				assistantRunMust(t, err)
				if len(raw) != len(s.assistantRunValidation.raw) {
					t.Fatal("request tamper changed storage size")
				}
				assistantRunMust(t, os.WriteFile(path, raw, 0600))
			}
			got, err := s.readAssistantRunSource(r, start, identity, store.project)
			if err == nil || !reflect.DeepEqual(got, assistantRunSource{}) {
				t.Fatal("warm proof bypassed private file refusal", mode, err)
			}
			want := int32(1)
			if mode == "malformed" || mode == "capture-request" {
				want = 2
			}
			if calls.Load() != want {
				t.Fatal("unexpected full decoder count", mode, calls.Load())
			}
		})
	}
}
