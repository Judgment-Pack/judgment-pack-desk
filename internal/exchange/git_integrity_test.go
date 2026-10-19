package exchange

import (
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise each edge of the independently verified Git object chain. Only a
// synthetic temporary repository is changed; the working tree is never edited.
func TestGitObjectChainIntegrity(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		for _, corrupt := range []string{"valid", "tag", "commit", "tree", "blob"} {
			t.Run(format+"/"+corrupt, func(t *testing.T) {
				dir := t.TempDir()
				run := func(args ...string) string {
					t.Helper()
					cmd := exec.Command("git", append([]string{"-c", "tag.gpgSign=false", "-c", "commit.gpgSign=false", "-c", "user.name=Review", "-c", "user.email=review@example.invalid", "-C", dir}, args...)...)
					for _, value := range os.Environ() {
						if !strings.HasPrefix(value, "GIT_") {
							cmd.Env = append(cmd.Env, value)
						}
					}
					cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatal(err, string(out))
					}
					return strings.TrimSpace(string(out))
				}
				run("init", "-q", "--object-format="+format)
				for name, raw := range fixture(t) {
					path := filepath.Join(dir, name)
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
				run("add", ".")
				run("commit", "-qm", "Fixture")
				run("tag", "-a", "v1.0.0", "-m", "Fixture release")
				if corrupt != "valid" {
					expr := map[string]string{"tag": "refs/tags/v1.0.0", "commit": "HEAD", "tree": "HEAD^{tree}", "blob": "HEAD:entries/example/company-research/README.md"}[corrupt]
					oid := run("rev-parse", expr)
					path := filepath.Join(dir, ".git", "objects", oid[:2], oid[2:])
					original, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					reader, err := zlib.NewReader(bytes.NewReader(original))
					if err != nil {
						t.Fatal(err)
					}
					raw, err := io.ReadAll(reader)
					reader.Close()
					if err != nil {
						t.Fatal(err)
					}
					raw[len(raw)-1] ^= 1
					var encoded bytes.Buffer
					writer := zlib.NewWriter(&encoded)
					if _, err := writer.Write(raw); err != nil {
						t.Fatal(err)
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(path, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
						t.Fatal(err)
					}
				}
				tree, close, err := OpenRelease(context.Background(), dir, "v1.0.0")
				defer close()
				if err != nil {
					if corrupt == "valid" {
						t.Fatal(err)
					}
					return
				}
				release, err := Review(context.Background(), tree, dir, "v1.0.0", tree.Commit, testCheck)
				if corrupt == "valid" {
					if err != nil || len(release.Candidates) != 1 {
						t.Fatalf("valid release refused: %v, %#v", err, release)
					}
				} else if err == nil && len(release.Candidates) != 0 {
					t.Fatalf("corrupt %s object offered as committed content", corrupt)
				}
			})
		}
	}
}

func TestGitTraversalBounds(t *testing.T) {
	tree := &GitTree{root: "root", trees: map[string][]treeEntry{"root": {{mode: "40000", name: "entries", id: "level0"}}}}
	for depth := 0; depth < 4; depth++ {
		id := "level" + string(rune('0'+depth))
		next := "level" + string(rune('0'+depth+1))
		for n := 0; n < 20; n++ {
			tree.trees[id] = append(tree.trees[id], treeEntry{mode: "40000", name: string(rune('a' + n)), id: next})
		}
	}
	tree.trees["level4"] = []treeEntry{{mode: "100644", name: "file", id: "unused"}}
	if _, err := tree.Files(context.Background()); err == nil {
		t.Fatal("unbounded repeated subtree expansion accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tree.Files(ctx); err == nil {
		t.Fatal("cancelled cached traversal accepted")
	}
	tree.trees["level0"] = []treeEntry{{mode: "40000", name: "empty", id: "empty"}}
	tree.trees["empty"] = []treeEntry{}
	if _, err := tree.Files(context.Background()); err == nil {
		t.Fatal("unlisted empty directory accepted")
	}
}
