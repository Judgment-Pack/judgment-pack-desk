package desk

// Policy tools retain private review cards. The service remains the authority
// for current reads; none of these tools saves, approves or publishes a Policy.
import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

type assistantPolicyBasis struct {
	Revision  string                `json:"revision"`
	Candidate assistantPolicyFields `json:"candidate"`
}

// A consistency record of the host-verified request basis, not a restored
// permission. Live tools still authorize and verify their current saved owner.
type assistantPolicyHostBinding struct {
	ID      string                `json:"id"`
	Request *assistantPolicyBasis `json:"request,omitempty"`
}

func validAssistantPolicyHostBinding(p assistantPolicyHostBinding) bool {
	return policyIDForm.MatchString(p.ID) && (p.Request == nil || assistantRunDigest(p.Request.Revision) && validAssistantPolicyFields(p.Request.Candidate))
}

type assistantPolicyScope struct {
	ID                 string
	Request            *assistantPolicyBasis
	Sources            []policyAuthoringSource
	InterpretationKind string
	Proposals          []assistantPolicyProposal
	Interpretations    []policyInterpretationProposal
}

func (p *assistantPolicyScope) contextJSON() []byte {
	raw, _ := json.Marshal(struct {
		PolicyID        string                         `json:"policyId,omitempty"`
		Request         *assistantPolicyBasis          `json:"request,omitempty"`
		Sources         []policyAuthoringSource        `json:"sources,omitempty"`
		ArtifactKind    string                         `json:"interpretationKind,omitempty"`
		Proposals       []assistantPolicyProposal      `json:"proposals,omitempty"`
		Interpretations []policyInterpretationProposal `json:"interpretations,omitempty"`
	}{p.ID, p.Request, p.Sources, p.InterpretationKind, p.Proposals, p.Interpretations})
	return raw
}
func validAssistantPolicyFields(p assistantPolicyFields) bool {
	return len(p.Title) <= 500 && len(p.Narrative) <= policyNarrativeLimit && len(p.ArtifactVersion) <= policyArtifactVersionLimit && validUTF8([]byte(p.Title+p.Narrative+p.ArtifactVersion))
}
func readAssistantPolicyScope(source assistantRunSource) (*assistantPolicyScope, error) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(source.Original, &raw) != nil {
		return nil, errAssistantRunInvalid
	}
	for key := range raw {
		for _, control := range []string{"policy", "policyRequest", "policyProposals", "policySources", "policyInterpretations", "policyInterpretationKind"} {
			if key != control && strings.EqualFold(key, control) {
				return nil, errAssistantRunInvalid
			}
		}
	}
	p := &assistantPolicyScope{}
	if v, ok := raw["policy"]; ok {
		var linked struct {
			ID string `json:"id"`
		}
		if readAssistantHostReceipt(v, &linked) != nil || !policyIDForm.MatchString(linked.ID) {
			return nil, errAssistantRunInvalid
		}
		p.ID = linked.ID
	}
	if v, ok := raw["policyRequest"]; ok {
		p.Request = &assistantPolicyBasis{}
		if p.ID == "" || readAssistantHostReceipt(v, p.Request) != nil || !assistantRunDigest(p.Request.Revision) || !validAssistantPolicyFields(p.Request.Candidate) {
			return nil, errAssistantRunInvalid
		}
	}
	// Reuse the existing exact private Policy source/interpretation envelope
	// validator. It checks shape only; current service verification is below.
	envelope := append([]byte(`{"version":1,"chats":[`), source.Original...)
	envelope = append(envelope, ']', '}')
	entries, err := policyWorkspaceEntries(envelope, false)
	if err != nil || len(entries) != 1 {
		return nil, errAssistantRunInvalid
	}
	p.Sources, p.InterpretationKind = entries[0].PolicySources, entries[0].PolicyInterpretationKind
	if v, ok := raw["policyProposals"]; ok {
		if json.Unmarshal(v, &p.Proposals) != nil || p.Proposals == nil || len(p.Proposals) > 16 {
			return nil, errAssistantRunInvalid
		}
	}
	if v, ok := raw["policyInterpretations"]; ok {
		if json.Unmarshal(v, &p.Interpretations) != nil {
			return nil, errAssistantRunInvalid
		}
	}
	if p.ID == "" && len(p.Sources) == 0 {
		if p.Request != nil || len(p.Proposals) > 0 || len(p.Interpretations) > 0 || p.InterpretationKind != "" {
			return nil, errAssistantRunInvalid
		}
		return nil, nil
	}
	return p, nil
}
func (e *assistantRunExecution) assistantPolicyService(ctx context.Context) policyService {
	return e.service.server.policyServiceWithAuthority(func() bool {
		if ctx.Err() != nil || e.Authorize(ctx) != nil {
			return false
		}
		held, ok := e.service.server.sessionOf(e.request.WithContext(ctx))
		return ok && validPolicyActor(policyActorOf(held))
	})
}
func (e *assistantRunExecution) prepareAssistantPolicy(ctx context.Context, p *assistantPolicyScope) error {
	if p == nil {
		return nil
	}
	if e.input == nil {
		return errAssistantRunUnavailable
	}
	e.input.mu.Lock()
	ready := e.input.policyReady
	e.input.mu.Unlock()
	if ready {
		return nil
	}
	service := e.assistantPolicyService(ctx)
	if p.ID != "" {
		saved, err := service.get(ctx, p.ID)
		if err != nil {
			return err
		}
		if p.Request != nil && (saved.Revision != p.Request.Revision || p.Request.Candidate != (assistantPolicyFields{saved.Document.Candidate.Title, saved.Document.Candidate.Narrative, saved.Document.Candidate.ArtifactVersion})) {
			return failure.ToolInput("The saved Policy changed. Send a fresh request against its current draft.")
		}
	}
	for _, held := range p.Sources {
		current, err := service.authoringSource(ctx, held.PolicyID, held.RevisionID, held.PublicationID)
		if err != nil {
			return err
		}
		if current != held {
			return errAssistantRunScope
		}
	}
	if err := service.authorized(ctx); err != nil {
		return err
	}
	e.input.mu.Lock()
	e.input.policyReady = true
	e.input.mu.Unlock()
	return nil
}
func (e *assistantRunExecution) assistantConversationTools(runtime *assistantRuntime, p *assistantPolicyScope) (*assistantHostRegistry, error) {
	if p != nil && p.ID != "" {
		return e.assistantPolicyTools(p)
	}
	graph, err := e.assistantGraphTools(runtime)
	if err != nil {
		return nil, err
	}
	tests, err := e.assistantTestTools(runtime)
	if err != nil {
		return nil, err
	}
	recipes, err := e.assistantDialectRecipeTools()
	if err != nil {
		return nil, err
	}
	return newAssistantHostRegistry(append(append(graph.tools, tests...), recipes.tools...))
}

