package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// Real pinned project, private store, session authorization and namespace.
// No New, HTTP listener, factory, Runtime, model, provider or installed binary.
func dialectArtifactOwnerFixture(t *testing.T, projectPath, configPath string) (*Server, *http.Request) {
	t.Helper()
	return dialectArtifactOwnerFixtureWithWorkLock(t, projectPath, configPath, true)
}

func dialectArtifactOwnerFixtureWithWorkLock(t *testing.T, projectPath, configPath string, initializeWorkLock bool) (*Server, *http.Request) {
	t.Helper()
	return dialectArtifactOwnerFixtureWithStorage(t, projectPath, configPath, initializeWorkLock, true)
}

// Home fixtures establish their actual binding before creating contained
// storage. In particular, do not serialize a raw non-UTF8 config path into a
// legacy JSON pointer as an intermediate setup step.
func dialectArtifactOwnerFixtureWithStorage(t *testing.T, projectPath, configPath string, initializeWorkLock, legacyData bool) (*Server, *http.Request) {
	t.Helper()
	if !custodyChecked {
		t.Skip("existing private custody is unavailable")
	}
	for _, path := range []string{projectPath, configPath} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	project, err := OpenProjectRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = project.Close() })
	private := openAssistantStore(configPath)
	if !private.usable() {
		t.Fatal(private.problem)
	}
	t.Cleanup(func() { _ = private.Close() })
	sessions, err := newSessionStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sessions.close)
	s := &Server{cfg: Config{DeskConfigDir: configPath, Token: "launch-only"}, configDir: configPath,
		project: project, root: project.own.root, projectDir: project.dir,
		assistant: private, writes: &sync.Mutex{}, sessions: sessions}
	token, err := sessions.createExpiring("local user", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "http://localhost/private-native-enrollment", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	// Preserve a real legacy chat root and initialize its pointer. Ordinary
	// fixtures also pre-create the work lock; the durability fixture leaves
	// that inode for enrollment to create against the existing pointer.
	if legacyData {
		if err := writePrivateData(private.root, s.conversationName(), []byte(`{"version":1,"chats":[]}`)); err != nil {
			t.Fatal(err)
		}
	}
	if initializeWorkLock {
		work, err := s.privateDataFileLock(".data-work.lock", true)
		if err != nil {
			t.Fatal(err)
		}
		if err = work.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if !legacyData {
		return s, r
	}
	lock, err := s.privateDataLockContext(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	data, openErr := s.openChatData()
	if openErr == nil {
		if !data.legacy {
			t.Fatal("fixture did not select its actual legacy root")
		}
		openErr = data.root.Close()
	}
	if err = errors.Join(openErr, lock.Close()); err != nil {
		t.Fatal(err)
	}
	return s, r
}

func dialectArtifactOwnerSetup(t *testing.T) (*Server, *http.Request) {
	t.Helper()
	return dialectArtifactOwnerFixture(t, filepath.Join(t.TempDir(), "project"), filepath.Join(t.TempDir(), "config"))
}

func dialectArtifactOwnerTablePath(s *Server, parts ...string) string {
	base := []string{s.deskOwner().configDir, secretsDirName, dialectArtifactOwnersFolder}
	return filepath.Join(append(base, parts...)...)
}

func dialectArtifactOwnerRowPaths(s *Server, v dialectArtifactOwnerEnrollment) (string, string) {
	return dialectArtifactOwnerTablePath(s, "owners", v.Owner[:2], v.Owner[2:]+".json"),
		dialectArtifactOwnerTablePath(s, "native", v.Namespace, v.Owner+".json")
}

func dialectArtifactOwnerRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func dialectArtifactOwnerNames(t *testing.T, root *os.Root) []string {
	t.Helper()
	entries, err := storageEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func dialectArtifactOwnerNoEffects(t *testing.T, s *Server) {
	t.Helper()
	if _, err := s.root.Lstat(retainedArtifactDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("enrollment touched artifact publication", err)
	}
	if _, err := s.assistant.root.Lstat("assistant-runs-" + digestOf([]byte(s.conversationIdentity())) + ".json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("enrollment created a run or intent", err)
	}
}

func TestDialectArtifactOwnerNativeProducer(t *testing.T) {
	s, r := dialectArtifactOwnerSetup(t)
	before := dialectArtifactOwnerNames(t, s.assistant.root)
	first, err := s.enrollDialectArtifactOwner(r)
	if err != nil {
		t.Fatal(err)
	}
	if first.Namespace != digestOf([]byte(s.conversationIdentity())) || !assistantRunDigest(first.Owner) {
		t.Fatal("producer did not bind actual namespace/root", first)
	}
	ownerPath, scopePath := dialectArtifactOwnerRowPaths(s, first)
	rowRaw, scopeRaw := dialectArtifactOwnerRead(t, ownerPath), dialectArtifactOwnerRead(t, scopePath)
	row, err := readDialectArtifactOwnerRecord(rowRaw)
	if err != nil || row.Owner != first.Owner || !bytes.Equal(row.PathBytes, []byte(s.projectDir)) ||
		!bytes.Equal(row.Work.PathBytes, []byte(s.configDir)) || row.Work.Kind != "settings" {
		t.Fatal("wrong actual project/work locator", row, err)
	}
	workRoot, err := s.assistant.root.Stat(".")
	if err != nil || row.Work.FolderIdentity != identityKey(workRoot) {
		t.Fatal("work domain did not follow actual assistant root", err)
	}
	scope, err := readDialectArtifactOwnerScope(scopeRaw)
	if err != nil || scope.Kind != "native" || scope.Namespace != first.Namespace || scope.Owner != first.Owner {
		t.Fatal("invalid native scope", scope, err)
	}
	if digestOf(rowRaw) != first.LegacyOwnerSHA256 || digestOf(scopeRaw) != first.ScopeSHA256 {
		t.Fatal("returned provenance does not name exact durable rows")
	}
	second, err := s.enrollDialectArtifactOwner(r)
	if err != nil || second != first {
		t.Fatal("repeat enrollment changed owner/scope", second, err)
	}
	if !reflect.DeepEqual(before, dialectArtifactOwnerNames(t, s.assistant.root)) {
		t.Fatal("registry consumed legacy chat enumeration entries")
	}
	for _, path := range []string{ownerPath, scopePath, filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker)} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("enrollment file custody", path, err)
		}
	}
	dialectArtifactOwnerNoEffects(t, s)
}

