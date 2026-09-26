package codexbridge

import (
	"bytes"
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
	for _, path := range []string{"managed", "lease", "profile/config.toml", "profile/model-catalog.json"} {
		info, err := os.Stat(filepath.Join(options.ProfileDir, path))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe file %s: %v %v", path, info, err)
		}
	}
	catalogPath := filepath.Join(options.ProfileDir, "profile/model-catalog.json")
	config, err := os.ReadFile(filepath.Join(options.ProfileDir, "profile/config.toml"))
	if err != nil || !strings.Contains(string(config), "\nmodel_catalog_json = \""+catalogPath+"\"\n") {
		t.Fatalf("configuration does not name the closed catalog: %v\n%s", err, config)
	}
	if written, err := os.ReadFile(catalogPath); err != nil || !bytes.Equal(written, modelCatalog) {
		t.Fatalf("catalog written to the profile differs from the embedded one: %v", err)
	}
	m.Close()
	m = reopenAccountManager(t, options, "pending")
	m.Close()
	// A replaced managed file is refused, never silently repaired: the catalog
	// with one member changed, then the configuration.
	changed := bytes.Replace(modelCatalog, []byte(`"tool_mode": null`), []byte(`"tool_mode": "code_mode_only"`), 1)
	if bytes.Equal(changed, modelCatalog) {
		t.Fatal("catalog has no tool_mode member to change")
	}
	for _, tamper := range []struct {
		path, name string
		data       []byte
	}{
		{catalogPath, "catalog", changed},
		{catalogPath, "catalog", modelCatalog[:len(modelCatalog)-2]},
		{filepath.Join(options.ProfileDir, "profile/config.toml"), "config", []byte("model_provider = \"other\"")},
	} {
		original, err := os.ReadFile(tamper.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tamper.path, tamper.data, 0600); err != nil {
			t.Fatal(err)
		}
		if other, err := NewManager(options); !errors.Is(err, ErrProfile) {
			if other != nil {
				other.Close()
			}
			t.Fatalf("adopted modified %s: %v", tamper.name, err)
		}
		if err := os.WriteFile(tamper.path, original, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// The configuration an earlier Desk wrote, which names no catalog, is
	// rewritten on the next open; the same text with any other change is not.
	configPath := filepath.Join(options.ProfileDir, "profile/config.toml")
	previous := bytes.Replace(modelCatalogConfig(t, configPath), []byte("\nmodel_catalog_json = \""+catalogPath+"\"\n"), []byte("\n"), 1)
	if err := os.WriteFile(configPath, append(previous, []byte("web_search = \"live\"\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if other, err := NewManager(options); !errors.Is(err, ErrProfile) {
		if other != nil {
			other.Close()
		}
		t.Fatalf("adopted an earlier configuration with an addition: %v", err)
	}
	if err := os.WriteFile(configPath, previous, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(catalogPath); err != nil {
		t.Fatal(err)
	}
	m = reopenAccountManager(t, options, "pending")
	m.Close()
	if current, err := os.ReadFile(configPath); err != nil || !bytes.Contains(current, []byte("\nmodel_catalog_json = \""+catalogPath+"\"\n")) {
		t.Fatalf("earlier configuration not rewritten: %v\n%s", err, current)
	}
	if written, err := os.ReadFile(catalogPath); err != nil || !bytes.Equal(written, modelCatalog) {
		t.Fatalf("catalog not written with the rewrite: %v", err)
	}
}

func modelCatalogConfig(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
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
