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
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestAssistantDialectFactoryInjectedActualGuard(t *testing.T) {
	p, registry, _, providerPath := injectedArtifactProviderFixture(t)
	f := assistantDialectAuthoritySetupWithProvider(t, p, registry)
	owner := f.owner
	if !owner.original() || owner.injected == nil || owner.objects != owner.injected || owner.inspection != owner.injected {
		t.Fatal("factory did not retain its actual enrolled repository")
	}
	work, executor := owner.lease.work, owner.lease.file
	ack, err := f.store.retainDraft(f.ctx, f.raw)
	assistantRunMust(t, err)
	if ack.Receipt == "" {
		t.Fatal("publication missing receipt")
	}
	ref := artifactReference(f.raw)
	path := filepath.Join(providerPath, "d5-objects", owner.enrollment.Owner, owner.enrollment.Namespace, ref.SHA256)
	raw, err := os.ReadFile(path)
	assistantRunMust(t, err)
	if !bytes.Equal(raw, f.raw) {
		t.Fatal("scoped raw bytes changed")
	}
	got, err := owner.inspection.Get(f.ctx, ref)
	assistantRunMust(t, err)
	if !bytes.Equal(got, f.raw) {
		t.Fatal("exact inspection changed bytes")
	}
	duplicate, err := f.store.retainDraft(f.ctx, f.raw)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(duplicate, ack) {
		t.Fatal("completed duplicate changed receipt")
	}
	if _, err := owner.inspection.Get(f.ctx, artifactReference([]byte("unselected"))); err == nil {
		t.Fatal("guessed reference reached provider")
	}
	if owner.lease.work != work || owner.lease.file != executor {
		t.Fatal("borrowed descriptors replaced")
	}
	_, err = work.Stat()
	assistantRunMust(t, err)
	_, err = executor.Stat()
	assistantRunMust(t, err)
	assistantDialectAssertReleased(t, f)
	if err := f.put(); err == nil {
		t.Fatal("consumed publication replayed")
	}
}

func TestAssistantDialectFactoryInjectedOriginalIdentity(t *testing.T) {
	p, registry, _, _ := injectedArtifactProviderFixture(t)
	f := assistantDialectAuthoritySetupWithProvider(t, p, registry)
	f.reserve(t, true)
	ownerCopy := copiedValue(f.owner)
	if _, err := ownerCopy.acquireHostHold(f.ctx); err == nil {
		t.Fatal("copied owner admitted host custody")
	}
	pub, err := readAssistantDialectDraftPublication(f.raw)
	assistantRunMust(t, err)
	reservation := *f.owner.reservations["candidate"]
	reservation.consumed = true
	guard, err := f.owner.acquireGuard(f.ctx, reservation, pub)
	assistantRunMust(t, err)
	g := guard.(*assistantDialectDraftGuard)
	copied := *g
	if err := copied.Check(f.ctx); err == nil {
		t.Fatal("copied publication guard admitted")
	}
	if g.data == nil || g.private == nil || g.view == nil {
		t.Fatal("original concrete custody incomplete")
	}
	assistantRunMust(t, g.Close())
	if err := g.Check(f.ctx); err == nil {
		t.Fatal("closed guard admitted")
	}
	assistantDialectAssertReleased(t, f)
}

func TestAssistantDialectFactoryInjectedAuthorityPinContention(t *testing.T) {
	for _, pin := range []string{"sessions", "signIn"} {
		t.Run(pin, func(t *testing.T) {
			p, registry, _, _ := injectedArtifactProviderFixture(t)
			f := assistantDialectAuthoritySetupWithProvider(t, p, registry)
			if pin == "signIn" && f.server.signIn == nil {
				t.Fatal("fixture must establish an actual sign-in policy")
			}
			f.reserve(t, true)
			pub, err := readAssistantDialectDraftPublication(f.raw)
			assistantRunMust(t, err)
			reservation := *f.owner.reservations["candidate"]
			actual, err := f.owner.acquireGuard(f.ctx, reservation, pub)
			assistantRunMust(t, err)
			g := actual.(*assistantDialectDraftGuard)
			op, err := p.pin()
			assistantRunMust(t, err)
			var release func()
			if pin == "sessions" {
				f.server.sessions.mu.Lock()
				release = f.server.sessions.mu.Unlock
			} else {
				f.server.signIn.mu.Lock()
				release = f.server.signIn.mu.Unlock
			}
			authority, err := g.holdInjectedAuthority(f.ctx, f.owner.registration, f.owner.injected.binding, op)
			release()
			if err == nil || authority != nil {
				t.Fatal("contended actual authority admitted publication")
			}
			assistantRunMust(t, g.Close())
			assistantRunMust(t, op.finish())
			assistantDialectAssertReleased(t, f)
		})
	}
}

