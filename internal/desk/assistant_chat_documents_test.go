package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
)

func assistantChatStoreDocument(t *testing.T, s *Server, ref assistantDocumentReference, raw []byte) {
	t.Helper()
	lock, err := s.privateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	defer lock.Close()
	store, err := s.openChatData()
	assistantRunMust(t, err)
	defer store.root.Close()
	name, err := resolveConversationName(store.root, s.conversationIdentity())
	assistantRunMust(t, err)
	assistantRunMust(t, writePrivateData(store.root, attachmentProjectPrefix(name)+ref.ID+".json", raw))
}

func assistantChatDocumentFixture(t *testing.T, engine, mode string, raw []byte, pin inputResearchPin, refs []assistantDocumentReference) (*Server, string, *assistantRunExecution, *upstream, string) {
	t.Helper()
	program := assistantDocumentProgram(t)
	bundle := os.Getenv("JPS_TEST_DOCUMENT_BUNDLE")
	if bundle == "" {
		bundle = os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	}
	if bundle == "" {
		t.Skip("explicit pinned Gateway bundle required")
	}
	if filepath.Join(bundle, executableName("gateway")) != program {
		t.Fatal("fixture verifier/bundle mismatch")
	}
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Fixture answer.\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	})
	var s *Server
	var token, trace string
	var old *assistantRunExecution
	if engine == "api" {
		s, token, old = assistantExecutionFixture(t, u.server.URL, "work")
	} else {
		s, token, old, _, trace = assistantCodexFixture(t, mode, "work")
	}
	s.cfg.LocalGatewayBundle = bundle
	writeDeskConfig(t, s, strings.Replace(researchDeskFile("http://127.0.0.1:9"), testSignerPublic, pin.Public, 1))
	assistantChatStoreDocument(t, s, refs[0], raw)
	var chat map[string]any
	assistantRunMust(t, json.Unmarshal([]byte(assistantChatSaved), &chat))
	chat["aiConnection"] = testAIAlpha
	file := func(ref assistantDocumentReference) map[string]any {
		return map[string]any{"id": ref.ID, "name": "saved-name-is-not-authority.txt", "text": "", "document": ref}
	}
	// Composer/saved metadata can contain plausible references and forged text;
	// only exact per-turn submissions nominate verification and readable pages.
	composer := refs[0]
	composer.Pages = []int{2}
	chat["documents"] = []any{map[string]any{"id": composer.ID, "name": "forged", "text": "FORGED-SAVED-TEXT", "document": composer}}
	state := chat["checkpoint"].(map[string]any)["state"].(map[string]any)
	turns := state["turns"].([]any)
	for _, row := range turns {
		t := row.(map[string]any)
		if t["id"] == "message-one" {
			t["attachments"] = []any{file(refs[len(refs)-1])}
		}
	}
	for i, ref := range refs[:len(refs)-1] {
		turns = append([]any{map[string]any{"id": "document-prior-" + string(rune('a'+i)), "role": "user", "kind": "message", "at": "2026-10-20T00:00:00Z", "text": "Earlier document selection", "attachments": []any{file(ref)}}}, turns...)
	}
	state["turns"] = turns
	e := assistantChatResave(t, s, token, old, string(assistantDocumentFixtureJSON(t, chat)), 201)
	return s, token, e, u, trace
}

func TestAssistantChatDocumentFactory(t *testing.T) {
	for _, engine := range []string{"api", "codex"} {
		t.Run(engine, func(t *testing.T) {
			raw, pin, ref := assistantDocumentFixture(t, "web", "http", nil)
			s, token, e, u, trace := assistantChatDocumentFixture(t, engine, "normal", raw, pin, []assistantDocumentReference{ref})
			before := policyWorkspaceRead(t, s, token, "/api/conversations")
			x := assistantExecutionOpen(t, s, e)
			found := false
			for _, document := range x.Run.Context.Documents {
				if document.Revision != ref.Digest {
					continue
				}
				found = true
				if document.Label != "Attached document: report.txt" || !strings.Contains(document.Value.Text(), "Selected first page.") || !strings.Contains(document.Value.Text(), "attachment:"+ref.ID+"/"+ref.Digest+"/page/1") || strings.Contains(document.Value.Text(), "Private second") || strings.Contains(document.Value.Text(), "FORGED-SAVED-TEXT") {
					t.Fatal("unsafe verified document context", document)
				}
			}
			if !found {
				t.Fatal("verified selection missing from canonical input")
			}
			off := false
			x.Run.Review = &off
			assistantRunMust(t, x.Adapter.Run(t.Context(), x.Run))
			wire := ""
			if engine == "api" {
				wire = string(u.only(t).body)
			} else {
				for _, row := range assistantCodexLog(t, trace) {
					if string(row["type"]) == `"generation"` {
						assistantRunMust(t, json.Unmarshal(row["prompt"], &wire))
					}
				}
			}
			if strings.Count(wire, "EXACT-SUBMITTED-INPUT") != 1 || strings.Contains(wire, "Private second") || strings.Contains(wire, "FORGED-SAVED-TEXT") {
				t.Fatal("opening projection duplicated input or exposed unselected text", wire)
			}
			checkpoint, err := json.Marshal(assistantExecutionRecord(t, e).Checkpoint)
			assistantRunMust(t, err)
			if !bytes.Contains(checkpoint, []byte("Selected first page.")) || bytes.Contains(checkpoint, []byte("Private second")) || bytes.Contains(checkpoint, []byte("FORGED-SAVED-TEXT")) {
				t.Fatal("canonical retained context lost page custody")
			}
			after := policyWorkspaceRead(t, s, token, "/api/conversations")
			if before.SHA256 != after.SHA256 || !bytes.Equal(before.Content, after.Content) {
				t.Fatal("input integration wrote chat bytes")
			}
		})
	}
}

