package desk

// Making a desk, around the runtime it runs: what a creation that stops
// removes and what it leaves, what other desks can do while one is made, and
// which configuration a desk's runtime reads.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// lockRefusal is a `packs lock` that refuses, as 0.24.0 refuses a
// configuration it cannot read.
const lockRefusal = "  printf '%s\\n' '{\"outputVersion\":\"2\",\"command\":\"packs lock\",\"status\":\"unsupported\",\"diagnostics\":[{\"code\":\"JPS-PROJECT-CONFIG-VERSION\",\"message\":\"The stand-in refuses this configuration.\"}]}'\n  exit 2"

// refusingWhen is a `packs lock` that refuses while marker exists, and locks
// a new desk otherwise.
func refusingWhen(marker string) string {
	return "  if [ -e '" + marker + "' ]; then\n" + lockRefusal + "\n  fi\n" + lockingAs(wantGatedConfig)
}

// pause stops a stand-in's `packs lock` until the test lets it go, so the
// test can act while a creation is running the runtime.
//
// Two FIFOs, each opened read-write here: on Linux that waits for no other
// end, so neither side blocks on an open, and closing them at cleanup ends
// any stand-in still waiting.
type pause struct {
	fragment string
	started  *bufio.Reader
	resume   *os.File
}

func newPause(t *testing.T) *pause {
	t.Helper()
	dir := t.TempDir()
	paused, resume := filepath.Join(dir, "paused"), filepath.Join(dir, "resume")
	var files []*os.File
	for _, path := range []string{paused, resume} {
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Skipf("no FIFO here: %v", err)
		}
		file, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	t.Cleanup(func() {
		for _, file := range files {
			file.Close()
		}
	})
	return &pause{
		fragment: "  printf 'started\\n' > '" + paused + "'\n  IFS= read -r _ < '" + resume + "'\n",
		started:  bufio.NewReader(files[0]),
		resume:   files[1],
	}
}

// reached waits until a stand-in has stopped in its `packs lock`.
func (p *pause) reached(t *testing.T) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := p.started.ReadString('\n')
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the runtime was never run")
	}
}

// release lets one stopped stand-in go on.
func (p *pause) release(t *testing.T) {
	t.Helper()
	if _, err := p.resume.WriteString("go\n"); err != nil {
		t.Fatal(err)
	}
}

// answer is a creation's answer, read off the test's goroutine.
type answer struct {
	status int
	body   []byte
	err    error
}

// createAsync starts a creation and returns where its answer will arrive.
func createAsync(ts *httptest.Server, name string) <-chan answer {
	done := make(chan answer, 1)
	go func() {
		r, _ := http.NewRequest("POST", ts.URL+"/api/desks", strings.NewReader(`{"name":"`+name+`"}`))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("Content-Type", "application/json")
		response, err := ts.Client().Do(r)
		if err != nil {
			done <- answer{err: err}
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		done <- answer{response.StatusCode, body, err}
	}()
	return done
}

func await(t *testing.T, pending <-chan answer) answer {
	t.Helper()
	select {
	case got := <-pending:
		if got.err != nil {
			t.Fatal(got.err)
		}
		return got
	case <-time.After(30 * time.Second):
		t.Fatal("the creation never answered")
	}
	return answer{}
}

// callWithin is a request that must answer within a few seconds: one held up
// behind a creation fails the test rather than hanging it.
func callWithin(t *testing.T, ts *httptest.Server, path, desk string) (int, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", ts.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+testToken)
	if desk != "" {
		r.Header.Set("X-Jpack-Desk", desk)
	}
	response, err := ts.Client().Do(r)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode, nil
}

// snapshotTree is a folder byte for byte: each path's kind, mode and bytes.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		row := info.Mode().String()
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			row += " " + string(data)
		}
		tree[rel] = row
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func sameTree(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Errorf("%s: %d entries before, %d after", what, len(before), len(after))
	}
	for path, row := range before {
		if after[path] != row {
			t.Errorf("%s: %s changed or went missing", what, path)
		}
	}
}

// restartedServer closes s and starts it again on the same folders, with a
// log the test reads.
func restartedServer(t *testing.T, s *Server) (*Server, *bytes.Buffer) {
	t.Helper()
	cfg := s.cfg
	cfg.Root = nil
	cfg.ProjectDir = s.projectDir
	logged := &bytes.Buffer{}
	cfg.Logger = log.New(logged, "", 0)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { again.Close() })
	return again, logged
}

