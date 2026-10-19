package desk

// The specification's reader controls for the endpoint scope, RFC 0018
// rounds 15 and 16 (judgment-pack-spec tests/test_dialect_scope.py at
// 4bec33a, endpoint_scope_cases), held on the Desk's own form and matcher.
// Each control keeps the specification's name and answer. The Desk answers it
// where the Desk decides it: a scope at registration (validatePageScope), the
// profile's form at admission (scopedPageProfile), and each receipt endpoint
// at the check after acquisition (admitPortablePageReceipt). A reading's
// endpoints are admitted as requested URLs too (requestedPageInScope), and a
// round-16 path is refused there as well.
//
// The controls the Desk does not decide stay with Runner's verifier and are
// named in specScopeVerifierOnly: a digest pin, a request commitment, a
// receipt signature, replay of a requested URL, the exact adapter and shape a
// receipt names, an operation binding and a historical exact profile.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// specScopeRound16Refused is the specification's ROUND_16_REFUSED, in its
// order: paths under the prefix that round 15's form admitted and round 16's
// refuses.
var specScopeRound16Refused = []string{
	"/a%2fb", "/a%2Fb", "/a%5cb", "/a%5Cb", "/%2e%2e%2fadmin", "/..;x", "/..;/admin",
	"/%2e%2e;x", "/%2E%2e;/admin", "/.;", "/.;/admin", "/.%2e;y=1", "/x/..;",
	"/..%3b/admin", "/%2e%2e%3B/admin", "/.%3fx", "/..%3F", "/..%23x", "/.%00/x",
	"/..;;x", "/..;a=b;c/admin", "/..;x%3by", "/..%23x;y",
}

// specScopeVerifierOnly are the controls whose answer is Runner's verifier's
// alone (§5.2 checks 2 to 4, the profile digest and an operation binding).
var specScopeVerifierOnly = []string{
	"scope changed scope with old pin", "scope changed request", "scope changed receipt without signature",
	"scope requested URL is not scope-checked in replay", "scope exact shape still held",
	"scope exact adapter still held", "scope on operation", "scope exact historical profile unchanged",
}

type specScopeControl struct {
	name   string
	answer string // the specification's reader's answer
	// scope is the JSON value of endpointScope; empty is the prefix alone.
	scope string
	// endpoints are the retained receipts' endpoints; nil is the two defaults.
	endpoints []any
	// profile edits the scoped page profile; admission marks a control whose
	// profile the Desk's admission refuses before any acquisition, where the
	// specification's reader refuses the profile or finds it mismatched.
	profile   func(*portableProfileDetails)
	admission bool
}

