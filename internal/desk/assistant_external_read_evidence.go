package desk

// Private proof association for the four separately reviewed Desk adapters.
// An association locates original proof; it is neither a receipt signature nor
// an approval. No production declaration is enrolled by this file.
import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

type assistantExternalScope struct {
	Execution  string `json:"execution"`
	Role       string `json:"role"`
	Task       string `json:"task"`
	Generation uint64 `json:"generation"`
}

type assistantExternalReadProof struct {
	Kind                string                      `json:"kind"`
	AcquisitionID       string                      `json:"acquisitionId"`
	Packet              attachmentReceipt           `json:"packet"`
	Document            *assistantDocumentReference `json:"document,omitempty"`
	ConfigurationDigest string                      `json:"configurationDigest"`
	CustodyDigest       string                      `json:"custodyDigest"`
}

type assistantExternalReadEvidence struct {
	Version             int                        `json:"version"`
	ID                  string                     `json:"id"`
	HostOperationID     string                     `json:"hostOperationId"`
	DurableDispatchID   string                     `json:"durableDispatchId"`
	Scope               assistantExternalScope     `json:"scope"`
	SourceBindingDigest string                     `json:"sourceBindingDigest"`
	OwnTool             string                     `json:"ownTool"`
	ArgsCanonical       string                     `json:"argsCanonical"`
	ArgsDigest          string                     `json:"argsDigest"`
	ResultCanonical     string                     `json:"resultCanonical"`
	ResultDigest        string                     `json:"resultDigest"`
	OutputDigest        string                     `json:"outputDigest"`
	ProjectionVersion   string                     `json:"projectionVersion"`
	ProjectionLimit     int                        `json:"projectionLimit"`
	Proof               assistantExternalReadProof `json:"proof"`
}

type assistantExternalCapabilityKey struct{}
type assistantExternalCurrentKey struct{}
type assistantExternalVerificationKey struct{}
type assistantExternalCapability struct {
	owner         *assistantRunExecution
	root, binding *runcontext.RunBinding
	store         *assistantCanonicalRunStore
	scope         assistantExternalScope
	mu            sync.Mutex
	active        bool
}

// This capability is created only at canonicalAdapter's original Writer seam,
// after the actual coordinator assigned this invocation's scope. Copies of a
// saved scope or receipt can never reconstruct it.
func (c *assistantExternalCapability) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil || ctx.Value(assistantExternalCapabilityKey{}) != c {
		return errAssistantRunScope
	}
	c.mu.Lock()
	active := c.active
	c.mu.Unlock()
	if !active || c.scope.Generation == 0 || c.binding == nil || c.root == nil || c.store == nil || c.binding.Root && c.binding != c.root || c.binding.Root != (c.scope.Task == "") || c.binding.Writer != c.root.Writer || c.binding.Writer.Store != c.store || c.binding.Scope.Execution != c.scope.Execution || c.binding.Scope.Role != c.scope.Role || c.binding.Scope.Task != c.scope.Task {
		return errAssistantRunScope
	}
	e := c.owner
	if e == nil {
		return errAssistantRunScope
	}
	c.store.ingress.Lock()
	accepting := c.store.accept
	c.store.ingress.Unlock()
	if !accepting {
		return errAssistantRunScope
	}
	e.canonicalMu.Lock()
	valid := e.canonicalBinding == c.root && e.canonicalStore == c.store && e.generation == c.scope.Generation && !e.stopRequested && e.lifecycle == "running"
	e.canonicalMu.Unlock()
	epoch, ok := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration)
	if !valid || !ok || epoch.owner != e || epoch.number != c.scope.Generation || epoch.finalizer {
		return errAssistantRunScope
	}
	return nil
}

