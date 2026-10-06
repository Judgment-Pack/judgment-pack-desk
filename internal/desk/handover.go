package desk

// Checkpoint hand-over by download or copy (ADR-0010, section 2, and the
// maintainer's answer to question 4; delivery row 5).
//
// # What is handed over
//
// The checkpoints `jpack audit checkpoint --config jpack.json --since <cursor>
// --limit 300` prints in its human form: one line each, in the runtime's RFC
// 8785 canonical form, ended by a newline. Desk asks again after the last
// sequence it received until fewer than 300 come back, and joins the batches
// as they were printed. It parses each line only to check it, and serves the
// bytes untouched: never decoded and encoded again, never reordered, with no
// newline added or removed. The runtime's notes on standard error are not
// part of them (`runRuntime` reads standard output alone).
//
//   - **Through the last chained record read first.** One call of the JSON
//     form, `audit checkpoint --config jpack.json --format json`, gives the
//     trail's identity and the sequence of its last chained record. The lines
//     handed over end with that record's: a line after it, of a record written
//     since, is left for the next hand-over. The bytes cut there end at a
//     line's newline, so every byte served is one the runtime printed, in its
//     place.
//   - **Each line is checked before anything is served**: one checkpoint
//     document of exactly its four members, of the trail identity read first,
//     and after the last line received. A chained record's sequence is its
//     line's number, and a line that is not chained has none, so the next
//     expected checkpoint is the next one after the last, not the last plus
//     one. A line that is not that, a batch the runtime fails, or a trail that
//     ends before the record read first, serves nothing.
//   - **Bounded**: at most 20 batches, 6,000 checkpoints, in one answer. Where
//     more remain, the answer says so, and the next hand-over starts after the
//     last one confirmed. A line is about 170 bytes, so a batch of 300 stays
//     within `runRuntime`'s 64 KiB.
//
// # The deliverer's cursor, and Desk's record
//
// Desk keeps, under the project, in `.desk-private/handover/`, owner-only and
// opened through the project's root, never through a link:
//
//   - `holders.json`: each holder the owner added, by a label and a channel of
//     the owner's own words;
//   - `<holder id>/record.json`: for each trail identity, the last sequence
//     confirmed as handed over to that holder, when (Desk's clock), and the
//     SHA-256 of the bytes confirmed;
//   - `<holder id>/<trail identity>.jsonl`: every line confirmed as handed
//     over to that holder for that trail, as the runtime printed it, appended
//     to only on a confirmation. It is what `audit verify --expect` is given
//     (audit_record.go).
//
// A download moves nothing. The cursor moves only when the owner confirms
// that the file went to the holder: the confirmation names the trail, the
// cursor it started after, the record it ended at and the SHA-256 of its
// bytes, and Desk asks the runtime for the same checkpoints again. Only the
// same bytes, from the holder's cursor as it stands, are recorded. A
// checkpoint is a function of its record's bytes, so the same request gives
// the same bytes until the trail is rewritten or moved aside.
//
// Where the checkpoints come back for another trail identity, the trail was
// moved aside: Desk starts that holder at 0 for the new trail, keeps what it
// recorded for the old one, and says so.
//
// # What Desk's record establishes
//
// Nothing, to a holder or to anyone else (ADR-0010, section 7): it is the
// operator's, and the operator can change it. Only the holder's own copy
// counts. Desk keeps it to know what to hand over next, and to hold the trail
// to it with `audit verify --expect`, which shows the operator what a holder
// would see.
//
// One Desk process serializes every change to these files (`handoverMu`). Two
// Desk processes on one project are not guarded against.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// handoverDir is where Desk keeps its record of hand-overs, under the
	// project. Desk's file API refuses everything under `.desk-private`.
	handoverDir = ".desk-private/handover"
	// handoverHoldersName and handoverRecordName are the list of holders, in
	// handoverDir, and each holder's record, in the holder's own folder.
	handoverHoldersName = "holders.json"
	handoverRecordName  = "record.json"
	// handoverVersion is the version of both files.
	handoverVersion = "1"
	// handoverBatch is the `--limit` of each `audit checkpoint --since`:
	// at about 170 bytes a line, within runtimeAnswerLimit (ADR-0010,
	// section 2).
	handoverBatch = 300
	// handoverBatches bounds the batches one answer joins.
	handoverBatches = 20
	// holderLabelLimit and holderChannelLimit bound a holder's label and
	// channel, in characters.
	holderLabelLimit   = 120
	holderChannelLimit = 200
	// maxHolders bounds the holders one desk keeps: each is an `--expect`
	// on every check of the decision record.
	maxHolders = 50
	// handoverListLimit bounds `holders.json` and a holder's `record.json`.
	handoverListLimit = 64 << 10
	// handoverHeldLimit bounds a holder's file of checkpoints: the most the
	// runtime reads for `--expect` (runtime 0.27.1, `audit.MaxHeldBytes`).
	handoverHeldLimit = 16 << 20
	// handoverRequestLimit bounds a request's body.
	handoverRequestLimit = 4 << 10
	// auditCheckpointCommand is the command's name as its JSON answer gives
	// it.
	auditCheckpointCommand = "audit checkpoint"
)

// The headers a download of checkpoints answers with, beside the bytes.
const (
	checkpointsTrailHeader   = "Desk-Checkpoints-Trail"
	checkpointsFromHeader    = "Desk-Checkpoints-From"
	checkpointsThroughHeader = "Desk-Checkpoints-Through"
	checkpointsDigestHeader  = "Desk-Checkpoints-Digest"
	checkpointsMoreHeader    = "Desk-Checkpoints-More"
)

