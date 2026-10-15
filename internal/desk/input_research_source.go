package desk

// Internal research uses the configured Gateway and independently verifies its
// returned bytes. A receipt authenticates acquisition, not a page's assertions.
import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type inputResearchPin struct {
	Authority string `json:"authority"`
	Public    string `json:"public"`
}
type inputResearchProof struct {
	Session   string           `json:"session"`
	Source    string           `json:"source"`
	Arguments json.RawMessage  `json:"arguments"`
	Pin       inputResearchPin `json:"pin"`
	Response  json.RawMessage  `json:"response"`
	Seal      json.RawMessage  `json:"seal,omitempty"`
}

func (s *Server) researchVerifier() (string, error) {
	if s.cfg.LocalGatewayBundle == "" {
		return "", errors.New("Install Desk's local processing components to verify research sources.")
	}
	if e := verifyGatewayBundle(s.cfg.LocalGatewayBundle); e != nil {
		return "", e
	}
	return filepath.Join(s.cfg.LocalGatewayBundle, executableName("gateway")), nil
}

// The local Gateway admits 16 MiB source results; receipts add a small envelope.
// These are retained acquisition bytes, separate from bounded model context.
const inputResearchSourceResponseLimit = (16 << 20) + (64 << 10)
const inputResearchSourceProofLimit = inputResearchSourceResponseLimit + (128 << 10)
const inputResearchRetainedByteLimit = 64 << 20
const inputResearchSourceMetadataReserve = 512 << 10

