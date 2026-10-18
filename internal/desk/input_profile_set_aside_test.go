package desk

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

// A profile Runner set aside (Runner v0.23.0, runner #156) is never a Desk
// candidate (desk #480): the listing serves its state and reason through, the
// review never resolves a pin to it, a frozen pin that names it is reported
// with Runner's reason, and Runner's refusal of a binding to it reaches the
// user as Desk's message, naming the profile and Runner's reason only.

// setAsideReason is a reason the stand-in lists its set-aside profile with.
const setAsideReason = "the input profile old-pages: an endpointScope entry is not an endpoint prefix in the constrained page form, nor the wildcard https://* (input-malformed)"

// setAsideListing is the stand-in's listing: two installed profiles and,
// after them, one set aside, as Runner v0.23.0 lists them.
const setAsideListing = `[{"profile":{"id":"answer-model","class":"generated","source":"model-answer","shape":"mcp","endpoint":"desk-model:fixture","research":{"capabilities":["answer"],"model":"shared-model"}},"digest":"sha256:` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `"},` +
	`{"profile":{"id":"current-pages","class":"generated","source":"web","shape":"http","endpoint":null,"endpointScope":["https://docs.example/guides"],"research":{"capabilities":["page"]}},"digest":"sha256:` + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" + `"},` +
	`{"profile":{"id":"old-pages","class":"generated","source":"web","shape":"http","endpoint":null,"endpointScope":["https://a.example/api/v4/projects/group%2Fproject"],"research":{"capabilities":["page"]}},"digest":"sha256:` + "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" + `","state":"set-aside","reason":"` + setAsideReason + `"}]`

const (
	setAsideAnswerDigest  = "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	setAsideCurrentDigest = "sha256:" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	setAsideOldDigest     = "sha256:" + "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// runnerSetAsideMessage is Runner v0.23.0's refusal of a binding to the
// set-aside profile id with reason, as its profile_set_aside.go words it.
func runnerSetAsideMessage(id, reason string) string {
	return fmt.Sprintf("The input profile %s was set aside when Runner opened its store, and is never bound: %s. Its id stays taken. Register the profile anew under another id and review a new binding.", id, reason)
}

// setAsideStandIn is a stand-in Runner v0.23.0. It lists setAsideListing and
// answers a binding with refusal, when one is set, or with a research plan
// for the source "research" requesting answer and page, as a Runner that
// resolved the plan would.
type setAsideStandIn struct {
	refusal atomic.Pointer[string]
	plans   atomic.Int32
}

func (r *setAsideStandIn) serve(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch req.Method + " " + req.URL.Path {
	case "GET /v1/input-profiles":
		w.Write([]byte(setAsideListing))
	case "POST /v1/inputs/next", "POST /v1/jobs":
		if refusal := r.refusal.Load(); refusal != nil {
			w.WriteHeader(422)
			w.Write([]byte(*refusal))
			return
		}
		r.plans.Add(1)
		w.Write([]byte(`{"next":{"name":"research","kind":"research","profile":"answer-model","arguments":{"question":"Assess the record","constraints":{},"model":"shared-model","evidence":[],"fields":[],"capabilities":["answer","page"]}}}`))
	default:
		w.WriteHeader(404)
		w.Write([]byte(`{"error":{"code":"not_found","message":"Not found.","retryable":false}}`))
	}
}

// refuse sets the stand-in to refuse every binding with code and message.
func (r *setAsideStandIn) refuse(t *testing.T, code, message string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"error": map[string]any{"code": code, "message": message, "retryable": false}})
	if err != nil {
		t.Fatal(err)
	}
	refusal := string(body)
	r.refusal.Store(&refusal)
}

func startSetAsideStandIn(t *testing.T) (*setAsideStandIn, *jobsCompanion) {
	t.Helper()
	standIn := &setAsideStandIn{}
	server := httptest.NewServer(http.HandlerFunc(standIn.serve))
	t.Cleanup(server.Close)
	return standIn, &jobsCompanion{url: server.URL, token: "test-private", done: make(chan struct{}), stop: make(chan struct{})}
}

// portableInputPinningPage is a research input whose source "research" binds
// its answer to the installed model profile and its page to page.
func portableInputPinningPage(t *testing.T, page, digest string) json.RawMessage {
	t.Helper()
	input, err := json.Marshal(map[string]any{"source": map[string]any{"mapping": map[string]any{"version": 3, "sources": []any{map[string]any{
		"name": "research", "kind": "research", "profile": "answer-model", "profileDigest": setAsideAnswerDigest, "capabilities": []string{"answer", "page"},
		"webProfiles": map[string]any{"page": map[string]string{"profile": page, "profileDigest": digest}},
	}}}, "case": map[string]any{}, "sources": map[string]any{}}})
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func TestRunnerSetAsideProfileIsNeverACandidate(t *testing.T) {
	listing, err := readInputProfileListing(json.RawMessage(setAsideListing))
	if err != nil || len(listing) != 3 {
		t.Fatal("listing unread", err)
	}
	for _, digest := range []string{setAsideOldDigest, setAsideCurrentDigest, ""} {
		entry, err := listing.installedProfile("old-pages", digestIs(digest))
		var aside setAsideProfileError
		if entry != nil || !errors.As(err, &aside) || aside != (setAsideProfileError{ID: "old-pages", Reason: setAsideReason}) {
			t.Fatalf("a pin naming the set-aside profile with digest %q resolved to %v, %v", digest, entry, err)
		}
	}
	if entry, err := listing.installedProfile("current-pages", digestIs(setAsideCurrentDigest)); err != nil || entry == nil || entry.Digest != setAsideCurrentDigest {
		t.Fatal("the installed profile is not a candidate", entry, err)
	}
	if entry, err := listing.installedProfile("current-pages", digestIs(setAsideOldDigest)); err != nil || entry != nil {
		t.Fatal("an installed profile resolved by another digest", entry, err)
	}
	// The message names the profile once, labels what remains of Runner's
	// reason as Runner's, without its code, and words the next step for a
	// job only where the binding is a job's.
	shown := "The input profile old-pages was set aside by Runner and cannot be bound. Runner's reason: an endpointScope entry is not an endpoint prefix in the constrained page form, nor the wildcard https://*."
	for _, tc := range []struct {
		refusal setAsideProfileError
		want    string
	}{
		{setAsideProfileError{ID: "old-pages", Reason: setAsideReason}, shown + " Choose another profile for this source."},
		{setAsideProfileError{ID: "old-pages", Reason: setAsideReason, Job: true}, shown + " Choose another profile for this source, or upgrade the job to a release that does not name it."},
		// A reason that names another profile keeps its words.
		{setAsideProfileError{ID: "old-pages", Reason: "the input profile other: refused (input-malformed)"}, "The input profile old-pages was set aside by Runner and cannot be bound. Runner's reason: the input profile other: refused. Choose another profile for this source."},
		{setAsideProfileError{ID: "old-pages"}, "The input profile old-pages was set aside by Runner and cannot be bound. Choose another profile for this source."},
		{setAsideProfileError{Job: true}, "An input profile this binding names was set aside by Runner and cannot be bound. Choose another profile for this source, or upgrade the job to a release that does not name it."},
	} {
		if got := tc.refusal.Error(); got != tc.want {
			t.Errorf("the refusal reads\n%s\nnot\n%s", got, tc.want)
		}
	}
}

// Installed means the state member is absent: an entry whose state is empty
// or null is no candidate either, and a pin naming it is refused.
func TestRunnerListingEntryWithAnyStateMemberIsNeverACandidate(t *testing.T) {
	for _, state := range []string{`""`, `null`, `"retired"`} {
		raw := `[{"profile":{"id":"pages"},"digest":"sha256:d","state":` + state + `,"reason":"refused"}]`
		listing, err := readInputProfileListing(json.RawMessage(raw))
		if err != nil || len(listing) != 1 || listing[0].installed() {
			t.Fatalf("state %s: listed as %+v, %v", state, listing, err)
		}
		entry, err := listing.installedProfile("pages", digestIs("sha256:d"))
		var aside setAsideProfileError
		if entry != nil || !errors.As(err, &aside) || aside.ID != "pages" {
			t.Fatalf("state %s: a pin naming it resolved to %v, %v", state, entry, err)
		}
	}
	listing, err := readInputProfileListing(json.RawMessage(`[{"profile":{"id":"pages"},"digest":"sha256:d"}]`))
	if entry, e := listing.installedProfile("pages", digestIs("sha256:d")); err != nil || e != nil || entry == nil {
		t.Fatal("an entry with no state member is not installed", entry, e, err)
	}
}

// The listing reaches the page as Runner serves it, the set-aside entry with
// its state and reason. Runner's refusal of a binding to a set-aside profile
// reaches it as Desk's message, with the profile and Runner's listed reason;
// any other refusal passes unchanged.
func TestJobsProxyServesSetAsideProfilesAndMapsTheirRefusal(t *testing.T) {
	standIn, companion := startSetAsideStandIn(t)
	s := &Server{jobs: companion}
	call := func(method, tail, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/operations/"+tail, strings.NewReader(body))
		w := httptest.NewRecorder()
		s.proxyJobs(w, r, tail, "")
		return w
	}
	if w := call("GET", "input-profiles", ""); w.Code != 200 || w.Body.String() != setAsideListing {
		t.Fatalf("the listing reached the page as %d %s", w.Code, w.Body)
	}
	desk := setAsideProfileError{ID: "old-pages", Reason: setAsideReason, Job: true}.Error()
	standIn.refuse(t, codeProfileSetAside, runnerSetAsideMessage("old-pages", setAsideReason))
	for _, tail := range []string{"inputs/next", "jobs"} {
		w := call("POST", tail, `{}`)
		var answer struct {
			Error map[string]any `json:"error"`
		}
		if w.Code != 422 || json.Unmarshal(w.Body.Bytes(), &answer) != nil {
			t.Fatal(tail, w.Code, w.Body)
		}
		want := map[string]any{"code": codeProfileSetAside, "message": desk, "retryable": false, "profile": "old-pages", "reason": setAsideReason}
		if len(answer.Error) != len(want) {
			t.Fatalf("%s: the refusal is %s", tail, w.Body)
		}
		for key, value := range want {
			if answer.Error[key] != value {
				t.Fatalf("%s: the refusal is %s", tail, w.Body)
			}
		}
		if strings.Contains(w.Body.String(), "Its id stays taken") || strings.Contains(w.Body.String(), "Register the profile anew") {
			t.Fatalf("%s: Runner's text reached the page: %s", tail, w.Body)
		}
	}
	// A refusal in another wording names no profile and gives no reason:
	// nothing of Runner's text is echoed.
	standIn.refuse(t, codeProfileSetAside, "A binding names old-pages <b>unbound</b>.")
	w := call("POST", "inputs/next", `{}`)
	if w.Code != 422 || w.Body.String() != `{"error":{"code":"input_profile_set_aside","message":"An input profile this binding names was set aside by Runner and cannot be bound. Choose another profile for this source, or upgrade the job to a release that does not name it.","retryable":false}}` {
		t.Fatalf("a refusal in another wording reached the page as %d %s", w.Code, w.Body)
	}
	// A trigger's caller gets Runner's answer unchanged.
	standIn.refuse(t, codeProfileSetAside, runnerSetAsideMessage("old-pages", setAsideReason))
	trigger := httptest.NewRecorder()
	s.proxyJobs(trigger, httptest.NewRequest("POST", "/", strings.NewReader(`{}`)), "jobs", "fixture-trigger-token")
	if !strings.Contains(trigger.Body.String(), "Its id stays taken") {
		t.Fatal("a trigger's answer was rewritten:", trigger.Body)
	}
	for _, other := range []string{"input_profile_conflict", "invalid_request"} {
		standIn.refuse(t, other, runnerSetAsideMessage("old-pages", setAsideReason))
		if w := call("POST", "inputs/next", `{}`); w.Code != 422 || !strings.Contains(w.Body.String(), "Its id stays taken") || !strings.Contains(w.Body.String(), `"code":"`+other+`"`) {
			t.Fatalf("another refusal (%s) was rewritten: %s", other, w.Body)
		}
	}
}

// The review resolves each requested capability's pin against the installed
// profiles only: a pin that names the set-aside profile is refused by its name
// with Runner's reason, before any other check, whatever digest it names; and
// so is a frozen review at its confirmation. Runner's own refusal of the
// binding reaches the review as Desk's message.
func TestPortableResearchReviewNeverOffersASetAsideProfile(t *testing.T) {
	s, ts := startDesk(t, Config{LocalAccess: true, ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken})
	s.inputResearchCancel()
	<-s.inputResearchDone
	t.Cleanup(func() { ts.Close(); s.Close() })
	registry := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Portable", "https://model.example.invalid/v1")}})
	c := registry.Connections[0]
	if code, raw := aiTestRequest(t, ts, "PUT", "/api/assistant/key", map[string]string{"key": "set-aside-fixture-key"}, map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision}); code != 200 {
		t.Fatalf("key %d %s", code, raw)
	}
	standIn, companion := startSetAsideStandIn(t)
	s.jobs = companion
	t.Cleanup(func() { companion.closed = true })
	choice := backgroundModelChoice{Connection: c.ID, Revision: c.Revision, Model: "shared-model"}
	limits := inputResearchLimits{Records: 1, ModelCalls: 3, Pages: 1}
	want := setAsideProfileError{ID: "old-pages", Reason: setAsideReason}
	for _, digest := range []string{setAsideOldDigest, setAsideCurrentDigest} {
		input := portableInputPinningPage(t, "old-pages", digest)
		_, err := s.reviewPortableResearch(t.Context(), input, choice, limits, "", "fixture-scope")
		var aside setAsideProfileError
		if !errors.As(err, &aside) || aside != want || err.Error() != want.Error() {
			t.Fatalf("a review pinning the set-aside profile (digest %s) ended with %v, not refused by its name", digest, err)
		}
		// The confirmation checks the frozen review again: the same refusal.
		frozen := inputResearchRun{Portable: &portableResearchJournal{Frozen: portableResearchFrozen{Input: input, Choice: choice, Limits: limits}}}
		if err := s.validatePortableResearchAdmission(t.Context(), frozen); err == nil || err.Error() != want.Error() {
			t.Fatalf("a frozen pin naming the set-aside profile is reported as %v", err)
		}
	}
	// The installed profile is a candidate: its review goes on past the pins,
	// to the checks this Desk without a Gateway cannot pass.
	if _, err := s.reviewPortableResearch(t.Context(), portableInputPinningPage(t, "current-pages", setAsideCurrentDigest), choice, limits, "", "fixture-scope"); err == nil || errors.As(err, new(setAsideProfileError)) {
		t.Fatal("the installed page profile was refused as set aside:", err)
	}
	// Runner v0.23.0 refuses the binding itself, before any plan: Desk's
	// message, naming the profile and the reason Runner lists it with.
	standIn.refuse(t, codeProfileSetAside, runnerSetAsideMessage("old-pages", setAsideReason))
	plans := standIn.plans.Load()
	_, err := s.reviewPortableResearch(t.Context(), portableInputPinningPage(t, "old-pages", setAsideOldDigest), choice, limits, "", "fixture-scope")
	var refused *inputRunnerError
	if !errors.As(err, &refused) || refused.Status != 422 || refused.Code != codeProfileSetAside || refused.Message != want.Error() || standIn.plans.Load() != plans {
		t.Fatalf("Runner's refusal reached the review as %v", err)
	}
}

// Against the real Runner: a store an earlier Runner (v0.21.0) wrote, holding
// an exact profile and a scoped page profile whose entry RFC 0018 round 16
// refuses, opens with the scoped one set aside. Desk's listing serves it with
// its state and reason, Desk never resolves a pin to it, and Runner's refusal
// of a binding to it reaches the page and Desk as Desk's message with the
// reason Runner lists. The earlier Runner's row is written with Python's
// standard sqlite3 module, the one tool Desk's tests have for Runner's store.
func TestRunnerInputProfilesSetAsideByTheRealRunner(t *testing.T) {
	bin, runtime := os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bin == "" || runtime == "" {
		t.Skip("requires isolated companion binaries")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("requires python3 to write the store an earlier Runner wrote")
	}
	config := t.TempDir()
	os.Chmod(config, 0700)
	cfg := Config{RunnerBin: bin, JpackBin: runtime, ProjectDir: t.TempDir(), DeskConfigDir: config, Token: testToken}
	key := "58" + strings.Repeat("66", 31)
	exact := `{"id":"exact","publicKey":"` + key + `","class":"record","source":"records","authority":"test","shape":"mcp","adapter":{"name":"mcp","version":"1","digest":"sha256:` + strings.Repeat("a", 64) + `"},"endpoint":null,"tools":["lookup"]}`
	// As v0.21.0's encode wrote the scoped profile: InputProfile's members
	// in its order, the entry an escaped slash as a GitLab project path has.
	scoped := `{"id":"pages","publicKey":"` + key + `","class":"generated","source":"web","authority":"test","shape":"http","adapter":{"name":"adapter-web","version":"1","digest":"sha256:` + strings.Repeat("b", 64) + `"},"endpoint":null,"endpointScope":["https://a.example/api/v4/projects/group%2Fproject"],"research":{"capabilities":["page"]}}`

	s, ts := startDesk(t, cfg)
	if _, err := s.inputRunner(t.Context(), "input-profiles", json.RawMessage(exact), ""); err != nil {
		ts.Close()
		s.Close()
		t.Fatal("the exact profile was not registered:", err)
	}
	store := filepath.Join(s.jobs.dir, "runs.sqlite")
	ts.Close()
	s.Close()
	seed := `import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
