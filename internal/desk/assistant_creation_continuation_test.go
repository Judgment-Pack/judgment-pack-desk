package desk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This is an actual provider-admission positive fixture. Unsafe historical raw
// values remain a separate handwritten browser custody witness.
const creationContinuationCanonicalFacts = `{"large":9007199254740991,"decimal":1.2,"exponent":100,"negativeZero":0,"flag":false,"nil":null,"numeric":"1","nested":{"amount":2.5}}`

const creationContinuationFacts = `{"large":9007199254740991,"decimal":1.20,"exponent":1e2,"negativeZero":-0,"flag":false,"nil":null,"numeric":"1","nested":{"amount":2.500}}`

func assistantCreationContinuationDocument(t *testing.T) json.RawMessage {
	t.Helper()
	fields, err := assistantTestObject([]byte(assistantCreationCasesDocument))
	assistantRunMust(t, err)
	rows, err := assistantChatArray(fields["cases"])
	assistantRunMust(t, err)
	invalid := func(id string) json.RawMessage {
		row, err := assistantTestObject(rows[1])
		assistantRunMust(t, err)
		row["id"] = assistantChatJSON(id)
		row["facts"] = []byte(creationContinuationFacts)
		return assistantChatEncodeObject(row)
	}
	return assistantChatJSON(map[string]any{"cases": []json.RawMessage{invalid("first"), rows[0], invalid("last")}})
}

func assistantCreationContinuationFixture(t *testing.T, correction string) (*Server, string, *atomic.Int32) {
	t.Helper()
	proposed := assistantCreationContinuationDocument(t)
	calls := &atomic.Int32{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		document := correction
		if n == 1 {
			document = string(proposed)
		}
		if n > 3 {
			t.Error("independent reviewer replayed", n)
		}
		assistantBriefSSE(w, assistantAuthoringProposalFixture(json.RawMessage(document)), "stop")
	}))
	t.Cleanup(provider.Close)
	s, token := assistantCreationActualFixture(t, provider.URL)
	return s, token, calls
}

// Export actual admitted request sources as well as terminal observations. The
// browser reuses these temporary outputs through its default persistence writers.
func assistantCreationContinuationRun(t *testing.T, s *Server, token string, action assistantCreationAction, n int) assistantRunView {
	t.Helper()
	before := policyWorkspaceRead(t, s, token, "/api/conversations")
	drafts := policyWorkspaceRead(t, s, token, "/api/draft-packs")
	start := assistantCreationActualStart(t, s, token, action, n)
	source := policyWorkspaceRead(t, s, token, "/api/conversations")
	first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 201)
	complete := assistantProtocolWait(t, s, token, first.ID, "complete")
	if complete.ChatProjection == nil || !complete.ChatProjection.Acknowledged {
		t.Fatal("terminal projection unacknowledged", complete.ProjectionIssue)
	}
	if again := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 200); again.ID != first.ID {
		t.Fatal("admission replayed")
	}
	if dir := os.Getenv("JPS_CREATION_OUTPUTS"); dir != "" {
		chat := policyWorkspaceRead(t, s, token, "/api/conversations")
		raw := assistantChatJSON(map[string]any{"before": json.RawMessage(before.Content), "drafts": json.RawMessage(drafts.Content), "source": json.RawMessage(source.Content), "chat": json.RawMessage(chat.Content), "run": complete})
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("%02d-%s.json", n, action.Kind)), raw, 0600))
	}
	return complete
}

func assistantCreationContinuationBasis(t *testing.T, s *Server, token string) map[string]json.RawMessage {
	t.Helper()
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	_, rows, err := assistantChatRows(saved.Content)
	assistantRunMust(t, err)
	chat, err := assistantTestObject(rows[0])
	assistantRunMust(t, err)
	cp, err := assistantTestObject(chat["checkpoint"])
	assistantRunMust(t, err)
	state, err := assistantTestObject(cp["state"])
	assistantRunMust(t, err)
	return state
}

func TestAssistantCreationMultipleCorrections(t *testing.T) {
	assistantCreationMultipleCorrections(t, false)
}

func TestAssistantCreationNumericNativeContinuation(t *testing.T) {
	assistantCreationMultipleCorrections(t, true)
}

