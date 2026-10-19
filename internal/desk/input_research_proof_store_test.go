package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func proofFixture() inputResearchProof {
	return inputResearchProof{Session: "session-1", Source: "web", Pin: inputResearchPin{Authority: "gateway:test", Public: testSignerPublic}, Arguments: json.RawMessage(" {\"url\" : \"https://example.com/\"}\n"), Response: json.RawMessage("\n{\"result\" : {\"text\":\"\\u0061\"}}\n")}
}
func TestResearchProofImmutableLifecycle(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			s, _ := newTestServer(t, false)
			if backend == "memory" {
				s.retainedArtifacts = newMemoryArtifacts()
			}
			root, _ := researchRunFixture(t)
			id := "attempt_" + strings.Repeat("a", 64)
			p := proofFixture()
			size, err := s.saveResearchProof(t.Context(), root, id, p, 0)
			if err != nil {
				t.Fatal(err)
			}
			first, err := s.loadResearchProof(t.Context(), root, id)
			if err != nil || !sameResearchAcquisition(first.Proof, p) || len(first.Proof.Seal) != 0 || first.Bytes != size {
				t.Fatal("acquisition changed", err)
			}
			for _, field := range []string{"arguments", "response", "pin", "session", "source"} {
				changed := p
				switch field {
				case "arguments":
					changed.Arguments = json.RawMessage(`{}`)
				case "response":
					changed.Response = json.RawMessage(`{}`)
				case "pin":
					changed.Pin.Public = "other"
				case "session":
					changed.Session = "another"
				case "source":
					changed.Source = "web-search"
				}
				if _, err = s.saveResearchProof(t.Context(), root, id, changed, 0); !errors.Is(err, errResearchProofConflict) {
					t.Fatal("acquisition replaced", field, err)
				}
			}
			p.Seal = json.RawMessage(" {\"sealed\" : true}\n")
			finalSize, err := s.saveResearchProof(t.Context(), root, id, p, 0)
			if err != nil || finalSize <= size {
				t.Fatal(finalSize, err)
			}
			sealed, err := s.loadResearchProof(t.Context(), root, id)
			if err != nil || !sameResearchAcquisition(sealed.Proof, p) || !bytes.Equal(sealed.Proof.Seal, p.Seal) {
				t.Fatal("seal lost exact acquisition", err)
			}
			if repeated, err := s.saveResearchProof(t.Context(), root, id, p, 0); err != nil || repeated != finalSize {
				t.Fatal("repeated seal", err)
			}
			for _, seal := range []json.RawMessage{nil, json.RawMessage(`{"sealed":false}`)} {
				changed := p
				changed.Seal = seal
				if _, err = s.saveResearchProof(t.Context(), root, id, changed, 0); !errors.Is(err, errResearchProofConflict) {
					t.Fatal("seal replaced", err)
				}
			}
			if n, err := retainedResearchProofSize(root, id); err != nil || n != finalSize {
				t.Fatal("failed-source accounting", n, err)
			}
			target, _ := researchRunFixture(t)
			if err = s.copyResearchProof(t.Context(), target, id, sealed); err != nil {
				t.Fatal(err)
			}
			copied, err := s.loadResearchProof(t.Context(), target, id)
			if err != nil || !bytes.Equal(copied.Raw, sealed.Raw) {
				t.Fatal("copy changed proof identity", err)
			}
			if backend == "memory" {
				assertNoArtifactFiles(t, s)
			}
		})
	}
}
func TestResearchProofLegacyReadAndCopyDoNotMigrate(t *testing.T) {
	s, _ := newTestServer(t, false)
	root, _ := researchRunFixture(t)
	id := "attempt_" + strings.Repeat("b", 64)
	p := proofFixture()
	p.Seal = json.RawMessage(`{"sealed":true}`)
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err = writePrivateData(root, id+".proof.json", raw); err != nil {
		t.Fatal(err)
	}
	saved, err := s.loadResearchProof(t.Context(), root, id)
	if err != nil || !bytes.Equal(saved.Raw, raw) || saved.Bytes != len(raw) {
		t.Fatal("legacy proof changed", err)
	}
	target, _ := researchRunFixture(t)
	if err = s.copyResearchProof(t.Context(), target, id, saved); err != nil {
		t.Fatal(err)
	}
	copied, err := readPrivateData(target, id+".proof.json", inputResearchSourceProofLimit)
	if err != nil || !bytes.Equal(copied, raw) {
		t.Fatal("legacy digest changed", err)
	}
	assertNoArtifactFiles(t, s)
}
func TestResearchProofMissingObjectCannotBeRepairedOrCopied(t *testing.T) {
	s, _ := newTestServer(t, false)
	memory := newMemoryArtifacts()
	s.retainedArtifacts = memory
	root, _ := researchRunFixture(t)
	target, _ := researchRunFixture(t)
	id := "attempt_" + strings.Repeat("c", 64)
	p := proofFixture()
	if _, err := s.saveResearchProof(t.Context(), root, id, p, 0); err != nil {
		t.Fatal(err)
	}
	saved, err := s.loadResearchProof(t.Context(), root, id)
	if err != nil {
		t.Fatal(err)
	}
	delete(memory.data, artifactReference(p.Response).SHA256)
	if _, err = s.loadResearchProof(t.Context(), root, id); err == nil {
		t.Fatal("missing response accepted")
	}
	p.Seal = json.RawMessage(`{}`)
	if _, err = s.saveResearchProof(t.Context(), root, id, p, 0); err == nil {
		t.Fatal("missing acquisition repaired by seal")
	}
	if err = s.copyResearchProof(t.Context(), target, id, saved); err == nil {
		t.Fatal("incomplete evidence copied")
	}
	if _, err = target.Stat(id + ".proof.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("incomplete evidence made visible", err)
	}
	prior, err := readPrivateData(root, id+".proof.json", inputResearchSourceProofLimit)
	if err != nil || !bytes.Equal(prior, saved.Raw) {
		t.Fatal("missing artifact changed original manifest", err)
	}
}
func TestResearchProofManifestWriteUncertaintyKeepsAcquisition(t *testing.T) {
	s, _ := newTestServer(t, false)
	s.retainedArtifacts = newMemoryArtifacts()
	root, _ := researchRunFixture(t)
	id := "attempt_" + strings.Repeat("d", 64)
	p := proofFixture()
	reads := 0
	testHookPrivateRead = func(name string) error {
		if name == id+".proof.json" {
			reads++
			return errors.New("manifest read-back unavailable")
		}
		return nil
	}
	defer func() { testHookPrivateRead = nil }()
	if n, err := s.saveResearchProof(t.Context(), root, id, p, 0); err == nil || n != 0 || reads != 1 {
		t.Fatal("uncertain commit acknowledged or retried", n, err, reads)
	}
	testHookPrivateRead = nil
	saved, err := s.loadResearchProof(t.Context(), root, id)
	if err != nil || !sameResearchAcquisition(saved.Proof, p) {
		t.Fatal("retained acquisition lost", err)
	}
}

