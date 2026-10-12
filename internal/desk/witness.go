package desk

// A witness as a holder of checkpoints (ADR-0012, accepted 2026-10-12 with
// its ten questions answered as recommended; ADR-0010's delivery row 10;
// issue #234).
//
// # What a witness is, to Desk
//
// A gateway another party runs (Gateway v0.11.0, `SPEC.md` §6 "Witness
// endpoints" and §8) that signs the checkpoints handed to it, chains its
// statements per trail, and serves them to anyone who names the trail. To
// Desk it is a holder (ADR-0010, section 2) whose channel is `witness`, one
// per desk (question 1), kept in `holders.json` with `"witness": true`, so a
// holder the owner labelled "witness" by hand is never taken for one. The
// hand-over's download and confirmation refuse it, and its list leaves it
// out: Desk submits to a witness, and nothing is downloaded for it.
//
// # What Desk keeps
//
// In the holder's own folder, `.desk-private/handover/<holder id>/`,
// owner-only, opened through the project's root and never through a link,
// refused by the file API and in no backup (handover.go):
//
//   - `witness.json`: the witness's URL, its public key, the interval and
//     when the owner set them; and `witness.pub`, the same key as the
//     runtime reads it (`--witness-key`), which a removal keeps beside the
//     statements it signed;
//   - `record.json`, the holder's record: under `witness`, for each trail
//     identity, the cursor (the last sequence the witness countersigned for
//     this desk, its statement's index and record digest, and when, by
//     Desk's clock), a head the witness answered `below-head` with, and
//     whether it retired the trail;
//   - `statements-<trail>.jsonl`: every statement the witness answered or
//     served, exactly as received, each once, appended and never rewritten;
//   - `head-<trail>.jsonl`: the head last fetched, replaced only once its
//     statement is in the statements file;
//   - `continuation-<trail>.json`: saved by the runtime (`--witness-save`)
//     after Desk's own reading with no finding;
//   - `answer.json`: the witness's last answer to a submission.
//
// And in Desk's custody, outside the project: the submitter's token, pasted
// once, at `secrets/witness/<name>/<holder id>.token`, 0600, `<name>` naming
// the desk as its signing key is named (question 2). It is never shown
// again, never written to a log, an answer or a project file, and passed to
// no process: Desk sends it only as `Authorization: Bearer` on a submission
// to the configured URL, and follows no redirect.
//
// **Everything read back is held to its record before use** (the hand-over's
// rule): the settings, the cursor, the statements, the head and the
// continuation, each to its form. "Could not be read now" is said, never
// taken for "not there". A file Desk cannot use, unreadable, out of its form,
// past its bound, or of a chain the runtime finds equivocating, is moved
// aside in the holder's folder under a name that says when and why, named on
// the decision record, and kept until the owner removes it: Desk removes no
// statement (the archive rule, ADR-0010 as amended 2026-10-08).
//
// # What Desk sends, and when (section 2; questions 3 and 5)
//
// On the owner's interval, 5 to 1440 minutes, 60 unless the owner says
// otherwise, where the trail's head has moved past the cursor, one attempt at
// a time per desk; "Send now" takes the same turn. The body is the lines
// `audit checkpoint --config jpack.json --since <cursor> --limit 300` prints,
// joined untouched through the trail's last chained record
// (`readCheckpoints`), `application/jsonl`: its last line is the head's
// checkpoint, the one the witness signs. Over the witness's 1 MiB, Desk sends
// the whole lines that end with the head's and leaves the earlier ones
// unsent, and says how many: the statement for the head covers them through
// the trail's own chain. Each request has 15 seconds.
//
// # What moves the cursor (section 3; question 4)
//
// Only a `200` whose body is one `checkpoint` statement for the last line
// sent, by trail, sequence and record digest, compared here, and that the
// runtime verifies under the pinned key: a reading with that statement as
// the head, the statements between Desk's last and it fetched first, that
// reports the `witness` section `read`, no `witness-*` finding, and its
// latest checkpoint statement at that index and sequence. No `409`, no
// refusal and no answer Desk could not read moves it.
//
// # How the decision record reads the chain (section 4)
//
// When the panel opens, Desk fetches the trail's head with no token, then the
// statements after its continuation's index, or from 0, up to the head's,
// 1000 at a time, appending each new one; and passes `audit verify` the
// pinned key, a private copy of the statements after the continuation, the
// head, the continuation, and where to save the next. Where the witness could
// not be reached or refused, it passes no head: a historical reading of what
// Desk keeps. `--require-countersigned-through` is passed only for one check
// the owner asks for, which saves nothing (question 6).
//
// # What Desk says
//
// The witness speaks in its own words: each refusal's `reason` and sentence,
// verbatim. Desk's own words for a witness that did not answer as asked are
// only "Desk could not reach the witness", with the HTTP status where there
// was one. What a statement establishes is ADR-0013's, as ADR-0012 section 5
// sets it out, and what the reading found is the runtime's report.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	// witnessChannel is a witness holder's channel, as the hand-over's list
	// shows it.
	witnessChannel = "witness"
	// The files a witness holder's folder keeps.
	witnessSettingsName = "witness.json"
	witnessKeyName      = "witness.pub"
	witnessAnswerName   = "answer.json"
	// witnessVersion is the version of witness.json and answer.json.
	witnessVersion = "1"
	// witnessTokensDir is the folder under `secrets/` that holds each desk's
	// submitter's token, in a folder of the desk's name.
	witnessTokensDir = "witness"
	witnessTokenKept = "its witness tokens"
	// witnessBodyLimit is the witness's request bound (SPEC.md §6).
	witnessBodyLimit = 1 << 20
	// witnessMaxLines bounds the checkpoint lines one submission reads: the
	// batches one hand-over joins, about 1 MiB of lines.
	witnessMaxLines = handoverBatch * handoverBatches
	// witnessLineLimit bounds one statement line, its newline not counted:
	// the witness's line bound (SPEC.md §6). A statement is about 610 bytes.
	witnessLineLimit = 4096
	// witnessAnswerLimit bounds what Desk reads of one answer of the
	// witness's: a statement, a refusal with two statements, or a page of
	// statements (`witnessPageLimit`).
	witnessAnswerLimit = 64 << 10
	// witnessPage is how many statements one read asks for, the witness's
	// most (SPEC.md §6); witnessPageLimit bounds that answer, in bytes.
	witnessPage      = 1000
	witnessPageLimit = witnessPage * (witnessLineLimit + 1)
	// witnessPages bounds the pages one reading fetches; the rest is
	// fetched by the next.
	witnessPages = 110
	// witnessStatementBound and witnessBytesBound are the runtime's bounds on
	// one reading (runtime 0.30.0, `audit.MaxWitnessStatements` and
	// `MaxWitnessBytes`), less the head's and the continuation's room: a
	// longer chain is read in steps.
	witnessStatementBound = 110000 - 3
	witnessBytesBound     = 64<<20 - 4*witnessLineLimit
	// witnessKeepLimit bounds the statements file Desk reads: the runtime's
	// bound on one reading.
	witnessKeepLimit = 64 << 20
	// witnessRequestTimeout is each request's time.
	witnessRequestTimeout = 15 * time.Second
	// The bounds of what the owner gives: the URL, the key as pasted and the
	// token, in bytes; the settings and the answer files Desk writes.
	witnessURLLimit          = 2048
	witnessKeyInputLimit     = 4096
	witnessTokenLimit        = 16 << 10
	witnessSettingsLimit     = 8 << 10
	witnessContinuationLimit = 4 * witnessLineLimit
	witnessRecordLimit       = 64 << 10
	// witnessRequestLimit bounds a request's body to Desk's routes.
	witnessRequestLimit = 64 << 10
	// The purposes a witness token names.
	witnessSetPurpose    = "set-witness"
	witnessRemovePurpose = "remove-witness"
	// witnessWakeEvery is how often a desk's submission loop looks whether a
	// submission is due.
	witnessWakeEvery = time.Minute
	// witnessRuntimeFloor is the first runtime that reads a witness's
	// statements (runtime #228).
	witnessRuntimeFloor = "0.28.0"
)

// Desk's own sentences about a witness. The page lists them
// (WITNESS_REASONS), so that each is shown in the owner's language.
const (
	witnessUnreachedWords = "Desk could not reach the witness"
	witnessRequestWords   = "Send the witness as JSON: its URL, its public key and the interval in minutes."
	witnessURLWords       = "Give the witness's URL as https, or as http to a loopback address such as 127.0.0.1, of at most 2048 characters, with a host, and with no user name, password, query, fragment, space or control character."
	witnessKeyInputWords  = "Give the witness's public key as its operator gave it to you: 64 hexadecimal characters."
	witnessIntervalWords  = "Give the interval as a whole number of minutes from 5 to 1440."
	witnessTokenWords     = "Paste the submitter's token the witness's operator issued for this witness: at most 16384 characters, with no space."
	witnessConfirmWords   = "Confirm the witness with the token Desk gave when it showed it."
	witnessRemoveWords    = "Remove the witness with the token the decision record gave."
	witnessStaleWords     = "The witness's settings changed after Desk showed them, so nothing was changed. Check the decision record again."
	witnessUsedWords      = "This confirmation was used already, so nothing was changed. Check the decision record again for a fresh one."
	witnessNoneWords      = "No witness is set for this desk, so Desk submits nothing."
	witnessBusyWords      = "A submission to the witness is in progress, so Desk starts no other. The decision record shows its outcome once it ends."
	witnessClosedWords    = "Desk is stopping, so it starts no submission."
	witnessStartWords     = "A submission needs no settings: send {} as JSON."
	witnessOwnKeyWords    = "This is the public key of Desk's own local gateway, which is yours as the operator: it is not a witness."
	witnessNewKeyWords    = "This desk has a witness under another key. A pinned key is never changed in place: remove the witness, and add the one under the new key as a new witness."
	witnessNothingWords   = "The trail's head has not moved past the last checkpoint the witness countersigned, so there is nothing to send."
	witnessNoChainedWords = "The trail has no chained record yet, so there is nothing to send."
	witnessRetiredWords   = "The witness retired this trail, so Desk submits nothing more for it. A trail's operator goes on under a new trail, registered anew, which Desk does not make."
	witnessOtherWords     = "The witness answered a statement for another checkpoint than the last one Desk sent, so Desk's cursor did not move."
	witnessUnconfirmed    = "The runtime's reading of the witness's chain did not confirm the statement, so Desk's cursor did not move."
	witnessNoRequireWords = "No witness's statements are passed to the check of this desk's trail, so there is no countersigned coverage to require."
	witnessRequireWords   = "Ask for a countersigned coverage once with a record from 1."
	witnessHolderWords    = "This holder is a witness: Desk submits its checkpoints to it, and nothing is downloaded or confirmed for it."
	witnessKeyReadWords   = "The runtime could not read the key Desk wrote for it, so the key is not checked and nothing was kept."
	witnessKeyOlderWords  = "This runtime (jpack %s) does not read a witness's statements. A witness needs jpack %s or later."
	// With a reason, in Desk's or the custody's words.
	witnessUnreadWords     = "Desk could not read the witness it keeps for this desk, so it uses none of it now: %s."
	witnessKeepWords       = "Desk could not keep this witness, and the decision record shows the witness it reads now: %s."
	witnessCustodyWords    = "Desk keeps no witness token here: %s."
	witnessBelowHeadWords  = "The witness holds a later checkpoint for this trail, at record %d, so Desk sends again only once the trail's head is past it."
	witnessRetryWords      = "The witness asked for no submission before %s, so Desk sends nothing before then."
	witnessNotRunWords     = "The submission did not finish: %s."
	witnessSetAsideWords   = "Desk moved %s aside in the witness's folder, and keeps it there until you remove it: %s."
	witnessReadingWords    = "Desk did not read the witness's chain this time: %s."
	witnessLocalReadWords  = "Desk could not read its own local gateway's key now, so it cannot tell this key from it: %s."
	witnessUndocumentedKey = "The runtime did not answer the check of the key as documented, so nothing was kept."
)

// The reasons a file is moved aside, as its name says them, and Desk's
// sentence for each.
var witnessSetAsideReasons = map[string]string{
	"unreadable":   "Desk could not read it as a file it keeps",
	"form":         "it is not in the form Desk keeps it in",
	"too-large":    "it is larger than the runtime reads in one reading",
	"equivocation": "the runtime found two statements of one index that differ in it, proof that the witness signed two chains",
}

var (
	// witnessedAtForm is a statement's `witnessedAt` (SPEC.md §8.2).
	witnessedAtForm = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)
	// witnessKinds are a statement's kinds.
	witnessKinds = map[string]bool{"checkpoint": true, "conflict": true, "retirement": true}
	// witnessTokenForm is a submitter's token as Desk keeps it: visible
	// ASCII, no space.
	witnessTokenForm = regexp.MustCompile(`^[\x21-\x7e]+$`)
	// witnessSetAsideForm is a file Desk moved aside in a holder's folder.
	witnessSetAsideForm = regexp.MustCompile(`^((?:statements|head)-[0-9a-f]{32}\.jsonl|continuation-[0-9a-f]{32}\.json|answer\.json|record\.json)\.set-aside-([0-9]{8}T[0-9]{6}Z)-(unreadable|form|too-large|equivocation)$`)
)

