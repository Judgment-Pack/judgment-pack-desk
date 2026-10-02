package desk

// Review and lock (ADR-0009, section 2). A stand-in runtime, by absolute
// path, answers `packs verify` and `packs lock` from files each test prepares,
// so these run where no runtime is installed. The last test drives the real
// runtime and skips without one.

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const reviewPack = `{"specVersion":"0.2.0-draft","id":"https://example.com/judgment-packs/minimal-literal","version":"0.1.0","title":"Minimal literal decision","decision":{"intent":"Provide the smallest focused valid pack.","question":"Does the literal rule select accept?"},"outcomes":[{"id":"accept","label":"Accept"},{"id":"reject","label":"Reject"}],"rules":[{"id":"literal-rule","description":"A true literal condition.","when":{"op":"literal","value":true},"outcome":"accept","onUnknown":"ignore"}]}` + "\n"

// reviewRuntime is a stand-in runtime for the review step, and the files
// that steer it.
type reviewRig struct {
	bin, calls string
	// verify is what `packs verify` prints; it exits 1 when it holds
	// "invalid".
	verify string
	// lock is what `packs lock` copies to jpack.lock.json. It is not created
	// until a test prepares it; until then `packs lock` locks a new desk.
	lock string
	// before is a shell fragment `packs lock` runs first.
	before string
}

// newReviewRig writes the stand-in. Every run appends its arguments and the
// JPACK_CONFIG it was given to calls. It uses shell builtins only.
func newReviewRig(t *testing.T, before string) *reviewRig {
	t.Helper()
	dir := t.TempDir()
	rig := &reviewRig{bin: filepath.Join(dir, "jpack"), verify: filepath.Join(dir, "verify.json"), lock: filepath.Join(dir, "lock.json"), before: before}
	copyOut := func(from string) string {
		return "  while IFS= read -r line || [ -n \"$line\" ]; do printf '%s\\n' \"$line\"; done < '" + from + "'"
	}
	lock := "  if [ -e '" + rig.lock + "' ]; then\n" + before + "\n" + copyOut(rig.lock) + " > jpack.lock.json\n" +
		"  printf '%s\\n' '{\"outputVersion\":\"2\",\"command\":\"packs lock\",\"status\":\"valid\"}'\n  exit 0\n  fi\n" + lockingAs(wantGatedConfig)
	rig.calls = writeStandInRuntime(t, rig.bin, reading(allConfigVersions), lock)
	script, err := os.ReadFile(rig.bin)
	if err != nil {
		t.Fatal(err)
	}
	verify := "'packs verify')\n" + copyOut(rig.verify) + "\n  IFS= read -r code < '" + rig.verify + ".exit'\n  exit \"$code\"\n  ;;\n"
	script = bytes.Replace(script, []byte("'packs lock')\n"), []byte(verify+"'packs lock')\n"), 1)
	if err := os.WriteFile(rig.bin, script, 0o755); err != nil {
		t.Fatal(err)
	}
	return rig
}

// answers sets what the stand-in's `packs verify` prints.
func (rig *reviewRig) answers(t *testing.T, status string, findings ...map[string]string) {
	t.Helper()
	if findings == nil {
		findings = []map[string]string{}
	}
	data, _ := json.Marshal(map[string]any{"outputVersion": "2", "command": "packs verify", "status": status, "findings": findings})
	code := "1\n"
	if status == "valid" {
		code = "0\n"
	}
	if os.WriteFile(rig.verify, data, 0o600) != nil || os.WriteFile(rig.verify+".exit", []byte(code), 0o600) != nil {
		t.Fatal("could not prepare the stand-in's answer")
	}
}

