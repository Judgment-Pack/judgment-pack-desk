package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func reviewCopyTree(tree memoryTree) memoryTree {
	out := memoryTree{}
	for k, v := range tree {
		out[k] = append([]byte(nil), v...)
	}
	return out
}
func reviewMetadata(t *testing.T, tree memoryTree, mutate func(map[string]any)) {
	t.Helper()
	var rows []Row
	if err := json.Unmarshal(tree["manifest.json"], &rows); err != nil {
		t.Fatal(err)
	}
	path := rows[0].Path + "/entry.json"
	var entry map[string]any
	json.Unmarshal(tree[path], &entry)
	mutate(entry)
	tree[path], _ = json.Marshal(entry)
	rows[0].EntryDigest = Digest(tree[path])
	tree["manifest.json"], _ = json.Marshal(rows)
}
func TestReviewValidUpgradeEntryDigest(t *testing.T) {
	old := fixture(t)
	current := reviewCopyTree(old)
	var rows []Row
	json.Unmarshal(old["manifest.json"], &rows)
	reviewMetadata(t, current, func(e map[string]any) { e["upgrade"] = map[string]string{rows[0].EntryDigest: "Metadata-only update"} })
	result, err := Review(context.Background(), historicalMemory{current, []HistoricalTree{{"v1.0.0", strings.Repeat("a", 40), old}}}, "catalog", "v2.0.0", strings.Repeat("b", 40), testCheck)
	if err != nil || len(result.Candidates) != 1 {
		t.Fatalf("valid entryDigest upgrade refused: %v; unavailable=%v", err, result.Unavailable)
	}
}
func TestReviewRejectsDialectDigestUpgrade(t *testing.T) {
	old := fixture(t)
	current := reviewCopyTree(old)
	var rows []Row
	json.Unmarshal(old["manifest.json"], &rows)
	var e Preset
	json.Unmarshal(old[rows[0].Path+"/entry.json"], &e)
	reviewMetadata(t, current, func(e2 map[string]any) { e2["upgrade"] = map[string]string{e.Dialect.Digest: "Wrong digest domain"} })
	result, err := Review(context.Background(), historicalMemory{current, []HistoricalTree{{"v1.0.0", strings.Repeat("a", 40), old}}}, "catalog", "v2.0.0", strings.Repeat("b", 40), testCheck)
	if err == nil && len(result.Candidates) == 1 {
		t.Fatal("accepted dialectDigest as upgrade key instead of prior row.entryDigest")
	}
}
func TestReviewRejectsMissingHistoricalNamedArtifact(t *testing.T) {
	current := executableFixture(t, "named-unbound-entry")
	old := reviewCopyTree(current)
	reviewMetadata(t, old, func(e map[string]any) { delete(e, "artifact") })
	result, err := Review(context.Background(), historicalMemory{current, []HistoricalTree{{"v1.0.0", strings.Repeat("a", 40), old}}}, "catalog", "v2.0.0", strings.Repeat("b", 40), testCheck, readingFixture)
	if err == nil && len(result.Candidates) == 1 {
		t.Fatal("accepted malformed held named-format history with required artifact absent")
	}
}

func TestReviewRejectsConflictInUnavailableCurrentRow(t *testing.T) {
	tree := executableFixture(t, "named-unbound-entry")
	var rows []Row
	json.Unmarshal(tree["manifest.json"], &rows)
	old := rows[0]
	next := old
	next.ID = "example.zz-conflict"
	next.Path = "entries/example/zz-conflict"
	for path, raw := range reviewCopyTree(tree) {
		if strings.HasPrefix(path, old.Path+"/") {
			tree[next.Path+strings.TrimPrefix(path, old.Path)] = append([]byte(nil), raw...)
		}
	}
	dpath := next.Path + "/dialect.json"
	tree[dpath] = []byte(strings.Replace(string(tree[dpath]), "300", "301", 1))
	_, changedDigest, _ := testCheck(context.Background(), tree[dpath])
	next.DialectDigest = Digest(tree[dpath])
	epath := next.Path + "/entry.json"
	var e map[string]any
	json.Unmarshal(tree[epath], &e)
	e["id"] = next.ID
	e["dialect"].(map[string]any)["digest"] = changedDigest
	tree[epath], _ = json.Marshal(e)
	next.EntryDigest = Digest(tree[epath])
	tree["manifest.json"], _ = json.Marshal(append(rows, next))
	replay := func(ctx context.Context, f ReplayInput) (json.RawMessage, error) {
		if strings.Contains(string(f.Dialect), "301") {
			return nil, errors.New("preparation-differs")
		}
		return readingFixture(ctx, f)
	}
	result, err := Review(context.Background(), historicalMemory{tree, nil}, "catalog", "v1.0.0", strings.Repeat("a", 40), testCheck, replay)
	if err == nil && len(result.Candidates) == 1 && len(result.Unavailable) == 1 {
		t.Fatalf("accepted release despite conflicting current named pair; unavailable=%v", result.Unavailable)
	}
}
