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

func dialectArtifactLocationPath(s *Server, owner string) string {
	return dialectArtifactOwnerTablePath(s, "owner-locations", owner[:2], owner[2:]+".json")
}

func TestDialectArtifactLocationClosedCodec(t *testing.T) {
	const identity = "18446744073709551615:18446744073709551615"
	id := strings.Repeat("a", 64)
	full := []byte("/" + strings.Repeat("x", 4095))
	relative := []byte(strings.Repeat("x", 4096))
	folder := []byte(strings.Repeat("f", 120))
	for _, leg := range []struct {
		kind   string
		path   []byte
		anchor string
		bound  int
	}{
		{"absolute", full, "", 5835}, {"work", relative, id, 5907}, {"home", folder, id, 603},
	} {
		t.Run(leg.kind, func(t *testing.T) {
			anchor := ""
			if leg.anchor != "" {
				anchor = `,"anchor":"` + leg.anchor + `"`
			}
			// Literal fixed labels/order; no serialization by the codec under test.
			raw := []byte(`{"version":2,"owner":"` + id + `","kind":"file","legacySHA256":"` + id + `","folderIdentity":"` + identity + `","location":{"kind":"` + leg.kind + `"` + anchor + `,"pathBytes":"` + base64.StdEncoding.EncodeToString(leg.path) + `"},"workDomain":"` + id + `"}`)
			if len(raw) != leg.bound {
				t.Fatal("handwritten leg envelope changed", len(raw), leg.bound)
			}
			row, err := readDialectArtifactLocationRecord(raw)
			if err != nil || !bytes.Equal(row.Location.PathBytes, leg.path) {
				t.Fatal("maximal leg refused", err)
			}
			if _, err = readDialectArtifactLocationRecord(append(append([]byte{}, raw...), ' ')); err == nil {
				t.Fatal("one-byte-over/noncanonical leg accepted")
			}
			for _, mutation := range []string{
				strings.Replace(string(raw), `"version":2,`, "", 1), strings.Replace(string(raw), `"owner":"`+id+`",`, "", 1),
				strings.Replace(string(raw), `"kind":"file",`, "", 1), strings.Replace(string(raw), `"legacySHA256":"`+id+`",`, "", 1),
				strings.Replace(string(raw), `"folderIdentity":"`+identity+`",`, "", 1), strings.Replace(string(raw), `,"workDomain":"`+id+`"`, "", 1),
				strings.Replace(string(raw), `"kind":"`+leg.kind+`"`, `"kind":"other"`, 1),
				strings.Replace(string(raw), `"kind":"`+leg.kind+`",`, "", 1),
				strings.Replace(string(raw), `"pathBytes":"`+base64.StdEncoding.EncodeToString(leg.path)+`"`, `"pathBytes":null`, 1),
				strings.Replace(string(raw), `,"pathBytes":"`+base64.StdEncoding.EncodeToString(leg.path)+`"`, "", 1),
				strings.Replace(string(raw), `"location":{"kind":"`+leg.kind+`"`+anchor+`,"pathBytes":"`+base64.StdEncoding.EncodeToString(leg.path)+`"},`, "", 1),
				strings.Replace(string(raw), `"version":2`, `"version":2,"version":2`, 1),
				strings.Replace(string(raw), `"location":{`, `"location":{"unknown":0,`, 1),
				strings.Replace(string(raw), `"owner":`, `"Owner":`, 1),
				strings.Replace(string(raw), `"location":{`, `"location":{"Kind":"`+leg.kind+`",`, 1),
			} {
				if _, err = readDialectArtifactLocationRecord([]byte(mutation)); err == nil {
					t.Fatal("closed member mutation accepted", mutation[:min(len(mutation), 150)])
				}
			}
			if leg.anchor != "" {
				for _, mutation := range []string{strings.Replace(string(raw), anchor, "", 1), strings.Replace(string(raw), anchor, `,"anchor":"`+strings.Repeat("b", 64)+`"`, 1)} {
					// A different well-formed home anchor is a codec value; actual
					// formula/binding refusal belongs to the real producer below.
					if leg.kind == "home" && strings.Contains(mutation, `"anchor":`) {
						continue
					}
					if _, err = readDialectArtifactLocationRecord([]byte(mutation)); err == nil {
						t.Fatal("anchor mutation accepted")
					}
				}
			} else if _, err = readDialectArtifactLocationRecord([]byte(strings.Replace(string(raw), `"kind":"absolute"`, `"kind":"absolute","anchor":"`+id+`"`, 1))); err == nil {
				t.Fatal("mixed absolute union accepted")
			}
			row.Location.PathBytes = append(append([]byte{}, leg.path...), 'x')
			over, _ := json.Marshal(row)
			if _, err = readDialectArtifactLocationRecord(over); err == nil {
				t.Fatal("raw leg overflow accepted")
			}
		})
	}
	base := dialectArtifactLocationRecord{Version: 2, Owner: id, Kind: "file", LegacySHA256: id, FolderIdentity: "-9223372036854775808:18446744073709551615", WorkDomain: id,
		Location: dialectArtifactLocation{Kind: "work", Anchor: id, PathBytes: []byte(".")}}
	for _, path := range [][]byte{[]byte("."), []byte("a\\b"), []byte("q\"\n"), {'x', 0xff}} {
		base.Location.PathBytes = path
		raw, _ := json.Marshal(base)
		v, err := readDialectArtifactLocationRecord(raw)
		if err != nil || !bytes.Equal(v.Location.PathBytes, path) {
			t.Fatal("native relative bytes changed", path, err)
		}
	}
	for _, path := range [][]byte{nil, {}, []byte("/absolute"), []byte("../escape"), []byte("a/../b"), []byte("a//b"), []byte("a/./b"), []byte("a/"), {'a', 0}} {
		base.Location.PathBytes = path
		raw, _ := json.Marshal(base)
		if _, err := readDialectArtifactLocationRecord(raw); err == nil {
			t.Fatal("invalid relative path accepted", path)
		}
	}
	base.Location.PathBytes = []byte(".")
	raw, _ := json.Marshal(base)
	for _, mutation := range []string{strings.Replace(string(raw), `"Lg=="`, `"Lh=="`, 1), strings.Replace(string(raw), `"Lg=="`, `"Lg"`, 1), strings.Replace(string(raw), `"location":{`, `"location":null,"ignored":{`, 1), strings.Replace(string(raw), `"folderIdentity":"-9223372036854775808:18446744073709551615"`, `"folderIdentity":"18446744073709551616:1"`, 1)} {
		if _, err := readDialectArtifactLocationRecord([]byte(mutation)); err == nil {
			t.Fatal("noncanonical location accepted")
		}
	}
}

