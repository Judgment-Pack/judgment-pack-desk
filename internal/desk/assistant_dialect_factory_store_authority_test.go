//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Synthetic complete-read evidence only. The source/run/session/phase, durable
// journal, lease, artifact queue and preparation view below are actual Desk
// owners. This fixture does not enroll the legacy read as production evidence.
type assistantDialectOwnedFixtureEvidence struct {
	assistantDialectFactoryFixtureEvidence
	calls  int
	fail   error
	mutate func(*runcontext.DialectSnapshot)
}

func (f *assistantDialectOwnedFixtureEvidence) completeDialectSnapshotUsingView(ctx context.Context, run assistantRunRecord, op assistantRunOperation, read assistantDialectRead, view inputPreparationReader) (runcontext.DialectSnapshot, runcontext.DialectSnapshot, error) {
	f.calls++
	empty := runcontext.DialectSnapshot{}
	if _, leaked := view.(inputPreparationRead); leaked {
		return empty, empty, errors.New("borrowed view leaked Close")
	}
	if f.fail != nil {
		return empty, empty, f.fail
	}
	original, err := f.completeDialectSnapshot(ctx, run, op, read)
	if err != nil {
		return empty, empty, err
	}
	current, err := view.Get(read.ID)
	if err != nil || current.SHA256 != original.Revision {
		return empty, empty, errAssistantRunScope
	}
	var fresh runcontext.DialectSnapshot
	if err = json.Unmarshal(assistantChatJSON(original), &fresh); err != nil {
		return empty, empty, err
	}
	if f.mutate != nil {
		f.mutate(&fresh)
	}
	return original, fresh, nil
}

type assistantDialectAuthorityFixture struct {
	server    *Server
	token     string
	execution *assistantRunExecution
	ctx       context.Context
	store     *assistantDialectFactoryStore
	owner     *assistantDialectDraftPutOwner
	evidence  *assistantDialectOwnedFixtureEvidence
	raw       []byte
	seed      assistantDialectDraftCustody
	saved     inputPreparationReply
}

func assistantDialectAuthoritySetup(t *testing.T) *assistantDialectAuthorityFixture {
	t.Helper()
	return assistantDialectAuthoritySetupWithProvider(t, nil, nil)
}
func assistantDialectAuthoritySetupWithProvider(t *testing.T, provider *persistentInjectedArtifactProvider, registry *retainedArtifactProviderRegistry) *assistantDialectAuthorityFixture {
	t.Helper()
	s, token, e := assistantDialectAuthorityExecutionSetup(t, provider, registry)
	return assistantDialectAuthoritySetupWithExecution(t, s, token, e)
}

