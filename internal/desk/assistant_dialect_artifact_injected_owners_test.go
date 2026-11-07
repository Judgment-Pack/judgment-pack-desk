package desk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
)

func injectedArtifactSelection(t *testing.T, raw []byte, publish bool) retainedArtifactInjectedSelection {
	t.Helper()
	s, err := newRetainedArtifactInjectedSelection([]retainedArtifactRef{artifactReference(raw)}, publish)
	assistantRunMust(t, err)
	return s
}
func injectedArtifactPublish(t *testing.T, s *Server, request *http.Request, raw []byte) dialectArtifactOwnerEnrollment {
	t.Helper()
	out, err := s.withDialectArtifactOwner(request, &dialectArtifactInjectedUse{selection: injectedArtifactSelection(t, raw, true), use: func(v *retainedArtifactInjectedView) error {
		return v.PublishExact(request.Context(), artifactReference(raw), raw)
	}})
	assistantRunMust(t, err)
	return out
}
func injectedArtifactInspect(s *Server, request *http.Request, ref retainedArtifactRef) (raw []byte, out dialectArtifactOwnerEnrollment, err error) {
	selection, err := newRetainedArtifactInjectedSelection([]retainedArtifactRef{ref}, false)
	if err != nil {
		return nil, out, err
	}
	out, err = s.withDialectArtifactOwner(request, &dialectArtifactInjectedUse{selection: selection, use: func(v *retainedArtifactInjectedView) error {
		raw, err = v.GetExact(request.Context(), ref)
		return err
	}})
	if err != nil {
		return nil, dialectArtifactOwnerEnrollment{}, err
	}
	return raw, out, nil
}
func injectedArtifactExpectedRows(t *testing.T, s *Server, providerPath string, out dialectArtifactOwnerEnrollment) {
	t.Helper()
	projectInfo, err := s.root.Stat(".")
	assistantRunMust(t, err)
	workInfo, err := s.assistant.root.Stat(".")
	assistantRunMust(t, err)
	lockInfo, err := s.assistant.root.Lstat(".data-work.lock")
	assistantRunMust(t, err)
	// The formula and all wire bytes are handwritten from actual host inputs.
	// Expected bytes are never marshaled from or copied out of produced rows.
	if out.Namespace != digestOf([]byte(s.conversationIdentity())) {
		t.Fatal("namespace did not come from actual saved selection", out.Namespace)
	}
	workID := digestOf([]byte(fmt.Sprintf(`["dialect-artifact-work-v1","%s","%s"]`, identityKey(workInfo), identityKey(lockInfo))))
	owner := []byte(fmt.Sprintf(`{"version":2,"owner":"%s","kind":"injected","provider":"%s","projectFolderIdentity":"%s","workDomain":"%s"}`, out.Owner, injectedFixtureProviderID, identityKey(projectInfo), workID))
	binding := []byte(fmt.Sprintf(`{"version":1,"provider":"%s","owner":"%s","projectFolderIdentity":"%s","workDomain":"%s","namespace":"%s"}`, injectedFixtureProviderID, out.Owner, identityKey(projectInfo), workID, out.Namespace))
	scope := []byte(fmt.Sprintf(`{"version":1,"kind":"native","owner":"%s","namespace":"%s"}`, out.Owner, out.Namespace))
	marker := []byte(fmt.Sprintf(`{"version":1,"owner":"%s"}`, out.Owner))
	ownerPath, scopePath := dialectArtifactOwnerRowPaths(s, out)
	for _, row := range []struct {
		path string
		raw  []byte
	}{
		{ownerPath, owner}, {scopePath, scope},
		{filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker), marker},
		{filepath.Join(providerPath, "d5-owners", out.Owner[:2], out.Owner[2:]+".json"), owner},
		{filepath.Join(providerPath, "d5-native", out.Namespace, out.Owner+".json"), binding},
	} {
		if got := dialectArtifactOwnerRead(t, row.path); !bytes.Equal(got, row.raw) {
			t.Fatal("wrong handwritten immutable row", row.path, string(got), string(row.raw))
		}
	}
	if out.OwnerSHA256 != digestOf(owner) || out.LegacyOwnerSHA256 != "" || out.ScopeSHA256 != digestOf(scope) || out.WorkDomain != workID {
		t.Fatal("injected snapshot used native provenance", out)
	}
	if _, err := os.Stat(dialectArtifactOwnerTablePath(s, "owner-locations", out.Owner[:2], out.Owner[2:]+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("injected enrollment wrote native locator", err)
	}
}

