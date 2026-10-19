package desk

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
)

func (s *Server) readCoveragePublicationLedger() (publicationLedger, error) {
	root, _, err := openOwnFolder(s.root, strings.Split(publicationDirectory, "/"), false, "artifact versions")
	if errors.Is(err, os.ErrNotExist) {
		return publicationLedger{Version: 1, Items: []artifactPublication{}}, nil
	}
	if err != nil {
		return publicationLedger{}, err
	}
	defer root.Close()
	return readPublicationLedger(root)
}
func (s *Server) resolveCoveragePack(ctx context.Context, selector policyCoveragePackSelector) (policyCoverageResolvedPack, error) {
	var result policyCoverageResolvedPack
	ledger, err := s.readCoveragePublicationLedger()
	if err != nil {
		return result, policyRefusal(503, "storage-unavailable", "Pack publication history is unavailable.")
	}
	for _, publication := range ledger.Items {
		if publication.ID != selector.PublicationID {
			continue
		}
		for _, d := range publication.Documents {
			if d.Kind == "pack" && d.ID == selector.ArtifactID && d.Version == selector.Version && d.Content == selector.Content {
				raw, err := s.artifactRepository().Get(ctx, d.Content)
				if err != nil {
					return result, policyRefusal(503, "storage-unavailable", "The selected exact Pack publication bytes are unavailable.")
				}
				return policyCoverageResolvedPack{Pack: policyCoveragePack{PublicationID: publication.ID, Document: d}, Publication: publication, Raw: raw}, nil
			}
		}
	}
	return result, policyRefusal(404, CodeNotFound, "The selected exact Pack publication is missing.")
}
func (s *Server) coverageOptions(ctx context.Context, service policyService, id string) (any, error) {
	answer := struct {
		PolicyRevision     string                            `json:"policyRevision"`
		PolicyPublications []policyCoveragePublicationChoice `json:"policyPublications"`
		PackPublications   []policyCoveragePackChoice        `json:"packPublications"`
		Truncated          bool                              `json:"truncated,omitempty"`
		Recovery           *policyRecoveryView               `json:"recovery,omitempty"`
	}{PolicyPublications: []policyCoveragePublicationChoice{}, PackPublications: []policyCoveragePackChoice{}}
	err := service.repository.View(ctx, func(l policyLedger) error {
		if err := service.authorized(ctx); err != nil {
			return err
		}
		record := l.Policies[id]
		if record.ID == "" {
			return policyRefusal(404, CodeNotFound, "Policy not found.")
		}
		answer.PolicyRevision = policyEffectiveRevision(record, l.SourceAuthorities[id])
		answer.Recovery = policyRecoveryProjection(record, l.SourceAuthorities[id])
		for i := len(record.Publications) - 1; i >= 0; i-- {
			p := record.Publications[i]
			_, r, err := coveragePublication(record, p.ID)
			if err != nil {
				return err
			}
			answer.PolicyPublications = append(answer.PolicyPublications, policyCoveragePublicationChoice{PublicationID: p.ID, RevisionID: p.CandidateRevisionID, CandidateDigest: p.CandidateDigest, ArtifactVersion: p.ArtifactVersion, Title: r.Title, Content: p.Content, At: p.At})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	ledger, err := s.readCoveragePublicationLedger()
	if err != nil {
		return nil, policyRefusal(503, "storage-unavailable", "Pack publication choices are unavailable.")
	}
	budget := 0
	for i := len(ledger.Items) - 1; i >= 0; i-- {
		p := ledger.Items[i]
		seen := map[policyCoveragePackSelector]bool{}
		for _, d := range p.Documents {
			if d.Kind != "pack" {
				continue
			}
			selector := coveragePackSelector(policyCoveragePack{PublicationID: p.ID, Document: d})
			if seen[selector] {
				continue
			}
			seen[selector] = true
			budget += len(d.ID) + len(d.Version) + len(d.Alias) + 256
			if len(answer.PackPublications) >= 256 || budget > 1<<20 {
				answer.Truncated = true
				return answer, service.authorized(ctx)
			}
			answer.PackPublications = append(answer.PackPublications, policyCoveragePackChoice{PublicationID: selector.PublicationID, ArtifactID: selector.ArtifactID, Version: selector.Version, Content: selector.Content, Alias: d.Alias})
		}
	}
	return answer, service.authorized(ctx)
}
func (s *Server) handlePolicyCoverage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.guard(w, r) {
		return
	}
	held, ok := s.sessionOf(r)
	if !ok || !validPolicyActor(policyActorOf(held)) {
		policyHTTPFailure(w, policyRefusal(401, CodeUnauthorized, "Sign in before accessing private Policy coverage."))
		return
	}
	actor := policyActorOf(held)
	service := s.localPolicyService(r, actor)
	id, coverage, candidate := r.PathValue("policy"), r.PathValue("coverage"), r.PathValue("candidate")
	if r.URL.RawQuery != "" || !policyIDForm.MatchString(id) || coverage != "" && !policyCoverageIDForm.MatchString(coverage) || candidate != "" && !policyCoverageCandidateIDForm.MatchString(candidate) {
		policyHTTPFailure(w, policyRefusal(400, CodeBadRequest, "Invalid exact Policy coverage selection."))
		return
	}
	var answer any
	var err error
	mutated := false
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/coverage-options"):
		answer, err = s.coverageOptions(r.Context(), service, id)
	case r.Method == http.MethodGet && coverage != "":
		answer, err = service.coverageGet(r.Context(), id, coverage, candidate)
	case r.Method == http.MethodGet:
		answer, err = service.coverageList(r.Context(), id)
	case strings.HasSuffix(r.URL.Path, "/resolve"):
		var q policyCoverageResolveRequest
		if inputRequest(r, &q) != nil {
			err = policyRefusal(400, CodeBadRequest, "Choose exact Policy and Pack publications.")
			break
		}
		answer, err = service.coverageResolve(r.Context(), id, q)
	case strings.HasSuffix(r.URL.Path, "/check"):
		var q policyCoverageCheckRequest
		if inputRequest(r, &q) != nil || !retainedArtifactDigest.MatchString(q.BaseRevision) || !retainedArtifactDigest.MatchString(q.CandidateDigest) || !retainedArtifactDigest.MatchString(q.PolicyRevision) {
			err = policyRefusal(400, CodeBadRequest, "Check the exact saved coverage candidate and current Policy context.")
			break
		}
		answer, err = service.coverageCheck(r.Context(), id, coverage, q)
	default:
		var q policyCoverageRequest
		if inputRequest(r, &q) != nil {
			err = policyRefusal(400, CodeBadRequest, "Provide one supported coverage action with only its exact fields.")
			break
		}
		answer, err = service.coverageCommand(r.Context(), id, actor, q)
		mutated = err == nil
	}
	if err == nil {
		err = service.authorized(r.Context())
		if err != nil && mutated {
			var f *policyFailure
			if errors.As(err, &f) {
				f.Uncertain = true
			}
		}
	}
	if err != nil {
		policyHTTPFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, answer)
}
