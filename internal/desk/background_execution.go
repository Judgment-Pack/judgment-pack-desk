package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	agentadapter "github.com/Judgment-Pack/judgment-pack-agent/go/adapter/agent"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/eino"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/aifailure"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// The decoder's complete encoded receipt bound is also reserved inside the
// original attempt. The extra framing covers its enclosing member and comma.
const backgroundModelReceiptBytes = runcontext.MaxCheckpointBytes
const backgroundModelReceiptReserve = backgroundModelReceiptBytes + 32

// This optional receipt belongs to the original reserved workflow attempt. It
// never creates a chat, grants requests, or resumes an interrupted invocation.
// The enclosing journal's existing byte cap includes every member below.
type backgroundModelReceipt struct {
	Version    int                   `json:"version"`
	Owner      string                `json:"owner"`
	Digest     string                `json:"digest"`
	Project    string                `json:"project"`
	Profile    string                `json:"profile"`
	Choice     backgroundModelChoice `json:"choice"`
	Dispatched bool                  `json:"dispatched"`
	Checkpoint *contract.Checkpoint  `json:"checkpoint,omitempty"`
	Response   json.RawMessage       `json:"response,omitempty"`
	Wire       json.RawMessage       `json:"wire,omitempty"`
	Failure    *aifailure.Failure    `json:"failure,omitempty"`
}

func (r *backgroundModelReceipt) UnmarshalJSON(raw []byte) error {
	type plain backgroundModelReceipt
	var value plain
	var fields map[string]json.RawMessage
	if len(raw) > backgroundModelReceiptBytes || decodeDataJSON(raw, &value) != nil || json.Unmarshal(raw, &fields) != nil {
		return errBackgroundReceipt
	}
	for _, name := range []string{"version", "owner", "digest", "project", "profile", "choice", "dispatched"} {
		if len(fields[name]) == 0 || bytes.Equal(fields[name], []byte("null")) {
			return errBackgroundReceipt
		}
	}
	if value.Version != 1 || value.Owner == "" || len(value.Owner) > 256 || !retainedArtifactDigest.MatchString(value.Digest) || !retainedArtifactDigest.MatchString(value.Project) || !retainedArtifactDigest.MatchString(value.Profile) || value.Choice.Connection == "" || value.Choice.Revision == "" || value.Choice.Model == "" || len(value.Response) > backgroundModelResponseBytes || len(value.Wire) > 2<<20 {
		return errBackgroundReceipt
	}
	if value.Checkpoint != nil {
		encoded, err := json.Marshal(value.Checkpoint)
		if err != nil || len(encoded) > runcontext.MaxCheckpointBytes {
			return errBackgroundReceipt
		}
		v, err := runcontext.ParseJSON(encoded)
		if err != nil {
			return errBackgroundReceipt
		}
		if _, err = runcontext.ReadExecution(v); err != nil {
			return errBackgroundReceipt
		}
	}
	if cp := value.Checkpoint; cp != nil {
		switch cp.Unit {
		case "model-requests":
			if cp.RequestCap == nil || *cp.RequestCap != 1 || cp.Limit != 1 || cp.Used > 1 || len(cp.Grants) != 0 {
				return errBackgroundReceipt
			}
		case "provider-managed":
			if cp.RequestCap != nil || cp.Limit != 1 || cp.Used != 0 || len(cp.Grants) != 0 {
				return errBackgroundReceipt
			}
		default:
			return errBackgroundReceipt
		}
	}
	if value.Dispatched && value.Checkpoint == nil || (len(value.Response) > 0 || len(value.Wire) > 0) && !value.Dispatched {
		return errBackgroundReceipt
	}
	if value.Failure != nil {
		raw, _ := json.Marshal(value.Failure)
		var held contract.Failure
		if json.Unmarshal(raw, &held) != nil {
			return errBackgroundReceipt
		}
		if _, ok := failure.Read(held); !ok {
			return errBackgroundReceipt
		}
	}
	*r = backgroundModelReceipt(value)
	return nil
}

