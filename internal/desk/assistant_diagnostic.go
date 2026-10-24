package desk

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"regexp"
)

const assistantDiagnosticPurpose = "connection-diagnostic"
const assistantDiagnosticInstruction = "Connection check only. Call get_schema once to read the Judgment Pack schema through Desk, then reply with one short confirmation. Do not propose changes or test cases. Do not use any other tool, request more compute, delegate, acquire sources, evaluate or write anything."

var assistantDiagnosticRequestID = regexp.MustCompile(`^[A-Za-z0-9-]{1,100}$`)

type assistantDiagnosticReference struct {
	Start *assistantRunStart `json:"start,omitempty"`
	Run   *assistantRunView  `json:"run,omitempty"`
}

func (s *Server) registerAssistantDiagnosticRuns() {
	s.mux.HandleFunc("POST /api/assistant-diagnostics/reference", s.handleAssistantDiagnosticReference)
	s.mux.HandleFunc("GET /api/assistant-diagnostics/requests/{request}", s.handleAssistantDiagnosticLookup)
	s.mux.HandleFunc("GET /api/assistant-runs/{run}/connection-diagnostic", s.handleAssistantDiagnosticResult)
}

// This reference allocates no owner, work, provider call or separate storage.
// Lost reference delivery cannot dispatch; only the explicit canonical start can.
func (s *Server) handleAssistantDiagnosticReference(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	var body struct {
		RequestID string `json:"requestId"`
	}
	if assistantRunRequest(r, &body) != nil || !assistantDiagnosticRequestID.MatchString(body.RequestID) {
		assistantRunHTTPError(w, errAssistantRunInvalid)
		return
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	store := s.assistantRuns()
	if store.problem != nil {
		assistantRunHTTPError(w, store.problem)
		return
	}
	project := store.project
	start := assistantRunStart{Purpose: assistantDiagnosticPurpose, OwnerID: r.Header.Get(aiConnectionHeader), RequestID: body.RequestID, AdmissionID: "diagnostic-" + body.RequestID}
	lock, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	defer lock.Close()
	source, err := s.assistantDiagnosticSource(r, start, identity, project)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	start.Revision = digestOf(source.Original)
	writeJSON(w, 200, assistantDiagnosticReference{Start: &start})
}
func (s *Server) handleAssistantDiagnosticLookup(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	id := r.PathValue("request")
	if !assistantDiagnosticRequestID.MatchString(id) {
		assistantRunHTTPError(w, errAssistantRunInvalid)
		return
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	run, err := s.assistantRuns().lookup(r.Context(), identity, "", "diagnostic-"+id, false)
	if errors.Is(err, errAssistantRunNotFound) {
		writeJSON(w, 200, assistantDiagnosticReference{})
		return
	}
	if err != nil || run.Binding.Owner.Kind != assistantDiagnosticPurpose || run.Binding.RequestID != id {
		assistantRunHTTPError(w, errAssistantRunScope)
		return
	}
	view := assistantRunPublic(run)
	writeJSON(w, 200, assistantDiagnosticReference{Run: &view})
}
func (s *Server) resolveAssistantDiagnosticRun(r *http.Request, start assistantRunStart, identity assistantRunBinding, project string) (assistantRunSource, error) {
	source, err := s.assistantDiagnosticSource(r, start, identity, project)
	if err != nil {
		return source, err
	}
	if !assistantRunDigest(start.Revision) || start.Revision != digestOf(source.Original) {
		return assistantRunSource{}, errAssistantRunConflict
	}
	source.Binding.Owner.Revision = start.Revision
	return source, nil
}
func (s *Server) assistantDiagnosticSource(r *http.Request, start assistantRunStart, identity assistantRunBinding, project string) (assistantRunSource, error) {
	var zero assistantRunSource
	if start.Purpose != assistantDiagnosticPurpose || !assistantDiagnosticRequestID.MatchString(start.RequestID) || start.AdmissionID != "diagnostic-"+start.RequestID || !aiIDPattern.MatchString(start.OwnerID) || start.OwnerID != r.Header.Get(aiConnectionHeader) {
		return zero, errAssistantRunInvalid
	}
	connection, err := s.selectedAIConnection(r, true)
	if err != nil || connection == nil || connection.Assistant.Engine != "codex" || connection.Assistant.Agent == nil || !contains(connection.Assistant.Agent.Tools, "get_schema") {
		return zero, errAssistantRunScope
	}
	profile, err := s.readAssistantProfile()
	if err != nil {
		return zero, err
	}
	selected := connection.Assistant.Agent.Model
	provisional := assistantRunSource{Connection: *connection, Profile: *profile}
	if preferences := assistantChatPreferences(provisional); preferences != nil && !preferences.Inherit {
		selected = preferences.Model
	}
	if selected == nil || !contains(connectionModels(connection, profile), *selected) {
		return zero, errAssistantRunScope
	}
	account, err := s.accountForConnection(r)
	if err != nil {
		return zero, err
	}
	custody, ok := account.(interface{ AccountBinding() (string, error) })
	if !ok {
		return zero, errAssistantRunUnavailable
	}
	epoch, err := custody.AccountBinding()
	if err != nil {
		return zero, err
	}
	runtime, err := exec.LookPath(s.cfg.JpackBin)
	if err != nil {
		return zero, errAssistantRunUnavailable
	}
	info, err := os.Stat(runtime)
	if err != nil || !info.Mode().IsRegular() {
		return zero, errAssistantRunUnavailable
	}
	// Only opaque digests enter storage. Account identifiers, profile paths and
	// credentials never enter the checkpoint or public projection.
	snapshot, _ := json.Marshal(struct {
		Identity                  assistantRunBinding
		Connection                aiConnection
		Profile                   assistantProfile
		Account, Runtime, Project string
		Size, Modified            int64
	}{identity, *connection, *profile, epoch, runtime, project, info.Size(), info.ModTime().UnixNano()})
	original, _ := json.Marshal(struct {
		Source      string `json:"source"`
		Instruction string `json:"instruction"`
	}{digestOf(snapshot), assistantDiagnosticInstruction})
	request, _ := json.Marshal(struct{ RequestID, Instruction string }{start.RequestID, assistantDiagnosticInstruction})
	profileRaw, _ := json.Marshal(profile)
	binding := identity
	binding.Project, binding.Owner = project, assistantRunOwner{Kind: assistantDiagnosticPurpose, ID: connection.ID, Revision: digestOf(original)}
	binding.RequestID, binding.RequestDigest = start.RequestID, digestOf(request)
	binding.SourceDigests = []string{digestOf(original)}
	binding.ConnectionID, binding.ConnectionRevision = connection.ID, connection.Revision
	binding.ProfileRevision, binding.Model, binding.Phase = digestOf(profileRaw), *selected, "work"
	return assistantRunSource{Start: start, Binding: binding, Original: original, Request: request, Text: assistantDiagnosticInstruction, Connection: *connection, Profile: *profile}, nil
}
func (s *Server) handleAssistantDiagnosticResult(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	run, err := s.assistantRunService().read(r, r.PathValue("run"))
	if err != nil || run.Binding.Owner.Kind != assistantDiagnosticPurpose || run.Historical {
		assistantRunHTTPError(w, errAssistantRunScope)
		return
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil || !assistantRunIdentityMatches(identity, run.Binding) {
		assistantRunHTTPError(w, errAssistantRunScope)
		return
	}
	held := r.Clone(r.Context())
	held.Header.Set(aiConnectionHeader, run.Binding.ConnectionID)
	held.Header.Set(aiRevisionHeader, run.Binding.ConnectionRevision)
	start := assistantRunStart{Purpose: assistantDiagnosticPurpose, OwnerID: run.Binding.Owner.ID, Revision: run.Binding.Owner.Revision, RequestID: run.Binding.RequestID, AdmissionID: run.AdmissionID}
	source, err := s.readAssistantRunSource(held, start, identity, run.Binding.Project)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	source.Binding.ExecutionKey, source.Binding.Unit = run.Binding.ExecutionKey, run.Binding.Unit
	if !reflect.DeepEqual(source.Binding, run.Binding) {
		assistantRunHTTPError(w, errAssistantRunScope)
		return
	}
	writeJSON(w, 200, struct {
		Succeeded bool `json:"succeeded"`
	}{assistantDiagnosticSucceeded(run)})
}

func assistantDiagnosticSucceeded(run assistantRunRecord) bool {
	if run.Binding.Owner.Kind != assistantDiagnosticPurpose || run.Historical || run.State != "complete" || run.Checkpoint == nil || run.Checkpoint.Status != "complete" || run.Checkpoint.Unit != "provider-managed" || run.Checkpoint.Used != 0 {
		return false
	}
	observations := 0
	for _, event := range run.Events {
		held := event.Event
		if held.Proposal != nil || held.Error != nil || held.ApprovalRequested != nil || held.UserQuestion != nil || held.DelegationRequested != nil {
			return false
		}
		if held.ToolResult != nil {
			if held.ToolResult.Name != "get_schema" || held.ToolResult.IsError {
				return false
			}
			observations++
		}
	}
	if observations != 1 {
		return false
	}
	reads := 0
	for _, op := range run.Operations {
		if op.State != "completed" && op.State != "refused" {
			return false
		}
		if op.Kind != "runtime" || op.Name != "tools/call" {
			continue
		}
		var frame struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if json.Unmarshal(op.Intent, &frame) != nil || frame.Params.Name != "get_schema" || op.State != "completed" || !assistantDiagnosticSchemaResult(op.Result) {
			return false
		}
		reads++
	}
	return reads == 1
}
func assistantDiagnosticSchemaResult(raw []byte) bool {
	if len(raw) == 0 || len(raw) > 1<<20 || !validUTF8(raw) || !uniqueJSON(raw) {
		return false
	}
	var frame struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &frame) != nil || frame.Error != nil || frame.Result.IsError || len(frame.Result.Content) == 0 {
		return false
	}
	for _, content := range frame.Result.Content {
		if content.Type != "text" || content.Text == "" {
			return false
		}
	}
	return true
}
