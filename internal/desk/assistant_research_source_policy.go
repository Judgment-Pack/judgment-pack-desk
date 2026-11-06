package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"reflect"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

const assistantResearchPolicyOperation = "dialect-research/source-policy"
const assistantResearchClaimOperation = "dialect-research/source-claim"

// These are private, host-authored associations. A retained policy is never
// authority on its own: final admission rechecks the live source and exact run.
// An empty AllowanceID freezes review-only; it cannot discover a later grant.
type inputResearchRequestDisposition struct {
	AllowanceID       string `json:"allowanceId"`
	AllowanceRevision string `json:"allowanceRevision"`
}

type assistantResearchSourceDisposition struct {
	Version         int                             `json:"version"`
	RunID           string                          `json:"runId"`
	ExecutionKey    string                          `json:"executionKey"`
	ReadOperationID string                          `json:"readOperationId"`
	SessionScope    string                          `json:"sessionScope"`
	Request         inputResearchRequest            `json:"request"`
	Arguments       json.RawMessage                 `json:"arguments"`
	Source          assistantHostSourceBinding      `json:"source"`
	Allowance       inputResearchRequestDisposition `json:"allowance"`
}

type assistantResearchSourceClaim struct {
	PolicyID          string `json:"policyId"`
	DurableDispatchID string `json:"durableDispatchId"`
	CallID            string `json:"callId"`
	CallbackID        string `json:"callbackId"`
}

type assistantResearchSourcePrepared struct {
	PolicyID    string
	Disposition assistantResearchSourceDisposition
}
type assistantResearchSourcePreparedKey struct{}

func researchAllowanceRevision(run inputResearchRun) string {
	// The existing allowance has no separate counter. Pin its complete reviewed
	// journal snapshot, including identity, approval, recipe, companies, sources,
	// limits, execution and budget basis; do not confuse preparation revision
	// with allowance revision or pick a different matching run at admission.
	return digestOf(assistantChatJSON(run))
}

func validResearchRequestDisposition(d inputResearchRequestDisposition) bool {
	return d.AllowanceID == "" && d.AllowanceRevision == "" || inputResearchID.MatchString(d.AllowanceID) && assistantRunDigest(d.AllowanceRevision)
}
func researchDispositionMatches(d inputResearchRequestDisposition, run inputResearchRun) bool {
	return d.AllowanceID != "" && run.ID == d.AllowanceID && researchAllowanceRevision(run) == d.AllowanceRevision
}

func assistantResearchApprovalBinding(approval assistantSourceApproval) (assistantHostSourceBinding, error) {
	row := approval.Snapshot
	if row.AdapterID != "desk-research-workflow" || validateAssistantSourceSnapshot(row, approval.Scope.Project) != nil || row.Status != "review-required" {
		return assistantHostSourceBinding{}, errAssistantRunScope
	}
	source := runcontext.ToolSource{Kind: row.Kind, ID: row.AdapterID, Trust: row.Trust, Version: row.AdapterVersion, ToolListDigest: row.ToolListDigest, Identity: row.Identity}
	raw, err := json.Marshal(source.Record(assistantResearchTool))
	if err != nil {
		return assistantHostSourceBinding{}, err
	}
	value, err := runcontext.ParseJSON(raw)
	if err != nil {
		return assistantHostSourceBinding{}, err
	}
	binding := assistantHostSourceBinding{Version: 1, OwnTool: assistantResearchTool, WireName: runcontext.WireName(row.AdapterID, assistantResearchTool), SourceRecord: []byte(runcontext.CanonicalJSON(value)), SourceDigest: runcontext.RecordDigest(value), ApprovalID: approval.ID, ApprovalRevision: approval.Revision, ConfigurationDigest: row.ConfigurationDigest, ProjectionVersion: row.AdapterVersion}
	if _, ok := assistantHostSourceRecord(&binding, assistantResearchTool); !ok {
		return assistantHostSourceBinding{}, errAssistantRunScope
	}
	return binding, nil
}

