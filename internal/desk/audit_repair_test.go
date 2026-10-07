package desk

// Repairing a desk's trail (ADR-0010, section 4, "Repair"; question 8). A
// stand-in runtime, by absolute path, answers `audit verify` from files each
// test prepares, as the decision record's tests have it, and `audit repair`
// from a shell fragment each test sets; every answer is runtime 0.27.1's own,
// measured, with the trail's path shortened. The tests that drive the real
// runtime skip without one.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// What runtime 0.27.1 printed for `audit repair --config jpack.json --format
// json`, measured: the repair of a trail whose fourth line was cut after
// eight bytes, `{"torn":` (exit 0); its refusal where the last line is
// complete (exit 1); and its refusal for a project whose audit member says
// chain false (exit 3).
const (
	repairedAnswer  = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit repair","status":"repaired","trailPath":"/project/.desk-private/audit/evaluations.jsonl","discontinuity":{"line":5,"reason":"incomplete-last-line","damagedLine":4,"bytes":8,"digest":"sha256:952cdc0f85ab10d18a1bdccfeb6c3991e59ab424dc2ce916544aac44f3d8b45e"}}`
	repairNothing   = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit repair","status":"error","diagnostics":[{"code":"JPS-AUDIT-REPAIR-NOTHING","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"The trail's last line is complete, so there is nothing at its end to repair; jpack audit verify reports any other damage."}]}`
	repairUnchained = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit repair","status":"error","diagnostics":[{"code":"JPS-AUDIT-REPAIR-UNCHAINED","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"This project's audit member says chain false, and a repair starts a chained segment."}]}`
	// tornBytes is the cut last line the measured answers name, and
	// tornDigest the SHA-256 of its bytes, as the runtime gives it.
	tornBytes  = `{"torn":`
	tornDigest = "sha256:952cdc0f85ab10d18a1bdccfeb6c3991e59ab424dc2ce916544aac44f3d8b45e"
	// repairWriteFailed is 0.27.1's answer, exit 4, where it could not write
	// to the trail, measured over a trail made read-only.
	repairWriteFailed = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit repair","status":"error","diagnostics":[{"code":"JPS-AUDIT-WRITE","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"Audit record could not be written."}]}`
)

// repairCall is the one repair a confirmation runs.
const repairCall = "audit repair --config jpack.json --format json [JPACK_CONFIG=unset]"

// withRepair adds `audit repair` to the stand-in at rig.bin: it runs the
// shell fragment in `<calls>.repair`, set by repairingAs, and otherwise exits
// 64.
func withRepair(t *testing.T, rig *auditRig) {
	t.Helper()
	script, err := os.ReadFile(rig.bin)
	if err != nil {
		t.Fatal(err)
	}
	repair := "'audit repair')\n  if [ -e '" + rig.calls + ".repair' ]; then . '" + rig.calls + ".repair'; exit $?; fi\n  exit 64\n  ;;\n"
	script = bytes.Replace(script, []byte("'packs lock')\n"), []byte(repair+"'packs lock')\n"), 1)
	if err := os.WriteFile(rig.bin, script, 0o755); err != nil {
		t.Fatal(err)
	}
}

// repairingAs steers the stand-in's `audit repair`: it runs fragment.
func repairingAs(t *testing.T, rig *auditRig, fragment string) {
	t.Helper()
	if err := os.WriteFile(rig.calls+".repair", []byte(fragment+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// repairsAsTheRuntime is a stand-in `audit repair` that does what the
// runtime does, as far as the panel can see: from then on its `audit verify`
// answers with after, exit 0, and it prints repairedAnswer. Each run appends
// a line to `<calls>.repairs`, after waiting, where wait is not empty, that
// many seconds.
func repairsAsTheRuntime(rig *auditRig, after, wait string) string {
	fragment := ""
	if wait != "" {
		fragment = "  sleep " + wait + "\n"
	}
	return fragment +
		"  printf 'repair\\n' >> '" + rig.calls + ".repairs'\n" +
		"  while IFS= read -r line || [ -n \"$line\" ]; do printf '%s\\n' \"$line\"; done < '" + after + "' > '" + rig.answer + "'\n" +
		"  printf '0\\n' > '" + rig.answer + ".exit'\n" +
		"  printf '%s\\n' " + shellQuote(repairedAnswer)
}

// repairs is how many repairs the stand-in made.
func (rig *auditRig) repairs(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(rig.calls + ".repairs")
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "\n")
}

// tornTrail is a trail of three lines and a fourth cut after eight bytes, as
// Desk reads it; the stand-in's reports and answers speak of the same.
const tornTrail = "{\"line\":1}\n{\"line\":2}\n{\"line\":3}\n" + tornBytes

// trailOf is the trail file in folder.
func trailOf(folder string) string {
	return filepath.Join(folder, ".desk-private", "audit", "evaluations.jsonl")
}

// writeTornTrail gives folder tornTrail, in place where it has a trail.
func writeTornTrail(t *testing.T, folder string) {
	t.Helper()
	writeProject(t, folder, map[string]string{".desk-private/audit/evaluations.jsonl": tornTrail})
}

// otherToken is a token of the right form that no desk gave.
var otherToken = strings.Repeat("ab", repairTokenLength/2)

// repairRig is a startup desk over a project that keeps a trail, with the
// stand-in behind it answering the torn trail's report, and repairing it as
// the runtime does.
type repairRig struct {
	s       *Server
	ts      *httptest.Server
	rig     *auditRig
	project string
	logged  *bytes.Buffer
	// after is the report the stand-in answers once it repaired.
	after string
}

func newRepairRig(t *testing.T, versions string) *repairRig {
	t.Helper()
	t.Setenv("JPACK_CONFIG", "")
	rig := newAuditRig(t, versions)
	withRepair(t, rig)
	rig.answers(t, 1, auditInvalidReport)
	after := filepath.Join(t.TempDir(), "after.json")
	if err := os.WriteFile(after, []byte(auditSegmentedReport), 0o600); err != nil {
		t.Fatal(err)
	}
	repairingAs(t, rig, repairsAsTheRuntime(rig, after, ""))
	project := t.TempDir()
	writeProject(t, project, map[string]string{"jpack.json": auditedConfig})
	writeTornTrail(t, project)
	logged := &bytes.Buffer{}
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: rig.bin, Token: testToken, Logger: log.New(logged, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	return &repairRig{s: s, ts: ts, rig: rig, project: project, logged: logged, after: after}
}

// offer is the panel's offer of a repair on desk, which the test requires it
// to make.
func offerOn(t *testing.T, ts *httptest.Server, desk string) auditRepair {
	t.Helper()
	status, data := reviewCall(t, ts, "GET", "/api/audit/verify", desk, nil, bearer)
	var answer auditAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Repair == nil || answer.Repair.State != repairAvailable {
		t.Fatalf("the panel offers no repair: %d %s", status, data)
	}
	return *answer.Repair
}

func (r *repairRig) offer(t *testing.T) auditRepair {
	t.Helper()
	return offerOn(t, r.ts, "")
}

// repairAnswered is what a repair answered: its status, the answer, and the
// refusal's sentence, code and the runtime's words.
type repairAnswered struct {
	status      int
	answer      repairAnswer
	error, code string
	diagnostics []runtimeDiagnostic
	data        string
}

func repairOn(t *testing.T, ts *httptest.Server, desk, token string) repairAnswered {
	t.Helper()
	status, data := reviewCall(t, ts, "POST", "/api/audit/repair", desk, map[string]string{"token": token}, bearer)
	got := repairAnswered{status: status, data: string(data)}
	if status == http.StatusOK {
		if json.Unmarshal(data, &got.answer) != nil {
			t.Fatalf("the repair answered %d %s", status, data)
		}
		return got
	}
	var refusal struct {
		Error       string              `json:"error"`
		Code        string              `json:"code"`
		Diagnostics []runtimeDiagnostic `json:"diagnostics"`
	}
	_ = json.Unmarshal(data, &refusal)
	got.error, got.code, got.diagnostics = refusal.Error, refusal.Code, refusal.Diagnostics
	return got
}

func (r *repairRig) repair(t *testing.T, token string) repairAnswered {
	t.Helper()
	return repairOn(t, r.ts, "", token)
}

// lockFree fails the test where a repair left this desk's lock held, and
// then releases it, so that no later request of the test waits on it for
// ever: a held mutex may be unlocked by any goroutine.
func lockFree(t *testing.T, s *Server) {
	t.Helper()
	if !s.repairMu.TryLock() {
		t.Error("the repair's lock is still held")
	}
	s.repairMu.Unlock()
}

// **Offered where the report names incomplete-last-line, and nowhere else.**
// A torn trail's report carries the offer: the line the finding names, and a
// token. A trail intact, one segmented by a repair, one with another finding,
// the runtime's refusal to check, an older runtime, and a project that keeps
// no trail carry none, and the member is absent. The panel itself never runs
// a repair.
func TestTheRepairIsOfferedOnlyWhereTheReportNamesTheFinding(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	offer := r.offer(t)
	if offer.Line != 4 || len(offer.Token) != repairTokenLength || strings.Trim(offer.Token, "0123456789abcdef") != "" || offer.Reason != "" {
		t.Errorf("the panel offers %+v, want line 4 and a token", offer)
	}
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"an intact trail", 0, auditValidReport},
		{"a segmented trail", 0, auditSegmentedReport},
		{"another finding", 1, edited(t, auditInvalidReport, func(report map[string]any) {
			report["findings"].([]any)[0].(map[string]any)["name"] = "another-finding"
		})},
		{"the runtime's refusal to check", 4, auditNoTrailYet},
	} {
		r.rig.answers(t, tc.code, tc.body)
		status, data := reviewCall(t, r.ts, "GET", "/api/audit/verify", "", nil, bearer)
		if status != http.StatusOK || strings.Contains(string(data), `"repair"`) {
			t.Errorf("%s: the panel answered %d %s, want no offer", tc.name, status, data)
		}
	}
	if calls := r.rig.ran(t); slices.Contains(calls, repairCall) || r.rig.repairs(t) != 0 {
		t.Errorf("the panel ran %q", calls)
	}

	older := newRepairRig(t, `["1","2","3","4","5"]`)
	if status, data := reviewCall(t, older.ts, "GET", "/api/audit/verify", "", nil, bearer); status != http.StatusOK || strings.Contains(string(data), `"repair"`) {
		t.Errorf("with an older runtime the panel answered %d %s", status, data)
	}
	untrailed := newRepairRig(t, withAuditVersions)
	writeProject(t, untrailed.project, map[string]string{"jpack.json": `{"configVersion":"5","packs":{}}` + "\n"})
	if status, data := reviewCall(t, untrailed.ts, "GET", "/api/audit/verify", "", nil, bearer); status != http.StatusOK || strings.Contains(string(data), `"repair"`) {
		t.Errorf("with no trail the panel answered %d %s", status, data)
	}
}

// **The repair runs once, on the owner's confirmation, and its token does not
// confirm another.** The confirmation asks `packs schema`, checks the trail
// again with `audit verify`, and runs exactly `audit repair --config
// jpack.json --format json`, once. It answers the discontinuity the runtime
// reports, and never the trail's path; the panel then shows the trail
// segmented, and offers nothing. The same token again is refused: the trail
// it was given for is not the trail now, and nothing runs.
func TestARepairRunsOnceOnTheOwnersConfirmation(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	offer := r.offer(t)
	r.rig.ran(t)
	got := r.repair(t, offer.Token)
	want := auditDiscontinuity{Line: 5, Reason: "incomplete-last-line", DamagedLine: 4, Bytes: 8, Digest: tornDigest}
	if got.status != http.StatusOK || got.answer.State != "repaired" || got.answer.Discontinuity != want {
		t.Fatalf("the repair answered %d %s", got.status, got.data)
	}
	for _, leaked := range []string{"trailPath", "/project", r.project} {
		if strings.Contains(got.data, leaked) {
			t.Errorf("the repair's answer says %q: %s", leaked, got.data)
		}
	}
	if calls := r.rig.ran(t); !slices.Equal(calls, []string{schemaCall, verifyCall, repairCall}) {
		t.Errorf("the confirmation ran %q, want packs schema, audit verify and one audit repair", calls)
	}
	if n := r.rig.repairs(t); n != 1 {
		t.Errorf("the runtime repaired %d times", n)
	}
	lockFree(t, r.s)
	_, answer, _ := readAudit(t, r.ts, "")
	if answer.Report == nil || answer.Report.Status != "segmented" || answer.Repair != nil || len(answer.Report.Discontinuities) != 1 || answer.Report.Discontinuities[0] != want {
		t.Errorf("after the repair the panel shows %+v", answer)
	}

	r.rig.ran(t)
	again := r.repair(t, offer.Token)
	if again.status != http.StatusConflict || again.code != CodeStale || again.error != repairUsedWords {
		t.Errorf("the same token again answered %d %s", again.status, again.data)
	}
	if calls := r.rig.ran(t); calls != nil || r.rig.repairs(t) != 1 {
		t.Errorf("the same token again ran %q", calls)
	}
	lockFree(t, r.s)
}

