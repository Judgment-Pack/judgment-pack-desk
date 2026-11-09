package desk

// This private selected owner checks supplied snapshots only. Runtime retains
// document/pack semantics; these guards bind its reports to the exact inputs.
// Ordinary authoring and the held assistant Runtime lifecycle do not enter here.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	agentcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

type retainedDialectRuntimeEvidence struct {
	Operation string
	// Complete stdout, including its original framing. Nil is never a clipped
	// report: the existing command owner discards output on overflow/cancel.
	Stdout    []byte
	RunFailed bool
}

type retainedDialectAuthoringCheck struct {
	Selection                         retainedConversionSelector
	Discovery, Validation, Coverage   retainedDialectRuntimeEvidence
	Document, Digest                  string
	FactTargets, EvidenceRequirements []string
	DocumentLimit, PackLimit          int64
	Accepted                          bool
}

var errRetainedDialectReport = errors.New("Runtime returned inconsistent selected dialect check evidence.")

// checkRetainedDialectAuthoring is an explicit host opt-in. A document, report,
// model argument or historical version cannot select this path for its caller.
// No result grants permission to write, acquire, prepare or execute anything.
func (s *Server) checkRetainedDialectAuthoring(ctx context.Context, selection retainedConversionSelector, document, pack []byte) (retainedDialectAuthoringCheck, error) {
	var result retainedDialectAuthoringCheck
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := retainedValidateSelector(selection, "document-validation"); err != nil {
		return result, err
	}
	if len(selection.Roles) != 1 {
		return result, retainedContractFailure("capability-unsupported")
	}
	if len(document) > agentcontext.DialectDocumentBytes || len(pack) > agentcontext.DialectPackBytes {
		return result, retainedContractFailure("input-over-bound")
	}
	result.Selection = selection
	result.Selection.Roles = slices.Clone(selection.Roles)
	document, pack = bytes.Clone(document), bytes.Clone(pack)
	selectors, err := retainedRuntimeSelectors("contract")
	if err != nil {
		return result, err
	}
	where, err := os.MkdirTemp("", "jpack-desk-retained-dialect-")
	if err != nil {
		return result, errDialectChecksUnavailable
	}
	defer os.RemoveAll(where)
	root, err := os.OpenRoot(where)
	if err != nil {
		return result, errDialectChecksUnavailable
	}
	defer root.Close()
	file, err := root.Open(".")
	if err != nil {
		return result, errDialectChecksUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return result, errDialectChecksUnavailable
	}
	dir := heldDir{file: file, path: where, info: info}
	run := func(operation string, input []byte, args ...string) (retainedDialectRuntimeEvidence, error) {
		argv := []string{"dialect", operation, "--draft-contract", selectors["draftContract"], "--draft-profile", selectors["draftProfile"]}
		argv = append(argv, args...)
		argv = append(argv, "--format", "json")
		out, runErr := runRuntimeInputLimit(ctx, s.cfg.JpackBin, dir, input, dialectAnswerLimit, argv...)
		evidence := retainedDialectRuntimeEvidence{Operation: operation, Stdout: bytes.Clone(out), RunFailed: runErr != nil}
		if err := ctx.Err(); err != nil {
			return evidence, err
		}
		// An overflow, timeout or launch failure has no complete stdout. Preserve
		// a complete nonzero-exit report for its structured refusal diagnostics.
		if runErr != nil && out == nil {
			return evidence, errDialectChecksUnavailable
		}
		return evidence, nil
	}
	result.Discovery, err = run("contract", nil)
	if err != nil {
		return result, err
	}
	if result.Discovery.RunFailed {
		return result, errDialectChecksUnavailable
	}
	if _, err = readRetainedDialectRuntimeReport("contract", result.Discovery.Stdout, nil); err != nil {
		return result, err
	}
	discovery, err := readRetainedRuntimeDiscovery(result.Discovery.Stdout)
	if err != nil {
		return result, err
	}
	admission, err := negotiateRetainedRoles(result.Selection, []string{"document-validation"}, false, retainedOwnerReports{Runtime: discovery.capabilities}, nil)
	if err != nil {
		return result, err
	}
	result.DocumentLimit, err = retainedArtifactLimit(admission, "documentBytes")
	if err != nil {
		return result, err
	}
	result.DocumentLimit = min(result.DocumentLimit, int64(agentcontext.DialectDocumentBytes))
	result.PackLimit = min(discovery.maxPackBytes, int64(agentcontext.DialectPackBytes))
	if int64(len(document)) > result.DocumentLimit || int64(len(pack)) > result.PackLimit {
		return result, retainedContractFailure("input-over-bound")
	}
	result.Validation, err = run("validate", document, "-")
	if err != nil {
		return result, err
	}
	validation, err := readRetainedDialectRuntimeReport("validate", result.Validation.Stdout, &discovery)
	if err != nil {
		return result, err
	}
	if !retainedDialectStatus(validation, "document-valid") {
		return result, nil
	}
	if result.Validation.RunFailed {
		return result, errDialectChecksUnavailable
	}
	if _, _, err = retainedDialectCanonicalReport(document, validation, result.DocumentLimit, "document"); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = root.WriteFile("pack.json", pack, 0o600); err != nil {
		return result, errDialectChecksUnavailable
	}
	result.Coverage, err = run("check", document, "-", "--pack", "pack.json")
	if err != nil {
		return result, err
	}
	coverage, err := readRetainedDialectRuntimeReport("check", result.Coverage.Stdout, &discovery)
	if err != nil {
		return result, err
	}
	if !retainedDialectStatus(coverage, "complete", "review-required") {
		return result, nil
	}
	if result.Coverage.RunFailed {
		return result, errDialectChecksUnavailable
	}
	result.Document, result.Digest, result.FactTargets, result.EvidenceRequirements, err = validateRetainedDialectReportPair(document, pack, validation, coverage, result.DocumentLimit)
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	result.Accepted = true
	return result, nil
}

