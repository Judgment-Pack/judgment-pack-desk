package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"reflect"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const storageHostFanoutWorkflow = "storage-single-file-fanout-v1"

// This closed ordinary-file variant selects the FULL retained owner reference
// set for the data group and no attribution for fixed control. It is not an
// arbitrary membership codec and cannot be used for a transfer or creation.
// Existing catalog retention preserves each root reference list and workspace
// definition; original physical root/generation/capacity checks remain required.
type storageIntentFanout struct {
	Binding storagebudget.Digest
	Initial storagebudget.Token
}

func storageSingleFileFanout(r storageIntentRecord, roots map[storagebudget.ID]*storageRootBinding, workspaces map[storagebudget.ID]storagebudget.Workspace) (*storagebudget.WorkspaceFanout, storagebudget.Digest, error) {
	if len(r.Components) < 1 || len(r.Components) > 2 {
		return nil, storagebudget.Digest{}, storagebudget.ErrBounds
	}
	f := &storagebudget.WorkspaceFanout{Memberships: make([]storagebudget.WorkspaceMembership, len(r.Components))}
	data := roots[r.Components[0].Root]
	if data == nil {
		return nil, storagebudget.Digest{}, storagebudget.ErrAuthority
	}
	category, refs := data.categoryOf(r.Target)
	if category != r.Components[0].Category || len(refs) <= storagebudget.MaxComponents-1 || len(refs) > storagebudget.MaxWorkspaces {
		return nil, storagebudget.Digest{}, storagebudget.ErrAuthority
	}
	refs = append([]storagebudget.ID(nil), refs...)
	sort.Slice(refs, func(i, j int) bool { return bytes.Compare(refs[i][:], refs[j][:]) < 0 })
	for i, id := range refs {
		w, ok := workspaces[id]
		if !ok || id == (storagebudget.ID{}) || w.Generation == 0 || i > 0 && id == refs[i-1] {
			return nil, storagebudget.Digest{}, storagebudget.ErrAuthority
		}
		f.Workspaces = append(f.Workspaces, storagebudget.WorkspaceReference{ID: id, Generation: w.Generation})
		f.Memberships[0][i/8] |= 1 << uint(i%8)
	}
	merged := data.configControl && category == storagebudget.Control
	if merged && len(r.Components) != 1 || !merged && len(r.Components) != 2 {
		return nil, storagebudget.Digest{}, storagebudget.ErrAuthority
	}
	for i, c := range r.Components {
		if c.Workspace != (storagebudget.ID{}) || c.WorkspaceGeneration != 0 || c.SettlementAttribution != (storagebudget.Vector{}) {
			return nil, storagebudget.Digest{}, storagebudget.ErrAuthority
		}
		want := storagebudget.Vector{}
		if i == 0 && len(refs) > 0 {
			want = storagebudget.Vector{Bytes: r.NewLength, Files: 1}
		}
		if c.Attribution != want || i == 0 && c.Physical != (storagebudget.Vector{Bytes: r.NewLength, Files: 1}) || i > 0 && (roots[c.Root] == nil || !roots[c.Root].configControl || c.Category != storagebudget.Control || c.Physical != (storagebudget.Vector{})) {
			return nil, storagebudget.Digest{}, storagebudget.ErrAuthority
		}
		settlement := storagebudget.Vector{}
		if i > 0 || merged {
			settlement.Bytes = 64 << 10
		}
		if c.SettlementPhysical != settlement {
			return nil, storagebudget.Digest{}, storagebudget.ErrAuthority
		}
	}
	h := sha256.New()
	h.Write([]byte("desk-storage-ordinary-reference-set-v1\x00"))
	h.Write(data.row.ID[:])
	h.Write(data.row.Identity[:])
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], data.row.Generation)
	h.Write(n[:])
	h.Write([]byte{category})
	for _, w := range f.Workspaces {
		h.Write(w.ID[:])
		binary.LittleEndian.PutUint64(n[:], w.Generation)
		h.Write(n[:])
	}
	for _, m := range f.Memberships {
		h.Write(m[:])
	}
	var d storagebudget.Digest
	copy(d[:], h.Sum(nil))
	return f, d, nil
}

func (o *storageBudgetOwner) originalIntentFanout(r storageIntentRecord) (*storagebudget.WorkspaceFanout, error) {
	if r.Transfer != nil {
		return o.originalTransferIntentFanout(r)
	}
	if r.Fanout == nil {
		return nil, nil
	}
	f, d, err := storageSingleFileFanout(r, o.roots, o.workspaces)
	if err != nil {
		return nil, err
	}
	if d != r.Fanout.Binding {
		return nil, storagebudget.ErrAuthority
	}
	return f, nil
}

func storageIntentQWorkflow(r storageIntentRecord) string {
	if r.Transfer != nil {
		return storageTransferWorkflow
	}
	if r.Fanout != nil {
		return storageHostFanoutWorkflow
	}
	return storageHostWorkflow
}

func storageKnownIntentWorkflow(workflow string) bool {
	return workflow == storageHostWorkflow || workflow == storageHostFanoutWorkflow || workflow == storageTransferWorkflow
}