func assistantCreationMultipleCorrections(t *testing.T, native bool) {
	t.Helper()
	output := os.Getenv("JPS_CREATION_OUTPUTS")
	if native && output != "" {
		output = filepath.Join(output, "native")
	}
	for _, order := range [][]string{{"first", "last"}, {"last", "first"}} {
		t.Run(order[0], func(t *testing.T) {
			if output != "" {
				dir := filepath.Join(output, order[0])
				assistantRunMust(t, os.MkdirAll(dir, 0700))
				t.Setenv("JPS_CREATION_OUTPUTS", dir)
			}
			s, token, calls := assistantCreationContinuationFixture(t, assistantCreationCorrectionDocument)
			count := calls.Load
			if native {
				trace := assistantNumericNative(t, s, "", "")
				replies := []string{assistantNumericFence(string(assistantCreationContinuationDocument(t))), assistantNumericFence(assistantCreationCorrectionDocument), assistantNumericFence(assistantCreationCorrectionDocument)}
				assistantRunMust(t, os.WriteFile(trace+".replies", assistantChatJSON(replies), 0600))
				count = func() int32 {
					var n int32
					for _, row := range assistantCodexLog(t, trace) {
						if string(row["type"]) == `"generation"` {
							n++
						}
					}
					if calls.Load() != 0 {
						t.Fatal("native continuation reached API fixture")
					}
					return n
				}
			}
			recovered := assistantCreationContinuationRun(t, s, token, assistantCreationAction{Kind: "recover", MessageID: "assistant-one", MessageDigest: digestOf([]byte(assistantAuthoringProposalFixture(json.RawMessage(reviewPack))))}, 1)
			digest := recovered.Creation.CandidateDigest
			assistantCreationRetainDraft(t, s, token)
			established := assistantCreationContinuationRun(t, s, token, assistantCreationAction{Kind: "establish-cases", CandidateDigest: digest}, 2)
			// Preserve canonical accepted event bytes; upstream normalization is
			// recorded separately and never reconstructed from reviewer prose.
			acceptedFacts := map[string]json.RawMessage{}
			for _, run := range assistantAuthoringLedger(t, s).Runs {
				if run.ID == established.ID {
					document, err := assistantCreationModelDocument(run)
					assistantRunMust(t, err)
					rows, err := assistantChatArray(document["cases"])
					assistantRunMust(t, err)
					for _, raw := range rows {
						row, err := assistantTestObject(raw)
						assistantRunMust(t, err)
						id, err := assistantTestString(row["id"])
						assistantRunMust(t, err)
						acceptedFacts[id] = row["facts"]
						if id != "typed" && string(row["facts"]) != creationContinuationCanonicalFacts {
							t.Fatalf("actual admitted canonical facts changed: %s", row["facts"])
						}
					}
				}
			}
			var approved assistantRunView
			for i, id := range order {
				assistantCreationRetainDraft(t, s, token)
				before := assistantCreationContinuationBasis(t, s, token)
				proposal := assistantCreationContinuationRun(t, s, token, assistantCreationAction{Kind: "propose-correction", CandidateDigest: digest, IssueID: id}, 3+i*2)
				after := assistantCreationContinuationBasis(t, s, token)
				for _, key := range []string{"cases", "expectationIssues", "heldProposal", "unknowns"} {
					if !assistantHostArgumentsMatch(before[key], after[key]) {
						t.Fatal("proposal rewrote scope-bound evidence", key)
					}
				}
				approved = assistantCreationContinuationRun(t, s, token, assistantCreationAction{Kind: "approve-correction", CandidateDigest: digest, IssueID: id, Token: proposal.Creation.Proposal.Token}, 4+i*2)
				var issues []assistantCreationIssue
				assistantRunMust(t, json.Unmarshal([]byte(approved.Creation.Issues), &issues))
				if len(issues) != 1-i || approved.Creation.Check == nil || approved.Creation.Check.TotalCases != 2+i {
					t.Fatal("partial correction lost check or unresolved issue", approved.Creation)
				}
			}
			var cases []assistantCreationCase
			assistantRunMust(t, json.Unmarshal([]byte(approved.Creation.Cases), &cases))
			if len(cases) != 3 || cases[0].ID != "first" || cases[1].ID != "typed" || cases[2].ID != "last" {
				t.Fatal("original independent order lost", approved.Creation.Cases)
			}
			if string(cases[0].Facts) != string(acceptedFacts["first"]) || string(cases[2].Facts) != string(acceptedFacts["last"]) {
				t.Fatal("canonical accepted facts changed during continuation")
			}
			assistantCreationRetainDraft(t, s, token)
			checked := assistantCreationContinuationRun(t, s, token, assistantCreationAction{Kind: "check", CandidateDigest: digest}, 7)
			if checked.Creation.Issues != "[]" || checked.Creation.Check.TotalCases != 3 || count() != 3 {
				t.Fatal("final check replayed or lost suite")
			}
			ledger := assistantAuthoringLedger(t, s)
			for _, run := range ledger.Runs {
				assistantRunMust(t, validateAssistantCreationReceipt(run))
			}
			assistantRunMust(t, validateAssistantCreationHeldWitnesses(ledger))
		})
	}
}

