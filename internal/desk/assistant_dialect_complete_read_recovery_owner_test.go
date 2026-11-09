//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func invokeRecoveryFixtureRead(t *testing.T, e *assistantRunExecution, saved inputPreparationReply, call string) runcontext.ToolResult {
	t.Helper()
	registry, err := e.assistantDialectTools()
	assistantRunMust(t, err)
	raw := assistantChatJSON(map[string]string{"id": saved.Document.ID, "packKey": "example"})
	assistantPolicyEvent(t, e, call, "read_dialect", raw)
	ctx := context.WithValue(runcontext.WithCallID(e.generationContext(t.Context(), 1), call), assistantHostRootKey{}, true)
	for _, tool := range registry.tools {
		if tool.Name == "read_dialect" {
			result, err := tool.Execute(ctx, raw)
			assistantRunMust(t, err)
			return result
		}
	}
	t.Fatal("actual read tool missing")
	return runcontext.ToolResult{}
}

func stoppedRecoveryFixture(t *testing.T) (*Server, *assistantRunExecution, assistantRunRecord, string) {
	t.Helper()
	s, e, saved := completeEnrollmentExecution(t)
	fired := false
	testHookDialectArtifactOwnerStep = func(step string) error {
		if step == "complete-read-raw.file-close" && !fired {
			fired = true
			return errAssistantDialectCompleteRead
		}
		return nil
	}
	t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
	result := invokeRecoveryFixtureRead(t, e, saved, "recovery-original")
	testHookDialectArtifactOwnerStep = nil
	if !fired || !result.Err() {
		t.Fatal("original actual raw Close failure not exercised")
	}
	before, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	if len(before.DialectReads) != 1 || before.DialectReads[0].Receipt != nil {
		t.Fatal("unacknowledged original not retained")
	}
	assistantRunMust(t, e.writer.Stop(t.Context()))
	terminal, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	if _, err = s.assistantRunService().pinDialectReadRecovery(t.Context(), terminal); err == nil {
		t.Fatal("terminal row displaced still-active original owner")
	}
	e.cancel()
	select {
	case <-e.done:
	case <-time.After(3 * time.Second):
		t.Fatal("original owner did not close")
	}
	run, err := s.assistantRuns().Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	return s, e, run, run.DialectReads[0].ReadOperationID
}

func TestDialectReadRecoveryStoppedNativeObservation(t *testing.T) {
	s, e, run, operation := stoppedRecoveryFixture(t)
	before := assistantChatJSON(run)
	pin, err := s.assistantRunService().pinDialectReadRecovery(t.Context(), run)
	assistantRunMust(t, err)
	defer func() { assistantRunMust(t, pin.Close()) }()
	r := e.request.Clone(t.Context())
	g, err := pin.acquire(r)
	assistantRunMust(t, err)
	state, reason, provenance, err := g.observeNative(t.Context(), operation)
	assistantRunMust(t, err)
	if state != "present-valid" || reason != "" || !assistantRunDigest(provenance) {
		t.Fatalf("observation %q/%q", state, reason)
	}
	assistantRunMust(t, g.Close())
	after, err := s.assistantRuns().Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if !bytes.Equal(before, assistantChatJSON(after)) || after.DialectReads[0].Receipt != nil {
		t.Fatal("observation changed original operation/result/custody")
	}
	// Repeated observation has no Put and does not grow a recovery file without
	// the real B2 reservation. This is read custody evidence, not quota evidence.
	lock, err := s.privateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	data, err := s.openChatData()
	assistantRunMust(t, err)
	_, statErr := data.root.Lstat(assistantDialectReadRecoveryName(run.Binding.Project, run.ID))
	assistantRunMust(t, data.root.Close())
	assistantRunMust(t, lock.Close())
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("read-only observation created metadata")
	}
}