func TestDialectArtifactInjectedPersistentReopen(t *testing.T) {
	for _, mode := range []string{"standalone", "actual-live"} {
		t.Run(mode, func(t *testing.T) {
			projectPath, configPath := t.TempDir(), t.TempDir()
			p, registry, root, providerPath := injectedArtifactProviderFixture(t)
			raw := []byte(" \n{\"candidate\":1.00}\n")
			var s *Server
			var out dialectArtifactOwnerEnrollment
			if mode == "standalone" {
				var request *http.Request
				s, request = dialectArtifactOwnerFixture(t, projectPath, configPath)
				injectedArtifactAttach(s, p, registry)
				out = injectedArtifactPublish(t, s, request, raw)
				injectedArtifactExpectedRows(t, s, providerPath, out)
				dialectArtifactOwnerNoEffects(t, s)
				assistantRunMust(t, s.project.Close())
				assistantRunMust(t, s.assistant.Close())
				s.sessions.close()
				assistantRunMust(t, registry.Close())
			} else {
				s = dialectArtifactExecutionServer(t, Config{ProjectDir: projectPath, DeskConfigDir: configPath})
				injectedArtifactAttach(s, p, registry)
				e := dialectArtifactExecutionStart(t, s, true)
				work, executor := dialectArtifactExecutionFDs(t, e)
				beforeNames, beforeJournal := dialectArtifactExecutionJournal(t, e)
				var err error
				out, err = e.withDialectArtifactOwner(t.Context(), &dialectArtifactInjectedUse{selection: injectedArtifactSelection(t, raw, true), use: func(v *retainedArtifactInjectedView) error {
					return v.PublishExact(t.Context(), artifactReference(raw), raw)
				}})
				assistantRunMust(t, err)
				afterNames, afterJournal := dialectArtifactExecutionJournal(t, e)
				if strings.Join(beforeNames, "\x00") != strings.Join(afterNames, "\x00") || !bytes.Equal(beforeJournal, afterJournal) {
					t.Fatal("injected enrollment introduced a D5 intent or changed the original run journal")
				}
				injectedArtifactExpectedRows(t, s, providerPath, out)
				dialectArtifactExecutionLive(t, e, work, executor)
				dialectArtifactExecutionDrain(t, e)
				assistantRunMust(t, s.Close())
			}
			assistantRunMust(t, root.Close())
			freshRoot, err := os.OpenRoot(providerPath)
			assistantRunMust(t, err)
			defer freshRoot.Close()
			fresh, err := openPersistentInjectedArtifactProvider(t.Context(), injectedFixtureProviderID, freshRoot)
			assistantRunMust(t, err)
			freshRegistry, err := newRetainedArtifactProviderRegistry(fresh)
			assistantRunMust(t, err)
			defer freshRegistry.Close()
			// Reopen real host roots and mint a new authenticated local request.
			// No in-memory owner map or old provider handle reaches this witness.
			s2, request2 := dialectArtifactOwnerFixtureWithStorage(t, projectPath, configPath, true, false)
			injectedArtifactAttach(s2, fresh, freshRegistry)
			got, reopened, err := injectedArtifactInspect(s2, request2, artifactReference(raw))
			assistantRunMust(t, err)
			if !bytes.Equal(got, raw) || reopened.Owner != out.Owner || reopened.Namespace != out.Namespace || reopened.OwnerSHA256 != out.OwnerSHA256 {
				t.Fatal("disk reopen changed scoped binding", reopened, out)
			}
			injectedArtifactExpectedRows(t, s2, providerPath, reopened)
			if got, err := p.Get(t.Context(), artifactReference(raw)); err == nil || got != nil {
				t.Fatal("old provider resurrected")
			}
		})
	}
}

func TestDialectArtifactInjectedPersistentReopenScopeIsolation(t *testing.T) {
	p, registry, _, _ := injectedArtifactProviderFixture(t)
	s1, r1 := dialectArtifactOwnerSetup(t)
	s2, r2 := dialectArtifactOwnerSetup(t)
	injectedArtifactAttach(s1, p, registry)
	injectedArtifactAttach(s2, p, registry)
	a, b := []byte("scope one exact payload"), []byte("scope two exact payload")
	one := injectedArtifactPublish(t, s1, r1, a)
	two := injectedArtifactPublish(t, s2, r2, b)
	if one.Owner == two.Owner || one.Namespace == two.Namespace {
		t.Fatal("actual projects shared scope")
	}
	if got, out, err := injectedArtifactInspect(s2, r2, artifactReference(a)); !errors.Is(err, os.ErrNotExist) || got != nil || out != (dialectArtifactOwnerEnrollment{}) {
		t.Fatal("guessed foreign digest exposed scope", string(got), out, err)
	}
	injectedArtifactPublish(t, s2, r2, a)
	got, _, err := injectedArtifactInspect(s1, r1, artifactReference(a))
	assistantRunMust(t, err)
	if !bytes.Equal(got, a) {
		t.Fatal("identical cross-scope payload changed")
	}
	if got, err := p.Get(t.Context(), artifactReference(a)); !errors.Is(err, os.ErrNotExist) || got != nil {
		t.Fatal("generic Get exposed D5 bytes", err)
	}
}

