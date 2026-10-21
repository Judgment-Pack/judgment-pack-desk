package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func assistantRecipeCall(t *testing.T, e *assistantRunExecution, x *assistantExecution, ctx context.Context, call string) runcontext.ToolResult {
	t.Helper()
	raw := json.RawMessage(assistantRecipeCandidate)
	assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: "propose_dialect_recipe", Args: raw}}))
	ctx = context.WithValue(runcontext.WithCallID(ctx, call), assistantHostRootKey{}, true)
	result, err := assistantGraphFind(t, x.Run.HostTools, "propose_dialect_recipe").Execute(ctx, raw)
	assistantRunMust(t, err)
	return result
}
func TestAssistantDialectRecipeFiniteArguments(t *testing.T) {
	if _, err := readAssistantDialectRecipeArguments("propose_dialect_recipe", []byte(assistantRecipeCandidate)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"\ufeffvalue\ufeff", "\u0085"} {
		encoded, _ := json.Marshal(name)
		valid := strings.Replace(assistantRecipeCandidate, `"Portable recipe"`, string(encoded), 1)
		if _, err := readAssistantDialectRecipeArguments("propose_dialect_recipe", []byte(valid)); err != nil {
			t.Fatal("browser-valid original text refused", name, err)
		}
	}
	for _, replace := range [][2]string{
		{`"name":"Portable recipe"`, `"name":"one","Name":"two"`},
		{`"name":"Portable recipe"`, `"name":"one","na\u006de":"two"`},
		{`"name":"Portable recipe"`, `"name":null`},
		{`"name":"Portable recipe"`, `"name":"\ufeff"`},
		{`"description":"Original title"`, `"description":"\ufeff"`},
		{`"qualificationTarget":"Review separately"`, `"qualificationTarget":"\ufeff"`},
		{`"name":"Portable recipe"`, `"name":"\ud800"`},
		{`"fields":[`, `"target":"companies","fields":[`},
		{`"type":"text"`, `"type":"text","Type":"integer"`},
		{`"type":"text"`, `"type":"text","value":false`},
		{`"criteria":[`, `"draftId":"caller-id","criteria":[`},
		{`"qualificationTarget":"Review separately"`, `"qualificationTarget":null`},
		{`"qualificationTarget":"Review separately"`, `"qualificationTarget":""`},
		{`"records":5`, `"records":5,"Records":4`},
		{`"records":5`, `"records":0`},
		{`"searches":3`, `"searches":3.0`},
		{`"approach":"delegated"`, `"approach":"delegated","authority":true`},
		{`"approach":"delegated"`, `"Approach":"delegated"`},
		{`"concurrency":2`, `"concurrency":3`},
		{`"limits":`, `"ai":{"connection":"caller"},"limits":`},
	} {
		bad := strings.Replace(assistantRecipeCandidate, replace[0], replace[1], 1)
		if bad == assistantRecipeCandidate {
			t.Fatal("fixture not applied")
		}
		if _, err := readAssistantDialectRecipeArguments("propose_dialect_recipe", []byte(bad)); err == nil {
			t.Fatal("unsupported recipe accepted", bad)
		}
	}
	for _, raw := range []string{`[]`, `null`, `{"id":"../outside"}`, `{"ID":"prep_00000000000000000000000000000001"}`, `{"id":"prep_00000000000000000000000000000001","url":"outside"}`} {
		if _, err := readAssistantDialectRecipeArguments("read_dialect_recipe", []byte(raw)); err == nil {
			t.Fatal("foreign read accepted", raw)
		}
	}
	for _, name := range []string{"list_dialects", "get_dialect_recipe_instructions"} {
		if _, err := readAssistantDialectRecipeArguments(name, []byte(`{"path":"outside"}`)); err == nil {
			t.Fatal("extra read fields accepted")
		}
	}
}
func TestAssistantDialectRecipePlanChildFreshAuthority(t *testing.T) {
	for _, phase := range []string{"plan", "work"} {
		t.Run(phase, func(t *testing.T) {
			s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", phase)
			saved := assistantRecipeSeed(t, s)
			x := assistantExecutionOpen(t, s, e)
			registry, err := e.assistantDialectRecipeTools()
			assistantRunMust(t, err)
			for _, tool := range registry.tools {
				if tool.Name != "get_dialect_recipe_instructions" && tool.ContextPolicy != nil {
					t.Fatal("mutable read marked immutable")
				}
			}
			read := assistantRecipeInvoke(t, e, x, "first-read", "read_dialect_recipe", `{"id":"`+saved.Document.ID+`"}`)
			if read.Err() || strings.Contains(read.Value().Text(), "ROW-SECRET") || strings.Contains(read.Value().Text(), "HIDDEN") || !strings.Contains(read.Value().Text(), "portable-template") {
				t.Fatal("unsafe recipe read", read.Value().Text())
			}
			d := saved.Document
			d.Name = "Fresh recipe"
			_, err = s.preparationRepository().CompareAndSwap(t.Context(), d, saved.SHA256)
			assistantRunMust(t, err)
			fresh, err := assistantGraphFind(t, x.Run.HostTools, "read_dialect_recipe").Execute(runcontext.WithCallID(t.Context(), "child-read"), json.RawMessage(`{"id":"`+d.ID+`"}`))
			assistantRunMust(t, err)
			if fresh.Err() || !strings.Contains(fresh.Value().Text(), "Fresh recipe") {
				t.Fatal("child read reused stale snapshot")
			}
			if phase == "plan" {
				for _, tool := range x.Run.HostTools {
					if tool.Name == "propose_dialect_recipe" {
						t.Fatal("Plan offered a recipe proposal")
					}
				}
				// Probe the actual dispatcher behind the filtered offer too.
				x.Run.HostTools = registry.tools
			}
			no, err := assistantGraphFind(t, x.Run.HostTools, "propose_dialect_recipe").Execute(runcontext.WithCallID(t.Context(), "child-proposal"), json.RawMessage(assistantRecipeCandidate))
			assistantRunMust(t, err)
			if !no.Err() {
				t.Fatal("child proposal ran")
			}
			if phase == "plan" {
				if !assistantRecipeCall(t, e, x, t.Context(), "plan-proposal").Err() {
					t.Fatal("Plan producer ran")
				}
			}
			s.sessions.revoke(token)
			no, err = assistantGraphFind(t, x.Run.HostTools, "list_dialects").Execute(runcontext.WithCallID(t.Context(), "revoked"), json.RawMessage(`{}`))
			assistantRunMust(t, err)
			if !no.Err() || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
				t.Fatal("revoked identity read or created")
			}
		})
	}
}
func TestAssistantDialectRecipeImportAndReplay(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	x := assistantExecutionOpen(t, s, e)
	result := assistantRecipeCall(t, e, x, t.Context(), "candidate")
	if result.Err() {
		t.Fatal(result.Value().Text())
	}
	r := assistantRecipeObserve(t, s, e)
	registry, err := e.assistantDialectRecipeTools()
	assistantRunMust(t, err)
	replayCtx := context.WithValue(runcontext.WithCallID(t.Context(), "candidate"), assistantHostRootKey{}, true)
	replay, err := assistantGraphFind(t, registry.tools, "propose_dialect_recipe").Execute(replayCtx, json.RawMessage(assistantRecipeCandidate))
	assistantRunMust(t, err)
	if replay.Err() || !bytes.Equal(replay.Value().JSON(), result.Value().JSON()) || len(assistantExecutionRecord(t, e).DomainOutputs) != 1 {
		t.Fatal("completed redelivery changed result")
	}
	original, _ := json.Marshal(r)
	for _, mode := range []string{"output-removed", "duplicate", "id", "time", "turn", "attribution", "content", "digest", "mixed-output", "foreign-meta", "missing-meta", "intent-content", "intent-origin", "intent-mixed", "intent-extra"} {
		t.Run(mode, func(t *testing.T) {
			var changed assistantRunRecord
			assistantRunMust(t, json.Unmarshal(original, &changed))
			out := changed.DomainOutputs[0].Recipe
			switch mode {
			case "output-removed":
				changed.DomainOutputs = nil
			case "duplicate":
				changed.DomainOutputs = append(changed.DomainOutputs, changed.DomainOutputs[0])
			case "id":
				out.Proposal.DraftID = "recipe-foreign"
			case "time":
				out.Proposal.CreatedAt = "2026-01-01T00:00:00Z"
			case "turn":
				out.Proposal.AfterTurnID = "other-turn"
			case "attribution":
				out.Proposal.Attribution.Model = "foreign-model"
			case "content":
				out.Proposal.Fields[0].Type = "number"
			case "digest":
				out.Retained.SHA256 = strings.Repeat("a", 64)
			case "mixed-output":
				changed.DomainOutputs[0].Draft.Content = "{}"
			default:
				for i := range changed.Operations {
					op := &changed.Operations[i]
					if op.Kind != "host" {
						continue
					}
					var in assistantHostIntent
					assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
					switch mode {
					case "foreign-meta":
						op.Name = "list_dialects"
						in.Arguments = []byte(`{}`)
					case "missing-meta":
						in.RecipeProposal = nil
					case "intent-content":
						in.RecipeProposal.Fields[0].Type = "number"
					case "intent-origin":
						in.RecipeProposal.DraftID = "recipe-foreign"
					case "intent-mixed":
						in.Policy = &assistantPolicyHostBinding{}
					}
					op.Intent, _ = json.Marshal(in)
					if mode == "intent-extra" {
						op.Intent = bytes.Replace(op.Intent, []byte(`"type":"text"`), []byte(`"type":"text","foreign":true`), 1)
					}
				}
			}
			if assistantRecipeImport(t, e, changed) == nil {
				t.Fatal("fabricated recipe imported", mode)
			}
		})
	}
}

