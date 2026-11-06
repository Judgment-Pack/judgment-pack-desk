package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Handwritten acceptance regression through actual New and normal API handlers.
// This must remain red until the complete qualified first-use enrollment route
// is implemented. No listener, browser, waits, retries, fabricated snapshot or
// synthetic native qualification is involved. Metadata excludes private values.
type storageFirstUseStatusDiagnostic struct {
	HTTP         int    `json:"http"`
	Availability string `json:"availability"`
	Reason       string `json:"reason"`
	ReadOnly     bool   `json:"readOnly"`
	Snapshot     bool   `json:"snapshot"`
}

type storageFirstUseStageDiagnostic struct {
	Stage                    string                          `json:"stage"`
	Origin                   string                          `json:"origin"`
	Projects                 string                          `json:"projects"`
	Sources                  string                          `json:"sources"`
	Cell                     string                          `json:"cell"`
	Lifetime                 string                          `json:"lifetime"`
	Q                        string                          `json:"q"`
	Sanitize                 string                          `json:"sanitize"`
	EpochMatches             bool                            `json:"epochMatches"`
	CensusEpoch              uint64                          `json:"censusEpoch"`
	SourceRoots              int                             `json:"sourceRoots"`
	ImplicitSelections       int                             `json:"implicitSelections"`
	ExplicitSelections       int                             `json:"explicitSelections"`
	MaterializedImplicit     int                             `json:"materializedImplicit"`
	MaterializedMatches      int                             `json:"materializedMatches"`
	MaterializedDefault      int                             `json:"materializedDefault"`
	MaterializedRootEnrolled int                             `json:"materializedRootEnrolled"`
	SelectionReadProblems    int                             `json:"selectionReadProblems"`
	CachePresent             bool                            `json:"cachePresent"`
	CacheEncoded             bool                            `json:"cacheEncoded"`
	CacheRefreshing          bool                            `json:"cacheRefreshing"`
	CacheFailed              bool                            `json:"cacheFailed"`
	CacheSequence            uint64                          `json:"cacheSequence"`
	Status                   storageFirstUseStatusDiagnostic `json:"status"`
}

func storageFirstUseErrorClass(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, errStorageCustody):
		return "custody"
	case errors.Is(err, errStorageOwnerTransitionRequired):
		return "owner-transition-required"
	case errors.Is(err, storagebudget.ErrAuthority):
		return "authority"
	case errors.Is(err, storagebudget.ErrBusy):
		return "busy"
	case errors.Is(err, storagebudget.ErrStale):
		return "stale"
	case errors.Is(err, storagebudget.ErrBounds):
		return "bounds"
	case errors.Is(err, storagebudget.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, os.ErrNotExist):
		return "absent"
	case errors.Is(err, os.ErrClosed):
		return "closed"
	default:
		return "other"
	}
}

