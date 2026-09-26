package codexbridge

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	ErrNotInstalled = errors.New("the managed Codex runtime has not been prepared")
	ErrInstall      = errors.New("the managed Codex runtime could not be prepared")
)

// Release metadata is pinned in Desk, never supplied by a project, request or
// downloaded manifest. Digests were checked against OpenAI's rust-v0.156.0
// release asset and its extracted executable. Updating it needs protocol tests.
type runtimeArtifact struct {
	URL, Member, ArchiveSHA256, BinarySHA256 string
	ArchiveSize, BinarySize                  int64
}

var linuxAMD64Runtime = runtimeArtifact{
	URL:           "https://github.com/openai/codex/releases/download/rust-v0.156.0/codex-x86_64-unknown-linux-musl.tar.gz",
	Member:        "codex-x86_64-unknown-linux-musl",
	ArchiveSHA256: "3d49d9af25a5168cfc51e50e520ab238b23083c259ae7c14f89b007cb2545c7b",
	BinarySHA256:  "78a11f06e0a2dda42d13fba1d50dc62e8cbdb2d5f69789722f4d4d99b5cdbe30",
	ArchiveSize:   107345927, BinarySize: 284361064,
}

const runtimeFilename = "codex-0.156.0"

func ManagedRuntimeSupported() bool { return runtime.GOOS == "linux" && runtime.GOARCH == "amd64" }

func runtimeDownloadURL(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Hostname() {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}
func runtimeHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // No inherited proxy credentials or endpoint overrides.
	return &http.Client{Transport: transport, Timeout: 3 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !runtimeDownloadURL(req.URL) {
			return ErrInstall
		}
		return nil
	}}
}

// Called under the Manager/profile lease. Read-only checks never download.
// Installation happens only inside an explicitly requested account login.
func (p *profile) managedBinary(ctx context.Context, install bool) (string, error) {
	if !ManagedRuntimeSupported() {
		return "", ErrUnavailable
	}
	client := runtimeHTTPClient()
	defer client.CloseIdleConnections()
	return p.resolveRuntime(ctx, install, linuxAMD64Runtime, client)
}
func (p *profile) runtimeRoot(create bool) (*os.Root, error) {
	if err := p.check(); err != nil {
		return nil, err
	}
	if create {
		if err := p.root.Mkdir("runtime", 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, ErrProfile
		}
	}
	info, err := p.root.Lstat("runtime")
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotInstalled
	}
	if err != nil || !privateInfo(info, true) {
		return nil, ErrProfile
	}
	root, err := p.root.OpenRoot("runtime")
	if err != nil {
		return nil, ErrProfile
	}
	return root, nil
}
func copyRuntime(ctx context.Context, out io.Writer, in io.Reader) (int64, error) {
	buffer := make([]byte, 128*1024)
	var count int64
	for {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		n, err := in.Read(buffer)
		if n > 0 {
			written, werr := out.Write(buffer[:n])
			count += int64(written)
			if werr != nil {
				return count, werr
			}
			if written != n {
				return count, io.ErrShortWrite
			}
		}
		if err == io.EOF {
			return count, nil
		}
		if err != nil {
			return count, err
		}
	}
}
func verifyRuntime(ctx context.Context, root *os.Root, artifact runtimeArtifact) error {
	info, err := root.Lstat(runtimeFilename)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotInstalled
	}
	if err != nil || !privateInfo(info, false) {
		return ErrProfile
	}
	if info.Size() != artifact.BinarySize || info.Mode().Perm()&0100 == 0 {
		return ErrNotInstalled
	}
	f, err := root.OpenFile(runtimeFilename, os.O_RDONLY|noFollow, 0)
	if err != nil {
		return ErrProfile
	}
	defer f.Close()
	hash := sha256.New()
	n, err := copyRuntime(ctx, hash, io.LimitReader(f, artifact.BinarySize+1))
	if err != nil {
		return err
	}
	if n != artifact.BinarySize || hex.EncodeToString(hash.Sum(nil)) != artifact.BinarySHA256 {
		return ErrNotInstalled
	}
	return nil
}
func runtimeTemp(root *os.Root) (*os.File, string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, "", ErrInstall
	}
	name := ".prepare-" + hex.EncodeToString(token[:])
	f, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL|noFollow, 0600)
	return f, name, err
}

