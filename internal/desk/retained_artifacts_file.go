package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
)

const retainedArtifactDirectory = ".desk-private/retained-artifacts"

type fileRetainedArtifactRepository struct {
	open      func(bool) (*os.Root, error)
	authorize func(context.Context) error
}

func (f fileRetainedArtifactRepository) WithAuthority(authorize func(context.Context) error) retainedArtifactRepository {
	f.authorize = authorize
	return f
}

func (f fileRetainedArtifactRepository) current(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.authorize != nil {
		return f.authorize(ctx)
	}
	return nil
}

// The caller retains the pinned project's lifetime. Features receive the same
// repository boundary without reconstructing artifact storage paths themselves.
func retainedArtifactsForProject(project *os.Root) retainedArtifactRepository {
	return fileRetainedArtifactRepository{open: func(create bool) (*os.Root, error) { return openRetainedArtifactsWithin(project, create) }}
}
func (s *Server) openRetainedArtifacts(create bool) (*os.Root, error) {
	return openRetainedArtifactsWithin(s.root, create)
}
func openRetainedArtifactsWithin(project *os.Root, create bool) (*os.Root, error) {
	root, made, err := openOwnFolder(project, strings.Split(retainedArtifactDirectory, "/"), create, "retained artifacts")
	if err == nil && made {
		err = writePrivateData(root, ".gitignore", []byte("*\n"))
		if err != nil {
			root.Close()
		}
	}
	return root, err
}
func (f fileRetainedArtifactRepository) Get(ctx context.Context, ref retainedArtifactRef) ([]byte, error) {
	if err := f.current(ctx); err != nil {
		return nil, err
	}
	if !ref.valid() {
		return nil, errRetainedArtifactChanged
	}
	root, err := f.open(false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	raw, err := readPrivateData(root, ref.SHA256, ref.Bytes)
	if err == nil {
		err = verifyRetainedArtifact(ref, raw)
	}
	if err == nil {
		err = f.current(ctx)
	}
	if err != nil {
		return nil, err
	}
	return raw, nil
}
func (f fileRetainedArtifactRepository) Put(ctx context.Context, raw []byte) (retainedArtifactRef, error) {
	if err := f.current(ctx); err != nil {
		return retainedArtifactRef{}, err
	}
	if len(raw) > retainedArtifactByteLimit {
		return retainedArtifactRef{}, errors.New("retained artifact exceeds its storage limit")
	}
	ref := artifactReference(raw)
	root, err := f.open(true)
	if err != nil {
		return retainedArtifactRef{}, err
	}
	defer root.Close()
	lock, err := root.Open(".")
	if err != nil {
		return retainedArtifactRef{}, err
	}
	defer lock.Close()
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		return retainedArtifactRef{}, err
	}
	if f.authorize != nil {
		if err = f.current(ctx); err != nil {
			return retainedArtifactRef{}, err
		}
	}
	prior, err := readPrivateData(root, ref.SHA256, retainedArtifactByteLimit)
	if err == nil {
		if !bytes.Equal(raw, prior) {
			return retainedArtifactRef{}, errRetainedArtifactChanged
		}
		if f.authorize != nil {
			if err = f.current(ctx); err != nil {
				return retainedArtifactRef{}, err
			}
		}
		return ref, nil // exact repeated content needs no write
	}
	if !errors.Is(err, os.ErrNotExist) {
		return retainedArtifactRef{}, err
	}
	if err = f.current(ctx); err != nil {
		return retainedArtifactRef{}, err
	}
	// Directory exclusion covers the absence check and staged publication. The
	// shared primitive syncs and reads back the file before acknowledging it.
	if err = writePrivateData(root, ref.SHA256, raw); err != nil {
		return retainedArtifactRef{}, err
	}
	if f.authorize != nil {
		if err = f.current(ctx); err != nil {
			return retainedArtifactRef{}, err
		}
	}
	return ref, nil
}