func specScopeControls() []specScopeControl {
	const prefix = deskScopePrefix
	invalid, malformed, overBound, mismatch := "finding:acquisition-invalid", "refusal:input-malformed", "refusal:input-over-bound", "finding:profile-mismatch"
	scope := func(entries ...any) string { return string(scopeJSON(entries)) }
	endpoints := func(e ...any) []any { return e }
	record := func(p *portableProfileDetails) { p.Class = "record" }
	controls := []specScopeControl{
		{name: "two different page endpoints", answer: "reading"},
		{name: "another host", answer: invalid, endpoints: endpoints("https://other.example/docs/one")},
		{name: "second response outside", answer: invalid, endpoints: endpoints(prefix, "https://other.example/docs")},
		{name: "segment boundary", answer: invalid, endpoints: endpoints(prefix + "x")},
		{name: "equal path", answer: "reading", endpoints: endpoints(prefix)},
		{name: "host case and default port", answer: "reading", endpoints: endpoints("https://A.EXAMPLE:443/docs/one")},
		{name: "written default port in scope", answer: "reading", scope: scope("https://A.EXAMPLE:443/docs")},
		{name: "empty endpoint port", answer: "reading", endpoints: endpoints("https://a.example:/docs/x")},
		{name: "empty endpoint port with written scope port", answer: "reading", scope: scope("https://a.example:443/docs"), endpoints: endpoints("https://a.example:/docs/x")},
		{name: "empty scope port", answer: "reading", scope: scope("https://a.example:/docs"), endpoints: endpoints("https://a.example/docs/x")},
		{name: "record wildcard", answer: malformed, scope: scope(pageScopeWildcard), profile: record, admission: true},
		{name: "record wildcard without research", answer: malformed, scope: scope(prefix, pageScopeWildcard), profile: func(p *portableProfileDetails) { p.Class, p.Research.Capabilities = "record", nil }, admission: true},
		{name: "trailing prefix slash", answer: "reading", scope: scope(prefix + "/")},
		{name: "trailing prefix excludes parent", answer: invalid, scope: scope(prefix + "/"), endpoints: endpoints(prefix)},
		{name: "root admits absent path", answer: "reading", scope: scope("https://a.example/"), endpoints: endpoints("https://a.example")},
		{name: "absent prefix path", answer: "reading", scope: scope("https://a.example"), endpoints: endpoints("https://a.example/")},
		{name: "union of prefixes", answer: "reading", scope: scope("https://other.example", prefix)},
		{name: "wildcard", answer: "reading", scope: scope(pageScopeWildcard), endpoints: endpoints("https://new.example/a", "https://next.example/b")},
		{name: "path case", answer: invalid, endpoints: endpoints("https://a.example/Docs/one")},
		{name: "host suffix", answer: invalid, endpoints: endpoints("https://sub.a.example/docs")},
		{name: "escaped slash at the prefix boundary is outside the form", answer: invalid, endpoints: endpoints(prefix + "%2Fx")},
		{name: "encoded spelling", answer: "reading", scope: scope(prefix + "/%7e"), endpoints: endpoints(prefix + "/%7e/x")},
		{name: "escape case differs", answer: invalid, scope: scope(prefix + "/%7e"), endpoints: endpoints(prefix + "/%7E")},
		{name: "no escape decoding", answer: invalid, scope: scope(prefix + "/%7e"), endpoints: endpoints(prefix + "/~")},
		{name: "repeated slash preserved", answer: invalid, scope: scope(prefix + "//x"), endpoints: endpoints(prefix + "/x")},
		{name: "path at sign is not user", answer: "reading", endpoints: endpoints(prefix + "/@name")},
	}
	for _, path := range specScopeRound16Refused {
		controls = append(controls,
			specScopeControl{name: "round 16 endpoint " + path, answer: invalid, endpoints: endpoints(prefix + path)},
			specScopeControl{name: "round 16 scope entry " + path, answer: malformed, scope: scope(prefix + path)})
	}
	controls = append(controls,
		specScopeControl{name: "round 16 parameter on an ordinary segment", answer: "reading", endpoints: endpoints(prefix + "/page;v=1")},
		specScopeControl{name: "round 16 parameter in a scope entry", answer: "reading", scope: scope(prefix + "/a;b"), endpoints: endpoints(prefix + "/a;b/x")},
		specScopeControl{name: "round 16 dots after the first parameter", answer: "reading", endpoints: endpoints(prefix + "/x;../y")},
		specScopeControl{name: "round 16 three dots with a parameter", answer: "reading", endpoints: endpoints(prefix + "/...;x")},
		specScopeControl{name: "round 16 escaped dot inside a segment", answer: "reading", endpoints: endpoints(prefix + "/a%2eb")},
		specScopeControl{name: "round 16 dots after an escaped delimiter", answer: "reading", endpoints: endpoints(prefix + "/x%3b../y")},
	)
	for _, endpoint := range []any{
		nil, "http://a.example/docs", "https://a.example:444/docs", "https://a.example:0443/docs",
		"https://a.example:0/docs", "https://a.example:80/docs", "https://a.example:4430/docs",
		"https://user@a.example/docs", prefix + "?x", prefix + "#x",
		"https://127.0.0.1/docs", "https://[::1]/docs", "https://127.1/docs",
		"https://localhost/docs", "https://a.example./docs", "https://a_b.example/docs",
		"https://-a.example/docs", "https://a-.example/docs", "https://a..example/docs",
		"https://" + strings.Repeat("a", 64) + ".example/docs", "https://a.123/docs", "https://0x7f.0x0.0x0.0x1/docs", "https://é.example/docs",
		prefix + "/%", prefix + "/%2", prefix + "/%gg", prefix + "/\\x", prefix + "/x\n",
		prefix + "/.", prefix + "/../x", prefix + "/%2e/x", prefix + "/.%2E/x",
		prefix + "/%2E%2e/x", prefix + "/%2e./x", prefix + "/x/..", "https://*/docs",
		"HTTPS://a.example/docs", "https://a.example/" + strings.Repeat("x", 2031),
	} {
		controls = append(controls, specScopeControl{name: "invalid endpoint " + string(scopeJSON(endpoint)), answer: invalid, scope: scope(pageScopeWildcard), endpoints: endpoints(endpoint)})
	}
	for _, bad := range []string{
		`[]`, `{}`, `"https://*"`, `[null]`, scope(prefix, prefix), scope("https://*/docs"),
		scope("http://a.example"), scope(prefix + "/.."), scope(prefix + "?x"),
		scope("https://127.0.0.1"), scope("https://a.example:444"), scope("https://a.example./"),
		scope("https://A.EXAMPLE/%2E/"), scope("https://a.example/" + strings.Repeat("x", 2031)),
	} {
		answer := malformed
		if strings.Contains(bad, strings.Repeat("x", 2031)) {
			answer = overBound
		}
		controls = append(controls, specScopeControl{name: "bad scope " + bad[:min(65, len(bad))], answer: answer, scope: bad})
	}
	controls = append(controls, specScopeControl{name: "null scope", answer: malformed, scope: `null`})
	for _, edit := range []struct {
		name string
		edit func(*portableProfileDetails)
	}{
		{"exact endpoint beside scope", func(p *portableProfileDetails) { p.Endpoint = prefix }},
		{"wrong adapter", func(p *portableProfileDetails) { p.Adapter.Name = "adapter-sources" }},
		{"wrong shape", func(p *portableProfileDetails) { p.Shape = "airbyte" }},
		{"search declaration", func(p *portableProfileDetails) { p.Research.Capabilities = []string{"search", "page"} }},
		{"answer declaration", func(p *portableProfileDetails) {
			p.Research.Capabilities, p.Research.Model = []string{"answer", "page"}, "synthetic-research-model-1"
		}},
		{"no generated declaration", func(p *portableProfileDetails) { p.Research.Capabilities = nil }},
	} {
		controls = append(controls, specScopeControl{name: edit.name, answer: malformed, profile: edit.edit, admission: true})
	}
	longest := "https://a.example/" + strings.Repeat("x", 2048-len("https://a.example/"))
	thirtyTwo := []any{prefix}
	for i := range 31 {
		thirtyTwo = append(thirtyTwo, fmt.Sprintf("https://site%d.example", i))
	}
	thirtyThree := []any{}
	for range 33 {
		thirtyThree = append(thirtyThree, prefix)
	}
	command := func(p *portableProfileDetails) { p.Shape = "command" }
	controls = append(controls,
		specScopeControl{name: "record profile on research", answer: mismatch, profile: record, admission: true},
		specScopeControl{name: "32 entries", answer: "reading", scope: string(scopeJSON(thirtyTwo))},
		specScopeControl{name: "33 entries before forms", answer: overBound, scope: string(scopeJSON(thirtyThree)), profile: command},
		specScopeControl{name: "2048 byte entry", answer: "reading", scope: scope(longest), endpoints: endpoints(longest)},
		specScopeControl{name: "2049 bytes before forms", answer: overBound, scope: scope(longest + "x"), profile: command},
		specScopeControl{name: "UTF8 bytes before grammar", answer: overBound, scope: scope(strings.Repeat("é", 1025))},
	)
	return controls
}

func scopeJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}

// deskScopeAnswer is the Desk's answer to a control, in the specification's
// vocabulary, and "admission-refused" for a profile its admission refuses.
func deskScopeAnswer(t *testing.T, c specScopeControl) string {
	t.Helper()
	raw := c.scope
	if raw == "" {
		raw = string(scopeJSON([]string{deskScopePrefix}))
	}
	var scope EndpointScope
	if json.Unmarshal([]byte(raw), &scope) != nil {
		return "refusal:input-malformed"
	}
	if err := validatePageScope(scope); err != nil {
		code, _, _ := strings.Cut(err.Error(), ":")
		return "refusal:" + code
	}
	profile := scopedDeskPageProfile()
	profile.EndpointScope = scope
	if c.profile != nil {
		c.profile(&profile)
	}
	if !scopedPageProfile(profile) {
		return "admission-refused"
	}
	endpoints := c.endpoints
	if endpoints == nil {
		endpoints = []any{deskScopePrefix + "/one", deskScopePrefix + "/two"}
	}
	answer := "reading"
	for _, endpoint := range endpoints {
		retained := scopeJSON(map[string]any{"receipt": map[string]any{"acquisition": map[string]any{"endpoint": endpoint}}})
		if admitPortablePageReceipt(profile, retained) != nil {
			answer = "finding:acquisition-invalid"
		}
	}
	if answer == "reading" {
		for _, endpoint := range endpoints {
			if text, _ := endpoint.(string); !requestedPageInScope(text, scope) || admitPortablePage(profile, scopeJSON(map[string]string{"url": text})) != nil {
				t.Errorf("admission refuses %q, which its receipt check admits", text)
			}
		}
	}
	return answer
}