// The sentences the hand-over says in its own words. The page lists them,
// so that it can show each in the owner's language.
const (
	holderTextWords   = "A holder's label is 1 to 120 characters and its channel 1 to 200, each with no control characters."
	noSuchHolderWords = "Desk keeps no holder by that id."
	noChainedWords    = "The trail has no chained record yet, so there is nothing to hand over."
	staleWords        = "What you downloaded is not what the trail gives now. Download it again and hand over that file."
	noTrailWords      = "This project keeps no trail: its jpack.json declares no audit directory, so the runtime records none of its deciding runs."
)

// holderIDForm is a holder's id: 16 lowercase hexadecimal characters.
var holderIDForm = regexp.MustCompile(`^[0-9a-f]{16}$`)

// newHolderID is a new holder's id. A variable only so a test can fix it.
var newHolderID = func() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// handoverClock is Desk's clock for a confirmation. A variable only so a test
// can fix it.
var handoverClock = time.Now

// testHookConfirmRead runs after a confirmation has read the holder's record,
// and is nil outside tests. It lets a test send a second confirmation in that
// moment.
var testHookConfirmRead func()

// errCheckpointsChanged is a trail whose checkpoints are not the ones asked
// for: of another identity, or ending before the record asked for.
var errCheckpointsChanged = errors.New("the trail changed while Desk read its checkpoints")

/* What Desk keeps ----------------------------------------------------------- */

// handoverHolder is one holder, as `holders.json` keeps it: the owner's label
// and channel, and when it was added, by Desk's clock.
type handoverHolder struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Channel string `json:"channel"`
	AddedAt int64  `json:"addedAt"`
}

type holdersFile struct {
	Version string           `json:"version"`
	Holders []handoverHolder `json:"holders"`
}

// handedOver is what Desk recorded of one trail handed over to one holder:
// the last sequence confirmed, when, and the SHA-256 of the bytes last
// confirmed.
type handedOver struct {
	Through     int64  `json:"through"`
	ConfirmedAt int64  `json:"confirmedAt"`
	Digest      string `json:"digest"`
}

type handoverRecord struct {
	Version string                `json:"version"`
	Trails  map[string]handedOver `json:"trails"`
}

// holderText is a holder's label or channel as Desk keeps it: trimmed, and
// 1 to limit characters, none of them a control, format, line or paragraph
// separator character (what the runtime does not print either, `displayedPath`).
func holderText(value string, limit int) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return "", false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return "", false
		}
	}
	return value, true
}

// openOwnFolder opens parts, one directory under another, from start: each a
// directory, not a link, owned by this user and open to no one else, and the
// directory that was looked at. With create, a missing one is made
// owner-only; made says whether the last one was. The root it answers is the
// caller's to close; start is never closed.
func openOwnFolder(start *os.Root, parts []string, create bool) (root *os.Root, made bool, err error) {
	current := start
	var opened *os.Root
	defer func() {
		if err != nil && opened != nil {
			opened.Close()
		}
	}()
	for _, part := range parts {
		made = false
		info, lookErr := current.Lstat(part)
		if errors.Is(lookErr, fs.ErrNotExist) && create {
			if lookErr = current.Mkdir(part, custodyDirMode); lookErr == nil {
				made = true
			} else if errors.Is(lookErr, fs.ErrExist) {
				lookErr = nil
			}
			if lookErr == nil {
				info, lookErr = current.Lstat(part)
			}
		}
		if lookErr != nil {
			return nil, false, lookErr
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
			return nil, false, fmt.Errorf("%s is not a folder Desk keeps its record of hand-overs in", part)
		}
		if info.Mode().Perm()&0o077 != 0 {
			return nil, false, fmt.Errorf("%s is open to other users (%v), so Desk does not keep its record of hand-overs in it", part, info.Mode().Perm())
		}
		if err := ownedByUs(part, info); err != nil {
			return nil, false, err
		}
		next, openErr := current.OpenRoot(part)
		if openErr != nil {
			return nil, false, openErr
		}
		if held, statErr := next.Stat("."); statErr != nil || !os.SameFile(info, held) {
			next.Close()
			return nil, false, fmt.Errorf("%s changed while it was being opened", part)
		}
		if opened != nil {
			opened.Close()
		}
		opened, current = next, next
	}
	return opened, made, nil
}

// openHandover opens `.desk-private/handover` through the project's root.
// With create, a missing folder is made owner-only, and a new hand-over
// folder ignores itself in Git: in a project Desk did not make,
// `.desk-private/` may not be ignored yet, and what it holds names the
// owner's counterparties.
func (s *Server) openHandover(create bool) (*os.Root, error) {
	root, made, err := openOwnFolder(s.root, strings.Split(handoverDir, "/"), create)
	if err != nil {
		return nil, err
	}
	if made {
		if err := writePrivateData(root, ".gitignore", []byte("*\n")); err != nil {
			root.Close()
			return nil, err
		}
	}
	return root, nil
}

// openHolderFolder opens a holder's own folder in the hand-over folder.
func openHolderFolder(handover *os.Root, id string, create bool) (*os.Root, error) {
	if !holderIDForm.MatchString(id) {
		return nil, errors.New("not a holder's id")
	}
	root, _, err := openOwnFolder(handover, []string{id}, create)
	return root, err
}