var errBackgroundReceipt = aifailure.Authored("stale-state", "desk", "not-started", "The reserved background operation is unavailable or has already been used.")

type backgroundAttemptKey struct{}
type backgroundAttempt struct {
	store                researchRunStore
	run, attempt, digest string
	revision             string
	gateway              inputResearchPin
}

func bindBackgroundAttempt(ctx context.Context, store researchRunStore, run inputResearchRun, attempt inputResearchAttempt) context.Context {
	return context.WithValue(ctx, backgroundAttemptKey{}, &backgroundAttempt{store: store, run: run.ID, attempt: attempt.ID, digest: attempt.Digest, revision: run.Revision, gateway: run.Gateway})
}
func backgroundAttemptOwner(ctx context.Context) string {
	if a, _ := ctx.Value(backgroundAttemptKey{}).(*backgroundAttempt); a != nil {
		return a.run + "/" + a.attempt
	}
	return ""
}
func (a *backgroundAttempt) change(choice backgroundModelChoice, f func(*inputResearchAttempt) error) error {
	_, err := a.store.change(func(run *inputResearchRun) error {
		if run.ID != a.run || run.State != "running" || run.StopRequested || !a.matches(*run, choice) {
			return errBackgroundReceipt
		}
		attempt, err := researchAttemptByID(run, a.attempt)
		if err != nil || attempt.Kind != "model" || attempt.Digest != a.digest || (attempt.State != "started" && attempt.State != "waiting") {
			return errBackgroundReceipt
		}
		return f(attempt)
	})
	return err
}

// Re-read the original workflow choice; the receipt cannot authorize a changed
// recipe or portable definition, even when the connection is still enabled.
func (a *backgroundAttempt) matches(run inputResearchRun, choice backgroundModelChoice) bool {
	current := run.Recipe.AI
	if run.Portable != nil {
		current = run.Portable.Frozen.Choice
	}
	return current == choice && run.Revision == a.revision && run.Gateway == a.gateway
}

type backgroundExecution struct {
	server           *Server
	attempt          *backgroundAttempt
	task             backgroundModelRequest
	project, profile string
	ctx              context.Context
	cancel           context.CancelFunc
	epoch            context.Context
	credential       string
	source           string
	account          providerAccountManager
	mu               sync.Mutex
	failed           error
	nativeInvoked    bool
}