// Caller holds the conversation/private lock. This workflow owner is entirely
// local: re-read the authenticated scope, reviewed complete declaration and
// current owner/configuration without acquiring, starting or probing a source.
func (e *assistantRunExecution) authorizeAssistantResearchSourceLocked(ctx context.Context, binding assistantHostSourceBinding) error {
	if err := e.authorizeAssistantResearchLocked(ctx); err != nil {
		return err
	}
	s := e.service.server
	scope, source, err := s.assistantSourceScopeCurrent(e.request.WithContext(ctx), e.record.Binding.Owner.ID)
	if err != nil {
		return err
	}
	if e.record.Binding.Phase != "work" || scope.Project != e.record.Binding.Project || scope.Principal != e.record.Binding.Principal || scope.PolicyDigest != e.record.Binding.Policy {
		return errAssistantRunScope
	}
	store, err := s.openChatData()
	if err != nil {
		return err
	}
	defer store.root.Close()
	ledger, err := readAssistantSourceLedger(store.root, scope.Project)
	if err != nil {
		return err
	}
	for _, approval := range ledger.Approvals {
		if approval.ID != binding.ApprovalID {
			continue
		}
		if approval.Historical || approval.Scope != scope || approval.RevokedAt != "" || approval.Revision != binding.ApprovalRevision || !s.assistant.usable() {
			return errAssistantRunScope
		}
		expected, err := assistantResearchApprovalBinding(approval)
		binding.CallbackAlias = ""
		if err != nil || !reflect.DeepEqual(expected, binding) {
			return errAssistantRunScope
		}
		row := approval.Snapshot
		owner := digestOf(assistantChatJSON([]string{"desk-source-owner/v1", scope.Project, digestOf([]byte(s.projectDir)), row.AdapterID}))
		if row.Configuration.OwnerDigest != owner {
			return errAssistantRunScope
		}
		return s.assistantSourceCatalogLocalCurrent(source, []assistantSourceSnapshot{row})
	}
	return errAssistantRunScope
}

// E6 supplies this policy when it explicitly enrolls the workflow source. The
// existing factory is deliberately unchanged. Neither a source-list approval
// nor an Agent approval checkpoint creates an allowance.
func (e *assistantRunExecution) assistantResearchSourcePolicy(approval assistantSourceApproval) runcontext.ApprovalPolicy {
	return func(call runcontext.ApprovalRequest) string {
		ctx := e.ctx
		binding, err := assistantResearchApprovalBinding(approval)
		if err != nil || call.Tool != assistantResearchTool || call.Name != binding.WireName {
			return "refuse"
		}
		raw, err := json.Marshal(call.Source)
		if err != nil || !assistantHostSourceCallRecordMatches(binding, raw) {
			return "refuse"
		}
		a, err := readAssistantDialectResearchArguments(call.Args)
		if err != nil || e.Authorize(ctx) != nil {
			return "refuse"
		}
		record, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
		if err != nil {
			return "refuse"
		}
		readOp, read, err := assistantDialectReadOperation(record, assistantResearchTool, a)
		if err != nil || !validAssistantDialectActionRead(read) || read.Revision != a.Revision || read.Recipe.Recipe.Research == nil || record.Binding.Phase != "work" || !assistantCanonicalActive(record) {
			return "refuse"
		}
		args, err := runcontext.ParseJSON(call.Args)
		if err != nil {
			return "refuse"
		}
		q := inputResearchRequest{ChatID: record.Binding.Owner.ID, PreparationID: a.ID, Revision: a.Revision, TurnID: record.Binding.RequestID}
		d := assistantResearchSourceDisposition{Version: 1, RunID: record.ID, ExecutionKey: record.Binding.ExecutionKey, ReadOperationID: readOp.ID, SessionScope: e.service.server.researchApprovalScope(e.request), Request: q, Arguments: []byte(runcontext.CanonicalJSON(args)), Source: binding}
		d.Allowance, err = e.freezeAssistantResearchAllowance(ctx, d, read)
		if err != nil {
			return "refuse"
		}
		prepared := false
		err = e.writer.change(ctx, false, func(current *assistantRunRecord) (bool, error) {
			// One pending preparation per execution. Do not substitute a policy
			// during the interval between approval and the first durable claim.
			if assistantResearchPolicyPending(*current) {
				return false, nil
			}
			op, held, err := assistantDialectReadOperation(*current, assistantResearchTool, a)
			if err != nil || op.ID != d.ReadOperationID || !reflect.DeepEqual(read, held) {
				return false, nil
			}
			// Agent reuses a non-read result for the same canonical effect before
			// Execute. That path has no new claim: retain the original association,
			// rather than leaving an unclaimable preparation behind. This check is
			// after live source authorization, never authority from saved history.
			if replay, err := assistantResearchSourceReplay(*current, d); replay || err != nil {
				prepared = replay && err == nil
				return false, nil
			}
			if err := appendAssistantResearchAssociation(current, assistantResearchPolicyOperation, d); err != nil {
				return false, err
			}
			prepared = true
			return true, nil
		})
		e.changed(err)
		if err != nil || !prepared {
			return "refuse"
		}
		// A is bounded by the exact independently reviewed allowance. B is
		// allowed only into an executor whose queue path is disabled by its pin.
		return "allow"
	}
}

