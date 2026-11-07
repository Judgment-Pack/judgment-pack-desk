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
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/eino"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
)

const observationFixturePrompt = "Observe one fixed response."

type observationStoreFixture struct {
	store   assistantRunStore
	source  assistantRunSource
	profile assistantObservationProfile
	record  assistantRunRecord
	lease   *assistantRunLease
	writer  *assistantRunWriter
	e       *assistantRunExecution
}

func observationFixtureInput() *runcontext.Input {
	one, off := 1, false
	return &runcontext.Input{Phase: "work", Connection: "legacy-api", RequestCap: &one, Memory: &off, ArtifactProposals: &off, CurrentRequest: &runcontext.CurrentRequest{Message: observationFixturePrompt, Input: observationFixturePrompt}}
}

func observationFixtureSession(input *runcontext.Input) runcontext.Session {
	named := runcontext.RunModels{Critic: "fixture", Child: "fixture"}
	run := &adapter.Run{Wire: "openai-compatible", ModelName: "fixture", Context: input}
	return runcontext.Session{Prompt: observationFixturePrompt, AllowConversation: true, Critic: "off", Context: input, Model: &runcontext.ModelSession{Family: "openai-compatible", Model: "fixture", Thinking: "off"}, PolicyLegacyModels: &named, PolicyFamilies: adapter.PolicyFamilies(run, named), PolicyConnections: append(adapter.PolicyConnectionIDs(run), named.Critic, named.Child)}
}

func newObservationStoreFixture(t *testing.T) observationStoreFixture {
	t.Helper()
	if !custodyChecked {
		t.Fatal("observation custody evidence requires a supported private store")
	}
	store, binding, _ := assistantRunFixture(t)
	prepared, err := runcontext.ContextualSession(t.Context(), observationFixtureSession(observationFixtureInput()))
	assistantRunMust(t, err)
	cp, err := prepared.Context.Snapshot("")
	prepared.Context.Close()
	assistantRunMust(t, err)
	original, request := []byte(`{"fixed":"observation"}`), []byte(`{"request":"one"}`)
	binding.Owner = assistantRunOwner{Kind: assistantSettingsObservationPurpose, ID: binding.ConnectionID, Revision: digestOf(original)}
	binding.RequestDigest, binding.SourceDigests = digestOf(request), []string{digestOf(original)}
	binding.ExecutionKey, binding.Unit, binding.Phase = cp.RequestKey, cp.Unit, cp.Phase
	source := assistantRunSource{Start: assistantRunStart{Purpose: assistantSettingsObservationPurpose}, Binding: binding, Original: original, Request: request}
	profile := assistantObservationProfile{Kind: "capability", Model: "fixture", OutputCeiling: 128, Digest: digestOf([]byte("fixed-profile-1"))}
	lease, err := store.Acquire(t.Context())
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = lease.Close() })
	record, created, err := lease.Admit(t.Context(), binding, "observation-one", digestOf([]byte("fixed-start")), func() error { return nil })
	assistantRunMust(t, err)
	if !created {
		t.Fatal("fixture reused an admission")
	}
	writer, err := lease.Writer(t.Context(), binding, record.ID)
	assistantRunMust(t, err)
	assistantRunMust(t, writer.initializeObservation(t.Context(), source, profile, cp))
	record, err = store.Read(t.Context(), binding, record.ID)
	assistantRunMust(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	e := &assistantRunExecution{source: source, record: record, writer: writer, ctx: ctx}
	e.cancel = func() { e.closeCanonicalDispatch(); cancel() }
	t.Cleanup(cancel)
	assistantRunMust(t, e.openObservationClaim(t.Context(), profile))
	return observationStoreFixture{store, source, profile, record, lease, writer, e}
}

func (f observationStoreFixture) saved(t *testing.T) assistantRunRecord {
	t.Helper()
	run, err := f.store.Read(t.Context(), f.source.Binding, f.record.ID)
	assistantRunMust(t, err)
	return run
}

func (f observationStoreFixture) input(t *testing.T) *runcontext.Input {
	t.Helper()
	input := observationFixtureInput()
	assistantRunMust(t, f.e.bindObservationInput(f.e.ctx, input))
	return input
}

func (f observationStoreFixture) opening(t *testing.T) runcontext.RunCommit {
	t.Helper()
	raw, err := json.Marshal(f.record.Checkpoint)
	assistantRunMust(t, err)
	value, err := runcontext.ParseJSON(raw)
	assistantRunMust(t, err)
	value.Set("durable", runcontext.MustParseJSON(`{"dispatches":[],"observations":[],"children":[]}`))
	value.Set("digest", runcontext.String(runcontext.RecordDigest(value)))
	return runcontext.RunCommit{RunID: f.record.Observation.RunID, CommitID: "initial", Checkpoint: value.JSON(), Events: [][]byte{}}
}