type failArtifactPut struct {
	retainedArtifactRepository
	calls int
}

func (f *failArtifactPut) Put(ctx context.Context, raw []byte) (retainedArtifactRef, error) {
	f.calls++
	if _, err := f.retainedArtifactRepository.Put(ctx, raw); err != nil {
		return retainedArtifactRef{}, err
	}
	return retainedArtifactRef{}, errors.New("artifact acknowledgement unavailable")
}
func TestResearchAcquisitionRetentionFailuresNeverReplay(t *testing.T) {
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if bundle == "" {
		t.Skip("requires installed Gateway verifier")
	}
	for _, failure := range []string{"artifact", "seal"} {
		t.Run(failure, func(t *testing.T) {
			requests := 0
			response := []byte(" \n{\"result\" : {\"text\":\"retained raw response\"}}\n")
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if strings.HasSuffix(r.URL.Path, "seal") {
					http.Error(w, "seal unavailable", 500)
					return
				}
				w.Write(response)
			}))
			defer gateway.Close()
			s, _ := newTestServer(t, false)
			s.cfg.LocalGatewayBundle = bundle
			writeDeskConfig(t, s, researchDeskFile(gateway.URL))
			memory := newMemoryArtifacts()
			s.retainedArtifacts = memory
			broken := &failArtifactPut{retainedArtifactRepository: memory}
			if failure == "artifact" {
				s.retainedArtifacts = broken
			}
			root, run := researchRunFixture(t)
			run.Gateway = inputResearchPin{Authority: "gateway:test", Public: testSignerPublic}
			if err := writeInputResearch(root, run); err != nil {
				t.Fatal(err)
			}
			args := json.RawMessage(`{"url":"https://example.com/"}`)
			call := func(ctx context.Context, current inputResearchRun, a inputResearchAttempt) (json.RawMessage, error) {
				source, err := s.acquireResearchSource(ctx, root, current, a, args)
				if err != nil {
					return nil, err
				}
				return json.Marshal(source)
			}
			if _, err := researchAttempt(t.Context(), fileResearchStore(root), "page", args, call); err == nil {
				t.Fatal("failed persistence or seal acknowledged")
			}
			count := requests
			if _, err := researchAttempt(t.Context(), fileResearchStore(root), "page", args, call); err == nil || requests != count {
				t.Fatal("uncertain acquisition replayed", requests, err)
			}
			saved, err := readInputResearch(root)
			if err != nil || len(saved.Attempts) != 1 || saved.Attempts[0].State != "uncertain" || len(saved.Sources) != 0 {
				t.Fatal("failure published evidence", err)
			}
			if failure == "artifact" {
				got, err := memory.Get(t.Context(), artifactReference(response))
				if err != nil || !bytes.Equal(got, response) {
					t.Fatal("uncertain artifact discarded source bytes", err)
				}
				if requests != 1 || broken.calls != 1 {
					t.Fatal("seal sent after failed artifact persistence", requests, broken.calls)
				}
			} else {
				proof, err := s.loadResearchProof(t.Context(), root, saved.Attempts[0].ID)
				if requests != 2 || err != nil || !bytes.Equal(proof.Proof.Response, response) || len(proof.Proof.Seal) != 0 || saved.RetrievedBytes != proof.Bytes {
					t.Fatal("seal failure lost response or byte accounting", requests, err)
				}
			}
			assertNoArtifactFiles(t, s)
		})
	}
}

