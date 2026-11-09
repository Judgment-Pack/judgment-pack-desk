package desk

// Fresh observations retain the normal authenticated outer owner, but Agent
// alone opens their first durable head. A retained record is never a launcher.
import (
	"context"
	"encoding/json"
	"reflect"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

const assistantSettingsObservationPurpose = "settings-observation"

type assistantObservationProfile struct {
	Kind          string
	Model         string
	OutputCeiling int
	Digest        string
}

// Only a concrete host executor implements this method. Neither the public
// purpose nor the persisted custody member can choose fresh persistence.
type assistantObservationExecutor interface {
	assistantObservationProfile(assistantRunSource) (assistantObservationProfile, error)
}

type assistantObservationCustody struct {
	Outcome       *assistantSettingsObservationOutcome `json:"outcome,omitempty"`
	Version       int                                  `json:"version"`
	Kind          string                               `json:"kind"`
	Model         string                               `json:"model"`
	OutputCeiling int                                  `json:"outputCeiling"`
	ProfileDigest string                               `json:"profileDigest"`
	SourceDigest  string                               `json:"sourceDigest"`
	RunID         string                               `json:"runId"`
	RequestKey    string                               `json:"requestKey"`
	Phase         string                               `json:"phase"`
	Unit          string                               `json:"unit"`
}

func assistantObservationAdmission(executor assistantRunExecutor, source assistantRunSource) (*assistantObservationProfile, error) {
	owner, ok := executor.(assistantObservationExecutor)
	if !ok {
		if source.Start.Purpose == assistantSettingsObservationPurpose {
			return nil, errAssistantRunScope
		}
		return nil, nil
	}
	profile, err := owner.assistantObservationProfile(source)
	if err != nil {
		return nil, err
	}
	if source.Start.Purpose != assistantSettingsObservationPurpose || source.Binding.Owner.Kind != assistantSettingsObservationPurpose || source.Binding.Owner.ID != source.Binding.ConnectionID || source.Binding.Model != profile.Model || source.Binding.Phase != "work" || !validAssistantObservationProfile(profile) || digestOf(source.Request) != source.Binding.RequestDigest || !contains(source.Binding.SourceDigests, digestOf(source.Original)) {
		return nil, errAssistantRunScope
	}
	return &profile, nil
}

func validAssistantObservationProfile(profile assistantObservationProfile) bool {
	return assistantRunDigest(profile.Digest) && assistantRunText(profile.Model, 128) && (profile.Kind == "capability" && profile.OutputCeiling == 128 || profile.Kind == "ping" && profile.OutputCeiling == 1)
}

func (w *assistantRunWriter) initializeObservation(ctx context.Context, source assistantRunSource, profile assistantObservationProfile, state contract.Checkpoint) error {
	return w.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		if !reflect.DeepEqual(run.Binding, source.Binding) || run.Observation != nil || run.Canonical != nil || run.CanonicalRequired || run.Checkpoint != nil || run.Historical || run.State != "admitted" || state.Durable != nil || !validAssistantObservationProfile(profile) || profile.Model != run.Binding.Model {
			return false, errAssistantRunConflict
		}
		cp, err := readAssistantRunCheckpoint(state, run.Binding)
		if err != nil {
			return false, err
		}
		custody := &assistantObservationCustody{Version: 1, Kind: profile.Kind, Model: profile.Model, OutputCeiling: profile.OutputCeiling, ProfileDigest: profile.Digest, SourceDigest: digestOf(source.Original), RunID: cp.ID, RequestKey: cp.RequestKey, Phase: cp.Phase, Unit: cp.Unit}
		if err = validateAssistantObservationCheckpoint(cp, custody, true); err != nil {
			return false, err
		}
		run.Observation, run.Checkpoint, run.CanonicalRequired = custody, cp, true
		return true, validateAssistantObservationRecord(*run)
	})
}

func validateAssistantObservationCheckpoint(cp *contract.Checkpoint, held *assistantObservationCustody, initial bool) error {
	if cp == nil || cp.ID != held.RunID || cp.RequestKey != held.RequestKey || cp.Phase != held.Phase || cp.Unit != held.Unit || cp.RequestCap == nil || *cp.RequestCap != 1 || cp.Limit != 1 || cp.Used < 0 || cp.Used > 1 || len(cp.Grants) != 0 || cp.Approval != nil || cp.Handoff != nil || cp.Delegation != nil || len(cp.ChildUsage) != 0 || cp.UsageBudget != nil || cp.TurnRoute != nil || len(cp.Failover) != 0 || len(cp.Operations) != 0 || len(cp.Plan) != 0 {
		return errAssistantRunScope
	}
	if initial && (cp.Status != "running" || cp.Used != 0 || len(cp.Prompts) != 0 || len(cp.Completions) != 0) {
		return errAssistantRunScope
	}
	if book := cp.Durable; book != nil {
		if len(book.Children) != 0 || book.PendingHandoff != nil || len(book.Dispatches) > 1 || initial && (len(book.Dispatches) != 0 || len(book.Observations) != 0) {
			return errAssistantRunScope
		}
		for _, dispatch := range book.Dispatches {
			if dispatch.Kind != "model" || dispatch.Tool != held.Model || dispatch.Scope.Execution != held.RunID || dispatch.Scope.Role != "author" || dispatch.Scope.Task != nil || dispatch.PlanPause != nil || len(dispatch.Artifacts) != 0 {
				return errAssistantRunScope
			}
		}
	}
	return nil
}

