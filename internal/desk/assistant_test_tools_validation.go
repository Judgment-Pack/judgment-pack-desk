package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

// These checks mirror the existing test workspace. Runtime owns matrix and fact
// semantics; Desk never repairs facts, thresholds or independently authored answers.
type assistantTestRuntimeReceiptKey struct{}
type assistantTestRuntimeReceipt struct{ ID string }

type assistantTestFinding struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}
type assistantTestFactFinding struct {
	Code          string   `json:"code"`
	Path          string   `json:"path"`
	Message       string   `json:"message"`
	Operator      string   `json:"operator"`
	FactType      string   `json:"factType"`
	ExpectedTypes []string `json:"expectedTypes"`
	Within        *string  `json:"within,omitempty"`
}
type assistantTestFactRow struct {
	Index    int                        `json:"index"`
	ID       string                     `json:"id"`
	Status   string                     `json:"status"`
	Findings []assistantTestFactFinding `json:"findings"`
	Message  string                     `json:"message,omitempty"`
}
type assistantTestIntentional struct {
	CaseID string  `json:"caseId"`
	Path   string  `json:"path"`
	Within *string `json:"within,omitempty"`
	Reason string  `json:"reason"`
}
type assistantTestMatrix struct {
	Raw         []byte
	Runtime     []byte
	Rows        []json.RawMessage
	IDs         []string
	Intentional []assistantTestIntentional
	Mappings    map[string]json.RawMessage
}

func assistantTestObject(raw []byte) (map[string]json.RawMessage, error) {
	// Exact data member names and lexical values remain opaque. Reuse the shared
	// bounded data reader, rather than applying a control-envelope case fold.
	if err := checkJSONMembers(raw, opaqueJSONMembers); err != nil {
		return nil, errAssistantRunInvalid
	}
	members, err := configMembers(raw)
	if err != nil {
		return nil, errAssistantRunInvalid
	}
	fields := map[string]json.RawMessage{}
	for _, m := range members {
		fields[m.name] = bytes.Clone(raw[m.start:m.end])
	}
	return fields, nil
}
func assistantTestControls(raw []byte, allowed ...string) (map[string]json.RawMessage, error) {
	fields, err := assistantTestObject(raw)
	if err != nil {
		return nil, err
	}
	for name := range fields {
		if !contains(allowed, name) {
			return nil, errAssistantRunInvalid
		}
	}
	return fields, nil
}
func assistantTestString(raw []byte) (string, error) {
	var text string
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &text) != nil {
		return "", errAssistantRunInvalid
	}
	return text, nil
}
func assistantTestUnavailable() error {
	return failure.ToolInput("Test proposal fact-type validation is unavailable in the connected Runtime. Update to a compatible reviewed Runtime before proposing cases (Desk #582). No cases or proposal were saved.")
}
func readAssistantTestMatrix(raw []byte) (assistantTestMatrix, error) {
	m := assistantTestMatrix{Raw: bytes.Clone(raw)}
	if len(raw) > 1048576 {
		return m, errAssistantRunInvalid
	}
	fields, err := assistantTestObject(raw)
	if err != nil {
		return m, err
	}
	if string(fields["matrixVersion"]) != `"3"` {
		return m, errAssistantRunInvalid
	}
	if m.Rows, err = assistantChatArray(fields["cases"]); err != nil || len(m.Rows) > 256 {
		return m, errAssistantRunInvalid
	}
	seen := map[string]bool{}
	for _, row := range m.Rows {
		f, err := assistantTestControls(row, "id", "origin", "facts", "evidenceAvailability", "supportedExtensions", "expectedDisposition", "expectedHandoffTarget", "expectedErrorClass", "expectedErrorPhase", "focus", "specSection", "cites")
		if err != nil {
			return m, err
		}
		id, err := assistantTestString(f["id"])
		if err != nil || strings.TrimSpace(id) == "" || seen[id] || f["facts"] == nil || (f["expectedDisposition"] != nil) == (f["expectedErrorClass"] != nil) {
			return m, errAssistantRunInvalid
		}
		if f["expectedErrorClass"] != nil {
			if _, err := assistantTestString(f["expectedErrorClass"]); err != nil {
				return m, err
			}
		}
		seen[id] = true
		m.IDs = append(m.IDs, id)
	}
	if v, ok := fields["intentionalFactTypes"]; ok {
		entries, err := assistantChatArray(v)
		if err != nil || len(entries) > 1024 {
			return m, errAssistantRunInvalid
		}
		for _, entry := range entries {
			f, err := assistantTestControls(entry, "caseId", "path", "within", "reason")
			if err != nil {
				return m, err
			}
			var held assistantTestIntentional
			if readAssistantHostReceipt(entry, &held) != nil || held.CaseID == "" || strings.TrimSpace(held.Reason) == "" || runcontext.U16Length(held.Reason) > 2000 || f["path"] == nil {
				return m, errAssistantRunInvalid
			}
			m.Intentional = append(m.Intentional, held)
		}
	}
	if v, ok := fields["sourceMappings"]; ok {
		m.Mappings, err = assistantTestObject(v)
		if err != nil {
			return m, err
		}
	}
	delete(fields, "sourceMappings")
	delete(fields, "intentionalFactTypes")
	m.Runtime = assistantChatEncodeObject(fields)
	return m, nil
}

