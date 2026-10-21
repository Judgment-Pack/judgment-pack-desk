package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

// Unknown outcomes close this execution after the exact failure receipt is
// observed. A failed create/readback never permits an automatic second create.
type assistantDialectRecipeUnknown struct{ cause error }

func (e *assistantDialectRecipeUnknown) Error() string {
	return "The Dialect draft outcome is unknown."
}
func (e *assistantDialectRecipeUnknown) Unwrap() error { return e.cause }

type assistantDialectRecipeCreate struct {
	dialectDraftRepository
	attempted bool
}

func (r *assistantDialectRecipeCreate) CompareAndSwap(ctx context.Context, d dialectDraftDocument, revision string) (dialectDraftReply, error) {
	r.attempted = true
	return r.dialectDraftRepository.CompareAndSwap(ctx, d, revision)
}
func assistantDialectRecipeFailure(err error) runcontext.ToolResult {
	var unknown *assistantDialectRecipeUnknown
	if errors.As(err, &unknown) {
		err = failure.Authored("storage", "The Dialect draft outcome is unknown. Inspect the retained draft before continuing; no automatic create or model retry is allowed.", "storage")
	}

	if _, ok := err.(*failure.Error); !ok {
		err = failure.Authored("storage", "The Dialect recipe tool could not complete. Inspect retained drafts before trying another proposal.", "desk")
	}
	return assistantHostFailure(err)
}
func (e *assistantRunExecution) assistantDialectRecipeTools() (*assistantHostRegistry, error) {
	descriptions := map[string]string{
		"get_dialect_recipe_instructions": "Read the Desk-owned portable Dialect recipe contract and field types. Does not call Runtime or begin research.",
		"list_dialects":                   "List this project's saved Dialects names and record counts, without record data or starting work.",
		"read_dialect_recipe":             "Read one current saved recipe for design context: intent, fields, limits and mapping, never credentials, provider bindings or records.",
		"propose_dialect_recipe":          "Retain an independent Dialect recipe draft for human review. Survives chat deletion; no preparation, search, research, qualification, publication or decision is started.",
	}
	schemas := assistantDialectRecipeSchemas()
	tools := []runcontext.HostTool{}
	for _, name := range assistantDialectRecipeNames {
		name := name
		tool := runcontext.HostTool{Name: name, Description: descriptions[name], InputSchema: schemas[name], Effect: "read", Delegation: "read"}
		if name == "get_dialect_recipe_instructions" {
			tool.ContextPolicy = &runcontext.ToolContextPolicy{Immutable: true, Reference: true}
		}
		if name == "propose_dialect_recipe" {
			tool.Effect = "propose"
			tool.Delegation = ""
		}
		tool.Execute = func(ctx context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
			a, err := readAssistantDialectRecipeArguments(name, raw)
			if err != nil {
				return assistantDialectRecipeFailure(err), nil
			}
			return e.invokeDomainHost(ctx, name, raw, func(ctx context.Context, root bool) (runcontext.ToolResult, *assistantGraphRead, *assistantDomainOutput, error) {
				if name != "propose_dialect_recipe" {
					value, err := e.readAssistantDialectRecipe(ctx, name, a)
					return value, nil, nil, err
				}
				return e.retainAssistantDialectRecipe(ctx, a.Content, root)
			})
		}
		tools = append(tools, tool)
	}
	return newAssistantHostRegistry(tools)
}

// The caller has already locked conversation writes and checked the exact
// current source. This callback takes no private-data lock: draft queue waits
// must not invert conversation/private/workflow/repository lock order.
func (e *assistantRunExecution) authorizeAssistantDialectRecipe(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.ctx.Err(); err != nil {
		return err
	}
	return e.service.server.authorizeAssistantRun(e.request.WithContext(ctx), e.record)
}

type authorizedAssistantDialectDrafts struct {
	repository dialectDraftRepository
	authorize  func(context.Context) error
}