func TestAssistantChatDocumentSelectionsAndConsent(t *testing.T) {
	for _, variant := range []string{"repeat", "pages", "consent", "partial-no-upgrade", "revision"} {
		t.Run(variant, func(t *testing.T) {
			var change func(map[string]any)
			if variant == "partial-no-upgrade" {
				change = func(r map[string]any) {
					r["content"].(map[string]any)["truncated"] = true
					r["processing"].(map[string]any)["status"] = "partial"
				}
			}
			raw, pin, first := assistantDocumentFixture(t, "inline", "command", change)
			second := first
			switch variant {
			case "pages":
				second.Pages = []int{2}
			case "consent":
				second.AllowPartial = true
			case "partial-no-upgrade":
				first.AllowPartial = true
			case "revision":
				second.Digest = "sha256:" + strings.Repeat("a", 64)
			}
			s, _, e, _, _ := assistantChatDocumentFixture(t, "api", "normal", raw, pin, []assistantDocumentReference{first, second})
			assistantExecutionRuntimeFixture(t, s, e)
			x, err := e.openAssistantExecution(t.Context(), assistantExecutionPrompt{Name: "served"})
			if variant == "partial-no-upgrade" || variant == "revision" {
				if !errors.Is(err, errAssistantDocument) {
					t.Fatal("selection/consent/revision silently combined", err)
				}
				return
			}
			assistantRunMust(t, err)
			defer x.Close()
			values := map[string]string{}
			for _, document := range x.Run.Context.Documents {
				if document.Revision == first.Digest {
					values[document.ID] = document.Value.Text()
				}
			}
			want := 2
			if variant == "repeat" {
				want = 1
			}
			if len(values) != want {
				t.Fatal("selection IDs collided or repeat was not exact", values)
			}
			if variant == "pages" {
				for _, text := range values {
					if strings.Contains(text, "Selected first") && strings.Contains(text, "Private second") {
						t.Fatal("pages were unioned")
					}
				}
			}
		})
	}
}

func TestAssistantChatDocumentUnverified(t *testing.T) {
	raw, pin, ref := assistantDocumentFixture(t, "web", "http", nil)
	raw = bytes.Replace(raw, []byte("Selected first page."), []byte("Injected first page."), 1)
	s, _, e, u, _ := assistantChatDocumentFixture(t, "api", "normal", raw, pin, []assistantDocumentReference{ref})
	assistantExecutionRuntimeFixture(t, s, e)
	if x, err := e.openAssistantExecution(t.Context(), assistantExecutionPrompt{Name: "served"}); !errors.Is(err, errAssistantDocument) {
		if x != nil {
			x.Close()
		}
		t.Fatal("unverified reference admitted", err)
	}
	if len(u.arrivals()) != 0 {
		t.Fatal("unverified reference reached model")
	}
}

