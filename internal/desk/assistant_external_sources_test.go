package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Handwritten operation facts, using the actual complete owner declarations;
// these are association vectors, not recorded provider/proof evidence.
func assistantExternalBindingVector(name string) assistantHostSourceBinding {
	for _, row := range assistantSourceBaseCatalog() {
		if row.CompleteDeclarations[0].Name != name {
			continue
		}
		config := strings.Repeat("a", 64)
		label := "Desk research HTTP "
		if name == assistantResearchTool {
			label = "Desk research workflow "
		}
		source := runcontext.ToolSource{Kind: "custom", ID: row.AdapterID, Trust: "first-party", Version: "1", ToolListDigest: row.ToolListDigest, Identity: &contract.ToolOfferIdentity{Kind: "connection", Label: label + config}}
		raw, _ := json.Marshal(source.Record(name))
		value := runcontext.MustParseJSON(string(raw))
		return assistantHostSourceBinding{Version: 1, OwnTool: name, WireName: runcontext.WireName(row.AdapterID, name), SourceRecord: []byte(runcontext.CanonicalJSON(value)), SourceDigest: runcontext.RecordDigest(value), ApprovalID: strings.Repeat("b", 32), ApprovalRevision: 1, ConfigurationDigest: config, ProjectionVersion: "1"}
	}
	panic("unknown fixture owner")
}

func assistantExternalCustodyVector() (assistantRunRecord, assistantHostIntent, assistantRunOperation) {
	run, in, op := assistantCanonicalCustodyVector()
	binding := assistantExternalBindingVector(assistantLinkTool)
	in.SourceBinding = &binding
	op.Name = assistantLinkTool
	d := &run.Checkpoint.Durable.Dispatches[0]
	d.Tool, d.Source = binding.WireName, binding.SourceDigest
	request := runcontext.MustParseJSON(d.Request)
	scope := runcontext.MustParseJSON(`{"execution":"execution-one","role":"author"}`)
	d.Fingerprint = runcontext.RecordDigest(runcontext.ObjectOf("kind", runcontext.String("host"), "scope", scope, "source", runcontext.String(d.Source), "tool", runcontext.String(d.Tool), "request", request))
	call := run.Events[0].Event.ToolCall
	call.Name = binding.WireName
	var source contract.ToolOfferSourceRecord
	_ = json.Unmarshal(binding.SourceRecord, &source)
	call.Source = &source
	op.Intent = assistantChatJSON(in)
	return run, in, op
}

