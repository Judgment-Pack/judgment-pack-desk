package desk

import (
	"context"
	"encoding/json"
	exchangeCatalog "github.com/Judgment-Pack/judgment-pack-desk/internal/exchange"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExchangeReviewImportAndWithdrawal(t *testing.T) {
	bin := os.Getenv("JPACK_EXCHANGE_TEST_BIN")
	if bin == "" {
		t.Skip("requires research-capable Runtime")
	}
	catalog := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", catalog, "-c", "tag.gpgSign=false", "-c", "commit.gpgSign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(out))
		}
	}
	git("init", "-q")
	e := filepath.WalkDir("../exchange/testdata/preset", func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() {
			return nil
		}
		relative, _ := filepath.Rel("../exchange/testdata/preset", path)
		dest := filepath.Join(catalog, relative)
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return e
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		return os.WriteFile(dest, raw, 0600)
	})
	if e != nil {
		t.Fatal(e)
	}
	release := func(tag string) {
		t.Helper()
		git("add", ".")
		git("commit", "-qm", tag)
		git("tag", "-a", tag, "-m", tag)
	}
	release("v1.0.0")
	project, config := t.TempDir(), t.TempDir()
	os.Chmod(config, 0700)
	writeProject(t, project, map[string]string{"jpack.json": `{"configVersion":"2","packs":{}}`})
	s, ts := startDesk(t, Config{JpackBin: bin, ProjectDir: project, DeskConfigDir: config, Token: testToken})
	defer func() { ts.Close(); s.Close() }()
	call := func(method, path string, body any) (int, []byte) {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	review := func(tag string) exchangeReviewed {
		t.Helper()
		code, raw := call("POST", "/api/exchange/review", map[string]any{"location": catalog, "tag": tag})
		if code != 200 {
			t.Fatal("review", code, string(raw))
		}
		var answer struct {
			Review exchangeReviewed `json:"review"`
		}
		if json.Unmarshal(raw, &answer) != nil || len(answer.Review.Release.Candidates) != 1 {
			t.Fatal(string(raw))
		}
		return answer.Review
	}
	first := review("v1.0.0")
	request := map[string]any{"reviewId": first.ID, "entryId": first.Release.Candidates[0].Row.ID, "base": first.Base}
	if code, raw := call("POST", "/api/exchange/imports", request); code != 400 {
		t.Fatal("unreviewed imported", code, string(raw))
	}
	request["reviewed"] = true
	code, raw := call("POST", "/api/exchange/imports", request)
	if code != 201 {
		t.Fatal("import", code, string(raw))
	}
	var installed exchangeImport
	json.Unmarshal(raw, &installed)
	if installed.At == "" || installed.Reviewer.Kind != "local-access" || installed.PreviousCommit != "" {
		t.Fatal(installed)
	}
	// A changed README is a reviewed update even with unchanged digest pins.
	os.WriteFile(filepath.Join(catalog, "entries/example/company-research/README.md"), []byte("Revised installation guidance."), 0600)
	release("v1.1.0")
	next := review("v1.1.0")
	code, raw = call("POST", "/api/exchange/imports", map[string]any{"reviewed": true, "reviewId": next.ID, "entryId": installed.EntryID, "base": next.Base})
	if code != 201 {
		t.Fatal(code, string(raw))
	}
	var updated exchangeImport
	json.Unmarshal(raw, &updated)
	if updated.PreviousCommit != installed.Commit {
		t.Fatal("missing reviewed base", updated)
	}
	// Restart the server: imported definitions and exact completed receipts
	// belong to the workspace, never the tab or an in-memory catalog cache.
	ts.Close()
	s.Close()
	s, ts = startDesk(t, Config{JpackBin: bin, ProjectDir: project, DeskConfigDir: config, Token: testToken})
	if code, raw = call("GET", "/api/exchange/imports/"+installed.ID, nil); code != 200 || !strings.Contains(string(raw), `"document":`) {
		t.Fatal("retained import after restart", code, string(raw))
	}
	// Returning a completed receipt after another import must not re-apply it.
	code, raw = call("POST", "/api/exchange/imports", request)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	var repeated exchangeImport
	json.Unmarshal(raw, &repeated)
	if repeated.ID != installed.ID {
		t.Fatal("completion lost")
	}
	if code, raw = call("POST", "/api/exchange/review", map[string]any{"location": catalog, "tag": "v1.0.0"}); code != 409 {
		t.Fatal("downgrade", code, string(raw))
	}
	git("tag", "-a", "-f", "v1.1.0", "v1.0.0^{}", "-m", "Moved tag")
	if code, raw = call("POST", "/api/exchange/review", map[string]any{"location": catalog, "tag": "v1.1.0"}); code != 409 {
		t.Fatal("moved tag", code, string(raw))
	}
	// Absence in a newer release leaves both accepted history records intact.
	os.RemoveAll(filepath.Join(catalog, "entries"))
	os.WriteFile(filepath.Join(catalog, "manifest.json"), []byte("[]\n"), 0600)
	release("v1.2.0")
	code, raw = call("POST", "/api/exchange/review", map[string]any{"location": catalog, "tag": "v1.2.0"})
	if code != 200 || !strings.Contains(string(raw), `"withdrawn":true`) {
		t.Fatal("withdrawal", code, string(raw))
	}
	code, raw = call("GET", "/api/exchange/imports", nil)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	var history struct {
		Items []exchangeImport `json:"items"`
	}
	json.Unmarshal(raw, &history)
	if len(history.Items) != 2 {
		t.Fatal("withdrawal erased history", string(raw))
	}
}

