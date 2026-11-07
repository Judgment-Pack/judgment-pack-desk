package desk

import (
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// **A storage limit edited after history enrollment does not make the next
// start read only.** The chat's first save enrolls the project's history,
// which retains a creation record in the storage intent table. Each edit,
// smaller or larger, moves Q to a new epoch and changes the workspace
// definitions; the restart must recognize the retained record and the
// definitions through the completed policy, and open writable with the edited
// limits in force. A second start, after another edit, does the same.
func TestStoragePolicyEditAfterHistoryEnrollmentRestartsWritable(t *testing.T) {
	storagePolicyHostRequireRecord(t)
	for _, change := range []struct {
		name   string
		deltas []int64
	}{{"smaller", []int64{-1}}, {"larger", []int64{1}}, {"twice", []int64{-1, 2}}} {
		t.Run(change.name, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
				t.Fatal(code)
			}
			saved := savedConversationBytes(t, s, ts)
			for i, delta := range change.deltas {
				storageTestMust(t, s.prepareStoragePolicy(t.Context()))
				first, err := s.readStoragePolicySnapshot(t.Context())
				storageTestMust(t, err)
				limits := first.Policy.Workspace
				limits.Total.Bytes = uint64(int64(limits.Total.Bytes) + delta)
				edited, err := s.updateStoragePolicy(t.Context(), storagePolicyEdit{Expected: first, Default: &limits})
				storageTestMust(t, err)
				s, ts, _ = startedAt(t, s, s.projectDir)
				if s.storageReadOnly {
					t.Fatalf("start %d after a %s limit was read only: %v", i+1, change.name, s.storageProblem)
				}
				got, err := s.readStoragePolicySnapshot(t.Context())
				storageTestMust(t, err)
				if got.Policy != edited.Policy {
					t.Fatal("the restart lost the edited limit")
				}
				if code, reply := chatRequest(t, ts, "GET", "", "", true, ""); code != 200 || string(reply.Content) != saved {
					t.Fatalf("lost history %d %s", code, reply.Content)
				}
			}
			// The recognition is the completed policy's alone: the endpoint it
			// was built on, at the epoch it covered, with Q at the policy's
			// boundary. Another endpoint or a later epoch explains nothing.
			o := s.storageConstruction.owner
			endpoint := s.storageConstruction.ref.cell.enrollmentEndpoint
			if endpoint == nil {
				t.Fatal("no enrollment endpoint after restart")
			}
			status, err := s.storageConstruction.ref.cell.q.InspectHostState(t.Context())
			storageTestMust(t, err)
			epoch, ok := o.policyExplainedCoverageEpoch(t.Context(), *endpoint, status, nil)
			if !ok || epoch != endpoint.CoveredEpoch || epoch >= status.Epoch {
				t.Fatalf("the completed policy did not explain the epoch advance: %d %v", epoch, ok)
			}
			other := *endpoint
			other.Raw = append([]byte{}, endpoint.Raw...)
			other.Raw[len(other.Raw)-1] ^= 1
			if _, ok := o.policyExplainedCoverageEpoch(t.Context(), other, status, nil); ok {
				t.Fatal("another endpoint's coverage was carried across the policy")
			}
			later := status
			later.Epoch++
			if _, ok := o.policyExplainedCoverageEpoch(t.Context(), *endpoint, later, nil); ok {
				t.Fatal("an epoch past the policy's boundary was explained")
			}
			if !o.policyCompletedDefinitions(t.Context(), mustInspectHost(t, s)) {
				t.Fatal("the completed policy's definitions were not recognized")
			}
		})
	}
}

func mustInspectHost(t *testing.T, s *Server) storagebudget.HostInspection {
	t.Helper()
	v, err := s.storageConstruction.ref.cell.q.InspectHost(t.Context())
	storageTestMust(t, err)
	return v
}
