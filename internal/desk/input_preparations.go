package desk

// Input preparations are private, provided data, not Runtime decisions or
// verified acquisition records. Imports retain their exact source bytes and
// stable row identities. The server owns revision checks and import commits.
import (
	"bytes"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const inputRecordLimit = 1000

var inputPreparationID = regexp.MustCompile(`^prep_[a-f0-9]{32}$`)

type inputTemplateOrigin struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Digest  string `json:"digest"`
	License string `json:"license"`
}

func validateInputTemplate(t *inputTemplateOrigin) error {
	if t == nil {
		return nil
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,79}$`).MatchString(t.ID) || strings.TrimSpace(t.Name) == "" || len(t.Name) > 160 || !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(t.Digest) || strings.TrimSpace(t.License) == "" || len(t.License) > 160 {
		return errors.New("Invalid Desk template origin.")
	}
	return nil
}

type inputRecipe struct {
	Template      *inputTemplateOrigin `json:"template,omitempty"`
	Intent        string               `json:"intent"`
	Research      *inputResearchRecipe `json:"research,omitempty"`
	Qualification *inputQualification  `json:"qualification,omitempty"`
	// No credentials, model-derived facts or qualification result enter this
	// initial supplied-input recipe. Live discovery is a separate worker.
}
type preparedInputRecord struct {
	Review   *inputRecordReview   `json:"review,omitempty"`
	ID       string               `json:"id"`
	Research *inputResearchRecord `json:"research,omitempty"`
	Name     string               `json:"name"`
	Values   map[string]string    `json:"values"`
	SourceID string               `json:"sourceId"`
	Row      int                  `json:"row"`
	Class    string               `json:"class"`
	Status   string               `json:"status"`
}
type inputImport struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Digest        string `json:"digest"`
	Content       string `json:"content"`
	At            string `json:"at"`
	RecipeVersion int    `json:"recipeVersion"`
}
type inputActivity struct {
	Kind          string `json:"kind"`
	At            string `json:"at"`
	Count         int    `json:"count,omitempty"`
	RecipeVersion int    `json:"recipeVersion"`
}
type inputRecipeRevision struct {
	Version int         `json:"version"`
	Name    string      `json:"name"`
	Recipe  inputRecipe `json:"recipe"`
	At      string      `json:"at"`
}
type inputChatOrigin struct {
	ChatID        string `json:"chatId"`
	DraftID       string `json:"draftId"`
	RequestDigest string `json:"requestDigest,omitempty"`
}

type inputPreparation struct {
	Reviews       []inputRecordReviewEvent `json:"reviews,omitempty"`
	ChatOrigin    *inputChatOrigin         `json:"chatOrigin,omitempty"`
	Version       int                      `json:"version"`
	ID            string                   `json:"id"`
	Name          string                   `json:"name"`
	Recipe        inputRecipe              `json:"recipe"`
	RecipeVersion int                      `json:"recipeVersion"`
	Recipes       []inputRecipeRevision    `json:"recipes"`
	CreatedAt     string                   `json:"createdAt"`
	UpdatedAt     string                   `json:"updatedAt"`
	Records       []preparedInputRecord    `json:"records"`
	Imports       []inputImport            `json:"imports"`
	Activity      []inputActivity          `json:"activity"`
}
type inputPreparationReply struct {
	SHA256   string           `json:"sha256"`
	Document inputPreparation `json:"document"`
}

func decodeInputPreparation(id string, raw []byte) (inputPreparationReply, error) {
	var result inputPreparationReply
	var err error
	if err = decodeDataJSON(raw, &result.Document); err != nil {
		return result, err
	}
	if result.Document.Version != 1 || result.Document.ID != id || len(result.Document.Records) > inputRecordLimit {
		return result, errors.New("invalid input preparation")
	}
	if err = validateInputTemplate(result.Document.Recipe.Template); err != nil {
		return result, err
	}
	if err = validateInputResearch(result.Document.Recipe.Research); err != nil {
		return result, err
	}
	if err = validateInputQualification(result.Document.Recipe.Qualification); err != nil {
		return result, err
	}
	if err = validateInputRecordReviews(result.Document); err != nil {
		return result, err
	}
	result.SHA256 = digestOf(raw)
	return result, nil
}
func inputRequest(r *http.Request, target any) error {
	raw, err := readBounded(r.Body, 2<<20)
	if err != nil {
		return err
	}
	if !validUTF8(raw) {
		return errors.New("invalid UTF-8")
	}
	// Reject ambiguous members anywhere, including recipe and import fields.
	if err = rejectDuplicateMembers(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("one JSON value required")
	}
	return nil
}
func (s *Server) handleInputPreparations(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("preparation")
	if r.URL.RawQuery != "" || id != "" && !inputPreparationID.MatchString(id) {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid input preparation request.")
		return
	}
	repository := s.preparationRepository()
	storageError := func(err error, fallback string) {
		if errors.Is(err, errInputPreparationBusy) {
			w.Header().Set("Retry-After", "1")
			writeJSONCoded(w, 503, "storage-busy", "Dialects are being saved. Try again shortly.")
		} else {
			writeJSONCoded(w, 409, CodeBadRequest, fallback)
		}
	}
	if r.Method == "GET" && id == "" {
		items, err := repository.List(r.Context())
		if err != nil {
			storageError(err, "Input preparations could not be listed.")
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	var err error
	var saved inputPreparationReply
	if id != "" {
		saved, err = repository.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeJSONCoded(w, 404, CodeNotFound, "Input preparation not found.")
			} else {
				storageError(err, "Input preparation could not be read.")
			}
			return
		}
	}
	if r.Method == "GET" {
		writeJSON(w, 200, saved)
		return
	}
	if id != "" && r.Header.Get("If-Match") != saved.SHA256 {
		writeJSONCoded(w, 409, CodeStale, "Dialects changed. Reload before saving.")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	doc := saved.Document
	// Early local files may retain only their current recipe. Preserve only
	// what is actually available; never reconstruct missing older versions.
	if id != "" && len(doc.Recipes) == 0 {
		doc.Recipes = []inputRecipeRevision{{doc.RecipeVersion, doc.Name, doc.Recipe, doc.UpdatedAt}}
	}
	if strings.HasSuffix(r.URL.Path, "/record-reviews") {
		changed, err := s.reviewInputRecord(r, &doc, now)
		if err != nil {
			writeJSONCoded(w, 422, CodeBadRequest, err.Error())
			return
		}
		if !changed {
			writeJSON(w, 200, saved)
			return
		}
	} else if strings.HasSuffix(r.URL.Path, "/imports") {
		var request struct {
			Kind    string `json:"kind"`
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		if inputRequest(r, &request) != nil || len(request.Name) > 256 {
			writeJSONCoded(w, 400, CodeBadRequest, "Choose a CSV file or a list of web URLs.")
			return
		}
		sourceID := "source_" + digestOf([]byte(request.Kind+"\n"+request.Content))
		for _, prior := range doc.Imports {
			if prior.ID == sourceID {
				writeJSON(w, 200, saved)
				return
			}
		}
		records, err := normalizeInputImport(request.Kind, request.Content, sourceID)
		if err != nil {
			writeJSONCoded(w, 422, CodeBadRequest, err.Error())
			return
		}
		if len(doc.Records)+len(records) > inputRecordLimit {
			writeJSONCoded(w, 422, CodeTooLarge, "A preparation can contain at most 1,000 records.")
			return
		}
		doc.Imports = append(doc.Imports, inputImport{sourceID, request.Kind, request.Name, "sha256:" + digestOf([]byte(request.Content)), request.Content, now, doc.RecipeVersion})
		doc.Records = append(doc.Records, records...)
		doc.Activity = append(doc.Activity, inputActivity{"imported", now, len(records), doc.RecipeVersion})
	} else {
		var request struct {
			Name       string           `json:"name"`
			Recipe     inputRecipe      `json:"recipe"`
			ChatOrigin *inputChatOrigin `json:"chatOrigin,omitempty"`
		}
		if inputRequest(r, &request) != nil || strings.TrimSpace(request.Name) == "" || len(request.Name) > 160 || len(request.Recipe.Intent) > 12000 {
			writeJSONCoded(w, 400, CodeBadRequest, "Provide a name and a preparation description.")
			return
		}
		if err := validateInputTemplate(request.Recipe.Template); err != nil {
			writeJSONCoded(w, 422, CodeBadRequest, err.Error())
			return
		}
		if err := validateInputResearch(request.Recipe.Research); err != nil {
			writeJSONCoded(w, 422, CodeBadRequest, err.Error())
			return
		}
		if err := validateInputQualification(request.Recipe.Qualification); err != nil {
			writeJSONCoded(w, 422, CodeBadRequest, err.Error())
			return
		}

		proposedID := ""
		if origin := request.ChatOrigin; origin != nil {
			valid := regexp.MustCompile(`^[a-zA-Z0-9-]{1,80}$`)
			if id != "" || !valid.MatchString(origin.ChatID) || !valid.MatchString(origin.DraftID) || origin.RequestDigest != "" {
				writeJSONCoded(w, 400, CodeBadRequest, "Invalid chat recipe reference.")
				return
			}
			proposedID = "prep_" + digestOf([]byte(origin.ChatID + "\n" + origin.DraftID))[:32]
			raw, _ := json.Marshal(request)
			origin.RequestDigest = digestOf(raw)
			prior, e := repository.Get(r.Context(), proposedID)
			if e == nil {
				if prior.Document.ChatOrigin == nil || prior.Document.ChatOrigin.RequestDigest != origin.RequestDigest {
					writeJSONCoded(w, 409, CodeStale, "This proposal was already saved with different settings. Open its input to make changes.")
					return
				}
				writeJSON(w, 200, prior)
				return
			}
			if !errors.Is(e, os.ErrNotExist) {
				storageError(e, "The saved chat input could not be read.")
				return
			}
		}
		// A fresh check of the exact candidate precedes the existing CAS write.
		// Loading historical recipes never depends on an available Runtime.
		if inputQualificationChanged(saved.Document.Recipe.Qualification, request.Recipe.Qualification) {
			if err := s.checkInputQualification(r.Context(), request.Recipe.Qualification); err != nil {
				writeJSONCoded(w, 422, CodeBadRequest, err.Error())
				return
			}
		}

		if id == "" {
			nonce := make([]byte, 16)
			if _, err = rand.Read(nonce); err != nil {
				writeJSONCoded(w, 500, CodeBadRequest, "The preparation could not be created.")
				return
			}
			doc = inputPreparation{Version: 1, ChatOrigin: request.ChatOrigin, ID: "prep_" + hex.EncodeToString(nonce), RecipeVersion: 1, CreatedAt: now, Records: []preparedInputRecord{}, Imports: []inputImport{}, Activity: []inputActivity{{Kind: "created", At: now, RecipeVersion: 1}}}
			if proposedID != "" {
				doc.ID = proposedID
			}
		} else {
			doc.RecipeVersion++
			doc.Activity = append(doc.Activity, inputActivity{Kind: "recipe-updated", At: now, RecipeVersion: doc.RecipeVersion})
		}
		doc.Name = strings.TrimSpace(request.Name)
		doc.Recipe = request.Recipe
		doc.Recipes = append(doc.Recipes, inputRecipeRevision{doc.RecipeVersion, doc.Name, doc.Recipe, now})
	}
	doc.UpdatedAt = now
	result, err := repository.CompareAndSwap(r.Context(), doc, saved.SHA256)
	if err != nil {
		if errors.Is(err, errInputPreparationConflict) {
			// A concurrent create of the same chat proposal is a replay. Return
			// its current snapshot, never overwrite it or repeat an uncertain write.
			if id == "" && doc.ChatOrigin != nil {
				prior, e := repository.Get(r.Context(), doc.ID)
				if e == nil && prior.Document.ChatOrigin != nil && prior.Document.ChatOrigin.RequestDigest == doc.ChatOrigin.RequestDigest {
					writeJSON(w, 200, prior)
					return
				}
			}
			writeJSONCoded(w, 409, CodeStale, "Dialects changed. Reload before saving.")
		} else if errors.Is(err, errInputPreparationCapacity) {
			writeJSONCoded(w, 422, CodeTooLarge, "This workspace can contain at most 100 preparations.")
		} else {
			storageError(err, "Input preparation could not be saved.")
		}
		return
	}
	code := 200
	if id == "" {
		code = 201
	}
	writeJSON(w, code, result)
}

func normalizeInputImport(kind, content, sourceID string) ([]preparedInputRecord, error) {
	var rows []map[string]string
	if kind == "csv" {
		reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(content, "\ufeff")))
		headers, err := reader.Read()
		if err != nil || len(headers) > 100 {
			return nil, errors.New("CSV needs a header with at most 100 columns.")
		}
		seen := map[string]bool{}
		for i, h := range headers {
			h = strings.TrimSpace(h)
			if h == "" || len(h) > 160 || seen[h] {
				return nil, errors.New("CSV column names must be nonempty and unique.")
			}
			seen[h] = true
			headers[i] = h
		}
		for {
			cells, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, errors.New("Every CSV record must match its header.")
			}
			values := map[string]string{}
			for i, cell := range cells {
				if len(cell) > 16000 {
					return nil, errors.New("An input field exceeds 16,000 characters.")
				}
				values[headers[i]] = cell
			}
			rows = append(rows, values)
			if len(rows) > inputRecordLimit {
				return nil, errors.New("A preparation can contain at most 1,000 records.")
			}
		}
	} else if kind == "urls" {
		seen := map[string]bool{}
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			u, err := url.Parse(line)
			if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || len(line) > 4096 {
				return nil, errors.New("Provide one complete HTTP or HTTPS URL per line, without credentials.")
			}
			u.Fragment = ""
			u.Host = strings.ToLower(u.Host)
			key := u.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			rows = append(rows, map[string]string{"url": key})
			if len(rows) > inputRecordLimit {
				return nil, errors.New("A preparation can contain at most 1,000 records.")
			}
		}
	} else {
		return nil, errors.New("Choose CSV or supplied URLs.")
	}
	if len(rows) == 0 {
		return nil, errors.New("No input records were provided.")
	}
	records := make([]preparedInputRecord, 0, len(rows))
	for i, values := range rows {
		name := ""
		for _, key := range []string{"name", "Name", "company", "Company", "title", "Title", "url"} {
			if strings.TrimSpace(values[key]) != "" {
				name = values[key]
				break
			}
		}
		if name == "" {
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if strings.TrimSpace(values[key]) != "" {
					name = values[key]
					break
				}
			}
		}
		if name == "" {
			name = "—"
		}
		if len([]rune(name)) > 160 {
			name = string([]rune(name)[:160])
		}
		identity, _ := json.Marshal([]any{sourceID, i + 1})
		status := "provided"
		if kind == "urls" {
			status = "needs-research"
		}
		records = append(records, preparedInputRecord{ID: "row_" + digestOf(identity), Name: name, Values: values, SourceID: sourceID, Row: i + 1, Class: "asserted", Status: status})
	}
	return records, nil
}

func rejectDuplicateMembers(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var read func(int) error
	read = func(depth int) error {
		if depth > 64 {
			return errors.New("JSON nesting exceeds limit")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[strings.ToLower(name)] {
					return errors.New("duplicate JSON member")
				}
				seen[strings.ToLower(name)] = true
				if err = read(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err = read(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := read(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("one JSON value required")
	}
	return nil
}
