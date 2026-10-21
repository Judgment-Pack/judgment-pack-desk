package desk

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func validAssistantTestRead(read assistantTestRead) bool {
	if read.Owner == "" || len(read.Owner) > 256 || !assistantRunDigest(read.Declaration) || !assistantRunDigest(read.PackDigest) || digestOf(read.Pack) != read.PackDigest || !json.Valid(read.Pack) || len(read.Pack) > maxFileBytes {
		return false
	}
	if read.Draft {
		if read.Path != "" {
			return false
		}
	} else {
		path, err := wireRelativePath(read.Path)
		if err != nil || path != read.Path {
			return false
		}
	}
	_, err := assistantTestSuite(read.Suite)
	if err != nil {
		return false
	}
	if !read.SuitePresent && !bytes.Equal(read.Suite, []byte(`{"revision":0,"cases":[],"runs":[],"recovered":[]}`)) {
		return false
	}
	if _, err := assistantTestCoverage(read, read.CoverageRunID); err != nil {
		return false
	}
	return true
}
func validateAssistantTestReadReceipt(op assistantRunOperation, in assistantHostIntent, result assistantHostResult) error {
	if result.Tests == nil {
		return nil
	}
	if op.Name != "read_pack_tests" || op.State != "completed" || result.Read != nil || result.PolicySource != nil || !validAssistantTestRead(*result.Tests) {
		return errAssistantRunInvalid
	}
	args, err := readAssistantTestArguments(op.Name, in.Arguments)
	if err != nil || args.Owner != "" && args.Owner != result.Tests.Owner || args.CoverageRunID != result.Tests.CoverageRunID {
		return errAssistantRunInvalid
	}
	value, err := runcontext.ParseJSON(result.Result)
	if err != nil {
		return err
	}
	tool := runcontext.ReadToolResult(value)
	if tool.Err() || len(tool.Content) != 1 {
		return errAssistantRunInvalid
	}
	fields, err := assistantTestObject([]byte(tool.Content[0].Text()))
	if err != nil {
		return err
	}
	pack, err := assistantTestString(fields["pack"])
	if err != nil || !bytes.Equal([]byte(pack), result.Tests.Pack) || string(fields["packDigest"]) != string(assistantChatJSON(result.Tests.PackDigest)) || string(fields["owner"]) != string(assistantChatJSON(result.Tests.Owner)) {
		return errAssistantRunInvalid
	}
	suite, _ := assistantTestSuite(result.Tests.Suite)
	if !assistantHostArgumentsMatch(fields["cases"], assistantChatEncodeArray(assistantTestVisibleCases(suite))) {
		return errAssistantRunInvalid
	}
	return nil
}
func validateAssistantTestOutput(run assistantRunRecord, out assistantDomainOutput, receipts map[string]assistantHostResult) error {
	p := out.TestProposal
	if out.Kind != "test-proposal" || p == nil || out.PolicyUpdate != nil || out.PolicyInterpretation != nil || out.PolicySource != nil || !reflect.DeepEqual(out.Draft, graphDraft{}) || out.RunID != run.ID || out.RequestKey != run.Binding.ExecutionKey || run.Binding.Phase == "plan" || !validAssistantTestRead(p.Read) {
		return errAssistantRunInvalid
	}
	var producer *assistantRunOperation
	producerIndex := -1
	for i := range run.Operations {
		if run.Operations[i].ID == out.OperationID {
			producer = &run.Operations[i]
			producerIndex = i
		}
	}
	if producer == nil || producer.Kind != "host" || producer.Name != "propose_test_cases" || producer.State != "completed" {
		return errAssistantRunInvalid
	}
	var in assistantHostIntent
	if readAssistantHostReceipt(producer.Intent, &in) != nil || !in.Root || in.CallID != out.CallID || in.Policy != nil {
		return errAssistantRunInvalid
	}
	args, err := readAssistantTestArguments(producer.Name, in.Arguments)
	if err != nil || args.Owner != "" && args.Owner != p.Read.Owner {
		return errAssistantRunInvalid
	}
	fields, err := assistantTestControls(p.Proposal, "id", "chatId", "at", "request", "packDigest", "model", "contractVersion", "sources", "caseSnapshots", "additionsOnly", "coverageRunId", "state", "message", "attempts")
	if err != nil {
		return err
	}
	id := "test-proposal-" + digestOf([]byte(run.Binding.ExecutionKey + "\x00" + in.CallID + "\x00" + in.CallbackID))[:40]
	if string(fields["id"]) != string(assistantChatJSON(id)) || string(fields["chatId"]) != string(assistantChatJSON(run.Binding.Owner.ID)) || string(fields["model"]) != string(assistantChatJSON(run.Binding.Model)) || string(fields["packDigest"]) != string(assistantChatJSON(p.Read.PackDigest)) || string(fields["contractVersion"]) != `"1"` || string(fields["state"]) != `"ready"` {
		return errAssistantRunInvalid
	}
	at, err := assistantTestString(fields["at"])
	if err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
		return errAssistantRunInvalid
	}
	for _, key := range []string{"request", "message"} {
		if _, err := assistantTestString(fields[key]); err != nil {
			return err
		}
	}
	attempts, err := assistantChatArray(fields["attempts"])
	if err != nil || len(attempts) != 1 {
		return errAssistantRunInvalid
	}
	attempt, err := assistantTestControls(attempts[0], "document", "unknowns", "message", "findings")
	if err != nil {
		return err
	}
	if !bytes.Equal(attempt["document"], []byte(args.Document)) || string(attempt["message"]) != string(assistantChatJSON(strings.TrimSpace(args.Summary))) || string(attempt["findings"]) != "[]" {
		return errAssistantRunInvalid
	}
	unknowns := args.Unknowns
	if unknowns == nil {
		unknowns = []string{}
	}
	if !bytes.Equal(attempt["unknowns"], assistantChatJSON(unknowns)) {
		return errAssistantRunInvalid
	}
	matrix, err := readAssistantTestMatrix(attempt["document"])
	if err != nil {
		return err
	}
	sources, err := assistantChatArray(fields["sources"])
	if err != nil {
		return err
	}
	suite, _ := assistantTestSuite(p.Read.Suite)
	cases, _ := assistantChatArray(suite["cases"])
	snapshots := map[string]string{}
	for _, raw := range cases {
		c, _ := assistantTestObject(raw)
		id, _ := assistantTestString(c["id"])
		snapshots[id] = assistantTestIdentity(raw)
	}
	var stored map[string]string
	if json.Unmarshal(fields["caseSnapshots"], &stored) != nil || !reflect.DeepEqual(stored, snapshots) {
		return errAssistantRunInvalid
	}
	if p.Read.CoverageRunID != "" {
		if string(fields["additionsOnly"]) != "true" || string(fields["coverageRunId"]) != string(assistantChatJSON(p.Read.CoverageRunID)) {
			return errAssistantRunInvalid
		}
		for id := range snapshots {
			if contains(matrix.IDs, id) {
				return errAssistantRunInvalid
			}
		}
	} else if fields["additionsOnly"] != nil || fields["coverageRunId"] != nil {
		return errAssistantRunInvalid
	}
	// A prior completed root read supplies this exact basis. Child work and a
	// caller-written ready flag cannot establish custody during import/recovery.
	before := false
	for i := 0; i < producerIndex; i++ {
		op := run.Operations[i]
		var held assistantHostIntent
		if op.Kind != "host" || op.Name != "read_pack_tests" || op.State != "completed" || readAssistantHostReceipt(op.Intent, &held) != nil || !held.Root {
			continue
		}
		r := receipts[op.ID]
		if r.Tests != nil && reflect.DeepEqual(*r.Tests, p.Read) {
			before = true
		}
	}
	if !before {
		return errAssistantRunInvalid
	}
	validated := false
	for i, op := range run.Operations {
		if op.ID != p.ValidationID {
			continue
		}
		if i <= producerIndex || op.Kind != "runtime" || op.Name != "tools/call" || op.State != "completed" {
			return errAssistantRunInvalid
		}
		var frame struct {
			Params struct {
				Name      string            `json:"name"`
				Arguments map[string]string `json:"arguments"`
			} `json:"params"`
		}
		if json.Unmarshal(op.Intent, &frame) != nil || frame.Params.Name != "experimental_validate_test_matrix" || len(frame.Params.Arguments) != 2 || frame.Params.Arguments["matrix"] != string(matrix.Runtime) || frame.Params.Arguments["pack"] != string(p.Read.Pack) {
			return errAssistantRunInvalid
		}
		var envelope struct {
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(op.Result, &envelope) != nil || envelope.Error != nil {
			return errAssistantRunInvalid
		}
		report, err := readAssistantTestPayload(envelope.Result)
		if err != nil {
			return err
		}
		findings, facts, err := assistantTestValidationReport(report, matrix)
		if err != nil || len(findings) > 0 || len(assistantTestFactReview(facts, matrix)) > 0 || len(assistantTestSourceReview(matrix, sources)) > 0 {
			return errAssistantRunInvalid
		}
		validated = true
	}
	if !validated {
		return errAssistantRunInvalid
	}
	receipt, ok := receipts[producer.ID]
	if !ok || receipt.Tests != nil || receipt.Read != nil || receipt.PolicySource != nil {
		return errAssistantRunInvalid
	}
	result, err := assistantTestProposalResult(p, run.Binding.Owner.ID)
	if err != nil || !bytes.Equal(result.Value().JSON(), receipt.Result) {
		return errAssistantRunInvalid
	}
	return nil
}
func validateAssistantTestProjectionSource(source []byte, run assistantRunRecord) error {
	hasTests := false
	for _, out := range run.DomainOutputs {
		if out.Kind == "test-proposal" {
			hasTests = true
		}
	}
	if !hasTests {
		return nil
	}
	fields, err := assistantTestObject(source)
	if err != nil {
		return err
	}
	cp, err := assistantTestObject(fields["checkpoint"])
	if err != nil {
		return err
	}
	state, err := assistantTestObject(cp["state"])
	if err != nil {
		return err
	}
	turns, err := assistantChatArray(state["turns"])
	if err != nil {
		return err
	}
	request := ""
	for _, raw := range turns {
		turn, _ := assistantTestObject(raw)
		id, _ := assistantTestString(turn["id"])
		if id == run.Binding.RequestID {
			request, _ = assistantTestString(turn["text"])
		}
	}
	for _, out := range run.DomainOutputs {
		if out.Kind != "test-proposal" {
			continue
		}
		p := out.TestProposal
		proposal, err := assistantTestObject(p.Proposal)
		if err != nil || string(proposal["request"]) != string(assistantChatJSON(request)) {
			return errAssistantRunScope
		}
		sources, err := assistantTestSources(source, p.Read.Suite)
		if err != nil || !bytes.Equal(proposal["sources"], assistantChatEncodeArray(sources)) {
			return errAssistantRunScope
		}
		for _, op := range run.Operations {
			if op.ID == out.OperationID {
				var in assistantHostIntent
				_ = readAssistantHostReceipt(op.Intent, &in)
				args, err := readAssistantTestArguments(op.Name, in.Arguments)
				if err != nil || args.Owner == "" && assistantTestDefaultOwner(source) != p.Read.Owner {
					return errAssistantRunScope
				}
			}
		}
	}
	return nil
}
func appendAssistantTestLinks(turns []json.RawMessage, run assistantRunRecord, at string) ([]json.RawMessage, error) {
	for _, out := range run.DomainOutputs {
		if out.Kind != "test-proposal" {
			continue
		}
		fields, err := assistantTestObject(out.TestProposal.Proposal)
		if err != nil {
			return nil, err
		}
		id, _ := assistantTestString(fields["id"])
		link := map[string]string{"owner": out.TestProposal.Read.Owner, "proposalId": id}
		turns = append(turns, assistantChatJSON(map[string]any{"id": assistantChatResponseID(run.ID) + "-test-" + digestOf([]byte(out.OperationID))[:16], "role": "assistant", "kind": "message", "text": "Test cases proposed for review. No cases have been saved.", "at": at, "testDesign": link}))
	}
	return turns, nil
}
