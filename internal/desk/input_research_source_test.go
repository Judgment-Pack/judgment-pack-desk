package desk

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// This exercises the actual, separately installed Gateway verifier. It makes no
// source or model request. CI can point this at its built/pinned Gateway binary.
func TestResearchProofIndependentVerification(t *testing.T) {
	program := os.Getenv("JPS_TEST_GATEWAY")
	if program == "" {
		t.Skip("set JPS_TEST_GATEWAY to the Gateway verifier binary")
	}
	ctx := context.Background()
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	public := key.Public().(ed25519.PublicKey)
	keyID := digestOf(public)[:32]
	canon := func(v any) []byte {
		raw, _ := json.Marshal(v)
		out, e := researchCanon(ctx, program, raw)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	args := json.RawMessage(`{"url":"https://example.com/"}`)
	salt := make([]byte, 32)
	commitment := append(append(salt, []byte("args:")...), canon(args)...)
	rawResult, _ := json.Marshal(map[string]string{"text": "public test text", "document": strings.Repeat("x", 3<<20)})
	result := json.RawMessage(rawResult)
	receipt := map[string]any{"receiptVersion": "3", "kind": "acquisition", "sessionId": "research-test", "callIndex": 0, "prevSignature": nil, "source": "web", "authority": "gateway:test", "keyId": keyID, "resultDigest": "sha256:" + digestOf(canon(result)), "argumentsCommitment": "sha256:" + digestOf(commitment), "caller": nil, "servedAt": "2026-10-12T00:00:00Z", "acquisition": map[string]any{"shape": "http", "endpoint": "https://example.com/", "adapter": map[string]any{"name": "test-adapter", "version": "1", "digest": "sha256:" + strings.Repeat("a", 64)}, "observedAt": "2026-10-12T00:00:00Z", "schema": nil, "snapshot": nil, "statement": nil, "peerIdentity": nil, "upstreamToken": nil}}
	receipt["signature"] = hex.EncodeToString(ed25519.Sign(key, append([]byte("judgment-pack-gateway/receipt/3:"), canon(receipt)...)))
	seal := map[string]any{"sessionId": "research-test", "finalCount": 1, "sealedAt": "2026-10-12T00:00:00Z", "keyId": keyID}
	seal["signature"] = hex.EncodeToString(ed25519.Sign(key, append([]byte("judgment-pack-gateway/seal/2:"), canon(seal)...)))
	response := canon(map[string]any{"receipt": receipt, "result": result, "salts": map[string]string{"args": hex.EncodeToString(salt)}})
	p := inputResearchProof{Session: "research-test", Source: "web", Arguments: args, Pin: inputResearchPin{Authority: "gateway:test", Public: hex.EncodeToString(public)}, Response: response, Seal: canon(seal)}
	if _, _, e := verifyResearchProof(ctx, program, p); e != nil {
		t.Fatal("valid receipt failed", e)
	}
	changed := p
	changed.Arguments = json.RawMessage(`{"url":"https://other.example/"}`)
	if _, _, e := verifyResearchProof(ctx, program, changed); e == nil {
		t.Fatal("different request verified")
	}
	changed = p
	changed.Pin.Public = strings.Repeat("a", 64)
	if _, _, e := verifyResearchProof(ctx, program, changed); e == nil {
		t.Fatal("foreign key verified")
	}
	changed = p
	changed.Response = []byte(strings.Replace(string(response), "public test text", "modified text", 1))
	if _, _, e := verifyResearchProof(ctx, program, changed); e == nil {
		t.Fatal("modified source verified")
	}
	changed = p
	changed.Seal = nil
	if _, _, e := verifyResearchProof(ctx, program, changed); e == nil {
		t.Fatal("unsealed source verified")
	}
}

func TestResearchSourceRefusalExplainsPolicyWithoutEchoingSourceText(t *testing.T) {
	err := researchSourceFailure(400, []byte(`{"error":"source failed: web-redirect-origin {\"redirect\":{\"policy\":\"google-grounding\"}} private-token"}`))
	if !strings.Contains(err.Error(), "Web access policy blocked") || !strings.Contains(err.Error(), "No automatic retry") || strings.Contains(err.Error(), "private-token") {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"error":"private-token"}`, `not JSON private-token`, strings.Repeat("private-token", 7000), `{"error":{"secret":"private-token"}}`} {
		err := researchSourceFailure(400, []byte(raw))
		if err.Error() != "The source request was refused (HTTP 400). No automatic retry was made." {
			t.Fatal(err)
		}
	}
}

func TestResearchSearchRefusalsKeepSupportedCause(t *testing.T) {
	for _, code := range []string{"search-temporary", "search-invalid-request", "search-invalid-response", "search-timeout", "search-not-grounded", "provider-unavailable", "credentials-required", "search-budget-exhausted", "rate-limited", "source-changed", "connect-required", "setup-required", "blocked-by-policy", "invalid-request"} {
		t.Run(code, func(t *testing.T) {
			for _, prefix := range []string{"", "source failed: "} {
				raw, _ := json.Marshal(map[string]string{"error": prefix + code})
				err := researchSourceFailure(400, raw)
				var refusal *researchSourceError
				if !errors.As(err, &refusal) || refusal.code != code || refusal.recovery != "" || !strings.Contains(refusal.message, "No automatic retry was made.") || strings.Contains(refusal.message, "Web access") {
					t.Fatalf("search refusal lost or misclassified: %v", err)
				}
			}
		})
	}
	for _, raw := range []string{`{"error":"source failed: search-timeout secret-token"}`, `{"error":"source failed: search-timeout: https://private.example/"}`, `{"error":"source failed: unknown-search-error"}`} {
		err := researchSourceFailure(400, []byte(raw))
		var refusal *researchSourceError
		if errors.As(err, &refusal) || strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "private.example") {
			t.Fatal("unsupported source text accepted", err)
		}
	}
}

func TestResearchSearchRefusalIsSavedWithoutReplay(t *testing.T) {
	root, _ := researchRunFixture(t)
	if _, err := changeInputResearch(root, func(run *inputResearchRun) error {
		run.Recipe.Limits.Searches = 2
		run.Recipe.Search = &inputResearchSearch{Connection: "search-test", Revision: "revision"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	args := json.RawMessage(`{"query":"insurance companies"}`)
	call := func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		calls++
		return nil, researchSourceFailure(400, []byte(`{"error":"source failed: search-timeout"}`))
	}
	for i := 0; i < 2; i++ {
		if _, err := researchAttempt(context.Background(), root, "search", args, call); err == nil {
			t.Fatal("timeout accepted as success")
		}
	}
	run, err := readInputResearch(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Attempts) != 1 {
		t.Fatalf("expected one search attempt, got %d", len(run.Attempts))
	}
	step := researchActivity(run).Steps[0]
	if calls != 1 || step.State != "uncertain" || step.ProblemCode != "search-timeout" || step.Recovery != "" || !strings.Contains(step.Problem, "timed out") {
		t.Fatalf("timeout classification lost or retried: calls=%d step=%+v", calls, step)
	}
}

func TestResearchPageUsesNativePDFTextAndKeepsRequestBinding(t *testing.T) {
	args := json.RawMessage(`{"url":"https://example.com/report.pdf"}`)
	page := func(pages []map[string]any, requested, status string) json.RawMessage {
		raw, err := json.Marshal(map[string]any{
			"attachmentVersion": "1", "content": map[string]any{"pages": pages},
			"processing": map[string]any{"status": status},
			"provenance": map[string]any{"source": map[string]any{"kind": "web", "url": "https://example.com/report.pdf", "requestedUrl": requested}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	source := inputResearchSource{Kind: "page", Verification: "verified", Digest: "retained-digest"}
	for _, extraction := range []string{"verbatim", "text-layer"} {
		t.Run(extraction, func(t *testing.T) {
			raw := page([]map[string]any{{"status": "ok", "extraction": extraction, "text": "Annual report evidence."}}, "https://example.com/report.pdf", "complete")
			got, err := decodeInputResearchSource(source, args, raw)
			if err != nil || got.Text != "Annual report evidence.\n" || got.Digest != source.Digest || got.URL != "https://example.com/report.pdf" {
				t.Fatalf("usable extraction discarded: %+v %v", got, err)
			}
		})
	}
	for _, tt := range []struct{ name, method, status, text, requested, processing string }{
		{"failed page", "text-layer", "failed", "unusable", "https://example.com/report.pdf", "partial"},
		{"OCR not enabled for research", "ocr", "ok", "inferred", "https://example.com/report.pdf", "complete"},
		{"unknown extraction", "invented", "ok", "unusable", "https://example.com/report.pdf", "complete"},
		{"empty page", "text-layer", "ok", "  ", "https://example.com/report.pdf", "complete"},
		{"different request", "text-layer", "ok", "text", "https://other.example/report.pdf", "complete"},
		{"failed processing", "text-layer", "ok", "text", "https://example.com/report.pdf", "failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeInputResearchSource(source, args, page([]map[string]any{{"status": tt.status, "extraction": tt.method, "text": tt.text}}, tt.requested, tt.processing)); err == nil {
				t.Fatal("unusable page accepted")
			}
		})
	}
	long := strings.Repeat("界", 16001)
	raw := page([]map[string]any{{"status": "ok", "extraction": "text-layer", "text": long}}, "https://example.com/report.pdf", "partial")
	got, err := decodeInputResearchSource(source, args, raw)
	if err != nil || len(got.Text) > 48000 || !validUTF8([]byte(got.Text)) {
		t.Fatal("page text bound changed", err)
	}
}

// Optional local regression using already saved bytes; never contacts a source.
func TestResearchRetainedPageOffline(t *testing.T) {
	path, program := os.Getenv("JPS_TEST_RESEARCH_PROOF"), os.Getenv("JPS_TEST_GATEWAY")
	if path == "" || program == "" {
		t.Skip("set JPS_TEST_RESEARCH_PROOF and JPS_TEST_GATEWAY for an offline saved-page check")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var proof inputResearchProof
	if json.Unmarshal(raw, &proof) != nil || proof.Source != "web" {
		t.Fatal("expected a retained web proof")
	}
	result, digest, err := verifyResearchProof(context.Background(), program, proof)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeInputResearchSource(inputResearchSource{Kind: "page", Verification: "verified", Digest: digest}, proof.Arguments, result)
	if err != nil || strings.TrimSpace(got.Text) == "" {
		t.Fatal("saved verified page unusable", err)
	}
}

func TestResearchSourceResponseSizeAndReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int
		broken bool
		want   string
	}{
		{"larger than old cap", 3 << 20, false, ""},
		{"at cap", inputResearchSourceResponseLimit, false, ""},
		{"over cap", inputResearchSourceResponseLimit + 1, false, "too large"},
		{"connection lost", 0, true, "could not be read completely"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.broken {
					w.Header().Set("Content-Length", "100")
					w.WriteHeader(200)
					_, _ = w.Write([]byte("short"))
					return
				}
				_, _ = w.Write(bytes.Repeat([]byte("x"), tc.size))
			}))
			defer gateway.Close()
			s, ts, _ := assistantServer(t)
			defer ts.Close()
			writeDeskConfig(t, s, researchDeskFile(gateway.URL))
			raw, err := s.researchSourceCall(context.Background(), inputResearchPin{Authority: "gateway:test", Public: testSignerPublic}, "acquire", map[string]any{})
			if tc.want == "" {
				if err != nil || len(raw) != tc.size {
					t.Fatal(len(raw), err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal(err)
			}
		})
	}
}

func TestResearchProofStorageBudgetAndEscaping(t *testing.T) {
	root, _ := researchRunFixture(t)
	// This remains a small enough document. Default JSON HTML escaping would
	// unnecessarily turn the retained three MiB payload into eighteen MiB.
	raw := json.RawMessage(`{"text":"` + strings.Repeat("<", 3<<20) + `"}`)
	p := inputResearchProof{Response: raw}
	size, err := saveResearchProof(root, "source.proof.json", p, 0)
	if err != nil || size > 4<<20 {
		t.Fatal(size, err)
	}
	stored, err := readPrivateData(root, "source.proof.json", inputResearchSourceProofLimit)
	var loaded inputResearchProof
	if err != nil || json.Unmarshal(stored, &loaded) != nil || !bytes.Equal(loaded.Response, raw) {
		t.Fatal("source bytes changed", err)
	}
	if _, err = saveResearchProof(root, "over.proof.json", p, inputResearchRetainedByteLimit-size); err == nil {
		t.Fatal("budget ignored")
	}
	if _, err = root.Stat("over.proof.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("over-budget proof written", err)
	}
}

// Inspect a local, already retained acquisition; never refetch a publisher.
func TestResearchRetainedLargeArtifactOffline(t *testing.T) {
	path, program := os.Getenv("JPS_TEST_RESEARCH_ARTIFACT"), os.Getenv("JPS_TEST_GATEWAY")
	if path == "" || program == "" {
		t.Skip("set retained artifact and Gateway verifier paths")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := researchCanon(context.Background(), program, raw)
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Provenance struct {
			Source struct {
				Requested string `json:"requestedUrl"`
			} `json:"source"`
		} `json:"provenance"`
	}
	if err = json.Unmarshal(canonical, &page); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"url": page.Provenance.Source.Requested})
	source, err := decodeInputResearchSource(inputResearchSource{Kind: "page"}, args, canonical)
	if err != nil || source.Text == "" || len(source.Text) > 48000 {
		t.Fatal("retained PDF not usable", err)
	}
	t.Logf("Retained artifact: %d bytes; bounded research text: %d bytes. No provider request.", len(raw), len(source.Text))
}

func TestResearchSourceCallRecoveryRequiresStableAcquisition(t *testing.T) {
	for _, tc := range []struct {
		name, path       string
		changed, wantGap bool
	}{
		{"acquire", "acquire", false, true},
		{"seal", "seal", false, false},
		{"changed gateway", "acquire", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			defer ts.Close()
			gatewayURL := ""
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.changed {
					writeDeskConfig(t, s, strings.Replace(researchDeskFile(gatewayURL), "gateway:test", "gateway:changed", 1))
				}
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"source failed: web-over-limit"}`))
			}))
			defer gateway.Close()
			gatewayURL = gateway.URL
			writeDeskConfig(t, s, researchDeskFile(gateway.URL))
			_, err := s.researchSourceCall(context.Background(), inputResearchPin{Authority: "gateway:test", Public: testSignerPublic}, tc.path, map[string]any{})
			var refusal *researchSourceError
			allow := false
			if errors.As(err, &refusal) {
				allow = researchContinuableAttempt(inputResearchAttempt{Kind: "page", State: "uncertain", ProblemCode: refusal.code, AcquisitionRefusal: refusal.acquisitionRefusal})
			}
			if err == nil || allow != tc.wantGap {
				t.Fatal("wrong source recovery authority", err, allow)
			}
		})
	}
}
