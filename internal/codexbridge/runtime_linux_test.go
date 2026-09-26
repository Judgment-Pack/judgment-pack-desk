package codexbridge

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func runtimeFixture(t *testing.T, member string, kind byte) (runtimeArtifact, []byte) {
	t.Helper()
	executable := []byte("synthetic executable; never executed\n")
	var data bytes.Buffer
	compressed := gzip.NewWriter(&data)
	archive := tar.NewWriter(compressed)
	size := int64(len(executable))
	if kind != tar.TypeReg {
		size = 0
	}
	if err := archive.WriteHeader(&tar.Header{Name: member, Typeflag: kind, Size: size, Mode: 0700, Linkname: "/outside"}); err != nil {
		t.Fatal(err)
	}
	if size > 0 {
		_, _ = archive.Write(executable)
	}
	_ = archive.Close()
	_ = compressed.Close()
	a, b := sha256.Sum256(data.Bytes()), sha256.Sum256(executable)
	return runtimeArtifact{Member: "codex-fixture", ArchiveSHA256: hex.EncodeToString(a[:]), BinarySHA256: hex.EncodeToString(b[:]), ArchiveSize: int64(data.Len()), BinarySize: int64(len(executable))}, data.Bytes()
}
func serveRuntime(t *testing.T, artifact *runtimeArtifact, data []byte) (*http.Client, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = w.Write(data) }))
	t.Cleanup(server.Close)
	artifact.URL = server.URL
	return server.Client(), calls
}
func TestManagedRuntimeExplicitPreparationCacheRepairAndRestart(t *testing.T) {
	m, options := accountManager(t, "pending")
	artifact, data := runtimeFixture(t, "codex-fixture", tar.TypeReg)
	client, calls := serveRuntime(t, &artifact, data)
	ctx := context.Background()
	if _, err := m.profile.resolveRuntime(ctx, false, artifact, client); !errors.Is(err, ErrNotInstalled) {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("status downloaded a runtime")
	}
	binary, err := m.profile.resolveRuntime(ctx, true, artifact, client)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(binary)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("permissions: %v %v", info, err)
	}
	m.Close()
	m = reopenAccountManager(t, options, "pending")
	if _, err = m.profile.resolveRuntime(ctx, false, artifact, client); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("restart downloaded an installed runtime")
	}
	if err = os.WriteFile(binary, bytes.Repeat([]byte("x"), int(artifact.BinarySize)), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = m.profile.resolveRuntime(ctx, false, artifact, client); !errors.Is(err, ErrNotInstalled) {
		t.Fatal("tampered executable accepted", err)
	}
	if calls.Load() != 1 {
		t.Fatal("read-only status repaired runtime")
	}
	if _, err = m.profile.resolveRuntime(ctx, true, artifact, client); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("explicit connection did not repair runtime")
	}
	files, _ := os.ReadDir(filepath.Dir(binary))
	if len(files) != 1 {
		t.Fatal("left staged files", files)
	}
}
func TestManagedRuntimeRefusesUnverifiedOrUnsafeArchives(t *testing.T) {
	for _, kind := range []string{"archive-digest", "binary-digest", "truncated", "oversized", "traversal", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			m, _ := accountManager(t, "pending")
			name, typ := "codex-fixture", byte(tar.TypeReg)
			if kind == "traversal" {
				name = "../codex-fixture"
			}
			if kind == "symlink" {
				typ = tar.TypeSymlink
			}
			if kind == "hardlink" {
				typ = tar.TypeLink
			}
			artifact, data := runtimeFixture(t, name, typ)
			switch kind {
			case "archive-digest":
				artifact.ArchiveSHA256 = strings.Repeat("0", 64)
			case "binary-digest":
				artifact.BinarySHA256 = strings.Repeat("0", 64)
			case "truncated":
				data = data[:len(data)-1]
			case "oversized":
				data = append(data, 0)
			}
			client, _ := serveRuntime(t, &artifact, data)
			if _, err := m.profile.resolveRuntime(context.Background(), true, artifact, client); !errors.Is(err, ErrInstall) {
				t.Fatal("accepted malformed download", err)
			}
			entries, _ := os.ReadDir(filepath.Join(m.profile.dir, "runtime"))
			if len(entries) != 0 {
				t.Fatal("partial files left", entries)
			}
		})
	}
}
func TestManagedRuntimeRejectsUnsafeCacheLocations(t *testing.T) {
	for _, kind := range []string{"directory-link", "file-link", "file-hardlink", "public-directory"} {
		t.Run(kind, func(t *testing.T) {
			m, _ := accountManager(t, "pending")
			artifact, data := runtimeFixture(t, "codex-fixture", tar.TypeReg)
			client, calls := serveRuntime(t, &artifact, data)
			dir := filepath.Join(m.profile.dir, "runtime")
			target := filepath.Join(t.TempDir(), "external")
			_ = os.WriteFile(target, []byte("untouched"), 0600)
			if kind == "directory-link" {
				_ = os.Symlink(t.TempDir(), dir)
			} else {
				_ = os.Mkdir(dir, 0700)
				switch kind {
				case "public-directory":
					_ = os.Chmod(dir, 0777)
				case "file-link":
					_ = os.Symlink(target, filepath.Join(dir, runtimeFilename))
				case "file-hardlink":
					_ = os.Link(target, filepath.Join(dir, runtimeFilename))
				}
			}
			if _, err := m.profile.resolveRuntime(context.Background(), true, artifact, client); !errors.Is(err, ErrProfile) {
				t.Fatal(err)
			}
			if calls.Load() != 0 {
				t.Fatal("unsafe location reached network")
			}
			contents, _ := os.ReadFile(target)
			if string(contents) != "untouched" {
				t.Fatal("external target changed")
			}
		})
	}
}
func TestManagedRuntimeCancellationAndRetry(t *testing.T) {
	m, _ := accountManager(t, "pending")
	artifact, data := runtimeFixture(t, "codex-fixture", tar.TypeReg)
	began := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(began)
			<-r.Context().Done()
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	artifact.URL = server.URL
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := m.profile.resolveRuntime(ctx, true, artifact, server.Client()); done <- err }()
	<-began
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancellation succeeded")
	}
	entries, _ := os.ReadDir(filepath.Join(m.profile.dir, "runtime"))
	if len(entries) != 0 {
		t.Fatal("canceled staging retained", entries)
	}
	if _, err := m.profile.resolveRuntime(context.Background(), true, artifact, server.Client()); err != nil {
		t.Fatal(err)
	}
}
func TestManagedRuntimeRedirectPolicy(t *testing.T) {
	for _, raw := range []string{"http://github.com/x", "https://example.com/x", "https://user:secret@github.com/x", "https://github.com:8443/x", "https://github.com.example.com/x"} {
		u, _ := url.Parse(raw)
		if runtimeDownloadURL(u) {
			t.Fatal("allowed redirect", raw)
		}
	}
	for _, raw := range []string{"https://github.com/openai/codex", "https://release-assets.githubusercontent.com/file"} {
		u, _ := url.Parse(raw)
		if !runtimeDownloadURL(u) {
			t.Fatal("refused release host", raw)
		}
	}
}
func TestManagedAccountPreparesOnlyOnLoginAndCancellationFollowsOwner(t *testing.T) {
	m, _ := accountManager(t, "pending")
	m.managed = true
	var checks, installs int
	m.resolveBinary = func(ctx context.Context, install bool) (string, error) {
		if install {
			installs++
		} else {
			checks++
		}
		return "", ErrNotInstalled
	}
	status, err := m.Status(context.Background(), "owner", false)
	if err != nil || status.Runtime != "not-installed" || installs != 0 || checks != 1 {
		t.Fatalf("passive status: %+v %v %d %d", status, err, checks, installs)
	}
	owner, endSession, endPolicy := ownerFixture()
	defer endSession()
	defer endPolicy()
	started := make(chan struct{})
	m.resolveBinary = func(ctx context.Context, install bool) (string, error) {
		if !install {
			t.Error("login did not prepare")
		}
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	done := make(chan error, 1)
	go func() { _, err := m.StartLogin(context.Background(), owner, "browser"); done <- err }()
	<-started
	if _, err = m.Status(context.Background(), owner.ID, false); !errors.Is(err, ErrBusy) {
		t.Fatal("concurrent operation accepted", err)
	}
	endSession()
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("revocation did not cancel preparation")
	}
}

