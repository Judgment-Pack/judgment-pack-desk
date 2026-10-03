//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package desk

// Downloading the trail as exact bytes (ADR-0010, section 2). The files the
// runtime writes are put in place by hand, and a test that needs a writer in
// the middle of an append takes the runtime writer's own lock itself: flock's
// exclusive lock on the trail, for the trail and its sidecar, and on the
// stamps file, for the stamps. No runtime runs here, except in the last test,
// which drives the real one and skips without it.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// trailDesk is a startup desk over a project whose jpack.json is config and
// that has an owner-only `.desk-private/audit`, and that folder.
func trailDesk(t *testing.T, config string) (*httptest.Server, *auditRig, string, string) {
	t.Helper()
	ts, rig, project := auditDesk(t, withAuditVersions, config)
	audit := filepath.Join(project, ".desk-private", "audit")
	if err := os.MkdirAll(audit, 0o700); err != nil {
		t.Fatal(err)
	}
	return ts, rig, project, audit
}

// download asks a desk for one file. query is the whole query string.
func download(t *testing.T, ts *httptest.Server, desk, query string) (int, http.Header, []byte) {
	t.Helper()
	r, err := http.NewRequest("GET", ts.URL+"/api/audit/trail?"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	bearer(r)
	if desk != "" {
		r.Header.Set("X-Jpack-Desk", desk)
	}
	response, err := ts.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("the download ended early: %v", err)
	}
	return response.StatusCode, response.Header, data
}

// writerLock takes flock's exclusive lock on path, as the runtime's writer
// does, and returns the release.
func writerLock(t *testing.T, path string) func() {
	t.Helper()
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	return func() {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
	}
}

func appendTo(t *testing.T, path, data string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(data); err != nil {
		t.Fatal(err)
	}
}

