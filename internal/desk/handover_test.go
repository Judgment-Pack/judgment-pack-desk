//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package desk

// Checkpoint hand-over by download or copy (ADR-0010, section 2; delivery
// row 5). A stand-in runtime, by absolute path, answers `audit checkpoint`
// from a file of checkpoint lines each test prepares, as runtime 0.27.1 does:
// the JSON form with the last one, and `--since`/`--limit` in the human form,
// with a note on standard error where more follow. Its lines put their
// members in another order than the runtime's, with a space and an escape, so
// that anything that decodes and encodes them again changes their bytes. The
// last tests drive the real runtime and skip without one.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Two trail identities: the trail's, and the one it has after it was moved
// aside.
const (
	handoverTrail = "9a5ef41d74e7d7e003c8a34cff056351"
	movedTrail    = "c962ef5fa560f62c67bd4c1c29e011e3"
)

// handoverNow is the clock the tests fix: 2026-10-05T12:00:00Z.
const handoverNow = 1791201600

// checkpointOf is the checkpoint of the record at sequence in trail, as the
// stand-in prints it, with its newline.
func checkpointOf(trail string, sequence int) string {
	return fmt.Sprintf(`{"sequence":%d, "trail":"%s","recordDigest":"sha256:%064x","checkpointVersion":"\u0031"}`, sequence, trail, sequence) + "\n"
}

// chainOf is the checkpoints of the records at sequences in trail, in order.
func chainOf(trail string, sequences ...int) string {
	var out strings.Builder
	for _, sequence := range sequences {
		out.WriteString(checkpointOf(trail, sequence))
	}
	return out.String()
}

// upTo is the sequences 1 to n.
func upTo(n int) []int {
	sequences := make([]int, n)
	for i := range sequences {
		sequences[i] = i + 1
	}
	return sequences
}

// noChainedRecord is runtime 0.27.1's answer, exit 1, for a trail with no
// chained record.
const noChainedRecord = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit checkpoint","status":"error","diagnostics":[{"code":"JPS-AUDIT-CHECKPOINT-NONE","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"The trail has no chained record to checkpoint."}]}`

// checkpointRefused is runtime 0.27.1's answer, exit 1, for a trail that
// fails a check.
const checkpointRefused = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.27.1"},"command":"audit checkpoint","status":"error","diagnostics":[{"code":"JPS-AUDIT-CHECKPOINT-REFUSED","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":"The trail fails 1 check(s), the first incomplete-last-line at line 4, so no checkpoint is given for it; jpack audit verify lists them."}]}`

// checkpointCase is the stand-in's `audit checkpoint`. Its JSON form, with no
// `--since`, prints `<calls>.head` and exits with `<calls>.head.exit` where
// they exist; its `--since` runs `<calls>.since`, a shell fragment, where it
// exists. Otherwise each answers from `<calls>.lines`, the checkpoints of the
// chained records in order, as the runtime does: the last, or those after
// `--since`, at most `--limit`, with a note on standard error where more
// follow; and with no lines, that the trail has no chained record.
func checkpointCase(calls string) string {
	lines := "'" + calls + ".lines'"
	return "'audit checkpoint')\n" +
		"  since=; limit=1000; format=human; prev=\n" +
		"  for arg do case \"$prev\" in --since) since=$arg ;; --limit) limit=$arg ;; --format) format=$arg ;; esac; prev=$arg; done\n" +
		"  if [ -z \"$since\" ] && [ -e '" + calls + ".head' ]; then\n" +
		"    while IFS= read -r line || [ -n \"$line\" ]; do printf '%s\\n' \"$line\"; done < '" + calls + ".head'\n" +
		"    IFS= read -r code < '" + calls + ".head.exit'; exit \"$code\"\n" +
		"  fi\n" +
		"  if [ -n \"$since\" ] && [ -e '" + calls + ".since' ]; then . '" + calls + ".since'; exit $?; fi\n" +
		"  if [ ! -s " + lines + " ]; then\n" +
		"    if [ \"$format\" = json ]; then printf '%s\\n' '" + noChainedRecord + "'; else printf 'error: The trail has no chained record to checkpoint.\\n' >&2; fi\n" +
		"    exit 1\n" +
		"  fi\n" +
		"  if [ -z \"$since\" ]; then\n" +
		"    last=; while IFS= read -r line; do last=$line; done < " + lines + "\n" +
		"    if [ \"$format\" = json ]; then printf '{\"outputVersion\":\"2\",\"tool\":{\"name\":\"jpack\",\"version\":\"0.27.1\"},\"command\":\"audit checkpoint\",\"status\":\"checkpointed\",\"trailPath\":\"/project/.desk-private/audit/evaluations.jsonl\",\"checkpoint\":%s,\"uncoveredLines\":0}\\n' \"$last\"; else printf '%s\\n' \"$last\"; fi\n" +
		"    exit 0\n" +
		"  fi\n" +
		"  n=0; more=; last=$since\n" +
		"  while IFS= read -r line; do\n" +
		"    seq=${line#*\\\"sequence\\\":}; seq=${seq%%,*}\n" +
		"    [ \"$seq\" -gt \"$since\" ] || continue\n" +
		"    if [ \"$n\" -ge \"$limit\" ]; then more=1; break; fi\n" +
		"    printf '%s\\n' \"$line\"; n=$((n+1)); last=$seq\n" +
		"  done < " + lines + "\n" +
		"  if [ -n \"$more\" ]; then printf 'note: more chained records follow; ask again with --since %s\\n' \"$last\" >&2; fi\n" +
		"  exit 0\n" +
		"  ;;\n"
}

// handoverRig is a startup desk over a project that keeps a trail, with a
// stand-in runtime that reads "6" and answers `audit checkpoint` and `audit
// verify`, and the project's folder.
type handoverRig struct {
	*auditRig
	s       *Server
	ts      *httptest.Server
	project string
	logged  *bytes.Buffer
}

// withCheckpoints adds checkpointCase to the stand-in at bin.
func withCheckpoints(t *testing.T, bin, calls string) {
	t.Helper()
	script, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	script = bytes.Replace(script, []byte("'packs lock')\n"), []byte(checkpointCase(calls)+"'packs lock')\n"), 1)
	if err := os.WriteFile(bin, script, 0o755); err != nil {
		t.Fatal(err)
	}
}

// fixHandover fixes Desk's clock, and the ids of the next holders.
func fixHandover(t *testing.T, ids ...string) {
	t.Helper()
	clock, made := handoverClock, newHolderID
	handoverClock = func() time.Time { return time.Unix(handoverNow, 0) }
	next := 0
	newHolderID = func() (string, error) {
		if next == len(ids) {
			return made()
		}
		next++
		return ids[next-1], nil
	}
	t.Cleanup(func() { handoverClock, newHolderID = clock, made })
}

