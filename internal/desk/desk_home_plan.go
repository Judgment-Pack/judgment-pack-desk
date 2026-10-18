package desk

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A plan contains locations and identities, not record or credential contents.
// Review is read-only, including when the default destination does not exist.
type deskHomeMove struct {
	Label    string `json:"label"`
	From     string `json:"from"`
	To       string `json:"to"`
	Identity string `json:"-"`
}
type deskHomePlan struct {
	Destination      string           `json:"destination"`
	Moves            []deskHomeMove   `json:"moves"`
	Blockers         []string         `json:"blockers"`
	Token            string           `json:"token"`
	Registry         deskHomeRegistry `json:"-"`
	Config           string           `json:"-"`
	Parent           string           `json:"-"`
	ParentIdentity   string           `json:"-"`
	ChatFiles        []string         `json:"-"`
	ChatPath         string           `json:"-"`
	ChatConversation string           `json:"-"`
	SourceRevision   string           `json:"-"`
}

func homePlanParent(destination string) (string, os.FileInfo, error) {
	path := destination
	for {
		info, err := os.Lstat(path)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return "", nil, errors.New("Choose a real folder; symbolic links cannot be used for Desk home.")
			}
			root, err := OpenProjectRoot(path)
			if err != nil {
				return "", nil, err
			}
			defer root.Close()
			if root.dir != path {
				return "", nil, errors.New("A destination parent resolves through a symbolic link.")
			}
			if err = safeMoveParent(info); err != nil {
				return "", nil, err
			}
			return path, info, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", nil, err
		}
		next := filepath.Dir(path)
		if next == path {
			return "", nil, err
		}
		path = next
	}
}
func uniqueHomeFolder(name, id string, used map[string]bool) string {
	folder := strings.TrimSuffix(homeFolderName(name, id), "-"+id[:8])
	if !validHomeFolder(folder) {
		folder = "desk"
	}
	candidate := folder
	for index := 2; used[strings.ToLower(candidate)]; index++ {
		candidate = fmt.Sprintf("%s-%d", folder, index)
	}
	used[strings.ToLower(candidate)] = true
	return candidate
}
func (s *Server) planDeskHome(destination string) (deskHomePlan, error) {
	plan := deskHomePlan{Destination: destination, Moves: []deskHomeMove{}, Blockers: []string{}, Config: s.configDir}
	if !validProjectHistoryPath(destination) || isFilesystemRoot(destination) || len(destination) > 2048 {
		return plan, errors.New("Choose an absolute Desk home folder.")
	}
	if !s.assistant.usable() {
		return plan, errors.New("Desk settings are unavailable.")
	}
	for _, name := range []string{runtimeConfigEnv, runtimeSigningKeyEnv, runtimeSigningPublicKeyEnv} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			plan.Blockers = append(plan.Blockers, "Restart Desk without runtime configuration or signing environment overrides before moving its home.")
			break
		}
	}
	parent, info, err := homePlanParent(destination)
	if err != nil {
		return plan, err
	}
	plan.Parent, plan.ParentIdentity = parent, identityKey(info)
	if parent == destination && info.Mode().Perm()&0077 != 0 {
		plan.Blockers = append(plan.Blockers, "The existing Desk home folder must be private (0700). Choose an unused folder or correct its permissions first.")
	}
	add := func(label, from, to string) error {
		if from == to {
			return nil
		}
		before, err := os.Lstat(from)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() && !before.Mode().IsRegular() {
			return errors.New("A source location is not a regular file or folder: " + label)
		}
		if err = checkMoveFilesystem(before, info); err != nil {
			plan.Blockers = append(plan.Blockers, label+": "+err.Error())
		}
		if pathContains(from, destination) {
			plan.Blockers = append(plan.Blockers, "Desk home cannot be inside "+label+".")
		}
		if _, err = os.Lstat(to); err == nil {
			plan.Blockers = append(plan.Blockers, "The destination already contains "+label+".")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		plan.Moves = append(plan.Moves, deskHomeMove{Label: label, From: from, To: to, Identity: identityKey(before)})
		return nil
	}
	if s.home != nil {
		if destination == s.home.Path {
			return plan, errors.New("Desk home is already at this location.")
		}
		if pathContains(s.home.Path, destination) || pathContains(destination, s.home.Path) {
			return plan, errors.New("Choose a folder outside the current Desk home.")
		}
		current, err := readDeskHome(s.configDir)
		if err != nil {
			return plan, err
		}
		plan.Registry = current.Registry
		if _, err = os.Lstat(destination); err == nil {
			plan.Blockers = append(plan.Blockers, "Choose an unused destination for the whole Desk home.")
		} else if !errors.Is(err, os.ErrNotExist) {
			return plan, err
		}
		if err = add("Desk home", s.home.Path, destination); err != nil {
			return plan, err
		}
	} else {
		if pathContains(s.projectDir, destination) || pathContains(destination, s.projectDir) || pathContains(s.configDir, destination) || pathContains(destination, s.configDir) {
			return plan, errors.New("Choose a Desk home outside the existing project and settings folders.")
		}
		if s.cfg.deskID != "" {
			return plan, errors.New("Open the startup project before consolidating the existing installation.")
		}
		if s.projectHasMoved() || s.locationStatus().Problem != "" || s.startupShared() || s.startupUnresolved() {
			return plan, errors.New("Resolve the current project move or identity before consolidating Desk home.")
		}
		plan.Registry = deskHomeRegistry{Version: 1, Owner: s.installationOwner(), Desks: []deskHomeEntry{}}
		used := map[string]bool{deskHomePlatform: true}
		id := digestOf([]byte("desk-home-project:" + s.installationOwner() + ":" + s.projectDir))[:32]
		entry := deskHomeEntry{ID: id, Name: filepath.Base(s.projectDir), Folder: uniqueHomeFolder(filepath.Base(s.projectDir), id, used), Kind: "project", HistoryIdentity: s.conversationIdentity(), StorageOwner: s.storageOwner(), RunnerWorkspace: digestOf([]byte(s.projectDir))}
		plan.Registry.Startup = id
		plan.Registry.Desks = append(plan.Registry.Desks, entry)
		if err = add("Installation settings and credentials", s.configDir, filepath.Join(destination, deskHomePlatform)); err != nil {
			return plan, err
		}
		if err = add("Startup Desk: "+entry.Name, s.projectDir, filepath.Join(destination, entry.Folder)); err != nil {
			return plan, err
		}
		// Inspect the on-disk registry, including Desks which could not be resumed.
		root, rootErr := s.registryRoot(false)
		if rootErr != nil && !errors.Is(rootErr, os.ErrNotExist) {
			return plan, rootErr
		}
		if rootErr == nil {
			defer root.Close()
			entries, err := storageEntries(root)
			if err != nil {
				return plan, err
			}
			for _, item := range entries {
				if !deskIDPattern.MatchString(item.Name()) {
					plan.Blockers = append(plan.Blockers, "The Desk registry contains an unrecognized folder; resolve it before moving.")
					continue
				}
				folder, err := root.OpenRoot(item.Name())
				if err != nil {
					return plan, err
				}
				record, err := readDeskManifest(folder, item.Name())
				folder.Close()
				if err != nil {
					plan.Blockers = append(plan.Blockers, "A saved Desk has missing or invalid metadata: "+item.Name())
					continue
				}
				entry := deskHomeEntry{ID: record.ID, Name: record.Name, Folder: uniqueHomeFolder(record.Name, record.ID, used), Kind: "desk", HistoryIdentity: "desk:" + record.ID, StorageOwner: record.ID, RunnerWorkspace: record.ID}
				plan.Registry.Desks = append(plan.Registry.Desks, entry)
				if err = add("Desk: "+record.Name, filepath.Join(s.registryPath(), record.ID), filepath.Join(destination, entry.Folder)); err != nil {
					return plan, err
				}
			}
		}
		if err = validateHomeRegistry(plan.Registry); err != nil {
			return plan, err
		}
		// Retained history is selected by its saved namespace, including attachments.
		// Do not initialize a missing pointer merely to preview a migration.
		chatPath := s.defaultChatDataPath()
		raw, e := readPrivateData(s.assistant.root, dataLocationName, 16<<10)
		if e == nil {
			var location dataLocation
			if decodeDataJSON(raw, &location) != nil || location.Version != 1 || !validProjectHistoryPath(location.Path) {
				return plan, errors.New("Chat storage settings need attention before moving.")
			}
			chatPath = location.Path
		} else if !errors.Is(e, os.ErrNotExist) {
			return plan, e
		} else {
			entries, e := storageEntries(s.assistant.root)
			if e != nil {
				return plan, e
			}
			for _, item := range entries {
				if conversationFileName.MatchString(item.Name()) {
					chatPath = s.configDir
					break
				}
			}
		}
		plan.ChatPath = chatPath
		if _, e := os.Stat(chatPath); e == nil {
			root, e := openPrivateDataRoot(chatPath, false)
			if e != nil {
				return plan, e
			}
			defer root.Close()
			conversation, e := resolveConversationName(root, s.conversationIdentity())
			if e != nil {
				return plan, e
			}
			plan.ChatConversation = conversation
			entries, e := storageEntries(root)
			if e != nil {
				return plan, e
			}
			suffix := strings.TrimPrefix(conversation, "conversations-")
			var revision strings.Builder
			for _, item := range entries {
				name := item.Name()
				belongs := name == conversation || name == "draft-graphs-"+suffix || name == "draft-packs-"+suffix || name == "briefs-"+suffix || name == "pack-tests-"+suffix || name == "source-reviews-"+suffix || name == "ai-memory-"+suffix || strings.HasPrefix(name, attachmentProjectPrefix(conversation))
				if !belongs {
					continue
				}
				content, e := readPrivateData(root, name, storageFileLimit(name))
				if e != nil {
					return plan, e
				}
				if e = validateStorageFile(name, content); e != nil {
					return plan, e
				}
				plan.ChatFiles = append(plan.ChatFiles, name)
				revision.WriteString(name + ":" + digestOf(content) + "\n")
			}
			plan.SourceRevision = digestOf([]byte(revision.String()))
		} else if !errors.Is(e, os.ErrNotExist) {
			return plan, e
		}
		// The startup's Runner directory follows its retained workspace unchanged.
		if err = add("Startup Jobs", filepath.Join(s.configDir, "jobs", entry.RunnerWorkspace), filepath.Join(destination, entry.Folder, ".desk-private", "jobs")); err != nil {
			return plan, err
		}
		// Platform services have explicit homes; accounts and credentials never
		// enter a Desk export or a project file tree.
		configEntries, e := storageEntries(s.assistant.root)
		if e != nil {
			return plan, e
		}
		for _, item := range configEntries {
			name := item.Name()
			if name == "gateway-connections" || name == "local-gateway" || name == "codex" || strings.HasPrefix(name, "codex-") {
				if e = add("Platform "+name, filepath.Join(s.configDir, name), platformPath(filepath.Join(destination, deskHomePlatform), name)); e != nil {
					return plan, e
				}
			}
		}
		for _, desk := range plan.Registry.Desks {
			key := desk.ID
			if desk.Kind == "project" {
				key = s.signingKeyName()
			}
			if key != "" {
				if e = add("Timestamp settings: "+desk.Name, filepath.Join(s.configDir, stampingDirName, key), filepath.Join(destination, desk.Folder, ".desk-private", stampingDirName)); e != nil {
					return plan, e
				}
			}
		}
		// Include installed artifacts explicitly. Never infer that an unrelated
		// sibling folder belongs to Desk simply because its name looks similar.
		artifacts := deskApplicationDataPath()
		for _, name := range []string{"local-builds", "releases"} {
			if err = add("Installed "+name, filepath.Join(artifacts, name), filepath.Join(destination, deskHomePlatform, name)); err != nil {
				return plan, err
			}
		}
	}
	install, installErr := homeManagedInstall(s.configDir)
	if installErr != nil {
		return plan, installErr
	}
	if install != "" {
		next := filepath.Join(destination, deskHomePlatform, "install")
		if s.home == nil {
			if err = add("Managed installation and releases", install, next); err != nil {
				return plan, err
			}
		}
		if err = managedInstallHomeReady(install); err != nil {
			plan.Blockers = append(plan.Blockers, err.Error())
		}
		// A recognized launcher is retargeted in the transaction. No script content
		// or credentials enter the preview response.
		userHome, _ := os.UserHomeDir()
		launcher := filepath.Join(userHome, ".local", "bin", "jpack-desk")
		if s.cfg.DeskConfigDir == "" {
			raw, e := readHomeLauncher(launcher)
			if e != nil {
				return plan, e
			}
			if raw != nil {
				if _, e = migratedLauncher(raw, install, next); e != nil {
					plan.Blockers = append(plan.Blockers, e.Error())
				}
			}
		}
	}
	sort.Strings(plan.Blockers)
	// Bind all internal IDs and saved history to this review as well as the rows
	// visible in Admin. A changed registry cannot be confirmed with an old token.
	snapshot, _ := json.Marshal(struct {
		Destination, Config, Parent, ParentID, Revision string
		Registry                                        deskHomeRegistry
		Moves                                           []deskHomeMove
		Identities                                      []string
		Files                                           []string
	}{destination, plan.Config, parent, plan.ParentIdentity, plan.SourceRevision, plan.Registry, plan.Moves, homeMoveIdentities(plan.Moves), plan.ChatFiles})
	mac := hmac.New(sha256.New, s.reviewKey[:])
	mac.Write(snapshot)
	plan.Token = hex.EncodeToString(mac.Sum(nil))
	return plan, nil
}
func homeMoveIdentities(moves []deskHomeMove) []string {
	ids := make([]string, 0, len(moves))
	for _, move := range moves {
		ids = append(ids, move.Identity)
	}
	return ids
}
