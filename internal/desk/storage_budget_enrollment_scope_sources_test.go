package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageEnrollmentRestrictedProjectCensusAbsenceAndDelegation(t *testing.T) {
	for _, delegated := range []bool{false, true} {
		name := "positive-absence"
		if delegated {
			name = "original-child-delegation"
		}
		t.Run(name, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			cfg.ProjectDir = filepath.Join(t.TempDir(), "original-public-project")
			storageTestMust(t, os.Mkdir(cfg.ProjectDir, 0755))
			// Unrelated non-private project content must never be enumerated,
			// counted, chmodded or treated as private by the restricted source.
			unrelated := filepath.Join(cfg.ProjectDir, "unrelated-user-file")
			storageTestMust(t, os.WriteFile(unrelated, []byte("retained user content"), 0644))
			storageTestMust(t, os.Chmod(unrelated, 0644))
			if delegated {
				storageTestMust(t, os.Mkdir(filepath.Join(cfg.ProjectDir, ".desk-private"), 0700))
				privateFixture(t, filepath.Join(cfg.ProjectDir, ".desk-private", "retained-control"), "same actual child")
				// Ordinary preexisting-child delegation retains a preexisting legitimate
				// identity. Newly issued identity lineage is its separate required gate.
				info, err := os.Stat(cfg.ProjectDir)
				storageTestMust(t, err)
				privateFixture(t, filepath.Join(cfg.ProjectDir, startupIdentityDir, startupIdentityName), string(identityRecord{ID: strings.Repeat("9", 64), Path: cfg.ProjectDir, Folder: identityKey(info)}.line()))
			}
			s := ownerActualNew(t, cfg)
			c, o := s.storageConstruction, s.storageConstruction.owner
			// This read-only candidate uses original native/source facts only. It
			// exercises real binding construction and r.current without publishing a
			// catalog, Q registration or a creation/qualification receipt.
			catalog, id := storageRestrictedProjectBindingFixture(t, c)
			storageRestrictedProjectBindingInputs(t, c, catalog, id)
			image, err := c.constructOwnerBindingImage(t.Context(), catalog, c.sources)
			storageTestMust(t, err)
			defer func() { storageTestMust(t, image.Close()) }()
			r := image.roots[id]
			if r == nil || r.restrictedNamespace != ".desk-private" {
				t.Fatal("restricted binding not constructed")
			}
			storageTestMust(t, r.current(t.Context()))
			walk := storageCensusWalk{owner: o, ctx: t.Context(), roots: map[storageDirectoryIdentity]*storageRootBinding{r.directory: r}}
			walk.observation = o.beginOwnedMeasurement([]*storageRootBinding{r}, image.workspaces, image.filesystems)

			if delegated {
				found := false
				for _, child := range o.orderedRoots() {
					if child.source != nil && child.source.path == filepath.Join(s.projectDir, ".desk-private") {
						if !reflect.DeepEqual(r.references, child.references) {
							t.Fatal("original child/workspace references differ from scoped parent")
						}
						walk.roots[child.directory] = child
						found = true
					}
				}
				if !found {
					t.Fatal("actual existing private child was not enrolled by startup")
				}
			}
			var usage [storagebudget.Categories]storagebudget.Vector
			count := 0
			storageTestMust(t, walk.projectPrivateScope(r, r.root, &count, &usage))
			if walk.observation == nil || walk.observation.value.Logical != (storagebudget.Vector{}) {
				t.Fatal("nonowning project anchor/delegated child became measured here")
			}
			if usage != ([storagebudget.Categories]storagebudget.Vector{}) || count != 0 {
				t.Fatal("restricted scope counted project content or double-counted delegated child")
			}
			before, err := os.Stat(unrelated)
			storageTestMust(t, err)
			if before.Mode().Perm() != 0644 {
				t.Fatal("restricted census changed unrelated project permissions")
			}
			if err = c.sources.current(t.Context()); err != nil {
				t.Fatal("read-only scope census changed original sources", err)
			}
		})
	}
}

// Check the actual original inputs before candidate construction, with the
// failing row and join named. In particular the unreferenced anchor must retain
// its canonical [] references in the live binding, not become nil on copying.
func storageRestrictedProjectBindingInputs(t *testing.T, c *storageBudgetConstruction, cat storageOwnerCatalogV2, id storagebudget.ID) {
	t.Helper()
	if err := c.originCurrent(t.Context(), false); err != nil {
		t.Fatal("original construction currentness", err)
	}
	if err := c.sources.current(t.Context()); err != nil {
		t.Fatal("original sources currentness", err)
	}
	if !reflect.DeepEqual(c.sources.owner, cat.OriginalOwner) {
		t.Fatal("original owner source differs from candidate")
	}
	anchors := 0
	for i, row := range cat.Roots {
		if row.Definition.ID == id {
			binding, err := storageEnrollmentPeakDecodeSource(row.Source.Relative)
			if err != nil {
				t.Fatal("restricted source codec", err)
			}
			if binding.Project != cat.Objects[row.DirectoryObject].Persistent {
				t.Fatal("restricted source/object native differs")
			}
			if !storageEnrollmentPeakSourceJoins(binding, cat, row) {
				t.Fatal("restricted source catalog/mapping join")
			}
			found := false
			for _, w := range c.sources.workspaces {
				if w.Definition.ID == binding.Workspace && w.Definition.Generation == binding.WorkspaceGeneration && bytes.Equal(w.OwnerKey, binding.OwnerKey) && bytes.Equal(w.HistoryIdentity, binding.HistoryIdentity) {
					found = true
				}
			}
			if !found {
				t.Fatal("restricted source original workspace join")
			}
			key, err := c.server.inspectStorageStartupKey()
			if err != nil || key != string(binding.OwnerKey) || c.server.conversationIdentity() != string(binding.HistoryIdentity) {
				t.Fatal("restricted source original project selection", err)
			}
			native, err := storageOwnerPersistentRoot(t.Context(), c.project.own.root)
			if err != nil || native != binding.Project {
				t.Fatal("restricted source original native", err)
			}
		}
		source, err := c.sources.exactRoot(t.Context(), cat, row)
		if err != nil {
			t.Fatalf("row%d role%d exact original source: %v", i, row.Role, err)
		}
		directory, err := source.directoryIdentity()
		if err != nil {
			t.Fatalf("row%d role%d original directory identity: %v", i, row.Role, err)
		}
		if err = source.current(t.Context(), c); err != nil {
			t.Fatalf("row%d role%d original source currentness: %v", i, row.Role, err)
		}
		if prior := c.owner.roots[row.Definition.ID]; prior != nil {
			if storageRegisteredRoot(prior.row) != row.Definition {
				t.Fatalf("row%d role%d existing definition differs", i, row.Role)
			}
			if prior.root != source.root || prior.directory != directory {
				t.Fatalf("row%d role%d existing held source/native differs", i, row.Role)
			}
			if prior.category != row.Category {
				t.Fatalf("row%d role%d existing category differs", i, row.Role)
			}
			if !reflect.DeepEqual(prior.references, row.References) {
				t.Fatalf("row%d role%d existing references differ: live nil=%t count=%d catalog nil=%t count=%d", i, row.Role, prior.references == nil, len(prior.references), row.References == nil, len(row.References))
			}
			if row.Role == 2 {
				anchors++
				if prior.references == nil || row.References == nil || len(prior.references) != 0 {
					t.Fatal("unreferenced original anchor lost canonical empty array")
				}
			}
		}
	}
	if anchors != 1 {
		t.Fatal("expected exactly one reused original anchor", anchors)
	}
}

// Required valid behavior, deliberately separate from the seeded census case.
// M1/M3/CC-2 lineage integration is still pending; no skip/refusal substitutes
// for normal automatic identity issuance and healthy immutable accounting.
func TestStorageEnrollmentPreexistingPrivateIdentityIssuanceRestartKeepsHealthyAuthority(t *testing.T) {
	cfg := ownerActualConfig(t)
	storageTestMust(t, os.Mkdir(filepath.Join(cfg.ProjectDir, startupIdentityDir), 0700))
	privateFixture(t, filepath.Join(cfg.ProjectDir, startupIdentityDir, "retained-control"), "original preexisting private bytes")
	s := ownerActualNew(t, cfg)
	c := s.storageConstruction
	record := *c.ref.cell.ownerRecord
	original := c.ref.cell.ownerCatalog.OriginalOwner
	workspace := s.storageEnrollment.workspace
	key, err := s.inspectStorageStartupKey()
	storageTestMust(t, err)
	if key == digestOf([]byte(s.projectDir)) {
		t.Fatal("normal preexisting-private startup did not issue its legitimate identity")
	}
	storageTestMust(t, s.Close())
	next := ownerActualNew(t, cfg)
	n := next.storageConstruction
	if *n.ref.cell.ownerRecord != record || !reflect.DeepEqual(n.ref.cell.ownerCatalog.OriginalOwner, original) || next.storageEnrollment.workspace != workspace {
		t.Fatal("automatic identity issuance changed immutable original custody/accounting")
	}
	currentKey, err := next.inspectStorageStartupKey()
	storageTestMust(t, err)
	if currentKey != key {
		t.Fatal("restart changed legitimately issued project identity")
	}
	storageTestMust(t, n.current(t.Context(), false))
	storageTestMust(t, n.sources.current(t.Context()))
	view, err := n.ref.cell.q.Snapshot(t.Context())
	storageTestMust(t, err)
	if view.Maintenance {
		t.Fatal("restart did not retain healthy ordinary authority")
	}
	for _, root := range view.Roots {
		if !root.Known {
			t.Fatal("restart lost original known root accounting")
		}
	}
	for _, row := range view.Workspaces {
		if !row.Known {
			t.Fatal("restart lost original known workspace accounting")
		}
	}
}

