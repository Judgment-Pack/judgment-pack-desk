package desk

// Handwritten inputs and signed offline fixture packets exercise the actual
// producers/verifiers. No fixture substitutes a signature verifier or records
// expected output from a previous run. These are direct-host source fixtures;
// they do not qualify the upstream API-final-scope or child-Used regressions.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func assistantExternalEvidenceVector() (assistantRunRecord, assistantHostIntent, assistantRunOperation, assistantHostResult) {
	run, in, op := assistantExternalCustodyVector()
	in.ExternalScope = &assistantExternalScope{Execution: run.Checkpoint.ID, Role: "author", Generation: 1}
	raw := []byte(`{"content":[{"type":"text","text":"Exact signed page"}],"structuredContent":{"big":9007199254740993,"number":1.20},"isError":false}`)
	args, _ := assistantExternalCanonical(in.Arguments)
	result, _ := assistantExternalCanonical(raw)
	a := &assistantExternalReadEvidence{Version: 1, HostOperationID: op.ID, DurableDispatchID: in.DurableDispatchID, Scope: *in.ExternalScope, SourceBindingDigest: digestOf(assistantChatJSON(in.SourceBinding)), OwnTool: op.Name, ArgsCanonical: args, ArgsDigest: digestOf(in.Arguments), ResultCanonical: result, ResultDigest: digestOf(raw), ProjectionVersion: "1", ProjectionLimit: runcontext.Defaults.ToolResultBytes, Proof: assistantExternalReadProof{Kind: "link", AcquisitionID: "physical-original", Packet: attachmentReceipt{ID: "00000000-0000-0000-0000-000000000001", SHA256: strings.Repeat("a", 64)}, Document: &assistantDocumentReference{ID: "00000000-0000-0000-0000-000000000001", Digest: "sha256:" + strings.Repeat("c", 64), Pages: []int{1}}, ConfigurationDigest: strings.Repeat("b", 64), CustodyDigest: strings.Repeat("d", 64)}}
	a.ID = assistantExternalEvidenceID(*a)
	retained := assistantHostResult{Result: raw, ExternalReadEvidence: a}
	op.Intent, op.Result, op.State = assistantChatJSON(in), assistantChatJSON(retained), "completed"
	d := &run.Checkpoint.Durable.Dispatches[0]
	d.State = "result"
	d.Result = raw
	run.Operations = []assistantRunOperation{op}
	return run, in, op, retained
}
func TestAssistantExternalEvidenceClosedExactAssociation(t *testing.T) {
	run, in, op, result := assistantExternalEvidenceVector()
	assistantRunMust(t, validateAssistantExternalEvidence(run, op, in, result))
	if assistantExternalAccepted(run, op, in) {
		t.Fatal("physical result and token granted accepted source custody")
	}
	for _, mode := range []string{"id", "host", "dispatch", "scope", "generation", "binding", "own", "arguments", "argument-lexeme", "result", "result-large-number", "version", "limit", "packet", "config", "custody", "partial", "extra", "null", "case", "duplicate", "missing"} {
		t.Run(mode, func(t *testing.T) {
			r, i, o, v := assistantExternalEvidenceVector()
			a := v.ExternalReadEvidence
			switch mode {
			case "id":
				a.ID = "desk-proof-v1-" + strings.Repeat("f", 64)
			case "host":
				a.HostOperationID = "sibling"
			case "dispatch":
				a.DurableDispatchID = "sibling"
			case "scope":
				a.Scope.Execution = "sibling"
			case "generation":
				a.Scope.Generation++
			case "binding":
				a.SourceBindingDigest = strings.Repeat("e", 64)
			case "own":
				a.OwnTool = assistantSearchTool
			case "arguments":
				a.ArgsCanonical = `{}`
			case "argument-lexeme":
				i.Arguments = bytes.ReplaceAll(i.Arguments, []byte("1.20"), []byte("1.2"))
			case "result":
				a.ResultCanonical = `{}`
			case "result-large-number":
				v.Result = bytes.ReplaceAll(v.Result, []byte("9007199254740993"), []byte("9007199254740992"))
			case "version":
				a.ProjectionVersion = "2"
			case "limit":
				a.ProjectionLimit--
			case "packet":
				a.Proof.Packet.ID = "arbitrary-model-id"
			case "config":
				a.Proof.ConfigurationDigest = ""
			case "custody":
				a.Proof.CustodyDigest = ""
			case "partial":
				a.Proof.Document = nil
			default:
				raw := assistantChatJSON(v)
				switch mode {
				case "extra":
					raw = bytes.Replace(raw, []byte(`"version":1`), []byte(`"extra":true,"version":1`), 1)
				case "null":
					raw = bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":null`), 1)
				case "case":
					raw = bytes.Replace(raw, []byte(`"version":1`), []byte(`"Version":1`), 1)
				case "duplicate":
					raw = bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1)
				case "missing":
					raw = bytes.Replace(raw, []byte(`"version":1,`), nil, 1)
				}
				var got assistantHostResult
				if readAssistantHostReceipt(raw, &got) == nil {
					t.Fatal("open association accepted", mode)
				}
				return
			}
			// Rehash locator mutations so rejection cannot depend only on its hash.
			if mode != "id" {
				a.ID = assistantExternalEvidenceID(*a)
			}
			if validateAssistantExternalEvidence(r, o, i, v) == nil {
				t.Fatal("changed association accepted", mode)
			}
		})
	}
	if bytes.Contains(result.Result, []byte(result.ExternalReadEvidence.ID)) {
		t.Fatal("receipt leaked to result")
	}
}
func TestAssistantExternalEvidenceFinalAcceptedEnvelope(t *testing.T) {
	makeAccepted := func() (assistantRunRecord, assistantHostIntent, assistantRunOperation) {
		run, in, op, retained := assistantExternalEvidenceVector()
		value, _ := runcontext.ParseJSON(retained.Result)
		result := runcontext.ReadToolResult(value)
		source := contract.ToolOfferSourceRecord{}
		_ = json.Unmarshal(in.SourceBinding.SourceRecord, &source)
		receipt := retained.ExternalReadEvidence.ID
		source.Receipt = &receipt
		// Handwritten current Writer stamp; expected data is not generated by it.
		version := int64(1)
		event := &contract.ToolResult{Type: "tool_result", CallID: &in.CallID, Name: in.SourceBinding.WireName, Source: &source, Text: result.Content[0].Text(), IsError: result.Err(), Structured: result.StructuredContent.JSON(), Version: &version}
		run.Canonical.Events = []assistantCanonicalEvent{{Cursor: "accepted", Bytes: assistantChatJSON(runcontext.StoredRunEnvelope{Version: 1, Scope: runcontext.DurableScope{Execution: in.ExternalScope.Execution, Role: "author"}, Event: contract.RunEvent{ToolResult: event}})}}
		return run, in, op
	}
	run, in, op := makeAccepted()
	if !assistantExternalAccepted(run, op, in) {
		t.Fatal("exact final envelope refused")
	}
	for _, mode := range []string{"absent", "claimed", "unknown", "receipt", "source", "call", "wire", "scope", "text", "structured", "error", "version-missing", "version-zero", "version-unsupported", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			r, i, o := makeAccepted()
			var env runcontext.StoredRunEnvelope
			assistantRunMust(t, json.Unmarshal(r.Canonical.Events[0].Bytes, &env))
			switch mode {
			case "absent":
				r.Canonical.Events = nil
			case "claimed":
				r.Checkpoint.Durable.Dispatches[0].State = "claimed"
			case "unknown":
				r.Checkpoint.Durable.Dispatches[0].State = "unknown"
			case "receipt":
				env.Event.ToolResult.Source.Receipt = nil
			case "source":
				env.Event.ToolResult.Source.ID = "desk-search"
			case "call":
				call := "sibling"
				env.Event.ToolResult.CallID = &call
			case "wire":
				env.Event.ToolResult.Name = assistantLinkTool
			case "scope":
				env.Scope.Execution = "sibling"
			case "text":
				env.Event.ToolResult.Text += "changed"
			case "structured":
				env.Event.ToolResult.Structured = []byte(`{"big":"9007199254740993","number":1.20}`)
			case "error":
				env.Event.ToolResult.IsError = true
			case "version-missing":
				env.Event.ToolResult.Version = nil
			case "version-zero":
				version := int64(0)
				env.Event.ToolResult.Version = &version
			case "version-unsupported":
				version := int64(99)
				env.Event.ToolResult.Version = &version
			case "duplicate":
				r.Canonical.Events = append(r.Canonical.Events, r.Canonical.Events[0])
			}
			if len(r.Canonical.Events) > 0 {
				r.Canonical.Events[0].Bytes = assistantChatJSON(env)
			}
			if assistantExternalAccepted(r, o, i) {
				t.Fatal("nonfinal/mutated source accepted", mode)
			}
		})
	}
}

