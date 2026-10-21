package desk

import (
	"bytes"
	"encoding/json"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
	"reflect"
	"strings"
	"time"
)

type assistantDialectActionMetadata struct {
	ID              string `json:"id"`
	CreatedAt       string `json:"createdAt"`
	AfterTurnID     string `json:"afterTurnId"`
	ReadOperationID string `json:"readOperationId"`
}
type assistantDialectCheckpoint struct {
	Document string          `json:"document"`
	Report   json.RawMessage `json:"report"`
}
type assistantDialectAction struct {
	Patch         *assistantDialectRecipePatch  `json:"patch,omitempty"`
	Section       string                        `json:"section,omitempty"`
	RecordIDs     []string                      `json:"recordIds,omitempty"`
	RunID         string                        `json:"runId,omitempty"`
	Version       int                           `json:"version"`
	ID            string                        `json:"id"`
	CreatedAt     string                        `json:"createdAt"`
	AfterTurnID   string                        `json:"afterTurnId"`
	InputID       string                        `json:"inputId"`
	Name          string                        `json:"name"`
	Revision      string                        `json:"revision"`
	RecipeVersion int                           `json:"recipeVersion"`
	Kind          string                        `json:"kind"`
	Dialect       *assistantDialectMapping      `json:"dialect,omitempty"`
	DialectCheck  *assistantDialectCheckpoint   `json:"dialectCheck,omitempty"`
	Publication   *assistantPublicationProposal `json:"publication,omitempty"`
}
type assistantDialectPreviewIssue struct {
	RecordID string                 `json:"recordId"`
	Issues   []inputConversionIssue `json:"issues"`
}
type assistantDialectPreview struct {
	Status            string                         `json:"status"`
	RecordCount       int                            `json:"recordCount"`
	RecordsWithIssues int                            `json:"recordsWithIssues"`
	Issues            []assistantDialectPreviewIssue `json:"issues"`
}
type assistantDialectActionOutput struct {
	ActiveResearch *assistantDialectResearchSummary `json:"activeResearch,omitempty"`
	Read           assistantDialectRead             `json:"read"`
	CheckID        string                           `json:"checkId"`
	MappingCheckID string                           `json:"mappingCheckId,omitempty"`
	Action         *assistantDialectAction          `json:"action,omitempty"`
	Published      *dialectPublicationAnswer        `json:"published,omitempty"`
	Preview        *assistantDialectPreview         `json:"preview,omitempty"`
	Ready          bool                             `json:"ready"`
}
type assistantDialectPreparedKey struct{}

