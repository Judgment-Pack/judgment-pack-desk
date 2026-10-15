package desk

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

func (s *Server) handleInputResearch(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	prep, id := r.PathValue("preparation"), r.PathValue("research")
	if !inputPreparationID.MatchString(prep) || id != "" && !inputResearchID.MatchString(id) || r.URL.RawQuery != "" {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid research request.")
		return
	}
	fail := func(e error) { writeJSONCoded(w, 409, CodeBadRequest, e.Error()) }
	root, e := s.inputResearchRoot(r.Method != "GET")
	if errors.Is(e, os.ErrNotExist) && r.Method == "GET" && id == "" {
		writeJSON(w, 200, map[string]any{"items": []any{}})
		return
	}
	if e != nil {
		fail(errors.New("Research storage is unavailable."))
		return
	}
	defer root.Close()
	// Serialize all admissions across windows and processes before locking a
	// run or preparation. The worker never takes this admission lock.
	if r.Method != "GET" {
		admission, err := root.Open(".")
		if err != nil {
			fail(err)
			return
		}
		defer admission.Close()
		if err = lockPrivateData(admission, true); err != nil {
			fail(errors.New("Another research change is being saved. Try again after it finishes."))
			return
		}
	}
	if id != "" {
		runRoot, e := openInputResearch(root, id, false)
		if e != nil {
			writeJSONCoded(w, 404, CodeNotFound, "Research run not found.")
			return
		}
		defer runRoot.Close()
		run, e := s.researchJournal(runRoot, id).Read(r.Context())
		if e != nil || run.PreparationID != prep {
			writeJSONCoded(w, 404, CodeNotFound, "Research run not found.")
			return
		}
		if r.Method == "GET" {
			if strings.HasSuffix(r.URL.Path, "/activity") {
				writeJSON(w, 200, researchActivity(run))
			} else {
				writeJSON(w, 200, run)
			}
			return
		}
		if strings.HasSuffix(r.URL.Path, "/budget-review") || strings.HasSuffix(r.URL.Path, "/budget-decision") {
			s.handleResearchBudget(w, r, root, runRoot, run)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/authorize") {
			s.handleAuthorizeResearch(w, r, root, runRoot, run)
			return
		}
		var empty struct{}
		if inputRequest(r, &empty) != nil {
			writeJSONCoded(w, 400, CodeBadRequest, "No extra research arguments are accepted.")
			return
		}
		if strings.HasSuffix(r.URL.Path, "/confirm") {
			if e = s.researchInputAvailable(r.Context(), root, prep, id); e != nil {
				fail(e)
				return
			}
		}
		run, e = s.researchJournal(runRoot, id).Update(r.Context(), func(run *inputResearchRun) error {
			if strings.HasSuffix(r.URL.Path, "/stop") {
				if run.Approval != nil && run.Approval.UsedAt == "" {
					run.Approval.RevokedAt = time.Now().UTC().Format(time.RFC3339Nano)
				}
				if pending := pendingResearchBudget(*run); pending != nil {
					actor := inputReviewActor{Kind: "local-access"}
					if held, ok := s.sessionOf(r); ok {
						actor = inputReviewActor{Kind: "session", Subject: held.subject, Issuer: held.issuer, Name: held.name}
					}
					if err := decideResearchBudget(run, pending.ID, "decline", actor); err != nil {
						return err
					}
					run.State = "stopped"
				}
				if run.State == "review" || run.State == "queued" {
					run.State = "stopped"
				}
				run.StopRequested = true
				return nil
			}
			if strings.HasSuffix(r.URL.Path, "/confirm") {
				if run.State != "review" {
					return errors.New("This research review was already confirmed or stopped.")
				}
				if run.Approval != nil {
					return errors.New("This review is reserved for its chat allowance. Review a new run to start manually.")
				}
				if _, err := s.validateResearchAdmission(r, *run); err != nil {
					return err
				}
				if run.Reprocess {
					if err := s.verifyResearchContinuation(r.Context(), runRoot, *run); err != nil {
						return err
					}
				}
				run.State = "queued"
				return nil
			}
			return errors.New("Unknown research action.")
		})
		if e != nil {
			fail(e)
			return
		}
		writeJSON(w, 200, run)
		return
	}
	if r.Method == "GET" {
		dir, e := root.Open(".")
		if e != nil {
			fail(e)
			return
		}
		entries, e := dir.ReadDir(-1)
		dir.Close()
		if e != nil {
			fail(e)
			return
		}
		runs := []inputResearchSummary{}
		for _, entry := range entries {
			if !inputResearchID.MatchString(entry.Name()) {
				continue
			}
			rr, e := openInputResearch(root, entry.Name(), false)
			if e != nil {
				fail(errors.New("A research run could not be opened."))
				return
			}
			run, e := s.researchJournal(rr, entry.Name()).Read(r.Context())
			rr.Close()
			if e != nil {
				fail(errors.New("A research run could not be read."))
				return
			}
			if run.PreparationID == prep {
				runs = append(runs, researchSummary(run))
			}
		}
		sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt > runs[j].CreatedAt })
		writeJSON(w, 200, map[string]any{"items": runs})
		return
	}
	var request struct {
		ContinueFrom  string          `json:"continueFrom,omitempty"`
		RecordIDs     []string        `json:"-"`
		SelectionJSON json.RawMessage `json:"recordIds,omitempty"`
	}
	if inputRequest(r, &request) != nil || request.ContinueFrom != "" && !inputResearchID.MatchString(request.ContinueFrom) {
		writeJSONCoded(w, 400, CodeBadRequest, "Save the research recipe before reviewing it.")
		return
	}
	if len(request.SelectionJSON) > 0 {
		if json.Unmarshal(request.SelectionJSON, &request.RecordIDs) != nil || len(request.RecordIDs) == 0 {
			fail(errors.New("Select at least one record to reprocess."))
			return
		}
	}
	if request.ContinueFrom != "" {
		fail(errors.New("Select records and choose Reprocess from saved sources. Whole-run continuation is no longer available."))
		return
	}
	if e = s.researchInputAvailable(r.Context(), root, prep, ""); e != nil {
		fail(e)
		return
	}
	p, e := s.preparationRepository().Read(r.Context())
	if e != nil {
		fail(e)
		return
	}
	defer p.Close()
	saved, e := p.Get(prep)
	if e != nil {
		fail(e)
		return
	}
	if r.Header.Get("If-Match") != saved.SHA256 {
		writeJSONCoded(w, 409, CodeStale, "Dialects changed. Reload before reviewing research.")
		return
	}
	recipe := saved.Document.Recipe.Research
	if recipe == nil || strings.TrimSpace(saved.Document.Recipe.Intent) == "" {
		fail(errors.New("Save research instructions and select a connection first."))
		return
	}
	if e = validateInputResearch(recipe); e != nil {
		fail(e)
		return
	}
	if len(request.RecordIDs) == 0 {
		if e = s.validateResearchSearchConnection(r.Context(), *recipe); e != nil {
			fail(e)
			return
		}
	}
	if e = s.validateResearchConnection(r.Context(), recipe.AI); e != nil {
		fail(e)
		return
	}
	if _, e = s.researchVerifier(); e != nil {
		fail(e)
		return
	}
	gateway, e := s.configuredResearch()
	if e != nil {
		fail(e)
		return
	}
	var nonce [16]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		fail(errors.New("Research could not be created."))
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	run := inputResearchRun{RelationshipVersion: 1, Version: 1, Gateway: inputResearchPin{Authority: gateway.authority, Public: gateway.signerPublic}, ID: "research_" + hex.EncodeToString(nonce[:]), PreparationID: prep, Revision: saved.SHA256, RecipeVersion: saved.Document.RecipeVersion, Intent: saved.Document.Recipe.Intent, Recipe: *recipe, CreatedAt: now, UpdatedAt: now, State: "review", Seeds: []string{}, Companies: []inputResearchEntity{}, Records: []inputResearchEntity{}, Sources: []inputResearchSource{}, Attempts: []inputResearchAttempt{}}
	run.ContractVersion = 2
	execution, err := s.researchExecution(r.Context(), recipe)
	if err != nil {
		fail(err)
		return
	}
	run.Execution = &execution
	run.Existing = researchRecordSnapshots(saved.Document)
	run.IdentityVersion = 1
	run.RowIDs = map[string]string{}
	run.EntityIDs = map[string]string{}
	for _, record := range saved.Document.Records {
		if record.Research != nil {
			key := researchEntityKey(record.Research.Entity)
			if run.RowIDs[key] == "" {
				run.RowIDs[key] = record.ID
				run.EntityIDs[key] = record.Research.Entity.ID
			}
		}
	}
	selected, err := selectedResearchCompanies(p, saved.Document, *recipe)
	if err != nil {
		fail(err)
		return
	}
	run.Companies = selected
	for _, company := range selected {
		run.Seeds = append(run.Seeds, company.URL)
	}
	for _, record := range saved.Document.Records {
		if record.Status == "needs-research" {
			if u, e := inputResearchURL(record.Values["url"]); e == nil {
				run.Seeds = append(run.Seeds, u)
			}
		}
	}
	if len(run.Seeds) > 100 {
		run.Seeds = run.Seeds[:100]
	}
	if request.RecordIDs != nil {
		if e = s.prepareResearchReprocessing(r.Context(), root, &run, saved.Document, request.RecordIDs); e != nil {
			fail(e)
			return
		}
	}
	if !run.Reprocess && recipe.Limits.Searches == 0 && len(run.Seeds) == 0 {
		fail(errors.New("Choose web search or supply starting URLs."))
		return
	}
	// Bound retained run count before accepting another run; no provider has run.
	dir, e := root.Open(".")
	if e != nil {
		fail(e)
		return
	}
	defer dir.Close()
	entries, e := dir.ReadDir(-1)
	if e != nil {
		fail(e)
		return
	}
	if len(entries) >= 501 {
		fail(errors.New("This project has reached its retained research-run limit."))
		return
	}
	if e = s.writeResearchReview(r.Context(), root, run, nil); e != nil {
		fail(e)
		return
	}
	writeJSON(w, 201, run)
}

