package desk

// The decision-record panel (ADR-0010, sections 4, 6 and 8).
//
// The panel shows what the runtime's own `jpack audit verify` finds in this
// desk's trail. Desk forms no verdict of its own: it passes through the
// report's status, coverage, segments, discontinuities and findings, and the
// runtime's own sentences about what the result establishes and what it does
// not.
//
// **It runs with the keys and checkpoints Desk keeps, and nothing else
// held.** On a desk Desk keeps a key for, each public key in its list,
// `<name>.keys.jsonl`, in order, as `--public-key` (signing.go): a desk Desk
// made, and the project Desk was started on where its upgrade made one
// (startup_key.go). For each holder Desk handed checkpoints
// of the current trail over to, the file of those it kept, as `--expect`
// (handover.go). Where the owner set a time-stamping authority, its roots as
// `--tsa-roots`, and its policies and revocation lists as `--tsa-policy` and
// `--tsa-crls` (stamping.go). Never a `--require-…` flag: those are a
// reader's demands, not the operator's. The runtime then checks the chain,
// the signatures against the keys it was given, the trail against the
// checkpoints it was given, and the stamps against the roots it was given,
// and says so in its own sentences.
//
// **Beside it, the runtime's word on the key** (ADR-0010, section 1): `packs
// validate`'s `audit-signing-key` check, which says whether the key the
// project names signs its records, or no check where it names none; and the
// public keys Desk keeps for the desk, for the owner to hand to a holder.
//
// **It runs only where the runtime has the command.** `packs schema` names the
// configuration versions the runtime reads, and "6" is the sign: the first
// release that reads it has every audit command. A `packs schema` that fails
// is an error, never taken for an older runtime. An `audit verify` that the
// command parser says does not exist (exit 3, and nothing but its words for
// that) is taken as the same absence. With either, the page says one sentence
// and runs nothing more. Every other refusal is the runtime's, shown in its
// words.
//
// **It runs where the review runs**, through `runRuntime`, in the directory
// this desk holds, with `--config jpack.json` named: never under a
// JPACK_CONFIG that names another project, which is refused as the review
// refuses it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

const (
	// auditConfigVersion is the configuration version whose presence among
	// a runtime's `supportedConfigVersions` says it has the audit commands:
	// the first release that reads it has every one of them (ADR-0010,
	// section 6).
	auditConfigVersion = "6"
	// auditRuntimeFloor is that release.
	auditRuntimeFloor = "0.26.0"
	// auditVerifyCommand is the command's name as its JSON answer gives it.
	auditVerifyCommand = "audit verify"
)

// What the panel can say: the runtime's report; the runtime's refusal to make
// one; that the runtime has no audit commands; or that the project keeps no
// trail.
const (
	auditStateReport     = "report"
	auditStateUnverified = "unverified"
	auditStateOlder      = "older-runtime"
	auditStateNoTrail    = "no-trail"
)

