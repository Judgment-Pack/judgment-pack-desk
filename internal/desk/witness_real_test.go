//go:build linux

package desk

// A witness as a holder, with the published runtime and the gateway's own
// witness (ADR-0012; issue #234). The witness is the gateway's reference,
// `gateway serve --config <engine.json>` with a `witness` member, run on a
// loopback port with a test issuer's key set, its marks on another device
// than its log, as the gateway's engine requires; the trail is registered by
// its operator, `gateway witness register`, before the witness starts. The
// submitter's token is minted here with the issuer's key, naming the
// witness's audience. Both binaries are named by absolute path:
// JPACK_BIN for the runtime and JPACK_GATEWAY_TEST_BIN for the gateway; the
// tests skip without them.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	witnessTestIssuer   = "https://issuer.example"
	witnessTestSubject  = "desk-deliverer"
	witnessTestAudience = "witness:desk-test"
	witnessTestEngine   = "gateway:signer"
)

// testWitnessGateway is a gateway witness run from the published binary: its
// configuration, its address, the issuer whose tokens it takes, and its
// public key.
type testWitnessGateway struct {
	bin, config, url, public string
	issuer                   ed25519.PrivateKey
	cmd                      *exec.Cmd
	stderr                   *lockedWriter
}

// newTestWitnessGateway writes the witness's engine configuration and its
// files, skipping where the binary or a device apart for its marks is not
// there. It does not start it.
func newTestWitnessGateway(t *testing.T) *testWitnessGateway {
	t.Helper()
	bin := os.Getenv("JPACK_GATEWAY_TEST_BIN")
	if bin == "" || !filepath.IsAbs(bin) {
		t.Skip("set JPACK_GATEWAY_TEST_BIN to the published gateway, by absolute path")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("no gateway at JPACK_GATEWAY_TEST_BIN: %v", err)
	}
	base := t.TempDir()
	marks, err := os.MkdirTemp("/dev/shm", "witness-marks-")
	if err != nil {
		t.Skipf("no folder for the witness's marks on another device: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(marks) })
	for _, dir := range []string{"log", "store", "decisions", "catalog"} {
		if err := os.Mkdir(filepath.Join(base, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if deviceOf(t, filepath.Join(base, "log")) == deviceOf(t, marks) {
		t.Skip("the witness's marks need a device apart from its log's, and /dev/shm is not one here")
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "gateway.seed"), []byte(hex.EncodeToString(seed)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	public, issuer, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys, _ := json.Marshal(map[string]any{"keys": []any{map[string]any{"kty": "OKP", "kid": "ed-1", "crv": "Ed25519", "x": base64.RawURLEncoding.EncodeToString(public)}}})
	if err := os.WriteFile(filepath.Join(base, "keys.json"), keys, 0o600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback port: %v", err)
	}
	address := listener.Addr().String()
	listener.Close()
	config := map[string]any{
		"engineVersion": "6", "authority": "gateway:witness", "seed": filepath.Join(base, "gateway.seed"),
		"store": filepath.Join(base, "store", "receipts"), "registry": filepath.Join(base, "registry.jsonl"),
		"decisionRecords": filepath.Join(base, "decisions"), "listen": address, "catalog": filepath.Join(base, "catalog"),
		"identity":  map[string]any{"issuer": witnessTestIssuer, "audience": witnessTestEngine, "keys": filepath.Join(base, "keys.json")},
		"witness":   map[string]any{"log": filepath.Join(base, "log", "witness.log"), "marks": filepath.Join(marks, "witness.marks"), "audience": witnessTestAudience, "registration": "operator"},
		"platforms": map[string]any{},
	}
	data, _ := json.Marshal(config)
	path := filepath.Join(base, "engine.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return &testWitnessGateway{bin: bin, config: path, url: "http://" + address, issuer: issuer,
		public: hex.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))}
}

// deviceOf is the device a folder is on.
func deviceOf(t *testing.T, dir string) uint64 {
	t.Helper()
	var st syscall.Stat_t
	if err := syscall.Stat(dir, &st); err != nil {
		t.Fatal(err)
	}
	return uint64(st.Dev)
}

// register registers trail to the test's submitter, by the operator's own
// act on the stopped witness.
func (g *testWitnessGateway) register(t *testing.T, trail string) {
	t.Helper()
	out, err := exec.Command(g.bin, "witness", "register", "--config", g.config, "--trail", trail, "--issuer", witnessTestIssuer, "--subject", witnessTestSubject).CombinedOutput()
	if err != nil {
		t.Fatalf("gateway witness register: %v: %s", err, out)
	}
}

// start starts the witness and waits until it answers.
func (g *testWitnessGateway) start(t *testing.T) {
	t.Helper()
	g.stderr = &lockedWriter{w: &bytes.Buffer{}}
	g.cmd = exec.Command(g.bin, "serve", "--config", g.config)
	g.cmd.Stdout, g.cmd.Stderr = g.stderr, g.stderr
	if err := g.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cmd := g.cmd
	t.Cleanup(func() { g.stopIf(cmd) })
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if response, err := http.Get(g.url + "/publickey"); err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	g.stop(t)
	t.Fatalf("the witness did not start: %s", g.stderr.String())
}

// stop stops the witness, and waits for it.
func (g *testWitnessGateway) stop(t *testing.T) {
	t.Helper()
	g.stopIf(g.cmd)
}

func (g *testWitnessGateway) stopIf(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return
	}
	cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		<-done
	}
}

// token is a token the test issuer signs, for subject, naming audience.
func (g *testWitnessGateway) token(subject, audience string) string {
	b64 := base64.RawURLEncoding.EncodeToString
	header, _ := json.Marshal(map[string]any{"alg": "EdDSA", "kid": "ed-1", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{"iss": witnessTestIssuer, "sub": subject, "aud": audience, "exp": time.Now().Add(time.Hour).Unix()})
	signed := b64(header) + "." + b64(claims)
	return signed + "." + b64(ed25519.Sign(g.issuer, []byte(signed)))
}

// post POSTs body to the witness under token, and answers its status and body.
func (g *testWitnessGateway) post(t *testing.T, path, token, contentType string, body []byte) (int, []byte) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, g.url+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return response.StatusCode, data
}

// capturingRuntime is a runtime at a path of its own that runs the published
// one, and keeps what each reading with a witness's head printed, exactly,
// in capture: so that what the page is given can be held to it.
func capturingRuntime(t *testing.T, bin string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "reading.json")
	script := "#!/bin/sh\n" +
		"case \" $* \" in\n" +
		"  *\" audit verify \"*\"--witness-head\"*) '" + bin + "' \"$@\" > '" + capture + ".tmp'; code=$?; cat '" + capture + ".tmp'; mv '" + capture + ".tmp' '" + capture + "'; exit $code;;\n" +
		"esac\nexec '" + bin + "' \"$@\"\n"
	path := filepath.Join(dir, "jpack")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, capture
}

// **With the published runtime and the gateway's own witness, end to end.**
// A desk Desk made, with two deciding runs; its trail registered at a witness
// the gateway's reference runs on loopback. The owner sets the witness: its
// key is held to the runtime's rule and kept; the submitter's token is kept
// in Desk's custody, 0600, and is in no answer, no log and no project file.
// Send now: the witness countersigns the head, and the cursor moves to it.
// The decision record fetches the head and the statements, and its
// countersigned coverage and witness section are the runtime's own report,
// byte for byte, a current reading. A third run is countersigned on the
// interval. A refused token, a 409 conflict, a stopped witness and a retired
// trail are each shown in the witness's words, or as "Desk could not reach
// the witness", with the cursor unmoved; with the witness stopped, Desk
// keeps working, and the decision record reads the chain as far as it was
// supplied, historically.
func TestWitnessWithThePublishedRuntimeAndGateway(t *testing.T) {
	real := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	gateway := newTestWitnessGateway(t)
	const id = "a00000000000000000000000000000b4"
	fixDeskIDs(t, id)
	w := fixWitness(t, id)
	bin, capture := capturingRuntime(t, real)
	logged := &lockedWriter{w: &bytes.Buffer{}}
	config := t.TempDir()
	s, ts := startDesk(t, Config{ProjectDir: t.TempDir(), JpackBin: bin, Token: testToken, DeskConfigDir: config, Logger: log.New(logged, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	row := createGatedDesk(t, ts)
	if row.ID != id {
		t.Fatalf("the desk was made as %s", row.ID)
	}
	if !slices.Contains(mustSchema(t, real, row.Folder), "6") {
		t.Skip("this runtime has no audit commands")
	}
	deskConfig := strings.Replace(gatedConfigFor(t, s, row), `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": reviewPack, "jpack.json": deskConfig})
	jpackIn(t, real, row.Folder, "packs", "lock", "--config", "jpack.json", "--format", "json")
	decide := func() {
		t.Helper()
		if code, out := decidingRun(t, real, row.Folder); code != 0 {
			t.Fatalf("a deciding run exited %d: %s", code, out)
		}
	}
	decide()
	decide()
	var head struct {
		Checkpoint struct {
			Trail string `json:"trail"`
		} `json:"checkpoint"`
	}
	if err := json.Unmarshal(jpackIn(t, real, row.Folder, "audit", "checkpoint", "--config", "jpack.json", "--format", "json"), &head); err != nil || !keyIDForm.MatchString(head.Checkpoint.Trail) {
		t.Fatalf("the trail's identity: %v", err)
	}
	trail := head.Checkpoint.Trail
	gateway.register(t, trail)
	gateway.start(t)
	submitter := gateway.token(witnessTestSubject, witnessTestAudience)

	// The owner sets the witness.
	proposal := map[string]any{"url": gateway.url, "publicKey": gateway.public}
	status, data := witnessCall(t, ts, ts.URL, "/api/audit/witness/check", row.ID, proposal)
	var checked witnessChecked
	if status != http.StatusOK || json.Unmarshal([]byte(data), &checked) != nil || checked.Trail != trail || checked.Shown.PublicKey != gateway.public {
		t.Fatalf("the check answered %d %s", status, data)
	}
	set := func(token string) {
		t.Helper()
		status, data := witnessCall(t, ts, ts.URL, "/api/audit/witness/check", row.ID, proposal)
		if status != http.StatusOK || json.Unmarshal([]byte(data), &checked) != nil {
			t.Fatalf("the check answered %d %s", status, data)
		}
		body := map[string]any{"url": gateway.url, "publicKey": gateway.public, "submitterToken": token, "token": checked.Token}
		if status, data := witnessCall(t, ts, ts.URL, "/api/audit/witness", row.ID, body); status != http.StatusOK || strings.Contains(data, token) {
			t.Fatalf("the witness was not kept: %d", status)
		}
	}
	set(submitter)
	tokenFile := filepath.Join(config, "secrets", "witness", id)
	entries, err := os.ReadDir(tokenFile)
	if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".token") {
		t.Fatalf("Desk's custody holds %v (%v)", entries, err)
	}
	if info, err := os.Stat(filepath.Join(tokenFile, entries[0].Name())); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the token's file is %v (%v)", info, err)
	}

	// Send now: countersigned through record 2.
	attempt := sendNow(t, ts, ts.URL, row.ID)
	if attempt.Outcome != witnessCountersigned || attempt.Through != 2 || attempt.Status != http.StatusOK || attempt.Index == nil || *attempt.Index != 0 {
		t.Fatalf("Send now: %+v; the witness said %s", attempt, gateway.stderr.String())
	}
	t.Logf("Send now: %s through record %d, statement index %d", attempt.Outcome, attempt.Through, *attempt.Index)

	// The decision record: the runtime's own coverage and witness section.
	readWitnessRecord := func() (auditAnswer, map[string]string) {
		t.Helper()
		os.Remove(capture)
		status, raw := reviewCall(t, ts, "GET", "/api/audit/verify", row.ID, nil, bearer)
		var answer auditAnswer
		if status != http.StatusOK || json.Unmarshal(raw, &answer) != nil || answer.Report == nil {
			t.Fatalf("the decision record answered %d %s", status, raw)
		}
		var page struct {
			Report json.RawMessage `json:"report"`
		}
		_ = json.Unmarshal(raw, &page)
		return answer, members(t, page.Report)
	}
	runtimeSaid := func() (map[string]string, map[string]string) {
		t.Helper()
		printed, err := os.ReadFile(capture)
		if err != nil {
			t.Fatalf("the runtime's reading was not captured: %v", err)
		}
		report := members(t, printed)
		return report, members(t, []byte(report["coverage"]))
	}
	answer, page := readWitnessRecord()
	printed, coverage := runtimeSaid()
	pageCoverage := members(t, []byte(page["coverage"]))
	if pageCoverage["countersigned"] != coverage["countersigned"] || page["witness"] != printed["witness"] || coverage["countersigned"] != `{"status":"through","through":2}` {
		t.Fatalf("the page's countersigned coverage %s and witness %s, the runtime's %s and %s", pageCoverage["countersigned"], page["witness"], coverage["countersigned"], printed["witness"])
	}
	if answer.Report.Witness == nil || answer.Report.Witness.Reading != "current" || answer.Witness == nil || !answer.Witness.Passed ||
		answer.Witness.Head == nil || answer.Witness.Head.State != "fetched" || answer.Witness.Cursor == nil || answer.Witness.Cursor.Through != 2 {
		t.Fatalf("the decision record says %+v %+v", answer.Report.Witness, answer.Witness)
	}
	t.Logf("the decision record: countersigned %s, witness %s", pageCoverage["countersigned"], page["witness"])
	if !answer.Report.Witness.ContinuationSaved {
		t.Errorf("no continuation was saved after a reading with no finding: %+v", answer.Report.Witness)
	}

	// A third run, countersigned on the interval; the next interval, with
	// the head where it was, sends nothing.
	decide()
	w.at(t, time.Hour)
	if _, answer, _ := readAudit(t, ts, row.ID); answer.Witness == nil || answer.Witness.Cursor == nil || answer.Witness.Cursor.Through != 3 ||
		answer.Report.Coverage.Countersigned == nil || answer.Report.Coverage.Countersigned.Through != 3 {
		t.Fatalf("after the interval the decision record says %+v %+v", answer.Witness, answer.Report.Coverage.Countersigned)
	}
	_, before, _ := readAudit(t, ts, row.ID)
	w.at(t, 2*time.Hour)
	if _, after, _ := readAudit(t, ts, row.ID); before.Witness == nil || before.Witness.Last == nil || after.Witness == nil || after.Witness.Last == nil ||
		after.Witness.Last.At != before.Witness.Last.At || after.Witness.Last.Outcome != witnessCountersigned {
		t.Errorf("with the head where it was, the loop did %+v", after.Witness)
	}

	// A refused token: the witness's own words, the cursor unmoved.
	decide()
	wrong := gateway.token(witnessTestSubject, witnessTestEngine)
	set(wrong)
	attempt = sendNow(t, ts, ts.URL, row.ID)
	lines := jpackIn(t, real, row.Folder, "audit", "checkpoint", "--config", "jpack.json", "--since", "3", "--limit", "300")
	status, said := gateway.post(t, "/witness/checkpoints", wrong, "application/jsonl", lines)
	var refusal struct{ Error, Reason string }
	_ = json.Unmarshal(said, &refusal)
	if attempt.Outcome != witnessRefused || attempt.Status != http.StatusUnauthorized || attempt.Reason != "unauthenticated" || status != http.StatusUnauthorized || attempt.Error != refusal.Error {
		t.Fatalf("a refused token: %+v; the witness says %d %s", attempt, status, said)
	}
	t.Logf("a refused token: %d %s %q", attempt.Status, attempt.Reason, attempt.Error)
	if _, answer, _ := readAudit(t, ts, row.ID); answer.Witness.Cursor.Through != 3 {
		t.Fatalf("a refused token moved the cursor: %+v", answer.Witness.Cursor)
	}

	// A conflict: another deliverer under the same credential had a forged
	// record signed at sequence 4; Desk's lines 4 and 5 conflict there.
	set(submitter)
	decide()
	forged := fmt.Sprintf(`{"checkpointVersion":"1","recordDigest":"sha256:%064x","sequence":4,"trail":"%s"}`+"\n", 4, trail)
	if status, said := gateway.post(t, "/witness/checkpoints", submitter, "application/jsonl", []byte(forged)); status != http.StatusOK {
		t.Fatalf("the forged checkpoint was not signed: %d %s", status, said)
	}
	attempt = sendNow(t, ts, ts.URL, row.ID)
	lines = jpackIn(t, real, row.Folder, "audit", "checkpoint", "--config", "jpack.json", "--since", "3", "--limit", "300")
	status, said = gateway.post(t, "/witness/checkpoints", submitter, "application/jsonl", lines)
	_ = json.Unmarshal(said, &refusal)
	if attempt.Outcome != witnessRefused || attempt.Status != http.StatusConflict || attempt.Reason != "conflict" || status != http.StatusConflict || attempt.Error != refusal.Error {
		t.Fatalf("a conflict: %+v; the witness says %d %s", attempt, status, said)
	}
	t.Logf("a conflict: %d %s %q", attempt.Status, attempt.Reason, attempt.Error)
	answer, page = readWitnessRecord()
	printed, coverage = runtimeSaid()
	if answer.Witness.Cursor.Through != 3 || answer.Report.Witness == nil || answer.Report.Witness.ConflictsTotal != 1 ||
		members(t, []byte(page["coverage"]))["countersigned"] != coverage["countersigned"] || page["witness"] != printed["witness"] {
		t.Fatalf("after the conflict the decision record says %+v %+v; the runtime's %s", answer.Witness.Cursor, answer.Report.Witness, printed["witness"])
	}

	// The witness stopped: Desk keeps working, says it could not reach the
	// witness, and reads the chain as far as it was supplied.
	gateway.stop(t)
	decide()
	attempt = sendNow(t, ts, ts.URL, row.ID)
	if attempt.Outcome != witnessUnreached || attempt.Problem != "" {
		t.Fatalf("with the witness stopped, Send now: %+v", attempt)
	}
	t.Logf("the witness stopped: %s", attempt.Outcome)
	if _, answer, _ := readAudit(t, ts, row.ID); answer.Report == nil || answer.Report.Witness == nil || answer.Report.Witness.Reading != "historical" ||
		answer.Witness.Head == nil || answer.Witness.Head.State != "unreached" || answer.Witness.Cursor.Through != 3 {
		t.Fatalf("with the witness stopped the decision record says %+v %+v", answer.Report, answer.Witness)
	}

	// Retired: the witness's words, the cursor unmoved, and nothing more is
	// sent for the trail.
	gateway.start(t)
	if status, said := gateway.post(t, "/witness/trails/"+trail+"/retirement", gateway.token("operator", witnessTestEngine), "", nil); status != http.StatusOK {
		t.Fatalf("the retirement: %d %s", status, said)
	}
	attempt = sendNow(t, ts, ts.URL, row.ID)
	if attempt.Outcome != witnessRefused || attempt.Status != http.StatusConflict || attempt.Reason != "retired" || attempt.Error == "" {
		t.Fatalf("a retired trail: %+v", attempt)
	}
	t.Logf("a retired trail: %d %s %q", attempt.Status, attempt.Reason, attempt.Error)
	if attempt = sendNow(t, ts, ts.URL, row.ID); attempt.Outcome != witnessNothing || attempt.Problem != witnessRetiredWords {
		t.Fatalf("after the retirement, Send now: %+v", attempt)
	}
	answer, page = readWitnessRecord()
	printed, coverage = runtimeSaid()
	if answer.Witness.Cursor.Through != 3 || !answer.Witness.Cursor.Retired || answer.Report.Witness == nil || !answer.Report.Witness.Retired ||
		members(t, []byte(page["coverage"]))["countersigned"] != coverage["countersigned"] || page["witness"] != printed["witness"] {
		t.Fatalf("after the retirement the decision record says %+v %+v", answer.Witness.Cursor, answer.Report.Witness)
	}

	// The token: in no answer, no log, no project file.
	for _, token := range []string{submitter, wrong} {
		if strings.Contains(logged.String(), token) {
			t.Error("Desk's log holds a submitter's token")
		}
		if found := fileHolding(t, row.Folder, token); found != "" {
			t.Errorf("a project file holds a submitter's token: %s", filepath.Base(found))
		}
	}
}