// readHolders is the holders Desk keeps, in the order they were added: none
// where it keeps no list. A list that is not one Desk writes is an error.
func readHolders(handover *os.Root) ([]handoverHolder, error) {
	data, err := readPrivateData(handover, handoverHoldersName, handoverListLimit)
	if errors.Is(err, fs.ErrNotExist) {
		return []handoverHolder{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file holdersFile
	if err := decodeDataJSON(data, &file); err != nil {
		return nil, fmt.Errorf("%s is not a list of holders Desk wrote: %w", handoverHoldersName, err)
	}
	if file.Version != handoverVersion || file.Holders == nil || len(file.Holders) > maxHolders {
		return nil, fmt.Errorf("%s is not a list of holders Desk wrote", handoverHoldersName)
	}
	seen := map[string]bool{}
	for _, holder := range file.Holders {
		label, labelOK := holderText(holder.Label, holderLabelLimit)
		channel, channelOK := holderText(holder.Channel, holderChannelLimit)
		if !holderIDForm.MatchString(holder.ID) || seen[holder.ID] || !labelOK || label != holder.Label ||
			!channelOK || channel != holder.Channel || holder.AddedAt < 0 {
			return nil, fmt.Errorf("%s is not a list of holders Desk wrote", handoverHoldersName)
		}
		seen[holder.ID] = true
	}
	return file.Holders, nil
}

// readHandoverRecord is a holder's record: empty where Desk has recorded no
// hand-over to it. A record that is not one Desk writes is an error.
func readHandoverRecord(holder *os.Root) (handoverRecord, error) {
	record := handoverRecord{Version: handoverVersion, Trails: map[string]handedOver{}}
	data, err := readPrivateData(holder, handoverRecordName, handoverListLimit)
	if errors.Is(err, fs.ErrNotExist) {
		return record, nil
	}
	if err != nil {
		return handoverRecord{}, err
	}
	if err := decodeDataJSON(data, &record); err != nil {
		return handoverRecord{}, fmt.Errorf("%s is not a record Desk wrote: %w", handoverRecordName, err)
	}
	if record.Version != handoverVersion || record.Trails == nil {
		return handoverRecord{}, fmt.Errorf("%s is not a record Desk wrote", handoverRecordName)
	}
	for trail, handed := range record.Trails {
		if !keyIDForm.MatchString(trail) || handed.Through < 1 || handed.ConfirmedAt < 0 || !recordForm.MatchString(handed.Digest) {
			return handoverRecord{}, fmt.Errorf("%s is not a record Desk wrote", handoverRecordName)
		}
	}
	return record, nil
}

// readHolderRecord is the holder id and Desk's record of it, read without
// making anything; found is false where Desk keeps no holder by that id.
func (s *Server) readHolderRecord(id string) (holder handoverHolder, record handoverRecord, found bool, err error) {
	handover, err := s.openHandover(false)
	if errors.Is(err, fs.ErrNotExist) {
		return handoverHolder{}, handoverRecord{}, false, nil
	}
	if err != nil {
		return handoverHolder{}, handoverRecord{}, false, err
	}
	defer handover.Close()
	holders, err := readHolders(handover)
	if err != nil {
		return handoverHolder{}, handoverRecord{}, false, err
	}
	index := slices.IndexFunc(holders, func(h handoverHolder) bool { return h.ID == id })
	if index < 0 {
		return handoverHolder{}, handoverRecord{}, false, nil
	}
	record = handoverRecord{Version: handoverVersion, Trails: map[string]handedOver{}}
	folder, err := openHolderFolder(handover, id, false)
	if errors.Is(err, fs.ErrNotExist) {
		return holders[index], record, true, nil
	}
	if err != nil {
		return handoverHolder{}, handoverRecord{}, false, err
	}
	defer folder.Close()
	record, err = readHandoverRecord(folder)
	if err != nil {
		return handoverHolder{}, handoverRecord{}, false, err
	}
	return holders[index], record, true, nil
}

/* The runtime's checkpoints ------------------------------------------------- */

// checkpointHead is the trail's identity and the sequence of its last
// chained record, as `audit checkpoint --format json` gives them.
type checkpointHead struct {
	Identity string `json:"identity"`
	Sequence int64  `json:"sequence"`
}

// checkpointLine is what Desk reads of one checkpoint document: its trail
// identity and sequence.
type checkpointLine struct {
	trail    string
	sequence int64
}

// readCheckpointLine holds one checkpoint document to the runtime's own shape
// (runtime 0.27.1, `audit.ParseCheckpoint`): one JSON object of exactly
// checkpointVersion "1", trail (32 lowercase hexadecimal characters),
// sequence (an integer from 1 to 2^53-2) and recordDigest ("sha256:" and 64
// lowercase hexadecimal characters), each named once.
func readCheckpointLine(document []byte) (checkpointLine, bool) {
	if len(document) == 0 || len(document) > 4096 || !json.Valid(document) {
		return checkpointLine{}, false
	}
	members, ok := exactMembers(document)
	if !ok || len(members) != 4 {
		return checkpointLine{}, false
	}
	var version, trail, digest string
	var sequence int64
	if !sidecarString(members["checkpointVersion"], &version) || version != "1" ||
		!sidecarString(members["trail"], &trail) || !keyIDForm.MatchString(trail) ||
		!sidecarInteger(members["sequence"], &sequence) ||
		!sidecarString(members["recordDigest"], &digest) || !recordForm.MatchString(digest) {
		return checkpointLine{}, false
	}
	return checkpointLine{trail: trail, sequence: sequence}, true
}

// readCheckpointHead asks the runtime for the trail's last chained record:
// `audit checkpoint --config jpack.json --format json`, read whatever the
// exit.
//
//   - Exit 0, "checkpointed", with a checkpoint of the runtime's shape: its
//     identity and sequence.
//   - The runtime's refusal because the trail has no chained record
//     (JPS-AUDIT-CHECKPOINT-NONE), or because there is no trail yet
//     (JPS-AUDIT-TRAIL-READ, where the trail is not there): nil, and no
//     refusal. There is nothing to hand over.
//   - Any other refusal, a non-zero exit with "error" or "unsupported" and
//     the runtime's own diagnostics: those, in its words.
//
// Anything else is not an answer the runtime documents, and is an error.
func (s *Server) readCheckpointHead(ctx context.Context, dir heldDir) (*checkpointHead, []runtimeDiagnostic, error) {
	out, runErr := runRuntime(ctx, s.cfg.JpackBin, dir, "audit", "checkpoint", "--config", runtimeConfigName, "--format", "json")
	code := 0
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			return nil, nil, runErr
		}
		code = exit.ExitCode()
	}
	var got struct {
		Command     string              `json:"command"`
		Status      string              `json:"status"`
		Checkpoint  json.RawMessage     `json:"checkpoint"`
		Diagnostics []runtimeDiagnostic `json:"diagnostics"`
	}
	undocumented := errors.New("its audit checkpoint did not answer as documented")
	if runErr != nil {
		undocumented = fmt.Errorf("its audit checkpoint did not answer as documented: %w", runErr)
	}
	if out == nil || json.Unmarshal(out, &got) != nil {
		return nil, nil, undocumented
	}
	switch {
	case code == 0 && got.Command == auditCheckpointCommand && got.Status == "checkpointed":
		if line, ok := readCheckpointLine(got.Checkpoint); ok {
			return &checkpointHead{Identity: line.trail, Sequence: line.sequence}, nil, nil
		}
	case code > 0 && (got.Status == "error" || got.Status == "unsupported") && (auditVerification{Diagnostics: got.Diagnostics}).said():
		if len(got.Diagnostics) == 1 && (got.Diagnostics[0].Code == "JPS-AUDIT-CHECKPOINT-NONE" ||
			got.Diagnostics[0].Code == "JPS-AUDIT-TRAIL-READ" && s.trailAbsent()) {
			return nil, nil, nil
		}
		return nil, got.Diagnostics, nil
	}
	return nil, nil, undocumented
}

