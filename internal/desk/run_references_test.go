package desk

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func referenceFixture(t *testing.T) (*Server, string, []byte, []byte) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	s := &Server{root: root, projectDir: dir, jobs: &jobsCompanion{workspace: "one-desk", profiles: json.RawMessage(`[{"id":"original-public-profile"}]`)}}
	id := "release_" + strings.Repeat("a", 32)
	release := []byte(`{"id":"` + id + `","pack":"pack bytes","number":9007199254740993,"title":"A < B"}`)
	request := []byte(`{"pack":"pack bytes"}`)
	return s, id, release, request
}

func TestRunReferencesAreIndependentHistoricalAndPrivate(t *testing.T) {
	s, id, release, request := referenceFixture(t)
	if err := s.retainRunReferences(release, request); err != nil {
		t.Fatal(err)
	}
	saved, err := s.readRunReferences(id)
	if err != nil {
		t.Fatal(err)
	}
	canonical := []byte(`{"id":"` + id + `","number":9007199254740993,"pack":"pack bytes","title":"A \u003c B"}`)
	if saved.ReleaseDigest != sha256Digest(canonical) {
		t.Fatalf("rounded or incompatible canonicalization: %s", saved.ReleaseDigest)
	}
	if string(saved.Profiles) != `[{"id":"original-public-profile"}]` {
		t.Fatal("profiles were not taken from installation")
	}
	s.jobs.profiles = json.RawMessage(`[{"id":"replacement-profile"}]`)
	if err := s.retainRunReferences(release, request); err != nil {
		t.Fatal(err)
	}
	again, err := s.readRunReferences(id)
	if err != nil || !bytes.Equal(again.Profiles, saved.Profiles) || again.RecordedAt != saved.RecordedAt {
		t.Fatal("historical trust changed")
	}
	changed := bytes.Replace(release, []byte(`"A < B"`), []byte(`"changed"`), 1)
	if err := s.retainRunReferences(changed, request); err == nil {
		t.Fatal("same identity overwrote trusted release")
	}
	file := filepath.Join(s.projectDir, runReferencesDir, id+".json")
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private reference file: %v %v", info, err)
	}
	// An actual reopen reads the historical snapshot, not current profiles.
	reopened, err := os.OpenRoot(s.projectDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	other := &Server{root: reopened, jobs: &jobsCompanion{workspace: "one-desk"}}
	held, err := other.readRunReferences(id)
	if err != nil || held.ReleaseDigest != saved.ReleaseDigest || !bytes.Equal(held.Profiles, saved.Profiles) {
		t.Fatal("reopen lost references")
	}
	other.jobs.workspace = "another-desk"
	if _, err = other.readRunReferences(id); err == nil {
		t.Fatal("another desk used these references")
	}
}

func TestRunReferencesDoNotFollowLinksOrCreateMissingHistory(t *testing.T) {
	s, id, release, request := referenceFixture(t)
	if _, err := s.readRunReferences(id); !os.IsNotExist(err) {
		t.Fatalf("missing history: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.projectDir, ".desk-private")); !os.IsNotExist(err) {
		t.Fatal("read created a private folder")
	}
	if err := s.retainRunReferences(release, request); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(s.projectDir, runReferencesDir, id+".json")
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "reference.json")
	if err = os.WriteFile(outside, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, name); err != nil {
		t.Fatal(err)
	}
	if _, err = s.readRunReferences(id); err == nil {
		t.Fatal("followed a reference symlink")
	}
	if err = s.retainRunReferences(release, request); err == nil {
		t.Fatal("replaced an unsafe reference")
	}
	after, _ := os.ReadFile(outside)
	if !bytes.Equal(after, raw) {
		t.Fatal("changed outside file")
	}
}

func TestRunReferencesRequireTheCheckedRelease(t *testing.T) {
	s, _, release, request := referenceFixture(t)
	for _, invalid := range [][]byte{
		bytes.Replace(release, []byte(`"pack bytes"`), []byte(`"another pack"`), 1),
		bytes.Replace(release, []byte(`"id":`), []byte(`"id":"release_forged","id":`), 1),
		bytes.Replace(release, []byte(`"id":`), []byte(`"ID":`), 1),
	} {
		if err := s.retainRunReferences(invalid, request); err == nil {
			t.Fatal("accepted a different or ambiguous release")
		}
	}
	if _, err := os.Stat(filepath.Join(s.projectDir, ".desk-private")); !os.IsNotExist(err) {
		t.Fatal("invalid release created trust")
	}
}
