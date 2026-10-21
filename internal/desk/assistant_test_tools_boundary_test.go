package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func assistantTestHostReplaceSuite(t *testing.T, s *Server, token, owner string, change func(map[string]json.RawMessage)) {
	t.Helper()
	saved := policyWorkspaceRead(t, s, token, "/api/pack-tests")
	fields, err := assistantTestObject(saved.Content)
	assistantRunMust(t, err)
	suites, err := assistantTestObject(fields["suites"])
	assistantRunMust(t, err)
	suite, err := assistantTestSuite(suites[owner])
	assistantRunMust(t, err)
	change(suite)
	suites[owner] = assistantChatEncodeObject(suite)
	fields["suites"] = assistantChatEncodeObject(suites)
	policyWorkspaceSave(t, s, token, "/api/pack-tests", json.RawMessage(assistantChatEncodeObject(fields)))
}
func TestAssistantTestHostPlanChildAndFreshness(t *testing.T) {
	for _, mode := range []string{"plan", "child-read", "pack", "cases", "draft-finalized", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			phase := "work"
			if mode == "plan" {
				phase = "plan"
			}
			draft := mode == "draft-finalized"
			s, token, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", phase, draft, false)
			x := assistantTestHostOpen(t, s, e)
			if mode == "plan" {
				for _, tool := range x.Run.HostTools {
					if tool.Name == "propose_test_cases" {
						t.Fatal("Plan offered test proposal")
					}
				}
				read := assistantTestHostInvoke(t, e, x, "plan-read", "read_pack_tests", map[string]any{})
				if read.Err() {
					t.Fatal(read.Value().Text())
				}
				tools, err := e.assistantTestTools(x.runtime)
				assistantRunMust(t, err)
				raw, _ := json.Marshal(assistantTestHostProposalArgs())
				ctx := context.WithValue(runcontext.WithCallID(t.Context(), "plan-write"), assistantHostRootKey{}, true)
				no, err := assistantGraphFind(t, tools, "propose_test_cases").Execute(ctx, raw)
				assistantRunMust(t, err)
				if !no.Err() {
					t.Fatal("Plan direct proposal accepted")
				}
				return
			}
			if mode == "child-read" {
				read, err := assistantGraphFind(t, x.Run.HostTools, "read_pack_tests").Execute(runcontext.WithCallID(t.Context(), "child-read"), json.RawMessage(`{}`))
				assistantRunMust(t, err)
				if read.Err() {
					t.Fatal(read.Value().Text())
				}
			} else {
				read := assistantTestHostInvoke(t, e, x, "read", "read_pack_tests", map[string]any{})
				if read.Err() {
					t.Fatal(read.Value().Text())
				}
			}
			switch mode {
			case "pack":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte(strings.Replace(assistantTestPack, `"threshold":"50"`, `"threshold":"51"`, 1)), 0600))
			case "cases":
				assistantTestHostReplaceSuite(t, s, token, "one", func(suite map[string]json.RawMessage) { suite["cases"] = []byte(`[]`) })
			case "draft-finalized":
				saved := policyWorkspaceRead(t, s, token, "/api/draft-packs")
				fields, _ := assistantTestObject(saved.Content)
				drafts, _ := assistantChatArray(fields["drafts"])
				d, _ := assistantTestObject(drafts[0])
				d["finalized"] = assistantChatJSON(map[string]string{"id": "one", "path": "one.json", "digest": digestOf([]byte(assistantTestPack))})
				drafts[0] = assistantChatEncodeObject(d)
				fields["drafts"] = assistantChatEncodeArray(drafts)
				policyWorkspaceSave(t, s, token, "/api/draft-packs", json.RawMessage(assistantChatEncodeObject(fields)))
			case "revoked":
				s.sessions.revoke(token)
			}
			count := len(assistantExecutionRuntimeLog(t, s))
			no := assistantTestHostInvoke(t, e, x, "propose", "propose_test_cases", assistantTestHostProposalArgs())
			if !no.Err() || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 || len(assistantExecutionRuntimeLog(t, s)) != count {
				t.Fatal("stale/scope refusal did work or retained proposal", no.Value().Text())
			}
		})
	}
}
func TestAssistantTestHostCoverageAndMissingSuite(t *testing.T) {
	for _, mode := range []string{"additions", "stale-coverage", "absent-empty", "missing-suite"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false)
			x := assistantTestHostOpen(t, s, e)
			if strings.HasPrefix(mode, "absent") || mode == "missing-suite" {
				saved := policyWorkspaceRead(t, s, token, "/api/pack-tests")
				fields, _ := assistantTestObject(saved.Content)
				suites, _ := assistantTestObject(fields["suites"])
				delete(suites, "one")
				fields["suites"] = assistantChatEncodeObject(suites)
				policyWorkspaceSave(t, s, token, "/api/pack-tests", json.RawMessage(assistantChatEncodeObject(fields)))
			} else {
				assistantTestHostReplaceSuite(t, s, token, "one", func(suite map[string]json.RawMessage) {
					suite["runs"] = assistantChatJSON([]any{map[string]any{"id": "coverage-one", "at": "2026-10-20T00:00:00Z", "packDigest": digestOf([]byte(assistantTestPack)), "packText": assistantTestPack, "packVersion": "1.0.0", "cases": json.RawMessage(suite["cases"]), "origin": "tests", "report": map[string]any{"packs": []any{map[string]any{"coverage": []any{map[string]string{"status": "missing", "id": "boundary-probe"}}}}}}})
				})
			}
			args := map[string]any{}
			if mode == "additions" || mode == "stale-coverage" {
				args["coverageRunId"] = "coverage-one"
			}
			if mode == "stale-coverage" {
				assistantTestHostReplaceSuite(t, s, token, "one", func(suite map[string]json.RawMessage) { suite["cases"] = []byte(`[]`) })
				read := assistantTestHostInvoke(t, e, x, "stale-read", "read_pack_tests", args)
				if !read.Err() {
					t.Fatal("explicit stale coverage accepted")
				}
				return
			}
			read := assistantTestHostInvoke(t, e, x, "read", "read_pack_tests", args)
			if read.Err() {
				t.Fatal(read.Value().Text())
			}
			if mode == "absent-empty" {
				saved := policyWorkspaceRead(t, s, token, "/api/pack-tests")
				fields, _ := assistantTestObject(saved.Content)
				suites, _ := assistantTestObject(fields["suites"])
				suites["one"] = []byte(`{"revision":0,"cases":[],"runs":[],"recovered":[]}`)
				fields["suites"] = assistantChatEncodeObject(suites)
				policyWorkspaceSave(t, s, token, "/api/pack-tests", json.RawMessage(assistantChatEncodeObject(fields)))
			}
			proposalArgs := assistantTestHostProposalArgs()
			if mode == "missing-suite" {
				fields, _ := assistantTestObject([]byte(proposalArgs["document"]))
				delete(fields, "sourceMappings")
				proposalArgs["document"] = string(assistantChatEncodeObject(fields))
			}
			result := assistantTestHostInvoke(t, e, x, "proposal", "propose_test_cases", proposalArgs)
			if mode == "missing-suite" {
				if result.Err() {
					t.Fatal(result.Value().Text())
				}
				suite := assistantTestHostSuite(t, s, token, "one")
				if string(suite["cases"]) != "[]" {
					t.Fatal("missing suite fabricated cases")
				}
				return
			}
			if !result.Err() || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
				t.Fatal("existing case replaced or absent suite recaptured")
			}
			if mode == "additions" {
				fields, _ := assistantTestObject([]byte(assistantTestDocument))
				rows, _ := assistantChatArray(fields["cases"])
				fields["cases"] = assistantChatEncodeArray(rows[1:])
				delete(fields, "sourceMappings")
				args := assistantTestHostProposalArgs()
				args["document"] = string(assistantChatEncodeObject(fields))
				added := assistantTestHostInvoke(t, e, x, "new-ids", "propose_test_cases", args)
				if added.Err() {
					t.Fatal(added.Value().Text())
				}
				suite := assistantTestHostSuite(t, s, token, "one")
				proposals, _ := assistantChatArray(suite["proposals"])
				p, _ := assistantTestObject(proposals[0])
				if string(p["additionsOnly"]) != "true" || string(p["coverageRunId"]) != `"coverage-one"` {
					t.Fatal("gap-filling identity lost")
				}
			}
		})
	}
}
func TestAssistantTestHostTypedAndRuntimeRefusals(t *testing.T) {
	for _, mode := range []string{"released-contract", "wrong-id", "omitted-row", "unordered", "incomplete", "unavailable", "unexplained-type", "foreign-source", "duplicate-explanation"} {
		t.Run(mode, func(t *testing.T) {
			s, _, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false)
			x := assistantTestHostOpen(t, s, e)
			read := assistantTestHostInvoke(t, e, x, "read", "read_pack_tests", map[string]any{})
			if read.Err() {
				t.Fatal(read.Value().Text())
			}
			args := assistantTestHostProposalArgs()
			switch mode {
			case "unexplained-type":
				args["document"] = strings.Replace(assistantTestDocument, `"wrong-type"`, `"wrong-type"`, 1)
				fields, _ := assistantTestObject([]byte(args["document"]))
				delete(fields, "intentionalFactTypes")
				args["document"] = string(assistantChatEncodeObject(fields))
			case "foreign-source":
				args["document"] = strings.Replace(assistantTestDocument, `"/amount":"requirement"`, `"/amount":"invented"`, 1)
			case "duplicate-explanation":
				fields, _ := assistantTestObject([]byte(args["document"]))
				items, _ := assistantChatArray(fields["intentionalFactTypes"])
				fields["intentionalFactTypes"] = assistantChatEncodeArray(append(items, items[0]))
				args["document"] = string(assistantChatEncodeObject(fields))
			default:
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "test-host-mode"), []byte(mode), 0600))
			}
			result := assistantTestHostInvoke(t, e, x, "proposal", "propose_test_cases", args)
			if !result.Err() || len(assistantExecutionRecord(t, e).DomainOutputs) != 0 {
				t.Fatal("incomplete types/identity or source accepted")
			}
			if mode == "released-contract" && !strings.Contains(result.Value().Text(), "#582") {
				t.Fatal("released Runtime limitation hidden")
			}
		})
	}
}
func TestAssistantTestHostLostReadbackAndCancellation(t *testing.T) {
	for _, mode := range []string{"lost-readback", "canceled-observation", "stop-action", "case-conflict", "identity-conflict"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false)
			x := assistantTestHostOpen(t, s, e)
			read := assistantTestHostInvoke(t, e, x, "read", "read_pack_tests", map[string]any{})
			if read.Err() {
				t.Fatal(read.Value().Text())
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var hookMu sync.Mutex
			armed := false
			reads := 0
			testHookAssistantTestsObserved = func() {
				hookMu.Lock()
				armed = true
				hookMu.Unlock()
				if mode == "canceled-observation" {
					cancel()
				} else if mode == "stop-action" {
					record := assistantExecutionRecord(t, e)
					response := policyCall(s, token, "POST", "/api/assistant-runs/"+e.record.ID+"/actions", assistantRunAction{ID: "stop-after-test-observation", Kind: "cancel", Revision: record.Revision})
					if response.Code != 200 {
						t.Fatalf("actual Stop status %d: %s", response.Code, response.Body.String())
					}
					stopped := assistantExecutionRecord(t, e)
					uncertain := false
					for _, op := range stopped.Operations {
						if op.Kind == "host" && op.Name == "propose_test_cases" && op.State == "uncertain" {
							uncertain = true
						}
					}
					if !uncertain {
						t.Fatal("actual Stop did not retain uncertain test producer")
					}
				}
			}
			testHookPrivateRead = func(name string) error {
				hookMu.Lock()
				defer hookMu.Unlock()
				if armed && strings.HasPrefix(name, "pack-tests-") {
					reads++
					if reads == 2 && mode != "canceled-observation" && mode != "stop-action" {
						return errors.New("fixture lost suite readback")
					}
				}
				return nil
			}
			t.Cleanup(func() { testHookAssistantTestsObserved = nil; testHookPrivateRead = nil })
			raw, _ := json.Marshal(assistantTestHostProposalArgs())
			assistantPolicyEvent(t, e, "proposal", "propose_test_cases", raw)
			callctx := context.WithValue(runcontext.WithCallID(ctx, "proposal"), assistantHostRootKey{}, true)
			result, err := assistantGraphFind(t, x.Run.HostTools, "propose_test_cases").Execute(callctx, raw)
			assistantRunMust(t, err)
			hookMu.Lock()
			armed = false
			hookMu.Unlock()
			record := assistantExecutionRecord(t, e)
			if len(record.DomainOutputs) != 1 {
				t.Fatal("prepared observation lost")
			}
			if mode == "canceled-observation" || mode == "stop-action" {
				if result.Err() {
					t.Fatal("completed observation not retained", result.Value().Text())
				}
				suite := assistantTestHostSuite(t, s, token, "one")
				if suite["proposals"] == nil {
					t.Fatal("cancellation lost suite proposal")
				}
				if mode == "stop-action" {
					if record.State != "stopped" && record.State != "interrupted" {
						t.Fatal("retention revived stopped run")
					}
					calls := len(assistantExecutionRuntimeLog(t, s))
					assistantRunMust(t, e.ProjectChat(t.Context()))
					if len(assistantExecutionRuntimeLog(t, s)) != calls {
						t.Fatal("Stop recovery replayed Runtime")
					}
				}
				return
			}
			if !result.Err() {
				t.Fatal("unverified readback exposed needs-review success")
			}
			if mode == "case-conflict" || mode == "identity-conflict" {
				assistantTestHostReplaceSuite(t, s, token, "one", func(suite map[string]json.RawMessage) {
					if mode == "case-conflict" {
						suite["proposals"] = []byte(`[]`)
						suite["cases"] = []byte(`[]`)
					} else {
						proposals, _ := assistantChatArray(suite["proposals"])
						p, _ := assistantTestObject(proposals[0])
						p["message"] = assistantChatJSON("Contradictory retained identity")
						proposals[0] = assistantChatEncodeObject(p)
						suite["proposals"] = assistantChatEncodeArray(proposals)
					}
				})
			}
			before := len(assistantExecutionRuntimeLog(t, s))
			again, err := assistantGraphFind(t, x.Run.HostTools, "propose_test_cases").Execute(context.WithValue(runcontext.WithCallID(t.Context(), "proposal"), assistantHostRootKey{}, true), raw)
			assistantRunMust(t, err)
			if len(assistantExecutionRuntimeLog(t, s)) != before || len(assistantExecutionRecord(t, e).DomainOutputs) != 1 {
				t.Fatal("lost acknowledgement replayed Runtime or producer")
			}
			if mode != "lost-readback" {
				if !again.Err() {
					t.Fatal("conflicting recovery accepted")
				}
				// Raw run snapshots retain observation evidence even when the
				// suite is contradictory; they are not current review actions.
				view := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+e.record.ID, nil), 200)
				retained := false
				for _, op := range view.Operations {
					if op.Kind == "host" && op.Name == "propose_test_cases" {
						for _, original := range record.Operations {
							if original.ID == op.ID {
								retained = op.State == "completed" && bytes.Equal(op.Result, original.Result)
							}
						}
					}
				}
				if !retained {
					t.Fatal("failed materialization hid or changed observed history")
				}
				assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+e.record.ID+"/actions", assistantRunAction{ID: "stop-conflicting-suite", Kind: "cancel", Revision: view.Revision}), 200)
				stopped := assistantProtocolView(t, policyCall(s, token, "GET", "/api/assistant-runs/"+e.record.ID, nil), 200)
				if stopped.State != "stopped" && stopped.State != "interrupted" {
					t.Fatal("suite conflict blocked Stop")
				}
				if e.ProjectChat(t.Context()) == nil {
					t.Fatal("contradictory suite produced a dangling chat link")
				}
				chat := policyWorkspaceRead(t, s, token, "/api/conversations")
				if strings.Contains(string(chat.Content), `"testDesign"`) {
					t.Fatal("unverified proposal link persisted")
				}
				return
			}
			if again.Err() {
				t.Fatal(again.Value().Text())
			}
			suite := assistantTestHostSuite(t, s, token, "one")
			proposals, _ := assistantChatArray(suite["proposals"])
			if len(proposals) != 1 || !bytes.Equal(proposals[0], record.DomainOutputs[0].TestProposal.Proposal) {
				t.Fatal("recovery duplicated or changed proposal")
			}
		})
	}
}
func TestAssistantTestHostImportAssociations(t *testing.T) {
	s, _, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false)
	x := assistantTestHostOpen(t, s, e)
	read := assistantTestHostInvoke(t, e, x, "read", "read_pack_tests", map[string]any{})
	if read.Err() {
		t.Fatal(read.Value().Text())
	}
	result := assistantTestHostInvoke(t, e, x, "proposal", "propose_test_cases", assistantTestHostProposalArgs())
	if result.Err() {
		t.Fatal(result.Value().Text())
	}
	record := assistantExecutionRecord(t, e)
	original, _ := json.Marshal(record)
	mutations := []func(*assistantRunRecord){
		func(r *assistantRunRecord) { r.DomainOutputs = nil },
		func(r *assistantRunRecord) { r.DomainOutputs = append(r.DomainOutputs, r.DomainOutputs[0]) },
		func(r *assistantRunRecord) { r.DomainOutputs[0].TestProposal.Read.PackDigest = strings.Repeat("f", 64) },
		func(r *assistantRunRecord) { r.DomainOutputs[0].TestProposal.ValidationID = "foreign" },
		func(r *assistantRunRecord) { r.DomainOutputs[0].CallID = "foreign" },
		func(r *assistantRunRecord) {
			p, _ := assistantTestObject(r.DomainOutputs[0].TestProposal.Proposal)
			p["caseSnapshots"] = []byte(`{}`)
			r.DomainOutputs[0].TestProposal.Proposal = assistantChatEncodeObject(p)
		},
		func(r *assistantRunRecord) {
			p, _ := assistantTestObject(r.DomainOutputs[0].TestProposal.Proposal)
			p["chatId"] = assistantChatJSON("other-chat")
			r.DomainOutputs[0].TestProposal.Proposal = assistantChatEncodeObject(p)
		},
		func(r *assistantRunRecord) {
			for i := range r.Operations {
				if r.Operations[i].Name == "read_pack_tests" {
					var in assistantHostIntent
					_ = readAssistantHostReceipt(r.Operations[i].Intent, &in)
					in.Root = false
					r.Operations[i].Intent, _ = json.Marshal(in)
				}
			}
		},
	}
	for i, change := range mutations {
		var bad assistantRunRecord
		assistantRunMust(t, json.Unmarshal(original, &bad))
		change(&bad)
		if validateAssistantDomainOutputs(bad) == nil {
			t.Fatal("test output import accepted contradictory custody", i)
		}
	}
}
func TestAssistantTestHostFiniteArguments(t *testing.T) {
	for _, raw := range []string{`{"owner":null}`, `{"Owner":"one"}`, `{"owner":"one","owner":"one"}`, `{"path":"one.json"}`, `{"owner":"one","url":"https://example.invalid"}`} {
		if _, err := readAssistantTestArguments("read_pack_tests", []byte(raw)); err == nil {
			t.Fatal("undeclared or ambiguous target accepted", raw)
		}
	}
	for _, raw := range []string{`{"document":"{}","summary":""}`, `{"document":"{}","summary":"x","unknowns":[null]}`, `{"document":"{}","summary":"x","Summary":"y"}`} {
		if _, err := readAssistantTestArguments("propose_test_cases", []byte(raw)); err == nil {
			t.Fatal("invalid proposal args accepted", raw)
		}
	}
	for _, raw := range []string{`{"matrixVersion":"3","cases":[{"id":"x","facts":{},"expectedErrorClass":"type-error","expectedDisposition":{}}]}`, `{"matrixVersion":"3","cases":[{"id":"x","Facts":{},"expectedErrorClass":"type-error"}]}`, `{"matrixVersion":"3","cases":[{"id":"x","facts":{},"expectedErrorClass":"type-error"},{"id":"x","facts":{},"expectedErrorClass":"type-error"}]}`} {
		if _, err := readAssistantTestMatrix([]byte(raw)); err == nil {
			t.Fatal("ambiguous matrix accepted", raw)
		}
	}
	raw := `{"matrixVersion":"3","cases":[{"id":"x","facts":{"Fact":null,"fact":false,"n":1.20,"zero":0},"expectedErrorClass":"type-error"}],"sourceMappings":{},"intentionalFactTypes":[],"runtimeMember":false}`
	m, err := readAssistantTestMatrix([]byte(raw))
	assistantRunMust(t, err)
	if !strings.Contains(string(m.Runtime), `"n":1.20`) || !strings.Contains(string(m.Runtime), `"runtimeMember":false`) || strings.Contains(string(m.Runtime), "sourceMappings") || strings.Contains(string(m.Runtime), "intentionalFactTypes") {
		t.Fatal("matrix rewrote facts or excluded undocumented metadata", fmt.Sprintf("%s", m.Runtime))
	}
}