func TestDialectArtifactInjectedTrustedResolution(t *testing.T) {
	for _, mutation := range []string{"removed-registration", "different-instance", "copied-instance", "wrapper", "missing-host", "missing-binding", "header-corrupt", "header-missing"} {
		t.Run(mutation, func(t *testing.T) {
			p, registry, root, providerPath := injectedArtifactProviderFixture(t)
			s, r := dialectArtifactOwnerSetup(t)
			injectedArtifactAttach(s, p, registry)
			raw := []byte("trusted exact scope")
			out := injectedArtifactPublish(t, s, r, raw)
			switch mutation {
			case "removed-registration":
				s.retainedArtifactProviders, _ = newRetainedArtifactProviderRegistry()
			case "different-instance":
				other, err := openPersistentInjectedArtifactProvider(t.Context(), injectedFixtureProviderID, root)
				assistantRunMust(t, err)
				defer other.Close()
				s.retainedArtifacts = other
			case "copied-instance":
				s.retainedArtifacts = &persistentInjectedArtifactProvider{self: p, id: p.id, root: p.root, info: p.info, identity: bytes.Clone(p.identity)}
			case "wrapper":
				s.retainedArtifacts = struct{ retainedArtifactRepository }{p}
			case "missing-host":
				ownerPath, _ := dialectArtifactOwnerRowPaths(s, out)
				assistantRunMust(t, os.Remove(ownerPath))
			case "missing-binding":
				assistantRunMust(t, os.Remove(filepath.Join(providerPath, "d5-native", out.Namespace, out.Owner+".json")))
			case "header-corrupt":
				assistantRunMust(t, os.WriteFile(filepath.Join(providerPath, "d5-provider.json"), []byte(`{"version":1}`), 0600))
			case "header-missing":
				assistantRunMust(t, os.Remove(filepath.Join(providerPath, "d5-provider.json")))
			}
			if got, actual, err := injectedArtifactInspect(s, r, artifactReference(raw)); err == nil || got != nil || actual != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("untrusted selection resolved", mutation, actual, err)
			}
			if mutation == "missing-binding" {
				if _, err := os.Stat(filepath.Join(providerPath, "d5-native", out.Namespace, out.Owner+".json")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("missing binding reconstructed", err)
				}
			}
		})
	}
}

