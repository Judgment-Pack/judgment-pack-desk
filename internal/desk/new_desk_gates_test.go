package desk

// A new desk starts gated (ADR-0009, section 1): it is written under reviewed
// law with deciding runs recorded, its audit folder is owner-only, and the
// runtime locks it before its manifest is written. These tests hold each part
// with a stand-in runtime, by absolute path, so they run where no runtime is
// installed. The last test drives the real runtime and skips without one.

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// The two configurations a new desk is written with, spelled out here rather
// than read from the code's constants, so that changing either one is a
// change to a test.
const (
	wantGatedConfig   = `{"configVersion":"5","requireReviewed":true,"requireComparableFacts":true,"audit":{"dir":".desk-private/audit"},"packs":{}}` + "\n"
	wantGatedConfigV4 = `{"configVersion":"4","requireReviewed":true,"audit":{"dir":".desk-private/audit"},"packs":{}}` + "\n"
)

// What `packs schema --format json` names as `supportedConfigVersions`,
// measured on 0.25.0, 0.24.0 and 0.23.1.
const (
	allConfigVersions = `["1","2","3","4","5"]`
	upToVersion4      = `["1","2","3","4"]`
	upToVersion3      = `["1","2","3"]`
)

// writeStandInRuntime puts a stand-in for the runtime at path. It answers the
// two commands a new desk is made with, each by a shell fragment run in the
// stand-in's working directory: schema for `packs schema`, and lock for
// `packs lock`. Every argument list it is given is appended to the returned
// file, one line per run. It uses shell builtins only.
func writeStandInRuntime(t *testing.T, path, schema, lock string) (calls string) {
	t.Helper()
	calls = filepath.Join(t.TempDir(), "calls")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> '" + calls + "'\n" +
		"case \"$1 $2\" in\n" +
		"'packs schema')\n" + schema + "\n  ;;\n" +
		"'packs lock')\n" + lock + "\n  ;;\n" +
		"*) exit 64 ;;\n" +
		"esac\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write the stand-in runtime: %v", err)
	}
	return calls
}

// reading is a `packs schema --format json` that names versions as
// `supportedConfigVersions`, as 0.25.0 prints it.
func reading(versions string) string {
	return "  printf '%s\\n' '" + `{"outputVersion":"2","tool":{"name":"jpack","version":"0.0.0-stand-in"},"command":"packs schema","status":"valid","kind":"non-normative-runtime-convention","configVersion":"5","supportedConfigVersions":` + versions + `}` + "'"
}

// lockingAs is a `packs lock` that writes a lock pinning config, and reports
// it as 0.25.0 does.
func lockingAs(config string) string {
	digest := "sha256:" + digestOf([]byte(config))
	return "  printf '%s\\n' '{\"lockVersion\":\"1\",\"config\":{\"digest\":\"" + digest + "\"}}' > jpack.lock.json\n" +
		"  printf '%s\\n' '" + lockAnswer(digest) + "'"
}

func lockAnswer(digest string) string {
	return `{"outputVersion":"2","command":"packs lock","status":"valid","configPath":"jpack.json","lockPath":"jpack.lock.json","lockVersion":"1","configDigest":"` + digest + `","summary":{"total":0,"passed":0,"failed":0},"entries":[],"writtenTo":"jpack.lock.json"}`
}

// standInRuntime is a stand-in that reads every configuration version and
// locks a new desk.
func standInRuntime(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	return bin
}

