package desk

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDeskHomeBrowser(t *testing.T) {
	script := os.Getenv("JPS_HOME_BROWSER")
	if script == "" {
		t.Skip("set JPS_HOME_BROWSER after building web/dist")
	}
	for _, mode := range []string{"custom", "default"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
			browserEnv := os.Environ()
			cfg := Config{Static: os.DirFS("../../web/dist"), ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, LocalAccess: true, JpackBin: os.Getenv("JPACK_BIN"), HomeMoveShutdown: func() {}}
			privateFixture(t, filepath.Join(cfg.ProjectDir, projectConfigName), `{"deskConfigVersion":1}`)
			destination := filepath.Join(t.TempDir(), "jpack-desk")
			fakeNativeFolderPicker(t, filepath.Dir(destination))
			if mode == "default" {
				destination = deskApplicationDataPath()
			}
			s, ts := startDesk(t, cfg)
			run := func(origin, phase string) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "node", script, origin, destination, phase, mode)
				cmd.Env = browserEnv
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("browser: %s %v", out, err)
				}
				t.Log(string(out))
			}
			run(ts.URL, "move")
			if !s.homeMove.queued.Load() {
				t.Fatal("no reviewed move was queued")
			}
			ts.Close()
			s.Close()
			if err := s.FinishDeskHomeMove(); err != nil {
				t.Fatal(err)
			}
			cfg.DeskConfigDir = filepath.Join(destination, deskHomePlatform)
			home, err := readDeskHome(cfg.DeskConfigDir)
			if err != nil {
				t.Fatal(err)
			}
			cfg.ProjectDir = filepath.Join(destination, home.Registry.Desks[0].Folder)
			_, again := startDesk(t, cfg)
			run(again.URL, "restarted")
		})
	}
}
