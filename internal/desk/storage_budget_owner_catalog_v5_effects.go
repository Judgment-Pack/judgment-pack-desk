package desk

import "github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"

// Four detached PROPOSED operands. The accepted15 remain the exact v4 table;
// its from-* rows still publish F and are never relabeled as S. This function
// neither registers an allowance nor binds a parent, issues a permit or writes.
func storageOwnerV5ProposedStampEffects() [4]storageOwnerV4CatalogEffect {
	return [4]storageOwnerV4CatalogEffect{
		{"m1-proposed/stamp-anchor/stamp-prepared/publish", "publish", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"m1-proposed/stamp-anchor/stamp-prepared/persist", "persist", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"m1-proposed/stamp-anchor/stamp-active/publish", "publish", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"m1-proposed/stamp-anchor/stamp-active/persist", "persist", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
	}
}

// Detached availability comparison only. Shared duplication keys are the same
// accepted keys for every P/C/S/F recovery and any earlier duplicate this launch.
// Never mint a fresh v5/S key, clear spent on failure, or mutate the caller ledger.
// A future issuer must consume each key BEFORE its first effect, independently
// validate original custody, combine all c.spent, and deny either spent key.
func storageOwnerV5DuplicateOperands(spent map[string]bool) ([2]string, error) {
	keys := [2]string{"lin4/bridge-duplicate/publish", "lin4/bridge-duplicate/persist"}
	for _, key := range keys {
		if spent[key] {
			return [2]string{}, storagebudget.ErrBounds
		}
	}
	return keys, nil
}