func (b *backgroundExecution) authorize(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	failed := b.failed
	b.mu.Unlock()
	if failed != nil {
		return failed
	}
	b.server.signIn.mu.Lock()
	epoch := b.server.signIn.epoch
	b.server.signIn.mu.Unlock()
	if epoch != b.epoch || epoch.Err() != nil || b.server.projectHasMoved() || digestOf([]byte(b.server.conversationIdentity())) != b.project {
		return errBackgroundReceipt
	}
	_, source, err := b.server.aiContext(b.task.Choice.Model)
	if err != nil {
		return err
	}
	if digestOf([]byte(source)) != b.source {
		return errBackgroundReceipt
	}
	if b.account != nil {
		current, err := b.server.accountForConnection(backgroundAIRequest(ctx, b.task.Choice))
		if err != nil {
			return err
		}
		if current != b.account {
			return errBackgroundReceipt
		}
	}
	profile, err := b.server.readAssistantProfile()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	if digestOf(raw) != b.profile {
		return errBackgroundReceipt
	}
	if err = b.server.validateBackgroundModelConnection(ctx, b.task.Choice); err != nil {
		return err
	}
	credential, err := b.server.backgroundCredential(ctx, b.task.Choice)
	if err != nil {
		return err
	}
	if credential != b.credential {
		return errBackgroundReceipt
	}
	run, err := b.attempt.store.read()
	if err != nil {
		return err
	}
	if !b.attempt.matches(run, b.task.Choice) {
		return errBackgroundReceipt
	}
	if run.Portable != nil {
		// Reuse the workflow's Runner/profile/Gateway/Work admission, including
		// rooted source validation, rather than treating a receipt as authority.
		if err := b.server.validatePortableResearchAdmission(ctx, run); err != nil {
			return err
		}
	} else if run.Gateway.Authority != "" {
		current, err := b.server.configuredResearch()
		if err != nil {
			return err
		}
		if current.authority != run.Gateway.Authority || current.signerPublic != run.Gateway.Public {
			return errBackgroundReceipt
		}
	}
	a, err := researchAttemptByID(&run, b.attempt.attempt)
	if err != nil || run.ID != b.attempt.run || run.State != "running" || run.StopRequested || a.Kind != "model" || a.Digest != b.attempt.digest || (a.State != "started" && a.State != "waiting") || a.Background == nil || a.Background.Owner != b.task.Owner || a.Background.Project != b.project || a.Background.Profile != b.profile || a.Background.Choice != b.task.Choice {
		return errBackgroundReceipt
	}
	return nil
}
func (b *backgroundExecution) retain(f func(*backgroundModelReceipt) error) error {
	if err := b.authorize(b.ctx); err != nil {
		return err
	}
	return b.attempt.change(b.task.Choice, func(a *inputResearchAttempt) error {
		if a.Background == nil {
			return errBackgroundReceipt
		}
		return f(a.Background)
	})
}
func (b *backgroundExecution) dispatch(ctx context.Context) error {
	if err := b.authorize(ctx); err != nil {
		return err
	}
	return b.retain(func(r *backgroundModelReceipt) error {
		if r.Dispatched {
			return errBackgroundReceipt
		}
		r.Dispatched = true
		return nil
	})
}
func (b *backgroundExecution) checkpoint(ctx context.Context, cp contract.Checkpoint) error {
	if err := b.authorize(ctx); err != nil {
		return err
	}
	raw, err := json.Marshal(cp)
	if err != nil || len(raw) > runcontext.MaxCheckpointBytes {
		return errBackgroundReceipt
	}
	return b.retain(func(r *backgroundModelReceipt) error { r.Checkpoint = &cp; return nil })
}
func (b *backgroundExecution) latch(err error) {
	if err != nil {
		b.mu.Lock()
		if b.failed == nil {
			b.failed = err
		}
		b.mu.Unlock()
	}
}

