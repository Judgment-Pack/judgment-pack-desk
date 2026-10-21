package desk

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

type assistantSearchConfiguration struct {
	Gateway          researchGateway
	Connection       string
	Revision         string
	Provider         string
	PreferenceDigest string
	StatusDigest     string
}

// Only the pinned latest saved turn and the configured mode select consent.
// Historical tool output, natural-language intent and old researchChoice do not.
func assistantSearchChoice(source assistantRunSource, mode string) (string, error) {
	var turn map[string]json.RawMessage
	var chat map[string]json.RawMessage
	if json.Unmarshal(source.Request, &turn) != nil || json.Unmarshal(source.Original, &chat) != nil {
		return "", errAssistantRunInvalid
	}
	if mode == "provided" {
		return "provided", nil
	}
	if raw, exists := turn["researchChoice"]; exists {
		var choice string
		if json.Unmarshal(raw, &choice) != nil || !contains([]string{"ask", "research", "provided"}, choice) {
			return "", errAssistantRunInvalid
		}
		return choice, nil
	}
	var authoring string
	_ = json.Unmarshal(chat["mode"], &authoring)
	if authoring == "web-research" {
		return "research", nil
	}
	return "ask", nil
}

// Search preference is project configuration. It never selects executable or
// credential paths. The canonical companion supplies bounded local metadata.
func (s *Server) assistantSearchPreference(source assistantRunSource) (string, string, string, error) {
	var chat struct {
		SearchConnection *string `json:"searchConnection"`
		ResearchMode     *string `json:"researchMode"`
	}
	if json.Unmarshal(source.Original, &chat) != nil {
		return "", "", "", errAssistantRunInvalid
	}
	file := s.readProjectFile("jpack-search.json")
	preference := struct {
		Version    int     `json:"version"`
		Connection *string `json:"connection"`
		Mode       string  `json:"mode"`
	}{Version: 1, Mode: "auto"}
	digest := "absent"
	if file.err != nil {
		return "", "", "", file.err
	}
	if file.present {
		fields, err := configMembers(file.data)
		if len(file.data) > 4096 || err != nil || len(fields) != 3 || json.Unmarshal(file.data, &preference) != nil || preference.Version != 1 {
			return "", "", "", errAssistantSearch
		}
		for _, f := range fields {
			if !contains([]string{"version", "connection", "mode"}, f.name) {
				return "", "", "", errAssistantSearch
			}
		}
		digest = digestOf(file.data)
	}
	mode := preference.Mode
	if chat.ResearchMode != nil {
		mode = *chat.ResearchMode
	}
	if !contains([]string{"auto", "provided"}, mode) {
		return "", "", "", errAssistantRunInvalid
	}
	selected := preference.Connection
	if chat.SearchConnection != nil {
		selected = chat.SearchConnection
	}
	id := ""
	if selected != nil {
		id = *selected
		if !researchSourceName.MatchString(id) {
			return "", "", "", errAssistantRunInvalid
		}
	}
	return id, mode, digest, nil
}
func (s *Server) assistantSearchConfiguration(ctx context.Context, source assistantRunSource) (assistantSearchConfiguration, error) {
	return s.assistantSearchConfigurationCurrent(ctx, source, true)
}

func (s *Server) assistantSearchConfigurationCurrent(ctx context.Context, source assistantRunSource, start bool) (assistantSearchConfiguration, error) {
	var held assistantSearchConfiguration
	id, _, digest, err := s.assistantSearchPreference(source)
	if err != nil {
		return held, err
	}
	held.Connection, held.PreferenceDigest = id, digest
	gateway, err := s.configuredResearchCurrent(start)
	if err != nil {
		return held, assistantSearchFailure("gateway-unavailable", "")
	}
	held.Gateway = gateway
	if !gateway.managedLocal || s.localGateway == nil || id == "" {
		return held, assistantSearchFailure("setup-required", "")
	}
	companion := s.connectionCompanion("web-search", true)
	if companion == nil {
		return held, assistantSearchFailure("setup-required", "")
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := companion.call(bounded, s.localGateway.bundle, platformPath(s.assistant.dir, "gateway-connections"), "status", json.RawMessage(`{}`), "web-search", !start)
	if err != nil {
		return held, assistantSearchFailure("setup-required", "")
	}
	var status struct {
		Version     int    `json:"version"`
		Error       string `json:"error"`
		Connections []struct {
			ID       string `json:"id"`
			Revision string `json:"revision"`
			Provider string `json:"provider"`
		} `json:"connections"`
	}
	if len(raw) > 64<<10 || !uniqueJSON(raw) || json.Unmarshal(raw, &status) != nil || status.Version != 1 || status.Error != "" || status.Connections == nil || len(status.Connections) > 32 {
		return held, assistantSearchFailure("setup-required", "")
	}
	seen := map[string]bool{}
	for _, c := range status.Connections {
		if !researchSourceName.MatchString(c.ID) || !researchConnectionRevision.MatchString(c.Revision) || !researchSourceName.MatchString(c.Provider) || seen[c.ID] {
			return held, assistantSearchFailure("setup-required", "")
		}
		seen[c.ID] = true
		if c.ID == id {
			held.Revision, held.Provider = c.Revision, c.Provider
		}
	}
	if held.Revision == "" {
		return held, assistantSearchFailure("connect-required", "")
	}
	// Usage/day metadata is mutable accounting, not a configuration revision.
	held.StatusDigest = digestOf(assistantChatJSON([]string{held.Connection, held.Revision, held.Provider}))
	return held, nil
}
func (e *assistantRunExecution) recheckAssistantSearch(ctx context.Context, held assistantSearchConfiguration) error {
	if err := e.recheckAssistantInput(ctx); err != nil {
		return err
	}
	current, err := e.service.server.assistantSearchConfiguration(ctx, e.source)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, held) {
		return assistantSearchFailure("source-changed", held.Provider)
	}
	return ctx.Err()
}

