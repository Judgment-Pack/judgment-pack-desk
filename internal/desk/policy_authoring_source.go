package desk

// Policy authoring sources are private Desk provenance. They do not grant
// approval, governing adoption, execution eligibility or portable Policy identity.
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

type policyAuthoringSource struct {
	Version         int                 `json:"version"`
	PolicyID        string              `json:"policyId"`
	RevisionID      string              `json:"revisionId"`
	CandidateDigest string              `json:"candidateDigest"`
	Content         retainedArtifactRef `json:"content"`
	Title           string              `json:"title"`
	ArtifactVersion string              `json:"artifactVersion"`
	Narrative       string              `json:"narrative"`
	PublicationID   string              `json:"publicationId,omitempty"`
}

func (s policyService) authoringSource(ctx context.Context, id, revisionID, publicationID string) (policyAuthoringSource, error) {
	var revision policyRevision
	err := s.repository.View(ctx, func(ledger policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		record, ok := ledger.Policies[id]
		if !ok {
			return policyRefusal(404, CodeNotFound, "Policy not found.")
		}
		var publication *policyPublication
		if publicationID != "" {
			for i := range record.Publications {
				if record.Publications[i].ID == publicationID {
					publication = &record.Publications[i]
					break
				}
			}
			if publication == nil {
				return policyRefusal(404, CodeNotFound, "Policy publication not found.")
			}
			if revisionID != "" && revisionID != publication.CandidateRevisionID {
				return policyRefusal(400, CodeBadRequest, "The Policy publication does not identify that revision.")
			}
			revisionID = publication.CandidateRevisionID
		}
		for _, item := range record.Revisions {
			if item.ID == revisionID {
				revision = item
				break
			}
		}
		if revision.ID == "" {
			return policyRefusal(404, CodeNotFound, "Policy revision not found.")
		}
		if publication != nil && (publication.CandidateDigest != revision.Digest || publication.Content != revision.Content || publication.ArtifactVersion != revision.ArtifactVersion) {
			return errors.New("retained Policy publication is inconsistent")
		}
		return nil
	})
	if err != nil {
		return policyAuthoringSource{}, err
	}
	// Reuse the retained candidate validator, including its exact artifact bytes
	// and original-source binding. Never reconstruct content from caller text.
	candidate, err := s.candidate(ctx, revision)
	if err != nil {
		return policyAuthoringSource{}, err
	}
	if err = s.authorized(ctx); err != nil {
		return policyAuthoringSource{}, err
	}
	return policyAuthoringSource{Version: 1, PolicyID: id, RevisionID: revision.ID, CandidateDigest: candidate.Digest, Content: candidate.Content, Title: candidate.Title, ArtifactVersion: candidate.ArtifactVersion, Narrative: candidate.Narrative, PublicationID: publicationID}, nil
}

func (s *Server) handlePolicyAuthoringSource(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.guard(w, r) {
		return
	}
	held, ok := s.sessionOf(r)
	if !ok || !validPolicyActor(policyActorOf(held)) {
		policyHTTPFailure(w, policyRefusal(401, CodeUnauthorized, "Sign in before accessing Policy authoring sources."))
		return
	}
	id := r.PathValue("policy")
	query, err := url.ParseQuery(r.URL.RawQuery)
	revisionID, publicationID := query.Get("revisionId"), query.Get("publicationId")
	if err != nil || !policyIDForm.MatchString(id) || len(query) != 1 || len(query["revisionId"])+len(query["publicationId"]) != 1 || revisionID == "" && publicationID == "" || revisionID != "" && (!policyEventIDForm.MatchString(revisionID) || !strings.HasPrefix(revisionID, "revision_")) || publicationID != "" && (!policyEventIDForm.MatchString(publicationID) || !strings.HasPrefix(publicationID, "publication_")) {
		policyHTTPFailure(w, policyRefusal(400, CodeBadRequest, "Choose exactly one saved Policy revision or publication."))
		return
	}
	source, err := s.localPolicyService(r, policyActorOf(held)).authoringSource(r.Context(), id, revisionID, publicationID)
	if err != nil {
		var failure *policyFailure
		if !errors.As(err, &failure) {
			err = policyRefusal(503, "storage-unavailable", "The selected saved Policy source could not be read. Keep the selection and retry after checking Policy storage.")
		}
		policyHTTPFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, source)
}

