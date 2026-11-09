package desk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const injectedFixtureProviderID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func injectedArtifactProviderFixture(t *testing.T) (*persistentInjectedArtifactProvider, *retainedArtifactProviderRegistry, *os.Root, string) {
	t.Helper()
	path := t.TempDir()
	assistantRunMust(t, os.Chmod(path, 0700))
	root, err := os.OpenRoot(path)
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = root.Close() })
	p, err := newPersistentInjectedArtifactProvider(t.Context(), injectedFixtureProviderID, root)
	assistantRunMust(t, err)
	registry, err := newRetainedArtifactProviderRegistry(p)
	assistantRunMust(t, err)
	t.Cleanup(func() { _ = registry.Close() })
	return p, registry, root, path
}

// injectedArtifactAttach selects p on a Desk assembled without New, which
// has no storage construction to bind a provider. A Desk made by New binds
// its provider at construction (injectedArtifactConstruction), as the
// project-move construction does, and its provider is never swapped while it
// runs: the storage construction refuses a swapped one.
func injectedArtifactAttach(t *testing.T, s *Server, p *persistentInjectedArtifactProvider, registry *retainedArtifactProviderRegistry) {
	t.Helper()
	if s.deskOwner().storageConstruction != nil {
		t.Fatal("a Desk made by New binds its provider at construction")
	}
	s.deskOwner().retainedArtifactProviders = registry
	s.retainedArtifacts = p
}

// injectedArtifactConstruction is the trusted pre-New selection of p for the
// projects at paths. New adopts the registry, pins p for the Desk's life and
// closes the registry when the Desk closes.
func injectedArtifactConstruction(t *testing.T, p *persistentInjectedArtifactProvider, registry *retainedArtifactProviderRegistry, paths ...string) *retainedArtifactServerConstruction {
	t.Helper()
	projects := make(map[string]string, len(paths))
	for _, path := range paths {
		assistantRunMust(t, os.MkdirAll(path, 0700))
		info, err := os.Stat(path)
		assistantRunMust(t, err)
		projects[identityKey(info)] = p.id
	}
	c, err := newRetainedArtifactServerConstruction(registry, projects)
	assistantRunMust(t, err)
	return c
}

// injectedArtifactConfigured binds p at New for the configured project.
func injectedArtifactConfigured(t *testing.T, p *persistentInjectedArtifactProvider, registry *retainedArtifactProviderRegistry) func(*Config) {
	return func(cfg *Config) {
		cfg.retainedArtifactConstruction = injectedArtifactConstruction(t, p, registry, cfg.ProjectDir)
	}
}
func injectedArtifactQueue(t *testing.T, root *os.Root) *os.File {
	t.Helper()
	file, err := root.Open(".")
	assistantRunMust(t, err)
	if err = lockPrivateData(file, true); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	return file
}

