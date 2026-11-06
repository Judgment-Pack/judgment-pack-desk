package desk

import (
	"bytes"
	"crypto/sha256"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// The endpoint is installed only by original completed reconciliation or exact
// native-bound startup reconstruction. It preserves old evidence across this
// explained enrollment epoch without editing an old Admission/RetirementEpoch.
// This comparison grants no effect and still requires actual current mappings
// and all64 ticket absence/monotone persisted retirement in the owning caller.
func (o *storageBudgetOwner) coveredEnrollmentHost(host storageIntentSlot, status storagebudget.HostState) bool {
	if o == nil || !o.valid() || host.record.Slot >= 64 {
		return false
	}
	o.ref.cell.mu.Lock()
	e := o.ref.cell.enrollmentEndpoint
	o.ref.cell.mu.Unlock()
	if e == nil || e.Installation != o.ref.cell.record.Installation || e.QBinding != o.ref.cell.binding {
		return false
	}
	return storageEnrollmentPeakCoversHost(*e, host, status)
}

func storageEnrollmentPeakCoversHost(e storageEnrollmentPeakEvidence, host storageIntentSlot, status storagebudget.HostState) bool {
	if e.Phase != 2 || e.CoveredEpoch != status.Epoch || e.AfterRevision > status.Revision || host.record.Slot >= 64 {
		return false
	}
	for _, coverage := range e.Board.Coverage {
		if coverage.Slot != uint16(host.record.Slot) {
			continue
		}
		if int(coverage.Witness) >= len(e.Witnesses) || int(coverage.Origin) >= len(e.Board.Origins) {
			return false
		}
		w := e.Witnesses[coverage.Witness]
		origin := e.Board.Origins[coverage.Origin]
		if w.Creation != nil && host.record.Phase == storageIntentRetired && !storageCreationTailIncluded(*w.Creation, e) {
			return false
		}
		if w.Core.Transfer != nil && host.record.Phase == storageIntentRetired && !storageTransferTailBound(w.Core, e) {
			return false
		}
		if !w.Selected || w.Slot != coverage.Slot || w.Generation != host.record.Generation || w.Operation != host.record.OperationID || w.Digest != sha256.Sum256(host.page) || !bytes.Equal(w.Page, host.page) || coverage.Page != w.Digest || coverage.Epoch != status.Epoch || coverage.Bridge != e.Bridge || int(origin.View) >= len(e.Views) {
			return false
		}
		switch host.record.Phase {
		case storageIntentRejected:
			if w.Core.Phase != storageIntentRejected || w.Core.AdmissionEpoch != e.Views[origin.View].Epoch {
				return false
			}
			for _, slot := range status.Slots {
				if storageIntentMatchingTicket(host.record, slot.Ticket, e.QBinding) {
					return false
				}
			}
			return true
		case storageIntentRetired:
			if w.Core.Phase != storageIntentRetired || host.record.Token.Slot >= 64 {
				return false
			}
			original := e.Views[origin.View].Slots[host.record.Token.Slot]
			current := status.Slots[host.record.Token.Slot]
			a, b := original.PersistedApplied, current.PersistedApplied
			if b.Sequence < a.Sequence || current.PersistedNextSequence < original.PersistedNextSequence || current.PersistedNextSequence <= b.Sequence {
				return false
			}
			if b.Sequence == a.Sequence {
				return b == a
			}
			return b.ID != (storagebudget.ID{}) && b.Generation > 0
		}
		return false
	}
	return false
}

func storageEnrollmentPeakPrefixCovered(e storageEnrollmentPeakEvidence, slot int, pages [2][]byte, current storagebudget.HostState) bool {
	if slot < 0 || slot >= 64 || e.Phase != 2 {
		return false
	}
	var selected *storageEnrollmentPeakWitness
	count := 0
	for i := range e.Witnesses {
		w := &e.Witnesses[i]
		if int(w.Slot) != slot {
			continue
		}
		if !bytes.Equal(w.Page, pages[w.Bank]) {
			return false
		}
		count++
		if w.Selected {
			selected = w
		}
	}
	if count != 2 || selected == nil {
		return false
	}
	return storageEnrollmentPeakCoversHost(e, storageIntentSlot{record: selected.Core, page: selected.Page, bank: int(selected.Bank)}, current)
}
