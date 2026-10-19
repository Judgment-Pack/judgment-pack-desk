package desk

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Exercises real auth and private storage in a disposable installation. No
// user's sign-in policy, sources, or AI connections are read or changed.
func TestPasswordBrowser(t *testing.T) {
	script := os.Getenv("JPS_PASSWORD_BROWSER")
	if script == "" {
		t.Skip("set JPS_PASSWORD_BROWSER after building web/dist")
	}
	s, ts := startDesk(t, Config{Static: os.DirFS("../../web/dist"), ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken})
	defer ts.Close()
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, ts.URL, testToken)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %s %v", out, err)
	} else {
		t.Log(string(out))
	}
}
