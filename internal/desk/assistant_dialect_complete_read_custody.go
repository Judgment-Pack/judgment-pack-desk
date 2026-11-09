package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// A saved selector is data. Only the original dispatched producer below may
// reserve/acknowledge a new root; decoding cannot recreate that admission.
type assistantDialectCompleteReadSelector struct {
	Version         int                 `json:"version"`
	ReadOperationID string              `json:"readOperationId"`
	Reference       retainedArtifactRef `json:"reference"`
	Receipt         string              `json:"receipt"`
	PackDigest      string              `json:"packDigest"`
	ResultSHA256    string              `json:"resultSha256"`
}
type assistantDialectCompleteReadIntent struct {
	Reference    retainedArtifactRef `json:"reference"`
	Receipt      string              `json:"receipt"`
	ResultSHA256 string              `json:"resultSha256"`
}
type assistantDialectCompleteReadCustody struct {
	ResearchIndex   *runcontext.DialectResearchIndex    `json:"researchIndex,omitempty"`
	Version         int                                 `json:"version"`
	ReadOperationID string                              `json:"readOperationId"`
	ResourceDigest  string                              `json:"resourceDigest"`
	InputID         string                              `json:"inputId"`
	Revision        string                              `json:"revision"`
	PackKey         string                              `json:"packKey"`
	PackPath        string                              `json:"packPath"`
	Declaration     string                              `json:"declaration"`
	PackDigest      string                              `json:"packDigest"`
	MetadataSHA256  string                              `json:"metadataSha256"`
	Intent          assistantDialectCompleteReadIntent  `json:"intent"`
	Receipt         *assistantDialectCompleteReadIntent `json:"receipt,omitempty"`
	Uncertainty     string                              `json:"uncertainty,omitempty"`
}

