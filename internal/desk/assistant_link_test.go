package desk

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func TestAssistantLinkURLParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/assistant-link-url.json")
	assistantRunMust(t, err)
	var vectors []struct {
		Name, Input, FetchURL, DisplayURL, Anchor, AnchorKind string
		Refused                                               bool
	}
	assistantRunMust(t, json.Unmarshal(raw, &vectors))
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			got, err := normalizeAssistantLink(v.Input)
			if (err != nil) != v.Refused {
				t.Fatal("normalization refusal differs", got, err)
			}
			if !v.Refused && got != (assistantNormalizedLink{v.FetchURL, v.DisplayURL, v.Anchor, v.AnchorKind}) {
				t.Fatal("normalization differs", got)
			}
		})
	}
	raw, err = os.ReadFile("testdata/assistant-link-extraction.json")
	assistantRunMust(t, err)
	var extracts []struct {
		Name, Text string
		Links      []string
	}
	assistantRunMust(t, json.Unmarshal(raw, &extracts))
	for _, v := range extracts {
		t.Run(v.Name, func(t *testing.T) {
			if got := extractAssistantLinks(v.Text); !reflect.DeepEqual(got, v.Links) {
				t.Fatal("extraction differs", got, v.Links)
			}
		})
	}
}
func TestAssistantLinkStrictArguments(t *testing.T) {
	if dir := os.Getenv("DESK_LINK_READER_OUTPUTS"); dir != "" {
		e := &assistantRunExecution{}
		registry, err := e.assistantLinkTools()
		assistantRunMust(t, err)
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, "input-schema.json"), registry.tools[0].InputSchema, 0600))
	}

	for _, raw := range []string{`null`, `[]`, `{}`, `{"url":null}`, `{"url":"https://example.org/","URL":"https://other.org/"}`, `{"url":"https://example.org/","url":"https://other.org/"}`, `{"url":"https://example.org/","offset":null}`, `{"url":"https://example.org/","offset":-1}`, `{"url":"https://example.org/","offset":1.5}`, `{"url":"https://example.org/","find":null}`, `{"url":"https://example.org/","find":"` + strings.Repeat("x", 201) + `"}`, `{"url":"https://example.org/` + strings.Repeat("x", 4096) + `"}`} {
		if _, err := readAssistantLinkArguments([]byte(raw)); err == nil {
			t.Fatal("malformed argument accepted", raw)
		}
	}
}
func assistantLinkSigned(t *testing.T, session string, q assistantLinkRequest, change func(map[string]any)) ([]byte, []byte) {
	t.Helper()
	raw, _, _ := assistantDocumentFixture(t, "web", "http", nil)
	var packet attachmentObject
	assistantRunMust(t, json.Unmarshal(raw, &packet))
	var env struct {
		Result map[string]any `json:"result"`
	}
	assistantRunMust(t, json.Unmarshal([]byte(packet.Proof.Response), &env))
	result := env.Result
	source := result["provenance"].(map[string]any)["source"].(map[string]any)
	source["url"], source["requestedUrl"] = q.URL, q.URL
	content := result["content"].(map[string]any)
	pages := content["pages"].([]any)
	texts := []string{"😀 alpha\u00a0beta cafe\u0301. K k K S s ſ.", "Second\ufeffpage alpha beta. Exact quote."}
	chars := 0
	for i, p := range pages {
		page := p.(map[string]any)
		page["text"], page["chars"] = texts[i], len([]rune(texts[i]))
		chars += len([]rune(texts[i]))
	}
	content["chars"] = chars
	if change != nil {
		change(result)
	}
	canonical := assistantDocumentFixtureJSON(t, result)
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	public := key.Public().(ed25519.PublicKey)
	keyID := assistantDocumentHash(public)[:32]
	committed := append(append(make([]byte, 32), []byte("args:")...), assistantDocumentFixtureJSON(t, q)...)
	receipt := map[string]any{"receiptVersion": "3", "kind": "acquisition", "sessionId": session, "callIndex": 0, "prevSignature": nil, "source": "web", "authority": "gateway:test", "keyId": keyID, "resultDigest": "sha256:" + assistantDocumentHash(canonical), "argumentsCommitment": "sha256:" + assistantDocumentHash(committed), "caller": nil, "servedAt": "2026-10-21T00:00:00Z", "acquisition": map[string]any{"shape": "http", "endpoint": nil, "adapter": map[string]any{"name": "fixture", "version": "1", "digest": "sha256:" + strings.Repeat("a", 64)}, "observedAt": "2026-10-21T00:00:00Z", "schema": nil, "snapshot": nil, "statement": nil, "peerIdentity": nil, "upstreamToken": nil}}
	receipt["signature"] = hex.EncodeToString(ed25519.Sign(key, append([]byte("judgment-pack-gateway/receipt/3:"), assistantDocumentFixtureJSON(t, receipt)...)))
	seal := map[string]any{"sessionId": session, "finalCount": 1, "sealedAt": "2026-10-21T00:00:00Z", "keyId": keyID}
	seal["signature"] = hex.EncodeToString(ed25519.Sign(key, append([]byte("judgment-pack-gateway/seal/2:"), assistantDocumentFixtureJSON(t, seal)...)))
	return assistantDocumentFixtureJSON(t, map[string]any{"receipt": receipt, "result": json.RawMessage(canonical), "salts": map[string]string{"args": strings.Repeat("0", 64)}}), assistantDocumentFixtureJSON(t, seal)
}
func newAssistantLinkFixture(t *testing.T, engine, choice, phase, upstream string, configure ...func(map[string]any)) *assistantSearchFixture {
	t.Helper()
	f := newAssistantSearchFixtureWithChat(t, engine, choice, phase, upstream, func(chat map[string]any) {
		state := chat["checkpoint"].(map[string]any)["state"].(map[string]any)
		for _, row := range state["turns"].([]any) {
			turn := row.(map[string]any)
			if turn["role"] == "user" {
				turn["text"] = "Read example.org/report#alpha-beta and https://éxample.org/über https://example.org/two https://example.org/three https://example.org/four"
			}
		}
		for _, edit := range configure {
			edit(chat)
		}
	})
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(r.URL.Path, "/")
		f.calls.Add(1)
		f.mu.Lock()
		before, after, change := f.before, f.after, f.resultChange
		f.mu.Unlock()
		if before != nil {
			before(path)
		}
		var in struct {
			Session, Source string
			Arguments       json.RawMessage
		}
		assistantRunMust(t, json.NewDecoder(r.Body).Decode(&in))
		w.Header().Set("Content-Type", "application/json")
		if path == "acquire" {
			f.acquisitions.Add(1)
			var response, seal []byte
			if in.Source == assistantWebsiteSource {
				var q assistantLinkRequest
				assistantRunMust(t, json.Unmarshal(in.Arguments, &q))
				response, seal = assistantWebsiteSigned(t, in.Session, q, change)
			} else if in.Source == "web-search" {
				var q assistantSearchRequest
				assistantRunMust(t, json.Unmarshal(in.Arguments, &q))
				response, seal = assistantSearchSigned(t, in.Session, q, change)
			} else {
				var q assistantLinkRequest
				assistantRunMust(t, json.Unmarshal(in.Arguments, &q))
				if in.Source != "web" || !adWebURL(q.URL) {
					t.Error("unbound source request", in)
				}
				response, seal = assistantLinkSigned(t, in.Session, q, change)
			}
			f.mu.Lock()
			f.responses[in.Session] = seal
			f.mu.Unlock()
			_, _ = w.Write(response)
		} else if path == "seal" {
			f.seals.Add(1)
			f.mu.Lock()
			seal := bytes.Clone(f.responses[in.Session])
			f.mu.Unlock()
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
	f.s.localGateway.mu.Lock()
	f.s.localGateway.pin.URL = gateway.URL
	f.s.localGateway.pin.documentProcessing = true
	f.s.localGateway.mu.Unlock()

	return f
}
func (f *assistantSearchFixture) invokeLink(t *testing.T, call string, root bool, raw string) runcontext.ToolResult {
	t.Helper()
	ctx := runcontext.WithCallID(t.Context(), call)
	if root {
		assistantPolicyEvent(t, f.e, call, assistantLinkTool, []byte(raw))
	}
	ctx = context.WithValue(ctx, assistantHostRootKey{}, root)
	tools, err := f.e.assistantLinkTools()
	assistantRunMust(t, err)
	result, err := tools.tools[0].Execute(ctx, json.RawMessage(raw))
	assistantRunMust(t, err)
	return result
}
func TestAssistantLinkSuppliedCustodyAndWindows(t *testing.T) {
	f := newAssistantLinkFixture(t, "api", "provided", "plan", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	result := f.invokeLink(t, "first", true, `{"url":"example.org/report#alpha-beta","find":"alpha beta"}`)
	if result.Err() {
		t.Fatal("supplied Plan read refused", string(result.JSON()))
	}
	run := assistantExecutionRecord(t, f.e)
	if len(run.Links) != 1 || len(run.DomainOutputs) != 1 || f.acquisitions.Load() != 1 || f.seals.Load() != 1 {
		t.Fatal("missing finite producer custody", run.Links)
	}
	held := run.Links[0]
	if held.Final.Receipt == nil || held.Stage.Receipt == nil || held.Original.Receipt == nil || held.SealStage.Receipt == nil {
		t.Fatal("missing exact receipt")
	}
	if assistantDocumentReferenced(f.e.source.Original, *run.DomainOutputs[0].Link.Attachment.Document) {
		t.Fatal("run custody weakened saved reference")
	}
	second := f.invokeLink(t, "second", false, `{"url":"https://example.org/report","offset":1}`)
	if second.Err() || f.calls.Load() != 2 || second.StructuredContent.Get("offset").Num() != 0 || !strings.Contains(second.Content[0].Text(), "😀 alpha") {
		t.Fatal("retained read charged or split the intact UTF16 character", string(second.JSON()), f.calls.Load())
	}
	if len(result.JSON()) > runcontext.Defaults.ToolResultBytes {
		t.Fatal("window exceeds shared context bytes")
	}
	assistantRunMust(t, assistantRecipeImport(t, f.e, assistantExecutionRecord(t, f.e)))
}
func TestAssistantLinkAuthorityAndBudget(t *testing.T) {
	f := newAssistantLinkFixture(t, "api", "provided", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	for _, url := range []string{"https://invented.org/", "https://example.org/nested", "https://example.org/assistant"} {
		if r := f.invokeLink(t, url, true, string(assistantChatJSON(map[string]any{"url": url}))); !r.Err() {
			t.Fatal("unprovided link acquired")
		}
	}
	if f.calls.Load() != 0 {
		t.Fatal("refusal acquired")
	}
	for i, url := range []string{"https://example.org/report", "https://example.org/two", "https://example.org/three"} {
		if r := f.invokeLink(t, fmt.Sprint("read-", i), i == 0, string(assistantChatJSON(map[string]any{"url": url}))); r.Err() {
			t.Fatal("permitted shared read refused", string(r.JSON()))
		}
	}
	if r := f.invokeLink(t, "fourth", true, `{"url":"https://example.org/four"}`); !r.Err() || f.acquisitions.Load() != 3 {
		t.Fatal("ordinary shared counter reset")
	}
}
func TestAssistantLinkActualProducersAndProjection(t *testing.T) {
	for _, engine := range []string{"api", "codex", "read-child"} {
		t.Run(engine, func(t *testing.T) {
			var requests atomic.Int32
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				finish := "stop"
				delta := map[string]any{"role": "assistant", "content": "The exact retained page text was read."}
				if n == 1 {
					finish = "tool_calls"
					delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "link-call", "type": "function", "function": map[string]any{"name": "read_link", "arguments": `{"url":"example.org/report#alpha-beta","find":"alpha beta"}`}}}}
				}
				raw := assistantChatJSON(map[string]any{"id": "link-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"link-fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", raw, finish)
			})
			actual := engine
			if actual == "read-child" {
				actual = "codex"
			}
			f := newAssistantLinkFixture(t, actual, "provided", "work", u.server.URL)
			if engine != "api" {
				peer := strings.Replace(assistantExecutionCodexPeer, "else: tool(offered('get_schema'),{})", "else: tool(offered('read_link'),{'url':'example.org/report#alpha-beta','find':'alpha beta'})", 1)
				if engine == "read-child" {
					peer = strings.Replace(peer, "if mode=='delegate' and index==1:\n   tool(offered('delegate_tasks'),{'tasks':[{'title':'Inspect schema','objective':'Inspect the schema only.','profile':'review'}]})", `if mode=='normal' and index==1: tool(offered('delegate_tasks'),{'tasks':[{'title':'Read supplied page','objective':'Read only the supplied page and report exact retained evidence.','profile':'review'}]})
  elif mode=='normal' and index==2: tool(offered('read_link'),{'url':'example.org/report#alpha-beta','find':'alpha beta'})
  elif mode=='normal' and index>=3: finish()`, 1)
					peer = strings.Replace(peer, "if mode=='delegate' and index==2:", "if mode=='normal' and index==2:", 1)
				}
				assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(f.trace), "peer.py"), []byte(peer), 0600))
			}
			x := assistantExecutionOpen(t, f.s, f.e)
			off := false
			x.Run.Review = &off
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
			record := assistantExecutionRecord(t, f.e)
			if len(record.DomainOutputs) != 1 || len(record.Links) != 1 || f.acquisitions.Load() != 1 || f.seals.Load() != 1 {
				t.Fatal("actual link producer lost custody", record.Links, record.DomainOutputs)
			}
			var host assistantHostIntent
			assistantRunMust(t, readAssistantHostReceipt(assistantDialectOperation(record, record.Links[0].HostOperationID).Intent, &host))
			if engine == "read-child" && host.Root {
				t.Fatal("read child mislabeled root")
			}
			if engine != "api" && host.CallbackID == "" {
				t.Fatal("native callback custody missing")
			}
			assistantRunMust(t, assistantRecipeImport(t, f.e, record))
			assistantRunMust(t, f.e.writer.State(t.Context(), "complete"))
			assistantRunMust(t, f.e.ProjectChat(t.Context()))
			assistantRunMust(t, f.e.ProjectChat(t.Context()))
			if f.calls.Load() != 2 {
				t.Fatal("terminal projection repeated effects")
			}
			if dir := os.Getenv("DESK_LINK_READER_OUTPUTS"); dir != "" {
				saved := policyWorkspaceRead(t, f.s, f.token, "/api/conversations")
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, engine+".json"), saved.Content, 0600))
				data, err := f.s.openChatData()
				assistantRunMust(t, err)
				name, err := resolveConversationName(data.root, f.s.conversationIdentity())
				assistantRunMust(t, err)
				packet, err := readPrivateData(data.root, attachmentProjectPrefix(name)+record.Links[0].FinalID+".json", maxAttachmentObjectBytes)
				assistantRunMust(t, err)
				assistantRunMust(t, data.root.Close())
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, engine+"-object.json"), packet, 0600))
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, engine+"-pin.json"), assistantChatJSON(map[string]any{"url": record.Links[0].Gateway.URL, "authority": record.Links[0].Gateway.Authority, "signer": map[string]string{"algorithm": "ed25519", "public": record.Links[0].Gateway.Public}}), 0600))
				var receipt assistantHostResult
				assistantRunMust(t, readAssistantHostReceipt(assistantDialectOperation(record, record.Links[0].HostOperationID).Result, &receipt))
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, engine+"-window.json"), receipt.Result, 0600))
			}
		})
	}
}

