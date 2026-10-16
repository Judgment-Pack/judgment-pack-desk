package desk

// A subscription upgrade is an immutable replacement, never a rewrite of the
// release which produced its historical rows. The coordinator holds both
// journals; each storage CAS remains short and Runner calls stay outside it.
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"time"
)

type subscriptionUpgradeReview struct {
	SourceID        string            `json:"sourceId"`
	SourceDigest    string            `json:"sourceDigest"`
	SourceReleaseID string            `json:"sourceReleaseId"`
	JobID           string            `json:"jobId"`
	BaseReleaseID   string            `json:"baseReleaseId"`
	BaseRevision    int               `json:"baseRevision"`
	TriggerID       string            `json:"triggerId"`
	Mode            string            `json:"mode"`
	SourceClass     string            `json:"sourceClass"`
	Baseline        map[string]string `json:"baseline"`
	PolicyDigest    string            `json:"policyDigest"`
	ReviewedAt      string            `json:"reviewedAt"`
}
type subscriptionSuccessor struct {
	CandidateID string `json:"candidateId"`
	State       string `json:"state"`
}

func subscriptionReserved(b inputEvaluation) bool {
	return len(b.Successors) > 0 && b.Successors[len(b.Successors)-1].State != "refused"
}
func subscriptionReviewDigest(b inputEvaluation) string {
	b.Successors = nil
	raw, _ := json.Marshal(b)
	return digestOf(raw)
}

type subscriptionLease struct {
	root       *os.Root
	lock       *os.File
	snapshot   evaluationSnapshot
	checkpoint evaluationCheckpoint
}

