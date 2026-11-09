package desk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func dialectArtifactHomeAnchorPath(s *Server, id string) string {
	return dialectArtifactOwnerTablePath(s, "home-anchors", id[:2], id[2:]+".json")
}

func TestDialectArtifactHomeAnchorClosedCodec(t *testing.T) {
	id := strings.Repeat("a", 64)
	const identity = "18446744073709551615:18446744073709551615"
	path := []byte("/" + strings.Repeat("x", 4095))
	raw := []byte(`{"version":1,"id":"` + id + `","kind":"home","pathBytes":"` + base64.StdEncoding.EncodeToString(path) + `","folderIdentity":"` + identity + `"}`)
	if len(raw) != 5639 || len(path) != 4096 || len(base64.StdEncoding.EncodeToString(path)) != 5464 {
		t.Fatal("handwritten home envelope changed", len(raw))
	}
	row, err := readDialectArtifactHomeAnchorRecord(raw)
	if err != nil || !bytes.Equal(row.PathBytes, path) {
		t.Fatal("maximum home row refused", err)
	}
	for _, mutation := range []string{
		string(raw) + " ", strings.Replace(string(raw), `"version":1,`, "", 1), strings.Replace(string(raw), `"id":"`+id+`",`, "", 1),
		strings.Replace(string(raw), `"kind":"home",`, "", 1), strings.Replace(string(raw), `"pathBytes":"`+base64.StdEncoding.EncodeToString(path)+`",`, "", 1), strings.Replace(string(raw), `,"folderIdentity":"`+identity+`"`, "", 1),
		strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1), strings.Replace(string(raw), `"id":`, `"ID":`, 1), strings.Replace(string(raw), `"kind":"home"`, `"kind":"work"`, 1),
		strings.Replace(string(raw), `"version":1`, `"unknown":0,"version":1`, 1), strings.Replace(string(raw), `"pathBytes":"`+base64.StdEncoding.EncodeToString(path)+`"`, `"pathBytes":null`, 1),
		strings.Replace(string(raw), `"id":"`+id+`"`, `"id":"`+strings.Repeat("A", 64)+`"`, 1),
	} {
		if _, err := readDialectArtifactHomeAnchorRecord([]byte(mutation)); err == nil {
			t.Fatal("closed home mutation accepted")
		}
	}
	for _, path := range [][]byte{nil, {}, []byte("relative"), {'/', 0}, append(append([]byte{}, path...), 'x')} {
		row.PathBytes = path
		raw, _ := json.Marshal(row)
		if _, err := readDialectArtifactHomeAnchorRecord(raw); err == nil {
			t.Fatal("invalid home path accepted")
		}
	}
	row.PathBytes = []byte{'/', 'q', '"', '\\', '\n', 0xff}
	raw, _ = json.Marshal(row)
	decoded, err := readDialectArtifactHomeAnchorRecord(raw)
	if err != nil || !bytes.Equal(decoded.PathBytes, row.PathBytes) {
		t.Fatal("home ancestor bytes normalized", err)
	}
	for _, identity := range []string{"0:0", "-9223372036854775808:18446744073709551615"} {
		row.FolderIdentity = identity
		raw, _ = json.Marshal(row)
		if _, err := readDialectArtifactHomeAnchorRecord(raw); err != nil {
			t.Fatal("native identity refused", identity, err)
		}
	}
	for _, identity := range []string{"", "1:", "+1:1", "01:1", "18446744073709551616:1", "-9223372036854775809:1"} {
		row.FolderIdentity = identity
		raw, _ = json.Marshal(row)
		if _, err := readDialectArtifactHomeAnchorRecord(raw); err == nil {
			t.Fatal("malformed home identity accepted", identity)
		}
	}
}