func TestDialectArtifactInjectedActualQueue(t *testing.T) {
	t.Run("drain-revocation-before-host", func(t *testing.T) {
		for _, fact := range []string{"session", "cancel"} {
			t.Run(fact, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					// Keep provider operations and fixture cleanup in the same bubble.
					p, registry, root, _ := injectedArtifactProviderFixture(t)
					s, r := dialectArtifactOwnerSetup(t)
					injectedArtifactAttach(s, p, registry)
					queue := injectedArtifactQueue(t, root)
					defer queue.Close()
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					result := make(chan error, 1)
					go func() {
						out, err := s.enrollDialectArtifactOwner(r.WithContext(ctx))
						if out != (dialectArtifactOwnerEnrollment{}) {
							t.Error("revoked enrollment acknowledged")
						}
						result <- err
					}()
					synctest.Wait()
					work, err := s.openPrivateDataFileLock(t.Context(), ".data-work.lock", true, false)
					assistantRunMust(t, err)
					assistantRunMust(t, work.Close())
					private, err := s.tryPrivateDataLockContext(t.Context(), true)
					assistantRunMust(t, err)
					assistantRunMust(t, private.Close())
					if !s.writes.TryLock() {
						t.Fatal("writes held during provider drain")
					}
					s.writes.Unlock()
					if fact == "session" {
						s.sessions.close()
					} else {
						cancel()
					}
					assistantRunMust(t, queue.Close())
					if err := <-result; err == nil {
						t.Fatal("queue gap lost current authority")
					}
					dialectArtifactOwnerNoEffects(t, s)
					if _, err := s.root.Lstat(".desk-private/" + dialectArtifactOwnerMarker); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("revoked drain published marker", err)
					}
				})
			})
		}
	})
	t.Run("one-final-try", func(t *testing.T) {
		p, registry, root, _ := injectedArtifactProviderFixture(t)
		s, r := dialectArtifactOwnerSetup(t)
		injectedArtifactAttach(s, p, registry)
		tries := 0
		var competitor *os.File
		testHookDialectArtifactOwnerStep = func(step string) error {
			if step == "injected-before-enrollment-queue" {
				tries++
				competitor = injectedArtifactQueue(t, root)
			}
			return nil
		}
		defer func() {
			testHookDialectArtifactOwnerStep = nil
			if competitor != nil {
				_ = competitor.Close()
			}
		}()
		out, err := s.enrollDialectArtifactOwner(r)
		if !errors.Is(err, errRetainedArtifactBusy) || out != (dialectArtifactOwnerEnrollment{}) || tries != 1 {
			t.Fatal("final queue contention waited/retried", out, err, tries)
		}
		if _, err := s.root.Lstat(".desk-private/" + dialectArtifactOwnerMarker); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("busy try published marker", err)
		}
		dialectArtifactOwnerNoEffects(t, s)
	})
	t.Run("actual-live-view-excludes-and-has-no-reentry", func(t *testing.T) {
		p, registry, root, _ := injectedArtifactProviderFixture(t)
		s := dialectArtifactExecutionServer(t, Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir()})
		injectedArtifactAttach(s, p, registry)
		e := dialectArtifactExecutionStart(t, s, false)
		work, executor := dialectArtifactExecutionFDs(t, e)
		raw := []byte("bounded current native transfer")
		var escaped *retainedArtifactInjectedView
		_, err := e.withDialectArtifactOwner(t.Context(), &dialectArtifactInjectedUse{selection: injectedArtifactSelection(t, raw, true), use: func(v *retainedArtifactInjectedView) error {
			escaped = v
			probe, err := root.Open(".")
			if err != nil {
				return err
			}
			lockErr := lockPrivateData(probe, true)
			closeErr := probe.Close()
			if !lockHeld(lockErr) || closeErr != nil {
				return errors.New("view does not hold actual provider queue")
			}
			// Both original host mutexes are held. Any sessionOf/Authorize
			// reentry here would deadlock this real positive publication.
			if s.sessions.mu.TryLock() {
				s.sessions.mu.Unlock()
				return errors.New("actual session custody missing")
			}
			if s.signIn.mu.TryLock() {
				s.signIn.mu.Unlock()
				return errors.New("actual policy custody missing")
			}
			if second, err := v.registration.beginD5ViewLocked(t.Context(), v.binding, v.selection, v.guard); !errors.Is(err, errRetainedArtifactBusy) || second != nil {
				return errors.New("locked transfer waited or acquired twice")
			}
			if err := v.PublishExact(t.Context(), artifactReference(raw), raw); err != nil {
				return err
			}
			got, err := v.GetExact(t.Context(), artifactReference(raw))
			if err != nil || !bytes.Equal(got, raw) {
				return errors.Join(errDialectArtifactOwner, err)
			}
			if got, err := v.GetExact(t.Context(), artifactReference([]byte("unselected"))); err == nil || got != nil {
				return errors.New("unselected read reached storage")
			}
			return nil
		}})
		assistantRunMust(t, err)
		dialectArtifactExecutionLive(t, e, work, executor)
		if got, err := escaped.GetExact(t.Context(), artifactReference(raw)); err == nil || got != nil {
			t.Fatal("view escaped original host lifetime")
		}
		probe := injectedArtifactQueue(t, root)
		assistantRunMust(t, probe.Close())
	})
}

