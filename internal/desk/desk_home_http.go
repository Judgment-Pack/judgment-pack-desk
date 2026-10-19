package desk

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

func localHomeRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	return err == nil && ip != nil && ip.IsLoopback()
}
func (s *Server) homeSelection(r *http.Request) (*Server, error) {
	id := r.Header.Get("X-Jpack-Desk")
	if id == "" {
		return s, nil
	}
	s.desksMu.Lock()
	defer s.desksMu.Unlock()
	child, ok := s.desks[id]
	if !ok {
		return nil, errors.New("The selected Desk is unavailable.")
	}
	return child, nil
}
func (s *Server) handleDeskHome(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	selected, err := s.homeSelection(r)
	if err != nil {
		writeJSONCoded(w, 404, CodeNotFound, err.Error())
		return
	}
	current, err := selected.deskRecord()
	if err != nil {
		storageFailure(w, err)
		return
	}
	s.desksMu.Lock()
	rows := []deskRecord{}
	startup, _ := s.deskRecord()
	rows = append(rows, startup)
	for _, child := range s.desks {
		row, err := child.deskRecord()
		if err == nil {
			rows = append(rows, row)
		}
	}
	s.desksMu.Unlock()
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	path := ""
	if s.home != nil {
		path = s.home.Path
	}
	s.homeMove.mu.Lock()
	destination := s.homeMove.destination
	s.homeMove.mu.Unlock()
	writeJSON(w, 200, map[string]any{"path": path, "defaultPath": deskApplicationDataPath(), "current": current, "desks": rows, "canMove": s.cfg.HomeMoveShutdown != nil, "restartRequired": s.homeMove.queued.Load(), "destination": destination})
}
func (s *Server) handleHomePreview(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	raw, err := readBounded(r.Body, 8192)
	var request projectMoveRequest
	if err != nil || decodeDataJSON(raw, &request) != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Choose a Desk home folder.")
		return
	}
	s.desksMu.Lock()
	defer s.desksMu.Unlock()
	// Live work can block a move even when every path and saved file is valid.
	// Review it here as well as at confirmation; preview never pauses triggers.
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var workBlockers []string
	for _, server := range s.homeServers() {
		if !server.location.use.TryLock() {
			workBlockers = append(workBlockers, "Finish active work in every Desk before moving Desk home.")
			continue
		}
		workErr := server.homeWorkBlocker(ctx)
		server.location.use.Unlock()
		if workErr != nil {
			workBlockers = append(workBlockers, workErr.Error())
		}
		if ctx.Err() != nil {
			workBlockers = append(workBlockers, "Jobs and active work could not be fully checked. Review the move again.")
			break
		}
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	plan, err := s.planDeskHome(request.Destination)
	if err != nil {
		writeJSONCoded(w, 409, CodeStale, err.Error())
		return
	}
	// Validate the metadata transformations during review, before any shutdown.
	check := plan
	check.Blockers = nil
	if _, err = s.prepareHomeTransaction(check); err != nil {
		plan.Blockers = append(plan.Blockers, err.Error())
	}
	plan.Blockers = append(plan.Blockers, workBlockers...)
	writeJSON(w, 200, plan)
}
func (s *Server) handleHomeFolderPicker(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	if !localHomeRequest(r) {
		writeJSONCoded(w, 403, CodeForbidden, "Open Desk on this computer to choose a folder.")
		return
	}
	if !s.location.picking.CompareAndSwap(false, true) {
		writeJSONCoded(w, 409, CodeStale, "A folder chooser is already open.")
		return
	}
	defer s.location.picking.Store(false)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	initial := filepath.Dir(deskApplicationDataPath())
	if s.home != nil {
		initial = filepath.Dir(s.home.Path)
	}
	if _, err := os.Stat(initial); err != nil {
		initial, _ = os.UserHomeDir()
	}
	selected, err := nativeProjectFolder(ctx, initial)
	if err != nil {
		writeJSONCoded(w, 503, CodeInternal, folderPickerUnavailable)
		return
	}
	if selected == "" {
		writeJSON(w, 200, map[string]bool{"canceled": true})
		return
	}
	parent, err := OpenProjectRoot(selected)
	if err != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "The selected folder is unavailable.")
		return
	}
	defer parent.Close()
	writeJSON(w, 200, map[string]string{"destination": filepath.Join(parent.dir, deskDirName)})
}
func (s *Server) handleHomeOpenFolder(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	if !localHomeRequest(r) {
		writeJSONCoded(w, 403, CodeForbidden, "Open Desk on this computer to open its folders.")
		return
	}
	raw, err := readBounded(r.Body, 1024)
	var request struct {
		Scope string `json:"scope"`
	}
	if err != nil || decodeDataJSON(raw, &request) != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Choose a Desk folder.")
		return
	}
	selected, err := s.homeSelection(r)
	if err != nil {
		writeJSONCoded(w, 404, CodeNotFound, err.Error())
		return
	}
	path := selected.projectDir
	if request.Scope == "home" {
		if s.home == nil {
			writeJSONCoded(w, 409, CodeStale, "Set up Desk home first.")
			return
		}
		path = s.home.Path
	} else if request.Scope != "current" {
		writeJSONCoded(w, 400, CodeBadRequest, "Choose the current Desk or Desk home.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err = openDeskFolder(ctx, path); err != nil {
		writeJSONCoded(w, 503, CodeInternal, "The file explorer could not open in this desktop session.")
		return
	}
	writeJSON(w, 200, map[string]bool{"opened": true})
}
func openDeskFolder(ctx context.Context, path string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "linux" && os.Getenv("WSL_DISTRO_NAME") != "" {
		converted, _, err := pickerCommand(ctx, "wslpath", "-w", path)
		if err != nil {
			return err
		}
		cmd = exec.Command("/mnt/c/Windows/explorer.exe", strings.TrimSpace(string(converted)))
	} else if runtime.GOOS == "darwin" {
		cmd = exec.Command("/usr/bin/open", path)
	} else {
		cmd = exec.Command("xdg-open", path)
	}
	// Do not bind the explorer lifetime to the HTTP request, which ends as
	// soon as launch is acknowledged. Only WSL path conversion uses ctx.
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