type assistantExternalEvidenceExecutor struct {
	assistantRunExecutor
	entered chan *assistantRunExecution
}

func (*assistantExternalEvidenceExecutor) assistantCanonicalRuns() {}
func (x *assistantExternalEvidenceExecutor) Execute(ctx context.Context, e *assistantRunExecution) error {
	x.entered <- e
	<-ctx.Done()
	return ctx.Err()
}

// Upgrade only the next fixture admission, before it exists. Never rewrite an
// admitted checkpoint or replace its original Store/Writer.
func assistantExternalEvidenceAdmit(t *testing.T, f *assistantSearchFixture) {
	t.Helper()
	old := f.e
	old.cancel()
	<-old.done
	executor := &assistantExternalEvidenceExecutor{assistantRunExecutor: f.s.assistantRunExecutor, entered: make(chan *assistantRunExecution, 1)}
	f.s.assistantRunExecutor = executor
	start := old.source.Start
	start.AdmissionID = "external-evidence-canonical"
	assistantProtocolView(t, policyCall(f.s, f.token, "POST", "/api/assistant-runs", start), 201)
	f.e = <-executor.entered
	t.Cleanup(func() { f.e.cancel(); <-f.e.done })
	build, err := launchedGatewayBuild(f.s.localGateway.bundle)
	assistantRunMust(t, err)
	f.s.localGateway.mu.Lock()
	f.s.localGateway.pin.build = &build
	f.s.localGateway.mu.Unlock()
	// Start only the explicit status fixture; review/currentness use observation.
	_, err = f.s.assistantSearchConfiguration(t.Context(), f.e.source)
	assistantRunMust(t, err)
}
func assistantExternalEvidenceApproved(t *testing.T, f *assistantSearchFixture, id string) (assistantSourceApproval, func(context.Context) error) {
	t.Helper()
	reviewed := assistantSourceTestRow(t, assistantSourceTestView(t, f.s, f.token), id)
	if reviewed.Status != "review-required" || reviewed.ReviewID == "" || reviewed.ReviewRevision != 1 {
		t.Fatal("actual source review selector unavailable", id, reviewed)
	}
	approved := assistantSourceTestApprove(t, f.s, f.token, reviewed)
	store, _, raw := assistantSourceTestStorage(t, f.s)
	ledger, err := decodeAssistantSourceLedger(raw, f.e.record.Binding.Project)
	assistantRunMust(t, err)
	_ = store
	var approval assistantSourceApproval
	for _, a := range ledger.Approvals {
		if a.ID == approved.Approval.ID {
			approval = a
		}
	}
	if approval.ID == "" {
		t.Fatal("actual approval absent")
	}
	current := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		view, err := f.s.assistantSourceApprovalTransaction(f.e.request.WithContext(ctx), "chat-one", "review", assistantSourceApproveRequest{}, assistantSourceRevokeRequest{})
		if err != nil {
			return err
		}
		for _, row := range view.Sources {
			if row.Snapshot.AdapterID == id && row.Status == "approved" && row.Approval != nil && row.Approval.ID == approval.ID && row.Approval.Revision == approval.Revision && reflect.DeepEqual(row.Snapshot, approval.Snapshot) {
				return nil
			}
		}
		return errAssistantRunScope
	}
	return approval, current
}