func TestExchangeReviewBoundsExactPersistedBytes(t *testing.T) {
	r := exchangeReviewed{Release: exchangeCatalog.Release{Candidates: []exchangeCatalog.Candidate{{Files: map[string]string{"README.md": strings.Repeat("\x01", retainedArtifactByteLimit/6)}}}}}
	if _, _, e := encodeExchangeReview(r); e == nil {
		t.Fatal("serialized expansion exceeded artifact storage bound")
	}
	r.Release.Candidates[0].Files["README.md"] = "Retained documentation"
	checked, raw, e := encodeExchangeReview(r)
	if e != nil || !artifactReference(raw).valid() || checked.ID == "" {
		t.Fatal(e)
	}
	again, other, e := encodeExchangeReview(checked)
	if e != nil || again.ID != checked.ID || string(raw) != string(other) {
		t.Fatal("identity changed on readback", e)
	}
}

// A reader upgrade applies only to new catalog admission. An already reviewed
// format-2 import keeps its exact bytes, identity and completion receipt.
func TestExchangeRetainedFormatTwoImportSurvivesReaderUpgrade(t *testing.T) {
	s, _ := newTestServer(t, false)
	prefix := "../exchange/testdata/preset/entries/example/company-research/"
	files := map[string]string{}
	for _, name := range []string{"entry.json", "dialect.json", "README.md"} {
		raw, err := os.ReadFile(prefix + name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(raw)
	}
	files["entry.json"] = strings.Replace(files["entry.json"], `"entryVersion": 3`, `"entryVersion": 2`, 1)
	var entry exchangeCatalog.Preset
	if err := json.Unmarshal([]byte(files["entry.json"]), &entry); err != nil {
		t.Fatal(err)
	}
	c := exchangeCatalog.Candidate{Entry: entry, Files: files, Document: files["dialect.json"], Digest: entry.Dialect.Digest, Row: exchangeCatalog.Row{ID: entry.ID, Kind: entry.Kind, EntryDigest: exchangeCatalog.Digest([]byte(files["entry.json"]))}}
	reviewed, raw, err := encodeExchangeReview(exchangeReviewed{Base: "retained", Release: exchangeCatalog.Release{Location: "catalog", Tag: "v1.0.0", Commit: strings.Repeat("a", 40), Candidates: []exchangeCatalog.Candidate{c}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.artifactRepository().Put(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	record := exchangeImport{ID: digestOf([]byte(reviewed.ID + "\n" + entry.ID)), EntryID: entry.ID, Location: "catalog", Tag: "v1.0.0", Commit: reviewed.Release.Commit, Review: artifactReference(raw)}
	got, old, err := s.readExchangeImport(context.Background(), record)
	if err != nil || got.ID != reviewed.ID || old.Entry.EntryVersion != 2 || !sameEvaluationValue(old, c) {
		t.Fatal("historical import changed", err, old)
	}
	again, err := s.artifactRepository().Get(context.Background(), record.Review)
	if err != nil || string(again) != string(raw) {
		t.Fatal("historical bytes rewritten", err)
	}
}
