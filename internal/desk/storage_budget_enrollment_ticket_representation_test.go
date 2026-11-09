package desk

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageEnrollmentPeakTicketCanonicalEmptySlices(t *testing.T) {
	// The independent handwritten body encodes counts, not Go allocation shape.
	original := storagebudget.Ticket{Token: storagebudget.Token{Slot: 63}}
	wire := epTicketBody(original)
	for _, tc := range []struct {
		name   string
		ticket storagebudget.Ticket
	}{
		{"nil", original},
		{"empty", storagebudget.Ticket{Token: original.Token, Components: []storagebudget.Component{}, Changes: []storagebudget.Change{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := storageEnrollmentPeakWriteTicket(tc.ticket)
			if err != nil || !bytes.Equal(encoded, wire) {
				t.Fatal("zero-count wire representation changed", err)
			}
			decoded, err := storageEnrollmentPeakReadTicket(encoded)
			if err != nil || !reflect.DeepEqual(decoded, original) || decoded.Fanout != nil {
				t.Fatal("zero counts did not reconstruct canonical original Q ticket", err)
			}
		})
	}
}

func TestStorageEnrollmentPeakRetainedOriginTicketRebase(t *testing.T) {
	// The existing independent retired fixture contains a nonempty terminal
	// ticket and all64 exact persisted slots, plus both original host peers.
	previous, core := epRetired(t, false, false)
	_, _, _, record, claim := epCatalogs(t)
	current := previous
	current.Views = []storageEnrollmentPeakView{previous.Views[0]}
	// The legacy fixture authors identical checkpoint banks but decodes only
	// the selected bank. Rebase writes the peer-aware version2 checkpoint, so
	// supply its original opposite bank as the actual constructor does.
	current.Checkpoint.PeerCheckpoint = append([]byte{}, previous.Checkpoint.Checkpoint...)
	// Model public Q cloneTicket's representation before dictionary interning:
	// it copies every nonempty element and uses nil for each zero-length slice.
	for i := range current.Views[0].Slots {
		ticket := &current.Views[0].Slots[i].Ticket
		ticket.Components = append([]storagebudget.Component(nil), ticket.Components...)
		ticket.Changes = append([]storagebudget.Change(nil), ticket.Changes...)
	}
	epoch := &storageEnrollmentEpoch{
		c:                &storageBudgetConstruction{ref: &storageInstallationRef{cell: &storageInstallation{ownerRecord: &record, ownerClaim: claim}}},
		previousEvidence: &previous,
	}
	if err := epoch.rebaseEnrollmentOrigins(&current); err != nil {
		t.Fatal("actual pure origin rebase failed", err)
	}
	if !storageEnrollmentPeakRetainsOrigins(current, previous) {
		t.Fatal("rebasing the same original history changed retained facts")
	}
	origin := current.Board.Origins[current.Witnesses[0].Origin]
	retained := current.Views[origin.View]
	original := previous.Views[previous.Board.Origins[0].View]
	if !reflect.DeepEqual(retained.Slots, original.Slots) {
		t.Fatal("rebase changed one or more of the complete64 original slots")
	}
	ticketBytes, err := storageEnrollmentPeakWriteTickets(current.Tickets)
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := storageEnrollmentPeakReadTickets(ticketBytes, uint32(len(current.Tickets)))
	if err != nil {
		t.Fatal(err)
	}
	viewBytes, err := storageEnrollmentPeakWriteViews(current.Views, current.Inner.Catalog, current.Tickets)
	if err != nil {
		t.Fatal(err)
	}
	views, err := storageEnrollmentPeakReadViews(viewBytes, uint32(len(current.Views)), current.Inner.Catalog, tickets)
	if err != nil {
		t.Fatal(err)
	}
	for i := range views {
		if !reflect.DeepEqual(views[i].Slots, current.Views[i].Slots) {
			t.Fatal("dictionary/view roundtrip changed exact slot contents", i)
		}
	}
	for _, field := range []string{"component", "change"} {
		t.Run(field, func(t *testing.T) {
			bad := current
			bad.Views = append([]storageEnrollmentPeakView(nil), current.Views...)
			ticket := &bad.Views[origin.View].Slots[core.Token.Slot].Ticket
			ticket.Components = append([]storagebudget.Component(nil), ticket.Components...)
			ticket.Changes = append([]storagebudget.Change(nil), ticket.Changes...)
			if len(ticket.Components) == 0 || len(ticket.Changes) == 0 {
				t.Fatal("fixture lost its nonempty original ticket")
			}
			if field == "component" {
				ticket.Components[0].Physical.Bytes++
			} else {
				ticket.Changes[0].PhysicalAdd.Bytes++
			}
			if _, err := storageEnrollmentPeakWriteTicket(*ticket); err != nil {
				t.Fatal("mutation must remain a valid typed ticket", err)
			}
			if storageEnrollmentPeakRetainsOrigins(bad, previous) {
				t.Fatal("retained history accepted changed nonempty ticket content", field)
			}
		})
	}
}
