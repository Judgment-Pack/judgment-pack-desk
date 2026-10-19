package desk

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOptionalFileReadAbsentThenCreated(t *testing.T) {
	_, ts, project := filesServer(t)
	const path = "/api/file?path=jpack-folders.json&optional=true"
	r, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	bearer(r)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent || len(body) != 0 || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("optional absence: status=%d, body=%q, cache=%q", resp.StatusCode, body, resp.Header.Get("Cache-Control"))
	}
	if _, err := os.Stat(filepath.Join(project, "jpack-folders.json")); !os.IsNotExist(err) {
		t.Fatalf("optional read must not create metadata: %v", err)
	}
	for _, suffix := range []string{"", "&optional=false", "&optional=1"} {
		status, body := getJSON(t, ts, "/api/file?path=jpack-folders.json"+suffix)
		if status != http.StatusNotFound || body["code"] != CodeNotFound {
			t.Fatalf("ordinary read %q: status=%d, body=%v", suffix, status, body)
		}
	}
	writeProjectFile(t, project, "jpack-folders.json", "{}")
	status, saved := getJSON(t, ts, path)
	if status != http.StatusOK || saved["content"] != "{}" || saved["sha256"] == "" {
		t.Fatalf("newly created metadata: status=%d, body=%v", status, saved)
	}
	_, strict := getJSON(t, ts, "/api/file?path=jpack-folders.json")
	if strict["sha256"] != saved["sha256"] {
		t.Fatal("optional read changed the digest")
	}
}

func TestOptionalFileReadPreservesRefusals(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "binary.json", "\xff")
	writeProjectFile(t, project, "plain.json", "{}")
	if err := os.Mkdir(filepath.Join(project, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path   string
		status int
		code   string
	}{
		{"", http.StatusBadRequest, CodeBadRequest},
		{"../outside.json", http.StatusForbidden, CodeOutsideRoot},
		{"binary.json", http.StatusUnsupportedMediaType, CodeNotUTF8},
		{"plain.json/child.json", http.StatusNotFound, CodeParentIsAFile},
		{"folder", http.StatusBadRequest, CodeNotAFile},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			status, body := getJSON(t, ts, "/api/file?optional=true&path="+c.path)
			if status != c.status || body["code"] != c.code {
				t.Fatalf("status=%d, body=%v", status, body)
			}
		})
	}
	if status, body := getAnon(t, ts, "/api/file?optional=true&path=missing.json"); status != http.StatusUnauthorized || body["code"] != CodeUnauthorized {
		t.Fatalf("optional read bypassed authentication: status=%d, body=%v", status, body)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink("missing.json", filepath.Join(project, "link.json")); err != nil {
			t.Fatal(err)
		}
		status, body := getJSON(t, ts, "/api/file?optional=true&path=link.json")
		if status != http.StatusForbidden || body["code"] != CodeSymlink {
			t.Fatalf("dangling symlink: status=%d, body=%v", status, body)
		}
	}
}

func TestOptionalFileReadPreservesPermissionError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions and a non-root user")
	}
	_, ts, project := filesServer(t)
	locked := writeProjectFile(t, project, "jpack-folders.json", "{}")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0600) })
	status, body := getJSON(t, ts, "/api/file?optional=true&path=jpack-folders.json")
	if status != http.StatusForbidden || body["code"] != CodeForbidden {
		t.Fatalf("unreadable metadata: status=%d, body=%v", status, body)
	}
}