// Opt-in native smoke uses only a previously downloaded official archive served
// by a local fixture. No account is read, adopted, or signed in.
func TestManagedNativeRuntimeFromVerifiedArchive(t *testing.T) {
	archive := os.Getenv("JPS_CODEX_MANAGED_ARCHIVE")
	if archive == "" {
		t.Skip("set JPS_CODEX_MANAGED_ARCHIVE to the official pinned archive")
	}
	if !ManagedRuntimeSupported() {
		t.Skip("unsupported managed host")
	}
	m, err := NewManager(Options{ProfileDir: filepath.Join(t.TempDir(), "codex"), ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	a := linuxAMD64Runtime
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(archive)
		if err != nil {
			t.Error(err)
			http.Error(w, "fixture unavailable", 500)
			return
		}
		defer f.Close()
		_, _ = io.Copy(w, f)
	}))
	defer server.Close()
	a.URL = server.URL
	binary, err := m.profile.resolveRuntime(context.Background(), true, a, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if binary == "" {
		t.Fatal("missing prepared executable")
	}
	status, err := m.Status(context.Background(), "smoke", false)
	if err != nil || status.Runtime != "available" || status.Account != "signed-out" {
		t.Fatalf("native managed status: %+v %v", status, err)
	}
}

func TestManagedAccountConnectAndRememberWithoutExecutableFlag(t *testing.T) {
	m, options := accountManager(t, "success")
	m.managed = true
	installed := false
	installs := 0
	resolve := func(_ context.Context, install bool) (string, error) {
		if !installed && !install {
			return "", ErrNotInstalled
		}
		if !installed {
			installs++
			installed = true
		}
		return options.Binary, nil
	}
	m.resolveBinary = resolve
	owner, endSession, endPolicy := ownerFixture()
	defer endSession()
	defer endPolicy()
	if _, err := m.StartLogin(context.Background(), owner, "browser"); err != nil {
		t.Fatal(err)
	}
	awaitAccount(t, m, "connected", "connected")
	m.Close()
	m = reopenAccountManager(t, options, "pending")
	m.managed = true
	m.resolveBinary = resolve
	status, err := m.Status(context.Background(), owner.ID, false)
	if err != nil || status.Account != "connected" || installs != 1 {
		t.Fatalf("not remembered: %+v %v installs=%d", status, err, installs)
	}
}

func TestManagedRuntimeRecoversAbandonedStagingOnlyOnConnect(t *testing.T) {
	m, _ := accountManager(t, "pending")
	artifact, data := runtimeFixture(t, "codex-fixture", tar.TypeReg)
	client, _ := serveRuntime(t, &artifact, data)
	root, err := m.profile.runtimeRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	staged, name, err := runtimeTemp(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = staged.Write([]byte("interrupted download"))
	_ = staged.Close()
	if _, err = m.profile.resolveRuntime(context.Background(), false, artifact, client); !errors.Is(err, ErrNotInstalled) {
		t.Fatal(err)
	}
	if _, err = root.Stat(name); err != nil {
		t.Fatal("read-only check removed staging", err)
	}
	if _, err = m.profile.resolveRuntime(context.Background(), true, artifact, client); err != nil {
		t.Fatal(err)
	}
	if _, err = root.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("orphaned staging left behind", err)
	}
}
