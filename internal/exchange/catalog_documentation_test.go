package exchange

import (
	"context"
	"strings"
	"testing"
)

func TestCatalogIndexDocumentationIsOutsideEntryInventory(t *testing.T) {
	baseline := review(t, fixture(t))
	for _, raw := range [][]byte{[]byte("# Catalog entries\n目录\n"), []byte(strings.Repeat("x", 64<<10))} {
		tree := fixture(t)
		tree["entries/README.md"] = raw
		got, err := Review(context.Background(), tree, "catalog", "v1.0.0", strings.Repeat("a", 40), testCheck)
		if err != nil || len(got.Candidates) != 1 || len(got.Unavailable) != 0 {
			t.Fatal("catalog index treated as an entry", err, got)
		}
		if got.Candidates[0].Document != baseline.Candidates[0].Document || got.Candidates[0].Digest != baseline.Candidates[0].Digest || len(got.Candidates[0].Files) != 3 {
			t.Fatal("catalog documentation changed checked entry identity or file set")
		}
	}
	empty := memoryTree{"manifest.json": []byte("[]\n"), "entries/README.md": []byte("No entries published yet.\n")}
	got, err := Review(context.Background(), empty, "catalog", "v1.0.0", strings.Repeat("a", 40), testCheck)
	if err != nil || len(got.Candidates) != 0 || len(got.Unavailable) != 0 {
		t.Fatal("empty documented catalog refused", err, got)
	}
}

func TestCatalogDocumentationAllowanceKeepsInventoryClosed(t *testing.T) {
	for _, scenario := range []struct {
		name, path string
		raw        []byte
	}{
		{"arbitrary-root-file", "entries/extra.json", []byte("{}")},
		{"other-root-doc", "entries/guide.md", []byte("not the catalog index")},
		{"publisher-doc", "entries/example/README.md", []byte("not the catalog index")},
		{"nested-doc", "entries/docs/topic/README.md", []byte("not an entry")},
		{"entry-extra-doc", "entries/example/company-research/docs/README.md", []byte("not a declared entry file")},
		{"invalid-index-utf8", "entries/README.md", []byte{0xff}},
		{"oversize-index", "entries/README.md", []byte(strings.Repeat("x", (64<<10)+1))},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tree := fixture(t)
			tree[scenario.path] = scenario.raw
			got, err := Review(context.Background(), tree, "catalog", "v1.0.0", strings.Repeat("a", 40), testCheck)
			if scenario.name == "entry-extra-doc" {
				if err != nil || len(got.Candidates) != 0 || len(got.Unavailable) != 1 {
					t.Fatal("extra entry documentation was not refused", err, got)
				}
			} else if err == nil {
				t.Fatal("catalog documentation bypassed inventory or byte bounds", got)
			}
		})
	}
}
