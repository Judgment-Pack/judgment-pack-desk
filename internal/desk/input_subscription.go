package desk

// Desk publishes reviewed immutable revisions. Runner owns durable occurrences,
// queues and execution; this outbox never evaluates a case or replays a provider.
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

type inputSubscription struct {
	PolicyDigest    string            `json:"policyDigest,omitempty"`
	RetryAt         string            `json:"retryAt,omitempty"`
	Failures        int               `json:"failures,omitempty"`
	Mode            string            `json:"mode"`
	IncludeExisting bool              `json:"includeExisting"`
	Enabled         bool              `json:"enabled"`
	StatePending    bool              `json:"statePending,omitempty"`
	Baseline        map[string]string `json:"baseline,omitempty"`
	Initialized     bool              `json:"initialized"`
	TriggerID       string            `json:"triggerId,omitempty"`
	SourceClass     string            `json:"sourceClass"`
}

func subscriptionTrigger() map[string]any {
	return map[string]any{"name": "Reviewed Dialects", "kind": "event", "missed": "skip", "overlap": "queue", "queueSeconds": 3600}
}
func subscriptionIdentity(b inputEvaluation, r preparedInputRecord) string {
	raw, _ := json.Marshal([]any{b.PreparationID, r.ID, inputRecordDigest(r), b.RecipeVersion, b.ReleaseID})
	return "input-" + digestOf(raw)
}
func subscriptionEligible(b inputEvaluation, doc inputPreparation, r preparedInputRecord) bool {
	a := b.Subscription
	if a == nil || r.Class != a.SourceClass {
		return false
	}
	if r.Class == "generated" && readyInputReview(doc, r) != nil {
		return false
	}
	if a.Mode == "new" {
		if _, ok := a.Baseline[r.ID]; ok {
			return false
		}
		for _, row := range b.Rows {
			if row.RecordID == r.ID {
				return false
			}
		}
	} else if a.Baseline[r.ID] == inputRecordDigest(r) {
		return false
	}
	identity := subscriptionIdentity(b, r)
	for _, row := range b.Rows {
		if row.EventID == identity {
			return false
		}
	}
	return true
}
func initializeSubscription(a *inputSubscription, doc inputPreparation) {
	if a.Initialized {
		return
	}
	a.Baseline = map[string]string{}
	if !a.IncludeExisting {
		for _, r := range doc.Records {
			a.Baseline[r.ID] = inputRecordDigest(r)
		}
	}
	a.Initialized = true
}
func (s *Server) subscriptionDocument(reader inputPreparationReader, b inputEvaluation) (inputPreparationReply, error) {
	saved, e := reader.Get(b.PreparationID)
	if e != nil {
		return saved, e
	}
	if saved.Document.RecipeVersion != b.RecipeVersion {
		return saved, errors.New("The recipe changed. Review a new Dialects subscription before continuing.")
	}
	q := saved.Document.Recipe.Qualification
	if q == nil {
		return saved, errors.New("The pack dialect was removed. Review a new subscription.")
	}
	snap, e := s.readSnapshot()
	if e != nil {
		return saved, e
	}
	matched := false
	for _, d := range snap.docs {
		if d.entry.Kind == "pack" && d.entry.ID == q.Key && string(d.data) == q.Pack {
			matched = true
		}
	}
	if !matched {
		return saved, errors.New("The pack changed. Review a new subscription before continuing.")
	}
	policyRaw, _ := json.Marshal([]any{string(snap.config), string(snap.lock), s.cfg.requireTestedReleases()})
	policy := digestOf(policyRaw)
	if b.Subscription.PolicyDigest != "" && policy != b.Subscription.PolicyDigest {
		return saved, errors.New("Project policy changed. Review a new Dialects subscription.")
	}
	// First enable checks the exact reviewed test snapshot. An active subscription
	// continues using that frozen release when later authoring cases are edited.
	if !b.Subscription.Initialized {
		fingerprint, _, _, e := s.inputReleaseSnapshot(q)
		if e != nil {
			return saved, e
		}
		if fingerprint != b.Fingerprint {
			return saved, errors.New("The reviewed release changed. Review a new Dialects subscription.")
		}
		b.Subscription.PolicyDigest = policy
	}

	return saved, nil
}
func (s *Server) handleInputSubscriptions(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	root, e := s.inputEvaluationRoot(false)
	if errors.Is(e, os.ErrNotExist) {
		writeJSON(w, 200, map[string]any{"items": []any{}})
		return
	}
	if e != nil {
		writeJSONCoded(w, 409, CodeBadRequest, "Subscriptions could not be read.")
		return
	}
	defer root.Close()
	dir, e := root.Open(".")
	if e != nil {
		writeJSONCoded(w, 409, CodeBadRequest, "Subscriptions could not be read.")
		return
	}
	defer dir.Close()
	entries, e := dir.ReadDir(-1)
	if e != nil {
		writeJSONCoded(w, 409, CodeBadRequest, "Subscriptions could not be read.")
		return
	}
	items := []inputEvaluation{}
	for _, entry := range entries {
		if !inputEvaluationID.MatchString(entry.Name()) {
			continue
		}
		br, e := openInputEvaluation(root, entry.Name(), false)
		if e != nil {
			writeJSONCoded(w, 409, CodeBadRequest, "Subscriptions could not be read.")
			return
		}
		snapshot, e := s.evaluationJournal(br, entry.Name()).Read(r.Context())
		b := snapshot.Document
		br.Close()
		if e != nil {
			writeJSONCoded(w, 409, CodeBadRequest, "Subscriptions could not be read.")
			return
		}
		if b.Subscription == nil {
			continue
		}
		b.Release = nil
		b.Recipe = inputRecipe{}
		b.Rows = nil
		held := *b.Subscription
		held.Baseline = nil
		b.Subscription = &held
		items = append(items, b)
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) advanceInputSubscription(ctx context.Context, checkpoint *evaluationCheckpoint, b *inputEvaluation) {
	if checkpoint.failure != nil {
		return
	}
	a := b.Subscription
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if at, e := time.Parse(time.RFC3339Nano, a.RetryAt); e == nil && time.Now().Before(at) {
		return
	}
	save := func() bool { return checkpoint.retain(*b) == nil }
	problem := func(e error) {
		b.Problem = e.Error()
		b.State = "blocked"
		a.Failures++
		a.RetryAt = ""
		var refusal *inputRunnerError
		retryable := errors.As(e, &refusal) && (refusal.Status == 0 || refusal.Status >= 500 || refusal.Status == 429)
		if retryable && a.Failures < 5 {
			b.State = "queued"
			a.RetryAt = time.Now().Add(time.Second * time.Duration(1<<a.Failures)).UTC().Format(time.RFC3339Nano)
		}
		save()
	}
	if e := s.ensureEvaluationJob(ctx, checkpoint, b); e != nil {
		if checkpoint.failure == nil {
			problem(e)
		}
		return
	}
	raw, e := s.inputRunner(ctx, "jobs/"+b.JobID+"/triggers", nil, "")
	if e != nil {
		problem(e)
		return
	}
	var list struct {
		Items []struct {
			ID       string          `json:"id"`
			Revision int             `json:"revision"`
			Paused   bool            `json:"paused"`
			Config   json.RawMessage `json:"config"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &list) != nil {
		problem(errors.New("The subscription trigger could not be read."))
		return
	}
	found := false
	for _, tr := range list.Items {
		if tr.ID != a.TriggerID {
			continue
		}
		found = true
		var config map[string]any
		json.Unmarshal(tr.Config, &config)
		if config["kind"] != "event" || config["overlap"] != "queue" || config["queueSeconds"] != float64(3600) {
			problem(errors.New("The subscription trigger changed. Review a new subscription."))
			return
		}
		if a.StatePending {
			if a.Enabled {
				// Read the trigger revision first, then the job. Runner bumps
				// trigger revisions atomically with a job upgrade, so the CAS
				// below cannot enable a later release after this pin check.
				jobRaw, err := s.inputRunner(ctx, "jobs/"+b.JobID, nil, "")
				var current struct {
					Job struct{ ID, ReleaseID string } `json:"job"`
				}
				if err != nil {
					problem(err)
					return
				}
				if json.Unmarshal(jobRaw, &current) != nil || current.Job.ID != b.JobID || current.Job.ReleaseID != b.ReleaseID {
					problem(errors.New("The job release changed. Review a new subscription before enabling it."))
					return
				}
			}
			if tr.Paused == a.Enabled {
				if _, e = s.inputRunner(ctx, "triggers/"+tr.ID+"/state", map[string]any{"revision": tr.Revision, "paused": !a.Enabled, "reviewed": a.Enabled}, ""); e != nil {
					problem(e)
					return
				}
			}
			a.StatePending = false
			if !save() {
				return
			}
		} else if tr.Paused && a.Enabled {
			a.Enabled = false
			b.State = "paused"
			save()
			return
		}
	}
	if !found {
		problem(errors.New("The subscription trigger is unavailable."))
		return
	}
	// Reconcile accepted deliveries without resubmitting them or acquiring sources.
	budget := 20
	if !a.Enabled {
		// Pausing closes admission, not receipt recovery. A delivery may have
		// been accepted immediately before Desk lost its acknowledgement.
		// Only read that exact receipt; 404 keeps the intent and signed bytes.
		for i := range b.Rows {
			row := &b.Rows[i]
			if row.State != "pending" || row.OccurrenceID != "" || row.EventID == "" {
				continue
			}
			if budget == 0 {
				break
			}
			budget--
			found, err := recoverPausedSubscriptionDelivery(ctx, *b, row, func(ctx context.Context, path string) (json.RawMessage, error) {
				return s.inputRunner(ctx, path, nil, "")
			})
			if err != nil {
				problem(err)
				return
			}
			if found && !save() {
				return
			}
		}
	}
	for i := range b.Rows {
		row := &b.Rows[i]
		if row.OccurrenceID == "" || row.State == "completed" || row.State == "failed" || row.State == "expired" || row.State == "skipped" || row.State == "cancelled" || row.State == "interrupted" {
			continue
		}
		if budget == 0 {
			break
		}
		budget--
		raw, e := s.inputRunner(ctx, "triggers/"+a.TriggerID+"/deliveries/"+row.EventID, nil, "")
		if e != nil {
			problem(e)
			return
		}
		var ack struct {
			Occurrence struct {
				ID     string `json:"id"`
				State  string `json:"state"`
				RunID  string `json:"runId"`
				Reason string `json:"reason"`
			} `json:"occurrence"`
		}
		if json.Unmarshal(raw, &ack) != nil || ack.Occurrence.ID != row.OccurrenceID {
			problem(errors.New("The retained occurrence changed identity."))
			return
		}
		row.State = ack.Occurrence.State
		row.RunID = ack.Occurrence.RunID
		row.Problem = ack.Occurrence.Reason
		if row.RunID != "" {
			raw, e = s.inputRunner(ctx, "runs/"+row.RunID, nil, "")
			if e != nil {
				problem(e)
				return
			}
			var run struct {
				ID      string          `json:"id"`
				State   string          `json:"state"`
				Result  json.RawMessage `json:"result"`
				Problem string          `json:"problem"`
				Signing json.RawMessage `json:"signing"`
			}
			if json.Unmarshal(raw, &run) != nil || run.ID != row.RunID {
				problem(errors.New("The retained run changed identity."))
				return
			}
			var result struct {
				Disposition   json.RawMessage `json:"disposition"`
				HandoffTarget json.RawMessage `json:"handoffTarget,omitempty"`
			}
			if len(run.Result) > 0 && json.Unmarshal(run.Result, &result) == nil {
				row.Result, _ = json.Marshal(result)
			}
			switch run.State {
			case "queued", "running", "completed", "failed", "interrupted":
			default:
				problem(errors.New("The retained run state is invalid."))
				return
			}
			row.State = run.State
			row.Problem = run.Problem
			row.Signing = run.Signing
		}
		if !save() {
			return
		}
	}
	if !a.Enabled {
		b.State = "paused"
		b.Problem = ""
		save()
		return
	}
	// Keep the reviewed collection stable through admission. This acquisition is
	// nonblocking, separate from the worker's ownership lease. A snapshot that
	// permits concurrent revocation is not sufficient here.
	view, e := s.preparationRepository().TryRead(ctx)
	if e != nil {
		return
	}
	defer view.Close()
	saved, e := s.subscriptionDocument(view, *b)
	if e != nil {
		problem(e)
		return
	}
	// Persist exact delivery bytes BEFORE the first delivery, never regenerate a
	// receipt or timestamp after an ambiguous transport failure.
	for i := range b.Rows {
		row := &b.Rows[i]
		if row.EventID == "" || row.OccurrenceID != "" || row.State != "pending" {
			continue
		}
		eligible := false
		for _, record := range saved.Document.Records {
			if record.ID == row.RecordID && inputRecordDigest(record) == row.RecordDigest && (record.Class != "generated" || readyInputReview(saved.Document, record) == nil) {
				eligible = true
				break
			}
		}
		if !eligible {
			raw, e := s.inputRunner(ctx, "triggers/"+a.TriggerID+"/deliveries/"+row.EventID, nil, "")
			var refusal *inputRunnerError
			if errors.As(e, &refusal) && refusal.Status == 404 {
				row.State = "superseded"
				row.Input = nil
				if !save() {
					return
				}
				continue
			}
			if e != nil {
				problem(e)
				return
			}
			var ack struct {
				Occurrence struct {
					ID    string `json:"id"`
					State string `json:"state"`
					RunID string `json:"runId"`
				} `json:"occurrence"`
			}
			if json.Unmarshal(raw, &ack) != nil || !strings.HasPrefix(ack.Occurrence.ID, "occ_") {
				problem(errors.New("The retained delivery could not be read."))
				return
			}
			row.OccurrenceID = ack.Occurrence.ID
			row.State = ack.Occurrence.State
			row.RunID = ack.Occurrence.RunID
			row.Input = nil
			if !save() {
				return
			}
			continue
		}
		event := map[string]any{"id": row.EventID, "occurredAt": row.EventAt, "input": row.Input}
		raw, e := s.inputRunner(ctx, "triggers/"+a.TriggerID+"/deliveries", map[string]any{"releaseId": b.ReleaseID, "event": event}, "")
		if e != nil {
			problem(e)
			return
		}
		var reply struct {
			Occurrence struct {
				ID    string `json:"id"`
				State string `json:"state"`
				RunID string `json:"runId"`
			} `json:"occurrence"`
		}
		if json.Unmarshal(raw, &reply) != nil || !strings.HasPrefix(reply.Occurrence.ID, "occ_") {
			problem(errors.New("The delivery acknowledgement is invalid. The same delivery will be reconciled."))
			return
		}
		row.Input = nil
		row.OccurrenceID = reply.Occurrence.ID
		row.RunID = reply.Occurrence.RunID
		row.State = reply.Occurrence.State
		if !save() {
			return
		}
	}
	if len(b.Rows) >= inputRecordLimit {
		problem(errors.New("This subscription reached its retained revision limit. Review a new subscription to continue."))
		return
	}
	for _, record := range saved.Document.Records {
		if !subscriptionEligible(*b, saved.Document, record) {
			continue
		}
		rows, e := prepareInputSelection(saved.Document, []string{record.ID})
		if e != nil {
			problem(e)
			return
		}
		if len(rows) != 1 || len(rows[0].Issues) != 0 {
			problem(errors.New("An eligible record has conversion issues. Review its dialect."))
			return
		}
		if e = s.prepareReviewedInputsForRelease(ctx, saved.Document, rows, b.Release); e != nil {
			problem(e)
			return
		}
		// Validate the same executable projection used by selected-record review.
		if _, e = s.inputRunner(ctx, "inputs/preview", rows[0].Input, ""); e != nil {
			problem(e)
			return
		}
		b.Rows = append(b.Rows, inputEvaluationRow{RecordID: record.ID, RecordDigest: inputRecordDigest(record), Input: rows[0].Input, State: "pending", EventID: subscriptionIdentity(*b, record), EventAt: time.Now().UTC().Format(time.RFC3339Nano)})
		b.State = "watching"
		b.Problem = ""
		save()
		return
	}
	b.State = "watching"
	a.Failures = 0
	a.RetryAt = ""
	b.Problem = ""
	save()
}

// The reader has no write arguments: receipt recovery cannot resubmit delivery,
// acquire sources, or enable a trigger. A missing acknowledgement stays unknown.
func recoverPausedSubscriptionDelivery(ctx context.Context, b inputEvaluation, row *inputEvaluationRow, read func(context.Context, string) (json.RawMessage, error)) (bool, error) {
	raw, err := read(ctx, "triggers/"+b.Subscription.TriggerID+"/deliveries/"+row.EventID)
	var refusal *inputRunnerError
	if errors.As(err, &refusal) && refusal.Status == 404 {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var reply struct {
		Occurrence struct {
			ID, JobID, ReleaseID, TriggerID, EventID, State, RunID, Reason string
		} `json:"occurrence"`
	}
	if json.Unmarshal(raw, &reply) != nil {
		return false, errors.New("The retained delivery could not be read.")
	}
	o := reply.Occurrence
	if !strings.HasPrefix(o.ID, "occ_") || o.JobID != b.JobID || o.ReleaseID != b.ReleaseID || o.TriggerID != b.Subscription.TriggerID || o.EventID != row.EventID || !contains([]string{"accepted", "preparing", "ready", "submitted", "skipped", "failed", "expired", "needs-attention", "cancelled"}, o.State) {
		return false, errors.New("The retained delivery does not match the paused subscription.")
	}
	row.OccurrenceID, row.RunID, row.State, row.Problem = o.ID, o.RunID, o.State, o.Reason
	row.Input = nil
	return true, nil
}