// **The token is bound to what the panel showed.** From another desk, for a
// trail whose identity, incomplete line or size changed since, for a trail a
// repair segmented since, and of another purpose, a token is refused with one
// plain sentence, and nothing but the fresh check runs.
func TestARepairTokenIsBoundToWhatWasShown(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	offer := r.offer(t)
	refused := func(t *testing.T, ts *httptest.Server, desk, token string, rig *auditRig, s *Server) {
		t.Helper()
		rig.ran(t)
		got := repairOn(t, ts, desk, token)
		if got.status != http.StatusConflict || got.code != CodeStale || got.error != repairStaleWords {
			t.Errorf("the repair answered %d %s, want the stale refusal", got.status, got.data)
		}
		if calls := rig.ran(t); slices.Contains(calls, repairCall) || rig.repairs(t) != 0 {
			t.Errorf("a stale token ran %q", calls)
		}
		lockFree(t, s)
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"another identity", func(report map[string]any) { report["trail"] = strings.Repeat("e", 32) }},
		{"another size", func(report map[string]any) { report["bytes"] = 2870 }},
		{"another line", func(report map[string]any) { report["findings"].([]any)[0].(map[string]any)["line"] = 5 }},
		{"no identity", func(report map[string]any) { delete(report, "trail") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.rig.answers(t, 1, edited(t, auditInvalidReport, tc.change))
			refused(t, r.ts, "", offer.Token, r.rig, r.s)
		})
	}
	t.Run("a trail a repair segmented since", func(t *testing.T) {
		r.rig.answers(t, 0, auditSegmentedReport)
		refused(t, r.ts, "", offer.Token, r.rig, r.s)
	})
	t.Run("a token of another purpose", func(t *testing.T) {
		// A MAC under this desk's own key over what the report showed, for
		// another purpose than a repair.
		r.rig.answers(t, 1, auditInvalidReport)
		mac := hmac.New(sha256.New, r.s.reviewKey[:])
		mac.Write([]byte(`{"purpose":"rotate-signing-key","trail":"9a5ef41d74e7d7e003c8a34cff056351","line":4,"bytes":2864}`))
		refused(t, r.ts, "", strings.Repeat("0", repairNonceLength)+hex.EncodeToString(mac.Sum(nil)), r.rig, r.s)
	})
	t.Run("another desk", func(t *testing.T) {
		// Two desks of one Desk, over the same report: each one's token
		// confirms nothing on the other.
		rot := newRotationRig(t, "c7000000000000000000000000000001", "")
		withRepair(t, rot.rig)
		after := filepath.Join(t.TempDir(), "after.json")
		if err := os.WriteFile(after, []byte(auditSegmentedReport), 0o600); err != nil {
			t.Fatal(err)
		}
		repairingAs(t, rot.rig, repairsAsTheRuntime(rot.rig, after, ""))
		writeProject(t, rot.s.cfg.ProjectDir, map[string]string{"jpack.json": auditedConfig})
		writeTornTrail(t, rot.s.cfg.ProjectDir)
		writeTornTrail(t, rot.desk)
		rot.rig.answers(t, 1, auditInvalidReport)
		made, startup := offerOn(t, rot.ts, rot.id), offerOn(t, rot.ts, "")
		if made.Token == startup.Token {
			t.Fatal("two desks gave the same token for the same report")
		}
		refused(t, rot.ts, "", made.Token, rot.rig, rot.s)
		refused(t, rot.ts, rot.id, startup.Token, rot.rig, rot.s.desks[rot.id])
	})
}

