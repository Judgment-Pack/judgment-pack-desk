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
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func assistantWebsiteFixtureDiscovery(seed string) map[string]any {
	u, err := assistantPublicURL(seed)
	if err != nil {
		panic(err)
	}
	pages := []any{map[string]any{"url": seed, "from": "", "depth": 0, "status": "discovered", "title": "Home", "reason": ""}}
	for i := range 9 {
		pages = append(pages, map[string]any{"url": fmt.Sprintf("https://%s/child-%d", u.Host(), i), "from": seed, "depth": 1, "status": "discovered", "title": "Child", "reason": ""})
	}
	pages = append(pages, map[string]any{"url": "https://" + u.Host() + "/blocked", "from": seed, "depth": 1, "status": "blocked", "title": "", "reason": "robots"})
	return map[string]any{"version": 1, "seed": seed, "origin": "https://" + u.Host(), "pages": pages, "stopReason": "page-limit", "bytes": 1000, "requests": 11, "externalLinks": 1, "limits": map[string]any{"pages": 10, "depth": 2, "links": 100, "bytes": 8 << 20, "seconds": 45}}
}
func assistantWebsiteSigned(t *testing.T, session string, q assistantLinkRequest, change func(map[string]any)) ([]byte, []byte) {
	t.Helper()
	response, seal := assistantLinkSigned(t, session, q, nil)
	var envelope map[string]any
	assistantRunMust(t, json.Unmarshal(response, &envelope))
	result := assistantWebsiteFixtureDiscovery(q.URL)
	if change != nil {
		change(result)
	}
	canonical := assistantDocumentFixtureJSON(t, result)
	receipt := envelope["receipt"].(map[string]any)
	delete(receipt, "signature")
	receipt["source"] = assistantWebsiteSource
	receipt["resultDigest"] = "sha256:" + digestOf(canonical)
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	receipt["signature"] = hex.EncodeToString(ed25519.Sign(key, append([]byte("judgment-pack-gateway/receipt/3:"), assistantDocumentFixtureJSON(t, receipt)...)))
	envelope["result"] = json.RawMessage(canonical)
	return assistantDocumentFixtureJSON(t, envelope), seal
}
func newAssistantWebsiteFixture(t *testing.T, engine, choice, phase, upstream string, configure ...func(map[string]any)) *assistantSearchFixture {
	t.Helper()
	f := newAssistantLinkFixture(t, engine, choice, phase, upstream, configure...)
	// Explicit controlled companion fixture, preserving the real supplied Gateway
	// verifier. This fixture's finite catalog advertises the browser contract.
	bundle := f.s.localGateway.bundle
	path := filepath.Join(bundle, executableName("gateway-connections"))
	raw, err := os.ReadFile(path)
	assistantRunMust(t, err)
	catalog := `{"version":2,"providers":[],"sources":[{"id":"web","input":"url","maxBytes":4194304,"mediaTypes":["text/html","text/plain","application/pdf"]},{"id":"web-discovery","input":"url","maxBytes":1048576,"mediaTypes":["application/vnd.jpack.web-discovery+json"]}]}`
	lines := strings.SplitN(string(raw), "\n", 3)
	modified := lines[0] + "\n" + lines[1] + "\nif '--catalog-v4' in sys.argv:\n print(" + fmt.Sprintf("%q", catalog) + ");sys.exit(0)\n" + lines[2]
	assistantRunMust(t, os.WriteFile(path, []byte(modified), 0700))
	manifestPath := filepath.Join(bundle, "gateway-bundle.json")
	manifestRaw, err := os.ReadFile(manifestPath)
	assistantRunMust(t, err)
	var manifest struct {
		Revision string            `json:"revision"`
		Files    map[string]string `json:"files"`
	}
	assistantRunMust(t, json.Unmarshal(manifestRaw, &manifest))
	manifest.Files[executableName("gateway-connections")] = digestOf([]byte(modified))
	manifestRaw = assistantChatJSON(manifest)
	assistantRunMust(t, os.WriteFile(manifestPath, manifestRaw, 0600))
	t.Setenv("JPACK_DESK_GATEWAY_MANIFEST_SHA256", digestOf(manifestRaw))
	return f
}
func (f *assistantSearchFixture) invokeWebsite(t *testing.T, call string, root bool, raw string) runcontext.ToolResult {
	t.Helper()
	ctx := runcontext.WithCallID(t.Context(), call)
	if root {
		assistantPolicyEvent(t, f.e, call, assistantWebsiteTool, []byte(raw))
	}
	ctx = context.WithValue(ctx, assistantHostRootKey{}, root)
	tools, err := f.e.assistantWebsiteTools()
	assistantRunMust(t, err)
	result, err := tools.tools[0].Execute(ctx, json.RawMessage(raw))
	assistantRunMust(t, err)
	return result
}
func TestAssistantWebsiteCustodyAndAuthority(t *testing.T) {
	f := newAssistantWebsiteFixture(t, "api", "research", "plan", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	result := f.invokeWebsite(t, "explore", true, `{"url":"example.org/report"}`)
	if result.Err() {
		t.Fatal("supplied exploration refused", string(result.JSON()))
	}
	record := assistantExecutionRecord(t, f.e)
	if len(record.Websites) != 1 || len(record.DomainOutputs) != 1 || len(record.Links) != 0 || f.calls.Load() != 2 {
		t.Fatal("website custody lost", record.Websites)
	}
	if result := f.invokeWebsite(t, "twice", true, `{"url":"example.org/report"}`); !result.Err() || f.calls.Load() != 2 {
		t.Fatal("exploration budget reset")
	}
	for _, url := range []string{"https://example.org/blocked", "https://example.org/guessed", "https://other.org/child-0"} {
		if result := f.invokeLink(t, url, true, string(assistantChatJSON(map[string]string{"url": url}))); !result.Err() || f.calls.Load() != 2 {
			t.Fatal("non-discovered URL acquired", url)
		}
	}
	page := f.invokeLink(t, "read-discovered", true, `{"url":"https://example.org/child-0"}`)
	if page.Err() {
		t.Fatal("discovered page refused", string(page.JSON()))
	}
	record = assistantExecutionRecord(t, f.e)
	if record.Links[0].Request.Site != "https://example.org/report" || record.Links[0].AuthorityPacket == nil {
		t.Fatal("site binding lost")
	}
	if result := f.invokeLink(t, "reread", true, `{"url":"https://example.org/child-0","find":"alpha beta"}`); result.Err() || f.calls.Load() != 4 {
		t.Fatal("retained page acquired again", string(result.JSON()))
	}
	// Website maps cannot authorize another exploration, even within the origin.
	if result := f.invokeWebsite(t, "recursive-map", true, `{"url":"https://example.org/child-0"}`); !result.Err() || f.calls.Load() != 4 {
		t.Fatal("map became seed authority")
	}
	raw := assistantChatJSON(assistantRunLedger{Version: 1, Project: record.Binding.Project, Runs: []assistantRunRecord{record}})
	resumed, err := decodeAssistantRunStorage(f.e.writer.lease.store.name, raw)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(record.Websites, resumed.Runs[0].Websites) || !reflect.DeepEqual(record.Links, resumed.Runs[0].Links) {
		t.Fatal("resume lost site or website custody")
	}
}
func TestAssistantWebsiteMixedLimits(t *testing.T) {
	f := newAssistantWebsiteFixture(t, "api", "research", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	if r := f.invokeWebsite(t, "map", true, `{"url":"https://example.org/report"}`); r.Err() {
		t.Fatal(string(r.JSON()))
	}
	for i, url := range []string{"https://example.org/two", "https://example.org/three", "https://example.org/four"} {
		if r := f.invokeLink(t, fmt.Sprint("ordinary", i), true, string(assistantChatJSON(map[string]string{"url": url}))); r.Err() {
			t.Fatal("ordinary read refused", string(r.JSON()))
		}
	}
	if r := f.invokeLink(t, "ordinary-spent", true, `{"url":"https://éxample.org/über"}`); !r.Err() {
		t.Fatal("ordinary threshold expanded")
	}
	for i := range 7 {
		if r := f.invokeLink(t, fmt.Sprint("discovered", i), true, fmt.Sprintf(`{"url":"https://example.org/child-%d"}`, i)); r.Err() {
			t.Fatal("shared discovery threshold refused early", i, string(r.JSON()))
		}
	}
	calls := f.calls.Load()
	if r := f.invokeLink(t, "ten-spent", true, `{"url":"https://example.org/child-7"}`); !r.Err() || f.calls.Load() != calls {
		t.Fatal("discovery limit became additive")
	}
	if r := f.invokeLink(t, "free", true, `{"url":"https://example.org/child-0"}`); r.Err() || f.calls.Load() != calls {
		t.Fatal("retained read charged")
	}
}

func TestAssistantWebsiteActualRefusalEvents(t *testing.T) {
	for _, vector := range []struct {
		name, arguments, choice string
		noConfig                bool
		malformedMap            string
	}{
		{"empty", `{"url":""}`, "provided", false, ""},
		{"missing", `{}`, "provided", false, ""},
		{"oversized", string(assistantChatJSON(map[string]string{"url": "https://example.org/" + strings.Repeat("x", 4096)})), "provided", false, ""},
		{"undeclared", `{"url":"https://example.org/report","site":"https://example.org/"}`, "provided", false, ""},
		{"not-supplied", `{"url":"https://invented.org/"}`, "provided", false, ""},
		{"no-config", `{"url":"https://example.org/report"}`, "provided", true, ""},
		{"malformed-map", `{"url":"https://example.org/report"}`, "research", false, "structure"},
		{"oversized-map", `{"url":"https://example.org/report"}`, "research", false, "oversized"},
	} {
		t.Run(vector.name, func(t *testing.T) {
			var requests atomic.Int32
			upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				finish := "stop"
				delta := map[string]any{"role": "assistant", "content": "Search was refused."}
				if requests.Add(1) == 1 {
					finish = "tool_calls"
					delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "refused-search", "type": "function", "function": map[string]any{"name": "explore_website", "arguments": vector.arguments}}}}
				}
				raw := assistantChatJSON(map[string]any{"id": "refusal-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"refusal-fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", raw, finish)
			})
			f := newAssistantWebsiteFixture(t, "api", vector.choice, "work", upstream.server.URL)
			if vector.noConfig {
				assistantRunMust(t, os.WriteFile(f.s.deskConfigPath(), []byte(`{"deskConfigVersion":1,"research":{"documents":null}}`), 0600))
			}
			if vector.malformedMap != "" {
				f.resultChange = func(v map[string]any) {
					if vector.malformedMap == "structure" {
						v["pages"].([]any)[1].(map[string]any)["from"] = "https://example.org/missing"
					} else {
						v["extension"] = strings.Repeat("x", assistantWebsiteLimit)
					}
				}
			}
			x := assistantExecutionOpen(t, f.s, f.e)
			assistantRunMust(t, x.Adapter.Run(t.Context(), x.Run))
			record := assistantExecutionRecord(t, f.e)
			refused := false
			events := []json.RawMessage{}
			for _, held := range record.Events {
				raw, err := assistantRunEventBytes(held.Event)
				assistantRunMust(t, err)
				events = append(events, raw)
				if result := held.Event.ToolResult; result != nil && result.Name == assistantWebsiteTool {
					refused = result.IsError
				}
			}
			expected := int32(0)
			held := 0
			if vector.malformedMap != "" {
				expected = 1
				held = 1
			}
			if !refused || f.calls.Load() != expected || len(record.Websites) != held || len(record.DomainOutputs) != 0 {
				t.Fatal("refusal did not retain a zero-acquisition error event", refused, f.calls.Load(), record.Websites)
			}
			if dir := os.Getenv("DESK_WEBSITE_READER_OUTPUTS"); dir != "" {
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, "refusal-"+vector.name+".json"), assistantChatJSON(events), 0600))
			}
		})
	}
}

