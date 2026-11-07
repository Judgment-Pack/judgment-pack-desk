package desk

// Explicit checks retain observations beside the original run. They never
// mutate its execution checkpoint, allowance, pending question or response.
import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"time"
)

type assistantPackRecheckRequest struct {
	ID             string `json:"id"`
	DocumentDigest string `json:"documentDigest"`
	SuiteDigest    string `json:"suiteDigest"`
}
type assistantPackRecheck struct {
	Validation     []byte              `json:"validation,omitempty"`
	Test           []byte              `json:"test,omitempty"`
	ID             string              `json:"id"`
	OperationID    string              `json:"operationId"`
	DocumentDigest string              `json:"documentDigest"`
	Read           assistantTestRead   `json:"read"`
	Check          *assistantPackCheck `json:"check,omitempty"`
}

func validateAssistantPackRechecks(run assistantRunRecord) error {
	if len(run.PackChecks) > 64 || run.SavedPack == nil && len(run.PackChecks) > 0 {
		return errAssistantRunInvalid
	}
	seen := map[string]bool{}
	for _, receipt := range run.PackChecks {
		if !assistantRunText(receipt.ID, 128) || seen[receipt.ID] || !validAssistantTestRead(receipt.Read) {
			return errAssistantRunInvalid
		}
		seen[receipt.ID] = true
		var p *assistantSavedPackProposal
		for _, out := range run.DomainOutputs {
			if out.OperationID == receipt.OperationID {
				p = out.PackProposal
			}
		}
		if p == nil || digestOf([]byte(p.Text)) != receipt.DocumentDigest || !assistantSavedPackTargetSame(run.SavedPack.Read, receipt.Read) {
			return errAssistantRunInvalid
		}
		if receipt.Check != nil && (!validAssistantPackCheck(*receipt.Check, p.Text, receipt.Read) || !assistantPackRecheckResults(receipt)) {
			return errAssistantRunInvalid
		}
	}
	return nil
}
func assistantSavedPackTargetSame(a, b assistantTestRead) bool {
	// A user-owned explicit recheck may use a freshly captured suite; it may
	// never move the original saved file, declaration or source identity.
	a.Suite, b.Suite = nil, nil
	a.SuitePresent, b.SuitePresent = false, false
	a.CoverageRunID, b.CoverageRunID = "", ""
	return reflect.DeepEqual(a, b)
}
func validAssistantPackCheck(c assistantPackCheck, text string, read assistantTestRead) bool {
	suite, err := assistantTestSuite(read.Suite)
	if err != nil {
		return false
	}
	cases, _ := assistantChatArray(suite["cases"])
	_, ids, err := assistantSavedPackMatrix(read.Suite)
	if err != nil || c.DocumentDigest != digestOf([]byte(text)) || c.SuiteKey != assistantTestIdentity(suite["cases"]) || c.TotalCases != len(cases) || c.Diagnostics == nil {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, c.At); err != nil {
		return false
	}
	return !(len(c.Report) > 0 && (!c.Valid || !validAssistantSavedPackReport(c.Report, ids)) || c.Valid && len(ids) > 0 && len(c.Report) == 0)
}
func (s *Server) handleAssistantPackRecheck(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	var request assistantPackRecheckRequest
	if assistantRunRequest(r, &request) != nil || !assistantRunText(request.ID, 128) || !assistantRunDigest(request.DocumentDigest) || !assistantRunDigest(request.SuiteDigest) {
		assistantRunHTTPError(w, errAssistantRunInvalid)
		return
	}
	check, err := s.recheckAssistantPackProposal(r, r.PathValue("run"), request)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	writeJSON(w, 200, check)
}