func TestDialectArtifactOwnerPublishesLocatorBeforeMarker(t *testing.T) {
	s, r := dialectArtifactOwnerSetup(t)
	var order []string
	testHookDialectArtifactOwnerStep = func(step string) error {
		switch step {
		case "owner.readback":
			if _, err := s.root.Lstat(".desk-private/" + dialectArtifactOwnerMarker); !errors.Is(err, os.ErrNotExist) {
				return errors.New("marker preceded durable owner row")
			}
			order = append(order, step)
		case "marker.publish":
			marker, err := readDialectArtifactOwnerMarker(dialectArtifactOwnerRead(t, filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker)))
			if err != nil {
				return err
			}
			row := dialectArtifactOwnerRead(t, dialectArtifactOwnerTablePath(s, "owners", marker.Owner[:2], marker.Owner[2:]+".json"))
			if _, err := readDialectArtifactOwnerRecord(row); err != nil {
				return err
			}
			order = append(order, step)
		case "scope.publish":
			order = append(order, step)
		}
		return nil
	}
	t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
	if _, err := s.enrollDialectArtifactOwner(r); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"owner.readback", "marker.publish", "scope.publish"}) {
		t.Fatal("unexpected durable enrollment order", order)
	}
}

func TestDialectArtifactOwnerDoesNotReconstructMissingLocator(t *testing.T) {
	s, r := dialectArtifactOwnerSetup(t)
	v, err := s.enrollDialectArtifactOwner(r)
	if err != nil {
		t.Fatal(err)
	}
	ownerPath, scopePath := dialectArtifactOwnerRowPaths(s, v)
	scopeBefore := dialectArtifactOwnerRead(t, scopePath)
	if err = os.Remove(ownerPath); err != nil {
		t.Fatal(err)
	}
	if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) {
		t.Fatal("marker reconstructed missing owner identity", got, err)
	}
	if _, err := os.Lstat(ownerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing locator was repaired", err)
	}
	if !bytes.Equal(scopeBefore, dialectArtifactOwnerRead(t, scopePath)) {
		t.Fatal("existing scope changed during refusal")
	}
}

