package desk

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func assistantPlanReferences(source []byte, additional []assistantPlanReference) ([]assistantPlanReference, error) {
	fields, err := assistantTestObject(source)
	if err != nil {
		return nil, err
	}
	refs := []assistantPlanReference{}
	for _, field := range []string{"pack", "graph", "input"} {
		if fields[field] == nil {
			continue
		}
		linked, err := assistantTestObject(fields[field])
		if err != nil {
			return nil, err
		}
		key, kind := "path", "file"
		if field == "input" {
			key, kind = "id", "dialect"
		}
		id, err := assistantTestString(linked[key])
		if err != nil {
			return nil, err
		}
		if field != "input" || id != "list" {
			refs = append(refs, assistantPlanReference{kind, id})
		}
	}
	refs = append(refs, additional...)
	unique, seen := []assistantPlanReference{}, map[string]bool{}
	for _, ref := range refs {
		if !validAssistantPlanReference(ref) {
			return nil, errAssistantRunInvalid
		}
		key := ref.Kind + "\x00" + ref.ID
		if !seen[key] {
			unique = append(unique, ref)
			seen[key] = true
		}
	}
	if len(unique) > 8 {
		return nil, errAssistantRunInvalid
	}
	return unique, nil
}
func (s *Server) assistantPlanView(ctx context.Context, refs []assistantPlanReference) (inputPreparationRead, error) {
	for _, ref := range refs {
		if ref.Kind == "dialect" {
			return s.preparationRepository().TryRead(ctx)
		}
	}
	return nil, nil
}

// Use the existing rooted file reader and consistent Dialect collection owner.
// A reference grants neither file contents nor an alternate path to a tool.
func (s *Server) resolveAssistantPlanReferences(refs []assistantPlanReference, view inputPreparationReader) ([]chatPlanReference, error) {
	resolved := []chatPlanReference{}
	for _, ref := range refs {
		if !validAssistantPlanReference(ref) {
			return nil, errAssistantRunInvalid
		}
		var revision string
		if ref.Kind == "file" {
			if err := s.refuseSymlinkedPath(ref.ID); err != nil {
				return nil, err
			}
			data, _, err := s.readThroughRoot(ref.ID)
			if err != nil {
				return nil, err
			}
			revision = digestOf(data)
		} else {
			if view == nil {
				return nil, errAssistantRunScope
			}
			current, err := view.Get(ref.ID)
			if err != nil {
				return nil, err
			}
			revision = current.SHA256
		}
		if !assistantRunDigest(revision) {
			return nil, errAssistantRunInvalid
		}
		resolved = append(resolved, chatPlanReference{ref.Kind, ref.ID, revision})
	}
	return resolved, nil
}

var testHookAssistantPlanObserved func()

// Notification only, while the human acceptance owner holds its final view.
var testHookAssistantPlanAcceptanceCommit func()

func (e *assistantRunExecution) proposeAssistantPlan(ctx context.Context, a assistantPlanArguments, root bool) (runcontext.ToolResult, *assistantDomainOutput, error) {
	var empty runcontext.ToolResult
	meta, ok := ctx.Value(assistantPlanPreparedKey{}).(*assistantPlanMetadata)
	if !root || e.record.Binding.Phase != "plan" || !ok || meta == nil {
		return empty, nil, errAssistantRunScope
	}
	refs, err := assistantPlanReferences(e.source.Original, a.References)
	if err != nil {
		return empty, nil, err
	}
	s := e.service.server
	s.writes.Lock()
	defer s.writes.Unlock()
	view, err := s.assistantPlanView(ctx, refs)
	if err != nil {
		return empty, nil, err
	}
	if view != nil {
		defer view.Close()
	}
	// Recheck after the real write/collection waits, including current Plan
	// state, session, Policy epoch, selected connection and exact source CAS.
	if err = e.Authorize(ctx); err != nil {
		return empty, nil, err
	}
	resolved, err := s.resolveAssistantPlanReferences(refs, view)
	if err != nil {
		return empty, nil, err
	}
	if err = e.Authorize(ctx); err != nil {
		return empty, nil, err
	}
	plan := chatPlan{ID: meta.ID, Revision: meta.Revision, Objective: a.Objective, Steps: a.Steps, Questions: a.Questions, References: resolved, Status: "proposed", CreatedAt: meta.CreatedAt}
	result, err := assistantPlanResult(plan)
	return result, &assistantDomainOutput{Kind: "plan", Plan: &plan}, err
}

type assistantPlanPreparedKey struct{}

func (s *Server) checkAssistantPlanProjection(source []byte, run assistantRunRecord, view inputPreparationReader) error {
	for _, out := range run.DomainOutputs {
		if out.Kind != "plan" {
			continue
		}
		op := assistantDialectOperation(run, out.OperationID)
		var in assistantHostIntent
		if op == nil || out.Plan == nil || readAssistantHostReceipt(op.Intent, &in) != nil {
			return errAssistantRunInvalid
		}
		a, err := readAssistantPlanArguments(in.Arguments)
		if err != nil {
			return err
		}
		refs, err := assistantPlanReferences(source, a.References)
		if err != nil {
			return err
		}
		current, err := s.resolveAssistantPlanReferences(refs, view)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, out.Plan.References) {
			return errAssistantRunConflict
		}
	}
	return nil
}

// This is called only by existing human conversation saves while their normal
// write/private CAS locks are held. Models/children receive no acceptance API.
func (s *Server) checkAssistantPlanAcceptance(r *http.Request, previous, next []byte) error {
	view, err := s.holdAssistantPlanAcceptance(r, previous, next)
	if view != nil {
		view.Close()
	}
	return err
}