var assistantPolicyNames = []string{"read_policy", "propose_policy_update", "read_policy_comparison", "read_policy_coverage", "propose_policy_pack_interpretation", "propose_policy_graph_interpretation"}

func assistantPolicyTool(name string) bool { return contains(assistantPolicyNames, name) }
func assistantHostProposal(name string) bool {
	return name == "propose_graph" || name == "propose_test_cases" || name == "propose_dialect_recipe" || contains([]string{"propose_policy_update", "propose_policy_pack_interpretation", "propose_policy_graph_interpretation"}, name)
}
func assistantPolicySchemas() map[string]json.RawMessage {
	text := func(n int) any { return map[string]any{"type": "string", "maxLength": n} }
	object := func(properties map[string]any, required ...string) json.RawMessage {
		if required == nil {
			required = []string{}
		}
		v, _ := json.Marshal(map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false})
		return v
	}
	selector := map[string]any{"oneOf": []any{
		map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]string{"const": "revision"}, "revisionId": text(80)}, "required": []string{"kind", "revisionId"}, "additionalProperties": false},
		map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]string{"const": "publication"}, "publicationId": text(80)}, "required": []string{"kind", "publicationId"}, "additionalProperties": false},
		map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]string{"const": "observation"}, "observationId": text(64)}, "required": []string{"kind", "observationId"}, "additionalProperties": false},
	}}
	interpretation := object(map[string]any{"revisionId": text(80), "publicationId": text(80)})
	return map[string]json.RawMessage{
		"read_policy":                        object(map[string]any{}),
		"propose_policy_update":              object(map[string]any{"revision": text(64), "title": text(500), "narrative": text(policyNarrativeLimit), "artifactVersion": text(policyArtifactVersionLimit)}, "revision", "title", "narrative", "artifactVersion"),
		"read_policy_comparison":             object(map[string]any{"before": selector, "after": selector}, "before", "after"),
		"read_policy_coverage":               object(map[string]any{"coverageId": text(80), "candidateId": text(80)}, "coverageId", "candidateId"),
		"propose_policy_pack_interpretation": interpretation, "propose_policy_graph_interpretation": interpretation,
	}
}