// Recognize only the exact root effect that Agent's durable writer will reuse.
// A completed result must retain its original policy, claim, physical producer
// and output. Unknown/refused outcomes cannot create another preparation or be
// promoted to successful observations. Call IDs remain those of the original
// effect; a replay's new tool event is not another physical host invocation.
func assistantResearchSourceReplay(run assistantRunRecord, d assistantResearchSourceDisposition) (bool, error) {
	if run.Checkpoint == nil || run.Checkpoint.Durable == nil {
		return false, errAssistantRunScope
	}
	in := assistantHostIntent{Root: true, Arguments: d.Arguments, SourceBinding: &d.Source}
	dispatchID := ""
	for _, dispatch := range run.Checkpoint.Durable.Dispatches {
		if !assistantCanonicalHostDispatch(run, assistantResearchTool, in, dispatch) {
			continue
		}
		if dispatchID != "" || dispatch.State != "result" || dispatch.Failure != nil || len(dispatch.Result) == 0 {
			return true, errAssistantRunScope
		}
		dispatchID = dispatch.ID
	}
	if dispatchID == "" {
		return false, nil
	}
	if validateAssistantResearchOperations(run) != nil {
		return true, errAssistantRunScope
	}
	var host *assistantRunOperation
	var intent assistantHostIntent
	var receipt assistantHostResult
	for _, op := range run.Operations {
		if op.Kind != "host" || op.Name != assistantResearchTool {
			continue
		}
		var held assistantHostIntent
		if readAssistantHostReceipt(op.Intent, &held) != nil || held.DurableDispatchID != dispatchID {
			continue
		}
		if host != nil || !held.Root || held.SourceBinding == nil || op.State != "completed" || !assistantCanonicalParentHostCall(run, op.Name, held) || !assistantNativeHostCall(run, op.Name, held) || !assistantCanonicalHostResultMatches(run, op, held) || readAssistantHostReceipt(op.Result, &receipt) != nil {
			return true, errAssistantRunScope
		}
		binding := *held.SourceBinding
		binding.CallbackAlias = ""
		if !reflect.DeepEqual(binding, d.Source) {
			return true, errAssistantRunScope
		}
		copy := op
		host, intent = &copy, held
	}
	if host == nil {
		return true, errAssistantRunScope
	}
	result, err := runcontext.ParseJSON(receipt.Result)
	if err != nil || runcontext.ReadToolResult(result).Err() {
		return true, errAssistantRunScope
	}
	results := 0
	for _, envelope := range run.Events {
		event := envelope.Event.ToolResult
		if event == nil || event.CallID == nil || *event.CallID != intent.CallID {
			continue
		}
		if event.Name != d.Source.WireName || event.Source == nil || !assistantExternalEventResultEqual(event, runcontext.ReadToolResult(result)) {
			return true, errAssistantRunScope
		}
		raw, err := json.Marshal(event.Source)
		if err != nil || !assistantHostSourceCallRecordMatches(d.Source, raw) {
			return true, errAssistantRunScope
		}
		results++
	}
	if results != 1 {
		return true, errAssistantRunScope
	}
	outputs := 0
	for _, output := range run.DomainOutputs {
		if output.DialectAction == nil || output.DialectAction.Research == nil {
			continue
		}
		producer := assistantDialectOperation(run, output.DialectAction.Research.OperationID)
		var request assistantResearchIntent
		if producer == nil || readAssistantHostReceipt(producer.Intent, &request) != nil || request.HostOperationID != host.ID {
			continue
		}
		if validateAssistantResearchOutput(run, output, map[string]assistantHostResult{host.ID: receipt}, *host, intent) != nil {
			return true, errAssistantRunScope
		}
		outputs++
	}
	if outputs != 1 {
		return true, errAssistantRunScope
	}
	return true, nil
}

