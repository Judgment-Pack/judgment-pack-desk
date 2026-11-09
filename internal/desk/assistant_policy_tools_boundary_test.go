package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func mustAssistantPolicyScope(t *testing.T, e *assistantRunExecution) *assistantPolicyScope {
	t.Helper()
	p, err := readAssistantPolicyScope(e.source)
	assistantRunMust(t, err)
	if p == nil {
		t.Fatal("missing Policy scope")
	}
	return p
}
func TestAssistantPolicyActualNativeParentChild(t *testing.T) {
	assistantPolicyActualNativeParentChild(t, true)
}

func TestAssistantPolicyActualNativeCertifiedShape(t *testing.T) {
	assistantPolicyActualNativeParentChild(t, false)
}

func assistantPolicyActualNativeParentChild(t *testing.T, reusedRoot bool) {
	t.Helper()
	s, _, e, p, trace := assistantPolicyFixture(t, "https://example.invalid", "work", true, nil)
	peer := strings.Replace(assistantExecutionCodexPeer, "if mode=='delegate' and index==1:\n   tool(offered('delegate_tasks'),{'tasks':[{'title':'Inspect schema','objective':'Inspect the schema only.','profile':'review'}]})", `if mode=='delegate' and index==1: tool(offered('read_policy'),{},call='same-provider-call')
  elif mode=='delegate' and index==2: tool(offered('read_policy'),{},call='same-provider-call')
  elif mode=='delegate' and index==3: tool(offered('propose_policy_update'),{'revision':`+fmt.Sprintf("%q", p.Revision)+`,'title':'Native candidate','narrative':`+fmt.Sprintf("%q", assistantPolicyNarrative)+`,'artifactVersion':'1.1.0'},call='same-provider-call')`, 1)
	// Keep repeated root IDs as a separate regression; this additional shape
	// matches the independently certified Graph parent-child-parent fixture.
	if !reusedRoot {
		peer = strings.Replace(peer, "tool(offered('read_policy'),{},call='same-provider-call')", "tool(offered('read_policy'),{},call='parent-read')", 1)
	}
	peer = strings.Replace(peer, "if mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')", `if mode=='delegate' and index==1: tool(offered('delegate_tasks'),{'tasks':[{'title':'Read Policy','objective':'Read the linked Policy only.','profile':'review'}]},call='delegate',rpc='rpc-delegate')
  elif mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')`, 1)
	assistantRunMust(t, os.WriteFile(filepath.Join(filepath.Dir(trace), "peer.py"), []byte(peer), 0600))
	x := assistantPolicyOpen(t, s, e)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
	record := assistantExecutionRecord(t, e)
	callbacks := map[string]bool{}
	roots := map[bool]int{}
	reads := 0
	for _, op := range record.Operations {
		if op.Kind != "host" {
			continue
		}
		var in assistantHostIntent
		assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
		if in.CallbackID == "" || callbacks[in.CallbackID] {
			t.Fatal("native callbacks collapsed")
		}
		callbacks[in.CallbackID] = true
		roots[in.Root]++
		if op.Name == "read_policy" {
			reads++
		}
	}
	if reads != 2 || roots[true] != 2 || roots[false] != 1 || len(record.DomainOutputs) != 1 || record.DomainOutputs[0].PolicyUpdate.Candidate.Narrative != assistantPolicyNarrative {
		t.Fatalf("native Policy custody: reads=%d roots=%v outputs=%d writer=%v", reads, roots, len(record.DomainOutputs), e.writer.Err())
	}
	assistantRunMust(t, validateAssistantDomainOutputs(record))
}
func TestAssistantPolicyFiniteArgumentsAndScope(t *testing.T) {
	revision := "revision_" + strings.Repeat("a", 48)
	publication := "publication_" + strings.Repeat("b", 48)
	for _, v := range []struct{ name, raw string }{
		{"read_policy", `{"policyId":"policy_` + strings.Repeat("a", 48) + `"}`},
		{"read_policy", `{"url":"https://example.invalid"}`},
		{"propose_policy_update", `{"revision":"` + strings.Repeat("a", 64) + `","title":"one","Title":"two","narrative":"x","artifactVersion":"1"}`},
		{"propose_policy_update", `{"revision":"` + strings.Repeat("a", 64) + `","title":null,"narrative":"x","artifactVersion":"1"}`},
		{"propose_policy_update", `{"revision":"` + strings.Repeat("a", 64) + `","title":"x","narrative":"x","artifactVersion":"1","workflow":{}}`},
		{"propose_policy_graph_interpretation", `{"revisionId":"` + revision + `","publicationId":"` + publication + `"}`},
		{"read_policy_comparison", `{"before":{"kind":"revision","RevisionId":"` + revision + `"},"after":{"kind":"publication","publicationId":"` + publication + `"}}`},
		{"read_policy_coverage", `{"coverageId":"latest","candidateId":"current"}`},
	} {
		if _, err := readAssistantPolicyArguments(v.name, []byte(v.raw)); err == nil {
			t.Fatal("invalid Policy args accepted", v.name, v.raw)
		}
	}
	for _, field := range []string{"title", "narrative", "artifactVersion"} {
		args := map[string]string{"revision": strings.Repeat("a", 64), "title": "", "narrative": "", "artifactVersion": ""}
		limits := map[string]int{"title": 500, "narrative": policyNarrativeLimit, "artifactVersion": policyArtifactVersionLimit}
		args[field] = strings.Repeat("x", limits[field]+1)
		raw, _ := json.Marshal(args)
		if _, err := readAssistantPolicyArguments("propose_policy_update", raw); err == nil {
			t.Fatal("oversized Policy field accepted", field)
		}
	}
	s, token, e, p, _ := assistantPolicyFixture(t, "http://127.0.0.1:1", "work", false, nil)
	x := assistantPolicyOpen(t, s, e)
	// A persisted request is data until the shared current service verifies it.
	forged := *mustAssistantPolicyScope(t, e)
	basis := *forged.Request
	basis.Candidate.Narrative = "Caller-supplied basis"
	forged.Request = &basis
	e.input.mu.Lock()
	e.input.policyReady = false
	e.input.mu.Unlock()
	if e.prepareAssistantPolicy(t.Context(), &forged) == nil {
		t.Fatal("forged request basis became authority")
	}
	e.input.mu.Lock()
	e.input.policyReady = true
	e.input.mu.Unlock()
	// The actual session can be withdrawn without changing retained historical
	// data. Even read-only callbacks must refuse current access thereafter.
	s.sessions.revoke(token)
	no, err := assistantGraphFind(t, x.Run.HostTools, "read_policy").Execute(runcontext.WithCallID(t.Context(), "withdrawn"), json.RawMessage(`{}`))
	assistantRunMust(t, err)
	if !no.Err() || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
		t.Fatal("withdrawn session retained execution authority")
	}
	_ = p
}
func TestAssistantPolicyOutputImportAndProjectionAssociations(t *testing.T) {
	s, _, e, p, _ := assistantPolicyFixture(t, "http://127.0.0.1:1", "work", false, nil)
	x := assistantPolicyOpen(t, s, e)
	result := assistantPolicyInvoke(t, e, x, "candidate", "propose_policy_update", map[string]string{"revision": p.Revision, "title": "Candidate"})
	if result.Err() {
		t.Fatal(result.Value().Text())
	}
	record := assistantExecutionRecord(t, e)
	original, _ := json.Marshal(record)
	for _, mutate := range []func(*assistantRunRecord){
		func(r *assistantRunRecord) { r.DomainOutputs = nil },
		func(r *assistantRunRecord) { r.DomainOutputs = append(r.DomainOutputs, r.DomainOutputs[0]) },
		func(r *assistantRunRecord) { r.DomainOutputs[0].RequestKey = "foreign" },
		func(r *assistantRunRecord) { r.DomainOutputs[0].PolicyUpdate.Before.Narrative = "foreign" },
		func(r *assistantRunRecord) { r.DomainOutputs[0].PolicyUpdate.Candidate.Narrative = "foreign" },
		func(r *assistantRunRecord) {
			r.DomainOutputs[0].PolicyUpdate.PolicyID = "policy_" + strings.Repeat("f", 48)
		},
		func(r *assistantRunRecord) { r.DomainOutputs[0].PolicyUpdate.Check.Scope = "approval" },
		func(r *assistantRunRecord) {
			r.DomainOutputs[0].PolicyUpdate.Save = json.RawMessage(`{"state":"complete"}`)
		},
		func(r *assistantRunRecord) { r.DomainOutputs[0].Draft.Path = "foreign.graph.json" },
		func(r *assistantRunRecord) { r.DomainOutputs[0].PolicyUpdate.ID = "caller-id" },
	} {
		var changed assistantRunRecord
		assistantRunMust(t, json.Unmarshal(original, &changed))
		mutate(&changed)
		if validateAssistantDomainOutputs(changed) == nil {
			t.Fatal("mutated Policy output accepted")
		}
	}
	// Reconstructed intent cannot change the linked Policy or request basis while
	// projecting into the frozen original chat, even if all outer IDs are valid.
	var changed assistantRunRecord
	assistantRunMust(t, json.Unmarshal(original, &changed))
	for i := range changed.Operations {
		if changed.Operations[i].Kind == "host" {
			var in assistantHostIntent
			assistantRunMust(t, readAssistantHostReceipt(changed.Operations[i].Intent, &in))
			in.Policy.ID = "policy_" + strings.Repeat("f", 48)
			changed.Operations[i].Intent, _ = json.Marshal(in)
		}
	}
	if validateAssistantPolicyProjectionSource(e.source.Original, changed) == nil {
		t.Fatal("foreign Policy intent projected")
	}
	if err := validateAssistantPolicyProjectionSource(e.source.Original, record); err != nil {
		t.Fatal(err)
	}
}
func TestAssistantPolicyProducerFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"storage", "canceled-observation"} {
		t.Run(mode, func(t *testing.T) {
			s, _, e, p, _ := assistantPolicyFixture(t, "http://127.0.0.1:1", "work", false, nil)
			x := assistantPolicyOpen(t, s, e)
			var mu sync.Mutex
			armed := false
			observed := false
			fault := errors.New("fixture Policy persistence unavailable")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			testHookAssistantPolicyObserved = func() {
				mu.Lock()
				observed = true
				armed = true
				mu.Unlock()
				if mode == "canceled-observation" {
					cancel()
				}
			}
			testHookPrivateRead = func(name string) error {
				mu.Lock()
				defer mu.Unlock()
				if mode == "storage" && armed && name == e.writer.lease.store.name {
					return fault
				}
				return nil
			}
			t.Cleanup(func() { e.cancel(); <-e.done; testHookAssistantPolicyObserved = nil; testHookPrivateRead = nil })
			id := "retained-candidate"
			raw, _ := json.Marshal(map[string]string{"revision": p.Revision, "title": "Exact candidate", "narrative": assistantPolicyNarrative, "artifactVersion": "1.1.0"})
			// Real parent-call journal admission is required; no positive result or output
			// is supplied by this fixture. The producer constructs both before the hook.
			assistantPolicyEvent(t, e, id, "propose_policy_update", raw)
			callctx := context.WithValue(runcontext.WithCallID(ctx, id), assistantHostRootKey{}, true)
			result, err := assistantGraphFind(t, x.Run.HostTools, "propose_policy_update").Execute(callctx, raw)
			assistantRunMust(t, err)
			mu.Lock()
			seen := observed
			armed = false
			mu.Unlock()
			if !seen {
				t.Fatal("real Policy producer not reached")
			}
			if mode == "storage" {
				if !result.Err() || !errors.Is(e.writer.Err(), fault) {
					t.Fatal("failed atomic storage exposed success")
				}
				return
			}
			record := assistantExecutionRecord(t, e)
			if result.Err() || len(record.DomainOutputs) != 1 || record.DomainOutputs[0].PolicyUpdate.Candidate.Narrative != assistantPolicyNarrative {
				t.Fatal("observed Policy candidate lost on cancellation")
			}
			if err := e.Dispatch(t.Context(), record.DomainOutputs[0].OperationID); err == nil {
				t.Fatal("retained operation replay dispatched")
			}
		})
	}
}