// Every control the specification's reader answers on the scope's form or
// matching is answered alike by the Desk.
func TestDeskAnswersTheSpecificationsScopeControls(t *testing.T) {
	controls := specScopeControls()
	if len(controls)+len(specScopeVerifierOnly) != 151 {
		t.Fatalf("%d controls ported and %d left to the verifier, not the specification's 151", len(controls), len(specScopeVerifierOnly))
	}
	// The specification answers its 151 controls 75 acquisition-invalid, 45
	// input-malformed, 24 readings, 4 input-over-bound and 3 profile-mismatch;
	// the verifier's alone take 4, 0, 2, 0 and 2 of them.
	tally := map[string]int{}
	for _, c := range controls {
		tally[c.answer]++
	}
	if want := map[string]int{"finding:acquisition-invalid": 71, "refusal:input-malformed": 45, "reading": 22, "refusal:input-over-bound": 4, "finding:profile-mismatch": 1}; !sameEvaluationValue(tally, want) {
		t.Fatalf("ported answers %v, not %v", tally, want)
	}
	agreed := 0
	for _, c := range controls {
		t.Run(c.name, func(t *testing.T) {
			got := deskScopeAnswer(t, c)
			want := c.answer
			if c.admission {
				if want != "refusal:input-malformed" && want != "finding:profile-mismatch" {
					t.Fatalf("an admission control the reader answers %s", want)
				}
				want = "admission-refused"
			}
			if got != want {
				t.Fatalf("the Desk answers %s; the specification's reader %s", got, c.answer)
			}
			agreed++
		})
	}
	t.Logf("the Desk agrees with the specification's reader on %d of %d controls; %d are the verifier's alone", agreed, len(controls), len(specScopeVerifierOnly))
}

// Round 16's refusals hold at every place a scoped page profile is used: its
// registration's scope, each requested URL before acquisition, with or without
// a query, and each receipt endpoint after acquisition.
func TestRound16PathFormAtRegistrationAdmissionAndReceipt(t *testing.T) {
	profile := scopedDeskPageProfile()
	for _, path := range specScopeRound16Refused {
		url := deskScopePrefix + path
		if err := validatePageScope(EndpointScope{url}); err == nil || !strings.HasPrefix(err.Error(), "input-malformed:") {
			t.Errorf("registration admits the scope entry %q: %v", url, err)
		}
		for _, requested := range []string{url, url + "?q=1"} {
			if err := admitPortablePage(profile, scopeJSON(map[string]string{"url": requested})); err == nil || !strings.HasPrefix(err.Error(), "acquisition-invalid:") {
				t.Errorf("admission admits %q: %v", requested, err)
			}
		}
		retained := scopeJSON(map[string]any{"receipt": map[string]any{"acquisition": map[string]any{"endpoint": url}}})
		if err := admitPortablePageReceipt(profile, retained); err == nil || !strings.HasPrefix(err.Error(), "acquisition-invalid:") {
			t.Errorf("the receipt check admits %q: %v", url, err)
		}
	}
}

// The receipt check reads the signed receipt's acquisition.endpoint exactly:
// a missing, null, non-string or duplicated member is refused, a profile
// without a scope is left to Runner's exact comparison, and an in-scope
// endpoint is admitted however the request was spelled.
func TestScopedPageReceiptCheckReadsTheReceiptEndpoint(t *testing.T) {
	profile := scopedDeskPageProfile()
	for _, retained := range []string{
		`{"receipt":{"acquisition":{"endpoint":"https://a.example/docs/one"}}}`,
		`{"request":{"url":"https://elsewhere.example/start?x=1"},"receipt":{"acquisition":{"endpoint":"https://A.EXAMPLE:/docs/one"}}}`,
	} {
		if err := admitPortablePageReceipt(profile, json.RawMessage(retained)); err != nil {
			t.Errorf("refused %s: %v", retained, err)
		}
	}
	for _, retained := range []string{
		`{}`, `{"receipt":null}`, `{"receipt":{}}`, `{"receipt":{"acquisition":null}}`,
		`{"receipt":{"acquisition":{}}}`, `{"receipt":{"acquisition":{"endpoint":null}}}`,
		`{"receipt":{"acquisition":{"endpoint":7}}}`, `{"receipt":{"acquisition":{"Endpoint":"https://a.example/docs/one"}}}`,
		`{"receipt":{"acquisition":{"endpoint":"https://a.example/docs/one","endpoint":"https://outside.example/x"}}}`,
		`{"receipt":{"acquisition":{"endpoint":"https://outside.example/x","endpoint":"https://a.example/docs/one"}}}`,
		`{"receipt":{"acquisition":{"endpoint":"https://a.example/docs/one?x=1"}}}`,
		`{"receipt":{"acquisition":{"endpoint":"https://example.com/outside/company"}}}`,
	} {
		if err := admitPortablePageReceipt(profile, json.RawMessage(retained)); err == nil || !strings.HasPrefix(err.Error(), "acquisition-invalid:") {
			t.Errorf("admitted %s: %v", retained, err)
		}
	}
	legacy := portableProfileDetails{Shape: "http", Adapter: searchBindingAdapter{Name: "adapter-web"}, Endpoint: "https://legacy.example/final"}
	if err := admitPortablePageReceipt(legacy, json.RawMessage(`{"receipt":{"acquisition":{"endpoint":"https://legacy.example/final"}}}`)); err != nil {
		t.Fatal("a profile without a scope was held to one", err)
	}
	if strings.Contains(errScopedPageReceipt.Error(), "http") || strings.Contains(errScopedPageRefusedAgain.Error(), "http") {
		t.Fatal("the finding the model reads names a URL")
	}
}

