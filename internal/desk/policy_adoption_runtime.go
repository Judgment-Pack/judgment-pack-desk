package desk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
)

const policyAdoptionFormat = "1.0.0"
const policyAdoptionSpecRevision = "0799b37b74e35c4a86bfbf70f7bee80bdb7bbe94"
const legacyAdoptionSpecRevision = "0852c5fa7726e51b36e574a5b110e7f5e012e03d"
const policyAdoptionCheckScope = "draft-supplied-exact-bytes-anchors-and-scoped-core-references"
const legacyAdoptionCheckScope = "candidate-supplied-exact-bytes-anchors-and-scoped-core-references"

var adoptionRuntimeContract = policyRuntimeContract{
	Profile: "jps-policy-companion", Version: policyAdoptionFormat,
	SpecRevision:  policyAdoptionSpecRevision,
	Specification: "https://github.com/Judgment-Pack/judgment-pack-spec/blob/" + policyAdoptionSpecRevision + "/spec/policy.md",
	Maturity:      "draft", ValidationScope: "closed-draft-document-and-local-references",
	CheckScope: policyAdoptionCheckScope, Candidate: true, CandidateCapabilities: adoptionRuntimeCapabilities,
}

// Legacy metadata is accepted only when verifying retained historical evidence.
// It never selects a Runtime contract for fresh preparation or review.
var legacyAdoptionRuntimeContract = policyRuntimeContract{
	Profile: "jps-policy-companion", Version: policyAdoptionFormat,
	SpecRevision:  legacyAdoptionSpecRevision,
	Specification: "https://github.com/Judgment-Pack/judgment-pack-spec/blob/" + legacyAdoptionSpecRevision + "/rfcs/policy-companion-contract/README.md",
	Maturity:      "proposed-draft", ValidationScope: "closed-candidate-document-and-local-references",
	CheckScope: legacyAdoptionCheckScope, Candidate: true,
	CandidateCapabilities: legacyAdoptionRuntimeCapabilities,
}

// Pinned capability metadata is transport verification, never a second compiler.
const adoptionRuntimeCapabilities = `{
  "capability": "jps-policy-companion-offline",
  "maturity": "draft",
  "formatVersions": [
    "1.0.0"
  ],
  "consumerSpecVersions": [
    "0.2.0-draft"
  ],
  "kinds": [
    "policy",
    "governing-adoption"
  ],
  "features": [
    "exact-byte-sha256",
    "utf8-section-anchors",
    "scoped-total-coverage",
    "typed-core-pack-references",
    "optional-jps-canon-identity",
    "multiple-media-types"
  ],
  "limits": {
    "documentBytes": 262144,
    "documentDepth": 32,
    "numberLiteralCharacters": 128,
    "exponentMagnitude": 308,
    "materialBytes": 1048576,
    "originalBytes": 4194304,
    "distinctRetainedBytes": 16777216,
    "materialRegistrations": 16,
    "versionCharacters": 256,
    "sections": 64,
    "sources": 16,
    "governingPolicies": 16,
    "consumerElementsPerSection": 64,
    "textCharacters": 4096,
    "uriCharacters": 2048,
    "localIdCharacters": 128,
    "mediaTypeCharacters": 128,
    "sourceRefsPerSection": 16,
    "scopeSectionIds": 64,
    "coverageRecordsPerPolicy": 64,
    "informationalSources": 16,
    "canonicalIntegerMagnitude": 9007199254740991
  },
  "excludes": [
    "stable-format",
    "graph-consumers",
    "evaluation",
    "full-core-semantics",
    "signatures",
    "review-assertion-trust",
    "live-authority",
    "permissions",
    "currency",
    "source-fetch",
    "automatic-migration",
    "independent-interoperability-evidence"
  ],
  "refusalCodes": [
    "input-over-bound",
    "invalid-json",
    "invalid-selector",
    "unsupported-format",
    "invalid-document",
    "missing-material",
    "reference-mismatch",
    "invalid-anchor",
    "invalid-coverage",
    "invalid-consumer",
    "unsupported-canonical-identity",
    "invalid-carrier"
  ]
}
`

