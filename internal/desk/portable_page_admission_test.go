package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Hand-authored journal and acquisition fixtures: no companion or model process
// supplies the expected endpoints, receipts, journal bytes or terminal states.
func portableAdmissionRun() inputResearchRun {
	const now = "2026-10-23T00:00:00Z"
	digest := "sha256:" + strings.Repeat("a", 64)
	frozen := portableResearchFrozen{
		Input:  json.RawMessage(`{"source":{"mapping":{"sources":[{"name":"research","profile":"answer","profileDigest":"` + digest + `","webProfiles":{"page":{"profile":"page","profileDigest":"` + digest + `"}}}]}}}`),
		Plan:   portableResearchPlan{Name: "research", Kind: "research", Profile: "answer", Source: modelSourceName, Arguments: json.RawMessage(`{"capabilities":["answer","page"],"fields":[]}`)},
		Choice: backgroundModelChoice{Connection: testAIAlpha, Revision: "reviewed", Model: "shared-model"},
		Limits: inputResearchLimits{Records: 1, ModelCalls: 30, Pages: 15}, SessionScope: "fixture-scope",
		Profiles: []portableResearchProfile{
			{Profile: json.RawMessage(`{"id":"answer"}`), Digest: digest},
			{Profile: json.RawMessage(`{"id":"page","class":"generated","shape":"http","endpoint":"https://legacy.example/final","adapter":{"name":"adapter-web"},"research":{"capabilities":["page"]}}`), Digest: digest},
		},
	}
	return inputResearchRun{Version: 2, ID: "research_" + strings.Repeat("a", 32), Revision: portableResearchRevision(frozen), CreatedAt: now, UpdatedAt: now, State: "running", Portable: &portableResearchJournal{Frozen: frozen}}
}

func admissionRetained(t *testing.T, request, result, receipt string) json.RawMessage {
	t.Helper()
	response := portableRetainedResponse{Request: json.RawMessage(request), Result: json.RawMessage(result), Receipt: json.RawMessage(receipt), Salts: json.RawMessage(`{}`)}
	response.Profile.ID, response.Profile.Digest = "page", "sha256:"+strings.Repeat("a", 64)
	raw, err := portableResearchJSON(response)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExactPageRedirectNeverEntersNextModelRequest(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	s := &Server{root: root}
	for _, memory := range []bool{false, true} {
		t.Run(fmt.Sprint("memory-", memory), func(t *testing.T) {
			run := portableAdmissionRun()
			store := portableResearchStoreFixture(t, run, memory)
			args := json.RawMessage(`{"url":"https://legacy.example/final?query=%2F"}`)
			profile := exactDeskPageProfile()
			if err := admitPortablePage(profile, args); err != nil {
				t.Fatal(err)
			}
			kept := admissionRetained(t, string(args), `{"text":"forbidden-page-body-marker"}`, `{"acquisition":{"endpoint":"https://outside.example/redirected"},"signature":"retained-signature"}`)
			acquisitions := 0
			acquire := func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
				acquisitions++
				envelope := json.RawMessage(`{"result":{"text":"forbidden-page-body-marker"},"receipt":{"acquisition":{"endpoint":"https://outside.example/redirected"},"signature":"retained-signature"},"salts":{}}`)
				return retainAdmittedPortableAcquisition(run, "page", profile, args, envelope)
			}
			if _, err := researchAttempt(t.Context(), store, "page", args, acquire); !errors.Is(err, errScopedPageReceipt) {
				t.Fatal("redirect was not refused", err)
			}
			held, err := store.read()
			if err != nil {
				t.Fatal(err)
			}
			prompt, err := s.portableResearchPrompt(held)
			if err != nil {
				t.Fatal(err)
			}
			models := 0
			_, err = researchAttempt(t.Context(), store, "model", json.RawMessage(prompt), func(_ context.Context, _ inputResearchRun, _ inputResearchAttempt) (json.RawMessage, error) {
				models++
				for _, forbidden := range []string{"forbidden-page-body-marker", "https://outside.example/redirected", "retained-signature"} {
					if strings.Contains(prompt, forbidden) {
						t.Errorf("next model request carries refused evidence: %s", forbidden)
					}
				}
				return admissionRetained(t, `{}`, `{"kind":"answer","fields":{}}`, `{}`), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = researchAttempt(t.Context(), store, "page", args, acquire); err == nil {
				t.Fatal("refused request replayed")
			}
			held, err = store.read()
			if err != nil {
				t.Fatal(err)
			}
			if acquisitions != 1 || models != 1 || held.Attempts[0].State != "refused" || !bytes.Equal(held.Attempts[0].Result, kept) {
				t.Fatal("refused receipt lost or replayed")
			}
		})
	}
}

func TestPortableRefusedJournalCapacityStopsBeforeDispatchAndReloads(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(fmt.Sprint("memory-", memory), func(t *testing.T) {
			store := portableResearchStoreFixture(t, portableAdmissionRun(), memory)
			// Each result fits the retained 2 MiB contract by less than 1 KiB. Control
			// characters and HTML characters are already escaped in these exact bytes.
			text := strings.Repeat(`\u0000<>&`, ((2<<20)-1024)/9)
			kept := admissionRetained(t, `{"url":"https://legacy.example/final"}`, `{"text":"`+text+`"}`, `{"acquisition":{"endpoint":"https://outside.example/large"},"signature":"kept"}`)
			if len(kept) > 2<<20 || len(kept) < (2<<20)-2048 {
				t.Fatal("fixture is not near the per-result limit", len(kept))
			}
			calls := 0
			for i := 0; i < 15; i++ {
				args := json.RawMessage(fmt.Sprintf(`{"url":"https://legacy.example/final?page=%d"}`, i))
				_, err := researchAttempt(t.Context(), store, "page", args, func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
					calls++
					return kept, &researchAcquisitionRefused{code: "acquisition-invalid", cause: errScopedPageReceipt}
				})
				if errors.Is(err, errPortableResearchJournalSize) {
					break
				}
				if !errors.Is(err, errScopedPageReceipt) {
					t.Fatal("terminal refused write failed", err)
				}
				// Successful model operations separate refusals, so a correction counter
				// cannot make this storage test pass before journal capacity is reached.
				_, err = researchAttempt(t.Context(), store, "model", json.RawMessage(fmt.Sprintf(`{"step":%d}`, i)), func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
					return admissionRetained(t, `{}`, `{"kind":"read_context","arguments":{}}`, `{}`), nil
				})
				if errors.Is(err, errPortableResearchJournalSize) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			held, err := store.read()
			if err != nil {
				t.Fatal(err)
			}
			if held.State != "partial" || held.Problem != errPortableResearchJournalSize.Error() || calls < 6 || calls >= 15 {
				t.Fatalf("capacity did not stop explicitly: state=%s calls=%d problem=%s", held.State, calls, held.Problem)
			}
			for _, a := range held.Attempts {
				if a.State != "completed" && a.State != "refused" || a.FinishedAt == "" || a.ReservedBytes != 0 {
					t.Fatalf("attempt left nonterminal: %s %s", a.Kind, a.State)
				}
				if a.Kind == "page" && !bytes.Equal(a.Result, kept) {
					t.Fatal("signed evidence changed")
				}
			}
			if err := portableResearchCapacity(held); err != nil {
				t.Fatal("answer capacity masked journal capacity", err)
			}
			raw, err := encodeResearchJournal(held, held.ID)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("capacity stop: %d refused pages, %d terminal attempts, %d encoded bytes", calls, len(held.Attempts), len(raw))
			restored, err := decodeResearchJournal(raw, held.ID)
			if err != nil {
				t.Fatal(err)
			}
			recoverInputResearch(&restored)
			if restored.State != "partial" || restored.Problem != held.Problem || len(restored.Attempts) != len(held.Attempts) {
				t.Fatal("reload changed capacity terminal state")
			}
			// Both the final response and additional terminal metadata still fit. No
			// retained signed evidence is rewritten or omitted to make room.
			if _, err = store.change(func(r *inputResearchRun) error {
				r.Portable.Response = json.RawMessage(`"` + strings.Repeat("x", portableResearchResponseLimit-2) + `"`)
				return nil
			}); err != nil {
				t.Fatal("final response reserve missing", err)
			}
		})
	}
}