func assistantHostSourceCallRecordMatches(binding assistantHostSourceBinding, raw []byte) bool {
	value, err := runcontext.ParseJSON(raw)
	return err == nil && runcontext.CanonicalJSON(value) == string(binding.SourceRecord)
}

func appendAssistantResearchAssociation(run *assistantRunRecord, name string, in any) error {
	id := nextAssistantRunIdentity(run, "action")
	changed, err := appendAssistantRunIntent(run, assistantRunOperation{ID: id, CallID: id, Kind: "action", Name: name, Intent: assistantChatJSON(in)})
	if err != nil || !changed {
		return errAssistantRunConflict
	}
	// This operation retains metadata, never dispatches work. Keep both parts
	// in the same bounded ledger transaction/readback, including actual bytes.
	op := &run.Operations[len(run.Operations)-1]
	op.State, op.Result = "completed", []byte(`{"retained":true}`)
	return nil
}

func assistantResearchPolicyPending(run assistantRunRecord) bool {
	claimed := map[string]bool{}
	for _, op := range run.Operations {
		if op.Name == assistantResearchClaimOperation {
			var claim assistantResearchSourceClaim
			if readAssistantHostReceipt(op.Intent, &claim) == nil {
				claimed[claim.PolicyID] = true
			}
		}
	}
	for _, op := range run.Operations {
		if op.Name == assistantResearchPolicyOperation && !claimed[op.ID] {
			return true
		}
	}
	return false
}

func (e *assistantRunExecution) freezeAssistantResearchAllowance(ctx context.Context, d assistantResearchSourceDisposition, read assistantDialectRead) (inputResearchRequestDisposition, error) {
	empty := inputResearchRequestDisposition{}
	s := e.service.server
	r := e.request.WithContext(ctx)
	lock, err := s.privateDataLockContext(ctx, true)
	if err != nil {
		return empty, err
	}
	defer lock.Close()
	if err = e.authorizeAssistantResearchSourceLocked(ctx, d.Source); err != nil {
		return empty, err
	}
	root, err := s.inputResearchRoot(false)
	if errors.Is(err, os.ErrNotExist) {
		return empty, e.checkAssistantResearchReadLocked(ctx, read)
	}
	if err != nil {
		return empty, err
	}
	defer root.Close()
	admission, err := root.Open(".")
	if err != nil {
		return empty, err
	}
	defer admission.Close()
	if err = lockPrivateData(admission, true); err != nil {
		return empty, err
	}
	grants, err := s.researchApprovals(ctx, root, d.Request.ChatID, d.SessionScope)
	if err != nil {
		return empty, err
	}
	if err = e.checkAssistantResearchReadLocked(ctx, read); err != nil {
		return empty, err
	}
	for _, grant := range grants {
		if matchingResearchRequest(grant, d.Request) {
			return empty, nil // Historical receipt remains observation only.
		}
	}
	for _, grant := range grants {
		if researchApprovalState(grant, time.Now()) != "ready" || grant.PreparationID != d.Request.PreparationID || grant.Revision != d.Request.Revision {
			continue
		}
		if err = s.researchInputAvailable(ctx, root, grant.PreparationID, grant.ID); err != nil {
			return empty, err
		}
		if err = s.researchBudgetIsLatest(ctx, root, grant); err != nil {
			return empty, err
		}
		preparation, err := s.preparationRepository().TryRead(ctx)
		if err != nil {
			return empty, err
		}
		_, err = s.validateResearchAdmissionView(r, grant, preparation)
		preparation.Close()
		if err != nil {
			return empty, err
		}
		return inputResearchRequestDisposition{AllowanceID: grant.ID, AllowanceRevision: researchAllowanceRevision(grant)}, nil
	}
	return empty, nil
}

func (e *assistantRunExecution) checkAssistantResearchReadLocked(ctx context.Context, read assistantDialectRead) error {
	p, err := e.service.server.preparationRepository().TryRead(ctx)
	if err != nil {
		return err
	}
	defer p.Close()
	saved, err := p.Get(read.ID)
	if err != nil {
		return err
	}
	if saved.SHA256 != read.Revision || !reflect.DeepEqual(assistantDialectRecipeBasis(saved), *read.Recipe) {
		return errAssistantRunConflict
	}
	return ctx.Err()
}

