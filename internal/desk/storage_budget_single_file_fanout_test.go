package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// These use the existing synthetic filesystem owner fixture. They exercise
// actual host/Q admission and persistence; they issue no native qualification.
func storageWriterFanoutFixture(t *testing.T, count int) (*storageBudgetOwner, *storageRootBinding, string, storageSingleFilePlan) {
	t.Helper()
	o, root, config := storageWriterFixture(t, false)
	var refs []storagebudget.ID
	for i := 0; i < count; i++ {
		id, err := o.addWorkspace(fmt.Sprintf("fanout-workspace-%04d", i), uint64(i+1), storagebudget.Limits{})
		storageTestMust(t, err)
		refs = append(refs, id)
	}
	storageTestMust(t, o.legacyHistory(root, refs))
	storageTestMust(t, o.InitialCensus(t.Context()))
	p := storageWriterPlan(root)
	p.Target = "ai-memory-" + strings.Repeat("a", 64) + ".json"
	return o, root, config, p
}

func storageWriterFanoutCatalog(o *storageBudgetOwner) storageOwnerCatalogV2 {
	c := storageOwnerCatalogV2{Installation: o.ref.cell.record.Installation, QBinding: o.ref.cell.binding}
	for _, root := range o.roots {
		role := uint8(3)
		refs := root.references
		if root.configControl {
			role, refs = 1, root.legacyReferences
		}
		if root.qControl {
			role = 2
		}
		c.Roots = append(c.Roots, storageOwnerRoot{Definition: storageRegisteredRoot(root.row), Role: role, Category: root.category, References: append([]storagebudget.ID(nil), refs...)})
	}
	for _, w := range o.workspaces {
		c.Workspaces = append(c.Workspaces, storageOwnerWorkspace{Definition: storagebudget.RegisteredWorkspace{ID: w.ID, Generation: w.Generation, Limits: w.Limits}})
	}
	return c
}

