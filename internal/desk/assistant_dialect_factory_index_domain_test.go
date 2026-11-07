package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This handwritten family witnesses Desk admission and independently measures
// the merged-source index format. It does not execute Agent 0.11 or 0.12.
// Source: judgment-pack-agent@9c5307102c003994f3cccabe927249d895d4de32,
// go/context/dialects.go (DialectResearchIndexOf, dialectInventoryDigest,
// dialectCanonical, dialectIndex), with the same index cap in
// packages/harness/dialects.ts.
const (
	indexDomainRevisions = 450
	indexDomainFields    = 30
	indexDomainIndexCap  = 2097152
	indexDomainIndexSize = 2180240 // 98 + 4842*450 + (9 + 90*2 + 351*3).
	indexDomainStamp     = "2026-10-28T12:00:00Z"
)

func indexDomainHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func indexDomainJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func indexDomainName(field int) string {
	return "f" + strings.Repeat("a", 61) + fmt.Sprintf("%02d", field)
}

// The expected declaration is authored directly, independently of the Desk
// serializer. Restricted ASCII strings need no escaping; order is significant
// because the merged-source declaration digest hashes exact declaration bytes.
func indexDomainDeclaration(revision, field int) []byte {
	return []byte(fmt.Sprintf(`{"name":"%s","description":"r%03d","type":"text"}`, indexDomainName(field), revision))
}