// Assemble a comparison-only Role7 candidate from the actual original owner.
// This creates no metadata/effects and provides no native qualification.
func storageRestrictedProjectBindingFixture(t *testing.T, c *storageBudgetConstruction) (storageOwnerCatalogV2, storagebudget.ID) {
	t.Helper()
	cat := ownerCatalogClone(c.ref.cell.ownerCatalog)
	native, err := storageOwnerPersistentRoot(t.Context(), c.project.own.root)
	storageTestMust(t, err)
	key, err := c.server.inspectStorageStartupKey()
	storageTestMust(t, err)
	var workspace storageOwnerWorkspace
	for _, v := range cat.Workspaces {
		if string(v.OwnerKey) == key && string(v.HistoryIdentity) == c.server.conversationIdentity() {
			workspace = v
		}
	}
	if workspace.Definition.ID == (storagebudget.ID{}) {
		t.Fatal("actual original project workspace missing")
	}
	var row storageOwnerRoot
	found := false
	for _, v := range cat.Roots {
		for _, f := range cat.Filesystems {
			if v.Definition.Filesystem == f.Definition.ID && f.Backend == native.Backend && f.VolumeUUID == native.VolumeUUID {
				row = v
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("actual original project filesystem missing")
	}
	row.Definition.ID = storagebudget.ID{255}
	row.Definition.Workspace = workspace.Definition.ID
	row.Definition.Generation = 1
	row.Definition.Locator = "project-private-parent:fixture"
	row.Role = 7
	row.Category = storagebudget.Control
	row.References = []storagebudget.ID{workspace.Definition.ID}
	row.DomainObject = storageOwnerNoObject
	row.WorkObject = storageOwnerNoObject
	row.DirectoryObject = uint32(len(cat.Objects))
	row.Membership.ExistingObjects = []uint32{}
	row.Source = storageOwnerRootSource{Kind: 7, OwnerKey: append([]byte{}, workspace.OwnerKey...), Path: []byte{}, Relative: []byte{}, ControlIndex: storageOwnerNoControl}
	cat.Objects = append(cat.Objects, storageOwnerObject{Role: 20, Source: 3, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: native, OwnerClass: 2, ModeClass: 1})
	source := storageEnrollmentPeakSource{Installation: cat.Installation, QBinding: cat.QBinding, Project: native, Workspace: workspace.Definition.ID, WorkspaceGeneration: workspace.Definition.Generation, OwnerKey: append([]byte{}, workspace.OwnerKey...), HistoryIdentity: append([]byte{}, workspace.HistoryIdentity...), Filesystem: row.Definition.Filesystem, CapacityCount: row.Definition.CapacityCount, CapacityRefs: row.Definition.CapacityRefs, Namespace: []byte(".desk-private"), Generation: 1}
	var fs storagebudget.Filesystem
	for _, v := range cat.Filesystems {
		if v.Definition.ID == source.Filesystem {
			fs = v.Definition
			source.FilesystemIdentity = fs.Identity
			source.FilesystemGeneration = fs.Generation
		}
	}
	source.HomeSource, err = storageEnrollmentHomeSourceDigest(source, cat)
	storageTestMust(t, err)
	source.CapacityBinding, err = storageEnrollmentPeakSourceMapping(source, cat, row)
	storageTestMust(t, err)
	row.Source.Relative, err = storageEnrollmentPeakEncodeSource(source)
	storageTestMust(t, err)
	row.Definition.Identity = storageEnrollmentPeakHash("desk-project-private-parent-scope-e1", row.Source.Relative)
	constraints := []storagebudget.CapacityConstraint{}
	for _, v := range cat.Constraints {
		constraints = append(constraints, v.Definition)
	}
	row.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storageOwnerRootValue(row.Definition), fs, constraints)
	storageTestMust(t, err)
	cat.Roots = append(cat.Roots, row)
	return cat, row.Definition.ID
}

func TestStorageEnrollmentScopedDirectoryIdentityKeepsPrivatePermissions(t *testing.T) {
	// These roots are owned test fixtures. No user project is chmodded by the
	// implementation; the new scope must preserve its original public mode.
	path := filepath.Join(t.TempDir(), "public-directory")
	storageTestMust(t, os.Mkdir(path, 0755))
	root, err := os.OpenRoot(path)
	storageTestMust(t, err)
	defer func() { storageTestMust(t, root.Close()) }()
	if _, err = storageDirectoryID(root); err == nil {
		t.Fatal("fixture unexpectedly private")
	}
	source := &storageOriginalSourceRoot{root: root, role: 7, relative: ".desk-private", scope: &storageEnrollmentPeakSource{Namespace: []byte(".desk-private")}}
	if _, err = source.directoryIdentity(); err != nil {
		t.Fatal("original restricted project refused", err)
	}
	ancestor := storageEnrollmentPeakSource{Namespace: []byte("history-base"), Default: &storageEnrollmentDefaultSource{Workflow: 2, Branch: storageCreationPersonal, Settings: epNative(200, 1), Selected: epNative(201, 1), SelectedPath: []byte("/original"), Next: []byte("child")}}
	source.role = 8
	source.relative = "child"
	source.scope = &ancestor
	if _, err = source.directoryIdentity(); err != nil {
		t.Fatal("original unowned ancestry refused", err)
	}
	ancestor.Default.OwnSelf = 1
	ancestor.Default.CreationPage = epHash("owned", []byte{1})
	if _, err = source.directoryIdentity(); err == nil {
		t.Fatal("owned ancestry accepted nonprivate directory")
	}
	source.scope = nil
	source.role = 6
	if _, err = source.directoryIdentity(); err == nil {
		t.Fatal("ordinary private source accepted nonprivate directory")
	}
	source.scope = &storageEnrollmentPeakSource{Namespace: []byte("arbitrary")}
	source.role = 7
	source.relative = ".desk-private"
	if _, err = source.directoryIdentity(); err == nil {
		t.Fatal("Role7 alone bypassed private custody")
	}
	source.scope = &ancestor
	source.role = 8
	source.scope.Default.OwnSelf = 0
	source.relative = "another-child"
	if _, err = source.directoryIdentity(); err == nil {
		t.Fatal("Role8 alone bypassed original next component")
	}
}

// Complete bytes are handwritten independently of the implementation encoder.
func storageDefaultSourceVector(s storageEnrollmentPeakSource) []byte {
	w := epBytes{}
	w.Write(epSourceBytes(s))
	d := s.Default
	w.fixed(uint16(1))
	w.fixed(d.Workflow)
	w.fixed(d.Branch)
	w.fixed(d.Settings)
	w.fixed(d.Selected)
	w.fixed(d.Marker)
	w.fixed(d.Location)
	w.fixed(d.LocationPayload)
	w.fixed(d.CreationPage)
	w.data(d.SelectedPath)
	w.fixed(d.OwnSelf)
	w.data(d.Next)
	return w.Bytes()
}
func TestStorageEnrollmentDefaultSourceClosedCodecAndMaximum(t *testing.T) {
	_, _, base, _, _ := epCatalogs(t)
	for _, branch := range []byte{storageCreationConfigured, storageCreationContained, storageCreationPersonal} {
		s := base
		s.Namespace = []byte("data")
		if branch == storageCreationPersonal {
			s.Namespace = []byte("history")
		}
		s.Default = &storageEnrollmentDefaultSource{Workflow: 1, Branch: branch, Settings: epNative(200, 1), Selected: epNative(201, 1), Marker: epNative(202, 2), Location: epNative(203, 2), LocationPayload: epHash("location", []byte{1}), CreationPage: epHash("creation", []byte{2}), SelectedPath: []byte("/selected/" + string(s.Namespace)), Next: []byte{}}
		want := storageDefaultSourceVector(s)
		got, err := storageEnrollmentPeakEncodeSource(s)
		storageTestMust(t, err)
		if !bytes.Equal(want, got) {
			t.Fatal("default source fields or widths changed", branch)
		}
		decoded, err := storageEnrollmentPeakDecodeSource(want)
		storageTestMust(t, err)
		if !reflect.DeepEqual(decoded, s) {
			t.Fatal("lost source operand", branch)
		}
		if _, err = storageEnrollmentPeakDecodeSource(epSourceBytes(s)); err == nil {
			t.Fatal("accepted missing new-workflow operands", branch)
		}
		for _, mutate := range []func(*storageEnrollmentPeakSource){func(v *storageEnrollmentPeakSource) { v.Namespace = []byte("arbitrary") }, func(v *storageEnrollmentPeakSource) { v.Namespace = []byte(".desk-private") }, func(v *storageEnrollmentPeakSource) { v.Default.Workflow = 0 }, func(v *storageEnrollmentPeakSource) { v.Default.Branch = 0 }, func(v *storageEnrollmentPeakSource) { v.Default.Marker = storageOwnerPersistentObject{} }, func(v *storageEnrollmentPeakSource) { v.Default.LocationPayload = storagebudget.Digest{} }, func(v *storageEnrollmentPeakSource) { v.Default.CreationPage = storagebudget.Digest{} }, func(v *storageEnrollmentPeakSource) { v.Default.SelectedPath = []byte("relative") }, func(v *storageEnrollmentPeakSource) { v.Default.Next = []byte("child") }, func(v *storageEnrollmentPeakSource) { v.Default.OwnSelf = 1 }} {
			bad := s
			d := *s.Default
			bad.Default = &d
			mutate(&bad)
			if _, err := storageEnrollmentPeakEncodeSource(bad); err == nil {
				t.Fatal("accepted widened default source", branch)
			}
		}
	}
	max := base
	max.OwnerKey = bytes.Repeat([]byte{'o'}, 4096)
	max.HistoryIdentity = bytes.Repeat([]byte{'h'}, 4096)
	max.Namespace = []byte("history-base")
	max.Default = &storageEnrollmentDefaultSource{Workflow: 2, Branch: storageCreationPersonal, Settings: epNative(200, 1), Selected: epNative(201, 1), SelectedPath: []byte("/" + strings.Repeat("p", 4095)), Next: bytes.Repeat([]byte{'n'}, 255)}
	want := storageDefaultSourceVector(max)
	got, err := storageEnrollmentPeakEncodeSource(max)
	storageTestMust(t, err)
	// Widths follow the handwritten field order above. Workflow2 permits both
	// maximum owner strings, a4096-byte absolute path and a255-byte next name.
	baseMaximum := 107 + 2*4098 + 217 + 14 + 8
	trailerMaximum := 2 + 1 + 1 + 4*35 + 2*32 + 4098 + 1 + 257
	if baseMaximum != 8542 || trailerMaximum != 4564 || len(want) != baseMaximum+trailerMaximum || len(got) != 13106 || !bytes.Equal(got, want) || storageEnrollmentDefaultSourceMaximum != baseMaximum+trailerMaximum || storageEnrollmentDefaultSourceTrailerMaximum != trailerMaximum {
		t.Fatal("maximum source width", len(want), len(got))
	}
	for _, raw := range [][]byte{got[:len(got)-1], append(append([]byte{}, got...), 0)} {
		if _, err := storageEnrollmentPeakDecodeSource(raw); err == nil {
			t.Fatal("accepted incomplete or trailing source")
		}
	}
	// Both board decoders must scan exactly the same typed source, leaving only
	// the section's zero padding to their own strict reader.
	reader := storageEnrollmentPeakReader{raw: append(append([]byte{}, got...), make([]byte, 16)...)}
	decoded, err := storageEnrollmentPeakScanSource(&reader)
	storageTestMust(t, err)
	if reader.pos != 13106 || !reflect.DeepEqual(decoded, max) || !reader.zeroTail() {
		t.Fatal("source scanner consumed section padding or lost an operand")
	}
	owned := max
	d := *max.Default
	owned.Default = &d
	owned.Default.OwnSelf = 1
	owned.Default.CreationPage = epHash("ancestor-creation", []byte{3})
	if _, err = storageEnrollmentPeakEncodeSource(owned); err != nil {
		t.Fatal("closed owned-ancestor form", err)
	}
	owned.Default.CreationPage = storagebudget.Digest{}
	if _, err = storageEnrollmentPeakEncodeSource(owned); err == nil {
		t.Fatal("owned ancestor omitted creation evidence")
	}
	legacy, err := storageEnrollmentPeakEncodeSource(base)
	storageTestMust(t, err)
	if !bytes.Equal(legacy, epSourceBytes(base)) {
		t.Fatal("legacy source bytes changed")
	}
}

func TestStorageEnrollmentDefaultHistoryControlAdditionKeepsLegacyRefusal(t *testing.T) {
	// This focused declared-use vector carries no publication/custody receipt.
	// Only the explicit Peak-v2 reader may recognize the additional relation;
	// its complete board/action/original joins remain separately mandatory.
	_, c, _, _, _ := epCatalogs(t)
	// epCatalogs inherits the actual declared history pointer at Roots[3];
	// changing config Roots[0] leaves that real user and cannot test an orphan.
	index, users := -1, 0
	for i, row := range c.Roots {
		if row.Role == 4 && row.Source.ControlKind == 2 && row.Source.ControlIndex == 0 {
			index = i
			users++
		}
	}
	if users != 1 || len(c.OwnerControls) != 1 {
		t.Fatal("expected one original history pointer user")
	}
	r := c.Roots[index]
	r.Source.ControlKind = 0
	r.Source.ControlIndex = storageOwnerNoControl
	r.Source.ControlSelection = 0
	c.Roots[index] = r
	c.OwnerControls = append([]storageOwnerControl{}, c.OwnerControls...)
	c.OwnerControls[0].Location = storageOwnerLocation{Version: 1, Path: append([]byte{}, r.Source.Path...), Previous: []byte{}, Selected: c.Objects[r.DirectoryObject].Persistent}
	if c.validateUses() == nil {
		t.Fatal("legacy catalog reader admitted the new orphan-control relation")
	}
	storageTestMust(t, c.validateUsesForDefault(true))
	for _, mutate := range []func(*storageOwnerCatalogV2){func(v *storageOwnerCatalogV2) { v.OwnerControls[0].Location.Path = []byte("/elsewhere") }, func(v *storageOwnerCatalogV2) { v.OwnerControls[0].Location.Selected = epNative(888, 1) }, func(v *storageOwnerCatalogV2) { v.OwnerControls[0].OwnerKey = bytes.Repeat([]byte{'z'}, 64) }, func(v *storageOwnerCatalogV2) { v.OwnerControls[0].Location.Previous = []byte("/previous") }} {
		bad := c
		bad.OwnerControls = append([]storageOwnerControl{}, c.OwnerControls...)
		mutate(&bad)
		if bad.validateUsesForDefault(true) == nil {
			t.Fatal("accepted an unrelated default control")
		}
	}
}

func TestStorageEnrollmentLegacyDefaultSourceIsPointerOnly(t *testing.T) {
	_, _, s, _, _ := epCatalogs(t)
	s.Namespace = []byte("legacy")
	native := epNative(200, 1)
	s.Default = &storageEnrollmentDefaultSource{Workflow: 3, Branch: storageCreationConfigured, Settings: native, Selected: native, Location: epNative(201, 2), LocationPayload: epHash("legacy-location", []byte{1}), CreationPage: epHash("legacy-creation", []byte{2}), SelectedPath: []byte("/original/config"), Next: []byte{}}
	want := storageDefaultSourceVector(s)
	raw, err := storageEnrollmentPeakEncodeSource(s)
	storageTestMust(t, err)
	if !bytes.Equal(raw, want) {
		t.Fatal("legacy branch changed shared source fields")
	}
	decoded, err := storageEnrollmentPeakDecodeSource(want)
	storageTestMust(t, err)
	if !reflect.DeepEqual(decoded, s) {
		t.Fatal("lost legacy source selection")
	}
	for _, mutate := range []func(*storageEnrollmentDefaultSource){func(d *storageEnrollmentDefaultSource) { d.Branch = storageCreationContained }, func(d *storageEnrollmentDefaultSource) { d.Marker = epNative(202, 2) }, func(d *storageEnrollmentDefaultSource) { d.Selected = epNative(203, 1) }, func(d *storageEnrollmentDefaultSource) { d.OwnSelf = 1 }, func(d *storageEnrollmentDefaultSource) { d.Next = []byte("data") }} {
		bad := s
		d := *s.Default
		bad.Default = &d
		mutate(&d)
		if _, err := storageEnrollmentPeakEncodeSource(bad); err == nil {
			t.Fatal("legacy branch admitted directory, marker or contained selection")
		}
	}
}

func TestStorageEnrollmentDefaultControlDictionaryReindexKeepsOriginalRegistry(t *testing.T) {
	// Handwritten dictionary operands: an original home registry at index0,
	// followed by the one admitted default-location relation which sorts first.
	config := epNative(900, 1)
	home := epNative(901, 1)
	selected := epNative(902, 1)
	old := storageOwnerCatalogV2{OriginalOwner: storageOwnerSource{Kind: 2, ConfigPath: []byte("/original/config"), HomeOwner: strings.Repeat("a", 64), HomePath: []byte("/original/home"), HomeObject: home, RegistryControl: 0, Entries: []storageOwnerHomeRelation{}}, Objects: []storageOwnerObject{{Role: 1, Persistent: config, Parent: storageOwnerNoObject}}, OwnerControls: []storageOwnerControl{{Role: 1, ParentObject: 0, Name: []byte(deskHomeRegistryName), OwnerKind: 1, OwnerKey: []byte(strings.Repeat("a", 64)), Location: storageOwnerLocation{Path: []byte{}, Previous: []byte{}}, MaxBytes: 1048576}}, Roots: []storageOwnerRoot{}}
	var err error
	old.OwnerControls[0].RelationDigest, err = old.controlDigest(old.OwnerControls[0])
	storageTestMust(t, err)
	next := ownerCatalogClone(old)
	next.OwnerControls = append(next.OwnerControls, storageOwnerControl{Role: 2, ParentObject: 0, Name: []byte(dataLocationName), OwnerKind: 2, OwnerKey: []byte(strings.Repeat("a", 64)), Location: storageOwnerLocation{Version: 1, Path: []byte("/original/config/data"), Previous: []byte{}, Selected: selected}, MaxBytes: 16384})
	next.OwnerControls[1].RelationDigest, err = next.controlDigest(next.OwnerControls[1])
	storageTestMust(t, err)
	source := storageEnrollmentPeakSource{Default: &storageEnrollmentDefaultSource{Workflow: 1, Branch: storageCreationConfigured, Settings: config, Selected: selected, SelectedPath: []byte("/original/config/data")}}
	storageTestMust(t, storageEnrollmentSortControls(&next))
	if next.OriginalOwner.RegistryControl != 1 || string(next.OwnerControls[0].Name) != dataLocationName || string(next.OwnerControls[1].Name) != deskHomeRegistryName {
		t.Fatal("handwritten canonical control order changed")
	}
	if !reflect.DeepEqual(old.OwnerControls[0], next.OwnerControls[1]) {
		t.Fatal("reindex changed exact original registry bytes/material")
	}
	if storageEnrollmentOriginalOwnerEqual(old, next, nil) {
		t.Fatal("unadmitted dictionary reindex accepted")
	}
	if !storageEnrollmentOriginalOwnerEqual(old, next, &source) || !storageEnrollmentOriginalOwnerEqual(next, old, &source) {
		t.Fatal("closed default addition lost exact registry material")
	}
	for _, mutation := range []string{"registry-owner", "registry-relation", "registry-bound", "registry-native", "original-owner", "original-home", "wrong-index", "location-owner", "location-path", "location-native", "second-addition"} {
		t.Run(mutation, func(t *testing.T) {
			bad := ownerCatalogClone(next)
			switch mutation {
			case "registry-owner":
				bad.OwnerControls[1].OwnerKey = []byte(strings.Repeat("b", 64))
			case "registry-relation":
				bad.OwnerControls[1].RelationDigest[0] ^= 1
			case "registry-bound":
				bad.OwnerControls[1].MaxBytes++
			case "registry-native":
				bad.Objects[0].Persistent = epNative(903, 1)
			case "original-owner":
				bad.OriginalOwner.HomeOwner = strings.Repeat("b", 64)
			case "original-home":
				bad.OriginalOwner.HomeObject = epNative(904, 1)
			case "wrong-index":
				bad.OriginalOwner.RegistryControl = 0
			case "location-owner":
				bad.OwnerControls[0].OwnerKey = []byte(strings.Repeat("b", 64))
				bad.OwnerControls[0].RelationDigest, err = bad.controlDigest(bad.OwnerControls[0])
				storageTestMust(t, err)
			case "location-path":
				bad.OwnerControls[0].Location.Path = []byte("/another/data")
			case "location-native":
				bad.OwnerControls[0].Location.Selected = epNative(905, 1)
			case "second-addition":
				bad.OwnerControls = append(bad.OwnerControls, bad.OwnerControls[0])
			}
			if storageEnrollmentOriginalOwnerEqual(old, bad, &source) {
				t.Fatal("accepted changed original/default relation", mutation)
			}
		})
	}
}

// Handwritten host path operands exercise the finite walker independently of
// any enrollment/native proof. These are custody fixtures, not qualification.
func TestStorageEnrollmentPersonalAncestryWalkIsFiniteAndAnchored(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	storageTestMust(t, err)
	storageTestMust(t, os.WriteFile(filepath.Join(base, "unrelated"), []byte("ordinary host data"), 0644))
	selected := filepath.Join(base, "one", "two", "history")
	walk, err := storageWalkHistoryAncestry(selected)
	storageTestMust(t, err)
	if walk.path != base || walk.next != "one" || !walk.missing {
		t.Fatal("first positive absence", walk.path, walk.next, walk.missing)
	}
	storageTestMust(t, walk.Close())
	storageTestMust(t, os.Mkdir(filepath.Join(base, "one"), 0700))
	walk, err = storageWalkHistoryAncestry(selected)
	storageTestMust(t, err)
	if walk.path != filepath.Join(base, "one") || walk.next != "two" || !walk.missing {
		t.Fatal("second positive absence")
	}
	storageTestMust(t, walk.Close())
	storageTestMust(t, os.Mkdir(filepath.Join(base, "one", "two"), 0700))
	walk, err = storageWalkHistoryAncestry(selected)
	storageTestMust(t, err)
	if walk.path != filepath.Dir(selected) || walk.next != "history" || walk.missing {
		t.Fatal("final parent selection")
	}
	storageTestMust(t, walk.Close())
	// A path deeper than the component accounting cap remains a finite path.
	deep := base
	for i := 0; i < 18; i++ {
		deep = filepath.Join(deep, "d")
		storageTestMust(t, os.Mkdir(deep, 0700))
	}
	walk, err = storageWalkHistoryAncestry(filepath.Join(deep, "history"))
	storageTestMust(t, err)
	if walk.path != deep || walk.next != "history" || walk.missing {
		t.Fatal("path depth was confused with Q component count")
	}
	storageTestMust(t, walk.Close())
	info, err := os.Stat(filepath.Join(base, "unrelated"))
	storageTestMust(t, err)
	if info.Mode().Perm() != 0644 {
		t.Fatal("host content permissions changed")
	}
}
func TestStorageEnrollmentPersonalAncestryRejectsExchangedAndSymlinkEdges(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	storageTestMust(t, err)
	storageTestMust(t, os.Mkdir(filepath.Join(base, "a"), 0700))
	walk, err := storageWalkHistoryAncestry(filepath.Join(base, "a", "next", "history"))
	storageTestMust(t, err)
	storageTestMust(t, os.Rename(filepath.Join(base, "a"), filepath.Join(base, "retained")))
	storageTestMust(t, os.Mkdir(filepath.Join(base, "a"), 0700))
	if err = walk.current(); err == nil {
		t.Fatal("exchanged ancestor accepted")
	}
	if err = walk.Close(); err == nil {
		t.Fatal("closing lost exchange evidence")
	}
	storageTestMust(t, os.Symlink("retained", filepath.Join(base, "link")))
	if w, err := storageWalkHistoryAncestry(filepath.Join(base, "link", "next", "history")); err == nil {
		_ = w.Close()
		t.Fatal("intermediate symlink accepted")
	}
	for _, bad := range []string{"relative/history", filepath.Join(base, "other"), base + "/../history"} {
		if w, err := storageWalkHistoryAncestry(bad); err == nil {
			_ = w.Close()
			t.Fatal("noncanonical or non-history selection accepted", bad)
		}
	}
	for _, pair := range [][2]string{{base, base}, {base, filepath.Join(base, "..", "elsewhere", "history")}} {
		if _, err = storageHistoryNextComponent(pair[0], pair[1]); err == nil {
			t.Fatal("non-descendant default accepted")
		}
	}
}

// Actual API demand must refuse absent native creation qualification before
// entering its private transaction queue or changing any final namespace.
// This proves refusal ordering only; successful native demand stays required.
func TestStorageEnrollmentDefaultHistoryDemandPrecedesPrivateQueue(t *testing.T) {
	s := ownerActualNew(t, ownerActualConfig(t))
	c := s.storageConstruction
	if c.ref.cell.enrollmentEndpoint != nil {
		t.Fatal("fixture unexpectedly enrolled")
	}
	before, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
	storageTestMust(t, err)
	release, err := s.privateDataTurns.tryAcquire(t.Context())
	storageTestMust(t, err)
	defer release()
	lock, err := s.tryPrivateDataLockContext(t.Context(), true)
	if lock != nil {
		_ = lock.Close()
		t.Fatal("unqualified demand entered private transaction")
	}
	if !errors.Is(err, errStorageEnrollmentQualification) {
		t.Fatalf("native demand was not checked before queue: %v", err)
	}
	for _, name := range []string{dataLocationName, "data", "storage-enrollment.a", "storage-enrollment.b", "storage-enrollment-step.a", "storage-enrollment-step.b"} {
		if _, err = os.Lstat(filepath.Join(s.configDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unqualified demand changed %s: %v", name, err)
		}
	}
	after, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
	storageTestMust(t, err)
	if !bytes.Equal(before, after) {
		t.Fatal("unqualified demand changed original intent bytes")
	}
	if c.ref.cell.enrollmentEndpoint != nil {
		t.Fatal("unqualified demand installed endpoint")
	}
}

// Handwritten dictionary rows isolate reference translation. This comparison
// does not create an admitted packet or establish native custody.
func TestStorageEnrollmentHistoryAncestorObjectIndexMappingIsExact(t *testing.T) {
	config, lock, ancestor := epNative(201, 1), epNative(202, 2), epNative(200, 1)
	before := storageOwnerCatalogV2{OriginalOwner: storageOwnerSource{Kind: 1, ConfigPath: []byte("/original/config"), HomeOwner: strings.Repeat("a", 64), HomePath: []byte{}, RegistryControl: storageOwnerNoControl, Entries: []storageOwnerHomeRelation{}}, Objects: []storageOwnerObject{{Role: 1, Source: 1, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: config, OwnerClass: 2, ModeClass: 1}, {Role: 18, Source: 3, Parent: 0, Name: []byte(".data.lock"), Persistent: lock, OwnerClass: 2, ModeClass: 2}}, OwnerControls: []storageOwnerControl{{Role: 1, ParentObject: 0, Name: []byte(deskHomeRegistryName), OwnerKind: 1, OwnerKey: []byte(strings.Repeat("a", 64)), Location: storageOwnerLocation{Path: []byte{}, Previous: []byte{}}, MaxBytes: 1048576}}}
	var err error
	before.OwnerControls[0].RelationDigest, err = before.controlDigest(before.OwnerControls[0])
	storageTestMust(t, err)
	after := before
	after.Objects = []storageOwnerObject{{Role: 20, Source: 3, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: ancestor, OwnerClass: 2, ModeClass: 1}, before.Objects[0], before.Objects[1]}
	after.Objects[2].Parent = 1
	after.OwnerControls = append([]storageOwnerControl{}, before.OwnerControls...)
	after.OwnerControls[0].ParentObject = 1
	source := storageEnrollmentPeakSource{Namespace: []byte("history-base"), Default: &storageEnrollmentDefaultSource{Workflow: 2, Branch: storageCreationPersonal, Settings: config, Selected: ancestor, SelectedPath: []byte("/original"), Next: []byte("next")}}
	if !storageEnrollmentHistoryAncestorObjectMapping(before, after, source) {
		t.Fatal("complete handwritten object/control reindex refused")
	}
	clone := func(value storageOwnerCatalogV2) storageOwnerCatalogV2 {
		raw, err := json.Marshal(value)
		storageTestMust(t, err)
		var out storageOwnerCatalogV2
		storageTestMust(t, json.Unmarshal(raw, &out))
		return out
	}
	mutations := []func(*storageOwnerCatalogV2){
		func(c *storageOwnerCatalogV2) { c.Objects = c.Objects[:2] },
		func(c *storageOwnerCatalogV2) { c.Objects[1].Persistent = c.Objects[2].Persistent },
		func(c *storageOwnerCatalogV2) { c.Objects[2].Parent = 0 },
		func(c *storageOwnerCatalogV2) { c.Objects[2].Role = 17 },
		func(c *storageOwnerCatalogV2) { c.Objects[2].Source = 2 },
		func(c *storageOwnerCatalogV2) { c.Objects[2].Name = []byte("changed.lock") },
		func(c *storageOwnerCatalogV2) { c.Objects[2].OwnerClass = 1 },
		func(c *storageOwnerCatalogV2) { c.Objects[2].ModeClass = 1 },
		func(c *storageOwnerCatalogV2) { c.Objects[2].FixedLength = 1 },
		func(c *storageOwnerCatalogV2) { c.Objects[0].Parent = 1 },
		func(c *storageOwnerCatalogV2) { c.Objects[0].Persistent = epNative(199, 1) },
		func(c *storageOwnerCatalogV2) { c.OwnerControls = nil },
		func(c *storageOwnerCatalogV2) { c.OwnerControls = append(c.OwnerControls, c.OwnerControls[0]) },
		func(c *storageOwnerCatalogV2) { c.OwnerControls[0].ParentObject = 0 },
		func(c *storageOwnerCatalogV2) { c.OwnerControls[0].OwnerKey = []byte(strings.Repeat("b", 64)) },
		func(c *storageOwnerCatalogV2) { c.OwnerControls[0].MaxBytes++ },
		func(c *storageOwnerCatalogV2) { c.OwnerControls[0].RelationDigest[0] ^= 1 },
		func(c *storageOwnerCatalogV2) { c.OwnerControls[0].Location.Selected = ancestor },
	}
	for i, mutate := range mutations {
		bad := clone(after)
		mutate(&bad)
		if storageEnrollmentHistoryAncestorObjectMapping(before, bad, source) {
			t.Fatal("tampered, missing or ambiguous relation accepted", i)
		}
	}
	duplicate := clone(before)
	duplicate.Objects = append(duplicate.Objects, duplicate.Objects[0])
	more := clone(after)
	more.Objects = append(more.Objects, more.Objects[1])
	if storageEnrollmentHistoryAncestorObjectMapping(duplicate, more, source) {
		t.Fatal("duplicate prior native accepted")
	}
	wrong := source
	d := *source.Default
	wrong.Default = &d
	wrong.Default.Workflow = 1
	wrong.Namespace = []byte("history")
	if storageEnrollmentHistoryAncestorObjectMapping(before, after, wrong) {
		t.Fatal("one-control workflow used ancestor index comparison")
	}
	x := &storageOriginalSources{}
	if x.admittedHistoryAncestorObjectIndices(t.Context(), after) {
		t.Fatal("dictionary data became an admitted source packet")
	}
}

// The held transfer lock is deliberately outside enrollment's lock set. An
// unqualified first-use backup must reach native demand before trying it, and
// must not create an archive stage. This is ordering/refusal evidence only.
func TestStorageEnrollmentBackupDemandPrecedesTransferAndStage(t *testing.T) {
	s := ownerActualNew(t, ownerActualConfig(t))
	token := policyTestSession(t, s)
	transfer, err := s.privateDataFileLock(".data-transfer.lock", true)
	storageTestMust(t, err)
	defer transfer.Close()
	names := func() []string {
		entries, err := storageEntries(s.assistant.root)
		storageTestMust(t, err)
		out := []string{}
		for _, entry := range entries {
			out = append(out, entry.Name())
		}
		return out
	}
	before := names()
	hosts, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
	storageTestMust(t, err)
	unauth := policyCall(s, "", "GET", "/api/storage/backup", nil)
	if unauth.Code != 401 || strings.Contains(unauth.Body.String(), errStorageEnrollmentQualification.Error()) {
		t.Fatal("backup demand preceded authentication guard", unauth.Code)
	}
	response := policyCall(s, token, "GET", "/api/storage/backup", nil)
	if response.Code == 200 || !strings.Contains(response.Body.String(), errStorageEnrollmentQualification.Error()) {
		t.Fatal("backup took transfer/work lock before original native demand", response.Code)
	}
	if !reflect.DeepEqual(before, names()) {
		t.Fatal("unqualified backup created a stage or final namespace")
	}
	after, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
	storageTestMust(t, err)
	if !bytes.Equal(hosts, after) {
		t.Fatal("unqualified backup changed intent history")
	}
	if s.storageConstruction.ref.cell.enrollmentEndpoint != nil {
		t.Fatal("unqualified backup fabricated enrollment")
	}
}

// These handwritten operands test the read-side dictionary join only. Actual
// restart must separately establish original holds, current Q/all64 hosts and
// the required captured-native profile before restoring any metadata.
func TestStorageEnrollmentCreationRestartDictionaryRestoresExactOriginal(t *testing.T) {
	for _, kind := range []uint8{1, 2} {
		t.Run(string(rune('0'+kind)), func(t *testing.T) {
			config, lock, selected, marker := epNative(930, 1), epNative(931, 2), epNative(928, 1), epNative(929, 2)
			old := storageOwnerCatalogV2{OriginalOwner: storageOwnerSource{Kind: kind, ConfigPath: []byte("/original/config"), HomeOwner: strings.Repeat("a", 64), HomePath: []byte{}, RegistryControl: storageOwnerNoControl, Entries: []storageOwnerHomeRelation{}}, Objects: []storageOwnerObject{{Role: 1, Source: 1, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: config, OwnerClass: 1, ModeClass: 1}, {Role: 18, Source: 1, Parent: 0, Name: []byte(".data.lock"), Persistent: lock, OwnerClass: 1, ModeClass: 2}}, OwnerControls: []storageOwnerControl{}}
			// Dictionary restoration uses the same closed canonical JSON shape as
			// a complete catalog: empty collections are arrays, never null.
			old.BeforeDefinitionIDs = storageOwnerDefinitionIDs{Roots: []storagebudget.ID{}, Workspaces: []storagebudget.ID{}, Filesystems: []storagebudget.ID{}, Constraints: []storagebudget.ID{}}
			old.Roots = []storageOwnerRoot{}
			old.Workspaces = []storageOwnerWorkspace{}
			old.Filesystems = []storageOwnerFilesystem{}
			old.Constraints = []storageOwnerConstraint{}
			old.Profiles = []storageOwnerProfile{}
			if kind == 2 {
				old.OriginalOwner.HomePath = []byte("/original/home")
				old.OriginalOwner.HomeObject = epNative(932, 1)
				old.OriginalOwner.RegistryControl = 0
				old.OwnerControls = append(old.OwnerControls, storageOwnerControl{Role: 1, ParentObject: 0, Name: []byte(deskHomeRegistryName), OwnerKind: 1, OwnerKey: []byte(strings.Repeat("a", 64)), Location: storageOwnerLocation{Path: []byte{}, Previous: []byte{}}, MaxBytes: 1048576})
				var err error
				old.OwnerControls[0].RelationDigest, err = old.controlDigest(old.OwnerControls[0])
				storageTestMust(t, err)
			}
			next := ownerCatalogClone(old)
			next.Objects = []storageOwnerObject{{Role: 21, Source: 1, Parent: 2, Name: []byte("data"), Persistent: selected, OwnerClass: 1, ModeClass: 1}, {Role: 23, Source: 1, Parent: 0, Name: []byte(dataMarkerName), Persistent: marker, OwnerClass: 1, ModeClass: 2, FixedLength: 96}, old.Objects[0], old.Objects[1]}
			next.Objects[3].Parent = 2
			for i := range next.OwnerControls {
				next.OwnerControls[i].ParentObject = 2
			}
			next.OwnerControls = append(next.OwnerControls, storageOwnerControl{Role: 2, ParentObject: 2, Name: []byte(dataLocationName), OwnerKind: 2, OwnerKey: []byte(strings.Repeat("a", 64)), Location: storageOwnerLocation{Version: 1, Path: []byte("/original/config/data"), Previous: []byte{}, Selected: selected}, MaxBytes: 16384})
			var err error
			last := len(next.OwnerControls) - 1
			next.OwnerControls[last].RelationDigest, err = next.controlDigest(next.OwnerControls[last])
			storageTestMust(t, err)
			storageTestMust(t, storageEnrollmentSortControls(&next))
			source := storageEnrollmentPeakSource{Namespace: []byte("data"), Default: &storageEnrollmentDefaultSource{Workflow: 1, Branch: storageCreationConfigured, Settings: config, Selected: selected, Marker: marker, Location: epNative(933, 2), LocationPayload: epHash("location", []byte("handwritten")), CreationPage: epHash("creation", []byte("handwritten")), SelectedPath: []byte("/original/config/data"), Next: []byte{}}}
			before := ownerCatalogClone(old)
			observed := ownerCatalogClone(next)
			if !storageEnrollmentOriginalObjectsObserved(old, next, []storageOwnerPersistentObject{config}) {
				t.Fatal("original natives and relations lost during discovery reorder")
			}
			restored, err := storageEnrollmentRestoreCreationDictionary(old, next, &source)
			storageTestMust(t, err)
			if !reflect.DeepEqual(restored, old) || !reflect.DeepEqual(old, before) || !reflect.DeepEqual(next, observed) {
				t.Fatal("restoration rewrote original or observed evidence")
			}
			// The published-Scope branch may already observe the complete desired
			// control set. Full desired objects and control/registry material still
			// precede exact dictionary restoration; equal counts alone do not prove it.
			desired := ownerCatalogClone(next)
			desired.Objects[0].Source = 3
			desired.Objects[0].OwnerClass = 2
			desired.Objects[0].Parent = storageOwnerNoObject
			desired.Objects[0].Name = []byte{}
			desired.Objects[1].Source = 3
			desired.Objects[1].OwnerClass = 2
			desired.Roots = []storageOwnerRoot{{Role: 4, References: []storagebudget.ID{}, Source: storageOwnerRootSource{Kind: 4, OwnerKey: []byte{}, Path: []byte{}, Relative: []byte{}}, DirectoryObject: 0, Membership: storageOwnerMembership{ExistingObjects: []uint32{}}}}
			if !storageEnrollmentOriginalObjectsObserved(desired, next, []storageOwnerPersistentObject{config, selected}) {
				t.Fatal("complete desired native/parent graph refused")
			}
			exactDesired, err := storageEnrollmentRestoreCreationDictionary(desired, next, nil)
			storageTestMust(t, err)
			if !reflect.DeepEqual(exactDesired, desired) {
				t.Fatal("already-observed desired dictionary was not restored exactly")
			}
			unchanged, err := storageEnrollmentRestoreCreationDictionary(old, old, nil)
			storageTestMust(t, err)
			if !reflect.DeepEqual(unchanged, old) {
				t.Fatal("ordinary dictionary restoration changed bytes")
			}
			for _, name := range []string{"owner", "config-native", "parent", "location-owner", "location-relation", "location-bound", "location-path", "duplicate", "missing", "registry"} {
				t.Run(name, func(t *testing.T) {
					bad := ownerCatalogClone(next)
					switch name {
					case "owner":
						bad.OriginalOwner.HomeOwner = strings.Repeat("b", 64)
					case "config-native":
						bad.Objects[2].Persistent = epNative(934, 1)
					case "parent":
						bad.OwnerControls[0].ParentObject = 0
					case "location-owner":
						bad.OwnerControls[0].OwnerKey = []byte(strings.Repeat("b", 64))
					case "location-relation":
						bad.OwnerControls[0].RelationDigest[0] ^= 1
					case "location-bound":
						bad.OwnerControls[0].MaxBytes++
					case "location-path":
						bad.OwnerControls[0].Location.Path = []byte("/changed/data")
					case "duplicate":
						bad.OwnerControls = append(bad.OwnerControls, bad.OwnerControls[0])
					case "missing":
						bad.OwnerControls = bad.OwnerControls[1:]
					case "registry":
						if kind == 2 {
							bad.OwnerControls[1].OwnerKey = []byte(strings.Repeat("b", 64))
						} else {
							bad.OriginalOwner.ConfigPath = []byte("/changed/config")
						}
					}
					if _, err := storageEnrollmentRestoreCreationDictionary(old, bad, &source); err == nil {
						t.Fatal("changed original/control evidence admitted")
					}
				})
			}
			wrong := source
			wrong.Namespace = []byte("arbitrary")
			if _, err := storageEnrollmentRestoreCreationDictionary(old, next, &wrong); err == nil {
				t.Fatal("unadmitted default context accepted")
			}
			if _, err := storageEnrollmentRestoreCreationDictionary(old, next, nil); err == nil {
				t.Fatal("added relation accepted as ordinary dictionary")
			}
		})
	}
}

func TestStorageEnrollmentCreationRestartOriginalObjectRelationsAreExact(t *testing.T) {
	config, lock, history, marker, scope := epNative(940, 1), epNative(941, 2), epNative(942, 1), epNative(943, 2), epNative(944, 1)
	original := storageOwnerCatalogV2{Objects: []storageOwnerObject{{Role: 1, Source: 1, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: config, OwnerClass: 1, ModeClass: 1}, {Role: 18, Source: 1, Parent: 0, Name: []byte(".data.lock"), Persistent: lock, OwnerClass: 1, ModeClass: 2}, {Role: 21, Source: 3, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: history, OwnerClass: 2, ModeClass: 1}, {Role: 23, Source: 3, Parent: 2, Name: []byte(dataMarkerName), Persistent: marker, OwnerClass: 2, ModeClass: 2, FixedLength: 96}, {Role: 20, Source: 3, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: scope, OwnerClass: 2, ModeClass: 1}}, Roots: []storageOwnerRoot{{Role: 4, Source: storageOwnerRootSource{Kind: 4}, DirectoryObject: 2}, {Role: 8, Source: storageOwnerRootSource{Kind: 9}, DirectoryObject: 4}}}
	// Fresh discovery has the same actual History/native marker through config's
	// data edge. The retained original independent boundary stays exact; only
	// the observations use the source class of that original config edge.
	observed := storageOwnerCatalogV2{Objects: []storageOwnerObject{original.Objects[2], original.Objects[3], original.Objects[0], original.Objects[1]}}
	observed.Objects[0].Source = 1
	observed.Objects[0].OwnerClass = 1
	observed.Objects[0].Parent = 2
	observed.Objects[0].Name = []byte("data")
	observed.Objects[1].Source = 1
	observed.Objects[1].OwnerClass = 1
	observed.Objects[1].Parent = 0
	observed.Objects[3].Parent = 2
	boundaries := []storageOwnerPersistentObject{config, history, scope}
	before := ownerCatalogClone(original)
	if !storageEnrollmentOriginalObjectsObserved(original, observed, boundaries) || !reflect.DeepEqual(original, before) {
		t.Fatal("exact original boundary/native relation not restored")
	}
	for _, name := range []string{"missing", "duplicate", "native", "parent", "name", "role", "source", "owner", "mode", "bound", "boundary-source", "boundary-owner"} {
		t.Run(name, func(t *testing.T) {
			bad := ownerCatalogClone(observed)
			switch name {
			case "missing":
				bad.Objects = bad.Objects[:3]
			case "duplicate":
				bad.Objects = append(bad.Objects, bad.Objects[3])
			case "native":
				bad.Objects[3].Persistent = epNative(945, 2)
			case "parent":
				bad.Objects[3].Parent = 0
			case "name":
				bad.Objects[3].Name = []byte("changed.lock")
			case "role":
				bad.Objects[3].Role = 17
			case "source":
				bad.Objects[3].Source = 2
			case "owner":
				bad.Objects[3].OwnerClass = 2
			case "mode":
				bad.Objects[3].ModeClass = 1
			case "bound":
				bad.Objects[3].FixedLength = 1
			case "boundary-source":
				bad.Objects[0].Source = 2
			case "boundary-owner":
				bad.Objects[0].OwnerClass = 2
			}
			if storageEnrollmentOriginalObjectsObserved(original, bad, boundaries) {
				t.Fatal("changed/missing/ambiguous original native relation accepted")
			}
		})
	}
	if storageEnrollmentOriginalObjectsObserved(original, observed, boundaries[:2]) {
		t.Fatal("unheld original personal scope accepted")
	}
	duplicate := ownerCatalogClone(original)
	duplicate.Objects = append(duplicate.Objects, duplicate.Objects[1])
	if storageEnrollmentOriginalObjectsObserved(duplicate, observed, boundaries) {
		t.Fatal("ambiguous original dictionary accepted")
	}
	wrong := ownerCatalogClone(original)
	wrong.Roots[0].Source.ParentRoot = storagebudget.ID{1}
	if storageEnrollmentOriginalObjectsObserved(wrong, observed, boundaries) {
		t.Fatal("changed original history parent relation accepted")
	}
}

// Real original discovery followed by the required legacy recovery observation.
// The lower dictionary joins below are data comparisons, not a native receipt:
// actual qualified Workflow3 crash/restart acceptance is a separate required test.
func TestStorageEnrollmentLegacyConfigPointerDiscoveryReachesRecoveryDictionary(t *testing.T) {
	for _, change := range []string{"unchanged", "same-body-new-native", "same-selection-new-payload"} {
		t.Run(change, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			storageTestMust(t, os.Mkdir(cfg.DeskConfigDir, 0700))
			name := "conversations-" + strings.Repeat("a", 64) + ".json"
			const history = `{"version":1,"chats":[]}`
			privateFixture(t, filepath.Join(cfg.DeskConfigDir, name), history)
			pointer, err := json.Marshal(dataLocation{Version: 1, Path: cfg.DeskConfigDir})
			storageTestMust(t, err)
			privateFixture(t, filepath.Join(cfg.DeskConfigDir, dataLocationName), string(pointer))
			s := ownerActualNew(t, cfg)
			c := s.storageConstruction
			x := c.sources
			original := ownerCatalogClone(c.ref.cell.ownerCatalog)
			fileInfo, err := os.Stat(filepath.Join(s.configDir, name))
			storageTestMust(t, err)
			hosts, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
			storageTestMust(t, err)
			// Ordinary V3 keeps its strict orphan-control interpretation. Its actual
			// explicit selection is discovered, then Peak reconciliation observes the
			// exact required control from that original pointer and config native.
			explicit := 0
			for _, v := range x.selections {
				if v.settings == c.original.config && v.explicit && v.location.Path == s.configDir && v.location.Previous == "" {
					explicit++
				}
			}
			if explicit != 1 {
				t.Fatal("actual original legacy pointer was not discovered")
			}
			storageTestMust(t, x.observeLegacyHistoryControl(t.Context()))
			config := c.ref.cell.ownerRecord.Config
			want := storageOwnerControl{Role: 2, ParentObject: x.configObject, Name: []byte(dataLocationName), OwnerKind: 2, OwnerKey: []byte(s.storageOwner()), Location: storageOwnerLocation{Version: 1, Path: []byte(s.configDir), Previous: []byte{}, Selected: config}, MaxBytes: 16384}
			want.RelationDigest, err = original.controlDigest(want)
			storageTestMust(t, err)
			if len(x.controls) != len(original.OwnerControls)+1 || !reflect.DeepEqual(x.controls[len(x.controls)-1], want) {
				t.Fatal("discovery omitted or changed exact config-selected pointer control")
			}
			for _, root := range x.roots {
				if root.role == 4 {
					t.Fatal("legacy pointer observation invented a History root")
				}
			}
			for _, entry := range []string{"data", dataMarkerName} {
				if _, err = os.Lstat(filepath.Join(s.configDir, entry)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("legacy pointer observation created namespace", entry, err)
				}
			}
			var native storageOwnerPersistentObject
			for _, v := range x.selections {
				if v.settings == c.original.config {
					if v.pointer == nil || !bytes.Equal(v.pointer.immutable, pointer) {
						t.Fatal("original pointer bytes/native were not retained")
					}
					native = v.pointerNative
				}
			}
			source := storageEnrollmentPeakSource{Namespace: []byte("legacy"), Default: &storageEnrollmentDefaultSource{Workflow: 3, Branch: storageCreationConfigured, Settings: config, Selected: config, Location: native, LocationPayload: sha256.Sum256(pointer), CreationPage: epHash("handwritten data-only page operand", []byte{1}), SelectedPath: []byte(s.configDir), Next: []byte{}}}
			observed := storageOwnerCatalogV2{OriginalOwner: x.owner, Objects: x.objects, OwnerControls: x.controls}
			before, err := storageEnrollmentRestoreCreationDictionary(original, observed, &source)
			storageTestMust(t, err)
			if !reflect.DeepEqual(before, original) {
				t.Fatal("retired-before-successor legacy dictionary did not restore exact original")
			}
			desired := ownerCatalogClone(original)
			desired.OwnerControls = append(desired.OwnerControls, want)
			after, err := storageEnrollmentRestoreCreationDictionary(desired, observed, nil)
			storageTestMust(t, err)
			if !reflect.DeepEqual(after, desired) {
				t.Fatal("completed legacy dictionary lost exact selected control")
			}
			storageTestMust(t, x.observeCatalogLegacyHistoryControl(t.Context(), desired))
			if len(x.controls) != len(desired.OwnerControls) {
				t.Fatal("repeat observation duplicated a control or lifetime")
			}
			switch change {
			case "same-body-new-native":
				storageTestMust(t, os.Rename(filepath.Join(s.configDir, dataLocationName), filepath.Join(s.configDir, "retained-original-pointer")))
				storageTestMust(t, os.WriteFile(filepath.Join(s.configDir, dataLocationName), pointer, 0600))
			case "same-selection-new-payload":
				changed, err := json.Marshal(struct {
					Path    string `json:"path"`
					Version int    `json:"version"`
				}{s.configDir, 1})
				storageTestMust(t, err)
				if bytes.Equal(changed, pointer) || len(changed) != len(pointer) {
					t.Fatal("handwritten changed payload precondition")
				}
				storageTestMust(t, os.WriteFile(filepath.Join(s.configDir, dataLocationName), changed, 0600))
			}
			currentErr := x.current(t.Context())
			if change == "unchanged" {
				storageTestMust(t, currentErr)
			} else if currentErr == nil {
				t.Fatal("matching selected path/body replaced original pointer custody")
			}
			currentHosts, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
			storageTestMust(t, err)
			if !bytes.Equal(hosts, currentHosts) {
				t.Fatal("discovery/dictionary comparison changed host bytes")
			}
			raw, err := os.ReadFile(filepath.Join(s.configDir, name))
			storageTestMust(t, err)
			afterInfo, err := os.Stat(filepath.Join(s.configDir, name))
			storageTestMust(t, err)
			if string(raw) != history || !os.SameFile(fileInfo, afterInfo) {
				t.Fatal("legacy history was replaced or rewritten")
			}
		})
	}
}

// Expected capsule bytes are handwritten independently of the production
// encoder; source bytes use the preexisting independent field-order vector.
func storageSuccessorIntentVector(source storageEnrollmentPeakSource, kind, family, flags byte) []byte {
	header := make([]byte, 96)
	copy(header, "DSF1")
	header[4], header[5], header[6], header[7], header[8] = 1, 1, family, kind, flags
	body := storageDefaultSourceVector(source)
	epSet32(header[12:], uint32(len(body)))
	if flags != 0 {
		header[16] = 91
	}
	header[32] = 92
	header[64] = 93
	return append(header, body...)
}
func storageSuccessorSourceVector(t *testing.T, family byte) storageEnrollmentPeakSource {
	t.Helper()
	_, _, source, _, _ := epCatalogs(t)
	source.Namespace = []byte("data")
	source.Default = &storageEnrollmentDefaultSource{Workflow: 1, Branch: storageCreationConfigured, Settings: epNative(200, 1), Selected: epNative(201, 1), Marker: epNative(202, 2), Location: epNative(203, 2), LocationPayload: storagebudget.Digest{31}, CreationPage: storagebudget.Digest{32}, SelectedPath: []byte("/selected/data"), Next: []byte{}}
	switch family {
	case 2, 3:
		source.Namespace = []byte("history-base")
		source.Default = &storageEnrollmentDefaultSource{Workflow: 2, Branch: storageCreationPersonal, Settings: epNative(200, 1), Selected: epNative(201, 1), SelectedPath: []byte("/selected/ancestor"), Next: []byte("next")}
		if family == 3 {
			source.Default.OwnSelf = 1
			source.Default.CreationPage = storagebudget.Digest{33}
		}
	case 4:
		source.Namespace = []byte("legacy")
		source.Default.Workflow = 3
		source.Default.Selected = source.Default.Settings
		source.Default.Marker = storageOwnerPersistentObject{}
		source.Default.SelectedPath = []byte("/selected/config")
	}
	return source
}
func TestStorageEnrollmentSuccessorIntentClosedWire(t *testing.T) {
	for family := byte(1); family <= 4; family++ {
		for _, flags := range []byte{0, 1, 3} {
			source := storageSuccessorSourceVector(t, family)
			kind := byte(9)
			if family == 2 || family == 3 {
				kind = 10
			}
			want := storageSuccessorIntentVector(source, kind, family, flags)
			decoded, err := storageEnrollmentReadSuccessorIntent(want)
			storageTestMust(t, err)
			if decoded.Family != family || decoded.SourceKind != kind || decoded.OriginFlags != flags || !bytes.Equal(decoded.Source, storageDefaultSourceVector(source)) {
				t.Fatal("lost closed capsule operands", family, flags)
			}
			encoded, err := storageEnrollmentWriteSuccessorIntent(decoded)
			storageTestMust(t, err)
			if !bytes.Equal(encoded, want) {
				t.Fatal("handwritten capsule differs", family, flags)
			}
			for _, mutate := range []func([]byte) []byte{
				func(v []byte) []byte { v[0] ^= 1; return v }, func(v []byte) []byte { v[4] = 2; return v }, func(v []byte) []byte { v[5] = 2; return v }, func(v []byte) []byte { v[6] = 0; return v }, func(v []byte) []byte { v[7] = 8; return v }, func(v []byte) []byte { v[8] = 2; return v }, func(v []byte) []byte { v[9] = 1; return v }, func(v []byte) []byte { epSet32(v[12:], 0); return v }, func(v []byte) []byte { epSet32(v[12:], 13107); return v }, func(v []byte) []byte {
					if flags == 0 {
						v[16] = 1
					} else {
						clear(v[16:32])
					}
					return v
				}, func(v []byte) []byte { clear(v[32:64]); return v }, func(v []byte) []byte { clear(v[64:96]); return v }, func(v []byte) []byte { return v[:len(v)-1] }, func(v []byte) []byte { return append(v, 0) }, func(v []byte) []byte { return append(v, v...) },
			} {
				bad := mutate(append([]byte{}, want...))
				if _, err := storageEnrollmentReadSuccessorIntent(bad); err == nil {
					t.Fatal("accepted malformed capsule", family, flags)
				}
			}
		}
	}
	for _, branch := range []byte{storageCreationContained, storageCreationPersonal} {
		source := storageSuccessorSourceVector(t, 1)
		source.Default.Branch = branch
		if branch == storageCreationPersonal {
			source.Namespace = []byte("history")
		}
		want := storageSuccessorIntentVector(source, 9, 1, 1)
		decoded, err := storageEnrollmentReadSuccessorIntent(want)
		storageTestMust(t, err)
		got, err := storageEnrollmentWriteSuccessorIntent(decoded)
		storageTestMust(t, err)
		if !bytes.Equal(got, want) {
			t.Fatal("lost supported History branch", branch)
		}
	}
	legacy := storageSuccessorSourceVector(t, 4)
	legacy.Default.Branch = storageCreationPersonal
	if _, err := storageEnrollmentReadSuccessorIntent(storageSuccessorIntentVector(legacy, 9, 4, 0)); err != nil {
		t.Fatal("lost supported personal legacy branch", err)
	}
	// Maximum width is derived from handwritten source field widths, not an
	// encoder's reported length or an arbitrary full after-catalog JSON body.
	source := storageSuccessorSourceVector(t, 2)
	source.OwnerKey = bytes.Repeat([]byte{'o'}, 4096)
	source.HistoryIdentity = bytes.Repeat([]byte{'h'}, 4096)
	source.Default.SelectedPath = []byte("/" + strings.Repeat("p", 4095))
	source.Default.Next = bytes.Repeat([]byte{'n'}, 255)
	maximum := storageSuccessorIntentVector(source, 10, 2, 3)
	if len(maximum) != 96+13106 {
		t.Fatal("independent maximum capsule width", len(maximum))
	}
	decoded, err := storageEnrollmentReadSuccessorIntent(maximum)
	storageTestMust(t, err)
	got, err := storageEnrollmentWriteSuccessorIntent(decoded)
	storageTestMust(t, err)
	if !bytes.Equal(got, maximum) {
		t.Fatal("maximal source lost")
	}
	if 64+4160+256+8320+len(maximum) != 26002 || 8388608-26002 != 8362606 {
		t.Fatal("independent fixed frame bound")
	}
}

func storageSuccessorScopeVector(t *testing.T, path byte) (epComplete, epComplete, []byte, storageEnrollmentPeakScope) {
	t.Helper()
	f, prior, _ := epExisting(t, path, 1)
	// Independently author the retained old Completed envelope; the new
	// expected Snapshot never obtains old marker bytes from its encoder.
	previous := epDecode(t, prior)
	metadata := make([]byte, 4160)
	metadata[0], metadata[2] = 2, 255
	epSet32(metadata[4:], 4096)
	epSet32(metadata[12:], 4096)
	copy(metadata[16:48], previous.Scope[88:120])
	copy(metadata[48:64], previous.Scope[8:24])
	copy(metadata[64:], previous.Scope[:])
	marker := epBytes{}
	marker.fixed(uint32(7))
	marker.fixed(byte(3))
	marker.fixed(previous.Bank)
	marker.fixed(uint16(0))
	marker.fixed(previous.Installation)
	marker.fixed(previous.QBinding)
	marker.fixed(previous.Bridge)
	marker.fixed(storagebudget.Digest{1})
	marker.fixed(previous.Before)
	marker.fixed(epHash("desk-enrollment-inner-catalog-peak-v1", previous.Inner.Raw))
	marker.fixed(sha256.Sum256(previous.Raw))
	marker.fixed(previous.Extent)
	for _, native := range previous.Controls {
		marker.fixed(native)
	}
	for _, n := range []uint64{previous.CoveredEpoch, previous.BeforeRevision, previous.BeforeEpoch, previous.AfterRevision, previous.AfterEpoch} {
		marker.fixed(n)
	}
	marker.fixed(previous.Record)
	marker.fixed(previous.Claim)
	marker.fixed(previous.Ready)
	page := make([]byte, 4096)
	copy(page, marker.Bytes())
	old := epFrame("DSKONP1\x00", append(metadata, page...), 6, true)
	s := f.scope
	epSet16(s[:2], 2)
	oldDigest := sha256.Sum256(old)
	copy(s[88:120], oldDigest[:])
	founding := ownerAdvisoryWire(prior.before)
	digest := sha256.Sum256(founding)
	copy(s[1640:1672], digest[:])
	copy(s[1672:1704], prior.before.PreviousCatalogDigest[:])
	s[1704] = 1
	normalized := s
	clear(normalized[184:216])
	normalized[321] = 0
	clear(normalized[1080:1220])
	digest = epHash("desk-enrollment-control-peak-v2", normalized[:])
	copy(s[184:216], digest[:])
	return f, prior, old, s
}
func TestStorageEnrollmentSuccessorSnapshotExactBeforeAndPair(t *testing.T) {
	for _, path := range []byte{2, 3} {
		_, prior, old, scope := storageSuccessorScopeVector(t, path)
		previous := epDecode(t, prior)
		source := storageSuccessorSourceVector(t, 2)
		capsule := storageSuccessorIntentVector(source, 10, 2, 0)
		intent, err := storageEnrollmentReadSuccessorIntent(capsule)
		storageTestMust(t, err)
		full := sha256.Sum256(old)
		snapshot := make([]byte, 256)
		snapshot[0], snapshot[1] = 1, 3
		epSet32(snapshot[4:], 8256)
		epSet32(snapshot[8:], uint32(8320+len(capsule)))
		copy(snapshot[16:80], old[:64])
		copy(snapshot[80:112], full[:])
		inner := epHash("desk-enrollment-inner-catalog-peak-v1", previous.Inner.Raw)
		copy(snapshot[112:144], inner[:])
		epAtNative(snapshot[144:179], previous.Controls[previous.Bank])
		snapshot[179] = previous.Bank
		epSet64(snapshot[180:], previous.Extent)
		ed := sha256.Sum256(previous.Raw)
		copy(snapshot[188:220], ed[:])
		epSet64(snapshot[220:], previous.AfterRevision)
		epSet64(snapshot[228:], previous.AfterEpoch)
		data := append(append(append([]byte{}, snapshot...), old[:8320]...), capsule...)
		metadata := make([]byte, 4160)
		metadata[1], metadata[2] = 1, 3
		epSet32(metadata[4:], 4096)
		epSet32(metadata[8:], uint32(len(data)))
		copy(metadata[16:48], full[:])
		copy(metadata[48:64], scope[8:24])
		copy(metadata[64:], scope[:])
		want := epFrame("DSKONP1\x00", append(metadata, data...), 7, true)
		value := storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 1, Sequence: 7, Scope: scope, Snapshot: &storageEnrollmentPeakSnapshot{Kind: 1, Full: full, Inner: inner, Evidence: previous.Controls[previous.Bank], Bank: previous.Bank, Extent: previous.Extent, EvidenceDigest: ed, Q3Revision: previous.AfterRevision, Q3Epoch: previous.AfterEpoch, Before: old, Intent: &intent}}
		got, err := storageEnrollmentPeakEncodeEnvelope(value, prior.record, prior.claim)
		storageTestMust(t, err)
		if !bytes.Equal(got, want) {
			t.Fatal("exact handwritten envelope mismatch", path)
		}
		value.Snapshot.Kind = 0
		if _, err := storageEnrollmentPeakEncodeEnvelope(value, prior.record, prior.claim); err == nil {
			t.Fatal("new intent accepted unused wrong Snapshot kind")
		}
		value.Snapshot.Kind = 1
		decoded, err := storageEnrollmentPeakDecodeEnvelope(want, prior.record, prior.claim, true)
		storageTestMust(t, err)
		if decoded.Snapshot.Encoding != 3 || !bytes.Equal(decoded.Snapshot.Before, old) || sha256.Sum256(decoded.Snapshot.Before) != full {
			t.Fatal("capsule polluted original8MiB Before")
		}
		if _, err := storageEnrollmentPeakReadSnapshot(data, scope, 2, prior.record, prior.claim); err == nil {
			t.Fatal("old strict encoding admitted new Snapshot")
		}
		if _, bank, err := storageEnrollmentPeakDecodePair(old, want, prior.record, prior.claim); err != nil || bank != 1 {
			t.Fatal("one new Fence lost behind old Completed peer", err)
		}
		if _, _, err := storageEnrollmentPeakDecodePair(want, want, prior.record, prior.claim); err != nil {
			t.Fatal("exact twoFence pair refused", err)
		}
		value.Snapshot.Intent = nil
		legacy, err := storageEnrollmentPeakEncodeEnvelope(value, prior.record, prior.claim)
		storageTestMust(t, err)
		legacyData := append(append([]byte{}, snapshot...), old[:8320]...)
		legacyData[1] = 2
		epSet32(legacyData[8:], 8320)
		legacyMetadata := append([]byte{}, metadata...)
		legacyMetadata[2] = 2
		epSet32(legacyMetadata[8:], uint32(len(legacyData)))
		if !bytes.Equal(legacy, epFrame("DSKONP1\x00", append(legacyMetadata, legacyData...), 7, true)) {
			t.Fatal("legacy encoding2 bytes changed")
		}
		if _, _, err := storageEnrollmentPeakDecodePair(want, legacy, prior.record, prior.claim); err == nil {
			t.Fatal("mixed encoding pair accepted")
		}
		for _, at := range []int{2, 96, 4224 + 256 + 8320 + 5, len(want) - 1} {
			bad := append([]byte{}, want...)
			bad[at] ^= 1
			if _, _, err := storageEnrollmentPeakDecodePair(want, bad, prior.record, prior.claim); err == nil {
				t.Fatal("valid peer excused torn/unknown catalog", at)
			}
		}
		changed := append([]byte{}, capsule...)
		changed[16] = 1
		changed[8] = 1
		alteredData := append(append(append([]byte{}, snapshot...), old[:8320]...), changed...)
		altered := epFrame("DSKONP1\x00", append(append([]byte{}, metadata...), alteredData...), 7, true)
		if _, _, err := storageEnrollmentPeakDecodePair(want, altered, prior.record, prior.claim); err == nil {
			t.Fatal("different valid capsules accepted at same sequence")
		}
	}
}

