package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
)

// The interface is a consumer boundary, not an admission capability. Every
// concrete implementation is created inside the original selected-source or
// destination lifetime; generic repositories/guards never qualify as owners.
type retainedArtifactTransferView interface {
	GetExact(context.Context, retainedArtifactRef) ([]byte, error)
	PublishExact(context.Context, retainedArtifactRef, []byte) error
	Close() error
}

type dialectArtifactSelectedSlotKey struct{ ledger, run, host, stage string }

type dialectArtifactSelectedSlot struct {
	key          dialectArtifactSelectedSlotKey
	scope        dialectArtifactOwnerScope
	ref          retainedArtifactRef
	acknowledged bool
	missing      bool
}

type dialectArtifactSelectedHistoricalSource struct {
	run   assistantRunRecord
	host  string
	stage string
}

type dialectArtifactSelectedStorageFile struct {
	name string
	raw  []byte
	info os.FileInfo
}

type dialectArtifactSelectedStorageSnapshot struct {
	location  dataLocation
	revision  string
	info      os.FileInfo
	namespace string
	files     []dialectArtifactSelectedStorageFile
	slots     []dialectArtifactSelectedSlot
}

type dialectArtifactSelectedSourceScope struct {
	owner    *dialectArtifactSelectedMetadataRow
	workRow  *dialectArtifactSelectedMetadataRow
	scopeRow *dialectArtifactSelectedMetadataRow
	scope    dialectArtifactOwnerScope
	work     *dialectArtifactSelectedWorkDomain
	native   *dialectArtifactSelectedNativeSource
	injected *dialectArtifactSelectedProviderPin
	binding  retainedArtifactInjectedBinding
}

type dialectArtifactSelectedSources struct {
	self       *dialectArtifactSelectedSources
	s          *Server
	request    *http.Request
	identity   assistantRunBinding
	initial    dialectArtifactSelectedStorageSnapshot
	metadata   *dialectArtifactSelectedMetadata
	works      *dialectArtifactSelectedWorkDomains
	providers  *dialectArtifactSelectedProviderPins
	scopes     map[dialectArtifactOwnerScope]*dialectArtifactSelectedSourceScope
	slots      map[dialectArtifactSelectedSlotKey]dialectArtifactSelectedSlot
	natives    []*dialectArtifactSelectedNativeSource
	authority  *dialectArtifactInjectedAuthorityHold
	viewActive bool
	view       retainedArtifactTransferView
	entered    bool
	active     bool
	closed     bool
}

// The current FD belongs to the original outer host transfer seam. That seam
// may establish its own current lock inode using ordinary authorized custody,
// but must not flock it before this sorted all-domain acquisition. No selected
// foreign lock/root is ever created, and this function never closes current.
// No production route calls this inactive integration foundation.
func (s *Server) withSelectedDialectArtifactSources(r *http.Request, current *os.File, use func(*dialectArtifactSelectedSources) error) error {
	return s.withSelectedDialectArtifactSourceSelection(r, current, nil, use)
}

// Historical inspection first uses the existing lookup's historicalRead scope:
// same current principal/project and terminal/paused retained state. It does not
// require the original session/Policy or a live factory/preparation owner. The
// selected callback still needs the downstream pure saved-read/lineage validator
// before any public inspection result can be acknowledged.
func (s *Server) withSelectedDialectArtifactRunSources(r *http.Request, current *os.File, id, host, stage string, use func(*dialectArtifactSelectedSources) error) error {
	if s == nil || r == nil || !assistantRunText(id, 128) || !assistantRunText(host, 128) || !contains([]string{"candidate", "checked", "final"}, stage) {
		return errAssistantRunScope
	}
	if err := s.authorizeAssistantRunStorage(r); err != nil {
		return err
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		return err
	}
	run, err := s.assistantRuns().lookup(r.Context(), identity, id, "", true)
	if err != nil {
		return err
	}
	if run.Binding.Owner.Kind != "chat" || !assistantRunTerminal(run.State) && !assistantRunPaused(run.State) {
		return errAssistantRunScope
	}
	held := assistantDialectDraftOf(&run, host)
	if held == nil || assistantDialectDraftSlotOf(held, stage).Intent == nil {
		return errAssistantRunScope
	}
	if _, _, err := assistantDialectDraftRoot(run, host); err != nil {
		return err
	}
	return s.withSelectedDialectArtifactSourceSelection(r, current, &dialectArtifactSelectedHistoricalSource{run: run, host: host, stage: stage}, use)
}

