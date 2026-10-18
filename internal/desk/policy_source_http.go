package desk

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"strings"
)

func (s *Server) handlePolicySourceSync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.guard(w, r) {
		return
	}
	held, ok := s.sessionOf(r)
	if !ok {
		policyHTTPFailure(w, policyRefusal(401, CodeUnauthorized, "Sign in before accessing policy sources."))
		return
	}
	actor := policyActorOf(held)
	id := r.PathValue("policy")
	if !validPolicyActor(actor) || !policyIDForm.MatchString(id) || r.URL.RawQuery != "" {
		policyHTTPFailure(w, policyRefusal(400, CodeBadRequest, "Invalid policy source request."))
		return
	}
	service := s.localPolicyService(r, actor)
	if r.Method == http.MethodGet {
		observation := r.PathValue("observation")
		if observation != "" {
			if !retainedArtifactDigest.MatchString(observation) {
				policyHTTPFailure(w, policyRefusal(400, CodeBadRequest, "Invalid source observation."))
				return
			}
			sync, err := service.sourceSync(r.Context(), id, false)
			if err != nil {
				policyHTTPFailure(w, err)
				return
			}
			var found *policySourceObservation
			if sync.State != nil {
				for _, item := range sync.State.Observations {
					if item.ID == observation {
						copy := item
						found = &copy
					}
				}
			}
			if found == nil {
				policyHTTPFailure(w, policyRefusal(404, CodeNotFound, "Source observation not found."))
				return
			}
			original, err := service.artifacts.Get(r.Context(), found.Original)
			if err != nil {
				policyHTTPFailure(w, err)
				return
			}
			document, err := service.artifacts.Get(r.Context(), found.Document)
			if err != nil {
				policyHTTPFailure(w, err)
				return
			}
			if err = service.authorized(r.Context()); err != nil {
				policyHTTPFailure(w, err)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/original") {
				w.Header().Set("Content-Type", found.Content.MediaType)
				w.Header().Set("X-Content-Type-Options", "nosniff")
				w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": found.Name}))
				w.WriteHeader(200)
				_, _ = w.Write(original)
				return
			}
			writeJSON(w, 200, map[string]any{"observation": found, "document": json.RawMessage(document)})
			return
		}
		result, err := service.sourceSync(r.Context(), id, s.policySourcesSupported(r.Context()))
		if err != nil {
			policyHTTPFailure(w, err)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	var q policySourceSyncRequest
	if inputRequest(r, &q) != nil {
		policyHTTPFailure(w, policyRefusal(400, CodeBadRequest, "Invalid policy source action."))
		return
	}
	s.signIn.policySourceMu.RLock()
	defer s.signIn.policySourceMu.RUnlock()
	authority, err := s.policySourceAuthorityBasis()
	if err != nil && q.Action != "revoke" && q.Action != "pause" && q.Action != "keep-current" {
		policyHTTPFailure(w, policyRefusal(409, "source-authorization-ended", "Source authorization is unavailable. Review administrator sign-in settings."))
		return
	}
	result, err := service.sourceCommand(r.Context(), id, actor, q, authority, s.policySourcesSupported(r.Context()))
	if err != nil {
		policyHTTPFailure(w, err)
		return
	}
	writeJSON(w, 200, result)
}

// Called while the shared authority write lock is held after a confirmed Drive
// disconnect. Closed/reopened desks are additionally fenced by Gateway's epoch.
func (s *Server) endPolicySourceConnection(ctx context.Context) {
	owner := s
	if s.cfg.parent != nil {
		owner = s.cfg.parent
	}
	servers := []*Server{owner}
	owner.desksMu.Lock()
	for _, child := range owner.desks {
		servers = append(servers, child)
	}
	owner.desksMu.Unlock()
	for _, server := range servers {
		_ = (filePolicyRepository{open: server.openPolicies}).Update(ctx, func(ledger *policyLedger) (bool, error) {
			changed := false
			for _, source := range ledger.Sources {
				if source.State != "revoked" && source.State != "authorization-ended" {
					source.State = "authorization-ended"
					source.Generation++
					source.Poll = nil
					source.Gap = true
					source.ErrorCode = "source-authorization-ended"
					source.CleanupOperations = appendPolicySourceCleanup(source.CleanupOperations, source.Grant.OperationID)
					if source.Binding != nil {
						source.Cleanup = appendPolicySourceCleanup(source.Cleanup, source.Binding.BindingID)
					}
					source.NextCheck = ""
					changed = true
				}
			}
			return changed, nil
		})
	}
}
