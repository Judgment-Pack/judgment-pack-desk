package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Handwritten source families enter the actual file collection/journal owners.
// These are inventory/custody witnesses, not executed acquisition or a complete
// production snapshot/root publication/factory qualification.
func completeReadSourceFixture(t *testing.T, revisions, fields, description int) (*Server, inputPreparationReply) {
	t.Helper()
	project, err := OpenProjectRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := project.Close(); err != nil {
			t.Error(err)
		}
	})
	s := &Server{project: project, root: project.own.root, projectDir: project.dir}
	const stamp = "2026-10-30T00:00:00Z"
	doc := inputPreparation{Version: 1, ID: "prep_" + strings.Repeat("a", 32), Name: "Complete source", CreatedAt: stamp, UpdatedAt: stamp,
		Records: []preparedInputRecord{}, Recipes: []inputRecipeRevision{}, Imports: []inputImport{}, Activity: []inputActivity{}}
	root, err := s.inputResearchRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for revision := 1; revision <= revisions; revision++ {
		recipe := researchRecipeFixture()
		recipe.Fields = []inputResearchField{}
		for field := 0; field < fields; field++ {
			name := "f" + strings.Repeat("a", 61) + fmt.Sprintf("%02d", field)
			prefix := fmt.Sprintf("r%03d ", revision)
			recipe.Fields = append(recipe.Fields, inputResearchField{Name: name, Description: prefix + strings.Repeat("x", description-len(prefix)), Type: "text"})
		}
		runID := fmt.Sprintf("research_%032x", revision)
		run := inputResearchRun{Version: 1, ID: runID, PreparationID: doc.ID, Revision: fmt.Sprintf("%064x", revision), RecipeVersion: revision, Recipe: recipe, State: "completed", CreatedAt: stamp, UpdatedAt: stamp,
			Intent: "Retain original declarations", Seeds: []string{}, Companies: []inputResearchEntity{}, Records: []inputResearchEntity{}, Sources: []inputResearchSource{}, Attempts: []inputResearchAttempt{}}
		dir, err := openInputResearch(root, runID, true)
		if err != nil {
			t.Fatal(err)
		}
		err = (fileResearchJournal{root: dir, id: runID}).Create(t.Context(), run)
		closeErr := dir.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		doc.Recipe = inputRecipe{Intent: run.Intent, Research: &recipe}
		doc.RecipeVersion = revision
		doc.Recipes = append(doc.Recipes, inputRecipeRevision{Version: revision, Name: "Original", Recipe: doc.Recipe, At: stamp})
		doc.Records = append(doc.Records, preparedInputRecord{ID: fmt.Sprintf("row_%064x", revision), Name: "Original", Class: "generated", Status: "researched", Values: map[string]string{}, SourceID: runID, Research: &inputResearchRecord{RunID: runID, RecipeVersion: revision}})
	}
	saved, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, "")
	if err != nil {
		t.Fatal(err)
	}
	return s, saved
}

func completeReadSnapshot(t *testing.T, id, revision string, count int, inventory assistantDialectCompleteInventory) runcontext.DialectSnapshot {
	t.Helper()
	index, err := runcontext.DialectResearchIndexOf(inventory.Declarations)
	if err != nil {
		t.Fatal(err)
	}
	return runcontext.DialectSnapshot{ID: id, Revision: revision, PackKey: "example", Name: "Complete inventory", RecordCount: int64(count), PackJSON: `{}`, ResearchIndex: index,
		Columns: []runcontext.DialectColumn{}, FactTargets: []string{}, EvidenceRequirements: []string{}, Conversions: map[string]runcontext.DialectConversion{}}
}

func withCompleteReadFixture(t *testing.T, s *Server, id string, use func(*assistantDialectCompleteSourceHold) error) error {
	t.Helper()
	view, err := s.preparationRepository().Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	return s.withAssistantDialectCompleteInventory(t.Context(), view, id, use)
}

