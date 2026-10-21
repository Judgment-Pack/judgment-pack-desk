package desk

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Fixtures are authored here; the production canonicalizer/verifier checks
// independent receipt signatures, never captured output or live providers.
func assistantSearchSigned(t *testing.T, session string, q assistantSearchRequest, change func(map[string]any)) ([]byte, []byte) {
	t.Helper()
	result := map[string]any{"version": 1, "connection": q.Connection, "revision": q.Revision, "query": q.Query, "provider": "tavily", "retrievedAt": "2026-10-21T00:00:00Z", "kind": "search-results", "hits": []any{map[string]any{"title": "Public policy 😀", "url": "https://example.org:443/policy", "snippet": "A search lead, never page evidence."}}, "signedExtension": map[string]any{"retained": true}}
	if change != nil {
		change(result)
	}
	canonical := assistantDocumentFixtureJSON(t, result)
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	public := key.Public().(ed25519.PublicKey)
	keyID := assistantDocumentHash(public)[:32]
	committed := append(append(make([]byte, 32), []byte("args:")...), assistantDocumentFixtureJSON(t, q)...)
	receipt := map[string]any{"receiptVersion": "3", "kind": "acquisition", "sessionId": session, "callIndex": 0, "prevSignature": nil, "source": "web-search", "authority": "gateway:test", "keyId": keyID, "resultDigest": "sha256:" + assistantDocumentHash(canonical), "argumentsCommitment": "sha256:" + assistantDocumentHash(committed), "caller": nil, "servedAt": "2026-10-21T00:00:00Z", "acquisition": map[string]any{"shape": "http", "endpoint": nil, "adapter": map[string]any{"name": "fixture", "version": "1", "digest": "sha256:" + strings.Repeat("a", 64)}, "observedAt": "2026-10-21T00:00:00Z", "schema": nil, "snapshot": nil, "statement": nil, "peerIdentity": nil, "upstreamToken": nil}}
	receipt["signature"] = hex.EncodeToString(ed25519.Sign(key, append([]byte("judgment-pack-gateway/receipt/3:"), assistantDocumentFixtureJSON(t, receipt)...)))
	seal := map[string]any{"sessionId": session, "finalCount": 1, "sealedAt": "2026-10-21T00:00:00Z", "keyId": keyID}
	seal["signature"] = hex.EncodeToString(ed25519.Sign(key, append([]byte("judgment-pack-gateway/seal/2:"), assistantDocumentFixtureJSON(t, seal)...)))
	return assistantDocumentFixtureJSON(t, map[string]any{"receipt": receipt, "result": json.RawMessage(canonical), "salts": map[string]string{"args": strings.Repeat("0", 64)}}), assistantDocumentFixtureJSON(t, seal)
}

type assistantSearchFixture struct {
	s            *Server
	token        string
	e            *assistantRunExecution
	config       assistantSearchConfiguration
	calls        atomic.Int32
	acquisitions atomic.Int32
	seals        atomic.Int32
	before       func(string)
	after        func(string)
	resultChange func(map[string]any)
	statusPath   string
	mu           sync.Mutex
	responses    map[string][]byte
	trace        string
}