func TestDialectReadRecoveryActualRouteRequiresOriginalBudget(t *testing.T) {
	s, e, run, operation := stoppedRecoveryFixture(t)
	request := httptest.NewRequest("POST", "/api/assistant-runs/"+run.ID+"/dialect-reads/"+operation+"/observe", strings.NewReader(`{"runRevision":"`+runRevisionForRecovery(run)+`","recoveryRevision":"0","requestId":"inspect-one"}`))
	request.Header = e.request.Header.Clone()
	answer := httptest.NewRecorder()
	s.mux.ServeHTTP(answer, request)
	if answer.Code != 503 {
		t.Fatalf("actual route status %d: %s", answer.Code, answer.Body.String())
	}
	if strings.Contains(answer.Body.String(), s.projectDir) || strings.Contains(answer.Body.String(), run.DialectArtifactOwner) {
		t.Fatal("public refusal disclosed storage identity")
	}
	after, err := s.assistantRuns().Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(run, after) {
		t.Fatal("unreserved route changed original history")
	}
	get := httptest.NewRequest("GET", "/api/assistant-runs/"+run.ID+"/dialect-reads", nil)
	get.Header = e.request.Header.Clone()
	answer = httptest.NewRecorder()
	s.mux.ServeHTTP(answer, get)
	if answer.Code != 200 {
		t.Fatalf("saved status %d: %s", answer.Code, answer.Body.String())
	}
	var view assistantDialectReadRecoveryView
	assistantRunMust(t, json.Unmarshal(answer.Body.Bytes(), &view))
	if view.RecoveryRevision != "0" || len(view.Reads) != 1 || view.Reads[0].CanInspect || view.Reads[0].Reason != "budget-unresolved" {
		t.Fatal("unavailable production quota fabricated readiness")
	}
}

func runRevisionForRecovery(run assistantRunRecord) string {
	return strings.Trim(string(assistantChatJSON(run.Revision)), " ")
}

func TestDialectReadRecoveryIndependentExactObjects(t *testing.T) {
	s, e, saved := completeEnrollmentExecution(t)
	fired := false
	testHookDialectArtifactOwnerStep = func(step string) error {
		if step == "complete-read-raw.file-close" && !fired {
			fired = true
			return errAssistantDialectCompleteRead
		}
		return nil
	}
	t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
	if result := invokeRecoveryFixtureRead(t, e, saved, "uncertain-object-a"); !result.Err() {
		t.Fatal("original did not fail")
	}
	testHookDialectArtifactOwnerStep = nil
	before, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	doc := saved.Document
	doc.ID = "prep_" + strings.Repeat("2", 32)
	other, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, "")
	assistantRunMust(t, err)
	if result := invokeRecoveryFixtureRead(t, e, other, "independent-object-b"); result.Err() {
		t.Fatalf("unrelated original read fenced: %s", result.Value().JSON())
	}
	after, err := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
	assistantRunMust(t, err)
	if len(after.DialectReads) != 2 || !reflect.DeepEqual(before.DialectReads[0], after.DialectReads[0]) || after.DialectReads[1].Receipt == nil {
		t.Fatal("independent read rewrote old uncertainty or failed to acknowledge itself")
	}
	if result := invokeRecoveryFixtureRead(t, e, saved, "same-unresolved-object-a"); !result.Err() {
		t.Fatal("same unresolved object replay admitted")
	}
}

// The only executable is this test's own binary and inherited fixture FDs.
// No installed program, token, provider or operator path enters the child.
func TestDialectReadRecoveryLeaseHelper(t *testing.T) {
	if os.Getenv("DESK_TEST_RECOVERY_LEASE_CHILD") != "1" {
		return
	}
	work, executor, ready := os.NewFile(3, "fixture-work"), os.NewFile(4, "fixture-executor"), os.NewFile(5, "fixture-ready")
	if work == nil || executor == nil || ready == nil {
		t.Fatal("fixture descriptors missing")
	}
	defer work.Close()
	defer executor.Close()
	defer ready.Close()
	assistantRunMust(t, lockPrivateData(work, false))
	assistantRunMust(t, lockPrivateData(executor, true))
	_, err := ready.Write([]byte("ready"))
	assistantRunMust(t, err)
	var release [1]byte
	_, err = io.ReadFull(os.Stdin, release[:])
	assistantRunMust(t, err)
}

