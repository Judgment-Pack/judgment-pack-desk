package desk

import (
	"crypto/sha256"
	"encoding/hex"
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