func TestAssistantWebsiteActualProducers(t *testing.T) {
	for _, engine := range []string{"api", "codex", "research-child"} {
		t.Run(engine, func(t *testing.T) {
			request := 0
			upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				request++
				finish := "stop"
				delta := map[string]any{"role": "assistant", "content": "Retained exact page evidence."}
				if request <= 2 {
					name, args := assistantWebsiteTool, `{"url":"example.org/report"}`
					if request == 2 {
						name, args = assistantLinkTool, `{"url":"https://example.org/child-0","find":"alpha beta"}`
					}
					finish = "tool_calls"
					delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprint("call", request), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}
				}
				raw := assistantChatJSON(map[string]any{"id": "website-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"website-fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", raw, finish)
			})
			actual := engine
			if actual == "research-child" {
				actual = "codex"
			}
			f := newAssistantWebsiteFixture(t, actual, "research", "work", upstream.server.URL)
			if engine != "api" {
				peer := strings.Replace(assistantExecutionCodexPeer, "else: tool(offered('get_schema'),{})", "else: tool(offered('explore_website'),{'url':'example.org/report'},call='map',rpc='map-rpc')", 1)
				peer = strings.Replace(peer, "elif not method:\n  log({'type':'answer','answer':q})", "elif not method:\n  log({'type':'answer','answer':q})\n  if q.get('id')=='map-rpc':\n   tool(offered('read_link'),{'url':'https://example.org/child-0','find':'alpha beta'},call='page',rpc='page-rpc');continue", 1)
				if engine == "research-child" {
					peer = strings.Replace(peer, "if mode=='delegate' and index==1:\n   tool(offered('delegate_tasks'),{'tasks':[{'title':'Inspect schema','objective':'Inspect the schema only.','profile':'review'}]})", `if mode=='normal' and index==1:
   tool(offered('delegate_tasks'),{'tasks':[{'title':'Explore supplied site','objective':'Explore only example.org/report and read its discovered child-0 page.','profile':'research'}]})
  elif mode=='normal' and index>=3: finish()`, 1)
					peer = strings.Replace(peer, "if mode=='delegate' and index==2:", "if mode=='normal' and index==2:", 1)
				}
				assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(f.trace), "peer.py"), []byte(peer), 0600))
			}
			x := assistantExecutionOpen(t, f.s, f.e)
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
			record := assistantExecutionRecord(t, f.e)
			if len(record.Websites) != 1 || len(record.Links) != 1 || len(record.DomainOutputs) != 2 || f.calls.Load() != 4 {
				t.Fatal("actual discovery/page producer missing", record.Websites, record.Links, record.DomainOutputs)
			}
			var in assistantHostIntent
			assistantRunMust(t, readAssistantHostReceipt(assistantDialectOperation(record, record.Websites[0].HostOperationID).Intent, &in))
			if engine == "research-child" && in.Root {
				t.Fatal("research child mislabeled root")
			}
			if engine != "api" && in.CallbackID == "" {
				t.Fatal("native callback lost")
			}
			if engine == "research-child" {
				// Handwritten resumed-refusal variant of the actual child result:
				// exact map receipts survive, while its original host result failed.
				var refused assistantRunRecord
				assistantRunMust(t, json.Unmarshal(assistantChatJSON(record), &refused))
				refused.State = "complete"
				refused.Websites[0].DispatchClosed = true
				for i := range refused.DomainOutputs {
					if refused.DomainOutputs[i].Website != nil {
						refused.DomainOutputs[i].Website.Recovered = true
					}
				}
				op := assistantDialectOperation(refused, refused.Websites[0].HostOperationID)
				var result assistantHostResult
				assistantRunMust(t, readAssistantHostReceipt(op.Result, &result))
				result.Result = assistantHostFailure(errors.New("fixture lost final acknowledgement")).Value().JSON()
				op.Result = assistantChatJSON(result)
				op.State = "refused"
				projected, err := compileAssistantChatProjection(f.e.source.Original, refused, "2026-10-21T00:00:00Z")
				assistantRunMust(t, err)
				var chat struct {
					Checkpoint struct {
						State struct {
							Responses []struct {
								Work struct {
									Items []struct {
										Name   string `json:"name"`
										Status string `json:"status"`
									} `json:"items"`
								} `json:"work"`
							} `json:"responses"`
						} `json:"state"`
					} `json:"checkpoint"`
				}
				assistantRunMust(t, json.Unmarshal(projected, &chat))
				found := false
				for _, item := range chat.Checkpoint.State.Responses[len(chat.Checkpoint.State.Responses)-1].Work.Items {
					if item.Name == assistantWebsiteTool {
						found = true
						if item.Status != "failed" {
							t.Fatal("recovered child refusal projected as completed work")
						}
					}
				}
				if !found {
					t.Fatal("recovered child work row missing")
				}
			}
			assistantRunMust(t, assistantRecipeImport(t, f.e, record))
			assistantRunMust(t, f.e.writer.State(t.Context(), "complete"))
			assistantRunMust(t, f.e.ProjectChat(t.Context()))
			assistantRunMust(t, f.e.ProjectChat(t.Context()))
			if f.calls.Load() != 4 {
				t.Fatal("projection repeated effects")
			}
			if dir := os.Getenv("DESK_WEBSITE_READER_OUTPUTS"); dir != "" {
				saved := policyWorkspaceRead(t, f.s, f.token, "/api/conversations")
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, engine+".json"), saved.Content, 0600))
				for _, held := range []assistantLinkCustody{assistantLinkCustody(record.Websites[0]), record.Links[0]} {
					kind := "page"
					if held.Discovery {
						kind = "website"
					}
					data, err := f.s.openChatData()
					assistantRunMust(t, err)
					conversation, err := resolveConversationName(data.root, f.s.conversationIdentity())
					assistantRunMust(t, err)
					packet, err := readPrivateData(data.root, attachmentProjectPrefix(conversation)+held.FinalID+".json", maxAttachmentObjectBytes)
					assistantRunMust(t, data.root.Close())
					assistantRunMust(t, err)
					assistantRunMust(t, os.WriteFile(filepath.Join(dir, engine+"-"+kind+".json"), packet, 0600))
					if kind == "page" {
						var receipt assistantHostResult
						assistantRunMust(t, readAssistantHostReceipt(assistantDialectOperation(record, held.HostOperationID).Result, &receipt))
						assistantRunMust(t, os.WriteFile(filepath.Join(dir, engine+"-window.json"), receipt.Result, 0600))
					}
				}
				pin := record.Websites[0].Gateway
				assistantRunMust(t, os.WriteFile(filepath.Join(dir, engine+"-pin.json"), assistantChatJSON(map[string]any{"url": pin.URL, "authority": pin.Authority, "signer": map[string]string{"algorithm": "ed25519", "public": pin.Public}}), 0600))
			}
		})
	}
}

