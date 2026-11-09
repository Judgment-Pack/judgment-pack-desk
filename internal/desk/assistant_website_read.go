package desk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
)

// Website custody is a distinct ledger collection. It shares the finite effect
// and receipt dialect with links; it is never a saved document reference.
type assistantWebsiteCustody assistantLinkCustody
type assistantVerifiedWebsite struct {
	Reference assistantWebsiteReference
	Discovery assistantWebsiteDiscovery
}

func assistantSavedWebsites(raw []byte) []assistantWebsiteReference {
	var chat struct {
		Websites []assistantWebsiteReference `json:"websites"`
	}
	if json.Unmarshal(raw, &chat) != nil || len(chat.Websites) > 16 {
		return nil
	}
	return chat.Websites
}
func assistantWebsiteOriginal(raw []byte) ([]byte, error) {
	if len(raw) > assistantWebsiteLimit {
		return nil, errAssistantDocument
	}
	return json.Marshal(attachmentObject{Version: 1, Original: attachmentOriginal{Name: "website-discovery.json", MediaType: "application/json", Bytes: base64.StdEncoding.EncodeToString(raw), SHA256: "sha256:" + digestOf(raw)}})
}
func verifyAssistantSavedWebsite(ctx context.Context, program string, raw []byte, ref assistantWebsiteReference, pin inputResearchPin) (assistantVerifiedWebsite, error) {
	zero := assistantVerifiedWebsite{}
	if !validAssistantWebsiteReference(ref) || validateAttachmentObject(raw) != nil || !assistantDocumentPacketFields(raw) {
		return zero, errAssistantDocument
	}
	var object attachmentObject
	if json.Unmarshal(raw, &object) != nil || object.Proof == nil {
		return zero, errAssistantDocument
	}
	p := object.Proof
	if p.Source != assistantWebsiteSource || p.Authority != pin.Authority || p.PublicKey != pin.Public || p.Web != nil || p.Drive != nil || p.Gmail != nil || p.Connected != nil || p.Resource != nil || object.Original.MediaType != "application/json" || len(object.Original.Bytes) > 1400000 || len(p.Response) > assistantLinkResponseLimit || len(p.Registry) > assistantSearchSealLimit+1 {
		return zero, errAssistantDocument
	}
	result, digest, err := verifyResearchProof(ctx, program, inputResearchProof{Session: p.Session, Source: assistantWebsiteSource, Arguments: assistantChatJSON(assistantLinkRequest{URL: ref.Seed}), Pin: pin, Response: []byte(p.Response), Seal: []byte(p.Registry)})
	if err != nil || digest != ref.Digest {
		return zero, errAssistantDocument
	}
	original, err := base64.StdEncoding.Strict().DecodeString(object.Original.Bytes)
	if err != nil || len(original) > assistantWebsiteLimit || base64.StdEncoding.EncodeToString(original) != object.Original.Bytes || !bytes.Equal(original, result) || object.Original.SHA256 != digest {
		return zero, errAssistantDocument
	}
	d, err := readAssistantWebsiteDiscovery(result, ref.Seed)
	if err != nil {
		return zero, err
	}
	return assistantVerifiedWebsite{ref, d}, nil
}
func verifyAssistantWebsiteObject(ctx context.Context, program string, raw []byte, held assistantLinkCustody) (assistantVerifiedWebsite, error) {
	zero := assistantVerifiedWebsite{}
	if !held.Discovery || held.Request.Site != "" || held.Stage.Receipt == nil || held.Stage.Receipt.ID != held.StageID || held.SealStage.Receipt == nil || held.SealStage.Receipt.ID != held.SealID || held.Original.Receipt == nil || held.Original.Receipt.ID != held.FinalID || held.Final.Intent == nil || held.Final.Intent.ID != held.FinalID || digestOf(raw) != held.Final.Intent.SHA256 {
		return zero, errAssistantDocument
	}
	var object attachmentObject
	if json.Unmarshal(raw, &object) != nil || object.Proof == nil || object.Proof.Session != held.Session || object.Original.Name != "website-discovery.json" {
		return zero, errAssistantDocument
	}
	found, err := verifyAssistantSavedWebsite(ctx, program, raw, assistantWebsiteReference{held.FinalID, held.Digest, held.Request.URL}, inputResearchPin{held.Gateway.Authority, held.Gateway.Public})
	if err != nil {
		return zero, err
	}
	p := object.Proof
	stage, err := assistantLinkPacket("chat-website-acquisition.json", []byte(p.Response))
	if err != nil || digestOf(stage) != held.Stage.Receipt.SHA256 {
		return zero, errAssistantDocument
	}
	seal, err := assistantLinkPacket("chat-website-seal.json", []byte(strings.TrimSuffix(p.Registry, "\n")))
	if err != nil || digestOf(seal) != held.SealStage.Receipt.SHA256 {
		return zero, errAssistantDocument
	}
	// Canonical original retains signed unknown fields too; never reserialize it.
	decoded, decodeErr := base64.StdEncoding.Strict().DecodeString(object.Original.Bytes)
	original, err := assistantWebsiteOriginal(decoded)
	if err != nil || decodeErr != nil || digestOf(original) != held.Original.Receipt.SHA256 {
		return zero, errAssistantDocument
	}
	return found, nil
}
func (e *assistantRunExecution) assistantWebsiteAuthority(ctx context.Context, url string, c *assistantLinkConfiguration) (bool, error) {
	candidate := *c
	if err := e.assistantWebsiteChoice(&candidate); err != nil {
		return false, nil
	}
	if err := e.service.server.assistantWebsiteCapability(ctx, &candidate, false); err != nil {
		return false, nil
	}
	program, err := e.service.server.researchVerifier()
	if err != nil {
		return false, err
	}
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return false, err
	}
	type owned struct {
		ref  assistantWebsiteReference
		held *assistantLinkCustody
	}
	refs := []owned{}
	for _, ref := range assistantSavedWebsites(e.source.Original) {
		refs = append(refs, owned{ref, nil})
	}
	for _, v := range run.Websites {
		held := assistantLinkCustody(v)
		if held.Final.Receipt != nil && !held.DispatchClosed {
			refs = append(refs, owned{assistantWebsiteReference{held.FinalID, held.Digest, held.Request.URL}, &held})
		}
	}
	latest := map[string]bool{}
	for i := len(refs) - 1; i >= 0; i-- {
		entry := refs[i]
		ref := entry.ref
		if latest[ref.Seed] {
			continue
		}
		latest[ref.Seed] = true
		if !validAssistantWebsiteReference(ref) || !assistantSameOrigin(url, ref.Seed) {
			continue
		}
		// A map never nominates another map: only supplied or current verified search
		// authority may nominate this seed. Keep that check separate from discovery.
		seedConfig := candidate
		allowed, err := e.assistantLinkAllowed(ctx, ref.Seed, &seedConfig)
		if err != nil {
			return false, err
		}
		if !allowed {
			continue
		}
		raw, err := e.assistantLinkPacket(ctx, ref.ID)
		if err != nil {
			continue
		}
		var found assistantVerifiedWebsite
		if entry.held != nil {
			if _, err = e.service.server.currentAssistantLinkConfiguration(ctx, e.source, *entry.held); err != nil {
				continue
			}
			found, err = verifyAssistantWebsiteObject(ctx, program, raw, *entry.held)
		} else {
			found, err = verifyAssistantSavedWebsite(ctx, program, raw, ref, inputResearchPin{candidate.Gateway.authority, candidate.Gateway.signerPublic})
		}
		if err != nil {
			continue
		}
		for _, page := range found.Discovery.Pages {
			if page.URL == url && page.Status == "discovered" {
				candidate.SeedAuthorityPacket = seedConfig.AuthorityPacket
				candidate.Site = ref.Seed
				candidate.AuthorityPacket = &attachmentReceipt{ID: ref.ID, SHA256: digestOf(raw)}
				// Seed search bytes are reverified before each wait; the website packet is
				// the exact physical authority retained for this discovered page acquisition.
				candidate.SearchConfigurationDigest = seedConfig.SearchConfigurationDigest
				*c = candidate
				if err = e.recheckAssistantLink(ctx, candidate); err != nil {
					return false, err
				}
				return true, nil
			}
		}
	}
	return false, nil
}

