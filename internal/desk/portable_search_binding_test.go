package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPortableSearchArgumentsHoldReviewedConnection(t *testing.T) {
	profile := portableProfileDetails{ID: "desk-search-" + strings.Repeat("a", 32), Source: "web-search"}
	run := inputResearchRun{Portable: &portableResearchJournal{Frozen: portableResearchFrozen{SearchBindings: map[string]searchBindingDescriptor{profile.ID: {FixedArguments: map[string]string{"connection": "reviewed", "revision": strings.Repeat("a", 64)}}}}}}
	raw, err := portableSearchArguments(run, profile, json.RawMessage(`{"query":"policies","maxResults":2}`))
	if err != nil {
		t.Fatal(err)
	}
	var args map[string]any
	json.Unmarshal(raw, &args)
	if args["connection"] != "reviewed" || args["revision"] != strings.Repeat("a", 64) || args["query"] != "policies" {
		t.Fatal("fixed binding not preserved", string(raw))
	}
	for _, bad := range []string{`null`, `{"connection":"other"}`, `{"revision":"other"}`, `{"connection":null}`, `{"connection":"reviewed","connection":"other"}`} {
		if _, err := portableSearchArguments(run, profile, json.RawMessage(bad)); err == nil {
			t.Fatal("accepted substituted binding", bad)
		}
	}
	delete(run.Portable.Frozen.SearchBindings, profile.ID)
	if _, err := portableSearchArguments(run, profile, json.RawMessage(`{}`)); err == nil {
		t.Fatal("legacy unreviewed search dispatched")
	}
	custom := portableProfileDetails{ID: "custom", Source: "web-search"}
	raw, err = portableSearchArguments(run, custom, json.RawMessage(`{"query":"custom"}`))
	if err != nil || string(raw) != `{"query":"custom"}` {
		t.Fatal("custom reviewed profile rewritten")
	}
}