type assistantWebsiteVectorFile struct {
	Base  json.RawMessage `json:"base"`
	Cases []struct {
		Name  string `json:"name"`
		Edits []struct {
			Path   []any `json:"path"`
			Value  any   `json:"value"`
			Remove bool  `json:"remove"`
		} `json:"edits"`
		Valid bool `json:"valid"`
	} `json:"cases"`
	Origins []struct {
		A    string `json:"a"`
		B    string `json:"b"`
		Same bool   `json:"same"`
	} `json:"origins"`
}

func TestAssistantWebsiteSharedVectors(t *testing.T) {
	if dir := os.Getenv("DESK_WEBSITE_READER_OUTPUTS"); dir != "" {
		e := &assistantRunExecution{}
		tools, err := e.assistantWebsiteTools()
		assistantRunMust(t, err)
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, "input-schema.json"), tools.tools[0].InputSchema, 0600))
	}
	raw, err := os.ReadFile("testdata/assistant-website-discovery.json")
	assistantRunMust(t, err)
	var vectors assistantWebsiteVectorFile
	assistantRunMust(t, json.Unmarshal(raw, &vectors))
	for _, v := range vectors.Cases {
		t.Run(v.Name, func(t *testing.T) {
			var value any
			assistantRunMust(t, json.Unmarshal(vectors.Base, &value))
			for _, edit := range v.Edits {
				at := value
				for _, key := range edit.Path[:len(edit.Path)-1] {
					switch key := key.(type) {
					case string:
						at = at.(map[string]any)[key]
					case float64:
						at = at.([]any)[int(key)]
					}
				}
				last := edit.Path[len(edit.Path)-1].(string)
				if edit.Remove {
					delete(at.(map[string]any), last)
				} else {
					at.(map[string]any)[last] = edit.Value
				}
			}
			_, err := readAssistantWebsiteDiscovery(assistantChatJSON(value), "https://example.org/report")
			if (err == nil) != v.Valid {
				t.Fatal("shared structural vector differs", v.Name, err)
			}
		})
	}
	for _, v := range vectors.Origins {
		if assistantSameOrigin(v.A, v.B) != v.Same {
			t.Fatal("shared origin vector differs", v)
		}
	}
}

