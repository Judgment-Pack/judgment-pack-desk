package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// storageMoveSettingsConflict is a project move whose recovery found the
// installation settings file edited after the move's rename: it holds neither
// the settings the move found nor the ones it committed to write. Recovery
// stays unresolved (the start is read only) and Desk never overwrites the later
// edit by itself. The person has one choice, offered under Admin → Desk home:
// finish the move with the moved settings, then make the later edit again.
//
// The choice is offered only where the move's committed settings can be
// restored exactly: the move found no settings file, so its committed bytes
// are the moved settings composed onto nothing, and removing the later file
// returns the settings to the state the move recorded. The next start's
// recovery then writes the committed bytes through the move's own journal.
type storageMoveSettingsConflict struct {
	File      string
	Digest    storagebudget.Digest
	Size      int
	Later     []string
	CanFinish bool
}

func (e storageMoveSettingsConflict) Error() string {
	later := strings.Join(e.Later, ", ")
	if e.CanFinish {
		return "The settings in " + e.File + " were edited after the project was moved, so the move could not finish. Desk does not overwrite a later edit by itself. Under Admin → General → Desk home, choose Finish the move with the moved settings, restart Desk, and then make this later edit again: " + later + "."
	}
	return "The settings in " + e.File + " were edited after the project was moved, so the move could not finish. Desk does not overwrite a later edit by itself, and it cannot restore the settings the move found. The later edit changed: " + later + ". Recovery information was kept."
}

// projectMoveSettingsConflictStatus is the conflict as the location card shows it.
type projectMoveSettingsConflictStatus struct {
	File      string   `json:"file"`
	Digest    string   `json:"digest"`
	Later     []string `json:"later"`
	CanFinish bool     `json:"canFinish"`
	Finished  bool     `json:"finished"`
}

// storageMoveSettingsWords names a settings member in words; the member's own
// name is kept in quotes so the person can find it in the file.
func storageMoveSettingsWords(name string) string {
	switch name {
	case "project":
		return "the startup project (“project”)"
	case "assistant":
		return "the assistant settings (“assistant”)"
	case "research":
		return "the document and research settings (“research”)"
	}
	return "“" + name + "”"
}

// newStorageMoveSettingsConflict words the conflict for the settings file at
// file, whose edited bytes are raw, against the move's settings tail v.
func newStorageMoveSettingsConflict(file string, raw []byte, v storageMoveTail, destination string) storageMoveSettingsConflict {
	out := storageMoveSettingsConflict{File: file, Digest: sha256.Sum256(raw), Size: len(raw)}
	project, _ := json.Marshal(map[string]string{"file": filepath.Join(destination, projectConfigName)})
	var moved []byte
	if v.Before == (storageOwnerPersistentObject{}) && v.BeforeBytes == 0 {
		composed, problems := composeDeskFile(nil, false, nil, project, nil)
		if len(problems) == 0 && uint64(len(composed)) == v.AfterBytes && sha256.Sum256(composed) == v.AfterDigest {
			moved = composed
			out.CanFinish = true
		}
	}
	movedMembers := map[string]json.RawMessage{}
	if moved != nil {
		if members, _, err := topLevelMembers(moved); err == nil {
			for _, m := range members {
				movedMembers[m.name] = m.raw
			}
		}
	}
	// The later edit is what the person wrote: each member of the later file
	// that the moved settings do not already hold with the same value.
	if members, duplicate, err := topLevelMembers(raw); err == nil && duplicate == "" {
		for _, m := range members {
			if old, ok := movedMembers[m.name]; ok && storageMoveSameJSON(old, m.raw) {
				continue
			}
			out.Later = append(out.Later, storageMoveSettingsWords(m.name))
		}
	}
	if len(out.Later) == 0 {
		out.Later = []string{"the whole settings file, which is no longer one Desk can read"}
	}
	return out
}

func storageMoveSameJSON(a, b json.RawMessage) bool {
	var x, y bytes.Buffer
	return json.Compact(&x, a) == nil && json.Compact(&y, b) == nil && bytes.Equal(x.Bytes(), y.Bytes())
}