// **The cleanup follows the directory it made, never the name.** While the
// runtime runs, the new folder is renamed aside and an existing desk is moved
// onto its name. The creation then fails: the existing desk must come through
// byte for byte, and what the creation made must be gone from the folder it
// made.
func TestAFailedCreationLeavesAnotherDeskAtItsNameUntouched(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "refuse")
	p := newPause(t)
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), p.fragment+refusingWhen(marker))
	s, ts, _ := gatesServer(t, bin)

	pending := createAsync(ts, "Existing")
	p.reached(t)
	p.release(t)
	made := await(t, pending)
	var existing createdDesk
	if made.status != 201 || json.Unmarshal(made.body, &existing) != nil {
		t.Fatalf("create: %d %s", made.status, made.body)
	}
	before := snapshotTree(t, existing.Folder)

	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	pending = createAsync(ts, "Doomed")
	p.reached(t)
	registry := filepath.Join(s.configDir, "desks")
	var doomed string
	for _, name := range desksLeft(t, s) {
		if name != existing.ID {
			doomed = name
		}
	}
	if doomed == "" {
		t.Fatal("the creation made no folder")
	}
	aside := filepath.Join(t.TempDir(), "aside")
	if err := os.Rename(filepath.Join(registry, doomed), aside); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(existing.Folder, filepath.Join(registry, doomed)); err != nil {
		t.Fatal(err)
	}
	p.release(t)
	got := await(t, pending)
	if got.status != http.StatusInternalServerError || !bytes.Contains(got.body, []byte("was left at")) {
		t.Errorf("the creation answered %d %s, want it refused and saying what it left", got.status, got.body)
	}
	sameTree(t, "the desk moved onto the new folder's name", before, snapshotTree(t, filepath.Join(registry, doomed)))
	if left, err := os.ReadDir(aside); err != nil || len(left) != 0 {
		t.Errorf("what the creation made was not removed from the folder it made: %v %v", left, err)
	}
}

// A creation that fails takes nothing with it: the desks beside it come
// through byte for byte, and still open.
func TestAFailedCreationLeavesItsNeighboursUntouched(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "refuse")
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), refusingWhen(marker))
	s, ts, _ := gatesServer(t, bin)
	a, b := createTestDesk(t, ts, "Alpha"), createTestDesk(t, ts, "Beta")
	if err := os.WriteFile(filepath.Join(a.Folder, "packs", "kept.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeA, beforeB := snapshotTree(t, a.Folder), snapshotTree(t, b.Folder)
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Refused"}`, true)
	if status != http.StatusInternalServerError || bytes.Contains(data, []byte("was left at")) {
		t.Fatalf("the creation answered %d %s, want it refused with nothing left", status, data)
	}
	sameTree(t, "Alpha", beforeA, snapshotTree(t, a.Folder))
	sameTree(t, "Beta", beforeB, snapshotTree(t, b.Folder))
	if left := desksLeft(t, s); len(left) != 2 {
		t.Errorf("the desks folder holds %v, want only the two desks", left)
	}
	for _, row := range []deskRecord{a, b} {
		if status, err := callWithin(t, ts, "/api/files", row.ID); err != nil || status != 200 {
			t.Errorf("%s no longer opens: %d %v", row.Name, status, err)
		}
	}
}

// What cannot be removed safely is left, and said: in the answer, in the log,
// and at the next start, which opens no desk from it.
func TestAnUnfinishedFolderIsLeftAndNamed(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), "  printf 'stray\\n' > stray\n"+lockRefusal)
	s, ts, logged := gatesServer(t, bin)
	status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Stray"}`, true)
	left := desksLeft(t, s)
	if len(left) != 1 {
		t.Fatalf("the desks folder holds %v, want the one folder left", left)
	}
	entry := filepath.Join(s.configDir, "desks", left[0])
	if status != http.StatusInternalServerError || !bytes.Contains(data, []byte("was left at "+entry)) {
		t.Errorf("the creation answered %d %s, want it to say where it left the folder", status, data)
	}
	if !strings.Contains(logged.String(), "could not remove its unfinished folder "+entry) {
		t.Errorf("the log does not name the folder left: %s", logged)
	}
	// Only what the creation did not make is still there.
	if inside, err := os.ReadDir(entry); err != nil || len(inside) != 1 || inside[0].Name() != "stray" {
		t.Errorf("the folder left holds %v (%v), want only what the creation did not make", inside, err)
	}
	again, logged := restartedServer(t, s)
	if len(again.desks) != 0 {
		t.Errorf("a start opened %d desk(s) from an unfinished folder", len(again.desks))
	}
	if !strings.Contains(logged.String(), entry+" is an unfinished folder") {
		t.Errorf("a start did not name the unfinished folder: %s", logged)
	}
}