func TestStorageStatusActualFirstUseEnrollment(t *testing.T) {
	cfg := ownerActualConfig(t)
	cfg.JpackBin = standInRuntime(t) // Match the actual browser fixture.
	cfg.Logger = log.New(io.Discard, "", 0)
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("actual startup refused: class=%s", storageFirstUseErrorClass(err))
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("actual close refused: class=%s", storageFirstUseErrorClass(err))
		}
	})
	if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
		t.Fatalf("actual startup did not enroll: class=%s", storageFirstUseErrorClass(s.storageProblem))
	}
	c, o := s.storageConstruction, s.storageConstruction.owner
	if c.sources == nil {
		t.Fatal("actual startup has no original sources")
	}
	origin := "http://127.0.0.1:" + strconv.Itoa(s.cfg.Port)
	request := func(method, path, session, body, revision string) *httptest.ResponseRecorder {
		t.Helper()
		var input io.Reader
		if body != "" {
			input = strings.NewReader(body)
		}
		r := httptest.NewRequest(method, origin+path, input)
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		if session != "" {
			r.Header.Set("Authorization", "Bearer "+session)
		}
		if revision != "" {
			r.Header.Set("If-Match", revision)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	stage := func(name, session string) storageFirstUseStageDiagnostic {
		t.Helper()
		ctx := t.Context()
		row := storageFirstUseStageDiagnostic{Stage: name, Q: "not-read", Sanitize: "not-read"}
		row.Origin = storageFirstUseErrorClass(c.originCurrent(ctx, false))
		row.Projects = storageFirstUseErrorClass(c.sources.currentProjects(ctx))
		row.Sources = storageFirstUseErrorClass(c.sources.current(ctx))
		row.Cell = storageFirstUseErrorClass(c.ref.cell.current())
		row.SourceRoots = len(c.sources.roots)
		o.mu.Lock()
		row.CensusEpoch = o.censusEpoch
		o.mu.Unlock()
		for _, selection := range c.sources.selections {
			if selection.explicit {
				row.ExplicitSelections++
			} else {
				row.ImplicitSelections++
			}
			raw, readErr := readPrivateData(selection.settings, dataLocationName, 16384)
			if errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			if readErr != nil {
				row.SelectionReadProblems++
				continue
			}
			row.MaterializedImplicit++
			var location dataLocation
			if decodeDataJSON(raw, &location) != nil {
				row.SelectionReadProblems++
				continue
			}
			if location == selection.location {
				row.MaterializedMatches++
			}
			if location.Path == s.defaultChatDataPath() {
				row.MaterializedDefault++
			}
			for _, root := range c.sources.roots {
				if root.role == 4 && root.path == location.Path {
					row.MaterializedRootEnrolled++
				}
			}
		}
		_, owner, use, lifetimeErr := s.storageStatusLifetime(ctx)
		row.Lifetime = storageFirstUseErrorClass(lifetimeErr)
		if lifetimeErr == nil {
			value, readErr := use.cell.q.StatusSnapshot(ctx)
			row.Q = storageFirstUseErrorClass(readErr)
			if readErr == nil {
				_, sanitizeErr := storageStatusSanitize(value, use.cell.record.Installation)
				row.Sanitize = storageFirstUseErrorClass(sanitizeErr)
				row.EpochMatches = row.CensusEpoch == 0 || value.Epoch == row.CensusEpoch
			}
			if closeErr := use.Close(); closeErr != nil {
				t.Errorf("diagnostic use close refused: class=%s", storageFirstUseErrorClass(closeErr))
			}
			if dropErr := owner.drop(); dropErr != nil {
				t.Errorf("diagnostic owner drop refused: class=%s", storageFirstUseErrorClass(dropErr))
			}
		}
		if session != "" {
			response := request(http.MethodGet, "/api/storage/status", session, "", "")
			row.Status.HTTP = response.Code
			var status storageStatusResponse
			if json.Unmarshal(response.Body.Bytes(), &status) != nil {
				t.Fatal("actual status returned invalid JSON")
			}
			row.Status.Availability, row.Status.ReadOnly = status.Availability, status.ReadOnly
			row.Status.Snapshot = status.Snapshot != nil
			if status.Reason != nil {
				if !storageStatusCode(*status.Reason) {
					t.Fatal("actual status reason escaped closed catalogue")
				}
				row.Status.Reason = *status.Reason
			}
		}
		o.mu.Lock()
		cache := o.statusCache
		o.mu.Unlock()
		if cache != nil {
			cache.mu.Lock()
			row.CachePresent, row.CacheEncoded = true, len(cache.encoded) != 0
			row.CacheRefreshing, row.CacheFailed, row.CacheSequence = cache.refreshing, cache.failed, cache.sequence
			cache.mu.Unlock()
		}
		encoded, encodeErr := json.Marshal(row)
		if encodeErr != nil {
			t.Fatal("typed diagnostic encoding refused")
		}
		t.Log(string(encoded))
		return row
	}
	startup := stage("startup", "")
	setup := request(http.MethodPost, "/api/auth/setup", "", `{"code":"`+testToken+`"}`, "")
	if setup.Code != http.StatusOK {
		t.Fatalf("actual setup refused: http=%d", setup.Code)
	}
	var session struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(setup.Body.Bytes(), &session) != nil || len(session.ID) != 48 {
		t.Fatal("actual setup did not issue expected session")
	}
	afterSetup := stage("after-setup", session.ID)
	initial := request(http.MethodGet, "/api/conversations", session.ID, "", "")
	if initial.Code != http.StatusOK {
		t.Fatalf("actual conversation read refused: http=%d", initial.Code)
	}
	var conversation conversationReply
	if json.Unmarshal(initial.Body.Bytes(), &conversation) != nil || conversation.SHA256 == "" {
		t.Fatal("actual conversation read did not return revision")
	}
	afterRead := stage("after-conversation-get", session.ID)
	const chats = `{"version":1,"chats":[{"id":"storage-save-failure","title":"Storage save fixture","pinned":false,"archived":false,"updatedAt":"2026-10-31T12:00:00Z","composer":"saved original","model":"","mode":"draft","view":"chat"}]}`
	saved := request(http.MethodPut, "/api/conversations", session.ID, chats, conversation.SHA256)
	if saved.Code != http.StatusOK {
		t.Fatalf("actual conversation write refused: http=%d", saved.Code)
	}
	afterWrite := stage("after-conversation-put", session.ID)
	if startup.MaterializedImplicit != 0 || afterSetup.MaterializedImplicit != 0 || startup.Sources != "ok" || afterSetup.Sources != "ok" || !afterSetup.Status.Snapshot {
		t.Error("first-use precondition differed; inspect typed stage metadata")
	}
	if afterRead.ExplicitSelections == 0 || afterRead.ImplicitSelections != 0 || afterRead.MaterializedMatches == 0 || afterRead.MaterializedDefault == 0 || afterRead.MaterializedRootEnrolled == 0 || afterRead.SelectionReadProblems != 0 {
		t.Error("first use did not publish the exact default selection and enroll its actual History root")
	}
	if afterRead.CensusEpoch <= startup.CensusEpoch || afterWrite.CensusEpoch != afterRead.CensusEpoch {
		t.Error("first-use enrollment did not advance census once or an ordinary save repeated enrollment")
	}
	for _, row := range []storageFirstUseStageDiagnostic{afterRead, afterWrite} {
		if row.Origin != "ok" || row.Projects != "ok" || row.Sources != "ok" || row.Cell != "ok" || row.Lifetime != "ok" || row.Q != "ok" || row.Sanitize != "ok" || !row.EpochMatches {
			t.Error("first use did not reconcile original source, owner, Q and census lifetimes")
		}
		if row.Status.HTTP != http.StatusOK || row.Status.ReadOnly || !row.Status.Snapshot {
			t.Error("actual supported status lost its healthy snapshot after normal conversation first use")
		}
	}
	// Ordinary automatic identity issuance and the actual completed endpoint
	// must survive a second New without seeded identity or another enrollment.
	beforeRestart := request(http.MethodGet, "/api/conversations", session.ID, "", "")
	if beforeRestart.Code != http.StatusOK {
		t.Fatal("pre-restart saved history unavailable")
	}
	var retained conversationReply
	storageTestMust(t, json.Unmarshal(beforeRestart.Body.Bytes(), &retained))
	selectedPath := s.defaultChatDataPath()
	selectedInfo, err := os.Stat(selectedPath)
	storageTestMust(t, err)
	markerInfo, err := os.Stat(filepath.Join(selectedPath, dataMarkerName))
	storageTestMust(t, err)
	pointer, err := os.ReadFile(filepath.Join(s.configDir, dataLocationName))
	storageTestMust(t, err)
	identity, err := s.inspectStorageStartupKey()
	storageTestMust(t, err)
	storageTestMust(t, s.Close())
	s, err = New(cfg)
	storageTestMust(t, err)
	if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
		t.Fatalf("actual completed restart refused: class=%s", storageFirstUseErrorClass(s.storageProblem))
	}
	c, o = s.storageConstruction, s.storageConstruction.owner
	identityAfter, err := s.inspectStorageStartupKey()
	storageTestMust(t, err)
	if identityAfter != identity {
		t.Fatal("ordinary restart changed automatic startup identity")
	}
	session.ID = policyTestSession(t, s)
	restarted := stage("after-actual-restart", session.ID)
	if restarted.Sources != "ok" || restarted.Origin != "ok" || restarted.Q != "ok" || restarted.Sanitize != "ok" || !restarted.EpochMatches || restarted.CensusEpoch != afterWrite.CensusEpoch || restarted.Status.HTTP != http.StatusOK || restarted.Status.ReadOnly || !restarted.Status.Snapshot {
		t.Fatal("actual completed restart did not retain healthy Q/source/census status")
	}
	afterRestart := request(http.MethodGet, "/api/conversations", session.ID, "", "")
	var restored conversationReply
	if afterRestart.Code != http.StatusOK || json.Unmarshal(afterRestart.Body.Bytes(), &restored) != nil || restored.SHA256 != retained.SHA256 || !bytes.Equal(restored.Content, retained.Content) {
		t.Fatal("actual completed restart lost saved history or repeated first use")
	}
	selectedAfter, err := os.Stat(selectedPath)
	storageTestMust(t, err)
	markerAfter, err := os.Stat(filepath.Join(selectedPath, dataMarkerName))
	storageTestMust(t, err)
	pointerAfter, err := os.ReadFile(filepath.Join(s.configDir, dataLocationName))
	storageTestMust(t, err)
	if !os.SameFile(selectedInfo, selectedAfter) || !os.SameFile(markerInfo, markerAfter) || !bytes.Equal(pointer, pointerAfter) {
		t.Fatal("actual completed restart replaced captured selection/marker/pointer")
	}

}

