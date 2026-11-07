package desk

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
)

const assistantCapabilityInstruction = "Call the function probe_echo twice in one reply: once with value 1 and once with value 2. Reply with the calls only."
const assistantObservationSchema = `{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"],"additionalProperties":false}`

type assistantObservationSelector struct {
	Kind  string `json:"kind"`
	Model string `json:"model"`
}
type assistantObservationReferenceRequest struct {
	RequestID string `json:"requestId"`
	Kind      string `json:"kind"`
	Model     string `json:"model,omitempty"`
}
type assistantObservationReference struct {
	Start *assistantRunStart `json:"start,omitempty"`
	Run   *assistantRunView  `json:"run,omitempty"`
}

func (s *Server) registerAssistantObservations() {
	s.mux.HandleFunc("POST /api/assistant-observations/reference", s.handleAssistantObservationReference)
	s.mux.HandleFunc("GET /api/assistant-observations/requests/{request}", s.handleAssistantObservationLookup)
	s.mux.HandleFunc("GET /api/assistant-runs/{run}/settings-observation", s.handleAssistantObservationResult)
}
func (s *Server) handleAssistantObservationReference(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	var body assistantObservationReferenceRequest
	if assistantRunRequest(r, &body) != nil {
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
	start := assistantRunStart{Purpose: assistantSettingsObservationPurpose, OwnerID: r.Header.Get(aiConnectionHeader), RequestID: body.RequestID, AdmissionID: "settings-observation-" + body.RequestID, Observation: &assistantObservationSelector{body.Kind, body.Model}}
	lock, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	defer lock.Close()
	source, err := s.assistantSettingsObservationSource(r, start, identity, store.project, true)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	start = source.Start
	start.Revision = digestOf(source.Original)
	writeJSON(w, 200, assistantObservationReference{Start: &start})
}
func (s *Server) handleAssistantObservationLookup(w http.ResponseWriter, r *http.Request) {
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
	run, err := s.assistantRuns().lookup(r.Context(), identity, "", "settings-observation-"+id, false)
	if errors.Is(err, errAssistantRunNotFound) {
		writeJSON(w, 200, assistantObservationReference{})
		return
	}
	if err != nil || run.Observation == nil || run.Binding.RequestID != id {
		assistantRunHTTPError(w, errAssistantRunScope)
		return
	}
	view := assistantRunPublic(run)
	writeJSON(w, 200, assistantObservationReference{Run: &view})
}
func (s *Server) resolveAssistantSettingsObservationRun(r *http.Request, start assistantRunStart, identity assistantRunBinding, project string) (assistantRunSource, error) {
	source, err := s.assistantSettingsObservationSource(r, start, identity, project, false)
	if err != nil {
		return source, err
	}
	if !assistantRunDigest(start.Revision) || start.Revision != digestOf(source.Original) {
		return assistantRunSource{}, errAssistantRunConflict
	}
	return source, nil
}
func assistantSettingsProfile(kind, model string) (assistantObservationProfile, error) {
	p := assistantObservationProfile{Kind: kind, Model: model}
	instruction := assistantCapabilityInstruction
	if kind == "capability" {
		p.OutputCeiling = 128
	} else if kind == "ping" {
		p.OutputCeiling = 1
		instruction = "ping"
	} else {
		return p, errAssistantRunInvalid
	}
	if !assistantRunText(model, 128) {
		return p, errAssistantRunInvalid
	}
	raw, _ := json.Marshal(struct {
		Version                          int
		Kind, Model, Instruction, Schema string
		Ceiling                          int
	}{1, kind, model, instruction, assistantObservationSchema, p.OutputCeiling})
	p.Digest = digestOf(raw)
	return p, nil
}

// Caller holds the existing private data lock. Reads are local and model-free;
// no recursive authorization or credential access occurs inside admission CAS.
func (s *Server) assistantSettingsObservationSource(r *http.Request, start assistantRunStart, identity assistantRunBinding, project string, reference bool) (assistantRunSource, error) {
	var zero assistantRunSource
	if start.Purpose != assistantSettingsObservationPurpose || !assistantDiagnosticRequestID.MatchString(start.RequestID) || start.AdmissionID != "settings-observation-"+start.RequestID || start.Observation == nil {
		return zero, errAssistantRunInvalid
	}
	selector := *start.Observation
	if selector.Kind != "capability" && selector.Kind != "ping" {
		return zero, errAssistantRunInvalid
	}
	endpoint, _, connection, err := s.endpointForConnection(r, selector.Kind == "capability")
	if err != nil {
		return zero, err
	}
	// Use the actual existing lazy legacy registry resolution; never invent an entry.
	if connection == nil {
		registry, e := s.readAIRegistry()
		if e != nil {
			return zero, e
		}
		for _, c := range registry.Connections {
			if c.ID == "legacy-api" {
				held := c
				connection = &held
				break
			}
		}
	}
	if connection == nil || connection.Assistant.Engine != "vercel" || connection.Assistant.Endpoint == nil {
		return zero, errAssistantRunScope
	}
	if start.OwnerID == "" && reference {
		start.OwnerID = connection.ID
	}
	if start.OwnerID != connection.ID || r.Header.Get(aiConnectionHeader) != "" && start.OwnerID != r.Header.Get(aiConnectionHeader) {
		return zero, errAssistantRunScope
	}
	profile := &assistantProfile{}
	if selector.Kind == "capability" {
		if endpoint.kind != "openai-compatible" {
			return zero, errAssistantRunScope
		}
		profile, err = s.readAssistantProfile()
		if err != nil {
			return zero, err
		}
		if !contains(connectionModels(connection, profile), selector.Model) {
			return zero, errAssistantRunScope
		}
	} else {
		if endpoint.kind != "anthropic" || endpoint.model == "" {
			return zero, errAssistantRunScope
		}
		if reference && selector.Model == "" {
			selector.Model = endpoint.model
		}
		if selector.Model != endpoint.model {
			return zero, errAssistantRunConflict
		}
	}
	fixed, err := assistantSettingsProfile(selector.Kind, selector.Model)
	if err != nil {
		return zero, err
	}
	start.Observation = &selector
	instruction := assistantCapabilityInstruction
	if selector.Kind == "ping" {
		instruction = "ping"
	}
	// Secrets and addresses are reduced to opaque identity digests in the source.
	snapshot, _ := json.Marshal(struct {
		Identity   assistantRunBinding
		Connection aiConnection
		Profile    assistantProfile
		Project    string
		Fixed      assistantObservationProfile
	}{identity, *connection, *profile, project, fixed})
	original, _ := json.Marshal(struct{ Source, Profile, Instruction string }{digestOf(snapshot), fixed.Digest, instruction})
	request, _ := json.Marshal(struct{ RequestID, Kind, Model, Instruction string }{start.RequestID, selector.Kind, selector.Model, instruction})
	profileRaw, _ := json.Marshal(profile)
	binding := identity
	binding.Project, binding.Owner = project, assistantRunOwner{Kind: assistantSettingsObservationPurpose, ID: connection.ID, Revision: digestOf(original)}
	binding.RequestID, binding.RequestDigest = start.RequestID, digestOf(request)
	binding.SourceDigests = []string{digestOf(original)}
	binding.ConnectionID, binding.ConnectionRevision = connection.ID, connection.Revision
	binding.ProfileRevision, binding.Model, binding.Phase = digestOf(profileRaw), selector.Model, "work"
	return assistantRunSource{Start: start, Binding: binding, Original: original, Request: request, Text: instruction, Connection: *connection, Profile: *profile}, nil
}
func (s *Server) handleAssistantObservationResult(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	run, err := s.assistantRunService().read(r, r.PathValue("run"))
	if err != nil || run.Observation == nil || run.Historical {
		assistantRunHTTPError(w, errAssistantRunScope)
		return
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil || !assistantRunIdentityMatches(identity, run.Binding) {
		assistantRunHTTPError(w, errAssistantRunScope)
		return
	}
	if !assistantRunTerminal(run.State) {
		assistantRunHTTPError(w, errAssistantRunConflict)
		return
	}
	if run.Observation.Outcome == nil {
		writeJSON(w, 200, assistantSettingsUnavailableResult(run))
		return
	}
	if validateAssistantSettingsOutcome(run) != nil {
		assistantRunHTTPError(w, errAssistantRunInvalid)
		return
	}
	writeJSON(w, 200, run.Observation.Outcome.Public)
}

// Selector parsing is exact even though encoding/json accepts case aliases.
func assistantObservationSelectorJSON(raw json.RawMessage) error {
	return readAssistantDialectCustody(raw, reflect.TypeOf(assistantObservationSelector{}))
}
