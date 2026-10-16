package exchange

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Regression cases identified by independent review of the import boundary.
func TestClosedMetadataRefusals(t *testing.T) {
	mutateEntry := func(tree memoryTree, mutate func(map[string]any)) {
		var entry map[string]any
		json.Unmarshal(tree["entries/example/company-research/entry.json"], &entry)
		mutate(entry)
		raw, _ := json.Marshal(entry)
		tree["entries/example/company-research/entry.json"] = raw
		var rows []Row
		json.Unmarshal(tree["manifest.json"], &rows)
		rows[0].EntryDigest = Digest(raw)
		tree["manifest.json"], _ = json.Marshal(rows)
	}
	cases := map[string]func(memoryTree){
		"manifest case alias": func(tree memoryTree) {
			tree["manifest.json"] = []byte(strings.Replace(string(tree["manifest.json"]), `"kind"`, `"Kind"`, 1))
		},
		"preset forbidden empty outputDigest": func(tree memoryTree) {
			tree["manifest.json"] = []byte(strings.Replace(string(tree["manifest.json"]), `"fixtures": []`, `"fixtures": [], "outputDigest":""`, 1))
		},
		"unsupported row with empty output digest": func(tree memoryTree) {
			var rows []map[string]any
			json.Unmarshal(tree["manifest.json"], &rows)
			rows = append(rows, map[string]any{"id": "example.unsupported", "kind": "dialect", "path": "entries/example/unsupported", "entryDigest": Digest([]byte("entry")), "dialectDigest": Digest([]byte("dialect")), "outputDigest": "", "fixtures": []any{}})
			tree["manifest.json"], _ = json.Marshal(rows)
			tree["entries/example/unsupported/README.md"] = []byte("Unsupported")
		},
		"unverifiable upgrade note": func(tree memoryTree) {
			mutateEntry(tree, func(entry map[string]any) {
				entry["upgrade"] = map[string]string{Digest([]byte("unrelated")): "Changed"}
			})
		},
		"empty description": func(tree memoryTree) {
			mutateEntry(tree, func(entry map[string]any) { entry["description"] = "" })
		},
		"oversized compatibility": func(tree memoryTree) {
			mutateEntry(tree, func(entry map[string]any) {
				entry["compatibility"] = map[string]string{"desk": "v" + strings.Repeat("9", 70) + ".0.0", "runner": "v1.0.0"}
			})
		},
		"unpaired surrogate": func(tree memoryTree) {
			raw := []byte(strings.Replace(string(tree["entries/example/company-research/entry.json"]), `"title": "Example: a company's headcount, from a registry and two research recipes"`, `"title":"\ud800"`, 1))
			tree["entries/example/company-research/entry.json"] = raw
			var rows []Row
			json.Unmarshal(tree["manifest.json"], &rows)
			rows[0].EntryDigest = Digest(raw)
			tree["manifest.json"], _ = json.Marshal(rows)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			tree := fixture(t)
			mutate(tree)
			release, err := Review(context.Background(), tree, "catalog", "v1.0.0", strings.Repeat("a", 40), testCheck)
			if err == nil && len(release.Candidates) != 0 {
				t.Fatalf("malformed metadata offered: candidates=%d unavailable=%v", len(release.Candidates), release.Unavailable)
			}
		})
	}
}

func TestLocalGitCannotExecuteConfiguredHelpers(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(err, string(out))
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("-c", "user.name=Example", "-c", "user.email=example@example.invalid", "commit", "-qm", "Fixture")
	run("tag", "v1.0.0")
	blob := run("rev-parse", "HEAD:manifest.json")
	run("config", "remote.origin.url", "ssh://fixture.invalid/repo")
	run("config", "remote.origin.promisor", "true")
	run("config", "extensions.partialClone", "origin")
	// No network or private data: the synthetic helper only writes this marker.
	run("config", "core.sshCommand", "sh -c 'printf probe > "+marker+"; exit 1'")
	if err := os.Remove(filepath.Join(dir, ".git", "objects", blob[:2], blob[2:])); err != nil {
		t.Fatal(err)
	}
	tree, close, err := OpenRelease(context.Background(), dir, "v1.0.0")
	defer close()
	if err == nil {
		if _, err = tree.Read(context.Background(), "manifest.json", 1<<20); err == nil {
			t.Fatal("missing promised blob was accepted")
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("repository-supplied helper executed: %v", err)
	}
}
