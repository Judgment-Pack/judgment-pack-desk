//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package desk

// A witness as a holder (ADR-0012; issue #234). A stand-in runtime, by
// absolute path, answers `audit checkpoint` from a file of checkpoint lines,
// as the hand-over's tests have it, and `audit verify` as the decision
// record's tests have it; a stand-in witness, on loopback, answers as the
// gateway's reference does (SPEC.md §6), from what each test prepares, and
// records what reached it. The published runtime and the gateway's own
// witness are driven by witness_real_test.go.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// standInWitnessKey is the key the stand-in witness's statements name: no
// signature is checked by Desk's Go, and the stand-in runtime accepts any.
const standInWitnessKey = "3b6a27bcceb6a42d62a3a8d02a6f0d73653215771de243a63ac048a18b59da29"

// statementOf is a statement line as a witness serves it, its newline
// included: of kind, at index, over the stand-in's checkpoint at sequence of
// trail.
func statementOf(kind string, index int64, trail string, sequence int) string {
	prev := "null"
	if index > 0 {
		prev = `"` + strings.Repeat("ab", 64) + `"`
	}
	return fmt.Sprintf(`{"checkpoint":{"checkpointVersion":"1","recordDigest":"%s","sequence":%d,"trail":"%s"},"index":%d,"keyId":"%s","kind":"%s","prevSignature":%s,"signature":"%s","witnessVersion":"1","witnessedAt":"2026-10-12T00:00:00Z"}`,
		standInDigest(sequence), sequence, trail, index, strings.Repeat("c", 32), kind, prev, strings.Repeat(fmt.Sprintf("%02x", index%256), 64)) + "\n"
}

// standInWitness is a witness on loopback that answers each request with
// what the test prepared, and records what reached it.
type standInWitness struct {
	*httptest.Server
	mu sync.Mutex
	// submit answers a submission, by its body.
	submit func(body []byte) (int, string, string)
	// head and statements are the trail's head and statements as served;
	// down makes every read answer 503.
	head       string
	statements []string
	// redirect, where set, answers every submission with a redirect there.
	redirect string
	seen     []witnessRequestSeen
}

// witnessRequestSeen is one request as it reached the stand-in.
type witnessRequestSeen struct {
	method, path, auth string
	body               []byte
}

func newStandInWitness(t *testing.T) *standInWitness {
	t.Helper()
	w := &standInWitness{}
	w.Server = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.mu.Lock()
		w.seen = append(w.seen, witnessRequestSeen{method: r.Method, path: r.URL.RequestURI(), auth: r.Header.Get("Authorization"), body: body})
		submit, head, statements, redirect := w.submit, w.head, slices.Clone(w.statements), w.redirect
		w.mu.Unlock()
		if redirect != "" && r.Method == http.MethodPost {
			http.Redirect(rw, r, redirect+r.URL.Path, http.StatusTemporaryRedirect)
			return
		}
		answer := func(status int, media, text string) {
			rw.Header().Set("Content-Type", media)
			rw.WriteHeader(status)
			io.WriteString(rw, text)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/witness/checkpoints" && submit != nil:
			status, media, text := submit(body)
			answer(status, media, text)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/head") && head != "":
			answer(http.StatusOK, "application/jsonl", head)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/statements"):
			from := 0
			fmt.Sscanf(r.URL.Query().Get("from"), "%d", &from)
			var out string
			for i := from; i < len(statements); i++ {
				out += statements[i]
			}
			answer(http.StatusOK, "application/jsonl", out)
		default:
			answer(http.StatusNotFound, "application/json", `{"error":"The witness holds no statement for this trail.","reason":"unknown-trail"}`)
		}
	}))
	t.Cleanup(w.Close)
	return w
}

// set changes what the stand-in answers.
func (w *standInWitness) set(change func(w *standInWitness)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	change(w)
}

// requests is what reached the stand-in since the last call, then forgets it.
func (w *standInWitness) requests() []witnessRequestSeen {
	w.mu.Lock()
	defer w.mu.Unlock()
	seen := w.seen
	w.seen = nil
	return seen
}

// witnessRig is a hand-over rig with Desk's configuration folder of its own,
// a stand-in witness, and the log kept.
type witnessRig struct {
	*handoverRig
	config  string
	witness *standInWitness
	logged  *lockedWriter
	wake    *witnessWaker
}

// witnessHolderID is the id the witness holder is made with.
const witnessHolderID = "00000000000000b4"

