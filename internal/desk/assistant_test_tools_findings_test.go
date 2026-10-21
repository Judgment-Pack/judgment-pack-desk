package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func assistantTestSelectedCoverage(t *testing.T, s *Server, token, probe string) {
	t.Helper()
	assistantTestHostReplaceSuite(t, s, token, "one", func(suite map[string]json.RawMessage) {
		suite["runs"] = assistantChatJSON([]any{map[string]any{"id": "coverage-one", "at": "2026-10-20T00:00:00Z", "packDigest": digestOf([]byte(assistantTestPack)), "packText": assistantTestPack, "packVersion": "1.0.0", "cases": json.RawMessage(suite["cases"]), "origin": "tests", "report": map[string]any{"packs": []any{map[string]any{"coverage": []any{map[string]string{"status": "missing", "id": probe}}}}}}})
	})
}
func assistantTestAdditionalArgs(t *testing.T) map[string]string {
	t.Helper()
	args := assistantTestHostProposalArgs()
	fields, err := assistantTestObject([]byte(args["document"]))
	assistantRunMust(t, err)
	rows, err := assistantChatArray(fields["cases"])
	assistantRunMust(t, err)
	fields["cases"] = assistantChatEncodeArray(rows[1:])
	delete(fields, "sourceMappings")
	args["document"] = string(assistantChatEncodeObject(fields))
	return args
}

// The two review counterexamples enter through the actual private API adapter.
// The wrong-type fact, explicit explanation and expected error remain unchanged.
func TestAssistantTestHostReviewProducerRefusals(t *testing.T) {
	for _, mode := range []string{"changed-coverage", "null-expected-type"} {
		t.Run(mode, func(t *testing.T) {
			var step atomic.Int32
			var s *Server
			var token string
			args := assistantTestHostProposalArgs()
			if mode == "changed-coverage" {
				args = assistantTestAdditionalArgs(t)
			}
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				switch step.Add(1) {
				case 1:
					readArgs := map[string]any{}
					if mode == "changed-coverage" {
						readArgs["coverageRunId"] = "coverage-one"
					}
					assistantPolicySSE(w, "read_pack_tests", readArgs, "")
				case 2:
					if mode == "changed-coverage" {
						assistantTestSelectedCoverage(t, s, token, "changed-probe")
					}
					assistantPolicySSE(w, "propose_test_cases", args, "")
				default:
					assistantPolicySSE(w, "", nil, "Review completed.")
				}
			})
			var e *assistantRunExecution
			s, token, e, _ = assistantTestHostFixture(t, u.server.URL, "work", false, false)
			if mode == "changed-coverage" {
				assistantTestSelectedCoverage(t, s, token, "original-probe")
			}
			x := assistantTestHostOpen(t, s, e)
			if mode == "null-expected-type" {
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "test-host-mode"), []byte(mode), 0600))
			}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
			record := assistantExecutionRecord(t, e)
			suite := assistantTestHostSuite(t, s, token, "one")
			proposals, _ := assistantChatArray(suite["proposals"])
			if step.Load() != 3 || len(record.DomainOutputs) != 0 || len(proposals) != 0 {
				t.Fatalf("malformed/stale producer accepted: model=%d outputs=%d proposals=%d", step.Load(), len(record.DomainOutputs), len(proposals))
			}
			if mode == "null-expected-type" {
				found, rawNull := false, false
				for _, op := range record.Operations {
					if op.Kind == "runtime" && bytes.Contains(bytes.ReplaceAll(op.Result, []byte(" "), nil), []byte(`"expectedTypes":[null]`)) {
						rawNull = true
					}
					if op.Name == "propose_test_cases" {
						var result assistantHostResult
						assistantRunMust(t, readAssistantHostReceipt(op.Result, &result))
						found = bytes.Contains(result.Result, []byte("#582"))
					}
				}
				if !found || !rawNull {
					t.Fatalf("malformed raw advice or authored unavailability absent: rawNull=%t authored=%t", rawNull, found)
				}
			}
		})
	}
}

