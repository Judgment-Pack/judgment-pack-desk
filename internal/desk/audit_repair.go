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
// # The owner's confirmation
//
// The offer carries a token: a MAC, under this desk's own review key, over
// what the report showed of the trail, its identity, the incomplete line and
// its size as read. A repair runs only where a fresh `audit verify`, run when
// the confirmation arrives, gives the same token (`repairTrail`). A token
// from another desk, a token replayed after the repair it confirmed, and a
// token for a trail that changed since, by identity, line or size, are each
// refused with a plain sentence, and nothing runs.
//
// # The repair
//
// `jpack audit repair --config jpack.json --format json`, through
// `runRuntime`, in the directory this desk holds, once. The runtime takes the
// trail's own lock for it; Desk takes no lock on the trail. One Desk process
// runs one repair of a desk at a time (`repairMu`). Once it is started, the
// request going away does not stop it: a repair cut short could leave a
// discontinuity record whose own write did not complete, which no repair
// mends. Its answer is read as runtime 0.27.1 gives it, under outputVersion
// "2"; its refusal is passed on in its own words, with no path. The page then
// checks the decision record again (ADR-0010, section 4, "When it runs").

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os/exec"
	"slices"
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
)

// repairOlderWords is what a confirmation says where the runtime has no
// `audit repair`: its version, and the first release with it.
func repairOlderWords(version string) string {
	return fmt.Sprintf("Nothing was repaired: the runtime this Desk runs (jpack %s) does not read configVersion 6 and has no audit repair. A runtime of %s or later has it.", version, auditRuntimeFloor)
}

// auditRepair is the decision record's offer of `audit repair`: the
// incomplete line the report names, and the token that confirms the repair of
// the trail as the report read it.
type auditRepair struct {
	Line  int64  `json:"line"`
	Token string `json:"token"`
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

// repairOffer is the offer of a repair, where report names the finding
// incomplete-last-line, and nil everywhere else.
func (s *Server) repairOffer(report *auditReport) *auditRepair {
	if report == nil {
		return nil
	}
	for _, finding := range report.Findings {
		if finding.Name == incompleteLastLine {
			return &auditRepair{Line: finding.Line, Token: s.repairToken(report.Trail, finding.Line, report.Bytes)}
		}
	}
	return nil
}

// repairToken binds a confirmation to this desk, by its own review key, and
// to the trail as a report read it: its identity, the incomplete line, and
// its size.
func (s *Server) repairToken(trail string, line, size int64) string {
	payload, _ := json.Marshal(struct {
		Purpose string `json:"purpose"`
		Trail   string `json:"trail"`
		Line    int64  `json:"line"`
		Bytes   int64  `json:"bytes"`
	}{
		Purpose: repairPurpose,
		Trail:   trail,
		Line:    line,
		Bytes:   size,
	})
	mac := hmac.New(sha256.New, s.reviewKey[:])
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
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
	if err != nil || decodeDataJSON(data, &request) != nil || len(request.Token) != 64 {
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
// the runtime has `audit repair`; whether the project keeps a trail; a fresh
// `audit verify`, whose offer must carry the same token; and then `audit
// repair`, once. The caller holds repairMu.
func (s *Server) repairTrail(ctx context.Context, project heldDir, token string) (*repairAnswer, *repairFailure) {
	nothing := func(status int, code string, err error) (*repairAnswer, *repairFailure) {
		return nil, &repairFailure{status: status, code: code, message: "Nothing was repaired: " + strings.TrimRight(err.Error(), ".") + "."}
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

	// **The trail as it is now, in the runtime's word.** The token was given
	// for a report; the repair runs only where a report of the trail now gives
	// the same one.
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
	offer := s.repairOffer(now.Report)
	if offer == nil || !hmac.Equal([]byte(offer.Token), []byte(token)) {
		return nil, &repairFailure{status: http.StatusConflict, code: CodeStale, message: repairStaleWords}
	}

	// **The repair, once. From here, the request going away stops nothing**:
	// a repair killed in the middle could leave a discontinuity record whose
	// own write did not complete, which no repair mends. The run keeps
	// runRuntime's own bound.
	out, runErr = runRuntime(context.WithoutCancel(ctx), s.cfg.JpackBin, project, "audit", "repair", "--config", runtimeConfigName, "--format", "json")
	return readRepaired(out, runErr)
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