func newWitnessRig(t *testing.T) *witnessRig {
	t.Helper()
	t.Setenv("JPACK_CONFIG", "")
	fixHandover(t, witnessHolderID)
	wake := fixWitness(t)
	rig := &handoverRig{auditRig: newAuditRig(t, withAuditVersions), project: t.TempDir(), logged: &bytes.Buffer{}}
	withCheckpoints(t, rig.bin, rig.calls)
	rig.answers(t, 0, auditValidReport)
	writeProject(t, rig.project, map[string]string{"jpack.json": auditedConfig})
	if err := os.MkdirAll(filepath.Join(rig.project, ".desk-private", "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	logged := &lockedWriter{w: &bytes.Buffer{}}
	config := t.TempDir()
	rig.s, rig.ts = startDesk(t, Config{ProjectDir: rig.project, JpackBin: rig.bin, Token: testToken, DeskConfigDir: config, Logger: log.New(logged, "", 0)})
	t.Cleanup(func() { rig.s.Close() })
	t.Cleanup(rig.ts.Close)
	return &witnessRig{handoverRig: rig, config: config, witness: newStandInWitness(t), logged: logged, wake: wake}
}

// setWitnessWith sets the stand-in as this desk's witness, under token,
// through the check and the confirmation.
func (r *witnessRig) setWitnessWith(t *testing.T, token string) {
	t.Helper()
	// The key's check reads the report the runtime gives once it has read
	// the key: one with a witness section of one key.
	r.answers(t, 1, witnessKeyCheckReport)
	defer r.answers(t, 0, auditValidReport)
	proposal := map[string]any{"url": r.witness.URL, "publicKey": standInWitnessKey}
	status, data := witnessCall(t, r.ts, r.ts.URL, "/api/audit/witness/check", "", proposal)
	var checked witnessChecked
	if status != http.StatusOK || json.Unmarshal([]byte(data), &checked) != nil {
		t.Fatalf("the check answered %d %s", status, data)
	}
	proposal["submitterToken"], proposal["token"] = token, checked.Token
	if status, data := witnessCall(t, r.ts, r.ts.URL, "/api/audit/witness", "", proposal); status != http.StatusOK || strings.Contains(data, token) {
		t.Fatalf("the witness was not kept, or its answer holds the token: %d", status)
	}
}

// cursor is Desk's cursor at the witness for trail, as its record keeps it.
func (r *witnessRig) cursor(t *testing.T, trail string) witnessCursor {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.project, ".desk-private", "handover", witnessHolderID, "record.json"))
	if os.IsNotExist(err) {
		return witnessCursor{}
	}
	var record handoverRecord
	if err != nil || json.Unmarshal(data, &record) != nil {
		t.Fatalf("the holder's record: %v %s", err, data)
	}
	return record.Witness[trail]
}

// kept is the statements Desk keeps for trail.
func (r *witnessRig) kept(t *testing.T, trail string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.project, ".desk-private", "handover", witnessHolderID, statementsName(trail)))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

// witnessReport is auditValidReport with a witness section whose latest
// checkpoint statement is at index and sequence, and its countersigned
// coverage, as runtime 0.30.0 prints them.
func witnessReport(index int64, sequence int) string {
	section := fmt.Sprintf(`,"witness":{"keysSupplied":1,"statementsRead":%d,"statementsChecked":%d,"began":"index-0","status":"read","reading":"current","headIndex":%d,"highestIndex":%d,"latestCheckpoint":{"index":%d,"sequence":%d,"witnessedAt":"2026-10-12T00:00:00Z"},"conflicts":[],"conflictsTotal":0,"retired":false,"countersignedAt":"2026-10-12T00:00:00Z","continuationSaved":false}`, index+1, index+1, index, index, index, sequence)
	report := strings.Replace(auditValidReport, `"stamped":{"status":"not-checked","detail":"no time-stamping roots were supplied"}}`,
		fmt.Sprintf(`"stamped":{"status":"not-checked","detail":"no time-stamping roots were supplied"},"countersigned":{"status":"through","through":%d}}`, sequence), 1)
	return strings.Replace(report, `,"findings":[]`, section+`,"findings":[]`, 1)
}

// witnessKeyCheckReport is what runtime 0.30.0 prints for `audit verify
// --witness-key <key>` over a trail of three records with no statement
// supplied: the key read, and a chain that does not begin at index 0.
var witnessKeyCheckReport = strings.NewReplacer(
	`"status":"valid"`, `"status":"invalid"`,
	`,"findings":[]`, `,"witness":{"keysSupplied":1,"statementsRead":0,"statementsChecked":0,"began":"index-0","status":"failed","headIndex":null,"conflicts":[],"conflictsTotal":0,"retired":false,"continuationSaved":false},"findings":[{"name":"witness-chain-broken","line":0,"detail":"no statement begins the chain at index 0: a chain is read from its first statement"}]`,
	`"findingsTotal":0`, `"findingsTotal":1`,
).Replace(auditValidReport)

/* The forms Desk reads ------------------------------------------------------------- */

// **A statement is held to its form, and nothing more.** Eight members, each
// of its form; a ninth, one missing, one named twice, an index spelled -0,
// 1.0 or 01, an unknown kind, an upper-case signature, and a witnessedAt of
// another form are each refused.
func TestWitnessStatementForm(t *testing.T) {
	good := strings.TrimSuffix(statementOf("checkpoint", 7, handoverTrail, 120), "\n")
	statement, ok := readWitnessStatement([]byte(good))
	if !ok || statement.index != 7 || statement.kind != "checkpoint" || statement.checkpoint.sequence != 120 || statement.checkpoint.trail != handoverTrail {
		t.Fatalf("a statement read as %+v %v", statement, ok)
	}
	if _, ok := readWitnessStatement([]byte(strings.TrimSuffix(statementOf("checkpoint", 0, handoverTrail, 1), "\n"))); !ok {
		t.Error("a statement at index 0 is refused")
	}
	for name, bad := range map[string]string{
		"a ninth member":        strings.Replace(good, `"witnessVersion":"1"`, `"witnessVersion":"1","extra":1`, 1),
		"a member missing":      strings.Replace(good, `,"witnessedAt":"2026-10-12T00:00:00Z"`, ``, 1),
		"a member twice":        strings.Replace(good, `"index":7`, `"index":7,"index":7`, 1),
		"an index of -0":        strings.Replace(good, `"index":7`, `"index":-0`, 1),
		"an index of 1.0":       strings.Replace(good, `"index":7`, `"index":1.0`, 1),
		"an index of 07":        strings.Replace(good, `"index":7`, `"index":07`, 1),
		"an unknown kind":       strings.Replace(good, `"kind":"checkpoint"`, `"kind":"other"`, 1),
		"an upper-case sig":     strings.Replace(good, `"signature":"07`, `"signature":"0A`, 1),
		"a witnessedAt of more": strings.Replace(good, `00:00:00Z`, `00:00:00.5Z`, 1),
		"version 2":             strings.Replace(good, `"witnessVersion":"1"`, `"witnessVersion":"2"`, 1),
	} {
		if bad == good {
			t.Fatalf("%s: the mutation did not apply", name)
		}
		if _, ok := readWitnessStatement([]byte(bad)); ok {
			t.Errorf("%s is read as a statement", name)
		}
	}
}

