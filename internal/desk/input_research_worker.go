package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/aicontext"
	"os"
	"strings"
	"time"
)

func (s *Server) startInputResearch() {
	ctx, cancel := context.WithCancel(context.Background())
	s.inputResearchCancel = cancel
	s.inputResearchDone = make(chan struct{})
	go func() {
		defer close(s.inputResearchDone)
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				s.scanInputResearch(ctx)
			}
		}
	}()
}
func (s *Server) scanInputResearch(ctx context.Context) {
	if !s.location.use.TryRLock() {
		return
	}
	defer s.location.use.RUnlock()
	if s.projectHasMoved() {
		return
	}
	root, e := s.inputResearchRoot(false)
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
		if !inputResearchID.MatchString(entry.Name()) {
			continue
		}
		rr, e := openInputResearch(root, entry.Name(), false)
		if e != nil {
			continue
		}
		func() {
			defer rr.Close()
			// Separate execution lease: progress/Stop use the record-directory lock and
			// remain available while this process waits on a provider.
			lease, e := rr.OpenFile("worker.lock", os.O_CREATE|os.O_RDWR|openNoFollow|openNonBlocking, 0600)
			if e != nil {
				return
			}
			defer lease.Close()
			if lockPrivateData(lease, true) != nil {
				return
			}
			run, e := s.researchJournal(rr, entry.Name()).Read(ctx)
			if e != nil {
				return
			}
			if run.State == "running" {
				run, e = s.researchJournal(rr, entry.Name()).Update(ctx, func(r *inputResearchRun) error { recoverInputResearch(r); return nil })
				if e != nil {
					return
				}
			}
			if len(run.Records) > 0 {
				if e = s.publishResearchRecords(run); e != nil {
					return
				}
			}
			if run.State != "queued" || run.StopRequested {
				return
			}
			run, e = s.researchJournal(rr, entry.Name()).Update(ctx, func(r *inputResearchRun) error {
				if r.State != "queued" || r.StopRequested {
					return errors.New("Research is no longer queued.")
				}
				r.State = "running"
				return nil
			})
			if e != nil {
				return
			}
			s.executeInputResearch(ctx, rr, run)
		}()
	}
}
func (s *Server) executeInputResearch(parent context.Context, root *os.Root, run inputResearchRun) {
	if run.Portable != nil {
		s.executePortableResearch(parent, s.researchRunStore(root, run.ID), run)
		return
	}
	executeInputResearch(parent, s.researchRunStore(root, run.ID), run, inputResearchOperations{
		validateSearch: s.validateResearchSearchConnection, validateAI: s.validateResearchConnection,
		model: s.inputResearchModel, source: s.researchSource(root), publish: s.publishResearchRecords,
	})
}
func executeInputResearch(parent context.Context, root researchRunStore, run inputResearchRun, ops inputResearchOperations) {
	// An overall time bound complements per-call and request-count bounds.
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				current, e := root.read()
				if e == nil && current.StopRequested {
					cancel()
					return
				}
			}
		}
	}()
	finish := func(state string, err error) {
		updated, e := root.change(func(r *inputResearchRun) error {
			if r.StopRequested {
				r.State = "stopped"
			} else if ctx.Err() != nil {
				r.State = "interrupted"
			} else {
				r.State = state
				if state == "completed" && (len(r.Records) < r.Recipe.Limits.Records || len(researchSourceFailures(*r)) > 0 || researchHasUnresolvedFields(*r) || researchNeedsCorrection(*r)) {
					r.State = "completed-with-gaps"
					r.Problem = "Review missing fields and source coverage."
					if len(r.Records) < r.Recipe.Limits.Records {
						r.Problem = fmt.Sprintf("Saved %d of up to %d records. The available evidence did not support more records within this run's limits.", len(r.Records), r.Recipe.Limits.Records)
					}
				}
			}
			cancelResearchRetry(r, "Research stopped before the scheduled retry. No further request was made.")
			if err != nil {
				r.Problem = err.Error()
			}
			if !r.StopRequested && ctx.Err() == nil && (state == "completed" || state == "partial") {
				pauseResearchForBudget(r)
			}
			return nil
		})
		if e == nil {
			if err := ops.publish(updated); err != nil {
				_, _ = root.change(func(r *inputResearchRun) error {
					if r.ContractVersion >= 2 && r.PublicationResumeState == "" {
						r.PublicationResumeState = r.State
						if r.State == "running" || r.State == "blocked" || r.State == "queued" {
							r.PublicationResumeState = "partial"
						}
					}
					if r.State != "paused" && r.State != "queued" {
						r.State = "blocked"
					}
					r.Problem = "Results are retained, but Dialects could not be updated: " + err.Error()
					if r.ContractVersion < 2 {
						r.Problem = "Records were saved with research, but could not be added to Dialects. Reload to retry the local save."
					}
					return nil
				})
			}
		}
	}
	if err := validateResearchSearchForPhase(ctx, run, ops); err != nil {
		finish("blocked", err)
		return
	}
	reject := func(step int, reason error) bool {
		updated, err := rejectResearchAction(root, step, reason)
		if err != nil {
			finish("blocked", err)
			return false
		}
		if consecutiveResearchReadRejections(updated) >= 3 {
			finish("partial", fmt.Errorf("The AI could not produce a usable action after two corrections. Saved sources and records remain available. Last issue: %s", reason))
			return false
		}
		return true
	}
	for step := run.Step; step < researchApprovedLimits(run).ModelCalls; step++ {
		if ctx.Err() != nil {
			finish("interrupted", errors.New("Research stopped. Saved records are available; outstanding requests were not repeated."))
			return
		}
		run, e := root.read()
		if e != nil {
			return
		}
		if researchWorkComplete(run) {
			finish("completed", nil)
			return
		}
		run, e = root.change(func(r *inputResearchRun) error {
			if r.Phase != "finalize" && (researchApprovedLimits(*r).ModelCalls-step <= 2 && step > 0 || researchRequestBudget(r, "page") != nil && researchRequestBudget(r, "search") != nil) {
				r.Phase = "finalize"
				if researchApprovedLimits(*r).ModelCalls-step <= 2 && step > 0 {
					r.PhaseReason = fmt.Sprintf("Source discovery stopped after %d of %d AI requests to reserve the remaining requests for saving records.", step, researchApprovedLimits(*r).ModelCalls)
				} else {
					r.PhaseReason = "The saved search and page-read limits were reached. Only retained evidence can be used."
				}
			}
			return nil
		})
		if e != nil {
			return
		}
		if e = validateResearchSearchForPhase(ctx, run, ops); e != nil {
			finish("blocked", e)
			return
		}
		if e = ops.validateAI(ctx, run.Recipe.AI); e != nil {
			finish("blocked", e)
			return
		}
		if delegatedResearch(run) && run.Reprocess {
			err := runResearchSelectedTasks(ctx, root, ops)
			if err != nil {
				finish("interrupted", err)
			} else {
				finish("completed", nil)
			}
			return
		}
		if e = readResearchCandidates(ctx, root, ops); e != nil {
			finish("interrupted", e)
			return
		}
		run, e = root.read()
		if e != nil {
			return
		}
		// Children consumed the same model allowance while the coordinator yielded.
		step = run.Step
		if step >= researchApprovedLimits(run).ModelCalls {
			break
		}
		if delegatedResearch(run) && researchApprovedLimits(run).ModelCalls-step <= 2 && run.Phase != "finalize" {
			run, e = root.change(func(r *inputResearchRun) error {
				r.Phase = "finalize"
				r.PhaseReason = "The remaining AI requests are reserved for saving retained findings."
				return nil
			})
			if e != nil {
				finish("blocked", e)
				return
			}
		}
		prompt, e := researchPrompt(run)
		if e != nil {
			finish("blocked", e)
			return
		}
		modelArgs, _ := json.Marshal(map[string]any{"step": step, "prompt": prompt, "ai": run.Recipe.AI})
		actionRaw, e := researchAttempt(ctx, root, "model", modelArgs, func(call context.Context, _ inputResearchRun, attempt inputResearchAttempt) (json.RawMessage, error) {
			return ops.model(call, run.Recipe.AI, prompt, func(waiting bool) error {
				_, err := root.change(func(r *inputResearchRun) error {
					if call.Err() != nil {
						return call.Err()
					}
					if r.StopRequested || r.State != "running" {
						return errors.New("Research stopped before the AI request started.")
					}
					return researchModelAvailability(r, attempt.ID, waiting)
				})
				return err
			})
		})
		if e != nil {
			finish("interrupted", e)
			return
		}
		_, e = root.change(func(r *inputResearchRun) error { r.Step = step + 1; return nil })
		if e != nil {
			return
		}
		action, e := decodeResearchAction(actionRaw)
		if e != nil {
			if reject(step, e) {
				continue
			}
			return
		}
		if action.Tool == "read_context" {
			if _, err := researchContextRead(run, action.Arguments); err != nil {
				if reject(step, err) {
					continue
				}
				return
			}
			continue
		}
		if action.Tool == "finish" {
			var result struct {
				Reason string `json:"reason,omitempty"`
			}
			if decodeDataJSON(action.Arguments, &result) != nil || len(result.Reason) > 2000 {
				if reject(step, errors.New("Finish requires a short reason explaining missing records or why the current page cannot support a record.")) {
					continue
				}
				return
			}
			if run.Phase == "save" {
				if strings.TrimSpace(result.Reason) == "" {
					if reject(step, errors.New("Save a candidate from the retained page, or finish with a reason explaining why it cannot support a record.")) {
						continue
					}
					return
				}
				_, e = root.change(func(r *inputResearchRun) error {
					r.Phase = ""
					r.PhaseReason = ""
					reviewResearchCandidates(r)
					return nil
				})
				if e != nil {
					return
				}
				continue
			}
			reason := researchFinishProblem(run, result.Reason)
			finish("completed", reason)
			return
		}
		if action.Tool == "delegate" {
			if !delegatedResearch(run) || run.Reprocess || run.Phase == "save" || run.Phase == "finalize" {
				if reject(step, errors.New("Delegation is unavailable for this research phase. Use retained evidence or finish.")) {
					continue
				}
				return
			}
			if err := queueResearchDiscoveryTasks(root, action.Arguments); err != nil {
				if reject(step, err) {
					continue
				}
				return
			}
			if err := runQueuedResearchTasks(ctx, root, ops); err != nil {
				finish("interrupted", err)
				return
			}
			latest, err := root.read()
			if err != nil {
				finish("blocked", err)
				return
			}
			step = latest.Step - 1
			continue
		}
		if action.Tool == "records" {
			run, e = root.change(func(r *inputResearchRun) error {
				if r.StopRequested {
					return errors.New("Research was stopped.")
				}
				before, _ := json.Marshal(r.Records)
				if err := applyResearchRecords(r, action.Arguments); err != nil {
					return &researchActionError{err}
				}
				after, _ := json.Marshal(r.Records)
				if r.ContractVersion < 2 && string(before) == string(after) {
					return &researchActionError{errors.New("These records are already saved unchanged. Finish with a reason or provide new supported information.")}
				}
				if r.ContractVersion >= 2 {
					recordResearchResults(r)
				}
				reviewResearchCandidates(r)
				if r.Phase == "save" && !researchNeedsCorrection(*r) {
					r.Phase = ""
					r.PhaseReason = ""
				}
				return nil
			})
			if e != nil {
				var invalid *researchActionError
				if errors.As(e, &invalid) {
					if reject(step, e) {
						continue
					}
					return
				}
				finish("blocked", e)
				return
			}
			if e = ops.publish(run); e != nil {
				finish("blocked", fmt.Errorf("Results are retained, but Dialects could not be updated: %w", e))
				return
			}
			if researchNeedsCorrection(run) && consecutiveResearchReadRejections(run) >= 3 {
				finish("partial", errors.New("Some records still need correction after two correction attempts. Valid results have been kept."))
				return
			}
		} else {
			if run.Phase == "save" || run.Phase == "finalize" {
				if reject(step, errors.New("This step is reserved for saving from retained evidence. Submit records or finish with a reason; no source request was sent.")) {
					continue
				}
				return
			}
			if step+1 >= researchApprovedLimits(run).ModelCalls {
				finish("partial", errors.New("The approved model request limit leaves no room to process another source. Saved records are available for review."))
				return
			}
			if action.Tool == "candidates" {
				_, err := root.change(func(r *inputResearchRun) error {
					if r.StopRequested {
						return errors.New("Research was stopped.")
					}
					return queueResearchCandidates(r, action.Arguments)
				})
				if err != nil {
					if !reject(step, err) {
						return
					}
				}
				continue
			}
			kind, args, e := researchSourceArguments(run, action)
			if e != nil {
				if reject(step, e) {
					continue
				}
				return
			}
			if researchRequestBudget(&run, kind) != nil {
				_, err := root.change(func(r *inputResearchRun) error {
					r.Phase = "finalize"
					r.PhaseReason = "Source request limit reached. Save from retained evidence."
					return nil
				})
				if err != nil {
					return
				}
				if reject(step, errors.New("The source request limit is reached. Save records from retained evidence or finish with a reason. No source request was sent.")) {
					continue
				}
				return
			}
			key := digestOf(append([]byte(kind+"\n"), args...))
			repeated := false
			for _, a := range run.Attempts {
				if a.Kind == kind && a.Digest == key {
					repeated = true
					break
				}
			}
			if repeated {
				if reject(step, errors.New("This source request was already made. Use its retained result, a different source, or finish. No request was repeated.")) {
					continue
				}
				return
			}
			sourceRaw, e := researchAttempt(ctx, root, kind, args, func(call context.Context, current inputResearchRun, attempt inputResearchAttempt) (json.RawMessage, error) {
				source, e := ops.source(call, current, attempt, args)
				if e != nil {
					return nil, e
				}
				return json.Marshal(source)
			})
			if e != nil {
				continued, failure := continueResearchSource(ctx, root, e)
				if !continued {
					state := "interrupted"
					if errors.Is(failure, errResearchRequestLimit) || errors.Is(failure, errResearchStorageLimit) {
						_, err := root.change(func(r *inputResearchRun) error {
							r.Phase = "finalize"
							r.PhaseReason = failure.Error()
							return nil
						})
						if err != nil {
							return
						}
						continue
					}
					finish(state, failure)
					return
				}
			} else {
				var source inputResearchSource
				if json.Unmarshal(sourceRaw, &source) != nil {
					finish("blocked", errors.New("Saved source metadata could not be read."))
					return
				}
				_, e = root.change(func(r *inputResearchRun) error {
					return retainResearchSource(r, source)
				})
				if e != nil {
					finish("blocked", e)
					return
				}
			}
		}
		run, e = root.change(func(r *inputResearchRun) error { r.Step = step + 1; return nil })
		if e != nil {
			return
		}
		if researchWorkComplete(run) {
			finish("completed", nil)
			return
		}
	}
	finish("partial", errors.New("The approved model request limit was reached. Saved records are available for review."))
}
func researchPrompt(run inputResearchRun) (string, error) {
	// Only host-retained text and reviewed recipe data enter the next request.
	// Keep the model context bounded even when a run has many retained pages.
	sources := append([]inputResearchSource(nil), run.Sources...)
	remaining := aicontext.Defaults().HistoryBytes * 2
	for i := len(sources) - 1; i >= 0; i-- {
		// Exact URLs and short references are listed once in availableURLs.
		sources[i].URLs = nil
		limit := min(len(sources[i].Text), remaining)
		sources[i].Text = sources[i].Text[:limit]
		for !validUTF8([]byte(sources[i].Text)) {
			sources[i].Text = sources[i].Text[:len(sources[i].Text)-1]
		}
		remaining -= len(sources[i].Text)
	}
	counts := map[string]int{}
	for _, a := range run.Attempts {
		if a.State != "waiting" && a.State != "not-started" {
			counts[a.Kind]++
		}
	}
	limits := researchApprovedLimits(run)
	remainingRequests := map[string]int{"model": max(0, limits.ModelCalls-counts["model"]), "search": max(0, limits.Searches-counts["search"]), "page": max(0, limits.Pages-counts["page"])}
	payload := map[string]any{"retainedSourceIndex": researchContextIndex(run), "requestedContext": researchContextSelection(run, ""), "contractVersion": run.ContractVersion, "instructions": run.Intent, "recipe": run.Recipe, "recordKind": researchTargetKind(run.Recipe.Target), "approvedLimits": limits, "phase": run.Phase, "phaseInstructions": run.PhaseReason, "availableURLs": researchURLReferences(run), "actionFeedback": researchActionFeedback(run), "selectedCompanies": run.Companies, "retainedSources": sources, "savedRecords": run.Records, "selectedRecords": researchSelectedContext(run), "existingRecords": researchExistingContext(run), "recordResults": run.Outcomes, "reprocessSelectedOnly": run.Reprocess, "requestsUsed": counts, "requestsRemaining": remainingRequests, "recordGoal": map[string]int{"maximum": run.Recipe.Limits.Records, "saved": len(run.Records), "remaining": max(0, run.Recipe.Limits.Records-len(run.Records))}, "limitsAuthority": "Use approvedLimits and recordGoal for total record counts and request budgets, even when instructions contain older numbers. Instructions define target and research criteria. Retry decisions belong to the host and the saved recipe policy.", "candidateQueue": researchCandidateContext(run), "unavailableSources": researchSourceFailures(run)}
	if delegatedResearch(run) {
		payload["execution"] = run.Execution
		payload["delegatedTasks"] = researchTaskViews(run)
	}
	raw, e := json.Marshal(payload)
	// Keep source IDs and exact URL references stable. Only a prompt copy of
	// text is shortened; full retained text/proofs remain available for review.
	for e == nil && len(raw) > aicontext.Defaults().InputBytes()-len(inputResearchInstructions)-aicontext.Defaults().BriefBytes-4096 {
		changed := false
		for i := range sources {
			if len(sources[i].Text) > 256 {
				sources[i].Text = sources[i].Text[:len(sources[i].Text)/2]
				for !validUTF8([]byte(sources[i].Text)) {
					sources[i].Text = sources[i].Text[:len(sources[i].Text)-1]
				}
				changed = true
			}
		}
		payload["contextNote"] = "Some source text is shortened in this request. Cite only quotations actually visible here; full sources remain saved."
		raw, e = json.Marshal(payload)
		if !changed {
			break
		}
	}
	if e != nil || len(raw) > aicontext.Defaults().InputBytes()-len(inputResearchInstructions)-aicontext.Defaults().BriefBytes-4096 {
		return "", errors.New("The reviewed research context exceeds its model request limit.")
	}
	return string(raw), nil
}
func researchSourceArguments(run inputResearchRun, action inputResearchAction) (string, json.RawMessage, error) {
	if action.Tool == "search" {
		var q struct {
			Query string `json:"query"`
		}
		if decodeDataJSON(action.Arguments, &q) != nil || strings.TrimSpace(q.Query) == "" || len(q.Query) > 2000 || run.Recipe.Search == nil {
			return "", nil, errors.New("Research requires a bounded query and the reviewed search connection.")
		}
		args, _ := json.Marshal(map[string]any{"connection": run.Recipe.Search.Connection, "revision": run.Recipe.Search.Revision, "query": q.Query, "maxResults": 10})
		return "search", args, nil
	}
	var request struct {
		URL    string `json:"url"`
		URLRef string `json:"urlRef"`
	}
	if action.Tool != "read" || decodeDataJSON(action.Arguments, &request) != nil {
		return "", nil, errors.New("Invalid research source action.")
	}
	normalized := ""
	if request.URLRef != "" {
		for _, candidate := range researchURLReferences(run) {
			if candidate.Ref == request.URLRef {
				normalized = candidate.URL
				break
			}
		}
		if normalized == "" {
			return "", nil, errResearchURLReference
		}
		// Ref + URL is accepted only when both identify the same exact target.
		if request.URL != "" {
			u, err := inputResearchURL(request.URL)
			if err != nil || u != normalized {
				return "", nil, errResearchURLReference
			}
		}
	} else {
		var err error
		normalized, err = inputResearchURL(request.URL)
		if err != nil {
			return "", nil, err
		}
	}
	if !researchAllowedURLs(run)[normalized] {
		return "", nil, errResearchURLReference
	}
	args, _ := json.Marshal(map[string]any{"url": normalized})
	return "page", args, nil
}
func researchAllowedURLs(run inputResearchRun) map[string]bool {
	allowed := map[string]bool{}
	for _, seed := range run.Seeds {
		allowed[seed] = true
	}
	for _, source := range run.Sources {
		for _, url := range source.URLs {
			allowed[url] = true
		}
	}
	return allowed
}
func applyResearchRecords(run *inputResearchRun, args json.RawMessage) error {
	if run.ContractVersion >= 2 {
		return applyResearchRecordsV2(run, args)
	}
	var batch struct {
		Records []inputResearchEntity `json:"records"`
	}
	if decodeDataJSON(args, &batch) != nil || len(batch.Records) == 0 || len(batch.Records) > run.Recipe.Limits.Records {
		return errors.New("The model returned an invalid record batch.")
	}
	sources := map[string]inputResearchSource{}
	companies := map[string]bool{}
	for _, source := range run.Sources {
		sources[source.ID] = source
	}
	for _, c := range run.Companies {
		companies[c.ID] = true
	}
	records := append([]inputResearchEntity(nil), run.Records...)
	for _, entity := range batch.Records {
		if e := validateResearchEntity(&entity, run.Recipe, sources, researchAllowedURLs(*run), companies); e != nil {
			return fmt.Errorf("Record %q: %w", entity.Name, e)
		}
		if run.IdentityVersion >= 1 {
			key := researchEntityKeyForRun(*run, entity)
			entity.ID = run.EntityIDs[key]
			if entity.ID == "" {
				entity.ID = "entity_" + digestOf([]byte(key))
			}
		}
		found := false
		for i := range records {
			if records[i].ID == entity.ID || run.IdentityVersion >= 1 && researchEntityKeyForRun(*run, records[i]) == researchEntityKeyForRun(*run, entity) {
				records[i] = entity
				found = true
				break
			}
		}
		if !found {
			records = append(records, entity)
		}
	}
	if len(records) > run.Recipe.Limits.Records {
		return errors.New("The record batch exceeds the approved record limit.")
	}
	run.Records = records
	return nil
}
func (s *Server) publishResearchRecords(run inputResearchRun) error {
	if run.ContractVersion >= 2 {
		return s.publishResearchRecordsV2(run)
	}
	if len(run.Records) == 0 {
		return nil
	}
	repository := s.preparationRepository()
	saved, e := repository.Get(context.Background(), run.PreparationID)
	if e != nil {
		return e
	}
	doc := saved.Document
	before, _ := json.Marshal(doc.Records)
	for _, entity := range run.Records {
		record := researchPreparedRecord(run, entity)
		id := record.ID
		found := false
		for i := range doc.Records {
			if doc.Records[i].ID == id {
				if doc.Records[i].Research != nil && doc.Records[i].Research.RunID != run.ID {
					newer, err := s.researchRecordIsNewer(doc.Records[i], run)
					if err != nil {
						return err
					}
					if newer {
						found = true
						break
					}
				}
				// Re-publication must neither erase an unchanged review nor
				// carry acceptance onto changed research. History stays saved.
				if inputRecordDigest(doc.Records[i]) == inputRecordDigest(record) {
					record.Review = doc.Records[i].Review
				}
				doc.Records[i] = record
				found = true
				break
			}
		}
		if !found {
			duplicate := false
			for _, existing := range doc.Records {
				if existing.Research != nil && researchEntityKeyForRun(run, existing.Research.Entity) == researchEntityKeyForRun(run, entity) {
					newer, err := s.researchRecordIsNewer(existing, run)
					if err != nil {
						return err
					}
					if newer {
						duplicate = true
						break
					}
				}
			}
			if duplicate {
				continue
			}
			if len(doc.Records) >= inputRecordLimit {
				return errors.New("Input record limit reached.")
			}
			doc.Records = append(doc.Records, record)
		}
	}
	after, _ := json.Marshal(doc.Records)
	if string(before) == string(after) {
		return nil
	}
	doc.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	doc.Activity = append(doc.Activity, inputActivity{Kind: "researched", At: doc.UpdatedAt, Count: len(run.Records), RecipeVersion: run.RecipeVersion})
	_, e = repository.CompareAndSwap(context.Background(), doc, saved.SHA256)
	return e
}