// noteMoveSettingsConflict records a conflict found at the start, worded on the
// location card. The start stays read only: the move's recovery is unresolved.
func (s *Server) noteMoveSettingsConflict(c storageMoveSettingsConflict) {
	s.location.mu.Lock()
	s.location.settings = &c
	s.location.problem = c.Error()
	s.location.mu.Unlock()
}

func (s *Server) moveSettingsConflictStatus() *projectMoveSettingsConflictStatus {
	c := s.location.settings
	if c == nil {
		return nil
	}
	return &projectMoveSettingsConflictStatus{File: c.File, Digest: hex.EncodeToString(c.Digest[:]), Later: append([]string{}, c.Later...), CanFinish: c.CanFinish && !s.location.settingsFinished, Finished: s.location.settingsFinished}
}

// handleProjectMoveFinishSettings is the one choice for a settings conflict:
// finish the move with the moved settings. It answers only for the conflict
// this start found and only with the exact edited bytes that start reported;
// it moves that later file aside, checks it, and removes it, so the settings
// are back to the state the move recorded. The next start's recovery writes
// the move's committed settings and finishes the move.
func (s *Server) handleProjectMoveFinishSettings(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request struct {
		Digest string `json:"digest"`
	}
	raw, err := readBounded(r.Body, 4<<10)
	if err != nil || decodeDataJSON(raw, &request) != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Name the settings conflict to finish.")
		return
	}
	s.location.mu.Lock()
	defer s.location.mu.Unlock()
	c := s.location.settings
	if c == nil || !c.CanFinish || s.location.settingsFinished {
		writeJSONCoded(w, 409, CodeStale, "There is no settings conflict this Desk can finish. Restart Desk to check the project move again.")
		return
	}
	if request.Digest != hex.EncodeToString(c.Digest[:]) {
		writeJSONCoded(w, 409, CodeStale, "The settings conflict changed. Restart Desk to see the current one.")
		return
	}
	if err := s.restoreMoveSettings(c); err != nil {
		writeJSONCoded(w, 409, CodeStale, err.Error())
		return
	}
	s.location.settingsFinished = true
	s.location.problem = "The moved settings are back in place of the later edit in " + c.File + ". Restart Desk to finish the move, and then make this later edit again: " + strings.Join(c.Later, ", ") + "."
	status := projectLocation{Path: s.projectDir, DefaultDestination: s.defaultProjectDestination(), Moving: s.location.moving.Load(), RestartRequired: true, MovedTo: s.projectDir, Problem: s.location.problem}
	status.SettingsConflict = &projectMoveSettingsConflictStatus{File: c.File, Digest: hex.EncodeToString(c.Digest[:]), Later: append([]string{}, c.Later...), Finished: true}
	writeJSON(w, 200, status)
}

// restoreMoveSettings removes the later settings file, and only the exact bytes
// the start reported: renamed aside first so a concurrent write cannot be
// removed unseen, checked there, and put back if they changed.
func (s *Server) restoreMoveSettings(c *storageMoveSettingsConflict) error {
	if s.assistant == nil || !s.assistant.usable() || filepath.Join(s.configDir, deskConfigName) != c.File {
		return errors.New("The settings folder is unavailable. Nothing was changed.")
	}
	root := s.assistant.root
	aside := "." + deskConfigName + ".later-edit"
	if _, err := root.Lstat(aside); !errors.Is(err, os.ErrNotExist) {
		return errors.New("A set-aside settings file from an earlier attempt is still present. Nothing was changed.")
	}
	if err := root.Rename(deskConfigName, aside); err != nil {
		return errors.New("The later settings could not be set aside. Nothing was changed.")
	}
	info, err := root.Lstat(aside)
	var raw []byte
	if err == nil && info.Mode().IsRegular() && info.Size() == int64(c.Size) {
		raw, err = root.ReadFile(aside)
	}
	if err != nil || len(raw) != c.Size || sha256.Sum256(raw) != c.Digest {
		if _, e := root.Lstat(deskConfigName); errors.Is(e, os.ErrNotExist) {
			_ = root.Rename(aside, deskConfigName)
		}
		return errors.New("The settings changed again since Desk started. Nothing was changed; restart Desk to see the current conflict.")
	}
	if err = root.Remove(aside); err != nil {
		return err
	}
	return syncPrivateDirectory(root)
}