// **The URL is the owner's, held to Desk's rules.** https anywhere; http only
// to a loopback address written as one; never a user, a query, a fragment or
// a space.
func TestWitnessURLForm(t *testing.T) {
	for address, want := range map[string]bool{
		"https://witness.example":           true,
		"https://witness.example/prefix/":   true,
		"http://127.0.0.1:8787":             true,
		"http://[::1]:8787":                 true,
		"http://localhost:8787":             false,
		"http://witness.example":            false,
		"https://user:pw@witness.example":   false,
		"https://witness.example/?a=b":      false,
		"https://witness.example/#here":     false,
		"https://witness.example/ space":    false,
		"ftp://witness.example":             false,
		"https://":                          false,
		strings.Repeat("https://a.b/", 200): false,
	} {
		if got := checkWitnessURL(address); got != want {
			t.Errorf("checkWitnessURL(%.40q) = %v, want %v", address, got, want)
		}
	}
}

// **The body is the lines since the cursor, ending with the head's, at most
// the witness's 1 MiB.** Under the bound, every line as printed; over it,
// the earliest whole lines left out, the last line the head's, and how many
// were left out said.
func TestWitnessBodyIsCutAtTheWitnessBound(t *testing.T) {
	lines := chainOf(handoverTrail, upTo(10)...)
	body, first, unsent := witnessBody([]byte(lines))
	if string(body) != lines || first != 1 || unsent != 0 {
		t.Fatalf("under the bound: %d bytes from %d, %d unsent", len(body), first, unsent)
	}
	n := witnessBodyLimit/len(checkpointOf(handoverTrail, 9999)) + 100
	lines = chainOf(handoverTrail, upTo(n)...)
	body, first, unsent = witnessBody([]byte(lines))
	if len(body) > witnessBodyLimit || !strings.HasSuffix(lines, string(body)) || !strings.HasSuffix(string(body), checkpointOf(handoverTrail, n)) ||
		!strings.HasPrefix(string(body), checkpointOf(handoverTrail, int(first))) || unsent != first-1 || unsent < 1 {
		t.Fatalf("over the bound: %d bytes from %d, %d unsent of %d lines", len(body), first, unsent, n)
	}
	if next := checkpointOf(handoverTrail, int(first)-1); len(body)+len(next) <= witnessBodyLimit {
		t.Errorf("a line that fits was left out: %d bytes and a line of %d", len(body), len(next))
	}
}

// **The witness's answers are read in its own form, or not at all.** A
// statement answers 200 application/jsonl; a refusal application/json, of
// exactly error and reason, and a 409's statements; Retry-After is read in
// seconds. A redirect, another type, a member more, and a 409's statement
// that is not one are each no answer of the witness's form.
func TestWitnessRepliesAreReadInTheWitnessForm(t *testing.T) {
	reply := func(status int, media, body string, header ...string) witnessReply {
		response := &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {media}}}
		if len(header) == 2 {
			response.Header.Set(header[0], header[1])
		}
		return readWitnessReply(response, []byte(body), false)
	}
	statement := statementOf("checkpoint", 0, handoverTrail, 3)
	if got := reply(200, "application/jsonl", statement); !got.Reached || len(got.statements) != 1 || string(got.statements[0]) != statement {
		t.Errorf("a statement: %+v", got)
	}
	conflict, _ := json.Marshal(map[string]any{"error": "Another record was offered.", "reason": "conflict",
		"statements": []string{strings.TrimSuffix(statementOf("checkpoint", 1, handoverTrail, 2), "\n"), strings.TrimSuffix(statementOf("conflict", 2, handoverTrail, 2), "\n")}})
	if got := reply(409, "application/json", string(conflict)); !got.Reached || got.Reason != "conflict" || got.Error != "Another record was offered." || len(got.statements) != 2 {
		t.Errorf("a conflict: %+v", got)
	}
	if got := reply(429, "application/json", `{"error":"Over the rate.","reason":"rate"}`, "Retry-After", "17"); !got.Reached || got.Reason != "rate" {
		t.Errorf("a refusal over the rate: %+v", got)
	}
	for name, got := range map[string]witnessReply{
		"a redirect":                   reply(307, "text/html", `<a href="https://elsewhere">moved</a>`),
		"a statement of another type":  reply(200, "application/json", statement),
		"a refusal with a member more": reply(403, "application/json", `{"error":"No.","reason":"not-registered","more":1}`),
		"a refusal with no reason":     reply(403, "application/json", `{"error":"No."}`),
		"a 409 statement that is not":  reply(409, "application/json", `{"error":"No.","reason":"conflict","statements":["{}"]}`),
		"statements on a 403":          reply(403, "application/json", `{"error":"No.","reason":"x","statements":[`+fmt.Sprintf("%q", strings.TrimSuffix(statement, "\n"))+`]}`),
	} {
		if got.Reached {
			t.Errorf("%s was read as the witness's answer: %+v", name, got)
		}
	}
}

