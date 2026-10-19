package desk

import (
	"context"
	"encoding/json"
	exchangeCatalog "github.com/Judgment-Pack/judgment-pack-desk/internal/exchange"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExchangeExecutableFixturesWithRealCompanions(t *testing.T) {
	runtime, runner := os.Getenv("JPACK_EXCHANGE_TEST_BIN"), os.Getenv("JPACK_EXCHANGE_TEST_RUNNER")
	if runtime == "" || runner == "" {
		t.Skip("requires Runtime and offline Runner binaries")
	}
	for _, name := range []string{"example-entry", "named-unbound-entry"} {
		t.Run(name, func(t *testing.T) {
			catalog := t.TempDir()
			root := filepath.Join("..", "exchange", "testdata", name)
			err := filepath.WalkDir(root, func(path string, entry os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if entry.IsDir() {
					return nil
				}
				relative, _ := filepath.Rel(root, path)
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
			if err != nil {
				t.Fatal(err)
			}
			git := func(args ...string) {
				cmd := exec.Command("git", append([]string{"-C", catalog, "-c", "commit.gpgSign=false", "-c", "tag.gpgSign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
				if raw, e := cmd.CombinedOutput(); e != nil {
					t.Fatal(e, string(raw))
				}
			}
			git("init", "-q")
			git("add", ".")
			git("commit", "-qm", "fixture")
			git("tag", "v1.0.0")
			for installation := 0; installation < 2; installation++ {
				tree, close, err := exchangeCatalog.OpenRelease(context.Background(), catalog, "v1.0.0")
				if err != nil {
					t.Fatal(err)
				}
				s := &Server{cfg: Config{JpackBin: runtime, RunnerBin: runner}}
				result, err := exchangeCatalog.Review(context.Background(), tree, catalog, "v1.0.0", tree.Commit, s.exchangeCheck, s.exchangeReplay)
				close()
				if err != nil || len(result.Candidates) != 1 {
					t.Fatalf("installation %d: %v %v", installation, err, result.Unavailable)
				}
				if len(result.Candidates[0].FixtureReports) != 1 {
					t.Fatal("replay report not retained")
				}
				var report map[string]any
				json.Unmarshal(result.Candidates[0].FixtureReports["valid"], &report)
				if report["binding"] != "not-checked" {
					t.Fatal("claimed a run binding", report)
				}
			}
		})
	}
}
func TestExchangeReplayRequiresInstalledOfflineRunner(t *testing.T) {
	s := &Server{}
	if _, err := s.exchangeReplay(context.Background(), exchangeCatalog.ReplayInput{}); err == nil {
		t.Fatal("missing verifier accepted")
	}
}

func TestPreparationReplayFailureAcceptsOnlyMatchingClosedReports(t *testing.T) {
	for _, tc := range []struct {
		code      int
		raw, want string
	}{
		{1, `{"result":"refusal","refusal":"kind-unread"}`, "Offline preparation check: kind-unread"},
		{2, `{"result":"finding","finding":"acquisition-stale"}`, "Offline preparation check: acquisition-stale"},
		{1, `{"result":"finding","refusal":"kind-unread"}`, "Invalid preparation verification report"},
		{2, `{"result":"finding","finding":"input-malformed"}`, "Invalid preparation verification report"},
		{1, `{"result":"refusal","refusal":"kind-unread","detail":"private diagnostic"}`, "Invalid preparation verification report"},
		{2, `{"result":"finding","finding":"private diagnostic"}`, "Invalid preparation verification report"},
		{3, `{"result":"refusal","refusal":"input-malformed"}`, "Invalid preparation verification report"},
		{1, `null`, "Invalid preparation verification report"},
	} {
		if err := preparationReplayFailure([]byte(tc.raw), tc.code); err.Error() != tc.want {
			t.Errorf("%s (exit %d): %v", tc.raw, tc.code, err)
		}
	}
}