// witnessClock is Desk's clock for a witness: when the settings were set,
// when an attempt was made, and whether one is due. A variable only so a
// test can fix it.
var witnessClock = time.Now

// newWitnessWake is what wakes the submission loop of the desk whose name is
// given, and how to stop it. A variable only so a test can wake it by hand.
var newWitnessWake = func(name string) (<-chan time.Time, func()) {
	ticker := time.NewTicker(witnessWakeEvery)
	return ticker.C, ticker.Stop
}

// testHookWitnessWoke identifies the wake channel after its loop has acted,
// and is nil outside tests.
var testHookWitnessWoke func(<-chan time.Time)

// witnessTransport is the transport every request to a witness goes by: a
// variable only so a test can count what reaches the network.
var witnessTransport http.RoundTripper = http.DefaultTransport

/* What Desk keeps ----------------------------------------------------------- */

// witnessFile is `witness.json` as Desk writes it.
type witnessFile struct {
	Version         string `json:"version"`
	URL             string `json:"url"`
	PublicKey       string `json:"publicKey"`
	IntervalMinutes int64  `json:"intervalMinutes"`
	SetAt           int64  `json:"setAt"`
}

// witnessCursor is what a holder's record keeps of one trail at the witness:
// the last sequence it countersigned for this desk, its statement's index
// and the record digest, and when the cursor moved (Through 0 where none
// was); a head the witness answered `below-head` with, after which Desk
// sends again only once the trail's head is past it; and whether the
// witness retired the trail.
type witnessCursor struct {
	Through   int64  `json:"through"`
	Index     int64  `json:"index"`
	Digest    string `json:"digest,omitempty"`
	At        int64  `json:"at"`
	BelowHead int64  `json:"belowHead,omitempty"`
	Retired   bool   `json:"retired,omitempty"`
}

// checkWitnessCursor is whether entry is one Desk writes for trail.
func checkWitnessCursor(trail string, entry witnessCursor) bool {
	if !keyIDForm.MatchString(trail) || entry.Through < 0 || entry.Index < 0 || entry.At < 0 || entry.BelowHead < 0 {
		return false
	}
	if entry.Through == 0 {
		return entry.Index == 0 && entry.Digest == ""
	}
	return recordForm.MatchString(entry.Digest)
}

// witnessStatement is what Desk reads of one statement: its kind, the
// checkpoint it names and its index. Desk reads the form alone; whether it
// verifies, and whether the chain holds, is the runtime's to say.
type witnessStatement struct {
	kind       string
	checkpoint checkpointLine
	index      int64
}

// readWitnessStatement holds one statement line, its newline not counted, to
// the statement's form (SPEC.md §8.2): one JSON object of exactly eight
// members, `witnessVersion` "1", a `kind` of the three, a `checkpoint` of the
// runtime's shape, an `index` from 0 to 2^53-2 in digits alone, a
// `prevSignature` null or 128 lowercase hexadecimal characters, a
// `witnessedAt` of its form, a `keyId` of 32 and a `signature` of 128
// lowercase hexadecimal characters, each named once.
func readWitnessStatement(line []byte) (witnessStatement, bool) {
	if len(line) == 0 || len(line) > witnessLineLimit || !utf8.Valid(line) || !json.Valid(line) {
		return witnessStatement{}, false
	}
	members, ok := exactMembers(line)
	if !ok || len(members) != 8 {
		return witnessStatement{}, false
	}
	var version, kind, witnessedAt, keyID, signature string
	if !sidecarString(members["witnessVersion"], &version) || version != "1" ||
		!sidecarString(members["kind"], &kind) || !witnessKinds[kind] ||
		!sidecarString(members["witnessedAt"], &witnessedAt) || !witnessedAtForm.MatchString(witnessedAt) ||
		!sidecarString(members["keyId"], &keyID) || !keyIDForm.MatchString(keyID) ||
		!sidecarString(members["signature"], &signature) || !signatureForm.MatchString(signature) {
		return witnessStatement{}, false
	}
	index, ok := statementIndex(members["index"])
	if !ok {
		return witnessStatement{}, false
	}
	if prev := members["prevSignature"]; string(prev) != "null" {
		var previous string
		if !sidecarString(prev, &previous) || !signatureForm.MatchString(previous) {
			return witnessStatement{}, false
		}
	}
	checkpoint, ok := readCheckpointLine(members["checkpoint"])
	if !ok {
		return witnessStatement{}, false
	}
	return witnessStatement{kind: kind, checkpoint: checkpoint, index: index}, true
}

// statementIndex reads a statement's index: 0, or an integer in digits alone
// from 1 to 2^53-2.
func statementIndex(raw json.RawMessage) (int64, bool) {
	if string(raw) == "0" {
		return 0, true
	}
	var index int64
	return index, sidecarInteger(raw, &index)
}

// keptStatement is one statement line as kept, its newline included, and
// what Desk read of it.
type keptStatement struct {
	line []byte
	witnessStatement
}

// readStatementLines holds data, a file of statements, to its form: whole
// lines, each a statement of trail, none twice. A file that is not is an
// error; an empty one is no statement.
func readStatementLines(data []byte, trail string) ([]keptStatement, error) {
	if len(data) > 0 && data[len(data)-1] != '\n' {
		return nil, errors.New("it does not end with a whole line")
	}
	var kept []keptStatement
	seen := map[string]bool{}
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		statement, ok := readWitnessStatement(line[:len(line)-1])
		switch {
		case !ok:
			return nil, errors.New("it holds a line that is not a statement")
		case statement.checkpoint.trail != trail:
			return nil, errors.New("it holds a statement of another trail")
		case seen[string(line)]:
			return nil, errors.New("it holds a statement twice")
		}
		seen[string(line)] = true
		kept = append(kept, keptStatement{line: line, witnessStatement: statement})
	}
	return kept, nil
}

// witnessContinuation is what Desk reads of a continuation the runtime saved:
// the index of its last statement.
type witnessContinuation struct {
	last int64
}

// readContinuation holds data to the continuation's form (runtime 0.30.0):
// one object of exactly `continuationVersion` "1", `last` and
// `latestCheckpoint`, each a statement of trail, the latest a checkpoint
// statement at or before the last.
func readContinuation(data []byte, trail string) (witnessContinuation, bool) {
	data = bytes.TrimSuffix(data, []byte("\n"))
	if len(data) == 0 || len(data) > witnessContinuationLimit || !json.Valid(data) {
		return witnessContinuation{}, false
	}
	members, ok := exactMembers(data)
	var version string
	if !ok || len(members) != 3 || !sidecarString(members["continuationVersion"], &version) || version != "1" {
		return witnessContinuation{}, false
	}
	last, lastOK := readWitnessStatement(compactJSON(members["last"]))
	latest, latestOK := readWitnessStatement(compactJSON(members["latestCheckpoint"]))
	if !lastOK || !latestOK || last.checkpoint.trail != trail || latest.checkpoint.trail != trail ||
		latest.kind != "checkpoint" || latest.index > last.index {
		return witnessContinuation{}, false
	}
	return witnessContinuation{last: last.index}, true
}

// compactJSON is raw with the white space between its tokens removed, or raw
// where it is not JSON.
func compactJSON(raw json.RawMessage) []byte {
	var out bytes.Buffer
	if json.Compact(&out, raw) != nil {
		return raw
	}
	return out.Bytes()
}

/* The owner's proposal ------------------------------------------------------ */

// checkWitnessURL is whether address is a witness's URL Desk keeps: https,
// or http to a loopback address written as one, with a host, of at most
// witnessURLLimit bytes of valid UTF-8, with no user name or password, no
// query, no fragment, and no space or control character.
func checkWitnessURL(address string) bool {
	if address == "" || len(address) > witnessURLLimit || !utf8.ValidString(address) ||
		strings.ContainsAny(address, "#?") || strings.IndexFunc(address, func(r rune) bool { return r <= ' ' || r == 0x7f || r > 0x7e }) >= 0 {
		return false
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return true
	case "http":
		ip := net.ParseIP(parsed.Hostname())
		return ip != nil && ip.IsLoopback()
	}
	return false
}

// witnessEndpoint is the witness's URL with one of SPEC.md §6's paths
// appended.
func witnessEndpoint(base, suffix string) string {
	return strings.TrimRight(base, "/") + suffix
}

// witnessProposal is what the owner proposes, as the page sends it.
type witnessProposal struct {
	URL             string `json:"url"`
	PublicKey       string `json:"publicKey"`
	IntervalMinutes *int64 `json:"intervalMinutes"`
}

// witnessPlan is a proposal held to Desk's rules; key is the key as given,
// trimmed, which the runtime holds to its rule.
type witnessPlan struct {
	url      string
	key      string
	interval int64
}