func TestAssistantLinkQueuedConfiguration(t *testing.T) {
	for _, change := range []string{"gateway-url", "session", "profile", "source"} {
		t.Run(change, func(t *testing.T) {
			f := newAssistantLinkFixture(t, "api", "research", "work", "http://127.0.0.1:1")
			_ = assistantExecutionOpen(t, f.s, f.e)
			finish := f.queuedCall(t, "queued-search", assistantLinkTool, `{"url":"https://example.org/report"}`, 3*time.Second)
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
			result := finish()
			if !result.Err() || f.calls.Load() != 0 {
				t.Fatal("queued changed authority dispatched", f.calls.Load(), string(result.Value().JSON()))
			}
		})
	}
}

func TestAssistantLinkCancelAfterAcquisitionRetainsStage(t *testing.T) {
	f := newAssistantLinkFixture(t, "api", "research", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	f.after = func(path string) {
		if path == "acquire" {
			f.e.cancel()
		}
	}
	result := f.invokeLink(t, "cancelled-search", true, `{"url":"https://example.org/report"}`)
	if !result.Err() || f.acquisitions.Load() != 1 || f.seals.Load() != 0 {
		t.Fatal("cancelled acquisition continued", f.calls.Load())
	}
	record := assistantExecutionRecord(t, f.e)
	if len(record.Links) != 1 || record.Links[0].Stage.Receipt == nil || record.Links[0].Final.Intent != nil || len(record.DomainOutputs) != 0 {
		t.Fatal("completed acquisition custody was lost", record.Links)
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

func TestAssistantLinkLostStorageAcknowledgement(t *testing.T) {
	for _, stage := range []string{"stage", "final"} {
		t.Run(stage, func(t *testing.T) {
			f := newAssistantLinkFixture(t, "api", "research", "work", "http://127.0.0.1:1")
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
				matches := stage == "stage" && object.Original.Name == "chat-link-acquisition.json" || stage == "final" && object.Proof != nil
				if matches {
					failed = true
					return errors.New("fixture lost exact storage readback acknowledgement")
				}
				return nil
			}
			t.Cleanup(func() { testHookPrivateRead = nil })
			result := f.invokeLink(t, "uncertain-storage", true, `{"url":"https://example.org/report"}`)
			testHookPrivateRead = nil
			if !failed || !result.Err() {
				t.Fatal("storage uncertainty was not observed")
			}
			before := assistantExecutionRecord(t, f.e)
			if len(before.Links) != 1 || !before.Links[0].DispatchClosed {
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
			hostBefore := bytes.Clone(assistantDialectOperation(beforeGet, beforeGet.Links[0].HostOperationID).Result)
			assistantRunMust(t, f.e.ProjectChat(t.Context()))
			recovered := assistantExecutionRecord(t, f.e)
			if stage == "final" {
				if recovered.Links[0].Final.Receipt == nil || len(recovered.DomainOutputs) != 1 || !recovered.DomainOutputs[0].Link.Recovered || !bytes.Equal(hostBefore, assistantDialectOperation(recovered, beforeGet.Links[0].HostOperationID).Result) {
					t.Fatal("exact final recovery changed original failure or lost custody")
				}
			} else if recovered.Links[0].Stage.Receipt == nil || len(recovered.DomainOutputs) != 0 {
				t.Fatal("unverified stage recovery authorized links or lost custody")
			}
			if f.calls.Load() != expected {
				t.Fatal("explicit receipt reconciliation repeated external work")
			}

		})
	}
}

func TestAssistantLinkActualRefusalEvents(t *testing.T) {
	for _, vector := range []struct {
		name, arguments, choice string
		noConfig                bool
	}{
		{"empty", `{"url":""}`, "provided", false},
		{"missing", `{}`, "provided", false},
		{"oversized", string(assistantChatJSON(map[string]string{"url": "https://example.org/" + strings.Repeat("x", 4096)})), "provided", false},
		{"undeclared", `{"url":"https://example.org/report","site":"https://example.org/"}`, "provided", false},
		{"not-supplied", `{"url":"https://invented.org/"}`, "provided", false},
		{"no-config", `{"url":"https://example.org/report"}`, "provided", true},
	} {
		t.Run(vector.name, func(t *testing.T) {
			var requests atomic.Int32
			upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				finish := "stop"
				delta := map[string]any{"role": "assistant", "content": "Search was refused."}
				if requests.Add(1) == 1 {
					finish = "tool_calls"
					delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "refused-search", "type": "function", "function": map[string]any{"name": "read_link", "arguments": vector.arguments}}}}
				}
				raw := assistantChatJSON(map[string]any{"id": "refusal-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"refusal-fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", raw, finish)
			})
			f := newAssistantLinkFixture(t, "api", vector.choice, "work", upstream.server.URL)
			if vector.noConfig {
				assistantRunMust(t, os.WriteFile(f.s.deskConfigPath(), []byte(`{"deskConfigVersion":1,"research":{"documents":null}}`), 0600))
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
				if result := held.Event.ToolResult; result != nil && result.Name == assistantLinkTool {
					refused = result.IsError
				}
			}
			if !refused || f.calls.Load() != 0 || len(record.Links) != 0 || len(record.DomainOutputs) != 0 {
				t.Fatal("refusal did not retain a zero-acquisition error event", refused, f.calls.Load(), record.Links)
			}
			if dir := os.Getenv("DESK_LINK_READER_OUTPUTS"); dir != "" {
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, "refusal-"+vector.name+".json"), assistantChatJSON(events), 0600))
			}
		})
	}
}

