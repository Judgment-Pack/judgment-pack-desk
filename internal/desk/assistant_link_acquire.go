package desk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
)

// Web source responses and proof.response are already bounded at 16 MiB by
// the shared attachment schema/outbound owner. The original is further bounded
// at min(configured maxFileBytes, 4 MiB). Packets stay outside the run ledger.
const assistantLinkResponseLimit = 16 << 20

func assistantLinkPacket(name string, raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > assistantLinkResponseLimit {
		return nil, errAssistantDocument
	}
	return json.Marshal(attachmentObject{Version: 1, Original: attachmentOriginal{Name: name, MediaType: "application/json", Bytes: base64.StdEncoding.EncodeToString(raw), SHA256: "sha256:" + digestOf(raw)}})
}

func (e *assistantRunExecution) acquireAssistantLink(ctx context.Context, args assistantLinkArguments, configuration assistantLinkConfiguration) (assistantHostObservation, error) {
	empty := assistantHostObservation{}
	s := e.service.server
	program, err := s.researchVerifier()
	if err != nil {
		return empty, err
	}
	host, err := e.assistantLinkHost(ctx, args)
	if err != nil {
		return empty, err
	}
	request := assistantLinkRequest{URL: args.Link.FetchURL}
	held, err := e.reserveAssistantLink(ctx, host, request, configuration)
	if err != nil {
		return assistantHostObservation{Result: assistantHostFailure(err)}, nil
	}
	fail := func(err error) (assistantHostObservation, error) {
		return assistantHostObservation{Result: assistantHostFailure(err)}, nil
	}
	unknown := func(cause error) (assistantHostObservation, error) {
		_ = e.changeAssistantLink(ctx, host, true, func(v *assistantLinkCustody) error { v.DispatchClosed = true; return nil })
		e.cancel()
		return fail(cause)
	}
	admission := func() error { return e.recheckAssistantLink(ctx, configuration) }
	if err = admission(); err != nil {
		return fail(err)
	}
	if err = e.changeAssistantLink(ctx, host, false, func(v *assistantLinkCustody) error {
		if v.Acquire != "reserved" {
			return errAssistantRunDispatch
		}
		v.Acquire = "dispatched"
		return nil
	}); err != nil {
		return fail(err)
	}

	response, acquireErr := s.researchSourceCallWithAdmission(ctx, inputResearchPin{configuration.Gateway.authority, configuration.Gateway.signerPublic}, "acquire", map[string]any{"session": held.Session, "source": "web", "arguments": request}, admission)
	// Complete raw response bytes must be retained even if a post-response pin
	// check or cancellation closes dispatch. No later network call is admitted by
	// this bounded custody completion.
	if len(response) == 0 {
		var refusal *researchSourceError
		if errors.As(acquireErr, &refusal) && refusal.acquisitionRefusal {
			_ = e.changeAssistantLink(ctx, host, true, func(v *assistantLinkCustody) error { v.Acquire = "refused"; return nil })
			return fail(acquireErr)
		}
		return unknown(acquireErr)
	}
	if len(response) > assistantLinkResponseLimit {
		return fail(errAssistantDocument)
	}
	if err = e.changeAssistantLink(ctx, host, true, func(v *assistantLinkCustody) error {
		if v.Acquire != "dispatched" {
			return errAssistantRunDispatch
		}
		v.Acquire = "completed"
		return nil
	}); err != nil {
		return fail(err)
	}
	stage, err := assistantLinkPacket("chat-link-acquisition.json", response)
	if err != nil {
		return fail(err)
	}
	if _, err = e.retainAssistantLinkObject(ctx, held, "stage", stage, configuration, true); err != nil {
		return unknown(err)
	}
	if acquireErr != nil {
		return fail(acquireErr)
	}
	if err = admission(); err != nil {
		return fail(err)
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if !validUTF8(response) || !uniqueJSON(response) || json.Unmarshal(response, &envelope) != nil {
		return fail(errAssistantDocument)
	}
	result, err := assistantDocumentCanon(ctx, program, envelope.Result, assistantLinkResponseLimit)
	if err != nil {
		return fail(errAssistantDocument)
	}
	record, err := readAssistantDocumentRecord(result)
	if err != nil || assistantLinkRecordSource(record)["kind"] != "web" || assistantLinkRecordSource(record)["requestedUrl"] != request.URL || record.size > min(int(configuration.Gateway.maxFileBytes), 4<<20) {
		return fail(errAssistantDocument)
	}
	originalRecord := adObject(record.raw["original"])
	original, err := json.Marshal(attachmentObject{Version: 1, Original: attachmentOriginal{Name: record.name, MediaType: record.mediaType, Bytes: adString(originalRecord["bytes"]), SHA256: record.id}})
	if err != nil || validateAttachmentObject(original) != nil {
		return fail(errAssistantDocument)
	}
	held, err = e.currentAssistantLink(ctx, host)
	if err != nil {
		return fail(err)
	}
	if err = admission(); err != nil {
		return fail(err)
	}
	if _, err = e.retainAssistantLinkObject(ctx, held, "original", original, configuration, false); err != nil {
		return unknown(err)
	}
	if err = admission(); err != nil {
		return fail(err)
	}
	if err = e.changeAssistantLink(ctx, host, false, func(v *assistantLinkCustody) error {
		if v.Seal != "not-started" {
			return errAssistantRunDispatch
		}
		v.Seal = "dispatched"
		return nil
	}); err != nil {
		return fail(err)
	}
	seal, sealErr := s.researchSourceCallWithAdmission(ctx, inputResearchPin{configuration.Gateway.authority, configuration.Gateway.signerPublic}, "seal", map[string]any{"session": held.Session}, admission)
	if len(seal) == 0 {
		return unknown(sealErr)
	}
	if len(seal) > assistantSearchSealLimit {
		return fail(errAssistantDocument)
	}
	if err = e.changeAssistantLink(ctx, host, true, func(v *assistantLinkCustody) error {
		if v.Seal != "dispatched" {
			return errAssistantRunDispatch
		}
		v.Seal = "completed"
		return nil
	}); err != nil {
		return fail(err)
	}
	sealPacket, err := assistantLinkPacket("chat-link-seal.json", seal)
	if err != nil {
		return fail(err)
	}
	if _, err = e.retainAssistantLinkObject(ctx, held, "seal", sealPacket, configuration, true); err != nil {
		return unknown(err)
	}
	if sealErr != nil {
		return fail(sealErr)
	}
	if err = admission(); err != nil {
		return fail(err)
	}
	canonicalArgs := assistantChatJSON(request)
	verified, digest, err := verifyResearchProof(ctx, program, inputResearchProof{Session: held.Session, Source: "web", Arguments: canonicalArgs, Pin: inputResearchPin{configuration.Gateway.authority, configuration.Gateway.signerPublic}, Response: response, Seal: seal})
	if err != nil || !bytes.Equal(verified, result) || digest != "sha256:"+digestOf(result) {
		return fail(errAssistantDocument)
	}
	var object attachmentObject
	if json.Unmarshal(original, &object) != nil {
		return fail(errAssistantDocument)
	}
	object.Proof = &attachmentProof{Session: held.Session, Source: "web", Authority: configuration.Gateway.authority, PublicKey: configuration.Gateway.signerPublic, Response: string(response), Registry: string(seal) + "\n", Web: &struct {
		URL  string `json:"url"`
		Site string `json:"site,omitempty"`
	}{URL: request.URL}}
	final, err := json.Marshal(object)
	if err != nil {
		return fail(err)
	}
	if err = admission(); err != nil {
		return fail(err)
	}
	if err = e.changeAssistantLink(ctx, host, false, func(v *assistantLinkCustody) error { v.Digest = digest; return nil }); err != nil {
		return fail(err)
	}
	held, err = e.currentAssistantLink(ctx, host)
	if err != nil {
		return fail(err)
	}
	receipt, err := e.retainAssistantLinkObject(ctx, held, "final", final, configuration, false)
	if err != nil {
		return unknown(err)
	}
	// Final links require the entire current authority after the storage wait.
	if err = admission(); err != nil {
		return fail(err)
	}
	held, err = e.currentAssistantLink(ctx, host)
	if err != nil {
		return fail(err)
	}
	doc, file, err := e.readAssistantAcquiredLink(ctx, held, configuration)
	if err != nil {
		return fail(err)
	}
	file.Link.Anchor = args.Link.Anchor
	tool, err := e.assistantLinkWindow(doc, args)
	if err != nil {
		return fail(err)
	}
	if tool.Err() {
		file.Document.NeedsReview = true
	}
	output := &assistantLinkOutput{Attachment: file, Stored: receipt, AcquisitionID: host}
	return assistantHostObservation{Result: tool, Output: &assistantDomainOutput{Kind: "link-document", Link: output}}, nil
}
