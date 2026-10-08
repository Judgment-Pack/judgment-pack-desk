package desk

// The decision record's list of what Desk archived, and the owner's Remove
// (archive.go): the only removal of a key in Desk.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// archivingServer is a bare made desk with its first key, and the key moved
// to the archive as a rotation's promotion would move it, its clock fixed:
// the server, its signing folder held, and the archived file's name.
func archivingServer(t *testing.T) (*Server, *signingDir, string) {
	t.Helper()
	const id = "a2d00000000000000000000000000001"
	s, _ := bareServer(t, filepath.Join(t.TempDir(), "project"), filepath.Join(t.TempDir(), "config"), id)
	s.cfg.Token, s.sessions = "probe", &sessionStore{}
	bareKeys(t, s)
	clock := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	was := archiveClock
	archiveClock = func() time.Time { clock = clock.Add(time.Second); return clock }
	t.Cleanup(func() { archiveClock = was })
	dir, err := s.assistant.openSigning(false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dir.Close)
	seed, err := dir.root.Lstat(id + seedSuffix)
	if err != nil {
		t.Fatal(err)
	}
	file, err := dir.archive(id+seedSuffix, seed, archived{identity: id, trail: fixtureTrail, sequence: 1, rule: archivePromoted, why: "a sentence of the test's"})
	if err != nil {
		t.Fatal(err)
	}
	return s, dir, file
}

// firstArchived is the one entry s's archive lists, which the test requires:
// read through a nil listing, a mutant would end the suite in a panic that
// names no test.
func firstArchived(t *testing.T, s *Server) archivedKey {
	t.Helper()
	listing := s.archiveListing()
	if listing == nil || len(listing.Entries) == 0 {
		t.Fatalf("the archive lists %+v", listing)
	}
	return listing.Entries[0]
}

