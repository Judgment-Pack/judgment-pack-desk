package desk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type pageBindingReply struct {
	Revision   string                  `json:"revision"`
	Descriptor pageBindingDescriptor   `json:"descriptor"`
	Profile    json.RawMessage         `json:"profile"`
	Entry      portableResearchProfile `json:"entry"`
}

func pageBindingJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func pageBindingFixture(t *testing.T, handler http.HandlerFunc) (*Server, *httptest.Server, inputResearchRun) {
	t.Helper()
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if bundle == "" || os.Getenv("JPACK_RUNNER_TEST_BIN") == "" || os.Getenv("JPACK_BIN") == "" {
		t.Skip("requires descriptor-enabled Gateway, Runner and Runtime")
	}
	manifest, err := os.ReadFile(filepath.Join(bundle, "gateway-bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("JPACK_DESK_GATEWAY_MANIFEST_SHA256", digestOf(manifest))
	return portableResearchFixture(t, handler)
}

func pageProfileRequest(t *testing.T, ts *httptest.Server, scope EndpointScope, review string, chat string) (int, pageBindingReply, []byte) {
	t.Helper()
	body := map[string]any{"endpointScope": scope}
	if review != "" {
		body["reviewedDigest"] = review
	}
	if chat != "" {
		body["chatId"] = chat
	}
	code, raw := aiTestRequest(t, ts, "POST", "/api/portable-research/page-binding", body, nil)
	var result pageBindingReply
	if code == 200 && json.Unmarshal(raw, &result) != nil {
		t.Fatal("invalid profile reply", string(raw))
	}
	return code, result, raw
}

func installedPageProfiles(t *testing.T, s *Server) []portableResearchProfile {
	t.Helper()
	raw, err := s.inputRunner(t.Context(), "input-profiles", nil, "")
	var profiles []portableResearchProfile
	if err != nil || json.Unmarshal(raw, &profiles) != nil {
		t.Fatal("profiles unavailable", err)
	}
	return profiles
}

func TestPortablePageBindingActualCompanionReviewAndRegistration(t *testing.T) {
	var calls atomic.Int32
	s, ts, basis := pageBindingFixture(t, func(http.ResponseWriter, *http.Request) { calls.Add(1); t.Error("profile lifecycle called a model") })
	before := installedPageProfiles(t, s)
	scope := EndpointScope{"https://Docs.EXAMPLE:/guides/%7e", "https://second.example/notes"}
	code, review, raw := pageProfileRequest(t, ts, scope, "", "")
	if code != 200 || review.Revision == "" || len(review.Entry.Profile) != 0 {
		t.Fatalf("preview %d %s", code, raw)
	}
	if len(installedPageProfiles(t, s)) != len(before) {
		t.Fatal("preview registered a profile")
	}
	var profile struct {
		ID, Class, Source, Shape string
		Endpoint                 *string
		EndpointScope            EndpointScope
		Research                 struct{ Capabilities []string }
	}
	if json.Unmarshal(review.Profile, &profile) != nil || profile.Class != "generated" || profile.Shape != "http" || profile.Source != "web" || profile.Endpoint != nil || !bytes.Equal(pageBindingJSON(t, scope), pageBindingJSON(t, profile.EndpointScope)) || len(profile.Research.Capabilities) != 1 || profile.Research.Capabilities[0] != "page" {
		t.Fatal("preview changed scoped profile", string(review.Profile))
	}
	if review.Descriptor.Adapter.Name != "adapter-web" || review.Descriptor.Adapter.Version == "" || review.Descriptor.Adapter.Digest == "" {
		t.Fatal("adapter identity not described")
	}
	code, registered, raw := pageProfileRequest(t, ts, scope, review.Revision, "")
	if code != 200 || len(registered.Entry.Profile) == 0 {
		t.Fatalf("registration %d %s", code, raw)
	}
	code, again, _ := pageProfileRequest(t, ts, scope, review.Revision, "")
	if code != 200 || again.Entry.Digest != registered.Entry.Digest || !bytes.Equal(again.Entry.Profile, registered.Entry.Profile) {
		t.Fatal("immutable registration is not idempotent")
	}
	reversed := EndpointScope{scope[1], scope[0]}
	code, changed, _ := pageProfileRequest(t, ts, reversed, "", "")
	if code != 200 || changed.Revision == review.Revision || bytes.Equal(changed.Profile, review.Profile) {
		t.Fatal("scope order omitted from profile identity/review")
	}
	if code, _, _ := pageProfileRequest(t, ts, reversed, review.Revision, ""); code != 409 {
		t.Fatal("stale scope review registered", code)
	}
	if code, _, _ := pageProfileRequest(t, ts, EndpointScope{pageScopeWildcard}, "", ""); code != 200 {
		t.Fatal("explicit wildcard preview refused", code)
	}
	if len(installedPageProfiles(t, s)) != len(before)+1 || calls.Load() != 0 {
		t.Fatal("review changed registry or acquired a source")
	}
	// A later exact-endpoint profile remains legacy when a scoped profile
	// precedes it in the same installation registry.
	var legacy map[string]any
	json.Unmarshal(registered.Entry.Profile, &legacy)
	delete(legacy, "endpointScope")
	legacy["id"], legacy["endpoint"] = "legacy-page-profile", "https://legacy.example/final"
	legacyRaw, err := s.inputRunner(t.Context(), "input-profiles", legacy, "")
	if err != nil {
		t.Fatal(err)
	}
	var legacyEntry portableResearchProfile
	json.Unmarshal(legacyRaw, &legacyEntry)
	var input map[string]any
	json.Unmarshal(basis.Portable.Frozen.Input, &input)
	assignment := input["source"].(map[string]any)["mapping"].(map[string]any)["sources"].([]any)[0].(map[string]any)
	assignment["capabilities"] = []string{"answer", "page"}
	assignment["webProfiles"] = map[string]any{"page": map[string]string{"profile": "legacy-page-profile", "profileDigest": legacyEntry.Digest}}
	encoded, _ := json.Marshal(input)
	run, err := s.reviewPortableResearch(t.Context(), encoded, basis.Portable.Frozen.Choice, inputResearchLimits{Records: 1, ModelCalls: 3, Pages: 1}, "", "fixture-scope")
	if err != nil {
		t.Fatal("mixed legacy review refused", err)
	}
	selected, err := portableResearchProfileFor(run, "page")
	if err != nil || selected.EndpointScope != nil {
		t.Fatal("legacy profile inherited installation scope", err, selected.EndpointScope)
	}
	if err := admitPortablePage(selected, json.RawMessage(`{"url":"https://legacy.example/redirect?query=%2F"}`)); err != nil {
		t.Fatal("legacy query/redirect request narrowed", err)
	}
}

func TestPortablePageBindingActualCompanionPlanAndCapacity(t *testing.T) {
	s, ts, _ := pageBindingFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected model acquisition") })
	chat := actionFixture{s: s, ts: ts, chat: map[string]any{"id": "page-binding-plan", "interaction": "plan"}, cases: []any{}}
	chat.save(t)
	scope := EndpointScope{"https://limits.example/pages"}
	code, preview, raw := pageProfileRequest(t, ts, scope, "", "page-binding-plan")
	if code != 200 {
		t.Fatalf("Plan preview %d %s", code, raw)
	}
	if code, _, _ := pageProfileRequest(t, ts, scope, preview.Revision, "page-binding-plan"); code != 409 {
		t.Fatal("Plan registered an installation binding", code)
	}
	chat.chat["interaction"] = "work"
	chat.save(t)
	if code, _, raw := pageProfileRequest(t, ts, scope, preview.Revision, "page-binding-plan"); code != 200 {
		t.Fatalf("Work registration %d %s", code, raw)
	}
	for i := len(installedPageProfiles(t, s)); i < 16; i++ {
		entries := EndpointScope{fmt.Sprintf("https://limits.example/profile/%d", i)}
		code, review, raw := pageProfileRequest(t, ts, entries, "", "")
		if code != 200 {
			t.Fatalf("capacity preview %d %s", code, raw)
		}
		if code, _, raw = pageProfileRequest(t, ts, entries, review.Revision, ""); code != 200 {
			t.Fatalf("capacity registration %d %s", code, raw)
		}
	}
	before := pageBindingJSON(t, installedPageProfiles(t, s))
	code, extra, raw := pageProfileRequest(t, ts, EndpointScope{"https://limits.example/extra"}, "", "")
	if code != 200 {
		t.Fatalf("full-registry preview %d %s", code, raw)
	}
	if code, _, raw = pageProfileRequest(t, ts, EndpointScope{"https://limits.example/extra"}, extra.Revision, ""); code != 409 || !bytes.Contains(raw, []byte("16")) {
		t.Fatalf("capacity widened %d %s", code, raw)
	}
	if !bytes.Equal(before, pageBindingJSON(t, installedPageProfiles(t, s))) {
		t.Fatal("capacity refusal changed existing immutable profiles")
	}
	if code, _, _ := pageProfileRequest(t, ts, scope, preview.Revision, "page-binding-plan"); code != 200 {
		t.Fatal("existing profile was not reusable at capacity", code)
	}
}

func TestPortablePageBindingActualCompanionRejectsStaleAdapterBeforeAcquisition(t *testing.T) {
	var calls atomic.Int32
	s, ts, basis := pageBindingFixture(t, func(http.ResponseWriter, *http.Request) { calls.Add(1); t.Error("stale page binding called a model") })
	scope := EndpointScope{"https://docs.example/guides"}
	code, preview, raw := pageProfileRequest(t, ts, scope, "", "")
	if code != 200 {
		t.Fatalf("preview %d %s", code, raw)
	}
	code, registered, raw := pageProfileRequest(t, ts, scope, preview.Revision, "")
	if code != 200 {
		t.Fatalf("registration %d %s", code, raw)
	}
	var profile map[string]any
	json.Unmarshal(registered.Entry.Profile, &profile)
	profile["id"] = "operator-stale-pages"
	profile["adapter"].(map[string]any)["digest"] = "sha256:" + strings.Repeat("a", 64)
	staleRaw, err := s.inputRunner(t.Context(), "input-profiles", profile, "")
	if err != nil {
		t.Fatal(err)
	}
	var stale portableResearchProfile
	json.Unmarshal(staleRaw, &stale)
	var input map[string]any
	json.Unmarshal(basis.Portable.Frozen.Input, &input)
	source := input["source"].(map[string]any)["mapping"].(map[string]any)["sources"].([]any)[0].(map[string]any)
	source["capabilities"] = []string{"answer", "page"}
	assign := func(id, digest string) json.RawMessage {
		source["webProfiles"] = map[string]any{"page": map[string]string{"profile": id, "profileDigest": digest}}
		raw, _ := json.Marshal(input)
		return raw
	}
	staleInput := assign("operator-stale-pages", stale.Digest)
	limits := inputResearchLimits{Records: 1, ModelCalls: 3, Pages: 1}
	if _, err := s.reviewPortableResearch(t.Context(), staleInput, basis.Portable.Frozen.Choice, limits, "", "fixture-scope"); err == nil || !strings.Contains(err.Error(), "page adapter changed") {
		t.Fatal("stale adapter reviewed", err)
	}
	var current portableProfileDetails
	json.Unmarshal(registered.Entry.Profile, &current)
	run, err := s.reviewPortableResearch(t.Context(), assign(current.ID, registered.Entry.Digest), basis.Portable.Frozen.Choice, limits, "", "fixture-scope")
	if err != nil {
		t.Fatal(err)
	}
	// Model a persisted review that names the formerly approved immutable
	// profile after its executable has legitimately changed in the bundle.
	run.Portable.Frozen.Input = staleInput
	run.Portable.Frozen.Profiles = []portableResearchProfile{basis.Portable.Frozen.Profiles[0], stale}
	run.Revision = portableResearchRevision(run.Portable.Frozen)
	store := portableResearchStoreFixture(t, run, false)
	if _, err = store.change(func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
		t.Fatal(err)
	}
	s.executePortableResearch(t.Context(), store, run)
	held, err := store.read()
	if err != nil || held.State != "blocked" || !strings.Contains(held.Problem, "page adapter changed") || len(held.Attempts) != 0 || calls.Load() != 0 {
		t.Fatalf("stale binding acquired state=%s problem=%s attempts=%d calls=%d err=%v", held.State, held.Problem, len(held.Attempts), calls.Load(), err)
	}
}

func TestPortableScopedPageRefusedBeforeAnyAcquisition(t *testing.T) {
	var currentURL atomic.Value
	currentURL.Store("https://elsewhere.example/guides")
	var calls atomic.Int32
	s, ts, basis := pageBindingFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		action, _ := json.Marshal(map[string]any{"kind": "page", "arguments": map[string]string{"url": currentURL.Load().(string)}})
		portableTestReply(w, string(action))
	})
	scope := EndpointScope{"https://docs.example/guides"}
	code, preview, raw := pageProfileRequest(t, ts, scope, "", "")
	if code != 200 {
		t.Fatalf("preview %d %s", code, raw)
	}
	code, registered, raw := pageProfileRequest(t, ts, scope, preview.Revision, "")
	if code != 200 {
		t.Fatalf("register %d %s", code, raw)
	}
	var details portableProfileDetails
	json.Unmarshal(registered.Entry.Profile, &details)
	var input map[string]any
	json.Unmarshal(basis.Portable.Frozen.Input, &input)
	source := input["source"].(map[string]any)["mapping"].(map[string]any)["sources"].([]any)[0].(map[string]any)
	source["capabilities"] = []string{"answer", "page"}
	source["webProfiles"] = map[string]any{"page": map[string]string{"profile": details.ID, "profileDigest": registered.Entry.Digest}}
	encoded, _ := json.Marshal(input)
	for _, url := range []string{"https://elsewhere.example/guides", "https://docs.example/guides-archive", "https://docs.example/guides/%2e/x", "https://docs.example/guides?bad=%"} {
		t.Run(url, func(t *testing.T) {
			currentURL.Store(url)
			calls.Store(0)
			run, err := s.reviewPortableResearch(t.Context(), encoded, basis.Portable.Frozen.Choice, inputResearchLimits{Records: 1, ModelCalls: 3, Pages: 3}, "", "fixture-scope")
			if err != nil {
				t.Fatal(err)
			}
			store := portableResearchStoreFixture(t, run, false)
			if _, err = store.change(func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
				t.Fatal(err)
			}
			s.executePortableResearch(t.Context(), store, run)
			held, err := store.read()
			if err != nil || held.State != "partial" || !strings.Contains(held.Problem, "acquisition-invalid") || calls.Load() != 3 || len(held.Attempts) != 3 {
				t.Fatalf("scope admission failed state=%s problem=%s models=%d attempts=%d err=%v", held.State, held.Problem, calls.Load(), len(held.Attempts), err)
			}
			for _, attempt := range held.Attempts {
				if attempt.Kind != "model" {
					t.Fatal("refused page reached acquisition journal", attempt.Kind)
				}
			}
			if researchBudgetUsage(held).Pages != 0 {
				t.Fatal("refused page spent source allowance")
			}
		})
	}
}