func TestAssistantWebsiteLostStorageAcknowledgement(t *testing.T) {
	for _, stage := range []string{"stage", "final"} {
		t.Run(stage, func(t *testing.T) {
			f := newAssistantWebsiteFixture(t, "api", "research", "work", "http://127.0.0.1:1")
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
				matches := stage == "stage" && object.Original.Name == "chat-website-acquisition.json" || stage == "final" && object.Proof != nil
				if matches {
					failed = true
					return errors.New("fixture lost exact storage readback acknowledgement")
				}
				return nil
			}
			t.Cleanup(func() { testHookPrivateRead = nil })
			result := f.invokeWebsite(t, "uncertain-storage", true, `{"url":"https://example.org/report"}`)
			testHookPrivateRead = nil
			if !failed || !result.Err() {
				t.Fatal("storage uncertainty was not observed")
			}
			before := assistantExecutionRecord(t, f.e)
			if len(before.Websites) != 1 || !before.Websites[0].DispatchClosed {
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
			hostBefore := bytes.Clone(assistantDialectOperation(beforeGet, beforeGet.Websites[0].HostOperationID).Result)
			assistantRunMust(t, f.e.ProjectChat(t.Context()))
			recovered := assistantExecutionRecord(t, f.e)
			if stage == "final" {
				if recovered.Websites[0].Final.Receipt == nil || len(recovered.DomainOutputs) != 1 || !recovered.DomainOutputs[0].Website.Recovered || !bytes.Equal(hostBefore, assistantDialectOperation(recovered, beforeGet.Websites[0].HostOperationID).Result) {
					t.Fatal("exact final recovery changed original failure or lost custody")
				}
			} else if recovered.Websites[0].Stage.Receipt == nil || len(recovered.DomainOutputs) != 0 {
				t.Fatal("unverified stage recovery authorized links or lost custody")
			}
			if f.calls.Load() != expected {
				t.Fatal("explicit receipt reconciliation repeated external work")
			}

		})
	}
}