func TestDialectArtifactOwnerActualAlias(t *testing.T) {
	s, r := dialectArtifactOwnerSetup(t)
	namespace := strings.Repeat("b", 64)
	conversation := "conversations-" + namespace + ".json"
	if err := writePrivateData(s.assistant.root, conversation, []byte(`{"version":1,"chats":[]}`)); err != nil {
		t.Fatal(err)
	}
	longAlias := "/trusted/" + strings.Repeat("long-history-path", 80)
	bindings, err := json.Marshal(projectBindings{Version: 1, Projects: map[string]string{s.conversationIdentity(): conversation, longAlias: conversation}})
	if err != nil {
		t.Fatal(err)
	}
	if err = writePrivateData(s.assistant.root, projectBindingsName, bindings); err != nil {
		t.Fatal(err)
	}
	v, err := s.enrollDialectArtifactOwner(r)
	if err != nil || v.Namespace != namespace {
		t.Fatal("producer ignored actual saved namespace alias", v, err)
	}
	ownerPath, scopePath := dialectArtifactOwnerRowPaths(s, v)
	row, err := readDialectArtifactOwnerRecord(dialectArtifactOwnerRead(t, ownerPath))
	if err != nil || !bytes.Equal(row.PathBytes, []byte(s.projectDir)) {
		t.Fatal("history alias became an owner locator", row, err)
	}
	if len(dialectArtifactOwnerRead(t, scopePath)) != 183 {
		t.Fatal("alias keys expanded a fixed scope row")
	}
}

func TestDialectArtifactOwnerLongExactPaths(t *testing.T) {
	base := t.TempDir()
	project, config := filepath.Join(base, "project"), filepath.Join(base, "config")
	component := strings.Repeat("q", 60) + "\"\\\n" + "é"
	for range 8 {
		project = filepath.Join(project, component)
		config = filepath.Join(config, component)
	}
	if runtime.GOOS == "linux" {
		project = filepath.Join(project, string([]byte{'r', 0xff}))
	}
	s, r := dialectArtifactOwnerFixture(t, project, config)
	v, err := s.enrollDialectArtifactOwner(r)
	if err != nil {
		t.Fatal("legal native paths were narrowed by metadata encoding", err)
	}
	ownerPath, _ := dialectArtifactOwnerRowPaths(s, v)
	row, err := readDialectArtifactOwnerRecord(dialectArtifactOwnerRead(t, ownerPath))
	if err != nil || !bytes.Equal(row.PathBytes, []byte(s.projectDir)) || !bytes.Equal(row.Work.PathBytes, []byte(s.configDir)) {
		t.Fatal("filesystem path bytes changed", err)
	}
	if len(row.PathBytes) <= startupIdentityLimit || len(row.Work.PathBytes) <= startupIdentityLimit {
		t.Fatal("test did not exceed unrelated startup identity envelope")
	}
}