func newHandoverRig(t *testing.T, ids ...string) *handoverRig {
	t.Helper()
	t.Setenv("JPACK_CONFIG", "")
	fixHandover(t, ids...)
	rig := &handoverRig{auditRig: newAuditRig(t, withAuditVersions), project: t.TempDir(), logged: &bytes.Buffer{}}
	withCheckpoints(t, rig.bin, rig.calls)
	rig.answers(t, 0, auditValidReport)
	writeProject(t, rig.project, map[string]string{"jpack.json": auditedConfig})
	if err := os.MkdirAll(filepath.Join(rig.project, ".desk-private", "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	rig.s, rig.ts = startDesk(t, Config{ProjectDir: rig.project, JpackBin: rig.bin, Token: testToken, Logger: log.New(rig.logged, "", 0)})
	t.Cleanup(func() { rig.s.Close() })
	t.Cleanup(rig.ts.Close)
	return rig
}

// chain sets the checkpoints the stand-in gives.
func (r *handoverRig) chain(t *testing.T, lines string) {
	t.Helper()
	if err := os.WriteFile(r.calls+".lines", []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
}

// heads sets what the stand-in's JSON form prints, and its exit.
func (r *handoverRig) heads(t *testing.T, code int, body string) {
	t.Helper()
	answerAt(t, r.calls+".head", code, body)
}

// listing sets the shell fragment the stand-in's `--since` runs.
func (r *handoverRig) listing(t *testing.T, fragment string) {
	t.Helper()
	if err := os.WriteFile(r.calls+".since", []byte(fragment+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (r *handoverRig) handoverPath(parts ...string) string {
	return filepath.Join(append([]string{r.project, ".desk-private", "handover"}, parts...)...)
}

// addHolder adds a holder, and fails the test unless it is added.
func (r *handoverRig) addHolder(t *testing.T, label, channel string) holderAnswer {
	t.Helper()
	status, data := reviewCall(t, r.ts, "POST", "/api/audit/holders", "", map[string]string{"label": label, "channel": channel}, bearer)
	var holder holderAnswer
	if status != http.StatusCreated || json.Unmarshal(data, &holder) != nil {
		t.Fatalf("adding %q answered %d %s", label, status, data)
	}
	return holder
}

// holders is `GET /api/audit/holders`, and fails the test unless it answers.
func (r *handoverRig) holders(t *testing.T) holdersAnswer {
	t.Helper()
	status, data := reviewCall(t, r.ts, "GET", "/api/audit/holders", "", nil, bearer)
	var answer holdersAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil {
		t.Fatalf("the holders answered %d %s", status, data)
	}
	return answer
}

// download is `GET /api/audit/checkpoints` with query as its whole query.
func (r *handoverRig) download(t *testing.T, query string) (int, http.Header, []byte) {
	t.Helper()
	request, err := http.NewRequest("GET", r.ts.URL+"/api/audit/checkpoints?"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	bearer(request)
	response, err := r.ts.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("the download ended early: %v", err)
	}
	return response.StatusCode, response.Header, data
}

// downloaded is a download that must answer the checkpoints.
func (r *handoverRig) downloaded(t *testing.T, holder string) (http.Header, []byte) {
	t.Helper()
	status, header, data := r.download(t, "holder="+holder)
	if status != http.StatusOK {
		t.Fatalf("the download answered %d %s", status, data)
	}
	return header, data
}

// confirm is `POST /api/audit/holders/{holder}/confirm` with body.
func (r *handoverRig) confirm(t *testing.T, holder string, body any) (int, []byte) {
	t.Helper()
	return reviewCall(t, r.ts, "POST", "/api/audit/holders/"+holder+"/confirm", "", body, bearer)
}

// confirmation is the body that confirms the download whose headers are
// header.
func confirmation(t *testing.T, header http.Header) map[string]any {
	t.Helper()
	from, err1 := strconv.ParseInt(header.Get(checkpointsFromHeader), 10, 64)
	through, err2 := strconv.ParseInt(header.Get(checkpointsThroughHeader), 10, 64)
	if err1 != nil || err2 != nil {
		t.Fatalf("the download's headers name no records: %v", header)
	}
	return map[string]any{"trail": header.Get(checkpointsTrailHeader), "from": from, "through": through, "digest": header.Get(checkpointsDigestHeader)}
}

// confirmed confirms the download whose headers are header, and fails the
// test unless it is recorded.
func (r *handoverRig) confirmed(t *testing.T, holder string, header http.Header) holderAnswer {
	t.Helper()
	status, data := r.confirm(t, holder, confirmation(t, header))
	var answer holderAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	return answer
}

// snapshot is every file under the hand-over folder, by name, with its mode
// and bytes.
func (r *handoverRig) snapshot(t *testing.T) string {
	t.Helper()
	var out strings.Builder
	filepath.WalkDir(r.handoverPath(), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, _ := entry.Info()
		rel, _ := filepath.Rel(r.handoverPath(), path)
		fmt.Fprintf(&out, "%s %v", rel, info.Mode())
		if !entry.IsDir() {
			data, _ := os.ReadFile(path)
			fmt.Fprintf(&out, " %q", data)
		}
		out.WriteString("\n")
		return nil
	})
	return out.String()
}

// modeOf is the mode of path, not following a link, or why there is none.
func modeOf(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return err.Error()
	}
	return info.Mode().String()
}

// since is an entry's count of records since, or -1 where it has none, so
// that a test reads it without dereferencing a nil.
func since(entry holderTrailAnswer) int64 {
	if entry.Unwitnessed == nil {
		return -1
	}
	return *entry.Unwitnessed
}

// holderAt is the answer's holder at index, or a zero holder where there is
// none.
func holderAt(answer holdersAnswer, index int) holderAnswer {
	if index >= len(answer.Holders) {
		return holderAnswer{}
	}
	return answer.Holders[index]
}

// lastOf is the last of calls, or "" where there is none.
func lastOf(calls []string) string {
	if len(calls) == 0 {
		return ""
	}
	return calls[len(calls)-1]
}

func digestOfString(data string) string {
	sum := sha256.Sum256([]byte(data))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// checkpointCalls is the stand-in's `audit checkpoint` runs since the last
// call, in order, and forgets every run.
func (r *handoverRig) checkpointCalls(t *testing.T) []string {
	t.Helper()
	var calls []string
	for _, call := range r.ran(t) {
		if strings.HasPrefix(call, "audit checkpoint") {
			calls = append(calls, strings.TrimSuffix(call, " [JPACK_CONFIG=unset]"))
		}
	}
	return calls
}

const (
	headCall = "audit checkpoint --config jpack.json --format json"
	holderA  = "a1b2c3d4e5f60718"
	holderB  = "0f1e2d3c4b5a6978"
	holderC  = "1122334455667788"
)

func sinceCall(since int) string {
	return "audit checkpoint --config jpack.json --since " + strconv.Itoa(since) + " --limit 300"
}

// **A holder is the owner's words, kept by Desk.** Added by a label and a
// channel, trimmed, with an id of Desk's and Desk's clock; kept in an
// owner-only folder that ignores itself in Git, in an owner-only file; and
// listed with the trail as the runtime gives it, and nothing recorded yet.
func TestAHolderIsAddedInTheOwnersWords(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3))
	if listed := rig.holders(t); len(listed.Holders) != 0 || listed.Trail == nil || *listed.Trail != (checkpointHead{Identity: handoverTrail, Sequence: 3}) {
		t.Fatalf("before any holder, the holders are %+v", listed)
	}
	added := rig.addHolder(t, "  Counterparty: procurement desk ", "\te-mail to records@example.com\n")
	want := handoverHolder{ID: holderA, Label: "Counterparty: procurement desk", Channel: "e-mail to records@example.com", AddedAt: handoverNow}
	if added.handoverHolder != want || len(added.Trails) != 0 || added.OtherTrail {
		t.Errorf("the holder was added as %+v, want %+v", added, want)
	}
	listed := rig.holders(t)
	if len(listed.Holders) != 1 || listed.Holders[0].handoverHolder != want || len(listed.Holders[0].Trails) != 0 || listed.Holders[0].OtherTrail ||
		listed.Trail == nil || *listed.Trail != (checkpointHead{Identity: handoverTrail, Sequence: 3}) || listed.Diagnostics != nil {
		t.Errorf("the holders are %+v", listed)
	}
	for path, mode := range map[string]os.FileMode{rig.handoverPath(): 0o700 | os.ModeDir, rig.handoverPath(handoverHoldersName): 0o600, rig.handoverPath(".gitignore"): 0o600} {
		if got := modeOf(path); got != mode.String() {
			t.Errorf("%s is %s, want %v", filepath.Base(path), got, mode)
		}
	}
	if ignore := readFile(t, rig.handoverPath(".gitignore")); ignore != "*\n" {
		t.Errorf("the hand-over folder's .gitignore is %q", ignore)
	}
	var file holdersFile
	if err := json.Unmarshal([]byte(readFile(t, rig.handoverPath(handoverHoldersName))), &file); err != nil || file.Version != "1" || !slices.Equal(file.Holders, []handoverHolder{want}) {
		t.Errorf("holders.json holds %+v (%v)", file, err)
	}
}

// **A holder's label and channel are bounded.** Each is trimmed, and must
// then be 1 to 120 and 1 to 200 characters, with no control, format, line or
// paragraph separator character; a request that is not one JSON object of
// exactly those two members is refused; and so is a 51st holder. A refusal
// writes nothing.
func TestAHoldersWordsAreBounded(t *testing.T) {
	rig := newHandoverRig(t)
	rig.chain(t, chainOf(handoverTrail, 1))
	label, channel := strings.Repeat("é", holderLabelLimit), strings.Repeat("c", holderChannelLimit)
	for _, refused := range []struct{ name, body string }{
		{"an empty label", `{"label":"","channel":"e-mail"}`},
		{"a label of spaces", `{"label":"  \t ","channel":"e-mail"}`},
		{"an empty channel", `{"label":"Auditor","channel":" "}`},
		{"a label one character too long", `{"label":"` + label + `x","channel":"e-mail"}`},
		{"a channel one character too long", `{"label":"Auditor","channel":"` + channel + `x"}`},
		{"a control character", `{"label":"Audi\u0007tor","channel":"e-mail"}`},
		{"a newline inside", `{"label":"Audi\ntor","channel":"e-mail"}`},
		{"a line separator inside", `{"label":"Auditor","channel":"e-\u2028mail"}`},
		{"a format character", `{"label":"Audi\u202etor","channel":"e-mail"}`},
		{"a member it does not take", `{"label":"Auditor","channel":"e-mail","id":"a1b2c3d4e5f60718"}`},
		{"no channel", `{"label":"Auditor"}`},
		{"not JSON", `label=Auditor`},
	} {
		status, data := deskCall(t, rig.ts, "POST", "/api/audit/holders", "", refused.body, true)
		if status != http.StatusBadRequest || refusalOf(data) != holderTextWords {
			t.Errorf("%s: answered %d %s", refused.name, status, data)
		}
	}
	if _, err := os.Lstat(rig.handoverPath(handoverHoldersName)); !os.IsNotExist(err) {
		t.Errorf("a refused holder wrote the list: %v", err)
	}
	if added := rig.addHolder(t, label, channel); added.Label != label || added.Channel != channel {
		t.Errorf("a label and a channel at their bounds were kept as %q, %q", added.Label, added.Channel)
	}
	// Sent as anything but JSON, or from another site.
	status, data := reviewCall(t, rig.ts, "POST", "/api/audit/holders", "", nil, bearer, func(r *http.Request) {
		r.Body = io.NopCloser(strings.NewReader(`{"label":"Auditor","channel":"e-mail"}`))
		r.Header.Set("Content-Type", "text/plain")
	})
	if status != http.StatusUnsupportedMediaType {
		t.Errorf("a holder sent as text answered %d %s", status, data)
	}
	status, data = reviewCall(t, rig.ts, "POST", "/api/audit/holders", "", map[string]string{"label": "Auditor", "channel": "e-mail"}, bearer, func(r *http.Request) {
		r.Header.Set("Sec-Fetch-Site", "cross-site")
	})
	if status != http.StatusForbidden {
		t.Errorf("a holder sent from another site answered %d %s", status, data)
	}
	for i := 1; i < maxHolders; i++ {
		rig.addHolder(t, "Holder "+strconv.Itoa(i), "e-mail")
	}
	before := rig.snapshot(t)
	status, data = reviewCall(t, rig.ts, "POST", "/api/audit/holders", "", map[string]string{"label": "One more", "channel": "e-mail"}, bearer)
	if status != http.StatusConflict || refusalOf(data) != "Desk keeps at most 50 holders for one desk, so it adds no more." {
		t.Errorf("a 51st holder answered %d %s", status, data)
	}
	if rig.snapshot(t) != before {
		t.Error("a 51st holder changed Desk's record")
	}
	if listed := rig.holders(t); len(listed.Holders) != maxHolders {
		t.Errorf("Desk lists %d holders, want %d", len(listed.Holders), maxHolders)
	}
}

// **The bytes the runtime printed, untouched.** A download is `audit
// checkpoint --since <cursor> --limit 300` in the human form, after the JSON
// form for the trail's identity and last record: the lines as printed, in
// order, each with its newline, and none of the runtime's notes on standard
// error; saved as a file named for the trail and the records; with headers
// that name the trail, the cursor, the last record and the SHA-256 of the
// body.
func TestCheckpointsAreHandedOverAsTheRuntimePrintedThem(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	lines := chainOf(handoverTrail, 1, 2, 3)
	rig.chain(t, lines)
	rig.addHolder(t, "Auditor", "e-mail")
	rig.ran(t)
	// The runtime notes, on standard error, what is not part of the bytes.
	rig.listing(t, "printf 'note: the 1 line(s) after sequence 3 are not chained\\n' >&2\n"+
		"while IFS= read -r line; do printf '%s\\n' \"$line\"; done < '"+rig.calls+".lines'")
	header, data := rig.downloaded(t, holderA)
	if string(data) != lines {
		t.Errorf("the download is\n%q\nwant the runtime's lines as printed\n%q", data, lines)
	}
	want := map[string]string{
		"Content-Type":           "application/jsonl",
		"Content-Disposition":    `attachment; filename="checkpoints-` + handoverTrail + `-1-3.jsonl"`,
		"Content-Length":         strconv.Itoa(len(lines)),
		checkpointsTrailHeader:   handoverTrail,
		checkpointsFromHeader:    "0",
		checkpointsThroughHeader: "3",
		checkpointsDigestHeader:  digestOfString(lines),
		checkpointsMoreHeader:    "false",
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "no-store",
	}
	for name, value := range want {
		if got := header.Get(name); got != value {
			t.Errorf("%s is %q, want %q", name, got, value)
		}
	}
	if calls := rig.checkpointCalls(t); !slices.Equal(calls, []string{headCall, sinceCall(0)}) {
		t.Errorf("the download ran %q", calls)
	}
	// A download moves nothing: the same request gives the same bytes, and
	// nothing is recorded.
	if again, twice := rig.downloaded(t, holderA); !bytes.Equal(twice, data) || again.Get(checkpointsDigestHeader) != digestOfString(lines) {
		t.Errorf("a second download gave %q", twice)
	}
	if holder := holderAt(rig.holders(t), 0); holder.ID != holderA || len(holder.Trails) != 0 {
		t.Errorf("a download recorded %+v", holder.Trails)
	}
	if _, err := os.Lstat(rig.handoverPath(holderA)); !os.IsNotExist(err) {
		t.Errorf("a download made the holder's folder: %v", err)
	}
}

// **The lines end at the record read first.** A record written between the
// JSON form and the listing is left for the next hand-over: the bytes end at
// the newline of the line whose sequence the JSON form gave. A trail whose
// chained records skip a line, as a repaired one does, is handed over as it
// is.
func TestCheckpointsEndAtTheRecordReadFirst(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3, 5))
	rig.addHolder(t, "Auditor", "e-mail")
	// The trail gains records 6 and 7 after the JSON form read record 5.
	rig.listing(t, "printf '%s' '"+chainOf(handoverTrail, 1, 2, 3, 5, 6, 7)+"'")
	header, data := rig.downloaded(t, holderA)
	if want := chainOf(handoverTrail, 1, 2, 3, 5); string(data) != want || header.Get(checkpointsThroughHeader) != "5" {
		t.Errorf("the download is through %s:\n%q\nwant through 5:\n%q", header.Get(checkpointsThroughHeader), data, want)
	}
}

// **Every line is checked, and a line that is not the next checkpoint serves
// nothing.** A line that is not a checkpoint document of exactly its four
// members, one of another trail, one not after the last, a last line with no
// newline, a batch the runtime fails, and a trail that ends before the record
// read first each answer an error, with none of the bytes and none of the
// headers of a hand-over.
func TestALineThatIsNotTheNextCheckpointServesNothing(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3))
	rig.addHolder(t, "Auditor", "e-mail")
	good := checkpointOf(handoverTrail, 1)
	for _, tc := range []struct{ name, printed, words string }{
		{"not JSON", good + "not a checkpoint\n", "printed a line that is not a checkpoint"},
		{"a member too many", good + `{"checkpointVersion":"1","recordDigest":"sha256:` + strings.Repeat("0", 64) + `","sequence":2,"trail":"` + handoverTrail + `","extra":1}` + "\n", "printed a line that is not a checkpoint"},
		{"a member named twice", good + `{"checkpointVersion":"1","recordDigest":"sha256:` + strings.Repeat("0", 64) + `","sequence":2,"sequence":3,"trail":"` + handoverTrail + `"}` + "\n", "printed a line that is not a checkpoint"},
		{"another checkpoint version", good + strings.Replace(checkpointOf(handoverTrail, 2), `\u0031`, "2", 1), "printed a line that is not a checkpoint"},
		{"a sequence that is not an integer", good + strings.Replace(checkpointOf(handoverTrail, 2), `"sequence":2`, `"sequence":2.0`, 1), "printed a line that is not a checkpoint"},
		{"another trail", good + checkpointOf(movedTrail, 2) + checkpointOf(handoverTrail, 3), "the trail changed while Desk read its checkpoints"},
		{"a sequence again", good + checkpointOf(handoverTrail, 1) + checkpointOf(handoverTrail, 3), "out of order"},
		{"a sequence before the last", chainOf(handoverTrail, 2, 1, 3), "out of order"},
		{"no newline at the end", chainOf(handoverTrail, 1, 2) + strings.TrimSuffix(checkpointOf(handoverTrail, 3), "\n"), "did not end its last line"},
		{"an empty line", good + "\n" + checkpointOf(handoverTrail, 3), "printed a line that is not a checkpoint"},
		{"the trail ends before the record read first", chainOf(handoverTrail, 1, 2), "the trail changed while Desk read its checkpoints"},
	} {
		rig.listing(t, "printf '%s' '"+strings.ReplaceAll(tc.printed, "'", `'"'"'`)+"'")
		status, header, data := rig.download(t, "holder="+holderA)
		if status != http.StatusInternalServerError || !strings.Contains(refusalOf(data), tc.words) || !strings.HasPrefix(refusalOf(data), "The checkpoints could not be read, and nothing was handed over: ") {
			t.Errorf("%s: answered %d %s", tc.name, status, data)
		}
		if bytes.Contains(data, []byte(handoverTrail)) || header.Get(checkpointsTrailHeader) != "" || header.Get(checkpointsDigestHeader) != "" {
			t.Errorf("%s: served %s with %v", tc.name, data, header)
		}
	}
	// A batch the runtime fails.
	rig.listing(t, "printf '%s' '"+good+"'; exit 1")
	if status, _, data := rig.download(t, "holder="+holderA); status != http.StatusInternalServerError || !strings.Contains(refusalOf(data), "failed: exit status 1") {
		t.Errorf("a failed batch answered %d %s", status, data)
	}
}