func (s *Server) leaseSubscriptionJournals(ctx context.Context, root *os.Root, ids ...string) (map[string]*subscriptionLease, func(), error) {
	sort.Strings(ids)
	held := map[string]*subscriptionLease{}
	close := func() {
		for _, id := range ids {
			if h := held[id]; h != nil {
				if h.lock != nil {
					h.lock.Close()
				}
				h.root.Close()
			}
		}
	}
	for _, id := range ids {
		if held[id] != nil {
			close()
			return nil, func() {}, errors.New("A subscription cannot replace itself.")
		}
		br, err := openInputEvaluation(root, id, false)
		if err != nil {
			close()
			return nil, func() {}, err
		}
		h := &subscriptionLease{root: br}
		held[id] = h
		h.lock, err = br.Open(".")
		if err == nil {
			err = waitPrivateDataLock(ctx, h.lock, true)
		}
		if err != nil {
			close()
			return nil, func() {}, errors.New("This subscription is being updated. Try again.")
		}
		journal := s.evaluationJournal(br, id)
		h.snapshot, err = journal.Read(ctx)
		if err != nil {
			close()
			return nil, func() {}, err
		}
		h.checkpoint = evaluationCheckpoint{journal: journal, revision: h.snapshot.SHA256}
	}
	return held, close, nil
}
func subscriptionSettled(b inputEvaluation) error {
	if b.Subscription == nil || b.ConfirmedAt == "" || b.JobID == "" || b.Subscription.Enabled || b.Subscription.StatePending {
		return errors.New("Pause this subscription and wait for its trigger before reviewing an upgrade.")
	}
	for _, row := range b.Rows {
		if !evaluationRowTerminal(row.State) {
			return errors.New("Wait for every retained delivery to settle before upgrading this subscription.")
		}
	}
	return nil
}
func (s *Server) subscriptionJobBase(ctx context.Context, b inputEvaluation) (inputUpgradeReview, error) {
	var base inputUpgradeReview
	health, err := s.inputRunner(ctx, "status", nil, "")
	var capabilities struct {
		Capabilities []string `json:"capabilities"`
	}
	if err != nil || json.Unmarshal(health, &capabilities) != nil || !contains(capabilities.Capabilities, "job-upgrade-receipts") {
		return base, errors.New("Update Runner before upgrading a subscription.")
	}
	raw, err := s.inputRunner(ctx, "jobs/"+b.JobID+"/triggers", nil, "")
	if err != nil {
		return base, err
	}
	var triggers struct {
		Items []struct {
			ID     string
			Paused bool
		} `json:"items"`
	}
	if json.Unmarshal(raw, &triggers) != nil {
		return base, errors.New("The subscription triggers could not be read.")
	}
	found := false
	for _, tr := range triggers.Items {
		if !tr.Paused {
			return base, errors.New("Pause every job trigger before reviewing this upgrade.")
		}
		if tr.ID == b.Subscription.TriggerID {
			found = true
		}
	}
	if !found {
		return base, errors.New("The subscription trigger is unavailable.")
	}
	raw, err = s.inputRunner(ctx, "jobs/"+b.JobID, nil, "")
	if err != nil {
		return base, err
	}
	var current struct {
		Job struct {
			ID, ReleaseID string
			Revision      int
		} `json:"job"`
	}
	if json.Unmarshal(raw, &current) != nil || current.Job.ID != b.JobID || !referenceReleaseID.MatchString(current.Job.ReleaseID) || current.Job.Revision < 1 {
		return base, errors.New("The subscription job could not be read.")
	}
	return inputUpgradeReview{JobID: current.Job.ID, BaseReleaseID: current.Job.ReleaseID, BaseRevision: current.Job.Revision}, nil
}
func (s *Server) reviewSubscriptionUpgrade(ctx context.Context, saved inputPreparationReply, candidate *inputEvaluation, sourceID string) error {
	if !inputEvaluationID.MatchString(sourceID) {
		return errors.New("Choose a saved subscription to upgrade.")
	}
	root, err := s.inputEvaluationRoot(false)
	if err != nil {
		return err
	}
	defer root.Close()
	leases, close, err := s.leaseSubscriptionJournals(ctx, root, sourceID)
	if err != nil {
		return err
	}
	defer close()
	source := leases[sourceID].snapshot.Document
	if source.PreparationID != candidate.PreparationID || subscriptionReserved(source) {
		return errors.New("This subscription already has a pending or completed upgrade.")
	}
	if err = subscriptionSettled(source); err != nil {
		return err
	}
	// A sample must come from the same asserted/generated source class.
	for _, row := range candidate.Rows {
		for _, record := range saved.Document.Records {
			if row.RecordID == record.ID && record.Class != source.Subscription.SourceClass {
				return errors.New("Choose a record from the subscription's original source class.")
			}
		}
	}
	base, err := s.subscriptionJobBase(ctx, source)
	if err != nil {
		return err
	}
	snap, err := s.readSnapshot()
	if err != nil {
		return err
	}
	policy, _ := json.Marshal([]any{string(snap.config), string(snap.lock), s.cfg.requireTestedReleases()})
	baseline := map[string]string{}
	for _, record := range saved.Document.Records {
		baseline[record.ID] = inputRecordDigest(record)
	}
	candidate.SubscriptionUpgrade = &subscriptionUpgradeReview{SourceID: sourceID, SourceDigest: subscriptionReviewDigest(source), SourceReleaseID: source.ReleaseID, JobID: base.JobID, BaseReleaseID: base.BaseReleaseID, BaseRevision: base.BaseRevision, TriggerID: source.Subscription.TriggerID, Mode: source.Subscription.Mode, SourceClass: source.Subscription.SourceClass, Baseline: baseline, PolicyDigest: digestOf(policy), ReviewedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	return nil
}
func (s *Server) handleSubscriptionUpgrade(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	fail := func(err error) { writeJSONCoded(w, 409, CodeStale, err.Error()) }
	var request struct {
		Reviewed bool   `json:"reviewed"`
		Action   string `json:"action"`
	}
	id, prep := r.PathValue("evaluation"), r.PathValue("preparation")
	if r.URL.RawQuery != "" || !inputEvaluationID.MatchString(id) || !inputPreparationID.MatchString(prep) || inputRequest(r, &request) != nil || !request.Reviewed || !contains([]string{"apply", "recover", "cancel", "retry"}, request.Action) {
		writeJSONCoded(w, 400, CodeBadRequest, "Review this subscription upgrade before confirming it.")
		return
	}
	root, err := s.inputEvaluationRoot(false)
	if err != nil {
		fail(err)
		return
	}
	defer root.Close()
	br, err := openInputEvaluation(root, id, false)
	if err != nil {
		fail(err)
		return
	}
	initial, err := s.evaluationJournal(br, id).Read(r.Context())
	br.Close()
	if err != nil {
		fail(err)
		return
	}
	if initial.Document.SubscriptionUpgrade == nil || initial.Document.PreparationID != prep {
		fail(errors.New("This evaluation is not a reviewed subscription upgrade."))
		return
	}
	leases, close, err := s.leaseSubscriptionJournals(r.Context(), root, id, initial.Document.SubscriptionUpgrade.SourceID)
	if err != nil {
		fail(err)
		return
	}
	defer close()
	candidateLease := leases[id]
	b := candidateLease.snapshot.Document
	review := b.SubscriptionUpgrade
	if review == nil || !sameEvaluationValue(review, initial.Document.SubscriptionUpgrade) || b.PreparationID != prep || r.Header.Get("If-Match") != b.Revision {
		fail(errors.New("Review the same subscription upgrade before continuing."))
		return
	}
	sourceLease := leases[review.SourceID]
	source := sourceLease.snapshot.Document
	if source.PreparationID != prep || source.JobID != review.JobID || source.Subscription == nil || source.Subscription.TriggerID != review.TriggerID {
		fail(errors.New("The subscription does not match the retained upgrade review."))
		return
	}
	candidateCheckpoint, sourceCheckpoint := &candidateLease.checkpoint, &sourceLease.checkpoint
	// This path precedes any mutable preparation read. It never dispatches an
	// upgrade, enables a trigger, or manufactures another execution record.
	if b.Subscription != nil && b.ConfirmedAt != "" {
		writeJSON(w, 200, b)
		return
	}
	finish := func() error {
		if b.Upgrade == nil || len(b.Upgrade.Result) == 0 {
			return errors.New("The upgrade has no accepted receipt.")
		}
		if len(source.Successors) == 0 || source.Successors[len(source.Successors)-1].CandidateID != b.ID {
			return errors.New("The retained subscription handoff is unavailable.")
		}
		last := &source.Successors[len(source.Successors)-1]
		if last.State != "pending" && last.State != "complete" {
			return errors.New("The subscription upgrade was refused.")
		}
		if last.State != "complete" {
			last.State = "complete"
			source.State = "superseded"
			source.Problem = ""
			if err := sourceCheckpoint.retain(source); err != nil {
				return err
			}
		}
		b.Subscription = &inputSubscription{Mode: review.Mode, SourceClass: review.SourceClass, TriggerID: review.TriggerID, Initialized: true, Baseline: review.Baseline, PolicyDigest: review.PolicyDigest}
		b.JobID = review.JobID
		b.ConfirmedAt = time.Now().UTC().Format(time.RFC3339Nano)
		b.Rows = []inputEvaluationRow{}
		b.State = "paused"
		b.Problem = ""
		return candidateCheckpoint.retain(b)
	}
	refuse := func(reason string) error {
		if b.Upgrade == nil || len(b.Upgrade.Result) > 0 {
			return errors.New("An accepted upgrade cannot be cancelled.")
		}
		b.Upgrade.Phase = "refused"
		b.Upgrade.Problem = reason
		if err := candidateCheckpoint.retain(b); err != nil {
			return err
		}
		if len(source.Successors) > 0 {
			last := &source.Successors[len(source.Successors)-1]
			if last.CandidateID == b.ID && last.State == "pending" {
				last.State = "refused"
				source.State = "paused"
				source.Problem = ""
				return sourceCheckpoint.retain(source)
			}
		}
		return nil
	}
	retrying := b.Upgrade != nil && b.Upgrade.Phase == "submitted"
	if b.Upgrade != nil {
		if len(b.Upgrade.Result) > 0 {
			if err = finish(); err != nil {
				fail(err)
				return
			}
			writeJSON(w, 200, b)
			return
		}
		if b.Upgrade.Phase == "refused" {
			if err = refuse(b.Upgrade.Problem); err != nil {
				fail(err)
				return
			}
			fail(errors.New(b.Upgrade.Problem))
			return
		}
		if b.Upgrade.Phase == "submitted" {
			q := url.Values{"baseRevision": {strconv.Itoa(review.BaseRevision)}, "baseReleaseId": {review.BaseReleaseID}, "releaseId": {b.ReleaseID}}
			raw, readErr := s.inputRunner(r.Context(), "jobs/"+review.JobID+"/upgrade-receipt?"+q.Encode(), nil, "")
			if readErr != nil {
				var missing *inputRunnerError
				if errors.As(readErr, &missing) && missing.Status == 409 && missing.Code == "job_changed" {
					// Runner has an immutable, different successor for this base.
					// This exact CAS can no longer commit; release only this attempt.
					if err = refuse("Another reviewed upgrade replaced this job revision. Review the subscription upgrade again."); err != nil {
						fail(err)
						return
					}
					writeJSON(w, 200, b)
					return
				}
				if !errors.As(readErr, &missing) || missing.Status != 404 {
					fail(readErr)
					return
				}
				if request.Action != "retry" {
					fail(errors.New("The upgrade result is still unknown. Check its receipt again or explicitly retry this exact reviewed upgrade."))
					return
				}
			} else {
				if err = retainInputUpgrade(&b, candidateCheckpoint, raw); err == nil {
					err = finish()
				}
				if err != nil {
					fail(err)
					return
				}
				writeJSON(w, 200, b)
				return
			}
		}
	}
	if request.Action == "retry" && !retrying {
		fail(errors.New("Only an uncertain submitted upgrade can be retried."))
		return
	}
	if request.Action == "recover" {
		fail(errors.New("The upgrade has not been submitted. Review and confirm it to continue."))
		return
	}
	if request.Action == "cancel" {
		if b.Upgrade == nil {
			fail(errors.New("This review has no reserved upgrade to cancel."))
			return
		}
		if err = refuse("This upgrade was cancelled before submission."); err != nil {
			fail(err)
			return
		}
		writeJSON(w, 200, b)
		return
	}
	if subscriptionReviewDigest(source) != review.SourceDigest || subscriptionReserved(source) && source.Successors[len(source.Successors)-1].CandidateID != id {
		fail(errors.New("The subscription changed. Review its upgrade again."))
		return
	}
	if err = subscriptionSettled(source); err != nil {
		fail(err)
		return
	}
	// Keep authoring and source approval stable until Runner has accepted/refused
	// this exact CAS. Nonblocking acquisition avoids reversing the worker's lease.
	view, err := s.preparationRepository().TryRead(r.Context())
	if err != nil {
		fail(err)
		return
	}
	defer view.Close()
	saved, err := view.Get(prep)
	if err != nil {
		fail(err)
		return
	}
	if saved.SHA256 != b.Revision {
		fail(errors.New("Dialects changed. Review the upgrade again."))
		return
	}
	unlock, err := s.lockProject(r.Context(), projectLockWait)
	if err != nil {
		fail(err)
		return
	}
	defer unlock()
	s.writes.Lock()
	defer s.writes.Unlock()
	private, err := s.privateDataLockContext(r.Context(), false)
	if err != nil {
		fail(err)
		return
	}
	defer private.Close()
	fingerprint, _, _, err := s.inputReleaseSnapshot(saved.Document.Recipe.Qualification)
	if err != nil || fingerprint != b.Fingerprint {
		fail(errors.New("The project, pack, or saved tests changed. Review the upgrade again."))
		return
	}
	var release struct {
		Tests string `json:"tests"`
	}
	if json.Unmarshal(b.Release, &release) != nil || release.Tests != "passed" && (b.RequireTested || release.Tests != "not-run") {
		fail(errors.New("Correct the saved tests before upgrading."))
		return
	}
	base, err := s.subscriptionJobBase(r.Context(), source)
	if err != nil {
		fail(err)
		return
	}
	if base.JobID != review.JobID || base.BaseReleaseID != review.BaseReleaseID || base.BaseRevision != review.BaseRevision {
		if retrying {
			q := url.Values{"baseRevision": {strconv.Itoa(review.BaseRevision)}, "baseReleaseId": {review.BaseReleaseID}, "releaseId": {b.ReleaseID}}
			raw, receiptErr := s.inputRunner(r.Context(), "jobs/"+review.JobID+"/upgrade-receipt?"+q.Encode(), nil, "")
			if receiptErr == nil {
				if err = retainInputUpgrade(&b, candidateCheckpoint, raw); err == nil {
					err = finish()
				}
				if err != nil {
					fail(err)
				} else {
					writeJSON(w, 200, b)
				}
				return
			}
		}
		fail(errors.New("The job changed. Check the retained upgrade result before reviewing another upgrade."))
		return
	}
	if b.Upgrade == nil {
		b.Upgrade = &inputUpgradeReview{JobID: review.JobID, BaseReleaseID: review.BaseReleaseID, BaseRevision: review.BaseRevision, Phase: "prepared"}
		if err = candidateCheckpoint.save(r.Context(), b); err != nil {
			fail(err)
			return
		}
	}
	if !subscriptionReserved(source) {
		source.Successors = append(source.Successors, subscriptionSuccessor{CandidateID: id, State: "pending"})
		if err = sourceCheckpoint.save(r.Context(), source); err != nil {
			fail(err)
			return
		}
	}
	if retrying {
		b.Upgrade.RetryAt = append(b.Upgrade.RetryAt, time.Now().UTC().Format(time.RFC3339Nano))
	}
	b.Upgrade.Phase = "submitted"
	if err = candidateCheckpoint.save(r.Context(), b); err != nil {
		fail(err)
		return
	}
	raw, err := s.inputRunner(r.Context(), "jobs/"+review.JobID+"/upgrade", map[string]any{"releaseId": b.ReleaseID, "baseReleaseId": review.BaseReleaseID, "baseRevision": review.BaseRevision, "reviewed": true}, "")
	if err != nil {
		var rejected *inputRunnerError
		if !retrying && errors.As(err, &rejected) && slices.Contains([]int{400, 403, 409, 422}, rejected.Status) {
			if savedErr := refuse(err.Error()); savedErr != nil {
				fail(savedErr)
				return
			}
		}
		fail(err)
		return
	}
	if err = retainInputUpgrade(&b, candidateCheckpoint, raw); err == nil {
		err = finish()
	}
	if err != nil {
		fail(err)
		return
	}
	writeJSON(w, 200, b)
}

// Retained review reads do not depend on a still-readable mutable preparation.
func (s *Server) handleSubscriptionUpgradeReview(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	id, prep := r.PathValue("evaluation"), r.PathValue("preparation")
	if r.URL.RawQuery != "" || !inputEvaluationID.MatchString(id) || !inputPreparationID.MatchString(prep) {
		writeJSONCoded(w, 400, CodeBadRequest, "Choose a retained subscription upgrade.")
		return
	}
	root, err := s.inputEvaluationRoot(false)
	if err != nil {
		writeJSONCoded(w, 404, CodeNotFound, "The retained upgrade is unavailable.")
		return
	}
	defer root.Close()
	br, err := openInputEvaluation(root, id, false)
	if err != nil {
		writeJSONCoded(w, 404, CodeNotFound, "The retained upgrade is unavailable.")
		return
	}
	defer br.Close()
	snapshot, err := s.evaluationJournal(br, id).Read(r.Context())
	if err != nil || snapshot.Document.PreparationID != prep || snapshot.Document.SubscriptionUpgrade == nil {
		writeJSONCoded(w, 404, CodeNotFound, "The retained upgrade is unavailable.")
		return
	}
	writeJSON(w, 200, snapshot.Document)
}
