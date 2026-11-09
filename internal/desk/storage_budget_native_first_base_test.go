package desk

import (
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// storageNativeWithFirstBase re-encodes a native prepared/active pair with the
// given founding commitment on each bank, chaining the active bank's Previous to
// the re-encoded prepared bank as the original publication does.
func storageNativeWithFirstBase(t *testing.T, pair [2][]byte, selected byte, prepared, active storagebudget.Digest, record storageOwnerRecordV2, claim storageOwnerClaimV2) ([2][]byte, error) {
	t.Helper()
	low, err := storageOwnerV6NativeDecode(pair[1-selected], record, claim)
	storageTestMust(t, err)
	high, err := storageOwnerV6NativeDecode(pair[selected], record, claim)
	storageTestMust(t, err)
	var out [2][]byte
	low.Stage.FirstBase, low.Stage.Raw = prepared, nil
	if out[1-selected], err = low.encode(record, claim); err != nil {
		return out, err
	}
	high.Stage.FirstBase, high.Stage.Raw = active, nil
	if high.Stage.Previous, err = storageCompositionChecksumOf(out[1-selected]); err != nil {
		return out, err
	}
	out[selected], err = high.encode(record, claim)
	return out, err
}

// **A native bank keeps the first base its lineage was founded on.** First-use
// history enrollment founds its composition on an already completed E1 pair,
// and every later lineage bank carries that founding commitment in its frozen
// core. A move writes the first native bank from such a lineage, so the native
// codec accepts the commitment on a later transition, round-trips it exactly,
// and pairs only banks that carry the same one. An enrollment stage founds its
// own lineage and still carries none.
func TestStorageNativeStageKeepsItsFirstBase(t *testing.T) {
	for _, selected := range []byte{0, 1} {
		actual, catalog, f, _ := policyNativeFixture(t, selected)
		next := policyNativePlan(t)
		published, err := storageOwnerV6NativeAppend(actual, selected, f.raw, catalog, next, storageCompositionPlan, next.Rows.LineagePlans[0].PlanID, f.record, f.claim)
		storageTestMust(t, err)
		founded := storagebudget.Digest{0x5a, 1}
		rooted, err := storageNativeWithFirstBase(t, published, selected, founded, founded, f.record, f.claim)
		if err != nil {
			t.Fatalf("bank%d: a lineage founded on a first base cannot be written natively: %v", selected, err)
		}
		pair, err := storageOwnerV6NativeReadPair(rooted[0], rooted[1], nil, nil, f.record, f.claim)
		if err != nil || pair.RecoveryOnly || pair.Selected != selected {
			t.Fatalf("bank%d: the founded native pair was not selected: %v", selected, err)
		}
		for i := range pair.Banks {
			if pair.Banks[i].Stage.FirstBase != founded {
				t.Fatalf("bank%d: bank %d lost its first base", selected, i)
			}
		}
		changed, err := storageNativeWithFirstBase(t, published, selected, founded, storagebudget.Digest{0x5a, 2}, f.record, f.claim)
		storageTestMust(t, err)
		if _, err = storageOwnerV6NativeReadPair(changed[0], changed[1], nil, nil, f.record, f.claim); err == nil {
			t.Fatalf("bank%d: a successor that changed its first base was paired", selected)
		}
		dropped, err := storageNativeWithFirstBase(t, published, selected, founded, storagebudget.Digest{}, f.record, f.claim)
		storageTestMust(t, err)
		if _, err = storageOwnerV6NativeReadPair(dropped[0], dropped[1], nil, nil, f.record, f.claim); err == nil {
			t.Fatalf("bank%d: a successor that dropped its first base was paired", selected)
		}
	}
}
