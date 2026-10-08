package desk

// The fourth pass of the ADR-0010 line audit (issues #329 to #334): the
// archive rule's edges, each with the auditor's scenario.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fileWrite is a Project Files save of content at rel, stating base as the
// bytes the editor loaded, on s's route; parents asks for missing folders.
func fileWrite(t *testing.T, s *Server, rel, content, base string, parents bool) *httptest.ResponseRecorder {
	t.Helper()
	s.cfg.Token, s.sessions = "probe", &sessionStore{}
	if s.writes == nil {
		s.writes = &sync.Mutex{}
	}
	body, err := json.Marshal(WriteRequest{Path: rel, Content: content, BaseSHA256: base, CreateParents: parents})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("PUT", "http://localhost/api/file", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer probe")
	request.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleFileWrite(w, request)
	return w
}

// **Project Files writes nothing in Desk's custody** (issue #329, the
// auditor's scenario). A project that holds Desk's configuration folder
// reaches the seed, the next seed, the list of public keys and the archive
// by ordinary paths. A save of each, with the right base and no signing lock
// to be had, is refused with the words the custody gives, and the file is the
// file it was, with its bytes; a save that would make a file there, its
// folders included, makes nothing; and the custody's name in another case is
// refused as well.
func TestProjectFilesWriteNothingInDesksCustody(t *testing.T) {
	base := t.TempDir()
	s, _ := bareServer(t, base, filepath.Join(base, "config"), desk2)
	bareKeys(t, s)
	signing := filepath.Join(s.configDir, "secrets", "signing")
	writeBare(t, filepath.Join(signing, desk2+nextSeedSuffix), secondSeed+"\n")
	dir, err := s.assistant.openSigning(false)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	seed, err := dir.root.Lstat(desk2 + seedSuffix)
	if err != nil {
		t.Fatal(err)
	}
	archivedName, err := dir.archive(desk2+seedSuffix, seed, archived{identity: desk2, rule: archivePromoted, why: "the test's"})
	if err != nil {
		t.Fatal(err)
	}
	writeBare(t, filepath.Join(signing, desk2+seedSuffix), standInSeed+"\n")
	noSigningLock(t)
	for _, name := range []string{desk2 + seedSuffix, desk2 + nextSeedSuffix, desk2 + keysSuffix, filepath.Join(archiveDirName, desk2, archivedName), filepath.Join(archiveDirName, desk2, archiveJournalName)} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(signing, name)
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			old := readFile(t, path)
			rel, _ := filepath.Rel(base, path)
			w := fileWrite(t, s, filepath.ToSlash(rel), "replaced\n", digestOf([]byte(old)), false)
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), signingCustodyWords) {
				t.Errorf("the save answered %d %s", w.Code, w.Body)
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) || readFile(t, path) != old {
				t.Errorf("the file is not the file it was: %v", err)
			}
		})
	}
	for _, rel := range []string{"config/secrets/signing/" + desk2 + ".new.seed", "config/secrets/signing/archive/" + desk2 + "/made/x.seed", "Config/SECRETS/signing/" + desk2 + seedSuffix} {
		w := fileWrite(t, s, rel, "made\n", "", true)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), signingCustodyWords) {
			t.Errorf("a save of %s answered %d %s", rel, w.Code, w.Body)
		}
	}
	if _, err := os.Lstat(filepath.Join(signing, desk2+".new.seed")); !os.IsNotExist(err) {
		t.Errorf("a file was made in the custody: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(signing, archiveDirName, desk2, "made")); !os.IsNotExist(err) {
		t.Errorf("a folder was made in the custody: %v", err)
	}
	// Elsewhere in the project, a save is a save.
	if w := fileWrite(t, s, "notes.md", "notes\n", "", false); w.Code != http.StatusOK {
		t.Errorf("an ordinary save answered %d %s", w.Code, w.Body)
	}
}

// **A project inside Desk's custody is all of it in custody** (issue #329):
// a project opened on the signing folder, or on the configuration folder's
// `secrets/`, saves nothing; one opened on the configuration folder saves
// beside `secrets/` and nothing in it.
func TestAProjectInDesksCustodyIsAllOfItInCustody(t *testing.T) {
	for _, tc := range []struct {
		name, project, rel string
		saved              bool
	}{
		{"the signing folder", "secrets/signing", "notes.md", false},
		{"secrets", "secrets", "notes.md", false},
		{"the configuration folder, beside secrets", ".", "notes.md", true},
		{"the configuration folder, in secrets", ".", "secrets/notes.md", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "config")
			s, _ := bareServer(t, filepath.Join(config, tc.project), config, desk2)
			w := fileWrite(t, s, tc.rel, "notes\n", "", false)
			if (w.Code == http.StatusOK) != tc.saved {
				t.Errorf("the save answered %d %s", w.Code, w.Body)
			}
		})
	}
}

// **The custody is found by identity too** (issue #329): where the path's
// spelling does not name `secrets/` (a case-insensitive volume's other
// case, a bind mount), a folder on the way that is `secrets/` itself is in
// custody.
func TestDesksCustodyIsFoundByIdentity(t *testing.T) {
	base := t.TempDir()
	s, _ := bareServer(t, base, filepath.Join(base, "config"), desk2)
	info, err := os.Lstat(filepath.Join(s.configDir, "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	custody := signingCustody{info: info}
	if !custody.holds(s.root, "config/secrets/signing/"+desk2+seedSuffix) {
		t.Error("a path through secrets/ is not in custody")
	}
	if custody.holds(s.root, "config/other.json") {
		t.Error("a path beside secrets/ is in custody")
	}
}

// **The startup cleanup removes nothing in Desk's custody** (issue #329): a
// file with Project Files' staging name in the signing folder is left, and
// said; one elsewhere in the project is removed, as it was.
func TestTheStartupCleanupRemovesNothingInDesksCustody(t *testing.T) {
	base := t.TempDir()
	s, logs := bareServer(t, base, filepath.Join(base, "config"), desk2)
	inCustody := filepath.Join(s.configDir, "secrets", "signing", stagingPrefix+"0123456789abcdef01234567.tmp")
	elsewhere := filepath.Join(base, stagingPrefix+"0123456789abcdef01234567.tmp")
	writeBare(t, inCustody, standInSeed+"\n")
	writeBare(t, elsewhere, "debris\n")
	s.removeStaleStaging()
	if _, err := os.Lstat(inCustody); err != nil {
		t.Errorf("the cleanup removed a file in the custody: %v", err)
	}
	if _, err := os.Lstat(elsewhere); !os.IsNotExist(err) {
		t.Errorf("the cleanup left its own debris: %v", err)
	}
	if !strings.Contains(logs.String(), "in the folder Desk keeps its signing keys in") {
		t.Errorf("the log does not say what was left: %s", logs)
	}
}