// runBackgroundModel uses the published adapters for one previously reserved
// operation. Its authority is the live workflow receipt, not chat reconstruction.
func (s *Server) runBackgroundModel(ctx context.Context, task backgroundModelRequest, availability func(bool) error) (result json.RawMessage, modelErr error) {
	defer func() {
		var canonical *failure.Error
		if errors.As(modelErr, &canonical) {
			modelErr = backgroundFailure(canonical.Failure)
		}
		modelErr = aiModelFailure(modelErr)
	}()
	admission, _ := ctx.Value(backgroundAttemptKey{}).(*backgroundAttempt)
	if admission == nil || task.Owner != backgroundAttemptOwner(ctx) || task.Instructions == "" || task.Response.Name == "" || task.Response.Description == "" || !json.Valid(task.Response.InputSchema) || task.Validate == nil {
		return nil, errBackgroundReceipt
	}
	if err := s.validateBackgroundModelConnection(ctx, task.Choice); err != nil {
		return nil, err
	}
	profile, err := s.readAssistantProfile()
	if err != nil {
		return nil, err
	}
	profileRaw, err := json.Marshal(profile)
	if err != nil {
		return nil, err
	}
	credential, err := s.backgroundCredential(ctx, task.Choice)
	if err != nil {
		return nil, err
	}
	s.signIn.mu.Lock()
	epoch := s.signIn.epoch
	s.signIn.mu.Unlock()
	_, source, err := s.aiContext(task.Choice.Model)
	if err != nil {
		return nil, err
	}
	b := &backgroundExecution{source: digestOf([]byte(source)), ctx: ctx, epoch: epoch, credential: credential, server: s, attempt: admission, task: task, project: digestOf([]byte(s.conversationIdentity())), profile: digestOf(profileRaw)}
	if err = admission.change(task.Choice, func(a *inputResearchAttempt) error {
		if a.Background != nil {
			return errBackgroundReceipt
		}
		a.Background = &backgroundModelReceipt{Version: 1, Owner: task.Owner, Digest: admission.digest, Project: b.project, Profile: b.profile, Choice: task.Choice}
		return nil
	}); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	defer cancel()
	defer func() {
		if modelErr != nil {
			held := backgroundFailure(failure.Normalize(backgroundCanonicalFailure(modelErr, false), "model", time.Now()))
			_ = b.retain(func(r *backgroundModelReceipt) error { r.Failure = held; return nil })
		}
	}()
	host, err := s.assistantPromptHost(ctx)
	if err != nil {
		return nil, err
	}
	memory, proposals := false, false
	input := &runcontext.Input{Connection: task.Choice.Connection, CurrentRequest: &runcontext.CurrentRequest{Message: task.Owner, Input: task.Prompt}, Memory: &memory, ArtifactProposals: &proposals, Authorize: b.authorize, Checkpoint: b.checkpoint}
	if profile.Context != nil {
		raw, _ := json.Marshal(profile.Context)
		v, e := runcontext.ParseJSON(raw)
		if e != nil {
			return nil, e
		}
		input.Configuration, e = runcontext.ReadConfiguration(v)
		if e != nil {
			return nil, e
		}
	}
	var action json.RawMessage
	native := false
	var eventFailure error
	emit := func(event contract.RunEvent) {
		if event.ExecutionCheckpoint != nil {
			if e := b.checkpoint(ctx, event.ExecutionCheckpoint.Checkpoint); e != nil {
				b.latch(e)
				cancel()
			}
		}
		if event.Error != nil {
			if event.Error.Failure != nil {
				eventFailure = backgroundFailure(*event.Error.Failure)
			} else {
				eventFailure = aifailure.New("invalid-response", "model", "unknown")
			}
		}
		if event.Message != nil && len(action) == 0 && !native {
			if len(event.Message.Text) > backgroundModelResponseBytes {
				eventFailure = aifailure.New("invalid-response", "model", "unknown")
			} else {
				action = json.RawMessage(strings.TrimSpace(event.Message.Text))
			}
		}
	}
	g := gate.New(gate.Options{Emit: emit})
	run := &adapter.Run{ModelName: task.Choice.Model, AllowConversation: true, Critic: "off", Context: input, Host: host, Gate: g.Calls(), Emit: emit, Prompt: task.Instructions + "\n\nReturn exactly one JSON object satisfying this host response schema, without markdown:\n" + string(task.Response.InputSchema) + "\n\n" + task.Prompt}
	connection, err := s.selectedAIConnection(backgroundAIRequest(ctx, task.Choice), true)
	if err != nil {
		return nil, err
	}
	var executor adapter.Adapter = eino.New()
	if connection.Assistant.Engine == "codex" {
		native = true
		response := runcontext.HostTool{Name: task.Response.Name, Description: task.Response.Description, InputSchema: task.Response.InputSchema, Effect: "read", Execute: func(call context.Context, raw json.RawMessage) (result runcontext.ToolResult, toolErr error) {
			defer func() { toolErr = backgroundCanonicalFailure(toolErr, false) }()
			if err := b.authorize(call); err != nil {
				return runcontext.ToolResult{}, err
			}
			if len(action) > 0 || len(raw) > backgroundModelResponseBytes {
				return runcontext.ToolResult{}, aifailure.New("invalid-response", "agent", "unknown")
			}
			if err := task.Validate(raw); err != nil {
				return runcontext.ToolResult{}, aifailure.Authored("invalid-response", "agent", "unknown", "The structured response does not match the host contract.")
			}
			action = append(json.RawMessage(nil), raw...)
			if err := b.retain(func(r *backgroundModelReceipt) error {
				r.Response = append(json.RawMessage(nil), action...)
				return nil
			}); err != nil {
				b.latch(err)
				cancel()
				return runcontext.ToolResult{}, err
			}
			return runcontext.ResultOf([]runcontext.Content{runcontext.TextContent("Response retained. Stop this invocation.")}, nil), nil
		}}
		g.SetOffer(contract.ToolOffer{Tools: []contract.ToolOfferRuntimeTool{}, HostTools: []contract.ToolOfferHostTool{response.Declaration()}})
		run.HostTools = []runcontext.HostTool{response}
		run.Agent, err = b.agent(ctx, availability)
		if err != nil {
			return nil, err
		}
		executor = agentadapter.New()
	} else {
		g.SetOffer(contract.ToolOffer{Tools: []contract.ToolOfferRuntimeTool{}, HostTools: []contract.ToolOfferHostTool{}})
		requestCap := 1
		input.RequestCap = &requestCap
		run.Wire = connection.Assistant.Endpoint.Kind
		run.Model = b.model(availability)
		if connection.Assistant.Endpoint.Capabilities != nil {
			raw, _ := json.Marshal(connection.Assistant.Endpoint.Capabilities)
			run.Capabilities = &contract.CapabilityEndpointCapabilities{}
			if err = json.Unmarshal(raw, run.Capabilities); err != nil {
				return nil, err
			}
		}
		// Pass the exact admitted effort through canonical policy, not a second
		// provider-specific translation in the host transport.
		if task.Choice.Effort != "" {
			run.Policy = runcontext.PolicyLayer{"models": map[string]any{"author": map[string]any{"reasoning": task.Choice.Effort}}}
		}
	}
	err = executor.Run(ctx, run)
	b.mu.Lock()
	latched := b.failed
	b.mu.Unlock()
	if latched != nil {
		return nil, latched
	}
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if eventFailure != nil {
		return nil, eventFailure
	}
	// The original structured workflow requires a completed response even when
	// conversation mode accepts prose with an unknown or tool-only stop reason.
	if connection.Assistant.Engine != "codex" {
		held, err := admission.store.read()
		if err != nil {
			return nil, err
		}
		a, err := researchAttemptByID(&held, admission.attempt)
		if err != nil || a.Background == nil || a.Background.Checkpoint == nil || len(a.Background.Checkpoint.Completions) != 1 {
			return nil, aifailure.New("invalid-response", "model", "unknown")
		}
		completion := a.Background.Checkpoint.Completions[0].Result
		if completion.FinishReason != "stop" {
			f := failure.New(failure.CompletionFailureCode(&completion), "model", "unknown")
			f.Completion = &completion
			return nil, failure.NewError(f)
		}
	}
	if len(action) == 0 || len(action) > backgroundModelResponseBytes {
		return nil, aifailure.New("invalid-response", "model", "unknown")
	}
	if !json.Valid(action) {
		action, _ = json.Marshal(string(action))
	}
	if err = b.retain(func(r *backgroundModelReceipt) error {
		r.Response = append(json.RawMessage(nil), action...)
		return nil
	}); err != nil {
		return nil, err
	}
	// A malformed response is completed data for the workflow's existing bounded
	// correction path. It is never dispatched as a domain action here.
	if err = b.authorize(ctx); err != nil {
		return nil, err
	}
	return action, nil
}

