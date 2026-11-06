package desk

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"os"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Actual provider operations precede host custody and finish after every
// borrowed queue/metadata/work descriptor. All owned Close errors precede the
// caller's HTTP acknowledgment or normal startup. No live session is invented
// to replay a specific already-durable move transaction.
func (s *Server) withDialectArtifactProjectMoveHost(ctx context.Context, use func(*os.File, *dialectArtifactSelectedProviderPins) error) error {
	return s.withDialectArtifactProjectMoveHostOwned(ctx, func(work *os.File, pins *dialectArtifactSelectedProviderPins, _ *storageCreatorLease) error {
		return use(work, pins)
	})
}
func (s *Server) withDialectArtifactProjectMoveHostOwned(ctx context.Context, use func(*os.File, *dialectArtifactSelectedProviderPins, *storageCreatorLease) error) (err error) {
	pins, err := s.pinDialectArtifactProjectMoveProviders(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, pins.Close()) }()
	if !s.location.use.TryLock() {
		return errRetainedArtifactBusy
	}
	defer s.location.use.Unlock()
	s.location.moving.Store(true)
	defer s.location.moving.Store(false)
	move, err := s.beginOriginalStorageMove(ctx)
	if err != nil {
		return err
	}
	if move != nil {
		defer func() { err = move.phase.finish(err) }()
	}
	projectLease, err := s.lockProjectOwned(ctx, 0)
	if err != nil {
		return err
	}
	defer func() {
		if move == nil || move.lease != projectLease {
			err = errors.Join(err, projectLease.Close())
		}
	}()
	if move != nil {
		move.lease, move.phase.project = projectLease, projectLease.file
		projectLease.move = move
		if err = move.phase.acquireIdentitySigning(ctx); err != nil {
			return err
		}
	}
	work, err := s.openDialectArtifactProjectMoveWork(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if move == nil || move.phase.work == nil {
			err = errors.Join(err, closeDialectArtifactOwner(ctx, "move-work", work.Close))
		}
	}()
	if move != nil {
		if err = move.attachWork(ctx, work); err != nil {
			return err
		}
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	if err = pins.check(ctx); err != nil {
		return err
	}
	return use(work, pins, projectLease)
}

func (s *Server) openDialectArtifactProjectMoveWork(ctx context.Context) (out *os.File, err error) {
	private, err := openDialectArtifactOwnerFolder(ctx, s.root, false, "move-work-marker", ".desk-private")
	if injectedArtifactOnlyNotExist(err) {
		return s.privateDataFileLock(".data-work.lock", true)
	}
	if err != nil {
		return nil, err
	}
	raw, readErr := readDialectArtifactOwnerFile(private, dialectArtifactOwnerMarker, dialectArtifactOwnerMarkerBytes)
	closeErr := closeDialectArtifactOwner(ctx, "move-work-marker", private.Close)
	if closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	if injectedArtifactOnlyNotExist(readErr) {
		return s.privateDataFileLock(".data-work.lock", true)
	}
	if readErr != nil {
		return nil, readErr
	}
	if _, err = readDialectArtifactOwnerMarker(raw); err != nil {
		return nil, err
	}
	// Enrollment has already created and durably pinned this exact domain.
	// Losing its lock is a refusal, never permission to recreate another inode.
	return lockDialectArtifactExistingMove(s.assistant.root, ".data-work.lock")
}