func TestAssistantExternalSourceCanonicalCustody(t *testing.T) {
	run, in, op := assistantExternalCustodyVector()
	if id, err := assistantCanonicalHostClaim(run, op.Name, in); err != nil || id != in.DurableDispatchID || !assistantParentHostCall(run, op.Name, in) {
		t.Fatal("exact sourced claim refused", id, err)
	}
	if assistantCanonicalHostSucceeded(run, op, in) {
		t.Fatal("mapping alone authorized source success")
	}
	for _, mode := range []string{"source", "wire", "own", "record-tool", "record-identity", "record-receipt", "record-null", "record-duplicate", "record-extra", "record-noncanonical", "approval", "revision", "config", "version", "digest", "source-removed", "event-source", "event-source-removed", "event-receipt", "event-name", "claim-source", "claim-wire", "claim-id", "fingerprint", "args", "args-duplicate", "request-digest", "scope-execution", "scope-role", "scope-task"} {
		t.Run(mode, func(t *testing.T) {
			r, intent, physical := assistantExternalCustodyVector()
			binding := intent.SourceBinding
			d := &r.Checkpoint.Durable.Dispatches[0]
			call := r.Events[0].Event.ToolCall
			switch mode {
			case "source":
				binding.SourceRecord = bytes.Replace(binding.SourceRecord, []byte(`"desk-link"`), []byte(`"desk-search"`), 1)
			case "wire":
				binding.WireName = "arbitrary__read_link"
			case "own":
				binding.OwnTool = assistantSearchTool
			case "record-tool":
				binding.SourceRecord = bytes.Replace(binding.SourceRecord, []byte(`"tool":"read_link"`), []byte(`"tool":"search_sources"`), 1)
			case "record-identity":
				binding.SourceRecord = bytes.Replace(binding.SourceRecord, []byte(`"kind":"connection"`), []byte(`"kind":"person"`), 1)
			case "record-receipt":
				binding.SourceRecord = append([]byte(`{"receipt":"forged",`), binding.SourceRecord[1:]...)
			case "record-null":
				binding.SourceRecord = bytes.Replace(binding.SourceRecord, []byte(`"tool":"read_link"`), []byte(`"tool":null`), 1)
			case "record-duplicate":
				binding.SourceRecord = append([]byte(`{"tool":"read_link",`), binding.SourceRecord[1:]...)
			case "record-extra":
				binding.SourceRecord = append([]byte(`{"extra":true,`), binding.SourceRecord[1:]...)
			case "record-noncanonical":
				binding.SourceRecord = append([]byte(" "), binding.SourceRecord...)
			case "approval":
				binding.ApprovalID = "foreign"
			case "revision":
				binding.ApprovalRevision = 2
			case "config":
				binding.ConfigurationDigest = strings.Repeat("c", 64)
			case "version":
				binding.ProjectionVersion = "2"
			case "digest":
				binding.SourceDigest = "sha256:" + strings.Repeat("c", 64)
			case "source-removed":
				intent.SourceBinding = nil
			case "event-source":
				call.Source.ID = "desk-search"
			case "event-source-removed":
				call.Source = nil
			case "event-receipt":
				value := "forged"
				call.Source.Receipt = &value
			case "event-name":
				call.Name = assistantLinkTool
			case "claim-source":
				d.Source = "host"
			case "claim-wire":
				d.Tool = assistantLinkTool
			case "claim-id":
				intent.DurableDispatchID = "foreign"
			case "fingerprint":
				d.Fingerprint = "sha256:" + strings.Repeat("c", 64)
			case "args":
				intent.Arguments = bytes.Replace(intent.Arguments, []byte("1.20"), []byte("1.2"), 1)
			case "args-duplicate":
				intent.Arguments = []byte(`{"a":1,"a":1}`)
			case "request-digest":
				d.RequestDigest = "sha256:" + strings.Repeat("c", 64)
			case "scope-execution":
				d.Scope.Execution = "foreign"
			case "scope-role":
				d.Scope.Role = "critic"
			case "scope-task":
				task := "child"
				d.Scope.Task = &task
			}
			if assistantParentHostCall(r, physical.Name, intent) {
				t.Fatal("mutated sourced custody accepted", mode)
			}
		})
	}
}

func TestAssistantExternalSourceClosedBindingAndOldHistory(t *testing.T) {
	_, in, _ := assistantExternalCustodyVector()
	raw := assistantChatJSON(in)
	for _, mode := range []string{"binding-null", "binding-case", "binding-duplicate", "member-unknown", "member-null", "member-case", "member-duplicate", "member-missing", "member-type"} {
		t.Run(mode, func(t *testing.T) {
			bad := bytes.Clone(raw)
			switch mode {
			case "binding-null":
				bad = bytes.Replace(bad, assistantChatJSON(in.SourceBinding), []byte("null"), 1)
			case "binding-case":
				bad = bytes.Replace(bad, []byte(`"sourceBinding"`), []byte(`"SourceBinding"`), 1)
			case "binding-duplicate":
				bad = append([]byte(`{"sourceBinding":`+string(assistantChatJSON(in.SourceBinding))+`,`), bad[1:]...)
			case "member-unknown":
				bad = bytes.Replace(bad, []byte(`"version":1`), []byte(`"version":1,"extra":true`), 1)
			case "member-null":
				bad = bytes.Replace(bad, []byte(`"ownTool":"read_link"`), []byte(`"ownTool":null`), 1)
			case "member-case":
				bad = bytes.Replace(bad, []byte(`"ownTool"`), []byte(`"OwnTool"`), 1)
			case "member-duplicate":
				bad = bytes.Replace(bad, []byte(`"ownTool":"read_link"`), []byte(`"ownTool":"read_link","ownTool":"read_link"`), 1)
			case "member-missing":
				bad = bytes.Replace(bad, []byte(`"callbackAlias":"",`), nil, 1)
				bad = bytes.Replace(bad, []byte(`,"callbackAlias":""`), nil, 1)
			case "member-type":
				bad = bytes.Replace(bad, []byte(`"approvalRevision":1`), []byte(`"approvalRevision":"1"`), 1)
			}
			var decoded assistantHostIntent
			if bytes.Equal(raw, bad) || readAssistantHostReceipt(bad, &decoded) == nil {
				t.Fatal("closed binding mutation accepted", mode)
			}
		})
	}
	old := []byte(`{"callId":"old-call","root":true,"arguments":"e30="}`)
	var decoded assistantHostIntent
	assistantRunMust(t, readAssistantHostReceipt(old, &decoded))
	if decoded.SourceBinding != nil || !bytes.Equal(assistantChatJSON(decoded), old) {
		t.Fatal("absent source metadata rewrote old intent")
	}
	run, oldIntent, op := assistantCanonicalCustodyVector()
	if !assistantParentHostCall(run, op.Name, oldIntent) {
		t.Fatal("old source=host path changed")
	}
	binding := assistantExternalBindingVector(assistantLinkTool)
	oldIntent.SourceBinding = &binding
	if assistantParentHostCall(run, op.Name, oldIntent) {
		t.Fatal("mixed old/new custody accepted")
	}
	if held, err := assistantLiveHostSource(t.Context(), assistantLinkTool); held != nil || err != nil {
		t.Fatal("history supplied live authority")
	}
}