func assistantDialectInjectedAbsent(t *testing.T, f *assistantDialectAuthorityFixture, providerPath string) {
	t.Helper()
	ref := artifactReference(f.raw)
	_, err := os.Stat(filepath.Join(providerPath, "d5-objects", f.owner.enrollment.Owner, f.owner.enrollment.Namespace, ref.SHA256))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused publication left a raw provider object", err)
	}
}
func assistantDialectInjectedAuthReleased(t *testing.T, f *assistantDialectAuthorityFixture) {
	t.Helper()
	if f.server.signIn == nil {
		t.Fatal("fixture requires real sign-in state")
	}
	for name, mu := range map[string]*sync.Mutex{"sessions": &f.server.sessions.mu, "signIn": &f.server.signIn.mu} {
		if !mu.TryLock() {
			t.Fatalf("%s pin leaked", name)
		}
		mu.Unlock()
	}
}

func TestAssistantDialectFactoryInjectedQueueAndRevocation(t *testing.T) {
	for _, fact := range []string{"session", "policy", "generation", "stop", "cancel", "source", "phase"} {
		t.Run("drain-"+fact, func(t *testing.T) {
			// The actual monitor must authorize before entering fake time. Its
			// real watcher and execution owner remain outside this bubble;
			// provider pin/finish/Close all remain inside it.
			s, token, e := assistantDialectAuthorityExecutionSetup(t, nil, nil)
			synctest.Test(t, func(t *testing.T) {
				p, registry, root, path := injectedArtifactProviderFixture(t)
				injectedArtifactAttach(s, p, registry)
				f := assistantDialectAuthoritySetupWithExecution(t, s, token, e)
				if f.owner.registration == nil || f.owner.registration.provider != p || f.owner.registration.registry != registry {
					t.Fatal("factory did not retain the bubble's actual provider registration")
				}
				f.reserve(t, true)
				ctx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				f.ctx = ctx
				queue := injectedArtifactQueue(t, root)
				defer queue.Close()
				result := make(chan error, 1)
				go func() { result <- f.put() }()
				synctest.Wait()
				assistantDialectAssertReleased(t, f)
				assistantDialectInjectedAuthReleased(t, f)
				switch fact {
				case "session":
					f.server.sessions.revoke(bearerOf(f.execution.request))
				case "policy":
					f.server.signIn.mu.Lock()
					f.server.signIn.generation++
					f.server.signIn.mu.Unlock()
				case "generation":
					f.execution.canonicalMu.Lock()
					f.execution.generation++
					f.execution.canonicalMu.Unlock()
				case "stop":
					f.execution.canonicalMu.Lock()
					f.execution.stopRequested = true
					f.execution.canonicalMu.Unlock()
				case "cancel":
					cancel()
				case "source", "phase":
					saved := policyWorkspaceRead(t, f.server, f.token, "/api/conversations")
					var doc conversationDocument
					assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
					if fact == "phase" {
						doc.Chats[0] = bytes.Replace(doc.Chats[0], []byte(`"work"`), []byte(`"plan"`), 1)
					} else {
						doc.Chats[0] = bytes.Replace(doc.Chats[0], []byte("Review exact input"), []byte("Changed latest request"), 1)
					}
					response := policyWorkspaceCall(f.server, f.token, "PUT", "/api/conversations", saved.SHA256, doc)
					if response.Code != 200 {
						t.Fatal("source transition failed while provider drained", response.Code)
					}
				}
				assistantRunMust(t, queue.Close())
				if err := <-result; err == nil {
					t.Fatal("drain lost fresh authority")
				}
				assistantDialectInjectedAbsent(t, f, path)
				if f.put() == nil {
					t.Fatal("refused drained attempt replayed")
				}
				// Close and join the provider here. Outer server cleanup sees
				// an already-closed registry and no live bubble WaitGroup.
				assistantRunMust(t, registry.Close())
			})
		})
	}
	for _, fact := range []string{"session", "policy", "expiry", "actor"} {
		t.Run("after-full-before-pins-"+fact, func(t *testing.T) {
			p, registry, _, path := injectedArtifactProviderFixture(t)
			f := assistantDialectAuthoritySetupWithProvider(t, p, registry)
			f.reserve(t, true)
			reached := false
			testHookDialectArtifactOwnerStep = func(step string) error {
				if step != "factory-before-authority" || reached {
					return nil
				}
				reached = true
				if fact == "policy" {
					f.server.signIn.mu.Lock()
					f.server.signIn.generation++
					f.server.signIn.mu.Unlock()
					return nil
				}
				if fact == "session" {
					f.server.sessions.revoke(bearerOf(f.execution.request))
					return nil
				}
				f.server.sessions.mu.Lock()
				handle := f.server.sessions.handle(bearerOf(f.execution.request))
				held, ok := f.server.sessions.live[handle]
				if !ok {
					t.Error("fixture lost actual session")
				}
				if fact == "expiry" {
					held.expires = time.Now().Add(-time.Second)
				} else {
					held.subject = "changed-actor"
				}
				f.server.sessions.live[handle] = held
				f.server.sessions.mu.Unlock()
				return nil
			}
			defer func() { testHookDialectArtifactOwnerStep = nil }()
			err := f.put()
			testHookDialectArtifactOwnerStep = nil
			if err == nil || !reached {
				t.Fatal("authority gap mutation was accepted or untested", err)
			}
			assistantDialectInjectedAbsent(t, f, path)
			if fact == "session" || fact == "expiry" {
				// Revocation and expiry both cancel the actual execution owner. Join its
				// terminal cleanup before probing mutexes that lease Close uses.
				// A publication guard leak would prevent this join from finishing.
				select {
				case <-f.execution.done:
				case <-time.After(3 * time.Second):
					t.Fatal("revoked execution owner did not drain")
				}
			}
			assistantDialectAssertReleased(t, f)
			assistantDialectInjectedAuthReleased(t, f)
		})
	}
	t.Run("single-final-queue-try", func(t *testing.T) {
		p, registry, root, path := injectedArtifactProviderFixture(t)
		f := assistantDialectAuthoritySetupWithProvider(t, p, registry)
		f.reserve(t, true)
		tries := 0
		var competitor *os.File
		testHookDialectArtifactOwnerStep = func(step string) error {
			if step == "factory-before-provider-queue" {
				tries++
				competitor = injectedArtifactQueue(t, root)
			}
			return nil
		}
		defer func() {
			testHookDialectArtifactOwnerStep = nil
			if competitor != nil {
				competitor.Close()
			}
		}()
		err := f.put()
		testHookDialectArtifactOwnerStep = nil
		if !errors.Is(err, errRetainedArtifactBusy) || tries != 1 {
			t.Fatal("final provider try retried/waited", err, tries)
		}
		assistantRunMust(t, competitor.Close())
		assistantDialectInjectedAbsent(t, f, path)
		assistantDialectAssertReleased(t, f)
		assistantDialectInjectedAuthReleased(t, f)
	})
}

