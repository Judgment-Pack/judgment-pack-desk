package codexbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var ErrProfile = errors.New("the private Codex profile is unavailable or unsafe")

// profile is deliberately separate from both terminal CODEX_HOME and projects.
// The only persistent auth-operation record is a cleanup bit, never credentials.
type profile struct {
	root                         *os.Root
	lease                        *os.File
	dir, home, codex, work, temp string
}

const profileMarker = "Desk managed Codex profile v1\n"

func openProfile(dir, project string) (*profile, error) {
	if !filepath.IsAbs(dir) || !filepath.IsAbs(project) || filepath.Clean(dir) != dir {
		return nil, ErrProfile
	}
	project, err := filepath.EvalSymlinks(project)
	if err != nil || within(dir, project) || within(project, dir) {
		return nil, ErrProfile
	}
	// The owner-controlled Desk configuration directory must already exist. Never
	// create parents, follow a nominated symlink, or adopt a terminal profile.
	parent := filepath.Dir(dir)
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		return nil, ErrProfile
	}
	info, err := os.Lstat(parent)
	if err != nil || !privateParent(info) {
		return nil, ErrProfile
	}
	parentRoot, err := os.OpenRoot(parent)
	if err != nil {
		return nil, ErrProfile
	}
	defer parentRoot.Close()
	name := filepath.Base(dir)
	created := false
	if err = parentRoot.Mkdir(name, 0700); err == nil {
		created = true
	} else if !errors.Is(err, os.ErrExist) {
		return nil, ErrProfile
	}
	info, err = parentRoot.Lstat(name)
	if err != nil || !privateInfo(info, true) {
		return nil, ErrProfile
	}
	root, err := parentRoot.OpenRoot(name)
	if err != nil {
		return nil, ErrProfile
	}
	p := &profile{root: root, dir: dir, home: filepath.Join(dir, "home"), codex: filepath.Join(dir, "profile"), work: filepath.Join(dir, "work"), temp: filepath.Join(dir, "tmp")}
	fail := func(err error) (*profile, error) { p.close(); return nil, err }
	p.lease, err = lockProfile(root)
	if err != nil {
		return fail(err)
	}
	if created {
		if err = p.writeNew("managed", []byte(profileMarker)); err != nil {
			return fail(err)
		}
	} else if data, err := p.readSmall("managed"); err != nil || string(data) != profileMarker {
		return fail(ErrProfile)
	}
	for _, child := range []string{"home", "profile", "work", "tmp"} {
		if err = root.Mkdir(child, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return fail(ErrProfile)
		}
		info, err = root.Lstat(child)
		if err != nil || !privateInfo(info, true) {
			return fail(ErrProfile)
		}
	}
	for _, file := range p.managedFiles() {
		if _, err = root.Lstat(file.name); errors.Is(err, os.ErrNotExist) {
			err = p.writeNew(file.name, file.data)
		} else if data, readErr := p.readBounded(file.name, len(file.data)); readErr != nil || !bytes.Equal(data, file.data) {
			err = ErrProfile
			if readErr == nil && file.name == "profile/config.toml" && p.previousConfig(data) {
				// Desk's own earlier configuration, from before it named the
				// catalog: an upgrade, not a replaced file. Anything else stays
				// refused.
				if err = root.Remove(file.name); err == nil {
					err = p.writeNew(file.name, file.data)
				}
			}
		}
		if err != nil {
			return fail(ErrProfile)
		}
	}
	if err = p.check(); err != nil {
		return fail(err)
	}
	return p, nil
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (p *profile) check() error {
	// Native Codex opens by pathname, so detect a renamed/replaced profile
	// before every launch. The held root and lease govern Desk's own writes.
	pathInfo, err := os.Lstat(p.dir)
	held, heldErr := p.root.Stat(".")
	if err != nil || heldErr != nil || !privateInfo(pathInfo, true) || !os.SameFile(pathInfo, held) {
		return ErrProfile
	}
	for _, name := range []string{"home", "profile", "work", "tmp"} {
		info, err := p.root.Lstat(name)
		if err != nil || !privateInfo(info, true) {
			return ErrProfile
		}
	}
	for _, file := range p.managedFiles() {
		data, err := p.readBounded(file.name, len(file.data))
		if err != nil || !bytes.Equal(data, file.data) {
			return ErrProfile
		}
	}
	for _, name := range []string{"profile/auth.json", "cleanup"} {
		info, err := p.root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !privateInfo(info, false) {
			return ErrProfile
		}
	}
	return nil
}

