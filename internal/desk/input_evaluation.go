package desk

// A review freezes exactly what the user sees. Confirmation first persists a
// durable intent. A server worker then creates the fixed job and submits each
// row with a stable idempotency key; neither navigation nor a lost response
// owns that work. Retrying a failed decision is a new, explicitly reviewed batch.
import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const inputEvaluationsDir = ".desk-private/input-evaluations"
const inputEvaluationLimit = 32 << 20

var inputEvaluationID = regexp.MustCompile(`^eval_[a-f0-9]{32}$`)

type inputEvaluationRow struct {
	RecordDigest string          `json:"recordDigest,omitempty"`
	EventID      string          `json:"eventId,omitempty"`
	EventAt      string          `json:"eventAt,omitempty"`
	OccurrenceID string          `json:"occurrenceId,omitempty"`
	RecordID     string          `json:"recordId"`
	Input        json.RawMessage `json:"input,omitempty"`
	RunID        string          `json:"runId,omitempty"`
	State        string          `json:"state"`
	Result       json.RawMessage `json:"result,omitempty"`
	Signing      json.RawMessage `json:"signing,omitempty"`
	Problem      string          `json:"problem,omitempty"`
}
type inputUpgradeReview struct {
	RetryAt       []string        `json:"retryAt,omitempty"`
	Phase         string          `json:"phase,omitempty"`
	Problem       string          `json:"problem,omitempty"`
	JobID         string          `json:"jobId"`
	BaseReleaseID string          `json:"baseReleaseId"`
	BaseRevision  int             `json:"baseRevision"`
	Result        json.RawMessage `json:"result,omitempty"`
}
type inputEvaluation struct {
	SubscriptionUpgrade *subscriptionUpgradeReview `json:"subscriptionUpgrade,omitempty"`
	Successors          []subscriptionSuccessor    `json:"successors,omitempty"`
	Upgrade             *inputUpgradeReview        `json:"upgrade,omitempty"`
	Subscription        *inputSubscription         `json:"subscription,omitempty"`
	Version             int                        `json:"version"`
	ID                  string                     `json:"id"`
	PreparationID       string                     `json:"preparationId"`
	Revision            string                     `json:"revision"`
	RecipeVersion       int                        `json:"recipeVersion"`
	Recipe              inputRecipe                `json:"recipe"`
	Name                string                     `json:"name"`
	Fingerprint         string                     `json:"fingerprint"`
	Release             json.RawMessage            `json:"release,omitempty"`
	ReleaseID           string                     `json:"releaseId"`
	JobID               string                     `json:"jobId,omitempty"`
	CreatedAt           string                     `json:"createdAt"`
	ConfirmedAt         string                     `json:"confirmedAt,omitempty"`
	State               string                     `json:"state"`
	Problem             string                     `json:"problem,omitempty"`
	Rows                []inputEvaluationRow       `json:"rows"`
	RequireTested       bool                       `json:"requireTested"`
	ProjectReviewed     bool                       `json:"projectReviewed"`
}