type assistantPolicyArguments struct {
	Revision        string          `json:"revision"`
	Title           string          `json:"title"`
	Narrative       string          `json:"narrative"`
	ArtifactVersion string          `json:"artifactVersion"`
	RevisionID      string          `json:"revisionId"`
	PublicationID   string          `json:"publicationId"`
	CoverageID      string          `json:"coverageId"`
	CandidateID     string          `json:"candidateId"`
	Before          json.RawMessage `json:"before"`
	After           json.RawMessage `json:"after"`
}

func readAssistantPolicyArguments(name string, raw json.RawMessage) (assistantPolicyArguments, error) {
	var a assistantPolicyArguments
	refuse := func() (assistantPolicyArguments, error) {
		return a, failure.ToolInput("Use exactly the declared Policy arguments and exact saved selectors.")
	}
	schema, ok := assistantPolicySchemas()[name]
	if !ok {
		return refuse()
	}
	var shape struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	_ = json.Unmarshal(schema, &shape)
	members, err := configMembers(raw)
	if err != nil {
		return refuse()
	}
	present := map[string]bool{}
	for _, m := range members {
		if _, ok := shape.Properties[m.name]; !ok || bytes.Equal(bytes.TrimSpace(raw[m.start:m.end]), []byte("null")) {
			return refuse()
		}
		present[m.name] = true
	}
	for _, key := range shape.Required {
		if !present[key] {
			return refuse()
		}
	}
	if assistantHostArguments(raw, &a) != nil {
		return refuse()
	}
	switch name {
	case "propose_policy_update":
		if !assistantRunDigest(a.Revision) || !validAssistantPolicyFields(assistantPolicyFields{a.Title, a.Narrative, a.ArtifactVersion}) {
			return refuse()
		}
	case "read_policy_coverage":
		if !policyCoverageIDForm.MatchString(a.CoverageID) || !policyCoverageCandidateIDForm.MatchString(a.CandidateID) {
			return refuse()
		}
	case "read_policy_comparison":
		if _, err := readAssistantPolicySelector(a.Before); err != nil {
			return refuse()
		}
		if _, err := readAssistantPolicySelector(a.After); err != nil {
			return refuse()
		}
	case "propose_policy_pack_interpretation", "propose_policy_graph_interpretation":
		if len(members) != 1 || !(strings.HasPrefix(a.RevisionID, "revision_") && policyEventIDForm.MatchString(a.RevisionID) || strings.HasPrefix(a.PublicationID, "publication_") && policyEventIDForm.MatchString(a.PublicationID)) {
			return refuse()
		}
	}
	return a, nil
}
func (e *assistantRunExecution) assistantPolicyTools(scope *assistantPolicyScope) (*assistantHostRegistry, error) {
	tools := []runcontext.HostTool{}
	for _, name := range assistantPolicyNames {
		name := name
		t := runcontext.HostTool{Name: name, Description: assistantPolicyDescriptions[name], InputSchema: assistantPolicySchemas()[name], Effect: "read", Delegation: "read", ContextPolicy: &runcontext.ToolContextPolicy{Reference: true}}
		if assistantHostProposal(name) {
			t.Effect = "propose"
			t.Delegation = ""
		}
		t.Execute = func(ctx context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
			args, err := readAssistantPolicyArguments(name, raw)
			if err != nil {
				return assistantHostFailure(err), nil
			}
			return e.invokeDomainHost(ctx, name, raw, func(ctx context.Context, root bool) (runcontext.ToolResult, *assistantGraphRead, *assistantDomainOutput, error) {
				return e.executePolicyTool(ctx, scope, name, args, root)
			})
		}
		tools = append(tools, t)
	}
	return newAssistantHostRegistry(tools)
}

