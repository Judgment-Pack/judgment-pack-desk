package desk

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Exercise concurrent HTTP acquisition, independent signed sessions, proof
// storage and the installed verifier without any website or provider traffic.
func TestResearchCandidatePagesVerifyIndependentProofs(t *testing.T) {
	program := os.Getenv("JPS_TEST_GATEWAY")
	if program == "" {
		t.Skip("set JPS_TEST_GATEWAY to the local verifier")
	}
	root, run := candidateFixture(t, 3)
	_, fixture, pin := handoffProofFixture(t, program)
	var original inputResearchProof
	if err := json.Unmarshal(fixture, &original); err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	var seals sync.Map
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Session   string          `json:"session"`
			Source    string          `json:"source"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		if strings.HasSuffix(r.URL.Path, "seal") {
			if seal, ok := seals.Load(request.Session); ok {
				w.Write(seal.([]byte))
				return
			}
			http.Error(w, "unknown session", 400)
			return
		}
		canon := func(v any) []byte {
			raw, err := json.Marshal(v)
			if err != nil {
				t.Error(err)
				return nil
			}
			out, err := researchCanon(r.Context(), program, raw)
			if err != nil {
				t.Error(err)
			}
			return out
		}
		var args struct {
			URL string `json:"url"`
		}
		json.Unmarshal(request.Arguments, &args)
		var envelope map[string]json.RawMessage
		json.Unmarshal(original.Response, &envelope)
		var receipt map[string]any
		json.Unmarshal(envelope["receipt"], &receipt)
		var result map[string]any
		json.Unmarshal(envelope["result"], &result)
		source := result["provenance"].(map[string]any)["source"].(map[string]any)
		source["url"] = args.URL
		source["requestedUrl"] = args.URL
		var salts map[string]string
		json.Unmarshal(envelope["salts"], &salts)
		salt, _ := hex.DecodeString(salts["args"])
		receipt["sessionId"] = request.Session
		receipt["argumentsCommitment"] = sha256Digest(append(append(salt, []byte("args:")...), canon(request.Arguments)...))
		receipt["resultDigest"] = sha256Digest(canon(result))
		receipt["acquisition"].(map[string]any)["endpoint"] = args.URL
		delete(receipt, "signature")
		receipt["signature"] = hex.EncodeToString(ed25519.Sign(key, append([]byte("judgment-pack-gateway/receipt/3:"), canon(receipt)...)))
		var seal map[string]any
		json.Unmarshal(original.Seal, &seal)
		seal["sessionId"] = request.Session
		delete(seal, "signature")
		seal["signature"] = hex.EncodeToString(ed25519.Sign(key, append([]byte("judgment-pack-gateway/seal/2:"), canon(seal)...)))
		if _, loaded := seals.LoadOrStore(request.Session, canon(seal)); loaded {
			t.Error("session was reused")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(canon(map[string]any{"receipt": receipt, "result": result, "salts": salts}))
	}))
	defer server.Close()
	s, _ := newTestServer(t, false)
	s.cfg.LocalGatewayBundle = filepath.Dir(program)
	writeDeskConfig(t, s, strings.Replace(researchDeskFile(server.URL), testSignerPublic, pin.Public, 1))
	run.Gateway = pin
	if err := writeInputResearch(root, run); err != nil {
		t.Fatal(err)
	}
	ops := recoveryOperations()
	ops.source = s.acquireResearchSource
	if err := readResearchCandidateBatch(context.Background(), root, ops); err != nil {
		t.Fatal(err)
	}
	saved, err := readInputResearch(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Sources) != 3 || researchReservedBytes(saved) != 0 {
		t.Fatal("parallel acquisition lost evidence")
	}
	bytes := 0
	for _, a := range saved.Attempts {
		raw, err := readPrivateData(root, a.ID+".proof.json", inputResearchSourceProofLimit)
		if err != nil {
			t.Fatal(err)
		}
		var proof inputResearchProof
		if err = json.Unmarshal(raw, &proof); err != nil {
			t.Fatal(err)
		}
		if _, _, err = verifyResearchProof(context.Background(), program, proof); err != nil {
			t.Fatal("saved proof does not independently verify", err)
		}
		bytes += len(raw) + len(a.Result)
	}
	if saved.RetrievedBytes != bytes {
		t.Fatal("retained bytes do not match independent proofs", saved.RetrievedBytes, bytes)
	}
}