// gatesServer is one chassis whose runtime is bin, with a log this test reads.
func gatesServer(t *testing.T, bin string) (*Server, *httptest.Server, *bytes.Buffer) {
	t.Helper()
	logged := &bytes.Buffer{}
	s, ts := startDesk(t, Config{ProjectDir: t.TempDir(), JpackBin: bin, Token: testToken, Logger: log.New(logged, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	return s, ts, logged
}

// createdDesk is the creation's answer.
type createdDesk struct {
	deskRecord
	ConfigVersion          string `json:"configVersion"`
	RequireComparableFacts bool   `json:"requireComparableFacts"`
	Notice                 string `json:"notice"`
}

func createGatedDesk(t *testing.T, ts *httptest.Server) createdDesk {
	t.Helper()
	status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Gated"}`, true)
	var row createdDesk
	if status != 201 || json.Unmarshal(data, &row) != nil || row.ID == "" {
		t.Fatalf("create: %d %s", status, data)
	}
	return row
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}
	return string(data)
}

// assertGatedFolder checks a new desk's files: the configuration's exact
// bytes, the lock, the owner-only audit folder, and the ignore file.
func assertGatedFolder(t *testing.T, folder, config string) {
	t.Helper()
	if got := readFile(t, filepath.Join(folder, "jpack.json")); got != config {
		t.Errorf("jpack.json is\n%s\nwant\n%s", got, config)
	}
	if info, err := os.Lstat(filepath.Join(folder, "jpack.lock.json")); err != nil || !info.Mode().IsRegular() {
		t.Errorf("the new desk has no lock: %v", err)
	}
	info, err := os.Lstat(filepath.Join(folder, ".desk-private", "audit"))
	if err != nil || !info.IsDir() {
		t.Fatalf("the new desk has no audit folder: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("the audit folder is %v, want owner-only 0700", perm)
	}
	if got := readFile(t, filepath.Join(folder, ".gitignore")); got != ".desk-private/\n" {
		t.Errorf(".gitignore is %q, want it to ignore .desk-private/", got)
	}
	if got := readFile(t, filepath.Join(folder, "jpack-desk.json")); got != "{\"deskConfigVersion\":1}\n" {
		t.Errorf("jpack-desk.json is %q", got)
	}
}

// desksLeft is what is in the installation's desks folder.
func desksLeft(t *testing.T, s *Server) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(s.configDir, "desks"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// assertNoDesk checks a refused creation: the answer says no desk was
// created, and nothing of one is left, on disk or in the registry.
func assertNoDesk(t *testing.T, s *Server, ts *httptest.Server, status int, data []byte, why string) {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &body)
	if status != http.StatusInternalServerError || !strings.HasPrefix(body.Error, "The desk was not created: ") || !strings.Contains(body.Error, why) {
		t.Errorf("creation answered %d %s, want 500 saying %q", status, data, why)
	}
	if left := desksLeft(t, s); len(left) != 0 {
		t.Errorf("a refused creation left %v in the desks folder", left)
	}
	s.desksMu.Lock()
	open := len(s.desks)
	s.desksMu.Unlock()
	if open != 0 {
		t.Errorf("a refused creation registered %d desk(s)", open)
	}
	_, listing := deskCall(t, ts, "GET", "/api/desks", "", "", true)
	var directory struct {
		Desks []deskRecord `json:"desks"`
	}
	if json.Unmarshal(listing, &directory) != nil || len(directory.Desks) != 1 {
		t.Errorf("the desk list is %s, want only the startup desk", listing)
	}
}

func TestANewDeskStartsUnderReviewedLawWithDecidingRunsRecorded(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(allConfigVersions), lockingAs(wantGatedConfig))
	_, ts, logged := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	assertGatedFolder(t, row.Folder, wantGatedConfig)
	if row.ConfigVersion != "5" || !row.RequireComparableFacts || row.Notice != "" || !row.Managed || row.Name != "Gated" {
		t.Errorf("the creation answered %+v", row)
	}
	// Asked once, then locked once, with the configuration named rather than
	// searched for: a JPACK_CONFIG in Desk's environment must not choose it.
	want := "packs schema --format json\npacks lock --config jpack.json --format json\n"
	if got := readFile(t, calls); got != want {
		t.Errorf("the runtime was run as\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(logged.String(), "requireComparableFacts") {
		t.Errorf("a runtime that reads 5 logged a fallback: %s", logged)
	}
}

func TestANewDeskFallsBackToVersion4WhenTheRuntimeCannotRead5(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(upToVersion4), lockingAs(wantGatedConfigV4))
	_, ts, logged := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	assertGatedFolder(t, row.Folder, wantGatedConfigV4)
	if row.ConfigVersion != "4" || row.RequireComparableFacts {
		t.Errorf("the creation answered %+v, want configVersion 4 without requireComparableFacts", row)
	}
	// It says so, in the answer and in Desk's log.
	for _, said := range []string{row.Notice, logged.String()} {
		if !strings.Contains(said, "configVersion 4, without requireComparableFacts") || !strings.Contains(said, "1, 2, 3, 4") {
			t.Errorf("the fallback was not stated: %q", said)
		}
	}
}

func TestANewDeskNeedsARuntimeThatReadsVersion4(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "jpack")
	calls := writeStandInRuntime(t, bin, reading(upToVersion3), lockingAs(wantGatedConfigV4))
	s, ts, _ := gatesServer(t, bin)
	status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Old runtime"}`, true)
	assertNoDesk(t, s, ts, status, data, "reads configuration versions 1, 2, 3, and a new desk needs 4 or later")
	if got := readFile(t, calls); got != "packs schema --format json\n" {
		t.Errorf("a runtime that cannot hold the desk was asked to lock it: %q", got)
	}
}

func TestAFailedLockLeavesNoDesk(t *testing.T) {
	refusal := "  printf '%s\\n' '{\"outputVersion\":\"2\",\"command\":\"packs lock\",\"status\":\"unsupported\",\"diagnostics\":[{\"code\":\"JPS-PROJECT-CONFIG-VERSION\",\"message\":\"The stand-in refuses this configuration.\"}]}'\n  exit 2"
	for _, tc := range []struct{ name, lock, why string }{
		{"refused", refusal, "the runtime did not lock it: The stand-in refuses this configuration"},
		{"exits without an answer", "  exit 3", "the runtime did not lock it: the runtime's packs lock --config jpack.json --format json failed: exit status 3"},
		{"pins other bytes", lockingAs(wantGatedConfigV4), "the runtime's lock does not pin the configuration this desk wrote"},
		{"writes no lock", "  printf '%s\\n' '" + lockAnswer("sha256:"+digestOf([]byte(wantGatedConfig))) + "'", "the runtime reported a lock, but there is none in the new desk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "jpack")
			writeStandInRuntime(t, bin, reading(allConfigVersions), tc.lock)
			s, ts, _ := gatesServer(t, bin)
			status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Unlocked"}`, true)
			assertNoDesk(t, s, ts, status, data, tc.why)
		})
	}
}