func assistantSearchBundle(t *testing.T) (string, string) {
	t.Helper()
	verifier := assistantDocumentProgram(t)
	python := os.Getenv("JPS_TEST_PYTHON")
	if python == "" {
		t.Skip("explicit fixture Python interpreter required")
	}
	if !filepath.IsAbs(python) {
		t.Fatal("fixture interpreter must be explicit")
	}
	bundle := t.TempDir()
	status := filepath.Join(t.TempDir(), "status.json")
	program := fmt.Sprintf("#!%s\nimport json,sys\nfor line in sys.stdin:\n q=json.loads(line)\n with open(%q) as f: result=json.load(f)\n print(json.dumps({'id':q['id'],'result':result}),flush=True)\n", python, status)
	files := map[string]string{}
	components := gatewayPlanFiles()
	components[executableName("gateway")] = ""
	components[executableName("gateway-connections")] = ""
	for name := range components {
		raw := []byte("fixture, never executed")
		if name == executableName("gateway") {
			var err error
			raw, err = os.ReadFile(verifier)
			assistantRunMust(t, err)
		}
		if name == executableName("gateway-connections") {
			raw = []byte(program)
		}
		assistantRunMust(t, os.WriteFile(filepath.Join(bundle, name), raw, 0700))
		files[name] = digestOf(raw)
	}
	manifest := assistantChatJSON(map[string]any{"revision": GatewayRevision, "files": files})
	assistantRunMust(t, os.WriteFile(filepath.Join(bundle, "gateway-bundle.json"), manifest, 0600))
	t.Setenv("JPACK_DESK_GATEWAY_MANIFEST_SHA256", digestOf(manifest))
	return bundle, status
}
func newAssistantSearchFixture(t *testing.T, engine, choice, phase string, upstreamURL string) *assistantSearchFixture {
	return newAssistantSearchFixtureWithChat(t, engine, choice, phase, upstreamURL, nil)
}
func newAssistantSearchFixtureWithChat(t *testing.T, engine, choice, phase string, upstreamURL string, configure func(map[string]any)) *assistantSearchFixture {
	t.Helper()
	f := &assistantSearchFixture{responses: map[string][]byte{}}
	bundle, status := assistantSearchBundle(t)
	f.statusPath = status
	revision := strings.Repeat("a", 64)
	assistantRunMust(t, os.WriteFile(status, assistantChatJSON(map[string]any{"version": 1, "connections": []any{map[string]any{"id": "demo", "revision": revision, "provider": "tavily"}}}), 0600))
	var old *assistantRunExecution
	trace := ""
	if engine == "api" {
		f.s, f.token, old = assistantRecipeBrowserFixture(t, upstreamURL)
	} else {
		f.s, f.token, old, _, trace = assistantCodexFixture(t, "normal", phase)
	}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(r.URL.Path, "/")
		f.calls.Add(1)
		f.mu.Lock()
		before, after, change := f.before, f.after, f.resultChange
		f.mu.Unlock()
		if before != nil {
			before(path)
		}
		var input struct {
			Session   string                 `json:"session"`
			Source    string                 `json:"source"`
			Arguments assistantSearchRequest `json:"arguments"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			t.Error("invalid source request")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if path == "acquire" {
			f.acquisitions.Add(1)
			if input.Source != "web-search" || input.Arguments.Connection != "demo" || input.Arguments.Revision != revision || input.Arguments.MaxResults != 5 {
				t.Error("host-only request binding changed", input)
			}
			response, seal := assistantSearchSigned(t, input.Session, input.Arguments, change)
			f.mu.Lock()
			f.responses[input.Session] = seal
			f.mu.Unlock()
			_, _ = w.Write(response)
		} else if path == "seal" {
			f.seals.Add(1)
			f.mu.Lock()
			seal := bytes.Clone(f.responses[input.Session])
			f.mu.Unlock()
			if len(seal) == 0 {
				t.Error("seal did not follow exact acquire")
			}
			_, _ = w.Write(seal)
		} else {
			t.Error("unexpected source operation", path)
			w.WriteHeader(404)
		}
		if after != nil {
			after(path)
		}
	}))
	t.Cleanup(gateway.Close)
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	public := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	done := make(chan struct{})
	f.s.cfg.LocalGatewayBundle = bundle
	f.s.localGateway = &localGateway{bundle: bundle, done: done, pin: &localGatewayPin{URL: gateway.URL, Authority: "gateway:test", Signer: localSigner{Algorithm: "ed25519", Public: public}}}
	t.Cleanup(func() { close(done) })
	writeDeskConfig(t, f.s, `{"deskConfigVersion":1}`)
	assistantRunMust(t, os.WriteFile(filepath.Join(f.s.cfg.ProjectDir, "jpack-search.json"), []byte(`{"version":1,"connection":"demo","mode":"auto"}`), 0600))
	var chat map[string]any
	assistantRunMust(t, json.Unmarshal(old.source.Original, &chat))
	for key, value := range map[string]any{"title": "Source search", "composer": "", "pinned": false, "archived": false, "updatedAt": "2026-10-21T00:00:00Z", "mode": "draft", "view": "chat", "searchConnection": "demo", "researchMode": "auto", "interaction": phase} {
		chat[key] = value
	}
	state := chat["checkpoint"].(map[string]any)["state"].(map[string]any)
	for _, row := range state["turns"].([]any) {
		turn := row.(map[string]any)
		if turn["id"] == old.source.Start.RequestID {
			if choice != "" {
				turn["researchChoice"] = choice
			}
		}
	}
	if configure != nil {
		configure(chat)
	}
	f.e = assistantChatResave(t, f.s, f.token, old, string(assistantDocumentFixtureJSON(t, chat)), 201)
	if engine == "codex" {
		fixture := filepath.Dir(trace)
		peer := strings.Replace(assistantExecutionCodexPeer, "else: tool(offered('get_schema'),{})", "else: tool(offered('search_sources'),{'query':'public policy'})", 1)
		assistantRunMust(t, os.WriteFile(filepath.Join(fixture, "peer.py"), []byte(peer), 0600))
		python := os.Getenv("JPS_TEST_PYTHON")
		// Replace the inherited peer wrapper's ambient interpreter before its first
		// launch. Every executed program is an explicit fixture supplied by this test.
		wrapper, err := os.ReadFile(filepath.Join(fixture, "codex-peer"))
		assistantRunMust(t, err)
		wrapper = bytes.Replace(wrapper, []byte("exec python3 "), []byte("exec '"+python+"' "), 1)
		assistantRunMust(t, os.WriteFile(filepath.Join(fixture, "codex-peer"), wrapper, 0700))
		data, err := f.s.openChatData()
		assistantRunMust(t, err)
		assistantRunMust(t, os.WriteFile(filepath.Join(fixture, "ledger-path"), []byte(filepath.Join(data.root.Name(), f.e.writer.lease.store.name)), 0600))
		assistantRunMust(t, data.root.Close())
	}
	f.trace = trace
	return f
}
func (f *assistantSearchFixture) invoke(t *testing.T, call string, root bool) runcontext.ToolResult {
	t.Helper()
	ctx := runcontext.WithCallID(t.Context(), call)
	if root {
		assistantPolicyEvent(t, f.e, call, assistantSearchTool, []byte(`{"query":"public policy"}`))
	}
	ctx = context.WithValue(ctx, assistantHostRootKey{}, root)
	registry, err := f.e.assistantSearchTools()
	assistantRunMust(t, err)
	result, err := registry.tools[0].Execute(ctx, json.RawMessage(`{"query":"public policy"}`))
	assistantRunMust(t, err)
	return result
}
func TestAssistantSearchURLIntersection(t *testing.T) {
	raw, err := os.ReadFile("testdata/assistant-search-url.json")
	assistantRunMust(t, err)
	var vectors []struct {
		Name    string
		URL     string
		Host    bool
		Browser bool
	}
	assistantRunMust(t, json.Unmarshal(raw, &vectors))
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			if assistantSearchURL(v.URL) != v.Browser {
				t.Fatal("current shared URL spelling reader differs from historical browser expectation", v.URL)
			}
		})
	}
}
func TestAssistantSearchArgumentsAndShape(t *testing.T) {
	valid := assistantSearchRequest{Connection: "demo", Revision: strings.Repeat("a", 64), Query: "query", MaxResults: 5}
	for _, raw := range []string{`null`, `[]`, `{"query":null}`, `{"query":"a","Query":"b"}`, `{"query":"a","query":"b"}`, `{"query":"a","connection":"secret"}`, `{"query":3}`, `{"query":""}`} {
		if _, err := readAssistantSearchQuery([]byte(raw)); err == nil {
			t.Fatal("invalid query accepted", raw)
		}
	}
	for _, n := range []int{999, 1000, 1001} {
		raw := assistantChatJSON(map[string]string{"query": "\ufeff" + strings.Repeat("😀", n) + " \ufeff"})
		q, err := readAssistantSearchQuery(raw)
		if (err == nil) != (n <= 1000) {
			t.Fatal("UTF16 query boundary", n, err)
		}
		if err == nil && runcontext.U16Length(q) != n*2 {
			t.Fatal("ECMAScript trim changed query")
		}
	}
	base := map[string]any{"version": 1, "connection": valid.Connection, "revision": valid.Revision, "query": valid.Query, "provider": "tavily", "kind": "search-results", "retrievedAt": "2026-10-21T00:00:00Z", "hits": []any{map[string]any{"title": strings.Repeat("😀", 256), "url": "https://example.org:443/x", "snippet": strings.Repeat("😀", 1000)}}, "extension": true}
	if _, err := readAssistantSearchResult(assistantChatJSON(base), valid); err != nil {
		t.Fatal(err)
	}
	base["retrievedAt"] = "2026-10-21"
	if _, err := readAssistantSearchResult(assistantChatJSON(base), valid); err == nil {
		t.Fatal("legacy Date.parse format accepted by private host")
	}
	base["retrievedAt"] = "2026-10-21T00:00:00Z"
	variants := map[string]func(map[string]any){"provider": func(v map[string]any) { v["provider"] = "Invalid" }, "date": func(v map[string]any) { v["retrievedAt"] = "not a date" }, "kind": func(v map[string]any) { v["kind"] = "answer" }, "duplicates": func(v map[string]any) { v["hits"] = []any{v["hits"].([]any)[0], v["hits"].([]any)[0]} }, "title": func(v map[string]any) { v["hits"].([]any)[0].(map[string]any)["title"] = strings.Repeat("😀", 257) }, "snippet": func(v map[string]any) {
		v["hits"].([]any)[0].(map[string]any)["snippet"] = strings.Repeat("😀", 1001)
	}, "url": func(v map[string]any) { v["hits"].([]any)[0].(map[string]any)["url"] = "https://user@example.org/" }, "generated": func(v map[string]any) { v["generatedAnswer"] = "not page evidence" }, "queries": func(v map[string]any) { v["queries"] = []any{strings.Repeat("😀", 1001)} }}
	for name, change := range variants {
		t.Run(name, func(t *testing.T) {
			var copy map[string]any
			assistantRunMust(t, json.Unmarshal(assistantChatJSON(base), &copy))
			change(copy)
			if _, err := readAssistantSearchResult(assistantChatJSON(copy), valid); err == nil {
				t.Fatal("invalid search result accepted")
			}
		})
	}
}
func TestAssistantSearchConsentZeroAcquisition(t *testing.T) {
	for _, choice := range []string{"ask", "provided", ""} {
		t.Run(choice, func(t *testing.T) {
			f := newAssistantSearchFixture(t, "api", choice, "work", "http://127.0.0.1:1")
			_ = assistantExecutionOpen(t, f.s, f.e)
			result := f.invoke(t, "search-consent", true)
			if f.calls.Load() != 0 || len(assistantExecutionRecord(t, f.e).Searches) != 0 {
				t.Fatal("consent caused acquisition")
			}
			if choice != "provided" && !strings.Contains(string(result.Value().JSON()), `"researchSuggestion":true`) {
				t.Fatal("pending suggestion lost", string(result.Value().JSON()))
			}
			if choice == "provided" && !result.Err() {
				t.Fatal("provided-only search succeeded")
			}
		})
	}
}
func TestAssistantSearchAcquisitionCustodyAndReplay(t *testing.T) {
	f := newAssistantSearchFixture(t, "api", "research", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	result := f.invoke(t, "search-one", true)
	if result.Err() {
		t.Fatal(string(result.Value().JSON()))
	}
	record := assistantExecutionRecord(t, f.e)
	if len(record.Searches) != 1 || len(record.DomainOutputs) != 1 || record.Searches[0].Final.Receipt == nil || f.acquisitions.Load() != 1 || f.seals.Load() != 1 {
		t.Fatal("search custody incomplete", record.Searches)
	}
	registry, err := f.e.assistantSearchTools()
	assistantRunMust(t, err)
	ctx := context.WithValue(runcontext.WithCallID(t.Context(), "search-one"), assistantHostRootKey{}, true)
	exact, err := registry.tools[0].Execute(ctx, json.RawMessage(`{"query":"public policy"}`))
	assistantRunMust(t, err)
	if !bytes.Equal(exact.Value().JSON(), result.Value().JSON()) || f.calls.Load() != 2 {
		t.Fatal("exact replay repeated source work")
	}
}

func TestAssistantSearchSharedTurnLimit(t *testing.T) {
	f := newAssistantSearchFixture(t, "api", "research", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	for i := 0; i < 3; i++ {
		result := f.invoke(t, fmt.Sprintf("search-%d", i), i != 1)
		if result.Err() {
			t.Fatal("shared search refused early", string(result.Value().JSON()))
		}
	}
	// A reconstructed registry and a retry have the same stable saved turn.
	result := f.invoke(t, "search-over-budget", true)
	if !result.Err() || f.acquisitions.Load() != 3 || f.seals.Load() != 3 || len(assistantExecutionRecord(t, f.e).Searches) != 3 {
		t.Fatal("root/child/retry limit reset", f.acquisitions.Load())
	}
}
func TestAssistantSearchQueuedConfiguration(t *testing.T) {
	for _, change := range []string{"gateway-url", "connection", "session", "profile", "source"} {
		t.Run(change, func(t *testing.T) {
			f := newAssistantSearchFixture(t, "api", "research", "work", "http://127.0.0.1:1")
			_ = assistantExecutionOpen(t, f.s, f.e)
			for range cap(f.s.researchSlots) {
				f.s.researchSlots <- struct{}{}
			}
			done := make(chan runcontext.ToolResult, 1)
			go func() { done <- f.invoke(t, "queued-search", true) }()
			deadline := time.Now().Add(3 * time.Second)
			for {
				record := assistantExecutionRecord(t, f.e)
				if len(record.Searches) == 1 && record.Searches[0].Acquire == "dispatched" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("search did not queue")
				}
				time.Sleep(5 * time.Millisecond)
			}
			switch change {
			case "gateway-url":
				f.s.localGateway.mu.Lock()
				pin := *f.s.localGateway.pin
				pin.URL = "http://127.0.0.1:2"
				f.s.localGateway.pin = &pin
				f.s.localGateway.mu.Unlock()
			case "connection":
				assistantRunMust(t, os.WriteFile(f.statusPath, assistantChatJSON(map[string]any{"version": 1, "connections": []any{map[string]any{"id": "demo", "revision": strings.Repeat("b", 64), "provider": "tavily"}}}), 0600))
			case "session":
				f.s.sessions.revoke(bearerOf(f.e.request))
			case "profile":
				assistantRunMust(t, os.WriteFile(filepath.Join(f.s.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":1,"api":{"inherit":true},"projectInstructions":"Changed"}`), 0600))
			case "source":
				saved := policyWorkspaceRead(t, f.s, f.token, "/api/conversations")
				var doc conversationDocument
				assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
				doc.Chats[0] = bytes.Replace(doc.Chats[0], []byte("Source search"), []byte("Changed source"), 1)
				w := policyWorkspaceCall(f.s, f.token, "PUT", "/api/conversations", saved.SHA256, doc)
				if w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			}
			<-f.s.researchSlots
			result := <-done
			for len(f.s.researchSlots) > 0 {
				<-f.s.researchSlots
			}
			if !result.Err() || f.calls.Load() != 0 {
				t.Fatal("queued changed authority dispatched", f.calls.Load(), string(result.Value().JSON()))
			}
		})
	}
}
func TestAssistantSearchCancelAfterAcquisitionRetainsStage(t *testing.T) {
	f := newAssistantSearchFixture(t, "api", "research", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	f.after = func(path string) {
		if path == "acquire" {
			f.e.cancel()
		}
	}
	result := f.invoke(t, "cancelled-search", true)
	if !result.Err() || f.acquisitions.Load() != 1 || f.seals.Load() != 0 {
		t.Fatal("cancelled acquisition continued", f.calls.Load())
	}
	record := assistantExecutionRecord(t, f.e)
	if len(record.Searches) != 1 || record.Searches[0].Stage.Receipt == nil || record.Searches[0].Final.Intent != nil || len(record.DomainOutputs) != 0 {
		t.Fatal("completed acquisition custody was lost", record.Searches)
	}
	// Repeated authenticated reads may reconcile receipts only. They never repeat
	// acquisition or sealing and never manufacture a verified reference.
	for range 2 {
		if _, err := f.s.assistantRunService().read(f.e.request.WithContext(t.Context()), record.ID); err != nil {
			t.Fatal(err)
		}
	}
	if f.calls.Load() != 1 {
		t.Fatal("GET repeated cancelled source")
	}
}
func TestAssistantSearchRegistryAndPlan(t *testing.T) {
	f := newAssistantSearchFixture(t, "api", "research", "plan", "http://127.0.0.1:1")
	x := assistantExecutionOpen(t, f.s, f.e)
	tool := assistantGraphFind(t, x.Run.HostTools, assistantSearchTool)
	if tool.Effect != "read" || tool.Delegation != "research" || tool.ContextPolicy != nil || tool.Source != nil || tool.Approve != nil {
		t.Fatal("search classification widened")
	}
	for _, mutate := range []func(*runcontext.HostTool){func(t *runcontext.HostTool) { t.Effect = "workflow" }, func(t *runcontext.HostTool) { t.Delegation = "read" }, func(t *runcontext.HostTool) { t.ContextPolicy = &runcontext.ToolContextPolicy{Immutable: true} }} {
		copy := tool
		mutate(&copy)
		if _, err := newAssistantHostRegistry([]runcontext.HostTool{copy}); err == nil {
			t.Fatal("search declaration widened")
		}
	}
	if result := f.invoke(t, "plan-search", true); result.Err() {
		t.Fatal("consented Plan read refused", string(result.Value().JSON()))
	}
}

func TestAssistantSearchActualProducersAndProjection(t *testing.T) {
	for _, engine := range []string{"api", "codex", "research-child"} {
		t.Run(engine, func(t *testing.T) {
			var requests atomic.Int32
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				finish := "stop"
				delta := map[string]any{"role": "assistant", "content": "Search found leads only; no pages were read."}
				if n == 1 {
					finish = "tool_calls"
					delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "search-call", "type": "function", "function": map[string]any{"name": "search_sources", "arguments": `{"query":"public policy"}`}}}}
				}
				raw := assistantChatJSON(map[string]any{"id": "search-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"search-fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", raw, finish)
			})
			actual := engine
			if actual == "research-child" {
				actual = "codex"
			}
			f := newAssistantSearchFixture(t, actual, "research", "work", u.server.URL)
			if engine == "research-child" {
				peer := strings.Replace(assistantExecutionCodexPeer, "if mode=='delegate' and index==1:\n   tool(offered('delegate_tasks'),{'tasks':[{'title':'Inspect schema','objective':'Inspect the schema only.','profile':'review'}]})", `if mode=='normal' and index==1: tool(offered('delegate_tasks'),{'tasks':[{'title':'Find public leads','objective':'Search public policy. Search leads are not page evidence.','profile':'research'}]})
  elif mode=='normal' and index==2: tool(offered('search_sources'),{'query':'public policy'})
  elif mode=='normal' and index>=3: finish()`, 1)
				peer = strings.Replace(peer, "if mode=='delegate' and index==2:", "if mode=='normal' and index==2:", 1)
				// A research report may have no findings/citations when only leads were
				// searched. It explicitly records that no source page was read.
				assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(f.trace), "peer.py"), []byte(peer), 0600))
			}
			x := assistantExecutionOpen(t, f.s, f.e)
			off := false
			x.Run.Review = &off
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
			record := assistantExecutionRecord(t, f.e)
			if len(record.DomainOutputs) != 1 || len(record.Searches) != 1 || f.acquisitions.Load() != 1 || f.seals.Load() != 1 {
				t.Fatal("actual source producer did not retain verified search", len(record.DomainOutputs), record.Searches)
			}
			var host assistantHostIntent
			assistantRunMust(t, readAssistantHostReceipt(assistantDialectOperation(record, record.Searches[0].HostOperationID).Intent, &host))
			if engine == "research-child" && host.Root {
				t.Fatal("research child was mislabeled root")
			}
			if engine != "api" && host.CallbackID == "" {
				t.Fatal("native source callback custody missing")
			}
			assistantRunMust(t, assistantRecipeImport(t, f.e, record))
			assistantRunMust(t, f.e.writer.State(t.Context(), "complete"))
			before := policyWorkspaceRead(t, f.s, f.token, "/api/conversations")
			assistantRunMust(t, f.e.ProjectChat(t.Context()))
			assistantRunMust(t, f.e.ProjectChat(t.Context()))
			saved := policyWorkspaceRead(t, f.s, f.token, "/api/conversations")
			if bytes.Equal(saved.Content, before.Content) || f.calls.Load() != 2 {
				t.Fatal("projection lost output or repeated acquisition")
			}
			if dir := os.Getenv("DESK_SEARCH_READER_OUTPUTS"); dir != "" {
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, engine+".json"), saved.Content, 0600))
				data, err := f.s.openChatData()
				assistantRunMust(t, err)
				name, err := resolveConversationName(data.root, f.s.conversationIdentity())
				assistantRunMust(t, err)
				raw, err := readPrivateData(data.root, attachmentProjectPrefix(name)+record.Searches[0].FinalID+".json", maxAttachmentObjectBytes)
				assistantRunMust(t, err)
				assistantRunMust(t, data.root.Close())
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, engine+"-object.json"), raw, 0600))
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, engine+"-pin.json"), assistantChatJSON(map[string]any{"url": record.Searches[0].Gateway.URL, "authority": record.Searches[0].Gateway.Authority, "signer": map[string]string{"algorithm": "ed25519", "public": record.Searches[0].Gateway.Public}}), 0600))
			}
		})
	}
}