// Required native acceptance at the exact death cut: J4 is Retired but its
// successor enrollment has not started. This uses actual New/API/Close/New,
// the existing boundary seam and ordinary automatic identity issuance. Empty
// installed qualification records keep this requirement open; no skip, injected
// proof or refusal-only result substitutes for the restored success assertion.
func TestStorageEnrollmentActualRetiredCreationRestart(t *testing.T) {
	for _, scenario := range []struct{ point, change string }{
		{"creation-retired-before-enrollment", "restored"}, {"creation-retired-before-enrollment", "changed-native"}, {"creation-retired-before-enrollment", "changed-marker-owner"}, {"creation-retired-before-enrollment", "changed-marker-payload"},
		{"creation-scope-prepared-before-q", "restored"}, {"creation-scope-prepared-before-q", "changed-native"}, {"creation-scope-prepared-before-q", "changed-marker-owner"}, {"creation-scope-prepared-before-q", "changed-marker-payload"},
	} {
		t.Run(scenario.point+"/"+scenario.change, func(t *testing.T) {
			change := scenario.change
			cfg := ownerActualConfig(t)
			cfg.JpackBin = standInRuntime(t)
			cfg.Logger = log.New(io.Discard, "", 0)
			s, err := New(cfg)
			storageTestMust(t, err)
			t.Cleanup(func() {
				if s != nil {
					storageTestMust(t, s.Close())
				}
			})
			if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
				t.Fatalf("actual startup refused: class=%s", storageFirstUseErrorClass(s.storageProblem))
			}
			c := s.storageConstruction
			cut := errors.New("handwritten retired-before-successor cut")
			entered := make(chan struct{})
			c.enrollmentFault = &storageEnrollmentFault{point: scenario.point, entered: entered, err: cut}
			response := policyCall(s, policyTestSession(t, s), http.MethodGet, "/api/conversations", nil)
			select {
			case <-entered:
			default:
				t.Fatal("actual default creation did not reach required qualified retired cut")
			}
			if response.Code == http.StatusOK {
				t.Fatal("retired-cut fault incorrectly returned successful API response")
			}
			if c.ref.cell.enrollmentEndpoint == nil {
				t.Fatal("retired cut has no original Completed endpoint")
			}
			prior := *c.ref.cell.enrollmentEndpoint
			hosts, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
			storageTestMust(t, err)
			pending, page, err := storageEnrollmentUncoveredCreation(hosts, prior)
			storageTestMust(t, err)
			if pending == nil || pending.Kind != storageCreationHistory || pending.Core.Phase != storageIntentRetired || len(page) != storageIntentPage {
				t.Fatal("cut did not retain the exact uncovered retired History record")
			}
			path := pending.SelectedPath
			selectedInfo, err := os.Stat(path)
			storageTestMust(t, err)
			markerPath := filepath.Join(path, dataMarkerName)
			markerInfo, err := os.Stat(markerPath)
			storageTestMust(t, err)
			marker, err := os.ReadFile(markerPath)
			storageTestMust(t, err)
			pointer, err := os.ReadFile(filepath.Join(s.configDir, dataLocationName))
			storageTestMust(t, err)
			identity, err := s.inspectStorageStartupKey()
			storageTestMust(t, err)
			storageTestMust(t, s.Close())
			s = nil
			switch change {
			case "changed-native":
				storageTestMust(t, os.Rename(path, path+"-retained-original"))
				storageTestMust(t, os.Mkdir(path, 0700))
				storageTestMust(t, os.WriteFile(markerPath, marker, 0600))
			case "changed-marker-owner":
				var m dataMarker
				storageTestMust(t, json.Unmarshal(marker, &m))
				m.Owner = strings.Repeat("b", 64)
				changed, err := json.Marshal(m)
				storageTestMust(t, err)
				storageTestMust(t, os.WriteFile(markerPath, changed, 0600))
			case "changed-marker-payload":
				var m dataMarker
				storageTestMust(t, json.Unmarshal(marker, &m))
				changed := []byte(`{"owner":"` + m.Owner + `","version":1}`)
				if len(changed) != len(marker) || bytes.Equal(changed, marker) {
					t.Fatal("handwritten same-size/same-owner altered payload precondition")
				}
				storageTestMust(t, os.WriteFile(markerPath, changed, 0600))
			}
			reopened, openErr := New(cfg)
			s = reopened
			if change != "restored" {
				if openErr == nil && s != nil && !s.storageReadOnly && s.storageConstruction != nil && s.storageEnrollment != nil {
					t.Fatal("tampered captured object became an active original owner")
				}
				current, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageIntentName))
				storageTestMust(t, err)
				if !bytes.Equal(hosts, current) {
					t.Fatal("tampered restart replayed or rewrote creation host evidence")
				}
				return
			}
			storageTestMust(t, openErr)
			if s == nil {
				t.Fatal("actual retired restart returned no server")
			}
			if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
				t.Fatalf("actual retired restart did not recover successfully: class=%s", storageFirstUseErrorClass(s.storageProblem))
			}
			next := s.storageConstruction.ref.cell.enrollmentEndpoint
			if next == nil || next.AfterEpoch <= prior.AfterEpoch || !storageCreationTailIncluded(*pending, *next) {
				t.Fatal("actual restart failed to enroll the exact retired member through successor epoch")
			}
			current, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
			storageTestMust(t, err)
			if !bytes.Equal(hosts, current) {
				t.Fatal("successful restart replayed/replaced the retired creation record")
			}
			identityAfter, err := s.inspectStorageStartupKey()
			storageTestMust(t, err)
			if identityAfter != identity {
				t.Fatal("retired recovery changed ordinary automatic identity lineage")
			}
			selectedAfter, err := os.Stat(path)
			storageTestMust(t, err)
			markerAfter, err := os.Stat(markerPath)
			storageTestMust(t, err)
			markerBytes, err := os.ReadFile(markerPath)
			storageTestMust(t, err)
			pointerAfter, err := os.ReadFile(filepath.Join(s.configDir, dataLocationName))
			storageTestMust(t, err)
			if !os.SameFile(selectedInfo, selectedAfter) || !os.SameFile(markerInfo, markerAfter) || !bytes.Equal(marker, markerBytes) || !bytes.Equal(pointer, pointerAfter) {
				t.Fatal("retired recovery repeated effects or changed captured native/payload evidence")
			}
			storageTestMust(t, s.storageConstruction.sources.current(t.Context()))
			status := policyCall(s, policyTestSession(t, s), http.MethodGet, "/api/storage/status", nil)
			var reply storageStatusResponse
			if status.Code != http.StatusOK || json.Unmarshal(status.Body.Bytes(), &reply) != nil || reply.ReadOnly || reply.Snapshot == nil {
				t.Fatal("actual retired recovery did not restore healthy status")
			}
		})
	}
}

