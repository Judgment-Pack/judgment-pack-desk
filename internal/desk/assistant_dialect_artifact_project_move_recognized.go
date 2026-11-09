package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
)

const dialectArtifactRecognizedMoveName = "retained-project-move.json"

// This journal describes an already-recognized same-inode external rename. It
// never authorizes a different inode or changes the saved startup preference.
// The source identity's exact original bytes bind previous path and folder.
type dialectArtifactRecognizedMove struct {
	Version        int                            `json:"version"`
	Destination    string                         `json:"destination"`
	BeforeIdentity []byte                         `json:"beforeIdentity"`
	Retained       dialectArtifactProjectMovePlan `json:"retained"`
}

func readDialectArtifactRecognizedMove(root *os.Root) (out dialectArtifactRecognizedMove, found bool, err error) {
	raw, err := readDialectArtifactOwnerFile(root, dialectArtifactRecognizedMoveName, 16<<10)
	if injectedArtifactOnlyNotExist(err) {
		return out, false, nil
	}
	if err != nil {
		return out, true, err
	}
	err = readDialectArtifactOwnerJSON(raw, 16<<10, &out)
	if err != nil {
		return out, true, err
	}
	record, err := parseIdentity(out.BeforeIdentity)
	after := record
	after.Path = out.Destination
	if err != nil || out.Version != 1 || record.Folder == "" || out.Destination == record.Path ||
		!dialectArtifactOwnerPath([]byte(out.Destination)) || len(after.line()) > startupIdentityLimit || !out.Retained.valid() {
		return out, true, errDialectArtifactOwner
	}
	return out, true, nil
}

// Called before the old unretained identity/history flow takes project custody.
// A real pending confirmed-move intent has already replayed at the pre-identity
// gate and continues through that existing history flow instead of creating a
// second transaction. No marker means ordinary legacy behavior remains intact.
func (s *Server) retainDialectArtifactRecognizedMove(private *os.Root, record identityRecord, info os.FileInfo) (moved identityRecord, written os.FileInfo, handled bool, err error) {
	if pending, found, e := readProjectMove(s.assistant.root); e != nil {
		return record, info, true, e
	} else if found && pending.Retained != nil && pending.Previous == record.Path && pending.Destination == s.projectDir && pending.Folder == record.Folder {
		return record, info, false, nil
	}
	marker, e := readDialectArtifactOwnerFile(private, dialectArtifactOwnerMarker, dialectArtifactOwnerMarkerBytes)
	if injectedArtifactOnlyNotExist(e) {
		return record, info, false, nil
	}
	if e != nil {
		return record, info, true, e
	}
	if _, e = readDialectArtifactOwnerMarker(marker); e != nil {
		return record, info, true, e
	}
	var journal []byte
	moved = record
	moved.Path = s.projectDir
	err = s.withDialectArtifactProjectMoveHost(context.Background(), func(work *os.File, pins *dialectArtifactSelectedProviderPins) (err error) {
		current, currentInfo, found, readErr := readIdentity(private)
		holds, lookErr := s.folderHoldsIdentity(record.Path, record.ID)
		if readErr != nil || !found || current != record || !os.SameFile(info, currentInfo) || lookErr != nil || holds || s.folderKey() != record.Folder {
			return errors.Join(errDialectArtifactOwner, readErr, lookErr)
		}
		if len(moved.line()) > startupIdentityLimit {
			return errIdentityTooLong
		}
		if _, found, e := readDialectArtifactRecognizedMove(s.assistant.root); e != nil || found {
			return errors.Join(errDialectArtifactOwner, e)
		}
		lock, err := s.privateDataLock(true)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, lock.Close()) }()
		data, err := s.openChatData()
		if err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, closeDialectArtifactOwner(context.Background(), "recognized-move-data", data.root.Close))
		}()
		if _, _, _, err = movedHistoryBinding(data.root, record.Path, s.projectDir); err != nil {
			return err
		}
		plan, err := s.dialectArtifactProjectMoveSnapshot(context.Background(), record.Path, s.projectDir, data)
		if err != nil || plan == nil {
			return errors.Join(errDialectArtifactOwner, err)
		}
		hold, err := s.holdDialectArtifactProjectMove(context.Background(), plan, work, pins, s.projectDir)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, hold.Close()) }()
		journal, err = json.Marshal(dialectArtifactRecognizedMove{Version: 1, Destination: s.projectDir, BeforeIdentity: record.line(), Retained: *plan})
		if err != nil {
			return err
		}
		if len(journal) > 16<<10 {
			return errDialectArtifactOwner
		} // bounded by original <=512-byte identities
		if err = hold.check(); err != nil {
			return err
		}
		if err = claimPrivateData(s.assistant.root, dialectArtifactRecognizedMoveName, journal); err != nil {
			return err
		}
		if err = hold.publish(s.projectDir); err != nil {
			return err
		}
		if err = hold.Close(); err != nil {
			return err
		}
		if err = linkDialectArtifactMovedHistory(data.root, record.Path, s.projectDir, plan); err != nil {
			return err
		}
		written, err = writeIdentity(private, moved, info)
		if err != nil {
			return err
		}
		return s.acknowledgeDialectArtifactMoveIdentity(private, moved)
	})
	if err == nil {
		err = s.removeDialectArtifactMoveIntent(dialectArtifactRecognizedMoveName, journal)
	}
	if err != nil {
		return record, info, true, err
	}
	return moved, written, true, nil
}

