package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func portableCustodyFixture(t *testing.T, memory bool) (*Server, *os.Root, researchRunStore, inputResearchRun) {
	t.Helper()
	dir := t.TempDir()
	project, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { project.Close() })
	s := &Server{root: project, projectDir: dir}
	if memory {
		s.retainedArtifacts = newMemoryArtifacts()
		s.researchJournals = newMemoryResearchJournals()
	}
	run := portableAdmissionRun()
	run.Gateway = inputResearchPin{Authority: "gateway:test", Public: testSignerPublic}
	run.Portable.Frozen.Profiles[0].Profile = json.RawMessage(`{"id":"answer","source":"` + modelSourceName + `","class":"generated","shape":"mcp","endpoint":"fixture:model","research":{"capabilities":["answer"]}}`)
	run.Portable.Frozen.Profiles[1].Profile = json.RawMessage(`{"id":"page","class":"generated","source":"web","shape":"http","endpoint":"https://legacy.example/final","adapter":{"name":"adapter-web"},"research":{"capabilities":["page"]}}`)
	run.Revision = portableResearchRevision(run.Portable.Frozen)
	runs, err := s.inputResearchRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runs.Close() })
	root, err := openInputResearch(runs, run.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	store := s.researchRunStore(root, run.ID)
	if err = store.journal.Create(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	return s, root, store, run
}

// All fixtures are hand-authored. The whitespace, escape spelling, receipt,
// salts and maximum-size padding must survive custody byte-for-byte.
func portableCustodyEnvelope(t *testing.T, size int, endpoint string) json.RawMessage {
	t.Helper()
	prefix := ` { "receipt" : {"acquisition":{"endpoint":"` + endpoint + `"},"signature":"kept-\u0061"}, "salts" : {"args":"raw-salt"}, "result" : {"text":"oversize-body-marker \u0061 `
	suffix := "\"}}\n"
	if size < len(prefix)+len(suffix) {
		t.Fatal("fixture size too small")
	}
	return json.RawMessage(prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix)
}
func portableCustodyRequest(t *testing.T, size int) json.RawMessage {
	t.Helper()
	prefix := `{ "url" : "https://legacy.example/final?raw=%2F", "padding" : "`
	suffix := `" }`
	return json.RawMessage(prefix + strings.Repeat("p", size-len(prefix)-len(suffix)) + suffix)
}
func acquireCustodyFixture(t *testing.T, store researchRunStore, run inputResearchRun, request, raw json.RawMessage, calls *int) error {
	t.Helper()
	profile, err := portableResearchProfileFor(run, "page")
	if err != nil {
		t.Fatal(err)
	}
	_, err = researchAttempt(t.Context(), store, "page", request, func(ctx context.Context, current inputResearchRun, a inputResearchAttempt) (json.RawMessage, error) {
		*calls++
		return retainPortableAcquisitionWithCustody(ctx, store, current, a, "page", profile, request, raw)
	})
	return err
}

func TestPortableOversizedAcquisitionCustody(t *testing.T) {
	for _, memory := range []bool{false, true} {
		for _, refused := range []bool{false, true} {
			for _, size := range []int{(2 << 20) + 1, inputResearchSourceResponseLimit} {
				t.Run(fmt.Sprintf("memory-%v/refused-%v/bytes-%d", memory, refused, size), func(t *testing.T) {
					s, _, store, run := portableCustodyFixture(t, memory)
					endpoint := "https://legacy.example/final"
					if refused {
						endpoint = "https://outside.example/redirected"
					}
					raw := portableCustodyEnvelope(t, size, endpoint)
					request := portableCustodyRequest(t, backgroundModelResponseBytes)
					calls := 0
					err := acquireCustodyFixture(t, store, run, request, raw, &calls)
					var refusal *researchAcquisitionRefused
					if !errors.As(err, &refusal) || refusal.proof == nil {
						t.Fatal("known complete acquisition not retained/refused", err)
					}
					held, err := store.read()
					if err != nil {
						t.Fatal(err)
					}
					a := held.Attempts[0]
					code := "source-response-too-large"
					problem := errPortableResearchAcquisitionSize.Error()
					if refused {
						code = "acquisition-invalid"
						problem = errScopedPageReceipt.Error()
					}
					if held.State != "partial" || held.Problem != errPortableResearchAcquisitionSize.Error() || a.State != "refused" || a.ProblemCode != code || a.Problem != problem || a.FinishedAt == "" || a.ReservedBytes != 0 || len(a.Result) != 0 {
						t.Fatal("terminal custody decision lost", held.State, a.State, a.ProblemCode, a.Problem)
					}
					saved, err := store.loadPortableProof(t.Context(), held, a)
					if err != nil {
						t.Fatal(err)
					}
					want := sha256.Sum256(raw)
					if !bytes.Equal(saved.Proof.Response, raw) || !bytes.Equal(saved.Proof.Arguments, request) || a.RetainedProof.Bytes != saved.Bytes || held.RetrievedBytes != saved.Bytes {
						t.Fatal("exact raw evidence or accounting changed")
					}
					var manifest storedResearchProof
					if decodeDataJSON(saved.Raw, &manifest) != nil || manifest.Response.SHA256 != hex.EncodeToString(want[:]) || manifest.Response.Bytes != size || manifest.Arguments.Bytes != backgroundModelResponseBytes {
						t.Fatal("manifest does not carry exact content identities")
					}
					prompt, err := s.portableResearchPrompt(held)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(prompt, "oversize-body-marker") || strings.Contains(prompt, "kept-\\u0061") || len(held.Portable.Response) != 0 {
						t.Fatal("refused content reached model or answer")
					}
					models := 0
					_, err = researchAttempt(t.Context(), store, "model", json.RawMessage(`{}`), func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
						models++
						return nil, nil
					})
					if err == nil || models != 0 {
						t.Fatal("terminal storage decision dispatched another model")
					}
					if err = acquireCustodyFixture(t, store, run, request, raw, &calls); err == nil || calls != 1 {
						t.Fatal("observing custody reacquired source")
					}
					recovered, err := store.read()
					if err != nil {
						t.Fatal(err)
					}
					recoverInputResearch(&recovered)
					if recovered.State != "partial" || recovered.Attempts[0].RetainedProof == nil {
						t.Fatal("reload lost terminal proof descriptor")
					}
				})
			}
		}
	}
}