// Real custom SourceGate, original durable claim/commit, real signed producer
// packets, actual verifier callback and final decoration. No API/native model
// adapter qualification is claimed by this direct-host fixture.
func TestAssistantExternalEvidenceActualProducersAndReplay(t *testing.T) {
	for _, kind := range []string{"search", "link", "saved-link", "website", "partial", "no-text"} {
		t.Run(kind, func(t *testing.T) {
			var f *assistantSearchFixture
			var savedPacket []byte
			var savedReference assistantDocumentReference
			expectedCalls, expectedAcquisitions, expectedSeals := int32(2), int32(1), int32(1)
			name, id, args := assistantSearchTool, "desk-search", `{"query":"public policy"}`
			if kind == "search" {
				f = newAssistantSearchFixture(t, "api", "research", "work", "http://127.0.0.1:1")
			} else {
				if kind == "saved-link" {
					// Save the authenticated selection before admission. Displayed
					// attachment text is deliberately different from the signed page.
					savedPacket, _, savedReference = assistantDocumentFixture(t, "web", "http", nil)
					f = newAssistantWebsiteFixture(t, "api", "research", "work", "http://127.0.0.1:1", func(chat map[string]any) {
						chat["documents"] = []any{map[string]any{"id": "file-one", "name": "report.txt", "text": "saved text is never evidence", "document": savedReference, "link": assistantLinkLocation{URL: "https://example.com/report"}}}
					})
				} else {
					f = newAssistantWebsiteFixture(t, "api", "research", "work", "http://127.0.0.1:1")
				}
				name, id, args = assistantLinkTool, "desk-link", `{"url":"example.org/report#alpha-beta","find":"alpha beta"}`
				if kind == "website" {
					name, id, args = assistantWebsiteTool, "desk-website", `{"url":"https://example.org/report"}`
				}
				if kind == "partial" {
					f.resultChange = func(v map[string]any) {
						v["content"].(map[string]any)["truncated"] = true
						v["processing"].(map[string]any)["status"] = "partial"
					}
				}
				if kind == "no-text" {
					f.resultChange = func(v map[string]any) {
						pages := v["content"].(map[string]any)["pages"].([]any)
						for _, page := range pages {
							p := page.(map[string]any)
							p["text"] = ""
							p["chars"] = 0
							p["status"] = "no-text"
						}
						v["content"].(map[string]any)["chars"] = 0
					}
				}
			}
			if kind == "saved-link" {
				storage, err := f.s.openChatData()
				assistantRunMust(t, err)
				conversation, err := resolveConversationName(storage.root, f.s.conversationIdentity())
				assistantRunMust(t, err)
				assistantRunMust(t, writePrivateData(storage.root, attachmentProjectPrefix(conversation)+savedReference.ID+".json", savedPacket))
				assistantRunMust(t, storage.root.Close())
				args = `{"url":"https://example.com/report","offset":0}`
				expectedCalls, expectedAcquisitions, expectedSeals = 0, 0, 0
			}
			assistantExternalEvidenceAdmit(t, f)
			if kind == "website" {
				// Associate the controlled live fixture with its actual verified
				// companion manifest, then observe the complete catalog through
				// explicit setup. Approval review cannot create this witness.
				manifest, err := assistantSourceManifest(f.s.localGateway.bundle)
				assistantRunMust(t, err)
				f.s.localGateway.mu.Lock()
				f.s.localGateway.launchBundleDigest = digestOf(manifest)
				f.s.localGateway.mu.Unlock()
				setup := policyCall(f.s, f.token, "POST", "/api/connections/catalog", map[string]any{})
				if setup.Code != 200 {
					t.Fatal("explicit website catalog setup failed", setup.Code, setup.Body.String())
				}
			}
			approval, current := assistantExternalEvidenceApproved(t, f, id)
			var registry *assistantHostRegistry
			var err error
			switch name {
			case assistantSearchTool:
				registry, err = f.e.assistantSearchTools()
			case assistantLinkTool:
				registry, err = f.e.assistantLinkTools()
			case assistantWebsiteTool:
				registry, err = f.e.assistantWebsiteTools()
			}
			assistantRunMust(t, err)
			var actualCapability *assistantExternalCapability
			verifier := func(callCtx context.Context, evidence runcontext.ReceiptEvidence) (bool, error) {
				actualCapability, _ = callCtx.Value(assistantExternalCapabilityKey{}).(*assistantExternalCapability)
				return f.e.verifyAssistantExternalReadReceipt(callCtx, evidence)
			}
			tool, err := assistantExternalSourceTool(registry.tools[0], approval, current, verifier, nil)
			assistantRunMust(t, err)
			ctx := f.e.generationContext(t.Context(), f.e.generation)
			input := &runcontext.Input{Execution: f.e.record.Checkpoint}
			assistantRunMust(t, f.e.bindCanonicalInput(ctx, input))
			scoped, closeScope, err := f.e.assistantExternalScopedRun(ctx, &adapter.Run{Context: input, HostTools: []runcontext.HostTool{tool}})
			assistantRunMust(t, err)
			defer closeScope()
			tool = scoped.HostTools[0]
			declared, err := runcontext.DeclareSources([]runcontext.HostTool{tool}, nil)
			assistantRunMust(t, err)
			policy := runcontext.Defaults
			rows := []contract.ProvenanceSourceFigures{}
			sourceGate := runcontext.NewSourceGate(declared, &policy, "[retrieved]", "json-schema", func() *[]contract.ProvenanceSourceFigures { return &rows }, nil)
			wire := runcontext.WireName(id, name)
			sourceRaw, _ := json.Marshal(tool.Source.Record(name))
			sourceValue := runcontext.MustParseJSON(string(sourceRaw))
			sourceDigest := runcontext.RecordDigest(sourceValue)
			invoke := func(call, args string) (runcontext.ToolResult, error) {
				callCtx := context.WithValue(runcontext.WithCallID(ctx, call), assistantHostRootKey{}, true)
				callEvent := sourceGate.Decorate(contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: wire, Args: json.RawMessage(args)}})
				_, err := input.Persistence.Writer.Event(callCtx, *input.Persistence, callEvent)
				if err != nil {
					return runcontext.ToolResult{}, err
				}
				return sourceGate.Call(callCtx, wire, json.RawMessage(args), func(actual context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
					return runcontext.InvokeDurable(actual, input.Persistence, "host", sourceDigest, wire, raw, true, func(original []byte) (runcontext.ToolResult, error) { return tool.Execute(actual, original) }, func(r runcontext.ToolResult) bool { return !r.Err() }, nil)
				})
			}
			result, err := invoke("external-first", args)
			assistantRunMust(t, err)
			run := assistantExecutionRecord(t, f.e)
			var op assistantRunOperation
			var in assistantHostIntent
			var retained assistantHostResult
			for _, candidate := range run.Operations {
				if candidate.Kind == "host" && candidate.Name == name {
					op = candidate
					assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
					assistantRunMust(t, readAssistantHostReceipt(op.Result, &retained))
				}
			}
			if retained.ExternalReadEvidence == nil || result.Receipt != "" || f.acquisitions.Load() != expectedAcquisitions || f.seals.Load() != expectedSeals || bytes.Contains(retained.Result, []byte(retained.ExternalReadEvidence.ID)) {
				t.Fatal("real proof association missing or leaked")
			}
			if assistantExternalAccepted(run, op, in) {
				t.Fatal("durable commit alone established source success")
			}
			if kind == "saved-link" {
				proof := retained.ExternalReadEvidence.Proof
				if result.Err() || op.State != "completed" || proof.Kind != "saved-link" || proof.AcquisitionID != "" || proof.Packet != (attachmentReceipt{ID: savedReference.ID, SHA256: digestOf(savedPacket)}) || !reflect.DeepEqual(proof.Document, &savedReference) || proof.CustodyDigest != digestOf(assistantChatJSON(savedReference)) || !bytes.Equal(result.JSON(), retained.Result) || len(run.Links) != 0 || len(run.DomainOutputs) != 0 {
					t.Fatal("saved read lost its exact packet, selected reference or physical result")
				}
				if len(result.Content) != 1 || !strings.Contains(result.Content[0].Text(), "Selected first page.") || strings.Contains(result.Content[0].Text(), "Private second page.") || strings.Contains(result.Content[0].Text(), "saved text is never evidence") {
					t.Fatal("saved read exposed text outside its authenticated page selection")
				}
				// Canonical checkpoints sort object members. The custody comparator
				// preserves numeric lexemes and types in the complete physical result.
				dispatch := assistantCanonicalRetainedHost(run, name, in)
				if dispatch == nil || dispatch.State != "result" || dispatch.Failure != nil || !assistantCanonicalHostResultWireEqual(retained.Result, dispatch.Result) {
					t.Fatal("saved physical result is absent from the original durable dispatch")
				}
			}
			if kind == "no-text" {
				proof := retained.ExternalReadEvidence.Proof
				if !result.Err() || op.State != "refused" || len(retained.ExternalFailure) != 0 || proof.Document == nil || proof.Document.Pages == nil || len(proof.Document.Pages) != 0 {
					t.Fatal("verified empty selection lost its explicit pages array or error result")
				}
				if len(result.Content) != 1 || result.Content[0].Text() != "The page was fetched but no readable text was retained. No page text was sent." {
					t.Fatal("no-text proof exposed page text or changed the review message")
				}
				var shown struct {
					DocumentID  string `json:"documentId"`
					Digest      string `json:"digest"`
					NeedsReview bool   `json:"needsReview"`
				}
				assistantRunMust(t, json.Unmarshal(result.StructuredContent.JSON(), &shown))
				if shown.DocumentID != proof.Document.ID || shown.Digest != proof.Document.Digest || !shown.NeedsReview || len(run.DomainOutputs) != 1 || run.DomainOutputs[0].Link == nil || run.DomainOutputs[0].Link.Attachment.Document == nil || !run.DomainOutputs[0].Link.Attachment.Document.NeedsReview || run.DomainOutputs[0].Link.Attachment.Document.Pages == nil || len(run.DomainOutputs[0].Link.Attachment.Document.Pages) != 0 {
					t.Fatal("no-text error lost its exact retained review attachment")
				}
				// The fix preserves the valid empty array at the producer seam;
				// it must not make a present-null private receipt member admissible.
				changed := retained
				a := *retained.ExternalReadEvidence
				document := *a.Proof.Document
				document.Pages = nil
				a.Proof.Document = &document
				a.ID = assistantExternalEvidenceID(a)
				changed.ExternalReadEvidence = &a
				var decoded assistantHostResult
				if readAssistantHostReceipt(assistantChatJSON(changed), &decoded) == nil {
					t.Fatal("present-null proof pages bypassed the closed receipt reader")
				}
			}
			call := in.CallID
			event := sourceGate.Decorate(contract.RunEvent{ToolResult: &contract.ToolResult{Type: "tool_result", CallID: &call, Name: wire, Text: result.Content[0].Text(), IsError: result.Err(), Structured: result.StructuredContent.JSON()}})
			_, err = input.Persistence.Writer.Event(ctx, *input.Persistence, event)
			assistantRunMust(t, err)
			run = assistantExecutionRecord(t, f.e)
			if !assistantExternalAccepted(run, op, in) {
				t.Fatal("actual accepted decorated event missing")
			}
			if (kind == "partial" || kind == "no-text") && (!result.Err() || assistantCanonicalHostSucceeded(run, op, in)) {
				t.Fatal("accepted error proof established successful Plan custody")
			}
			// Reuse the actual still-live capability captured by the real callback.
			// Saved evidence never reconstructs an invocation capability.
			if actualCapability == nil {
				t.Fatal("actual verifier scope absent")
			}
			c := actualCapability
			replayCtx := context.WithValue(runcontext.WithCallID(ctx, in.CallID), assistantExternalCapabilityKey{}, c)
			replayCtx = context.WithValue(replayCtx, assistantExternalCurrentKey{}, current)
			replayCtx = context.WithValue(replayCtx, assistantHostSourceKey{}, *in.SourceBinding)
			restored, err := f.e.restoreAssistantExternalReceipt(replayCtx, op, in, retained)
			assistantRunMust(t, err)
			if restored.Receipt != retained.ExternalReadEvidence.ID || !bytes.Equal(restored.JSON(), retained.Result) || f.calls.Load() != expectedCalls {
				t.Fatal("replay re-acquired or lost real receipt")
			}
			replayIntent := in
			replayIntent.DurableDispatchID = ""
			selected, err := f.e.selectAssistantExternalReplay(replayCtx, run, name, replayIntent)
			assistantRunMust(t, err)
			if selected == nil || selected.ID != op.ID {
				t.Fatal("exact read-only retained selection missing")
			}
			for _, mutation := range []string{"call", "callback", "scope", "generation", "source", "args", "no-physical-result", "new-claim"} {
				changedRun := run
				changed := replayIntent
				sourceCopy := *changed.SourceBinding
				changed.SourceBinding = &sourceCopy
				scopeCopy := *changed.ExternalScope
				changed.ExternalScope = &scopeCopy
				switch mutation {
				case "call":
					changed.CallID = "new-call"
				case "callback":
					changed.CallbackID = "foreign-callback"
				case "scope":
					changed.ExternalScope.Execution = "sibling"
				case "generation":
					changed.ExternalScope.Generation++
				case "source":
					changed.SourceBinding.ApprovalID = strings.Repeat("f", 32)
				case "args":
					changed.Arguments = []byte(`{}`)
				case "no-physical-result":
					changedRun.Operations = nil
				case "new-claim":
					checkpoint := *run.Checkpoint
					book := *run.Checkpoint.Durable
					book.Dispatches = append([]contract.CheckpointDurableDispatch(nil), book.Dispatches...)
					fresh := book.Dispatches[0]
					fresh.ID = "new-claim"
					fresh.State = "claimed"
					fresh.Result = nil
					book.Dispatches = append(book.Dispatches, fresh)
					checkpoint.Durable = &book
					changedRun.Checkpoint = &checkpoint
					id, err := assistantCanonicalHostClaim(changedRun, name, changed)
					if err != nil || id != "new-claim" {
						t.Fatal("fresh claimed-only guard changed", err)
					}
				}
				if selected, _ := f.e.selectAssistantExternalReplay(replayCtx, changedRun, name, changed); selected != nil {
					t.Fatal("replay selected foreign/new work", mutation)
				}
			}

			evidence := runcontext.ReceiptEvidence{Source: string(in.SourceBinding.SourceRecord), Tool: name, Args: retained.ExternalReadEvidence.ArgsCanonical, Result: retained.ExternalReadEvidence.ResultCanonical, Receipt: retained.ExternalReadEvidence.ID}
			verifyCtx := runcontext.WithCallID(ctx, in.CallID)
			for _, mutation := range []string{"source", "own", "args", "result", "locator", "call", "deadline"} {
				changed := evidence
				actualCtx := verifyCtx
				switch mutation {
				case "source":
					changed.Source = strings.Replace(changed.Source, approval.Snapshot.ConfigurationDigest, strings.Repeat("f", 64), 1)
				case "own":
					changed.Tool = "foreign"
				case "args":
					changed.Args = `{}`
				case "result":
					changed.Result = `{}`
				case "locator":
					changed.Receipt = "desk-proof-v1-" + strings.Repeat("f", 64)
				case "call":
					actualCtx = runcontext.WithCallID(ctx, "sibling-call")
				case "deadline":
					cancelled, stop := context.WithCancel(verifyCtx)
					stop()
					actualCtx = cancelled
				}
				if ok, err := tool.Source.VerifyReceipt(actualCtx, changed); ok || err == nil {
					t.Fatal("mutated real source callback accepted", mutation)
				}
			}
			// Alter actual private packet bytes, preserving the locator. A
			// receipt must never exempt the original bytes/key from validation.
			storage, err := f.s.openChatData()
			assistantRunMust(t, err)
			conversation, err := resolveConversationName(storage.root, f.s.conversationIdentity())
			assistantRunMust(t, err)
			packetName := attachmentProjectPrefix(conversation) + retained.ExternalReadEvidence.Proof.Packet.ID + ".json"
			originalPacket, err := readPrivateData(storage.root, packetName, maxAttachmentObjectBytes)
			assistantRunMust(t, err)
			var object attachmentObject
			assistantRunMust(t, json.Unmarshal(originalPacket, &object))
			object.Proof.PublicKey = strings.Repeat("f", 64)
			assistantRunMust(t, writePrivateData(storage.root, packetName, assistantChatJSON(object)))
			if ok, err := tool.Source.VerifyReceipt(verifyCtx, evidence); ok || err == nil {
				t.Fatal("changed original packet/key bypassed actual callback")
			}
			assistantRunMust(t, writePrivateData(storage.root, packetName, originalPacket))
			assistantRunMust(t, storage.root.Close())
			// A fresh retained link window is a new physical call, using the same
			// original proof without acquire/seal, even when offset zero is explicit.
			if kind == "link" {
				// Same scope, same complete args/result, different exact call
				// and claim: an earlier sibling token still cannot verify.
				_, err = invoke("external-equal", args)
				assistantRunMust(t, err)
				if ok, err := tool.Source.VerifyReceipt(runcontext.WithCallID(ctx, "external-equal"), evidence); ok || err == nil {
					t.Fatal("equal-argument sibling receipt accepted")
				}
			}
			if kind == "link" {
				_, err = invoke("external-window", `{"url":"https://example.org/report","offset":0}`)
				assistantRunMust(t, err)
				if f.calls.Load() != expectedCalls {
					t.Fatal("retained window reacquired")
				}
			}
			cancelled, cancel := context.WithCancel(replayCtx)
			cancel()
			if _, err = f.e.restoreAssistantExternalReceipt(cancelled, op, in, retained); !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled replay accepted", err)
			}
			if kind == "saved-link" {
				changedReference := savedReference
				changedReference.Pages = []int{2}
				_ = assistantDocumentSavedSource(t, f.s, f.token, f.e.source.Start, changedReference)
				if _, err = f.e.restoreAssistantExternalReceipt(replayCtx, op, in, retained); err == nil || f.calls.Load() != expectedCalls || f.acquisitions.Load() != 0 || f.seals.Load() != 0 {
					t.Fatal("changed saved selection granted replay or reacquired the page")
				}
				return
			}
			revoked := policyCall(f.s, f.token, "POST", "/api/assistant-source-approvals/chat-one/revoke", assistantSourceRevokeRequest{ApprovalID: approval.ID, ApprovalRevision: approval.Revision})
			if revoked.Code != 200 {
				t.Fatal("revoke failed", revoked.Code)
			}
			if _, err = f.e.restoreAssistantExternalReceipt(replayCtx, op, in, retained); err == nil || f.calls.Load() != expectedCalls {
				t.Fatal("historical proof granted live replay authority")
			}
		})
	}
}