// Only the two documented Desk metadata members are removed. Everything else
// reaches Runtime unchanged, including unknown matrix members for its refusal.
func (r *assistantRuntime) testTool(ctx context.Context, name string, args map[string]string) (map[string]json.RawMessage, string, error) {
	capture := &assistantTestRuntimeReceipt{}
	ctx = context.WithValue(ctx, assistantTestRuntimeReceiptKey{}, capture)
	raw, err := r.rpc(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, "", assistantTestUnavailable()
	}
	fields, err := readAssistantTestPayload(raw)
	return fields, capture.ID, err
}

func readAssistantTestPayload(raw []byte) (map[string]json.RawMessage, error) {
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
	}
	if json.Unmarshal(raw, &result) != nil || result.IsError {
		return nil, assistantTestUnavailable()
	}
	body := result.Structured
	if body == nil {
		var parts []string
		for _, c := range result.Content {
			if c.Type == "text" {
				parts = append(parts, c.Text)
			}
		}
		body = []byte(strings.Join(parts, "\n"))
	}
	fields, err := assistantTestObject(body)
	if err != nil || string(fields["contractVersion"]) != `"1"` || string(fields["specVersion"]) != `"0.2.0-draft"` {
		return nil, assistantTestUnavailable()
	}
	for key := range fields {
		for _, reserved := range []string{"contractVersion", "specVersion", "status", "findings", "results", "factCheckVersion", "factChecks", "contract"} {
			if key != reserved && strings.EqualFold(key, reserved) {
				return nil, assistantTestUnavailable()
			}
		}
	}
	return fields, nil
}
func readAssistantTestFindings(raw []byte) ([]assistantTestFinding, error) {
	list, err := assistantChatArray(raw)
	if err != nil {
		return nil, assistantTestUnavailable()
	}
	out := []assistantTestFinding{}
	for _, row := range list {
		f, err := assistantTestObject(row)
		if err != nil {
			return nil, assistantTestUnavailable()
		}
		var value assistantTestFinding
		for _, name := range []string{"code", "path", "message"} {
			if _, err := assistantTestString(f[name]); err != nil {
				return nil, assistantTestUnavailable()
			}
		}
		if json.Unmarshal(row, &value) != nil {
			return nil, assistantTestUnavailable()
		}
		out = append(out, value)
	}
	return out, nil
}
func assistantTestValidationReport(report map[string]json.RawMessage, m assistantTestMatrix) ([]assistantTestFinding, []assistantTestFactRow, error) {
	errors, err := readAssistantTestFindings(report["findings"])
	if err != nil {
		return nil, nil, err
	}
	rows, err := assistantChatArray(report["results"])
	if err != nil || len(rows) != len(m.Rows) {
		return nil, nil, assistantTestUnavailable()
	}
	for i, raw := range rows {
		f, err := assistantTestObject(raw)
		if err != nil {
			return nil, nil, assistantTestUnavailable()
		}
		var index int
		if json.Unmarshal(f["index"], &index) != nil || f["index"] == nil || index != i || func() bool { id, _ := assistantTestString(f["id"]); return id != m.IDs[i] }() {
			return nil, nil, assistantTestUnavailable()
		}
		status, _ := assistantTestString(f["status"])
		held, err := readAssistantTestFindings(f["findings"])
		if err != nil || !contains([]string{"valid", "invalid"}, status) || (status == "valid") != (len(held) == 0) {
			return nil, nil, assistantTestUnavailable()
		}
		errors = append(errors, held...)
	}
	status, _ := assistantTestString(report["status"])
	if !contains([]string{"valid", "invalid"}, status) || (status == "valid") != (len(errors) == 0) {
		return nil, nil, assistantTestUnavailable()
	}
	for _, f := range errors {
		if f.Code == "MATRIX-LIMIT" {
			return nil, nil, failure.ToolInput("The proposal exceeds Runtime matrix limits. Reduce the requested suite size; nothing was saved.")
		}
	}
	if len(errors) > 0 {
		return errors, nil, nil
	}
	if string(report["factCheckVersion"]) != `"1"` {
		return nil, nil, assistantTestUnavailable()
	}
	factRows, err := assistantChatArray(report["factChecks"])
	if err != nil || len(factRows) != len(m.Rows) {
		return nil, nil, assistantTestUnavailable()
	}
	facts := []assistantTestFactRow{}
	for i, raw := range factRows {
		var row assistantTestFactRow
		if readAssistantHostReceipt(raw, &row) != nil || row.Index != i || row.ID != m.IDs[i] || !contains([]string{"complete", "unavailable"}, row.Status) || row.Findings == nil || row.Status == "unavailable" && row.Message == "" {
			return nil, nil, assistantTestUnavailable()
		}
		fields, _ := assistantTestObject(raw)
		nested, err := assistantChatArray(fields["findings"])
		if err != nil {
			return nil, nil, assistantTestUnavailable()
		}
		for _, finding := range nested {
			var exact assistantTestFactFinding
			if readAssistantHostReceipt(finding, &exact) != nil {
				return nil, nil, assistantTestUnavailable()
			}
			// Keep raw member types: []string decoding turns null into "".
			advice, err := assistantTestObject(finding)
			if err != nil {
				return nil, nil, assistantTestUnavailable()
			}
			types, err := assistantChatArray(advice["expectedTypes"])
			if err != nil {
				return nil, nil, assistantTestUnavailable()
			}
			for _, rawType := range types {
				if _, err := assistantTestString(rawType); err != nil {
					return nil, nil, assistantTestUnavailable()
				}
			}
		}
		for _, f := range row.Findings {
			if f.Code != "FACT-NOT-COMPARABLE" || len(f.ExpectedTypes) == 0 {
				return nil, nil, assistantTestUnavailable()
			}
		}
		facts = append(facts, row)
	}
	return errors, facts, nil
}
func assistantTestFactReview(rows []assistantTestFactRow, m assistantTestMatrix) []assistantTestFinding {
	out := []assistantTestFinding{}
	used := make([]int, len(m.Intentional))
	for _, row := range rows {
		if row.Status != "complete" {
			out = append(out, assistantTestFinding{"FACT-CHECK-UNAVAILABLE", "/facts", "Runtime could not check every fact type."})
			continue
		}
		for _, f := range row.Findings {
			found := false
			for i, x := range m.Intentional {
				if x.CaseID == row.ID && x.Path == f.Path && (x.Within == nil) == (f.Within == nil) && (x.Within == nil || *x.Within == *f.Within) {
					used[i]++
					found = true
				}
			}
			if !found {
				out = append(out, assistantTestFinding{"FACT-TYPE-REVIEW", "/facts" + f.Path, "Preserve the intended value. Correct accidental encoding or explain this deliberate wrong-type test in intentionalFactTypes; do not change the expected answer."})
			}
		}
	}
	seen := map[string]bool{}
	for i, x := range m.Intentional {
		key := x.CaseID + "\x00" + x.Path + "\x00absent"
		if x.Within != nil {
			key = x.CaseID + "\x00" + x.Path + "\x00present" + *x.Within
		}
		if used[i] != 1 || seen[key] {
			out = append(out, assistantTestFinding{"FACT-TYPE-REVIEW", "/intentionalFactTypes", "Explain each current fact-type finding exactly once."})
		}
		seen[key] = true
	}
	return out
}
func assistantTestSourceReview(m assistantTestMatrix, sources []json.RawMessage) []assistantTestFinding {
	ids := map[string]bool{}
	for _, raw := range sources {
		var source struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &source)
		ids[source.ID] = source.ID != ""
	}
	for id, raw := range m.Mappings {
		if !contains(m.IDs, id) {
			return []assistantTestFinding{{"SOURCE-MAPPING", "/sourceMappings", "Map existing case fact pointers to supplied source IDs only."}}
		}
		fields, err := assistantTestObject(raw)
		if err != nil {
			return []assistantTestFinding{{"SOURCE-MAPPING", "/sourceMappings", "Source mappings must be objects."}}
		}
		for path, v := range fields {
			source, err := assistantTestString(v)
			if err != nil || !strings.HasPrefix(path, "/") || !ids[source] {
				return []assistantTestFinding{{"SOURCE-MAPPING", "/sourceMappings", "Map existing case fact pointers to supplied source IDs only."}}
			}
		}
	}
	return nil
}
