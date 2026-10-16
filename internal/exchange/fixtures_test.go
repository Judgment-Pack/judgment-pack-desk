package exchange

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type historicalMemory struct {
	memoryTree
	prior []HistoricalTree
}

func (t historicalMemory) PriorReleases(context.Context, string) ([]HistoricalTree, error) {
	return t.prior, nil
}
func executableFixture(t *testing.T, name string) memoryTree {
	t.Helper()
	tree := memoryTree{}
	root := filepath.Join("testdata", name)
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		tree[filepath.ToSlash(rel)] = raw
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
func readingFixture(_ context.Context, f ReplayInput) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"result": "reading", "digest": f.ExpectedDigest, "binding": "not-checked"})
}
func TestExecutableImportRequiresReplayAndRetainsReport(t *testing.T) {
	tree := executableFixture(t, "example-entry")
	noReplay, err := Review(context.Background(), tree, "catalog", "v1.0.0", strings.Repeat("a", 40), testCheck)
	if err != nil || len(noReplay.Candidates) != 0 {
		t.Fatal("accepted without verifier", err)
	}
	result, err := Review(context.Background(), tree, "catalog", "v1.0.0", strings.Repeat("a", 40), testCheck, readingFixture)
	if err != nil || len(result.Candidates) != 1 || len(result.Candidates[0].FixtureReports) != 1 {
		t.Fatal(err, result.Unavailable)
	}
	for path := range tree {
		if strings.HasSuffix(path, "/preparation.json") {
			tree[path] = []byte("{}")
		}
	}
	result, err = Review(context.Background(), tree, "catalog", "v1.0.0", strings.Repeat("a", 40), testCheck, readingFixture)
	if err != nil || len(result.Candidates) != 0 {
		t.Fatal("accepted modified expected preparation", err)
	}
}
func TestNamedArtifactChecksHistoricalContent(t *testing.T) {
	tree := executableFixture(t, "named-unbound-entry")
	current, err := Review(context.Background(), historicalMemory{tree, nil}, "catalog", "v2.0.0", strings.Repeat("b", 40), testCheck, readingFixture)
	if err != nil || len(current.Candidates) != 1 {
		t.Fatal(err, current.Unavailable)
	}
	history := historicalMemory{tree, []HistoricalTree{{"v1.0.0", strings.Repeat("a", 40), tree}}}
	if err = checkCatalogHistory(context.Background(), history, &current, testCheck); err != nil || len(current.History) != 1 {
		t.Fatal(err)
	}
	current.Candidates[0].Digest = "sha256:" + strings.Repeat("0", 64)
	if err = checkCatalogHistory(context.Background(), history, &current, testCheck); err == nil || err.Error() != "artifact-content-changed" {
		t.Fatal(err)
	}
	current.Candidates[0].Entry.Artifact.Version = "0.2.0"
	if err = checkCatalogHistory(context.Background(), history, &current, testCheck); err != nil {
		t.Fatal(err)
	}
	current.Candidates[0].Entry.Upgrade = map[string]string{"sha256:" + strings.Repeat("1", 64): "changed"}
	if err = checkCatalogHistory(context.Background(), history, &current, testCheck); err == nil {
		t.Fatal("accepted unknown upgrade origin")
	}
}
