package desk

// Repairing a desk's trail, on the owner's word (ADR-0010, section 4,
// "Repair"; the maintainer's answer to question 8).
//
// # What a repair is
//
// A write that did not complete leaves the trail's last line with no newline,
// and the runtime chains nothing after it: every deciding run is refused,
// exit 4, "the audit trail's last line is incomplete". The runtime's `jpack
// audit repair` ends the damaged bytes with a newline and keeps them in place
// as a line of their own, and appends a discontinuity record after them,
// naming that line, its length and the SHA-256 of its bytes. The trail then
// has a new segment after the damaged line, and `audit verify` reports it as
// `segmented`, never as intact across it. A repair never restores the lost
// line.
//
// # When Desk offers it
//
// Only where the decision record's report names the finding
// `incomplete-last-line` (`repairOffer`): never on a timer, never on Desk's
// own initiative, and never on the Jobs record, whose chain of runs is
// Runner's. Where the runtime reads no "6", or the project declares no audit
// directory, there is no report, and nothing is offered.
//
// **Only on a chained trail** (review round 1). The runtime repairs only a
// trail its audit member chains, and refuses one that says `"chain": false`;
// and on such a trail a deciding run is not refused at all, so the
// confirmation's sentence would not be true. Where jpack.json says so, the
// panel says why it offers nothing, and promises nothing.
//
// # The owner's confirmation
//
// The offer carries a token: a nonce, and a MAC, under this desk's own review
// key, over the nonce and what Desk read of the trail. From the report: its
// identity (none where no line is chained yet), the incomplete line and the
// size the runtime read. From the trail itself, read as the download reads it
// (`readTrail`): the file's identity, device and inode, its size, and the
// SHA-256 of its bytes. A repair runs only where a fresh `audit verify` and a
// fresh reading, made when the confirmation arrives, immediately before the
// repair, give the same MAC (`repairTrail`). A token from another desk, a
// token for a trail that changed since, by identity, line, size, bytes or
// file, and a token whose nonce was used already, are each refused with a
// plain sentence, and nothing runs.
//
// **A token confirms one attempt** (review round 1). Its nonce is spent, under
// repairMu, before the runtime is asked to repair, whatever it then answers:
// a replay after the repair, with the trail's bytes put back, or after the
// runtime's refusal, once what it refused is changed, needs a fresh offer.
//
// # The repair
//
// `jpack audit repair --config jpack.json --format json`, through
// `runRuntime`, in the directory this desk holds, once. The runtime takes the
// trail's own lock for it; Desk takes no lock on the trail, beyond the shared
// one its reading takes to read the size between two writes. One Desk process
// runs one repair of a desk at a time (`repairMu`). Once it is started, the
// request going away does not stop it: a repair cut short could leave a
// discontinuity record whose own write did not complete, which no repair
// mends. Its answer is read as runtime 0.27.1 gives it, under outputVersion
// "2"; its refusal is passed on in its own words, with no path. The page then
// checks the decision record again (ADR-0010, section 4, "When it runs").
//
// **What Desk cannot close** (review round 1). Between Desk's last reading
// and the runtime's own, another writer can still append: `audit repair`
// takes no precondition to hold it to what Desk read. So Desk compares the
// discontinuity the runtime reports, the damaged line, its length and its
// digest, with the bytes it read, and where they differ it says so, never as
// a repair confirmed. Closing that window needs a precondition in the
// runtime.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"mime"
	"net/http"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

const (
	// incompleteLastLine is the finding `audit verify` names for a trail
	// whose last line has no newline (runtime 0.27.1, measured).
	incompleteLastLine = "incomplete-last-line"
	// auditRepairCommand is the command's name as its JSON answer gives it.
	auditRepairCommand = "audit repair"
	// repairOutputVersion is the runtime's outputVersion whose answer Desk
	// reads, as the other audit answers are read.
	repairOutputVersion = "2"
	// repairPurpose names what a repair's token confirms, so that no other
	// token of this desk's confirms a repair.
	repairPurpose = "repair-audit-trail"
	// repairConfirmLimit bounds a confirmation's body: a token.
	repairConfirmLimit = 4 << 10
	// repairNonceLength is the length of a token's nonce, in hexadecimal
	// characters, and repairTokenLength a token's: the nonce, then the MAC.
	repairNonceLength = 32
	repairTokenLength = repairNonceLength + 2*sha256.Size
	// What the panel can say of a repair: offered, with a token; or not, and
	// why, in Desk's words.
	repairAvailable   = "available"
	repairUnavailable = "unavailable"
)