func TestAssistantTestHostEnabledCriticProposalCustody(t *testing.T) {
	s, _, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false, true)
	x := assistantTestHostOpen(t, s, e)
	read := assistantTestHostInvoke(t, e, x, "read", "read_pack_tests", map[string]any{})
	if read.Err() {
		t.Fatal(read.Value().Text())
	}
	result := assistantTestHostInvoke(t, e, x, "proposal", "propose_test_cases", assistantTestHostProposalArgs())
	if result.Err() {
		t.Fatal(result.Value().Text())
	}
	if err := e.recheckAssistantCritic(t.Context()); err != nil {
		t.Fatal("host-authored review-only proposal invalidated unchanged critic cases", err)
	}
	before := e.input.critic.fingerprint
	// Reconstruct through the actual published checkpoint JSON reader, retaining
	// the original binding rather than capturing a post-proposal suite.
	record := assistantExecutionRecord(t, e)
	wire, err := json.Marshal(record.Checkpoint)
	assistantRunMust(t, err)
	value, err := runcontext.ParseJSON(wire)
	assistantRunMust(t, err)
	cp, err := runcontext.ReadExecution(value)
	assistantRunMust(t, err)
	assistantRunMust(t, e.Checkpoint(t.Context(), *cp))
	continued := *e
	continued.input = &assistantChatCustody{}
	continued.record = assistantExecutionRecord(t, e)
	next, err := continued.openAssistantExecution(t.Context(), assistantExecutionPrompt{Name: "served"})
	assistantRunMust(t, err)
	defer next.Close()
	if continued.input.critic.fingerprint != before || len(next.Run.Cases) != 1 || next.Run.Context.Approval != nil {
		t.Fatal("proposal continuation rebound critic input or restored authority")
	}
	// Exercise a physical Runtime dispatch after reconstruction, including the
	// existing post-serialization critic fence.
	_, _, err = next.runtime.testTool(t.Context(), "experimental_get_test_matrix_contract", map[string]string{})
	assistantRunMust(t, err)
}

