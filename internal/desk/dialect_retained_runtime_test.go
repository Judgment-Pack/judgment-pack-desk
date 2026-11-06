package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	agentcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// These are handwritten inputs and report-consistency fixtures, never captured
// successful Runtime reports. Only the explicitly enabled actual-binary test
// below claims to observe Runtime semantics. No fixture grants execution truth.
const retainedRuntimeTestDocument = `{"case":{"evidence":[{"availability":{"column":"License","contractVersion":"1"},"requirement":"license","source":"/retained"}],"facts":[{"conversion":{"column":"Country","contractVersion":"1","kind":"text"},"source":"/retained","target":"/company/country"}]},"version":2}`

const retainedRuntimeTestPack = " {\n" + `"specVersion":"0.2.0-draft","id":"https://example.invalid/pack/retained","version":"1.0.0","title":"Retained Runtime fixture","decision":{"intent":"Check a declared fact","question":"Ready?"},"outcomes":[{"id":"ready","label":"Ready"},{"id":"other","label":"Other"}],"rules":[{"id":"country","description":"Declared read","when":{"op":"fact","path":"/company/country","operator":"equals","value":-0.00},"outcome":"ready","onUnknown":"escalate"}],"evidenceRequirements":[{"id":"license","description":"License","required":false}]` + "\n}\n"

func retainedRuntimeTestSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func retainedRuntimeTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func retainedRuntimeTestReport(operation string) map[string]any {
	selected := agentcontext.DialectRetainedColumnDraft()
	limits := map[string]any{"documentBytes": 1048576, "frameBytes": 2097152}
	report := map[string]any{"outputVersion": "2", "contractVersion": "1", "command": "dialect " + operation, "contract": selected.Contract, "spec": selected.Spec, "profile": selected.Profile, "role": "document-validation", "maturity": "draft", "specRevision": selected.Spec.Revision, "specification": "https://github.com/Judgment-Pack/judgment-pack-spec/blob/" + selected.Spec.Revision + "/spec/retained-column.md", "limits": limits, "diagnostics": []any{}, "auxiliary": map[string]any{"opaque": true}}
	if operation == "contract" {
		report["status"] = "available"
		report["capabilities"] = map[string]any{"conversionContracts": []any{map[string]any{"contract": selected.Contract, "spec": selected.Spec, "profile": selected.Profile, "role": "document-validation", "limits": limits}}, "operations": []string{"contract", "validate", "check"}, "packCheckScope": "declared-inventory-coverage", "maxPackBytes": 10485760}
		return report
	}
	report["status"], report["dialectVersion"] = "document-valid", 2
	report["validationScope"] = "experimental-retained-column-base-document"
	report["document"] = retainedRuntimeTestDocument
	report["digest"] = "sha256:" + retainedRuntimeTestSHA([]byte(retainedRuntimeTestDocument))
	if operation == "check" {
		report["status"] = "complete"
		report["validationScope"] = "experimental-retained-column-base-document-and-declared-pack-inventory"
		report["packValidation"] = map[string]any{"status": "valid"}
		report["compatibility"] = map[string]any{"scope": "declared-inventory-coverage", "status": "complete", "inventoryOverApproximates": true, "valueTypesChecked": false, "executionChecked": false, "pack": map[string]any{"id": "https://example.invalid/pack/retained", "version": "1.0.0", "specVersion": "0.2.0-draft", "sha256": retainedRuntimeTestSHA([]byte(retainedRuntimeTestPack))}, "facts": []any{map[string]any{"target": "/company/country", "status": "mapped", "producers": []string{"/company/country"}, "acknowledgedBy": []string{}}}, "evidence": []any{map[string]any{"target": "license", "status": "mapped", "producers": []string{"license"}, "acknowledgedBy": []string{}}}, "notes": []any{}}
	}
	return report
}