func TestAssistantExternalEvidenceOriginalWriterGeneration(t *testing.T) {
	_, _, _, writer, record := canonicalActivationAdmitted(t)
	e := &assistantRunExecution{record: record, writer: writer, generation: 1, lifecycle: "running"}
	ctx := e.generationContext(t.Context(), 1)
	input := &runcontext.Input{Execution: record.Checkpoint}
	assistantRunMust(t, e.bindCanonicalInput(ctx, input))
	binding := input.Persistence
	c := &assistantExternalCapability{owner: e, root: binding, binding: binding, store: e.canonicalStore, scope: assistantExternalScope{Execution: binding.Scope.Execution, Role: binding.Scope.Role, Task: binding.Scope.Task, Generation: 1}, active: true}
	ctx = context.WithValue(ctx, assistantExternalCapabilityKey{}, c)
	assistantRunMust(t, c.check(ctx))
	for _, mode := range []string{"context", "writer", "store", "scope", "epoch", "inactive", "root-binding"} {
		t.Run(mode, func(t *testing.T) {
			copy := assistantExternalCapability{owner: c.owner, root: c.root, store: c.store, scope: c.scope, active: c.active}
			copy.binding = binding
			actual := context.WithValue(ctx, assistantExternalCapabilityKey{}, &copy)
			switch mode {
			case "context":
				actual = ctx
			case "writer":
				copy.binding = &runcontext.RunBinding{Writer: &runcontext.RunWriter{}, Scope: binding.Scope, Root: binding.Root}
			case "store":
				copy.store = &assistantCanonicalRunStore{}
			case "scope":
				copy.scope.Execution = "sibling"
			case "epoch":
				copy.scope.Generation++
			case "inactive":
				copy.active = false
			case "root-binding":
				copy.binding = &runcontext.RunBinding{Writer: binding.Writer, Scope: binding.Scope, Root: binding.Root}
			}
			if copy.check(actual) == nil {
				t.Fatal("foreign/stale original writer capability accepted", mode)
			}
		})
	}
}