func (s *Server) confirmProjectMove(ctx context.Context, request projectMoveRequest) error {
	// Lazy history and the original P/C/S/F prerequisite run before the move
	// registers its exclusive epoch. A nested ordinary retain cannot enter it.
	if s.storageConstruction != nil && s.storageConstruction.owner != nil {
		// Reject a stale confirmation before the prerequisite can publish its
		// original C/S/F. Recheck the full snapshot again under move custody.
		preview, err := s.moveSnapshotOriginal(ctx, request.Destination, nil)
		if err != nil {
			return err
		}
		if request.Token == "" || !hmac.Equal([]byte(request.Token), []byte(preview.Token)) {
			return errors.New("Project settings changed. Review the move again.")
		}
		if err := s.storageConstruction.completeOriginalIdentityFor(ctx, s); err != nil {
			return err
		}
	}
	err := s.withDialectArtifactProjectMoveHostOwned(ctx, func(work *os.File, pins *dialectArtifactSelectedProviderPins, projectLease *storageCreatorLease) error {
		preview, err := s.moveSnapshotOriginal(ctx, request.Destination, projectLease.move)
		if err == nil && (request.Token == "" || !hmac.Equal([]byte(request.Token), []byte(preview.Token))) {
			err = errors.New("Project settings changed. Review the move again.")
		}
		if err == nil {
			err = s.projectMoveIdle(ctx)
		}
		if err != nil {
			return err
		}
		parent, err := prepareProjectDestination(preview)
		if err != nil {
			return err
		}
		return s.moveProjectFolder(ctx, request.Destination, parent, work, pins, preview.retained, projectLease)
	})
	if err != nil {
		return err
	}
	return s.finishDialectArtifactProjectMove()
}

// Only the same installation journal and actual same-inode adopted project can
// authorize this pre-identity replay. Historical v1 paths never select roots.
func (s *Server) replayDialectArtifactProjectMove() error {
	if !s.assistant.usable() || s.cfg.deskID != "" {
		return nil
	}
	if err := s.replayDialectArtifactRecognizedMove(); err != nil {
		return err
	}
	intent, found, err := readProjectMove(s.assistant.root)
	if err != nil {
		return err
	}
	if found && intent.Storage != nil {
		return s.recoverOriginalStorageMove(context.Background())
	}
	if !found || intent.Retained == nil || s.projectDir != intent.Previous && s.projectDir != intent.Destination {
		return nil
	}
	if s.folderKey() != intent.Folder || !intent.Retained.binds(intent.Previous, intent.Destination, intent.Folder, s.configDir) {
		return errDialectArtifactOwner
	}
	return s.withDialectArtifactProjectMoveHostOwned(context.Background(), func(work *os.File, pins *dialectArtifactSelectedProviderPins, lease *storageCreatorLease) (err error) {
		current, exists, err := readProjectMove(s.assistant.root)
		if err != nil || !exists {
			return errors.Join(errDialectArtifactOwner, err)
		}
		a, _ := json.Marshal(intent)
		b, _ := json.Marshal(current)
		if !bytes.Equal(a, b) {
			return errDialectArtifactOwner
		}
		if lease == nil {
			return storagebudget.ErrAuthority
		}
		var lock *privateDataShortLock
		if lease.move == nil {
			lock, err = s.privateDataLock(true)
		} else {
			lock, err = lease.move.privateDataLock(context.Background(), s)
		}
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, lock.Close()) }()
		data, err := s.openChatData()
		if err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, closeDialectArtifactOwner(context.Background(), "move-replay-data", data.root.Close))
		}()
		namespace, err := assistantRunNamespace(data.root, intent.Previous)
		if err != nil || namespace != intent.Retained.Namespace {
			return errors.Join(errDialectArtifactOwner, err)
		}
		hold, err := s.holdDialectArtifactProjectMove(context.Background(), intent.Retained, work, pins, s.projectDir)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, hold.Close()) }()
		if s.projectDir == intent.Previous {
			if _, e := os.Lstat(intent.Destination); !errors.Is(e, os.ErrNotExist) {
				return errDialectArtifactOwner
			}
			if hold.location != nil && !bytes.Equal(hold.location.raw, intent.Retained.Before) {
				return errDialectArtifactOwner
			}
			return nil // old-path recovery may clear only this unchanged BEFORE state
		}
		return hold.publish(intent.Destination)
	})
}

