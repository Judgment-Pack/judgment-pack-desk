package desk

// This adapter reads selected wire metadata. Installation trust still comes
// from the component lock/artifact verification and each actual owner, never
// from a caller/report agreement or a frame's contents.
import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"

	agentcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

type retainedConversionSpec = agentcontext.DialectConversionSpec

type retainedConversionSelector struct {
	Contract string                 `json:"contract"`
	Spec     retainedConversionSpec `json:"spec"`
	Profile  string                 `json:"profile"`
	Roles    []string               `json:"roles"`
}
type retainedConversionCapability struct {
	Contract        string                 `json:"contract"`
	Spec            retainedConversionSpec `json:"spec"`
	Profile         string                 `json:"profile"`
	Role            string                 `json:"role"`
	Limits          map[string]int64       `json:"limits"`
	ProducerGrammar *string                `json:"producerGrammar,omitempty"`
}
type retainedContractError struct{ Code string }

func (e *retainedContractError) Error() string {
	switch e.Code {
	case "capability-unsupported":
		return "A required owner does not support the exact retained-column selection."
	case "input-over-bound":
		return "A complete retained-column artifact exceeds its negotiated limit."
	default:
		return "The retained-column selector or capability is malformed."
	}
}
func retainedContractFailure(code string) error { return &retainedContractError{Code: code} }
func retainedContractCode(err error) string {
	var failure *retainedContractError
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}

const retainedProducerGrammar = "desk-reviewed-input/1"

var retainedSpecRevision = regexp.MustCompile(`^[a-f0-9]{40}$`)
var retainedSpecDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func retainedRoleOrder() []string {
	return []string{"document-validation", "preview", "preparation", "preparation-verification", "producer-carrier"}
}

// The immutable tuple is the published Agent package helper, not another copy
// of design literals. The selected Runner supports Base only.
func retainedInstalledSelector(roles ...string) retainedConversionSelector {
	installed := agentcontext.DialectRetainedColumnDraft()
	return retainedConversionSelector{Contract: installed.Contract, Spec: installed.Spec, Profile: installed.Profile, Roles: append([]string(nil), roles...)}
}
func retainedRoleIndex(role string) int {
	for i, known := range retainedRoleOrder() {
		if role == known {
			return i
		}
	}
	return -1
}
func retainedGrammarRole(role string) bool {
	return role == "preparation" || role == "preparation-verification" || role == "producer-carrier"
}