func TestStorageSingleFileFanout1024ExactAdmissionSettlementAndRetirement(t *testing.T) {
	o, _, config, p := storageWriterFanoutFixture(t, 1024)
	a, err := o.admitSingleFile(t.Context(), p)
	storageTestMust(t, err)
	records := storageWriterRecords(t, o)
	if len(records) != 1 || records[0].Fanout == nil || len(records[0].Components) != 2 {
		t.Fatal("full reference set was refused or split into tickets")
	}
	r := records[0]
	status, err := o.ref.cell.q.InspectHostState(t.Context())
	storageTestMust(t, err)
	ticket := status.Slots[a.token.Slot].Ticket
	if ticket.Fanout == nil || len(ticket.Fanout.Workspaces) != 1024 || len(ticket.Fanout.Memberships) != 2 || ticket.Fanout.Memberships[0][127] != 255 || ticket.Fanout.Memberships[1] != (storagebudget.WorkspaceMembership{}) {
		t.Fatal("exact full membership/control partition lost")
	}
	request, err := o.ref.cell.q.Manifest(t.Context(), ticket.Token, o.ref.cell.binding)
	storageTestMust(t, err)
	expected, err := o.originalIntentRequest(r)
	storageTestMust(t, err)
	if len(request.Entries) != 0 || !reflect.DeepEqual(request, expected) || request.Operation == "single:"+storageHexID(r.OperationID) {
		t.Fatal("complete closed host plan not bound into original Q")
	}
	// The independent copy reads the actual complete native Q manifest bytes.
	raw, err := os.ReadFile(filepath.Join(config, storageAnchorName, "manifests"))
	storageTestMust(t, err)
	start := int(ticket.Token.Slot) * storagebudget.ManifestBytes
	qt := storageEnrollmentPeakQTicketValue(ticket)
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(o.ref.cell.record.Installation), Binding: storageEnrollmentPeakQDigest(o.ref.cell.binding)}
	parsed, err := storageEnrollmentPeakQDecodeFanoutManifest(append([]byte(nil), raw[start:start+storagebudget.ManifestBytes]...), qt, anchor)
	storageTestMust(t, err)
	if !storageEnrollmentPeakQSameFanout(parsed.Fanout, qt.Fanout) || storageEnrollmentPeakQValidateTicket(parsed) != nil {
		t.Fatal("independent reader lost full actual membership")
	}
	snapshot, err := o.ref.cell.q.Snapshot(t.Context())
	storageTestMust(t, err)
	if len(snapshot.Tickets) != 1 || snapshot.Promised[storagebudget.Memory] != (storagebudget.Vector{Bytes: uint64(len(p.Bytes)), Files: 1}) || snapshot.Promised[storagebudget.Control] != (storagebudget.Vector{Bytes: 64 << 10}) {
		t.Fatal("physical or settlement charged per reference")
	}
	storageTestMust(t, a.Write(t.Context()))
	storageTestMust(t, a.Close())
	got, err := os.ReadFile(filepath.Join(config, p.Target))
	storageTestMust(t, err)
	if !bytes.Equal(got, p.Bytes) {
		t.Fatal("wrong published payload")
	}
	snapshot, err = o.ref.cell.q.Snapshot(t.Context())
	storageTestMust(t, err)
	if len(snapshot.Tickets) != 0 || len(snapshot.Workspaces) != 1024 || snapshot.Installation[storagebudget.Memory] != (storagebudget.Vector{Bytes: uint64(len(p.Bytes)), Files: 1}) {
		t.Fatal("physical fanout or retirement changed")
	}
	for _, w := range snapshot.Workspaces {
		if w.Usage[storagebudget.Memory] != (storagebudget.Vector{Bytes: uint64(len(p.Bytes)), Files: 1}) {
			t.Fatal("member missing exact independent charge", w.ID)
		}
	}
	r = storageWriterRecords(t, o)[0]
	status, err = o.ref.cell.q.InspectHostState(t.Context())
	storageTestMust(t, err)
	if r.Phase != storageIntentRetired || status.Slots[r.Token.Slot].Ticket.Fanout == nil || len(status.Slots[r.Token.Slot].Ticket.Fanout.Workspaces) != 0 {
		t.Fatal("applied page incorrectly dereferenced retired dictionary")
	}
	catalog := storageWriterFanoutCatalog(o)
	if !storageIntentFanoutRetiredOperand(r, catalog, status.Slots[r.Token.Slot]) {
		t.Fatal("exact retained catalog cannot reconstruct original manifest")
	}
	// Retired payload is no longer an authority. Alter it in place and prove the
	// current original host reader reconstructs without opening its old manifest.
	file, err := os.OpenFile(filepath.Join(config, storageAnchorName, "manifests"), os.O_RDWR, 0)
	storageTestMust(t, err)
	_, err = file.WriteAt([]byte("retired-test-only"), int64(start))
	storageTestMust(t, err)
	storageTestMust(t, file.Close())
	storageTestMust(t, o.withControl(t.Context(), func(ctx context.Context, table *storageIntentTable) error {
		return o.validateIntentMappings(ctx, table, status)
	}))
	for _, field := range []string{"target", "stage", "CAS", "initial", "reference", "generation", "binding"} {
		t.Run(field, func(t *testing.T) {
			bad := r
			f := *r.Fanout
			bad.Fanout = &f
			c := catalog
			c.Roots = append([]storageOwnerRoot(nil), catalog.Roots...)
			c.Workspaces = append([]storageOwnerWorkspace(nil), catalog.Workspaces...)
			switch field {
			case "target":
				bad.Target = "different-target"
			case "stage":
				bad.Stage = ".different-stage"
			case "CAS":
				bad.CASBinding[0] ^= 1
			case "initial":
				bad.Fanout.Initial.Generation++
			case "reference":
				for i := range c.Roots {
					if c.Roots[i].Role == 1 {
						c.Roots[i].References = append([]storagebudget.ID(nil), c.Roots[i].References[1:]...)
					}
				}
			case "generation":
				c.Workspaces[0].Definition.Generation++
			case "binding":
				bad.Fanout.Binding[0] ^= 1
			}
			if storageIntentFanoutRetiredOperand(bad, c, status.Slots[r.Token.Slot]) {
				t.Fatal("retired original changed without refusal")
			}
		})
	}
}