func observationFixtureReply() *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"P\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))}
}

func (f observationStoreFixture) run(t *testing.T, input *runcontext.Input, calls *int) *adapter.Run {
	t.Helper()
	return &adapter.Run{Wire: "openai-compatible", ModelName: "fixture", Thinking: "off", Critic: "off", AllowConversation: true, Prompt: observationFixturePrompt, Context: input, RequestOutputCeiling: 128, Model: func(context.Context, string, relay.Request) (*http.Response, error) {
		*calls++
		if f.e.canonicalBinding == nil || f.e.canonicalBinding.Writer.Store != f.e.observation.store {
			t.Fatal("physical fixture request preceded original-writer Attach")
		}
		return observationFixtureReply(), nil
	}}
}

type observationFixtureLink struct {
	claim  *assistantObservationClaim
	before func(*runcontext.SessionContext)
}

func (link observationFixtureLink) Attach(sc *runcontext.SessionContext) {
	if link.before != nil {
		link.before(sc)
	}
	link.claim.Attach(sc)
}

func TestAssistantSettingsObservationActualStartupAndOriginalWriter(t *testing.T) {
	f := newObservationStoreFixture(t)
	head, err := f.e.observation.store.Load(t.Context(), f.record.Observation.RunID)
	assistantRunMust(t, err)
	if head != nil || f.record.Canonical != nil || !f.record.CanonicalRequired || f.record.State != "admitted" {
		t.Fatal("prepared admission opened a canonical head")
	}
	input := f.input(t)
	if input.Execution != nil || input.Persistence != nil || input.Checkpoint != nil || input.RunStore.Resume {
		t.Fatal("fresh Observe input was prebound or disguised as resume")
	}
	opened, err := runcontext.ContextualSession(f.e.ctx, observationFixtureSession(input))
	assistantRunMust(t, err)
	defer opened.Context.Close()
	original := opened.Context.Persistence("author")
	if f.e.canonicalBinding == nil || original.Writer != f.e.canonicalBinding.Writer || original == f.e.canonicalBinding || original.Writer.Store != f.e.observation.store || original.Writer.ID != f.record.Observation.RunID {
		t.Fatal("Attach did not capture the actual original writer through its binding copy")
	}
	saved := f.saved(t)
	if !assistantCanonicalActive(saved) || saved.Checkpoint.ID != f.record.Observation.RunID || saved.Checkpoint.Used != 0 || saved.State != "running" || len(saved.Canonical.Commits) == 0 {
		t.Fatal("actual startup did not atomically activate its canonical projection")
	}
	assistantRunMust(t, validateAssistantCanonicalProjection(saved))
	if err := f.e.bindCanonicalInput(t.Context(), &runcontext.Input{}); err == nil {
		t.Fatal("normal resume path reopened an observation writer")
	}
	if _, err := f.writer.canonicalStore(t.Context(), saved.Observation.RunID); err == nil {
		t.Fatal("ordinary store constructor reopened observation")
	}
	if err := f.e.bindObservationInput(t.Context(), observationFixtureInput()); err == nil {
		t.Fatal("one execution admitted another fresh opening")
	}
	f.e.cancel()
	assistantRunMust(t, f.e.finalizeCanonicalStop())
	assistantRunMust(t, f.writer.Stop(t.Context()))
	terminal := f.saved(t)
	if terminal.State != "stopped" || terminal.Checkpoint.Status != "stopped" || f.e.canonicalBinding.Writer != original.Writer {
		t.Fatal("Stop lost the original writer or its canonical acknowledgement")
	}
}

func TestAssistantSettingsObservationActualObserveAndDuplicateNoReplay(t *testing.T) {
	f := newObservationStoreFixture(t)
	input, calls := f.input(t), 0
	run := f.run(t, input, &calls)
	result, err := eino.Observe(f.e.ctx, run, adapter.ObservationOptions{})
	assistantRunMust(t, err)
	if calls != 1 || result.Checkpoint.Used != 1 || result.Checkpoint.Limit != 1 || result.Checkpoint.RequestCap == nil || *result.Checkpoint.RequestCap != 1 || result.Receipt.CommitID == "" || result.Checkpoint.Status != "complete" {
		t.Fatal("actual observation lost one-request durable completion", calls)
	}
	if _, err := eino.Observe(f.e.ctx, run, adapter.ObservationOptions{}); err == nil || calls != 1 {
		t.Fatal("duplicate fresh observation replayed")
	}
	assistantRunMust(t, f.writer.State(t.Context(), "complete"))
	before, _ := json.Marshal(f.saved(t))
	existing, created, err := f.lease.Admit(t.Context(), f.source.Binding, f.record.AdmissionID, f.record.BodyDigest, func() error { t.Fatal("duplicate admission revalidated for dispatch"); return nil })
	assistantRunMust(t, err)
	if created || existing.ID != f.record.ID {
		t.Fatal("lost admission response created a replacement")
	}
	assistantRunMust(t, f.lease.Close())
	reopened, err := f.store.Acquire(t.Context())
	assistantRunMust(t, err)
	defer reopened.Close()
	after, _ := json.Marshal(f.saved(t))
	if !bytes.Equal(before, after) || calls != 1 {
		t.Fatal("terminal restart changed receipt or replayed")
	}
}

