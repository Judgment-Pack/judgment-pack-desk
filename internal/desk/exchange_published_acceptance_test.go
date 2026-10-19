package desk

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	exchangeCatalog "github.com/Judgment-Pack/judgment-pack-desk/internal/exchange"
)

// Opt in to a real signed catalog tag; never copy, rewrite or retag its files.
// These are import/recovery checks, not a publication-assets or research run claim.
func TestExchangeSignedReleaseImportOnTwoInstallations(t *testing.T) {
	catalog, tag, commit := os.Getenv("JPACK_EXCHANGE_ACCEPTANCE_CATALOG"), os.Getenv("JPACK_EXCHANGE_ACCEPTANCE_TAG"), os.Getenv("JPACK_EXCHANGE_ACCEPTANCE_COMMIT")
	if catalog == "" && tag == "" && commit == "" {
		t.Skip("set explicit catalog path, signed tag and expected commit for release acceptance")
	}
	runtime := os.Getenv("JPACK_EXCHANGE_TEST_BIN")
	if catalog == "" || tag == "" || commit == "" || runtime == "" {
		t.Fatal("release acceptance requires JPACK_EXCHANGE_ACCEPTANCE_CATALOG, JPACK_EXCHANGE_ACCEPTANCE_TAG, JPACK_EXCHANGE_ACCEPTANCE_COMMIT and JPACK_EXCHANGE_TEST_BIN")
	}
	git := func(args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", catalog}, args...)...)
		raw, err := cmd.Output()
		if err != nil {
			t.Fatalf("catalog Git %v: %v", args, err)
		}
		return raw
	}
	if got := strings.TrimSpace(string(git("rev-parse", "--verify", "refs/tags/"+tag+"^{commit}"))); got != commit {
		t.Fatalf("release commit differs: got %s, want %s", got, commit)
	}
	git("verify-tag", "refs/tags/"+tag)
	manifest := git("show", commit+":manifest.json")
	var rows []exchangeCatalog.Row
	if err := json.Unmarshal(manifest, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("release acceptance requires a nonempty preset catalog")
	}
	wantIDs := []string{}
	expected := map[string]map[string]string{}
	for _, row := range rows {
		if row.Kind != "preset" {
			t.Fatal("this import acceptance check requires preset entries", row)
		}
		wantIDs = append(wantIDs, row.ID)
		files := map[string]string{}
		for _, name := range []string{"entry.json", "dialect.json", "README.md"} {
			files[name] = string(git("show", commit+":"+row.Path+"/"+name))
		}
		if exchangeCatalog.Digest([]byte(files["entry.json"])) != row.EntryDigest || exchangeCatalog.Digest([]byte(files["dialect.json"])) != row.DialectDigest {
			t.Fatal("tag blobs differ from published digest pins", row.ID)
		}
		expected[row.ID] = files
	}
	var firstRelease []byte
	for _, installation := range []string{"first", "second"} {
		t.Run(installation, func(t *testing.T) {
			project, config := t.TempDir(), t.TempDir()
			if err := os.Chmod(config, 0700); err != nil {
				t.Fatal(err)
			}
			writeProject(t, project, map[string]string{"jpack.json": `{"configVersion":"2","packs":{}}`})
			cfg := Config{JpackBin: runtime, ProjectDir: project, DeskConfigDir: config, Token: testToken}
			s, ts := startDesk(t, cfg)
			t.Cleanup(func() { ts.Close(); s.Close() })
			call := func(method, path string, body any, authenticated bool) (int, []byte) {
				t.Helper()
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest(method, path, bytes.NewReader(raw))
				if authenticated {
					r.Header.Set("Authorization", "Bearer "+testToken)
				}
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				return w.Code, w.Body.Bytes()
			}
			history := func() []exchangeImport {
				t.Helper()
				code, raw := call("GET", "/api/exchange/imports", nil, true)
				var reply struct{ Items []exchangeImport }
				if code != 200 || json.Unmarshal(raw, &reply) != nil {
					t.Fatal("import history unavailable", code, string(raw))
				}
				return reply.Items
			}
			review := func() exchangeReviewed {
				t.Helper()
				code, raw := call("POST", "/api/exchange/review", map[string]any{"location": catalog, "tag": tag}, true)
				var reply struct{ Review exchangeReviewed }
				if code != 200 || json.Unmarshal(raw, &reply) != nil {
					t.Fatal("signed release review", code, string(raw))
				}
				release := reply.Review.Release
				if release.Commit != commit || release.Tag != tag || release.Location != catalog || release.Manifest != string(manifest) || len(release.Unavailable) != 0 || len(release.Candidates) != len(wantIDs) {
					t.Fatal("release not fully checked", release)
				}
				for _, candidate := range release.Candidates {
					if !sameEvaluationValue(candidate.Files, expected[candidate.Row.ID]) || candidate.Digest != candidate.Entry.Dialect.Digest || candidate.Document == "" || len(candidate.FixtureReports) != 0 {
						t.Fatal("checked preset lost exact files or canonical identity", candidate.Row.ID)
					}
				}
				canonical, err := json.Marshal(release)
				if err != nil {
					t.Fatal(err)
				}
				if firstRelease == nil {
					firstRelease = canonical
				} else if !bytes.Equal(firstRelease, canonical) {
					t.Fatal("installations read different catalog bytes or canonical definitions")
				}
				return reply.Review
			}
			if code, _ := call("POST", "/api/exchange/review", map[string]any{"location": catalog, "tag": tag}, false); code != 401 {
				t.Fatal("unauthenticated catalog review admitted", code)
			}
			initial := review()
			for _, candidate := range initial.Release.Candidates {
				request := map[string]any{"reviewId": initial.ID, "entryId": candidate.Row.ID, "base": initial.Base}
				if code, raw := call("POST", "/api/exchange/imports", request, true); code != 400 {
					t.Fatal("unreviewed preset imported", code, string(raw))
				}
				request["reviewed"] = true
				if code, _ := call("POST", "/api/exchange/imports", request, false); code != 401 {
					t.Fatal("unauthenticated import admitted", code)
				}
			}
			if len(history()) != 0 {
				t.Fatal("review or refusal wrote import history")
			}
			requests := []map[string]any{}
			receipts := []exchangeImport{}
			candidates := map[string]exchangeCatalog.Candidate{}
			for _, id := range wantIDs {
				checked := review() // Each import requires the current history base.
				for _, candidate := range checked.Release.Candidates {
					candidates[candidate.Row.ID] = candidate
				}
				request := map[string]any{"reviewId": checked.ID, "entryId": id, "base": checked.Base, "reviewed": true}
				code, raw := call("POST", "/api/exchange/imports", request, true)
				var receipt exchangeImport
				if code != 201 || json.Unmarshal(raw, &receipt) != nil || receipt.Commit != commit || receipt.Tag != tag || receipt.EntryID != id || receipt.Reviewer.Kind != "local-access" || receipt.At == "" || receipt.PreviousCommit != "" {
					t.Fatal("reviewed preset not retained", code, string(raw))
				}
				requests, receipts = append(requests, request), append(receipts, receipt)
			}
			if len(history()) != len(wantIDs) {
				t.Fatal("missing or duplicate imports")
			}
			ts.Close()
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, ts = startDesk(t, cfg)
			before := history()
			for i, receipt := range receipts {
				code, raw := call("GET", "/api/exchange/imports/"+receipt.ID, nil, true)
				var retained struct {
					Import    exchangeImport
					Candidate exchangeCatalog.Candidate
				}
				if code != 200 || json.Unmarshal(raw, &retained) != nil || !sameEvaluationValue(retained.Import, receipt) || !sameEvaluationValue(retained.Candidate, candidates[receipt.EntryID]) {
					t.Fatal("restart changed retained import/document/digests", code, string(raw))
				}
				code, raw = call("POST", "/api/exchange/imports", requests[i], true)
				var repeated exchangeImport
				if code != 200 || json.Unmarshal(raw, &repeated) != nil || !sameEvaluationValue(repeated, receipt) {
					t.Fatal("completed import replay changed receipt", code, string(raw))
				}
			}
			if !sameEvaluationValue(before, history()) {
				t.Fatal("completed requests reapplied imports")
			}
			t.Logf("retained all %d signed-tag presets from %s at %s across restart; no research execution requested", len(wantIDs), tag, commit)
		})
	}
}
