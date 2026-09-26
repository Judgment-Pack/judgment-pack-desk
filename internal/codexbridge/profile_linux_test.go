package codexbridge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateProfileLeaseIsolationAndConfiguration(t *testing.T) {
	m, options := accountManager(t, "pending")
	if other, err := NewManager(options); !errors.Is(err, ErrBusy) {
		if other != nil {
			other.Close()
		}
		t.Fatalf("competing owner: %v", err)
	}
	cmd := m.profile.command(options.Binary)
	if cmd.Dir == options.ProjectDir || strings.Contains(strings.Join(cmd.Env, "\n"), "OPENAI_API_KEY") {
		t.Fatal("ambient environment imported")
	}
	t.Setenv("OPENAI_API_KEY", "PRIVATE_SENTINEL")
	t.Setenv("CODEX_HOME", "PRIVATE_SENTINEL")
	t.Setenv("HTTPS_PROXY", "PRIVATE_SENTINEL")
	if strings.Contains(strings.Join(m.profile.command(options.Binary).Env, "\n"), "PRIVATE_SENTINEL") {
		t.Fatal("inherited provider override")
	}
	for _, path := range []string{"managed", "lease", "profile/config.toml"} {
		info, err := os.Stat(filepath.Join(options.ProfileDir, path))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe file %s: %v %v", path, info, err)
		}
	}
	m.Close()
	m = reopenAccountManager(t, options, "pending")
	m.Close()
	// A replaced managed configuration is refused, never silently repaired.
	if err := os.WriteFile(filepath.Join(options.ProfileDir, "profile/config.toml"), []byte("model_provider = \"other\""), 0600); err != nil {
		t.Fatal(err)
	}
	if other, err := NewManager(options); !errors.Is(err, ErrProfile) {
		if other != nil {
			other.Close()
		}
		t.Fatalf("adopted modified config: %v", err)
	}
}

func TestProfileRejectsProjectSymlinksPermissionsAndForeignState(t *testing.T) {
	for _, kind := range []string{"project", "root-symlink", "parent-symlink", "public-root", "foreign-root", "auth-symlink", "auth-hardlink"} {
		t.Run(kind, func(t *testing.T) {
			options := Options{Binary: os.Args[0], ProfileDir: filepath.Join(t.TempDir(), "codex"), ProjectDir: t.TempDir()}
			switch kind {
			case "project":
				options.ProfileDir = filepath.Join(options.ProjectDir, "codex")
			case "root-symlink":
				_ = os.Symlink(t.TempDir(), options.ProfileDir)
			case "parent-symlink":
				target := t.TempDir()
				link := filepath.Join(t.TempDir(), "alias")
				_ = os.Symlink(target, link)
				options.ProfileDir = filepath.Join(link, "codex")
			case "public-root":
				_ = os.Mkdir(options.ProfileDir, 0755)
			case "foreign-root":
				_ = os.Mkdir(options.ProfileDir, 0700)
			default:
				m, err := NewManager(options)
				if err != nil {
					t.Fatal(err)
				}
				m.Close()
				target := filepath.Join(t.TempDir(), "private")
				_ = os.WriteFile(target, []byte("PRIVATE_SENTINEL"), 0600)
				path := filepath.Join(options.ProfileDir, "profile/auth.json")
				if kind == "auth-symlink" {
					_ = os.Symlink(target, path)
				} else {
					_ = os.Link(target, path)
				}
			}
			if m, err := NewManager(options); !errors.Is(err, ErrProfile) {
				if m != nil {
					m.Close()
				}
				t.Fatalf("unsafe profile accepted: %v", err)
			}
		})
	}
}
