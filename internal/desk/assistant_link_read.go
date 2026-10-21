package desk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

type assistantLinkAttachment struct {
	ID       string                      `json:"id"`
	Name     string                      `json:"name"`
	Text     string                      `json:"text"`
	Document *assistantDocumentReference `json:"document,omitempty"`
	Link     *assistantLinkLocation      `json:"link,omitempty"`
}
type assistantLinkLocation struct {
	URL         string `json:"url"`
	ResolvedURL string `json:"resolvedUrl,omitempty"`
	Anchor      string `json:"anchor,omitempty"`
}

func assistantSavedLinkAttachments(raw []byte) []assistantLinkAttachment {
	var chat struct {
		Documents   []assistantLinkAttachment `json:"documents"`
		Attachments []assistantLinkAttachment `json:"attachments"`
		Checkpoint  struct {
			State struct {
				Turns []struct {
					Attachments []assistantLinkAttachment `json:"attachments"`
				} `json:"turns"`
				Responses []struct {
					Documents []assistantLinkAttachment `json:"documents"`
				} `json:"responses"`
			} `json:"state"`
		} `json:"checkpoint"`
	}
	if json.Unmarshal(raw, &chat) != nil {
		return nil
	}
	out := append(chat.Documents, chat.Attachments...)
	for _, t := range chat.Checkpoint.State.Turns {
		out = append(out, t.Attachments...)
	}
	for _, r := range chat.Checkpoint.State.Responses {
		out = append(out, r.Documents...)
	}
	return out
}
func assistantLinkGiven(raw []byte, url string) bool {
	for _, file := range assistantSavedLinkAttachments(raw) {
		if file.Document != nil && file.Link != nil && file.Link.URL == url {
			return true
		}
	}
	var chat struct {
		Checkpoint struct {
			State struct {
				Turns []struct {
					Role string `json:"role"`
					Text string `json:"text"`
				} `json:"turns"`
			} `json:"state"`
		} `json:"checkpoint"`
	}
	if json.Unmarshal(raw, &chat) != nil {
		return false
	}
	for _, turn := range chat.Checkpoint.State.Turns {
		if turn.Role != "user" {
			continue
		}
		for _, found := range extractAssistantLinks(turn.Text) {
			link, err := normalizeAssistantLink(found)
			if err == nil && link.FetchURL == url {
				return true
			}
		}
	}
	return false
}