func TestAssistantPolicyExactCoverageCandidate(t *testing.T) {
	var selected, current policyCoverageReply
	s, token, e, p, _ := assistantPolicyFixture(t, "http://127.0.0.1:1", "work", false, func(s *Server, token string, p policyReply) policyReply {
		p = policyApprove(t, s, token, p)
		p = policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+p.Document.ID+"/publish", policyPublishBody(t, p))).Policy
		pack := coveragePublishFixture(t, s, []byte(reviewPack))
		service, actor := coverageServiceFixture(s, token)
		q := coverageSaveBody(t, p, pack)
		saved, err := service.coverageCommand(t.Context(), p.Document.ID, actor, q)
		assistantRunMust(t, err)
		service.checkCoverage = coverageAcceptedFixture
		reviewed, err := service.coverageCommand(t.Context(), p.Document.ID, actor, coverageReviewBody(t, *saved.Coverage))
		assistantRunMust(t, err)
		selected = *reviewed.Coverage
		q.OperationID = policyOp(t)
		q.CoverageID = selected.Document.ID
		q.BaseRevision = selected.Revision
		q.Statement = &policyCoverageStatement{Status: "manual", Handoff: "Use a different retained manual review."}
		changed, err := service.coverageCommand(t.Context(), p.Document.ID, actor, q)
		assistantRunMust(t, err)
		current = *changed.Coverage
		return policyRead(t, s, token, p.Document.ID)
	})
	x := assistantPolicyOpen(t, s, e)
	result := assistantPolicyInvoke(t, e, x, "historical-coverage", "read_policy_coverage", map[string]string{"coverageId": selected.Document.ID, "candidateId": selected.Document.Candidate.ID})
	if result.Err() {
		t.Fatal(result.Value().Text())
	}
	var raw map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(result.StructuredContent.JSON(), &raw))
	delete(raw, "guidance")
	retained, err := json.Marshal(raw)
	assistantRunMust(t, err)
	var got policyCoverageReply
	assistantRunMust(t, json.Unmarshal(retained, &got))
	if got.Document.Candidate.ID != selected.Document.Candidate.ID || got.Document.Candidate.Digest != selected.Document.Candidate.Digest || got.Document.Candidate.ID == current.Document.Candidate.ID || got.Document.ReviewState != "reviewed" || len(got.Document.Reviews) != 1 || len(got.Document.Candidates) != 2 || got.Availability.PackPublication != "present" {
		t.Fatal("selected coverage substituted or lost history")
	}
	if got.Document.Candidate.Source != selected.Document.Candidate.Source || got.Document.Candidate.Statement.Status != "implemented" {
		t.Fatal("historical source or declaration changed")
	}
	unchanged := coverageReplyOf(t, s, token, p.Document.ID, selected.Document.ID)
	if unchanged.Revision != current.Revision || unchanged.Document.Candidate.ID != current.Document.Candidate.ID || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
		t.Fatal("coverage read checked, saved or proposed")
	}
}