// Control/no-proof fixtures use the same actual canonical/source admission as
// positive proof fixtures. work may supply only fixture behavior at the real
// producer boundary; it cannot author source, scope, claim or receipt identity.
func assistantExternalEvidenceControlScope(t *testing.T, f *assistantSearchFixture, work func(context.Context, bool) (assistantHostObservation, error), verifiers ...func(context.Context, runcontext.ReceiptEvidence) (bool, error)) (func(string, json.RawMessage) (runcontext.ToolResult, error), func(string, json.RawMessage) (runcontext.ToolResult, error)) {
	t.Helper()
	assistantExternalEvidenceAdmit(t, f)
	approval, current := assistantExternalEvidenceApproved(t, f, "desk-search")
	registry, err := f.e.assistantSearchTools()
	assistantRunMust(t, err)
	declaration := registry.tools[0]
	if work != nil {
		declaration.Execute = func(ctx context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
			if _, err := readAssistantSearchQuery(raw); err != nil {
				return assistantExternalInputFailure(ctx, assistantSearchTool, err)
			}
			return f.e.invokeObservedHost(ctx, assistantSearchTool, raw, work)
		}
	}
	verify := f.e.verifyAssistantExternalReadReceipt
	if len(verifiers) > 0 {
		verify = verifiers[0]
	}
	tool, err := assistantExternalSourceTool(declaration, approval, current, verify, nil)
	assistantRunMust(t, err)
	ctx := f.e.generationContext(t.Context(), f.e.generation)
	input := &runcontext.Input{Execution: f.e.record.Checkpoint}
	assistantRunMust(t, f.e.bindCanonicalInput(ctx, input))
	scoped, closeScope, err := f.e.assistantExternalScopedRun(ctx, &adapter.Run{Context: input, HostTools: []runcontext.HostTool{tool}})
	assistantRunMust(t, err)
	t.Cleanup(closeScope)
	tool = scoped.HostTools[0]
	declared, err := runcontext.DeclareSources([]runcontext.HostTool{tool}, nil)
	assistantRunMust(t, err)
	policy := runcontext.Defaults
	rows := []contract.ProvenanceSourceFigures{}
	sourceGate := runcontext.NewSourceGate(declared, &policy, "[retrieved]", "json-schema", func() *[]contract.ProvenanceSourceFigures { return &rows }, nil)
	wire := runcontext.WireName("desk-search", assistantSearchTool)
	sourceRaw, _ := json.Marshal(tool.Source.Record(assistantSearchTool))
	sourceValue := runcontext.MustParseJSON(string(sourceRaw))
	sourceDigest := runcontext.RecordDigest(sourceValue)
	callContext := func(call string) context.Context {
		return context.WithValue(runcontext.WithCallID(ctx, call), assistantHostRootKey{}, true)
	}
	invoke := func(call string, args json.RawMessage) (runcontext.ToolResult, error) {
		callCtx := callContext(call)
		event := sourceGate.Decorate(contract.RunEvent{ToolCall: &contract.ToolCall{Type: "tool_call", CallID: &call, Name: wire, Args: args}})
		if _, err := input.Persistence.Writer.Event(callCtx, *input.Persistence, event); err != nil {
			return runcontext.ToolResult{}, err
		}
		return sourceGate.Call(callCtx, wire, args, func(actual context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
			return runcontext.InvokeDurable(actual, input.Persistence, "host", sourceDigest, wire, raw, true, func(original []byte) (runcontext.ToolResult, error) { return tool.Execute(actual, original) }, func(r runcontext.ToolResult) bool { return !r.Err() }, nil)
		})
	}
	replay := func(call string, args json.RawMessage) (runcontext.ToolResult, error) {
		return tool.Execute(callContext(call), args)
	}
	return invoke, replay
}