func retainedRuntimeTestNodes(t *testing.T, validation, coverage map[string]any) (*retainedJSONNode, *retainedJSONNode) {
	t.Helper()
	discovery, err := readRetainedRuntimeDiscovery(retainedRuntimeTestJSON(t, retainedRuntimeTestReport("contract")))
	if err != nil {
		t.Fatal(err)
	}
	a, err := readRetainedDialectRuntimeReport("validate", retainedRuntimeTestJSON(t, validation), &discovery)
	if err != nil {
		t.Fatal(err)
	}
	b, err := readRetainedDialectRuntimeReport("check", retainedRuntimeTestJSON(t, coverage), &discovery)
	if err != nil {
		t.Fatal(err)
	}
	return a, b
}

func TestRetainedDialectRuntimeValueIdentity(t *testing.T) {
	// Runtime sorts these parameter keys by UTF-8; Agent sorts by UTF-16. This
	// comparison checks values, not either serializer's byte ordering.
	left := []byte(`{"version":2,"case":{"parameters":{"\ud800\udc00":{"pointer":"/b","type":"string"},"\ue000":{"pointer":"/a","type":"string"}},"facts":[],"evidence":[]}}`)
	right := []byte(`{"case":{"evidence":[],"facts":[],"parameters":{"\ue000":{"type":"string","pointer":"/a"},"\ud800\udc00":{"type":"string","pointer":"/b"}}},"version":2}`)
	if !retainedDialectSameDocument(left, right) {
		t.Fatal("legal supplementary/BMP ordering changed value identity")
	}
	for _, pair := range [][2]string{{`{"n":-0}`, `{"n":0}`}, {`{"s":"\u2028\u2029"}`, "{\"s\":\"\u2028\u2029\"}"}, {`{"n":9007199254740991}`, `{"n":9007199254740991}`}} {
		if !retainedDialectSameDocument([]byte(pair[0]), []byte(pair[1])) {
			t.Fatal("valid scalar/safe integer comparison refused", pair)
		}
	}
	for _, pair := range [][2]string{{`{"n":9007199254740991}`, `{"n":9007199254740990}`}, {`{"n":1e0}`, `{"n":1}`}, {`{"n":1.0}`, `{"n":1}`}, {`{"n":9007199254740992}`, `{"n":9007199254740992}`}, {`{"n":1,"\u006e":1}`, `{"n":1}`}, {`{"s":"\ud800"}`, `{"s":"�"}`}, {`{"a":null}`, `{}`}, {`{"a":false}`, `{"a":0}`}, {`[]`, `[]`}, {`{"a":1} true`, `{"a":1}`}} {
		if retainedDialectSameDocument([]byte(pair[0]), []byte(pair[1])) {
			t.Fatal("invalid or changed document compared equal", pair)
		}
	}
}

func TestRetainedDialectRuntimeCompleteInventory(t *testing.T) {
	raw := []byte(`{"id":"i","version":"v","specVersion":"s","a":{"op":"fact","path":"/a"},"b":{"op":"other","path":"/b"},"literal":{"op":"literal","value":{"op":null,"path":"/data"}},"array":[{"op":false,"path":""},{"op":0,"path":"/a"},{"op":[],"path":"/a~1b"},{"op":{},"path":"/\ue000"},{"op":true,"path":"/\ud800\udc00"}],"evidenceRequirements":[{"id":"z"},{"id":"a"},{"id":"z"}]}`)
	got, err := retainedDialectPackInventory(raw)
	if err != nil || !reflect.DeepEqual(got.Facts, []string{"", "/a", "/a~1b", "/b", "/data", "/\ue000", "/\U00010000"}) || !reflect.DeepEqual(got.Evidence, []string{"a", "z"}) {
		t.Fatalf("full Runtime inventory: %+v %v", got, err)
	}
	for _, raw := range []string{`{"id":"i","id":"j"}`, `{"id":"i","version":"v","specVersion":"s","evidenceRequirements":null}`, `{"id":"i","version":"v","specVersion":"s","a":{"op":1,"path":"\ud800"}}`} {
		if _, err := retainedDialectPackInventory([]byte(raw)); err == nil {
			t.Fatal("malformed inventory accepted", raw)
		}
	}
	got, err = retainedDialectPackInventory([]byte(`{"id":"i","version":"v","specVersion":"s","a":{"op":null,"path":"not-a-pointer"}}`))
	if err != nil || !reflect.DeepEqual(got.Facts, []string{"not-a-pointer"}) {
		t.Fatal("non-pointer silently filtered before Runtime refusal", got, err)
	}
}

