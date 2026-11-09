package desk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Fixed labels and values are handwritten, not recorded from move production.
func dialectArtifactMovePlanVector(folder, previous, destination string) []byte {
	id := strings.Repeat("a", 64)
	location := func(path string) string {
		return `{"version":2,"owner":"` + id + `","kind":"file","legacySHA256":"` + id + `","folderIdentity":"` + folder + `","location":{"kind":"absolute","pathBytes":"` + base64.StdEncoding.EncodeToString([]byte(path)) + `"},"workDomain":"` + id + `"}`
	}
	return []byte(`{"version":1,"owner":"` + id + `","kind":"file","legacySHA256":"` + id + `","markerSHA256":"` + id + `","namespace":"` + id + `","nativeScopeSHA256":"` + id + `","workDomain":"` + id + `","workSHA256":"` + id + `","before":"` + base64.StdEncoding.EncodeToString([]byte(location(previous))) + `","after":"` + base64.StdEncoding.EncodeToString([]byte(location(destination))) + `"}`)
}

func TestDialectArtifactProjectMoveClosedPlanAndAcceptedEnvelope(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, vector := range []struct{ folder, previous, destination, previousJSON, destinationJSON string }{
		{"1:1", "/" + strings.Repeat("p", 412), "/" + strings.Repeat("q", 412), "/" + strings.Repeat("p", 412), "/" + strings.Repeat("q", 412)},
		{"18446744073709551615:18446744073709551615", "/" + strings.Repeat("<", 60) + strings.Repeat("p", 14), "/" + strings.Repeat("&", 60) + strings.Repeat("q", 14), "/" + strings.Repeat(`\u003c`, 60) + strings.Repeat("p", 14), "/" + strings.Repeat(`\u0026`, 60) + strings.Repeat("q", 14)},
	} {
		beforeIdentity := []byte(`{"id":"` + id + `","path":"` + vector.previousJSON + `","folder":"` + vector.folder + `"}` + "\n")
		afterIdentity := []byte(`{"id":"` + id + `","path":"` + vector.destinationJSON + `","folder":"` + vector.folder + `"}` + "\n")
		if len(beforeIdentity) != 512 || len(afterIdentity) != 512 {
			t.Fatal("handwritten accepted identity envelope", len(beforeIdentity), len(afterIdentity))
		}
		before, err := parseIdentity(beforeIdentity)
		if err != nil || before.Path != vector.previous {
			t.Fatal("accepted original identity", err)
		}
		if _, err = parseIdentity(afterIdentity); err != nil {
			t.Fatal(err)
		}
		planRaw := dialectArtifactMovePlanVector(vector.folder, vector.previous, vector.destination)
		var plan dialectArtifactProjectMovePlan
		if err = json.Unmarshal(planRaw, &plan); err != nil {
			t.Fatal(err)
		}
		// The unchanged ORIGINAL intent itself has the analytical1050-F bound.
		original := `{"version":1,"previous":"` + vector.previousJSON + `","destination":"` + vector.destinationJSON + `","folder":"` + vector.folder + `","identity":"` + id + `","configRevision":"` + id + `"}`
		if len(original) != 1050-len(vector.folder) {
			t.Fatal("original bound drift", len(original))
		}
		intentRaw := []byte(strings.TrimSuffix(original, "}") + `,"retained":` + string(planRaw) + `}`)
		if len(intentRaw) > 5898 || len(intentRaw) > 16<<10 {
			t.Fatal("accepted move domain grew past proven intent bound", len(intentRaw))
		}
		root, err := os.OpenRoot(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err = claimPrivateData(root, projectMoveName, intentRaw); err != nil {
			t.Fatal(err)
		}
		if _, found, err := readProjectMove(root); err != nil || !found {
			t.Fatal("closed optional intent refused", err)
		}
		root.Close()
		for _, mutation := range []string{
			strings.Replace(string(planRaw), `"version":1`, `"version":1,"version":1`, 1),
			strings.Replace(string(planRaw), `"owner":`, `"Owner":`, 1),
			strings.Replace(string(planRaw), `"kind":"file"`, `"kind":"injected"`, 1),
			strings.Replace(string(planRaw), `"before":"`, `"before":null,"unknown":"`, 1),
			strings.Replace(string(planRaw), `"version":1,`, "", 1),
			string(planRaw) + " ",
		} {
			if _, err := readDialectArtifactProjectMovePlan([]byte(mutation)); err == nil {
				t.Fatal("closed mutation accepted")
			}
		}
	}
	// Long accepted work provenance never enters the move carrier. Its full
	// canonical path envelope remains supported independently of identity512.
	workPath := []byte("/" + strings.Repeat("x", 4095))
	workID := dialectArtifactWorkDomainID("1:2", "1:3")
	work := []byte(`{"version":1,"id":"` + workID + `","kind":"settings","pathBytes":"` + base64.StdEncoding.EncodeToString(workPath) + `","folderIdentity":"1:2","lockIdentity":"1:3"}`)
	if _, err := readDialectArtifactWorkDomainRecord(work); err != nil {
		t.Fatal("unchanged long work provenance was narrowed", err)
	}
}

func TestDialectArtifactProjectMoveLegacyDecoderAtLimit(t *testing.T) {
	id := strings.Repeat("a", 64)
	base := `{"version":1,"previous":"/old","destination":"/next","folder":"1:1","identity":"` + id + `","configRevision":""}`
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	raw := []byte(base + strings.Repeat(" ", (16<<10)-len(base)))
	if err = claimPrivateData(root, projectMoveName, raw); err != nil {
		t.Fatal(err)
	}
	if _, found, err := readProjectMove(root); err != nil || !found {
		t.Fatal("legacy16KiB acceptance changed", err)
	}
	for _, extra := range []string{`,"retained":null`, `,"Retained":null`} {
		if err = writePrivateData(root, projectMoveName, []byte(strings.TrimSuffix(base, "}")+extra+`}`)); err != nil {
			t.Fatal(err)
		}
		if _, _, err = readProjectMove(root); err == nil {
			t.Fatal("null optional retained control accepted")
		}
	}
}

func enrollDialectArtifactMoveServer(t *testing.T, s *Server) dialectArtifactOwnerEnrollment {
	t.Helper()
	token, err := s.sessions.createExpiring("move-test-owner", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "http://localhost/private-retained-move", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	value, err := s.enrollDialectArtifactOwner(r)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestDialectArtifactProjectMoveNativeRenameRecovery(t *testing.T) {
	requireStorageLine(t)
	for _, stop := range []string{"none", "before-rename", "after-rename", "move-location.after-cas", "move-queue.close"} {
		t.Run(stop, func(t *testing.T) {
			// Establish the real project identity before New can discover or
			// admit its workspace. Never replace a key after charged work.
			project := t.TempDir()
			identity := strings.Repeat("c", 64)
			if err := os.Mkdir(filepath.Join(project, startupIdentityDir), 0700); err != nil {
				t.Fatal(err)
			}
			privateFixture(t, filepath.Join(project, startupIdentityDir, startupIdentityName), identityLine(identity, project, ""))
			s, ts := startDesk(t, Config{
				ProjectDir: project, JpackBin: standInRuntime(t), Token: testToken,
				Logger: log.New(&bytes.Buffer{}, "", 0), DeskConfigDir: t.TempDir(),
			})
			t.Cleanup(func() { s.Close() })
			t.Cleanup(ts.Close)
			if key, err := s.inspectStorageStartupKey(); err != nil || key != identity {
				t.Fatal("original fixture identity was not discovered", err)
			}
			c := s.storageConstruction
			if c == nil || c.sources == nil || c.ref == nil {
				t.Fatal("original workspace discovery missing")
			}
			var originalWorkspace storagebudget.RegisteredWorkspace
			workspaceCount := 0
			for _, w := range c.sources.workspaces {
				if bytes.Equal(w.OwnerKey, []byte(identity)) && bytes.Equal(w.HistoryIdentity, []byte(project)) {
					originalWorkspace = w.Definition
					workspaceCount++
				}
			}
			if workspaceCount != 1 {
				t.Fatal("original fixture workspace identity is not unique")
			}
			if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
				t.Fatal(code)
			}
			owner := enrollDialectArtifactMoveServer(t, s)
			ownerPath, scopePath := dialectArtifactOwnerRowPaths(s, owner)
			originalOwner, originalScope := dialectArtifactOwnerRead(t, ownerPath), dialectArtifactOwnerRead(t, scopePath)
			markerPath := filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker)
			originalMarker := dialectArtifactOwnerRead(t, markerPath)
			raw := []byte("retained exact bytes: <original>\n\x00")
			ref, err := s.artifactRepository().Put(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}
			// Prove that admission retained the very workspace/key discovered
			// before the first chat, rather than a newly stamped replacement.
			if key, err := s.inspectStorageStartupKey(); err != nil || key != identity {
				t.Fatal("original fixture identity changed before move", err)
			}
			c.ref.cell.mu.Lock()
			admittedWorkspaces := append([]storageOwnerWorkspace(nil), c.ref.cell.ownerCatalog.Workspaces...)
			c.ref.cell.mu.Unlock()
			admittedCount := 0
			for _, w := range admittedWorkspaces {
				if w.Definition.ID == originalWorkspace.ID {
					if w.Definition.Generation != originalWorkspace.Generation || !bytes.Equal(w.OwnerKey, []byte(identity)) || !bytes.Equal(w.HistoryIdentity, []byte(project)) {
						t.Fatal("admitted fixture workspace identity changed before move")
					}
					admittedCount++
				}
			}
			if admittedCount != 1 {
				t.Fatal("original fixture workspace was not uniquely admitted")
			}
			destination := filepath.Join(t.TempDir(), "next")
			request := movePreview(t, ts, destination)
			injected := errors.New("retained move interrupted")
			var failedCloseEpoch *storageEnrollmentEpoch
			var cutReached atomic.Bool
			testProjectMoveStep = func(at string) error {
				if at == stop {
					cutReached.Store(true)
					return injected
				}
				return nil
			}
			testHookDialectArtifactOwnerStep = func(at string) error {
				if at == stop {
					cutReached.Store(true)
					if stop == "move-queue.close" {
						g := &s.storageConstruction.owner.admission
						g.mu.Lock()
						failedCloseEpoch = g.enrollment
						g.mu.Unlock()
					}
					return injected
				}
				return nil
			}
			t.Cleanup(func() { testProjectMoveStep = nil; testHookDialectArtifactOwnerStep = nil })
			code, response := moveRequest(t, ts, "move", request)
			testProjectMoveStep, testHookDialectArtifactOwnerStep = nil, nil
			if stop != "none" && !cutReached.Load() {
				t.Fatal("requested move cut was not reached", stop)
			}
			if stop == "move-queue.close" {
				p := failedCloseEpoch
				if p == nil {
					t.Fatal("failed original hold Close lost its epoch")
				}
				select {
				case <-p.done:
				default:
					t.Fatal("original move epoch did not drain")
				}
				if p.identity == nil || p.identity.finished || p.identityDone || p.reconciled || p.identityLaunch == nil || p.identityPlan == nil {
					t.Fatal("failed original hold Close completed the move")
				}
				for _, application := range p.identityLaunch.applications {
					if application.plan == p.identityPlan.PlanID && application.action == storageIdentityIntentMove && application.complete {
						t.Fatal("failed Close completed the factual move application")
					}
				}
				if p.err == nil || s.storageConstruction.owner.admissionOpen() {
					t.Fatal("failed Close restored successful admission")
				}
				if _, found, e := readProjectMove(s.assistant.root); e != nil || !found {
					t.Fatal("failed Close lost original recovery intent", e)
				}
			}
			next := destination
			if stop == "before-rename" {
				next = s.projectDir
				if code != 409 {
					t.Fatal(code, string(response))
				}
			} else if code != 200 {
				t.Fatal(code, string(response))
			}
			again, _, _ := startedAt(t, s, next)
			// Read the existing outer startup result once. A successful New
			// may intentionally return read-only and skip move recovery. This
			// diagnostic never calls replay/finish, changes fields or retries.
			startupErr := again.storageProblem
			usable := again.assistant != nil && again.assistant.usable()
			managed := again.cfg.deskID != ""
			expectedProject := again.projectDir == next
			t.Logf("move-restart readonly=%t enrolled=%t usable=%t managed=%t projectMatches=%t error=%t qualification=%t ownerRecovery=%t custody=%t bootstrap=%t infrastructure=%t authority=%t stale=%t busy=%t unknown=%t bounds=%t cancelled=%t deadline=%t",
				again.storageReadOnly, again.storageEnrollment != nil, usable, managed, expectedProject, startupErr != nil,
				errors.Is(startupErr, errStorageEnrollmentQualification), errors.Is(startupErr, errStorageOwnerRecovery),
				errors.Is(startupErr, errStorageCustody), errors.Is(startupErr, errStorageBootstrap), errors.Is(startupErr, errStorageInfrastructure),
				errors.Is(startupErr, storagebudget.ErrAuthority), errors.Is(startupErr, storagebudget.ErrStale),
				errors.Is(startupErr, storagebudget.ErrBusy), errors.Is(startupErr, storagebudget.ErrUnknown), errors.Is(startupErr, storagebudget.ErrBounds),
				errors.Is(startupErr, context.Canceled), errors.Is(startupErr, context.DeadlineExceeded))
			if again.storageReadOnly || !usable || managed || !expectedProject {
				t.Fatal("STARTUP_NOT_RECOVERY_READY")
			}
			if again.locationStatus().Problem != "" {
				t.Fatal(again.locationStatus())
			}
			if !bytes.Equal(dialectArtifactOwnerRead(t, ownerPath), originalOwner) || !bytes.Equal(dialectArtifactOwnerRead(t, scopePath), originalScope) {
				t.Fatal("immutable provenance changed")
			}
			if !bytes.Equal(dialectArtifactOwnerRead(t, filepath.Join(next, ".desk-private", dialectArtifactOwnerMarker)), originalMarker) {
				t.Fatal("physical owner marker changed")
			}
			location, err := readDialectArtifactLocationRecord(dialectArtifactOwnerRead(t, dialectArtifactLocationPath(again, owner.Owner)))
			if err != nil || string(location.Location.PathBytes) != next {
				t.Fatal("current locator was not recovered", location, err)
			}
			got, err := again.artifactRepository().Get(context.Background(), ref)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatal("retained exact bytes lost", err)
			}
			if _, err = os.Lstat(filepath.Join(again.configDir, projectMoveName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("completed intent retained", err)
			}
		})
	}
}

func TestDialectArtifactRecognizedExternalMove(t *testing.T) {
	t.Skip(projectFollowedOutsideDesk)
	s, ts, _ := assistantServer(t)
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	stampMoveIdentity(t, s)
	owner := enrollDialectArtifactMoveServer(t, s)
	for i := 0; i < 2; i++ {
		next := s.projectDir + "-moved"
		if err := os.Rename(s.projectDir, next); err != nil {
			t.Fatal(err)
		}
		s, ts, _ = startedAt(t, s, next)
		if s.locationStatus().Problem != "" {
			t.Fatal(s.locationStatus())
		}
		location, err := readDialectArtifactLocationRecord(dialectArtifactOwnerRead(t, dialectArtifactLocationPath(s, owner.Owner)))
		if err != nil || string(location.Location.PathBytes) != next {
			t.Fatal("recognized rename lost locator", err)
		}
		if code, reply := chatRequest(t, ts, "GET", "", "", true, ""); code != 200 || reply.SHA256 == "absent" {
			t.Fatal("recognized rename lost original history", code)
		}
	}
}

// Missing pre-existing custody must refuse before owning workflow startup even
// though there is no recovery journal yet. Restore the same lock inode to retry.
func TestDialectArtifactRecognizedMoveMissingMarkerLockRefusesStartup(t *testing.T) {
	t.Skip(projectFollowedOutsideDesk)
	s, ts, _ := assistantServer(t)
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	stampMoveIdentity(t, s)
	owner := enrollDialectArtifactMoveServer(t, s)
	previous, next := s.projectDir, s.projectDir+"-external"
	locationPath := dialectArtifactLocationPath(s, owner.Owner)
	data, err := s.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	dataPath := data.root.Name()
	namespace, err := resolveConversationName(data.root, previous)
	data.root.Close()
	if err != nil {
		t.Fatal(err)
	}
	cfg := s.cfg
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(previous, next); err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(next, startupIdentityDir, startupIdentityName)
	identityBefore := dialectArtifactOwnerRead(t, identityPath)
	locationBefore := dialectArtifactOwnerRead(t, locationPath)
	historySnapshot := func() map[string][]byte {
		t.Helper()
		entries, err := os.ReadDir(dataPath)
		if err != nil {
			t.Fatal(err)
		}
		rows := make(map[string][]byte)
		for _, entry := range entries {
			if historyMember(entry.Name(), namespace) || entry.Name() == projectBindingsName {
				rows[entry.Name()] = dialectArtifactOwnerRead(t, filepath.Join(dataPath, entry.Name()))
			}
		}
		return rows
	}
	historyBefore := historySnapshot()
	lockPath := filepath.Join(next, ".desk-private", ".dialect-artifact-owner.lock")
	heldLockPath := lockPath + ".held-for-test"
	if err = os.Rename(lockPath, heldLockPath); err != nil {
		t.Fatal(err)
	}
	cfg.Root, cfg.ProjectDir = nil, next
	failed, err := New(cfg)
	if failed != nil {
		failed.Close()
		t.Fatal("missing retained custody returned a server and reached owning workflow startup")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing retained marker lock did not refuse startup with its custody failure", err)
	}
	if _, err = os.Lstat(filepath.Join(cfg.DeskConfigDir, dialectArtifactRecognizedMoveName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fixture did not fail before journal creation", err)
	}
	if _, err = os.Lstat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("startup recreated the missing enrolled lock", err)
	}
	if !bytes.Equal(dialectArtifactOwnerRead(t, identityPath), identityBefore) || !bytes.Equal(dialectArtifactOwnerRead(t, locationPath), locationBefore) {
		t.Fatal("pre-journal refusal changed identity or locator")
	}
	historyAfter := historySnapshot()
	if len(historyAfter) != len(historyBefore) {
		t.Fatal("pre-journal refusal changed history membership")
	}
	for name, before := range historyBefore {
		if !bytes.Equal(historyAfter[name], before) {
			t.Fatal("pre-journal refusal changed history", name)
		}
	}
	if err = os.Rename(heldLockPath, lockPath); err != nil {
		t.Fatal(err)
	}
	again, err := New(cfg)
	if err != nil {
		t.Fatal("restoring exact custody did not recover the recognized move", err)
	}
	defer again.Close()
	identity, err := parseIdentity(dialectArtifactOwnerRead(t, identityPath))
	if err != nil || identity.Path != next {
		t.Fatal("recovered identity did not acknowledge the move", err)
	}
	location, err := readDialectArtifactLocationRecord(dialectArtifactOwnerRead(t, locationPath))
	if err != nil || string(location.Location.PathBytes) != next || again.locationStatus().Problem != "" {
		t.Fatal("recovered locator did not acknowledge the move", err)
	}
	data, err = again.openChatData()
	if err != nil {
		t.Fatal(err)
	}
	recoveredNamespace, err := resolveConversationName(data.root, next)
	data.root.Close()
	if err != nil || recoveredNamespace != namespace {
		t.Fatal("recovered move did not retain the history namespace", err)
	}
	againHTTP := httptest.NewServer(again)
	defer againHTTP.Close()
	if code, reply := chatRequest(t, againHTTP, "GET", "", "", true, ""); code != 200 || reply.SHA256 == "absent" {
		t.Fatal("recovered move lost original history", code)
	}
}

func TestDialectArtifactRecognizedMoveUncertainCASRestartsBeforeIdentity(t *testing.T) {
	t.Skip(projectFollowedOutsideDesk)
	s, ts, _ := assistantServer(t)
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	stampMoveIdentity(t, s)
	owner := enrollDialectArtifactMoveServer(t, s)
	previous, next := s.projectDir, s.projectDir+"-external"
	cfg := s.cfg
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(previous, next); err != nil {
		t.Fatal(err)
	}
	cfg.Root, cfg.ProjectDir = nil, next
	testHookDialectArtifactOwnerStep = func(at string) error {
		if at == "move-location.after-cas" {
			return errors.New("uncertain acknowledged publication")
		}
		return nil
	}
	t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
	failed, err := New(cfg)
	testHookDialectArtifactOwnerStep = nil
	if err == nil || failed != nil {
		if failed != nil {
			failed.Close()
		}
		t.Fatal("incomplete recognized move started workers")
	}
	if _, err = os.Lstat(filepath.Join(cfg.DeskConfigDir, dialectArtifactRecognizedMoveName)); err != nil {
		t.Fatal("uncertain move lost its journal", err)
	}
	again, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	location, err := readDialectArtifactLocationRecord(dialectArtifactOwnerRead(t, dialectArtifactLocationPath(again, owner.Owner)))
	if err != nil || string(location.Location.PathBytes) != next {
		t.Fatal("exact recorded CAS not replayed", err)
	}
	if _, err = os.Lstat(filepath.Join(cfg.DeskConfigDir, dialectArtifactRecognizedMoveName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed recognized intent not removed", err)
	}
}

func TestDialectArtifactProjectMoveLongUnchangedWorkProvenance(t *testing.T) {
	config := t.TempDir()
	for range 10 {
		config = filepath.Join(config, strings.Repeat("q", 60))
	}
	if len(config) <= startupIdentityLimit {
		t.Fatal("fixture did not exceed identity path limit")
	}
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	s, ts, _ := assistantServerIn(t, config)
	stampMoveIdentity(t, s)
	// The existing private work-lock opener admits regular owner-only files
	// regardless of payload length. A move may not add an empty-file policy.
	if err := os.WriteFile(filepath.Join(s.configDir, ".data-work.lock"), []byte("existing lock payload"), 0600); err != nil {
		t.Fatal(err)
	}
	owner := enrollDialectArtifactMoveServer(t, s)
	workPath := dialectArtifactWorkDomainPath(s, owner.WorkDomain)
	before := dialectArtifactOwnerRead(t, workPath)
	destination := filepath.Join(t.TempDir(), "next")
	request := movePreview(t, ts, destination)
	if code, raw := moveRequest(t, ts, "move", request); code != 200 {
		t.Fatal(code, string(raw))
	}
	if !bytes.Equal(dialectArtifactOwnerRead(t, workPath), before) {
		t.Fatal("unchanged long work row was rewritten")
	}
	again, _, _ := startedAt(t, s, destination)
	if again.locationStatus().Problem != "" {
		t.Fatal(again.locationStatus())
	}
}

func TestDialectArtifactProjectMoveMetadataChangeRefusesBeforeRename(t *testing.T) {
	s, ts, _ := assistantServer(t)
	stampMoveIdentity(t, s)
	owner := enrollDialectArtifactMoveServer(t, s)
	destination := filepath.Join(t.TempDir(), "next")
	request := movePreview(t, ts, destination)
	locationPath := dialectArtifactLocationPath(s, owner.Owner)
	row, err := readDialectArtifactLocationRecord(dialectArtifactOwnerRead(t, locationPath))
	if err != nil {
		t.Fatal(err)
	}
	row.Location.PathBytes = []byte(s.projectDir + "-unreviewed")
	changed, err := marshalDialectArtifactLocation(row)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(locationPath, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if code, _ := moveRequest(t, ts, "move", request); code != 409 {
		t.Fatal("stale locator accepted", code)
	}
	if _, err = os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale move renamed project", err)
	}
	if _, err = os.Lstat(filepath.Join(s.configDir, projectMoveName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale move claimed intent", err)
	}
}