func TestDialectArtifactInjectedClosedFormats(t *testing.T) {
	id := strings.Repeat("a", 64)
	maxIdentity := "18446744073709551615:18446744073709551615"
	// All expected shapes are handwritten. No fixture is serialized from a
	// producer or captured from enrollment output.
	identity := []byte(fmt.Sprintf(`{"version":1,"provider":"%s","storageFolderIdentity":"%s"}`, id, maxIdentity))
	owner := []byte(fmt.Sprintf(`{"version":2,"owner":"%s","kind":"injected","provider":"%s","projectFolderIdentity":"%s","workDomain":"%s"}`, id, id, maxIdentity, id))
	binding := []byte(fmt.Sprintf(`{"version":1,"provider":"%s","owner":"%s","projectFolderIdentity":"%s","workDomain":"%s","namespace":"%s"}`, id, id, maxIdentity, id, id))
	for _, fixture := range []struct {
		name string
		raw  []byte
		size int
		read func([]byte) error
	}{
		{"identity159", identity, 159, func(b []byte) error { _, err := readRetainedArtifactInjectedIdentity(b); return err }},
		{"owner332", owner, 332, func(b []byte) error { _, err := readDialectArtifactInjectedOwnerRecord(b); return err }},
		{"binding393", binding, 393, func(b []byte) error { _, err := readRetainedArtifactInjectedBinding(b); return err }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if len(fixture.raw) != fixture.size {
				t.Fatal("handwritten maximum changed", len(fixture.raw))
			}
			assistantRunMust(t, fixture.read(fixture.raw))
			for name, raw := range map[string][]byte{
				"over":      append(bytes.Clone(fixture.raw), ' '),
				"newline":   append(bytes.Clone(fixture.raw), '\n'),
				"missing":   bytes.Replace(fixture.raw, []byte(`"provider":"`+id+`",`), nil, 1),
				"null":      bytes.Replace(fixture.raw, []byte(`"provider":"`+id+`"`), []byte(`"provider":null`), 1),
				"duplicate": bytes.Replace(fixture.raw, []byte(`"provider":`), []byte(`"provider":"`+id+`","provider":`), 1),
				"case":      bytes.Replace(fixture.raw, []byte(`"provider":`), []byte(`"Provider":`), 1),
				"escaped":   bytes.Replace(fixture.raw, []byte(`"provider":`), []byte(`"provi\u0064er":`), 1),
				"unknown":   bytes.Replace(fixture.raw, []byte(`"version":`), []byte(`"unknown":0,"version":`), 1),
				"upper-id":  bytes.Replace(fixture.raw, []byte(id), []byte(strings.Repeat("A", 64)), 1),
				"short-id":  bytes.Replace(fixture.raw, []byte(id), []byte(strings.Repeat("a", 63)), 1),
				"url":       bytes.Replace(fixture.raw, []byte(id), []byte("https://example.invalid/provider"), 1),
				"fraction":  bytes.Replace(fixture.raw, []byte(`"version":1`), []byte(`"version":1.0`), 1),
			} {
				// owner uses version2; keep the intended malformed integer there.
				if name == "fraction" && bytes.Equal(raw, fixture.raw) {
					raw = bytes.Replace(fixture.raw, []byte(`"version":2`), []byte(`"version":2.0`), 1)
				}
				t.Run(name, func(t *testing.T) {
					if fixture.read(raw) == nil {
						t.Fatal("accepted noncanonical row", string(raw))
					}
				})
			}
			for _, identity := range []string{"01:2", "-0:1", "+1:2", "18446744073709551616:1", "1", "1:2:3"} {
				if fixture.read(bytes.Replace(fixture.raw, []byte(maxIdentity), []byte(identity), 1)) == nil {
					t.Fatal("accepted identity", identity)
				}
			}
		})
	}
	if _, err := readDialectArtifactOwnerRecord(owner); err == nil {
		t.Fatal("injected row decoded as native v1")
	}
	if _, err := readDialectArtifactLocationRecord(owner); err == nil {
		t.Fatal("injected row decoded as native location")
	}
}

