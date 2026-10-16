package desk

// Dialect drafts are project artifacts, not conversation checkpoints or runnable
// preparations. Incomplete form values are retained; promotion uses the normal
// recipe validators and never starts research.
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const dialectDraftLimit = 256 << 10

var dialectDraftID = regexp.MustCompile(`^draft_[a-f0-9]{32}$`)
var dialectDraftKey = regexp.MustCompile(`^[a-zA-Z0-9-]{1,80}$`)

type dialectDraftOrigin struct {
	ChatID  string `json:"chatId"`
	DraftID string `json:"draftId"`
}
type dialectDraftDocument struct {
	Version      int                 `json:"version"`
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Recipe       inputRecipe         `json:"recipe"`
	Origin       *dialectDraftOrigin `json:"origin,omitempty"`
	CreatedAt    string              `json:"createdAt"`
	UpdatedAt    string              `json:"updatedAt"`
	SavedInputID string              `json:"savedInputId,omitempty"`
	Discarded    bool                `json:"discarded,omitempty"`
}
type dialectDraftReply struct {
	SHA256   string               `json:"sha256"`
	Document dialectDraftDocument `json:"document"`
}
type dialectDraftRequest struct {
	Key    string              `json:"key,omitempty"`
	Origin *dialectDraftOrigin `json:"origin,omitempty"`
	Name   string              `json:"name"`
	Recipe inputRecipe         `json:"recipe"`
}

