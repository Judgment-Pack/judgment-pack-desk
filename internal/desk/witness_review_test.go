//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package desk

// Review round 1 of #382 (OpenAI gpt-6-astra, head 022c574): the reviewer's
// probes, kept as regression tests. Each failed at 022c574 and holds the fix:
// the token sent only to the origin it was confirmed for; an answer that
// carries a credential kept out of the log, answer.json and the page; a head
// or answer Desk cannot use moved aside before it is replaced; the key's
// check failing closed on an answer the runtime does not document; the
// countersigned coverage the runtime's alone; the token read only when it is
// sent; Retry-After held in either form from receipt; the original answer and
// statements of another trail kept apart. The witness is a transport of the
// test's own, at an origin no network reaches.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// witnessTransportFunc is a transport the test answers itself.
type witnessTransportFunc func(*http.Request) (*http.Response, error)

func (f witnessTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// replyOf is an answer of the given status, type and body.
func replyOf(status int, media, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {media}}, Body: io.NopCloser(strings.NewReader(body))}
}

// answeredBy makes every request to a witness go to f, for this test.
func answeredBy(t *testing.T, f witnessTransportFunc) {
	t.Helper()
	was := witnessTransport
	witnessTransport = f
	t.Cleanup(func() { witnessTransport = was })
}

// confirmedSecret is the token the confirmed witness is set with.
const confirmedSecret = "review.synthetic.secret"

// newConfirmedWitnessRig is a witness rig whose witness was confirmed at
// https://confirmed.example, under confirmedSecret, over a trail through
// record 3.
func newConfirmedWitnessRig(t *testing.T) *witnessRig {
	t.Helper()
	r := newWitnessRig(t)
	r.chain(t, chainOf(handoverTrail, upTo(3)...))
	plan := witnessPlan{url: "https://confirmed.example", key: standInWitnessKey, interval: 60}
	token := r.s.stampingToken(witnessSetPurpose, newStampingNonce(), plan.digest(), currentDigest(nil, false))
	r.s.witnessMu.Lock()
	_, failure := r.s.setWitness(plan, confirmedSecret, token)
	r.s.witnessMu.Unlock()
	if failure != nil {
		t.Fatal(failure.message)
	}
	return r
}

// **Finding 1: the token goes to the origin it was confirmed for, and no
// other.** With witness.json's URL edited to another origin and nothing
// confirmed, a submission sends nothing, and says why in one sentence. A
// path at the same origin sends; a URL confirmed again, with its token, sends
// to the new origin.
func TestWitnessTokenIsSentOnlyToTheOriginItWasConfirmedFor(t *testing.T) {
	r := newConfirmedWitnessRig(t)
	at := r.handoverPath(witnessHolderID, witnessSettingsName)
	confirmed := readFile(t, at)
	if err := os.WriteFile(at, []byte(strings.ReplaceAll(confirmed, "https://confirmed.example", "https://unconfirmed.example")), 0o600); err != nil {
		t.Fatal(err)
	}
	var hosts []string
	answeredBy(t, func(req *http.Request) (*http.Response, error) {
		hosts = append(hosts, req.URL.Host)
		return replyOf(401, "application/json", `{"error":"refused","reason":"unauthenticated"}`), nil
	})
	if attempt := r.s.witnessLoop.attempt(true); attempt.Outcome != witnessProblem || attempt.Problem != witnessOriginWords || len(hosts) != 0 {
		t.Fatalf("a URL at an unconfirmed origin: %+v, sent to %q", attempt, hosts)
	}
	if err := os.WriteFile(at, []byte(strings.ReplaceAll(confirmed, "https://confirmed.example", "https://CONFIRMED.example:443/prefix")), 0o600); err != nil {
		t.Fatal(err)
	}
	if attempt := r.s.witnessLoop.attempt(true); attempt.Outcome != witnessRefused || !slices.Equal(hosts, []string{"CONFIRMED.example:443"}) {
		t.Fatalf("a path at the confirmed origin: %+v, sent to %q", attempt, hosts)
	}
	plan := witnessPlan{url: "https://unconfirmed.example", key: standInWitnessKey, interval: 60}
	raw, _, _, _ := r.s.currentWitness()
	token := r.s.stampingToken(witnessSetPurpose, newStampingNonce(), plan.digest(), currentDigest(raw, true))
	r.s.witnessMu.Lock()
	_, failure := r.s.setWitness(plan, "another.synthetic.secret", token)
	r.s.witnessMu.Unlock()
	if failure != nil {
		t.Fatal(failure.message)
	}
	if attempt := r.s.witnessLoop.attempt(true); attempt.Outcome != witnessRefused || hosts[len(hosts)-1] != "unconfirmed.example" {
		t.Fatalf("a URL confirmed again: %+v, sent to %q", attempt, hosts)
	}
}