// Unfinished folders are not desks, so they do not count toward the bound: a
// few of them would otherwise hold every desk shut at the next start.
func TestUnfinishedFoldersDoNotHoldTheRegistryShut(t *testing.T) {
	was := maxDesks
	maxDesks = 1
	t.Cleanup(func() { maxDesks = was })
	s, ts, _ := gatesServer(t, standInRuntime(t))
	row := createTestDesk(t, ts, "Kept")
	for i := range 2 {
		if err := os.Mkdir(filepath.Join(s.configDir, "desks", fmt.Sprintf("%032x", i)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	again, logged := restartedServer(t, s)
	if len(again.desks) != 1 || again.desks[row.ID] == nil {
		t.Errorf("the desk was not resumed beside two unfinished folders: %s", logged)
	}
}

// **The registry is not held while a desk is made.** A creation stopped in
// its runtime holds up neither another desk's requests nor the desk list.
func TestACreationDoesNotHoldUpOtherDesks(t *testing.T) {
	p := newPause(t)
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), p.fragment+lockingAs(wantGatedConfig))
	_, ts, _ := gatesServer(t, bin)
	pending := createAsync(ts, "Open")
	p.reached(t)
	p.release(t)
	var open createdDesk
	if got := await(t, pending); got.status != 201 || json.Unmarshal(got.body, &open) != nil {
		t.Fatalf("create: %d %s", got.status, got.body)
	}

	pending = createAsync(ts, "Slow")
	p.reached(t)
	if status, err := callWithin(t, ts, "/api/files", open.ID); err != nil || status != 200 {
		t.Errorf("another desk's request waited on a creation: %d %v", status, err)
	}
	if status, err := callWithin(t, ts, "/api/desks", ""); err != nil || status != 200 {
		t.Errorf("the desk list waited on a creation: %d %v", status, err)
	}
	p.release(t)
	if got := await(t, pending); got.status != 201 {
		t.Errorf("the slow creation answered %d %s", got.status, got.body)
	}
}

// A desk being made when Desk shuts down is not published, and leaves no
// folder.
func TestADeskMadeDuringShutdownIsNotPublished(t *testing.T) {
	p := newPause(t)
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), p.fragment+lockingAs(wantGatedConfig))
	s, ts, _ := gatesServer(t, bin)
	pending := createAsync(ts, "Late")
	p.reached(t)
	s.closeDesks()
	p.release(t)
	got := await(t, pending)
	if got.status != http.StatusConflict || !bytes.Contains(got.body, []byte("shutting down")) {
		t.Errorf("a creation finished during shutdown answered %d %s", got.status, got.body)
	}
	if left := desksLeft(t, s); len(left) != 0 {
		t.Errorf("a creation finished during shutdown left %v", left)
	}
}

// Desks being made count toward the bound, so creations in flight cannot pass
// it between them.
func TestDesksBeingMadeCountTowardTheBound(t *testing.T) {
	was := maxDesks
	maxDesks = 1
	t.Cleanup(func() { maxDesks = was })
	p := newPause(t)
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), p.fragment+lockingAs(wantGatedConfig))
	_, ts, _ := gatesServer(t, bin)
	first := createAsync(ts, "First")
	p.reached(t)
	second := createAsync(ts, "Second")
	select {
	case got := <-second:
		if got.err != nil || got.status != http.StatusConflict {
			t.Errorf("a creation past the bound answered %d %s %v", got.status, got.body, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Error("a creation past the bound was not refused")
	}
	p.release(t)
	if got := await(t, first); got.status != 201 {
		t.Errorf("the first creation answered %d %s", got.status, got.body)
	}
}

// relayStarted opens a relay and waits until the stand-in runtime behind it
// has logged its start: the n-th `mcp` line in calls, returned.
func relayStarted(t *testing.T, ts *httptest.Server, query, calls string, n int) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws"+query, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{ts.URL}, "Authorization": []string{"Bearer " + testToken}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close(websocket.StatusNormalClosure, "") })
	for ctx.Err() == nil {
		data, _ := os.ReadFile(calls)
		var lines []string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "mcp ") {
				lines = append(lines, line)
			}
		}
		if len(lines) >= n {
			return lines[n-1]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the relay started no runtime")
	return ""
}