// auditCoverageState is one protection's reach, as runtime 0.26.0 reports it
// (`result.AuditCoverageState`).
type auditCoverageState struct {
	Status  string `json:"status"`
	Through int64  `json:"through,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// auditCoverage is how much of the trail each kind of protection reaches, as
// runtime 0.26.0 reports it (`result.AuditCoverage`).
type auditCoverage struct {
	LegacyPrefix    int64              `json:"legacyPrefix"`
	Chained         int64              `json:"chained"`
	Unchained       int64              `json:"unchained"`
	Uncovered       int64              `json:"uncovered"`
	Damaged         int64              `json:"damaged"`
	Signed          auditCoverageState `json:"signed"`
	SignedRecords   int64              `json:"signedRecords"`
	UnsignedRecords int64              `json:"unsignedRecords"`
	Checkpointed    auditCoverageState `json:"checkpointed"`
	Witnessed       int64              `json:"witnessed"`
	Unwitnessed     int64              `json:"unwitnessed"`
	Stamped         auditCoverageState `json:"stamped"`
}

type auditSegment struct {
	FirstLine int64 `json:"firstLine"`
	LastLine  int64 `json:"lastLine"`
}

type auditDiscontinuity struct {
	Line        int64  `json:"line"`
	Reason      string `json:"reason"`
	DamagedLine int64  `json:"damagedLine"`
	Bytes       int64  `json:"bytes"`
	Digest      string `json:"digest"`
}

type auditFinding struct {
	Name   string `json:"name"`
	Line   int64  `json:"line"`
	Detail string `json:"detail"`
}

// auditSignatures is the signature sidecar as a verification read it, by
// runtime 0.26.0's own member names: its lines, those of no shape it reads,
// the rotations followed, the keys and revocations supplied, and the first
// key and the key in force at the end, by keyId.
type auditSignatures struct {
	Lines        int64  `json:"lines"`
	Unreadable   int64  `json:"unreadable"`
	Rotations    int64  `json:"rotations"`
	KeysSupplied int64  `json:"keysSupplied"`
	Revocations  int64  `json:"revocations"`
	FirstKey     string `json:"firstKey"`
	KeyInForce   string `json:"keyInForce"`
}

// auditStamps is the stamps file as a verification read it, by runtime
// 0.27.1's own member names: its lines, those of no shape it reads, the stamps
// that hold under the roots supplied and match the trail ("trusted", the
// runtime's word), how many of those had their certificates' revocation
// checked against a supplied list and how many did not, the time the stamped
// records existed by, and the lag between records' `at` and their first such
// stamp.
type auditStamps struct {
	Lines                int64         `json:"lines"`
	Unreadable           int64         `json:"unreadable"`
	Trusted              int64         `json:"trusted"`
	RevocationChecked    int64         `json:"revocationChecked"`
	RevocationNotChecked int64         `json:"revocationNotChecked"`
	CoveredBy            string        `json:"coveredBy,omitempty"`
	Lag                  auditStampLag `json:"lag"`
}

// auditStampLag is the runtime's lag between each covered record's `at`, the
// operator's word, and the time the first stamp covering it attests: over
// Records records, the longest and its record, the shortest and its record;
// whether some record's `at` is later than that time; and how many covered
// records' `at` could not be read.
type auditStampLag struct {
	Records      int64   `json:"records"`
	MaxSeconds   float64 `json:"maxSeconds"`
	MaxSequence  int64   `json:"maxSequence,omitempty"`
	MinSeconds   float64 `json:"minSeconds"`
	MinSequence  int64   `json:"minSequence,omitempty"`
	AtAfterStamp bool    `json:"atAfterStamp"`
	AtUnreadable int64   `json:"atUnreadable"`
}

// auditReport is the part of `audit verify --format json` the panel shows,
// by runtime 0.26.0's own member names (`result.AuditVerification`). The
// lists hold at most the runtime's first hundred of each; the totals count
// every one.
type auditReport struct {
	// Status is the runtime's: "valid", "segmented" or "invalid".
	Status                string               `json:"status"`
	Lines                 int64                `json:"lines"`
	Bytes                 int64                `json:"bytes"`
	SnapshotBetweenWrites bool                 `json:"snapshotBetweenWrites"`
	Coverage              auditCoverage        `json:"coverage"`
	Segments              []auditSegment       `json:"segments"`
	SegmentsTotal         int64                `json:"segmentsTotal"`
	Discontinuities       []auditDiscontinuity `json:"discontinuities"`
	DiscontinuitiesTotal  int64                `json:"discontinuitiesTotal"`
	Findings              []auditFinding       `json:"findings"`
	FindingsTotal         int64                `json:"findingsTotal"`
	// Signatures is the signature sidecar as the runtime read it, given
	// where a public key was passed (`result.AuditSignatures`).
	Signatures *auditSignatures `json:"signatures,omitempty"`
	// Stamps is the stamps file as the runtime read it, given where
	// time-stamping roots were passed (`result.AuditStamps`).
	Stamps *auditStamps `json:"stamps,omitempty"`
	// Establishes and DoesNotEstablish are the runtime's own sentences, in
	// English, passed through as it wrote them.
	Establishes      []string `json:"establishes"`
	DoesNotEstablish []string `json:"doesNotEstablish"`
	// Trail is the trail's identity as the runtime read it, where it read
	// one: a trail with no chained line has none. The repair's token binds
	// it (audit_repair.go), and the page is given it, so that it tells the
	// checkpoint a stamp run named of this trail from one of another
	// (Stamping.tsx; line audit, finding 5).
	Trail string `json:"trail,omitempty"`
	// head is the report's last chained record as the runtime names it,
	// trail, sequence and record digest, where its member is a checkpoint of
	// the runtime's shape; it is kept for the stamping scheduler, and the page
	// is not given it.
	head checkpointLine
}

// chainedAfter is how many chained records of report's trail follow the
// record at through, where the report says it, and records true; or, where it
// does not, how many lines follow it through the trail's last chained line,
// and records false. A line a repair names as damaged, and an unchained line
// a later chained one commits to, are lines and not records (line audit,
// finding 7), so a count is never one sequence taken from another, except
// where the report says no line of either kind is there:
//
//   - after no record, every chained record (`coverage.chained`);
//   - after the record the held checkpoints reach (`checkpointed.through`),
//     the records none of them witnesses (`coverage.unwitnessed`), the
//     runtime's own count;
//   - where the trail holds no damaged and no unchained line, every line
//     after it through the last chained one, which is the trail's lines less
//     the unchained ones after the last chained line (`coverage.uncovered`).
func chainedAfter(report *auditReport, through int64) (count int64, records bool) {
	c := report.Coverage
	last := report.Lines - c.Uncovered
	switch {
	case through == 0:
		return c.Chained, true
	case c.Checkpointed.Status == "through" && c.Checkpointed.Through == through:
		return c.Unwitnessed, true
	case c.Damaged == 0 && c.Unchained == 0:
		return max(last-through, 0), true
	}
	return max(last-through, 0), false
}

// handedSince is one holder's count after the last record handed over to it
// (`chainedAfter`), of the trail the report is of: Records where the report
// says how many chained records follow it, and Lines, in its place, where it
// does not.
type handedSince struct {
	Holder  string `json:"holder"`
	Trail   string `json:"trail"`
	Through int64  `json:"through"`
	Records *int64 `json:"records,omitempty"`
	Lines   *int64 `json:"lines,omitempty"`
}

// sinceHandedOver is, for each holder whose checkpoints were passed, what
// follows the last record handed over to it, as report says it: nothing where
// there is no report, or the report is of another trail than the one whose
// checkpoints were passed.
func sinceHandedOver(expect heldExpectations, report *auditReport) []handedSince {
	if report == nil || report.Trail == "" || report.Trail != expect.trail {
		return nil
	}
	var since []handedSince
	for _, held := range expect.handed {
		entry := handedSince{Holder: held.holder, Trail: expect.trail, Through: held.through}
		entry.Records, entry.Lines = countedAs(chainedAfter(report, held.through))
		since = append(since, entry)
	}
	return since
}

// auditAnswer is what `GET /api/audit/verify` answers.
type auditAnswer struct {
	// State is "report", "unverified" (the runtime refused to verify, and
	// Diagnostics say why), "older-runtime" or "no-trail".
	State string `json:"state"`
	// Runtime is the runtime's own version, as `packs schema` reports it,
	// and Floor the first release with the audit commands.
	Runtime     string              `json:"runtime,omitempty"`
	Floor       string              `json:"floor,omitempty"`
	Report      *auditReport        `json:"report,omitempty"`
	Diagnostics []runtimeDiagnostic `json:"diagnostics,omitempty"`
	// Files is which of the trail, its signature sidecar and its stamps the
	// audit directory holds for download, by the names the download takes:
	// "evaluations", "signatures" and "stamps". It is given with a report and
	// with the runtime's refusal, and with nothing else.
	Files []string `json:"files,omitempty"`
	// Keys and Signing are given with a report and with the runtime's
	// refusal, and with nothing else.
	Keys    *auditKeys    `json:"keys,omitempty"`
	Signing *auditSigning `json:"signing,omitempty"`
	// Rotation is whether the owner can rotate the desk's signing key now,
	// with the token that confirms it, or why not (rotation.go). It is given
	// with a report and with the runtime's refusal, and with nothing else.
	Rotation *auditRotation `json:"rotation,omitempty"`
	// Identity is the owner's choice on this project's identity, where it is
	// unresolved (issue #309, startup_identity.go): given with every answer,
	// whatever the trail's state (review round 1 of #315), and beside the
	// route's refusals too.
	Identity *identityOffer `json:"identity,omitempty"`
	// Expected is how many holders' files of checkpoints were passed as
	// `--expect`, and ExpectUnread the labels of the holders whose file could
	// not be read now, or is not ours, and was passed over (handover.go).
	// Each is given with a report and with the runtime's refusal, where it
	// is not zero or empty.
	Expected     int      `json:"expected,omitempty"`
	ExpectUnread []string `json:"expectUnread,omitempty"`
	// Since is, for each holder whose checkpoints were passed, the chained
	// records after the last one handed over to it, or the lines where the
	// report does not say (`sinceHandedOver`). It is given with a report,
	// and on the decision record alone.
	Since []handedSince `json:"since,omitempty"`
	// HandoverProblem is why Desk passed none of the checkpoints it handed
	// over, where it could not read its record of hand-overs, or tell which
	// trail is current: never the same as keeping none.
	HandoverProblem string `json:"handoverProblem,omitempty"`
	// Repair is the offer of `audit repair`, with the token that confirms it,
	// given only with a report that names the finding incomplete-last-line,
	// on the decision record and nowhere else (audit_repair.go).
	Repair *auditRepair `json:"repair,omitempty"`
	// Stamping is this desk's stamping: its settings, whether their roots
	// were passed, the records pending a stamp and the last stamp run
	// (stamping.go). It is given with a report and with the runtime's
	// refusal, on the decision record and nowhere else.
	Stamping *auditStamping `json:"stamping,omitempty"`
}

// What the panel holds of the desk's keys.
const (
	// keysKept: Desk keeps a key for this desk, Public lists its public keys,
	// and each was passed to `audit verify`, in order.
	keysKept = "kept"
	// keysNone: a desk Desk made, for which it keeps no key: made unsigned,
	// or before Desk kept keys.
	keysNone = "none"
	// keysStartup: the project Desk was started on, for which Desk keeps no
	// key: the upgrade that offers one was not taken.
	keysStartup = "startup"
	// keysUnread: Desk could not read or pass the keys it keeps, Problem says
	// why, and none was passed.
	keysUnread = "unread"
)

// auditKeys is the public keys Desk keeps for this desk, as the panel shows
// them and as it passed them.
type auditKeys struct {
	State   string          `json:"state"`
	Public  []deskPublicKey `json:"public,omitempty"`
	Problem string          `json:"problem,omitempty"`
}

// What the panel can say of `packs validate`'s `audit-signing-key` check.
const (
	// signingChecked: the runtime reported the check, with its status and its
	// words.
	signingChecked = "check"
	// signingNoKey: it reported no such check, which it does only where no key
	// is named, by the configuration or by JPACK_SIGNING_KEY.
	signingNoKey = "no-key"
	// signingUnread: it did not say; Diagnostics or Problem say why.
	signingUnread = "unread"
	// auditSigningKeyCheck is the check's name (runtime 0.26.0,
	// `project.CheckAuditSigningKey`).
	auditSigningKeyCheck = "audit-signing-key"
)

// auditSigning is `packs validate`'s word on whether the key named for this
// project signs its records. Status is the runtime's: "passed", "failed" or
// "skipped"; Detail is its sentence, in English.
type auditSigning struct {
	State       string              `json:"state"`
	Status      string              `json:"status,omitempty"`
	Detail      string              `json:"detail,omitempty"`
	Diagnostics []runtimeDiagnostic `json:"diagnostics,omitempty"`
	Problem     string              `json:"problem,omitempty"`
}

// auditRuntime is the directory and refusal for running the audit commands
// over this desk's project: the review's own, in the panel's words.
//
// **The refusal names the variable, never its value.** JPACK_CONFIG holds a
// path, and a path where Desk was started says where the owner keeps their
// files. The panel and the trail's download say which setting refuses them
// and why; the review's older wording, which quotes the value, is unchanged.
func (s *Server) auditRuntime() (heldDir, string) {
	dir, named, ok := s.projectRuntime()
	switch {
	case !ok:
		return heldDir{}, "This desk holds no project whose decision record Desk can check."
	case named != "":
		return dir, "This project's runtime reads the configuration that JPACK_CONFIG names where Desk was started, and not this project's " + runtimeConfigName + ", so Desk does not check its decision record here."
	}
	return dir, ""
}

// handleAuditVerify answers `GET /api/audit/verify`. It runs only when asked:
// the page asks when the panel opens, and when the owner asks again.
func (s *Server) handleAuditVerify(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	// **The owner's question on this project's identity is its own member**
	// (review round 1 of #315, finding 5): given with every answer of this
	// route, a refusal and a project that keeps no trail among them, since
	// the owner's word on it is needed whether or not a trail is checked.
	identity := s.identityOfferNow()
	refuse := func(status int, code, message string) {
		body := map[string]any{"error": message, "code": code}
		if identity != nil {
			body["identity"] = identity
		}
		writeJSON(w, status, body)
	}
	dir, refusal := s.auditRuntime()
	if refusal != "" {
		refuse(http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	answer, err := s.auditVerify(r.Context(), dir)
	if err != nil {
		s.log.Printf("desk: the decision record could not be checked: %v", err)
		refuse(http.StatusInternalServerError, CodeInternal, "The decision record could not be checked: "+strings.TrimRight(s.withoutPaths(err.Error()), ".")+".")
		return
	}
	answer.Identity = identity
	shown := s.withoutPathsIn(answer)
	if before, after := mustJSON(answer), mustJSON(shown); before != after {
		// The page is told no path; the owner's own log keeps them.
		s.log.Printf("desk: the decision record, as the runtime said it: %s", before)
	}
	writeJSON(w, http.StatusOK, shown)
}

// mustJSON is v as JSON, for comparing two answers and for the log.
func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

// withoutPaths is message with no path from the root of a file system in it,
// for an answer that may name this project's audit directory
// (withoutPathsUnder, with the audit directory jpack.json declares now).
func (s *Server) withoutPaths(message string) string {
	auditDir, _, _ := s.projectAuditDir()
	return s.withoutPathsUnder(message, auditDir)
}

// withoutPathsUnder is message with no path from the root of a file system
// in it, where auditDir is the audit directory jpack.json declares.
//
//  1. **The audit directory, whole.** The runtime names its trail as the
//     folder it ran in joined with audit.dir, whatever audit.dir holds: an
//     absolute path is joined on, and ".." is resolved, so a directory
//     outside the project is named by where it is. Desk joins audit.dir the
//     same way to each name it has for that folder (the configured path, the
//     resolved one, and a /proc/self/fd/N the runtime entered it through),
//     and takes an absolute audit.dir on its own too. Each such span is
//     replaced whole, longest first, before anything else, so a space or a
//     parenthesis in it cannot end the replacement early.
//  2. **The paths this desk was configured with:** JPACK_CONFIG's value by
//     the variable's name, the runtime binary by its file name, and the
//     project's folder by "the project's folder"; an inherited
//     JPACK_SIGNING_KEY's value, the key Desk keeps for this desk, its list
//     of public keys and every folder on the way to them by "…"
//     (`custodySpans`). These and the spans of step 1 are replaced in one
//     pass, longest first.
//  3. **Every other path from a root**, Unix or a drive letter, by "…",
//     keeping a last name only where it is one of the runtime's own files
//     (withoutAbsolutePaths).
//
// **It is the one way a message reaches the page from the panel or the
// download:** Desk's own errors, and every sentence of the runtime's that they
// pass on (withoutPathsIn). So an answer names a setting and why, and never
// says where the owner keeps their files. The log keeps the message whole.
func (s *Server) withoutPathsUnder(message, auditDir string) string {
	const held = "\x00"
	message = strings.ReplaceAll(message, held, "")
	var spans []pathSpan
	for _, span := range s.auditDirSpans(message, auditDir) {
		spans = append(spans, pathSpan{value: span, with: held})
	}
	// An inherited JPACK_SIGNING_KEY names where a secret is kept: whole, and
	// every folder on the way to it, as the runtime prints each too.
	if key := strings.TrimSpace(os.Getenv(runtimeSigningKeyEnv)); filepath.IsAbs(key) {
		spans = append(spans, pathSpans(key, false, held)...)
	}
	// The key Desk keeps for this desk, its list, and every folder on the way
	// to them, the home folder among them.
	spans = append(spans, s.custodySpans(s.signingKeyName(), held)...)
	for _, path := range []struct{ value, name string }{
		{strings.TrimSpace(os.Getenv(runtimeConfigEnv)), runtimeConfigEnv},
		{s.cfg.JpackBin, filepath.Base(s.cfg.JpackBin)},
		{s.projectDir, "the project's folder"},
		{s.cfg.ProjectDir, "the project's folder"},
		// And as the runtime prints it, a control or separator character as
		// "?" (`displayedPath`).
		{displayedPath(s.projectDir), "the project's folder"},
		{displayedPath(s.cfg.ProjectDir), "the project's folder"},
	} {
		if path.value != "" && path.value != path.name && filepath.IsAbs(path.value) {
			spans = append(spans, pathSpan{value: path.value, with: path.name})
		}
	}
	// One pass, longest first (`replaceSpans`): the audit directory before
	// the project's folder it is in, and the project's folder before
	// Desk's configuration folder or the home folder it is in.
	return strings.ReplaceAll(withoutAbsolutePaths(replaceSpans(message, spans)), held, "…")
}

// procFD is the name the runtime has for the folder it was started in where
// Desk enters it through a descriptor it holds: on Linux the trampoline's
// `cd /proc/self/fd/3/.` (project_linux.go), so the runtime's own working
// directory is runtimeTrampolineDir. Any other such name a sentence holds is
// taken too.
var procFD = regexp.MustCompile(`/proc/self/fd/[0-9]+`)

// runtimeTrampolineDir is the working directory the trampoline gives the
// runtime. An audit.dir that climbs out is joined through it, so
// "../x" is named /proc/self/fd/x, with no descriptor number left to find.
const runtimeTrampolineDir = "/proc/self/fd/3"

// auditDirSpans is every way a sentence can name the audit directory
// auditDir: joined to each name of the folder the runtime ran in, and on its
// own where it is absolute. Longest first, so a shorter span never cuts a
// longer one.
func (s *Server) auditDirSpans(message, auditDir string) []string {
	if strings.TrimSpace(auditDir) == "" {
		return nil
	}
	bases := []string{s.projectDir, s.cfg.ProjectDir, runtimeTrampolineDir}
	if real, err := filepath.EvalSymlinks(s.projectDir); err == nil {
		bases = append(bases, real)
	}
	bases = append(bases, procFD.FindAllString(message, -1)...)
	var spans []string
	for _, base := range bases {
		if base != "" {
			spans = append(spans, filepath.Join(base, auditDir))
		}
	}
	if filepath.IsAbs(auditDir) {
		spans = append(spans, filepath.Clean(auditDir), auditDir)
		if real, err := filepath.EvalSymlinks(auditDir); err == nil {
			spans = append(spans, real)
		}
	}
	for _, span := range spans {
		if shown := displayedPath(span); shown != span {
			spans = append(spans, shown)
		}
	}
	slices.SortFunc(spans, func(a, b string) int { return len(b) - len(a) })
	return slices.Compact(spans)
}

// displayedPath is path as the runtime prints it in a sentence. It is the
// runtime's own rule, read from its source and not measured case by case
// (runtime 0.26.0, `internal/display/sanitize.go`, `Sanitize`): a control
// character (Cc), a format character (Cf), a line separator (Zl) or a
// paragraph separator (Zp) is printed as "?", and every other character as
// it is. A runtime that changes that rule needs this to change with it.
func displayedPath(path string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return '?'
		}
		return r
	}, path)
}

// pathInMessage is a path from the root of a file system that a message
// names: "/" or a drive letter, at the start or after a space, a double
// quote, a parenthesis, a bracket or "=", up to the next space, quote,
// parenthesis or bracket. Not after a single quote: the runtime's schema
// diagnostics quote JSON pointers so ('/audit/chain'), and those are not
// paths. Not after a colon either, so a URL's "//" is left as it is.
var pathInMessage = regexp.MustCompile(`(?:^|[\s"(=\[])((?:/|[A-Za-z]:[\\/])[^\s"'()\[\]]*)`)

// runtimeFileNames are the runtime's own files, whose names a redacted path
// keeps: which file a sentence is about is what makes it useful.
var runtimeFileNames = []string{"evaluations.jsonl", "signatures.jsonl", "stamps.jsonl", runtimeConfigName, runtimeLockName}

// pathToRuntimeFile is a path from a root that ends in one of the runtime's
// own files' names, however many spaces or "?" it holds on the way: the form
// in which the runtime names its trail and the files beside it. It starts
// where pathInMessage does, and may not cross a line, a double quote, a colon
// or a semicolon, so it does not run from one path, over the clause between, to
// another.
var pathToRuntimeFile = regexp.MustCompile(`(?:^|[\s"(=\[])((?:/|[A-Za-z]:[\\/])[^\n":;]*?[/\\](?:evaluations\.jsonl|signatures\.jsonl|stamps\.jsonl|jpack\.lock\.json|jpack\.json))(?:$|[\s"'),.;:])`)

// withoutAbsolutePaths is message with each absolute path replaced by "…",
// or by "…/" and its last name where that is one of runtimeFileNames.
// Punctuation that ends a sentence after a path stays. A path that ends in a
// runtime file's name is taken whole first, spaces and all
// (pathToRuntimeFile); then every other path, up to its first space
// (pathInMessage).
func withoutAbsolutePaths(message string) string {
	return withoutPathsMatching(withoutPathsMatching(message, pathToRuntimeFile), pathInMessage)
}

// withoutPathsMatching is message with each path pattern's first group
// matches replaced as withoutAbsolutePaths says.
func withoutPathsMatching(message string, pattern *regexp.Regexp) string {
	var out strings.Builder
	last := 0
	for _, match := range pattern.FindAllStringSubmatchIndex(message, -1) {
		start, end := match[2], match[3]
		path := strings.TrimRight(message[start:end], ".,:;")
		out.WriteString(message[last:start])
		base := path[strings.LastIndexAny(path, `/\`)+1:]
		if slices.Contains(runtimeFileNames, base) {
			out.WriteString("…/" + base)
		} else {
			out.WriteString("…")
		}
		last = start + len(path)
	}
	out.WriteString(message[last:])
	return out.String()
}

// withoutPathsIn is answer with every sentence in it passed through
// withoutPaths: the runtime's diagnostics, and its report's findings,
// details, reasons and the sentences of what it establishes.
func (s *Server) withoutPathsIn(answer auditAnswer) auditAnswer {
	auditDir, _, _ := s.projectAuditDir()
	clean := func(message string) string { return s.withoutPathsUnder(message, auditDir) }
	answer = withoutPathsInKeys(answer, clean)
	answer.HandoverProblem = clean(answer.HandoverProblem)
	if answer.Stamping != nil {
		stamping := *answer.Stamping
		stamping.Problem = clean(stamping.Problem)
		stamping.PassProblem = clean(stamping.PassProblem)
		if stamping.Last != nil {
			last := *stamping.Last
			last.Problem = clean(last.Problem)
			if last.Diagnostics != nil {
				said := make([]runtimeDiagnostic, len(last.Diagnostics))
				for i, diagnostic := range last.Diagnostics {
					said[i] = runtimeDiagnostic{Code: diagnostic.Code, Message: clean(diagnostic.Message)}
				}
				last.Diagnostics = said
			}
			stamping.Last = &last
		}
		if stamping.LastChecked != nil {
			checked := *stamping.LastChecked
			checked.Reason = clean(checked.Reason)
			stamping.LastChecked = &checked
		}
		answer.Stamping = &stamping
	}
	if answer.Diagnostics != nil {
		said := make([]runtimeDiagnostic, len(answer.Diagnostics))
		for i, diagnostic := range answer.Diagnostics {
			said[i] = runtimeDiagnostic{Code: diagnostic.Code, Message: clean(diagnostic.Message)}
		}
		answer.Diagnostics = said
	}
	if answer.Report == nil {
		return answer
	}
	report := *answer.Report
	for _, state := range []*auditCoverageState{&report.Coverage.Signed, &report.Coverage.Checkpointed, &report.Coverage.Stamped} {
		state.Detail = clean(state.Detail)
	}
	report.Discontinuities = slices.Clone(report.Discontinuities)
	for i := range report.Discontinuities {
		report.Discontinuities[i].Reason = clean(report.Discontinuities[i].Reason)
	}
	report.Findings = slices.Clone(report.Findings)
	for i := range report.Findings {
		report.Findings[i].Detail = clean(report.Findings[i].Detail)
	}
	report.Establishes = sentencesWithoutPaths(clean, report.Establishes)
	report.DoesNotEstablish = sentencesWithoutPaths(clean, report.DoesNotEstablish)
	answer.Report = &report
	return answer
}

// withoutPathsInKeys is the panel's words on the keys and on the runtime's
// check of them, passed through clean.
func withoutPathsInKeys(answer auditAnswer, clean func(string) string) auditAnswer {
	if answer.Keys != nil {
		keys := *answer.Keys
		keys.Problem = clean(keys.Problem)
		answer.Keys = &keys
	}
	if answer.Signing != nil {
		signing := *answer.Signing
		signing.Detail = clean(signing.Detail)
		signing.Problem = clean(signing.Problem)
		if signing.Diagnostics != nil {
			said := make([]runtimeDiagnostic, len(signing.Diagnostics))
			for i, diagnostic := range signing.Diagnostics {
				said[i] = runtimeDiagnostic{Code: diagnostic.Code, Message: clean(diagnostic.Message)}
			}
			signing.Diagnostics = said
		}
		answer.Signing = &signing
	}
	if answer.Rotation != nil {
		rotation := *answer.Rotation
		rotation.Reason = clean(rotation.Reason)
		answer.Rotation = &rotation
	}
	return answer
}

func sentencesWithoutPaths(clean func(string) string, sentences []string) []string {
	if sentences == nil {
		return nil
	}
	out := make([]string, len(sentences))
	for i, sentence := range sentences {
		out[i] = clean(sentence)
	}
	return out
}

// auditDirOf is the audit directory a configuration declares, `audit.dir`,
// and whether it declares one. It reads that member and nothing else: which
// configuration is valid is the runtime's to say.
func auditDirOf(config []byte) (string, bool, error) {
	var declared struct {
		Audit *struct {
			Dir *string `json:"dir"`
		} `json:"audit"`
	}
	if err := json.Unmarshal(config, &declared); err != nil {
		return "", false, err
	}
	if declared.Audit == nil || declared.Audit.Dir == nil || *declared.Audit.Dir == "" {
		return "", false, nil
	}
	return *declared.Audit.Dir, true, nil
}

// projectAuditDir reads this desk's `jpack.json` under the file API's rules,
// and returns the audit directory it declares, if any.
func (s *Server) projectAuditDir() (string, bool, error) {
	config, err := s.readReviewFileWithin(runtimeConfigName, reviewTextLimit)
	if err != nil {
		return "", false, fmt.Errorf("%s could not be read: %w", runtimeConfigName, err)
	}
	dir, declared, err := auditDirOf(config)
	if err != nil {
		return "", false, fmt.Errorf("%s is not a configuration Desk can read: %w", runtimeConfigName, err)
	}
	return dir, declared, nil
}

// auditVerify is the panel's answer: nothing run where the project keeps no
// trail; `packs schema` alone where the runtime reads no "6"; and otherwise
// `packs validate`'s word on the key, and the runtime's `audit verify`, with
// the public keys Desk keeps for the desk and the checkpoints it handed over,
// and nothing else held.
func (s *Server) auditVerify(ctx context.Context, dir heldDir) (auditAnswer, error) {
	if _, declared, err := s.projectAuditDir(); err != nil {
		return auditAnswer{}, err
	} else if !declared {
		return auditAnswer{State: auditStateNoTrail}, nil
	}
	schema, err := readRuntimeSchema(ctx, s.cfg.JpackBin, dir)
	if err != nil {
		return auditAnswer{}, err
	}
	older := auditAnswer{State: auditStateOlder, Runtime: schema.version, Floor: auditRuntimeFloor}
	if !slices.Contains(schema.supported, auditConfigVersion) {
		return older, nil
	}
	signing := s.signingCheck(ctx, dir)
	// Under the desk's key lock, so the panel never reads a rotation half
	// made in this process, and its token names what it read.
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	keys, held := s.heldKeys(ctx, dir)
	defer held.Close()
	rotation := s.rotationOffer(ctx, dir, keys, held)
	args := []string{"audit", "verify", "--config", runtimeConfigName, "--format", "json"}
	if keys.State == keysKept {
		files, remove, err := held.dir.publicKeyFiles(keys.Public)
		if err != nil {
			s.log.Printf("desk: the public keys could not be written for audit verify in %s: %v", held.dir.path, err)
			keys = auditKeys{State: keysUnread, Problem: "Desk could not hand the runtime the public keys it keeps for this desk, so no signature was checked."}
		} else {
			defer func() {
				if err := remove(); err != nil {
					s.log.Printf("desk: the public keys written for audit verify in %s could not all be removed: %v", held.dir.path, err)
				}
			}()
			for _, file := range files {
				args = append(args, "--public-key", file)
			}
		}
	}
	expect := s.heldCheckpoints(ctx, dir)
	args = append(args, expect.args...)
	// The stamping settings' roots, held for reading until the runtime has
	// read them.
	stamping := s.stampingForVerify()
	args = append(args, stamping.args...)
	out, runErr := runRuntime(ctx, s.cfg.JpackBin, dir, args...)
	stamping.done()
	answer, err := readAuditVerification(out, runErr)
	if err != nil {
		return auditAnswer{}, err
	}
	if answer.State == auditStateOlder {
		return older, nil
	}
	answer.Stamping = s.stampingAfterVerify(stamping.view, answer.Report, stamping.since)
	answer.Stamping.LastChecked = lastRunChecked(*answer.Stamping, answer.Report, func(named, head checkpointHead) (*checkpointHead, error) {
		at, err := s.checkpointAt(ctx, dir, named, head)
		if err != nil && !errors.Is(err, errCheckpointsChanged) && !errors.Is(err, errTrailMovedSince) {
			s.log.Printf("desk: the checkpoint the last stamp run of desk %s named could not be asked of the runtime: %v", s.signingKeyName(), err)
		}
		return at, err
	})
	answer.Runtime = schema.version
	answer.Files = s.auditFilesPresent()
	answer.Keys = &keys
	answer.Signing = &signing
	answer.Rotation = &rotation
	answer.Repair = s.repairOffer(ctx, answer.Report)
	answer.Expected = expect.count
	answer.ExpectUnread = expect.unread
	answer.HandoverProblem = expect.problem
	answer.Since = sinceHandedOver(expect, answer.Report)
	return answer, nil
}

// keyReading is what the panel read of the keys Desk keeps for a desk, held
// while it is used: the signing folder; the seed as inspected; the list of
// public keys as read, its file and bytes; the key the runtime read from the
// seed; and the trail's signature sidecar, or why it could not be read.
type keyReading struct {
	dir        *signingDir
	seed       os.FileInfo
	list       keysFile
	keys       []deskPublicKey
	current    deskPublicKey
	sidecar    sidecarReading
	sidecarErr error
}

// Close releases the signing folder. A nil reading holds nothing.
func (k *keyReading) Close() {
	if k != nil {
		k.dir.Close()
	}
}

// heldKeys is the public keys Desk keeps for this desk, and what it read of
// them, held, where there are any. Nothing is made or changed in Desk's
// custody to read them.
//
// **A list of keys is only passed with the key it belongs to.** The list is
// read whole and strictly (`parseDeskKeys`): its first key takes over from 0,
// and each later one from a later sequence. Its last key, the one in force,
// must be the key the runtime reads from the seed Desk keeps for the desk
// (`jpack audit key public <seed> --format json`, run in project, the folder
// the panel's commands run in); Desk never reads the seed's bytes itself. A
// seed with no list, a list with no seed, and a list whose key is not the
// seed's are each said, and no key is passed: only a desk with neither has
// none.
//
// **And with the rotations the trail's sidecar records** (`checkKeysAgainst`).
// Where the signature sidecar can be read, under the trail's lock, each later
// key must be the one its key rotations hand over to, in order, at the
// sequence the list gives. A list that disagrees is said, and no key is
// passed. A sidecar that cannot be read now is said in Desk's log, and the
// list is passed on the other checks: `audit verify` reads the sidecar
// itself.
//
// **The project Desk was started on is read as a desk is**, under its own
// name (`signingKeyName`): where its upgrade made a key, that key and its
// list are held to the same rules and passed the same way.
func (s *Server) heldKeys(ctx context.Context, project heldDir) (auditKeys, *keyReading) {
	dir, err := s.assistant.openSigning(false)
	keys, reading := s.keysIn(ctx, project, dir, err)
	if reading == nil {
		dir.Close()
	}
	return keys, reading
}

// noKeys is what the panel says where Desk keeps neither a key nor a list of
// keys for this desk: that it keeps none for this desk, or, on the project
// Desk was started on, none for the project it was started on.
func (s *Server) noKeys() auditKeys {
	if s.cfg.deskID == "" {
		return auditKeys{State: keysStartup}
	}
	return auditKeys{State: keysNone}
}

// keysIn is heldKeys's reading, through dir, the signing folder the caller
// opened (openErr where it could not), and closes nothing: a rotation reads
// its keys so through the folder it holds the lock of (rotation.go). The
// reading it answers refers to dir. The keys are those kept under this
// desk's name (`signingKeyName`).
func (s *Server) keysIn(ctx context.Context, project heldDir, dir *signingDir, openErr error) (auditKeys, *keyReading) {
	if errors.Is(openErr, errNoSigningDir) {
		return s.noKeys(), nil
	}
	if err := openErr; err != nil {
		s.log.Printf("desk: the signing folder could not be opened for the decision record: %v", err)
		return auditKeys{State: keysUnread, Problem: "Desk could not open the folder it keeps signing keys in: " + strings.TrimRight(s.custodyWords(err.Error()), ".") + "."}, nil
	}
	unread := func(problem string) (auditKeys, *keyReading) {
		return auditKeys{State: keysUnread, Problem: problem}, nil
	}
	name := s.signingKeyName()
	if name == "" {
		// The project's identity could not be read now (issue #283): what
		// Desk keeps for it cannot be found, which is not "none".
		return unread("Desk could not read this project's identity, which names the key Desk keeps for it, so it passed no key.")
	}
	seedName := name + seedSuffix
	seed, seedErr := dir.root.Lstat(seedName)
	public, list, found, err := dir.readKeysFile(name + keysSuffix)
	switch {
	case err != nil:
		return unread("Desk could not read the public keys it keeps for this desk: " + err.Error() + ".")
	case !found && errors.Is(seedErr, fs.ErrNotExist):
		return s.noKeys(), nil
	case !found:
		return unread("Desk keeps a key for this desk, but no list of its public keys, so it passed no key.")
	case seedErr != nil:
		return unread("Desk keeps a list of public keys for this desk, but not its key, so it passed no key.")
	}
	if err := checkSeed(seedName, seed); err != nil {
		return unread("Desk could not use the key it keeps for this desk: " + strings.TrimRight(err.Error(), ".") + ".")
	}
	if err := dir.namesFile(seedName, seed); err != nil {
		return unread("The key Desk keeps for this desk is not at the path the runtime reads it from, so it passed no key.")
	}
	current, err := s.publicKeyOf(ctx, project, dir, seedName)
	if err != nil {
		return unread("The runtime could not read the key Desk keeps for this desk, so it passed no key: " + strings.TrimRight(err.Error(), ".") + ".")
	}
	if err := checkKeysAgainst(public, current, nil); err != nil {
		return unread("Desk's list of this desk's public keys does not name the key Desk keeps for it, so it passed no key.")
	}
	reading := &keyReading{dir: dir, seed: seed, list: list, keys: public, current: current}
	reading.sidecar, reading.sidecarErr = s.readSidecar(ctx)
	if reading.sidecarErr != nil {
		s.log.Printf("desk: the signature sidecar of desk %s could not be read to check its list of keys: %v", name, reading.sidecarErr)
	} else if err := checkKeysAgainst(public, current, &reading.sidecar); err != nil {
		return unread("Desk's list of this desk's public keys does not agree with the key rotations in the trail's signature sidecar, so it passed no key: " + err.Error() + ".")
	} else {
		// This trail's keys, where it began after an earlier one's (issue #285).
		public = keysOfTrail(public, &reading.sidecar)
	}
	return auditKeys{State: keysKept, Public: public}, reading
}

// signingCheck runs `packs validate --config jpack.json --format json` and
// reads its `audit-signing-key` check. Where the configuration declares
// packs, it names one with `--id`: the configuration's own checks are made
// either way, and one pack's report keeps the answer within `runRuntime`'s
// bound however many packs the project has.
func (s *Server) signingCheck(ctx context.Context, dir heldDir) auditSigning {
	args := []string{"packs", "validate", "--config", runtimeConfigName, "--format", "json"}
	if id, ok := s.onePackID(); ok {
		args = append(args, "--id", id)
	}
	out, runErr := runRuntime(ctx, s.cfg.JpackBin, dir, args...)
	return readSigningCheck(out, runErr)
}

// onePackID is the first of the pack ids this project's jpack.json declares,
// in order, where it declares any. It reads that member and nothing else.
func (s *Server) onePackID() (string, bool) {
	config, err := s.readReviewFileWithin(runtimeConfigName, reviewTextLimit)
	if err != nil {
		return "", false
	}
	var declared struct {
		Packs map[string]json.RawMessage `json:"packs"`
	}
	if json.Unmarshal(config, &declared) != nil || len(declared.Packs) == 0 {
		return "", false
	}
	return slices.Sorted(maps.Keys(declared.Packs))[0], true
}

// readSigningCheck reads what `packs validate` printed, whatever the exit.
//
//   - "packs validate", exit 0 with "valid" or exit 1 with "invalid": its
//     `audit-signing-key` check, where it reported one, which must have a
//     status of the three a check has, appear once, and not have failed
//     under a "valid"; and where it reported none, that no key is named.
//   - Any other non-zero exit with "error" or "unsupported" and the
//     runtime's own diagnostics: its refusal, in its words.
//
// Anything else is not an answer the runtime documents, and says so.
func readSigningCheck(out []byte, runErr error) auditSigning {
	code := 0
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			return auditSigning{State: signingUnread, Problem: runErr.Error()}
		}
		code = exit.ExitCode()
	}
	undocumented := auditSigning{State: signingUnread, Problem: "Its packs validate did not answer as documented."}
	var got struct {
		Command     string              `json:"command"`
		Status      string              `json:"status"`
		Diagnostics []runtimeDiagnostic `json:"diagnostics"`
		Checks      []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if out == nil || json.Unmarshal(out, &got) != nil {
		return undocumented
	}
	switch {
	case got.Command == "packs validate" && (code == 0 && got.Status == "valid" || code == 1 && got.Status == "invalid"):
		found := auditSigning{State: signingNoKey}
		for _, check := range got.Checks {
			if check.Name != auditSigningKeyCheck {
				continue
			}
			if found.State == signingChecked || !slices.Contains([]string{"passed", "failed", "skipped"}, check.Status) ||
				check.Status == "failed" && got.Status != "invalid" {
				return undocumented
			}
			found = auditSigning{State: signingChecked, Status: check.Status, Detail: check.Detail}
		}
		return found
	case code > 0 && (got.Status == "error" || got.Status == "unsupported") && (auditVerification{Diagnostics: got.Diagnostics}).said():
		return auditSigning{State: signingUnread, Diagnostics: got.Diagnostics}
	}
	return undocumented
}

// The answers runtime 0.25.0 gives, measured, for an `audit verify` it does
// not have: with Desk's flags, its root command knows no `--config`; without
// them, it knows no `audit`. A runtime with an `audit` group and no `verify`
// would say the third. Each comes alone, under JPS-INVOCATION-ARGUMENTS, exit
// 3. Runner's probe for the same absence reads the same three.
var auditAbsentMessages = []string{
	`unknown command "audit" for "jpack"`,
	`unknown command "verify" for "jpack audit"`,
	`unknown flag: --config`,
}

// auditVerification is `audit verify --format json` as it arrives. Every
// member a report must have is a pointer, so that a missing one is told from
// a zero, and is checked before anything is shown.
type auditVerification struct {
	Command              string                   `json:"command"`
	Status               string                   `json:"status"`
	Diagnostics          []runtimeDiagnostic      `json:"diagnostics"`
	Lines                *int64                   `json:"lines"`
	Bytes                *int64                   `json:"bytes"`
	Snapshot             *bool                    `json:"snapshotBetweenWrites"`
	Coverage             *wireAuditCoverage       `json:"coverage"`
	Segments             []wireAuditSegment       `json:"segments"`
	SegmentsTotal        *int64                   `json:"segmentsTotal"`
	Discontinuities      []wireAuditDiscontinuity `json:"discontinuities"`
	DiscontinuitiesTotal *int64                   `json:"discontinuitiesTotal"`
	Findings             []wireAuditFinding       `json:"findings"`
	FindingsTotal        *int64                   `json:"findingsTotal"`
	Signatures           *wireAuditSignatures     `json:"signatures"`
	Stamps               *wireAuditStamps         `json:"stamps"`
	Trail                *string                  `json:"trail"`
	Head                 json.RawMessage          `json:"head"`
	Establishes          []string                 `json:"establishes"`
	DoesNotEstablish     []string                 `json:"doesNotEstablish"`
}

type wireAuditCoverageState struct {
	Status  *string `json:"status"`
	Through *int64  `json:"through"`
	Detail  *string `json:"detail"`
}

type wireAuditCoverage struct {
	LegacyPrefix    *int64                  `json:"legacyPrefix"`
	Chained         *int64                  `json:"chained"`
	Unchained       *int64                  `json:"unchained"`
	Uncovered       *int64                  `json:"uncovered"`
	Damaged         *int64                  `json:"damaged"`
	Signed          *wireAuditCoverageState `json:"signed"`
	SignedRecords   *int64                  `json:"signedRecords"`
	UnsignedRecords *int64                  `json:"unsignedRecords"`
	Checkpointed    *wireAuditCoverageState `json:"checkpointed"`
	Witnessed       *int64                  `json:"witnessed"`
	Unwitnessed     *int64                  `json:"unwitnessed"`
	Stamped         *wireAuditCoverageState `json:"stamped"`
}

type wireAuditSegment struct {
	FirstLine *int64 `json:"firstLine"`
	LastLine  *int64 `json:"lastLine"`
}

type wireAuditDiscontinuity struct {
	Line        *int64  `json:"line"`
	Reason      *string `json:"reason"`
	DamagedLine *int64  `json:"damagedLine"`
	Bytes       *int64  `json:"bytes"`
	Digest      *string `json:"digest"`
}

type wireAuditFinding struct {
	Name   *string `json:"name"`
	Line   *int64  `json:"line"`
	Detail *string `json:"detail"`
}

type wireAuditSignatures struct {
	Lines        *int64  `json:"lines"`
	Unreadable   *int64  `json:"unreadable"`
	Rotations    *int64  `json:"rotations"`
	KeysSupplied *int64  `json:"keysSupplied"`
	Revocations  *int64  `json:"revocations"`
	FirstKey     *string `json:"firstKey"`
	KeyInForce   *string `json:"keyInForce"`
}

type wireAuditStamps struct {
	Lines                *int64             `json:"lines"`
	Unreadable           *int64             `json:"unreadable"`
	Trusted              *int64             `json:"trusted"`
	RevocationChecked    *int64             `json:"revocationChecked"`
	RevocationNotChecked *int64             `json:"revocationNotChecked"`
	CoveredBy            *string            `json:"coveredBy"`
	Lag                  *wireAuditStampLag `json:"lag"`
}

type wireAuditStampLag struct {
	Records      *int64   `json:"records"`
	MaxSeconds   *float64 `json:"maxSeconds"`
	MaxSequence  *int64   `json:"maxSequence"`
	MinSeconds   *float64 `json:"minSeconds"`
	MinSequence  *int64   `json:"minSequence"`
	AtAfterStamp *bool    `json:"atAfterStamp"`
	AtUnreadable *int64   `json:"atUnreadable"`
}

// stamps is the stamps file as the runtime read it: every count present and
// not negative, a lag of every member, its records' sequences present where
// it covers any record, and a time the stamped records existed by exactly
// where the stamps reach a record (`stamped`).
func (p *auditPresence) stamps(value *wireAuditStamps, stamped auditCoverageState) *auditStamps {
	if value == nil {
		return nil
	}
	stamps := &auditStamps{Lines: p.count(value.Lines), Unreadable: p.count(value.Unreadable), Trusted: p.count(value.Trusted),
		RevocationChecked: p.count(value.RevocationChecked), RevocationNotChecked: p.count(value.RevocationNotChecked)}
	if value.CoveredBy != nil {
		if _, err := time.Parse(time.RFC3339Nano, *value.CoveredBy); err != nil {
			p.missing = true
		}
		stamps.CoveredBy = *value.CoveredBy
	}
	if (stamps.CoveredBy != "") != (stamped.Status == "through") || stamps.RevocationChecked+stamps.RevocationNotChecked != stamps.Trusted {
		p.missing = true
	}
	lag := value.Lag
	if lag == nil || lag.MaxSeconds == nil || lag.MinSeconds == nil || lag.AtAfterStamp == nil {
		p.missing = true
		return stamps
	}
	stamps.Lag = auditStampLag{Records: p.count(lag.Records), MaxSeconds: *lag.MaxSeconds, MinSeconds: *lag.MinSeconds,
		AtAfterStamp: *lag.AtAfterStamp, AtUnreadable: p.count(lag.AtUnreadable)}
	if stamps.Lag.Records > 0 {
		stamps.Lag.MaxSequence, stamps.Lag.MinSequence = p.count(lag.MaxSequence), p.count(lag.MinSequence)
		if stamps.Lag.MaxSequence < 1 || stamps.Lag.MinSequence < 1 {
			p.missing = true
		}
	}
	return stamps
}

// auditPresence reads required members, and remembers whether any was
// missing or held a value no report has.
type auditPresence struct{ missing bool }

// count is a count or a sequence: present, and not negative.
func (p *auditPresence) count(value *int64) int64 {
	if value == nil || *value < 0 {
		p.missing = true
		return 0
	}
	return *value
}

// text is a present string; named says it must not be empty either.
func (p *auditPresence) text(value *string, named bool) string {
	if value == nil || named && *value == "" {
		p.missing = true
		return ""
	}
	return *value
}

// state is one protection's reach: a status, and the sequence it reaches
// through, which a status of "through" must name.
func (p *auditPresence) state(value *wireAuditCoverageState) auditCoverageState {
	if value == nil {
		p.missing = true
		return auditCoverageState{}
	}
	state := auditCoverageState{Status: p.text(value.Status, true)}
	if value.Through != nil || state.Status == "through" {
		state.Through = p.count(value.Through)
		if state.Status == "through" && state.Through < 1 {
			p.missing = true
		}
	}
	if value.Detail != nil {
		state.Detail = *value.Detail
	}
	return state
}

// report is the answer as a report, or false where a member a report has is
// missing, holds no valid value, or disagrees with the status: a list longer
// than its total, findings under a status that says every check passed, or
// none under one that says a check failed.
func (got auditVerification) report() (*auditReport, bool) {
	p := &auditPresence{}
	if got.Coverage == nil || got.Snapshot == nil || got.Segments == nil || got.Discontinuities == nil || got.Findings == nil || got.Establishes == nil || got.DoesNotEstablish == nil {
		return nil, false
	}
	c := got.Coverage
	report := &auditReport{
		Status: got.Status, Lines: p.count(got.Lines), Bytes: p.count(got.Bytes), SnapshotBetweenWrites: *got.Snapshot,
		Coverage: auditCoverage{
			LegacyPrefix: p.count(c.LegacyPrefix), Chained: p.count(c.Chained), Unchained: p.count(c.Unchained),
			Uncovered: p.count(c.Uncovered), Damaged: p.count(c.Damaged),
			Signed: p.state(c.Signed), SignedRecords: p.count(c.SignedRecords), UnsignedRecords: p.count(c.UnsignedRecords),
			Checkpointed: p.state(c.Checkpointed), Witnessed: p.count(c.Witnessed), Unwitnessed: p.count(c.Unwitnessed),
			Stamped: p.state(c.Stamped),
		},
		Segments: []auditSegment{}, SegmentsTotal: p.count(got.SegmentsTotal),
		Discontinuities: []auditDiscontinuity{}, DiscontinuitiesTotal: p.count(got.DiscontinuitiesTotal),
		Findings: []auditFinding{}, FindingsTotal: p.count(got.FindingsTotal),
		Establishes: got.Establishes, DoesNotEstablish: got.DoesNotEstablish,
	}
	if got.Trail != nil {
		report.Trail = *got.Trail
	}
	if head, ok := readCheckpointLine(got.Head); ok {
		report.head = head
	}
	for _, segment := range got.Segments {
		report.Segments = append(report.Segments, auditSegment{FirstLine: p.count(segment.FirstLine), LastLine: p.count(segment.LastLine)})
	}
	for _, item := range got.Discontinuities {
		report.Discontinuities = append(report.Discontinuities, auditDiscontinuity{
			Line: p.count(item.Line), Reason: p.text(item.Reason, true), DamagedLine: p.count(item.DamagedLine),
			Bytes: p.count(item.Bytes), Digest: p.text(item.Digest, true),
		})
	}
	for _, finding := range got.Findings {
		report.Findings = append(report.Findings, auditFinding{Name: p.text(finding.Name, true), Line: p.count(finding.Line), Detail: p.text(finding.Detail, false)})
	}
	report.Stamps = p.stamps(got.Stamps, report.Coverage.Stamped)
	if sig := got.Signatures; sig != nil {
		report.Signatures = &auditSignatures{
			Lines: p.count(sig.Lines), Unreadable: p.count(sig.Unreadable), Rotations: p.count(sig.Rotations),
			KeysSupplied: p.count(sig.KeysSupplied), Revocations: p.count(sig.Revocations),
			FirstKey: p.text(sig.FirstKey, true), KeyInForce: p.text(sig.KeyInForce, true),
		}
	}
	if p.missing ||
		report.SegmentsTotal < int64(len(report.Segments)) ||
		report.DiscontinuitiesTotal < int64(len(report.Discontinuities)) ||
		report.FindingsTotal < int64(len(report.Findings)) ||
		(report.Status == "invalid") != (report.FindingsTotal > 0) {
		return nil, false
	}
	return report, true
}

// said is the runtime's diagnostics, where it gave any and each has a code and
// words.
func (got auditVerification) said() bool {
	if len(got.Diagnostics) == 0 {
		return false
	}
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Code == "" || diagnostic.Message == "" {
			return false
		}
	}
	return true
}

// absent is whether the answer is the command parser's, saying the runtime
// has no `audit verify`: exit 3, and one diagnostic, JPS-INVOCATION-ARGUMENTS,
// in one of the forms auditAbsentMessages lists. Any other invocation error,
// such as an argument the runtime could not read, is the runtime's refusal.
func (got auditVerification) absent(code int) bool {
	return code == 3 && len(got.Diagnostics) == 1 && got.Diagnostics[0].Code == "JPS-INVOCATION-ARGUMENTS" &&
		slices.Contains(auditAbsentMessages, got.Diagnostics[0].Message)
}

// readAuditVerification reads what `audit verify` printed, whatever the exit.
//
//   - A runtime without the command (absent) is said as one.
//   - Exit 0 with "valid" or "segmented", and exit 1 with "invalid", from
//     `audit verify`, with every member a report has, is the report: exit 1 is
//     a failed check, never a failure to check.
//   - Any other non-zero exit with "error" or "unsupported" and the runtime's
//     own diagnostics is its refusal to verify, shown in its words: a trail no
//     record has been written to yet (exit 4), a configuration it refuses
//     (exit 1 or 2), an argument it could not read (exit 3).
//
// Anything else is not an answer the runtime documents, and is an error.
func readAuditVerification(out []byte, runErr error) (auditAnswer, error) {
	code := 0
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			return auditAnswer{}, runErr
		}
		code = exit.ExitCode()
	}
	var got auditVerification
	if out == nil || json.Unmarshal(out, &got) != nil {
		return auditAnswer{}, undocumentedAudit(runErr)
	}
	switch {
	case got.absent(code):
		return auditAnswer{State: auditStateOlder}, nil
	case got.Command == auditVerifyCommand && (code == 0 && (got.Status == "valid" || got.Status == "segmented") || code == 1 && got.Status == "invalid"):
		if report, ok := got.report(); ok {
			return auditAnswer{State: auditStateReport, Report: report}, nil
		}
	case code > 0 && (got.Status == "error" || got.Status == "unsupported") && got.said():
		return auditAnswer{State: auditStateUnverified, Diagnostics: got.Diagnostics}, nil
	}
	return auditAnswer{}, undocumentedAudit(runErr)
}

func undocumentedAudit(runErr error) error {
	if runErr != nil {
		return fmt.Errorf("its audit verify did not answer as documented: %w", runErr)
	}
	return errors.New("its audit verify did not answer as documented")
}