// Retained intent removal follows all source/queue/operation Close results.
// The actual config/identity completion remains the existing project move flow.
func (s *Server) finishDialectArtifactProjectMove() error {
	if !s.assistant.usable() || s.cfg.deskID != "" {
		return nil
	}
	intent, found, err := readProjectMove(s.assistant.root)
	if err != nil {
		return err
	}
	if found && intent.Storage != nil {
		return s.finishOriginalStorageMove(context.Background(), intent)
	}
	if !found || intent.Retained == nil || s.projectDir != intent.Previous && s.projectDir != intent.Destination {
		return nil
	}
	if s.locationStatus().Problem != "" || s.startupShared() || s.startupUnresolved() {
		return errDialectArtifactOwner
	}
	if !sameDirectory(intent.Destination, s.project.info) {
		return errDialectArtifactOwner
	}
	// Existing legacy completion performs the config CAS under work/writes. The
	// new metadata extension stays pending until that completion is acknowledged.
	err = s.withDialectArtifactProjectMoveHostOwned(context.Background(), func(work *os.File, pins *dialectArtifactSelectedProviderPins, lease *storageCreatorLease) (err error) {
		if lease == nil {
			return storagebudget.ErrAuthority
		}
		var lock *privateDataShortLock
		if lease.move == nil {
			lock, err = s.privateDataLock(true)
		} else {
			lock, err = lease.move.privateDataLock(context.Background(), s)
		}
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, lock.Close()) }()
		data, err := s.openChatData()
		if err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, closeDialectArtifactOwner(context.Background(), "move-finish-data", data.root.Close))
		}()
		namespace, err := assistantRunNamespace(data.root, intent.Previous)
		if err != nil || namespace != intent.Retained.Namespace {
			return errors.Join(errDialectArtifactOwner, err)
		}
		hold, err := s.holdDialectArtifactProjectMove(context.Background(), intent.Retained, work, pins, intent.Destination)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, hold.Close()) }()
		if err = hold.publish(intent.Destination); err != nil {
			return err
		}
		if err = hold.Close(); err != nil {
			return err
		}
		private, err := s.openIdentityFolder()
		if err != nil {
			return err
		}
		record, _, ok, readErr := readIdentity(private)
		closeErr := closeDialectArtifactOwner(context.Background(), "move-finish-identity", private.Close)
		if readErr != nil || closeErr != nil || !ok || record.ID != intent.Identity || record.Folder != intent.Folder || record.Path != intent.Destination {
			return errors.Join(errDialectArtifactOwner, readErr, closeErr)
		}
		private, err = s.openIdentityFolder()
		if err != nil {
			return err
		}
		ackErr := s.acknowledgeDialectArtifactMoveIdentity(private, record)
		closeErr = closeDialectArtifactOwner(context.Background(), "move-finish-identity-ack", private.Close)
		if ackErr != nil || closeErr != nil {
			return errors.Join(ackErr, closeErr)
		}
		if err = s.finishProjectMoveConfig(intent); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	raw, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	return s.removeDialectArtifactMoveIntent(projectMoveName, raw)
}

func (s *Server) removeDialectArtifactMoveIntent(name string, expected []byte) (err error) {
	file, err := openDialectArtifactOwnerFile(s.assistant.root, name)
	if err != nil {
		return err
	}
	info, statErr := file.Stat()
	raw, readErr := readBounded(file, 16<<10)
	closeErr := closeDialectArtifactOwner(context.Background(), "move-intent-file", file.Close)
	if statErr != nil || readErr != nil || closeErr != nil || !bytes.Equal(raw, expected) {
		return errors.Join(errDialectArtifactOwner, statErr, readErr, closeErr)
	}
	if err = dialectArtifactOwnerStep(context.Background(), "move-intent.before-remove"); err != nil {
		return err
	}
	current, err := readDialectArtifactOwnerFile(s.assistant.root, name, 16<<10)
	if err != nil || !bytes.Equal(current, expected) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	settings, err := s.assistant.root.Stat(".")
	if err != nil || !sameDirectory(s.configDir, settings) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	again, err := s.assistant.root.Lstat(name)
	if err != nil || !os.SameFile(info, again) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	if err = s.assistant.root.Remove(name); err != nil {
		return err
	}
	return syncPrivateDirectory(s.assistant.root)
}

func (s *Server) acknowledgeDialectArtifactMoveIdentity(private *os.Root, record identityRecord) error {
	ctx := context.Background()
	if err := syncExistingDialectArtifactOwner(ctx, private, startupIdentityName, record.line(), "move-identity"); err != nil {
		return err
	}
	return acknowledgeDialectArtifactOwner(ctx, private, startupIdentityName, record.line(), "move-identity", func() error {
		if !sameDirectory(record.Path, s.project.info) {
			return errDialectArtifactOwner
		}
		return nil
	})
}