func (a authorizedAssistantDialectDrafts) List(ctx context.Context) ([]dialectDraftSummary, error) {
	if err := a.authorize(ctx); err != nil {
		return nil, err
	}
	out, err := a.repository.List(ctx)
	if err != nil {
		return nil, err
	}
	if err = a.authorize(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
func (a authorizedAssistantDialectDrafts) Get(ctx context.Context, id string) (dialectDraftReply, error) {
	if err := a.authorize(ctx); err != nil {
		return dialectDraftReply{}, err
	}
	out, err := a.repository.Get(ctx, id)
	if err != nil {
		return dialectDraftReply{}, err
	}
	if err = a.authorize(ctx); err != nil {
		return dialectDraftReply{}, err
	}
	return out, nil
}
func (a authorizedAssistantDialectDrafts) CompareAndSwap(ctx context.Context, d dialectDraftDocument, revision string) (dialectDraftReply, error) {
	if err := a.authorize(ctx); err != nil {
		return dialectDraftReply{}, err
	}
	out, err := a.repository.CompareAndSwap(ctx, d, revision)
	if err != nil {
		return dialectDraftReply{}, err
	}
	if err = a.authorize(ctx); err != nil {
		return dialectDraftReply{}, err
	}
	return out, nil
}
func (s *Server) assistantDialectDraftRepository(authorize func(context.Context) error) dialectDraftRepository {
	if s.dialectDrafts != nil {
		return authorizedAssistantDialectDrafts{s.dialectDrafts, authorize}
	}
	return fileDialectDraftRepository{open: s.openDialectDrafts, authorize: authorize}
}

// Notification only, after actual independent draft readback. It cannot supply
// a result, draft, metadata or authority to this producer.
var testHookAssistantDialectRecipeRetained func()

func (e *assistantRunExecution) retainAssistantDialectRecipe(ctx context.Context, a assistantDialectRecipeContent, root bool) (runcontext.ToolResult, *assistantGraphRead, *assistantDomainOutput, error) {
	var empty runcontext.ToolResult
	if !root || e.record.Binding.Phase != "work" {
		return empty, nil, nil, failure.ToolInput("Dialect recipe proposals require the parent Work phase.")
	}
	p, ok := ctx.Value(assistantDialectRecipePreparedKey{}).(*assistantDialectRecipeProposal)
	in := assistantHostIntent{CallID: runcontext.CallIDOf(ctx), Root: root, Arguments: nil}
	in.CallbackID, _ = ctx.Value(assistantHostCallbackKey{}).(string)
	in.Arguments, _ = json.Marshal(a)
	// The shared dispatcher supplied this metadata only after persisting the
	// original exact arguments. Compare content, not a second reconstructed call.
	if !ok || p == nil || p.DraftID != assistantDialectRecipeProposalID(e.record.Binding, in) || !sameDraftValue(p.content(), a) {
		return empty, nil, nil, errAssistantRunScope
	}
	s := e.service.server
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := e.Authorize(ctx); err != nil {
		return empty, nil, nil, err
	}
	unlock, err := s.lockDialectDraftWorkflow(ctx)
	if err != nil {
		return empty, nil, nil, err
	}
	defer unlock()
	if err = e.authorizeAssistantDialectRecipe(ctx); err != nil {
		return empty, nil, nil, err
	}
	repository := s.assistantDialectDraftRepository(e.authorizeAssistantDialectRecipe)
	origin := &dialectDraftOrigin{e.record.Binding.Owner.ID, p.DraftID}
	creation := &assistantDialectRecipeCreate{dialectDraftRepository: repository}
	saved, err := s.ensureDialectDraftUsing(ctx, creation, dialectDraftRequest{Origin: origin, Name: p.Name, Recipe: p.content().recipe()}, p.CreatedAt)
	if err != nil {
		if creation.attempted && !errors.Is(err, errDialectDraftCapacity) {
			return empty, nil, nil, &assistantDialectRecipeUnknown{err}
		}
		known := failure.New("storage", "storage", "not-started")
		detail := "The Dialect draft could not be created. Resolve the current storage or capacity refusal before proposing again."
		known.Detail = &detail
		return empty, nil, nil, failure.NewError(known)
	}
	observed, err := repository.Get(ctx, saved.Document.ID)
	if err != nil {
		return empty, nil, nil, &assistantDialectRecipeUnknown{err}
	}
	if observed.SHA256 != saved.SHA256 || !validAssistantDialectRecipeRetained(e.record.Binding, *p, observed) {
		return empty, nil, nil, &assistantDialectRecipeUnknown{errAssistantRunConflict}
	}
	if testHookAssistantDialectRecipeRetained != nil {
		testHookAssistantDialectRecipeRetained()
	}
	result, err := assistantDialectRecipeResult(*p, observed.Document.ID)
	return result, nil, &assistantDomainOutput{Kind: "dialect-recipe", Recipe: &assistantDialectRecipeOutput{Proposal: *p, Retained: assistantDialectRecipeRetention{observed.Document.ID, observed.SHA256}}}, err
}
func (s *Server) checkAssistantDialectRecipeDrafts(r *http.Request, run assistantRunRecord) error {
	repository := s.assistantDialectDraftRepository(func(ctx context.Context) error { return s.authorizeAssistantRun(r.WithContext(ctx), run) })
	for _, out := range run.DomainOutputs {
		if out.Kind != "dialect-recipe" {
			continue
		}
		saved, err := repository.Get(r.Context(), out.Recipe.Retained.ID)
		if err != nil {
			return err
		}
		if saved.SHA256 != out.Recipe.Retained.SHA256 || !validAssistantDialectRecipeRetained(run.Binding, out.Recipe.Proposal, saved) {
			return errAssistantRunConflict
		}
	}
	return nil
}
