package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func assistantResearchHTTP(t *testing.T, s *Server, token, path, revision, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("If-Match", revision)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func assistantResearchSeed(t *testing.T, s *Server, token string, e *assistantRunExecution, grant bool) (inputPreparationReply, *os.Root, inputResearchRun) {
	t.Helper()
	writeDeskConfig(t, s, researchDeskFile("http://127.0.0.1:1"))
	d := repositoryDocument(1)
	recipe := researchRecipeFixture()
	recipe.Target = "records"
	recipe.AI = inputResearchAI{Connection: e.source.Binding.ConnectionID, Revision: e.source.Binding.ConnectionRevision, Model: e.source.Binding.Model}
	recipe.Limits.Records = 3
	recipe.Limits.Pages = 3
	recipe.Limits.ModelCalls = 6
	d.Recipe.Research = &recipe
	saved, err := s.preparationRepository().CompareAndSwap(t.Context(), d, "")
	assistantRunMust(t, err)
	root, err := s.inputResearchRoot(true)
	assistantRunMust(t, err)
	defer root.Close()
	rr, err := openInputResearch(root, assistantRecordRun, true)
	assistantRunMust(t, err)
	t.Cleanup(func() { rr.Close() })
	run := inputResearchRun{Version: 1, ID: assistantRecordRun, PreparationID: d.ID, Revision: saved.SHA256, RecipeVersion: d.RecipeVersion, Recipe: recipe, Gateway: inputResearchPin{Authority: "gateway:test", Public: testSignerPublic}, State: "review", CreatedAt: "2026-10-20T00:00:00Z", UpdatedAt: "2026-10-20T00:00:00Z", Records: []inputResearchEntity{}, Sources: []inputResearchSource{}, Attempts: []inputResearchAttempt{}, Seeds: []string{}, Companies: []inputResearchEntity{}}
	assistantRunMust(t, s.researchJournal(rr, run.ID).Create(t.Context(), run))
	// Keep the independent worker's execution lease; this fixture admits real
	// queued research without ever allowing a provider/background model dispatch.
	worker, err := rr.OpenFile("worker.lock", os.O_CREATE|os.O_RDWR, 0600)
	assistantRunMust(t, err)
	assistantRunMust(t, waitPrivateDataLock(t.Context(), worker, true))
	t.Cleanup(func() { worker.Close() })
	if grant {
		w := assistantResearchHTTP(t, s, token, "/api/input-preparations/"+run.PreparationID+"/research/"+run.ID+"/authorize", saved.SHA256, fmt.Sprintf(`{"chatId":%q,"minutes":60}`, e.record.Binding.Owner.ID))
		if w.Code != http.StatusOK {
			t.Fatal("real grant refused", w.Code, w.Body.String())
		}
	}
	// A run offers request_dialect_research only with a current approval of
	// Desk's research workflow, made after the Desk file it reviews is written.
	assistantSourceTestApproveFor(t, s, token, e.record.Binding.Owner.ID, "desk-research-workflow")
	return saved, rr, run
}

// assistantCanonicalHostCall runs one root host call of a canonical run the
// way Agent v0.13.2 runs it (context/session.go ExecuteDurableHost): its
// tool_call event is persisted, a sourced tool's call passes the source gate,
// which puts an act call to the tool's approval policy, and the call runs
// through the run's durable writer, which claims its dispatch before the host
// handler runs and records its outcome. A repeated call meets that record.
// Like the executor's own invocation, it carries the execution's current
// generation, which the canonical store requires of every write. It uses no
// *testing.T, so a caller may run it on another goroutine.
func assistantCanonicalHostCall(e *assistantRunExecution, x *assistantExecution, ctx context.Context, call, name, raw string, events bool) (runcontext.ToolResult, error) {
	var tool *runcontext.HostTool
	for i := range x.Run.HostTools {
		if x.Run.HostTools[i].Name == name {
			tool = &x.Run.HostTools[i]
		}
	}
	input := x.Run.Context
	if tool == nil || input == nil || input.Persistence == nil {
		return runcontext.ToolResult{}, fmt.Errorf("%s is not a canonical root host tool of this run", name)
	}
	binding := input.Persistence.WithRole("author")
	e.canonicalMu.Lock()
	generation := e.generation
	e.canonicalMu.Unlock()
	ctx = context.WithValue(runcontext.WithCallID(e.generationContext(ctx, generation), call), assistantHostRootKey{}, true)
	args := json.RawMessage(raw)
	wire, source := name, "host"
	gate := (*runcontext.SourceGate)(nil)
	if tool.Source != nil {
		declared, err := runcontext.DeclareSources([]runcontext.HostTool{*tool}, nil)
		if err != nil {
			return runcontext.ToolResult{}, err
		}
		policy := runcontext.Defaults
		rows := []contract.ProvenanceSourceFigures{}
		gate = runcontext.NewSourceGate(declared, &policy, "[retrieved]", "json-schema", func() *[]contract.ProvenanceSourceFigures { return &rows }, nil)
		record, _ := json.Marshal(tool.Source.Record(name))
		wire, source = runcontext.WireName(tool.Source.ID, name), runcontext.RecordDigest(runcontext.MustParseJSON(string(record)))
	}
	if events {
		event := contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: wire, Args: args}}
		if gate != nil {
			event = gate.Decorate(event)
		}
		if _, err := binding.Writer.Event(ctx, *binding, event); err != nil {
			return runcontext.ToolResult{}, err
		}
	}
	run := func(ctx context.Context, args json.RawMessage) (runcontext.ToolResult, error) {
		return runcontext.InvokeDurable(ctx, binding, "host", source, wire, args, runcontext.DurableReadingTool(tool), func(original []byte) (runcontext.ToolResult, error) {
			return tool.Execute(ctx, original)
		}, func(r runcontext.ToolResult) bool { return !r.Err() }, nil)
	}
	var result runcontext.ToolResult
	var err error
	if gate != nil {
		result, err = gate.Call(ctx, wire, args, run)
	} else {
		result, err = run(ctx, args)
	}
	if err != nil || !events {
		return result, err
	}
	// The result event as the Agent writes it: its text parts joined, its
	// structured content, and a sourced call's source record.
	parts := []string{}
	for _, part := range result.Content {
		parts = append(parts, part.Text())
	}
	event := contract.RunEvent{ToolResult: &contract.ToolResult{Type: "tool_result", CallID: &call, Name: wire, Text: strings.Join(parts, "\n"), IsError: result.Err()}}
	if result.StructuredContent != nil {
		event.ToolResult.Structured = result.StructuredContent.JSON()
	}
	if gate != nil {
		event = gate.Decorate(event)
	}
	_, err = binding.Writer.Event(ctx, *binding, event)
	return result, err
}