func TestDialectArtifactOwnerMetadataEnvelope(t *testing.T) {
	id := strings.Repeat("f", 64)
	identity := "18446744073709551615:18446744073709551615"
	path := append([]byte{'/'}, bytes.Repeat([]byte{'x'}, 4095)...)
	maximum := dialectArtifactOwnerRecord{Version: 1, Owner: id, Kind: "file", PathBytes: path, FolderIdentity: identity,
		Work: dialectArtifactWorkOwner{Kind: "settings", ID: id, PathBytes: path, FolderIdentity: identity}}
	raw, err := json.Marshal(maximum)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 11281 {
		t.Fatal("two base64 paths were not fully accounted", len(raw))
	}
	if dialectArtifactOwnerRecordBytes != 11281 {
		t.Fatal("declared owner record bound changed", dialectArtifactOwnerRecordBytes)
	}
	if _, err = readDialectArtifactOwnerRecord(raw); err != nil {
		t.Fatal("maximum declared metadata carrier refused", err)
	}
	maximum.PathBytes = append(maximum.PathBytes, 'x')
	over, _ := json.Marshal(maximum)
	if _, err = readDialectArtifactOwnerRecord(over); err == nil {
		t.Fatal("one-over raw locator accepted")
	}
	native := dialectArtifactOwnerScope{Version: 1, Kind: "native", Owner: id, Namespace: id}
	imported := native
	imported.Kind, imported.RunID = "import", "arun_"+strings.Repeat("f", 32)
	for _, test := range []struct {
		value dialectArtifactOwnerScope
		bytes int
	}{{native, 183}, {imported, 231}} {
		encoded, err := json.Marshal(test.value)
		if err != nil || len(encoded) != test.bytes {
			t.Fatal("scope bound changed", len(encoded), err)
		}
		if _, err := readDialectArtifactOwnerScope(encoded); err != nil {
			t.Fatal(err)
		}
	}
	marker, _ := json.Marshal(dialectArtifactOwnerMarkerRecord{Version: 1, Owner: id})
	if len(marker) != 88 {
		t.Fatal("marker bound changed", len(marker))
	}
	if _, err := readDialectArtifactOwnerMarker(marker); err != nil {
		t.Fatal(err)
	}
	for _, identity := range []string{"0:0", "-2147483648:18446744073709551615", "-9223372036854775808:9223372036854775807"} {
		if !dialectArtifactOwnerFileIdentity(identity) {
			t.Fatal("native signed/unsigned identity was narrowed", identity)
		}
	}
	for _, identity := range []string{"", ":1", "+1:1", "01:1", "-0:1", "18446744073709551616:1", "1:2:3"} {
		if dialectArtifactOwnerFileIdentity(identity) {
			t.Fatal("non-native identity spelling accepted", identity)
		}
	}
	// Invalid UTF-8, newlines, quotes and backslashes are byte data, not
	// paths supplied by a caller; only the pinned producer can enroll one.
	exact := []byte{'/', 'q', '"', '\\', '\n', 0xff}
	maximum.PathBytes, maximum.Work.PathBytes = exact, exact
	encoded, _ := json.Marshal(maximum)
	decoded, err := readDialectArtifactOwnerRecord(encoded)
	if err != nil || !bytes.Equal(decoded.PathBytes, exact) {
		t.Fatal("byte locator underwent JSON string normalization", err)
	}
	valid := []byte(`{"version":1,"kind":"native","owner":"` + id + `","namespace":"` + id + `"}`)
	for _, mutation := range [][]byte{
		append(append([]byte{}, valid...), '\n'),
		bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		bytes.Replace(valid, []byte(`"kind"`), []byte(`"Kind"`), 1),
		bytes.Replace(valid, []byte(`"native"`), []byte(`null`), 1),
		bytes.Replace(valid, []byte(`"namespace"`), []byte(`"extra"`), 1),
		bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":1.0`), 1),
		bytes.Replace(valid, []byte(`"native"`), []byte(`"import"`), 1),
	} {
		if _, err := readDialectArtifactOwnerScope(mutation); err == nil {
			t.Fatal("noncanonical or invalid local controls accepted", string(mutation))
		}
	}
}