func TestStorageSingleFileFanoutCutsKeepOriginalDebtAndRefuseReplay(t *testing.T) {
	for _, point := range []struct{ name, phase string }{{"reserve", "before"}, {"intent", "after-write"}, {"publish", "before-rename"}, {"publish", "after-rename"}, {"settle", "after"}, {"checkpoint", "after"}} {
		t.Run(point.name+"-"+point.phase, func(t *testing.T) {
			o, _, config, p := storageWriterFanoutFixture(t, 16)
			if point.name == "reserve" || point.name == "intent" {
				o.fault = &storageBudgetFault{Name: point.name, Phase: point.phase}
				if _, err := o.admitSingleFile(t.Context(), p); err == nil {
					t.Fatal("cut ignored")
				}
			} else {
				a, err := o.admitSingleFile(t.Context(), p)
				storageTestMust(t, err)
				o.fault = &storageBudgetFault{Name: point.name, Phase: point.phase}
				if err = a.Write(t.Context()); err == nil {
					t.Fatal("effect cut ignored")
				}
				storageTestMust(t, a.Close())
			}
			o.fault = nil
			before, err := o.ref.cell.q.InspectHostState(t.Context())
			storageTestMust(t, err)
			tree := storageTestTree(t, config)
			if _, err = o.admitSingleFile(t.Context(), p); err == nil {
				t.Fatal("uncertain original replayed")
			}
			after, err := o.ref.cell.q.InspectHostState(t.Context())
			storageTestMust(t, err)
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(tree, storageTestTree(t, config)) {
				t.Fatal("retry rewrote or forgave original debt")
			}
		})
	}
}

func TestStorageSingleFileFanoutFormatAndPlanAreClosed(t *testing.T) {
	o, _, _, p := storageWriterFanoutFixture(t, 16)
	a, err := o.admitSingleFile(t.Context(), p)
	storageTestMust(t, err)
	defer a.Close()
	r := storageWriterRecords(t, o)[0]
	b, err := storageEncodeIntent(r, o.ref.cell.record.Installation, o.ref.cell.binding)
	storageTestMust(t, err)
	if string(b[:8]) != "DSKHINT8" || r.encodedLength() != 15856 || len(b)-r.encodedLength() != 528 || !storageAllZero(b[15856:]) {
		t.Fatal("closed page/floor arithmetic changed")
	}
	got, err := storageDecodeIntent(b, int(r.Slot), o.ref.cell.record.Installation, o.ref.cell.binding)
	storageTestMust(t, err)
	if !reflect.DeepEqual(r, got) {
		t.Fatal("restart lost original dictionary binding/token")
	}
	witness := storageEnrollmentPeakWitness{Slot: r.Slot, Version: 8, Generation: r.Generation, Operation: r.OperationID, Digest: sha256.Sum256(b), Phase: byte(r.Phase), Page: b, Core: r}
	witnessBytes, err := storageEnrollmentPeakWriteWitnesses([]storageEnrollmentPeakWitness{witness}, o.ref.cell.record.Installation, o.ref.cell.binding)
	storageTestMust(t, err)
	witnesses, err := storageEnrollmentPeakReadWitnesses(witnessBytes, 1, o.ref.cell.record.Installation, o.ref.cell.binding)
	storageTestMust(t, err)
	if len(witnesses) != 1 || !reflect.DeepEqual(witnesses[0].Core, r) {
		t.Fatal("ordinary8 witness lost complete original record")
	}
	witness.Version = 2
	if _, err = storageEnrollmentPeakWriteWitnesses([]storageEnrollmentPeakWitness{witness}, o.ref.cell.record.Installation, o.ref.cell.binding); err == nil {
		t.Fatal("new witness relabeled as oldflat")
	}
	// Same-format peers select exact state; the legacy2 decoder's fixed header
	// and body-length contract cannot accept this page, even with a valid checksum.
	if string(b[:8]) == "DSKHINT2" || uint32(r.encodedLength()-128) == storageIntentBody {
		t.Fatal("new page aliases old flat format")
	}
	for _, field := range []string{"target", "stage", "CAS", "root", "locator", "length", "control"} {
		bad := r
		bad.Components = append([]storagebudget.Component(nil), r.Components...)
		bad.RootIdentities = append([]storagebudget.Digest(nil), r.RootIdentities...)
		bad.Locators = append([]string(nil), r.Locators...)
		switch field {
		case "target":
			bad.Target += "x"
		case "stage":
			bad.Stage += "x"
		case "CAS":
			bad.CASBinding[0] ^= 1
		case "root":
			bad.RootIdentities[0][0] ^= 1
		case "locator":
			bad.Locators[0] += "x"
		case "length":
			bad.NewLength++
		case "control":
			bad.Components[1].SettlementPhysical.Bytes++
		}
		if storageIntentOperation(bad) == storageIntentOperation(r) {
			t.Fatal("immutable host plan field omitted", field)
		}
	}
	// Flat records keep the exact former body and magic, without any extension.
	flat := r
	flat.Fanout = nil
	flat.Components = append([]storagebudget.Component(nil), r.Components...)
	flat.Components[0].Workspace = storagebudget.ID{1}
	flat.Components[0].WorkspaceGeneration = 1
	old, err := storageEncodeIntent(flat, o.ref.cell.record.Installation, o.ref.cell.binding)
	storageTestMust(t, err)
	if string(old[:8]) != "DSKHINT2" || flat.encodedLength() != 15791 || !storageAllZero(old[15791:]) {
		t.Fatal("old flat representation changed")
	}
	sum := append([]byte(nil), old...)
	clear(sum[76:108])
	d := sha256.Sum256(sum)
	if !bytes.Equal(d[:], old[76:108]) {
		t.Fatal("flat checksum contract changed")
	}
}