type assistantRecipeDraftFault struct {
	dialectDraftRepository
	writes atomic.Int32
	fault  error
	before func(dialectDraftDocument)
}

func (f *assistantRecipeDraftFault) CompareAndSwap(ctx context.Context, d dialectDraftDocument, rev string) (dialectDraftReply, error) {
	f.writes.Add(1)
	if f.before != nil {
		f.before(d)
	}
	saved, err := f.dialectDraftRepository.CompareAndSwap(ctx, d, rev)
	if err != nil {
		return saved, err
	}
	return dialectDraftReply{}, f.fault
}
func TestAssistantDialectRecipeUncertainWrite(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	x := assistantExecutionOpen(t, s, e)
	actual := s.draftRepository()
	var created dialectDraftDocument
	wrapper := &assistantRecipeDraftFault{dialectDraftRepository: actual, fault: errors.New("fixture write acknowledgement unavailable")}
	wrapper.before = func(d dialectDraftDocument) {
		created = d
		r := assistantExecutionRecord(t, e)
		found := false
		for _, op := range r.Operations {
			if op.Kind == "host" && op.Name == "propose_dialect_recipe" {
				var in assistantHostIntent
				assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
				if op.State != "dispatched" || in.RecipeProposal == nil || d.CreatedAt != in.RecipeProposal.CreatedAt || d.ID != dialectDraftIdentity("", &dialectDraftOrigin{r.Binding.Owner.ID, in.RecipeProposal.DraftID}) {
					t.Fatal("create preceded durable exact metadata")
				}
				found = true
			}
		}
		if !found {
			t.Fatal("create without intent")
		}
	}
	s.dialectDrafts = wrapper
	result := assistantRecipeCall(t, e, x, t.Context(), "uncertain")
	if !result.Err() || wrapper.writes.Load() != 1 || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
		t.Fatal("uncertain write exposed success")
	}
	if _, err := actual.Get(t.Context(), created.ID); err != nil {
		t.Fatal("fixture did not physically write", err)
	}
	ctx := context.WithValue(runcontext.WithCallID(t.Context(), "uncertain"), assistantHostRootKey{}, true)
	again, err := assistantGraphFind(t, x.Run.HostTools, "propose_dialect_recipe").Execute(ctx, json.RawMessage(assistantRecipeCandidate))
	assistantRunMust(t, err)
	if !again.Err() || wrapper.writes.Load() != 1 {
		t.Fatal("redelivery repeated uncertain create")
	}
	// The acknowledged refusal/uncertain receipt remains valid import data.
	assistantRunMust(t, assistantRecipeImport(t, e, assistantExecutionRecord(t, e)))
}
func TestAssistantDialectRecipeQueuedAuthority(t *testing.T) {
	for _, mode := range []string{"revoked", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
			x := assistantExecutionOpen(t, s, e)
			root, err := s.openDialectDrafts(true)
			assistantRunMust(t, err)
			defer root.Close()
			lock, err := root.Open(".")
			assistantRunMust(t, err)
			assistantRunMust(t, waitPrivateDataLock(t.Context(), lock, true))
			defer lock.Close()
			reached := make(chan struct{}, 1)
			// Preserve the production file adapter's queue and current-authority callback.
			s.dialectDrafts = fileDialectDraftRepository{open: func(create bool) (*os.Root, error) {
				select {
				case reached <- struct{}{}:
				default:
				}
				return s.openDialectDrafts(create)
			}, authorize: e.authorizeAssistantDialectRecipe}
			call := "queued"
			raw := json.RawMessage(assistantRecipeCandidate)
			assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: "propose_dialect_recipe", Args: raw}}))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx = context.WithValue(runcontext.WithCallID(ctx, call), assistantHostRootKey{}, true)
			done := make(chan runcontext.ToolResult, 1)
			tool := assistantGraphFind(t, x.Run.HostTools, "propose_dialect_recipe")
			go func() { result, _ := tool.Execute(ctx, raw); done <- result }()
			select {
			case <-reached:
			case <-time.After(3 * time.Second):
				t.Fatal("producer did not reach actual draft queue")
			}
			select {
			case <-done:
				t.Fatal("producer bypassed held directory lock")
			default:
			}
			if mode == "revoked" {
				s.sessions.revoke(token)
			} else {
				cancel()
			}
			assistantRunMust(t, lock.Close())
			select {
			case result := <-done:
				if !result.Err() {
					t.Fatal("withdrawn queued producer created draft")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("queued producer did not drain (lock reentry)")
			}
			directory, err := root.Open(".")
			assistantRunMust(t, err)
			defer directory.Close()
			entries, err := directory.ReadDir(-1)
			assistantRunMust(t, err)
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".json") {
					t.Fatal("withdrawn producer physically wrote")
				}
			}
			if len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
				t.Fatal("withdrawn producer output retained")
			}
		})
	}
}
func TestAssistantDialectRecipeObservedCancellationAndStaleProjection(t *testing.T) {
	for _, mode := range []string{"cancel", "edit", "discard", "save", "delete"} {
		t.Run(mode, func(t *testing.T) {
			s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
			x := assistantExecutionOpen(t, s, e)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			observed := false
			testHookAssistantDialectRecipeRetained = func() {
				observed = true
				if mode == "cancel" {
					cancel()
				}
			}
			t.Cleanup(func() { e.cancel(); <-e.done; testHookAssistantDialectRecipeRetained = nil })
			result := assistantRecipeCall(t, e, x, ctx, "retained")
			if result.Err() || !observed {
				t.Fatal("actual retained output lost", result.Value().Text())
			}
			r := assistantRecipeObserve(t, s, e)
			out := r.DomainOutputs[0].Recipe
			if mode == "cancel" {
				if e.Dispatch(t.Context(), r.DomainOutputs[0].OperationID) == nil {
					t.Fatal("completed output dispatched")
				}
				return
			}
			saved, err := s.draftRepository().Get(t.Context(), out.Retained.ID)
			assistantRunMust(t, err)
			if mode == "delete" {
				root, err := s.openDialectDrafts(false)
				assistantRunMust(t, err)
				assistantRunMust(t, root.Remove(out.Retained.ID+".json"))
				assistantRunMust(t, root.Close())
			} else {
				d := saved.Document
				switch mode {
				case "edit":
					d.Name = "Human-edited recipe"
				case "discard":
					d.Discarded = true
				case "save":
					d.SavedInputID = dialectDraftPreparationID(d)
				}
				d.UpdatedAt = "2026-10-20T01:00:00Z"
				_, err = s.draftRepository().CompareAndSwap(t.Context(), d, saved.SHA256)
				assistantRunMust(t, err)
			}
			cp := *r.Checkpoint
			cp.Status = "complete"
			assistantRunMust(t, e.Checkpoint(t.Context(), cp))
			assistantRunMust(t, e.writer.State(t.Context(), "complete"))
			if e.ProjectChat(t.Context()) == nil {
				t.Fatal("stale independent draft projected")
			}
			after := assistantExecutionRecord(t, e)
			if len(after.DomainOutputs) != 1 || !sameDraftValue(after.DomainOutputs, r.DomainOutputs) {
				t.Fatal("stale draft reclassified completed producer")
			}
			current, err := s.draftRepository().Get(t.Context(), out.Retained.ID)
			if mode == "delete" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatal("deleted draft resurrected")
				}
			} else {
				assistantRunMust(t, err)
				if current.SHA256 == saved.SHA256 {
					t.Fatal("manual edit overwritten")
				}
			}
		})
	}
}

