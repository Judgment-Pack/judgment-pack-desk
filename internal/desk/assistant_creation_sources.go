package desk

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

var assistantCreationPage = regexp.MustCompile(`^attachment:([a-f0-9-]{36})/(sha256:[a-f0-9]{64})/page/([1-9][0-9]*)$`)

type assistantCreationCitation struct {
	SourceID  string `json:"sourceId"`
	Location  string `json:"location"`
	URL       string `json:"url"`
	Quote     string `json:"quote"`
	Traced    bool   `json:"traced"`
	ExcerptID any    `json:"excerptId"`
	Reason    string `json:"reason"`
}
type assistantCreationSourceProof struct {
	Location         string `json:"location"`
	AttachmentDigest string `json:"attachmentDigest"`
	GatewayDigest    string `json:"gatewayDigest"`
}
type assistantCreationSourceCache struct {
	candidate string
	citations []assistantCreationCitation
	proofs    []assistantCreationSourceProof
	documents map[string]assistantVerifiedDocument
}

func assistantCreationDeclaredCitations(text string) ([]assistantCreationCitation, error) {
	pack, err := assistantTestObject([]byte(text))
	if err != nil {
		return nil, err
	}
	citations := []assistantCreationCitation{}
	if pack["sources"] == nil {
		return citations, nil
	}
	rows, err := assistantChatArray(pack["sources"])
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, raw := range rows {
		row, err := assistantTestObject(raw)
		if err != nil {
			return nil, err
		}
		citation, err := assistantTestObject(row["citation"])
		if err != nil {
			return nil, err
		}
		locator, err := assistantTestObject(row["locator"])
		if err != nil {
			return nil, err
		}
		id, a := assistantTestString(row["id"])
		location, b := assistantTestString(citation["location"])
		quote, c := assistantTestString(citation["excerpt"])
		url, d := assistantTestString(locator["value"])
		if a != nil || b != nil || c != nil || d != nil || seen[id] || !assistantCreationPage.MatchString(location) || strings.TrimSpace(quote) == "" {
			return nil, errAssistantDocument
		}
		seen[id] = true
		citations = append(citations, assistantCreationCitation{SourceID: id, Location: location, URL: url, Quote: quote})
	}
	return citations, nil
}

// Current rooted document verification, shared with read_link and attachments.
// A retained source's own verification flag is never read as authority.
func (e *assistantRunExecution) verifyAssistantCreationSources(ctx context.Context, input *assistantCreationInput) ([]assistantCreationCitation, []assistantCreationSourceProof, error) {
	citations, err := assistantCreationDeclaredCitations(input.Candidate)
	if err != nil {
		return nil, nil, err
	}
	files := assistantSavedLinkAttachments(e.source.Original)
	held := map[string]assistantVerifiedDocument{}
	proofs := []assistantCreationSourceProof{}
	if e.input != nil {
		e.input.mu.Lock()
		defer e.input.mu.Unlock()
	}
	if e.input != nil && e.input.creationSources != nil && e.input.creationSources.candidate == input.Candidate {
		cache := e.input.creationSources
		citations, proofs, held = cache.citations, cache.proofs, cache.documents
	} else {
		for i := range citations {
			citation := &citations[i]
			match := assistantCreationPage.FindStringSubmatch(citation.Location)
			pageNumber, _ := strconv.Atoi(match[3])
			var reference *assistantDocumentReference
			for _, file := range files {
				if file.Document != nil && file.Document.ID == match[1] && file.Document.Digest == match[2] {
					selected := false
					for _, page := range file.Document.Pages {
						if page == pageNumber {
							selected = true
						}
					}
					if !selected {
						continue
					}
					reference = file.Document
					break
				}
			}
			if reference == nil {
				return nil, nil, errAssistantDocument
			}
			key := assistantChatReferenceKey(*reference)
			document, ok := held[key]
			if !ok {
				document, err = e.service.server.readAssistantDocument(e.request.WithContext(ctx), e.source, *reference)
				if err != nil {
					return nil, nil, err
				}
				held[key] = document
			}
			found := false
			for _, page := range document.Pages {
				if page.Citation == citation.Location {
					_, found = researchSourceQuote(page.Text, citation.Quote)
				}
			}
			read := assistantLinkRecordSource(document.record)
			if !found || read["kind"] == "web" && read["url"] != citation.URL && read["requestedUrl"] != citation.URL {
				return nil, nil, errAssistantDocument
			}
			citation.Traced = true
			proofs = append(proofs, assistantCreationSourceProof{Location: citation.Location, AttachmentDigest: document.attachmentDigest, GatewayDigest: digestOf(assistantChatJSON(assistantSearchGatewaySnapshot(document.gateway)))})
		}
	}
	if e.input != nil {
		e.input.creationSources = &assistantCreationSourceCache{input.Candidate, citations, proofs, held}
	}
	input.Scope.EvidenceDigest = digestOf(assistantChatJSON(proofs))
	input.Fresh = func(ctx context.Context) error {
		if err := e.Authorize(ctx); err != nil {
			return err
		}
		for _, document := range held {
			if err := e.service.server.recheckAssistantDocument(e.request.WithContext(ctx), document); err != nil {
				return err
			}
		}
		return nil
	}
	input.FreshLocked = func(ctx context.Context) error {
		for _, document := range held {
			raw, gateway, err := e.service.server.assistantDocumentSnapshotLocked(e.request.WithContext(ctx), document.owner, document.reference)
			if err != nil || digestOf(raw) != document.attachmentDigest || digestOf(assistantChatJSON(assistantSearchGatewaySnapshot(gateway))) != digestOf(assistantChatJSON(assistantSearchGatewaySnapshot(document.gateway))) {
				return errAssistantDocument
			}
		}
		return nil
	}
	return citations, proofs, nil
}

func assistantCreationProofsMatch(input *assistantCreationInput, citations []assistantCreationCitation, proofs []assistantCreationSourceProof, digest string) bool {
	declared, err := assistantCreationDeclaredCitations(input.Candidate)
	if err != nil || len(citations) != len(declared) || len(proofs) != len(declared) || digestOf(assistantChatJSON(proofs)) != digest {
		return false
	}
	for i := range declared {
		declared[i].Traced = true
		if string(assistantChatJSON(declared[i])) != string(assistantChatJSON(citations[i])) || proofs[i].Location != declared[i].Location || !assistantRunDigest(proofs[i].AttachmentDigest) || !assistantRunDigest(proofs[i].GatewayDigest) {
			return false
		}
	}
	return true
}