func TestAssistantTestHostObservationSourceCustody(t *testing.T) {
	for _, mode := range []string{"saved-before-append", "draft-before-append", "retained-history"} {
		t.Run(mode, func(t *testing.T) {
			draft := mode == "draft-before-append"
			s, token, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", draft, false)
			x := assistantTestHostOpen(t, s, e)
			read := assistantTestHostInvoke(t, e, x, "read", "read_pack_tests", map[string]any{})
			if read.Err() {
				t.Fatal(read.Value().Text())
			}
			change := func() {
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte(`{"id":"one","changed":true}`), 0600))
			}
			restore := func() {
				assistantRunMust(t, os.WriteFile(filepath.Join(s.cfg.ProjectDir, "one.json"), []byte(assistantTestPack), 0600))
			}
			if draft {
				saved := policyWorkspaceRead(t, s, token, "/api/draft-packs")
				restore = func() { policyWorkspaceSave(t, s, token, "/api/draft-packs", json.RawMessage(saved.Content)) }
				change = func() {
					fields, _ := assistantTestObject(saved.Content)
					drafts, _ := assistantChatArray(fields["drafts"])
					d, _ := assistantTestObject(drafts[0])
					d["finalized"] = assistantChatJSON(map[string]string{"id": "one", "path": "one.json", "digest": digestOf([]byte(assistantTestPack))})
					drafts[0] = assistantChatEncodeObject(d)
					fields["drafts"] = assistantChatEncodeArray(drafts)
					policyWorkspaceSave(t, s, token, "/api/draft-packs", json.RawMessage(assistantChatEncodeObject(fields)))
				}
			}
			if mode != "retained-history" {
				testHookAssistantTestsObserved = change
				t.Cleanup(func() { testHookAssistantTestsObserved = nil })
			}
			args := assistantTestHostProposalArgs()
			result := assistantTestHostInvoke(t, e, x, "proposal", "propose_test_cases", args)
			testHookAssistantTestsObserved = nil
			record := assistantExecutionRecord(t, e)
			if len(record.DomainOutputs) != 1 {
				t.Fatal("source conflict discarded completed observation")
			}
			if mode == "retained-history" {
				if result.Err() {
					t.Fatal(result.Value().Text())
				}
				change()
			} else {
				if !result.Err() {
					t.Fatal("changed source/lifecycle appended proposal or exposed success")
				}
				owner := "one"
				if draft {
					owner = "draft-tests"
				}
				suite := assistantTestHostSuite(t, s, token, owner)
				if suite["proposals"] != nil {
					t.Fatal("source conflict materialized proposal")
				}
				restore()
			}
			calls := len(assistantExecutionRuntimeLog(t, s))
			raw, _ := json.Marshal(args)
			again, err := assistantGraphFind(t, x.Run.HostTools, "propose_test_cases").Execute(context.WithValue(runcontext.WithCallID(t.Context(), "proposal"), assistantHostRootKey{}, true), raw)
			assistantRunMust(t, err)
			if again.Err() || len(assistantExecutionRuntimeLog(t, s)) != calls || len(assistantExecutionRecord(t, e).DomainOutputs) != 1 {
				t.Fatal("observation recovery lost history or replayed producer", again.Value().Text())
			}
		})
	}
}

