package desk

// One offline boundary for manual coverage checks and reviewed commits. Runtime
// owns prototype canonicalization/reference semantics; a check grants no authority.
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
)

const policyCoverageProfile = "jps-policy-companion-prototype"
const policyCoverageProfileVersion = "0.0.2"
const policyCoverageSpecRevision = "8a56a95d555c17640d2db4984363b779536b3c2a"
const policyCoverageCheckScope = "supplied-exact-bytes-anchors-and-scoped-core-references"
const policyCoverageCheckByteLimit = 2 << 20

type policyCoverageRuntimeCapabilities struct {
	Kinds                    []string `json:"kinds"`
	ProfileVersions          []string `json:"profileVersions"`
	CanonicalProfiles        []string `json:"canonicalProfiles"`
	ConsumerKinds            []string `json:"consumerKinds"`
	ConsumerElementKinds     []string `json:"consumerElementKinds"`
	Operations               []string `json:"operations"`
	ValidationScope          string   `json:"validationScope"`
	CheckScope               string   `json:"checkScope"`
	MaxDocumentBytes         int      `json:"maxDocumentBytes"`
	MaxMaterialBytes         int      `json:"maxMaterialBytes"`
	MaxOriginalBytes         int      `json:"maxOriginalBytes"`
	MaxTotalMaterialBytes    int      `json:"maxTotalMaterialBytes"`
	MaxMaterials             int      `json:"maxMaterials"`
	MaxMaterialsWireBytes    int      `json:"maxMaterialsWireBytes"`
	MaxDepth                 int      `json:"maxDepth"`
	MaxCoreNumberBytes       int      `json:"maxCoreNumberBytes"`
	MaxCoreExponentMagnitude int      `json:"maxCoreExponentMagnitude"`
	MaxArtifactVersionBytes  int      `json:"maxArtifactVersionBytes"`
	Experimental             bool     `json:"experimental"`
	Maturity                 string   `json:"maturity"`
	FullDocumentConformance  bool     `json:"fullDocumentConformance"`
	ApprovalVerification     bool     `json:"approvalVerification"`
	ExecutionAuthority       bool     `json:"executionAuthority"`
	LiveAuthority            bool     `json:"liveAuthority"`
	NetworkAcquisition       bool     `json:"networkAcquisition"`
}
type policyCoverageRuntimeDiagnostic struct {
	Code          string `json:"code"`
	CodeStability string `json:"codeStability"`
	Layer         string `json:"layer"`
	Severity      string `json:"severity"`
	InstancePath  string `json:"instancePath"`
	Message       string `json:"message"`
}
type policyCoverageRuntimeReport struct {
	OutputVersion string `json:"outputVersion"`
	Tool          struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"tool"`
	Command              string                            `json:"command"`
	ContractVersion      string                            `json:"contractVersion"`
	Specification        string                            `json:"specification"`
	SpecRevision         string                            `json:"specRevision"`
	Experimental         bool                              `json:"experimental"`
	Maturity             string                            `json:"maturity"`
	Status               string                            `json:"status"`
	Profile              string                            `json:"profile"`
	ProfileVersion       string                            `json:"profileVersion"`
	Kind                 string                            `json:"kind,omitempty"`
	ArtifactID           string                            `json:"artifactId,omitempty"`
	ArtifactVersion      string                            `json:"artifactVersion,omitempty"`
	ValidationScope      string                            `json:"validationScope,omitempty"`
	Document             string                            `json:"document,omitempty"`
	BytesDigest          string                            `json:"bytesDigest,omitempty"`
	CanonicalDigest      string                            `json:"canonicalDigest,omitempty"`
	StarterTemplate      bool                              `json:"starterTemplate,omitempty"`
	RequiredMaterials    json.RawMessage                   `json:"requiredMaterials,omitempty"`
	Resource             json.RawMessage                   `json:"resource,omitempty"`
	Capabilities         policyCoverageRuntimeCapabilities `json:"capabilities"`
	Diagnostics          []policyCoverageRuntimeDiagnostic `json:"diagnostics"`
	DiagnosticsTruncated bool                              `json:"diagnosticsTruncated,omitempty"`
}

func validCoverageRuntimeReport(r policyCoverageRuntimeReport, operation string) bool {
	c := r.Capabilities
	if r.OutputVersion != "2" || r.ContractVersion != "1" || r.Command != "experimental policy "+operation || r.Tool.Name != "jpack" || r.Tool.Version == "" || r.Profile != policyCoverageProfile || r.ProfileVersion != policyCoverageProfileVersion || r.SpecRevision != policyCoverageSpecRevision || r.Specification != "https://github.com/Judgment-Pack/judgment-pack-spec/blob/"+policyCoverageSpecRevision+"/rfcs/0023-policy-companion.md" || !r.Experimental || r.Maturity != "non-normative" || !c.Experimental || c.Maturity != "non-normative" || c.FullDocumentConformance || c.ApprovalVerification || c.ExecutionAuthority || c.LiveAuthority || c.NetworkAcquisition || r.StarterTemplate || c.CheckScope != policyCoverageCheckScope || c.ValidationScope != "closed-prototype-document-and-local-references" || !slices.Contains(c.ProfileVersions, policyCoverageProfileVersion) || !slices.Contains(c.CanonicalProfiles, "jps-canon") || !slices.Contains(c.ConsumerKinds, "pack") || c.MaxDocumentBytes < 1 || c.MaxDocumentBytes > 1<<20 || c.MaxMaterialBytes < 1 || c.MaxMaterialBytes > 4<<20 || c.MaxOriginalBytes < 1 || c.MaxOriginalBytes > 4<<20 || c.MaxTotalMaterialBytes < 1 || c.MaxTotalMaterialBytes > 16<<20 || c.MaxMaterials < 4 || c.MaxMaterials > 16 || c.MaxMaterialsWireBytes < 1 || c.MaxMaterialsWireBytes > 24<<20 || r.Diagnostics == nil {
		return false
	}
	for _, op := range []string{"contract", "canonicalize", "check"} {
		if !slices.Contains(c.Operations, op) {
			return false
		}
	}
	for _, kind := range []string{"policy", "governing-adoption"} {
		if !slices.Contains(c.Kinds, kind) {
			return false
		}
	}
	for _, kind := range []string{"rule", "exception", "applicability", "fallback"} {
		if !slices.Contains(c.ConsumerElementKinds, kind) {
			return false
		}
	}
	return true
}
func (s *Server) runCoverageRuntime(ctx context.Context, dir heldDir, input []byte, operation string, args ...string) (json.RawMessage, policyCoverageRuntimeReport, error) {
	argv := append([]string{"experimental", "policy", operation}, args...)
	argv = append(argv, "--format", "json")
	out, runErr := runRuntimeInputLimit(ctx, s.cfg.JpackBin, dir, input, policyCoverageCheckByteLimit, argv...)
	var report policyCoverageRuntimeReport
	if ctx.Err() != nil {
		return nil, report, ctx.Err()
	}
	if rejectDuplicateMembers(out) != nil || decodeDataJSON(out, &report) != nil || !validCoverageRuntimeReport(report, operation) {
		return nil, report, errors.New("Runtime Policy check response is unavailable or inconsistent.")
	}
	if report.Status == "refused" {
		var exit *exec.ExitError
		if runErr == nil || !errors.As(runErr, &exit) || !slices.Contains([]int{1, 2}, exit.ExitCode()) || len(report.Diagnostics) == 0 {
			return nil, report, errors.New("Runtime Policy refusal is inconsistent.")
		}
		return out, report, runErr
	}
	if runErr != nil {
		return nil, report, errors.New("Runtime did not complete the Policy check.")
	}
	return out, report, nil
}
func coverageRuntimeContent(raw []byte, media string) map[string]any {
	return map[string]any{"bytesDigest": "sha256:" + digestOf(raw), "byteLength": len(raw), "mediaType": media}
}
func (s *Server) checkPolicyCoverage(ctx context.Context, m policyCoverageMaterial) policyCoverageCheckReply {
	unavailable := func() policyCoverageCheckReply {
		return policyCoverageCheckReply{Availability: "unavailable", Reason: "Runtime does not provide compatible experimental Policy prototype 0.0.2 checks. The saved draft and history remain available."}
	}
	unsupported := func(reason string) policyCoverageCheckReply {
		return policyCoverageCheckReply{Availability: "unsupported", Reason: reason}
	}
	where, err := os.MkdirTemp("", "jpack-desk-policy-coverage-")
	if err != nil {
		return unavailable()
	}
	defer os.RemoveAll(where)
	root, err := os.OpenRoot(where)
	if err != nil {
		return unavailable()
	}
	defer root.Close()
	file, err := root.Open(".")
	if err != nil {
		return unavailable()
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return unavailable()
	}
	dir := heldDir{file: file, path: where, info: info}
	_, contract, err := s.runCoverageRuntime(ctx, dir, nil, "contract")
	if err != nil || contract.Status != "available" {
		return unavailable()
	}
	caps := contract.Capabilities
	narrative := []byte(m.Content.Source.Narrative)
	basis := []byte(m.Content.Statement.Basis)
	if len(narrative) == 0 || len(narrative) > caps.MaxMaterialBytes || len(m.Pack) > caps.MaxDocumentBytes || len(m.Pack) > caps.MaxMaterialBytes || len(m.Original) > caps.MaxOriginalBytes {
		return unsupported("The exact selected narrative, original or Pack exceeds this Runtime prototype's supported material bounds.")
	}
	policy := coverageRuntimePolicy(m)
	policyID := "urn:desk:policy:" + m.Content.Source.PolicyID
	prototype := map[string]any{"profile": policyCoverageProfile, "proposalVersion": policyCoverageProfileVersion, "status": "non-normative"}
	policyRaw, _ := json.Marshal(policy)
	canonicalize := func(raw []byte) ([]byte, policyCoverageRuntimeReport, error) {
		_, r, err := s.runCoverageRuntime(ctx, dir, raw, "canonicalize", "-")
		if err != nil {
			return nil, r, err
		}
		if r.Status != "encoded" || r.Document == "" || r.BytesDigest != "sha256:"+digestOf(raw) || r.CanonicalDigest != "sha256:"+digestOf([]byte(r.Document)) || !sameDialectJSON(raw, []byte(r.Document)) {
			return nil, r, errors.New("Runtime changed the submitted Policy document.")
		}
		return []byte(r.Document), r, nil
	}
	if len(policyRaw) > caps.MaxDocumentBytes {
		return unsupported("The generated experimental Policy document exceeds the supported bound.")
	}
	policyCanonical, _, err := canonicalize(policyRaw)
	if err != nil {
		return unavailable()
	}
	statement := m.Content.Statement
	row := coverageRuntimeStatement(statement)
	consumer := map[string]any{"kind": "pack", "id": m.Content.Pack.Document.ID, "version": m.Content.Pack.Document.Version, "content": coverageRuntimeContent(m.Pack, "application/json")}
	adoption := map[string]any{"prototype": prototype, "kind": "governing-adoption", "id": "urn:desk:coverage:" + digestOf([]byte(policyID+"\x00"+m.Content.Source.PublicationID+"\x00"+m.Content.Pack.PublicationID)), "version": "1.0.0", "consumer": consumer, "governingPolicies": []any{map[string]any{"policy": map[string]any{"kind": "policy", "id": policyID, "version": m.Content.Source.ArtifactVersion, "content": coverageRuntimeContent(policyCanonical, "application/json")}, "scope": map[string]any{"kind": "whole-policy"}, "coverage": []any{row}}}}
	adoptionRaw, _ := json.Marshal(adoption)
	if len(adoptionRaw) > caps.MaxDocumentBytes {
		return unsupported("The generated experimental coverage document exceeds the supported bound.")
	}
	adoptionCanonical, _, err := canonicalize(adoptionRaw)
	if err != nil {
		return unavailable()
	}
	type supplied struct {
		Content    map[string]any `json:"content"`
		DataBase64 string         `json:"dataBase64"`
	}
	materials := []supplied{}
	retainedMaterials := [][]byte{}
	seen := map[string]bool{}
	total := 0
	add := func(raw []byte, media string) {
		key := digestOf(raw) + "/" + media
		if seen[key] {
			return
		}
		seen[key] = true
		total += len(raw)
		retainedMaterials = append(retainedMaterials, raw)
		materials = append(materials, supplied{Content: coverageRuntimeContent(raw, media), DataBase64: base64.StdEncoding.EncodeToString(raw)})
	}
	add(narrative, "text/plain")
	add(m.Pack, "application/json")
	add(policyCanonical, "application/json")
	if statement.Status == "implemented" {
		add(basis, "text/plain")
	}
	if m.OriginalSource != nil {
		add(m.Original, m.OriginalSource.MediaType)
	}
	wire, _ := json.Marshal(struct {
		Materials []supplied `json:"materials"`
	}{materials})
	if len(materials) > caps.MaxMaterials || total > caps.MaxTotalMaterialBytes || len(wire) > caps.MaxMaterialsWireBytes {
		return unsupported("The complete retained coverage material exceeds this Runtime's supported check bound.")
	}
	if root.WriteFile("materials.json", wire, 0600) != nil {
		return unavailable()
	}
	raw, report, runErr := s.runCoverageRuntime(ctx, dir, adoptionCanonical, "check", "-", "--materials", "materials.json")
	if raw == nil {
		return unavailable()
	}
	if report.Kind != "governing-adoption" || report.ArtifactID != adoption["id"] || report.ArtifactVersion != "1.0.0" || report.BytesDigest != "sha256:"+digestOf(adoptionCanonical) || report.CanonicalDigest != "sha256:"+digestOf(adoptionCanonical) || report.Document != "" && !sameDialectJSON(adoptionCanonical, []byte(report.Document)) {
		return unavailable()
	}
	if report.Status == "references-valid" {
		if runErr != nil || report.ValidationScope != policyCoverageCheckScope || len(report.Diagnostics) > 0 {
			return unavailable()
		}
		return policyCoverageCheckReply{Availability: "available", Report: raw, prepared: &policyCoveragePreparedCheck{Document: adoptionCanonical, Materials: retainedMaterials}}
	}
	if report.Status != "refused" {
		return unavailable()
	}
	availability := "available"
	var exit *exec.ExitError
	if errors.As(runErr, &exit) && exit.ExitCode() == 2 {
		availability = "unsupported"
	}
	// URI support is narrower than existing local Pack publication identities.
	for _, d := range report.Diagnostics {
		if strings.HasPrefix(d.InstancePath, "/consumer/id") {
			availability = "unsupported"
		}
	}
	return policyCoverageCheckReply{Availability: availability, Report: raw, Reason: "Runtime refused the exact experimental coverage document. Review its structured findings."}
}

func coverageRuntimePolicy(m policyCoverageMaterial) map[string]any {
	narrative := []byte(m.Content.Source.Narrative)
	original := []any{}
	if m.OriginalSource != nil {
		original = append(original, map[string]any{"id": "retained-original", "source": map[string]any{"provider": "desk-user-upload", "accountId": "local", "resourceId": m.OriginalSource.Content.SHA256}, "sourceRevision": nil, "content": coverageRuntimeContent(m.Original, m.OriginalSource.MediaType)})
	}
	return map[string]any{"prototype": map[string]any{"profile": policyCoverageProfile, "proposalVersion": policyCoverageProfileVersion, "status": "non-normative"}, "kind": "policy", "id": "urn:desk:policy:" + m.Content.Source.PolicyID, "version": m.Content.Source.ArtifactVersion, "title": m.Content.Source.Title, "managementMode": "desk-managed", "content": coverageRuntimeContent(narrative, "text/plain"), "sections": []any{map[string]any{"id": "whole-policy", "title": "Whole published narrative", "anchor": map[string]any{"startByte": 0, "endByte": len(narrative), "bytesDigest": "sha256:" + digestOf(narrative)}}}, "sourceProvenance": original}
}
func coverageRuntimeStatement(statement policyCoverageStatement) map[string]any {
	row := map[string]any{"sectionId": "whole-policy", "status": statement.Status}
	switch statement.Status {
	case "implemented":
		row["consumerElements"] = statement.ConsumerElements
		row["reviewBasis"] = coverageRuntimeContent([]byte(statement.Basis), "text/plain")
	case "manual":
		row["handoff"] = statement.Handoff
	case "awaiting-interpretation":
		row["explanation"] = statement.Explanation
	case "out-of-scope":
		row["rationale"] = statement.Rationale
	}
	return row
}

func (v *policyCoverageRuntimeReport) UnmarshalJSON(raw []byte) error {
	type plain policyCoverageRuntimeReport
	var p plain
	if err := coverageDecode(raw, &p); err != nil {
		return err
	}
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	var caps map[string]json.RawMessage
	_ = json.Unmarshal(top["capabilities"], &caps)
	for _, field := range []string{"fullDocumentConformance", "approvalVerification", "executionAuthority", "liveAuthority", "networkAcquisition"} {
		if caps[field] == nil || string(caps[field]) != "false" {
			return errors.New("Runtime must explicitly disclaim unsupported authority capabilities")
		}
	}
	*v = policyCoverageRuntimeReport(p)
	return nil
}