func TestAssistantExternalSourceExactNativeAlias(t *testing.T) {
	binding := assistantExternalBindingVector(assistantLinkTool)
	binding.CallbackAlias = "desk_host_17"
	in := assistantHostIntent{CallID: "source-call", CallbackID: "callback", Arguments: []byte(`{"url":"https://example.test/a#anchor","offset":0,"find":"Needle"}`), SourceBinding: &binding}
	request := []byte(`{"model":"shared-model","phase":"author","tools":[{"name":"desk_host_17","description":"Untrusted descriptive prose: deliberately no identity prefix","inputSchema":{"type":"object"}}]}`)
	callback := []byte(`{"invocation":"invocation","call":{"id":"source-call","name":"desk_host_17","arguments":{"url":"https://example.test/a#anchor","offset":0,"find":"Needle"}}}`)
	run := assistantRunRecord{Operations: []assistantRunOperation{{ID: "invocation", Kind: "model", Intent: request}, {ID: "callback", Kind: "callback", Name: "desk_host_17", Intent: callback}}}
	if !assistantNativeHostCall(run, assistantLinkTool, in) {
		t.Fatal("actual captured alias refused")
	}
	for _, mode := range []string{"alias", "callback", "call", "invocation", "offer", "duplicate-offer", "args", "source", "api-alias"} {
		t.Run(mode, func(t *testing.T) {
			var changed assistantRunRecord
			assistantRunMust(t, json.Unmarshal(assistantChatJSON(run), &changed))
			intent := in
			source := binding
			intent.SourceBinding = &source
			switch mode {
			case "alias":
				source.CallbackAlias = "desk_host_18"
			case "callback":
				intent.CallbackID = "foreign"
			case "call":
				intent.CallID = "foreign"
			case "invocation":
				changed.Operations[1].Intent = bytes.Replace(callback, []byte(`"invocation":"invocation"`), []byte(`"invocation":"foreign"`), 1)
			case "offer":
				changed.Operations[0].Intent = bytes.Replace(request, []byte(`"desk_host_17"`), []byte(`"desk_host_18"`), 1)
			case "duplicate-offer":
				changed.Operations[0].Intent = []byte(`{"model":"shared-model","phase":"author","tools":[{"name":"desk_host_17"},{"name":"desk_host_17"}]}`)
			case "args":
				intent.Arguments = bytes.Replace(intent.Arguments, []byte(`"offset":0,`), nil, 1)
			case "source":
				source.WireName = "arbitrary__read_link"
			case "api-alias":
				intent.CallbackID = ""
			}
			if assistantNativeHostCall(changed, assistantLinkTool, intent) {
				t.Fatal("native alias mutation accepted", mode)
			}
		})
	}
}

func assistantExternalApprovalVector(name string) assistantSourceApproval {
	project := strings.Repeat("a", 64)
	for _, row := range assistantSourceBaseCatalog() {
		if row.CompleteDeclarations[0].Name != name {
			continue
		}
		row.Configuration = assistantSourceConfiguration{Target: "https://example.test", DeskDigest: strings.Repeat("b", 64), OwnerDigest: strings.Repeat("c", 64), TargetDigest: strings.Repeat("d", 64), AuthorityDigest: strings.Repeat("e", 64), PublicKeyDigest: strings.Repeat("f", 64)}
		label := "Desk research HTTP "
		if name == assistantResearchTool {
			row.Configuration = assistantSourceConfiguration{Target: "Desk private research workflow", DeskDigest: strings.Repeat("b", 64), OwnerDigest: strings.Repeat("c", 64)}
			label = "Desk research workflow "
		}
		if name == assistantSearchTool {
			row.Configuration.Connection = "search"
			row.Configuration.Revision = strings.Repeat("5", 64)
			row.Configuration.Provider = "provider"
			row.Configuration.PreferenceDigest = "absent"
		}
		row.ConfigurationDigest = assistantSourceConfigurationDigest(row, project)
		row.Identity = &contract.ToolOfferIdentity{Kind: "connection", Label: label + row.ConfigurationDigest}
		row.IdentityDigest = runcontext.IdentityDigest(*row.Identity)
		row.Status = "review-required"
		return assistantSourceApproval{Version: 1, ID: strings.Repeat("1", 32), Revision: 1, Scope: assistantSourceScope{Project: project, ChatID: "chat-one", SessionScope: strings.Repeat("2", 64), Principal: strings.Repeat("3", 64), PolicyDigest: strings.Repeat("4", 64)}, Snapshot: row}
	}
	panic("unknown owner")
}

