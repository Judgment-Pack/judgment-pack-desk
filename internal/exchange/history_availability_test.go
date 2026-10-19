package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestFinalUnchangedPairWithUnavailableReplay(t *testing.T) {
	tree := executableFixture(t, "named-unbound-entry")
	var rows []Row
	json.Unmarshal(tree["manifest.json"], &rows)
	old := rows[0]
	next := old
	next.ID = "example.zz-same"
	next.Path = "entries/example/zz-same"
	for path, raw := range reviewCopyTree(tree) {
		if strings.HasPrefix(path, old.Path+"/") {
			tree[next.Path+strings.TrimPrefix(path, old.Path)] = append([]byte(nil), raw...)
		}
	}
	path := next.Path + "/entry.json"
	var e map[string]any
	json.Unmarshal(tree[path], &e)
	e["id"] = next.ID
	tree[path], _ = json.Marshal(e)
	next.EntryDigest = Digest(tree[path])
	tree["manifest.json"], _ = json.Marshal(append(rows, next))
	tree[next.Path+"/fixtures/valid/case.json"] = []byte(`{"review-unavailable":true}`)
	replay := func(ctx context.Context, f ReplayInput) (json.RawMessage, error) {
		if strings.Contains(string(f.Case), "review-unavailable") {
			return nil, errors.New("fixture unavailable")
		}
		return readingFixture(ctx, f)
	}
	result, err := Review(context.Background(), historicalMemory{tree, nil}, "catalog", "v1.0.0", strings.Repeat("a", 40), testCheck, replay)
	if err != nil || len(result.Candidates) != 1 || len(result.Unavailable) != 1 {
		t.Fatalf("availability conflated with identity: %v %#v", err, result.Unavailable)
	}
}
func TestFinalDeclaredPairCannotHideWithIntegerReference(t *testing.T) {
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
	ref := e["dialect"].(map[string]any)
	ref["digest"] = changedDigest
	delete(ref, "formatVersion")
	ref["dialectVersion"] = 3
	tree[epath], _ = json.Marshal(e)
	next.EntryDigest = Digest(tree[epath])
	tree["manifest.json"], _ = json.Marshal(append(rows, next))
	result, err := Review(context.Background(), historicalMemory{tree, nil}, "catalog", "v1.0.0", strings.Repeat("a", 40), testCheck, readingFixture)
	if err == nil && len(result.Candidates) == 1 {
		t.Fatalf("declared artifact conflict hidden by invalid integer reference: %v", result.Unavailable)
	}
}