func TestPortableCustodyMissingTamperedAndWrongIdentity(t *testing.T) {
	s, root, store, run := portableCustodyFixture(t, true)
	request := json.RawMessage(`{"url":"https://legacy.example/final"}`)
	raw := portableCustodyEnvelope(t, (2<<20)+1, "https://outside.example/redirected")
	calls := 0
	if err := acquireCustodyFixture(t, store, run, request, raw, &calls); err == nil {
		t.Fatal("oversized response not refused")
	}
	held, err := store.read()
	if err != nil {
		t.Fatal(err)
	}
	a := held.Attempts[0]
	saved, err := store.loadPortableProof(t.Context(), held, a)
	if err != nil {
		t.Fatal(err)
	}
	// Consumer-side checks are independent of the repository implementation.
	originalLoad := store.loadProof
	for _, edit := range []func(*researchProofSnapshot){
		func(p *researchProofSnapshot) { p.Proof.Session = "another-session" },
		func(p *researchProofSnapshot) { p.Proof.Pin.Public = "another-pin" },
		func(p *researchProofSnapshot) { p.Proof.Source = "another-source" },
		func(p *researchProofSnapshot) { p.Raw = append(p.Raw, ' ') },
		func(p *researchProofSnapshot) { p.Bytes++ },
		func(p *researchProofSnapshot) { p.Proof.Response = json.RawMessage(`{}`) },
		func(p *researchProofSnapshot) { p.Proof.Arguments = json.RawMessage(`{}`) },
	} {
		store.loadProof = func(context.Context, string) (researchProofSnapshot, error) {
			p := saved
			p.Raw = bytes.Clone(saved.Raw)
			edit(&p)
			return p, nil
		}
		if _, err = store.loadPortableProof(t.Context(), held, a); err == nil {
			t.Fatal("changed custody identity accepted")
		}
	}
	store.loadProof = originalLoad
	manifest := saved.Raw
	if err = root.Remove(a.ID + ".proof.json"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.read(); !errors.Is(err, errPortableResearchProofUnavailable) {
		t.Fatal("missing manifest accepted", err)
	}
	if err = writePrivateData(root, a.ID+".proof.json", manifest); err != nil {
		t.Fatal(err)
	}
	var metadata storedResearchProof
	if err = decodeDataJSON(manifest, &metadata); err != nil {
		t.Fatal(err)
	}
	// Missing/tampered actual objects fail at the repository and consumer boundary.
	memory := s.retainedArtifacts.(*memoryArtifacts)
	original := bytes.Clone(memory.data[metadata.Response.SHA256])
	for _, tamper := range []bool{false, true} {
		if tamper {
			memory.data[metadata.Response.SHA256] = []byte(`{}`)
		} else {
			delete(memory.data, metadata.Response.SHA256)
		}
		if _, err = store.read(); !errors.Is(err, errPortableResearchProofUnavailable) || calls != 1 {
			t.Fatal("missing/tampered object caused replay or success", err)
		}
	}
	memory.data[metadata.Response.SHA256] = original
	if _, err = store.read(); err != nil {
		t.Fatal(err)
	}
	changed := a
	copyRef := *a.RetainedProof
	changed.RetainedProof = &copyRef
	changed.RetainedProof.Profile = "another-profile"
	if _, err = store.loadPortableProof(t.Context(), held, changed); err == nil {
		t.Fatal("another profile accepted the proof")
	}

	if _, err = store.journal.Update(t.Context(), func(r *inputResearchRun) error { r.Attempts[0].RetainedProof.Bytes--; return nil }); err == nil {
		t.Fatal("terminal proof descriptor was mutable")
	}
}

func TestPortableCustodyDescriptorMalformed(t *testing.T) {
	_, _, store, run := portableCustodyFixture(t, true)
	request := json.RawMessage(`{"url":"https://legacy.example/final"}`)
	calls := 0
	_ = acquireCustodyFixture(t, store, run, request, portableCustodyEnvelope(t, (2<<20)+1, "https://legacy.example/final"), &calls)
	held, err := store.read()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := portableResearchJSON(held)
	if err != nil {
		t.Fatal(err)
	}
	binding := `"profile":"page","profileDigest":"sha256:` + strings.Repeat("a", 64) + `",`
	for _, descriptor := range []string{`null`, `{}`, `{` + binding + `"version":2,"manifest":{"sha256":"` + strings.Repeat("a", 64) + `","bytes":10},"bytes":10}`, `{` + binding + `"version":1,"manifest":{"sha256":"../x","bytes":10},"bytes":10}`, `{` + binding + `"version":1,"manifest":{"sha256":"` + strings.Repeat("a", 64) + `","bytes":8193},"bytes":10}`, `{` + binding + `"version":1,"manifest":{"sha256":"` + strings.Repeat("a", 64) + `","bytes":10},"bytes":-1}`, `{` + binding + `"version":1,"manifest":{"sha256":"` + strings.Repeat("a", 64) + `","bytes":10},"bytes":10,"unknown":true}`} {
		var document map[string]json.RawMessage
		if err = json.Unmarshal(raw, &document); err != nil {
			t.Fatal(err)
		}
		var attempts []map[string]json.RawMessage
		if err = json.Unmarshal(document["attempts"], &attempts); err != nil {
			t.Fatal(err)
		}
		attempts[0]["retainedProof"] = json.RawMessage(descriptor)
		document["attempts"], _ = json.Marshal(attempts)
		malformed, _ := json.Marshal(document)
		if _, err = decodeResearchJournal(malformed, held.ID); err == nil {
			t.Fatal("malformed descriptor accepted", descriptor)
		}
	}
	for _, edit := range []func(*inputResearchRun){
		func(r *inputResearchRun) { r.RetrievedBytes = 0 },
		func(r *inputResearchRun) { r.Attempts[0].RetainedProof = nil },
		func(r *inputResearchRun) { r.Attempts[0].RetainedProof.Profile = "another-profile" },
		func(r *inputResearchRun) { r.Attempts[0].FinishedAt = "invalid" },
	} {
		copy, err := decodeResearchJournal(raw, held.ID)
		if err != nil {
			t.Fatal(err)
		}
		edit(&copy)
		malformed, err := portableResearchJSON(copy)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = decodeResearchJournal(malformed, held.ID); err == nil {
			t.Fatal("malformed custody state/accounting accepted")
		}
	}

}

func TestPortableCustodyProjectSnapshotCopyAndMove(t *testing.T) {
	s, _, store, run := portableCustodyFixture(t, false)
	request := json.RawMessage(`{"url":"https://legacy.example/final"}`)
	raw := portableCustodyEnvelope(t, (2<<20)+1, "https://outside.example/redirected")
	calls := 0
	_ = acquireCustodyFixture(t, store, run, request, raw, &calls)
	held, err := store.read()
	if err != nil {
		t.Fatal(err)
	}
	a := held.Attempts[0]
	saved, err := store.loadPortableProof(t.Context(), held, a)
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err = s.copyResearchProof(t.Context(), target, a.ID, saved); err != nil {
		t.Fatal(err)
	}
	copied, err := s.loadResearchProof(t.Context(), target, a.ID)
	if err != nil || !bytes.Equal(copied.Raw, saved.Raw) || !bytes.Equal(copied.Proof.Response, raw) {
		t.Fatal("proof copy broke closure", err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	if err = os.CopyFS(snapshot, os.DirFS(s.projectDir)); err != nil {
		t.Fatal(err)
	}
	// A project snapshot preserves private permissions too. CopyFS deliberately
	// normalizes file modes, so restore the source's mode bits in this fixture.
	if err = filepath.WalkDir(s.projectDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(s.projectDir, path)
		if err != nil {
			return err
		}
		return os.Chmod(filepath.Join(snapshot, relative), info.Mode().Perm())
	}); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	if err = os.Rename(snapshot, moved); err != nil {
		t.Fatal(err)
	}
	project, err := os.OpenRoot(moved)
	if err != nil {
		t.Fatal(err)
	}
	defer project.Close()
	restored := &Server{root: project, projectDir: moved}
	rr, err := project.OpenRoot(inputResearchDirectory + "/" + run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Close()
	read, err := restored.researchRunStore(rr, run.ID).read()
	if err != nil {
		t.Fatal(err)
	}
	proof, err := restored.loadResearchProof(t.Context(), rr, read.Attempts[0].ID)
	if err != nil || !bytes.Equal(proof.Raw, saved.Raw) || !bytes.Equal(proof.Proof.Response, raw) {
		t.Fatal("project snapshot/move lost evidence closure", err)
	}
}

func TestPortableCustodyCapacityBeforeDispatch(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(fmt.Sprint("memory-", memory), func(t *testing.T) {
			_, _, store, run := portableCustodyFixture(t, memory)
			raw := portableCustodyEnvelope(t, inputResearchSourceResponseLimit, "https://outside.example/large")
			calls := 0
			for i := 0; i < 10; i++ {
				// Explicit host attempts with distinct arguments exercise the cumulative
				// guard; the production worker stops at the first oversized acquisition.
				if _, err := store.change(func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
					t.Fatal(err)
				}
				request := json.RawMessage(fmt.Sprintf(`{"url":"https://legacy.example/final?attempt=%d"}`, i))
				err := acquireCustodyFixture(t, store, run, request, raw, &calls)
				if errors.Is(err, errPortableResearchStorageLimit) {
					break
				}
				var refusal *researchAcquisitionRefused
				if !errors.As(err, &refusal) || refusal.proof == nil {
					t.Fatal(err)
				}
			}
			held, err := store.read()
			if err != nil {
				t.Fatal(err)
			}
			if calls != 3 || held.State != "partial" || held.Problem != errPortableResearchStorageLimit.Error() || len(held.Attempts) != 3 || held.RetrievedBytes > inputResearchRetainedByteLimit {
				t.Fatal("retained-byte reservation did not stop dispatch", calls, held.State, held.Problem)
			}
			encoded, err := portableResearchJSON(held)
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded)+held.RetrievedBytes > inputResearchRetainedByteLimit {
				t.Fatal("proofs plus journal exceeded total storage")
			}
			for _, a := range held.Attempts {
				if a.State != "refused" || a.ReservedBytes != 0 {
					t.Fatal("capacity left a nonterminal attempt")
				}
			}
			t.Logf("capacity stopped after %d maximum acquisitions; accounted proof bytes=%d journal bytes=%d", calls, held.RetrievedBytes, len(encoded))
		})
	}
}

func TestPortableCustodyStorageUncertaintyNeverReplays(t *testing.T) {
	for _, failure := range []string{"artifact", "manifest", "journal"} {
		t.Run(failure, func(t *testing.T) {
			s, root, store, run := portableCustodyFixture(t, true)
			raw := portableCustodyEnvelope(t, (2<<20)+1, "https://outside.example/large")
			request := json.RawMessage(`{"url":"https://legacy.example/final"}`)
			calls := 0
			if failure == "artifact" {
				s.retainedArtifacts = &failArtifactPut{retainedArtifactRepository: s.retainedArtifacts}
			}
			if failure == "manifest" {
				testHookPrivateRead = func(name string) error {
					if strings.HasSuffix(name, ".proof.json") {
						return errors.New("manifest read-back unavailable")
					}
					return nil
				}
				t.Cleanup(func() { testHookPrivateRead = nil })
			}
			if failure == "journal" {
				base := store.journal
				store.journal = portableCustodyFailTerminalJournal{base}
			}
			err := acquireCustodyFixture(t, store, run, request, raw, &calls)
			testHookPrivateRead = nil
			if err == nil {
				t.Fatal("uncertain storage was acknowledged")
			}
			held, readErr := store.journal.Read(t.Context())
			if readErr != nil {
				t.Fatal(readErr)
			}
			if held.Attempts[0].State != "uncertain" && held.Attempts[0].State != "started" {
				t.Fatal("storage failure fabricated a terminal known acquisition", held.Attempts[0].State)
			}
			if failure != "journal" && held.RetrievedBytes < inputResearchSourceProofLimit {
				t.Fatal("uncertain artifacts were not conservatively charged")
			}
			if failure == "artifact" {
				broken := s.retainedArtifacts.(*failArtifactPut)
				kept, loadErr := broken.retainedArtifactRepository.Get(t.Context(), artifactReference(raw))
				if loadErr != nil || !bytes.Equal(kept, raw) || broken.calls != 1 {
					t.Fatal("uncertain artifact bytes were discarded or retried", loadErr)
				}
			}
			if failure == "journal" || failure == "manifest" {
				proof, loadErr := s.loadResearchProof(t.Context(), root, held.Attempts[0].ID)
				if loadErr != nil || !bytes.Equal(proof.Proof.Response, raw) {
					t.Fatal("journal failure discarded committed proof", loadErr)
				}
			}
			recoverInputResearch(&held)
			if held.State != "interrupted" || held.Attempts[0].State != "uncertain" || held.Attempts[0].RetainedProof != nil {
				t.Fatal("recovery fabricated custody")
			}
			if again := acquireCustodyFixture(t, store, run, request, raw, &calls); again == nil || calls != 1 {
				t.Fatal("storage uncertainty replayed source", again, calls)
			}
		})
	}
}

type portableCustodyFailTerminalJournal struct{ researchJournal }

func (j portableCustodyFailTerminalJournal) Update(ctx context.Context, change func(*inputResearchRun) error) (inputResearchRun, error) {
	return j.researchJournal.Update(ctx, func(r *inputResearchRun) error {
		if err := change(r); err != nil {
			return err
		}
		for _, a := range r.Attempts {
			if a.RetainedProof != nil {
				return errors.New("terminal journal write unavailable")
			}
		}
		return nil
	})
}

func TestPortableCustodyDescriptorContractVector(t *testing.T) {
	vector := json.RawMessage(`{"version":1,"profile":"page","profileDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","manifest":{"sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","bytes":512},"bytes":2099000}`)
	var ref portableResearchProofReference
	if err := decodeDataJSON(vector, &ref); err != nil || !ref.valid() {
		t.Fatal("hand-authored descriptor vector rejected", err)
	}
	for _, member := range []string{"version", "profile", "profileDigest", "manifest", "bytes"} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(vector, &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, member)
		malformed, _ := json.Marshal(fields)
		var missing portableResearchProofReference
		if decodeDataJSON(malformed, &missing) == nil && missing.valid() {
			t.Fatal("missing descriptor member accepted", member)
		}
	}
	for _, member := range []string{"sha256", "bytes"} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(vector, &fields); err != nil {
			t.Fatal(err)
		}
		var manifest map[string]json.RawMessage
		if err := json.Unmarshal(fields["manifest"], &manifest); err != nil {
			t.Fatal(err)
		}
		delete(manifest, member)
		fields["manifest"], _ = json.Marshal(manifest)
		malformed, _ := json.Marshal(fields)
		var missing portableResearchProofReference
		if decodeDataJSON(malformed, &missing) == nil && missing.valid() {
			t.Fatal("missing manifest member accepted", member)
		}
	}
}