func TestDialectReadRecoveryRealExecutorExclusion(t *testing.T) {
	for _, substitute := range []bool{false, true} {
		name := "original-executor"
		if substitute {
			name = "substituted-executor"
		}
		t.Run(name, func(t *testing.T) { recoveryExternalExecutorBarrier(t, substitute) })
	}
}
func recoveryExternalExecutorBarrier(t *testing.T, substitute bool) {
	s, _, run, _ := stoppedRecoveryFixture(t)
	work, err := s.assistant.root.OpenFile(".data-work.lock", os.O_RDWR|openNoFollow|openNonBlocking, 0)
	assistantRunMust(t, err)
	executor, err := s.assistant.root.OpenFile("assistant-run-executor-"+run.Binding.Project+".lock", os.O_RDWR|openNoFollow|openNonBlocking, 0)
	assistantRunMust(t, err)
	readyRead, readyWrite, err := os.Pipe()
	assistantRunMust(t, err)
	releaseRead, releaseWrite, err := os.Pipe()
	assistantRunMust(t, err)
	cmd := exec.Command(os.Args[0], "-test.run=^TestDialectReadRecoveryLeaseHelper$")
	cmd.Env = []string{"DESK_TEST_RECOVERY_LEASE_CHILD=1"}
	cmd.ExtraFiles = []*os.File{work, executor, readyWrite}
	cmd.Stdin = releaseRead
	assistantRunMust(t, cmd.Start())
	defer func() {
		_ = releaseWrite.Close()
		_ = readyRead.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	assistantRunMust(t, work.Close())
	assistantRunMust(t, executor.Close())
	assistantRunMust(t, readyWrite.Close())
	assistantRunMust(t, releaseRead.Close())
	assistantRunMust(t, readyRead.SetReadDeadline(time.Now().Add(3*time.Second)))
	var ready [5]byte
	_, err = io.ReadFull(readyRead, ready[:])
	assistantRunMust(t, err)
	if string(ready[:]) != "ready" {
		t.Fatal("child barrier not reached")
	}
	if substitute {
		name := "assistant-run-executor-" + run.Binding.Project + ".lock"
		assistantRunMust(t, s.assistant.root.Rename(name, name+".original-fixture"))
		replacement, err := s.assistant.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|openNoFollow, 0600)
		assistantRunMust(t, err)
		assistantRunMust(t, replacement.Close())
	}
	before := assistantChatJSON(run)
	if pin, err := s.assistantRunService().pinDialectReadRecovery(t.Context(), run); err == nil {
		_ = pin.Close()
		t.Fatal("live original executor was displaced")
	}
	after, err := s.assistantRuns().Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if !bytes.Equal(before, assistantChatJSON(after)) {
		t.Fatal("refused external acquisition changed original history")
	}
	assertRecoverySidecarAbsent(t, s, run)
	_, err = releaseWrite.Write([]byte{1})
	assistantRunMust(t, err)
	assistantRunMust(t, releaseWrite.Close())
	assistantRunMust(t, cmd.Wait())
	pin, err := s.assistantRunService().pinDialectReadRecovery(t.Context(), run)
	assistantRunMust(t, err)
	assistantRunMust(t, pin.Close())
}

func assertRecoverySidecarAbsent(t *testing.T, s *Server, run assistantRunRecord) {
	t.Helper()
	data, err := s.openChatData()
	assistantRunMust(t, err)
	_, err = data.root.Lstat(assistantDialectReadRecoveryName(run.Binding.Project, run.ID))
	if !injectedArtifactOnlyNotExist(err) {
		t.Fatal("observation unexpectedly created recovery history", err)
	}
	assistantRunMust(t, data.root.Close())
}