// locks prepares the lock the stand-in's `packs lock` writes.
func (rig *reviewRig) locks(t *testing.T, data []byte) {
	t.Helper()
	if err := os.WriteFile(rig.lock, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// lockOf is the lock the runtime writes for a project's files as they are.
func lockOf(t *testing.T, project string, packs map[string]string) []byte {
	t.Helper()
	config, err := os.ReadFile(filepath.Join(project, "jpack.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]lockEntry{}
	for id, path := range packs {
		data, err := os.ReadFile(filepath.Join(project, path))
		if err != nil {
			t.Fatal(err)
		}
		entries[id] = lockEntry{Path: path, Digest: sha256Digest(data)}
	}
	lock := map[string]any{"lockVersion": "1", "config": map[string]string{"digest": sha256Digest(config)}}
	if len(entries) > 0 {
		lock["packs"] = entries
	}
	data, _ := json.MarshalIndent(lock, "", "  ")
	return append(data, '\n')
}

func writeProject(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const twoPacks = `{"configVersion":"5","requireReviewed":true,"packs":{"alpha":{"path":"packs/a.json"},"beta":{"path":"packs/b.json"}}}` + "\n"

// reviewProject is a startup desk over a project with two packs and a lock of
// them as they are, and the rig behind it.
func reviewProject(t *testing.T, before string) (*Server, *httptest.Server, *reviewRig, string) {
	t.Helper()
	rig := newReviewRig(t, before)
	project := t.TempDir()
	writeProject(t, project, map[string]string{"jpack.json": twoPacks, "packs/a.json": reviewPack, "packs/b.json": strings.Replace(reviewPack, "minimal-literal", "other", 1)})
	if err := os.WriteFile(filepath.Join(project, "jpack.lock.json"), lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}), 0o644); err != nil {
		t.Fatal(err)
	}
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: rig.bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	return s, ts, rig, project
}

// reviewCall is one request to the review routes, decorated as asked.
func reviewCall(t *testing.T, ts *httptest.Server, method, path, desk string, body any, decorate ...func(*http.Request)) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	r, err := http.NewRequest(method, ts.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if desk != "" {
		r.Header.Set("X-Jpack-Desk", desk)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, d := range decorate {
		d(r)
	}
	response, err := ts.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return response.StatusCode, data
}

func readReview(t *testing.T, ts *httptest.Server, desk string) reviewAnswer {
	t.Helper()
	status, data := reviewCall(t, ts, "GET", "/api/review", desk, nil, bearer)
	var answer reviewAnswer
	if status != 200 || json.Unmarshal(data, &answer) != nil {
		t.Fatalf("review: %d %s", status, data)
	}
	return answer
}

func confirm(t *testing.T, ts *httptest.Server, desk string, set *reviewSet) (int, []byte) {
	t.Helper()
	return reviewCall(t, ts, "POST", "/api/review/lock", desk, map[string]any{"set": set}, bearer)
}

func countCalls(t *testing.T, calls, prefix string) int {
	t.Helper()
	data, _ := os.ReadFile(calls)
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}

func copiesIn(t *testing.T, project string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(project, ".desk-private", "reviewed"))
	if os.IsNotExist(err) {
		return map[string]string{}
	}
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]string{}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(project, ".desk-private", "reviewed", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		kept[entry.Name()] = string(data)
	}
	return kept
}

// The review passes the runtime's findings through, says what a lock would
// cover, and shows an earlier copy only where Desk kept exactly the bytes the
// lock names. It locks nothing.
func TestTheReviewShowsTheRuntimesFindings(t *testing.T) {
	s, ts, rig, project := reviewProject(t, "")
	lockBefore := readFile(t, filepath.Join(project, "jpack.lock.json"))
	// A copy of alpha's locked bytes, and a copy under beta's locked digest
	// that holds other bytes.
	copies, err := s.reviewedCopies(true)
	if err != nil {
		t.Fatal(err)
	}
	alphaName, _ := copyName(sha256Digest([]byte(reviewPack)))
	if err := writePrivateData(copies, alphaName, []byte(reviewPack)); err != nil {
		t.Fatal(err)
	}
	betaName, _ := copyName(sha256Digest([]byte(strings.Replace(reviewPack, "minimal-literal", "other", 1))))
	if err := writePrivateData(copies, betaName, []byte("not what the lock names\n")); err != nil {
		t.Fatal(err)
	}
	copies.Close()
	writeProject(t, project, map[string]string{"packs/a.json": strings.Replace(reviewPack, "Minimal", "Edited", 1), "packs/b.json": strings.Replace(reviewPack, "Minimal", "Changed", 1)})
	rig.answers(t, "invalid",
		map[string]string{"name": "document-drift", "kind": "pack", "id": "alpha", "path": "packs/a.json", "detail": "The pack document's bytes differ from the reviewed set."},
		map[string]string{"name": "document-drift", "kind": "pack", "id": "beta", "path": "packs/b.json", "detail": "The pack document's bytes differ from the reviewed set."},
		map[string]string{"name": "lock-entry-missing", "kind": "pack", "id": "gamma", "path": "packs/c.json", "detail": "The configuration declares this pack and the reviewed set does not name it."},
		map[string]string{"name": "locked-but-undeclared", "kind": "pack", "id": "delta", "path": "packs/d.json", "detail": "The reviewed set names this pack and the configuration no longer declares it."},
	)
	answer := readReview(t, ts, "")
	if answer.Status != "invalid" || !answer.Locked || len(answer.Findings) != 4 {
		t.Fatalf("the review answered %+v", answer)
	}
	alpha, beta, gamma, delta := answer.Findings[0], answer.Findings[1], answer.Findings[2], answer.Findings[3]
	if alpha.Name != "document-drift" || alpha.ID != "alpha" || alpha.Path != "packs/a.json" || alpha.Detail != "The pack document's bytes differ from the reviewed set." {
		t.Errorf("the finding was not passed through: %+v", alpha)
	}
	if alpha.Earlier.State != "text" || alpha.Earlier.Text != reviewPack || alpha.Now.State != "text" || !strings.Contains(alpha.Now.Text, "Edited") {
		t.Errorf("alpha's comparison is %+v / %+v, want its locked copy beside the file", alpha.Earlier, alpha.Now)
	}
	if beta.Earlier.State != "no-copy" {
		t.Errorf("a copy whose bytes are not the locked ones was shown: %+v", beta.Earlier)
	}
	if gamma.Earlier.State != "unlocked" || delta.Now.State != "absent" {
		t.Errorf("new and removed packs were shown as %+v and %+v", gamma.Earlier, delta.Now)
	}
	if answer.Set == nil || answer.Set.Config != sha256Digest([]byte(twoPacks)) || len(answer.Set.Entries) != 2 ||
		answer.Set.Entries[0] != (reviewEntry{Kind: "pack", ID: "alpha", Path: "packs/a.json", Digest: sha256Digest([]byte(strings.Replace(reviewPack, "Minimal", "Edited", 1)))}) {
		t.Errorf("the set a lock would cover is %+v", answer.Set)
	}
	if got := readFile(t, rig.calls); got != "packs verify --config jpack.json --format json [JPACK_CONFIG=unset]\n" {
		t.Errorf("the review ran %q, want only packs verify", got)
	}
	if readFile(t, filepath.Join(project, "jpack.lock.json")) != lockBefore {
		t.Error("the review changed the lock")
	}
}

// **Nothing is locked without a confirmation**, and a confirmation has to
// come from this desk's page: the session, the Origin, the browser's own
// fetch metadata and the body are each checked before anything runs.
func TestNothingIsLockedWithoutAConfirmation(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "")
	rig.answers(t, "valid")
	lockBefore := readFile(t, filepath.Join(project, "jpack.lock.json"))
	set := readReview(t, ts, "").Set
	for _, tc := range []struct {
		name     string
		body     any
		want     int
		decorate []func(*http.Request)
	}{
		{"no session", map[string]any{"set": set}, 401, nil},
		{"another origin", map[string]any{"set": set}, 403, []func(*http.Request){bearer, func(r *http.Request) { r.Header.Set("Origin", "http://example.com") }}},
		{"cross-site", map[string]any{"set": set}, 403, []func(*http.Request){bearer, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }}},
		{"not JSON", map[string]any{"set": set}, 415, []func(*http.Request){bearer, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }}},
		{"no set", map[string]any{}, 400, []func(*http.Request){bearer}},
		{"another member", map[string]any{"set": set, "force": true}, 400, []func(*http.Request){bearer}},
	} {
		status, data := reviewCall(t, ts, "POST", "/api/review/lock", "", tc.body, tc.decorate...)
		if status != tc.want {
			t.Errorf("%s: %d %s, want %d", tc.name, status, data, tc.want)
		}
	}
	// The route is POST only: a GET reaches the page's static fallback.
	if status, _ := reviewCall(t, ts, "GET", "/api/review/lock", "", nil, bearer); status != http.StatusNotFound {
		t.Errorf("a GET of the lock route answered %d", status)
	}
	if n := countCalls(t, rig.calls, "packs lock"); n != 0 {
		t.Errorf("packs lock ran %d time(s) without a confirmation", n)
	}
	if readFile(t, filepath.Join(project, "jpack.lock.json")) != lockBefore || len(copiesIn(t, project)) != 0 {
		t.Error("something was written without a confirmation")
	}
}