func TestAssistantExternalSourcePrivateRegistration(t *testing.T) {
	for _, owner := range assistantSourceOwners() {
		t.Run(owner.id, func(t *testing.T) {
			tool := owner.declaration
			calls, checks := 0, 0
			var observed *assistantHostSourceBinding
			tool.Execute = func(ctx context.Context, args json.RawMessage) (runcontext.ToolResult, error) {
				calls++
				var err error
				observed, err = assistantLiveHostSource(ctx, tool.Name)
				return runcontext.ToolResult{}, err
			}
			currentErr := error(nil)
			current := func(context.Context) error { checks++; return currentErr }
			verify := func(context.Context, runcontext.ReceiptEvidence) (bool, error) {
				return false, errors.New("E3 verifier intentionally absent")
			}
			policy := func(runcontext.ApprovalRequest) string { return "refuse" }
			approval := assistantExternalApprovalVector(tool.Name)
			registered, err := assistantExternalSourceTool(tool, approval, current, verify, policy)
			assistantRunMust(t, err)
			if calls != 0 || checks != 0 {
				t.Fatal("registration executed source work")
			}
			registry, err := newAssistantHostRegistry([]runcontext.HostTool{registered})
			assistantRunMust(t, err)
			if len(registry.tools) != 1 || registered.Effect != "" || registered.SourceTool != "" || assistantHostDeclaredRead(registered) != (tool.Name != assistantResearchTool) {
				t.Fatal("owner classification changed")
			}
			_, err = registered.Execute(t.Context(), json.RawMessage(`{}`))
			assistantRunMust(t, err)
			if calls != 1 || checks != 1 || observed == nil || observed.OwnTool != tool.Name || observed.WireName != runcontext.WireName(owner.id, tool.Name) {
				t.Fatal("private own/wire registration lost")
			}
			ctx := context.WithValue(t.Context(), assistantHostCallbackKey{}, "callback")
			if _, err = registered.Execute(ctx, json.RawMessage(`{}`)); err == nil || calls != 1 {
				t.Fatal("unobserved native alias executed")
			}
			ctx = context.WithValue(ctx, assistantHostCallbackAliasKey{}, "desk_host_17")
			_, err = registered.Execute(ctx, json.RawMessage(`{}`))
			assistantRunMust(t, err)
			if calls != 2 || observed.CallbackAlias != "desk_host_17" {
				t.Fatal("actual native alias not captured")
			}
			currentErr = errAssistantRunScope
			if _, err = registered.Source.CurrentIdentity(t.Context()); err == nil {
				t.Fatal("revoked identity accepted")
			}
			if _, err = registered.Execute(t.Context(), json.RawMessage(`{}`)); err == nil || calls != 2 {
				t.Fatal("history approval authorized new execution")
			}
			if _, err = assistantExternalSourceTool(tool, approval, nil, verify, policy); err == nil {
				t.Fatal("retained approval substituted for current authority")
			}
			approval.Historical = true
			if _, err = assistantExternalSourceTool(tool, approval, current, verify, policy); err == nil {
				t.Fatal("imported historical approval activated")
			}
		})
	}
}