// Frozen pre-publication candidate metadata; never used for new Runtime calls.
const legacyAdoptionRuntimeCapabilities = `{
  "capability": "jps-policy-companion-offline",
  "maturity": "proposed-draft",
  "formatVersions": [
    "1.0.0"
  ],
  "consumerSpecVersions": [
    "0.2.0-draft"
  ],
  "kinds": [
    "policy",
    "governing-adoption"
  ],
  "features": [
    "exact-byte-sha256",
    "utf8-section-anchors",
    "scoped-total-coverage",
    "typed-core-pack-references",
    "optional-jps-canon-identity",
    "multiple-media-types"
  ],
  "limits": {
    "documentBytes": 262144,
    "documentDepth": 32,
    "numberLiteralCharacters": 128,
    "exponentMagnitude": 308,
    "materialBytes": 1048576,
    "originalBytes": 4194304,
    "distinctRetainedBytes": 16777216,
    "materialRegistrations": 16,
    "versionCharacters": 256,
    "sections": 64,
    "sources": 16,
    "governingPolicies": 16,
    "consumerElementsPerSection": 64,
    "textCharacters": 4096,
    "uriCharacters": 2048,
    "localIdCharacters": 128,
    "mediaTypeCharacters": 128,
    "sourceRefsPerSection": 16,
    "scopeSectionIds": 64,
    "coverageRecordsPerPolicy": 64,
    "informationalSources": 16,
    "canonicalIntegerMagnitude": 9007199254740991
  },
  "excludes": [
    "adopted-or-stable-format",
    "graph-consumers",
    "evaluation",
    "full-core-semantics",
    "signatures",
    "review-assertion-trust",
    "live-authority",
    "permissions",
    "currency",
    "source-fetch",
    "automatic-migration",
    "independent-interoperability-evidence"
  ],
  "refusalCodes": [
    "input-over-bound",
    "invalid-json",
    "invalid-selector",
    "unsupported-format",
    "invalid-document",
    "missing-material",
    "reference-mismatch",
    "invalid-anchor",
    "invalid-coverage",
    "invalid-consumer",
    "unsupported-canonical-identity",
    "invalid-carrier"
  ]
}
`