// **Finding 2: an answer that carries a credential is withheld.** A refusal
// that reflects the token, in its body or a header, and one whose body holds
// a bearer token of any kind, leave nothing of their words in Desk's log, in
// answer.json or in what the page is given: only the status, and a reason
// that is a word.
func TestWitnessAnswerCarryingACredentialIsWithheld(t *testing.T) {
	r := newConfirmedWitnessRig(t)
	for _, step := range []struct {
		name   string
		answer func(token string) *http.Response
		reason string
	}{
		{"the token reflected in the body", func(token string) *http.Response {
			body, _ := json.Marshal(map[string]string{"reason": token, "error": "rejected " + token})
			return replyOf(401, "application/json", string(body))
		}, ""},
		{"a part of the token reflected in a header", func(token string) *http.Response {
			answer := replyOf(401, "application/json", `{"error":"refused","reason":"unauthenticated"}`)
			answer.Header.Set("X-Echo", "secret")
			answer.Header.Set("X-Seen", strings.Split(token, ".")[1])
			return answer
		}, "unauthenticated"},
		{"a bearer token of another in the body", func(string) *http.Response {
			return replyOf(403, "application/json", `{"error":"try Bearer abcdefghijklmnop instead","reason":"not-registered"}`)
		}, "not-registered"},
	} {
		answeredBy(t, func(req *http.Request) (*http.Response, error) {
			return step.answer(strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")), nil
		})
		attempt := r.s.witnessLoop.attempt(true)
		page, _ := json.Marshal(r.s.withoutPathsInAttempt(attempt))
		answer := readFile(t, r.handoverPath(witnessHolderID, witnessAnswerName))
		if attempt.Outcome != witnessWithheld || attempt.Error != "" || attempt.Reason != step.reason || attempt.Problem != witnessCredentialWords {
			t.Errorf("%s: %+v", step.name, attempt)
		}
		for _, sink := range []string{r.logged.String(), answer, string(page)} {
			if strings.Contains(sink, "synthetic") || strings.Contains(sink, "abcdefghijklmnop") {
				t.Fatalf("%s: a credential reached a sink", step.name)
			}
		}
	}
}

// **Finding 3: a head or an answer Desk cannot use is moved aside, never
// written over.** Each is moved aside, as it was, under the archive rule,
// before its replacement is written; where it cannot be moved aside, nothing
// replaces it.
func TestWitnessHeadAndAnswerOutOfFormAreMovedAsideBeforeReplacement(t *testing.T) {
	r := newConfirmedWitnessRig(t)
	for _, name := range []string{headName(handoverTrail), witnessAnswerName} {
		if err := os.WriteFile(r.handoverPath(witnessHolderID, name), []byte("UNUSABLE ORIGINAL "+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	head := statementOf("checkpoint", 0, handoverTrail, 3)
	answeredBy(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/head") {
			return replyOf(200, "application/jsonl", head), nil
		}
		return replyOf(200, "application/jsonl", ""), nil
	})
	r.s.handoverMu.Lock()
	witness, _, err := r.s.readWitness()
	r.s.handoverMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	r.s.witnessReadMu.Lock()
	_, err = r.s.fetchForReading(context.Background(), witness, handoverTrail)
	r.s.witnessReadMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	r.s.handoverMu.Lock()
	r.s.keepAnswer(witness.holder.ID, witnessAttempt{At: 1, Outcome: witnessUnreached}, nil)
	r.s.handoverMu.Unlock()
	folder := r.handoverPath(witnessHolderID)
	for _, name := range []string{headName(handoverTrail), witnessAnswerName} {
		matches, _ := filepath.Glob(filepath.Join(folder, name+".set-aside-*-form"))
		if len(matches) != 1 || readFile(t, matches[0]) != "UNUSABLE ORIGINAL "+name {
			t.Errorf("%s was not moved aside as it was: %q", name, matches)
		}
	}
	if readFile(t, filepath.Join(folder, headName(handoverTrail))) != head {
		t.Error("the head was not replaced once the one before was moved aside")
	}

	// Where it cannot be moved aside (the clock is fixed, so its name in this
	// second is the one just taken), nothing replaces it.
	if err := os.WriteFile(filepath.Join(folder, headName(handoverTrail)), []byte("UNUSABLE AGAIN"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.s.witnessReadMu.Lock()
	_, err = r.s.fetchForReading(context.Background(), witness, handoverTrail)
	r.s.witnessReadMu.Unlock()
	if err == nil || readFile(t, filepath.Join(folder, headName(handoverTrail))) != "UNUSABLE AGAIN" {
		t.Errorf("a head that could not be moved aside was replaced: %v", err)
	}
}

// **Finding 4: the key's check fails closed.** An answer that only names the
// command, an error the runtime does not document after reading the key, and
// a report with no witness section are each "the key could not be checked".
func TestWitnessKeyCheckFailsClosedOnAnUndocumentedAnswer(t *testing.T) {
	for _, answer := range []string{
		`{"command":"audit verify"}`,
		`{"command":"audit verify","status":"error","diagnostics":[{"code":"JPS-UNEXPECTED-ERROR","message":"stopped before checking inputs"}]}`,
		auditValidReport,
	} {
		if failure := keyCheckAnswer([]byte(answer), nil); failure == nil || failure.message != witnessUndocumentedKey {
			t.Errorf("%.60s accepted the key: %+v", answer, failure)
		}
	}
}

// **Finding 4, through the check: a small-order key is not kept on an
// answer the runtime does not document.**
func TestWitnessProposalOfAnInvalidKeyIsRefusedOnAnUndocumentedAnswer(t *testing.T) {
	r := newConfirmedWitnessRig(t)
	r.answers(t, 0, `{"command":"audit verify"}`)
	if _, _, failure := r.s.checkProposal(context.Background(), witnessProposal{URL: "https://confirmed.example", PublicKey: strings.Repeat("0", 64)}); failure == nil || failure.message != witnessUndocumentedKey {
		t.Fatalf("the all-zero key on a command-only answer: %+v", failure)
	}
}

// **Finding 5: the countersigned coverage is the runtime's.** A witness that
// countersigned records before Desk submitted anything: the report says
// through record 3 and Desk keeps no cursor; the page derives what the
// witness countersigned from the report alone (witness.test.tsx).
func TestWitnessCountersignedCoverageWithoutACursorIsTheReports(t *testing.T) {
	r := newConfirmedWitnessRig(t)
	answeredBy(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/head") {
			return replyOf(200, "application/jsonl", statementOf("checkpoint", 0, handoverTrail, 3)), nil
		}
		return replyOf(200, "application/jsonl", ""), nil
	})
	r.answers(t, 0, witnessReport(0, 3))
	dir, refusal := r.s.auditRuntime()
	if refusal != "" {
		t.Fatal(refusal)
	}
	answer, err := r.s.auditVerify(context.Background(), dir)
	if err != nil || answer.Report == nil || answer.Report.Coverage.Countersigned == nil || answer.Report.Coverage.Countersigned.Through != 3 ||
		answer.Witness == nil || answer.Witness.Cursor != nil {
		t.Fatalf("the decision record: %v %+v", err, answer.Witness)
	}
}

// **Finding 6: the token is read only when it is sent.** No chained record,
// a head at the cursor and a retired trail read no token.
func TestWitnessTokenIsNotReadWhenNothingIsSent(t *testing.T) {
	r := newConfirmedWitnessRig(t)
	was := testHookPrivateRead
	t.Cleanup(func() { testHookPrivateRead = was })
	read := 0
	testHookPrivateRead = func(name string) error {
		if name == witnessTokenName(witnessHolderID) {
			read++
		}
		return nil
	}
	answeredBy(t, func(*http.Request) (*http.Response, error) {
		t.Error("a request reached the witness")
		return nil, io.EOF
	})
	r.chain(t, "")
	if attempt := r.s.witnessLoop.attempt(false); attempt.Outcome != witnessNothing || read != 0 {
		t.Fatalf("with no chained record: %+v, the token read %d times", attempt, read)
	}
	r.chain(t, chainOf(handoverTrail, upTo(3)...))
	r.s.handoverMu.Lock()
	err := r.s.updateCursor(witnessHolderID, handoverTrail, func(entry *witnessCursor) {
		entry.Through, entry.Index, entry.Digest = 3, 0, standInDigest(3)
	})
	r.s.handoverMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if attempt := r.s.witnessLoop.attempt(true); attempt.Outcome != witnessNothing || read != 0 {
		t.Fatalf("with the head at the cursor: %+v, the token read %d times", attempt, read)
	}
}

// **Finding 7: Retry-After in either form, from receipt.** Seconds past a
// day and an HTTP date are each held, a count past ten years held to ten
// years; the time is taken when the answer is received, so a slow answer
// waits its whole delay; and scheduled and asked-for submissions alike wait
// for it.
func TestWitnessRetryAfterIsHeldInEitherFormFromReceipt(t *testing.T) {
	received := time.Unix(handoverNow, 0)
	for value, want := range map[string]time.Time{
		"172800":                        received.Add(48 * time.Hour),
		"Wed, 14 Oct 2026 00:00:00 GMT": time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
		"999999999999":                  received.Add(witnessRetryMost),
	} {
		if got, ok := retryUntil(value, received); !ok || !got.Equal(want) {
			t.Errorf("Retry-After %q: %v %v, want %v", value, got, ok, want)
		}
	}
	r := newConfirmedWitnessRig(t)
	requests := 0
	answeredBy(t, func(*http.Request) (*http.Response, error) {
		requests++
		// A slow answer: received half a minute after it was asked for.
		r.wake.set(30 * time.Second)
		answer := replyOf(503, "application/json", `{"error":"busy","reason":"busy"}`)
		answer.Header.Set("Retry-After", "7200")
		return answer, nil
	})
	attempt := r.s.witnessLoop.attempt(true)
	if want := handoverNow + 30 + 7200; attempt.RetryUntil != int64(want) {
		t.Fatalf("the deadline is %d, want %d, from receipt", attempt.RetryUntil, want)
	}
	r.wake.set(7200*time.Second + 10*time.Second)
	if status, data := witnessCall(t, r.ts, r.ts.URL, "/api/audit/witness/send", "", map[string]any{}); status != http.StatusConflict || !strings.Contains(data, "asked for no submission before") || requests != 1 {
		t.Fatalf("Send now before the deadline: %d %s, %d requests", status, data, requests)
	}
	r.wake.at(t, 7200*time.Second+20*time.Second)
	if requests != 1 {
		t.Fatalf("a scheduled turn before the deadline sent: %d requests", requests)
	}
	r.wake.at(t, 7200*time.Second+31*time.Second)
	if requests != 2 {
		t.Fatalf("a scheduled turn after the deadline did not send: %d requests", requests)
	}
}

// **Finding 8: the answer as received, and statements of another trail,
// are kept apart.** A 200 statement of another trail is kept in the file of
// statements kept apart, never the trail's, and never given to a
// verification; answer.json holds the answer's bytes as received beside
// what Desk read of it.
func TestWitnessKeepsTheOriginalAnswerAndAnotherTrailsStatementApart(t *testing.T) {
	r := newConfirmedWitnessRig(t)
	other := statementOf("checkpoint", 0, movedTrail, 3)
	answeredBy(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			return replyOf(200, "application/jsonl", other), nil
		}
		return replyOf(404, "application/json", `{"error":"none","reason":"unknown-trail"}`), nil
	})
	attempt := r.s.witnessLoop.attempt(true)
	if attempt.Outcome != witnessKept || attempt.Problem != witnessOtherWords {
		t.Fatalf("a statement of another trail: %+v", attempt)
	}
	if got := readFile(t, r.handoverPath(witnessHolderID, witnessUnusableName)); got != other {
		t.Errorf("kept apart: %q", got)
	}
	if strings.Contains(r.kept(t, handoverTrail), movedTrail) {
		t.Error("a statement of another trail is among the trail's")
	}
	var file witnessAnswerFile
	if err := json.Unmarshal([]byte(readFile(t, r.handoverPath(witnessHolderID, witnessAnswerName))), &file); err != nil || file.Original == nil ||
		!bytes.Equal(file.Original.Body, []byte(other)) || file.Original.Status != 200 || file.Original.ContentType != "application/jsonl" {
		t.Fatalf("the answer as received: %v %+v", err, file.Original)
	}
	r.ran(t)
	readAudit(t, r.ts, "")
	if slices.ContainsFunc(r.ran(t), func(call string) bool {
		return strings.Contains(call, witnessUnusableName) || strings.Contains(call, witnessAnswerName)
	}) {
		t.Error("what is kept apart was given to a verification")
	}
}

