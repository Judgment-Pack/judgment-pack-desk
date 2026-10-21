package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAssistantDialectMappingProjectionFreshness(t *testing.T) {
	for _, scenario := range []string{"mapping-preparation", "mapping-pack", "publication-preparation", "publication-base", "publication-evidence"} {
		t.Run(scenario, func(t *testing.T) {
			s, token, e := assistantRecipeBrowserFixture(t, "http://127.0.0.1:1")
			saved := assistantMappingSeed(t, s)
			x := assistantMappingOpen(t, s, e)
			read, proposal := "read_dialect", "propose_dialect"
			if strings.HasPrefix(scenario, "publication") {
				read, proposal = "read_dialect_publication", "propose_dialect_publication"
			}
			if r := assistantRecipeInvoke(t, e, x, "read", read, assistantMappingArguments(saved, read)); r.Err() {
				t.Fatal(r.Content)
			}
			if r := assistantRecipeInvoke(t, e, x, "proposal", proposal, assistantMappingArguments(saved, proposal)); r.Err() {
				t.Fatal(r.Content)
			}
			record := assistantMappingObserve(t, s, e, 1)
			cp := *record.Checkpoint
			cp.Status = "complete"
			assistantRunMust(t, e.Checkpoint(t.Context(), cp))
			assistantRunMust(t, e.writer.State(t.Context(), "complete"))
			assistantRunMust(t, s.prepareAssistantChatProjection(e.request.Clone(t.Context()), record.ID))
			switch scenario {
			case "mapping-preparation", "publication-preparation":
				d := saved.Document
				d.Name = "Changed"
				_, err := s.preparationRepository().CompareAndSwap(t.Context(), d, saved.SHA256)
				assistantRunMust(t, err)
			case "mapping-pack":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "mapped.json"), []byte(qualificationTestPack+"\n"), 0600))
			case "publication-base":
				_ = checkAndPublishDialect(t, s, saved, "asserted", "1.0.0")
			case "publication-evidence":
				ref := record.DomainOutputs[0].DialectAction.Action.Publication.Answer.Review
				root, err := s.openRetainedArtifacts(false)
				assistantRunMust(t, err)
				assistantRunMust(t, root.Remove(ref.SHA256))
				assistantRunMust(t, root.Close())
			}
			if err := s.writeAssistantChatProjection(e.request.Clone(t.Context()), record.ID); err == nil {
				t.Fatal("changed source/evidence projected a fresh action")
			}
			current := policyWorkspaceRead(t, s, token, "/api/conversations")
			if bytes.Contains(current.Content, []byte(`"kind":"update-dialect"`)) || bytes.Contains(current.Content, []byte(`"kind":"publish-dialect"`)) {
				t.Fatal("refused projection wrote review action")
			}
			if w := policyCall(s, token, "GET", "/api/assistant-runs/"+record.ID, nil); w.Code != 200 {
				t.Fatal("freshness blocked historical GET")
			}
			assistantRunMust(t, assistantRecipeImport(t, e, assistantExecutionRecord(t, e)))
		})
	}
}
func TestAssistantDialectMappingRawCustody(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	saved := assistantMappingSeed(t, s)
	x := assistantMappingOpen(t, s, e)
	for _, name := range []string{"read_dialect", "propose_dialect"} {
		if r := assistantRecipeInvoke(t, e, x, name, name, assistantMappingArguments(saved, name)); r.Err() {
			t.Fatal(r.Content)
		}
	}
	record := assistantMappingObserve(t, s, e, 1)
	for _, member := range []string{"ready", "action.version", "action.recipeVersion", "preview.recordCount", "preview.recordsWithIssues", "action.dialect.facts", "action.dialect.facts.kind", "foreign"} {
		t.Run(member, func(t *testing.T) {
			raw := assistantChatJSON(assistantRunLedger{Version: 1, Project: record.Binding.Project, Runs: []assistantRunRecord{record}})
			var ledger map[string]json.RawMessage
			assistantRunMust(t, json.Unmarshal(raw, &ledger))
			var runs []map[string]json.RawMessage
			assistantRunMust(t, json.Unmarshal(ledger["runs"], &runs))
			var outputs []map[string]json.RawMessage
			assistantRunMust(t, json.Unmarshal(runs[0]["domainOutputs"], &outputs))
			var p map[string]json.RawMessage
			assistantRunMust(t, json.Unmarshal(outputs[0]["dialectAction"], &p))
			if member == "foreign" {
				p["recipeProposal"] = json.RawMessage(`{}`)
			} else if member == "ready" {
				p[member] = json.RawMessage(`null`)
			} else {
				keys := strings.Split(member, ".")
				var f map[string]json.RawMessage
				assistantRunMust(t, json.Unmarshal(p[keys[0]], &f))
				if len(keys) == 2 {
					f[keys[1]] = json.RawMessage(`null`)
				} else {
					var dialect map[string]json.RawMessage
					assistantRunMust(t, json.Unmarshal(f["dialect"], &dialect))
					if len(keys) == 3 {
						dialect["facts"] = json.RawMessage(`null`)
					} else {
						var facts []map[string]json.RawMessage
						assistantRunMust(t, json.Unmarshal(dialect["facts"], &facts))
						var conversion map[string]json.RawMessage
						assistantRunMust(t, json.Unmarshal(facts[0]["conversion"], &conversion))
						conversion["kind"] = json.RawMessage(`null`)
						facts[0]["conversion"] = assistantChatJSON(conversion)
						dialect["facts"] = assistantChatJSON(facts)
					}
					f["dialect"] = assistantChatJSON(dialect)
				}
				p[keys[0]] = assistantChatJSON(f)
			}
			outputs[0]["dialectAction"] = assistantChatJSON(p)
			runs[0]["domainOutputs"] = assistantChatJSON(outputs)
			ledger["runs"] = assistantChatJSON(runs)
			if _, err := decodeAssistantRunStorage(e.writer.lease.store.name, assistantChatJSON(ledger)); err == nil {
				t.Fatal("raw null/foreign custody accepted")
			}
		})
	}
	// Current metadata is mandatory before a proposal can attempt check effects.
	var in assistantHostIntent
	var op assistantRunOperation
	for _, held := range record.Operations {
		if held.Name == "propose_dialect" {
			op = held
			assistantRunMust(t, readAssistantHostReceipt(held.Intent, &in))
		}
	}
	if in.DialectAction == nil {
		t.Fatal("metadata absent")
	}
	forged := in
	meta := *in.DialectAction
	meta.ID = "dialect-action-forged"
	forged.DialectAction = &meta
	if validateAssistantDialectIntent(record, op, forged) == nil {
		t.Fatal("derived action identity was replaceable")
	}
	wrong := in.DialectAction.ReadOperationID
	p := record.DomainOutputs[0].DialectAction
	if _, _, _, err := readAssistantDialectCheck(record, p.CheckID, wrong); err == nil {
		t.Fatal("check borrowed an earlier read's custody")
	}
	reordered := assistantVariantClone(t, record)
	for i := range reordered.Operations {
		if reordered.Operations[i].ID == p.CheckID {
			for j := range reordered.Operations {
				if reordered.Operations[j].ID == op.ID {
					reordered.Operations[i], reordered.Operations[j] = reordered.Operations[j], reordered.Operations[i]
					break
				}
			}
			break
		}
	}
	if _, _, _, err := readAssistantDialectCheck(reordered, p.CheckID, op.ID); err == nil {
		t.Fatal("check before producer accepted")
	}
	for _, name := range []string{"list_dialects", "list_decisions", "unknown_historical_tool"} {
		raw := assistantChatJSON(assistantHostResult{Result: []byte(`{"content":[]}`), DialectRead: &p.Read})
		if validateAssistantDomainOutputs(assistantRunRecord{Operations: []assistantRunOperation{{Kind: "host", Name: name, Result: raw}}}) == nil {
			t.Fatal("unrelated host accepted Dialect custody")
		}
	}

	for _, n := range []int{31, 32, 33} {
		copy := in
		copy.DialectAction = nil
		if prepareAssistantDialectAction(record, op.Name, &copy, n) == nil {
			t.Fatal("action capacity bypassed")
		}
	}
	// Existing unrelated raw actions and applied fields remain exact; conflicting
	// deterministic IDs can never replace a different candidate.
	existing := json.RawMessage(`{"id":"unrelated","kind":"research","opaque":{"A":0,"a":false}}`)
	chat := map[string]json.RawMessage{"inputActions": assistantChatEncodeArray([]json.RawMessage{existing})}
	assistantRunMust(t, appendAssistantDialectActions(chat, record.DomainOutputs))
	rows, err := assistantChatArray(chat["inputActions"])
	assistantRunMust(t, err)
	if !bytes.Equal(rows[0], existing) {
		t.Fatal("unrelated raw action changed")
	}
	var action map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(rows[1], &action))
	action["name"] = assistantChatJSON("Different")
	chat["inputActions"] = assistantChatEncodeArray([]json.RawMessage{assistantChatJSON(action)})
	if appendAssistantDialectActions(chat, record.DomainOutputs) == nil {
		t.Fatal("conflicting action identity overwritten")
	}
	// Source inspection and cancellation remain available after a completed output.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e.authorizeAssistantDialect(ctx) == nil {
		t.Fatal("canceled authority admitted")
	}
}

