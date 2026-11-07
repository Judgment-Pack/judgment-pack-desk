package desk

import (
	"bytes"
	"context"
	"encoding/json"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// This is private operation custody, not an approval or a dispatch capability.
// The live Execute wrapper is its only producer. Historical readers validate
// association without consulting (or recreating) current source approval.
type assistantHostSourceBinding struct {
	Version             int             `json:"version"`
	OwnTool             string          `json:"ownTool"`
	WireName            string          `json:"wireName"`
	SourceRecord        json.RawMessage `json:"sourceRecord"`
	SourceDigest        string          `json:"sourceDigest"`
	ApprovalID          string          `json:"approvalId"`
	ApprovalRevision    uint64          `json:"approvalRevision"`
	ConfigurationDigest string          `json:"configurationDigest"`
	ProjectionVersion   string          `json:"projectionVersion"`
	CallbackAlias       string          `json:"callbackAlias"`
}

type assistantHostSourceKey struct{}
type assistantHostCallbackAliasKey struct{}

// Classification precedes Agent's contextual conversion of Effect. ReadingTool
// describes retrieved data (including act results), not permission to use an act
// in Plan, research-choice continuation, or a delegated read scope.
func assistantHostDeclaredRead(t runcontext.HostTool) bool {
	if t.Source != nil {
		return t.Effect == "" && t.Taxonomy != nil && t.Taxonomy.Effect == "read-external" && runcontext.LegacyClass(t.Taxonomy) == "read"
	}
	return t.Effect == "read"
}

func assistantExternalSourceDeclaration(t runcontext.HostTool) bool {
	if t.Source == nil || t.SourceTool != "" || t.Effect != "" || t.Taxonomy == nil || t.ContextPolicy != nil || t.TaxonomyLimit != nil || t.Interaction != "" || len(t.Annotations) != 0 {
		return false
	}
	for _, owner := range assistantSourceOwners() {
		if owner.declaration.Name != t.Name {
			continue
		}
		read := owner.taxonomy.Effect == "read-external"
		if t.Description != owner.declaration.Description || !bytes.Equal(t.InputSchema, owner.declaration.InputSchema) || *t.Taxonomy != *owner.taxonomy || t.EffectClass != runcontext.LegacyClass(owner.taxonomy) || t.Delegation != owner.declaration.Delegation || (read && (t.Approve != nil || t.Approval != "" && t.Approval != "none" || t.Source.VerifyReceipt == nil)) || (!read && (t.Delegation != "" || t.Approval != "required" || t.Approve == nil)) {
			return false
		}
		base := assistantSourceBaseCatalog()
		for _, row := range base {
			if row.AdapterID == owner.id {
				return t.Source.Kind == "custom" && t.Source.ID == owner.id && t.Source.Trust == "first-party" && t.Source.Version == assistantSourceAdapterVersion && t.Source.ToolListDigest == row.ToolListDigest && t.Source.Identity != nil && t.Source.Identity.Kind == "connection" && t.Source.CurrentIdentity != nil && t.Source.GatewayReceipt == nil && t.Source.CurrentDigest == nil && len(t.Source.DescriptorSnapshot) == 0 && t.Source.DescriptorApproval == nil && t.Source.CurrentDescriptorSnapshot == nil
			}
		}
	}
	return false
}

func assistantHostSourceRecord(binding *assistantHostSourceBinding, name string) (contract.ToolOfferSourceRecord, bool) {
	var record contract.ToolOfferSourceRecord
	if binding == nil || binding.Version != 1 || binding.OwnTool != name || !assistantSourceRecordID.MatchString(binding.ApprovalID) || binding.ApprovalRevision != 1 || !assistantRunDigest(binding.ConfigurationDigest) || binding.ProjectionVersion != assistantSourceAdapterVersion || binding.CallbackAlias != "" && !assistantRunText(binding.CallbackAlias, 200) || !uniqueJSON(binding.SourceRecord) {
		return record, false
	}
	// The canonical declaration record has exactly these members. Receipt belongs
	// only to the later accepted result event and cannot appear in an intent.
	members, err := configMembers(binding.SourceRecord)
	if err != nil || len(members) != 8 {
		return record, false
	}
	allowed := map[string]bool{"kind": true, "id": true, "trust": true, "version": true, "toolListDigest": true, "identity": true, "identityDigest": true, "tool": true}
	for _, member := range members {
		if !allowed[member.name] || bytes.Equal(bytes.TrimSpace(binding.SourceRecord[member.start:member.end]), []byte("null")) {
			return record, false
		}
		delete(allowed, member.name)
	}
	// encoding/json's treatment of nested struct members must not erase part
	// of the reviewed source identity. Compare the complete canonical raw
	// record to the package-produced closed record after decoding below.
	if len(allowed) != 0 || json.Unmarshal(binding.SourceRecord, &record) != nil || record.Kind != "custom" || record.Trust != "first-party" || record.Version == nil || *record.Version != binding.ProjectionVersion || record.Tool == nil || *record.Tool != name || record.Identity == nil || record.Identity.Kind != "connection" || record.IdentityDigest == nil || *record.IdentityDigest != runcontext.IdentityDigest(*record.Identity) || record.ToolListDigest == nil || record.Receipt != nil || binding.WireName != runcontext.WireName(record.ID, name) {
		return record, false
	}
	closedRaw, err := json.Marshal(record)
	if err != nil {
		return record, false
	}
	closedValue, err := runcontext.ParseJSON(closedRaw)
	if err != nil || runcontext.CanonicalJSON(closedValue) != string(binding.SourceRecord) {
		return record, false
	}
	for _, row := range assistantSourceBaseCatalog() {
		if row.AdapterID == record.ID && len(row.CompleteDeclarations) == 1 && row.CompleteDeclarations[0].Name == name && *record.ToolListDigest == row.ToolListDigest {
			label := "Desk research HTTP "
			if record.ID == "desk-research-workflow" {
				label = "Desk research workflow "
			}
			value, err := runcontext.ParseJSON(binding.SourceRecord)
			if err != nil || record.Identity.Label != label+binding.ConfigurationDigest || string(binding.SourceRecord) != runcontext.CanonicalJSON(value) || binding.SourceDigest != runcontext.RecordDigest(value) {
				return record, false
			}
			return record, true
		}
	}
	return record, false
}

// This explicit seam is intentionally not called by tool factories in E2.
// E3/E4/E5 must supply real receipt/control/policy behavior before E6 enrolls
// sources. current must re-read the exact current authenticated approval; the
// retained approval below is only a pin and never authorizes a call by itself.
func assistantExternalSourceTool(tool runcontext.HostTool, approval assistantSourceApproval, current func(context.Context) error, verify func(context.Context, runcontext.ReceiptEvidence) (bool, error), policy runcontext.ApprovalPolicy) (runcontext.HostTool, error) {
	if current == nil || tool.Execute == nil || tool.Source != nil || tool.SourceTool != "" || tool.Taxonomy != nil || tool.EffectClass != "" || tool.Approval != "" || tool.Approve != nil || approval.Version != 1 || approval.Historical || approval.RevokedAt != "" || approval.Revision != 1 || !assistantSourceRecordID.MatchString(approval.ID) || !assistantSourceScopeValid(approval.Scope, approval.Scope.Project) || validateAssistantSourceSnapshot(approval.Snapshot, approval.Scope.Project) != nil || approval.Snapshot.Status != "review-required" {
		return runcontext.HostTool{}, errAssistantRunScope
	}
	row := approval.Snapshot
	for _, owner := range assistantSourceOwners() {
		if owner.id != row.AdapterID || tool.Name != owner.declaration.Name {
			continue
		}
		tool.Effect, tool.SourceTool = "", ""
		taxonomy := *owner.taxonomy
		tool.Taxonomy, tool.EffectClass = &taxonomy, runcontext.LegacyClass(&taxonomy)
		tool.Approval, tool.Approve = "none", nil
		if taxonomy.Effect == "act" {
			tool.Approval, tool.Approve = "required", policy
		}
		identity := *row.Identity
		tool.Source = &runcontext.ToolSource{Kind: row.Kind, ID: row.AdapterID, Trust: row.Trust, Version: row.AdapterVersion, ToolListDigest: row.ToolListDigest, Identity: &identity, VerifyReceipt: verify, CurrentIdentity: func(ctx context.Context) (contract.ToolOfferIdentity, error) {
			if err := ctx.Err(); err != nil {
				return contract.ToolOfferIdentity{}, err
			}
			if err := current(ctx); err != nil {
				return contract.ToolOfferIdentity{}, err
			}
			return identity, ctx.Err()
		}}
		if !assistantExternalSourceDeclaration(tool) {
			return runcontext.HostTool{}, errAssistantRunInvalid
		}
		raw, _ := json.Marshal(tool.Source.Record(tool.Name))
		value, err := runcontext.ParseJSON(raw)
		if err != nil {
			return runcontext.HostTool{}, err
		}
		binding := assistantHostSourceBinding{Version: 1, OwnTool: tool.Name, WireName: runcontext.WireName(row.AdapterID, tool.Name), SourceRecord: []byte(runcontext.CanonicalJSON(value)), SourceDigest: runcontext.RecordDigest(value), ApprovalID: approval.ID, ApprovalRevision: approval.Revision, ConfigurationDigest: row.ConfigurationDigest, ProjectionVersion: row.AdapterVersion}
		if _, ok := assistantHostSourceRecord(&binding, tool.Name); !ok {
			return runcontext.HostTool{}, errAssistantRunInvalid
		}
		execute := tool.Execute
		tool.Execute = func(ctx context.Context, args json.RawMessage) (runcontext.ToolResult, error) {
			if err := ctx.Err(); err != nil {
				return runcontext.ToolResult{}, err
			}
			if err := current(ctx); err != nil {
				return runcontext.ToolResult{}, err
			}
			if ctx.Value(assistantHostSourceKey{}) != nil {
				return runcontext.ToolResult{}, errAssistantRunScope
			}
			held := binding
			held.SourceRecord = bytes.Clone(binding.SourceRecord)
			held.CallbackAlias, _ = ctx.Value(assistantHostCallbackAliasKey{}).(string)
			callback, _ := ctx.Value(assistantHostCallbackKey{}).(string)
			if (callback != "") != (held.CallbackAlias != "") {
				return runcontext.ToolResult{}, errAssistantRunScope
			}
			ctx = context.WithValue(ctx, assistantExternalCurrentKey{}, current)
			return execute(context.WithValue(ctx, assistantHostSourceKey{}, held), args)
		}
		return tool, nil
	}
	return runcontext.HostTool{}, errAssistantRunScope
}

// Only actual current source approval admits these external owners to the
// executable offer. Setup/review/changed/revoked status remains in the source
// review API and control; observation here creates neither review nor consent.
func (e *assistantRunExecution) assistantEnrolledSourceTools(ctx context.Context, tools []runcontext.HostTool) ([]runcontext.HostTool, error) {
	approved, err := e.assistantSourceApprovals(ctx)
	if err != nil {
		return nil, err
	}
	owners := map[string]string{}
	for _, owner := range assistantSourceOwners() {
		owners[owner.declaration.Name] = owner.id
	}
	out := make([]runcontext.HostTool, 0, len(tools))
	for _, tool := range tools {
		id, external := owners[tool.Name]
		if !external {
			out = append(out, tool)
			continue
		}
		approval, ok := approved[id]
		if !ok {
			continue
		}
		current := func(ctx context.Context) error {
			return e.assistantSourceApprovalStillCurrent(ctx, approval)
		}
		verify := e.verifyAssistantExternalReadReceipt
		var policy runcontext.ApprovalPolicy
		if tool.Name == assistantResearchTool {
			binding, err := assistantResearchApprovalBinding(approval)
			if err != nil {
				return nil, err
			}
			current = func(ctx context.Context) error {
				lock, err := e.service.server.privateDataLockContext(ctx, true)
				if err != nil {
					return err
				}
				defer lock.Close()
				return e.authorizeAssistantResearchSourceLocked(ctx, binding)
			}
			verify = nil
			policy = e.assistantResearchSourcePolicy(approval)
		}
		enrolled, err := assistantExternalSourceTool(tool, approval, current, verify, policy)
		if err != nil {
			return nil, err
		}
		out = append(out, enrolled)
	}
	return out, nil
}

func assistantLiveHostSource(ctx context.Context, name string) (*assistantHostSourceBinding, error) {
	value := ctx.Value(assistantHostSourceKey{})
	if value == nil {
		return nil, nil
	}
	held, ok := value.(assistantHostSourceBinding)
	if !ok {
		return nil, errAssistantRunScope
	}
	if _, ok = assistantHostSourceRecord(&held, name); !ok {
		return nil, errAssistantRunScope
	}
	held.SourceRecord = bytes.Clone(held.SourceRecord)
	return &held, nil
}

func assistantHostSourceCallMatches(in assistantHostIntent, call *contract.ToolCall) bool {
	if in.SourceBinding == nil {
		return call.Source == nil
	}
	if _, ok := assistantHostSourceRecord(in.SourceBinding, in.SourceBinding.OwnTool); !ok || call.Source == nil {
		return false
	}
	raw, err := json.Marshal(call.Source)
	if err != nil {
		return false
	}
	value, err := runcontext.ParseJSON(raw)
	return err == nil && runcontext.CanonicalJSON(value) == string(in.SourceBinding.SourceRecord)
}