func TestDialectArtifactOwnerNoAggregateRegistryCap(t *testing.T) {
	s, r := dialectArtifactOwnerSetup(t)
	owners := dialectArtifactOwnerTablePath(s, "owners", "ff")
	scopes := dialectArtifactOwnerTablePath(s, "native", strings.Repeat("e", 64))
	for _, path := range []string{owners, scopes} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Empty irrelevant rows prove no enumeration; a separate blob proves
	// the table's aggregate size is not an admission quota.
	for i := 0; i <= 4096; i++ {
		for _, path := range []string{filepath.Join(owners, fmt.Sprintf("%062x.json", i)), filepath.Join(scopes, fmt.Sprintf("%064x.json", i))} {
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(scopes, "unselected-large"), bytes.Repeat([]byte{'x'}, (1<<20)+1), 0600); err != nil {
		t.Fatal(err)
	}
	before := dialectArtifactOwnerNames(t, s.assistant.root)
	if _, err := s.enrollDialectArtifactOwner(r); err != nil {
		t.Fatal("unselected metadata count/bytes narrowed enrollment", err)
	}
	if !reflect.DeepEqual(before, dialectArtifactOwnerNames(t, s.assistant.root)) {
		t.Fatal("new registry table consumed legacy chat entries")
	}
}

func TestDialectArtifactOwnerRefusesChangedMetadata(t *testing.T) {
	for _, change := range []string{"owner-body", "scope-body", "owner-link", "scope-link", "owner-mode", "scope-mode", "copied-project"} {
		t.Run(change, func(t *testing.T) {
			s, r := dialectArtifactOwnerSetup(t)
			v, err := s.enrollDialectArtifactOwner(r)
			if err != nil {
				t.Fatal(err)
			}
			ownerPath, scopePath := dialectArtifactOwnerRowPaths(s, v)
			path := ownerPath
			if strings.HasPrefix(change, "scope-") {
				path = scopePath
			}
			switch change {
			case "owner-body", "scope-body":
				if err = os.WriteFile(path, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "owner-link", "scope-link":
				outside := filepath.Join(t.TempDir(), "outside")
				if err = os.WriteFile(outside, []byte("unrelated private bytes"), 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			case "owner-mode", "scope-mode":
				if err = os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "copied-project":
				other, _ := dialectArtifactOwnerFixture(t, filepath.Join(t.TempDir(), "copy"), s.configDir)
				if err = os.Mkdir(filepath.Join(other.projectDir, ".desk-private"), 0700); err != nil {
					t.Fatal(err)
				}
				marker := dialectArtifactOwnerRead(t, filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker))
				if err = os.WriteFile(filepath.Join(other.projectDir, ".desk-private", dialectArtifactOwnerMarker), marker, 0600); err != nil {
					t.Fatal(err)
				}
				s = other
				token, tokenErr := s.sessions.createExpiring("local user", nil, "", "")
				if tokenErr != nil {
					t.Fatal(tokenErr)
				}
				r = httptest.NewRequest("POST", "http://localhost/private-native-enrollment", nil)
				r.Header.Set("Authorization", "Bearer "+token)
			}
			if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("changed metadata/root gained enrollment", got, err)
			}
			if change == "owner-body" || change == "scope-body" {
				if string(dialectArtifactOwnerRead(t, path)) != `{}` {
					t.Fatal("corrupt immutable metadata was repaired")
				}
			}
		})
	}
}

func TestDialectArtifactOwnerPersistenceFailures(t *testing.T) {
	var steps []string
	for _, role := range []string{"marker", "owner", "scope"} {
		for _, phase := range []string{"write", "sync", "file-close", "publish", "directory-sync", "readback"} {
			steps = append(steps, role+"."+phase)
		}
	}
	steps = append(steps, "work.sync", "work.directory-sync", "selected-readback", "acknowledge", "scope-folder.close", "owner-folder.close", "owner-lock.close", "metadata.close", "marker-lock.close", "project-private.close", "data.close", "private.close", "work.close")
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			s, r := dialectArtifactOwnerSetup(t)
			injected := errors.New("injected owner persistence failure")
			called := false
			testHookDialectArtifactOwnerStep = func(at string) error {
				if at == step {
					called = true
					return injected
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			got, err := s.enrollDialectArtifactOwner(r)
			if !called || !errors.Is(err, injected) || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("failed persistence was acknowledged", called, got, err)
			}
			dialectArtifactOwnerNoEffects(t, s)
		})
	}
}

