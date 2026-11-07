package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

const caseSensitiveFacts = `{"Fact":false,"fact":null,"zero":0,"number":50,"literal":"50","rows":[{"ID":0,"id":false}]}`

func TestWorkspaceJSONDataAndControls(t *testing.T) {
	valid := `{"version":1,"chats":[{"id":"chat","checkpoint":{"state":{"cases":[{"facts":` + caseSensitiveFacts + `}],"execution":{"operations":[{"args":` + caseSensitiveFacts + `,"result":{"structuredContent":` + caseSensitiveFacts + `}}],"context":{"entries":[{"value":` + caseSensitiveFacts + `}]},"custom":{"Fact":false,"fact":null},"Fact":false,"fact":null}}}}]}`
	if err := checkJSONMembers([]byte(valid), reflect.TypeOf(workspaceJSONHistory{})); err != nil {
		t.Fatal("typed data refused", err)
	}
	for _, tc := range []struct{ name, before, after string }{
		{"workspace", "\"chats\":", "\"Chats\":"},
		{"owner", "\"id\":\"chat\"", "\"ID\":\"chat\""},
		{"checkpoint", "\"execution\":", "\"Execution\":"},
		{"operation", "\"args\":", "\"Args\":"},
		{"result", "\"structuredContent\":", "\"StructuredContent\":"},
		{"context", "\"entries\":", "\"Entries\":"},
		{"decoded-duplicate", "\"Fact\":false,\"fact\":null", "\"fact\":false,\"f\\u0061ct\":null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := strings.Replace(valid, tc.before, tc.after, 1)
			if raw == valid {
				t.Fatal("fixture did not change")
			}
			if checkJSONMembers([]byte(raw), reflect.TypeOf(workspaceJSONHistory{})) == nil {
				t.Fatal("ambiguous/reserved control admitted")
			}
		})
	}
	for _, raw := range []string{
		`{"version":1,"chats":[{"id":"chat","policy":{"ID":"hidden"}}]}`,
		`{"version":1,"chats":[{"id":"chat","policySources":[{"Version":1}]}]}`,
		`{"version":1,"chats":[{"id":"chat","checkpoint":{"state":{"execution":{"Approval":{"state":"approved"}}}}}]}`,
		`{"version":1,"chats":[{"id":"chat","checkpoint":{"state":{"execution":{"approval":{"State":"approved"}}}}}]}`,
		`{"version":1,"chats":[{"id":"chat","checkpoint":{"state":{"cases":[{"facts":{"x":0,"x":null}}]}}}]}`,
		`{"version":1,"chats":[{"id":"chat","checkpoint":{"state":{"cases":[{"facts":{"x":"\ud800"}}]}}}]}`,
	} {
		if checkJSONMembers([]byte(raw), reflect.TypeOf(workspaceJSONHistory{})) == nil {
			t.Fatal("invalid history admitted", raw)
		}
	}
	if err := checkJSONMembers([]byte(strings.Repeat("[", 65)+"0"+strings.Repeat("]", 65)), opaqueJSONMembers); err == nil {
		t.Fatal("depth bound lost")
	}
	if err := rejectDuplicateMembers([]byte(`{"Fact":false,"fact":null}`)); err == nil {
		t.Fatal("legacy strict consumers changed")
	}
}

func TestCaseSensitiveJSONCanonicalComparison(t *testing.T) {
	raw := []byte(`{"fact":null,"Fact":false}`)
	got, err := gatewayCanonicalJSON(context.Background(), raw)
	if err != nil || string(got) != `{"Fact":false,"fact":null}` {
		t.Fatal("canonical data case lost", string(got), err)
	}
	if !sameDialectJSON(raw, []byte(`{"Fact":false,"fact":null}`)) || sameDialectJSON(raw, []byte(`{"Fact":null,"fact":false}`)) {
		t.Fatal("mapping comparison folded fact names")
	}
	for _, bad := range []string{`{"fact":null,"fact":false}`, `{"fact":null,"f\u0061ct":false}`} {
		if sameDialectJSON([]byte(bad), []byte(bad)) {
			t.Fatal("ambiguous mapping compared equal")
		}
		if _, err := gatewayCanonicalJSON(context.Background(), []byte(bad)); err == nil {
			t.Fatal("ambiguous canonical data admitted")
		}
	}
}