func (o *storageBudgetOwner) originalIntentRequest(r storageIntentRecord) (storagebudget.Request, error) {
	f, err := o.originalIntentFanout(r)
	if err != nil {
		return storagebudget.Request{}, err
	}
	request := storagebudget.Request{Workflow: storageIntentQWorkflow(r), Operation: storageIntentOperation(r), Owner: o.ref.cell.binding, Components: append([]storagebudget.Component(nil), r.Components...), Fanout: f}
	if f == nil {
		request.Entries = []storagebudget.Entry{{Kind: storagebudget.ObjectEntry, Component: 0, Target: r.Target, Stage: r.Stage, OldIdentity: r.CASBinding, NewIdentity: r.CASBinding, Bytes: r.NewLength, Files: 1}}
	}
	return request, nil
}

func storageIntentFanoutPlan(r storageIntentRecord) storagebudget.Digest {
	r = storageIntentImmutable(r)
	r.Changes = make([]storagebudget.Change, len(r.Components))
	b := make([]byte, storageIntentPage)
	if !storageIntentBodyCodec(b, &r, false) {
		return storagebudget.Digest{}
	}
	return storageEnrollmentPeakHash("desk-storage-ordinary-fanout-plan-v1", b[128:r.encodedLength()])
}

// Reconstruct from the exact retained catalog projection, never a fresh source
// discovery. Existing retention forbids replacing these definitions/references.
func storageCatalogIntentFanout(r storageIntentRecord, catalog storageOwnerCatalogV2) (*storagebudget.WorkspaceFanout, error) {
	if r.Transfer != nil {
		return storageTransferCatalogIntentFanout(r, catalog)
	}
	roots := map[storagebudget.ID]*storageRootBinding{}
	workspaces := map[storagebudget.ID]storagebudget.Workspace{}
	for _, row := range catalog.Roots {
		roots[row.Definition.ID] = &storageRootBinding{row: storageOwnerRootValue(row.Definition), category: row.Category, references: append([]storagebudget.ID(nil), row.References...), legacyReferences: append([]storagebudget.ID(nil), row.References...), configControl: row.Role == 1, qControl: row.Role == 2}
	}
	for _, row := range catalog.Workspaces {
		w := row.Definition
		workspaces[w.ID] = storagebudget.Workspace{ID: w.ID, Generation: w.Generation, Limits: w.Limits}
	}
	f, d, err := storageSingleFileFanout(r, roots, workspaces)
	if err != nil || r.Fanout == nil || d != r.Fanout.Binding {
		return nil, storagebudget.ErrAuthority
	}
	return f, nil
}

// The caller holds the original host control gate and supplies its checked Q
// inspection. Reconstruct purely from that exact snapshot: no extra Q/native
// call, retired-manifest read, or new authority is introduced.
func (o *storageBudgetOwner) validateAppliedIntentFanout(ctx context.Context, r storageIntentRecord, status storagebudget.HostState) error {
	if r.Fanout == nil && r.Transfer == nil {
		return nil
	}
	f, err := o.originalIntentFanout(r)
	if err != nil {
		return err
	}
	row := status.Slots[r.Token.Slot]
	if row.PersistedApplied != r.Token || row.Ticket.Token != r.Token {
		return nil
	} // Existing persisted retirement/reuse proof is checked separately. A newer ticket owns this manifest even while its predecessor remains Applied.
	if !storageIntentFanoutAppliedOperand(r, o.ref.cell.record.Installation, o.ref.cell.binding, row, f) {
		return storagebudget.ErrUnknown
	}
	return ctx.Err()
}

func storageIntentFanoutAppliedOperand(r storageIntentRecord, installation storagebudget.ID, binding storagebudget.Digest, row storagebudget.HostSlotState, f *storagebudget.WorkspaceFanout) bool {
	t := row.Ticket
	if r.Fanout == nil && r.Transfer == nil || row.PersistedApplied != r.Token || row.PersistedNextSequence <= r.Token.Sequence || t.Token != r.Token || t.State != storagebudget.Terminal || t.Owner != binding || t.Workflow != storageIntentQWorkflow(r) || t.Operation != storageIntentOperation(r) || t.PolicyRevision != r.PolicyRevision || t.Evidence != r.Evidence || t.Fanout == nil || !reflect.DeepEqual(t.Components, r.Components) || !reflect.DeepEqual(t.Changes, r.Changes) {
		return false
	}
	initial := storagebudget.Token{}
	if r.Transfer != nil {
		initial = r.Transfer.Initial
	} else if r.Fanout != nil {
		initial = r.Fanout.Initial
	}
	t.Token, t.Fanout = initial, f
	qt := storageEnrollmentPeakQTicketValue(t)
	b, err := storageEnrollmentPeakQEncodeFanoutManifest(qt, storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(installation), Binding: storageEnrollmentPeakQDigest(binding)})
	return err == nil && t.ManifestLength == storagebudget.ManifestBytes && sha256.Sum256(b) == t.Manifest
}

func storageIntentFanoutRetiredOperand(r storageIntentRecord, catalog storageOwnerCatalogV2, row storagebudget.HostSlotState) bool {
	if r.Fanout == nil && r.Transfer == nil {
		return true
	}
	f, err := storageCatalogIntentFanout(r, catalog)
	return err == nil && storageIntentFanoutAppliedOperand(r, catalog.Installation, catalog.QBinding, row, f)
}