// The host keeps addresses/credentials/capacity; Eino alone constructs requests
// and parses responses. A physical dispatch consumes the original one-use receipt.
func (b *backgroundExecution) model(availability func(bool) error) relay.Call {
	return func(ctx context.Context, suffix string, request relay.Request) (response *http.Response, callErr error) {
		defer func() { callErr = backgroundCanonicalFailure(callErr, false) }()
		if err := b.authorize(ctx); err != nil {
			return nil, err
		}
		r := backgroundAIRequest(ctx, b.task.Choice)
		endpoint, keyName, _, err := b.server.endpointForConnection(r, true)
		if err != nil {
			return nil, err
		}
		path, pair, _ := strings.Cut(suffix, "?")
		if request.Method != "" && request.Method != "POST" || relay.CallSuffixProblem(suffix, endpoint.kind) != "" || !profileAPIModelAllowed(endpoint.kind, path, request.Body, []string{b.task.Choice.Model}) {
			return nil, errBackgroundReceipt
		}
		target, err := relayTarget(endpoint.url, path, pair)
		if err != nil {
			return nil, err
		}
		if availability != nil {
			if err = availability(true); err != nil {
				return nil, err
			}
		}
		key := b.server.modelCapacityKey(endpoint.url, keyName)
		release, err := modelCapacity.Acquire(ctx, key)
		if err != nil {
			return nil, err
		}
		defer release()
		if err = b.authorize(ctx); err != nil {
			return nil, err
		}
		stored, err := b.server.assistant.readKeyNamed(keyName)
		if err != nil {
			return nil, err
		}
		keyless := !stored.present && endpoint.keyless()
		if !stored.present && !keyless {
			return nil, aifailure.New("authentication", "model", "not-started")
		}
		if bindingProblem(stored, endpoint) != "" && !keyless {
			return nil, aifailure.New("configuration", "model", "not-started")
		}
		policy, _, err := b.server.aiContext(b.task.Choice.Model)
		if err != nil {
			return nil, err
		}
		if err = policy.Check(string(request.Body)); err != nil {
			return nil, err
		}
		outbound, err := http.NewRequestWithContext(ctx, "POST", target.String(), bytes.NewReader(request.Body))
		if err != nil {
			return nil, err
		}
		for name, value := range request.Headers {
			if relayedRequestHeader(name) {
				outbound.Header.Set(name, value)
			}
		}
		header, value, ok := credentialHeader(endpoint.kind, endpoint.auth, stored.key)
		if !ok {
			return nil, aifailure.New("configuration", "model", "not-started")
		}
		if !keyless {
			outbound.Header.Set(header, value)
		}
		if availability != nil {
			if err = availability(false); err != nil {
				return nil, err
			}
		}
		if err = b.dispatch(ctx); err != nil {
			return nil, err
		}
		transport := relayTransport
		if transport == nil {
			transport = http.DefaultTransport
		}
		response, err = transport.RoundTrip(outbound)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, aifailure.New("connection-lost", "model", "unknown")
		}
		defer response.Body.Close()
		if response.StatusCode == 429 {
			modelCapacity.Throttle(key)
		}
		raw, err := readBounded(response.Body, 2<<20)
		if err != nil {
			return nil, aifailure.New("invalid-response", "model", "unknown")
		}
		if stored.key != "" && bytes.Contains(raw, []byte(stored.key)) {
			return nil, aifailure.New("invalid-response", "model", "unknown")
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, aifailure.HTTPResponse(response.StatusCode, response.Header, raw, "model", time.Now())
		}
		held, _ := json.Marshal(struct {
			Status int    `json:"status"`
			Body   []byte `json:"body"`
		}{response.StatusCode, raw})
		if len(held) > 2<<20 {
			return nil, aifailure.New("invalid-response", "model", "unknown")
		}
		if err = b.retain(func(r *backgroundModelReceipt) error { r.Wire = held; return nil }); err != nil {
			b.latch(err)
			return nil, err
		}
		response.Body = io.NopCloser(bytes.NewReader(raw))
		response.ContentLength = int64(len(raw))
		return response, nil
	}
}

