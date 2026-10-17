package desk

// Normative endpoint form/matching probes mirrored from Runner #142 and
// spec dialect §5.1. Keep case, percent spelling and all URL bytes intact.
import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const deskScopePrefix = "https://a.example/docs"

func TestRequestedPageScopeQueryAndBounds(t *testing.T) {
	scope := EndpointScope{deskScopePrefix}
	for _, requested := range []string{deskScopePrefix, deskScopePrefix + "?", deskScopePrefix + "/x?q=%2F&next=?/../\\x", "https://A.EXAMPLE:/docs?x=1"} {
		if !requestedPageInScope(requested, scope) {
			t.Errorf("refused submitted URL %q", requested)
		}
	}
	for _, requested := range []string{deskScopePrefix + "?bad=%", deskScopePrefix + "?bad=%2", deskScopePrefix + "?bad=%gg", deskScopePrefix + "?x#fragment", deskScopePrefix + "?x y", deskScopePrefix + "?é", deskScopePrefix + "/../x?q=1", deskScopePrefix + "x?q=1", deskScopePrefix + "?" + strings.Repeat("x", 2048)} {
		if requestedPageInScope(requested, scope) {
			t.Errorf("admitted invalid/out-of-scope URL %q", requested)
		}
	}
}

func TestPageScopeProducerBoundsAndForm(t *testing.T) {
	for _, scope := range []EndpointScope{{deskScopePrefix}, {pageScopeWildcard}, {deskScopePrefix, "https://A.EXAMPLE:443/docs"}} {
		if err := validatePageScope(scope); err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range []EndpointScope{nil, {}, {deskScopePrefix, deskScopePrefix}, {"http://a.example"}, {"https://*.example"}, {"https://a.example/%2E/"}, {"https://a.example?x"}} {
		if err := validatePageScope(scope); err == nil || !strings.HasPrefix(err.Error(), "input-malformed:") {
			t.Fatalf("scope %v: %v", scope, err)
		}
	}
	for _, scope := range []EndpointScope{make(EndpointScope, 33), {strings.Repeat("é", 1025)}} {
		if err := validatePageScope(scope); err == nil || !strings.HasPrefix(err.Error(), "input-over-bound:") {
			t.Fatalf("bounds not counted before form: %v", err)
		}
	}
	for _, raw := range []string{`null`, `{}`, `"https://*"`, `[null]`} {
		var scope EndpointScope
		if err := json.Unmarshal([]byte(raw), &scope); err == nil && validatePageScope(scope) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func scopedDeskPageProfile() portableProfileDetails {
	p := portableProfileDetails{Class: "generated", Shape: "http", Adapter: searchBindingAdapter{Name: "adapter-web"}, EndpointScope: EndpointScope{deskScopePrefix}}
	p.Research.Capabilities = []string{"page"}
	return p
}

func TestPortablePageAdmissionPreservesSubmittedArguments(t *testing.T) {
	profile := scopedDeskPageProfile()
	raw := json.RawMessage(`{ "url" : "https://A.EXAMPLE:/docs/%7e/x?query=%2F&case=UPPER", "site": "https://a.example" }`)
	before := append([]byte(nil), raw...)
	if err := admitPortablePage(profile, raw); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, raw) {
		t.Fatal("submitted page arguments changed")
	}
	for _, raw := range []string{`{}`, `{"URL":"https://a.example/docs"}`, `{"url":null}`, `{"url":"https://a.example/docs","url":"https://elsewhere.example/docs"}`, `{"url":"https://elsewhere.example/docs","url":"https://a.example/docs"}`, `{"url":"https://a.example/docs","url":"https://a.example/docs"}`, `{"url":"https://a.example/docsx"}`, `{"url":"https://a.example/docs?bad=%"}`, `{"url":"https://a.example/docs/%2e/x"}`} {
		if err := admitPortablePage(profile, json.RawMessage(raw)); err == nil || !strings.HasPrefix(err.Error(), "acquisition-invalid:") {
			t.Fatalf("dispatched %s: %v", raw, err)
		}
	}
}

func TestLegacyPageProfilesKeepExactEndpointAndExistingToolContract(t *testing.T) {
	profile := portableProfileDetails{Shape: "http", Adapter: searchBindingAdapter{Name: "adapter-web"}, Endpoint: "http://legacy.example:8080/page?x=1"}
	good := json.RawMessage(`{ "url" : "http://legacy.example:8080/redirect?x=2" }`)
	before := append([]byte(nil), good...)
	if err := admitPortablePage(profile, good); err != nil {
		t.Fatal("legacy exact endpoint narrowed by scope grammar", err)
	}
	if !bytes.Equal(before, good) {
		t.Fatal("legacy submitted request bytes changed")
	}
	profile.Shape, profile.Endpoint = "mcp", "fixture:page"
	if err := admitPortablePage(profile, json.RawMessage(`{"tool":"page"}`)); err != nil {
		t.Fatal("legacy fixed-service page tool changed", err)
	}
}

func TestPortablePageProfileLookupDoesNotInheritAnotherProfilesScope(t *testing.T) {
	run := inputResearchRun{Portable: &portableResearchJournal{Frozen: portableResearchFrozen{
		Input: json.RawMessage(`{"source":{"mapping":{"sources":[{"name":"research","profile":"answer","profileDigest":"answer-digest","webProfiles":{"page":{"profile":"legacy","profileDigest":"legacy-digest"}}}]}}}`),
		Profiles: []portableResearchProfile{
			{Profile: json.RawMessage(`{"id":"scoped","endpointScope":["https://docs.example/guides"],"adapter":{"name":"adapter-web"}}`), Digest: "scoped-digest"},
			{Profile: json.RawMessage(`{"id":"legacy","class":"generated","shape":"http","endpoint":"https://legacy.example/final","adapter":{"name":"adapter-web"},"research":{"capabilities":["page"]}}`), Digest: "legacy-digest"},
		},
	}}}
	run.Portable.Frozen.Plan.Name = "research"
	profile, err := portableResearchProfileFor(run, "page")
	if err != nil {
		t.Fatal(err)
	}
	if profile.EndpointScope != nil {
		t.Fatal("legacy profile inherited another profile's scope", profile.EndpointScope)
	}
	if err := admitPortablePage(profile, json.RawMessage(`{"url":"https://legacy.example/redirect?query=%2F"}`)); err != nil {
		t.Fatal("legacy request narrowed by an unrelated scope", err)
	}
}

// A string in the constrained page form (§5.1) is read as the text gives it,
// nothing decoded: the host folded to lower case for comparison alone, the
// path as written.
func TestDeskPageEndpointFormVectors(t *testing.T) {
	longest := "https://a.example/" + strings.Repeat("x", 2048-len("https://a.example/"))
	for endpoint, want := range map[string][2]string{
		"https://a.example":                               {"a.example", ""},
		"https://a.example/":                              {"a.example", "/"},
		"https://A.EXAMPLE:443/docs":                      {"a.example", "/docs"},
		"https://a.example:/docs/x":                       {"a.example", "/docs/x"},
		"https://a.example:443":                           {"a.example", ""},
		"https://a.example:":                              {"a.example", ""},
		"https://a.example/docs/%7e/x":                    {"a.example", "/docs/%7e/x"},
		"https://a.example/docs/%2Fx":                     {"a.example", "/docs/%2Fx"},
		"https://a.example/@name":                         {"a.example", "/@name"},
		"https://a.example/...":                           {"a.example", "/..."},
		"https://a.example/.x/x.":                         {"a.example", "/.x/x."},
		"https://a.example//x":                            {"a.example", "//x"},
		"https://a-b.c1.example/x":                        {"a-b.c1.example", "/x"},
		"https://1a.example":                              {"1a.example", ""},
		"https://" + strings.Repeat("a", 63) + ".example": {strings.Repeat("a", 63) + ".example", ""},
		longest: {"a.example", longest[len("https://a.example"):]},
	} {
		host, path, ok := constrainedPageEndpoint(endpoint)
		if !ok || host != want[0] || path != want[1] {
			t.Errorf("%s reads as %v %q %q, not %q %q", endpoint, ok, host, path, want[0], want[1])
		}
	}
	for _, endpoint := range []string{
		"", "http://a.example/docs", "HTTPS://a.example/docs", "https://", "https://a.example:444/docs",
		"https://a.example:0443/docs", "https://a.example:0/docs", "https://a.example:80/docs",
		"https://a.example:4430/docs", "https://a.example:443:443/docs", "https://user@a.example/docs",
		deskScopePrefix + "?x", deskScopePrefix + "#x", deskScopePrefix + "?", "https://a.example?x",
		"https://127.0.0.1/docs", "https://[::1]/docs", "https://127.1/docs", "https://0x7f.0x0.0x0.0x1/docs",
		"https://localhost/docs", "https://a.example./docs", "https://.a.example/docs", "https://a_b.example/docs",
		"https://-a.example/docs", "https://a-.example/docs", "https://a..example/docs", "https://a.123/docs",
		"https://" + strings.Repeat("a", 64) + ".example/docs", "https://é.example/docs", "https://*", "https://*/docs",
		"https://*.example/docs", deskScopePrefix + "/%", deskScopePrefix + "/%2", deskScopePrefix + "/%gg", deskScopePrefix + "/\\x",
		deskScopePrefix + "/x\n", deskScopePrefix + "/x y", deskScopePrefix + "/.", deskScopePrefix + "/..", deskScopePrefix + "/../x",
		deskScopePrefix + "/%2e/x", deskScopePrefix + "/%2E/x", deskScopePrefix + "/.%2E/x", deskScopePrefix + "/%2E%2e/x",
		deskScopePrefix + "/%2e./x", deskScopePrefix + "/x/..", "https://a.example/./",
		"https://a.example/" + strings.Repeat("x", 2031),
	} {
		if _, _, ok := constrainedPageEndpoint(endpoint); ok {
			t.Errorf("%q is read in the constrained page form", endpoint)
		}
	}
}

// A scope admits an endpoint when an entry does (§5.1, "Matching"): hosts
// equal with ASCII letters folded, the three spellings of port 443 one port,
// a path prefix at a literal-slash boundary, nothing decoded; the wildcard
// admits every endpoint in the form, and nothing outside it.
func TestDeskPageScopeMatchingVectors(t *testing.T) {
	for _, tt := range []struct {
		scope    []string
		endpoint string
		in       bool
	}{
		{[]string{deskScopePrefix}, deskScopePrefix + "/one", true},
		{[]string{deskScopePrefix}, deskScopePrefix, true},
		{[]string{deskScopePrefix}, deskScopePrefix + "x", false},
		{[]string{deskScopePrefix}, deskScopePrefix + "x/one", false},
		{[]string{deskScopePrefix}, "https://a.example/doc", false},
		{[]string{deskScopePrefix}, "https://a.example", false},
		{[]string{deskScopePrefix}, "https://other.example/docs/one", false},
		{[]string{deskScopePrefix}, "https://A.EXAMPLE:443/docs/one", true},
		{[]string{deskScopePrefix}, "https://a.example:/docs/x", true},
		{[]string{deskScopePrefix}, "https://a.example:443/docs", true},
		{[]string{"https://A.EXAMPLE:443/docs"}, deskScopePrefix + "/one", true},
		{[]string{"https://a.example:443/docs"}, "https://a.example:/docs/x", true},
		{[]string{"https://a.example:/docs"}, "https://a.example/docs/x", true},
		{[]string{deskScopePrefix + "/"}, deskScopePrefix + "/one", true},
		{[]string{deskScopePrefix + "/"}, deskScopePrefix + "/", true},
		{[]string{deskScopePrefix + "/"}, deskScopePrefix, false},
		{[]string{"https://a.example/"}, "https://a.example", true},
		{[]string{"https://a.example/"}, "https://a.example/anything/at/all", true},
		{[]string{"https://a.example"}, "https://a.example/", true},
		{[]string{"https://a.example"}, "https://a.example:443", true},
		{[]string{"https://other.example", deskScopePrefix}, deskScopePrefix + "/one", true},
		{[]string{"https://other.example", deskScopePrefix}, "https://other.example/x", true},
		{[]string{"https://other.example", deskScopePrefix}, "https://third.example/x", false},
		{[]string{deskScopePrefix}, "https://a.example/Docs/one", false},
		{[]string{deskScopePrefix}, "https://sub.a.example/docs", false},
		{[]string{"https://sub.a.example/docs"}, deskScopePrefix, false},
		{[]string{deskScopePrefix}, deskScopePrefix + "%2Fx", false},
		{[]string{deskScopePrefix + "/%7e"}, deskScopePrefix + "/%7e/x", true},
		{[]string{deskScopePrefix + "/%7e"}, deskScopePrefix + "/%7E", false},
		{[]string{deskScopePrefix + "/%7e"}, deskScopePrefix + "/~", false},
		{[]string{deskScopePrefix + "//x"}, deskScopePrefix + "/x", false},
		{[]string{deskScopePrefix}, deskScopePrefix + "//x", true},
		{[]string{deskScopePrefix}, deskScopePrefix + "/@name", true},
		{[]string{pageScopeWildcard}, "https://new.example/a", true},
		{[]string{pageScopeWildcard}, "https://next.example", true},
		{[]string{pageScopeWildcard}, "https://a.example:/x", true},
		{[]string{pageScopeWildcard}, "https://a.example:444/x", false},
		{[]string{pageScopeWildcard}, "http://a.example/x", false},
		{[]string{pageScopeWildcard}, "https://127.0.0.1/x", false},
		{[]string{pageScopeWildcard}, "https://a.example/x?y", false},
		{[]string{pageScopeWildcard}, deskScopePrefix + "/%2e/x", false},
		{[]string{pageScopeWildcard}, "", false},
		{[]string{deskScopePrefix}, deskScopePrefix + "/../x", false},
	} {
		if got := pageEndpointInScope(tt.endpoint, tt.scope); got != tt.in {
			t.Errorf("%v admits %q: %v, not %v", tt.scope, tt.endpoint, got, tt.in)
		}
	}
}