func TestStorageOwnerCategoryPartitionMatchesCompleteCensus(t *testing.T) {
	memory := "ai-memory-" + strings.Repeat("a", 64) + ".json"
	attachment := "attachment-" + strings.Repeat("a", 64) + "-00000000-0000-0000-0000-000000000000.json"
	refs := []storagebudget.ID{{1}, {2}}
	for _, row := range []struct {
		name     string
		root     storageRootBinding
		path     string
		want     uint8
		wantRefs bool
	}{
		{"managed-memory", storageRootBinding{category: storagebudget.History, references: refs}, memory, storagebudget.Memory, true},
		{"managed-attachment", storageRootBinding{category: storagebudget.History, references: refs}, attachment, storagebudget.Attachments, true},
		{"legacy-memory", storageRootBinding{configControl: true, legacyReferences: refs}, memory, storagebudget.Memory, true},
		{"legacy-attachment", storageRootBinding{configControl: true, legacyReferences: refs}, attachment, storagebudget.Attachments, true},
		{"retained-memory", storageRootBinding{category: storagebudget.Retained, references: refs}, memory, storagebudget.Retained, true},
		{"nested-legacy", storageRootBinding{configControl: true, legacyReferences: refs}, "nested/" + memory, storagebudget.Control, false},
		{"other-control", storageRootBinding{configControl: true, legacyReferences: refs}, "settings.json", storagebudget.Control, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			category, got := row.root.categoryOf(row.path)
			if category != row.want || !reflect.DeepEqual(got, func() []storagebudget.ID {
				if row.wantRefs {
					return refs
				}
				return nil
			}()) {
				t.Fatal("owner partition/attribution differs")
			}
		})
	}
	o, r, config := storageWriterFixture(t, false)
	id, err := o.addWorkspace("category-census-workspace", 1, storagebudget.Limits{})
	storageTestMust(t, err)
	storageTestMust(t, o.legacyHistory(r, []storagebudget.ID{id}))
	storageTestMust(t, os.WriteFile(filepath.Join(config, memory), []byte("memory"), 0600))
	storageTestMust(t, os.WriteFile(filepath.Join(config, attachment), []byte("attachment"), 0600))
	storageTestMust(t, o.InitialCensus(t.Context()))
	snapshot, err := o.ref.cell.q.Snapshot(t.Context())
	storageTestMust(t, err)
	if snapshot.Installation[storagebudget.Memory] != (storagebudget.Vector{Bytes: 6, Files: 1}) || snapshot.Installation[storagebudget.Attachments] != (storagebudget.Vector{Bytes: 10, Files: 1}) || len(snapshot.Workspaces) != 1 || snapshot.Workspaces[0].Usage[storagebudget.Memory] != (storagebudget.Vector{Bytes: 6, Files: 1}) || snapshot.Workspaces[0].Usage[storagebudget.Attachments] != (storagebudget.Vector{Bytes: 10, Files: 1}) {
		t.Fatal("complete census and owner categories diverge")
	}
}