// The envelope remains open for Runtime-owned auxiliary data. Every consumed
// authority member has an explicit type; nested tuple/limit/capability objects
// are closed. Unknown diagnostics/metadata are retained, never interpreted as
// authority. Later MCP integration must supply the actual command spelling.
func readRetainedDialectRuntimeReport(operation string, raw []byte, discovery *retainedRuntimeDiscovery) (*retainedJSONNode, error) {
	if len(raw) == 0 || len(raw) > dialectAnswerLimit {
		return nil, errDialectChecksUnavailable
	}
	node, err := parseRetainedJSON(raw)
	if err != nil || node.kind != '{' {
		return nil, errRetainedDialectReport
	}
	for key, expected := range map[string]string{"outputVersion": "2", "contractVersion": "1", "command": "dialect " + operation} {
		got, err := retainedMemberString(node, key)
		if err != nil || got != expected {
			return nil, errRetainedDialectReport
		}
	}
	if err = retainedRuntimeIdentity(node); err != nil {
		return nil, err
	}
	if _, err = retainedReadLimits(node.member("limits"), "document-validation"); err != nil {
		return nil, err
	}
	if node.member("diagnostics") == nil || node.member("diagnostics").kind != '[' {
		return nil, errRetainedDialectReport
	}
	switch operation {
	case "contract":
		if !retainedDialectStatus(node, "available") {
			return nil, retainedContractFailure("capability-unsupported")
		}
		capabilities := node.member("capabilities")
		contracts := capabilities.member("conversionContracts")
		if contracts == nil || contracts.kind != '[' || len(contracts.elements) != 1 || !retainedExactObject(contracts.elements[0], []string{"contract", "spec", "profile", "role", "limits"}) {
			return nil, errRetainedDialectReport
		}
		operations, err := retainedDialectStrings(capabilities.member("operations"))
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, name := range operations {
			if seen[name] {
				return nil, errRetainedDialectReport
			}
			seen[name] = true
		}
	case "validate":
		if !retainedDialectStatus(node, "document-valid", "refused") {
			return nil, errRetainedDialectReport
		}
	case "check":
		if !retainedDialectStatus(node, "complete", "review-required", "incomplete", "not-applicable", "refused") {
			return nil, errRetainedDialectReport
		}
	default:
		return nil, retainedContractFailure("capability-unsupported")
	}
	if discovery != nil {
		if err = checkRetainedRuntimeReportIdentity(raw, *discovery); err != nil {
			return nil, err
		}
	}
	return node, nil
}