func TestAssistantSettingsObservationBeforeFirstCommitCancellationAndRecovery(t *testing.T) {
	f := newObservationStoreFixture(t)
	input, calls := f.input(t), 0
	input.Authorize = func(context.Context) error { f.e.cancel(); return context.Canceled }
	if _, err := eino.Observe(f.e.ctx, f.run(t, input, &calls), adapter.ObservationOptions{}); err == nil || calls != 0 {
		t.Fatal("canceled fresh startup dispatched")
	}
	if _, err := f.e.observation.store.Commit(t.Context(), f.opening(t)); err == nil {
		t.Fatal("late first commit activated after cancellation")
	}
	if f.saved(t).Canonical != nil {
		t.Fatal("cancellation manufactured a head")
	}
	assistantRunMust(t, f.e.finalizeCanonicalStop())
	assistantRunMust(t, f.writer.Stop(t.Context()))
	assistantRunMust(t, f.lease.Close())
	lease, err := f.store.Acquire(t.Context())
	assistantRunMust(t, err)
	defer lease.Close()
	saved := f.saved(t)
	if saved.State != "stopped" || saved.Canonical != nil || saved.Checkpoint.Status != "stopped" {
		t.Fatal("prepared cancellation was not retained")
	}
	if err := f.e.openObservationClaim(t.Context(), f.profile); err == nil {
		t.Fatal("recovered stopped admission reopened as fresh")
	}
}

func TestAssistantSettingsObservationAfterFirstCommitBeforeAttachCancellation(t *testing.T) {
	for _, kind := range []string{"stop", "session-revocation"} {
		t.Run(kind, func(t *testing.T) {
			f := newObservationStoreFixture(t)
			input, calls := f.input(t), 0
			var original *runcontext.RunWriter
			input.Link = observationFixtureLink{claim: f.e.observation, before: func(sc *runcontext.SessionContext) {
				if !assistantCanonicalActive(f.saved(t)) || f.e.canonicalBinding != nil {
					t.Fatal("fixture did not stop in committed-before-Attach window")
				}
				original = sc.Persistence("author").Writer
				f.e.cancel() // The service uses this same callback for session revocation.
			}}
			if _, err := eino.Observe(f.e.ctx, f.run(t, input, &calls), adapter.ObservationOptions{}); err == nil || calls != 0 {
				t.Fatal("late Attach reopened model dispatch")
			}
			if f.e.canonicalBinding == nil || f.e.canonicalBinding.Writer != original {
				t.Fatal("late Attach lost the actual original writer")
			}
			assistantRunMust(t, f.e.finalizeCanonicalStop())
			assistantRunMust(t, f.writer.Stop(t.Context()))
			if f.saved(t).Checkpoint.Status != "stopped" {
				t.Fatal("original writer did not retain stopped acknowledgement")
			}
		})
	}
}

func TestAssistantSettingsObservationStartupLostAckAndNoAttachNeverFabricateStop(t *testing.T) {
	for _, mode := range []string{"lost-first-ack", "startup-refusal"} {
		t.Run(mode, func(t *testing.T) {
			f := newObservationStoreFixture(t)
			input, calls := f.input(t), 0
			run := f.run(t, input, &calls)
			injected := false
			if mode == "lost-first-ack" {
				reads := 0
				testHookPrivateRead = func(name string) error {
					if name == f.store.name {
						reads++
						// Load, transaction read, then first replacement read-back.
						if reads == 3 {
							injected = true
							return errors.New("first observation acknowledgement lost")
						}
					}
					return nil
				}
				t.Cleanup(func() { testHookPrivateRead = nil })
			} else {
				run.Host = runcontext.HostFuncs{BriefFunc: func(context.Context) (string, error) {
					injected = true
					return "", errors.New("startup fixture refuses before Attach")
				}}
			}
			_, err := eino.Observe(f.e.ctx, run, adapter.ObservationOptions{})
			testHookPrivateRead = nil
			if err == nil || !injected || calls != 0 || f.e.canonicalBinding != nil {
				t.Fatal("startup fixture did not retain the no-Attach failure", err, injected, calls)
			}
			before := f.saved(t)
			if !assistantCanonicalActive(before) || before.Checkpoint.Status != "running" {
				t.Fatal("startup failure lost its actual committed head")
			}
			f.e.cancel()
			if err := f.e.finalizeCanonicalStop(); err == nil {
				t.Fatal("missing original writer fabricated a stop acknowledgement")
			}
			assistantRunMust(t, f.writer.interruptObservation(t.Context()))
			after := f.saved(t)
			if after.State != "interrupted" || !reflect.DeepEqual(before.Canonical, after.Canonical) || after.Checkpoint.Status != "running" {
				t.Fatal("uncertainty rewrote the acknowledged canonical head")
			}
			assistantRunMust(t, f.lease.Close())
			lease, err := f.store.Acquire(t.Context())
			assistantRunMust(t, err)
			defer lease.Close()
			if err := f.e.openObservationClaim(t.Context(), f.profile); err == nil {
				t.Fatal("uncertain restart reopened fresh execution")
			}
			if _, err := eino.Observe(context.Background(), run, adapter.ObservationOptions{}); err == nil || calls != 0 {
				t.Fatal("lost startup acknowledgement replayed on readmission")
			}
		})
	}
}