// managedFiles are written once and must read back byte for byte before every
// launch: the configuration, and the closed model catalog it names.
func (p *profile) managedFiles() []struct {
	name string
	data []byte
} {
	return []struct {
		name string
		data []byte
	}{{"profile/config.toml", p.config()}, {"profile/" + catalogFilename, modelCatalog}}
}

func (p *profile) readSmall(name string) ([]byte, error) { return p.readBounded(name, 16384) }

func (p *profile) readBounded(name string, limit int) ([]byte, error) {
	info, err := p.root.Lstat(name)
	if err != nil || !privateInfo(info, false) || info.Size() > int64(limit) {
		return nil, ErrProfile
	}
	f, err := p.root.OpenFile(name, os.O_RDONLY|noFollow, 0)
	if err != nil {
		return nil, ErrProfile
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) > limit {
		return nil, ErrProfile
	}
	return data, nil
}

func (p *profile) writeNew(name string, data []byte) error {
	f, err := p.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|noFollow, 0600)
	if err != nil {
		return ErrProfile
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return ErrProfile
	}
	return p.sync()
}
func (p *profile) sync() error {
	f, err := p.root.Open(".")
	if err != nil {
		return ErrProfile
	}
	defer f.Close()
	if f.Sync() != nil {
		return ErrProfile
	}
	return nil
}
func (p *profile) needsCleanup() (bool, error) {
	_, err := p.root.Lstat("cleanup")
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, ErrProfile
	}
	data, err := p.readSmall("cleanup")
	if err != nil || string(data) != "pending\n" {
		return false, ErrProfile
	}
	return true, nil
}
func (p *profile) markCleanup() error {
	pending, err := p.needsCleanup()
	if err != nil || pending {
		return err
	}
	return p.writeNew("cleanup", []byte("pending\n"))
}
func (p *profile) clearCleanup() error {
	if err := p.root.Remove("cleanup"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrProfile
	}
	return p.sync()
}
func (p *profile) close() {
	if p.lease != nil {
		_ = p.lease.Close()
	}
	if p.root != nil {
		_ = p.root.Close()
	}
}

func (p *profile) command(binary string) *exec.Cmd {
	cmd := exec.Command(binary, "app-server", "--stdio", "--strict-config")
	cmd.Dir = p.work
	// No inheritance: terminal auth, alternate providers, proxy variables, and
	// API keys cannot retarget this subscription profile.
	cmd.Env = []string{
		"HOME=" + p.home, "CODEX_HOME=" + p.codex, "TMPDIR=" + p.temp,
		"XDG_CONFIG_HOME=" + p.home, "XDG_CACHE_HOME=" + p.home,
		"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "RUST_LOG=off",
	}
	return cmd
}

func quote(v string) string { b, _ := json.Marshal(v); return string(b) }

func (p *profile) catalogLine() string {
	return "model_catalog_json = " + quote(filepath.Join(p.codex, catalogFilename)) + "\n"
}

// previousConfig reports whether data is exactly the configuration the Desk
// before the model catalog wrote for this profile: today's without its line.
func (p *profile) previousConfig(data []byte) bool {
	return string(data) == strings.Replace(string(p.config()), p.catalogLine(), "", 1)
}

func (p *profile) config() []byte {
	return []byte(`model_provider = "openai"
` + p.catalogLine() + `approval_policy = "never"
forced_login_method = "chatgpt"
cli_auth_credentials_store = "file"
web_search = "disabled"
project_doc_max_bytes = 0
default_permissions = "jps"
[permissions.jps.filesystem]
":minimal" = "read"
` + quote(p.work) + ` = "read"
` + quote(p.codex) + ` = "deny"
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
