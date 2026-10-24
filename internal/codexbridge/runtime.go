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
	"regexp"
	"runtime"
	"strings"
	"time"
)

var (
	ErrNotInstalled = errors.New("the managed Codex runtime has not been prepared")
	ErrInstall      = errors.New("the managed Codex runtime could not be prepared")
)

// Release metadata is pinned in Desk, never supplied by a project, request or
// downloaded manifest. Digests were checked against OpenAI's rust-v0.157.1
// release asset and its extracted executable. Updating it needs protocol tests.
type runtimeArtifact struct {
	URL, Member, ArchiveSHA256, BinarySHA256 string
	ArchiveSize, BinarySize                  int64
}

var linuxAMD64Runtime = runtimeArtifact{
	URL:           "https://github.com/openai/codex/releases/download/rust-v0.157.1/codex-x86_64-unknown-linux-musl.tar.gz",
	Member:        "codex-x86_64-unknown-linux-musl",
	ArchiveSHA256: "e98c1e8e028e8137fa2d2415c82ec58e7b3701a627e3554aace5b3ca31454af2",
	BinarySHA256:  "3e2584f3f3829a43a0495011a1cecb2facbe64a2403e2b682351fd9c2983f970",
	ArchiveSize:   107868387, BinarySize: 285340072,
}

const runtimeFilename = "codex-0.157.1"

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
			return nil, failureCause(ErrProfile, err)
		}
	}
	info, err := p.root.Lstat("runtime")
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotInstalled
	}
	if err != nil || !privateInfo(info, true) {
		return nil, failureCause(ErrProfile, err)
	}
	root, err := p.root.OpenRoot("runtime")
	if err != nil {
		return nil, failureCause(ErrProfile, err)
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

type runtimeStage interface {
	io.ReadWriteSeeker
	Chmod(os.FileMode) error
	Sync() error
	Close() error
}

func (p *profile) newRuntimeTemp(root *os.Root) (runtimeStage, string, error) {
	if p.stageRuntime != nil {
		return p.stageRuntime(root)
	}
	return runtimeTemp(root)
}
func runtimeTemp(root *os.Root) (runtimeStage, string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, "", failureCause(ErrInstall, err)
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
		if err := root.Remove(entry.Name()); err != nil {
			return failureCause(ErrInstall, err)
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
		if install {
			err = p.pruneRuntimes(root)
		}
		return path, err
	}
	if !errors.Is(err, ErrNotInstalled) || !install {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	archive, archiveName, err := p.newRuntimeTemp(root)
	if err != nil {
		return "", failureCause(ErrInstall, err)
	}
	defer func() { archive.Close(); _ = root.Remove(archiveName) }()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return "", failureCause(ErrInstall, err)
	}
	request.Header.Set("User-Agent", "JPS-Desk-managed-runtime")
	response, err := client.Do(request)
	if err != nil {
		return "", failureCause(ErrInstall, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > artifact.ArchiveSize {
		return "", failureCause(ErrInstall, err)
	}
	hash := sha256.New()
	n, err := copyRuntime(ctx, io.MultiWriter(archive, hash), io.LimitReader(response.Body, artifact.ArchiveSize+1))
	if err != nil || n != artifact.ArchiveSize || hex.EncodeToString(hash.Sum(nil)) != artifact.ArchiveSHA256 {
		return "", failureCause(ErrInstall, err)
	}
	if _, err = archive.Seek(0, io.SeekStart); err != nil {
		return "", failureCause(ErrInstall, err)
	}
	compressed, err := gzip.NewReader(archive)
	if err != nil {
		return "", failureCause(ErrInstall, err)
	}
	defer compressed.Close()
	unpacked := io.LimitReader(compressed, artifact.BinarySize+(1<<20))
	contents := tar.NewReader(unpacked)
	header, err := contents.Next()
	if err != nil || header.Typeflag != tar.TypeReg || header.Name != artifact.Member || header.Size != artifact.BinarySize {
		return "", failureCause(ErrInstall, err)
	}
	binary, binaryName, err := p.newRuntimeTemp(root)
	if err != nil {
		return "", failureCause(ErrInstall, err)
	}
	defer func() { binary.Close(); _ = root.Remove(binaryName) }()
	hash = sha256.New()
	n, err = copyRuntime(ctx, io.MultiWriter(binary, hash), contents)
	if err != nil || n != artifact.BinarySize || hex.EncodeToString(hash.Sum(nil)) != artifact.BinarySHA256 {
		return "", failureCause(ErrInstall, err)
	}
	if _, err = contents.Next(); err != io.EOF {
		return "", failureCause(ErrInstall, err)
	}
	// Read the gzip trailer, checking its CRC and bounding any trailing payload.
	if n, err = copyRuntime(ctx, io.Discard, unpacked); err != nil || n > 16384 {
		return "", failureCause(ErrInstall, err)
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = binary.Chmod(0700); err == nil {
		err = binary.Sync()
	}
	if err == nil {
		err = binary.Close()
	}
	if err != nil {
		return "", failureCause(ErrInstall, err)
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
		return "", failureCause(ErrInstall, err)
	}
	dir, err := root.Open(".")
	if err != nil {
		return "", failureCause(ErrInstall, err)
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return "", failureCause(ErrInstall, err)
	}
	// Verify the published bytes before any superseded executable is removed.
	if err = verifyRuntime(ctx, root, artifact); err != nil {
		return "", err
	}
	if err = p.pruneRuntimes(root); err != nil {
		return "", err
	}
	return path, nil
}

var managedRuntimeName = regexp.MustCompile(`^codex-[0-9]+\.[0-9]+\.[0-9]+$`)

// Called only after verified preparation, under the Manager mutex and lifetime
// profile lease. Setup cannot overlap a live run; another Desk cannot prepare
// this profile. Unlink only owned, private, singly linked executable files by
// name through the held root. Never traverse or adopt unrelated cache entries.
func (p *profile) pruneRuntimes(root *os.Root) error {
	if err := p.check(); err != nil {
		return err
	}
	current, err := p.root.Lstat("runtime")
	held, heldErr := root.Stat(".")
	if err != nil || heldErr != nil || !os.SameFile(current, held) {
		return ErrProfile
	}
	dir, err := root.Open(".")
	if err != nil {
		return failureCause(ErrProfile, err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return failureCause(ErrProfile, err)
	}
	removed := false
	for _, entry := range entries {
		name := entry.Name()
		if name == runtimeFilename || !managedRuntimeName.MatchString(name) {
			continue
		}
		info, err := root.Lstat(name)
		if err != nil {
			return failureCause(ErrProfile, err)
		}
		if !privateInfo(info, false) || info.Mode().Perm()&0100 == 0 {
			continue
		}
		if err = root.Remove(name); err != nil {
			return failureCause(ErrInstall, err)
		}
		removed = true
	}
	if removed {
		if err = dir.Sync(); err != nil {
			return failureCause(ErrInstall, err)
		}
	}
	return nil
}
