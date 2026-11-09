package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// These are detached comparison vectors, not startup/native authority. Real
// startup exclusion and strict decoding are exercised by the move journeys.
func TestStorageMoveControlOriginalImages(t *testing.T) {
	for _, name := range []string{"exact-both", "changed-selected", "changed-peer", "swapped", "short-selected", "short-peer", "recovery-only", "invalid-selection", "missing"} {
		t.Run(name, func(t *testing.T) {
			actual := [2][]byte{bytes.Repeat([]byte{1}, storageOwnerCatalogBytes), bytes.Repeat([]byte{2}, storageOwnerCatalogBytes)}
			original := &storagePolicyOriginal{Selected: 1, Actual: [2][]byte{bytes.Clone(actual[0]), bytes.Clone(actual[1])}}
			switch name {
			case "changed-selected":
				actual[1][storageOwnerCatalogBytes-1] ^= 1
			case "changed-peer":
				actual[0][storageOwnerCatalogBytes-1] ^= 1
			case "swapped":
				actual[0], actual[1] = actual[1], actual[0]
			case "short-selected":
				actual[1] = actual[1][:storageOwnerCatalogBytes-1]
			case "short-peer":
				actual[0] = actual[0][:storageOwnerCatalogBytes-1]
			case "recovery-only":
				original.RecoveryOnly = true
			case "invalid-selection":
				original.Selected = 2
			case "missing":
				original = nil
			}
			if got := storageControlOriginalImages(original, actual); got != (name == "exact-both") {
				t.Fatal("two physical images must remain exact", got)
			}
		})
	}
}

// A hand-authored, retired J5 and selected latest-move row. No Q mutation or
// native operation creates these operands; their full codecs validate below.
func storageMoveControlReceiptVector(t *testing.T) (storageIdentityIntent, []byte, storagePolicyOriginal, storagebudget.HostState) {
	t.Helper()
	request, j, lineage := moveRequestVector(t)
	_, after, err := request.operands(lineage, nil)
	storageTestMust(t, err)
	j.Core.Generation = 9
	j.Core.PreviousPageDigest = storagebudget.Digest{44}
	j.Core.Phase, j.Core.Outcome = storageIntentRetired, storageIntentPublished
	j.Core.PolicyRevision, j.Core.RetirementEpoch = 1, 7
	j.Core.PreviousToken = storagebudget.Token{Slot: 1, ID: storagebudget.ID{45}, Sequence: 2, Generation: 2}
	j.Core.Token = j.Core.PreviousToken
	j.Core.Token.Generation++
	j.Core.StageExists = true
	j.NativePhase, j.Presence, j.Facts = storageIdentityTargetDurable, 27, 32767
	j.AllocatedStage = j.ExpectedOld
	j.AllocatedStage.Object++
	j.Target = j.AllocatedStage
	j.AllocatedLinks, j.LinkedTargetLinks, j.FinalTargetLinks = 1, 1, 1
	j.Core.Changes, err = storageIdentitySettlementChanges(j)
	storageTestMust(t, err)
	j.Core.Evidence, err = storageOwnerDigest("desk-storage-identity-action-publication-v2", storageIdentityPublicationMaterial(j))
	storageTestMust(t, err)
	storageTestMust(t, j.validate())
	page, err := storageEncodeIdentityIntent(j, request.Installation, request.Binding)
	storageTestMust(t, err)
	digest, err := storageIdentityIntentDigest(page, int(j.Core.Slot), request.Installation, request.Binding, j)
	storageTestMust(t, err)
	receipt := storageOwnerV5Receipt{OperationID: j.Core.OperationID, Terminal: j.Core.Token, PersistedApplied: j.Core.Token, PersistedNextSequence: 3, RetirementEpoch: 7, IntentDigest: digest, TicketDigest: storagebudget.Digest{46}, PublishedObject: j.Target, PublishedBytesDigest: sha256.Sum256(after), PublicationEvidence: j.Core.Evidence}
	row := storageMoveCurrent{PlanID: request.Plan, PlanDigest: request.PlanDigest, CompletionDigest: request.CompletionDigest, Current: &storageMovePublication{RequestDigest: j.Core.CASBinding, Receipt: receipt, Body: after}}
	if !row.valid() {
		t.Fatal("handwritten latest move row is invalid")
	}
	original := storagePolicyOriginal{Lineage: lineage, Moves: []storageMoveCurrent{row}}
	status := storagebudget.HostState{Epoch: 7}
	status.Slots[j.Core.Token.Slot].PersistedApplied = j.Core.Token
	status.Slots[j.Core.Token.Slot].PersistedNextSequence = 3
	return j, page, original, status
}

func TestStorageMoveControlOriginalReceipt(t *testing.T) {
	for _, name := range []string{"exact-retired", "wrong-slot", "wrong-current-move", "pending-current-move", "wrong-current-native", "foreign-plan", "changed-j-page", "recovery-only", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			j, page, original, status := storageMoveControlReceiptVector(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch name {
			case "wrong-slot":
				status.Slots[j.Core.Token.Slot] = storagebudget.HostSlotState{}
			case "wrong-current-move":
				original.Moves[0].Current.RequestDigest[0] ^= 1
			case "pending-current-move":
				original.Moves[0].PendingIntent = storagebudget.Digest{49}
			case "wrong-current-native":
				original.Moves[0].Current.Receipt.PublishedObject.Object++
			case "foreign-plan":
				original.Moves[0].PlanDigest[0] ^= 1
			case "changed-j-page":
				page[len(page)-1] ^= 1
			case "recovery-only":
				original.RecoveryOnly = true
			case "cancelled":
				cancel()
			}
			// This private comparison uses no owner IO. The caller must have
			// already validated authority and exact actual images; a receipt
			// predicate itself never creates either authority or a permit.
			var owner storageBudgetOwner
			got := owner.identityReceiptCurrentOriginal(ctx, j, page, original.Lineage, status, &original)
			if got != (name == "exact-retired") {
				t.Fatal("original per-slot/current-move receipt binding", got)
			}
		})
	}
}