func assistantDialectActionID(binding assistantRunBinding, in assistantHostIntent) string {
	return "dialect-action-" + digestOf([]byte(binding.ExecutionKey + "\x00" + in.CallID + "\x00" + in.CallbackID))[:40]
}
func assistantDialectOperationID(in assistantHostIntent) string {
	in.DialectAction = nil
	raw, _ := json.Marshal(in)
	return "dialect-host-" + digestOf(raw)[:32]
}
func assistantDialectReadOperation(run assistantRunRecord, name string, a assistantDialectArguments) (assistantRunOperation, assistantDialectRead, error) {
	readName := "read_dialect"
	if name == "propose_dialect_action" {
		readName = "read_dialect_recipe"
	}
	if name == "propose_dialect_publication" {
		readName = "read_dialect_publication"
	}
	for i := len(run.Operations) - 1; i >= 0; i-- {
		op := run.Operations[i]
		if op.Kind != "host" || op.Name != readName || op.State != "completed" {
			continue
		}
		var in assistantHostIntent
		var result assistantHostResult
		if readAssistantHostReceipt(op.Intent, &in) != nil || !in.Root || readAssistantHostReceipt(op.Result, &result) != nil || result.DialectRead == nil {
			continue
		}
		read := *result.DialectRead
		rawFields, rawErr := assistantTestObject(op.Result)
		if rawErr != nil || readAssistantDialectCustody(rawFields["dialectRead"], reflect.TypeOf(read)) != nil {
			return assistantRunOperation{}, assistantDialectRead{}, errAssistantRunInvalid
		}
		if read.ID == a.ID && read.PackKey == a.PackKey && read.Variant == a.Variant {
			return op, read, nil
		}
	}
	return assistantRunOperation{}, assistantDialectRead{}, errAssistantRunScope
}
func prepareAssistantDialectAction(run assistantRunRecord, name string, in *assistantHostIntent, sourceCount int) error {
	a, err := readAssistantDialectArguments(name, in.Arguments)
	if err != nil {
		return err
	}
	if !in.Root || run.Binding.Phase != "work" || sourceCount < 0 || sourceCount >= 32 || !assistantDialectRecipeText(run.Binding.RequestID, 160) {
		return errAssistantRunScope
	}
	count := 0
	for _, op := range run.Operations {
		if op.Kind == "host" && (assistantDialectProposal(op.Name)) {
			count++
		}
	}
	if count >= 16 || sourceCount+count >= 32 {
		return errAssistantRunInvalid
	}
	op, read, err := assistantDialectReadOperation(run, name, a)
	if err != nil || read.Revision != a.Revision {
		return errAssistantRunScope
	}
	in.DialectAction = &assistantDialectActionMetadata{assistantDialectActionID(run.Binding, *in), time.Now().UTC().Format(time.RFC3339Nano), run.Binding.RequestID, op.ID}
	return nil
}
func validateAssistantDialectIntent(run assistantRunRecord, op assistantRunOperation, in assistantHostIntent) error {
	if in.Policy != nil || in.RecipeProposal != nil {
		return errAssistantRunInvalid
	}
	a, err := readAssistantDialectArguments(op.Name, in.Arguments)
	if err != nil {
		return err
	}
	if op.ID != assistantDialectOperationID(in) {
		return errAssistantRunInvalid
	}
	if !assistantDialectProposal(op.Name) {
		if in.DialectAction != nil {
			return errAssistantRunInvalid
		}
		return nil
	}
	meta := in.DialectAction
	if meta == nil || !in.Root || run.Binding.Phase != "work" || meta.ID != assistantDialectActionID(run.Binding, in) || meta.AfterTurnID != run.Binding.RequestID || !assistantDialectRecipeText(meta.AfterTurnID, 160) {
		return errAssistantRunInvalid
	}
	if _, err := time.Parse(time.RFC3339Nano, meta.CreatedAt); err != nil {
		return errAssistantRunInvalid
	}
	before := assistantRunRecord{Binding: run.Binding}
	for _, earlier := range run.Operations {
		if earlier.ID == op.ID {
			break
		}
		before.Operations = append(before.Operations, earlier)
	}
	readOp, read, err := assistantDialectReadOperation(before, op.Name, a)
	if err != nil || readOp.ID != meta.ReadOperationID || read.Revision != a.Revision {
		return errAssistantRunInvalid
	}
	return nil
}
func assistantDialectReserved(raw []byte, key string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	for name := range fields {
		if strings.EqualFold(name, key) {
			return true
		}
	}
	return false
}
func validAssistantDialectRead(read assistantDialectRead) bool {
	if read.Recipe != nil {
		return validAssistantDialectActionRead(read)
	}
	if !inputPreparationID.MatchString(read.ID) || !assistantDialectRecipeText(read.Name, 160) || !assistantRunDigest(read.Revision) || read.RecipeVersion < 1 || (!validUTF8(read.View) || jsonscalar.ValidateEscapes(read.View) != nil) {
		return false
	}
	fields, err := assistantTestObject(read.View)
	if err != nil {
		return false
	}
	if !validAssistantDialectView(read, fields) {
		return false
	}
	if string(fields["id"]) != string(assistantChatJSON(read.ID)) || string(fields["revision"]) != string(assistantChatJSON(read.Revision)) || string(fields["name"]) != string(assistantChatJSON(read.Name)) || string(fields["recipeVersion"]) != string(assistantChatJSON(read.RecipeVersion)) {
		return false
	}
	if read.Variant != "" {
		if !contains([]string{"asserted", "generated"}, read.Variant) || read.Pack != nil || read.PackKey != "" || read.PackPath != "" || read.Declaration != "" || read.BasePublication != "" && !assistantRunDigest(read.BasePublication) {
			return false
		}
		if string(fields["variant"]) != string(assistantChatJSON(read.Variant)) {
			return false
		}
	} else {
		if read.BasePublication != "" || !assistantDialectRecipeText(read.PackKey, 512) || !assistantRunDigest(read.Declaration) || !json.Valid(read.Pack) || len(read.Pack) > 1<<20 {
			return false
		}
		path, err := wireRelativePath(read.PackPath)
		if err != nil || path != read.PackPath {
			return false
		}
		if string(fields["packKey"]) != string(assistantChatJSON(read.PackKey)) || string(fields["packDigest"]) != string(assistantChatJSON("sha256:"+digestOf(read.Pack))) || string(fields["pack"]) != string(assistantChatJSON(string(read.Pack))) {
			return false
		}
	}
	return true
}
func validateAssistantDialectReadReceipt(op assistantRunOperation, in assistantHostIntent, result assistantHostResult) error {
	if result.DialectRead == nil {
		return nil
	}
	read := *result.DialectRead
	rawFields, rawErr := assistantTestObject(op.Result)
	if rawErr != nil || readAssistantDialectCustody(rawFields["dialectRead"], reflect.TypeOf(read)) != nil {
		return errAssistantRunInvalid
	}
	if op.State != "completed" || !assistantDialectTool(op.Name) && op.Name != "read_dialect_recipe" || op.Name != "read_dialect" && op.Name != "read_dialect_publication" && op.Name != "read_dialect_recipe" || !validAssistantDialectRead(read) || result.Tests != nil || result.Read != nil || result.PolicySource != nil {
		return errAssistantRunInvalid
	}
	var a assistantDialectArguments
	var err error
	if op.Name == "read_dialect_recipe" {
		var v assistantDialectRecipeArguments
		v, err = readAssistantDialectRecipeArguments(op.Name, in.Arguments)
		a.ID = v.ID
		if read.Recipe == nil {
			return errAssistantRunInvalid
		}
	} else {
		a, err = readAssistantDialectArguments(op.Name, in.Arguments)
		if read.Recipe != nil {
			return errAssistantRunInvalid
		}
	}
	if err != nil || a.ID != read.ID || a.PackKey != read.PackKey || a.Variant != read.Variant {
		return errAssistantRunInvalid
	}
	expected, err := assistantHostJSON(json.RawMessage(read.View))
	if err != nil || !bytes.Equal(expected.Value().JSON(), result.Result) {
		return errAssistantRunInvalid
	}
	return nil
}
func newAssistantDialectAction(meta assistantDialectActionMetadata, read assistantDialectRead, kind string) *assistantDialectAction {
	return &assistantDialectAction{Version: 1, ID: meta.ID, CreatedAt: meta.CreatedAt, AfterTurnID: meta.AfterTurnID, InputID: read.ID, Name: read.Name, Revision: read.Revision, RecipeVersion: read.RecipeVersion, Kind: kind}
}