// Required qualified Workflow3 route: original bytes remain in config, only
// its absent pointer is created, and retired/prepared/completed restart cuts
// must recover to healthy authority without a History directory or marker.
func TestStorageEnrollmentActualLegacyConfigSelectionRestart(t *testing.T) {
	for _, cut := range []string{"creation-retired-before-enrollment", "creation-scope-prepared-before-q", "fully-completed"} {
		t.Run(cut, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			cfg.JpackBin = standInRuntime(t)
			cfg.Logger = log.New(io.Discard, "", 0)
			name := "conversations-" + strings.Repeat("a", 64) + ".json"
			const retained = `{"version":1,"chats":[]}`
			privateFixture(t, filepath.Join(cfg.DeskConfigDir, name), retained)
			s, err := New(cfg)
			storageTestMust(t, err)
			t.Cleanup(func() {
				if s != nil {
					storageTestMust(t, s.Close())
				}
			})
			if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
				t.Fatal("actual legacy startup lacks original healthy authority")
			}
			c := s.storageConstruction
			config := c.ref.cell.ownerRecord.Config
			oldConfig, err := os.Stat(s.configDir)
			storageTestMust(t, err)
			oldHistory, err := os.Stat(filepath.Join(s.configDir, name))
			storageTestMust(t, err)
			entered := make(chan struct{})
			fault := errors.New("handwritten legacy restart cut")
			if cut != "fully-completed" {
				c.enrollmentFault = &storageEnrollmentFault{point: cut, entered: entered, err: fault}
			}
			response := policyCall(s, policyTestSession(t, s), http.MethodGet, "/api/conversations", nil)
			if cut == "fully-completed" {
				if response.Code != http.StatusOK {
					t.Fatal("qualified legacy first use did not complete", response.Code)
				}
			} else {
				select {
				case <-entered:
				default:
					t.Fatal("qualified legacy route did not reach requested original cut")
				}
				if response.Code == http.StatusOK {
					t.Fatal("legacy cut reported success")
				}
			}
			hosts, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
			storageTestMust(t, err)
			var created *storageCreationIntent
			for slot := 0; slot < 64; slot++ {
				at := slot * 2 * storageIntentPage
				choice, err := storageSelectCreationHost(hosts[at:at+storageIntentPage], hosts[at+storageIntentPage:at+2*storageIntentPage], slot, c.ref.cell.ownerRecord.Installation, c.ref.cell.ownerRecord.QBinding)
				storageTestMust(t, err)
				if choice.Creation != nil && choice.Creation.Kind == storageCreationHistory {
					if created != nil {
						t.Fatal("legacy fixture has ambiguous creation records")
					}
					value := *choice.Creation
					created = &value
				}
			}
			if created == nil || !created.legacyConfigSelection() || created.Core.Phase != storageIntentRetired || len(created.Effects) != 1 || created.Effects[0].Name != dataLocationName || created.SelectedPath != s.configDir {
				t.Fatal("legacy route did not retain pointer-only exact original creation")
			}
			pointer, err := os.ReadFile(filepath.Join(s.configDir, dataLocationName))
			storageTestMust(t, err)
			pointerInfo, err := os.Stat(filepath.Join(s.configDir, dataLocationName))
			storageTestMust(t, err)
			var location dataLocation
			storageTestMust(t, json.Unmarshal(pointer, &location))
			if location.Version != 1 || location.Path != s.configDir || location.Previous != "" {
				t.Fatal("legacy pointer changed original config selection")
			}
			endpoint := c.ref.cell.enrollmentEndpoint
			if endpoint == nil {
				t.Fatal("legacy cut lost prior Completed endpoint")
			}
			oldEpoch := endpoint.AfterEpoch
			storageTestMust(t, s.Close())
			s = nil
			s, err = New(cfg)
			storageTestMust(t, err)
			if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
				t.Fatalf("qualified legacy restart did not restore healthy authority: class=%s", storageFirstUseErrorClass(s.storageProblem))
			}
			next := s.storageConstruction.ref.cell.enrollmentEndpoint
			if next == nil || !storageCreationTailIncluded(*created, *next) || next.Board.Source.Default == nil || next.Board.Source.Default.Workflow != 3 || next.Board.Source.Default.Settings != config || next.Board.Source.Default.Selected != config || next.Board.Source.Default.Marker != (storageOwnerPersistentObject{}) {
				t.Fatal("legacy restart failed exact Workflow3 config/control coverage")
			}
			if cut == "fully-completed" {
				if next.AfterEpoch != oldEpoch {
					t.Fatal("completed legacy restart repeated enrollment")
				}
			} else if next.AfterEpoch <= oldEpoch {
				t.Fatal("legacy retired/published recovery did not finish successor")
			}
			for _, root := range s.storageConstruction.sources.roots {
				if root.role == 4 {
					t.Fatal("legacy restart invented a History root")
				}
			}
			for _, entry := range []string{"data", dataMarkerName} {
				if _, err = os.Lstat(filepath.Join(s.configDir, entry)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("legacy restart created unrelated history namespace", entry, err)
				}
			}
			configAfter, err := os.Stat(s.configDir)
			storageTestMust(t, err)
			historyAfter, err := os.Stat(filepath.Join(s.configDir, name))
			storageTestMust(t, err)
			pointerAfterInfo, err := os.Stat(filepath.Join(s.configDir, dataLocationName))
			storageTestMust(t, err)
			historyAfterBytes, err := os.ReadFile(filepath.Join(s.configDir, name))
			storageTestMust(t, err)
			pointerAfter, err := os.ReadFile(filepath.Join(s.configDir, dataLocationName))
			storageTestMust(t, err)
			hostsAfter, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
			storageTestMust(t, err)
			if !os.SameFile(oldConfig, configAfter) || !os.SameFile(oldHistory, historyAfter) || !os.SameFile(pointerInfo, pointerAfterInfo) || string(historyAfterBytes) != retained || !bytes.Equal(pointer, pointerAfter) || !bytes.Equal(hosts, hostsAfter) {
				t.Fatal("legacy recovery changed config/history/pointer/J4 native or bytes")
			}
			storageTestMust(t, s.storageConstruction.sources.current(t.Context()))
			response = policyCall(s, policyTestSession(t, s), http.MethodGet, "/api/storage/status", nil)
			var status storageStatusResponse
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &status) != nil || status.ReadOnly || status.Snapshot == nil {
				t.Fatal("legacy restart lost healthy actual status")
			}
		})
	}
}

