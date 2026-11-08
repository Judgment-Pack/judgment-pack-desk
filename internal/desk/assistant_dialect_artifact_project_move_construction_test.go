package desk

import (
	"os"
	"testing"
)

func TestDialectArtifactConstructionRetainsStartupOperation(t *testing.T) {
	requireStorageLine(t)
	cfg := ownerActualConfig(t)
	p, registry, _, _ := injectedArtifactProviderFixture(t)
	info, err := os.Stat(cfg.ProjectDir)
	if err != nil {
		t.Fatal(err)
	}
	construction, err := newRetainedArtifactServerConstruction(registry, map[string]string{identityKey(info): p.id})
	if err != nil {
		t.Fatal(err)
	}
	cfg.retainedArtifactConstruction = construction
	s := ownerActualNew(t, cfg)
	pins := s.storageStartupProviders
	if pins == nil || len(pins.ordered) != 1 || pins.byID[p.id] == nil || pins.byID[p.id].operation.finished.Load() {
		t.Fatal("pre-home startup operation was not retained")
	}
	if err = pins.check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s.storageConstruction.providers != registry {
		t.Fatal("original source construction changed provider")
	}
	child := &Server{cfg: Config{parent: s, retainedArtifactConstruction: construction}, root: s.root}
	if err = child.bindRetainedArtifactConstruction(); err != nil {
		t.Fatal(err)
	}
	if child.storageStartupProviders != nil || child.retainedArtifacts != p {
		t.Fatal("child took ownership of parent startup operation")
	}
	if err = pins.check(t.Context()); err != nil {
		t.Fatal("child binding released parent startup operation", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if !pins.closed || !pins.byID[p.id].operation.finished.Load() || !p.revoked.Load() {
		t.Fatal("Close did not drain source views, release startup operation, then close provider")
	}
}

func TestDialectArtifactConstructionFailureReleasesOwnedPin(t *testing.T) {
	// A failed New after binding must release its retained pin before waiting on
	// registry/provider Close. No provider-backed host growth is needed here.
	cfg := ownerActualConfig(t)
	p, registry, _, _ := injectedArtifactProviderFixture(t)
	info, err := os.Stat(cfg.ProjectDir)
	if err != nil {
		t.Fatal(err)
	}
	construction, err := newRetainedArtifactServerConstruction(registry, map[string]string{identityKey(info): p.id})
	if err != nil {
		t.Fatal(err)
	}
	cfg.retainedArtifactConstruction = construction
	if err = os.Mkdir(cfg.DeskConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	privateFixture(t, homeJournalPath(cfg.DeskConfigDir), "{ malformed pending home transaction")
	s, err := New(cfg)
	if err == nil {
		if s != nil {
			_ = s.Close()
		}
		t.Fatal("invalid launch unexpectedly succeeded")
	}
	if !p.revoked.Load() {
		t.Fatal("failed actual New did not release owned registry")
	}
}