// Exercise malformed input through the real producer so refusal events, rather
// than only parser errors, remain available to the pinned schema self-check.
func TestAssistantSearchActualRefusalEvents(t *testing.T) {
	for _, vector := range []struct {
		name, arguments, choice string
		noConfig                bool
	}{
		{"empty", `{"query":""}`, "research", false},
		{"missing", `{}`, "research", false},
		{"oversized", string(assistantChatJSON(map[string]string{"query": strings.Repeat("😀", 1001)})), "research", false},
		{"undeclared", `{"query":"public policy","connection":"other"}`, "research", false},
		{"provided", `{"query":"public policy"}`, "provided", false},
		{"no-config", `{"query":"public policy"}`, "research", true},
	} {
		t.Run(vector.name, func(t *testing.T) {
			var requests atomic.Int32
			upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				finish := "stop"
				delta := map[string]any{"role": "assistant", "content": "Search was refused."}
				if requests.Add(1) == 1 {
					finish = "tool_calls"
					delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "refused-search", "type": "function", "function": map[string]any{"name": "search_sources", "arguments": vector.arguments}}}}
				}
				raw := assistantChatJSON(map[string]any{"id": "refusal-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"refusal-fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", raw, finish)
			})
			f := newAssistantSearchFixture(t, "api", vector.choice, "work", upstream.server.URL)
			if vector.noConfig {
				assistantRunMust(t, os.WriteFile(f.statusPath, assistantChatJSON(map[string]any{"version": 1, "connections": []any{}}), 0600))
			}
			x := assistantExecutionOpen(t, f.s, f.e)
			off := false
			x.Run.Review = &off
			assistantRunMust(t, x.Adapter.Run(t.Context(), x.Run))
			record := assistantExecutionRecord(t, f.e)
			refused := false
			events := []json.RawMessage{}
			for _, held := range record.Events {
				raw, err := assistantRunEventBytes(held.Event)
				assistantRunMust(t, err)
				events = append(events, raw)
				if result := held.Event.ToolResult; result != nil && result.Name == assistantSearchTool {
					refused = result.IsError
				}
			}
			if !refused || f.calls.Load() != 0 || len(record.Searches) != 0 || len(record.DomainOutputs) != 0 {
				t.Fatal("refusal did not retain a zero-acquisition error event", refused, f.calls.Load(), record.Searches)
			}
			if dir := os.Getenv("DESK_SEARCH_READER_OUTPUTS"); dir != "" {
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, "refusal-"+vector.name+".json"), assistantChatJSON(events), 0600))
			}
		})
	}
}