// **The reviewer's guards, kept: the cursor moves on nothing but a
// confirmed statement for the last line sent.** A statement of another
// trail, another digest, an earlier line, a body cut short, and one the
// runtime's reading finds a witness finding in each leave it; the valid
// statement moves it.
func TestWitnessCursorGuardsAgainstEachWrongStatement(t *testing.T) {
	for _, name := range []string{"wrong-trail", "wrong-digest", "earlier-line", "truncated", "runtime-witness-finding", "valid"} {
		t.Run(name, func(t *testing.T) {
			r := newConfirmedWitnessRig(t)
			line := statementOf("checkpoint", 0, handoverTrail, 3)
			switch name {
			case "wrong-trail":
				line = strings.ReplaceAll(line, handoverTrail, strings.Repeat("f", 32))
			case "wrong-digest":
				line = strings.ReplaceAll(line, standInDigest(3), standInDigest(2))
			case "earlier-line":
				line = statementOf("checkpoint", 0, handoverTrail, 2)
			case "truncated":
				line = strings.TrimSuffix(line, "\n")
			}
			report, exit := witnessReport(0, 3), 0
			if name == "runtime-witness-finding" {
				report = strings.NewReplacer(`"status":"valid"`, `"status":"invalid"`,
					`"findings":[]`, `"findings":[{"name":"witness-signature-invalid","line":0,"detail":"signature refused"}]`,
					`"findingsTotal":0`, `"findingsTotal":1`).Replace(report)
				exit = 1
			}
			r.answers(t, exit, report)
			answeredBy(t, func(*http.Request) (*http.Response, error) { return replyOf(200, "application/jsonl", line), nil })
			attempt := r.s.witnessLoop.attempt(true)
			through := r.cursor(t, handoverTrail).Through
			if name == "valid" && through != 3 || name != "valid" && through != 0 {
				t.Fatalf("the cursor at %d: %+v", through, attempt)
			}
		})
	}
}