func validateAssistantObservationRecord(run assistantRunRecord) error {
	held := run.Observation
	if held == nil {
		if run.Binding.Owner.Kind == assistantSettingsObservationPurpose && (run.Checkpoint != nil || run.CanonicalRequired || run.Canonical != nil) {
			return errAssistantRunInvalid
		}
		return nil
	}
	profile := assistantObservationProfile{held.Kind, held.Model, held.OutputCeiling, held.ProfileDigest}
	if held.Version != 1 || !validAssistantObservationProfile(profile) || run.Binding.Owner.Kind != assistantSettingsObservationPurpose || run.Binding.Owner.ID != run.Binding.ConnectionID || held.Model != run.Binding.Model || !run.CanonicalRequired || !assistantCanonicalID(held.RunID) || held.RequestKey != run.Binding.ExecutionKey || held.Phase != "work" || held.Phase != run.Binding.Phase || held.Unit != "model-requests" || held.Unit != run.Binding.Unit || !contains(run.Binding.SourceDigests, held.SourceDigest) || !assistantRunDigest(held.SourceDigest) {
		return errAssistantRunInvalid
	}
	if err := validateAssistantObservationCheckpoint(run.Checkpoint, held, false); err != nil {
		return err
	}
	if run.Creation != nil || run.ChatProjection != nil || run.CriticCustody != nil || len(run.DomainOutputs) != 0 || len(run.Operations) > 1 {
		return errAssistantRunInvalid
	}
	for _, action := range run.Actions {
		if action.Kind != "cancel" {
			return errAssistantRunInvalid
		}
	}
	for _, op := range run.Operations {
		if op.Kind != "model" || op.Name != held.Model {
			return errAssistantRunInvalid
		}
	}
	if run.Canonical == nil {
		if run.Checkpoint.Durable != nil || run.Checkpoint.Used != 0 || len(run.Checkpoint.Prompts) != 0 || len(run.Checkpoint.Completions) != 0 || len(run.Operations) != 0 || len(run.Events) != 0 || run.State != "admitted" && run.State != "stopped" && run.State != "interrupted" && run.State != "failed" || run.Checkpoint.Status != "running" && run.Checkpoint.Status != "stopped" || run.State == "admitted" && run.Checkpoint.Status != "running" {
			return errAssistantRunInvalid
		}
	} else if !assistantCanonicalActive(run) || run.Canonical.Head.RunID != held.RunID || run.Checkpoint.Durable == nil {
		return errAssistantRunInvalid
	}
	return validateAssistantSettingsOutcome(run)
}

// The claim belongs to one in-memory execution and one outer writer. It is not
// reconstructed from retained data, including after a lost acknowledgement.
type assistantObservationClaim struct {
	execution *assistantRunExecution
	custody   assistantObservationCustody
	store     *assistantCanonicalRunStore
	inputUsed bool  // protected by canonicalMu
	attachErr error // protected by canonicalMu
}

func (e *assistantRunExecution) openObservationClaim(ctx context.Context, profile assistantObservationProfile) error {
	latest, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return err
	}
	if latest.Historical || latest.State != "admitted" || latest.Canonical != nil || validateAssistantObservationRecord(latest) != nil || latest.Observation == nil || latest.Observation.ProfileDigest != profile.Digest || latest.Observation.Kind != profile.Kind || latest.Observation.Model != profile.Model || latest.Observation.OutputCeiling != profile.OutputCeiling || !reflect.DeepEqual(latest.Binding, e.source.Binding) || latest.Observation.SourceDigest != digestOf(e.source.Original) || latest.Binding.RequestDigest != digestOf(e.source.Request) {
		return &runcontext.ContinuationNotRecoverable{}
	}
	claim := &assistantObservationClaim{execution: e, custody: *latest.Observation}
	claim.store = &assistantCanonicalRunStore{writer: e.writer, id: claim.custody.RunID, observation: claim}
	e.canonicalMu.Lock()
	defer e.canonicalMu.Unlock()
	if e.observation != nil || e.canonicalBinding != nil || e.stopRequested || e.ctx.Err() != nil {
		return &runcontext.ContinuationNotRecoverable{}
	}
	e.observation = claim
	return nil
}