func (s *Server) inputEvaluationRoot(create bool) (*os.Root, error) {
	root, made, err := openOwnFolder(s.root, strings.Split(inputEvaluationsDir, "/"), create, "input evaluations")
	if err == nil && made {
		err = writePrivateData(root, ".gitignore", []byte("*\n"))
		if err != nil {
			root.Close()
		}
	}
	return root, err
}
func openInputEvaluation(root *os.Root, id string, create bool) (*os.Root, error) {
	if !inputEvaluationID.MatchString(id) {
		return nil, errors.New("Invalid evaluation ID.")
	}
	r, _, e := openOwnFolder(root, []string{id}, create, "input evaluation")
	return r, e
}
func (s *Server) inputRunner(ctx context.Context, path string, body any, key string) (json.RawMessage, error) {
	if s.jobs == nil {
		return nil, &inputRunnerError{Status: 0, Message: "The local runner is unavailable."}
	}
	endpoint, token, e := s.jobs.endpoint()
	if e != nil {
		return nil, &inputRunnerError{Status: 0, Message: "The local runner is unavailable."}
	}
	var raw []byte
	if body != nil {
		raw, e = json.Marshal(body)
		if e != nil {
			return nil, e
		}
		if len(raw) > 2<<20 {
			return nil, errors.New("This record exceeds the run request limit.")
		}
	}
	method := "GET"
	if body != nil {
		method = "POST"
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint+"/v1/"+path, bytes.NewReader(raw))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	client := http.Client{Timeout: 130 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, e := client.Do(req)
	if e != nil {
		return nil, &inputRunnerError{Status: 0, Message: "The runner did not respond. Accepted work will be reconciled with the same submission key."}
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, int64(runnerExportLimit)+1))
	if e != nil || len(data) > runnerExportLimit {
		return nil, &inputRunnerError{Status: 0, Message: "The runner response could not be read. Accepted work will be reconciled."}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Error struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		json.Unmarshal(data, &failure)
		if failure.Error.Message == "" {
			failure.Error.Message = "The runner refused this request."
		}
		return nil, &inputRunnerError{Status: resp.StatusCode, Message: failure.Error.Message, Code: failure.Error.Code}
	}
	if !json.Valid(data) {
		return nil, errors.New("The runner response is invalid.")
	}
	if path == "previews" {
		if e = s.retainRunReferences(data, raw); e != nil {
			return nil, errors.New("Release verification references could not be saved. Review again before evaluating.")
		}
	}
	return data, nil
}

type inputRunnerError struct {
	Status  int
	Message string
	Code    string
}

func (e *inputRunnerError) Error() string { return e.Message }

// Read saved authoring cases, never old test reports or caller-asserted results.
func (s *Server) inputReleaseSnapshot(q *inputQualification) (string, map[string]any, bool, error) {
	fail := func(e error) (string, map[string]any, bool, error) { return "", nil, false, e }
	snap, e := s.readSnapshot()
	if e != nil {
		return fail(e)
	}
	var config struct {
		Packs           map[string]briefDeclaration `json:"packs"`
		RequireReviewed bool                        `json:"requireReviewed"`
	}
	if json.Unmarshal(snap.config, &config) != nil {
		return fail(errors.New("Project settings could not be read."))
	}
	declared, ok := config.Packs[q.Key]
	if !ok {
		return fail(errors.New("The selected pack is no longer in this project."))
	}
	matched := false
	for _, d := range snap.docs {
		if d.entry.Kind == "pack" && d.entry.ID == q.Key && string(d.data) == q.Pack {
			matched = true
		}
	}
	if !matched {
		return fail(errors.New("The pack changed. Update its recipe snapshot before evaluating."))
	}
	store, e := s.openChatData()
	if e != nil {
		return fail(e)
	}
	defer store.root.Close()
	name, e := resolveConversationName(store.root, s.conversationIdentity())
	if e != nil {
		return fail(e)
	}
	tests, e := s.readWorkspaceRecord(store.root, strings.Replace(name, "conversations-", "pack-tests-", 1), false)
	if e != nil {
		return fail(e)
	}
	var saved struct {
		Suites map[string]struct {
			Revision int `json:"revision"`
			Cases    []struct {
				ID        string         `json:"id"`
				Name      string         `json:"name"`
				Row       map[string]any `json:"row"`
				Rationale string         `json:"rationale"`
				Origin    string         `json:"origin"`
			} `json:"cases"`
			Deleted   []string `json:"deleted"`
			Recovered []string `json:"recovered"`
		} `json:"suites"`
	}
	decoder := json.NewDecoder(bytes.NewReader(tests.Content))
	decoder.UseNumber()
	if decoder.Decode(&saved) != nil {
		return fail(errors.New("Saved tests could not be read."))
	}
	suite := saved.Suites[q.Key]
	seen := map[string]bool{}
	for _, id := range suite.Deleted {
		seen[id] = true
	}
	cases := []map[string]any{}
	names := map[string]string{}
	exploratory := 0
	for _, c := range suite.Cases {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		if c.Row == nil {
			return fail(errors.New("Saved test case is invalid."))
		}
		_, disposition := c.Row["expectedDisposition"]
		errorClass, _ := c.Row["expectedErrorClass"].(string)
		if !disposition && errorClass == "" {
			exploratory++
			continue
		}
		c.Row["id"] = c.ID
		if c.Rationale != "" {
			c.Row["focus"] = c.Rationale
		}
		if c.Origin == "ai" {
			c.Row["origin"] = "ai"
		} else if _, ok := c.Row["origin"]; !ok {
			c.Row["origin"] = c.Origin
		}
		cases = append(cases, c.Row)
		names[c.ID] = c.Name
	}
	var configured []byte
	if declared.Matrix != "" {
		configured, e = s.readReviewFile(declared.Matrix)
		if e != nil {
			return fail(e)
		}
		recovered := false
		for _, v := range suite.Recovered {
			if v == "matrix:"+declared.Matrix {
				recovered = true
			}
		}
		if !recovered {
			var matrix struct {
				Cases []map[string]any `json:"cases"`
			}
			decoder := json.NewDecoder(bytes.NewReader(configured))
			decoder.UseNumber()
			if decoder.Decode(&matrix) != nil || len(matrix.Cases) > 256 {
				return fail(errors.New("The configured test matrix could not be read."))
			}
			for _, c := range matrix.Cases {
				id, ok := c["id"].(string)
				if !ok || id == "" {
					return fail(errors.New("A configured test case has no identity."))
				}
				if seen[id] {
					continue
				}
				seen[id] = true
				cases = append(cases, c)
				names[id] = id
			}
		}
	}
	request := map[string]any{"pack": q.Pack}
	if len(cases) > 0 {
		matrix, _ := json.Marshal(map[string]any{"matrixVersion": "3", "cases": cases})
		request["matrix"] = string(matrix)
		request["testSource"] = map[string]any{"packKey": q.Key, "suiteRevision": suite.Revision, "caseNames": names, "exploratoryCount": exploratory}
	}
	identity, _ := json.Marshal([]any{s.projectDir, snap.set, string(snap.lock), tests.SHA256, string(configured), s.cfg.requireTestedReleases()})
	return digestOf(identity), request, config.RequireReviewed, nil
}

