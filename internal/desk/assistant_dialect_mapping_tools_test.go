package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The model fixtures are authored independently of produced checks. Runtime is
// the pinned, offline executable, not a simulated dialect evaluator.
func assistantMappingSeed(t *testing.T, s *Server) inputPreparationReply {
	t.Helper()
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "mapped.json"), []byte(qualificationTestPack), 0600))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "jpack.json"), []byte(`{"packs":{"example":{"path":"mapped.json"}},"graphs":{}}`), 0600))
	d := repositoryDocument(1)
	d.Recipe.Qualification = qualificationFixture()
	d.Records[0].ID = "row_" + strings.Repeat("1", 64)
	d.Records[0].Values = map[string]string{"Country": "CA", "License": "present", "Amount": "1.20", "Flag": "no", "Empty": "", "Zero": "0"}
	d.Records[0].SourceID = "source_" + strings.Repeat("2", 64)
	d.Records[0].Row = 1
	d.Imports = []inputImport{{ID: d.Records[0].SourceID, Kind: "csv", Name: "Original", Digest: "sha256:" + digestOf([]byte("Country,License\nCA,present\n")), Content: "Country,License\nCA,present\n", At: d.CreatedAt, RecipeVersion: 1}}
	saved, err := s.preparationRepository().CompareAndSwap(t.Context(), d, "")
	assistantRunMust(t, err)
	return saved
}
func assistantMappingArguments(saved inputPreparationReply, name string) string {
	if strings.Contains(name, "publication") {
		a := map[string]any{"id": saved.Document.ID, "variant": "asserted"}
		if strings.HasPrefix(name, "propose") {
			a["revision"] = saved.SHA256
			a["version"] = "1.0.0"
			a["impact"] = artifactImpact{"initial", "Review original asserted fields <record>& 中文"}
		}
		return string(assistantChatJSON(a))
	}
	a := map[string]any{"id": saved.Document.ID, "packKey": "example"}
	if strings.HasPrefix(name, "propose") {
		a["revision"] = saved.SHA256
		a["packDigest"] = qualificationFixture().Digest
		a["summary"] = "Retain exact country and evidence <record>& 中文"
		a["facts"] = []any{map[string]any{"target": "/company/country", "column": "Country", "conversion": map[string]string{"kind": "text"}}}
		a["evidence"] = []any{map[string]string{"requirement": "license", "column": "License"}}
	}
	return string(assistantChatJSON(a))
}

// Only explicitly supplied offline fixtures are eligible. An explicit bad
// dedicated fixture fails; it must never silently select the operator fallback.
func assistantMappingBinary(t *testing.T) string {
	t.Helper()
	bin, configured := os.LookupEnv("DESK_DIALECT_MAPPING_RUNTIME")
	if !configured {
		bin, configured = os.LookupEnv("JPACK_BIN")
	}
	if !configured {
		t.Skip("supply the pinned offline Dialect mapping Runtime fixture")
	}
	if !filepath.IsAbs(bin) {
		t.Fatal("Runtime fixture must be an explicit absolute path")
	}
	info, err := os.Stat(bin)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		t.Fatal("configured Runtime fixture is unavailable", err)
	}
	return bin
}

