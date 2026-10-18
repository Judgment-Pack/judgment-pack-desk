package desk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) policySourceGateway(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if s.policySourceCall != nil {
		return s.policySourceCall(ctx, method, params)
	}
	_, raw, err := s.readDeskFile()
	if err != nil {
		return nil, policySourceFailure("source-unavailable")
	}
	status := s.localGatewayStatus(raw)
	if s.localGateway == nil || status == nil || status.Status != "ready" {
		return nil, policySourceFailure("source-unsupported")
	}
	catalogRaw, err := readConnectionCatalog(ctx, s.localGateway.bundle)
	var catalog connectionCatalog
	if err != nil || json.Unmarshal(catalogRaw, &catalog) != nil {
		return nil, policySourceFailure("source-unavailable")
	}
	allowed := false
	for _, d := range catalog.Providers {
		if d.ID == "google-drive" {
			allowed = contains(d.Operations, method)
		}
	}
	if !allowed {
		return nil, policySourceFailure("source-unsupported")
	}
	if method == "capabilities" {
		return nil, nil
	}
	body, _ := json.Marshal(params)
	companion := s.connectionCompanion("google-drive", true)
	if companion == nil {
		return nil, policySourceFailure("source-unavailable")
	}
	result, err := companion.call(ctx, s.localGateway.bundle, platformPath(s.assistant.dir, "gateway-connections"), method, body, "google-drive", false)
	if err != nil {
		return nil, policySourceFailure("source-unavailable")
	}
	var failure struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(result, &failure)
	if failure.Error != "" {
		return nil, policySourceFailure(policySafeSourceError(failure.Error))
	}
	return result, nil
}
func (s *Server) policySourcesSupported(ctx context.Context) bool {
	if s.policySourceCall != nil {
		return true
	}
	_, raw, err := s.readDeskFile()
	if err != nil {
		return false
	}
	status := s.localGatewayStatus(raw)
	if s.localGateway == nil || status == nil || status.Status != "ready" {
		return false
	}
	raw, err = readConnectionCatalog(ctx, s.localGateway.bundle)
	var c connectionCatalog
	if err != nil || json.Unmarshal(raw, &c) != nil {
		return false
	}
	for _, d := range c.Providers {
		if d.ID == "google-drive" {
			for _, method := range []string{"source-bind", "source-bindings", "source-revoke", "source-observe"} {
				if !contains(d.Operations, method) {
					return false
				}
			}
			return true
		}
	}
	return false
}
func policySafeSourceError(code string) string {
	if contains([]string{"source-binding-unavailable", "source-binding-idempotency-conflict", "source-binding-limit", "source-unsupported", "source-unavailable", "source-invalid", "storage-capacity", "connect-required", "reconnect-required", "blocked-by-policy", "permission-required", "not-downloadable", "file-too-large", "unsupported-file", "source-changed", "rate-limited", "retrieval-failed", "processing-failed", "response-too-large"}, code) {
		return code
	}
	return "source-unavailable"
}
func policySourceErrorCode(err error) string {
	if err == nil {
		return ""
	}
	return policySafeSourceError(strings.TrimPrefix(err.Error(), "policy source: "))
}
func (s *Server) canonicalPolicySource(ctx context.Context, raw []byte) ([]byte, error) {
	if s.policySourceCanon != nil {
		return s.policySourceCanon(ctx, raw)
	}
	program, err := s.researchVerifier()
	if err != nil {
		return nil, err
	}
	return researchCanon(ctx, program, raw)
}

type policyPreparedObservation struct {
	Metadata           policySourceObservation
	Original, Document []byte
}