func TestPortableSearchBindingReviewAndImmutableRegistration(t *testing.T) {
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if bundle == "" || os.Getenv("JPACK_RUNNER_TEST_BIN") == "" || os.Getenv("JPACK_BIN") == "" {
		t.Skip("requires pinned Gateway, Runner and Runtime")
	}
	// A reviewed development bundle is admitted only within this fixture. Full
	// suite synthetic bundles must retain their independent manifest trust setup.
	manifest, err := os.ReadFile(filepath.Join(bundle, "gateway-bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("JPACK_DESK_GATEWAY_MANIFEST_SHA256", digestOf(manifest))
	var calls atomic.Int32
	s, ts, base := portableResearchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		portableTestReply(w, `{"kind":"answer","fields":{"eligible":{"status":"found","value":false,"cites":[0]}}}`)
	})
	companion := s.connectionCompanion("web-search", true)
	call := func(method string, body any) json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(body)
		out, err := companion.call(t.Context(), bundle, platformPath(s.assistant.dir, "gateway-connections"), method, raw, "web-search", false)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	call("configure", map[string]any{"id": "reviewed", "name": "Reviewed search", "provider": "tavily", "credential": "tvly-test-private-secret", "dailyLimit": 1})
	before := call("status", map[string]any{})
	var status struct {
		Connections []struct {
			ID       string `json:"id"`
			Revision string `json:"revision"`
			Requests int    `json:"requests"`
		}
	}
	if json.Unmarshal(before, &status) != nil || len(status.Connections) != 1 {
		t.Fatal("missing search connection")
	}
	choice := inputResearchSearch{Connection: status.Connections[0].ID, Revision: status.Connections[0].Revision}
	code, review := aiTestRequest(t, ts, "POST", "/api/portable-research/search-binding", map[string]any{"choice": choice}, nil)
	if code != 200 || bytes.Contains(review, []byte("tvly-test-private-secret")) {
		t.Fatalf("review %d %s", code, review)
	}
	var value struct {
		Revision   string                  `json:"revision"`
		Descriptor searchBindingDescriptor `json:"descriptor"`
		Entry      portableResearchProfile `json:"entry"`
	}
	if json.Unmarshal(review, &value) != nil || value.Revision == "" || len(value.Entry.Profile) != 0 {
		t.Fatal("review wrote a binding")
	}
	code, registered := aiTestRequest(t, ts, "POST", "/api/portable-research/search-binding", map[string]any{"choice": choice, "reviewedDigest": value.Revision}, nil)
	if code != 200 || json.Unmarshal(registered, &value) != nil || len(value.Entry.Profile) == 0 {
		t.Fatalf("register %d %s", code, registered)
	}
	code, again := aiTestRequest(t, ts, "POST", "/api/portable-research/search-binding", map[string]any{"choice": choice, "reviewedDigest": value.Revision}, nil)
	if code != 200 || !bytes.Equal(again, registered) {
		t.Fatal("registration not idempotent")
	}
	if !bytes.Equal(before, call("status", map[string]any{})) || calls.Load() != 0 {
		t.Fatal("review/registration charged search or called AI")
	}
	var details portableProfileDetails
	json.Unmarshal(value.Entry.Profile, &details)
	var input map[string]any
	json.Unmarshal(base.Portable.Frozen.Input, &input)
	source := input["source"].(map[string]any)["mapping"].(map[string]any)["sources"].([]any)[0].(map[string]any)
	source["capabilities"] = []string{"answer", "search"}
	source["webProfiles"] = map[string]any{"search": map[string]string{"profile": details.ID, "profileDigest": value.Entry.Digest}}
	encoded, _ := json.Marshal(input)
	run, err := s.reviewPortableResearch(t.Context(), encoded, base.Portable.Frozen.Choice, inputResearchLimits{Records: 1, ModelCalls: 3, Searches: 1}, "", "fixture-scope")
	if err != nil {
		t.Fatal(err)
	}
	if run.Portable.Frozen.SearchBindings[details.ID].FixedArguments["revision"] != choice.Revision {
		t.Fatal("reviewed binding not frozen")
	}
	store := portableResearchStoreFixture(t, run, false)
	if _, err := store.change(func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
		t.Fatal(err)
	}
	s.executePortableResearch(t.Context(), store, run)
	held, err := store.read()
	if err != nil || held.State != "completed" || calls.Load() != 1 {
		t.Fatal("search-capable portable result incomplete", err, held.State, held.Problem)
	}
	// An operator-owned profile keeps the pre-existing generic path even when
	// its configured source is the same web-search adapter.
	var custom map[string]any
	json.Unmarshal(value.Entry.Profile, &custom)
	custom["id"] = "operator-search"
	customRaw, err := s.inputRunner(t.Context(), "input-profiles", custom, "")
	if err != nil {
		t.Fatal(err)
	}
	var customEntry portableResearchProfile
	if json.Unmarshal(customRaw, &customEntry) != nil {
		t.Fatal("invalid custom profile")
	}
	source["webProfiles"] = map[string]any{"search": map[string]string{"profile": "operator-search", "profileDigest": customEntry.Digest}}
	customInput, _ := json.Marshal(input)
	generic, err := s.reviewPortableResearch(t.Context(), customInput, base.Portable.Frozen.Choice, inputResearchLimits{Records: 1, ModelCalls: 3, Searches: 1}, "", "fixture-scope")
	if err != nil || len(generic.Portable.Frozen.SearchBindings) != 0 {
		t.Fatal("operator profile was claimed by managed discovery", err)
	}
	changed := map[string]any{"id": choice.Connection, "revision": choice.Revision, "name": "Changed", "provider": "tavily", "dailyLimit": 1}
	call("configure", changed)
	code, _ = aiTestRequest(t, ts, "POST", "/api/portable-research/search-binding", map[string]any{"choice": choice, "reviewedDigest": value.Revision}, nil)
	if code != 409 {
		t.Fatal("changed connection accepted", code)
	}
	if err := s.validatePortableResearchAdmission(context.Background(), run); err == nil {
		t.Fatal("old binding silently refreshed")
	}
	if calls.Load() != 1 {
		t.Fatal("stale review dispatched")
	}
}
