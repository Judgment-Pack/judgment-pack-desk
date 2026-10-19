package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type researchRecordSnapshot struct {
	SourceAliases map[string]string   `json:"sourceAliases,omitempty"`
	RecordID      string              `json:"recordId"`
	Digest        string              `json:"digest"`
	RunID         string              `json:"runId"`
	RecipeVersion int                 `json:"recipeVersion"`
	Entity        inputResearchEntity `json:"entity"`
	SourceIDs     []string            `json:"sourceIds,omitempty"`
}
type researchRecordOutcome struct {
	ParentID    string `json:"parentId,omitempty"`
	Key         string `json:"key"`
	RecordID    string `json:"recordId,omitempty"`
	EntityID    string `json:"entityId,omitempty"`
	Name        string `json:"name"`
	State       string `json:"state"`
	Problem     string `json:"problem,omitempty"`
	AttemptID   string `json:"attemptId,omitempty"`
	RecoveredBy string `json:"recoveredBy,omitempty"`
}

func researchRecordSnapshots(doc inputPreparation) []researchRecordSnapshot {
	result := []researchRecordSnapshot{}
	for _, row := range doc.Records {
		if row.Research != nil {
			result = append(result, researchRecordSnapshot{RecordID: row.ID, Digest: inputRecordDigest(row), RunID: row.Research.RunID, RecipeVersion: row.Research.RecipeVersion, Entity: row.Research.Entity})
		}
	}
	return result
}
func researchExistingContext(run inputResearchRun) []map[string]any {
	result := []map[string]any{}
	for _, row := range run.Existing {
		result = append(result, map[string]any{"recordId": row.RecordID, "kind": row.Entity.Kind, "name": row.Entity.Name, "evidenceUrl": row.Entity.URL, "website": row.Entity.Website, "companyId": row.Entity.CompanyID})
	}
	return result
}
func sourceHasURL(source inputResearchSource, raw string) bool {
	if raw == "" {
		return false
	}
	for _, u := range source.URLs {
		if u == raw {
			return true
		}
	}
	return source.URL == raw
}