// Caller holds one existing private-data transaction. No Runtime work is done
// until the exact selected candidate and fresh suite have been retained.
func (s *Server) assistantPackRecheckSourceLocked(r *http.Request, run assistantRunRecord, digest string) (assistantTestRead, string, string, error) {
	if err := s.authorizeAssistantRun(r, run); err != nil {
		return assistantTestRead{}, "", "", err
	}
	if run.SavedPack == nil {
		return assistantTestRead{}, "", "", errAssistantRunScope
	}
	data, err := s.openChatData()
	if err != nil {
		return assistantTestRead{}, "", "", err
	}
	defer data.root.Close()
	name, err := resolveConversationName(data.root, s.conversationIdentity())
	if err != nil {
		return assistantTestRead{}, "", "", err
	}
	full, err := readPrivateData(data.root, name, maxConversationBytes)
	if err != nil {
		return assistantTestRead{}, "", "", err
	}
	if err = s.authorizePolicyConversations(r, full); err != nil {
		return assistantTestRead{}, "", "", err
	}
	_, rows, err := assistantChatRows(full)
	if err != nil {
		return assistantTestRead{}, "", "", err
	}
	index, err := assistantChatSelected(rows, run.Binding.Owner.ID)
	if err != nil {
		return assistantTestRead{}, "", "", err
	}
	chat, err := assistantTestObject(rows[index])
	if err != nil {
		return assistantTestRead{}, "", "", err
	}
	if err = validateAssistantSavedPackSource(rows[index], run); err != nil {
		return assistantTestRead{}, "", "", err
	}
	cp, err := assistantTestObject(chat["checkpoint"])
	if err != nil {
		return assistantTestRead{}, "", "", err
	}
	state, err := assistantTestObject(cp["state"])
	if err != nil {
		return assistantTestRead{}, "", "", err
	}
	candidates, err := assistantChatArray(state["candidates"])
	if err != nil || len(candidates) == 0 {
		return assistantTestRead{}, "", "", errAssistantRunScope
	}
	latest, err := assistantTestObject(candidates[len(candidates)-1])
	if err != nil {
		return assistantTestRead{}, "", "", err
	}
	text, err := assistantTestString(latest["text"])
	if err != nil || digestOf([]byte(text)) != digest {
		return assistantTestRead{}, "", "", errAssistantRunScope
	}
	operation := ""
	for _, out := range run.DomainOutputs {
		if out.PackProposal != nil && out.PackProposal.Text == text {
			operation = out.OperationID
		}
	}
	if operation == "" || string(latest["responseId"]) != string(assistantChatJSON(assistantChatResponseID(run.ID))) {
		return assistantTestRead{}, "", "", errAssistantRunScope
	}
	config, _, err := s.assistantDecisionDeclarations()
	if err != nil {
		return assistantTestRead{}, "", "", err
	}
	current, err := s.readAssistantTestTargetLocked(data.root, name, config, run.SavedPack.Basis.PackID)
	if err != nil {
		return assistantTestRead{}, "", "", err
	}
	if !assistantSavedPackTargetSame(run.SavedPack.Read, current) {
		return assistantTestRead{}, "", "", errAssistantRunScope
	}
	return current, text, operation, nil
}
func (s *Server) recheckAssistantPackProposal(r *http.Request, id string, request assistantPackRecheckRequest) (assistantPackCheck, error) {
	store := s.assistantRuns()
	var held assistantPackRecheck
	var text string
	var replay bool
	_, err := store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		run, err := assistantChatProjectionRun(ledger, id)
		if err != nil {
			return false, err
		}
		current, candidate, operation, err := s.assistantPackRecheckSourceLocked(r, *run, request.DocumentDigest)
		if err != nil {
			return false, err
		}
		suite, _ := assistantTestSuite(current.Suite)
		if digestOf([]byte(assistantTestIdentity(suite["cases"]))) != request.SuiteDigest {
			return false, errAssistantRunConflict
		}
		for _, receipt := range run.PackChecks {
			if receipt.ID == request.ID {
				if receipt.DocumentDigest != request.DocumentDigest || receipt.OperationID != operation || !assistantTestSame(receipt.Read, current) || receipt.Check == nil {
					return false, errAssistantRunConflict
				}
				held = receipt
				replay = true
				return false, nil
			}
		}
		if len(run.PackChecks) >= 64 {
			return false, errAssistantRunInvalid
		}
		held = assistantPackRecheck{ID: request.ID, OperationID: operation, DocumentDigest: request.DocumentDigest, Read: current}
		text = candidate
		run.PackChecks = append(run.PackChecks, held)
		run.Revision++
		return true, nil
	})
	if err != nil {
		return assistantPackCheck{}, err
	}
	if replay {
		return *held.Check, nil
	}
	fresh := func(ctx context.Context) error {
		_, err := store.transaction(ctx, func(ledger *assistantRunLedger) (bool, error) {
			run, err := assistantChatProjectionRun(ledger, id)
			if err != nil {
				return false, err
			}
			current, candidate, operation, err := s.assistantPackRecheckSourceLocked(r.WithContext(ctx), *run, request.DocumentDigest)
			if err != nil {
				return false, err
			}
			if candidate != text || operation != held.OperationID || !assistantTestSame(held.Read, current) {
				return false, errAssistantRunConflict
			}
			return false, nil
		})
		return err
	}
	return checkAssistantSavedPack(r.Context(), held.Read, text, assistantSavedPackCheckPorts{Fresh: fresh, Call: func(ctx context.Context, name string, args any) (json.RawMessage, error) {
		if err := fresh(ctx); err != nil {
			return nil, err
		}
		raw, err := s.callAssistantPackSnapshot(ctx, name, args)
		if err != nil {
			return nil, err
		}
		observed, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer finish()
		_, err = store.transaction(observed, func(ledger *assistantRunLedger) (bool, error) {
			run, err := assistantChatProjectionRun(ledger, id)
			if err != nil {
				return false, err
			}
			for i := range run.PackChecks {
				receipt := &run.PackChecks[i]
				if receipt.ID == request.ID {
					if name == "validate" {
						if receipt.Validation != nil {
							return false, errAssistantRunConflict
						}
						receipt.Validation = bytes.Clone(raw)
					} else {
						if receipt.Test != nil {
							return false, errAssistantRunConflict
						}
						receipt.Test = bytes.Clone(raw)
					}
					run.Revision++
					return true, nil
				}
			}
			return false, errAssistantRunConflict
		})
		return raw, err
	}, Retain: func(ctx context.Context, check *assistantPackCheck) error {
		if check == nil {
			return fresh(ctx)
		}
		_, err := store.transaction(ctx, func(ledger *assistantRunLedger) (bool, error) {
			run, err := assistantChatProjectionRun(ledger, id)
			if err != nil {
				return false, err
			}
			current, candidate, operation, err := s.assistantPackRecheckSourceLocked(r.WithContext(ctx), *run, request.DocumentDigest)
			if err != nil || candidate != text || operation != held.OperationID || !assistantTestSame(held.Read, current) {
				return false, errAssistantRunConflict
			}
			for i := range run.PackChecks {
				receipt := &run.PackChecks[i]
				if receipt.ID == request.ID {
					if receipt.Check != nil {
						return false, errAssistantRunConflict
					}
					receipt.Check = check
					run.Revision++
					return true, nil
				}
			}
			return false, errAssistantRunConflict
		})
		return err
	}})
}

