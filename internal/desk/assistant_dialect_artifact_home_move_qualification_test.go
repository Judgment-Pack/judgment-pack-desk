package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Hand-authored semantic envelope: every selected file owner has its two
// immutable pins and mutable locator, plus the shared work and home rows.
// No filesystem replay or byte padding supplies this codec expectation.
func homeMoveSemanticEnvelope(t *testing.T, count int) deskHomeTransaction {
	t.Helper()
	hash := strings.Repeat("a", 64)
	p := &dialectArtifactHomeMovePlan{Version: 2, RenameParents: []dialectArtifactHomeMoveParent{{homePath("/"), "1:1"}}, ConfigIdentity: "1:2", LeaseIdentity: "1:3", WorkLockIdentity: "1:4", PrivateLockIdentity: "1:5", SecretsIdentity: "1:6", MetadataIdentity: "1:7", SourceHome: homePath("/old"), SourceHomeIdentity: "1:8", DestinationIdentity: "1:8", RegistryOwner: hash, RegistryIdentity: "1:9", RegistrySHA256: hash, Projects: []dialectArtifactHomeMoveProject{}, Pins: []dialectArtifactHomeMovePin{}, Rows: []dialectArtifactHomeMoveRow{}}
	p.WorkDomain = dialectArtifactWorkDomainID(p.ConfigIdentity, p.WorkLockIdentity)
	tx := deskHomeTransaction{Version: 2, Config: "/old/platform", Destination: "/new", Renames: []homeRename{{"/old", "/new", "1:8"}}, Writes: []homeWrite{}, Retained: p}
	encode := func(v any) []byte {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	add := func(table, id string, before, after []byte) {
		row := dialectArtifactHomeMoveRow{Table: table, ID: id, Identity: "2:1", FolderIdentity: "2:2", BeforeKind: "present", Before: before, BeforeSHA256: digestOf(before), After: after, AfterSHA256: digestOf(after), Write: len(tx.Writes)}
		p.Rows = append(p.Rows, row)
		tx.Writes = append(tx.Writes, homeWrite{Path: homeMoveRowPath(filepath.Join(tx.Destination, deskHomePlatform), table, id), Before: row.BeforeSHA256, Data: after})
	}
	work := dialectArtifactWorkDomainRecord{Version: 1, ID: p.WorkDomain, Kind: "settings", PathBytes: []byte(tx.Config), FolderIdentity: p.ConfigIdentity, LockIdentity: p.WorkLockIdentity}
	before := encode(work)
	work.PathBytes = []byte(filepath.Join(tx.Destination, deskHomePlatform))
	add("work-domains", work.ID, before, encode(work))
	anchor := dialectArtifactHomeAnchorRecord{Version: 1, ID: dialectArtifactHomeAnchorID(p.RegistryOwner, p.DestinationIdentity), Kind: "home", PathBytes: []byte("/old"), FolderIdentity: p.DestinationIdentity}
	before = encode(anchor)
	anchor.PathBytes = []byte(tx.Destination)
	add("home-anchors", anchor.ID, before, encode(anchor))
	for i := 0; i < count; i++ {
		owner := fmt.Sprintf("%064x", i+1)
		folder := fmt.Sprintf("desk-%d", i)
		p.Projects = append(p.Projects, dialectArtifactHomeMoveProject{ID: fmt.Sprintf("%032x", i+1), Folder: folder, From: homePath(filepath.Join("/old", folder)), To: homePath(filepath.Join("/new", folder)), Identity: "3:1", PrivateIdentity: "3:2", MarkerIdentity: "3:3", MarkerSHA256: hash, MarkerLockIdentity: "3:4", OwnerLockIdentity: "3:5", Owner: owner, Namespace: hash, Kind: "file"})
		p.Pins = append(p.Pins, dialectArtifactHomeMovePin{Table: "owners", ID: owner, Identity: "4:1", FolderIdentity: "4:2", SHA256: hash}, dialectArtifactHomeMovePin{Table: "native", ID: owner, Namespace: hash, Identity: "4:3", FolderIdentity: "4:4", SHA256: hash})
		location := dialectArtifactLocationRecord{Version: 2, Owner: owner, Kind: "file", LegacySHA256: hash, FolderIdentity: "3:1", WorkDomain: p.WorkDomain, Location: dialectArtifactLocation{Kind: "home", Anchor: anchor.ID, PathBytes: []byte(folder)}}
		add("owner-locations", owner, encode(location), encode(location))
	}
	tx.Writes = append(tx.Writes, homeWrite{Path: "/bootstrap/home.json", Before: "absent", Data: []byte(`{"version":1,"path":"/new"}`)})
	return tx
}

func TestDialectArtifactHomeMoveSemanticEnvelope(t *testing.T) {
	tx := homeMoveSemanticEnvelope(t, maxDesks+1)
	raw, err := marshalHomeTransaction(tx)
	if err != nil {
		t.Fatal("semantic maximum", err)
	}
	got, err := decodeHomeTransaction(raw)
	if err != nil || len(got.Retained.Projects) != maxDesks+1 || len(got.Retained.Pins) != 2*(maxDesks+1) || len(got.Retained.Rows) != maxDesks+3 {
		t.Fatal("semantic maximum round trip", err)
	}
	if len(raw) >= homeTransactionLimit {
		t.Fatal("semantic fixture unexpectedly reaches wire cap")
	}
	over := homeMoveSemanticEnvelope(t, maxDesks+2)
	if _, err = marshalHomeTransaction(over); err == nil {
		t.Fatal("semantic maximum +1 emitted")
	}
	// json.Marshal intentionally bypasses the checked writer to test admission.
	overRaw, err := json.Marshal(over)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeHomeTransaction(overRaw); err == nil {
		t.Fatal("semantic maximum +1 admitted")
	}
	for _, dimension := range []string{"writes", "renames", "prelude"} {
		t.Run(dimension, func(t *testing.T) {
			v := homeMoveSemanticEnvelope(t, 1)
			switch dimension {
			case "writes":
				for len(v.Writes) < 2000 {
					v.Writes = append(v.Writes[:len(v.Writes)-1], homeWrite{Path: "/ordinary", Before: "absent", Data: []byte{}}, v.Writes[len(v.Writes)-1])
				}
			case "renames":
				for len(v.Renames) < 10000 {
					v.Renames = append(v.Renames, homeRename{"/unused", "/unused-next", "5:1"})
				}
			case "prelude":
				v.Prelude = []homeWrite{{Path: "/launcher", Before: "absent", Data: []byte{}}}
			}
			at, e := marshalHomeTransaction(v)
			if e != nil {
				t.Fatal("retained count maximum", e)
			}
			if _, e = decodeHomeTransaction(at); e != nil {
				t.Fatal(e)
			}
			switch dimension {
			case "writes":
				v.Writes = append(v.Writes, homeWrite{Path: "/extra", Before: "absent"})
			case "renames":
				v.Renames = append(v.Renames, homeRename{"/extra", "/extra-next", "5:1"})
			case "prelude":
				v.Prelude = append(v.Prelude, v.Prelude[0])
			}
			if _, e = marshalHomeTransaction(v); e == nil {
				t.Fatal("count +1 emitted")
			}
			b, e := json.Marshal(v)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = decodeHomeTransaction(b); e == nil {
				t.Fatal("count +1 admitted")
			}
		})
	}
}

func TestDialectArtifactHomeMoveStrictRetainedMutations(t *testing.T) {
	tx := homeMoveSemanticEnvelope(t, 1)
	raw, err := marshalHomeTransaction(tx)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, old, replacement string }{
		{"unknown", `"retained":{"version":2,`, `"retained":{"unexpected":0,"version":2,`},
		{"duplicate", `"retained":{"version":2,`, `"retained":{"version":2,"version":2,`},
		{"case", `"projects":`, `"Projects":`},
		{"project-unknown", `"folder":"desk-0",`, `"folder":"desk-0","unexpected":0,`},
		{"pin-duplicate", `"table":"owners",`, `"table":"owners","table":"owners",`},
		{"row-case", `"beforeKind":"present",`, `"BeforeKind":"present",`},
		{"row-hash", `"afterSHA256":"`, `"afterSHA256":"f`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := bytes.Replace(raw, []byte(tc.old), []byte(tc.replacement), 1)
			if bytes.Equal(b, raw) {
				t.Fatal("mutation not exercised")
			}
			if _, e := decodeHomeTransaction(b); e == nil {
				t.Fatal("malformed retained control admitted")
			}
		})
	}
	for _, member := range []string{"projects", "pins", "rows", "renameParents"} {
		t.Run("null-"+member, func(t *testing.T) {
			var envelope map[string]json.RawMessage
			var retained map[string]json.RawMessage
			if e := json.Unmarshal(raw, &envelope); e != nil {
				t.Fatal(e)
			}
			if e := json.Unmarshal(envelope["retained"], &retained); e != nil {
				t.Fatal(e)
			}
			retained[member] = json.RawMessage(`null`)
			envelope["retained"], err = json.Marshal(retained)
			if err != nil {
				t.Fatal(err)
			}
			b, e := json.Marshal(envelope)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = decodeHomeTransaction(b); e == nil {
				t.Fatal("null retained member admitted")
			}
		})
	}
	for _, kind := range []string{"duplicate-project", "duplicate-pin", "duplicate-row", "write-index", "row-third-bytes"} {
		t.Run(kind, func(t *testing.T) {
			v := homeMoveSemanticEnvelope(t, 1)
			switch kind {
			case "duplicate-project":
				v.Retained.Projects = append(v.Retained.Projects, v.Retained.Projects[0])
			case "duplicate-pin":
				v.Retained.Pins = append(v.Retained.Pins, v.Retained.Pins[0])
			case "duplicate-row":
				v.Retained.Rows = append(v.Retained.Rows, v.Retained.Rows[0])
			case "write-index":
				v.Retained.Rows[0].Write = len(v.Writes) - 1
			case "row-third-bytes":
				v.Retained.Rows[0].After = []byte(`{}`)
			}
			b, e := json.Marshal(v)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = decodeHomeTransaction(b); e == nil {
				t.Fatal("semantic mutation admitted")
			}
		})
	}
}