func TestDialectArtifactInjectedDurabilityAndClose(t *testing.T) {
	for _, reused := range []bool{false, true} {
		for _, step := range []string{"injected-identity.sync", "injected-identity.file-close", "injected-identity.directory-sync", "injected-identity.readback", "injected-provider-owner.sync", "injected-provider-binding.sync", "injected-owner.sync", "marker.sync", "scope.sync", "injected-enrollment-queue.close", "injected-authority.close", "injected-selected-file.file-close", "data.close", "owner-lock.close", "work.close"} {
			t.Run(fmt.Sprintf("reused=%t/%s", reused, step), func(t *testing.T) {
				p, registry, _, _ := injectedArtifactProviderFixture(t)
				s, r := dialectArtifactOwnerSetup(t)
				injectedArtifactAttach(s, p, registry)
				if reused {
					_, err := s.enrollDialectArtifactOwner(r)
					assistantRunMust(t, err)
				}
				failure, reached := errors.New("injected exact durable fault"), false
				testHookDialectArtifactOwnerStep = func(actual string) error {
					if actual == step {
						reached = true
						return failure
					}
					return nil
				}
				defer func() { testHookDialectArtifactOwnerStep = nil }()
				out, err := s.enrollDialectArtifactOwner(r)
				if !reached || !errors.Is(err, failure) || out != (dialectArtifactOwnerEnrollment{}) {
					t.Fatal("durability/Close uncertainty acknowledged", reached, out, err)
				}
			})
		}
	}
	t.Run("object-reuse-and-view-close-zero-inspection", func(t *testing.T) {
		p, registry, _, _ := injectedArtifactProviderFixture(t)
		s, r := dialectArtifactOwnerSetup(t)
		injectedArtifactAttach(s, p, registry)
		raw := []byte("immutable scoped artifact")
		injectedArtifactPublish(t, s, r, raw)
		for _, step := range []string{"injected-object.sync", "injected-object.file-close", "injected-object.directory-sync", "injected-view-queue.close"} {
			failure, reached := errors.New("uncertain scoped artifact"), false
			testHookDialectArtifactOwnerStep = func(actual string) error {
				if actual == step {
					reached = true
					return failure
				}
				return nil
			}
			out, err := s.withDialectArtifactOwner(r, &dialectArtifactInjectedUse{selection: injectedArtifactSelection(t, raw, true), use: func(v *retainedArtifactInjectedView) error {
				return v.PublishExact(r.Context(), artifactReference(raw), raw)
			}})
			testHookDialectArtifactOwnerStep = nil
			if !reached || !errors.Is(err, failure) || out != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("reused exact object skipped durable acknowledgment", step, reached, out, err)
			}
		}
		for _, step := range []string{"injected-object-read.file-close", "injected-object-folder.close"} {
			failure, reached := errors.New("exact inspection Close uncertain"), false
			testHookDialectArtifactOwnerStep = func(actual string) error {
				if actual == step {
					reached = true
					return failure
				}
				return nil
			}
			got, out, err := injectedArtifactInspect(s, r, artifactReference(raw))
			testHookDialectArtifactOwnerStep = nil
			if !reached || !errors.Is(err, failure) || got != nil || out != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("exact inspection file/root Close escaped", step, reached, out, err)
			}
		}
		failure := errors.New("view Close uncertain")
		testHookDialectArtifactOwnerStep = func(actual string) error {
			if actual == "injected-view-queue.close" {
				return failure
			}
			return nil
		}
		defer func() { testHookDialectArtifactOwnerStep = nil }()
		if got, out, err := injectedArtifactInspect(s, r, artifactReference(raw)); !errors.Is(err, failure) || got != nil || out != (dialectArtifactOwnerEnrollment{}) {
			t.Fatal("inspection escaped later Close error", string(got), out, err)
		}
	})
	t.Run("provider-close-wins", func(t *testing.T) {
		p, registry, _, _ := injectedArtifactProviderFixture(t)
		s, r := dialectArtifactOwnerSetup(t)
		injectedArtifactAttach(s, p, registry)
		revoked := make(chan struct{})
		closed := make(chan error, 1)
		testHookDialectArtifactOwnerStep = func(step string) error {
			if step == "injected-before-enrollment-queue" {
				go func() { closed <- p.Close() }()
				<-revoked
			}
			if step == "injected-provider-revoked" {
				close(revoked)
			}
			return nil
		}
		defer func() { testHookDialectArtifactOwnerStep = nil }()
		out, err := s.enrollDialectArtifactOwner(r)
		if err == nil || out != (dialectArtifactOwnerEnrollment{}) {
			t.Fatal("revoked in-flight provider acknowledged", out, err)
		}
		assistantRunMust(t, <-closed)
	})
}

func TestDialectArtifactInjectedUnsupportedAndInactive(t *testing.T) {
	for _, mode := range []string{"memory", "authority-only", "guarded-wrapper"} {
		t.Run(mode, func(t *testing.T) {
			s, r := dialectArtifactOwnerSetup(t)
			memory := newMemoryArtifacts()
			switch mode {
			case "memory":
				s.retainedArtifacts = memory
			case "authority-only":
				s.retainedArtifacts = &injectedArtifactLegacyAuthority{memory}
			case "guarded-wrapper":
				s.retainedArtifacts = &artifactAuthorityTestAdapter{retainedArtifactRepository: memory, owner: fileRetainedArtifactRepository{open: s.openRetainedArtifacts}}
			}
			out, err := s.enrollDialectArtifactOwner(r)
			if err == nil || out != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("unsupported injected owner enrolled", out, err)
			}
			if _, err := s.root.Lstat(".desk-private"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unsupported route touched project metadata", err)
			}
			if _, err := s.assistant.secrets.Lstat(dialectArtifactOwnersFolder); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unsupported route touched host metadata", err)
			}
			raw := []byte("legacy non-D5 behavior")
			ref, err := s.artifactRepository().Put(t.Context(), raw)
			assistantRunMust(t, err)
			got, err := s.artifactRepository().Get(t.Context(), ref)
			assistantRunMust(t, err)
			if !bytes.Equal(got, raw) {
				t.Fatal("legacy generic behavior changed")
			}
			dialectArtifactOwnerNoEffects(t, s)
		})
	}
}

