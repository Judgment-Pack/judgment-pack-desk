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
	"sync"
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
func assistantDialectMappingActualAPIProducer(t *testing.T, monitor *dialectMonitorRegression) {
	if monitor != nil {
		monitor.install(t)
	}
	var step atomic.Int32
	var saved inputPreparationReply
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := step.Add(1)
		finish := "tool_calls"
		var delta map[string]any
		names := []string{"read_dialect", "propose_dialect", "read_dialect_publication", "propose_dialect_publication"}
		if n <= 4 && (monitor == nil || !monitor.replace || n == 1) {
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
	if monitor != nil {
		monitor.bind(t, s, e)
	}
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
	if monitor == nil {
		assistantRunMust(t, x.Adapter.Run(e.generationContext(ctx, e.generation), x.Run))
	} else {
		monitor.run(t, s, e, x, ctx)
		if monitor.replace {
			monitor.requireRefused(t, s, e, step.Load())
			return
		}
		monitor.requireBenign(t)
	}
	if step.Load() != 5 {
		// Keep the failed integration gate actionable without dumping request
		// headers, credentials, source documents or unbounded context history.
		bounded := func(raw []byte) string {
			const limit = 2048
			if len(raw) > limit {
				return string(raw[:limit]) + " [truncated]"
			}
			return string(raw)
		}
		failed, readErr := e.writer.lease.store.Read(t.Context(), e.record.Binding, e.record.ID)
		t.Logf("actual API diagnostic: writer=%v read=%v state=%s canonicalRequired=%t canonicalActive=%t", e.writer.Err(), readErr, failed.State, failed.CanonicalRequired, assistantCanonicalActive(failed))
		if cp := failed.Checkpoint; cp != nil {
			reason := ""
			if cp.Reason != nil {
				reason = *cp.Reason
			}
			t.Logf("actual API checkpoint: status=%s phase=%s used=%d limit=%d reason=%s explanation=%s", cp.Status, cp.Phase, cp.Used, cp.Limit, bounded([]byte(reason)), bounded([]byte(cp.Explanation)))
		}
		for i, op := range failed.Operations {
			if i < len(failed.Operations)-12 {
				continue
			}
			var in assistantHostIntent
			if op.Kind == "host" && readAssistantHostReceipt(op.Intent, &in) == nil {
				t.Logf("actual API host: id=%s name=%s state=%s call=%s callback=%s root=%t dispatch=%s action=%s", op.ID, op.Name, op.State, in.CallID, in.CallbackID, in.Root, in.DurableDispatchID, bounded(assistantChatJSON(in.DialectAction)))
			} else {
				t.Logf("actual API operation: id=%s kind=%s name=%s state=%s call=%s", op.ID, op.Kind, op.Name, op.State, op.CallID)
			}
			var result assistantHostResult
			if op.Kind == "host" && readAssistantHostReceipt(op.Result, &result) == nil {
				t.Logf("actual API host result: %s", bounded(result.Result))
			}
		}
		for i, event := range failed.Events {
			if i < len(failed.Events)-12 {
				continue
			}
			if event.Event.Error != nil || event.Event.End != nil || event.Event.ResponseRecovery != nil || event.Event.Guardrail != nil || event.Event.RepeatDetected != nil {
				t.Logf("actual API terminal event: %s", bounded(assistantChatJSON(event.Event)))
			}
		}
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
	for _, scenario := range []string{"renamed-source", "selected-source"} {
		t.Run(scenario, func(t *testing.T) {
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
			if scenario == "selected-source" {
				changed.Recipe.Qualification = retainedB4Qualification(t)
			}
			current, err := s.preparationRepository().CompareAndSwap(t.Context(), changed, saved.SHA256)
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
			observations := 0
			for _, op := range r.Operations {
				if op.Kind == "runtime" && op.Name == "dialect/check" {
					t.Fatal("completed observation started a fresh Runtime check")
				}
				if op.Kind == "runtime" && op.Name == "dialect-publication/check" {
					observations++
					var receipt assistantDialectPublicationResult
					assistantRunMust(t, readAssistantHostReceipt(op.Result, &receipt))
					if op.State != "completed" || receipt.Status != 200 || receipt.Answer == nil || !receipt.Answer.Saved || receipt.MappingCheckID != "" {
						t.Fatal("completed observation lost its durable receipt")
					}
				}
			}
			if observations != 1 {
				t.Fatal("completed observation was not journaled exactly once", observations)
			}
			after, err := s.preparationRepository().Get(t.Context(), saved.Document.ID)
			assistantRunMust(t, err)
			if after.SHA256 != current.SHA256 {
				t.Fatal("completed observation changed today's source")
			}
			assistantRunMust(t, assistantRecipeImport(t, e, r))
		})
	}
}

func TestAssistantDialectMappingPublicationSelectedFreshRefuses(t *testing.T) {
	for _, mismatch := range []string{"preparation", "revision", "variant", "version"} {
		t.Run(mismatch, func(t *testing.T) {
			s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
			saved := assistantMappingSeed(t, s)
			s.cfg.JpackBin = assistantMappingBinary(t)
			published := checkAndPublishDialect(t, s, saved, "asserted", "1.0.0")
			if mismatch == "preparation" {
				doc := repositoryDocument(2)
				doc.Recipe.Qualification = qualificationFixture()
				var err error
				saved, err = s.preparationRepository().CompareAndSwap(t.Context(), doc, "")
				assistantRunMust(t, err)
			}
			x := assistantMappingOpen(t, s, e)
			a, err := readAssistantDialectArguments("propose_dialect_publication", []byte(assistantMappingArguments(saved, "propose_dialect_publication")))
			assistantRunMust(t, err)
			if mismatch == "variant" {
				a.Variant = "generated"
			}
			if mismatch == "version" {
				a.Version = "1.0.1"
			}
			read := func() {
				t.Helper()
				raw := string(assistantChatJSON(map[string]string{"id": a.ID, "variant": a.Variant}))
				if result := assistantRecipeInvoke(t, e, x, "read", "read_dialect_publication", raw); result.Err() {
					t.Fatal("read refused", result.Content)
				}
			}
			if mismatch != "revision" {
				read()
			}
			selected := saved.Document
			selected.Recipe.Qualification = retainedB4Qualification(t)
			current, err := s.preparationRepository().CompareAndSwap(t.Context(), selected, saved.SHA256)
			assistantRunMust(t, err)
			if mismatch == "revision" {
				a.Revision = current.SHA256
				read()
			}
			s.cfg.JpackBin = filepath.Join(t.TempDir(), "absent-runtime")
			result := assistantRecipeInvoke(t, e, x, "fresh", "propose_dialect_publication", string(assistantChatJSON(a)))
			if !result.Err() {
				t.Fatal("uncompleted selected request observed a different publication")
			}
			r := assistantExecutionRecord(t, e)
			for _, op := range r.Operations {
				if op.Kind == "runtime" && (op.Name == "dialect/check" || op.Name == "dialect-publication/check") {
					t.Fatal("uncompleted selected request journaled an ordinary check")
				}
			}
			if len(r.DomainOutputs) != 0 {
				t.Fatal("uncompleted selected request produced an action or publication observation")
			}
			ledger, err := s.readDialectPublicationLedger()
			assistantRunMust(t, err)
			if len(ledger.Items) != 1 || ledger.Items[0].ID != published.Publication.ID {
				t.Fatal("selected refusal changed immutable publications")
			}
			after, err := s.preparationRepository().Get(t.Context(), saved.Document.ID)
			assistantRunMust(t, err)
			if after.SHA256 != current.SHA256 {
				t.Fatal("selected refusal changed source")
			}
			assistantRunMust(t, assistantRecipeImport(t, e, r))
		})
	}
}

func TestAssistantDialectMappingPublicationCompletedUnauthorized(t *testing.T) {
	s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	saved := assistantMappingSeed(t, s)
	s.cfg.JpackBin = assistantMappingBinary(t)
	checkAndPublishDialect(t, s, saved, "asserted", "1.0.0")
	x := assistantMappingOpen(t, s, e)
	if result := assistantRecipeInvoke(t, e, x, "read", "read_dialect_publication", assistantMappingArguments(saved, "read_dialect_publication")); result.Err() {
		t.Fatal("read refused", result.Content)
	}
	s.cfg.JpackBin = filepath.Join(t.TempDir(), "absent-runtime")
	if !s.sessions.revoke(token) {
		t.Fatal("fixture session was not revoked")
	}
	// Invoke the real host tool directly: the model event helper requires a live
	// writer, while the revoked session can already have stopped that writer.
	ctx := context.WithValue(runcontext.WithCallID(t.Context(), "revoked-completed"), assistantHostRootKey{}, true)
	raw := json.RawMessage(assistantMappingArguments(saved, "propose_dialect_publication"))
	result, err := assistantGraphFind(t, x.Run.HostTools, "propose_dialect_publication").Execute(ctx, raw)
	if err == nil && !result.Err() {
		t.Fatal("completed publication bypassed current session authority")
	}
	r := assistantExecutionRecord(t, e)
	if len(r.DomainOutputs) != 0 {
		t.Fatal("revoked session observed a completed publication")
	}
	for _, op := range r.Operations {
		if op.Kind == "runtime" && (op.Name == "dialect/check" || op.Name == "dialect-publication/check") {
			t.Fatal("revoked session journaled a publication observation or check")
		}
	}
}

// TestAssistantDialectMappingMonitorRegression retains the existing API producer
// and all its successful-loop assertions. Neither case calls a tool directly or
// creates policy events, ownership flags, host intents, slots or receipts.
func TestAssistantDialectMappingMonitorRegression(t *testing.T) {
	t.Run("ownership-change", func(t *testing.T) {
		assistantDialectMappingActualAPIProducer(t, newDialectMonitorRegression(t, true))
	})
	t.Run("benign-contention", func(t *testing.T) {
		assistantDialectMappingActualAPIProducer(t, newDialectMonitorRegression(t, false))
	})
}

func TestAssistantDialectMappingActualAPIProducer(t *testing.T) {
	assistantDialectMappingActualAPIProducer(t, nil)
}

type dialectMonitorRegressionKey struct{}
type dialectMonitorRegressionReturn struct {
	failed, original bool
}
type dialectMonitorRegression struct {
	replace                   bool
	ctx                       context.Context
	cancel                    context.CancelFunc
	phase                     atomic.Uint32
	sourceName                atomic.Pointer[string]
	execution                 atomic.Pointer[assistantRunExecution]
	generation                atomic.Uint64
	rootHasReturned           atomic.Bool
	privateBusy, privateOther atomic.Uint32
	pinRejected               atomic.Uint32
	queued                    atomic.Uint32
	hookExpired               atomic.Bool
	enrolled                  chan struct{}
	monitorHeld               chan struct{}
	readRelease               chan struct{}
	monitorRelease            chan struct{}
	queueEntered              chan struct{}
	returned                  chan dialectMonitorRegressionReturn
	readOnce, monitorOnce     sync.Once
	adapterDone               chan struct{}
	adapterErr                error // read only after adapterDone is closed
	outcome                   dialectMonitorRegressionReturn
	artifactNames             []string
}

func newDialectMonitorRegression(t *testing.T, replace bool) *dialectMonitorRegression {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	return &dialectMonitorRegression{replace: replace, ctx: ctx, cancel: cancel,
		enrolled: make(chan struct{}), monitorHeld: make(chan struct{}),
		readRelease: make(chan struct{}), monitorRelease: make(chan struct{}),
		queueEntered: make(chan struct{}, 1), returned: make(chan dialectMonitorRegressionReturn, 1)}
}

func (m *dialectMonitorRegression) releaseRead() {
	m.readOnce.Do(func() { close(m.readRelease) })
}
func (m *dialectMonitorRegression) releaseMonitor() {
	m.monitorOnce.Do(func() { close(m.monitorRelease) })
}
func (m *dialectMonitorRegression) waitGate(release <-chan struct{}) {
	select {
	case <-release:
	case <-m.ctx.Done():
		m.hookExpired.Store(true)
	}
}

func (m *dialectMonitorRegression) install(t *testing.T) {
	t.Helper()
	if testHookDialectArtifactOwnerStep != nil || testHookPrivateRead != nil ||
		testHookAssistantDialectCompleteReadObserved != nil || testHookPrivateDataTurnQueued != nil {
		t.Fatal("monitor regression requires unused scheduling hooks")
	}
	// Installed once, before the fixture starts its owner/monitor goroutines.
	// Arming and observations are atomic; hook function values never change live.
	testHookDialectArtifactOwnerStep = func(label string) error {
		if label == "factory-after-owner-enrollment" && m.phase.CompareAndSwap(0, 1) {
			close(m.enrolled)
			m.waitGate(m.readRelease)
		}
		return nil
	}
	testHookPrivateRead = func(name string) error {
		selected := m.sourceName.Load()
		if selected != nil && name == *selected && m.phase.CompareAndSwap(1, 2) {
			// The original API tool is parked after enrollment; the fixture's
			// owning Execute is waiting for cancellation. No fixture source read
			// runs here. This next saved-conversation refresh is the real monitor.
			// readAssistantRunSource owns both private turn and EX .data.lock.
			close(m.monitorHeld)
			m.waitGate(m.monitorRelease)
		}
		return nil
	}
	testHookPrivateDataTurnQueued = func(ctx context.Context) {
		if ctx.Value(dialectMonitorRegressionKey{}) != m || m.phase.Load() != 2 || m.rootHasReturned.Load() {
			return
		}
		if m.queued.CompareAndSwap(0, 1) {
			select {
			case m.queueEntered <- struct{}{}:
			default:
			}
		}
	}
	testHookAssistantDialectCompleteReadObserved = func(ctx context.Context, stage assistantDialectCompleteReadObservation, class assistantDialectCompleteReadObservationClass) {
		if ctx.Value(dialectMonitorRegressionKey{}) != m {
			return
		}
		switch stage {
		case assistantDialectCompleteReadObservedPrivate:
			if class == assistantDialectCompleteReadObservedTurnBusy {
				m.privateBusy.Add(1)
			} else {
				m.privateOther.Add(1)
			}
		case assistantDialectCompleteReadObservedPin:
			m.pinRejected.Add(1)
		case assistantDialectCompleteReadObservedReturn:
			m.rootHasReturned.Store(true)
			g, live := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration)
			physical, dispatched := ctx.Value(assistantExternalPhysicalKey{}).(assistantExternalPhysical)
			root, _ := ctx.Value(assistantHostRootKey{}).(bool)
			original := live && g.owner == m.execution.Load() && g.number == m.generation.Load() && !g.finalizer &&
				root && dispatched && physical.Name == "read_dialect" && physical.ID != "" && runcontext.CallIDOf(ctx) == physical.Intent.CallID
			select {
			case m.returned <- dialectMonitorRegressionReturn{failed: class != assistantDialectCompleteReadObservedSuccess, original: original}:
			default:
			}
		}
	}
	t.Cleanup(func() {
		m.releaseRead()
		m.releaseMonitor()
		m.cancel()
		if e := m.execution.Load(); e != nil {
			e.cancel()
			select {
			case <-e.done: // runOwner joins monitorDone before closing done.
			case <-time.After(15 * time.Second):
				t.Error("monitor regression owner did not drain; hooks retained")
				return
			}
		}
		if m.adapterDone != nil {
			select {
			case <-m.adapterDone:
			case <-time.After(15 * time.Second):
				t.Error("monitor regression adapter did not drain; hooks retained")
				return
			}
		}
		// Registered before fixture cleanup, so this runs after its goroutines
		// and runtime have drained. Restore only after the explicit joins above.
		testHookDialectArtifactOwnerStep = nil
		testHookPrivateRead = nil
		testHookAssistantDialectCompleteReadObserved = nil
		testHookPrivateDataTurnQueued = nil
	})
}

func (m *dialectMonitorRegression) bind(t *testing.T, s *Server, e *assistantRunExecution) {
	t.Helper()
	name := s.conversationName()
	m.sourceName.Store(&name)
	m.execution.Store(e)
	e.canonicalMu.Lock()
	m.generation.Store(e.generation)
	e.canonicalMu.Unlock()
	if m.generation.Load() == 0 {
		t.Fatal("actual service invocation has no generation")
	}
	if s.testAssistantRunMonitorChecked != nil {
		t.Fatal("actual API regression must not park the monitor after authorization")
	}
}

func dialectMonitorArtifactNames(t *testing.T, s *Server) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(s.cfg.ProjectDir, retainedArtifactDirectory))
	if os.IsNotExist(err) {
		return nil
	}
	assistantRunMust(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func (m *dialectMonitorRegression) await(t *testing.T, ch <-chan struct{}, stage string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(15 * time.Second):
		t.Fatal("monitor regression schedule did not reach " + stage)
	}
}