func (s *Server) withSelectedDialectArtifactSourceSelection(r *http.Request, current *os.File, target *dialectArtifactSelectedHistoricalSource, use func(*dialectArtifactSelectedSources) error) (err error) {
	if s == nil || r == nil || current == nil || use == nil || s.project == nil || s.project.own == nil ||
		s.root != s.project.own.root || s.assistant == nil || !s.assistant.usable() || s.writes == nil {
		return errRetainedArtifactAuthorityUnavailable
	}
	ctx := r.Context()
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		return err
	}
	selected := &dialectArtifactSelectedSources{s: s, request: r, identity: identity,
		scopes: make(map[dialectArtifactOwnerScope]*dialectArtifactSelectedSourceScope), slots: make(map[dialectArtifactSelectedSlotKey]dialectArtifactSelectedSlot)}
	selected.self = selected
	defer func() {
		if !selected.entered && injectedArtifactOnlyNotExist(err) {
			err = errors.Join(errDialectArtifactOwner, err)
		}
	}()
	full := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.authorizeAssistantRunStorage(r); err != nil {
			return err
		}
		currentIdentity, _, identityErr := s.assistantRunIdentity(r)
		if identityErr != nil || !assistantRunIdentityMatches(currentIdentity, selected.identity) || !sameDirectory(s.projectDir, s.project.info) {
			return errors.Join(errAssistantRunScope, identityErr)
		}
		return nil
	}
	if err = full(); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, full()) }()
	transfer, err := s.openPrivateDataFileLock(ctx, ".data-transfer.lock", true, false)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-transfer", transfer.Close)) }()
	selected.initial, err = s.snapshotDialectArtifactSelectedStorage(r, target)
	if err != nil {
		return err
	}
	// Exact flat bytes plus every closure row are measured before owner lookup.
	// Missing rows do not consume declared object bytes or an object-table row.
	if err = selected.initial.minimumArchive(); err != nil {
		return err
	}
	defer func() {
		selected.active = false
		selected.closed = true
		for i := len(selected.natives) - 1; i >= 0; i-- {
			err = errors.Join(err, selected.natives[i].Close())
		}
		if selected.works != nil {
			err = errors.Join(err, selected.works.Close())
		}
		if selected.metadata != nil {
			err = errors.Join(err, selected.metadata.Close())
		}
		if selected.providers != nil {
			err = errors.Join(err, selected.providers.Close())
		}
	}()
	if len(selected.initial.slots) != 0 {
		selected.metadata, err = s.openDialectArtifactSelectedMetadata(ctx)
		if err != nil {
			return err
		}
	}
	ownerRows := make(map[string]*dialectArtifactSelectedMetadataRow)
	locationRows := make(map[string]*dialectArtifactSelectedMetadataRow)
	workRows := make(map[string]*dialectArtifactSelectedMetadataRow)
	workRecords := []dialectArtifactWorkDomainRecord{}
	providerIDs := []string{}
	for _, slot := range selected.initial.slots {
		if !assistantRunDigest(slot.scope.Owner) {
			return errRetainedArtifactAuthorityUnavailable
		}
		selected.slots[slot.key] = slot
		if selected.scopes[slot.scope] != nil {
			continue
		}
		ownerRow := ownerRows[slot.scope.Owner]
		if ownerRow == nil {
			ownerRow, err = selected.metadata.ownerRow(ctx, slot.scope.Owner)
			if err != nil {
				return err
			}
			ownerRows[slot.scope.Owner] = ownerRow
		}
		scopeRow, scopeErr := selected.metadata.scopeRow(ctx, slot.scope)
		if scopeErr != nil {
			return scopeErr
		}
		source := &dialectArtifactSelectedSourceScope{owner: ownerRow, scopeRow: scopeRow, scope: slot.scope}
		workID := ""
		if native, nativeErr := readDialectArtifactOwnerRecord(ownerRow.raw); nativeErr == nil {
			workID = native.Work.ID
			if locationRows[native.Owner] == nil {
				locationRows[native.Owner], err = selected.metadata.locationRow(ctx, ownerRow)
				if err != nil {
					return err
				}
			}
		} else {
			injected, injectedErr := readDialectArtifactInjectedOwnerRecord(ownerRow.raw)
			if injectedErr != nil {
				return injectedErr
			}
			workID = injected.WorkDomain
			providerIDs = append(providerIDs, injected.Provider)
			source.binding = retainedArtifactInjectedBinding{Version: 1, Provider: injected.Provider, Owner: injected.Owner,
				ProjectFolderIdentity: injected.ProjectFolderIdentity, WorkDomain: workID, Namespace: slot.scope.Namespace}
		}
		if workRows[workID] == nil {
			workRows[workID], err = selected.metadata.workRow(ctx, workID)
			if err != nil {
				return err
			}
			record, readErr := readDialectArtifactWorkDomainRecord(workRows[workID].raw)
			if readErr != nil {
				return readErr
			}
			workRecords = append(workRecords, record)
		}
		source.workRow = workRows[workID]
		selected.scopes[slot.scope] = source
	}
	// Pin exact trusted registrations before any outer host/EX-work custody.
	selected.providers, err = pinDialectArtifactSelectedProviders(ctx, s.deskOwner(), providerIDs)
	if err != nil {
		return err
	}
	currentRecord, currentErr := s.dialectArtifactSelectedCurrentWorkRecord(ctx, current)
	if currentErr != nil {
		return currentErr
	}
	workRecords = append(workRecords, currentRecord)
	selected.works, err = openDialectArtifactSelectedWorkDomains(ctx, workRecords)
	if err != nil {
		return err
	}
	nativeByOwner := make(map[string]*dialectArtifactSelectedNativeSource)
	for _, slot := range selected.initial.slots {
		source := selected.scopes[slot.scope]
		workRecord, readErr := readDialectArtifactWorkDomainRecord(source.workRow.raw)
		if readErr != nil {
			return readErr
		}
		source.work = selected.works.byID[workRecord.ID]
		if source.binding.Provider != "" {
			source.injected = selected.providers.byID[source.binding.Provider]
			continue
		}
		if nativeByOwner[slot.scope.Owner] == nil {
			native, openErr := openDialectArtifactSelectedNativeSource(ctx, source.owner, locationRows[slot.scope.Owner], source.work)
			if openErr != nil {
				return openErr
			}
			nativeByOwner[slot.scope.Owner] = native
			selected.natives = append(selected.natives, native)
		}
		source.native = nativeByOwner[slot.scope.Owner]
	}
	_, err = s.withDialectArtifactOwnerOuter(ctx, full, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !sameDirectory(s.projectDir, s.project.info) {
			return errDialectArtifactOwner
		}
		return nil
	}, func(_ *Server, _ *dialectArtifactHomeHold, outerCheck, outerLeaf func() error) (out dialectArtifactOwnerEnrollment, err error) {
		if err = outerCheck(); err != nil {
			return out, err
		}
		// End all selected EX-work descriptors before the original outer home
		// and location holds end. Later cleanup still owns source roots/home
		// leases/metadata and finishes provider operations last.
		defer func() { err = errors.Join(err, selected.works.Close()) }()
		if err = selected.works.acquire(ctx); err != nil {
			return out, err
		}
		if !s.writes.TryLock() {
			return out, errRetainedArtifactBusy
		}
		defer s.writes.Unlock()
		private, lockErr := s.tryPrivateDataLockContext(ctx, true)
		if lockErr != nil {
			return out, lockErr
		}
		defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-private", private.Close)) }()
		data, openErr := s.openChatData()
		if openErr != nil {
			return out, openErr
		}
		defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-data", data.root.Close)) }()
		fresh, snapshotErr := s.snapshotDialectArtifactSelectedStorageSelectionLocked(r, data, target)
		if snapshotErr != nil {
			return out, snapshotErr
		}
		if !selected.initial.same(fresh) {
			return out, errDialectArtifactOwner
		}
		if err = outerCheck(); err != nil {
			return out, err
		}
		if err = selected.checkPhysical(ctx); err != nil {
			return out, err
		}
		for scope, source := range selected.scopes {
			if source.native != nil {
				if err = source.native.namespace(ctx, data.root, scope); err != nil {
					return out, err
				}
			}
		}
		// Only this original fully revalidated lexical continuation can admit
		// object views. Neither source records nor pin collections can set it.
		selected.active = true
		selected.entered = true
		defer func() {
			err = errors.Join(err, selected.closeActive())
			selected.active = false
			err = errors.Join(err, selected.checkPhysical(ctx), outerLeaf(), full())
		}()
		err = use(selected)
		if err == nil {
			after, afterErr := s.snapshotDialectArtifactSelectedStorageSelectionLocked(r, data, target)
			if afterErr != nil {
				err = afterErr
			} else if !selected.initial.same(after) {
				err = errDialectArtifactOwner
			}
		}
		return out, err
	})
	return err
}