func assistantMappingOpen(t *testing.T, s *Server, e *assistantRunExecution) *assistantExecution {
	t.Helper()
	bin := assistantMappingBinary(t)
	x := assistantExecutionOpen(t, s, e)
	// The MCP process has already started from its own peer. New subprocesses
	// enter the exact offline Runtime; no installed program is discovered by name.
	wrapper := filepath.Join(t.TempDir(), "mapping-runtime")
	script := "#!/usr/bin/env python3\nimport os,sys\nos.execv(" + fmt.Sprintf("%q", bin) + ",[" + fmt.Sprintf("%q", bin) + "]+sys.argv[1:])\n"
	assistantRunMust(t, os.WriteFile(wrapper, []byte(script), 0700))
	s.cfg.JpackBin = wrapper
	return x
}
func assistantMappingObserve(t *testing.T, s *Server, e *assistantRunExecution, want int) assistantRunRecord {
	t.Helper()
	r := assistantExecutionRecord(t, e)
	if len(r.DomainOutputs) != want {
		t.Fatalf("outputs=%d want=%d writer=%v", len(r.DomainOutputs), want, e.writer.Err())
	}
	for _, out := range r.DomainOutputs {
		if out.DialectAction == nil || !out.DialectAction.Ready {
			t.Fatalf("not ready: %+v", out)
		}
		if out.DialectAction.Action.Kind == "publish-dialect" {
			assistantRunMust(t, s.checkAssistantDialectPublicationEvidence(t.Context(), *out.DialectAction))
		}
	}
	assistantRunMust(t, assistantRecipeImport(t, e, r))
	return r
}
func TestAssistantDialectMappingActualAPIProducer(t *testing.T) {
	var step atomic.Int32
	var saved inputPreparationReply
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := step.Add(1)
		finish := "tool_calls"
		var delta map[string]any
		names := []string{"read_dialect", "propose_dialect", "read_dialect_publication", "propose_dialect_publication"}
		if n <= 4 {
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "same-provider-call", "type": "function", "function": map[string]any{"name": names[n-1], "arguments": assistantMappingArguments(saved, names[n-1])}}}}
		} else {
			delta = map[string]any{"role": "assistant", "content": "Review exact mapping and publication checks. Nothing was saved or published."}
			finish = "stop"
		}
		raw, _ := json.Marshal(map[string]any{"id": "mapping-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+string(raw)+"\n\ndata: {\"id\":\"mapping-fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\""+finish+"\"}]}\n\ndata: [DONE]\n\n")
	})
	s, token, e := assistantRecipeBrowserFixture(t, u.server.URL)
	saved = assistantMappingSeed(t, s)
	for _, name := range []string{"read_dialect", "read_dialect_publication"} {
		a, err := readAssistantDialectArguments(name, []byte(assistantMappingArguments(saved, name)))
		assistantRunMust(t, err)
		read, err := e.readAssistantDialect(t.Context(), a, strings.Contains(name, "publication"))
		assistantRunMust(t, err)
		if !validAssistantDialectRead(read) {
			t.Fatalf("safe read invalid %s: %s", name, read.View)
		}
	}
	x := assistantMappingOpen(t, s, e)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	if step.Load() != 5 {
		t.Fatal("actual four-tool loop did not finish", step.Load())
	}
	record := assistantMappingObserve(t, s, e, 2)
	after, err := s.preparationRepository().Get(t.Context(), saved.Document.ID)
	assistantRunMust(t, err)
	if after.SHA256 != saved.SHA256 {
		t.Fatal("check changed preparation")
	}
	ledger, err := s.readDialectPublicationLedger()
	assistantRunMust(t, err)
	if len(ledger.Items) != 0 {
		t.Fatal("check published an artifact")
	}
	for _, name := range assistantDialectNames {
		if !bytes.Contains(u.arrivals()[0].body, []byte(name)) {
			t.Fatal("actual offer absent", name)
		}
	}
	assistantRunMust(t, e.writer.State(t.Context(), "complete"))
	assistantRunMust(t, s.prepareAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
	assistantRunMust(t, s.writeAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	assistantRunMust(t, e.ProjectChat(t.Context()))
	if step.Load() != 5 {
		t.Fatal("ack replay called model")
	}
	chat := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(chat.Content, &doc))
	var fields map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(doc.Chats[0], &fields))
	actions, err := assistantChatArray(fields["inputActions"])
	assistantRunMust(t, err)
	if len(actions) != 2 {
		t.Fatal("actual actions absent", string(fields["inputActions"]))
	}
	if path := os.Getenv("DESK_DIALECT_MAPPING_READER_CHAT"); path != "" {
		assistantRunMust(t, os.WriteFile(path, chat.Content, 0600))
	}
	if path := os.Getenv("DESK_DIALECT_MAPPING_READER_SOURCE"); path != "" {
		assistantRunMust(t, os.WriteFile(path, assistantChatJSON(saved), 0600))
	}
	if path := os.Getenv("DESK_DIALECT_MAPPING_READER_REVIEW"); path != "" {
		p := record.DomainOutputs[1].DialectAction.Action.Publication
		review, err := s.artifactReview(t.Context(), *p.Answer.Review)
		assistantRunMust(t, err)
		assistantRunMust(t, os.WriteFile(path, assistantChatJSON(review), 0600))
	}
	// Existing applied state and unrelated actions survive exact acknowledgement.
	fields["inputActions"] = append([]byte(nil), fields["inputActions"]...)
	var originals []map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(fields["inputActions"], &originals))
	originals[0]["applied"] = assistantChatJSON(map[string]string{"at": "2026-10-20T00:00:00Z"})
	fields["inputActions"] = assistantChatJSON(originals)
	assistantRunMust(t, appendAssistantDialectActions(fields, record.DomainOutputs))
	if !bytes.Contains(fields["inputActions"], []byte(`"applied":{"at":"2026-10-20T00:00:00Z"}`)) {
		t.Fatal("applied state lost")
	}
	for _, kind := range []string{"read", "metadata", "candidate", "check", "preview", "mixed"} {
		t.Run("import-"+kind, func(t *testing.T) {
			bad := assistantVariantClone(t, record)
			p := bad.DomainOutputs[0].DialectAction
			switch kind {
			case "read":
				p.Read.Revision = strings.Repeat("a", 64)
			case "metadata":
				p.Action.ID = "dialect-action-forged"
			case "candidate":
				p.Action.Dialect.Facts[0].Column = "Zero"
			case "check":
				p.CheckID = record.DomainOutputs[1].DialectAction.CheckID
			case "preview":
				p.Preview.RecordCount = 0
			case "mixed":
				p.Action.Publication = record.DomainOutputs[1].DialectAction.Action.Publication
			}
			if assistantRecipeImport(t, e, bad) == nil {
				t.Fatal("invalid custody accepted")
			}
		})
	}
}
func TestAssistantDialectMappingActualNativeParentChild(t *testing.T) {
	for _, publication := range []bool{false, true} {
		t.Run(fmt.Sprint(publication), func(t *testing.T) {
			s, _, e, _, trace := assistantCodexFixture(t, "delegate", "work")
			saved := assistantMappingSeed(t, s)
			read, propose := "read_dialect", "propose_dialect"
			if publication {
				read, propose = "read_dialect_publication", "propose_dialect_publication"
			}
			peer := strings.Replace(assistantExecutionCodexPeer, "if mode=='delegate' and index==1:\n   tool(offered('delegate_tasks'),{'tasks':[{'title':'Inspect schema','objective':'Inspect the schema only.','profile':'review'}]})", `if mode=='delegate' and index==1: tool(offered('`+read+`'),json.loads(`+fmt.Sprintf("%q", assistantMappingArguments(saved, read))+`),call='same-provider-call')
  elif mode=='delegate' and index==2: tool(offered('`+read+`'),json.loads(`+fmt.Sprintf("%q", assistantMappingArguments(saved, read))+`),call='same-provider-call')
  elif mode=='delegate' and index==3: tool(offered('`+propose+`'),json.loads(`+fmt.Sprintf("%q", assistantMappingArguments(saved, propose))+`),call='same-provider-call')`, 1)
			peer = strings.Replace(peer, "if mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')", `if mode=='delegate' and index==1: tool(offered('delegate_tasks'),{'tasks':[{'title':'Read Dialect','objective':'Read the explicit Dialect only.','profile':'review'}]},call='delegate',rpc='rpc-delegate')
  elif mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')`, 1)
			assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
			x := assistantMappingOpen(t, s, e)
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
			r := assistantMappingObserve(t, s, e, 1)
			roots, children := 0, 0
			callbacks := map[string]bool{}
			for _, op := range r.Operations {
				if op.Kind == "host" {
					var in assistantHostIntent
					assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
					if in.Root {
						roots++
					} else {
						children++
					}
					if in.CallbackID == "" || callbacks[in.CallbackID] {
						t.Fatal("callback identity collapsed")
					}
					callbacks[in.CallbackID] = true
				}
			}
			if roots != 2 || children != 1 {
				t.Fatal("native custody absent", roots, children)
			}
		})
	}
}
func TestAssistantDialectMappingArguments(t *testing.T) {
	saved := inputPreparationReply{Document: repositoryDocument(1), SHA256: strings.Repeat("a", 64)}
	for _, name := range assistantDialectNames {
		good := assistantMappingArguments(saved, name)
		if _, err := readAssistantDialectArguments(name, []byte(good)); err != nil {
			t.Fatal(name, err)
		}
		for _, key := range []string{"ID", "action", "reviewed", "review", "basePublication", "checkDigest", "dialectAction", "recipeProposal", "unknown"} {
			raw := strings.TrimSuffix(good, "}") + `,"` + key + `":null}`
			if _, err := readAssistantDialectArguments(name, []byte(raw)); err == nil {
				t.Fatal("foreign argument", name, key)
			}
		}
	}
	good := assistantMappingArguments(saved, "propose_dialect")
	for _, change := range []string{`"facts":null`, `"facts":[null]`, `"facts":[{"target":"/company/country","column":"Country","conversion":{"kind":"text","trueValue":null}}]`, `"facts":[{"target":"/company/country","column":"Country","conversion":{"kind":null}}]`, `"facts":[{"target":"/company/country","column":null,"conversion":{"kind":"text"}}]`, `"evidence":[{"requirement":null,"column":"License"}]`} {
		var fields map[string]json.RawMessage
		assistantRunMust(t, json.Unmarshal([]byte(good), &fields))
		pair := strings.SplitN(change, ":", 2)
		fields[strings.Trim(pair[0], `"`)] = json.RawMessage(pair[1])
		if _, err := readAssistantDialectArguments("propose_dialect", assistantChatJSON(fields)); err == nil {
			t.Fatal("raw type accepted", change)
		}
	}
	for _, raw := range []string{`{"status":"accepted","diagnostics":null}`, `{"status":"accepted","diagnostics":[{"code":null,"message":"x"}]}`, `{"status":"accepted","diagnostics":[],"compatibility":{"notes":[{"code":"x","kind":"x","target":"x","severity":null}]}}`} {
		if validAssistantDialectReport([]byte(raw)) {
			t.Fatal("raw report type accepted")
		}
	}
}
func TestAssistantDialectMappingPlanStaleAndNoRead(t *testing.T) {
	for _, name := range []string{"propose_dialect", "propose_dialect_publication"} {
		for _, scenario := range []string{"plan", "no-read", "stale-preparation", "stale-pack"} {
			t.Run(name+"/"+scenario, func(t *testing.T) {
				phase := "work"
				if scenario == "plan" {
					phase = "plan"
				}
				s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", phase)
				saved := assistantMappingSeed(t, s)
				x := assistantMappingOpen(t, s, e)
				if phase == "plan" {
					for _, tool := range x.Run.HostTools {
						if assistantHostProposal(tool.Name) {
							t.Fatal("Plan proposal offer")
						}
					}
					return
				}
				if scenario != "no-read" {
					read := "read_dialect"
					if strings.Contains(name, "publication") {
						read = "read_dialect_publication"
					}
					if assistantRecipeInvoke(t, e, x, "read", read, assistantMappingArguments(saved, read)).Err() {
						t.Fatal("read failed")
					}
				}
				if scenario == "stale-preparation" {
					d := saved.Document
					d.Name = "New name"
					_, err := s.preparationRepository().CompareAndSwap(t.Context(), d, saved.SHA256)
					assistantRunMust(t, err)
				}
				if scenario == "stale-pack" {
					if strings.Contains(name, "publication") {
						d := saved.Document
						d.Recipe.Intent = "Changed recipe"
						_, err := s.preparationRepository().CompareAndSwap(t.Context(), d, saved.SHA256)
						assistantRunMust(t, err)
					} else {
						assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "mapped.json"), []byte(qualificationTestPack+"\n"), 0600))
					}
				}
				var result runcontext.ToolResult
				if scenario == "no-read" {
					call := "proposal"
					raw := json.RawMessage(assistantMappingArguments(saved, name))
					assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: name, Args: raw}}))
					ctx := context.WithValue(runcontext.WithCallID(t.Context(), call), assistantHostRootKey{}, true)
					var err error
					result, err = assistantGraphFind(t, x.Run.HostTools, name).Execute(ctx, raw)
					assistantRunMust(t, err)
				} else {
					result = assistantRecipeInvoke(t, e, x, "proposal", name, assistantMappingArguments(saved, name))
				}
				if !result.Err() {
					t.Fatal("proposal without current read accepted")
				}
				if len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
					t.Fatal("refusal produced action")
				}
			})
		}
	}
}

