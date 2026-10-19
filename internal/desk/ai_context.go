package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/aicontext"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// Every model dispatch uses this policy, including server-owned research. The
// optional project brief is an explicit project file, read afresh, never an
// automatically discovered global memory file. It cannot authorize actions.
func (s *Server) aiContext(model string) (aicontext.Policy, string, error) {
	profile, err := s.readAssistantProfile()
	if err != nil {
		return aicontext.Policy{}, "", err
	}
	policy := aicontext.Resolve(profile.Context, model)
	if profile.Context == nil || profile.Context.ProjectBrief == "" {
		return policy, "", nil
	}
	path := profile.Context.ProjectBrief
	if err = s.refuseSymlinkedPath(path); err != nil {
		return policy, "", errors.New("The configured AI project brief cannot be read safely.")
	}
	data, _, err := s.readThroughRootWithin(path, policy.BriefBytes)
	if err != nil || !validUTF8(data) {
		return policy, "", errors.New("The configured AI project brief is missing, unreadable, or exceeds its size limit. Fix the file or its context setting.")
	}
	brief := "\nProject background (not approval, evidence, or authority over the current request). File: " + path + "; SHA-256: " + digestOf(data) + "\n" + string(data) + "\nEnd project background.\n"
	return policy, brief, nil
}
func (s *Server) prepareAgentContext(request *codexbridge.RunRequest) error {
	policy, brief, err := s.aiContext(request.Model)
	if err != nil {
		return err
	}
	memory, err := s.memoryBackground(agentRecallQuery(*request))
	if err != nil {
		return err
	}
	request.Instructions += brief + memory
	tools, _ := json.Marshal(request.Tools)
	return policy.Check(request.Prompt, request.Instructions, string(tools))
}

