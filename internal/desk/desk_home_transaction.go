package desk

// Migration is a recoverable sequence of no-replace renames and compare-and-
// swap metadata writes. It runs only after all Desk processes have released
// their installation leases. Evidence, run chains and databases are moved as
// opaque bytes; they are never deserialized and rewritten by this transaction.
import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const deskHomeTransactionName = ".jpack-desk-home-migration.json"

type homeRename struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Identity string `json:"identity"`
}
type homeWrite struct {
	Path       string `json:"path"`
	Before     string `json:"before"`
	Data       []byte `json:"data"`
	Executable bool   `json:"executable,omitempty"`
}
type deskHomeTransaction struct {
	RunnerBinary      string                 `json:"runnerBinary,omitempty"`
	RunnerDigest      string                 `json:"runnerDigest,omitempty"`
	RunnerRelocations []homeRunnerRelocation `json:"runnerRelocations,omitempty"`
	Version           int                    `json:"version"`
	Destination       string                 `json:"destination"`
	Config            string                 `json:"config"`
	Prelude           []homeWrite            `json:"prelude,omitempty"`
	Renames           []homeRename           `json:"renames"`
	Writes            []homeWrite            `json:"writes"`
	Parent            string                 `json:"parent"`
	ParentIdentity    string                 `json:"parentIdentity"`
	Complete          bool                   `json:"complete"`
}