func assistantChatChangeDocument(t *testing.T, mode string, s *Server, token string, raw []byte, ref assistantDocumentReference) {
	t.Helper()
	switch mode {
	case "attachment":
		assistantChatStoreDocument(t, s, ref, append(bytes.Clone(raw), ' '))
	case "trust":
		writeDeskConfig(t, s, strings.Replace(researchDeskFile("http://127.0.0.1:9"), "gateway:test", "gateway:changed", 1))
	case "owner":
		saved := policyWorkspaceRead(t, s, token, "/api/conversations")
		var doc conversationDocument
		assistantRunMust(t, json.Unmarshal(bytes.ReplaceAll(saved.Content, []byte("EXACT-AUTHORED-REQUEST"), []byte("Changed owner request")), &doc))
		if w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestAssistantChatDocumentQueuedDispatch(t *testing.T) {
	for _, engine := range []string{"api", "codex"} {
		for _, mode := range []string{"attachment", "trust", "owner"} {
			t.Run(engine+"-"+mode, func(t *testing.T) {
				raw, pin, ref := assistantDocumentFixture(t, "web", "http", nil)
				s, token, e, u, trace := assistantChatDocumentFixture(t, engine, "discovery-wait", raw, pin, []assistantDocumentReference{ref})
				x := assistantExecutionOpen(t, s, e)
				done := make(chan error, 1)
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				joined := make(chan struct{})
				releaseSlots := func() {}
				defer func() {
					cancel()
					releaseSlots()
					select {
					case <-joined:
					case <-time.After(5 * time.Second):
						t.Error("queued fixture worker did not stop during cleanup")
					}
				}()
				if engine == "api" {
					releaseSlots = assistantFixtureRelayReservations(s.relaySlots)
					go func() {
						defer close(joined)
						r, err := x.Run.Model(ctx, "chat/completions", assistantExecutionRequest())
						if r != nil {
							_, _ = io.Copy(io.Discard, r.Body)
							_ = r.Body.Close()
						}
						done <- err
					}()
					deadline := time.Now().Add(3 * time.Second)
					for len(s.relayQueue) == 0 && time.Now().Before(deadline) {
						time.Sleep(time.Millisecond)
					}
					if len(s.relayQueue) == 0 {
						t.Fatal("real relay queue bypassed")
					}
				} else {
					go func() {
						defer close(joined)
						done <- x.Run.Agent.Run(ctx, assistantCodexRequest(), adapter.AgentCallbacks{Event: func(context.Context, adapter.AgentEvent) error { return nil }, Tool: func(context.Context, adapter.AgentToolCall) (adapter.AgentToolAnswer, error) {
							return adapter.AgentToolAnswer{}, nil
						}})
					}()
					assistantCodexWait(t, trace+".waiting")
				}
				assistantChatChangeDocument(t, mode, s, token, raw, ref)
				if engine == "api" {
					releaseSlots()
				} else {
					assistantRunMust(t, os.WriteFile(trace+".release", []byte("release"), 0600))
				}
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("changed input did not refuse")
				}
				if len(u.arrivals()) != 0 {
					t.Fatal("changed document/trust/owner crossed API dispatch")
				}
				if engine == "codex" {
					for _, row := range assistantCodexLog(t, trace) {
						if string(row["type"]) == `"generation"` {
							t.Fatal("changed input crossed native generation")
						}
					}
				}
				if mode != "owner" && e.ctx.Err() == nil {
					t.Fatal("changed document custody left root admission open")
				}
			})
		}
	}
}

func TestAssistantChatDocumentContinuation(t *testing.T) {
	for _, when := range []string{"before-child", "before-parent"} {
		t.Run(when, func(t *testing.T) {
			raw, pin, ref := assistantDocumentFixture(t, "web", "http", nil)
			s, _, e, _, trace := assistantChatDocumentFixture(t, "codex", "delegate", raw, pin, []assistantDocumentReference{ref})
			x := assistantExecutionOpen(t, s, e)
			off := false
			x.Run.Review = &off
			if when == "before-child" {
				original := x.Run.Children
				x.Run.Children = func(ctx context.Context, p contract.DelegatedTaskProfile, emit func(contract.RunEvent)) (*gate.Calls, []contract.ToolOfferRuntimeTool, error) {
					calls, tools, err := original(ctx, p, emit)
					assistantChatStoreDocument(t, s, ref, append(bytes.Clone(raw), ' '))
					return calls, tools, err
				}
			} else {
				original := x.Run.Agent.Run
				x.Run.Agent.Run = func(ctx context.Context, r adapter.AgentRequest, c adapter.AgentCallbacks) error {
					err := original(ctx, r, c)
					if r.Model == "child-model" && err == nil {
						assistantChatStoreDocument(t, s, ref, append(bytes.Clone(raw), ' '))
					}
					return err
				}
			}
			// The published coordinator can report a child/continuation failure
			// semantically and return nil. Root closure and physical generations
			// establish that no changed-input invocation continued.
			if err := x.Adapter.Run(t.Context(), x.Run); e.ctx.Err() == nil {
				t.Fatal("changed document continued through delegation", err)
			}
			generations := 0
			for _, row := range assistantCodexLog(t, trace) {
				if string(row["type"]) == `"generation"` {
					generations++
				}
			}
			want := 1
			if when == "before-parent" {
				want = 2
			}
			if generations != want {
				t.Fatal("continuation recheck missed physical invocation", generations, want)
			}
			completed := 0
			for _, op := range assistantExecutionRecord(t, e).Operations {
				if op.Kind == "model" && op.State == "completed" {
					completed++
				}
			}
			if completed != want {
				t.Fatal("earlier invocation receipts changed", completed, want)
			}
		})
	}
}