// **The bytes on disk, untouched, under the runtime's own name.** A trail
// whose last line has no newline, a sidecar with CRLF line ends and stamps
// that are not UTF-8 each come back byte for byte, as a download of the
// runtime's file name and a type nothing re-encodes. A desk Desk made hands
// over its own trail.
func TestTheTrailIsHandedOverAsExactBytes(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	ts, rig, _, audit := trailDesk(t, auditedConfig)
	files := map[string]string{
		"evaluations": `{"recordVersion":"1","sequence":1,"note":"& 1.0"}` + "\n" + `{"recordVersion":"1","sequence":2}` + "\n" + `{"torn":`,
		"signatures":  "{\"kind\":\"record-signature\",\"sequence\":1}\r\n{\"kind\":\"record-signature\",\"sequence\":2}\r\n",
		"stamps":      "\xff\xfe{\"stamp\":1}\n{\"stamp\":2}",
	}
	for which, data := range files {
		if err := os.WriteFile(filepath.Join(audit, auditTrailFiles[which]), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for which, data := range files {
		status, header, body := download(t, ts, "", "file="+which)
		name := auditTrailFiles[which]
		if status != http.StatusOK || !bytes.Equal(body, []byte(data)) {
			t.Errorf("%s: answered %d %q, want its bytes %q", which, status, body, data)
		}
		if header.Get("Content-Type") != "application/octet-stream" || header.Get("Content-Disposition") != "attachment; filename="+name ||
			header.Get("Content-Length") != strconv.Itoa(len(data)) || header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: answered with %v", which, header)
		}
	}
	if calls := rig.ran(t); len(calls) != 0 {
		t.Errorf("a download ran the runtime: %q", calls)
	}

	_, ts2, _ := gatesServer(t, rig.bin)
	row := createGatedDesk(t, ts2)
	own := `{"desk":"made"}` + "\n"
	if err := os.WriteFile(filepath.Join(row.Folder, ".desk-private", "audit", "evaluations.jsonl"), []byte(own), 0o600); err != nil {
		t.Fatal(err)
	}
	if status, _, body := download(t, ts2, row.ID, "file=evaluations"); status != http.StatusOK || string(body) != own {
		t.Errorf("the made desk handed over %d %q, want its own trail", status, body)
	}
}

// **Ask for one of three files, once, or be refused.** Nothing but `file`,
// named once, with one of the three values, is answered with bytes.
func TestABadTrailRequestIsRefused(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	ts, _, _, audit := trailDesk(t, auditedConfig)
	for _, name := range []string{"evaluations.jsonl", "signatures.jsonl", "stamps.jsonl"} {
		if err := os.WriteFile(filepath.Join(audit, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{
		"", "file", "file=", "file=other", "file=EVALUATIONS", "file=evaluations.jsonl", "file=..%2Fjpack.json", "file=..%2F..%2Fjpack.json",
		"file=evaluations&file=evaluations", "file=evaluations&file=stamps", "file=evaluations&x=1", "x=evaluations",
		"file=%zz", "file=evaluations;x=1", "file=evaluations%00", "file=%20evaluations", "file=evaluations&%zz=1", "file=evaluations&a=%zz",
	} {
		status, _, body := download(t, ts, "", query)
		if status != http.StatusBadRequest || refusalOf(body) != "Ask for one file, once: file=evaluations, file=signatures or file=stamps." {
			t.Errorf("%q: answered %d %q", query, status, body)
		}
	}
	// A well-formed spelling of a good request is that request.
	if status, _, body := download(t, ts, "", "file=%65valuations"); status != http.StatusOK || string(body) != "{}\n" {
		t.Errorf("an encoded file=evaluations answered %d %q", status, body)
	}
}

// **Through the project's root, refusing links.** A trail that is a link, an
// audit directory reached through a link (to a folder in the project or out
// of it), and an audit directory that jpack.json places outside the project
// are each refused, and none of their bytes is served.
func TestATrailReachedThroughALinkIsRefused(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	secret := "{\"elsewhere\":true}\n"
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "evaluations.jsonl"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}

	ts, _, project, audit := trailDesk(t, auditedConfig)
	if err := os.WriteFile(filepath.Join(project, "kept.jsonl"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(project, "kept.jsonl"), filepath.Join(audit, "evaluations.jsonl")); err != nil {
		t.Skip(err)
	}
	refused := func(what string, wantStatus int, says string) {
		t.Helper()
		status, _, body := download(t, ts, "", "file=evaluations")
		if status != wantStatus || !strings.Contains(refusalOf(body), says) || bytes.Contains(body, []byte("elsewhere")) {
			t.Errorf("%s: answered %d %q, want %d saying %q", what, status, body, wantStatus, says)
		}
	}
	refused("a trail that is a link", http.StatusForbidden, "passes through a symbolic link")

	os.Remove(filepath.Join(audit, "evaluations.jsonl"))
	if err := os.Mkdir(filepath.Join(project, "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "other", "evaluations.jsonl"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Remove(audit)
	if err := os.Symlink(filepath.Join(project, "other"), audit); err != nil {
		t.Fatal(err)
	}
	refused("an audit directory that is a link inside the project", http.StatusForbidden, "passes through a symbolic link")
	os.Remove(audit)
	if err := os.Symlink(outside, audit); err != nil {
		t.Fatal(err)
	}
	refused("an audit directory that is a link out of the project", http.StatusForbidden, "passes through a symbolic link")
	os.Remove(audit)
	private := filepath.Join(project, ".desk-private")
	os.Remove(private)
	if err := os.Symlink(outside, private); err != nil {
		t.Fatal(err)
	}
	refused("a link on the way to the audit directory", http.StatusForbidden, "passes through a symbolic link")

	for _, dir := range []string{"../" + filepath.Base(outside), outside, "audit/../../" + filepath.Base(outside), `audit\..\..`} {
		config := strings.Replace(auditedConfig, `".desk-private/audit"`, strconv.Quote(dir), 1)
		ts, _, _, _ := trailDesk(t, config)
		status, _, body := download(t, ts, "", "file=evaluations")
		if status != http.StatusForbidden || !strings.Contains(refusalOf(body), "is not a folder inside the project that Desk reads, so Desk does not read it") || bytes.Contains(body, []byte("elsewhere")) {
			t.Errorf("an audit directory at %q: answered %d %q", dir, status, body)
		}
	}

	// A trail that is a folder is not a file to hand over.
	ts, _, _, audit = trailDesk(t, auditedConfig)
	if err := os.Mkdir(filepath.Join(audit, "evaluations.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	if status, _, body := download(t, ts, "", "file=evaluations"); status != http.StatusForbidden || !strings.Contains(refusalOf(body), "not a folder and a regular file") {
		t.Errorf("a trail that is a folder: answered %d %q", status, body)
	}
}

// **What is not there is said, as not there.** A file the audit directory
// does not hold is 404, and names it; a sidecar with no trail beside it has no
// lock to be read under, and is refused; a project that keeps no trail says
// so; and the download is unavailable where the review is.
func TestAMissingTrailFileIsSaidToBeMissing(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	ts, _, _, audit := trailDesk(t, auditedConfig)
	for which, name := range auditTrailFiles {
		status, _, body := download(t, ts, "", "file="+which)
		if status != http.StatusNotFound || refusalOf(body) != "There is no "+name+" in this project's audit directory, .desk-private/audit." {
			t.Errorf("%s: answered %d %q", which, status, body)
		}
	}
	if err := os.WriteFile(filepath.Join(audit, "signatures.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if status, _, body := download(t, ts, "", "file=signatures"); status != http.StatusConflict || refusalOf(body) != "The signature sidecar is read under the trail's lock, and there is no evaluations.jsonl beside it in .desk-private/audit." {
		t.Errorf("a sidecar with no trail: answered %d %q", status, body)
	}
	os.RemoveAll(filepath.Dir(audit))
	if status, _, body := download(t, ts, "", "file=evaluations"); status != http.StatusNotFound || !strings.HasPrefix(refusalOf(body), "There is no evaluations.jsonl") {
		t.Errorf("an audit directory not made yet: answered %d %q", status, body)
	}

	ts, _, _, _ = trailDesk(t, `{"configVersion":"5","packs":{}}`)
	if status, _, body := download(t, ts, "", "file=evaluations"); status != http.StatusNotFound || refusalOf(body) != "This project's jpack.json declares no audit directory, so it keeps no trail." {
		t.Errorf("a project with no audit directory: answered %d %q", status, body)
	}

	ts, _, project, audit := trailDesk(t, auditedConfig)
	if err := os.WriteFile(filepath.Join(audit, "evaluations.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	writeProject(t, elsewhere, map[string]string{"jpack.json": auditedConfig})
	t.Setenv("JPACK_CONFIG", filepath.Join(elsewhere, "jpack.json"))
	want := "This project's runtime reads the configuration that JPACK_CONFIG names where Desk was started, and not this project's jpack.json, so Desk does not check its decision record here."
	if status, _, body := download(t, ts, "", "file=evaluations"); status != http.StatusConflict || refusalOf(body) != want {
		t.Errorf("under another project's JPACK_CONFIG: answered %d %q", status, body)
	}
	t.Setenv("JPACK_CONFIG", filepath.Join(project, "jpack.json"))
	if status, _, body := download(t, ts, "", "file=evaluations"); status != http.StatusOK || string(body) != "{}\n" {
		t.Errorf("under this project's own JPACK_CONFIG: answered %d %q", status, body)
	}
}

// **A writer in the middle of an append is waited for, and its line is
// whole.** The test holds the lock the runtime's writer holds, the trail's
// for the trail and its sidecar and the stamps file's own for the stamps,
// with half a line written. The download waits for it; once the line is
// finished and the lock released, the bytes served end with that whole line.
// A writer holding another file's lock does not hold it up.
func TestAWriterInTheMiddleOfAnAppendIsWaitedFor(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	for _, tc := range []struct{ which, lock string }{
		{"evaluations", "evaluations.jsonl"},
		{"signatures", "evaluations.jsonl"},
		{"stamps", "stamps.jsonl"},
	} {
		ts, _, _, audit := trailDesk(t, auditedConfig)
		for _, name := range []string{"evaluations.jsonl", "signatures.jsonl", "stamps.jsonl"} {
			if err := os.WriteFile(filepath.Join(audit, name), []byte(`{"line":1}`+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		target := filepath.Join(audit, auditTrailFiles[tc.which])
		release := writerLock(t, filepath.Join(audit, tc.lock))
		appendTo(t, target, `{"line":2,`)
		type answer struct {
			status int
			body   []byte
		}
		done := make(chan answer, 1)
		go func() {
			status, _, body := download(t, ts, "", "file="+tc.which)
			done <- answer{status, body}
		}()
		var got answer
		waited := false
		select {
		case got = <-done:
			t.Errorf("%s: answered %d %q while a writer held %s", tc.which, got.status, got.body, tc.lock)
		case <-time.After(300 * time.Millisecond):
			waited = true
		}
		appendTo(t, target, `"whole":true}`+"\n")
		release()
		if waited {
			got = <-done
		}
		if want := `{"line":1}` + "\n" + `{"line":2,"whole":true}` + "\n"; got.status != http.StatusOK || string(got.body) != want {
			t.Errorf("%s: answered %d %q, want %q", tc.which, got.status, got.body, want)
		}
	}

	// The stamps writer's lock does not hold up the trail, nor the trail's
	// the stamps.
	ts, _, _, audit := trailDesk(t, auditedConfig)
	for _, name := range []string{"evaluations.jsonl", "stamps.jsonl"} {
		if err := os.WriteFile(filepath.Join(audit, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ which, lock string }{{"evaluations", "stamps.jsonl"}, {"stamps", "evaluations.jsonl"}} {
		release := writerLock(t, filepath.Join(audit, tc.lock))
		start := time.Now()
		status, _, body := download(t, ts, "", "file="+tc.which)
		release()
		if status != http.StatusOK || string(body) != "{}\n" || time.Since(start) > 2*time.Second {
			t.Errorf("%s while %s was locked: answered %d %q after %s", tc.which, tc.lock, status, body, time.Since(start))
		}
	}
}

// **A lock held past the bound is refused, not waited out.** The answer
// says so, serves nothing, and comes before the writer lets go.
func TestALockHeldTooLongIsRefused(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	defer func(previous time.Duration) { auditLockWait = previous }(auditLockWait)
	auditLockWait = 200 * time.Millisecond
	ts, _, _, audit := trailDesk(t, auditedConfig)
	trail := filepath.Join(audit, "evaluations.jsonl")
	if err := os.WriteFile(trail, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	release := writerLock(t, trail)
	let := time.AfterFunc(3*time.Second, release)
	defer func() {
		if let.Stop() {
			release()
		}
	}()
	status, _, body := download(t, ts, "", "file=evaluations")
	if status != http.StatusServiceUnavailable || refusalOf(body) != "A runtime held the lock on evaluations.jsonl for longer than 200ms. Try again." {
		t.Errorf("answered %d %q", status, body)
	}
}

// **Where no flock can be taken, nothing is handed over.** A file system that
// answers that it supports none, as the runtime reads those answers, is
// refused with a sentence that says why; any other failure to lock is an
// error, and neither serves a byte.
func TestNoTrailIsHandedOverWithoutTheLock(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	defer func(previous func(*os.File, int) error) { flockAudit = previous }(flockAudit)
	ts, _, _, audit := trailDesk(t, auditedConfig)
	if err := os.WriteFile(filepath.Join(audit, "evaluations.jsonl"), []byte("{\"kept\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		answer error
		status int
		says   string
	}{
		{syscall.EOPNOTSUPP, http.StatusNotImplemented, "Desk can take no lock on evaluations.jsonl here, so it cannot read it between two writes, and does not hand it over."},
		{syscall.ENOSYS, http.StatusNotImplemented, "Desk can take no lock on evaluations.jsonl here, so it cannot read it between two writes, and does not hand it over."},
		{syscall.ENOLCK, http.StatusInternalServerError, "evaluations.jsonl could not be read: "},
	} {
		flockAudit = func(*os.File, int) error { return tc.answer }
		status, _, body := download(t, ts, "", "file=evaluations")
		if status != tc.status || !strings.HasPrefix(refusalOf(body), tc.says) || bytes.Contains(body, []byte("kept")) {
			t.Errorf("flock answering %v: answered %d %q", tc.answer, status, body)
		}
	}
}

// **The panel names the files there are to download**, as the download would
// open them: with a report and with the runtime's refusal, never a link, and
// never with an older runtime or a project that keeps no trail.
func TestThePanelNamesTheFilesThereAreToDownload(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	ts, rig, project, audit := trailDesk(t, auditedConfig)
	rig.answers(t, 0, auditValidReport)
	files := func() []string {
		t.Helper()
		status, answer, refusal := readAudit(t, ts, "")
		if status != http.StatusOK {
			t.Fatalf("the panel answered %d %q", status, refusal)
		}
		return answer.Files
	}
	if got := files(); len(got) != 0 {
		t.Errorf("an empty audit directory offers %q", got)
	}
	for _, name := range []string{"evaluations.jsonl", "stamps.jsonl"} {
		if err := os.WriteFile(filepath.Join(audit, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(project, "jpack.json"), filepath.Join(audit, "signatures.jsonl")); err != nil {
		t.Fatal(err)
	}
	if got := files(); !slices.Equal(got, []string{"evaluations", "stamps"}) {
		t.Errorf("the panel offers %q, want the trail and the stamps", got)
	}
	os.Remove(filepath.Join(audit, "signatures.jsonl"))
	if err := os.WriteFile(filepath.Join(audit, "signatures.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rig.answers(t, 4, auditNoTrailYet)
	if got := files(); !slices.Equal(got, []string{"evaluations", "signatures", "stamps"}) {
		t.Errorf("with the runtime's refusal the panel offers %q", got)
	}

	older, olderRig, _ := auditDesk(t, allConfigVersions, auditedConfig)
	olderRig.answers(t, 0, auditValidReport)
	if status, answer, _ := readAudit(t, older, ""); status != http.StatusOK || answer.State != auditStateOlder || answer.Files != nil {
		t.Errorf("an older runtime's panel answered %d %+v", status, answer)
	}
	none, _, _ := auditDesk(t, withAuditVersions, `{"configVersion":"5","packs":{}}`)
	if status, answer, _ := readAudit(t, none, ""); status != http.StatusOK || answer.State != auditStateNoTrail || answer.Files != nil {
		t.Errorf("a project that keeps no trail answered %d %+v", status, answer)
	}
}

// The real runtime, end to end on a desk Desk made: two deciding runs signed
// with a key the runtime made, the trail and its sidecar downloaded, and the
// runtime's own `audit verify` over the downloaded copy, with the key's public
// half, finds it consistent and every record signed. Skipped without a runtime
// that reads "6".
func TestTheTrailDownloadWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	var schema struct {
		Supported []string `json:"supportedConfigVersions"`
	}
	if err := json.Unmarshal(jpackIn(t, bin, t.TempDir(), "packs", "schema", "--format", "json"), &schema); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(schema.Supported, "6") {
		t.Skip("this runtime has no audit commands")
	}
	keys := t.TempDir()
	seed := filepath.Join(keys, "desk.seed")
	var generated struct {
		PublicKey string `json:"publicKey"`
	}
	if err := json.Unmarshal(jpackIn(t, bin, keys, "audit", "key", "generate", seed, "--format", "json"), &generated); err != nil || len(generated.PublicKey) != 64 {
		t.Fatalf("audit key generate: %+v %v", generated, err)
	}
	public := filepath.Join(keys, "desk.pub")
	if err := os.WriteFile(public, []byte(generated.PublicKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, ts, _ := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	config := strings.Replace(gatedConfigFor(t, row.ConfigVersion), `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": reviewPack, "jpack.json": config})
	jpackIn(t, bin, row.Folder, "packs", "lock", "--config", "jpack.json", "--format", "json")
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		run := exec.Command(bin, "experimental", "evaluate", "--config", "jpack.json", "--pack-id", "alpha", "--facts", facts, "--format", "json")
		run.Dir = row.Folder
		run.Env = append(os.Environ(), "JPACK_CONFIG=", "JPACK_SIGNING_KEY="+seed)
		if out, err := run.CombinedOutput(); err != nil {
			t.Fatalf("a deciding run: %v\n%s", err, out)
		}
	}
	status, answer, refusal := readAudit(t, ts, row.ID)
	if status != http.StatusOK || answer.State != auditStateReport || !slices.Equal(answer.Files, []string{"evaluations", "signatures"}) {
		t.Fatalf("the panel answered %d %+v %q", status, answer, refusal)
	}
	copies := t.TempDir()
	for _, which := range []string{"evaluations", "signatures"} {
		status, _, body := download(t, ts, row.ID, "file="+which)
		onDisk, err := os.ReadFile(filepath.Join(row.Folder, ".desk-private", "audit", auditTrailFiles[which]))
		if err != nil || status != http.StatusOK || !bytes.Equal(body, onDisk) {
			t.Fatalf("%s: downloaded %d, %d bytes, against %d on disk (%v)", which, status, len(body), len(onDisk), err)
		}
		if err := os.WriteFile(filepath.Join(copies, auditTrailFiles[which]), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var verified struct {
		Status   string        `json:"status"`
		Coverage auditCoverage `json:"coverage"`
	}
	out := jpackIn(t, bin, copies, "audit", "verify", "--trail", filepath.Join(copies, "evaluations.jsonl"), "--public-key", public, "--format", "json")
	if err := json.Unmarshal(out, &verified); err != nil || verified.Status != "valid" || verified.Coverage.Chained != 2 ||
		verified.Coverage.Signed.Status != "through" || verified.Coverage.Signed.Through != 2 || verified.Coverage.SignedRecords != 2 {
		t.Errorf("the runtime's audit verify over the downloaded copy answered %s", out)
	}
}