// **The key is held to the runtime's rule, and only its refusal is read.**
// JPS-AUDIT-WITNESS-KEY-INVALID is the runtime's refusal, in its words; a
// report, or a refusal of the trail made after the key was read, is the
// key's acceptance; a key the runtime could not read, an older runtime, and
// an answer of no form are each said, and nothing is kept.
func TestWitnessKeyIsHeldToTheRuntimesRefusal(t *testing.T) {
	invalid := `{"outputVersion":"2","tool":{"name":"jpack","version":"0.30.0"},"command":"audit verify","status":"error","diagnostics":[{"code":"JPS-AUDIT-WITNESS-KEY-INVALID","message":"The witness key k.pub is refused (key-small-order): the public key is of small order."}]}`
	if failure := keyCheckAnswer([]byte(invalid), exitError(t, 3)); failure == nil || failure.message != "The witness key k.pub is refused (key-small-order): the public key is of small order." || failure.status != http.StatusBadRequest {
		t.Errorf("a key the runtime refuses: %+v", failure)
	}
	if failure := keyCheckAnswer([]byte(witnessKeyCheckReport), exitError(t, 1)); failure != nil {
		t.Errorf("a report of the key read refused the key: %+v", failure)
	}
	if failure := keyCheckAnswer([]byte(auditValidReport), nil); failure == nil || failure.message != witnessUndocumentedKey {
		t.Errorf("a report with no witness section accepted the key: %+v", failure)
	}
	if failure := keyCheckAnswer([]byte(auditNoTrailYet), exitError(t, 4)); failure != nil {
		t.Errorf("a refusal of the trail, after the key was read, refused the key: %+v", failure)
	}
	read := `{"outputVersion":"2","tool":{"name":"jpack","version":"0.30.0"},"command":"audit verify","status":"error","diagnostics":[{"code":"JPS-AUDIT-WITNESS-KEY-READ","message":"The witness key k.pub could not be read as one bounded regular file."}]}`
	older := `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"jpack","status":"error","diagnostics":[{"code":"JPS-INVOCATION-ARGUMENTS","message":"unknown flag: --witness-key"}]}`
	for name, answer := range map[string]string{"a key not read": read, "an older runtime": older, "no form": `not json`} {
		if failure := keyCheckAnswer([]byte(answer), exitError(t, 3)); failure == nil {
			t.Errorf("%s accepted the key", name)
		}
	}
	if failure := keyCheckAnswer([]byte(older), exitError(t, 3)); failure == nil || !strings.Contains(failure.message, "jpack 0.27.1") {
		t.Errorf("an older runtime: %+v", failure)
	}
}

// **The check runs the runtime's key rule, and refuses Desk's own gateway's
// key.** A key the runtime refuses is refused in its words, and nothing is
// kept; the public key of Desk's own local gateway, the operator's, is
// refused in Desk's; in neither case is a holder or a token kept.
func TestWitnessCheckHoldsTheKeyToTheRuntimeAndRefusesTheOperatorsOwn(t *testing.T) {
	r := newWitnessRig(t)
	r.chain(t, chainOf(handoverTrail, upTo(2)...))
	said := "The witness key is refused (key-small-order): the public key is of small order."
	r.answers(t, 3, `{"outputVersion":"2","tool":{"name":"jpack","version":"0.30.0"},"command":"audit verify","status":"error","diagnostics":[{"code":"JPS-AUDIT-WITNESS-KEY-INVALID","message":"`+said+`"}]}`)
	r.ran(t)
	proposal := map[string]any{"url": r.witness.URL, "publicKey": standInWitnessKey}
	status, data := witnessCall(t, r.ts, r.ts.URL, "/api/audit/witness/check", "", proposal)
	if status != http.StatusBadRequest || refusalOf([]byte(data)) != said {
		t.Fatalf("a key the runtime refuses: %d %s", status, data)
	}
	if !slices.ContainsFunc(r.ran(t), func(call string) bool {
		return strings.HasPrefix(call, "audit verify --config jpack.json --format json --witness-key .desk-private/handover/.witness-key-")
	}) {
		t.Error("the key was not given to the runtime's audit verify --witness-key")
	}
	r.answers(t, 1, witnessKeyCheckReport)
	marker, _ := json.Marshal(localSigner{Algorithm: "ed25519", Public: standInWitnessKey})
	if err := os.WriteFile(filepath.Join(r.config, "local-gateway-identity.json"), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	if status, data := witnessCall(t, r.ts, r.ts.URL, "/api/audit/witness/check", "", proposal); status != http.StatusBadRequest || refusalOf([]byte(data)) != witnessOwnKeyWords {
		t.Errorf("the local gateway's key: %d %s", status, data)
	}
	if entries, _ := os.ReadDir(filepath.Join(r.project, ".desk-private", "handover")); slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.Name() != ".gitignore" }) {
		t.Errorf("a refused check kept %v", entries)
	}
}

/* The cursor ---------------------------------------------------------------- */