// A later explicit call must close an interrupted publication's durability
// gap, rather than treating visibility as acknowledgment.
func TestDialectArtifactOwnerPublicationReentry(t *testing.T) {
	for _, role := range []string{"marker", "scope"} {
		t.Run(role, func(t *testing.T) {
			s, r := dialectArtifactOwnerSetup(t)
			injected := errors.New("interrupted before directory sync")
			called := false
			testHookDialectArtifactOwnerStep = func(step string) error {
				if step == role+".publish" {
					called = true
					return injected
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			if got, err := s.enrollDialectArtifactOwner(r); !called || !errors.Is(err, injected) || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("interrupted publication acknowledged", called, got, err)
			}
			markerPath := filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker)
			markerRaw := dialectArtifactOwnerRead(t, markerPath)
			marker, err := readDialectArtifactOwnerMarker(markerRaw)
			if err != nil {
				t.Fatal(err)
			}
			namespace := digestOf([]byte(s.conversationIdentity()))
			ownerPath, scopePath := dialectArtifactOwnerRowPaths(s, dialectArtifactOwnerEnrollment{Owner: marker.Owner, Namespace: namespace})
			ownerRaw := dialectArtifactOwnerRead(t, ownerPath)
			selectedPath := markerPath
			if role == "scope" {
				selectedPath = scopePath
			}
			selectedRaw := dialectArtifactOwnerRead(t, selectedPath)
			called = false
			testHookDialectArtifactOwnerStep = func(step string) error {
				if step == role+".publish" {
					t.Error("visible exact entry was published again")
				}
				if step == role+".directory-sync" {
					called = true
					return injected
				}
				return nil
			}
			if got, err := s.enrollDialectArtifactOwner(r); !called || !errors.Is(err, injected) || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("re-entry skipped uncertain entry durability", called, got, err)
			}
			seen := map[string]bool{}
			testHookDialectArtifactOwnerStep = func(step string) error { seen[step] = true; return nil }
			v, err := s.enrollDialectArtifactOwner(r)
			if err != nil || v.Owner != marker.Owner || v.Namespace != namespace {
				t.Fatal("explicit re-entry did not retain exact identity", v, err)
			}
			for _, phase := range []string{"sync", "file-close", "directory-sync", "readback"} {
				if !seen[role+"."+phase] {
					t.Fatal("existing entry omitted durability acknowledgment", role, phase)
				}
			}
			if seen[role+".publish"] || !bytes.Equal(selectedRaw, dialectArtifactOwnerRead(t, selectedPath)) ||
				!bytes.Equal(markerRaw, dialectArtifactOwnerRead(t, markerPath)) || !bytes.Equal(ownerRaw, dialectArtifactOwnerRead(t, ownerPath)) {
				t.Fatal("re-entry replaced exact immutable metadata")
			}
		})
	}
}

func TestDialectArtifactOwnerFolderReentry(t *testing.T) {
	for _, test := range []struct {
		label string
		edge  int
	}{{"project-folder", 1}, {"metadata-folder", 1}, {"scope-folder", 1}, {"scope-folder", 2}} {
		t.Run(fmt.Sprintf("%s-%d", test.label, test.edge), func(t *testing.T) {
			s, r := dialectArtifactOwnerSetup(t)
			injected := errors.New("selected folder durability uncertain")
			calls := 0
			testHookDialectArtifactOwnerStep = func(step string) error {
				if step == test.label+".directory-publish" {
					calls++
					if calls == test.edge {
						return injected
					}
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			if got, err := s.enrollDialectArtifactOwner(r); calls != test.edge || !errors.Is(err, injected) || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("visible folder acknowledged before parent sync", calls, got, err)
			}
			path := filepath.Join(s.projectDir, ".desk-private")
			if test.label == "metadata-folder" {
				path = dialectArtifactOwnerTablePath(s)
			} else if test.label == "scope-folder" {
				path = dialectArtifactOwnerTablePath(s, "native")
				if test.edge == 2 {
					path = filepath.Join(path, digestOf([]byte(s.conversationIdentity())))
				}
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal("fault did not leave visible selected folder", err)
			}
			calls = 0
			testHookDialectArtifactOwnerStep = func(step string) error {
				if step == test.label+".directory-sync" {
					calls++
					if calls == test.edge {
						return injected
					}
				}
				return nil
			}
			if got, err := s.enrollDialectArtifactOwner(r); calls != test.edge || !errors.Is(err, injected) || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("re-entry skipped selected parent sync", calls, got, err)
			}
			testHookDialectArtifactOwnerStep = nil
			if _, err := s.enrollDialectArtifactOwner(r); err != nil {
				t.Fatal("explicit folder re-entry failed", err)
			}
			after, err := os.Stat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("folder re-entry replaced selected inode", err)
			}
		})
	}
}