// Existing actions stay byte-preserved, including applied state. Exact repeated
// output can acknowledge its existing action, but never replace its contents.
func appendAssistantDialectActions(chat map[string]json.RawMessage, outputs []assistantDomainOutput) error {
	rows := []json.RawMessage{}
	if chat["inputActions"] != nil {
		var err error
		rows, err = assistantChatArray(chat["inputActions"])
		if err != nil {
			return err
		}
	}
	index := map[string]int{}
	for i, row := range rows {
		fields, err := assistantTestObject(row)
		if err != nil {
			return err
		}
		id, err := assistantTestString(fields["id"])
		if err != nil {
			return err
		}
		if _, ok := index[id]; ok {
			return errAssistantRunConflict
		}
		index[id] = i
	}
	for _, out := range outputs {
		if out.Kind != "dialect-action" || out.DialectAction == nil || out.DialectAction.Action == nil {
			continue
		}
		action := out.DialectAction.Action
		raw, err := json.Marshal(action)
		if err != nil {
			return err
		}
		if at, ok := index[action.ID]; ok {
			fields, err := assistantTestObject(rows[at])
			if err != nil {
				return err
			}
			delete(fields, "applied")
			// A typed zero cannot turn an explicit null into an omitted
			// optional member of the new review-only action variants.
			if out.DialectAction.Read.Recipe != nil {
				if err = readAssistantDialectCustody(assistantChatEncodeObject(fields), reflect.TypeOf(*action)); err != nil {
					return err
				}
			}
			if _, err = assistantTestControls(assistantChatEncodeObject(fields), "version", "id", "createdAt", "afterTurnId", "inputId", "name", "revision", "recipeVersion", "kind", "dialect", "dialectCheck", "publication", "patch", "section", "recordIds", "runId"); err != nil {
				return err
			}
			if !assistantHostArgumentsMatch(assistantChatEncodeObject(fields), raw) {
				// Member order of existing browser actions is display serialization, not
				// identity. Compare the finite action fields with exact typed values.
				var held assistantDialectAction
				if json.Unmarshal(assistantChatEncodeObject(fields), &held) != nil || !reflect.DeepEqual(held, *action) {
					return errAssistantRunConflict
				}
			}
			continue
		}
		if len(rows) >= 32 {
			return errAssistantRunInvalid
		}
		index[action.ID] = len(rows)
		rows = append(rows, raw)
	}
	if len(rows) > 0 || chat["inputActions"] != nil {
		chat["inputActions"] = assistantChatEncodeArray(rows)
	}
	return nil
}