// **The cursor moves only on a confirmed statement for the last line sent.**
// With the trail through record 3 and the cursor at 0, Desk sends the lines
// the runtime printed, exactly, under the token. A 409 conflict, a 401, a
// 503 and a statement for record 2 each leave the cursor where it was, and
// every statement answered is kept as received; a statement for record 3
// that the runtime's reading does not confirm leaves it too; one it
// confirms moves it to record 3, with the statement's index.
func TestWitnessCursorMovesOnlyOnAConfirmedStatementForTheLastLineSent(t *testing.T) {
	r := newWitnessRig(t)
	r.chain(t, chainOf(handoverTrail, upTo(3)...))
	r.setWitnessWith(t, "submitter.token.value")
	// Were the statement not held to the last line sent, the runtime's
	// reading would confirm one for record 2.
	r.answers(t, 0, witnessReport(2, 2))
	conflict, _ := json.Marshal(map[string]any{"error": "Another record was offered for a sequence the witness holds.", "reason": "conflict",
		"statements": []string{strings.TrimSuffix(statementOf("checkpoint", 0, handoverTrail, 1), "\n"), strings.TrimSuffix(statementOf("conflict", 1, handoverTrail, 1), "\n")}})
	for _, step := range []struct {
		name   string
		status int
		media  string
		body   string
	}{
		{"a conflict", 409, "application/json", string(conflict)},
		{"a refused token", 401, "application/json", `{"error":"the token has expired","reason":"unauthenticated"}`},
		{"a stopped witness", 503, "application/json", `{"error":"The witness signs nothing until it restarts.","reason":"stopped"}`},
		{"a statement for another line", 200, "application/jsonl", statementOf("checkpoint", 2, handoverTrail, 2)},
	} {
		r.witness.set(func(w *standInWitness) {
			w.submit = func([]byte) (int, string, string) { return step.status, step.media, step.body }
		})
		attempt := sendNow(t, r.ts, r.ts.URL, "")
		if got := r.cursor(t, handoverTrail); got.Through != 0 {
			t.Errorf("%s moved the cursor to %d", step.name, got.Through)
		}
		if step.status != 200 && (attempt.Outcome != witnessRefused || attempt.Status != step.status || attempt.Error == "" || !strings.Contains(step.body, attempt.Error)) {
			t.Errorf("%s was not shown in the witness's words: %+v", step.name, attempt)
		}
		seen := r.witness.requests()
		if len(seen) != 1 || seen[0].path != "/witness/checkpoints" || string(seen[0].body) != chainOf(handoverTrail, upTo(3)...) || seen[0].auth != "Bearer submitter.token.value" {
			t.Fatalf("%s: the witness saw %+v", step.name, seen)
		}
	}
	if kept := r.kept(t, handoverTrail); !strings.Contains(kept, statementOf("conflict", 1, handoverTrail, 1)) || !strings.Contains(kept, statementOf("checkpoint", 2, handoverTrail, 2)) {
		t.Errorf("the statements answered are not kept as received: %q", kept)
	}

	// A statement for record 3, which the runtime's reading does not confirm:
	// its latest checkpoint statement is another.
	line := statementOf("checkpoint", 3, handoverTrail, 3)
	r.witness.set(func(w *standInWitness) {
		w.submit = func([]byte) (int, string, string) { return 200, "application/jsonl", line }
		w.statements = []string{statementOf("checkpoint", 0, handoverTrail, 1), statementOf("conflict", 1, handoverTrail, 1), statementOf("checkpoint", 2, handoverTrail, 2), line}
	})
	if attempt := sendNow(t, r.ts, r.ts.URL, ""); attempt.Outcome != witnessKept || r.cursor(t, handoverTrail).Through != 0 {
		t.Fatalf("an unconfirmed statement: %+v, the cursor at %d", attempt, r.cursor(t, handoverTrail).Through)
	}
	// The same, confirmed.
	r.answers(t, 0, witnessReport(3, 3))
	r.ran(t)
	attempt := sendNow(t, r.ts, r.ts.URL, "")
	if got := r.cursor(t, handoverTrail); attempt.Outcome != witnessCountersigned || got.Through != 3 || got.Index != 3 || got.Digest != standInDigest(3) {
		t.Fatalf("a confirmed statement: %+v, the cursor %+v", attempt, got)
	}
	reading := slices.IndexFunc(r.ran(t), func(call string) bool { return strings.Contains(call, "--witness-head") })
	if reading < 0 {
		t.Fatal("the cursor moved with no reading by the runtime")
	}
	// Nothing more to send: the head is at the cursor, and nothing reaches the
	// witness.
	r.witness.requests()
	if attempt := sendNow(t, r.ts, r.ts.URL, ""); attempt.Outcome != witnessNothing || attempt.Problem != witnessNothingWords || len(r.witness.requests()) != 0 {
		t.Errorf("with the head at the cursor: %+v", attempt)
	}
}

// **Below the witness's head, Desk waits; after a retirement, it stops.** A
// 409 below-head keeps the head and sends again only once the trail's head
// is past it; a 409 retired keeps the retirement and submits nothing more
// for the trail.
func TestWitnessWaitsBelowItsHeadAndStopsOnARetirement(t *testing.T) {
	r := newWitnessRig(t)
	r.chain(t, chainOf(handoverTrail, upTo(3)...))
	r.setWitnessWith(t, "submitter.token.value")
	below, _ := json.Marshal(map[string]any{"error": "The witness holds a later checkpoint.", "reason": "below-head", "statements": []string{strings.TrimSuffix(statementOf("checkpoint", 4, handoverTrail, 5), "\n")}})
	r.witness.set(func(w *standInWitness) {
		w.submit = func([]byte) (int, string, string) { return 409, "application/json", string(below) }
	})
	if attempt := sendNow(t, r.ts, r.ts.URL, ""); attempt.Reason != "below-head" || r.cursor(t, handoverTrail).BelowHead != 5 {
		t.Fatalf("below the head: %+v %+v", attempt, r.cursor(t, handoverTrail))
	}
	r.witness.requests()
	if attempt := sendNow(t, r.ts, r.ts.URL, ""); attempt.Outcome != witnessNothing || len(r.witness.requests()) != 0 {
		t.Errorf("with the trail still below the witness's head: %+v", attempt)
	}
	r.chain(t, chainOf(handoverTrail, upTo(6)...))
	retired, _ := json.Marshal(map[string]any{"error": "The trail is retired.", "reason": "retired", "statements": []string{strings.TrimSuffix(statementOf("retirement", 5, handoverTrail, 5), "\n")}})
	r.witness.set(func(w *standInWitness) {
		w.submit = func([]byte) (int, string, string) { return 409, "application/json", string(retired) }
	})
	if attempt := sendNow(t, r.ts, r.ts.URL, ""); attempt.Reason != "retired" || attempt.Error != "The trail is retired." || !r.cursor(t, handoverTrail).Retired || r.cursor(t, handoverTrail).Through != 0 {
		t.Fatalf("a retirement: %+v %+v", attempt, r.cursor(t, handoverTrail))
	}
	r.witness.requests()
	if attempt := sendNow(t, r.ts, r.ts.URL, ""); attempt.Outcome != witnessNothing || attempt.Problem != witnessRetiredWords || len(r.witness.requests()) != 0 {
		t.Errorf("after a retirement: %+v", attempt)
	}
	if kept := r.kept(t, handoverTrail); !strings.Contains(kept, statementOf("retirement", 5, handoverTrail, 5)) || !strings.Contains(kept, statementOf("checkpoint", 4, handoverTrail, 5)) {
		t.Errorf("the statements the refusals carried are not kept: %q", kept)
	}
}