// removeOn posts the owner's Remove to s's route, and answers its answer.
func removeOn(s *Server, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest("POST", "http://localhost/api/audit/key/archive/remove", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer probe")
	request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	w := httptest.NewRecorder()
	s.handleRemoveArchived(w, request)
	return w
}

// removal is the body of a Remove of entry, with token.
func removal(entry archivedKey, token string) string {
	data, _ := json.Marshal(map[string]string{"scope": entry.Scope, "identity": entry.Identity, "file": entry.File, "token": token})
	return string(data)
}

// **The decision record lists every archived file, and why** (the archive
// rule). A file with its journal line is listed with its identity, the trail
// and sequence its name records, when, the rule and the sentence, as this
// desk's own, with a token; a file the journal holds no line for is listed
// with the sentence that says so; a line whose file is not there is listed
// as missing, with no token.
func TestTheDecisionRecordListsWhatDeskArchived(t *testing.T) {
	s, dir, file := archivingServer(t)
	listing := s.archiveListing()
	if listing == nil || len(listing.Entries) != 1 {
		t.Fatalf("the archive lists %+v", listing)
	}
	entry := listing.Entries[0]
	want := archivedKey{Scope: archiveScopeDesk, Identity: s.cfg.deskID, File: file, Kind: "seed", Trail: fixtureTrail, Sequence: 1, At: "2026-10-08T12:00:01Z", Rule: archivePromoted, Why: "a sentence of the test's", Own: true}
	if entry.Scope != want.Scope || entry.Identity != want.Identity || entry.File != want.File || entry.Kind != want.Kind || entry.Trail != want.Trail ||
		entry.Sequence != want.Sequence || entry.At != want.At || entry.Rule != want.Rule || entry.Why != want.Why || !entry.Own || entry.Missing || len(entry.Token) != 64 {
		t.Errorf("the archive lists %+v, want %+v", entry, want)
	}
	folder := filepath.Join(dir.path, archiveDirName, s.cfg.deskID)
	unexplained := "none-none-20261008T130000.000000000Z.keys.jsonl"
	writeBare(t, filepath.Join(folder, unexplained), "{}\n")
	if err := os.Rename(filepath.Join(folder, file), filepath.Join(t.TempDir(), "elsewhere")); err != nil {
		t.Fatal(err)
	}
	listing = s.archiveListing()
	found := map[string]archivedKey{}
	for _, entry := range listing.Entries {
		found[entry.File] = entry
	}
	if got := found[unexplained]; got.Why != archiveNoLineWords || got.Token == "" {
		t.Errorf("a file with no line is listed %+v", got)
	}
	if got := found[file]; !got.Missing || got.Token != "" || !strings.HasPrefix(got.Why, archiveMissingWords) {
		t.Errorf("a line with no file is listed %+v", got)
	}
}

// **The list is bounded, and says how many more**: past archiveListLimit,
// the newest are listed and More counts the rest.
func TestTheArchivesListIsBounded(t *testing.T) {
	s, dir, _ := archivingServer(t)
	for i := 0; i < archiveListLimit+4; i++ {
		name := s.cfg.deskID + keysSuffix
		writeBare(t, filepath.Join(dir.path, name), "{}\n")
		info, err := dir.root.Lstat(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dir.archive(name, info, archived{identity: s.cfg.deskID, rule: archiveCreationStopped, why: "one of many"}); err != nil {
			t.Fatal(err)
		}
	}
	listing := s.archiveListing()
	if len(listing.Entries) != archiveListLimit || listing.More != 5 {
		t.Errorf("the archive lists %d and %d more", len(listing.Entries), listing.More)
	}
}

// **A journal line Desk could not read back is never written** (bounds on
// both ends): a journal at its bound refuses the line, and the file stays at
// its name; one byte under, it takes it.
func TestTheArchivesJournalIsBoundedWhenWritten(t *testing.T) {
	s, dir, _ := archivingServer(t)
	name := s.cfg.deskID + keysSuffix
	folder := filepath.Join(dir.path, archiveDirName, s.cfg.deskID)
	journal := filepath.Join(folder, archiveJournalName)
	at := time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC)
	archiveClock = func() time.Time { return at }
	next := archiveLine{Version: "1", Event: "archived", File: archiveName("", 0, at, "keys.jsonl"), From: name, Rule: archiveCreationStopped, Why: "bounded", Digest: sha256Digest([]byte("{}\n")), At: at.Format(time.RFC3339Nano)}
	size := int64(len(next.line()))
	for _, tc := range []struct {
		pad   int64
		moved bool
	}{{archiveJournalLimit - size + 1, false}, {archiveJournalLimit - size, true}} {
		if err := os.WriteFile(journal, []byte(strings.Repeat("x", int(tc.pad-1))+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		writeBare(t, filepath.Join(dir.path, name), "{}\n")
		info, err := dir.root.Lstat(name)
		if err != nil {
			t.Fatal(err)
		}
		_, err = dir.archive(name, info, archived{identity: s.cfg.deskID, rule: archiveCreationStopped, why: "bounded"})
		if (err == nil) != tc.moved {
			t.Errorf("with %d bytes in the journal, the move answered %v", tc.pad, err)
		}
		if _, err := os.Lstat(filepath.Join(dir.path, name)); (err == nil) == tc.moved {
			t.Errorf("with %d bytes in the journal, the file at its name: %v", tc.pad, err)
		}
	}
}

// **The owner removes an archived file on their word, and only so.** A token
// for another file, or none, is refused; a file replaced since the list was
// read is refused; the right token removes it, after a journal line that
// says the owner did, and the list no longer names it; the same token again
// finds nothing. A cross-site request, another body, or another media type
// are refused before anything is read.
func TestTheOwnerRemovesAnArchivedFileOnTheirWord(t *testing.T) {
	s, dir, file := archivingServer(t)
	entry := firstArchived(t, s)
	folder := filepath.Join(dir.path, archiveDirName, s.cfg.deskID)
	for name, tc := range map[string]struct {
		body    string
		headers map[string]string
		status  int
	}{
		"another token":    {removal(entry, strings.Repeat("a", 64)), nil, http.StatusConflict},
		"a cross-site one": {removal(entry, entry.Token), map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		"another body":     {`{"scope":"desk","identity":"x","file":"y","token":"z"}`, nil, http.StatusBadRequest},
		"another media":    {removal(entry, entry.Token), map[string]string{"Content-Type": "text/plain"}, http.StatusUnsupportedMediaType},
		"no bearer":        {removal(entry, entry.Token), map[string]string{"Authorization": "Bearer wrong"}, http.StatusUnauthorized},
	} {
		if w := removeOn(s, tc.body, tc.headers); w.Code != tc.status {
			t.Errorf("%s: answered %d %s, want %d", name, w.Code, w.Body, tc.status)
		}
		if _, err := os.Lstat(filepath.Join(folder, file)); err != nil {
			t.Fatalf("%s removed the file: %v", name, err)
		}
	}
	// Replaced since the list was read: another file under the name.
	renamedOver(t, filepath.Join(folder, file), standInSeed+"\n")
	if w := removeOn(s, removal(entry, entry.Token), nil); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "changed after the decision record showed it") {
		t.Fatalf("a replaced file answered %d %s", w.Code, w.Body)
	}
	listing := s.archiveListing()
	if listing == nil || len(listing.Entries) != 1 {
		t.Fatalf("the archive lists %+v", listing)
	}
	entry = listing.Entries[0]
	if w := removeOn(s, removal(entry, entry.Token), nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"state":"removed"`) {
		t.Fatalf("the owner's Remove answered %d %s", w.Code, w.Body)
	}
	if _, err := os.Lstat(filepath.Join(folder, file)); !os.IsNotExist(err) {
		t.Errorf("the file is still there: %v", err)
	}
	if !strings.Contains(readFile(t, filepath.Join(folder, archiveJournalName)), `"event":"removed","file":"`+file+`","rule":"owner"`) {
		t.Error("the journal does not say the owner removed it")
	}
	if listing := s.archiveListing(); listing != nil {
		t.Errorf("the archive still lists %+v", listing.Entries)
	}
	if w := removeOn(s, removal(entry, entry.Token), nil); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "not in Desk's archive now") {
		t.Errorf("the same token again answered %d %s", w.Code, w.Body)
	}
}

// removeProbeWriter notes whether the desk's key lock and the signing folder's
// were free when the answer was written.
type removeProbeWriter struct {
	*httptest.ResponseRecorder
	s          *Server
	dir        *signingDir
	keyFree    bool
	signFree   bool
	wroteFirst bool
}

func (w *removeProbeWriter) Write(data []byte) (int, error) {
	if !w.wroteFirst {
		w.wroteFirst = true
		if w.s.keyMu.TryLock() {
			w.keyFree = true
			w.s.keyMu.Unlock()
		}
		if unlock, err := lockSigning(w.dir); err == nil {
			w.signFree = true
			unlock()
		}
	}
	return w.ResponseRecorder.Write(data)
}

// **The answer is written with the locks let go** (checklist: locks held no
// longer than the change itself): taken or refused, the desk's key lock and
// the signing folder's are free when the answer is written.
func TestTheRemovalsAnswerIsWrittenWithTheLocksLetGo(t *testing.T) {
	for _, taken := range []bool{true, false} {
		s, dir, _ := archivingServer(t)
		entry := firstArchived(t, s)
		token := entry.Token
		if !taken {
			token = strings.Repeat("b", 64)
		}
		request := httptest.NewRequest("POST", "http://localhost/api/audit/key/archive/remove", strings.NewReader(removal(entry, token)))
		request.Header.Set("Authorization", "Bearer probe")
		request.Header.Set("Content-Type", "application/json")
		w := &removeProbeWriter{ResponseRecorder: httptest.NewRecorder(), s: s, dir: dir}
		s.handleRemoveArchived(w, request)
		if !w.wroteFirst || !w.keyFree || !w.signFree {
			t.Errorf("taken %v: answered %d with the key lock free %v and the signing lock free %v", taken, w.Code, w.keyFree, w.signFree)
		}
	}
}

// **A move never goes over a file archived under the same name** (the
// archive rule): two moves of one kind, in one instant of Desk's clock, keep
// both files, the second under the next nanosecond's name.
func TestAnArchiveMoveNeverGoesOverAnother(t *testing.T) {
	s, dir, _ := archivingServer(t)
	at := time.Date(2026, 10, 8, 14, 0, 0, 0, time.UTC)
	archiveClock = func() time.Time { return at }
	name := s.cfg.deskID + keysSuffix
	for _, body := range []string{"first\n", "second\n"} {
		writeBare(t, filepath.Join(dir.path, name), body)
		info, err := dir.root.Lstat(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dir.archive(name, info, archived{identity: s.cfg.deskID, rule: archiveCreationStopped, why: "one instant"}); err != nil {
			t.Fatal(err)
		}
	}
	folder := filepath.Join(dir.path, archiveDirName, s.cfg.deskID)
	held := map[string]bool{}
	for _, file := range namesIn(t, folder) {
		if strings.HasSuffix(file, ".keys.jsonl") {
			held[readFile(t, filepath.Join(folder, file))] = true
		}
	}
	if !held["first\n"] || !held["second\n"] {
		t.Errorf("the archive holds %v, want both files", held)
	}
}

// **An archive line names no path** (checklist 5): a creation whose runtime
// wrote part of a seed and failed, in words that name the seed under a
// configuration folder whose name holds a space, a tab and U+2028, archives
// what it made with a line that names none of it.
func TestAnArchiveLineNamesNoPath(t *testing.T) {
	base := filepath.Join(t.TempDir(), "Home DIR (owner)")
	config := filepath.Join(base, "Owner SECRET\tKEYS TAIL dir")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Skip("this file system refuses the names")
	}
	const id = "a2d00000000000000000000000000003"
	s, _ := bareServer(t, filepath.Join(t.TempDir(), "project"), config, id)
	writeBare(t, s.cfg.JpackBin, "#!/bin/sh\nprintf 'part of a seed' > \"$4\"\nprintf '{\"command\":\"audit key generate\",\"status\":\"error\",\"diagnostics\":[{\"code\":\"JPS-AUDIT-KEY-WRITE\",\"message\":\"The seed at %s could not be written whole.\"}]}\\n' \"$4\"\nexit 4\n")
	if err := os.Chmod(s.cfg.JpackBin, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := s.assistant.openSigning(false)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	project, why := s.auditRuntime()
	if why != "" {
		t.Fatal(why)
	}
	if _, err := generateKeyMarked(context.Background(), s.cfg.JpackBin, project, dir, id, nil); err == nil {
		t.Fatal("the creation was not refused")
	}
	if got := archivedIn(t, dir.path, id); len(got) == 0 {
		t.Fatal("nothing was archived")
	}
	journal := readFile(t, filepath.Join(dir.path, archiveDirName, id, archiveJournalName))
	for _, part := range []string{"SECRET", "KEYS", "TAIL", "DIR", "(owner)", " "} {
		if strings.Contains(journal, part) {
			t.Errorf("the archive's journal names %q: %s", part, journal)
		}
	}
}

// **What Desk archived comes with every answer of the decision record**, a
// report, a project that keeps no trail and a refusal among them.
func TestTheArchiveComesWithEveryAnswer(t *testing.T) {
	r := newRotationRig(t, "a2d00000000000000000000000000004", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	if status, data := r.rotate(t, r.token(t)); status != http.StatusOK {
		t.Fatalf("the rotation answered %d %s", status, data)
	}
	listed := func(t *testing.T, what string, data []byte) {
		t.Helper()
		var body struct {
			Archive *auditArchive `json:"archive"`
		}
		if json.Unmarshal(data, &body) != nil || body.Archive == nil || len(body.Archive.Entries) != 1 || body.Archive.Entries[0].Kind != "seed" ||
			body.Archive.Entries[0].Rule != archivePromoted || len(body.Archive.Entries[0].Token) != 64 {
			t.Errorf("%s lists %s", what, data)
		}
	}
	_, data := r.panel(t)
	listed(t, "a report", []byte(data))
	writeProject(t, r.desk, map[string]string{"jpack.json": `{"configVersion":"6","packs":{}}` + "\n"})
	status, body := reviewCall(t, r.ts, "GET", "/api/audit/verify", r.id, nil, bearer)
	if status != http.StatusOK || !strings.Contains(string(body), `"state":"no-trail"`) {
		t.Fatalf("a project with no trail answered %d %s", status, body)
	}
	listed(t, "a project with no trail", body)
	writeProject(t, r.desk, map[string]string{"jpack.json": "not json\n"})
	status, body = reviewCall(t, r.ts, "GET", "/api/audit/verify", r.id, nil, bearer)
	if status < 400 {
		t.Fatalf("an unreadable configuration answered %d %s", status, body)
	}
	listed(t, "a refusal", body)
}

// **A manifest that could not be read now is no licence for a copy**
// (the archive rule; the nightly's survivors of #326). A made desk's copy,
// opened directly while the desk is in Desk's desks folder, takes custody
// only where that desk's manifest is shown not to be a desk's: a read that
// fails for a moment, an I/O error that says nothing of the file, leaves the
// copy's identity unknown, and the copy rotates nothing.
func TestAManifestNotReadNowIsNoLicenceForACopy(t *testing.T) {
	base := t.TempDir()
	config := filepath.Join(base, "config")
	const id = "a2d00000000000000000000000000005"
	original := filepath.Join(config, "desks", id)
	s, _ := bareServer(t, original, config, id)
	bareKeys(t, s)
	writeBare(t, filepath.Join(original, deskManifest), `{"id":"`+id+`","name":"live"}`)
	copyPath := filepath.Join(base, "copy")
	copyPrivateTree(t, original, copyPath)
	b, _ := bareServer(t, copyPath, config, id)
	failed := 0
	testHookPrivateRead = func(name string) error {
		if name == deskManifest && failed == 0 {
			failed++
			return syscall.EIO
		}
		return nil
	}
	t.Cleanup(func() { testHookPrivateRead = nil })
	shared, why := b.identityShared()
	testHookPrivateRead = nil
	if failed != 1 {
		t.Fatal("no read of the manifest was failed")
	}
	if !shared || why != deskUnknownWords {
		t.Errorf("a copy whose original's manifest could not be read now is taken as the desk: %v %q", shared, why)
	}
}

// **An archived file written again in place is not removed by an older
// token** (review round 1 of #327, finding 1, the reviewer's scenario). The
// token binds the file's bytes by their digest, so a seed rewritten in place,
// keeping its inode, is answered as changed and stays; and its bytes are
// checked again, under the signing folder's lock, immediately before the
// removal: rewritten in that moment, it stays too. A fresh token removes it.
func TestAnArchivedFileWrittenAgainIsNotRemovedByAnOlderToken(t *testing.T) {
	s, dir, file := archivingServer(t)
	path := filepath.Join(dir.path, archiveDirName, s.cfg.deskID, file)
	entry := firstArchived(t, s)
	writeBare(t, path, secondSeed+"\n")
	if now := firstArchived(t, s); now.Token == entry.Token {
		t.Error("a seed written again in place keeps its token")
	}
	if w := removeOn(s, removal(entry, entry.Token), nil); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "changed after the decision record showed it") {
		t.Errorf("an older token answered %d %s", w.Code, w.Body)
	}
	if got := readFile(t, path); got != secondSeed+"\n" {
		t.Fatalf("the seed written again is %q", got)
	}
	entry = firstArchived(t, s)
	testHookKeyBetween = func(at string) {
		if at == "archive: removal confirmed" {
			writeBare(t, path, thirdSeed+"\n")
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	if w := removeOn(s, removal(entry, entry.Token), nil); w.Code != http.StatusConflict {
		t.Errorf("a seed written again after its token was checked answered %d %s", w.Code, w.Body)
	}
	testHookKeyBetween = nil
	if got := readFile(t, path); got != thirdSeed+"\n" {
		t.Fatalf("the seed written again is %q", got)
	}
	entry = firstArchived(t, s)
	if w := removeOn(s, removal(entry, entry.Token), nil); w.Code != http.StatusOK {
		t.Errorf("a fresh token answered %d %s", w.Code, w.Body)
	}
}

// **A Remove's token is spent once it is used** (review round 1 of #327,
// finding 2, the reviewer's scenario). The owner removes an archived seed
// while a second name of the same file is kept elsewhere; that file is put
// back at its archived name, the same inode with the same bytes; the token
// used once is refused, and the file stays. The decision record lists it
// again, saying the journal says it was removed, with a new token, which
// removes it on a new confirmation.
func TestARemovalsTokenIsSpentOnceUsed(t *testing.T) {
	s, dir, file := archivingServer(t)
	path := filepath.Join(dir.path, archiveDirName, s.cfg.deskID, file)
	saved := filepath.Join(t.TempDir(), "saved.seed")
	if err := os.Link(path, saved); err != nil {
		t.Fatal(err)
	}
	entry := firstArchived(t, s)
	if w := removeOn(s, removal(entry, entry.Token), nil); w.Code != http.StatusOK {
		t.Fatalf("the owner's Remove answered %d %s", w.Code, w.Body)
	}
	if err := os.Link(saved, path); err != nil {
		t.Fatal(err)
	}
	if w := removeOn(s, removal(entry, entry.Token), nil); w.Code != http.StatusConflict {
		t.Errorf("the spent token answered %d %s", w.Code, w.Body)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("the file put back was removed by a spent token: %v", err)
	}
	back := firstArchived(t, s)
	if back.Token == entry.Token || !strings.HasPrefix(back.Why, archiveBackWords) {
		t.Errorf("the file put back is listed %+v", back)
	}
	if !strings.Contains(readFile(t, filepath.Join(dir.path, archiveDirName, s.cfg.deskID, archiveJournalName)), `"event":"removed","file":"`+file+`","rule":"owner","generation":1,`) {
		t.Error("the journal does not say which generation the removal spent")
	}
	if w := removeOn(s, removal(back, back.Token), nil); w.Code != http.StatusOK {
		t.Errorf("a new confirmation answered %d %s", w.Code, w.Body)
	}
}

// **An archive move that fails leaves the file at its name** (review round 1
// of #327): a rename refused across devices (EXDEV), an archive folder that
// cannot be made, and an archive whose next 65 names in one instant are all
// taken each answer an error, and the seed stays at its name with its bytes.
func TestAnArchiveMoveThatFailsLeavesTheFile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, dir *signingDir, id string)
	}{
		{"a rename across devices", func(t *testing.T, dir *signingDir, id string) {
			was := archiveRename
			archiveRename = func(*os.Root, string, string) error { return &os.LinkError{Op: "rename", Err: syscall.EXDEV} }
			t.Cleanup(func() { archiveRename = was })
		}},
		{"an archive folder that cannot be made", func(t *testing.T, dir *signingDir, id string) {
			writeBare(t, filepath.Join(dir.path, archiveDirName), "not a folder\n")
		}},
		{"every free name taken", func(t *testing.T, dir *signingDir, id string) {
			at := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
			archiveClock = func() time.Time { return at }
			for attempt := 0; attempt <= 64; attempt++ {
				writeBare(t, filepath.Join(dir.path, archiveDirName, id, archiveName("", 0, at.Add(time.Duration(attempt)), "seed")), "taken\n")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "a2d00000000000000000000000000007"
			s, _ := bareServer(t, filepath.Join(t.TempDir(), "project"), filepath.Join(t.TempDir(), "config"), id)
			bareKeys(t, s)
			was := archiveClock
			t.Cleanup(func() { archiveClock = was })
			dir, err := s.assistant.openSigning(false)
			if err != nil {
				t.Fatal(err)
			}
			defer dir.Close()
			tc.prepare(t, dir, id)
			info, err := dir.root.Lstat(id + seedSuffix)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := dir.archive(id+seedSuffix, info, archived{identity: id, rule: archivePromoted, why: "a test's"}); err == nil {
				t.Fatal("the move was answered as made")
			}
			if got := readFile(t, filepath.Join(dir.path, id+seedSuffix)); got != standInSeed+"\n" {
				t.Errorf("the seed at its name is %q", got)
			}
		})
	}
}

// **A file written while it is archived is said** (review round 1 of #327):
// the move compares the file's bytes, by their digest, before and after, not
// its inode alone. The seed is written again, in place, between the journal
// line and the rename: the log says its bytes changed while it was moved,
// and the decision record that they are not the ones Desk archived.
func TestAFileWrittenWhileItIsArchivedIsSaid(t *testing.T) {
	const id = "a2d00000000000000000000000000008"
	s, logs := bareServer(t, filepath.Join(t.TempDir(), "project"), filepath.Join(t.TempDir(), "config"), id)
	bareKeys(t, s)
	dir, err := s.assistant.openSigning(false)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	info, err := dir.root.Lstat(id + seedSuffix)
	if err != nil {
		t.Fatal(err)
	}
	testHookKeyBetween = func(at string) {
		if at == "archive: line written" {
			writeBare(t, filepath.Join(dir.path, id+seedSuffix), secondSeed+"\n")
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	if _, err := dir.archive(id+seedSuffix, info, archived{identity: id, rule: archivePromoted, why: "a test's"}); err != nil {
		t.Fatal(err)
	}
	testHookKeyBetween = nil
	if !strings.Contains(logs.String(), "its bytes changed while it was moved") {
		t.Errorf("the log does not say the bytes changed: %s", logs)
	}
	if entry := firstArchived(t, s); !strings.HasSuffix(entry.Why, archiveChangedWords) {
		t.Errorf("the decision record lists %+v", entry)
	}
}
