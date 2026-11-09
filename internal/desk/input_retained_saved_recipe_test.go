package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// These logical rules and pack are handwritten. The research-value column need
// not exist yet: document validity is independent of per-record readiness.
func retainedB4Qualification(t *testing.T) *inputQualification {
	t.Helper()
	q := retainedMappingFixture(t)
	q.Kind, q.Key = "pack", "selected"
	q.Mapping.Unmapped, q.Mapping.UnmappedEvidence = nil, nil
	q.Pack = `{"specVersion":"0.2.0-draft","id":"https://example.invalid/pack/retained","version":"1.0.0","title":"Retained logical conversion fixture","decision":{"intent":"Inspect logical conversions","question":"Ready?"},"outcomes":[{"id":"ready","label":"Ready"},{"id":"other","label":"Other"}],"rules":[{"id":"text","description":"Text","when":{"op":"fact","path":"/text","operator":"equals","value":"x"},"outcome":"ready","onUnknown":"escalate"},{"id":"decimal","description":"Decimal","when":{"op":"fact","path":"/decimal","operator":"equals","value":"x"},"outcome":"ready","onUnknown":"escalate"},{"id":"number","description":"Number","when":{"op":"fact","path":"/number","operator":"equals","value":"x"},"outcome":"ready","onUnknown":"escalate"},{"id":"boolean","description":"Boolean","when":{"op":"fact","path":"/boolean","operator":"equals","value":"x"},"outcome":"ready","onUnknown":"escalate"},{"id":"research","description":"Original research value","when":{"op":"fact","path":"/research","operator":"equals","value":"x"},"outcome":"ready","onUnknown":"escalate"}],"evidenceRequirements":[{"id":"license","description":"License","required":false}]}`
	q.Digest = retainedDigest([]byte(q.Pack))
	return q
}

func retainedB4Reports(t *testing.T, h retainedRuntimeTestHarness, q *inputQualification) {
	t.Helper()
	document, err := json.Marshal(q.Mapping)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"validate", "check"} {
		report := retainedRuntimeTestReport(operation)
		report["document"], report["digest"] = string(document), retainedDigest(document)
		if operation == "check" {
			compatibility := report["compatibility"].(map[string]any)
			compatibility["pack"].(map[string]any)["sha256"] = digestOf([]byte(q.Pack))
			compatibility["facts"] = []any{
				map[string]any{"target": "/boolean", "status": "mapped", "producers": []string{"/boolean"}, "acknowledgedBy": []string{}},
				map[string]any{"target": "/decimal", "status": "mapped", "producers": []string{"/decimal"}, "acknowledgedBy": []string{}},
				map[string]any{"target": "/number", "status": "mapped", "producers": []string{"/number"}, "acknowledgedBy": []string{}},
				map[string]any{"target": "/research", "status": "mapped", "producers": []string{"/research"}, "acknowledgedBy": []string{}},
				map[string]any{"target": "/text", "status": "mapped", "producers": []string{"/text"}, "acknowledgedBy": []string{}},
			}
		}
		h.output(t, operation, append(retainedRuntimeTestJSON(t, report), '\n'), 0)
	}
}