/* The token's custody --------------------------------------------------------- */

// **The token is in Desk's custody, and nowhere else.** It is kept at
// `secrets/witness/<name>/<holder id>.token`, 0600; sent only as a bearer
// token on a submission to the URL set; never on a read, never to where a
// redirect points; in no answer, no log and no project file. Removing the
// witness removes it, and keeps the statements.
func TestWitnessTokenIsKeptInCustodyAndSentOnlyOnASubmission(t *testing.T) {
	r := newWitnessRig(t)
	r.chain(t, chainOf(handoverTrail, upTo(2)...))
	const token = "eyJ.custody-test.token"
	r.setWitnessWith(t, token)
	name := r.s.signingKeyName()
	path := filepath.Join(r.config, "secrets", "witness", name, witnessHolderID+".token")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the token's file: %v %v", info, err)
	}
	var kept witnessCustody
	if data, _ := os.ReadFile(path); json.Unmarshal(data, &kept) != nil || kept.Token != token || kept.Origin != "http://"+strings.TrimPrefix(r.witness.URL, "http://") {
		t.Fatal("the token's file does not hold the token pasted, bound to the witness's origin")
	}
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		t.Errorf("a redirect was followed, carrying %d bytes of authorization", len(req.Header.Get("Authorization")))
	}))
	t.Cleanup(elsewhere.Close)
	// A redirect answers with a Location the client would follow.
	r.witness.set(func(w *standInWitness) { w.redirect = elsewhere.URL })
	attempt := sendNow(t, r.ts, r.ts.URL, "")
	if attempt.Outcome != witnessUnreached || attempt.Status != http.StatusTemporaryRedirect {
		t.Errorf("a redirecting witness: %+v", attempt)
	}
	r.witness.set(func(w *standInWitness) {
		w.redirect = ""
		w.head = statementOf("checkpoint", 0, handoverTrail, 2)
		w.statements = []string{w.head}
	})
	r.answers(t, 0, witnessReport(0, 2))
	status, data := reviewCall(t, r.ts, "GET", "/api/audit/verify", "", nil, bearer)
	if status != http.StatusOK || strings.Contains(string(data), token) {
		t.Fatalf("the decision record answered %d, holding the token: %v", status, strings.Contains(string(data), token))
	}
	for _, seen := range r.witness.requests() {
		if seen.method == http.MethodGet && seen.auth != "" {
			t.Errorf("a read carried an authorization: %s", seen.path)
		}
	}
	if strings.Contains(r.logged.String(), token) {
		t.Error("Desk's log holds the token")
	}
	if found := fileHolding(t, r.project, token); found != "" {
		t.Errorf("a project file holds the token: %s", filepath.Base(found))
	}
	// Removed: the token and the settings go; the statements stay.
	_, answer, _ := readAudit(t, r.ts, "")
	if answer.Witness == nil || answer.Witness.RemoveToken == "" {
		t.Fatalf("the decision record offers no removal: %+v", answer.Witness)
	}
	if status, data := witnessCall(t, r.ts, r.ts.URL, "/api/audit/witness/remove", "", map[string]any{"token": answer.Witness.RemoveToken}); status != http.StatusOK {
		t.Fatalf("the removal answered %d %s", status, data)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the token's file is still there: %v", err)
	}
	if r.kept(t, handoverTrail) == "" {
		t.Error("the statements were removed with the witness")
	}
	if _, answer, _ := readAudit(t, r.ts, ""); answer.Witness != nil {
		t.Errorf("the decision record still shows a witness: %+v", answer.Witness)
	}
}

/* The decision record's reading ------------------------------------------------- */