type assistantLinkWindowVector struct {
	PageCount       int                  `json:"pageCount"`
	ExpectedRefusal bool                 `json:"expectedRefusal"`
	Name            string               `json:"name"`
	Pages           []string             `json:"pages"`
	Padding         int                  `json:"padding"`
	Suffix          string               `json:"suffix"`
	Repeat          int                  `json:"repeat"`
	Offset          *int                 `json:"offset"`
	Find            string               `json:"find"`
	ExpectedOffset  int                  `json:"expectedOffset"`
	ExpectedPages   []int                `json:"expectedPages"`
	ExpectedMatches []assistantLinkRange `json:"expectedMatches"`
	Continuation    bool                 `json:"continuation"`
}

func TestAssistantLinkSharedWindowVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/assistant-link-window.json")
	assistantRunMust(t, err)
	var vectors []assistantLinkWindowVector
	assistantRunMust(t, json.Unmarshal(raw, &vectors))
	for index, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			f := newAssistantLinkFixture(t, "api", "provided", "work", "http://127.0.0.1:1")
			_ = assistantExecutionOpen(t, f.s, f.e)
			f.resultChange = func(record map[string]any) {
				content := record["content"].(map[string]any)
				pages := []any{}
				chars := 0
				texts := v.Pages
				if v.PageCount > 0 {
					texts = make([]string, v.PageCount)
					for i := range texts {
						texts[i] = v.Pages[0]
					}
				}
				for i, text := range texts {
					if i == 0 {
						if v.Repeat > 0 {
							text = strings.Repeat(text, v.Repeat)
						}
						text += strings.Repeat("x", v.Padding) + v.Suffix
					}
					chars += len([]rune(text))
					pages = append(pages, map[string]any{"number": i + 1, "status": "ok", "extraction": "verbatim", "text": text, "chars": len([]rune(text)), "unmapped": 0})
				}
				content["pages"], content["pageCount"], content["chars"] = pages, len(pages), chars
			}
			args := map[string]any{"url": "https://example.org/report"}
			if v.Offset != nil {
				args["offset"] = *v.Offset
			}
			if v.Find != "" {
				args["find"] = v.Find
			}
			result := f.invokeLink(t, "window", true, string(assistantChatJSON(args)))
			if v.ExpectedRefusal {
				if !result.Err() || len(result.JSON()) > runcontext.Defaults.ToolResultBytes {
					t.Fatal("oversized window metadata escaped the shared byte bound")
				}
				record := assistantExecutionRecord(t, f.e)
				if len(record.DomainOutputs) != 1 || !record.DomainOutputs[0].Link.Attachment.Document.NeedsReview {
					t.Fatal("metadata refusal lost retained review reference")
				}
				assistantLinkExportWindow(t, f, index, v.Name, []any{args}, []json.RawMessage{result.JSON()})
				return
			}
			if result.Err() {
				t.Fatal("window vector refused", string(result.JSON()))
			}
			detail := result.StructuredContent
			if int(detail.Get("offset").Num()) != v.ExpectedOffset {
				t.Fatal("safe actual offset differs", detail.Text())
			}
			var pages []int
			assistantRunMust(t, json.Unmarshal(detail.Get("pages").JSON(), &pages))
			if !reflect.DeepEqual(pages, v.ExpectedPages) {
				t.Fatal("selected page spans differ", pages)
			}
			if v.ExpectedMatches != nil {
				var matches []assistantLinkRange
				assistantRunMust(t, json.Unmarshal(detail.Get("matches").JSON(), &matches))
				if !reflect.DeepEqual(matches, v.ExpectedMatches) {
					t.Fatal("ECMAScript whitespace/find differs", matches)
				}
			}
			if len(result.JSON()) > runcontext.Defaults.ToolResultBytes {
				t.Fatal("shared context result byte bound omitted")
			}
			results := []json.RawMessage{result.JSON()}
			arguments := []any{args}
			if v.Continuation {
				end := int(detail.Get("chars").Num())
				text := result.Content[0].Text()
				marker := "showing characters "
				at := strings.LastIndex(text, marker)
				if at < 0 {
					t.Fatal("no continuation range")
				}
				var start, shownEnd, total int
				if _, err := fmt.Sscanf(text[at:], "showing characters %d–%d of %d", &start, &shownEnd, &total); err != nil {
					t.Fatal(err)
				}
				if !detail.Get("more").Truth() || shownEnd <= start || shownEnd >= end || shownEnd%2 != 0 {
					t.Fatal("cutoff split pair or failed to progress", detail.Text(), shownEnd)
				}
				nextArgs := map[string]any{"url": "https://example.org/report", "offset": shownEnd}
				next := f.invokeLink(t, "next-window", false, string(assistantChatJSON(nextArgs)))
				if next.Err() || int(next.StructuredContent.Get("offset").Num()) != shownEnd || !strings.Contains(next.Content[0].Text(), "😀") {
					t.Fatal("next offset skipped/split text")
				}
				arguments = append(arguments, nextArgs)
				results = append(results, next.JSON())
				if f.acquisitions.Load() != 1 {
					t.Fatal("continuation reacquired")
				}
			}
			assistantLinkExportWindow(t, f, index, v.Name, arguments, results)
		})
	}
}
func TestAssistantLinkPartialAndSelectedPages(t *testing.T) {
	f := newAssistantLinkFixture(t, "api", "provided", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	f.resultChange = func(record map[string]any) {
		record["content"].(map[string]any)["truncated"] = true
		record["processing"].(map[string]any)["status"] = "partial"
	}
	result := f.invokeLink(t, "partial", true, `{"url":"https://example.org/report"}`)
	if !result.Err() || strings.Contains(result.Content[0].Text(), "Exact quote") {
		t.Fatal("unreviewed text entered model result")
	}
	run := assistantExecutionRecord(t, f.e)
	if len(run.DomainOutputs) != 1 || !run.DomainOutputs[0].Link.Attachment.Document.NeedsReview || run.DomainOutputs[0].Link.Attachment.Document.AllowPartial {
		t.Fatal("partial retained review reference lost")
	}
	held := run.Links[0]
	packet, err := f.e.assistantLinkPacket(t.Context(), held.FinalID)
	assistantRunMust(t, err)
	ref := *run.DomainOutputs[0].Link.Attachment.Document
	ref.AllowPartial = true
	ref.NeedsReview = false
	ref.Pages = []int{2}
	verified, err := verifyAssistantDocument(t.Context(), assistantDocumentProgram(t), packet, inputResearchPin{held.Gateway.Authority, held.Gateway.Public}, ref)
	assistantRunMust(t, err)
	args, err := readAssistantLinkArguments([]byte(`{"url":"https://example.org/report"}`))
	assistantRunMust(t, err)
	window, err := f.e.assistantLinkWindow(verified, args)
	assistantRunMust(t, err)
	if window.Err() || strings.Contains(window.Content[0].Text(), "cafe") || !strings.Contains(window.Content[0].Text(), "Exact quote") || !strings.Contains(window.Content[0].Text(), "/page/2") {
		t.Fatal("selected partial pages or exact citations lost", string(window.JSON()))
	}
	if dir := os.Getenv("DESK_LINK_READER_OUTPUTS"); dir != "" {
		file := run.DomainOutputs[0].Link.Attachment
		file.Document = &ref
		fixture := map[string]any{"object": json.RawMessage(packet), "attachment": file, "unreviewed": result.Value(), "reviewed": window.Value(), "pin": map[string]any{"url": held.Gateway.URL, "authority": held.Gateway.Authority, "signer": map[string]string{"algorithm": "ed25519", "public": held.Gateway.Public}}}
		fixture["unreviewed"] = json.RawMessage(result.JSON())
		fixture["reviewed"] = json.RawMessage(window.JSON())
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, "partial-selected.json"), assistantChatJSON(fixture), 0600))
	}
	assistantRunMust(t, assistantRecipeImport(t, f.e, run))
	f.e.cancel()
	<-f.e.done
	assistantRunMust(t, f.e.ProjectChat(t.Context()))
}
func TestAssistantLinkVerifiedSearchAuthorization(t *testing.T) {
	for _, spelling := range []string{"https://éxample.net/über", "https://xn--xample-9ua.net/%C3%BCber"} {
		t.Run(spelling, func(t *testing.T) {
			f := newAssistantLinkFixture(t, "api", "research", "work", "http://127.0.0.1:1")
			_ = assistantExecutionOpen(t, f.s, f.e)
			f.resultChange = func(result map[string]any) {
				if result["attachmentVersion"] == nil {
					result["hits"] = []any{map[string]any{"url": spelling, "title": "Signed IDN", "snippet": "Lead only"}}
				}
			}
			if result := f.invoke(t, "search", true); result.Err() {
				t.Fatal("current search refused", string(result.JSON()))
			}
			result := f.invokeLink(t, "searched-link", false, string(assistantChatJSON(map[string]any{"url": spelling})))
			if result.Err() {
				t.Fatal("verified search link refused", string(result.JSON()))
			}
			run := assistantExecutionRecord(t, f.e)
			if len(run.Searches) != 1 || len(run.Links) != 1 || run.Links[0].SearchConfigurationDigest == "" {
				t.Fatal("search receipt/config dependency missing")
			}
			packet, err := f.e.assistantLinkPacket(t.Context(), run.Searches[0].FinalID)
			assistantRunMust(t, err)
			var object attachmentObject
			assistantRunMust(t, json.Unmarshal(packet, &object))
			if !strings.Contains(object.Proof.Response, spelling) {
				t.Fatal("signed URL spelling changed")
			}
			assistantRunMust(t, assistantRecipeImport(t, f.e, run))
		})
	}
}
func TestAssistantLinkExactProofAssociation(t *testing.T) {
	f := newAssistantLinkFixture(t, "api", "provided", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	result := f.invokeLink(t, "proof", true, `{"url":"https://example.org/report"}`)
	if result.Err() {
		t.Fatal(string(result.JSON()))
	}
	held := assistantExecutionRecord(t, f.e).Links[0]
	raw, err := f.e.assistantLinkPacket(t.Context(), held.FinalID)
	assistantRunMust(t, err)
	for _, name := range []string{"missing-proof", "url", "session", "pin", "digest", "storage", "receipt"} {
		t.Run(name, func(t *testing.T) {
			copy := held
			var object attachmentObject
			assistantRunMust(t, json.Unmarshal(raw, &object))
			switch name {
			case "missing-proof":
				object.Proof = nil
			case "url":
				object.Proof.Web.URL = "https://example.org/other"
			case "session":
				object.Proof.Session = "foreign-session"
			case "pin":
				copy.Gateway.Public = strings.Repeat("a", 64)
			case "digest":
				copy.Digest = "sha256:" + strings.Repeat("b", 64)
			case "storage":
				copy.FinalID = "11111111-1111-1111-1111-111111111111"
			case "receipt":
				copy.Stage.Receipt = &attachmentReceipt{ID: copy.StageID, SHA256: strings.Repeat("a", 64)}
			}
			packet := assistantChatJSON(object)
			receipt := attachmentReceipt{ID: copy.FinalID, SHA256: digestOf(packet)}
			copy.Final.Intent = &receipt
			copy.Final.Receipt = &receipt
			if _, _, err := verifyAssistantLinkObject(t.Context(), assistantDocumentProgram(t), packet, copy); err == nil {
				t.Fatal("substituted proof accepted", name)
			}
		})
	}
}

func TestAssistantLinkRetainedLimit(t *testing.T) {
	f := newAssistantLinkFixture(t, "api", "provided", "work", "http://127.0.0.1:1", func(chat map[string]any) {
		rows := []any{}
		for i := range 256 {
			rows = append(rows, map[string]any{"id": fmt.Sprintf("retained-%03d", i), "name": "saved.txt", "text": ""})
		}
		chat["documents"] = rows
	})
	_ = assistantExecutionOpen(t, f.s, f.e)
	if result := f.invokeLink(t, "over-retained-limit", true, `{"url":"https://example.org/report"}`); !result.Err() || f.calls.Load() != 0 || len(assistantExecutionRecord(t, f.e).Links) != 0 {
		t.Fatal("retained256 limit omitted")
	}
}
func TestAssistantLinkFreshAdmissionBudget(t *testing.T) {
	f := newAssistantLinkFixture(t, "api", "provided", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	for i, url := range []string{"https://example.org/report", "https://example.org/two", "https://example.org/three"} {
		if result := f.invokeLink(t, fmt.Sprint("read-", i), i != 1, string(assistantChatJSON(map[string]any{"url": url}))); result.Err() {
			t.Fatal("shared read refused", string(result.JSON()))
		}
	}
	if result := f.invokeLink(t, "limited", false, `{"url":"https://example.org/four"}`); !result.Err() {
		t.Fatal("limit missing")
	}
	if result := f.invokeLink(t, "retained-after-limit", false, `{"url":"https://example.org/report","offset":2}`); result.Err() || f.acquisitions.Load() != 3 {
		t.Fatal("ordinary limit blocked/charged retained read", string(result.JSON()))
	}
	old := f.e
	old.cancel()
	<-old.done
	f.s.assistantRunExecutor = &assistantProtocolExecutor{execute: func(ctx context.Context, _ *assistantRunExecution) error { <-ctx.Done(); return ctx.Err() }}
	start := old.source.Start
	start.AdmissionID = "fresh-link-budget"
	view := assistantProtocolView(t, policyCall(f.s, f.token, "POST", "/api/assistant-runs", start), 201)
	service := f.s.assistantRunService()
	service.mu.Lock()
	f.e = service.active[view.ID]
	service.mu.Unlock()
	if f.e == nil {
		t.Fatal("fresh admission missing")
	}
	t.Cleanup(func() { f.e.cancel(); <-f.e.done })
	_, err := f.e.prepareAssistantChatInput(t.Context())
	assistantRunMust(t, err)
	if result := f.invokeLink(t, "fresh-limited", true, `{"url":"https://example.org/four"}`); !result.Err() || f.acquisitions.Load() != 3 {
		t.Fatal("fresh admission reset saved-turn budget")
	}
}
func TestAssistantLinkSavedNominationOnly(t *testing.T) {
	raw := assistantChatJSON(map[string]any{"documents": []any{map[string]any{"id": "file", "link": map[string]string{"url": "https://example.org/no-document"}}}, "checkpoint": map[string]any{"state": map[string]any{"turns": []any{map[string]any{"role": "assistant", "text": "https://example.org/assistant"}, map[string]any{"role": "user", "text": "I supplied https://example.org/real", "nested": map[string]string{"url": "https://example.org/nested"}}}}}})
	for _, url := range []string{"https://example.org/assistant", "https://example.org/nested", "https://example.org/no-document"} {
		if assistantLinkGiven(raw, url) {
			t.Fatal("unowned nomination admitted", url)
		}
	}
	if !assistantLinkGiven(raw, "https://example.org/real") {
		t.Fatal("saved user nomination lost")
	}
}
func TestAssistantLinkUnknownExternalOutcome(t *testing.T) {
	for _, stage := range []string{"acquire", "seal"} {
		t.Run(stage, func(t *testing.T) {
			f := newAssistantLinkFixture(t, "api", "provided", "work", "http://127.0.0.1:1")
			_ = assistantExecutionOpen(t, f.s, f.e)
			original := relayTransport
			failed := false
			relayTransport = assistantSearchTransport(func(r *http.Request) (*http.Response, error) {
				response, err := http.DefaultTransport.RoundTrip(r)
				if !failed && strings.HasSuffix(r.URL.Path, "/"+stage) && err == nil {
					failed = true
					_ = response.Body.Close()
					return nil, errors.New("fixture lost external acknowledgement")
				}
				return response, err
			})
			t.Cleanup(func() { relayTransport = original })
			result := f.invokeLink(t, "unknown", true, `{"url":"https://example.org/report"}`)
			relayTransport = original
			if !failed || !result.Err() {
				t.Fatal("unknown external effect was accepted")
			}
			<-f.e.done
			run := assistantExecutionRecord(t, f.e)
			if len(run.Links) != 1 || !run.Links[0].DispatchClosed || !assistantLinkPending(run.Links[0]) {
				t.Fatal("unknown dispatch not retained")
			}
			// A crash need not have written DispatchClosed: durable intent alone fences
			// fresh admission, and receipt observation never repeats an external effect.
			copy := run.Links[0]
			copy.DispatchClosed = false
			if !assistantLinkPending(copy) {
				t.Fatal("crash intent loses unknown fence")
			}
			calls := f.calls.Load()
			for range 2 {
				_, err := f.s.assistantRunService().read(f.e.request.WithContext(t.Context()), run.ID)
				assistantRunMust(t, err)
			}
			if f.calls.Load() != calls {
				t.Fatal("GET replayed unknown external effect")
			}
			assistantRunMust(t, assistantRecipeImport(t, f.e, run))
			_, err := f.e.writer.lease.store.transaction(t.Context(), func(ledger *assistantRunLedger) (bool, error) {
				held, err := assistantRunBound(ledger, f.e.record.Binding, run.ID)
				if err != nil {
					return false, err
				}
				held.Links[0].DispatchClosed = false
				held.Revision++
				return true, nil
			})
			assistantRunMust(t, err)
			restartAssistantLinkWithSavedChat(t, f, "crash-intent-"+stage, nil)
			if result := f.invokeLink(t, "fresh-after-crash", true, `{"url":"https://example.org/two"}`); !result.Err() || f.calls.Load() != calls {
				t.Fatal("fresh admission replayed an unresolved durable intent")
			}

		})
	}
}

func TestAssistantLinkCustodyShapeAndResume(t *testing.T) {
	f := newAssistantLinkFixture(t, "api", "research", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	if result := f.invokeLink(t, "resume", true, `{"url":"https://example.org/report","find":"alpha beta","offset":1}`); result.Err() {
		t.Fatal(string(result.JSON()))
	}
	run := assistantExecutionRecord(t, f.e)
	raw := assistantChatJSON(assistantRunLedger{Version: 1, Project: run.Binding.Project, Runs: []assistantRunRecord{run}})
	resumed, err := decodeAssistantRunStorage(f.e.writer.lease.store.name, raw)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(run.Links, resumed.Runs[0].Links) || !reflect.DeepEqual(run.DomainOutputs, resumed.Runs[0].DomainOutputs) {
		t.Fatal("resume lost private link custody/output fields")
	}
	// Every member carried by this actual producer fails if removed from its
	// scratch struct schema. Nested storage/Gateway/reference members are covered.
	count := 0
	var omit func([]byte, reflect.Type, string)
	omit = func(value []byte, shape reflect.Type, path string) {
		for shape.Kind() == reflect.Pointer {
			shape = shape.Elem()
		}
		if shape.Kind() != reflect.Struct {
			return
		}
		var fields map[string]json.RawMessage
		assistantRunMust(t, json.Unmarshal(value, &fields))
		for i := range shape.NumField() {
			field := shape.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			v, ok := fields[name]
			if !ok {
				continue
			}
			scratch := []reflect.StructField{}
			for j := range shape.NumField() {
				if j != i {
					scratch = append(scratch, shape.Field(j))
				}
			}
			if readAssistantDialectCustody(value, reflect.StructOf(scratch)) == nil {
				t.Fatal("schema member omission not witnessed", path+"."+name)
			}
			count++
			nested := field.Type
			for nested.Kind() == reflect.Pointer {
				nested = nested.Elem()
			}
			if nested.Kind() == reflect.Struct {
				omit(v, field.Type, path+"."+name)
			}
		}
	}
	omit(assistantChatJSON(run.Links[0]), reflect.TypeOf(assistantLinkCustody{}), "links")
	omit(assistantChatJSON(run.DomainOutputs[0].Link), reflect.TypeOf(assistantLinkOutput{}), "output")
	// Handwritten optional custody/output members complement the actual supplied
	// URL producer above. Search, cancellation, redirect and recovery tests carry
	// these members on their real authorization and terminal-resume paths.
	optional := run.Links[0]
	optional.AuthorityPacket = &attachmentReceipt{ID: optional.FinalID, SHA256: optional.Final.Receipt.SHA256}
	optional.SearchConfigurationDigest = digestOf([]byte("search-configuration"))
	optional.SearchPreferenceDigest = digestOf([]byte("research-preference"))
	optional.DispatchClosed = true
	optionalOutput := *run.DomainOutputs[0].Link
	optionalOutput.Recovered = true
	optionalOutput.Attachment.Link = &assistantLinkLocation{URL: optional.Request.URL, ResolvedURL: "https://redirect.example.net/report", Anchor: "section"}
	omit(assistantChatJSON(optional), reflect.TypeOf(assistantLinkCustody{}), "optional-links")
	omit(assistantChatJSON(optionalOutput), reflect.TypeOf(assistantLinkOutput{}), "optional-output")
	var restored assistantLinkCustody
	var restoredOutput assistantLinkOutput
	assistantRunMust(t, json.Unmarshal(assistantChatJSON(optional), &restored))
	assistantRunMust(t, json.Unmarshal(assistantChatJSON(optionalOutput), &restoredOutput))
	if !reflect.DeepEqual(optional, restored) || !reflect.DeepEqual(optionalOutput, restoredOutput) {
		t.Fatal("private optional checkpoint members did not round trip")
	}
	if count < 50 {
		t.Fatal("private schema omission coverage unexpectedly small", count)
	}
	for _, member := range []string{"configurationDigest", "hostOperationId", "turnId", "requestDigest", "request", "gateway", "session", "stageId", "sealId", "finalId", "acquire", "seal", "stage", "sealStage", "original", "final"} {
		var wire map[string]any
		assistantRunMust(t, json.Unmarshal(raw, &wire))
		held := wire["runs"].([]any)[0].(map[string]any)["links"].([]any)[0].(map[string]any)
		delete(held, member)
		if _, err := decodeAssistantRunStorage(f.e.writer.lease.store.name, assistantChatJSON(wire)); err == nil {
			t.Fatal("missing resumed custody member accepted", member)
		}
	}
	t.Logf("private schema omission witnesses: %d actual and handwritten optional nested members", count)
}

func restartAssistantLinkWithSavedChat(t *testing.T, f *assistantSearchFixture, admission string, edit func(map[string]any)) {
	t.Helper()
	old := f.e
	old.cancel()
	<-old.done
	saved := policyWorkspaceRead(t, f.s, f.token, "/api/conversations")
	var document conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &document))
	if edit != nil {
		var chat map[string]any
		assistantRunMust(t, json.Unmarshal(document.Chats[0], &chat))
		edit(chat)
		document.Chats[0] = assistantChatJSON(chat)
		w := policyWorkspaceCall(f.s, f.token, "PUT", "/api/conversations", saved.SHA256, document)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		saved = policyWorkspaceRead(t, f.s, f.token, "/api/conversations")
		assistantRunMust(t, json.Unmarshal(saved.Content, &document))
	}
	f.s.assistantRunExecutor = &assistantProtocolExecutor{execute: func(ctx context.Context, _ *assistantRunExecution) error { <-ctx.Done(); return ctx.Err() }}
	start := old.source.Start
	start.AdmissionID = admission
	start.Revision = digestOf(document.Chats[0])
	view := assistantProtocolView(t, policyCall(f.s, f.token, "POST", "/api/assistant-runs", start), 201)
	service := f.s.assistantRunService()
	service.mu.Lock()
	fresh := service.active[view.ID]
	service.mu.Unlock()
	if fresh == nil {
		t.Fatal("fresh saved-source admission missing")
	}
	f.e = fresh
	t.Cleanup(func() { fresh.cancel(); <-fresh.done })
	_, err := fresh.prepareAssistantChatInput(t.Context())
	assistantRunMust(t, err)
}
func TestAssistantLinkSavedSearchAndRedirectAuthority(t *testing.T) {
	t.Run("saved search", func(t *testing.T) {
		f := newAssistantLinkFixture(t, "api", "research", "work", "http://127.0.0.1:1")
		_ = assistantExecutionOpen(t, f.s, f.e)
		f.resultChange = func(result map[string]any) {
			if result["attachmentVersion"] == nil {
				result["hits"] = []any{map[string]any{"url": "https://éxample.net/über", "title": "saved IDN", "snippet": "lead only"}}
			}
		}
		if result := f.invoke(t, "saved-search", true); result.Err() {
			t.Fatal(string(result.JSON()))
		}
		f.e.cancel()
		<-f.e.done
		assistantRunMust(t, f.e.ProjectChat(t.Context()))
		restartAssistantLinkWithSavedChat(t, f, "saved-search-link", nil)
		result := f.invokeLink(t, "read-saved-search", true, `{"url":"https://éxample.net/über"}`)
		if result.Err() {
			t.Fatal("saved verified search refused", string(result.JSON()))
		}
		run := assistantExecutionRecord(t, f.e)
		if len(run.Searches) != 0 || len(run.Links) != 1 || run.Links[0].AuthorityPacket == nil {
			t.Fatal("saved search proof custody absent")
		}
	})
	for _, spoof := range []bool{false, true} {
		t.Run(fmt.Sprint("redirect-spoof-", spoof), func(t *testing.T) {
			f := newAssistantLinkFixture(t, "api", "research", "work", "http://127.0.0.1:1")
			_ = assistantExecutionOpen(t, f.s, f.e)
			f.resultChange = func(record map[string]any) {
				record["provenance"].(map[string]any)["source"].(map[string]any)["url"] = "https://redirect.example.net/report"
			}
			if result := f.invokeLink(t, "redirected", true, `{"url":"https://example.org/report"}`); result.Err() {
				t.Fatal(string(result.JSON()))
			}
			f.e.cancel()
			<-f.e.done
			assistantRunMust(t, f.e.ProjectChat(t.Context()))
			target := "https://redirect.example.net/report"
			var edit func(map[string]any)
			if spoof {
				target = "https://spoof.example.net/report"
				edit = func(chat map[string]any) {
					chat["documents"].([]any)[0].(map[string]any)["link"].(map[string]any)["resolvedUrl"] = target
				}
			}
			restartAssistantLinkWithSavedChat(t, f, fmt.Sprint("redirect-link-", spoof), edit)
			calls := f.calls.Load()
			result := f.invokeLink(t, "read-redirect", true, string(assistantChatJSON(map[string]any{"url": target})))
			if result.Err() != spoof {
				t.Fatal("verified requested/resolved URL pair differs", spoof, string(result.JSON()))
			}
			if spoof && f.calls.Load() != calls {
				t.Fatal("unverified redirect string acquired")
			}
		})
	}
}
func TestAssistantLinkQueuedVerifiedSearchAuthority(t *testing.T) {
	for _, change := range []string{"connection", "proof-bytes"} {
		t.Run(change, func(t *testing.T) {
			f := newAssistantLinkFixture(t, "api", "research", "work", "http://127.0.0.1:1")
			_ = assistantExecutionOpen(t, f.s, f.e)
			f.resultChange = func(result map[string]any) {
				if result["attachmentVersion"] == nil {
					result["hits"] = []any{map[string]any{"url": "https://news.example.net/report", "title": "public lead", "snippet": "lead"}}
				}
			}
			if result := f.invoke(t, "queued-search", true); result.Err() {
				t.Fatal(string(result.JSON()))
			}
			finish := f.queuedCall(t, "queued-searched-link", assistantLinkTool, `{"url":"https://news.example.net/report"}`, 3*time.Second)
			if change == "connection" {
				assistantRunMust(t, os.WriteFile(f.statusPath, assistantChatJSON(map[string]any{"version": 1, "connections": []any{map[string]any{"id": "demo", "revision": strings.Repeat("b", 64), "provider": "tavily"}}}), 0600))
			} else {
				run := assistantExecutionRecord(t, f.e)
				data, err := f.s.openChatData()
				assistantRunMust(t, err)
				name, err := resolveConversationName(data.root, f.s.conversationIdentity())
				assistantRunMust(t, err)
				file := attachmentProjectPrefix(name) + run.Searches[0].FinalID + ".json"
				packet, err := readPrivateData(data.root, file, maxAttachmentObjectBytes)
				assistantRunMust(t, err)
				packet = append(packet, ' ')
				assistantRunMust(t, writePrivateData(data.root, file, packet))
				assistantRunMust(t, data.root.Close())
			}
			result := finish()
			if !result.Err() || f.calls.Load() != 2 {
				t.Fatal("queued revoked search proof/config acquired", string(result.JSON()), f.calls.Load())
			}
		})
	}
}

func assistantLinkExportWindow(t *testing.T, f *assistantSearchFixture, index int, name string, arguments []any, results []json.RawMessage) {
	t.Helper()
	if dir := os.Getenv("DESK_LINK_READER_OUTPUTS"); dir != "" {
		record := assistantExecutionRecord(t, f.e)
		held := record.Links[0]
		packet, err := f.e.assistantLinkPacket(t.Context(), held.FinalID)
		assistantRunMust(t, err)
		fixture := map[string]any{"name": name, "object": json.RawMessage(packet), "attachment": record.DomainOutputs[0].Link.Attachment, "pin": map[string]any{"url": held.Gateway.URL, "authority": held.Gateway.Authority, "signer": map[string]string{"algorithm": "ed25519", "public": held.Gateway.Public}}, "arguments": arguments, "results": results}
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("window-%02d.json", index)), assistantChatJSON(fixture), 0600))
	}
}