// PutGuarded owns the complete queue/guard/try lifecycle. In particular it must
// never call acquire while artifact exclusion is held, or wait on artifact
// exclusion after acquiring the guard. Final contention is a refusal, not a retry.
func (f fileRetainedArtifactRepository) PutGuarded(ctx context.Context, raw []byte, acquire func(context.Context) (retainedArtifactGuard, error)) (ref retainedArtifactRef, err error) {
	// Old callback-bound owners must not silently bypass their authority. This
	// stronger path is constructed from the unbound owner and an exact guard.
	if acquire == nil || f.authorize != nil {
		return retainedArtifactRef{}, errRetainedArtifactAuthorityUnavailable
	}
	if err = ctx.Err(); err != nil {
		return retainedArtifactRef{}, err
	}
	if len(raw) > retainedArtifactByteLimit {
		return retainedArtifactRef{}, errors.New("retained artifact exceeds its storage limit")
	}
	raw = bytes.Clone(raw)
	expected := artifactReference(raw)
	root, err := f.openGuarded(ctx, acquire)
	if err != nil {
		return retainedArtifactRef{}, err
	}
	defer func() {
		err = errors.Join(err, root.Close())
		if err != nil {
			ref = retainedArtifactRef{}
		}
	}()
	if err = drainRetainedArtifactQueue(ctx, root); err != nil {
		return retainedArtifactRef{}, err
	}
	guard, err := acquireRetainedArtifactGuard(ctx, acquire)
	if err != nil {
		return retainedArtifactRef{}, err
	}
	defer func() { err = errors.Join(err, guard.Close()) }()
	lock, err := root.Open(".")
	if err != nil {
		return retainedArtifactRef{}, err
	}
	// This defer runs before guard.Close; artifact exclusion never outlives
	// host custody. Close errors prevent acknowledgment even after publication.
	defer func() { err = errors.Join(err, lock.Close()) }()
	if err = lockPrivateData(lock, true); err != nil {
		if lockHeld(err) {
			err = errors.Join(errRetainedArtifactBusy, err)
		}
		return retainedArtifactRef{}, err
	}
	if err = checkRetainedArtifactGuard(ctx, guard); err != nil {
		return retainedArtifactRef{}, err
	}
	prior, err := readPrivateData(root, expected.SHA256, retainedArtifactByteLimit)
	if err == nil {
		if !bytes.Equal(raw, prior) {
			return retainedArtifactRef{}, errRetainedArtifactChanged
		}
		if err = checkRetainedArtifactGuard(ctx, guard); err != nil {
			return retainedArtifactRef{}, err
		}
		return expected, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return retainedArtifactRef{}, err
	}
	if err = writeGuardedRetainedArtifact(ctx, root, expected.SHA256, raw, guard); err != nil {
		return retainedArtifactRef{}, err
	}
	return expected, nil
}

// Missing-directory setup may create owner bookkeeping only while guarded. It
// releases that guard before the real artifact wait; effect admission is fresh.
func (f fileRetainedArtifactRepository) openGuarded(ctx context.Context, acquire func(context.Context) (retainedArtifactGuard, error)) (root *os.Root, err error) {
	root, err = f.open(false)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return root, err
	}
	guard, err := acquireRetainedArtifactGuard(ctx, acquire)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, guard.Close())
		if err != nil && root != nil {
			err = errors.Join(err, root.Close())
			root = nil
		}
	}()
	if err = checkRetainedArtifactGuard(ctx, guard); err != nil {
		return nil, err
	}
	root, err = f.open(true)
	if err == nil {
		err = checkRetainedArtifactGuard(ctx, guard)
	}
	return root, err
}

func acquireRetainedArtifactGuard(ctx context.Context, acquire func(context.Context) (retainedArtifactGuard, error)) (retainedArtifactGuard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	guard, err := acquire(ctx)
	if err == nil && guard == nil {
		err = errRetainedArtifactAuthorityUnavailable
	}
	if err == nil {
		err = checkRetainedArtifactGuard(ctx, guard)
	}
	if err != nil {
		if guard != nil {
			err = errors.Join(err, guard.Close())
		}
		return nil, err
	}
	return guard, nil
}

func checkRetainedArtifactGuard(ctx context.Context, guard retainedArtifactGuard) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := guard.Check(ctx); err != nil {
		return err
	}
	return ctx.Err()
}

func drainRetainedArtifactQueue(ctx context.Context, root *os.Root) (err error) {
	lock, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	return waitPrivateDataLock(ctx, lock, true)
}