// trailAbsent is whether the trail is not there: its audit directory, or the
// trail in it. A trail that is there and could not be opened is not absent.
func (s *Server) trailAbsent() bool {
	dir, declared, err := s.projectAuditDir()
	if err != nil || !declared {
		return false
	}
	parts, err := auditDirParts(dir)
	if err != nil {
		return false
	}
	root, err := s.openAuditDir(parts)
	if err != nil {
		return errors.Is(err, fs.ErrNotExist)
	}
	defer root.Close()
	_, err = root.Lstat(auditTrailFiles["evaluations"])
	return errors.Is(err, fs.ErrNotExist)
}

// checkpoints is what the runtime printed for the checkpoints after a
// cursor: the bytes, untouched; the sequence of the last line in them; and
// whether more remain past the bound.
type checkpoints struct {
	data    []byte
	through int64
	more    bool
}

// readCheckpoints asks the runtime for the checkpoints of identity after
// from, up to and with through: `audit checkpoint --config jpack.json --since
// <n> --limit 300`, in the human form, again after the last sequence
// received until fewer than 300 come back, at most handoverBatches times.
//
// Each line must be a checkpoint of identity, after the last one received;
// the lines are kept as printed, and the bytes end at the newline of the line
// whose sequence is through. A trail of another identity, or one that ends
// before through, is errCheckpointsChanged; a batch the runtime fails, or a
// line that is not a checkpoint or comes out of order, is an error.
func (s *Server) readCheckpoints(ctx context.Context, dir heldDir, identity string, from, through int64) (checkpoints, error) {
	var data []byte
	last := from
	for range handoverBatches {
		out, err := runRuntime(ctx, s.cfg.JpackBin, dir, "audit", "checkpoint", "--config", runtimeConfigName,
			"--since", strconv.FormatInt(last, 10), "--limit", strconv.Itoa(handoverBatch))
		if err != nil {
			return checkpoints{}, err
		}
		received := 0
		for _, line := range bytes.SplitAfter(out, []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			if line[len(line)-1] != '\n' {
				return checkpoints{}, errors.New("the runtime's audit checkpoint did not end its last line")
			}
			checkpoint, ok := readCheckpointLine(line[:len(line)-1])
			switch {
			case !ok:
				return checkpoints{}, errors.New("the runtime's audit checkpoint printed a line that is not a checkpoint")
			case checkpoint.trail != identity:
				return checkpoints{}, errCheckpointsChanged
			case checkpoint.sequence <= last:
				return checkpoints{}, errors.New("the runtime's audit checkpoint printed its checkpoints out of order")
			case checkpoint.sequence > through:
				return checkpoints{data: data, through: last}, nil
			}
			received++
			data = append(data, line...)
			last = checkpoint.sequence
			if last == through {
				return checkpoints{data: data, through: last}, nil
			}
		}
		if received < handoverBatch {
			return checkpoints{}, errCheckpointsChanged
		}
	}
	return checkpoints{data: data, through: last, more: true}, nil
}