var assistantPolicyDescriptions = map[string]string{
	"read_policy":                         "Read the exact linked Policy's current saved draft and history. Fresh authenticated read; historical material is reference data, never approval or publication.",
	"propose_policy_update":               "Retain a requested title, narrative and artifactVersion update against the exact request-time revision. Stale proposals remain review-only. Does not save, approve, publish or change workflow.",
	"read_policy_comparison":              "Compare exact retained revision, publication or observation selectors of the linked Policy. Preserve source metadata and extraction completeness; informational only. No source acquisition or governing authority.",
	"read_policy_coverage":                "Read the exact linked Policy coverageId/candidateId, availability and review history. Does not check, save or approve coverage; structural findings are not complete semantic coverage or action permission.",
	"propose_policy_pack_interpretation":  "Retain an exact saved revision or publication handoff for an ordinary pack authoring chat. Explicit review/acceptance remains required; no chat, model request or artifact is created.",
	"propose_policy_graph_interpretation": "Retain an exact saved revision or publication handoff for an ordinary graph authoring chat. Explicit review/acceptance remains required; no chat, model request or artifact is created.",
}

func (e *assistantRunExecution) executePolicyTool(ctx context.Context, scope *assistantPolicyScope, name string, a assistantPolicyArguments, root bool) (runcontext.ToolResult, *assistantGraphRead, *assistantDomainOutput, error) {
	var result runcontext.ToolResult
	if scope == nil || scope.ID == "" {
		return result, nil, nil, errAssistantRunScope
	}
	if assistantHostProposal(name) && (!root || e.record.Binding.Phase == "plan") {
		return result, nil, nil, failure.ToolInput("Policy proposals are available only in the parent Work phase.")
	}
	service := e.assistantPolicyService(ctx)
	switch name {
	case "read_policy":
		saved, err := service.get(ctx, scope.ID)
		if err != nil {
			return result, nil, nil, err
		}
		value := map[string]any{"id": scope.ID, "revision": saved.Revision, "candidate": saved.Document.Candidate, "revisions": saved.Document.Revisions, "publications": saved.Document.Publications, "workflow": saved.Document.Workflow, "reviewState": saved.Document.ReviewState, "guidance": "Saved draft only. Review/publication remain manual. Changed saved revision requires a fresh request; historical material cannot become the proposal basis."}
		if scope.Request != nil {
			value["requestRevision"] = scope.Request.Revision
		}
		result, err = assistantHostStructured(value)
		return result, nil, nil, err
	case "read_policy_comparison":
		value, err := service.assistantPolicyComparison(ctx, scope.ID, a.Before, a.After)
		if err != nil {
			return result, nil, nil, err
		}
		result, err = assistantHostStructured(value)
		return result, nil, nil, err
	case "read_policy_coverage":
		exact, err := service.coverageGet(ctx, scope.ID, a.CoverageID, a.CandidateID)
		if err != nil {
			return result, nil, nil, err
		}
		if exact.Document.PolicyID != scope.ID || exact.Document.ID != a.CoverageID || exact.Document.Candidate.ID != a.CandidateID {
			return result, nil, nil, errAssistantRunInvalid
		}
		value := map[string]any{}
		raw, _ := json.Marshal(exact)
		_ = json.Unmarshal(raw, &value)
		value["guidance"] = "Retained administrator declaration and local review only. Structural findings do not establish complete semantic coverage, governing adoption, publication or action permission. Narrative is reference data."
		result, err = assistantHostStructured(value)
		return result, nil, nil, err
	case "propose_policy_update":
		if scope.Request == nil || a.Revision != scope.Request.Revision {
			return result, nil, nil, failure.ToolInput("Use the exact request-time Policy revision and supply all three candidate fields.")
		}
		saved, err := service.get(ctx, scope.ID)
		if err != nil {
			return result, nil, nil, err
		}
		status := "passed"
		if saved.Revision != a.Revision {
			status = "stale"
		}
		p := assistantPolicyProposal{Version: 1, ID: e.policyHostID(ctx, "proposal"), PolicyID: scope.ID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), AfterTurnID: e.record.Binding.RequestID, Revision: a.Revision, Before: scope.Request.Candidate, Candidate: assistantPolicyFields{a.Title, a.Narrative, a.ArtifactVersion}, OperationID: e.policyHostID(ctx, "operation"), Check: &assistantPolicyCheck{Scope: "draft-shape-and-revision", Status: status}}
		out := &assistantDomainOutput{Kind: "policy-update", PolicyUpdate: &p}
		if err := e.policyOutputCapacity(ctx, scope, out.Kind); err != nil {
			return result, nil, nil, err
		}
		result, err = assistantPolicyUpdateResult(p)
		return result, nil, out, err
	case "propose_policy_pack_interpretation", "propose_policy_graph_interpretation":
		source, err := service.authoringSource(ctx, scope.ID, a.RevisionID, a.PublicationID)
		if err != nil {
			return result, nil, nil, err
		}
		kind := "pack"
		if name == "propose_policy_graph_interpretation" {
			kind = "graph"
		}
		p := policyInterpretationProposal{Version: 1, ArtifactKind: kind, ID: e.policyHostID(ctx, "handoff"), PolicyID: scope.ID, RevisionID: source.RevisionID, PublicationID: source.PublicationID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), AfterTurnID: e.record.Binding.RequestID}
		out := &assistantDomainOutput{Kind: "policy-interpretation", PolicyInterpretation: &p, PolicySource: &source}
		if err := e.policyOutputCapacity(ctx, scope, out.Kind); err != nil {
			return result, nil, nil, err
		}
		result, err = assistantPolicyInterpretationResult(p, source)
		return result, nil, out, err
	}
	return result, nil, nil, errAssistantRunInvalid
}
func (e *assistantRunExecution) policyHostID(ctx context.Context, kind string) string {
	return assistantPolicyRecordID(e.record.Binding.ExecutionKey, runcontext.CallIDOf(ctx), ctxPolicyCallback(ctx), kind)
}
func assistantPolicyRecordID(request, call, callback, kind string) string {
	return "policy-" + kind + "-" + digestOf([]byte(request + "\x00" + call + "\x00" + callback))[:40]
}
func ctxPolicyCallback(ctx context.Context) string {
	v, _ := ctx.Value(assistantHostCallbackKey{}).(string)
	return v
}
func (e *assistantRunExecution) policyOutputCapacity(ctx context.Context, scope *assistantPolicyScope, kind string) error {
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return err
	}
	n := len(scope.Proposals)
	if kind == "policy-interpretation" {
		n = len(scope.Interpretations)
	}
	for _, out := range run.DomainOutputs {
		if out.Kind == kind {
			n++
		}
	}
	if n >= 16 {
		return failure.ToolInput("This conversation has sixteen proposals of this kind. Review them before proposing another.")
	}
	return nil
}

