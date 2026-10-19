package desk

// One offline check for manual authoring, AI proposals and job mappings.
// Runtime owns document semantics; Desk owns revisions, conversion policies and
// write approval. No result from this boundary authorizes acquisition or a job.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"slices"
)

const dialectDocumentLimit = 128 << 10

// Coverage (2 MiB), escaped canonical document, pack findings and envelope.
const dialectAnswerLimit = 4 << 20

var errDialectChecksUnavailable = errors.New("Runtime dialect checks are unavailable. Check the Runtime installation before saving this mapping.")

type dialectAuthoringReport struct {
	OutputVersion   string              `json:"outputVersion"`
	ContractVersion string              `json:"contractVersion"`
	Command         string              `json:"command"`
	Status          string              `json:"status"`
	FormatVersion   string              `json:"formatVersion"`
	ArtifactID      string              `json:"artifactId"`
	ArtifactVersion string              `json:"artifactVersion"`
	DialectVersion  int                 `json:"dialectVersion"`
	Document        string              `json:"document"`
	Digest          string              `json:"digest"`
	Diagnostics     []runtimeDiagnostic `json:"diagnostics"`
	Capabilities    *struct {
		FormatVersions     []string `json:"formatVersions"`
		ValidationVersions []int    `json:"validationVersions"`
		PackCheckVersions  []int    `json:"packCheckVersions"`
	} `json:"capabilities"`
	Compatibility *struct {
		Scope string `json:"scope"`
		Pack  struct {
			SHA256 string `json:"sha256"`
		} `json:"pack"`
	} `json:"compatibility"`
}

func (s *Server) runDialectAuthoring(ctx context.Context, dir heldDir, input []byte, operation string, args ...string) (json.RawMessage, dialectAuthoringReport, error) {
	argv := append([]string{"dialect", operation}, args...)
	argv = append(argv, "--format", "json")
	out, runErr := runRuntimeInputLimit(ctx, s.cfg.JpackBin, dir, input, dialectAnswerLimit, argv...)
	var report dialectAuthoringReport
	if ctx.Err() != nil {
		return nil, report, ctx.Err()
	}
	if rejectDuplicateMembers(out) != nil || json.Unmarshal(out, &report) != nil || report.OutputVersion != "2" || report.ContractVersion != "1" || report.Command != "dialect "+operation || report.Status == "" {
		return nil, report, errDialectChecksUnavailable
	}
	// Invalid/unsupported documents are useful structured results, not transport
	// failures. An unsuccessful process must never supply an accepted check.
	if runErr != nil && report.Status != "refused" && report.Status != "incomplete" && report.Status != "not-applicable" {
		return nil, report, errDialectChecksUnavailable
	}
	return json.RawMessage(out), report, nil
}

func (s *Server) checkDialectAuthoring(ctx context.Context, document []byte, pack string) (json.RawMessage, dialectAuthoringReport, error) {
	var empty dialectAuthoringReport
	if len(document) > dialectDocumentLimit || len(pack) > maxFileBytes {
		return nil, empty, errors.New("The dialect or pack exceeds the authoring check limit.")
	}
	// No user paths or project files enter the subprocess. A short-lived snapshot
	// is outside the workspace watcher and is removed even after interruption.
	where, err := os.MkdirTemp("", "jpack-desk-dialect-")
	if err != nil {
		return nil, empty, errDialectChecksUnavailable
	}
	defer os.RemoveAll(where)
	root, err := os.OpenRoot(where)
	if err != nil {
		return nil, empty, errDialectChecksUnavailable
	}
	defer root.Close()
	file, err := root.Open(".")
	if err != nil {
		return nil, empty, errDialectChecksUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, empty, errDialectChecksUnavailable
	}
	dir := heldDir{file: file, path: where, info: info}
	_, contract, err := s.runDialectAuthoring(ctx, dir, nil, "contract")
	if err != nil || contract.Status != "available" || contract.Capabilities == nil {
		return nil, empty, errDialectChecksUnavailable
	}
	// Malformed documents still go to Runtime for its authoritative diagnostic.
	var head struct {
		Version       json.RawMessage `json:"version"`
		FormatVersion string          `json:"formatVersion"`
	}
	_ = json.Unmarshal(document, &head)
	versions := contract.Capabilities.ValidationVersions
	operation := "validate"
	args := []string{"-"}
	if pack != "" {
		versions = contract.Capabilities.PackCheckVersions
		operation = "check"
		args = append(args, "--pack", "pack.json")
	}
	var family int
	_ = json.Unmarshal(head.Version, &family)
	if head.FormatVersion != "" && !slices.Contains(contract.Capabilities.FormatVersions, head.FormatVersion) || family != 0 && !slices.Contains(versions, family) {
		return nil, empty, errors.New("This Runtime does not support the requested dialect format.")
	}
	if pack != "" {
		if err = root.WriteFile("pack.json", []byte(pack), 0o600); err != nil {
			return nil, empty, errDialectChecksUnavailable
		}
	}
	raw, report, err := s.runDialectAuthoring(ctx, dir, document, operation, args...)
	if err != nil {
		return nil, report, err
	}
	if report.Document != "" {
		if report.Digest != "sha256:"+digestOf([]byte(report.Document)) {
			return nil, empty, errDialectChecksUnavailable
		}
		// Compare values without converting JSON numbers to floating point. Runtime
		// may canonicalize spelling, but may not replace the supplied document.

		var given, returned any
		decode := func(raw []byte, v *any) error {
			d := json.NewDecoder(bytes.NewReader(raw))
			d.UseNumber()
			return d.Decode(v)
		}
		if decode(document, &given) != nil || decode([]byte(report.Document), &returned) != nil {
			return nil, empty, errDialectChecksUnavailable
		}
		a, _ := json.Marshal(dialectComparable(given))
		b, _ := json.Marshal(dialectComparable(returned))
		if !bytes.Equal(a, b) {
			return nil, empty, errDialectChecksUnavailable
		}
	}
	if report.Compatibility != nil && (report.Compatibility.Scope != "declared-inventory-coverage" || report.Compatibility.Pack.SHA256 != digestOf([]byte(pack))) {
		return nil, empty, errDialectChecksUnavailable
	}
	return raw, report, nil
}