func TestDialectReadRecoverySubstitutedWorkRefusesOriginalProvenance(t *testing.T) {
	s, e, run, operation := stoppedRecoveryFixture(t)
	assistantRunMust(t, s.assistant.root.Rename(".data-work.lock", ".data-work.lock.original-fixture"))
	replacement, err := s.assistant.root.OpenFile(".data-work.lock", os.O_CREATE|os.O_EXCL|os.O_RDWR|openNoFollow, 0600)
	assistantRunMust(t, err)
	assistantRunMust(t, replacement.Close())
	pin, err := s.assistantRunService().pinDialectReadRecovery(t.Context(), run)
	assistantRunMust(t, err)
	defer func() { assistantRunMust(t, pin.Close()) }()
	guard, err := pin.acquire(e.request.Clone(t.Context()))
	assistantRunMust(t, err)
	rawRead := false
	testHookDialectArtifactOwnerStep = func(step string) error {
		if strings.HasPrefix(step, "complete-read-raw.") {
			rawRead = true
		}
		return nil
	}
	state, reason, _, observedErr := guard.observeNative(t.Context(), operation)
	if observedErr == nil || state != "unavailable" || reason != "ownership-mismatch" || rawRead {
		t.Fatal("replacement work inode acquired original raw authority", state, reason, observedErr, rawRead)
	}
	// Ordering-only guard-copy assertion: no actual ledger is rewritten and no
	// new execution/owner/absence authority is manufactured by this fixture.
	guard.run.DialectReads = nil
	state, reason, _, observedErr = guard.observeNative(t.Context(), operation)
	if observedErr == nil || state != "unavailable" || reason != "ownership-mismatch" || rawRead {
		t.Fatal("unreserved guard copy promoted substituted work into absence", state, reason, observedErr, rawRead)
	}
	guard.run.DialectArtifactOwner = ""
	state, reason, _, observedErr = guard.observeNative(t.Context(), operation)
	if observedErr == nil || state != "unavailable" || reason != "ownership-mismatch" || rawRead {
		t.Fatal("missing original owner became physical absence evidence", state, reason, observedErr, rawRead)
	}
	testHookDialectArtifactOwnerStep = nil
	assistantRunMust(t, guard.Close())
	after, err := s.assistantRuns().Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(run, after) {
		t.Fatal("replacement work observation changed original history")
	}
	assertRecoverySidecarAbsent(t, s, run)
}

func TestDialectReadRecoveryCorruptionCleanupUncertainty(t *testing.T) {
	for _, cleanupFailure := range []bool{false, true} {
		name := "pure-corruption"
		if cleanupFailure {
			name = "corruption-and-root-close"
		}
		t.Run(name, func(t *testing.T) {
			s, e, run, operation := stoppedRecoveryFixture(t)
			ref := run.DialectReads[0].Intent.Reference
			path := filepath.Join(s.projectDir, retainedArtifactDirectory, ref.SHA256)
			raw, err := os.ReadFile(path)
			assistantRunMust(t, err)
			raw[0] ^= 1
			assistantRunMust(t, os.WriteFile(path, raw, 0600))
			pin, err := s.assistantRunService().pinDialectReadRecovery(t.Context(), run)
			assistantRunMust(t, err)
			defer func() { assistantRunMust(t, pin.Close()) }()
			guard, err := pin.acquire(e.request.Clone(t.Context()))
			assistantRunMust(t, err)
			closeFailure := errors.New("fixture original raw root Close uncertainty")
			fired := false
			testHookDialectArtifactOwnerStep = func(step string) error {
				if cleanupFailure && step == "complete-raw-root.close" {
					fired = true
					return closeFailure
				}
				return nil
			}
			state, reason, _, observedErr := guard.observeNative(t.Context(), operation)
			testHookDialectArtifactOwnerStep = nil
			assistantRunMust(t, guard.Close())
			if cleanupFailure {
				if !fired || state != "unavailable" || reason != "storage-io" || !errors.Is(observedErr, closeFailure) || !errors.Is(observedErr, errRetainedArtifactChanged) {
					t.Fatal("joined cleanup uncertainty was hidden", state, reason, observedErr)
				}
			} else if state != "corrupt" || reason != "" || observedErr != nil {
				t.Fatal("pure content corruption lost its distinct classification", state, reason, observedErr)
			}
			after, err := s.assistantRuns().Read(t.Context(), run.Binding, run.ID)
			assistantRunMust(t, err)
			if !reflect.DeepEqual(run, after) || after.DialectReads[0].Receipt != nil {
				t.Fatal("observation changed original ledger/result/receipt")
			}
			if s.assistantRunService().dialectReadRecoveryWriter != nil {
				t.Fatal("fixture minted accounting admission")
			}
			assertRecoverySidecarAbsent(t, s, run)
		})
	}
}

