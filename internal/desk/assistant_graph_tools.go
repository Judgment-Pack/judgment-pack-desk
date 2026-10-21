package desk

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

type assistantDecisionDeclaration struct {
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
	declaration string
}
type assistantDecisions struct {
	Packs  map[string]assistantDecisionDeclaration `json:"packs"`
	Graphs map[string]assistantDecisionDeclaration `json:"graphs"`
}

// A notification-only fixture fence; it cannot replace a read or grant authority.
var testHookAssistantGraphGuidance func()

func (e *assistantRunExecution) graphDeclarations(ctx context.Context) (assistantDecisions, []byte, error) {
	if err := e.Authorize(ctx); err != nil {
		return assistantDecisions{}, nil, err
	}
	return e.service.server.assistantDecisionDeclarations()
}

// Shared authenticated callers own authorization; declaration parsing stays the
// same for dispatch and locked reconciliation of already observed proposals.
func (s *Server) assistantDecisionDeclarations() (assistantDecisions, []byte, error) {
	if _, refusal := s.graphRuntime(); refusal != "" {
		return assistantDecisions{}, nil, failure.ToolInput(refusal)
	}
	file := s.readProjectFile(runtimeConfigName)
	var config assistantDecisions
	if file.err != nil || !file.present || json.Unmarshal(file.data, &config) != nil {
		return config, nil, failure.ToolInput("The current project declarations could not be read.")
	}
	members, err := configMembers(file.data)
	if err != nil {
		return config, nil, failure.ToolInput("The project declarations must have unique members.")
	}
	for _, member := range members {
		for _, name := range []string{"packs", "graphs"} {
			if strings.EqualFold(member.name, name) && member.name != name {
				return config, nil, errAssistantRunInvalid
			}
		}
		if member.name != "packs" && member.name != "graphs" {
			continue
		}
		declarations, err := configMembers(file.data[member.start:member.end])
		if err != nil {
			return config, nil, errAssistantRunInvalid
		}
		entries := config.Packs
		if member.name == "graphs" {
			entries = config.Graphs
		}
		for _, decl := range declarations {
			raw := file.data[member.start+decl.start : member.start+decl.end]
			fields, err := configMembers(raw)
			if err != nil {
				return config, nil, errAssistantRunInvalid
			}
			for _, field := range fields {
				for _, name := range []string{"path", "description"} {
					if strings.EqualFold(field.name, name) && field.name != name {
						return config, nil, errAssistantRunInvalid
					}
				}
			}
			entry := entries[decl.name]
			entry.declaration = digestOf(raw)
			entries[decl.name] = entry
		}
	}

	return config, file.data, nil
}
func assistantDecisionID(id string) bool {
	return len(id) > 0 && len(id) <= 128 && graphDeclarationID.MatchString(id)
}
func assistantDecisionEntry(entries map[string]assistantDecisionDeclaration, id string) (assistantDecisionDeclaration, error) {
	entry, ok := entries[id]
	if !assistantDecisionID(id) || !ok || entry.Path == "" {
		return entry, failure.ToolInput("Choose a declared decision id from list_decisions.")
	}
	path, err := wireRelativePath(entry.Path)
	if err != nil || path != entry.Path {
		return entry, failure.ToolInput("The declared decision must name a project-relative file.")
	}
	return entry, nil
}
func (e *assistantRunExecution) graphDocument(entry assistantDecisionDeclaration) ([]byte, error) {
	file := e.service.server.readProjectFile(entry.Path)
	if file.err != nil || !file.present || !utf8.Valid(file.data) {
		return nil, failure.ToolInput("The declared decision document could not be read.")
	}
	return file.data, nil
}
func assistantDeclarationDigest(entry assistantDecisionDeclaration) string {
	if entry.declaration != "" {
		return entry.declaration
	}
	raw, _ := json.Marshal(entry)
	return digestOf(raw)
}
func assistantGraphSchemas() map[string]json.RawMessage {
	id := map[string]any{"type": "string", "pattern": "^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$", "maxLength": 128}
	text := func(n int) any { return map[string]any{"type": "string", "maxLength": n} }
	schema := func(p map[string]any, required ...string) json.RawMessage {
		if required == nil {
			required = []string{}
		}
		raw, _ := json.Marshal(map[string]any{"type": "object", "properties": p, "required": required, "additionalProperties": false})
		return raw
	}
	return map[string]json.RawMessage{
		"list_decisions":                   schema(map[string]any{}),
		"read_decision":                    schema(map[string]any{"type": map[string]any{"type": "string", "enum": []string{"pack", "graph"}}, "id": id}, "type", "id"),
		"get_graph_authoring_instructions": schema(map[string]any{"relationship": text(20000), "packs": map[string]any{"type": "array", "items": id, "minItems": 1, "maxItems": 64}}, "relationship", "packs"),
		"graph_validate":                   schema(map[string]any{"content": text(maxFileBytes)}, "content"),
		"graph_explain":                    schema(map[string]any{"content": text(maxFileBytes)}, "content"),
		"propose_graph":                    schema(map[string]any{"id": id, "content": text(1048576), "description": text(4096)}, "id", "content"),
		"graph_rehearse":                   schema(map[string]any{"id": id, "inputs": text(maxFileBytes)}, "id", "inputs"),
	}
}
func (e *assistantRunExecution) assistantGraphTools(runtime *assistantRuntime) (*assistantHostRegistry, error) {
	schemas := assistantGraphSchemas()
	descriptions := map[string]string{
		"list_decisions":                   "List the project's declared packs and graphs. Reads declarations; evaluates nothing.",
		"read_decision":                    "Read a declared pack or graph by type and id. Graph reads also inspect its referenced declared packs.",
		"get_graph_authoring_instructions": "Read Runtime's author_graph contract using the exact current declared packs and intended relationship before proposing a graph.",
		"graph_validate":                   "Validate the exact proposed graph document using Runtime. Does not save it.",
		"graph_explain":                    "Explain the exact proposed graph document using Runtime. Does not save or execute it.",
		"propose_graph":                    "Retain a graph draft for human review. Does not save a project file, change configuration or run a decision. Read the current graph before proposing a revision.",
		"graph_rehearse":                   "Rehearse an existing declared graph with explicitly supplied inputs keyed by node id. Ask for missing facts; never invent them. Does not write rows, an audit decision or execute actions.",
	}
	tools := []runcontext.HostTool{}
	for _, name := range []string{"list_decisions", "read_decision", "get_graph_authoring_instructions", "graph_validate", "graph_explain", "propose_graph", "graph_rehearse"} {
		name := name
		t := runcontext.HostTool{Name: name, Description: descriptions[name], InputSchema: schemas[name], Effect: "read"}
		if name == "propose_graph" {
			t.Effect = "propose"
		} else {
			t.Delegation = "read"
		}
		if name == "get_graph_authoring_instructions" {
			t.ContextPolicy = &runcontext.ToolContextPolicy{Reference: true}
		}
		t.Execute = func(ctx context.Context, args json.RawMessage) (runcontext.ToolResult, error) {
			// Decode/bound all arguments before the producer or any Runtime work.
			parsed, err := readAssistantGraphArguments(name, args)
			if err != nil {
				return assistantHostFailure(err), nil
			}
			return e.invokeHost(ctx, name, args, func(ctx context.Context, root bool) (runcontext.ToolResult, *assistantGraphRead, *graphDraft, error) {
				return e.executeGraphTool(ctx, runtime, name, parsed, root)
			})
		}
		tools = append(tools, t)
	}
	return newAssistantHostRegistry(tools)
}