func TestAssistantExternalEvidenceCallbackRefusalKeepsPhysical(t *testing.T) {
	for _, mode := range []string{"refusal", "deadline", "revocation"} {
		t.Run(mode, func(t *testing.T) {
			f := newAssistantSearchFixture(t, "api", "research", "work", "http://127.0.0.1:1")
			verified := false
			callback := func(ctx context.Context, evidence runcontext.ReceiptEvidence) (bool, error) {
				ok, err := f.e.verifyAssistantExternalReadReceipt(ctx, evidence)
				if err != nil || !ok {
					return false, err
				}
				verified = true
				switch mode {
				case "refusal":
					return false, errAssistantRunScope
				case "deadline":
					cancelled, stop := context.WithCancel(ctx)
					stop()
					return f.e.verifyAssistantExternalReadReceipt(cancelled, evidence)
				case "revocation":
					run := assistantExecutionRecord(t, f.e)
					for _, op := range run.Operations {
						if op.Kind != "host" || op.Name != assistantSearchTool {
							continue
						}
						var in assistantHostIntent
						assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
						response := policyCall(f.s, f.token, "POST", "/api/assistant-source-approvals/chat-one/revoke", assistantSourceRevokeRequest{ApprovalID: in.SourceBinding.ApprovalID, ApprovalRevision: in.SourceBinding.ApprovalRevision})
						if response.Code != 200 {
							t.Fatal("fixture revocation failed", response.Code)
						}
					}
					// The per-source wrapper must perform its currentness check again even
					// after the real verifier succeeded and before returning acceptance.
					return true, nil
				}
				return false, errAssistantRunScope
			}
			invoke, _ := assistantExternalEvidenceControlScope(t, f, nil, callback)
			result, err := invoke("callback-refusal", json.RawMessage(`{"query":"public policy"}`))
			if err == nil || result.Receipt != "" || !verified || f.calls.Load() != 2 {
				t.Fatal("callback refusal was lost or source repeated", mode, err)
			}
			run := assistantExecutionRecord(t, f.e)
			found := false
			for _, op := range run.Operations {
				if op.Kind != "host" || op.Name != assistantSearchTool {
					continue
				}
				var in assistantHostIntent
				var retained assistantHostResult
				assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
				assistantRunMust(t, readAssistantHostReceipt(op.Result, &retained))
				if retained.ExternalReadEvidence == nil || op.State != "completed" || assistantExternalAccepted(run, op, in) || assistantCanonicalHostSucceeded(run, op, in) {
					t.Fatal("physical success became accepted source evidence", mode)
				}
				found = true
			}
			if !found || len(run.DomainOutputs) != 1 {
				t.Fatal("actual physical result was discarded")
			}
		})
	}
}