func TestDialectArtifactHomeMoveRemainingFaultPhases(t *testing.T) {
	for _, phase := range []string{"home-move-stage.close", "home-move-row.before-cas", "third-bytes", "ordinary-write", "pointer-landed", "home-move-config.close", "home-move-journal-root.close"} {
		t.Run(phase, func(t *testing.T) {
			journal, tx, pointer := queuedDialectArtifactHomeMoveForAcknowledgement(t)
			fired := 0
			install := func() {
				testHookDialectArtifactOwnerStep = func(at string) error {
					target := phase
					if phase == "third-bytes" {
						target = "home-move-row.before-cas"
					}
					if at == target {
						fired++
						return errors.New("qualification interruption")
					}
					return nil
				}
				testHomeMigrationStep = func(at string) error {
					index := len(tx.Writes) - 1
					if phase == "ordinary-write" {
						index = -1
						for i := range tx.Writes[:len(tx.Writes)-1] {
							retained := false
							for _, r := range tx.Retained.Rows {
								retained = retained || r.Write == i
							}
							if !retained {
								index = i
								break
							}
						}
						if index < 0 {
							t.Fatal("fixture lacks ordinary write")
						}
					}
					if (phase == "ordinary-write" || phase == "pointer-landed") && at == fmt.Sprintf("write-%d", index) {
						fired++
						return errors.New("qualification write interruption")
					}
					return nil
				}
			}
			install()
			for attempt := 0; attempt < 2; attempt++ {
				previous := fired
				if err := finishHomeTransaction(journal); err == nil || fired == previous {
					t.Fatal("fault not observed", phase, attempt, err)
				}
				pending, e := readHomeTransaction(journal)
				if e != nil || pending == nil || pending.Complete {
					t.Fatal("interrupted journal completed", e)
				}
				landed := phase == "pointer-landed" || phase == "home-move-config.close" || phase == "home-move-journal-root.close"
				b, e := os.ReadFile(pointer)
				if landed {
					if e != nil || !bytes.Equal(b, tx.Writes[len(tx.Writes)-1].Data) {
						t.Fatal("landed pointer changed", e)
					}
				} else if !errors.Is(e, os.ErrNotExist) {
					t.Fatal("pointer published early", e)
				}
				if phase == "home-move-stage.close" || phase == "home-move-row.before-cas" || phase == "third-bytes" {
					r := tx.Retained.Rows[0]
					path := homeMoveRowPath(filepath.Join(tx.Destination, deskHomePlatform), r.Table, r.ID)
					b = dialectArtifactOwnerRead(t, path)
					if !bytes.Equal(b, r.Before) {
						t.Fatal("BEFORE row changed before CAS")
					}
				}
			}
			testHookDialectArtifactOwnerStep = nil
			testHomeMigrationStep = nil
			if phase == "third-bytes" {
				r := tx.Retained.Rows[0]
				path := homeMoveRowPath(filepath.Join(tx.Destination, deskHomePlatform), r.Table, r.ID)
				third := []byte(`{"third":true}`)
				if e := os.WriteFile(path, third, 0600); e != nil {
					t.Fatal(e)
				}
				for i := 0; i < 2; i++ {
					if e := finishHomeTransaction(journal); e == nil {
						t.Fatal("third bytes recovered")
					}
					assertDialectArtifactHomeMoveUnacknowledged(t, journal, pointer)
					if !bytes.Equal(dialectArtifactOwnerRead(t, path), third) {
						t.Fatal("third bytes overwritten")
					}
				}
				return
			}
			assertDialectArtifactHomeMoveAcknowledged(t, journal, pointer)
			for _, row := range tx.Retained.Rows {
				path := homeMoveRowPath(filepath.Join(tx.Destination, deskHomePlatform), row.Table, row.ID)
				if !bytes.Equal(dialectArtifactOwnerRead(t, path), row.After) {
					t.Fatal("successful recovery lacks exact AFTER row")
				}
			}
		})
	}
}