type policyWorkspaceEntry struct {
	ID     string `json:"id"`
	Policy *struct {
		ID string `json:"id"`
	} `json:"policy"`
	PolicySources []policyAuthoringSource `json:"policySources"`
}

type policyInterpretationProposal struct {
	Version       int    `json:"version"`
	ID            string `json:"id"`
	PolicyID      string `json:"policyId"`
	RevisionID    string `json:"revisionId"`
	PublicationID string `json:"publicationId,omitempty"`
	CreatedAt     string `json:"createdAt"`
	AfterTurnID   string `json:"afterTurnId,omitempty"`
	OpenedChatID  string `json:"openedChatId,omitempty"`
}

var policyInterpretationIDForm = regexp.MustCompile(`^[A-Za-z0-9-]{1,80}$`)
var policyInterpretationTimeForm = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func policySourceKey(source policyAuthoringSource) string {
	return source.PolicyID + "/" + source.RevisionID + "/" + source.PublicationID
}

// Validate the source envelope in every workspace/file/backup path. Unknown
// fields inside a source are refused so callers cannot persist a second basis.
func policyWorkspaceEntries(raw []byte, drafts bool) ([]policyWorkspaceEntry, error) {
	if err := rejectDuplicateMembers(raw); err != nil {
		return nil, errors.New("Workspace history has duplicate fields.")
	}
	var documentFields map[string]json.RawMessage
	if json.Unmarshal(raw, &documentFields) != nil || documentFields == nil {
		return nil, errors.New("Invalid Policy workspace history.")
	}
	for key := range documentFields {
		for _, reserved := range []string{"version", "chats", "drafts", "deleted"} {
			if key != reserved && strings.EqualFold(key, reserved) {
				return nil, errors.New("Use exact workspace history field names.")
			}
		}
	}
	var doc struct {
		Chats  []json.RawMessage `json:"chats"`
		Drafts []json.RawMessage `json:"drafts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, errors.New("Invalid Policy workspace history.")
	}
	items := doc.Chats
	if drafts {
		items = doc.Drafts
	}
	entries := make([]policyWorkspaceEntry, 0, len(items))
	ids := map[string]int{}
	for _, item := range items {
		var fields map[string]json.RawMessage
		if json.Unmarshal(item, &fields) != nil || fields == nil {
			return nil, errors.New("Workspace entries must be objects.")
		}
		// encoding/json otherwise accepts case-insensitive aliases. Browser JSON
		// access is case-sensitive, so accepting both could hide a private source
		// from authorization while leaving its canonical field visible to the UI.
		for key := range fields {
			for _, reserved := range []string{"id", "policy", "policySources", "policyInterpretations"} {
				if key != reserved && strings.EqualFold(key, reserved) {
					return nil, errors.New("Use exact workspace Policy field names.")
				}
			}
		}
		var entry policyWorkspaceEntry
		if json.Unmarshal(item, &entry) != nil {
			return nil, errors.New("Invalid Policy workspace entry.")
		}
		if sources, present := fields["policySources"]; present {
			var list []json.RawMessage
			if json.Unmarshal(sources, &list) != nil || list == nil || len(list) > 8 {
				return nil, errors.New("Keep at most eight exact Policy authoring sources per workspace entry.")
			}
			seen := map[string]bool{}
			for _, encoded := range list {
				var source policyAuthoringSource
				var required map[string]json.RawMessage
				if decodeDataJSON(encoded, &source) != nil || json.Unmarshal(encoded, &required) != nil {
					return nil, errors.New("Invalid Policy authoring source.")
				}
				for key := range required {
					if !contains([]string{"version", "policyId", "revisionId", "candidateDigest", "content", "title", "artifactVersion", "narrative", "publicationId"}, key) {
						return nil, errors.New("Use exact Policy authoring source field names.")
					}
				}
				for _, key := range []string{"version", "policyId", "revisionId", "candidateDigest", "content", "title", "artifactVersion", "narrative"} {
					if len(required[key]) == 0 || string(required[key]) == "null" {
						return nil, errors.New("Incomplete Policy authoring source.")
					}
				}
				if _, present := required["publicationId"]; present && source.PublicationID == "" {
					return nil, errors.New("A selected Policy publication must have its exact publication identity.")
				}
				var content map[string]json.RawMessage
				if json.Unmarshal(required["content"], &content) != nil || len(content) != 2 || len(content["sha256"]) == 0 || len(content["bytes"]) == 0 || string(content["sha256"]) == "null" || string(content["bytes"]) == "null" {
					return nil, errors.New("Use the exact retained Policy content identity.")
				}
				key := policySourceKey(source)
				if source.Version != 1 || !policyIDForm.MatchString(source.PolicyID) || !policyEventIDForm.MatchString(source.RevisionID) || !strings.HasPrefix(source.RevisionID, "revision_") || source.PublicationID != "" && (!policyEventIDForm.MatchString(source.PublicationID) || !strings.HasPrefix(source.PublicationID, "publication_")) || !source.Content.valid() || source.Content.Bytes < 1 || source.Content.Bytes > policyCandidateByteLimit || source.CandidateDigest != source.Content.SHA256 || len(source.Title) > 500 || len(source.ArtifactVersion) > policyArtifactVersionLimit || len(source.Narrative) > policyNarrativeLimit || seen[key] {
					return nil, errors.New("Invalid, duplicate or oversized Policy authoring source.")
				}
				seen[key] = true
			}
		}
		if proposals, present := fields["policyInterpretations"]; present {
			var list []json.RawMessage
			if drafts || entry.Policy == nil || json.Unmarshal(proposals, &list) != nil || list == nil || len(list) > 16 {
				return nil, errors.New("Keep at most sixteen interpretation proposals in their owning Policy conversation.")
			}
			seen := map[string]bool{}
			for _, encoded := range list {
				var proposal policyInterpretationProposal
				if decodeDataJSON(encoded, &proposal) != nil || proposal.Version != 1 || !policyInterpretationIDForm.MatchString(proposal.ID) || seen[proposal.ID] || proposal.PolicyID != entry.Policy.ID || !policyEventIDForm.MatchString(proposal.RevisionID) || !strings.HasPrefix(proposal.RevisionID, "revision_") || proposal.PublicationID != "" && (!policyEventIDForm.MatchString(proposal.PublicationID) || !strings.HasPrefix(proposal.PublicationID, "publication_")) || len(proposal.AfterTurnID) > 256 || proposal.OpenedChatID != "" && !policyInterpretationIDForm.MatchString(proposal.OpenedChatID) {
					return nil, errors.New("Invalid Policy interpretation proposal.")
				}
				var fields map[string]json.RawMessage
				if json.Unmarshal(encoded, &fields) != nil {
					return nil, errors.New("Invalid Policy interpretation proposal.")
				}
				for key := range fields {
					if !contains([]string{"version", "id", "policyId", "revisionId", "publicationId", "createdAt", "afterTurnId", "openedChatId"}, key) {
						return nil, errors.New("Use exact Policy interpretation proposal field names.")
					}
				}
				for _, key := range []string{"publicationId", "afterTurnId", "openedChatId"} {
					if value, present := fields[key]; present && string(value) == "null" {
						return nil, errors.New("Optional Policy interpretation fields must have supported values when present.")
					}
				}
				if _, present := fields["publicationId"]; present && proposal.PublicationID == "" {
					return nil, errors.New("Choose an exact Policy publication identity.")
				}
				if _, present := fields["openedChatId"]; present && !policyInterpretationIDForm.MatchString(proposal.OpenedChatID) {
					return nil, errors.New("Use the exact opened interpretation conversation identity.")
				}
				if _, err := time.Parse(time.RFC3339Nano, proposal.CreatedAt); err != nil || !policyInterpretationTimeForm.MatchString(proposal.CreatedAt) {
					return nil, errors.New("Invalid Policy interpretation proposal timestamp.")
				}
				seen[proposal.ID] = true
			}
		}
		if entry.Policy != nil && !policyIDForm.MatchString(entry.Policy.ID) {
			return nil, errors.New("Invalid Policy workspace scope.")
		}
		if (entry.Policy != nil || len(entry.PolicySources) > 0) && (entry.ID == "" || len(entry.ID) > 256) {
			return nil, errors.New("Policy workspace entries need a unique retained identity.")
		}
		ids[entry.ID]++
		entries = append(entries, entry)
	}
	for _, entry := range entries {
		if (entry.Policy != nil || len(entry.PolicySources) > 0) && ids[entry.ID] != 1 {
			return nil, errors.New("Policy workspace entries need a unique retained identity.")
		}
	}
	return entries, nil
}

func (s *Server) authorizeStoredPolicyWorkspace(r *http.Request, root *os.Root) error {
	entries, err := storageEntries(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if privateWorkspaceStorageFile(name) {
			if err = s.authorizePrivateWorkspace(r); err != nil {
				return err
			}
			continue
		}
		drafts := draftPackFileName.MatchString(name)
		if !drafts && !conversationFileName.MatchString(name) {
			continue
		}
		data, err := readPrivateData(root, name, maxConversationBytes)
		if err != nil {
			return err
		}
		if err = s.authorizePolicyWorkspace(r, data, drafts); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) authorizePolicyWorkspace(r *http.Request, raw []byte, drafts bool) error {
	entries, err := policyWorkspaceEntries(raw, drafts)
	if err != nil {
		return withCode(CodeBadRequest, err)
	}
	for _, entry := range entries {
		if entry.Policy != nil || len(entry.PolicySources) > 0 {
			return s.authorizePolicyHistory(r)
		}
	}
	return nil
}

func (s *Server) authorizePolicyHistory(r *http.Request) error {
	held, ok := s.sessionOf(r)
	if !ok || !validPolicyActor(policyActorOf(held)) || s.localPolicyService(r, policyActorOf(held)).authorized(r.Context()) != nil {
		return withCode(CodeUnauthorized, errors.New("Sign in before accessing private Policy history."))
	}
	return nil
}

// Caller holds the workspace CAS/write lock. Policy lookups take only their
// own read transaction; they must never reacquire the workspace lock.
func (s *Server) admitPolicyWorkspaceUpdate(r *http.Request, previous, incoming []byte, drafts bool) error {
	before, err := policyWorkspaceEntries(previous, drafts)
	if err != nil {
		return withCode(CodeBadRequest, err)
	}
	after, err := policyWorkspaceEntries(incoming, drafts)
	if err != nil {
		return withCode(CodeBadRequest, err)
	}
	prior := map[string]policyWorkspaceEntry{}
	for _, entry := range before {
		prior[entry.ID] = entry
	}
	for _, entry := range after {
		old := prior[entry.ID]
		if old.Policy != nil && (entry.Policy == nil || old.Policy.ID != entry.Policy.ID) {
			return withCode(CodeForbidden, errors.New("Keep the private Policy association on retained history; delete the whole entry to remove it."))
		}
		sources := map[string]policyAuthoringSource{}
		for _, source := range entry.PolicySources {
			sources[policySourceKey(source)] = source
		}
		unchanged := map[string]bool{}
		for _, source := range old.PolicySources {
			key := policySourceKey(source)
			if sources[key] != source {
				return withCode(CodeForbidden, errors.New("Keep the exact private Policy source on retained history; delete the whole entry to remove it."))
			}
			unchanged[key] = true
		}
		for _, source := range entry.PolicySources {
			if unchanged[policySourceKey(source)] {
				continue // Historical availability is independent of current access.
			}
			held, ok := s.sessionOf(r)
			if !ok || !validPolicyActor(policyActorOf(held)) {
				return s.authorizePolicyHistory(r)
			}
			resolved, err := s.localPolicyService(r, policyActorOf(held)).authoringSource(r.Context(), source.PolicyID, source.RevisionID, source.PublicationID)
			if err != nil {
				var failure *policyFailure
				if errors.As(err, &failure) {
					return err
				}
				return policyRefusal(503, "storage-unavailable", "The selected Policy source is unavailable; nothing was saved. Check Policy storage before retrying.")
			}
			if source != resolved {
				return withCode(CodeBadRequest, errors.New("The Policy authoring source changed. Select its exact saved revision again."))
			}
		}
	}
	return nil
}