func TestAssistantDialectFactoryInjectedCleanupOrder(t *testing.T) {
	p, registry, root, _ := injectedArtifactProviderFixture(t)
	f := assistantDialectAuthoritySetupWithProvider(t, p, registry)
	f.reserve(t, true)
	var order []string
	privateReads := 0
	testHookDialectArtifactOwnerStep = func(step string) error {
		switch step {
		case "factory-after-provider-queue":
			for name, mu := range map[string]*sync.Mutex{"writes": f.server.writes, "canonical": &f.execution.canonicalMu, "writer": &f.owner.writer.mu, "lease": &f.owner.lease.mu, "sessions": &f.server.sessions.mu, "signIn": &f.server.signIn.mu} {
				if mu.TryLock() {
					mu.Unlock()
					t.Errorf("%s was not held under queue", name)
				}
			}
			testHookPrivateRead = func(string) error {
				privateReads++
				return errors.New("private/source reentry under provider exclusion")
			}
		case "injected-view-queue.close":
			order = append(order, "queue")
			testHookPrivateRead = nil
			queue := injectedArtifactQueue(t, root)
			assistantRunMust(t, queue.Close())
			for name, mu := range map[string]*sync.Mutex{"sessions": &f.server.sessions.mu, "signIn": &f.server.signIn.mu, "writer": &f.owner.writer.mu} {
				if mu.TryLock() {
					mu.Unlock()
					t.Errorf("%s released before queue-close boundary", name)
				}
			}
		case "factory-data.close":
			order = append(order, "data")
			assistantDialectInjectedAuthReleased(t, f)
			if f.owner.writer.mu.TryLock() {
				f.owner.writer.mu.Unlock()
				t.Error("writer released before data/private cleanup")
			}
			private, err := f.server.tryPrivateDataLockContext(f.ctx, true)
			if err == nil {
				private.Close()
				t.Error("private released before data cleanup")
			}
			view, err := f.server.preparationRepository().TryRead(f.ctx)
			assistantRunMust(t, err)
			assistantRunMust(t, view.Close())
		case "factory-before-operation-finish":
			order = append(order, "operation")
			assistantDialectAssertReleased(t, f)
			assistantDialectInjectedAuthReleased(t, f)
		}
		return nil
	}
	defer func() { testHookDialectArtifactOwnerStep = nil; testHookPrivateRead = nil }()
	assistantRunMust(t, f.put())
	testHookDialectArtifactOwnerStep, testHookPrivateRead = nil, nil
	if privateReads != 0 || !reflect.DeepEqual(order, []string{"queue", "data", "operation"}) {
		t.Fatal("cleanup order or queue reentry changed", order, privateReads)
	}
}

