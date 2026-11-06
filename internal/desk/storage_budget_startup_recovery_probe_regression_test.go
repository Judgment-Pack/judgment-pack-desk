package desk

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Actual New must inspect retained-run presence without turning blank or
// chat-only legacy settings into a first-use history enrollment. The original
// startup services run normally; the fixture seeds no enrollment or run ledger.
func TestStorageStartupRecoveryProbeDoesNotDemandHistory(t *testing.T) {
	for _, name := range []string{"blank", "chat-only-legacy", "contained-blank"} {
		t.Run(name, func(t *testing.T) {
			var cfg Config
			if name == "contained-blank" {
				cfg = storageRecoveryProbeContainedConfig(t)
			} else {
				cfg = ownerActualConfig(t)
			}
			legacy := name == "chat-only-legacy"
			legacyName := "conversations-" + strings.Repeat("a", 64) + ".json"
			const retained = `{"version":1,"chats":[]}`
			if legacy {
				storageTestMust(t, os.Mkdir(cfg.DeskConfigDir, 0700))
				privateFixture(t, filepath.Join(cfg.DeskConfigDir, legacyName), retained)
			}
			s := ownerActualNew(t, cfg)
			c := s.storageConstruction
			if !c.owner.admissionOpen() || s.assistantRunRecoveryFailure() != nil {
				t.Fatal("settings-only startup probe failed ordinary admission or run recovery")
			}
			c.ref.cell.mu.Lock()
			endpoint := c.ref.cell.enrollmentEndpoint
			c.ref.cell.mu.Unlock()
			if endpoint != nil {
				t.Fatal("startup probe completed an unsolicited enrollment")
			}
			for _, root := range c.sources.roots {
				if root.role == 7 || root.role == 8 {
					t.Fatal("startup probe captured an unsolicited enrollment parent")
				}
			}
			before, err := c.ref.cell.q.InspectEnrollment(t.Context())
			storageTestMust(t, err)
			c.owner.mu.Lock()
			beforeEpoch := c.owner.censusEpoch
			c.owner.mu.Unlock()
			if before.View.Configuration.Maintenance || beforeEpoch != 1 || before.View.Configuration.Epoch != beforeEpoch {
				t.Fatal("startup probe changed the original startup census")
			}
			beforeRoots := len(c.sources.roots)
			s.recoverAssistantRuns()
			after, err := c.ref.cell.q.InspectEnrollment(t.Context())
			storageTestMust(t, err)
			c.owner.mu.Lock()
			afterEpoch := c.owner.censusEpoch
			c.owner.mu.Unlock()
			c.ref.cell.mu.Lock()
			afterEndpoint := c.ref.cell.enrollmentEndpoint
			c.ref.cell.mu.Unlock()
			if !reflect.DeepEqual(before, after) || afterEpoch != beforeEpoch || len(c.sources.roots) != beforeRoots || afterEndpoint != nil || !c.owner.admissionOpen() || s.assistantRunRecoveryFailure() != nil {
				t.Fatal("repeated settings probe changed Q, census, sources, or recovery admission")
			}
			for _, absent := range []string{dataLocationName, "storage-enrollment.a", "storage-enrollment.b", "storage-enrollment-step.a", "storage-enrollment-step.b"} {
				if _, err := os.Lstat(filepath.Join(s.configDir, absent)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("startup probe materialized history enrollment", absent, err)
				}
			}
			if _, err := os.Lstat(s.defaultChatDataPath()); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("startup probe materialized default history", err)
			}
			if name == "contained-blank" {
				if !s.containedDeskData() {
					t.Fatal("fixture did not select actual contained settings")
				}
				if _, err := s.root.Lstat(startupIdentityDir); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("startup probe materialized contained settings", err)
				}
			}
			if legacy {
				raw, err := os.ReadFile(filepath.Join(s.configDir, legacyName))
				storageTestMust(t, err)
				if !bytes.Equal(raw, []byte(retained)) {
					t.Fatal("settings probe changed retained legacy chat bytes")
				}
			}
			storageTestMust(t, c.current(t.Context(), false))
		})
	}
}

// Same actual registered-home configuration as the existing home fixtures,
// with no private settings or history created before New.
func storageRecoveryProbeContainedConfig(t *testing.T) Config {
	t.Helper()
	cfg := ownerActualConfig(t)
	base := filepath.Dir(cfg.DeskConfigDir)
	cfg.DeskConfigDir = filepath.Join(base, deskHomePlatform)
	cfg.ProjectDir = filepath.Join(base, "project")
	storageTestMust(t, os.Mkdir(cfg.DeskConfigDir, 0700))
	storageTestMust(t, os.Mkdir(cfg.ProjectDir, 0700))
	id, owner := strings.Repeat("a", 32), strings.Repeat("b", 64)
	registry := `{"version":1,"owner":"` + owner + `","startup":"` + id + `","desks":[{"id":"` + id + `","name":"Project","folder":"project","kind":"project","historyIdentity":"/original/project","storageOwner":"` + owner + `","runnerWorkspace":"` + strings.Repeat("c", 64) + `"}]}`
	privateFixture(t, filepath.Join(cfg.DeskConfigDir, deskHomeRegistryName), registry)
	privateFixture(t, filepath.Join(cfg.ProjectDir, projectConfigName), `{"deskConfigVersion":1}`)
	return cfg
}

func TestStorageStartupRecoveryProbeRefusesDanglingPrivateEdge(t *testing.T) {
	s := ownerActualNew(t, storageRecoveryProbeContainedConfig(t))
	if !s.containedDeskData() || s.assistantRunRecoveryFailure() != nil || !s.storageConstruction.owner.admissionOpen() {
		t.Fatal("actual blank contained startup did not remain available")
	}
	before, err := s.storageConstruction.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	const target = "missing-private-settings"
	storageTestMust(t, os.Symlink(target, filepath.Join(s.projectDir, startupIdentityDir)))
	s.recoverAssistantRuns()
	if !errors.Is(s.assistantRunRecoveryFailure(), os.ErrNotExist) {
		t.Fatal("failed settings open through an existing edge was treated as absence")
	}
	link, err := os.Readlink(filepath.Join(s.projectDir, startupIdentityDir))
	storageTestMust(t, err)
	if link != target {
		t.Fatal("refused recovery changed the unsafe private edge")
	}
	after, err := s.storageConstruction.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(before, after) || !s.storageConstruction.owner.admissionOpen() {
		t.Fatal("refused settings probe changed Q or enrollment admission")
	}
}