// Derive only the actual current host domain. The supplied original descriptor
// is borrowed as an inode witness; selected acquisition opens and owns its own
// noncreating descriptor to the same inode. The witness is never flocked or
// closed here, so its outer owner can establish first-use compatibility without
// transferring ownership or leaving EX-work beyond selected-source cleanup.
func (s *Server) dialectArtifactSelectedCurrentWorkRecord(ctx context.Context, file *os.File) (dialectArtifactWorkDomainRecord, error) {
	var out dialectArtifactWorkDomainRecord
	if ctx == nil || ctx.Err() != nil || s == nil || s.assistant == nil || !s.assistant.usable() || file == nil {
		return out, errDialectArtifactOwner
	}
	folder, statErr := s.assistant.root.Stat(".")
	held, heldErr := file.Stat()
	entry, lookErr := s.assistant.root.Lstat(".data-work.lock")
	if statErr != nil || heldErr != nil || lookErr != nil || !os.SameFile(held, entry) || !sameDirectory(s.configDir, folder) ||
		ownerOnlyFile("current source work", held.Mode()) != nil || ownerOnlyFile("current source work", entry.Mode()) != nil ||
		ownedByUs("current source work", held) != nil || ownedByUs("current source work", entry) != nil {
		return out, errors.Join(errDialectArtifactOwner, statErr, heldErr, lookErr)
	}
	out = dialectArtifactWorkDomainRecord{Version: 1, Kind: "settings", PathBytes: []byte(s.configDir), FolderIdentity: identityKey(folder), LockIdentity: identityKey(held)}
	out.ID = dialectArtifactWorkDomainID(out.FolderIdentity, out.LockIdentity)
	if !out.valid() {
		return dialectArtifactWorkDomainRecord{}, errDialectArtifactOwner
	}
	return out, nil
}