// Execute binds exactly once to the first matching live canonical root claim.
// Agent's generic approval continuation cannot manufacture this host association.
func (e *assistantRunExecution) claimAssistantResearchSourcePolicy(ctx context.Context, raw []byte, binding assistantHostSourceBinding) (*assistantResearchSourcePrepared, error) {
	root, _ := ctx.Value(assistantHostRootKey{}).(bool)
	if !root || e.record.Binding.Phase != "work" {
		return nil, errAssistantRunScope
	}
	callback, _ := ctx.Value(assistantHostCallbackKey{}).(string)
	in := assistantHostIntent{Root: true, Arguments: bytes.Clone(raw), CallID: runcontext.CallIDOf(ctx), CallbackID: callback, SourceBinding: &binding}
	var prepared *assistantResearchSourcePrepared
	err := e.writer.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		claimID, err := assistantCanonicalHostClaim(*run, assistantResearchTool, in)
		if err != nil {
			return false, nil
		}
		in.DurableDispatchID = claimID
		if !assistantParentHostCall(*run, assistantResearchTool, in) || !assistantNativeHostCall(*run, assistantResearchTool, in) {
			return false, nil
		}
		used := map[string]bool{}
		for _, op := range run.Operations {
			if op.Name == assistantResearchClaimOperation {
				var claim assistantResearchSourceClaim
				if readAssistantHostReceipt(op.Intent, &claim) != nil || claim.DurableDispatchID == claimID {
					return false, nil
				}
				used[claim.PolicyID] = true
			}
		}
		for _, op := range run.Operations {
			if op.Name != assistantResearchPolicyOperation || used[op.ID] {
				continue
			}
			var d assistantResearchSourceDisposition
			if readAssistantHostReceipt(op.Intent, &d) != nil || !assistantHostArgumentsMatch(d.Arguments, raw) {
				return false, nil
			}
			expected := binding
			expected.CallbackAlias = ""
			if !reflect.DeepEqual(d.Source, expected) {
				return false, nil
			}
			claim := assistantResearchSourceClaim{PolicyID: op.ID, DurableDispatchID: claimID, CallID: in.CallID, CallbackID: callback}
			if err := appendAssistantResearchAssociation(run, assistantResearchClaimOperation, claim); err != nil {
				return false, err
			}
			prepared = &assistantResearchSourcePrepared{PolicyID: op.ID, Disposition: d}
			return true, nil
		}
		return false, nil
	})
	e.changed(err)
	if err != nil {
		return nil, err
	}
	if prepared == nil {
		return nil, errAssistantRunScope
	}
	return prepared, nil
}

func assistantResearchSourceHostMatches(run assistantRunRecord, host assistantRunOperation, in assistantHostIntent, policyID string) bool {
	policy := assistantDialectOperation(run, policyID)
	if policy == nil || policy.Name != assistantResearchPolicyOperation || in.SourceBinding == nil || in.DialectAction == nil {
		return false
	}
	var d assistantResearchSourceDisposition
	if readAssistantHostReceipt(policy.Intent, &d) != nil || d.ReadOperationID != in.DialectAction.ReadOperationID || !assistantHostArgumentsMatch(d.Arguments, in.Arguments) {
		return false
	}
	binding := *in.SourceBinding
	binding.CallbackAlias = ""
	if !reflect.DeepEqual(binding, d.Source) {
		return false
	}
	count := 0
	for _, op := range run.Operations {
		if op.Name != assistantResearchClaimOperation {
			continue
		}
		var claim assistantResearchSourceClaim
		if readAssistantHostReceipt(op.Intent, &claim) == nil && claim.PolicyID == policyID {
			if claim.DurableDispatchID != in.DurableDispatchID || claim.CallID != in.CallID || claim.CallbackID != in.CallbackID || !assistantDialectAfter(run, host.ID, op.ID) {
				return false
			}
			count++
		}
	}
	return count == 1
}