func researchPreparedRecord(run inputResearchRun, entity inputResearchEntity) preparedInputRecord {
	id := "row_" + digestOf([]byte(run.ID+"\n"+entity.ID))
	if run.IdentityVersion >= 1 {
		key := researchEntityKeyForRun(run, entity)
		id = run.RowIDs[key]
		if id == "" {
			id = "row_" + digestOf([]byte(run.PreparationID+"\n"+key))
		}
	}
	if run.ContractVersion >= 2 {
		for _, outcome := range run.Outcomes {
			if outcome.EntityID == entity.ID && outcome.RecordID != "" {
				id = outcome.RecordID
				break
			}
		}
	}
	values := map[string]string{"name": entity.Name, "url": entity.URL}
	if entity.Website != "" {
		values["website"] = entity.Website
	}
	for key, field := range entity.Fields {
		if len(field.Value) == 0 || string(field.Value) == "null" {
			continue
		}
		var text string
		if json.Unmarshal(field.Value, &text) == nil {
			values[key] = text
		} else {
			values[key] = string(field.Value)
		}
	}
	// The host owns this relationship. A model field cannot replace the exact
	// company selected for this run, and exports/mappings must retain the link.
	if run.RelationshipVersion == 1 && entity.CompanyID != "" {
		values["company_id"] = entity.CompanyID
		for _, company := range run.Companies {
			if company.ID == entity.CompanyID {
				values["company_name"] = company.Name
				values["company_url"] = company.URL
				break
			}
		}
	}
	return preparedInputRecord{ID: id, Name: entity.Name, Values: values, SourceID: run.ID, Class: "generated", Status: "researched", Research: &inputResearchRecord{RunID: run.ID, RecipeVersion: run.RecipeVersion, Entity: entity}}
}