func assistantDialectOperation(run assistantRunRecord, id string) *assistantRunOperation {
	for i := range run.Operations {
		if run.Operations[i].ID == id {
			return &run.Operations[i]
		}
	}
	return nil
}
func readAssistantDialectCheck(run assistantRunRecord, id, host string) (assistantDialectCheckIntent, assistantDialectCheckResult, dialectAuthoringReport, error) {
	var in assistantDialectCheckIntent
	var result assistantDialectCheckResult
	var report dialectAuthoringReport
	op := assistantDialectOperation(run, id)
	if op == nil || op.Kind != "runtime" || op.Name != "dialect/check" || op.State != "completed" || readAssistantHostReceipt(op.Intent, &in) != nil || in.HostOperationID != host || readAssistantHostReceipt(op.Result, &result) != nil || result.Unavailable || !assistantDialectAfter(run, id, host) || !validAssistantDialectReport(result.Report) {
		return in, result, report, errAssistantRunInvalid
	}
	var err error
	report, err = readDialectAuthoringReport(in.Document, string(in.Pack), result.Report)
	return in, result, report, err
}
func validateAssistantDialectOutput(run assistantRunRecord, out assistantDomainOutput, receipts map[string]assistantHostResult) error {
	p := out.DialectAction
	if out.Kind != "dialect-action" || p == nil || out.Recipe != nil || out.TestProposal != nil || out.PolicyUpdate != nil || out.PolicyInterpretation != nil || out.PolicySource != nil || !reflect.DeepEqual(out.Draft, graphDraft{}) || out.RunID != run.ID || out.RequestKey != run.Binding.ExecutionKey || !validAssistantDialectRead(p.Read) {
		return errAssistantRunInvalid
	}
	producer := assistantDialectOperation(run, out.OperationID)
	if producer == nil || producer.Kind != "host" || !assistantDialectProposal(producer.Name) || producer.State != "completed" && producer.State != "refused" {
		return errAssistantRunInvalid
	}
	var in assistantHostIntent
	if readAssistantHostReceipt(producer.Intent, &in) != nil || in.DialectAction == nil || in.CallID != out.CallID || validateAssistantDialectIntent(run, *producer, in) != nil {
		return errAssistantRunInvalid
	}
	a, err := readAssistantDialectArguments(producer.Name, in.Arguments)
	if err != nil {
		return err
	}
	readReceipt, ok := receipts[in.DialectAction.ReadOperationID]
	if !ok || readReceipt.DialectRead == nil || !reflect.DeepEqual(*readReceipt.DialectRead, p.Read) {
		return errAssistantRunInvalid
	}
	if producer.Name == "propose_dialect_action" {
		return validateAssistantDialectReviewOutput(run, out, receipts, *producer, in)
	}
	if p.Read.Recipe != nil || p.ActiveResearch != nil || p.Action != nil && (p.Action.Patch != nil || p.Action.Section != "" || p.Action.RecordIDs != nil || p.Action.RunID != "") {
		return errAssistantRunInvalid
	}
	if p.Action != nil {
		kind := "update-dialect"
		if producer.Name == "propose_dialect_publication" {
			kind = "publish-dialect"
		}
		base := newAssistantDialectAction(*in.DialectAction, p.Read, kind)
		held := *p.Action
		held.Dialect = nil
		held.DialectCheck = nil
		held.Publication = nil
		if !reflect.DeepEqual(*base, held) || p.Published != nil {
			return errAssistantRunInvalid
		}
	}
	if producer.Name == "propose_dialect" {
		if p.Action == nil || p.Action.Dialect == nil || p.Action.DialectCheck == nil || p.Action.Publication != nil || p.Published != nil || p.Preview == nil || p.MappingCheckID != "" || !reflect.DeepEqual(*p.Action.Dialect, a.mapping()) {
			return errAssistantRunInvalid
		}
		checked, result, report, err := readAssistantDialectCheck(run, p.CheckID, producer.ID)
		if err != nil || !bytes.Equal(checked.Pack, p.Read.Pack) || string(checked.Document) != p.Action.DialectCheck.Document || !assistantHostArgumentsMatch(result.Report, p.Action.DialectCheck.Report) {
			return errAssistantRunInvalid
		}
		// Rebuild only declarative structure, never values or an evaluator. Exact
		// columns from the retained safe read establish the supported binding set.
		fields, err := assistantTestObject(p.Read.View)
		if err != nil {
			return err
		}
		columns, err := assistantChatArray(fields["columns"])
		if err != nil {
			return err
		}
		doc := inputPreparation{Records: []preparedInputRecord{{Values: map[string]string{}}}}
		for _, raw := range columns {
			c, err := assistantTestObject(raw)
			if err != nil {
				return err
			}
			name, err := assistantTestString(c["name"])
			if err != nil {
				return err
			}
			doc.Records[0].Values[name] = ""
		}
		q, err := assistantDialectQualification(doc, a, p.Read)
		if err != nil {
			return err
		}
		expected, _ := assistantDialectMappingDocument(q.Mapping)
		if !bytes.Equal(expected, checked.Document) {
			return errAssistantRunInvalid
		}
		var recordCount int
		if json.Unmarshal(fields["recordCount"], &recordCount) != nil || report.accepted(true) && p.Preview.RecordCount != recordCount {
			return errAssistantRunInvalid
		}
		preview := p.Preview
		if !contains([]string{"complete", "unavailable", "not-started"}, preview.Status) || preview.RecordCount < 0 || preview.RecordCount > 1000 || preview.RecordsWithIssues < 0 || preview.RecordsWithIssues > preview.RecordCount || preview.Issues == nil || len(preview.Issues) > 5 || p.Ready != (report.accepted(true) && preview.Status == "complete") {
			return errAssistantRunInvalid
		}
		for _, row := range preview.Issues {
			if row.RecordID == "" || row.Issues == nil || len(row.Issues) > 10 {
				return errAssistantRunInvalid
			}
			for _, issue := range row.Issues {
				if issue.Target == "" || issue.Message == "" {
					return errAssistantRunInvalid
				}
			}
		}
		if !report.accepted(true) && (preview.Status != "not-started" || preview.RecordCount != 0 || preview.RecordsWithIssues != 0 || len(preview.Issues) != 0) {
			return errAssistantRunInvalid
		}
	} else {
		if p.Preview != nil {
			return errAssistantRunInvalid
		}
		outer := assistantDialectOperation(run, p.CheckID)
		var request assistantDialectPublicationIntent
		var result assistantDialectPublicationResult
		if outer == nil || outer.Kind != "runtime" || outer.Name != "dialect-publication/check" || outer.State != "completed" || !assistantDialectAfter(run, outer.ID, producer.ID) || readAssistantHostReceipt(outer.Intent, &request) != nil || readAssistantHostReceipt(outer.Result, &result) != nil || result.Status != 200 || result.Answer == nil || request.HostOperationID != producer.ID || !reflect.DeepEqual(request.Read, p.Read) || request.CreatedAt != in.DialectAction.CreatedAt || p.MappingCheckID != result.MappingCheckID {
			return errAssistantRunInvalid
		}
		expected := dialectPublicationRequest{Action: "check", Revision: a.Revision, Variant: a.Variant, Version: a.Version, Impact: a.Impact}
		if !reflect.DeepEqual(expected, request.Request) {
			return errAssistantRunInvalid
		}
		answer := result.Answer
		pub := answer.Publication
		if pub.Dialect == nil || !pub.Dialect.valid(pub) || pub.ID != publicationID(pub) || pub.Dialect.PreparationID != a.ID || pub.Dialect.Variant != a.Variant || len(pub.Documents) != 1 || pub.Documents[0].ID != dialectArtifactID(a.ID, a.Variant) || pub.Documents[0].Version != a.Version || answer.Digest != "sha256:"+digestOf([]byte(answer.Document)) || pub.Documents[0].Content != artifactReference([]byte(answer.Document)) {
			return errAssistantRunInvalid
		}
		if answer.Saved {
			if p.Action != nil || p.Published == nil || !reflect.DeepEqual(*p.Published, *answer) || p.Ready {
				return errAssistantRunInvalid
			}
		} else {
			if p.Action == nil || p.Action.Publication == nil || p.Action.Dialect != nil || p.Action.DialectCheck != nil || p.Published != nil || !p.Ready || answer.Review == nil || !answer.Review.valid() || answer.BasePublication != p.Read.BasePublication || pub.At != in.DialectAction.CreatedAt || pub.Dialect.Revision != a.Revision || pub.Dialect.Request != dialectPublicationRequestID(a.ID, expected) {
				return errAssistantRunInvalid
			}
			expectedProposal := assistantPublicationProposal{a.Variant, a.Version, a.Impact, *answer}
			if !reflect.DeepEqual(expectedProposal, *p.Action.Publication) {
				return errAssistantRunInvalid
			}
			check, _, report, err := readAssistantDialectCheck(run, p.MappingCheckID, producer.ID)
			if err != nil || !assistantDialectAfter(run, p.MappingCheckID, outer.ID) || !report.accepted(true) || report.Document != answer.Document || report.Digest != answer.Digest || artifactReference(check.Pack) != pub.Dialect.Pack {
				return errAssistantRunInvalid
			}
		}
	}
	result, ok := receipts[producer.ID]
	if !ok || result.DialectRead != nil || result.Tests != nil || result.Read != nil || result.PolicySource != nil {
		return errAssistantRunInvalid
	}
	expected, err := assistantDialectActionResult(p)
	if err != nil || !bytes.Equal(expected.Value().JSON(), result.Result) || (producer.State == "refused") != expected.Err() {
		return errAssistantRunInvalid
	}
	return nil
}