// Reprocessing is an explicit, immutable update scope. Only associated retained
// evidence is copied. The provider cannot broaden this scope or acquire sources.
func (s *Server) prepareResearchReprocessing(ctx context.Context, root *os.Root, run *inputResearchRun, doc inputPreparation, ids []string) error {
	if len(ids) == 0 || len(ids) > run.Recipe.Limits.Records {
		return errors.New("Select at least one record within the saved record limit. Review Limits and retries before increasing the selection.")
	}
	run.Reprocess = true
	run.SourceRuns = map[string]string{}
	run.SourceOriginIDs = map[string]string{}
	run.Seeds = nil
	run.Sources = nil
	seenRows := map[string]bool{}
	sources := map[string]inputResearchSource{}
	for _, id := range ids {
		if seenRows[id] {
			return errors.New("The selection contains a repeated record. Review the selection again.")
		}
		seenRows[id] = true
		var selected *researchRecordSnapshot
		for _, snapshot := range run.Existing {
			if snapshot.RecordID == id {
				copy := snapshot
				selected = &copy
				break
			}
		}
		if selected == nil {
			return fmt.Errorf("Selected record %s has no saved research. No records were skipped.", id)
		}
		if !researchKindAllowed(run.Recipe.Target, selected.Entity.Kind) {
			return fmt.Errorf("%s does not match the saved recipe target.", selected.Entity.Name)
		}
		rr, err := openInputResearch(root, selected.RunID, false)
		if err != nil {
			return fmt.Errorf("Saved research for %s is unavailable.", selected.Entity.Name)
		}
		parent, err := s.researchJournal(rr, selected.RunID).Read(ctx)
		if err != nil || parent.PreparationID != doc.ID || !researchCanContinue(parent) || parent.Gateway != run.Gateway {
			rr.Close()
			return fmt.Errorf("Saved evidence for %s is unavailable or incompatible. Review its research history.", selected.Entity.Name)
		}
		linked := map[string]bool{}
		for _, value := range selected.Entity.Fields {
			for _, cite := range value.Sources {
				linked[cite.SourceID] = true
			}
		}
		for _, source := range parent.Sources {
			if sourceHasURL(source, selected.Entity.URL) || sourceHasURL(source, selected.Entity.Website) {
				linked[source.ID] = true
			}
		}
		// Older missing fields may not cite their attempted page. Use only a host
		// candidate association whose normalized name identifies this record.
		for _, c := range parent.Candidates {
			if researchName(c.Name) == researchName(selected.Entity.Name) && c.SourceID != "" {
				linked[c.SourceID] = true
			}
		}
		subset := parent
		subset.Sources = nil
		hasPage := false
		for _, source := range parent.Sources {
			if linked[source.ID] {
				subset.Sources = append(subset.Sources, source)
				if source.Kind == "page" && source.Verification == "verified" {
					hasPage = true
				}
			}
		}
		if !hasPage {
			rr.Close()
			return fmt.Errorf("%s has no usable saved page evidence. Start new research to collect sources. No records were skipped.", selected.Entity.Name)
		}
		err = s.verifyResearchContinuation(ctx, rr, subset)
		rr.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", selected.Entity.Name, err)
		}
		for _, source := range subset.Sources {
			originID := source.ID
			if old, ok := sources[source.ID]; ok {
				a, _ := json.Marshal(old)
				b, _ := json.Marshal(source)
				if string(a) != string(b) {
					source.ID = "attempt_" + digestOf([]byte(parent.ID+"\n"+originID))
					if selected.SourceAliases == nil {
						selected.SourceAliases = map[string]string{}
					}
					selected.SourceAliases[originID] = source.ID
				}
			}
			selected.SourceIDs = append(selected.SourceIDs, source.ID)
			if _, exists := sources[source.ID]; exists {
				continue
			}
			sources[source.ID] = source
			run.Sources = append(run.Sources, source)
			run.SourceRuns[source.ID] = parent.ID
			run.SourceOriginIDs[source.ID] = originID
		}
		run.Selection = append(run.Selection, *selected)
	}
	run.Existing = append([]researchRecordSnapshot(nil), run.Selection...)
	run.Phase = "finalize"
	run.PhaseReason = "Reprocess only selected record IDs from their associated saved sources. No new records, searches or website reads are allowed."
	run.Recipe.Limits.Records = len(ids)
	run.Recipe.Limits.Searches = 0
	run.Recipe.Limits.Pages = 0
	run.Recipe.Search = nil
	return nil
}
func researchName(name string) string {
	parts := strings.Fields(strings.ToLower(strings.TrimSpace(name)))
	for len(parts) > 1 {
		last := strings.Trim(parts[len(parts)-1], ".,")
		if last != "inc" && last != "incorporated" && last != "ltd" && last != "limited" && last != "llc" && last != "corp" && last != "corporation" {
			break
		}
		parts = parts[:len(parts)-1]
	}
	return strings.Trim(strings.Join(parts, " "), " ,.")
}
func researchHost(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}
func researchSameIdentity(a, b inputResearchEntity) bool {
	if a.Kind != b.Kind || researchName(a.Name) != researchName(b.Name) {
		return false
	}
	if a.Kind != "company" {
		return a.URL != "" && a.URL == b.URL
	}
	for _, x := range []string{a.URL, a.Website} {
		for _, y := range []string{b.URL, b.Website} {
			if researchHost(x) != "" && researchHost(x) == researchHost(y) {
				return true
			}
		}
	}
	return false
}
func researchRef(run inputResearchRun, ref string) string {
	for _, r := range researchURLReferences(run) {
		if r.Ref == ref {
			return r.URL
		}
	}
	return ""
}
func researchOutcomeKey(recordID, candidateID, name string) string {
	if recordID != "" {
		return "row:" + recordID
	}
	if candidateID != "" {
		return "candidate:" + candidateID
	}
	return "name:" + researchName(name)
}
func putResearchOutcome(run *inputResearchRun, outcome researchRecordOutcome) {
	for i, o := range run.Outcomes {
		if o.Key == outcome.Key || outcome.RecordID != "" && outcome.RecordID == o.RecordID && o.State != "needs-correction" {
			run.Outcomes[i] = outcome
			return
		}
	}
	run.Outcomes = append(run.Outcomes, outcome)
}
func researchNeedsCorrection(run inputResearchRun) bool {
	for _, o := range run.Outcomes {
		if o.State == "needs-correction" {
			return true
		}
	}
	return false
}

