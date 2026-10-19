package desk

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// This demand check is independent of Jobs startup, availability and credentials.
// A transport or worker cannot discard the installation's retained profile.
func (s *Server) manualExecutionDemand() (bool, error) {
	if s.policyAuthorityMachine == nil {
		return false, nil
	}
	r, err := s.policyAuthorityMachine.registry()
	if err != nil {
		return false, err
	}
	w, found, err := policyMachineWorkspaceFor(r, s.project)
	if err != nil || !found {
		return false, policyMachineRefusal()
	}
	if w.Execution == nil {
		return false, nil
	}
	if w.Execution.State != "enabled" {
		return false, policyMachineRefusal()
	}
	return true, nil
}

// Host-reviewed read/check tools. Runtime annotations and advertised capabilities
// cannot add execution authority. Unknown tools remain unavailable in this profile.
var manualReadRuntimeTools = map[string]bool{
	"get_schema": true, "list_examples": true, "get_example": true, "validate": true,
	"list_packs": true, "get_pack": true, "get_artifact_version_options": true,
	"experimental_list_graphs": true, "experimental_get_graph": true,
	"experimental_test_packs": true, "experimental_test_graphs": true, "test_conformance": true,
	"get_dialect_contract": true, "get_dialect_schema": true, "get_dialect_reference": true,
	"dialect_validate": true, "dialect_check": true, "dialect_canonicalize": true, "dialect_digest": true,
}

func manualRuntimeFrameAllowed(raw []byte) bool {
	if !utf8.Valid(raw) || rejectDuplicateMembers(raw) != nil {
		return false
	}
	var frame map[string]json.RawMessage
	if json.Unmarshal(raw, &frame) != nil || frame == nil {
		return false
	}
	for key := range frame {
		if !contains([]string{"jsonrpc", "id", "method", "params", "result", "error"}, key) {
			return false
		}
	}
	if string(frame["jsonrpc"]) != `"2.0"` {
		return false
	}
	var method string
	if json.Unmarshal(frame["method"], &method) != nil {
		// Replies to server requests grant no invocation authority.
		return frame["method"] == nil && frame["id"] != nil && (frame["result"] != nil || frame["error"] != nil)
	}
	switch method {
	case "initialize", "ping", "tools/list", "resources/list", "resources/templates/list", "resources/read", "prompts/list", "prompts/get", "notifications/initialized", "notifications/cancelled":
		return true
	case "tools/call":
		var p map[string]json.RawMessage
		if json.Unmarshal(frame["params"], &p) != nil || p == nil {
			return false
		}
		for k := range p {
			if k != "name" && k != "arguments" && k != "_meta" {
				return false
			}
		}
		var name string
		if json.Unmarshal(p["name"], &name) != nil {
			return false
		}
		if manualReadRuntimeTools[name] {
			return true
		}
		if name != "experimental_evaluate" {
			return false
		}
		var args map[string]json.RawMessage
		if json.Unmarshal(p["arguments"], &args) != nil || args == nil {
			return false
		}
		for key := range args {
			if strings.EqualFold(key, "rehearsal") && key != "rehearsal" {
				return false
			}
		}
		// Require the exact wire boolean. Absence, aliases and caller truthiness do
		// not turn an operational evaluator into a rehearsal.
		return strings.TrimSpace(string(args["rehearsal"])) == "true"
	default:
		return false
	}
}
func (s *Server) admitRuntimeFrame(c *conn, raw []byte) bool {
	enabled, err := s.manualExecutionDemand()
	if err == nil && (!enabled || manualRuntimeFrameAllowed(raw)) {
		return true
	}
	var frame map[string]json.RawMessage
	_ = json.Unmarshal(raw, &frame)
	id := json.RawMessage("null")
	if value := frame["id"]; len(value) > 0 {
		var scalar any
		if json.Unmarshal(value, &scalar) == nil {
			switch scalar.(type) {
			case string, float64:
				id = value
			}
		}
	}
	answer, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32003, "message": "Operational execution requires the registered manual Jobs admission path.", "data": map[string]string{"code": "policy-execution-required"}}})
	c.send(answer)
	return false
}