func (s *Server) verifyPolicyObservation(ctx context.Context, raw []byte, binding policySourceBinding) (policyPreparedObservation, error) {
	bad := policySourceFailure("source-invalid")
	var o policyGatewayObservation
	if len(raw) > 16<<20 || rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &o) != nil || o.Version != 1 || o.BindingID != binding.BindingID || o.Source != binding.Source || !retainedArtifactDigest.MatchString(o.ObservationID) || o.SourceRevision == "" || len(o.SourceRevision) > 4096 || !validPolicyTime(o.ObservedAt) || o.Availability != "present" || o.Coverage != "snapshot-only" || o.Lifecycle.Kind != "not-exposed" || o.Content.ByteLength < 1 || o.Content.ByteLength > 4<<20 || o.Extraction.Content.ByteLength < 1 || o.Extraction.Content.ByteLength > 16<<20 || o.Extraction.Content.MediaType != "application/vnd.jpack.attachment+json" || o.Extraction.Profile != "attachment-record-v1/canon" || !validPolicyPrefixedDigest(o.Extraction.ConfigurationDigest) {
		return policyPreparedObservation{}, bad
	}
	document, err := s.canonicalPolicySource(ctx, o.Document)
	if err != nil || o.Extraction.Content.BytesDigest != "sha256:"+digestOf(document) || o.Extraction.Content.ByteLength != len(document) {
		return policyPreparedObservation{}, bad
	}
	var record struct {
		AttachmentVersion string `json:"attachmentVersion"`
		Document          struct {
			ID, Name, MediaType string
			Size                int
			Version             string
		} `json:"document"`
		Original struct{ Retention, Encoding, Bytes string } `json:"original"`
		Content  struct {
			Kind      string
			PageCount int
			Pages     []struct {
				Number                   int
				Status, Text, Extraction string
				Chars, Unmapped          int
			}
			Chars     int
			Truncated bool
		} `json:"content"`
		Processing struct {
			Status     string
			Errors     []json.RawMessage
			Bounds     map[string]int
			DurationMs int
		} `json:"processing"`
		Provenance struct {
			Adapter               json.RawMessage
			Source                struct{ Kind, FileID, Version string }
			ObservedAt, Processor string
			OCR                   json.RawMessage
		} `json:"provenance"`
	}
	if json.Unmarshal(document, &record) != nil || record.AttachmentVersion != "1" || record.Original.Retention != "inline" || record.Original.Encoding != "base64" || record.Document.ID != o.Content.BytesDigest || record.Document.Size != o.Content.ByteLength || record.Document.MediaType != o.Content.MediaType || record.Document.Version != o.SourceRevision || record.Provenance.Source.Kind != "google-drive" || record.Provenance.Source.FileID != o.Source.ResourceID || record.Provenance.Source.Version != o.SourceRevision || !validPolicyTime(record.Provenance.ObservedAt) || record.Provenance.Processor != o.Extraction.Processor || record.Document.Name == "" || len(record.Document.Name) > 255 || strings.ContainsAny(record.Document.Name, "\x00\r\n") || record.Content.Kind != "text" || !contains([]string{"complete", "partial", "failed"}, record.Processing.Status) || record.Content.Pages == nil || len(record.Content.Pages) > 500 || record.Content.PageCount < 0 || record.Content.PageCount > 500 || len(record.Content.Pages) > record.Content.PageCount {
		return policyPreparedObservation{}, bad
	}
	var adapter struct{ Name, Version, Digest string }
	if json.Unmarshal(record.Provenance.Adapter, &adapter) != nil || adapter.Version != o.Extraction.ProcessorVersion || !validPolicyPrefixedDigest(adapter.Digest) {
		return policyPreparedObservation{}, bad
	}
	original, err := base64.StdEncoding.Strict().DecodeString(record.Original.Bytes)
	if err != nil || base64.StdEncoding.EncodeToString(original) != record.Original.Bytes || len(original) != o.Content.ByteLength || "sha256:"+digestOf(original) != o.Content.BytesDigest {
		return policyPreparedObservation{}, bad
	}
	chars, textBytes := 0, 0
	for i, p := range record.Content.Pages {
		if p.Number != i+1 || !contains([]string{"ok", "no-text", "needs-ocr", "failed"}, p.Status) || !contains([]string{"text-layer", "ocr", "verbatim", "none"}, p.Extraction) || p.Chars != utf8.RuneCountInString(p.Text) || p.Unmapped < 0 || (p.Status != "ok" && p.Text != "") || (p.Status == "ok" && p.Text == "") {
			return policyPreparedObservation{}, bad
		}
		chars += p.Chars
		textBytes += len(p.Text)
	}
	if chars != record.Content.Chars || textBytes > 8<<20 || record.Processing.Bounds["maxBytes"] < len(original) || record.Processing.Bounds["maxPages"] < record.Content.PageCount || record.Processing.Bounds["maxTextBytes"] < textBytes {
		return policyPreparedObservation{}, bad
	}
	var ocr any
	if string(record.Provenance.OCR) != "null" {
		var applied struct{ Program, Digest string }
		if json.Unmarshal(record.Provenance.OCR, &applied) != nil || applied.Program == "" || !validPolicyPrefixedDigest(applied.Digest) {
			return policyPreparedObservation{}, bad
		}
		ocr = map[string]any{"program": applied.Program, "digest": applied.Digest}
	}
	config, _ := json.Marshal(map[string]any{"profile": "drive-document-processing-v1", "bounds": record.Processing.Bounds, "requestOCR": "auto", "adapter": json.RawMessage(record.Provenance.Adapter), "processor": record.Provenance.Processor, "ocr": ocr})
	config, err = s.canonicalPolicySource(ctx, config)
	if err != nil || "sha256:"+digestOf(config) != o.Extraction.ConfigurationDigest {
		return policyPreparedObservation{}, bad
	}
	// The exact extraction hash includes observation timing. The separate
	// semantic projection only omits acquisition timing/duration and original
	// base64 (whose independent digest remains included); text/status/config,
	// metadata and processor changes therefore still create an update.
	var semantic map[string]json.RawMessage
	_ = json.Unmarshal(document, &semantic)
	delete(semantic, "original")
	var provenance map[string]json.RawMessage
	_ = json.Unmarshal(semantic["provenance"], &provenance)
	delete(provenance, "observedAt")
	semantic["provenance"], _ = json.Marshal(provenance)
	var processing map[string]json.RawMessage
	_ = json.Unmarshal(semantic["processing"], &processing)
	delete(processing, "durationMs")
	semantic["processing"], _ = json.Marshal(processing)
	projection, _ := json.Marshal(struct {
		Binding                string
		Source                 policyConnectedIdentity
		Revision               string
		Content                policySourceContent
		Configuration          string
		Lifecycle              policySourceLifecycle
		Coverage, Availability string
		Document               map[string]json.RawMessage
	}{binding.BindingID, o.Source, o.SourceRevision, o.Content, o.Extraction.ConfigurationDigest, o.Lifecycle, o.Coverage, o.Availability, semantic})
	projection, err = s.canonicalPolicySource(ctx, projection)
	if err != nil {
		return policyPreparedObservation{}, bad
	}
	originalRef, documentRef := artifactReference(original), artifactReference(document)
	return policyPreparedObservation{Original: original, Document: document, Metadata: policySourceObservation{ID: o.ObservationID, BindingID: o.BindingID, Source: o.Source, SourceRevision: o.SourceRevision, ObservedAt: o.ObservedAt, RetainedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"), Availability: o.Availability, Coverage: o.Coverage, Lifecycle: o.Lifecycle, Content: o.Content, Extraction: o.Extraction, Original: originalRef, Document: documentRef, SemanticDigest: digestOf(projection), Name: record.Document.Name}}, nil
}

func (s *Server) startPolicySources() {
	ctx, cancel := context.WithCancel(context.Background())
	s.policySourceCancel = cancel
	s.policySourceDone = make(chan struct{})
	go func() {
		defer close(s.policySourceDone)
		token, _ := randomStagingName("")
		owner := digestOf([]byte(token))
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				s.pollPolicySources(ctx, owner)
				timer.Reset(time.Second)
			}
		}
	}()
}