// Keep the real watcher and execution owner in their caller's clock domain.
// Queue fixtures can then put only provider operations in a synctest bubble.
func assistantDialectAuthorityExecutionSetup(t *testing.T, provider *persistentInjectedArtifactProvider, registry *retainedArtifactProviderRegistry) (*Server, string, *assistantRunExecution) {
	t.Helper()
	monitorChecked := make(chan *assistantRunExecution, 1)
	s, token, e := assistantExecutionFixtureSource(t, "http://127.0.0.1:1", "work", nil, func(s *Server, _ string, start assistantRunStart) assistantRunStart {
		if s.testAssistantRunMonitorChecked != nil {
			t.Fatal("monitor barrier already installed")
		}
		// The owner still performs its actual periodic authorization. Park it
		// only after Authorize releases private custody so the fixture's
		// deliberate nonblocking reservations have no unrelated competitor.
		s.testAssistantRunMonitorChecked = func(ctx context.Context, checked *assistantRunExecution) {
			select {
			case monitorChecked <- checked:
			case <-ctx.Done():
				return
			}
			<-ctx.Done()
		}
		if provider != nil {
			injectedArtifactAttach(s, provider, registry)
		}
		return start
	})
	select {
	case checked := <-monitorChecked:
		if checked != e {
			t.Fatal("monitor checked a foreign execution")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("actual periodic authorization did not finish")
	}
	return s, token, e
}

func assistantDialectAuthoritySetupWithExecution(t *testing.T, s *Server, token string, e *assistantRunExecution) *assistantDialectAuthorityFixture {
	t.Helper()
	// No assistantExecutionOpen: this test starts no Runtime, model or provider.
	saved, err := s.preparationRepository().CompareAndSwap(t.Context(), repositoryDocument(1), "")
	assistantRunMust(t, err)
	template, read, _ := assistantDialectFactoryFixtureRun(t)
	c := assistantDialectFactoryFixtureCandidate()
	c.ID, c.Revision = saved.Document.ID, saved.SHA256
	read.ID, read.Revision = c.ID, c.Revision
	var view map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(read.View, &view))
	view["id"], view["revision"] = assistantChatJSON(read.ID), assistantChatJSON(read.Revision)
	read.View = assistantChatJSON(view)
	readIn := assistantHostIntent{CallID: "read-call", Root: true, Arguments: assistantChatJSON(map[string]string{"id": c.ID, "packKey": c.PackKey})}
	tool, err := assistantHostJSON(json.RawMessage(read.View))
	assistantRunMust(t, err)
	readOp := assistantRunOperation{ID: assistantDialectOperationID(readIn), CallID: "read-host", Kind: "host", Name: "read_dialect", Intent: assistantChatJSON(readIn), State: "completed", Result: assistantChatJSON(assistantHostResult{Result: tool.Value().JSON(), DialectRead: &read})}
	in := assistantHostIntent{CallID: "propose-call", Root: true, Arguments: assistantChatJSON(map[string]any{"id": c.ID, "revision": c.Revision, "packKey": c.PackKey, "packDigest": c.PackDigest, "facts": c.Facts, "evidence": c.Evidence, "summary": c.Summary})}
	in.DialectAction = &assistantDialectActionMetadata{ID: assistantDialectActionID(e.record.Binding, in), CreatedAt: "2026-01-01T00:00:00Z", AfterTurnID: e.record.Binding.RequestID, ReadOperationID: readOp.ID}
	root := assistantRunOperation{ID: assistantDialectOperationID(in), CallID: "propose-host", Kind: "host", Name: "propose_dialect", Intent: assistantChatJSON(in), State: "dispatched"}
	template.Events[0].Event.ToolCall.Args, template.Events[1].Event.ToolCall.Args = readIn.Arguments, in.Arguments
	assistantRunMust(t, e.writer.change(t.Context(), false, func(run *assistantRunRecord) (bool, error) {
		run.Operations = []assistantRunOperation{readOp, root}
		run.Events = append(run.Events, template.Events...)
		run.State = "running"
		return true, nil
	}))
	e.canonicalMu.Lock()
	e.generation, e.lifecycle = 1, "running"
	e.canonicalMu.Unlock()
	ctx := e.generationContext(t.Context(), 1)
	ctx = context.WithValue(runcontext.WithCallID(ctx, in.CallID), assistantHostRootKey{}, true)
	ctx = context.WithValue(ctx, assistantDialectPreparedKey{}, in.DialectAction)
	ctx = context.WithValue(ctx, assistantExternalPhysicalKey{}, assistantExternalPhysical{ID: root.ID, Name: root.Name, Intent: in})
	fields := map[string]any{"version": 2, "attemptId": strings.Repeat("a", 32), "candidate": c, "stage": "candidate", "previousReceipt": nil}
	fields["fingerprint"] = assistantDialectFactoryFixtureHash(t, map[string]any{"attemptId": fields["attemptId"], "candidate": c})
	fields["digest"] = assistantDialectFactoryFixtureHash(t, fields)
	raw := assistantChatJSON(fields)
	seed := assistantDialectDraftCustody{Version: 1, HostOperationID: root.ID, ActionID: in.DialectAction.ID, ReadOperationID: readOp.ID, ResourceDigest: assistantDialectResource(read.ID, read.PackKey), AttemptID: strings.Repeat("a", 32), CandidateFingerprint: assistantDialectFactoryFixtureHash(t, c), DraftFingerprint: fields["fingerprint"].(string)}
	evidence := &assistantDialectOwnedFixtureEvidence{assistantDialectFactoryFixtureEvidence: assistantDialectFactoryFixtureEvidence{run: e.record.ID, binding: e.record.Binding, readID: readOp.ID, snapshot: runcontext.DialectSnapshot{ID: c.ID, Revision: c.Revision, PackKey: c.PackKey, PackJSON: c.PackJSON, Columns: c.Columns, ResearchIndex: c.ResearchIndex, RetainedRead: c.RetainedRead, Conversions: map[string]runcontext.DialectConversion{}, FactTargets: []string{"/company/country"}, EvidenceRequirements: []string{"license"}}}}
	store, err := newAssistantDialectFactoryStore(ctx, e, root.ID, evidence)
	assistantRunMust(t, err)
	return &assistantDialectAuthorityFixture{server: s, token: token, execution: e, ctx: ctx, store: store, owner: store.put.(*assistantDialectDraftPutOwner), evidence: evidence, raw: raw, seed: seed, saved: saved}
}

