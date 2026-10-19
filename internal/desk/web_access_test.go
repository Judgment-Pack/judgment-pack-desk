package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func unsetWebPolicy(t *testing.T) {
	t.Helper()
	old, ok := os.LookupEnv(webPolicyEnv)
	os.Unsetenv(webPolicyEnv)
	t.Cleanup(func() {
		if ok {
			os.Setenv(webPolicyEnv, old)
		} else {
			os.Unsetenv(webPolicyEnv)
		}
	})
}
func webRequest(t *testing.T, rig *processingRig, method string, body any) (int, webAccessStatus) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, rig.ts.URL+"/api/web-access", bytes.NewReader(raw))
	bearer(req)
	resp, err := rig.ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var status webAccessStatus
	if resp.StatusCode == 200 && json.Unmarshal(data, &status) != nil {
		t.Fatalf("invalid response %s", data)
	}
	return resp.StatusCode, status
}
func realWebRig(t *testing.T) *processingRig {
	t.Helper()
	bundle := os.Getenv("JPACK_GATEWAY_TEST_BUNDLE")
	if bundle == "" {
		t.Skip("set JPACK_GATEWAY_TEST_BUNDLE to the verified Gateway bundle")
	}
	rig := newProcessingRig(t)
	rig.s.localGateway.bundle = bundle
	return rig
}
func TestWebAccessSavedOptInDoesNotStartOrRetry(t *testing.T) {
	unsetWebPolicy(t)
	rig := realWebRig(t)
	code, off := webRequest(t, rig, "GET", nil)
	if code != 200 || !off.Local || !off.Supported || off.Enabled || off.Managed || off.Revision == "" {
		t.Fatalf("status %d %+v", code, off)
	}
	if rig.s.localGateway.cmd != nil {
		t.Fatal("status started the gateway")
	}
	code, _ = webRequest(t, rig, "POST", map[string]any{"enabled": true, "revision": off.Revision})
	if code != 400 {
		t.Fatalf("missing warning: %d", code)
	}
	code, on := webRequest(t, rig, "POST", map[string]any{"enabled": true, "revision": off.Revision, "restartWarned": true})
	if code != 200 || !on.Enabled || on.Revision == off.Revision {
		t.Fatalf("apply %d %+v", code, on)
	}
	if rig.s.localGateway.cmd != nil {
		t.Fatal("Apply started the gateway")
	}
	value, _, err := webPolicyValue(rig.s.assistant)
	if err != nil || value == nil || *value != groundingPolicy {
		t.Fatalf("not retained: %v %v", value, err)
	}
	// A new worker receives the setting; the real companion emits the flag.
	t.Setenv(webPolicyEnv, *value)
	plan, err := localGatewaySourceArgs(context.Background(), rig.s.localGateway.bundle, filepath.Join(rig.s.assistant.dir, "gateway-connections"))
	if err != nil || !strings.Contains(strings.Join(plan.args, " "), "--redirect-policies=google-grounding") {
		t.Fatalf("plan %v %v", plan, err)
	}
	os.Unsetenv(webPolicyEnv)
	code, _ = webRequest(t, rig, "POST", map[string]any{"enabled": false, "revision": off.Revision, "restartWarned": true})
	if code != 409 {
		t.Fatalf("stale write %d", code)
	}
	// A running gateway is stopped once; no replacement is started by Apply.
	if _, err = rig.s.localGateway.ensure(rig.s.assistant); err != nil {
		t.Fatal(err)
	}
	code, off = webRequest(t, rig, "POST", map[string]any{"enabled": false, "revision": on.Revision, "restartWarned": true})
	if code != 200 || off.Enabled || rig.s.localGateway.cmd != nil {
		t.Fatalf("disable %d %+v", code, off)
	}
	starts, _ := os.ReadFile(filepath.Join(rig.dir, "starts"))
	if string(starts) != "start\n" {
		t.Fatalf("unexpected starts %q", starts)
	}
}
func TestWebAccessOperatorOverrideAndConcurrentSaves(t *testing.T) {
	unsetWebPolicy(t)
	rig := realWebRig(t)
	_, initial := webRequest(t, rig, "GET", nil)
	var codes [2]int
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = webRequest(t, rig, "POST", map[string]any{"enabled": true, "revision": initial.Revision, "restartWarned": true})
		}()
	}
	wg.Wait()
	if !(codes[0] == 200 && codes[1] == 409 || codes[1] == 200 && codes[0] == 409) {
		t.Fatalf("competing saves %v", codes)
	}
	t.Setenv(webPolicyEnv, "")
	_, managed := webRequest(t, rig, "GET", nil)
	if !managed.Managed || managed.Enabled {
		t.Fatalf("override %+v", managed)
	}
	code, _ := webRequest(t, rig, "POST", map[string]any{"enabled": true, "revision": managed.Revision, "restartWarned": true})
	if code != 409 {
		t.Fatalf("managed write %d", code)
	}
}
func TestWebAccessDoesNotFollowASettingsSymlink(t *testing.T) {
	unsetWebPolicy(t)
	rig := realWebRig(t)
	target := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(target, []byte(`{"version":1,"googleGrounding":true}`), 0600)
	os.Symlink(target, filepath.Join(rig.s.assistant.dir, webAccessFile))
	code, _ := webRequest(t, rig, "GET", nil)
	if code != 503 {
		t.Fatalf("linked settings %d", code)
	}
	if rig.s.localGateway.cmd != nil {
		t.Fatal("unreadable settings started gateway")
	}
}