// A page this run acquired and then refused at its receipt check (its final
// address after redirects outside the scope) is not acquired again: a request
// for it is the same acquisition-invalid validation finding, within the same
// correction bound, and spends no further page allowance.
func TestPortableScopedPageRefusedAtItsReceiptIsNotRequestedAgain(t *testing.T) {
	const requested = "https://docs.example/guides/moved"
	var calls atomic.Int32
	s, ts, basis := pageBindingFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		action, _ := json.Marshal(map[string]any{"kind": "page", "arguments": map[string]string{"url": requested}})
		portableTestReply(w, string(action))
	})
	scope := EndpointScope{"https://docs.example/guides"}
	code, preview, raw := pageProfileRequest(t, ts, scope, "", "")
	if code != 200 {
		t.Fatalf("preview %d %s", code, raw)
	}
	code, registered, raw := pageProfileRequest(t, ts, scope, preview.Revision, "")
	if code != 200 {
		t.Fatalf("register %d %s", code, raw)
	}
	var details portableProfileDetails
	json.Unmarshal(registered.Entry.Profile, &details)
	var input map[string]any
	json.Unmarshal(basis.Portable.Frozen.Input, &input)
	source := input["source"].(map[string]any)["mapping"].(map[string]any)["sources"].([]any)[0].(map[string]any)
	source["capabilities"] = []string{"answer", "page"}
	source["webProfiles"] = map[string]any{"page": map[string]string{"profile": details.ID, "profileDigest": registered.Entry.Digest}}
	encoded, _ := json.Marshal(input)
	run, err := s.reviewPortableResearch(t.Context(), encoded, basis.Portable.Frozen.Choice, inputResearchLimits{Records: 1, ModelCalls: 3, Pages: 3}, "", "fixture-scope")
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := s.researchVerifier()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := researchCanon(t.Context(), verifier, pageBindingJSON(t, map[string]string{"url": requested}))
	if err != nil {
		t.Fatal(err)
	}
	key := digestOf(append([]byte("page\n"), identity...))
	kept := pageBindingJSON(t, map[string]any{"profile": map[string]string{"id": details.ID, "digest": registered.Entry.Digest}, "request": map[string]string{"url": requested}, "result": map[string]any{}, "receipt": map[string]any{"acquisition": map[string]any{"endpoint": "https://elsewhere.example/landing"}, "signature": "kept"}, "salts": map[string]any{}})
	store := portableResearchStoreFixture(t, run, false)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = store.change(func(r *inputResearchRun) error {
		r.State = "running"
		r.Attempts = append(r.Attempts, inputResearchAttempt{ID: "attempt_" + key, Kind: "page", Digest: key, State: "refused", Number: 1, StartedAt: now, FinishedAt: now, URL: requested, Result: kept, Problem: errScopedPageReceipt.Error(), ProblemCode: "acquisition-invalid"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.executePortableResearch(t.Context(), store, run)
	held, err := store.read()
	if err != nil || held.State != "partial" || !strings.Contains(held.Problem, errScopedPageRefusedAgain.Error()) || calls.Load() != 3 || len(held.Attempts) != 4 {
		t.Fatalf("refused page requested again: state=%s problem=%s models=%d attempts=%d err=%v", held.State, held.Problem, calls.Load(), len(held.Attempts), err)
	}
	for _, attempt := range held.Attempts[1:] {
		if attempt.Kind != "model" || attempt.ActionProblem != errScopedPageRefusedAgain.Error() {
			t.Fatalf("a refused page was dispatched again or its finding changed: %+v", attempt)
		}
	}
	if researchBudgetUsage(held).Pages != 1 || !bytes.Equal(held.Attempts[0].Result, kept) || held.Attempts[0].State != "refused" {
		t.Fatal("the refused page spent more allowance or lost its kept receipt")
	}
}

func TestPortablePageBindingRouteGuards(t *testing.T) {
	s, ts := startDesk(t, Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken})
	t.Cleanup(func() { ts.Close(); s.Close() })
	request, err := http.NewRequest("POST", ts.URL+"/api/portable-research/page-binding", strings.NewReader(`{"endpointScope":["https://docs.example"]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := ts.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("unauthenticated profile route admitted", response.StatusCode)
	}
	// Round 16's path form holds at registration: an escaped slash or a dot
	// segment before a path parameter is refused before any adapter is run.
	for _, scope := range []any{nil, []string{}, []string{"http://docs.example"}, []string{"https://docs.example", "https://docs.example"}, []string{"https://docs.example/guides%2f..%2fadmin"}, []string{"https://docs.example/guides/..;/admin"}} {
		code, _ := aiTestRequest(t, ts, "POST", "/api/portable-research/page-binding", map[string]any{"endpointScope": scope}, nil)
		if code != 400 {
			t.Fatal("malformed scope admitted", scope, code)
		}
	}
}