// Required successful native C1 entries. These use actual New, authenticated
// demand and original fault seams. Empty installed qualification does not skip
// or turn refusal into success; platform/first-use gates remain explicit.
func TestStorageEnrollmentActualSuccessorFenceFamiliesRestart(t *testing.T) {
	paths := map[byte]bool{}
	familyPaths := map[byte]byte{}
	for family := byte(1); family <= 4; family++ {
		cuts := []string{"successor-one-fence", "successor-two-fence", "successor-entered-write", "successor-entered", "successor-armed-chunk", "successor-armed", "successor-first-prepared"}
		if family == 2 || family == 3 {
			cuts = append(cuts, "successor-stage-a-chunk", "successor-stage-a", "successor-first-reference", "successor-two-reference", "successor-stage-b-chunk")
		}
		for _, cut := range cuts {
			// Stage/R groups exist only in the actual path3 plan. The first
			// oneFence case records the real shape; both paths remain required
			// across the matrix, with no skip for missing native qualification.
			growthOnly := cut == "successor-stage-a-chunk" || cut == "successor-stage-a" || cut == "successor-first-reference" || cut == "successor-two-reference" || cut == "successor-stage-b-chunk"
			if growthOnly && familyPaths[family] != 3 {
				continue
			}
			t.Run("family"+strconv.Itoa(int(family))+"/"+cut, func(t *testing.T) {
				cfg := ownerActualConfig(t)
				cfg.JpackBin = standInRuntime(t)
				cfg.Logger = log.New(io.Discard, "", 0)
				if family == 2 || family == 3 {
					base := t.TempDir()
					configHome := filepath.Join(base, "configuration")
					parent := filepath.Join(base, "public-original-data-parent")
					storageTestMust(t, os.Mkdir(configHome, 0700))
					storageTestMust(t, os.Mkdir(parent, 0755))
					storageTestMust(t, os.Chmod(parent, 0755))
					t.Setenv("XDG_CONFIG_HOME", configHome)
					t.Setenv("XDG_DATA_HOME", filepath.Join(parent, "missing-one", "missing-two"))
					cfg.DeskConfigDir = ""
				}
				if family == 4 {
					privateFixture(t, filepath.Join(cfg.DeskConfigDir, "conversations-"+strings.Repeat("a", 64)+".json"), `{"version":1,"chats":[]}`)
				}
				s, err := New(cfg)
				storageTestMust(t, err)
				t.Cleanup(func() {
					if s != nil {
						storageTestMust(t, s.Close())
					}
				})
				if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
					t.Fatalf("required qualified original startup unavailable: %s", storageFirstUseErrorClass(s.storageProblem))
				}
				c := s.storageConstruction
				identity, err := s.inspectStorageStartupKey()
				storageTestMust(t, err)
				entered := make(chan struct{})
				sentinel := errors.New("handwritten exact successor crash cut")
				c.enrollmentFault = &storageEnrollmentFault{point: cut + "-family" + strconv.Itoa(int(family)), entered: entered, err: sentinel}
				response := policyCall(s, policyTestSession(t, s), http.MethodGet, "/api/conversations", nil)
				select {
				case <-entered:
				default:
					t.Fatal("actual requested family/cut was not reached", family, cut)
				}
				if response.Code == http.StatusOK {
					t.Fatal("interrupted demand reported success")
				}
				prior := c.ref.cell.enrollmentEndpoint
				if prior == nil {
					t.Fatal("original prior endpoint absent")
				}
				var catalogs [2][]byte
				for bank, name := range []string{"storage-owner.a", "storage-owner.b"} {
					catalogs[bank], err = os.ReadFile(filepath.Join(s.configDir, name))
					storageTestMust(t, err)
				}
				outer, _, err := storageEnrollmentPeakDecodePair(catalogs[0], catalogs[1], *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
				storageTestMust(t, err)
				paths[outer.Scope.path()] = true
				familyPaths[family] = outer.Scope.path()
				var source storageEnrollmentPeakSource
				var catalogDigest storagebudget.Digest
				var intended *storageEnrollmentSuccessorIntent
				found := false
				for _, raw := range catalogs {
					e, err := storageEnrollmentPeakDecodeEnvelope(raw, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim, true)
					storageTestMust(t, err)
					if e.Snapshot != nil && e.Snapshot.Intent != nil {
						if e.Snapshot.Intent.Family != family {
							t.Fatal("cut selected another capsule family")
						}
						source, err = storageEnrollmentPeakDecodeSource(e.Snapshot.Intent.Source)
						storageTestMust(t, err)
						intended = e.Snapshot.Intent
						catalogDigest = intended.Catalog
						found = true
					}
				}
				if !found {
					if outer.Marker == nil {
						t.Fatal("cut lost both capsule and selected packet")
					}
					raw, err := os.ReadFile(filepath.Join(s.configDir, storageEnrollmentNativeName(1+outer.Marker.Bank)))
					storageTestMust(t, err)
					e, err := storageEnrollmentPeakDecodeEvidence(raw, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
					storageTestMust(t, err)
					source = e.Board.Source
					if storageEnrollmentSuccessorFamily(e.Board.SourceKind, source) != family {
						t.Fatal("R selected another source family")
					}
					rawCatalog, err := json.Marshal(e.Inner.Catalog)
					storageTestMust(t, err)
					catalogDigest = sha256.Sum256(rawCatalog)
				}
				if source.Default == nil {
					t.Fatal("exact intended source absent")
				}
				selectedPath := string(source.Default.SelectedPath)
				selectedNative, err := os.Stat(selectedPath)
				storageTestMust(t, err)
				hosts, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
				storageTestMust(t, err)
				configPath := s.configDir
				storageTestMust(t, s.Close())
				s = nil
				s, err = New(cfg)
				storageTestMust(t, err)
				if s == nil || s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
					t.Fatal("actual C1 recovery failed required successful route", family, cut)
				}
				endpoint := s.storageConstruction.ref.cell.enrollmentEndpoint
				if endpoint == nil || endpoint.AfterEpoch <= prior.AfterEpoch || endpoint.Scope != outer.Scope || endpoint.Board.Source.Default == nil || !bytes.Equal(endpoint.Board.Source.Default.SelectedPath, source.Default.SelectedPath) {
					t.Fatal("restart changed or failed to complete exact intended Scope")
				}
				actualCatalog, err := json.Marshal(endpoint.Inner.Catalog)
				storageTestMust(t, err)
				if sha256.Sum256(actualCatalog) != catalogDigest {
					t.Fatal("restart changed intended complete catalog or original mappings")
				}
				expectedSource, err := storageEnrollmentPeakEncodeSource(source)
				storageTestMust(t, err)
				actualSource, err := storageEnrollmentPeakEncodeSource(endpoint.Board.Source)
				storageTestMust(t, err)
				if !bytes.Equal(expectedSource, actualSource) {
					t.Fatal("restart changed complete intended source")
				}
				if !storageEnrollmentPeakRetainsOrigins(*endpoint, *prior) {
					t.Fatal("restart lost complete retained origin/view/projection lineage")
				}
				if intended != nil {
					present := false
					for _, origin := range endpoint.Board.Origins {
						present = present || origin.ID == intended.Origin
					}
					if present != (intended.OriginFlags == 3) {
						t.Fatal("restart changed generated origin retention/absence", intended.OriginFlags)
					}
				}
				currentHosts, err := os.ReadFile(filepath.Join(configPath, storageIntentName))
				storageTestMust(t, err)
				if !bytes.Equal(hosts, currentHosts) {
					t.Fatal("C1 restart replayed or rewrote any of64 host pairs")
				}
				currentNative, err := os.Stat(selectedPath)
				storageTestMust(t, err)
				if !os.SameFile(selectedNative, currentNative) {
					t.Fatal("restart replaced captured/selected original source")
				}
				currentIdentity, err := s.inspectStorageStartupKey()
				storageTestMust(t, err)
				if currentIdentity != identity {
					t.Fatal("normal unseeded identity lineage changed")
				}
				storageTestMust(t, s.storageConstruction.sources.current(t.Context()))
				status := policyCall(s, policyTestSession(t, s), http.MethodGet, "/api/storage/status", nil)
				var reply storageStatusResponse
				if status.Code != http.StatusOK || json.Unmarshal(status.Body.Bytes(), &reply) != nil || reply.ReadOnly || reply.Snapshot == nil {
					t.Fatal("successful recovery did not preserve healthy actual status")
				}
			})
		}
	}
	if !paths[2] || !paths[3] {
		t.Error("required actual successor coverage did not establish both no-growth and growth paths", paths)
	}
}

