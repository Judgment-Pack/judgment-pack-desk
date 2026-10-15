package desk

// A Desk home separates installation custody from portable Desk folders. IDs
// and retained namespaces are explicit: moving a folder never invents an owner,
// signing workspace, or conversation namespace.
import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const deskHomeRegistryName = "desks.json"
const deskHomePointerName = "desk-home.json"
const deskHomePlatform = "_platform"

type deskHomeEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Folder string `json:"folder"`
	// A migrated startup project preserves its existing signing identity.
	Kind            string `json:"kind"`
	HistoryIdentity string `json:"historyIdentity"`
	StorageOwner    string `json:"storageOwner"`
	RunnerWorkspace string `json:"runnerWorkspace"`
}
type deskHomeRegistry struct {
	Version int             `json:"version"`
	Owner   string          `json:"owner"`
	Startup string          `json:"startup"`
	Desks   []deskHomeEntry `json:"desks"`
}
type deskHome struct {
	Path     string
	Registry deskHomeRegistry
}
type deskHomePointer struct {
	Version int    `json:"version"`
	Path    string `json:"path"`
}

func validHomeFolder(name string) bool {
	// Keep folders portable to Windows/WSL, including device names with extensions.
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	runes := []rune(base)
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" ||
		(len(runes) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && strings.ContainsRune("123456789¹²³", runes[3])) {
		return false
	}
	return name != "" && name != "." && !strings.HasPrefix(name, ".") && !strings.EqualFold(name, deskHomePlatform) && filepath.IsLocal(name) && filepath.Base(name) == name && !strings.ContainsAny(name, `\/:*?"<>|`) && !strings.ContainsFunc(name, unicode.IsControl) && len(name) <= 120 && !strings.HasSuffix(name, ".") && !strings.HasSuffix(name, " ")
}
func validateHomeRegistry(value deskHomeRegistry) error {
	if value.Version != 1 || !startupIDForm.MatchString(value.Owner) || len(value.Desks) == 0 || len(value.Desks) > maxDesks+1 {
		return errors.New("Desk home registry is invalid.")
	}
	ids, folders := map[string]bool{}, map[string]bool{}
	startup := false
	for _, entry := range value.Desks {
		folder := strings.ToLower(entry.Folder)
		if !deskIDPattern.MatchString(entry.ID) || !validDeskName(entry.Name) || !validHomeFolder(entry.Folder) || ids[entry.ID] || folders[folder] || (entry.Kind != "desk" && entry.Kind != "project") || entry.HistoryIdentity == "" || (!deskIDPattern.MatchString(entry.StorageOwner) && !startupIDForm.MatchString(entry.StorageOwner)) || (!deskIDPattern.MatchString(entry.RunnerWorkspace) && !startupIDForm.MatchString(entry.RunnerWorkspace)) {
			return errors.New("Desk home contains an invalid or duplicate Desk identity.")
		}
		if entry.Kind == "desk" && (entry.HistoryIdentity != "desk:"+entry.ID || entry.StorageOwner != entry.ID || entry.RunnerWorkspace != entry.ID) {
			return errors.New("A managed Desk's retained identity cannot be changed.")
		}
		if entry.Kind == "project" && !validProjectHistoryPath(entry.HistoryIdentity) {
			return errors.New("The original project's history identity is invalid.")
		}
		ids[entry.ID], folders[folder] = true, true
		startup = startup || entry.ID == value.Startup
	}
	if !startup {
		return errors.New("The startup Desk is missing from Desk home.")
	}
	return nil
}
func readDeskHome(config string) (*deskHome, error) {
	if filepath.Base(config) != deskHomePlatform {
		return nil, nil
	}
	root, err := openPrivateDataRoot(config, false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	raw, err := readPrivateData(root, deskHomeRegistryName, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("Desk home registry could not be read: %w", err)
	}
	var value deskHomeRegistry
	if err = decodeDataJSON(raw, &value); err == nil {
		err = validateHomeRegistry(value)
	}
	if err != nil {
		return nil, err
	}
	return &deskHome{Path: filepath.Dir(config), Registry: value}, nil
}
func (h *deskHome) entry(path string) *deskHomeEntry {
	if h == nil {
		return nil
	}
	for _, entry := range h.Registry.Desks {
		if filepath.Join(h.Path, entry.Folder) == path {
			copy := entry
			return &copy
		}
	}
	return nil
}
func (s *Server) homeEntry() *deskHomeEntry { return s.home.entry(s.projectDir) }
func (s *Server) containedDeskData() bool   { return s.cfg.deskID != "" || s.homeEntry() != nil }
func (s *Server) installationOwner() string {
	if s.home != nil {
		return s.home.Registry.Owner
	}
	return digestOf([]byte(s.configDir))
}
func (s *Server) registryPath() string {
	if s.home != nil {
		return s.home.Path
	}
	return filepath.Join(s.configDir, "desks")
}
func (s *Server) registryFolder(id string) (string, error) {
	if !deskIDPattern.MatchString(id) {
		return "", errors.New("Invalid Desk ID.")
	}
	if s.home == nil {
		return id, nil
	}
	// Read under the caller's registry lock. Newly published Desks and changed
	// startup selections must not be hidden by a cached startup snapshot.
	home, err := readDeskHome(s.configDir)
	if err != nil {
		return "", err
	}
	for _, entry := range home.Registry.Desks {
		if entry.ID == id {
			return entry.Folder, nil
		}
	}
	return "", os.ErrNotExist
}
func (s *Server) registryRoot(create bool) (*os.Root, error) {
	if s.home != nil {
		return openPrivateDataRoot(s.home.Path, false)
	}
	if create {
		return s.desksRoot()
	}
	return s.assistant.root.OpenRoot("desks")
}
func homeFolderName(name, id string) string {
	var out strings.Builder
	dash := false
	for _, char := range strings.ToLower(name) {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			if dash && out.Len() > 0 {
				out.WriteByte('-')
			}
			dash = false
			out.WriteRune(char)
		} else {
			dash = true
		}
		if out.Len() >= 64 {
			break
		}
	}
	slug := out.String()
	if slug == "" {
		slug = "desk"
	}
	// The ID suffix makes concurrent creations and equal translated names safe.
	return slug + "-" + id[:8]
}
func (s *Server) publishHomeEntry(id, name, folder string) error {
	if s.home == nil {
		return nil
	}
	home, err := readDeskHome(s.configDir)
	if err != nil {
		return err
	}
	home.Registry.Desks = append(home.Registry.Desks, deskHomeEntry{ID: id, Name: name, Folder: folder, Kind: "desk", HistoryIdentity: "desk:" + id, StorageOwner: id, RunnerWorkspace: id})
	if err = validateHomeRegistry(home.Registry); err != nil {
		return err
	}
	raw, err := json.Marshal(home.Registry)
	if err != nil {
		return err
	}
	return writePrivateData(s.assistant.root, deskHomeRegistryName, raw)
}