func TestDialectArtifactInjectedTrustedResolutionConstructor(t *testing.T) {
	p, registry, root, path := injectedArtifactProviderFixture(t)
	info, err := root.Stat(".")
	assistantRunMust(t, err)
	expected := []byte(fmt.Sprintf(`{"version":1,"provider":"%s","storageFolderIdentity":"%s"}`, injectedFixtureProviderID, identityKey(info)))
	if got := dialectArtifactOwnerRead(t, filepath.Join(path, "d5-provider.json")); !bytes.Equal(got, expected) {
		t.Fatal("wrong durable provider header", string(got))
	}
	if q, err := newPersistentInjectedArtifactProvider(t.Context(), injectedFixtureProviderID, root); err == nil || q != nil {
		t.Fatal("CREATE reused an initialized root")
	}
	if _, err := newRetainedArtifactProviderRegistry(p, p); err == nil {
		t.Fatal("duplicate provider registered")
	}
	copied := &persistentInjectedArtifactProvider{self: p, id: p.id, root: p.root, info: p.info, identity: bytes.Clone(p.identity)}
	if _, err := newRetainedArtifactProviderRegistry(copied); err == nil {
		t.Fatal("copied adapter registered")
	}
	assistantRunMust(t, registry.Close())
	if _, err := newRetainedArtifactProviderRegistry(p); err == nil {
		t.Fatal("closed pointer registered")
	}
	q, err := openPersistentInjectedArtifactProvider(t.Context(), injectedFixtureProviderID, root)
	assistantRunMust(t, err)
	assistantRunMust(t, q.Close())
	if q, err := openPersistentInjectedArtifactProvider(t.Context(), strings.Repeat("b", 64), root); err == nil || q != nil {
		t.Fatal("wrong constructor ID adopted root")
	}
	copyPath := t.TempDir()
	assistantRunMust(t, os.Chmod(copyPath, 0700))
	assistantRunMust(t, os.WriteFile(filepath.Join(copyPath, "d5-provider.json"), expected, 0600))
	copyRoot, err := os.OpenRoot(copyPath)
	assistantRunMust(t, err)
	defer copyRoot.Close()
	if q, err := openPersistentInjectedArtifactProvider(t.Context(), injectedFixtureProviderID, copyRoot); err == nil || q != nil {
		t.Fatal("different-inode copy adopted")
	}
	assistantRunMust(t, os.Remove(filepath.Join(path, "d5-provider.json")))
	if q, err := openPersistentInjectedArtifactProvider(t.Context(), injectedFixtureProviderID, root); err == nil || q != nil {
		t.Fatal("OPEN repaired missing header")
	}
	assistantRunMust(t, os.WriteFile(filepath.Join(path, "d5-provider.json"), []byte(`{"version":1}`), 0600))
	if q, err := openPersistentInjectedArtifactProvider(t.Context(), injectedFixtureProviderID, root); err == nil || q != nil {
		t.Fatal("OPEN repaired corrupt header")
	}
}

func TestDialectArtifactInjectedUnsupportedAndInactiveGeneric(t *testing.T) {
	p, _, _, path := injectedArtifactProviderFixture(t)
	raw := []byte("ordinary compatibility payload")
	ref, err := p.Put(t.Context(), raw)
	assistantRunMust(t, err)
	got, err := p.Get(t.Context(), ref)
	assistantRunMust(t, err)
	if !bytes.Equal(got, raw) {
		t.Fatal("generic bytes changed")
	}
	if _, ok := any(p).(guardedRetainedArtifactRepository); ok {
		t.Fatal("unbound provider exposes guarded D5 publication")
	}
	if _, err := os.Stat(filepath.Join(path, "d5-objects")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("generic storage entered D5", err)
	}
	assistantRunMust(t, p.Close())
	if got, err := p.Get(context.Background(), ref); err == nil || got != nil {
		t.Fatal("closed generic read acknowledged")
	}
	if got, err := p.Put(context.Background(), raw); err == nil || got != (retainedArtifactRef{}) {
		t.Fatal("closed generic write acknowledged")
	}
}

func TestDialectArtifactInjectedClosedFormatsSelection(t *testing.T) {
	ref := artifactReference([]byte("selected exact input"))
	if _, err := newRetainedArtifactInjectedSelection([]retainedArtifactRef{ref, {SHA256: ref.SHA256, Bytes: ref.Bytes + 1}}, false); err == nil {
		t.Fatal("conflicting reference accepted")
	}
	refs := make([]retainedArtifactRef, 4097)
	if _, err := newRetainedArtifactInjectedSelection(refs, false); err == nil {
		t.Fatal("oversized request accepted")
	}
	var bounded []retainedArtifactRef
	for i := 0; i < 5; i++ {
		bounded = append(bounded, retainedArtifactRef{SHA256: fmt.Sprintf("%064x", i+1), Bytes: 32 << 20})
	}
	if _, err := newRetainedArtifactInjectedSelection(bounded, true); err == nil {
		t.Fatal("payload request cap bypassed")
	}
	selection, err := newRetainedArtifactInjectedSelection([]retainedArtifactRef{ref}, false)
	assistantRunMust(t, err)
	input := []retainedArtifactRef{ref}
	copied, err := newRetainedArtifactInjectedSelection(input, true)
	assistantRunMust(t, err)
	input[0].Bytes++
	if !selection.contains(ref) || !copied.contains(ref) || copied.contains(input[0]) {
		t.Fatal("selection retained mutable input")
	}
}