func TestAssistantDialectFactoryInjectedCloseAndNoReplay(t *testing.T) {
	for _, step := range []string{"injected-object.write", "injected-object.sync", "injected-object.file-close", "injected-object.publish", "injected-object.directory-sync", "injected-object.readback", "injected-object-folder.close", "injected-view-queue.close", "factory-data.close", "factory-before-operation-finish"} {
		t.Run(step, func(t *testing.T) {
			p, registry, _, path := injectedArtifactProviderFixture(t)
			f := assistantDialectAuthoritySetupWithProvider(t, p, registry)
			failure := errors.New("handwritten factory publication uncertainty")
			reached, dataCloses := false, 0
			testHookDialectArtifactOwnerStep = func(actual string) error {
				if actual == "factory-data.close" {
					dataCloses++
				}
				// The first data Close is reservation cleanup. This case targets
				// the actual publication guard after bytes have landed.
				if actual == step && !reached && (step != "factory-data.close" || dataCloses == 2) {
					reached = true
					return failure
				}
				return nil
			}
			defer func() { testHookDialectArtifactOwnerStep = nil }()
			ack, err := f.store.retainDraft(f.ctx, f.raw)
			testHookDialectArtifactOwnerStep = nil
			if !reached || !errors.Is(err, failure) || ack.Receipt != "" {
				t.Fatal("uncertain publication acknowledged or hook untested", err, reached)
			}
			run := assistantExecutionRecord(t, f.execution)
			held := run.DialectDrafts[0]
			if run.DialectArtifactOwner != f.owner.enrollment.Owner || held.Candidate.Intent == nil || held.Candidate.Receipt != nil || held.Uncertainty == "" {
				t.Fatal("uncertain publication lost owner/pending fence")
			}
			ref := artifactReference(f.raw)
			landed, readErr := os.ReadFile(filepath.Join(path, "d5-objects", f.owner.enrollment.Owner, f.owner.enrollment.Namespace, ref.SHA256))
			if readErr == nil && !bytes.Equal(landed, f.raw) {
				t.Fatal("uncertain object bytes changed")
			}
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				t.Fatal(readErr)
			}
			if _, err := newAssistantDialectFactoryStore(f.ctx, f.execution, f.owner.host, f.evidence); err == nil {
				t.Fatal("saved bytes reconstructed a live raw owner")
			}
			assistantRunMust(t, f.execution.writer.Stop(f.ctx))
			reloaded, err := f.owner.lease.store.readSnapshot(t.Context())
			assistantRunMust(t, err)
			if reloaded.Runs[0].DialectArtifactOwner != f.owner.enrollment.Owner || reloaded.Runs[0].DialectDrafts[0].Candidate.Receipt != nil || reloaded.Runs[0].DialectDrafts[0].Uncertainty == "" {
				t.Fatal("Stop/reload lost unknown-effect fence")
			}
			drains := 0
			testHookDialectArtifactOwnerStep = func(actual string) error {
				if actual == "injected-before-drain" {
					drains++
				}
				return nil
			}
			if _, err := f.store.retainDraft(f.ctx, f.raw); err == nil {
				t.Fatal("unknown publication replayed")
			}
			testHookDialectArtifactOwnerStep = nil
			if drains != 0 {
				t.Fatal("unknown publication reentered provider")
			}
			assistantDialectAssertReleased(t, f)
			assistantDialectInjectedAuthReleased(t, f)
		})
	}
	for _, step := range []string{"injected-object-read.file-close", "injected-object-folder.close", "injected-view-queue.close", "factory-data.close", "factory-before-operation-finish"} {
		t.Run("inspection-"+step, func(t *testing.T) {
			p, registry, _, _ := injectedArtifactProviderFixture(t)
			f := assistantDialectAuthoritySetupWithProvider(t, p, registry)
			_, err := f.store.retainDraft(f.ctx, f.raw)
			assistantRunMust(t, err)
			before := assistantRunStorageValidationRead(t, f.owner.lease.store)
			failure, reached := errors.New("handwritten exact inspection uncertainty"), false
			testHookDialectArtifactOwnerStep = func(actual string) error {
				if actual == step {
					reached = true
					return failure
				}
				return nil
			}
			defer func() { testHookDialectArtifactOwnerStep = nil }()
			got, err := f.owner.inspection.Get(f.ctx, artifactReference(f.raw))
			testHookDialectArtifactOwnerStep = nil
			if !reached || !errors.Is(err, failure) || got != nil {
				t.Fatal("failed inspection leaked bytes", err, reached)
			}
			if !bytes.Equal(before, assistantRunStorageValidationRead(t, f.owner.lease.store)) {
				t.Fatal("inspection uncertainty rewrote receipt")
			}
			assistantDialectAssertReleased(t, f)
			assistantDialectInjectedAuthReleased(t, f)
		})
	}
}