// plan is the proposal held to Desk's rules, or the sentence that says which
// it breaks. The key is held only to a bound and to text: its rule is the
// runtime's (`checkWitnessKey`). The interval is 60 minutes where none is
// given.
func (p witnessProposal) plan() (witnessPlan, string) {
	plan := witnessPlan{url: p.URL, key: strings.TrimSpace(p.PublicKey), interval: stampingDefaultInterval}
	if !checkWitnessURL(p.URL) {
		return witnessPlan{}, witnessURLWords
	}
	if plan.key == "" || len(p.PublicKey) > witnessKeyInputLimit || !utf8.ValidString(plan.key) ||
		strings.IndexFunc(plan.key, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0 {
		return witnessPlan{}, witnessKeyInputWords
	}
	if p.IntervalMinutes != nil {
		plan.interval = *p.IntervalMinutes
	}
	if plan.interval < stampingMinInterval || plan.interval > stampingMaxInterval {
		return witnessPlan{}, witnessIntervalWords
	}
	return plan, ""
}

// pinned is the key as Desk keeps it, once the runtime accepted it: as the
// runtime reads it, trimmed and in lowercase.
func (p witnessPlan) pinned() string {
	return strings.ToLower(p.key)
}

// digest is the SHA-256 of what the plan keeps, as a token binds it.
func (p witnessPlan) digest() string {
	data, _ := json.Marshal(witnessFile{Version: witnessVersion, URL: p.url, PublicKey: p.pinned(), IntervalMinutes: p.interval})
	return sha256Digest(data)
}

// witnessShown is a witness's settings as the page is shown them, before a
// confirmation and on the decision record. The token is never among them.
type witnessShown struct {
	URL             string `json:"url"`
	PublicKey       string `json:"publicKey"`
	IntervalMinutes int64  `json:"intervalMinutes"`
	SetAt           int64  `json:"setAt,omitempty"`
}

/* The submitter's token, in Desk's custody ------------------------------------- */

// openWitnessTokens opens `secrets/witness/<name>/` in Desk's custody; with
// create, made owner-only where missing.
func (s *Server) openWitnessTokens(create bool) (*os.Root, error) {
	if !s.assistant.usable() {
		return nil, fmt.Errorf(witnessCustodyWords, strings.TrimRight(s.custodyWords(s.assistant.problem.Error()), "."))
	}
	name := s.signingKeyName()
	if !stampingName.MatchString(name) {
		return nil, fmt.Errorf(witnessCustodyWords, "this desk has no name its token can be kept under")
	}
	root, _, err := openOwnFolder(s.assistant.secrets, []string{witnessTokensDir, name}, create, witnessTokenKept)
	return root, err
}

// witnessTokenName is the token's file for the holder id.
func witnessTokenName(id string) string { return id + ".token" }

// readWitnessToken is the holder's token, held to its form. A token that is
// not there is fs.ErrNotExist; one that cannot be read now is that error.
func (s *Server) readWitnessToken(id string) (string, error) {
	folder, err := s.openWitnessTokens(false)
	if err != nil {
		return "", err
	}
	defer folder.Close()
	data, err := readPrivateData(folder, witnessTokenName(id), witnessTokenLimit)
	if err != nil {
		return "", err
	}
	if !witnessTokenForm.Match(data) {
		return "", errors.New("the token Desk keeps is not in the form it keeps one")
	}
	return string(data), nil
}

/* The witness holder ---------------------------------------------------------- */

// witnessHolder is this desk's witness as Desk read it back and held it to
// its record: the holder, its settings and their bytes, and its record.
type witnessHolder struct {
	holder handoverHolder
	file   witnessFile
	raw    []byte
	record handoverRecord
}

// findWitnessHolder is the witness holder among holders: one at most.
func findWitnessHolder(holders []handoverHolder) (handoverHolder, bool) {
	index := slices.IndexFunc(holders, func(h handoverHolder) bool { return h.Witness })
	if index < 0 {
		return handoverHolder{}, false
	}
	return holders[index], true
}

// readWitnessFile holds witness.json's bytes to the writer's rules.
func readWitnessFile(raw []byte) (witnessFile, bool) {
	var file witnessFile
	if decodeDataJSON(raw, &file) != nil || file.Version != witnessVersion || !checkWitnessURL(file.URL) ||
		!publicKeyForm.MatchString(file.PublicKey) || file.IntervalMinutes < stampingMinInterval ||
		file.IntervalMinutes > stampingMaxInterval || file.SetAt < 0 {
		return witnessFile{}, false
	}
	return file, true
}

// readWitness reads this desk's witness: found is false where Desk keeps no
// witness holder. A holder whose settings, key file or record cannot be read
// now, or are not what Desk writes, is an error. The caller holds handoverMu.
func (s *Server) readWitness() (witness witnessHolder, found bool, err error) {
	handover, err := s.openHandover(false)
	if errors.Is(err, fs.ErrNotExist) {
		return witnessHolder{}, false, nil
	}
	if err != nil {
		return witnessHolder{}, false, err
	}
	defer handover.Close()
	holders, err := readHolders(handover)
	if err != nil {
		return witnessHolder{}, false, err
	}
	holder, found := findWitnessHolder(holders)
	if !found {
		return witnessHolder{}, false, nil
	}
	folder, err := openHolderFolder(handover, holder.ID, false)
	if err != nil {
		return witnessHolder{}, false, err
	}
	defer folder.Close()
	raw, err := readPrivateData(folder, witnessSettingsName, witnessSettingsLimit)
	if err != nil {
		return witnessHolder{}, false, err
	}
	file, ok := readWitnessFile(raw)
	if !ok {
		return witnessHolder{}, false, fmt.Errorf("%s is not a witness Desk wrote", witnessSettingsName)
	}
	key, err := readPrivateData(folder, witnessKeyName, witnessKeyInputLimit)
	if err != nil {
		return witnessHolder{}, false, err
	}
	if string(key) != file.PublicKey+"\n" {
		return witnessHolder{}, false, fmt.Errorf("%s is not the key %s names", witnessKeyName, witnessSettingsName)
	}
	record, err := readHandoverRecord(folder)
	if err != nil {
		return witnessHolder{}, false, err
	}
	return witnessHolder{holder: holder, file: file, raw: raw, record: record}, true, nil
}

// holderPath is the path, relative to the project, of a file in the witness
// holder's folder: what `runRuntime`, which runs in the project, is given.
func (w witnessHolder) holderPath(name string) string {
	return path.Join(handoverDir, w.holder.ID, name)
}

// The names of a trail's files in a witness holder's folder.
func statementsName(trail string) string   { return "statements-" + trail + ".jsonl" }
func headName(trail string) string         { return "head-" + trail + ".jsonl" }
func continuationName(trail string) string { return "continuation-" + trail + ".json" }
func readingName(trail string) string      { return ".reading-" + trail + ".jsonl" }
func submittedName(trail string) string    { return ".submitted-" + trail + ".jsonl" }

// shown is the settings as the page is shown them.
func (w witnessHolder) shown() witnessShown {
	return witnessShown{URL: w.file.URL, PublicKey: w.file.PublicKey, IntervalMinutes: w.file.IntervalMinutes, SetAt: w.file.SetAt}
}

/* The files of a reading -------------------------------------------------------- */

// setAside moves name aside in folder, under a name that says when and why
// (`witnessSetAsideForm`), and says so in Desk's log: the archive rule. A file
// that is not there is nothing to move. It answers the name it was given.
func (s *Server) setAside(folder *os.Root, name, reason string) (string, error) {
	aside := name + ".set-aside-" + witnessClock().UTC().Format("20060102T150405Z") + "-" + reason
	if _, err := folder.Lstat(aside); err == nil {
		// One a second, at most; a second in the same second waits for the
		// next name.
		return "", fmt.Errorf("%s was moved aside already in this second", name)
	}
	if err := folder.Rename(name, aside); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	if err := syncPrivateDirectory(folder); err != nil {
		s.log.Printf("desk: the witness's folder could not be synced after %s was moved aside: %v", name, err)
	}
	s.log.Printf("desk: %s was moved aside in the witness's folder as %s: %s", name, aside, witnessSetAsideReasons[reason])
	return aside, nil
}

// statementsKept reads the statements file of trail in folder, held to its
// form, and the bytes it holds. A file that is not there is no statement. A
// file Desk cannot use is moved aside, and named, and none of it is used.
// An error is a file that could not be read now, or moved aside.
func (s *Server) statementsKept(folder *os.Root, trail string) (data []byte, kept []keptStatement, aside string, err error) {
	name := statementsName(trail)
	data, err = readPrivateData(folder, name, witnessKeepLimit)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil, "", nil
	case err != nil && codeOf(err) == CodeTooLarge:
		aside, err = s.setAside(folder, name, "too-large")
		return nil, nil, aside, err
	case err != nil && codeOf(err) == CodeForbidden:
		aside, err = s.setAside(folder, name, "unreadable")
		return nil, nil, aside, err
	case err != nil:
		return nil, nil, "", err
	}
	kept, formErr := readStatementLines(data, trail)
	if formErr != nil {
		aside, err = s.setAside(folder, name, "form")
		return nil, nil, aside, err
	}
	return data, kept, "", nil
}

// keepStatements appends to the statements file of trail in folder each of
// lines, statements of trail as received, newline-ended, that it does not
// hold: the file as read stays the start of what is written. It answers what
// the file holds then, and the name of a file it moved aside first.
func (s *Server) keepStatements(folder *os.Root, trail string, lines [][]byte) ([]keptStatement, string, error) {
	data, kept, aside, err := s.statementsKept(folder, trail)
	if err != nil {
		return nil, aside, err
	}
	held := map[string]bool{}
	for _, statement := range kept {
		held[string(statement.line)] = true
	}
	next := slices.Clip(data)
	for _, line := range lines {
		statement, ok := readWitnessStatement(bytes.TrimSuffix(line, []byte("\n")))
		if !ok || statement.checkpoint.trail != trail || !bytes.HasSuffix(line, []byte("\n")) {
			return nil, aside, errors.New("a statement to keep is not one of this trail")
		}
		if held[string(line)] {
			continue
		}
		held[string(line)] = true
		next = append(next, line...)
		kept = append(kept, keptStatement{line: line, witnessStatement: statement})
	}
	if len(next) == len(data) {
		return kept, aside, nil
	}
	if len(next) > witnessKeepLimit {
		return nil, aside, fmt.Errorf("the statements Desk keeps would pass the %d bytes it reads of them", witnessKeepLimit)
	}
	if err := writePrivateData(folder, statementsName(trail), next); err != nil {
		return nil, aside, err
	}
	return kept, aside, nil
}

// continuationKept reads the continuation of trail in folder, held to its
// form: found is false where there is none. One Desk cannot use is moved
// aside, and named.
func (s *Server) continuationKept(folder *os.Root, trail string) (continuation witnessContinuation, found bool, aside string, err error) {
	name := continuationName(trail)
	if err := ownRuntimeFile(folder, name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		aside, err = s.setAside(folder, name, "unreadable")
		return witnessContinuation{}, false, aside, err
	}
	data, err := readPrivateData(folder, name, witnessContinuationLimit)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return witnessContinuation{}, false, "", nil
	case err != nil && (codeOf(err) == CodeTooLarge || codeOf(err) == CodeForbidden):
		aside, err = s.setAside(folder, name, "unreadable")
		return witnessContinuation{}, false, aside, err
	case err != nil:
		return witnessContinuation{}, false, "", err
	}
	continuation, ok := readContinuation(data, trail)
	if !ok {
		aside, err = s.setAside(folder, name, "form")
		return witnessContinuation{}, false, aside, err
	}
	return continuation, true, "", nil
}

// ownRuntimeFile narrows a file the runtime wrote in folder, the
// continuation, to the mode Desk keeps its files in: the runtime writes it as
// any file it saves, and the holder's folder is open to no one else. Only a
// regular file of this user, not a link, the one that was looked at, by its
// own descriptor.
func ownRuntimeFile(folder *os.Root, name string) error {
	info, err := folder.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", name)
	}
	if err := ownedByUs(name, info); err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	file, err := folder.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errPrivateDataChanged
	}
	return file.Chmod(custodyFileMode)
}

/* The witness's answers ------------------------------------------------------- */