// This private byte reader grants nothing. Each caller first supplies a typed
// saved reference or an exact current-run custody receipt, never a model ID.
func (e *assistantRunExecution) assistantLinkPacket(ctx context.Context, id string) ([]byte, error) {
	if !attachmentID.MatchString(id) {
		return nil, errAssistantDocument
	}
	s := e.service.server
	lock, err := s.privateDataLockContext(ctx, true)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err = e.authorizeAssistantResearchLocked(ctx); err != nil {
		return nil, err
	}
	store, err := s.openChatData()
	if err != nil {
		return nil, err
	}
	defer store.root.Close()
	if err = e.writer.lease.store.checkScope(store.root); err != nil {
		return nil, err
	}
	name, err := resolveConversationName(store.root, s.conversationIdentity())
	if err != nil {
		return nil, err
	}
	return readPrivateData(store.root, attachmentProjectPrefix(name)+id+".json", maxAttachmentObjectBytes)
}
func (e *assistantRunExecution) assistantLinkAllowed(ctx context.Context, url string, c *assistantLinkConfiguration) (bool, error) {

	if assistantLinkGiven(e.source.Original, url) {
		return true, nil
	}
	_, mode, preference, err := e.service.server.assistantSearchPreference(e.source)
	if err != nil {
		return false, err
	}
	c.SearchPreferenceDigest = preference
	choice, err := assistantSearchChoice(e.source, mode)
	if err != nil || choice != "research" {
		return false, err
	}
	// Redirect strings become authority only alongside the verified requested URL.
	for _, file := range assistantSavedLinkAttachments(e.source.Original) {
		if file.Document == nil || file.Link == nil || file.Link.ResolvedURL != url {
			continue
		}
		held, err := e.service.server.readAssistantDocument(e.request.WithContext(ctx), e.source, *file.Document)
		if err != nil {
			continue
		}
		source := assistantLinkRecordSource(held.record)
		if source["kind"] == "web" && source["requestedUrl"] == file.Link.URL && source["url"] == url {
			c.AuthorityPacket = &attachmentReceipt{ID: held.reference.ID, SHA256: held.attachmentDigest}
			if err = e.recheckAssistantLink(ctx, *c); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	config, err := e.service.server.assistantSearchConfigurationCurrent(ctx, e.source, false)
	if err != nil {
		return false, err
	}
	c.SearchConfigurationDigest = assistantSearchConfigurationDigest(config)
	program, err := e.service.server.researchVerifier()
	if err != nil {
		return false, err
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return false, err
	}
	for _, held := range run.Searches {
		if held.Final.Receipt == nil || held.DispatchClosed || held.ConfigurationDigest != assistantSearchConfigurationDigest(config) || held.Gateway != c.Snapshot {
			continue
		}
		raw, err := e.assistantLinkPacket(ctx, held.FinalID)
		if err != nil {
			return false, err
		}
		verified, err := verifyAssistantSearchObject(ctx, program, raw, held)
		if err != nil {
			return false, err
		}
		for _, hit := range verified.Result.Hits {
			if normalized, err := normalizeAssistantLink(hit.URL); err == nil && normalized.FetchURL == url {
				c.AuthorityPacket = &attachmentReceipt{ID: held.FinalID, SHA256: digestOf(raw)}
				if err = e.recheckAssistantSearch(ctx, config); err != nil {
					return false, err
				}
				return true, nil
			}
		}
	}
	var chat struct {
		Searches []assistantSearchReference `json:"searches"`
	}
	if json.Unmarshal(e.source.Original, &chat) != nil {
		return false, errAssistantSearch
	}
	for _, ref := range chat.Searches {
		if !validAssistantSearchReference(ref) || ref.Request.Connection != config.Connection || ref.Request.Revision != config.Revision {
			return false, errAssistantSearch
		}
		raw, err := e.assistantLinkPacket(ctx, ref.ID)
		if err != nil {
			return false, err
		}
		result, err := verifyAssistantSavedSearch(ctx, program, raw, ref, c.Gateway)
		if err != nil {
			return false, err
		}
		for _, hit := range result.Hits {
			if normalized, err := normalizeAssistantLink(hit.URL); err == nil && normalized.FetchURL == url {
				c.AuthorityPacket = &attachmentReceipt{ID: ref.ID, SHA256: digestOf(raw)}
				again, err := e.assistantLinkPacket(ctx, ref.ID)
				if err != nil || !bytes.Equal(raw, again) {
					return false, errAssistantSearch
				}
				if err = e.recheckAssistantSearch(ctx, config); err != nil {
					return false, err
				}
				return true, nil
			}
		}
	}
	return false, nil
}
func verifyAssistantSavedSearch(ctx context.Context, program string, raw []byte, ref assistantSearchReference, pin researchGateway) (assistantSearchResult, error) {
	var zero assistantSearchResult
	var object attachmentObject
	if !validAssistantSearchReference(ref) || validateAttachmentObject(raw) != nil || !assistantDocumentPacketFields(raw) || json.Unmarshal(raw, &object) != nil || object.Proof == nil {
		return zero, errAssistantSearch
	}
	p := object.Proof
	if p.Source != "web-search" || p.PublicKey != pin.signerPublic || p.Authority != pin.authority || p.Web != nil || p.Drive != nil || p.Gmail != nil || p.Connected != nil || p.Resource != nil || object.Original.MediaType != "application/json" {
		return zero, errAssistantSearch
	}
	result, digest, err := verifyResearchProof(ctx, program, inputResearchProof{Session: p.Session, Source: p.Source, Arguments: assistantChatJSON(ref.Request), Pin: inputResearchPin{p.Authority, p.PublicKey}, Response: []byte(p.Response), Seal: []byte(p.Registry)})
	original, decodeErr := base64.StdEncoding.Strict().DecodeString(object.Original.Bytes)
	if err != nil || decodeErr != nil || digest != ref.Digest || object.Original.SHA256 != digest || !bytes.Equal(result, original) {
		return zero, errAssistantSearch
	}
	return readAssistantSearchResult(result, ref.Request)
}
func assistantLinkRecordSource(record assistantDocumentRecord) map[string]any {
	return adObject(adObject(record.raw["provenance"])["source"])
}
func assistantLinkReference(id, digest string, record assistantDocumentRecord) assistantDocumentReference {
	ref := assistantDocumentReference{ID: id, Digest: digest, Pages: []int{}, NeedsReview: record.partial}
	for i, page := range record.pages {
		if page.usable {
			ref.Pages = append(ref.Pages, i+1)
		}
	}
	return ref
}
func assistantLinkFile(ref assistantDocumentReference, record assistantDocumentRecord, link assistantNormalizedLink) assistantLinkAttachment {
	location := &assistantLinkLocation{URL: link.FetchURL, Anchor: link.Anchor}
	source := assistantLinkRecordSource(record)
	if resolved := adString(source["url"]); resolved != link.FetchURL {
		location.ResolvedURL = resolved
	}
	return assistantLinkAttachment{ID: ref.ID, Name: record.name, Document: &ref, Link: location}
}
func (e *assistantRunExecution) readAssistantAcquiredLink(ctx context.Context, held assistantLinkCustody, c assistantLinkConfiguration) (assistantVerifiedDocument, assistantLinkAttachment, error) {
	var zero assistantVerifiedDocument
	if held.Final.Receipt == nil || held.DispatchClosed || held.Gateway != c.Snapshot || held.ConfigurationDigest != assistantLinkConfigurationDigest(c) {
		return zero, assistantLinkAttachment{}, errAssistantDocument
	}
	raw, err := e.assistantLinkPacket(ctx, held.FinalID)
	if err != nil {
		return zero, assistantLinkAttachment{}, err
	}
	program, err := e.service.server.researchVerifier()
	if err != nil {
		return zero, assistantLinkAttachment{}, err
	}
	verified, file, err := verifyAssistantLinkObject(ctx, program, raw, held)
	if err != nil {
		return zero, file, err
	}
	if held.Request.Site != "" {
		authority, err := e.assistantLinkPacket(ctx, held.AuthorityPacket.ID)
		if err != nil {
			return zero, file, err
		}
		run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
		if err != nil {
			return zero, file, err
		}
		if err = verifyAssistantWebsitePageAuthority(ctx, program, authority, run, e.source, held); err != nil {
			return zero, file, err
		}
	}
	again, err := e.assistantLinkPacket(ctx, held.FinalID)
	if err != nil || !bytes.Equal(again, raw) {
		return zero, file, errAssistantDocument
	}
	if err = e.recheckAssistantLink(ctx, c); err != nil {
		return zero, file, err
	}
	return verified, file, nil
}
func verifyAssistantLinkObject(ctx context.Context, program string, raw []byte, held assistantLinkCustody) (assistantVerifiedDocument, assistantLinkAttachment, error) {
	var zero assistantVerifiedDocument
	var file assistantLinkAttachment
	if held.Final.Intent == nil || held.Final.Intent.ID != held.FinalID || digestOf(raw) != held.Final.Intent.SHA256 || held.Stage.Receipt == nil || held.SealStage.Receipt == nil || held.Original.Receipt == nil || held.Original.Receipt.ID != held.FinalID || held.Stage.Receipt.ID != held.StageID || held.SealStage.Receipt.ID != held.SealID {
		return zero, file, errAssistantDocument
	}
	var object attachmentObject
	if json.Unmarshal(raw, &object) != nil || object.Proof == nil {
		return zero, file, errAssistantDocument
	}
	p := object.Proof
	if p.Source != "web" || p.Session != held.Session || p.Web == nil || p.Web.URL != held.Request.URL || p.Web.Site != held.Request.Site || p.Authority != held.Gateway.Authority || p.PublicKey != held.Gateway.Public {
		return zero, file, errAssistantDocument
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal([]byte(p.Response), &envelope) != nil {
		return zero, file, errAssistantDocument
	}
	record, err := readAssistantDocumentRecord(envelope.Result)
	if err != nil {
		return zero, file, err
	}
	ref := assistantLinkReference(held.FinalID, held.Digest, record)
	verified, err := verifyAssistantDocumentPacket(ctx, program, raw, inputResearchPin{held.Gateway.Authority, held.Gateway.Public}, ref, true)
	if err != nil {
		return zero, file, err
	}
	stage, err := assistantLinkPacket("chat-link-acquisition.json", []byte(p.Response))
	if err != nil || digestOf(stage) != held.Stage.Receipt.SHA256 {
		return zero, file, errAssistantDocument
	}
	seal, err := assistantLinkPacket("chat-link-seal.json", bytes.TrimSuffix([]byte(p.Registry), []byte("\n")))
	if err != nil || digestOf(seal) != held.SealStage.Receipt.SHA256 {
		return zero, file, errAssistantDocument
	}
	original, _ := json.Marshal(attachmentObject{Version: 1, Original: object.Original})
	if digestOf(original) != held.Original.Receipt.SHA256 {
		return zero, file, errAssistantDocument
	}
	link, err := normalizeAssistantLink(held.Request.URL)
	if err != nil {
		return zero, file, err
	}
	return verified, assistantLinkFile(ref, record, link), nil
}
func (e *assistantRunExecution) executeAssistantLink(ctx context.Context, args assistantLinkArguments) (assistantHostObservation, error) {
	fail := func(err error) (assistantHostObservation, error) {
		return assistantHostObservation{Result: assistantHostFailure(err)}, nil
	}
	c, err := e.service.server.assistantLinkConfiguration(true)
	if err != nil {
		return fail(err)
	}
	if err = e.recheckAssistantLink(ctx, c); err != nil {
		return fail(err)
	}
	allowed, err := e.assistantWebsiteAuthority(ctx, args.Link.FetchURL, &c)
	if err == nil && !allowed {
		allowed, err = e.assistantLinkAllowed(ctx, args.Link.FetchURL, &c)
	}
	if err != nil {
		return fail(err)
	}
	if !allowed {
		return fail(errorsNewAssistantLinkAuthority())
	}
	for _, file := range reverseAssistantLinkFiles(assistantSavedLinkAttachments(e.source.Original)) {
		if file.Document == nil || file.Link == nil || file.Link.URL != args.Link.FetchURL {
			continue
		}
		held, err := e.service.server.readAssistantDocument(e.request.WithContext(ctx), e.source, *file.Document)
		if err != nil {
			return fail(err)
		}
		if err = e.recheckAssistantLink(ctx, c); err != nil {
			return fail(err)
		}
		result, err := e.assistantLinkWindow(held, args)
		if err != nil {
			return fail(err)
		}
		return assistantHostObservation{Result: result}, nil
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return fail(err)
	}
	for _, held := range run.Links {
		if held.Request.URL == args.Link.FetchURL && held.Final.Receipt != nil {
			retainedConfig, err := e.service.server.currentAssistantLinkConfiguration(ctx, e.source, held)
			if err != nil {
				return fail(err)
			}
			doc, _, err := e.readAssistantAcquiredLink(ctx, held, retainedConfig)
			if err != nil {
				return fail(err)
			}
			result, err := e.assistantLinkWindow(doc, args)
			if err != nil {
				return fail(err)
			}
			return assistantHostObservation{Result: result}, nil
		}
	}
	return e.acquireAssistantLink(ctx, args, c)
}
func errorsNewAssistantLinkAuthority() error {
	return failure.ToolInput("That link was not supplied or authorized by a currently verified search or website map. Use a supplied URL, a current verified search result, or the exact discovered URL with the person's saved research choice.")
}
func reverseAssistantLinkFiles(files []assistantLinkAttachment) []assistantLinkAttachment {
	for i, j := 0, len(files)-1; i < j; i, j = i+1, j-1 {
		files[i], files[j] = files[j], files[i]
	}
	return files
}