func (e *assistantRunExecution) assistantExternalScopedRun(ctx context.Context, r *adapter.Run) (*adapter.Run, func(), error) {
	if r == nil {
		return nil, nil, errAssistantRunScope
	}
	needed := false
	for _, tool := range r.HostTools {
		if tool.Source != nil && assistantExternalReadName(tool.Name) {
			needed = true
		}
	}
	if !needed {
		return r, func() {}, nil
	}
	e.canonicalMu.Lock()
	root, store, generation := e.canonicalBinding, e.canonicalStore, e.generation
	e.canonicalMu.Unlock()
	if r == nil || r.Context == nil || r.Context.Persistence == nil || root == nil || store == nil {
		return nil, nil, errAssistantRunScope
	}
	binding := r.Context.Persistence
	if binding.Writer != root.Writer || binding.Writer.Store != store || store.writer != e.writer || store.owner != e || store.generation != generation {
		return nil, nil, errAssistantRunScope
	}
	scope := assistantExternalScope{Execution: binding.Scope.Execution, Role: binding.Scope.Role, Task: binding.Scope.Task, Generation: generation}
	// The seam supports admitted author children. Agent's child accounting and
	// final API-scope regressions remain qualification blockers, not fallbacks.
	if scope.Execution == "" || scope.Role != "author" || binding.Root && (binding != root || scope.Task != "") || !binding.Root && (scope.Task == "" || scope.Execution != scope.Task) {
		return nil, nil, errAssistantRunScope
	}
	c := &assistantExternalCapability{owner: e, root: root, binding: binding, store: store, scope: scope, active: true}
	held := *r
	held.HostTools = append([]runcontext.HostTool(nil), r.HostTools...)
	for i := range held.HostTools {
		tool := &held.HostTools[i]
		if tool.Source == nil || !assistantExternalReadName(tool.Name) {
			continue
		}
		source := *tool.Source
		previous := source.VerifyReceipt
		current := source.CurrentIdentity
		expected := source.Identity
		rawSource, _ := json.Marshal(source.Record(tool.Name))
		expectedSource, sourceErr := assistantExternalCanonical(rawSource)
		if sourceErr != nil {
			return nil, nil, sourceErr
		}
		source.VerifyReceipt = func(callCtx context.Context, evidence runcontext.ReceiptEvidence) (bool, error) {
			callCtx = context.WithValue(callCtx, assistantExternalCapabilityKey{}, c)
			callCtx = context.WithValue(callCtx, assistantExternalVerificationKey{}, true)
			callCtx = context.WithValue(callCtx, assistantExternalCurrentKey{}, func(currentCtx context.Context) error {
				if current == nil || expected == nil {
					return errAssistantRunScope
				}
				identity, err := current(currentCtx)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(identity, *expected) {
					return errAssistantRunScope
				}
				return nil
			})
			if evidence.Source != expectedSource {
				return false, errAssistantRunScope
			}
			if err := c.check(callCtx); err != nil {
				return false, err
			}
			if previous == nil || current == nil || expected == nil {
				return false, errAssistantRunScope
			}
			identity, err := current(callCtx)
			if err != nil || !reflect.DeepEqual(identity, *expected) {
				return false, errAssistantRunScope
			}
			ok, err := previous(callCtx, evidence)
			if err != nil || !ok {
				return false, err
			}
			identity, err = current(callCtx)
			if err != nil || !reflect.DeepEqual(identity, *expected) {
				return false, errAssistantRunScope
			}
			return true, c.check(callCtx)
		}
		tool.Source = &source
		execute := tool.Execute
		tool.Execute = func(callCtx context.Context, args json.RawMessage) (runcontext.ToolResult, error) {
			callCtx = context.WithValue(callCtx, assistantExternalCapabilityKey{}, c)
			if err := c.check(callCtx); err != nil {
				return runcontext.ToolResult{}, err
			}
			return execute(callCtx, args)
		}
	}
	return &held, func() { c.mu.Lock(); c.active = false; c.mu.Unlock() }, nil
}

func assistantExternalCurrent(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ctx.Value(assistantHostSourceKey{}) == nil && ctx.Value(assistantExternalVerificationKey{}) != true {
		return nil
	}
	current, ok := ctx.Value(assistantExternalCurrentKey{}).(func(context.Context) error)
	c, capable := ctx.Value(assistantExternalCapabilityKey{}).(*assistantExternalCapability)
	if !ok || !capable {
		return errAssistantRunScope
	}
	if err := c.check(ctx); err != nil {
		return err
	}
	if err := current(ctx); err != nil {
		return err
	}
	return c.check(ctx)
}

func assistantExternalIntentScope(ctx context.Context, in *assistantHostIntent) error {
	if in.SourceBinding == nil || !assistantExternalReadName(in.SourceBinding.OwnTool) {
		return nil
	}
	c, ok := ctx.Value(assistantExternalCapabilityKey{}).(*assistantExternalCapability)
	if !ok {
		return errAssistantRunScope
	}
	if err := assistantExternalCurrent(ctx); err != nil {
		return err
	}
	if in.Root != c.binding.Root {
		return errAssistantRunScope
	}
	scope := c.scope
	in.ExternalScope = &scope
	return nil
}

func assistantExternalScopeMatches(scope assistantExternalScope, d contract.CheckpointDurableDispatch) bool {
	task := ""
	if d.Scope.Task != nil {
		task = *d.Scope.Task
	}
	return scope.Execution == d.Scope.Execution && scope.Role == d.Scope.Role && scope.Task == task
}

