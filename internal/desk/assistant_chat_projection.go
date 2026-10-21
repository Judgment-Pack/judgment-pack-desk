package desk

// Terminal chat projection is a private host workflow, not a JSON patch API.
// A prepared selected-chat copy is immutable; pending execution stays in its ledger.
import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

var errAssistantChatProjectionUnsupported = errors.New("This response contains data not supported by chat projection yet. Its run is retained.")
var errAssistantChatProjectionQuestion = fmt.Errorf("%w: unresolved question", errAssistantChatProjectionUnsupported)

type assistantChatProjection struct {
	Version      int    `json:"version"`
	SourceDigest string `json:"sourceDigest"`
	EventDigest  string `json:"eventDigest"`
	Output       []byte `json:"output"`
	Digest       string `json:"digest"`
	ResponseID   string `json:"responseId"`
	Acknowledged bool   `json:"acknowledged"`
}
type assistantChatProjectionView struct {
	ResponseID   string `json:"responseId"`
	Digest       string `json:"digest"`
	Acknowledged bool   `json:"acknowledged"`
}

func assistantChatProjectionPublic(p *assistantChatProjection) *assistantChatProjectionView {
	if p == nil {
		return nil
	}
	return &assistantChatProjectionView{ResponseID: p.ResponseID, Digest: p.Digest, Acknowledged: p.Acknowledged}
}

// Lease/storage recovery already maps running checkpoint operations to
// interrupted. Normalize that display-only transition before hashing so a
// prepared terminal output survives recovery without rewriting its exact bytes.
// Copy the slice: calculating a projection must never mutate the original journal.
func assistantChatProjectionCheckpoint(run assistantRunRecord) *contract.Checkpoint {
	if run.Checkpoint == nil {
		return nil
	}
	cp := *run.Checkpoint
	if run.Checkpoint.Operations != nil {
		cp.Operations = append([]contract.CheckpointOperation{}, run.Checkpoint.Operations...)
	}
	for i := range cp.Operations {
		if cp.Operations[i].Status == "running" {
			cp.Operations[i].Status = "interrupted"
		}
	}
	return &cp
}
func assistantChatEventDigest(run assistantRunRecord) string {
	// Preserve prepared receipts produced before the first domain variant existed.
	if len(run.DomainOutputs) == 0 {
		raw, _ := json.Marshal(struct {
			State      string
			Checkpoint any
			Events     []assistantRunEvent
		}{run.State, assistantChatProjectionCheckpoint(run), run.Events})
		return digestOf(raw)
	}
	raw, _ := json.Marshal(struct {
		State      string
		Checkpoint any
		Events     []assistantRunEvent
		Outputs    []assistantDomainOutput
	}{run.State, assistantChatProjectionCheckpoint(run), run.Events, run.DomainOutputs})
	return digestOf(raw)
}
func validateAssistantChatProjection(run assistantRunRecord) error {
	if run.ProjectionIssue != "" && run.ProjectionIssue != "proposal-target" && run.ProjectionIssue != "pending-question" && run.ProjectionIssue != "saved-chat-validation" && run.ProjectionIssue != "unsupported-unicode" {
		return errAssistantRunInvalid
	}
	p := run.ChatProjection
	if p == nil {
		return nil
	}
	if p.Version != 1 || p.SourceDigest != run.Binding.Owner.Revision || p.EventDigest != assistantChatEventDigest(run) || p.ResponseID != assistantChatResponseID(run.ID) || !assistantRunDigest(p.Digest) || len(p.Output) == 0 || len(p.Output) > maxConversationBytes || digestOf(p.Output) != p.Digest || !assistantRunTerminal(run.State) || run.ProjectionIssue != "" {
		return errAssistantRunInvalid
	}
	chat, err := assistantChatObject(p.Output)
	if err != nil {
		return err
	}
	var id string
	_ = json.Unmarshal(chat["id"], &id)
	if id != run.Binding.Owner.ID {
		return errAssistantRunScope
	}
	return nil
}

// Validate the selected output before it can become a prepared receipt. Raw
// checkpoint arguments/results need scalar-valid JSON. Candidate text is itself
// serialized JSON inside a JSON string, so check that second layer explicitly.
// Readers and original strings are unchanged; unsupported data stays in the run.
func assistantChatProjectionUnicode(output []byte) bool {
	if !validUTF8(output) || jsonscalar.ValidateEscapes(output) != nil {
		return false
	}
	chat, err := assistantChatObject(output)
	if err != nil {
		return false
	}
	checkpoint, err := assistantChatObject(chat["checkpoint"])
	if err != nil {
		return false
	}
	state, err := assistantChatObject(checkpoint["state"])
	if err != nil {
		return false
	}
	candidates, err := assistantChatArray(state["candidates"])
	if err != nil {
		return false
	}
	for _, raw := range candidates {
		candidate, err := assistantChatObject(raw)
		if err != nil {
			return false
		}
		var text string
		if json.Unmarshal(candidate["text"], &text) != nil || !validUTF8([]byte(text)) || !json.Valid([]byte(text)) || jsonscalar.ValidateEscapes([]byte(text)) != nil {
			return false
		}
	}
	return true
}

