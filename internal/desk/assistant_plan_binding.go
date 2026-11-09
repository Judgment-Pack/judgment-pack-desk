package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Immutable host-selected data; capability closures stay on assistantRunSource.
// The original accepted array is never replaced with its validated projection.
type assistantRunPlan struct {
	Snapshot []byte
	Locators []assistantPlanReference
}

// Called only from the existing lock-held saved-chat source resolver. Briefs,
// diagnostics and independent authoring purposes do not acquire a chat Plan.
func (s *Server) selectAssistantRunPlanLocked(r *http.Request, source *assistantRunSource) error {
	if source.Start.Purpose != "chat" || source.Binding.Phase != "work" {
		return nil
	}
	plans, err := assistantPlanSource(source.Original)
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		return nil
	}
	plan := plans[len(plans)-1]
	if plan.Status != "accepted" && plan.Status != "started" {
		return nil
	}
	if len(plan.Questions) != 0 {
		return errAssistantRunScope
	}
	locators, err := s.assistantPlanLocatorsLocked(source.Original, plan, &source.Binding)
	if err != nil {
		return err
	}
	view, err := s.assistantPlanView(r.Context(), locators)
	if err != nil {
		return err
	}
	if view != nil {
		defer view.Close()
	}
	if err = s.checkCurrentAssistantPlanUsingView(source.Original, plan, view); err != nil {
		return err
	}
	fields, err := assistantTestObject(source.Original)
	if err != nil {
		return err
	}
	rows, err := assistantChatArray(fields["plans"])
	if err != nil {
		return err
	}
	source.Plan = &assistantRunPlan{Snapshot: bytes.Clone(rows[len(rows)-1]), Locators: append([]assistantPlanReference{}, locators...)}
	// Capture only immutable host input. Fresh resolution repeats the existing
	// source/session/project/connection/Policy guards under fresh private custody.
	held := *source
	held.PlanResolver = nil
	held.Original, held.Request = bytes.Clone(source.Original), bytes.Clone(source.Request)
	held.Plan = &assistantRunPlan{Snapshot: bytes.Clone(source.Plan.Snapshot), Locators: append([]assistantPlanReference{}, locators...)}
	resolve := func(ctx context.Context, kind, id string, legacy *runcontext.PlanReference) (runcontext.ResolvedPlanReference, error) {
		var empty runcontext.ResolvedPlanReference
		lock, err := s.privateDataLockContext(ctx, true)
		if err != nil {
			return empty, err
		}
		defer lock.Close()
		request := r.Clone(ctx)
		identity, _, err := s.assistantRunIdentity(request)
		if err != nil {
			return empty, err
		}
		fresh, err := s.resolveAssistantRun(request, held.Start, identity, held.Binding.Project)
		if err != nil {
			return empty, err
		}
		if !assistantRunFreshSourceEqual(held, fresh) {
			return empty, errAssistantRunScope
		}
		index := -1
		for i, ref := range plan.References {
			if ref.Kind == kind && ref.ID == id && ((legacy == nil && ref.Revision == "") || (legacy != nil && ref.Revision != "" && ref.Revision == legacy.LegacyRevision)) {
				if index >= 0 {
					return empty, errAssistantRunInvalid
				}
				index = i
			}
		}
		if index < 0 {
			return empty, &runcontext.LegacyPlanReferenceUnresolved{}
		}
		view, err := s.assistantPlanView(ctx, held.Plan.Locators)
		if err != nil {
			return empty, err
		}
		if view != nil {
			defer view.Close()
		}
		live, err := s.resolveAssistantPlanReferences(held.Plan.Locators, view)
		if err != nil {
			return empty, err
		}
		ref := plan.References[index]
		if legacy == nil {
			return runcontext.ResolvedPlanReference{Kind: ref.Kind, ID: ref.ID, Digest: live[index].Revision}, ctx.Err()
		}
		currentKind := "artifact"
		if ref.Kind == "dialect" {
			currentKind = "dialect"
		}
		// Stable Plan-scoped legacy handles exist only in validatedReferences.
		raw, _ := json.Marshal([]string{held.Binding.Project, held.Binding.Owner.ID, plan.ID, strconv.Itoa(plan.Revision), strconv.Itoa(index), ref.Kind, ref.ID})
		return runcontext.ResolvedPlanReference{Kind: currentKind, ID: "plan-legacy-" + digestOf(raw), Digest: live[index].Revision}, ctx.Err()
	}
	source.PlanResolver = &runcontext.PlanResolver{
		Resolve: func(ctx context.Context, kind, id string) (runcontext.ResolvedPlanReference, error) {
			return resolve(ctx, kind, id, nil)
		},
		Legacy: func(ctx context.Context, ref runcontext.PlanReference) (runcontext.ResolvedPlanReference, error) {
			return resolve(ctx, ref.Kind, ref.ID, &ref)
		},
	}
	return nil
}