func TestAssistantTestHostSelectedCoverageCustody(t *testing.T) {
	for _, mode := range []string{"new-append", "projection-recovery", "retained-history", "unrelated-history"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false)
			x := assistantTestHostOpen(t, s, e)
			assistantTestSelectedCoverage(t, s, token, "original-probe")
			read := assistantTestHostInvoke(t, e, x, "read", "read_pack_tests", map[string]any{"coverageRunId": "coverage-one"})
			if read.Err() {
				t.Fatal(read.Value().Text())
			}
			held, err := e.rootAssistantTestRead(t.Context(), "one")
			assistantRunMust(t, err)
			change := func() { assistantTestSelectedCoverage(t, s, token, "changed-probe") }
			restore := func() { assistantTestSelectedCoverage(t, s, token, "original-probe") }
			// Direct freshness checks are the same precondition used at each physical
			// Runtime dispatch and at the end of the required root read.
			change()
			if e.recheckAssistantTestRead(t.Context(), held) == nil {
				t.Fatal("changed selected coverage passed dispatch/read recheck")
			}
			restore()
			assistantRunMust(t, e.recheckAssistantTestRead(t.Context(), held))
			if mode == "unrelated-history" {
				assistantTestHostReplaceSuite(t, s, token, "one", func(suite map[string]json.RawMessage) {
					runs, _ := assistantChatArray(suite["runs"])
					other, _ := assistantTestObject(runs[0])
					other["id"] = assistantChatJSON("unrelated-run")
					other["report"] = []byte(`{"different":true}`)
					suite["runs"] = assistantChatEncodeArray(append(runs, assistantChatEncodeObject(other)))
					suite["reviewOnly"] = []byte(`{"questions":["Unrelated review metadata"]}`)
				})
				assistantRunMust(t, e.recheckAssistantTestRead(t.Context(), held))
			}
			if mode == "new-append" {
				testHookAssistantTestsObserved = change
				t.Cleanup(func() { testHookAssistantTestsObserved = nil })
			}
			args := assistantTestAdditionalArgs(t)
			result := assistantTestHostInvoke(t, e, x, "proposal", "propose_test_cases", args)
			testHookAssistantTestsObserved = nil
			record := assistantExecutionRecord(t, e)
			if len(record.DomainOutputs) != 1 {
				t.Fatal("completed retained observation lost")
			}
			assistantRunMust(t, validateAssistantDomainOutputs(record))
			assistantRunMust(t, assistantRecipeImport(t, e, record))
			calls := len(assistantExecutionRuntimeLog(t, s))
			if mode == "new-append" {
				if !result.Err() {
					t.Fatal("changed selected coverage appended proposal")
				}
				suite := assistantTestHostSuite(t, s, token, "one")
				proposals, _ := assistantChatArray(suite["proposals"])
				if len(proposals) != 0 {
					t.Fatal("changed selected coverage wrote a new proposal")
				}
				// A retained observation cannot be rematerialized under a different basis.
				if err := s.materializeAssistantTests(e.request.Clone(t.Context()), e.record.ID); err == nil {
					t.Fatal("changed coverage recovered a new append")
				}
				restore()
			} else if result.Err() {
				t.Fatal(result.Value().Text())
			}
			if mode == "projection-recovery" {
				assistantRunMust(t, e.writer.change(t.Context(), false, func(run *assistantRunRecord) (bool, error) { run.State = "complete"; return true, nil }))
				assistantRunMust(t, s.prepareAssistantChatProjection(e.request.Clone(t.Context()), e.record.ID))
				assistantTestHostReplaceSuite(t, s, token, "one", func(suite map[string]json.RawMessage) { suite["proposals"] = []byte(`[]`) })
				change()
				if err := s.writeAssistantChatProjection(e.request.Clone(t.Context()), e.record.ID); err == nil {
					t.Fatal("projection linked new proposal under changed coverage")
				}
				chats := policyWorkspaceRead(t, s, token, "/api/conversations")
				if strings.Contains(string(chats.Content), `"testDesign"`) {
					t.Fatal("dangling proposal link")
				}
				restore()
				assistantRunMust(t, s.writeAssistantChatProjection(e.request.Clone(t.Context()), e.record.ID))
			} else {
				if mode == "retained-history" {
					change()
				}
				raw, _ := json.Marshal(args)
				again, err := assistantGraphFind(t, x.Run.HostTools, "propose_test_cases").Execute(context.WithValue(runcontext.WithCallID(t.Context(), "proposal"), assistantHostRootKey{}, true), raw)
				assistantRunMust(t, err)
				if again.Err() {
					t.Fatal("exact observation recovery refused", again.Value().Text())
				}
			}
			suite := assistantTestHostSuite(t, s, token, "one")
			proposals, _ := assistantChatArray(suite["proposals"])
			if len(proposals) != 1 || !bytes.Equal(proposals[0], record.DomainOutputs[0].TestProposal.Proposal) || len(assistantExecutionRuntimeLog(t, s)) != calls {
				t.Fatal("coverage recovery changed history, duplicated or replayed Runtime")
			}
			// Generic history remains readable even after coverage moves.
			if response := policyCall(s, token, "GET", "/api/assistant-runs/"+e.record.ID, nil); response.Code != 200 {
				t.Fatal("coverage gated generic history", response.Code)
			}
		})
	}
}