func TestDialectArtifactOwnerExistingMetadataSyncRefusal(t *testing.T) {
	for _, role := range []string{"owner", "marker", "scope"} {
		for _, phase := range []string{"sync", "file-close", "directory-sync", "readback"} {
			t.Run(role+"."+phase, func(t *testing.T) {
				s, r := dialectArtifactOwnerSetup(t)
				v, err := s.enrollDialectArtifactOwner(r)
				if err != nil {
					t.Fatal(err)
				}
				ownerPath, scopePath := dialectArtifactOwnerRowPaths(s, v)
				path := ownerPath
				if role == "marker" {
					path = filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker)
				} else if role == "scope" {
					path = scopePath
				}
				before := dialectArtifactOwnerRead(t, path)
				injected := errors.New("existing exact metadata sync uncertain")
				called := false
				testHookDialectArtifactOwnerStep = func(step string) error {
					if step == role+"."+phase {
						called = true
						return injected
					}
					return nil
				}
				t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
				if got, err := s.enrollDialectArtifactOwner(r); !called || !errors.Is(err, injected) || got != (dialectArtifactOwnerEnrollment{}) {
					t.Fatal("existing metadata uncertainty acknowledged", called, got, err)
				}
				if !bytes.Equal(before, dialectArtifactOwnerRead(t, path)) {
					t.Fatal("durability refusal rewrote existing metadata")
				}
			})
		}
	}
}

func TestDialectArtifactOwnerFirstWorkLockDurability(t *testing.T) {
	for _, failAt := range []string{"", "work.sync", "work.directory-sync"} {
		t.Run("first-work-lock/"+failAt, func(t *testing.T) {
			s, r := dialectArtifactOwnerFixtureWithWorkLock(t, filepath.Join(t.TempDir(), "project"), filepath.Join(t.TempDir(), "config"), false)
			pointerPath := filepath.Join(s.configDir, dataLocationName)
			pointer := dialectArtifactOwnerRead(t, pointerPath)
			workPath := filepath.Join(s.configDir, ".data-work.lock")
			if _, err := os.Lstat(workPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("fixture pre-created work lock", err)
			}
			injected := errors.New("work identity durability uncertain")
			seen := map[string]bool{}
			testHookDialectArtifactOwnerStep = func(step string) error {
				seen[step] = true
				if step == "owner.write" && (!seen["work.sync"] || !seen["work.directory-sync"]) {
					t.Error("owner persisted before held work file and parent sync")
				}
				if step == failAt {
					return injected
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			v, err := s.enrollDialectArtifactOwner(r)
			if failAt != "" {
				if !seen[failAt] || !errors.Is(err, injected) || v != (dialectArtifactOwnerEnrollment{}) {
					t.Fatal("uncertain work identity acknowledged", seen, v, err)
				}
				for _, path := range []string{dialectArtifactOwnerTablePath(s), filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker)} {
					if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("work durability refusal published metadata", path, err)
					}
				}
			} else {
				if err != nil || !seen["owner.write"] || !seen["work.sync"] || !seen["work.directory-sync"] {
					t.Fatal("first work lock enrollment failed", seen, v, err)
				}
				rootInfo, rootErr := s.assistant.root.Stat(".")
				workInfo, workErr := os.Stat(workPath)
				if rootErr != nil || workErr != nil {
					t.Fatal(rootErr, workErr)
				}
				raw, err := json.Marshal([]string{"dialect-artifact-work-v1", identityKey(rootInfo), identityKey(workInfo)})
				if err != nil || v.WorkDomain != digestOf(raw) {
					t.Fatal("retained work identity differs from synced inode", v, err)
				}
				testHookDialectArtifactOwnerStep = nil
				if again, err := s.enrollDialectArtifactOwner(r); err != nil || again != v {
					t.Fatal("first-created work identity did not survive re-entry", again, err)
				}
			}
			if !bytes.Equal(pointer, dialectArtifactOwnerRead(t, pointerPath)) {
				t.Fatal("enrollment rewrote existing data pointer")
			}
			dialectArtifactOwnerNoEffects(t, s)
		})
	}
}