// **The panel fetches the head, and passes the reading; a witness that does
// not answer is read historically.** With the stand-in serving the head and
// the statements, `audit verify` is given the pinned key, the statements, the
// head and where to save the continuation; the coverage and the witness
// section are the runtime's report, as printed. With the stand-in stopped,
// the same reading has no head, and the decision record says only that Desk
// could not reach the witness.
func TestWitnessPanelFetchesTheHeadAndReadsHistoricallyWithoutIt(t *testing.T) {
	r := newWitnessRig(t)
	r.chain(t, chainOf(handoverTrail, upTo(2)...))
	r.setWitnessWith(t, "submitter.token.value")
	r.witness.set(func(w *standInWitness) {
		w.head = statementOf("checkpoint", 1, handoverTrail, 2)
		w.statements = []string{statementOf("checkpoint", 0, handoverTrail, 1), w.head}
	})
	r.answers(t, 0, witnessReport(1, 2))
	r.ran(t)
	status, raw := reviewCall(t, r.ts, "GET", "/api/audit/verify", "", nil, bearer)
	var answer auditAnswer
	if status != http.StatusOK || json.Unmarshal(raw, &answer) != nil || answer.Report == nil || answer.Witness == nil {
		t.Fatalf("the decision record answered %d %s", status, raw)
	}
	folder := ".desk-private/handover/" + witnessHolderID + "/"
	verify := r.ran(t)
	want := "--witness-key " + folder + "witness.pub --witness " + folder + ".reading-" + handoverTrail + ".jsonl --witness-head " + folder + "head-" + handoverTrail + ".jsonl --witness-save " + folder + "continuation-" + handoverTrail + ".json"
	if !slices.ContainsFunc(verify, func(call string) bool { return strings.Contains(call, "audit verify") && strings.Contains(call, want) }) {
		t.Fatalf("the panel ran %q, want %q", verify, want)
	}
	var page struct {
		Report struct {
			Coverage map[string]json.RawMessage `json:"coverage"`
			Witness  json.RawMessage            `json:"witness"`
		} `json:"report"`
	}
	_ = json.Unmarshal(raw, &page)
	var printed struct {
		Coverage map[string]json.RawMessage `json:"coverage"`
		Witness  json.RawMessage            `json:"witness"`
	}
	_ = json.Unmarshal([]byte(witnessReport(1, 2)), &printed)
	if compacted(t, page.Report.Coverage["countersigned"]) != compacted(t, printed.Coverage["countersigned"]) || compacted(t, page.Report.Witness) != compacted(t, printed.Witness) {
		t.Errorf("the page's coverage %s and witness %s are not the runtime's", page.Report.Coverage["countersigned"], page.Report.Witness)
	}
	if answer.Witness.Head == nil || answer.Witness.Head.State != "fetched" || !answer.Witness.Passed || r.kept(t, handoverTrail) != statementOf("checkpoint", 0, handoverTrail, 1)+statementOf("checkpoint", 1, handoverTrail, 2) {
		t.Errorf("the decision record says %+v, and keeps %q", answer.Witness, r.kept(t, handoverTrail))
	}

	r.witness.Close()
	_, answer, _ = readAudit(t, r.ts, "")
	verify = r.ran(t)
	if answer.Witness == nil || answer.Witness.Head == nil || answer.Witness.Head.State != "unreached" || !answer.Witness.Passed {
		t.Fatalf("with the witness stopped the decision record says %+v", answer.Witness)
	}
	if slices.ContainsFunc(verify, func(call string) bool { return strings.Contains(call, "--witness-head") }) ||
		!slices.ContainsFunc(verify, func(call string) bool { return strings.Contains(call, "--witness "+folder) }) {
		t.Errorf("with the witness stopped the panel ran %q", verify)
	}

	// The one check the owner asks for: the requirement, and no save.
	r.ran(t)
	status, raw = reviewCall(t, r.ts, "GET", "/api/audit/verify?countersignedThrough=2", "", nil, bearer)
	verify = r.ran(t)
	if status != http.StatusOK || !slices.ContainsFunc(verify, func(call string) bool {
		return strings.Contains(call, "--require-countersigned-through 2") && !strings.Contains(call, "--witness-save")
	}) {
		t.Errorf("the one check answered %d and ran %q", status, verify)
	}
	for _, query := range []string{"countersignedThrough=0", "countersignedThrough=x", "countersignedThrough=1&countersignedThrough=2", "other=1"} {
		if status, _ := reviewCall(t, r.ts, "GET", "/api/audit/verify?"+query, "", nil, bearer); status != http.StatusBadRequest {
			t.Errorf("?%s answered %d", query, status)
		}
	}
}

// **Nothing to require without a witness.** On a desk with no witness, the
// one check is refused, and runs nothing.
func TestWitnessOneOffCheckNeedsAWitness(t *testing.T) {
	r := newWitnessRig(t)
	r.chain(t, chainOf(handoverTrail, upTo(2)...))
	r.ran(t)
	status, data := reviewCall(t, r.ts, "GET", "/api/audit/verify?countersignedThrough=2", "", nil, bearer)
	if status != http.StatusConflict || refusalOf(data) != witnessNoRequireWords {
		t.Errorf("the one check with no witness answered %d %s", status, data)
	}
	if slices.ContainsFunc(r.ran(t), func(call string) bool { return strings.HasPrefix(call, "audit verify") }) {
		t.Error("the one check with no witness ran audit verify")
	}
}

/* The archive rule ---------------------------------------------------------- */