// **A trail that passes the record read first without it serves nothing.**
// The JSON form names record 5, and the listing goes from record 4 to record
// 6: record 5 is no longer a chained record of the trail. However many
// checkpoints follow, past the bound included, nothing is handed over, and a
// confirmation through record 5 is stale.
func TestATrailThatSkipsTheRecordReadFirstServesNothing(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.addHolder(t, "Auditor", "e-mail")
	rig.heads(t, 0, `{"outputVersion":"2","command":"audit checkpoint","status":"checkpointed","checkpoint":`+strings.TrimSuffix(checkpointOf(handoverTrail, 5), "\n")+`}`)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3, 4)+chainOf(handoverTrail, upTo(handoverBatches*handoverBatch + 10)[5:]...))
	status, header, data := rig.download(t, "holder="+holderA)
	if status != http.StatusInternalServerError || !strings.Contains(refusalOf(data), "the trail changed while Desk read its checkpoints") || header.Get(checkpointsDigestHeader) != "" {
		t.Errorf("a trail past the record read first answered %d %v %.200s", status, header, data)
	}
	body := map[string]any{"trail": handoverTrail, "from": 0, "through": 5, "digest": digestOfString(chainOf(handoverTrail, 1, 2, 3, 4, 5))}
	if status, data := rig.confirm(t, holderA, body); status != http.StatusConflict || !bytes.Contains(data, []byte(`"reason":"stale"`)) {
		t.Errorf("a confirmation through the record no longer there answered %d %s", status, data)
	}
}

// **In batches of 300, and bounded.** The runtime is asked again after the
// last sequence received until fewer than 300 come back, and the batches are
// joined as printed; at most 20 batches, 6,000 checkpoints, in one answer,
// which says more remain. Confirmed, the next download starts after them.
func TestCheckpointsAreAskedForInBatchesAndBounded(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.addHolder(t, "Auditor", "e-mail")
	rig.chain(t, chainOf(handoverTrail, upTo(650)...))
	rig.ran(t)
	header, data := rig.downloaded(t, holderA)
	if string(data) != chainOf(handoverTrail, upTo(650)...) || header.Get(checkpointsThroughHeader) != "650" || header.Get(checkpointsMoreHeader) != "false" {
		t.Errorf("650 checkpoints came back through %s, more %s, %d bytes", header.Get(checkpointsThroughHeader), header.Get(checkpointsMoreHeader), len(data))
	}
	if calls := rig.checkpointCalls(t); !slices.Equal(calls, []string{headCall, sinceCall(0), sinceCall(300), sinceCall(600)}) {
		t.Errorf("650 checkpoints were asked for as %q", calls)
	}
	// Exactly 300: the last is the record read first, and nothing more is
	// asked for.
	rig.chain(t, chainOf(handoverTrail, upTo(300)...))
	if header, _ := rig.downloaded(t, holderA); header.Get(checkpointsThroughHeader) != "300" {
		t.Errorf("300 checkpoints came back through %s", header.Get(checkpointsThroughHeader))
	}
	if calls := rig.checkpointCalls(t); !slices.Equal(calls, []string{headCall, sinceCall(0)}) {
		t.Errorf("300 checkpoints were asked for as %q", calls)
	}
	// Past the bound.
	all := upTo(handoverBatches*handoverBatch + 7)
	rig.chain(t, chainOf(handoverTrail, all...))
	header, data = rig.downloaded(t, holderA)
	if string(data) != chainOf(handoverTrail, all[:6000]...) || header.Get(checkpointsThroughHeader) != "6000" || header.Get(checkpointsMoreHeader) != "true" ||
		header.Get("Content-Disposition") != `attachment; filename="checkpoints-`+handoverTrail+`-1-6000.jsonl"` {
		t.Errorf("past the bound, %d bytes came back through %s, more %s, as %s", len(data), header.Get(checkpointsThroughHeader), header.Get(checkpointsMoreHeader), header.Get("Content-Disposition"))
	}
	if calls := rig.checkpointCalls(t); len(calls) != 1+handoverBatches || calls[handoverBatches] != sinceCall(5700) {
		t.Errorf("past the bound, the download ran %d commands, the last %q", len(calls), lastOf(calls))
	}
	rig.confirmed(t, holderA, header)
	header, data = rig.downloaded(t, holderA)
	if string(data) != chainOf(handoverTrail, all[6000:]...) || header.Get(checkpointsFromHeader) != "6000" || header.Get(checkpointsMoreHeader) != "false" {
		t.Errorf("after the first 6,000 were confirmed, the download is from %s: %d bytes", header.Get(checkpointsFromHeader), len(data))
	}
}

// **A confirmation records exactly what was downloaded, and only then.** The
// confirmation asks the runtime for the same checkpoints again, and records
// those bytes, appended to the holder's file for the trail, and the record
// moved to them, by Desk's clock. The next download starts after them, and
// gives only what was written since; with nothing since, nothing at all.
func TestAConfirmationRecordsExactlyWhatWasDownloaded(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	first := chainOf(handoverTrail, 1, 2, 3)
	rig.chain(t, first)
	rig.addHolder(t, "Auditor", "e-mail")
	header, data := rig.downloaded(t, holderA)
	rig.ran(t)
	answer := rig.confirmed(t, holderA, header)
	unwitnessed := int64(0)
	want := holderTrailAnswer{handedOver: handedOver{Through: 3, ConfirmedAt: handoverNow, Digest: digestOfString(first)}, Unwitnessed: &unwitnessed}
	if len(answer.Trails) != 1 || answer.Trails[handoverTrail].handedOver != want.handedOver || since(answer.Trails[handoverTrail]) != 0 || answer.OtherTrail {
		t.Errorf("the confirmation answered %+v", answer)
	}
	if calls := rig.checkpointCalls(t); !slices.Equal(calls, []string{headCall, sinceCall(0)}) {
		t.Errorf("the confirmation ran %q, want the same checkpoints asked for again", calls)
	}
	held := rig.handoverPath(holderA, handoverTrail+".jsonl")
	if got := readFile(t, held); got != string(data) {
		t.Errorf("the holder's file holds %q, want the bytes downloaded %q", got, data)
	}
	var record handoverRecord
	if err := json.Unmarshal([]byte(readFile(t, rig.handoverPath(holderA, handoverRecordName))), &record); err != nil ||
		record.Version != "1" || len(record.Trails) != 1 || record.Trails[handoverTrail] != want.handedOver {
		t.Errorf("record.json holds %+v (%v)", record, err)
	}
	for path, mode := range map[string]os.FileMode{rig.handoverPath(holderA): 0o700 | os.ModeDir, held: 0o600, rig.handoverPath(holderA, handoverRecordName): 0o600} {
		if got := modeOf(path); got != mode.String() {
			t.Errorf("%s is %s, want %v", filepath.Base(path), got, mode)
		}
	}

	// Nothing new: no content, and the trail named.
	status, header, data := rig.download(t, "holder="+holderA)
	if status != http.StatusNoContent || len(data) != 0 || header.Get(checkpointsTrailHeader) != handoverTrail || header.Get(checkpointsDigestHeader) != "" {
		t.Errorf("with nothing new the download answered %d %v %q", status, header, data)
	}

	// Two more records: only they come back, and the record counts them.
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3, 4, 5))
	listed := holderAt(rig.holders(t), 0).Trails[handoverTrail]
	if since(listed) != 2 || listed.Through != 3 {
		t.Errorf("after two more records the holder shows %+v", listed)
	}
	header, data = rig.downloaded(t, holderA)
	second := chainOf(handoverTrail, 4, 5)
	if string(data) != second || header.Get(checkpointsFromHeader) != "3" || header.Get(checkpointsThroughHeader) != "5" ||
		header.Get("Content-Disposition") != `attachment; filename="checkpoints-`+handoverTrail+`-4-5.jsonl"` {
		t.Errorf("the second download is %v\n%q", header, data)
	}
	handoverClock = func() time.Time { return time.Unix(handoverNow+60, 0) }
	answer = rig.confirmed(t, holderA, header)
	if entry := answer.Trails[handoverTrail]; entry.Through != 5 || entry.ConfirmedAt != handoverNow+60 || entry.Digest != digestOfString(second) || since(entry) != 0 {
		t.Errorf("the second confirmation answered %+v", entry)
	}
	if got := readFile(t, held); got != first+second {
		t.Errorf("the holder's file holds %q, want both hand-overs, in order", got)
	}
	// The record keeps where the last confirmed checkpoints start, so that
	// the check can hold the file to them; and the file is passed.
	if got := readFile(t, rig.handoverPath(holderA, handoverRecordName)); !strings.Contains(got, `"from":3,"through":5,`) {
		t.Errorf("record.json holds %s, want the second hand-over from record 3 through record 5", got)
	}
	if _, answer, _ := readAudit(t, rig.ts, ""); answer.Expected != 1 || answer.ExpectUnread != nil {
		t.Errorf("after two hand-overs the panel answered %+v", answer)
	}
}