func TestDialectReadRecoveryBorrowedLeaseUnrelatedActiveLifetime(t *testing.T) {
	s, original, run, operation := stoppedRecoveryFixture(t)
	token := strings.TrimPrefix(original.request.Header.Get("Authorization"), "Bearer ")
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var document conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &document))
	chat := map[string]any{"id": "recovery-unrelated", "interaction": "work", "aiConnection": testAIAlpha, "model": "shared-model", "checkpoint": map[string]any{"state": map[string]any{"turns": []any{map[string]any{"id": "recovery-unrelated-message", "role": "user", "kind": "message", "text": "Independent admitted run", "at": "2026-10-20T00:00:00Z"}}}}}
	document.Chats = append(document.Chats, assistantChatJSON(chat))
	answer := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, document)
	if answer.Code != 200 {
		t.Fatal("save unrelated chat", answer.Code, answer.Body.String())
	}
	saved = policyWorkspaceRead(t, s, token, "/api/conversations")
	assistantRunMust(t, json.Unmarshal(saved.Content, &document))
	var rawChat json.RawMessage
	for _, row := range document.Chats {
		var id struct {
			ID string `json:"id"`
		}
		assistantRunMust(t, json.Unmarshal(row, &id))
		if id.ID == "recovery-unrelated" {
			rawChat = row
		}
	}
	if rawChat == nil {
		t.Fatal("unrelated chat not retained")
	}
	checked := make(chan *assistantRunExecution, 1)
	s.testAssistantRunMonitorChecked = func(ctx context.Context, e *assistantRunExecution) {
		select {
		case checked <- e:
		case <-ctx.Done():
			return
		}
		<-ctx.Done()
	}
	start := assistantRunStart{AdmissionID: "recovery-unrelated-admission", Purpose: "chat", OwnerID: "recovery-unrelated", Revision: digestOf(rawChat), RequestID: "recovery-unrelated-message"}
	view := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	var live *assistantRunExecution
	select {
	case live = <-checked:
	case <-time.After(3 * time.Second):
		t.Fatal("unrelated active owner did not cross authorization barrier")
	}
	t.Cleanup(func() {
		live.cancel()
		select {
		case <-live.done:
		case <-time.After(3 * time.Second):
			t.Error("unrelated owner did not drain")
		}
	})
	if live.record.ID != view.ID || live.record.ID == run.ID {
		t.Fatal("unrelated service admission missing")
	}
	host := s.assistantRunService()
	pin, err := host.pinDialectReadRecovery(t.Context(), run)
	assistantRunMust(t, err)
	defer func() { assistantRunMust(t, pin.Close()) }()
	if !pin.borrowed || pin.lease != live.writer.lease {
		t.Fatal("same-service original lease was not borrowed")
	}
	guard, err := pin.acquire(original.request.Clone(t.Context()))
	assistantRunMust(t, err)
	state, _, _, err := guard.observeNative(t.Context(), operation)
	assistantRunMust(t, err)
	if state != "present-valid" {
		t.Fatal("unrelated active run prevented original observation")
	}
	assistantRunMust(t, guard.Close())
	// The unrelated admitted writer remains usable between short guards.
	assistantRunMust(t, live.writer.State(t.Context(), "running"))
	assistantRunMust(t, live.writer.Stop(t.Context()))
	live.cancel()
	select {
	case <-live.done:
	case <-time.After(3 * time.Second):
		t.Fatal("unrelated owner did not close")
	}
	host.mu.Lock()
	retained := host.lease == pin.lease && pin.lease.file != nil && pin.lease.work != nil
	host.mu.Unlock()
	if !retained {
		t.Fatal("releaseIdle closed pinned original borrowed descriptors")
	}
	assistantRunMust(t, pin.Close())
	host.mu.Lock()
	released := host.lease == nil
	host.mu.Unlock()
	if !released {
		t.Fatal("last recovery pin did not release idle service lease")
	}
	after, err := s.assistantRuns().Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(run, after) {
		t.Fatal("unrelated run changed original read outcome")
	}
	assertRecoverySidecarAbsent(t, s, run)
}