func TestRetainedDialectRuntimeReportPair(t *testing.T) {
	validation, coverage := retainedRuntimeTestReport("validate"), retainedRuntimeTestReport("check")
	a, b := retainedRuntimeTestNodes(t, validation, coverage)
	document, digest, facts, evidence, err := validateRetainedDialectReportPair([]byte(retainedRuntimeTestDocument), []byte(retainedRuntimeTestPack), a, b, 1048576)
	if err != nil || document != retainedRuntimeTestDocument || digest != "sha256:"+retainedRuntimeTestSHA([]byte(document)) || !reflect.DeepEqual(facts, []string{"/company/country"}) || !reflect.DeepEqual(evidence, []string{"license"}) {
		t.Fatal("handwritten report consistency refused", err)
	}
	mutations := []struct {
		name string
		edit func(map[string]any, map[string]any)
	}{
		{"changed document", func(v, c map[string]any) {
			c["document"] = `{}`
			c["digest"] = "sha256:" + retainedRuntimeTestSHA([]byte(`{}`))
		}},
		{"pair bytes", func(v, c map[string]any) {
			c["document"] = " " + retainedRuntimeTestDocument
			c["digest"] = "sha256:" + retainedRuntimeTestSHA([]byte(" "+retainedRuntimeTestDocument))
		}},
		{"digest", func(v, c map[string]any) { c["digest"] = "sha256:" + strings.Repeat("0", 64) }},
		{"dialect version", func(v, c map[string]any) { c["dialectVersion"] = "2" }},
		{"scope", func(v, c map[string]any) { c["validationScope"] = "version-2-document-and-declared-pack-inventory" }},
		{"compatibility status", func(v, c map[string]any) { c["compatibility"].(map[string]any)["status"] = "review-required" }},
		{"value types", func(v, c map[string]any) { c["compatibility"].(map[string]any)["valueTypesChecked"] = true }},
		{"execution missing", func(v, c map[string]any) { delete(c["compatibility"].(map[string]any), "executionChecked") }},
		{"overapprox null", func(v, c map[string]any) { c["compatibility"].(map[string]any)["inventoryOverApproximates"] = nil }},
		{"raw pack hash", func(v, c map[string]any) {
			c["compatibility"].(map[string]any)["pack"].(map[string]any)["sha256"] = retainedRuntimeTestSHA([]byte(strings.TrimSpace(retainedRuntimeTestPack)))
		}},
		{"pack identity", func(v, c map[string]any) {
			c["compatibility"].(map[string]any)["pack"].(map[string]any)["version"] = "other"
		}},
		{"pack validation", func(v, c map[string]any) { c["packValidation"] = map[string]any{"status": "invalid"} }},
		{"missing inventory", func(v, c map[string]any) { c["compatibility"].(map[string]any)["facts"] = []any{} }},
		{"extra inventory", func(v, c map[string]any) {
			m := c["compatibility"].(map[string]any)
			m["facts"] = append(m["facts"].([]any), m["facts"].([]any)[0])
		}},
		{"wrong target", func(v, c map[string]any) {
			c["compatibility"].(map[string]any)["evidence"].([]any)[0].(map[string]any)["target"] = "other"
		}},
		{"null inventory", func(v, c map[string]any) { c["compatibility"].(map[string]any)["facts"] = nil }},
		{"wrong row shape", func(v, c map[string]any) {
			c["compatibility"].(map[string]any)["facts"].([]any)[0].(map[string]any)["producers"] = nil
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			v, c := retainedRuntimeTestReport("validate"), retainedRuntimeTestReport("check")
			mutation.edit(v, c)
			a, b := retainedRuntimeTestNodes(t, v, c)
			if _, _, _, _, err := validateRetainedDialectReportPair([]byte(retainedRuntimeTestDocument), []byte(retainedRuntimeTestPack), a, b, 1048576); err == nil {
				t.Fatal("changed evidence accepted")
			}
		})
	}
}