func assistantExternalCanonical(raw []byte) (string, error) {
	if !uniqueJSON(raw) {
		return "", errAssistantRunInvalid
	}
	value, err := runcontext.ParseJSON(raw)
	if err != nil {
		return "", err
	}
	return runcontext.CanonicalJSON(value), nil
}
func assistantExternalEvidenceID(a assistantExternalReadEvidence) string {
	a.ID = ""
	return "desk-proof-v1-" + digestOf(assistantChatJSON(a))
}
func assistantExternalProofShape(p assistantExternalReadProof, name string) bool {
	if !contains([]string{"search", "website", "link", "saved-link"}, p.Kind) || !attachmentID.MatchString(p.Packet.ID) || !assistantRunDigest(p.Packet.SHA256) || (!assistantRunDigest(p.ConfigurationDigest) || !assistantRunDigest(p.CustodyDigest)) {
		return false
	}
	if p.Kind == "saved-link" {
		return name == assistantLinkTool && p.AcquisitionID == "" && p.Document != nil && p.Document.ID == p.Packet.ID && assistantDocumentReferenceValid(*p.Document)
	}
	return assistantRunText(p.AcquisitionID, 128) && ((p.Kind == "search" && name == assistantSearchTool || p.Kind == "website" && name == assistantWebsiteTool) && p.Document == nil || p.Kind == "link" && name == assistantLinkTool && p.Document != nil && p.Document.ID == p.Packet.ID)
}
func validateAssistantExternalEvidence(run assistantRunRecord, op assistantRunOperation, in assistantHostIntent, result assistantHostResult) error {
	a := result.ExternalReadEvidence
	if a == nil {
		if in.SourceBinding != nil && assistantExternalReadName(op.Name) && op.Result != nil && len(result.ExternalFailure) == 0 {
			return errAssistantRunInvalid
		}
		return nil
	}
	if in.SourceBinding == nil || in.ExternalScope == nil || result.ExternalControl != nil || len(result.ExternalFailure) != 0 || !assistantExternalReadName(op.Name) || !assistantExternalProofShape(a.Proof, op.Name) || a.Version != 1 || a.ID != assistantExternalEvidenceID(*a) || a.HostOperationID != op.ID || a.DurableDispatchID != in.DurableDispatchID || a.DurableDispatchID == "" || a.Scope != *in.ExternalScope || a.Scope.Generation == 0 || a.SourceBindingDigest != digestOf(assistantChatJSON(in.SourceBinding)) || a.OwnTool != op.Name || a.ProjectionVersion != in.SourceBinding.ProjectionVersion || a.ProjectionLimit != runcontext.Defaults.ToolResultBytes || a.ArgsDigest != digestOf(in.Arguments) || a.ResultDigest != digestOf(result.Result) {
		return errAssistantRunInvalid
	}
	args, err := assistantExternalCanonical(in.Arguments)
	if err != nil || args != a.ArgsCanonical {
		return errAssistantRunInvalid
	}
	value, err := assistantExternalCanonical(result.Result)
	if err != nil || value != a.ResultCanonical {
		return errAssistantRunInvalid
	}
	outputs := 0
	for _, out := range run.DomainOutputs {
		if out.OperationID == op.ID {
			outputs++
			if digestOf(assistantChatJSON(out)) != a.OutputDigest {
				return errAssistantRunInvalid
			}
		}
	}
	if outputs > 1 || (outputs == 0) != (a.OutputDigest == "") {
		return errAssistantRunInvalid
	}
	d := assistantCanonicalRetainedHost(run, op.Name, in)
	if d == nil || !assistantExternalScopeMatches(a.Scope, *d) {
		return errAssistantRunInvalid
	}
	// Tokens are private association IDs; canonical result JSON has no token.
	parsed, err := runcontext.ParseJSON(result.Result)
	if err != nil || parsed.Get("receipt") != nil {
		return errAssistantRunInvalid
	}
	return nil
}