type assistantRecipeReadbackFault struct {
	dialectDraftRepository
	reads int
}

func (f *assistantRecipeReadbackFault) Get(ctx context.Context, id string) (dialectDraftReply, error) {
	f.reads++
	out, err := f.dialectDraftRepository.Get(ctx, id)
	if err == nil {
		out.Document.Name = "Foreign readback"
	}
	return out, err
}
func TestAssistantDialectRecipeReadbackBeforeSuccess(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	x := assistantExecutionOpen(t, s, e)
	actual := s.draftRepository()
	fault := &assistantRecipeReadbackFault{dialectDraftRepository: actual}
	s.dialectDrafts = fault
	result := assistantRecipeCall(t, e, x, t.Context(), "readback")
	if !result.Err() || fault.reads != 2 || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
		t.Fatal("foreign independent readback exposed success")
	}
	list, err := actual.List(t.Context())
	assistantRunMust(t, err)
	if len(list) != 1 {
		t.Fatal("fixture missed actual independent write")
	}
}
func TestAssistantDialectRecipeMetadataAndCapacity(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	x := assistantExecutionOpen(t, s, e)
	if assistantRecipeCall(t, e, x, t.Context(), "metadata").Err() {
		t.Fatal("producer failed")
	}
	r := assistantRecipeObserve(t, s, e)
	for _, op := range r.Operations {
		if op.Kind != "host" {
			continue
		}
		var in assistantHostIntent
		assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
		if op.Name != "propose_dialect_recipe" {
			continue
		}
		for _, mode := range []string{"id", "time", "turn", "model", "candidate", "phase", "child", "mixed", "operation", "blank-turn", "blank-model"} {
			raw, _ := json.Marshal(in)
			var changed assistantHostIntent
			assistantRunMust(t, json.Unmarshal(raw, &changed))
			held := r
			changedOp := op
			switch mode {
			case "blank-turn":
				held.Binding.RequestID = "\ufeff"
				changed.RecipeProposal.AfterTurnID = "\ufeff"
			case "blank-model":
				held.Binding.Model = "\ufeff"
				changed.RecipeProposal.Attribution.Model = "\ufeff"
			case "id":
				changed.RecipeProposal.DraftID = "recipe-foreign"
			case "time":
				changed.RecipeProposal.CreatedAt = "bad-clock"
			case "turn":
				changed.RecipeProposal.AfterTurnID = "foreign"
			case "model":
				changed.RecipeProposal.Attribution.Model = "foreign"
			case "candidate":
				changed.RecipeProposal.Fields[0].Type = "number"
			case "phase":
				held.Binding.Phase = "plan"
			case "child":
				changed.Root = false
			case "mixed":
				changed.Policy = &assistantPolicyHostBinding{}
			case "operation":
				changedOp.ID = "foreign"
			}
			changedOp.Intent, _ = json.Marshal(changed)
			if validateAssistantDialectRecipeIntent(held, changedOp, changed) == nil {
				t.Fatal("metadata binding refused to distinguish", mode)
			}
		}
		for _, name := range []string{"list_dialects", "read_dialect_recipe", "get_dialect_recipe_instructions"} {
			changedOp := op
			changedOp.Name = name
			if validateAssistantDialectRecipeIntent(r, changedOp, in) == nil {
				t.Fatal("read carried producer metadata", name)
			}
		}
	}
	in := assistantHostIntent{CallID: "capacity", Root: true, Arguments: []byte(assistantRecipeCandidate)}
	if prepareAssistantDialectRecipeIntent(assistantRunRecord{Binding: r.Binding}, &in, 16) == nil || in.RecipeProposal != nil {
		t.Fatal("source capacity admitted new metadata")
	}
	held := assistantRunRecord{Binding: r.Binding}
	for i := 0; i < 16; i++ {
		held.Operations = append(held.Operations, assistantRunOperation{Kind: "host", Name: "propose_dialect_recipe", State: "uncertain"})
	}
	if prepareAssistantDialectRecipeIntent(held, &in, 0) == nil {
		t.Fatal("uncertain intents did not count toward capacity")
	}
}

// The pre-write authorization belongs inside the actual file CAS, after bounded
// scanning and just before the mutation. Post-read refusal cannot undo a write.
func TestAssistantDialectRecipeFileAuthorityBeforeWrite(t *testing.T) {
	s, _ := newTestServer(t, false)
	checks := 0
	f := s.draftRepositoryWithAuthority(func(context.Context) error {
		checks++
		if checks >= 2 {
			return errAssistantRunScope
		}
		return nil
	})
	a, err := readAssistantDialectRecipeArguments("propose_dialect_recipe", []byte(assistantRecipeCandidate))
	assistantRunMust(t, err)
	d := dialectDraftDocument{Version: 1, ID: dialectDraftIdentity("authority", nil), Name: a.Content.Name, Recipe: a.Content.recipe(), CreatedAt: "2026-10-20T00:00:00Z", UpdatedAt: "2026-10-20T00:00:00Z"}
	if _, err = f.CompareAndSwap(t.Context(), d, ""); err == nil || checks < 2 {
		t.Fatal("pre-write authority not checked")
	}
	if _, err = s.draftRepository().Get(t.Context(), d.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("withdrawn authority physically wrote", err)
	}
}