func retainedDialectStatus(node *retainedJSONNode, statuses ...string) bool {
	status, err := retainedMemberString(node, "status")
	return err == nil && slices.Contains(statuses, status)
}

func retainedDialectStrings(node *retainedJSONNode) ([]string, error) {
	if node == nil || node.kind != '[' {
		return nil, errRetainedDialectReport
	}
	result := make([]string, 0, len(node.elements))
	for _, element := range node.elements {
		value, err := element.scalarString()
		if err != nil {
			return nil, errRetainedDialectReport
		}
		result = append(result, value)
	}
	return result, nil
}

type retainedDialectInventory struct {
	ID, Version, SpecVersion string
	Facts, Evidence          []string
}

// Runtime's inventory deliberately includes condition-shaped literal data and
// every op/path pair, not just op==fact. This is a consistency walk, not a pack
// semantic validator or a replacement for Runtime's accepted coverage status.
func retainedDialectPackInventory(raw []byte) (retainedDialectInventory, error) {
	var result retainedDialectInventory
	if len(raw) > agentcontext.DialectPackBytes {
		return result, retainedContractFailure("input-over-bound")
	}
	root, err := parseRetainedJSON(raw)
	if err != nil || root.kind != '{' {
		return result, errRetainedDialectReport
	}
	for key, destination := range map[string]*string{"id": &result.ID, "version": &result.Version, "specVersion": &result.SpecVersion} {
		value, err := retainedMemberString(root, key)
		if err != nil || value == "" {
			return result, errRetainedDialectReport
		}
		*destination = value
	}
	result.Facts, result.Evidence = []string{}, []string{}
	var walk func(*retainedJSONNode) error
	walk = func(node *retainedJSONNode) error {
		if node.kind == '{' {
			if path := node.member("path"); node.member("op") != nil && path != nil && path.kind == '"' {
				value, err := path.scalarString()
				if err != nil {
					return errRetainedDialectReport
				}
				result.Facts = append(result.Facts, value)
			}
			for _, child := range node.members {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		for _, child := range node.elements {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err = walk(root); err != nil {
		return result, err
	}
	if requirements := root.member("evidenceRequirements"); requirements != nil {
		if requirements.kind != '[' {
			return result, errRetainedDialectReport
		}
		for _, row := range requirements.elements {
			id, err := retainedMemberString(row, "id")
			if err != nil || id == "" {
				return result, errRetainedDialectReport
			}
			result.Evidence = append(result.Evidence, id)
		}
	}
	sort.Strings(result.Facts)
	result.Facts = slices.Compact(result.Facts)
	sort.Strings(result.Evidence)
	result.Evidence = slices.Compact(result.Evidence)
	return result, nil
}

func retainedDialectCanonicalReport(original []byte, report *retainedJSONNode, limit int64, scope string) (string, string, error) {
	if version := report.member("dialectVersion"); version == nil || string(version.raw) != "2" {
		return "", "", errRetainedDialectReport
	}
	selected := retainedInstalledSelector()
	wantScope := "experimental-retained-column-" + selected.Profile + "-" + scope
	gotScope, err := retainedMemberString(report, "validationScope")
	if err != nil || gotScope != wantScope {
		return "", "", errRetainedDialectReport
	}
	document, err := retainedMemberString(report, "document")
	if err != nil || document == "" || int64(len(document)) > limit {
		return "", "", errRetainedDialectReport
	}
	digest, err := retainedMemberString(report, "digest")
	if err != nil || digest != retainedDigest([]byte(document)) {
		return "", "", errRetainedDialectReport
	}
	if !retainedDialectSameDocument(original, []byte(document)) {
		return "", "", errRetainedDialectReport
	}
	return document, digest, nil
}

// Runtime owns canonical spelling (its JPS order is UTF-8, not Agent's UTF-16
// CanonicalJSON order). Compare strict values with the existing UseNumber
// pattern, retaining exact report bytes/digest separately. Only -0 may change
// numeric spelling within Runtime's safe-integer canonical document domain.
func retainedDialectSameDocument(original, returned []byte) bool {
	decode := func(raw []byte) (any, error) {
		root, err := parseRetainedJSON(raw)
		if err != nil || root.kind != '{' || jsonscalar.ValidateEscapes(raw) != nil {
			return nil, errRetainedDialectReport
		}
		var domain func(*retainedJSONNode) bool
		domain = func(node *retainedJSONNode) bool {
			if node.kind == '-' || node.kind >= '0' && node.kind <= '9' {
				literal := string(node.raw)
				if strings.ContainsAny(literal, ".eE") {
					return false
				}
				n, err := strconv.ParseInt(literal, 10, 64)
				if err != nil || n < -9007199254740991 || n > 9007199254740991 {
					return false
				}
			}
			for _, child := range node.members {
				if !domain(child) {
					return false
				}
			}
			for _, child := range node.elements {
				if !domain(child) {
					return false
				}
			}
			return true
		}
		if !domain(root) {
			return nil, errRetainedDialectReport
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		return dialectComparable(value), nil
	}
	left, err := decode(original)
	if err != nil {
		return false
	}
	right, err := decode(returned)
	if err != nil {
		return false
	}
	a, err := json.Marshal(left)
	if err != nil {
		return false
	}
	b, err := json.Marshal(right)
	return err == nil && bytes.Equal(a, b)
}

func validateRetainedDialectReportPair(document, pack []byte, validation, coverage *retainedJSONNode, limit int64) (string, string, []string, []string, error) {
	fail := func() (string, string, []string, []string, error) {
		return "", "", nil, nil, errRetainedDialectReport
	}
	if !retainedDialectStatus(validation, "document-valid") || !retainedDialectStatus(coverage, "complete", "review-required") {
		return fail()
	}
	canonical, digest, err := retainedDialectCanonicalReport(document, validation, limit, "document")
	if err != nil {
		return fail()
	}
	checked, checkedDigest, err := retainedDialectCanonicalReport(document, coverage, limit, "document-and-declared-pack-inventory")
	if err != nil || checked != canonical || checkedDigest != digest {
		return fail()
	}
	inventory, err := retainedDialectPackInventory(pack)
	if err != nil {
		return fail()
	}
	compatibility := coverage.member("compatibility")
	for key, want := range map[string]string{"scope": "declared-inventory-coverage", "status": mustRetainedDialectStatus(coverage)} {
		got, err := retainedMemberString(compatibility, key)
		if err != nil || got != want {
			return fail()
		}
	}
	for key, want := range map[string]string{"inventoryOverApproximates": "true", "valueTypesChecked": "false", "executionChecked": "false"} {
		if member := compatibility.member(key); member == nil || string(member.raw) != want {
			return fail()
		}
	}
	identity := compatibility.member("pack")
	for key, want := range map[string]string{"id": inventory.ID, "version": inventory.Version, "specVersion": inventory.SpecVersion, "sha256": digestOf(pack)} {
		got, err := retainedMemberString(identity, key)
		if err != nil || got != want {
			return fail()
		}
	}
	if !retainedDialectStatus(coverage.member("packValidation"), "valid") {
		return fail()
	}
	for _, entry := range []struct {
		name string
		want []string
	}{{"facts", inventory.Facts}, {"evidence", inventory.Evidence}} {
		rows := compatibility.member(entry.name)
		if rows == nil || rows.kind != '[' || len(rows.elements) != len(entry.want) {
			return fail()
		}
		for i, row := range rows.elements {
			target, err := retainedMemberString(row, "target")
			if err != nil || target != entry.want[i] || !retainedDialectStatus(row, "mapped", "partial", "acknowledged") {
				return fail()
			}
			if entry.name == "evidence" && retainedDialectStatus(row, "partial") {
				return fail()
			}
			for _, key := range []string{"producers", "acknowledgedBy"} {
				if _, err = retainedDialectStrings(row.member(key)); err != nil {
					return fail()
				}
			}
		}
	}
	return canonical, digest, slices.Clone(inventory.Facts), slices.Clone(inventory.Evidence), nil
}

func mustRetainedDialectStatus(node *retainedJSONNode) string {
	status, _ := retainedMemberString(node, "status")
	return status
}
