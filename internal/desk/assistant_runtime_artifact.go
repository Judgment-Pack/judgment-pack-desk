package desk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// assistantRuntimeArtifact retains verified bytes independently of installation
// path updates. The launch owner must retain it until its exact process is reaped.
// This uses the trusted local operator boundary: it does not resist a process
// running as that operator, nor make path-based launch atomic against one.
type assistantRuntimeArtifact struct {
	mu        sync.Mutex
	path      string
	dir       string
	file      *os.File
	directory *os.File
	fileInfo  os.FileInfo
	dirInfo   os.FileInfo
	digest    string
	closed    bool
	closeErr  error
}

// expectedDigest must come from trusted host provisioning (the release build
// embeds the digest of its already-verified bundled Runtime). Hashing an
// arbitrary selected source does not establish this authority. The trusted host
// may separately classify explicit operator startup selection for local custody;
// a browser/model/checkpoint path or digest never supplies that authority.
func openAssistantRuntimeArtifact(ctx context.Context, source, expectedDigest string) (_ *assistantRuntimeArtifact, err error) {
	decoded, err := hex.DecodeString(expectedDigest)
	if err != nil || len(decoded) != sha256.Size || expectedDigest != strings.ToLower(expectedDigest) {
		return nil, errors.New("trusted runtime artifact digest unavailable or invalid")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// NONBLOCK also refuses a FIFO without waiting for an external writer.
	src, err := os.OpenFile(source, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open runtime artifact: %w", err)
	}
	defer src.Close()
	return holdAssistantRuntimeArtifact(ctx, src, expectedDigest)
}

// holdAssistantRuntimeArtifact copies from the exact held descriptor whose
// identity the trusted host selected. It never re-resolves a source pathname.
// expectedDigest may identify publisher-verified bundled bytes or an explicitly
// operator-authoritative local selection; neither interpretation is browser data.
func holdAssistantRuntimeArtifact(ctx context.Context, src *os.File, expectedDigest string) (_ *assistantRuntimeArtifact, err error) {
	decoded, err := hex.DecodeString(expectedDigest)
	if err != nil || len(decoded) != sha256.Size || expectedDigest != strings.ToLower(expectedDigest) {
		return nil, errors.New("trusted runtime artifact digest unavailable or invalid")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := src.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat runtime artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("runtime artifact is not a regular file")
	}
	dir, err := os.MkdirTemp("", "jpack-desk-runtime-")
	if err != nil {
		return nil, fmt.Errorf("create private runtime directory: %w", err)
	}
	// Freeze the absolute launch path even if the operator supplied a relative
	// temporary directory and the later child uses a different working directory.
	absoluteDir, err := filepath.Abs(dir)
	if err != nil {
		_ = os.Remove(dir)
		return nil, err
	}
	a := &assistantRuntimeArtifact{dir: absoluteDir, path: filepath.Join(absoluteDir, "jpack"), digest: expectedDigest}
	// This exclusive directory contains only our exact file. Do not RemoveAll:
	// even cleanup must not delete a replacement or unrelated directory entry.
	defer func() {
		if err != nil {
			if a.file != nil {
				_ = a.file.Close()
			}
			if a.directory != nil {
				_ = a.directory.Close()
			}
			if current, statErr := os.Lstat(a.path); statErr == nil && a.fileInfo != nil && os.SameFile(a.fileInfo, current) {
				_ = os.Remove(a.path)
			}
			if current, statErr := os.Lstat(a.dir); statErr == nil && a.dirInfo != nil && os.SameFile(a.dirInfo, current) {
				_ = os.Remove(a.dir)
			}
		}
	}()
	a.dirInfo, err = os.Lstat(a.dir)
	if err != nil {
		return nil, err
	}
	a.directory, err = os.Open(a.dir)
	if err != nil {
		return nil, err
	}
	a.dirInfo, err = a.directory.Stat()
	if err != nil {
		return nil, err
	}
	dst, err := os.OpenFile(a.path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("create private runtime artifact: %w", err)
	}
	a.fileInfo, err = dst.Stat()
	if err != nil {
		_ = dst.Close()
		return nil, err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(dst, hash), assistantRuntimeArtifactReader{ctx: ctx, reader: io.NewSectionReader(src, 0, info.Size())})
	if current, statErr := src.Stat(); copyErr == nil && (statErr != nil || !os.SameFile(info, current) || current.Size() != info.Size()) {
		copyErr = errors.New("runtime artifact changed while copying")
	}
	if copyErr == nil && hex.EncodeToString(hash.Sum(nil)) != expectedDigest {
		copyErr = errors.New("runtime artifact digest mismatch")
	}
	if copyErr == nil {
		copyErr = dst.Sync()
	}
	if copyErr == nil {
		copyErr = dst.Chmod(0500)
	}
	closeErr := dst.Close()
	if copyErr != nil {
		return nil, fmt.Errorf("copy runtime artifact: %w", copyErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close writable runtime artifact: %w", closeErr)
	}
	// No writable handle survives successful construction. Verify destination
	// bytes separately, retaining the file and directory identities for Current.
	a.file, err = os.Open(a.path)
	if err != nil {
		return nil, err
	}
	a.fileInfo, err = a.file.Stat()
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = a.Current(); err != nil {
		return nil, err
	}
	return a, nil
}

type assistantRuntimeArtifactReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r assistantRuntimeArtifactReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (a *assistantRuntimeArtifact) currentIdentity() error {
	directory, err := a.directory.Stat()
	if err != nil {
		return err
	}
	dirPath, err := os.Lstat(a.dir)
	if err != nil || !directory.IsDir() || directory.Mode() != os.ModeDir|0700 || !os.SameFile(a.dirInfo, directory) || !os.SameFile(directory, dirPath) {
		return errors.New("private runtime directory identity changed")
	}
	file, err := a.file.Stat()
	if err != nil {
		return err
	}
	filePath, err := os.Lstat(a.path)
	if err != nil || !file.Mode().IsRegular() || file.Mode() != 0500 || file.Size() != a.fileInfo.Size() || !os.SameFile(a.fileInfo, file) || !os.SameFile(file, filePath) {
		return errors.New("private runtime artifact identity changed")
	}
	return nil
}

// Current rechecks the held bytes and the private launch path. The owner still
// checks process/transport liveness separately; this helper enables no port.
func (a *assistantRuntimeArtifact) Current() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("runtime artifact closed")
	}
	if err := a.currentIdentity(); err != nil {
		return err
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.NewSectionReader(a.file, 0, a.fileInfo.Size()))
	if err != nil {
		return fmt.Errorf("verify private runtime artifact: %w", err)
	}
	if n != a.fileInfo.Size() || hex.EncodeToString(hash.Sum(nil)) != a.digest {
		return errors.New("private runtime artifact digest changed")
	}
	return a.currentIdentity()
}

// Close releases held handles exactly once. The launch owner calls it only
// after reaping its process. Repeated calls return the original cleanup result.
func (a *assistantRuntimeArtifact) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return a.closeErr
	}
	a.closed = true
	filePath, fileErr := os.Lstat(a.path)
	dirPath, dirErr := os.Lstat(a.dir)
	a.closeErr = errors.Join(a.file.Close(), a.directory.Close())
	if fileErr == nil && os.SameFile(a.fileInfo, filePath) && dirErr == nil && os.SameFile(a.dirInfo, dirPath) {
		a.closeErr = errors.Join(a.closeErr, os.Remove(a.path))
	} else {
		a.closeErr = errors.Join(a.closeErr, errors.New("private runtime artifact cleanup identity changed"))
	}
	if dirErr == nil && os.SameFile(a.dirInfo, dirPath) {
		a.closeErr = errors.Join(a.closeErr, os.Remove(a.dir))
	} else {
		a.closeErr = errors.Join(a.closeErr, errors.New("private runtime directory cleanup identity changed"))
	}
	return a.closeErr
}