func TestDialectArtifactLocationActualProducer(t *testing.T) {
	for _, kind := range []string{"absolute", "work", "equal", "work-alias", "absolute-alias", "work-native-bytes", "absolute-native-bytes"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			config := filepath.Join(base, "config")
			project := filepath.Join(base, "external")
			expectedKind, expectedPath := "absolute", project
			if strings.HasPrefix(kind, "work") {
				project = filepath.Join(config, "ordinary", "project")
				expectedKind, expectedPath = "work", filepath.Join("ordinary", "project")
			}
			if strings.Contains(kind, "native-bytes") {
				suffix := "-quote\"-backslash\\-control\n-" + string([]byte{0xff})
				project += suffix
				if expectedKind == "work" {
					expectedPath += suffix
				} else {
					expectedPath = project
				}
			}
			if kind == "equal" {
				project = config
				expectedKind, expectedPath = "work", "."
			}
			input := project
			if strings.HasSuffix(kind, "alias") {
				if err := os.MkdirAll(project, 0755); err != nil {
					t.Fatal(err)
				}
				input = filepath.Join(base, "alias")
				if err := os.Symlink(project, input); err != nil {
					t.Fatal(err)
				}
			}
			s, r := dialectArtifactOwnerFixture(t, input, config)
			// Account relative to the inherited project marker layout, including
			// the work==project case; new tables add nothing at this level.
			if err := os.MkdirAll(filepath.Join(project, ".desk-private"), 0700); err != nil {
				t.Fatal(err)
			}
			before := dialectArtifactOwnerNames(t, s.assistant.root)
			v, err := s.enrollDialectArtifactOwner(r)
			if err != nil {
				t.Fatal(err)
			}
			raw := dialectArtifactOwnerRead(t, dialectArtifactLocationPath(s, v.Owner))
			row, err := readDialectArtifactLocationRecord(raw)
			if err != nil || row.Location.Kind != expectedKind || !bytes.Equal(row.Location.PathBytes, []byte(expectedPath)) || row.WorkDomain != v.WorkDomain || row.LegacySHA256 != v.LegacyOwnerSHA256 || digestOf(raw) != v.OwnerSHA256 || v.HomeAnchor != "" || v.HomeSHA256 != "" {
				t.Fatal("wrong actual location", row, v, err)
			}
			if expectedKind == "work" && row.Location.Anchor != v.WorkDomain {
				t.Fatal("wrong work anchor")
			}
			if !reflect.DeepEqual(before, dialectArtifactOwnerNames(t, s.assistant.root)) {
				t.Fatal("new location table consumed chat-root entries")
			}
			if again, err := s.enrollDialectArtifactOwner(r); err != nil || again != v {
				t.Fatal("repeat changed selected location", again, err)
			}
		})
	}
}