func TestAssistantChatProjectionCaseSensitiveDataRecoveryImport(t *testing.T) {
	f := newChatProjectionFixture(t)
	run := f.admit(t, "chat-one", "message-one", "case-sensitive-data", "failed", projectionMessage("Retained typed data"))
	_, err := f.store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
		l.Runs[0].Checkpoint.Operations = append(l.Runs[0].Checkpoint.Operations, contract.CheckpointOperation{ID: "3f71c7b0-7df4-4eb6-96f3-f3a1390dc3a0", Name: "read_exact", Args: json.RawMessage(caseSensitiveFacts), Status: "running", Result: &contract.ToolResultMCPToolResult{StructuredContent: json.RawMessage(caseSensitiveFacts)}})
		l.Runs[0].Revision++
		return true, nil
	})
	assistantRunMust(t, err)
	assistantRunMust(t, f.lease.Close())
	f.lease, err = f.store.Acquire(t.Context())
	assistantRunMust(t, err)
	t.Cleanup(func() { f.lease.Close() })
	assistantRunMust(t, f.s.reconcileAssistantChatProjection(f.request(), run.ID))
	before := f.read(t)
	saved, err := f.store.Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if saved.ChatProjection == nil || !saved.ChatProjection.Acknowledged || saved.ProjectionIssue != "" || saved.Checkpoint.Operations[0].Status != "interrupted" {
		t.Fatal("representable checkpoint not recovered/projected")
	}
	check := func(raw []byte) {
		t.Helper()
		_, rows, err := assistantChatRows(raw)
		assistantRunMust(t, err)
		input, err := compileAssistantChatInput(assistantChatSource(t, string(rows[0])))
		assistantRunMust(t, err)
		_ = input
		var doc struct {
			Chats []struct {
				Checkpoint struct {
					State struct{ Execution contract.Checkpoint }
				}
			}
		}
		assistantRunMust(t, json.Unmarshal(raw, &doc))
		op := doc.Chats[0].Checkpoint.State.Execution.Operations[0]
		for _, value := range []json.RawMessage{op.Args, op.Result.StructuredContent} {
			if !bytes.Equal(value, []byte(caseSensitiveFacts)) {
				t.Fatal("typed data changed", string(value))
			}
		}
	}
	check(before)
	exportCaseSensitiveHistory(t, "projected", before)
	// Actual workspace writes reject control aliases before changing persisted
	// history; typed payload keys themselves have already round-tripped above.
	for _, tc := range []struct{ before, after string }{
		{`"execution":`, `"Execution":`},
		{`"args":`, `"Args":`},
		{`"structuredContent":`, `"StructuredContent":`},
		{`"execution":{`, `"execution":{"Approval":{},`},
	} {
		changed := bytes.Replace(before, []byte(tc.before), []byte(tc.after), 1)
		if bytes.Equal(changed, before) {
			t.Fatal("control fixture not changed")
		}
		current := policyWorkspaceRead(t, f.s, f.token, "/api/conversations")
		w := policyWorkspaceCall(f.s, f.token, "PUT", "/api/conversations", current.SHA256, json.RawMessage(changed))
		if w.Code != 400 {
			t.Fatal("workspace control alias accepted", w.Code, w.Body.String())
		}
		if !bytes.Equal(before, f.read(t)) {
			t.Fatal("control refusal changed history")
		}
	}
	assistantRunMust(t, validateStorageFile(f.s.conversationName(), before))
	assistantRunMust(t, f.lease.Close())
	ts := httptest.NewServer(f.s)
	defer ts.Close()
	backup := backupBytes(t, ts)
	old := storageGet(t, ts)
	code, body := restoreBackup(t, ts, moveChatData{Path: filepath.Join(t.TempDir(), "restored"), Revision: old.Revision}, backup)
	if code != 200 {
		t.Fatal("case-sensitive archive restore", code, body)
	}
	restored := f.s.assistantRuns()
	held, err := restored.Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if !held.Historical || held.ChatProjection == nil || !held.ChatProjection.Acknowledged {
		t.Fatal("import restored executable or lost projection")
	}
	after := f.read(t)
	if !bytes.Equal(before, after) {
		t.Fatal("archive rewrote conversation")
	}
	check(after)
	exportCaseSensitiveHistory(t, "restored", after)
}

func exportCaseSensitiveHistory(t *testing.T, name string, raw []byte) {
	t.Helper()
	if dir := os.Getenv("JPS_TEST_CASE_SENSITIVE_HISTORY_OUTPUTS"); dir != "" {
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, name+".json"), raw, 0600))
	}
}