// Fresh maps prevent callers from mutating this installation's profile caps.
// Values are complete artifacts from the selected published Spec/Runner Base.
func retainedRoleCaps(role string) map[string]int64 {
	switch role {
	case "document-validation":
		return map[string]int64{"documentBytes": 1048576, "frameBytes": 2097152}
	case "preview":
		return map[string]int64{"documentBytes": 1048576, "combinedBytes": 12582912, "frameBytes": 2097152, "profilesBytes": 81920, "requestBodyBytes": 16777216, "inputReplyBytes": 37748736}
	case "preparation", "preparation-verification":
		return map[string]int64{"documentBytes": 1048576, "combinedBytes": 12582912, "frameBytes": 2097152, "preparationBytes": 16777216, "recordBytes": 9437184, "profilesBytes": 81920, "requestBodyBytes": 16777216, "inputReplyBytes": 37748736, "releaseStoredBytes": 41943040, "runStoredBytes": 41943040, "exportBaseBytes": 75497472, "exportWireBytes": 90177536}
	case "producer-carrier":
		return map[string]int64{"originalSnapshotBytes": 524288, "carrierBytes": 3145728, "grantBytes": 3145728, "adapterStdoutBytes": 3145728, "responseBytes": 5242880, "profilesBytes": 81920}
	}
	return nil
}
func retainedExactObject(node *retainedJSONNode, required []string, optional ...string) bool {
	if node == nil || node.kind != '{' {
		return false
	}
	count := len(required)
	for _, key := range required {
		if node.member(key) == nil {
			return false
		}
	}
	for _, key := range optional {
		if node.member(key) != nil {
			count++
		}
	}
	return len(node.members) == count
}
func retainedReadIdentity(node *retainedJSONNode) (string, retainedConversionSpec, string, error) {
	var spec retainedConversionSpec
	malformed := retainedContractFailure("input-malformed")
	contract, err := retainedMemberString(node, "contract")
	if err != nil {
		return "", spec, "", malformed
	}
	profile, err := retainedMemberString(node, "profile")
	if err != nil {
		return "", spec, "", malformed
	}
	pin := node.member("spec")
	if !retainedExactObject(pin, []string{"version", "revision", "schemaSha256", "corpusSha256"}) {
		return "", spec, "", malformed
	}
	for key, destination := range map[string]*string{"version": &spec.Version, "revision": &spec.Revision, "schemaSha256": &spec.SchemaSHA256, "corpusSha256": &spec.CorpusSHA256} {
		value, err := retainedMemberString(pin, key)
		if err != nil {
			return "", spec, "", malformed
		}
		*destination = value
	}
	if spec.Version == "" || !retainedSpecRevision.MatchString(spec.Revision) || !retainedSpecDigest.MatchString(spec.SchemaSHA256) || !retainedSpecDigest.MatchString(spec.CorpusSHA256) {
		return "", spec, "", malformed
	}
	expected := retainedInstalledSelector()
	if contract != expected.Contract || profile != expected.Profile || spec != expected.Spec {
		return "", spec, "", retainedContractFailure("capability-unsupported")
	}
	return contract, spec, profile, nil
}
func retainedPositiveLimit(node *retainedJSONNode, zero bool) (int64, error) {
	bad := retainedContractFailure("input-malformed")
	if node == nil || len(node.raw) > 17 {
		return 0, bad
	}
	raw := string(node.raw)
	if zero && raw == "0" {
		return 0, nil
	}
	if !retainedPositiveInteger.MatchString(raw) {
		return 0, bad
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 || value > 9007199254740991 {
		return 0, bad
	}
	return value, nil
}
func retainedReadLimits(node *retainedJSONNode, role string) (map[string]int64, error) {
	caps := retainedRoleCaps(role)
	if caps == nil {
		return nil, retainedContractFailure("capability-unsupported")
	}
	names := make([]string, 0, len(caps))
	for key := range caps {
		names = append(names, key)
	}
	if !retainedExactObject(node, names) {
		return nil, retainedContractFailure("input-malformed")
	}
	limits := map[string]int64{}
	for key := range caps {
		value, err := retainedPositiveLimit(node.member(key), false)
		if err != nil {
			return nil, err
		}
		limits[key] = value
	}
	return limits, nil
}
func retainedValidateSelector(selection retainedConversionSelector, required ...string) error {
	expected := retainedInstalledSelector()
	if selection.Contract != expected.Contract || selection.Profile != expected.Profile || selection.Spec != expected.Spec {
		return retainedContractFailure("capability-unsupported")
	}
	if len(selection.Roles) == 0 {
		return retainedContractFailure("input-malformed")
	}
	last := -1
	for _, role := range selection.Roles {
		at := retainedRoleIndex(role)
		if at < 0 {
			return retainedContractFailure("capability-unsupported")
		}
		if at <= last {
			return retainedContractFailure("input-malformed")
		}
		last = at
	}
	for _, role := range required {
		if !contains(selection.Roles, role) {
			return retainedContractFailure("capability-unsupported")
		}
	}
	return nil
}
func parseRetainedSelector(raw []byte, required ...string) (retainedConversionSelector, error) {
	var selection retainedConversionSelector
	node, err := parseRetainedJSON(raw)
	if err != nil || !retainedExactObject(node, []string{"contract", "spec", "profile", "roles"}) {
		return selection, retainedContractFailure("input-malformed")
	}
	selection.Contract, selection.Spec, selection.Profile, err = retainedReadIdentity(node)
	if err != nil {
		return selection, err
	}
	roles := node.member("roles")
	if roles.kind != '[' {
		return selection, retainedContractFailure("input-malformed")
	}
	for _, role := range roles.elements {
		value, err := role.scalarString()
		if err != nil {
			return selection, retainedContractFailure("input-malformed")
		}
		selection.Roles = append(selection.Roles, value)
	}
	return selection, retainedValidateSelector(selection, required...)
}
func parseRetainedCapabilities(raw []byte) ([]retainedConversionCapability, error) {
	node, err := parseRetainedJSON(raw)
	if err != nil || node.kind != '[' || len(node.elements) > 32 {
		return nil, retainedContractFailure("input-malformed")
	}
	capabilities := make([]retainedConversionCapability, 0, len(node.elements))
	seen := map[string]bool{}
	for _, entry := range node.elements {
		if !retainedExactObject(entry, []string{"contract", "spec", "profile", "role", "limits"}, "producerGrammar") {
			return nil, retainedContractFailure("input-malformed")
		}
		var capability retainedConversionCapability
		capability.Contract, capability.Spec, capability.Profile, err = retainedReadIdentity(entry)
		if err != nil {
			return nil, err
		}
		capability.Role, err = retainedMemberString(entry, "role")
		if err != nil {
			return nil, retainedContractFailure("input-malformed")
		}
		if retainedRoleIndex(capability.Role) < 0 {
			return nil, retainedContractFailure("capability-unsupported")
		}
		capability.Limits, err = retainedReadLimits(entry.member("limits"), capability.Role)
		if err != nil {
			return nil, err
		}
		if grammar := entry.member("producerGrammar"); grammar != nil {
			value, err := grammar.scalarString()
			if err != nil || value == "" || !retainedGrammarRole(capability.Role) {
				return nil, retainedContractFailure("input-malformed")
			}
			if value != retainedProducerGrammar {
				return nil, retainedContractFailure("capability-unsupported")
			}
			capability.ProducerGrammar = &value
		}
		identity := capability.Role + "/absent"
		if capability.ProducerGrammar != nil {
			identity = capability.Role + "/present/" + *capability.ProducerGrammar
		}
		if seen[identity] {
			return nil, retainedContractFailure("input-malformed")
		}
		seen[identity] = true
		capabilities = append(capabilities, capability)
	}
	return capabilities, nil
}

// The host supplies reports from independently verified owner routes/profiles.
// Keeping slots separate prevents Runtime discovery being mistaken for Runner,
// producer or offline verifier support. Parsing any slot does not authenticate it.
type retainedOwnerReports struct {
	Runtime  []retainedConversionCapability
	Runner   []retainedConversionCapability
	Producer []retainedConversionCapability
	Verifier []retainedConversionCapability
}
type retainedAdmission struct {
	selection retainedConversionSelector
	limits    map[string]map[string]int64
	generated bool
}

func negotiateRetainedRoles(selection retainedConversionSelector, required []string, generated bool, reports retainedOwnerReports, downward map[string]map[string]int64) (*retainedAdmission, error) {
	if err := retainedValidateSelector(selection, required...); err != nil {
		return nil, err
	}
	admission := &retainedAdmission{selection: selection, limits: map[string]map[string]int64{}, generated: generated}
	for role := range downward {
		if !contains(required, role) {
			return nil, retainedContractFailure("input-malformed")
		}
	}
	for _, role := range required {
		var report []retainedConversionCapability
		switch role {
		case "document-validation":
			report = reports.Runtime
		case "preview", "preparation":
			report = reports.Runner
		case "producer-carrier":
			report = reports.Producer
		case "preparation-verification":
			report = reports.Verifier
		default:
			return nil, retainedContractFailure("capability-unsupported")
		}
		if len(report) == 0 {
			return nil, retainedContractFailure("capability-unsupported")
		}
		// Revalidate even programmatically supplied metadata; typed structs cannot
		// bypass the lexical/closed report readers or mutate a prior admission.
		encoded, err := json.Marshal(report)
		if err != nil {
			return nil, retainedContractFailure("input-malformed")
		}
		validated, err := parseRetainedCapabilities(encoded)
		if err != nil {
			return nil, err
		}
		limits := retainedRoleCaps(role)
		matched := false
		for _, capability := range validated {
			if capability.Role != role || generated && retainedGrammarRole(role) && (capability.ProducerGrammar == nil || *capability.ProducerGrammar != retainedProducerGrammar) {
				continue
			}
			matched = true
			// All eligible reports constrain the same owner role. This conservative
			// minimum is deterministic when ordinary and grammar identities coexist.
			for key, cap := range limits {
				limits[key] = min(cap, capability.Limits[key])
			}
		}
		if !matched {
			return nil, retainedContractFailure("capability-unsupported")
		}
		if caller, exists := downward[role]; exists {
			if len(caller) != len(limits) {
				return nil, retainedContractFailure("input-malformed")
			}
			for key, cap := range limits {
				n := caller[key]
				if n < 1 || n > 9007199254740991 {
					return nil, retainedContractFailure("input-malformed")
				}
				limits[key] = min(cap, n)
			}
		}
		admission.limits[role] = limits
	}
	admission.selection.Roles = append([]string(nil), selection.Roles...)
	return admission, nil
}

func readRetainedRunnerCapabilities(raw []byte) ([]retainedConversionCapability, error) {
	node, err := parseRetainedJSON(raw)
	if err != nil || node.kind != '{' {
		return nil, retainedContractFailure("input-malformed")
	}
	capabilities := node.member("conversionContracts")
	if capabilities == nil {
		return nil, retainedContractFailure("capability-unsupported")
	}
	return parseRetainedCapabilities(capabilities.raw)
}

type retainedRuntimeDiscovery struct {
	capabilities []retainedConversionCapability
	limits       map[string]int64
	maxPackBytes int64
}

func retainedRuntimeIdentity(node *retainedJSONNode) error {
	if _, _, _, err := retainedReadIdentity(node); err != nil {
		return err
	}
	installed := retainedInstalledSelector()
	for key, want := range map[string]string{"role": "document-validation", "maturity": "draft", "specRevision": installed.Spec.Revision, "specification": "https://github.com/Judgment-Pack/judgment-pack-spec/blob/" + installed.Spec.Revision + "/spec/retained-column.md"} {
		got, err := retainedMemberString(node, key)
		if err != nil || got != want {
			return retainedContractFailure("capability-unsupported")
		}
	}
	return nil
}
func readRetainedRuntimeDiscovery(raw []byte) (retainedRuntimeDiscovery, error) {
	var discovery retainedRuntimeDiscovery
	node, err := parseRetainedJSON(raw)
	if err != nil || node.kind != '{' {
		return discovery, retainedContractFailure("input-malformed")
	}
	if err := retainedRuntimeIdentity(node); err != nil {
		return discovery, err
	}
	status, err := retainedMemberString(node, "status")
	if err != nil || status != "available" {
		return discovery, retainedContractFailure("capability-unsupported")
	}
	discovery.limits, err = retainedReadLimits(node.member("limits"), "document-validation")
	if err != nil {
		return discovery, err
	}
	capabilities := node.member("capabilities")
	entries := capabilities.member("conversionContracts")
	if entries == nil {
		return discovery, retainedContractFailure("capability-unsupported")
	}
	discovery.capabilities, err = parseRetainedCapabilities(entries.raw)
	if err != nil {
		return discovery, err
	}
	if len(discovery.capabilities) != 1 || discovery.capabilities[0].Role != "document-validation" || !retainedSameLimits(discovery.capabilities[0].Limits, discovery.limits) {
		return discovery, retainedContractFailure("capability-unsupported")
	}
	scope, err := retainedMemberString(capabilities, "packCheckScope")
	if err != nil || scope != "declared-inventory-coverage" {
		return discovery, retainedContractFailure("capability-unsupported")
	}
	operations := capabilities.member("operations")
	if operations == nil || operations.kind != '[' {
		return discovery, retainedContractFailure("capability-unsupported")
	}
	found := map[string]bool{}
	for _, operation := range operations.elements {
		name, err := operation.scalarString()
		if err != nil {
			return discovery, retainedContractFailure("input-malformed")
		}
		found[name] = true
	}
	if !found["validate"] || !found["check"] {
		return discovery, retainedContractFailure("capability-unsupported")
	}
	discovery.maxPackBytes, err = retainedPositiveLimit(capabilities.member("maxPackBytes"), false)
	if err != nil {
		return discovery, err
	}
	// Package authoring admission remains independently at one MiB.
	discovery.maxPackBytes = min(discovery.maxPackBytes, 1048576)
	return discovery, nil
}
func retainedSameLimits(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

// Each validate/check report must independently match discovery's installed
// identity and exact reported limits. Existing/package readers still own the
// canonical document, pack coverage and accepted/refused semantic decision.
func checkRetainedRuntimeReportIdentity(raw []byte, discovery retainedRuntimeDiscovery) error {
	node, err := parseRetainedJSON(raw)
	if err != nil || node.kind != '{' {
		return retainedContractFailure("input-malformed")
	}
	if err := retainedRuntimeIdentity(node); err != nil {
		return err
	}
	limits, err := retainedReadLimits(node.member("limits"), "document-validation")
	if err != nil {
		return err
	}
	if len(discovery.capabilities) != 1 || !retainedSameLimits(limits, discovery.limits) {
		return retainedContractFailure("capability-unsupported")
	}
	return nil
}
