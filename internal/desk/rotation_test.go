package desk

// Rotating a desk's signing key (ADR-0010, section 1, "Rotating it"), and
// finishing or undoing a rotation a stop cut short. A stand-in runtime, by
// absolute path, generates, reads and rotates keys as runtime 0.27.1 does,
// telling each key by its seed, and appends its key-rotation line to the
// desk's sidecar in the runtime's own form; every line, list and refusal the
// tests expect is spelled out here, from what 0.27.1 printed. The test that
// drives the real runtime skips without one.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// A third key, as runtime 0.27.1 printed it for a seed it generated; its
// keyId checked outside this code, as the first two were.
const (
	thirdPublicKey = "b4ea56addfb46905a3fc5be6a1ee1d41f89d516f9a31713bfbde719aa5ea4223"
	thirdKeyID     = "c25f6d97011c4372b3bf49dc44101a3f"
)

// The seeds the stand-in tells the second and third keys by: any 64
// hexadecimal characters, since no test derives a key from one.
const (
	secondSeed = "2222222222222222222222222222222222222222222222222222222222222222"
	thirdSeed  = "3333333333333333333333333333333333333333333333333333333333333333"
)

// fixtureTrail is a trail id in the runtime's form, for the sidecar lines
// the tests write and the stand-in appends.
const fixtureTrail = "322f164eb0705594cf781190d417112e"

var (
	fixtureSignature = strings.Repeat("5a", 64)
	fixtureRecord    = "sha256:" + strings.Repeat("7c", 32)
	key1             = deskPublicKey{standInPublicKey, standInKeyID, 0}
)

// recordLine is a record's signature line in the sidecar, as runtime 0.27.1
// writes one (measured): its members in code-point order, no spaces.
func recordLine(keyID string, sequence int) string {
	return `{"keyId":"` + keyID + `","kind":"record-signature","record":"` + fixtureRecord + `","sequence":` + strconv.Itoa(sequence) +
		`,"sidecarVersion":"1","signature":"` + fixtureSignature + `","trail":"` + fixtureTrail + `"}` + "\n"
}

// rotationLine is a key-rotation line in the sidecar, as runtime 0.27.1
// writes one (measured).
func rotationLine(at int, fromKeyID, next string) string {
	return `{"at":` + strconv.Itoa(at) + `,"keyId":"` + fromKeyID + `","kind":"key-rotation","next":"` + next +
		`","sidecarVersion":"1","signature":"` + fixtureSignature + `","trail":"` + fixtureTrail + `"}` + "\n"
}

// What runtime 0.27.1 printed, measured, when `audit key rotate` refused:
// before the trail had a chained record (exit 1), while its last line was
// incomplete (exit 4), and with a key that was not the key in force (exit 1).
const (
	rotateNoTrail    = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit key rotate","status":"error","diagnostics":[{"code":"JPS-AUDIT-KEY-NO-TRAIL","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"The trail has no chained record yet, so there is no trail to rotate the key of; the first record is signed with whichever key the project names."}]}`
	rotateIncomplete = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit key rotate","status":"error","diagnostics":[{"code":"JPS-AUDIT-WRITE","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"Audit record could not be written: the audit trail's last line is incomplete, and no record is chained after an incomplete line."}]}`
	rotateNotInForce = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit key rotate","status":"error","diagnostics":[{"code":"JPS-AUDIT-KEY-NOT-IN-FORCE","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"The project's signing key is not the key in force in the signature sidecar, so it cannot hand signing over; only the key in force can."}]}`
	// publicRefused is a refusal of `audit key public`, in the runtime's
	// words, without the path it names.
	publicRefused = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit key public","status":"invalid","diagnostics":[{"code":"JPS-AUDIT-KEY-REFUSED","message":"The key is refused: the signing key is not 64 hexadecimal characters."}]}`
)

// keyOfSeed is a shell fragment that sets pub and id to the key of the seed
// in $seed, or empties both for a seed it does not know.
var keyOfSeed = "  case \"$seed\" in\n" +
	"  " + standInSeed + ") pub=" + standInPublicKey + "; id=" + standInKeyID + " ;;\n" +
	"  " + secondSeed + ") pub=" + secondPublicKey + "; id=" + secondKeyID + " ;;\n" +
	"  " + thirdSeed + ") pub=" + thirdPublicKey + "; id=" + thirdKeyID + " ;;\n" +
	"  *) pub=; id= ;;\n" +
	"  esac\n"

// generateBySeed is the stand-in's `audit key generate` for these tests: the
// desk's own key is the first; a rotation's next key is the second, and any
// later one the third. A next key generated with no rotation marker beside it
// is noted in `<calls>.order`.
func generateBySeed(calls string) string {
	return "  case \"$4\" in\n" +
		"  *.next.seed)\n" +
		"    [ -e \"${4%.next.seed}.rotating\" ] || printf 'generate without a marker\\n' >> '" + calls + ".order'\n" +
		"    if [ -e '" + calls + ".second' ]; then seed=" + thirdSeed + "; else seed=" + secondSeed + "; : > '" + calls + ".second'; fi ;;\n" +
		"  *) seed=" + standInSeed + " ;;\n" +
		"  esac\n" + keyOfSeed +
		"  (umask 077; set -C; printf '%s\\n' \"$seed\" > \"$4\") || exit 4\n" +
		"  printf '{\"outputVersion\":\"2\",\"tool\":{\"name\":\"jpack\",\"version\":\"0.27.1\"},\"command\":\"audit key generate\",\"status\":\"generated\",\"publicKey\":\"%s\",\"keyId\":\"%s\"}\\n' \"$pub\" \"$id\""
}

// publicBySeed is the stand-in's `audit key public` for these tests: the key
// of the seed at "$4", or the runtime's refusal of one it does not know.
var publicBySeed = "  seed=; [ -r \"$4\" ] && IFS= read -r seed < \"$4\"\n" + keyOfSeed +
	"  if [ -z \"$pub\" ]; then printf '%s\\n' " + shellQuote(publicRefused) + "; exit 1; fi\n" +
	"  printf '{\"outputVersion\":\"2\",\"tool\":{\"name\":\"jpack\",\"version\":\"0.27.1\"},\"command\":\"audit key public\",\"status\":\"read\",\"publicKey\":\"%s\",\"keyId\":\"%s\"}\\n' \"$pub\" \"$id\""

// writingTheLine is the part of the stand-in's `audit key rotate` that does
// what the runtime does: it reads the current key from the desk's seed and
// the next from "$5", takes `at` as the trail's number of lines, refuses as
// 0.27.1 does where the trail has none, and appends the key-rotation line to
// the sidecar. It notes in `<calls>.order` how many keys the list held when it
// ran, and a run with no rotation marker beside the seed.
func writingTheLine(calls string) string {
	return "  cur=\"${5%.next.seed}.seed\"\n" +
		"  seed=; [ -r \"$cur\" ] && IFS= read -r seed < \"$cur\"\n" + keyOfSeed + "  from=$id\n" +
		"  seed=; [ -r \"$5\" ] && IFS= read -r seed < \"$5\"\n" + keyOfSeed + "  next=$pub; nextid=$id\n" +
		"  [ -e \"${5%.next.seed}.rotating\" ] || printf 'rotate without a marker\\n' >> '" + calls + ".order'\n" +
		"  n=0; while IFS= read -r _; do n=$((n+1)); done < \"${5%.next.seed}.keys.jsonl\"; printf 'rotate with %d keys listed\\n' \"$n\" >> '" + calls + ".order'\n" +
		"  at=0; if [ -e .desk-private/audit/evaluations.jsonl ]; then while IFS= read -r _; do at=$((at+1)); done < .desk-private/audit/evaluations.jsonl; fi\n" +
		"  if [ \"$at\" = 0 ]; then printf '%s\\n' " + shellQuote(rotateNoTrail) + "; exit 1; fi\n" +
		"  printf '{\"at\":%d,\"keyId\":\"%s\",\"kind\":\"key-rotation\",\"next\":\"%s\",\"sidecarVersion\":\"1\",\"signature\":\"" + fixtureSignature + "\",\"trail\":\"" + fixtureTrail + "\"}\\n' \"$at\" \"$from\" \"$next\" >> .desk-private/audit/signatures.jsonl\n"
}

// rotateAsTheRuntime is the stand-in's `audit key rotate`: writingTheLine,
// and the runtime's answer.
func rotateAsTheRuntime(calls string) string {
	return writingTheLine(calls) +
		"  printf '{\"outputVersion\":\"2\",\"tool\":{\"name\":\"jpack\",\"version\":\"0.27.1\"},\"command\":\"audit key rotate\",\"status\":\"rotated\",\"trailPath\":\"/project/.desk-private/audit/evaluations.jsonl\",\"at\":%d,\"trail\":\"" + fixtureTrail + "\",\"from\":\"%s\",\"next\":\"%s\",\"nextPublicKey\":\"%s\"}\\n' \"$at\" \"$from\" \"$nextid\" \"$next\""
}

// refusingWith is a stand-in `audit key rotate` that writes nothing, prints
// answer and exits with code.
func refusingWith(answer string, code int) string {
	return "  printf '%s\\n' " + shellQuote(answer) + "\n  exit " + strconv.Itoa(code)
}