// Exercise the actual existing whole-ledger serialization fence with real
// proof-bearing custody present. Padding is an opaque fixture-owned runtime
// operation; it does not replace/clip any source packet, argument or result.
func TestAssistantExternalEvidenceWholeRecordBounds(t *testing.T) {
	f := newAssistantSearchFixture(t, "api", "research", "work", "http://127.0.0.1:1")
	invoke, _ := assistantExternalEvidenceControlScope(t, f, nil)
	_, err := invoke("whole-record-proof", json.RawMessage(`{"query":"public policy"}`))
	assistantRunMust(t, err)
	store := f.e.writer.lease.store
	ledger, err := store.readSnapshot(t.Context())
	assistantRunMust(t, err)
	current, err := assistantRunBound(&ledger, f.e.record.Binding, f.e.record.ID)
	assistantRunMust(t, err)
	var original assistantRunOperation
	for _, op := range current.Operations {
		if op.Kind == "host" && op.Name == assistantSearchTool {
			original = op
		}
	}
	if original.ID == "" {
		t.Fatal("actual producer custody missing")
	}
	filler := assistantRunOperation{ID: "whole-record-padding", CallID: "whole-record-padding", Kind: "runtime", Name: "fixture-padding", State: "completed", Intent: []byte{}, Result: []byte(`{}`)}
	current.Operations = append(current.Operations, filler)
	current.Revision++
	baseline, err := json.Marshal(ledger)
	assistantRunMust(t, err)
	remaining := assistantRunRecordLimit - len(baseline)
	if remaining < 12 {
		t.Fatal("positive source custody already exceeded existing whole-record bound")
	}
	filler.Intent = bytes.Repeat([]byte("a"), remaining/4*3)
	filler.Name += strings.Repeat("x", remaining%4)
	current.Operations[len(current.Operations)-1] = filler
	expected, err := json.Marshal(ledger)
	assistantRunMust(t, err)
	if len(expected) != assistantRunRecordLimit {
		t.Fatal("whole-record boundary fixture is not exact", len(expected))
	}
	assistantRunMust(t, f.e.writer.change(t.Context(), false, func(run *assistantRunRecord) (bool, error) {
		run.Operations = append(run.Operations, filler)
		return true, nil
	}))
	private, err := f.s.openChatData()
	assistantRunMust(t, err)
	defer private.root.Close()
	before, err := readPrivateData(private.root, store.name, assistantRunRecordLimit)
	assistantRunMust(t, err)
	if len(before) != assistantRunRecordLimit || !bytes.Equal(before, expected) {
		t.Fatal("complete boundary record was clipped or changed")
	}
	// One byte more in the complete serialized record must refuse atomically.
	err = f.e.writer.change(t.Context(), false, func(run *assistantRunRecord) (bool, error) {
		run.Operations[len(run.Operations)-1].Name += "x"
		return true, nil
	})
	if codeOf(err) != CodeTooLarge {
		t.Fatal("one-over complete record did not refuse", err)
	}
	after, err := readPrivateData(private.root, store.name, assistantRunRecordLimit)
	assistantRunMust(t, err)
	if !bytes.Equal(before, after) || f.calls.Load() != 2 {
		t.Fatal("capacity refusal rewrote custody or repeated source work")
	}
	saved, err := store.Read(t.Context(), f.e.record.Binding, f.e.record.ID)
	assistantRunMust(t, err)
	retained := assistantDialectOperation(saved, original.ID)
	if retained == nil || !bytes.Equal(retained.Intent, original.Intent) || !bytes.Equal(retained.Result, original.Result) {
		t.Fatal("whole-record fence clipped original source bytes")
	}
}