// **A file Desk cannot use is moved aside, named, and kept.** A statements
// file with a line that is not a statement, and a continuation out of its
// form, are each moved aside in the witness's folder under a name that says
// why, named on the decision record, and kept; the reading goes on from what
// is left, from index 0.
func TestWitnessFilesOutOfFormAreMovedAsideAndKept(t *testing.T) {
	r := newWitnessRig(t)
	r.chain(t, chainOf(handoverTrail, upTo(2)...))
	r.setWitnessWith(t, "submitter.token.value")
	folder := filepath.Join(r.project, ".desk-private", "handover", witnessHolderID)
	broken := statementOf("checkpoint", 0, handoverTrail, 1) + "not a statement\n"
	if err := os.WriteFile(filepath.Join(folder, statementsName(handoverTrail)), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, continuationName(handoverTrail)), []byte(`{"continuationVersion":"1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r.witness.set(func(w *standInWitness) {
		w.head = statementOf("checkpoint", 0, handoverTrail, 2)
		w.statements = []string{w.head}
	})
	r.answers(t, 0, witnessReport(0, 2))
	_, answer, _ := readAudit(t, r.ts, "")
	if answer.Witness == nil || len(answer.Witness.SetAside) != 2 {
		t.Fatalf("the decision record names %+v", answer.Witness)
	}
	entries, _ := os.ReadDir(folder)
	var aside []string
	for _, entry := range entries {
		if witnessSetAsideForm.MatchString(entry.Name()) {
			aside = append(aside, entry.Name())
			if data, _ := os.ReadFile(filepath.Join(folder, entry.Name())); strings.HasPrefix(entry.Name(), "statements-") && string(data) != broken {
				t.Errorf("the statements moved aside are not as they were: %q", data)
			}
		}
	}
	if len(aside) != 2 || !strings.HasSuffix(aside[0], "-form") || !strings.HasSuffix(aside[1], "-form") {
		t.Errorf("moved aside: %q", aside)
	}
	if r.kept(t, handoverTrail) != statementOf("checkpoint", 0, handoverTrail, 2) {
		t.Errorf("the reading did not start again from index 0: %q", r.kept(t, handoverTrail))
	}
}

/* The hand-over ---------------------------------------------------------------- */

// **A witness is not handed over to.** It is not listed among the holders,
// and its checkpoints are neither downloaded nor confirmed.
func TestWitnessIsNotAHolderToDownloadFor(t *testing.T) {
	r := newWitnessRig(t)
	r.chain(t, chainOf(handoverTrail, upTo(2)...))
	r.setWitnessWith(t, "submitter.token.value")
	status, data := reviewCall(t, r.ts, "GET", "/api/audit/holders", "", nil, bearer)
	var holders holdersAnswer
	if status != http.StatusOK || json.Unmarshal(data, &holders) != nil || len(holders.Holders) != 0 {
		t.Fatalf("the holders: %d %s", status, data)
	}
	if status, data := reviewCall(t, r.ts, "GET", "/api/audit/checkpoints?holder="+witnessHolderID, "", nil, bearer); status != http.StatusConflict || refusalOf(data) != witnessHolderWords {
		t.Errorf("a download for the witness answered %d %s", status, data)
	}
	confirm := map[string]any{"trail": handoverTrail, "from": 0, "through": 2, "digest": sha256Digest([]byte(chainOf(handoverTrail, 1, 2)))}
	if status, data := reviewCall(t, r.ts, "POST", "/api/audit/holders/"+witnessHolderID+"/confirm", "", confirm, bearer); status != http.StatusConflict || refusalOf(data) != witnessHolderWords {
		t.Errorf("a confirmation for the witness answered %d %s", status, data)
	}
}

/* Helpers shared with witness_real_test.go -------------------------------------- */

// witnessWaker wakes the submission loop of the desk started while it is
// set, and hears each wake acted on.
type witnessWaker struct {
	mu   sync.Mutex
	now  time.Time
	wake chan time.Time
	woke chan struct{}
}

// fixWitness fixes the submission loop's clock at handoverNow, and the wakes
// of the desk named only (the first desk started, where only is empty) to the
// test's own, for the servers started from now on. Every other desk's loop is
// never woken.
func fixWitness(t *testing.T, only ...string) *witnessWaker {
	t.Helper()
	w := &witnessWaker{now: time.Unix(handoverNow, 0), wake: make(chan time.Time), woke: make(chan struct{}, 64)}
	wasClock, wasWake, wasWoke := witnessClock, newWitnessWake, testHookWitnessWoke
	witnessClock = func() time.Time {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.now
	}
	var mu sync.Mutex
	wired := false
	newWitnessWake = func(name string) (<-chan time.Time, func()) {
		mu.Lock()
		defer mu.Unlock()
		if wired || len(only) > 0 && !slices.Contains(only, name) {
			return nil, func() {}
		}
		wired = true
		return w.wake, func() {}
	}
	testHookWitnessWoke = func(wake <-chan time.Time) {
		if wake == w.wake {
			w.woke <- struct{}{}
		}
	}
	t.Cleanup(func() { witnessClock, newWitnessWake, testHookWitnessWoke = wasClock, wasWake, wasWoke })
	return w
}

// at wakes the loop with the clock at handoverNow and after, and waits until
// it has acted.
func (w *witnessWaker) at(t *testing.T, after time.Duration) {
	t.Helper()
	at := time.Unix(handoverNow, 0).Add(after)
	w.mu.Lock()
	w.now = at
	w.mu.Unlock()
	select {
	case w.wake <- at:
	case <-time.After(10 * time.Second):
		t.Fatal("the submission loop did not wake")
	}
	select {
	case <-w.woke:
	case <-time.After(60 * time.Second):
		t.Fatal("the submission loop did not act")
	}
}

// set moves the clock without a wake.
func (w *witnessWaker) set(after time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.now = time.Unix(handoverNow, 0).Add(after)
}

// witnessCall is one call of a witness route.
func witnessCall(t *testing.T, ts interface{ Client() *http.Client }, url, path, desk string, body any) (int, string) {
	t.Helper()
	data, _ := json.Marshal(body)
	request, err := http.NewRequest(http.MethodPost, url+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if desk != "" {
		request.Header.Set("X-Jpack-Desk", desk)
	}
	request.Header.Set("Content-Type", "application/json")
	bearer(request)
	response, err := ts.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	out, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(out)
}

// sendNow is Send now on desk, and the attempt it answered.
func sendNow(t *testing.T, ts interface{ Client() *http.Client }, url, desk string) witnessAttempt {
	t.Helper()
	status, data := witnessCall(t, ts, url, "/api/audit/witness/send", desk, map[string]any{})
	var answer struct {
		Attempt witnessAttempt `json:"attempt"`
	}
	if status != http.StatusOK || json.Unmarshal([]byte(data), &answer) != nil {
		t.Fatalf("Send now answered %d %s", status, data)
	}
	return answer.Attempt
}

// compacted is raw with no white space between its tokens.
func compacted(t *testing.T, raw []byte) string {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	return out.String()
}

// members is the members of the JSON object in raw, each compacted.
func members(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("not an object: %.300s", raw)
	}
	out := map[string]string{}
	for name, value := range object {
		out[name] = compacted(t, value)
	}
	return out
}

// fileHolding is a file under root whose bytes hold needle, or "".
func fileHolding(t *testing.T, root, needle string) string {
	t.Helper()
	found := ""
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || found != "" {
			return nil
		}
		if data, err := os.ReadFile(path); err == nil && bytes.Contains(data, []byte(needle)) {
			found = path
		}
		return nil
	})
	return found
}