func TestAssistantSearchRetainedChatLimit(t *testing.T) {
	f := newAssistantSearchFixture(t, "api", "research", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	template := assistantExecutionRecord(t, f.e)
	_, err := f.e.writer.lease.store.transaction(t.Context(), func(ledger *assistantRunLedger) (bool, error) {
		for i := 0; i < 64; i++ {
			copy := template
			copy.ID = fmt.Sprintf("arun_%032x", i+1)
			copy.AdmissionID = fmt.Sprintf("search-history-%d", i)
			copy.Binding.RequestID = fmt.Sprintf("old-turn-%d", i)
			copy.State = "interrupted"
			copy.Historical = true
			cp := *template.Checkpoint
			cp.Status = "stopped"
			copy.Checkpoint = &cp
			in := assistantHostIntent{CallID: "historical-call", Root: false, Arguments: []byte(`{"query":"public policy"}`)}
			copy.Events = nil
			copy.DomainOutputs = nil
			copy.Operations = []assistantRunOperation{{ID: "historical-search", CallID: "historical-search", Kind: "host", Name: assistantSearchTool, Intent: assistantChatJSON(in), State: "refused", Result: assistantChatJSON(assistantHostResult{Result: assistantSearchError(assistantSearchFailure("search-failed", "tavily"), assistantSearchStep{Query: "public policy"}).Value().JSON()})}}
			uuid := func(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012x", n) }
			copy.Searches = []assistantSearchCustody{{ConfigurationDigest: strings.Repeat("a", 64), HostOperationID: "historical-search", TurnID: copy.Binding.RequestID, RequestDigest: copy.Binding.RequestDigest, Request: assistantSearchRequest{Connection: "demo", Revision: strings.Repeat("a", 64), Query: "public policy", MaxResults: 5}, Gateway: assistantSearchGateway{URL: f.s.localGateway.pin.URL, Authority: "gateway:test", Public: f.s.localGateway.pin.Signer.Public}, Provider: "tavily", Session: fmt.Sprintf("search-history-%d", i), StageID: uuid(i*3 + 1), SealID: uuid(i*3 + 2), FinalID: uuid(i*3 + 3), Acquire: "reserved", Seal: "not-started"}}
			ledger.Runs = append(ledger.Runs, copy)
		}
		return true, nil
	})
	assistantRunMust(t, err)
	result := f.invoke(t, "chat-limit", true)
	if !result.Err() || f.calls.Load() != 0 || len(assistantExecutionRecord(t, f.e).Searches) != 0 {
		t.Fatal("retained64 limit admitted source IO")
	}
}
func TestAssistantSearchCancelledSealAndChangedResponse(t *testing.T) {
	for _, mode := range []string{"seal-cancel", "pin-after-acquire", "invalid-shape"} {
		t.Run(mode, func(t *testing.T) {
			f := newAssistantSearchFixture(t, "api", "research", "work", "http://127.0.0.1:1")
			_ = assistantExecutionOpen(t, f.s, f.e)
			if mode == "invalid-shape" {
				f.resultChange = func(result map[string]any) { result["provider"] = "INVALID" }
			} else {
				f.after = func(path string) {
					if mode == "seal-cancel" && path == "seal" {
						f.e.cancel()
					}
					if mode == "pin-after-acquire" && path == "acquire" {
						f.s.localGateway.mu.Lock()
						pin := *f.s.localGateway.pin
						pin.URL = "http://127.0.0.1:2"
						f.s.localGateway.pin = &pin
						f.s.localGateway.mu.Unlock()
					}
				}
			}
			result := f.invoke(t, "changed-result", true)
			if !result.Err() {
				t.Fatal("unverified result authorized links")
			}
			record := assistantExecutionRecord(t, f.e)
			if len(record.Searches) != 1 || record.Searches[0].Stage.Receipt == nil || len(record.DomainOutputs) != 0 {
				t.Fatal("unverified acquisition custody lost")
			}
			if mode == "seal-cancel" {
				if f.calls.Load() != 2 || record.Searches[0].SealStage.Receipt == nil || record.Searches[0].Final.Intent != nil {
					t.Fatal("completed cancelled seal lost or continued")
				}
			} else if f.calls.Load() != 1 {
				t.Fatal("invalid result continued sealing")
			}
		})
	}
}
func TestAssistantSearchLostStorageAcknowledgement(t *testing.T) {
	for _, stage := range []string{"stage", "final"} {
		t.Run(stage, func(t *testing.T) {
			f := newAssistantSearchFixture(t, "api", "research", "work", "http://127.0.0.1:1")
			_ = assistantExecutionOpen(t, f.s, f.e)
			failed := false
			testHookPrivateRead = func(name string) error {
				if failed || !attachmentFileName.MatchString(name) {
					return nil
				}
				// This hook is reached only by post-write readback, never the initial
				// missing-object CAS. Inspect the exact immutable original name locally.
				data, err := f.s.openChatData()
				if err != nil {
					return err
				}
				defer data.root.Close()
				file, err := data.root.Open(name)
				if err != nil {
					return nil
				}
				raw, err := io.ReadAll(file)
				_ = file.Close()
				if err != nil {
					return err
				}
				var object attachmentObject
				if json.Unmarshal(raw, &object) != nil {
					return nil
				}
				matches := stage == "stage" && object.Original.Name == "chat-search-acquisition.json" || stage == "final" && object.Proof != nil
				if matches {
					failed = true
					return errors.New("fixture lost exact storage readback acknowledgement")
				}
				return nil
			}
			t.Cleanup(func() { testHookPrivateRead = nil })
			result := f.invoke(t, "uncertain-storage", true)
			testHookPrivateRead = nil
			if !failed || !result.Err() {
				t.Fatal("storage uncertainty was not observed")
			}
			before := assistantExecutionRecord(t, f.e)
			if len(before.Searches) != 1 || !before.Searches[0].DispatchClosed {
				t.Fatal("uncertain storage kept dispatch open")
			}
			<-f.e.done
			beforeGet := assistantExecutionRecord(t, f.e)
			data, readErr := f.s.openChatData()
			assistantRunMust(t, readErr)
			ledgerBefore, readErr := readPrivateData(data.root, f.e.writer.lease.store.name, assistantRunRecordLimit)
			assistantRunMust(t, readErr)
			f.s.localGateway.mu.Lock()
			f.s.localGateway.closed = true
			f.s.localGateway.mu.Unlock()
			for range 2 {
				_, err := f.s.assistantRunService().read(f.e.request.WithContext(t.Context()), before.ID)
				assistantRunMust(t, err)
			}
			f.s.localGateway.mu.Lock()
			f.s.localGateway.closed = false
			f.s.localGateway.mu.Unlock()
			afterGet := assistantExecutionRecord(t, f.e)
			ledgerAfter, readErr := readPrivateData(data.root, f.e.writer.lease.store.name, assistantRunRecordLimit)
			assistantRunMust(t, readErr)
			assistantRunMust(t, data.root.Close())
			if beforeGet.Revision != afterGet.Revision || !bytes.Equal(ledgerBefore, ledgerAfter) {
				t.Fatal("GET changed terminal uncertain ledger or depended on Gateway readiness")
			}
			expected := int32(1)
			if stage == "final" {
				expected = 2
			}
			if f.calls.Load() != expected {
				t.Fatal("readback observation repeated external work")
			}
			hostBefore := bytes.Clone(assistantDialectOperation(beforeGet, beforeGet.Searches[0].HostOperationID).Result)
			assistantRunMust(t, f.e.ProjectChat(t.Context()))
			recovered := assistantExecutionRecord(t, f.e)
			if stage == "final" {
				if recovered.Searches[0].Final.Receipt == nil || len(recovered.DomainOutputs) != 1 || !recovered.DomainOutputs[0].Search.Recovered || !bytes.Equal(hostBefore, assistantDialectOperation(recovered, beforeGet.Searches[0].HostOperationID).Result) {
					t.Fatal("exact final recovery changed original failure or lost custody")
				}
			} else if recovered.Searches[0].Stage.Receipt == nil || len(recovered.DomainOutputs) != 0 {
				t.Fatal("unverified stage recovery authorized links or lost custody")
			}
			if f.calls.Load() != expected {
				t.Fatal("explicit receipt reconciliation repeated external work")
			}

		})
	}
}
func TestAssistantSearchNoStartSnapshot(t *testing.T) {
	f := newAssistantSearchFixture(t, "api", "research", "work", "http://127.0.0.1:1")
	current, err := f.s.configuredResearchCurrent(false)
	assistantRunMust(t, err)
	if current.url != f.s.localGateway.pin.URL || !current.managedLocal {
		t.Fatal("snapshot lost configured owner")
	}
	// Unavailable process facts refuse without attempting its deliberately absent
	// executable, changing process identity or initializing another store.
	f.s.localGateway.mu.Lock()
	done := f.s.localGateway.done
	f.s.localGateway.done = nil
	f.s.localGateway.mu.Unlock()
	_, err = f.s.configuredResearchCurrent(false)
	f.s.localGateway.mu.Lock()
	f.s.localGateway.done = done
	f.s.localGateway.mu.Unlock()
	if err == nil || f.calls.Load() != 0 {
		t.Fatal("no-start snapshot initialized or dispatched")
	}
}

type assistantSearchTransport func(*http.Request) (*http.Response, error)

func (f assistantSearchTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAssistantSearchUnknownExternalOutcome(t *testing.T) {
	for _, phase := range []string{"acquire", "seal"} {
		t.Run(phase, func(t *testing.T) {
			f := newAssistantSearchFixture(t, "api", "research", "work", "http://127.0.0.1:1")
			_ = assistantExecutionOpen(t, f.s, f.e)
			prior := relayTransport
			t.Cleanup(func() { relayTransport = prior })
			relayTransport = assistantSearchTransport(func(r *http.Request) (*http.Response, error) {
				transport := prior
				if transport == nil {
					transport = http.DefaultTransport
				}
				response, err := transport.RoundTrip(r)
				if err != nil {
					return nil, err
				}
				if strings.Trim(r.URL.Path, "/") == phase {
					_ = response.Body.Close()
					return nil, errors.New("fixture external acknowledgement lost")
				}
				return response, nil
			})
			result := f.invoke(t, "unknown-external", true)
			if !result.Err() {
				t.Fatal("unknown external outcome returned success")
			}
			record := assistantExecutionRecord(t, f.e)
			if len(record.Searches) != 1 || !record.Searches[0].DispatchClosed || len(record.DomainOutputs) != 0 {
				t.Fatal("unknown external outcome kept dispatch open")
			}
			expected := int32(1)
			if phase == "seal" {
				expected = 2
				if record.Searches[0].Original.Receipt == nil {
					t.Fatal("unknown seal lost original")
				}
			}
			for range 2 {
				_, err := f.s.assistantRunService().read(f.e.request.WithContext(t.Context()), record.ID)
				assistantRunMust(t, err)
			}
			if f.calls.Load() != expected {
				t.Fatal("unknown source outcome repeated automatically")
			}
		})
	}
}
func TestAssistantSearchExactProofAssociation(t *testing.T) {
	f := newAssistantSearchFixture(t, "api", "research", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	if result := f.invoke(t, "proof-search", true); result.Err() {
		t.Fatal(result.Value().Text())
	}
	held := assistantExecutionRecord(t, f.e).Searches[0]
	data, err := f.s.openChatData()
	assistantRunMust(t, err)
	name, err := resolveConversationName(data.root, f.s.conversationIdentity())
	assistantRunMust(t, err)
	raw, err := readPrivateData(data.root, attachmentProjectPrefix(name)+held.FinalID+".json", maxAttachmentObjectBytes)
	assistantRunMust(t, err)
	assistantRunMust(t, data.root.Close())
	program, err := f.s.researchVerifier()
	assistantRunMust(t, err)
	if _, err = verifyAssistantSearchObject(t.Context(), program, raw, held); err != nil {
		t.Fatal("baseline final proof failed", err)
	}
	for _, variant := range []string{"request", "pin", "session", "source", "index", "seal", "original", "stage", "result"} {
		t.Run(variant, func(t *testing.T) {
			copy := held
			intent := *held.Final.Intent
			copy.Final.Intent = &intent
			var object attachmentObject
			assistantRunMust(t, json.Unmarshal(raw, &object))
			switch variant {
			case "request":
				copy.Request.Query = "other query"
			case "pin":
				copy.Gateway.Public = strings.Repeat("b", 64)
			case "session":
				object.Proof.Session = "another-session"
			case "source":
				object.Proof.Source = "web"
			case "stage":
				receipt := *copy.Stage.Receipt
				receipt.SHA256 = strings.Repeat("b", 64)
				copy.Stage.Receipt = &receipt
			case "original":
				original := []byte(`{"different":"canonical result"}`)
				object.Original.Bytes = base64.StdEncoding.EncodeToString(original)
				object.Original.SHA256 = "sha256:" + digestOf(original)
			case "index", "result":
				var env map[string]any
				assistantRunMust(t, json.Unmarshal([]byte(object.Proof.Response), &env))
				if variant == "index" {
					env["receipt"].(map[string]any)["callIndex"] = 1
				} else {
					env["result"].(map[string]any)["hits"] = []any{}
				}
				object.Proof.Response = string(assistantDocumentFixtureJSON(t, env))
			case "seal":
				var seal map[string]any
				assistantRunMust(t, json.Unmarshal([]byte(object.Proof.Registry), &seal))
				seal["finalCount"] = 2
				object.Proof.Registry = string(assistantDocumentFixtureJSON(t, seal)) + "\n"
			}
			changed := assistantChatJSON(object)
			intent.SHA256 = digestOf(changed)
			if _, err = verifyAssistantSearchObject(t.Context(), program, changed, copy); err == nil {
				t.Fatal("substituted exact proof association accepted", variant)
			}
		})
	}
	if assistantDocumentReferenced(f.e.source.Original, assistantDocumentReference{ID: held.FinalID, Digest: held.Digest, Pages: []int{1}}) {
		t.Fatal("search custody widened arbitrary saved document access")
	}
}
