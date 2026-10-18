package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	exchangeCatalog "github.com/Judgment-Pack/judgment-pack-desk/internal/exchange"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/releaseplan"
)

const exchangeAcquisitionFixtureRevision = "4b9fa82ddeae772654d8f8bcf7b33d47ac65a512"

type exchangeAcquisitionPrompt struct {
	Request json.RawMessage `json:"request"`
	Next    int             `json:"nextResponseIndex"`
	Index   []struct {
		Index          int
		Kind, SourceID string
	} `json:"retainedResponses"`
	Sources []struct{ ID, Text string } `json:"retainedSources"`
}

type exchangeAcquisitionScenario struct {
	mu        sync.Mutex
	t         *testing.T
	kind      string
	fields    []string
	pageURL   string
	answer    json.RawMessage
	callbacks int
}

func (scenario *exchangeAcquisitionScenario) respond(request *http.Request) string {
	scenario.mu.Lock()
	defer scenario.mu.Unlock()
	t := scenario.t
	var wire struct {
		Contents []struct {
			Parts []struct{ Text string }
		}
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if err != nil || json.Unmarshal(raw, &wire) != nil {
		t.Error("invalid deterministic Gemini request")
		return `{}`
	}
	var prompt exchangeAcquisitionPrompt
	found := false
	for _, message := range wire.Contents {
		for _, part := range message.Parts {
			var candidate exchangeAcquisitionPrompt
			if json.Unmarshal([]byte(part.Text), &candidate) == nil && len(candidate.Request) > 0 {
				prompt = candidate
				found = true
			}
		}
	}
	if !found {
		t.Errorf("portable prompt absent from Gemini request: %s", raw)
		return `{}`
	}
	scenario.callbacks++
	encode := func(value any) string { return string(pageBindingJSON(t, value)) }
	switch prompt.Next {
	case 0:
		return encode(map[string]any{"kind": "search", "arguments": map[string]any{"query": "Synthetic " + scenario.kind + " source", "maxResults": 3}})
	case 2:
		if len(prompt.Index) != 2 || prompt.Index[1].Kind != "search" || prompt.Index[1].Index != 1 {
			t.Error("search receipt missing from shared ordered context")
			return `{}`
		}
		var search struct {
			Kind string
			Hits []struct{ URL, Snippet string }
		}
		for _, source := range prompt.Sources {
			if source.ID == prompt.Index[1].SourceID {
				if json.Unmarshal([]byte(source.Text), &search) != nil {
					t.Error("retained search result unreadable")
				}
			}
		}
		for _, hit := range search.Hits {
			if strings.Contains(hit.URL, "/fixtures/"+scenario.kind) {
				scenario.pageURL = hit.URL
				if scenario.kind == "company" && (!strings.Contains(hit.Snippet, "Fixture District") || hit.URL != "https://example.com/fixtures/company?from=search%2Ffixture") {
					t.Error("genuine conflicting observation or queried discovery URL missing")
				}
				return encode(map[string]any{"kind": "page", "arguments": map[string]string{"url": hit.URL}})
			}
		}
		t.Error("reviewed search did not discover the selected page")
		return `{}`
	case 4:
		if len(prompt.Index) != 4 || prompt.Index[3].Kind != "page" || prompt.Index[3].Index != 3 {
			t.Error("page receipt missing from shared ordered context")
			return `{}`
		}
		var page struct {
			AttachmentVersion string
			Content           struct{ Pages []struct{ Text string } }
			Provenance        struct {
				Source struct{ RequestedURL, URL string }
			}
		}
		for _, source := range prompt.Sources {
			if source.ID == prompt.Index[3].SourceID {
				if json.Unmarshal([]byte(source.Text), &page) != nil {
					t.Error("retained page unreadable")
				}
			}
		}
		if page.AttachmentVersion != "1" || page.Provenance.Source.RequestedURL != scenario.pageURL || page.Provenance.Source.URL != scenario.pageURL || len(page.Content.Pages) != 1 {
			t.Error("exact discovered page URL or real attachment missing")
			return `{}`
		}
		text := page.Content.Pages[0].Text
		if !strings.Contains(text, "Example Fixture Company") || scenario.kind == "company" && (!strings.Contains(text, "Fixture City") || !strings.Contains(text, "10 to 20") || !strings.Contains(text, "2026-01-01")) {
			t.Error("synthetic source statements missing from acquired page")
		}
		fields := exchangeAcquisitionFields(scenario.kind, scenario.pageURL)
		for name, entry := range fields {
			if value, ok := entry.(map[string]any)["value"].(string); ok {
				// The published date field asks for this explicit full-date
				// encoding; other found strings must occur verbatim on the page.
				if name == "employee_range_as_of" {
					value = strings.TrimSuffix(value, "T00:00:00Z")
				}
				if !strings.Contains(text, value) {
					t.Error("found string lacks its acquired page observation", name)
				}
			}
		}
		if scenario.kind == "notice" && !strings.Contains(text, "No deadline is stated.") {
			t.Error("missing deadline fixture observation changed")
		}
		if len(fields) != len(scenario.fields) {
			t.Error("published fields changed")
		}
		for _, name := range scenario.fields {
			if _, ok := fields[name]; !ok {
				t.Error("required published field absent", name)
			}
		}
		scenario.answer = pageBindingJSON(t, fields)
		return encode(map[string]any{"kind": "answer", "fields": fields})
	default:
		t.Error("unexpected extra provider request or source ordering", prompt.Next)
		return `{}`
	}
}

func exchangeAcquisitionFields(kind, acquiredURL string) map[string]any {
	found := func(value any) any { return map[string]any{"status": "found", "value": value, "cites": []int{3}} }
	fields := map[string]any{"source_urls": found([]string{acquiredURL})}
	switch kind {
	case "company":
		fields["company_name"] = found("Example Fixture Company")
		fields["website"] = found("https://example.com/fixtures/company")
		fields["location"] = map[string]any{"status": "conflicting", "values": []any{map[string]any{"value": "Fixture District", "cites": []int{1}}, map[string]any{"value": "Fixture City", "cites": []int{3}}}, "cites": []int{}}
		fields["employee_range"] = found(map[string]int{"min": 10, "max": 20})
		fields["employee_range_as_of"] = found("2026-01-01T00:00:00Z")
		fields["routing_signal"] = found("Public routing: https://example.com/fixtures/contact")
	case "person":
		fields["company_name"] = found("Example Fixture Company")
		fields["person_name"] = found("Alex Fixture")
		fields["role"] = found("Operations director")
		fields["profile_url"] = found("https://example.com/fixtures/person")
		fields["business_contact_url"] = found("https://example.com/fixtures/contact")
	case "notice":
		fields["issuer"] = found("Example Fixture Company")
		fields["title"] = found("Fixture supply notice")
		fields["notice_url"] = found("https://example.com/fixtures/notice")
		fields["deadline"] = map[string]any{"status": "not-found", "cites": []int{}}
		fields["region"] = found("Fixture City")
		fields["notice_status"] = found("open")
	}
	return fields
}

// Verify actual released Runtime/Runner bytes and the separately pinned fixture
// bundle before approving its manifest for this test's lifetime.
func exchangeAcquisitionComponents(t *testing.T, runtime, runner, bundle, artifacts string) {
	t.Helper()
	type component struct {
		Repository, Version, Revision string
		Files                         map[string]string
	}
	var records map[string]component
	raw, err := os.ReadFile(artifacts)
	if err != nil || json.Unmarshal(raw, &records) != nil {
		t.Fatal("requires explicit verified official component provenance", err)
	}
	for name, program := range map[string]string{"runtime": runtime, "runner": runner} {
		pin := releaseplan.Locked.Components[name]
		record := records[name]
		filename := map[string]string{"runtime": "jpack", "runner": "jpack-runner"}[name]
		data, err := os.ReadFile(program)
		if err != nil || record.Repository != pin.Repository || record.Version != pin.Version || record.Revision != pin.Revision || record.Files[filename] != digestOf(data) {
			t.Fatal("requires actual pinned released component bytes", name, err)
		}
	}
	var manifest struct {
		Fixture           bool
		Version, Revision string
		Files             map[string]string
	}
	var provenance struct {
		Kind            string
		OfficialRelease *bool
		Base            component
		Fixture         struct {
			Revision, Version, ProgramSHA256 string
			Dirty                            *bool
			ReplacedPrograms                 []string
		}
	}
	manifestRaw, err := os.ReadFile(filepath.Join(bundle, "gateway-bundle.json"))
	proof, proofErr := os.ReadFile(filepath.Join(bundle, "fixture-provenance.json"))
	if err != nil || proofErr != nil || json.Unmarshal(manifestRaw, &manifest) != nil || json.Unmarshal(proof, &provenance) != nil {
		t.Fatal("fixture identity unavailable", err, proofErr)
	}
	if !manifest.Fixture || provenance.OfficialRelease == nil || *provenance.OfficialRelease || provenance.Kind != "gateway-acquisition-fixture" || manifest.Revision != exchangeAcquisitionFixtureRevision || provenance.Fixture.Revision != manifest.Revision || provenance.Fixture.Dirty == nil || *provenance.Fixture.Dirty || manifest.Version != "fixture."+exchangeAcquisitionFixtureRevision[:12] || provenance.Fixture.Version != manifest.Version || !slices.Equal(provenance.Fixture.ReplacedPrograms, []string{"adapter-sources", "adapter-web"}) {
		t.Fatal("requires the separately pinned clean reviewed fixture identity")
	}
	base := records["gateway"]
	if base.Repository != provenance.Base.Repository || base.Version != provenance.Base.Version || base.Revision != provenance.Base.Revision || base.Revision != releaseplan.Locked.Components["gateway"].Revision || !sameEvaluationValue(base.Files, provenance.Base.Files) {
		t.Fatal("fixture replaced its pinned official signer/control provenance")
	}
	programs := []string{"gateway", "gateway-connections", "adapter-document", "adapter-drive", "adapter-gmail", "adapter-sources", "adapter-web", "adapter-render", "ocr-cloud", "ocr-tesseract"}
	if len(manifest.Files) != len(programs) {
		t.Fatal("incomplete fixture program inventory")
	}
	for _, name := range programs {
		data, err := os.ReadFile(filepath.Join(bundle, name))
		want := base.Files[name]
		if name == "adapter-sources" || name == "adapter-web" {
			want = provenance.Fixture.ProgramSHA256
		}
		if err != nil || want == "" || manifest.Files[name] != want || digestOf(data) != want {
			t.Fatal("fixture executable differs from pinned identity", name, err)
		}
	}
	t.Setenv("JPACK_DESK_GATEWAY_MANIFEST_SHA256", digestOf(manifestRaw))
}

func TestExchangeSignedReleaseAcquisitionsOnTwoInstallations(t *testing.T) {
	if os.Getenv("JPACK_EXCHANGE_ACQUISITION_ACCEPTANCE") != "1" {
		t.Skip("set JPACK_EXCHANGE_ACQUISITION_ACCEPTANCE=1 for signed search/page acquisition acceptance")
	}
	catalog, tag, commit := os.Getenv("JPACK_EXCHANGE_ACCEPTANCE_CATALOG"), os.Getenv("JPACK_EXCHANGE_ACCEPTANCE_TAG"), os.Getenv("JPACK_EXCHANGE_ACCEPTANCE_COMMIT")
	runtime, runner, bundle, artifacts := os.Getenv("JPACK_EXCHANGE_TEST_BIN"), os.Getenv("JPACK_EXCHANGE_TEST_RUNNER"), os.Getenv("JPS_TEST_GATEWAY_BUNDLE"), os.Getenv("JPACK_EXCHANGE_COMPONENT_PROVENANCE")
	if catalog == "" || tag != "v0.2.0" || commit != "565ddf477c7ed986906409f19bcf8fccfaafa8a5" || runtime == "" || runner == "" || bundle == "" || artifacts == "" {
		t.Fatal("requires explicit signed v0.2.0 catalog, released components/provenance and pinned fixture bundle")
	}
	rows, files := signedExchangeAcceptanceFiles(t, catalog, tag, commit)
	exchangeAcquisitionComponents(t, runtime, runner, bundle, artifacts)
	firstFacts, firstSources, firstDefinitions := map[string]json.RawMessage{}, map[string]json.RawMessage{}, map[string]string{}
	var firstPins []string
	var firstPublic string
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			var active atomic.Pointer[exchangeAcquisitionScenario]
			installation := newExchangeResearchInstallationFor(t, name, runtime, runner, bundle, exchangeResearchFixture{credential: "tvly-fixture-only", dailyLimit: 3, scope: EndpointScope{"https://example.com/fixtures"}, respond: func(r *http.Request) string {
				if scenario := active.Load(); scenario != nil {
					return scenario.respond(r)
				}
				t.Error("model called outside a confirmed signed preset")
				return `{}`
			}})
			s, ts := installation.server, installation.http
			profiles := []portableResearchProfile{installation.answer, installation.search, installation.page}
			pins := map[string]map[string]string{}
			var digests []string
			for index, capability := range []string{"answer", "search", "page"} {
				var profile portableProfileDetails
				if json.Unmarshal(profiles[index].Profile, &profile) != nil {
					t.Fatal("invalid registered profile")
				}
				pins[capability] = map[string]string{"profile": profile.ID, "profileDigest": profiles[index].Digest}
				digests = append(digests, profiles[index].Digest)
				if capability == "answer" {
					if name == "first" {
						firstPublic = profile.PublicKey
					} else if profile.PublicKey == firstPublic {
						t.Fatal("installation reused a Gateway signing identity")
					}
				}
			}
			if name == "first" {
				firstPins = digests
			} else {
				for index := range digests {
					if digests[index] == firstPins[index] {
						t.Fatal("installation reused a local binding")
					}
				}
			}
			var completedIDs []string
			for _, row := range rows {
				t.Run(row.ID, func(t *testing.T) {
					code, raw := aiTestRequest(t, ts, "POST", "/api/exchange/review", map[string]any{"location": catalog, "tag": tag}, nil)
					var checked struct{ Review exchangeReviewed }
					if code != 200 || json.Unmarshal(raw, &checked) != nil || checked.Review.Release.Commit != commit {
						t.Fatalf("signed release review %d %s", code, raw)
					}
					var candidate exchangeCatalog.Candidate
					for _, item := range checked.Review.Release.Candidates {
						if item.Row.ID == row.ID {
							candidate = item
						}
					}
					code, raw = aiTestRequest(t, ts, "POST", "/api/exchange/imports", map[string]any{"reviewId": checked.Review.ID, "entryId": row.ID, "base": checked.Review.Base, "reviewed": true}, nil)
					var imported exchangeImport
					if code != 201 || json.Unmarshal(raw, &imported) != nil {
						t.Fatalf("signed preset import %d %s", code, raw)
					}
					retained, err := s.exchangeCandidate(t.Context(), imported.ID)
					if err != nil || !sameEvaluationValue(retained.Files, files[row.ID]) || !sameEvaluationValue(candidate.Files, retained.Files) || retained.Digest != candidate.Digest {
						t.Fatal("signed preset bytes or identity changed", err)
					}
					if name == "first" {
						firstDefinitions[row.ID] = candidate.Digest
					} else if firstDefinitions[row.ID] != candidate.Digest {
						t.Fatal("definition identity differs between installations")
					}
					published := []byte(files[row.ID]["dialect.json"])
					var definition struct {
						Sources []struct {
							Name, Kind, Model string
							Capabilities      []string
							Fields            []struct{ Name string }
						}
					}
					if json.Unmarshal(published, &definition) != nil || len(definition.Sources) != 1 || definition.Sources[0].Kind != "research" || definition.Sources[0].Model != exchangeAcceptanceModel || !slices.Equal(definition.Sources[0].Capabilities, []string{"answer", "search", "page"}) {
						t.Fatal("published declaration changed")
					}
					source := definition.Sources[0]
					kind := map[string]string{"company-research": "company", "people-at-company": "person", "procurement-opportunity": "notice"}[filepath.Base(row.Path)]
					scenario := &exchangeAcquisitionScenario{t: t, kind: kind}
					for _, field := range source.Fields {
						scenario.fields = append(scenario.fields, field.Name)
					}
					active.Store(scenario)
					defer active.Store(nil)
					binding := pageBindingJSON(t, map[string]any{"dialect": candidate.Digest, "sources": map[string]any{source.Name: map[string]any{"profile": pins["answer"]["profile"], "profileDigest": pins["answer"]["profileDigest"], "webProfiles": map[string]any{"search": pins["search"], "page": pins["page"]}}}})
					caseValue := pageBindingJSON(t, map[string]string{"request": "Synthetic acceptance for one Example Fixture Company " + kind + ". Retain unavailable fields and conflicting observations."})
					input := pageBindingJSON(t, map[string]any{"source": map[string]any{"import": map[string]any{"dialect": json.RawMessage(published), "binding": json.RawMessage(binding)}, "case": json.RawMessage(caseValue), "sources": map[string]any{}}})
					before := installation.calls.Load()
					installation.chat.chat["interaction"] = "plan"
					installation.chat.save(t)
					code, raw = aiTestRequest(t, ts, "POST", "/api/portable-research", map[string]any{"input": json.RawMessage(input), "choice": installation.choice, "limits": inputResearchLimits{Records: 1, ModelCalls: 3, Searches: 1, Pages: 1}, "chatId": "signed-preset-acceptance"}, nil)
					var review struct{ ID, Revision, InputText string }
					if code != 201 || json.Unmarshal(raw, &review) != nil {
						t.Fatalf("portable review %d %s", code, raw)
					}
					var frozen struct {
						Source struct {
							Import struct{ Dialect json.RawMessage }
						}
					}
					if json.Unmarshal([]byte(review.InputText), &frozen) != nil || !bytes.Equal(bytes.TrimSpace(published), frozen.Source.Import.Dialect) {
						t.Fatal("signed definition JSON was rewritten")
					}
					path := "/api/portable-research/" + review.ID
					if code, _ := aiTestRequest(t, ts, "POST", path+"/confirm", map[string]any{}, map[string]string{"If-Match": review.Revision}); code != 409 || installation.calls.Load() != before {
						t.Fatal("Plan acquired signed preset research", code)
					}
					installation.chat.chat["interaction"] = "work"
					installation.chat.save(t)
					if code, raw := aiTestRequest(t, ts, "POST", path+"/confirm", map[string]any{}, map[string]string{"If-Match": review.Revision}); code != 200 || installation.calls.Load() != before {
						t.Fatalf("Work confirmation %d %s", code, raw)
					}
					s.scanInputResearch(t.Context())
					held := exchangeAcceptanceResearchRun(t, s, review.ID)
					scenario.mu.Lock()
					pageURL, answer, callbacks := scenario.pageURL, scenario.answer, scenario.callbacks
					scenario.mu.Unlock()
					if held.State != "completed" || installation.calls.Load() != before+3 || callbacks != 3 {
						t.Fatalf("research not completed state=%s problem=%s calls=%d", held.State, held.Problem, installation.calls.Load()-before)
					}
					completedIDs = append(completedIDs, review.ID)
					usage := researchBudgetUsage(held)
					if usage.ModelCalls != 3 || usage.Searches != 1 || usage.Pages != 1 || len(held.Attempts) != 5 {
						t.Fatal("acquisition allowance/order changed", usage)
					}
					responses, err := portableResearchResponses(held)
					if err != nil || len(responses) != 5 {
						t.Fatal("genuine retained receipts missing", err)
					}
					for index, expected := range []string{"model", "search", "model", "page", "model"} {
						capability := expected
						if expected == "model" {
							capability = "answer"
						}
						if held.Attempts[index].Kind != expected || held.Attempts[index].State != "completed" || responses[index].Profile.ID != pins[capability]["profile"] || responses[index].Profile.Digest != pins[capability]["profileDigest"] {
							t.Fatal("receipt order/profile pin changed", index)
						}
						var proof struct {
							Signature   string
							Acquisition struct {
								Adapter                       struct{ Name, Version, Digest string }
								Endpoint, Statement, Snapshot string
							}
						}
						if json.Unmarshal(responses[index].Receipt, &proof) != nil || proof.Signature == "" {
							t.Fatal("unsigned retained acquisition")
						}
						if expected != "model" {
							program := map[string]string{"search": "adapter-sources", "page": "adapter-web"}[expected]
							data, err := os.ReadFile(filepath.Join(bundle, program))
							if err != nil || proof.Acquisition.Adapter.Name != program || proof.Acquisition.Adapter.Version != "fixture."+exchangeAcquisitionFixtureRevision[:12] || proof.Acquisition.Adapter.Digest != "sha256:"+digestOf(data) {
								t.Fatal("receipt claimed another adapter binary", err)
							}
							if expected == "page" {
								var submitted struct{ URL string }
								endpoint, _, _ := strings.Cut(pageURL, "?")
								if json.Unmarshal(responses[index].Request, &submitted) != nil || submitted.URL != pageURL || proof.Acquisition.Endpoint != endpoint || !strings.HasPrefix(proof.Acquisition.Statement, "sha256:") || !strings.HasPrefix(proof.Acquisition.Snapshot, "sha256:") {
									t.Fatal("submitted URL, final endpoint or real acquisition commitment changed")
								}
							}
						}
					}
					completed, err := portableResearchInputWithResponse(held, held.Portable.Response)
					if err != nil {
						t.Fatal(err)
					}
					var completedMembers map[string]json.RawMessage
					json.Unmarshal(completed, &completedMembers)
					var acquired map[string]json.RawMessage
					json.Unmarshal(completedMembers["source"], &acquired)
					prepared, err := s.inputRunner(t.Context(), "inputs/preview", completed, "")
					var preview struct {
						Input struct{ Facts, Preparation json.RawMessage }
					}
					if err != nil || json.Unmarshal(prepared, &preview) != nil || !bytes.Contains(preview.Input.Preparation, []byte(`"generatedInfluence":true`)) {
						t.Fatal("genuine acquisition preparation failed", err, string(prepared))
					}
					var facts struct{ Research json.RawMessage }
					if json.Unmarshal(preview.Input.Facts, &facts) != nil || !sameEvaluationValue(facts.Research, answer) {
						t.Fatal("typed values, missing/conflicting status or citations changed", string(preview.Input.Facts))
					}
					if name == "first" {
						firstFacts[row.ID], firstSources[row.ID] = preview.Input.Facts, acquired["sources"]
					} else if !sameEvaluationValue(firstFacts[row.ID], preview.Input.Facts) {
						t.Fatal("different installation bindings changed prepared facts")
					}
					var profileValues []json.RawMessage
					for _, profile := range profiles {
						profileValues = append(profileValues, profile.Profile)
					}
					fixture := exchangeCatalog.ReplayInput{Dialect: published, Binding: binding, Case: caseValue, Acquisitions: acquired["sources"], Profiles: pageBindingJSON(t, profileValues), Preparation: preview.Input.Preparation, ExpectedDigest: sha256Digest(preview.Input.Preparation)}
					verifier, err := s.researchVerifier()
					if err != nil {
						t.Fatal(err)
					}
					canonical, err := researchCanon(t.Context(), verifier, preview.Input.Preparation)
					if err != nil {
						t.Fatal(err)
					}
					for range 2 {
						replayed, err := s.exchangeReplay(t.Context(), fixture)
						var report map[string]any
						if err != nil || json.Unmarshal(replayed, &report) != nil || report["result"] != "reading" || report["binding"] != "not-checked" || report["digest"] != sha256Digest(canonical) {
							t.Fatal("offline preparation replay failed", err, string(replayed))
						}
					}
					if name == "second" {
						acquired["sources"] = firstSources[row.ID]
						completedMembers["source"] = pageBindingJSON(t, acquired)
						_, err := s.inputRunner(t.Context(), "inputs/preview", json.RawMessage(pageBindingJSON(t, completedMembers)), "")
						var refused *inputRunnerError
						if !errors.As(err, &refused) || refused.Status != 422 || !strings.Contains(refused.Message, "profile-mismatch") {
							t.Fatal("foreign installation receipts accepted", err)
						}
					}
					s.scanInputResearch(t.Context())
					if installation.calls.Load() != before+3 {
						t.Fatal("replay or completed scan reacquired a response")
					}
					t.Log("three genuine model-answer, one search and one discovered page acquisition; exact signed definition; typed fields/statuses/citations replayed offline; zero decisions")
				})
			}
			if installation.calls.Load() != 9 {
				t.Fatal("unexpected main acceptance model acquisition count")
			}
			installation.http.Close()
			installation.server.Close()
			installation.server, installation.http = startDesk(t, installation.config)
			installation.server.inputResearchCancel()
			<-installation.server.inputResearchDone
			installation.server.scanInputResearch(t.Context())
			for _, id := range completedIDs {
				held := exchangeAcceptanceResearchRun(t, installation.server, id)
				if held.State != "completed" || len(held.Attempts) != 5 {
					t.Fatal("persisted completed acquisition lost after Desk restart", id)
				}
			}
			if installation.calls.Load() != 9 {
				t.Fatal("Desk reload replayed completed acquisitions")
			}
			var status struct{ Connections []struct{ Requests int } }
			if json.Unmarshal(installation.searchStatus(), &status) != nil || len(status.Connections) != 1 || status.Connections[0].Requests != 3 {
				t.Fatal("durable real search budget differs from three acquired presets")
			}
		})
	}
}