type injectedArtifactLegacyAuthority struct{ retainedArtifactRepository }

func (a *injectedArtifactLegacyAuthority) WithAuthority(authorize func(context.Context) error) retainedArtifactRepository {
	return a
}

// Avoid attributing any archive authority to arbitrary provider/key strings.
func TestDialectArtifactInjectedTrustedResolutionMalformedConstruction(t *testing.T) {
	for _, id := range []string{"../provider", "https://example.invalid/provider", strings.Repeat("A", 64), ""} {
		root, err := os.OpenRoot(t.TempDir())
		assistantRunMust(t, err)
		p, constructErr := newPersistentInjectedArtifactProvider(t.Context(), id, root)
		assistantRunMust(t, root.Close())
		if constructErr == nil || p != nil {
			t.Fatal("untrusted constructor ID accepted", id)
		}
	}
}

func TestDialectArtifactInjectedDurabilityAndCloseServerOwnership(t *testing.T) {
	p, registry, root, _ := injectedArtifactProviderFixture(t)
	cfg := Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir()}
	info, err := os.Stat(cfg.ProjectDir)
	assistantRunMust(t, err)
	construction, err := newRetainedArtifactServerConstruction(registry, map[string]string{identityKey(info): p.id})
	assistantRunMust(t, err)
	cfg.retainedArtifactConstruction = construction
	s := dialectArtifactExecutionServer(t, cfg)
	e := dialectArtifactExecutionStart(t, s, false)
	failure := errors.New("owned provider root Close uncertain")
	revocations := 0
	testHookDialectArtifactOwnerStep = func(step string) error {
		if step == "injected-provider-revoked" {
			revocations++
			select {
			case <-e.done:
			default:
				t.Error("provider closed before original run drained")
			}
		}
		if step == "injected-provider-root.close" {
			return failure
		}
		return nil
	}
	defer func() { testHookDialectArtifactOwnerStep = nil }()
	if err := s.Close(); !errors.Is(err, failure) {
		t.Fatal("Server.Close lost provider error", err)
	}
	if err := s.Close(); !errors.Is(err, failure) || revocations != 1 {
		t.Fatal("provider Close repeated or error lost", err, revocations)
	}
	if _, err := root.Stat("."); err != nil {
		t.Fatal("provider closed borrowed construction root", err)
	}
}

func TestDialectArtifactInjectedDurabilityAndCloseArtifactCorruption(t *testing.T) {
	p, registry, _, providerPath := injectedArtifactProviderFixture(t)
	s, r := dialectArtifactOwnerSetup(t)
	injectedArtifactAttach(s, p, registry)
	raw := []byte("exact immutable artifact")
	out := injectedArtifactPublish(t, s, r, raw)
	ref := artifactReference(raw)
	path := filepath.Join(providerPath, "d5-objects", out.Owner, out.Namespace, ref.SHA256)
	corrupt := []byte("same name corrupt bytes")
	assistantRunMust(t, os.WriteFile(path, corrupt, 0600))
	if got, out, err := injectedArtifactInspect(s, r, ref); err == nil || got != nil || out != (dialectArtifactOwnerEnrollment{}) {
		t.Fatal("corrupt object inspected", out, err)
	}
	got, err := s.withDialectArtifactOwner(r, &dialectArtifactInjectedUse{selection: injectedArtifactSelection(t, raw, true), use: func(v *retainedArtifactInjectedView) error { return v.PublishExact(r.Context(), ref, raw) }})
	if err == nil || got != (dialectArtifactOwnerEnrollment{}) {
		t.Fatal("corrupt immutable object repaired", got, err)
	}
	if actual := dialectArtifactOwnerRead(t, path); !bytes.Equal(actual, corrupt) {
		t.Fatal("failed publication overwrote corrupt bytes")
	}
}

