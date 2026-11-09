package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func storageChildTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
func storageChildTestServer(t *testing.T) *Server {
	t.Helper()
	dir := storageChildTestDir(t)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	info, err := root.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	return &Server{cfg: Config{deskID: "isolated-observation"}, root: root, projectDir: dir, project: &ProjectRoot{dir: dir, info: info}}
}
func storageChildTestWalk(ctx context.Context) *storageChildWalk {
	return &storageChildWalk{ctx: ctx, remaining: storageChildEntries, seen: map[string]bool{}}
}
func TestStorageChildConfiguredRootsMetadataOnly(t *testing.T) {
	s := storageChildTestServer(t)
	custom := storageChildTestDir(t)
	audit := filepath.Join(custom, "configured-audit")
	signing := filepath.Join(custom, "signing-seed")
	if err := os.Mkdir(audit, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(audit, "one"), []byte("audit"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(signing, []byte("DO_NOT_READ_SECRET"), 0000); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(custom, "selected.json")
	raw, _ := json.Marshal(map[string]any{"configVersion": "7", "audit": map[string]any{"dir": "configured-audit", "signingKey": signing}})
	if err := os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	// Startup alone inherits these exact selectors, matching runtimeEnv's root
	// selection. A named Desk strips them and reads its own local configuration.
	s.cfg.deskID = ""
	t.Setenv(runtimeConfigEnv, config)
	t.Setenv(runtimeSigningKeyEnv, "")
	rows := s.storageChildRuntime(t.Context(), storageChildTestWalk(t.Context()))
	if len(rows) != 2 || rows[0].State != "measured" || *rows[0].LogicalBytes != "5" || rows[1].State != "measured" || *rows[1].LogicalBytes != "18" {
		t.Fatal("configured metadata selection", rows)
	}
	public, _ := json.Marshal(rows)
	if bytes.Contains(public, []byte(custom)) || bytes.Contains(public, []byte("DO_NOT_READ_SECRET")) {
		t.Fatal("path or credential escaped")
	}
	s.cfg.deskID = "named"
	rows = s.storageChildRuntime(t.Context(), storageChildTestWalk(t.Context()))
	if rows[0].State != "inactive" || rows[1].State != "inactive" {
		t.Fatal("inherited roots adopted by named Desk", rows)
	}
}
func TestStorageChildAliasesNestedHardlinksAndBounds(t *testing.T) {
	rootDir := storageChildTestDir(t)
	nested := filepath.Join(rootDir, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "a"), []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(nested, "a"), filepath.Join(rootDir, "alias")); err != nil {
		t.Fatal(err)
	}
	w := storageChildTestWalk(t.Context())
	child := w.path("runtime", "audit", nested)
	parent := w.path("runner", "workspace", rootDir)
	alias := w.path("native", "profile", nested)
	if child.State != "measured" || *child.LogicalBytes != "3" || parent.State != "partial" || parent.Reason != "shared" || *parent.LogicalBytes != "0" || alias.State != "partial" || alias.Reason != "shared" || alias.LogicalBytes != nil {
		t.Fatal("double attribution", child, parent, alias)
	}
	names := storageChildTestWalk(t.Context())
	names.names = storageChildNameBytes
	if row := names.path("runner", "workspace", rootDir); row.State != "partial" || row.Reason != "bounded" {
		t.Fatal("name budget not bounded", row)
	}
	bounded := storageChildTestWalk(t.Context())
	bounded.remaining = 1
	row := bounded.path("runtime", "audit", rootDir)
	if row.State != "partial" || row.Reason != "bounded" || row.LogicalBytes == nil {
		t.Fatal("bounded walk claimed complete", row)
	}
	deep := storageChildTestDir(t)
	current := deep
	for i := 0; i < storageChildDepth+1; i++ {
		current = filepath.Join(current, "d")
		if err := os.Mkdir(current, 0700); err != nil {
			t.Fatal(err)
		}
	}
	row = storageChildTestWalk(t.Context()).path("runtime", "audit", deep)
	if row.State != "partial" || row.Reason != "bounded" {
		t.Fatal("depth not bounded", row)
	}
}
func TestStorageChildSymlinkReplacementAndCancellation(t *testing.T) {
	parent := storageChildTestDir(t)
	dir := filepath.Join(parent, "root")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, current, close, err := storageChildPinAbsolute(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	if err := os.Rename(dir, dir+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if current() == nil {
		t.Fatal("replacement accepted")
	}
	info, _ := root.Stat(".")
	old, _ := os.Stat(dir + "-original")
	if !os.SameFile(info, old) {
		t.Fatal("borrow adopted replacement")
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, _, done, err := storageChildPinAbsolute(t.Context(), link); err == nil {
		done()
		t.Fatal("symlink root followed")
	}
	if err := os.Symlink(filepath.Join(parent, "not-owned"), filepath.Join(dir, "skip-link")); err != nil {
		t.Fatal(err)
	}
	partial := storageChildTestWalk(t.Context()).path("runner", "workspace", dir)
	if partial.State != "partial" || partial.Reason != "unknown" {
		t.Fatal("inner symlink claimed complete", partial)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	row := storageChildTestWalk(ctx).path("runner", "workspace", dir)
	if row.State != "unavailable" || row.LogicalBytes != nil {
		t.Fatal("cancelled measurement became zero", row)
	}
}
func TestStorageChildLowSpaceUnknownAndPartial(t *testing.T) {
	root, err := os.OpenRoot(storageChildTestDir(t))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	w := storageChildTestWalk(t.Context())
	// Syntactic native-counter fixture only, never qualification or admission.
	w.statfs = func(context.Context, *os.File) (storageNativeStatfsRaw, error) {
		return storageNativeStatfsRaw{BlockSize: 4096, FragmentSize: 4096, Blocks: 1048576, FreeBlocks: 2, AvailableBlocks: 0}, nil
	}
	row := w.root("runner", "workspace", root)
	if row.LowSpace == nil || !*row.LowSpace || row.AvailableBytes == nil || *row.AvailableBytes != "0" {
		t.Fatal("measured exhaustion lost", row)
	}
	w = storageChildTestWalk(t.Context())
	w.statfs = func(context.Context, *os.File) (storageNativeStatfsRaw, error) {
		return storageNativeStatfsRaw{}, os.ErrPermission
	}
	row = w.root("runner", "workspace", root)
	if row.AvailableBytes != nil || row.LowSpace != nil {
		t.Fatal("unavailable native counter became healthy zero", row)
	}
}
func TestStorageChildStatusCacheIndependentUnavailableQ(t *testing.T) {
	s := storageChildTestServer(t)
	sessions, err := newSessionStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sessions.close)
	s.sessions = sessions
	s.cfg.Token = testToken
	s.storageReadOnly = true
	jobs := storageChildTestDir(t)
	if err := os.WriteFile(filepath.Join(jobs, "retained"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	s.jobs = &jobsCompanion{dir: jobs}
	before := storageTestTree(t, s.projectDir)
	response, out := storageStatusTestRequest(t, s, "GET", "/api/storage/status", nil)
	if response.Code != 200 || out.Snapshot != nil || out.ChildObservation == nil {
		t.Fatal("Q failure suppressed child observation", response.Body.String())
	}
	first := out.ChildObservation
	if err := os.WriteFile(filepath.Join(jobs, "later"), []byte("later"), 0600); err != nil {
		t.Fatal(err)
	}
	_, again := storageStatusTestRequest(t, s, "GET", "/api/storage/status", nil)
	if !reflect.DeepEqual(first, again.ChildObservation) {
		t.Fatal("request bypassed coalesced refresh")
	}
	if !reflect.DeepEqual(before, storageTestTree(t, s.projectDir)) {
		t.Fatal("observation wrote project")
	}
	if bytes.Contains(response.Body.Bytes(), []byte(jobs)) {
		t.Fatal("selected path exposed")
	}
	s.childStorageStatus.mu.Lock()
	s.childStorageStatus.failed = true
	s.childStorageStatus.mu.Unlock()
	stale := s.storageChildStatus(t.Context(), time.Now())
	if stale == nil || stale.State != "stale" || stale.ObservedAt != first.ObservedAt {
		t.Fatal("failed refresh erased original timestamp", stale)
	}
	s.childStorageStatus.retire()
	if s.storageChildStatus(t.Context(), time.Now()) != nil {
		t.Fatal("closed cache served counters")
	}
}
func TestStorageChildScratchExactRegistrationLifetimeAndOverflow(t *testing.T) {
	s := storageChildTestServer(t)
	root, err := os.OpenRoot(storageChildTestDir(t))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	releases := []func(){}
	for i := 0; i < 17; i++ {
		releases = append(releases, s.observeExchangeScratch(root))
	}
	defer func() {
		for _, done := range releases {
			done()
		}
	}()
	if s.childScratchOverflow != 1 {
		t.Fatal("observation overflow missing")
	}
	out, err := s.observeChildStorage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range out.Roots {
		if row.Component == "git" {
			count++
		}
	}
	if count != 17 || out.State != "partial" {
		t.Fatal("active scratch coverage", count, out.State)
	}
	s.mu.Lock()
	entered := make(chan struct{})
	done := make(chan struct{})
	go func() { close(entered); releases[0](); close(done) }()
	<-entered
	select {
	case <-done:
		s.mu.Unlock()
		t.Fatal("cleanup passed borrowed scratch mutex")
	default:
	}
	s.mu.Unlock()
	<-done
	releases[0]() // release is idempotent and cannot close a reused slot
	for _, release := range releases[1:] {
		release()
	}
	out, err = s.observeChildStorage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range out.Roots {
		if row.Component == "git" && row.State != "inactive" {
			t.Fatal("retired scratch remained active", row)
		}
	}
	if s.childScratchOverflow != 0 {
		t.Fatal("overflow borrow leaked")
	}
}
func TestStorageChildDTOClosedNoPrivateMetadata(t *testing.T) {
	row := storageChildUnknown("native", "profile")
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		t.Fatal("invalid DTO")
	}
	expected := []string{"component", "role", "state", "reason", "logicalBytes", "files", "directories", "availableBytes", "lowSpace"}
	if len(object) != len(expected) {
		t.Fatal("unexpected public field")
	}
	for _, key := range expected {
		if _, ok := object[key]; !ok {
			t.Fatal("missing field", key)
		}
	}
	if strings.Contains(string(raw), "path") || !bytes.Contains(raw, []byte(`"logicalBytes":null`)) {
		t.Fatal("unknown is not null", string(raw))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s := storageChildTestServer(t)
	if got := s.storageChildStatus(ctx, time.Now()); got != nil {
		t.Fatal("outer cancellation published new observation", got)
	}
	if _, _, _, err := storageChildPinAbsolute(t.Context(), "relative"); !errors.Is(err, os.ErrInvalid) {
		t.Fatal("caller-like relative root accepted")
	}
}
func TestStorageChildGatewaySelectionsAndActualMetadataLeaf(t *testing.T) {
	s := storageChildTestServer(t)
	dir := storageChildTestDir(t)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	s.assistant = &assistantStore{dir: dir, root: root}
	s.configDir = dir
	handoffs := storageChildTestDir(t)
	s.localGateway = &localGateway{inputHandoffs: handoffs}
	for _, path := range []string{platformPath(dir, "local-gateway"), platformPath(dir, "gateway-connections")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(handoffs, "owned-handoff"), []byte("child"), 0600); err != nil {
		t.Fatal(err)
	}
	// No executable/bundle exists. Calling ensure/start would fail this fixture.
	out, err := s.observeChildStorage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	selected := 0
	for _, row := range out.Roots {
		if row.Component == "gateway" {
			selected++
			if row.State != "measured" {
				t.Fatal("managed selection not observed", row)
			}
			if row.Role == "handoffs" && *row.LogicalBytes != "5" {
				t.Fatal("custom handoffs ignored", row)
			}
		}
	}
	if selected != 3 || s.localGateway.cmd != nil || s.localGateway.pin != nil {
		t.Fatal("gateway started or roots omitted", selected)
	}
	w := storageChildTestWalk(t.Context())
	leaf := w.path("gateway", "handoffs", handoffs)
	if leaf.State != "measured" || leaf.Files == nil || *leaf.Files != "1" || *leaf.LogicalBytes != "5" {
		t.Fatal("actual metadata leaf", leaf)
	}
	if (leaf.AvailableBytes == nil) != (leaf.LowSpace == nil) {
		t.Fatal("unknown native space fabricated", leaf)
	}
	if leaf.AvailableBytes != nil {
		available, err := strconv.ParseUint(*leaf.AvailableBytes, 10, 64)
		if err != nil || *leaf.LowSpace != (available < storageChildLowBytes) {
			t.Fatal("actual native warning mismatch", leaf)
		}
	}
}

func TestStorageChildInWalkChangesInvalidateAggregate(t *testing.T) {
	for _, cut := range []string{"truncate", "remove", "replace", "directory-post"} {
		t.Run(cut, func(t *testing.T) {
			dir := storageChildTestDir(t)
			name := filepath.Join(dir, "counted")
			if err := os.WriteFile(name, []byte(strings.Repeat("x", 100)), 0600); err != nil {
				t.Fatal(err)
			}
			// A later removal has an unambiguous directory mtime change on filesystems
			// with coarser clocks. No sleep or timing-dependent race is needed.
			before := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			if err := os.Chtimes(dir, before, before); err != nil {
				t.Fatal(err)
			}
			w := storageChildTestWalk(t.Context())
			w.statfs = func(context.Context, *os.File) (storageNativeStatfsRaw, error) {
				return storageNativeStatfsRaw{BlockSize: 4096, FragmentSize: 4096, Blocks: 1048576, FreeBlocks: 2, AvailableBlocks: 0}, nil
			}
			entered := false
			w.cut = func(phase string, root *os.Root, entry string) {
				if entered || cut == "directory-post" && phase != "directory-post" || cut != "directory-post" && phase != "file-counted" {
					return
				}
				entered = true
				var err error
				switch cut {
				case "truncate":
					err = os.Truncate(name, 0)
				case "remove", "directory-post":
					err = root.Remove("counted")
				case "replace":
					err = root.Rename("counted", "original-counted")
					if err == nil {
						err = os.WriteFile(name, nil, 0600)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			row := w.path("runner", "workspace", dir)
			if !entered || row.State != "partial" || row.Reason != "changed" || row.LogicalBytes != nil || row.Files != nil || row.Directories != nil {
				t.Fatal("known-invalid counts retained", entered, row)
			}
			if row.AvailableBytes == nil || *row.AvailableBytes != "0" || row.LowSpace == nil || !*row.LowSpace {
				t.Fatal("independent filesystem fact lost", row)
			}
		})
	}
}
