package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAssistantContextProtocolVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/chat-context-protocol.json")
	assistantRunMust(t, err)
	var rows []struct {
		Name, Kind, Purpose, Raw string
		Valid                    bool
	}
	assistantRunMust(t, json.Unmarshal(raw, &rows))
	for _, row := range rows {
		t.Run(row.Name, func(t *testing.T) {
			var err error
			if row.Kind == "selector" {
				err = assistantContextSelectorJSON([]byte(row.Raw), row.Purpose)
			} else {
				_, err = readAssistantContextRequest(row.Kind, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(row.Raw)))
			}
			if (err == nil) != row.Valid {
				t.Fatalf("acceptance %v, want %v: %v", err == nil, row.Valid, err)
			}
		})
	}
}
func assistantContextDocumentFixture(t *testing.T, s *Server, token, registration string) assistantContextDocumentView {
	t.Helper()
	w := policyCall(s, token, "POST", "/api/assistant-contexts/documents", map[string]string{"registrationId": registration})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var view assistantContextDocumentView
	assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &view))
	return view
}
func assistantContextPaneFixture(t *testing.T, s *Server, token, document, registration, purpose string) assistantContextPaneView {
	t.Helper()
	w := policyCall(s, token, "POST", "/api/assistant-contexts/panes", assistantContextPaneRequest{document, registration, purpose})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var view assistantContextPaneView
	assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &view))
	return view
}
func TestAssistantContextHTTPRegistrationAndRetirement(t *testing.T) {
	s, token, _, executor := assistantProtocolFixture(t, nil)
	first := assistantContextDocumentFixture(t, s, token, "document-one")
	retry := assistantContextDocumentFixture(t, s, token, "document-one")
	second := assistantContextDocumentFixture(t, s, token, "document-two")
	if first != retry || first == second {
		t.Fatal("registration did not preserve document identity")
	}
	pane := assistantContextPaneFixture(t, s, token, first.Document, "pane-one", "describe-pack")
	if again := assistantContextPaneFixture(t, s, token, first.Document, "pane-one", "describe-pack"); again != pane {
		t.Fatal("lost pane acknowledgement changed identity")
	}
	if w := policyCall(s, token, "POST", "/api/assistant-contexts/panes", assistantContextPaneRequest{first.Document, "pane-one", "author-pack"}); w.Code != 409 {
		t.Fatal("purpose changed", w.Code)
	}
	release := assistantContextPaneRelease{pane.Document, pane.Pane}
	for i := 0; i < 2; i++ {
		if w := policyCall(s, token, "POST", "/api/assistant-contexts/panes/release", release); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"released":true}` {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := policyCall(s, token, "POST", "/api/assistant-contexts/panes", assistantContextPaneRequest{first.Document, "pane-one", "describe-pack"}); w.Code != 404 {
		t.Fatal("retired pane recreated", w.Code)
	}
	remount := assistantContextPaneFixture(t, s, token, first.Document, "pane-two", "describe-pack")
	if remount == pane {
		t.Fatal("real remount reused retired pane")
	}
	if w := policyCall(s, token, "POST", "/api/assistant-contexts/documents/release", assistantContextDocumentRelease{first.Document}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := policyCall(s, token, "POST", "/api/assistant-contexts/documents", assistantContextDocumentRequest{"document-one"}); w.Code != 404 {
		t.Fatal("retired document recreated", w.Code)
	}
	if executor.calls.Load() != 0 {
		t.Fatal("model-free registration dispatched an executor")
	}
}
func TestAssistantContextHTTPClosedAndAuthenticated(t *testing.T) {
	s, token, _, _ := assistantProtocolFixture(t, nil)
	for _, body := range []any{map[string]any{"registrationId": "one", "principal": "forged"}, map[string]any{"registrationId": nil}, map[string]any{"RegistrationId": "one"}} {
		if w := policyCall(s, token, "POST", "/api/assistant-contexts/documents", body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := policyCall(s, "foreign-session", "POST", "/api/assistant-contexts/documents", assistantContextDocumentRequest{"one"}); w.Code == 200 {
		t.Fatal("unauthenticated registration")
	}
	if w := policyCall(s, token, "POST", "/api/assistant-contexts/panes", assistantContextPaneRequest{strings.Repeat("a", 64), "pane", "author-pack"}); w.Code != 404 {
		t.Fatal("unknown document allocated a pane", w.Code)
	}
}
func TestAssistantContextStartAndCaptureReference(t *testing.T) {
	original := assistantRunStart{AdmissionID: "author-admit-" + strings.Repeat("a", 32), Purpose: "describe-pack", OwnerID: "author_" + strings.Repeat("b", 32), Revision: strings.Repeat("c", 64), RequestID: "capture-one"}
	first := original
	first.Context = &assistantRunContextSelector{strings.Repeat("d", 64), strings.Repeat("e", 64)}
	second := first
	selector := *first.Context
	second.Context = &selector
	if !sameAssistantAuthoringReference(original, first) || !sameAssistantAuthoringReference(first, second) {
		t.Fatal("capture source comparison used ephemeral selector identity")
	}
	originalBytes, _ := json.Marshal(original)
	firstBytes, _ := json.Marshal(first)
	if bytes.Equal(originalBytes, firstBytes) || digestOf(originalBytes) == digestOf(firstBytes) {
		t.Fatal("Start body digest lost Context")
	}
	for _, mutate := range []func(*assistantRunStart){func(s *assistantRunStart) { s.AdmissionID = "other" }, func(s *assistantRunStart) { s.RequestID = "other" }, func(s *assistantRunStart) { s.OwnerID = "other" }, func(s *assistantRunStart) { s.Revision = "other" }, func(s *assistantRunStart) { s.Purpose = "author-pack" }} {
		changed := first
		mutate(&changed)
		if sameAssistantAuthoringReference(original, changed) {
			t.Fatal("capture reference member ignored")
		}
	}
	var decoded assistantRunStart
	assistantRunMust(t, assistantRunRequest(httptest.NewRequest("POST", "/", bytes.NewReader(firstBytes)), &decoded))
	if !reflect.DeepEqual(decoded, first) {
		t.Fatal("Start selector did not round trip structurally")
	}
	for _, purpose := range []string{"chat", "brief", "connection-diagnostic", "settings-observation"} {
		invalid := first
		invalid.Purpose = purpose
		raw, _ := json.Marshal(invalid)
		if assistantRunRequest(httptest.NewRequest("POST", "/", bytes.NewReader(raw)), &decoded) == nil {
			t.Fatal("pane selector accepted for", purpose)
		}
	}
}

// The durable admission key wins before any new context allocation/preparation.
// C1-D additionally asserts the same host handle at Prepare/Execute/continuation.
func TestAssistantContextAdmissionIdentityRace(t *testing.T) {
	entered := make(chan *assistantRunExecution, 1)
	s, token, original, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		<-ctx.Done()
		return ctx.Err()
	})
	first := assistantContextDocumentFixture(t, s, token, "first-document")
	second := assistantContextDocumentFixture(t, s, token, "second-document")
	starts := []assistantRunStart{original, original}
	starts[0].Context = &assistantRunContextSelector{Document: first.Document}
	starts[1].Context = &assistantRunContextSelector{Document: second.Document}
	type result struct {
		index, status int
		body          []byte
	}
	results := make(chan result, 2)
	begin := make(chan struct{})
	for i := range starts {
		go func(i int) {
			<-begin
			w := policyCall(s, token, "POST", "/api/assistant-runs", starts[i])
			results <- result{i, w.Code, bytes.Clone(w.Body.Bytes())}
		}(i)
	}
	close(begin)
	winner := -1
	for i := 0; i < 2; i++ {
		got := <-results
		switch got.status {
		case 201:
			if winner != -1 {
				t.Fatal("duplicate admitted run")
			}
			winner = got.index
		case 409:
		default:
			t.Fatal(got.status, string(got.body))
		}
	}
	if winner == -1 {
		t.Fatal("no admission won")
	}
	var execution *assistantRunExecution
	select {
	case execution = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("admitted execution did not start")
	}
	defer execution.cancel()
	// A structurally equal decoded selector preserves the original Start digest.
	retry := starts[winner]
	selector := *retry.Context
	retry.Context = &selector
	if w := policyCall(s, token, "POST", "/api/assistant-runs", retry); w.Code != 200 {
		t.Fatal("exact admission did not reconcile", w.Code, w.Body.String())
	}
	if executor.calls.Load() != 1 {
		t.Fatal("idempotency replayed execution")
	}
}

// Exercise the HTTP-installed authority watchers without pretending that a
// model-free route owns the pending/run settlement lifecycle supplied by C1-D.
func TestAssistantContextHTTPAuthorityRetirement(t *testing.T) {
	for _, mode := range []string{"session", "policy", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			s, token, _, executor := assistantProtocolFixture(t, nil)
			doc := assistantContextDocumentFixture(t, s, token, "watched-document")
			r := httptest.NewRequest("POST", "/api/assistant-contexts/documents", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			binding, _, err := s.assistantRunIdentity(r)
			assistantRunMust(t, err)
			binding.Project = s.assistantRuns().project
			binding.Owner = assistantRunOwner{Kind: "chat", ID: "chat-one", Revision: strings.Repeat("a", 64)}
			binding.RequestID = "owned-request"
			registry := s.assistantRunService().contexts.registry
			selector := assistantRunContextSelector{Document: doc.Document}
			pending, err := registry.retain(binding, selector, "chat", "chat-one")
			assistantRunMust(t, err)
			defer pending.Release()
			assistantRunMust(t, registry.bindRun(pending, "retained-run"))
			defer registry.settleRun("retained-run")
			original := pending.Chat()
			switch mode {
			case "session":
				s.sessions.revoke(token)
			case "policy":
				s.signIn.mu.Lock()
				s.signIn.cancelEpoch()
				s.signIn.epoch, s.signIn.cancelEpoch = context.WithCancel(context.Background())
				s.signIn.generation++
				s.signIn.mu.Unlock()
			case "shutdown":
				s.assistantRunService().close()
			}
			deadline := time.NewTimer(3 * time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(time.Millisecond)
			defer tick.Stop()
			for {
				registry.mu.Lock()
				retired := registry.documents[doc.Document] == nil
				held, retained := registry.runs["retained-run"]
				references := pending.entry.refs
				registry.mu.Unlock()
				if retired {
					if !retained || held.entry.chat != original || references != 2 {
						t.Fatal("authority retirement settled separately held work")
					}
					break
				}
				select {
				case <-deadline.C:
					t.Fatal("authority watcher did not retire reuse")
				case <-tick.C:
				}
			}
			if _, err := registry.retain(binding, selector, "chat", "chat-one"); err == nil {
				t.Fatal("retired document granted new reuse")
			}
			if executor.calls.Load() != 0 {
				t.Fatal("lifecycle route dispatched executor")
			}
		})
	}
}
