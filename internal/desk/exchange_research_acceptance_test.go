package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	exchangeCatalog "github.com/Judgment-Pack/judgment-pack-desk/internal/exchange"
)

const exchangeAcceptanceModel = "gemini-2.5-flash"

type exchangeResearchInstallation struct {
	server       *Server
	http         *httptest.Server
	choice       backgroundModelChoice
	answer       portableResearchProfile
	search       portableResearchProfile
	page         portableResearchProfile
	chat         *actionFixture
	calls        atomic.Int32
	reply        atomic.Value
	searchStatus func() json.RawMessage
	config       Config
}

type exchangeResearchFixture struct {
	respond    func(*http.Request) string
	credential string
	dailyLimit int
	scope      EndpointScope
}

func exchangeAcceptanceResearchRun(t *testing.T, s *Server, id string) inputResearchRun {
	t.Helper()
	root, err := s.inputResearchRoot(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	record, err := openInputResearch(root, id, false)
	if err != nil {
		t.Fatal(err)
	}
	defer record.Close()
	run, err := s.researchJournal(record, id).Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return run
}

// Only loopback Gemini test-provider requests are performed. Search/page
// profiles bind the unchanged declarations, but neither adapter acquires data.
func newExchangeResearchInstallation(t *testing.T, name, runtime, runner, bundle string) *exchangeResearchInstallation {
	t.Helper()
	return newExchangeResearchInstallationFor(t, name, runtime, runner, bundle, exchangeResearchFixture{})
}

func newExchangeResearchInstallationFor(t *testing.T, name, runtime, runner, bundle string, fixture exchangeResearchFixture) *exchangeResearchInstallation {
	t.Helper()
	installation := &exchangeResearchInstallation{}
	installation.reply.Store("")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		installation.calls.Add(1)
		if r.URL.Path != "/v1beta/models/"+exchangeAcceptanceModel+":generateContent" {
			t.Errorf("declared model route changed: %s", r.URL.Path)
		}
		reply := installation.reply.Load().(string)
		if fixture.respond != nil {
			reply = fixture.respond(r)
		}
		if reply == "" {
			t.Error("provider called outside explicitly confirmed research")
			http.Error(w, "fixture not ready", 500)
			return
		}
		portableFamilyReply(t, "gemini", w, reply)
	}))
	t.Cleanup(provider.Close)
	project, config := t.TempDir(), t.TempDir()
	if err := os.Chmod(config, 0700); err != nil {
		t.Fatal(err)
	}
	writeProject(t, project, map[string]string{"jpack.json": `{"configVersion":"2","packs":{}}`})
	installation.config = Config{LocalAccess: true, ProjectDir: project, DeskConfigDir: config, Token: testToken, JpackBin: runtime, RunnerBin: runner, LocalGatewayBundle: bundle}
	s, ts := startDesk(t, installation.config)
	s.inputResearchCancel()
	<-s.inputResearchDone
	t.Cleanup(func() { installation.http.Close(); installation.server.Close() })
	installation.server, installation.http = s, ts
	connection := aiTestConnection(testAIAlpha, "Signed preset acceptance "+name, provider.URL)
	connection.Assistant.Endpoint.Kind = "gemini"
	connection.Assistant.Endpoint.Models = []string{exchangeAcceptanceModel}
	model := exchangeAcceptanceModel
	connection.Assistant.Endpoint.Model = &model
	registry := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
	current := registry.Connections[0]
	code, raw := aiTestRequest(t, ts, "PUT", "/api/assistant/key", map[string]string{"key": "acceptance-fixture-only"}, map[string]string{aiConnectionHeader: current.ID, aiRevisionHeader: current.Revision})
	if code != 200 {
		t.Fatalf("model fixture key %d %s", code, raw)
	}
	installation.choice = backgroundModelChoice{Connection: current.ID, Revision: current.Revision, Model: exchangeAcceptanceModel}
	installation.chat = &actionFixture{s: s, ts: ts, chat: map[string]any{"id": "signed-preset-acceptance", "interaction": "work"}, cases: []any{}}
	installation.chat.save(t)
	code, raw = aiTestRequest(t, ts, "POST", "/api/portable-research/profiles", map[string]any{"choice": installation.choice, "reviewed": true, "chatId": "signed-preset-acceptance"}, nil)
	if code != 200 || json.Unmarshal(raw, &installation.answer) != nil {
		t.Fatalf("answer binding %d %s", code, raw)
	}
	searchCall := func(method string, body any) json.RawMessage {
		t.Helper()
		encoded, _ := json.Marshal(body)
		current := installation.server
		raw, err := current.connectionCompanion("web-search", true).call(t.Context(), bundle, platformPath(current.assistant.dir, "gateway-connections"), method, encoded, "web-search", false)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if fixture.credential == "" {
		fixture.credential = "tvly-acceptance-fixture-only"
	}
	if fixture.dailyLimit == 0 {
		fixture.dailyLimit = 1
	}
	if fixture.scope == nil {
		fixture.scope = EndpointScope{"https://acceptance.example.invalid/pages"}
	}
	searchCall("configure", map[string]any{"id": "acceptance-search-" + name, "name": "Acceptance search", "provider": "tavily", "credential": fixture.credential, "dailyLimit": fixture.dailyLimit})
	var status struct {
		Connections []struct{ ID, Revision string }
	}
	if json.Unmarshal(searchCall("status", map[string]any{}), &status) != nil || len(status.Connections) != 1 {
		t.Fatal("search descriptor fixture unavailable")
	}
	searchChoice := inputResearchSearch{Connection: status.Connections[0].ID, Revision: status.Connections[0].Revision}
	code, raw = aiTestRequest(t, ts, "POST", "/api/portable-research/search-binding", map[string]any{"choice": searchChoice, "chatId": "signed-preset-acceptance"}, nil)
	var searchReview struct {
		Revision string
		Entry    portableResearchProfile
	}
	if code != 200 || json.Unmarshal(raw, &searchReview) != nil || searchReview.Revision == "" {
		t.Fatalf("search preview %d %s", code, raw)
	}
	code, raw = aiTestRequest(t, ts, "POST", "/api/portable-research/search-binding", map[string]any{"choice": searchChoice, "reviewedDigest": searchReview.Revision, "chatId": "signed-preset-acceptance"}, nil)
	if code != 200 || json.Unmarshal(raw, &searchReview) != nil || len(searchReview.Entry.Profile) == 0 {
		t.Fatalf("search registration %d %s", code, raw)
	}
	installation.search = searchReview.Entry
	installation.searchStatus = func() json.RawMessage { return searchCall("status", map[string]any{}) }
	code, pageReview, raw := pageProfileRequest(t, ts, fixture.scope, "", "signed-preset-acceptance")
	if code != 200 {
		t.Fatalf("page preview %d %s", code, raw)
	}
	code, pageReview, raw = pageProfileRequest(t, ts, fixture.scope, pageReview.Revision, "signed-preset-acceptance")
	if code != 200 || len(pageReview.Entry.Profile) == 0 {
		t.Fatalf("page registration %d %s", code, raw)
	}
	installation.page = pageReview.Entry
	if installation.calls.Load() != 0 {
		t.Fatal("profile setup acquired a model answer")
	}
	return installation
}