func dialectDraftIdentity(key string, origin *dialectDraftOrigin) string {
	if origin != nil {
		key = "chat\n" + origin.ChatID + "\n" + origin.DraftID
	} else {
		key = "manual\n" + key
	}
	return "draft_" + digestOf([]byte(key))[:32]
}
func dialectDraftPreparationID(d dialectDraftDocument) string {
	if d.Origin != nil {
		return "prep_" + digestOf([]byte(d.Origin.ChatID + "\n" + d.Origin.DraftID))[:32]
	}
	return "prep_" + digestOf([]byte("dialect-draft\n" + d.ID))[:32]
}
func dialectDraftChatOrigin(d dialectDraftDocument) *inputChatOrigin {
	if d.Origin == nil {
		return nil
	}
	origin := &inputChatOrigin{ChatID: d.Origin.ChatID, DraftID: d.Origin.DraftID}
	raw, _ := json.Marshal(struct {
		Name       string           `json:"name"`
		Recipe     inputRecipe      `json:"recipe"`
		ChatOrigin *inputChatOrigin `json:"chatOrigin,omitempty"`
	}{strings.TrimSpace(d.Name), d.Recipe, origin})
	origin.RequestDigest = digestOf(raw)
	return origin
}
func validateDialectDraft(name string, recipe inputRecipe) error {
	bad := errors.New("The dialect draft exceeds its supported fields or limits.")
	if len(name) > 160 || len(recipe.Intent) > 12000 {
		return bad
	}
	if err := validateInputTemplate(recipe.Template); err != nil {
		return err
	}
	// The draft can retain unfinished fields and connections, but its shape,
	// identifiers, and storage remain bounded. Runnable validation stays strict.
	if r := recipe.Research; r != nil {
		if r.Target == "" {
			r.Target = "records"
		}
		if r.Version != 1 || !contains([]string{"records", "companies", "people", "people-at-companies"}, r.Target) || len(r.Fields) > 30 || len(r.Criteria) > 30 || len(r.CompanyIDs)+len(r.CompanyInputs) > 50 {
			return bad
		}
		if len(r.AI.Connection) > 128 || len(r.AI.Revision) > 256 || len(r.AI.Model) > 128 || len(r.AI.Effort) > 16 {
			return bad
		}
		if r.Search != nil && (len(r.Search.Connection) > 128 || len(r.Search.Revision) > 256) {
			return bad
		}
		l := r.Limits
		if l.Records < 0 || l.Records > 50 || l.Searches < 0 || l.Searches > 30 || l.Pages < 0 || l.Pages > 100 || l.ModelCalls < 0 || l.ModelCalls > 60 {
			return bad
		}
		if r.Execution != nil {
			if err := r.Execution.Validate(); err != nil {
				return err
			}
		}
		if r.Retries != nil && (r.Retries.MaxRetries < 0 || r.Retries.MaxRetries > 3 || !contains([]string{"off", "temporary"}, r.Retries.Mode)) {
			return bad
		}
		for _, f := range r.Fields {
			if len(f.Name) > 64 || len(f.Description) > 1000 || !contains([]string{"string", "number", "boolean"}, f.Type) {
				return bad
			}
		}
		for _, c := range r.Criteria {
			if len(c.ID) > 64 || len(c.Description) > 2000 || len(c.QualificationTarget) > 512 {
				return bad
			}
		}
		for _, id := range r.CompanyIDs {
			if len(id) > 128 {
				return bad
			}
		}
		for _, ref := range r.CompanyInputs {
			if !inputPreparationID.MatchString(ref.PreparationID) || len(ref.Revision) > 256 || len(ref.EntityID) > 128 {
				return bad
			}
		}
	}
	return validateInputQualification(recipe.Qualification)
}
func (s *Server) ensureDialectDraft(ctx context.Context, request dialectDraftRequest) (dialectDraftReply, error) {
	repository := s.draftRepository()
	id := dialectDraftIdentity(request.Key, request.Origin)
	prior, err := repository.Get(ctx, id)
	if err == nil {
		return prior, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return prior, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	doc := dialectDraftDocument{Version: 1, ID: id, Name: request.Name, Recipe: request.Recipe, Origin: request.Origin, CreatedAt: now, UpdatedAt: now}
	// Recover a legacy Save-to-Dialects whose chat receipt was interrupted.
	if origin := request.Origin; origin != nil {
		legacyID := dialectDraftPreparationID(doc)
		legacy, e := s.preparationRepository().Get(ctx, legacyID)
		if e == nil {
			if legacy.Document.ChatOrigin == nil || legacy.Document.ChatOrigin.ChatID != origin.ChatID || legacy.Document.ChatOrigin.DraftID != origin.DraftID {
				return prior, errors.New("The saved dialect has a different proposal origin.")
			}
			doc.SavedInputID = legacyID
		} else if !errors.Is(e, os.ErrNotExist) {
			return prior, e
		}
	}
	saved, err := repository.CompareAndSwap(ctx, doc, "")
	if errors.Is(err, errDialectDraftConflict) {
		// Only a known create conflict can reopen an existing draft. An uncertain
		// write stops; it never authorizes another mutation or publication.
		return repository.Get(ctx, id)
	}
	return saved, err
}
func (s *Server) handleDialectDrafts(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("draft")
	if r.URL.RawQuery != "" || id != "" && !dialectDraftID.MatchString(id) {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid dialect draft request.")
		return
	}
	fail := func(err error) {
		if errors.Is(err, errDialectDraftBusy) {
			w.Header().Set("Retry-After", "1")
			writeJSONCoded(w, 503, "storage-busy", "Dialect drafts are being saved. Try again shortly.")
		} else if errors.Is(err, errDialectDraftConflict) {
			writeJSONCoded(w, 409, CodeStale, "The dialect draft changed. Reload before saving.")
		} else {
			writeJSONCoded(w, 409, CodeBadRequest, "The dialect draft could not be saved or read: "+err.Error())
		}
	}
	repository := s.draftRepository()
	if r.Method == "GET" {
		if id == "" {
			items, err := repository.List(r.Context())
			if err != nil {
				fail(err)
				return
			}
			writeJSON(w, 200, map[string]any{"items": items})
			return
		}
		saved, err := repository.Get(r.Context(), id)
		if errors.Is(err, os.ErrNotExist) {
			writeJSONCoded(w, 404, CodeNotFound, "Dialect draft not found.")
			return
		}
		if err != nil {
			fail(err)
			return
		}
		writeJSON(w, 200, saved)
		return
	}
	// Parse request bodies before taking the workflow lease. Slow clients must
	// not hold draft persistence or block unrelated promotion/recovery.
	var request dialectDraftRequest
	if id == "" {
		if inputRequest(r, &request) != nil || request.Origin == nil && !dialectDraftKey.MatchString(request.Key) || request.Origin != nil && (request.Key != "" || !dialectDraftKey.MatchString(request.Origin.ChatID) || !dialectDraftKey.MatchString(request.Origin.DraftID)) {
			writeJSONCoded(w, 400, CodeBadRequest, "Provide a stable dialect draft reference.")
			return
		}
		if err := validateDialectDraft(request.Name, request.Recipe); err != nil {
			writeJSONCoded(w, 422, CodeBadRequest, err.Error())
			return
		}
	} else if r.Method == "PUT" {
		var edit struct {
			Name   string      `json:"name"`
			Recipe inputRecipe `json:"recipe"`
		}
		if inputRequest(r, &edit) != nil {
			writeJSONCoded(w, 400, CodeBadRequest, "Invalid dialect draft.")
			return
		}
		if err := validateDialectDraft(edit.Name, edit.Recipe); err != nil {
			writeJSONCoded(w, 422, CodeBadRequest, err.Error())
			return
		}
		request.Name, request.Recipe = edit.Name, edit.Recipe
	}
	unlock, err := s.lockDialectDraftWorkflow(r.Context())
	if err != nil {
		fail(err)
		return
	}
	defer unlock()
	if id == "" {
		saved, err := s.ensureDialectDraft(r.Context(), request)
		if err != nil {
			fail(err)
			return
		}
		writeJSON(w, 200, saved)
		return
	}
	saved, err := repository.Get(r.Context(), id)
	if errors.Is(err, os.ErrNotExist) {
		writeJSONCoded(w, 404, CodeNotFound, "Dialect draft not found.")
		return
	}
	if err != nil {
		fail(err)
		return
	}
	if saved.Document.Discarded {
		writeJSONCoded(w, 409, CodeStale, "This dialect draft was discarded.")
		return
	}
	promoting := strings.HasSuffix(r.URL.Path, "/publish")
	if promoting && saved.Document.SavedInputID != "" {
		writeJSON(w, 200, saved)
		return
	}
	if r.Header.Get("If-Match") != saved.SHA256 || saved.Document.SavedInputID != "" {
		writeJSONCoded(w, 409, CodeStale, "The dialect draft changed. Reload before saving.")
		return
	}
	doc := saved.Document
	if promoting {
		prepID, err := s.publishDialectDraft(r.Context(), doc)
		if err != nil {
			writeJSONCoded(w, 422, CodeBadRequest, err.Error())
			return
		}
		doc.SavedInputID = prepID
	} else {
		// A preparation may have been committed before its draft receipt. Never
		// accept new draft edits over that completed operation after an interruption.
		_, e := s.preparationRepository().Get(r.Context(), dialectDraftPreparationID(doc))
		if e == nil {
			writeJSONCoded(w, 409, CodeStale, "This draft was already saved to Dialects. Finish saving to open it.")
			return
		}
		if !errors.Is(e, os.ErrNotExist) {
			fail(e)
			return
		}
		if r.Method == "DELETE" {
			doc.Discarded = true
		} else {
			doc.Name = request.Name
			doc.Recipe = request.Recipe
		}
	}
	doc.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	result, err := repository.CompareAndSwap(r.Context(), doc, saved.SHA256)
	if err != nil {
		fail(err)
		return
	}
	writeJSON(w, 200, result)
}
func (s *Server) publishDialectDraft(ctx context.Context, d dialectDraftDocument) (string, error) {
	repository := s.preparationRepository()
	id := dialectDraftPreparationID(d)
	if prior, e := repository.Get(ctx, id); e == nil {
		if expected := dialectDraftChatOrigin(d); expected != nil && (prior.Document.ChatOrigin == nil || prior.Document.ChatOrigin.RequestDigest != expected.RequestDigest) {
			return "", errors.New("This proposal was already saved with different settings. Open its dialect to review the saved recipe.")
		}
		return prior.Document.ID, nil
	} else if !errors.Is(e, os.ErrNotExist) {
		return "", e
	}
	if strings.TrimSpace(d.Name) == "" {
		return "", errors.New("Enter a name.")
	}
	if err := validateInputResearch(d.Recipe.Research); err != nil {
		return "", err
	}
	if err := validateInputQualification(d.Recipe.Qualification); err != nil {
		return "", err
	}
	if err := s.checkInputQualification(ctx, d.Recipe.Qualification); err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	doc := inputPreparation{Version: 1, ID: id, Name: strings.TrimSpace(d.Name), Recipe: d.Recipe, RecipeVersion: 1, CreatedAt: now, UpdatedAt: now, Records: []preparedInputRecord{}, Imports: []inputImport{}, Activity: []inputActivity{{Kind: "created", At: now, RecipeVersion: 1}}, Recipes: []inputRecipeRevision{{Version: 1, Name: strings.TrimSpace(d.Name), Recipe: d.Recipe, At: now}}}
	doc.ChatOrigin = dialectDraftChatOrigin(d)
	_, err := repository.CompareAndSwap(ctx, doc, "")
	if errors.Is(err, errInputPreparationConflict) {
		// Only an explicit create conflict permits reconciliation. A transport or
		// verification error remains uncertain; do not replay the mutation.
		prior, readErr := repository.Get(ctx, id)
		if readErr != nil {
			return "", readErr
		}
		if expected := dialectDraftChatOrigin(d); expected != nil && (prior.Document.ChatOrigin == nil || prior.Document.ChatOrigin.RequestDigest != expected.RequestDigest) {
			return "", errors.New("This proposal was already saved with different settings. Open its dialect to review the saved recipe.")
		}
		return prior.Document.ID, nil
	}
	if errors.Is(err, errInputPreparationCapacity) {
		return "", errors.New("This workspace can contain at most 100 preparations.")
	}
	return id, err
}

// Migrate old chat-only proposals before a chat can be deleted, and retain new
// proposals from every conversation writer. Existing drafts are never replaced.
func (s *Server) retainConversationDialectDrafts(ctx context.Context, documents ...[]byte) error {
	requests := []dialectDraftRequest{}
	for _, raw := range documents {
		var doc struct {
			Chats []struct {
				ID     string `json:"id"`
				Drafts []struct {
					DraftID      string `json:"draftId"`
					Name         string `json:"name"`
					Intent       string `json:"intent"`
					SavedInputID string `json:"savedInputId"`
					inputResearchRecipe
				} `json:"inputDrafts"`
			} `json:"chats"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return err
		}
		for _, chat := range doc.Chats {
			for _, p := range chat.Drafts {
				if p.SavedInputID != "" {
					continue
				}
				if !dialectDraftKey.MatchString(chat.ID) || !dialectDraftKey.MatchString(p.DraftID) {
					return errors.New("Invalid dialect proposal reference.")
				}
				research := p.inputResearchRecipe
				research.Version = 1
				research.AI = inputResearchAI{}
				research.Search = nil
				research.Limits.Searches = 0
				recipe := inputRecipe{Intent: p.Intent, Research: &research}
				if err := validateDialectDraft(p.Name, recipe); err != nil {
					return err
				}
				requests = append(requests, dialectDraftRequest{Name: p.Name, Recipe: recipe, Origin: &dialectDraftOrigin{ChatID: chat.ID, DraftID: p.DraftID}})
			}
		}
	}
	if len(requests) == 0 {
		return nil
	}
	unlock, err := s.lockDialectDraftWorkflow(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	for _, request := range requests {
		if _, err = s.ensureDialectDraft(ctx, request); err != nil {
			return err
		}
	}
	return nil
}