key = "registered-input-profiles-v1"
(row,) = db.execute("SELECT value FROM metadata WHERE key=?", (key,)).fetchone()
assert row.startswith("[") and row.endswith("]"), row
db.execute("UPDATE metadata SET value=? WHERE key=?", (row[:-1] + "," + sys.argv[2] + "]", key))
db.commit()
db.close()
`
	if out, err := exec.Command(python, "-I", "-c", seed, store, scoped).CombinedOutput(); err != nil {
		t.Fatalf("the earlier Runner's row was not written: %v %s", err, out)
	}

	s, ts = startDesk(t, cfg)
	defer ts.Close()
	defer s.Close()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/operations/"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := call("GET", "input-profiles", "")
	var entries []map[string]json.RawMessage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &entries) != nil || len(entries) < 2 {
		t.Fatal("the listing is", w.Code, w.Body)
	}
	for _, entry := range entries[:len(entries)-1] {
		if entry["state"] != nil || entry["reason"] != nil {
			t.Fatal("an installed profile is listed with a state:", w.Body)
		}
	}
	last := entries[len(entries)-1]
	var reason, digest string
	if string(last["state"]) != `"set-aside"` || json.Unmarshal(last["reason"], &reason) != nil || reason == "" || json.Unmarshal(last["digest"], &digest) != nil || string(last["profile"]) != scoped {
		t.Fatal("the scoped profile is not listed as set aside:", w.Body)
	}
	if !strings.Contains(w.Body.String(), `"id":"exact"`) {
		t.Fatal("the exact profile is not listed:", w.Body)
	}
	listing, err := readInputProfileListing(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	want := setAsideProfileError{ID: "pages", Reason: reason, Job: true}
	if entry, err := listing.installedProfile("pages", digestIs(digest)); entry != nil || err == nil || err.Error() != (setAsideProfileError{ID: "pages", Reason: reason}).Error() {
		t.Fatal("a pin naming the set-aside profile resolved to", entry, err)
	}
	// A binding to it: Runner refuses it, and the page and Desk are told so
	// in Desk's words, with the reason Runner lists.
	binding := `{"source":{"mapping":{"version":2,"case":{"facts":[],"evidence":[]},"sources":[{"name":"pages","kind":"operation","profile":"pages","profileDigest":"` + digest + `","maxAge":300,"arguments":{"tool":"lookup","arguments":{}},"read":{"copy":{"facts":[],"evidence":[]}}}]},"case":{}}}`
	w = call("POST", "inputs/next", binding)
	var answer struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Profile string `json:"profile"`
			Reason  string `json:"reason"`
		} `json:"error"`
	}
	if w.Code != 422 || json.Unmarshal(w.Body.Bytes(), &answer) != nil || answer.Error.Code != codeProfileSetAside || answer.Error.Message != want.Error() || answer.Error.Profile != "pages" || answer.Error.Reason != reason || strings.Contains(w.Body.String(), "Its id stays taken") {
		t.Fatal("the binding's refusal reached the page as", w.Code, w.Body)
	}
	_, err = s.inputRunner(t.Context(), "inputs/next", json.RawMessage(binding), "")
	var refused *inputRunnerError
	if !errors.As(err, &refused) || refused.Code != codeProfileSetAside || refused.Message != (setAsideProfileError{ID: "pages", Reason: reason}).Error() {
		t.Fatal("the binding's refusal reached Desk as", err)
	}
	// The real Runner's reason names the profile and ends with its code. The
	// message names the profile once, labels the reason as Runner's, and
	// shows no bare code; the next step is a job's only on the Jobs routes.
	code := regexp.MustCompile(`\([a-z][a-z0-9_-]*\)$`).FindString(reason)
	if !strings.HasPrefix(reason, "the input profile pages: ") || code == "" {
		t.Fatal("Runner's reason is no longer the form this test reads:", reason)
	}
	for _, shown := range []string{answer.Error.Message, refused.Message} {
		if n := len(regexp.MustCompile(`\bpages\b`).FindAllString(shown, -1)); n != 1 || strings.Contains(shown, code) || !strings.Contains(shown, " Runner's reason: an endpointScope entry ") {
			t.Fatalf("the message names the profile %d times or shows Runner's code: %s", n, shown)
		}
	}
	if !strings.HasSuffix(answer.Error.Message, "or upgrade the job to a release that does not name it.") || strings.Contains(refused.Message, "upgrade the job") {
		t.Fatal("the next step is worded for the wrong surface:", answer.Error.Message, refused.Message)
	}
}