// witnessClient is the client every request to a witness is made with: 15
// seconds, and no redirect followed, so the token goes to the configured
// URL and nowhere else.
func witnessClient() *http.Client {
	return &http.Client{
		Transport:     witnessTransport,
		Timeout:       witnessRequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// witnessReply is one answer of the witness's, as Desk read it: Reached is
// false where there was no answer of the witness's form (`Status` then the
// HTTP status where there was one). A refusal is its `reason` and its
// sentence, as the witness gave them; a 409 carries statements, and a 200
// carries them as its body.
type witnessReply struct {
	Reached    bool
	Status     int
	Reason     string
	Error      string
	RetryAfter int64
	statements [][]byte
}

// statementsBody holds a 200's body to a list of statement lines, each
// newline-ended, at least one; another body is none.
func statementsBody(body []byte) ([][]byte, bool) {
	if len(body) == 0 || body[len(body)-1] != '\n' {
		return nil, false
	}
	var lines [][]byte
	for _, line := range bytes.SplitAfter(body, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		if _, ok := readWitnessStatement(line[:len(line)-1]); !ok {
			return nil, false
		}
		lines = append(lines, line)
	}
	return lines, len(lines) > 0
}

// readWitnessReply reads what the witness answered. A statement answer is
// `200`, `application/jsonl`, its statements each a line; with statements
// allowed empty, none at all (a read past the head). A refusal is
// `application/json`, an object of exactly `error` and `reason`, each a
// string that is not empty, and, with a 409, `statements`, each a statement
// line, without its newline, as a string. Anything else is not of the
// witness's form.
func readWitnessReply(response *http.Response, body []byte, empty bool) witnessReply {
	reply := witnessReply{Status: response.StatusCode}
	media := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if response.StatusCode == http.StatusOK {
		if media != "application/jsonl" {
			return reply
		}
		if empty && len(body) == 0 {
			reply.Reached = true
			return reply
		}
		lines, ok := statementsBody(body)
		if ok {
			reply.Reached, reply.statements = true, lines
		}
		return reply
	}
	if media != "application/json" || !json.Valid(body) {
		return reply
	}
	members, ok := exactMembers(body)
	if !ok {
		return reply
	}
	var sentence, reason string
	if !sidecarString(members["error"], &sentence) || sentence == "" || !sidecarString(members["reason"], &reason) || reason == "" {
		return reply
	}
	allowed := 2
	if raw, has := members["statements"]; has {
		if response.StatusCode != http.StatusConflict {
			return reply
		}
		var texts []string
		if json.Unmarshal(raw, &texts) != nil || len(texts) == 0 {
			return reply
		}
		for _, text := range texts {
			if _, ok := readWitnessStatement([]byte(text)); !ok {
				return reply
			}
			reply.statements = append(reply.statements, append([]byte(text), '\n'))
		}
		allowed = 3
	}
	if len(members) != allowed {
		reply.statements = nil
		return reply
	}
	reply.Reached, reply.Reason, reply.Error = true, reason, sentence
	if seconds, err := strconv.ParseInt(strings.TrimSpace(response.Header.Get("Retry-After")), 10, 64); err == nil && seconds > 0 && seconds <= 86400 {
		reply.RetryAfter = seconds
	}
	return reply
}

// ask makes one request of the witness and reads its answer, bounded by
// limit. A request that could not be made, an answer past its bound, and
// one not of the witness's form are each not reached, with the status where
// there was one. Nothing of the request, its token included, is in what
// this answers.
func ask(ctx context.Context, request *http.Request, limit int, empty bool) witnessReply {
	response, err := witnessClient().Do(request.WithContext(ctx))
	if err != nil {
		return witnessReply{}
	}
	defer response.Body.Close()
	body, err := readBounded(response.Body, limit)
	if err != nil {
		return witnessReply{Status: response.StatusCode}
	}
	return readWitnessReply(response, body, empty)
}

// submit POSTs body to the witness under token.
func submit(ctx context.Context, base, token string, body []byte) witnessReply {
	request, err := http.NewRequest(http.MethodPost, witnessEndpoint(base, "/witness/checkpoints"), bytes.NewReader(body))
	if err != nil {
		return witnessReply{}
	}
	request.Header.Set("Content-Type", "application/jsonl")
	request.Header.Set("Authorization", "Bearer "+token)
	reply := ask(ctx, request, witnessAnswerLimit, false)
	if reply.Reached && reply.Status == http.StatusOK && len(reply.statements) != 1 {
		// A submission is answered with one statement, for its last line.
		return witnessReply{Status: reply.Status}
	}
	return reply
}

// fetchHead asks the witness for the trail's head, with no token.
func fetchHead(ctx context.Context, base, trail string) witnessReply {
	request, err := http.NewRequest(http.MethodGet, witnessEndpoint(base, "/witness/trails/"+trail+"/head"), nil)
	if err != nil {
		return witnessReply{}
	}
	reply := ask(ctx, request, witnessAnswerLimit, false)
	if reply.Reached && reply.Status == http.StatusOK && len(reply.statements) != 1 {
		return witnessReply{Status: reply.Status}
	}
	return reply
}

// fetchStatements asks the witness for the trail's statements from index
// from, with no token, a page at most.
func fetchStatements(ctx context.Context, base, trail string, from int64) witnessReply {
	request, err := http.NewRequest(http.MethodGet, witnessEndpoint(base, "/witness/trails/"+trail+"/statements?from="+strconv.FormatInt(from, 10)+"&limit="+strconv.Itoa(witnessPage)), nil)
	if err != nil {
		return witnessReply{}
	}
	return ask(ctx, request, witnessPageLimit, true)
}

// fetchAfter fetches the statements of trail after the index after (from 0
// where after is -1) through the index through, a page at a time, at most
// witnessPages pages, each in index order from the one asked. It answers the
// lines fetched, and the reply that ended the fetch where it was not a page:
// a refusal, or no answer of the witness's form.
func fetchAfter(ctx context.Context, base, trail string, after, through int64) ([][]byte, *witnessReply) {
	var lines [][]byte
	next := after + 1
	for range witnessPages {
		if next > through {
			return lines, nil
		}
		reply := fetchStatements(ctx, base, trail, next)
		if !reply.Reached || reply.Status != http.StatusOK {
			return lines, &reply
		}
		if len(reply.statements) == 0 {
			return lines, nil
		}
		for _, line := range reply.statements {
			statement, _ := readWitnessStatement(line[:len(line)-1])
			if statement.index != next || statement.checkpoint.trail != trail {
				// Not the page asked for: of the witness's form, and not
				// what was asked.
				return lines, &witnessReply{Status: reply.Status}
			}
			lines = append(lines, line)
			next++
		}
	}
	return lines, nil
}

/* The submission loop ------------------------------------------------------------- */

// The outcomes of a submission (`witnessAttempt.Outcome`).
const (
	// witnessCountersigned: the witness answered a statement for the last
	// line sent, and the runtime's reading confirmed it; the cursor moved.
	witnessCountersigned = "countersigned"
	// witnessKept: the witness answered a statement, kept, and the cursor
	// did not move; Problem, and the reading's findings, say why.
	witnessKept = "kept"
	// witnessRefused: the witness refused, in its words.
	witnessRefused = "refused"
	// witnessUnreached: no answer of the witness's form.
	witnessUnreached = "unreached"
	// witnessNothing: nothing to send, or nothing sent; Problem says why.
	witnessNothing = "nothing"
	// witnessProblem: Desk could not make the submission; Problem says why.
	witnessProblem = "problem"
)

// witnessAttempt is one submission's outcome: when, by Desk's clock; whether
// the owner asked for it; what was sent (the cursor it followed, the first
// and last sequence sent, how many lines, and how many it left unsent); and
// the witness's answer, its status, reason and sentence verbatim, or Desk's
// words, or the runtime's.
type witnessAttempt struct {
	At          int64               `json:"at"`
	Requested   bool                `json:"requested,omitempty"`
	Outcome     string              `json:"outcome"`
	Trail       string              `json:"trail,omitempty"`
	From        int64               `json:"from,omitempty"`
	First       int64               `json:"first,omitempty"`
	Through     int64               `json:"through,omitempty"`
	Lines       int64               `json:"lines,omitempty"`
	Unsent      int64               `json:"unsent,omitempty"`
	Status      int                 `json:"status,omitempty"`
	Reason      string              `json:"reason,omitempty"`
	Error       string              `json:"error,omitempty"`
	RetryAfter  int64               `json:"retryAfter,omitempty"`
	Index       *int64              `json:"index,omitempty"`
	Problem     string              `json:"problem,omitempty"`
	Diagnostics []runtimeDiagnostic `json:"diagnostics,omitempty"`
	Findings    []auditFinding      `json:"findings,omitempty"`
}

// witnessAnswerFile is `answer.json`: the last attempt that reached the
// network.
type witnessAnswerFile struct {
	Version string         `json:"version"`
	Attempt witnessAttempt `json:"attempt"`
}

// witnessScheduler is one desk's submissions to its witness: one loop, one
// attempt at a time, and what it last did.
type witnessScheduler struct {
	s *Server
	// turn is held by the one attempt in progress, scheduled or asked for.
	turn    sync.Mutex
	running atomic.Bool
	// mu guards the rest.
	mu          sync.Mutex
	lastAttempt time.Time
	// notBefore is the witness's Retry-After, as Desk's clock reads it.
	notBefore time.Time
	last      *witnessAttempt
	said      string
	closed    bool
	stop      chan struct{}
	done      chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
}

// startWitness starts this desk's submission loop: one loop, until Close.
func (s *Server) startWitness() {
	ctx, cancel := context.WithCancel(context.Background())
	ws := &witnessScheduler{s: s, stop: make(chan struct{}), done: make(chan struct{}), ctx: ctx, cancel: cancel}
	s.witnessLoop = ws
	wake, stop := newWitnessWake(s.signingKeyName())
	go func() {
		defer close(ws.done)
		defer stop()
		for {
			select {
			case <-ws.stop:
				return
			case <-wake:
			}
			ws.wake()
			if testHookWitnessWoke != nil {
				testHookWitnessWoke(wake)
			}
		}
	}()
}

// close stops the loop, and waits for an attempt in progress. Once closed,
// nothing starts one.
func (ws *witnessScheduler) close() {
	if ws == nil {
		return
	}
	ws.mu.Lock()
	if ws.closed {
		ws.mu.Unlock()
		return
	}
	ws.closed = true
	ws.mu.Unlock()
	close(ws.stop)
	ws.cancel()
	<-ws.done
	ws.turn.Lock()
	ws.turn.Unlock()
}

// report is the last attempt since Desk started, and whether one is running.
func (ws *witnessScheduler) report() (*witnessAttempt, bool) {
	if ws == nil {
		return nil, false
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.last == nil {
		return nil, ws.running.Load()
	}
	last := *ws.last
	return &last, ws.running.Load()
}

// settingsChanged makes the next wake an attempt.
func (ws *witnessScheduler) settingsChanged() {
	if ws == nil {
		return
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.lastAttempt, ws.notBefore = time.Time{}, time.Time{}
}

// wake is one wake of the loop: where a witness is set, its interval has
// passed since the last attempt, and the witness's Retry-After is past, one
// attempt, unless one is in progress.
func (ws *witnessScheduler) wake() {
	s := ws.s
	s.witnessMu.RLock()
	s.handoverMu.Lock()
	witness, found, err := s.readWitness()
	s.handoverMu.Unlock()
	s.witnessMu.RUnlock()
	ws.mu.Lock()
	if err != nil && err.Error() != ws.said {
		ws.said = err.Error()
		s.log.Printf("desk: the witness of desk %s could not be read, so nothing is submitted: %v", s.signingKeyName(), err)
	} else if err == nil {
		ws.said = ""
	}
	now := witnessClock()
	due := err == nil && found && !ws.closed && !now.Before(ws.notBefore) &&
		(ws.lastAttempt.IsZero() || !now.Before(ws.lastAttempt.Add(time.Duration(witness.file.IntervalMinutes)*time.Minute)))
	ws.mu.Unlock()
	if !due || !ws.turn.TryLock() {
		return
	}
	defer ws.turn.Unlock()
	ws.mu.Lock()
	ws.lastAttempt = now
	ws.mu.Unlock()
	ws.attempt(false)
}

// record keeps attempt as the last, and in answer.json where it reached the
// network, and says it in Desk's log, never with the token, and never with
// the witness's URL, which may name more than the owner meant to show.
func (ws *witnessScheduler) record(attempt witnessAttempt) witnessAttempt {
	s := ws.s
	ws.mu.Lock()
	kept := attempt
	ws.last = &kept
	if attempt.RetryAfter > 0 {
		ws.notBefore = time.Unix(attempt.At, 0).Add(time.Duration(attempt.RetryAfter) * time.Second)
	}
	ws.mu.Unlock()
	by := "the scheduler"
	if attempt.Requested {
		by = "the owner's request"
	}
	switch attempt.Outcome {
	case witnessCountersigned:
		s.log.Printf("desk: a submission of desk %s to its witness, on %s: countersigned through record %d", s.signingKeyName(), by, attempt.Through)
	case witnessRefused:
		s.log.Printf("desk: a submission of desk %s to its witness, on %s, was refused: %d %s", s.signingKeyName(), by, attempt.Status, attempt.Reason)
	case witnessUnreached:
		s.log.Printf("desk: a submission of desk %s to its witness, on %s: %s (HTTP %d)", s.signingKeyName(), by, witnessUnreachedWords, attempt.Status)
	default:
		s.log.Printf("desk: a submission of desk %s to its witness, on %s, %s: %s", s.signingKeyName(), by, attempt.Outcome, attempt.Problem)
	}
	return attempt
}

// keepAnswer writes the attempt to the holder's answer.json. The caller holds
// handoverMu.
func (s *Server) keepAnswer(id string, attempt witnessAttempt) {
	handover, err := s.openHandover(false)
	if err == nil {
		var folder *os.Root
		folder, err = openHolderFolder(handover, id, false)
		handover.Close()
		if err == nil {
			var data []byte
			data, err = json.Marshal(witnessAnswerFile{Version: witnessVersion, Attempt: attempt})
			if err == nil && len(data)+1 <= witnessRecordLimit {
				err = writePrivateData(folder, witnessAnswerName, append(data, '\n'))
			}
			folder.Close()
		}
	}
	if err != nil {
		s.log.Printf("desk: the witness's last answer could not be kept: %v", err)
	}
}

// readAnswer is the holder's answer.json, held to its form: nil where there
// is none; an error where it cannot be read now or is not what Desk writes.
func readAnswer(folder *os.Root) (*witnessAttempt, error) {
	data, err := readPrivateData(folder, witnessAnswerName, witnessRecordLimit)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file witnessAnswerFile
	if decodeDataJSON(data, &file) != nil || file.Version != witnessVersion || file.Attempt.At < 0 ||
		!slices.Contains([]string{witnessCountersigned, witnessKept, witnessRefused, witnessUnreached}, file.Attempt.Outcome) {
		return nil, fmt.Errorf("%s is not an answer Desk wrote", witnessAnswerName)
	}
	return &file.Attempt, nil
}

// witnessBody is the body of a submission from the lines the runtime
// printed, data: all of them where they fit the witness's bound, and
// otherwise the whole lines that end with the last, as many as fit. It
// answers the body, the sequence of its first line, and how many lines it
// left out.
func witnessBody(data []byte) (body []byte, first int64, unsent int64) {
	start := 0
	for len(data)-start > witnessBodyLimit {
		next := bytes.IndexByte(data[start:], '\n')
		if next < 0 {
			return nil, 0, 0
		}
		start += next + 1
		unsent++
	}
	body = data[start:]
	if end := bytes.IndexByte(body, '\n'); end > 0 {
		if line, ok := readCheckpointLine(body[:end]); ok {
			first = line.sequence
		}
	}
	return body, first, unsent
}

// lastLine is data's last line, without its newline.
func lastLine(data []byte) []byte {
	data = bytes.TrimSuffix(data, []byte("\n"))
	return data[bytes.LastIndexByte(data, '\n')+1:]
}

// witnessRuntime is the folder the witness's commands run in, or why they do
// not run: the decision record's own refusal; a project that keeps no trail;
// and a runtime with no audit commands.
func (s *Server) witnessRuntime(ctx context.Context) (heldDir, *lockFailure) {
	return s.handoverRuntime(ctx)
}

// attempt is one submission, in the turn the caller holds: the settings and
// the token read under witnessMu, held for reading until the witness has
// answered, so that once a removal or a change is answered nothing more is
// sent under what it replaced.
func (ws *witnessScheduler) attempt(requested bool) witnessAttempt {
	s := ws.s
	ws.running.Store(true)
	defer ws.running.Store(false)
	at := witnessClock().Unix()
	problem := func(outcome, words string) witnessAttempt {
		attempt := witnessAttempt{At: at, Requested: requested, Outcome: outcome, Problem: words}
		if outcome == witnessNothing && !requested {
			// **A scheduled turn with nothing to send records nothing**: the
			// last submission stays the one shown, and the log is not told
			// each interval that nothing moved.
			return attempt
		}
		return ws.record(attempt)
	}
	dir, failure := s.witnessRuntime(ws.ctx)
	if failure != nil {
		return problem(witnessProblem, failure.message)
	}
	s.witnessMu.RLock()
	defer s.witnessMu.RUnlock()
	s.handoverMu.Lock()
	witness, found, err := s.readWitness()
	s.handoverMu.Unlock()
	switch {
	case err != nil:
		return problem(witnessProblem, fmt.Sprintf(witnessUnreadWords, strings.TrimRight(err.Error(), ".")))
	case !found:
		return problem(witnessNothing, witnessNoneWords)
	}
	token, err := s.readWitnessToken(witness.holder.ID)
	if err != nil {
		return problem(witnessProblem, fmt.Sprintf(witnessUnreadWords, strings.TrimRight(s.custodyWords(err.Error()), ".")))
	}
	head, said, err := s.readCheckpointHead(ws.ctx, dir, deskTrail)
	switch {
	case err != nil:
		return problem(witnessProblem, fmt.Sprintf(witnessNotRunWords, strings.TrimRight(err.Error(), ".")))
	case said != nil:
		return ws.record(witnessAttempt{At: at, Requested: requested, Outcome: witnessProblem, Diagnostics: said, Problem: runtimeRefusalWords(said)})
	case head == nil:
		return problem(witnessNothing, witnessNoChainedWords)
	}
	cursor := witness.record.Witness[head.Identity]
	switch {
	case cursor.Retired:
		return problem(witnessNothing, witnessRetiredWords)
	case head.Sequence <= cursor.BelowHead:
		return problem(witnessNothing, fmt.Sprintf(witnessBelowHeadWords, cursor.BelowHead))
	case head.Sequence <= cursor.Through:
		return problem(witnessNothing, witnessNothingWords)
	}
	from := cursor.Through
	if head.Sequence-from > witnessMaxLines {
		from = head.Sequence - witnessMaxLines
	}
	read, err := s.readCheckpoints(ws.ctx, dir, deskTrail, head.Identity, from, head.Sequence)
	if err == nil && read.through != head.Sequence {
		err = errCheckpointsChanged
	}
	if err != nil {
		return problem(witnessProblem, fmt.Sprintf(witnessNotRunWords, strings.TrimRight(err.Error(), ".")))
	}
	body, first, unsent := witnessBody(read.data)
	sent, _ := readCheckpointLine(lastLine(body))
	// What is left unsent: the lines cut at the witness's bound, and, past
	// the most one submission reads, the records between the cursor and the
	// first line read, by sequence.
	attempt := witnessAttempt{At: at, Requested: requested, Trail: head.Identity, From: cursor.Through, First: first, Through: sent.sequence,
		Lines: int64(bytes.Count(body, []byte("\n"))), Unsent: unsent + (from - cursor.Through)}
	reply := submit(ws.ctx, witness.file.URL, token, body)
	attempt.Status, attempt.Reason, attempt.Error, attempt.RetryAfter = reply.Status, reply.Reason, reply.Error, reply.RetryAfter
	switch {
	case !reply.Reached:
		attempt.Outcome = witnessUnreached
	case reply.Status == http.StatusOK:
		attempt = s.answered(ws.ctx, dir, witness, sent, reply.statements[0], attempt)
	default:
		attempt.Outcome = witnessRefused
		s.refusedWith(witness, head.Identity, reply)
	}
	s.handoverMu.Lock()
	s.keepAnswer(witness.holder.ID, attempt)
	s.handoverMu.Unlock()
	return ws.record(attempt)
}

// refusedWith keeps the statements a refusal carried, and what it says of
// the trail: a below-head's head, after which Desk waits for the trail's
// head to pass it; a retirement, after which it submits nothing more for the
// trail.
func (s *Server) refusedWith(witness witnessHolder, trail string, reply witnessReply) {
	s.witnessReadMu.Lock()
	defer s.witnessReadMu.Unlock()
	s.handoverMu.Lock()
	defer s.handoverMu.Unlock()
	if len(reply.statements) > 0 {
		if err := s.inHolder(witness.holder.ID, func(folder *os.Root) error {
			_, _, err := s.keepStatements(folder, trail, ofTrail(reply.statements, trail))
			return err
		}); err != nil {
			s.log.Printf("desk: the statements the witness refused with could not be kept: %v", err)
		}
	}
	if reply.Status != http.StatusConflict || len(reply.statements) != 1 {
		return
	}
	statement, _ := readWitnessStatement(reply.statements[0][:len(reply.statements[0])-1])
	if statement.checkpoint.trail != trail {
		return
	}
	if err := s.updateCursor(witness.holder.ID, trail, func(entry *witnessCursor) {
		switch reply.Reason {
		case "below-head":
			entry.BelowHead = statement.checkpoint.sequence
		case "retired":
			if statement.kind == "retirement" {
				entry.Retired = true
			}
		}
	}); err != nil {
		s.log.Printf("desk: what the witness said of the trail could not be recorded: %v", err)
	}
}

// ofTrail is the lines among lines whose statement names trail.
func ofTrail(lines [][]byte, trail string) [][]byte {
	var kept [][]byte
	for _, line := range lines {
		if statement, ok := readWitnessStatement(bytes.TrimSuffix(line, []byte("\n"))); ok && statement.checkpoint.trail == trail {
			kept = append(kept, line)
		}
	}
	return kept
}

// inHolder runs act in the holder id's folder. The caller holds handoverMu.
func (s *Server) inHolder(id string, act func(folder *os.Root) error) error {
	handover, err := s.openHandover(false)
	if err != nil {
		return err
	}
	defer handover.Close()
	folder, err := openHolderFolder(handover, id, false)
	if err != nil {
		return err
	}
	defer folder.Close()
	return act(folder)
}

// updateCursor changes the holder's record of trail at the witness by
// change, as Desk read it now, and writes it with the rest of the record as
// read. The caller holds handoverMu.
func (s *Server) updateCursor(id, trail string, change func(entry *witnessCursor)) error {
	return s.inHolder(id, func(folder *os.Root) error {
		record, err := readHandoverRecord(folder)
		if err != nil {
			return err
		}
		entries := maps.Clone(record.Witness)
		if entries == nil {
			entries = map[string]witnessCursor{}
		}
		entry := entries[trail]
		change(&entry)
		if !checkWitnessCursor(trail, entry) {
			return errors.New("the cursor would not be one Desk writes")
		}
		entries[trail] = entry
		record.Witness = entries
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if len(data)+1 > handoverListLimit {
			return fmt.Errorf("Desk's record of this holder would be larger than the %d bytes Desk reads of it", handoverListLimit)
		}
		return writePrivateData(folder, handoverRecordName, append(data, '\n'))
	})
}

// answered is what Desk does with a 200: it keeps the statement, and moves
// the cursor only where the statement is a checkpoint statement for the last
// line sent, by trail, sequence and record digest, and the runtime's reading
// with it as the head confirms it (`confirmStatement`).
func (s *Server) answered(ctx context.Context, dir heldDir, witness witnessHolder, sent checkpointLine, line []byte, attempt witnessAttempt) witnessAttempt {
	statement, _ := readWitnessStatement(line[:len(line)-1])
	index := statement.index
	attempt.Index = &index
	s.witnessReadMu.Lock()
	defer s.witnessReadMu.Unlock()
	s.handoverMu.Lock()
	err := s.inHolder(witness.holder.ID, func(folder *os.Root) error {
		_, _, err := s.keepStatements(folder, sent.trail, ofTrail([][]byte{line}, sent.trail))
		return err
	})
	s.handoverMu.Unlock()
	if err != nil {
		attempt.Outcome, attempt.Problem = witnessKept, fmt.Sprintf(witnessKeepWords, strings.TrimRight(err.Error(), "."))
		return attempt
	}
	if statement.kind != "checkpoint" || statement.checkpoint != sent {
		attempt.Outcome, attempt.Problem = witnessKept, witnessOtherWords
		return attempt
	}
	confirmed, findings, said, problem := s.confirmStatement(ctx, dir, witness, sent.trail, line, statement)
	if !confirmed {
		attempt.Outcome, attempt.Findings, attempt.Diagnostics = witnessKept, findings, said
		attempt.Problem = witnessUnconfirmed
		if problem != "" {
			attempt.Problem = problem
		}
		return attempt
	}
	s.handoverMu.Lock()
	err = s.updateCursor(witness.holder.ID, sent.trail, func(entry *witnessCursor) {
		entry.Through, entry.Index, entry.Digest, entry.At = sent.sequence, statement.index, sent.digest, witnessClock().Unix()
	})
	s.handoverMu.Unlock()
	if err != nil {
		attempt.Outcome, attempt.Problem = witnessKept, fmt.Sprintf(witnessKeepWords, strings.TrimRight(err.Error(), "."))
		return attempt
	}
	attempt.Outcome = witnessCountersigned
	return attempt
}

/* The runtime's reading -------------------------------------------------------- */

// witnessReading is one reading's inputs, prepared in the holder's folder:
// the arguments `audit verify` is given, and what Desk says of them.
type witnessReading struct {
	args []string
	// head is the head the reading is given, or nil for a historical one.
	head *witnessStatement
	// steps is true where more statements are kept than one reading takes.
	steps bool
	// none is true where there is nothing to read: no continuation, no
	// statement kept and no head. The runtime reads a chain from its first
	// statement, so a reading of none is a broken chain, and none is run.
	none bool
	// aside is each file moved aside while the reading was prepared.
	aside []string
	// cleanup removes the private copies the reading was given.
	cleanup func()
}

// prepareReading prepares a reading of trail in the witness holder's
// folder: the statements kept, after the continuation's index, through a
// private copy; the head, from headFile in the folder where it is not empty;
// the continuation, and where the next is saved, unless save is false. The
// caller holds witnessReadMu and handoverMu.
func (s *Server) prepareReading(witness witnessHolder, trail, headFile string, save bool) (witnessReading, error) {
	reading := witnessReading{cleanup: func() {}}
	err := s.inHolder(witness.holder.ID, func(folder *os.Root) error {
		continuation, resumed, aside, err := s.continuationKept(folder, trail)
		if aside != "" {
			reading.aside = append(reading.aside, aside)
		}
		if err != nil {
			return err
		}
		_, kept, aside, err := s.statementsKept(folder, trail)
		if aside != "" {
			reading.aside = append(reading.aside, aside)
		}
		if err != nil {
			return err
		}
		after := int64(-1)
		if resumed {
			after = continuation.last
		}
		var selected []keptStatement
		for _, statement := range kept {
			if statement.index > after {
				selected = append(selected, statement)
			}
		}
		slices.SortStableFunc(selected, func(a, b keptStatement) int { return int(a.index - b.index) })
		var copied []byte
		for i, statement := range selected {
			if i >= witnessStatementBound || len(copied)+len(statement.line) > witnessBytesBound {
				reading.steps = true
				break
			}
			copied = append(copied, statement.line...)
		}
		if len(copied) == 0 && !resumed {
			reading.none = true
			return nil
		}
		if err := writePrivateData(folder, readingName(trail), copied); err != nil {
			return err
		}
		reading.cleanup = func() {
			if err := s.inHolder(witness.holder.ID, func(folder *os.Root) error { return folder.Remove(readingName(trail)) }); err != nil && !errors.Is(err, fs.ErrNotExist) {
				s.log.Printf("desk: the private copy of the witness's statements could not be removed: %v", err)
			}
		}
		reading.args = []string{"--witness-key", witness.holderPath(witnessKeyName), "--witness", witness.holderPath(readingName(trail))}
		if headFile != "" && !reading.steps {
			data, err := readPrivateData(folder, headFile, witnessLineLimit+1)
			if err != nil {
				return err
			}
			head, ok := readWitnessStatement(bytes.TrimSuffix(data, []byte("\n")))
			if !ok || head.checkpoint.trail != trail {
				return errors.New("the head Desk keeps is not a statement of this trail")
			}
			reading.head = &head
			reading.args = append(reading.args, "--witness-head", witness.holderPath(headFile))
		}
		if resumed {
			reading.args = append(reading.args, "--witness-resume", witness.holderPath(continuationName(trail)))
		}
		if save {
			reading.args = append(reading.args, "--witness-save", witness.holderPath(continuationName(trail)))
		}
		return nil
	})
	if err != nil {
		reading.cleanup()
		return witnessReading{}, err
	}
	return reading, nil
}

// afterReading does what a reading's report asks of the files: a chain the
// runtime finds equivocating moves the statements file aside, and a
// continuation the runtime saved is narrowed to the mode Desk keeps its
// files in. It answers the names of the files it moved aside. The caller
// holds witnessReadMu and handoverMu.
func (s *Server) afterReading(witness witnessHolder, trail string, report *auditReport) []string {
	if report == nil || report.Witness == nil {
		return nil
	}
	var aside []string
	err := s.inHolder(witness.holder.ID, func(folder *os.Root) error {
		if report.Witness.ContinuationSaved {
			if err := ownRuntimeFile(folder, continuationName(trail)); err != nil {
				return err
			}
		}
		if slices.ContainsFunc(report.Findings, func(f auditFinding) bool { return f.Name == "witness-equivocation" }) {
			name, err := s.setAside(folder, statementsName(trail), "equivocation")
			if name != "" {
				aside = append(aside, name)
			}
			return err
		}
		return nil
	})
	if err != nil {
		s.log.Printf("desk: the witness's files could not be kept after the reading: %v", err)
	}
	return aside
}

// readingArgs is `audit verify` with the reading's arguments, and nothing
// else held: what a submission's confirmation runs.
func readingArgs(reading witnessReading) []string {
	return append([]string{"audit", "verify", "--config", runtimeConfigName, "--format", "json"}, reading.args...)
}

// confirmStatement is whether the runtime confirms line, the statement the
// witness answered a submission with: the statements between Desk's last and
// it fetched and kept first, then a reading with it as the head that reports
// the witness section `read`, no `witness-*` finding, and the latest
// checkpoint statement at its index and sequence. Where it does not, the
// reading's findings, or the runtime's refusal, or Desk's sentence. The
// caller holds witnessReadMu, and not handoverMu.
func (s *Server) confirmStatement(ctx context.Context, dir heldDir, witness witnessHolder, trail string, line []byte, statement witnessStatement) (bool, []auditFinding, []runtimeDiagnostic, string) {
	s.handoverMu.Lock()
	var highest int64 = -1
	err := s.inHolder(witness.holder.ID, func(folder *os.Root) error {
		continuation, resumed, _, err := s.continuationKept(folder, trail)
		if err != nil {
			return err
		}
		if resumed {
			highest = continuation.last
		}
		_, kept, _, err := s.statementsKept(folder, trail)
		highest = contiguousThrough(kept, highest)
		return err
	})
	s.handoverMu.Unlock()
	if err != nil {
		return false, nil, nil, fmt.Sprintf(witnessReadingWords, strings.TrimRight(err.Error(), "."))
	}
	// The statements between Desk's last and this one.
	if statement.index > highest+1 {
		lines, _ := fetchAfter(ctx, witness.file.URL, trail, highest, statement.index-1)
		s.handoverMu.Lock()
		err = s.inHolder(witness.holder.ID, func(folder *os.Root) error {
			_, _, err := s.keepStatements(folder, trail, lines)
			return err
		})
		s.handoverMu.Unlock()
		if err != nil {
			return false, nil, nil, fmt.Sprintf(witnessReadingWords, strings.TrimRight(err.Error(), "."))
		}
	}
	s.handoverMu.Lock()
	err = s.inHolder(witness.holder.ID, func(folder *os.Root) error { return writePrivateData(folder, submittedName(trail), line) })
	var reading witnessReading
	if err == nil {
		reading, err = s.prepareReading(witness, trail, submittedName(trail), true)
	}
	s.handoverMu.Unlock()
	defer func() {
		s.handoverMu.Lock()
		defer s.handoverMu.Unlock()
		if err := s.inHolder(witness.holder.ID, func(folder *os.Root) error { return folder.Remove(submittedName(trail)) }); err != nil && !errors.Is(err, fs.ErrNotExist) {
			s.log.Printf("desk: the copy of the witness's statement could not be removed: %v", err)
		}
	}()
	if err != nil {
		return false, nil, nil, fmt.Sprintf(witnessReadingWords, strings.TrimRight(err.Error(), "."))
	}
	defer func() {
		s.handoverMu.Lock()
		defer s.handoverMu.Unlock()
		reading.cleanup()
	}()
	if reading.head == nil || reading.none {
		return false, nil, nil, fmt.Sprintf(witnessReadingWords, "more statements are kept than one reading takes")
	}
	out, runErr := runRuntime(ctx, s.cfg.JpackBin, dir, readingArgs(reading)...)
	answer, err := readAuditVerification(out, runErr)
	if err != nil {
		return false, nil, nil, fmt.Sprintf(witnessReadingWords, strings.TrimRight(err.Error(), "."))
	}
	if answer.State != auditStateReport || answer.Report == nil {
		return false, nil, answer.Diagnostics, ""
	}
	report := answer.Report
	s.handoverMu.Lock()
	s.afterReading(witness, trail, report)
	s.handoverMu.Unlock()
	return statementConfirmed(report, statement), report.Findings, nil, ""
}

// contiguousThrough is the highest index from after on that kept holds with
// every index between: after where it holds not the next.
func contiguousThrough(kept []keptStatement, after int64) int64 {
	held := map[int64]bool{}
	for _, statement := range kept {
		held[statement.index] = true
	}
	for held[after+1] {
		after++
	}
	return after
}

// statementConfirmed is whether report, a reading with statement as the
// head, confirms it: the witness section read, no witness-* finding, and the
// latest checkpoint statement at its index and sequence.
func statementConfirmed(report *auditReport, statement witnessStatement) bool {
	w := report.Witness
	if w == nil || w.Status != "read" || w.LatestCheckpoint == nil {
		return false
	}
	if slices.ContainsFunc(report.Findings, func(f auditFinding) bool { return strings.HasPrefix(f.Name, "witness-") }) {
		return false
	}
	return w.LatestCheckpoint.Index == statement.index && w.LatestCheckpoint.Sequence == statement.checkpoint.sequence
}

/* The decision record's reading --------------------------------------------------- */

// What the decision record says of the witness (`auditWitnessHolder.State`).
const (
	witnessStateNone        = "none"
	witnessStateSet         = "set"
	witnessStateUnread      = "unread"
	witnessStateUnavailable = "unavailable"
)

// witnessFetch is the decision record's fetch of the trail's head: State is
// "fetched", "refused" (the witness's reason and sentence, verbatim) or
// "unreached" (Desk could not reach the witness, with the HTTP status where
// there was one); Index is the head's index where it was fetched.
type witnessFetch struct {
	State  string `json:"state"`
	Status int    `json:"status,omitempty"`
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
	Index  *int64 `json:"index,omitempty"`
}

// witnessSetAside is a file Desk moved aside in the witness's folder: its
// name, which says when, and why, in Desk's words.
type witnessSetAside struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// auditWitnessHolder is the decision record's word on the witness, given
// with a report and with the runtime's refusal: the settings; the trail and
// the cursor at the witness; the fetch of the head; whether the reading was
// passed, current or historical, or why not; the last submission since Desk
// started, or the last one kept; the files moved aside; and the token that
// confirms a removal of the witness as shown.
type auditWitnessHolder struct {
	State       string         `json:"state"`
	Settings    *witnessShown  `json:"settings,omitempty"`
	Problem     string         `json:"problem,omitempty"`
	Trail       string         `json:"trail,omitempty"`
	Cursor      *witnessCursor `json:"cursor,omitempty"`
	Head        *witnessFetch  `json:"head,omitempty"`
	Passed      bool           `json:"passed,omitempty"`
	PassProblem string         `json:"passProblem,omitempty"`
	Steps       bool           `json:"steps,omitempty"`
	// NoStatements is true where Desk keeps no statement of the witness for
	// the trail yet, and no continuation: nothing is passed, since a chain is
	// read from its first statement.
	NoStatements bool              `json:"noStatements,omitempty"`
	Require      int64             `json:"require,omitempty"`
	Last         *witnessAttempt   `json:"last,omitempty"`
	Running      bool              `json:"running,omitempty"`
	SetAside     []witnessSetAside `json:"setAside,omitempty"`
	RemoveToken  string            `json:"removeToken,omitempty"`
}

// witnessInputs is what the decision record passes `audit verify` of the
// witness, and what it says; done releases what is held, and removes the
// private copies, once the runtime has read them.
type witnessInputs struct {
	args    []string
	view    *auditWitnessHolder
	witness witnessHolder
	trail   string
	done    func()
}

// witnessForVerify prepares the decision record's reading of the witness's
// chain for the trail as the runtime gives it now: the head fetched with no
// token, the statements after the continuation fetched and kept, and the
// arguments `audit verify` is given; a historical reading where the head
// could not be fetched; and nothing passed where Desk keeps no witness,
// cannot read the one it keeps, or the trail has no chained record. With
// require, the one check the owner asked for, which saves nothing.
func (s *Server) witnessForVerify(ctx context.Context, dir heldDir, require int64) witnessInputs {
	inputs := witnessInputs{done: func() {}}
	s.witnessMu.RLock()
	release := []func(){s.witnessMu.RUnlock}
	inputs.done = func() {
		for i := len(release) - 1; i >= 0; i-- {
			release[i]()
		}
	}
	s.handoverMu.Lock()
	witness, found, err := s.readWitness()
	var raw []byte
	if err != nil {
		raw = s.witnessSettingsBytes()
	}
	s.handoverMu.Unlock()
	switch {
	case err != nil:
		inputs.view = &auditWitnessHolder{State: witnessStateUnread, Problem: fmt.Sprintf(witnessUnreadWords, strings.TrimRight(err.Error(), "."))}
		if raw != nil {
			inputs.view.RemoveToken = s.stampingToken(witnessRemovePurpose, newStampingNonce(), "", currentDigest(raw, true))
		}
		return inputs
	case !found:
		return inputs
	}
	view := &auditWitnessHolder{State: witnessStateSet, Settings: ptr(witness.shown()), Require: require,
		RemoveToken: s.stampingToken(witnessRemovePurpose, newStampingNonce(), "", currentDigest(witness.raw, true))}
	view.Last, view.Running = s.witnessLoop.report()
	if view.Last == nil {
		s.handoverMu.Lock()
		if err := s.inHolder(witness.holder.ID, func(folder *os.Root) error {
			last, err := readAnswer(folder)
			view.Last = last
			return err
		}); err != nil {
			view.PassProblem = fmt.Sprintf(witnessUnreadWords, strings.TrimRight(err.Error(), "."))
		}
		s.handoverMu.Unlock()
	}
	inputs.view, inputs.witness = view, witness
	head, said, err := s.readCheckpointHead(ctx, dir, deskTrail)
	switch {
	case err != nil:
		view.PassProblem = fmt.Sprintf(witnessReadingWords, strings.TrimRight(err.Error(), "."))
		return inputs
	case said != nil:
		view.PassProblem = fmt.Sprintf(witnessReadingWords, strings.TrimRight(runtimeRefusalWords(said), "."))
		return inputs
	case head == nil:
		return inputs
	}
	inputs.trail, view.Trail = head.Identity, head.Identity
	if cursor, ok := witness.record.Witness[head.Identity]; ok {
		view.Cursor = &cursor
	}
	s.witnessReadMu.Lock()
	release = append(release, s.witnessReadMu.Unlock)
	headFile := ""
	fetch, err := s.fetchForReading(ctx, witness, head.Identity)
	view.Head = &fetch
	switch {
	case err != nil:
		// Desk's own failure to keep what it fetched: said in its words,
		// and the reading is of what it keeps, historical.
		view.PassProblem = fmt.Sprintf(witnessReadingWords, strings.TrimRight(err.Error(), "."))
	case fetch.State == "fetched":
		headFile = headName(head.Identity)
	}
	s.handoverMu.Lock()
	reading, err := s.prepareReading(witness, head.Identity, headFile, require == 0)
	s.handoverMu.Unlock()
	if err != nil {
		view.PassProblem = fmt.Sprintf(witnessReadingWords, strings.TrimRight(err.Error(), "."))
		return inputs
	}
	release = append(release, func() {
		s.handoverMu.Lock()
		defer s.handoverMu.Unlock()
		reading.cleanup()
	})
	if reading.none {
		view.NoStatements = true
		return inputs
	}
	view.Steps = reading.steps
	inputs.args = reading.args
	if require > 0 {
		inputs.args = append(inputs.args, "--require-countersigned-through", strconv.FormatInt(require, 10))
	}
	view.Passed = true
	return inputs
}

// ptr is a pointer to v.
func ptr[T any](v T) *T { return &v }

// witnessSettingsBytes is the witness holder's settings file's bytes as they
// are now, whatever they hold, for a removal's token; nil where they cannot
// be read. The caller holds handoverMu.
func (s *Server) witnessSettingsBytes() []byte {
	handover, err := s.openHandover(false)
	if err != nil {
		return nil
	}
	defer handover.Close()
	holders, err := readHolders(handover)
	if err != nil {
		return nil
	}
	holder, found := findWitnessHolder(holders)
	if !found {
		return nil
	}
	var raw []byte
	_ = s.inHolderOf(handover, holder.ID, func(folder *os.Root) error {
		raw, err = readPrivateData(folder, witnessSettingsName, witnessSettingsLimit)
		return err
	})
	return raw
}

// inHolderOf is inHolder through a hand-over folder already open.
func (s *Server) inHolderOf(handover *os.Root, id string, act func(folder *os.Root) error) error {
	folder, err := openHolderFolder(handover, id, false)
	if err != nil {
		return err
	}
	defer folder.Close()
	return act(folder)
}

// fetchForReading fetches the trail's head with no token and, where it was
// fetched, keeps its statement and then replaces the head file with it, and
// fetches and keeps the statements after Desk's continuation through the
// head's index. A fetch that did not reach the witness, a refusal, and a
// page that did not come are each said, and the reading is then historical.
// A failure of Desk's own to keep what was fetched is an error, and the
// reading is then historical. The caller holds witnessReadMu, and not
// handoverMu.
func (s *Server) fetchForReading(ctx context.Context, witness witnessHolder, trail string) (witnessFetch, error) {
	reply := fetchHead(ctx, witness.file.URL, trail)
	fetch := fetchOf(reply)
	if fetch.State != "fetched" {
		return fetch, nil
	}
	line := reply.statements[0]
	statement, _ := readWitnessStatement(line[:len(line)-1])
	if statement.checkpoint.trail != trail {
		// A head of another trail is not an answer of the witness's form
		// to the question asked.
		return witnessFetch{State: "unreached", Status: reply.Status}, nil
	}
	index := statement.index
	fetch.Index = &index
	var after int64 = -1
	s.handoverMu.Lock()
	err := s.inHolder(witness.holder.ID, func(folder *os.Root) error {
		continuation, resumed, _, err := s.continuationKept(folder, trail)
		if resumed {
			after = continuation.last
		}
		return err
	})
	s.handoverMu.Unlock()
	if err != nil {
		return fetch, err
	}
	lines, ended := fetchAfter(ctx, witness.file.URL, trail, after, statement.index-1)
	s.handoverMu.Lock()
	defer s.handoverMu.Unlock()
	err = s.inHolder(witness.holder.ID, func(folder *os.Root) error {
		if _, _, err := s.keepStatements(folder, trail, append(lines, line)); err != nil {
			return err
		}
		if ended != nil {
			return nil
		}
		// **Replaced only once its statement is in the statements file.**
		return writePrivateData(folder, headName(trail), line)
	})
	switch {
	case err != nil:
		return fetch, err
	case ended != nil:
		return fetchOf(*ended), nil
	}
	return fetch, nil
}

// fetchOf is a reply as the decision record says it.
func fetchOf(reply witnessReply) witnessFetch {
	switch {
	case !reply.Reached:
		return witnessFetch{State: "unreached", Status: reply.Status}
	case reply.Status == http.StatusOK:
		return witnessFetch{State: "fetched", Status: reply.Status}
	}
	return witnessFetch{State: "refused", Status: reply.Status, Reason: reply.Reason, Error: reply.Error}
}

// setAsideIn is the files moved aside in the witness holder's folder, by
// name, with Desk's sentence on why. A folder that cannot be listed now is
// said in Desk's log.
func (s *Server) setAsideIn(witness witnessHolder) []witnessSetAside {
	s.handoverMu.Lock()
	defer s.handoverMu.Unlock()
	var listed []witnessSetAside
	err := s.inHolder(witness.holder.ID, func(folder *os.Root) error {
		dir, err := folder.Open(".")
		if err != nil {
			return err
		}
		defer dir.Close()
		names, err := dir.Readdirnames(-1)
		if err != nil {
			return err
		}
		slices.Sort(names)
		for _, name := range names {
			if found := witnessSetAsideForm.FindStringSubmatch(name); found != nil {
				listed = append(listed, witnessSetAside{Name: name, Reason: fmt.Sprintf(witnessSetAsideWords, found[1], witnessSetAsideReasons[found[3]])})
			}
		}
		return nil
	})
	if err != nil {
		s.log.Printf("desk: the witness's folder could not be listed for the files moved aside: %v", err)
	}
	return listed
}

// witnessAfterVerify completes what the decision record says of the
// witness once the runtime has answered, and does what the report asks of
// the files.
func (s *Server) witnessAfterVerify(inputs witnessInputs, report *auditReport) {
	if inputs.view == nil || inputs.view.State != witnessStateSet {
		return
	}
	if len(inputs.args) > 0 {
		s.handoverMu.Lock()
		s.afterReading(inputs.witness, inputs.trail, report)
		s.handoverMu.Unlock()
	}
	inputs.view.SetAside = s.setAsideIn(inputs.witness)
}

// witnessAbsent is whether answer is the runtime's own refusal of the
// witness's flags: a runtime that reads no witness's statements.
func witnessAbsent(answer auditAnswer) bool {
	return answer.State == auditStateUnverified && len(answer.Diagnostics) == 1 && answer.Diagnostics[0].Code == "JPS-INVOCATION-ARGUMENTS" &&
		strings.HasPrefix(answer.Diagnostics[0].Message, "unknown flag: --witness")
}

/* The key, held to the runtime's rule ----------------------------------------------- */

// checkWitnessKey holds key to the runtime's public-key rule: Desk writes it
// to a private file in the hand-over folder and runs `audit verify --config
// jpack.json --format json --witness-key <file>` once, and reads only its
// refusal. JPS-AUDIT-WITNESS-KEY-INVALID is the runtime's refusal of the key,
// in its words; an answer of the command past the key's reading is its
// acceptance. Anything else is not a check of the key, and nothing is kept.
func (s *Server) checkWitnessKey(ctx context.Context, dir heldDir, key string) *lockFailure {
	s.handoverMu.Lock()
	handover, err := s.openHandover(true)
	if err != nil {
		s.handoverMu.Unlock()
		return &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("Desk's record of hand-overs could not be opened: ", err)}
	}
	defer handover.Close()
	name := ".witness-key-" + newStampingNonce() + ".pub"
	err = writePrivateData(handover, name, []byte(key+"\n"))
	s.handoverMu.Unlock()
	defer func() {
		s.handoverMu.Lock()
		defer s.handoverMu.Unlock()
		if err := handover.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			s.log.Printf("desk: the key written for its check could not be removed: %v", err)
		}
	}()
	if err != nil {
		return &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("The key could not be written for its check: ", err)}
	}
	out, runErr := runRuntime(ctx, s.cfg.JpackBin, dir, "audit", "verify", "--config", runtimeConfigName, "--format", "json", "--witness-key", path.Join(handoverDir, name))
	return keyCheckAnswer(out, runErr)
}

// keyCheckAnswer reads the runtime's answer to the key's check.
func keyCheckAnswer(out []byte, runErr error) *lockFailure {
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			return &lockFailure{http.StatusInternalServerError, CodeInternal, fmt.Sprintf(witnessNotRunWords, strings.TrimRight(runErr.Error(), "."))}
		}
	}
	var got struct {
		Command     string                   `json:"command"`
		Status      string                   `json:"status"`
		Tool        struct{ Version string } `json:"tool"`
		Diagnostics []runtimeDiagnostic      `json:"diagnostics"`
	}
	if out == nil || json.Unmarshal(out, &got) != nil {
		return &lockFailure{http.StatusInternalServerError, CodeInternal, witnessUndocumentedKey}
	}
	for _, diagnostic := range got.Diagnostics {
		switch {
		case diagnostic.Code == "JPS-AUDIT-WITNESS-KEY-INVALID":
			return &lockFailure{http.StatusBadRequest, CodeBadRequest, strings.TrimSpace(diagnostic.Message)}
		case diagnostic.Code == "JPS-AUDIT-WITNESS-KEY-READ":
			return &lockFailure{http.StatusInternalServerError, CodeInternal, witnessKeyReadWords}
		case diagnostic.Code == "JPS-INVOCATION-ARGUMENTS" && strings.HasPrefix(diagnostic.Message, "unknown flag: --witness"):
			return &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(witnessKeyOlderWords, got.Tool.Version, witnessRuntimeFloor)}
		case strings.HasPrefix(diagnostic.Code, "JPS-INVOCATION"):
			return &lockFailure{http.StatusInternalServerError, CodeInternal, witnessUndocumentedKey}
		}
	}
	if got.Command != auditVerifyCommand {
		return &lockFailure{http.StatusInternalServerError, CodeInternal, witnessUndocumentedKey}
	}
	return nil
}