func TestStorageEnrollmentActualSuccessorFenceIntentTamperRefusal(t *testing.T) {
	for family := byte(1); family <= 4; family++ {
		for _, change := range []string{"intended-catalog", "original-inputs", "changed-captured-native"} {
			t.Run("family"+strconv.Itoa(int(family))+"/"+change, func(t *testing.T) {
				cfg := ownerActualConfig(t)
				cfg.JpackBin = standInRuntime(t)
				cfg.Logger = log.New(io.Discard, "", 0)
				if family == 2 || family == 3 {
					base := t.TempDir()
					configHome := filepath.Join(base, "configuration")
					parent := filepath.Join(base, "data-parent")
					storageTestMust(t, os.Mkdir(configHome, 0700))
					storageTestMust(t, os.Mkdir(parent, 0755))
					t.Setenv("XDG_CONFIG_HOME", configHome)
					t.Setenv("XDG_DATA_HOME", filepath.Join(parent, "missing-one", "missing-two"))
					cfg.DeskConfigDir = ""
				}
				if family == 4 {
					privateFixture(t, filepath.Join(cfg.DeskConfigDir, "conversations-"+strings.Repeat("a", 64)+".json"), `{"version":1,"chats":[]}`)
				}
				s, err := New(cfg)
				storageTestMust(t, err)
				t.Cleanup(func() {
					if s != nil {
						storageTestMust(t, s.Close())
					}
				})
				if s.storageReadOnly || s.storageConstruction == nil || s.storageEnrollment == nil {
					t.Fatal("required qualified startup unavailable for actual tamper boundary")
				}
				c := s.storageConstruction
				entered := make(chan struct{})
				c.enrollmentFault = &storageEnrollmentFault{point: "successor-two-fence-family" + strconv.Itoa(int(family)), entered: entered, err: errors.New("handwritten twoFence tamper cut")}
				response := policyCall(s, policyTestSession(t, s), http.MethodGet, "/api/conversations", nil)
				select {
				case <-entered:
				default:
					t.Fatal("required family did not reach twoFence")
				}
				if response.Code == http.StatusOK {
					t.Fatal("interrupted demand reported success")
				}
				configPath := s.configDir
				raw, err := os.ReadFile(filepath.Join(configPath, "storage-owner.a"))
				storageTestMust(t, err)
				outer, err := storageEnrollmentPeakDecodeEnvelope(raw, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim, true)
				storageTestMust(t, err)
				if outer.Snapshot == nil || outer.Snapshot.Intent == nil || outer.Snapshot.Intent.Family != family {
					t.Fatal("wrong committed family")
				}
				source, err := storageEnrollmentPeakDecodeSource(outer.Snapshot.Intent.Source)
				storageTestMust(t, err)
				hosts, err := os.ReadFile(filepath.Join(configPath, storageIntentName))
				storageTestMust(t, err)
				var checkpoints, tickets [2][]byte
				for bank, suffix := range []string{"a", "b"} {
					checkpoints[bank], err = os.ReadFile(filepath.Join(configPath, storageAnchorName, "checkpoint."+suffix))
					storageTestMust(t, err)
					tickets[bank], err = os.ReadFile(filepath.Join(configPath, storageAnchorName, "tickets."+suffix))
					storageTestMust(t, err)
				}
				storageTestMust(t, s.Close())
				s = nil
				if change == "changed-captured-native" {
					path := string(source.Default.SelectedPath)
					if family == 4 {
						path = filepath.Join(configPath, dataLocationName)
						body, err := os.ReadFile(path)
						storageTestMust(t, err)
						storageTestMust(t, os.Rename(path, path+"-retained-original"))
						storageTestMust(t, os.WriteFile(path, body, 0600))
					} else {
						markerPath := filepath.Join(path, dataMarkerName)
						marker, markerErr := os.ReadFile(markerPath)
						storageTestMust(t, os.Rename(path, path+"-retained-original"))
						storageTestMust(t, os.Mkdir(path, 0700))
						if markerErr == nil {
							storageTestMust(t, os.WriteFile(markerPath, marker, 0600))
						} else if !errors.Is(markerErr, os.ErrNotExist) {
							t.Fatal(markerErr)
						}
					}
				} else {
					payload := append([]byte{}, raw[64:64+int(storageEnrollmentPeakU32(raw[24:28]))]...)
					// Handwritten capsule offset and outer checksum, with both valid
					// peers changed identically so pair selection alone cannot refuse.
					at := 4160 + 256 + 8320 + 32
					if change == "original-inputs" {
						at += 32
					}
					payload[at] ^= 1
					altered := epFrame("DSKONP1\x00", payload, storageEnrollmentPeakU64(raw[16:24]), true)
					for _, name := range []string{"storage-owner.a", "storage-owner.b"} {
						storageTestMust(t, os.WriteFile(filepath.Join(configPath, name), altered, 0600))
					}
				}
				reopened, openErr := New(cfg)
				s = reopened
				if openErr == nil && s != nil && !s.storageReadOnly && s.storageConstruction != nil && s.storageEnrollment != nil {
					t.Fatal("tampered intent/current native became active owner")
				}
				after, err := os.ReadFile(filepath.Join(configPath, storageIntentName))
				storageTestMust(t, err)
				if !bytes.Equal(after, hosts) {
					t.Fatal("refused restart replayed or repaired host bytes")
				}
				for bank, suffix := range []string{"a", "b"} {
					cp, err := os.ReadFile(filepath.Join(configPath, storageAnchorName, "checkpoint."+suffix))
					storageTestMust(t, err)
					tk, err := os.ReadFile(filepath.Join(configPath, storageAnchorName, "tickets."+suffix))
					storageTestMust(t, err)
					if !bytes.Equal(cp, checkpoints[bank]) || !bytes.Equal(tk, tickets[bank]) {
						t.Fatal("refused restart changed original Q/ticket peers")
					}
				}
			})
		}
	}
}