func TestAssistantDialectMappingTypedPreview(t *testing.T) {
	for _, tc := range []struct{ kind, raw string }{{"text", `""`}, {"integer", `0`}, {"decimal", `"1.20"`}, {"boolean", `false`}, {"text-list", `[]`}, {"text-list", `["a","b"]`}, {"range", `{"min":"0.00","max":"1.01"}`}} {
		t.Run(tc.kind+tc.raw, func(t *testing.T) {
			doc, row := typedResearchFixture(t, tc.kind, tc.raw)
			doc.ID = "prep_" + strings.Repeat("1", 32)
			doc.Records = []preparedInputRecord{row}
			pack := strings.Replace(qualificationTestPack, "/company/country", "/value", 1)
			read := assistantDialectRead{ID: doc.ID, Revision: strings.Repeat("a", 64), PackKey: "example", Pack: []byte(pack)}
			a := assistantDialectArguments{ID: doc.ID, Revision: read.Revision, PackKey: "example", PackDigest: "sha256:" + digestOf(read.Pack), Facts: []assistantDialectFact{{"/value", "value", inputConversion{Kind: "research-value"}}}, Evidence: []assistantDialectEvidence{}, Summary: "Preserve original typed research"}
			q, err := assistantDialectQualification(doc, a, read)
			assistantRunMust(t, err)
			doc.Recipe.Qualification = q
			preview, err := prepareInputRecord(doc, row)
			assistantRunMust(t, err)
			raw, err := json.Marshal(preview.Prepared["/value"])
			assistantRunMust(t, err)
			if string(raw) != tc.raw || preview.Origin != "generated" || len(preview.Input) != 0 {
				t.Fatal("typed value promoted or changed", string(raw), preview)
			}
			for _, status := range []string{"missing", "conflicting", "inaccessible", "stale"} {
				v := row.Research.Entity.Fields["value"]
				v.Status = status
				row.Research.Entity.Fields["value"] = v
				p, err := prepareInputRecord(doc, row)
				assistantRunMust(t, err)
				if len(p.Prepared) != 0 {
					t.Fatal("unknown became a fact")
				}
			}
		})
	}
}
func TestAssistantDialectMappingPublicationUncertaintyStops(t *testing.T) {
	var step atomic.Int32
	var saved inputPreparationReply
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := step.Add(1)
		finish := "stop"
		delta := map[string]any{"role": "assistant", "content": "No more work"}
		if n <= 2 {
			name := []string{"read_dialect_publication", "propose_dialect_publication"}[n-1]
			finish = "tool_calls"
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "same-provider-call", "type": "function", "function": map[string]any{"name": name, "arguments": assistantMappingArguments(saved, name)}}}}
		}
		raw, _ := json.Marshal(map[string]any{"id": "uncertain", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+string(raw)+"\n\ndata: {\"id\":\"uncertain\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\""+finish+"\"}]}\n\ndata: [DONE]\n\n")
	})
	s, token, e := assistantRecipeBrowserFixture(t, u.server.URL)
	saved = assistantMappingSeed(t, s)
	x := assistantMappingOpen(t, s, e)
	s.retainedArtifacts = assistantMappingArtifactOwner{base: s.artifactRepository(), fail: true}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_ = x.Adapter.Run(ctx, x.Run)
	if step.Load() != 2 || e.ctx.Err() == nil {
		t.Fatal("unknown retention continued model", step.Load(), e.ctx.Err())
	}
	r := assistantExecutionRecord(t, e)
	if len(r.DomainOutputs) != 0 {
		t.Fatal("uncertainty produced ready action")
	}
	found := false
	for _, op := range r.Operations {
		if op.Kind == "host" && op.Name == "propose_dialect_publication" {
			var held assistantHostResult
			assistantRunMust(t, readAssistantHostReceipt(op.Result, &held))
			v, err := runcontext.ParseJSON(held.Result)
			assistantRunMust(t, err)
			f := runcontext.ReadToolResult(v).StructuredContent.Get("failure")
			if f.Get("code").Str() != "storage" || f.Get("outcome").Str() != "unknown" {
				t.Fatal("uncertain retention mislabeled", string(held.Result))
			}
			found = true
		}
	}
	if !found {
		t.Fatal("durable failure absent")
	}
	if w := policyCall(s, token, "GET", "/api/assistant-runs/"+r.ID, nil); w.Code != 200 {
		t.Fatal("historical inspection blocked", w.Body.String())
	}
	assistantRunMust(t, assistantRecipeImport(t, e, r))
	ledger, err := s.readDialectPublicationLedger()
	assistantRunMust(t, err)
	if len(ledger.Items) != 0 {
		t.Fatal("unknown check published")
	}
	root, err := s.openRetainedArtifacts(false)
	assistantRunMust(t, err)
	defer root.Close()
	f, err := root.Open(".")
	assistantRunMust(t, err)
	defer f.Close()
	entries, err := f.ReadDir(-1)
	assistantRunMust(t, err)
	if len(entries) < 2 {
		t.Fatal("uncertainty fixture did not retain physical evidence")
	}
}
func TestAssistantDialectMappingPublicationCompletedReconciliation(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	saved := assistantMappingSeed(t, s)
	s.cfg.JpackBin = assistantMappingBinary(t)
	published := checkAndPublishDialect(t, s, saved, "asserted", "1.0.0")
	x := assistantMappingOpen(t, s, e)
	if assistantRecipeInvoke(t, e, x, "read", "read_dialect_publication", assistantMappingArguments(saved, "read_dialect_publication")).Err() {
		t.Fatal("read refused")
	}
	changed := saved.Document
	changed.Name = "Edited after completed publication"
	_, err := s.preparationRepository().CompareAndSwap(t.Context(), changed, saved.SHA256)
	assistantRunMust(t, err)
	s.cfg.JpackBin = filepath.Join(t.TempDir(), "absent-runtime")
	result := assistantRecipeInvoke(t, e, x, "completed", "propose_dialect_publication", assistantMappingArguments(saved, "propose_dialect_publication"))
	if result.Err() || !strings.Contains(result.Content[0].Text(), "already-published") {
		t.Fatal("exact completed observation rechecked source", result.Content)
	}
	r := assistantExecutionRecord(t, e)
	if len(r.DomainOutputs) != 1 || r.DomainOutputs[0].DialectAction.Published == nil || r.DomainOutputs[0].DialectAction.Published.Publication.ID != published.Publication.ID || r.DomainOutputs[0].DialectAction.Action != nil {
		t.Fatal("completed observation became a new action")
	}
	assistantRunMust(t, assistantRecipeImport(t, e, r))
}