// Witnesses come only from the real producers' verified typed custody. This
// function re-reads the same immutable packet and recomputes the whole result;
// it never acquires, seals, clips arguments, or selects a same-output sibling.
func (e *assistantRunExecution) assistantExternalProjection(ctx context.Context, run assistantRunRecord, name string, args []byte, p assistantExternalReadProof, limit int) (runcontext.ToolResult, error) {
	empty := runcontext.ToolResult{}
	if !assistantExternalProofShape(p, name) {
		return empty, errAssistantRunInvalid
	}
	program, err := e.service.server.researchVerifier()
	if err != nil {
		return empty, err
	}
	packet, err := e.assistantLinkPacket(ctx, p.Packet.ID)
	if err != nil || digestOf(packet) != p.Packet.SHA256 {
		return empty, errAssistantDocument
	}
	var result runcontext.ToolResult
	var originalPackets []*attachmentReceipt
	switch p.Kind {
	case "search":
		query, readErr := readAssistantSearchQuery(args)
		if readErr != nil {
			return empty, readErr
		}
		held, readErr := assistantSearchCustodyOf(run, p.AcquisitionID)
		if readErr != nil || held.DispatchClosed || held.Final.Receipt == nil || *held.Final.Receipt != p.Packet || held.Request.Query != query || held.ConfigurationDigest != p.ConfigurationDigest || digestOf(assistantChatJSON(held)) != p.CustodyDigest {
			return empty, errAssistantSearch
		}
		c, readErr := e.service.server.assistantSearchConfigurationCurrent(ctx, e.source, false)
		if readErr != nil || assistantSearchConfigurationDigest(c) != held.ConfigurationDigest || assistantSearchGatewaySnapshot(c.Gateway) != held.Gateway {
			return empty, errAssistantRunScope
		}
		originalPackets = []*attachmentReceipt{held.Stage.Receipt, held.SealStage.Receipt}
		if readErr = e.assistantExternalPackets(ctx, originalPackets...); readErr != nil {
			return empty, readErr
		}
		found, readErr := verifyAssistantSearchObject(ctx, program, packet, held)
		if readErr != nil {
			return empty, readErr
		}
		ref := assistantSearchReference{ID: held.FinalID, Digest: held.Digest, Request: held.Request}
		result, err = assistantSearchResultReply(found.Result, assistantSearchStep{Query: query, Provider: found.Result.Provider, Submitted: true, Reference: &ref})
	case "website", "link":
		var held *assistantLinkCustody
		for _, candidate := range run.Links {
			if p.Kind == "link" && candidate.HostOperationID == p.AcquisitionID {
				copy := candidate
				if held != nil {
					return empty, errAssistantRunInvalid
				}
				held = &copy
			}
		}
		for _, candidate := range run.Websites {
			if p.Kind == "website" && candidate.HostOperationID == p.AcquisitionID {
				copy := assistantLinkCustody(candidate)
				if held != nil {
					return empty, errAssistantRunInvalid
				}
				held = &copy
			}
		}
		if held == nil || held.DispatchClosed || held.Final.Receipt == nil || *held.Final.Receipt != p.Packet || held.ConfigurationDigest != p.ConfigurationDigest || digestOf(assistantChatJSON(*held)) != p.CustodyDigest {
			return empty, errAssistantDocument
		}
		c, readErr := e.service.server.currentAssistantLinkConfiguration(ctx, e.source, *held)
		if readErr != nil {
			return empty, readErr
		}
		originalPackets = []*attachmentReceipt{held.Stage.Receipt, held.SealStage.Receipt}
		if readErr = e.assistantExternalPackets(ctx, originalPackets...); readErr != nil {
			return empty, readErr
		}
		if held.Request.Site == "" {
			if readErr = e.assistantExternalURLAuthority(ctx, program, run, held.Request.URL, held.AuthorityPacket, c.Gateway); readErr != nil {
				return empty, readErr
			}
		} else {
			if held.AuthorityPacket == nil {
				return empty, errAssistantDocument
			}
			authority, readErr := e.assistantLinkPacket(ctx, held.AuthorityPacket.ID)
			if readErr != nil {
				return empty, readErr
			}
			if readErr = verifyAssistantWebsitePageAuthority(ctx, program, authority, run, e.source, *held); readErr != nil {
				return empty, readErr
			}
			if readErr = e.assistantExternalURLAuthority(ctx, program, run, held.Request.Site, held.SeedAuthorityPacket, c.Gateway); readErr != nil {
				return empty, readErr
			}
		}
		if p.Kind == "website" {
			input, readErr := readAssistantWebsiteArguments(args)
			if readErr != nil || input.Link.FetchURL != held.Request.URL {
				return empty, errAssistantDocument
			}
			found, readErr := verifyAssistantWebsiteObject(ctx, program, packet, *held)
			if readErr != nil {
				return empty, readErr
			}
			result = assistantWebsiteReply(found.Discovery)
		} else {
			input, readErr := readAssistantLinkArguments(args)
			if readErr != nil || input.Link.FetchURL != held.Request.URL {
				return empty, errAssistantDocument
			}
			doc, _, readErr := e.readAssistantAcquiredLink(ctx, *held, c)
			if readErr != nil || !reflect.DeepEqual(doc.reference, p.DocumentValue()) {
				return empty, errAssistantDocument
			}
			result, err = assistantRenderLinkWindow(doc, input, limit)
		}
		if readErr = e.recheckAssistantLink(ctx, c); readErr != nil {
			return empty, readErr
		}
	case "saved-link":
		input, readErr := readAssistantLinkArguments(args)
		if readErr != nil {
			return empty, readErr
		}
		// readAssistantDocument proves the complete saved reference, selected pages,
		// partial permission and current authenticated owner before using this ID.
		doc, readErr := e.service.server.readAssistantDocument(e.request.WithContext(ctx), e.source, *p.Document)
		if readErr != nil || doc.attachmentDigest != p.Packet.SHA256 || digestOf(assistantChatJSON(*p.Document)) != p.CustodyDigest {
			return empty, errAssistantDocument
		}
		c, readErr := e.service.server.assistantLinkConfiguration(false)
		if readErr != nil {
			return empty, readErr
		}
		allowed, readErr := e.assistantWebsiteAuthority(ctx, input.Link.FetchURL, &c)
		if readErr == nil && !allowed {
			allowed, readErr = e.assistantLinkAllowed(ctx, input.Link.FetchURL, &c)
		}
		if readErr != nil || !allowed || assistantLinkConfigurationDigest(c) != p.ConfigurationDigest {
			return empty, errAssistantRunScope
		}
		result, err = assistantRenderLinkWindow(doc, input, limit)
		if readErr = e.recheckAssistantLink(ctx, c); readErr != nil {
			return empty, readErr
		}
	}
	if err != nil {
		return empty, err
	}
	if err = e.assistantExternalPackets(ctx, originalPackets...); err != nil {
		return empty, err
	}
	again, err := e.assistantLinkPacket(ctx, p.Packet.ID)
	if err != nil || !bytes.Equal(again, packet) {
		return empty, errAssistantDocument
	}
	return result, ctx.Err()
}
func (p assistantExternalReadProof) DocumentValue() assistantDocumentReference {
	if p.Document == nil {
		return assistantDocumentReference{}
	}
	return *p.Document
}

