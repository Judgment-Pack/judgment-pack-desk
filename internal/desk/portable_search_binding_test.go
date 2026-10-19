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

func searchBindingDescription(t *testing.T, edit func(map[string]any)) []byte {
	t.Helper()
	d := map[string]any{"version": 1, "shape": "http", "adapter": map[string]string{"name": "adapter-sources", "version": "v0.19.0", "digest": "sha256:" + strings.Repeat("a", 64)}, "endpoint": "https://api.tavily.com/search", "research": map[string]any{"capabilities": []string{"search"}}, "fixedArguments": map[string]string{"connection": "reviewed", "revision": strings.Repeat("b", 64)}, "requestSchema": map[string]any{"type": "object"}, "resultRecord": "web-search-v1"}
	if edit != nil {
		edit(d)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func versionTwo(d map[string]any) {
	d["version"] = 2
	d["requestSchemaVersion"] = "tavily-request-1"
	d["resultSchema"] = map[string]any{"type": "object"}
	d["resultSchemaVersion"] = "tavily-result-1"
}

// The check describeSearchBinding applies to what the program printed.
func readSearchBindingDescription(raw []byte) (searchBindingDescriptor, bool) {
	return admitSearchBindingDescription(raw, inputResearchSearch{Connection: "reviewed", Revision: strings.Repeat("b", 64)})
}

func TestSearchBindingDescriptionVersionsAreClosed(t *testing.T) {
	v1 := searchBindingDescription(t, nil)
	d1, ok := readSearchBindingDescription(v1)
	if !ok {
		t.Fatal("version 1 refused")
	}
	again, _ := json.Marshal(d1)
	var plain, kept map[string]any
	json.Unmarshal(v1, &plain)
	json.Unmarshal(again, &kept)
	if len(kept) != len(plain) || kept["requestSchemaVersion"] != nil || kept["resultSchema"] != nil || kept["resultSchemaVersion"] != nil {
		t.Fatal("version 1 gained members when held", string(again))
	}
	d2, ok := readSearchBindingDescription(searchBindingDescription(t, versionTwo))
	if !ok || d2.RequestSchemaVersion != "tavily-request-1" || d2.ResultSchemaVersion != "tavily-result-1" || string(d2.ResultSchema) != `{"type":"object"}` {
		t.Fatal("version 2 refused or its members lost")
	}
	if _, ok := readSearchBindingDescription(searchBindingDescription(t, func(d map[string]any) {
		versionTwo(d)
		d["requestSchemaVersion"], d["resultSchemaVersion"] = "google-grounding-request-1", "google-grounding-result-1"
	})); !ok {
		t.Fatal("google-grounding version 2 refused")
	}
	for name, edit := range map[string]func(map[string]any){
		"unknown member":                         func(d map[string]any) { versionTwo(d); d["extra"] = "x" },
		"version 3":                              func(d map[string]any) { versionTwo(d); d["version"] = 3 },
		"version 0":                              func(d map[string]any) { versionTwo(d); d["version"] = 0 },
		"version 2 without members":              func(d map[string]any) { d["version"] = 2 },
		"version 2 request version":              func(d map[string]any) { versionTwo(d); delete(d, "requestSchemaVersion") },
		"version 2 result version":               func(d map[string]any) { versionTwo(d); d["resultSchemaVersion"] = "" },
		"version 2 result schema":                func(d map[string]any) { versionTwo(d); delete(d, "resultSchema") },
		"malformed request version":              func(d map[string]any) { versionTwo(d); d["requestSchemaVersion"] = "Tavily Request 1" },
		"malformed result version":               func(d map[string]any) { versionTwo(d); d["resultSchemaVersion"] = "tavily-result-0" },
		"swapped versions":                       func(d map[string]any) { versionTwo(d); d["requestSchemaVersion"] = "tavily-result-1" },
		"version not a string":                   func(d map[string]any) { versionTwo(d); d["resultSchemaVersion"] = 1 },
		"result schema not an object":            func(d map[string]any) { versionTwo(d); d["resultSchema"] = []int{1} },
		"result schema null":                     func(d map[string]any) { versionTwo(d); d["resultSchema"] = nil },
		"version 1 with empty version 2 members": func(d map[string]any) { d["requestSchemaVersion"], d["resultSchemaVersion"] = "", "" },
		"member name in another case": func(d map[string]any) {
			versionTwo(d)
			d["ResultSchemaVersion"] = d["resultSchemaVersion"]
			delete(d, "resultSchemaVersion")
		},
		"another choice": func(d map[string]any) {
			versionTwo(d)
			d["fixedArguments"] = map[string]string{"connection": "other", "revision": strings.Repeat("b", 64)}
		},
		"version 1 with a version 2 member": func(d map[string]any) {
			d["requestSchemaVersion"] = "tavily-request-1"
		},
	} {
		if _, ok := readSearchBindingDescription(searchBindingDescription(t, edit)); ok {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestSearchBindingAdmitsAnotherProvidersSchemaNames(t *testing.T) {
	if _, ok := readSearchBindingDescription(searchBindingDescription(t, func(d map[string]any) { versionTwo(d); d["resultSchemaVersion"] = "google-grounding-result-1" })); !ok {
		t.Fatal("by design the form is checked, not the provider")
	}
}

func TestValidatePortableResearchJournalHoldsFrozenSearchBindingVersions(t *testing.T) {
	_, _, run := portableResearchFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected acquisition") })
	var profile struct {
		ID string `json:"id"`
	}
	json.Unmarshal(run.Portable.Frozen.Profiles[0].Profile, &profile)
	for name, tc := range map[string]struct {
		edit func(map[string]any)
		ok   bool
	}{"version 1": {nil, true}, "version 2": {versionTwo, true}, "version 2 without members": {func(d map[string]any) { d["version"] = 2 }, false}, "version 3": {func(d map[string]any) { versionTwo(d); d["version"] = 3 }, false}} {
		d, _ := readSearchBindingDescription(searchBindingDescription(t, nil))
		if tc.edit != nil {
			var raw map[string]any
			json.Unmarshal(searchBindingDescription(t, tc.edit), &raw)
			// The journal holds the typed form, so build it without the reader.
			encoded, _ := json.Marshal(raw)
			d = searchBindingDescriptor{}
			if err := json.Unmarshal(encoded, &d); err != nil {
				t.Fatal(err)
			}
		}
		held := run
		portable := *run.Portable
		held.Portable = &portable
		held.Portable.Frozen.SearchBindings = map[string]searchBindingDescriptor{profile.ID: d}
		held.Revision = portableResearchRevision(held.Portable.Frozen)
		if err := validatePortableResearchJournal(held, held.ID); (err == nil) != tc.ok {
			t.Fatalf("%s: journal check returned %v", name, err)
		}
	}
}

func TestPortableResearchPromptCarriesNoResultSchemaButTheJournalKeepsIt(t *testing.T) {
	d, ok := readSearchBindingDescription(searchBindingDescription(t, versionTwo))
	if !ok {
		t.Fatal("version 2 refused")
	}
	id := "desk-search-" + strings.Repeat("a", 32)
	bindings := map[string]searchBindingDescriptor{id: d}
	shown, _ := json.Marshal(promptSearchBindings(bindings))
	if bytes.Contains(shown, []byte("resultSchema\"")) || !bytes.Contains(shown, []byte("requestSchemaVersion")) || !bytes.Contains(shown, []byte("requestSchema\"")) {
		t.Fatal("prompt body wrong", string(shown))
	}
	if len(bindings[id].ResultSchema) == 0 {
		t.Fatal("the frozen binding lost its result schema")
	}
	kept, _ := json.Marshal(portableResearchFrozen{SearchBindings: bindings})
	var back portableResearchFrozen
	if decodeDataJSON(kept, &back) != nil || len(back.SearchBindings[id].ResultSchema) == 0 {
		t.Fatal("the journal dropped the result schema")
	}
}