func retainedB4Request(t *testing.T, s *Server, server *httptest.Server, method, path string, body any, revision string) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	// Use the browser launch/session exchange and retain one actor for this server.
	// The launch credential itself cannot authorize private workspace requests.
	r.Header.Set("Authorization", "Bearer "+privateWorkspaceTestSession(t, server))
	r.Header.Set("If-Match", revision)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestRetainedB4SavedLogicalValidation(t *testing.T) {
	q := retainedB4Qualification(t)
	before, _ := json.Marshal(q)
	if err := validateInputQualification(q); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(q)
	if !bytes.Equal(before, after) {
		t.Fatal("selected validation normalized the saved rules")
	}
	for _, scenario := range []string{"tuple", "sidecar", "source", "profile", "unlisted fact", "unlisted evidence", "pack digest", "missing arrays"} {
		t.Run(scenario, func(t *testing.T) {
			q := retainedB4Qualification(t)
			switch scenario {
			case "tuple":
				q.ConversionContract.Contract = "unsupported"
			case "sidecar":
				delete(q.Conversions, "/research")
			case "source":
				q.Mapping.Case.Facts[0].Source = "/values/Name"
			case "profile":
				*q.Mapping.Profile = "extended"
			case "unlisted fact":
				q.Mapping.Case.Facts[0].Target = "/absent"
			case "unlisted evidence":
				q.Mapping.Case.Evidence[0].Requirement = "absent"
			case "pack digest":
				q.Digest = "sha256:wrong"
			case "missing arrays":
				q.Mapping.Sources = nil
			}
			if validateInputQualification(q) == nil {
				t.Fatal("invalid selected recipe admitted")
			}
		})
	}
	// Runtime inventories any op/path pair, including an opaque literal branch.
	// Desk must not use its older op==fact-only walk for this selected check.
	q.Pack = strings.Replace(q.Pack, `"path":"/text"`, `"path":"/replaced"`, 1)
	q.Pack = strings.TrimSuffix(q.Pack, "}") + `,"opaque":{"op":"literal","path":"/text"}}`
	q.Digest = retainedDigest([]byte(q.Pack))
	if err := validateInputQualification(q); err != nil {
		t.Fatal("selected complete inventory was filtered", err)
	}
}

