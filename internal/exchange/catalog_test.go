package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type memoryTree map[string][]byte

func (t memoryTree) Files(context.Context) ([]string, error) {
	out := []string{}
	for p := range t {
		out = append(out, p)
	}
	return out, nil
}
func (t memoryTree) Read(_ context.Context, p string, limit int) ([]byte, error) {
	raw, ok := t[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	if len(raw) > limit {
		return nil, errors.New("too large")
	}
	return raw, nil
}
func fixture(t *testing.T) memoryTree {
	t.Helper()
	out := memoryTree{}
	err := filepath.WalkDir("testdata/preset", func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			name, _ := filepath.Rel("testdata/preset", path)
			out[filepath.ToSlash(name)] = raw
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func testCheck(_ context.Context, raw []byte) (string, string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", "", err
	}
	raw, _ = json.Marshal(value)
	return string(raw), Digest(raw), nil
}
func review(t *testing.T, tree memoryTree) Release {
	t.Helper()
	r, e := Review(context.Background(), tree, "catalog", "v1.0.0", strings.Repeat("a", 40), testCheck)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestPresetBytesFormAndAuthority(t *testing.T) {
	tree := fixture(t)
	r := review(t, tree)
	if len(r.Candidates) != 1 || len(r.Unavailable) != 0 {
		t.Fatal(r.Unavailable)
	}
	c := r.Candidates[0]
	if c.Digest == c.Row.DialectDigest {
		t.Fatal("file and canonical digests conflated")
	}
	if c.Entry.Kind != "preset" || len(c.Entry.Verification) != 0 {
		t.Fatal("false replay claim")
	}
	for _, mutate := range []func(memoryTree){
		func(t memoryTree) { t["entries/example/company-research/execute.sh"] = []byte("echo bad") },
		func(t memoryTree) {
			t["entries/example/company-research/README.md"] = []byte("Bearer " + strings.Repeat("a", 24))
		},
		func(t memoryTree) { t["entries/example/company-research/README.md"] = make([]byte, (64<<10)+1) },
		func(t memoryTree) { t["entries/example/company-research/dialect.json"] = []byte(`{}`) },
	} {
		copy := fixture(t)
		mutate(copy)
		r := review(t, copy)
		if len(r.Candidates) != 0 || len(r.Unavailable) != 1 {
			t.Fatal("bad preset offered", r)
		}
	}
	r, e := Review(context.Background(), tree, "catalog", "v1.0.0", strings.Repeat("a", 40), func(context.Context, []byte) (string, string, error) {
		return "", "", errors.New("Runtime unavailable")
	})
	if e != nil || len(r.Candidates) != 0 || len(r.Unavailable) != 1 {
		t.Fatal("check bypass", e, r)
	}
}
func TestManifestRefusals(t *testing.T) {
	for _, raw := range []string{`null`, `[] {}`, `[{"id":"a.b","id":"a.c"}]`, `[{"id":"a.b","kind":"preset","path":"../elsewhere","entryDigest":"sha256:` + strings.Repeat("a", 64) + `","fixtures":[]}]`, `[{"id":"a.b","kind":"preset","path":"entries/a/b","entryDigest":"sha256:` + strings.Repeat("a", 64) + `","dialectDigest":null,"fixtures":[]}]`} {
		tree := fixture(t)
		tree["manifest.json"] = []byte(raw)
		if _, err := Review(context.Background(), tree, "catalog", "v1.0.0", strings.Repeat("a", 40), testCheck); err == nil {
			t.Fatal("bad manifest", raw)
		}
	}
	tree := fixture(t)
	tree["entries/missing/row/README.md"] = []byte("unlisted")
	if _, err := Review(context.Background(), tree, "catalog", "v1.0.0", strings.Repeat("a", 40), testCheck); err == nil {
		t.Fatal("unlisted entry")
	}
}
func TestReleaseUpdatePinsAndAllFileChanges(t *testing.T) {
	initial := review(t, fixture(t))
	pins := []Pin{{initial.Location, initial.Tag, initial.Commit}}
	lower := initial
	lower.Tag = "v0.99999999999999999999999999999.0"
	if CheckReleasePins(lower, pins) == nil {
		t.Fatal("downgrade")
	}
	moved := initial
	moved.Commit = strings.Repeat("b", 40)
	if CheckReleasePins(moved, pins) == nil {
		t.Fatal("moved tag")
	}
	next := initial
	next.Tag = "v1.1.0"
	next.Commit = strings.Repeat("c", 40)
	if e := CheckReleasePins(next, pins); e != nil {
		t.Fatal(e)
	}
	tree := fixture(t)
	tree["entries/example/company-research/README.md"] = []byte("Changed only the documentation")
	updated := review(t, tree)
	diff := Changes(initial.Candidates[0], updated)
	if strings.Join(diff.Files, ",") != "README.md" {
		t.Fatal("unpinned file change missed", diff)
	}
	empty := Release{Candidates: []Candidate{}}
	if !Changes(initial.Candidates[0], empty).Withdrawn || initial.Candidates[0].Files["dialect.json"] == "" {
		t.Fatal("withdrawal lost retained source")
	}
}
func TestGitReleaseReadsCommitNotWorktree(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(out))
		}
	}
	run("init", "-q")
	for path, raw := range fixture(t) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0700)
		os.WriteFile(filepath.Join(dir, path), raw, 0600)
	}
	run("add", ".")
	run("-c", "user.name=Example", "-c", "user.email=example@example.invalid", "commit", "-qm", "Fixture")
	run("-c", "user.name=Example", "-c", "user.email=example@example.invalid", "tag", "-a", "v1.0.0", "-m", "Release")
	// An annotated tag must be peeled. Working-tree bytes must not replace it.
	os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`[]`), 0600)
	tree, close, e := OpenRelease(context.Background(), dir, "v1.0.0")
	if e != nil {
		t.Fatal(e)
	}
	defer close()
	r, e := Review(context.Background(), tree, dir, "v1.0.0", tree.Commit, testCheck)
	if e != nil || len(r.Candidates) != 1 {
		t.Fatal(e, r.Unavailable)
	}
	if _, _, e = OpenRelease(context.Background(), dir, "main"); e == nil {
		t.Fatal("branch imported")
	}
}
func TestPresetUsesRuntimeOnTwoInstallations(t *testing.T) {
	bin := os.Getenv("JPACK_EXCHANGE_TEST_BIN")
	if bin == "" {
		t.Skip("set JPACK_EXCHANGE_TEST_BIN to the research-capable Runtime")
	}
	for i := 0; i < 2; i++ {
		dir := t.TempDir()
		check := func(ctx context.Context, raw []byte) (string, string, error) {
			cmd := exec.CommandContext(ctx, bin, "dialect", "validate", "-", "--format", "json")
			cmd.Dir = dir
			cmd.Stdin = strings.NewReader(string(raw))
			out, e := cmd.Output()
			if e != nil {
				return "", "", e
			}
			var report struct {
				Status   string `json:"status"`
				Document string `json:"document"`
				Digest   string `json:"digest"`
			}
			if e = json.Unmarshal(out, &report); e != nil {
				return "", "", e
			}
			if report.Status != "document-valid" {
				return "", "", errors.New(report.Status)
			}
			return report.Document, report.Digest, nil
		}
		r, e := Review(context.Background(), fixture(t), "catalog", "v1.0.0", strings.Repeat("a", 40), check)
		if e != nil || len(r.Candidates) != 1 {
			t.Fatal(i, e, r.Unavailable)
		}
	}
}

