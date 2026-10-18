package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
)

type policyAdoptionChoice struct {
	PolicyID            string                     `json:"policyId"`
	CoverageID          string                     `json:"coverageId"`
	Revision            string                     `json:"revision"`
	CandidateID         string                     `json:"candidateId"`
	ReviewID            string                     `json:"reviewId"`
	PolicyPublicationID string                     `json:"policyPublicationId"`
	PolicyVersion       string                     `json:"policyVersion"`
	Title               string                     `json:"title"`
	Scope               policyCoverageScope        `json:"scope"`
	Statement           policyCoverageStatement    `json:"statement"`
	Pack                policyCoveragePackSelector `json:"pack"`
}

func (s *Server) adoptionOptions(ctx context.Context, service policyService, alias string) (any, error) {
	answer := struct {
		PackPublications []policyCoveragePackChoice `json:"packPublications"`
		Coverages        []policyAdoptionChoice     `json:"coverages"`
		Truncated        bool                       `json:"truncated,omitempty"`
	}{PackPublications: []policyCoveragePackChoice{}, Coverages: []policyAdoptionChoice{}}
	if err := service.authorized(ctx); err != nil {
		return nil, err
	}
	if alias == "" || len(alias) > 4096 {
		return nil, policyRefusal(400, CodeBadRequest, "Choose a configured saved Pack.")
	}
	var config struct {
		Packs map[string]briefDeclaration `json:"packs"`
	}
	configRaw := s.readProjectFile(runtimeConfigName)
	if configRaw.err != nil || !configRaw.present || rejectDuplicateMembers(configRaw.data) != nil || json.Unmarshal(configRaw.data, &config) != nil {
		return nil, policyRefusal(503, "storage-unavailable", "Configured Pack selection is unavailable.")
	}
	d, ok := config.Packs[alias]
	if !ok {
		return nil, policyRefusal(404, CodeNotFound, "The configured Pack is unavailable.")
	}
	raw := s.readProjectFile(d.Path)
	if raw.err != nil || !raw.present || rejectDuplicateMembers(raw.data) != nil {
		return nil, policyRefusal(503, "storage-unavailable", "The saved configured Pack bytes are unavailable.")
	}
	var head struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if json.Unmarshal(raw.data, &head) != nil {
		return nil, policyRefusal(422, "unsupported-pack", "The saved Pack identity is unavailable.")
	}
	ledger, err := s.readCoveragePublicationLedger()
	if err != nil {
		return nil, policyRefusal(503, "storage-unavailable", "Pack publication history is unavailable.")
	}
	selectors := map[policyCoveragePackSelector]policyCoverageResolvedPack{}
	budget := 0
	for i := len(ledger.Items) - 1; i >= 0; i-- {
		p := ledger.Items[i]
		for _, doc := range p.Documents {
			if doc.Kind != "pack" || doc.ID != head.ID || doc.Version != head.Version || doc.Content != artifactReference(raw.data) {
				continue
			}
			selector := coveragePackSelector(policyCoveragePack{PublicationID: p.ID, Document: doc})
			if _, seen := selectors[selector]; seen {
				continue
			}
			budget += len(doc.ID) + len(doc.Version) + len(doc.Alias) + 256
			if len(answer.PackPublications) >= 256 || budget > 1<<20 {
				answer.Truncated = true
				break
			}
			selectors[selector] = policyCoverageResolvedPack{Pack: policyCoveragePack{PublicationID: p.ID, Document: doc}, Publication: p, Raw: raw.data}
			answer.PackPublications = append(answer.PackPublications, policyCoveragePackChoice{PublicationID: p.ID, ArtifactID: doc.ID, Version: doc.Version, Content: doc.Content, Alias: alias})
		}
	}
	err = service.repository.View(ctx, func(l policyLedger) error {
		if err := service.authorized(ctx); err != nil {
			return err
		}
		ids := []string{}
		for id := range l.Coverages {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			r := l.Coverages[id]
			h := coverageHead(r)
			state, review := coverageReviewState(r, h.ID)
			if state != "reviewed" {
				continue
			}
			pack, ok := selectors[h.Pack]
			if !ok {
				continue
			}
			selection := policyAdoptionSelection{PolicyID: r.PolicyID, CoverageID: r.ID, Revision: coverageRevision(r), CandidateID: h.ID, ReviewID: review}
			resolved, e := service.adoptionResolve(ctx, l, pack, []policyAdoptionSelection{selection})
			if e != nil {
				var failure *policyFailure
				if errors.As(e, &failure) && failure.Status == 401 {
					return e
				}
				continue
			}
			dep := resolved.Basis.Dependencies[0]
			encoded, _ := json.Marshal(dep)
			budget += len(encoded)
			if len(answer.Coverages) >= 256 || budget > 2<<20 {
				answer.Truncated = true
				break
			}
			answer.Coverages = append(answer.Coverages, policyAdoptionChoice{PolicyID: selection.PolicyID, CoverageID: selection.CoverageID, Revision: selection.Revision, CandidateID: selection.CandidateID, ReviewID: selection.ReviewID, PolicyPublicationID: dep.PolicyPublicationID, PolicyVersion: dep.Source.ArtifactVersion, Title: dep.Source.Title, Scope: dep.Scope, Statement: dep.Statement, Pack: h.Pack})
		}
		return service.authorized(ctx)
	})
	return answer, err
}
func (s *Server) handlePolicyAdoption(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.guard(w, r) {
		return
	}
	held, ok := s.sessionOf(r)
	if !ok || !validPolicyActor(policyActorOf(held)) {
		policyHTTPFailure(w, policyRefusal(401, CodeUnauthorized, "Sign in before accessing private Policy adoptions."))
		return
	}
	actor := policyActorOf(held)
	service := s.localPolicyService(r, actor)
	id := r.PathValue("adoption")
	options := r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/options")
	if id != "" && !policyAdoptionIDForm.MatchString(id) || !options && r.URL.RawQuery != "" || options && (len(r.URL.Query()) != 1 || len(r.URL.Query()["pack"]) != 1) {
		policyHTTPFailure(w, policyRefusal(400, CodeBadRequest, "Invalid exact adoption selection."))
		return
	}
	var answer any
	var err error
	mutated := false
	if options {
		answer, err = s.adoptionOptions(r.Context(), service, r.URL.Query().Get("pack"))
	} else if r.Method == http.MethodGet {
		if id == "" {
			answer, err = service.adoptionList(r.Context())
		} else {
			answer, err = service.adoptionGet(r.Context(), id)
		}
	} else {
		var q policyAdoptionRequest
		if inputRequest(r, &q) != nil {
			err = policyRefusal(400, CodeBadRequest, "Provide one supported adoption command with only its exact fields.")
		} else {
			answer, err = service.adoptionCommand(r.Context(), actor, q)
			mutated = err == nil
		}
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
	writeJSON(w, 200, answer)
}