func researchCanon(ctx context.Context, program string, raw []byte) ([]byte, error) {
	if len(raw) > inputResearchSourceResponseLimit {
		return nil, errors.New("Research source JSON exceeds its verification limit.")
	}
	cmd := exec.CommandContext(ctx, program, "canon")
	cmd.Stdin = bytes.NewReader(raw)
	result, e := cmd.Output()
	if e != nil || len(result) > inputResearchSourceResponseLimit {
		return nil, errors.New("Research source JSON could not be canonicalized.")
	}
	return result, nil
}
func verifyResearchProof(ctx context.Context, program string, p inputResearchProof) (json.RawMessage, string, error) {
	fail := errors.New("The retained source proof could not be verified against the reviewed Gateway.")
	if _, e := researchCanon(ctx, program, p.Response); e != nil {
		return nil, "", fail
	}
	var envelope struct {
		Result  json.RawMessage   `json:"result"`
		Receipt json.RawMessage   `json:"receipt"`
		Salts   map[string]string `json:"salts"`
	}
	if json.Unmarshal(p.Response, &envelope) != nil {
		return nil, "", fail
	}
	var receipt struct {
		Version     string `json:"receiptVersion"`
		Kind        string `json:"kind"`
		Source      string `json:"source"`
		Session     string `json:"sessionId"`
		Index       int    `json:"callIndex"`
		Digest      string `json:"resultDigest"`
		Commitment  string `json:"argumentsCommitment"`
		Acquisition struct {
			Shape string `json:"shape"`
		} `json:"acquisition"`
	}
	if json.Unmarshal(envelope.Receipt, &receipt) != nil || receipt.Version != "3" || receipt.Kind != "acquisition" || receipt.Source != p.Source || receipt.Session != p.Session || receipt.Index != 0 || receipt.Acquisition.Shape != "http" {
		return nil, "", fail
	}
	args, e := researchCanon(ctx, program, p.Arguments)
	if e != nil {
		return nil, "", fail
	}
	salt, e := hex.DecodeString(envelope.Salts["args"])
	if e != nil || len(salt) != 32 {
		return nil, "", fail
	}
	commitment := append(append(salt, []byte("args:")...), args...)
	if receipt.Commitment != "sha256:"+digestOf(commitment) {
		return nil, "", fail
	}
	result, e := researchCanon(ctx, program, envelope.Result)
	if e != nil {
		return nil, "", fail
	}
	digest := digestOf(result)
	if receipt.Digest != "sha256:"+digest {
		return nil, "", fail
	}
	seal, e := researchCanon(ctx, program, p.Seal)
	if e != nil {
		return nil, "", fail
	}
	var anchor struct {
		Session string `json:"sessionId"`
		Count   int    `json:"finalCount"`
	}
	if json.Unmarshal(seal, &anchor) != nil || anchor.Session != p.Session || anchor.Count != 1 {
		return nil, "", fail
	}
	// The store contains only this response and its one-receipt signed seal.
	dir, e := os.MkdirTemp("", "desk-research-proof-")
	if e != nil {
		return nil, "", fail
	}
	defer os.RemoveAll(dir)
	for _, sub := range []string{"artifacts", filepath.Join("receipts", p.Session)} {
		if e = os.MkdirAll(filepath.Join(dir, sub), 0700); e != nil {
			return nil, "", fail
		}
	}
	for name, raw := range map[string][]byte{filepath.Join("artifacts", digest): result, filepath.Join("receipts", p.Session, "0.json"): envelope.Receipt, "registry.jsonl": append(seal, '\n')} {
		if e = os.WriteFile(filepath.Join(dir, name), raw, 0600); e != nil {
			return nil, "", fail
		}
	}
	cmd := exec.CommandContext(ctx, program, "verify", dir, filepath.Join(dir, "registry.jsonl"), p.Pin.Authority)
	cmd.Stdin = strings.NewReader(p.Pin.Public)
	raw, e := cmd.Output()
	var verdict struct {
		OK bool `json:"ok"`
	}
	if e != nil || json.Unmarshal(raw, &verdict) != nil || !verdict.OK {
		return nil, "", fail
	}
	return result, receipt.Digest, nil
}
func (s *Server) researchSourceCall(ctx context.Context, pin inputResearchPin, path string, body any) ([]byte, error) {
	gateway, e := s.configuredResearch()
	if e != nil {
		return nil, e
	}
	if gateway.authority != pin.Authority || gateway.signerPublic != pin.Public {
		return nil, errors.New("The research Gateway changed. Review the connections before starting again.")
	}
	target, e := researchTarget(gateway.url, path)
	if e != nil {
		return nil, e
	}
	raw, _ := json.Marshal(body)
	request, e := http.NewRequestWithContext(ctx, "POST", target.String(), bytes.NewReader(raw))
	if e != nil {
		return nil, e
	}
	request.Header.Set("Content-Type", "application/json")
	select {
	case s.researchSlots <- struct{}{}:
		defer func() { <-s.researchSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	transport := relayTransport
	if transport == nil {
		transport = http.DefaultTransport
	}
	response, e := transport.RoundTrip(request)
	if e != nil {
		return nil, errors.New("The source request did not return a retained response. Its usage may be unknown.")
	}
	defer response.Body.Close()
	currentGateway := func() error {
		current, err := s.configuredResearch()
		if err != nil || current.authority != pin.Authority || current.signerPublic != pin.Public {
			return errors.New("The Gateway changed during the source request.")
		}
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, err := readBounded(response.Body, 64<<10)
		if err != nil {
			detail = nil
		}
		if err := currentGateway(); err != nil {
			return nil, err
		}
		failure := researchSourceFailure(response.StatusCode, detail)
		var refusal *researchSourceError
		if path == "acquire" && errors.As(failure, &refusal) {
			refusal.acquisitionRefusal = true
		}
		return nil, failure
	}
	limit := inputResearchSourceResponseLimit
	if path != "acquire" {
		limit = 64 << 10
	}
	raw, e = readBounded(response.Body, limit)
	if err := currentGateway(); err != nil {
		return nil, err
	}
	if e != nil {
		var oversized overLimit
		if errors.As(e, &oversized) {
			if path == "acquire" {
				return nil, &researchSourceError{code: "source-response-too-large", message: "This source is too large for research. Use a smaller document or a different source. No automatic retry was made."}
			}
			return nil, errors.New("The source seal exceeded its response limit. No automatic retry was made.")
		}
		return nil, errors.New("The source response could not be read completely. Its outcome is uncertain; no automatic retry was made.")
	}
	return raw, nil
}
func (s *Server) acquireResearchSource(ctx context.Context, root *os.Root, run inputResearchRun, attempt inputResearchAttempt, args json.RawMessage) (inputResearchSource, error) {
	var source inputResearchSource
	run.RetrievedBytes += researchReservedBytes(run) - attempt.ReservedBytes
	if run.RetrievedBytes < 0 || run.RetrievedBytes+inputResearchSourceMetadataReserve >= inputResearchRetainedByteLimit {
		return source, errors.New("Research reached its saved-source storage limit. Use fewer or smaller documents in a new run. No automatic retry was made.")
	}
	program, e := s.researchVerifier()
	if e != nil {
		return source, e
	}
	var nonce [16]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		return source, e
	}
	name := "web"
	if attempt.Kind == "search" {
		name = "web-search"
	}
	proof := inputResearchProof{Session: "inputs-" + hex.EncodeToString(nonce[:]), Source: name, Arguments: args, Pin: run.Gateway}
	proof.Response, e = s.researchSourceCall(ctx, run.Gateway, "acquire", map[string]any{"session": proof.Session, "source": name, "arguments": args})
	if e != nil {
		return source, e
	}
	// Persist the exact response before sealing: an interrupted seal must not
	// silently cause a second acquisition or erase the original provider usage.
	storedBytes := 0
	save := func() error {
		var err error
		storedBytes, err = saveResearchProof(root, attempt.ID+".proof.json", proof, run.RetrievedBytes)
		return err
	}
	if e = save(); e != nil {
		return source, e
	}
	proof.Seal, e = s.researchSourceCall(ctx, run.Gateway, "seal", map[string]any{"session": proof.Session})
	if e != nil {
		return source, e
	}
	if e = save(); e != nil {
		return source, e
	}
	result, digest, e := verifyResearchProof(ctx, program, proof)
	if e != nil {
		return source, e
	}
	source = inputResearchSource{ID: attempt.ID, Kind: attempt.Kind, At: time.Now().UTC().Format(time.RFC3339Nano), Digest: digest, Verification: "verified", Bytes: storedBytes}
	return decodeInputResearchSource(source, args, result)
}

// Only called after verifying the receipt, request commitment and session seal.
// Keep extraction separate so retained responses can be checked without a new
// provider request. The proof still contains each page's extraction method.
func decodeInputResearchSource(source inputResearchSource, args, result json.RawMessage) (inputResearchSource, error) {
	if source.Kind == "search" {
		var request struct {
			Connection string `json:"connection"`
			Revision   string `json:"revision"`
			Query      string `json:"query"`
			MaxResults int    `json:"maxResults"`
		}
		var found struct {
			Version    int    `json:"version"`
			Connection string `json:"connection"`
			Revision   string `json:"revision"`
			Query      string `json:"query"`
			Kind       string `json:"kind"`
			Hits       []struct {
				Title   string `json:"title"`
				URL     string `json:"url"`
				Snippet string `json:"snippet"`
			} `json:"hits"`
		}
		if json.Unmarshal(args, &request) != nil || json.Unmarshal(result, &found) != nil || found.Version != 1 || found.Connection != request.Connection || found.Revision != request.Revision || found.Query != request.Query || !contains([]string{"search-results", "grounded-answer"}, found.Kind) || len(found.Hits) > request.MaxResults {
			return source, errors.New("Search results did not match the reviewed request.")
		}
		for _, hit := range found.Hits {
			url, e := inputResearchURL(hit.URL)
			if e != nil || !strings.HasPrefix(url, "https://") || len(hit.Title) > 512 || len(hit.Snippet) > 2000 {
				return source, errors.New("Search returned an invalid lead.")
			}
			source.URLs = append(source.URLs, url)
		}
		leads, _ := json.Marshal(found.Hits)
		source.Text = string(leads)
		source.Query = request.Query
	} else {
		var request struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(args, &request)
		var page struct {
			Version string `json:"attachmentVersion"`
			Content struct {
				Pages []struct {
					Text       string `json:"text"`
					Status     string `json:"status"`
					Extraction string `json:"extraction"`
				} `json:"pages"`
			} `json:"content"`
			Processing struct {
				Status string `json:"status"`
			} `json:"processing"`
			Provenance struct {
				Source struct {
					Kind      string `json:"kind"`
					URL       string `json:"url"`
					Requested string `json:"requestedUrl"`
				} `json:"source"`
			} `json:"provenance"`
		}
		if json.Unmarshal(result, &page) != nil || page.Version != "1" || page.Provenance.Source.Kind != "web" || page.Provenance.Source.Requested != request.URL || !contains([]string{"complete", "partial"}, page.Processing.Status) {
			return source, errors.New("The page did not return usable text for the requested URL.")
		}
		var e error
		source.URL, e = inputResearchURL(page.Provenance.Source.URL)
		if e != nil {
			return source, e
		}
		source.URLs = []string{source.URL, request.URL}
		for _, p := range page.Content.Pages {
			// HTML/plain text uses verbatim; a PDF's embedded text uses
			// text-layer. Neither is an AI interpretation or OCR output.
			if p.Status == "ok" && contains([]string{"verbatim", "text-layer"}, p.Extraction) {
				source.Text += p.Text + "\n"
			}
		}
		text := strings.ToLower(strings.Join(strings.Fields(source.Text), " "))
		if text == "loading × sorry to interrupt css error refresh" || text == "loading sorry to interrupt css error refresh" {
			return source, &researchSourceError{code: "source-empty-text", message: "The page returned a loading or error screen without usable research text. Its request was not repeated."}
		}
		if len(source.Text) > 48000 {
			source.TextTruncated = true
			source.Text = source.Text[:48000]
			for !validUTF8([]byte(source.Text)) {
				source.Text = source.Text[:len(source.Text)-1]
			}
		}
		if strings.TrimSpace(source.Text) == "" {
			return source, &researchSourceError{code: "source-empty-text", message: "The page contains no usable text for research. This source was left out; its request was not repeated."}
		}
	}
	return source, nil
}

// Retain a business-readable failure classification, never arbitrary adapter
// stderr, page text, URLs, credentials or instructions from a failed source.
type researchSourceError struct {
	code, message, recovery string
	acquisitionRefusal      bool // never authorize replay after a successful acquire or during sealing
}

func (e *researchSourceError) Error() string { return e.message }

func researchSourceFailure(status int, raw []byte) error {
	var envelope struct {
		Error string `json:"error"`
	}
	if len(raw) <= 64<<10 && json.Unmarshal(raw, &envelope) == nil {
		value := strings.TrimPrefix(strings.TrimSpace(envelope.Error), "source failed: ")
		// Search adapters return a protocol token, not a provider response.
		// Match the complete token so arbitrary diagnostics never enter history.
		searchMessages := map[string]string{
			"search-timeout":          "The search provider timed out. Try a shorter query or increase Search timeout in Admin > Connections > Web search before starting a new run.",
			"search-not-grounded":     "The search provider returned no usable grounded sources. Try more specific research instructions before starting a new run.",
			"provider-unavailable":    "The search provider could not complete this request. Its cause is unknown; check the search connection before starting a new run.",
			"search-temporary":        "The search provider reported a temporary server error.",
			"search-invalid-request":  "The search provider rejected the request. Check the search connection and model settings before starting a new run.",
			"search-invalid-response": "The search provider returned an unusable response. No source was retained for this request.",
			"credentials-required":    "The search provider rejected authentication or access. Check this connection in Admin > Connections > Web search.",
			"search-budget-exhausted": "This connection has reached its daily search limit. Wait for the limit to reset or update it in Web search settings.",
			"rate-limited":            "The search provider is limiting requests. Wait before starting a new run.",
			"source-changed":          "The search connection changed. Review the recipe's search connection before starting a new run.",
			"connect-required":        "The selected search connection is no longer available. Select a connection in Web search settings.",
			"setup-required":          "The selected search connection needs configuration. Check Web search settings.",
			"blocked-by-policy":       "Web search is disabled by the connection policy.",
			"invalid-request":         "The search request was rejected as invalid.",
		}
		if message, ok := searchMessages[value]; ok {
			return &researchSourceError{code: value, message: message + " No automatic retry was made."}
		}
		fields := strings.Fields(value)
		if len(fields) > 0 {
			messages := map[string]string{
				"web-redirect-origin":   "The website redirected to another site, and the current Web access policy blocked the read. Review Web access settings before starting another run.",
				"web-public-only":       "The website resolves to a private or local address, which the page reader cannot access.",
				"web-invalid-url":       "The website address is not a public HTTPS URL the page reader accepts.",
				"web-unavailable":       "The website could not be read. It may be unavailable, require sign-in, or have timed out.",
				"web-over-limit":        "The page exceeded the page reader's size limit.",
				"web-unsupported-type":  "The page format is not supported by the page reader.",
				"web-redirect-limit":    "The website redirected too many times.",
				"web-outside-site":      "The page is outside the selected website.",
				"web-processing-failed": "The page was retrieved, but its text could not be processed.",
				"web-invalid-request":   "The page reader refused the request format.",
			}
			if message, ok := messages[fields[0]]; ok {
				return &researchSourceError{code: fields[0], message: message + " No automatic retry was made.", recovery: researchRedirectRecovery(envelope.Error)}
			}
		}
	}
	return fmt.Errorf("The source request was refused (HTTP %d). No automatic retry was made.", status)
}

// Keep proof growth bounded before touching disk. HTML escaping is unnecessary
// for private JSON files and can expand a valid document sixfold. Canonical
// verification continues to bind its original result and receipt exactly.
func saveResearchProof(root *os.Root, name string, proof inputResearchProof, retainedBytes int) (int, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(proof); err != nil {
		return 0, err
	}
	size := encoded.Len()
	if size > inputResearchSourceProofLimit || retainedBytes < 0 || retainedBytes+size+inputResearchSourceMetadataReserve > inputResearchRetainedByteLimit {
		return 0, errors.New("Research reached its saved-source storage limit. Use fewer or smaller documents in a new run. No automatic retry was made.")
	}
	if err := writePrivateData(root, name, encoded.Bytes()); err != nil {
		return 0, errors.New("The acquired source could not be saved. No automatic retry was made.")
	}
	return size, nil
}