// Caller holds a repository read view. Copy exactly the selected records;
// another collection cannot change them beneath this admission.
func selectedResearchCompanies(reader inputPreparationReader, current inputPreparation, recipe inputResearchRecipe) ([]inputResearchEntity, error) {
	result := []inputResearchEntity{}
	find := func(doc inputPreparation, id string) error {
		for _, record := range doc.Records {
			if record.Research != nil && (record.Research.Entity.Kind == "company" || record.Research.Entity.Kind == "record") && record.Research.Entity.ID == id {
				result = append(result, record.Research.Entity)
				return nil
			}
		}
		return errors.New("A selected company is no longer available. Review the selection.")
	}
	for _, id := range recipe.CompanyIDs {
		if err := find(current, id); err != nil {
			return nil, err
		}
	}
	loaded := map[string]inputPreparationReply{}
	for _, ref := range recipe.CompanyInputs {
		doc, ok := loaded[ref.PreparationID]
		if !ok {
			var err error
			doc, err = reader.Get(ref.PreparationID)
			if err != nil {
				return nil, errors.New("The selected company input could not be read. Review the selection.")
			}
			loaded[ref.PreparationID] = doc
		}
		if doc.SHA256 != ref.Revision {
			return nil, errors.New("Company records changed. Review the current selection before research.")
		}
		if err := find(doc.Document, ref.EntityID); err != nil {
			return nil, err
		}
	}
	return result, nil
}