// A confirmation of digests the files no longer have locks nothing and
// writes nothing.
func TestAStaleConfirmationLocksNothing(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "")
	rig.answers(t, "valid")
	lockBefore := readFile(t, filepath.Join(project, "jpack.lock.json"))
	set := readReview(t, ts, "").Set
	writeProject(t, project, map[string]string{"packs/a.json": strings.Replace(reviewPack, "Minimal", "Edited after the review", 1)})
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
	status, data := confirm(t, ts, "", set)
	if status != http.StatusConflict || !bytes.Contains(data, []byte(`"code":"stale"`)) || !bytes.Contains(data, []byte("nothing was locked")) {
		t.Errorf("a stale confirmation answered %d %s", status, data)
	}
	if n := countCalls(t, rig.calls, "packs lock"); n != 0 {
		t.Errorf("packs lock ran %d time(s) for a stale confirmation", n)
	}
	if readFile(t, filepath.Join(project, "jpack.lock.json")) != lockBefore || len(copiesIn(t, project)) != 0 {
		t.Error("a stale confirmation wrote something")
	}
}

// A confirmation of the set as it is locks it, and keeps a copy of each file
// it locked, named by its digest.
func TestAConfirmedSetIsLockedAndCopied(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "")
	writeProject(t, project, map[string]string{"packs/a.json": strings.Replace(reviewPack, "Minimal", "Edited", 1)})
	rig.answers(t, "invalid", map[string]string{"name": "document-drift", "kind": "pack", "id": "alpha", "path": "packs/a.json"})
	set := readReview(t, ts, "").Set
	want := lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"})
	rig.locks(t, want)
	status, data := confirm(t, ts, "", set)
	if status != 200 || !bytes.Contains(data, []byte(`"files":3`)) || !bytes.Contains(data, []byte(`"copies":"stored"`)) {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	if got := readFile(t, filepath.Join(project, "jpack.lock.json")); got != string(want) {
		t.Errorf("the lock is %s", got)
	}
	if got := readFile(t, rig.calls); !strings.Contains(got, "packs lock --config jpack.json --format json [JPACK_CONFIG=unset]") {
		t.Errorf("the runtime was run as %q", got)
	}
	copies := copiesIn(t, project)
	for _, file := range []string{"jpack.json", "packs/a.json", "packs/b.json"} {
		data := readFile(t, filepath.Join(project, filepath.FromSlash(file)))
		name, _ := copyName(sha256Digest([]byte(data)))
		if copies[name] != data {
			t.Errorf("no copy of %s's locked bytes under %s", file, name)
		}
	}
	if copies[".gitignore"] != "*\n" {
		t.Errorf("the copies folder does not ignore itself: %q", copies[".gitignore"])
	}
	info, err := os.Stat(filepath.Join(project, ".desk-private", "reviewed"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the copies folder is %v %v, want owner-only", info, err)
	}
}

// The copies are kept only in a real `.desk-private/reviewed`: a link there
// is not followed, and the lock still stands.
func TestReviewedCopiesAreNotKeptThroughALink(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "")
	if err := os.Symlink("packs", filepath.Join(project, ".desk-private")); err != nil {
		t.Skip(err)
	}
	rig.answers(t, "valid")
	set := readReview(t, ts, "").Set
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
	status, data := confirm(t, ts, "", set)
	if status != 200 || !bytes.Contains(data, []byte(`"copies":"not-stored"`)) {
		t.Errorf("the confirmation answered %d %s", status, data)
	}
	entries, _ := os.ReadDir(filepath.Join(project, "packs"))
	if len(entries) != 2 {
		t.Errorf("copies were written through the link: %v", entries)
	}
}

