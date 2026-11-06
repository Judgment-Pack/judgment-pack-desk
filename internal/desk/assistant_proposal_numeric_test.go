package desk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// The top-level event document map is serialized in sorted order by Desk;
// nested raw facts keep the accepted event order (see continuation fixture).
const assistantNumericCanonicalDocument = `{"decimal":1.2,"exponent":100,"flag":false,"large":9007199254740991,"negativeZero":0,"nested":{"amount":2.5},"nil":null,"numeric":"1"}`

// This is original provider wire, never parsed and re-emitted by a fixture.
// Never replace the unsafe token with its rounded JavaScript representation.
const assistantNumericUnsafeDocument = `{"facts":{"items":[{"amount":9007199254740993}]}}`

// The fenced envelope deliberately keeps document.
func assistantNumericFence(document string) string {
	return "```json\n" + `{"proposal":{"kind":"create","document":` + document + `,"unknowns":[]}}` + "\n```"
}

// Exercise the real private Manager and dynamic tool callback, using the same
// handwritten protocol peer as the existing actual-native creation fixtures.
func assistantNumericNative(t *testing.T, s *Server, text, args string) string {
	t.Helper()
	registry, err := s.readAIRegistry()
	assistantRunMust(t, err)
	model := "shared-model"
	registry.Connections[0].Assistant.Engine = "codex"
	registry.Connections[0].Assistant.Endpoint = nil
	registry.Connections[0].Assistant.Agent = &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Model: &model, Models: []string{model}, Effort: "medium", Tools: []string{"get_schema"}}
	raw, err := encodeAIRegistry(registry.aiRegistry)
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	writeProjectFile(t, s.projectDir, assistantProfilePath, `{"profileVersion":1,"codex":{"inherit":true},"critic":"off"}`)
	fixture := t.TempDir()
	binary, trace := filepath.Join(fixture, "codex-peer"), filepath.Join(fixture, "trace.jsonl")
	textExpression := fmt.Sprintf("%q", text)
	if text == "" {
		// A fixed handwritten reply per generation; no accepted output is recorded.
		textExpression = "json.loads(open(trace+'.replies').read())[index-1]"
	}
	peer := strings.Replace(assistantExecutionCodexPeer, "text='Fixture answer.'", "text="+textExpression, 1)
	branch := "elif mode=='brief': finish()\n  else: tool(offered('get_schema'),{})"
	if args != "" {
		// Python decodes only the tool arguments; documentJson remains original text.
		branch = "elif mode=='brief': tool(offered('propose_pack'),json.loads(" + fmt.Sprintf("%q", args) + "))\n  else: tool(offered('get_schema'),{})"
	}
	peer = strings.Replace(peer, "else: tool(offered('get_schema'),{})", branch, 1)
	assistantRunMust(t, os.WriteFile(filepath.Join(fixture, "peer.py"), []byte(peer), 0600))
	data, err := s.openChatData()
	assistantRunMust(t, err)
	ledgerPath := filepath.Join(data.root.Name(), s.assistantRuns().name)
	assistantRunMust(t, data.root.Close())
	wrapper := fmt.Sprintf("#!/bin/sh\nexec python3 '%s' brief '%s' '%s'\n", filepath.Join(fixture, "peer.py"), trace, ledgerPath)
	assistantRunMust(t, os.WriteFile(binary, []byte(wrapper), 0700))
	manager, err := codexbridge.NewManager(codexbridge.Options{Binary: binary, ProfileDir: filepath.Join(fixture, "account"), ProjectDir: s.cfg.ProjectDir})
	assistantRunMust(t, err)
	t.Cleanup(manager.Close)
	s.codex = manager
	s.aiAccounts.values = map[string]providerAccountManager{testAIAlpha: manager}
	return trace
}