func TestAssistantDialectMappingUnsupportedOwner(t *testing.T) {
	s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
	saved := assistantMappingSeed(t, s)
	x := assistantMappingOpen(t, s, e)
	owner := newMemoryArtifacts()
	s.retainedArtifacts = owner
	if result := assistantRecipeInvoke(t, e, x, "read", "read_dialect_publication", assistantMappingArguments(saved, "read_dialect_publication")); result.Err() {
		t.Fatal(result.Content)
	}
	result := assistantRecipeInvoke(t, e, x, "proposal", "propose_dialect_publication", assistantMappingArguments(saved, "propose_dialect_publication"))
	if !result.Err() {
		t.Fatal("unsupported owner admitted host mutation")
	}
	r := assistantExecutionRecord(t, e)
	for _, op := range r.Operations {
		if op.Name == "dialect/check" {
			t.Fatal("unsupported owner admitted Runtime effect")
		}
	}
	if len(owner.data) != 0 || e.ctx.Err() != nil {
		t.Fatal("known prewrite refusal treated as unknown", len(owner.data), e.ctx.Err())
	}
	// A normal/manual caller retains its original injected-owner contract.
	ref, err := s.artifactRepository().Put(t.Context(), []byte("manual immutable artifact"))
	assistantRunMust(t, err)
	raw, err := s.artifactRepository().Get(t.Context(), ref)
	assistantRunMust(t, err)
	if string(raw) != "manual immutable artifact" {
		t.Fatal("manual owner changed")
	}
}