func (e *assistantRunExecution) prepareAssistantExternalEvidence(ctx context.Context, op assistantRunOperation, in assistantHostIntent, result runcontext.ToolResult, proof *assistantExternalReadProof, output *assistantDomainOutput) (*assistantExternalReadEvidence, error) {
	if in.SourceBinding == nil || !assistantExternalReadName(op.Name) {
		return nil, nil
	}
	if proof == nil || result.Receipt != "" || in.ExternalScope == nil {
		return nil, errAssistantRunDispatch
	}
	if err := assistantExternalCurrent(ctx); err != nil {
		return nil, err
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return nil, err
	}
	d := assistantCanonicalRetainedHost(run, op.Name, in)
	if d == nil || d.State != "claimed" || !assistantExternalScopeMatches(*in.ExternalScope, *d) {
		return nil, errAssistantRunScope
	}
	projected, err := e.assistantExternalProjection(ctx, run, op.Name, in.Arguments, *proof, runcontext.Defaults.ToolResultBytes)
	if err != nil || !bytes.Equal(projected.JSON(), result.JSON()) {
		return nil, errAssistantDocument
	}
	a := assistantExternalReadEvidence{Version: 1, HostOperationID: op.ID, DurableDispatchID: in.DurableDispatchID, Scope: *in.ExternalScope, SourceBindingDigest: digestOf(assistantChatJSON(in.SourceBinding)), OwnTool: op.Name, ArgsDigest: digestOf(in.Arguments), ResultDigest: digestOf(result.JSON()), ProjectionVersion: in.SourceBinding.ProjectionVersion, ProjectionLimit: runcontext.Defaults.ToolResultBytes, Proof: *proof}
	a.ArgsCanonical, err = assistantExternalCanonical(in.Arguments)
	if err != nil {
		return nil, err
	}
	a.ResultCanonical, err = assistantExternalCanonical(result.JSON())
	if err != nil {
		return nil, err
	}
	if output != nil {
		clone := *output
		clone.RunID, clone.RequestKey, clone.OperationID, clone.CallID = run.ID, run.Binding.ExecutionKey, op.ID, in.CallID
		a.OutputDigest = digestOf(assistantChatJSON(clone))
	}
	a.ID = assistantExternalEvidenceID(a)
	return &a, assistantExternalCurrent(ctx)
}