func TestPortableJournalReservationCountsEncodedMetadataAndInFlightBytes(t *testing.T) {
	run := portableAdmissionRun()
	// Six encoded bytes per NUL; a raw-string byte count would admit this.
	run.Intent = strings.Repeat("\x00", (inputResearchRecordLimit-portableResearchAttemptReserve-portableResearchFinalReserve)/6)
	before := len(run.Attempts)
	if _, _, err := beginResearchAttempt(&run, "page", json.RawMessage(`{"url":"https://legacy.example/final"}`)); !errors.Is(err, errPortableResearchJournalSize) || len(run.Attempts) != before {
		t.Fatal("encoded metadata was not counted before dispatch", err)
	}
	run = portableAdmissionRun()
	run.Attempts = []inputResearchAttempt{{ID: "inflight", Kind: "page", State: "started", ReservedBytes: inputResearchRecordLimit}}
	if _, _, err := beginResearchAttempt(&run, "model", json.RawMessage(`{}`)); !errors.Is(err, errPortableResearchJournalSize) {
		t.Fatal("in-flight reservation spent twice", err)
	}
}

func TestPortablePageRetainerRejectsDuplicateReceiptEnvelopes(t *testing.T) {
	run := portableAdmissionRun()
	for _, raw := range []string{
		`{"result":{},"receipt":{"acquisition":{"endpoint":"https://outside.example/final"}},"receipt":{"acquisition":{"endpoint":"https://legacy.example/final"}},"salts":{}}`,
		`{"result":{},"receipt":{"acquisition":{"endpoint":"https://legacy.example/final","endpoint":"https://outside.example/final"}},"salts":{}}`,
	} {
		if _, err := retainAdmittedPortableAcquisition(run, "page", exactDeskPageProfile(), json.RawMessage(`{"url":"https://legacy.example/final"}`), json.RawMessage(raw)); err == nil {
			t.Fatal("duplicate receipt reached completed acquisition")
		}
	}
}

func TestPortableJournalBoundsEncodedDiagnosticsWithoutChangingEvidence(t *testing.T) {
	store := portableResearchStoreFixture(t, portableAdmissionRun(), true)
	kept := admissionRetained(t, `{}`, `{"text":"kept"}`, `{"signature":"kept"}`)
	diagnostic := strings.Repeat("\x00", 2<<20)
	_, err := researchAttempt(t.Context(), store, "page", json.RawMessage(`{"url":"https://legacy.example/final"}`), func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		return kept, &researchAcquisitionRefused{code: "acquisition-invalid", cause: errors.New(diagnostic)}
	})
	var refusal *researchAcquisitionRefused
	if !errors.As(err, &refusal) {
		t.Fatal(err)
	}
	held, err := store.read()
	if err != nil {
		t.Fatal(err)
	}
	a := held.Attempts[0]
	if a.State != "refused" || a.Problem != portableResearchProblemText(diagnostic) || len(a.Problem) > 8192 || !bytes.Equal(a.Result, kept) {
		t.Fatal("diagnostics consumed signed evidence or terminal space")
	}
	if portableResearchProblemText("small diagnostic") != "small diagnostic" {
		t.Fatal("ordinary diagnostic changed")
	}
}
