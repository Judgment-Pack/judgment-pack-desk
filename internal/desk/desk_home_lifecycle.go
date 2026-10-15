package desk

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type homeMoveState struct {
	queued      atomic.Bool
	mu          sync.Mutex
	journal     string
	destination string
}

func openHomeLease(config string, exclusive bool) (*os.File, error) {
	root, err := openPrivateDataRoot(config, false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.OpenFile(".home-use.lock", os.O_CREATE|os.O_RDWR|openNoFollow|openNonBlocking, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || ownedByUs("Desk home lease", info) != nil {
		file.Close()
		return nil, errors.New("Desk home lease is unsafe.")
	}
	if err = lockPrivateData(file, exclusive); err != nil {
		file.Close()
		return nil, errors.New("Another Desk process is using this installation. Close it before moving Desk home.")
	}
	return file, nil
}
func readHomeTransaction(path string) (*deskHomeTransaction, error) {
	raw, err := readHomeMetadata(path, 4<<20)
	if err != nil || raw == nil {
		return nil, err
	}
	var tx deskHomeTransaction
	if decodeDataJSON(raw, &tx) != nil || tx.Version != 1 || !validProjectHistoryPath(tx.Config) || !validProjectHistoryPath(tx.Destination) || len(tx.Renames) > 10000 || len(tx.Writes) > 2000 || len(tx.Prelude) > 1 {
		return nil, errors.New("Desk home recovery data is invalid.")
	}
	for _, op := range tx.Renames {
		if !validProjectHistoryPath(op.From) || !validProjectHistoryPath(op.To) || op.Identity == "" {
			return nil, errors.New("Desk home recovery paths are invalid.")
		}
	}
	for _, write := range append(append([]homeWrite{}, tx.Prelude...), tx.Writes...) {
		if !validProjectHistoryPath(write.Path) || write.Before == "" {
			return nil, errors.New("Desk home recovery settings are invalid.")
		}
	}
	return &tx, nil
}
func finishHomeTransaction(path string) error {
	tx, err := readHomeTransaction(path)
	if err != nil || tx == nil || tx.Complete {
		return err
	}
	config := tx.Config
	// The original platform folder is still authoritative after its rename.
	if _, err = os.Lstat(filepath.Join(config, ".home-use.lock")); errors.Is(err, os.ErrNotExist) {
		config = filepath.Join(tx.Destination, deskHomePlatform)
	}
	lease, err := openHomeLease(config, true)
	if err != nil {
		return err
	}
	defer lease.Close()
	return applyHomeTransaction(path, tx)
}

// RecoverDeskHomeMigration runs before opening projects, account managers or
// Runner databases. Recovery cannot run alongside another current Desk process.
func RecoverDeskHomeMigration() error {
	legacy := legacyConfigDir()
	path := homeJournalPath(legacy)
	if err := finishHomeTransaction(path); err != nil {
		return err
	}
	chosen := os.Getenv("JPACK_DESK_HOME")
	if chosen == "" {
		raw, err := readHomeMetadata(filepath.Join(legacy, deskHomePointerName), 4096)
		if err != nil {
			return err
		}
		if raw != nil {
			var pointer deskHomePointer
			if decodeDataJSON(raw, &pointer) != nil || pointer.Version != 1 || !validProjectHistoryPath(pointer.Path) {
				return errors.New("Desk home location is invalid.")
			}
			chosen = pointer.Path
		}
	}
	if chosen != "" {
		if !validProjectHistoryPath(chosen) {
			return errors.New("Desk home must name an absolute folder.")
		}
		if err := finishHomeTransaction(homeJournalPath(filepath.Join(chosen, deskHomePlatform))); err != nil {
			return err
		}
	}
	config, err := resolveHomeConfig("")
	if err != nil {
		return err
	}
	if other := homeJournalPath(config); other != path {
		return finishHomeTransaction(other)
	}
	return nil
}
func (s *Server) FinishDeskHomeMove() error {
	s.homeMove.mu.Lock()
	path := s.homeMove.journal
	s.homeMove.mu.Unlock()
	if path == "" {
		return nil
	}
	return finishHomeTransaction(path)
}

// The caller holds desksMu so every open Desk is included in a stable order.
func (s *Server) homeServers() []*Server {
	ids := make([]string, 0, len(s.desks))
	for id := range s.desks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	servers := []*Server{s}
	for _, id := range ids {
		servers = append(servers, s.desks[id])
	}
	return servers
}

func (s *Server) homeWorkBlocker(ctx context.Context) error {
	err := s.homeWorkIdle(ctx)
	if err == nil {
		return nil
	}
	name := filepath.Base(s.projectDir)
	if row, readErr := s.deskRecord(); readErr == nil {
		name = row.Name
	}
	return fmt.Errorf("%s: %w", name, err)
}

func (s *Server) homeWorkIdle(ctx context.Context) error {
	s.mu.Lock()
	for c := range s.conns {
		c.requestsMu.Lock()
		busy := c.silent || len(c.requests) > 0
		c.requestsMu.Unlock()
		if busy {
			s.mu.Unlock()
			return errors.New("Finish active assistant and runtime work before moving Desk home.")
		}
	}
	s.mu.Unlock()
	if s.stamping != nil && s.stamping.running.Load() {
		return errors.New("Wait for timestamping to finish before moving Desk home.")
	}
	if s.jobs != nil {
		after := int64(0)
		for pageIndex := 0; pageIndex < 200; pageIndex++ {
			raw, err := s.inputRunner(ctx, "jobs?after="+strconv.FormatInt(after, 10), nil, "")
			if err != nil {
				return errors.New("Jobs triggers could not be checked.")
			}
			var jobs struct {
				Items []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"items"`
				Next int64 `json:"next"`
			}
			if json.Unmarshal(raw, &jobs) != nil || jobs.Items == nil {
				return errors.New("Jobs triggers could not be verified.")
			}
			for _, job := range jobs.Items {
				if !regexp.MustCompile(`^job_[a-f0-9]{32}$`).MatchString(job.ID) {
					return errors.New("Jobs returned an invalid identifier.")
				}
				raw, err = s.inputRunner(ctx, "jobs/"+job.ID+"/triggers", nil, "")
				if err != nil {
					return err
				}
				var triggers struct {
					Items []struct {
						Paused *bool `json:"paused"`
						Config struct {
							Name string `json:"name"`
						} `json:"config"`
					} `json:"items"`
				}
				if json.Unmarshal(raw, &triggers) != nil || triggers.Items == nil {
					return errors.New("Jobs triggers could not be verified.")
				}
				for _, trigger := range triggers.Items {
					if trigger.Paused == nil {
						return errors.New("Jobs triggers could not be verified.")
					}
					if !*trigger.Paused {
						jobName, triggerName := job.Name, trigger.Config.Name
						if jobName == "" {
							jobName = job.ID
						}
						if triggerName == "" {
							triggerName = "Automatic trigger"
						}
						return fmt.Errorf("Pause %q in Jobs → %s → Triggers before moving Desk home. Its saved settings and history will move with the Desk.", triggerName, jobName)
					}
				}
				if err := s.homeSourcesIdle(ctx, job.ID); err != nil {
					return err
				}
			}
			if jobs.Next == 0 {
				break
			}
			if jobs.Next == after || pageIndex == 199 {
				return errors.New("The complete Jobs list could not be checked.")
			}
			after = jobs.Next
		}
		for _, state := range []string{"queued", "running"} {
			raw, err := s.inputRunner(ctx, "runs?state="+state, nil, "")
			if err != nil {
				return errors.New("Jobs could not be checked. Wait for Runner to become available.")
			}
			var page struct {
				Items []json.RawMessage `json:"items"`
				Next  int64             `json:"next"`
			}
			if json.Unmarshal(raw, &page) != nil || page.Items == nil {
				return errors.New("The active Jobs list could not be verified.")
			}
			if len(page.Items) > 0 || page.Next != 0 {
				return errors.New("Wait for queued and running Jobs to finish before moving Desk home.")
			}
		}
	}
	return nil
}
func (s *Server) queueHomeMove(ctx context.Context, request projectMoveRequest) error {
	if s.cfg.HomeMoveShutdown == nil {
		return errors.New("This launch cannot stop safely for a Desk home move. Restart with the local Desk launcher.")
	}
	if err := ctx.Err(); err != nil {
		return errors.New("The move confirmation expired. Review the move again.")
	}
	if !s.homeMove.mu.TryLock() {
		return errors.New("Another move confirmation is being checked. Wait for its result.")
	}
	defer s.homeMove.mu.Unlock()
	if s.homeMove.queued.Load() {
		return errors.New("A Desk home move is already scheduled. Restart Desk to finish it.")
	}
	s.desksMu.Lock()
	defer s.desksMu.Unlock()
	if s.deskCreations > 0 || s.desksClosed {
		return errors.New("Wait for Desk creation to finish before moving.")
	}
	servers := s.homeServers()
	var held []*Server
	defer func() {
		for _, server := range held {
			server.location.use.Unlock()
		}
	}()
	for _, server := range servers {
		if !server.location.use.TryLock() {
			return errors.New("Finish active work in every Desk before moving Desk home.")
		}
		held = append(held, server)
		if err := server.homeWorkBlocker(ctx); err != nil {
			return err
		}
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	plan, err := s.planDeskHome(request.Destination)
	if err != nil {
		return err
	}
	if request.Token == "" || !hmac.Equal([]byte(request.Token), []byte(plan.Token)) {
		return errors.New("Desk locations or history changed. Review the move again.")
	}
	for _, entry := range plan.Registry.Desks {
		if (s.home == nil && entry.Kind == "desk" || s.home != nil && filepath.Join(s.home.Path, entry.Folder) != s.projectDir) && s.desks[entry.ID] == nil {
			return errors.New("A saved Desk could not be opened. Resolve its startup error before moving this installation.")
		}
	}
	tx, err := s.prepareHomeTransaction(plan)
	if err != nil {
		return err
	}
	if s.homeLease == nil {
		return errors.New("Desk cannot safely exclude other processes from this move.")
	}
	if err = lockPrivateData(s.homeLease, true); err != nil {
		_ = lockPrivateData(s.homeLease, false)
		return errors.New("Close other Desk processes before moving this installation.")
	}
	queued := false
	defer func() {
		if !queued {
			_ = lockPrivateData(s.homeLease, false)
		}
	}()
	journal := homeJournalPath(s.configDir)
	previous, err := readHomeTransaction(journal)
	if err != nil {
		return err
	}
	if previous != nil && !previous.Complete {
		return errors.New("An earlier Desk home move needs recovery. Restart Desk first.")
	}
	if previous != nil {
		// Preserve completed migration metadata under platform custody.
		archive, err := openPrivateDataRoot(filepath.Join(s.configDir, "migrations"), true)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(previous)
		err = writePrivateData(archive, digestOf(data)+".json", data)
		archive.Close()
		if err != nil {
			return err
		}
		parent, err := OpenProjectRoot(filepath.Dir(journal))
		if err != nil {
			return err
		}
		err = parent.own.root.Remove(filepath.Base(journal))
		parent.Close()
		if err != nil {
			return err
		}
	}
	// The client may have canceled while inventory or companion checks ran.
	// No durable move may be queued on behalf of that expired confirmation.
	if err := ctx.Err(); err != nil {
		return errors.New("The move confirmation expired. Review the move again.")
	}
	if err = saveHomeTransaction(journal, tx, true); err != nil {
		return err
	}
	s.homeMove.journal, s.homeMove.destination = journal, plan.Destination
	s.homeMove.queued.Store(true)
	queued = true
	// Prevent periodic preparation and timestamp work from starting while main
	// drains HTTP and closes every worker. Runner closes before any file moves.
	for _, server := range servers {
		server.location.moving.Store(true)
		server.markProjectMoved(plan.Destination)
	}
	return nil
}
func (s *Server) handleHomeMove(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	raw, err := readBounded(r.Body, 8192)
	var request projectMoveRequest
	if err != nil || decodeDataJSON(raw, &request) != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Choose a Desk home and review the move first.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	s.log.Print("desk home move: checking confirmation")
	started := time.Now()
	if err = s.queueHomeMove(ctx, request); err != nil {
		s.log.Printf("desk home move: refused after %s: %v", time.Since(started).Round(time.Millisecond), err)
		writeJSONCoded(w, 409, CodeStale, err.Error())
		return
	}
	s.log.Printf("desk home move: queued after %s", time.Since(started).Round(time.Millisecond))
	writeJSON(w, 200, map[string]any{"restartRequired": true, "destination": request.Destination})
	if flush, ok := w.(http.Flusher); ok {
		flush.Flush()
	}
	go func() { time.Sleep(200 * time.Millisecond); s.cfg.HomeMoveShutdown() }()
}

func (s *Server) homeSourcesIdle(ctx context.Context, job string) error {
	after := int64(0)
	for index := 0; index < 200; index++ {
		raw, err := s.inputRunner(ctx, "jobs/"+job+"/occurrences?after="+strconv.FormatInt(after, 10), nil, "")
		if err != nil {
			return errors.New("Saved source preparations could not be checked.")
		}
		var page struct {
			Items []struct {
				State string `json:"state"`
			} `json:"items"`
			Next int64 `json:"next"`
		}
		if json.Unmarshal(raw, &page) != nil || page.Items == nil {
			return errors.New("Saved source preparations could not be verified.")
		}
		for _, item := range page.Items {
			switch item.State {
			case "accepted", "preparing", "waiting", "needs-attention":
				return errors.New("Finish or cancel pending Jobs source preparations before moving Desk home.")
			case "submitted", "failed", "expired", "cancelled", "skipped":
			default:
				return errors.New("A saved source preparation has an unsupported state. Resolve it before moving Desk home.")
			}
		}
		if page.Next == 0 {
			return nil
		}
		if page.Next == after {
			break
		}
		after = page.Next
	}
	return errors.New("The complete source preparation history could not be checked.")
}