// **The token is bound to the trail's bytes and its file** (review round 1).
// Bytes changed to others of the same length, under the same identity, line
// and size; and a trail with no chained line, so no identity, moved aside for
// a copy of the same bytes: each is the trail changed since, nothing runs,
// and the trail is as it was left.
func TestARepairTokenIsBoundToTheTrailsBytesAndFile(t *testing.T) {
	stale := func(t *testing.T, r *repairRig, token, want string) {
		t.Helper()
		r.rig.ran(t)
		got := r.repair(t, token)
		if got.status != http.StatusConflict || got.code != CodeStale || got.error != repairStaleWords {
			t.Errorf("the repair answered %d %s, want the stale refusal", got.status, got.data)
		}
		if calls := r.rig.ran(t); slices.Contains(calls, repairCall) || r.rig.repairs(t) != 0 || readFile(t, trailOf(r.project)) != want {
			t.Errorf("a token for other bytes ran %q", calls)
		}
		lockFree(t, r.s)
	}
	t.Run("bytes of the same length", func(t *testing.T) {
		r := newRepairRig(t, withAuditVersions)
		token := r.offer(t).Token
		evil := strings.Replace(tornTrail, tornBytes, `{"evil":`, 1)
		if err := os.WriteFile(trailOf(r.project), []byte(evil), 0o600); err != nil {
			t.Fatal(err)
		}
		stale(t, r, token, evil)
	})
	t.Run("a trail with no identity, moved aside for a copy", func(t *testing.T) {
		r := newRepairRig(t, withAuditVersions)
		r.rig.answers(t, 1, edited(t, auditInvalidReport, func(report map[string]any) { delete(report, "trail") }))
		token := r.offer(t).Token
		trail := trailOf(r.project)
		if err := os.Rename(trail, trail+".moved"); err != nil {
			t.Fatal(err)
		}
		writeTornTrail(t, r.project)
		stale(t, r, token, tornTrail)
	})
}