/* The routes ------------------------------------------------------------------ */

// handoverRuntime is the folder the hand-over's commands run in, or why they
// do not run: the decision record's own refusal; a project that keeps no
// trail; and a runtime with no audit commands, each in the words the
// decision record uses for it.
func (s *Server) handoverRuntime(ctx context.Context) (heldDir, *lockFailure) {
	dir, refusal := s.auditRuntime()
	if refusal != "" {
		return heldDir{}, &lockFailure{http.StatusConflict, CodeBadRequest, refusal}
	}
	_, declared, err := s.projectAuditDir()
	if err != nil {
		return heldDir{}, &lockFailure{http.StatusInternalServerError, CodeInternal, "The hand-over could not be read: " + strings.TrimRight(err.Error(), ".") + "."}
	}
	if !declared {
		return heldDir{}, &lockFailure{http.StatusConflict, CodeBadRequest, noTrailWords}
	}
	schema, err := readRuntimeSchema(ctx, s.cfg.JpackBin, dir)
	if err != nil {
		return heldDir{}, &lockFailure{http.StatusInternalServerError, CodeInternal, "The hand-over could not be read: " + strings.TrimRight(err.Error(), ".") + "."}
	}
	if !slices.Contains(schema.supported, auditConfigVersion) {
		return heldDir{}, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf("This runtime (jpack %s) writes an unchained trail and has no audit commands. Chaining, checkpoints, signing and stamping need jpack %s or later.", schema.version, auditRuntimeFloor)}
	}
	return dir, nil
}

// refuse writes failure, its message passed through withoutPaths: the one
// way a message reaches the page from these routes. The log keeps it whole.
func (s *Server) refuse(w http.ResponseWriter, failure *lockFailure) {
	message := s.withoutPaths(failure.message)
	if message != failure.message || failure.status == http.StatusInternalServerError {
		s.log.Printf("desk: the hand-over, as said: %s", failure.message)
	}
	writeJSONCoded(w, failure.status, failure.code, message)
}

// handoverWords is a failure's error, ended as a sentence.
func handoverWords(prefix string, err error) string {
	return prefix + strings.TrimRight(err.Error(), ".") + "."
}

// holderTrailAnswer is one trail in a holder's record, as the page is shown
// it: unwitnessed, by that holder, only for the current trail.
type holderTrailAnswer struct {
	handedOver
	Unwitnessed *int64 `json:"unwitnessed,omitempty"`
}

// holderAnswer is one holder as the page is shown it. OtherTrail is true
// where Desk's record of it names only trails other than the current one:
// the trail was moved aside, and this holder starts at 0 for it.
type holderAnswer struct {
	handoverHolder
	Trails     map[string]holderTrailAnswer `json:"trails"`
	OtherTrail bool                         `json:"otherTrail,omitempty"`
}

// holdersAnswer is what `GET /api/audit/holders` answers. Trail is null where
// the trail has no chained record yet, or the runtime refuses to give a
// checkpoint, and Diagnostics then say why in its words.
type holdersAnswer struct {
	Holders     []holderAnswer      `json:"holders"`
	Trail       *checkpointHead     `json:"trail"`
	Diagnostics []runtimeDiagnostic `json:"diagnostics,omitempty"`
}

// shownHolder is holder, with Desk's record of it, as the page is shown it
// against head, the trail as it is now (nil where it has no chained record).
func shownHolder(holder handoverHolder, record handoverRecord, head *checkpointHead) holderAnswer {
	shown := holderAnswer{handoverHolder: holder, Trails: map[string]holderTrailAnswer{}}
	for trail, handed := range record.Trails {
		entry := holderTrailAnswer{handedOver: handed}
		if head != nil && trail == head.Identity {
			since := max(head.Sequence-handed.Through, 0)
			entry.Unwitnessed = &since
		}
		shown.Trails[trail] = entry
	}
	if head != nil && len(record.Trails) > 0 {
		_, current := record.Trails[head.Identity]
		shown.OtherTrail = !current
	}
	return shown
}

// handleHolders answers `GET /api/audit/holders`: the holders Desk keeps, and
// what it recorded of each, against the trail as the runtime gives it now.
func (s *Server) handleHolders(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	dir, failure := s.handoverRuntime(r.Context())
	if failure != nil {
		s.refuse(w, failure)
		return
	}
	head, refusal, err := s.readCheckpointHead(r.Context(), dir)
	if err != nil {
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("The trail's last checkpoint could not be read: ", err)})
		return
	}
	answer := holdersAnswer{Holders: []holderAnswer{}, Trail: head}
	for _, diagnostic := range refusal {
		answer.Diagnostics = append(answer.Diagnostics, runtimeDiagnostic{Code: diagnostic.Code, Message: s.withoutPaths(diagnostic.Message)})
	}
	s.handoverMu.Lock()
	defer s.handoverMu.Unlock()
	handover, err := s.openHandover(false)
	if errors.Is(err, fs.ErrNotExist) {
		writeJSON(w, http.StatusOK, answer)
		return
	}
	if err != nil {
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("Desk's record of hand-overs could not be read: ", err)})
		return
	}
	defer handover.Close()
	holders, err := readHolders(handover)
	if err != nil {
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("Desk's list of holders could not be read: ", err)})
		return
	}
	for _, holder := range holders {
		record := handoverRecord{Version: handoverVersion, Trails: map[string]handedOver{}}
		folder, err := openHolderFolder(handover, holder.ID, false)
		if err == nil {
			record, err = readHandoverRecord(folder)
			folder.Close()
		} else if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
		if err != nil {
			s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("Desk's record of what was handed over to "+holder.Label+" could not be read: ", err)})
			return
		}
		answer.Holders = append(answer.Holders, shownHolder(holder, record, head))
	}
	writeJSON(w, http.StatusOK, answer)
}