func (tx *deskHomeTransaction) relocate(path string) string {
	for _, op := range tx.Renames {
		if pathContains(op.From, path) {
			relative, _ := filepath.Rel(op.From, path)
			path = filepath.Join(op.To, relative)
		}
	}
	return path
}
func (tx *deskHomeTransaction) addRename(from, to, identity string) {
	from = tx.relocate(from)
	if from != to {
		tx.Renames = append(tx.Renames, homeRename{from, to, identity})
	}
}
func (tx *deskHomeTransaction) addJSON(path string, before []byte, value any) error {
	after, err := json.Marshal(value)
	if err != nil {
		return err
	}
	after = append(after, '\n')
	revision := "absent"
	if before != nil {
		revision = digestOf(before)
	}
	tx.Writes = append(tx.Writes, homeWrite{Path: path, Before: revision, Data: after})
	return nil
}
func readHomeMetadata(path string, limit int) ([]byte, error) {
	parent, err := OpenProjectRoot(filepath.Dir(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	raw, err := readHomeMetadataIn(parent.own.root, filepath.Base(path), limit)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return raw, err
}
func readHomeMetadataIn(root *os.Root, name string, limit int) ([]byte, error) {
	if name != "jpack.json" && name != runtimeLockName {
		return readPrivateData(root, name, limit)
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || ownedByUs(name, info) != nil {
		return nil, errors.New("Project settings are unsafe to relocate.")
	}
	f, err := root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	held, err := f.Stat()
	if err != nil || !os.SameFile(info, held) {
		return nil, errPrivateDataChanged
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > limit {
		return nil, errors.New("Project settings exceed the migration limit.")
	}
	return raw, nil
}

func (s *Server) prepareHomeTransaction(plan deskHomePlan) (deskHomeTransaction, error) {
	tx := deskHomeTransaction{Version: 1, Destination: plan.Destination, Config: plan.Config, Parent: plan.Parent, ParentIdentity: plan.ParentIdentity, Renames: []homeRename{}, Writes: []homeWrite{}}
	if len(plan.Blockers) > 0 {
		return tx, errors.New("Resolve all move blockers first.")
	}
	for _, move := range plan.Moves {
		tx.addRename(move.From, move.To, move.Identity)
	}
	relocations, err := s.homeRunnerRelocations(plan)
	if err != nil {
		return tx, err
	}
	if len(relocations) > 0 {
		tx.RunnerBinary = tx.relocate(s.cfg.RunnerBin)
		tx.RunnerDigest, err = homeBinaryDigest(s.cfg.RunnerBin)
		if err != nil {
			return tx, err
		}
		for i := range relocations {
			relocations[i].ID, err = newDeskID()
			if err != nil {
				return tx, err
			}
		}
		tx.RunnerRelocations = relocations
	}
	newConfig := filepath.Join(plan.Destination, deskHomePlatform)
	if s.home == nil {
		entry := plan.Registry.Desks[0]
		newProject := filepath.Join(plan.Destination, entry.Folder)
		for _, name := range plan.ChatFiles {
			from := filepath.Join(plan.ChatPath, name)
			info, err := os.Lstat(from)
			if err != nil {
				return tx, err
			}
			tx.addRename(from, filepath.Join(newProject, ".desk-private", "data", name), identityKey(info))
		}
		if err := tx.addJSON(filepath.Join(newProject, ".desk-private", dataLocationName), nil, dataLocation{Version: 1, Path: "data"}); err != nil {
			return tx, err
		}
		if err := tx.addJSON(filepath.Join(newProject, ".desk-private", "data", dataMarkerName), nil, dataMarker{Version: 1, Owner: entry.StorageOwner}); err != nil {
			return tx, err
		}
		// Preserve a previous explicit history binding without copying bindings for
		// other projects into this Desk's private store.
		if plan.ChatConversation != "" && plan.ChatConversation != "conversations-"+digestOf([]byte(entry.HistoryIdentity))+".json" {
			value := projectBindings{Version: 1, Projects: map[string]string{entry.HistoryIdentity: plan.ChatConversation}}
			if err := tx.addJSON(filepath.Join(newProject, ".desk-private", "data", projectBindingsName), nil, value); err != nil {
				return tx, err
			}
		}
		if err := tx.addJSON(filepath.Join(newConfig, deskHomeRegistryName), nil, plan.Registry); err != nil {
			return tx, err
		}
	}
	// Native account configuration pins its own sandbox paths. Rebuild only an
	// exact Desk-authored configuration; never inspect account credential bytes.
	entries, err := storageEntries(s.assistant.root)
	if err != nil {
		return tx, err
	}
	profiles := []string{}
	if s.home != nil {
		accountRoot, err := s.assistant.root.OpenRoot("accounts")
		if err == nil {
			items, e := storageEntries(accountRoot)
			accountRoot.Close()
			if e != nil {
				return tx, e
			}
			for _, item := range items {
				profiles = append(profiles, item.Name())
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return tx, err
		}
	} else {
		for _, item := range entries {
			profiles = append(profiles, item.Name())
		}
	}
	for _, name := range profiles {
		if name != "codex" && !strings.HasPrefix(name, "codex-") {
			continue
		}
		oldDir := platformPath(s.configDir, name)
		newDir := platformPath(newConfig, name)
		raw, err := readHomeMetadata(filepath.Join(oldDir, "profile", "config.toml"), 16<<10)
		if err != nil {
			return tx, err
		}
		if raw == nil {
			continue
		}
		after, err := codexbridge.RelocatedProfileConfig(raw, oldDir, newDir)
		if err != nil {
			return tx, err
		}
		tx.Writes = append(tx.Writes, homeWrite{Path: filepath.Join(newDir, "profile", "config.toml"), Before: digestOf(raw), Data: after})
	}
	// Retarget only mutable location metadata. Historic record text, retained
	// exports, source quotations and signed chains are deliberately not rewritten.
	for _, entry := range plan.Registry.Desks {
		oldProject := s.projectDir
		if s.home != nil {
			oldProject = filepath.Join(s.home.Path, entry.Folder)
		} else if entry.Kind == "desk" {
			oldProject = filepath.Join(s.configDir, "desks", entry.ID)
		}
		newProject := filepath.Join(plan.Destination, entry.Folder)
		raw, err := readHomeMetadata(filepath.Join(oldProject, "jpack.json"), maxDeskConfigBytes)
		if err != nil {
			return tx, err
		}
		if raw != nil {
			var config map[string]any
			if decodeDataJSON(raw, &config) != nil {
				return tx, errors.New("A project configuration is invalid.")
			}
			if audit, ok := config["audit"].(map[string]any); ok {
				if key, ok := audit["signingKey"].(string); ok && pathContains(s.configDir, key) {
					relative, _ := filepath.Rel(s.configDir, key)
					audit["signingKey"] = filepath.Join(newConfig, relative)
					if err = tx.addJSON(filepath.Join(newProject, "jpack.json"), raw, config); err != nil {
						return tx, err
					}
					if err = tx.relocateConfigLock(oldProject, newProject, raw, tx.Writes[len(tx.Writes)-1].Data); err != nil {
						return tx, fmt.Errorf("%s: %w", entry.Name, err)
					}
				}
			}
		}
		if entry.Kind == "project" {
			if _, err := os.Lstat(filepath.Join(oldProject, projectConfigName)); errors.Is(err, os.ErrNotExist) {
				if err = tx.addJSON(filepath.Join(newProject, projectConfigName), nil, map[string]any{"deskConfigVersion": 1}); err != nil {
					return tx, err
				}
			} else if err != nil {
				return tx, err
			}
			raw, err = readHomeMetadata(filepath.Join(oldProject, startupIdentityDir, startupIdentityName), startupIdentityLimit)
			if err != nil {
				return tx, err
			}
			if raw != nil {
				record, err := parseIdentity(raw)
				if err != nil {
					return tx, err
				}
				if record.Path != oldProject || record.From != "" {
					return tx, errors.New("Resolve the startup project's identity before moving.")
				}
				record.Path = newProject
				if len(record.line()) > startupIdentityLimit {
					return tx, errors.New("Choose a shorter Desk home path.")
				}
				tx.Writes = append(tx.Writes, homeWrite{Path: filepath.Join(newProject, startupIdentityDir, startupIdentityName), Before: digestOf(raw), Data: record.line()})
			} else {
				info, err := os.Stat(oldProject)
				if err != nil {
					return tx, err
				}
				record := identityRecord{ID: s.signingKeyName(), Path: newProject, Folder: identityKey(info)}
				if !startupIDForm.MatchString(record.ID) {
					return tx, errors.New("The startup project's signing identity is unavailable.")
				}
				tx.Writes = append(tx.Writes, homeWrite{Path: filepath.Join(newProject, startupIdentityDir, startupIdentityName), Before: "absent", Data: record.line()})
			}
		}
	}
	raw, err := readHomeMetadata(filepath.Join(s.configDir, deskConfigName), maxDeskConfigBytes)
	if err != nil {
		return tx, err
	}
	config := map[string]any{"deskConfigVersion": 1}
	if raw != nil && decodeDataJSON(raw, &config) != nil {
		return tx, errors.New("Desk settings are invalid.")
	}
	var startup deskHomeEntry
	for _, entry := range plan.Registry.Desks {
		if entry.ID == plan.Registry.Startup {
			startup = entry
		}
	}
	project, ok := config["project"].(map[string]any)
	if !ok {
		project = map[string]any{}
		config["project"] = project
	}
	if saved, exists := project["file"]; exists {
		if file, ok := saved.(string); ok && file != "" {
			relocated := tx.relocate(file)
			if relocated == file {
				return tx, errors.New("The saved startup project is outside this Desk home. Choose a registered Desk before moving.")
			}
			project["file"] = relocated
		}
	} else {
		project["file"] = filepath.Join(plan.Destination, startup.Folder, projectConfigName)
	}
	if err = tx.addJSON(filepath.Join(newConfig, deskConfigName), raw, config); err != nil {
		return tx, err
	}
	if s.cfg.DeskConfigDir == "" {
		install, err := homeManagedInstall(s.configDir)
		if err != nil {
			return tx, err
		}
		if install != "" {
			userHome, _ := os.UserHomeDir()
			launcher := filepath.Join(userHome, ".local", "bin", "jpack-desk")
			before, err := readHomeLauncher(launcher)
			if err != nil {
				return tx, err
			}
			if before != nil {
				after, err := migratedLauncher(before, install, filepath.Join(newConfig, "install"))
				if err != nil {
					return tx, err
				}
				bridge, err := homeLauncherBridge(before, install, filepath.Join(newConfig, "install"))
				if err != nil {
					return tx, err
				}
				tx.Prelude = append(tx.Prelude, homeWrite{Path: launcher, Before: digestOf(before), Data: bridge, Executable: true})
				tx.Writes = append(tx.Writes, homeWrite{Path: launcher, Before: digestOf(bridge), Data: after, Executable: true})
			}
		}
	}
	// This pointer is installed last. Until then startup replays the retained
	// migration journal instead of opening either half as a fresh installation.
	bootstrap := legacyConfigDir()
	if s.cfg.DeskConfigDir != "" {
		bootstrap = s.cfg.DeskConfigDir
		if s.home != nil {
			bootstrap = filepath.Join(filepath.Dir(s.home.Path), "bootstrap")
		}
	}
	pointerPath := filepath.Join(bootstrap, deskHomePointerName)
	before, err := readHomeMetadata(pointerPath, 4096)
	if err != nil {
		return tx, err
	}
	if err = tx.addJSON(pointerPath, before, deskHomePointer{Version: 1, Path: plan.Destination}); err != nil {
		return tx, err
	}
	return tx, nil
}

func makeHomeParents(path string) error {
	ancestor, _, err := homePlanParent(path)
	if err != nil {
		return err
	}
	root, err := OpenProjectRoot(ancestor)
	if err != nil {
		return err
	}
	defer root.Close()
	rel, err := filepath.Rel(ancestor, path)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" || component == ".." {
			return errors.New("Invalid migration parent.")
		}
	}
	if err = root.own.root.MkdirAll(rel, 0700); err != nil {
		return err
	}
	return syncPrivateDirectory(root.own.root)
}
func applyHomeRename(op homeRename) error {
	if moved, err := os.Lstat(op.To); err == nil && identityKey(moved) == op.Identity {
		return nil
	}
	before, err := os.Lstat(op.From)
	if errors.Is(err, os.ErrNotExist) {
		moved, e := os.Lstat(op.To)
		if e == nil && identityKey(moved) == op.Identity {
			return nil
		}
		return errors.New("A migration source is missing; recovery cannot identify its destination.")
	}
	if err != nil {
		return err
	}
	if identityKey(before) != op.Identity || before.Mode()&os.ModeSymlink != 0 {
		return errors.New("A migration source changed after review.")
	}
	if err = makeHomeParents(filepath.Dir(op.To)); err != nil {
		return err
	}
	source, err := OpenProjectRoot(filepath.Dir(op.From))
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := OpenProjectRoot(filepath.Dir(op.To))
	if err != nil {
		return err
	}
	defer target.Close()
	if source.dir != filepath.Dir(op.From) || target.dir != filepath.Dir(op.To) {
		return errors.New("A migration parent changed.")
	}
	if err = safeMoveParent(source.info); err != nil {
		return err
	}
	if err = safeMoveParent(target.info); err != nil {
		return err
	}
	current, err := source.own.root.Lstat(filepath.Base(op.From))
	if err != nil || identityKey(current) != op.Identity {
		return errors.New("A migration source changed while opening.")
	}
	if err = renameDirectoryNoReplace(source.own.dirFile, filepath.Base(op.From), target.own.dirFile, filepath.Base(op.To)); err != nil {
		return err
	}
	if err = source.own.dirFile.Sync(); err != nil {
		return err
	}
	return target.own.dirFile.Sync()
}
func applyHomeWrite(write homeWrite) error {
	if err := makeHomeParents(filepath.Dir(write.Path)); err != nil {
		return err
	}
	root, err := OpenProjectRoot(filepath.Dir(write.Path))
	if err != nil {
		return err
	}
	defer root.Close()
	var raw []byte
	if write.Executable {
		raw, err = readHomeLauncher(write.Path)
	} else {
		raw, err = readHomeMetadataIn(root.own.root, filepath.Base(write.Path), 4<<20)
	}
	revision := "absent"
	if err == nil {
		revision = digestOf(raw)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if revision == digestOf(write.Data) {
		if write.Executable {
			return root.own.root.Chmod(filepath.Base(write.Path), 0700)
		}
		return nil
	}
	if revision != write.Before {
		return errors.New("Migration metadata changed; recovery kept both locations for review.")
	}
	if write.Before == "absent" {
		return claimPrivateData(root.own.root, filepath.Base(write.Path), write.Data)
	}
	if err = writePrivateData(root.own.root, filepath.Base(write.Path), write.Data); err != nil {
		return err
	}
	if write.Executable {
		return root.own.root.Chmod(filepath.Base(write.Path), 0700)
	}
	return nil
}

// Kept outside the folders being moved so a crash cannot hide the journal.
func homeJournalPath(config string) string {
	parent := filepath.Dir(config)
	if filepath.Base(config) == deskHomePlatform {
		parent = filepath.Dir(parent)
	}
	return filepath.Join(parent, deskHomeTransactionName)
}
func saveHomeTransaction(path string, tx deskHomeTransaction, create bool) error {
	root, err := OpenProjectRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	raw, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	if create {
		return claimPrivateData(root.own.root, filepath.Base(path), raw)
	}
	return writePrivateData(root.own.root, filepath.Base(path), raw)
}

var testHomeMigrationStep func(string) error

func applyHomeTransaction(path string, tx *deskHomeTransaction) error {
	if tx.Version != 1 || !validProjectHistoryPath(tx.Destination) || !validProjectHistoryPath(tx.Config) {
		return errors.New("Invalid Desk home migration journal.")
	}
	if tx.Complete {
		return nil
	}
	if tx.Parent != "" {
		info, err := os.Lstat(tx.Parent)
		if err != nil || identityKey(info) != tx.ParentIdentity {
			return errors.New("The reviewed destination parent changed. Migration was not continued.")
		}
	}
	// A launcher bridge can open either location across the installation rename.
	// On replay after finalization, do not downgrade the final launcher.
	for _, write := range tx.Prelude {
		raw, err := readHomeLauncher(write.Path)
		if err != nil {
			return err
		}
		final := false
		for _, after := range tx.Writes {
			if after.Path == write.Path && digestOf(raw) == digestOf(after.Data) {
				final = true
				break
			}
		}
		if !final {
			if err := applyHomeWrite(write); err != nil {
				return err
			}
		}
	}
	for i, op := range tx.Renames {
		if err := applyHomeRename(op); err != nil {
			return fmt.Errorf("Desk home migration stopped at folder %d: %w", i+1, err)
		}
		if testHomeMigrationStep != nil {
			if err := testHomeMigrationStep(fmt.Sprintf("rename-%d", i)); err != nil {
				return err
			}
		}
	}
	if len(tx.RunnerRelocations) > 0 {
		binary := tx.relocate(tx.RunnerBinary)
		digest, err := homeBinaryDigest(binary)
		if err != nil || digest != tx.RunnerDigest {
			return errors.New("The reviewed Runner executable changed. Restore it before recovering this move.")
		}
		for _, request := range tx.RunnerRelocations {
			if err := applyHomeRunnerRelocation(binary, request); err != nil {
				return err
			}
		}
	}
	for i, write := range tx.Writes {
		if err := applyHomeWrite(write); err != nil {
			return fmt.Errorf("Desk home migration stopped at setting %d: %w", i+1, err)
		}
		if testHomeMigrationStep != nil {
			if err := testHomeMigrationStep(fmt.Sprintf("write-%d", i)); err != nil {
				return err
			}
		}
	}
	tx.Complete = true
	return saveHomeTransaction(path, *tx, false)
}

// The mutable lock pins exact configuration bytes. Moving the key's location
// changes that digest, without changing or re-resolving any reviewed pack or
// graph pin. An already stale lock stays byte-for-byte stale; moving folders
// does not grant review approval.
func (tx *deskHomeTransaction) relocateConfigLock(previous, next string, before, after []byte) error {
	raw, err := readHomeMetadata(filepath.Join(previous, runtimeLockName), 4<<20)
	if err != nil || raw == nil {
		return err
	}
	var lock map[string]json.RawMessage
	if decodeDataJSON(raw, &lock) != nil || string(lock["lockVersion"]) != `"1"` {
		return errors.New("The project lock cannot be safely relocated.")
	}
	var config map[string]json.RawMessage
	if decodeDataJSON(lock["config"], &config) != nil {
		return errors.New("The project lock has no configuration binding.")
	}
	var digest string
	if json.Unmarshal(config["digest"], &digest) != nil || !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
		return errors.New("The project lock has an invalid configuration binding.")
	}
	if digest != "sha256:"+digestOf(before) {
		if digest == "sha256:"+digestOf(after) {
			return errors.New("The move would make a stale project lock current. Review that configuration before moving.")
		}
		// Include the unchanged bytes in the transaction's conflict checks.
		tx.Writes = append(tx.Writes, homeWrite{Path: filepath.Join(next, runtimeLockName), Before: digestOf(raw), Data: raw})
		return nil
	}
	config["digest"], _ = json.Marshal("sha256:" + digestOf(after))
	lock["config"], _ = json.Marshal(config)
	return tx.addJSON(filepath.Join(next, runtimeLockName), raw, lock)
}