// Global contract errors reject the action. Individual data errors are durable
// results, so supported siblings are not lost and corrections target only gaps.
func applyResearchRecordsV2(run *inputResearchRun, args json.RawMessage) error {
	var batch struct {
		Records []struct {
			inputResearchEntity
			RecordID     string `json:"recordId,omitempty"`
			CandidateID  string `json:"candidateId,omitempty"`
			URLRef       string `json:"urlRef,omitempty"`
			WebsiteRef   string `json:"websiteRef,omitempty"`
			CorrectionOf string `json:"correctionOf,omitempty"`
		} `json:"records"`
	}
	if decodeDataJSON(args, &batch) != nil || len(batch.Records) == 0 || len(batch.Records) > run.Recipe.Limits.Records {
		return errors.New("Return a bounded records array using saved references and selected record IDs.")
	}
	attemptID := run.AppliedAttempt
	if attemptID == "" && len(run.Attempts) > 0 {
		attemptID = run.Attempts[len(run.Attempts)-1].ID
	}
	if attemptID != "" {
		if a, e := researchAttemptByID(run, attemptID); e == nil {
			sequence := 0
			for _, prior := range run.Attempts {
				sequence = max(sequence, prior.AppliedSequence)
			}
			a.AppliedSequence = sequence + 1
		}
	}
	sources := map[string]inputResearchSource{}
	companies := map[string]bool{}
	for _, s := range run.Sources {
		sources[s.ID] = s
	}
	for _, c := range run.Companies {
		companies[c.ID] = true
	}
	for _, proposed := range batch.Records {
		entity := proposed.inputResearchEntity
		key := researchOutcomeKey(proposed.RecordID, proposed.CandidateID, entity.Name)
		if proposed.RecordID == "" && proposed.CandidateID == "" {
			key += ":" + proposed.URLRef + ":" + entity.URL
		}
		if run.IdentityVersion >= 2 && proposed.RecordID == "" && entity.CompanyID != "" {
			key += ":parent:" + entity.CompanyID
		}
		out := researchRecordOutcome{Key: key, RecordID: proposed.RecordID, Name: entity.Name, State: "needs-correction", AttemptID: attemptID}
		if run.IdentityVersion >= 2 {
			out.ParentID = entity.CompanyID
		}
		err := func() error {
			if proposed.CorrectionOf != "" {
				matched := false
				for _, previous := range run.Outcomes {
					if previous.Key == proposed.CorrectionOf && previous.State == "needs-correction" && researchName(previous.Name) == researchName(entity.Name) && previous.RecordID == proposed.RecordID && (run.IdentityVersion < 2 || previous.ParentID == entity.CompanyID) {
						out.Key = previous.Key
						matched = true
						break
					}
				}
				if !matched {
					return errors.New("correctionOf must identify an unresolved result for this same record.")
				}
			}
			var snapshot *researchRecordSnapshot
			for _, s := range run.Existing {
				if s.RecordID == proposed.RecordID {
					copy := s
					snapshot = &copy
					break
				}
			}
			if run.Reprocess && (proposed.RecordID == "" || snapshot == nil) {
				return errors.New("Choose a recordId from selectedRecords. This run cannot create or change other records.")
			}
			if proposed.RecordID != "" && snapshot == nil {
				return errors.New("The recordId is not in existingRecords.")
			}
			if snapshot != nil && (entity.Kind != snapshot.Entity.Kind || researchName(entity.Name) != researchName(snapshot.Entity.Name) || run.IdentityVersion >= 2 && entity.CompanyID != snapshot.Entity.CompanyID) {
				return errors.New("Keep the selected record's identity and name.")
			}
			if proposed.CandidateID != "" {
				found := false
				for _, c := range run.Candidates {
					if c.ID == proposed.CandidateID && researchName(c.Name) == researchName(entity.Name) {
						found = true
					}
				}
				if !found {
					return errors.New("Use an associated candidateId from candidateQueue.")
				}
			}
			if proposed.WebsiteRef != "" {
				entity.Website = researchRef(*run, proposed.WebsiteRef)
				if entity.Website == "" {
					return errors.New("Website wasn't found in saved sources. Choose a retained page reference or omit websiteRef.")
				}
				page := false
				for _, s := range run.Sources {
					if s.Kind == "page" && s.URL == entity.Website && s.Verification == "verified" {
						page = true
					}
				}
				if !page {
					return errors.New("Website is not a retained page. Omit websiteRef when unconfirmed.")
				}
			} else if entity.Website != "" {
				return errors.New("Use websiteRef for a confirmed website; omit it when unconfirmed.")
			} else if snapshot != nil {
				entity.Website = snapshot.Entity.Website
			}
			if proposed.URLRef != "" {
				entity.URL = researchRef(*run, proposed.URLRef)
				if entity.URL == "" {
					return errors.New("Evidence link wasn't found in saved sources. Use a ref from availableURLs.")
				}
			}
			// The host can choose a cited page as the evidence link. This does not make
			// it the official website. Raw URL compatibility remains provenance checked.
			if entity.URL == "" {
				for _, f := range run.Recipe.Fields {
					for _, cite := range entity.Fields[f.Name].Sources {
						if s, ok := sources[cite.SourceID]; ok && s.Kind == "page" {
							entity.URL = s.URL
							break
						}
					}
					if entity.URL != "" {
						break
					}
				}
			}
			if run.Reprocess {
				linked := map[string]bool{}
				for _, id := range snapshot.SourceIDs {
					linked[id] = true
				}
				allowed := map[string]bool{}
				for id := range linked {
					for _, u := range sources[id].URLs {
						allowed[u] = true
					}
				}
				if !allowed[entity.URL] {
					return errors.New("Use evidence associated with this selected record.")
				}
				if proposed.WebsiteRef != "" && !allowed[entity.Website] {
					return errors.New("Website evidence is not associated with this selected record.")
				}
				for _, v := range entity.Fields {
					for _, cite := range v.Sources {
						if !linked[cite.SourceID] {
							return errors.New("A citation belongs to another selected record's evidence.")
						}
					}
				}
			}
			normalized, urlErr := inputResearchURL(entity.URL)
			if urlErr != nil || !researchAllowedURLs(*run)[normalized] {
				return errors.New("Evidence link wasn't found in saved sources. Use a saved reference; leave an unconfirmed website missing. A source title alone is not a URL reference.")
			}
			entity.URL = normalized
			validationRecipe := run.Recipe
			if snapshot != nil && (validationRecipe.Target == "records" || validationRecipe.Target == "") {
				// Identity was checked against this exact saved row above.
				switch snapshot.Entity.Kind {
				case "company":
					validationRecipe.Target = "companies"
				case "person":
					validationRecipe.Target = "people"
				}
			}
			if err := validateResearchEntity(&entity, validationRecipe, sources, researchAllowedURLs(*run), companies); err != nil {
				return err
			}
			if snapshot == nil {
				for _, s := range run.Existing {
					if researchSameIdentityForRun(*run, s.Entity, entity) {
						if snapshot != nil {
							return errors.New("Multiple existing records match this company. Supply the intended recordId.")
						}
						copy := s
						snapshot = &copy
					}
				}
			}
			if snapshot != nil {
				entity.ID = snapshot.Entity.ID
				out.RecordID = snapshot.RecordID
			} else {
				entity.ID = "entity_" + digestOf([]byte(researchEntityKeyForRun(*run, entity)))
				for _, saved := range run.Records {
					if researchSameIdentityForRun(*run, saved, entity) {
						entity.ID = saved.ID
						break
					}
				}
				if entity.ID == "" {
					entity.ID = "entity_" + digestOf([]byte(researchEntityKeyForRun(*run, entity)))
				}
				out.RecordID = "row_" + digestOf([]byte(run.PreparationID+"\n"+entity.ID))
			}
			out.EntityID = entity.ID
			found := false
			for i, old := range run.Records {
				if old.ID == entity.ID {
					run.Records[i] = entity
					found = true
					break
				}
			}
			if !found {
				if len(run.Records) >= run.Recipe.Limits.Records {
					return errors.New("The approved record limit is reached.")
				}
				run.Records = append(run.Records, entity)
			}
			out.State = "pending"
			run.PublicationPending = true
			return nil
		}()
		if err != nil {
			out.Problem = err.Error()
		}
		putResearchOutcome(run, out)
	}
	return nil
}
func recordResearchResults(run *inputResearchRun) {
	if len(run.Attempts) == 0 {
		return
	}
	index := len(run.Attempts) - 1
	if run.AppliedAttempt != "" {
		for i := range run.Attempts {
			if run.Attempts[i].ID == run.AppliedAttempt {
				index = i
				break
			}
		}
	}
	last := &run.Attempts[index]
	last.RecordResults = nil
	for _, out := range run.Outcomes {
		if out.AttemptID == last.ID {
			last.RecordResults = append(last.RecordResults, out)
		}
	}
	problems := []string{}
	for _, out := range last.RecordResults {
		if out.State == "needs-correction" {
			problems = append(problems, fmt.Sprintf("%s: %s", out.Name, out.Problem))
		}
	}
	last.ActionProblem = strings.Join(problems, "\n")
}
func researchContentEqual(row preparedInputRecord, entity inputResearchEntity, version int) bool {
	if row.Research == nil || row.Research.RecipeVersion != version {
		return false
	}
	a, _ := json.Marshal(row.Research.Entity)
	b, _ := json.Marshal(entity)
	return string(a) == string(b)
}
func (s *Server) publishResearchRecordsV2(run inputResearchRun) error {
	if !run.PublicationPending {
		return nil
	}
	outcomes := append([]researchRecordOutcome(nil), run.Outcomes...)
	err := func() error {
		repository := s.preparationRepository()
		saved, e := repository.Get(context.Background(), run.PreparationID)
		if e != nil {
			return e
		}
		doc := saved.Document
		if doc.RecipeVersion != run.RecipeVersion {
			return errors.New("The recipe changed before results could be published. Retained results were kept; review the current recipe.")
		}
		changed := 0
		for oi := range outcomes {
			out := &outcomes[oi]
			if out.State != "pending" {
				continue
			}
			var entity *inputResearchEntity
			for _, v := range run.Records {
				if v.ID == out.EntityID {
					copy := v
					entity = &copy
					break
				}
			}
			if entity == nil {
				return errors.New("A retained record result is unavailable.")
			}
			rowIndex := -1
			for i, r := range doc.Records {
				if r.ID == out.RecordID {
					rowIndex = i
					break
				}
			}
			var original *researchRecordSnapshot
			for _, s := range run.Existing {
				if s.RecordID == out.RecordID {
					copy := s
					original = &copy
					break
				}
			}
			if run.Reprocess && original == nil {
				return errors.New("A result is outside the selected records.")
			}
			if original != nil && rowIndex < 0 {
				return errors.New("A selected record was removed. Review the selection again.")
			}
			if rowIndex >= 0 {
				current := doc.Records[rowIndex]
				// Reconcile a write committed before a crash without repeating it. A newer
				// edit or review cannot be overwritten merely because the worker retries.
				if researchUnchangedContent(run, current, *entity) {
					out.State = "unchanged"
					if current.Research.RunID == run.ID {
						out.State = "added"
						if original != nil {
							out.State = "updated"
						}
					}
					continue
				}
				if original == nil || inputRecordDigest(current) != original.Digest {
					if current.Research == nil || current.Research.RunID != run.ID {
						return errors.New("A record changed while research was running. Retained results were kept; review the selection again.")
					}
				}
				row := researchPreparedRecord(run, *entity)
				row.ID = current.ID
				doc.Records[rowIndex] = row
				out.State = "updated"
				if original == nil {
					out.State = "added"
				}
				changed++
			} else {
				if run.Reprocess {
					return errors.New("Reprocessing cannot add records.")
				}
				for _, r := range doc.Records {
					if r.Research != nil && researchSameIdentityForRun(run, r.Research.Entity, *entity) {
						return errors.New("A matching record was added while research was running. Review the current records.")
					}
				}
				if len(doc.Records) >= inputRecordLimit {
					return errors.New("Input record limit reached.")
				}
				row := researchPreparedRecord(run, *entity)
				row.ID = out.RecordID
				doc.Records = append(doc.Records, row)
				out.State = "added"
				changed++
			}
		}
		if changed > 0 {
			doc.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			doc.Activity = append(doc.Activity, inputActivity{Kind: "researched", At: doc.UpdatedAt, Count: changed, RecipeVersion: run.RecipeVersion})
			_, e = repository.CompareAndSwap(context.Background(), doc, saved.SHA256)
		}
		return e
	}()
	if err != nil {
		return err
	}
	root, err := s.inputResearchRoot(false)
	if err != nil {
		return err
	}
	defer root.Close()
	rr, err := openInputResearch(root, run.ID, false)
	if err != nil {
		return err
	}
	defer rr.Close()
	_, err = s.researchRunStore(rr, run.ID).change(func(current *inputResearchRun) error {
		// A stale publication pass must not acknowledge a newer proposal.
		a, _ := json.Marshal(current.Records)
		b, _ := json.Marshal(run.Records)
		if string(a) != string(b) {
			return errors.New("Newer results are waiting to be published.")
		}
		current.Outcomes = outcomes
		for _, outcome := range outcomes {
			if outcome.State == "pending" || outcome.State == "needs-correction" {
				continue
			}
			for i := range current.Attempts {
				for j := range current.Attempts[i].RecordResults {
					result := &current.Attempts[i].RecordResults[j]
					if result.Key != outcome.Key {
						continue
					}
					if current.Attempts[i].ID == outcome.AttemptID {
						result.State = outcome.State
					}
					if result.State == "needs-correction" && result.RecoveredBy == "" {
						result.RecoveredBy = outcome.AttemptID
					}
				}
			}
		}
		current.PublicationPending = false
		if current.PublicationResumeState != "" {
			current.State = current.PublicationResumeState
			current.PublicationResumeState = ""
			current.Problem = ""
			if current.State == "partial" {
				current.Problem = "Retained results were saved to Dialects. This run stopped before its remaining work completed."
			}
		}
		return nil
	})
	return err
}