// **Each holder has a cursor of its own.** One holder handed records 1 to 3
// is given what follows them; another, handed nothing, is given everything.
func TestEachHolderHasACursorOfItsOwn(t *testing.T) {
	rig := newHandoverRig(t, holderA, holderB)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3))
	rig.addHolder(t, "Auditor", "e-mail")
	rig.addHolder(t, "Counterparty", "ticket")
	header, _ := rig.downloaded(t, holderA)
	rig.confirmed(t, holderA, header)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3, 4))
	if header, data := rig.downloaded(t, holderA); header.Get(checkpointsFromHeader) != "3" || string(data) != checkpointOf(handoverTrail, 4) {
		t.Errorf("the holder handed 1 to 3 is given %v %q", header, data)
	}
	if header, data := rig.downloaded(t, holderB); header.Get(checkpointsFromHeader) != "0" || string(data) != chainOf(handoverTrail, 1, 2, 3, 4) {
		t.Errorf("the holder handed nothing is given %v %q", header, data)
	}
	listed := rig.holders(t)
	if len(listed.Holders) != 2 || len(listed.Holders[1].Trails) != 0 || since(listed.Holders[0].Trails[handoverTrail]) != 1 {
		t.Errorf("the holders are %+v", listed)
	}
	// A trail now shorter than what was handed over to a holder: nothing.
	rig.chain(t, chainOf(handoverTrail, 1, 2))
	if status, _, data := rig.download(t, "holder="+holderA); status != http.StatusConflict || refusalOf(data) != "Desk recorded checkpoints through record 3 as handed over to this holder, and the trail's last chained record is now record 2: the trail is shorter than what was handed over, so Desk hands nothing over." {
		t.Errorf("a trail shorter than what was handed over answered %d %s", status, data)
	}
	if status, _, data := rig.download(t, "holder="+holderC); status != http.StatusNotFound || refusalOf(data) != noSuchHolderWords {
		t.Errorf("a holder Desk does not keep answered %d %s", status, data)
	}
	for _, query := range []string{"", "holder=", "holder=A1B2C3D4E5F60718", "holder=a1b2c3d4e5f6071", "holder=" + holderA + "&holder=" + holderB, "holder=" + holderA + "&since=0", "holder=../x"} {
		if status, _, data := rig.download(t, query); status != http.StatusBadRequest {
			t.Errorf("%q answered %d %s", query, status, data)
		}
	}
}

// **A confirmation of anything else records nothing, and says the file is
// stale.** Another SHA-256, a cursor that is not the holder's own (a replay
// of an earlier confirmation), records the runtime no longer gives the same
// way, a trail moved aside, and a last record past the trail's each answer
// 409 with "stale", and change nothing Desk keeps.
func TestAStaleConfirmationRecordsNothing(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3))
	rig.addHolder(t, "Auditor", "e-mail")
	header, _ := rig.downloaded(t, holderA)
	first := confirmation(t, header)
	rig.confirmed(t, holderA, header)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3, 4, 5))
	header, _ = rig.downloaded(t, holderA)
	good := confirmation(t, header)
	before := rig.snapshot(t)
	with := func(change func(map[string]any)) map[string]any {
		body := map[string]any{}
		for name, value := range good {
			body[name] = value
		}
		change(body)
		return body
	}
	stale := func(name string, body map[string]any) {
		t.Helper()
		status, data := rig.confirm(t, holderA, body)
		var answer struct{ Reason, Code, Error string }
		if status != http.StatusConflict || json.Unmarshal(data, &answer) != nil || answer.Reason != "stale" || answer.Error != staleWords {
			t.Errorf("%s: answered %d %s", name, status, data)
		}
		if after := rig.snapshot(t); after != before {
			t.Errorf("%s: Desk's record changed:\n%s\nwas\n%s", name, after, before)
		}
	}
	stale("another digest", with(func(b map[string]any) { b["digest"] = "sha256:" + strings.Repeat("0", 64) }))
	stale("the first file's digest", with(func(b map[string]any) { b["digest"] = first["digest"] }))
	stale("the earlier confirmation again", first)
	stale("a cursor before the holder's", with(func(b map[string]any) { b["from"] = 2 }))
	stale("a cursor after the holder's", with(func(b map[string]any) { b["from"] = 4 }))
	stale("a record past the trail's last", with(func(b map[string]any) { b["through"] = 6 }))
	stale("fewer records than downloaded", with(func(b map[string]any) { b["through"] = 4 }))
	// Record 5 rewritten since the download.
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3, 4)+strings.Replace(checkpointOf(handoverTrail, 5), "sha256:0", "sha256:f", 1))
	stale("a record rewritten since", good)
	// The trail moved aside.
	rig.chain(t, chainOf(movedTrail, 1, 2, 3, 4, 5))
	stale("a trail moved aside", good)
	stale("the new trail's name", with(func(b map[string]any) { b["trail"] = movedTrail }))
	// No chained record now.
	rig.chain(t, "")
	stale("no chained record now", good)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3, 4, 5))
	// Moved aside while the runtime was asked: the JSON form names the trail,
	// and the listing another.
	rig.listing(t, "printf '%s' '"+chainOf(movedTrail, 4, 5)+"'")
	stale("a trail moved aside between two commands", good)
	rig.listing(t, "printf '%s' '"+chainOf(handoverTrail, 4, 5)+"'")
	if status, data := rig.confirm(t, holderA, good); status != http.StatusOK {
		t.Errorf("the right confirmation, after them, answered %d %s", status, data)
	}
	// What a confirmation must name.
	for _, body := range []string{`{}`, `{"trail":"` + handoverTrail + `","from":3,"through":5}`, `{"trail":"x","from":3,"through":5,"digest":"` + good["digest"].(string) + `"}`,
		`{"trail":"` + handoverTrail + `","from":-1,"through":5,"digest":"` + good["digest"].(string) + `"}`, `{"trail":"` + handoverTrail + `","from":5,"through":5,"digest":"` + good["digest"].(string) + `"}`,
		`{"trail":"` + handoverTrail + `","from":3,"through":5,"digest":"` + good["digest"].(string) + `","holder":"x"}`} {
		if status, data := deskCall(t, rig.ts, "POST", "/api/audit/holders/"+holderA+"/confirm", "", body, true); status != http.StatusBadRequest {
			t.Errorf("%s answered %d %s", body, status, data)
		}
	}
	if status, data := rig.confirm(t, holderC, good); status != http.StatusNotFound || refusalOf(data) != noSuchHolderWords {
		t.Errorf("a holder Desk does not keep answered %d %s", status, data)
	}
}

// **One confirmation at a time.** Two confirmations of the same download,
// sent together: one records it, and the other finds the holder's cursor
// moved, and is stale. The holder's file holds the checkpoints once.
func TestConfirmationsAreOneAtATime(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	lines := chainOf(handoverTrail, 1, 2, 3)
	rig.chain(t, lines)
	rig.addHolder(t, "Auditor", "e-mail")
	header, _ := rig.downloaded(t, holderA)
	// The first to read the record waits, a while, for the second to read it
	// too: which it can only where nothing holds them apart.
	var mu sync.Mutex
	read := 0
	second := make(chan struct{})
	testHookConfirmRead = func() {
		mu.Lock()
		read++
		n := read
		mu.Unlock()
		switch n {
		case 1:
			select {
			case <-second:
			case <-time.After(500 * time.Millisecond):
			}
		case 2:
			close(second)
		}
	}
	t.Cleanup(func() { testHookConfirmRead = nil })
	statuses := make(chan int, 2)
	for range 2 {
		go func() {
			status, _ := rig.confirm(t, holderA, confirmation(t, header))
			statuses <- status
		}()
	}
	got := []int{<-statuses, <-statuses}
	slices.Sort(got)
	if !slices.Equal(got, []int{http.StatusOK, http.StatusConflict}) {
		t.Errorf("two confirmations together answered %v, want one recorded and one stale", got)
	}
	if held := readFile(t, rig.handoverPath(holderA, handoverTrail+".jsonl")); held != lines {
		t.Errorf("the holder's file holds %q, want the checkpoints once", held)
	}
}

