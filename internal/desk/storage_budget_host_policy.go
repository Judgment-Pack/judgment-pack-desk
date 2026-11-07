package desk

import (
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// These finite defaults are authored by the host. Neither retained bytes,
// observations nor a portable catalog can increase them. Ordinary reopen uses
// the exact original policy; changing it requires Q's revision-checked update.
func storageHostInitialPolicy() storagebudget.Policy {
	installation := storagebudget.Limits{Total: storagebudget.Vector{Bytes: 24 << 30, Files: 262144, Directories: 65536}}
	workspace := storagebudget.Limits{Total: storagebudget.Vector{Bytes: 4 << 30, Files: 65536, Directories: 16384}}
	ib := [storagebudget.Categories]uint64{6 << 30, 12 << 30, 6 << 30, 256 << 20, 512 << 20, 256 << 20, 8 << 30, 4 << 30, 1 << 30}
	wb := [storagebudget.Categories]uint64{1 << 30, 2 << 30, 1 << 30, 32 << 20, 256 << 20, 64 << 20, 4 << 30, 2 << 30, 4 << 30}
	for i := range ib {
		installation.Category[i] = storagebudget.Vector{Bytes: ib[i], Files: installation.Total.Files, Directories: installation.Total.Directories}
		workspace.Category[i] = storagebudget.Vector{Bytes: wb[i], Files: workspace.Total.Files, Directories: workspace.Total.Directories}
	}
	// Binaries remain installation attributed. The workspace Binaries ceiling
	// only completes the finite schema. Settlement is an aggregate category;
	// the separate per-operation 128MiB ceiling remains mandatory.
	return storagebudget.Policy{Installation: installation, Workspace: workspace, ObservationMaxAge: time.Duration(storageAdvisoryInitialMaxAgeNS)}
}