func (b *backgroundExecution) agent(ctx context.Context, availability func(bool) error) (*adapter.Agent, error) {
	account, err := b.server.accountForConnection(backgroundAIRequest(ctx, b.task.Choice))
	if err != nil {
		return nil, err
	}
	b.account = account
	runner, ok := account.(providerRunner)
	if !ok {
		return nil, codexbridge.ErrUnavailable
	}
	b.server.signIn.mu.Lock()
	policy := b.server.signIn.epoch
	b.server.signIn.mu.Unlock()
	return &adapter.Agent{Model: b.task.Choice.Model, Effort: b.task.Choice.Effort, Enabled: func(ctx context.Context, model string) error {
		if model != b.task.Choice.Model {
			return errBackgroundReceipt
		}
		return b.authorize(ctx)
	}, Run: func(ctx context.Context, request adapter.AgentRequest, callbacks adapter.AgentCallbacks) (callErr error) {
		defer func() { callErr = backgroundCanonicalFailure(callErr, true) }()
		b.mu.Lock()
		if b.nativeInvoked {
			b.mu.Unlock()
			return errBackgroundReceipt
		}
		b.nativeInvoked = true
		b.mu.Unlock()
		bridge := codexbridge.RunRequest{WaitForAvailability: true, Model: request.Model, Effort: request.Effort, Prompt: request.Prompt, Instructions: request.Instructions, Phase: request.Phase}
		// Canonical sessions include context/progress tools for conversation. This
		// already-reserved workflow offers only its fixed structured response.
		if request.Model != b.task.Choice.Model || request.Phase != "author" || request.Delegated {
			return errBackgroundReceipt
		}
		for _, tool := range request.Tools {
			if strings.HasPrefix(tool.Description, b.task.Response.Name+":") {
				bridge.Tools = append(bridge.Tools, codexbridge.Tool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
			}
		}
		if len(bridge.Tools) != 1 {
			return errBackgroundReceipt
		}
		responseAlias := bridge.Tools[0].Name
		policyGuard, _, err := b.server.aiContext(b.task.Choice.Model)
		if err != nil {
			return err
		}
		toolJSON, _ := json.Marshal(bridge.Tools)
		if err = policyGuard.Check(bridge.Prompt, bridge.Instructions, string(toolJSON)); err != nil {
			return err
		}
		bridge.AuthorizeAdmission = b.authorize
		var generated atomic.Bool
		bridge.BeforeGeneration = func(ctx context.Context) error {
			if availability != nil {
				if err := availability(false); err != nil {
					return err
				}
			}
			if err := b.dispatch(ctx); err != nil {
				return err
			}
			generated.Store(true)
			return nil
		}
		var mu sync.Mutex
		events := []adapter.AgentEvent{}
		type exchange struct {
			Call   adapter.AgentToolCall   `json:"call"`
			Answer adapter.AgentToolAnswer `json:"answer"`
		}
		tools := []exchange{}
		size := 0
		err = runner.Run(ctx, codexbridge.Owner{ID: b.task.Owner, Session: ctx, Policy: policy}, bridge, func(event codexbridge.RunEvent) error {
			if event.Type == "waiting" {
				if availability != nil {
					return availability(true)
				}
				return nil
			}
			if event.Type == "admitted" {
				return b.authorize(ctx)
			}
			if err := b.authorize(ctx); err != nil {
				return err
			}
			held := adapter.AgentEvent{Type: event.Type, ID: event.ID, Text: event.Text, Phase: event.Phase, Attempt: event.Attempt}
			mu.Lock()
			defer mu.Unlock()
			size += len(held.Text) + 256
			if size > 1<<20 {
				return codexbridge.ErrLimit
			}
			events = append(events, held)
			return callbacks.Event(ctx, held)
		}, func(call context.Context, tool codexbridge.ToolCall) (codexbridge.ToolAnswer, error) {
			mu.Lock()
			defer mu.Unlock()
			if err := b.authorize(call); err != nil {
				return codexbridge.ToolAnswer{}, err
			}
			heldCall := adapter.AgentToolCall{ID: tool.ID, Name: tool.Name, Arguments: append(json.RawMessage(nil), tool.Arguments...)}
			answer := adapter.AgentToolAnswer{Text: "Only the fixed host response tool is available.", IsError: true}
			var err error
			if tool.Name == responseAlias {
				answer, err = callbacks.Tool(call, heldCall)
			}
			size += len(tool.Arguments) + len(answer.Text) + 512
			if size > 1<<20 {
				return codexbridge.ToolAnswer{}, codexbridge.ErrLimit
			}
			tools = append(tools, exchange{Call: heldCall, Answer: answer})
			return codexbridge.ToolAnswer{Text: answer.Text, IsError: answer.IsError}, err
		})
		mu.Lock()
		raw, _ := json.Marshal(struct {
			Events []adapter.AgentEvent `json:"events"`
			Tools  []exchange           `json:"tools"`
		}{events, tools})
		mu.Unlock()
		if saveErr := b.retain(func(r *backgroundModelReceipt) error {
			if generated.Load() {
				r.Wire = raw
			}
			return nil
		}); saveErr != nil {
			b.latch(saveErr)
			return saveErr
		}
		// Never expose the adapter's retryable admission sentinel. This workflow
		// owns every retry decision and has reserved just this one invocation.
		if err != nil {
			held := aiAgentFailure(err)
			var known *aifailure.Failure
			if errors.As(held, &known) {
				copy := *known
				if generated.Load() {
					copy.Outcome = "unknown"
				}
				held = &copy
				if saveErr := b.retain(func(r *backgroundModelReceipt) error { r.Failure = &copy; return nil }); saveErr != nil {
					b.latch(saveErr)
					return saveErr
				}
			}
			// This workflow owns retry decisions. End its canonical invocation before
			// returning a not-started busy class; no synthetic callback marks admission.
			b.latch(held)
			b.cancel()
			return held
		}
		return nil
	}}, nil
}

func backgroundFailure(held contract.Failure) *aifailure.Failure {
	raw, _ := json.Marshal(held)
	var result aifailure.Failure
	if json.Unmarshal(raw, &result) != nil {
		return aifailure.New("unknown", "model", "unknown")
	}
	return &result
}

// Convert at capability boundaries so canonical normalization preserves the
// Desk catalogue code, bounded host detail, completion and refusal metadata.
func backgroundCanonicalFailure(err error, native bool) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	var canonical *failure.Error
	if errors.As(err, &canonical) {
		return canonical
	}
	if native {
		err = aiAgentFailure(err)
	} else {
		err = aiModelFailure(err)
	}
	var held *aifailure.Failure
	if errors.As(err, &held) {
		raw, _ := json.Marshal(held)
		var value contract.Failure
		if json.Unmarshal(raw, &value) == nil {
			return failure.NewError(value)
		}
	}
	return failure.NewError(failure.Normalize(err, "model", time.Now()))
}