func TestStorageEnrollmentSuccessorOriginalInputCommitment(t *testing.T) {
	f, prior, old, scope := storageSuccessorScopeVector(t, 2)
	original := storageEnrollmentPeakOriginalComparison{Catalogs: [2][]byte{old, append([]byte{}, old...)}, Checkpoints: [2][]byte{f.cp[0], f.cp[0]}, Tickets: f.tickets}
	for slot := range original.Hosts {
		for bank := range original.Hosts[slot] {
			original.Hosts[slot][bank] = make([]byte, 16384)
		}
	}
	steps := epPeerDispatch(epDecode(t, prior))
	step := steps[scope[324]]
	ready := f.record.QBinding[:]
	// Independent streaming tuple: fixed order/width and domain below are
	// handwritten, including both restored old C banks and every zero host.
	h := sha256.New()
	h.Write([]byte("desk-successor-fence-original-inputs-v1\x00"))
	h.Write([]byte{1, 0})
	h.Write(scope[:])
	h.Write(old)
	h.Write(old)
	var length [8]byte
	epSet64(length[:], uint64(len(prior.raw)))
	h.Write(length[:])
	h.Write(prior.raw)
	h.Write(ready)
	for _, raw := range original.Checkpoints {
		h.Write(raw)
	}
	for _, raw := range original.Tickets {
		h.Write(raw)
	}
	for _, pair := range original.Hosts {
		for _, raw := range pair {
			h.Write(raw)
		}
	}
	h.Write(step)
	var expected storagebudget.Digest
	copy(expected[:], h.Sum(nil))
	got, err := storageEnrollmentSuccessorInputs(scope, original, prior.raw, ready, step)
	storageTestMust(t, err)
	if got != expected {
		t.Fatal("exact original input tuple drift")
	}
	if 2+4096+2*8388608+8+71303168+32+2*4194304+2*1048576+64*2*16384+4096 != 100671530 {
		t.Fatal("independent maximal tuple width")
	}
	for _, kind := range []string{"catalog0", "catalog1", "checkpoint0", "checkpoint1", "ticket0", "ticket1", "host0-peer0", "host63-peer1", "prior", "ready", "step3", "scope"} {
		changed := original
		sc := scope
		body := prior.raw
		r := ready
		d := step
		flip := func(raw []byte) []byte { v := append([]byte{}, raw...); v[len(v)-1] ^= 1; return v }
		switch kind {
		case "catalog0":
			changed.Catalogs[0] = flip(changed.Catalogs[0])
		case "catalog1":
			changed.Catalogs[1] = flip(changed.Catalogs[1])
		case "checkpoint0":
			changed.Checkpoints[0] = flip(changed.Checkpoints[0])
		case "checkpoint1":
			changed.Checkpoints[1] = flip(changed.Checkpoints[1])
		case "ticket0":
			changed.Tickets[0] = flip(changed.Tickets[0])
		case "ticket1":
			changed.Tickets[1] = flip(changed.Tickets[1])
		case "host0-peer0":
			changed.Hosts[0][0] = flip(changed.Hosts[0][0])
		case "host63-peer1":
			changed.Hosts[63][1] = flip(changed.Hosts[63][1])
		case "prior":
			body = flip(body)
		case "ready":
			r = flip(r)
		case "step3":
			d = flip(d)
		case "scope":
			sc[8] ^= 1
		}
		digest, err := storageEnrollmentSuccessorInputs(sc, changed, body, r, d)
		if err == nil && digest == expected {
			t.Fatal("original operand omitted from commitment", kind)
		}
	}
	bad := original
	bad.Hosts[63][1] = bad.Hosts[63][1][:16383]
	if _, err := storageEnrollmentSuccessorInputs(scope, bad, prior.raw, ready, step); err == nil {
		t.Fatal("short original host accepted")
	}
}
func TestStorageEnrollmentSuccessorOriginCandidatePresence(t *testing.T) {
	prior, original, f := epOriginFixture(t)
	candidate := storagebudget.ID{91}
	flags, err := storageEnrollmentSuccessorOriginFlags(original, prior, candidate, f.record)
	storageTestMust(t, err)
	if flags != 1 {
		t.Fatal("all retained histories lost unused candidate rule")
	}
	missing := prior
	missing.Witnesses = append([]storageEnrollmentPeakWitness{}, prior.Witnesses...)
	selected := -1
	for i, w := range missing.Witnesses {
		if w.Selected && w.Slot == 63 {
			selected = i
			break
		}
	}
	if selected < 0 {
		t.Fatal("handwritten slot63 selected witness absent")
	}
	missing.Witnesses = append(missing.Witnesses[:selected], missing.Witnesses[selected+1:]...)
	flags, err = storageEnrollmentSuccessorOriginFlags(original, missing, candidate, f.record)
	storageTestMust(t, err)
	if flags != 3 {
		t.Fatal("one new selected page failed to retain fresh ID")
	}
	retainedID := prior.Board.Origins[0].ID
	if _, err = storageEnrollmentSuccessorOriginFlags(original, missing, retainedID, f.record); err == nil {
		t.Fatal("retained fresh candidate collided with an existing retained origin")
	}
	flags, err = storageEnrollmentSuccessorOriginFlags(original, prior, retainedID, f.record)
	storageTestMust(t, err)
	if flags != 1 {
		t.Fatal("discarded candidate unnecessarily changed retained origins")
	}
	invalid := prior
	invalid.Witnesses = append([]storageEnrollmentPeakWitness{}, prior.Witnesses...)
	for i := range invalid.Witnesses {
		if invalid.Witnesses[i].Selected {
			invalid.Witnesses[i].Origin = 65535
			break
		}
	}
	if _, err = storageEnrollmentSuccessorOriginFlags(original, invalid, candidate, f.record); err == nil {
		t.Fatal("missing retained-origin reference accepted")
	}
	if _, err = storageEnrollmentSuccessorOriginFlags(original, prior, storagebudget.ID{}, f.record); err == nil {
		t.Fatal("nonempty history accepted absent candidate")
	}
	for slot := range original.Hosts {
		for bank := range original.Hosts[slot] {
			original.Hosts[slot][bank] = make([]byte, 16384)
		}
	}
	flags, err = storageEnrollmentSuccessorOriginFlags(original, prior, storagebudget.ID{}, f.record)
	storageTestMust(t, err)
	if flags != 0 {
		t.Fatal("empty history invented candidate")
	}
	if _, err = storageEnrollmentSuccessorOriginFlags(original, prior, candidate, f.record); err == nil {
		t.Fatal("empty history accepted unused generated candidate")
	}
}