func TestAssistantSettingsObservationFirstCommitIdentityAndAuthority(t *testing.T) {
	cases := []string{"run-id", "request-key", "phase", "unit", "cap", "used", "limit", "approval", "source", "revision", "lease"}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			f := newObservationStoreFixture(t)
			commit := f.opening(t)
			value, err := runcontext.ParseJSON(commit.Checkpoint)
			assistantRunMust(t, err)
			switch kind {
			case "run-id":
				commit.RunID = "foreign"
			case "request-key":
				value.Set("requestKey", runcontext.String(digestOf([]byte("other"))))
			case "phase":
				value.Set("phase", runcontext.String("plan"))
			case "unit":
				value.Set("unit", runcontext.String("provider-managed"))
			case "cap":
				value.Set("requestCap", runcontext.Number(2))
			case "used":
				value.Set("used", runcontext.Number(1))
			case "limit":
				value.Set("limit", runcontext.Number(2))
			case "approval":
				value.Set("approval", runcontext.MustParseJSON(`{"state":"asked"}`))
			case "source":
				f.e.source.Original = []byte(`{"fixed":"changed"}`)
			case "revision":
				other, err := f.lease.Writer(t.Context(), f.source.Binding, f.record.ID)
				assistantRunMust(t, err)
				assistantRunMust(t, other.change(t.Context(), false, func(*assistantRunRecord) (bool, error) { return true, nil }))
			case "lease":
				assistantRunMust(t, f.lease.Close())
			}
			value.Set("digest", runcontext.String(runcontext.RecordDigest(value)))
			commit.Checkpoint = value.JSON()
			if _, err := f.e.observation.store.Commit(t.Context(), commit); err == nil {
				t.Fatal("foreign or unauthorized first commit activated", kind)
			}
			if f.saved(t).Canonical != nil {
				t.Fatal("refused first commit left a head")
			}
		})
	}
}

func TestAssistantSettingsObservationCustodyClosedAndAtomic(t *testing.T) {
	f := newObservationStoreFixture(t)
	commit := f.opening(t)
	receipt, err := f.e.observation.store.Commit(t.Context(), commit)
	assistantRunMust(t, err)
	saved := f.saved(t)
	if !assistantCanonicalActive(saved) || saved.Canonical.Head.Revision != receipt.Revision || saved.Canonical.Head.CommitID != commit.CommitID || saved.Checkpoint.ID != saved.Observation.RunID {
		t.Fatal("first commit did not activate head and projection in one replacement")
	}
	before, _ := json.Marshal(saved)
	duplicate, err := f.e.observation.store.Commit(t.Context(), commit)
	assistantRunMust(t, err)
	after, _ := json.Marshal(f.saved(t))
	if receipt != duplicate || !bytes.Equal(before, after) {
		t.Fatal("exact first-commit duplicate changed its receipt or state")
	}
	for _, mutation := range []string{"absent", "unknown", "zero-version", "wrong-run", "wrong-source", "wrong-counter"} {
		t.Run(mutation, func(t *testing.T) {
			raw, _ := json.Marshal(saved)
			var copy assistantRunRecord
			assistantRunMust(t, json.Unmarshal(raw, &copy))
			switch mutation {
			case "absent":
				copy.Observation = nil
			case "zero-version":
				copy.Observation.Version = 0
			case "wrong-run":
				copy.Observation.RunID = "other"
			case "wrong-source":
				copy.Observation.SourceDigest = digestOf([]byte("other"))
			case "wrong-counter":
				copy.Checkpoint.Used = 2
			case "unknown":
				if err := readAssistantObservationCustody(json.RawMessage(`{"version":1,"future":true}`)); err == nil {
					t.Fatal("unknown custody member accepted")
				}
				return
			}
			if err := validateAssistantCanonicalProjection(copy); err == nil {
				t.Fatal("inconsistent retained custody accepted")
			}
		})
	}
}
