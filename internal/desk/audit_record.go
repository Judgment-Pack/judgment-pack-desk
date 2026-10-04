package desk

// The decision-record panel (ADR-0010, sections 4, 6 and 8).
//
// The panel shows what the runtime's own `jpack audit verify` finds in this
// desk's trail. Desk forms no verdict of its own: it passes through the
// report's status, coverage, segments, discontinuities and findings, and the
// runtime's own sentences about what the result establishes and what it does
// not.
//
// **It runs with no held input.** No `--public-key`, `--expect` or `--tsa-…`,
// because this desk holds no key, no record of a hand-over and no stamping
// roots yet, and never a `--require-…` flag: those are a reader's demands, not
// the operator's. The runtime then checks the chain alone, and says so in its
// own sentences.
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
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
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
	// Establishes and DoesNotEstablish are the runtime's own sentences, in
	// English, passed through as it wrote them.
	Establishes      []string `json:"establishes"`
	DoesNotEstablish []string `json:"doesNotEstablish"`
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
	dir, refusal := s.auditRuntime()
	if refusal != "" {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	answer, err := s.auditVerify(r.Context(), dir)
	if err != nil {
		s.log.Printf("desk: the decision record could not be checked: %v", err)
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal, "The decision record could not be checked: "+strings.TrimRight(s.withoutPaths(err.Error()), ".")+".")
		return
	}
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

// withoutPaths is message with no path from the root of a file system in it.
//
// Each path this desk was configured with is replaced by what it names:
// JPACK_CONFIG's value by the variable's name, the runtime binary by its file
// name, and the project's folder by "the project's folder". Then every other
// path from a root, such as the one the runtime resolves an absolute
// `audit.dir` to, is replaced by "…", keeping its last name only where that is
// one of the runtime's own files (withoutAbsolutePaths).
//
// **It is the one way a message reaches the page from the panel or the
// download:** Desk's own errors, and every sentence of the runtime's that they
// pass on (withoutPathsIn). So an answer names a setting and why, and never
// says where the owner keeps their files. The log keeps the message whole.
func (s *Server) withoutPaths(message string) string {
	for _, path := range []struct{ value, name string }{
		{strings.TrimSpace(os.Getenv(runtimeConfigEnv)), runtimeConfigEnv},
		{s.cfg.JpackBin, filepath.Base(s.cfg.JpackBin)},
		{s.projectDir, "the project's folder"},
	} {
		if path.value != "" && path.value != path.name {
			message = strings.ReplaceAll(message, path.value, path.name)
		}
	}
	return withoutAbsolutePaths(message)
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

// withoutAbsolutePaths is message with each absolute path replaced by "…",
// or by "…/" and its last name where that is one of runtimeFileNames.
// Punctuation that ends a sentence after a path stays.
func withoutAbsolutePaths(message string) string {
	var out strings.Builder
	last := 0
	for _, match := range pathInMessage.FindAllStringSubmatchIndex(message, -1) {
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
	if answer.Diagnostics != nil {
		said := make([]runtimeDiagnostic, len(answer.Diagnostics))
		for i, diagnostic := range answer.Diagnostics {
			said[i] = runtimeDiagnostic{Code: diagnostic.Code, Message: s.withoutPaths(diagnostic.Message)}
		}
		answer.Diagnostics = said
	}
	if answer.Report == nil {
		return answer
	}
	report := *answer.Report
	for _, state := range []*auditCoverageState{&report.Coverage.Signed, &report.Coverage.Checkpointed, &report.Coverage.Stamped} {
		state.Detail = s.withoutPaths(state.Detail)
	}
	report.Discontinuities = slices.Clone(report.Discontinuities)
	for i := range report.Discontinuities {
		report.Discontinuities[i].Reason = s.withoutPaths(report.Discontinuities[i].Reason)
	}
	report.Findings = slices.Clone(report.Findings)
	for i := range report.Findings {
		report.Findings[i].Detail = s.withoutPaths(report.Findings[i].Detail)
	}
	report.Establishes = sentencesWithoutPaths(s, report.Establishes)
	report.DoesNotEstablish = sentencesWithoutPaths(s, report.DoesNotEstablish)
	answer.Report = &report
	return answer
}

func sentencesWithoutPaths(s *Server, sentences []string) []string {
	if sentences == nil {
		return nil
	}
	out := make([]string, len(sentences))
	for i, sentence := range sentences {
		out[i] = s.withoutPaths(sentence)
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
// the runtime's `audit verify`, with no held input.
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
	out, runErr := runRuntime(ctx, s.cfg.JpackBin, dir, "audit", "verify", "--config", runtimeConfigName, "--format", "json")
	answer, err := readAuditVerification(out, runErr)
	if err != nil {
		return auditAnswer{}, err
	}
	if answer.State == auditStateOlder {
		return older, nil
	}
	answer.Runtime = schema.version
	return answer, nil
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
