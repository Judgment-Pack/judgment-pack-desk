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
// release that reads it has every audit command. An `audit verify` that the
// runtime does not know (it answers for its root command, exit 3) is taken as
// the same absence. With either, the page says one sentence and runs nothing
// more.
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
	"os/exec"
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
	// Files is which of the trail, its signature sidecar and its stamps the
	// audit directory holds for download, by the names the download takes:
	// "evaluations", "signatures" and "stamps". It is given with a report and
	// with the runtime's refusal, and with nothing else.
	Files []string `json:"files,omitempty"`
}

// auditRuntime is the directory and refusal for running the audit commands
// over this desk's project: the review's own, in the panel's words.
func (s *Server) auditRuntime() (heldDir, string) {
	return s.projectRuntime("This desk holds no project whose decision record Desk can check.", "check its decision record")
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
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal, "The decision record could not be checked: "+strings.TrimRight(err.Error(), ".")+".")
		return
	}
	writeJSON(w, http.StatusOK, answer)
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
	answer.Files = s.auditFilesPresent()
	return answer, nil
}

// auditVerification is `audit verify --format json` as it arrives, with the
// members a report must have kept as pointers, so that a missing one is told
// from an empty one.
type auditVerification struct {
	Command              string               `json:"command"`
	Status               string               `json:"status"`
	Diagnostics          []runtimeDiagnostic  `json:"diagnostics"`
	Lines                int64                `json:"lines"`
	Bytes                int64                `json:"bytes"`
	Snapshot             *bool                `json:"snapshotBetweenWrites"`
	Coverage             *auditCoverage       `json:"coverage"`
	Segments             []auditSegment       `json:"segments"`
	SegmentsTotal        int64                `json:"segmentsTotal"`
	Discontinuities      []auditDiscontinuity `json:"discontinuities"`
	DiscontinuitiesTotal int64                `json:"discontinuitiesTotal"`
	Findings             []auditFinding       `json:"findings"`
	FindingsTotal        int64                `json:"findingsTotal"`
	Establishes          []string             `json:"establishes"`
	DoesNotEstablish     []string             `json:"doesNotEstablish"`
}

// readAuditVerification reads what `audit verify` printed, whatever the exit.
//
//   - Exit 0 with "valid" or "segmented", and exit 1 with "invalid", is the
//     report: exit 1 is a failed check, never a failure to check.
//   - Exit 3 answered for any command but `audit verify` is a runtime without
//     it: runtime 0.25.0, asked for `audit verify --config …`, answers for its
//     root command that it knows no `--config` there.
//   - Any other non-zero exit with the runtime's own diagnostics is its
//     refusal to verify, such as a trail no record has been written to yet.
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
	if code == 3 && got.Command != auditVerifyCommand && got.Status == "error" {
		return auditAnswer{State: auditStateOlder}, nil
	}
	if got.Command != auditVerifyCommand {
		return auditAnswer{}, undocumentedAudit(runErr)
	}
	switch {
	case code == 0 && (got.Status == "valid" || got.Status == "segmented"), code == 1 && got.Status == "invalid":
		if got.Coverage == nil || got.Snapshot == nil || got.Segments == nil || got.Discontinuities == nil || got.Findings == nil || got.Establishes == nil || got.DoesNotEstablish == nil {
			return auditAnswer{}, undocumentedAudit(runErr)
		}
		return auditAnswer{State: auditStateReport, Report: &auditReport{
			Status: got.Status, Lines: got.Lines, Bytes: got.Bytes, SnapshotBetweenWrites: *got.Snapshot,
			Coverage: *got.Coverage, Segments: got.Segments, SegmentsTotal: got.SegmentsTotal,
			Discontinuities: got.Discontinuities, DiscontinuitiesTotal: got.DiscontinuitiesTotal,
			Findings: got.Findings, FindingsTotal: got.FindingsTotal,
			Establishes: got.Establishes, DoesNotEstablish: got.DoesNotEstablish,
		}}, nil
	case code > 1 && (got.Status == "error" || got.Status == "unsupported") && len(got.Diagnostics) > 0:
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