func TestANewDeskNeedsItsRuntime(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-runtime-here")
	s, ts, _ := gatesServer(t, missing)
	status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"No runtime"}`, true)
	assertNoDesk(t, s, ts, status, data, "could not be found")
}

func TestANewDeskRefusesARuntimeThatAnswersTooMuch(t *testing.T) {
	// A schema answer that is valid JSON followed by more than the bound of
	// padding: read whole, it would be accepted.
	bin := filepath.Join(t.TempDir(), "jpack")
	padded := reading(allConfigVersions) + "\n  i=0\n  while [ \"$i\" -lt 1200 ]; do printf '%64s\\n' ''; i=$((i+1)); done"
	writeStandInRuntime(t, bin, padded, lockingAs(wantGatedConfig))
	s, ts, _ := gatesServer(t, bin)
	status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Verbose"}`, true)
	assertNoDesk(t, s, ts, status, data, "was larger than 65536 bytes")
}

func TestANewDeskRefusesARuntimeThatHangs(t *testing.T) {
	was := runtimeCommandTimeout
	runtimeCommandTimeout = time.Second
	t.Cleanup(func() { runtimeCommandTimeout = was })
	bin := filepath.Join(t.TempDir(), "jpack")
	writeStandInRuntime(t, bin, reading(allConfigVersions), "  while :; do :; done")
	s, ts, _ := gatesServer(t, bin)
	// The client gives up long after the bound, so an unbounded command is
	// a failed test rather than a hung suite.
	ts.Client().Timeout = 15 * time.Second
	status, data := deskCall(t, ts, "POST", "/api/desks", "", `{"name":"Hung"}`, true)
	assertNoDesk(t, s, ts, status, data, "the runtime did not finish packs lock --config jpack.json --format json within 1s")
}