func assistantDialectResultHasAction(raw []byte) bool {
	value, err := runcontext.ParseJSON(raw)
	if err != nil {
		return false
	}
	tool := runcontext.ReadToolResult(value)
	if len(tool.Content) != 1 {
		return false
	}
	fields, err := assistantTestObject([]byte(tool.Content[0].Text()))
	if err != nil {
		return false
	}
	return string(fields["status"]) == `"check-refused"` && fields["actionId"] != nil
}

// Checks must be actual later operations of this producer, never a check from
// an earlier callback, another candidate or an unbound imported operation.
func assistantDialectAfter(run assistantRunRecord, later, earlier string) bool {
	seen := false
	for _, op := range run.Operations {
		if op.ID == later {
			return seen
		}
		if op.ID == earlier {
			seen = true
		}
	}
	return false
}

// The browser checkpoint reader consumes the raw report. Keep all published
// diagnostics/coverage notes, and reject null/string coercion before decoding.
func validAssistantDialectReport(raw []byte) bool {
	fields, err := assistantTestObject(raw)
	if err != nil || (!validUTF8(raw) || jsonscalar.ValidateEscapes(raw) != nil) {
		return false
	}
	status, err := assistantTestString(fields["status"])
	if err != nil || status == "" {
		return false
	}
	diagnostics, err := assistantChatArray(fields["diagnostics"])
	if err != nil {
		return false
	}
	for _, d := range diagnostics {
		f, err := assistantTestObject(d)
		if err != nil {
			return false
		}
		for _, key := range []string{"code", "message"} {
			if _, err = assistantTestString(f[key]); err != nil {
				return false
			}
		}
		if f["instancePath"] != nil {
			if _, err = assistantTestString(f["instancePath"]); err != nil {
				return false
			}
		}
	}
	if fields["compatibility"] != nil {
		c, err := assistantTestObject(fields["compatibility"])
		if err != nil {
			return false
		}
		notes, err := assistantChatArray(c["notes"])
		if err != nil {
			return false
		}
		for _, n := range notes {
			f, err := assistantTestObject(n)
			if err != nil {
				return false
			}
			for _, key := range []string{"code", "kind", "target", "severity"} {
				if _, err = assistantTestString(f[key]); err != nil {
					return false
				}
			}
		}
	}
	return true
}