func assistantDialectCompleteReadOf(run *assistantRunRecord, host string) *assistantDialectCompleteReadCustody {
	for i := range run.DialectReads {
		if run.DialectReads[i].ReadOperationID == host {
			return &run.DialectReads[i]
		}
	}
	return nil
}
func assistantDialectCompleteReadRoot(run assistantRunRecord, host string) (assistantRunOperation, assistantHostIntent, error) {
	var in assistantHostIntent
	op := assistantDialectOperation(run, host)
	if op == nil || op.Kind != "host" || op.Name != "read_dialect" || readAssistantHostReceipt(op.Intent, &in) != nil || !in.Root || in.SourceBinding != nil || in.ExternalScope != nil || in.DialectAction != nil || run.Binding.Owner.Kind != "chat" || !contains([]string{"plan", "work"}, run.Binding.Phase) || validateAssistantDialectIntent(run, *op, in) != nil {
		return assistantRunOperation{}, in, errAssistantRunScope
	}
	args, err := readAssistantDialectArguments("read_dialect", in.Arguments)
	if err != nil || args.Cursor != nil {
		return assistantRunOperation{}, in, errAssistantRunScope
	}
	return *op, in, nil
}
func assistantDialectCompleteReadReceipt(run assistantRunRecord, held assistantDialectCompleteReadCustody, in assistantHostIntent) string {
	op := assistantDialectOperation(run, held.ReadOperationID)
	if op == nil {
		return ""
	}
	base := "drr-" + digestOf(assistantChatJSON([]any{"dialect-complete-read/1", run.Binding, run.ID, held.ReadOperationID, op.Intent, in, held.ResourceDigest, held.InputID, held.Revision, held.PackKey, held.PackPath, held.Declaration, held.PackDigest, held.MetadataSHA256, held.Intent.Reference, held.Intent.ResultSHA256}))
	if held.ResearchIndex != nil {
		return "drr-" + digestOf(assistantChatJSON([]any{"dialect-complete-read-index/1", base, held.ResearchIndex}))
	}
	return base
}
func validateAssistantDialectCompleteReadCustody(run assistantRunRecord) error {
	if len(run.DialectReads) > assistantRunOperationLimit {
		return errAssistantRunInvalid
	}
	seen := map[string]bool{}
	for _, held := range run.DialectReads {
		op, in, err := assistantDialectCompleteReadRoot(run, held.ReadOperationID)
		args, e := readAssistantDialectArguments("read_dialect", in.Arguments)
		path, p := wireRelativePath(held.PackPath)
		if err != nil || e != nil || held.Version != 1 || seen[held.ReadOperationID] || args.ID != held.InputID || args.PackKey != held.PackKey || !assistantRunDigest(held.Revision) || held.ResourceDigest != assistantDialectResource(held.InputID, held.PackKey) || p != nil || path != held.PackPath || !assistantRunDigest(held.Declaration) || !assistantDialectPackageDigest(held.PackDigest) || !assistantRunDigest(held.MetadataSHA256) || !held.Intent.Reference.valid() || held.Intent.Reference.Bytes < 1 || !assistantRunDigest(held.Intent.ResultSHA256) || held.Intent.Receipt != assistantDialectCompleteReadReceipt(run, held, in) || held.Receipt != nil && !reflect.DeepEqual(held.Receipt, &held.Intent) || held.Uncertainty != "" && !contains([]string{"storage-unacknowledged", "ack-mismatch", "cancelled-after-intent"}, held.Uncertainty) {
			return errAssistantRunInvalid
		}
		if held.ResearchIndex != nil && validateAssistantDialectCompleteIndex(held.ResearchIndex) != nil {
			return errAssistantRunInvalid
		}
		seen[held.ReadOperationID] = true
		if op.State == "completed" {
			var result assistantHostResult
			if held.Receipt == nil || held.Uncertainty != "" || readAssistantHostReceipt(op.Result, &result) != nil || result.DialectRead == nil || !assistantDialectCompleteSelectorMatches(*result.DialectRead, held) || digestOf(result.Result) != held.Intent.ResultSHA256 {
				return errAssistantRunInvalid
			}
			if held.ResearchIndex != nil && validateAssistantDialectCompleteIndexResult(held.ResearchIndex, result.Result) != nil {
				return errAssistantRunInvalid
			}
		}
	}
	return nil
}
func assistantDialectCompleteSelectorMatches(read assistantDialectRead, held assistantDialectCompleteReadCustody) bool {
	c := read.Complete
	return c != nil && c.Version == 1 && c.ReadOperationID == held.ReadOperationID && c.Reference == held.Intent.Reference && c.Receipt == held.Intent.Receipt && c.ResultSHA256 == held.Intent.ResultSHA256 && c.PackDigest == held.PackDigest && read.ID == held.InputID && read.Revision == held.Revision && read.PackKey == held.PackKey && read.PackPath == held.PackPath && read.Declaration == held.Declaration && digestOf(read.View) == held.MetadataSHA256
}
func readAssistantDialectCompleteReadCustody(raw []byte) error {
	if err := readAssistantDialectFactoryShape(raw, reflect.TypeOf([]assistantDialectCompleteReadCustody{}), false, 1); err != nil {
		return err
	}
	rows, err := assistantChatArray(raw)
	if err != nil {
		return err
	}
	for _, row := range rows {
		fields, err := assistantTestObject(row)
		if err != nil {
			return err
		}
		if index := fields["researchIndex"]; index != nil && assistantDialectCompleteShape(index, reflect.TypeOf(runcontext.DialectResearchIndex{}), false) != nil {
			return errAssistantRunInvalid
		}
	}
	return nil
}
func validAssistantDialectCompleteReadSelector(read assistantDialectRead) bool {
	c := read.Complete
	if c == nil || c.Version != 1 || read.Recipe != nil || read.Variant != "" || read.BasePublication != "" || read.Pack != nil || !inputPreparationID.MatchString(read.ID) || !assistantRunDigest(read.Revision) || !assistantDialectRecipeText(read.Name, 160) || !retainedSafeVersion(read.RecipeVersion) || !assistantDialectRecipeText(read.PackKey, 512) || !assistantRunDigest(read.Declaration) || !assistantRunText(c.ReadOperationID, 128) || !c.Reference.valid() || c.Reference.Bytes < 1 || !assistantRunText(c.Receipt, 80) || !assistantDialectPackageDigest(c.PackDigest) || !assistantRunDigest(c.ResultSHA256) {
		return false
	}
	path, err := wireRelativePath(read.PackPath)
	if err != nil || path != read.PackPath {
		return false
	}
	fields, err := assistantDialectCompleteObject(read.View, []string{"columns", "recordCount"})
	if err != nil {
		return false
	}
	var count int
	if json.Unmarshal(fields["recordCount"], &count) != nil || count < 0 || count > inputRecordLimit {
		return false
	}
	columns, err := assistantChatArray(fields["columns"])
	if err != nil || len(columns) > runcontext.DialectColumns {
		return false
	}
	seen := map[string]bool{}
	for _, raw := range columns {
		f, e := assistantDialectCompleteObject(raw, []string{"name"})
		if e != nil {
			return false
		}
		name, e := assistantTestString(f["name"])
		if e != nil || seen[name] || !assistantDialectRecipeText(name, 1024) || contains([]string{"constructor", "prototype", "__proto__"}, name) {
			return false
		}
		seen[name] = true
	}
	return true
}
func assistantDialectCompleteReadLiveRun(run assistantRunRecord) bool {
	return !run.Historical && run.State == "running" && run.Binding.Owner.Kind == "chat" && contains([]string{"plan", "work"}, run.Binding.Phase) && run.Checkpoint != nil && run.Checkpoint.Status == "running" && run.Observation == nil
}