func signedExchangeAcceptanceFiles(t *testing.T, catalog, tag, commit string) ([]exchangeCatalog.Row, map[string]map[string]string) {
	t.Helper()
	git := func(args ...string) []byte {
		t.Helper()
		raw, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", catalog}, args...)...).Output()
		if err != nil {
			t.Fatalf("catalog Git %v: %v", args, err)
		}
		return raw
	}
	if actual := strings.TrimSpace(string(git("rev-parse", "--verify", "refs/tags/"+tag+"^{commit}"))); actual != commit {
		t.Fatalf("signed release commit %s differs from expected %s", actual, commit)
	}
	git("verify-tag", "refs/tags/"+tag)
	manifest := git("show", commit+":manifest.json")
	var rows []exchangeCatalog.Row
	if json.Unmarshal(manifest, &rows) != nil || len(rows) != 3 {
		t.Fatal("this acceptance requires the three signed v0.2.0 research presets")
	}
	files := map[string]map[string]string{}
	for _, row := range rows {
		if row.Kind != "preset" {
			t.Fatal("expected an unchanged signed preset", row.ID)
		}
		files[row.ID] = map[string]string{}
		for _, name := range []string{"entry.json", "dialect.json", "README.md"} {
			files[row.ID][name] = string(git("show", commit+":"+row.Path+"/"+name))
		}
		if exchangeCatalog.Digest([]byte(files[row.ID]["entry.json"])) != row.EntryDigest || exchangeCatalog.Digest([]byte(files[row.ID]["dialect.json"])) != row.DialectDigest {
			t.Fatal("signed preset file digest differs", row.ID)
		}
	}
	return rows, files
}