// Write RawMessage values directly: json.Marshal would compact unrelated bytes.
func assistantChatEncodeObject(fields map[string]json.RawMessage) []byte {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out bytes.Buffer
	out.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			out.WriteByte(',')
		}
		name, _ := json.Marshal(k)
		out.Write(name)
		out.WriteByte(':')
		out.Write(fields[k])
	}
	out.WriteByte('}')
	return out.Bytes()
}
func assistantChatEncodeArray(items []json.RawMessage) []byte {
	var out bytes.Buffer
	out.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			out.WriteByte(',')
		}
		out.Write(item)
	}
	out.WriteByte(']')
	return out.Bytes()
}
func assistantChatJSON(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }
func assistantChatObject(raw []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if !validUTF8(raw) || !uniqueJSON(raw) || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, errAssistantRunInvalid
	}
	return fields, nil
}
func assistantChatArray(raw []byte) ([]json.RawMessage, error) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil {
		return nil, errAssistantRunInvalid
	}
	return items, nil
}
func assistantChatRows(raw []byte) (map[string]json.RawMessage, []json.RawMessage, error) {
	fields, err := assistantChatObject(raw)
	if err != nil {
		return nil, nil, err
	}
	var version int
	if json.Unmarshal(fields["version"], &version) != nil || version != 1 {
		return nil, nil, errAssistantRunInvalid
	}
	rows, err := assistantChatArray(fields["chats"])
	if err != nil || len(rows) > 256 {
		return nil, nil, errAssistantRunInvalid
	}
	ids := map[string]bool{}
	for _, raw := range rows {
		chat, e := assistantChatObject(raw)
		if e != nil {
			return nil, nil, e
		}
		var id string
		_ = json.Unmarshal(chat["id"], &id)
		if !researchChatID.MatchString(id) || ids[id] {
			return nil, nil, errAssistantRunInvalid
		}
		ids[id] = true
	}
	return fields, rows, nil
}
func assistantChatSelected(rows []json.RawMessage, id string) (int, error) {
	for i, raw := range rows {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		var held string
		_ = json.Unmarshal(fields["id"], &held)
		if held == id {
			return i, nil
		}
	}
	return -1, errAssistantRunNotFound
}
func assistantChatRequest(raw []byte, requestID, digest string) bool {
	chat, e := assistantChatObject(raw)
	if e != nil {
		return false
	}
	checkpoint, e := assistantChatObject(chat["checkpoint"])
	if e != nil {
		return false
	}
	state, e := assistantChatObject(checkpoint["state"])
	if e != nil {
		return false
	}
	turns, e := assistantChatArray(state["turns"])
	if e != nil {
		return false
	}
	seen := map[string]bool{}
	found := false
	for _, raw := range turns {
		turn, e := assistantChatObject(raw)
		if e != nil {
			return false
		}
		var id, role string
		_ = json.Unmarshal(turn["id"], &id)
		_ = json.Unmarshal(turn["role"], &role)
		if !assistantChatProjectionID(id) || seen[id] {
			return false
		}
		seen[id] = true
		if id == requestID {
			found = role == "user" && digestOf(raw) == digest
		}
	}
	return found
}
func assistantChatResponseID(runID string) string {
	return "response-" + digestOf([]byte(runID))[:32]
}
func assistantChatProjectionID(id string) bool {
	if len(id) < 1 || len(id) > 100 {
		return false
	}
	for _, r := range id {
		if r != '-' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// Existing strings/raw turn and candidate slices are retained unchanged. New
// canonical create proposals use the Go package's documented sorted-map form;
// this is not a reconstruction of original model JSON bytes (#537).
func compileAssistantChatProjection(source []byte, run assistantRunRecord, at string) ([]byte, error) {
	chat, err := assistantChatObject(source)
	if err != nil {
		return nil, err
	}
	cp, err := assistantChatObject(chat["checkpoint"])
	if err != nil {
		return nil, err
	}
	state, err := assistantChatObject(cp["state"])
	if err != nil {
		return nil, err
	}
	for key, value := range map[string]any{"brief": "", "seedUrls": []string{}, "turns": []any{}, "questions": []any{}, "candidates": []any{}, "cases": []any{}, "expectationIssues": []any{}, "unknowns": []string{}, "sessions": []string{}, "revisionsUsed": 0, "responses": []any{}} {
		if state[key] == nil {
			state[key] = assistantChatJSON(value)
		}
	}
	turns, err := assistantChatArray(state["turns"])
	if err != nil {
		return nil, err
	}
	candidates, err := assistantChatArray(state["candidates"])
	if err != nil {
		return nil, err
	}
	responses, err := assistantChatArray(state["responses"])
	if err != nil {
		return nil, err
	}
	if err := validateAssistantDomainOutputs(run); err != nil {
		return nil, err
	}
	if err := validateAssistantPolicyProjectionSource(source, run); err != nil {
		return nil, err
	}
	graphOutputs := []assistantDomainOutput{}
	for _, out := range run.DomainOutputs {
		if out.Kind == "graph" {
			graphOutputs = append(graphOutputs, out)
		}
	}
	if len(graphOutputs) > 0 {
		existing := chat["graphDrafts"]
		if existing == nil {
			existing = []byte("[]")
		}
		drafts, err := assistantChatArray(existing)
		if err != nil {
			return nil, err
		}
		if len(drafts)+len(graphOutputs) > 16 {
			return nil, errAssistantRunInvalid
		}
		seen := map[string]bool{}
		for _, raw := range drafts {
			var d graphDraft
			if json.Unmarshal(raw, &d) != nil {
				return nil, errAssistantRunInvalid
			}
			seen[d.DraftID] = true
		}
		for _, out := range graphOutputs {
			if seen[out.Draft.DraftID] {
				return nil, errAssistantRunConflict
			}
			seen[out.Draft.DraftID] = true
			drafts = append(drafts, assistantChatJSON(out.Draft))
		}
		chat["graphDrafts"] = assistantChatEncodeArray(drafts)
	}
	if err := appendAssistantDialectRecipeOutputs(chat, run.DomainOutputs); err != nil {
		return nil, err
	}
	if err := appendAssistantPolicyOutputs(chat, run.DomainOutputs); err != nil {
		return nil, err
	}
	// Validate association identities rather than pairing by tool name or prose.
	ids := map[string]bool{}
	for _, raw := range turns {
		var turn struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &turn) != nil || !assistantChatProjectionID(turn.ID) || ids[turn.ID] {
			return nil, errAssistantRunInvalid
		}
		ids[turn.ID] = true
	}
	responseIDs := map[string]bool{}
	for _, raw := range responses {
		var row struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &row) != nil || !assistantChatProjectionID(row.ID) || responseIDs[row.ID] || row.ID == assistantChatResponseID(run.ID) {
			return nil, errAssistantRunConflict
		}
		responseIDs[row.ID] = true
	}
	responseID := assistantChatResponseID(run.ID)
	items := []map[string]any{}
	calls := map[string]int{}
	completed := map[string]bool{}
	notices := []string{}
	messageID := ""
	work := map[string]any{}
	for _, held := range run.Events {
		event := held.Event
		switch {
		case event.Message != nil:
			id := responseID + "-message-" + digestOf([]byte(held.ID))[:16]
			if ids[id] {
				return nil, errAssistantRunConflict
			}
			ids[id] = true
			turn := map[string]any{"id": id, "role": "assistant", "kind": "message", "text": event.Message.Text, "at": at}
			if run.State != "complete" {
				turn["interrupted"] = true
			}
			turns = append(turns, assistantChatJSON(turn))
			messageID = id
		case event.ToolCall != nil:
			call := event.ToolCall
			if call.CallID == nil || !assistantRunText(*call.CallID, 256) || !assistantRunText(call.Name, 200) {
				return nil, errAssistantRunInvalid
			}
			if _, ok := calls[*call.CallID]; ok {
				return nil, errAssistantRunConflict
			}
			calls[*call.CallID] = len(items)
			items = append(items, map[string]any{"id": *call.CallID, "name": call.Name, "status": "interrupted"})
		case event.ToolResult != nil:
			result := event.ToolResult
			if result.CallID == nil {
				return nil, errAssistantRunInvalid
			}
			index, ok := calls[*result.CallID]
			if !ok || completed[*result.CallID] || items[index]["name"] != result.Name {
				return nil, errAssistantRunConflict
			}
			completed[*result.CallID] = true
			status := "complete"
			if result.IsError {
				status = "failed"
			}
			items[index]["status"] = status
		case event.Error != nil:
			if event.Error.Failure != nil {
				held, ok := failure.Read(*event.Error.Failure)
				if !ok {
					return nil, errAssistantRunInvalid
				}
				state["failure"] = assistantChatJSON(held)
			}
			// Retain the canonical error's text separately from artifact-check state.
			notices = append(notices, event.Error.Message)
		case event.Critique != nil:
			c := event.Critique
			// Match the shared saved critic reader; generated event types alone do
			// not validate domain payload bounds. Refuse rather than save unreadable history.
			if len(c.Cases) > 200 || len(c.Incomplete) > 64 {
				return nil, errAssistantRunInvalid
			}
			for _, id := range c.Incomplete {
				if runcontext.U16Length(id) > 200 {
					return nil, errAssistantRunInvalid
				}
			}
			if f := c.Figures; f != nil {
				if f.Model == "" || runcontext.U16Length(f.Model) > 200 || !contains([]string{"model-requests", "agent-runs"}, f.Unit) || f.Requests < 0 || f.Requests > 1000 {
					return nil, errAssistantRunInvalid
				}
				for _, n := range []*int64{f.HarnessCalls, f.InputTokens, f.OutputTokens, f.ReasoningTokens, f.CacheReadTokens, f.CacheWriteTokens} {
					if n != nil && (*n < 0 || *n > 9007199254740991) {
						return nil, errAssistantRunInvalid
					}
				}
			}
			work["critique"] = c.Text
			if len(c.Cases) > 0 || len(c.Incomplete) > 0 || c.Figures != nil {
				disagreed := 0
				for _, row := range c.Cases {
					if !row.Agrees {
						disagreed++
					}
				}
				critic := map[string]any{"compared": len(c.Cases), "disagreed": disagreed, "incomplete": c.Incomplete, "runModel": run.Binding.Model}
				if c.Incomplete == nil {
					critic["incomplete"] = []string{}
				}
				if c.Figures != nil {
					critic["figures"] = c.Figures
				}
				work["critic"] = critic
			}
		case event.Proposal != nil:
			for _, key := range []string{"pack", "graph", "input", "policy", "draftId"} {
				if raw, ok := chat[key]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					return nil, errAssistantChatProjectionUnsupported
				}
			}
			if event.Proposal.Document == nil {
				return nil, errAssistantRunInvalid
			}
			text, err := json.Marshal(event.Proposal.Document)
			if err != nil || !uniqueJSON(text) {
				return nil, errAssistantRunInvalid
			}
			candidates = append(candidates, assistantChatJSON(map[string]any{"text": string(text), "responseId": responseID}))
			unknowns := event.Proposal.Unknowns
			if unknowns == nil {
				unknowns = []string{}
			}
			state["unknowns"] = assistantChatJSON(unknowns)
		case event.UserQuestion != nil:
			// An unresolved interactive question must remain pinned to its canonical
			// continuation in the ledger, rather than become a new chat action.
			return nil, errAssistantChatProjectionQuestion
		}
	}
	work["items"] = items
	work["notices"] = notices
	if run.Checkpoint != nil {
		work["report"] = map[string]any{"requests": run.Checkpoint.Used, "unit": run.Checkpoint.Unit}
		display := *assistantChatProjectionCheckpoint(run)
		// Approval/handoff capabilities stay in the bound ledger. Historical
		// allowance records and used counters are facts, not fresh grants.
		display.Approval = nil
		display.Handoff = nil
		state["execution"] = assistantChatJSON(display)
	}
	response := map[string]any{"id": responseID, "afterTurnId": run.Binding.RequestID, "documents": []any{}, "websites": []any{}, "sourceIds": []string{}, "work": work}
	if messageID != "" {
		response["messageId"] = messageID
		delete(response, "afterTurnId")
	}
	responses = append(responses, assistantChatJSON(response))
	state["turns"] = assistantChatEncodeArray(turns)
	state["candidates"] = assistantChatEncodeArray(candidates)
	state["responses"] = assistantChatEncodeArray(responses)
	status := "stopped"
	if run.State == "complete" {
		status = "ready"
	}
	if run.State == "failed" {
		status = "failed"
	}
	state["status"] = assistantChatJSON(status)
	state["phase"] = assistantChatJSON("conversation")
	cp["state"] = assistantChatEncodeObject(state)
	if cp["sources"] == nil {
		cp["sources"] = []byte("[]")
	}
	chat["checkpoint"] = assistantChatEncodeObject(cp)
	chat["updatedAt"] = assistantChatJSON(at)
	out := assistantChatEncodeObject(chat)
	if len(out) > maxConversationBytes {
		return nil, fmt.Errorf("chat projection exceeds storage bounds")
	}
	return out, nil
}
func assistantChatProjectionTime() string { return time.Now().UTC().Format(time.RFC3339Nano) }
