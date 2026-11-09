package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// The history is constructed from this handwritten parameter family, not
// recorded from an implementation. Each original revision has one actual
// retained generated record and a complete distinct ordered field inventory.
const (
	d0aRevisions         = 150
	d0aFieldsPerRevision = 30
	d0aDescriptionBytes  = 1000
)

func d0aFactoryDomain(t *testing.T, bundle string) {
	s, _ := newTestServer(t, false)
	s.cfg.LocalGatewayBundle = bundle
	q := qualificationFixture()
	q.Mapping.Case.Facts = []inputDialectFact{{Target: "/company/country", Source: "/values/field_00"}}
	q.Mapping.Case.Evidence = []inputDialectEvidence{}
	q.Conversions = map[string]inputConversion{"/company/country": {Kind: "text"}}
	var pack map[string]any
	if err := json.Unmarshal([]byte(q.Pack), &pack); err != nil {
		t.Fatal(err)
	}
	pack["evidenceRequirements"] = []any{}
	q.Pack = string(d0aJSON(t, pack))
	q.Digest = sha256Digest([]byte(q.Pack))
	if err := validateInputQualification(q); err != nil {
		t.Fatal(err)
	}
	doc := inputPreparation{Version: 1, ID: "prep_" + strings.Repeat("c", 32), Name: "D0a complete original revision inventory", CreatedAt: d0aStamp, UpdatedAt: d0aStamp, Records: []preparedInputRecord{}, Recipes: []inputRecipeRevision{}, Imports: []inputImport{}, Activity: []inputActivity{}}
	for revision := 1; revision <= d0aRevisions; revision++ {
		recipe := researchRecipeFixture()
		recipe.Fields = []inputResearchField{}
		values := map[string]inputResearchValue{}
		for field := 0; field < d0aFieldsPerRevision; field++ {
			name := fmt.Sprintf("field_%02d", field)
			prefix := fmt.Sprintf("Original revision %03d field %02d. ", revision, field)
			description := prefix + strings.Repeat("x", d0aDescriptionBytes-len(prefix))
			recipe.Fields = append(recipe.Fields, inputResearchField{Name: name, Type: "text", Description: description})
			values[name] = inputResearchValue{Value: json.RawMessage(`"x"`), Status: "interpretation", Sources: []inputResearchCitation{}}
		}
		if err := validateInputResearch(&recipe); err != nil {
			t.Fatalf("legal original recipe %d: %v", revision, err)
		}
		url := fmt.Sprintf("https://revision-%03d.example.com/", revision)
		run := inputResearchRun{Version: 1, ID: fmt.Sprintf("research_%032x", revision), PreparationID: doc.ID, RecipeVersion: revision, Recipe: recipe, State: "completed", CreatedAt: d0aStamp, UpdatedAt: d0aStamp, Intent: "Complete original history family", Seeds: []string{url}, Sources: []inputResearchSource{}, Records: []inputResearchEntity{}, Attempts: []inputResearchAttempt{}}
		args := d0aJSON(t, map[string]any{"records": []inputResearchEntity{{Kind: "company", Name: fmt.Sprintf("Revision %03d", revision), URL: url, Fields: values}}})
		if err := applyResearchRecords(&run, args); err != nil {
			t.Fatalf("actual original model record %d: %v", revision, err)
		}
		run.Attempts = []inputResearchAttempt{{ID: "attempt_model", Kind: "model", State: "completed", Result: d0aJSON(t, inputResearchAction{Tool: "records", Arguments: args}), FinishedAt: d0aStamp}}
		d0aStoreRun(t, s, run, nil)
		original := inputRecipe{Intent: run.Intent, Research: &recipe, Qualification: q}
		doc.Recipes = append(doc.Recipes, inputRecipeRevision{Version: revision, Name: fmt.Sprintf("Original %03d", revision), Recipe: original, At: d0aStamp})
		doc.Records = append(doc.Records, researchPreparedRecord(run, run.Records[0]))
		doc.Recipe, doc.RecipeVersion = original, revision
	}
	// The real repository applies its complete 8 MiB encoded file admission and
	// validates the complete stored collection. No enlarged test-only cap is used.
	saved, err := s.preparationRepository().CompareAndSwap(context.Background(), doc, "")
	if err != nil {
		t.Fatalf("actual legal collection admission: %v", err)
	}
	saved = acceptFixture(t, s, saved)
	collectionRaw := d0aJSON(t, saved.Document)
	if len(collectionRaw) > inputPreparationLimit || len(saved.Document.Records) != d0aRevisions || len(saved.Document.Recipes) != d0aRevisions {
		t.Fatal("collection is not within the actual original storage domain")
	}
	rows, err := prepareInputSelection(saved.Document, []string{saved.Document.Records[0].ID})
	if err != nil || len(rows) != 1 || len(rows[0].Issues) != 0 {
		t.Fatalf("actual historical record selection: %v %#v", err, rows)
	}
	originalSnapshot, err := s.reviewedInputSnapshot(context.Background(), saved.Document, saved.Document.Records[0], rows[0])
	if err != nil {
		t.Fatalf("actual old-revision snapshot producer: %v", err)
	}
	dialectRaw := d0aJSON(t, q.Mapping)
	dialectText := string(dialectRaw)
	snapshot := runcontext.DialectSnapshot{ID: saved.Document.ID, Revision: saved.SHA256, PackKey: q.Key, Name: saved.Document.Name, RecordCount: int64(len(saved.Document.Records)), PackJSON: q.Pack, DialectJSON: &dialectText, Columns: []runcontext.DialectColumn{}, FactTargets: []string{"/company/country"}, EvidenceRequirements: []string{}, Conversions: map[string]runcontext.DialectConversion{"/company/country": {Kind: "text"}}, RetainedRead: &runcontext.DialectRetainedRead{DocumentJSON: dialectText, Occurrence: runcontext.DialectRetainedOccurrence{Kind: "case"}, FramePointer: "/retained"}}
	for field := 0; field < d0aFieldsPerRevision; field++ {
		snapshot.Columns = append(snapshot.Columns, runcontext.DialectColumn{Name: fmt.Sprintf("field_%02d", field), Samples: []runcontext.DialectSample{{ValueJSON: `"x"`, Origin: "generated"}}})
	}
	for _, name := range []string{"name", "url"} {
		snapshot.Columns = append(snapshot.Columns, runcontext.DialectColumn{Name: name, Samples: []runcontext.DialectSample{{ValueJSON: string(d0aJSON(t, saved.Document.Records[0].Values[name])), Origin: "generated"}}})
	}
	// Every retained record maps to exactly its original history entry and
	// original durable run. Inventory is deduplicated by collection recipe
	// version, not by a displayed sample or a current requested type.
	root, err := s.inputResearchRoot(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	represented := map[int]string{}
	originals := []runcontext.DialectResearchRevision{}
	for _, record := range saved.Document.Records {
		if record.Research == nil || record.Research.RecipeVersion < 1 || record.Research.RecipeVersion > len(saved.Document.Recipes) {
			t.Fatal("record lacks an original retained revision")
		}
		revision := record.Research.RecipeVersion
		history := saved.Document.Recipes[revision-1]
		if history.Version != revision || history.Recipe.Research == nil {
			t.Fatal("history association is incomplete")
		}
		dir, err := openInputResearch(root, record.Research.RunID, false)
		if err != nil {
			t.Fatal(err)
		}
		run, readErr := s.researchJournal(dir, record.Research.RunID).Read(context.Background())
		closeErr := dir.Close()
		if readErr != nil || closeErr != nil || run.PreparationID != saved.Document.ID || run.RecipeVersion != revision || !reflect.DeepEqual(run.Recipe.Fields, history.Recipe.Research.Fields) {
			t.Fatalf("full original run/history relation at %d: %v %v", revision, readErr, closeErr)
		}
		if _, seen := represented[revision]; seen {
			continue
		}
		represented[revision] = record.Research.RunID
		inventory := runcontext.DialectResearchRevision{Revision: saved.Document.ID + ":recipe:" + strconv.Itoa(revision), Fields: []runcontext.DialectResearchField{}}
		for _, field := range history.Recipe.Research.Fields {
			inventory.Fields = append(inventory.Fields, runcontext.DialectResearchField{Name: field.Name, FieldJSON: string(d0aJSON(t, field))})
		}
		originals = append(originals, inventory)
	}
	if len(represented) != d0aRevisions || len(originals) != d0aRevisions {
		t.Fatal("original inventories were omitted or duplicated")
	}
	snapshot.ResearchIndex, err = runcontext.DialectResearchIndexOf(originals)
	assistantRunMust(t, err)
	snapshotRaw := d0aJSON(t, snapshot)
	inventoryRaw := d0aJSON(t, originals)
	want := []runcontext.DialectDeclaration{}
	for _, revision := range originals {
		for _, field := range revision.Fields {
			want = append(want, runcontext.DialectDeclaration{Revision: revision.Revision, Name: field.Name, FieldJSON: field.FieldJSON})
		}
	}
	// Each bounded page reads the field indexes of the revisions it spans.
	// A revision crossing a page boundary is checked on both pages.
	wantPageCalls, wantFieldCalls := 0, 0
	for cursor := 0; cursor < d0aRevisions*d0aFieldsPerRevision; cursor += runcontext.DialectPageDeclarations {
		end := min(cursor+runcontext.DialectPageDeclarations, d0aRevisions*d0aFieldsPerRevision)
		wantPageCalls++
		wantFieldCalls += (end-1)/d0aFieldsPerRevision - cursor/d0aFieldsPerRevision + 1
	}
	verified := []runcontext.DialectDeclaration{}
	readCalls, fieldCalls, declarationCalls, forbiddenCalls := 0, 0, 0, 0
	selection := runcontext.DialectRetainedColumnDraft()
	tools := runcontext.NewDialectTools(runcontext.DialectAuthoringPorts{
		ConversionContract: &selection,
		Read: func(ctx context.Context, request runcontext.DialectReadRequest) (runcontext.DialectSnapshot, error) {
			readCalls++
			current, err := s.preparationRepository().Get(ctx, request.ID)
			if err != nil || current.SHA256 != saved.SHA256 || request.PackKey != q.Key {
				return runcontext.DialectSnapshot{}, fmt.Errorf("original collection revision changed: %v", err)
			}
			return snapshot, nil
		},
		ReadFieldIndex: func(ctx context.Context, request runcontext.DialectFieldIndexRequest) (runcontext.DialectFieldIndexPage, error) {
			fieldCalls++
			current, err := s.preparationRepository().Get(ctx, request.ID)
			if err != nil || current.SHA256 != saved.SHA256 || request.ID != snapshot.ID || request.PackKey != q.Key || request.Revision != saved.SHA256 || request.InventoryDigest != snapshot.ResearchIndex.Digest || request.Entry < 0 || request.Entry >= len(originals) {
				return runcontext.DialectFieldIndexPage{}, fmt.Errorf("original field-index scope changed")
			}
			index := runcontext.DialectFieldIndexOf(originals[request.Entry])
			if index == nil {
				return runcontext.DialectFieldIndexPage{}, fmt.Errorf("original declarations unavailable")
			}
			return runcontext.DialectFieldIndexPage{ID: request.ID, PackKey: request.PackKey, Revision: request.Revision, InventoryDigest: request.InventoryDigest, Entry: request.Entry, FieldIndex: *index}, nil
		},
		ReadDeclarations: func(ctx context.Context, request runcontext.DialectDeclarationPageRequest) (runcontext.DialectDeclarationPage, error) {
			declarationCalls++
			current, err := s.preparationRepository().Get(ctx, request.ID)
			if err != nil || current.SHA256 != saved.SHA256 || request.ID != snapshot.ID || request.PackKey != q.Key || request.Revision != saved.SHA256 || request.InventoryDigest != snapshot.ResearchIndex.Digest || request.Cursor < 0 || request.Limit < 1 || request.Limit > runcontext.DialectColumns {
				return runcontext.DialectDeclarationPage{}, fmt.Errorf("original declaration scope changed")
			}
			all := []runcontext.DialectDeclaration{}
			for _, revision := range originals {
				for _, field := range revision.Fields {
					all = append(all, runcontext.DialectDeclaration{Revision: revision.Revision, Name: field.Name, FieldJSON: field.FieldJSON})
				}
			}
			if request.Cursor > len(all) {
				return runcontext.DialectDeclarationPage{}, fmt.Errorf("original declaration cursor changed")
			}
			end := min(request.Cursor+request.Limit, len(all))
			verified = append(verified, all[request.Cursor:end]...)
			return runcontext.DialectDeclarationPage{ID: request.ID, PackKey: request.PackKey, Revision: request.Revision, InventoryDigest: request.InventoryDigest, Cursor: request.Cursor, Declarations: all[request.Cursor:end]}, nil
		},
		Runtime: func(context.Context, string, map[string]string) (runcontext.ToolResult, error) {
			forbiddenCalls++
			return runcontext.ToolResult{}, fmt.Errorf("D0a read must not reach Runtime")
		},
		Preview: func(context.Context, runcontext.DialectCandidate, string) (runcontext.DialectPreview, error) {
			forbiddenCalls++
			return runcontext.DialectPreview{}, fmt.Errorf("D0a read must not preview")
		},
		RetainDraft: func(context.Context, runcontext.DialectDraftCheckpoint) (runcontext.DialectDraftRetention, error) {
			forbiddenCalls++
			return runcontext.DialectDraftRetention{}, fmt.Errorf("D0a read must not retain")
		},
		Retain: func(context.Context, runcontext.DialectReviewProposal) (runcontext.DialectRetention, error) {
			forbiddenCalls++
			return runcontext.DialectRetention{}, fmt.Errorf("D0a read must not propose")
		},
	})
	result, factoryErr := tools[0].Execute(context.Background(), d0aJSON(t, runcontext.DialectReadRequest{ID: saved.Document.ID, PackKey: q.Key}))
	if readCalls != 1 || fieldCalls != wantFieldCalls || declarationCalls != wantPageCalls || !reflect.DeepEqual(verified, want) || forbiddenCalls != 0 {
		t.Fatalf("published factory initial verification: reads=%d/1 fields=%d/%d pages=%d/%d declarations=%d/%d exact=%t downstream=%d error=%v result=%#v", readCalls, fieldCalls, wantFieldCalls, declarationCalls, wantPageCalls, len(verified), len(want), reflect.DeepEqual(verified, want), forbiddenCalls, factoryErr, result)
	}
	// Retrieve every exact original declaration through the published paging API.
	// Samples and the current recipe cannot substitute for any historical entry.
	seen := []runcontext.DialectDeclaration{}
	if factoryErr == nil && !result.Err() {
		cursor := 0
		for {
			page, err := tools[0].Execute(context.Background(), d0aJSON(t, map[string]any{"id": snapshot.ID, "packKey": snapshot.PackKey, "cursor": cursor}))
			if err != nil || page.Err() || len(page.Content) != 1 {
				t.Fatalf("complete original page at %d: %v", cursor, err)
			}
			var value struct {
				Declarations []runcontext.DialectDeclaration `json:"declarations"`
				NextCursor   *int                            `json:"nextCursor"`
			}
			assistantRunMust(t, json.Unmarshal([]byte(page.Content[0].Text()), &value))
			if len(value.Declarations) == 0 {
				t.Fatal("empty page before original inventory completed")
			}
			seen = append(seen, value.Declarations...)
			if value.NextCursor == nil {
				break
			}
			if *value.NextCursor <= cursor {
				t.Fatal("original inventory cursor did not advance")
			}
			cursor = *value.NextCursor
		}
		if !reflect.DeepEqual(seen, want) || declarationCalls != 2*wantPageCalls || fieldCalls != 2*wantFieldCalls {
			t.Fatalf("indexed output bytes/order: exact=%t declarations=%d/%d fields=%d/%d pages=%d/%d", reflect.DeepEqual(seen, want), len(seen), len(want), fieldCalls, 2*wantFieldCalls, declarationCalls, 2*wantPageCalls)
		}
	}
	refusal := ""
	if factoryErr != nil {
		refusal = factoryErr.Error()
	}
	d0aEvidence(t, "agent012-domain-collection.json", collectionRaw)
	d0aEvidence(t, "agent012-domain-original-snapshot.json", originalSnapshot)
	d0aEvidence(t, "agent012-domain-original-record-revisions.json", d0aJSON(t, represented))
	d0aEvidence(t, "agent012-domain-complete-factory-snapshot.json", snapshotRaw)
	d0aEvidence(t, "agent012-domain-complete-deduplicated-inventory.json", inventoryRaw)
	d0aEvidence(t, "agent012-domain-exact-template.json", dialectRaw)
	d0aEvidence(t, "agent012-domain-exact-pack.json", []byte(q.Pack))
	d0aEvidence(t, "agent012-domain-factory-result.json", d0aJSON(t, result))
	d0aEvidence(t, "agent012-domain-observation.json", d0aJSON(t, map[string]any{"originalRevisions": d0aRevisions, "fieldsPerRevision": d0aFieldsPerRevision, "descriptionUTF8Bytes": d0aDescriptionBytes, "records": len(saved.Document.Records), "collectionBytes": len(collectionRaw), "collectionOwnerLimit": inputPreparationLimit, "originalSnapshotBytes": len(originalSnapshot), "factorySnapshotBytes": len(snapshotRaw), "completeInventoryBytes": len(inventoryRaw), "factorySnapshotLimit": runcontext.DialectSnapshotBytes, "factoryCandidateLimit": runcontext.DialectCandidateBytes, "factoryCheckpointLimit": runcontext.DialectDraftBytes, "factoryReadCalls": readCalls, "fieldIndexCalls": fieldCalls, "declarationPageCalls": declarationCalls, "carrierVersion": 2, "downstreamPortCalls": forbiddenCalls, "factoryError": refusal, "candidateCheckpointStatus": "not-issued; indexed read fixture only", "newV2SignedCarrier": "not-exercised"}))
	// Domain parity is required. A correctly bounded refusal is still a concrete
	// adoption failure for this reachable complete original collection; never
	// turn this into a passing diagnostic or omit declarations to make it fit.
	if factoryErr != nil || result.Err() || len(snapshotRaw) > runcontext.DialectSnapshotBytes || len(d0aJSON(t, snapshot.ResearchIndex)) > runcontext.DialectIndexBytes {
		t.Fatalf("reachable original domain does not fit published factory: collection=%d/%d originalSnapshot=%d/524288 revisions=%d completeFields=%d inventory=%d snapshot=%d/%d candidateLimit=%d checkpointLimit=%d refusal=%q", len(collectionRaw), inputPreparationLimit, len(originalSnapshot), d0aRevisions, d0aRevisions*d0aFieldsPerRevision, len(inventoryRaw), len(snapshotRaw), runcontext.DialectSnapshotBytes, runcontext.DialectCandidateBytes, runcontext.DialectDraftBytes, refusal)
	}
}

// Separate from the archived inline-carrier failure witness: execute the final
// published indexed carrier over the full revision bound without external tools.
func TestAssistantDialectFactoryFinalIndexedInventory(t *testing.T) {
	const originalCount = 1000
	originals := make([]runcontext.DialectResearchRevision, originalCount)
	want := make([]runcontext.DialectDeclaration, 0, len(originals))
	for i := range originals {
		field := runcontext.DialectResearchField{Name: "original", FieldJSON: fmt.Sprintf(`{ "name":"original", "type":"text", "description":"Original revision %d" }`, i)}
		originals[i] = runcontext.DialectResearchRevision{Revision: fmt.Sprintf("original:%04d", i), Fields: []runcontext.DialectResearchField{field}}
		want = append(want, runcontext.DialectDeclaration{Revision: originals[i].Revision, Name: field.Name, FieldJSON: field.FieldJSON})
	}
	for _, mode := range []string{"complete", "stale-source", "wrong-index-owner", "changed-declaration"} {
		t.Run(mode, func(t *testing.T) {
			index, err := runcontext.DialectResearchIndexOf(originals)
			assistantRunMust(t, err)
			snapshot := runcontext.DialectSnapshot{ID: "collection", Revision: strings.Repeat("1", 64), PackKey: "pack", Name: "Full original inventory", PackJSON: `{}`, Columns: []runcontext.DialectColumn{}, FactTargets: []string{}, EvidenceRequirements: []string{}, Conversions: map[string]runcontext.DialectConversion{}, ResearchIndex: index}
			changed, fieldCalls, pageCalls := false, 0, 0
			verified := []runcontext.DialectDeclaration{}
			verifiedEntries := make([]int, originalCount)
			wantPages := (originalCount + runcontext.DialectPageDeclarations - 1) / runcontext.DialectPageDeclarations
			tools := runcontext.NewDialectTools(runcontext.DialectAuthoringPorts{
				Read: func(context.Context, runcontext.DialectReadRequest) (runcontext.DialectSnapshot, error) {
					current := snapshot
					if changed && mode == "stale-source" {
						current.Revision = strings.Repeat("2", 64)
					}
					return current, nil
				},
				ReadFieldIndex: func(_ context.Context, r runcontext.DialectFieldIndexRequest) (runcontext.DialectFieldIndexPage, error) {
					fieldCalls++
					if r.ID != snapshot.ID || r.PackKey != snapshot.PackKey || r.Revision != snapshot.Revision || r.InventoryDigest != index.Digest || r.Entry < 0 || r.Entry >= len(originals) {
						return runcontext.DialectFieldIndexPage{}, errAssistantRunScope
					}
					verifiedEntries[r.Entry]++
					page := runcontext.DialectFieldIndexPage{ID: r.ID, PackKey: r.PackKey, Revision: r.Revision, InventoryDigest: r.InventoryDigest, Entry: r.Entry, FieldIndex: *runcontext.DialectFieldIndexOf(originals[r.Entry])}
					if changed && mode == "wrong-index-owner" {
						page.Revision = strings.Repeat("3", 64)
					}
					return page, nil
				},
				ReadDeclarations: func(_ context.Context, r runcontext.DialectDeclarationPageRequest) (runcontext.DialectDeclarationPage, error) {
					pageCalls++
					if r.ID != snapshot.ID || r.PackKey != snapshot.PackKey || r.Revision != snapshot.Revision || r.InventoryDigest != index.Digest || r.Cursor < 0 || r.Cursor >= len(want) || r.Limit < 1 || r.Limit > runcontext.DialectColumns {
						return runcontext.DialectDeclarationPage{}, errAssistantRunScope
					}
					rows := append([]runcontext.DialectDeclaration{}, want[r.Cursor:min(r.Cursor+r.Limit, len(want))]...)
					if changed && mode == "changed-declaration" {
						rows[0].FieldJSON += " "
					}
					verified = append(verified, rows...)
					return runcontext.DialectDeclarationPage{ID: r.ID, PackKey: r.PackKey, Revision: r.Revision, InventoryDigest: r.InventoryDigest, Cursor: r.Cursor, Declarations: rows}, nil
				},
			})
			read, err := tools[0].Execute(t.Context(), assistantChatJSON(map[string]any{"id": snapshot.ID, "packKey": snapshot.PackKey}))
			// The released read verifies the complete original inventory before
			// retaining the read identity. One field per revision means every
			// revision index is visited exactly once, followed by bounded pages.
			if err != nil || read.Err() || fieldCalls != originalCount || pageCalls != wantPages || !reflect.DeepEqual(verified, want) {
				t.Fatalf("initial complete verification: fields=%d/%d pages=%d/%d declarations=%d/%d exact=%t error=%v result=%#v", fieldCalls, originalCount, pageCalls, wantPages, len(verified), len(want), reflect.DeepEqual(verified, want), err, read)
			}
			for entry, count := range verifiedEntries {
				if count != 1 {
					t.Fatalf("initial original entry %d checked %d times, want 1", entry, count)
				}
			}
			initialFields, initialPages := fieldCalls, pageCalls
			changed = true
			seen := []runcontext.DialectDeclaration{}
			for cursor := 0; ; {
				page, err := tools[0].Execute(t.Context(), assistantChatJSON(map[string]any{"id": snapshot.ID, "packKey": snapshot.PackKey, "cursor": cursor}))
				if mode != "complete" {
					if err == nil && !page.Err() {
						t.Fatalf("%s original page accepted: cursor=%d fields=%d pages=%d result=%#v", mode, cursor, fieldCalls, pageCalls, page)
					}
					if mode == "stale-source" && (pageCalls != initialPages || fieldCalls != initialFields) {
						t.Fatalf("stale source reached inventory ports: fields=%d/%d pages=%d/%d error=%v", fieldCalls, initialFields, pageCalls, initialPages, err)
					}
					break
				}
				assistantRunMust(t, err)
				if page.Err() || len(page.Content) != 1 {
					t.Fatalf("complete original page refused: cursor=%d fields=%d pages=%d result=%#v", cursor, fieldCalls, pageCalls, page)
				}
				var value struct {
					Declarations []runcontext.DialectDeclaration `json:"declarations"`
					NextCursor   *int                            `json:"nextCursor"`
				}
				assistantRunMust(t, json.Unmarshal([]byte(page.Content[0].Text()), &value))
				seen = append(seen, value.Declarations...)
				if value.NextCursor == nil {
					break
				}
				if *value.NextCursor <= cursor {
					t.Fatal("original cursor did not advance")
				}
				cursor = *value.NextCursor
			}
			if mode == "complete" && (!reflect.DeepEqual(seen, want) || pageCalls-initialPages != wantPages || fieldCalls-initialFields != originalCount) {
				t.Fatalf("paged original bytes/order: exact=%t declarations=%d/%d additionalFields=%d/%d additionalPages=%d/%d", reflect.DeepEqual(seen, want), len(seen), len(want), fieldCalls-initialFields, originalCount, pageCalls-initialPages, wantPages)
			}
			t.Logf("mode=%s initialFields=%d initialPages=%d initialDeclarations=%d additionalFields=%d additionalPages=%d outputDeclarations=%d", mode, initialFields, initialPages, len(want), fieldCalls-initialFields, pageCalls-initialPages, len(seen))
		})
	}
}