func (m *dialectMonitorRegression) replaceWorkEntry(t *testing.T, s *Server, e *assistantRunExecution) {
	t.Helper()
	// Change the actual scratch directory entry, retaining the original held
	// descriptor and enrolled identities. Never reopen/relock/adopt replacement.
	e.writer.lease.mu.Lock()
	defer e.writer.lease.mu.Unlock()
	held, err := e.writer.lease.work.Stat()
	assistantRunMust(t, err)
	path := filepath.Join(s.configDir, ".data-work.lock")
	entry, err := os.Lstat(path)
	assistantRunMust(t, err)
	if !os.SameFile(held, entry) {
		t.Fatal("original work entry was already changed")
	}
	replacement, err := os.CreateTemp(s.configDir, "monitor-work-replacement-")
	assistantRunMust(t, err)
	assistantRunMust(t, replacement.Close())
	assistantRunMust(t, os.Rename(replacement.Name(), path))
	changed, err := os.Lstat(path)
	assistantRunMust(t, err)
	again, err := e.writer.lease.work.Stat()
	assistantRunMust(t, err)
	if os.SameFile(held, changed) || !os.SameFile(held, again) {
		t.Fatal("replacement did not preserve only the original held FD")
	}
}

func (m *dialectMonitorRegression) run(t *testing.T, s *Server, e *assistantRunExecution, x *assistantExecution, ctx context.Context) {
	t.Helper()
	m.artifactNames = dialectMonitorArtifactNames(t, s)
	m.adapterDone = make(chan struct{})
	ctx = context.WithValue(ctx, dialectMonitorRegressionKey{}, m)
	adapterCtx, cancel := context.WithCancel(ctx)
	// This cleanup precedes runtime Close and fixture owner drain. It always
	// opens both test barriers before canceling and joining the API adapter.
	t.Cleanup(func() {
		m.releaseRead()
		m.releaseMonitor()
		cancel()
		select {
		case <-m.adapterDone:
		case <-time.After(15 * time.Second):
			t.Error("monitor regression API adapter cleanup did not drain")
		}
	})
	go func() {
		defer close(m.adapterDone)
		m.adapterErr = x.Adapter.Run(e.generationContext(adapterCtx, m.generation.Load()), x.Run)
	}()
	m.await(t, m.enrolled, "original enrollment release")
	m.await(t, m.monitorHeld, "real monitor private source read")
	if m.replace {
		m.replaceWorkEntry(t, s, e)
	}
	m.releaseRead()
	select {
	case m.outcome = <-m.returned:
		// Unchanged C reaches this refusal before it can queue. Open the real
		// monitor immediately; this is an assertion failure, not a retry.
		m.releaseMonitor()
	case <-m.queueEntered:
		// A staged repair must actually queue the original read's short
		// transaction. Merely sampling a free lock cannot trigger this branch.
		m.releaseMonitor()
		select {
		case m.outcome = <-m.returned:
		case <-time.After(15 * time.Second):
			t.Fatal("queued original API read did not finish after monitor release")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("original API read neither queued nor returned")
	}
	m.phase.Store(3)
	select {
	case <-m.adapterDone:
	case <-adapterCtx.Done():
		t.Fatal("original API adapter exceeded its existing request deadline")
	}
	if m.hookExpired.Load() || !m.outcome.original {
		t.Fatal("monitor schedule expired or original API root/generation was lost")
	}
	assistantRunMust(t, m.adapterErr)
}

func (m *dialectMonitorRegression) requireBenign(t *testing.T) {
	t.Helper()
	if m.outcome.failed {
		if m.privateBusy.Load() == 1 && m.privateOther.Load() == 0 && m.pinRejected.Load() == 0 && m.queued.Load() == 0 {
			t.Log("monitor regression: case=benign-contention schedule=monitor-private-held admission=private-turn-busy root=refused")
		}
		t.Fatal("benign monitor contention refused the original API read")
	}
	if m.queued.Load() != 1 || m.privateBusy.Load() != 0 || m.privateOther.Load() != 0 || m.pinRejected.Load() != 0 {
		t.Fatal("successful original read did not use the staged private admission schedule")
	}
	t.Log("monitor regression: case=benign-contention schedule=monitor-private-held admission=queued root=completed")
}

func (m *dialectMonitorRegression) requireRefused(t *testing.T, s *Server, e *assistantRunExecution, steps int32) {
	t.Helper()
	if !m.outcome.failed || m.pinRejected.Load() != 1 || m.privateBusy.Load() != 0 || m.privateOther.Load() != 0 {
		t.Fatal("changed original work inode was not refused by its original pin guard")
	}
	run := assistantExecutionRecord(t, e)
	if len(run.DialectReads) != 0 || len(run.DomainOutputs) != 0 {
		t.Fatal("changed ownership created a read slot, receipt or proposal")
	}
	reads := 0
	for _, op := range run.Operations {
		if op.Kind == "host" && op.Name == "read_dialect" {
			reads++
			if op.State != "refused" {
				t.Fatal("changed ownership completed its original API read")
			}
		}
	}
	if reads != 1 || steps != 2 {
		t.Fatal("ownership control did not execute exactly one API read and one finish")
	}
	if strings.Join(m.artifactNames, "\x00") != strings.Join(dialectMonitorArtifactNames(t, s), "\x00") {
		t.Fatal("changed ownership published retained raw bytes")
	}
	t.Log("monitor regression: case=ownership-change pin=original-entry-changed root=refused no-publication=true")
}