// The attachment owner holds the short storage lock. This callback performs
// trusted local inspection only. No companion, verifier or capacity wait here.
func (e *assistantRunExecution) recheckAssistantSearchLocked(ctx context.Context, held assistantSearchConfiguration) error {
	if err := e.authorizeAssistantResearchLocked(ctx); err != nil {
		return err
	}
	gateway, err := e.service.server.configuredResearchCurrent(false)
	if err != nil {
		return err
	}
	id, _, digest, err := e.service.server.assistantSearchPreference(e.source)
	if err != nil {
		return err
	}
	if gateway != held.Gateway || id != held.Connection || digest != held.PreferenceDigest {
		return errAssistantRunScope
	}
	return nil
}

type assistantSearchFailureError struct{ code, provider string }

func (e *assistantSearchFailureError) Error() string {
	return "Web search could not complete. No new links were authorized."
}
func assistantSearchFailure(code, provider string) error {
	return &assistantSearchFailureError{code, provider}
}
func assistantSearchReply(message string, structured any, isError bool) runcontext.ToolResult {
	var value *runcontext.Value
	if structured != nil {
		value, _ = runcontext.ParseJSON(assistantChatJSON(structured))
	}
	return runcontext.ResultOf([]runcontext.Content{runcontext.TextContent(message)}, value).WithIsError(isError)
}
func assistantSearchError(err error, step assistantSearchStep) runcontext.ToolResult {
	code := "search-failed"
	var source *researchSourceError
	if errors.As(err, &source) {
		code = source.code
		if code == "source-response-too-large" {
			code = "search-over-limit"
		}
	}
	var authored *assistantSearchFailureError
	if errors.As(err, &authored) {
		code = authored.code
		if step.Provider == "" {
			step.Provider = authored.provider
		}
	}
	if errors.Is(err, errAssistantSearch) {
		code = "search-verification-failed"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = "search-canceled"
	}
	// Normalize through the shared catalog; browser's search code remains separate.
	canonical := failure.Alias(code)
	if canonical == "" {
		canonical = map[string]string{"search-verification-failed": "invalid-response", "search-proof-failed": "invalid-response", "search-over-limit": "context-limit", "search-canceled": "cancelled", "gateway-unavailable": "configuration", "setup-required": "configuration", "connect-required": "configuration", "provider-unavailable": "unavailable", "search-budget-exhausted": "provider-limit"}[code]
	}
	if canonical == "" {
		canonical = "unknown"
	}
	normalized := failure.Authored(canonical, "Web search did not complete. No new links were authorized. No automatic retry was made.", "desk")
	if source != nil {
		metadata := failure.Normalize(source, "tool", time.Now())
		metadata.Detail = normalized.Failure.Detail
		normalized = failure.NewError(metadata)
	}
	text, raw := failure.ToolFailure(normalized)
	var structured map[string]any
	_ = json.Unmarshal(raw, &structured)
	structured["searchStep"] = step
	if code == "search-turn-limit" || code == "search-chat-limit" {
		return assistantSearchReply("This request has reached its source search limit. Use retained sources or start a new conversation.", structured, true)
	}
	structured["searchFailure"] = code
	return assistantSearchReply(text, structured, true)
}
func (e *assistantRunExecution) assistantSearchTools() (*assistantHostRegistry, error) {
	schema := json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","maxLength":2000,"description":"A concise public search query. Do not include private attachments or secrets."}},"required":["query"],"additionalProperties":false}`)
	tool := runcontext.HostTool{Name: assistantSearchTool, Effect: "read", Delegation: "research", InputSchema: schema, Description: "Search public web sources using the configured search connection. Results are leads, never page evidence. Respect the recorded research choice. At most three searches per current user turn. Do not repeat unknown or interrupted operations."}
	// Only an already-consented current request reads local connection metadata
	// while preparing presentation. Ask/provided never acquire a source.
	_, mode, _, preferenceErr := e.service.server.assistantSearchPreference(e.source)
	choice, choiceErr := assistantSearchChoice(e.source, mode)
	if preferenceErr == nil && choiceErr == nil && choice == "research" {
		if configured, err := e.service.server.assistantSearchConfiguration(e.ctx, e.source); err == nil {
			tool.Presentation = &contract.ToolOfferHostToolPresentation{Provider: configured.Provider}
		}
	}
	tool.Execute = func(ctx context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
		query, err := readAssistantSearchQuery(raw)
		if err != nil {
			return assistantHostFailure(err), nil
		}
		return e.invokeObservedHost(ctx, assistantSearchTool, raw, func(ctx context.Context, _ bool) (assistantHostObservation, error) {
			return e.executeAssistantSearch(ctx, query)
		})
	}
	return newAssistantHostRegistry([]runcontext.HostTool{tool})
}

// Exact host invocation identity is established by invokeObservedHost. A child
// uses the same current saved turn and the same durable project reservation.
func (e *assistantRunExecution) assistantSearchHost(ctx context.Context, query string) (string, error) {
	call := runcontext.CallIDOf(ctx)
	callback, _ := ctx.Value(assistantHostCallbackKey{}).(string)
	record, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return "", err
	}
	for _, op := range record.Operations {
		var in assistantHostIntent
		if op.Kind == "host" && op.Name == assistantSearchTool && op.State == "dispatched" && readAssistantHostReceipt(op.Intent, &in) == nil && in.CallID == call && in.CallbackID == callback {
			q, err := readAssistantSearchQuery(in.Arguments)
			if err == nil && q == query {
				return op.ID, nil
			}
		}
	}
	return "", errAssistantRunScope
}