func bindAssistantRunPlan(source assistantRunSource, input *runcontext.Input) error {
	if source.Plan == nil {
		if input.Execution != nil && assistantCheckpointAcceptedPlan(input.Execution) != nil {
			return errAssistantRunScope
		}
		return nil
	}
	if source.Start.Purpose != "chat" || source.Binding.Phase != "work" || source.PlanResolver == nil {
		return errAssistantRunScope
	}
	var plan chatPlan
	if json.Unmarshal(source.Plan.Snapshot, &plan) != nil || (plan.Status != "accepted" && plan.Status != "started") || len(plan.Questions) != 0 {
		return errAssistantRunInvalid
	}
	fields, err := assistantTestObject(source.Original)
	if err != nil {
		return err
	}
	rows, err := assistantChatArray(fields["plans"])
	if err != nil || len(rows) == 0 || !bytes.Equal(rows[len(rows)-1], source.Plan.Snapshot) {
		return errAssistantRunScope
	}
	value, err := runcontext.ParseJSON(source.Plan.Snapshot)
	if err != nil {
		return err
	}
	if input.Execution != nil && !assistantAcceptedPlanMatches(value, input.Execution) {
		// Retained callback records predate accepted-lineage seeding. Preserve
		// their existing progress without fabricating lineage or durable proof.
		if assistantCheckpointAcceptedPlan(input.Execution) != nil || input.RunStore != nil || input.Persistence != nil || input.Execution.Durable != nil {
			return errAssistantRunScope
		}
	}
	input.Plan, input.PlanResolver = value, source.PlanResolver
	return nil
}

func assistantCheckpointAcceptedPlan(cp *contract.Checkpoint) *runcontext.Value {
	if cp == nil || cp.PlanState == nil {
		return nil
	}
	raw, _ := json.Marshal(cp.PlanState)
	value, err := runcontext.ParseJSON(raw)
	if err != nil {
		return nil
	}
	return value.Get("accepted")
}

func assistantAcceptedPlanMatches(plan *runcontext.Value, cp *contract.Checkpoint) bool {
	accepted := assistantCheckpointAcceptedPlan(cp)
	if accepted == nil {
		return false
	}
	if accepted.Get("id").Str() != plan.Get("id").Str() || runcontext.CanonicalJSON(accepted.Get("sourceRevision")) != runcontext.CanonicalJSON(plan.Get("revision")) || accepted.Get("acceptedAt").Str() != plan.Get("acceptedAt").Str() {
		return false
	}
	for _, key := range []string{"objective", "steps", "references"} {
		if runcontext.CanonicalJSON(accepted.Get(key)) != runcontext.CanonicalJSON(plan.Get(key)) {
			return false
		}
	}
	return true
}

// Explicit resume/approval comparisons have a persisted binding/checkpoint,
// rather than another fully resolved source with host capabilities.
func assistantRunSourceMatchesRecord(source assistantRunSource, run assistantRunRecord) bool {
	if !reflect.DeepEqual(source.Binding, run.Binding) {
		return false
	}
	if source.Plan == nil {
		return assistantCheckpointAcceptedPlan(run.Checkpoint) == nil
	}
	if assistantCheckpointAcceptedPlan(run.Checkpoint) == nil {
		return !run.CanonicalRequired && !assistantCanonicalActive(run) && (run.Checkpoint == nil || run.Checkpoint.Durable == nil)
	}
	value, err := runcontext.ParseJSON(source.Plan.Snapshot)
	return err == nil && assistantAcceptedPlanMatches(value, run.Checkpoint)
}