// Low-level real pinned roots, actual shared home lease and fresh registry.
// This exercises enrollment, not the New/startup application constructor.
func dialectArtifactHomeAnchorFixture(t *testing.T, suffix string) (*Server, *http.Request, *Server, *http.Request) {
	t.Helper()
	homePath := filepath.Join(t.TempDir(), "home"+suffix)
	config := filepath.Join(homePath, deskHomePlatform)
	parent, request := dialectArtifactOwnerFixtureWithStorage(t, filepath.Join(homePath, "startup"), config, true, false)
	child, childRequest := dialectArtifactOwnerFixtureWithStorage(t, filepath.Join(homePath, "registered-child"), config, true, false)
	owner := strings.Repeat("a", 64)
	startupID, childID := strings.Repeat("b", 32), strings.Repeat("c", 32)
	registry := deskHomeRegistry{Version: 1, Owner: owner, Startup: startupID, Desks: []deskHomeEntry{
		{ID: startupID, Name: "Startup", Folder: "startup", Kind: "project", HistoryIdentity: "/original/startup", StorageOwner: owner, RunnerWorkspace: owner},
		{ID: childID, Name: "Child", Folder: "registered-child", Kind: "desk", HistoryIdentity: "desk:" + childID, StorageOwner: childID, RunnerWorkspace: childID},
	}}
	if err := validateHomeRegistry(registry); err != nil {
		t.Fatal(err)
	}
	parent.home = &deskHome{Path: homePath, Registry: registry}
	child.home = &deskHome{Path: homePath, Registry: registry}
	child.cfg.parent, child.cfg.deskID = parent, childID
	child.writes = parent.writes
	raw, _ := json.MarshalIndent(registry, "", "  ") // accepted lexical registry formatting remains exact
	if err := os.WriteFile(filepath.Join(config, deskHomeRegistryName), append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	lease, err := openHomeLease(config, false)
	if err != nil {
		t.Fatal(err)
	}
	parent.homeLease = lease
	t.Cleanup(func() { _ = lease.Close() })
	for _, s := range []*Server{parent, child} {
		if err := os.MkdirAll(filepath.Join(s.projectDir, ".desk-private"), 0700); err != nil {
			t.Fatal(err)
		}
		lock, err := s.privateDataLockContext(t.Context(), true)
		if err != nil {
			t.Fatal(err)
		}
		data, openErr := s.openChatData()
		if openErr == nil {
			if data.legacy {
				t.Fatal("home fixture used legacy data root")
			}
			openErr = data.root.Close()
		}
		if err := errors.Join(openErr, lock.Close()); err != nil {
			t.Fatal(err)
		}
		// Physical ancestor bytes are carried by the open roots. The existing
		// contained storage JSON persists only the relative, valid-UTF8 leaf.
		pointer := dialectArtifactOwnerRead(t, filepath.Join(s.projectDir, ".desk-private", dataLocationName))
		if string(pointer) != `{"version":1,"path":"data"}` {
			t.Fatal("home fixture did not persist its contained relative path", string(pointer))
		}
	}
	if _, err := os.Lstat(filepath.Join(config, dataLocationName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("home fixture initialized a shared legacy storage pointer", err)
	}
	return parent, request, child, childRequest
}

func TestDialectArtifactHomeAnchorActualParentAndChildProducer(t *testing.T) {
	// CR/LF are codec-only values: existing contained chat storage refuses
	// those physical paths. Tab and invalid UTF8 ancestors are accepted here.
	for _, suffix := range []string{"", "-quote\"-backslash\\-control\t-" + string([]byte{0xff})} {
		t.Run(fmt.Sprintf("ancestor-bytes-%d", len(suffix)), func(t *testing.T) {
			parent, r, child, childR := dialectArtifactHomeAnchorFixture(t, suffix)
			for _, path := range []string{parent.home.Path, parent.projectDir, child.projectDir} {
				if err := os.Chmod(path, 0755); err != nil {
					t.Fatal(err)
				}
			}
			registryPath := filepath.Join(parent.configDir, deskHomeRegistryName)
			registry := dialectArtifactOwnerRead(t, registryPath)
			before := dialectArtifactOwnerNames(t, parent.assistant.root)
			var parentAnchor string
			for _, subject := range []struct {
				s *Server
				r *http.Request
			}{{parent, r}, {child, childR}} {
				s, r := subject.s, subject.r
				v, err := s.enrollDialectArtifactOwner(r)
				if err != nil {
					t.Fatal(err)
				}
				if parentAnchor == "" {
					parentAnchor = v.HomeAnchor
				} else if parentAnchor != v.HomeAnchor {
					t.Fatal("child did not use actual parent anchor")
				}
				raw := dialectArtifactOwnerRead(t, dialectArtifactHomeAnchorPath(s, v.HomeAnchor))
				anchor, err := readDialectArtifactHomeAnchorRecord(raw)
				info, statErr := os.Stat(parent.home.Path)
				formula, _ := json.Marshal([]string{"dialect-artifact-home-v1", parent.home.Registry.Owner, identityKey(info)})
				locationRaw := dialectArtifactOwnerRead(t, dialectArtifactLocationPath(s, v.Owner))
				location, decodeErr := readDialectArtifactLocationRecord(locationRaw)
				if err != nil || statErr != nil || decodeErr != nil || anchor.ID != digestOf(formula) || anchor.ID != v.HomeAnchor || !bytes.Equal(anchor.PathBytes, []byte(parent.home.Path)) || anchor.FolderIdentity != identityKey(info) || digestOf(raw) != v.HomeSHA256 || location.Location.Kind != "home" || location.Location.Anchor != anchor.ID || string(location.Location.PathBytes) != s.homeEntry().Folder || location.WorkDomain != v.WorkDomain || digestOf(locationRaw) != v.OwnerSHA256 || v.Namespace != digestOf([]byte(s.homeEntry().HistoryIdentity)) {
					t.Fatal("actual home/namespace formula mismatch", anchor, location, v, err, statErr, decodeErr)
				}
				if bytes.Contains(raw, []byte("Startup")) || bytes.Contains(raw, []byte("desks")) || bytes.Contains(locationRaw, []byte("Child")) {
					t.Fatal("home metadata copied display registry")
				}
				if again, err := s.enrollDialectArtifactOwner(r); err != nil || again != v {
					t.Fatal("home repeat changed provenance", again, err)
				}
				reopened, request := dialectArtifactOwnerFixtureWithStorage(t, s.projectDir, s.configDir, true, false)
				reopened.home = s.home
				reopened.cfg.parent, reopened.cfg.deskID = s.cfg.parent, s.cfg.deskID
				reopened.writes = s.writes
				if reopened.cfg.parent == nil {
					lease, err := openHomeLease(s.configDir, false)
					if err != nil {
						t.Fatal(err)
					}
					reopened.homeLease = lease
					t.Cleanup(func() { _ = lease.Close() })
				}
				if again, err := reopened.enrollDialectArtifactOwner(request); err != nil || again != v {
					t.Fatal("real home reopen changed selected provenance", again, err)
				}
			}
			if !bytes.Equal(registry, dialectArtifactOwnerRead(t, registryPath)) {
				t.Fatal("accepted registry bytes rewritten")
			}
			if !reflect.DeepEqual(before, dialectArtifactOwnerNames(t, parent.assistant.root)) {
				t.Fatal("home tables changed chat-root entries")
			}
		})
	}
}

func TestDialectArtifactHomeAnchorCurrentRegistryDomain(t *testing.T) {
	for _, oversize := range []bool{false, true} {
		t.Run(fmt.Sprint(oversize), func(t *testing.T) {
			s, r, _, _ := dialectArtifactHomeAnchorFixture(t, "")
			registry := s.home.Registry
			registry.Desks = append([]deskHomeEntry{}, registry.Desks...)
			for n := 0; n < 255; n++ {
				id := fmt.Sprintf("%032x", n+1)
				registry.Desks = append(registry.Desks, deskHomeEntry{ID: id, Name: fmt.Sprint("Other ", n), Folder: fmt.Sprint("other-", n), Kind: "desk", HistoryIdentity: "desk:" + id, StorageOwner: id, RunnerWorkspace: id})
			}
			raw, _ := json.MarshalIndent(registry, "", " ")
			raw = append(raw, bytes.Repeat([]byte{' '}, (1<<20)-len(raw))...)
			if oversize {
				raw = append(raw, ' ')
			}
			path := filepath.Join(s.configDir, deskHomeRegistryName)
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			v, err := s.enrollDialectArtifactOwner(r)
			if oversize {
				if err == nil || v != (dialectArtifactOwnerEnrollment{}) {
					t.Fatal("oversized registry accepted")
				}
				return
			}
			if err != nil || v.HomeAnchor == "" || len(registry.Desks) != 257 {
				t.Fatal("existing 1MiB/257 entry domain rejected", v, err)
			}
			if !bytes.Equal(raw, dialectArtifactOwnerRead(t, path)) {
				t.Fatal("registry lexical bytes changed")
			}
		})
	}
}

func TestDialectArtifactHomeAnchorFreshNameChangePreservesBinding(t *testing.T) {
	s, r, _, _ := dialectArtifactHomeAnchorFixture(t, "")
	registry := s.home.Registry
	registry.Desks = append([]deskHomeEntry{}, registry.Desks...)
	registry.Desks[0].Name = "Changed display name"
	raw, _ := json.MarshalIndent(registry, "", "   ")
	path := filepath.Join(s.configDir, deskHomeRegistryName)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if v, err := s.enrollDialectArtifactOwner(r); err != nil || v.HomeAnchor == "" {
		t.Fatal("fresh display name changed stable home binding", v, err)
	}
	if !bytes.Equal(raw, dialectArtifactOwnerRead(t, path)) {
		t.Fatal("fresh registry bytes were rewritten")
	}
}

func TestDialectArtifactHomeAnchorCustodyRefusals(t *testing.T) {
	for _, change := range []string{"lease-absent", "lease-closed", "lease-entry-absent", "lease-replaced", "lease-symlink", "lease-insecure", "exclusive-home", "desks-busy", "location-busy", "queued-home", "moving-project", "moved-project", "owner-changed", "binding-changed", "home-absent", "config-edge-symlink", "cached-owner-changed", "cached-folder-changed"} {
		t.Run(change, func(t *testing.T) {
			s, r, _, _ := dialectArtifactHomeAnchorFixture(t, "")
			lockPath := filepath.Join(s.configDir, ".home-use.lock")
			var release func()
			switch change {
			case "lease-absent":
				s.homeLease = nil
			case "lease-closed":
				if err := s.homeLease.Close(); err != nil {
					t.Fatal(err)
				}
			case "lease-entry-absent":
				if err := os.Remove(lockPath); err != nil {
					t.Fatal(err)
				}
			case "lease-replaced", "lease-symlink":
				if err := os.Rename(lockPath, lockPath+".original"); err != nil {
					t.Fatal(err)
				}
				if change == "lease-symlink" {
					if err := os.Symlink(lockPath+".original", lockPath); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(lockPath, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "lease-insecure":
				if err := os.Chmod(lockPath, 0644); err != nil {
					t.Fatal(err)
				}
			case "exclusive-home":
				if err := s.homeLease.Close(); err != nil {
					t.Fatal(err)
				}
				file, err := os.OpenFile(lockPath, os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				s.homeLease = file
				t.Cleanup(func() { _ = file.Close() })
				exclusive, err := openHomeLease(s.configDir, true)
				if err != nil {
					t.Fatal(err)
				}
				release = func() { _ = exclusive.Close() }
			case "desks-busy":
				s.desksMu.Lock()
				release = s.desksMu.Unlock
			case "location-busy":
				s.location.use.Lock()
				release = s.location.use.Unlock
			case "queued-home":
				s.homeMove.queued.Store(true)
			case "moving-project":
				s.location.moving.Store(true)
			case "moved-project":
				s.location.moved = "/moved"
			case "home-absent":
				s.home = nil
			case "cached-owner-changed":
				s.home.Registry.Owner = strings.Repeat("f", 64)
			case "cached-folder-changed":
				s.home.Registry.Desks[0].Folder = "another"
			case "owner-changed", "binding-changed":
				registry := s.home.Registry
				registry.Desks = append([]deskHomeEntry{}, registry.Desks...)
				if change == "owner-changed" {
					registry.Owner = strings.Repeat("f", 64)
				} else {
					registry.Desks[0].HistoryIdentity = "/different/history"
				}
				raw, _ := json.Marshal(registry)
				if err := os.WriteFile(filepath.Join(s.configDir, deskHomeRegistryName), raw, 0600); err != nil {
					t.Fatal(err)
				}
			case "config-edge-symlink":
				if err := os.Rename(s.configDir, s.configDir+".held"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(s.configDir+".held", s.configDir); err != nil {
					t.Fatal(err)
				}
			}
			if release != nil {
				defer release()
			}
			if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("home custody fault gained enrollment", got, err)
			}
			if _, err := s.root.Lstat(".desk-private/" + dialectArtifactOwnerMarker); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("home refusal published marker", err)
			}
			if change == "lease-entry-absent" {
				if _, err := os.Lstat(lockPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("producer created missing home lease", err)
				}
			}
		})
	}
}

func TestDialectArtifactHomeAnchorPublicationFaults(t *testing.T) {
	steps := []string{"home-registry.sync", "home-registry.close", "home-config.sync", "home-config.close", "home-parent.sync", "home-parent.close", "home-anchor-folder.directory-publish", "home-anchor-folder.directory-sync", "home-anchor-folder.close", "home-anchor-readback.close", "home-anchor.selected-readback", "home-registry-check.close", "home-edge.close", "home-root.close", "home-hold.close"}
	for _, phase := range []string{"write", "sync", "file-close", "publish", "directory-sync", "readback"} {
		steps = append(steps, "home-anchor."+phase)
	}
	for _, reuse := range []bool{false, true} {
		for _, step := range steps {
			if reuse && (step == "home-anchor-folder.directory-publish" || step == "home-anchor.write" || step == "home-anchor.publish") {
				continue
			}
			t.Run(fmt.Sprintf("reuse-%v/%s", reuse, step), func(t *testing.T) {
				s, r, _, _ := dialectArtifactHomeAnchorFixture(t, "")
				if reuse {
					if _, err := s.enrollDialectArtifactOwner(r); err != nil {
						t.Fatal(err)
					}
				}
				seen := false
				testHookDialectArtifactOwnerStep = func(at string) error {
					if at == step {
						seen = true
						return errors.New("home boundary fault")
					}
					return nil
				}
				t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
				if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) || !seen {
					t.Fatal("home boundary fault acknowledged/not exercised", got, err, seen)
				}
				if !reuse && ((strings.HasPrefix(step, "home-anchor.") && step != "home-anchor.selected-readback") || step == "home-registry.sync" || step == "home-config.sync" || step == "home-parent.sync") {
					if _, err := s.root.Lstat(".desk-private/" + dialectArtifactOwnerMarker); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("marker preceded home durability", err)
					}
				}
			})
		}
	}
}

func TestDialectArtifactHomeAnchorRegistryAndOriginalLeaseRaces(t *testing.T) {
	for _, selected := range []string{deskHomePlatform, "startup"} {
		for _, replacement := range []string{"same-inode-symlink", "different-inode"} {
			for _, closeFault := range []bool{false, true} {
				t.Run(fmt.Sprintf("inspection-open/%s/%s/close-%v", selected, replacement, closeFault), func(t *testing.T) {
					s, r, _, _ := dialectArtifactHomeAnchorFixture(t, "")
					fired, continued := false, false
					var mutationErr error
					closeErr := errors.New("home race edge Close fault")
					inspected := func(name string) error {
						if fired {
							continued = true // the attacked proof must fail at its own edge
							return nil
						}
						if name != selected {
							return nil
						}
						fired = true
						path := filepath.Join(s.home.Path, name)
						mutationErr = os.Rename(path, path+".held")
						if mutationErr == nil {
							if replacement == "same-inode-symlink" {
								// In-root relative symlink resolves to the inspected inode.
								mutationErr = os.Symlink(name+".held", path)
							} else {
								mutationErr = os.Mkdir(path, 0755)
							}
						}
						return mutationErr
					}
					r = r.WithContext(context.WithValue(r.Context(), dialectArtifactHomeEdgeInspectionKey{}, inspected))
					testHookDialectArtifactOwnerStep = func(step string) error {
						if fired && step == "home-registry-check.close" {
							continued = true // no registry proof after a stale edge
						}
						if fired && closeFault && step == "home-edge.close" {
							return closeErr
						}
						return nil
					}
					t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
					got, err := s.enrollDialectArtifactOwner(r)
					if err == nil || got != (dialectArtifactOwnerEnrollment{}) || !fired || continued {
						t.Fatal("home inspection/open replacement passed its edge proof", got, err, fired, continued)
					}
					if mutationErr != nil {
						t.Fatal("home inspection/open fixture mutation failed", mutationErr)
					}
					if closeFault && (!errors.Is(err, closeErr) || !errors.Is(err, errDialectArtifactOwner)) {
						t.Fatal("stale home edge lost its joined Close error", err)
					}
					if _, err := s.root.Lstat(".desk-private/" + dialectArtifactOwnerMarker); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("inspection/open uncertainty published marker", err)
					}
				})
			}
		}
	}
	for _, change := range []string{"registry-inode", "unrelated-registry-publication", "namespace", "original-lease-close", "home-root", "project-edge", "selected-anchor-branch"} {
		t.Run(change, func(t *testing.T) {
			s, r, _, _ := dialectArtifactHomeAnchorFixture(t, "")
			fired := false
			var mutationErr error
			testHookDialectArtifactOwnerStep = func(step string) (hookErr error) {
				at := "work-domain.readback"
				if change == "selected-anchor-branch" {
					at = "selected-readback"
				}
				if fired || step != at {
					return nil
				}
				fired = true
				defer func() { mutationErr = hookErr }()
				switch change {
				case "registry-inode":
					path := filepath.Join(s.configDir, deskHomeRegistryName)
					raw := dialectArtifactOwnerRead(t, path)
					if err := os.Rename(path, path+".held"); err != nil {
						return err
					}
					return os.WriteFile(path, raw, 0600)
				case "unrelated-registry-publication":
					path := filepath.Join(s.configDir, deskHomeRegistryName)
					raw := dialectArtifactOwnerRead(t, path)
					return os.WriteFile(path, append(raw, ' '), 0600)
				case "namespace":
					name := "conversations-" + strings.Repeat("d", 64) + ".json"
					dataPath := filepath.Join(s.projectDir, ".desk-private", "data")
					if err := os.WriteFile(filepath.Join(dataPath, name), []byte(`{"version":1,"chats":[]}`), 0600); err != nil {
						return err
					}
					raw, _ := json.Marshal(projectBindings{Version: 1, Projects: map[string]string{s.homeEntry().HistoryIdentity: name}})
					return os.WriteFile(filepath.Join(dataPath, projectBindingsName), raw, 0600)
				case "original-lease-close":
					if err := s.homeLease.Close(); err != nil {
						return err
					}
					exclusive, err := openHomeLease(s.configDir, true)
					if err == nil {
						_ = exclusive.Close()
						return errors.New("owned extra shared home hold was absent")
					}
					return nil
				case "home-root":
					if err := os.Rename(s.home.Path, s.home.Path+".held"); err != nil {
						return err
					}
					return os.Mkdir(s.home.Path, 0700)
				case "project-edge":
					if err := os.Rename(s.projectDir, s.projectDir+".held"); err != nil {
						return err
					}
					return os.Symlink(s.projectDir+".held", s.projectDir)
				case "selected-anchor-branch":
					path := dialectArtifactOwnerTablePath(s, "home-anchors")
					return os.Rename(path, path+".detached")
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) || !fired {
				t.Fatal("home race acknowledged/not exercised", got, err, fired)
			}
			if mutationErr != nil {
				t.Fatal("home race fixture mutation/owned shared hold check failed", mutationErr)
			}
		})
	}
}

func TestDialectArtifactHomeAnchorSelectedCorruptionAndAuthority(t *testing.T) {
	for _, change := range []string{"anchor-body", "anchor-mode", "anchor-symlink", "location-home-anchor", "revoke", "cancel"} {
		t.Run(change, func(t *testing.T) {
			s, r, _, _ := dialectArtifactHomeAnchorFixture(t, "")
			v, err := s.enrollDialectArtifactOwner(r)
			if err != nil {
				t.Fatal(err)
			}
			anchorPath := dialectArtifactHomeAnchorPath(s, v.HomeAnchor)
			switch change {
			case "anchor-body":
				if err := os.WriteFile(anchorPath, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "anchor-mode":
				if err := os.Chmod(anchorPath, 0644); err != nil {
					t.Fatal(err)
				}
			case "anchor-symlink":
				if err := os.Rename(anchorPath, anchorPath+".held"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(anchorPath+".held", anchorPath); err != nil {
					t.Fatal(err)
				}
			case "location-home-anchor":
				path := dialectArtifactLocationPath(s, v.Owner)
				raw := dialectArtifactOwnerRead(t, path)
				raw = bytes.Replace(raw, []byte(v.HomeAnchor), []byte(strings.Repeat("e", 64)), 1)
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			fired := false
			if change == "revoke" || change == "cancel" {
				testHookDialectArtifactOwnerStep = func(step string) error {
					if step == "acknowledge" && !fired {
						fired = true
						if change == "revoke" {
							s.sessions.revoke(bearerOf(r))
						} else {
							cancel()
						}
					}
					return nil
				}
				t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			}
			if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("home corruption/authority change acknowledged", got, err)
			}
			if (change == "revoke" || change == "cancel") && !fired {
				t.Fatal("authority boundary not reached")
			}
			if change == "anchor-body" && string(dialectArtifactOwnerRead(t, anchorPath)) != `{}` {
				t.Fatal("corrupt home anchor repaired")
			}
		})
	}
}
