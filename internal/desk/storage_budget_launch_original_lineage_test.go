package desk

import (
	"bytes"
	"crypto/sha256"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Actual physical/source vectors with independent P/C and E1 operands. This is
// pure byte/lineage correspondence evidence, not native execution authority.
func TestStorageIdentityLaunchOriginalNativeLineage(t *testing.T) {
	for _, name := range []string{"bank0-exact", "bank0-selected-changed", "bank0-peer-changed", "bank0-recovery-only", "bank1-exact", "bank1-selected-changed", "bank1-peer-changed", "bank1-recovery-only"} {
		t.Run(name, func(t *testing.T) {
			bank := byte(0)
			if name[4] == '1' {
				bank = 1
			}
			actual, catalog, f, _ := moveNativeCompleteVector(t, bank)
			pair, err := storageOwnerV6NativeReadPair(actual[0], actual[1], &catalog, f.raw, f.record, f.claim)
			storageTestMust(t, err)
			if pair.RecoveryOnly || pair.Selected != bank {
				t.Fatal("fixture is not the independently completed original pair")
			}
			state := storagePolicyOriginal{Actual: [2][]byte{bytes.Clone(actual[0]), bytes.Clone(actual[1])}, Selected: pair.Selected, Catalog: catalog, Lineage: pair.Banks[pair.Selected].Stage.Lineage, Native: true}
			state.Evidence.Raw = f.raw
			before := [2]storagebudget.Digest{sha256.Sum256(state.Actual[0]), sha256.Sum256(state.Actual[1])}
			switch name[6:] {
			case "selected-changed":
				actual[bank] = bytes.Clone(actual[bank])
				actual[bank][100] ^= 1
			case "peer-changed":
				actual[1-bank] = bytes.Clone(actual[1-bank])
				actual[1-bank][100] ^= 1
			case "recovery-only":
				// A completed pair cannot be relabeled a prepared continuation.
				state.RecoveryOnly = true
			}
			current, durable, err := storageIdentityLaunchOriginalLineages(actual, state, f.record, f.claim)
			if name[6:] == "exact" {
				storageTestMust(t, err)
				if !reflect.DeepEqual(current, state.Lineage) || !reflect.DeepEqual(durable, state.Lineage) || len(current.Rows.LineagePlans) != 1 || len(current.Rows.LineageCompletions) != 1 {
					t.Fatal("lost immutable P/C needed by original receipt mapping")
				}
			} else if err == nil {
				t.Fatal("foreign current/peer or unprepared recovery state accepted")
			}
			if before != ([2]storagebudget.Digest{sha256.Sum256(state.Actual[0]), sha256.Sum256(state.Actual[1])}) {
				t.Fatal("comparison edited retained original images")
			}
		})
	}
}
