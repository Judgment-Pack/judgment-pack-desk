package desk

import (
	"bytes"
	"encoding/json"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// These are the closed domain registry's existing effects. New domain tools
// must declare a classification here before acquiring canonical custody.
func assistantCanonicalHostRead(name string) (bool, bool) {
	known := assistantGraphTool(name) || assistantPolicyTool(name) || assistantDialectRecipeTool(name) || assistantTestTool(name) || assistantDialectTool(name) || name == assistantSearchTool || name == assistantLinkTool || name == assistantWebsiteTool || name == assistantPlanTool || name == assistantSavedPackTool || name == "request_dialect_research"
	return !assistantHostProposal(name) && name != assistantPlanTool && name != "request_dialect_research", known
}

func assistantCanonicalHostDispatch(run assistantRunRecord, name string, in assistantHostIntent, d contract.CheckpointDurableDispatch) bool {
	read, known := assistantCanonicalHostRead(name)
	source, tool := "host", name
	if in.SourceBinding != nil {
		record, ok := assistantHostSourceRecord(in.SourceBinding, name)
		if !ok {
			return false
		}
		source, tool = in.SourceBinding.SourceDigest, in.SourceBinding.WireName
		// Durable custody follows the reviewed owner's call effect, distinct
		// from ReadingTool's framing of every sourced answer as retrieved data.
		// Neither a retained dispatch nor caller metadata chooses this bit.
		known = false
		for _, owner := range assistantSourceOwners() {
			if owner.id == record.ID && owner.declaration.Name == name {
				read = owner.taxonomy.Effect == "read-external" && runcontext.LegacyClass(owner.taxonomy) == "read"
				known = true
				break
			}
		}
	}
	if run.Checkpoint == nil {
		return false
	}
	scopeOK := in.Root && d.Scope.Execution == run.Checkpoint.ID && d.Scope.Role == "author" && d.Scope.Task == nil
	if in.ExternalScope != nil {
		scopeOK = in.SourceBinding != nil && in.ExternalScope.Role == "author" && assistantExternalScopeMatches(*in.ExternalScope, d) && (in.Root && in.ExternalScope.Task == "" && in.ExternalScope.Execution == run.Checkpoint.ID || !in.Root && in.ExternalScope.Task != "" && in.ExternalScope.Execution == in.ExternalScope.Task)
	}
	if !known || !assistantCanonicalActive(run) || run.Checkpoint == nil || !scopeOK || d.Kind != "host" || d.Source != source || d.Tool != tool || d.Read != read || !uniqueJSON([]byte(d.Request)) || !uniqueJSON(in.Arguments) || !assistantHostArgumentsMatch([]byte(d.Request), in.Arguments) || d.RequestDigest != "sha256:"+digestOf([]byte(d.Request)) {
		return false
	}
	request, err := runcontext.ParseJSON([]byte(d.Request))
	if err != nil {
		return false
	}
	raw, _ := json.Marshal(d.Scope)
	scope, err := runcontext.ParseJSON(raw)
	if err != nil {
		return false
	}
	fingerprint := runcontext.RecordDigest(runcontext.ObjectOf("kind", runcontext.String(d.Kind), "scope", scope, "source", runcontext.String(d.Source), "tool", runcontext.String(d.Tool), "request", request))
	return d.Fingerprint == fingerprint
}

// Called before computing immutable physical intent/operation identities. Only
// the live, acknowledged claim may supply an ID; past same-argument reads are
// never selected as a new invocation's custody.
func assistantCanonicalHostClaim(run assistantRunRecord, name string, in assistantHostIntent) (string, error) {
	if run.Checkpoint == nil || run.Checkpoint.Durable == nil {
		return "", errAssistantRunScope
	}
	id := ""
	for _, d := range run.Checkpoint.Durable.Dispatches {
		if d.State != "claimed" || !assistantCanonicalHostDispatch(run, name, in, d) {
			continue
		}
		if id != "" || d.ID == "" {
			return "", errAssistantRunScope
		}
		id = d.ID
	}
	if id == "" {
		return "", errAssistantRunScope
	}
	return id, nil
}

func assistantCanonicalRetainedHost(run assistantRunRecord, name string, in assistantHostIntent) *contract.CheckpointDurableDispatch {
	if in.DurableDispatchID == "" || run.Checkpoint == nil || run.Checkpoint.Durable == nil {
		return nil
	}
	var found *contract.CheckpointDurableDispatch
	for _, d := range run.Checkpoint.Durable.Dispatches {
		if d.ID != in.DurableDispatchID {
			continue
		}
		if found != nil || !assistantCanonicalHostDispatch(run, name, in, d) {
			return nil
		}
		copy := d
		found = &copy
	}
	return found
}

func assistantCanonicalParentHostCall(run assistantRunRecord, name string, in assistantHostIntent) bool {
	if assistantCanonicalRetainedHost(run, name, in) == nil {
		return false
	}
	args, err := runcontext.ParseJSON(in.Arguments)
	if err != nil {
		return false
	}
	callName := name
	if in.SourceBinding != nil {
		callName = in.SourceBinding.WireName
	}
	count := 0
	for _, event := range run.Events {
		c := event.Event.ToolCall
		if c == nil || c.CallID == nil || *c.CallID != in.CallID {
			continue
		}
		if c.Name != callName || !assistantHostSourceCallMatches(in, c) || !uniqueJSON(c.Args) {
			return false
		}
		value, err := runcontext.ParseJSON(c.Args)
		if err != nil || runcontext.CanonicalJSON(value) != runcontext.CanonicalJSON(args) {
			return false
		}
		count++
	}
	return count == 1
}

// A physical result can precede Agent's canonical result commit. Keep that
// interval and unknown outcomes readable; neither is successful Plan custody.
func assistantCanonicalHostResultMatches(run assistantRunRecord, op assistantRunOperation, in assistantHostIntent) bool {
	d := assistantCanonicalRetainedHost(run, op.Name, in)
	if d == nil {
		return false
	}
	if d.State != "result" {
		return d.State == "claimed" || d.State == "unknown"
	}
	var receipt assistantHostResult
	if op.State != "completed" && op.State != "uncertain" && op.State != "refused" || readAssistantHostReceipt(op.Result, &receipt) != nil || !uniqueJSON(receipt.Result) || !uniqueJSON(d.Result) {
		return false
	}
	return assistantCanonicalHostResultWireEqual(receipt.Result, d.Result)
}

func assistantCanonicalHostResultWireEqual(physicalRaw, canonicalRaw []byte) bool {
	if !uniqueJSON(physicalRaw) || !uniqueJSON(canonicalRaw) {
		return false
	}
	// Canonical storage orders object members. Preserve numeric lexemes and
	// string/number/null distinctions when comparing the original ToolResult;
	// the request's exact bytes are checked separately above.
	ordered := func(raw []byte) []byte {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) != nil {
			return nil
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		return encoded
	}
	physical, canonical := ordered(physicalRaw), ordered(canonicalRaw)
	return physical != nil && canonical != nil && bytes.Equal(physical, canonical)
}

func assistantCanonicalHostSucceeded(run assistantRunRecord, op assistantRunOperation, in assistantHostIntent) bool {
	// Sourced evidence additionally requires the final accepted scoped envelope.
	if in.SourceBinding != nil {
		if !assistantExternalAccepted(run, op, in) {
			return false
		}
	}
	if !assistantCanonicalActive(run) {
		return in.DurableDispatchID == ""
	}
	d := assistantCanonicalRetainedHost(run, op.Name, in)
	if d == nil || d.State != "result" || d.Failure != nil || op.State != "completed" || !assistantCanonicalHostResultMatches(run, op, in) {
		return false
	}
	result, err := runcontext.ParseJSON(d.Result)
	if err != nil || runcontext.ReadToolResult(result).Err() {
		return false
	}
	for _, o := range run.Checkpoint.Durable.Observations {
		if o.Kind == "operation" && o.DispatchID == d.ID && o.Scope == d.Scope {
			return true
		}
	}
	return false
}