// Source services bound retained documents; SessionContext owns retrieval and
// compaction. Preserve the exact structured result instead of truncating it.
func assistantHostStructured(v any) (runcontext.ToolResult, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return runcontext.ToolResult{}, err
	}
	parsed, err := runcontext.ParseJSON(raw)
	if err != nil {
		return runcontext.ToolResult{}, err
	}
	return runcontext.ResultOf(nil, parsed), nil
}
func assistantPolicyUpdateResult(p assistantPolicyProposal) (runcontext.ToolResult, error) {
	if p.Check != nil && p.Check.Status == "stale" {
		return assistantHostFailure(failure.ToolInput("The saved Policy changed during this request. The proposal is retained but cannot be saved. Send a fresh request against the latest saved draft.")), nil
	}
	return assistantHostJSON(map[string]any{"artifactType": "policy-update", "proposalId": p.ID, "policyId": p.PolicyID, "revision": p.Revision, "status": "proposed", "checks": map[string]string{"scope": "draft-shape", "status": "passed"}, "guidance": "Review the exact diff and save through the chat action. Shape checks do not establish policy correctness or authorize review/publication. No Policy was changed."})
}
func assistantPolicyInterpretationResult(p policyInterpretationProposal, s policyAuthoringSource) (runcontext.ToolResult, error) {
	return assistantHostJSON(map[string]any{"status": "proposed", "proposalId": p.ID, "policyId": p.PolicyID, "revisionId": s.RevisionID, "publicationId": s.PublicationID, "title": s.Title, "artifactVersion": s.ArtifactVersion, "guidance": "Wait for explicit user review and acceptance. No chat message was sent and no " + p.ArtifactKind + " was generated or saved."})
}