func (s *Server) backgroundCredential(ctx context.Context, choice backgroundModelChoice) (string, error) {
	c, err := s.selectedAIConnection(backgroundAIRequest(ctx, choice), true)
	if err != nil {
		return "", err
	}
	if c == nil {
		return "", errBackgroundReceipt
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	if c.Assistant.Engine != "codex" {
		stored, err := s.assistant.readKeyNamed(connectionKeyName(c))
		if err != nil {
			return "", err
		}
		// The fingerprint remains process-local. No credential bytes enter a journal.
		raw = append(raw, []byte(stored.key)...)
	}
	return digestOf(raw), nil
}

// Once observed, identity and completed bytes cannot be replaced or cleared by
// another journal update. Recovery may change the original attempt state only.
func backgroundReceiptTransition(old, next inputResearchAttempt) bool {
	a, b := old.Background, next.Background
	if a == nil {
		return b == nil || old.Kind == "model" && (old.State == "started" || old.State == "waiting")
	}
	if b == nil {
		return false
	}
	if old.State != "started" && old.State != "waiting" {
		return reflect.DeepEqual(a, b)
	}
	if a.Version != b.Version || a.Owner != b.Owner || a.Digest != b.Digest || a.Project != b.Project || a.Profile != b.Profile || a.Choice != b.Choice || a.Dispatched && !b.Dispatched {
		return false
	}
	if len(a.Response) > 0 && !bytes.Equal(a.Response, b.Response) || len(a.Wire) > 0 && !bytes.Equal(a.Wire, b.Wire) || a.Failure != nil && !reflect.DeepEqual(a.Failure, b.Failure) {
		return false
	}
	if a.Checkpoint != nil {
		ac, bc := a.Checkpoint, b.Checkpoint
		if bc == nil || ac.ID != bc.ID || ac.RequestKey != bc.RequestKey || ac.Unit != bc.Unit || ac.Limit != bc.Limit || !reflect.DeepEqual(ac.RequestCap, bc.RequestCap) || bc.Used < ac.Used {
			return false
		}
	}
	return true
}