func TestDialectArtifactOwnerRefusesPathReplacementAndRevocation(t *testing.T) {
	for _, step := range []string{"before-marker", "marker.publish", "acknowledge", "revoke", "cancel", "work-replacement", "table-replacement"} {
		t.Run(step, func(t *testing.T) {
			s, r := dialectArtifactOwnerSetup(t)
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			called := false
			testHookDialectArtifactOwnerStep = func(at string) error {
				want := step
				if step == "revoke" || step == "cancel" || step == "table-replacement" {
					want = "owner.publish"
				}
				if step == "work-replacement" {
					want = "acknowledge"
				}
				if called || at != want {
					return nil
				}
				called = true
				switch step {
				case "revoke":
					s.sessions.revoke(bearerOf(r))
				case "cancel":
					cancel()
				case "work-replacement":
					path := filepath.Join(s.configDir, ".data-work.lock")
					if err := os.Rename(path, path+".old"); err != nil {
						return err
					}
					return os.WriteFile(path, nil, 0600)
				case "table-replacement":
					path := dialectArtifactOwnerTablePath(s, "owners")
					return os.Rename(path, path+"-old")
				default:
					if err := os.Rename(s.projectDir, s.projectDir+"-old"); err != nil {
						return err
					}
					return os.Mkdir(s.projectDir, 0700)
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			if got, err := s.enrollDialectArtifactOwner(r); !called || err == nil || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("changed live ownership gained enrollment", called, got, err)
			}
		})
	}
}

type dialectArtifactOwnerInjected struct{ calls int }

func (v *dialectArtifactOwnerInjected) Get(context.Context, retainedArtifactRef) ([]byte, error) {
	v.calls++
	return nil, errors.New("unexpected injected Get")
}

func (v *dialectArtifactOwnerInjected) Put(context.Context, []byte) (retainedArtifactRef, error) {
	v.calls++
	return retainedArtifactRef{}, errors.New("unexpected injected Put")
}

func TestDialectArtifactOwnerRefusesBeforeMetadata(t *testing.T) {
	for _, kind := range []string{"injected", "launch", "revoked", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			s, r := dialectArtifactOwnerSetup(t)
			injected := &dialectArtifactOwnerInjected{}
			switch kind {
			case "injected":
				s.retainedArtifacts = injected
			case "launch":
				r.Header.Set("Authorization", "Bearer launch-only")
			case "revoked":
				s.sessions.revoke(bearerOf(r))
			case "cancelled":
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				r = r.WithContext(ctx)
			}
			if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("unsupported or unauthorized enrollment admitted", got, err)
			}
			for _, path := range []string{filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker), dialectArtifactOwnerTablePath(s)} {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("refusal wrote metadata", path, err)
				}
			}
			if injected.calls != 0 {
				t.Fatal("unsupported injected owner was contacted")
			}
		})
	}
}

func TestDialectArtifactOwnerTryOnlyCustody(t *testing.T) {
	for _, kind := range []string{"work", "writes", "private", "marker", "owner"} {
		t.Run(kind, func(t *testing.T) {
			s, r := dialectArtifactOwnerSetup(t)
			var release func() error
			switch kind {
			case "work":
				file, err := s.privateDataFileLock(".data-work.lock", true)
				if err != nil {
					t.Fatal(err)
				}
				release = file.Close
			case "writes":
				s.writes.Lock()
				release = func() error { s.writes.Unlock(); return nil }
			case "private":
				lock, err := s.privateDataLockContext(t.Context(), true)
				if err != nil {
					t.Fatal(err)
				}
				release = lock.Close
			default:
				v, err := s.enrollDialectArtifactOwner(r)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(s.projectDir, ".desk-private", ".dialect-artifact-owner.lock")
				if kind == "owner" {
					path = dialectArtifactOwnerTablePath(s, "locks", v.Owner[:2], v.Owner[2:]+".lock")
				}
				file, err := os.OpenFile(path, os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				if err = lockPrivateData(file, true); err != nil {
					file.Close()
					t.Fatal(err)
				}
				release = file.Close
			}
			defer func() {
				if err := release(); err != nil {
					t.Error(err)
				}
			}()
			start := time.Now()
			if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("held custody was reacquired", got, err)
			}
			if time.Since(start) > time.Second {
				t.Fatal("enrollment waited behind held custody")
			}
		})
	}
}
