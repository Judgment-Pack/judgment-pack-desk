package codexbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	// A managed file changed while the profile is held is refused before the
	// next launch, whether the change keeps the size or grows it, and nothing
	// is launched.
	changed := bytes.Replace(modelCatalog, []byte(`"tool_mode": null`), []byte(`"tool_mode": "code_mode_only"`), 1)
	if bytes.Equal(changed, modelCatalog) {
		t.Fatal("catalog has no tool_mode member to change")
	}
	sameSize := append([]byte{}, modelCatalog...)
	sameSize[bytes.Index(sameSize, []byte(`"tool_mode": null`))+len(`"tool_mode": `)] = 'N'
	m.launch = func(ctx context.Context, native *exec.Cmd) (*client, error) {
		t.Error("launched with a changed managed file")
		return nil, ErrUnavailable
	}
	for _, late := range [][]byte{sameSize, changed} {
		if err := os.WriteFile(catalogPath, late, 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := m.Status(ctx, "test", false)
		cancel()
		if !errors.Is(err, ErrProfile) {
			t.Fatalf("launch after the catalog changed underneath: %v", err)
		}
	}
	if err := os.WriteFile(catalogPath, modelCatalog, 0600); err != nil {
		t.Fatal(err)
	}
	m.Close()
	// A replaced managed file is refused on open, never silently repaired: the
	// catalog with one member changed, then the configuration.
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
	previous := configBeforeTheCatalog(options.ProfileDir)
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
	// A replacement staged by an interrupted earlier rewrite is discarded, and
	// the account file the earlier Desk left is not touched.
	if err := os.WriteFile(configPath+".new", []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(options.ProfileDir, "profile/auth.json")
	if err := os.WriteFile(authPath, []byte(`{"tokens": "PRIVATE_SENTINEL"}`), 0600); err != nil {
		t.Fatal(err)
	}
	m = reopenAccountManager(t, options, "pending")
	m.Close()
	if current, err := os.ReadFile(configPath); err != nil || !bytes.Contains(current, []byte("\nmodel_catalog_json = \""+catalogPath+"\"\n")) {
		t.Fatalf("earlier configuration not rewritten: %v\n%s", err, current)
	}
	if _, err := os.Lstat(configPath + ".new"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged replacement left behind: %v", err)
	}
	if written, err := os.ReadFile(catalogPath); err != nil || !bytes.Equal(written, modelCatalog) {
		t.Fatalf("catalog not written with the rewrite: %v", err)
	}
	if auth, err := os.ReadFile(authPath); err != nil || string(auth) != `{"tokens": "PRIVATE_SENTINEL"}` {
		t.Fatalf("account file changed by the rewrite: %v %q", err, auth)
	}
}

// configBeforeTheCatalog is, word for word, the configuration the Desk at
// e36244e wrote for a profile: what an upgrading profile holds. It is kept
// verbatim rather than derived from today's template, so that a change to the
// template cannot silently move what the upgrade recognises.
func configBeforeTheCatalog(profileDir string) []byte {
	quote := func(v string) string { b, _ := json.Marshal(v); return string(b) }
	return []byte(`model_provider = "openai"
approval_policy = "never"
forced_login_method = "chatgpt"
cli_auth_credentials_store = "file"
web_search = "disabled"
project_doc_max_bytes = 0
default_permissions = "jps"
[permissions.jps.filesystem]
":minimal" = "read"
` + quote(filepath.Join(profileDir, "work")) + ` = "read"
` + quote(filepath.Join(profileDir, "profile")) + ` = "deny"
[permissions.jps.network]
enabled = false
[skills]
include_instructions = false
[skills.bundled]
enabled = false
[analytics]
enabled = false
[history]
persistence = "none"
[tools.experimental_request_user_input]
enabled = false
[features]
shell_tool = false
unified_exec = false
shell_snapshot = false
multi_agent = false
multi_agent_v2 = false
apps = false
hooks = false
plugins = false
remote_plugin = false
plugin_sharing = false
memories = false
goals = false
browser_use = false
browser_use_external = false
browser_use_full_cdp_access = false
computer_use = false
in_app_browser = false
skill_mcp_dependency_install = false
skill_search = false
workspace_dependencies = false
auth_elicitation = false
tool_suggest = false
image_generation = false
code_mode = false
code_mode_host = false
enable_request_compression = false
`)
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