func (r dialectAuthoringReport) accepted(pack bool) bool {
	if r.Document == "" || r.Digest == "" {
		return false
	}
	if !pack {
		return r.Status == "document-valid"
	}
	return r.Compatibility != nil && (r.Status == "complete" || r.Status == "review-required")
}
func (s *Server) checkInputQualification(ctx context.Context, q *inputQualification) error {
	if q == nil {
		return nil
	}
	raw, err := json.Marshal(q.Mapping)
	if err != nil {
		return err
	}
	_, err = s.checkedDialectMapping(ctx, raw, q.Pack)
	return err
}
func (s *Server) checkedDialectMapping(ctx context.Context, raw []byte, pack string) (dialectAuthoringReport, error) {
	_, report, err := s.checkDialectAuthoring(ctx, raw, pack)
	if err != nil {
		return report, err
	}
	if !report.accepted(true) {
		return report, errors.New("Runtime dialect checks did not pass. Review the mapping and its pack coverage before saving.")
	}
	return report, nil
}

// Conversion produces a new case mapping. Check those actual bytes once per
// distinct mapping in this request, preserving all case/acquisition byte slices.
// This request-local index cannot make a later workspace read or check stale.
func (s *Server) checkPreparedMappings(ctx context.Context, doc inputPreparation, rows []inputPreparedRow) error {
	if doc.Recipe.Qualification == nil {
		return errors.New("Choose a saved pack and review its dialect.")
	}
	checked := map[string]dialectAuthoringReport{}
	for i := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		var input, source map[string]json.RawMessage
		if json.Unmarshal(rows[i].Input, &input) != nil || json.Unmarshal(input["source"], &source) != nil || len(source["mapping"]) == 0 {
			return errors.New("The prepared dialect mapping is unavailable.")
		}
		// This is a newly produced Runner v2 mapping, not a rewrite of a
		// published document. Spell out its historical envelope members and
		// omit empty sources so Runtime checks the exact identity Runner keeps.
		var mapping map[string]json.RawMessage
		if json.Unmarshal(source["mapping"], &mapping) != nil {
			return errors.New("The prepared dialect mapping is unavailable.")
		}
		if selector, named := mapping["formatVersion"]; named {
			if !bytes.Equal(selector, []byte(`"2.0.0"`)) {
				return errors.New("The prepared dialect format is unsupported.")
			}
		} else {
			if !bytes.Equal(bytes.TrimSpace(mapping["version"]), []byte("2")) {
				return errors.New("The prepared dialect format is unsupported.")
			}
			mapping["provider"] = json.RawMessage(`""`)
			mapping["facts"] = json.RawMessage(`null`)
			mapping["evidence"] = json.RawMessage(`null`)
			if bytes.Equal(bytes.TrimSpace(mapping["sources"]), []byte(`[]`)) {
				delete(mapping, "sources")
			}
		}
		source["mapping"], _ = json.Marshal(mapping)
		key := string(source["mapping"])
		report, ok := checked[key]
		if !ok {
			var err error
			report, err = s.checkedDialectMapping(ctx, source["mapping"], doc.Recipe.Qualification.Pack)
			if err != nil {
				return err
			}
			checked[key] = report
		}
		source["mapping"] = json.RawMessage(report.Document)
		source["mappingDigest"], _ = json.Marshal(report.Digest)
		input["source"], _ = json.Marshal(source)
		rows[i].Input, _ = json.Marshal(input)
	}
	return nil
}

func (s *Server) handleDialectAuthoringCheck(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request struct {
		Document string `json:"document"`
		Pack     string `json:"pack"`
	}
	if inputRequest(r, &request) != nil || request.Document == "" {
		writeJSONCoded(w, 400, CodeBadRequest, "Provide a dialect document and an optional pack snapshot.")
		return
	}
	raw, _, err := s.checkDialectAuthoring(r.Context(), []byte(request.Document), request.Pack)
	if err != nil {
		writeJSONCoded(w, 503, CodeBadRequest, err.Error())
		return
	}
	writeJSON(w, 200, raw)
}

// Canonical Dialects identify negative zero with zero, without rounding other
// integers or altering numeric spellings in case data (which never enters here).
func dialectComparable(v any) any {
	switch x := v.(type) {
	case json.Number:
		if x == "-0" {
			return json.Number("0")
		}
	case map[string]any:
		for k, y := range x {
			x[k] = dialectComparable(y)
		}
	case []any:
		for i, y := range x {
			x[i] = dialectComparable(y)
		}
	}
	return v
}
func inputQualificationChanged(before, after *inputQualification) bool {
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	return !bytes.Equal(a, b)
}
