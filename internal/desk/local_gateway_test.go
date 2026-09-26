package desk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLocalIdentityStableAndConcurrent(t *testing.T) {
	store := openAssistantStore(t.TempDir())
	defer store.Close()
	if !store.usable() {
		t.Fatal(store.problem)
	}
	var group sync.WaitGroup
	values := make(chan string, 8)
	failures := make(chan error, 8)
	for range 8 {
		group.Add(1)
		go func() { defer group.Done(); pin, err := localIdentity(store); values <- pin; failures <- err }()
	}
	group.Wait()
	close(values)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var want string
	for value := range values {
		if want == "" {
			want = value
		}
		if value != want {
			t.Fatal("identity rotated concurrently")
		}
	}
	reopened := openAssistantStore(store.dir)
	defer reopened.Close()
	got, err := localIdentity(reopened)
	if err != nil || got != want {
		t.Fatalf("restart changed identity: %v", err)
	}
	for _, name := range []string{"secrets/local-gateway.seed", "local-gateway-identity.json"} {
		info, err := store.root.Stat(name)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("not private: %s", name)
		}
	}
}
func TestLocalIdentityRefusesLossAndRotation(t *testing.T) {
	for _, name := range []string{"missing", "changed", "corrupt", "symlink"} {
		t.Run(name, func(t *testing.T) {
			store := openAssistantStore(t.TempDir())
			defer store.Close()
			if _, err := localIdentity(store); err != nil {
				t.Fatal(err)
			}
			seed := filepath.Join(store.dir, "secrets", "local-gateway.seed")
			switch name {
			case "missing":
				os.Remove(seed)
			case "changed":
				os.WriteFile(seed, []byte(strings.Repeat("ab", 32)+"\n"), 0600)
			case "corrupt":
				os.WriteFile(seed, []byte("broken"), 0600)
			case "symlink":
				os.Remove(seed)
				os.Symlink(filepath.Join(t.TempDir(), "absent"), seed)
			}
			if _, err := localIdentity(store); err == nil {
				t.Fatal("damaged established identity was replaced")
			}
			if name == "missing" {
				if _, err := os.Stat(seed); !os.IsNotExist(err) {
					t.Fatal("missing key silently regenerated")
				}
			}
		})
	}
}
func TestLocalBundleRefusesWrongRevisionAndTampering(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{}
	for _, name := range []string{"gateway", "adapter-document", "gateway-connections", "adapter-drive", "adapter-gmail", "adapter-sources", "adapter-web"} {
		name = executableName(name)
		raw := []byte(name)
		os.WriteFile(filepath.Join(dir, name), raw, 0700)
		hash := sha256.Sum256(raw)
		files[name] = hex.EncodeToString(hash[:])
	}
	write := func(revision string) {
		raw, _ := json.Marshal(map[string]any{"revision": revision, "files": files})
		os.WriteFile(filepath.Join(dir, "gateway-bundle.json"), raw, 0600)
	}
	write(GatewayRevision)
	if err := verifyGatewayBundle(dir); err != nil {
		t.Fatal(err)
	}
	write("wrong")
	if verifyGatewayBundle(dir) == nil {
		t.Fatal("wrong version accepted")
	}
	write(GatewayRevision)
	os.WriteFile(filepath.Join(dir, executableName("adapter-document")), []byte("other"), 0700)
	if verifyGatewayBundle(dir) == nil {
		t.Fatal("modified component accepted")
	}
}
func TestLocalReadinessUsesPinnedIdentityAndRefusesRedirect(t *testing.T) {
	for _, kind := range []string{"match", "wrong-key", "redirect"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "redirect" {
					http.Redirect(w, r, "http://127.0.0.1:1/", 302)
					return
				}
				key := testSignerPublic
				if kind == "wrong-key" {
					key = strings.Repeat("ab", 32)
				}
				json.NewEncoder(w).Encode(map[string]string{"algorithm": "ed25519", "publicKey": key, "authority": localAuthority})
			}))
			defer server.Close()
			err := checkLocalGateway(context.Background(), server.URL, testSignerPublic)
			if (err == nil) != (kind == "match") {
				t.Fatalf("identity result for %s: %v", kind, err)
			}
		})
	}
}
func TestLocalGatewayNeverFallsBackForRefusedOrExternalConfig(t *testing.T) {
	s, _, _ := assistantServer(t)
	s.localGateway = &localGateway{bundle: t.TempDir()}
	for _, config := range []string{"", "{broken", `{"deskConfigVersion":1,"unknown":true}`} {
		if got := s.localGatewayStatus([]byte(config)); got != nil {
			t.Fatalf("refused file got local status: %+v", got)
		}
	}
	if got := s.localGatewayStatus([]byte(researchDeskFile("http://127.0.0.1:1"))); got == nil || got.Status != "external" {
		t.Fatalf("external configuration replaced: %+v", got)
	}
	if _, err := s.assistant.secrets.Stat("local-gateway.seed"); !os.IsNotExist(err) {
		t.Fatal("fallback provisioned a signing key")
	}
	if got := s.localGatewayStatus(nil); got == nil || got.Status != "unavailable" {
		t.Fatalf("missing bundle advertised ready: %+v", got)
	}
}

func TestLocalGatewayPreparesOnlyMissingConfigBase(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "config")
	if err := PrepareDeskConfigBase(filepath.Join(base, "jpack-desk")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(base)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("base not private")
	}
	if err := os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	if err := PrepareDeskConfigBase(filepath.Join(base, "jpack-desk")); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(base)
	if info.Mode().Perm() != 0755 {
		t.Fatal("changed an existing ancestor")
	}
	if err := PrepareDeskConfigBase(filepath.Join(parent, "missing-home", "config", "jpack-desk")); err == nil {
		t.Fatal("created a missing home")
	}
}

func TestLocalGatewayStatusKeepsLaunchedBuildIdentity(t *testing.T) {
	s, _, _ := assistantServer(t)
	done := make(chan struct{})
	defer close(done)
	build := &gatewayBuild{Version: "v0.3.1", Revision: GatewayRevision}
	s.localGateway = &localGateway{bundle: t.TempDir(), done: done, pin: &localGatewayPin{build: build}}
	// A different manifest on disk must not relabel a still-running companion.
	os.WriteFile(filepath.Join(s.localGateway.bundle, "gateway-bundle.json"), []byte(`{"version":"future","revision":"replacement"}`), 0600)
	got := s.localGatewayStatus(nil)
	if got.Status != "ready" || got.Build == nil || *got.Build != *build {
		t.Fatalf("running build identity changed: %+v", got)
	}
	got = s.localGatewayStatus([]byte(researchDeskFile("http://127.0.0.1:1")))
	if got.Status != "external" || got.Build != nil {
		t.Fatalf("local build attributed to external gateway: %+v", got)
	}
}