// The profile lease excludes another live installer. Recover staging left by
// a killed Desk process, without touching any published version or auth file.
func cleanupRuntimeStaging(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return ErrProfile
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return ErrProfile
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".prepare-") {
			continue
		}
		token, err := hex.DecodeString(strings.TrimPrefix(entry.Name(), ".prepare-"))
		if err != nil || len(token) != 16 {
			continue
		}
		info, err := root.Lstat(entry.Name())
		if err != nil || !privateInfo(info, false) {
			return ErrProfile
		}
		if root.Remove(entry.Name()) != nil {
			return ErrInstall
		}
	}
	return nil
}
func (p *profile) resolveRuntime(ctx context.Context, install bool, artifact runtimeArtifact, client *http.Client) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, err := p.runtimeRoot(install)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if install {
		if err = cleanupRuntimeStaging(root); err != nil {
			return "", err
		}
	}
	path := filepath.Join(p.dir, "runtime", runtimeFilename)
	err = verifyRuntime(ctx, root, artifact)
	if err == nil {
		return path, nil
	}
	if !errors.Is(err, ErrNotInstalled) || !install {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	archive, archiveName, err := runtimeTemp(root)
	if err != nil {
		return "", ErrInstall
	}
	defer func() { archive.Close(); _ = root.Remove(archiveName) }()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return "", ErrInstall
	}
	request.Header.Set("User-Agent", "JPS-Desk-managed-runtime")
	response, err := client.Do(request)
	if err != nil {
		return "", ErrInstall
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > artifact.ArchiveSize {
		return "", ErrInstall
	}
	hash := sha256.New()
	n, err := copyRuntime(ctx, io.MultiWriter(archive, hash), io.LimitReader(response.Body, artifact.ArchiveSize+1))
	if err != nil || n != artifact.ArchiveSize || hex.EncodeToString(hash.Sum(nil)) != artifact.ArchiveSHA256 {
		return "", ErrInstall
	}
	if _, err = archive.Seek(0, io.SeekStart); err != nil {
		return "", ErrInstall
	}
	compressed, err := gzip.NewReader(archive)
	if err != nil {
		return "", ErrInstall
	}
	defer compressed.Close()
	unpacked := io.LimitReader(compressed, artifact.BinarySize+(1<<20))
	contents := tar.NewReader(unpacked)
	header, err := contents.Next()
	if err != nil || header.Typeflag != tar.TypeReg || header.Name != artifact.Member || header.Size != artifact.BinarySize {
		return "", ErrInstall
	}
	binary, binaryName, err := runtimeTemp(root)
	if err != nil {
		return "", ErrInstall
	}
	defer func() { binary.Close(); _ = root.Remove(binaryName) }()
	hash = sha256.New()
	n, err = copyRuntime(ctx, io.MultiWriter(binary, hash), contents)
	if err != nil || n != artifact.BinarySize || hex.EncodeToString(hash.Sum(nil)) != artifact.BinarySHA256 {
		return "", ErrInstall
	}
	if _, err = contents.Next(); err != io.EOF {
		return "", ErrInstall
	}
	// Read the gzip trailer, checking its CRC and bounding any trailing payload.
	if n, err = copyRuntime(ctx, io.Discard, unpacked); err != nil || n > 16384 {
		return "", ErrInstall
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if binary.Chmod(0700) != nil || binary.Sync() != nil || binary.Close() != nil {
		return "", ErrInstall
	}
	// Recheck custody before publishing; no partial executable is ever launched.
	if err = p.check(); err != nil {
		return "", err
	}
	current, err := p.root.Lstat("runtime")
	held, heldErr := root.Stat(".")
	if err != nil || heldErr != nil || !os.SameFile(current, held) {
		return "", ErrProfile
	}
	if err = root.Rename(binaryName, runtimeFilename); err != nil {
		return "", ErrInstall
	}
	dir, err := root.Open(".")
	if err != nil {
		return "", ErrInstall
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return "", ErrInstall
	}
	return path, nil
}
