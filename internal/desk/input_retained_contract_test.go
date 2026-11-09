package desk

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const retainedD2SpecJSON = `{"version":"0.3.0","revision":"27cb29bf13e4a2d58d69fe19c7ebfeb762b014fc","schemaSha256":"sha256:0cb49f238c78391cb163a65474cc9af962df2bb1024a55c5310c2b784b2f0557","corpusSha256":"sha256:17b16ff7a9b74fe4cc81eb6f6129a6aabdd9834621d9e756aa2d4c1a3f245863"}`
const retainedD2SelectorJSON = `{"contract":"retained-column/1","spec":` + retainedD2SpecJSON + `,"profile":"base","roles":["document-validation","preview","preparation"]}`

// Expected role inventories/ceilings are handwritten from the published Spec
// table and selected Runner, independent of retainedRoleCaps's implementation.
func retainedD2LimitsJSON(role string) string {
	switch role {
	case "document-validation":
		return `{"documentBytes":1048576,"frameBytes":2097152}`
	case "preview":
		return `{"documentBytes":1048576,"combinedBytes":12582912,"frameBytes":2097152,"profilesBytes":81920,"requestBodyBytes":16777216,"inputReplyBytes":37748736}`
	case "preparation", "preparation-verification":
		return `{"documentBytes":1048576,"combinedBytes":12582912,"frameBytes":2097152,"preparationBytes":16777216,"recordBytes":9437184,"profilesBytes":81920,"requestBodyBytes":16777216,"inputReplyBytes":37748736,"releaseStoredBytes":41943040,"runStoredBytes":41943040,"exportBaseBytes":75497472,"exportWireBytes":90177536}`
	case "producer-carrier":
		return `{"originalSnapshotBytes":524288,"carrierBytes":3145728,"grantBytes":3145728,"adapterStdoutBytes":3145728,"responseBytes":5242880,"profilesBytes":81920}`
	}
	panic("unknown fixture role")
}
func retainedD2CapabilityJSON(role string, grammar bool) string {
	extra := ""
	if grammar {
		extra = `,"producerGrammar":"desk-reviewed-input/1"`
	}
	return `{"contract":"retained-column/1","spec":` + retainedD2SpecJSON + `,"profile":"base","role":"` + role + `","limits":` + retainedD2LimitsJSON(role) + extra + `}`
}
func retainedD2ParseCapabilities(t *testing.T, entries ...string) []retainedConversionCapability {
	t.Helper()
	caps, err := parseRetainedCapabilities([]byte("[" + strings.Join(entries, ",") + "]"))
	if err != nil {
		t.Fatal(err)
	}
	return caps
}
func retainedD2Reports(t *testing.T) retainedOwnerReports {
	return retainedOwnerReports{
		Runtime:  retainedD2ParseCapabilities(t, retainedD2CapabilityJSON("document-validation", false)),
		Runner:   retainedD2ParseCapabilities(t, retainedD2CapabilityJSON("preview", false), retainedD2CapabilityJSON("preparation", true)),
		Producer: retainedD2ParseCapabilities(t, retainedD2CapabilityJSON("producer-carrier", true)),
		Verifier: retainedD2ParseCapabilities(t, retainedD2CapabilityJSON("preparation-verification", true)),
	}
}
func retainedD2Selection(t *testing.T, generated bool) retainedConversionSelector {
	t.Helper()
	raw := retainedD2SelectorJSON
	if generated {
		raw = strings.Replace(raw, `"preparation"]`, `"preparation","producer-carrier"]`, 1)
	}
	selected, err := parseRetainedSelector([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return selected
}
func retainedD2ExpectCode(t *testing.T, err error, want string) {
	t.Helper()
	if retainedContractCode(err) != want {
		t.Fatalf("code %q, want %q; error %v", retainedContractCode(err), want, err)
	}
}

func TestRetainedD2PublishedTupleAndRoleLimits(t *testing.T) {
	selected, err := parseRetainedSelector([]byte(retainedD2SelectorJSON))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(selected, retainedInstalledSelector("document-validation", "preview", "preparation")) {
		t.Fatal("published Agent helper differs from exact handwritten tuple")
	}
	for _, role := range []string{"document-validation", "preview", "preparation", "preparation-verification", "producer-carrier"} {
		capability := retainedD2ParseCapabilities(t, retainedD2CapabilityJSON(role, retainedGrammarRole(role)))[0]
		if !reflect.DeepEqual(capability.Limits, retainedRoleCaps(role)) {
			t.Fatalf("profile cap differs for %s", role)
		}
	}
	cap := retainedRoleCaps("preview")
	cap["documentBytes"] = 1
	if retainedRoleCaps("preview")["documentBytes"] != 1048576 {
		t.Fatal("caller mutated installed cap")
	}
	if got, err := parseRetainedCapabilities([]byte(`[]`)); err != nil || got == nil || len(got) != 0 {
		t.Fatal("known empty capability inventory should grant nothing")
	}
}

func TestRetainedD2ClosedSelectorRefusals(t *testing.T) {
	for name, row := range map[string]struct{ raw, code string }{
		"extra":              {strings.TrimSuffix(retainedD2SelectorJSON, "}") + `,"limits":{}}`, "input-malformed"},
		"unknown-contract":   {strings.Replace(retainedD2SelectorJSON, "retained-column/1", "retained-column/2", 1), "capability-unsupported"},
		"wrong-version":      {strings.Replace(retainedD2SelectorJSON, `"version":"0.3.0"`, `"version":"0.3.0-rc.2"`, 1), "capability-unsupported"},
		"wrong-revision":     {strings.Replace(retainedD2SelectorJSON, "27cb29", "17cb29", 1), "capability-unsupported"},
		"malformed-revision": {strings.Replace(retainedD2SelectorJSON, "27cb29", "27CB29", 1), "input-malformed"},
		"wrong-schema":       {strings.Replace(retainedD2SelectorJSON, "sha256:0cb49", "sha256:1cb49", 1), "capability-unsupported"},
		"wrong-corpus":       {strings.Replace(retainedD2SelectorJSON, "sha256:17b16", "sha256:27b16", 1), "capability-unsupported"},
		"extended":           {strings.Replace(retainedD2SelectorJSON, `"base"`, `"extended"`, 1), "capability-unsupported"},
		"unknown-role":       {strings.Replace(retainedD2SelectorJSON, `"preview"`, `"evaluate"`, 1), "capability-unsupported"},
		"repeated-role":      {strings.Replace(retainedD2SelectorJSON, `"preview"`, `"preview","preview"`, 1), "input-malformed"},
		"role-order":         {strings.Replace(retainedD2SelectorJSON, `"document-validation","preview"`, `"preview","document-validation"`, 1), "input-malformed"},
		"empty-roles":        {strings.Replace(retainedD2SelectorJSON, `["document-validation","preview","preparation"]`, `[]`, 1), "input-malformed"},
		"null-roles":         {strings.Replace(retainedD2SelectorJSON, `["document-validation","preview","preparation"]`, `null`, 1), "input-malformed"},
		"scalar-role":        {strings.Replace(retainedD2SelectorJSON, `"preview"`, `1`, 1), "input-malformed"},
		"wrong-case":         {strings.Replace(retainedD2SelectorJSON, `"contract"`, `"Contract"`, 1), "input-malformed"},
		"duplicate":          {strings.Replace(retainedD2SelectorJSON, `"profile":"base"`, `"profile":"base","\u0070rofile":"base"`, 1), "input-malformed"},
		"spec-extra":         {strings.Replace(retainedD2SelectorJSON, `"version":"0.3.0"`, `"version":"0.3.0","other":1`, 1), "input-malformed"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseRetainedSelector([]byte(row.raw))
			retainedD2ExpectCode(t, err, row.code)
		})
	}
	_, err := parseRetainedSelector([]byte(retainedD2SelectorJSON), "producer-carrier")
	retainedD2ExpectCode(t, err, "capability-unsupported")
}

func TestRetainedD2CapabilityRefusals(t *testing.T) {
	valid := retainedD2CapabilityJSON("preview", false)
	for name, row := range map[string]struct{ raw, code string }{
		"duplicate":          {`[` + valid + `,` + valid + `]`, "input-malformed"},
		"over32":             {`[` + strings.TrimSuffix(strings.Repeat(valid+",", 33), ",") + `]`, "input-malformed"},
		"null":               {`null`, "input-malformed"},
		"missing-limits":     {`[` + strings.Replace(valid, `,"limits":`+retainedD2LimitsJSON("preview"), "", 1) + `]`, "input-malformed"},
		"unknown-limit":      {`[` + strings.Replace(valid, `"documentBytes":1048576`, `"otherBytes":1048576`, 1) + `]`, "input-malformed"},
		"extra-limit":        {`[` + strings.Replace(valid, `"documentBytes":1048576`, `"documentBytes":1048576,"otherBytes":1`, 1) + `]`, "input-malformed"},
		"exponent":           {`[` + strings.Replace(valid, `1048576`, `1e6`, 1) + `]`, "input-malformed"},
		"fraction":           {`[` + strings.Replace(valid, `1048576`, `1048576.0`, 1) + `]`, "input-malformed"},
		"zero":               {`[` + strings.Replace(valid, `1048576`, `0`, 1) + `]`, "input-malformed"},
		"negative":           {`[` + strings.Replace(valid, `1048576`, `-1`, 1) + `]`, "input-malformed"},
		"unsafe":             {`[` + strings.Replace(valid, `1048576`, `9007199254740992`, 1) + `]`, "input-malformed"},
		"string-limit":       {`[` + strings.Replace(valid, `1048576`, `"1048576"`, 1) + `]`, "input-malformed"},
		"case-limit":         {`[` + strings.Replace(valid, `"documentBytes"`, `"DocumentBytes"`, 1) + `]`, "input-malformed"},
		"duplicate-limit":    {`[` + strings.Replace(valid, `"documentBytes":1048576`, `"documentBytes":1048576,"\u0064ocumentBytes":1`, 1) + `]`, "input-malformed"},
		"wrong-role":         {`[` + strings.Replace(valid, `"role":"preview"`, `"role":"execute"`, 1) + `]`, "capability-unsupported"},
		"wrong-tuple":        {`[` + strings.Replace(valid, `"version":"0.3.0"`, `"version":"0.3.0-rc.2"`, 1) + `]`, "capability-unsupported"},
		"grammar-on-preview": {`[` + retainedD2CapabilityJSON("preview", true) + `]`, "input-malformed"},
		"unknown-grammar":    {`[` + strings.Replace(retainedD2CapabilityJSON("preparation", true), "desk-reviewed-input/1", "other/1", 1) + `]`, "capability-unsupported"},
		"null-grammar":       {`[` + strings.Replace(retainedD2CapabilityJSON("preparation", true), `"desk-reviewed-input/1"`, `null`, 1) + `]`, "input-malformed"},
		"empty-grammar":      {`[` + strings.Replace(retainedD2CapabilityJSON("preparation", true), `"desk-reviewed-input/1"`, `""`, 1) + `]`, "input-malformed"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseRetainedCapabilities([]byte(row.raw))
			retainedD2ExpectCode(t, err, row.code)
		})
	}
	// Absence and presence are different capability identities, not duplicates.
	retainedD2ParseCapabilities(t, retainedD2CapabilityJSON("preparation", false), retainedD2CapabilityJSON("preparation", true))
}