func (f *assistantDialectAuthorityFixture) reserve(t *testing.T, bind bool) assistantRunRecord {
	t.Helper()
	cp, _, err := readAssistantDialectFactoryDraft(f.raw)
	assistantRunMust(t, err)
	before := assistantExecutionRecord(t, f.execution)
	intent := assistantDialectDraftIntent{Reference: artifactReference(f.raw), PackageDigest: cp.Digest}
	intent.Receipt = assistantDialectDraftReceipt(before, f.seed, "candidate", intent)
	assistantRunMust(t, f.execution.writer.changeDialectDraft(f.ctx, f.owner, "candidate", intent, func(run *assistantRunRecord) (bool, error) {
		held := f.seed
		held.Candidate.Intent = &intent
		run.DialectDrafts = []assistantDialectDraftCustody{held}
		run.DialectArtifactOwner = f.owner.enrollment.Owner
		return true, nil
	}))
	run := assistantExecutionRecord(t, f.execution)
	if bind {
		assistantRunMust(t, f.owner.bindReservation(run, "candidate", run.Revision, *run.DialectDrafts[0].Candidate.Intent))
	}
	return run
}
func (f *assistantDialectAuthorityFixture) put() error {
	_, err := f.owner.putDialectDraft(f.ctx, artifactReference(f.raw), f.raw)
	return err
}

func assistantDialectAssertReleased(t *testing.T, f *assistantDialectAuthorityFixture) {
	t.Helper()
	for name, mu := range map[string]*sync.Mutex{"writes": f.server.writes, "canonical": &f.execution.canonicalMu, "writer": &f.owner.writer.mu, "lease": &f.owner.lease.mu} {
		if !mu.TryLock() {
			t.Fatalf("%s custody leaked", name)
		}
		mu.Unlock()
	}
	lock, err := f.server.tryPrivateDataLockContext(t.Context(), true)
	assistantRunMust(t, err)
	assistantRunMust(t, lock.Close())
	view, err := f.server.preparationRepository().TryRead(t.Context())
	assistantRunMust(t, err)
	assistantRunMust(t, view.Close())
}

func TestAssistantDialectDraftAuthorityOriginalReservationAndNoRetry(t *testing.T) {
	f := assistantDialectAuthoritySetup(t)
	ack, err := f.store.retainDraft(f.ctx, f.raw)
	assistantRunMust(t, err)
	if ack.Receipt == "" || f.evidence.calls < 1 || f.evidence.calls > 2 {
		t.Fatal("unexpected full guard authentication count", f.evidence.calls)
	}
	run := assistantExecutionRecord(t, f.execution)
	if run.DialectDrafts[0].Candidate.Receipt == nil {
		t.Fatal("exact manifest receipt missing")
	}
	if err = f.put(); err == nil {
		t.Fatal("owner retried consumed publication")
	}
	// Completed delivery uses Get, never arms the owner or repeats its Put.
	before := f.evidence.calls
	duplicate, err := f.store.retainDraft(f.ctx, f.raw)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(ack, duplicate) || f.evidence.calls != before {
		t.Fatal("duplicate repeated effect admission")
	}
	assistantDialectAssertReleased(t, f)
}