// **A token confirms one attempt** (review round 1). After the repair it
// confirmed, with the report and the trail's bytes put back as they were;
// and after the runtime's refusal, once the runtime would repair: the same
// token runs nothing, because its nonce is spent. A token whose nonce is not
// the one its MAC was made over is the trail changed since.
func TestARepairTokenConfirmsOneAttempt(t *testing.T) {
	used := func(t *testing.T, r *repairRig, token string, repairs int) {
		t.Helper()
		r.rig.ran(t)
		got := r.repair(t, token)
		if got.status != http.StatusConflict || got.code != CodeStale || got.error != repairUsedWords {
			t.Errorf("the token again answered %d %s", got.status, got.data)
		}
		if calls := r.rig.ran(t); calls != nil || r.rig.repairs(t) != repairs {
			t.Errorf("the token again ran %q", calls)
		}
		lockFree(t, r.s)
	}
	t.Run("after the repair, the trail put back", func(t *testing.T) {
		r := newRepairRig(t, withAuditVersions)
		token := r.offer(t).Token
		if got := r.repair(t, token); got.status != http.StatusOK {
			t.Fatalf("the repair answered %d %s", got.status, got.data)
		}
		lockFree(t, r.s)
		r.rig.answers(t, 1, auditInvalidReport)
		writeTornTrail(t, r.project)
		used(t, r, token, 1)
	})
	t.Run("after the runtime's refusal", func(t *testing.T) {
		r := newRepairRig(t, withAuditVersions)
		token := r.offer(t).Token
		repairingAs(t, r.rig, "  printf '%s\\n' "+shellQuote(repairWriteFailed)+"\n  exit 4")
		if got := r.repair(t, token); got.status != http.StatusConflict || got.error != repairRefusedWords {
			t.Fatalf("the refused repair answered %d %s", got.status, got.data)
		}
		lockFree(t, r.s)
		repairingAs(t, r.rig, repairsAsTheRuntime(r.rig, r.after, ""))
		used(t, r, token, 0)
	})
	t.Run("another nonce under the same MAC", func(t *testing.T) {
		r := newRepairRig(t, withAuditVersions)
		token := r.offer(t).Token
		r.rig.ran(t)
		got := r.repair(t, strings.Repeat("0", repairNonceLength)+token[repairNonceLength:])
		if got.status != http.StatusConflict || got.code != CodeStale || got.error != repairStaleWords {
			t.Errorf("another nonce answered %d %s", got.status, got.data)
		}
		if calls := r.rig.ran(t); !slices.Equal(calls, []string{schemaCall, verifyCall}) || r.rig.repairs(t) != 0 {
			t.Errorf("another nonce ran %q", calls)
		}
		lockFree(t, r.s)
	})
}

// **Offered only on a chained trail** (review round 1). Where jpack.json's
// audit member says chain false, the runtime repairs nothing and refuses no
// deciding run: the panel says why it offers no repair, with no token and no
// promise, and a confirmation given while the trail was chained runs nothing
// once it is not. An audit member that says chain true, or says nothing of
// it, is chained, as the runtime reads it.
func TestARepairIsOfferedOnlyOnAChainedTrail(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	token := r.offer(t).Token
	writeProject(t, r.project, map[string]string{"jpack.json": `{"configVersion":"6","audit":{"dir":".desk-private/audit","chain":false},"packs":{}}` + "\n"})
	_, answer, _ := readAudit(t, r.ts, "")
	if want := (auditRepair{State: repairUnavailable, Line: 4, Reason: repairUnchainedWords}); answer.Repair == nil || *answer.Repair != want {
		t.Errorf("on a trail that is not chained the panel says %+v, want %+v", answer.Repair, want)
	}
	r.rig.ran(t)
	got := r.repair(t, token)
	if got.status != http.StatusConflict || got.error != repairNotChainedWords {
		t.Errorf("the confirmation answered %d %s", got.status, got.data)
	}
	if calls := r.rig.ran(t); !slices.Equal(calls, []string{schemaCall}) || r.rig.repairs(t) != 0 {
		t.Errorf("the confirmation ran %q", calls)
	}
	lockFree(t, r.s)
	for _, config := range []string{`{"configVersion":"6","audit":{"dir":".desk-private/audit","chain":true},"packs":{}}` + "\n", auditedConfig} {
		writeProject(t, r.project, map[string]string{"jpack.json": config})
		if offer := r.offer(t); offer.Line != 4 {
			t.Errorf("with %s the panel offers %+v", config, offer)
		}
	}
}

// **A repair the runtime reports of other damaged bytes is not the one
// confirmed** (review round 1). Another writer can append between Desk's
// reading and the runtime's: where the line the runtime kept as damaged, its
// length or its digest is not what Desk read, Desk says so in one sentence,
// and never that the trail was repaired.
func TestARepairOfOtherBytesIsNotTheOneConfirmed(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"another line", func(d map[string]any) { d["damagedLine"], d["line"] = 5, 6 }},
		{"another length", func(d map[string]any) { d["bytes"] = 9 }},
		{"another digest", func(d map[string]any) { d["digest"] = "sha256:" + strings.Repeat("0", 64) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.rig.answers(t, 1, auditInvalidReport)
			answer := edited(t, repairedAnswer, func(a map[string]any) { tc.change(a["discontinuity"].(map[string]any)) })
			repairingAs(t, r.rig, "  printf '%s\\n' "+shellQuote(answer))
			got := r.repair(t, r.offer(t).Token)
			if got.status != http.StatusConflict || got.code != CodeBadRequest || got.error != repairDiffersWords || strings.Contains(got.data, "repaired\"") {
				t.Errorf("the repair answered %d %s", got.status, got.data)
			}
			lockFree(t, r.s)
		})
	}
}

// **No repair over a trail Desk cannot read** (review round 1). A repair is
// bound to the bytes Desk reads; where it cannot read them, as where the
// trail has a second name, a hard link, the panel says why and offers none,
// and a confirmation given before runs nothing.
func TestARepairIsNotOfferedOverATrailDeskCannotRead(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	token := r.offer(t).Token
	if err := os.Link(trailOf(r.project), filepath.Join(r.project, "copy.jsonl")); err != nil {
		t.Fatal(err)
	}
	const problem = "evaluations.jsonl in this project's audit directory, or the trail it is read beside, has another name as well, a hard link, so Desk does not hand it over"
	_, answer, _ := readAudit(t, r.ts, "")
	if want := (auditRepair{State: repairUnavailable, Line: 4, Reason: "Desk binds a repair to the trail's bytes as it reads them, and offers none now: " + problem + "."}); answer.Repair == nil || *answer.Repair != want {
		t.Errorf("the panel says %+v, want %+v", answer.Repair, want)
	}
	r.rig.ran(t)
	got := r.repair(t, token)
	if got.status != http.StatusConflict || got.error != "Nothing was repaired: Desk could not read the trail again: "+problem+"." || r.rig.repairs(t) != 0 {
		t.Errorf("the confirmation answered %d %s", got.status, got.data)
	}
	lockFree(t, r.s)
}

