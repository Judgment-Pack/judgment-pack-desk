package desk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const genericPlanFixture = `{"version":1,"sources":[{"id":"documents","executable":"adapter-document","args":[],"shape":"command","timeout":40,"connections":false},{"id":"future-files","executable":"adapter-sources","args":["--provider","future-files"],"shape":"http","timeout":60,"connections":true}]}`

func TestLocalPlanLaunchesNewSourceOnlyFromInstalledManifest(t *testing.T) {
	files := map[string]string{executableName("adapter-document"): strings.Repeat("a", 64), executableName("adapter-sources"): strings.Repeat("b", 64)}
	args, err := decodeLocalSourcePlan([]byte(genericPlanFixture), files)
	if err != nil || !strings.Contains(strings.Join(args, " "), "future-files="+executableName("adapter-sources")+" --provider future-files") {
		t.Fatal(args, err)
	}
	for _, changed := range []string{
		strings.Replace(genericPlanFixture, `"adapter-sources"`, `"../adapter-sources"`, 1),
		strings.Replace(genericPlanFixture, `"adapter-sources"`, `"adapter-unverified"`, 1),
		strings.Replace(genericPlanFixture, `"future-files"]`, `"future files"]`, 1),
		strings.Replace(genericPlanFixture, `"future-files"]`, `"future\u00a0files"]`, 1),
		strings.Replace(genericPlanFixture, `"future-files"]`, `"future\u000bfiles"]`, 1),
		strings.Replace(genericPlanFixture, `"http"`, `"execute"`, 1),
		strings.Replace(genericPlanFixture, `"timeout":60`, `"timeout":600`, 1),
		strings.Replace(genericPlanFixture, `"version":1`, `"version":2`, 1),
		strings.Replace(genericPlanFixture, `"id":"future-files"`, `"id":"documents"`, 1),
	} {
		if result, err := decodeLocalSourcePlan([]byte(changed), files); err == nil || result != nil {
			t.Fatal("unsafe plan accepted", changed)
		}
	}
}

func TestIndependentGatewayRequiresExactOperatorManifestApproval(t *testing.T) {
	raw := []byte(`{"revision":"another-reviewed-revision","files":{}}`)
	digest := sha256.Sum256(raw)
	t.Setenv("JPACK_DESK_GATEWAY_MANIFEST_SHA256", hex.EncodeToString(digest[:]))
	if !manifestRevisionApproved(raw) {
		t.Fatal("approved replacement rejected")
	}
	if manifestRevisionApproved(append(raw, ' ')) {
		t.Fatal("altered manifest accepted")
	}
	t.Setenv("JPACK_DESK_GATEWAY_MANIFEST_SHA256", "")
	if manifestRevisionApproved(raw) {
		t.Fatal("empty approval accepted")
	}
}

func TestLocalPlanSearchAllowsLongerEnvelopeWithoutExtendingOtherSources(t *testing.T) {
	files := map[string]string{executableName("adapter-document"): strings.Repeat("a", 64), executableName("adapter-sources"): strings.Repeat("b", 64)}
	search := strings.ReplaceAll(genericPlanFixture, "future-files", "web-search")
	// Gateway v0.10.0 gives web-search its 130 s with --long-search (#219).
	search = strings.Replace(search, `"web-search"]`, `"web-search","--long-search"]`, 1)
	search = strings.Replace(search, `"timeout":60`, `"timeout":130`, 1)
	if _, err := decodeLocalSourcePlan([]byte(search), files); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		strings.Replace(search, `"timeout":130`, `"timeout":131`, 1),
		strings.Replace(genericPlanFixture, `"timeout":60`, `"timeout":130`, 1),
		// The longer envelope without the flag that says why.
		strings.Replace(search, `,"--long-search"]`, `]`, 1),
	} {
		if _, err := decodeLocalSourcePlan([]byte(raw), files); err == nil {
			t.Fatal("unbounded or unrelated source accepted", raw)
		}
	}
}

