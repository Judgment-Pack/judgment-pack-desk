package desk

// These are finite catalog-only operands, not an issuer or a second allowance
// universe. Every group is max once per launch, including failed effects. The
// original config parent and combined existing c.spent/M1/M3 ledger must be
// validated before activation; this table supplies no custody or write grant.
const storageOwnerV4OriginalConfigParent uint8 = 1

type storageOwnerV4CatalogEffect struct {
	Key                                 string
	Action                              string
	Bytes, Files, Directories, Metadata uint64
	PolicyBytes, PolicyInodes           uint64
	Parent                              uint8
	MaxPerLaunch                        uint8
}

func storageOwnerV4CatalogEffects() [15]storageOwnerV4CatalogEffect {
	return [15]storageOwnerV4CatalogEffect{
		{"lin4/plan-prepared/publish", "publish", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"lin4/plan-prepared/persist", "persist", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"lin4/plan-active/publish", "publish", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"lin4/plan-active/persist", "persist", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"lin4/completion-prepared/publish", "publish", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"lin4/completion-prepared/persist", "persist", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"lin4/completion-active/publish", "publish", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"lin4/completion-active/persist", "persist", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"lin4/from-prepared/publish", "publish", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"lin4/from-prepared/persist", "persist", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"lin4/from-active/publish", "publish", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"lin4/from-active/persist", "persist", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		// The duplicate pair is shared by exact bridge Prepared duplication and
		// independently validated Active-peer reconstitution. Max once TOTAL;
		// never reencode, advance sequence/history, or reset a failed key.
		{"lin4/bridge-duplicate/publish", "publish", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		{"lin4/bridge-duplicate/persist", "persist", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
		// Also shared with ordinary selected-base reopening, never repeated after
		// the selected survivor's completion has consumed this key.
		{"lin4/reopen-base/persist", "persist", 8388608, 0, 0, 4, 17301504, 2, storageOwnerV4OriginalConfigParent, 1},
	}
}
