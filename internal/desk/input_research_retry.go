package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/aifailure"
	"math/rand/v2"
	"os"
	"strings"
	"time"
)

type inputResearchPendingRetry struct {
	AfterAttempt string `json:"afterAttempt"`
	At           string `json:"at"`
	Number       int    `json:"number"`
	MaxAttempts  int    `json:"maxAttempts"`
	ProblemCode  string `json:"problemCode"`
}

var errResearchRequestLimit = errors.New("The approved research request limit was reached.")

func researchRequestBudget(run *inputResearchRun, kind string) error {
	limits := researchApprovedLimits(*run)
	limit := limits.ModelCalls
	if kind == "search" {
		limit = limits.Searches
	}
	if kind == "page" {
		limit = limits.Pages
	}
	count := 0
	for _, a := range run.Attempts {
		if a.Kind == kind && a.State != "not-started" {
			count++
		}
	}
	if count >= limit || len(run.Attempts) >= 250 {
		return errResearchRequestLimit
	}
	return nil
}

func researchRetryAllowed(recipe inputResearchRecipe, a inputResearchAttempt) bool {
	p := recipe.Retries
	if p == nil || p.Mode != "temporary" || a.Kind != "search" || !a.AcquisitionRefusal {
		return false
	}
	// Older Gateways fold permanent and transient failures into one code.
	// Only an explicit temporary status from acquisition authorizes replay;
	// ambiguous transport, authentication and post-acquisition failures do not.
	return a.ProblemCode == "search-temporary" || a.ProblemCode == "rate-limited" || (a.ProblemCode == "search-timeout" && p.SearchTimeouts)
}
func researchAttemptNumber(a inputResearchAttempt) int { return max(1, a.Number) }
func withoutNoResearchRetry(message string) string {
	return strings.TrimSuffix(message, " No automatic retry was made.")
}
func researchPendingRetries(run *inputResearchRun) []inputResearchPendingRetry {
	if len(run.PendingRetries) > 0 {
		return append([]inputResearchPendingRetry(nil), run.PendingRetries...)
	}
	if run.PendingRetry != nil {
		return []inputResearchPendingRetry{*run.PendingRetry}
	}
	return nil
}
func setResearchPendingRetries(run *inputResearchRun, pending []inputResearchPendingRetry) {
	run.PendingRetries = pending
	run.PendingRetry = nil
	for i := range pending {
		if run.PendingRetry == nil || pending[i].At < run.PendingRetry.At {
			p := pending[i]
			run.PendingRetry = &p
		}
	}
}
func removeResearchRetry(run *inputResearchRun, after, note string) {
	pending := []inputResearchPendingRetry{}
	for _, p := range researchPendingRetries(run) {
		if after != "" && p.AfterAttempt != after {
			pending = append(pending, p)
			continue
		}
		if note != "" {
			if a, e := researchAttemptByID(run, p.AfterAttempt); e == nil {
				a.RetryNote = note
			}
		}
	}
	setResearchPendingRetries(run, pending)
}
func cancelResearchRetry(run *inputResearchRun, note string) { removeResearchRetry(run, "", note) }