// Revalidate the exact owned map on retained reads/recovery too; a signed site
// hint or a rewritten custody receipt cannot stand in for discovered status.
func verifyAssistantWebsitePageAuthority(ctx context.Context, program string, raw []byte, run assistantRunRecord, source assistantRunSource, held assistantLinkCustody) error {
	if held.Request.Site == "" {
		return nil
	}
	if held.AuthorityPacket == nil || digestOf(raw) != held.AuthorityPacket.SHA256 {
		return errAssistantDocument
	}
	var found assistantVerifiedWebsite
	owned := false
	for _, v := range run.Websites {
		if v.FinalID == held.AuthorityPacket.ID && v.Request.URL == held.Request.Site && v.Final.Receipt != nil && !v.DispatchClosed {
			var err error
			found, err = verifyAssistantWebsiteObject(ctx, program, raw, assistantLinkCustody(v))
			if err != nil {
				return err
			}
			owned = true
			break
		}
	}
	if !owned {
		for _, ref := range assistantSavedWebsites(source.Original) {
			if ref.ID == held.AuthorityPacket.ID && ref.Seed == held.Request.Site {
				var err error
				found, err = verifyAssistantSavedWebsite(ctx, program, raw, ref, inputResearchPin{held.Gateway.Authority, held.Gateway.Public})
				if err != nil {
					return err
				}
				owned = true
				break
			}
		}
	}
	if !owned {
		return errAssistantDocument
	}
	for _, page := range found.Discovery.Pages {
		if page.URL == held.Request.URL && page.Status == "discovered" {
			return nil
		}
	}
	return errAssistantDocument
}