func TestDialectArtifactInjectedDurabilityAndCloseBranchContinuity(t *testing.T) {
	p, registry, _, providerPath := injectedArtifactProviderFixture(t)
	s, r := dialectArtifactOwnerSetup(t)
	injectedArtifactAttach(s, p, registry)
	out, err := s.enrollDialectArtifactOwner(r)
	assistantRunMust(t, err)
	branch := filepath.Join(providerPath, "d5-native", out.Namespace)
	row := []byte(fmt.Sprintf(`{"version":1,"provider":"%s","owner":"%s","projectFolderIdentity":"%s","workDomain":"%s","namespace":"%s"}`, injectedFixtureProviderID, out.Owner, out.ProjectIdentity, out.WorkDomain, out.Namespace))
	replaced := false
	testHookDialectArtifactOwnerStep = func(step string) error {
		if step == "injected-owner.readback" && !replaced {
			replaced = true
			if err := os.Rename(branch, branch+"-old"); err != nil {
				return err
			}
			if err := os.Mkdir(branch, 0700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(branch, out.Owner+".json"), row, 0600)
		}
		return nil
	}
	defer func() { testHookDialectArtifactOwnerStep = nil }()
	got, err := s.enrollDialectArtifactOwner(r)
	if !replaced || err == nil || got != (dialectArtifactOwnerEnrollment{}) {
		t.Fatal("byte-equal replaced branch acknowledged", replaced, got, err)
	}
}

func TestDialectArtifactInjectedTrustedResolutionUnrelatedRows(t *testing.T) {
	p, registry, _, providerPath := injectedArtifactProviderFixture(t)
	s, r := dialectArtifactOwnerSetup(t)
	injectedArtifactAttach(s, p, registry)
	raw := []byte("selected finite scope")
	out := injectedArtifactPublish(t, s, r, raw)
	// An unrelated fixed fixture is deliberately larger than every row cap.
	// Selected lookup never enumerates or imposes an aggregate lifetime quota.
	assistantRunMust(t, os.MkdirAll(filepath.Join(providerPath, "d5-native", strings.Repeat("f", 64)), 0700))
	assistantRunMust(t, os.WriteFile(filepath.Join(providerPath, "d5-native", strings.Repeat("f", 64), strings.Repeat("e", 64)+".json"), bytes.Repeat([]byte("x"), 16384), 0600))
	got, again, err := injectedArtifactInspect(s, r, artifactReference(raw))
	assistantRunMust(t, err)
	if !bytes.Equal(got, raw) || again != out {
		t.Fatal("unrelated row affected selected lookup", again, out)
	}
}

func TestDialectArtifactInjectedUnsupportedAndInactiveBoundGuard(t *testing.T) {
	p, registry, _, _ := injectedArtifactProviderFixture(t)
	s, r := dialectArtifactOwnerSetup(t)
	injectedArtifactAttach(s, p, registry)
	raw := []byte("generic guard cannot publish into D5")
	calls, closes := 0, 0
	acquire := func(context.Context) (retainedArtifactGuard, error) {
		calls++
		return &artifactAuthorityTestGuard{close: func() error { closes++; return nil }}, nil
	}
	var bound *boundInjectedArtifactRepository
	_, err := s.withDialectArtifactOwner(r, &dialectArtifactInjectedUse{selection: injectedArtifactSelection(t, raw, true), use: func(v *retainedArtifactInjectedView) error {
		var err error
		bound, err = v.boundRepository(t.Context(), acquire)
		return err
	}})
	assistantRunMust(t, err)
	if got, err := bound.PutGuarded(t.Context(), raw, acquire); !errors.Is(err, errRetainedArtifactAuthorityUnavailable) || got != (retainedArtifactRef{}) || calls != 1 || closes != 1 {
		t.Fatal("generic guard qualified for D5", got, err, calls, closes)
	}
	if got, out, err := injectedArtifactInspect(s, r, artifactReference(raw)); !errors.Is(err, os.ErrNotExist) || got != nil || out != (dialectArtifactOwnerEnrollment{}) {
		t.Fatal("unsupported bound guard published", out, err)
	}
}

func TestDialectArtifactInjectedPersistentReopenActualHome(t *testing.T) {
	parent, child := dialectArtifactExecutionHome(t)
	p, registry, _, providerPath := injectedArtifactProviderFixture(t)
	injectedArtifactAttach(parent, p, registry)
	injectedArtifactAttach(child, p, registry)
	raw := []byte("same bytes in two real home scopes")
	var prior dialectArtifactOwnerEnrollment
	for _, s := range []*Server{parent, child} {
		e := dialectArtifactExecutionStart(t, s, true)
		work, executor := dialectArtifactExecutionFDs(t, e)
		out, err := e.withDialectArtifactOwner(t.Context(), &dialectArtifactInjectedUse{selection: injectedArtifactSelection(t, raw, true), use: func(v *retainedArtifactInjectedView) error {
			return v.PublishExact(t.Context(), artifactReference(raw), raw)
		}})
		assistantRunMust(t, err)
		injectedArtifactExpectedRows(t, s, providerPath, out)
		if out.HomeAnchor == "" || out.HomeSHA256 == "" || prior.Owner != "" && (out.Owner == prior.Owner || out.Namespace == prior.Namespace || out.HomeAnchor != prior.HomeAnchor || out.WorkDomain != prior.WorkDomain) {
			t.Fatal("home scope lost actual custody", prior, out)
		}
		dialectArtifactExecutionLive(t, e, work, executor)
		dialectArtifactExecutionDrain(t, e)
		got, again, err := injectedArtifactInspect(s, e.request.WithContext(t.Context()), artifactReference(raw))
		assistantRunMust(t, err)
		if !bytes.Equal(got, raw) || again != out {
			t.Fatal("home native-scope resolution changed", again, out)
		}
		prior = out
	}
}

