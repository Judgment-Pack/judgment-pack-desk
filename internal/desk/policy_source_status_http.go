package desk

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Server) handlePolicySourceStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.guard(w, r) {
		return
	}
	held, ok := s.sessionOf(r)
	if !ok || !validPolicyActor(policyActorOf(held)) {
		policyHTTPFailure(w, policyRefusal(401, CodeUnauthorized, "Sign in before accessing source authority."))
		return
	}
	id := r.PathValue("policy")
	if !policyIDForm.MatchString(id) || r.URL.RawQuery != "" {
		policyHTTPFailure(w, policyRefusal(400, CodeBadRequest, "Invalid source authority request."))
		return
	}
	service := s.localPolicyService(r, policyActorOf(held))
	if version := r.PathValue("version"); version != "" {
		if !policyAuthorityIDForm.MatchString(version) || !strings.HasPrefix(version, "external_") {
			policyHTTPFailure(w, policyRefusal(400, CodeBadRequest, "Invalid external version selection."))
			return
		}
		result, err := service.externalVersion(r.Context(), id, version)
		if err != nil {
			policyHTTPFailure(w, err)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	// This is the same grant fence used by source binding, disconnect and auth
	// policy changes. A stale active record never substitutes for current authority.
	s.signIn.policySourceMu.RLock()
	defer s.signIn.policySourceMu.RUnlock()
	authority, err := s.policySourceAuthorityBasis()
	if err != nil {
		authority = ""
	}
	if r.Method == http.MethodGet {
		result, err := service.sourceStatus(r.Context(), id, authority)
		if err != nil {
			policyHTTPFailure(w, err)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	var q policySourceStatusRequest
	if inputRequest(r, &q) != nil {
		policyHTTPFailure(w, policyRefusal(400, CodeBadRequest, "Invalid reviewed source authority action."))
		return
	}
	result, err := service.sourceStatusCommand(r.Context(), id, policyActorOf(held), q, authority)
	if err != nil {
		policyHTTPFailure(w, err)
		return
	}
	writeJSON(w, 200, result)
}

type policyExternalVersionReply struct {
	Version       policyExternalVersion           `json:"version"`
	Configuration policySourceStatusConfiguration `json:"configuration"`
	Document      json.RawMessage                 `json:"document"`
}

func (s policyService) externalVersion(ctx context.Context, id, version string) (policyExternalVersionReply, error) {
	var result policyExternalVersionReply
	found := false
	err := s.repository.View(ctx, func(ledger policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		stream := ledger.SourceAuthorities[id]
		if stream == nil {
			return policyRefusal(404, CodeNotFound, "External source version not found.")
		}
		configs := map[string]policySourceStatusConfiguration{}
		for _, event := range stream.Events {
			if event.Configuration != nil {
				configs[event.Configuration.ID] = *event.Configuration
			}
			if event.ExternalVersion != nil && event.ExternalVersion.ID == version {
				result.Version = *event.ExternalVersion
				result.Configuration = configs[result.Version.ConfigurationID]
				found = true
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if !found {
		return result, policyRefusal(404, CodeNotFound, "External source version not found.")
	}
	if _, err = s.artifacts.Get(ctx, result.Version.Observation.Original); err != nil {
		return result, err
	}
	raw, err := s.artifacts.Get(ctx, result.Version.Observation.Document)
	if err != nil {
		return result, err
	}
	result.Document = raw
	return result, s.authorized(ctx)
}