// jpackIn runs the real runtime by absolute path in folder, with no
// JPACK_CONFIG, and fails the test if it exits non-zero.
func jpackIn(t *testing.T, bin, folder string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = folder
	cmd.Env = append(os.Environ(), "JPACK_CONFIG=")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("jpack %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// gatedConfigFor is the configuration a creation that answered version
// should have written.
func gatedConfigFor(t *testing.T, version string) string {
	t.Helper()
	want := map[string]string{"5": wantGatedConfig, "4": wantGatedConfigV4}[version]
	if want == "" {
		t.Fatalf("the creation answered configVersion %q", version)
	}
	return want
}

// The real runtime locks the new desk's own configuration, even where Desk
// was started with JPACK_CONFIG naming another, and the lock verifies.
func TestNewDeskWithTheRuntimeLocksItsOwnConfiguration(t *testing.T) {
	bin := requireBinary(t)
	decoy := t.TempDir()
	if err := os.WriteFile(filepath.Join(decoy, "jpack.json"), []byte(`{"configVersion":"3","packs":{}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JPACK_CONFIG", filepath.Join(decoy, "jpack.json"))
	_, ts, _ := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	assertGatedFolder(t, row.Folder, gatedConfigFor(t, row.ConfigVersion))
	if _, err := os.Lstat(filepath.Join(decoy, "jpack.lock.json")); !os.IsNotExist(err) {
		t.Errorf("the lock was written beside JPACK_CONFIG's configuration: %v", err)
	}
	jpackIn(t, bin, row.Folder, "packs", "verify", "--config", "jpack.json", "--format", "json")
}

// The real runtime, end to end, through the new desk's own relay: a deciding
// run of a draft is refused, a reviewed pack is evaluated and recorded once
// locked, and rehearsals answer throughout and record nothing.
func TestNewDeskWithTheRuntimeGatesDecidingRuns(t *testing.T) {
	bin := requireBinary(t)
	// The relay's `jpack mcp` reads JPACK_CONFIG before ./jpack.json.
	t.Setenv("JPACK_CONFIG", "")
	_, ts, _ := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	want := gatedConfigFor(t, row.ConfigVersion)
	assertGatedFolder(t, row.Folder, want)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session := func() *rpcSession {
		t.Helper()
		c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws?desk="+row.ID, &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": []string{ts.URL}, "Authorization": []string{"Bearer " + testToken}},
		})
		if err != nil {
			t.Fatalf("dial the new desk: %v", err)
		}
		t.Cleanup(func() { c.Close(websocket.StatusNormalClosure, "") })
		c.SetReadLimit(readLimit)
		rpc := &rpcSession{t: t, ctx: ctx, ws: c}
		rpc.initialize()
		return rpc
	}
	var next float64 = 10
	evaluate := func(rpc *rpcSession, args map[string]any) (refusal string, structured map[string]any) {
		t.Helper()
		next++
		args["facts"] = "{}"
		rpc.send(map[string]any{"jsonrpc": "2.0", "id": next, "method": "tools/call",
			"params": map[string]any{"name": "experimental_evaluate", "arguments": args}})
		result := rpc.result(next)
		structured, _ = result["structuredContent"].(map[string]any)
		if isErr, _ := result["isError"].(bool); isErr {
			diagnostics, _ := structured["diagnostics"].([]any)
			if len(diagnostics) == 0 {
				t.Fatalf("a refusal named no diagnostic: %v", result)
			}
			code, _ := diagnostics[0].(map[string]any)["code"].(string)
			return code, structured
		}
		if structured["status"] != "evaluated" {
			t.Fatalf("not evaluated: %v", result)
		}
		return "", structured
	}
	records := func() int {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(row.Folder, ".desk-private", "audit", "evaluations.jsonl"))
		if os.IsNotExist(err) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		return bytes.Count(data, []byte("\n"))
	}

	pack := `{"specVersion":"0.2.0-draft","id":"https://example.com/judgment-packs/minimal-literal","version":"0.1.0","title":"Minimal literal decision","decision":{"intent":"Provide the smallest focused valid pack.","question":"Does the literal rule select accept?"},"outcomes":[{"id":"accept","label":"Accept"},{"id":"reject","label":"Reject"}],"rules":[{"id":"literal-rule","description":"A true literal condition.","when":{"op":"literal","value":true},"outcome":"accept","onUnknown":"ignore"}]}`
	rpc := session()
	if code, _ := evaluate(rpc, map[string]any{"pack": pack}); code != "JPS-LOCK-REVIEW-REQUIRED" {
		t.Errorf("a deciding run of a pack passed as text answered %q, want it refused as a draft", code)
	}
	if code, _ := evaluate(rpc, map[string]any{"pack": pack, "rehearsal": true}); code != "" {
		t.Errorf("a rehearsal of a draft was refused: %s", code)
	}

	// A pack added to the project, as Desk's editor adds one, is a draft
	// until the owner locks again.
	if err := os.WriteFile(filepath.Join(row.Folder, "packs", "literal.json"), []byte(pack+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	declared := strings.Replace(want, `"packs":{}`, `"packs":{"literal":{"path":"packs/literal.json"}}`, 1)
	if err := os.WriteFile(filepath.Join(row.Folder, "jpack.json"), []byte(declared), 0o600); err != nil {
		t.Fatal(err)
	}
	rpc = session()
	if code, _ := evaluate(rpc, map[string]any{"pack_id": "literal"}); code != "JPS-LOCK-VERIFY" {
		t.Errorf("a deciding run of an unlocked pack answered %q, want it refused", code)
	}
	if code, _ := evaluate(rpc, map[string]any{"pack_id": "literal", "rehearsal": true}); code != "" {
		t.Errorf("a rehearsal of an unlocked pack was refused: %s", code)
	}
	if n := records(); n != 0 {
		t.Fatalf("%d record(s) before any deciding run was allowed", n)
	}

	jpackIn(t, bin, row.Folder, "packs", "lock", "--config", "jpack.json")
	rpc = session()
	code, structured := evaluate(rpc, map[string]any{"pack_id": "literal"})
	if code != "" || structured["reviewed"] != true {
		t.Errorf("a deciding run of the locked pack answered %q, reviewed=%v", code, structured["reviewed"])
	}
	if n := records(); n != 1 {
		t.Errorf("the deciding run left %d record(s), want 1", n)
	}
	if code, _ := evaluate(rpc, map[string]any{"pack_id": "literal", "rehearsal": true}); code != "" {
		t.Errorf("a rehearsal of the locked pack was refused: %s", code)
	}
	if n := records(); n != 1 {
		t.Errorf("a rehearsal changed the trail to %d record(s)", n)
	}
}
