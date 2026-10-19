package desk

import (
	"net/http"
	"regexp"
	"strings"
)

const manualIntentHeader = "X-Jpack-Manual-Intent"

var manualJobsRunPath = regexp.MustCompile(`^jobs/(job_[a-f0-9]{32})/runs$`)
var manualJobsID = regexp.MustCompile(`^job_[a-f0-9]{32}$`)

func (s *Server) localExecutionCore(r *http.Request) (policyExecutionService, error) {
	core, enabled, err := s.executionCore()
	if err != nil {
		return core, err
	}
	if !enabled {
		return core, executionFailure("unsupported-profile")
	}
	held, ok := s.sessionOf(r)
	if !ok || held.issuer != nil {
		return core, policyRefusal(401, CodeUnauthorized, "A current local administrator session is required for manual execution admission.")
	}
	actor := policyActorOf(held)
	core.authorize = func() bool {
		current, ok := s.sessionOf(r)
		return ok && current.issuer == nil && policyActorOf(current) == actor && r.Context().Err() == nil
	}
	return core, nil
}

type policyExecutionStatus struct {
	Version int    `json:"version"`
	Enabled bool   `json:"enabled"`
	Profile string `json:"profile,omitempty"`
}
type policyExecutionRegistrationView struct {
	Version      int                          `json:"version"`
	State        string                       `json:"state"`
	Resource     *policyExecutionResource     `json:"resource,omitempty"`
	Registration *policyExecutionRegistration `json:"registration,omitempty"`
	AdoptionID   string                       `json:"adoptionId,omitempty"`
	ReviewID     string                       `json:"reviewId,omitempty"`
	Eligible     bool                         `json:"eligible"`
	Reason       string                       `json:"reason"`
}

func (s *Server) handleExecutionStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.guard(w, r) {
		return
	}
	if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.URL.ForceQuery {
		policyHTTPFailure(w, executionFailure("invalid-request"))
		return
	}
	_, enabled, err := s.executionCore()
	if err != nil {
		policyHTTPFailure(w, err)
		return
	}
	if !enabled {
		writeJSON(w, 200, policyExecutionStatus{Version: 1})
		return
	}
	core, err := s.localExecutionCore(r)
	if err != nil {
		policyHTTPFailure(w, err)
		return
	}
	if err = core.authorized(r.Context()); err != nil {
		policyHTTPFailure(w, err)
		return
	}
	writeJSON(w, 200, policyExecutionStatus{1, true, policyExecutionProfile})
}
func (s *Server) handleExecutionRegistration(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.guard(w, r) {
		return
	}
	job := r.PathValue("job")
	if !manualJobsID.MatchString(job) || strings.Contains(r.URL.EscapedPath(), "%") || r.URL.RawQuery != "" || r.URL.ForceQuery {
		policyHTTPFailure(w, executionFailure("invalid-request"))
		return
	}
	core, err := s.localExecutionCore(r)
	if err != nil {
		policyHTTPFailure(w, err)
		return
	}
	if r.Method == http.MethodGet {
		result := policyExecutionRegistrationView{Version: 1, State: "unregistered", Reason: "resource-unregistered"}
		err = core.repository.View(r.Context(), func(l policyLedger) error {
			if err := core.authorized(r.Context()); err != nil {
				return err
			}
			if err := core.scoped(l); err != nil {
				return err
			}
			if value, ok := l.Execution.Resources[executionResourceID(core.identity, job)]; ok {
				now, err := core.now()
				if err != nil {
					return err
				}
				reason := "clock-unavailable"
				if now >= l.Execution.LastTime {
					reason = core.eligible(r.Context(), l, value, now)
				}
				result = policyExecutionRegistrationView{1, "registered", &value.Content.Resource, &value.Registration, value.Content.Adoption.ID, value.Content.Adoption.Review.ID, reason == "eligible", reason}
			}
			return core.authorized(r.Context())
		})
		if err != nil {
			policyHTTPFailure(w, err)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	if r.Method != http.MethodPost {
		policyHTTPFailure(w, executionFailure("invalid-request"))
		return
	}
	raw, err := readBounded(r.Body, 4096)
	if err != nil {
		policyHTTPFailure(w, executionFailure("invalid-request"))
		return
	}
	var q struct {
		OperationID string `json:"operationId"`
		AdoptionID  string `json:"adoptionId"`
		ReviewID    string `json:"reviewId"`
	}
	if executionDecode(raw, &q) != nil {
		policyHTTPFailure(w, executionFailure("invalid-request"))
		return
	}
	result, err := core.register(r.Context(), policyExecutionRegisterRequest{q.OperationID, job, q.AdoptionID, q.ReviewID})
	if err != nil {
		policyHTTPFailure(w, err)
		return
	}
	if err = core.authorized(r.Context()); err != nil {
		failure := policyRefusal(401, CodeUnauthorized, "Sign in again before recovering this registration operation.")
		failure.Uncertain = true
		policyHTTPFailure(w, failure)
		return
	}
	writeJSON(w, 200, result)
}

func authorityHeaderValues(r *http.Request, name string) []string {
	var values []string
	for key, entries := range r.Header {
		if strings.EqualFold(key, name) {
			values = append(values, entries...)
		}
	}
	return values
}
func (s *Server) prepareManualJobsRequest(r *http.Request, tail string, raw []byte) (string, error) {
	parts := manualJobsRunPath.FindStringSubmatch(tail)
	if len(parts) != 2 || r.Method != http.MethodPost || r.URL.RawQuery != "" || r.URL.ForceQuery || len(authorityHeaderValues(r, manualIntentHeader)) != 0 {
		return "", executionFailure("invalid-request")
	}
	keys := authorityHeaderValues(r, "Idempotency-Key")
	if len(keys) != 1 || !executionKey(keys[0]) {
		return "", executionFailure("invalid-request")
	}
	core, err := s.localExecutionCore(r)
	if err != nil {
		return "", err
	}
	// Read current owning Runner binding outside the Policy transaction. Exact
	// registered tuple comparison prevents a job upgrade inheriting approval.
	current, err := s.jobs.executionBinding(r.Context(), parts[1])
	if err != nil {
		return "", err
	}
	var registration policyExecutionRegistration
	err = core.repository.View(r.Context(), func(l policyLedger) error {
		if err := core.authorized(r.Context()); err != nil {
			return err
		}
		if err := core.scoped(l); err != nil {
			return err
		}
		value, ok := l.Execution.Resources[executionResourceID(core.identity, parts[1])]
		if !ok {
			return executionFailure("resource-unregistered")
		}
		if value.Content.Resource != current.Resource {
			return executionFailure("resource-changed")
		}
		registration = value.Registration
		return nil
	})
	if err != nil {
		return "", err
	}
	intent, _, err := core.prepareIntent(r.Context(), current.Resource, registration, keys[0], raw)
	if err != nil {
		return "", err
	}
	if err = core.authorized(r.Context()); err != nil {
		return "", err
	}
	return intent.ID, nil
}

// New jobs and pure previews can be authored. Acquisition, triggers, upgrades,
// workflow and unknown operational routes have no first-profile admission path.
func manualJobsRouteAllowed(method, tail string) bool {
	if strings.HasPrefix(tail, "inputs/") || strings.Contains(tail, "/triggers") || strings.HasPrefix(tail, "triggers/") || strings.HasPrefix(tail, "occurrences/") {
		return false
	}
	if method == http.MethodPost {
		return tail == "jobs" || tail == "previews" || manualJobsRunPath.MatchString(tail)
	}
	return method == http.MethodGet
}