// refuseLocalKey refuses key where it is the public key of Desk's own local
// gateway, the operator's (ADR-0010, section 2). A marker Desk cannot read
// now refuses too: it cannot tell.
func (s *Server) refuseLocalKey(key string) *lockFailure {
	data, err := readPrivateData(s.assistant.root, "local-gateway-identity.json", 256)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	var signer localSigner
	if err == nil && json.Unmarshal(data, &signer) != nil {
		err = errors.New("its record of the key is not one Desk wrote")
	}
	if err != nil {
		return &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(witnessLocalReadWords, strings.TrimRight(s.custodyWords(err.Error()), "."))}
	}
	if strings.EqualFold(signer.Public, key) {
		return &lockFailure{http.StatusBadRequest, CodeBadRequest, witnessOwnKeyWords}
	}
	return nil
}

/* The routes -------------------------------------------------------------------- */

// witnessChecked is what a check answers: what Desk would keep, the trail
// identity the witness's operator registers, where the trail has one, and
// the token that confirms it.
type witnessChecked struct {
	Token string       `json:"token"`
	Shown witnessShown `json:"shown"`
	Trail string       `json:"trail,omitempty"`
}

// refuseWitness writes failure, its message passed through withoutPaths:
// the one way a message of Desk's or the runtime's reaches the page from
// these routes. The log keeps it whole.
func (s *Server) refuseWitness(w http.ResponseWriter, failure *lockFailure) {
	message := s.withoutPaths(failure.message)
	if message != failure.message || failure.status == http.StatusInternalServerError {
		s.log.Printf("desk: the witness of desk %s, as said: %s", s.signingKeyName(), failure.message)
	}
	writeJSONCoded(w, failure.status, failure.code, message)
}