// **A confirmation whose file was written and whose record was not is
// finished, and nothing in the file is written twice.** Where the holder's
// file already holds checkpoints past its record, the confirmation from the
// record appends only what follows them, where they are the same bytes; and
// where they are not, it records nothing.
func TestTheHoldersFileIsOnlyAppendedTo(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3, 4))
	rig.addHolder(t, "Auditor", "e-mail")
	held := rig.handoverPath(holderA, handoverTrail+".jsonl")
	if err := os.Mkdir(rig.handoverPath(holderA), 0o700); err != nil {
		t.Fatal(err)
	}
	// A file of 1 to 3 and no record: as a stop between the two leaves it.
	if err := os.WriteFile(held, []byte(chainOf(handoverTrail, 1, 2, 3)), 0o600); err != nil {
		t.Fatal(err)
	}
	header, _ := rig.downloaded(t, holderA)
	if header.Get(checkpointsFromHeader) != "0" {
		t.Fatalf("with no record the download is from %s", header.Get(checkpointsFromHeader))
	}
	rig.confirmed(t, holderA, header)
	if got := readFile(t, held); got != chainOf(handoverTrail, 1, 2, 3, 4) {
		t.Errorf("the holder's file holds %q, want 1 to 4 once", got)
	}
	// A file past its record that is not what the runtime gives now.
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3, 4, 5, 6))
	if err := os.WriteFile(held, []byte(chainOf(handoverTrail, 1, 2, 3, 4)+strings.Replace(checkpointOf(handoverTrail, 5), "sha256:0", "sha256:e", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	before := rig.snapshot(t)
	header, _ = rig.downloaded(t, holderA)
	if status, data := rig.confirm(t, holderA, confirmation(t, header)); status != http.StatusInternalServerError || !strings.Contains(refusalOf(data), "are not the ones confirmed now") {
		t.Errorf("a file that disagrees answered %d %s", status, data)
	}
	if rig.snapshot(t) != before {
		t.Error("a file that disagrees was written to")
	}
}

// **A trail moved aside starts each holder at 0.** The holder's record of
// the old trail is kept and shown, flagged as another trail, with nothing
// counted against it; the next download is the new trail's, from its start.
func TestAHolderStartsAt0ForATrailMovedAside(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3))
	rig.addHolder(t, "Auditor", "e-mail")
	header, _ := rig.downloaded(t, holderA)
	rig.confirmed(t, holderA, header)
	rig.chain(t, chainOf(movedTrail, 1, 2))
	listed := rig.holders(t)
	holder := holderAt(listed, 0)
	if !holder.OtherTrail || len(holder.Trails) != 1 || holder.Trails[handoverTrail].Through != 3 || holder.Trails[handoverTrail].Unwitnessed != nil ||
		listed.Trail == nil || listed.Trail.Identity != movedTrail {
		t.Errorf("after the trail was moved aside the holders are %+v", listed)
	}
	header, data := rig.downloaded(t, holderA)
	if header.Get(checkpointsTrailHeader) != movedTrail || header.Get(checkpointsFromHeader) != "0" || string(data) != chainOf(movedTrail, 1, 2) {
		t.Errorf("the new trail's download is %v %q", header, data)
	}
	answer := rig.confirmed(t, holderA, header)
	if answer.OtherTrail || answer.Trails[movedTrail].Through != 2 || answer.Trails[handoverTrail].Through != 3 {
		t.Errorf("after the new trail's confirmation the holder is %+v", answer)
	}
}

// **Where the trail has no chained record, or the runtime gives no
// checkpoint, nothing is handed over.** The holders are listed with no trail
// and nothing counted; the runtime's refusal is said in its words; a
// download says why.
func TestNothingIsHandedOverWithoutACheckpoint(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.addHolder(t, "Auditor", "e-mail")
	listed := rig.holders(t)
	if listed.Trail != nil || listed.Diagnostics != nil || len(listed.Holders) != 1 {
		t.Errorf("with no chained record the holders are %+v", listed)
	}
	if status, _, data := rig.download(t, "holder="+holderA); status != http.StatusConflict || refusalOf(data) != noChainedWords {
		t.Errorf("with no chained record the download answered %d %s", status, data)
	}
	// No trail yet: the runtime cannot open one, and there is none.
	rig.heads(t, 4, `{"outputVersion":"2","command":"audit checkpoint","status":"error","diagnostics":[{"code":"JPS-AUDIT-TRAIL-READ","message":"The project's trail `+rig.project+`/.desk-private/audit/evaluations.jsonl does not exist yet: no record has been written."}]}`)
	if listed := rig.holders(t); listed.Trail != nil || listed.Diagnostics != nil {
		t.Errorf("with no trail yet the holders are %+v", listed)
	}
	// A trail there that it could not open is its refusal.
	writeProject(t, rig.project, map[string]string{".desk-private/audit/evaluations.jsonl": ""})
	if listed := rig.holders(t); listed.Trail != nil || len(listed.Diagnostics) != 1 || listed.Diagnostics[0].Code != "JPS-AUDIT-TRAIL-READ" {
		t.Errorf("with a trail it could not open the holders are %+v", listed)
	}
	rig.heads(t, 1, checkpointRefused)
	listed = rig.holders(t)
	if listed.Trail != nil || len(listed.Diagnostics) != 1 || listed.Diagnostics[0].Code != "JPS-AUDIT-CHECKPOINT-REFUSED" || len(listed.Holders) != 1 {
		t.Errorf("with a trail the runtime refuses the holders are %+v", listed)
	}
	status, _, data := rig.download(t, "holder="+holderA)
	if status != http.StatusConflict || refusalOf(data) != "The runtime gives no checkpoint of this trail now: The trail fails 1 check(s), the first incomplete-last-line at line 4, so no checkpoint is given for it; jpack audit verify lists them." {
		t.Errorf("with a trail the runtime refuses the download answered %d %s", status, data)
	}
	// An answer the runtime does not document is an error.
	for _, answer := range []struct {
		code int
		body string
	}{
		{0, `{"command":"audit checkpoint","status":"checkpointed","checkpoint":{"trail":"` + handoverTrail + `","sequence":3}}`},
		{0, `{"command":"audit verify","status":"checkpointed","checkpoint":` + strings.TrimSuffix(checkpointOf(handoverTrail, 3), "\n") + `}`},
		{1, `{"command":"audit checkpoint","status":"checkpointed","checkpoint":` + strings.TrimSuffix(checkpointOf(handoverTrail, 3), "\n") + `}`},
		{0, `not JSON`},
		{1, `{"command":"audit checkpoint","status":"error","diagnostics":[]}`},
	} {
		rig.heads(t, answer.code, answer.body)
		status, data := reviewCall(t, rig.ts, "GET", "/api/audit/holders", "", nil, bearer)
		if status != http.StatusInternalServerError || !strings.Contains(refusalOf(data), "did not answer as documented") {
			t.Errorf("%s (exit %d) answered %d %s", answer.body, answer.code, status, data)
		}
	}
}

// **The hand-over offers nothing where the decision record does not.** A
// project that keeps no trail, and a runtime with no audit commands, are
// each said in the decision record's words, and nothing is kept or run; and
// every route is behind the desk's guard.
func TestTheHandOverOffersNothingWhereTheRecordDoesNot(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.chain(t, chainOf(handoverTrail, 1))
	routes := []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/audit/holders", nil},
		{"POST", "/api/audit/holders", map[string]string{"label": "Auditor", "channel": "e-mail"}},
		{"GET", "/api/audit/checkpoints?holder=" + holderA, nil},
		{"POST", "/api/audit/holders/" + holderA + "/confirm", map[string]any{"trail": handoverTrail, "from": 0, "through": 1, "digest": digestOfString(checkpointOf(handoverTrail, 1))}},
	}
	for _, route := range routes {
		if status, data := reviewCall(t, rig.ts, route.method, route.path, "", route.body); status != http.StatusUnauthorized {
			t.Errorf("%s %s with no session answered %d %s", route.method, route.path, status, data)
		}
	}
	writeProject(t, rig.project, map[string]string{"jpack.json": `{"configVersion":"5","packs":{}}` + "\n"})
	for _, route := range routes {
		if status, data := reviewCall(t, rig.ts, route.method, route.path, "", route.body, bearer); status != http.StatusConflict || refusalOf(data) != noTrailWords {
			t.Errorf("%s %s with no trail answered %d %s", route.method, route.path, status, data)
		}
	}
	if _, err := os.Lstat(rig.handoverPath()); !os.IsNotExist(err) {
		t.Errorf("a refused hand-over made its folder: %v", err)
	}
	ts, older, project := auditDesk(t, allConfigVersions, auditedConfig)
	for _, route := range routes {
		status, data := reviewCall(t, ts, route.method, route.path, "", route.body, bearer)
		if status != http.StatusConflict || refusalOf(data) != "This runtime (jpack 0.0.0-stand-in) writes an unchained trail and has no audit commands. Chaining, checkpoints, signing and stamping need jpack 0.26.0 or later." {
			t.Errorf("%s %s with an older runtime answered %d %s", route.method, route.path, status, data)
		}
	}
	if calls := older.ran(t); slices.ContainsFunc(calls, func(call string) bool { return strings.HasPrefix(call, "audit") }) {
		t.Errorf("an older runtime was asked %q", calls)
	}
	if _, err := os.Lstat(filepath.Join(project, ".desk-private", "handover")); !os.IsNotExist(err) {
		t.Errorf("a hand-over refused for an older runtime made its folder: %v", err)
	}
}

// **The decision record is held to what was handed over.** `audit verify`
// is given, as `--expect`, the file Desk keeps for each holder handed
// checkpoints of the trail as it is now, by its path in the project, and no
// holder's file of another trail; the answer says how many, and the panel's
// sentence follows from it. With no holder, no `audit checkpoint` is run and
// nothing is passed.
func TestTheDecisionRecordIsHeldToTheCheckpointsHandedOver(t *testing.T) {
	rig := newHandoverRig(t, holderA, holderB, holderC)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3))
	rig.ran(t)
	if _, answer, _ := readAudit(t, rig.ts, ""); answer.Expected != 0 || answer.ExpectUnread != nil {
		t.Errorf("with no holder the panel answered %+v", answer)
	}
	if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall, validateCall, verifyCall}) {
		t.Errorf("with no holder the panel ran %q", calls)
	}
	rig.addHolder(t, "Auditor", "e-mail")
	rig.addHolder(t, "Counterparty", "ticket")
	rig.addHolder(t, "Regulator", "portal")
	// Holders but no file: nothing passed.
	rig.ran(t)
	if _, answer, _ := readAudit(t, rig.ts, ""); answer.Expected != 0 {
		t.Errorf("with no file the panel answered %+v", answer)
	}
	if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall, validateCall, headCall + " [JPACK_CONFIG=unset]", verifyCall}) {
		t.Errorf("with no file the panel ran %q", calls)
	}
	// A: the current trail. B: only a trail moved aside. C: nothing.
	header, _ := rig.downloaded(t, holderA)
	rig.confirmed(t, holderA, header)
	rig.chain(t, chainOf(movedTrail, 1))
	header, _ = rig.downloaded(t, holderB)
	rig.confirmed(t, holderB, header)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3))
	rig.ran(t)
	_, answer, _ := readAudit(t, rig.ts, "")
	if answer.State != auditStateReport || answer.Expected != 1 || answer.ExpectUnread != nil {
		t.Errorf("the panel answered %+v", answer)
	}
	want := verifyCall[:len(verifyCall)-len(" [JPACK_CONFIG=unset]")] + " --expect .desk-private/handover/" + holderA + "/" + handoverTrail + ".jsonl [JPACK_CONFIG=unset]"
	if calls := rig.ran(t); !slices.Equal(calls, []string{schemaCall, validateCall, headCall + " [JPACK_CONFIG=unset]", want}) {
		t.Errorf("the panel ran %q, want audit verify with A's file alone", calls)
	}
	// Both holders handed the current trail: both files, in the holders' order.
	header, _ = rig.downloaded(t, holderC)
	rig.confirmed(t, holderC, header)
	rig.ran(t)
	if _, answer, _ := readAudit(t, rig.ts, ""); answer.Expected != 2 {
		t.Errorf("with two files the panel answered %+v", answer)
	}
	want = verifyCall[:len(verifyCall)-len(" [JPACK_CONFIG=unset]")] + " --expect .desk-private/handover/" + holderA + "/" + handoverTrail + ".jsonl --expect .desk-private/handover/" + holderC + "/" + handoverTrail + ".jsonl [JPACK_CONFIG=unset]"
	if calls := rig.ran(t); lastOf(calls) != want {
		t.Errorf("the panel ran %q, want %q", lastOf(calls), want)
	}
	// The trail moved aside: B's file, and only B's.
	rig.chain(t, chainOf(movedTrail, 1))
	rig.ran(t)
	readAudit(t, rig.ts, "")
	want = verifyCall[:len(verifyCall)-len(" [JPACK_CONFIG=unset]")] + " --expect .desk-private/handover/" + holderB + "/" + movedTrail + ".jsonl [JPACK_CONFIG=unset]"
	if calls := rig.ran(t); lastOf(calls) != want {
		t.Errorf("after the trail was moved aside the panel ran %q, want %q", lastOf(calls), want)
	}
	// No chained record: nothing passed.
	rig.chain(t, "")
	rig.ran(t)
	if _, answer, _ := readAudit(t, rig.ts, ""); answer.Expected != 0 {
		t.Errorf("with no chained record the panel answered %+v", answer)
	}
	if calls := rig.ran(t); lastOf(calls) != verifyCall {
		t.Errorf("with no chained record the panel ran %q", lastOf(calls))
	}
}