func indexDomainEvidence(t *testing.T, name string, raw []byte) {
	t.Helper()
	t.Logf("artifact %s bytes=%d sha256=%s", name, len(raw), indexDomainHash(raw))
	dir := os.Getenv("JPS_DIALECT_INDEX_DOMAIN_EVIDENCE")
	if dir == "" {
		return
	}
	if !filepath.IsAbs(dir) || filepath.Base(name) != name {
		t.Fatal("index domain evidence requires an absolute scratch directory and basename")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// reviewedInputSnapshot checks bundle integrity even when no citations need a
// verifier. These inert, nonexecutable test files exercise that integrity owner
// only. Any attempted component invocation fails; they provide no acquisition
// evidence and make no claim about a released Gateway or Agent execution.
func indexDomainInertBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{}
	for _, component := range []string{"gateway", "adapter-document", "gateway-connections"} {
		name := executableName(component)
		raw := []byte("inert index-domain integrity fixture: " + name + "\n")
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
		files[name] = strings.TrimPrefix(indexDomainHash(raw), "sha256:")
	}
	manifest := indexDomainJSON(t, map[string]any{"revision": GatewayRevision, "files": files})
	if err := os.WriteFile(filepath.Join(dir, "gateway-bundle.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	// Scope a possibly inherited installation manifest pin to this test fixture.
	t.Setenv("JPACK_DESK_GATEWAY_MANIFEST_SHA256", strings.TrimPrefix(indexDomainHash(manifest), "sha256:"))
	if err := verifyGatewayBundle(dir); err != nil {
		t.Fatalf("inert integrity fixture: %v", err)
	}
	return dir
}

func indexDomainCollectionBytes(t *testing.T, s *Server, saved inputPreparationReply) []byte {
	t.Helper()
	root, err := s.openInputPreparations(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	raw, err := readPrivateData(root, saved.Document.ID+".json", inputPreparationLimit)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimPrefix(indexDomainHash(raw), "sha256:") != saved.SHA256 || !bytes.Equal(raw, indexDomainJSON(t, saved.Document)) {
		t.Fatal("repository read-back differs from the actual complete collection bytes")
	}
	return raw
}

type indexDomainOriginalField struct {
	Name      string `json:"name"`
	FieldJSON string `json:"fieldJson"`
}

type indexDomainOriginalRevision struct {
	Revision string                     `json:"revision"`
	Fields   []indexDomainOriginalField `json:"fields"`
}

// Independently implement only this fixture's restricted ASCII index encoding.
// Canonical JSON sorts object keys: digest before name, fields before revision,
// and digest before revisions. Arrays retain every original in its saved order.
// No Agent parser, canonicalizer, index helper, limit constant or factory is used.
func indexDomainCanonicalRevisions(t *testing.T, originals []indexDomainOriginalRevision) []byte {
	t.Helper()
	var out strings.Builder
	out.WriteByte('[')
	for i, revision := range originals {
		if i > 0 {
			out.WriteByte(',')
		}
		out.WriteString(`{"fields":[`)
		for j, field := range revision.Fields {
			if j > 0 {
				out.WriteByte(',')
			}
			// All input strings are handwritten ASCII, with no control, quote,
			// backslash, HTML or non-ASCII characters requiring special encoding.
			if field.Name != indexDomainName(j) || len(field.FieldJSON) != 110 {
				t.Fatal("canonical fixture encoder received a different declaration family")
			}
			out.WriteString(`{"digest":`)
			out.WriteString(strconv.Quote(indexDomainHash([]byte(field.FieldJSON))))
			out.WriteString(`,"name":`)
			out.WriteString(strconv.Quote(field.Name))
			out.WriteByte('}')
		}
		out.WriteString(`],"revision":`)
		out.WriteString(strconv.Quote(revision.Revision))
		out.WriteByte('}')
	}
	out.WriteByte(']')
	return []byte(out.String())
}

func TestDialectRetainedIndexDomainDeskAcceptance(t *testing.T) {
	// One local, bounded acceptance witness. No listener, model, network request,
	// installed program, subprocess, Runtime call or Agent factory is involved.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s, err := New(Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, Port: testPort, JpackBin: filepath.Join(t.TempDir(), "absent-runtime")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.cfg.LocalGatewayBundle = indexDomainInertBundle(t)
	q := qualificationFixture()
	q.Mapping.Case.Facts = []inputDialectFact{{Target: "/company/country", Source: "/values/" + indexDomainName(0)}}
	q.Mapping.Case.Evidence = []inputDialectEvidence{}
	q.Conversions = map[string]inputConversion{"/company/country": {Kind: "text"}}
	q.Pack = strings.Replace(qualificationTestPack, `[{"id":"license","description":"License","required":false}]`, `[]`, 1)
	q.Digest = indexDomainHash([]byte(q.Pack))
	if err := validateInputQualification(q); err != nil {
		t.Fatal(err)
	}
	doc := inputPreparation{Version: 1, ID: "prep_" + strings.Repeat("c", 32), Name: "Complete retained index domain", CreatedAt: indexDomainStamp, UpdatedAt: indexDomainStamp, Records: []preparedInputRecord{}, Recipes: []inputRecipeRevision{}, Imports: []inputImport{}, Activity: []inputActivity{}}
	root, err := s.inputResearchRoot(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	journalBytes := 0
	for revision := 1; revision <= indexDomainRevisions; revision++ {
		recipe := researchRecipeFixture()
		recipe.Fields = []inputResearchField{}
		values := map[string]inputResearchValue{}
		for field := 0; field < indexDomainFields; field++ {
			name := indexDomainName(field)
			if len(name) != 64 {
				t.Fatal("handwritten field name length changed")
			}
			recipe.Fields = append(recipe.Fields, inputResearchField{Name: name, Description: fmt.Sprintf("r%03d", revision), Type: "text"})
			values[name] = inputResearchValue{Value: json.RawMessage(`"x"`), Status: "interpretation", Sources: []inputResearchCitation{}}
			if !bytes.Equal(indexDomainJSON(t, recipe.Fields[field]), indexDomainDeclaration(revision, field)) {
				t.Fatalf("handwritten original declaration changed at %d/%d", revision, field)
			}
		}
		if err := validateInputResearch(&recipe); err != nil {
			t.Fatalf("actual recipe admission %d: %v", revision, err)
		}
		url := fmt.Sprintf("https://revision-%03d.example.com/", revision)
		run := inputResearchRun{Version: 1, ID: fmt.Sprintf("research_%032x", revision), PreparationID: doc.ID, RecipeVersion: revision, Recipe: recipe, State: "completed", CreatedAt: indexDomainStamp, UpdatedAt: indexDomainStamp, Intent: "Complete retained original history", Seeds: []string{url}, Sources: []inputResearchSource{}, Records: []inputResearchEntity{}, Attempts: []inputResearchAttempt{}}
		args := indexDomainJSON(t, map[string]any{"records": []inputResearchEntity{{Kind: "company", Name: fmt.Sprintf("Revision %03d", revision), URL: url, Fields: values}}})
		if err := applyResearchRecords(&run, args); err != nil || len(run.Records) != 1 {
			t.Fatalf("actual model record admission %d: %v", revision, err)
		}
		run.Attempts = []inputResearchAttempt{{ID: "attempt_model", Kind: "model", State: "completed", Result: indexDomainJSON(t, inputResearchAction{Tool: "records", Arguments: args}), FinishedAt: indexDomainStamp}}
		dir, err := openInputResearch(root, run.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		createErr := s.researchJournal(dir, run.ID).Create(ctx, run)
		closeErr := dir.Close()
		if createErr != nil || closeErr != nil {
			t.Fatalf("actual durable journal creation %d: %v %v", revision, createErr, closeErr)
		}
		original := inputRecipe{Intent: run.Intent, Research: &recipe, Qualification: q}
		doc.Recipes = append(doc.Recipes, inputRecipeRevision{Version: revision, Name: fmt.Sprintf("Original %03d", revision), Recipe: original, At: indexDomainStamp})
		doc.Records = append(doc.Records, researchPreparedRecord(run, run.Records[0]))
		doc.Recipe, doc.RecipeVersion = original, revision
	}
	saved, err := s.preparationRepository().CompareAndSwap(ctx, doc, "")
	if err != nil {
		t.Fatalf("actual complete collection CAS admission: %v", err)
	}
	readBack, err := s.preparationRepository().Get(ctx, doc.ID)
	if err != nil || !reflect.DeepEqual(readBack, saved) {
		t.Fatalf("actual complete collection repository read-back: %v", err)
	}
	indexDomainCollectionBytes(t, s, saved)

	// Exercise the actual authenticated human review route and its CAS, retaining
	// one current-recipe review event over the oldest original record.
	reviewRequest := indexDomainJSON(t, map[string]string{"recordId": saved.Document.Records[0].ID, "decision": "accepted", "note": "Handwritten index domain witness"})
	request := httptest.NewRequest("POST", "/api/input-preparations/"+doc.ID+"/record-reviews", bytes.NewReader(reviewRequest)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("If-Match", saved.SHA256)
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("actual human review admission: status=%d body=%s", response.Code, response.Body.String())
	}
	// A public reply cannot carry the private repository cell custody. Decode
	// into a fresh value so the earlier snapshot's unexported metadata cannot
	// survive json.Unmarshal, then compare the exact existing HTTP encoding.
	var publicSaved inputPreparationReply
	if err := json.Unmarshal(response.Body.Bytes(), &publicSaved); err != nil {
		t.Fatal(err)
	}
	readBack, err = s.preparationRepository().Get(ctx, doc.ID)
	if err != nil {
		t.Fatalf("actual reviewed collection repository read-back: %v", err)
	}
	if publicSaved.SHA256 != readBack.SHA256 || !bytes.Equal(response.Body.Bytes(), append(indexDomainJSON(t, readBack), '\n')) {
		t.Fatal("actual reviewed collection public reply differs from repository read-back")
	}
	// Downstream owner checks use the actual held repository snapshot, never
	// browser-shaped JSON or metadata copied from a previous collection SHA.
	saved = readBack
	collectionRaw := indexDomainCollectionBytes(t, s, saved)
	if len(collectionRaw) > inputPreparationLimit || saved.Document.RecipeVersion != indexDomainRevisions || len(saved.Document.Records) != indexDomainRevisions || len(saved.Document.Recipes) != indexDomainRevisions || len(saved.Document.Reviews) != 1 || saved.Document.Records[0].Review == nil || saved.Document.Records[0].Review.RecipeVersion != indexDomainRevisions {
		t.Fatal("complete accepted collection or current human review is incomplete")
	}
	rows, err := prepareInputSelection(saved.Document, []string{saved.Document.Records[0].ID})
	if err != nil || len(rows) != 1 || len(rows[0].Issues) != 0 {
		t.Fatalf("actual old record selection: %v %#v", err, rows)
	}
	originalSnapshot, err := s.reviewedInputSnapshot(ctx, saved.Document, saved.Document.Records[0], rows[0])
	if err != nil || len(originalSnapshot) == 0 || len(originalSnapshot) > 524288 {
		t.Fatalf("actual old snapshot admission: bytes=%d/524288 err=%v", len(originalSnapshot), err)
	}
	var snapshot struct {
		Research struct {
			RecipeVersion int                 `json:"recipeVersion"`
			Recipe        inputResearchRecipe `json:"recipe"`
		} `json:"research"`
	}
	if err := json.Unmarshal(originalSnapshot, &snapshot); err != nil || snapshot.Research.RecipeVersion != 1 || !reflect.DeepEqual(snapshot.Research.Recipe.Fields, saved.Document.Recipes[0].Recipe.Research.Fields) {
		t.Fatalf("old snapshot lost its actual original declarations: %v", err)
	}

	originals := []indexDomainOriginalRevision{}
	relations := []map[string]any{}
	seenVersions, seenRecords := map[int]bool{}, map[string]bool{}
	fieldCount := 0
	for i, record := range saved.Document.Records {
		version := i + 1
		if saved.Document.Recipes[i].Version != version || record.Research == nil || record.Research.RecipeVersion != version || record.Research.RunID != fmt.Sprintf("research_%032x", version) || seenRecords[record.ID] {
			t.Fatalf("retained record identity/order changed at %d", version)
		}
		seenRecords[record.ID] = true
		dir, err := openInputResearch(root, record.Research.RunID, false)
		if err != nil {
			t.Fatal(err)
		}
		run, readErr := s.researchJournal(dir, record.Research.RunID).Read(ctx)
		journalRaw, rawErr := readPrivateData(dir, "record.json", inputResearchRecordLimit)
		closeErr := dir.Close()
		if readErr != nil || rawErr != nil || closeErr != nil {
			t.Fatalf("actual journal read %d: %v %v %v", version, readErr, rawErr, closeErr)
		}
		journalBytes += len(journalRaw)
		history, err := resolveRetainedResearchHistory(saved.Document, record, run)
		if err != nil || history.RecipeVersion != version || history.PreparationID != doc.ID || history.RecordID != record.ID || history.RunID != run.ID || len(history.Fields) != indexDomainFields || seenVersions[version] {
			t.Fatalf("actual D3 original-history join %d: %v", version, err)
		}
		seenVersions[version] = true
		original := indexDomainOriginalRevision{Revision: doc.ID + ":recipe:" + strconv.Itoa(history.RecipeVersion), Fields: []indexDomainOriginalField{}}
		for field, declaration := range history.Fields {
			raw := indexDomainJSON(t, declaration)
			if !bytes.Equal(raw, indexDomainDeclaration(version, field)) || !bytes.Equal(raw, indexDomainJSON(t, saved.Document.Recipes[i].Recipe.Research.Fields[field])) || !bytes.Equal(raw, indexDomainJSON(t, run.Recipe.Fields[field])) {
				t.Fatalf("exact original declaration bytes/order changed at %d/%d", version, field)
			}
			original.Fields = append(original.Fields, indexDomainOriginalField{Name: declaration.Name, FieldJSON: string(raw)})
			fieldCount++
		}
		originals = append(originals, original)
		relations = append(relations, map[string]any{"originalRevision": original.Revision, "recipeVersion": history.RecipeVersion, "recordId": record.ID, "runId": run.ID, "journalBytes": len(journalRaw), "journalSHA256": indexDomainHash(journalRaw), "d3JoinAccepted": true})
	}
	visible := assistantDialectColumns(saved.Document)
	if len(originals) != indexDomainRevisions || len(seenVersions) != indexDomainRevisions || fieldCount != indexDomainRevisions*indexDomainFields || len(visible) != indexDomainFields+3 {
		t.Fatalf("complete original inventory/visible column count changed: revisions=%d fields=%d visible=%d", len(originals), fieldCount, len(visible))
	}
	revisionsCanonical := indexDomainCanonicalRevisions(t, originals)
	inventoryDigest := indexDomainHash(revisionsCanonical)
	indexCanonical := []byte(`{"digest":` + strconv.Quote(inventoryDigest) + `,"revisions":` + string(revisionsCanonical) + `}`)
	// This expected size is handwritten structural arithmetic, not recorded
	// output from either an Agent implementation or the Desk code under test.
	if !json.Valid(indexCanonical) || len(indexCanonical) != indexDomainIndexSize || len(indexCanonical) <= indexDomainIndexCap {
		t.Fatalf("independent complete index measurement: bytes=%d want=%d mergedSourceCap=%d", len(indexCanonical), indexDomainIndexSize, indexDomainIndexCap)
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("bounded local witness incomplete: %v", err)
	}
	indexDomainEvidence(t, "index-domain-collection.json", collectionRaw)
	indexDomainEvidence(t, "index-domain-original-snapshot.json", originalSnapshot)
	indexDomainEvidence(t, "index-domain-original-declarations.json", indexDomainJSON(t, originals))
	indexDomainEvidence(t, "index-domain-original-relations.json", indexDomainJSON(t, relations))
	indexDomainEvidence(t, "index-domain-revisions-canonical.json", revisionsCanonical)
	indexDomainEvidence(t, "index-domain-index-canonical.json", indexCanonical)
	indexDomainEvidence(t, "index-domain-observation.json", indexDomainJSON(t, map[string]any{
		"status": "actual-desk-acceptance-with-independent-merged-source-index-overflow", "originalRevisions": len(originals), "fieldsPerRevision": indexDomainFields, "completeFields": fieldCount, "actualVisibleColumns": visible,
		"collectionBytes": len(collectionRaw), "collectionSHA256": indexDomainHash(collectionRaw), "collectionOwnerLimit": inputPreparationLimit, "actualCollectionCASAndReadBack": true, "actualHumanReviewEvents": len(saved.Document.Reviews), "actualD3Joins": len(relations), "journalBytesTotal": journalBytes,
		"originalSnapshotBytes": len(originalSnapshot), "originalSnapshotSHA256": indexDomainHash(originalSnapshot), "originalSnapshotOwnerLimit": 524288, "actualReviewedOldSnapshotAccepted": true,
		"independentIndexCanonicalBytes": len(indexCanonical), "independentIndexCanonicalSHA256": indexDomainHash(indexCanonical), "inventoryDigest": inventoryDigest, "mergedSourceIndexCap": indexDomainIndexCap, "indexCapOverBytes": len(indexCanonical) - indexDomainIndexCap,
		"mergedSourceCommit": "9c5307102c003994f3cccabe927249d895d4de32", "mergedGoSourceSHA256": "368d7f5ed594eedd261f77d839aeea6c75ede196ff9a85900140f4b715fbb32b", "mergedTypeScriptSourceSHA256": "8e9bc6be2e3dd233b2a6bce8181e2e5a3bb8a5ee774d5e6fe5703b46f05ec9c0",
		"componentFixture": "inert integrity-only nonexecutable files; no acquisition evidence", "agent011FactoryExecuted": false, "agent012Executed": false, "releasedPackageAdoptionAndD7": "pending",
	}))
}