func TestPortableCustodyOversizedModelAcquisitionIsTerminal(t *testing.T) {
	_, _, store, run := portableCustodyFixture(t, true)
	profile, err := portableResearchProfileFor(run, "answer")
	if err != nil {
		t.Fatal(err)
	}
	raw := portableCustodyEnvelope(t, (2<<20)+1, "fixture:model")
	_, err = researchAttempt(t.Context(), store, "model", json.RawMessage(`{"step":1}`), func(ctx context.Context, current inputResearchRun, a inputResearchAttempt) (json.RawMessage, error) {
		return retainPortableAcquisitionWithCustody(ctx, store, current, a, "answer", profile, json.RawMessage(`{"request":"reviewed"}`), raw)
	})
	var refusal *researchAcquisitionRefused
	if !errors.As(err, &refusal) || refusal.proof == nil {
		t.Fatal("complete model acquisition lost custody", err)
	}
	held, err := store.read()
	if err != nil {
		t.Fatal(err)
	}
	activity := portableResearchActivity(held)
	if held.State != "partial" || len(activity.Steps) != 1 || activity.Steps[0].State != "refused" || activity.Steps[0].ProblemCode != "source-response-too-large" || activity.SourceCount != 0 {
		t.Fatal("existing activity contract lost terminal custody decision")
	}
}

func TestPortableCustodyRetainedMetadataExceedsInlineLimit(t *testing.T) {
	_, _, store, run := portableCustodyFixture(t, true)
	request := portableCustodyRequest(t, backgroundModelResponseBytes)
	raw := portableCustodyEnvelope(t, (2<<20)-(64<<10), "https://legacy.example/final")
	calls := 0
	err := acquireCustodyFixture(t, store, run, request, raw, &calls)
	var refusal *researchAcquisitionRefused
	if !errors.As(err, &refusal) || refusal.proof == nil {
		t.Fatal("request/envelope metadata escaped retained custody bound", err)
	}
	held, err := store.read()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.loadPortableProof(t.Context(), held, held.Attempts[0])
	if err != nil || !bytes.Equal(saved.Proof.Response, raw) || !bytes.Equal(saved.Proof.Arguments, request) {
		t.Fatal("combined metadata custody changed bytes", err)
	}
}
