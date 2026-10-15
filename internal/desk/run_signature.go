package desk

// This explicit check verifies exactly one saved record's signature, using
// independently retained Runner public keys. It does not feed the export's
// own releaseDigest back to verify-run as independent trust. Where Desk
// captured release/profile references before the run, Runner checks those too.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var signatureRunID = regexp.MustCompile(`^run_[a-f0-9]{32}$`)

type runSignatureAnswer struct {
	Status       string `json:"status"`
	References   string `json:"references"`
	CheckedAt    string `json:"checkedAt"`
	RecordDigest string `json:"recordDigest,omitempty"`
	PublicKey    string `json:"publicKey,omitempty"`
}

func (s *Server) handleRunSignature(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("run")
	body, err := readBounded(r.Body, 1)
	if !signatureRunID.MatchString(id) || r.URL.RawQuery != "" || err != nil || len(body) != 0 {
		writeJSONCoded(w, 400, CodeBadRequest, "invalid saved record verification request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	result := s.checkRunSignature(ctx, id)
	writeJSON(w, 200, result)
}
func (s *Server) fetchSignatureExport(ctx context.Context, id string) ([]byte, error) {
	if s.jobs == nil {
		return nil, errNoRunner
	}
	endpoint, token, err := s.jobs.endpoint()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"/v1/runs/"+id+"/verification?version=5", http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("saved export unavailable")
	}
	return readBounded(resp.Body, runnerExportLimit)
}
func (s *Server) checkRunSignature(ctx context.Context, id string) (result runSignatureAnswer) {
	result = runSignatureAnswer{Status: "unavailable", References: "unavailable", CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	raw, err := s.fetchSignatureExport(ctx, id)
	if err != nil {
		return result
	}
	// Exact member names: no case-folding of ID, auditBytes or signatures.
	var bundle map[string]json.RawMessage
	if json.Unmarshal(raw, &bundle) != nil {
		return result
	}
	var run map[string]json.RawMessage
	if json.Unmarshal(bundle["run"], &run) != nil {
		return result
	}
	var named string
	var record, signatures []byte
	if json.Unmarshal(run["id"], &named) != nil || named != id || json.Unmarshal(run["auditBytes"], &record) != nil || len(record) == 0 || len(record) > 8<<20 {
		return result
	}
	if sidecar, ok := run["auditSignatures"]; ok && json.Unmarshal(sidecar, &signatures) != nil {
		return result
	}
	if len(signatures) > 16<<10 {
		return result
	}
	digest := sha256.Sum256(record)
	result.RecordDigest = "sha256:" + hex.EncodeToString(digest[:])
	defer func() { result.References = s.checkRunReferences(ctx, raw, result) }()
	if len(signatures) == 0 {
		result.Status = "unsigned"
		return result
	}
	if s.jobs.key == nil {
		return result
	}
	signing, keysDir, err := s.assistant.openRunnerSigning()
	if err != nil {
		return result
	}
	defer signing.Close()
	defer keysDir.Close()
	// Historical records use the retained list, not today's live seed or a
	// public key asserted by the export. No creation, rotation or removal here.
	keys, found, err := keysDir.readKeys(s.jobs.key.name + keysSuffix)
	if err != nil || !found || len(keys) == 0 {
		return result
	}
	return verifySavedSignature(ctx, s.cfg.JpackBin, record, signatures, keys, result)
}
func verifySavedSignature(ctx context.Context, bin string, record, signatures []byte, keys []deskPublicKey, result runSignatureAnswer) runSignatureAnswer {
	where, err := os.MkdirTemp("", "jpack-record-signature-")
	if err != nil {
		return result
	}
	defer os.RemoveAll(where)
	// Runner exports record bytes without JSONL framing. Add only its line
	// terminator; the signed payload bytes themselves are never re-encoded.
	framed := bytes.Clone(record)
	if !bytes.HasSuffix(framed, []byte("\n")) {
		framed = append(framed, '\n')
	}
	for name, data := range map[string][]byte{"trail.jsonl": framed, "signatures.jsonl": signatures} {
		if os.WriteFile(filepath.Join(where, name), data, 0600) != nil {
			return result
		}
	}
	file, err := os.Open(where)
	if err != nil {
		return result
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return result
	}
	dir := heldDir{file: file, path: where, info: info}
	result.Status = "invalid"
	for _, key := range keys {
		if key.check() != nil {
			continue
		}
		if os.WriteFile(filepath.Join(where, "public.key"), []byte(key.PublicKey+"\n"), 0600) != nil {
			result.Status = "unavailable"
			return result
		}
		out, runErr := runRuntime(ctx, bin, dir, "audit", "verify", "--trail", "trail.jsonl", "--signatures", "signatures.jsonl", "--public-key", "public.key", "--format", "json")
		answer, err := readAuditVerification(out, runErr)
		if err != nil || answer.Report == nil {
			result.Status = "unavailable"
			continue
		}
		report := answer.Report
		if runErr == nil && report.Status == "valid" && report.Lines == 1 && report.Coverage.SignedRecords == 1 && report.Coverage.UnsignedRecords == 0 && report.Signatures != nil && report.Signatures.KeysSupplied == 1 && report.Signatures.FirstKey == key.KeyID {
			result.Status = "verified"
			result.PublicKey = key.PublicKey
			return result
		}
	}
	return result
}