func TestRetainedB4SelectedSaveCASReadback(t *testing.T) {
	h := retainedRuntimeHarness(t)
	s, ts := newTestServer(t, false)
	s.cfg.JpackBin = h.peer
	q := retainedB4Qualification(t)
	retainedB4Reports(t, h, q)
	original := retainedB2Snapshot(t, retainedB2Cells)
	saved, err := s.preparationRepository().CompareAndSwap(t.Context(), original.Document, "")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/input-preparations/" + saved.Document.ID
	body := map[string]any{"name": "Selected logical recipe", "recipe": inputRecipe{Intent: "Inspect planned fields", Qualification: q}}
	stale := retainedB4Request(t, s, ts, "PUT", path, body, "wrong")
	if stale.Code != 409 || len(h.calls(t)) != 0 {
		t.Fatal("stale save dispatched Runtime", stale.Code)
	}
	w := retainedB4Request(t, s, ts, "PUT", path, body, saved.SHA256)
	if w.Code != 200 {
		t.Fatalf("selected save: %d %s", w.Code, w.Body.Bytes())
	}
	current, err := s.preparationRepository().Get(t.Context(), saved.Document.ID)
	if err != nil {
		t.Fatal(err)
	}
	var reply inputPreparationReply
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.SHA256 != current.SHA256 || current.SHA256 == saved.SHA256 {
		t.Fatal("save did not return committed readback")
	}
	if current.Document.RecipeVersion != 2 || len(current.Document.Recipes) != 2 || current.Document.Recipes[0].Recipe.Qualification != nil || !reflect.DeepEqual(current.Document.Imports, saved.Document.Imports) {
		t.Fatal("selected save changed original history")
	}
	mapping, _ := json.Marshal(q.Mapping)
	actualMapping, _ := json.Marshal(current.Document.Recipe.Qualification.Mapping)
	if !bytes.Equal(mapping, actualMapping) {
		t.Fatal("saved logical template was replaced by checked or bound mapping")
	}
	raw, err := os.ReadFile(filepath.Join(s.projectDir, inputPreparationDir, saved.Document.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	node, err := parseRetainedJSON(raw)
	if err != nil || string(node.member("records").elements[0].member("values").member("null").raw) != "null" {
		t.Fatal("CAS lost exact historical null", err)
	}
	if current.Document.cellCustody == nil || current.Document.cellCustody.sha != current.SHA256 {
		t.Fatal("readback did not reattach custody")
	}
	calls := h.calls(t)
	if len(calls) != 3 || calls[0].Operation != "contract" || calls[1].Operation != "validate" || calls[2].Operation != "check" {
		t.Fatal("save did not perform one selected check", calls)
	}
	if !bytes.Equal(calls[1].Input, mapping) || string(calls[2].Pack) != q.Pack {
		t.Fatal("Runtime did not receive the exact logical candidate and retained pack")
	}
	for _, call := range calls {
		if !contains(call.Args, "--draft-contract") || bytes.Contains(call.Input, []byte("profileDigest")) || bytes.Contains(call.Input, []byte(`"grant"`)) {
			t.Fatal("save materialized an execution or lost explicit selection")
		}
	}
	s.cfg.JpackBin = filepath.Join(t.TempDir(), "unavailable-runtime")
	for _, methodPath := range []string{"/api/input-preparations", path} {
		w = retainedB4Request(t, s, ts, "GET", methodPath, nil, "")
		if w.Code != 200 {
			t.Fatal("Runtime outage hid saved selected history", w.Code)
		}
	}
	// An unchanged qualification retains the existing save policy. Renaming still
	// appends exactly one revision; it must not require Runtime or a cached grant.
	w = retainedB4Request(t, s, ts, "PUT", path, map[string]any{"name": "Renamed", "recipe": current.Document.Recipe}, current.SHA256)
	if w.Code != 200 || len(h.calls(t)) != 3 {
		t.Fatalf("unchanged selected save: %d %s", w.Code, w.Body.Bytes())
	}
	renamed, err := s.preparationRepository().Get(t.Context(), current.Document.ID)
	if err != nil || renamed.Document.RecipeVersion != 3 || len(renamed.Document.Recipes) != 3 || !reflect.DeepEqual(renamed.Document.Recipes[:2], current.Document.Recipes) {
		t.Fatal("unchanged save rewrote history", err)
	}
}

func TestRetainedB4ChangedSelectedSaveRequiresFreshCheck(t *testing.T) {
	h := retainedRuntimeHarness(t)
	s, ts := newTestServer(t, false)
	s.cfg.JpackBin = h.peer
	q := retainedB4Qualification(t)
	retainedB4Reports(t, h, q)
	w := retainedB4Request(t, s, ts, "POST", "/api/input-preparations", map[string]any{"name": "Selected", "recipe": inputRecipe{Qualification: q}}, "")
	var saved inputPreparationReply
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &saved) != nil {
		t.Fatal("initial selected save failed", w.Code)
	}
	q.Mapping.Case.Facts[0].Conversion.Column = "Changed planned column"
	s.cfg.JpackBin = filepath.Join(t.TempDir(), "unavailable-runtime")
	w = retainedB4Request(t, s, ts, "PUT", "/api/input-preparations/"+saved.Document.ID, map[string]any{"name": "Changed", "recipe": inputRecipe{Qualification: q}}, saved.SHA256)
	if w.Code != 422 {
		t.Fatal("changed selected candidate reused an earlier check", w.Code)
	}
	after, err := s.preparationRepository().Get(t.Context(), saved.Document.ID)
	if err != nil || after.SHA256 != saved.SHA256 || len(after.Document.Recipes) != 1 {
		t.Fatal("failed fresh check wrote recipe history", err)
	}
}

type retainedB4ConcurrentCAS struct {
	inputPreparationRepository
	compete         bool
	failAfterCommit bool
}

func (r *retainedB4ConcurrentCAS) CompareAndSwap(ctx context.Context, doc inputPreparation, revision string) (inputPreparationReply, error) {
	if r.compete {
		r.compete = false
		prior, err := r.inputPreparationRepository.Get(ctx, doc.ID)
		if err != nil {
			return inputPreparationReply{}, err
		}
		winner := prior.Document
		winner.Name = "Concurrent winner"
		if _, err = r.inputPreparationRepository.CompareAndSwap(ctx, winner, revision); err != nil {
			return inputPreparationReply{}, err
		}
	}
	saved, err := r.inputPreparationRepository.CompareAndSwap(ctx, doc, revision)
	if err == nil && r.failAfterCommit {
		return inputPreparationReply{}, errors.New("test write acknowledgement unavailable")
	}
	return saved, err
}

func TestRetainedB4SelectedSaveConflictAndUncertainty(t *testing.T) {
	for _, scenario := range []string{"conflict", "uncertain"} {
		t.Run(scenario, func(t *testing.T) {
			h := retainedRuntimeHarness(t)
			s, ts := newTestServer(t, false)
			s.cfg.JpackBin = h.peer
			q := retainedB4Qualification(t)
			retainedB4Reports(t, h, q)
			repository := s.preparationRepository()
			doc := repositoryDocument(5)
			saved, err := repository.CompareAndSwap(t.Context(), doc, "")
			if err != nil {
				t.Fatal(err)
			}
			wrapped := &retainedB4ConcurrentCAS{inputPreparationRepository: repository, compete: scenario == "conflict", failAfterCommit: scenario == "uncertain"}
			s.inputPreparations = wrapped
			w := retainedB4Request(t, s, ts, "PUT", "/api/input-preparations/"+doc.ID, map[string]any{"name": "Candidate", "recipe": inputRecipe{Qualification: q}}, saved.SHA256)
			if w.Code != 409 {
				t.Fatalf("save did not stop: %d %s", w.Code, w.Body.Bytes())
			}
			after, err := repository.Get(t.Context(), doc.ID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "conflict" && (after.Document.Name != "Concurrent winner" || after.Document.RecipeVersion != 1 || len(after.Document.Recipes) != 0) {
				t.Fatal("checked candidate overwrote concurrent winner")
			}
			if scenario == "uncertain" && (after.Document.Name != "Candidate" || after.Document.RecipeVersion != 2 || len(after.Document.Recipes) != 2) {
				t.Fatal("uncertain write was repeated or discarded")
			}
			if len(h.calls(t)) != 3 {
				t.Fatal("failed CAS retried its check")
			}
		})
	}
}

func TestRetainedB4SelectedCreateReplayAndActualLogicalCheck(t *testing.T) {
	for _, actual := range []bool{false, true} {
		name := "handwritten-peer"
		if actual {
			name = "actual-runtime"
		}
		t.Run(name, func(t *testing.T) {
			bin := ""
			if actual {
				bin = os.Getenv("JPACK_RETAINED_DIALECT_RUNTIME")
				if bin == "" {
					t.Skip("explicit verified Runtime fixture required: JPACK_RETAINED_DIALECT_RUNTIME")
				}
				if !filepath.IsAbs(bin) {
					t.Fatal("actual Runtime must be explicitly selected by absolute path")
				}
				raw, err := os.ReadFile(bin)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("explicit Runtime fixture sha256:%s", digestOf(raw))
			}
			h := retainedRuntimeHarness(t)
			s, ts := newTestServer(t, false)
			q := retainedB4Qualification(t)
			retainedB4Reports(t, h, q)
			s.cfg.JpackBin = h.peer
			if actual {
				s.cfg.JpackBin = bin
			}
			body := map[string]any{"name": "Five logical conversions", "recipe": inputRecipe{Qualification: q}, "chatOrigin": inputChatOrigin{ChatID: "logical", DraftID: "selected"}}
			w := retainedB4Request(t, s, ts, "POST", "/api/input-preparations", body, "")
			if w.Code != 201 {
				t.Fatalf("zero-record logical save: %d %s", w.Code, w.Body.Bytes())
			}
			var created inputPreparationReply
			if json.Unmarshal(w.Body.Bytes(), &created) != nil || len(created.Document.Records) != 0 || len(created.Document.Recipes) != 1 || len(created.Document.Recipe.Qualification.Mapping.Case.Facts) != 5 {
				t.Fatal("saved planned research-value required records or changed rules")
			}
			// No Runner or Gateway is installed in this server. A save check cannot
			// negotiate producer profiles, create a grant or acquire an input.
			if s.jobs != nil || s.localGateway != nil {
				t.Fatal("fixture unexpectedly has execution components")
			}
			s.cfg.JpackBin = filepath.Join(t.TempDir(), "unavailable-runtime")
			replayed := retainedB4Request(t, s, ts, "POST", "/api/input-preparations", body, "")
			if replayed.Code != 200 || !bytes.Equal(replayed.Body.Bytes(), w.Body.Bytes()) {
				t.Fatal("completed selected save did not reconcile before Runtime")
			}
			calls := h.calls(t)
			if !actual && len(calls) != 3 || actual && len(calls) != 0 {
				t.Fatal("wrong Runtime owner or completed-save recheck", len(calls))
			}
		})
	}
}