// assistantResearchInvoke is one research call: through the canonical claim
// path above on a canonical run, otherwise directly as before.
func assistantResearchInvoke(t *testing.T, e *assistantRunExecution, x *assistantExecution, ctx context.Context, call, name, raw string) runcontext.ToolResult {
	t.Helper()
	if !assistantCanonicalActive(e.record) {
		assistantPolicyEvent(t, e, call, name, []byte(raw))
		result, err := assistantGraphFind(t, x.Run.HostTools, name).Execute(context.WithValue(runcontext.WithCallID(ctx, call), assistantHostRootKey{}, true), []byte(raw))
		assistantRunMust(t, err)
		assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolResult: &contract.ToolResult{Type: "tool_result", CallID: &call, Name: name, Text: result.Content[0].Text(), IsError: result.Err()}}))
		return result
	}
	result, err := assistantCanonicalHostCall(e, x, ctx, call, name, raw, true)
	assistantRunMust(t, err)
	return result
}
func assistantResearchArgs(saved inputPreparationReply) string {
	return string(assistantChatJSON(map[string]string{"id": saved.Document.ID, "revision": saved.SHA256}))
}
func assistantResearchUsed(t *testing.T, s *Server, rr *os.Root, run inputResearchRun) inputResearchRun {
	t.Helper()
	got, err := s.researchJournal(rr, run.ID).Read(t.Context())
	assistantRunMust(t, err)
	if got.State != "queued" || got.Approval == nil || got.Approval.UsedAt == "" || len(got.Attempts) != 0 || got.StopRequested {
		t.Fatalf("real allowance was not queued once without provider work: %+v", got)
	}
	return got
}
func TestAssistantDialectResearchRequestAndReplay(t *testing.T) {
	// manual-first and other-host-first change the recipe after the earlier
	// same-turn admission; the -current modes leave it as it was read.
	for _, mode := range []string{"review", "grant", "manual-first", "other-host-first", "manual-first-current", "other-host-first-current"} {
		t.Run(mode, func(t *testing.T) {
			current := strings.HasSuffix(mode, "-current")
			mode := strings.TrimSuffix(mode, "-current")
			s, token, e := assistantResearchSourceBrowserFixture(t, "http://127.0.0.1:1")
			saved, rr, run := assistantResearchSeed(t, s, token, e, mode != "review")
			x := assistantExecutionOpen(t, s, e)
			assistantRecordRead(t, e, x, saved)
			if mode == "manual-first" || mode == "other-host-first" {
				q := inputResearchRequest{ChatID: e.record.Binding.Owner.ID, PreparationID: run.PreparationID, Revision: run.Revision, TurnID: e.record.Binding.RequestID}
				if mode == "other-host-first" {
					q.RequestKey = strings.Repeat("b", 64)
				}
				receipt, err := s.requestResearch(e.request.WithContext(t.Context()), q, nil)
				assistantRunMust(t, err)
				if receipt.Status != "started" {
					t.Fatal("earlier same-turn request not admitted")
				}
				if !current {
					// The research workflow's approval policy refuses a call whose
					// recipe read is no longer current, before it consults the
					// same-turn historical receipt: nothing is admitted again and
					// the earlier admission keeps its own provenance.
					before, err := s.researchJournal(rr, run.ID).Read(t.Context())
					assistantRunMust(t, err)
					doc := saved.Document
					doc.Name = "Changed after original admission"
					_, err = s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
					assistantRunMust(t, err)
					_, err = assistantCanonicalHostCall(e, x, t.Context(), "request", assistantResearchTool, assistantResearchArgs(saved), true)
					if err == nil || !strings.Contains(err.Error(), "approval policy refused") {
						t.Fatal("a stale recipe read was not refused by the workflow's policy", err)
					}
					after, err := s.researchJournal(rr, run.ID).Read(t.Context())
					assistantRunMust(t, err)
					if !bytes.Equal(assistantChatJSON(before), assistantChatJSON(after)) || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
						t.Fatal("refused stale call changed the owner or retained an output")
					}
					return
				}
				// With the recipe as it was read, the same-turn historical receipt
				// is returned without another admission.
			}
			r := assistantResearchInvoke(t, e, x, t.Context(), "request", assistantResearchTool, assistantResearchArgs(saved))
			if r.Err() {
				t.Fatal("research request refused", r.Value().Text())
			}
			record := assistantExecutionRecord(t, e)
			assistantRunMust(t, assistantRecipeImport(t, e, record))
			if len(record.DomainOutputs) != 1 || record.DomainOutputs[0].DialectAction.Research == nil {
				t.Fatal("finite research output missing")
			}
			p := record.DomainOutputs[0].DialectAction
			if mode == "review" {
				if p.Action.Applied != nil || !strings.Contains(r.Value().Text(), "review-required") {
					t.Fatal("no grant manufactured admission")
				}
			} else {
				used := assistantResearchUsed(t, s, rr, run)
				if p.Action.Applied == nil || p.Action.Applied.RunID != used.ID || p.Action.Applied.At != used.Approval.UsedAt {
					t.Fatal("applied does not derive from actual used receipt")
				}
				if mode == "manual-first" && used.Approval.RequestKey != "" || mode == "other-host-first" && used.Approval.RequestKey != strings.Repeat("b", 64) {
					t.Fatal("existing same-turn provenance rewritten")
				}
			}
			// The same call again returns the immutable host receipt, never the
			// owner again: the run's durable writer answers it from its record.
			before, err := s.researchJournal(rr, run.ID).Read(t.Context())
			assistantRunMust(t, err)
			r2 := assistantResearchInvoke(t, e, x, t.Context(), "request-again", assistantResearchTool, assistantResearchArgs(saved))
			if r2.Err() || !bytes.Equal(r.Value().JSON(), r2.Value().JSON()) {
				t.Fatal("delivery changed retained result")
			}
			after, err := s.researchJournal(rr, run.ID).Read(t.Context())
			assistantRunMust(t, err)
			if !bytes.Equal(assistantChatJSON(before), assistantChatJSON(after)) {
				t.Fatal("redelivery mutated owner")
			}
		})
	}
}
func TestAssistantDialectResearchFiniteArguments(t *testing.T) {
	good := `{"id":"prep_00000000000000000000000000000001","revision":"` + strings.Repeat("a", 64) + `"}`
	if _, err := readAssistantDialectResearchArguments([]byte(good)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(good, `"id":`, `"ID":`, 1), strings.Replace(good, `"id":`, `"id":null,"id":`, 1), strings.Replace(good, `"revision":"`+strings.Repeat("a", 64)+`"`, `"revision":null`, 1), strings.TrimSuffix(good, "}") + `,"turnId":"injected"}`, strings.TrimSuffix(good, "}") + `,"approval":true}`, `null`, `[]`} {
		if _, err := readAssistantDialectResearchArguments([]byte(bad)); err == nil {
			t.Fatal("foreign request authority accepted", bad)
		}
	}
}