// Desk's own sentences about a repair. The page lists them (REPAIR_REASONS),
// so that each is shown in the owner's language.
const (
	repairStaleWords      = "The trail changed after the decision record showed it, so nothing was repaired. Check the decision record again."
	repairNoTrailWords    = "Nothing was repaired: this project's jpack.json declares no audit directory, so it keeps no trail."
	repairUncheckedWords  = "Nothing was repaired: the runtime did not check the trail again, so Desk could not tell that it is the trail the decision record showed. It said:"
	repairRefusedWords    = "The runtime did not report a repair. It said:"
	repairUndocumented    = "The runtime's audit repair did not answer as documented, so Desk cannot say whether it repaired the trail. Check the decision record again: it shows what the trail holds now."
	repairCheckAgainWords = ". Desk cannot say whether it repaired the trail. Check the decision record again: it shows what the trail holds now."
	// The trail is not chained: no offer, and no repair on a confirmation.
	repairUnchainedWords    = "This project's jpack.json says audit.chain false, and the runtime repairs only a chained trail, so Desk offers no repair."
	repairNotChainedWords   = "Nothing was repaired: this project's jpack.json says audit.chain false, and the runtime repairs only a chained trail."
	repairConfigUnreadWords = "Desk could not read this project's jpack.json to tell whether its trail is chained, so it offers no repair."
	// A token whose nonce was spent.
	repairUsedWords = "This confirmation was used already, so nothing was repaired. Check the decision record again: it offers a fresh one where a repair is still needed."
	// A repair the runtime reports of other damaged bytes than Desk read.
	repairDiffersWords = "The runtime repaired a damaged last line other than the one the decision record showed, so this is not the repair you confirmed: check the decision record again."
)

// repairOlderWords is what a confirmation says where the runtime has no
// `audit repair`: its version, and the first release with it.
func repairOlderWords(version string) string {
	return fmt.Sprintf("Nothing was repaired: the runtime this Desk runs (jpack %s) does not read configVersion 6 and has no audit repair. A runtime of %s or later has it.", version, auditRuntimeFloor)
}