// witnessRequest reads a request's JSON body, as a stamping request is read.
func witnessRequest(w http.ResponseWriter, r *http.Request, into any, refused string) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSONCoded(w, http.StatusForbidden, CodeForbidden, "A cross-site request cannot change this desk's witness.")
		return false
	}
	return stampingRequest(w, r, witnessRequestLimit, into, refused)
}

// currentWitness is the witness holder's settings bytes as they are now, for
// a token over them, and the key they pin: none where Desk keeps no witness.
// The caller holds witnessMu.
func (s *Server) currentWitness() (raw []byte, key string, found bool, err error) {
	s.handoverMu.Lock()
	defer s.handoverMu.Unlock()
	witness, found, err := s.readWitness()
	if err != nil || !found {
		return nil, "", found, err
	}
	return witness.raw, witness.file.PublicKey, true, nil
}

// checkProposal holds a proposal to every rule Desk keeps a witness by, the
// runtime's key rule among them: the plan, or the failure that says why not.
func (s *Server) checkProposal(ctx context.Context, proposal witnessProposal) (witnessPlan, heldDir, *lockFailure) {
	plan, refused := proposal.plan()
	if refused != "" {
		return witnessPlan{}, heldDir{}, &lockFailure{http.StatusBadRequest, CodeBadRequest, refused}
	}
	dir, failure := s.witnessRuntime(ctx)
	if failure != nil {
		return witnessPlan{}, heldDir{}, failure
	}
	if !s.assistant.usable() {
		return witnessPlan{}, heldDir{}, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(witnessCustodyWords, strings.TrimRight(s.custodyWords(s.assistant.problem.Error()), "."))}
	}
	if failure := s.checkWitnessKey(ctx, dir, plan.key); failure != nil {
		return witnessPlan{}, heldDir{}, failure
	}
	if !publicKeyForm.MatchString(plan.pinned()) {
		return witnessPlan{}, heldDir{}, &lockFailure{http.StatusInternalServerError, CodeInternal, witnessUndocumentedKey}
	}
	if failure := s.refuseLocalKey(plan.pinned()); failure != nil {
		return witnessPlan{}, heldDir{}, failure
	}
	return plan, dir, nil
}