func validAssistantDialectView(read assistantDialectRead, fields map[string]json.RawMessage) bool {
	keys := []string{"id", "name", "revision", "recipeVersion", "guidance"}
	if read.Variant != "" {
		keys = append(keys, "variant", "mapping", "latest")
	} else {
		keys = append(keys, "packKey", "packDigest", "pack", "facts", "evidenceRequirements", "recordCount", "columns", "current")
	}
	if len(fields) != len(keys) {
		return false
	}
	for _, key := range keys {
		if fields[key] == nil {
			return false
		}
	}
	if _, err := assistantTestControls(read.View, keys...); err != nil {
		return false
	}
	if _, err := assistantTestString(fields["guidance"]); err != nil {
		return false
	}
	if read.Variant != "" {
		if string(fields["mapping"]) != "null" {
			var m inputRecordDialect
			if readAssistantHostReceipt(fields["mapping"], &m) != nil {
				return false
			}
		}
		if read.BasePublication == "" {
			return string(fields["latest"]) == "null"
		}
		latest, err := assistantTestControls(fields["latest"], "version", "digest", "document")
		if err != nil || len(latest) != 3 {
			return false
		}
		for _, key := range []string{"version", "digest", "document"} {
			if _, err = assistantTestString(latest[key]); err != nil {
				return false
			}
		}
		document, _ := assistantTestString(latest["document"])
		digest, _ := assistantTestString(latest["digest"])
		return digest == "sha256:"+digestOf([]byte(document))
	}
	var count int
	if json.Unmarshal(fields["recordCount"], &count) != nil || string(fields["recordCount"]) == "null" || count < 0 || count > inputRecordLimit {
		return false
	}
	facts, err := assistantDialectFacts(read.Pack)
	if err != nil || !sameDialectJSON(fields["facts"], assistantChatJSON(facts)) {
		return false
	}
	packFields, err := assistantTestObject(read.Pack)
	if err != nil {
		return false
	}
	requirements := json.RawMessage(`[]`)
	if packFields["evidenceRequirements"] != nil {
		requirements = packFields["evidenceRequirements"]
	}
	if !sameDialectJSON(fields["evidenceRequirements"], requirements) {
		return false
	}
	columns, err := assistantChatArray(fields["columns"])
	if err != nil || len(columns) > 1000 {
		return false
	}
	seen := map[string]bool{}
	for _, raw := range columns {
		var c assistantDialectColumnView
		if readAssistantHostReceipt(raw, &c) != nil || !assistantDialectRecipeText(c.Name, 1024) || seen[c.Name] || c.Samples == nil || len(c.Samples) > 3 {
			return false
		}
		seen[c.Name] = true
		f, _ := assistantTestObject(raw)
		samples, _ := assistantChatArray(f["samples"])
		for _, sample := range samples {
			var v assistantDialectSample
			if readAssistantHostReceipt(sample, &v) != nil || runcontext.U16Length(v.Value) > 160 || !contains([]string{"asserted", "generated"}, v.Origin) {
				return false
			}
		}
	}
	if string(fields["current"]) != "null" {
		f, err := assistantTestControls(fields["current"], "packKey", "packDigest", "mapping", "conversions")
		if err != nil || len(f) != 4 {
			return false
		}
		var key, digest string
		if json.Unmarshal(f["packKey"], &key) != nil || key == "" || json.Unmarshal(f["packDigest"], &digest) != nil || !strings.HasPrefix(digest, "sha256:") {
			return false
		}
		var mapping inputRecordDialect
		if readAssistantHostReceipt(f["mapping"], &mapping) != nil {
			return false
		}
		var conversions map[string]json.RawMessage
		if json.Unmarshal(f["conversions"], &conversions) != nil || conversions == nil {
			return false
		}
		for _, raw := range conversions {
			var c inputConversion
			if readAssistantHostReceipt(raw, &c) != nil {
				return false
			}
		}
	}
	return true
}