func (s *Server) snapshotDialectArtifactSelectedStorage(r *http.Request, target *dialectArtifactSelectedHistoricalSource) (_ dialectArtifactSelectedStorageSnapshot, err error) {
	if !s.writes.TryLock() {
		return dialectArtifactSelectedStorageSnapshot{}, errRetainedArtifactBusy
	}
	defer s.writes.Unlock()
	private, err := s.tryPrivateDataLockContext(r.Context(), true)
	if err != nil {
		return dialectArtifactSelectedStorageSnapshot{}, err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(r.Context(), "selected-discovery-private", private.Close))
	}()
	data, err := s.openChatData()
	if err != nil {
		return dialectArtifactSelectedStorageSnapshot{}, err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(r.Context(), "selected-discovery-data", data.root.Close))
	}()
	return s.snapshotDialectArtifactSelectedStorageSelectionLocked(r, data, target)
}

func (s *Server) snapshotDialectArtifactSelectedStorageSelectionLocked(r *http.Request, data *chatDataStore, target *dialectArtifactSelectedHistoricalSource) (out dialectArtifactSelectedStorageSnapshot, err error) {
	if err = validateStoredBindings(data.root); err != nil {
		return out, err
	}
	if err = checkPrivateDataPath(data); err != nil {
		return out, err
	}
	out.location, out.revision = data.location, data.revision
	out.info, err = data.root.Stat(".")
	if err != nil {
		return out, err
	}
	store := s.assistantRunsLocked(data.root)
	if store.problem != nil {
		return out, store.problem
	}
	out.namespace = store.project
	entries, err := storageEntries(data.root)
	if err != nil {
		return out, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	total := int64(0)
	for _, entry := range entries {
		name := entry.Name()
		if target != nil && name != store.name {
			continue
		}
		if !chatStorageFile(name) {
			if data.legacy || name == dataMarkerName || strings.HasPrefix(name, ".data-") || strings.HasPrefix(name, configStagingPrefix) {
				continue
			}
			return out, errDialectArtifactOwner
		}
		if err = r.Context().Err(); err != nil {
			return out, err
		}
		file, openErr := openDialectArtifactOwnerFile(data.root, name)
		if openErr != nil {
			return out, openErr
		}
		info, statErr := file.Stat()
		raw, readErr := readBounded(file, storageFileLimit(name))
		closeErr := closeDialectArtifactOwner(r.Context(), "selected-storage-file", file.Close)
		if err = errors.Join(statErr, readErr, closeErr); err != nil {
			return out, err
		}
		if err = validateStorageFile(name, raw); err != nil {
			return out, err
		}
		if err = s.authorizeDialectArtifactSelectedStorageFile(r, name, raw); err != nil {
			return out, err
		}
		total += int64(len(raw))
		if total > maxBackupData || len(out.files)+2 > maxStorageFiles {
			return out, withCode(CodeTooLarge, errDialectArtifactOwner)
		}
		out.files = append(out.files, dialectArtifactSelectedStorageFile{name: name, raw: raw, info: info})
		if !assistantRunFileName.MatchString(name) {
			continue
		}
		ledger, decodeErr := decodeAssistantRunStorage(name, raw)
		if decodeErr != nil {
			return out, decodeErr
		}
		for _, run := range ledger.Runs {
			if target != nil {
				if run.ID != target.run.ID {
					continue
				}
				if !reflect.DeepEqual(run, target.run) || run.Binding.Principal != target.run.Binding.Principal || run.Binding.Project != store.project {
					return out, errAssistantRunScope
				}
			}
			scope := dialectArtifactOwnerScope{Version: 1, Kind: "native", Owner: run.DialectArtifactOwner, Namespace: ledger.Project}
			if run.DialectArtifactImport != nil {
				scope.Kind, scope.RunID = "import", run.ID
			}
			for _, held := range run.DialectDrafts {
				if target != nil && held.HostOperationID != target.host {
					continue
				}
				for stageIndex, stage := range []string{"candidate", "checked", "final"} {
					if target != nil && stageIndex > map[string]int{"candidate": 0, "checked": 1, "final": 2}[target.stage] {
						continue
					}
					slot := assistantDialectDraftSlotOf(&held, stage)
					if slot.Intent == nil {
						continue
					}
					availability, availabilityErr := assistantDialectArtifactImportedAvailability(run, held.HostOperationID, stage)
					if availabilityErr != nil {
						return out, availabilityErr
					}
					out.slots = append(out.slots, dialectArtifactSelectedSlot{key: dialectArtifactSelectedSlotKey{name, run.ID, held.HostOperationID, stage}, scope: scope,
						ref: slot.Intent.Reference, acknowledged: slot.Receipt != nil, missing: availability == "missing"})
				}
			}
		}
	}
	if target != nil {
		found := false
		for _, slot := range out.slots {
			if slot.key.run == target.run.ID && slot.key.host == target.host && slot.key.stage == target.stage {
				found = true
				break
			}
		}
		if !found {
			return out, errAssistantRunScope
		}
	}
	return out, nil
}

func (s *Server) authorizeDialectArtifactSelectedStorageFile(r *http.Request, name string, raw []byte) error {
	if assistantSourceFileName.MatchString(name) {
		return s.authorizeAssistantSourceStorage(r)
	}
	if assistantRunFileName.MatchString(name) {
		return s.authorizeAssistantRunStorage(r)
	}
	if privateWorkspaceStorageFile(name) {
		return s.authorizePrivateWorkspace(r)
	}
	if conversationFileName.MatchString(name) {
		return s.authorizePolicyConversations(r, raw)
	}
	if draftPackFileName.MatchString(name) {
		return s.authorizePolicyWorkspace(r, raw, true)
	}
	return nil
}

func (s dialectArtifactSelectedStorageSnapshot) minimumArchive() error {
	type closure struct {
		Ledger       string `json:"ledger"`
		RunID        string `json:"runId"`
		Host         string `json:"hostOperationId"`
		Stage        string `json:"stage"`
		Availability string `json:"availability"`
	}
	manifest := struct {
		Version int           `json:"version"`
		Created string        `json:"createdAt"`
		Files   []backupEntry `json:"files"`
		Objects []backupEntry `json:"objects"`
		Closure []closure     `json:"dialectClosure"`
	}{2, "2000-01-01T00:00:00Z", []backupEntry{}, []backupEntry{}, []closure{}}
	total := int64(0)
	for _, file := range s.files {
		total += int64(len(file.raw))
		manifest.Files = append(manifest.Files, backupEntry{Name: file.name, Bytes: int64(len(file.raw)), SHA256: digestOf(file.raw)})
	}
	if total > maxBackupData || len(s.files)+1 > maxStorageFiles {
		return withCode(CodeTooLarge, errDialectArtifactOwner)
	}
	for _, slot := range s.slots {
		manifest.Closure = append(manifest.Closure, closure{slot.key.ledger, slot.key.run, slot.key.host, slot.key.stage, "missing"})
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if len(raw) > maxBackupManifest {
		return withCode(CodeTooLarge, errDialectArtifactOwner)
	}
	return nil
}

func (s dialectArtifactSelectedStorageSnapshot) same(other dialectArtifactSelectedStorageSnapshot) bool {
	if s.location != other.location || s.revision != other.revision || s.namespace != other.namespace || !os.SameFile(s.info, other.info) ||
		len(s.files) != len(other.files) || !reflect.DeepEqual(s.slots, other.slots) {
		return false
	}
	for i, file := range s.files {
		next := other.files[i]
		if file.name != next.name || !os.SameFile(file.info, next.info) || !bytes.Equal(file.raw, next.raw) {
			return false
		}
	}
	return true
}

func (s *dialectArtifactSelectedSources) checkPhysical(ctx context.Context) error {
	if s == nil || s.self != s || s.closed || ctx == nil || s.works == nil || s.providers == nil {
		return errRetainedArtifactAuthorityUnavailable
	}
	if err := s.works.check(ctx); err != nil {
		return err
	}
	if err := s.providers.check(ctx); err != nil {
		return err
	}
	if s.metadata != nil {
		for _, row := range s.metadata.rows {
			if err := row.check(ctx); err != nil {
				return err
			}
		}
	}
	for _, native := range s.natives {
		if err := native.check(ctx, true); err != nil {
			return err
		}
	}
	for _, source := range s.scopes {
		if source.injected != nil {
			if err := source.checkProviderScope(ctx); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

func (s *dialectArtifactSelectedSources) checkSource(ctx context.Context, scope dialectArtifactOwnerScope) error {
	if s == nil || s.self != s || !s.active || s.closed || ctx == nil {
		return errRetainedArtifactAuthorityUnavailable
	}
	source := s.scopes[scope]
	if source == nil || source.scope != scope || source.work == nil {
		return errRetainedArtifactAuthorityUnavailable
	}
	if err := source.owner.check(ctx); err != nil {
		return err
	}
	if err := source.scopeRow.check(ctx); err != nil {
		return err
	}
	if err := source.workRow.check(ctx); err != nil {
		return err
	}
	if err := source.work.check(ctx, true); err != nil {
		return err
	}
	if source.native != nil {
		return source.native.check(ctx, true)
	}
	if source.injected == nil || source.binding.Owner != scope.Owner || source.binding.Namespace != scope.Namespace {
		return errRetainedArtifactAuthorityUnavailable
	}
	return source.checkProviderScope(ctx)
}

func (source *dialectArtifactSelectedSourceScope) checkProviderScope(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			err = errors.Join(errDialectArtifactOwner, err)
		}
	}()
	if source == nil || source.injected == nil || !source.binding.valid() {
		return errRetainedArtifactAuthorityUnavailable
	}
	if err := source.injected.check(ctx); err != nil {
		return err
	}
	b := source.binding
	root := source.injected.registration.root
	if err := readInjectedArtifactRow(ctx, root, source.owner.raw, b.Owner[2:]+".json", "d5-owners", b.Owner[:2]); err != nil {
		return err
	}
	if source.scope.Kind == "native" {
		raw, err := json.Marshal(b)
		if err != nil {
			return err
		}
		return readInjectedArtifactRow(ctx, root, raw, b.Owner+".json", "d5-native", b.Namespace)
	}
	if source.scope.Kind != "import" || !source.scope.valid() {
		return errDialectArtifactOwner
	}
	return readInjectedArtifactRow(ctx, root, source.scopeRow.raw, b.Owner+".json", "d5-imports", b.Namespace, source.scope.RunID)
}

func (s *dialectArtifactSelectedSources) closeActive() (err error) {
	if s.view != nil {
		err = errors.Join(err, s.view.Close())
	}
	if s.authority != nil {
		err = errors.Join(err, s.authority.Close())
	}
	return err
}

func (s *dialectArtifactSelectedSources) checkReadAdmission(ctx context.Context, scope dialectArtifactOwnerScope) error {
	if err := s.checkSource(ctx, scope); err != nil {
		return err
	}
	if err := s.s.authorizeAssistantRunStorage(s.request.WithContext(ctx)); err != nil {
		return err
	}
	identity, _, err := s.s.assistantRunIdentity(s.request.WithContext(ctx))
	if err != nil || !assistantRunIdentityMatches(identity, s.identity) {
		return errors.Join(errAssistantRunScope, err)
	}
	return nil
}

// A copied passive slot has no authority on its own. Only a matching slot from
// this original admitted source set can be used, and only inside this callback.
// One-ref views preserve existing injected request caps without summing unknown
// declared sizes or recursively holding a shared native/provider queue.
func (s *dialectArtifactSelectedSources) withExactSource(slot dialectArtifactSelectedSlot, use func(retainedArtifactTransferView) error) (err error) {
	if s == nil || s.self != s || use == nil || s.slots[slot.key] != slot {
		return errRetainedArtifactAuthorityUnavailable
	}
	if s.viewActive || s.authority != nil {
		return errRetainedArtifactBusy
	}
	s.viewActive = true
	defer func() { s.viewActive = false }()
	ctx := s.request.Context()
	defer func() {
		if slot.acknowledged && injectedArtifactOnlyNotExist(err) {
			err = errors.Join(errRetainedArtifactChanged, err)
		}
	}()
	if err = s.checkReadAdmission(ctx, slot.scope); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.checkReadAdmission(ctx, slot.scope)) }()
	if slot.missing {
		view := &retainedArtifactMissingTransferView{sources: s, slot: slot}
		s.view = view
		defer func() { err = joinSelectedDialectArtifactClose(err, view.Close()) }()
		return use(view)
	}
	source := s.scopes[slot.scope]
	if source.native == nil {
		if source.injected == nil {
			return errRetainedArtifactAuthorityUnavailable
		}
		guard, guardErr := s.holdInjectedReadAuthority(slot.scope)
		if guardErr != nil {
			return guardErr
		}
		defer func() { err = joinSelectedDialectArtifactClose(err, guard.Close()) }()
		selection, selectionErr := newRetainedArtifactInjectedSelection([]retainedArtifactRef{slot.ref}, false)
		if selectionErr != nil {
			return selectionErr
		}
		view, viewErr := source.injected.registration.beginD5ViewLocked(ctx, source.binding, selection, guard)
		if viewErr != nil {
			return viewErr
		}
		defer func() { err = joinSelectedDialectArtifactClose(err, view.Close()) }()
		return use(view)
	}
	view, err := beginRetainedArtifactNativeTransferView(s, slot)
	if err != nil {
		return err
	}
	defer func() { err = joinSelectedDialectArtifactClose(err, view.Close()) }()
	return use(view)
}

func joinSelectedDialectArtifactClose(prior, closed error) error {
	if closed == nil {
		return prior
	}
	return errors.Join(prior, errRetainedArtifactChanged, closed)
}

func (s *dialectArtifactSelectedSources) holdInjectedReadAuthority(scope dialectArtifactOwnerScope) (*dialectArtifactInjectedAuthorityHold, error) {
	if s == nil || s.self != s || !s.active || s.closed || s.authority != nil {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	source := s.scopes[scope]
	if source == nil || source.injected == nil || !source.binding.valid() {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	if err := s.checkSource(s.request.Context(), scope); err != nil {
		return nil, err
	}
	// This is the original active source set and trusted pin, never an ambient
	// current Put registration, copied operation or generic physical callback.
	g := &dialectArtifactInjectedAuthorityHold{s: s.s, request: s.request, sessions: s.s.sessions, signIn: s.s.signIn,
		expected: s.identity, registration: source.injected.registration, operation: source.injected.operation, binding: source.binding,
		selected: s, selectedScope: scope, physical: func() error { return s.checkSource(s.request.Context(), scope) }}
	s.authority = g
	if err := pinDialectArtifactInjectedAuthentication(g); err != nil {
		s.authority = nil
		return nil, err
	}
	return g, nil
}

type retainedArtifactMissingTransferView struct {
	sources  *dialectArtifactSelectedSources
	slot     dialectArtifactSelectedSlot
	closed   bool
	closeErr error
}

func (v *retainedArtifactMissingTransferView) GetExact(ctx context.Context, ref retainedArtifactRef) ([]byte, error) {
	if v == nil || v.closed || v.sources.view != v || !v.slot.missing || ref != v.slot.ref {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	if err := v.sources.checkSource(ctx, v.slot.scope); err != nil {
		return nil, err
	}
	return nil, os.ErrNotExist
}
func (*retainedArtifactMissingTransferView) PublishExact(context.Context, retainedArtifactRef, []byte) error {
	return errRetainedArtifactAuthorityUnavailable
}
func (v *retainedArtifactMissingTransferView) Close() error {
	if v == nil {
		return nil
	}
	if v.closed {
		return v.closeErr
	}
	if v.sources.view != v {
		return errRetainedArtifactAuthorityUnavailable
	}
	v.closeErr = v.sources.checkSource(v.sources.request.Context(), v.slot.scope)
	v.closed = true
	v.sources.view = nil
	return v.closeErr
}

type retainedArtifactNativeTransferView struct {
	sources  *dialectArtifactSelectedSources
	slot     dialectArtifactSelectedSlot
	native   *dialectArtifactSelectedNativeSource
	root     *os.Root
	queue    *os.File
	edges    []retainedArtifactInjectedEdge
	held     bool
	absent   bool
	closed   bool
	closeErr error
}

func beginRetainedArtifactNativeTransferView(sources *dialectArtifactSelectedSources, slot dialectArtifactSelectedSlot) (_ *retainedArtifactNativeTransferView, err error) {
	if sources == nil || sources.self != sources || sources.slots[slot.key] != slot || sources.view != nil {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	ctx := sources.request.Context()
	if err = sources.checkSource(ctx, slot.scope); err != nil {
		return nil, err
	}
	source := sources.scopes[slot.scope]
	if source.native == nil {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	v := &retainedArtifactNativeTransferView{sources: sources, slot: slot, native: source.native}
	sources.view = v
	defer func() {
		if err != nil {
			err = errors.Join(err, v.Close())
		}
	}()
	v.root, err = openRetainedArtifactsWithin(source.native.root, false)
	if err != nil {
		if slot.acknowledged || !injectedArtifactOnlyNotExist(err) {
			return nil, err
		}
		if absentErr := v.absence(ctx); !injectedArtifactOnlyNotExist(absentErr) {
			return nil, absentErr
		}
		v.absent = true
		return v, nil
	}
	v.edges, err = pinInjectedArtifactHeldEdges(ctx, source.native.root, v.root, ".desk-private", "retained-artifacts")
	if err != nil {
		return nil, err
	}
	v.queue, err = v.root.Open(".")
	if err != nil {
		return nil, err
	}
	if err = lockPrivateData(v.queue, true); err != nil {
		if lockHeld(err) {
			err = errors.Join(errRetainedArtifactBusy, err)
		}
		return nil, err
	}
	v.held = true
	if err = v.check(ctx); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *retainedArtifactNativeTransferView) absence(ctx context.Context) (err error) {
	if err = v.sources.checkSource(ctx, v.slot.scope); err != nil {
		return err
	}
	private, err := openDialectArtifactOwnerFolder(ctx, v.native.root, false, "selected-native-absence", ".desk-private")
	if err != nil {
		return errors.Join(errDialectArtifactOwner, err)
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-native-absence", private.Close))
	}()
	_, err = private.Lstat("retained-artifacts")
	if injectedArtifactOnlyNotExist(err) {
		return os.ErrNotExist
	}
	return errors.Join(errDialectArtifactOwner, err)
}

func (v *retainedArtifactNativeTransferView) check(ctx context.Context) error {
	if v == nil || v.closed || ctx == nil || v.sources == nil || v.sources.view != v || v.sources.slots[v.slot.key] != v.slot {
		return errRetainedArtifactAuthorityUnavailable
	}
	if err := v.sources.checkSource(ctx, v.slot.scope); err != nil {
		return err
	}
	if v.absent {
		err := v.absence(ctx)
		if !injectedArtifactOnlyNotExist(err) {
			return err
		}
		return nil
	}
	if !v.held || v.root == nil || v.queue == nil {
		return errRetainedArtifactAuthorityUnavailable
	}
	return checkInjectedArtifactEdges(ctx, v.edges)
}

func (v *retainedArtifactNativeTransferView) GetExact(ctx context.Context, ref retainedArtifactRef) (raw []byte, err error) {
	defer func() {
		if err != nil {
			raw = nil
		}
	}()
	if v == nil || ref != v.slot.ref {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	if err = v.check(ctx); err != nil {
		return nil, err
	}
	if v.absent {
		return nil, os.ErrNotExist
	}
	raw, err = readInjectedArtifactExactFile(ctx, v.root, ref.SHA256, ref.Bytes, "selected-native-object")
	if err == nil {
		err = verifyRetainedArtifact(ref, raw)
	}
	err = errors.Join(err, v.check(ctx))
	return raw, err
}
func (*retainedArtifactNativeTransferView) PublishExact(context.Context, retainedArtifactRef, []byte) error {
	return errRetainedArtifactAuthorityUnavailable
}
func (v *retainedArtifactNativeTransferView) Close() error {
	if v == nil {
		return nil
	}
	if v.closed {
		return v.closeErr
	}
	if v.sources.view != v {
		return errRetainedArtifactAuthorityUnavailable
	}
	if v.held || v.absent {
		v.closeErr = v.check(v.sources.request.Context())
	}
	v.closed = true
	v.held = false
	if v.queue != nil {
		v.closeErr = errors.Join(v.closeErr, closeDialectArtifactOwner(v.sources.request.Context(), "selected-native-view-queue", v.queue.Close))
	}
	if v.root != nil {
		v.closeErr = errors.Join(v.closeErr, closeDialectArtifactOwner(v.sources.request.Context(), "selected-native-view-root", v.root.Close))
	}
	v.sources.view = nil
	return v.closeErr
}