func TestStorageEnrollmentPrivateChildSourceClosedTrailer(t *testing.T) {
	_, _, parent, _, _ := epCatalogs(t)
	parent.Default = nil
	parent.Namespace = []byte(".desk-private")
	legacy := epSourceBytes(parent) // handwritten field writer; not production encoding
	got, err := storageEnrollmentPeakEncodeSource(parent)
	storageTestMust(t, err)
	if !bytes.Equal(got, legacy) {
		t.Fatal("parent source bytes changed")
	}
	child := parent
	child.ActionPageDigest = storagebudget.Digest{71}
	tail := make([]byte, 48)
	copy(tail, []byte{0x44, 0x53, 0x4b, 0x50, 0x43, 0x31, 0, 0})
	tail[8], tail[10], tail[16] = 1, 48, 71
	want := append(append([]byte{}, legacy...), tail...)
	got, err = storageEnrollmentPeakEncodeSource(child)
	storageTestMust(t, err)
	if !bytes.Equal(got, want) {
		t.Fatal("child trailer offsets/widths differ")
	}
	parsed, err := storageEnrollmentPeakDecodeSource(want)
	storageTestMust(t, err)
	if !reflect.DeepEqual(parsed, child) {
		t.Fatal("lost exact child source")
	}
	projected, err := storageEnrollmentPrivateParentSource(child)
	storageTestMust(t, err)
	if !bytes.Equal(projected, legacy) || child.ActionPageDigest != (storagebudget.Digest{71}) {
		t.Fatal("projection changed child or parent")
	}
	for _, offset := range []int{0, 6, 8, 9, 10, 11, 12, 15} {
		bad := append([]byte{}, want...)
		bad[len(legacy)+offset] ^= 1
		if _, err := storageEnrollmentPeakDecodeSource(bad); err == nil {
			t.Fatal("accepted malformed trailer", offset)
		}
	}
	zero := append([]byte{}, want...)
	clear(zero[len(legacy)+16:])
	for _, bad := range [][]byte{zero, want[:len(want)-1], append(append([]byte{}, want...), 0), append(append([]byte{}, want...), tail...)} {
		if _, err := storageEnrollmentPeakDecodeSource(bad); err == nil {
			t.Fatal("accepted zero/truncated/extra/repeated trailer")
		}
	}
	for _, input := range []storageEnrollmentPeakSource{parent, child} {
		for kind := byte(7); kind <= 10; kind++ {
			want := byte(0)
			if kind == 7 && input.ActionPageDigest == (storagebudget.Digest{}) {
				want = 5
			}
			if kind == 8 && input.ActionPageDigest != (storagebudget.Digest{}) {
				want = 6
			}
			if got := storageEnrollmentSuccessorFamily(kind, input); got != want {
				t.Fatal("private family cross product", kind, got, want)
			}
		}
	}
	for family := byte(1); family <= 4; family++ {
		old := storageSuccessorSourceVector(t, family)
		old.ActionPageDigest = storagebudget.Digest{71}
		if _, err := storageEnrollmentPeakEncodeSource(old); err == nil || storageEnrollmentSuccessorFamily(9, old) != 0 || storageEnrollmentSuccessorFamily(10, old) != 0 {
			t.Fatal("history source accepted private trailer")
		}
	}
	max := child
	max.OwnerKey = bytes.Repeat([]byte{'o'}, 4096)
	max.HistoryIdentity = bytes.Repeat([]byte{'h'}, 4096)
	wire, err := storageEnrollmentPeakEncodeSource(max)
	storageTestMust(t, err)
	if len(wire) != 8591 || len(wire) > 13106 {
		t.Fatal("private source bound changed", len(wire))
	}
}