// Negative pin mechanics use real current identity but cannot manufacture a
// successful guard. Positive authority is exercised only by the lexical views.
func TestDialectArtifactInjectedActualQueueAuthorityContention(t *testing.T) {
	for _, kind := range []string{"session", "policy"} {
		t.Run(kind, func(t *testing.T) {
			p, registry, _, _ := injectedArtifactProviderFixture(t)
			s := dialectArtifactExecutionServer(t, Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir()})
			injectedArtifactAttach(s, p, registry)
			e := dialectArtifactExecutionStart(t, s, false)
			identity, _, err := s.assistantRunIdentity(e.request.WithContext(t.Context()))
			assistantRunMust(t, err)
			op, err := p.pin()
			assistantRunMust(t, err)
			defer op.finish()
			if kind == "session" {
				s.sessions.mu.Lock()
			} else {
				s.signIn.mu.Lock()
			}
			g, pinErr := holdDialectArtifactInjectedAuthority(s, e.request.WithContext(t.Context()), identity, registry.byID[p.id], op, func() error { return errDialectArtifactOwner })
			if kind == "session" {
				s.sessions.mu.Unlock()
			} else {
				s.signIn.mu.Unlock()
			}
			if g != nil || !errors.Is(pinErr, errRetainedArtifactBusy) {
				t.Fatal("contested authority pin acknowledged", kind, g, pinErr)
			}
			if !s.sessions.mu.TryLock() {
				t.Fatal("failed second pin retained session custody")
			}
			s.sessions.mu.Unlock()
		})
	}
}

func TestDialectArtifactInjectedActualQueueLiveAdmissionGap(t *testing.T) {
	for _, fact := range []string{"session", "policy", "generation", "stop", "cancel-call", "cancel-execution"} {
		t.Run(fact, func(t *testing.T) {
			p, registry, _, providerPath := injectedArtifactProviderFixture(t)
			s := dialectArtifactExecutionServer(t, Config{ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir()})
			injectedArtifactAttach(s, p, registry)
			e := dialectArtifactExecutionStart(t, s, false)
			work, executor := dialectArtifactExecutionFDs(t, e)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			seen := false
			testHookDialectArtifactOwnerStep = func(step string) error {
				if step != "injected-after-drain" || seen {
					return nil
				}
				seen = true
				// These are negative mutations of the original registered host,
				// under its actual locks. They supply no positive authority.
				switch fact {
				case "session":
					s.sessions.revoke(bearerOf(e.request))
				case "policy":
					s.signIn.mu.Lock()
					s.signIn.generation++
					s.signIn.cancelEpoch()
					s.signIn.mu.Unlock()
				case "generation":
					e.canonicalMu.Lock()
					e.generation = 0
					e.canonicalMu.Unlock()
				case "stop":
					e.canonicalMu.Lock()
					e.stopRequested = true
					e.canonicalMu.Unlock()
				case "cancel-call":
					cancel()
				case "cancel-execution":
					e.cancel()
				}
				return nil
			}
			defer func() { testHookDialectArtifactOwnerStep = nil }()
			out, err := e.enrollDialectArtifactOwner(ctx)
			testHookDialectArtifactOwnerStep = nil
			if !seen || err == nil || out != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("drain gap lost actual live authority", fact, seen, out, err)
			}
			if fact == "generation" || fact == "stop" || fact == "cancel-call" {
				e.canonicalMu.Lock()
				e.generation = 1
				e.stopRequested = false
				e.canonicalMu.Unlock()
				dialectArtifactExecutionLive(t, e, work, executor)
			}
			if _, err := s.root.Lstat(".desk-private/" + dialectArtifactOwnerMarker); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("revoked live gap published marker", err)
			}
			if _, err := os.Stat(filepath.Join(providerPath, "d5-owners")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("revoked live gap published provider owner", err)
			}
		})
	}
}