// E6 supplies this actual callback to assistantExternalSourceTool. It is dormant
// until enrollment and must execute inside the original scoped adapter closure.
func (e *assistantRunExecution) verifyAssistantExternalReadReceipt(ctx context.Context, evidence runcontext.ReceiptEvidence) (bool, error) {
	if ctx.Value(assistantExternalVerificationKey{}) != true {
		return false, errAssistantRunScope
	}
	if err := assistantExternalCurrent(ctx); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c, ok := ctx.Value(assistantExternalCapabilityKey{}).(*assistantExternalCapability)
	if !ok || c.owner != e {
		return false, errAssistantRunScope
	}
	if err := c.check(ctx); err != nil {
		return false, err
	}
	if err := e.Authorize(ctx); err != nil {
		return false, err
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return false, err
	}
	var op *assistantRunOperation
	var in assistantHostIntent
	var retained assistantHostResult
	for _, candidate := range run.Operations {
		var r assistantHostResult
		if candidate.Kind != "host" || candidate.Result == nil || readAssistantHostReceipt(candidate.Result, &r) != nil || r.ExternalReadEvidence == nil || r.ExternalReadEvidence.ID != evidence.Receipt {
			continue
		}
		if op != nil || readAssistantHostReceipt(candidate.Intent, &in) != nil {
			return false, errAssistantRunInvalid
		}
		copy := candidate
		op = &copy
		retained = r
	}
	if op == nil || in.SourceBinding == nil || in.CallID != runcontext.CallIDOf(ctx) || retained.ExternalReadEvidence.Scope != c.scope || validateAssistantExternalEvidence(run, *op, in, retained) != nil || evidence.Source != string(in.SourceBinding.SourceRecord) || evidence.Tool != op.Name || evidence.Args != retained.ExternalReadEvidence.ArgsCanonical || evidence.Result != retained.ExternalReadEvidence.ResultCanonical {
		return false, errAssistantRunInvalid
	}
	d := assistantCanonicalRetainedHost(run, op.Name, in)
	if d == nil || d.State != "result" || d.Failure != nil || !assistantCanonicalHostResultMatches(run, *op, in) {
		return false, errAssistantRunDispatch
	}
	projected, err := e.assistantExternalProjection(ctx, run, op.Name, in.Arguments, retained.ExternalReadEvidence.Proof, retained.ExternalReadEvidence.ProjectionLimit)
	if err != nil || !bytes.Equal(projected.JSON(), retained.Result) {
		return false, errAssistantDocument
	}
	// Re-read after verifier/process waits. No lock spans verification; immutable
	// physical bytes, exact canonical row and original Writer must still match.
	latest, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return false, err
	}
	current := assistantDialectOperation(latest, op.ID)
	if current == nil || !bytes.Equal(current.Intent, op.Intent) || !bytes.Equal(current.Result, op.Result) || current.State != op.State || !assistantCanonicalHostResultMatches(latest, *current, in) {
		return false, errAssistantRunConflict
	}
	if err = e.Authorize(ctx); err != nil {
		return false, err
	}
	return true, assistantExternalCurrent(ctx)
}

func (e *assistantRunExecution) restoreAssistantExternalReceipt(ctx context.Context, op assistantRunOperation, in assistantHostIntent, retained assistantHostResult) (runcontext.ToolResult, error) {
	if retained.ExternalReadEvidence == nil {
		return runcontext.ToolResult{}, errAssistantRunDispatch
	}
	c, ok := ctx.Value(assistantExternalCapabilityKey{}).(*assistantExternalCapability)
	if !ok || c.owner != e || c.scope != retained.ExternalReadEvidence.Scope || in.CallID != runcontext.CallIDOf(ctx) {
		return runcontext.ToolResult{}, errAssistantRunScope
	}
	if err := assistantExternalCurrent(ctx); err != nil {
		return runcontext.ToolResult{}, err
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return runcontext.ToolResult{}, err
	}
	if validateAssistantExternalEvidence(run, op, in, retained) != nil {
		return runcontext.ToolResult{}, errAssistantRunInvalid
	}
	// Replay may follow a claimed row before Agent commits, or its exact durable
	// result. It never turns an unknown outcome into a success or redoes HTTP.
	d := assistantCanonicalRetainedHost(run, op.Name, in)
	if d == nil || d.State != "claimed" && d.State != "result" || d.Failure != nil {
		return runcontext.ToolResult{}, errAssistantRunDispatch
	}
	result, err := e.assistantExternalProjection(ctx, run, op.Name, in.Arguments, retained.ExternalReadEvidence.Proof, retained.ExternalReadEvidence.ProjectionLimit)
	if err != nil || !bytes.Equal(result.JSON(), retained.Result) {
		return runcontext.ToolResult{}, errAssistantDocument
	}
	if err = assistantExternalCurrent(ctx); err != nil {
		return runcontext.ToolResult{}, err
	}
	result.Receipt = retained.ExternalReadEvidence.ID
	return result, nil
}