func TestAssistantTestHostMalformedAdviceImport(t *testing.T) {
	s, _, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false)
	x := assistantTestHostOpen(t, s, e)
	if r := assistantTestHostInvoke(t, e, x, "read", "read_pack_tests", map[string]any{}); r.Err() {
		t.Fatal(r.Value().Text())
	}
	if r := assistantTestHostInvoke(t, e, x, "proposal", "propose_test_cases", assistantTestHostProposalArgs()); r.Err() {
		t.Fatal(r.Value().Text())
	}
	original := assistantExecutionRecord(t, e)
	assistantRunMust(t, assistantRecipeImport(t, e, original))
	for _, rawTypes := range []string{`[null]`, `["decimal-string",null]`, `[false]`, `[0]`, `[{}]`, `[[]]`, `null`, `[]`} {
		t.Run(rawTypes, func(t *testing.T) {
			bad := assistantVariantClone(t, original)
			found := false
			for i := range bad.Operations {
				op := &bad.Operations[i]
				if op.ID != bad.DomainOutputs[0].TestProposal.ValidationID {
					continue
				}
				envelope, err := assistantTestObject(op.Result)
				assistantRunMust(t, err)
				result, err := assistantTestObject(envelope["result"])
				assistantRunMust(t, err)
				report, err := assistantTestObject(result["structuredContent"])
				assistantRunMust(t, err)
				rows, err := assistantChatArray(report["factChecks"])
				assistantRunMust(t, err)
				for j, raw := range rows {
					row, err := assistantTestObject(raw)
					assistantRunMust(t, err)
					id, _ := assistantTestString(row["id"])
					if id != "wrong-type" {
						continue
					}
					findings, err := assistantChatArray(row["findings"])
					assistantRunMust(t, err)
					finding, err := assistantTestObject(findings[0])
					assistantRunMust(t, err)
					finding["expectedTypes"] = []byte(rawTypes)
					findings[0] = assistantChatEncodeObject(finding)
					row["findings"] = assistantChatEncodeArray(findings)
					rows[j] = assistantChatEncodeObject(row)
					found = true
				}
				report["factChecks"] = assistantChatEncodeArray(rows)
				matrix, err := readAssistantTestMatrix([]byte(assistantTestDocument))
				assistantRunMust(t, err)
				if _, _, err := assistantTestValidationReport(report, matrix); err == nil {
					t.Fatal("raw malformed advice accepted by shared live/receipt parser")
				}
				result["structuredContent"] = assistantChatEncodeObject(report)
				envelope["result"] = assistantChatEncodeObject(result)
				op.Result = assistantChatEncodeObject(envelope)
			}
			if !found {
				t.Fatal("wrong-type finding absent")
			}
			if validateAssistantDomainOutputs(bad) == nil {
				t.Fatal("malformed retained validation advice accepted")
			}
			if assistantRecipeImport(t, e, bad) == nil {
				t.Fatal("malformed validation evidence imported")
			}
			if !bytes.Equal(original.DomainOutputs[0].TestProposal.Proposal, bad.DomainOutputs[0].TestProposal.Proposal) {
				t.Fatal("proposal fact/explanation/answer changed")
			}
		})
	}
}