func TestAssistantDialectFactoryInjectedCurrentOperation(t *testing.T) {
	p, registry, _, _ := injectedArtifactProviderFixture(t)
	f := assistantDialectAuthoritySetupWithProvider(t, p, registry)
	f.reserve(t, true)
	pub, err := readAssistantDialectDraftPublication(f.raw)
	assistantRunMust(t, err)
	reservation := *f.owner.reservations["candidate"]
	actual, err := f.owner.acquireGuard(f.ctx, reservation, pub)
	assistantRunMust(t, err)
	g := actual.(*assistantDialectDraftGuard)
	defer g.Close()
	old, err := p.pin()
	assistantRunMust(t, err)
	assistantRunMust(t, old.finish())
	if authority, err := g.holdInjectedAuthority(f.ctx, f.owner.registration, f.owner.injected.binding, old); err == nil || authority != nil {
		t.Fatal("finished operation pin reused")
	}
	current, err := p.pin()
	assistantRunMust(t, err)
	defer current.finish()
	authority, err := g.holdInjectedAuthority(f.ctx, f.owner.registration, f.owner.injected.binding, current)
	assistantRunMust(t, err)
	if authority.operation != current {
		t.Fatal("authority did not retain exact current operation")
	}
	foreign, err := p.pin()
	assistantRunMust(t, err)
	defer foreign.finish()
	authority.operation = foreign
	if authority.Check(f.ctx) == nil {
		t.Fatal("same-provider wrong operation pointer accepted")
	}
	authority.operation = current
	assistantRunMust(t, authority.Close())
	assistantRunMust(t, g.Close())
	assistantDialectAssertReleased(t, f)
}