// handoverRequest reads a request's JSON body into into, under the same
// rules as a rotation's confirmation: a JSON body, from the same site,
// bounded, and of exactly the members asked for.
func handoverRequest(w http.ResponseWriter, r *http.Request, into any, refused string) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSONCoded(w, http.StatusForbidden, CodeForbidden, "A cross-site request cannot change this desk's record of hand-overs.")
		return false
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeJSONCoded(w, http.StatusUnsupportedMediaType, CodeBadRequest, "Send it as JSON.")
		return false
	}
	data, err := readBounded(r.Body, handoverRequestLimit)
	if err != nil || decodeDataJSON(data, into) != nil {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, refused)
		return false
	}
	return true
}

// handleAddHolder answers `POST /api/audit/holders`: a holder the owner adds,
// by a label and a channel of their own words.
func (s *Server) handleAddHolder(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request struct {
		Label   string `json:"label"`
		Channel string `json:"channel"`
	}
	if !handoverRequest(w, r, &request, holderTextWords) {
		return
	}
	label, labelOK := holderText(request.Label, holderLabelLimit)
	channel, channelOK := holderText(request.Channel, holderChannelLimit)
	if !labelOK || !channelOK {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, holderTextWords)
		return
	}
	if _, failure := s.handoverRuntime(r.Context()); failure != nil {
		s.refuse(w, failure)
		return
	}
	s.handoverMu.Lock()
	defer s.handoverMu.Unlock()
	handover, err := s.openHandover(true)
	if err != nil {
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("Desk's record of hand-overs could not be opened: ", err)})
		return
	}
	defer handover.Close()
	holders, err := readHolders(handover)
	if err != nil {
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("Desk's list of holders could not be read: ", err)})
		return
	}
	if len(holders) >= maxHolders {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, fmt.Sprintf("Desk keeps at most %d holders for one desk, so it adds no more.", maxHolders))
		return
	}
	id, err := newHolderID()
	if err == nil && (!holderIDForm.MatchString(id) || slices.ContainsFunc(holders, func(h handoverHolder) bool { return h.ID == id })) {
		err = errors.New("no new holder id could be made")
	}
	if err != nil {
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("The holder could not be added: ", err)})
		return
	}
	holder := handoverHolder{ID: id, Label: label, Channel: channel, AddedAt: handoverClock().Unix()}
	data, err := json.Marshal(holdersFile{Version: handoverVersion, Holders: append(holders, holder)})
	if err == nil {
		err = writePrivateData(handover, handoverHoldersName, append(data, '\n'))
	}
	if err != nil {
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("The holder could not be added: ", err)})
		return
	}
	writeJSON(w, http.StatusCreated, holderAnswer{handoverHolder: holder, Trails: map[string]holderTrailAnswer{}})
}

// checkpointsRequest reads the one thing a download may ask: `holder`, once,
// a holder's id. Anything else is refused.
func checkpointsRequest(raw string) (string, bool) {
	values, err := url.ParseQuery(raw)
	if err != nil || len(values) != 1 || len(values["holder"]) != 1 || !holderIDForm.MatchString(values["holder"][0]) {
		return "", false
	}
	return values["holder"][0], true
}

// checkpointsName is the file a download is saved under.
func checkpointsName(trail string, from, through int64) string {
	return fmt.Sprintf("checkpoints-%s-%d-%d.jsonl", trail, from+1, through)
}

// handleCheckpoints answers `GET /api/audit/checkpoints?holder=<id>`: the
// checkpoints after that holder's cursor, through the trail's last chained
// record, as the exact bytes the runtime printed. It moves nothing: only a
// confirmation does.
func (s *Server) handleCheckpoints(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	id, ok := checkpointsRequest(r.URL.RawQuery)
	if !ok {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Ask for one holder's checkpoints, once: holder=<its id>.")
		return
	}
	dir, failure := s.handoverRuntime(r.Context())
	if failure != nil {
		s.refuse(w, failure)
		return
	}
	s.handoverMu.Lock()
	_, record, found, err := s.readHolderRecord(id)
	s.handoverMu.Unlock()
	switch {
	case err != nil:
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("Desk's record of hand-overs could not be read: ", err)})
		return
	case !found:
		writeJSONCoded(w, http.StatusNotFound, CodeNotFound, noSuchHolderWords)
		return
	}
	head, refusal, err := s.readCheckpointHead(r.Context(), dir)
	switch {
	case err != nil:
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("The checkpoints could not be read: ", err)})
		return
	case refusal != nil:
		s.refuse(w, &lockFailure{http.StatusConflict, CodeBadRequest, runtimeRefusalWords(refusal)})
		return
	case head == nil:
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, noChainedWords)
		return
	}
	cursor := record.Trails[head.Identity].Through
	if cursor == head.Sequence {
		w.Header().Set(checkpointsTrailHeader, head.Identity)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if cursor > head.Sequence {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, fmt.Sprintf("Desk recorded checkpoints through record %d as handed over to this holder, and the trail's last chained record is now record %d: the trail is shorter than what was handed over, so Desk hands nothing over.", cursor, head.Sequence))
		return
	}
	read, err := s.readCheckpoints(r.Context(), dir, head.Identity, cursor, head.Sequence)
	if err != nil {
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("The checkpoints could not be read, and nothing was handed over: ", err)})
		return
	}
	w.Header().Set("Content-Type", "application/jsonl")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+checkpointsName(head.Identity, cursor, read.through)+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(read.data)))
	w.Header().Set(checkpointsTrailHeader, head.Identity)
	w.Header().Set(checkpointsFromHeader, strconv.FormatInt(cursor, 10))
	w.Header().Set(checkpointsThroughHeader, strconv.FormatInt(read.through, 10))
	w.Header().Set(checkpointsDigestHeader, sha256Digest(read.data))
	w.Header().Set(checkpointsMoreHeader, strconv.FormatBool(read.more))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(read.data)
}