// These additional generic guard cases are separate from the six signed preset
// success counts. The production worker and Runner own admission/refusal.
func TestExchangeAcquisitionScopeAndRedirectRefusals(t *testing.T) {
	if os.Getenv("JPACK_EXCHANGE_ACQUISITION_ACCEPTANCE") != "1" {
		t.Skip("set JPACK_EXCHANGE_ACQUISITION_ACCEPTANCE=1 for real acquisition refusal checks")
	}
	runtime, runner, bundle, artifacts := os.Getenv("JPACK_EXCHANGE_TEST_BIN"), os.Getenv("JPACK_EXCHANGE_TEST_RUNNER"), os.Getenv("JPS_TEST_GATEWAY_BUNDLE"), os.Getenv("JPACK_EXCHANGE_COMPONENT_PROVENANCE")
	if runtime == "" || runner == "" || bundle == "" || artifacts == "" {
		t.Fatal("requires explicit released components/provenance and pinned fixture bundle")
	}
	exchangeAcquisitionComponents(t, runtime, runner, bundle, artifacts)
	t.Setenv("JPACK_BIN", runtime)
	t.Setenv("JPACK_RUNNER_TEST_BIN", runner)
	for _, test := range []struct {
		name, url string
		pages     int
	}{
		{"before_acquisition", "https://example.com/outside/company?submitted=unchanged%2Fbytes", 0},
		{"final_redirect", "https://example.com/fixtures/leave-scope?submitted=unchanged%2Fbytes", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			s, ts, basis := pageBindingFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					portableTestReply(w, string(pageBindingJSON(t, map[string]any{"kind": "page", "arguments": map[string]string{"url": test.url}})))
				} else {
					// A generated interpretation cites this genuine model answer;
					// it cannot erase an unsafe retained page acquisition.
					portableTestReply(w, string(pageBindingJSON(t, map[string]any{"kind": "answer", "fields": map[string]any{"eligible": map[string]any{"status": "found", "value": false, "cites": []int{1 + test.pages}}}})))
				}
			})
			scope := EndpointScope{"https://example.com/fixtures"}
			code, preview, raw := pageProfileRequest(t, ts, scope, "", "")
			if code != 200 {
				t.Fatalf("page preview %d %s", code, raw)
			}
			code, registered, raw := pageProfileRequest(t, ts, scope, preview.Revision, "")
			if code != 200 {
				t.Fatalf("page registration %d %s", code, raw)
			}
			var profile portableProfileDetails
			json.Unmarshal(registered.Entry.Profile, &profile)
			var input map[string]any
			json.Unmarshal(basis.Portable.Frozen.Input, &input)
			source := input["source"].(map[string]any)["mapping"].(map[string]any)["sources"].([]any)[0].(map[string]any)
			source["capabilities"] = []string{"answer", "page"}
			source["webProfiles"] = map[string]any{"page": map[string]string{"profile": profile.ID, "profileDigest": registered.Entry.Digest}}
			code, raw = aiTestRequest(t, ts, "POST", "/api/portable-research", map[string]any{"input": input, "choice": basis.Portable.Frozen.Choice, "limits": inputResearchLimits{Records: 1, ModelCalls: 2, Pages: 1}}, nil)
			var review struct{ ID, Revision string }
			if code != 201 || json.Unmarshal(raw, &review) != nil {
				t.Fatalf("guard review %d %s", code, raw)
			}
			path := "/api/portable-research/" + review.ID
			if code, raw := aiTestRequest(t, ts, "POST", path+"/confirm", map[string]any{}, map[string]string{"If-Match": review.Revision}); code != 200 {
				t.Fatalf("guard confirmation %d %s", code, raw)
			}
			s.scanInputResearch(t.Context())
			held := exchangeAcceptanceResearchRun(t, s, review.ID)
			usage := researchBudgetUsage(held)
			if calls.Load() != 2 || usage.ModelCalls != 2 || usage.Pages != test.pages || usage.Searches != 0 || len(held.Attempts) != 2+test.pages {
				t.Fatalf("refusal spent wrong allowance %+v state=%s problem=%s", usage, held.State, held.Problem)
			}
			if test.pages == 0 {
				if held.State != "completed" || held.Attempts[0].ActionProblem == "" || held.Attempts[1].Kind != "model" {
					t.Fatalf("out-of-scope request reached acquisition or lost refusal: %+v", held)
				}
			} else {
				if held.State != "paused" || pendingResearchBudget(held) == nil || len(held.Portable.Response) != 0 || held.Attempts[2].ActionProblem == "" {
					t.Fatalf("unsafe final endpoint was accepted: state=%s problem=%s finding=%s", held.State, held.Problem, held.Attempts[2].ActionProblem)
				}
				var page portableRetainedResponse
				json.Unmarshal(held.Attempts[1].Result, &page)
				var receipt struct {
					Acquisition struct{ Endpoint string }
					Signature   string
				}
				if json.Unmarshal(page.Receipt, &receipt) != nil || receipt.Signature == "" || receipt.Acquisition.Endpoint != "https://example.com/outside/company" || !strings.Contains(held.Attempts[2].ActionProblem, "endpoint") {
					t.Fatalf("Runner did not refuse the genuine redirected final endpoint: %s", held.Attempts[2].ActionProblem)
				}
				var request struct{ URL string }
				if json.Unmarshal(page.Request, &request) != nil || request.URL != test.url {
					t.Fatal("redirect changed submitted page arguments")
				}
			}
			s.scanInputResearch(t.Context())
			if calls.Load() != 2 {
				t.Fatal("refusal/completed scan replayed an acquisition")
			}
			t.Logf("additional guard case: two genuine model receipts, %d genuine page receipts, zero searches/decisions", test.pages)
		})
	}
}