func TestAssistantDialectMappingQueuedSourceAndCancel(t *testing.T) {
	for _, mode := range []string{"source", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
			saved := assistantMappingSeed(t, s)
			x := assistantMappingOpen(t, s, e)
			if result := assistantRecipeInvoke(t, e, x, "read", "read_dialect_publication", assistantMappingArguments(saved, "read_dialect_publication")); result.Err() {
				t.Fatal(result.Content)
			}
			root, err := s.openRetainedArtifacts(true)
			assistantRunMust(t, err)
			defer root.Close()
			lock, err := root.Open(".")
			assistantRunMust(t, err)
			defer lock.Close()
			assistantRunMust(t, waitPrivateDataLock(t.Context(), lock, true))
			entered := make(chan struct{}, 1)
			s.retainedArtifacts = fileRetainedArtifactRepository{open: func(create bool) (*os.Root, error) {
				select {
				case entered <- struct{}{}:
				default:
				}
				return s.openRetainedArtifacts(create)
			}}
			name, call := "propose_dialect_publication", "queued-publication"
			raw := json.RawMessage(assistantMappingArguments(saved, name))
			assistantRunMust(t, e.writer.boundEvent(t.Context(), contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: name, Args: raw}}))
			tool := assistantGraphFind(t, x.Run.HostTools, name)
			ctx := context.WithValue(runcontext.WithCallID(t.Context(), call), assistantHostRootKey{}, true)
			type answer struct {
				result runcontext.ToolResult
				err    error
			}
			done := make(chan answer, 1)
			go func() { r, err := tool.Execute(ctx, raw); done <- answer{r, err} }()
			assistantMappingAwait(t, entered)
			if mode == "source" {
				doc := saved.Document
				doc.Recipe.Intent = "Changed while artifact owner queued"
				_, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
				assistantRunMust(t, err)
			} else {
				e.cancel()
			}
			assistantRunMust(t, lock.Close())
			select {
			case a := <-done:
				assistantRunMust(t, a.err)
				if !a.result.Err() {
					t.Fatal("queued changed authority/source produced ready output")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("queued producer did not drain")
			}
			r := assistantExecutionRecord(t, e)
			if len(r.DomainOutputs) != 0 {
				t.Fatal("queued changed source produced action")
			}
			ledger, err := s.readDialectPublicationLedger()
			assistantRunMust(t, err)
			if len(ledger.Items) != 0 {
				t.Fatal("check published")
			}
			if mode == "cancel" {
				file, err := root.Open(".")
				assistantRunMust(t, err)
				defer file.Close()
				entries, err := file.ReadDir(-1)
				assistantRunMust(t, err)
				if len(entries) != 1 || entries[0].Name() != ".gitignore" {
					t.Fatal("canceled queued check wrote artifact")
				}
			}
		})
	}
}

func TestAssistantDialectMappingBoundedRead(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(fmt.Sprint(large), func(t *testing.T) {
			s, _, e := assistantExecutionFixture(t, "http://127.0.0.1:1", "work")
			saved := assistantMappingSeed(t, s)
			d := saved.Document
			d.Records[0].Values["constructor"] = "opaque column data"
			if large {
				for i := 0; i < 1001; i++ {
					d.Records[0].Values[fmt.Sprintf("column-%d", i)] = "value"
				}
			}
			_, err := s.preparationRepository().CompareAndSwap(t.Context(), d, saved.SHA256)
			assistantRunMust(t, err)
			x := assistantMappingOpen(t, s, e)
			r := assistantRecipeInvoke(t, e, x, "read", "read_dialect", assistantMappingArguments(saved, "read_dialect"))
			if r.Err() != large || e.writer.Err() != nil || e.ctx.Err() != nil {
				t.Fatal("bounded read corrupted its journal instead of refusing", r.Err(), e.writer.Err(), e.ctx.Err())
			}
			if !large && !strings.Contains(r.Content[0].Text(), "constructor") {
				t.Fatal("opaque column hidden")
			}
		})
	}
}
