package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDialectArtifactProjectMoveInjectedConstructorReplay(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "restart after rename"}[interrupted], func(t *testing.T) {
			provider, registry, root, providerPath := injectedArtifactProviderFixture(t)
			project := t.TempDir()
			info, err := os.Stat(project)
			if err != nil {
				t.Fatal(err)
			}
			construction, err := newRetainedArtifactServerConstruction(registry, map[string]string{identityKey(info): provider.id})
			if err != nil {
				t.Fatal(err)
			}
			cfg := Config{ProjectDir: project, DeskConfigDir: t.TempDir(), Token: testToken, Port: testPort,
				JpackBin: standInRuntime(t), CodexBin: "off", retainedArtifactConstruction: construction}
			s, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			if s.retainedArtifacts != provider || s.retainedArtifactProviders != registry {
				t.Fatal("constructor did not bind original configured instance")
			}
			stampMoveIdentity(t, s)
			owner := enrollDialectArtifactMoveServer(t, s)
			hostOwner, _ := dialectArtifactOwnerRowPaths(s, owner)
			providerOwner := filepath.Join(providerPath, "d5-owners", owner.Owner[:2], owner.Owner[2:]+".json")
			providerBinding := filepath.Join(providerPath, "d5-native", owner.Namespace, owner.Owner+".json")
			hostOriginal := dialectArtifactOwnerRead(t, hostOwner)
			providerOriginal, bindingOriginal := dialectArtifactOwnerRead(t, providerOwner), dialectArtifactOwnerRead(t, providerBinding)
			destination := filepath.Join(t.TempDir(), "next")
			s.writes.Lock()
			preview, err := s.moveSnapshot(destination)
			s.writes.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if interrupted {
				testProjectMoveStep = func(at string) error {
					if at == "after-rename" {
						return errors.New("interrupted")
					}
					return nil
				}
			}
			t.Cleanup(func() { testProjectMoveStep = nil })
			err = s.confirmProjectMove(context.Background(), projectMoveRequest{Destination: destination, Token: preview.Token})
			testProjectMoveStep = nil
			if interrupted && err == nil || !interrupted && err != nil {
				t.Fatal("move outcome", err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			// OPEN the same durable identity/root before the next New. Neither the
			// pending journal nor a saved provider ID supplies a root or credential.
			opened, err := openPersistentInjectedArtifactProvider(t.Context(), injectedFixtureProviderID, root)
			if err != nil {
				t.Fatal(err)
			}
			nextRegistry, err := newRetainedArtifactProviderRegistry(opened)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = nextRegistry.Close() })
			nextConstruction, err := newRetainedArtifactServerConstruction(nextRegistry, map[string]string{identityKey(info): opened.id})
			if err != nil {
				t.Fatal(err)
			}
			cfg.ProjectDir, cfg.retainedArtifactConstruction = destination, nextConstruction
			again, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = again.Close() })
			if !bytes.Equal(dialectArtifactOwnerRead(t, hostOwner), hostOriginal) ||
				!bytes.Equal(dialectArtifactOwnerRead(t, providerOwner), providerOriginal) ||
				!bytes.Equal(dialectArtifactOwnerRead(t, providerBinding), bindingOriginal) {
				t.Fatal("same-inode move rewrote original provider provenance")
			}
			nextOwner := enrollDialectArtifactMoveServer(t, again)
			if nextOwner.Owner != owner.Owner || nextOwner.Namespace != owner.Namespace {
				t.Fatal("owner/empty native namespace changed across move", nextOwner)
			}
			if _, err = os.Lstat(filepath.Join(again.configDir, projectMoveName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("replay did not finish intent", err)
			}
		})
	}
}

func TestDialectArtifactProjectMoveConstructionCannotBeCopied(t *testing.T) {
	provider, registry, _, _ := injectedArtifactProviderFixture(t)
	project := t.TempDir()
	info, err := os.Stat(project)
	if err != nil {
		t.Fatal(err)
	}
	construction, err := newRetainedArtifactServerConstruction(registry, map[string]string{identityKey(info): provider.id})
	if err != nil {
		t.Fatal(err)
	}
	copy := *construction
	cfg := Config{ProjectDir: project, DeskConfigDir: t.TempDir(), Token: testToken, Port: testPort,
		JpackBin: standInRuntime(t), CodexBin: "off", retainedArtifactConstruction: &copy}
	if server, err := New(cfg); err == nil || server != nil {
		if server != nil {
			server.Close()
		}
		t.Fatal("copied constructor seal accepted")
	}
	if err = registry.byID[provider.id].current(); err != nil {
		t.Fatal("refused copy closed caller's original registry", err)
	}
}