func TestAssistantDialectCompleteReadOwnedInventoryDomain(t *testing.T) {
	for _, family := range []struct{ revisions, description int }{{150, 1000}, {450, 100}, {1000, 20}} {
		t.Run(fmt.Sprint(family.revisions), func(t *testing.T) {
			s, saved := completeReadSourceFixture(t, family.revisions, 30, family.description)
			err := withCompleteReadFixture(t, s, saved.Document.ID, func(h *assistantDialectCompleteSourceHold) error {
				if len(h.inventory.Declarations) != family.revisions || len(h.inventory.Sources) != family.revisions {
					t.Fatal("original revision/source omission")
				}
				for _, revision := range h.inventory.Declarations {
					if len(revision.Fields) != 30 {
						t.Fatal("original field omission")
					}
				}
				// The original borrowed view remains usable: its mutex is not held
				// across the callback. The source Get independently rereads bytes.
				if _, err := h.view.Get(saved.Document.ID); err != nil {
					return err
				}
				// Supplied Get remains usable and freshly decodes exact held bytes.
				current, err := h.Get(saved.Document.ID)
				if err != nil || current.SHA256 != saved.SHA256 {
					t.Fatal(err)
				}
				snapshot := completeReadSnapshot(t, saved.Document.ID, saved.SHA256, len(saved.Document.Records), h.inventory)
				raw, err := h.encode(t.Context(), snapshot)
				if err != nil {
					return err
				}
				decoded, err := readAssistantDialectCompleteRead(t.Context(), raw)
				if err != nil {
					return err
				}
				all, err := runcontext.ReadDialectDeclarations(t.Context(), decoded.fieldIndex, decoded.declarations, snapshot.ID, snapshot.PackKey, snapshot.Revision, snapshot.ResearchIndex)
				if err != nil || !reflect.DeepEqual(all, h.inventory.Declarations) {
					t.Fatal("released012 index/page reconstruction:", err)
				}
				t.Logf("actualFileJoins=%d originalFields=%d sourceBytes=%d carrierBytes=%d", len(h.inventory.Sources), family.revisions*30, len(h.collection.raw), len(raw))
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func completeReadPassive(t *testing.T, fields []runcontext.DialectResearchField, availability string) (assistantDialectCompleteInventory, runcontext.DialectSnapshot) {
	t.Helper()
	id := "prep_" + strings.Repeat("a", 32)
	revision := strings.Repeat("b", 64)
	var original *string
	if availability == "known" {
		value := strings.Repeat("c", 64)
		original = &value
	}
	inventory := assistantDialectCompleteInventory{Declarations: []runcontext.DialectResearchRevision{{Revision: assistantDialectCompleteRevision(id, 1), Fields: fields}}, Sources: []assistantDialectCompleteSource{{RecordID: "row", RunID: "research_" + strings.Repeat("d", 32), RunRevision: original, RecipeVersion: 1, Availability: availability, Inventory: 0}}}
	return inventory, completeReadSnapshot(t, id, revision, 1, inventory)
}

func TestAssistantDialectCompleteReadExactSpansAndClosedControls(t *testing.T) {
	// Old exact string spellings remain exact even when not issued by the new
	// serializer. Whitespace/HTML spelling and original inner escapes matter.
	field := `{ "name":"country", "description":"Keep <>& and \u2028 and 😀", "type":"text" }`
	inv, snapshot := completeReadPassive(t, []runcontext.DialectResearchField{{Name: "country", FieldJSON: field}}, "known")
	raw, err := encodeAssistantDialectCompleteRead(t.Context(), snapshot, inv)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(field)) {
		t.Fatal("raw declaration was rewritten")
	}
	decoded, err := readAssistantDialectCompleteRead(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = ' '
	if decoded.Inventory.Declarations[0].Fields[0].FieldJSON != field {
		t.Fatal("span changed with caller bytes")
	}
	newRaw, err := assistantDialectCompleteJSON(inputResearchField{Name: "country", Description: "<>&\u2028\u2029😀", Type: "text"})
	if err != nil || !bytes.Contains(newRaw, []byte("<>&")) || !bytes.Contains(newRaw, []byte(`\u2028\u2029`)) {
		t.Fatal("new selected serializer:", string(newRaw), err)
	}
	good, err := encodeAssistantDialectCompleteRead(t.Context(), snapshot, inv)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string][]byte{
		"duplicate":      bytes.Replace(good, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		"alias":          bytes.Replace(good, []byte(`"version":1`), []byte(`"Version":1`), 1),
		"null":           bytes.Replace(good, []byte(`"version":1`), []byte(`"version":null`), 1),
		"version":        bytes.Replace(good, []byte(`"version":1`), []byte(`"version":2`), 1),
		"extra":          bytes.Replace(good, []byte(`"version":1`), []byte(`"version":1,"extra":0`), 1),
		"sourceRevision": bytes.Replace(good, []byte(strings.Repeat("c", 64)), []byte("opaque"), 1),
		"wrongIndex":     bytes.Replace(good, []byte(`"inventory":0`), []byte(`"inventory":1`), 1),
		"nullSources":    bytes.Replace(good, []byte(`"originalSources":[`), []byte(`"originalSources":null,"unused":[`), 1),
		"mixedSnapshot":  bytes.Replace(good, []byte(`"snapshot":{`), []byte(`"snapshot":{"researchDeclarations":[],`), 1),
		"rawFieldExtra":  bytes.Replace(good, []byte(`"description":"Keep`), []byte(`"extra":0,"description":"Keep`), 1),
	}
	for name, bad := range mutations {
		t.Run(name, func(t *testing.T) {
			if _, err := readAssistantDialectCompleteRead(t.Context(), bad); err == nil {
				t.Fatal("malformed control accepted")
			}
		})
	}
}

func TestAssistantDialectCompleteReadNilEmptyAndPageBindings(t *testing.T) {
	for _, availability := range []string{"known", "unavailable"} {
		var fields []runcontext.DialectResearchField
		if availability == "known" {
			fields = []runcontext.DialectResearchField{}
		}
		inv, snapshot := completeReadPassive(t, fields, availability)
		raw, err := encodeAssistantDialectCompleteRead(t.Context(), snapshot, inv)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := readAssistantDialectCompleteRead(t.Context(), raw)
		if err != nil || (decoded.Inventory.Declarations[0].Fields == nil) != (fields == nil) {
			t.Fatal("null/empty distinction:", err)
		}
		if _, err := decoded.fieldIndex(t.Context(), runcontext.DialectFieldIndexRequest{ID: snapshot.ID, PackKey: "wrong", Revision: snapshot.Revision, InventoryDigest: snapshot.ResearchIndex.Digest, Entry: 0}); err == nil {
			t.Fatal("foreign page binding")
		}
	}
	for _, inventory := range []assistantDialectCompleteInventory{{Sources: []assistantDialectCompleteSource{}}, {Declarations: []runcontext.DialectResearchRevision{}, Sources: []assistantDialectCompleteSource{}}} {
		snapshot := completeReadSnapshot(t, "prep_"+strings.Repeat("a", 32), strings.Repeat("b", 64), 0, inventory)
		raw, err := encodeAssistantDialectCompleteRead(t.Context(), snapshot, inventory)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := readAssistantDialectCompleteRead(t.Context(), raw)
		if err != nil || (decoded.Inventory.Declarations == nil) != (inventory.Declarations == nil) {
			t.Fatal("top unavailable/empty:", err)
		}
	}
}

func TestAssistantDialectCompleteReadDuplicateRecordClaims(t *testing.T) {
	for _, kind := range []string{"identical", "conflicting", "known-unavailable", "unavailable-known", "unavailable-identical"} {
		t.Run(kind, func(t *testing.T) {
			inv, _ := completeReadPassive(t, []runcontext.DialectResearchField{}, "known")
			first := inv.Sources[0]
			second := first
			second.RecordID = "row_second"
			if kind == "conflicting" {
				second.RunID = "research_" + strings.Repeat("f", 32)
			}
			if kind == "known-unavailable" || kind == "unavailable-known" {
				inv.Declarations = append(inv.Declarations, runcontext.DialectResearchRevision{Revision: assistantDialectCompleteRevision("prep_"+strings.Repeat("a", 32), 2), Fields: nil})
				unknown := assistantDialectCompleteSource{RecordID: "row_second", RunID: "research_" + strings.Repeat("e", 32), RecipeVersion: 2, Availability: "unavailable", Inventory: 1}
				if kind == "known-unavailable" {
					second = unknown
				} else {
					first = unknown
					first.RecordID = "row"
				}
			}
			if kind == "unavailable-identical" {
				inv, _ = completeReadPassive(t, nil, "unavailable")
				first = inv.Sources[0]
				second = first
				second.RecordID = "row_second"
			}
			inv.Sources = []assistantDialectCompleteSource{first, second}
			snapshot := completeReadSnapshot(t, "prep_"+strings.Repeat("a", 32), strings.Repeat("b", 64), 2, inv)
			good, err := encodeAssistantDialectCompleteRead(t.Context(), snapshot, inv)
			if err != nil {
				t.Fatal("distinct original records refused:", err)
			}
			if _, err = readAssistantDialectCompleteRead(t.Context(), good); err != nil {
				t.Fatal("distinct decoder twin:", err)
			}
			// Change only the recorded identity. Snapshot, index, availability
			// and each join are independently well formed, so this catches the
			// duplicate rather than relying on a conflicting package digest.
			bad := bytes.Replace(good, []byte(`"recordId":"row_second"`), []byte(`"recordId":"row"`), 1)
			if bytes.Equal(good, bad) {
				t.Fatal("handwritten mutation did not select recordId")
			}
			if _, err = readAssistantDialectCompleteRead(t.Context(), bad); err == nil {
				t.Fatal("duplicate original record claim decoded")
			}
			inv.Sources[1].RecordID = inv.Sources[0].RecordID
			if _, err = encodeAssistantDialectCompleteRead(t.Context(), snapshot, inv); err == nil {
				t.Fatal("duplicate original record claim encoded")
			}
		})
	}
}

func TestAssistantDialectCompleteReadDuplicateSourceProducer(t *testing.T) {
	s, saved := completeReadSourceFixture(t, 2, 1, 20)
	doc := saved.Document
	// Author a duplicate source identity through the unchanged file owner;
	// these handwritten records have no inherited cell-custody mutation seal.
	doc.cellCustody = nil
	for i := range doc.Records {
		doc.Records[i].rawCells = nil
	}
	doc.Records[1].ID = doc.Records[0].ID
	duplicate, err := s.preparationRepository().CompareAndSwap(t.Context(), doc, saved.SHA256)
	if err != nil {
		t.Fatal("generic historical source behavior changed:", err)
	}
	legacy, err := s.preparationRepository().Get(t.Context(), duplicate.Document.ID)
	if err != nil || len(legacy.Document.Records) != 2 || legacy.Document.Records[0].ID != legacy.Document.Records[1].ID {
		t.Fatal("generic source no longer readable:", err)
	}
	called := false
	err = withCompleteReadFixture(t, s, duplicate.Document.ID, func(*assistantDialectCompleteSourceHold) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("ambiguous source was promoted to complete original evidence")
	}
}

type completeDetachedReader struct{ saved inputPreparationReply }

func (v completeDetachedReader) Get(string) (inputPreparationReply, error) { return v.saved, nil }

type completeUnexpectedJournals struct{}

func (completeUnexpectedJournals) Run(string) researchJournal {
	panic("injected provider must not be replaced or invoked")
}

func TestAssistantDialectCompleteReadSourceAuthorityAndCurrentness(t *testing.T) {
	for _, mode := range []string{"detached", "foreignView", "injectedCollection", "injected", "busyJournal", "collectionBytes", "journalBytes", "journalRoot", "preparationRoot", "cancelled", "closedView", "badProvenance", "closeFailure", "currentSnapshot"} {
		t.Run(mode, func(t *testing.T) {
			s, saved := completeReadSourceFixture(t, 1, 1, 20)
			view, err := s.preparationRepository().Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			reader := inputPreparationReader(view)
			var exclusion *os.File
			switch mode {
			case "detached":
				reader = completeDetachedReader{saved}
			case "foreignView":
				other, doc := completeReadSourceFixture(t, 1, 1, 20)
				foreign, e := other.preparationRepository().Read(t.Context())
				if e != nil {
					t.Fatal(e)
				}
				defer foreign.Close()
				_ = doc
				reader = foreign
			case "injectedCollection":
				s.inputPreparations = &memoryPreparationRepository{data: map[string][]byte{}}
			case "injected":
				s.researchJournals = completeUnexpectedJournals{}
			case "busyJournal":
				exclusion, err = os.Open(filepath.Join(s.projectDir, inputResearchDirectory, saved.Document.Records[0].Research.RunID))
				if err != nil {
					t.Fatal(err)
				}
				defer exclusion.Close()
				if err = lockPrivateData(exclusion, true); err != nil {
					t.Fatal(err)
				}
			case "badProvenance":
				path := filepath.Join(s.projectDir, inputResearchDirectory, saved.Document.Records[0].Research.RunID, "record.json")
				raw, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				raw = bytes.Replace(raw, []byte(fmt.Sprintf("%064x", 1)), []byte("opaque"), 1)
				if e = os.WriteFile(path, raw, 0600); e != nil {
					t.Fatal(e)
				}
			case "closeFailure":
				testHookDialectArtifactOwnerStep = func(step string) error {
					if step == "complete-collection-file.close" {
						return errors.New("injected close failure")
					}
					return nil
				}
				defer func() { testHookDialectArtifactOwnerStep = nil }()
			}
			called := false
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			err = s.withAssistantDialectCompleteInventory(ctx, reader, saved.Document.ID, func(h *assistantDialectCompleteSourceHold) error {
				called = true
				switch mode {
				case "collectionBytes":
					return os.WriteFile(filepath.Join(s.projectDir, inputPreparationDir, saved.Document.ID+".json"), append(bytes.Clone(h.collection.raw), ' '), 0600)
				case "journalBytes":
					j := h.journals[h.order[0]]
					return os.WriteFile(filepath.Join(s.projectDir, inputResearchDirectory, h.order[0], "record.json"), append(bytes.Clone(j.file.raw), ' '), 0600)
				case "journalRoot":
					path := filepath.Join(s.projectDir, inputResearchDirectory, h.order[0])
					return os.Rename(path, path+"-moved")
				case "preparationRoot":
					path := filepath.Join(s.projectDir, inputPreparationDir)
					return os.Rename(path, path+"-moved")
				case "cancelled":
					cancel()
					return nil
				case "closedView":
					return view.Close()
				case "currentSnapshot":
					snapshot := completeReadSnapshot(t, saved.Document.ID, strings.Repeat("f", 64), 1, h.inventory)
					_, e := h.encode(t.Context(), snapshot)
					return e
				}
				return nil
			})
			if err == nil {
				t.Fatal("changed/unowned source acknowledged")
			}
			if contains([]string{"detached", "foreignView", "injectedCollection", "injected", "busyJournal", "badProvenance"}, mode) && called {
				t.Fatal("entered callback without source custody")
			}
		})
	}
}

func TestAssistantDialectCompleteReadSourceAndCarrierBounds(t *testing.T) {
	s, saved := completeReadSourceFixture(t, 1, 1, 20)
	for _, kind := range []string{"collection", "journal"} {
		t.Run(kind, func(t *testing.T) {
			var path string
			var raw []byte
			var limit int
			if kind == "collection" {
				doc := saved.Document
				doc.Name = ""
				base, e := json.Marshal(doc)
				if e != nil {
					t.Fatal(e)
				}
				doc.Name = strings.Repeat("x", inputPreparationLimit-len(base))
				raw, e = json.Marshal(doc)
				if e != nil {
					t.Fatal(e)
				}
				limit = inputPreparationLimit
				path = filepath.Join(s.projectDir, inputPreparationDir, saved.Document.ID+".json")
			} else {
				path = filepath.Join(s.projectDir, inputResearchDirectory, saved.Document.Records[0].Research.RunID, "record.json")
				original, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				var run inputResearchRun
				if json.Unmarshal(original, &run) != nil {
					t.Fatal("fixture")
				}
				run.Intent = ""
				base, e := json.Marshal(run)
				if e != nil {
					t.Fatal(e)
				}
				run.Intent = strings.Repeat("x", inputResearchRecordLimit-len(base))
				raw, e = json.Marshal(run)
				if e != nil {
					t.Fatal(e)
				}
				limit = inputResearchRecordLimit
			}
			if len(raw) != limit {
				t.Fatal("handwritten exact boundary", len(raw), limit)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := withCompleteReadFixture(t, s, saved.Document.ID, func(*assistantDialectCompleteSourceHold) error { return nil }); err != nil {
				t.Fatal("inclusive actual source bound:", err)
			}
			if err := os.WriteFile(path, append(raw, ' '), 0600); err != nil {
				t.Fatal(err)
			}
			if err := withCompleteReadFixture(t, s, saved.Document.ID, func(*assistantDialectCompleteSourceHold) error { t.Fatal("over-limit callback"); return nil }); err == nil {
				t.Fatal("over source bound")
			}
			// Restore this selected fixture before the next independent boundary.
			if kind == "collection" {
				original, e := json.Marshal(saved.Document)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(path, original, 0600); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
	inv, snapshot := completeReadPassive(t, []runcontext.DialectResearchField{}, "known")
	raw, err := encodeAssistantDialectCompleteRead(t.Context(), snapshot, inv)
	if err != nil {
		t.Fatal(err)
	}
	// Outer whitespace is legal but not emitted; this checks the real codec
	// inclusive guard, not a claim a source producer reaches32MiB.
	padded := append(bytes.Clone(raw), bytes.Repeat([]byte{' '}, assistantDialectCompleteReadBytes-len(raw))...)
	if _, err = readAssistantDialectCompleteRead(context.Background(), padded); err != nil {
		t.Fatal("inclusive carrier guard:", err)
	}
	if _, err = readAssistantDialectCompleteRead(context.Background(), append(padded, ' ')); err == nil {
		t.Fatal("over carrier guard")
	}
}
