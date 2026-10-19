package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
)

func validatePolicyAdoptions(l policyLedger) error {
	bad := errors.New("Policy adoption storage is inconsistent")
	if len(l.Adoptions) > policyAdoptionCountLimit {
		return bad
	}
	for key, r := range l.Adoptions {
		if r.SchemaVersion != 1 || key != r.ID || !policyAdoptionIDForm.MatchString(key) || !policyAdoptionCandidateIDForm.MatchString(r.CandidateID) || !r.Content.valid() || r.Content.Bytes < 1 || r.Content.Bytes > policyAdoptionContentLimit || !validPolicyActor(r.Actor) || !validPolicyTime(r.CreatedAt) || !validPolicyTime(r.UpdatedAt) || r.Reviews == nil || len(r.Reviews) > policyHistoryLimit {
			return bad
		}
		at, current := r.CreatedAt, ""
		seen := map[string]bool{}
		for i, v := range r.Reviews {
			if !policyAdoptionReviewIDForm.MatchString(v.ID) || seen[v.ID] || v.Sequence != i+1 || v.CandidateID != r.CandidateID || v.CandidateDigest != r.Content.SHA256 || !retainedArtifactDigest.MatchString(v.BasisDigest) || !contains([]string{"approve", "request-changes"}, v.Decision) || len(v.Comment) > 8192 || !validPolicyActor(v.Actor) || !validPolicyTime(v.At) || (v.Decision == "approve") != (v.Check != nil) || v.Check != nil && (!v.Check.valid() || v.Check.Bytes > policyCoverageCheckByteLimit) {
				return bad
			}
			seen[v.ID] = true
			at, current = v.At, v.ID
		}
		if r.UpdatedAt != at || r.CurrentReviewID != current {
			return bad
		}
		if r.Recovery != nil {
			found := false
			for _, v := range l.Recoveries {
				if v.SnapshotID == r.Recovery.SnapshotID && v.At == r.Recovery.RestoredAt {
					found = true
				}
			}
			if !found || r.Recovery.Sequence < 0 || r.Recovery.Sequence > len(r.Reviews) {
				return bad
			}
		}
	}
	return nil
}
func adoptionDependencyEligibility(l policyLedger, id, publicationID, now string) (string, error) {
	p := l.Policies[id]
	a := l.SourceAuthorities[id]
	publication, _, err := coveragePublication(p, publicationID)
	if err != nil {
		return "ineligible", err
	}
	owner, boundary := policyAuthorityOwnership(a)
	boundary = policyRecoveryReviewBoundary(p, boundary)
	if owner.Mode != "desk-managed" {
		return "ineligible", policyRefusal(409, "external-ownership", "This slice cannot adopt publications under external lifecycle authority.")
	}
	if publication.Sequence <= boundary {
		return "ineligible", policyRefusal(409, "restored-review-required", "Publish a newly reviewed Policy under current ownership before adopting it. Restored history remains inactive.")
	}
	if policySourcePending(p, l.Sources[id]) || policyRecoverySourceHeld(l.Sources[id]) {
		return "ineligible", policyRefusal(409, "source-update-unresolved", "Resolve the current Policy source update before adoption review.")
	}
	for _, v := range policyLifecycleView(p, l.Lifecycles[id], now, a).Releases {
		if v.PublicationID == publicationID {
			if contains([]string{"withdrawn", "expired", "superseded"}, v.Status) {
				return "ineligible", policyRefusal(409, "dependency-ineligible", "A chosen Policy publication is withdrawn, expired or superseded.")
			}
			if v.Status == "effective" {
				return "effective", nil
			}
			return "not-yet-effective", nil
		}
	}
	return "ineligible", policyRefusal(409, "dependency-ineligible", "The chosen Policy publication has no current lifecycle state.")
}
func (s policyService) adoptionResolve(ctx context.Context, l policyLedger, pack policyCoverageResolvedPack, selections []policyAdoptionSelection) (policyAdoptionMaterial, error) {
	m := policyAdoptionMaterial{Basis: policyAdoptionBasis{Pack: coveragePackSelector(pack.Pack), Publication: pack.Publication, Dependencies: []policyAdoptionDependency{}}, Materials: []policyCoverageMaterial{}}
	seen := map[string]bool{}
	for _, selection := range selections {
		if seen[selection.PolicyID] {
			return m, policyRefusal(400, CodeBadRequest, "Choose each Policy once in the complete adoption set.")
		}
		seen[selection.PolicyID] = true
		p := l.Policies[selection.PolicyID]
		c := l.Coverages[selection.CoverageID]
		head := coverageHead(c)
		if p.ID == "" || c.PolicyID != selection.PolicyID || c.ID == "" {
			return m, policyRefusal(404, CodeNotFound, "A chosen coverage snapshot is unavailable.")
		}
		state, reviewID := coverageReviewState(c, head.ID)
		if coverageRevision(c) != selection.Revision || head.ID != selection.CandidateID || reviewID != selection.ReviewID || state != "reviewed" {
			return m, policyRefusal(409, "revision-conflict", "Choose the current exact approved coverage snapshot again.")
		}
		if head.Pack != m.Basis.Pack || head.Status == "awaiting-interpretation" {
			return m, policyRefusal(409, "review-incomplete", "Every chosen coverage must cover this exact Pack and have a complete interpretation.")
		}
		var review policyCoverageReview
		for _, v := range c.Reviews {
			if v.ID == reviewID {
				review = v
			}
		}
		policyRevision := policyEffectiveRevision(p, l.SourceAuthorities[p.ID])
		if review.Check == nil || review.Decision != "approve" || review.PolicyRevision != policyRevision {
			return m, policyRefusal(409, "revision-conflict", "The Policy context changed after its coverage review. Review coverage again before adoption.")
		}
		if _, err := adoptionDependencyEligibility(l, p.ID, head.PolicyPublicationID, s.lifecycleNow()); err != nil {
			return m, err
		}
		material, err := s.coverageMaterial(ctx, p, head)
		if err != nil {
			return m, policyBeforeCommitFailure(err)
		}
		// Validate retained coverage evidence, without converting prototype approval
		// into a candidate-format approval. Adoption still requires its own review.
		if _, err = archivedCoverageCheck(ctx, s.artifacts, review, material); err != nil {
			return m, policyRefusal(409, "integrity-failed", "The exact retained coverage approval evidence is inconsistent.")
		}
		d := policyAdoptionDependency{Selection: selection, CandidateDigest: head.Digest, CoverageContent: head.Content, CoverageReview: review, CoverageRecovery: c.Recovery, PolicyRevision: policyRevision, LifecycleRevision: policyLifecycleRevision(l.Lifecycles[p.ID]), SourceRevision: policySourceRevision(l.Sources[p.ID]), AuthorityRevision: policyAuthorityRevision(l.SourceAuthorities[p.ID]), PolicyPublicationID: head.PolicyPublicationID, Source: material.Content.Source, Scope: material.Content.Scope, Statement: material.Content.Statement}
		m.Basis.Dependencies = append(m.Basis.Dependencies, d)
		m.Materials = append(m.Materials, material)
	}
	return m, s.authorized(ctx)
}
func (s policyService) adoptionContent(ctx context.Context, r policyAdoptionRecord) (policyAdoptionContent, error) {
	var c policyAdoptionContent
	raw, err := s.artifacts.Get(ctx, r.Content)
	if err != nil {
		return c, err
	}
	if len(raw) > policyAdoptionContentLimit || coverageDecode(raw, &c) != nil || c.SchemaVersion != 1 || c.FormatVersion != policyAdoptionFormat || c.PortableID != "urn:desk:adoption-candidate:"+r.CandidateID || c.BasisDigest != adoptionBasisDigest(c.Basis) || !validCoveragePackSelector(c.Basis.Pack) || len(c.Basis.Dependencies) < 1 || len(c.Basis.Dependencies) > policyAdoptionSetLimit || !c.Document.valid() || !c.Check.valid() || c.Materials == nil || len(c.Materials) > 16 {
		return c, errors.New("Retained adoption candidate is inconsistent")
	}
	seen := map[string]bool{}
	for _, d := range c.Basis.Dependencies {
		if !validAdoptionSelection(d.Selection) || seen[d.Selection.PolicyID] || d.CandidateDigest != d.CoverageContent.SHA256 || !d.CoverageContent.valid() || d.CoverageReview.ID != d.Selection.ReviewID || d.CoverageReview.CandidateID != d.Selection.CandidateID || d.CoverageReview.CandidateDigest != d.CandidateDigest || d.CoverageReview.Decision != "approve" || d.CoverageReview.Check == nil || d.CoverageReview.PolicyRevision != d.PolicyRevision || d.Source.PolicyID != d.Selection.PolicyID || d.Source.PublicationID != d.PolicyPublicationID || d.Scope.Kind != "whole-narrative" || d.Statement.Status == "awaiting-interpretation" || d.Statement.validate() != nil {
			return c, errors.New("Retained adoption dependency is inconsistent")
		}
		seen[d.Selection.PolicyID] = true
	}
	return c, nil
}
func adoptionSelections(c policyAdoptionContent) []policyAdoptionSelection {
	v := []policyAdoptionSelection{}
	for _, d := range c.Basis.Dependencies {
		v = append(v, d.Selection)
	}
	return v
}
func adoptionCandidate(r policyAdoptionRecord, c policyAdoptionContent) policyAdoptionCandidate {
	return policyAdoptionCandidate{ID: r.CandidateID, Digest: r.Content.SHA256, Content: r.Content, PortableID: c.PortableID, FormatVersion: c.FormatVersion, BasisDigest: c.BasisDigest, Pack: c.Basis.Pack, Dependencies: c.Basis.Dependencies, Document: c.Document, Materials: c.Materials, Check: c.Check}
}
func (s policyService) adoptionReply(ctx context.Context, l policyLedger, r policyAdoptionRecord, c policyAdoptionContent, pack *policyCoverageResolvedPack) policyAdoptionReply {
	state, review := adoptionReviewState(r)
	reply := policyAdoptionReply{Revision: adoptionRevision(r), Document: policyAdoptionDocument{SchemaVersion: 1, ID: r.ID, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Candidate: adoptionCandidate(r, c), ReviewState: state, CurrentReviewID: review, Reviews: r.Reviews}, Readiness: policyAdoptionState{Status: "ready"}, Eligibility: policyAdoptionState{Status: "effective"}}
	if pack == nil {
		reply.Readiness = policyAdoptionState{Status: "unavailable", Reason: "The original exact Pack publication is unavailable. Retained history remains inspectable."}
		reply.Eligibility.Status = "unavailable"
	} else {
		current, err := s.adoptionResolve(ctx, l, *pack, adoptionSelections(c))
		if err != nil || adoptionBasisDigest(current.Basis) != c.BasisDigest {
			reply.Readiness = policyAdoptionState{Status: "stale", Reason: "The complete chosen Policy, coverage or lifecycle basis changed. Prepare a new candidate before review."}
		}
		for _, d := range c.Basis.Dependencies {
			status, err := adoptionDependencyEligibility(l, d.Selection.PolicyID, d.PolicyPublicationID, s.lifecycleNow())
			if err != nil {
				reply.Eligibility = policyAdoptionState{Status: "ineligible", Reason: "A chosen publication is currently ineligible."}
				break
			}
			if status != "effective" {
				reply.Eligibility = policyAdoptionState{Status: "not-yet-effective", Reason: "At least one chosen publication is published or scheduled and is not yet effective."}
			}
		}
	}
	if r.Recovery != nil {
		reply.RecoveryBoundary = r.Recovery
		reply.Recovery = &policyRecoveryView{SnapshotID: r.Recovery.SnapshotID, RestoredAt: r.Recovery.RestoredAt, CurrentnessUnknown: true, FreshReviewRequired: true}
		reply.Readiness = policyAdoptionState{Status: "stale", Reason: "Restored adoption history is inactive. Prepare and explicitly review a new destination candidate."}
		reply.Eligibility = policyAdoptionState{Status: "ineligible", Reason: "Restored adoption history conveys no current authority."}
	}
	return reply
}
func (s policyService) adoptionGet(ctx context.Context, id string) (policyAdoptionReply, error) {
	var result policyAdoptionReply
	var record policyAdoptionRecord
	var content policyAdoptionContent
	err := s.repository.View(ctx, func(l policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		record = l.Adoptions[id]
		if record.ID == "" {
			return policyRefusal(404, CodeNotFound, "Adoption not found.")
		}
		var err error
		content, err = s.adoptionContent(ctx, record)
		return err
	})
	if err != nil {
		return result, err
	}
	var pack *policyCoverageResolvedPack
	if s.coveragePack != nil {
		p, err := s.coveragePack(ctx, content.Basis.Pack)
		if err == nil {
			pack = &p
		}
	}
	err = s.repository.View(ctx, func(l policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		if adoptionRevision(l.Adoptions[id]) != adoptionRevision(record) {
			return policyRefusal(409, "revision-conflict", "This adoption review changed. Reload its saved history.")
		}
		result = s.adoptionReply(ctx, l, record, content, pack)
		return s.authorized(ctx)
	})
	return result, err
}
func (s policyService) adoptionList(ctx context.Context) (any, error) {
	items := []policyAdoptionSummary{}
	err := s.repository.View(ctx, func(l policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		for _, r := range l.Adoptions {
			c, err := s.adoptionContent(ctx, r)
			if err != nil {
				return err
			}
			state, _ := adoptionReviewState(r)
			items = append(items, policyAdoptionSummary{ID: r.ID, Revision: adoptionRevision(r), CandidateDigest: r.Content.SHA256, Pack: c.Basis.Pack, Dependencies: adoptionSelections(c), ReviewState: state, UpdatedAt: r.UpdatedAt})
		}
		return s.authorized(ctx)
	})
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt == items[j].UpdatedAt {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	return struct {
		Items []policyAdoptionSummary `json:"items"`
	}{items}, err
}
func (s policyService) adoptionRunCheck(ctx context.Context, m policyAdoptionMaterial) policyAdoptionCheck {
	if s.checkAdoption == nil {
		return policyAdoptionCheck{Availability: "unavailable", Reason: "Use a Runtime supporting official Policy draft policy-v1.0.0-draft.1 (format 1.0.0). Saved drafts and retained history remain available."}
	}
	return s.checkAdoption(ctx, m)
}
func adoptionCheckFailure(c policyAdoptionCheck) error {
	code, status := "adoption-check-failed", 422
	if c.Availability == "unavailable" {
		code, status = "runtime-unavailable", 503
	}
	if c.Availability == "unsupported" {
		code = "unsupported-contract"
	}
	return policyRefusal(status, code, "Runtime could not confirm the complete exact candidate set. No adoption review was committed.")
}
func (s policyService) adoptionCommand(ctx context.Context, actor policyActor, q policyAdoptionRequest) (policyMutationReply, error) {
	var empty policyMutationReply
	if err := s.authorized(ctx); err != nil {
		return empty, err
	}
	if err := validateAdoptionRequest(q); err != nil {
		return empty, err
	}
	raw, _ := json.Marshal(struct {
		Actor   policyActor
		Request policyAdoptionRequest
	}{actor, q})
	command := policyCommand{Kind: "adoption-" + q.Action, OperationID: q.OperationID, RequestDigest: digestOf(raw), Actor: actor}
	var previous policyAdoptionRecord
	var content policyAdoptionContent
	var recovered *policyMutationReply
	err := s.repository.View(ctx, func(l policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		if q.Action == "prepare" {
			command.PolicyID = q.Coverages[0].PolicyID
		} else {
			previous = l.Adoptions[q.AdoptionID]
			if previous.ID == "" {
				return policyRefusal(404, CodeNotFound, "Adoption not found.")
			}
			var e error
			content, e = s.adoptionContent(ctx, previous)
			if e != nil {
				return e
			}
			command.PolicyID = content.Basis.Dependencies[0].Selection.PolicyID
		}
		var e error
		recovered, e = s.recovered(ctx, l, command)
		if e != nil || recovered != nil {
			return e
		}
		if q.Action == "review" && (adoptionRevision(previous) != q.BaseRevision || previous.Content.SHA256 != q.CandidateDigest || content.BasisDigest != q.BasisDigest) {
			return policyRefusal(409, "revision-conflict", "Review the exact current immutable adoption candidate.")
		}
		return nil
	})
	if err != nil {
		return empty, err
	}
	if recovered != nil {
		return *recovered, nil
	}
	var pack policyCoverageResolvedPack
	selector := q.Pack
	if q.Action == "review" {
		selector = &content.Basis.Pack
	}
	requireBasis := q.Action == "prepare" || q.Decision == "approve"
	packPresent := false
	if s.coveragePack != nil {
		pack, err = s.coveragePack(ctx, *selector)
		packPresent = err == nil
		if err != nil && requireBasis {
			return empty, err
		}
	} else if requireBasis {
		return empty, policyRefusal(503, "storage-unavailable", "Exact Pack publication storage is unavailable.")
	}
	selections := q.Coverages
	if q.Action == "review" {
		selections = adoptionSelections(content)
	}
	var material policyAdoptionMaterial
	// Restrictive review can be recorded even when publication storage is absent.
	// Approval re-resolves the complete live basis and every retained dependency.
	if requireBasis {
		err = s.repository.View(ctx, func(l policyLedger) error {
			if err := s.authorized(ctx); err != nil {
				return err
			}
			var e error
			material, e = s.adoptionResolve(ctx, l, pack, selections)
			return e
		})
		if err != nil {
			return empty, err
		}
		if q.Action == "review" && (previous.Recovery != nil || adoptionBasisDigest(material.Basis) != content.BasisDigest) {
			return empty, policyRefusal(409, "revision-conflict", "The complete adoption basis changed or was restored. Prepare a new candidate for explicit review.")
		}
	}
	candidateID := previous.CandidateID
	if q.Action == "prepare" {
		candidateID, err = newPolicyEventID("adoption_candidate")
		if err != nil {
			return empty, policyBeforeCommitFailure(err)
		}
	}
	material.PortableID = "urn:desk:adoption-candidate:" + candidateID
	var check policyAdoptionCheck
	if requireBasis {
		check = s.adoptionRunCheck(ctx, material)
		if err := s.authorized(ctx); err != nil {
			return empty, err
		}
		if !adoptionCheckAccepted(check) || check.prepared == nil {
			return empty, adoptionCheckFailure(check)
		}
	}
	if requireBasis && (len(check.prepared.Materials) > 16 || len(check.prepared.Document) > 262144 || len(check.Report) > policyCoverageCheckByteLimit) {
		return empty, policyRefusal(422, "unsupported-contract", "The complete chosen candidate exceeds supported retained material bounds.")
	}
	if q.Action == "review" && q.Decision == "approve" {
		doc, e := s.artifacts.Get(ctx, content.Document)
		if e != nil || !bytes.Equal(doc, check.prepared.Document) {
			return empty, policyRefusal(409, "integrity-failed", "Runtime's fresh candidate does not match the retained immutable adoption.")
		}
	}
	var result policyMutationReply
	err = s.repository.Update(ctx, func(l *policyLedger) (bool, error) {
		if err := s.authorized(ctx); err != nil {
			return false, err
		}
		r, e := s.recovered(ctx, *l, command)
		if e != nil {
			return false, e
		}
		if r != nil {
			result = *r
			return false, nil
		}
		if len(l.Operations) >= policyOperationLimit {
			return false, policyRefusal(409, "storage-capacity", "Policy operation history has reached capacity.")
		}
		var currentPack *policyCoverageResolvedPack
		if requireBasis {
			// Re-resolve publication inside the policy transaction immediately before
			// committing. Runtime has already completed outside this write lock.
			p, e := s.coveragePack(ctx, *selector)
			if e != nil {
				return false, e
			}
			if !samePolicyStatusValue(p.Pack, pack.Pack) || !samePolicyStatusValue(p.Publication, pack.Publication) || !bytes.Equal(p.Raw, pack.Raw) {
				return false, policyRefusal(409, "revision-conflict", "The exact Pack publication changed during preparation.")
			}
			currentPack = &p
			current, e := s.adoptionResolve(ctx, *l, p, selections)
			if e != nil {
				return false, e
			}
			if adoptionBasisDigest(current.Basis) != adoptionBasisDigest(material.Basis) {
				return false, policyRefusal(409, "revision-conflict", "The complete selected basis changed during Runtime checking.")
			}
		} else if packPresent {
			currentPack = &pack
		}
		at := s.lifecycleNow()
		record := previous
		if q.Action == "prepare" {
			if len(l.Adoptions) >= policyAdoptionCountLimit {
				return false, policyRefusal(409, "storage-capacity", "Adoption history has reached capacity.")
			}
			id, e := newPolicyEventID("adoption")
			if e != nil {
				return false, e
			}
			content = policyAdoptionContent{SchemaVersion: 1, PortableID: material.PortableID, FormatVersion: policyAdoptionFormat, BasisDigest: adoptionBasisDigest(material.Basis), Basis: material.Basis, Materials: []retainedArtifactRef{}}
			content.Document, e = s.artifacts.Put(ctx, check.prepared.Document)
			if e != nil {
				return false, e
			}
			content.Check, e = s.artifacts.Put(ctx, check.Report)
			if e != nil {
				return false, e
			}
			for _, data := range check.prepared.Materials {
				ref, e := s.artifacts.Put(ctx, data)
				if e != nil {
					return false, e
				}
				content.Materials = append(content.Materials, ref)
			}
			raw, _ := json.Marshal(content)
			if len(raw) > policyAdoptionContentLimit {
				return false, policyRefusal(413, "storage-capacity", "The complete adoption candidate exceeds private storage bounds.")
			}
			ref, e := s.artifacts.Put(ctx, raw)
			if e != nil {
				return false, e
			}
			record = policyAdoptionRecord{SchemaVersion: 1, ID: id, CreatedAt: at, UpdatedAt: at, Actor: actor, CandidateID: candidateID, Content: ref, Reviews: []policyAdoptionReview{}}
		} else {
			record = l.Adoptions[q.AdoptionID]
			if adoptionRevision(record) != q.BaseRevision || record.Content.SHA256 != q.CandidateDigest {
				return false, policyRefusal(409, "revision-conflict", "This adoption review changed during Runtime checking.")
			}
			if len(record.Reviews) >= policyHistoryLimit {
				return false, policyRefusal(409, "storage-capacity", "Adoption review history has reached capacity.")
			}
			id, e := newPolicyEventID("adoption_review")
			if e != nil {
				return false, e
			}
			review := policyAdoptionReview{ID: id, Sequence: len(record.Reviews) + 1, CandidateID: record.CandidateID, CandidateDigest: record.Content.SHA256, BasisDigest: content.BasisDigest, Decision: q.Decision, Comment: q.Comment, At: at, Actor: actor}
			if q.Decision == "approve" {
				ref, e := s.artifacts.Put(ctx, check.Report)
				if e != nil {
					return false, e
				}
				review.Check = &ref
			}
			record.Reviews = append(record.Reviews, review)
			record.CurrentReviewID = id
			record.UpdatedAt = at
		}
		if l.Adoptions == nil {
			l.Adoptions = map[string]policyAdoptionRecord{}
		}
		l.Adoptions[record.ID] = record
		reply := s.adoptionReply(ctx, *l, record, content, currentPack)
		receipt := policyOperationReceipt{OperationID: q.OperationID, Kind: command.Kind, PolicyID: command.PolicyID, RequestDigest: command.RequestDigest, At: at, Actor: actor, Revision: content.BasisDigest, CandidateDigest: record.Content.SHA256, ReviewID: record.CurrentReviewID, AdoptionID: record.ID, AdoptionRevision: reply.Revision, AdoptionCandidateID: record.CandidateID}
		result = policyMutationReply{Delivery: "committed", OutcomeKind: "adoption", PolicyID: command.PolicyID, PolicyRevision: content.BasisDigest, Adoption: &reply, Receipt: receipt}
		encoded, _ := json.Marshal(result)
		ref, e := s.artifacts.Put(ctx, encoded)
		if e != nil {
			return false, e
		}
		l.Operations[q.OperationID] = policyStoredOperation{OperationID: q.OperationID, Kind: command.Kind, PolicyID: command.PolicyID, RequestDigest: command.RequestDigest, Result: ref}
		return true, s.authorized(ctx)
	})
	return result, err
}
func validPolicyAdoptionResult(r policyMutationReply) bool {
	if r.Receipt.Kind != "adoption-prepare" && r.Receipt.Kind != "adoption-review" {
		return r.Adoption == nil && r.Receipt.AdoptionID == "" && r.Receipt.AdoptionRevision == "" && r.Receipt.AdoptionCandidateID == ""
	}
	a := r.Adoption
	if a == nil || r.OutcomeKind != "adoption" || r.Lifecycle != nil || r.SourceStatus != nil || r.SourceSync != nil || r.Coverage != nil || a.Document.SchemaVersion != 1 || a.Document.ID != r.Receipt.AdoptionID || a.Revision != r.Receipt.AdoptionRevision || a.Document.Candidate.ID != r.Receipt.AdoptionCandidateID || a.Document.Candidate.Digest != r.Receipt.CandidateDigest || a.Document.Candidate.Content.SHA256 != r.Receipt.CandidateDigest || a.Document.Candidate.BasisDigest != r.Receipt.Revision || !policyAdoptionIDForm.MatchString(a.Document.ID) || !policyAdoptionCandidateIDForm.MatchString(a.Document.Candidate.ID) || !retainedArtifactDigest.MatchString(a.Revision) || !contains([]string{"unreviewed", "reviewed", "changes-requested"}, a.Document.ReviewState) {
		return false
	}
	if r.Receipt.Kind == "adoption-review" {
		if len(a.Document.Reviews) == 0 {
			return false
		}
		v := a.Document.Reviews[len(a.Document.Reviews)-1]
		return v.ID == r.Receipt.ReviewID && v.Actor == r.Receipt.Actor && v.At == r.Receipt.At && v.CandidateDigest == r.Receipt.CandidateDigest && v.BasisDigest == r.Receipt.Revision
	}
	return r.Receipt.ReviewID == "" && len(a.Document.Reviews) == 0 && a.Document.ReviewState == "unreviewed"
}