// handleWitnessCheck answers `POST /api/audit/witness/check`: the owner's
// proposal held to every rule, and, where it holds, what Desk would keep, the
// trail identity the witness's operator registers, and a token over it and
// over the witness as it is now. It changes nothing.
func (s *Server) handleWitnessCheck(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var proposal witnessProposal
	if !witnessRequest(w, r, &proposal, witnessRequestWords) {
		return
	}
	plan, dir, failure := s.checkProposal(r.Context(), proposal)
	if failure != nil {
		s.refuseWitness(w, failure)
		return
	}
	s.witnessMu.RLock()
	raw, key, found, err := s.currentWitness()
	s.witnessMu.RUnlock()
	switch {
	case err != nil:
		s.refuseWitness(w, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(witnessUnreadWords, strings.TrimRight(err.Error(), "."))})
		return
	case found && key != plan.pinned():
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, witnessNewKeyWords)
		return
	}
	checked := witnessChecked{Token: s.stampingToken(witnessSetPurpose, newStampingNonce(), plan.digest(), currentDigest(raw, found)),
		Shown: witnessShown{URL: plan.url, PublicKey: plan.pinned(), IntervalMinutes: plan.interval}}
	if head, _, err := s.readCheckpointHead(r.Context(), dir, deskTrail); err == nil && head != nil {
		checked.Trail = head.Identity
	}
	writeJSON(w, http.StatusOK, checked)
}

// handleWitnessSet answers `POST /api/audit/witness`: keep the witness the
// token confirms, with the submitter's token pasted once, or change
// nothing. The token must be the one a check gave for the same proposal,
// against the witness as it is now, and is spent before anything is
// written, whatever follows.
func (s *Server) handleWitnessSet(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request struct {
		witnessProposal
		SubmitterToken string `json:"submitterToken"`
		Token          string `json:"token"`
	}
	if !witnessRequest(w, r, &request, witnessRequestWords) {
		return
	}
	if len(request.Token) != stampingTokenLength {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, witnessConfirmWords)
		return
	}
	submitter := strings.TrimSpace(request.SubmitterToken)
	request.SubmitterToken = ""
	if !witnessTokenForm.MatchString(submitter) {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, witnessTokenWords)
		return
	}
	plan, refused := request.plan()
	if refused != "" {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, refused)
		return
	}
	if _, failure := s.witnessRuntime(r.Context()); failure != nil {
		s.refuseWitness(w, failure)
		return
	}
	shown, failure := func() (*witnessShown, *lockFailure) {
		s.witnessMu.Lock()
		defer s.witnessMu.Unlock()
		return s.setWitness(plan, submitter, request.Token)
	}()
	if failure != nil {
		s.refuseWitness(w, failure)
		return
	}
	s.witnessLoop.settingsChanged()
	s.log.Printf("desk: a witness was set for desk %s on the owner's confirmation", s.signingKeyName())
	writeJSON(w, http.StatusOK, auditWitnessHolder{State: witnessStateSet, Settings: shown})
}