func assistantNumericNoProposal(t *testing.T, s *Server, run assistantRunView) {
	t.Helper()
	if run.Creation != nil {
		t.Fatal("numeric refusal minted creation receipt", run.Creation)
	}
	for _, event := range run.Events {
		if event.Event.Proposal != nil {
			t.Fatal("unsafe provider document became accepted proposal")
		}
	}
	for _, record := range assistantAuthoringLedger(t, s).Runs {
		if record.ID != run.ID {
			continue
		}
		if record.Creation != nil {
			t.Fatal("refusal persisted creation receipt")
		}
		for _, event := range record.Events {
			if event.Event.Proposal != nil {
				t.Fatal("refusal persisted proposal")
			}
		}
	}
}

func TestAssistantProposalNumericActualAPI(t *testing.T) {
	for _, tc := range []struct {
		name, document, canonical string
		requests                  int
	}{
		{"stable", creationContinuationFacts, assistantNumericCanonicalDocument, 1},
		{"adjacent-exact", `{"n":9007199254740992}`, `{"n":9007199254740992}`, 1},
		{"unsafe", assistantNumericUnsafeDocument, "", 1},
		{"decimal-loss", `{"n":0.10000000000000001}`, "", 1},
		{"work-bound", `{"n":0e10000000}`, "", 1},
		{"malformed-reformat", `{"n":1,}`, `{"n":1}`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				document := tc.document
				if n > 1 {
					if tc.requests != 2 {
						t.Error("numeric refusal automatically retried/reformatted")
					}
					document = tc.canonical
				}
				assistantBriefSSE(w, assistantNumericFence(document), "stop")
			}))
			defer provider.Close()
			s, token := assistantAuthoringFixture(t, provider.URL)
			a := assistantAuthoringRequestFixture("numeric-api", "author-pack")
			ref := assistantAuthoringCaptureFixture(t, s, token, a)
			first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start), 201)
			state := "complete"
			if tc.canonical == "" {
				state = "stopped"
			}
			run := assistantProtocolWait(t, s, token, first.ID, state)
			if calls.Load() != int32(tc.requests) {
				t.Fatal("unexpected requests", calls.Load())
			}
			if tc.canonical == "" {
				assistantNumericNoProposal(t, s, run)
			} else {
				assistantNumericAccepted(t, run, tc.canonical)
			}
			if string(s.readProjectFile("one.pack.json").data) != reviewPack {
				t.Fatal("proposal mutated saved pack")
			}
		})
	}
}

func assistantNumericAccepted(t *testing.T, run assistantRunView, expected string) {
	t.Helper()
	found := 0
	for _, event := range run.Events {
		if event.Event.Proposal != nil {
			found++
			if got := string(assistantChatJSON(event.Event.Proposal.Document)); got != expected {
				t.Fatalf("canonical proposal bytes: %s, want %s", got, expected)
			}
		}
	}
	if found != 1 {
		t.Fatal("expected one accepted proposal", found)
	}
}