func assistantWebsiteSavedPacket(t *testing.T, seed string) (assistantWebsiteReference, []byte) {
	t.Helper()
	response, seal := assistantWebsiteSigned(t, "saved-website", assistantLinkRequest{URL: seed}, nil)
	var envelope struct {
		Result  json.RawMessage `json:"result"`
		Receipt struct {
			Digest string `json:"resultDigest"`
		} `json:"receipt"`
	}
	assistantRunMust(t, json.Unmarshal(response, &envelope))
	original, err := assistantWebsiteOriginal(envelope.Result)
	assistantRunMust(t, err)
	var object attachmentObject
	assistantRunMust(t, json.Unmarshal(original, &object))
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	object.Proof = &attachmentProof{Session: "saved-website", Source: assistantWebsiteSource, Authority: "gateway:test", PublicKey: hex.EncodeToString(key.Public().(ed25519.PublicKey)), Response: string(response), Registry: string(seal) + "\n"}
	return assistantWebsiteReference{"12345678-1234-1234-1234-123456789abc", envelope.Receipt.Digest, seed}, assistantChatJSON(object)
}
func assistantWebsiteFixturePacket(t *testing.T, f *assistantSearchFixture, id string, change func([]byte) []byte) []byte {
	t.Helper()
	data, err := f.s.openChatData()
	assistantRunMust(t, err)
	defer data.root.Close()
	name, err := resolveConversationName(data.root, f.s.conversationIdentity())
	assistantRunMust(t, err)
	path := attachmentProjectPrefix(name) + id + ".json"
	raw, err := readPrivateData(data.root, path, maxAttachmentObjectBytes)
	if change != nil {
		raw = change(raw)
		assistantRunMust(t, writePrivateData(data.root, path, raw))
	} else {
		assistantRunMust(t, err)
	}
	return raw
}
func TestAssistantWebsiteReplacementAndSavedAuthority(t *testing.T) {
	ref, raw := assistantWebsiteSavedPacket(t, "https://example.org/report")
	f := newAssistantWebsiteFixture(t, "api", "research", "work", "http://127.0.0.1:1", func(chat map[string]any) {
		refs := []any{ref}
		for i := range 15 {
			refs = append(refs, assistantWebsiteReference{fmt.Sprintf("12345678-1234-1234-1234-%012d", i), "sha256:" + strings.Repeat("a", 64), fmt.Sprintf("https://site%d.example.org/", i)})
		}
		chat["websites"] = refs
		state := chat["checkpoint"].(map[string]any)["state"].(map[string]any)
		user := state["turns"].([]any)[0].(map[string]any)["id"]
		state["responses"] = []any{map[string]any{"id": "old-map-response", "afterTurnId": user, "websites": []any{ref}, "documents": []any{}, "sourceIds": []any{}, "work": map[string]any{"items": []any{}, "notices": []any{}}}}
	})
	assistantWebsiteFixturePacket(t, f, ref.ID, func([]byte) []byte { return raw })
	_ = assistantExecutionOpen(t, f.s, f.e)
	// The new current-run map supersedes the old saved map for this seed.
	f.resultChange = func(value map[string]any) { value["pages"].([]any)[1].(map[string]any)["status"] = "blocked" }
	if r := f.invokeWebsite(t, "replace", true, `{"url":"https://example.org/report"}`); r.Err() {
		t.Fatal("same seed replacement refused at sixteen", string(r.JSON()))
	}
	f.resultChange = nil
	if r := f.invokeLink(t, "stale-child", true, `{"url":"https://example.org/child-0"}`); !r.Err() || f.calls.Load() != 2 {
		t.Fatal("stale map fallback authorized child")
	}
	run := assistantExecutionRecord(t, f.e)
	forged := assistantLinkCustody{Request: assistantLinkRequest{URL: "https://example.org/child-1", Site: ref.Seed}, Gateway: run.Websites[0].Gateway, AuthorityPacket: &attachmentReceipt{ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", SHA256: digestOf(raw)}}
	program, err := f.s.researchVerifier()
	assistantRunMust(t, err)
	if verifyAssistantWebsitePageAuthority(t.Context(), program, raw, run, f.e.source, forged) == nil {
		t.Fatal("unowned physical website ID authorized page")
	}
	f.e.cancel()
	<-f.e.done
	assistantRunMust(t, f.e.ProjectChat(t.Context()))
	saved := policyWorkspaceRead(t, f.s, f.token, "/api/conversations")
	var document conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &document))
	var chat struct {
		Websites   []assistantWebsiteReference `json:"websites"`
		Checkpoint struct {
			State struct {
				Responses []struct {
					Websites []assistantWebsiteReference `json:"websites"`
				} `json:"responses"`
			} `json:"state"`
		} `json:"checkpoint"`
	}
	assistantRunMust(t, json.Unmarshal(document.Chats[0], &chat))
	if len(chat.Websites) != 16 || chat.Websites[len(chat.Websites)-1].ID == ref.ID || chat.Checkpoint.State.Responses[0].Websites[0] != ref || !bytes.Equal(raw, assistantWebsiteFixturePacket(t, f, ref.ID, nil)) {
		t.Fatal("same seed replacement discarded immutable history")
	}
	restartAssistantLinkWithSavedChat(t, f, "fresh-map-budget", nil)
	if r := f.invokeWebsite(t, "fresh-explore", true, `{"url":"https://example.org/report"}`); !r.Err() || f.calls.Load() != 2 {
		t.Fatal("fresh admission reset exploration limit")
	}
	// A retained map and response history cannot supply their own seed.
	restartAssistantLinkWithSavedChat(t, f, "lost-seed", func(chat map[string]any) {
		state := chat["checkpoint"].(map[string]any)["state"].(map[string]any)
		for _, row := range state["turns"].([]any) {
			turn := row.(map[string]any)
			if turn["role"] == "user" {
				turn["text"] = "Read retained maps without a supplied address"
			}
		}
	})
	if r := f.invokeLink(t, "seed-lost", true, `{"url":"https://example.org/child-1"}`); !r.Err() || f.calls.Load() != 2 {
		t.Fatal("lost seed authority survived saved map")
	}
}
func TestAssistantWebsiteDistinctLimit(t *testing.T) {
	f := newAssistantWebsiteFixture(t, "api", "research", "work", "http://127.0.0.1:1", func(chat map[string]any) {
		refs := []assistantWebsiteReference{}
		for i := range 16 {
			refs = append(refs, assistantWebsiteReference{fmt.Sprintf("12345678-1234-1234-1234-%012d", i), "sha256:" + strings.Repeat("a", 64), fmt.Sprintf("https://site%d.example.org/", i)})
		}
		chat["websites"] = refs
	})
	_ = assistantExecutionOpen(t, f.s, f.e)
	if r := f.invokeWebsite(t, "seventeen", true, `{"url":"https://example.org/report"}`); !r.Err() || f.calls.Load() != 0 {
		t.Fatal("distinct seed limit omitted")
	}
}
func TestAssistantWebsiteQueuedAuthority(t *testing.T) {
	for _, which := range []string{"map-receipt", "seed-receipt", "research-preference"} {
		t.Run(which, func(t *testing.T) {
			f := newAssistantWebsiteFixture(t, "api", "research", "work", "http://127.0.0.1:1")
			_ = assistantExecutionOpen(t, f.s, f.e)
			seed := "https://example.org/report"
			calls := int32(2)
			if which == "seed-receipt" {
				seed = "https://news.example.net/report"
				calls = 4
				f.resultChange = func(v map[string]any) {
					v["hits"] = []any{map[string]any{"url": seed, "title": "Seed", "snippet": "Navigation lead"}}
				}
				if r := f.invoke(t, "seed-search", true); r.Err() {
					t.Fatal(string(r.JSON()))
				}
				f.resultChange = nil
			}
			if r := f.invokeWebsite(t, "map", true, string(assistantChatJSON(map[string]string{"url": seed}))); r.Err() {
				t.Fatal(string(r.JSON()))
			}
			u, _ := assistantPublicURL(seed)
			target := "https://" + u.Host() + "/child-0"
			for range cap(f.s.researchSlots) {
				f.s.researchSlots <- struct{}{}
			}
			done := make(chan runcontext.ToolResult, 1)
			go func() {
				done <- f.invokeLink(t, "queued", true, string(assistantChatJSON(map[string]string{"url": target})))
			}()
			deadline := time.Now().Add(8 * time.Second)
			for {
				run := assistantExecutionRecord(t, f.e)
				if len(run.Links) == 1 && run.Links[0].Acquire == "dispatched" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("discovered read did not queue")
				}
				time.Sleep(5 * time.Millisecond)
			}
			run := assistantExecutionRecord(t, f.e)
			if which == "research-preference" {
				assistantRunMust(t, os.WriteFile(filepath.Join(f.s.cfg.ProjectDir, "jpack-search.json"), []byte(`{"version":1,"connection":"demo","mode":"provided"}`), 0600))
			} else {
				id := run.Websites[0].FinalID
				if which == "seed-receipt" {
					id = run.Searches[0].FinalID
				}
				assistantWebsiteFixturePacket(t, f, id, func(b []byte) []byte { return append(b, ' ') })
			}
			<-f.s.researchSlots
			result := <-done
			for len(f.s.researchSlots) > 0 {
				<-f.s.researchSlots
			}
			if !result.Err() || f.calls.Load() != calls {
				t.Fatal("queued website authority changed but acquired", which, f.calls.Load(), string(result.JSON()))
			}
		})
	}
}