// **The runtime's refusal is passed on in its own words, with no path.**
// Desk says the runtime did not report a repair, and gives each of its
// diagnostics, code and message, as it said them: where one names the
// trail's path, the path is replaced as the panel replaces it, and Desk's own
// log keeps it whole.
func TestARepairRefusalIsPassedOnInTheRuntimesWords(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	trail := filepath.Join(r.project, ".desk-private", "audit", "evaluations.jsonl")
	naming := `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit repair","status":"error","diagnostics":[{"code":"JPS-AUDIT-TRAIL-READ","message":` +
		jsonString("The project's trail "+trail+" could not be opened.") + `},{"code":"JPS-AUDIT-REPAIR-NOTHING","message":"The trail's last line is complete, so there is nothing at its end to repair; jpack audit verify reports any other damage."}]}`
	for _, tc := range []struct {
		name   string
		answer string
		code   int
		want   []runtimeDiagnostic
	}{
		{"nothing to repair", repairNothing, 1, []runtimeDiagnostic{{"JPS-AUDIT-REPAIR-NOTHING", "The trail's last line is complete, so there is nothing at its end to repair; jpack audit verify reports any other damage."}}},
		{"chain false", repairUnchained, 3, []runtimeDiagnostic{{"JPS-AUDIT-REPAIR-UNCHAINED", "This project's audit member says chain false, and a repair starts a chained segment."}}},
		{"a refusal that names the trail", naming, 4, []runtimeDiagnostic{
			{"JPS-AUDIT-TRAIL-READ", "The project's trail …/evaluations.jsonl could not be opened."},
			{"JPS-AUDIT-REPAIR-NOTHING", "The trail's last line is complete, so there is nothing at its end to repair; jpack audit verify reports any other damage."},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.rig.answers(t, 1, auditInvalidReport)
			repairingAs(t, r.rig, "  printf '%s\\n' "+shellQuote(tc.answer)+"\n  exit "+strconv.Itoa(tc.code))
			got := r.repair(t, r.offer(t).Token)
			if got.status != http.StatusConflict || got.code != CodeBadRequest || got.error != repairRefusedWords || !slices.Equal(got.diagnostics, tc.want) {
				t.Errorf("the refusal answered %d %s", got.status, got.data)
			}
			if strings.Contains(got.data, r.project) {
				t.Errorf("the refusal names the project's folder: %s", got.data)
			}
			lockFree(t, r.s)
		})
	}
	if !strings.Contains(r.logged.String(), trail) {
		t.Errorf("Desk's log does not keep the runtime's words whole: %s", r.logged.String())
	}
}

// **An answer the runtime does not document is not a repair.** Exit 0 with
// another status, another command, another outputVersion, no discontinuity,
// or one with a member missing, out of order or not in the runtime's form;
// a refusal with no words; and an answer that is not JSON: each says that
// Desk cannot say whether the trail was repaired, and that the decision
// record shows what it holds now.
func TestAnAnswerTheRepairDoesNotDocumentIsNotARepair(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	changed := func(change func(map[string]any)) string {
		return edited(t, repairedAnswer, change)
	}
	discontinuity := func(change func(map[string]any)) string {
		return changed(func(answer map[string]any) { change(answer["discontinuity"].(map[string]any)) })
	}
	for _, tc := range []struct {
		name, answer string
		code         int
	}{
		{"another status", changed(func(a map[string]any) { a["status"] = "done" }), 0},
		{"another command", changed(func(a map[string]any) { a["command"] = "audit verify" }), 0},
		{"another outputVersion", changed(func(a map[string]any) { a["outputVersion"] = "3" }), 0},
		{"no outputVersion", changed(func(a map[string]any) { delete(a, "outputVersion") }), 0},
		{"no discontinuity", changed(func(a map[string]any) { delete(a, "discontinuity") }), 0},
		{"no digest", discontinuity(func(d map[string]any) { delete(d, "digest") }), 0},
		{"a digest of another form", discontinuity(func(d map[string]any) { d["digest"] = "952cdc" }), 0},
		{"no damaged line", discontinuity(func(d map[string]any) { d["damagedLine"] = 0 }), 0},
		{"a record before its damaged line", discontinuity(func(d map[string]any) { d["line"] = 4 }), 0},
		{"no damaged bytes", discontinuity(func(d map[string]any) { d["bytes"] = 0 }), 0},
		{"no reason", discontinuity(func(d map[string]any) { d["reason"] = "" }), 0},
		{"a repair with a failed exit", repairedAnswer, 1},
		{"a refusal with no words", `{"outputVersion":"2","command":"audit repair","status":"error","diagnostics":[]}`, 1},
		{"a refusal of another outputVersion", strings.Replace(repairNothing, `"outputVersion":"2"`, `"outputVersion":"3"`, 1), 1},
		{"not JSON", "repaired", 0},
		{"nothing", "", 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.rig.answers(t, 1, auditInvalidReport)
			repairingAs(t, r.rig, "  printf '%s\\n' "+shellQuote(tc.answer)+"\n  exit "+strconv.Itoa(tc.code))
			got := r.repair(t, r.offer(t).Token)
			if got.status != http.StatusInternalServerError || got.code != CodeInternal || got.error != repairUndocumented || got.diagnostics != nil {
				t.Errorf("the repair answered %d %s", got.status, got.data)
			}
			lockFree(t, r.s)
		})
	}
}

// **The repair's answer names no path.** Where the runtime's reason for the
// discontinuity names the trail, it is replaced as the panel replaces it, and
// the trail's path the runtime gives beside it is never passed on.
func TestARepairAnswerNamesNoPath(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	trail := filepath.Join(r.project, ".desk-private", "audit", "evaluations.jsonl")
	answer := edited(t, repairedAnswer, func(a map[string]any) {
		a["trailPath"] = trail
		a["discontinuity"].(map[string]any)["reason"] = "incomplete-last-line of " + trail
	})
	repairingAs(t, r.rig, "  printf '%s\\n' "+shellQuote(answer))
	got := r.repair(t, r.offer(t).Token)
	if got.status != http.StatusOK || got.answer.Discontinuity.Reason != "incomplete-last-line of …/evaluations.jsonl" || strings.Contains(got.data, r.project) {
		t.Errorf("the repair answered %d %s", got.status, got.data)
	}
	lockFree(t, r.s)
}

// **A repair that does not finish within the runtime's bound is said so**:
// Desk cannot say whether the trail was repaired, and says that the decision
// record shows what it holds now.
func TestARepairThatDoesNotFinishIsSaid(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	token := r.offer(t).Token
	repairingAs(t, r.rig, "  sleep 3")
	defer func(bound time.Duration) { runtimeCommandTimeout = bound }(runtimeCommandTimeout)
	runtimeCommandTimeout = time.Second
	got := r.repair(t, token)
	want := "The runtime's audit repair did not finish as asked: the runtime did not finish audit repair --config jpack.json --format json within 1s. Desk cannot say whether it repaired the trail. Check the decision record again: it shows what the trail holds now."
	if got.status != http.StatusInternalServerError || got.error != want {
		t.Errorf("the repair answered %d %s", got.status, got.data)
	}
	lockFree(t, r.s)
}