// rotatingAs steers the stand-in whose calls file is calls: its `audit key
// rotate` runs fragment, a shell fragment. The next seed's path is "$5".
func rotatingAs(t *testing.T, calls, fragment string) {
	t.Helper()
	if err := os.WriteFile(calls+".rotate", []byte(fragment+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// rotationRig is a chassis over the stand-in, with one desk made signed on it
// by the first key.
type rotationRig struct {
	s       *Server
	ts      *httptest.Server
	rig     *auditRig
	logged  *bytes.Buffer
	id      string
	desk    string
	signing string
	config  string
}

// newRotationRig makes the desk id, signed, under Desk's configuration folder
// config (a folder of the test's own where it is empty), and steers the
// stand-in to generate, read and rotate keys by seed.
func newRotationRig(t *testing.T, id, config string) *rotationRig {
	t.Helper()
	t.Setenv("JPACK_CONFIG", "")
	rig := newAuditRig(t, withAuditVersions)
	rig.answers(t, 0, auditValidReport)
	if config == "" {
		config = t.TempDir()
	}
	logged := &bytes.Buffer{}
	s, ts := startDesk(t, Config{ProjectDir: t.TempDir(), JpackBin: rig.bin, Token: testToken, DeskConfigDir: config, Logger: log.New(logged, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	seed := filepath.Join(config, "secrets", "signing", id+seedSuffix)
	fixDeskIDs(t, id)
	signed := strings.Replace(wantSignedConfig(config, id), `"signingKey":"`+seed+`"`, `"signingKey":`+jsonString(seed), 1)
	digest := "sha256:" + digestOf([]byte(signed))
	if err := os.WriteFile(rig.calls+".lock-"+id, []byte(`{"lockVersion":"1","config":{"digest":"`+digest+`"}}`+"\n"+lockAnswer(digest)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	row := createGatedDesk(t, ts)
	if row.ID != id || !row.Signed || row.ConfigVersion != "6" {
		t.Fatalf("the desk was made %+v, want %s signed at 6", row, id)
	}
	generatingAs(t, rig.calls, generateBySeed(rig.calls))
	readingKeyAs(t, rig.calls, publicBySeed)
	rotatingAs(t, rig.calls, rotateAsTheRuntime(rig.calls))
	rig.ran(t)
	return &rotationRig{s: s, ts: ts, rig: rig, logged: logged, id: id, desk: row.Folder, signing: filepath.Join(config, "secrets", "signing"), config: config}
}

// auditFolder is the desk's audit folder.
func (r *rotationRig) auditFolder() string { return filepath.Join(r.desk, ".desk-private", "audit") }

// writeTrail gives the desk a trail of records lines, and a signature sidecar
// of sidecar, where it is not empty. Desk never parses the trail: it reads the
// sidecar under the trail's lock.
func (r *rotationRig) writeTrail(t *testing.T, records int, sidecar string) {
	t.Helper()
	if records > 0 {
		var trail strings.Builder
		for line := 1; line <= records; line++ {
			fmt.Fprintf(&trail, "{\"line\":%d}\n", line)
		}
		if err := os.WriteFile(filepath.Join(r.auditFolder(), "evaluations.jsonl"), []byte(trail.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if sidecar != "" {
		if err := os.WriteFile(filepath.Join(r.auditFolder(), "signatures.jsonl"), []byte(sidecar), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// panelOn is the decision record of desk id, as ts answers it, with its word
// on rotation.
func panelOn(t *testing.T, ts *httptest.Server, id string) (auditAnswer, string) {
	t.Helper()
	status, data := reviewCall(t, ts, "GET", "/api/audit/verify", id, nil, bearer)
	var answer auditAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Rotation == nil {
		t.Fatalf("the panel answered %d %s", status, data)
	}
	return answer, string(data)
}

func (r *rotationRig) panel(t *testing.T) (auditAnswer, string) {
	t.Helper()
	return panelOn(t, r.ts, r.id)
}

// rotate asks for a rotation with token.
func (r *rotationRig) rotate(t *testing.T, token string) (int, []byte) {
	t.Helper()
	return reviewCall(t, r.ts, "POST", "/api/audit/key/rotate", r.id, map[string]string{"token": token}, bearer)
}

// token is the panel's token, which the test requires it to give.
func (r *rotationRig) token(t *testing.T) string {
	t.Helper()
	answer, data := r.panel(t)
	if answer.Rotation.State != rotationAvailable || len(answer.Rotation.Token) != 64 {
		t.Fatalf("the panel offers no rotation: %s", data)
	}
	return answer.Rotation.Token
}

// seedIs is which key's seed the file at path holds: "1", "2", "3", "absent",
// or "other".
func seedIs(path string) string {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "absent"
	case err != nil:
		return "unread"
	}
	switch strings.TrimSuffix(string(data), "\n") {
	case standInSeed:
		return "1"
	case secondSeed:
		return "2"
	case thirdSeed:
		return "3"
	}
	return "other"
}

// describe is what the signing folder holds for the desk, by the names that
// start with its id, and how many key rotations its sidecar holds, in one
// line.
func (r *rotationRig) describe(t *testing.T) string {
	t.Helper()
	list, _ := os.ReadFile(filepath.Join(r.signing, r.id+keysSuffix))
	sidecar, _ := os.ReadFile(filepath.Join(r.auditFolder(), "signatures.jsonl"))
	var names []string
	for _, name := range namesIn(t, r.signing) {
		if mine, ok := strings.CutPrefix(name, r.id); ok {
			names = append(names, mine)
		}
	}
	return fmt.Sprintf("%s seed=%s next=%s keys=%d rotations=%d", strings.Join(names, ","),
		seedIs(filepath.Join(r.signing, r.id+seedSuffix)), seedIs(filepath.Join(r.signing, r.id+nextSeedSuffix)),
		bytes.Count(list, []byte("\n")), bytes.Count(sidecar, []byte(`"kind":"key-rotation"`)))
}

// snapshot is every name in the signing folder and the audit folder, with its
// bytes and the file it is, for "nothing changed".
func (r *rotationRig) snapshot(t *testing.T) string {
	t.Helper()
	var out strings.Builder
	for _, folder := range []string{r.signing, r.auditFolder()} {
		for _, name := range namesIn(t, folder) {
			path := filepath.Join(folder, name)
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(path)
			fmt.Fprintf(&out, "%s %v %d %q\n", name, info.Mode(), info.Sys().(*syscall.Stat_t).Ino, data)
		}
	}
	return out.String()
}

// The next key's path, and the runs a rotation makes after reading the keys.
func (r *rotationRig) nextPath() string { return filepath.Join(r.signing, r.id+nextSeedSuffix) }

func (r *rotationRig) generateCall() string {
	return "audit key generate " + r.nextPath() + " --format json [JPACK_CONFIG=unset]"
}

func (r *rotationRig) rotateCall() string {
	return "audit key rotate --next " + r.nextPath() + " --config jpack.json --format json [JPACK_CONFIG=unset]"
}

// **The steps, in order, each through Desk's signing folder** (ADR-0010,
// section 1). The panel offers the rotation with a token; the confirmed
// request reads the keys again, writes the marker, has the runtime generate
// the next key beside the seed and rotate to it, writes the list with the
// next key at the sequence the runtime gave, renames the next seed over the
// current one, and removes the marker: each step seen at the moment after
// it. Neither JPACK_CONFIG nor an inherited JPACK_SIGNING_KEY reaches a run.
// The list and the seed end exactly as spelled out here, 0600, with nothing
// else left; the panel then passes both keys, in order.
func TestARotationRunsItsStepsInOrder(t *testing.T) {
	t.Setenv("JPACK_SIGNING_KEY", filepath.Join(t.TempDir(), "owner.seed"))
	r := newRotationRig(t, "c1000000000000000000000000000001", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	token := r.token(t)
	r.rig.ran(t)
	r.rig.keysSeen(t)
	os.Remove(r.rig.calls + ".env")
	var seen []string
	testHookKeyBetween = func(at string) {
		if strings.HasPrefix(at, "rotation: ") {
			seen = append(seen, at+": "+r.describe(t))
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	status, data := r.rotate(t, token)
	testHookKeyBetween = nil
	var answer rotationAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil {
		t.Fatalf("the rotation answered %d %s", status, data)
	}
	if want := (rotationAnswer{State: "rotated", At: 1, From: key1, Next: deskPublicKey{secondPublicKey, secondKeyID, 1}}); answer != want {
		t.Errorf("the rotation answered %+v, want %+v", answer, want)
	}
	want := []string{
		"rotation: marker written: .keys.jsonl,.rotating,.seed seed=1 next=absent keys=1 rotations=0",
		"rotation: next generated: .keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=0",
		"rotation: line written: .keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=1",
		"rotation: list written: .keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=2 rotations=1",
		"rotation: seed renamed: .keys.jsonl,.rotating,.seed seed=2 next=absent keys=2 rotations=1",
	}
	if !slices.Equal(seen, want) {
		t.Errorf("the steps were\n%s\nwant\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
	if calls := r.rig.ran(t); !slices.Equal(calls, []string{schemaCall, publicCall, r.generateCall(), r.rotateCall()}) {
		t.Errorf("the rotation ran %q", calls)
	}
	for _, line := range envSeen(t, r.rig.calls) {
		if !strings.Contains(line, "[JPACK_SIGNING_KEY=unset]") {
			t.Errorf("a rotation's command inherited the signing key: %s", line)
		}
	}
	if got := readFile(t, r.rig.calls+".order"); got != "rotate with 1 keys listed\n" {
		t.Errorf("the runtime saw %q", got)
	}
	list := filepath.Join(r.signing, r.id+keysSuffix)
	if got := readFile(t, list); got != wantKeyLine(standInPublicKey, standInKeyID, 0)+wantKeyLine(secondPublicKey, secondKeyID, 1) {
		t.Errorf("the list of public keys is %q", got)
	}
	if got := readFile(t, filepath.Join(r.signing, r.id+seedSuffix)); got != secondSeed+"\n" {
		t.Errorf("the seed is %q, want the next key's", got)
	}
	for _, path := range []string{list, filepath.Join(r.signing, r.id+seedSuffix)} {
		if perm := permOf(t, path); perm != 0o600 {
			t.Errorf("%s is %v", filepath.Base(path), perm)
		}
	}
	if names := namesIn(t, r.signing); !slices.Equal(names, []string{r.id + keysSuffix, r.id + seedSuffix}) {
		t.Errorf("the signing folder holds %q", names)
	}
	if got := readFile(t, filepath.Join(r.auditFolder(), "signatures.jsonl")); got != recordLine(standInKeyID, 1)+rotationLine(1, standInKeyID, secondPublicKey) {
		t.Errorf("the sidecar is %q", got)
	}

	// The panel passes both keys, in order, and offers no second rotation
	// until a record is signed with the next key.
	panel, _ := r.panel(t)
	if panel.Keys == nil || panel.Keys.State != keysKept || !slices.Equal(panel.Keys.Public, []deskPublicKey{key1, {secondPublicKey, secondKeyID, 1}}) {
		t.Errorf("the panel shows the keys %+v", panel.Keys)
	}
	if seen := r.rig.keysSeen(t); !slices.Equal(seen, []string{standInPublicKey, secondPublicKey}) {
		t.Errorf("audit verify was given %q", seen)
	}
	if panel.Rotation.State != rotationUnavailable || panel.Rotation.Reason != "This desk's key took over after record 1, and no record has been signed since: a rotation now would take over after the same record. Make a deciding run first." || panel.Rotation.Token != "" {
		t.Errorf("the panel offers %+v", panel.Rotation)
	}
	r.writeTrail(t, 2, recordLine(standInKeyID, 1)+rotationLine(1, standInKeyID, secondPublicKey)+recordLine(secondKeyID, 2))
	if panel, _ := r.panel(t); panel.Rotation.State != rotationAvailable {
		t.Errorf("after a record signed with the next key the panel offers %+v", panel.Rotation)
	}
}

// assertUnchanged checks that the desk's keys are the first key alone, as
// made, with nothing of a rotation left.
func (r *rotationRig) assertUnchanged(t *testing.T) {
	t.Helper()
	if got := r.describe(t); !strings.HasPrefix(got, ".keys.jsonl,.seed seed=1 next=absent keys=1 ") {
		t.Errorf("the signing folder holds %s, want the first key alone", got)
	}
	if got := readFile(t, filepath.Join(r.signing, r.id+keysSuffix)); got != wantKeyLine(standInPublicKey, standInKeyID, 0) {
		t.Errorf("the list of public keys is %q", got)
	}
}

// **A refusal at step 3 changes nothing, and is said in the runtime's
// words; a line the runtime wrote is finished, whatever it answered.** Before
// the trail has a chained record, while its last line is incomplete, and with
// a current key that is not in force, the runtime refuses: the next key and
// the marker are removed, the list and the seed are as they were, and the
// answer is the runtime's own sentence. A runtime that wrote its line and then
// refused, as a write whose sync failed would, has rotated: the sidecar says
// so, and the rotation is finished. A next key the runtime will not generate
// leaves nothing either, not even what its failed run left under the next
// key's name.
// **No rotation past the key list's bound.** A desk whose list already holds
// as many keys as Desk keeps is offered no rotation, and a token the panel gave
// before is refused before the runtime is asked anything: the runtime would
// hand signing to a key the list could not name (review round 1).
func TestNoRotationPastTheKeyListsBound(t *testing.T) {
	t.Setenv("JPACK_SIGNING_KEY", filepath.Join(t.TempDir(), "owner.seed"))
	r := newRotationRig(t, "c1000000000000000000000000000077", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	token := r.token(t)
	rotationKeyLimit = 1
	t.Cleanup(func() { rotationKeyLimit = maxDeskKeys })
	panel, _ := r.panel(t)
	want := "This desk already has 1 signing keys, the most Desk keeps for one desk, so it rotates no further key."
	if panel.Rotation.State != rotationUnavailable || panel.Rotation.Reason != want || panel.Rotation.Token != "" {
		t.Errorf("at the bound the panel offers %+v", panel.Rotation)
	}
	r.rig.ran(t)
	if status, data := r.rotate(t, token); status != http.StatusConflict || !strings.Contains(string(data), want) {
		t.Errorf("a rotation at the bound answered %d %s", status, data)
	}
	for _, call := range r.rig.ran(t) {
		if strings.HasPrefix(call, "audit key generate") || strings.HasPrefix(call, "audit key rotate") {
			t.Errorf("a rotation at the bound ran %q", call)
		}
	}
	if names := namesIn(t, r.signing); !slices.Equal(names, []string{r.id + keysSuffix, r.id + seedSuffix}) {
		t.Errorf("a rotation at the bound left %q", names)
	}
}

func TestARotationTheRuntimeRefusesChangesNothing(t *testing.T) {
	const id = "c2000000000000000000000000000001"
	for _, tc := range []struct {
		name     string
		records  int
		sidecar  string
		rotate   func(calls string) string
		generate string
		why      string
	}{
		{"no chained record yet", 0, "", rotateAsTheRuntime, "",
			"The runtime did not rotate the key, and nothing was changed: Desk kept the current key. It said: The trail has no chained record yet, so there is no trail to rotate the key of; the first record is signed with whichever key the project names."},
		{"an incomplete last line", 2, recordLine(standInKeyID, 1), func(string) string { return refusingWith(rotateIncomplete, 4) }, "",
			"The runtime did not rotate the key, and nothing was changed: Desk kept the current key. It said: Audit record could not be written: the audit trail's last line is incomplete, and no record is chained after an incomplete line."},
		{"a current key not in force", 2, recordLine(standInKeyID, 1), func(string) string { return refusingWith(rotateNotInForce, 1) }, "",
			"The runtime did not rotate the key, and nothing was changed: Desk kept the current key. It said: The project's signing key is not the key in force in the signature sidecar, so it cannot hand signing over; only the key in force can."},
		{"a next key the runtime will not generate", 1, recordLine(standInKeyID, 1), rotateAsTheRuntime,
			"  printf 'part of a seed' > \"$4\"\n  printf '%s\\n' " + shellQuote(`{"outputVersion":"2","command":"audit key generate","status":"error","diagnostics":[{"code":"JPS-AUDIT-KEY-EXISTS","message":"Something is already there, and a key is never written over anything."}]}`) + "\n  exit 4",
			"Nothing was rotated: the runtime did not generate its signing key: Something is already there, and a key is never written over anything."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRotationRig(t, id, "")
			r.writeTrail(t, tc.records, tc.sidecar)
			rotatingAs(t, r.rig.calls, tc.rotate(r.rig.calls))
			if tc.generate != "" {
				generatingAs(t, r.rig.calls, tc.generate)
			}
			sidecarBefore, _ := os.ReadFile(filepath.Join(r.auditFolder(), "signatures.jsonl"))
			status, data := r.rotate(t, r.token(t))
			if status != http.StatusConflict || refusalOf(data) != tc.why {
				t.Errorf("the rotation answered %d %q, want 409 %q", status, refusalOf(data), tc.why)
			}
			r.assertUnchanged(t)
			if sidecarAfter, _ := os.ReadFile(filepath.Join(r.auditFolder(), "signatures.jsonl")); !bytes.Equal(sidecarBefore, sidecarAfter) {
				t.Errorf("the sidecar changed: %q", sidecarAfter)
			}
		})
	}

	t.Run("a line written and then refused", func(t *testing.T) {
		r := newRotationRig(t, id, "")
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		rotatingAs(t, r.rig.calls, writingTheLine(r.rig.calls)+"  printf '%s\\n' "+shellQuote(`{"outputVersion":"2","command":"audit key rotate","status":"error","diagnostics":[{"code":"JPS-AUDIT-WRITE","message":"Audit record could not be written."}]}`)+"\n  exit 4")
		status, data := r.rotate(t, r.token(t))
		var answer rotationAnswer
		if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.At != 1 || answer.Next != (deskPublicKey{secondPublicKey, secondKeyID, 1}) {
			t.Fatalf("the rotation answered %d %s, want it finished from the sidecar", status, data)
		}
		if got := r.describe(t); got != ".keys.jsonl,.seed seed=2 next=absent keys=2 rotations=1" {
			t.Errorf("the signing folder holds %s", got)
		}
	})
}

// **A rotation the runtime did not write says Desk kept its key, and no
// more** (issue #239). Another runtime rotated the trail after Desk read it,
// so the runtime refuses: the desk's key is not the key in force. Desk removes
// the next key and the marker and keeps the current seed, and says that; it
// never says that key still signs, which it did not check, and which the
// runtime has just said it does not. The panel says the same of a rotation a
// stop left before the runtime wrote its line.
func TestARotationTheRuntimeDidNotWriteSaysDeskKeptItsKey(t *testing.T) {
	t.Run("another runtime rotated the trail", func(t *testing.T) {
		r := newRotationRig(t, "c2000000000000000000000000000002", "")
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		token := r.token(t)
		rotatingAs(t, r.rig.calls, "  printf '%s' "+shellQuote(rotationLine(1, standInKeyID, thirdPublicKey))+" >> .desk-private/audit/signatures.jsonl\n"+refusingWith(rotateNotInForce, 1))
		status, data := r.rotate(t, token)
		want := "The runtime did not rotate the key, and nothing was changed: Desk kept the current key. It said: The project's signing key is not the key in force in the signature sidecar, so it cannot hand signing over; only the key in force can."
		if status != http.StatusConflict || refusalOf(data) != want {
			t.Errorf("the rotation answered %d %q, want 409 %q", status, refusalOf(data), want)
		}
		if got := r.describe(t); got != ".keys.jsonl,.seed seed=1 next=absent keys=1 rotations=1" {
			t.Errorf("the signing folder holds %s, want the current key alone", got)
		}
	})

	t.Run("the panel, on a rotation the runtime did not write", func(t *testing.T) {
		r := newRotationRig(t, "c2000000000000000000000000000003", "")
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		if os.WriteFile(filepath.Join(r.signing, r.id+rotatingSuffix), nil, 0o600) != nil || os.WriteFile(r.nextPath(), []byte(secondSeed+"\n"), 0o600) != nil {
			t.Fatal("could not leave a rotation unfinished")
		}
		answer, _ := r.panel(t)
		want := "The runtime did not write the rotation: Desk removes the next key when it next starts, and keeps the current key."
		if answer.Rotation.State != rotationUnfinished || answer.Rotation.Reason != want {
			t.Errorf("the panel says %+v, want unfinished: %q", answer.Rotation, want)
		}
	})
}

// **Where no rotation can be made, none is, and the panel says why.** The
// project Desk was started on keeps no key Desk rotates; a desk with no key
// has none to rotate; a token the panel did not give is stale; a rotation
// that did not finish blocks another; a runtime that does not read "6" has no
// audit key rotate; a key that took over after the last record cannot hand
// over after it again; and a request that is not the owner's confirmation is
// refused before anything is read. None of them runs `audit key generate`.
func TestARotationIsMadeOnlyWhereItCanBe(t *testing.T) {
	r := newRotationRig(t, "c3000000000000000000000000000001", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	noGenerate := func(t *testing.T) {
		t.Helper()
		for _, call := range r.rig.ran(t) {
			if strings.HasPrefix(call, "audit key generate") || strings.HasPrefix(call, "audit key rotate") {
				t.Errorf("a refused rotation ran %q", call)
			}
		}
		r.assertUnchanged(t)
	}

	t.Run("the project Desk was started on", func(t *testing.T) {
		writeProject(t, r.s.projectDir, map[string]string{"jpack.json": auditedConfig})
		answer, _ := panelOn(t, r.ts, "")
		want := "This is the project Desk was started on. Desk keeps no signing key for it in this version, so it has none to rotate."
		if answer.Rotation.State != rotationUnavailable || answer.Rotation.Reason != want {
			t.Errorf("the startup desk's panel offers %+v", answer.Rotation)
		}
		status, data := reviewCall(t, r.ts, "POST", "/api/audit/key/rotate", "", map[string]string{"token": strings.Repeat("a", 64)}, bearer)
		if status != http.StatusConflict || refusalOf(data) != want {
			t.Errorf("the startup desk answered %d %s", status, data)
		}
		noGenerate(t)
	})

	t.Run("a stale token", func(t *testing.T) {
		status, data := r.rotate(t, strings.Repeat("a", 64))
		if status != http.StatusConflict || refusalOf(data) != "This desk's keys changed after the decision record showed them, so nothing was rotated. Check the decision record again." {
			t.Errorf("a stale token answered %d %s", status, data)
		}
		noGenerate(t)
	})

	t.Run("a token for keys that changed since", func(t *testing.T) {
		token := r.token(t)
		// Another key, kept as Desk keeps one: the panel would offer it, with
		// another token.
		seed := filepath.Join(r.signing, r.id+seedSuffix)
		writeKeys(t, r.signing, r.id, wantKeyLine(secondPublicKey, secondKeyID, 0))
		if err := os.WriteFile(seed, []byte(secondSeed+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if other := r.token(t); other == token {
			t.Error("the token did not change with the keys")
		}
		status, data := r.rotate(t, token)
		if status != http.StatusConflict || refusalOf(data) != "This desk's keys changed after the decision record showed them, so nothing was rotated. Check the decision record again." {
			t.Errorf("a token for other keys answered %d %s", status, data)
		}
		writeKeys(t, r.signing, r.id, wantKeyLine(standInPublicKey, standInKeyID, 0))
		if err := os.WriteFile(seed, []byte(standInSeed+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		noGenerate(t)
	})

	t.Run("a token for a list that changed since, the key the same", func(t *testing.T) {
		token := r.token(t)
		writeKeys(t, r.signing, r.id, wantKeyLine(thirdPublicKey, thirdKeyID, 0)+wantKeyLine(standInPublicKey, standInKeyID, 1))
		r.writeTrail(t, 2, recordLine(thirdKeyID, 1)+rotationLine(1, thirdKeyID, standInPublicKey)+recordLine(standInKeyID, 2))
		if other := r.token(t); other == token {
			t.Error("the token did not change with the list")
		}
		status, data := r.rotate(t, token)
		if status != http.StatusConflict || refusalOf(data) != "This desk's keys changed after the decision record showed them, so nothing was rotated. Check the decision record again." {
			t.Errorf("a token for another list answered %d %s", status, data)
		}
		writeKeys(t, r.signing, r.id, wantKeyLine(standInPublicKey, standInKeyID, 0))
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		noGenerate(t)
	})

	t.Run("a rotation that did not finish", func(t *testing.T) {
		token := r.token(t)
		marker := filepath.Join(r.signing, r.id+rotatingSuffix)
		if err := os.WriteFile(marker, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(marker)
		answer, _ := r.panel(t)
		if answer.Rotation.State != rotationUnfinished || answer.Rotation.Reason != "Nothing of it is left to do but remove its marker, which Desk does when it next starts." {
			t.Errorf("the panel says %+v", answer.Rotation)
		}
		status, data := r.rotate(t, token)
		if status != http.StatusConflict || !strings.HasPrefix(refusalOf(data), "A rotation of this desk's key did not finish, so Desk starts no other.") {
			t.Errorf("a rotation over another's marker answered %d %s", status, data)
		}
		r.rig.ran(t)
		if names := namesIn(t, r.signing); !slices.Equal(names, []string{r.id + keysSuffix, r.id + rotatingSuffix, r.id + seedSuffix}) {
			t.Errorf("the signing folder holds %q", names)
		}
	})

	t.Run("no record since the last rotation", func(t *testing.T) {
		writeKeys(t, r.signing, r.id, wantKeyLine(thirdPublicKey, thirdKeyID, 0)+wantKeyLine(standInPublicKey, standInKeyID, 1))
		r.writeTrail(t, 1, recordLine(thirdKeyID, 1)+rotationLine(1, thirdKeyID, standInPublicKey))
		defer func() {
			writeKeys(t, r.signing, r.id, wantKeyLine(standInPublicKey, standInKeyID, 0))
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		}()
		answer, _ := r.panel(t)
		if answer.Keys.State != keysKept || answer.Rotation.State != rotationUnavailable || !strings.HasPrefix(answer.Rotation.Reason, "This desk's key took over after record 1, and no record has been signed since") {
			t.Errorf("the panel shows %+v and %+v", answer.Keys, answer.Rotation)
		}
	})

	t.Run("a sidecar Desk cannot read", func(t *testing.T) {
		sidecar := filepath.Join(r.auditFolder(), "signatures.jsonl")
		if err := os.Link(sidecar, filepath.Join(r.auditFolder(), "copy.jsonl")); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(filepath.Join(r.auditFolder(), "copy.jsonl"))
		answer, _ := r.panel(t)
		want := "Desk reads the trail's signature sidecar to tell whether a rotation was written, and it could not be read: it, or the trail it is read beside, has another name as well, a hard link. So Desk does not rotate the key now."
		if answer.Keys.State != keysKept || answer.Rotation.State != rotationUnavailable || answer.Rotation.Reason != want {
			t.Errorf("the panel shows %+v and %+v", answer.Keys, answer.Rotation)
		}
	})

	t.Run("requests that are not the owner's confirmation", func(t *testing.T) {
		token := r.token(t)
		r.rig.ran(t)
		for _, tc := range []struct {
			name     string
			status   int
			decorate func(*http.Request)
			body     any
		}{
			{"cross-site", http.StatusForbidden, func(req *http.Request) { req.Header.Set("Sec-Fetch-Site", "cross-site") }, map[string]string{"token": token}},
			{"not JSON", http.StatusUnsupportedMediaType, func(req *http.Request) { req.Header.Set("Content-Type", "text/plain") }, map[string]string{"token": token}},
			{"no token", http.StatusBadRequest, func(*http.Request) {}, map[string]string{"token": "short"}},
			{"no bearer", http.StatusUnauthorized, func(req *http.Request) { req.Header.Del("Authorization") }, map[string]string{"token": token}},
		} {
			status, data := reviewCall(t, r.ts, "POST", "/api/audit/key/rotate", r.id, tc.body, bearer, tc.decorate)
			if status != tc.status {
				t.Errorf("%s answered %d %s, want %d", tc.name, status, data, tc.status)
			}
		}
		if calls := r.rig.ran(t); calls != nil {
			t.Errorf("a refused request ran %q", calls)
		}
		r.assertUnchanged(t)
	})

	t.Run("a desk with no key", func(t *testing.T) {
		other := newRotationRig(t, "c3000000000000000000000000000002", "")
		for _, name := range []string{other.id + keysSuffix, other.id + seedSuffix} {
			removeNamed(t, other.signing, name)
		}
		answer, _ := other.panel(t)
		want := "Desk keeps no signing key for this desk, so it has none to rotate."
		if answer.Rotation.State != rotationUnavailable || answer.Rotation.Reason != want {
			t.Errorf("the panel offers %+v", answer.Rotation)
		}
		status, data := other.rotate(t, strings.Repeat("a", 64))
		if status != http.StatusConflict || refusalOf(data) != "Nothing was rotated. "+want {
			t.Errorf("a desk with no key answered %d %s", status, data)
		}
	})

	t.Run("a runtime that does not read 6", func(t *testing.T) {
		other := newRotationRig(t, "c3000000000000000000000000000003", "")
		other.writeTrail(t, 1, recordLine(standInKeyID, 1))
		token := other.token(t)
		script, err := os.ReadFile(other.rig.bin)
		if err != nil {
			t.Fatal(err)
		}
		older := bytes.Replace(script, []byte(`"supportedConfigVersions":["1","2","3","4","5","6"]`), []byte(`"supportedConfigVersions":["1","2","3","4","5"]`), 1)
		if bytes.Equal(older, script) || os.WriteFile(other.rig.bin, older, 0o755) != nil {
			t.Fatal("could not make the stand-in older")
		}
		other.rig.ran(t)
		status, data := other.rotate(t, token)
		if status != http.StatusConflict || refusalOf(data) != "The runtime this Desk runs (jpack 0.0.0-stand-in) does not read configVersion 6 and has no audit key rotate, so Desk rotates no key with it. A runtime of 0.26.0 or later has it." {
			t.Errorf("an older runtime answered %d %s", status, data)
		}
		if calls := other.rig.ran(t); !slices.Equal(calls, []string{schemaCall}) {
			t.Errorf("an older runtime ran %q", calls)
		}
	})
}

// **No answer of a rotation names a path.** Desk's configuration folder has a
// space, a tab and a line separator in its name, under a home folder with a
// space and parentheses. The runtime's refusals name the next key and the
// signing folder as it prints them, and as JSON carries them whole; neither
// reaches the answer, nor does the panel's word on a rotation that did not
// finish.
func TestARotationsAnswersNameNoPath(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "Home DIR (owner)")
	config := filepath.Join(base, "Owner SECRET\tKEYS TAIL dir")
	if os.Mkdir(base, 0o700) != nil || os.Mkdir(config, 0o700) != nil {
		t.Skip("this file system refuses the names")
	}
	signing := filepath.Join(config, "secrets", "signing")
	leaks := func(t *testing.T, what, said string) {
		t.Helper()
		for _, part := range []string{"SECRET", "TAIL", "KEYS", "DIR", "(owner)", root, runtimePrints(config)} {
			if strings.Contains(said, part) {
				t.Errorf("%s names %q: %s", what, part, said)
			}
		}
	}
	naming := func(id string) string {
		next := filepath.Join(signing, id+nextSeedSuffix)
		return "The next key at " + runtimePrints(next) + " is refused: the directory " + runtimePrints(signing) + " is not the one held; also " + next + " and (" + signing + ")."
	}
	for _, tc := range []struct{ id, name, generate, rotate, why string }{
		{"c4000000000000000000000000000001", "a refusal to generate", "  printf '%s\\n' " + shellQuote(`{"outputVersion":"2","command":"audit key generate","status":"error","diagnostics":[{"code":"JPS-AUDIT-KEY-REFUSED","message":`+jsonString(naming("c4000000000000000000000000000001"))+`}]}`) + "\n  exit 1", "",
			"Nothing was rotated: the runtime did not generate its signing key: The next key at … is refused: the directory … is not the one held; also … and (…)."},
		{"c4000000000000000000000000000002", "a refusal to rotate", "", refusingWith(`{"outputVersion":"2","command":"audit key rotate","status":"error","diagnostics":[{"code":"JPS-AUDIT-KEY-REFUSED","message":`+jsonString(naming("c4000000000000000000000000000002"))+`}]}`, 1),
			"The runtime did not rotate the key, and nothing was changed: Desk kept the current key. It said: The next key at … is refused: the directory … is not the one held; also … and (…)."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRotationRig(t, tc.id, config)
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			if tc.generate != "" {
				generatingAs(t, r.rig.calls, tc.generate)
			}
			if tc.rotate != "" {
				rotatingAs(t, r.rig.calls, tc.rotate)
			}
			status, data := r.rotate(t, r.token(t))
			if status != http.StatusConflict || refusalOf(data) != tc.why {
				t.Errorf("the rotation answered %d %q, want %q", status, refusalOf(data), tc.why)
			}
			leaks(t, "the answer", string(data))
			r.assertUnchanged(t)
			if !strings.Contains(r.logged.String(), "SECRET") {
				t.Errorf("Desk's log does not keep the runtime's words whole: %s", r.logged)
			}
		})
	}

	t.Run("a rotation that did not finish", func(t *testing.T) {
		const id = "c4000000000000000000000000000003"
		next := filepath.Join(signing, id+nextSeedSuffix)
		r := newRotationRig(t, id, config)
		r.writeTrail(t, 1, recordLine(standInKeyID, 1))
		if os.WriteFile(filepath.Join(signing, id+rotatingSuffix), nil, 0o600) != nil || os.WriteFile(next, []byte("not a seed\n"), 0o600) != nil {
			t.Fatal("could not leave a rotation unfinished")
		}
		readingKeyAs(t, r.rig.calls, "  case \"$4\" in *.next.seed) printf '%s\\n' "+shellQuote(`{"outputVersion":"2","command":"audit key public","status":"invalid","diagnostics":[{"code":"JPS-AUDIT-KEY-REFUSED","message":`+jsonString("The key at "+runtimePrints(next)+" is refused: it is not 64 hexadecimal characters.")+`}]}`)+"; exit 1 ;; esac\n"+publicBySeed)
		answer, data := r.panel(t)
		want := "Desk cannot tell whether the runtime wrote the rotation, so it changes nothing: the runtime could not read the next key: The key at … is refused: it is not 64 hexadecimal characters."
		if answer.Rotation.State != rotationUnfinished || answer.Rotation.Reason != want {
			t.Errorf("the panel says %+v, want %q", answer.Rotation, want)
		}
		leaks(t, "the panel", data)
	})
}

// **The list is read whole and strictly, and against the sidecar.** Beside
// parseDeskKeys's own rule, the last key must be the seed's, and each later
// key the one the sidecar's key rotations hand over to, in order, made by the
// key before it, at the sequence the list gives; the sidecar's lines are read
// as the runtime reads them. Each disagreement passes no key and says why. A
// sidecar Desk cannot read is not a disagreement: the keys are passed on the
// other checks.
func TestTheListOfKeysIsHeldToTheSidecar(t *testing.T) {
	key2 := deskPublicKey{secondPublicKey, secondKeyID, 3}
	two := wantKeyLine(standInPublicKey, standInKeyID, 0) + wantKeyLine(secondPublicKey, secondKeyID, 3)
	before := recordLine(standInKeyID, 1) + recordLine(standInKeyID, 2) + recordLine(standInKeyID, 3)
	reordered := `{"next":"` + secondPublicKey + `","at":3,"kind":"key-rotation","keyId":"` + standInKeyID + `","trail":"` + fixtureTrail + `","signature":"` + fixtureSignature + `","sidecarVersion":"1"}` + "\n"
	disagree := "Desk's list of this desk's public keys does not agree with the key rotations in the trail's signature sidecar, so it passed no key: "
	for _, tc := range []struct {
		name, list, sidecar string
		seed                string
		why                 string
	}{
		{"two keys the sidecar names", two, before + rotationLine(3, standInKeyID, secondPublicKey) + recordLine(secondKeyID, 4), secondSeed, ""},
		{"a rotation line in another member order, as the runtime reads it", two, before + reordered, secondSeed, ""},
		{"two keys and no rotation", two, before, secondSeed,
			disagree + "the trail's signature sidecar records 0 key rotations, and the list names 1 keys after the first."},
		{"two keys and no sidecar", two, "", secondSeed,
			disagree + "the trail's signature sidecar records 0 key rotations, and the list names 1 keys after the first."},
		{"a rotation the list does not name", wantKeyLine(secondPublicKey, secondKeyID, 0), before + rotationLine(3, standInKeyID, secondPublicKey), secondSeed,
			disagree + "the trail's signature sidecar records 1 key rotations, and the list names 0 keys after the first."},
		{"another sequence", wantKeyLine(standInPublicKey, standInKeyID, 0) + wantKeyLine(secondPublicKey, secondKeyID, 4), before + rotationLine(3, standInKeyID, secondPublicKey), secondSeed,
			disagree + "key 2 of the list takes over from sequence 4, and the sidecar's key rotation to it from 3."},
		{"a rotation to another key", two, before + rotationLine(3, standInKeyID, thirdPublicKey), secondSeed,
			disagree + "key 2 of the list is not the key the sidecar's key rotation 1 hands over to."},
		{"a rotation made by another key", two, before + rotationLine(3, thirdKeyID, secondPublicKey), secondSeed,
			disagree + "the sidecar's key rotation 1 was made by a key other than key 1 of the list."},
		{"a rotation line a write left incomplete", two, before + strings.TrimSuffix(rotationLine(3, standInKeyID, secondPublicKey), "\n"), secondSeed,
			disagree + "the trail's signature sidecar records 0 key rotations, and the list names 1 keys after the first."},
		{"a rotation line with a member more", two, before + strings.Replace(rotationLine(3, standInKeyID, secondPublicKey), `{"at"`, `{"note":"x","at"`, 1), secondSeed,
			disagree + "the trail's signature sidecar records 0 key rotations, and the list names 1 keys after the first."},
		{"a rotation line with a member twice", two, before + strings.Replace(rotationLine(3, standInKeyID, secondPublicKey), `{"at":3`, `{"at":3,"at":3`, 1), secondSeed,
			disagree + "the trail's signature sidecar records 0 key rotations, and the list names 1 keys after the first."},
		{"a rotation line at 03", two, before + strings.Replace(rotationLine(3, standInKeyID, secondPublicKey), `{"at":3`, `{"at":03`, 1), secondSeed,
			disagree + "the trail's signature sidecar records 0 key rotations, and the list names 1 keys after the first."},
		{"a rotation line longer than any line", two, before + strings.Replace(rotationLine(3, standInKeyID, secondPublicKey), `"sidecarVersion":"1"`, `"sidecarVersion":"1"`+strings.Repeat(" ", 4096), 1), secondSeed,
			disagree + "the trail's signature sidecar records 0 key rotations, and the list names 1 keys after the first."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRotationRig(t, "c5000000000000000000000000000001", "")
			writeKeys(t, r.signing, r.id, tc.list)
			if err := os.WriteFile(filepath.Join(r.signing, r.id+seedSuffix), []byte(tc.seed+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			r.writeTrail(t, 4, tc.sidecar)
			answer, data := r.panel(t)
			if tc.why == "" {
				if answer.Keys.State != keysKept || !slices.Equal(answer.Keys.Public, []deskPublicKey{key1, key2}) {
					t.Errorf("the panel shows the keys %+v", answer.Keys)
				}
				if seen := r.rig.keysSeen(t); !slices.Equal(seen, []string{standInPublicKey, secondPublicKey}) {
					t.Errorf("audit verify was given %q", seen)
				}
				return
			}
			if answer.Keys.State != keysUnread || answer.Keys.Problem != tc.why || answer.Keys.Public != nil {
				t.Errorf("the panel shows the keys %+v, want unread: %q", answer.Keys, tc.why)
			}
			if seen := r.rig.keysSeen(t); seen != nil {
				t.Errorf("audit verify was given %q", seen)
			}
			if answer.Rotation.State != rotationUnavailable {
				t.Errorf("the panel offers %+v over keys it did not pass", answer.Rotation)
			}
			if strings.Contains(data, r.config) {
				t.Errorf("the panel names a path: %s", data)
			}
		})
	}

	t.Run("a sidecar Desk cannot read", func(t *testing.T) {
		r := newRotationRig(t, "c5000000000000000000000000000002", "")
		writeKeys(t, r.signing, r.id, two)
		if err := os.WriteFile(filepath.Join(r.signing, r.id+seedSuffix), []byte(secondSeed+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		r.writeTrail(t, 4, before)
		if err := os.Link(filepath.Join(r.auditFolder(), "signatures.jsonl"), filepath.Join(r.auditFolder(), "copy.jsonl")); err != nil {
			t.Fatal(err)
		}
		answer, _ := r.panel(t)
		if answer.Keys.State != keysKept || !slices.Equal(answer.Keys.Public, []deskPublicKey{key1, key2}) {
			t.Errorf("the panel shows the keys %+v", answer.Keys)
		}
		if !strings.Contains(r.logged.String(), "could not be read to check its list of keys") {
			t.Errorf("Desk's log does not say the sidecar was not read: %s", r.logged)
		}
	})
}

// abandonRotation asks for a rotation and stops it at the step at, as a
// crash would: the request's handler is aborted there, and nothing after it
// runs.
func (r *rotationRig) abandonRotation(t *testing.T, at string) {
	t.Helper()
	token := r.token(t)
	testHookKeyBetween = func(step string) {
		if step == at {
			panic(http.ErrAbortHandler)
		}
	}
	defer func() { testHookKeyBetween = nil }()
	request, _ := http.NewRequest("POST", r.ts.URL+"/api/audit/key/rotate", strings.NewReader(`{"token":"`+token+`"}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Jpack-Desk", r.id)
	if response, err := http.DefaultClient.Do(request); err == nil {
		response.Body.Close()
	}
}

// restart stops the chassis and starts it again over the same folders, and
// serves the new one.
func (r *rotationRig) restart(t *testing.T) (*Server, *httptest.Server, *bytes.Buffer) {
	t.Helper()
	r.ts.Close()
	again, logged := restartedServer(t, r.s)
	ts := httptest.NewServer(again)
	t.Cleanup(ts.Close)
	return again, ts, logged
}

// **A rotation stopped at any step is finished or undone at the next start.**
// Stopped before the runtime wrote its line, the next key and the marker go,
// and Desk keeps the first key; stopped after it, the list is written with
// the next key at the sequence the line gives, the next seed is renamed over
// the current one, and the marker goes. What each stop left is checked, and
// what the start made of it; the panel then passes the keys the desk has.
func TestAStoppedRotationIsFinishedOrUndoneAtTheNextStart(t *testing.T) {
	const id = "c6000000000000000000000000000001"
	first := ".keys.jsonl,.seed seed=1 next=absent keys=1"
	rotated := ".keys.jsonl,.seed seed=2 next=absent keys=2 rotations=1"
	for _, tc := range []struct {
		at, left, after string
	}{
		{"rotation: marker written", ".keys.jsonl,.rotating,.seed seed=1 next=absent keys=1 rotations=0", first + " rotations=0"},
		{"rotation: next generated", ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=0", first + " rotations=0"},
		{"rotation: line written", ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=1", rotated},
		{"rotation: list written", ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=2 rotations=1", rotated},
		{"rotation: seed renamed", ".keys.jsonl,.rotating,.seed seed=2 next=absent keys=2 rotations=1", rotated},
	} {
		t.Run(tc.at, func(t *testing.T) {
			r := newRotationRig(t, id, "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			r.abandonRotation(t, tc.at)
			if got := r.describe(t); got != tc.left {
				t.Fatalf("the stop left %s, want %s", got, tc.left)
			}
			_, ts, logged := r.restart(t)
			if got := r.describe(t); got != tc.after {
				t.Errorf("after the next start the folder holds %s, want %s (%s)", got, tc.after, logged)
			}
			answer, _ := panelOn(t, ts, id)
			want := []deskPublicKey{key1}
			if tc.after == rotated {
				want = append(want, deskPublicKey{secondPublicKey, secondKeyID, 1})
				if got := readFile(t, filepath.Join(r.signing, id+keysSuffix)); got != wantKeyLine(standInPublicKey, standInKeyID, 0)+wantKeyLine(secondPublicKey, secondKeyID, 1) {
					t.Errorf("the list is %q", got)
				}
			}
			if answer.Keys.State != keysKept || !slices.Equal(answer.Keys.Public, want) || answer.Rotation.State == rotationUnfinished {
				t.Errorf("after the next start the panel shows %+v and %+v", answer.Keys, answer.Rotation)
			}
		})
	}
}

// **Where Desk cannot tell, it changes nothing.** A rotation stopped after the
// runtime wrote its line, and one stopped before, are each left with one thing
// that decides it unreadable: the sidecar with a second name, the trail's
// lock not taken, the current seed gone, a next key the runtime will not read,
// or a later rotation to another key. (The signing folder's lock, held by
// another process, is signing_lock_test.go's.) The next start changes no byte
// and no file of the signing folder or the audit folder, and the panel says a
// rotation did not finish, and why. So no seed is ever removed because a read
// failed.
func TestARotationDeskCannotTellAboutChangesNothing(t *testing.T) {
	const id = "c7000000000000000000000000000001"
	cannot := "Desk cannot tell whether the runtime wrote the rotation, so it changes nothing: "
	for _, stop := range []string{"rotation: line written", "rotation: next generated"} {
		for _, tc := range []struct {
			name  string
			setUp func(t *testing.T, r *rotationRig) func()
			why   string
		}{
			{"a sidecar with a second name", func(t *testing.T, r *rotationRig) func() {
				copy := filepath.Join(r.auditFolder(), "copy.jsonl")
				if err := os.Link(filepath.Join(r.auditFolder(), "signatures.jsonl"), copy); err != nil {
					t.Fatal(err)
				}
				return func() {}
			}, cannot + "the trail's signature sidecar could not be read: it, or the trail it is read beside, has another name as well, a hard link."},
			{"the trail's lock not taken", func(t *testing.T, r *rotationRig) func() {
				was, wait := flockAudit, auditLockWait
				flockAudit = func(file *os.File, how int) error {
					if how&syscall.LOCK_SH != 0 {
						return syscall.EWOULDBLOCK
					}
					return was(file, how)
				}
				auditLockWait = 20 * time.Millisecond
				return func() { flockAudit, auditLockWait = was, wait }
			}, cannot + "the trail's signature sidecar could not be read: a runtime held the trail's lock for too long."},
			{"no current key", func(t *testing.T, r *rotationRig) func() {
				removeNamed(t, r.signing, r.id+seedSuffix)
				return func() {}
			}, cannot + "Desk holds no current key for this desk."},
			{"a next key the runtime will not read", func(t *testing.T, r *rotationRig) func() {
				if err := os.WriteFile(r.nextPath(), []byte("not a seed\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				return func() {}
			}, cannot + "the runtime could not read the next key: The key is refused: the signing key is not 64 hexadecimal characters."},
			{"a later rotation to another key", func(t *testing.T, r *rotationRig) func() {
				sidecar := filepath.Join(r.auditFolder(), "signatures.jsonl")
				data := readFile(t, sidecar)
				if !strings.Contains(data, `"kind":"key-rotation"`) {
					data += rotationLine(1, standInKeyID, secondPublicKey)
				}
				if err := os.WriteFile(sidecar, []byte(data+rotationLine(1, secondKeyID, thirdPublicKey)), 0o600); err != nil {
					t.Fatal(err)
				}
				return func() {}
			}, cannot + "the trail's signature sidecar names the next key, and its last key rotation does not hand over to it."},
		} {
			t.Run(stop+"/"+tc.name, func(t *testing.T) {
				r := newRotationRig(t, id, "")
				r.writeTrail(t, 1, recordLine(standInKeyID, 1))
				r.abandonRotation(t, stop)
				if !strings.Contains(r.describe(t), ".next.seed,.rotating,") {
					t.Fatalf("the stop left %s", r.describe(t))
				}
				restore := tc.setUp(t, r)
				defer restore()
				before := r.snapshot(t)
				_, ts, logged := r.restart(t)
				if after := r.snapshot(t); after != before {
					t.Errorf("the start changed what it could not tell about:\nbefore\n%s\nafter\n%s\n(%s)", before, after, logged)
				}
				answer, _ := panelOn(t, ts, id)
				if answer.Rotation.State != rotationUnfinished || answer.Rotation.Reason != tc.why {
					t.Errorf("the panel says %+v, want %q", answer.Rotation, tc.why)
				}
				if after := r.snapshot(t); after != before {
					t.Errorf("the panel changed what it could not tell about:\n%s", after)
				}
				if !strings.Contains(logged.String(), "was left as it is") {
					t.Errorf("Desk's log does not say the rotation was left: %s", logged)
				}
			})
		}
	}
}

// **Two more things Desk cannot tell.** A rotation to the next key made by
// a key other than the current one, and, with no next key left, a sidecar
// that records a rotation the list does not: each is left as it is, and said.
func TestARotationFromElsewhereIsLeftAsItIs(t *testing.T) {
	const id = "c7000000000000000000000000000002"
	cannot := "Desk cannot tell whether the runtime wrote the rotation, so it changes nothing: "
	for _, tc := range []struct{ stop, line, why string }{
		{"rotation: next generated", rotationLine(1, thirdKeyID, secondPublicKey),
			cannot + "the sidecar's last key rotation hands over to the next key from a key other than the current one."},
		{"rotation: marker written", rotationLine(1, standInKeyID, secondPublicKey),
			cannot + "there is no next key, and the list of this desk's public keys does not agree with its current key and the sidecar: the trail's signature sidecar records 1 key rotations, and the list names 0 keys after the first."},
	} {
		t.Run(tc.stop, func(t *testing.T) {
			r := newRotationRig(t, id, "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			r.abandonRotation(t, tc.stop)
			sidecar := filepath.Join(r.auditFolder(), "signatures.jsonl")
			if err := os.WriteFile(sidecar, []byte(readFile(t, sidecar)+tc.line), 0o600); err != nil {
				t.Fatal(err)
			}
			before := r.snapshot(t)
			_, ts, logged := r.restart(t)
			if after := r.snapshot(t); after != before {
				t.Errorf("the start changed what it could not tell about:\n%s\n(%s)", after, logged)
			}
			answer, _ := panelOn(t, ts, id)
			if answer.Rotation.State != rotationUnfinished || answer.Rotation.Reason != tc.why {
				t.Errorf("the panel says %+v, want %q", answer.Rotation, tc.why)
			}
		})
	}
}

// **A replaced signing folder makes no key and rotates nothing.** Replaced
// once the marker is written, the runtime is never asked for a key; replaced
// while it makes one, the key is found not to be where the desk names it, and
// is removed from the folder Desk holds, with the marker. Neither rotates, and
// nothing is written into the folder swapped in.
func TestARotationThroughAReplacedSigningFolderMakesNothing(t *testing.T) {
	for _, tc := range []struct{ at, why string }{
		{"rotation: marker written", "Nothing was rotated: Desk's signing folder was replaced before the runtime made the next key, so no key was made."},
		{"rotation: next generated", "Nothing was rotated: Desk's signing folder was replaced while the runtime made the next key."},
	} {
		t.Run(tc.at, func(t *testing.T) {
			r := newRotationRig(t, "c8000000000000000000000000000002", "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			token := r.token(t)
			r.rig.ran(t)
			var aside string
			testHookKeyBetween = func(at string) {
				if at == tc.at {
					aside = swapSigningFolder(t, r.signing)
				}
			}
			t.Cleanup(func() { testHookKeyBetween = nil })
			status, data := r.rotate(t, token)
			testHookKeyBetween = nil
			if status != http.StatusConflict || refusalOf(data) != tc.why {
				t.Errorf("the rotation answered %d %q, want %q", status, refusalOf(data), tc.why)
			}
			if aside == "" {
				t.Fatal("the swap was never made")
			}
			for _, call := range r.rig.ran(t) {
				if strings.HasPrefix(call, "audit key rotate") || tc.at == "rotation: marker written" && strings.HasPrefix(call, "audit key generate") {
					t.Errorf("a replaced folder ran %q", call)
				}
			}
			if names := namesIn(t, r.signing); len(names) != 0 {
				t.Errorf("the folder swapped in holds %q", names)
			}
			if names := namesIn(t, aside); !slices.Equal(names, []string{r.id + keysSuffix, r.id + seedSuffix}) {
				t.Errorf("the folder Desk held keeps %q", names)
			}
		})
	}
}

// **An answer that is not the rotation asked for is not taken for one.** The
// runtime exits 0 with an answer of another command, another status, from
// another key, to another key, or at no record, and writes no line: Desk reads
// the sidecar, finds no rotation to the next key, and changes nothing.
func TestAnAnswerThatIsNotTheRotationAskedForIsNotTakenForOne(t *testing.T) {
	answer := func(change func(map[string]any)) string {
		value := map[string]any{"outputVersion": "2", "command": "audit key rotate", "status": "rotated", "at": 1, "trail": fixtureTrail,
			"from": standInKeyID, "next": secondKeyID, "nextPublicKey": secondPublicKey}
		change(value)
		data, _ := json.Marshal(value)
		return string(data)
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"another command", func(v map[string]any) { v["command"] = "audit key generate" }},
		{"another status", func(v map[string]any) { v["status"] = "generated" }},
		{"from another key", func(v map[string]any) { v["from"] = thirdKeyID }},
		{"to another key", func(v map[string]any) { v["nextPublicKey"] = thirdPublicKey }},
		{"to another keyId", func(v map[string]any) { v["next"] = thirdKeyID }},
		{"at no record", func(v map[string]any) { v["at"] = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRotationRig(t, "c8000000000000000000000000000003", "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			rotatingAs(t, r.rig.calls, refusingWith(answer(tc.change), 0))
			status, data := r.rotate(t, r.token(t))
			if status != http.StatusConflict || refusalOf(data) != "The runtime did not rotate the key, and nothing was changed: Desk kept the current key. It said: its audit key rotate did not answer as documented." {
				t.Errorf("the rotation answered %d %s", status, data)
			}
			r.assertUnchanged(t)
		})
	}
}

// **What changed while a rotation was finished is not written over, and a
// rotation is never said before its effect.** The runtime wrote its line;
// then the list of keys, or the current seed, is replaced before Desk writes
// or renames over it, or the marker after the rename. Desk does not write over
// either, and answers that the rotation did not finish, never that the key was
// rotated, saying that the next key signs only once it is the one the desk
// names. The panel, read afresh, then says a rotation did not finish, and
// passes the next key only where it is in force; the next start finishes it.
func TestWhatChangedWhileARotationFinishedIsNotWrittenOver(t *testing.T) {
	// Each is replaced by another file, its old one kept aside so that its
	// file is not freed and its number given to the new one.
	replace := func(t *testing.T, path, data string) {
		t.Helper()
		if os.Rename(path, path+".aside") != nil || os.WriteFile(path, []byte(data), 0o600) != nil {
			t.Fatal("could not replace " + filepath.Base(path))
		}
	}
	notRenamed := regexp.MustCompile(`^The rotation of this desk's key did not finish: the runtime wrote it, and Desk could not finish it: .+\. Records written until it is finished may be unsigned\. Desk finishes it when it next starts, and the decision record says a rotation did not finish\.$`)
	renamed := regexp.MustCompile(`^The rotation of this desk's key did not finish: the next key now signs the records after record 1, and its marker could not be removed: .+\. Desk removes the marker when it next starts, and starts no other rotation until then\.$`)
	for _, tc := range []struct {
		name, at string
		change   func(t *testing.T, r *rotationRig)
		answer   *regexp.Regexp
		left     string
		reason   string
		// passed is the keys the panel passes after the failure, or nil
		// where it passes none.
		passed []deskPublicKey
	}{
		{"the list", "rotation: line written", func(t *testing.T, r *rotationRig) {
			replace(t, filepath.Join(r.signing, r.id+keysSuffix), wantKeyLine(standInPublicKey, standInKeyID, 0))
		}, notRenamed, ".keys.jsonl,.keys.jsonl.aside,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=1",
			"The runtime wrote the rotation, and Desk did not finish it: Desk finishes it when it next starts. Until then, records are written unsigned.", nil},
		{"the seed", "rotation: list written", func(t *testing.T, r *rotationRig) {
			replace(t, filepath.Join(r.signing, r.id+seedSuffix), standInSeed+"\n")
		}, notRenamed, ".keys.jsonl,.next.seed,.rotating,.seed,.seed.aside seed=1 next=2 keys=2 rotations=1",
			"The runtime wrote the rotation, and Desk did not finish it: Desk finishes it when it next starts. Until then, records are written unsigned.", nil},
		{"the marker, after the rename", "rotation: seed renamed", func(t *testing.T, r *rotationRig) {
			replace(t, filepath.Join(r.signing, r.id+rotatingSuffix), "")
		}, renamed, ".keys.jsonl,.rotating,.rotating.aside,.seed seed=2 next=absent keys=2 rotations=1",
			"Nothing of it is left to do but remove its marker, which Desk does when it next starts.", []deskPublicKey{key1, {secondPublicKey, secondKeyID, 1}}},
		// Issue #239: the old list put back once the new one is written, the
		// list edited in place, the same file with other bytes, and the next
		// key replaced just before the rename. None is renamed against, and
		// the marker and the current seed stay.
		{"the list, put back after it was written", "rotation: list written", func(t *testing.T, r *rotationRig) {
			replace(t, filepath.Join(r.signing, r.id+keysSuffix), wantKeyLine(standInPublicKey, standInKeyID, 0))
		}, notRenamed, ".keys.jsonl,.keys.jsonl.aside,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=1",
			"The runtime wrote the rotation, and Desk did not finish it: Desk finishes it when it next starts. Until then, records are written unsigned.", nil},
		{"the list, edited in place", "rotation: line written", func(t *testing.T, r *rotationRig) {
			if err := os.WriteFile(filepath.Join(r.signing, r.id+keysSuffix), []byte(wantKeyLine(thirdPublicKey, thirdKeyID, 0)), 0o600); err != nil {
				t.Fatal(err)
			}
		}, notRenamed, ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=1",
			"Desk cannot tell whether the runtime wrote the rotation, so it changes nothing: the list of this desk's public keys ends in neither the current key nor the next one.", nil},
		{"the next key, before the rename", "rotation: list written", func(t *testing.T, r *rotationRig) {
			replace(t, r.nextPath(), secondSeed+"\n")
		}, notRenamed, ".keys.jsonl,.next.seed,.next.seed.aside,.rotating,.seed seed=1 next=2 keys=2 rotations=1",
			"The runtime wrote the rotation, and Desk did not finish it: Desk finishes it when it next starts. Until then, records are written unsigned.", nil},
		// Review round 1: the check before the rename holds the list to its
		// file and to its bytes, each alone. The list written is rolled back
		// in place, the same file with the old bytes; and replaced by another
		// file with the very bytes written.
		{"the list, rolled back in place after it was written", "rotation: list written", func(t *testing.T, r *rotationRig) {
			if err := os.WriteFile(filepath.Join(r.signing, r.id+keysSuffix), []byte(wantKeyLine(standInPublicKey, standInKeyID, 0)), 0o600); err != nil {
				t.Fatal(err)
			}
		}, notRenamed, ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=1",
			"The runtime wrote the rotation, and Desk did not finish it: Desk finishes it when it next starts. Until then, records are written unsigned.", nil},
		{"the list, replaced by the same bytes after it was written", "rotation: list written", func(t *testing.T, r *rotationRig) {
			list := filepath.Join(r.signing, r.id+keysSuffix)
			replace(t, list, readFile(t, list))
		}, notRenamed, ".keys.jsonl,.keys.jsonl.aside,.next.seed,.rotating,.seed seed=1 next=2 keys=2 rotations=1",
			"The runtime wrote the rotation, and Desk did not finish it: Desk finishes it when it next starts. Until then, records are written unsigned.", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRotationRig(t, "c8000000000000000000000000000004", "")
			r.writeTrail(t, 1, recordLine(standInKeyID, 1))
			token := r.token(t)
			r.rig.keysSeen(t)
			testHookKeyBetween = func(at string) {
				if at == tc.at {
					tc.change(t, r)
				}
			}
			t.Cleanup(func() { testHookKeyBetween = nil })
			status, data := r.rotate(t, token)
			testHookKeyBetween = nil
			said := refusalOf(data)
			if status != http.StatusInternalServerError || !tc.answer.MatchString(said) || strings.Contains(said, "rotated") || strings.Contains(string(data), `"state":"rotated"`) {
				t.Errorf("the rotation answered %d %s", status, data)
			}
			if got := r.describe(t); got != tc.left {
				t.Errorf("the signing folder holds %s, want %s", got, tc.left)
			}
			answer, _ := r.panel(t)
			if answer.Rotation.State != rotationUnfinished || answer.Rotation.Reason != tc.reason {
				t.Errorf("the panel says %+v, want unfinished: %q", answer.Rotation, tc.reason)
			}
			if tc.passed == nil {
				if answer.Keys.State == keysKept || r.rig.keysSeen(t) != nil {
					t.Errorf("the panel passed keys %+v while the rotation is not finished", answer.Keys)
				}
			} else if answer.Keys.State != keysKept || !slices.Equal(answer.Keys.Public, tc.passed) {
				t.Errorf("the panel shows the keys %+v, want %+v", answer.Keys, tc.passed)
			}
		})
	}
}

// **A start renames no key against a list put back since it inspected it**
// (issue #239). A rotation stops once its list is written. At the next start,
// after the recovery has inspected that list as written, the old list is put
// back in its place: the recovery does not rename the next key over the
// current one, and keeps the marker, so the panel says a rotation did not
// finish, where it would otherwise pass no key and offer nothing to finish.
// The start after it, with nothing changed under it, finishes the rotation.
func TestAStartRenamesNoKeyAgainstAListPutBackSinceItsInspection(t *testing.T) {
	r := newRotationRig(t, "c6000000000000000000000000000002", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	r.abandonRotation(t, "rotation: list written")
	if got := r.describe(t); got != ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=2 rotations=1" {
		t.Fatalf("the stop left %s", got)
	}
	list := filepath.Join(r.signing, r.id+keysSuffix)
	testHookKeyBetween = func(at string) {
		if at == "rotation: inspected" {
			if os.Rename(list, list+".aside") != nil || os.WriteFile(list, []byte(wantKeyLine(standInPublicKey, standInKeyID, 0)), 0o600) != nil {
				t.Error("could not put the old list back")
			}
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	again, ts, logged := r.restart(t)
	testHookKeyBetween = nil
	if got := r.describe(t); got != ".keys.jsonl,.keys.jsonl.aside,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=1" {
		t.Errorf("after the start the signing folder holds %s, want the seed, the next key and the marker as the stop left them (%s)", got, logged)
	}
	if !strings.Contains(logged.String(), "could not be finished") {
		t.Errorf("Desk's log does not say the rotation was not finished: %s", logged)
	}
	answer, _ := panelOn(t, ts, r.id)
	if answer.Rotation.State != rotationUnfinished || answer.Rotation.Reason != "The runtime wrote the rotation, and Desk did not finish it: Desk finishes it when it next starts. Until then, records are written unsigned." {
		t.Errorf("after the start the panel says %+v", answer.Rotation)
	}

	ts.Close()
	last, logged := restartedServer(t, again)
	ts = httptest.NewServer(last)
	t.Cleanup(ts.Close)
	if got := r.describe(t); got != ".keys.jsonl,.keys.jsonl.aside,.seed seed=2 next=absent keys=2 rotations=1" {
		t.Errorf("after the start after it the signing folder holds %s (%s)", got, logged)
	}
	answer, _ = panelOn(t, ts, r.id)
	if answer.Keys.State != keysKept || !slices.Equal(answer.Keys.Public, []deskPublicKey{key1, {secondPublicKey, secondKeyID, 1}}) || answer.Rotation.State == rotationUnfinished {
		t.Errorf("after the start after it the panel shows %+v and %+v", answer.Keys, answer.Rotation)
	}
}

// **A panic in a rotation leaves the desk working** (issue #239). The
// request's handler panics once the marker is written, as a fault in it
// would, and net/http recovers it. The desk's key lock is released with the
// signing folder's: on the same Desk, with no restart, the decision record
// answers and says a rotation did not finish, and a rotation is refused for
// that, not left waiting.
func TestAPanicInARotationLeavesTheDeskWorking(t *testing.T) {
	r := newRotationRig(t, "c8000000000000000000000000000005", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	r.abandonRotation(t, "rotation: marker written")
	// The lock is asked first: a request would otherwise wait on it for good.
	if !r.s.desks[r.id].keyMu.TryLock() {
		t.Fatal("the desk's key lock is still held after the panic was recovered")
	}
	r.s.desks[r.id].keyMu.Unlock()
	answer, _ := r.panel(t)
	if answer.Rotation.State != rotationUnfinished || answer.Rotation.Reason != "Nothing of it is left to do but remove its marker, which Desk does when it next starts." {
		t.Errorf("after the panic the panel says %+v", answer.Rotation)
	}
	if answer.Keys == nil || answer.Keys.State != keysKept || !slices.Equal(answer.Keys.Public, []deskPublicKey{key1}) {
		t.Errorf("after the panic the panel shows the keys %+v", answer.Keys)
	}
	status, data := r.rotate(t, strings.Repeat("a", 64))
	if status != http.StatusConflict || !strings.HasPrefix(refusalOf(data), "A rotation of this desk's key did not finish, so Desk starts no other.") {
		t.Errorf("a rotation after the panic answered %d %s", status, data)
	}
}

// stalledWriter is a response writer whose writes wait until release is
// closed, as a client that stops reading holds a writer; writing is closed
// once a write waits.
type stalledWriter struct {
	header           http.Header
	writing, release chan struct{}
	once             sync.Once
}

func (w *stalledWriter) Header() http.Header { return w.header }
func (w *stalledWriter) WriteHeader(int)     {}
func (w *stalledWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.writing) })
	<-w.release
	return len(p), nil
}

// **A client that stops reading holds no key lock** (issue #239, review
// round 1). The answer to a rotation is written once the rotation is over and
// the desk's key lock released: while the writer of that answer waits, the
// decision record answers, and so does another rotation.
func TestAStalledAnswerToARotationHoldsNoKeyLock(t *testing.T) {
	r := newRotationRig(t, "c8000000000000000000000000000006", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	stalled := &stalledWriter{header: http.Header{}, writing: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(stalled.release) }) })
	request := httptest.NewRequest("POST", "/api/audit/key/rotate", strings.NewReader(`{"token":"`+strings.Repeat("a", 64)+`"}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Jpack-Desk", r.id)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.s.ServeHTTP(stalled, request)
	}()
	select {
	case <-stalled.writing:
	case <-done:
		t.Fatal("the rotation answered without writing")
	case <-time.After(30 * time.Second):
		t.Fatal("the rotation's answer was never written")
	}
	// The lock is asked first: a request would otherwise wait on it for good.
	if !r.s.desks[r.id].keyMu.TryLock() {
		t.Fatal("the desk's key lock is held while the rotation's answer is written")
	}
	r.s.desks[r.id].keyMu.Unlock()
	if answer, data := r.panel(t); answer.Rotation.State != rotationAvailable {
		t.Errorf("while an answer waits the panel says %s", data)
	}
	if status, data := r.rotate(t, strings.Repeat("a", 64)); status != http.StatusConflict || refusalOf(data) != "This desk's keys changed after the decision record showed them, so nothing was rotated. Check the decision record again." {
		t.Errorf("while an answer waits another rotation answered %d %s", status, data)
	}
	release.Do(func() { close(stalled.release) })
	<-done
}

// **A refused rotation whose sidecar cannot be read is left as it is.** The
// runtime refuses, and the sidecar, which says whether it wrote its line,
// cannot be read: the next key and the marker stay, nothing else changes,
// and the answer says Desk cannot tell.
func TestARefusalDeskCannotCheckLeavesTheNextKey(t *testing.T) {
	r := newRotationRig(t, "c8000000000000000000000000000001", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	token := r.token(t)
	rotatingAs(t, r.rig.calls, refusingWith(rotateNotInForce, 1))
	// The sidecar is given a second name once the runtime has answered, so
	// the offer, which reads it, is made.
	testHookKeyBetween = func(at string) {
		if at == "rotation: line written" {
			if err := os.Link(filepath.Join(r.auditFolder(), "signatures.jsonl"), filepath.Join(r.auditFolder(), "copy.jsonl")); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	status, data := r.rotate(t, token)
	testHookKeyBetween = nil
	want := "The runtime did not answer as asked (The project's signing key is not the key in force in the signature sidecar, so it cannot hand signing over; only the key in force can), and Desk cannot tell whether it wrote the rotation: the trail's signature sidecar could not be read: it, or the trail it is read beside, has another name as well, a hard link. Desk changed nothing, and the decision record says a rotation did not finish."
	if status != http.StatusInternalServerError || refusalOf(data) != want {
		t.Errorf("the rotation answered %d %q, want %q", status, refusalOf(data), want)
	}
	if got := r.describe(t); got != ".keys.jsonl,.next.seed,.rotating,.seed seed=1 next=2 keys=1 rotations=0" {
		t.Errorf("the signing folder holds %s", got)
	}
}

// **The start acts only on a desk's own marker.** A marker in a folder below
// the signing folder, one whose name is not a desk's id, and one of a desk
// the registry did not open are each left, with everything beside them.
func TestTheStartActsOnlyOnADesksOwnMarker(t *testing.T) {
	r := newRotationRig(t, "c9000000000000000000000000000001", "")
	r.writeTrail(t, 1, recordLine(standInKeyID, 1))
	runner := filepath.Join(r.signing, "runner")
	if err := os.Mkdir(runner, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(runner, r.id+rotatingSuffix), filepath.Join(runner, r.id+nextSeedSuffix),
		filepath.Join(r.signing, "notadesk"+rotatingSuffix),
		filepath.Join(r.signing, "c9000000000000000000000000000002"+rotatingSuffix),
		filepath.Join(r.signing, "c9000000000000000000000000000002"+nextSeedSuffix),
	} {
		if err := os.WriteFile(path, []byte(secondSeed+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := r.snapshot(t) + strings.Join(namesIn(t, runner), ",")
	_, _, logged := r.restart(t)
	if after := r.snapshot(t) + strings.Join(namesIn(t, runner), ","); after != before {
		t.Errorf("the start changed\n%s\ninto\n%s", before, after)
	}
	if !strings.Contains(logged.String(), "desk c9000000000000000000000000000002's key was left as it is, because that desk is not open") {
		t.Errorf("Desk's log does not name the marker it left: %s", logged)
	}
}

// requireRotationWithTheRuntime is the panel's token on ts for desk id,
// failing the test unless the panel offers a rotation.
func requireRotationWithTheRuntime(t *testing.T, ts *httptest.Server, id string) string {
	t.Helper()
	answer, data := panelOn(t, ts, id)
	if answer.Rotation.State != rotationAvailable {
		t.Fatalf("the panel offers no rotation: %s", data)
	}
	return answer.Rotation.Token
}

// **With the runtime: a rotation, the records after it signed by the next
// key, and the runtime's own check agreeing.** A rotation before any record is
// refused in the runtime's words and leaves nothing. After a signed deciding
// run, the desk's key is rotated; a second run is signed by the next key; the
// panel passes both keys, in order, and reports both records signed and one
// rotation; and the runtime's own `audit verify --public-key k1 --public-key
// k2`, run apart from Desk, agrees. A second rotation stopped after the
// runtime wrote its line is finished at the next start, and a third run is
// then signed by the third key, which the runtime's own check confirms. With a
// runtime that does not read "6", the desk is unsigned and no rotation is
// offered.
func TestRotatingADesksKeyWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	s, ts, _ := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	if !row.Signed {
		// A runtime that does not read "6": the panel says the runtime has no
		// audit commands, and offers nothing; a request is refused, and says
		// why.
		status, data := reviewCall(t, ts, "GET", "/api/audit/verify", row.ID, nil, bearer)
		var answer auditAnswer
		if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.State != auditStateOlder || answer.Rotation != nil {
			t.Errorf("an unsigned desk's panel answered %d %s", status, data)
		}
		status, data = reviewCall(t, ts, "POST", "/api/audit/key/rotate", row.ID, map[string]string{"token": strings.Repeat("a", 64)}, bearer)
		if status != http.StatusConflict || !strings.Contains(refusalOf(data), "does not read configVersion 6 and has no audit key rotate, so Desk rotates no key with it. A runtime of 0.26.0 or later has it.") {
			t.Errorf("an unsigned desk's rotation answered %d %s", status, data)
		}
		if names := namesIn(t, signingFolderOf(s)); names != nil {
			t.Errorf("a refused rotation left %q", names)
		}
		return
	}
	signing := signingFolderOf(s)
	seed := filepath.Join(signing, row.ID+seedSuffix)
	publicOf := func(t *testing.T) deskPublicKey {
		t.Helper()
		var key deskPublicKey
		if err := json.Unmarshal(jpackIn(t, bin, row.Folder, "audit", "key", "public", seed, "--format", "json"), &key); err != nil {
			t.Fatal(err)
		}
		return key
	}
	k1 := publicOf(t)
	rotate := func(t *testing.T, ts *httptest.Server) (int, []byte) {
		t.Helper()
		return reviewCall(t, ts, "POST", "/api/audit/key/rotate", row.ID, map[string]string{"token": requireRotationWithTheRuntime(t, ts, row.ID)}, bearer)
	}

	// Before any record: the runtime's refusal, and nothing left.
	status, data := rotate(t, ts)
	if status != http.StatusConflict || refusalOf(data) != "The runtime did not rotate the key, and nothing was changed: Desk kept the current key. It said: The trail has no chained record yet, so there is no trail to rotate the key of; the first record is signed with whichever key the project names." {
		t.Errorf("a rotation before any record answered %d %s", status, data)
	}
	if names := namesIn(t, signing); !slices.Equal(names, []string{row.ID + keysSuffix, row.ID + seedSuffix}) {
		t.Errorf("a refused rotation left %q", names)
	}
	if got := readFile(t, filepath.Join(signing, row.ID+keysSuffix)); got != wantKeyLine(k1.PublicKey, k1.KeyID, 0) {
		t.Errorf("a refused rotation left the list %q", got)
	}
	if publicOf(t) != k1 {
		t.Error("a refused rotation changed the seed")
	}

	// A deciding run, as an outside caller makes one.
	config := strings.Replace(wantSignedConfig(s.configDir, row.ID), `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": reviewPack, "jpack.json": config})
	jpackIn(t, bin, row.Folder, "packs", "lock", "--config", "jpack.json", "--format", "json")
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	decide := func(t *testing.T) {
		t.Helper()
		jpackIn(t, bin, row.Folder, "experimental", "evaluate", "--config", "jpack.json", "--pack-id", "alpha", "--facts", facts, "--format", "json")
	}
	decide(t)

	status, data = rotate(t, ts)
	var answer rotationAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.At != 1 || answer.From != k1 {
		t.Fatalf("the rotation answered %d %s", status, data)
	}
	k2 := publicOf(t)
	if k2 == k1 || answer.Next != (deskPublicKey{k2.PublicKey, k2.KeyID, 1}) {
		t.Errorf("the rotation answered %+v; the seed now holds %+v", answer, k2)
	}
	if got := readFile(t, filepath.Join(signing, row.ID+keysSuffix)); got != wantKeyLine(k1.PublicKey, k1.KeyID, 0)+wantKeyLine(k2.PublicKey, k2.KeyID, 1) {
		t.Errorf("the list is %q", got)
	}
	if names := namesIn(t, signing); !slices.Equal(names, []string{row.ID + keysSuffix, row.ID + seedSuffix}) {
		t.Errorf("the rotation left %q", names)
	}
	decide(t)

	verifyAs := func(t *testing.T, keys []deskPublicKey, records int64) {
		t.Helper()
		args := []string{"audit", "verify", "--config", "jpack.json", "--format", "json"}
		for i, key := range keys {
			held := filepath.Join(t.TempDir(), fmt.Sprintf("k%d.pub", i+1))
			if err := os.WriteFile(held, []byte(key.PublicKey+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			args = append(args, "--public-key", held)
		}
		cmd := exec.Command(bin, args...)
		cmd.Dir = row.Folder
		cmd.Env = append(os.Environ(), "JPACK_CONFIG=", "JPACK_SIGNING_KEY=")
		out, err := cmd.Output()
		var own struct {
			Status   string `json:"status"`
			Coverage struct {
				Signed        auditCoverageState `json:"signed"`
				SignedRecords int64              `json:"signedRecords"`
			} `json:"coverage"`
			Signatures auditSignatures `json:"signatures"`
		}
		if err != nil || json.Unmarshal(out, &own) != nil || own.Status != "valid" || own.Coverage.Signed != (auditCoverageState{Status: "through", Through: records}) ||
			own.Coverage.SignedRecords != records || own.Signatures.Rotations != int64(len(keys)-1) || own.Signatures.KeyInForce != keys[len(keys)-1].KeyID {
			t.Errorf("the runtime's own audit verify with %d keys answered %v %s", len(keys), err, out)
		}
	}

	panel, panelData := panelOn(t, ts, row.ID)
	report := panel.Report
	if report == nil || report.Status != "valid" || report.Coverage.Signed != (auditCoverageState{Status: "through", Through: 2}) || report.Coverage.SignedRecords != 2 ||
		report.Signatures == nil || report.Signatures.Rotations != 1 || report.Signatures.KeysSupplied != 2 || report.Signatures.FirstKey != k1.KeyID || report.Signatures.KeyInForce != k2.KeyID {
		t.Errorf("the panel reports %s", panelData)
	}
	if panel.Keys == nil || panel.Keys.State != keysKept || !slices.Equal(panel.Keys.Public, []deskPublicKey{{k1.PublicKey, k1.KeyID, 0}, {k2.PublicKey, k2.KeyID, 1}}) {
		t.Errorf("the panel shows the keys %+v", panel.Keys)
	}
	if strings.Contains(panelData, s.configDir) {
		t.Errorf("the panel names a path: %s", panelData)
	}
	verifyAs(t, []deskPublicKey{k1, k2}, 2)

	// A second rotation, stopped after the runtime wrote its line, and the
	// next start.
	token := requireRotationWithTheRuntime(t, ts, row.ID)
	testHookKeyBetween = func(at string) {
		if at == "rotation: line written" {
			panic(http.ErrAbortHandler)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	request, _ := http.NewRequest("POST", ts.URL+"/api/audit/key/rotate", strings.NewReader(`{"token":"`+token+`"}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Jpack-Desk", row.ID)
	if response, err := http.DefaultClient.Do(request); err == nil {
		response.Body.Close()
	}
	testHookKeyBetween = nil
	if names := namesIn(t, signing); !slices.Equal(names, []string{row.ID + keysSuffix, row.ID + nextSeedSuffix, row.ID + rotatingSuffix, row.ID + seedSuffix}) {
		t.Fatalf("the stop left %q", names)
	}
	ts.Close()
	again, logged := restartedServer(t, s)
	ts2 := httptest.NewServer(again)
	t.Cleanup(ts2.Close)
	if names := namesIn(t, signing); !slices.Equal(names, []string{row.ID + keysSuffix, row.ID + seedSuffix}) {
		t.Fatalf("after the next start the signing folder holds %q (%s)", names, logged)
	}
	k3 := publicOf(t)
	if got := readFile(t, filepath.Join(signing, row.ID+keysSuffix)); got != wantKeyLine(k1.PublicKey, k1.KeyID, 0)+wantKeyLine(k2.PublicKey, k2.KeyID, 1)+wantKeyLine(k3.PublicKey, k3.KeyID, 2) {
		t.Errorf("after the next start the list is %q", got)
	}
	decide(t)
	verifyAs(t, []deskPublicKey{k1, k2, k3}, 3)
	panel, panelData = panelOn(t, ts2, row.ID)
	if panel.Keys == nil || panel.Keys.State != keysKept || len(panel.Keys.Public) != 3 || panel.Report == nil || panel.Report.Coverage.SignedRecords != 3 {
		t.Errorf("after the next start the panel answered %s", panelData)
	}
}