// **The bytes confirmed are the bytes locked.** A file that changes while
// the runtime locks puts the previous lock back, byte for byte, and nothing
// else is written; where there was no lock, none is left.
func TestAFileChangedDuringTheLockPutsThePreviousLockBack(t *testing.T) {
	for _, hadLock := range []bool{true, false} {
		_, ts, rig, project := reviewProject(t, "  printf 'changed during the lock\\n' >> packs/a.json")
		if !hadLock {
			os.Remove(filepath.Join(project, "jpack.lock.json"))
		}
		rig.answers(t, "valid")
		set := readReview(t, ts, "").Set
		lockBefore, _ := os.ReadFile(filepath.Join(project, "jpack.lock.json"))
		// What the runtime would lock after the change.
		changed := filepath.Join(t.TempDir(), "project")
		writeProject(t, changed, map[string]string{"jpack.json": twoPacks, "packs/a.json": reviewPack + "changed during the lock\n", "packs/b.json": readFile(t, filepath.Join(project, "packs", "b.json"))})
		rig.locks(t, lockOf(t, changed, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
		status, data := confirm(t, ts, "", set)
		if status != http.StatusConflict || !bytes.Contains(data, []byte("previous lock was put back")) {
			t.Errorf("had lock %v: the confirmation answered %d %s", hadLock, status, data)
		}
		after, err := os.ReadFile(filepath.Join(project, "jpack.lock.json"))
		if hadLock && (err != nil || !bytes.Equal(after, lockBefore)) {
			t.Errorf("the previous lock was not put back: %s", after)
		}
		if !hadLock && !os.IsNotExist(err) {
			t.Errorf("a lock was left where there was none: %s %v", after, err)
		}
		if len(copiesIn(t, project)) != 0 {
			t.Error("copies were written for a lock that was put back")
		}
	}
}

// A lock the runtime refuses puts the previous one back, and the answer says
// what the runtime said.
func TestARefusedLockPutsThePreviousLockBack(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "  printf 'half written' > jpack.lock.json\n  printf '%s\\n' '{\"command\":\"packs lock\",\"status\":\"error\",\"diagnostics\":[{\"code\":\"JPS-X\",\"message\":\"The stand-in refuses.\"}]}'\n  exit 4")
	rig.answers(t, "valid")
	lockBefore := readFile(t, filepath.Join(project, "jpack.lock.json"))
	set := readReview(t, ts, "").Set
	rig.locks(t, []byte("{}\n"))
	status, data := confirm(t, ts, "", set)
	if status != http.StatusInternalServerError || !bytes.Contains(data, []byte("The stand-in refuses")) || !bytes.Contains(data, []byte("previous lock was put back")) {
		t.Errorf("a refused lock answered %d %s", status, data)
	}
	if readFile(t, filepath.Join(project, "jpack.lock.json")) != lockBefore {
		t.Error("the previous lock was not put back")
	}
}

// A named desk reviews its own project, and its commands run without
// JPACK_CONFIG. The startup desk reviews only where its runtime reads its own
// jpack.json.
func TestTheReviewOnTheStartupDeskAndANamedDesk(t *testing.T) {
	rig := newReviewRig(t, "")
	project := t.TempDir()
	writeProject(t, project, map[string]string{"jpack.json": twoPacks, "packs/a.json": reviewPack, "packs/b.json": reviewPack})
	elsewhere := filepath.Join(t.TempDir(), "jpack.json")
	t.Setenv("JPACK_CONFIG", elsewhere)
	_, ts, _ := gatesServer(t, rig.bin)
	// gatesServer's project is empty; this one is the startup desk's.
	s2, ts2 := startDesk(t, Config{ProjectDir: project, JpackBin: rig.bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { s2.Close(); ts2.Close() })
	if status, data := reviewCall(t, ts2, "GET", "/api/review", "", nil, bearer); status != http.StatusConflict || !bytes.Contains(data, []byte("JPACK_CONFIG names")) {
		t.Errorf("a startup desk whose runtime reads another configuration answered %d %s", status, data)
	}
	if status, _ := confirm(t, ts2, "", &reviewSet{Config: "sha256:00"}); status != http.StatusConflict {
		t.Errorf("a startup desk whose runtime reads another configuration locked: %d", status)
	}
	t.Setenv("JPACK_CONFIG", filepath.Join(project, "jpack.json"))
	rig.answers(t, "valid")
	if answer := readReview(t, ts2, ""); answer.Set == nil {
		t.Errorf("a startup desk whose JPACK_CONFIG names its own jpack.json was not reviewed: %+v", answer)
	}

	t.Setenv("JPACK_CONFIG", elsewhere)
	row := createGatedDesk(t, ts)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": reviewPack, "jpack.json": strings.Replace(wantGatedConfig, `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)})
	rig.answers(t, "invalid", map[string]string{"name": "config-drift", "path": "jpack.json"}, map[string]string{"name": "lock-entry-missing", "kind": "pack", "id": "alpha", "path": "packs/a.json"})
	answer := readReview(t, ts, row.ID)
	if len(answer.Findings) != 2 || answer.Set == nil || len(answer.Set.Entries) != 1 || answer.Findings[0].Now.Text != readFile(t, filepath.Join(row.Folder, "jpack.json")) {
		t.Fatalf("the named desk's review answered %+v", answer)
	}
	rig.locks(t, lockOf(t, row.Folder, map[string]string{"alpha": "packs/a.json"}))
	if status, data := confirm(t, ts, row.ID, answer.Set); status != 200 {
		t.Fatalf("the named desk's confirmation answered %d %s", status, data)
	}
	if len(copiesIn(t, row.Folder)) != 3 {
		t.Errorf("the named desk keeps %v", copiesIn(t, row.Folder))
	}
	for _, line := range strings.Split(strings.TrimSpace(readFile(t, rig.calls)), "\n") {
		if strings.HasPrefix(line, "packs verify") || strings.HasPrefix(line, "packs lock") {
			if !strings.HasSuffix(line, "[JPACK_CONFIG=unset]") {
				t.Errorf("a review command ran as %q", line)
			}
		}
	}
}

// The real runtime, end to end on a named desk: a pack added is reviewed and
// locked, an edit after that shows its diff against the copy Desk kept, and
// a confirmation made before the edit locks nothing.
func TestReviewAndLockWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	_, ts, _ := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	config := strings.Replace(gatedConfigFor(t, row.ConfigVersion), `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": reviewPack, "jpack.json": config})

	first := readReview(t, ts, row.ID)
	names := map[string]reviewFinding{}
	for _, finding := range first.Findings {
		names[finding.Name] = finding
	}
	if first.Status != "invalid" || names["config-drift"].Name == "" || names["lock-entry-missing"].ID != "alpha" || names["lock-entry-missing"].Earlier.State != "unlocked" {
		t.Fatalf("the first review answered %+v", first)
	}
	if status, data := confirm(t, ts, row.ID, first.Set); status != 200 {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	jpackIn(t, bin, row.Folder, "packs", "verify", "--config", "jpack.json", "--format", "json")
	if again := readReview(t, ts, row.ID); again.Status != "valid" || len(again.Findings) != 0 {
		t.Errorf("after the lock the review answered %+v", again)
	}

	edited := strings.Replace(reviewPack, "Minimal literal decision", "Edited decision", 1)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": edited})
	second := readReview(t, ts, row.ID)
	if len(second.Findings) != 1 || second.Findings[0].Name != "document-drift" || second.Findings[0].Earlier.Text != reviewPack || second.Findings[0].Now.Text != edited {
		t.Fatalf("the edit was reviewed as %+v", second)
	}
	lock := readFile(t, filepath.Join(row.Folder, "jpack.lock.json"))
	if status, _ := confirm(t, ts, row.ID, first.Set); status != http.StatusConflict || readFile(t, filepath.Join(row.Folder, "jpack.lock.json")) != lock {
		t.Errorf("a confirmation made before the edit answered %d, or changed the lock", status)
	}
	if status, data := confirm(t, ts, row.ID, second.Set); status != 200 {
		t.Fatalf("the second confirmation answered %d %s", status, data)
	}
	jpackIn(t, bin, row.Folder, "packs", "verify", "--config", "jpack.json", "--format", "json")
}