func TestRetainedDialectRuntimeInventoryReportMismatch(t *testing.T) {
	// A consistency-only fixture exercises the entire over-approximated target
	// list. It supplies no evidence that this invented pack was Runtime-valid.
	pack := []byte(`{"id":"https://example.invalid/pack/retained","version":"1.0.0","specVersion":"0.2.0-draft","literal":{"op":"literal","value":{"op":null,"path":"/b"}},"nested":[{"op":"other","path":"/a"},{"op":false,"path":""}]}`)
	for _, targets := range [][]string{{"", "/a", "/b"}, {"", "/b", "/a"}, {"", "/a"}, {"", "/a", "/a"}, {"", "/a", "/b", "/extra"}} {
		v, c := retainedRuntimeTestReport("validate"), retainedRuntimeTestReport("check")
		compatibility := c["compatibility"].(map[string]any)
		compatibility["pack"].(map[string]any)["sha256"] = retainedRuntimeTestSHA(pack)
		rows := []any{}
		for _, target := range targets {
			rows = append(rows, map[string]any{"target": target, "status": "partial", "producers": []string{}, "acknowledgedBy": []string{}})
		}
		compatibility["facts"], compatibility["evidence"] = rows, []any{}
		a, b := retainedRuntimeTestNodes(t, v, c)
		_, _, _, _, err := validateRetainedDialectReportPair([]byte(retainedRuntimeTestDocument), pack, a, b, 1048576)
		want := reflect.DeepEqual(targets, []string{"", "/a", "/b"})
		if (err == nil) != want {
			t.Fatal("complete sorted unique inventory comparison differs", targets, err)
		}
	}
}

func TestRetainedDialectRuntimeAuthorityReader(t *testing.T) {
	for _, edit := range []func(map[string]any){
		func(r map[string]any) { r["role"] = "preparation" },
		func(r map[string]any) { r["specRevision"] = strings.Repeat("0", 40) },
		func(r map[string]any) { r["maturity"] = "final" },
		func(r map[string]any) { r["command"] = "dialect validate" },
		func(r map[string]any) { r["outputVersion"] = 2 },
		func(r map[string]any) { r["diagnostics"] = nil },
		func(r map[string]any) { r["limits"].(map[string]any)["extra"] = 1 },
		func(r map[string]any) { r["limits"].(map[string]any)["documentBytes"] = json.RawMessage(`1e6`) },
		func(r map[string]any) { r["limits"].(map[string]any)["documentBytes"] = 0 },
		func(r map[string]any) {
			r["capabilities"].(map[string]any)["operations"] = []string{"validate", "validate", "check"}
		},
		func(r map[string]any) {
			r["capabilities"].(map[string]any)["conversionContracts"].([]any)[0].(map[string]any)["producerGrammar"] = "desk-reviewed-input/1"
		},
	} {
		r := retainedRuntimeTestReport("contract")
		edit(r)
		if _, err := readRetainedDialectRuntimeReport("contract", retainedRuntimeTestJSON(t, r), nil); err == nil {
			t.Fatal("malformed authority accepted", r)
		}
	}
	raw := retainedRuntimeTestJSON(t, retainedRuntimeTestReport("contract"))
	for _, changed := range [][]byte{append(bytes.Clone(raw), []byte(` true`)...), bytes.Replace(raw, []byte(`"status":"available"`), []byte(`"status":"available","\u0073tatus":"available"`), 1), bytes.Repeat([]byte(" "), dialectAnswerLimit+1)} {
		if _, err := readRetainedDialectRuntimeReport("contract", changed, nil); err == nil {
			t.Fatal("invalid complete envelope accepted")
		}
	}
	discovery, err := readRetainedRuntimeDiscovery(raw)
	if err != nil {
		t.Fatal(err)
	}
	r := retainedRuntimeTestReport("validate")
	r["limits"].(map[string]any)["documentBytes"] = 64
	if _, err := readRetainedDialectRuntimeReport("validate", retainedRuntimeTestJSON(t, r), &discovery); err == nil {
		t.Fatal("changed owner limits accepted")
	}
}

type retainedRuntimeTestCall struct {
	Operation string
	Args      []string
	Input     []byte
	Pack      []byte
	PackMode  uint32
	Directory string
	Config    string
	Signing   string
}