// auditRepair is the decision record's word on `audit repair`, given only
// where the report names incomplete-last-line: the incomplete line it names;
// with "available", the token that confirms the repair of the trail as Desk
// read it; with "unavailable", why Desk offers none, in its own words.
type auditRepair struct {
	State  string `json:"state"`
	Line   int64  `json:"line"`
	Token  string `json:"token,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// repairAnswer is what a repair answers: the discontinuity record the runtime
// reports it wrote, by its own member names.
type repairAnswer struct {
	// State is "repaired".
	State         string             `json:"state"`
	Discontinuity auditDiscontinuity `json:"discontinuity"`
}

// repairFailure is why nothing was repaired, or why Desk cannot say whether
// it was: Desk's sentence, and the runtime's diagnostics where it gave them.
type repairFailure struct {
	status      int
	code        string
	message     string
	diagnostics []runtimeDiagnostic
}

// repairFinding is the incomplete line report names, where it names one.
func repairFinding(report *auditReport) (int64, bool) {
	if report == nil {
		return 0, false
	}
	for _, finding := range report.Findings {
		if finding.Name == incompleteLastLine {
			return finding.Line, true
		}
	}
	return 0, false
}

// repairOffer is the panel's word on a repair, where report names the finding
// incomplete-last-line, and nil everywhere else: offered where the trail is
// chained and Desk could read it, with a token over a fresh nonce; otherwise
// not, and why.
func (s *Server) repairOffer(ctx context.Context, report *auditReport) *auditRepair {
	line, found := repairFinding(report)
	if !found {
		return nil
	}
	unavailable := func(reason string) *auditRepair {
		return &auditRepair{State: repairUnavailable, Line: line, Reason: reason}
	}
	chained, err := s.trailChained()
	switch {
	case err != nil:
		s.log.Printf("desk: jpack.json could not be read to tell whether the trail is chained: %v", err)
		return unavailable(repairConfigUnreadWords)
	case !chained:
		return unavailable(repairUnchainedWords)
	}
	offered, err := s.readTrail(ctx)
	if err != nil {
		s.log.Printf("desk: the trail could not be read to bind a repair to it: %v", err)
		return unavailable("Desk binds a repair to the trail's bytes as it reads them, and offers none now: " + trailProblem(err) + ".")
	}
	return &auditRepair{State: repairAvailable, Line: line, Token: s.repairToken(newRepairNonce(), report, line, offered)}
}

// newRepairNonce is a fresh nonce for a token, in hexadecimal.
func newRepairNonce() string {
	var nonce [repairNonceLength / 2]byte
	// crypto/rand never fails, and never returns short (Go 1.24 and later).
	_, _ = rand.Read(nonce[:])
	return hex.EncodeToString(nonce[:])
}

// repairToken binds a confirmation to this desk, by its own review key; to
// one attempt, by nonce; to the trail as report read it, its identity, the
// incomplete line and its size; and to the trail as Desk read it, the file's
// identity and its bytes. It is the nonce, then the MAC.
func (s *Server) repairToken(nonce string, report *auditReport, line int64, read trailReading) string {
	payload, _ := json.Marshal(struct {
		Purpose string `json:"purpose"`
		Nonce   string `json:"nonce"`
		Trail   string `json:"trail"`
		Line    int64  `json:"line"`
		Bytes   int64  `json:"bytes"`
		File    string `json:"file"`
		Content string `json:"content"`
	}{
		Purpose: repairPurpose,
		Nonce:   nonce,
		Trail:   report.Trail,
		Line:    line,
		Bytes:   report.Bytes,
		File:    read.file,
		Content: read.content(),
	})
	mac := hmac.New(sha256.New, s.reviewKey[:])
	mac.Write(payload)
	return nonce + hex.EncodeToString(mac.Sum(nil))
}

// trailChained is whether this project's jpack.json leaves its trail chained,
// as the runtime reads it (runtime 0.27.1, `Audit.Chains`): unless its audit
// member says `"chain": false`. It reads that member and nothing else.
func (s *Server) trailChained() (bool, error) {
	config, err := s.readReviewFileWithin(runtimeConfigName, reviewTextLimit)
	if err != nil {
		return false, fmt.Errorf("%s could not be read: %w", runtimeConfigName, err)
	}
	var declared struct {
		Audit *struct {
			Chain json.RawMessage `json:"chain"`
		} `json:"audit"`
	}
	if err := json.Unmarshal(config, &declared); err != nil {
		return false, fmt.Errorf("%s is not a configuration Desk can read: %w", runtimeConfigName, err)
	}
	return declared.Audit == nil || !bytes.Equal(bytes.TrimSpace(declared.Audit.Chain), []byte("false")), nil
}

// trailReading is the trail as Desk read it to bind a repair to it: the
// file's identity, device and inode; its size, read between two writes; the
// SHA-256 of its bytes up to that size; and the length and SHA-256 of the
// bytes after its last newline, the line a repair keeps as damaged.
type trailReading struct {
	file       string
	size       int64
	digest     string
	tail       int64
	tailDigest string
}

// content is the size and the digest, as the token binds them.
func (r trailReading) content() string {
	return strconv.FormatInt(r.size, 10) + " " + r.digest
}

// errTrailUnidentified is a trail whose file identity this system does not
// give.
var errTrailUnidentified = errors.New("Desk cannot tell this file's identity here")

// readTrail reads this desk's trail as the trail's download reads it
// (`snapshotAuditFile`): through the project's root, refusing links and a
// second name, its size read under the trail's shared lock, so it falls
// between two writes, and the bytes before it read from the same descriptor.
func (s *Server) readTrail(ctx context.Context) (trailReading, error) {
	dir, declared, err := s.projectAuditDir()
	if err != nil {
		return trailReading{}, err
	}
	if !declared {
		return trailReading{}, errAuditNoTrail
	}
	parts, err := auditDirParts(dir)
	if err != nil {
		return trailReading{}, err
	}
	root, err := s.openAuditDir(parts)
	if err != nil {
		return trailReading{}, err
	}
	defer root.Close()
	snapshot, err := snapshotAuditFile(ctx, root, "evaluations")
	if err != nil {
		return trailReading{}, err
	}
	defer snapshot.file.Close()
	info, err := snapshot.file.Stat()
	if err != nil {
		return trailReading{}, err
	}
	read := trailReading{file: identityKey(info), size: snapshot.size}
	if read.file == "" {
		return trailReading{}, errTrailUnidentified
	}
	whole := &lastNewline{sum: sha256.New(), last: -1}
	if _, err := io.Copy(whole, io.NewSectionReader(snapshot.file, 0, snapshot.size)); err != nil {
		return trailReading{}, err
	}
	read.digest = "sha256:" + hex.EncodeToString(whole.sum.Sum(nil))
	start := whole.last + 1
	tail := sha256.New()
	if _, err := io.Copy(tail, io.NewSectionReader(snapshot.file, start, snapshot.size-start)); err != nil {
		return trailReading{}, err
	}
	read.tail, read.tailDigest = snapshot.size-start, "sha256:"+hex.EncodeToString(tail.Sum(nil))
	return read, nil
}

// lastNewline hashes what is written to it, and keeps the offset of the last
// newline in it: -1, as it starts, where there is none.
type lastNewline struct {
	sum    hash.Hash
	offset int64
	last   int64
}

func (l *lastNewline) Write(p []byte) (int, error) {
	if i := bytes.LastIndexByte(p, '\n'); i >= 0 {
		l.last = l.offset + int64(i)
	}
	l.offset += int64(len(p))
	return l.sum.Write(p)
}

// trailProblem is why the trail could not be read, in the download's words,
// with no path.
func trailProblem(err error) string {
	if errors.Is(err, errTrailUnidentified) {
		return errTrailUnidentified.Error()
	}
	_, _, message := auditTrailRefusal(err, auditTrailFiles["evaluations"])
	return strings.TrimRight(message, ".")
}

// handleAuditRepair answers `POST /api/audit/repair`: repair this desk's
// trail, as the decision record the token names showed it, or run nothing.
//
// It is a `POST` with a JSON body and the desk's bearer, which a cross-site
// page cannot send: the guard checks the session and the Origin, and a
// request a browser marks cross-site is refused besides.
func (s *Server) handleAuditRepair(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSONCoded(w, http.StatusForbidden, CodeForbidden, "A cross-site request cannot repair this desk's trail.")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeJSONCoded(w, http.StatusUnsupportedMediaType, CodeBadRequest, "Send the confirmation as JSON.")
		return
	}
	var request struct {
		Token string `json:"token"`
	}
	data, err := readBounded(r.Body, repairConfirmLimit)
	if err != nil || decodeDataJSON(data, &request) != nil || len(request.Token) != repairTokenLength {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Confirm the repair with the token the decision record gave.")
		return
	}
	project, refusal := s.auditRuntime()
	if refusal != "" {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	// **The lock around the repair alone, released by a defer**, as a
	// rotation's is: a panic, which net/http recovers, or a client that stops
	// reading the answer, never leaves it held.
	answer, failure := func() (*repairAnswer, *repairFailure) {
		s.repairMu.Lock()
		defer s.repairMu.Unlock()
		return s.repairTrail(r.Context(), project, request.Token)
	}()
	auditDir, _, _ := s.projectAuditDir()
	clean := func(message string) string { return s.withoutPathsUnder(message, auditDir) }
	if failure != nil {
		s.refuseRepair(w, failure, clean)
		return
	}
	s.log.Printf("desk: the trail of desk %s was repaired on the owner's confirmation: line %d (%d bytes, %s) is kept as damaged, and the discontinuity record at line %d starts a new segment",
		s.signingKeyName(), answer.Discontinuity.DamagedLine, answer.Discontinuity.Bytes, answer.Discontinuity.Digest, answer.Discontinuity.Line)
	shown := *answer
	shown.Discontinuity.Reason = clean(answer.Discontinuity.Reason)
	writeJSON(w, http.StatusOK, shown)
}

// refuseRepair answers failure: Desk's sentence and the runtime's
// diagnostics, each passed through clean, so that no path reaches the page.
// The log keeps them whole.
func (s *Server) refuseRepair(w http.ResponseWriter, failure *repairFailure, clean func(string) string) {
	message := clean(failure.message)
	said := make([]runtimeDiagnostic, len(failure.diagnostics))
	for i, diagnostic := range failure.diagnostics {
		said[i] = runtimeDiagnostic{Code: diagnostic.Code, Message: clean(diagnostic.Message)}
	}
	if message != failure.message || !slices.Equal(said, failure.diagnostics) || failure.status == http.StatusInternalServerError {
		s.log.Printf("desk: the repair of desk %s's trail, as said: %s %s", s.signingKeyName(), failure.message, mustJSON(failure.diagnostics))
	}
	body := map[string]any{"error": message, "code": failure.code}
	if len(said) > 0 {
		body["diagnostics"] = said
	}
	writeJSON(w, failure.status, body)
}

// repairTrail runs the repair the token confirms, or runs nothing: whether
// its nonce was spent; whether the runtime has `audit repair`; whether the
// project keeps a trail, and a chained one; a fresh `audit verify` and a
// fresh reading of the trail, which must give the same MAC; the nonce spent;
// `audit repair`, once; and what it reports held to the bytes read. The
// caller holds repairMu.
func (s *Server) repairTrail(ctx context.Context, project heldDir, token string) (*repairAnswer, *repairFailure) {
	nothing := func(status int, code string, err error) (*repairAnswer, *repairFailure) {
		return nil, &repairFailure{status: status, code: code, message: "Nothing was repaired: " + strings.TrimRight(err.Error(), ".") + "."}
	}
	nonce := token[:repairNonceLength]
	if s.repairNonces[nonce] {
		return nil, &repairFailure{status: http.StatusConflict, code: CodeStale, message: repairUsedWords}
	}
	schema, err := readRuntimeSchema(ctx, s.cfg.JpackBin, project)
	if err != nil {
		return nothing(http.StatusInternalServerError, CodeInternal, err)
	}
	if !slices.Contains(schema.supported, auditConfigVersion) {
		return nil, &repairFailure{status: http.StatusConflict, code: CodeBadRequest, message: repairOlderWords(schema.version)}
	}
	if _, declared, err := s.projectAuditDir(); err != nil {
		return nothing(http.StatusInternalServerError, CodeInternal, err)
	} else if !declared {
		return nil, &repairFailure{status: http.StatusConflict, code: CodeBadRequest, message: repairNoTrailWords}
	}
	if chained, err := s.trailChained(); err != nil {
		return nothing(http.StatusInternalServerError, CodeInternal, err)
	} else if !chained {
		return nil, &repairFailure{status: http.StatusConflict, code: CodeBadRequest, message: repairNotChainedWords}
	}

	// **The trail as it is now, in the runtime's word and as Desk reads it,
	// immediately before the repair.** The token was given for a report and a
	// reading; the repair runs only where both, made again now, give the same
	// MAC.
	out, runErr := runRuntime(ctx, s.cfg.JpackBin, project, "audit", "verify", "--config", runtimeConfigName, "--format", "json")
	now, err := readAuditVerification(out, runErr)
	switch {
	case err != nil:
		return nothing(http.StatusInternalServerError, CodeInternal, fmt.Errorf("the runtime could not check the trail again: %w", err))
	case now.State == auditStateOlder:
		return nil, &repairFailure{status: http.StatusConflict, code: CodeBadRequest, message: repairOlderWords(schema.version)}
	case now.State == auditStateUnverified:
		return nil, &repairFailure{status: http.StatusConflict, code: CodeBadRequest, message: repairUncheckedWords, diagnostics: now.Diagnostics}
	}
	line, found := repairFinding(now.Report)
	if !found {
		return nil, &repairFailure{status: http.StatusConflict, code: CodeStale, message: repairStaleWords}
	}
	read, err := s.readTrail(ctx)
	if err != nil {
		s.log.Printf("desk: the trail could not be read again to repair it: %v", err)
		return nil, &repairFailure{status: http.StatusConflict, code: CodeBadRequest, message: "Nothing was repaired: Desk could not read the trail again: " + trailProblem(err) + "."}
	}
	if !hmac.Equal([]byte(s.repairToken(nonce, now.Report, line, read)), []byte(token)) {
		return nil, &repairFailure{status: http.StatusConflict, code: CodeStale, message: repairStaleWords}
	}

	// **The nonce is spent before the runtime is asked, whatever it answers**:
	// the token confirms this attempt and no other.
	if s.repairNonces == nil {
		s.repairNonces = map[string]bool{}
	}
	s.repairNonces[nonce] = true

	// **The repair, once. From here, the request going away stops nothing**:
	// a repair killed in the middle could leave a discontinuity record whose
	// own write did not complete, which no repair mends. The run keeps
	// runRuntime's own bound.
	out, runErr = runRuntime(context.WithoutCancel(ctx), s.cfg.JpackBin, project, "audit", "repair", "--config", runtimeConfigName, "--format", "json")
	answer, failure := readRepaired(out, runErr)
	if failure != nil {
		return nil, failure
	}

	// **Held to the bytes Desk read.** Another writer can append between
	// Desk's reading and the runtime's: where the line the runtime kept as
	// damaged is not the one read, it is not the repair confirmed.
	if repaired := answer.Discontinuity; repaired.DamagedLine != line || repaired.Bytes != read.tail || repaired.Digest != read.tailDigest {
		s.log.Printf("desk: the runtime repaired line %d (%d bytes, %s) of desk %s's trail, and Desk had read line %d (%d bytes, %s)",
			repaired.DamagedLine, repaired.Bytes, repaired.Digest, s.signingKeyName(), line, read.tail, read.tailDigest)
		return nil, &repairFailure{status: http.StatusConflict, code: CodeBadRequest, message: repairDiffersWords}
	}
	return answer, nil
}

// repairWire is `audit repair --format json` as it arrives.
type repairWire struct {
	OutputVersion string                  `json:"outputVersion"`
	Command       string                  `json:"command"`
	Status        string                  `json:"status"`
	Discontinuity *wireAuditDiscontinuity `json:"discontinuity"`
	Diagnostics   []runtimeDiagnostic     `json:"diagnostics"`
}

// readRepaired reads what `audit repair` printed, whatever the exit, under
// outputVersion "2".
//
//   - Exit 0, "audit repair" and "repaired", with a discontinuity record of
//     every member, after the line it names as damaged: the repair.
//   - Any other non-zero exit with "error" or "unsupported" and the
//     runtime's own diagnostics: its refusal, passed on in its words.
//
// Anything else is not an answer the runtime documents, and Desk cannot say
// whether it repaired the trail.
func readRepaired(out []byte, runErr error) (*repairAnswer, *repairFailure) {
	undocumented := &repairFailure{status: http.StatusInternalServerError, code: CodeInternal, message: repairUndocumented}
	code := 0
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			// It did not finish within its bound, or answered more than Desk
			// reads: whatever it did, Desk did not see it.
			return nil, &repairFailure{status: http.StatusInternalServerError, code: CodeInternal,
				message: "The runtime's audit repair did not finish as asked: " + strings.TrimRight(runErr.Error(), ".") + repairCheckAgainWords}
		}
		code = exit.ExitCode()
	}
	var got repairWire
	if out == nil || json.Unmarshal(out, &got) != nil || got.OutputVersion != repairOutputVersion {
		return nil, undocumented
	}
	switch {
	case code == 0 && got.Command == auditRepairCommand && got.Status == "repaired" && got.Discontinuity != nil:
		if repaired, ok := readDiscontinuity(got.Discontinuity); ok {
			return &repairAnswer{State: "repaired", Discontinuity: repaired}, nil
		}
	case code > 0 && (got.Status == "error" || got.Status == "unsupported") && (auditVerification{Diagnostics: got.Diagnostics}).said():
		return nil, &repairFailure{status: http.StatusConflict, code: CodeBadRequest, message: repairRefusedWords, diagnostics: got.Diagnostics}
	}
	return nil, undocumented
}

// readDiscontinuity is the discontinuity record a repair reports, where it
// has every member, names a damaged line of at least one byte, follows it, and
// gives that line's digest in the runtime's form.
func readDiscontinuity(wire *wireAuditDiscontinuity) (auditDiscontinuity, bool) {
	p := &auditPresence{}
	read := auditDiscontinuity{
		Line: p.count(wire.Line), Reason: p.text(wire.Reason, true), DamagedLine: p.count(wire.DamagedLine),
		Bytes: p.count(wire.Bytes), Digest: p.text(wire.Digest, true),
	}
	if p.missing || read.DamagedLine < 1 || read.Line <= read.DamagedLine || read.Bytes < 1 || !recordForm.MatchString(read.Digest) {
		return auditDiscontinuity{}, false
	}
	return read, true
}