// runtimeRefusalWords is the runtime's refusal to give a checkpoint, in its
// own words.
func runtimeRefusalWords(refusal []runtimeDiagnostic) string {
	said := make([]string, 0, len(refusal))
	for _, diagnostic := range refusal {
		said = append(said, strings.TrimSpace(diagnostic.Message))
	}
	return "The runtime gives no checkpoint of this trail now: " + strings.Join(said, " ")
}

// staleRefusal is what a confirmation that moves nothing answers: the
// owner's file is not what the trail gives now, from the holder's cursor as
// it stands.
func staleRefusal(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]string{"reason": "stale", "code": CodeStale, "error": staleWords})
}

// heldAfter is held, a holder's file of checkpoints, with data appended where
// it follows from: the checkpoints confirmed now, after the record from.
//
// **Only ever appended to.** The file as read stays the start of what is
// written. Where it already holds checkpoints after from, as it does when a
// confirmation's file was written and its record was not, those must be the
// first of data, and only the rest is appended; anything else in their place
// says the file and Desk's record disagree, and nothing is written.
func heldAfter(held, data []byte, from int64) ([]byte, error) {
	if len(held) > 0 && held[len(held)-1] != '\n' {
		return nil, errors.New("the checkpoints Desk keeps for this holder do not end with a whole line")
	}
	cut := len(held)
	for cut > 0 {
		start := bytes.LastIndexByte(held[:cut-1], '\n') + 1
		line, ok := readCheckpointLine(held[start : cut-1])
		if !ok {
			return nil, errors.New("the checkpoints Desk keeps for this holder hold a line that is not a checkpoint")
		}
		if line.sequence <= from {
			break
		}
		cut = start
	}
	tail := held[cut:]
	if !bytes.HasPrefix(data, tail) {
		return nil, errors.New("the checkpoints Desk keeps for this holder go past its record, and are not the ones confirmed now")
	}
	next := append(slices.Clip(held), data[len(tail):]...)
	if len(next) > handoverHeldLimit {
		return nil, fmt.Errorf("the checkpoints Desk keeps for this holder would pass the %d bytes the runtime reads for one holder", handoverHeldLimit)
	}
	return next, nil
}

// handleConfirmHandover answers `POST /api/audit/holders/{id}/confirm`: the
// owner's word that the file downloaded went to the holder. Desk asks the
// runtime for the same checkpoints again, from the holder's cursor as it
// stands, and records them only where they are the same bytes: appended to
// the holder's file for the trail, and its record moved to them. Otherwise it
// changes nothing, and says the file is stale: a trail with no chained record
// now, one moved aside (its lines are of another identity), one that ends
// before the record confirmed, and one that gives other bytes are each that.
func (s *Server) handleConfirmHandover(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("id")
	var request struct {
		Trail   string `json:"trail"`
		From    *int64 `json:"from"`
		Through *int64 `json:"through"`
		Digest  string `json:"digest"`
	}
	const refused = "Confirm a hand-over with the trail, the records and the SHA-256 its download gave."
	if !handoverRequest(w, r, &request, refused) {
		return
	}
	if !holderIDForm.MatchString(id) || !keyIDForm.MatchString(request.Trail) || request.From == nil || request.Through == nil ||
		*request.From < 0 || *request.Through <= *request.From || !recordForm.MatchString(request.Digest) {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, refused)
		return
	}
	from, through := *request.From, *request.Through
	dir, failure := s.handoverRuntime(r.Context())
	if failure != nil {
		s.refuse(w, failure)
		return
	}
	s.handoverMu.Lock()
	defer s.handoverMu.Unlock()
	holder, record, found, err := s.readHolderRecord(id)
	if testHookConfirmRead != nil {
		testHookConfirmRead()
	}
	switch {
	case err != nil:
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("Desk's record of hand-overs could not be read, and nothing was recorded: ", err)})
		return
	case !found:
		writeJSONCoded(w, http.StatusNotFound, CodeNotFound, noSuchHolderWords)
		return
	case record.Trails[request.Trail].Through != from:
		staleRefusal(w)
		return
	}
	head, refusal, err := s.readCheckpointHead(r.Context(), dir)
	switch {
	case err != nil:
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("The checkpoints could not be read, and nothing was recorded: ", err)})
		return
	case refusal != nil:
		s.refuse(w, &lockFailure{http.StatusConflict, CodeBadRequest, runtimeRefusalWords(refusal)})
		return
	case head == nil:
		staleRefusal(w)
		return
	}
	read, err := s.readCheckpoints(r.Context(), dir, request.Trail, from, through)
	switch {
	case errors.Is(err, errCheckpointsChanged):
		staleRefusal(w)
		return
	case err != nil:
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("The checkpoints could not be read, and nothing was recorded: ", err)})
		return
	case read.through != through || sha256Digest(read.data) != request.Digest:
		staleRefusal(w)
		return
	}
	if err := s.recordHandover(id, request.Trail, from, through, read.data, &record); err != nil {
		s.refuse(w, &lockFailure{http.StatusInternalServerError, CodeInternal, handoverWords("The hand-over could not be recorded: ", err)})
		return
	}
	writeJSON(w, http.StatusOK, shownHolder(holder, record, head))
}