// **A holder's file that cannot be read now, or is not ours, is passed over
// and named, and the check runs without it.** A file others can read, one
// that cannot be opened, a link in its place, and an empty one are each left
// out of `audit verify`, and the holder's label is in the answer.
func TestAHeldFileThatCannotBeReadIsPassedOverAndNamed(t *testing.T) {
	rig := newHandoverRig(t, holderA, holderB)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3))
	rig.addHolder(t, "Auditor", "e-mail")
	rig.addHolder(t, "Counterparty", "ticket")
	for _, id := range []string{holderA, holderB} {
		header, _ := rig.downloaded(t, id)
		rig.confirmed(t, id, header)
	}
	held := rig.handoverPath(holderA, handoverTrail+".jsonl")
	lines := readFile(t, held)
	onlyB := " --expect .desk-private/handover/" + holderB + "/" + handoverTrail + ".jsonl [JPACK_CONFIG=unset]"
	for _, tc := range []struct {
		name   string
		break_ func()
	}{
		{"readable by others", func() { os.Chmod(held, 0o640) }},
		{"not to be opened", func() { os.Chmod(held, 0o200) }},
		{"a link", func() { os.Remove(held); os.Symlink(rig.handoverPath(holderB, handoverTrail+".jsonl"), held) }},
		{"empty", func() { os.WriteFile(held, nil, 0o600) }},
	} {
		tc.break_()
		rig.ran(t)
		status, answer, refusal := readAudit(t, rig.ts, "")
		if status != http.StatusOK || answer.State != auditStateReport || answer.Expected != 1 || !slices.Equal(answer.ExpectUnread, []string{"Auditor"}) {
			t.Errorf("%s: the panel answered %d %+v %q", tc.name, status, answer, refusal)
		}
		if calls := rig.ran(t); !strings.HasSuffix(lastOf(calls), "--format json"+onlyB) {
			t.Errorf("%s: the panel ran %q, want B's file alone", tc.name, lastOf(calls))
		}
		os.Remove(held)
		if err := os.WriteFile(held, []byte(lines), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, answer, _ := readAudit(t, rig.ts, ""); answer.Expected != 2 || answer.ExpectUnread != nil {
		t.Errorf("mended, the panel answered %+v", answer)
	}
}

// **Desk's record is kept only where only you can read it.** A hand-over
// folder open to other users, or a link in its place, even one to a folder
// inside the project, is refused, and nothing is written in it or through
// it; a list of holders others can read is not read.
func TestTheHandOverFolderIsOwnerOnly(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.chain(t, chainOf(handoverTrail, 1))
	add := func() (int, []byte) {
		return reviewCall(t, rig.ts, "POST", "/api/audit/holders", "", map[string]string{"label": "Auditor", "channel": "e-mail"}, bearer)
	}
	if err := os.Mkdir(rig.handoverPath(), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(rig.handoverPath(), 0o750); err != nil {
		t.Fatal(err)
	}
	if status, data := add(); status != http.StatusInternalServerError || !strings.Contains(refusalOf(data), "open to other users") {
		t.Errorf("with a hand-over folder others can read, adding a holder answered %d %s", status, data)
	}
	if entries, _ := os.ReadDir(rig.handoverPath()); len(entries) != 0 {
		t.Errorf("a folder others can read was written to: %v", entries)
	}
	os.Remove(rig.handoverPath())
	elsewhere := filepath.Join(rig.project, ".desk-private", "elsewhere")
	if err := os.Mkdir(elsewhere, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", rig.handoverPath()); err != nil {
		t.Fatal(err)
	}
	if status, data := add(); status != http.StatusInternalServerError || !strings.Contains(refusalOf(data), "not a folder Desk keeps") {
		t.Errorf("with a link in place of the hand-over folder, adding a holder answered %d %s", status, data)
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Errorf("a link was written through: %v", entries)
	}
	os.Remove(rig.handoverPath())
	rig.addHolder(t, "Auditor", "e-mail")
	if err := os.Chmod(rig.handoverPath(handoverHoldersName), 0o644); err != nil {
		t.Fatal(err)
	}
	if status, data := reviewCall(t, rig.ts, "GET", "/api/audit/holders", "", nil, bearer); status != http.StatusInternalServerError || strings.Contains(string(data), "Auditor") {
		t.Errorf("with a list of holders others can read, the holders answered %d %s", status, data)
	}
}

// **The file API refuses Desk's record of hand-overs.** Reading or writing
// anything under `.desk-private/handover` is refused, and the listing does
// not show it.
func TestTheFileAPIRefusesTheHandOver(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.chain(t, chainOf(handoverTrail, 1))
	rig.addHolder(t, "Counterparty: SECRET-LABEL", "e-mail")
	header, _ := rig.downloaded(t, holderA)
	rig.confirmed(t, holderA, header)
	for _, name := range []string{".desk-private/handover/holders.json", ".desk-private/handover/" + holderA + "/record.json", ".desk-private/handover/" + holderA + "/" + handoverTrail + ".jsonl", ".DESK-PRIVATE/handover/holders.json"} {
		status, data := deskCall(t, rig.ts, "GET", "/api/file?path="+strings.ReplaceAll(name, "/", "%2F"), "", "", true)
		var refused struct{ Code string }
		if status == http.StatusOK || json.Unmarshal(data, &refused) != nil || refused.Code != CodeExcludedDirectory || bytes.Contains(data, []byte("SECRET")) {
			t.Errorf("reading %s answered %d %s", name, status, data)
		}
		status, data = deskCall(t, rig.ts, "PUT", "/api/file", "", `{"path":"`+name+`","content":"{}","baseSha256":"","createParents":true}`, true)
		if status == http.StatusOK || status == http.StatusCreated || json.Unmarshal(data, &refused) != nil || refused.Code != CodeExcludedDirectory {
			t.Errorf("writing %s answered %d %s", name, status, data)
		}
	}
	if status, data := deskCall(t, rig.ts, "GET", "/api/files", "", "", true); status != http.StatusOK || bytes.Contains(data, []byte("handover")) || bytes.Contains(data, []byte("holders.json")) {
		t.Errorf("the listing answered %d %s", status, data)
	}
	if !strings.Contains(readFile(t, rig.handoverPath(handoverHoldersName)), "SECRET-LABEL") {
		t.Error("the refused writes changed the list of holders")
	}
}

// **No path reaches the page.** The runtime's refusal names the project's
// folder: the holders' answer, a download and a confirmation each say it
// with "the project's folder" in its place; Desk's log keeps it whole.
func TestTheHandOverQuotesNoPath(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.chain(t, chainOf(handoverTrail, 1))
	rig.addHolder(t, "Auditor", "e-mail")
	header, _ := rig.downloaded(t, holderA)
	trail := filepath.Join(rig.project, ".desk-private", "audit", "evaluations.jsonl")
	rig.heads(t, 4, `{"outputVersion":"2","command":"audit checkpoint","status":"error","diagnostics":[{"code":"JPS-AUDIT-TRAIL-READ","message":"The project's trail `+trail+` could not be opened as one regular file inside the project."}]}`)
	writeProject(t, rig.project, map[string]string{".desk-private/audit/evaluations.jsonl": ""})
	status, data := reviewCall(t, rig.ts, "GET", "/api/audit/holders", "", nil, bearer)
	status2, _, data2 := rig.download(t, "holder="+holderA)
	status3, data3 := rig.confirm(t, holderA, confirmation(t, header))
	for _, answer := range []struct {
		status int
		data   []byte
	}{{status, data}, {status2, data2}, {status3, data3}} {
		if answer.status == http.StatusOK && !bytes.Contains(answer.data, []byte("JPS-AUDIT-TRAIL-READ")) || answer.status != http.StatusOK && answer.status != http.StatusConflict {
			t.Errorf("the refusal answered %d %s", answer.status, answer.data)
		}
		if bytes.Contains(answer.data, []byte(rig.project)) || !bytes.Contains(answer.data, []byte("…/evaluations.jsonl")) {
			t.Errorf("the refusal says where the project is: %s", answer.data)
		}
	}
	if !strings.Contains(rig.logged.String(), trail) {
		t.Errorf("Desk's log does not keep the runtime's words whole: %s", rig.logged)
	}
}

/* With the runtime ----------------------------------------------------------- */

// evaluateIn makes one deciding run in folder with the runtime at bin.
func evaluateIn(t *testing.T, bin, folder string) {
	t.Helper()
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	jpackIn(t, bin, folder, "experimental", "evaluate", "--config", "jpack.json", "--pack-id", "alpha", "--facts", facts, "--format", "json")
}

// **With the runtime, end to end.** A desk Desk made, with three records
// its runtime wrote: a holder is added; the download is the runtime's own
// `audit checkpoint --since 0`, byte for byte; once confirmed, the decision
// record's `audit verify` witnesses every record through the one confirmed;
// one more record is unwitnessed; the next download is that record's line
// alone; and a confirmation with the first file's SHA-256 is stale.
func TestHandOverWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	fixHandover(t, holderA)
	s, ts, _ := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	if row.ConfigVersion != "6" {
		t.Skipf("this runtime makes desks at configVersion %s, with no audit commands", row.ConfigVersion)
	}
	if info, err := os.Lstat(filepath.Join(row.Folder, ".desk-private", "handover")); err != nil || info.Mode() != 0o700|os.ModeDir {
		t.Errorf("the new desk's hand-over folder is %v (%v)", info.Mode(), err)
	}
	config := strings.Replace(gatedConfigFor(t, s, row), `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": reviewPack, "jpack.json": config})
	jpackIn(t, bin, row.Folder, "packs", "lock", "--config", "jpack.json", "--format", "json")
	for range 3 {
		evaluateIn(t, bin, row.Folder)
	}
	call := func(method, path string, body any) (int, []byte) {
		return reviewCall(t, ts, method, path, row.ID, body, bearer)
	}
	status, data := call("POST", "/api/audit/holders", map[string]string{"label": "Auditor", "channel": "e-mail to records@example.com"})
	if status != http.StatusCreated {
		t.Fatalf("adding a holder answered %d %s", status, data)
	}
	get := func(query string) (int, http.Header, []byte) {
		request, _ := http.NewRequest("GET", ts.URL+"/api/audit/checkpoints?"+query, nil)
		bearer(request)
		request.Header.Set("X-Jpack-Desk", row.ID)
		response, err := ts.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		return response.StatusCode, response.Header, data
	}
	status, header, first := get("holder=" + holderA)
	if want := jpackIn(t, bin, row.Folder, "audit", "checkpoint", "--config", "jpack.json", "--since", "0"); status != http.StatusOK || !bytes.Equal(first, want) || bytes.Count(first, []byte("\n")) != 3 {
		t.Fatalf("the download answered %d\n%s\nwant the runtime's own\n%s", status, first, want)
	}
	if header.Get(checkpointsThroughHeader) != "3" || header.Get(checkpointsDigestHeader) != digestOfString(string(first)) {
		t.Errorf("the download's headers are %v", header)
	}
	firstConfirmation := confirmation(t, header)
	if status, data := call("POST", "/api/audit/holders/"+holderA+"/confirm", firstConfirmation); status != http.StatusOK {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	_, answer, refusal := readAudit(t, ts, row.ID)
	if answer.State != auditStateReport || answer.Expected != 1 || answer.Report.Coverage.Witnessed != 3 || answer.Report.Coverage.Unwitnessed != 0 ||
		answer.Report.Coverage.Checkpointed != (auditCoverageState{Status: "through", Through: 3}) {
		t.Fatalf("after the hand-over the panel answered %+v %q", answer, refusal)
	}
	// Desk's file cut short to its first checkpoint, or with its last newline
	// gone, is passed over and named, never passed as confirmed (review round
	// 1, finding 1).
	heldPath := filepath.Join(row.Folder, ".desk-private", "handover", holderA, firstConfirmation["trail"].(string)+".jsonl")
	whole := readFile(t, heldPath)
	for _, broken := range []string{whole[:strings.Index(whole, "\n")+1], strings.TrimSuffix(whole, "\n")} {
		if err := os.WriteFile(heldPath, []byte(broken), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, answer, _ := readAudit(t, ts, row.ID); answer.Report == nil || answer.Expected != 0 || !slices.Equal(answer.ExpectUnread, []string{"Auditor"}) ||
			answer.Report.Coverage.Witnessed != 0 || answer.Report.Coverage.Checkpointed.Status != "not-supplied" {
			t.Errorf("with the held file %q the panel answered %+v", broken, answer)
		}
	}
	if err := os.WriteFile(heldPath, []byte(whole), 0o600); err != nil {
		t.Fatal(err)
	}
	evaluateIn(t, bin, row.Folder)
	if _, answer, _ := readAudit(t, ts, row.ID); answer.Report == nil || answer.Report.Coverage.Witnessed != 3 || answer.Report.Coverage.Unwitnessed != 1 {
		t.Errorf("after one more record the panel answered %+v", answer)
	}
	status, data = call("GET", "/api/audit/holders", nil)
	var listed holdersAnswer
	if status != http.StatusOK || json.Unmarshal(data, &listed) != nil || listed.Trail == nil || listed.Trail.Sequence != 4 ||
		since(holderAt(listed, 0).Trails[listed.Trail.Identity]) != 1 {
		t.Errorf("after one more record the holders answered %d %s", status, data)
	}
	status, header, second := get("holder=" + holderA)
	if status != http.StatusOK || bytes.Count(second, []byte("\n")) != 1 || !bytes.Contains(second, []byte(`"sequence":4,`)) || header.Get(checkpointsFromHeader) != "3" {
		t.Fatalf("the second download answered %d %v\n%s", status, header, second)
	}
	// The first file's confirmation again, and the second file with the
	// first's SHA-256: each stale.
	for _, body := range []map[string]any{firstConfirmation, func() map[string]any {
		body := confirmation(t, header)
		body["digest"] = firstConfirmation["digest"]
		return body
	}()} {
		if status, data := call("POST", "/api/audit/holders/"+holderA+"/confirm", body); status != http.StatusConflict || !bytes.Contains(data, []byte(`"reason":"stale"`)) {
			t.Errorf("a confirmation with the first file's digest answered %d %s", status, data)
		}
	}
	if status, data := call("POST", "/api/audit/holders/"+holderA+"/confirm", confirmation(t, header)); status != http.StatusOK {
		t.Fatalf("the second confirmation answered %d %s", status, data)
	}
	held := readFile(t, filepath.Join(row.Folder, ".desk-private", "handover", holderA, listed.Trail.Identity+".jsonl"))
	if held != string(first)+string(second) {
		t.Errorf("the holder's file holds\n%s\nwant both downloads, in order", held)
	}
	if _, answer, _ := readAudit(t, ts, row.ID); answer.Report == nil || answer.Report.Coverage.Witnessed != 4 || answer.Report.Coverage.Unwitnessed != 0 {
		t.Errorf("after the second hand-over the panel answered %+v", answer)
	}
}

// **With the runtime: no path reaches the page.** In a project whose path
// holds a space, a tab and a line separator, the runtime's refusal of a
// trail it cannot open names that path; the holders' answer and a download
// each say which file, and not where.
func TestTheHandOverQuotesNoPathWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	fixHandover(t, holderA)
	project := filepath.Join(t.TempDir(), "Top SECRET\tproject\u2028TAIL")
	if err := os.MkdirAll(filepath.Join(project, ".desk-private", "audit", "evaluations.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeProject(t, project, map[string]string{"jpack.json": auditedConfig})
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	status, data := reviewCall(t, ts, "GET", "/api/audit/holders", "", nil, bearer)
	if status == http.StatusConflict && strings.Contains(refusalOf(data), "has no audit commands") {
		t.Skip("this runtime has no audit commands")
	}
	if status, data := reviewCall(t, ts, "POST", "/api/audit/holders", "", map[string]string{"label": "Auditor", "channel": "e-mail"}, bearer); status != http.StatusCreated {
		t.Fatalf("adding a holder answered %d %s", status, data)
	}
	request, _ := http.NewRequest("GET", ts.URL+"/api/audit/checkpoints?holder="+holderA, nil)
	bearer(request)
	response, err := ts.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	downloaded, _ := io.ReadAll(response.Body)
	response.Body.Close()
	var listed holdersAnswer
	if status != http.StatusOK || json.Unmarshal(data, &listed) != nil || listed.Trail != nil || len(listed.Diagnostics) != 1 || listed.Diagnostics[0].Code != "JPS-AUDIT-TRAIL-READ" {
		t.Errorf("the holders answered %d %s", status, data)
	}
	if response.StatusCode != http.StatusConflict {
		t.Errorf("the download answered %d %s", response.StatusCode, downloaded)
	}
	for _, said := range [][]byte{data, downloaded} {
		for _, leaked := range []string{"SECRET", "TAIL", project, "/tmp"} {
			if strings.Contains(string(said), leaked) {
				t.Errorf("the answer says %q: %s", leaked, said)
			}
		}
		if !bytes.Contains(said, []byte("evaluations.jsonl")) {
			t.Errorf("the answer does not say which file: %s", said)
		}
	}
}

/* Review round 1 ---------------------------------------------------------- */

// recordOf is a holder's record.json naming one trail, as Desk writes it.
func recordOf(trail string, from, through int64, digest string) string {
	return fmt.Sprintf(`{"version":"1","trails":{%q:{"from":%d,"through":%d,"confirmedAt":1,"digest":%q}}}`, trail, from, through, digest) + "\n"
}

// **A held file is passed only where it is what Desk recorded as handed
// over** (review round 1, finding 1). Read whole, every line a complete
// checkpoint of the record's trail, in strictly increasing order, ending at
// the record's last sequence, and ending in the very bytes last confirmed. A
// file cut short to its first checkpoint, one whose last newline is gone, one
// ending before its record (with a digest made to match), one of another
// trail, one out of order, one whose last batch is not the one confirmed, or
// was confirmed from another cursor, one ahead of its record, a record with
// no file, and a file with no record are each passed over and named, and the
// check runs without them.
func TestAHeldFileThatIsNotWhatWasHandedOverIsPassedOverAndNamed(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	lines := chainOf(handoverTrail, 1, 2, 3)
	rig.chain(t, lines)
	rig.addHolder(t, "Auditor", "e-mail")
	header, _ := rig.downloaded(t, holderA)
	rig.confirmed(t, holderA, header)
	held := rig.handoverPath(holderA, handoverTrail+".jsonl")
	recordPath := rig.handoverPath(holderA, handoverRecordName)
	record := readFile(t, recordPath)
	if !strings.Contains(record, `"from":0,"through":3`) {
		t.Fatalf("record.json holds %s", record)
	}
	passedAlone := verifyCall[:len(verifyCall)-len(" [JPACK_CONFIG=unset]")] + " --expect .desk-private/handover/" + holderA + "/" + handoverTrail + ".jsonl [JPACK_CONFIG=unset]"
	rig.ran(t)
	if _, answer, _ := readAudit(t, rig.ts, ""); answer.Expected != 1 || answer.ExpectUnread != nil || answer.HandoverProblem != "" {
		t.Fatalf("with the file as handed over the panel answered %+v", answer)
	}
	if calls := rig.ran(t); lastOf(calls) != passedAlone {
		t.Fatalf("with the file as handed over the panel ran %q", lastOf(calls))
	}
	altered := strings.Replace(checkpointOf(handoverTrail, 3), "sha256:0", "sha256:d", 1)
	reordered := chainOf(handoverTrail, 1, 3, 2)
	elsewhere := chainOf(movedTrail, 1, 2, 3)
	for _, tc := range []struct {
		name, file, record string
	}{
		{"cut short to its first checkpoint", checkpointOf(handoverTrail, 1), ""},
		{"its last newline removed", strings.TrimSuffix(lines, "\n"), ""},
		{"its last newline removed, with a digest made to match", strings.TrimSuffix(lines, "\n"), recordOf(handoverTrail, 0, 3, digestOfString(strings.TrimSuffix(lines, "\n")))},
		{"ending before its record, with a digest made to match", chainOf(handoverTrail, 1, 2), recordOf(handoverTrail, 0, 3, digestOfString(chainOf(handoverTrail, 1, 2)))},
		{"of another trail, with a digest made to match", elsewhere, recordOf(handoverTrail, 0, 3, digestOfString(elsewhere))},
		{"out of order, with a digest made to match", reordered, recordOf(handoverTrail, 0, 2, digestOfString(reordered))},
		{"a last batch that is not the one confirmed", chainOf(handoverTrail, 1, 2) + altered, ""},
		{"a last batch confirmed from another cursor", lines, recordOf(handoverTrail, 1, 3, digestOfString(lines))},
		{"ahead of its record", lines, recordOf(handoverTrail, 0, 2, digestOfString(chainOf(handoverTrail, 1, 2)))},
		{"a line that is not a checkpoint", checkpointOf(handoverTrail, 1) + "not a checkpoint\n" + checkpointOf(handoverTrail, 3), ""},
		{"a record with no file", "", ""},
		{"a file with no record", lines, `{"version":"1","trails":{}}` + "\n"},
	} {
		os.Remove(held)
		if tc.file != "" {
			if err := os.WriteFile(held, []byte(tc.file), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if tc.record != "" {
			if err := os.WriteFile(recordPath, []byte(tc.record), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		rig.ran(t)
		status, answer, refusal := readAudit(t, rig.ts, "")
		if status != http.StatusOK || answer.State != auditStateReport || answer.Expected != 0 || !slices.Equal(answer.ExpectUnread, []string{"Auditor"}) || answer.HandoverProblem != "" {
			t.Errorf("%s: the panel answered %d %+v %q", tc.name, status, answer, refusal)
		}
		if calls := rig.ran(t); lastOf(calls) != verifyCall {
			t.Errorf("%s: the panel ran %q, want audit verify with no held checkpoint", tc.name, lastOf(calls))
		}
		if err := os.WriteFile(held, []byte(lines), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(recordPath, []byte(record), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// **A record of hand-overs Desk cannot read is said, never taken for none**
// (review round 1, finding 3). A hand-over folder others can read, a list of
// holders others can read or that is not one Desk writes, and a runtime that
// does not say which trail is current each leave the check with no held
// checkpoint, and the answer says why; a folder with no holder says nothing.
func TestARecordOfHandOversDeskCannotReadIsSaid(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.chain(t, chainOf(handoverTrail, 1, 2, 3))
	rig.addHolder(t, "Auditor", "e-mail")
	header, _ := rig.downloaded(t, holderA)
	rig.confirmed(t, holderA, header)
	holders := rig.handoverPath(handoverHoldersName)
	list := readFile(t, holders)
	for _, tc := range []struct {
		name, says   string
		break_, mend func()
	}{
		{"a hand-over folder others can read", "open to other users",
			func() { os.Chmod(rig.handoverPath(), 0o750) }, func() { os.Chmod(rig.handoverPath(), 0o700) }},
		{"a list of holders others can read", "readable or writable by someone other than its owner",
			func() { os.Chmod(holders, 0o644) }, func() { os.Chmod(holders, 0o600) }},
		{"a list of holders Desk did not write", "is not a list of holders Desk wrote",
			func() { os.WriteFile(holders, []byte(`{"version":"1","holders":[{"id":"x"}]}`), 0o600) }, func() { os.WriteFile(holders, []byte(list), 0o600) }},
		{"a runtime that does not say which trail is current", "Desk could not tell which trail the checkpoints it handed over belong to, so it passed none of them to the check: its audit checkpoint did not answer as documented",
			func() { rig.heads(t, 0, "not JSON") }, func() { os.Remove(rig.calls + ".head") }},
		{"a trail the runtime gives no checkpoint of", "Desk could not tell which trail the checkpoints it handed over belong to, so it passed none of them to the check: The runtime gives no checkpoint of this trail now: The trail fails 1 check(s)",
			func() { rig.heads(t, 1, checkpointRefused) }, func() { os.Remove(rig.calls + ".head") }},
		{"a trail the runtime cannot open, named by its path", "Desk could not tell which trail the checkpoints it handed over belong to, so it passed none of them to the check: The runtime gives no checkpoint of this trail now: The project's trail …/evaluations.jsonl could not be opened as one regular file inside the project.",
			func() {
				writeProject(t, rig.project, map[string]string{".desk-private/audit/evaluations.jsonl": ""})
				rig.heads(t, 4, `{"outputVersion":"2","command":"audit checkpoint","status":"error","diagnostics":[{"code":"JPS-AUDIT-TRAIL-READ","message":"The project's trail `+filepath.Join(rig.project, ".desk-private", "audit", "evaluations.jsonl")+` could not be opened as one regular file inside the project."}]}`)
			}, func() {
				os.Remove(rig.calls + ".head")
				os.Remove(filepath.Join(rig.project, ".desk-private", "audit", "evaluations.jsonl"))
			}},
	} {
		tc.break_()
		rig.ran(t)
		status, answer, refusal := readAudit(t, rig.ts, "")
		if status != http.StatusOK || answer.Expected != 0 || answer.ExpectUnread != nil || !strings.Contains(answer.HandoverProblem, tc.says) {
			t.Errorf("%s: the panel answered %d %+v %q", tc.name, status, answer, refusal)
		}
		if !strings.HasPrefix(tc.says, "Desk could not tell") && !strings.HasPrefix(answer.HandoverProblem, "Desk could not read its record of hand-overs, so it passed none of the checkpoints it handed over to the check: ") {
			t.Errorf("%s: the panel says %q", tc.name, answer.HandoverProblem)
		}
		if strings.Contains(answer.HandoverProblem, rig.project) {
			t.Errorf("%s: the panel names the project's folder: %q", tc.name, answer.HandoverProblem)
		}
		if calls := rig.ran(t); lastOf(calls) != verifyCall {
			t.Errorf("%s: the panel ran %q, want audit verify with no held checkpoint", tc.name, lastOf(calls))
		}
		tc.mend()
	}
	if _, answer, _ := readAudit(t, rig.ts, ""); answer.Expected != 1 || answer.HandoverProblem != "" {
		t.Errorf("mended, the panel answered %+v", answer)
	}
	// A folder that keeps no holder says nothing.
	if err := os.WriteFile(holders, []byte(`{"version":"1","holders":[]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, answer, _ := readAudit(t, rig.ts, ""); answer.Expected != 0 || answer.HandoverProblem != "" || answer.ExpectUnread != nil {
		t.Errorf("with no holder the panel answered %+v", answer)
	}
}

// holderWords is a label and a channel whose JSON, as Desk writes it, is
// exactly bytes long: ASCII where it can be, and `&` (written `&`, six
// bytes) and `"` (written `\"`, two) where it must be longer than its
// characters.
func holderWords(t *testing.T, bytes int) (string, string) {
	t.Helper()
	if bytes < 2 || bytes > 6*(holderLabelLimit+holderChannelLimit) {
		t.Fatalf("no label and channel take %d bytes", bytes)
	}
	if bytes <= holderLabelLimit+holderChannelLimit {
		label := min(holderLabelLimit, bytes-1)
		return strings.Repeat("x", label), strings.Repeat("y", bytes-label)
	}
	extra := bytes - holderLabelLimit - holderChannelLimit
	sixes, twos := extra/5, extra%5
	chars := []rune(strings.Repeat("&", sixes) + strings.Repeat(`"`, twos) + strings.Repeat("x", holderLabelLimit+holderChannelLimit-sixes-twos))
	return string(chars[:holderLabelLimit]), string(chars[holderLabelLimit:])
}

// **The list of holders is written only within the bound it is read with**
// (review round 1, finding 2). Holders with labels of emoji and JSON-escaped
// characters fill the list; a holder that would take it one byte past 65,536
// is refused in plain words and changes nothing; one that takes it to exactly
// 65,536, newline included, is added; and the list is read, and a hand-over
// made, afterwards.
func TestTheListOfHoldersIsWrittenWithinTheBoundItIsReadWith(t *testing.T) {
	ids := make([]string, maxHolders+5)
	for i := range ids {
		ids[i] = fmt.Sprintf("%016x", i+1)
	}
	rig := newHandoverRig(t, ids...)
	rig.chain(t, chainOf(handoverTrail, 1))
	big := handoverHolder{Label: strings.Repeat("😀", 100) + strings.Repeat("&", 20), Channel: strings.Repeat("<", 150) + strings.Repeat(`"`, 50), AddedAt: handoverNow}
	size := func(holders []handoverHolder) int {
		data, err := json.Marshal(holdersFile{Version: "1", Holders: holders})
		if err != nil {
			t.Fatal(err)
		}
		return len(data) + 1
	}
	kept := []handoverHolder{}
	next := 0
	for {
		candidate := big
		candidate.ID = ids[next]
		if size(append(slices.Clone(kept), candidate))+100 > handoverListLimit {
			break
		}
		added := rig.addHolder(t, big.Label, big.Channel)
		if added.handoverHolder != candidate {
			t.Fatalf("holder %d was added as %+v", next, added.handoverHolder)
		}
		kept = append(kept, candidate)
		next++
	}
	if next < 30 {
		t.Fatalf("only %d large holders fit; the test means to fill the list", next)
	}
	if got := len(readFile(t, rig.handoverPath(handoverHoldersName))); got != size(kept) {
		t.Fatalf("holders.json is %d bytes, want %d", got, size(kept))
	}
	// The bytes a last holder's words may take: the room left, less what a
	// holder with one-character words takes beyond them.
	least := handoverHolder{ID: ids[next], Label: "x", Channel: "y", AddedAt: handoverNow}
	room := handoverListLimit - size(append(slices.Clone(kept), least)) + 2
	label, channel := holderWords(t, room+1)
	before := rig.snapshot(t)
	status, data := reviewCall(t, rig.ts, "POST", "/api/audit/holders", "", map[string]string{"label": label, "channel": channel}, bearer)
	if status != http.StatusConflict || refusalOf(data) != fmt.Sprintf(holdersFullWords, handoverListLimit) {
		t.Errorf("a holder one byte past the bound answered %d %s", status, data)
	}
	if rig.snapshot(t) != before {
		t.Error("a holder past the bound changed Desk's record")
	}
	next++ // the refused holder took an id
	label, channel = holderWords(t, room)
	added := rig.addHolder(t, label, channel)
	if added.ID != ids[next] || added.Label != label || added.Channel != channel {
		t.Errorf("the holder that fills the list was added as %+v", added.handoverHolder)
	}
	if got := len(readFile(t, rig.handoverPath(handoverHoldersName))); got != handoverListLimit {
		t.Errorf("holders.json is %d bytes, want exactly %d", got, handoverListLimit)
	}
	if listed := rig.holders(t); len(listed.Holders) != len(kept)+1 {
		t.Errorf("Desk lists %d holders, want %d", len(listed.Holders), len(kept)+1)
	}
	if status, data := reviewCall(t, rig.ts, "POST", "/api/audit/holders", "", map[string]string{"label": "x", "channel": "y"}, bearer); status != http.StatusConflict {
		t.Errorf("a holder after the list is full answered %d %s", status, data)
	}
	header, _ := rig.downloaded(t, added.ID)
	rig.confirmed(t, added.ID, header)
}

// **A holder's record is written only within the bound it is read with**, and
// a confirmation that would pass it writes nothing: neither the record nor
// the holder's file.
func TestAHoldersRecordIsWrittenWithinTheBoundItIsReadWith(t *testing.T) {
	rig := newHandoverRig(t, holderA)
	rig.chain(t, chainOf(handoverTrail, 1))
	rig.addHolder(t, "Auditor", "e-mail")
	if err := os.Mkdir(rig.handoverPath(holderA), 0o700); err != nil {
		t.Fatal(err)
	}
	// A record of other trails, moved aside, with no room for one more.
	entry := func(i int) string {
		return fmt.Sprintf(`"%032x":{"from":0,"through":1,"confirmedAt":1,"digest":"sha256:%064x"}`, i+1, i)
	}
	var entries []string
	for {
		candidate := `{"version":"1","trails":{` + strings.Join(append(slices.Clone(entries), entry(len(entries))), ",") + "}}\n"
		if len(candidate) > handoverListLimit {
			break
		}
		entries = append(entries, entry(len(entries)))
	}
	record := `{"version":"1","trails":{` + strings.Join(entries, ",") + "}}\n"
	if err := os.WriteFile(rig.handoverPath(holderA, handoverRecordName), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
	before := rig.snapshot(t)
	header, _ := rig.downloaded(t, holderA)
	status, data := rig.confirm(t, holderA, confirmation(t, header))
	if status != http.StatusInternalServerError || !strings.Contains(refusalOf(data), "would be larger than the 65536 bytes Desk reads of it") {
		t.Errorf("a confirmation past the record's bound answered %d %s", status, data)
	}
	if rig.snapshot(t) != before {
		t.Error("a confirmation past the record's bound wrote to Desk's record")
	}
}