func TestRetainedD2IndependentOwnersAndMinimum(t *testing.T) {
	selection := retainedD2Selection(t, true)
	reports := retainedD2Reports(t)
	reports.Runtime[0].Limits["documentBytes"] = 64
	reports.Runner[0].Limits["documentBytes"] = 80
	reports.Runner[1].Limits["documentBytes"] = 96
	reports.Producer[0].Limits["profilesBytes"] = 32
	downward := map[string]map[string]int64{"preparation": {}}
	var caller map[string]int64
	if err := json.Unmarshal([]byte(retainedD2LimitsJSON("preparation")), &caller); err != nil {
		t.Fatal(err)
	}
	downward["preparation"] = caller
	downward["preparation"]["documentBytes"] = 48
	admission, err := negotiateRetainedRoles(selection, selection.Roles, true, reports, downward)
	if err != nil {
		t.Fatal(err)
	}
	limit, err := retainedArtifactLimit(admission, "documentBytes")
	if err != nil || limit != 48 {
		t.Fatalf("independent minimum: %d %v", limit, err)
	}
	limit, err = retainedArtifactLimit(admission, "profilesBytes")
	if err != nil || limit != 32 {
		t.Fatalf("producer profile cap lost: %d %v", limit, err)
	}
	reports.Runtime[0].Limits["documentBytes"] = 1
	downward["preparation"]["documentBytes"] = 1
	if limit, _ := retainedArtifactLimit(admission, "documentBytes"); limit != 48 {
		t.Fatal("admission aliases mutable caller/report data")
	}
	for name, mutate := range map[string]func(*retainedOwnerReports){
		"missing-runtime":                 func(r *retainedOwnerReports) { r.Runtime = nil },
		"missing-runner":                  func(r *retainedOwnerReports) { r.Runner = nil },
		"missing-producer":                func(r *retainedOwnerReports) { r.Producer = nil },
		"runtime-cannot-authorize-runner": func(r *retainedOwnerReports) { r.Runner = r.Runtime },
		"runner-cannot-authorize-runtime": func(r *retainedOwnerReports) { r.Runtime = r.Runner },
		"missing-preparation-grammar":     func(r *retainedOwnerReports) { r.Runner[1].ProducerGrammar = nil },
		"missing-producer-grammar":        func(r *retainedOwnerReports) { r.Producer[0].ProducerGrammar = nil },
	} {
		t.Run(name, func(t *testing.T) {
			r := retainedD2Reports(t)
			mutate(&r)
			_, err := negotiateRetainedRoles(selection, selection.Roles, true, r, nil)
			retainedD2ExpectCode(t, err, "capability-unsupported")
		})
	}
	// The online /status roles grant no offline verification capability.
	verifierSelection := retainedInstalledSelector("preparation-verification")
	r := retainedD2Reports(t)
	r.Verifier = r.Runner
	_, err = negotiateRetainedRoles(verifierSelection, verifierSelection.Roles, true, r, nil)
	retainedD2ExpectCode(t, err, "capability-unsupported")
}