// Historical source custody requires the final accepted original envelope,
// after durable commit and custom verification. A physical result/observation
// or receipt locator alone is insufficient. This does not grant live authority.
func assistantExternalAccepted(run assistantRunRecord, op assistantRunOperation, in assistantHostIntent) bool {
	var retained assistantHostResult
	if readAssistantHostReceipt(op.Result, &retained) != nil || retained.ExternalReadEvidence == nil || validateAssistantExternalEvidence(run, op, in, retained) != nil || run.Canonical == nil {
		return false
	}
	d := assistantCanonicalRetainedHost(run, op.Name, in)
	if d == nil || d.State != "result" || d.Failure != nil || !assistantCanonicalHostResultMatches(run, op, in) {
		return false
	}
	count := 0
	for _, raw := range run.Canonical.Events {
		var envelope runcontext.StoredRunEnvelope
		if json.Unmarshal(raw.Bytes, &envelope) != nil {
			return false
		}
		event := envelope.Event.ToolResult
		if event == nil || event.CallID == nil || *event.CallID != in.CallID {
			continue
		}
		if envelope.Scope.Execution != retained.ExternalReadEvidence.Scope.Execution || envelope.Scope.Role != retained.ExternalReadEvidence.Scope.Role || envelope.Scope.Task != retained.ExternalReadEvidence.Scope.Task {
			continue
		}
		if event.Name != in.SourceBinding.WireName || event.Source == nil || event.Source.Receipt == nil || *event.Source.Receipt != retained.ExternalReadEvidence.ID {
			return false
		}
		source := *event.Source
		source.Receipt = nil
		rawSource, _ := json.Marshal(source)
		canonical, err := assistantExternalCanonical(rawSource)
		if err != nil || canonical != string(in.SourceBinding.SourceRecord) {
			return false
		}
		// Contract's exact content/structured/isError projection uses Agent's own
		// ToolResult reader, preserving the complete result rather than text alone.
		value, err := runcontext.ParseJSON(retained.Result)
		if err != nil || !assistantExternalEventResultEqual(event, runcontext.ReadToolResult(value)) {
			return false
		}
		count++
	}
	return count == 1
}

func assistantExternalEventResultEqual(event *contract.ToolResult, result runcontext.ToolResult) bool {
	// The original RunWriter stamps the final event before canonical storage.
	// An unversioned decoration alone is not an acknowledged final envelope.
	if event.Type != "tool_result" || event.IsError != result.Err() || event.Proposed != nil || event.Version == nil || *event.Version != contract.EventVersion {
		return false
	}
	parts := []string{}
	for _, part := range result.Content {
		if part.Type() != "text" {
			return false
		}
		parts = append(parts, part.Text())
	}
	if event.Text != strings.Join(parts, "\n") {
		return false
	}
	if result.StructuredContent == nil {
		return len(event.Structured) == 0
	}
	return assistantCanonicalHostResultWireEqual(result.StructuredContent.JSON(), event.Structured)
}

// Only invokeObservedHost installs this exact operation after durable admission.
// Producer acquisition lookup does not scan siblings by public call ID.
type assistantExternalPhysicalKey struct{}
type assistantExternalPhysical struct {
	ID, Name string
	Intent   assistantHostIntent
}

func (e *assistantRunExecution) assistantExternalPhysicalHost(ctx context.Context, name string, fallback func() (string, error)) (string, error) {
	if !assistantExternalSourced(ctx, name) {
		return fallback()
	}
	held, ok := ctx.Value(assistantExternalPhysicalKey{}).(assistantExternalPhysical)
	if !ok || held.Name != name || held.Intent.ExternalScope == nil {
		return "", errAssistantRunScope
	}
	if err := assistantExternalCurrent(ctx); err != nil {
		return "", err
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return "", err
	}
	op := assistantDialectOperation(run, held.ID)
	if op == nil || op.State != "dispatched" || op.Name != name || !bytes.Equal(op.Intent, assistantChatJSON(held.Intent)) {
		return "", errAssistantRunScope
	}
	return held.ID, nil
}

func (e *assistantRunExecution) assistantExternalPackets(ctx context.Context, receipts ...*attachmentReceipt) error {
	for _, receipt := range receipts {
		if receipt == nil {
			return errAssistantDocument
		}
		raw, err := e.assistantLinkPacket(ctx, receipt.ID)
		if err != nil || digestOf(raw) != receipt.SHA256 {
			return errAssistantDocument
		}
	}
	return ctx.Err()
}