func TestAssistantWebsiteMembersAndEnums(t *testing.T) {
	f := newAssistantWebsiteFixture(t, "api", "research", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	seed := "https://news.example.net/report"
	f.resultChange = func(v map[string]any) {
		v["hits"] = []any{map[string]any{"url": seed, "title": "Seed", "snippet": "Navigation lead"}}
	}
	if r := f.invoke(t, "search", true); r.Err() {
		t.Fatal(string(r.JSON()))
	}
	f.resultChange = nil
	if r := f.invokeWebsite(t, "map", true, string(assistantChatJSON(map[string]string{"url": seed}))); r.Err() {
		t.Fatal(string(r.JSON()))
	}
	if r := f.invokeLink(t, "page", true, `{"url":"https://news.example.net/child-0"}`); r.Err() {
		t.Fatal(string(r.JSON()))
	}
	run := assistantExecutionRecord(t, f.e)
	if run.Links[0].SeedAuthorityPacket == nil {
		t.Fatal("seed proof receipt not persisted")
	}
	raw := assistantChatJSON(assistantRunLedger{Version: 1, Project: run.Binding.Project, Runs: []assistantRunRecord{run}})
	restored, err := decodeAssistantRunStorage(f.e.writer.lease.store.name, raw)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(run.Websites, restored.Runs[0].Websites) || !reflect.DeepEqual(run.Links, restored.Runs[0].Links) || !reflect.DeepEqual(run.DomainOutputs, restored.Runs[0].DomainOutputs) {
		t.Fatal("resume lost website checkpoint fields")
	}
	count := 0
	var omit func([]byte, reflect.Type)
	omit = func(raw []byte, shape reflect.Type) {
		for shape.Kind() == reflect.Pointer {
			shape = shape.Elem()
		}
		if shape.Kind() != reflect.Struct {
			return
		}
		var fields map[string]json.RawMessage
		assistantRunMust(t, json.Unmarshal(raw, &fields))
		for i := range shape.NumField() {
			field := shape.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			value, ok := fields[name]
			if !ok {
				continue
			}
			scratch := []reflect.StructField{}
			for j := range shape.NumField() {
				if j != i {
					scratch = append(scratch, shape.Field(j))
				}
			}
			if readAssistantDialectCustody(raw, reflect.StructOf(scratch)) == nil {
				t.Fatal("private schema member omission survived", shape.Name(), name)
			}
			count++
			nested := field.Type
			for nested.Kind() == reflect.Pointer {
				nested = nested.Elem()
			}
			if nested.Kind() == reflect.Struct {
				omit(value, field.Type)
			}
		}
	}
	held := run.Websites[0]
	held.DispatchClosed = true
	out := *run.DomainOutputs[1].Website
	out.Recovered = true
	omit(assistantChatJSON(held), reflect.TypeOf(assistantWebsiteCustody{}))
	omit(assistantChatJSON(run.Links[0]), reflect.TypeOf(assistantLinkCustody{}))
	omit(assistantChatJSON(out), reflect.TypeOf(assistantWebsiteOutput{}))
	record := assistantWebsiteFixtureDiscovery(seed)
	rawRecord := assistantChatJSON(record)
	omit(rawRecord, reflect.TypeOf(assistantWebsiteDiscovery{}))
	omit(assistantChatJSON(record["pages"].([]any)[0]), reflect.TypeOf(assistantWebsitePage{}))
	// Remove each new finite enum from a scratch copy of the actual reader schema.
	schema := assistantWebsiteEnumSchema()
	for _, status := range schema.Statuses {
		v := assistantWebsiteFixtureDiscovery(seed)
		v["pages"].([]any)[1].(map[string]any)["status"] = status
		scratch := assistantWebsiteEnumSchema()
		scratch.Statuses = nil
		for _, other := range schema.Statuses {
			if other != status {
				scratch.Statuses = append(scratch.Statuses, other)
			}
		}
		if _, err := readAssistantWebsiteWithEnums(assistantChatJSON(v), seed, scratch); err == nil {
			t.Fatal("status omission survived", status)
		}
	}
	for _, stop := range schema.Stops {
		v := assistantWebsiteFixtureDiscovery(seed)
		v["stopReason"] = stop
		scratch := assistantWebsiteEnumSchema()
		scratch.Stops = nil
		for _, other := range schema.Stops {
			if other != stop {
				scratch.Stops = append(scratch.Stops, other)
			}
		}
		if _, err := readAssistantWebsiteWithEnums(assistantChatJSON(v), seed, scratch); err == nil {
			t.Fatal("stop omission survived", stop)
		}
	}
	for _, field := range []string{"websites", "website"} {
		var wire map[string]any
		assistantRunMust(t, json.Unmarshal(raw, &wire))
		r := wire["runs"].([]any)[0].(map[string]any)
		if field == "websites" {
			delete(r, field)
		} else {
			for _, o := range r["domainOutputs"].([]any) {
				out := o.(map[string]any)
				if out["kind"] == "website" {
					delete(out, field)
				}
			}
		}
		if _, err := decodeAssistantRunStorage(f.e.writer.lease.store.name, assistantChatJSON(wire)); err == nil {
			t.Fatal("resume member omission survived", field)
		}
	}
	t.Logf("website private schema: %d nested member omissions; four statuses/eight stop reasons; exact resumed receipts/site fields", count)
}

func TestAssistantWebsiteCancelAfterAcquisition(t *testing.T) {
	f := newAssistantWebsiteFixture(t, "api", "research", "work", "http://127.0.0.1:1")
	_ = assistantExecutionOpen(t, f.s, f.e)
	f.after = func(path string) {
		if path == "acquire" {
			f.e.cancel()
		}
	}
	result := f.invokeWebsite(t, "cancelled-search", true, `{"url":"https://example.org/report"}`)
	if !result.Err() || f.acquisitions.Load() != 1 || f.seals.Load() != 0 {
		t.Fatal("cancelled acquisition continued", f.calls.Load())
	}
	record := assistantExecutionRecord(t, f.e)
	if len(record.Websites) != 1 || record.Websites[0].Stage.Receipt == nil || record.Websites[0].Final.Intent != nil || len(record.DomainOutputs) != 0 {
		t.Fatal("completed acquisition custody was lost", record.Websites)
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

func TestAssistantWebsiteUnknownExternalOutcome(t *testing.T) {
	for _, stage := range []string{"acquire", "seal"} {
		t.Run(stage, func(t *testing.T) {
			f := newAssistantWebsiteFixture(t, "api", "research", "work", "http://127.0.0.1:1")
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
			result := f.invokeWebsite(t, "unknown", true, `{"url":"https://example.org/report"}`)
			relayTransport = original
			if !failed || !result.Err() {
				t.Fatal("unknown external effect was accepted")
			}
			<-f.e.done
			run := assistantExecutionRecord(t, f.e)
			if len(run.Websites) != 1 || !run.Websites[0].DispatchClosed || !assistantLinkPending(assistantLinkCustody(run.Websites[0])) {
				t.Fatal("unknown dispatch not retained")
			}
			// A crash need not have written DispatchClosed: durable intent alone fences
			// fresh admission, and receipt observation never repeats an external effect.
			copy := run.Websites[0]
			copy.DispatchClosed = false
			if !assistantLinkPending(assistantLinkCustody(copy)) {
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
				held.Websites[0].DispatchClosed = false
				held.Revision++
				return true, nil
			})
			assistantRunMust(t, err)
			restartAssistantLinkWithSavedChat(t, f, "crash-intent-"+stage, nil)
			if result := f.invokeWebsite(t, "fresh-after-crash", true, `{"url":"https://example.org/two"}`); !result.Err() || f.calls.Load() != calls {
				t.Fatal("fresh admission replayed an unresolved durable intent")
			}

		})
	}
}
