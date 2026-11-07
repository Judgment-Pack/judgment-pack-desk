package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStorageChildAllConfiguredNativeProfilesWithoutInitialization(t *testing.T) {
	s := storageChildTestServer(t)
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	s.configDir = dir
	s.assistant = &assistantStore{dir: dir, root: root}
	model := "shared-model"
	registry := aiRegistry{Version: 1, Connections: []aiConnection{}}
	for i := 0; i < 32; i++ {
		id := fmt.Sprintf("ai-%024x", i+1)
		registry.Connections = append(registry.Connections, aiConnection{ID: id, Name: fmt.Sprintf("Profile %d", i+1), Enabled: true, Assistant: AssistantSlotView{Engine: "codex", Thinking: "off", Agent: &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Model: &model, Models: []string{model}, Tools: []string{}}}})
		profile := platformPath(dir, "codex-"+id)
		if err := os.Mkdir(profile, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(profile, "credential-metadata"), []byte("private"), 0000); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, aiConnectionsFile), raw, 0600); err != nil {
		t.Fatal(err)
	}
	before := []os.FileInfo{}
	for i := 0; i < 32; i++ {
		info, err := os.Lstat(filepath.Join(platformPath(dir, fmt.Sprintf("codex-ai-%024x", i+1)), "credential-metadata"))
		if err != nil {
			t.Fatal(err)
		}
		before = append(before, info)
	}
	rows := s.storageChildNative(t.Context(), storageChildTestWalk(t.Context()))
	if len(rows) != 33 || rows[0].State != "inactive" {
		t.Fatal("32 configured profiles omitted", len(rows))
	}
	for _, row := range rows[1:] {
		if row.State != "measured" || row.LogicalBytes == nil || *row.LogicalBytes != "7" {
			t.Fatal("metadata-only configured profile", row)
		}
	}
	if len(s.aiAccounts.values) != 0 {
		t.Fatal("observation initialized provider manager")
	}
	for i, info := range before {
		after, err := os.Lstat(filepath.Join(platformPath(dir, fmt.Sprintf("codex-ai-%024x", i+1)), "credential-metadata"))
		if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || info.Mode() != after.Mode() || !info.ModTime().Equal(after.ModTime()) {
			t.Fatal("native observation modified metadata", err)
		}
	}
}

// A read-only existing-owner fixture; other account operations are unreachable.
// It issues no native qualification and does not initialize a provider.
type storageChildHeldProfileFixture struct {
	providerAccountManager
	root *os.Root
}

func (p *storageChildHeldProfileFixture) ObserveStorage(_ context.Context, visit func(*os.Root) error) error {
	return visit(p.root)
}

func TestStorageChildNativeCallbackChecksCompleteAncestorChain(t *testing.T) {
	for _, during := range []bool{false, true} {
		label := "before-callback"
		if during {
			label = "during-callback"
		}
		t.Run(label, func(t *testing.T) {
			s := storageChildTestServer(t)
			base := storageChildTestDir(t)
			ancestor := filepath.Join(base, "ancestor")
			dir := filepath.Join(ancestor, "config", "profile")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "metadata-only"), []byte("child"), 0000); err != nil {
				t.Fatal(err)
			}
			held, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			s.codex = &storageChildHeldProfileFixture{root: held}
			cut := func() {
				if err := os.Rename(ancestor, ancestor+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(ancestor+"-original", ancestor); err != nil {
					t.Fatal(err)
				}
			}
			w := storageChildTestWalk(t.Context())
			entered := false
			if during {
				w.cut = func(phase string, _ *os.Root, _ string) {
					if phase == "file-counted" && !entered {
						entered = true
						cut()
					}
				}
			} else {
				cut()
			}
			rows := s.storageChildNative(t.Context(), w)
			if len(rows) != 1 || rows[0].State != "unavailable" || rows[0].LogicalBytes != nil || rows[0].AvailableBytes != nil {
				t.Fatal("native visit bypassed held-path proof", rows)
			}
			if during && !entered {
				t.Fatal("in-walk ancestor cut did not execute")
			}
		})
	}
}