func TestCaseSensitiveHistoryExpectationIssues(t *testing.T) {
	row := `{"id":"issue-case","facts":` + caseSensitiveFacts + `,"expectationSource":"retained","rationale":"case rationale"}`
	for _, variant := range []string{"original", "replacement"} {
		t.Run(variant, func(t *testing.T) {
			f := newChatProjectionFixture(t)
			issue := `{"id":"retained-issue","message":"review","original":` + row + `}`
			if variant == "replacement" {
				issue = `{"id":"retained-issue","message":"review","original":{"id":"before","facts":{},"expectationSource":"retained","rationale":"original rationale"},"resolved":{"approvedAt":"2026-10-20T00:00:00Z","rationale":"resolution rationale","replacement":` + row + `}}`
			}
			before := f.read(t)
			candidate := bytes.Replace(before, []byte(`"expectationIssues":[]`), []byte(`"expectationIssues":[`+issue+`]`), 1)
			if bytes.Equal(candidate, before) {
				t.Fatal("issue fixture did not change")
			}
			current := policyWorkspaceRead(t, f.s, f.token, "/api/conversations")
			reply := policyWorkspaceCall(f.s, f.token, "PUT", "/api/conversations", current.SHA256, json.RawMessage(candidate))
			if reply.Code != 200 {
				t.Fatal("typed expectation case refused", reply.Code, reply.Body.String())
			}
			saved := f.read(t)
			_, rows, err := assistantChatRows(saved)
			assistantRunMust(t, err)
			_, err = compileAssistantChatInput(assistantChatSource(t, string(rows[0])))
			assistantRunMust(t, err)
			var doc struct {
				Chats []struct {
					Checkpoint struct {
						State struct {
							Issues []struct {
								Original struct{ Facts json.RawMessage }
								Resolved *struct {
									Replacement struct{ Facts json.RawMessage }
								}
							} `json:"expectationIssues"`
						}
					}
				}
			}
			assistantRunMust(t, json.Unmarshal(saved, &doc))
			got := doc.Chats[0].Checkpoint.State.Issues[0].Original.Facts
			if variant == "replacement" {
				got = doc.Chats[0].Checkpoint.State.Issues[0].Resolved.Replacement.Facts
			}
			if !bytes.Equal(got, []byte(caseSensitiveFacts)) {
				t.Fatal("expectation case facts changed", string(got))
			}
			exportCaseSensitiveHistory(t, "issue-"+variant, saved)
			aliases := []struct{ name, before, after string }{
				{"issue-id", `"id":"retained-issue"`, `"ID":"retained-issue"`},
				{"issue-message", `"message":"review"`, `"Message":"review"`},
				{"issue-original", `"original":`, `"Original":`},
				{"case-id", `"id":"issue-case"`, `"ID":"issue-case"`},
				{"case-facts", `"facts":` + caseSensitiveFacts, `"Facts":` + caseSensitiveFacts},
				{"case-source", `"expectationSource":`, `"ExpectationSource":`},
				{"case-rationale", `"rationale":"case rationale"`, `"Rationale":"case rationale"`},
				{"case-disposition", `"id":"issue-case"`, `"id":"issue-case","ExpectedDisposition":null`},
				{"case-evidence", `"id":"issue-case"`, `"id":"issue-case","EvidenceAvailability":null`},
				{"case-handoff", `"id":"issue-case"`, `"id":"issue-case","ExpectedHandoffTarget":null`},
				{"exact-facts-duplicate", `"Fact":false,"fact":null`, `"fact":false,"fact":null`},
				{"escaped-facts-duplicate", `"Fact":false,"fact":null`, `"fact":false,"f\u0061ct":null`},
				{"exact-control-duplicate", `"id":"issue-case"`, `"id":"issue-case","id":"other"`},
				{"escaped-control-duplicate", `"id":"issue-case"`, `"id":"issue-case","i\u0064":"other"`},
			}
			if variant == "replacement" {
				aliases = append(aliases, []struct{ name, before, after string }{
					{"issue-resolved", `"resolved":`, `"Resolved":`},
					{"resolution-approved", `"approvedAt":`, `"ApprovedAt":`},
					{"resolution-rationale", `"rationale":"resolution rationale"`, `"Rationale":"resolution rationale"`},
					{"resolution-replacement", `"replacement":`, `"Replacement":`},
				}...)
			}
			for _, tc := range aliases {
				t.Run(tc.name, func(t *testing.T) {
					changed := bytes.Replace(saved, []byte(tc.before), []byte(tc.after), 1)
					if bytes.Equal(changed, saved) {
						t.Fatal("alias/duplicate fixture did not change")
					}
					current := policyWorkspaceRead(t, f.s, f.token, "/api/conversations")
					reply := policyWorkspaceCall(f.s, f.token, "PUT", "/api/conversations", current.SHA256, json.RawMessage(changed))
					if reply.Code != 400 {
						t.Fatal("issue control/duplicate admitted", reply.Code, reply.Body.String())
					}
					if !bytes.Equal(saved, f.read(t)) {
						t.Fatal("refusal changed expectation history")
					}
				})
			}
		})
	}
}