func TestAssistantDialectDraftAuthorityReservationRecreationAndRevision(t *testing.T) {
	for _, mode := range []string{"pending-construction", "wrong-bound-revision", "stale-writer-revision", "no-binding", "wrong-ref", "wrong-generation", "missing-generation", "wrong-root", "malformed-raw"} {
		t.Run(mode, func(t *testing.T) {
			f := assistantDialectAuthoritySetup(t)
			run := f.reserve(t, false)
			if mode == "pending-construction" {
				if _, err := newAssistantDialectDraftPutOwner(f.ctx, f.execution, f.owner.host, f.evidence); err == nil {
					t.Fatal("recreated owner used existing pending intent")
				}
				return
			}
			if mode == "wrong-bound-revision" {
				if f.owner.bindReservation(run, "candidate", run.Revision+1, *run.DialectDrafts[0].Candidate.Intent) == nil {
					t.Fatal("wrong committed revision armed")
				}
				return
			}
			if mode != "no-binding" {
				assistantRunMust(t, f.owner.bindReservation(run, "candidate", run.Revision, *run.DialectDrafts[0].Candidate.Intent))
			}
			ref := artifactReference(f.raw)
			switch mode {
			case "stale-writer-revision":
				f.owner.writer.mu.Lock()
				f.owner.writer.revision++
				f.owner.writer.mu.Unlock()
			case "wrong-ref":
				ref.Bytes++
			case "wrong-generation":
				f.execution.canonicalMu.Lock()
				f.execution.generation++
				f.execution.canonicalMu.Unlock()
			case "missing-generation":
				f.ctx = context.WithValue(f.ctx, assistantInvocationGenerationKey{}, nil)
			case "wrong-root":
				f.ctx = context.WithValue(f.ctx, assistantHostRootKey{}, false)
			case "malformed-raw":
				if _, err := f.owner.putDialectDraft(f.ctx, ref, []byte(`{}`)); err == nil {
					t.Fatal("malformed raw accepted")
				}
				if f.put() == nil {
					t.Fatal("malformed attempt became a fresh effect")
				}
				return
			}
			if _, err := f.owner.putDialectDraft(f.ctx, ref, f.raw); err == nil {
				t.Fatal("invalid original reservation published")
			}
			root := artifactAuthorityTestRoot(t, f.server)
			assertGuardedArtifactAbsent(t, root, f.raw)
			if f.put() == nil {
				t.Fatal("failed owner call retried")
			}
			assistantDialectAssertReleased(t, f)
		})
	}
}

func TestAssistantDialectDraftAuthorityTryOnlyCustody(t *testing.T) {
	for _, lockName := range []string{"writes", "canonical", "writer", "lease", "private", "preparation"} {
		t.Run(lockName, func(t *testing.T) {
			f := assistantDialectAuthoritySetup(t)
			f.reserve(t, true)
			// Avoid the optional missing-directory setup branch so exactly one fresh
			// effect admission is exercised for each independently occupied owner.
			artifactAuthorityTestRoot(t, f.server)
			var release func()
			switch lockName {
			case "writes":
				f.server.writes.Lock()
				release = f.server.writes.Unlock
			case "canonical":
				f.execution.canonicalMu.Lock()
				release = f.execution.canonicalMu.Unlock
			case "writer":
				f.owner.writer.mu.Lock()
				release = f.owner.writer.mu.Unlock
			case "lease":
				f.owner.lease.mu.Lock()
				release = f.owner.lease.mu.Unlock
			case "private":
				lock, err := f.server.privateDataLockContext(t.Context(), true)
				assistantRunMust(t, err)
				release = func() { lock.Close() }
			case "preparation":
				root, err := f.server.openInputPreparations(false)
				assistantRunMust(t, err)
				lock := artifactAuthorityTestLock(t, root)
				release = func() { lock.Close(); root.Close() }
			}
			result := make(chan error, 1)
			go func() { result <- f.put() }()
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("occupied custody admitted effect")
				}
			case <-time.After(time.Second):
				release()
				t.Fatal("guard waited on occupied owner")
			}
			release()
			assistantDialectAssertReleased(t, f)
		})
	}
}

// The actual file queue holds no host custody. The injected variant explicitly
// delegates that same file queue; older authority-only wrappers cannot qualify.
type assistantDialectQueueFixtureOwner struct {
	fileRetainedArtifactRepository
	entered chan struct{}
	once    sync.Once
}

func (o *assistantDialectQueueFixtureOwner) PutGuarded(ctx context.Context, raw []byte, acquire func(context.Context) (retainedArtifactGuard, error)) (retainedArtifactRef, error) {
	o.once.Do(func() { close(o.entered) })
	return o.fileRetainedArtifactRepository.PutGuarded(ctx, raw, acquire)
}