// Run only the two finite, model-free snapshot tools in a private temporary
// directory. No workspace path or user-selected executable enters the child.
func (s *Server) callAssistantPackSnapshot(ctx context.Context, name string, args any) (json.RawMessage, error) {
	if name != "validate" && name != "experimental_test_cases" {
		return nil, errAssistantRunInvalid
	}
	where, err := os.MkdirTemp("", "jpack-desk-pack-check-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(where)
	root, err := os.OpenRoot(where)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	dir := heldDir{file: file, path: where, info: info}
	input := []byte{}
	for _, frame := range []any{map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "desk-pack-check", "version": "1"}}}, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}} {
		raw, _ := json.Marshal(frame)
		input = append(input, raw...)
		input = append(input, '\n')
	}
	out, err := runRuntimeInputLimit(ctx, s.cfg.JpackBin, dir, input, readLimit, "mcp")
	if err != nil {
		return nil, err
	}
	var result json.RawMessage
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		frame, err := assistantTestObject(line)
		if err != nil {
			return nil, err
		}
		if string(frame["id"]) != "2" {
			continue
		}
		if result != nil || frame["error"] != nil || frame["result"] == nil {
			return nil, errAssistantRunInvalid
		}
		result = frame["result"]
	}
	if result == nil {
		return nil, errAssistantRunInvalid
	}
	return result, nil
}

func assistantPackRecheckResults(receipt assistantPackRecheck) bool {
	validation, err := assistantSavedPackPayload(receipt.Validation)
	if err != nil {
		return false
	}
	c := receipt.Check
	if c == nil || (string(validation["status"]) == `"valid"`) != c.Valid || !assistantHostArgumentsMatch(validation["diagnostics"], assistantChatEncodeArray(c.Diagnostics)) {
		return false
	}
	if len(c.Report) == 0 {
		return len(receipt.Test) == 0
	}
	report, err := assistantSavedPackPayload(receipt.Test)
	return err == nil && assistantHostArgumentsMatch(assistantChatEncodeObject(report), c.Report)
}