func (s *Server) replayDialectArtifactRecognizedMove() error {
	intent, found, err := readDialectArtifactRecognizedMove(s.assistant.root)
	if err != nil || !found {
		return err
	}
	if s.projectDir != intent.Destination {
		return nil
	}
	before, _ := parseIdentity(intent.BeforeIdentity)
	after := before
	after.Path = intent.Destination
	if s.folderKey() != before.Folder || !intent.Retained.binds(before.Path, after.Path, before.Folder, s.configDir) {
		return errDialectArtifactOwner
	}
	if _, other, e := readProjectMove(s.assistant.root); e != nil || other {
		return errors.Join(errDialectArtifactOwner, e)
	}
	original, _ := json.Marshal(intent)
	err = s.withDialectArtifactProjectMoveHost(context.Background(), func(work *os.File, pins *dialectArtifactSelectedProviderPins) (err error) {
		currentIntent, exists, e := readDialectArtifactRecognizedMove(s.assistant.root)
		currentRaw, _ := json.Marshal(currentIntent)
		if e != nil || !exists || !bytes.Equal(currentRaw, original) {
			return errors.Join(errDialectArtifactOwner, e)
		}
		private, err := s.openIdentityFolder()
		if err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, closeDialectArtifactOwner(context.Background(), "recognized-replay-private", private.Close))
		}()
		record, info, found, err := readIdentity(private)
		if err != nil || !found || record != before && record != after {
			return errors.Join(errDialectArtifactOwner, err)
		}
		holds, e := s.folderHoldsIdentity(before.Path, before.ID)
		if e != nil || holds {
			return errors.Join(errDialectArtifactOwner, e)
		}
		lock, err := s.privateDataLock(true)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, lock.Close()) }()
		data, err := s.openChatData()
		if err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, closeDialectArtifactOwner(context.Background(), "recognized-replay-data", data.root.Close))
		}()
		namespace, e := assistantRunNamespace(data.root, before.Path)
		if e != nil || namespace != intent.Retained.Namespace {
			return errors.Join(errDialectArtifactOwner, e)
		}
		hold, err := s.holdDialectArtifactProjectMove(context.Background(), &intent.Retained, work, pins, s.projectDir)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, hold.Close()) }()
		if err = hold.publish(s.projectDir); err != nil {
			return err
		}
		if err = hold.Close(); err != nil {
			return err
		}
		if err = linkDialectArtifactMovedHistory(data.root, before.Path, after.Path, &intent.Retained); err != nil {
			return err
		}
		if record == before {
			_, err = writeIdentity(private, after, info)
		}
		if err != nil {
			return err
		}
		return s.acknowledgeDialectArtifactMoveIdentity(private, after)
	})
	if err != nil {
		return err
	}
	return s.removeDialectArtifactMoveIntent(dialectArtifactRecognizedMoveName, original)
}

func (s *Server) requireDialectArtifactRecognizedMoveSettled() error {
	if s.retainedRecognizedMoveStartupErr != nil {
		return s.retainedRecognizedMoveStartupErr
	}
	if !s.assistant.usable() || s.cfg.deskID != "" {
		return nil
	}
	intent, found, err := readDialectArtifactRecognizedMove(s.assistant.root)
	if err != nil {
		return err
	}
	if found && intent.Destination == s.projectDir {
		return errors.New("Retained project move is incomplete. Restart Desk to recover its exact saved transition.")
	}
	return nil
}