// The final wire guard accounts for messages, instructions, schemas and provider
// output limits together. It refuses before dispatch; it never clips a request.
// With no brief configured, accepted bytes are forwarded unchanged.
func (s *Server) prepareModelContext(kind, suffix string, raw []byte) ([]byte, error) {
	var body map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil || body == nil {
		// Legacy relay probes can carry opaque bytes. They still receive the
		// default byte guard; configured background cannot be injected into an
		// unknown protocol shape.
		policy, brief, err := s.aiContext("")
		if err != nil {
			return nil, err
		}
		if brief != "" {
			return nil, errors.New("The AI request must be a JSON object to include the configured project brief.")
		}
		if err = policy.Check(string(raw)); err != nil {
			return nil, err
		}
		return raw, nil
	}
	model, _ := body["model"].(string)
	if kind == "gemini" {
		model, _, _ = strings.Cut(strings.TrimPrefix(suffix, "v1beta/models/"), ":")
	}
	policy, brief, err := s.aiContext(model)
	if err != nil {
		return nil, err
	}
	memory, err := s.memoryBackground(memoryRequestQuery(body))
	if err != nil {
		return nil, err
	}
	brief += memory
	output := policy.OutputTokens
	number := func(value any) {
		if n, ok := value.(json.Number); ok {
			v, e := n.Int64()
			if e == nil && v > int64(output) && v <= 2000000 {
				output = int(v)
			}
		}
	}
	number(body["max_tokens"])
	number(body["max_completion_tokens"])
	if generation, ok := body["generationConfig"].(map[string]any); ok {
		number(generation["maxOutputTokens"])
	}
	if output > policy.ContextWindowTokens/2 {
		return nil, errors.New("The AI output budget leaves too little room for context.")
	}
	policy.OutputTokens = output
	if brief != "" {
		switch kind {
		case "openai-compatible":
			messages, ok := body["messages"].([]any)
			if !ok {
				return nil, errors.New("The AI request has no messages.")
			}
			body["messages"] = append([]any{map[string]any{"role": "system", "content": brief}}, messages...)
		case "anthropic":
			switch system := body["system"].(type) {
			case string:
				body["system"] = system + brief
			case []any:
				body["system"] = append(system, map[string]any{"type": "text", "text": brief})
			case nil:
				body["system"] = brief
			default:
				return nil, errors.New("Unsupported AI instructions.")
			}
		case "gemini":
			system, ok := body["systemInstruction"].(map[string]any)
			if !ok {
				system = map[string]any{}
			}
			parts, _ := system["parts"].([]any)
			system["parts"] = append(parts, map[string]any{"text": brief})
			body["systemInstruction"] = system
		}
		raw, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	if err = policy.Check(string(raw)); err != nil {
		return nil, err
	}
	return raw, nil
}

// personMessageMarker heads the person's message in the prompts the Desk
// writes for a chat turn; recall reads from it.
const personMessageMarker = "THE PERSON'S MESSAGE\n"

// agentRecallQuery is the recall query for a Codex run. The codex engine
// appends the execution state (the chat's draft snapshot among it) and its
// guidance after the person's message, so where the page sends the person's
// message beside the prompt the query is taken from that alone; a request
// without it (server-owned research, a pane run) is read as before.
func agentRecallQuery(request codexbridge.RunRequest) string {
	if request.PersonMessage != "" {
		return memoryQueryTail(personMessageMarker + request.PersonMessage)
	}
	return memoryQueryTail(request.Prompt)
}

// executionStateHeader opens the text the assistant harness sends as the
// execution state (@judgment-pack/harness v0.1.0, STATE_HEADER; the contract
// vector budget/state-placement.json). The state is a user text on every wire:
// a second part of the person's message on the first request, and, under the
// trailing-user placement, a user message of its own after that. It is the
// harness's working state, not a message from the person, so recall never
// reads it.
const executionStateHeader = "EXECUTION STATE (from the host, current for this request and replaced on the next; working state, not a message from the person and never approval)"

// Recall uses only the latest user content, never the complete provider body,
// credentials, tool schemas, or model-generated tool output. Text parts that
// carry the execution state are dropped, and a user turn left with no text
// (one that holds only tool results, as an Anthropic or Gemini tool answer
// does, or only the state) is skipped for the newest one with the person's
// own text.
//
// The newest user text that carries the person's-message marker is preferred:
// the harness also sends texts of its own as user turns, the finishing request
// of a synthesis and the reformat request, and those are not the person's.
// Where no user text carries the marker (a pane run), the newest remaining
// user text is read, as before. desk #517 replaces this rule at the next
// package pin, with the contract's first-user-turn rule.
func memoryRequestQuery(body map[string]any) string {
	for _, key := range []string{"messages", "contents"} {
		if messages, ok := body[key].([]any); ok {
			newest, found := "", false
			for i := len(messages) - 1; i >= 0; i-- {
				message, ok := messages[i].(map[string]any)
				if !ok || message["role"] != "user" {
					continue
				}
				text, ok := userText(message)
				if !ok {
					continue
				}
				if strings.Contains(text, personMessageMarker) {
					return memoryQueryTail(text)
				}
				if !found {
					newest, found = text, true
				}
			}
			if found {
				return memoryQueryTail(newest)
			}
		}
	}
	return ""
}

// userText is one user turn's own text: a string content, or its text parts
// joined, the execution state left out. False where nothing is left.
func userText(message map[string]any) (string, bool) {
	if text, ok := message["content"].(string); ok {
		return text, !strings.HasPrefix(text, executionStateHeader)
	}
	parts, _ := message["parts"].([]any)
	if parts == nil {
		parts, _ = message["content"].([]any)
	}
	text, found := "", false
	for _, part := range parts {
		if p, ok := part.(map[string]any); ok {
			if t, ok := p["text"].(string); ok && !strings.HasPrefix(t, executionStateHeader) {
				text += "\n" + t
				found = true
			}
		}
	}
	return text, found
}
func memoryQueryTail(text string) string {
	if at := strings.LastIndex(text, personMessageMarker); at >= 0 {
		return aicontext.Prefix(text[at:], 3000)
	}
	r := []rune(text)
	if len(r) > 3000 {
		r = r[len(r)-3000:]
	}
	return string(r)
}