func TestAssistantDialectDraftAuthorityQueuedCurrentness(t *testing.T) {
	for _, mode := range []string{"phase", "source", "delete", "policy", "session", "generation", "lease", "stop", "cancel", "preparation"} {
		for _, injected := range []bool{false, true} {
			name := mode + "/file"
			if injected {
				name = mode + "/injected"
			}
			t.Run(name, func(t *testing.T) {
				f := assistantDialectAuthoritySetup(t)
				f.reserve(t, true)
				root := artifactAuthorityTestRoot(t, f.server)
				lock := artifactAuthorityTestLock(t, root)
				defer lock.Close()
				entered := make(chan struct{})
				cancel := func() {}
				if mode == "cancel" {
					f.ctx, cancel = context.WithCancel(f.ctx)
				}
				defer cancel()
				fileOwner := fileRetainedArtifactRepository{open: func(create bool) (*os.Root, error) {
					select {
					case <-entered:
					default:
						close(entered)
					}
					return f.server.openRetainedArtifacts(create)
				}}
				if injected {
					f.owner.objects = &assistantDialectQueueFixtureOwner{fileRetainedArtifactRepository: fileOwner, entered: make(chan struct{})}
				} else {
					f.owner.objects = fileOwner
				}
				result := make(chan error, 1)
				go func() { result <- f.put() }()
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("artifact queue not entered")
				}
				// Existing artifact root is locked before entry, so publication cannot
				// acquire a guard until this test completes its authority transition.
				assistantDialectAssertReleased(t, f)
				switch mode {
				case "phase", "source", "delete":
					saved := policyWorkspaceRead(t, f.server, f.token, "/api/conversations")
					var doc conversationDocument
					assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
					if mode == "delete" {
						doc.Chats = []json.RawMessage{}
					} else if mode == "phase" {
						doc.Chats[0] = bytes.Replace(doc.Chats[0], []byte(`"work"`), []byte(`"plan"`), 1)
					} else {
						doc.Chats[0] = bytes.Replace(doc.Chats[0], []byte("Review exact input"), []byte("Changed latest request"), 1)
					}
					response := policyWorkspaceCall(f.server, f.token, "PUT", "/api/conversations", saved.SHA256, doc)
					if response.Code != 200 {
						t.Fatal("source transition did not complete while queued", response.Code, response.Body.String())
					}
				case "policy":
					f.server.signIn.mu.Lock()
					f.server.signIn.generation++
					f.server.signIn.mu.Unlock()
				case "session":
					f.execution.request.Header.Set("Authorization", "Bearer invalid-session")
				case "generation":
					f.execution.canonicalMu.Lock()
					f.execution.generation++
					f.execution.canonicalMu.Unlock()
				case "lease":
					assistantRunMust(t, f.owner.lease.Close())
				case "stop":
					f.execution.canonicalMu.Lock()
					f.execution.stopRequested = true
					f.execution.canonicalMu.Unlock()
				case "cancel":
					cancel()
				case "preparation":
					doc := f.saved.Document
					doc.Name = "Changed during artifact wait"
					_, err := f.server.preparationRepository().CompareAndSwap(t.Context(), doc, f.saved.SHA256)
					assistantRunMust(t, err)
				}
				assistantRunMust(t, lock.Close())
				select {
				case err := <-result:
					if err == nil {
						t.Fatal("queued changed authority published")
					}
				case <-time.After(2 * time.Second):
					t.Fatal("queued put failed to drain")
				}
				assertGuardedArtifactAbsent(t, root, f.raw)
				if f.put() == nil {
					t.Fatal("queued refusal retried")
				}
			})
		}
	}
}