// Verify the originally selected authority, not whichever later source happens
// to contain the same URL. Current consent/configuration remain separate checks.
func (e *assistantRunExecution) assistantExternalURLAuthority(ctx context.Context, program string, run assistantRunRecord, url string, receipt *attachmentReceipt, pin researchGateway) error {
	if receipt == nil {
		if assistantLinkGiven(e.source.Original, url) {
			return nil
		}
		return errAssistantRunScope
	}
	raw, err := e.assistantLinkPacket(ctx, receipt.ID)
	if err != nil || digestOf(raw) != receipt.SHA256 {
		return errAssistantDocument
	}
	var result assistantSearchResult
	found := false
	for _, held := range run.Searches {
		if held.Final.Receipt == nil || held.FinalID != receipt.ID {
			continue
		}
		if found || *held.Final.Receipt != *receipt || held.DispatchClosed {
			return errAssistantDocument
		}
		current, err := e.service.server.assistantSearchConfigurationCurrent(ctx, e.source, false)
		if err != nil || assistantSearchConfigurationDigest(current) != held.ConfigurationDigest || held.Gateway != assistantSearchGatewaySnapshot(pin) {
			return errAssistantRunScope
		}
		if err = e.assistantExternalPackets(ctx, held.Stage.Receipt, held.SealStage.Receipt); err != nil {
			return err
		}
		verified, err := verifyAssistantSearchObject(ctx, program, raw, held)
		if err != nil {
			return err
		}
		result, found = verified.Result, true
	}
	if !found {
		var source struct {
			Searches []assistantSearchReference `json:"searches"`
		}
		if json.Unmarshal(e.source.Original, &source) != nil {
			return errAssistantRunInvalid
		}
		for _, ref := range source.Searches {
			if ref.ID != receipt.ID {
				continue
			}
			if found {
				return errAssistantDocument
			}
			current, err := e.service.server.assistantSearchConfigurationCurrent(ctx, e.source, false)
			if err != nil || current.Connection != ref.Request.Connection || current.Revision != ref.Request.Revision {
				return errAssistantRunScope
			}
			result, err = verifyAssistantSavedSearch(ctx, program, raw, ref, pin)
			if err != nil {
				return err
			}
			found = true
		}
	}
	if found {
		for _, hit := range result.Hits {
			normalized, err := normalizeAssistantLink(hit.URL)
			if err == nil && normalized.FetchURL == url {
				return e.assistantExternalPackets(ctx, receipt)
			}
		}
		return errAssistantRunScope
	}
	// Existing supplied-document redirect authority requires its exact selected
	// saved reference and verified requested/resolved URL relationship.
	for _, file := range assistantSavedLinkAttachments(e.source.Original) {
		if file.Document == nil || file.Link == nil || file.Document.ID != receipt.ID || file.Link.ResolvedURL != url {
			continue
		}
		doc, err := e.service.server.readAssistantDocument(e.request.WithContext(ctx), e.source, *file.Document)
		if err != nil || doc.attachmentDigest != receipt.SHA256 {
			return errAssistantDocument
		}
		source := assistantLinkRecordSource(doc.record)
		if source["kind"] == "web" && source["requestedUrl"] == file.Link.URL && source["url"] == url {
			return e.assistantExternalPackets(ctx, receipt)
		}
	}
	return errAssistantRunScope
}

// Select a read-only retained result before the fresh-dispatch claim path.
// No claim is created/selected by this function. Public args alone cannot select
// history: actual call/callback, current original Writer, scope, generation and
// the complete original intent must match exactly. New claimed work always uses
// the unchanged fresh claimed-row guard and cannot be turned into this replay.
func (e *assistantRunExecution) selectAssistantExternalReplay(ctx context.Context, run assistantRunRecord, name string, in assistantHostIntent) (*assistantRunOperation, error) {
	if in.SourceBinding == nil || in.ExternalScope == nil {
		return nil, nil
	}
	c, ok := ctx.Value(assistantExternalCapabilityKey{}).(*assistantExternalCapability)
	if !ok || c.owner != e || c.scope != *in.ExternalScope || in.CallID != runcontext.CallIDOf(ctx) || in.DurableDispatchID != "" {
		return nil, errAssistantRunScope
	}
	if err := c.check(ctx); err != nil {
		return nil, err
	}
	if run.Checkpoint == nil || run.Checkpoint.Durable == nil {
		return nil, errAssistantRunScope
	}
	for _, d := range run.Checkpoint.Durable.Dispatches {
		if d.State == "claimed" && assistantCanonicalHostDispatch(run, name, in, d) {
			return nil, nil
		}
	}
	expected := assistantChatJSON(in)
	var selected *assistantRunOperation
	for _, op := range run.Operations {
		if op.Kind != "host" || op.Name != name || op.State != "completed" && op.State != "refused" {
			continue
		}
		var original assistantHostIntent
		if readAssistantHostReceipt(op.Intent, &original) != nil {
			return nil, errAssistantRunInvalid
		}
		base := original
		base.DurableDispatchID = ""
		if !bytes.Equal(assistantChatJSON(base), expected) {
			continue
		}
		if selected != nil {
			return nil, errAssistantRunScope
		}
		d := assistantCanonicalRetainedHost(run, name, original)
		var result assistantHostResult
		if d == nil || d.State != "unknown" && d.State != "result" || readAssistantHostReceipt(op.Result, &result) != nil || validateAssistantExternalResult(run, op, original, result) != nil || validateAssistantExternalEvidence(run, op, original, result) != nil || !assistantCanonicalHostResultMatches(run, op, original) || !assistantNativeHostCall(run, name, original) || original.Root && !assistantParentHostCall(run, name, original) {
			return nil, errAssistantRunScope
		}
		copy := op
		selected = &copy
	}
	return selected, nil
}