// This acceptance is deliberately narrower than a live research claim: six
// model-answer acquisitions (three signed presets, two installations), with all
// fields not-found; zero search/page acquisitions and zero decisions.
func TestExchangeSignedReleaseResearchPreparationOnTwoInstallations(t *testing.T) {
	if os.Getenv("JPACK_EXCHANGE_RESEARCH_ACCEPTANCE") != "1" {
		t.Skip("set JPACK_EXCHANGE_RESEARCH_ACCEPTANCE=1 for signed-release research acceptance")
	}
	catalog, tag, commit := os.Getenv("JPACK_EXCHANGE_ACCEPTANCE_CATALOG"), os.Getenv("JPACK_EXCHANGE_ACCEPTANCE_TAG"), os.Getenv("JPACK_EXCHANGE_ACCEPTANCE_COMMIT")
	runtime, runner, bundle := os.Getenv("JPACK_EXCHANGE_TEST_BIN"), os.Getenv("JPACK_EXCHANGE_TEST_RUNNER"), os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if catalog == "" || tag == "" || commit == "" || runtime == "" || runner == "" || bundle == "" {
		t.Fatal("requires explicit signed catalog/tag/commit, Runtime, Runner and descriptor-enabled Gateway bundle")
	}
	rows, files := signedExchangeAcceptanceFiles(t, catalog, tag, commit)
	manifestBytes, err := os.ReadFile(filepath.Join(bundle, "gateway-bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	// The exact development manifest is approved only for this test's lifetime.
	// It never changes global trust for unrelated synthetic-bundle tests.
	t.Setenv("JPACK_DESK_GATEWAY_MANIFEST_SHA256", digestOf(manifestBytes))
	firstSources := map[string]json.RawMessage{}
	firstDefinitions := map[string]string{}
	firstFacts := map[string]json.RawMessage{}
	var firstBindings map[string]string
	var firstPublic string
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			installation := newExchangeResearchInstallation(t, name, runtime, runner, bundle)
			s, ts := installation.server, installation.http
			profiles := []portableResearchProfile{installation.answer, installation.search, installation.page}
			pins := map[string]map[string]string{}
			bindings := map[string]string{}
			for index, capability := range []string{"answer", "search", "page"} {
				var profile portableProfileDetails
				if json.Unmarshal(profiles[index].Profile, &profile) != nil {
					t.Fatal("invalid registered profile")
				}
				pins[capability] = map[string]string{"profile": profile.ID, "profileDigest": profiles[index].Digest}
				bindings[capability] = profiles[index].Digest
				if index == 0 {
					if name == "first" {
						firstPublic = profile.PublicKey
					} else if firstPublic == profile.PublicKey {
						t.Fatal("installations share Gateway signing identity")
					}
				}
			}
			if name == "first" {
				firstBindings = bindings
			} else {
				for capability, digest := range bindings {
					if digest == firstBindings[capability] {
						t.Fatal("installation binding did not change", capability)
					}
				}
			}
			beforeSearch := installation.searchStatus()
			for _, row := range rows {
				t.Run(row.ID, func(t *testing.T) {
					code, raw := aiTestRequest(t, ts, "POST", "/api/exchange/review", map[string]any{"location": catalog, "tag": tag}, nil)
					var checked struct{ Review exchangeReviewed }
					if code != 200 || json.Unmarshal(raw, &checked) != nil {
						t.Fatalf("signed release review %d %s", code, raw)
					}
					var candidate exchangeCatalog.Candidate
					for _, item := range checked.Review.Release.Candidates {
						if item.Row.ID == row.ID {
							candidate = item
						}
					}
					if checked.Review.Release.Commit != commit || !sameEvaluationValue(candidate.Files, files[row.ID]) || candidate.Digest == "" {
						t.Fatal("signed bytes or canonical identity changed", row.ID)
					}
					code, raw = aiTestRequest(t, ts, "POST", "/api/exchange/imports", map[string]any{"reviewId": checked.Review.ID, "entryId": row.ID, "base": checked.Review.Base, "reviewed": true}, nil)
					var imported exchangeImport
					if code != 201 || json.Unmarshal(raw, &imported) != nil {
						t.Fatalf("signed preset import %d %s", code, raw)
					}
					retained, err := s.exchangeCandidate(t.Context(), imported.ID)
					if err != nil || !sameEvaluationValue(retained.Files, files[row.ID]) || retained.Document != candidate.Document || retained.Digest != candidate.Digest {
						t.Fatal("retained signed preset changed", err)
					}
					if name == "first" {
						firstDefinitions[row.ID] = candidate.Digest
					} else if firstDefinitions[row.ID] != candidate.Digest {
						t.Fatal("installation changed definition identity")
					}
					var document struct {
						Sources []struct {
							Name, Kind, Model string
							Capabilities      []string
							Fields            []struct{ Name string }
						}
					}
					published := []byte(files[row.ID]["dialect.json"])
					if json.Unmarshal(published, &document) != nil || len(document.Sources) != 1 {
						t.Fatal("expected one published research source")
					}
					source := document.Sources[0]
					if source.Kind != "research" || source.Model != exchangeAcceptanceModel || !slices.Equal(source.Capabilities, []string{"answer", "search", "page"}) {
						t.Fatal("published model/capabilities changed")
					}
					fields := map[string]any{}
					for _, field := range source.Fields {
						fields[field.Name] = map[string]any{"status": "not-found", "cites": []any{}}
					}
					answer, _ := json.Marshal(map[string]any{"kind": "answer", "fields": fields})
					installation.reply.Store(string(answer))
					binding, _ := json.Marshal(map[string]any{"dialect": candidate.Digest, "sources": map[string]any{source.Name: map[string]any{"profile": pins["answer"]["profile"], "profileDigest": pins["answer"]["profileDigest"], "webProfiles": map[string]any{"search": pins["search"], "page": pins["page"]}}}})
					caseValue, _ := json.Marshal(map[string]string{"request": "Synthetic acceptance request for " + row.ID + ". Preserve every unavailable field as not-found."})
					input, _ := json.Marshal(map[string]any{"source": map[string]any{"import": map[string]any{"dialect": json.RawMessage(published), "binding": json.RawMessage(binding)}, "case": json.RawMessage(caseValue), "sources": map[string]any{}}})
					before := installation.calls.Load()
					installation.chat.chat["interaction"] = "plan"
					installation.chat.save(t)
					code, raw = aiTestRequest(t, ts, "POST", "/api/portable-research", map[string]any{"input": json.RawMessage(input), "choice": installation.choice, "limits": inputResearchLimits{Records: 1, ModelCalls: 1, Searches: 1, Pages: 1}, "chatId": "signed-preset-acceptance"}, nil)
					var review struct{ ID, Revision, State, InputText string }
					if code != 201 || json.Unmarshal(raw, &review) != nil {
						t.Fatalf("portable review %d %s", code, raw)
					}
					var frozen struct {
						Source struct {
							Import struct{ Dialect json.RawMessage }
						}
					}
					if json.Unmarshal([]byte(review.InputText), &frozen) != nil || !bytes.Equal(bytes.TrimSpace(published), frozen.Source.Import.Dialect) {
						t.Fatal("published definition JSON was rewritten")
					}
					path := "/api/portable-research/" + review.ID
					if code, _ := aiTestRequest(t, ts, "POST", path+"/confirm", map[string]any{}, map[string]string{"If-Match": review.Revision}); code != 409 {
						t.Fatal("Plan started published research", code)
					}
					if installation.calls.Load() != before {
						t.Fatal("review or Plan spent compute")
					}
					installation.chat.chat["interaction"] = "work"
					installation.chat.save(t)
					if code, raw = aiTestRequest(t, ts, "POST", path+"/confirm", map[string]any{}, map[string]string{"If-Match": review.Revision}); code != 200 {
						t.Fatalf("Work confirm %d %s", code, raw)
					}
					if installation.calls.Load() != before {
						t.Fatal("confirmation bypassed the durable worker")
					}
					s.scanInputResearch(t.Context())
					code, raw = aiTestRequest(t, ts, "GET", path, nil, nil)
					var finished struct {
						State, InputText, ResponseText string
						Activity                       inputResearchActivity
					}
					if code != 200 || json.Unmarshal(raw, &finished) != nil || finished.State != "completed" || installation.calls.Load() != before+1 {
						t.Fatalf("research not completed %d %s calls=%d", code, raw, installation.calls.Load()-before)
					}
					var result struct {
						Result struct {
							Fields    json.RawMessage
							Responses []portableRetainedResponse
						}
					}
					if json.Unmarshal([]byte(finished.ResponseText), &result) != nil || len(result.Result.Responses) != 1 || !sameEvaluationValue(json.RawMessage(result.Result.Fields), json.RawMessage(pageBindingJSON(t, fields))) {
						t.Fatal("missing-value answer or acquisition count changed")
					}
					receipt := result.Result.Responses[0]
					if receipt.Profile.ID != pins["answer"]["profile"] || receipt.Profile.Digest != pins["answer"]["profileDigest"] {
						t.Fatal("model receipt not bound to this installation")
					}
					var proof struct {
						Signature, Authority, Source string
						Acquisition                  struct{ Adapter struct{ Name string } }
					}
					if json.Unmarshal(receipt.Receipt, &proof) != nil || proof.Signature == "" || proof.Source != modelSourceName || proof.Acquisition.Adapter.Name != "jpack-desk/model-answer" {
						t.Fatal("not a genuine Gateway model acquisition envelope")
					}
					for _, step := range finished.Activity.Steps {
						if step.Kind != "model" {
							t.Fatal("unexpected search/page acquisition", step.Kind)
						}
					}
					held := exchangeAcceptanceResearchRun(t, s, review.ID)
					usage := researchBudgetUsage(held)
					if usage.ModelCalls != 1 || usage.Searches != 0 || usage.Pages != 0 {
						t.Fatalf("unexpected acquisition usage %+v", usage)
					}
					s.scanInputResearch(t.Context())
					if installation.calls.Load() != before+1 {
						t.Fatal("completed worker replayed a model acquisition")
					}
					completedInput, err := portableResearchInputWithResponse(held, held.Portable.Response)
					if err != nil {
						t.Fatal(err)
					}
					var completed map[string]json.RawMessage
					json.Unmarshal(completedInput, &completed)
					var acquired map[string]json.RawMessage
					json.Unmarshal(completed["source"], &acquired)
					prepared, err := s.inputRunner(t.Context(), "inputs/preview", json.RawMessage(completedInput), "")
					if err != nil {
						t.Fatal("Runner preparation refused genuine retained answer", err)
					}
					var preview struct {
						Input struct{ Facts, Preparation json.RawMessage }
					}
					if json.Unmarshal(prepared, &preview) != nil || len(preview.Input.Preparation) == 0 || !bytes.Contains(preview.Input.Preparation, []byte(`"generatedInfluence":true`)) {
						t.Fatal("generated preparation missing", string(prepared))
					}
					if name == "first" {
						firstSources[row.ID] = append(json.RawMessage(nil), acquired["sources"]...)
						firstFacts[row.ID] = append(json.RawMessage(nil), preview.Input.Facts...)
					} else if !sameEvaluationValue(firstFacts[row.ID], preview.Input.Facts) {
						t.Fatal("different bindings changed the deterministic prepared facts")
					}
					profileValues := []json.RawMessage{}
					for _, profile := range profiles {
						profileValues = append(profileValues, profile.Profile)
					}
					profileJSON, _ := json.Marshal(profileValues)
					fixture := exchangeCatalog.ReplayInput{Dialect: published, Binding: binding, Case: caseValue, Acquisitions: acquired["sources"], Profiles: profileJSON, Preparation: preview.Input.Preparation, ExpectedDigest: sha256Digest(preview.Input.Preparation)}
					// The file precondition pins the retained wire bytes; the report
					// names Runner's canonical preparation identity. Neither receipt
					// nor request is changed to compute this disposable digest.
					verifier, err := s.researchVerifier()
					if err != nil {
						t.Fatal(err)
					}
					canonicalPreparation, err := researchCanon(t.Context(), verifier, preview.Input.Preparation)
					if err != nil {
						t.Fatal(err)
					}
					wantPreparationDigest := sha256Digest(canonicalPreparation)
					for range 2 {
						replayed, err := s.exchangeReplay(t.Context(), fixture)
						var report map[string]any
						if err != nil || json.Unmarshal(replayed, &report) != nil || report["result"] != "reading" || report["binding"] != "not-checked" || report["digest"] != wantPreparationDigest {
							t.Fatalf("offline replay %s %v", replayed, err)
						}
					}
					if name == "second" {
						acquired["sources"] = firstSources[row.ID]
						completed["source"], _ = json.Marshal(acquired)
						foreign, _ := json.Marshal(completed)
						_, err := s.inputRunner(t.Context(), "inputs/preview", json.RawMessage(foreign), "")
						var refused *inputRunnerError
						if !errors.As(err, &refused) || refused.Status != 422 || !strings.Contains(refused.Message, "profile-mismatch") {
							t.Fatal("foreign installation receipt accepted", err)
						}
					}
					if installation.calls.Load() != before+1 {
						t.Fatal("preparation/replay spent another provider request")
					}
					t.Log("one Gateway-signed model-answer acquisition; zero search/page acquisitions; unchanged signed preset and not-found outcomes replayed offline")
				})
			}
			if installation.calls.Load() != 3 || !bytes.Equal(beforeSearch, installation.searchStatus()) {
				t.Fatal("unexpected acquisition or search usage")
			}
		})
	}
}
