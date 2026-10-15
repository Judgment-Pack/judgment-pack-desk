package desk

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestProjectMoveBrowser(t *testing.T) {
	script := os.Getenv("JPS_MOVE_BROWSER")
	if script == "" {
		t.Skip("set JPS_MOVE_BROWSER after building web/dist")
	}
	for _, mode := range []string{"custom", "default"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "missing-data"))
			browserEnv := os.Environ()
			project := t.TempDir()
			config := t.TempDir()
			destination := filepath.Join(t.TempDir(), filepath.Base(project))
			fakeNativeFolderPicker(t, filepath.Dir(destination))
			privateFixture(t, filepath.Join(project, projectConfigName), "{\"deskConfigVersion\":1}\n")
			cfg := Config{Static: os.DirFS("../../web/dist"), ProjectDir: project, DeskConfigDir: config, Token: testToken, LocalAccess: true, JpackBin: os.Getenv("JPACK_BIN")}
			s, ts := startDesk(t, cfg)
			if mode == "default" {
				destination = s.defaultProjectDestination()
			}
			run := func(origin, phase string) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "node", script, origin, destination, phase, mode)
				cmd.Env = browserEnv
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("browser: %s %v", out, err)
				} else {
					t.Log(string(out))
				}
			}
			run(ts.URL, "move")
			if !s.projectHasMoved() || s.locationStatus().Problem != "" {
				t.Fatal(s.locationStatus())
			}
			ts.Close()
			s.Close()
			pinned, err := OpenProject("", config)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Root = pinned
			cfg.ProjectDir = ""
			again, ats := startDesk(t, cfg)
			defer again.Close()
			defer ats.Close()
			if again.projectDir != destination {
				t.Fatal("wrong restart project", again.projectDir)
			}
			run(ats.URL, "restarted")
		})
	}
}