func TestCurrentEntryVersionIsSeparateFromDocumentVersion(t *testing.T) {
	for _, version := range []any{1, 2, 3, 4, "3", nil} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			tree := fixture(t)
			path := "entries/example/company-research/entry.json"
			var entry map[string]any
			json.Unmarshal(tree[path], &entry)
			entry["entryVersion"] = version
			tree[path], _ = json.Marshal(entry)
			var rows []Row
			json.Unmarshal(tree["manifest.json"], &rows)
			rows[0].EntryDigest = Digest(tree[path])
			tree["manifest.json"], _ = json.Marshal(rows)
			result := review(t, tree)
			if version == 3 {
				if len(result.Candidates) != 1 || result.Candidates[0].Entry.EntryVersion != 3 || result.Candidates[0].Entry.Dialect.Version != 3 {
					t.Fatal(result)
				}
			} else if len(result.Candidates) != 0 || len(result.Unavailable) != 1 || version != nil && !strings.Contains(result.Unavailable[0].Reason, "entry-version-unread") {
				t.Fatal("incompatible metadata interpreted", result)
			}
		})
	}
	for _, change := range []string{"artifact", "named-selector", "both-selectors"} {
		t.Run(change, func(t *testing.T) {
			tree := fixture(t)
			path := "entries/example/company-research/entry.json"
			var entry map[string]any
			json.Unmarshal(tree[path], &entry)
			if change == "artifact" {
				entry["artifact"] = map[string]string{"id": "https://example.invalid/dialect", "version": "1.0.0"}
			} else {
				document := entry["dialect"].(map[string]any)
				document["formatVersion"] = "3.0.0"
				if change == "named-selector" {
					delete(document, "dialectVersion")
				}
			}
			tree[path], _ = json.Marshal(entry)
			var rows []Row
			json.Unmarshal(tree["manifest.json"], &rows)
			rows[0].EntryDigest = Digest(tree[path])
			tree["manifest.json"], _ = json.Marshal(rows)
			if result := review(t, tree); len(result.Candidates) != 0 {
				t.Fatal("named artifact admitted as preset", result)
			}
		})
	}
}