func TestDialectArtifactHomeMoveForeignAbsoluteSharedWork(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg, _ := homeFixture(t)
	// Initial consolidation starts with plain separate settings. Both owners
	// enroll before any home binding; the foreign project is never registered.
	cfg.ProjectDir = t.TempDir()
	cfg.DeskConfigDir = t.TempDir()
	privateFixture(t, filepath.Join(cfg.ProjectDir, projectConfigName), `{"deskConfigVersion":1}`)
	cfg.HomeMoveShutdown = func() {}
	s, ts := startDesk(t, cfg)
	t.Cleanup(func() { ts.Close(); s.Close() })
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal(code)
	}
	if s.home != nil {
		t.Fatal("fixture bound a home before initial consolidation")
	}
	stampMoveIdentity(t, s)
	selected := enrollDialectArtifactMoveServer(t, s)
	foreign, _ := dialectArtifactOwnerFixtureWithStorage(t, t.TempDir(), s.configDir, true, false)
	foreign.cfg.parent = s
	enrolled := enrollDialectArtifactMoveServer(t, foreign)
	if enrolled.WorkDomain != selected.WorkDomain || foreign.configDir != s.configDir {
		t.Fatal("fixture does not share moved work domain")
	}
	workLock, e := os.Stat(filepath.Join(s.configDir, ".data-work.lock"))
	if e != nil {
		t.Fatal(e)
	}
	ownerPath, scopePath := dialectArtifactOwnerRowPaths(foreign, enrolled)
	locator := dialectArtifactLocationPath(foreign, enrolled.Owner)
	poison := homeMoveRowPath(s.configDir, "owners", strings.Repeat("f", 64))
	if e = os.MkdirAll(filepath.Dir(poison), 0700); e != nil {
		t.Fatal(e)
	}
	privateFixture(t, poison, `{"unrelated":"malformed owner must not be read"}`)
	originals := map[string][]byte{poison: dialectArtifactOwnerRead(t, poison)}
	for _, path := range []string{ownerPath, scopePath, locator} {
		originals[path] = dialectArtifactOwnerRead(t, path)
	}
	loc, e := readDialectArtifactLocationRecord(originals[locator])
	if e != nil || loc.Location.Kind != "absolute" {
		t.Fatal("foreign locator is not absolute", e)
	}
	object := []byte("foreign retained bytes\n\x00\xff")
	ref, e := foreign.artifactRepository().Put(context.Background(), object)
	if e != nil {
		t.Fatal(e)
	}
	destination := filepath.Join(t.TempDir(), "foreign-home")
	if relative, err := filepath.Rel(destination, foreign.projectDir); err != nil || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("foreign project is inside destination", err)
	}
	plan, e := s.planDeskHome(destination)
	if e != nil || len(plan.Blockers) != 0 || plan.Retained == nil {
		t.Fatal("plan", plan.Blockers, e)
	}
	for _, p := range plan.Retained.Projects {
		if p.Owner == enrolled.Owner {
			t.Fatal("foreign owner selected")
		}
	}
	for _, p := range plan.Retained.Pins {
		if p.ID == enrolled.Owner {
			t.Fatal("foreign owner pinned")
		}
	}
	for _, r := range plan.Retained.Rows {
		if r.ID == enrolled.Owner {
			t.Fatal("foreign locator rewritten")
		}
	}
	if e = s.queueHomeMove(context.Background(), projectMoveRequest{Destination: destination, Token: plan.Token}); e != nil {
		t.Fatal(e)
	}
	journal := homeJournalPath(s.configDir)
	tx, e := readHomeTransaction(journal)
	if e != nil {
		t.Fatal(e)
	}
	workCount := 0
	var workRow dialectArtifactHomeMoveRow
	for _, r := range tx.Retained.Rows {
		if r.Table == "work-domains" {
			workCount++
			workRow = r
		}
	}
	if workCount != 1 {
		t.Fatal("shared work row not selected once", workCount)
	}
	ts.Close()
	s.Close()
	if e = finishHomeTransaction(journal); e != nil {
		t.Fatal(e)
	}
	if bytes.Equal(workRow.Before, workRow.After) || !bytes.Equal(dialectArtifactOwnerRead(t, homeMoveRowPath(filepath.Join(destination, deskHomePlatform), workRow.Table, workRow.ID)), workRow.After) {
		t.Fatal("shared work row did not change to exact AFTER bytes")
	}
	for path, before := range originals {
		if !bytes.Equal(dialectArtifactOwnerRead(t, tx.relocate(path)), before) {
			t.Fatal("foreign immutable row changed")
		}
	}
	// Reopen the actual selected startup project with its completed home binding.
	// Typed selected metadata resolves the foreign absolute owner independently
	// of registry membership, using only its owner/location/work rows.
	cfg.DeskConfigDir = filepath.Join(destination, deskHomePlatform)
	for _, p := range tx.Retained.Projects {
		if p.Owner == selected.Owner {
			cfg.ProjectDir = string(p.To)
		}
	}
	viewer, vts := startDesk(t, cfg)
	t.Cleanup(func() { vts.Close(); viewer.Close() })
	if viewer.home == nil || viewer.home.entry(foreign.projectDir) != nil {
		t.Fatal("foreign project entered the selected home registry")
	}
	source, works := selectedNativeSourceFixture(t, viewer, enrolled)
	if e = works.acquire(t.Context()); e != nil {
		t.Fatal(e)
	}
	if e = source.check(t.Context(), true); e != nil {
		t.Fatal(e)
	}
	if source.location.Location.Kind != "absolute" || source.project.dir != foreign.projectDir || !os.SameFile(source.info, foreign.project.info) || string(source.work.record.PathBytes) != filepath.Join(destination, deskHomePlatform) || source.work.record.ID != enrolled.WorkDomain || source.work.record.LockIdentity != identityKey(workLock) || !os.SameFile(source.work.lockInfo, workLock) {
		t.Fatal("foreign source/work selection changed")
	}
	got, e := retainedArtifactsForProject(source.root).Get(t.Context(), ref)
	if e != nil || !bytes.Equal(got, object) {
		t.Fatal("actual foreign read after move", e)
	}
}
