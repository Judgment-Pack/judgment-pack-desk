package codexbridge

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccountBindingPrivateCustodyMutationAndRestart(t *testing.T) {
	m, options := accountManager(t, "success")
	before, err := m.AccountBinding()
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := hex.DecodeString(before); err != nil || len(raw) != 32 {
		t.Fatal("not an opaque digest")
	}
	path := filepath.Join(options.ProfileDir, "profile/auth.json")
	if err := os.WriteFile(path, []byte(`{"fixture":"PRIVATE-CREDENTIAL-A"}`), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := m.AccountBinding()
	if err != nil || a == before {
		t.Fatal("new auth file did not withdraw capture", err)
	}
	if err := os.WriteFile(path, []byte(`{"fixture":"PRIVATE-CREDENTIAL-B"}`), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := m.AccountBinding()
	if err != nil || a == b {
		t.Fatal("credential renewal did not withdraw capture", err)
	}
	if err := os.WriteFile(path+".new", []byte(`{"fixture":"PRIVATE-CREDENTIAL-B"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
	replaced, err := m.AccountBinding()
	if err != nil || replaced == b {
		t.Fatal("credential replacement did not withdraw capture", err)
	}
	m.Close()
	again := reopenAccountManager(t, options, "success")
	restarted, err := again.AccountBinding()
	if err != nil || restarted == replaced {
		t.Fatal("manager restart kept capture", err)
	}
	// Public status adds only this opaque digest, never file identity or credentials.
	status, err := again.Status(context.Background(), "binding-test", false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(status)
	if !bytes.Contains(raw, []byte(`"accountRevision":"`+status.AccountRevision+`"`)) {
		t.Fatal("opaque public member omitted")
	}
	if len(status.AccountRevision) != 64 || strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "auth.json") || strings.Contains(string(raw), "Ino") {
		t.Fatal("public custody leak", string(raw))
	}
}
func TestAccountBindingPendingLoginLogoutAndLateCompletion(t *testing.T) {
	m, _ := accountManager(t, "cancel-late")
	owner, sessionEnd, policyEnd := ownerFixture()
	defer sessionEnd()
	defer policyEnd()
	before, err := m.AccountBinding()
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := m.StartLogin(context.Background(), owner, "browser")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := m.AccountBinding()
	if err != nil || pending == before {
		t.Fatal("pending login kept capture", err)
	}
	if err := m.CancelLogin(context.Background(), owner.ID, challenge.ID); err != nil {
		t.Fatal(err)
	}
	canceled, err := m.AccountBinding()
	if err != nil {
		t.Fatal(err)
	}
	// Logout is a distinct custody transition even if sign-in was never completed.
	if err := m.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := m.AccountBinding()
	if err != nil || after == canceled {
		t.Fatal("logout kept capture", err)
	}
}

func TestAccountBindingSharedPublicMemberVector(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "assistant", "serverRuns", "fixtures", "diagnostic-account.json"))
	if err != nil {
		t.Fatal(err)
	}
	var status Status
	if err := json.Unmarshal(raw, &status); err != nil {
		t.Fatal(err)
	}
	if status.AccountRevision != strings.Repeat("a", 64) {
		t.Fatal("shared account custody vector omitted member")
	}
	encoded, _ := json.Marshal(status)
	if !bytes.Contains(encoded, []byte(`"accountRevision":"`+status.AccountRevision+`"`)) {
		t.Fatal("shared custody field did not roundtrip")
	}
}