// **Nothing runs on a request that is not the owner's confirmation**: one a
// browser marks cross-site, one that is not JSON, one with no token or a
// member besides it, and one without the desk's bearer, each with the
// panel's own token.
func TestARepairIsAskedForInOneWay(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	token := r.offer(t).Token
	r.rig.ran(t)
	for _, tc := range []struct {
		name     string
		status   int
		decorate func(*http.Request)
		body     any
	}{
		{"cross-site", http.StatusForbidden, func(req *http.Request) { req.Header.Set("Sec-Fetch-Site", "cross-site") }, map[string]string{"token": token}},
		{"not JSON", http.StatusUnsupportedMediaType, func(req *http.Request) { req.Header.Set("Content-Type", "text/plain") }, map[string]string{"token": token}},
		{"a short token", http.StatusBadRequest, func(*http.Request) {}, map[string]string{"token": token[1:]}},
		{"a member besides the token", http.StatusBadRequest, func(*http.Request) {}, map[string]string{"token": token, "line": "4"}},
		{"no bearer", http.StatusUnauthorized, func(req *http.Request) { req.Header.Del("Authorization") }, map[string]string{"token": token}},
	} {
		status, data := reviewCall(t, r.ts, "POST", "/api/audit/repair", "", tc.body, bearer, tc.decorate)
		if status != tc.status {
			t.Errorf("%s answered %d %s, want %d", tc.name, status, data, tc.status)
		}
	}
	if calls := r.rig.ran(t); calls != nil || r.rig.repairs(t) != 0 {
		t.Errorf("a refused request ran %q", calls)
	}
	if status, data := reviewCall(t, r.ts, "GET", "/api/audit/repair", "", nil, bearer); status == http.StatusOK || r.rig.repairs(t) != 0 {
		t.Errorf("a GET answered %d %s", status, data)
	}

	// **The bound, on both sides of it.** A confirmation one byte past 4 KiB
	// is refused before it is read, though every byte past the token is JSON's
	// own white space; one at the bound is read. The bound is the test's own
	// number: repairConfirmLimit would follow a change to itself.
	const bound = 4 << 10
	confirmation := `{"token":"` + token + `"}`
	padded := func(size int) int {
		t.Helper()
		request, _ := http.NewRequest("POST", r.ts.URL+"/api/audit/repair", strings.NewReader(confirmation+strings.Repeat(" ", size-len(confirmation))))
		request.Header.Set("Content-Type", "application/json")
		bearer(request)
		response, err := r.ts.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if status := padded(bound + 1); status != http.StatusBadRequest || r.rig.repairs(t) != 0 {
		t.Errorf("a confirmation past the bound answered %d, and the runtime repaired %d times", status, r.rig.repairs(t))
	}
	lockFree(t, r.s)
	if status := padded(bound); status != http.StatusOK || r.rig.repairs(t) != 1 {
		t.Errorf("a confirmation at the bound answered %d, and the runtime repaired %d times", status, r.rig.repairs(t))
	}
	lockFree(t, r.s)
}

// **Nothing is repaired where the runtime has no audit commands, or the
// project keeps no trail**, and Desk says which in one sentence, having run
// nothing but `packs schema`.
func TestARepairIsNotRunWithoutTheCommandOrATrail(t *testing.T) {
	token := otherToken
	older := newRepairRig(t, `["1","2","3","4","5"]`)
	got := older.repair(t, token)
	if got.status != http.StatusConflict || got.error != "Nothing was repaired: the runtime this Desk runs (jpack 0.0.0-stand-in) does not read configVersion 6 and has no audit repair. A runtime of 0.26.0 or later has it." {
		t.Errorf("with an older runtime the repair answered %d %s", got.status, got.data)
	}
	if calls := older.rig.ran(t); !slices.Equal(calls, []string{schemaCall}) {
		t.Errorf("with an older runtime the repair ran %q", calls)
	}
	untrailed := newRepairRig(t, withAuditVersions)
	writeProject(t, untrailed.project, map[string]string{"jpack.json": `{"configVersion":"5","packs":{}}` + "\n"})
	got = untrailed.repair(t, token)
	if got.status != http.StatusConflict || got.error != repairNoTrailWords {
		t.Errorf("with no trail the repair answered %d %s", got.status, got.data)
	}
	if calls := untrailed.rig.ran(t); !slices.Equal(calls, []string{schemaCall}) {
		t.Errorf("with no trail the repair ran %q", calls)
	}
}

// **A runtime that does not check the trail again repairs nothing.** Its
// refusal to check is said, in its words with no path, and is never taken
// for the trail the panel showed.
func TestARepairIsNotRunWhereTheTrailIsNotCheckedAgain(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	token := r.offer(t).Token
	r.rig.answers(t, 4, auditNoTrailYet)
	r.rig.ran(t)
	got := r.repair(t, token)
	want := []runtimeDiagnostic{{"JPS-AUDIT-TRAIL-READ", "The project's trail …/evaluations.jsonl does not exist yet: no record has been written."}}
	if got.status != http.StatusConflict || got.error != repairUncheckedWords || !slices.Equal(got.diagnostics, want) {
		t.Errorf("the repair answered %d %s", got.status, got.data)
	}
	if calls := r.rig.ran(t); !slices.Equal(calls, []string{schemaCall, verifyCall}) || r.rig.repairs(t) != 0 {
		t.Errorf("the repair ran %q", calls)
	}
	lockFree(t, r.s)
}

// **Desk's own sentence names no path either.** What the runtime says of
// itself reaches Desk's sentences too: a version that names a folder is
// replaced as any path is.
func TestARepairSaysNoPathOfItsOwn(t *testing.T) {
	r := newRepairRig(t, `["1","2","3","4","5"]`)
	secret := filepath.Join(t.TempDir(), "SECRET-FOLDER")
	script, err := os.ReadFile(r.rig.bin)
	if err != nil {
		t.Fatal(err)
	}
	script = bytes.Replace(script, []byte(`"version":"0.0.0-stand-in"`), []byte(`"version":"0.25.0 (`+secret+`)"`), 1)
	if err := os.WriteFile(r.rig.bin, script, 0o755); err != nil {
		t.Fatal(err)
	}
	got := r.repair(t, otherToken)
	if got.status != http.StatusConflict || strings.Contains(got.data, "SECRET") || !strings.Contains(got.error, "(jpack 0.25.0 (…))") {
		t.Errorf("the repair answered %d %s", got.status, got.data)
	}
}

// **A request that goes away stops no repair.** The owner's page closed
// while the runtime repairs would otherwise kill it, and a repair cut short
// can leave a discontinuity record whose own write did not complete, which no
// repair mends. The runtime finishes, and the lock is released after it.
func TestARepairIsNotStoppedByARequestThatGoesAway(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	after := filepath.Join(t.TempDir(), "after.json")
	if err := os.WriteFile(after, []byte(auditSegmentedReport), 0o600); err != nil {
		t.Fatal(err)
	}
	repairingAs(t, r.rig, repairsAsTheRuntime(r.rig, after, "1"))
	token := r.offer(t).Token
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "POST", r.ts.URL+"/api/audit/repair", strings.NewReader(`{"token":"`+token+`"}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	if response, err := r.ts.Client().Do(request); err == nil {
		response.Body.Close()
		t.Fatal("the request was answered before it went away")
	}
	deadline := time.Now().Add(10 * time.Second)
	for r.rig.repairs(t) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := r.rig.repairs(t); n != 1 {
		t.Fatalf("the runtime finished %d repairs after the request went away", n)
	}
	for !r.s.repairMu.TryLock() {
		if time.Now().After(deadline) {
			t.Fatal("the repair's lock is still held")
		}
		time.Sleep(50 * time.Millisecond)
	}
	r.s.repairMu.Unlock()
}

// **Two confirmations at once run one repair.** The second waits for the
// first, and then finds the trail changed: it runs nothing.
func TestTwoConfirmationsAtOnceRunOneRepair(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	after := filepath.Join(t.TempDir(), "after.json")
	if err := os.WriteFile(after, []byte(auditSegmentedReport), 0o600); err != nil {
		t.Fatal(err)
	}
	repairingAs(t, r.rig, repairsAsTheRuntime(r.rig, after, "1"))
	token := r.offer(t).Token
	var wg sync.WaitGroup
	statuses := make([]int, 2)
	for i := range statuses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i], _ = reviewCall(t, r.ts, "POST", "/api/audit/repair", "", map[string]string{"token": token}, bearer)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		// A lock the first kept would hold the second for ever.
		lockFree(t, r.s)
		<-done
	}
	slices.Sort(statuses)
	if n := r.rig.repairs(t); n != 1 || !slices.Equal(statuses, []int{http.StatusOK, http.StatusConflict}) {
		t.Errorf("two confirmations at once ran %d repairs and answered %v", n, statuses)
	}
	lockFree(t, r.s)
}

// **The Jobs record offers no repair**: the chain of runs is Runner's, and a
// report of it that names an incomplete last line carries no offer, and runs
// nothing.
func TestTheJobsRecordOffersNoRepair(t *testing.T) {
	rig := newJobsRig(t)
	rig.jobsAnswers(t, 1, edited(t, jobsVerified, func(report map[string]any) {
		report["status"] = "invalid"
		report["findings"] = []any{map[string]any{"name": incompleteLastLine, "line": 4, "detail": "the trail ends in 8 bytes with no newline: a write that did not complete"}}
		report["findingsTotal"] = 1
		report["establishes"] = []any{}
	}))
	rig.ran(t)
	status, answer, data := readJobs(t, rig.ts, "")
	if status != http.StatusOK || answer.Report == nil || len(answer.Report.Findings) != 1 || answer.Report.Findings[0].Name != incompleteLastLine {
		t.Fatalf("the Jobs record answered %d %s", status, data)
	}
	if answer.Repair != nil || bytes.Contains(data, []byte(`"repair"`)) {
		t.Errorf("the Jobs record offers a repair: %s", data)
	}
	if calls := withoutEnv(rig.ran(t)); slices.ContainsFunc(calls, func(call string) bool { return strings.HasPrefix(call, "audit repair") }) {
		t.Errorf("the Jobs record ran %q", calls)
	}
}

/* With the runtime ----------------------------------------------------------- */

// decidingRun is one deciding run of the pack alpha in folder, by the runtime
// at bin: its exit, and what it printed.
func decidingRun(t *testing.T, bin, folder string) (int, []byte) {
	t.Helper()
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "experimental", "evaluate", "--config", "jpack.json", "--pack-id", "alpha", "--facts", facts, "--format", "json")
	cmd.Dir = folder
	cmd.Env = append(os.Environ(), "JPACK_CONFIG=")
	out, err := cmd.Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, out
	case errors.As(err, &exit):
		return exit.ExitCode(), out
	}
	t.Fatal(err)
	return 0, nil
}

// cutTrail appends a last line with no newline to folder's trail, as a write
// that did not complete leaves one.
func cutTrail(t *testing.T, folder string) {
	t.Helper()
	trail, err := os.OpenFile(filepath.Join(folder, ".desk-private", "audit", "evaluations.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer trail.Close()
	if _, err := trail.WriteString(tornBytes); err != nil {
		t.Fatal(err)
	}
}

// **With the runtime: the repair, end to end.** On a desk Desk made, two
// deciding runs are recorded and the trail's last line is cut. The panel
// names incomplete-last-line and offers the repair; a deciding run is refused,
// exit 4, in the runtime's words. The owner's confirmation repairs it: the
// runtime reports the discontinuity, naming the damaged line, its bytes and
// its digest. The panel then shows the trail segmented, its two segments, the
// discontinuity, and the runtime's sentence that the history is not intact
// across it, and offers nothing; the same token again runs nothing; and a
// deciding run is accepted again, after the discontinuity.
func TestTheRepairWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	s, ts, _ := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	if !slices.Contains(mustSchema(t, bin, row.Folder), "6") {
		t.Skip("this runtime has no audit commands")
	}
	config := strings.Replace(gatedConfigFor(t, s, row), `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": reviewPack, "jpack.json": config})
	jpackIn(t, bin, row.Folder, "packs", "lock", "--config", "jpack.json", "--format", "json")
	for range 2 {
		if code, out := decidingRun(t, bin, row.Folder); code != 0 {
			t.Fatalf("a deciding run exited %d: %s", code, out)
		}
	}
	cutTrail(t, row.Folder)

	_, answer, _ := readAudit(t, ts, row.ID)
	if answer.Report == nil || answer.Report.Status != "invalid" || len(answer.Report.Findings) != 1 || answer.Report.Findings[0].Name != incompleteLastLine ||
		answer.Report.Findings[0].Line != 3 || answer.Repair == nil || answer.Repair.Line != 3 {
		t.Fatalf("after a cut the panel shows %+v", answer)
	}
	code, out := decidingRun(t, bin, row.Folder)
	if code != 4 || !strings.Contains(string(out), "the audit trail's last line is incomplete") {
		t.Errorf("a deciding run on the cut trail exited %d: %s", code, out)
	}

	// Bytes of the same length in place of the cut ones: the token the panel
	// gave is for the trail as it was, and nothing is repaired (review round 1).
	trailPath := trailOf(row.Folder)
	cut := readFile(t, trailPath)
	evil := strings.TrimSuffix(cut, tornBytes) + `{"evil":`
	if err := os.WriteFile(trailPath, []byte(evil), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := repairOn(t, ts, row.ID, answer.Repair.Token); got.status != http.StatusConflict || got.error != repairStaleWords || readFile(t, trailPath) != evil {
		t.Errorf("a token for other bytes of the same length answered %d %s", got.status, got.data)
	}
	lockFree(t, s.desks[row.ID])
	if err := os.WriteFile(trailPath, []byte(cut), 0o600); err != nil {
		t.Fatal(err)
	}
	_, answer, _ = readAudit(t, ts, row.ID)
	if answer.Repair == nil || answer.Repair.State != repairAvailable {
		t.Fatalf("with the cut bytes back the panel shows %+v", answer)
	}

	got := repairOn(t, ts, row.ID, answer.Repair.Token)
	lockFree(t, s.desks[row.ID])
	digest := sha256.Sum256([]byte(tornBytes))
	want := auditDiscontinuity{Line: 4, Reason: incompleteLastLine, DamagedLine: 3, Bytes: int64(len(tornBytes)), Digest: "sha256:" + hex.EncodeToString(digest[:])}
	if got.status != http.StatusOK || got.answer.Discontinuity != want || strings.Contains(got.data, row.Folder) {
		t.Fatalf("the repair answered %d %s, want %+v", got.status, got.data, want)
	}
	repaired := readFile(t, trailPath)
	if !strings.Contains(repaired, "\n"+tornBytes+"\n") {
		t.Errorf("the damaged bytes are not kept as a line of their own: %q", repaired)
	}

	_, after, _ := readAudit(t, ts, row.ID)
	report := after.Report
	if report == nil || report.Status != "segmented" || after.Repair != nil || len(report.Findings) != 0 ||
		!slices.Equal(report.Segments, []auditSegment{{1, 2}, {4, 4}}) || !slices.Equal(report.Discontinuities, []auditDiscontinuity{want}) {
		t.Fatalf("after the repair the panel shows %+v", after)
	}
	const notIntact = "That the history is intact across a discontinuity: a repair keeps the damaged line in place and links over it, so what the damaged line held is not part of any segment."
	if !slices.Contains(report.DoesNotEstablish, notIntact) {
		t.Errorf("the panel does not pass on the runtime's sentence on the discontinuity: %q", report.DoesNotEstablish)
	}

	// The same token again, even with the trail's bytes put back as they
	// were before the repair, runs nothing: it confirmed one attempt (review
	// round 1).
	if err := os.WriteFile(trailPath, []byte(cut), 0o600); err != nil {
		t.Fatal(err)
	}
	again := repairOn(t, ts, row.ID, answer.Repair.Token)
	lockFree(t, s.desks[row.ID])
	if again.status != http.StatusConflict || again.error != repairUsedWords || readFile(t, trailPath) != cut {
		t.Errorf("the same token again answered %d %s", again.status, again.data)
	}
	if err := os.WriteFile(trailPath, []byte(repaired), 0o600); err != nil {
		t.Fatal(err)
	}

	if code, out := decidingRun(t, bin, row.Folder); code != 0 {
		t.Fatalf("a deciding run after the repair exited %d: %s", code, out)
	}
	if _, last, _ := readAudit(t, ts, row.ID); last.Report == nil || last.Report.Status != "segmented" || last.Report.Lines != 5 ||
		!slices.Equal(last.Report.Segments, []auditSegment{{1, 2}, {4, 5}}) {
		t.Errorf("after a deciding run the panel shows %+v", last)
	}
}

// **With the runtime: no path, in a repair or in its refusal.** The project
// Desk was started on is in a folder whose name holds a space, a tab and a
// line separator. The runtime names its trail by that path in its answer to a
// repair, and Desk's answer does not; its refusal, for a project whose audit
// member says chain false, is passed on in its words.
func TestTheRepairQuotesNoPathWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv(runtimeSigningKeyEnv, "")
	project := filepath.Join(t.TempDir(), "Top SECRET\t(desk) PATH")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(mustSchema(t, bin, project), "6") {
		t.Skip("this runtime has no audit commands")
	}
	chained := `{"configVersion":"6","audit":{"dir":".desk-private/audit"},"packs":{"alpha":{"path":"packs/a.json"}}}` + "\n"
	writeProject(t, project, map[string]string{"packs/a.json": reviewPack, "jpack.json": chained})
	jpackIn(t, bin, project, "packs", "lock", "--config", "jpack.json", "--format", "json")
	if code, out := decidingRun(t, bin, project); code != 0 {
		t.Fatalf("a deciding run exited %d: %s", code, out)
	}
	cutTrail(t, project)
	logged := &bytes.Buffer{}
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: bin, Token: testToken, Logger: log.New(logged, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	leaks := func(t *testing.T, data string) {
		t.Helper()
		for _, leaked := range []string{"SECRET", "PATH", "(desk)", "/proc/self", project, "trailPath"} {
			if strings.Contains(data, leaked) {
				t.Errorf("Desk says %q: %s", leaked, data)
			}
		}
	}

	got := repairOn(t, ts, "", offerOn(t, ts, "").Token)
	lockFree(t, s)
	if got.status != http.StatusOK || got.answer.Discontinuity.DamagedLine != 2 {
		t.Fatalf("the repair answered %d %s", got.status, got.data)
	}
	leaks(t, got.data)

	// The runtime's refusal, in its words: a trail it cannot write to.
	cutTrail(t, project)
	if err := os.Chmod(trailOf(project), 0o400); err != nil {
		t.Fatal(err)
	}
	refused := repairOn(t, ts, "", offerOn(t, ts, "").Token)
	lockFree(t, s)
	want := []runtimeDiagnostic{{"JPS-AUDIT-WRITE", "Audit record could not be written."}}
	if refused.status != http.StatusConflict || refused.error != repairRefusedWords || !slices.Equal(refused.diagnostics, want) {
		t.Errorf("the refusal answered %d %s", refused.status, refused.data)
	}
	leaks(t, refused.data)

	// Chain false: no offer, and why (review round 1).
	writeProject(t, project, map[string]string{"jpack.json": strings.Replace(chained, `"dir":".desk-private/audit"`, `"dir":".desk-private/audit","chain":false`, 1)})
	status, data := reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer)
	var answer auditAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Repair == nil || *answer.Repair != (auditRepair{State: repairUnavailable, Line: answer.Repair.Line, Reason: repairUnchainedWords}) {
		t.Errorf("with chain false the panel answered %d %s", status, data)
	}
	leaks(t, string(data))
}

// mustSchema is the configuration versions the runtime at bin reads.
func mustSchema(t *testing.T, bin, folder string) []string {
	t.Helper()
	var schema struct {
		Supported []string `json:"supportedConfigVersions"`
	}
	if err := json.Unmarshal(jpackIn(t, bin, folder, "packs", "schema", "--format", "json"), &schema); err != nil {
		t.Fatal(err)
	}
	return schema.Supported
}