func TestRetainedD2UntrustedCeilingsCannotExpandProfile(t *testing.T) {
	selection := retainedD2Selection(t, false)
	reports := retainedD2Reports(t)
	for _, caps := range [][]retainedConversionCapability{reports.Runtime, reports.Runner} {
		for _, capability := range caps {
			for key := range capability.Limits {
				capability.Limits[key] = 9007199254740991
			}
		}
	}
	admission, err := negotiateRetainedRoles(selection, selection.Roles, false, reports, nil)
	if err != nil {
		t.Fatal(err)
	}
	limit, err := retainedArtifactLimit(admission, "requestBodyBytes")
	if err != nil || limit != 16777216 {
		t.Fatalf("profile expanded: %d %v", limit, err)
	}
	for name, downward := range map[string]map[string]map[string]int64{"missing-key": {"preview": {"documentBytes": 1}}, "zero": {"document-validation": {"documentBytes": 0, "frameBytes": 1}}, "unsafe": {"document-validation": {"documentBytes": 9007199254740992, "frameBytes": 1}}, "unused-role": {"producer-carrier": {}}} {
		t.Run(name, func(t *testing.T) {
			_, err := negotiateRetainedRoles(selection, selection.Roles, false, reports, downward)
			retainedD2ExpectCode(t, err, "input-malformed")
		})
	}
}