func TestResearchProofObjectSurvivesProjectRename(t *testing.T) {
	s, _ := newTestServer(t, false)
	repo := s.artifactRepository()
	raw := []byte("retained alongside project")
	ref, err := repo.Put(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	// The project's move protocol renames the complete project. Artifacts must
	// remain inside that root and must not persist an absolute location in refs.
	moved := filepath.Join(t.TempDir(), "moved")
	if err = os.Rename(s.projectDir, moved); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(t.Context(), ref)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("project-relative evidence was stranded", err)
	}
}

type unreadableArtifacts struct{ retainedArtifactRepository }

func (r unreadableArtifacts) Get(context.Context, retainedArtifactRef) ([]byte, error) {
	return nil, errors.New("artifact unavailable")
}
func TestResearchProofDoesNotPublishUnreadableArtifacts(t *testing.T) {
	s, _ := newTestServer(t, false)
	s.retainedArtifacts = unreadableArtifacts{newMemoryArtifacts()}
	root, _ := researchRunFixture(t)
	id := "attempt_" + strings.Repeat("e", 64)
	if _, err := s.saveResearchProof(t.Context(), root, id, proofFixture(), 0); err == nil {
		t.Fatal("unreadable artifacts acknowledged")
	}
	if _, err := root.Stat(id + ".proof.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("published unreadable artifacts", err)
	}
}