// The copied test executable supplies both the Linux cwd trampoline and the
// negative Runtime peer. Tests never discover an installed shell or Runtime.
func init() {
	if os.Getenv("JPS_RETAINED_RUNTIME_TEST_PEER") != "1" {
		return
	}
	if len(os.Args) >= 4 && os.Args[1] == "-c" {
		directory := os.NewFile(3, "held-test-directory")
		if directory == nil || directory.Chdir() != nil {
			os.Exit(91)
		}
		_ = directory.Close()
		if syscall.Exec(os.Args[3], os.Args[3:], os.Environ()) != nil {
			os.Exit(92)
		}
		os.Exit(0)
	}
	if len(os.Args) < 3 || os.Args[1] != "dialect" {
		os.Exit(93)
	}
	operation := os.Args[2]
	if operation != "contract" && operation != "validate" && operation != "check" {
		os.Exit(94)
	}
	fixture := os.Getenv("JPS_RETAINED_RUNTIME_TEST_DIRECTORY")
	input, _ := io.ReadAll(os.Stdin)
	pack, _ := os.ReadFile("pack.json")
	var mode uint32
	if info, err := os.Stat("pack.json"); err == nil {
		mode = uint32(info.Mode().Perm())
	}
	directory, _ := os.Getwd()
	call := retainedRuntimeTestCall{Operation: operation, Args: os.Args[1:], Input: input, Pack: pack, PackMode: mode, Directory: directory, Config: os.Getenv("JPACK_CONFIG"), Signing: os.Getenv("JPACK_SIGNING_KEY")}
	trace, err := os.OpenFile(filepath.Join(fixture, "calls.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil || json.NewEncoder(trace).Encode(call) != nil {
		os.Exit(95)
	}
	_ = trace.Close()
	if _, err := os.Stat(filepath.Join(fixture, operation+".hang")); err == nil {
		for {
			time.Sleep(time.Hour)
		}
	}
	output, err := os.ReadFile(filepath.Join(fixture, operation+".stdout"))
	if err != nil {
		os.Exit(96)
	}
	_, _ = os.Stdout.Write(output)
	code := 0
	if raw, err := os.ReadFile(filepath.Join(fixture, operation+".exit")); err == nil {
		code, _ = strconv.Atoi(string(raw))
	}
	os.Exit(code)
}

type retainedRuntimeTestHarness struct {
	server    *Server
	directory string
	peer      string
}

func retainedRuntimeHarness(t *testing.T) retainedRuntimeTestHarness {
	t.Helper()
	directory := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	peer := filepath.Join(directory, "runtime-test-peer")
	if err := os.WriteFile(peer, raw, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JPS_RETAINED_RUNTIME_TEST_PEER", "1")
	t.Setenv("JPS_RETAINED_RUNTIME_TEST_DIRECTORY", directory)
	t.Setenv("JPACK_CONFIG", "must-not-enter-private-check")
	t.Setenv("JPACK_SIGNING_KEY", "must-not-enter-private-check")
	// The production Linux boundary looks up sh. Supply that executable too,
	// without changing the boundary or inheriting any installed PATH program.
	if err := os.Link(peer, filepath.Join(directory, "sh")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	h := retainedRuntimeTestHarness{server: &Server{cfg: Config{JpackBin: peer}}, directory: directory, peer: peer}
	for _, operation := range []string{"contract", "validate", "check"} {
		h.output(t, operation, append(retainedRuntimeTestJSON(t, retainedRuntimeTestReport(operation)), '\n'), 0)
	}
	return h
}

func (h retainedRuntimeTestHarness) output(t *testing.T, operation string, stdout []byte, code int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.directory, operation+".stdout"), stdout, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.directory, operation+".exit"), []byte(strconv.Itoa(code)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (h retainedRuntimeTestHarness) calls(t *testing.T) []retainedRuntimeTestCall {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.directory, "calls.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	result := []retainedRuntimeTestCall{}
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte{'\n'}) {
		var call retainedRuntimeTestCall
		if err := json.Unmarshal(line, &call); err != nil {
			t.Fatal(err)
		}
		result = append(result, call)
	}
	return result
}

func TestRetainedDialectRuntimePrivateOwnerCustody(t *testing.T) {
	h := retainedRuntimeHarness(t)
	document, pack := []byte(retainedRuntimeTestDocument), []byte(retainedRuntimeTestPack)
	selection := retainedInstalledSelector("document-validation")
	result, err := h.server.checkRetainedDialectAuthoring(t.Context(), selection, document, pack)
	if err != nil || !result.Accepted {
		t.Fatalf("handwritten transport/consistency fixture: %+v %v", result, err)
	}
	calls := h.calls(t)
	if len(calls) != 3 || calls[0].Operation != "contract" || calls[1].Operation != "validate" || calls[2].Operation != "check" {
		t.Fatal("unexpected dispatch", calls)
	}
	for i, call := range calls {
		if call.Config != "" || call.Signing != "" || i < 2 && call.Pack != nil || i > 0 && !bytes.Equal(call.Input, document) {
			t.Fatal("snapshot/environment custody changed", call)
		}
		if _, err := os.Stat(call.Directory); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("private snapshot retained after check", call.Directory, err)
		}
		want := []string{"dialect", call.Operation, "--draft-contract", selection.Contract + "@" + selection.Spec.Revision, "--draft-profile", selection.Profile}
		if i > 0 {
			want = append(want, "-")
		}
		if i == 2 {
			want = append(want, "--pack", "pack.json")
		}
		want = append(want, "--format", "json")
		if !reflect.DeepEqual(call.Args, want) {
			t.Fatal("selected command changed", call.Args, want)
		}
	}
	if !bytes.Equal(calls[2].Pack, pack) || calls[2].PackMode != 0o600 {
		t.Fatal("original raw pack or private file mode changed", calls[2])
	}
	for _, item := range []retainedDialectRuntimeEvidence{result.Discovery, result.Validation, result.Coverage} {
		want, err := os.ReadFile(filepath.Join(h.directory, item.Operation+".stdout"))
		if err != nil || !bytes.Equal(item.Stdout, want) {
			t.Fatal("complete raw stdout changed", item.Operation, err)
		}
	}
	selection.Roles[0], document[0], pack[0] = "preparation", ' ', '['
	if result.Selection.Roles[0] != "document-validation" || !bytes.Equal(calls[2].Pack, []byte(retainedRuntimeTestPack)) {
		t.Fatal("caller mutation changed detached result/input")
	}
}

func TestRetainedDialectRuntimeBeforeEffectRefusals(t *testing.T) {
	for _, scenario := range []string{"selector", "extra role", "document bound", "pack bound", "discovery", "lower document", "lower pack", "validate refusal", "check incomplete", "nonzero validation", "nonzero check", "overflow", "cancel", "hang"} {
		t.Run(scenario, func(t *testing.T) {
			h := retainedRuntimeHarness(t)
			selection := retainedInstalledSelector("document-validation")
			document, pack := []byte(retainedRuntimeTestDocument), []byte(retainedRuntimeTestPack)
			ctx := t.Context()
			wantCalls, wantError := 0, true
			switch scenario {
			case "selector":
				selection.Profile = "extended"
			case "extra role":
				selection.Roles = append(selection.Roles, "preview")
			case "document bound":
				document = bytes.Repeat([]byte(" "), 1048577)
			case "pack bound":
				pack = bytes.Repeat([]byte(" "), 1048577)
			case "discovery":
				r := retainedRuntimeTestReport("contract")
				r["capabilities"].(map[string]any)["packCheckScope"] = "other"
				h.output(t, "contract", retainedRuntimeTestJSON(t, r), 0)
				wantCalls = 1
			case "lower document", "lower pack":
				r := retainedRuntimeTestReport("contract")
				if scenario == "lower document" {
					r["limits"].(map[string]any)["documentBytes"] = len(document) - 1
				} else {
					r["capabilities"].(map[string]any)["maxPackBytes"] = len(pack) - 1
				}
				h.output(t, "contract", retainedRuntimeTestJSON(t, r), 0)
				wantCalls = 1
			case "validate refusal":
				r := retainedRuntimeTestReport("validate")
				r["status"], r["diagnostics"] = "refused", []any{map[string]any{"code": "input-malformed", "message": "Handwritten refusal fixture"}}
				delete(r, "document")
				delete(r, "digest")
				h.output(t, "validate", append(retainedRuntimeTestJSON(t, r), '\n'), 1)
				wantCalls, wantError = 2, false
			case "check incomplete":
				r := retainedRuntimeTestReport("check")
				r["status"] = "incomplete"
				h.output(t, "check", retainedRuntimeTestJSON(t, r), 1)
				wantCalls, wantError = 3, false
			case "nonzero validation":
				h.output(t, "validate", retainedRuntimeTestJSON(t, retainedRuntimeTestReport("validate")), 1)
				wantCalls = 2
			case "nonzero check":
				h.output(t, "check", retainedRuntimeTestJSON(t, retainedRuntimeTestReport("check")), 1)
				wantCalls = 3
			case "overflow":
				h.output(t, "contract", bytes.Repeat([]byte("x"), dialectAnswerLimit+1), 0)
				wantCalls = 1
			case "cancel":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			case "hang":
				if err := os.WriteFile(filepath.Join(h.directory, "contract.hang"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
				bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				ctx, wantCalls = bounded, 1
			}
			result, err := h.server.checkRetainedDialectAuthoring(ctx, selection, document, pack)
			if result.Accepted || (err != nil) != wantError {
				t.Fatal("refusal accepted or lost", scenario, err, result.Accepted)
			}
			calls := h.calls(t)
			if len(calls) != wantCalls {
				t.Fatal("refusal crossed dispatch boundary", scenario, len(calls), wantCalls)
			}
			for _, call := range calls {
				if call.Operation != "check" && call.Pack != nil {
					t.Fatal("pack written before discovery/validation admission", scenario)
				}
				if _, err := os.Stat(call.Directory); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("refused/canceled private snapshot remains", err)
				}
			}
			if scenario == "overflow" && result.Discovery.Stdout != nil {
				t.Fatal("clipped prefix represented as complete evidence")
			}
			if scenario == "validate refusal" {
				want, _ := os.ReadFile(filepath.Join(h.directory, "validate.stdout"))
				if !bytes.Equal(result.Validation.Stdout, want) || result.Coverage.Stdout != nil {
					t.Fatal("complete refusal diagnostics lost or check dispatched")
				}
			}
		})
	}
}

func TestRetainedDialectRuntimeExactLowerOwnerCaps(t *testing.T) {
	h := retainedRuntimeHarness(t)
	for _, operation := range []string{"contract", "validate", "check"} {
		r := retainedRuntimeTestReport(operation)
		r["limits"].(map[string]any)["documentBytes"] = len(retainedRuntimeTestDocument)
		if operation == "contract" {
			r["capabilities"].(map[string]any)["maxPackBytes"] = len(retainedRuntimeTestPack)
		}
		h.output(t, operation, retainedRuntimeTestJSON(t, r), 0)
	}
	result, err := h.server.checkRetainedDialectAuthoring(t.Context(), retainedInstalledSelector("document-validation"), []byte(retainedRuntimeTestDocument), []byte(retainedRuntimeTestPack))
	if err != nil || !result.Accepted || result.DocumentLimit != int64(len(retainedRuntimeTestDocument)) || result.PackLimit != int64(len(retainedRuntimeTestPack)) {
		t.Fatal("exact independent lower artifact caps refused", result, err)
	}
	// The one-over counterparts are the lower document/pack cases above. These
	// labelled lower-cap fixtures establish no full production maximum claim.
}

// JPACK_RETAINED_DIALECT_RUNTIME must name an explicitly selected fixture binary
// by absolute path. This test has no installed/PATH/build fallback. Four owners,
// each containing at most three calls, bound actual Runtime commands to twelve.
// All expected inputs/status/inventory below are handwritten, not run recordings.
func TestRetainedDialectRuntimeActualSelected(t *testing.T) {
	bin := os.Getenv("JPACK_RETAINED_DIALECT_RUNTIME")
	if bin == "" {
		t.Skip("explicit verified Runtime fixture required: JPACK_RETAINED_DIALECT_RUNTIME")
	}
	if !filepath.IsAbs(bin) {
		t.Fatal("the actual Runtime fixture must be an absolute explicit selection")
	}
	info, err := os.Stat(bin)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Fatal("actual Runtime fixture is unavailable or not executable", err)
	}
	raw, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("explicit Runtime fixture sha256:%s", retainedRuntimeTestSHA(raw))
	h := retainedRuntimeHarness(t)
	h.server.cfg.JpackBin = bin
	unicodeDocument := `{"version":2,"case":{"parameters":{"\ud800\udc00":{"pointer":"/supplementary","type":"string"},"\ue000":{"pointer":"/bmp","type":"string"}},"evidence":[{"requirement":"license","source":"/retained","availability":{"contractVersion":"1","column":"License"}}],"facts":[{"target":"/company/country","source":"/retained","conversion":{"contractVersion":"1","column":"Country","kind":"text"}}]}}`
	namedSource := `{"formatVersion":"2.0.0","id":"urn:example:dialect:retained","version":"1.0.0","case":{"facts":[],"evidence":[]},"sources":[{"name":"reviewed","kind":"operation","profile":"fixture","profileDigest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","maxAge":300,"arguments":{"tool":"read","arguments":{"n":-0}},"read":{"copy":{"facts":[{"target":"/company/country","source":"/retained","conversion":{"contractVersion":"1","column":"Country","kind":"text"}}],"evidence":[{"requirement":"license","source":"/retained","availability":{"contractVersion":"1","column":"License"}}]}}}]}`
	for _, tc := range []struct {
		name, document, status string
		accepted               bool
	}{
		{"unicode case", unicodeDocument, "complete", true},
		{"named source occurrence", namedSource, "complete", true},
		{"acknowledged omissions", `{"version":2,"case":{"facts":[],"evidence":[]},"unmapped":["/company/country"],"unmappedEvidence":["license"]}`, "review-required", true},
		{"actual invalid leaf", `{"version":2,"case":{"facts":[{"target":"/company/country","source":"/retained","conversion":{"contractVersion":"1","column":"Country","kind":"invented"}}],"evidence":[]}}`, "refused", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := h.server.checkRetainedDialectAuthoring(t.Context(), retainedInstalledSelector("document-validation"), []byte(tc.document), []byte(retainedRuntimeTestPack))
			if err != nil || result.Accepted != tc.accepted {
				t.Fatalf("actual selected owner: accepted=%t err=%v validation=%s coverage=%s", result.Accepted, err, result.Validation.Stdout, result.Coverage.Stdout)
			}
			var observed map[string]json.RawMessage
			last := result.Coverage.Stdout
			if !tc.accepted {
				last = result.Validation.Stdout
				if result.Coverage.Stdout != nil {
					t.Fatal("Runtime refused validation but check ran")
				}
			}
			if json.Unmarshal(last, &observed) != nil || string(observed["status"]) != strconv.Quote(tc.status) {
				t.Fatal("actual handwritten expected status differs", string(last))
			}
			if tc.accepted {
				if !reflect.DeepEqual(result.FactTargets, []string{"/company/country"}) || !reflect.DeepEqual(result.EvidenceRequirements, []string{"license"}) || result.Digest != "sha256:"+retainedRuntimeTestSHA([]byte(result.Document)) {
					t.Fatal("actual exact identity/full inventory differs", result)
				}
				var compatibility struct {
					Pack struct {
						SHA256 string `json:"sha256"`
					} `json:"pack"`
				}
				if json.Unmarshal(observed["compatibility"], &compatibility) != nil || compatibility.Pack.SHA256 != retainedRuntimeTestSHA([]byte(retainedRuntimeTestPack)) {
					t.Fatal("Runtime lost original whitespace/numeric pack identity")
				}
			} else if string(observed["diagnostics"]) == "[]" || len(observed["diagnostics"]) == 0 {
				t.Fatal("actual Runtime refusal diagnostics missing")
			}
		})
	}
	if calls := h.calls(t); len(calls) != 0 {
		t.Fatal("actual witness unexpectedly used the synthetic Runtime peer", calls)
	}
}