func (s *Server) pollPolicySources(ctx context.Context, owner string) {
	repository := filePolicyRepository{open: s.openPolicies}
	var ids []string
	if repository.View(ctx, func(ledger policyLedger) error {
		for id := range ledger.Sources {
			ids = append(ids, id)
		}
		return nil
	}) != nil {
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		s.pollPolicySource(ctx, owner, id)
	}
}

func (s *Server) pollPolicySource(parent context.Context, owner, id string) {
	ctx, cancel := context.WithTimeout(parent, 55*time.Second)
	defer cancel()
	repository := filePolicyRepository{open: s.openPolicies}
	var admitted *policyConnectedSource
	// Authentication-policy mutation takes the exclusive lock before touching
	// signIn.mu. Never hold this read lock across provider I/O.
	s.signIn.policySourceMu.RLock()
	authority, authErr := s.policySourceAuthorityBasis()
	err := repository.Update(ctx, func(ledger *policyLedger) (bool, error) {
		source := ledger.Sources[id]
		if source == nil || policyRecoverySourceHeld(source) {
			return false, nil
		}
		if (authErr != nil || source.Grant.Authority != authority) && source.State != "authorization-ended" && source.State != "revoked" {
			source.State = "authorization-ended"
			source.Generation++
			source.Poll = nil
			source.CleanupOperations = appendPolicySourceCleanup(source.CleanupOperations, source.Grant.OperationID)
			if source.Binding != nil {
				source.Cleanup = appendPolicySourceCleanup(source.Cleanup, source.Binding.BindingID)
			}
			source.ErrorCode = "source-authorization-ended"
			source.Gap = true
			source.NextCheck = ""
			return true, nil
		}
		now := time.Now().UTC()
		interrupted := false
		if source.Poll != nil {
			deadline, _ := time.Parse("2006-01-02T15:04:05Z", source.Poll.Deadline)
			if now.Before(deadline) {
				return false, nil
			}
			interrupted = true
			source.Gap = true
		}
		if !interrupted && source.NextCheck != "" {
			next, _ := time.Parse("2006-01-02T15:04:05Z", source.NextCheck)
			if now.Before(next) {
				return false, nil
			}
		}
		if source.State != "active" && source.State != "binding" && len(source.Cleanup) == 0 && len(source.CleanupOperations) == 0 {
			return false, nil
		}
		token, _ := randomStagingName("")
		source.Poll = &policySourcePoll{ID: digestOf([]byte(token)), Owner: owner, Deadline: now.Add(time.Minute).Format("2006-01-02T15:04:05Z"), Generation: source.Generation}
		source.LastAttempt = now.Format("2006-01-02T15:04:05Z")
		source.NextCheck = now.Add(time.Duration(source.FrequencyMinutes) * time.Minute).Format("2006-01-02T15:04:05Z")
		copyRaw, _ := json.Marshal(source)
		var copy policyConnectedSource
		_ = json.Unmarshal(copyRaw, &copy)
		admitted = &copy
		return true, nil
	})
	s.signIn.policySourceMu.RUnlock()
	if err != nil || admitted == nil {
		return
	}
	var binding *policySourceBinding
	var prepared *policyPreparedObservation
	cleanup := []string{}
	cleanupIDs := append([]string{}, admitted.Cleanup...)
	remainingOperations := append([]string{}, admitted.CleanupOperations...)
	if len(admitted.CleanupOperations) > 0 {
		raw, e := s.policySourceGateway(ctx, "source-bindings", struct{}{})
		var bindings struct {
			Version  int                   `json:"version"`
			Bindings []policySourceBinding `json:"bindings"`
		}
		if e == nil && decodeDataJSON(raw, &bindings) == nil && bindings.Version == 1 && len(bindings.Bindings) <= 48 {
			for _, b := range bindings.Bindings {
				if validPolicySourceBinding(b) && contains(admitted.CleanupOperations, b.OperationID) {
					cleanupIDs = appendPolicySourceCleanup(cleanupIDs, b.BindingID)
					remainingOperations = removePolicySourceCleanup(remainingOperations, b.OperationID)
				}
			}
		}
	}
	for _, bindingID := range cleanupIDs {
		if _, e := s.policySourceGateway(ctx, "source-revoke", map[string]string{"bindingId": bindingID}); e != nil {
			cleanup = append(cleanup, bindingID)
		}
	}
	if admitted.State == "binding" {
		raw, e := s.policySourceGateway(ctx, "source-bind", map[string]string{"operationId": admitted.Grant.OperationID, "resourceId": admitted.Grant.ResourceID, "selectionContext": admitted.Grant.SelectionContext, "consent": "recurring-read"})
		err = e
		var b policySourceBinding
		if err == nil {
			if rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &b) != nil || !validPolicySourceBinding(b) || b.OperationID != admitted.Grant.OperationID || b.Source.ResourceID != admitted.Grant.ResourceID || b.State != "active" {
				err = policySourceFailure("source-invalid")
			} else {
				binding = &b
			}
		}
	} else if admitted.State == "active" {
		binding = admitted.Binding
	}
	if err == nil && binding != nil {
		// A revoked or replaced pending bind may have completed after admission.
		// Recheck the durable generation before issuing a provider read.
		current := false
		_ = repository.View(ctx, func(ledger policyLedger) error {
			held := ledger.Sources[id]
			current = held != nil && held.Generation == admitted.Generation && (held.State == "active" || held.State == "binding")
			return nil
		})
		if !current {
			return
		}
		raw, e := s.policySourceGateway(ctx, "source-observe", map[string]string{"bindingId": binding.BindingID})
		err = e
		if err == nil {
			item, e := s.verifyPolicyObservation(ctx, raw, *binding)
			err = e
			if err == nil {
				prepared = &item
			}
		}
	}
	s.signIn.policySourceMu.RLock()
	defer s.signIn.policySourceMu.RUnlock()
	currentAuthority, currentErr := s.policySourceAuthorityBasis()
	_ = repository.Update(ctx, func(ledger *policyLedger) (bool, error) {
		source := ledger.Sources[id]
		if source == nil || policyRecoverySourceHeld(source) || source.Generation != admitted.Generation || source.Poll == nil || *source.Poll != *admitted.Poll || ((currentErr != nil || currentAuthority != admitted.Grant.Authority) && admitted.State != "authorization-ended" && admitted.State != "revoked") {
			return false, nil
		}
		source.Poll = nil
		source.Cleanup = cleanup
		source.CleanupOperations = remainingOperations
		if binding != nil {
			source.Binding = binding
			if source.State == "binding" {
				source.State = "active"
			}
		}
		if err != nil {
			source.ErrorCode = policySourceErrorCode(err)
			if contains([]string{"source-binding-unavailable", "connect-required", "reconnect-required", "blocked-by-policy"}, source.ErrorCode) {
				source.State = "authorization-ended"
				source.Generation++
				source.CleanupOperations = appendPolicySourceCleanup(source.CleanupOperations, source.Grant.OperationID)
				if source.Binding != nil {
					source.Cleanup = appendPolicySourceCleanup(source.Cleanup, source.Binding.BindingID)
				}
			}
			source.Gap = true
			return true, nil
		}
		if prepared != nil {
			observation := &prepared.Metadata

			if len(source.Observations) == 0 || source.Observations[len(source.Observations)-1].SemanticDigest != observation.SemanticDigest {
				if len(source.Observations) >= policyHistoryLimit {
					source.ErrorCode = "storage-capacity"
					source.Gap = true
					source.State = "paused"
					source.Generation++
					return true, nil
				}
				for _, held := range source.Observations {
					if held.ID == observation.ID {
						return false, errors.New("reused source observation identity")
					}
				}
				if _, retainErr := s.artifactRepository().Put(ctx, prepared.Original); retainErr != nil {
					return false, retainErr
				}
				if _, retainErr := s.artifactRepository().Put(ctx, prepared.Document); retainErr != nil {
					return false, retainErr
				}
				source.Observations = append(source.Observations, *observation)
			}
			source.LastSuccess = observation.ObservedAt
			source.ErrorCode = ""
			source.Gap = false
		}
		return true, nil
	})
}

func removePolicySourceCleanup(ids []string, id string) []string {
	out := []string{}
	for _, held := range ids {
		if held != id {
			out = append(out, held)
		}
	}
	return out
}