func beginResearchRetry(run *inputResearchRun, kind string, args json.RawMessage, after string) (inputResearchAttempt, error) {
	if run.StopRequested || run.State != "running" {
		return inputResearchAttempt{}, errors.New("Research is no longer running.")
	}
	var pending *inputResearchPendingRetry
	for _, p := range researchPendingRetries(run) {
		if p.AfterAttempt == after {
			copy := p
			pending = &copy
			break
		}
	}
	if pending == nil || pending.AfterAttempt != after || len(run.Attempts) == 0 {
		return inputResearchAttempt{}, errors.New("The scheduled retry is no longer available.")
	}
	previous, err := researchAttemptByID(run, after)
	if err != nil {
		return inputResearchAttempt{}, err
	}
	key := digestOf(append([]byte(kind+"\n"), args...))
	if previous.ID != after || previous.Kind != kind || previous.Digest != key || previous.State != "uncertain" || previous.RetriedBy != "" || !researchRetryAllowed(run.Recipe, *previous) || pending.Number != researchAttemptNumber(*previous)+1 || pending.Number > run.Recipe.Retries.MaxRetries+1 {
		return inputResearchAttempt{}, errors.New("The saved retry policy does not authorize this attempt.")
	}
	if err := researchRequestBudget(run, kind); err != nil {
		return inputResearchAttempt{}, err
	}
	next := inputResearchAttempt{TaskID: previous.TaskID, ID: fmt.Sprintf("attempt_%s_%d", key, pending.Number), Kind: kind, Digest: key, State: "started", Number: pending.Number, RetryOf: previous.ID, URL: previous.URL, Query: previous.Query, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := reserveResearchSource(run, &next); err != nil {
		return inputResearchAttempt{}, err
	}
	previous.RetriedBy = next.ID
	previous.RetryNote = "This operation was retried under the saved recipe policy."
	run.Attempts = append(run.Attempts, next)
	removeResearchRetry(run, after, "")
	return next, nil
}

func waitResearchRetry(ctx context.Context, at time.Time) error {
	timer := time.NewTimer(max(time.Until(at), 0))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// researchAcquisitionRefused is returned, with the retained response, by a
// source call whose acquisition completed and which the caller's own check then
// refused. The attempt is finished "refused" under code, its response kept for
// audit and never used as a source; it is not continued or retried.
type researchAcquisitionRefused struct {
	code  string
	cause error
}

func (e *researchAcquisitionRefused) Error() string { return e.cause.Error() }
func (e *researchAcquisitionRefused) Unwrap() error { return e.cause }

func researchAttempt(ctx context.Context, root researchRunStore, kind string, args json.RawMessage, call func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error)) (json.RawMessage, error) {
	return researchAttemptWithWait(ctx, root, kind, args, call, waitResearchRetry)
}

// Only this live invocation may consume its scheduled retry. Process recovery
// interrupts the run and clears pending work instead of replaying requests.
func researchAttemptWithWait(ctx context.Context, root researchRunStore, kind string, args json.RawMessage, call func(context.Context, inputResearchRun, inputResearchAttempt) (json.RawMessage, error), wait func(context.Context, time.Time) error) (json.RawMessage, error) {
	after := ""
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var attempt inputResearchAttempt
		var cached bool
		run, e := root.change(func(r *inputResearchRun) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var err error
			if after != "" {
				if taskID := researchTaskID(ctx); taskID != "" {
					if err = admitResearchTaskOperation(r, taskID, kind); err != nil {
						return err
					}
				}
				attempt, err = beginResearchRetry(r, kind, args, after)
			} else {
				if taskID := researchTaskID(ctx); taskID != "" {
					if err = admitResearchTaskOperation(r, taskID, kind); err != nil {
						return err
					}
				}
				attempt, cached, err = beginResearchAttempt(r, kind, args)
				if err == nil && !cached && researchTaskID(ctx) != "" {
					attempt.TaskID = researchTaskID(ctx)
					r.Attempts[len(r.Attempts)-1].TaskID = attempt.TaskID
					if kind == "model" {
						r.Step++
					}
				}
			}
			return err
		})
		if e != nil {
			if errors.Is(e, errPortableResearchJournalSize) {
				_, saveErr := root.change(func(r *inputResearchRun) error {
					if r.State == "running" {
						if r.StopRequested {
							r.State = "stopped"
						} else {
							r.State = "partial"
						}
						r.Problem = errPortableResearchJournalSize.Error()
					}
					return nil
				})
				if saveErr != nil {
					return nil, saveErr
				}
			}
			if after != "" {
				_, saveErr := root.change(func(r *inputResearchRun) error {
					removeResearchRetry(r, after, "The retry could not be admitted. No further request was sent.")
					return nil
				})
				if saveErr != nil {
					return nil, saveErr
				}
			}
			return nil, e
		}
		if cached {
			return attempt.Result, nil
		}
		deadline := 180 * time.Second
		if kind == "model" {
			deadline = backgroundModelTimeout
		}
		callCtx, cancel := context.WithTimeout(ctx, deadline)
		raw, callErr := call(callCtx, run, attempt)
		cancel()
		problem := ""
		if callErr != nil {
			problem = callErr.Error()
		}
		if run.Portable != nil {
			problem = portableResearchProblemText(problem)
		}
		var refused *researchAcquisitionRefused
		if !errors.As(callErr, &refused) || kind == "model" || len(raw) == 0 {
			refused = nil
		}
		_, e = root.change(func(r *inputResearchRun) error {
			current, err := researchAttemptByID(r, attempt.ID)
			if err != nil {
				return err
			}
			size := len(raw)
			if kind != "model" && callErr == nil && r.Portable == nil {
				var source inputResearchSource
				if json.Unmarshal(raw, &source) != nil {
					return errors.New("Invalid source metadata.")
				}
				size += source.Bytes
			}
			if kind != "model" && callErr != nil && r.Portable == nil {
				// Acquisition can leave a proof even when extraction has no
				// usable text. Count those bytes before continuing to another
				// source, rather than allowing empty pages to evade the cap.
				retained, err := root.proofSize(attempt.ID)
				if err == nil {
					size += retained
				} else if !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			if len(raw) > 2<<20 || r.RetrievedBytes+researchReservedBytes(*r)-current.ReservedBytes+size > inputResearchRetainedByteLimit {
				return errors.New("Research response storage limit reached.")
			}
			r.RetrievedBytes += size
			if current.State == "waiting" {
				if callErr == nil {
					callErr = errors.New("The AI request was not admitted.")
				}
				if errors.Is(callErr, context.DeadlineExceeded) {
					callErr = errors.New("The AI connection stayed busy until the wait limit was reached. No AI request was sent. Try a new run when the other operation finishes.")
				} else if errors.Is(callErr, context.Canceled) {
					callErr = errors.New("Research stopped while waiting for the AI connection. No AI request was sent.")
				}
				problem = callErr.Error()
			}
			if refused != nil {
				return refuseResearchAttempt(r, attempt.ID, raw, refused.code, problem)
			}
			if err := finishResearchAttempt(r, attempt.ID, raw, problem); err != nil {
				return err
			}
			var modelFailure *aifailure.Failure
			if errors.As(callErr, &modelFailure) {
				current.Failure = modelFailure
				current.ProblemCode = modelFailure.Code
				current.Recovery = modelFailure.Recovery()
			}
			var refusal *researchSourceError
			if errors.As(callErr, &refusal) {
				a := current
				a.ProblemCode, a.Recovery, a.AcquisitionRefusal = refusal.code, refusal.recovery, refusal.acquisitionRefusal
			}
			return nil
		})
		if e != nil {
			return nil, errors.New("The response could not be saved. Its outcome is uncertain; no further request was made.")
		}
		if callErr == nil {
			return raw, nil
		}
		if refused != nil {
			return nil, callErr
		}

		var pending *inputResearchPendingRetry
		var finalized inputResearchAttempt
		terminal := callErr
		_, e = root.change(func(r *inputResearchRun) error {
			a, err := researchAttemptByID(r, attempt.ID)
			if err != nil {
				return err
			}
			finalized = *a
			number := researchAttemptNumber(*a)
			reason := ""
			switch {
			case r.StopRequested || ctx.Err() != nil:
				reason = "Research stopped. No further retry was made."
			case !researchRetryAllowed(r.Recipe, *a):
				if number == 1 {
					return nil
				}
				reason = "This failure is not eligible for automatic retry."
			case number > r.Recipe.Retries.MaxRetries:
				reason = "The saved retry limit was reached."
			case researchRequestBudget(r, kind) != nil:
				reason = "The saved request limit leaves no room for another retry."
			}
			if reason != "" {
				a.RetryNote = reason
				if number > 1 {
					a.Problem = withoutNoResearchRetry(a.Problem)
				}
				terminal = fmt.Errorf("%s %s Attempts made: %d.", withoutNoResearchRetry(a.Problem), reason, number)
				return nil
			}
			// Bounded exponential backoff with jitter (5–6s, 10–12s, 20–24s).
			delay := 5 * time.Second * time.Duration(1<<(number-1))
			delay += time.Duration(rand.Int64N(int64(delay/5) + 1))
			at := time.Now().UTC().Add(delay).Format(time.RFC3339Nano)
			a.Problem = withoutNoResearchRetry(a.Problem)
			a.RetryAt = at
			a.RetryNote = "A retry was scheduled under the saved recipe policy."
			pending = &inputResearchPendingRetry{AfterAttempt: a.ID, At: at, Number: number + 1, MaxAttempts: r.Recipe.Retries.MaxRetries + 1, ProblemCode: a.ProblemCode}
			list := researchPendingRetries(r)
			list = append(list, *pending)
			setResearchPendingRetries(r, list)
			return nil
		})
		if e != nil {
			return nil, errors.New("Retry information could not be saved. No further request was made.")
		}
		if pending == nil {
			if ctx.Err() == nil && researchContinuableAttempt(finalized) {
				return nil, &researchSourceGap{attemptID: finalized.ID, cause: terminal}
			}
			return nil, terminal
		}
		at, _ := time.Parse(time.RFC3339Nano, pending.At)
		if e = wait(ctx, at); e != nil {
			_, saveErr := root.change(func(r *inputResearchRun) error {
				removeResearchRetry(r, pending.AfterAttempt, "Research stopped before the scheduled retry. No further request was made.")
				return nil
			})
			if saveErr != nil {
				return nil, errors.New("Research stopped, but its retry status could not be saved. No further request was made.")
			}
			return nil, errors.New("Research stopped before the scheduled retry. No further request was made.")
		}
		after = pending.AfterAttempt
	}
}