func TestAssistantDialectDraftAuthorityEvidenceAndRelease(t *testing.T) {
	for _, mode := range []string{"legacy-only", "missing", "borrowed-view-failure", "closed-lease", "recovery-error", "changed-profile"} {
		t.Run(mode, func(t *testing.T) {
			f := assistantDialectAuthoritySetup(t)
			if mode == "legacy-only" || mode == "missing" {
				var evidence assistantDialectFactoryReadEvidence
				if mode == "legacy-only" {
					evidence = f.evidence.assistantDialectFactoryFixtureEvidence
				}
				if _, err := newAssistantDialectDraftPutOwner(f.ctx, f.execution, f.owner.host, evidence); err == nil {
					t.Fatal("missing owned complete-read capability accepted")
				}
				return
			}
			f.reserve(t, true)
			switch mode {
			case "borrowed-view-failure":
				f.evidence.fail = errors.New("complete read no longer current")
			case "closed-lease":
				assistantRunMust(t, f.owner.lease.Close())
			case "recovery-error":
				f.owner.lease.mu.Lock()
				f.owner.lease.recoveryErr = errors.New("recovery unknown")
				f.owner.lease.mu.Unlock()
			case "changed-profile":
				assistantRunMust(t, os.WriteFile(filepath.Join(f.server.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":1,"api":{"inherit":true},"projectInstructions":"changed"}`), 0600))
			}
			if f.put() == nil {
				t.Fatal("invalid complete guard accepted")
			}
			assistantDialectAssertReleased(t, f)
		})
	}
}

func TestAssistantDialectDraftAuthorityCompleteSnapshotMutationTwins(t *testing.T) {
	mutations := map[string]func(*runcontext.DialectSnapshot){
		"pack-bytes":        func(s *runcontext.DialectSnapshot) { s.PackJSON += "\n" },
		"dialect-bytes":     func(s *runcontext.DialectSnapshot) { value := *s.DialectJSON + "\n"; s.DialectJSON = &value },
		"declarations-null": func(s *runcontext.DialectSnapshot) { s.ResearchIndex = nil },
		"declarations-empty": func(s *runcontext.DialectSnapshot) {
			s.ResearchIndex, _ = runcontext.DialectResearchIndexOf([]runcontext.DialectResearchRevision{})
		},
		"declarations-order": func(s *runcontext.DialectSnapshot) {
			s.ResearchIndex.Revisions[0], s.ResearchIndex.Revisions[1] = s.ResearchIndex.Revisions[1], s.ResearchIndex.Revisions[0]
		},
		"fields-null": func(s *runcontext.DialectSnapshot) {
			s.ResearchIndex.Revisions[1].Fields = nil
			s.ResearchIndex.Revisions[1].Digest = nil
			s.ResearchIndex.Revisions[1].Bytes = nil
		},
		"retained-occurrence": func(s *runcontext.DialectSnapshot) { s.RetainedRead.Occurrence.Kind = "other" },
		"retained-frame":      func(s *runcontext.DialectSnapshot) { s.RetainedRead.FramePointer = "/other" },
		"sample":              func(s *runcontext.DialectSnapshot) { s.Columns[0].Samples[0].ValueJSON = `"US"` },
		"history-revision":    func(s *runcontext.DialectSnapshot) { s.Revision = strings.Repeat("f", 64) },
		"conversions": func(s *runcontext.DialectSnapshot) {
			s.Conversions["/company/country"] = runcontext.DialectConversion{Kind: "integer"}
		},
		"fact-inventory": func(s *runcontext.DialectSnapshot) { s.FactTargets = []string{} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			f := assistantDialectAuthoritySetup(t)
			original := &f.evidence.snapshot
			dialect := `{"original":true}`
			original.DialectJSON = &dialect
			original.ResearchIndex, _ = runcontext.DialectResearchIndexOf([]runcontext.DialectResearchRevision{{Revision: "old", Fields: nil}, {Revision: "new", Fields: []runcontext.DialectResearchField{}}})
			original.RetainedRead = &runcontext.DialectRetainedRead{DocumentJSON: `{"retained":true}`, Occurrence: runcontext.DialectRetainedOccurrence{Kind: "root"}, FramePointer: "/frame"}
			cp, _, err := readAssistantDialectFactoryDraft(f.raw)
			assistantRunMust(t, err)
			c := cp.Candidate
			c.ResearchIndex = original.ResearchIndex
			c.RetainedRead = original.RetainedRead
			digest := "sha256:" + digestOf([]byte(dialect))
			c.DialectDigest = &digest
			fields := map[string]any{"version": 2, "attemptId": cp.AttemptID, "candidate": c, "stage": "candidate", "previousReceipt": nil}
			fields["fingerprint"] = assistantDialectFactoryFixtureHash(t, map[string]any{"attemptId": cp.AttemptID, "candidate": c})
			fields["digest"] = assistantDialectFactoryFixtureHash(t, fields)
			f.raw = assistantChatJSON(fields)
			f.seed.DraftFingerprint = fields["fingerprint"].(string)
			f.seed.CandidateFingerprint = assistantDialectFactoryFixtureHash(t, c)
			f.evidence.mutate = mutate
			f.reserve(t, true)
			if f.put() == nil {
				t.Fatal("changed full snapshot admitted")
			}
			assistantDialectAssertReleased(t, f)
		})
	}
}

type assistantDialectPreparationFixtureAdapter struct {
	inputPreparationRepository
	closeFailure error
	tries        int
}
type assistantDialectPreparationFixtureRead struct {
	inputPreparationRead
	failure error
}

func (v *assistantDialectPreparationFixtureRead) Close() error {
	return errors.Join(v.inputPreparationRead.Close(), v.failure)
}
func (a *assistantDialectPreparationFixtureAdapter) TryRead(ctx context.Context) (inputPreparationRead, error) {
	a.tries++
	v, err := a.inputPreparationRepository.TryRead(ctx)
	if err != nil {
		return nil, err
	}
	return &assistantDialectPreparationFixtureRead{inputPreparationRead: v, failure: a.closeFailure}, nil
}

func TestAssistantDialectDraftAuthorityHeldViewExcludesReplacement(t *testing.T) {
	for _, injected := range []bool{false, true} {
		name := "file"
		if injected {
			name = "injected"
		}
		t.Run(name, func(t *testing.T) {
			f := assistantDialectAuthoritySetup(t)
			f.reserve(t, true)
			if injected {
				f.server.inputPreparations = &assistantDialectPreparationFixtureAdapter{inputPreparationRepository: f.server.preparationRepository()}
			}
			p, err := readAssistantDialectDraftPublication(f.raw)
			assistantRunMust(t, err)
			reservation := *f.owner.reservations["candidate"]
			guard, err := f.owner.acquireGuard(f.ctx, reservation, p)
			assistantRunMust(t, err)
			// A replacement cannot complete while the actual held read view is live.
			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
			doc := f.saved.Document
			doc.Name = "Blocked replacement"
			_, err = f.server.preparationRepository().CompareAndSwap(ctx, doc, f.saved.SHA256)
			cancel()
			if err == nil {
				guard.Close()
				t.Fatal("held preparation view allowed replacement")
			}
			assistantRunMust(t, guard.Check(f.ctx))
			assistantRunMust(t, guard.Close())
			assistantRunMust(t, guard.Close())
			if guard.Check(f.ctx) == nil {
				t.Fatal("closed guard reused")
			}
			_, err = f.server.preparationRepository().CompareAndSwap(t.Context(), doc, f.saved.SHA256)
			assistantRunMust(t, err)
			assistantDialectAssertReleased(t, f)
		})
	}
}

func TestAssistantDialectDraftAuthorityCleanupUncertaintyNeverRetries(t *testing.T) {
	f := assistantDialectAuthoritySetup(t)
	// Use an existing directory: one payload commit, followed by one injected
	// Close failure after the real preparation view has released its exclusion.
	artifactAuthorityTestRoot(t, f.server)
	closeFailure := errors.New("fixture preparation close failed")
	adapter := &assistantDialectPreparationFixtureAdapter{inputPreparationRepository: f.server.preparationRepository(), closeFailure: closeFailure}
	f.server.inputPreparations = adapter
	if _, err := f.store.retainDraft(f.ctx, f.raw); !errors.Is(err, closeFailure) {
		t.Fatal("close uncertainty acknowledged", err)
	}
	run := assistantExecutionRecord(t, f.execution)
	held := run.DialectDrafts[0]
	if held.Candidate.Intent == nil || held.Candidate.Receipt != nil || held.Uncertainty == "" {
		t.Fatal("uncertain commit lost pending custody")
	}
	raw, err := f.server.artifactRepository().Get(t.Context(), artifactReference(f.raw))
	assistantRunMust(t, err)
	if !bytes.Equal(raw, f.raw) {
		t.Fatal("uncertain physical object changed")
	}
	tries := adapter.tries
	if _, err = f.store.retainDraft(f.ctx, f.raw); err == nil {
		t.Fatal("uncertain slot replayed")
	}
	if adapter.tries != tries {
		t.Fatal("uncertain delivery reacquired effect view")
	}
	// Cleanup checks use normal Close again; the injected failure was exercised.
	adapter.closeFailure = nil
	assistantDialectAssertReleased(t, f)
}

// Full valid ledger boundary, with opaque fixture-owned runtime bytes rather
// than padded/invalid package objects. The original revision 8 projects to 9
// for reservation and 10 for receipt settlement, crossing a decimal carry.
func TestAssistantDialectFactoryStoreCompletedLedgerBoundary(t *testing.T) {
	for _, over := range []bool{false, true} {
		t.Run(map[bool]string{false: "inclusive", true: "one-over"}[over], func(t *testing.T) {
			f := assistantDialectAuthoritySetup(t)
			store := f.execution.writer.lease.store
			ledger, err := store.readSnapshot(t.Context())
			assistantRunMust(t, err)
			current, err := assistantRunBound(&ledger, f.execution.record.Binding, f.execution.record.ID)
			assistantRunMust(t, err)
			current.Revision = 10
			var cp runcontext.DialectDraftCheckpoint
			assistantRunMust(t, json.Unmarshal(f.raw, &cp))
			held := f.seed
			intent := assistantDialectDraftIntent{Reference: artifactReference(f.raw), PackageDigest: cp.Digest}
			intent.Receipt = "ddr-" + digestOf(assistantChatJSON([]any{current.Binding.Project, current.ID, held.HostOperationID, held.ActionID, held.AttemptID, "candidate", intent.Reference, intent.PackageDigest, intent.PreviousReceipt}))
			held.Candidate.Intent, held.Candidate.Receipt = &intent, &intent
			current.DialectDrafts = []assistantDialectDraftCustody{held}
			current.DialectArtifactOwner = f.owner.enrollment.Owner
			filler := assistantRunOperation{ID: "d5-ledger-boundary", CallID: "d5-ledger-boundary", Kind: "runtime", Name: "fixture-bytes", State: "completed", Intent: []byte{}, Result: []byte(`{}`)}
			current.Operations = append(current.Operations, filler)
			baseline, err := json.Marshal(ledger)
			assistantRunMust(t, err)
			target := assistantRunRecordLimit
			if over {
				target++
			}
			remaining := target - len(baseline)
			if remaining < 4 {
				t.Fatal("boundary fixture has no room for runtime bytes")
			}
			// Operation byte slices use base64 in the existing ledger. Complete
			// groups add four bytes; the bounded name accounts for the remainder.
			filler.Intent = bytes.Repeat([]byte("a"), remaining/4*3)
			filler.Name += strings.Repeat("x", remaining%4)
			current.Operations[len(current.Operations)-1] = filler
			expected, err := json.Marshal(ledger)
			assistantRunMust(t, err)
			if len(expected) != target || validateAssistantRunLedger(ledger, store.project) != nil {
				t.Fatal("authored completed ledger is not the exact valid target", len(expected))
			}
			// Both twins have a valid full-byte decoder witness. For one-over,
			// the old mistaken revision-9 projection is exactly the 16 MiB cap.
			if over {
				current.Revision = 9
			}
			witness, err := json.Marshal(ledger)
			assistantRunMust(t, err)
			if len(witness) != assistantRunRecordLimit {
				t.Fatal("inclusive decoder witness is not exact", len(witness))
			}
			_, err = decodeAssistantRunStorage(store.name, witness)
			assistantRunMust(t, err)
			current.Revision, current.DialectDrafts, current.DialectArtifactOwner = 8, nil, ""
			original := *current
			_, err = store.transaction(t.Context(), func(saved *assistantRunLedger) (bool, error) {
				run, err := assistantRunBound(saved, original.Binding, original.ID)
				if err != nil {
					return false, err
				}
				*run = original
				return true, nil
			})
			assistantRunMust(t, err)
			f.execution.writer.mu.Lock()
			f.execution.writer.revision = 8
			f.execution.writer.mu.Unlock()
			before := assistantRunStorageValidationRead(t, store)
			root := artifactAuthorityTestRoot(t, f.server)
			ack, err := f.store.retainDraft(f.ctx, f.raw)
			if over {
				if codeOf(err) != CodeTooLarge {
					t.Fatal("completed one-over ledger was admitted", err)
				}
				after := assistantRunStorageValidationRead(t, store)
				if !bytes.Equal(before, after) || len(f.owner.reservations) != 0 {
					t.Fatal("preflight failure persisted intent or armed effect owner")
				}
				assertGuardedArtifactAbsent(t, root, f.raw)
				return
			}
			assistantRunMust(t, err)
			after := assistantRunStorageValidationRead(t, store)
			if ack.Receipt != intent.Receipt || !bytes.Equal(after, expected) {
				t.Fatal("inclusive receipt CAS differed from exact completed projection", len(after))
			}
			assistantDialectAssertReleased(t, f)
		})
	}
}