// spendWitnessNonce is whether token's nonce was not spent, and spends it.
// The caller holds witnessMu for writing.
func (s *Server) spendWitnessNonce(token string) bool {
	nonce := token[:stampingNonceLength]
	if s.witnessNonces[nonce] {
		return false
	}
	if s.witnessNonces == nil {
		s.witnessNonces = map[string]bool{}
	}
	s.witnessNonces[nonce] = true
	return true
}

// setWitness keeps plan, with the submitter's token, where token confirms it
// against the witness as it is now: under the same key, the same holder, its
// token and settings replaced; otherwise a new holder, its token first, then
// its key and settings, and the list of holders last, so a holder is listed
// only once all of it is kept. The caller holds witnessMu for writing.
func (s *Server) setWitness(plan witnessPlan, submitter, token string) (*witnessShown, *lockFailure) {
	nonce := token[:stampingNonceLength]
	if s.witnessNonces[nonce] {
		return nil, &lockFailure{http.StatusConflict, CodeStale, witnessUsedWords}
	}
	raw, key, found, err := s.currentWitness()
	if err != nil {
		return nil, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(witnessUnreadWords, strings.TrimRight(err.Error(), "."))}
	}
	if !hmac.Equal([]byte(s.stampingToken(witnessSetPurpose, nonce, plan.digest(), currentDigest(raw, found))), []byte(token)) {
		return nil, &lockFailure{http.StatusConflict, CodeStale, witnessStaleWords}
	}
	if found && key != plan.pinned() {
		return nil, &lockFailure{http.StatusConflict, CodeBadRequest, witnessNewKeyWords}
	}
	s.spendWitnessNonce(token)
	keepFailed := func(err error) (*witnessShown, *lockFailure) {
		return nil, &lockFailure{http.StatusInternalServerError, CodeInternal, fmt.Sprintf(witnessKeepWords, strings.TrimRight(s.custodyWords(err.Error()), "."))}
	}
	s.handoverMu.Lock()
	defer s.handoverMu.Unlock()
	handover, err := s.openHandover(true)
	if err != nil {
		return keepFailed(err)
	}
	defer handover.Close()
	holders, err := readHolders(handover)
	if err != nil {
		return keepFailed(err)
	}
	holder, listed := findWitnessHolder(holders)
	if !listed {
		if len(holders) >= maxHolders {
			return nil, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf("Desk keeps at most %d holders for one desk, so it adds no more.", maxHolders)}
		}
		id, err := newHolderID()
		if err == nil && (!holderIDForm.MatchString(id) || slices.ContainsFunc(holders, func(h handoverHolder) bool { return h.ID == id })) {
			err = errors.New("no new holder id could be made")
		}
		if err != nil {
			return keepFailed(err)
		}
		label := "Witness"
		if parsed, err := url.Parse(plan.url); err == nil && parsed.Host != "" {
			if text, ok := holderText("Witness at "+parsed.Host, holderLabelLimit); ok {
				label = text
			}
		}
		holder = handoverHolder{ID: id, Label: label, Channel: witnessChannel, AddedAt: witnessClock().Unix(), Witness: true}
	}
	tokens, err := s.openWitnessTokens(true)
	if err != nil {
		return keepFailed(err)
	}
	defer tokens.Close()
	if err := writePrivateData(tokens, witnessTokenName(holder.ID), []byte(submitter)); err != nil {
		return keepFailed(errors.New("the token could not be kept in Desk's custody"))
	}
	kept := false
	defer func() {
		// **A new witness not kept whole leaves no token behind**: no listed
		// holder names it, so nothing would ever remove it.
		if !kept && !listed {
			if err := tokens.Remove(witnessTokenName(holder.ID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				s.log.Printf("desk: the token of a witness not kept could not be removed from Desk's custody: %v", err)
			}
		}
	}()
	folder, err := openHolderFolder(handover, holder.ID, true)
	if err != nil {
		return keepFailed(err)
	}
	defer folder.Close()
	file := witnessFile{Version: witnessVersion, URL: plan.url, PublicKey: plan.pinned(), IntervalMinutes: plan.interval, SetAt: witnessClock().Unix()}
	data, err := json.Marshal(file)
	if err == nil && len(data)+1 > witnessSettingsLimit {
		err = fmt.Errorf("these settings would be larger than the %d bytes Desk reads of them", witnessSettingsLimit)
	}
	if err == nil && !listed {
		err = writePrivateData(folder, witnessKeyName, []byte(file.PublicKey+"\n"))
	}
	if err == nil {
		err = writePrivateData(folder, witnessSettingsName, append(data, '\n'))
	}
	if err == nil && !listed {
		next, marshalErr := json.Marshal(holdersFile{Version: handoverVersion, Holders: append(holders, holder)})
		if err = marshalErr; err == nil && len(next)+1 > handoverListLimit {
			err = fmt.Errorf(holdersFullWords, handoverListLimit)
		}
		if err == nil {
			err = writePrivateData(handover, handoverHoldersName, append(next, '\n'))
		}
	}
	if err != nil {
		return keepFailed(err)
	}
	kept = true
	shown := witnessShown{URL: file.URL, PublicKey: file.PublicKey, IntervalMinutes: file.IntervalMinutes, SetAt: file.SetAt}
	return &shown, nil
}

// handleWitnessRemove answers `POST /api/audit/witness/remove`: remove this
// desk's witness as the decision record the token names showed it, or change
// nothing. Its token and its settings are removed and it leaves the list of
// holders; its statements, head, continuation, key and record are kept as
// they are.
func (s *Server) handleWitnessRemove(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request struct {
		Token string `json:"token"`
	}
	if !witnessRequest(w, r, &request, witnessRemoveWords) {
		return
	}
	if len(request.Token) != stampingTokenLength {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, witnessRemoveWords)
		return
	}
	if _, refusal := s.auditRuntime(); refusal != "" {
		s.refuseWitness(w, &lockFailure{http.StatusConflict, CodeBadRequest, refusal})
		return
	}
	failure := func() *lockFailure {
		s.witnessMu.Lock()
		defer s.witnessMu.Unlock()
		return s.removeWitness(request.Token)
	}()
	if failure != nil {
		s.refuseWitness(w, failure)
		return
	}
	s.witnessLoop.settingsChanged()
	s.log.Printf("desk: the witness of desk %s was removed on the owner's confirmation; its statements are kept", s.signingKeyName())
	writeJSON(w, http.StatusOK, auditWitnessHolder{State: witnessStateNone})
}

// removeWitness removes the witness token confirms, as it is now: the
// token first, so nothing more is sent, then the settings, then the holder
// from the list. The caller holds witnessMu for writing.
func (s *Server) removeWitness(token string) *lockFailure {
	nonce := token[:stampingNonceLength]
	if s.witnessNonces[nonce] {
		return &lockFailure{http.StatusConflict, CodeStale, witnessUsedWords}
	}
	s.handoverMu.Lock()
	defer s.handoverMu.Unlock()
	handover, err := s.openHandover(false)
	if errors.Is(err, fs.ErrNotExist) {
		return &lockFailure{http.StatusConflict, CodeStale, witnessStaleWords}
	}
	if err != nil {
		return &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(witnessUnreadWords, strings.TrimRight(err.Error(), "."))}
	}
	defer handover.Close()
	holders, err := readHolders(handover)
	if err != nil {
		return &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(witnessUnreadWords, strings.TrimRight(err.Error(), "."))}
	}
	holder, found := findWitnessHolder(holders)
	if !found {
		return &lockFailure{http.StatusConflict, CodeStale, witnessStaleWords}
	}
	var raw []byte
	err = s.inHolderOf(handover, holder.ID, func(folder *os.Root) error {
		raw, err = readPrivateData(folder, witnessSettingsName, witnessSettingsLimit)
		return err
	})
	switch {
	case err != nil:
		return &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(witnessUnreadWords, strings.TrimRight(err.Error(), "."))}
	case !hmac.Equal([]byte(s.stampingToken(witnessRemovePurpose, nonce, "", currentDigest(raw, true))), []byte(token)):
		return &lockFailure{http.StatusConflict, CodeStale, witnessStaleWords}
	}
	s.spendWitnessNonce(token)
	removeFailed := func(err error) *lockFailure {
		return &lockFailure{http.StatusInternalServerError, CodeInternal, fmt.Sprintf(witnessKeepWords, strings.TrimRight(s.custodyWords(err.Error()), "."))}
	}
	// The token first, so that nothing more is sent under it; then the list
	// of holders, so that a witness is no longer read; then its settings.
	if tokens, err := s.openWitnessTokens(false); err == nil {
		err = tokens.Remove(witnessTokenName(holder.ID))
		if err == nil {
			err = syncPrivateDirectory(tokens)
		}
		tokens.Close()
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return removeFailed(errors.New("the token could not be removed from Desk's custody"))
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return removeFailed(err)
	}
	rest := slices.DeleteFunc(slices.Clone(holders), func(h handoverHolder) bool { return h.ID == holder.ID })
	data, err := json.Marshal(holdersFile{Version: handoverVersion, Holders: rest})
	if err == nil {
		err = writePrivateData(handover, handoverHoldersName, append(data, '\n'))
	}
	if err != nil {
		return removeFailed(err)
	}
	if err := s.inHolderOf(handover, holder.ID, func(folder *os.Root) error { return folder.Remove(witnessSettingsName) }); err != nil {
		// No longer listed, so no longer read: what is left is said in
		// Desk's log, and kept.
		s.log.Printf("desk: the removed witness's settings could not be removed from its folder: %v", err)
	}
	return nil
}

// handleWitnessSend answers `POST /api/audit/witness/send`: one submission
// on the owner's request, in the desk's one turn; or why none ran. The page
// then checks the decision record again.
func (s *Server) handleWitnessSend(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request struct{}
	if !witnessRequest(w, r, &request, witnessStartWords) {
		return
	}
	if _, failure := s.witnessRuntime(r.Context()); failure != nil {
		s.refuseWitness(w, failure)
		return
	}
	s.witnessMu.RLock()
	_, _, found, err := s.currentWitness()
	s.witnessMu.RUnlock()
	switch {
	case err != nil:
		s.refuseWitness(w, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(witnessUnreadWords, strings.TrimRight(err.Error(), "."))})
		return
	case !found:
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, witnessNoneWords)
		return
	}
	ws := s.witnessLoop
	if !ws.turn.TryLock() {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, witnessBusyWords)
		return
	}
	attempt, failure := func() (witnessAttempt, *lockFailure) {
		defer ws.turn.Unlock()
		ws.mu.Lock()
		closed, notBefore := ws.closed, ws.notBefore
		now := witnessClock()
		if !closed && !now.Before(notBefore) {
			ws.lastAttempt = now
		}
		ws.mu.Unlock()
		switch {
		case closed:
			return witnessAttempt{}, &lockFailure{http.StatusConflict, CodeBadRequest, witnessClosedWords}
		case now.Before(notBefore):
			return witnessAttempt{}, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(witnessRetryWords, notBefore.UTC().Format(time.RFC3339))}
		}
		return ws.attempt(true), nil
	}()
	if failure != nil {
		s.refuseWitness(w, failure)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Attempt witnessAttempt `json:"attempt"`
	}{s.withoutPathsInAttempt(attempt)})
}

// withoutPathsInAttempt is attempt with Desk's and the runtime's sentences
// passed through withoutPaths. The witness's own words are its own, and
// passed on as it gave them.
func (s *Server) withoutPathsInAttempt(attempt witnessAttempt) witnessAttempt {
	attempt.Problem = s.withoutPaths(attempt.Problem)
	if attempt.Diagnostics != nil {
		said := make([]runtimeDiagnostic, len(attempt.Diagnostics))
		for i, diagnostic := range attempt.Diagnostics {
			said[i] = runtimeDiagnostic{Code: diagnostic.Code, Message: s.withoutPaths(diagnostic.Message)}
		}
		attempt.Diagnostics = said
	}
	if attempt.Findings != nil {
		findings := slices.Clone(attempt.Findings)
		for i := range findings {
			findings[i].Detail = s.withoutPaths(findings[i].Detail)
		}
		attempt.Findings = findings
	}
	return attempt
}

// requireThrough reads the one check the owner asks for,
// `countersignedThrough=<record>`, from a query: 0 where none is asked, and
// false for anything else.
func requireThrough(raw string) (int64, bool) {
	if raw == "" {
		return 0, true
	}
	values, err := url.ParseQuery(raw)
	if err != nil || len(values) != 1 || len(values["countersignedThrough"]) != 1 {
		return 0, false
	}
	var through int64
	if !sidecarInteger(json.RawMessage(values["countersignedThrough"][0]), &through) {
		return 0, false
	}
	return through, true
}