func validAdoptionRuntimeCapabilities(raw []byte, contract policyRuntimeContract) bool {
	return contract.CandidateCapabilities != "" && rejectDuplicateMembers(raw) == nil && sameDialectJSON(raw, []byte(contract.CandidateCapabilities))
}
func adoptionRuntimePolicy(m policyCoverageMaterial) map[string]any {
	p := coverageRuntimePolicy(m)
	delete(p, "prototype")
	p["formatVersion"] = policyAdoptionFormat
	p["managementMode"] = "workspace-managed"
	// Candidate identities are private and distinct from prototype identities.
	p["id"] = "urn:desk:policy-candidate:" + m.Content.Source.PolicyID + ":" + m.Content.Source.PublicationID
	return p
}
func (s *Server) checkPolicyAdoption(ctx context.Context, m policyAdoptionMaterial) policyAdoptionCheck {
	unavailable := func() policyAdoptionCheck {
		return policyAdoptionCheck{Availability: "unavailable", Reason: "Use a Runtime supporting official Policy draft policy-v1.0.0-draft.1 (format 1.0.0). Saved drafts and retained history remain available."}
	}
	unsupported := func() policyAdoptionCheck {
		return policyAdoptionCheck{Availability: "unsupported", Reason: "The complete chosen set exceeds the discovered Runtime candidate material or document bounds."}
	}
	where, err := os.MkdirTemp("", "jpack-desk-policy-adoption-")
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
	_, contract, err := s.runPolicyRuntime(ctx, dir, nil, "contract", adoptionRuntimeContract)
	if err != nil || contract.Status != "available" {
		return unavailable()
	}
	caps := contract.Capabilities
	canonicalize := func(raw []byte) ([]byte, error) {
		if len(raw) > caps.MaxDocumentBytes {
			return nil, errors.New("document bound")
		}
		_, r, e := s.runPolicyRuntime(ctx, dir, raw, "canonicalize", adoptionRuntimeContract, "-")
		var identity struct {
			Kind    string `json:"kind"`
			ID      string `json:"id"`
			Version string `json:"version"`
		}
		_ = json.Unmarshal(raw, &identity)
		if e != nil || r.Kind != identity.Kind || r.ArtifactID != identity.ID || r.ArtifactVersion != identity.Version || r.Status != "encoded" || r.Document == "" || r.BytesDigest != "sha256:"+digestOf(raw) || r.CanonicalDigest != "sha256:"+digestOf([]byte(r.Document)) || !sameDialectJSON(raw, []byte(r.Document)) {
			return nil, errors.New("inconsistent canonicalization")
		}
		return []byte(r.Document), nil
	}
	type supplied struct {
		Content    map[string]any `json:"content"`
		DataBase64 string         `json:"dataBase64"`
	}
	suppliedMaterials := []supplied{}
	retained := [][]byte{}
	seen := map[string]bool{}
	distinct := map[string]bool{}
	total := 0
	add := func(raw []byte, media string, original bool) bool {
		bound := caps.MaxMaterialBytes
		if original {
			bound = caps.MaxOriginalBytes
		}
		if len(raw) > bound {
			return false
		}
		key := digestOf(raw) + "/" + media
		if seen[key] {
			return true
		}
		seen[key] = true
		if !distinct[digestOf(raw)] {
			total += len(raw)
			retained = append(retained, raw)
			distinct[digestOf(raw)] = true
		}
		suppliedMaterials = append(suppliedMaterials, supplied{coverageRuntimeContent(raw, media), base64.StdEncoding.EncodeToString(raw)})
		return len(suppliedMaterials) <= caps.MaxMaterials && total <= caps.MaxTotalMaterialBytes
	}
	governing := []any{}
	if len(m.Materials) < 1 || len(m.Materials) > policyAdoptionSetLimit {
		return unsupported()
	}
	for _, v := range m.Materials {
		if !add([]byte(v.Content.Source.Narrative), "text/plain", false) || !add(v.Pack, "application/json", false) {
			return unsupported()
		}
		if v.Content.Statement.Status == "implemented" && !add([]byte(v.Content.Statement.Basis), "text/plain", false) {
			return unsupported()
		}
		if v.OriginalSource != nil && !add(v.Original, v.OriginalSource.MediaType, true) {
			return unsupported()
		}
		p := adoptionRuntimePolicy(v)
		raw, _ := json.Marshal(p)
		if len(raw) > caps.MaxDocumentBytes {
			return unsupported()
		}
		canonical, e := canonicalize(raw)
		if e != nil {
			return unavailable()
		}
		if !add(canonical, "application/json", false) {
			return unsupported()
		}
		governing = append(governing, map[string]any{"policy": map[string]any{"kind": "policy", "id": p["id"], "version": v.Content.Source.ArtifactVersion, "content": coverageRuntimeContent(canonical, "application/json")}, "scope": map[string]any{"kind": "whole-policy"}, "coverage": []any{coverageRuntimeStatement(v.Content.Statement)}})
	}
	pack := m.Materials[0].Content.Pack.Document
	doc := map[string]any{"formatVersion": policyAdoptionFormat, "kind": "governing-adoption", "id": m.PortableID, "version": "1.0.0", "consumer": map[string]any{"kind": "pack", "id": pack.ID, "version": pack.Version, "content": coverageRuntimeContent(m.Materials[0].Pack, "application/json")}, "governingPolicies": governing}
	raw, _ := json.Marshal(doc)
	if len(raw) > caps.MaxDocumentBytes {
		return unsupported()
	}
	canonical, e := canonicalize(raw)
	if e != nil {
		return unavailable()
	}
	wire, _ := json.Marshal(struct {
		Materials []supplied `json:"materials"`
	}{suppliedMaterials})
	if len(wire) > caps.MaxMaterialsWireBytes {
		return unsupported()
	}
	if root.WriteFile("materials.json", wire, 0600) != nil {
		return unavailable()
	}
	reportRaw, r, runErr := s.runPolicyRuntime(ctx, dir, canonical, "check", adoptionRuntimeContract, "-", "--materials", "materials.json")
	if reportRaw == nil || r.Kind != "governing-adoption" || r.ArtifactID != m.PortableID || r.ArtifactVersion != "1.0.0" || r.BytesDigest != "sha256:"+digestOf(canonical) || r.CanonicalDigest != r.BytesDigest || r.Document != "" && r.Document != string(canonical) {
		return unavailable()
	}
	if r.Status == "references-valid" && runErr == nil && r.ValidationScope == policyAdoptionCheckScope && len(r.Diagnostics) == 0 && !r.DiagnosticsTruncated {
		return policyAdoptionCheck{Availability: "available", Report: reportRaw, prepared: &policyAdoptionPrepared{Document: canonical, Materials: retained, Report: reportRaw}}
	}
	if r.Status != "refused" {
		return unavailable()
	}
	availability := "available"
	var exit *exec.ExitError
	if errors.As(runErr, &exit) && exit.ExitCode() == 2 {
		availability = "unsupported"
	}
	return policyAdoptionCheck{Availability: availability, Report: reportRaw, Reason: "Runtime refused the complete chosen candidate set. No adoption was prepared."}
}
func adoptionCheckAccepted(c policyAdoptionCheck) bool {
	var report policyCoverageRuntimeReport
	return c.Availability == "available" && coverageDecode(c.Report, &report) == nil && validPolicyRuntimeReport(report, "check", adoptionRuntimeContract) && report.Status == "references-valid" && report.ValidationScope == policyAdoptionCheckScope && len(report.Diagnostics) == 0 && !report.DiagnosticsTruncated
}
