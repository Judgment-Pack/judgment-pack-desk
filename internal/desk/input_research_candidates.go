package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const researchPageConcurrency = 3
const researchCandidateLimit = 100

// Candidates are unverified leads, not saved records or qualification decisions.
// A queue entry identifies one retained URL; distinct URLs for the same entity
// may supply different evidence. Record identity/deduplication remains separate.
type inputResearchCandidate struct {
	TaskID   string `json:"taskId,omitempty"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	State    string `json:"state"`
	SourceID string `json:"sourceId,omitempty"`
}

func validateResearchCandidates(run inputResearchRun) error {
	if len(run.Candidates) > researchCandidateLimit {
		return errors.New("Research candidate limit exceeded.")
	}
	for _, a := range run.Attempts {
		if a.ReservedBytes < 0 || a.ReservedBytes > inputResearchSourceProofLimit+(2<<20) || a.ReservedBytes > 0 && (a.Kind == "model" || a.State != "started") {
			return errors.New("Invalid research storage reservation.")
		}
	}
	seen := map[string]bool{}
	for _, c := range run.Candidates {
		normalized, err := inputResearchURL(c.URL)
		if err != nil || normalized != c.URL || c.ID != "candidate_"+digestOf([]byte(c.URL)) || seen[c.ID] || strings.TrimSpace(c.Name) == "" || len(c.Name) > 512 || !contains([]string{"queued", "reading", "ready", "reviewed", "unavailable", "uncertain"}, c.State) {
			return errors.New("Invalid research candidate.")
		}
		seen[c.ID] = true
	}
	return nil
}
func queueResearchCandidates(run *inputResearchRun, raw json.RawMessage) error {
	var request struct {
		Candidates []struct {
			Name   string `json:"name"`
			URLRef string `json:"urlRef"`
		} `json:"candidates"`
	}
	if decodeDataJSON(raw, &request) != nil || len(request.Candidates) == 0 || len(request.Candidates) > 20 {
		return errors.New("Queue 1 to 20 relevant candidates using names and urlRef values from availableURLs.")
	}
	next := append([]inputResearchCandidate(nil), run.Candidates...)
	seen := map[string]bool{}
	for _, c := range next {
		seen[c.URL] = true
	}
	for _, c := range request.Candidates {
		if strings.TrimSpace(c.Name) == "" || len(c.Name) > 512 || c.URLRef == "" {
			return errors.New("Each candidate needs a short name and a retained URL reference.")
		}
		raw, _ := json.Marshal(map[string]string{"urlRef": c.URLRef})
		_, args, err := researchSourceArguments(*run, inputResearchAction{Tool: "read", Arguments: raw})
		if err != nil {
			return err
		}
		url, _ := researchRequestCoordinates("page", args)
		if seen[url] {
			continue
		}
		// Completed and uncertain reads are never acquired again under a new name.
		key := digestOf(append([]byte("page\n"), args...))
		used := false
		for _, a := range run.Attempts {
			if a.Kind == "page" && a.Digest == key {
				used = true
				break
			}
		}
		if used {
			continue
		}
		seen[url] = true
		next = append(next, inputResearchCandidate{ID: "candidate_" + digestOf([]byte(url)), Name: strings.TrimSpace(c.Name), URL: url, State: "queued"})
	}
	if len(next) > researchCandidateLimit {
		return errors.New("The saved candidate queue is full. Use retained evidence.")
	}
	if len(next) == len(run.Candidates) {
		return errors.New("These candidate URLs were already queued or read. Use their retained evidence or choose new leads.")
	}
	run.Candidates = next
	return nil
}
func reviewResearchCandidates(run *inputResearchRun) {
	for i := range run.Candidates {
		if run.Candidates[i].State == "ready" {
			run.Candidates[i].State = "reviewed"
		}
	}
}
func researchCandidateContext(run inputResearchRun) []map[string]string {
	refs := map[string]string{}
	for _, ref := range researchURLReferences(run) {
		refs[ref.URL] = ref.Ref
	}
	rows := []map[string]string{}
	for _, c := range run.Candidates {
		rows = append(rows, map[string]string{"id": c.ID, "name": c.Name, "urlRef": refs[c.URL], "state": c.State, "sourceId": c.SourceID})
	}
	return rows
}
func retainResearchSource(run *inputResearchRun, source inputResearchSource) error {
	for _, saved := range run.Sources {
		if saved.ID == source.ID {
			return nil
		}
	}
	if len(run.Sources) >= 130 {
		return errors.New("Research source limit reached.")
	}
	run.Sources = append(run.Sources, source)
	if source.Kind == "page" {
		run.Phase = "save"
		run.PhaseReason = "Pages were retained. Save all supported candidates from these pages now, leaving unsupported fields missing, or finish with a reason explaining why the pages are insufficient. Queued candidates will be read after this checkpoint."
	}
	return nil
}

// Read one small batch, then yield to the model to save records. Search and
// model calls remain serial. Each read has its own journal/proof and atomic
// request+byte reservation. Join every call before changing the run's phase.
func readResearchCandidateBatch(ctx context.Context, root *os.Root, ops inputResearchOperations) error {
	var batch []inputResearchCandidate
	run, err := changeInputResearch(root, func(r *inputResearchRun) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if r.StopRequested || r.State != "running" {
			return errors.New("Research stopped before reading candidates.")
		}
		if r.Phase == "save" || r.Phase == "finalize" {
			return nil
		}
		remaining := r.Recipe.Limits.Records - len(r.Records)
		if remaining <= 0 {
			return nil
		}
		pages := researchApprovedLimits(*r).Pages
		for _, a := range r.Attempts {
			if a.Kind == "page" && a.State != "not-started" {
				pages--
			}
		}
		bytes := (inputResearchRetainedByteLimit - r.RetrievedBytes - researchReservedBytes(*r)) / (inputResearchSourceProofLimit + (2 << 20))
		count := min(researchPageConcurrency, remaining, pages, bytes, 250-len(r.Attempts))
		for i := range r.Candidates {
			c := &r.Candidates[i]
			if c.State != "queued" {
				continue
			}
			if count <= 0 {
				if len(batch) == 0 {
					r.Phase = "finalize"
					r.PhaseReason = "The saved page-read or storage limit was reached. Save from retained evidence."
				}
				break
			}
			c.State = "reading"
			batch = append(batch, *c)
			count--
		}
		return nil
	})
	if err != nil || len(batch) == 0 {
		return err
	}
	if err = validateResearchSearchForPhase(ctx, run, ops); err != nil {
		return err
	}
	batchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, len(batch))
	for _, c := range batch {
		go func(c inputResearchCandidate) {
			args, _ := json.Marshal(map[string]string{"url": c.URL})
			raw, callErr := researchAttempt(batchCtx, root, "page", args, func(call context.Context, current inputResearchRun, a inputResearchAttempt) (json.RawMessage, error) {
				source, e := ops.source(call, root, current, a, args)
				if e != nil {
					return nil, e
				}
				return json.Marshal(source)
			})
			state, sourceID := "uncertain", ""
			var source inputResearchSource
			if callErr == nil {
				if json.Unmarshal(raw, &source) != nil {
					callErr = errors.New("Saved source metadata could not be read.")
				} else {
					state = "ready"
					sourceID = source.ID
				}
			} else {
				if ok, e := continueResearchSource(batchCtx, root, callErr); ok {
					state = "unavailable"
					callErr = nil
				} else {
					callErr = e
				}
			}
			_, saveErr := changeInputResearch(root, func(r *inputResearchRun) error {
				if state == "ready" {
					if err := retainResearchSource(r, source); err != nil {
						return err
					}
				}
				for i := range r.Candidates {
					if r.Candidates[i].ID == c.ID {
						r.Candidates[i].State = state
						r.Candidates[i].SourceID = sourceID
						return nil
					}
				}
				return errors.New("Saved research candidate was not found.")
			})
			if saveErr != nil {
				callErr = fmt.Errorf("Candidate progress could not be saved: %w", saveErr)
			}
			// Send the original terminal error before cancelling siblings so a generic
			// cancellation cannot hide an access-policy, verification or storage failure.
			results <- callErr
			if callErr != nil {
				cancel()
			}
		}(c)
	}
	var first error
	for range batch {
		if e := <-results; e != nil && first == nil {
			first = e
		}
	}
	return first
}

func readResearchCandidates(ctx context.Context, root *os.Root, ops inputResearchOperations) error {
	current, err := readInputResearch(root)
	if err != nil {
		return err
	}
	if delegatedResearch(current) {
		return runResearchCandidateTasks(ctx, root, ops)
	}
	for {
		if err := readResearchCandidateBatch(ctx, root, ops); err != nil {
			return err
		}
		run, err := readInputResearch(root)
		if err != nil {
			return err
		}
		if run.Phase == "save" || run.Phase == "finalize" || len(run.Records) >= run.Recipe.Limits.Records {
			return nil
		}
		queued := false
		for _, c := range run.Candidates {
			if c.State == "queued" {
				queued = true
				break
			}
		}
		if !queued {
			return nil
		}
	}
}
