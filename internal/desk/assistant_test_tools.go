package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

type assistantTestArguments struct {
	Owner         string   `json:"owner,omitempty"`
	CoverageRunID string   `json:"coverageRunId,omitempty"`
	Document      string   `json:"document,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	Unknowns      []string `json:"unknowns,omitempty"`
}
type assistantTestRead struct {
	Owner         string `json:"owner"`
	Draft         bool   `json:"draft"`
	Path          string `json:"path"`
	Declaration   string `json:"declaration"`
	Pack          []byte `json:"pack"`
	PackDigest    string `json:"packDigest"`
	Title         string `json:"title"`
	SuitePresent  bool   `json:"suitePresent"`
	Suite         []byte `json:"suite"`
	CoverageRunID string `json:"coverageRunId,omitempty"`
}
type assistantTestProposalOutput struct {
	Read         assistantTestRead `json:"read"`
	Proposal     []byte            `json:"proposal"`
	ValidationID string            `json:"validationId"`
}

func assistantTestTool(name string) bool {
	return name == "read_pack_tests" || name == "propose_test_cases"
}
func assistantTestSchemas() map[string]json.RawMessage {
	text := func(n int) any { return map[string]any{"type": "string", "maxLength": n} }
	owner := text(256)
	object := func(fields map[string]any, required []string) json.RawMessage {
		return assistantChatJSON(map[string]any{"type": "object", "properties": fields, "required": required, "additionalProperties": false})
	}
	return map[string]json.RawMessage{"read_pack_tests": object(map[string]any{"owner": owner, "coverageRunId": text(256)}, []string{}), "propose_test_cases": object(map[string]any{"owner": owner, "document": text(1048576), "summary": text(4000), "unknowns": map[string]any{"type": "array", "maxItems": 20, "items": text(2000)}}, []string{"document", "summary"})}
}
func readAssistantTestArguments(name string, raw []byte) (assistantTestArguments, error) {
	var a assistantTestArguments
	allowed := []string{"owner", "coverageRunId"}
	if name == "propose_test_cases" {
		allowed = []string{"owner", "document", "summary", "unknowns"}
	} else if name != "read_pack_tests" {
		return a, errAssistantRunInvalid
	}
	fields, err := assistantTestControls(raw, allowed...)
	if err != nil || assistantHostArguments(raw, &a) != nil {
		return a, failure.ToolInput("Use exactly the declared test-design arguments.")
	}
	for key, v := range fields {
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return a, errAssistantRunInvalid
		}
		if key == "owner" && (a.Owner == "" || len(a.Owner) > 256) || key == "coverageRunId" && (a.CoverageRunID == "" || len(a.CoverageRunID) > 256) {
			return a, errAssistantRunInvalid
		}
	}
	if name == "propose_test_cases" {
		if fields["document"] == nil || fields["summary"] == nil || len(a.Document) > 1048576 || strings.TrimSpace(a.Summary) == "" || runcontext.U16Length(a.Summary) > 4000 || len(a.Unknowns) > 20 {
			return a, failure.ToolInput("Provide a test matrix up to 1 MiB and a brief explanation.")
		}
		for _, q := range a.Unknowns {
			if strings.TrimSpace(q) == "" || runcontext.U16Length(q) > 2000 {
				return a, failure.ToolInput("Provide nonempty policy questions.")
			}
		}
	}
	return a, nil
}
func (e *assistantRunExecution) assistantTestTools(runtime *assistantRuntime) ([]runcontext.HostTool, error) {
	tools := []runcontext.HostTool{}
	for _, name := range []string{"read_pack_tests", "propose_test_cases"} {
		name := name
		description := "Read an exact declared pack or saved unfinalized draft, saved tests, current coverage and Runtime test-design guidance. Required before proposing cases. Does not run tests or change cases."
		t := runcontext.HostTool{Name: name, Description: description, InputSchema: assistantTestSchemas()[name], Effect: "read", Delegation: "read", ContextPolicy: &runcontext.ToolContextPolicy{Reference: true}}
		if name == "propose_test_cases" {
			t.Description = "Retain a Runtime-validated test proposal in the exact owner's test suite for human review. Read current tests first. No cases, pack, thresholds or expected outcomes are saved or changed. Requires compatible Runtime fact-type validation."
			t.Effect = "propose"
			t.Delegation = ""
		}
		t.Execute = func(ctx context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
			a, err := readAssistantTestArguments(name, raw)
			if err != nil {
				return assistantHostFailure(err), nil
			}
			return e.invokeObservedHost(ctx, name, raw, func(ctx context.Context, root bool) (assistantHostObservation, error) {
				r, read, out, err := e.executeTestTool(ctx, runtime, name, a, root)
				return assistantHostObservation{Result: r, TestsRead: read, Output: out}, err
			})
		}
		tools = append(tools, t)
	}
	return tools, nil
}
func assistantTestDefaultOwner(source []byte) string {
	fields, err := assistantTestObject(source)
	if err != nil {
		return ""
	}
	if pack, err := assistantTestObject(fields["pack"]); err == nil {
		id, _ := assistantTestString(pack["id"])
		if id != "" {
			return id
		}
	}
	id, _ := assistantTestString(fields["draftId"])
	return id
}
func (e *assistantRunExecution) readAssistantTestTarget(ctx context.Context, owner string) (assistantTestRead, error) {
	if err := e.Authorize(ctx); err != nil {
		return assistantTestRead{}, err
	}
	if owner == "" {
		owner = assistantTestDefaultOwner(e.source.Original)
	}
	if owner == "" || len(owner) > 256 {
		return assistantTestRead{}, failure.ToolInput("Choose a declared saved pack or a real unfinalized draft.")
	}
	s := e.service.server
	config, _, err := e.graphDeclarations(ctx)
	if err != nil {
		return assistantTestRead{}, err
	}
	lock, err := s.privateDataLockContext(ctx, true)
	if err != nil {
		return assistantTestRead{}, err
	}
	defer lock.Close()
	store, err := s.openChatData()
	if err != nil {
		return assistantTestRead{}, err
	}
	defer store.root.Close()
	name, err := resolveConversationName(store.root, s.conversationIdentity())
	if err != nil {
		return assistantTestRead{}, err
	}
	return s.readAssistantTestTargetLocked(store.root, name, config, owner)
}

func (s *Server) readAssistantTestTargetLocked(root *os.Root, name string, config assistantDecisions, owner string) (assistantTestRead, error) {
	target := assistantTestRead{Owner: owner, Path: "", Declaration: "", Pack: []byte{}, Suite: []byte(`{"revision":0,"cases":[],"runs":[],"recovered":[]}`)}
	draft, err := s.readWorkspaceRecord(root, draftPackName(name), true)
	if err != nil {
		return target, err
	}
	var drafts struct {
		Drafts  []json.RawMessage `json:"drafts"`
		Deleted []string          `json:"deleted"`
	}
	if json.Unmarshal(draft.Content, &drafts) != nil {
		return target, errAssistantRunInvalid
	}
	for _, raw := range drafts.Drafts {
		fields, err := assistantTestObject(raw)
		if err != nil {
			return target, err
		}
		id, _ := assistantTestString(fields["id"])
		if id != owner {
			continue
		}
		if contains(drafts.Deleted, owner) {
			return target, errAssistantRunScope
		}
		if v := fields["finalized"]; v != nil && !bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			break
		}
		cp, err := assistantTestObject(fields["checkpoint"])
		if err != nil {
			return target, err
		}
		state, err := assistantTestObject(cp["state"])
		if err != nil {
			return target, err
		}
		candidates, err := assistantChatArray(state["candidates"])
		if err != nil || len(candidates) == 0 {
			return target, failure.ToolInput("This draft has no retained pack to test yet.")
		}
		latest, err := assistantTestObject(candidates[len(candidates)-1])
		if err != nil {
			return target, err
		}
		text, err := assistantTestString(latest["text"])
		if err != nil || !json.Valid([]byte(text)) {
			return target, errAssistantRunInvalid
		}
		var generation int64
		title, titleErr := assistantTestString(fields["title"])
		if json.Unmarshal(fields["generation"], &generation) != nil || generation < 1 || generation > 9007199254740991 || !strings.HasPrefix(owner, "draft-") || titleErr != nil || strings.TrimSpace(title) == "" || runcontext.U16Length(title) > 500 {
			return target, errAssistantRunInvalid
		}
		target.Draft = true
		target.Pack = []byte(text)
		target.Title, _ = assistantTestString(fields["title"])
		// Generation/lifecycle identity is retained separately from exact candidate bytes.
		target.Declaration = digestOf(assistantChatEncodeObject(map[string]json.RawMessage{"id": fields["id"], "generation": fields["generation"]}))
		break
	}
	if !target.Draft {
		entry, ok := config.Packs[owner]
		if !ok {
			return target, failure.ToolInput("Choose a declared saved pack or a real unfinalized draft.")
		}
		path, err := wireRelativePath(entry.Path)
		if err != nil || path != entry.Path {
			return target, errAssistantRunScope
		}
		target.Path = path
		target.Declaration = assistantDeclarationDigest(entry)
		file := s.readProjectFile(entry.Path)
		if file.err != nil || !file.present || !validUTF8(file.data) {
			return target, errAssistantRunScope
		}
		target.Pack = file.data
		fields, err := assistantTestObject(target.Pack)
		if err != nil {
			return target, err
		}
		target.Title, _ = assistantTestString(fields["title"])
		if target.Title == "" {
			target.Title = owner
		}
	}
	target.PackDigest = digestOf(target.Pack)
	saved, err := s.readWorkspaceRecord(root, strings.Replace(name, "conversations-", "pack-tests-", 1), false)
	if err != nil {
		return target, err
	}
	fields, err := assistantTestObject(saved.Content)
	if err != nil {
		return target, err
	}
	suites, err := assistantTestObject(fields["suites"])
	if err != nil {
		return target, err
	}
	if raw, ok := suites[owner]; ok {
		target.SuitePresent = true
		target.Suite = bytes.Clone(raw)
	}
	if _, err := assistantTestSuite(target.Suite); err != nil {
		return target, err
	}
	return target, nil
}
func assistantTestSuite(raw []byte) (map[string]json.RawMessage, error) {
	fields, err := assistantTestObject(raw)
	if err != nil {
		return nil, err
	}
	var revision int64
	if json.Unmarshal(fields["revision"], &revision) != nil || revision < 0 || revision > 9007199254740991 {
		return nil, errAssistantRunInvalid
	}
	cases, err := assistantChatArray(fields["cases"])
	if err != nil || len(cases) > 256 {
		return nil, errAssistantRunInvalid
	}
	if _, err := assistantChatArray(fields["runs"]); err != nil {
		return nil, err
	}
	if _, err := assistantChatArray(fields["recovered"]); err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, raw := range cases {
		c, err := assistantTestObject(raw)
		if err != nil {
			return nil, err
		}
		id, err := assistantTestString(c["id"])
		if err != nil || ids[id] {
			return nil, errAssistantRunInvalid
		}
		ids[id] = true
		row, err := assistantTestObject(c["row"])
		if err != nil || string(row["id"]) != string(c["id"]) {
			return nil, errAssistantRunInvalid
		}
		for _, name := range []string{"name", "rationale", "origin"} {
			if _, err := assistantTestString(c[name]); err != nil {
				return nil, err
			}
		}
		var revision int64
		if c["revision"] == nil || json.Unmarshal(c["revision"], &revision) != nil || revision < 0 {
			return nil, errAssistantRunInvalid
		}
		if _, err := assistantChatArray(c["sources"]); err != nil {
			return nil, err
		}
		if _, err := assistantTestObject(c["sourceMappings"]); err != nil {
			return nil, err
		}
	}
	return fields, nil
}
func assistantTestSame(a, b assistantTestRead) bool {
	af, _ := assistantTestSuite(a.Suite)
	bf, _ := assistantTestSuite(b.Suite)
	return a.Owner == b.Owner && a.Draft == b.Draft && a.Path == b.Path && a.Declaration == b.Declaration && bytes.Equal(a.Pack, b.Pack) && a.PackDigest == b.PackDigest && a.SuitePresent == b.SuitePresent && bytes.Equal(af["cases"], bf["cases"])
}
func (e *assistantRunExecution) recheckAssistantTestRead(ctx context.Context, held assistantTestRead) error {
	current, err := e.readAssistantTestTarget(ctx, held.Owner)
	if err == nil && !assistantTestSame(held, current) {
		err = failure.ToolInput("The pack or test cases changed. Read the current pack and suite before proposing cases.")
	}
	if err == nil && held.CoverageRunID != "" {
		_, err = assistantTestCoverage(current, held.CoverageRunID)
	}
	return err
}
func (e *assistantRunExecution) rootAssistantTestRead(ctx context.Context, owner string) (assistantTestRead, error) {
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return assistantTestRead{}, err
	}
	for i := len(run.Operations) - 1; i >= 0; i-- {
		op := run.Operations[i]
		if op.Kind != "host" || op.Name != "read_pack_tests" || op.State != "completed" {
			continue
		}
		var in assistantHostIntent
		var result assistantHostResult
		if readAssistantHostReceipt(op.Intent, &in) == nil && in.Root && readAssistantHostReceipt(op.Result, &result) == nil && result.Tests != nil && result.Tests.Owner == owner {
			return *result.Tests, nil
		}
	}
	return assistantTestRead{}, failure.ToolInput("Read the current pack and test suite with read_pack_tests before proposing cases.")
}
func assistantTestCoverage(read assistantTestRead, explicit string) (json.RawMessage, error) {
	suite, _ := assistantTestSuite(read.Suite)
	cases, _ := assistantChatArray(suite["cases"])
	runs, _ := assistantChatArray(suite["runs"])
	for i := len(runs) - 1; i >= 0; i-- {
		run, err := assistantTestObject(runs[i])
		if err != nil {
			return nil, err
		}
		id, _ := assistantTestString(run["id"])
		if explicit != "" && id != explicit {
			continue
		}
		digest, _ := assistantTestString(run["packDigest"])
		rows, err := assistantChatArray(run["cases"])
		valid := err == nil && digest == read.PackDigest && run["report"] != nil && !bytes.Equal(bytes.TrimSpace(run["report"]), []byte("null")) && (run["error"] == nil || bytes.Equal(run["error"], []byte(`""`)))
		executable := []json.RawMessage{}
		for _, raw := range cases {
			c, _ := assistantTestObject(raw)
			row, _ := assistantTestObject(c["row"])
			errorClass, _ := assistantTestString(row["expectedErrorClass"])
			if row["expectedDisposition"] != nil || errorClass != "" {
				executable = append(executable, raw)
			}
		}
		valid = valid && len(rows) == len(executable)
		for _, raw := range executable {
			c, _ := assistantTestObject(raw)
			matched := false
			for _, v := range rows {
				held, _ := assistantTestObject(v)
				if bytes.Equal(c["id"], held["id"]) && assistantTestIdentity(c["row"]) == assistantTestIdentity(held["row"]) {
					matched = true
				}
			}
			valid = valid && matched
		}
		if valid {
			return runs[i], nil
		}
		if explicit != "" {
			return nil, failure.ToolInput("Coverage changed. Run the current suite before designing from these gaps.")
		}
	}
	if explicit != "" {
		return nil, failure.ToolInput("Coverage changed. Run the current suite before designing from these gaps.")
	}
	return nil, nil
}

// This is the existing browser consumer identity, not custody or an expectation
// rewrite. Original case/document bytes remain independently retained unchanged.
func assistantTestIdentity(raw []byte) string {
	v, err := runcontext.ParseJSON(raw)
	if err != nil {
		return ""
	}
	var sorted func(*runcontext.Value) *runcontext.Value
	sorted = func(v *runcontext.Value) *runcontext.Value {
		if v.IsArray() {
			out := runcontext.Array()
			for _, item := range v.Items() {
				out.Append(sorted(item))
			}
			return out
		}
		if v.IsObject() {
			keys := v.Keys()
			sort.SliceStable(keys, func(i, j int) bool {
				a, b := utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))
				for k := 0; k < len(a) && k < len(b); k++ {
					if a[k] != b[k] {
						return a[k] < b[k]
					}
				}
				return len(a) < len(b)
			})
			out := runcontext.Object()
			for _, k := range keys {
				out.Set(k, sorted(v.Get(k)))
			}
			return out
		}
		return v.Clone()
	}
	return sorted(v).Text()
}
func assistantTestSources(source []byte, suite []byte) ([]json.RawMessage, error) {
	out := []json.RawMessage{}
	indices := map[string]int{}
	add := func(raw json.RawMessage) error {
		list, err := assistantChatArray(raw)
		if err != nil {
			return err
		}
		for _, v := range list {
			f, err := assistantTestObject(v)
			if err != nil {
				return err
			}
			id, err := assistantTestString(f["id"])
			if err != nil || id == "" {
				return errAssistantRunInvalid
			}
			for _, key := range []string{"name", "text"} {
				if _, err := assistantTestString(f[key]); err != nil {
					return err
				}
			}
			if index, ok := indices[id]; ok {
				out[index] = v
			} else {
				indices[id] = len(out)
				out = append(out, v)
			}
		}
		return nil
	}
	chat, err := assistantTestObject(source)
	if err != nil {
		return nil, err
	}
	if chat["documents"] != nil {
		if err := add(chat["documents"]); err != nil {
			return nil, err
		}
	}
	fields, err := assistantTestSuite(suite)
	if err != nil {
		return nil, err
	}
	cases, _ := assistantChatArray(fields["cases"])
	for _, v := range cases {
		c, _ := assistantTestObject(v)
		if err := add(c["sources"]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func assistantTestJSON(v any) (runcontext.ToolResult, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return runcontext.ToolResult{}, err
	}
	return runcontext.ResultOf([]runcontext.Content{runcontext.TextContent(string(raw))}, nil), nil
}
func (e *assistantRunExecution) executeTestTool(ctx context.Context, runtime *assistantRuntime, name string, a assistantTestArguments, root bool) (runcontext.ToolResult, *assistantTestRead, *assistantDomainOutput, error) {
	var empty runcontext.ToolResult
	target, err := e.readAssistantTestTarget(ctx, a.Owner)
	if err != nil {
		return empty, nil, nil, err
	}
	if name == "read_pack_tests" {
		target.CoverageRunID = a.CoverageRunID
		coverage, err := assistantTestCoverage(target, a.CoverageRunID)
		if err != nil {
			return empty, nil, nil, err
		}
		ctx = withAssistantHostPrecondition(ctx, func(ctx context.Context) error { return e.recheckAssistantTestRead(ctx, target) })
		report, _, err := runtime.testTool(ctx, "experimental_get_test_matrix_contract", map[string]string{})
		if err != nil {
			return empty, nil, nil, err
		}
		contract, err := assistantTestObject(report["contract"])
		if err != nil || string(contract["matrixVersion"]) != `"3"` {
			return empty, nil, nil, assistantTestUnavailable()
		}
		if _, err := assistantChatArray(contract["rowMembers"]); err != nil {
			return empty, nil, nil, assistantTestUnavailable()
		}
		guidance, err := runtime.Prompt(ctx, assistantExecutionPrompt{Name: "test_pack", Arguments: map[string]string{"pack": string(target.Pack)}})
		if err != nil {
			return empty, nil, nil, err
		}
		suite, _ := assistantTestSuite(target.Suite)
		cases, _ := assistantChatArray(suite["cases"])
		sources, err := assistantTestSources(e.source.Original, target.Suite)
		if err != nil {
			return empty, nil, nil, err
		}
		factChecks := any([]assistantTestFactRow{})
		if len(cases) > 0 {
			rows := []json.RawMessage{}
			for _, raw := range cases {
				c, _ := assistantTestObject(raw)
				rows = append(rows, c["row"])
			}
			matrix, _ := readAssistantTestMatrix(assistantChatEncodeObject(map[string]json.RawMessage{"matrixVersion": []byte(`"3"`), "cases": assistantChatEncodeArray(rows)}))
			checked, _, err := runtime.testTool(ctx, "experimental_validate_test_matrix", map[string]string{"matrix": string(matrix.Runtime), "pack": string(target.Pack)})
			if err == nil {
				_, facts, err := assistantTestValidationReport(checked, matrix)
				if err == nil {
					factChecks = facts
				} else {
					factChecks = map[string]string{"status": "unavailable", "message": "Current Runtime fact-type advice is unavailable (Desk #582)."}
				}
			} else {
				if ctx.Err() != nil {
					return empty, nil, nil, ctx.Err()
				}
				factChecks = map[string]string{"status": "unavailable", "message": "Current Runtime fact-type advice is unavailable (Desk #582)."}
			}
		}
		if err := e.recheckAssistantTestRead(ctx, target); err != nil {
			return empty, nil, nil, err
		}
		value := map[string]any{"owner": target.Owner, "draft": target.Draft, "title": target.Title, "pack": string(target.Pack), "packDigest": target.PackDigest, "contract": json.RawMessage(report["contract"]), "runtimeGuidance": guidance, "factChecks": factChecks, "cases": assistantTestVisibleCases(suite), "recentResults": assistantTestRecentResults(suite, target.PackDigest), "previousQuestions": assistantTestPreviousQuestions(suite), "sources": sources, "coverage": assistantTestCoverageDescription(coverage, a.CoverageRunID != ""), "instruction": "Answer the current question. Propose only when requested; no tests are run by design. Expectations come from requirements, never observed outputs. Preserve decimal strings, intentional wrong types, missing, null, false and zero. Ask about ambiguous policy. Every proposal requires review before saving."}
		result, err := assistantTestJSON(value)
		if err != nil {
			return empty, nil, nil, err
		}
		// Read custody travels through the fixed typed receipt, including child reads;
		// only a completed root read may establish the proposal's before basis.
		return result, &target, nil, nil
	}
	if !root || e.record.Binding.Phase == "plan" {
		return empty, nil, nil, failure.ToolInput("Test proposals are available only in the parent Work phase.")
	}
	held, err := e.rootAssistantTestRead(ctx, target.Owner)
	if err != nil {
		return empty, nil, nil, err
	}
	if !assistantTestSame(held, target) {
		return empty, nil, nil, failure.ToolInput("Read the current pack and test suite before proposing cases.")
	}
	if err := e.recheckAssistantTestRead(ctx, held); err != nil {
		return empty, nil, nil, err
	}
	m, err := readAssistantTestMatrix([]byte(a.Document))
	if err != nil {
		return empty, nil, nil, failure.ToolInput("Provide a version 3 matrix with unique case IDs, facts and exactly one authored expected disposition or expected error class.")
	}
	sources, err := assistantTestSources(e.source.Original, held.Suite)
	if err != nil {
		return empty, nil, nil, err
	}
	ctx = withAssistantHostPrecondition(ctx, func(ctx context.Context) error { return e.recheckAssistantTestRead(ctx, held) })
	report, validationID, err := runtime.testTool(ctx, "experimental_validate_test_matrix", map[string]string{"matrix": string(m.Runtime), "pack": string(held.Pack)})
	if err != nil {
		return empty, nil, nil, err
	}
	findings, facts, err := assistantTestValidationReport(report, m)
	if err != nil {
		return empty, nil, nil, err
	}
	findings = append(findings, assistantTestFactReview(facts, m)...)
	findings = append(findings, assistantTestSourceReview(m, sources)...)
	suite, _ := assistantTestSuite(held.Suite)
	cases, _ := assistantChatArray(suite["cases"])
	if held.CoverageRunID != "" {
		for _, raw := range cases {
			c, _ := assistantTestObject(raw)
			id, _ := assistantTestString(c["id"])
			if contains(m.IDs, id) {
				findings = append(findings, assistantTestFinding{"EXISTING-CASE", "/cases", "Gap filling must add new unique IDs without replacing existing cases."})
			}
		}
	}
	if len(findings) > 0 {
		r, err := assistantTestJSON(map[string]any{"status": "invalid", "findings": findings, "message": "No proposal retained. Preserve intended facts and expectations; explain intentional wrong types and ask unresolved policy questions."})
		return r.WithIsError(true), nil, nil, err
	}
	if err := e.recheckAssistantTestRead(ctx, held); err != nil {
		return empty, nil, nil, err
	}
	snapshots := map[string]string{}
	for _, raw := range cases {
		c, _ := assistantTestObject(raw)
		id, _ := assistantTestString(c["id"])
		snapshots[id] = assistantTestIdentity(raw)
	}
	id := "test-proposal-" + digestOf([]byte(e.record.Binding.ExecutionKey + "\x00" + runcontext.CallIDOf(ctx) + "\x00" + ctxPolicyCallback(ctx)))[:40]
	unknowns := a.Unknowns
	if unknowns == nil {
		unknowns = []string{}
	}
	proposal := map[string]json.RawMessage{"id": assistantChatJSON(id), "chatId": assistantChatJSON(e.record.Binding.Owner.ID), "at": assistantChatJSON(time.Now().UTC().Format(time.RFC3339Nano)), "request": assistantChatJSON(e.source.Text), "packDigest": assistantChatJSON(held.PackDigest), "model": assistantChatJSON(e.record.Binding.Model), "contractVersion": []byte(`"1"`), "sources": assistantChatEncodeArray(sources), "caseSnapshots": assistantChatJSON(snapshots), "state": []byte(`"ready"`), "message": assistantChatJSON("The proposal is ready for review. No cases have been saved."), "attempts": assistantChatEncodeArray([]json.RawMessage{assistantChatEncodeObject(map[string]json.RawMessage{"document": m.Raw, "unknowns": assistantChatJSON(unknowns), "message": assistantChatJSON(strings.TrimSpace(a.Summary)), "findings": []byte(`[]`)})})}
	if held.CoverageRunID != "" {
		proposal["additionsOnly"] = []byte(`true`)
		proposal["coverageRunId"] = assistantChatJSON(held.CoverageRunID)
	}
	output := &assistantTestProposalOutput{Read: held, Proposal: assistantChatEncodeObject(proposal), ValidationID: validationID}
	result, err := assistantTestProposalResult(output, e.record.Binding.Owner.ID)
	return result, nil, &assistantDomainOutput{Kind: "test-proposal", TestProposal: output}, err
}
func assistantTestProposalResult(out *assistantTestProposalOutput, chat string) (runcontext.ToolResult, error) {
	fields, err := assistantTestObject(out.Proposal)
	if err != nil {
		return runcontext.ToolResult{}, err
	}
	id, _ := assistantTestString(fields["id"])
	path := "/packs/" + url.PathEscape(out.Read.Owner) + "/matrix"
	if out.Read.Draft {
		path = "/packs/drafts/" + url.PathEscape(out.Read.Owner)
	}
	query := url.Values{"chat": []string{chat}, "testProposal": []string{id}}
	return assistantHostJSON(map[string]string{"status": "needs-review", "proposalId": id, "reviewUrl": path + "?" + query.Encode(), "message": "The proposal is ready for review. No cases have been saved."})
}

func assistantTestVisibleCases(suite map[string]json.RawMessage) []json.RawMessage {
	cases, _ := assistantChatArray(suite["cases"])
	out := []json.RawMessage{}
	for _, raw := range cases {
		c, _ := assistantTestObject(raw)
		intentional := c["intentionalFactTypes"]
		if intentional == nil {
			intentional = []byte(`[]`)
		}
		out = append(out, assistantChatEncodeObject(map[string]json.RawMessage{"row": c["row"], "rationale": c["rationale"], "intentionalFactTypes": intentional}))
	}
	return out
}
func assistantTestCoverageDescription(raw []byte, explicit bool) any {
	if raw == nil {
		return "Coverage has not been checked for this exact pack and suite. Consider normal decisions, exceptions, missing evidence, boundaries and fallthrough. Do not infer expectations from observations."
	}
	run, _ := assistantTestObject(raw)
	report, _ := assistantTestObject(run["report"])
	packs, _ := assistantChatArray(report["packs"])
	probes := []json.RawMessage{}
	for _, raw := range packs {
		p, _ := assistantTestObject(raw)
		coverage, _ := assistantChatArray(p["coverage"])
		for _, raw := range coverage {
			probe, _ := assistantTestObject(raw)
			if string(probe["status"]) == `"missing"` {
				probes = append(probes, raw)
			}
		}
	}
	id, _ := assistantTestString(run["id"])
	instruction := "Consider these probes only when design is requested; explain infeasible or policy-dependent coverage. Do not generate cases for ordinary diagnostic questions."
	if explicit {
		instruction = "Design additional cases for these missing probes using new unique IDs. Keep existing cases and expectations unchanged. Ground expectations independently in requirements and sources, never observations. Explain probes requiring a policy decision. Cases require review and are not saved or run automatically."
	}
	return map[string]any{"runId": id, "missingProbes": probes, "instruction": instruction}
}
func assistantTestRecentResults(suite map[string]json.RawMessage, digest string) []json.RawMessage {
	runs, _ := assistantChatArray(suite["runs"])
	if len(runs) > 3 {
		runs = runs[len(runs)-3:]
	}
	out := []json.RawMessage{}
	for _, raw := range runs {
		run, _ := assistantTestObject(raw)
		held, _ := assistantTestString(run["packDigest"])
		fields := map[string]json.RawMessage{"at": run["at"], "matchesCurrentPack": assistantChatJSON(held == digest)}
		for _, key := range []string{"report", "trial", "error"} {
			if run[key] != nil {
				fields[key] = run[key]
			}
		}
		out = append(out, assistantChatEncodeObject(fields))
	}
	return out
}
func assistantTestPreviousQuestions(suite map[string]json.RawMessage) json.RawMessage {
	proposals, _ := assistantChatArray(suite["proposals"])
	if len(proposals) > 0 {
		p, _ := assistantTestObject(proposals[len(proposals)-1])
		attempts, _ := assistantChatArray(p["attempts"])
		if len(attempts) > 0 {
			a, _ := assistantTestObject(attempts[len(attempts)-1])
			if a["unknowns"] != nil {
				return a["unknowns"]
			}
		}
	}
	return []byte(`[]`)
}
