package desk

// Review and lock (ADR-0009, section 2). A stand-in runtime, by absolute
// path, answers `packs verify` and `packs lock` from files each test prepares,
// so these run where no runtime is installed. The last test drives the real
// runtime and skips without one.

import (
	"bytes"
	"encoding/json"
	"fmt"
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

var otherPack = strings.Replace(reviewPack, "minimal-literal", "other", 1)

const reviewGraph = `{"graph":"stand-in"}` + "\n"

// reviewRig is a stand-in runtime for the review step, and the files that
// steer it.
type reviewRig struct {
	bin, calls string
	// verify is what `packs verify` prints; it exits 1 unless it holds
	// "valid".
	verify string
	// lock is what `packs lock` copies to jpack.lock.json. Until a test
	// prepares it, `packs lock` locks a new desk instead.
	lock string
}

// newReviewRig writes the stand-in. verifyFirst and lockFirst are shell
// fragments each command runs before answering. Every run appends its
// arguments and the JPACK_CONFIG it was given to calls. Builtins only.
func newReviewRig(t *testing.T, verifyFirst, lockFirst string) *reviewRig {
	t.Helper()
	dir := t.TempDir()
	rig := &reviewRig{bin: filepath.Join(dir, "jpack"), verify: filepath.Join(dir, "verify.json"), lock: filepath.Join(dir, "lock.json")}
	copyOut := func(from string) string {
		return "  while IFS= read -r line || [ -n \"$line\" ]; do printf '%s\\n' \"$line\"; done < '" + from + "'"
	}
	lock := "  if [ -e '" + rig.lock + "' ]; then\n" + lockFirst + "\n" + copyOut(rig.lock) + " > jpack.lock.json\n" +
		"  printf '%s\\n' '{\"outputVersion\":\"2\",\"command\":\"packs lock\",\"status\":\"valid\"}'\n  exit 0\n  fi\n" + lockingAs(wantGatedConfig)
	rig.calls = writeStandInRuntime(t, rig.bin, reading(allConfigVersions), lock)
	script, err := os.ReadFile(rig.bin)
	if err != nil {
		t.Fatal(err)
	}
	verify := "'packs verify')\n" + verifyFirst + "\n" + copyOut(rig.verify) + "\n  IFS= read -r code < '" + rig.verify + ".exit'\n  exit \"$code\"\n  ;;\n"
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
	answer := map[string]any{"outputVersion": "2", "command": "packs verify", "status": status, "findings": findings}
	if status == "error" {
		answer["diagnostics"] = []map[string]string{{"code": "JPS-LOCK-ABSENT", "message": "There is no reviewed-set lock at jpack.lock.json."}}
	}
	data, _ := json.Marshal(answer)
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
	return lockOfAll(t, project, packs, nil)
}

func lockOfAll(t *testing.T, project string, packs, graphs map[string]string) []byte {
	t.Helper()
	config, err := os.ReadFile(filepath.Join(project, "jpack.json"))
	if err != nil {
		t.Fatal(err)
	}
	lock := map[string]any{"lockVersion": "1", "config": map[string]string{"digest": sha256Digest(config)}}
	for member, declared := range map[string]map[string]string{"packs": packs, "graphs": graphs} {
		entries := map[string]lockEntry{}
		for id, path := range declared {
			data, err := os.ReadFile(filepath.Join(project, path))
			if err != nil {
				t.Fatal(err)
			}
			entries[id] = lockEntry{Path: path, Digest: sha256Digest(data)}
		}
		if len(entries) > 0 {
			lock[member] = entries
		}
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
func reviewProject(t *testing.T, verifyFirst, lockFirst string) (*Server, *httptest.Server, *reviewRig, string) {
	t.Helper()
	rig := newReviewRig(t, verifyFirst, lockFirst)
	project := t.TempDir()
	writeProject(t, project, map[string]string{"jpack.json": twoPacks, "packs/a.json": reviewPack, "packs/b.json": otherPack})
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

func confirm(t *testing.T, ts *httptest.Server, desk, token string) (int, []byte) {
	t.Helper()
	return reviewCall(t, ts, "POST", "/api/review/lock", desk, map[string]any{"token": token}, bearer)
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

// shown is the text a side of the review names, from its contents.
func shown(answer reviewAnswer, side reviewSide) string {
	if side.State != "text" {
		return ""
	}
	return answer.Contents[side.Digest]
}

// fileOf is the review's row for a path.
func fileOf(t *testing.T, answer reviewAnswer, path string) reviewFile {
	t.Helper()
	for _, file := range answer.Files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("the review shows no %s: %+v", path, answer.Files)
	return reviewFile{}
}

func keepCopy(t *testing.T, s *Server, data string) {
	t.Helper()
	copies, err := s.reviewedCopies(true)
	if err != nil {
		t.Fatal(err)
	}
	defer copies.Close()
	name, _ := copyName(sha256Digest([]byte(data)))
	if err := writePrivateData(copies, name, []byte(data)); err != nil {
		t.Fatal(err)
	}
}

// The review shows every file a lock would cover, and what only the lock
// names, beside the runtime's findings passed through; an earlier copy only
// where Desk kept exactly the bytes the lock names. It locks nothing.
func TestTheReviewShowsEveryFileAndTheRuntimesFindings(t *testing.T) {
	s, ts, rig, project := reviewProject(t, "", "")
	removed := `{"removed":true}` + "\n"
	writeProject(t, project, map[string]string{"packs/d.json": removed})
	lock := lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json", "delta": "packs/d.json"})
	writeProject(t, project, map[string]string{"jpack.lock.json": string(lock)})
	os.Remove(filepath.Join(project, "packs", "d.json"))
	keepCopy(t, s, reviewPack)
	keepCopy(t, s, removed)
	// A copy under beta's locked digest that holds other bytes.
	copies, _ := s.reviewedCopies(false)
	betaName, _ := copyName(sha256Digest([]byte(otherPack)))
	if err := writePrivateData(copies, betaName, []byte("not what the lock names\n")); err != nil {
		t.Fatal(err)
	}
	copies.Close()
	edited := strings.Replace(reviewPack, "Minimal", "Edited", 1)
	writeProject(t, project, map[string]string{"packs/a.json": edited, "packs/b.json": strings.Replace(otherPack, "Minimal", "Changed", 1)})
	findings := []map[string]string{
		{"name": "document-drift", "kind": "pack", "id": "alpha", "path": "packs/a.json", "detail": "The pack document's bytes differ from the reviewed set."},
		{"name": "locked-but-undeclared", "kind": "pack", "id": "delta", "path": "packs/d.json", "detail": "The reviewed set names this pack and the configuration no longer declares it."},
	}
	rig.answers(t, "invalid", findings...)
	lockBefore := readFile(t, filepath.Join(project, "jpack.lock.json"))

	answer := readReview(t, ts, "")
	if answer.Status != "invalid" || !answer.Locked || len(answer.Findings) != 2 || answer.Findings[0] != (reviewFinding{Name: "document-drift", Kind: "pack", ID: "alpha", Path: "packs/a.json", Detail: "The pack document's bytes differ from the reviewed set."}) {
		t.Fatalf("the findings were not passed through: %+v", answer.Findings)
	}
	if len(answer.Files) != 4 {
		t.Fatalf("the review shows %d files, want the configuration, two packs and the removed one: %+v", len(answer.Files), answer.Files)
	}
	config := fileOf(t, answer, "jpack.json")
	if config.Kind != "config" || config.Lock != "same" || shown(answer, config.Now) != twoPacks || config.Digest != sha256Digest([]byte(twoPacks)) {
		t.Errorf("the configuration is shown as %+v", config)
	}
	alpha := fileOf(t, answer, "packs/a.json")
	if alpha.Lock != "other" || alpha.Earlier.State != "text" || shown(answer, *alpha.Earlier) != reviewPack || shown(answer, alpha.Now) != edited || alpha.Digest != sha256Digest([]byte(edited)) {
		t.Errorf("alpha is shown as %+v", alpha)
	}
	if beta := fileOf(t, answer, "packs/b.json"); beta.Lock != "other" || beta.Earlier.State != "no-copy" || beta.Now.State != "text" {
		t.Errorf("a copy whose bytes are not the locked ones was shown: %+v", beta)
	}
	if delta := fileOf(t, answer, "packs/d.json"); delta.Lock != "removed" || delta.Now.State != "absent" || shown(answer, *delta.Earlier) != removed {
		t.Errorf("what only the lock names is shown as %+v", delta)
	}
	if len(answer.Token) != 64 || answer.Blocked != "" {
		t.Errorf("a review of files that can all be shown gave token %q, blocked %q", answer.Token, answer.Blocked)
	}
	if got := readFile(t, rig.calls); got != "packs verify --config jpack.json --format json [JPACK_CONFIG=unset]\n" {
		t.Errorf("the review ran %q, want only packs verify", got)
	}
	if readFile(t, filepath.Join(project, "jpack.lock.json")) != lockBefore {
		t.Error("the review changed the lock")
	}
}

// **One reading.** The reviewer's case: a file changed while the review ran
// was shown as it was and confirmed as it became. Now the contents shown and
// the digests confirmed are one reading, and the runtime verifies a private
// copy of it; a confirmation of a reading the files have left locks nothing.
func TestTheBytesShownAreTheBytesConfirmed(t *testing.T) {
	project := t.TempDir()
	seen := t.TempDir()
	changed := strings.Replace(reviewPack, "Minimal", "Changed while the review ran", 1)
	// Where the runtime ran, and what it read there, before the project's
	// file changes under the review.
	rig := newReviewRig(t, "  printf '%s\\n' \"$(pwd -P)\" > '"+filepath.Join(seen, "where")+"'\n"+
		"  while IFS= read -r line || [ -n \"$line\" ]; do printf '%s\\n' \"$line\"; done < packs/a.json > '"+filepath.Join(seen, "read")+"'\n"+
		"  printf '%s\\n' '"+strings.TrimSuffix(changed, "\n")+"' > '"+filepath.Join(project, "packs", "a.json")+"'", "")
	writeProject(t, project, map[string]string{"jpack.json": twoPacks, "packs/a.json": reviewPack, "packs/b.json": otherPack})
	writeProject(t, project, map[string]string{"jpack.lock.json": string(lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))})
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: rig.bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { s.Close(); ts.Close() })
	rig.answers(t, "valid")
	lockBefore := readFile(t, filepath.Join(project, "jpack.lock.json"))

	answer := readReview(t, ts, "")
	alpha := fileOf(t, answer, "packs/a.json")
	if shown(answer, alpha.Now) != reviewPack || alpha.Digest != sha256Digest([]byte(reviewPack)) {
		t.Errorf("the review showed %q under %s, want the one reading it took", shown(answer, alpha.Now), alpha.Digest)
	}
	if readFile(t, filepath.Join(project, "packs", "a.json")) != changed {
		t.Fatal("the stand-in did not change the file while the review ran")
	}
	// The runtime verified a private copy of the reading, not the project.
	if where := strings.TrimSpace(readFile(t, filepath.Join(seen, "where"))); where == "" || where == s.projectDir || readFile(t, filepath.Join(seen, "read")) != reviewPack {
		t.Errorf("the runtime verified %q, holding %q, not a copy of the reading", where, readFile(t, filepath.Join(seen, "read")))
	}
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
	status, data := confirm(t, ts, "", answer.Token)
	if status != http.StatusConflict || !bytes.Contains(data, []byte(`"code":"stale"`)) {
		t.Errorf("confirming a reading the files have left answered %d %s", status, data)
	}
	if countCalls(t, rig.calls, "packs lock") != 0 || readFile(t, filepath.Join(project, "jpack.lock.json")) != lockBefore || len(copiesIn(t, project)) != 0 {
		t.Error("a confirmation of what the files no longer hold wrote something")
	}
}

// **A first lock is a full review.** The reviewer's case: with no lock the
// runtime finds nothing, and the page showed no file but offered the lock.
// Now every file a lock would cover is shown, and a file that cannot be shown
// cannot be confirmed.
func TestTheFirstLockShowsEveryFile(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	os.Remove(filepath.Join(project, "jpack.lock.json"))
	rig.answers(t, "error")
	answer := readReview(t, ts, "")
	if answer.Locked || len(answer.Findings) != 0 || len(answer.Diagnostics) != 1 || len(answer.Files) != 3 || answer.Token == "" {
		t.Fatalf("the first review answered %+v", answer)
	}
	for path, text := range map[string]string{"jpack.json": twoPacks, "packs/a.json": reviewPack, "packs/b.json": otherPack} {
		if file := fileOf(t, answer, path); file.Lock != "none" || shown(answer, file.Now) != text {
			t.Errorf("%s is shown as %+v", path, file)
		}
	}
	// A file too large to show is a file the owner cannot confirm.
	writeProject(t, project, map[string]string{"packs/b.json": strings.Repeat(" ", reviewTextLimit+1)})
	large := readReview(t, ts, "")
	if large.Token != "" || !strings.Contains(large.Blocked, "packs/b.json is larger than") || len(large.Contents) != 0 {
		t.Errorf("a file too large to show was offered for confirmation: token %q, blocked %q", large.Token, large.Blocked)
	}
	if status, _ := confirm(t, ts, "", answer.Token); status != http.StatusConflict {
		t.Errorf("an earlier token confirmed a file too large to show: %d", status)
	}
	// And so is one that is not text.
	writeProject(t, project, map[string]string{"packs/b.json": "{\"bytes\":\"\xff\xfe\"}\n"})
	if binary := readReview(t, ts, ""); binary.Token != "" || !strings.Contains(binary.Blocked, "packs/b.json is not text") {
		t.Errorf("a file that is not text was offered for confirmation: token %q, blocked %q", binary.Token, binary.Blocked)
	}
	if _, err := os.Stat(filepath.Join(project, "jpack.lock.json")); !os.IsNotExist(err) || countCalls(t, rig.calls, "packs lock") != 0 {
		t.Error("something was locked")
	}
}

// retainedBy counts what each reading of the review holds, file by file,
// through the review's own hook.
func retainedBy(t *testing.T) (reads map[string]int, most *int) {
	t.Helper()
	reads, top := map[string]int{}, 0
	testHookReviewRetained = func(clean string, retained int) {
		reads[clean]++
		if retained > top {
			top = retained
		}
	}
	t.Cleanup(func() { testHookReviewRetained = nil })
	return reads, &top
}

// padded is a pack of about size bytes: valid JSON, padded with spaces.
func padded(size int) string {
	return strings.Replace(reviewPack, `"title":`, strings.Repeat(" ", size-len(reviewPack))+`"title":`, 1)
}

// **Each file is read once, however many ids name it.** The reviewer's case:
// 64 ids naming one padded pack of nearly 1 MiB were read, and kept, 64
// times before any budget was looked at.
func TestManyIdsNamingOneFileAreReadOnce(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	ids := map[string]map[string]string{}
	for i := range 64 {
		ids[fmt.Sprintf("p%02d", i)] = map[string]string{"path": "packs/big.json"}
	}
	config, _ := json.Marshal(map[string]any{"configVersion": "5", "packs": ids})
	writeProject(t, project, map[string]string{"jpack.json": string(config) + "\n", "packs/big.json": padded(reviewTextLimit - 1024)})
	rig.answers(t, "valid")
	reads, most := retainedBy(t)
	status, data := reviewCall(t, ts, "GET", "/api/review", "", nil, bearer)
	var answer reviewAnswer
	if status != 200 || json.Unmarshal(data, &answer) != nil {
		t.Fatalf("review: %d %.200s", status, data)
	}
	if reads["packs/big.json"] != 1 || *most > 2*reviewTextLimit {
		t.Errorf("one file named by 64 ids was read %d time(s), holding %d bytes at most", reads["packs/big.json"], *most)
	}
	// The configuration, 64 ids, and the two packs only the old lock names.
	if answer.Token == "" || answer.Blocked != "" || len(answer.Files) != 67 || len(answer.Contents) != 2 {
		t.Errorf("the review gave token %q, blocked %q, %d files and %d contents", answer.Token, answer.Blocked, len(answer.Files), len(answer.Contents))
	}
	if len(data) > 3*reviewTextLimit {
		t.Errorf("the answer is %d bytes: the file's text is not shown once", len(data))
	}
}

// **The budget is checked before each read.** Distinct files that add up to
// more than the reading budget stop the review at the first file that would
// pass it, with nothing read after, and nothing held past the budget.
func TestTheReadingBudgetStopsTheReviewBeforeItIsPassed(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	packs := map[string]map[string]string{}
	files := map[string]string{}
	for i := range 2 * reviewReadingLimit / reviewTextLimit {
		name := fmt.Sprintf("packs/p%02d.json", i)
		packs[fmt.Sprintf("p%02d", i)] = map[string]string{"path": name}
		files[name] = padded(reviewTextLimit - 1024)
	}
	config, _ := json.Marshal(map[string]any{"configVersion": "5", "packs": packs})
	files["jpack.json"] = string(config) + "\n"
	writeProject(t, project, files)
	rig.answers(t, "valid")
	reads, most := retainedBy(t)
	answer := readReview(t, ts, "")
	if answer.Token != "" || !strings.Contains(answer.Blocked, "add up to more than") || len(answer.Contents) != 0 {
		t.Errorf("a reading past the budget gave token %q, blocked %q", answer.Token, answer.Blocked)
	}
	if *most > reviewReadingLimit || len(reads) > reviewReadingLimit/(reviewTextLimit-1024)+1 {
		t.Errorf("the review held %d bytes, over %d files, before stopping", *most, len(reads))
	}
}

// **Too many entries stop the review before any document is read.**
func TestTooManyEntriesAreRefusedBeforeAnyRead(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	ids := map[string]map[string]string{}
	for i := range reviewEntryLimit + 1 {
		ids[fmt.Sprintf("p%04d", i)] = map[string]string{"path": "packs/a.json"}
	}
	config, _ := json.Marshal(map[string]any{"configVersion": "5", "packs": ids})
	writeProject(t, project, map[string]string{"jpack.json": string(config) + "\n"})
	rig.answers(t, "valid")
	reads, _ := retainedBy(t)
	answer := readReview(t, ts, "")
	if answer.Token != "" || !strings.Contains(answer.Blocked, "more than 1024 documents") {
		t.Errorf("too many entries gave token %q, blocked %q", answer.Token, answer.Blocked)
	}
	if len(reads) != 1 || reads["jpack.json"] != 1 {
		t.Errorf("documents were read before the entries were counted: %v", reads)
	}
}

// **The earlier copies shown are bounded too.** Copies that add up to more
// than their budget are shown until it, and then reported as not shown,
// without being read.
func TestTheEarlierCopiesShownAreBounded(t *testing.T) {
	s, ts, rig, project := reviewProject(t, "", "")
	packs := map[string]map[string]string{}
	earlier := map[string]string{}
	current := map[string]string{}
	for i := range 2 * reviewEarlierLimit / reviewTextLimit {
		name := fmt.Sprintf("packs/p%02d.json", i)
		packs[fmt.Sprintf("p%02d", i)] = map[string]string{"path": name}
		earlier[name] = strings.Replace(padded(reviewTextLimit-1024), "minimal-literal", fmt.Sprintf("p%02d", i), 1)
		current[name] = strings.Replace(reviewPack, "minimal-literal", fmt.Sprintf("p%02d", i), 1)
	}
	config, _ := json.Marshal(map[string]any{"configVersion": "5", "packs": packs})
	earlier["jpack.json"] = string(config) + "\n"
	writeProject(t, project, earlier)
	byPath := map[string]string{}
	for name := range packs {
		byPath[name] = packs[name]["path"]
		keepCopy(t, s, earlier[packs[name]["path"]])
	}
	writeProject(t, project, map[string]string{"jpack.lock.json": string(lockOf(t, project, byPath))})
	writeProject(t, project, current)
	rig.answers(t, "invalid")
	status, data := reviewCall(t, ts, "GET", "/api/review", "", nil, bearer)
	var answer reviewAnswer
	if status != 200 || json.Unmarshal(data, &answer) != nil {
		t.Fatalf("review: %d %.200s", status, data)
	}
	states := map[string]int{}
	for _, file := range answer.Files {
		if file.Earlier != nil {
			states[file.Earlier.State]++
		}
	}
	if states["text"] == 0 || states["not-shown"] == 0 || states["text"] > reviewEarlierLimit/(reviewTextLimit-1024) {
		t.Errorf("the earlier copies were shown as %v", states)
	}
	if answer.Token == "" || len(data) > reviewEarlierLimit+2*reviewTextLimit {
		t.Errorf("the answer is %d bytes, token %q", len(data), answer.Token)
	}
}

// **Nothing is locked without a confirmation**, and a confirmation has to
// come from this desk's page: the session, the Origin, the browser's own
// fetch metadata, the body and the token are each checked first.
func TestNothingIsLockedWithoutAConfirmation(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	rig.answers(t, "valid")
	lockBefore := readFile(t, filepath.Join(project, "jpack.lock.json"))
	token := readReview(t, ts, "").Token
	other := strings.Repeat("0", 64)
	for _, tc := range []struct {
		name     string
		body     any
		want     int
		decorate []func(*http.Request)
	}{
		{"no session", map[string]any{"token": token}, 401, nil},
		{"another origin", map[string]any{"token": token}, 403, []func(*http.Request){bearer, func(r *http.Request) { r.Header.Set("Origin", "http://example.com") }}},
		{"cross-site", map[string]any{"token": token}, 403, []func(*http.Request){bearer, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }}},
		{"not JSON", map[string]any{"token": token}, 415, []func(*http.Request){bearer, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }}},
		{"no token", map[string]any{}, 400, []func(*http.Request){bearer}},
		{"another member", map[string]any{"token": token, "force": true}, 400, []func(*http.Request){bearer}},
		{"a token this review did not give", map[string]any{"token": other}, 409, []func(*http.Request){bearer}},
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

// A confirmation of a reading the files have since left locks nothing and
// writes nothing.
func TestAStaleConfirmationLocksNothing(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	rig.answers(t, "valid")
	lockBefore := readFile(t, filepath.Join(project, "jpack.lock.json"))
	token := readReview(t, ts, "").Token
	writeProject(t, project, map[string]string{"packs/a.json": strings.Replace(reviewPack, "Minimal", "Edited after the review", 1)})
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
	status, data := confirm(t, ts, "", token)
	if status != http.StatusConflict || !bytes.Contains(data, []byte(`"code":"stale"`)) || !bytes.Contains(data, []byte("nothing was locked")) {
		t.Errorf("a stale confirmation answered %d %s", status, data)
	}
	if n := countCalls(t, rig.calls, "packs lock"); n != 0 {
		t.Errorf("packs lock ran %d time(s) for a stale confirmation", n)
	}
	if readFile(t, filepath.Join(project, "jpack.lock.json")) != lockBefore || len(copiesIn(t, project)) != 0 {
		t.Error("a stale confirmation wrote something")
	}
	// The lock is part of the reading too: one locked elsewhere after the
	// review makes the confirmation stale.
	writeProject(t, project, map[string]string{"packs/a.json": reviewPack})
	token = readReview(t, ts, "").Token
	relocked := lockOf(t, project, map[string]string{"alpha": "packs/a.json"})
	writeProject(t, project, map[string]string{"jpack.lock.json": string(relocked)})
	if status, data := confirm(t, ts, "", token); status != http.StatusConflict || countCalls(t, rig.calls, "packs lock") != 0 {
		t.Errorf("a confirmation of a lock since replaced answered %d %s", status, data)
	}
	if readFile(t, filepath.Join(project, "jpack.lock.json")) != string(relocked) {
		t.Error("a stale confirmation changed the lock")
	}
}

// **A confirmation is bound to its desk.** The reviewer's case: the startup
// desk's confirmation, sent with a named desk's header, locked the named
// desk where the bytes matched. Now it locks nothing.
func TestAConfirmationIsBoundToItsDesk(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	rig.answers(t, "valid")
	row := createGatedDesk(t, ts)
	for _, name := range []string{"jpack.json", "jpack.lock.json", "packs/a.json", "packs/b.json"} {
		writeProject(t, row.Folder, map[string]string{name: readFile(t, filepath.Join(project, filepath.FromSlash(name)))})
	}
	startup, named := readReview(t, ts, ""), readReview(t, ts, row.ID)
	for i := range startup.Files {
		if startup.Files[i].Digest != named.Files[i].Digest {
			t.Fatalf("the two desks hold different bytes: %+v %+v", startup.Files[i], named.Files[i])
		}
	}
	lockBefore := readFile(t, filepath.Join(row.Folder, "jpack.lock.json"))
	rig.locks(t, []byte(lockBefore))
	locks := countCalls(t, rig.calls, "packs lock")
	if status, data := confirm(t, ts, row.ID, startup.Token); status != http.StatusConflict {
		t.Errorf("another desk's confirmation answered %d %s", status, data)
	}
	if countCalls(t, rig.calls, "packs lock") != locks || readFile(t, filepath.Join(row.Folder, "jpack.lock.json")) != lockBefore || len(copiesIn(t, row.Folder)) != 0 {
		t.Error("another desk's confirmation locked or wrote something")
	}
}

// A confirmation of the reading as it is locks it, and keeps a copy of each
// file it locked, named by its digest.
func TestAConfirmedSetIsLockedAndCopied(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	writeProject(t, project, map[string]string{"packs/a.json": strings.Replace(reviewPack, "Minimal", "Edited", 1)})
	rig.answers(t, "invalid", map[string]string{"name": "document-drift", "kind": "pack", "id": "alpha", "path": "packs/a.json"})
	token := readReview(t, ts, "").Token
	want := lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"})
	rig.locks(t, want)
	status, data := confirm(t, ts, "", token)
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

// **The lock must pin exactly the reading.** A lock that pins another
// configuration, another graph, or an entry more is put back byte for byte,
// and nothing else is written.
func TestALockOfAnythingElseIsPutBack(t *testing.T) {
	const withGraph = `{"configVersion":"5","requireReviewed":true,"packs":{"alpha":{"path":"packs/a.json"}},"graphs":{"flow":{"path":"flow.json"}}}` + "\n"
	for _, tc := range []struct {
		name string
		lock func(t *testing.T, project string) []byte
	}{
		// The control: a lock of exactly the reading, graph included, stands.
		{"the reading itself", func(t *testing.T, project string) []byte {
			return lockOfAll(t, project, map[string]string{"alpha": "packs/a.json"}, map[string]string{"flow": "flow.json"})
		}},
		{"another configuration", func(t *testing.T, project string) []byte {
			lock := lockOfAll(t, project, map[string]string{"alpha": "packs/a.json"}, map[string]string{"flow": "flow.json"})
			return bytes.Replace(lock, []byte(sha256Digest([]byte(withGraph))), []byte(sha256Digest([]byte(twoPacks))), 1)
		}},
		{"another graph", func(t *testing.T, project string) []byte {
			lock := lockOfAll(t, project, map[string]string{"alpha": "packs/a.json"}, map[string]string{"flow": "flow.json"})
			return bytes.Replace(lock, []byte(sha256Digest([]byte(reviewGraph))), []byte(sha256Digest([]byte(reviewPack))), 1)
		}},
		{"an entry more", func(t *testing.T, project string) []byte {
			return lockOfAll(t, project, map[string]string{"alpha": "packs/a.json"}, map[string]string{"flow": "flow.json", "extra": "flow.json"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newReviewRig(t, "", "")
			project := t.TempDir()
			writeProject(t, project, map[string]string{"jpack.json": withGraph, "packs/a.json": reviewPack, "flow.json": reviewGraph})
			previous := lockOfAll(t, project, map[string]string{"alpha": "packs/a.json"}, map[string]string{"flow": "flow.json"})
			writeProject(t, project, map[string]string{"jpack.lock.json": string(previous), "flow.json": strings.Replace(reviewGraph, "stand-in", "edited", 1)})
			s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: rig.bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
			t.Cleanup(func() { s.Close(); ts.Close() })
			rig.answers(t, "invalid", map[string]string{"name": "document-drift", "kind": "graph", "id": "flow", "path": "flow.json"})
			review := readReview(t, ts, "")
			if flow := fileOf(t, review, "flow.json"); flow.Kind != "graph" || flow.Lock != "other" {
				t.Errorf("the changed graph is shown as %+v", flow)
			}
			writeProject(t, project, map[string]string{"flow.json": reviewGraph})
			again := readReview(t, ts, "")
			if review.Token == again.Token || fileOf(t, again, "flow.json").Lock != "same" {
				t.Fatal("a change to a graph did not change the review")
			}
			want := tc.lock(t, project)
			rig.locks(t, want)
			status, data := confirm(t, ts, "", again.Token)
			if tc.name == "the reading itself" {
				if status != 200 || readFile(t, filepath.Join(project, "jpack.lock.json")) != string(want) || len(copiesIn(t, project)) != 4 {
					t.Errorf("a lock of exactly the reading answered %d %s", status, data)
				}
				return
			}
			if status != http.StatusConflict || !bytes.Contains(data, []byte("previous lock was put back")) {
				t.Errorf("a lock of %s answered %d %s", tc.name, status, data)
			}
			if got := readFile(t, filepath.Join(project, "jpack.lock.json")); got != string(previous) {
				t.Errorf("the previous lock was not put back exactly: %s", got)
			}
			if len(copiesIn(t, project)) != 0 {
				t.Error("copies were written for a lock that was put back")
			}
		})
	}
}

// **The bytes confirmed are the bytes locked.** A file that changes while
// the runtime locks puts the previous lock back, byte for byte, and nothing
// else is written; where there was no lock, none is left.
func TestAFileChangedDuringTheLockPutsThePreviousLockBack(t *testing.T) {
	for _, hadLock := range []bool{true, false} {
		_, ts, rig, project := reviewProject(t, "", "  printf 'changed during the lock\\n' >> packs/a.json")
		if !hadLock {
			os.Remove(filepath.Join(project, "jpack.lock.json"))
		}
		rig.answers(t, "valid")
		token := readReview(t, ts, "").Token
		lockBefore, _ := os.ReadFile(filepath.Join(project, "jpack.lock.json"))
		changed := filepath.Join(t.TempDir(), "project")
		writeProject(t, changed, map[string]string{"jpack.json": twoPacks, "packs/a.json": reviewPack + "changed during the lock\n", "packs/b.json": otherPack})
		rig.locks(t, lockOf(t, changed, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
		status, data := confirm(t, ts, "", token)
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
	_, ts, rig, project := reviewProject(t, "", "  printf 'half written' > jpack.lock.json\n  printf '%s\\n' '{\"command\":\"packs lock\",\"status\":\"error\",\"diagnostics\":[{\"code\":\"JPS-X\",\"message\":\"The stand-in refuses.\"}]}'\n  exit 4")
	rig.answers(t, "valid")
	lockBefore := readFile(t, filepath.Join(project, "jpack.lock.json"))
	token := readReview(t, ts, "").Token
	rig.locks(t, []byte("{}\n"))
	status, data := confirm(t, ts, "", token)
	if status != http.StatusInternalServerError || !bytes.Contains(data, []byte("The stand-in refuses")) || !bytes.Contains(data, []byte("previous lock was put back")) {
		t.Errorf("a refused lock answered %d %s", status, data)
	}
	if readFile(t, filepath.Join(project, "jpack.lock.json")) != lockBefore {
		t.Error("the previous lock was not put back")
	}
}

// **The review reads by the file API's rules.** The reviewer's case: a
// declared pack that is a link into `.desk-private` returned private bytes
// through the review while the file route refused it. A link anywhere on the
// path of the configuration, a document or the lock is refused, and nothing
// of what it points at is shown.
func TestTheReviewReadsByTheFileAPIsRules(t *testing.T) {
	const secret = "a private record that the file API refuses to read"
	for _, link := range []string{"packs/a.json", "jpack.lock.json"} {
		t.Run(link, func(t *testing.T) {
			_, ts, rig, project := reviewProject(t, "", "")
			rig.answers(t, "valid")
			writeProject(t, project, map[string]string{".desk-private/secret.json": secret + "\n"})
			os.Remove(filepath.Join(project, filepath.FromSlash(link)))
			target, _ := filepath.Rel(filepath.Dir(filepath.Join(project, filepath.FromSlash(link))), filepath.Join(project, ".desk-private", "secret.json"))
			if err := os.Symlink(target, filepath.Join(project, filepath.FromSlash(link))); err != nil {
				t.Skip(err)
			}
			if status, _ := reviewCall(t, ts, "GET", "/api/file?path="+link, "", nil, bearer); status == 200 {
				t.Fatalf("the file route read %s", link)
			}
			status, data := reviewCall(t, ts, "GET", "/api/review", "", nil, bearer)
			var answer reviewAnswer
			if status != 200 || json.Unmarshal(data, &answer) != nil {
				t.Fatalf("review: %d %s", status, data)
			}
			if bytes.Contains(data, []byte(secret)) {
				t.Errorf("the review showed what %s links to", link)
			}
			if answer.Token != "" || !strings.Contains(answer.Blocked, "symbolic link") {
				t.Errorf("a reading through a link was offered for confirmation: token %q, blocked %q", answer.Token, answer.Blocked)
			}
		})
	}
}

// The copies are kept only in a real `.desk-private/reviewed`: a link there
// is not followed, and the lock still stands.
func TestReviewedCopiesAreNotKeptThroughALink(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	if err := os.Symlink("packs", filepath.Join(project, ".desk-private")); err != nil {
		t.Skip(err)
	}
	rig.answers(t, "valid")
	token := readReview(t, ts, "").Token
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
	status, data := confirm(t, ts, "", token)
	if status != 200 || !bytes.Contains(data, []byte(`"copies":"not-stored"`)) {
		t.Errorf("the confirmation answered %d %s", status, data)
	}
	entries, _ := os.ReadDir(filepath.Join(project, "packs"))
	if len(entries) != 2 {
		t.Errorf("copies were written through the link: %v", entries)
	}
}

// **A copies folder open to others is not trusted.** The reviewer's case: a
// 0777 `.desk-private/reviewed` stayed 0777 and was reported as holding the
// copies. Now nothing is written there, the answer says why, and a copy
// found there is not shown.
func TestAnUnsafeCopiesFolderIsNotTrusted(t *testing.T) {
	s, ts, rig, project := reviewProject(t, "", "")
	folder := filepath.Join(project, ".desk-private", "reviewed")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	keepCopy(t, s, reviewPack)
	if err := os.Chmod(folder, 0o777); err != nil {
		t.Fatal(err)
	}
	before := copiesIn(t, project)
	writeProject(t, project, map[string]string{"packs/a.json": strings.Replace(reviewPack, "Minimal", "Edited", 1)})
	rig.answers(t, "invalid", map[string]string{"name": "document-drift", "kind": "pack", "id": "alpha", "path": "packs/a.json"})
	answer := readReview(t, ts, "")
	if alpha := fileOf(t, answer, "packs/a.json"); alpha.Earlier.State != "no-copy" {
		t.Errorf("a copy from a folder open to others was shown: %+v", alpha.Earlier)
	}
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
	status, data := confirm(t, ts, "", answer.Token)
	if status != 200 || !bytes.Contains(data, []byte(`"copies":"not-stored"`)) || !bytes.Contains(data, []byte("open to other users")) {
		t.Errorf("the confirmation answered %d %s", status, data)
	}
	info, _ := os.Stat(folder)
	if after := copiesIn(t, project); len(after) != len(before) || info.Mode().Perm() != 0o777 {
		t.Errorf("Desk wrote into a folder open to others: %v -> %v, %v", before, after, info.Mode().Perm())
	}
}

// A named desk reviews its own project, and its commands run without
// JPACK_CONFIG. The startup desk reviews only where its runtime reads its own
// jpack.json, by path: the reviewer's case was a hard link to it from another
// directory, whose documents and lock are another project's.
func TestTheReviewOnTheStartupDeskAndANamedDesk(t *testing.T) {
	rig := newReviewRig(t, "", "")
	project := t.TempDir()
	writeProject(t, project, map[string]string{"jpack.json": twoPacks, "jpack.other.json": twoPacks, "packs/a.json": reviewPack, "packs/b.json": reviewPack})
	elsewhere := t.TempDir()
	writeProject(t, elsewhere, map[string]string{"jpack.json": twoPacks})
	linked := t.TempDir()
	if err := os.Link(filepath.Join(project, "jpack.json"), filepath.Join(linked, "jpack.json")); err != nil {
		t.Skip(err)
	}
	pointing := t.TempDir()
	if err := os.Symlink(filepath.Join(project, "jpack.json"), filepath.Join(pointing, "jpack.json")); err != nil {
		t.Skip(err)
	}
	_, ts, _ := gatesServer(t, rig.bin)
	s2, ts2 := startDesk(t, Config{ProjectDir: project, JpackBin: rig.bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { s2.Close(); ts2.Close() })
	rig.answers(t, "valid")
	for _, tc := range []struct {
		name, value string
		allowed     bool
	}{
		{"another project's", filepath.Join(elsewhere, "jpack.json"), false},
		{"a hard link from another directory", filepath.Join(linked, "jpack.json"), false},
		{"a link from another directory", filepath.Join(pointing, "jpack.json"), false},
		{"another file in this project", filepath.Join(project, "jpack.other.json"), false},
		{"this project's, by path", filepath.Join(project, "jpack.json"), true},
		{"this project's, relative", "jpack.json", true},
	} {
		t.Setenv("JPACK_CONFIG", tc.value)
		status, data := reviewCall(t, ts2, "GET", "/api/review", "", nil, bearer)
		if allowed := status == 200; allowed != tc.allowed {
			t.Errorf("JPACK_CONFIG naming %s: the review answered %d %s", tc.name, status, data)
		}
		if !tc.allowed {
			if status, data := confirm(t, ts2, "", strings.Repeat("0", 64)); status != http.StatusConflict || !bytes.Contains(data, []byte("JPACK_CONFIG names")) {
				t.Errorf("JPACK_CONFIG naming %s: the lock answered %d %s", tc.name, status, data)
			}
		}
	}

	// A jpack.json in this directory that is itself a link to another one.
	os.Rename(filepath.Join(project, "jpack.json"), filepath.Join(project, "kept.json"))
	if err := os.Symlink(filepath.Join(elsewhere, "jpack.json"), filepath.Join(project, "jpack.json")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JPACK_CONFIG", filepath.Join(project, "jpack.json"))
	if status, data := reviewCall(t, ts2, "GET", "/api/review", "", nil, bearer); status != http.StatusConflict {
		t.Errorf("JPACK_CONFIG naming a link in this directory: the review answered %d %s", status, data)
	}
	os.Remove(filepath.Join(project, "jpack.json"))
	os.Rename(filepath.Join(project, "kept.json"), filepath.Join(project, "jpack.json"))

	t.Setenv("JPACK_CONFIG", filepath.Join(elsewhere, "jpack.json"))
	row := createGatedDesk(t, ts)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": reviewPack, "jpack.json": strings.Replace(wantGatedConfig, `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)})
	rig.answers(t, "invalid", map[string]string{"name": "config-drift", "path": "jpack.json"}, map[string]string{"name": "lock-entry-missing", "kind": "pack", "id": "alpha", "path": "packs/a.json"})
	answer := readReview(t, ts, row.ID)
	if len(answer.Findings) != 2 || len(answer.Files) != 2 || answer.Token == "" || shown(answer, fileOf(t, answer, "jpack.json").Now) != readFile(t, filepath.Join(row.Folder, "jpack.json")) {
		t.Fatalf("the named desk's review answered %+v", answer)
	}
	rig.locks(t, lockOf(t, row.Folder, map[string]string{"alpha": "packs/a.json"}))
	if status, data := confirm(t, ts, row.ID, answer.Token); status != 200 {
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

// **The lock's own entry, for each id** (ADR-0009, question 4). Jobs compares
// the bytes a release is made from with what the lock pins for the release's
// decision id, so the review names that entry: the lock's digest, as the one
// reading read the lock, never the file's own, never another id's, and
// nothing where the lock pins nothing. On the startup desk and on a named desk,
// each from its own project.
func TestTheReviewNamesWhatTheLockPinsForEachId(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	rig := newReviewRig(t, "", "")
	s, ts, _ := gatesServer(t, rig.bin)
	startup := s.projectDir
	writeProject(t, startup, map[string]string{"jpack.json": twoPacks, "packs/a.json": reviewPack, "packs/b.json": otherPack})
	rig.answers(t, "error")
	for _, file := range readReview(t, ts, "").Files {
		if file.Locked != "" || file.Lock != "none" {
			t.Errorf("with no lock, %s is shown as locked: %+v", file.Path, file)
		}
	}

	removed := `{"removed":true}` + "\n"
	writeProject(t, startup, map[string]string{"packs/d.json": removed})
	writeProject(t, startup, map[string]string{"jpack.lock.json": string(lockOf(t, startup, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json", "delta": "packs/d.json"}))})
	os.Remove(filepath.Join(startup, "packs", "d.json"))
	edited := strings.Replace(reviewPack, "Minimal", "Edited", 1)
	grown := strings.Replace(twoPacks, `"beta"`, `"gamma":{"path":"packs/c.json"},"beta"`, 1)
	writeProject(t, startup, map[string]string{"packs/a.json": edited, "packs/c.json": reviewPack, "jpack.json": grown})
	rig.answers(t, "invalid")
	answer := readReview(t, ts, "")
	want := map[string]struct{ locked, digest, lock string }{
		"jpack.json":   {sha256Digest([]byte(twoPacks)), sha256Digest([]byte(grown)), "other"},
		"packs/a.json": {sha256Digest([]byte(reviewPack)), sha256Digest([]byte(edited)), "other"},
		"packs/b.json": {sha256Digest([]byte(otherPack)), sha256Digest([]byte(otherPack)), "same"},
		"packs/c.json": {"", sha256Digest([]byte(reviewPack)), "none"},
		"packs/d.json": {sha256Digest([]byte(removed)), "", "removed"},
	}
	if len(answer.Files) != len(want) {
		t.Fatalf("the startup desk's review shows %+v", answer.Files)
	}
	for path, w := range want {
		if file := fileOf(t, answer, path); file.Locked != w.locked || file.Digest != w.digest || file.Lock != w.lock {
			t.Errorf("the startup desk shows %s as %+v, want the lock's %q beside the file's %q", path, file, w.locked, w.digest)
		}
	}
	// gamma names alpha's locked bytes, and the lock pins nothing for gamma.
	if gamma := fileOf(t, answer, "packs/c.json"); gamma.ID != "gamma" || gamma.Locked != "" {
		t.Errorf("an id the lock does not name was given another id's entry: %+v", gamma)
	}

	row := createGatedDesk(t, ts)
	named := strings.Replace(otherPack, "Minimal", "Named", 1)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": named, "jpack.json": strings.Replace(wantGatedConfig, `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)})
	writeProject(t, row.Folder, map[string]string{"jpack.lock.json": string(lockOf(t, row.Folder, map[string]string{"alpha": "packs/a.json"}))})
	rig.answers(t, "valid")
	if alpha := fileOf(t, readReview(t, ts, row.ID), "packs/a.json"); alpha.ID != "alpha" || alpha.Locked != sha256Digest([]byte(named)) || alpha.Lock != "same" {
		t.Errorf("the named desk shows alpha as %+v, want its own lock's entry", alpha)
	}
	if alpha := fileOf(t, readReview(t, ts, ""), "packs/a.json"); alpha.Locked != sha256Digest([]byte(reviewPack)) {
		t.Errorf("the startup desk answered with another desk's lock: %+v", alpha)
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
	if first.Status != "invalid" || names["config-drift"].Name == "" || names["lock-entry-missing"].ID != "alpha" || fileOf(t, first, "packs/a.json").Lock != "none" || shown(first, fileOf(t, first, "packs/a.json").Now) != reviewPack {
		t.Fatalf("the first review answered %+v", first)
	}
	if status, data := confirm(t, ts, row.ID, first.Token); status != 200 {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	jpackIn(t, bin, row.Folder, "packs", "verify", "--config", "jpack.json", "--format", "json")
	if again := readReview(t, ts, row.ID); again.Status != "valid" || len(again.Findings) != 0 || fileOf(t, again, "packs/a.json").Lock != "same" || fileOf(t, again, "packs/a.json").Locked != sha256Digest([]byte(reviewPack)) {
		t.Errorf("after the lock the review answered %+v", again)
	}

	edited := strings.Replace(reviewPack, "Minimal literal decision", "Edited decision", 1)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": edited})
	second := readReview(t, ts, row.ID)
	alpha := fileOf(t, second, "packs/a.json")
	if len(second.Findings) != 1 || second.Findings[0].Name != "document-drift" || shown(second, *alpha.Earlier) != reviewPack || shown(second, alpha.Now) != edited || alpha.Locked != sha256Digest([]byte(reviewPack)) {
		t.Fatalf("the edit was reviewed as %+v", second)
	}
	lock := readFile(t, filepath.Join(row.Folder, "jpack.lock.json"))
	if status, _ := confirm(t, ts, row.ID, first.Token); status != http.StatusConflict || readFile(t, filepath.Join(row.Folder, "jpack.lock.json")) != lock {
		t.Errorf("a confirmation made before the edit answered %d, or changed the lock", status)
	}
	if status, data := confirm(t, ts, row.ID, second.Token); status != 200 {
		t.Fatalf("the second confirmation answered %d %s", status, data)
	}
	jpackIn(t, bin, row.Folder, "packs", "verify", "--config", "jpack.json", "--format", "json")
}