// gatewayPlan is one of the plans gateway v0.10.0's companion printed as
// `gateway-connections --local-plan` from its published linux/amd64 archive:
// with nothing configured; with a program processor chosen (mode auto); with
// a search connection's timeout of 90 s; and with both.
func gatewayPlan(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "gateway-plans", "v0.10.0-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

// gatewayPlanFiles is a manifest that names every program those plans launch.
func gatewayPlanFiles() map[string]string {
	files := map[string]string{}
	for _, name := range []string{"adapter-document", "adapter-render", "adapter-drive", "adapter-gmail", "adapter-sources", "adapter-web"} {
		files[executableName(name)] = strings.Repeat("a", 64)
	}
	return files
}

// Gateway v0.10.0's plans are taken as it prints them, and Desk says which of
// them gives the document sources the processing envelope.
func TestLocalPlanTakesTheReleasedGatewaysPlans(t *testing.T) {
	for _, tt := range []struct {
		name       string
		processing bool
		envelopes  map[string]string
	}{
		{"ordinary", false, map[string]string{"documents": "40", "web-search": "60", "drive": "60"}},
		{"processing", true, map[string]string{"documents": "150", "drive": "150", "web": "150", "aws-s3": "150", "gmail": "60", "web-search": "60"}},
		{"long-search", false, map[string]string{"documents": "40", "web-search": "130"}},
		{"processing-long-search", true, map[string]string{"documents": "150", "web-search": "130", "notion": "60"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := decodeLocalPlan([]byte(gatewayPlan(t, tt.name)), gatewayPlanFiles())
			if err != nil {
				t.Fatal(err)
			}
			if plan.documentProcessing != tt.processing {
				t.Fatalf("the plan's processing was read as %v", plan.documentProcessing)
			}
			args := strings.Join(plan.args, " ")
			for id, seconds := range tt.envelopes {
				if !strings.Contains(args, "--source-timeout "+id+"="+seconds+" ") {
					t.Fatalf("%s is not given %s s: %s", id, seconds, args)
				}
			}
			if tt.processing && !strings.Contains(args, "--source-env documents=JPACK_CONNECTIONS_DIR") {
				t.Fatal("the document source is not given the connections directory", args)
			}
		})
	}
}

// Desk takes the processing envelope and the two flags only as the gateway's
// plan gives them; anything beyond refuses the plan whole.
func TestLocalPlanRefusesProcessingBeyondWhatTheGatewayGives(t *testing.T) {
	processing, ordinary := gatewayPlan(t, "processing"), gatewayPlan(t, "ordinary")
	for name, raw := range map[string]string{
		"a document source past 150 s": strings.Replace(processing, `"--document-processing"],"shape":"http","timeout":150`, `"--document-processing"],"shape":"http","timeout":151`, 1),
		"150 s without the flag":       strings.Replace(ordinary, `"args":[],"shape":"http","timeout":60`, `"args":[],"shape":"http","timeout":150`, 1),
		"the flag on another source":   strings.Replace(processing, `["--principal","desk-local"],"shape":"http","timeout":60`, `["--principal","desk-local","--document-processing"],"shape":"http","timeout":60`, 1),
		"the flag twice":               strings.Replace(processing, `["--document-processing"]`, `["--document-processing","--document-processing"]`, 1),
		"the flag without the directory": strings.Replace(processing, `"args":["--document-processing"],"shape":"http","timeout":150,"connections":true`,
			`"args":["--document-processing"],"shape":"http","timeout":150,"connections":false`, 1),
		"the flag on some document sources only": strings.Replace(processing,
			`"args":["--document-processing"],"shape":"http","timeout":150,"connections":true`, `"args":[],"shape":"http","timeout":60,"connections":false`, 1),
		"the search flag on a document source": strings.Replace(ordinary, `"args":[],"shape":"http","timeout":60`, `"args":["--long-search"],"shape":"http","timeout":60`, 1),
		"the search flag twice":                strings.Replace(gatewayPlan(t, "long-search"), `"--long-search"]`, `"--long-search","--long-search"]`, 1),
		// Each refused for its flag alone, the envelope and the count of
		// flagged document sources being what the gateway gives.
		"the flag moved from a document source to another source": strings.Replace(strings.Replace(processing,
			`{"id":"gmail","executable":"adapter-gmail","args":["--principal","desk-local"],"shape":"http","timeout":60,"connections":true}`,
			`{"id":"gmail","executable":"adapter-gmail","args":["--principal","desk-local","--document-processing"],"shape":"http","timeout":60,"connections":true}`, 1),
			`{"id":"web","executable":"adapter-web","args":["--document-processing"],"shape":"http","timeout":150,"connections":true}`,
			`{"id":"web","executable":"adapter-web","args":[],"shape":"http","timeout":60,"connections":false}`, 1),
		"the search flag twice within 60 s": strings.Replace(ordinary, `"--provider","web-search","--principal","desk-local"],"shape":"http","timeout":60`,
			`"--provider","web-search","--principal","desk-local","--long-search","--long-search"],"shape":"http","timeout":60`, 1),
	} {
		if raw == processing || raw == ordinary {
			t.Fatalf("%s: the fixture did not change", name)
		}
		if _, err := decodeLocalPlan([]byte(raw), gatewayPlanFiles()); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

// The companion is asked for the plan with this desk's connections
// directory, whatever Desk's own environment carries, so the plan follows
// the settings this desk's companions keep.
func TestLocalPlanIsAskedWithThisDesksConnectionsDirectory(t *testing.T) {
	bundle, seen := t.TempDir(), filepath.Join(t.TempDir(), "seen")
	connections := filepath.Join(t.TempDir(), "gateway-connections")
	processing, ordinary := filepath.Join(bundle, "processing.json"), filepath.Join(bundle, "ordinary.json")
	os.WriteFile(processing, []byte(gatewayPlan(t, "processing")), 0o600)
	os.WriteFile(ordinary, []byte(gatewayPlan(t, "ordinary")), 0o600)
	// A stand-in companion: the processing plan for the directory Desk names,
	// the ordinary one for any other, and what it was given written down.
	companion := "#!/bin/sh\nprintf '%s' \"$JPACK_CONNECTIONS_DIR\" > '" + seen + "'\n" +
		"if [ \"$JPACK_CONNECTIONS_DIR\" = '" + connections + "' ]; then exec /bin/cat '" + processing + "'; fi\nexec /bin/cat '" + ordinary + "'\n"
	files := gatewayPlanFiles()
	for name, content := range map[string]string{"gateway": "gateway", "gateway-connections": companion} {
		os.WriteFile(filepath.Join(bundle, executableName(name)), []byte(content), 0o700)
		sum := sha256.Sum256([]byte(content))
		files[executableName(name)] = hex.EncodeToString(sum[:])
	}
	for name := range gatewayPlanFiles() {
		os.WriteFile(filepath.Join(bundle, name), []byte("a"), 0o700)
		sum := sha256.Sum256([]byte("a"))
		files[name] = hex.EncodeToString(sum[:])
	}
	manifest, _ := json.Marshal(map[string]any{"revision": GatewayRevision, "files": files})
	os.WriteFile(filepath.Join(bundle, "gateway-bundle.json"), manifest, 0o600)
	t.Setenv("JPACK_CONNECTIONS_DIR", filepath.Join(t.TempDir(), "another"))
	plan, err := localGatewaySourceArgs(context.Background(), bundle, connections)
	if err != nil {
		t.Fatal(err)
	}
	if given, _ := os.ReadFile(seen); string(given) != connections {
		t.Fatalf("the companion was asked with %q", given)
	}
	if !plan.documentProcessing || !strings.Contains(strings.Join(plan.args, " "), "--source-timeout drive=150") {
		t.Fatal("the plan for this desk's settings was not taken", plan)
	}
}

func TestLocalPlanBooleanFlagSpellings(t *testing.T) {
	for _, flag := range []string{"document-processing", "long-search"} {
		fixture := "processing"
		if flag == "long-search" {
			fixture = "long-search"
		}
		for _, tt := range []struct {
			args             []string
			enabled, counted bool
		}{
			{[]string{"-" + flag}, true, true},
			{[]string{"--" + flag}, true, true},
			{[]string{"--" + flag + "=true"}, true, true},
			{[]string{"-" + flag + "=1"}, true, true},
			{[]string{"--" + flag + "=t"}, true, true},
			{[]string{"--" + flag + "=T"}, true, true},
			{[]string{"--" + flag + "=TRUE"}, true, true},
			{[]string{"--" + flag + "=True"}, true, true},
			{[]string{"--" + flag + "=false"}, false, true},
			{[]string{"--" + flag + "=0"}, false, true},
			{[]string{"--" + flag + "=tRuE"}, false, true},
			{[]string{"--" + flag + "="}, false, true},
			{[]string{"--" + flag, "true"}, true, true}, // true is positional.
			{[]string{"--", "--" + flag}, true, true},   // The rule still counts flags after --.
			{[]string{"--" + flag + "-extra"}, false, false},
			{[]string{"--extra-" + flag}, false, false},
		} {
			spelling, _ := json.Marshal(tt.args)
			members := string(spelling[1 : len(spelling)-1])
			raw := strings.ReplaceAll(gatewayPlan(t, fixture), `"--`+flag+`"`, members)
			_, err := decodeLocalPlan([]byte(raw), gatewayPlanFiles())
			if (err == nil) != tt.enabled {
				t.Fatalf("%s: %v", spelling, err)
			}
			// Ordinary envelope: a counted flag on gmail must refuse the plan.
			raw = strings.Replace(gatewayPlan(t, "ordinary"), `["--principal","desk-local"]`, `["--principal","desk-local",`+members+`]`, 1)
			if _, err := decodeLocalPlan([]byte(raw), gatewayPlanFiles()); (err != nil) != tt.counted {
				t.Fatalf("forbidden source %s: %v", spelling, err)
			}
		}
	}
}