func assistantGraphProposalResult(chatID string, draft graphDraft) (runcontext.ToolResult, error) {
	return assistantHostJSON(map[string]string{"artifactType": "graph", "status": "draft", "draftId": draft.DraftID, "reviewUrl": "/graphs?chat=" + chatID + "&draft=" + draft.DraftID, "message": "Draft retained for review. No project file was written. Open the draft to validate, review and save."})
}

func assistantGraphDocument(content string) bool {
	v, err := runcontext.ParseJSON([]byte(content))
	return err == nil && v.IsObject() && v.Get("formatVersion").IsString() && v.Get("formatVersion").Str() == "1" && v.Get("nodes").IsObject() && v.Get("edges").IsArray()
}

type assistantGraphArguments struct {
	Type         string   `json:"type"`
	ID           string   `json:"id"`
	Content      string   `json:"content"`
	Description  string   `json:"description"`
	Relationship string   `json:"relationship"`
	Packs        []string `json:"packs"`
	Inputs       string   `json:"inputs"`
}

func readAssistantGraphArguments(name string, raw json.RawMessage) (assistantGraphArguments, error) {
	var a assistantGraphArguments
	// Each concrete decoder has only its declared members, including optional ones.
	var err error
	var shape struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	schema, ok := assistantGraphSchemas()[name]
	if !ok {
		return a, failure.ToolInput("Unknown Graph tool.")
	}
	_ = json.Unmarshal(schema, &shape)
	members, memberErr := configMembers(raw)
	if memberErr != nil {
		return a, failure.ToolInput("Provide one JSON object with unique declared arguments.")
	}
	present := map[string]bool{}
	for _, member := range members {
		if _, ok := shape.Properties[member.name]; !ok || bytes.Equal(bytes.TrimSpace(raw[member.start:member.end]), []byte("null")) {
			return a, failure.ToolInput("Use only the declared argument names and types.")
		}
		present[member.name] = true
	}
	for _, key := range shape.Required {
		if !present[key] {
			return a, failure.ToolInput("Provide every required argument.")
		}
	}
	switch name {
	case "list_decisions":
		var v struct{}
		err = assistantHostArguments(raw, &v)
	case "read_decision":
		var v struct {
			Type *string `json:"type"`
			ID   *string `json:"id"`
		}
		err = assistantHostArguments(raw, &v)
		if err == nil && v.Type != nil && v.ID != nil {
			a.Type = *v.Type
			a.ID = *v.ID
		} else {
			err = errAssistantRunInvalid
		}
	case "get_graph_authoring_instructions":
		var v struct {
			Relationship *string  `json:"relationship"`
			Packs        []string `json:"packs"`
		}
		err = assistantHostArguments(raw, &v)
		if err == nil && v.Relationship != nil {
			a.Relationship = *v.Relationship
			a.Packs = v.Packs
		} else {
			err = errAssistantRunInvalid
		}
	case "graph_validate", "graph_explain":
		var v struct {
			Content *string `json:"content"`
		}
		err = assistantHostArguments(raw, &v)
		if err == nil && v.Content != nil {
			a.Content = *v.Content
		} else {
			err = errAssistantRunInvalid
		}
	case "propose_graph":
		var v struct {
			ID          *string `json:"id"`
			Content     *string `json:"content"`
			Description *string `json:"description,omitempty"`
		}
		err = assistantHostArguments(raw, &v)
		if err == nil && v.ID != nil && v.Content != nil {
			a.ID = *v.ID
			a.Content = *v.Content
			if v.Description != nil {
				a.Description = *v.Description
			}
		} else {
			err = errAssistantRunInvalid
		}
	case "graph_rehearse":
		var v struct {
			ID     *string `json:"id"`
			Inputs *string `json:"inputs"`
		}
		err = assistantHostArguments(raw, &v)
		if err == nil && v.ID != nil && v.Inputs != nil {
			a.ID = *v.ID
			a.Inputs = *v.Inputs
		} else {
			err = errAssistantRunInvalid
		}
	default:
		err = errAssistantRunInvalid
	}
	if err != nil || len(a.Content) > maxFileBytes || name == "propose_graph" && len(a.Content) > 1048576 || len(a.Inputs) > maxFileBytes || len(a.Description) > 4096 || len(a.Relationship) > 20000 || len(a.Packs) > 64 || (name == "read_decision" && a.Type != "pack" && a.Type != "graph") || (a.ID != "" && !assistantDecisionID(a.ID)) || (contains([]string{"read_decision", "propose_graph", "graph_rehearse"}, name) && a.ID == "") || (name == "get_graph_authoring_instructions" && len(a.Packs) == 0) {
		return a, failure.ToolInput("Provide the tool's declared arguments within their stated bounds.")
	}
	for _, id := range a.Packs {
		if !assistantDecisionID(id) {
			return a, failure.ToolInput("Choose declared pack ids from list_decisions.")
		}
	}
	return a, nil
}
func (e *assistantRunExecution) executeGraphTool(ctx context.Context, runtime *assistantRuntime, name string, a assistantGraphArguments, root bool) (result runcontext.ToolResult, read *assistantGraphRead, draft *graphDraft, err error) {
	config, raw, err := e.graphDeclarations(ctx)
	if err != nil {
		return result, nil, nil, err
	}
	s := e.service.server
	answer := func(v any) (runcontext.ToolResult, *assistantGraphRead, *graphDraft, error) {
		r, err := assistantHostJSON(v)
		return r, nil, nil, err
	}
	switch name {
	case "list_decisions":
		list := func(entries map[string]assistantDecisionDeclaration) []map[string]string {
			out := []map[string]string{}
			ids := []string{}
			for id := range entries {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				v := map[string]string{"id": id}
				if entries[id].Description != "" {
					v["description"] = entries[id].Description
				}
				out = append(out, v)
			}
			return out
		}
		return answer(map[string]any{"packs": list(config.Packs), "graphs": list(config.Graphs)})
	case "read_decision":
		entries := config.Packs
		if a.Type == "graph" {
			entries = config.Graphs
		}
		entry, err := assistantDecisionEntry(entries, a.ID)
		if err != nil {
			return result, nil, nil, err
		}
		content, err := e.graphDocument(entry)
		if err != nil {
			return result, nil, nil, err
		}
		packs := []map[string]string{}
		if a.Type == "graph" {
			var g struct {
				Nodes map[string]struct {
					Pack string `json:"pack"`
				} `json:"nodes"`
			}
			if json.Unmarshal(content, &g) != nil {
				return result, nil, nil, failure.ToolInput("The declared graph is not a JSON graph document.")
			}
			ids := []string{}
			for _, node := range g.Nodes {
				if !contains(ids, node.Pack) {
					ids = append(ids, node.Pack)
				}
			}
			sort.Strings(ids)
			if len(ids) > 64 {
				return result, nil, nil, failure.ToolInput("Read a graph referencing at most 64 packs.")
			}
			for _, id := range ids {
				p, err := assistantDecisionEntry(config.Packs, id)
				if err != nil {
					return result, nil, nil, err
				}
				body, err := e.graphDocument(p)
				if err != nil {
					return result, nil, nil, err
				}
				packs = append(packs, map[string]string{"id": id, "content": string(body), "sha256": digestOf(body)})
			}
			if root {
				read = &assistantGraphRead{ID: a.ID, Path: entry.Path, SHA256: digestOf(content), Declaration: assistantDeclarationDigest(entry)}
			}
		}
		result, err = assistantHostJSON(map[string]any{"type": a.Type, "id": a.ID, "content": string(content), "sha256": digestOf(content), "packs": packs})
		return result, read, nil, err
	case "get_graph_authoring_instructions":
		bodies := []string{}
		snapshots := map[string]struct {
			entry   assistantDecisionDeclaration
			content []byte
		}{}
		for _, id := range a.Packs {
			entry, err := assistantDecisionEntry(config.Packs, id)
			if err != nil {
				return result, nil, nil, err
			}
			body, err := e.graphDocument(entry)
			if err != nil {
				return result, nil, nil, err
			}
			bodies = append(bodies, string(body))
			snapshots[id] = struct {
				entry   assistantDecisionDeclaration
				content []byte
			}{entry, body}
		}
		packs := "[" + strings.Join(bodies, ",") + "]"
		if len(packs) > 1048576 {
			return result, nil, nil, failure.ToolInput("This decision context exceeds 1 MiB. Read fewer packs at a time.")
		}
		fresh := func(ctx context.Context) error {
			current, _, err := e.graphDeclarations(ctx)
			if err != nil {
				return err
			}
			for id, snap := range snapshots {
				entry, err := assistantDecisionEntry(current.Packs, id)
				if err != nil || entry != snap.entry {
					return errAssistantRunScope
				}
				body, err := e.graphDocument(entry)
				if err != nil || !bytes.Equal(body, snap.content) {
					return errAssistantRunScope
				}
			}
			return nil
		}
		ctx = withAssistantHostPrecondition(ctx, fresh)
		if runtime == nil {
			return result, nil, nil, errAssistantRunUnavailable
		}
		if testHookAssistantGraphGuidance != nil {
			testHookAssistantGraphGuidance()
		}
		text, err := runtime.Prompt(ctx, assistantExecutionPrompt{Name: "author_graph", Arguments: map[string]string{"relationship": a.Relationship, "packs": packs}})
		if err != nil {
			return result, nil, nil, err
		}
		result, err = assistantHostText(text + "\n\nUse propose_graph to retain a graph draft for human review. Do not use a pack proposal fence. Rehearsals run saved graphs only; no rows, lock or project files are written by these tools.")
		return result, nil, nil, err
	case "graph_validate", "graph_explain":
		command := strings.TrimPrefix(name, "graph_")
		if err = e.Authorize(ctx); err != nil {
			return result, nil, nil, err
		}
		dir, refusal := s.graphRuntime()
		if refusal != "" {
			return result, nil, nil, failure.ToolInput(refusal)
		}
		body, _, err := s.runGraphInput(ctx, dir, command, []byte(a.Content), "-")
		if err != nil {
			return result, nil, nil, err
		}
		result, err = assistantHostText("{\"answer\":" + string(s.shownGraphAnswer(body)) + "}\n")
		return result, nil, nil, err
	case "graph_rehearse":
		input := []byte(a.Inputs)
		trimmed := bytes.TrimSpace(input)
		if !json.Valid(input) || len(trimmed) == 0 || trimmed[0] != '{' {
			return result, nil, nil, failure.ToolInput("The rehearsal inputs must be a JSON object keyed by node id.")
		}
		entry, err := assistantDecisionEntry(config.Graphs, a.ID)
		if err != nil {
			return result, nil, nil, err
		}
		dir, refusal := s.graphRuntime()
		if refusal != "" {
			return result, nil, nil, failure.ToolInput(refusal)
		}
		path, _, refusal, err := s.graphPathOf(ctx, dir, a.ID)
		if err != nil {
			return result, nil, nil, err
		}
		if refusal != "" {
			return result, nil, nil, failure.ToolInput(refusal)
		}
		current, _, err := e.graphDeclarations(ctx)
		if err != nil {
			return result, nil, nil, err
		}
		now, err := assistantDecisionEntry(current.Graphs, a.ID)
		if err != nil || now != entry || filepath.Clean(path) != filepath.Clean(entry.Path) {
			return result, nil, nil, failure.ToolInput("The graph declaration changed. Read the current graph again.")
		}
		body, head, err := s.runGraphInput(ctx, dir, "evaluate", input, path, "--inputs", "-", "--rehearsal")
		if err != nil {
			return result, nil, nil, err
		}
		var mark struct {
			Rehearsal bool `json:"rehearsal"`
		}
		if json.Unmarshal(body, &mark) != nil || !mark.Rehearsal || head.Status != "evaluated" {
			refusal := assistantHostFailure(failure.ToolInput("Runtime refused the rehearsal or did not return rehearsal: true."))
			refusal.Content = append([]runcontext.Content{runcontext.TextContent(string(s.shownGraphAnswer(body, input)))}, refusal.Content...)
			return refusal, nil, nil, nil
		}
		result, err = assistantHostText(string(s.shownGraphAnswer(body, input)))
		return result, nil, nil, err
	case "propose_graph":
		if !root || e.record.Binding.Phase == "plan" {
			return result, nil, nil, failure.ToolInput("Graph drafts are available only in the parent Work phase.")
		}
		if !assistantGraphDocument(a.Content) {
			return result, nil, nil, failure.ToolInput("Provide a graph document, not a pack.")
		}

		record, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
		if err != nil {
			return result, nil, nil, err
		}
		var source struct {
			GraphDrafts []json.RawMessage `json:"graphDrafts"`
		}
		if json.Unmarshal(e.source.Original, &source) != nil || len(source.GraphDrafts)+len(record.DomainOutputs) >= 16 {
			return result, nil, nil, failure.ToolInput("This conversation already has 16 graph drafts. Review or remove one before proposing another.")
		}
		var basis *assistantGraphRead
		for _, op := range record.Operations {
			if op.Name == "read_decision" && op.Kind == "host" && op.State == "completed" {
				var receipt assistantHostResult
				if readAssistantHostReceipt(op.Result, &receipt) == nil && receipt.Read != nil && receipt.Read.ID == a.ID {
					copy := *receipt.Read
					basis = &copy
				}
			}
		}
		path := a.ID + ".graph.json"
		base := ""
		if declared, exists := config.Graphs[a.ID]; exists {
			entry, err := assistantDecisionEntry(config.Graphs, a.ID)
			if err != nil {
				return result, nil, nil, err
			}
			if basis == nil || basis.Path != declared.Path || basis.Declaration != assistantDeclarationDigest(entry) {
				return result, nil, nil, failure.ToolInput("Read the current graph with read_decision before proposing a revision.")
			}

			path = entry.Path
			base = basis.SHA256
		} else if basis != nil {
			return result, nil, nil, failure.ToolInput("The graph declaration was removed after it was read. Read the current declarations again.")
		}
		proposal := graphProposal{ID: a.ID, Path: path, Content: a.Content, Description: a.Description, BaseSHA256: base}
		if _, err := graphDeclaration(s.projectDir, raw, proposal); err != nil {
			return result, nil, nil, failure.ToolInput("The proposed graph target conflicts with the current project declarations.")
		}
		if base == "" {
			file := s.readProjectFile(path)
			if file.err != nil || file.present {
				return result, nil, nil, failure.ToolInput("The new graph target already exists or cannot be checked.")
			}
		}
		if err = e.Authorize(ctx); err != nil {
			return result, nil, nil, err
		}
		// Declaration and target custody are checked again immediately before retain.
		_, now, err := e.graphDeclarations(ctx)
		if err != nil || !bytes.Equal(now, raw) {
			return result, nil, nil, errAssistantRunScope
		}
		if base != "" {
			file := s.readProjectFile(path)
			if file.err != nil || !file.present || digestOf(file.data) != base {
				return result, nil, nil, errAssistantRunScope
			}
		}
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			return result, nil, nil, err
		}
		draft = &graphDraft{DraftID: hex.EncodeToString(id[:]), ID: a.ID, Path: path, Content: a.Content, Description: a.Description, BaseSHA256: base, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		result, err = assistantGraphProposalResult(e.record.Binding.Owner.ID, *draft)
		return result, nil, draft, err
	}
	return result, nil, nil, errAssistantRunInvalid
}