func TestAssistantCreationContinuationStopRetainsPartial(t *testing.T) {
	for _, mode := range []string{"mismatch", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("JPS_CREATION_OUTPUTS", "")
			correction := assistantCreationCorrectionDocument
			s, token, calls := assistantCreationContinuationFixture(t, correction)
			// Profiling shows repeated strict validation of the retained ledger
			// across real-Runtime setup actions. Bound each setup action consistently
			// under variable CI and host load while keeping all completion checks.
			const setupWait = 45 * time.Second
			recovered := assistantCreationActualRunWithin(t, s, token, assistantCreationAction{Kind: "recover", MessageID: "assistant-one", MessageDigest: digestOf([]byte(assistantAuthoringProposalFixture(json.RawMessage(reviewPack))))}, 1, setupWait)
			digest := recovered.Creation.CandidateDigest
			assistantCreationRetainDraft(t, s, token)
			assistantCreationActualRunWithin(t, s, token, assistantCreationAction{Kind: "establish-cases", CandidateDigest: digest}, 2, setupWait)
			assistantCreationRetainDraft(t, s, token)
			proposed := assistantCreationActualRunWithin(t, s, token, assistantCreationAction{Kind: "propose-correction", CandidateDigest: digest, IssueID: "first"}, 3, setupWait)
			partial := assistantCreationActualRunWithin(t, s, token, assistantCreationAction{Kind: "approve-correction", CandidateDigest: digest, IssueID: "first", Token: proposed.Creation.Proposal.Token}, 4, setupWait)
			assistantCreationRetainDraft(t, s, token)
			proposed = assistantCreationActualRunWithin(t, s, token, assistantCreationAction{Kind: "propose-correction", CandidateDigest: digest, IssueID: "last"}, 5, setupWait)
			action := assistantCreationAction{Kind: "approve-correction", CandidateDigest: digest, IssueID: "last", Token: proposed.Creation.Proposal.Token}
			var stopped assistantRunView
			if mode == "cancel" {
				marker := assistantCreationBlockedRuntime(t, s, "experimental_test_cases")
				stopped = assistantCreationStopAt(t, s, token, action, 6, marker)
			} else {
				// A local fixture Runtime returns a well-formed mismatch while retaining
				// the real staged operation; no model or expectation is altered to pass.
				bin := assistantCreationMismatchRuntime(t, s)
				s.cfg.JpackBin = bin
				start := assistantCreationActualStart(t, s, token, action, 6)
				first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 201)
				stopped = assistantProtocolWait(t, s, token, first.ID, "stopped")
			}
			if stopped.Creation == nil || stopped.Creation.Check != nil || stopped.Creation.Proposal == nil || stopped.Creation.Cases != partial.Creation.Cases || stopped.Creation.Issues != partial.Creation.Issues || calls.Load() != 3 {
				t.Fatal("second stopped retest published or replayed", stopped.Creation)
			}
		})
	}
}

func assistantCreationMismatchRuntime(t *testing.T, s *Server) string {
	t.Helper()
	assistantCreationBlockedRuntime(t, s, "experimental_test_cases")
	bin := s.cfg.JpackBin
	raw, err := os.ReadFile(bin)
	assistantRunMust(t, err)
	branch := `cases=json.loads(p['arguments']['matrix'])['cases']
  result={'content':[],'structuredContent':{'status':'mismatch','summary':{'total':len(cases),'passed':0},'packs':[{'rows':[{'id':c['id'],'status':'mismatch','expected':json.dumps(c['expectedDisposition']),'actual':'{}'} for c in cases]}]}}`
	text := strings.Replace(string(raw), "time.sleep(30)\n  sys.exit(0)", branch, 1)
	assistantRunMust(t, os.WriteFile(bin, []byte(text), 0700))
	return bin
}