// The old config location holds only a bootstrap pointer after migration. A bad
// pointer is an error, never permission to create a second empty installation.
func resolveHomeConfig(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	legacy := legacyConfigDir()
	chosen := os.Getenv("JPACK_DESK_HOME")
	if chosen == "" {
		if _, err := os.Lstat(filepath.Join(legacy, deskHomePointerName)); errors.Is(err, os.ErrNotExist) {
			return legacy, nil
		} else if err != nil {
			return "", err
		}
		root, err := openPrivateDataRoot(legacy, false)
		if err != nil {
			return "", err
		}
		defer root.Close()
		raw, err := readPrivateData(root, deskHomePointerName, 4096)
		if err != nil {
			return "", err
		}
		var pointer deskHomePointer
		if decodeDataJSON(raw, &pointer) != nil || pointer.Version != 1 {
			return "", errors.New("Desk home location is invalid.")
		}
		chosen = pointer.Path
	}
	if !validProjectHistoryPath(chosen) || isFilesystemRoot(chosen) {
		return "", errors.New("Desk home must name an absolute folder.")
	}
	config := filepath.Join(chosen, deskHomePlatform)
	if _, err := readDeskHome(config); err != nil {
		return "", err
	}
	return config, nil
}

// Shared services remain platform-owned; generated evidence stays with its Desk.
func platformRelative(config, name string) string {
	if filepath.Base(config) != deskHomePlatform {
		return name
	}
	switch {
	case name == "gateway-connections":
		return "connections"
	case name == "local-gateway":
		return filepath.Join("services", "gateway")
	case name == "codex" || strings.HasPrefix(name, "codex-"):
		return filepath.Join("accounts", name)
	}
	return name
}
func platformPath(config, name string) string {
	return filepath.Join(config, platformRelative(config, name))
}
func (s *Server) inputHandoffPath() string {
	if s.home != nil {
		return filepath.Join(s.projectDir, ".desk-private", "input-handoffs")
	}
	return filepath.Join(s.configDir, "input-handoffs")
}