func TestAssistantTestHostProjectionSuiteCustody(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			s, token, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false)
			x := assistantTestHostOpen(t, s, e)
			read := assistantTestHostInvoke(t, e, x, "read", "read_pack_tests", map[string]any{})
			if read.Err() {
				t.Fatal(read.Value().Text())
			}
			result := assistantTestHostInvoke(t, e, x, "proposal", "propose_test_cases", assistantTestHostProposalArgs())
			if result.Err() {
				t.Fatal(result.Value().Text())
			}
			assistantRunMust(t, e.writer.change(t.Context(), false, func(run *assistantRunRecord) (bool, error) { run.State = "complete"; return true, nil }))
			assistantRunMust(t, s.prepareAssistantChatProjection(e.request.Clone(t.Context()), e.record.ID))
			assistantTestHostReplaceSuite(t, s, token, "one", func(suite map[string]json.RawMessage) {
				suite["proposals"] = []byte(`[]`)
				if conflict {
					suite["cases"] = []byte(`[]`)
				}
			})
			calls := len(assistantExecutionRuntimeLog(t, s))
			err := s.writeAssistantChatProjection(e.request.Clone(t.Context()), e.record.ID)
			chat := policyWorkspaceRead(t, s, token, "/api/conversations")
			if conflict {
				if err == nil || strings.Contains(string(chat.Content), `"testDesign"`) {
					t.Fatal("final write persisted dangling proposal link")
				}
			} else {
				assistantRunMust(t, err)
				suite := assistantTestHostSuite(t, s, token, "one")
				proposals, _ := assistantChatArray(suite["proposals"])
				if len(proposals) != 1 || !strings.Contains(string(chat.Content), `"testDesign"`) {
					t.Fatal("final write did not recover exact suite and link")
				}
			}
			if len(assistantExecutionRuntimeLog(t, s)) != calls {
				t.Fatal("projection suite recovery replayed Runtime")
			}
		})
	}
}