// Validate new custody's original typed members before Go can map null to a
// zero scalar or drop a missing nested array element during storage import.
func readAssistantDialectCustody(raw []byte, shape reflect.Type) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errAssistantRunInvalid
	}
	for shape.Kind() == reflect.Pointer {
		shape = shape.Elem()
	}
	if shape == reflect.TypeOf(json.RawMessage{}) {
		return nil
	}
	switch shape.Kind() {
	case reflect.Struct:
		fields, err := assistantTestObject(raw)
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for i := 0; i < shape.NumField(); i++ {
			f := shape.Field(i)
			name, options, _ := strings.Cut(f.Tag.Get("json"), ",")
			known[name] = true
			if value, ok := fields[name]; ok {
				if err = readAssistantDialectCustody(value, f.Type); err != nil {
					return err
				}
			} else if !strings.Contains(options, "omit") {
				return errAssistantRunInvalid
			}
		}
		for name := range fields {
			if !known[name] {
				return errAssistantRunInvalid
			}
		}
	case reflect.Slice:
		if shape.Elem().Kind() == reflect.Uint8 {
			var value []byte
			if json.Unmarshal(raw, &value) != nil {
				return errAssistantRunInvalid
			}
			return nil
		}
		rows, err := assistantChatArray(raw)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err = readAssistantDialectCustody(row, shape.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		fields, err := assistantTestObject(raw)
		if err != nil {
			return err
		}
		for _, value := range fields {
			if err = readAssistantDialectCustody(value, shape.Elem()); err != nil {
				return err
			}
		}
	default:
		value := reflect.New(shape).Interface()
		if json.Unmarshal(raw, value) != nil {
			return errAssistantRunInvalid
		}
	}
	return nil
}