// The final conversation owner retains this nonblocking collection read through
// rename/readback. No model callback receives it and no private lock waits on it.
func (s *Server) holdAssistantPlanAcceptance(r *http.Request, previous, next []byte) (inputPreparationRead, error) {
	var old, candidate struct {
		Chats []struct {
			ID    string     `json:"id"`
			Plans []chatPlan `json:"plans"`
		} `json:"chats"`
	}
	if json.Unmarshal(previous, &old) != nil || json.Unmarshal(next, &candidate) != nil {
		return nil, errAssistantRunInvalid
	}
	before := map[string][]chatPlan{}
	for _, chat := range old.Chats {
		before[chat.ID] = chat.Plans
	}
	refs := []assistantPlanReference{}
	for _, chat := range candidate.Chats {
		if len(chat.Plans) == 0 || reflect.DeepEqual(before[chat.ID], chat.Plans) {
			continue
		}
		plan := chat.Plans[len(chat.Plans)-1]
		if plan.Status != "accepted" && plan.Status != "started" {
			continue
		}
		for _, ref := range plan.References {
			refs = append(refs, assistantPlanReference{ref.Kind, ref.ID})
		}
	}
	view, err := s.assistantPlanView(r.Context(), refs)
	if err != nil {
		return nil, err
	}
	if err = s.checkAssistantPlanAcceptanceUsingView(r, previous, next, view); err != nil {
		if view != nil {
			view.Close()
		}
		return nil, err
	}
	return view, nil
}
func (s *Server) checkAssistantPlanAcceptanceUsingView(r *http.Request, previous, next []byte, view inputPreparationReader) error {
	var old, candidate conversationDocument
	if json.Unmarshal(previous, &old) != nil || json.Unmarshal(next, &candidate) != nil {
		return errAssistantRunInvalid
	}
	byID := map[string]json.RawMessage{}
	for _, raw := range old.Chats {
		var chat struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &chat) != nil || byID[chat.ID] != nil {
			return errAssistantRunInvalid
		}
		byID[chat.ID] = raw
	}
	for _, raw := range candidate.Chats {
		var chat struct {
			ID          string     `json:"id"`
			Interaction string     `json:"interaction"`
			Plans       []chatPlan `json:"plans"`
		}
		if json.Unmarshal(raw, &chat) != nil {
			return errAssistantRunInvalid
		}
		if len(chat.Plans) == 0 {
			continue
		}
		plan := chat.Plans[len(chat.Plans)-1]
		if plan.Status != "accepted" && plan.Status != "started" {
			continue
		}
		priorRaw := byID[chat.ID]
		priorPlans, err := assistantPlanSource(priorRaw)
		if err != nil || len(priorPlans) != len(chat.Plans) || len(priorPlans) == 0 {
			return errAssistantRunConflict
		}
		prior := priorPlans[len(priorPlans)-1]
		if reflect.DeepEqual(priorPlans, chat.Plans) {
			continue // ordinary autosave of an already accepted snapshot
		}
		phaseFields, err := assistantTestObject(priorRaw)
		if err != nil {
			return err
		}
		phase, err := assistantChatPhase(phaseFields["interaction"])
		if err != nil || chat.Interaction != "work" || len(plan.Questions) != 0 || prior.ID != plan.ID || (prior.Status == "proposed" && (phase != "plan" || plan.Status != "accepted")) || (prior.Status != "proposed" && (prior.Status != "accepted" || plan.Status != "started" || prior.AcceptedAt != plan.AcceptedAt)) {
			return errAssistantRunConflict
		}
		nextFields, err := assistantTestObject(raw)
		if err != nil {
			return err
		}
		for _, key := range []string{"pack", "graph", "input"} {
			before, after := phaseFields[key], nextFields[key]
			if (before == nil) != (after == nil) || before != nil && !sameDialectJSON(before, after) {
				return errAssistantRunConflict
			}
		}
		expected := append([]chatPlan(nil), priorPlans...)
		expected[len(expected)-1].Status, expected[len(expected)-1].AcceptedAt = plan.Status, plan.AcceptedAt
		if !reflect.DeepEqual(expected, chat.Plans) {
			return errAssistantRunConflict
		}
		if err := s.checkCurrentAssistantPlanUsingView(raw, plan, view); err != nil {
			return err
		}
	}
	return r.Context().Err()
}
func (s *Server) checkCurrentAssistantPlan(source []byte, plan chatPlan) error {
	refs := []assistantPlanReference{}
	for _, ref := range plan.References {
		refs = append(refs, assistantPlanReference{ref.Kind, ref.ID})
	}
	view, err := s.assistantPlanView(context.Background(), refs)
	if err != nil {
		return err
	}
	if view != nil {
		defer view.Close()
	}
	return s.checkCurrentAssistantPlanUsingView(source, plan, view)
}
func (s *Server) checkCurrentAssistantPlanUsingView(source []byte, plan chatPlan, view inputPreparationReader) error {
	if !validAssistantPlanReferences(plan.References) {
		return errAssistantRunInvalid
	}
	refs := []assistantPlanReference{}
	for _, ref := range plan.References {
		refs = append(refs, assistantPlanReference{ref.Kind, ref.ID})
	}
	linked, err := assistantPlanReferences(source, nil)
	if err != nil {
		return err
	}
	for _, ref := range linked {
		found := false
		for _, held := range refs {
			found = found || held == ref
		}
		if !found {
			return errAssistantRunConflict
		}
	}
	current, err := s.resolveAssistantPlanReferences(refs, view)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, plan.References) {
		return errAssistantRunConflict
	}
	return nil
}