// Keep the immutable stage/write/fsync/rename/dirsync/readback sequence. The
// additional check occurs immediately before publication; a later error leaves
// the caller's predeclared intent uncertain and must never cause an owner retry.
func writeGuardedRetainedArtifact(ctx context.Context, root *os.Root, name string, raw []byte, guard retainedArtifactGuard) (err error) {
	file, stage, err := newDataStage(root)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, file.Close())
		}
		if removeErr := root.Remove(stage); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	if err = checkRetainedArtifactGuard(ctx, guard); err != nil {
		return err
	}
	if _, err = file.Write(raw); err != nil {
		return err
	}
	if err = file.Chmod(custodyFileMode); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	closed = true
	if err = file.Close(); err != nil {
		return err
	}
	if err = checkRetainedArtifactGuard(ctx, guard); err != nil {
		return err
	}
	if err = root.Rename(stage, name); err != nil {
		return err
	}
	if err = syncPrivateDirectory(root); err != nil {
		return err
	}
	read, err := readPrivateData(root, name, len(raw))
	if err != nil {
		return err
	}
	if !bytes.Equal(read, raw) {
		return errRetainedArtifactChanged
	}
	return checkRetainedArtifactGuard(ctx, guard)
}

// Preview/export uses this noncreating original file-owner read so every actual
// Close is checked. The original project root is borrowed under the capture
// lease, avoiding ordinary f.open(false)'s unchecked departed ancestors.
// It grants no reader-drain or removal capability.
func (f fileRetainedArtifactRepository) retentionRead(ctx context.Context, ref retainedArtifactRef, project *os.Root, o *storageBudgetOwner) (raw []byte, err error) {
	if !ref.valid() {
		return nil, errRetainedArtifactChanged
	}
	if err = f.current(ctx); err != nil {
		return nil, err
	}
	root, absent, err := storageRetentionOwnFolder(project, strings.Split(retainedArtifactDirectory, "/"), "retained artifacts", o, "retained-ancestor-close")
	if err != nil {
		return nil, err
	}
	if absent {
		return nil, os.ErrNotExist
	}
	defer func() {
		err = errors.Join(err, root.Close())
		if err != nil {
			raw = nil
		}
	}()
	raw, _, err = storageRetentionRead(root, ref.SHA256, ref.Bytes)
	if err != nil {
		return nil, err
	}
	if err = verifyRetainedArtifact(ref, raw); err != nil {
		return nil, err
	}
	return raw, f.current(ctx)
}

// The caller holds the original project lease; this adapter alone chooses the
// retained byte store. Injected repositories require their own guarded export.
func (s *Server) readRetainedArtifactForRetention(ctx context.Context, ref retainedArtifactRef, owner *storageBudgetOwner) ([]byte, error) {
	if s.retainedArtifacts != nil || s.root == nil || s.project == nil || s.project.own == nil || s.root != s.project.own.root {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	return (fileRetainedArtifactRepository{}).retentionRead(ctx, ref, s.root, owner)
}

// Exact byte readback helper also owns root/file Close errors. Generic native
// Get historically ignores root Close; new complete evidence cannot do so.
func (s *Server) readAssistantDialectCompleteNativeRaw(ctx context.Context, ref retainedArtifactRef) (raw []byte, err error) {
	if s.retainedArtifacts != nil || !ref.valid() {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	root, err := s.openRetainedArtifacts(false)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "complete-raw-root", root.Close))
		if err != nil {
			raw = nil
		}
	}()
	raw, err = readInjectedArtifactExactFile(ctx, root, ref.SHA256, ref.Bytes, "complete-read-raw")
	if err == nil {
		err = verifyRetainedArtifact(ref, raw)
	}
	return raw, err
}

// Resource ownership stays in h, including failures after an operand is opened.
// The original hold's deferred Close releases the queue, edges and root.
func (h *dialectArtifactProjectMoveHold) openRetainedForProjectMove(ctx context.Context, s *Server) (err error) {
	h.artifacts, err = s.openRetainedArtifacts(false)
	if err != nil {
		// Only the initial opener can establish the supported empty-store case.
		// Later missing edges or queue acquisition are loss of held custody.
		if injectedArtifactOnlyNotExist(err) {
			return nil
		}
		return err
	}
	artifactEdges, e := pinInjectedArtifactHeldEdges(ctx, s.root, h.artifacts, ".desk-private", "retained-artifacts")
	if e != nil {
		return e
	}
	h.edges = append(h.edges, artifactEdges...)
	h.queueRoot = h.artifacts
	h.queue, err = h.artifacts.Open(".")
	// Existing test-only fault hook acknowledges the real Open result. Do not
	// add a production context read or replace/skip the actual acquisition.
	if err == nil && testHookDialectArtifactOwnerStep != nil {
		err = testHookDialectArtifactOwnerStep("move-queue.open")
	}
	return err
}