func TestAssistantExternalSourceDeclarationRefusals(t *testing.T) {
	for _, mode := range []string{"legacy-effect", "source-tool", "wrong-owner", "incomplete-list", "taxonomy", "effect-class", "delegation", "missing-verifier", "mcp", "gateway-receipt", "missing-current", "snapshot", "context-policy"} {
		t.Run(mode, func(t *testing.T) {
			tool := assistantLinkDeclaration()
			tool.Execute = func(context.Context, json.RawMessage) (runcontext.ToolResult, error) {
				return runcontext.ToolResult{}, nil
			}
			registered, err := assistantExternalSourceTool(tool, assistantExternalApprovalVector(tool.Name), func(context.Context) error { return nil }, func(context.Context, runcontext.ReceiptEvidence) (bool, error) { return false, nil }, nil)
			assistantRunMust(t, err)
			switch mode {
			case "legacy-effect":
				registered.Effect = "read"
			case "source-tool":
				registered.SourceTool = registered.Name
			case "wrong-owner":
				registered.Source.ID = "other"
			case "incomplete-list":
				registered.Source.ToolListDigest = "sha256:" + strings.Repeat("0", 64)
			case "taxonomy":
				registered.Taxonomy.Effect = "act"
			case "effect-class":
				registered.EffectClass = "act"
			case "delegation":
				registered.Delegation = "research"
			case "missing-verifier":
				registered.Source.VerifyReceipt = nil
			case "mcp":
				registered.Source.Kind = "mcp"
			case "gateway-receipt":
				registered.Source.GatewayReceipt = &runcontext.GatewayReceiptTrust{}
			case "missing-current":
				registered.Source.CurrentIdentity = nil
			case "snapshot":
				registered.Source.DescriptorSnapshot = []byte(`{}`)
			case "context-policy":
				registered.ContextPolicy = &runcontext.ToolContextPolicy{}
			}
			if _, err := newAssistantHostRegistry([]runcontext.HostTool{registered}); err == nil {
				t.Fatal("altered sourced declaration accepted", mode)
			}
		})
	}
}

// Recompute dependent public facts so this proves closed nested-record
// validation rather than merely rejecting an old checksum or fingerprint.
func TestAssistantExternalSourceClosedNestedIdentity(t *testing.T) {
	for _, mutation := range []string{
		`{"kind":"connection","label":LABEL,"unexpected":true}`,
		`{"Kind":"connection","label":LABEL}`,
		`{"kind":"connection","Label":LABEL}`,
		`{"kind":"connection","label":LABEL,"label":LABEL}`,
		`{"kind":"connection","label":LABEL,"extra":{"nested":"value"}}`,
	} {
		t.Run(mutation, func(t *testing.T) {
			run, in, op := assistantExternalCustodyVector()
			var source contract.ToolOfferSourceRecord
			assistantRunMust(t, json.Unmarshal(in.SourceBinding.SourceRecord, &source))
			original := bytes.Clone(in.SourceBinding.SourceRecord)
			identityRaw, _ := json.Marshal(source.Identity)
			labelRaw, _ := json.Marshal(source.Identity.Label)
			replacement := []byte(strings.ReplaceAll(mutation, "LABEL", string(labelRaw)))
			in.SourceBinding.SourceRecord = bytes.Replace(in.SourceBinding.SourceRecord, identityRaw, replacement, 1)
			if bytes.Equal(in.SourceBinding.SourceRecord, original) {
				t.Fatal("fixture failed to mutate identity")
			}
			// ParseJSON normalizes the non-duplicate mutation for canonical source
			// bytes, but the duplicate case remains raw to exercise its own guard.
			if !strings.Contains(mutation, `"label":LABEL,"label":LABEL`) {
				value := runcontext.MustParseJSON(string(in.SourceBinding.SourceRecord))
				in.SourceBinding.SourceRecord = []byte(runcontext.CanonicalJSON(value))
			}
			value := runcontext.MustParseJSON(string(in.SourceBinding.SourceRecord))
			in.SourceBinding.SourceDigest = runcontext.RecordDigest(value)
			d := &run.Checkpoint.Durable.Dispatches[0]
			d.Source = in.SourceBinding.SourceDigest
			scopeRaw, _ := json.Marshal(d.Scope)
			d.Fingerprint = runcontext.RecordDigest(runcontext.ObjectOf("kind", runcontext.String(d.Kind), "scope", runcontext.MustParseJSON(string(scopeRaw)), "source", runcontext.String(d.Source), "tool", runcontext.String(d.Tool), "request", runcontext.MustParseJSON(d.Request)))
			if _, ok := assistantHostSourceRecord(in.SourceBinding, op.Name); ok {
				t.Fatal("nested identity mutation accepted with recomputed source digest and claim")
			}
			if assistantCanonicalHostDispatch(run, op.Name, in, *d) {
				t.Fatal("mutated nested identity borrowed canonical dispatch")
			}
		})
	}
}