// Handwritten initial v1 state. This never calls the enrollment producer to
// manufacture its own legacy transition expectation.
func dialectArtifactLocationLegacyFixture(t *testing.T, historical bool) (*Server, *http.Request, string, []byte) {
	t.Helper()
	s, r := dialectArtifactOwnerSetup(t)
	const owner = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	project, err := s.root.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.assistant.root.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := s.assistant.root.Stat(".data-work.lock")
	if err != nil {
		t.Fatal(err)
	}
	formula, _ := json.Marshal([]string{"dialect-artifact-work-v1", identityKey(work), identityKey(lock)})
	projectPath, workPath := s.projectDir, s.configDir
	if historical {
		projectPath, workPath = "/historical-never-open/project", "/historical-never-open/settings"
	}
	raw := []byte(`{"version":1,"owner":"` + owner + `","kind":"file","pathBytes":"` + base64.StdEncoding.EncodeToString([]byte(projectPath)) + `","folderIdentity":"` + identityKey(project) + `","work":{"kind":"settings","id":"` + digestOf(formula) + `","pathBytes":"` + base64.StdEncoding.EncodeToString([]byte(workPath)) + `","folderIdentity":"` + identityKey(work) + `"}}`)
	path := dialectArtifactOwnerTablePath(s, "owners", owner[:2], owner[2:]+".json")
	for _, dir := range []string{filepath.Dir(path), filepath.Join(s.projectDir, ".desk-private")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker), []byte(`{"version":1,"owner":"`+owner+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	return s, r, owner, raw
}

func TestDialectArtifactLocationLegacyTransition(t *testing.T) {
	s, r, owner, legacy := dialectArtifactLocationLegacyFixture(t, false)
	markerPath := filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker)
	marker := dialectArtifactOwnerRead(t, markerPath)
	v, err := s.enrollDialectArtifactOwner(r)
	if err != nil || v.Owner != owner || v.LegacyOwnerSHA256 != digestOf(legacy) || v.Namespace != digestOf([]byte(s.conversationIdentity())) {
		t.Fatal("initial v1 transition changed identity", v, err)
	}
	ownerPath, _ := dialectArtifactOwnerRowPaths(s, v)
	if !bytes.Equal(legacy, dialectArtifactOwnerRead(t, ownerPath)) || !bytes.Equal(marker, dialectArtifactOwnerRead(t, markerPath)) {
		t.Fatal("transition rewrote immutable v1/marker")
	}
	for _, change := range []string{"missing", "corrupt", "wrong-path", "wrong-work"} {
		t.Run(change, func(t *testing.T) {
			s, r, owner, raw := dialectArtifactLocationLegacyFixture(t, false)
			path := dialectArtifactOwnerTablePath(s, "owners", owner[:2], owner[2:]+".json")
			switch change {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				raw = []byte(`{}`)
			case "wrong-path":
				row, _ := readDialectArtifactOwnerRecord(raw)
				row.PathBytes = []byte("/another/project")
				raw, _ = json.Marshal(row)
			case "wrong-work":
				row, _ := readDialectArtifactOwnerRecord(raw)
				row.Work.ID = strings.Repeat("b", 64)
				raw, _ = json.Marshal(row)
			}
			if change != "missing" {
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("invalid v1 gained transition", got, err)
			}
			for _, table := range []string{"owner-locations", "work-domains", "home-anchors", "native"} {
				if _, err := os.Lstat(dialectArtifactOwnerTablePath(s, table)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("invalid v1 wrote rows", table, err)
				}
			}
		})
	}
}