func validateAssistantResearchSourcePolicies(run assistantRunRecord) error {
	policies := map[string]assistantResearchSourceDisposition{}
	claims := map[string]bool{}
	dispatches := map[string]bool{}
	for _, op := range run.Operations {
		if op.Name != assistantResearchPolicyOperation && op.Name != assistantResearchClaimOperation {
			continue
		}
		if op.Kind != "action" || op.State != "completed" || !bytes.Equal(op.Result, []byte(`{"retained":true}`)) {
			return errAssistantRunInvalid
		}
		if op.Name == assistantResearchPolicyOperation {
			var d assistantResearchSourceDisposition
			if readAssistantDialectCustody(op.Intent, reflect.TypeOf(d)) != nil || readAssistantHostReceipt(op.Intent, &d) != nil || d.Version != 1 || d.RunID != run.ID || d.ExecutionKey != run.Binding.ExecutionKey || !assistantRunDigest(d.SessionScope) || !validResearchRequest(d.Request) || d.Request.RequestKey != "" || d.Request.ChatID != run.Binding.Owner.ID || d.Request.TurnID != run.Binding.RequestID || !validResearchRequestDisposition(d.Allowance) || d.Source.CallbackAlias != "" || run.Binding.Phase != "work" {
				return errAssistantRunInvalid
			}
			if _, ok := assistantHostSourceRecord(&d.Source, assistantResearchTool); !ok {
				return errAssistantRunInvalid
			}
			a, err := readAssistantDialectResearchArguments(d.Arguments)
			value, parseErr := runcontext.ParseJSON(d.Arguments)
			before := assistantRunRecord{Binding: run.Binding}
			for _, earlier := range run.Operations {
				if earlier.ID == op.ID {
					break
				}
				before.Operations = append(before.Operations, earlier)
			}
			readOp, read, readErr := assistantDialectReadOperation(before, assistantResearchTool, a)
			if err != nil || parseErr != nil || runcontext.CanonicalJSON(value) != string(d.Arguments) || readErr != nil || !validAssistantDialectActionRead(read) || read.Recipe.Recipe.Research == nil || readOp.ID != d.ReadOperationID || a.ID != d.Request.PreparationID || a.Revision != d.Request.Revision || a.Revision != read.Revision {
				return errAssistantRunInvalid
			}
			for id := range policies {
				if !claims[id] {
					return errAssistantRunInvalid
				}
			}
			policies[op.ID] = d
			continue
		}
		var claim assistantResearchSourceClaim
		if readAssistantDialectCustody(op.Intent, reflect.TypeOf(claim)) != nil || readAssistantHostReceipt(op.Intent, &claim) != nil || claims[claim.PolicyID] || dispatches[claim.DurableDispatchID] || !assistantRunText(claim.DurableDispatchID, 200) || !assistantRunText(claim.CallID, 256) || claim.CallbackID != "" && !assistantRunText(claim.CallbackID, 200) {
			return errAssistantRunInvalid
		}
		d, ok := policies[claim.PolicyID]
		if !ok {
			return errAssistantRunInvalid
		}
		// Retained claim must still refer to that exact canonical dispatch; its
		// state may have advanced to result/unknown and never reauthorizes work.
		in := assistantHostIntent{Root: true, CallID: claim.CallID, CallbackID: claim.CallbackID, Arguments: d.Arguments, SourceBinding: &d.Source, DurableDispatchID: claim.DurableDispatchID}
		if assistantCanonicalRetainedHost(run, assistantResearchTool, in) == nil || !assistantCanonicalParentHostCall(run, assistantResearchTool, in) {
			return errAssistantRunInvalid
		}
		claims[claim.PolicyID], dispatches[claim.DurableDispatchID] = true, true
	}
	return nil
}

// authority callbacks run while the request owner holds private admission. They
// never reacquire its lock or the assistant ledger.
func (e *assistantRunExecution) assistantResearchSourceAuthority(d assistantResearchSourceDisposition) func(context.Context) error {
	return func(ctx context.Context) error {
		if e.service.server.researchApprovalScope(e.request) != d.SessionScope {
			return errAssistantRunScope
		}
		return e.authorizeAssistantResearchSourceLocked(ctx, d.Source)
	}
}

// Keep the request type at this private seam; no browser/model field can select
// an allowance or elevate a review-only disposition.
func (s *Server) requestResearchWithDisposition(r *http.Request, q inputResearchRequest, authority func(context.Context) error, d inputResearchRequestDisposition) (inputResearchRequestReceipt, error) {
	if authority == nil || !validResearchRequestDisposition(d) {
		return inputResearchRequestReceipt{}, errAssistantRunScope
	}
	return s.requestResearchOwned(r, q, authority, &d)
}