// Fence only an unresolved physical object shared by this original owner and
// namespace. A fresh capture/operation cannot replay the earlier operation; its
// ordinary original admission remains mandatory. Observation alone never clears
// this promise: the original accounting adapter must supply future settlement.
func assistantDialectCompleteReadFence(ledger assistantRunLedger, current assistantRunRecord, host, owner string, ref retainedArtifactRef) error {
	if !assistantRunDigest(owner) || !ref.valid() {
		return errAssistantRunScope
	}
	for _, run := range ledger.Runs {
		if run.DialectArtifactOwner != owner || run.Binding.Project != current.Binding.Project {
			continue
		}
		for _, held := range run.DialectReads {
			if held.Intent.Reference != ref {
				continue
			}
			if run.ID == current.ID && held.ReadOperationID == host {
				continue
			}
			op := assistantDialectOperation(run, held.ReadOperationID)
			if held.Receipt == nil || held.Uncertainty != "" || op == nil || op.State != "completed" {
				return errAssistantRunScope
			}
		}
	}
	return nil
}

func (v assistantDialectCompleteRead) result(ctx context.Context) (runcontext.ToolResult, error) {
	tools := runcontext.NewDialectTools(runcontext.DialectAuthoringPorts{Read: func(context.Context, runcontext.DialectReadRequest) (runcontext.DialectSnapshot, error) {
		return v.Snapshot, nil
	}, ReadFieldIndex: v.fieldIndex, ReadDeclarations: v.declarations})
	raw, _ := json.Marshal(runcontext.DialectReadRequest{ID: v.Snapshot.ID, PackKey: v.Snapshot.PackKey})
	result, err := tools[0].Execute(ctx, raw)
	if err != nil || result.Err() {
		return runcontext.ToolResult{}, errors.Join(errAssistantDialectCompleteRead, err)
	}
	return result, nil
}
func assistantDialectCompleteReadMetadata(snapshot runcontext.DialectSnapshot) []byte {
	columns := []map[string]string{}
	for _, column := range snapshot.Columns {
		columns = append(columns, map[string]string{"name": column.Name})
	}
	return assistantChatJSON(map[string]any{"columns": columns, "recordCount": snapshot.RecordCount})
}

// Only exact concrete read admissions can change these append-only slots.
// Neither a JSON receipt nor a user/provider callback is an admission proof.
func validateAssistantDialectCompleteReadTransition(prior, run assistantRunRecord, a *assistantDialectArtifactOwnerAdmission) (bool, error) {
	if len(run.DialectReads) < len(prior.DialectReads) {
		return false, errAssistantRunScope
	}
	changed := false
	for i, was := range prior.DialectReads {
		now := run.DialectReads[i]
		if !reflect.DeepEqual(was.Intent, now.Intent) || was.Version != now.Version || was.ReadOperationID != now.ReadOperationID {
			return false, errAssistantRunScope
		}
		original, updated := was, now
		original.Receipt, updated.Receipt = nil, nil
		original.Uncertainty, updated.Uncertainty = "", ""
		if !reflect.DeepEqual(original, updated) || was.Receipt != nil && !reflect.DeepEqual(was.Receipt, now.Receipt) || was.Uncertainty != "" && was.Uncertainty != now.Uncertainty {
			return false, errAssistantRunScope
		}
		if !reflect.DeepEqual(was.Receipt, now.Receipt) {
			if a == nil || a.read == nil || a.readStage != "ack" || !reflect.DeepEqual(*a.read, now) {
				return false, errAssistantRunScope
			}
			changed = true
		}
	}
	for _, held := range run.DialectReads[len(prior.DialectReads):] {
		if a == nil || a.read == nil || a.readStage != "intent" || !reflect.DeepEqual(*a.read, held) || held.Receipt != nil || held.Uncertainty != "" {
			return false, errAssistantRunScope
		}
		changed = true
	}
	if changed && (a == nil || a.owner != run.DialectArtifactOwner || a.runID != run.ID || a.host != a.read.ReadOperationID || !reflect.DeepEqual(a.binding, prior.Binding) || !reflect.DeepEqual(a.binding, run.Binding)) {
		return false, errAssistantRunScope
	}
	return changed, nil
}

func assistantDialectCompleteResultBytes(result runcontext.ToolResult) []byte {
	return bytes.Clone(result.Value().JSON())
}
