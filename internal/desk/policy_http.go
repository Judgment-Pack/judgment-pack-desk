package desk

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strings"
)

func policyActorOf(held session) policyActor {
	actor := policyActor{Kind: "local", Subject: held.subject}
	if held.issuer != nil {
		actor.Kind, actor.Issuer = "oidc", *held.issuer
	}
	return actor
}

func (s *Server) localPolicyService(r *http.Request, actor policyActor) policyService {
	return policyService{
		repository: s.policyRepository(), history: s.policyHistoryReader(), artifacts: s.artifactRepository(),
		coveragePack: s.resolveCoveragePack, checkCoverage: s.checkPolicyCoverage, checkAdoption: s.checkPolicyAdoption,
		authorize: func() bool {
			held, ok := s.sessionOf(r)
			return ok && policyActorOf(held) == actor && r.Context().Err() == nil
		},
		validateVersion: func(ctx context.Context, base, version string) error {
			dir, _, ok := s.projectRuntime()
			if !ok {
				return policyRefusal(503, "runtime-unavailable", "Runtime version checks are unavailable. Keep this policy draft and retry later.")
			}
			// The shared Runtime command owns version grammar. This validates
			// arithmetic only and makes no Policy-format conformance claim.
			raw, err := runRuntime(ctx, s.cfg.JpackBin, dir, "artifact", "version", "--base", base, "--candidate", version, "--format", "json")
			var answer struct {
				artifactVersionReply
				Diagnostics []struct {
					Code string `json:"code"`
				} `json:"diagnostics"`
			}
			decoded := json.Unmarshal(raw, &answer) == nil
			bound := decoded && answer.OutputVersion == "2" && answer.Command == "artifact version" && answer.Scope == "version-arithmetic" && !answer.CompatibilityChecked
			if bound && answer.Status == "refused" {
				for _, finding := range answer.Diagnostics {
					if finding.Code == "JPS-ARTIFACT-VERSION" {
						return policyRefusal(422, "invalid-version", "Use an artifact version accepted by Runtime before approval or publication.")
					}
				}
			}
			if err != nil || !bound || answer.Status != "available" || answer.Base != base || answer.Candidate != version || !contains([]string{"lower", "equal", "greater"}, answer.Comparison) {
				return policyRefusal(503, "runtime-unavailable", "Runtime could not check this artifact version. Keep the policy draft and retry after checking Runtime.")
			}
			if base == version && answer.Comparison != "equal" {
				return policyRefusal(503, "runtime-unavailable", "Runtime returned an inconsistent version check. Keep the policy draft and retry later.")
			}
			if base != version && answer.Comparison != "greater" {
				return policyRefusal(409, "version-conflict", "Choose an artifact version greater than the latest publication before publishing this candidate.")
			}
			return nil
		},
	}
}

func policyHTTPFailure(w http.ResponseWriter, err error) {
	var failure *policyFailure
	if !errors.As(err, &failure) {
		failure = policyUncertainStorage()
	}
	if failure.Status == 503 {
		w.Header().Set("Retry-After", "1")
	}
	body := codedBody(failure.Code, failure.Message)
	body["outcome"] = "not-committed"
	if failure.Uncertain {
		body["outcome"] = "unknown"
	}
	if failure.CurrentPolicyRevision != "" {
		body["currentPolicyRevision"] = failure.CurrentPolicyRevision
	}
	if failure.CurrentRevision != "" {
		body["currentRevision"] = failure.CurrentRevision
	}
	writeJSON(w, failure.Status, body)
}

func (s *Server) handlePolicies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.guard(w, r) {
		return
	}
	// Launch credentials are deliberately insufficient for governance records:
	// even this single-owner lifecycle needs an actual authenticated session actor.
	held, ok := s.sessionOf(r)
	if !ok {
		policyHTTPFailure(w, policyRefusal(401, CodeUnauthorized, "Sign in before accessing policies."))
		return
	}
	actor := policyActorOf(held)
	if !validPolicyActor(actor) {
		policyHTTPFailure(w, policyRefusal(403, CodeForbidden, "This session has no supported policy administrator identity."))
		return
	}
	service := s.localPolicyService(r, actor)
	id, revision, operation := r.PathValue("policy"), r.PathValue("revision"), r.PathValue("operation")
	if r.URL.RawQuery != "" || id != "" && !policyIDForm.MatchString(id) || revision != "" && (!policyEventIDForm.MatchString(revision) || !strings.HasPrefix(revision, "revision_")) || operation != "" && !policyOperationIDForm.MatchString(operation) {
		policyHTTPFailure(w, policyRefusal(400, CodeBadRequest, "Invalid policy request."))
		return
	}
	if r.Method == http.MethodGet {
		if id != "" && strings.HasSuffix(r.URL.Path, "/source") {
			source, raw, err := service.source(r.Context(), id)
			if err != nil {
				policyHTTPFailure(w, err)
				return
			}
			w.Header().Set("Content-Type", source.MediaType+"; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": source.Name}))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(raw)
			return
		}
		var result any
		var err error
		switch {
		case operation != "":
			result, err = service.operation(r.Context(), operation)
		case revision != "":
			result, err = service.revision(r.Context(), id, revision)
		case id != "":
			result, err = service.get(r.Context(), id)
		default:
			var items []policySummary
			items, err = service.list(r.Context())
			result = map[string]any{"items": items}
		}
		if err != nil {
			policyHTTPFailure(w, err)
			return
		}
		if err = service.authorized(r.Context()); err != nil {
			policyHTTPFailure(w, err)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	command := policyCommand{PolicyID: id, Actor: actor}
	var body any
	switch {
	case strings.HasSuffix(r.URL.Path, "/review"):
		command.Kind = "review"
		command.Review = &policyReviewRequest{}
		body = command.Review
	case strings.HasSuffix(r.URL.Path, "/publish"):
		command.Kind = "publish"
		command.Publish = &policyPublishRequest{}
		body = command.Publish
	default:
		command.Kind = "save-draft"
		if id == "" {
			command.Kind = "create"
		}
		command.Edit = &policyEditRequest{}
		body = command.Edit
	}
	if inputRequest(r, body) != nil {
		policyHTTPFailure(w, policyRefusal(400, CodeBadRequest, "Provide one policy request with only supported fields."))
		return
	}
	switch {
	case command.Edit != nil:
		command.OperationID = command.Edit.OperationID
	case command.Review != nil:
		command.OperationID = command.Review.OperationID
	default:
		command.OperationID = command.Publish.OperationID
	}
	raw, _ := json.Marshal(struct {
		Kind, PolicyID string
		Actor          policyActor
		Request        any
	}{command.Kind, command.PolicyID, actor, body})
	command.RequestDigest = digestOf(raw)
	result, err := service.execute(r.Context(), command)
	if err != nil {
		policyHTTPFailure(w, err)
		return
	}
	if err = service.authorized(r.Context()); err != nil {
		var failure *policyFailure
		if errors.As(err, &failure) {
			failure.Uncertain = true
		}
		policyHTTPFailure(w, err)
		return
	}
	writeJSON(w, 200, result)
}