// Handwritten current companion for either current or historical immutable v1.
// No enrollment producer is called to manufacture the selected-v2 state.
func dialectArtifactLocationWriteCurrentV2(t *testing.T, s *Server, owner string, legacy []byte) {
	t.Helper()
	v1, err := readDialectArtifactOwnerRecord(legacy)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"version":2,"owner":"` + owner + `","kind":"file","legacySHA256":"` + digestOf(legacy) + `","folderIdentity":"` + v1.FolderIdentity + `","location":{"kind":"absolute","pathBytes":"` + base64.StdEncoding.EncodeToString([]byte(s.projectDir)) + `"},"workDomain":"` + v1.Work.ID + `"}`)
	path := dialectArtifactLocationPath(s, owner)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDialectArtifactLocationHistoricalV1SoleSelection(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(fmt.Sprint(present), func(t *testing.T) {
			s, r, owner, legacy := dialectArtifactLocationLegacyFixture(t, true)
			if present {
				dialectArtifactLocationWriteCurrentV2(t, s, owner, legacy)
			}
			v, err := s.enrollDialectArtifactOwner(r)
			if !present {
				if err == nil || v != (dialectArtifactOwnerEnrollment{}) {
					t.Fatal("missing v2 reconstructed historical v1", v, err)
				}
				return
			}
			if err != nil || v.LegacyOwnerSHA256 != digestOf(legacy) {
				t.Fatal("present actual v2 tried historical paths", v, err)
			}
			if !bytes.Equal(legacy, dialectArtifactOwnerRead(t, dialectArtifactOwnerTablePath(s, "owners", owner[:2], owner[2:]+".json"))) {
				t.Fatal("historical bytes changed")
			}
		})
	}
}

func TestDialectArtifactLocationPresentV2NeverFallsBack(t *testing.T) {
	for _, change := range []string{"malformed", "unreadable", "wrong-hash", "mixed-union", "symlink", "wrong-mode", "case-alias"} {
		t.Run(change, func(t *testing.T) {
			s, r := dialectArtifactOwnerSetup(t)
			v, err := s.enrollDialectArtifactOwner(r)
			if err != nil {
				t.Fatal(err)
			}
			path := dialectArtifactLocationPath(s, v.Owner)
			raw := dialectArtifactOwnerRead(t, path)
			switch change {
			case "malformed":
				raw = []byte(`{}`)
			case "wrong-hash":
				raw = bytes.Replace(raw, []byte(v.LegacyOwnerSHA256), []byte(strings.Repeat("b", 64)), 1)
			case "mixed-union":
				raw = bytes.Replace(raw, []byte(`"kind":"absolute"`), []byte(`"kind":"absolute","anchor":"`+v.WorkDomain+`"`), 1)
			case "case-alias":
				raw = bytes.Replace(raw, []byte(`"location":`), []byte(`"Location":`), 1)
			case "wrong-mode":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".original", path); err != nil {
					t.Fatal(err)
				}
			}
			if change != "symlink" && change != "wrong-mode" {
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var rows bool
			testHookDialectArtifactOwnerStep = func(step string) error {
				if change == "unreadable" && step == "location.preflight-open" {
					return os.ErrPermission
				}
				if step == "work-domain.readback" || step == "owner.readback" || step == "scope.readback" {
					rows = true
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) || rows {
				t.Fatal("present bad v2 fell back/wrote rows", got, err, rows)
			}
		})
	}
}

func TestDialectArtifactLocationPublicationFaults(t *testing.T) {
	steps := []string{"location-folder.directory-publish", "location-folder.directory-sync", "location-folder.close", "location-preflight.close", "location-preflight-file.close", "location.preflight-open", "location-readback.close", "location.selected-readback"}
	for _, phase := range []string{"write", "sync", "file-close", "publish", "directory-sync", "readback"} {
		steps = append(steps, "location."+phase)
	}
	for _, reuse := range []bool{false, true} {
		for _, step := range steps {
			if !reuse && (step == "location-preflight.close" || step == "location-preflight-file.close" || step == "location.preflight-open") {
				continue
			}
			if reuse && (step == "location-folder.directory-publish" || step == "location.write" || step == "location.publish") {
				continue
			}
			t.Run(fmt.Sprintf("reuse-%v/%s", reuse, step), func(t *testing.T) {
				s, r := dialectArtifactOwnerSetup(t)
				if reuse {
					if _, err := s.enrollDialectArtifactOwner(r); err != nil {
						t.Fatal(err)
					}
				}
				seen := false
				testHookDialectArtifactOwnerStep = func(at string) error {
					if at == step {
						seen = true
						return errors.New("location boundary fault")
					}
					return nil
				}
				t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
				if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) || !seen {
					t.Fatal("location fault did not zero output/reach boundary", got, err, seen)
				}
				if !reuse && (step == "location.write" || step == "location.sync" || step == "location.publish" || step == "location.readback") {
					if _, err := s.root.Lstat(".desk-private/" + dialectArtifactOwnerMarker); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("marker preceded location durability", err)
					}
				}
			})
		}
	}
}

func TestDialectArtifactLocationSelectedPathAndBranchRaces(t *testing.T) {
	for _, change := range []string{"relative-symlink", "relative-replacement", "selected-branch", "cancel"} {
		t.Run(change, func(t *testing.T) {
			base := t.TempDir()
			config := filepath.Join(base, "config")
			project := filepath.Join(config, "ordinary", "project")
			s, r := dialectArtifactOwnerFixture(t, project, config)
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			var mutationErr error
			fired := false
			testHookDialectArtifactOwnerStep = func(step string) (hookErr error) {
				if fired || step != "selected-readback" {
					return nil
				}
				fired = true
				defer func() { mutationErr = hookErr }()
				switch change {
				case "relative-symlink", "relative-replacement":
					if err := os.Rename(project, project+".held"); err != nil {
						return err
					}
					if change == "relative-symlink" {
						return os.Symlink(project+".held", project)
					}
					return os.Mkdir(project, 0700)
				case "selected-branch":
					path := dialectArtifactOwnerTablePath(s, "owner-locations")
					return os.Rename(path, path+".detached")
				case "cancel":
					cancel()
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) || !fired {
				t.Fatal("selected path/branch race acknowledged", got, err, fired)
			}
			if mutationErr != nil {
				t.Fatal("race fixture mutation failed", mutationErr)
			}
		})
	}
}

func TestDialectArtifactLocationObservedV2DisappearanceRefuses(t *testing.T) {
	t.Run("before-open", func(t *testing.T) {
		s, r := dialectArtifactOwnerSetup(t)
		v, err := s.enrollDialectArtifactOwner(r)
		if err != nil {
			t.Fatal(err)
		}
		path := dialectArtifactLocationPath(s, v.Owner)
		observed, wrote := false, false
		var mutationErr error
		testHookDialectArtifactOwnerStep = func(step string) error {
			if step == "location.preflight-open" && !observed {
				observed = true
				mutationErr = os.Remove(path)
				return mutationErr
			}
			if step == "work-domain.readback" || step == "owner.readback" || step == "scope.readback" {
				wrote = true
			}
			return nil
		}
		t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
		if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) || !observed || wrote {
			t.Fatal("observed v2 disappearance entered initial v1 gate", got, err, observed, wrote)
		}
		if mutationErr != nil {
			t.Fatal("disappearance fixture failed", mutationErr)
		}
	})
	for _, historical := range []bool{false, true} {
		for _, at := range []string{"work-domain.readback", "owner.readback"} {
			for _, edge := range []string{"leaf", "shard", "table"} {
				t.Run(fmt.Sprintf("historical-%v/%s/%s", historical, at, edge), func(t *testing.T) {
					s, r, owner, legacy := dialectArtifactLocationLegacyFixture(t, historical)
					dialectArtifactLocationWriteCurrentV2(t, s, owner, legacy)
					path := dialectArtifactLocationPath(s, owner)
					if edge == "shard" {
						path = filepath.Dir(path)
					} else if edge == "table" {
						path = filepath.Dir(filepath.Dir(path))
					}
					markerPath := filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker)
					marker := dialectArtifactOwnerRead(t, markerPath)
					fired, acknowledged := false, false
					var mutationErr error
					testHookDialectArtifactOwnerStep = func(step string) error {
						if fired && (step == "location.write" || step == "location.publish" || strings.HasPrefix(step, "marker.") || strings.HasPrefix(step, "scope.")) {
							acknowledged = true
						}
						if !fired && step == at {
							fired = true
							mutationErr = os.Rename(path, path+".disappeared")
							return mutationErr
						}
						return nil
					}
					t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
					if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) || !fired || acknowledged {
						t.Fatal("post-preflight disappearance recreated/acknowledged selected v2", got, err, fired, acknowledged)
					}
					if mutationErr != nil {
						t.Fatal("post-preflight fixture mutation failed", mutationErr)
					}
					if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("selected edge was resurrected", edge, err)
					}
					if !bytes.Equal(marker, dialectArtifactOwnerRead(t, markerPath)) || !bytes.Equal(legacy, dialectArtifactOwnerRead(t, dialectArtifactOwnerTablePath(s, "owners", owner[:2], owner[2:]+".json"))) {
						t.Fatal("uncertain selection changed marker or immutable history")
					}
					if _, err := os.Lstat(dialectArtifactOwnerTablePath(s, "native")); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("uncertain selection published native scope", err)
					}
				})
			}
		}
	}
}

func TestDialectArtifactLocationRelativePathCloseFaults(t *testing.T) {
	for _, equal := range []bool{false, true} {
		t.Run(fmt.Sprint(equal), func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "config")
			project := config
			if !equal {
				project = filepath.Join(config, "ordinary", "project")
			}
			s, r := dialectArtifactOwnerFixture(t, project, config)
			seen := false
			testHookDialectArtifactOwnerStep = func(step string) error {
				if step == "location-path.close" {
					seen = true
					return errors.New("relative path close fault")
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) || !seen {
				t.Fatal("relative path close fault acknowledged/not reached", got, err, seen)
			}
			if _, err := s.root.Lstat(".desk-private/" + dialectArtifactOwnerMarker); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("path proof Close uncertainty published marker", err)
			}
		})
	}
}