// A page refused at its receipt check finishes "refused" with its retained
// response kept for audit: it counts against the page allowance, is no
// source, is never continued or requested again, and its state and receipt
// cannot be rewritten.
func TestRefusedPageAcquisitionIsKeptForAuditAndNeverASource(t *testing.T) {
	root, _ := researchRunFixture(t)
	store := fileResearchStore(root)
	args := json.RawMessage(`{"url":"https://example.com/fixtures/leave-scope"}`)
	retained := json.RawMessage(`{"receipt":{"acquisition":{"endpoint":"https://example.com/outside/company"},"signature":"kept"}}`)
	calls := 0
	_, err := researchAttempt(t.Context(), store, "page", args, func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		calls++
		return retained, &researchAcquisitionRefused{code: "acquisition-invalid", cause: errScopedPageReceipt}
	})
	var refused *researchAcquisitionRefused
	if !errors.As(err, &refused) || !errors.Is(err, errScopedPageReceipt) {
		t.Fatal("the refusal was not returned as a refusal", err)
	}
	held, err := store.read()
	if err != nil || len(held.Attempts) != 1 {
		t.Fatal(err)
	}
	a := held.Attempts[0]
	if a.State != "refused" || a.ProblemCode != "acquisition-invalid" || a.Problem != errScopedPageReceipt.Error() || !bytes.Equal(a.Result, retained) || a.FinishedAt == "" || a.ReservedBytes != 0 {
		t.Fatalf("refused attempt not kept for audit: %+v", a)
	}
	if held.RetrievedBytes != len(retained) || researchBudgetUsage(held).Pages != 1 || researchContinuableAttempt(a) {
		t.Fatal("refused page escaped its allowance or became continuable")
	}
	if _, err = researchAttempt(t.Context(), store, "page", args, func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error) {
		calls++
		return retained, nil
	}); err == nil || calls != 1 {
		t.Fatal("a refused page was requested again", err, calls)
	}
	for _, rewrite := range []func(*inputResearchAttempt){
		func(a *inputResearchAttempt) { a.State = "completed" },
		func(a *inputResearchAttempt) { a.Result = json.RawMessage(`{}`) },
	} {
		if _, err = store.change(func(r *inputResearchRun) error { rewrite(&r.Attempts[0]); return nil }); err == nil {
			t.Fatal("a refused attempt was rewritten")
		}
	}
}

// A page refused at its receipt check withholds no bounded allowance offer,
// as a page refused before acquisition withholds none; an uncertain
// operation still does.
func TestRefusedPageDoesNotWithholdTheAllowanceOffer(t *testing.T) {
	limits := inputResearchLimits{Records: 1, ModelCalls: 2, Pages: 1}
	run := inputResearchRun{ID: "research_" + strings.Repeat("c", 32), State: "partial", Problem: errResearchRequestLimit.Error(), Portable: &portableResearchJournal{Frozen: portableResearchFrozen{Limits: limits, Plan: portableResearchPlan{Arguments: json.RawMessage(`{"capabilities":["answer","page"]}`)}}}}
	run.Attempts = []inputResearchAttempt{{ID: "m1", Kind: "model", State: "completed"}, {ID: "p1", Kind: "page", State: "refused", ProblemCode: "acquisition-invalid"}, {ID: "m2", Kind: "model", State: "completed"}}
	if offer := portableResearchBudgetProposal(run); offer == nil || offer.Proposed.ModelCalls <= limits.ModelCalls || offer.Proposed.Pages <= limits.Pages {
		t.Fatalf("a refused page withheld the allowance offer: %+v", offer)
	}
	run.Attempts[1].State = "uncertain"
	if portableResearchBudgetProposal(run) != nil {
		t.Fatal("an uncertain operation no longer withholds the offer")
	}
}