func (s *Server) handleInputEvaluation(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("preparation")
	batchID := r.PathValue("evaluation")
	if !inputPreparationID.MatchString(id) || r.URL.RawQuery != "" || batchID != "" && !inputEvaluationID.MatchString(batchID) {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid input evaluation request.")
		return
	}
	fail := func(code int, e error) { writeJSONCoded(w, code, CodeBadRequest, e.Error()) }
	if r.Method == "GET" {
		root, e := s.inputEvaluationRoot(false)
		if errors.Is(e, os.ErrNotExist) {
			writeJSON(w, 200, map[string]any{"items": []any{}})
			return
		}
		if e != nil {
			fail(409, errors.New("Evaluation history could not be opened."))
			return
		}
		defer root.Close()
		dir, e := root.Open(".")
		if e != nil {
			fail(409, e)
			return
		}
		defer dir.Close()
		entries, e := dir.ReadDir(-1)
		if e != nil {
			fail(409, e)
			return
		}
		items := []inputEvaluation{}
		for _, entry := range entries {
			if !inputEvaluationID.MatchString(entry.Name()) {
				continue
			}
			br, e := openInputEvaluation(root, entry.Name(), false)
			if e != nil {
				fail(409, e)
				return
			}
			snapshot, e := s.evaluationJournal(br, entry.Name()).Read(r.Context())
			b := snapshot.Document
			br.Close()
			if e != nil {
				fail(409, errors.New("An evaluation record could not be read."))
				return
			}
			if b.PreparationID != id || b.ConfirmedAt == "" {
				continue
			}
			b.Release = nil
			b.Recipe = inputRecipe{}
			for i := range b.Rows {
				b.Rows[i].Input = nil
			}
			items = append(items, b)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ConfirmedAt > items[j].ConfirmedAt })
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	view, e := s.preparationRepository().Read(r.Context())
	if e != nil {
		fail(409, errors.New("Dialects could not be read. Try again."))
		return
	}
	defer view.Close()
	saved, e := view.Get(id)
	if e != nil {
		fail(409, errors.New("Dialects could not be read."))
		return
	}
	// A repeat confirmation is allowed after the recipe changes: it returns the
	// existing batch only, never confirms another selection or creates more work.
	if batchID != "" {
		s.confirmInputEvaluation(w, r, view, saved, batchID)
		return
	}
	if r.Header.Get("If-Match") != saved.SHA256 {
		writeJSONCoded(w, 409, CodeStale, "Dialects changed. Reload and review the selection again.")
		return
	}
	if strings.HasSuffix(r.URL.Path, "/conversion-preview") {
		var request struct {
			Qualification *inputQualification `json:"qualification"`
		}
		if inputRequest(r, &request) != nil || request.Qualification == nil {
			fail(400, errors.New("Choose a pack and conversions to preview."))
			return
		}
		if e = validateInputQualification(request.Qualification); e != nil {
			fail(422, e)
			return
		}
		doc := saved.Document
		doc.Recipe.Qualification = request.Qualification
		rows := []inputPreparedRow{}
		for _, record := range doc.Records {
			row, e := prepareInputRecord(doc, record)
			if e != nil {
				fail(422, e)
				return
			}
			row.Input = nil
			rows = append(rows, row)
		}
		writeJSON(w, 200, map[string]any{"rows": rows})
		return
	}
	var request struct {
		RecordIDs      []string `json:"recordIds"`
		SubscriptionID string   `json:"subscriptionId,omitempty"`
	}
	if inputRequest(r, &request) != nil || request.SubscriptionID != "" && strings.HasSuffix(r.URL.Path, "/prepare") {
		fail(400, errors.New("Choose the records to review."))
		return
	}
	rows, e := prepareInputSelection(saved.Document, request.RecordIDs)
	if e != nil {
		fail(422, e)
		return
	}
	// Local snapshot acquisition calls no model or upstream data provider.
	if e = s.prepareReviewedInputs(r.Context(), saved.Document, rows); e != nil {
		fail(422, e)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/prepare") {
		writeJSON(w, 200, map[string]any{"rows": rows})
		return
	}
	for _, row := range rows {
		if len(row.Issues) > 0 {
			fail(422, errors.New("Resolve conversion issues or change the selection before evaluating."))
			return
		}
	}
	// Only the review operation performs a rehearsal; /prepare remains read-only.
	fingerprint, previewRequest, projectReviewed, e := s.inputReleaseSnapshot(saved.Document.Recipe.Qualification)
	if e != nil {
		fail(409, e)
		return
	}
	previewRequest["input"] = rows[0].Input
	release, e := s.inputRunner(r.Context(), "previews", previewRequest, "")
	if e != nil {
		fail(422, e)
		return
	}
	after, _, _, e := s.inputReleaseSnapshot(saved.Document.Recipe.Qualification)
	if e != nil || after != fingerprint {
		writeJSONCoded(w, 409, CodeStale, "The project or saved tests changed. Review evaluation again.")
		return
	}
	var releaseData reviewedInputRelease
	if json.Unmarshal(release, &releaseData) != nil || !referenceReleaseID.MatchString(releaseData.ID) || releaseData.Pack != saved.Document.Recipe.Qualification.Pack {
		fail(502, errors.New("The release does not match the reviewed pack."))
		return
	}
	if e = verifyPublishedRelease(rows[0].Input, releaseData.InputMapping, releaseData.Dialect); e != nil {
		fail(502, e)
		return
	}
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		fail(500, e)
		return
	}
	b := inputEvaluation{Version: 1, ID: "eval_" + hex.EncodeToString(nonce), PreparationID: id, Revision: saved.SHA256, RecipeVersion: saved.Document.RecipeVersion, Recipe: saved.Document.Recipe, Name: saved.Document.Name, Fingerprint: fingerprint, Release: release, ReleaseID: releaseData.ID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), State: "review", RequireTested: s.cfg.requireTestedReleases(), ProjectReviewed: projectReviewed}
	for _, row := range rows {
		b.Rows = append(b.Rows, inputEvaluationRow{RecordID: row.ID, Input: row.Input, State: "pending"})
	}
	if request.SubscriptionID != "" {
		if e = s.reviewSubscriptionUpgrade(r.Context(), saved, &b, request.SubscriptionID); e != nil {
			fail(409, e)
			return
		}
	}
	er, e := s.inputEvaluationRoot(true)
	if e != nil {
		fail(409, e)
		return
	}
	defer er.Close()
	if e = s.createEvaluationReview(r.Context(), er, b); e != nil {
		fail(409, e)
		return
	}
	writeJSON(w, 201, b)
}
func (s *Server) confirmInputEvaluation(w http.ResponseWriter, r *http.Request, reader inputPreparationReader, saved inputPreparationReply, id string) {
	fail := func(e error) { writeJSONCoded(w, 409, CodeStale, e.Error()) }
	var request struct {
		Reviewed     bool                `json:"reviewed"`
		Upgrade      *inputUpgradeReview `json:"upgrade,omitempty"`
		Subscription *inputSubscription  `json:"subscription,omitempty"`
		Enabled      *bool               `json:"enabled,omitempty"`
	}
	if inputRequest(r, &request) != nil || !request.Reviewed {
		writeJSONCoded(w, 400, CodeBadRequest, "Confirm the reviewed evaluation before starting it.")
		return
	}
	upgrading := strings.HasSuffix(r.URL.Path, "/upgrade")
	if upgrading != (request.Upgrade != nil) || upgrading && (request.Subscription != nil || request.Enabled != nil || !evaluationJobID.MatchString(request.Upgrade.JobID) || !referenceReleaseID.MatchString(request.Upgrade.BaseReleaseID) || request.Upgrade.BaseRevision < 1 || len(request.Upgrade.Result) != 0 || request.Upgrade.Phase != "" || request.Upgrade.Problem != "" || len(request.Upgrade.RetryAt) > 0) {
		writeJSONCoded(w, 400, CodeBadRequest, "Choose an existing job revision and review its upgrade.")
		return
	}
	root, e := s.inputEvaluationRoot(false)
	if e != nil {
		fail(errors.New("The evaluation review is unavailable."))
		return
	}
	defer root.Close()
	br, e := openInputEvaluation(root, id, false)
	if e != nil {
		fail(errors.New("The evaluation review is unavailable."))
		return
	}
	defer br.Close()
	lock, e := br.Open(".")
	if e != nil {
		fail(e)
		return
	}
	defer lock.Close()
	if e = waitPrivateDataLock(r.Context(), lock, true); e != nil {
		fail(errors.New("This evaluation is being updated. Try again."))
		return
	}
	snapshot, e := s.evaluationJournal(br, id).Read(r.Context())
	b := snapshot.Document
	checkpoint := evaluationCheckpoint{journal: s.evaluationJournal(br, id), revision: snapshot.SHA256}
	if e != nil || b.PreparationID != saved.Document.ID {
		fail(errors.New("The evaluation review does not belong to these inputs."))
		return
	}
	if b.SubscriptionUpgrade != nil && (b.Subscription == nil || upgrading) || subscriptionReserved(b) {
		fail(errors.New("This evaluation belongs to a pending or completed subscription upgrade."))
		return
	}
	if b.Upgrade != nil && b.SubscriptionUpgrade == nil && !upgrading {
		fail(errors.New("This evaluation is reserved for a reviewed job upgrade."))
		return
	}
	if upgrading {
		if r.Header.Get("If-Match") != b.Revision {
			fail(errors.New("Review the same evaluation revision before upgrading."))
			return
		}
		if b.Upgrade != nil {
			prior := *b.Upgrade
			prior.Result = nil
			if !sameEvaluationValue(prior, *request.Upgrade) {
				fail(errors.New("This review belongs to a different job upgrade."))
				return
			}
			if len(b.Upgrade.Result) != 0 {
				writeJSON(w, 200, b.Upgrade.Result)
				return
			}
			// Resolve an uncertain write before validating current mutable inputs.
			// A read can return an older receipt, but can never admit another upgrade.
			q := url.Values{"baseRevision": {strconv.Itoa(prior.BaseRevision)}, "baseReleaseId": {prior.BaseReleaseID}, "releaseId": {b.ReleaseID}}
			raw, err := s.inputRunner(r.Context(), "jobs/"+prior.JobID+"/upgrade-receipt?"+q.Encode(), nil, "")
			if err == nil {
				if err = retainInputUpgrade(&b, &checkpoint, raw); err != nil {
					fail(err)
					return
				}
				writeJSON(w, 200, json.RawMessage(raw))
				return
			}
			var refused *inputRunnerError
			if !errors.As(err, &refused) || refused.Status != 404 {
				fail(err)
				return
			}
		}
	}
	if strings.HasSuffix(r.URL.Path, "/subscription-state") && (b.ConfirmedAt == "" || b.Subscription == nil) {
		fail(errors.New("Only a saved subscription can be enabled or paused."))
		return
	}
	if b.ConfirmedAt != "" && request.Subscription != nil && (b.Subscription == nil || b.Subscription.Mode != request.Subscription.Mode || b.Subscription.IncludeExisting != request.Subscription.IncludeExisting) {
		fail(errors.New("This review already saved a different configuration. Review a new subscription."))
		return
	}
	if upgrading && b.ConfirmedAt != "" {
		fail(errors.New("This evaluation already started execution. Review a new upgrade sample."))
		return
	}
	if b.ConfirmedAt != "" {
		if b.Subscription != nil && strings.HasSuffix(r.URL.Path, "/subscription-state") {
			if request.Enabled == nil {
				fail(errors.New("Choose whether to enable or pause this subscription."))
				return
			}
			if *request.Enabled {
				fresh, e := s.subscriptionDocument(reader, b)
				if e != nil {
					fail(e)
					return
				}
				initializeSubscription(b.Subscription, fresh.Document)
			}
			b.Subscription.Enabled = *request.Enabled
			b.Subscription.StatePending = true
			b.Subscription.Failures = 0
			b.Subscription.RetryAt = ""
			b.State = "queued"
			b.Problem = ""
			if e = checkpoint.save(r.Context(), b); e != nil {
				fail(e)
				return
			}
		}
		if strings.HasSuffix(r.URL.Path, "/resume") && b.State == "blocked" {
			b.State = "queued"
			b.Problem = ""
			if e = checkpoint.save(r.Context(), b); e != nil {
				fail(e)
				return
			}
		}
		writeJSON(w, 200, b)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/resume") {
		fail(errors.New("Only confirmed evaluations can resume."))
		return
	}
	if b.Revision != saved.SHA256 || r.Header.Get("If-Match") != saved.SHA256 {
		fail(errors.New("Dialects changed. Reload and review the selection again."))
		return
	}
	unlock, e := s.lockProject(r.Context(), projectLockWait)
	if e != nil {
		fail(e)
		return
	}
	defer unlock()
	s.writes.Lock()
	defer s.writes.Unlock()
	privateLock, e := s.privateDataLockContext(r.Context(), false)
	if e != nil {
		fail(errors.New("Saved tests are being updated. Try again."))
		return
	}
	defer privateLock.Close()
	fingerprint, _, _, e := s.inputReleaseSnapshot(saved.Document.Recipe.Qualification)
	if e != nil || fingerprint != b.Fingerprint {
		fail(errors.New("The project, pack, or saved tests changed. Review evaluation again."))
		return
	}
	var release struct {
		Tests string `json:"tests"`
	}
	if json.Unmarshal(b.Release, &release) != nil || release.Tests != "passed" && release.Tests != "not-run" {
		fail(errors.New("Correct the saved tests and review evaluation again."))
		return
	}
	if b.RequireTested && release.Tests != "passed" {
		fail(errors.New("Saved tests are required. Add tests for this pack and review evaluation again."))
		return
	}
	if upgrading {
		// The preparation read view and project locks still hold the exact
		// accepted records, publication and saved tests checked above. Runner
		// owns the job CAS, immutable revision receipt and paused triggers.
		if b.Upgrade == nil {
			health, err := s.inputRunner(r.Context(), "status", nil, "")
			var capabilities struct {
				Capabilities []string `json:"capabilities"`
			}
			if err != nil || json.Unmarshal(health, &capabilities) != nil || !contains(capabilities.Capabilities, "job-upgrade-receipts") {
				fail(errors.New("Update Runner before upgrading a job from reviewed records."))
				return
			}
			b.Upgrade = request.Upgrade
			if err := checkpoint.save(r.Context(), b); err != nil {
				fail(err)
				return
			}
		}
		requestBody := map[string]any{"releaseId": b.ReleaseID, "baseReleaseId": request.Upgrade.BaseReleaseID, "baseRevision": request.Upgrade.BaseRevision, "reviewed": true}
		raw, err := s.inputRunner(r.Context(), "jobs/"+request.Upgrade.JobID+"/upgrade", requestBody, "")
		if err != nil {
			fail(err)
			return
		}
		if err = retainInputUpgrade(&b, &checkpoint, raw); err != nil {
			fail(err)
			return
		}
		// Do not confirm the evaluation batch: doing so would enqueue another
		// job or record runs. Its retained review is the candidate's provenance.
		writeJSON(w, 200, json.RawMessage(raw))
		return
	}
	if request.Subscription != nil {
		if request.Subscription.Mode != "new" && request.Subscription.Mode != "changed" {
			fail(errors.New("Choose new records or new and changed records."))
			return
		}
		b.Subscription = &inputSubscription{Mode: request.Subscription.Mode, IncludeExisting: request.Subscription.IncludeExisting, SourceClass: saved.Document.Records[0].Class}
		// The preview selection already enforces a single source class.
		for _, record := range saved.Document.Records {
			if record.ID == b.Rows[0].RecordID {
				b.Subscription.SourceClass = record.Class
				break
			}
		}
		b.Rows = []inputEvaluationRow{}
	}
	b.ConfirmedAt = time.Now().UTC().Format(time.RFC3339Nano)
	b.State = "queued"
	if e = checkpoint.save(r.Context(), b); e != nil {
		fail(e)
		return
	}
	writeJSON(w, 202, b)
}