// This leaves all forbidden Observe input fields absent. Only the actual
// ContextualSession is permitted to create and attach its original writer.
func (e *assistantRunExecution) bindObservationInput(ctx context.Context, input *runcontext.Input) error {
	if input == nil || input.RunStore != nil || input.Execution != nil || input.Persistence != nil || input.Checkpoint != nil || input.Approval != nil || input.Link != nil || input.Objective != "" {
		return &runcontext.ContinuationIdentityChanged{}
	}
	e.canonicalMu.Lock()
	defer e.canonicalMu.Unlock()
	claim := e.observation
	if claim == nil || claim.inputUsed || e.stopRequested || e.ctx.Err() != nil || ctx.Err() != nil {
		return &runcontext.ContinuationNotRecoverable{}
	}
	claim.inputUsed = true
	input.RunStore = &runcontext.DurableRunInput{Store: claim.store, ID: claim.custody.RunID}
	input.Link = claim
	return nil
}

func (claim *assistantObservationClaim) Attach(sc *runcontext.SessionContext) {
	var binding *runcontext.RunBinding
	if sc != nil {
		binding = sc.Persistence("author")
	}
	// Checkpoint takes Agent's writer mutex; do it before canonicalMu. A commit
	// already holds that mutex before entering this store's canonicalMu.
	var cp *contract.Checkpoint
	if binding != nil && binding.Writer != nil {
		cp = binding.Writer.Checkpoint()
	}
	e := claim.execution
	e.canonicalMu.Lock()
	refused := binding == nil || binding.Writer == nil || binding.Writer.Store != claim.store || binding.Writer.ID != claim.custody.RunID || !binding.Root || binding.Scope.Execution != claim.custody.RunID || binding.Scope.Role != "author" || binding.Scope.Task != "" || validateAssistantObservationCheckpoint(cp, &claim.custody, true) != nil || e.observation != claim || e.canonicalBinding != nil && e.canonicalBinding.Writer != binding.Writer
	if refused {
		claim.attachErr = &runcontext.ContinuationIdentityChanged{}
	} else {
		e.canonicalBinding = binding
	}
	if binding != nil && binding.Writer != nil && (refused || e.stopRequested || e.ctx.Err() != nil) {
		_ = binding.Writer.CloseDispatch(*binding)
	}
	e.canonicalMu.Unlock()
	if refused && e.cancel != nil {
		e.cancel()
	}
}

func (e *assistantRunExecution) observationError() error {
	e.canonicalMu.Lock()
	defer e.canonicalMu.Unlock()
	if e.observation != nil {
		return e.observation.attachErr
	}
	return nil
}

func (claim *assistantObservationClaim) validateCommit(run *assistantRunRecord, input runcontext.RunCommit) error {
	e := claim.execution
	if e.observation != claim || !reflect.DeepEqual(run.Observation, &claim.custody) || !reflect.DeepEqual(run.Binding, e.source.Binding) || digestOf(e.source.Original) != claim.custody.SourceDigest || digestOf(e.source.Request) != run.Binding.RequestDigest || validateAssistantObservationRecord(*run) != nil || input.Checkpoint == nil {
		return &runcontext.ContinuationIdentityChanged{}
	}
	value, err := runcontext.ParseJSON(input.Checkpoint)
	if err != nil {
		return err
	}
	cp, err := runcontext.ReadExecution(value)
	if err != nil {
		return err
	}
	initial := run.Canonical == nil
	if err = validateAssistantObservationCheckpoint(cp, &claim.custody, initial); err != nil {
		return err
	}
	if initial {
		if e.stopRequested || e.ctx.Err() != nil {
			return failure.Cancelled
		}
		if run.State != "admitted" || len(run.Actions) != 0 || input.ExpectedRevision != "" || len(input.Events) != 0 {
			return &runcontext.ContinuationIdentityChanged{}
		}
		run.Canonical = &assistantCanonicalJournal{Version: 1, Active: true, Head: assistantCanonicalHead{RunID: claim.custody.RunID}, Events: []assistantCanonicalEvent{}, Commits: []assistantCanonicalCommit{}}
	}
	return nil
}

// Uncertainty closes the outer reservation without editing an acknowledged
// canonical head. This result-only write never grants dispatch or a new writer.
func (w *assistantRunWriter) interruptObservation(ctx context.Context) error {
	return w.change(ctx, true, func(run *assistantRunRecord) (bool, error) {
		if run.Observation == nil || assistantRunTerminal(run.State) {
			return false, nil
		}
		run.State = "interrupted"
		for i := range run.Operations {
			if run.Operations[i].State == "dispatched" {
				run.Operations[i].State = "uncertain"
			}
		}
		return true, nil
	})
}

// Keep custody parsing alongside checkpoint raw validation; Go's decoder must
// not turn an absent required member into a trusted zero value.
func readAssistantObservationCustody(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	return readAssistantDialectCustody(raw, reflect.TypeOf(assistantObservationCustody{}))
}