// **The reviewer's guard, kept: a runtime that does not answer in time
// releases every lock the reading held.**
func TestWitnessReadingThatTimesOutReleasesItsLocks(t *testing.T) {
	r := newConfirmedWitnessRig(t)
	answeredBy(t, func(*http.Request) (*http.Response, error) {
		return replyOf(200, "application/jsonl", statementOf("checkpoint", 0, handoverTrail, 3)), nil
	})
	script, err := os.ReadFile(r.bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.bin, bytes.Replace(script, []byte("'audit verify')\n"), []byte("'audit verify')\n sleep 1\n"), 1), 0o755); err != nil {
		t.Fatal(err)
	}
	was := runtimeCommandTimeout
	runtimeCommandTimeout = 100 * time.Millisecond
	t.Cleanup(func() { runtimeCommandTimeout = was })
	dir, refusal := r.s.auditRuntime()
	if refusal != "" {
		t.Fatal(refusal)
	}
	if _, err := r.s.auditVerify(context.Background(), dir); err == nil {
		t.Fatal("a runtime past its bound was answered")
	}
	for name, lock := range map[string]interface{ TryLock() bool }{"key": &r.s.keyMu, "reading": &r.s.witnessReadMu, "witness": &r.s.witnessMu, "hand-over": &r.s.handoverMu} {
		if !lock.TryLock() {
			t.Errorf("the %s lock is held", name)
			continue
		}
		lock.(interface{ Unlock() }).Unlock()
	}
}