// **A desk Desk made reads only its own configuration.** `jpack mcp` reads
// JPACK_CONFIG before ./jpack.json, so a named desk's relay that inherited
// one would evaluate under another project's law. The startup desk keeps it:
// there it is the owner's only way to choose, and the launch says so.
func TestANamedDeskRuntimeReadsOnlyItsOwnConfiguration(t *testing.T) {
	decoy := filepath.Join(t.TempDir(), "jpack.json")
	t.Setenv("JPACK_CONFIG", decoy)
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	_, ts, logged := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	if got := relayStarted(t, ts, "?desk="+row.ID, calls, 1); got != "mcp [JPACK_CONFIG=unset]" {
		t.Errorf("the named desk's runtime started as %q, want it without JPACK_CONFIG", got)
	}
	if got := relayStarted(t, ts, "", calls, 2); got != "mcp [JPACK_CONFIG="+decoy+"]" {
		t.Errorf("the startup desk's runtime started as %q, want the inherited JPACK_CONFIG kept", got)
	}
	if !strings.Contains(logged.String(), "JPACK_CONFIG is set, so this project's runtime reads "+decoy) {
		t.Errorf("the launch did not say the startup desk reads JPACK_CONFIG: %s", logged)
	}
}

// signingKeySaid is what the launch says of an inherited JPACK_SIGNING_KEY.
const signingKeySaid = "desk: JPACK_SIGNING_KEY is set, so where this project's audit trail is chained, its runtime signs each record with the key it names, if it accepts that key; desks Desk made ignore it"

// **A desk Desk made never signs with the startup desk's key** (ADR-0010,
// section 1). The runtime signs every record it writes with the key
// JPACK_SIGNING_KEY names, before the configuration's own, so inherited it
// signed every desk's records with one key. A made desk's runtimes, the
// commands that made it and its relay, start without it and with the rest of
// Desk's environment. The startup desk's relay keeps it, as the owner's, and
// the launch says so once, naming neither its path nor its value.
func TestAnInheritedSigningKeyStaysWithTheStartupDesk(t *testing.T) {
	key := filepath.Join(t.TempDir(), "owner-signing.seed")
	t.Setenv("JPACK_SIGNING_KEY", key)
	t.Setenv("DESK_TEST_INHERITED", "kept")
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	_, ts, logged := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	made := []string{
		"packs schema --format json [JPACK_SIGNING_KEY=unset] [DESK_TEST_INHERITED=kept]",
		"packs lock --config jpack.json --format json [JPACK_SIGNING_KEY=unset] [DESK_TEST_INHERITED=kept]",
	}
	if got := envSeen(t, calls); strings.Join(got, "\n") != strings.Join(made, "\n") {
		t.Errorf("the commands that made a desk ran with %q, want %q", got, made)
	}
	relayStarted(t, ts, "?desk="+row.ID, calls, 1)
	relayStarted(t, ts, "", calls, 2)
	var relays []string
	for _, line := range envSeen(t, calls) {
		if strings.HasPrefix(line, "mcp ") {
			relays = append(relays, line)
		}
	}
	want := []string{"mcp [JPACK_SIGNING_KEY=unset] [DESK_TEST_INHERITED=kept]", "mcp [JPACK_SIGNING_KEY=" + key + "] [DESK_TEST_INHERITED=kept]"}
	if strings.Join(relays, "\n") != strings.Join(want, "\n") {
		t.Errorf("the named desk's and the startup desk's runtimes started as %q, want %q", relays, want)
	}
	said := logged.String()
	if n := strings.Count(said, signingKeySaid); n != 1 {
		t.Errorf("the launch said the inherited JPACK_SIGNING_KEY %d times, want once: %s", n, said)
	}
	for _, secret := range []string{key, filepath.Base(key), filepath.Dir(key)} {
		if strings.Contains(said, secret) {
			t.Errorf("the log names the signing key's path (%s): %s", secret, said)
		}
	}
}

// A JPACK_SIGNING_KEY that is blank names no key to the runtime, and the
// launch says nothing of one.
func TestABlankSigningKeyIsNotSaid(t *testing.T) {
	t.Setenv("JPACK_SIGNING_KEY", "  ")
	_, _, logged := gatesServer(t, standInRuntime(t))
	if strings.Contains(logged.String(), "JPACK_SIGNING_KEY") {
		t.Errorf("the launch spoke of a blank JPACK_SIGNING_KEY: %s", logged)
	}
}