func retainedD2RuntimeDiscoveryJSON() string {
	return `{"contract":"retained-column/1","spec":` + retainedD2SpecJSON + `,"profile":"base","role":"document-validation","maturity":"draft","specRevision":"27cb29bf13e4a2d58d69fe19c7ebfeb762b014fc","specification":"https://github.com/Judgment-Pack/judgment-pack-spec/blob/27cb29bf13e4a2d58d69fe19c7ebfeb762b014fc/spec/retained-column.md","status":"available","limits":` + retainedD2LimitsJSON("document-validation") + `,"capabilities":{"conversionContracts":[` + retainedD2CapabilityJSON("document-validation", false) + `],"packCheckScope":"declared-inventory-coverage","operations":["validate","check"],"maxPackBytes":1048576}}`
}
func TestRetainedD2RuntimeReportsAndRunnerStatus(t *testing.T) {
	raw := retainedD2RuntimeDiscoveryJSON()
	discovery, err := readRetainedRuntimeDiscovery([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if discovery.maxPackBytes != 1048576 {
		t.Fatal("independent pack limit changed")
	}
	if err := checkRetainedRuntimeReportIdentity([]byte(raw), discovery); err != nil {
		t.Fatal(err)
	}
	for name, changed := range map[string]string{
		"maturity":          strings.Replace(raw, `"maturity":"draft"`, `"maturity":"stable"`, 1),
		"scope":             strings.Replace(raw, "declared-inventory-coverage", "all-types-checked", 1),
		"missing-operation": strings.Replace(raw, `"validate","check"`, `"validate"`, 1),
		"wrong-source":      strings.Replace(raw, "/spec/retained-column.md", "/rfcs/0029.md", 1),
		"wrong-identity":    strings.Replace(raw, `"version":"0.3.0"`, `"version":"0.3.0-rc.2"`, 1),
		"changed-limits":    strings.Replace(raw, `"documentBytes":1048576`, `"documentBytes":1000`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readRetainedRuntimeDiscovery([]byte(changed))
			if err == nil {
				t.Fatal("bad discovery accepted")
			}
		})
	}
	checked := strings.Replace(raw, `"documentBytes":1048576`, `"documentBytes":1000`, 1)
	retainedD2ExpectCode(t, checkRetainedRuntimeReportIdentity([]byte(checked), discovery), "capability-unsupported")
	for _, entry := range []string{`{"conversionContracts":[` + retainedD2CapabilityJSON("preview", false) + `],"capabilities":["manual"]}`, `{"conversionContracts":[],"healthy":true}`} {
		if _, err := readRetainedRunnerCapabilities([]byte(entry)); err != nil {
			t.Fatal(err)
		}
	}
	_, err = readRetainedRunnerCapabilities([]byte(`{"capabilities":{"conversionContracts":[]}}`))
	retainedD2ExpectCode(t, err, "capability-unsupported")
	_, err = readRetainedRunnerCapabilities([]byte(`{"conversionContracts":[],"opaque":{"x":1,"\u0078":2}}`))
	retainedD2ExpectCode(t, err, "input-malformed")
}