func TestAssistantDialectFactoryInjectedProviderCloseWins(t *testing.T) {
	p, registry, _, path := injectedArtifactProviderFixture(t)
	f := assistantDialectAuthoritySetupWithProvider(t, p, registry)
	f.reserve(t, true)
	revoked, closed := make(chan struct{}), make(chan error, 1)
	reached := false
	testHookDialectArtifactOwnerStep = func(step string) error {
		if step == "factory-after-provider-queue" && !reached {
			reached = true
			go func() { closed <- p.Close() }()
			<-revoked
		}
		if step == "injected-provider-revoked" {
			close(revoked)
		}
		return nil
	}
	defer func() { testHookDialectArtifactOwnerStep = nil }()
	err := f.put()
	if !reached || err == nil {
		t.Fatal("provider revocation acknowledged publication", err)
	}
	assistantRunMust(t, <-closed)
	testHookDialectArtifactOwnerStep = nil
	assistantDialectInjectedAbsent(t, f, path)
	assistantDialectAssertReleased(t, f)
	assistantDialectInjectedAuthReleased(t, f)
}

type assistantDialectFactoryMarkerGuard struct{ artifactAuthorityTestGuard }

func (*assistantDialectFactoryMarkerGuard) injectedArtifactPhysicalGuard() {}

func TestAssistantDialectFactoryInjectedConcreteGuardRefusals(t *testing.T) {
	for _, mode := range []string{"generic", "marker", "inspection", "foreign-factory"} {
		t.Run(mode, func(t *testing.T) {
			p, registry, _, path := injectedArtifactProviderFixture(t)
			f := assistantDialectAuthoritySetupWithProvider(t, p, registry)
			f.reserve(t, true)
			var acquire func(context.Context) (retainedArtifactGuard, error)
			switch mode {
			case "generic":
				acquire = func(context.Context) (retainedArtifactGuard, error) {
					return &artifactAuthorityTestGuard{close: func() error { return nil }}, nil
				}
			case "marker":
				acquire = func(context.Context) (retainedArtifactGuard, error) {
					return &assistantDialectFactoryMarkerGuard{artifactAuthorityTestGuard{close: func() error { return nil }}}, nil
				}
			case "inspection":
				f.owner.mu.Lock()
				f.owner.reservations["candidate"].consumed = true
				f.owner.mu.Unlock()
				acquire = func(ctx context.Context) (retainedArtifactGuard, error) {
					return f.owner.acquireInspectionGuard(ctx, artifactReference(f.raw))
				}
			case "foreign-factory":
				other := assistantDialectAuthoritySetupWithProvider(t, p, registry)
				other.reserve(t, true)
				publication, err := readAssistantDialectDraftPublication(other.raw)
				assistantRunMust(t, err)
				reservation := *other.owner.reservations["candidate"]
				reservation.consumed = true
				acquire = func(context.Context) (retainedArtifactGuard, error) {
					return other.owner.acquireGuard(other.ctx, reservation, publication)
				}
			}
			ref, err := f.owner.injected.PutGuarded(f.ctx, f.raw, acquire)
			if err == nil || ref != (retainedArtifactRef{}) {
				t.Fatal("non-original publication guard qualified", mode, err)
			}
			assistantDialectInjectedAbsent(t, f, path)
			assistantDialectAssertReleased(t, f)
			assistantDialectInjectedAuthReleased(t, f)
		})
	}
}