func TestStorageCategoryCensusDoesNotForgiveOldHistoryPromise(t *testing.T) {
	o, r, config, p := storageWriterFanoutFixture(t, 1)
	p.Target = "conversations-" + strings.Repeat("b", 64) + ".json"
	// A retained pre-cutover History obligation has its original category. A new
	// complete census must refuse it; the new classifier grants no cancellation.
	components, err := o.writerComponents(t.Context(), p, r.root)
	storageTestMust(t, err)
	if components[0].Category != storagebudget.History {
		t.Fatal("history fixture not original category")
	}
	observations, err := o.writerObservations(t.Context(), components)
	storageTestMust(t, err)
	token, err := o.ref.cell.q.Reserve(t.Context(), storagebudget.Request{Workflow: "test-old-history", Operation: "old-unresolved", Owner: o.ref.cell.binding, Components: components}, observations)
	storageTestMust(t, err)
	before, err := o.ref.cell.q.InspectHostState(t.Context())
	storageTestMust(t, err)
	if err = o.InitialCensus(t.Context()); err == nil {
		t.Fatal("old unresolved promise cancelled by category cutover")
	}
	after, err := o.ref.cell.q.InspectHostState(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(before.Slots, after.Slots) || after.Slots[token.Slot].Ticket.Components[0].Category != storagebudget.History {
		t.Fatal("old category/token obligations changed")
	}
	if _, err = os.Stat(filepath.Join(config, p.Stage)); !os.IsNotExist(err) {
		t.Fatal("census created writer stage")
	}
}

func TestStorageSingleFileFanoutRetainedHostSurvivesQSlotReuse(t *testing.T) {
	o, _, _, p := storageWriterFanoutFixture(t, 16)
	a, err := o.admitSingleFile(t.Context(), p)
	storageTestMust(t, err)
	storageTestMust(t, a.Write(t.Context()))
	storageTestMust(t, a.Close())
	first := storageWriterRecords(t, o)[0]
	p.Target = "ai-memory-" + strings.Repeat("b", 64) + ".json"
	p.Operation = "second-original"
	p.Stage = ".second-stage"
	second, err := o.admitSingleFile(t.Context(), p)
	storageTestMust(t, err)
	status, err := o.ref.cell.q.InspectHostState(t.Context())
	storageTestMust(t, err)
	if second.token.Slot != first.Token.Slot || status.Slots[first.Token.Slot].PersistedApplied != first.Token || status.Slots[first.Token.Slot].Ticket.Token == first.Token {
		t.Fatal("fixture did not retain old Applied with newer live ticket")
	}
	storageTestMust(t, second.Write(t.Context()))
	storageTestMust(t, second.Close())
	records := storageWriterRecords(t, o)
	if len(records) != 2 || records[0].OperationID != first.OperationID || records[0].Phase != storageIntentRetired || records[1].Phase != storageIntentRetired {
		t.Fatal("old host promise overwritten or newer admission failed")
	}
	snapshot, err := o.ref.cell.q.Snapshot(t.Context())
	storageTestMust(t, err)
	for _, w := range snapshot.Workspaces {
		if w.Usage[storagebudget.Memory] != (storagebudget.Vector{Bytes: 2 * uint64(len(p.Bytes)), Files: 2}) {
			t.Fatal("slot reuse changed independent attribution")
		}
	}
}