func TestAssistantProposalNumericActualNative(t *testing.T) {
	for _, tc := range []struct {
		name, document string
		retired        bool
	}{
		{"stable", creationContinuationFacts, false}, {"unsafe", assistantNumericUnsafeDocument, false}, {"retired-document", `{"n":1}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, token := assistantAuthoringFixture(t, "https://example.invalid")
			args := string(assistantChatJSON(map[string]any{"documentJson": tc.document, "unknowns": []string{}}))
			if tc.retired {
				args = `{"document":{"n":1},"documentJson":"{\"n\":1}"}`
			}
			trace := assistantNumericNative(t, s, "Proposal attempt finished.", args)
			a := assistantAuthoringRequestFixture("numeric-native", "author-pack")
			a.Effort = "medium"
			ref := assistantAuthoringCaptureFixture(t, s, token, a)
			first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", ref.Start), 201)
			run := assistantProtocolWait(t, s, token, first.ID, "complete")
			if tc.name == "stable" {
				assistantNumericAccepted(t, run, assistantNumericCanonicalDocument)
			} else {
				assistantNumericNoProposal(t, s, run)
			}
			generations, answers := 0, 0
			for _, row := range assistantCodexLog(t, trace) {
				switch string(row["type"]) {
				case `"generation"`:
					generations++
					var request struct {
						DynamicTools []struct {
							Description string
							InputSchema json.RawMessage `json:"inputSchema"`
						} `json:"dynamicTools"`
					}
					assistantRunMust(t, json.Unmarshal(row["request"], &request))
					offered := false
					for _, tool := range request.DynamicTools {
						if strings.HasPrefix(tool.Description, "propose_pack:") {
							offered = true
							fields, err := assistantTestObject(tool.InputSchema)
							assistantRunMust(t, err)
							properties, err := assistantTestObject(fields["properties"])
							assistantRunMust(t, err)
							documentSchema, err := assistantTestObject(properties["documentJson"])
							assistantRunMust(t, err)
							if properties["document"] != nil || string(documentSchema["type"]) != `"string"` {
								t.Fatal("retired native schema offered", string(tool.InputSchema))
							}
						}
					}
					if !offered {
						t.Fatal("no exported native proposal offer")
					}
				case `"answer"`:
					answers++
					var answer struct {
						Result struct {
							Success bool `json:"success"`
						} `json:"result"`
					}
					assistantRunMust(t, json.Unmarshal(row["answer"], &answer))
					if answer.Result.Success != (tc.name == "stable") {
						t.Fatal("native callback admission result changed", string(row["answer"]))
					}
				}
			}
			if generations != 1 || answers != 1 {
				t.Fatal("native proposal automatically replayed", generations, answers)
			}
			if string(s.readProjectFile("one.pack.json").data) != reviewPack {
				t.Fatal("native proposal saved pack")
			}
		})
	}
}

func TestAssistantCreationNumericRefusal(t *testing.T) {
	for _, engine := range []string{"api", "native"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("JPS_CREATION_OUTPUTS", "")
			var calls atomic.Int32
			// The original unsafe number is within an otherwise usable case proposal.
			document := strings.Replace(assistantCreationCasesDocument, `"zero":0`, `"zero":0,"unsafe":9007199254740993`, 1)
			wire := assistantNumericFence(document)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); assistantBriefSSE(w, wire, "stop") }))
			defer provider.Close()
			s, token := assistantCreationActualFixture(t, provider.URL)
			trace := ""
			if engine == "native" {
				trace = assistantNumericNative(t, s, wire, "")
			}
			recovered := assistantCreationActualRun(t, s, token, assistantCreationAction{Kind: "recover", MessageID: "assistant-one", MessageDigest: digestOf([]byte(assistantAuthoringProposalFixture(json.RawMessage(reviewPack))))}, 1)
			assistantCreationRetainDraft(t, s, token)
			before := assistantCreationContinuationBasis(t, s, token)
			drafts := policyWorkspaceRead(t, s, token, "/api/draft-packs")
			start := assistantCreationActualStart(t, s, token, assistantCreationAction{Kind: "establish-cases", CandidateDigest: recovered.Creation.CandidateDigest}, 2)
			first := assistantProtocolView(t, assistantBriefCall(s, token, "POST", "/api/assistant-runs", start), 201)
			run := assistantProtocolWait(t, s, token, first.ID, "stopped")
			assistantNumericNoProposal(t, s, run)
			after := assistantCreationContinuationBasis(t, s, token)
			for _, key := range []string{"cases", "expectationIssues", "heldProposal", "candidates"} {
				if string(before[key]) != string(after[key]) {
					t.Fatal("numeric refusal changed stored candidate/evidence", key)
				}
			}
			if got := policyWorkspaceRead(t, s, token, "/api/draft-packs"); !bytes.Equal(got.Content, drafts.Content) {
				t.Fatal("numeric refusal mutated draft")
			}
			if engine == "api" && calls.Load() != 1 {
				t.Fatal("API refusal reformatted/retried", calls.Load())
			}
			if engine == "native" {
				generations := 0
				for _, row := range assistantCodexLog(t, trace) {
					if string(row["type"]) == `"generation"` {
						generations++
					}
				}
				if generations != 1 || calls.Load() != 0 {
					t.Fatal("native refusal replayed", generations, calls.Load())
				}
			}
		})
	}
}