// The result is an immutable completion receipt, separate from evaluation
// confirmation: workers must not create a second job or submit these rows.
func retainInputUpgrade(b *inputEvaluation, checkpoint *evaluationCheckpoint, raw json.RawMessage) error {
	var job struct {
		ID        string `json:"id"`
		ReleaseID string `json:"releaseId"`
		Revision  int    `json:"revision"`
	}
	if b.Upgrade == nil || json.Unmarshal(raw, &job) != nil || job.ID != b.Upgrade.JobID || job.ReleaseID != b.ReleaseID || job.Revision != b.Upgrade.BaseRevision+1 {
		return errors.New("The job response does not match the reviewed upgrade.")
	}
	b.Upgrade.Result = raw
	return checkpoint.retain(*b)
}

func (s *Server) startInputEvaluations() {
	if s.jobs == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.inputEvaluationCancel = cancel
	s.inputEvaluationDone = make(chan struct{})
	go func() {
		defer close(s.inputEvaluationDone)
		select {
		case <-s.jobs.started:
		case <-ctx.Done():
			return
		}
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.resumeInputEvaluations(ctx)
			}
		}
	}()
}
func (s *Server) resumeInputEvaluations(ctx context.Context) {
	if !s.location.use.TryRLock() {
		return
	}
	defer s.location.use.RUnlock()
	if s.projectHasMoved() {
		return
	}
	root, e := s.inputEvaluationRoot(false)
	if e != nil {
		return
	}
	defer root.Close()
	dir, e := root.Open(".")
	if e != nil {
		return
	}
	entries, e := dir.ReadDir(-1)
	dir.Close()
	if e != nil {
		return
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if !inputEvaluationID.MatchString(entry.Name()) {
			continue
		}
		br, e := openInputEvaluation(root, entry.Name(), false)
		if e != nil {
			continue
		}
		func() {
			defer br.Close()
			lock, e := br.Open(".")
			if e != nil {
				return
			}
			defer lock.Close()
			if lockPrivateData(lock, true) != nil {
				return
			}
			snapshot, e := s.evaluationJournal(br, entry.Name()).Read(ctx)
			b := snapshot.Document
			checkpoint := evaluationCheckpoint{journal: s.evaluationJournal(br, entry.Name()), revision: snapshot.SHA256}
			if e != nil || b.ConfirmedAt == "" || b.State == "completed" || b.State == "partial" || b.State == "blocked" {
				return
			}
			if b.Subscription != nil {
				if subscriptionReserved(b) {
					return
				}
				s.advanceInputSubscription(ctx, &checkpoint, &b)
			} else {
				s.advanceInputEvaluation(ctx, &checkpoint, &b)
			}
		}()
	}
}
func (s *Server) advanceInputEvaluation(ctx context.Context, checkpoint *evaluationCheckpoint, b *inputEvaluation) {
	if checkpoint.failure != nil {
		return
	}
	save := func() bool {
		if e := checkpoint.retain(*b); e != nil {
			s.log.Printf("desk: input evaluation progress could not be saved")
			return false
		}
		return true
	}
	problem := func(err error) {
		b.Problem = err.Error()
		var refusal *inputRunnerError
		if errors.As(err, &refusal) && refusal.Status >= 400 && refusal.Status < 500 && refusal.Status != 429 {
			b.State = "blocked"
		}
		save()
	}
	if e := s.ensureEvaluationJob(ctx, checkpoint, b); e != nil {
		if checkpoint.failure == nil {
			problem(e)
		}
		return
	}
	b.State = "running"
	b.Problem = ""
	budget := 20
	for i := range b.Rows {
		if ctx.Err() != nil {
			return
		}
		row := &b.Rows[i]
		if row.State == "completed" || row.State == "failed" || row.State == "interrupted" {
			continue
		}
		if budget == 0 {
			break
		}
		budget--
		var raw json.RawMessage
		var e error
		if row.RunID == "" {
			raw, e = s.inputRunner(ctx, "jobs/"+b.JobID+"/runs", row.Input, b.ID+":"+row.RecordID)
		} else {
			raw, e = s.inputRunner(ctx, "runs/"+row.RunID, nil, "")
		}
		if e != nil {
			problem(e)
			return
		}
		var run struct {
			ID      string          `json:"id"`
			JobID   string          `json:"jobId"`
			State   string          `json:"state"`
			Result  json.RawMessage `json:"result"`
			Signing json.RawMessage `json:"signing"`
			Problem string          `json:"problem"`
		}
		if json.Unmarshal(raw, &run) != nil || !regexp.MustCompile(`^run_[a-f0-9]{32}$`).MatchString(run.ID) || run.JobID != b.JobID {
			problem(errors.New("The run response is invalid."))
			return
		}
		switch run.State {
		case "queued", "running", "completed", "failed", "interrupted":
		default:
			problem(errors.New("The run state is invalid."))
			return
		}
		// The ledger is an index, not a second copy of every potentially large
		// trace. Full decisions and receipts remain in Runner's retained record.
		var decision struct {
			Disposition   json.RawMessage `json:"disposition,omitempty"`
			HandoffTarget json.RawMessage `json:"handoffTarget,omitempty"`
		}
		var summary json.RawMessage
		if len(run.Result) > 0 && json.Unmarshal(run.Result, &decision) == nil {
			summary, _ = json.Marshal(decision)
			if len(summary) > 16<<10 {
				summary = nil
			}
		}
		if len(run.Problem) > 4096 {
			run.Problem = run.Problem[:4096]
		}
		row.RunID, row.State, row.Result, row.Signing, row.Problem = run.ID, run.State, summary, run.Signing, run.Problem
		if !save() {
			return
		}
	}
	terminal, failed := 0, 0
	for _, row := range b.Rows {
		if row.State == "completed" || row.State == "failed" || row.State == "interrupted" {
			terminal++
		}
		if row.State == "failed" || row.State == "interrupted" {
			failed++
		}
	}
	if terminal == len(b.Rows) {
		b.State = "completed"
		if failed > 0 {
			b.State = "partial"
		}
	}
	save()
}

// Kept as a small formatter for operator logs/tests without leaking source values.
func (b inputEvaluation) String() string {
	return fmt.Sprintf("%s: %s (%d records)", b.ID, b.State, len(b.Rows))
}