// recordHandover appends data, the checkpoints of trail after from through
// through, to the holder's file for that trail, and then moves its record to
// them, by Desk's clock. The caller holds handoverMu, and record is the
// record it read under it, which this updates.
func (s *Server) recordHandover(id, trail string, from, through int64, data []byte, record *handoverRecord) error {
	handover, err := s.openHandover(true)
	if err != nil {
		return err
	}
	defer handover.Close()
	folder, err := openHolderFolder(handover, id, true)
	if err != nil {
		return err
	}
	defer folder.Close()
	name := trail + ".jsonl"
	held, err := readPrivateData(folder, name, handoverHeldLimit)
	if errors.Is(err, fs.ErrNotExist) {
		held, err = nil, nil
	}
	if err != nil {
		return err
	}
	next, err := heldAfter(held, data, from)
	if err != nil {
		return err
	}
	if err := writePrivateData(folder, name, next); err != nil {
		return err
	}
	trails := maps.Clone(record.Trails)
	if trails == nil {
		trails = map[string]handedOver{}
	}
	trails[trail] = handedOver{Through: through, ConfirmedAt: handoverClock().Unix(), Digest: sha256Digest(data)}
	written, err := json.Marshal(handoverRecord{Version: handoverVersion, Trails: trails})
	if err != nil {
		return err
	}
	if err := writePrivateData(folder, handoverRecordName, append(written, '\n')); err != nil {
		return err
	}
	record.Trails = trails
	return nil
}

/* What the decision record passes ---------------------------------------- */

// heldExpectations is what the decision record passes `audit verify` of
// Desk's record of hand-overs: an `--expect` for each holder's file of
// checkpoints for the current trail, and the labels of the holders whose file
// could not be read now, or is not ours, and was passed over.
type heldExpectations struct {
	args   []string
	count  int
	unread []string
}

// heldCheckpoints is the decision record's `--expect` arguments: one for each
// holder whose file of checkpoints for the trail as it is now (`audit
// checkpoint --format json`) is there, by its path relative to the project,
// where `runRuntime` runs. A holder's file for another trail identity is never
// passed: a held checkpoint of another trail fails the check. A file that
// cannot be read now, or is not ours, is passed over and named. Where Desk
// keeps no holder, nothing is run and nothing passed.
func (s *Server) heldCheckpoints(ctx context.Context, dir heldDir) heldExpectations {
	s.handoverMu.Lock()
	defer s.handoverMu.Unlock()
	var held heldExpectations
	handover, err := s.openHandover(false)
	if errors.Is(err, fs.ErrNotExist) {
		return held
	}
	if err != nil {
		s.log.Printf("desk: the record of hand-overs could not be opened for the decision record: %v", err)
		return held
	}
	defer handover.Close()
	holders, err := readHolders(handover)
	if err != nil {
		s.log.Printf("desk: the list of holders could not be read for the decision record: %v", err)
		return held
	}
	if len(holders) == 0 {
		return held
	}
	head, _, err := s.readCheckpointHead(ctx, dir)
	if err != nil || head == nil {
		return held
	}
	name := head.Identity + ".jsonl"
	for _, holder := range holders {
		folder, err := openHolderFolder(handover, holder.ID, false)
		if err == nil {
			err = checkHeldFile(folder, name)
			folder.Close()
		}
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			s.log.Printf("desk: the checkpoints handed over to holder %s were not passed to the decision record: %v", holder.ID, err)
			held.unread = append(held.unread, holder.Label)
		default:
			held.args = append(held.args, "--expect", path.Join(handoverDir, holder.ID, name))
			held.count++
		}
	}
	return held
}

// checkHeldFile is whether a holder's file of checkpoints can be read now and
// is ours: as `readPrivateData` reads a file, owner-only, owned by this user,
// a regular file and not a link, the one that was looked at, and within the
// runtime's bound; and not empty, as Desk never writes it.
func checkHeldFile(folder *os.Root, name string) error {
	info, err := folder.Lstat(name)
	if err != nil {
		return err
	}
	if err := ownerOnlyFile(name, info.Mode()); err != nil {
		return err
	}
	if err := ownedByUs(name, info); err != nil {
		return err
	}
	if info.Size() == 0 || info.Size() > handoverHeldLimit {
		return fmt.Errorf("%s holds %d bytes", name, info.Size())
	}
	file, err := folder.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, opened) {
		return errPrivateDataChanged
	}
	return nil
}