type researchOutcomeCounts struct {
	Added           int `json:"added"`
	Updated         int `json:"updated"`
	Unchanged       int `json:"unchanged"`
	NeedsCorrection int `json:"needsCorrection"`
	Pending         int `json:"pending"`
}

func countResearchOutcomes(run inputResearchRun) researchOutcomeCounts {
	counts := researchOutcomeCounts{}
	seen := map[string]bool{}
	for _, o := range run.Outcomes {
		key := o.Key
		if o.RecordID != "" {
			key = o.RecordID
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		switch o.State {
		case "added":
			counts.Added++
		case "updated":
			counts.Updated++
		case "unchanged":
			counts.Unchanged++
		case "needs-correction":
			counts.NeedsCorrection++
		case "pending":
			counts.Pending++
		}
	}
	return counts
}

func validateResearchRecordScope(run inputResearchRun) error {
	if run.IdentityVersion < 0 || run.IdentityVersion > 2 {
		return errors.New("Unsupported research identity version.")
	}
	if run.ContractVersion < 0 || run.ContractVersion > 2 {
		return errors.New("Unsupported research record contract.")
	}
	if !run.Reprocess {
		return nil
	}
	if run.ContractVersion != 2 || len(run.Selection) == 0 || len(run.Selection) != run.Recipe.Limits.Records || len(run.Existing) != len(run.Selection) || run.Recipe.Limits.Searches != 0 || run.Recipe.Limits.Pages != 0 || run.Recipe.Search != nil || run.Phase != "finalize" || run.Approval != nil || len(run.BudgetRequests) > 0 {
		return errors.New("Invalid selected-record research scope.")
	}
	seen := map[string]bool{}
	for i, row := range run.Selection {
		if row.RecordID == "" || row.Entity.ID == "" || row.Digest == "" || seen[row.RecordID] || len(row.SourceIDs) == 0 {
			return errors.New("Invalid selected record snapshot.")
		}
		seen[row.RecordID] = true
		a, _ := json.Marshal(row)
		b, _ := json.Marshal(run.Existing[i])
		if string(a) != string(b) {
			return errors.New("Selected records no longer match their saved snapshots.")
		}
	}
	for _, out := range run.Outcomes {
		if out.State != "needs-correction" && !seen[out.RecordID] {
			return errors.New("A retained result is outside the selected records.")
		}
	}
	for _, source := range run.Sources {
		if !inputResearchID.MatchString(run.SourceRuns[source.ID]) {
			return errors.New("Missing saved evidence origin.")
		}
	}
	return nil
}

// Source IDs are local references, not signed receipt identifiers. Two parent
// runs may have used the same request hash for different observations. Namespace
// collisions while retaining each original proof and per-record association.
func researchSelectedContext(run inputResearchRun) []researchRecordSnapshot {
	raw, _ := json.Marshal(run.Selection)
	var selected []researchRecordSnapshot
	_ = json.Unmarshal(raw, &selected)
	for i := range selected {
		for key, value := range selected[i].Entity.Fields {
			for j := range value.Sources {
				if alias := selected[i].SourceAliases[value.Sources[j].SourceID]; alias != "" {
					value.Sources[j].SourceID = alias
				}
			}
			selected[i].Entity.Fields[key] = value
		}
	}
	return selected
}
func researchUnchangedContent(run inputResearchRun, row preparedInputRecord, entity inputResearchEntity) bool {
	if row.Research == nil {
		return false
	}
	raw, _ := json.Marshal(entity)
	var compared inputResearchEntity
	_ = json.Unmarshal(raw, &compared)
	for key, value := range compared.Fields {
		for j := range value.Sources {
			id := value.Sources[j].SourceID
			if run.SourceRuns[id] == row.Research.RunID && run.SourceOriginIDs[id] != "" {
				value.Sources[j].SourceID = run.SourceOriginIDs[id]
			}
		}
		compared.Fields[key] = value
	}
	return researchContentEqual(row, compared, run.RecipeVersion)
}
