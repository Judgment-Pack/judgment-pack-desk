package storagebudget

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Expectations are authored from the protocol: no epoch advance, no surviving
// snapshot grant, strict peer refusal, and a new invocation on complete restart.
func TestExclusiveCensusSameEpochAndFreshRestart(t *testing.T) {
	q, path := fixture(t)
	before, err := q.InspectHost(t.Context())
	must(t, err)
	s, v, err := q.CaptureExclusiveCensus(t.Context())
	must(t, err)
	if !reflect.DeepEqual(before, v) {
		t.Fatal("capture did not expose its exact persisted table")
	}
	must(t, q.PublishExclusiveCensus(t.Context(), s, fixtureCensus()))
	after, err := q.InspectHost(t.Context())
	must(t, err)
	if after.Configuration.Epoch != 1 || after.Configuration.Maintenance || after.Configuration.Revision != 4 || !reflect.DeepEqual(before.Slots, after.Slots) || !reflect.DeepEqual(before.Configuration.Policy, after.Configuration.Policy) {
		t.Fatal("census changed epoch, policy or exact tickets")
	}
	if err = q.PublishExclusiveCensus(t.Context(), s, fixtureCensus()); !errors.Is(err, ErrAuthority) {
		t.Fatal("snapshot reused", err)
	}
	reopened, root, witness := openFixture(t, path)
	defer root.Close()
	defer witness.Close()
	defer reopened.Close()
	if err = reopened.PublishExclusiveCensus(t.Context(), s, fixtureCensus()); !errors.Is(err, ErrAuthority) {
		t.Fatal("lost live proof adopted by new coordinator", err)
	}
	next, _, err := reopened.CaptureExclusiveCensus(t.Context())
	must(t, err)
	must(t, reopened.PublishExclusiveCensus(t.Context(), next, fixtureCensus()))
	again, err := reopened.InspectHost(t.Context())
	must(t, err)
	if again.Configuration.Epoch != 1 || again.Configuration.Revision != 5 || again.Configuration.Maintenance {
		t.Fatal("healthy subsequent restart self-invalidated")
	}
}

func TestExclusiveCensusReserveWithoutRevisionAndNoReplay(t *testing.T) {
	q, path := fixture(t)
	s, before, err := q.CaptureExclusiveCensus(t.Context())
	must(t, err)
	token, err := q.Reserve(t.Context(), request(3), observations())
	must(t, err)
	reserved, err := q.InspectHost(t.Context())
	must(t, err)
	if reserved.Configuration.Revision != before.Configuration.Revision {
		t.Fatal("fixture no longer demonstrates Reserve without checkpoint revision")
	}
	hashes := configurationTestHashes(t, path)
	if err = q.PublishExclusiveCensus(t.Context(), s, fixtureCensus()); err == nil {
		t.Fatal("same revision concealed changed ticket")
	}
	if !reflect.DeepEqual(hashes, configurationTestHashes(t, path)) {
		t.Fatal("refused publication wrote storage")
	}
	token, err = q.Settle(t.Context(), SettlementFact{Token: token, Owner: owner, Evidence: digest(51), Changes: []Change{{PhysicalAdd: Vector{3, 1, 0}, AttributionAdd: Vector{3, 1, 0}}}})
	must(t, err)
	hashes = configurationTestHashes(t, path)
	if _, _, err = q.CaptureExclusiveCensus(t.Context()); !errors.Is(err, ErrBusy) {
		t.Fatal("unincorporated terminal replay accepted", err)
	}
	if !reflect.DeepEqual(hashes, configurationTestHashes(t, path)) {
		t.Fatal("capture checkpointed terminal replay")
	}
	must(t, q.Checkpoint(t.Context()))
	_, retired, err := q.CaptureExclusiveCensus(t.Context())
	must(t, err)
	if retired.Slots[token.Slot].PersistedApplied != token || retired.Slots[token.Slot].PersistedNextSequence != 2 {
		t.Fatal("exact retired tuple lost")
	}
}

func TestExclusiveCensusUnknownMaintenanceAndCancellation(t *testing.T) {
	q, path := fixture(t)
	s, v, err := q.CaptureExclusiveCensus(t.Context())
	must(t, err)
	_, err = q.BeginMaintenance(t.Context(), v.Configuration.Revision)
	must(t, err)
	hashes := configurationTestHashes(t, path)
	if err = q.PublishExclusiveCensus(t.Context(), s, fixtureCensus()); !errors.Is(err, ErrStale) {
		t.Fatal("later maintenance accepted", err)
	}
	if _, _, err = q.CaptureExclusiveCensus(t.Context()); !errors.Is(err, ErrStale) {
		t.Fatal("unknown maintenance cleared", err)
	}
	if !reflect.DeepEqual(hashes, configurationTestHashes(t, path)) {
		t.Fatal("refusal changed maintenance")
	}
	fresh, _ := fixture(t)
	snap, _, err := fresh.CaptureExclusiveCensus(t.Context())
	must(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = fresh.PublishExclusiveCensus(ctx, snap, fixtureCensus()); err == nil {
		t.Fatal("canceled publication accepted")
	}
}

func TestExclusiveCensusPublicationCuts(t *testing.T) {
	// Initial fixture selected checkpoint.a at generation3. One census writes b4.
	for _, prior := range []bool{false, true} {
		for _, row := range []struct {
			name, phase    string
			short, healthy bool
		}{
			{"torn-first-page", "write", true, false},
			{"complete-sync-error", "sync", false, true},
			{"complete-close-error", "close", false, true},
			{"complete-readback-error", "readback", false, true},
		} {
			label := "first-"
			if prior {
				label = "second-"
			}
			t.Run(label+row.name, func(t *testing.T) {
				q, path := fixture(t)
				if prior {
					initial, _, err := q.CaptureExclusiveCensus(t.Context())
					must(t, err)
					must(t, q.PublishExclusiveCensus(t.Context(), initial, fixtureCensus()))
				}
				s, _, err := q.CaptureExclusiveCensus(t.Context())
				must(t, err)
				target := "checkpoint.b"
				published := uint64(4)
				if prior {
					target = "checkpoint.a"
					published = 5
				}
				q.fault = &ioFault{Name: target, Phase: row.phase, Offset: 0, Short: row.short}
				if err = q.PublishExclusiveCensus(t.Context(), s, fixtureCensus()); err == nil {
					t.Fatal("fault was not reached")
				}
				reopened, root, witness := openFixture(t, path)
				defer root.Close()
				defer witness.Close()
				defer reopened.Close()
				next, v, err := reopened.CaptureExclusiveCensus(t.Context())
				if !row.healthy {
					if err == nil {
						t.Fatal("torn peer fell back")
					}
					return
				}
				must(t, err)
				if v.Configuration.Epoch != 1 || v.Configuration.Revision != published || v.Configuration.Maintenance {
					t.Fatal("complete but failed result lost durable publication")
				}
				must(t, reopened.PublishExclusiveCensus(t.Context(), next, fixtureCensus()))
				final, err := reopened.InspectHost(t.Context())
				must(t, err)
				if final.Configuration.Revision != published+1 || final.Configuration.Epoch != 1 {
					t.Fatal("subsequent original restart failed")
				}
			})
		}
	}
}

func TestExclusiveCensusStrictCarrierAndIrrelevantBank(t *testing.T) {
	for _, name := range []string{"checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
		t.Run("legacy-"+name, func(t *testing.T) {
			q, path := fixture(t)
			f, err := os.OpenFile(filepath.Join(path, name), os.O_RDWR, 0)
			must(t, err)
			_, err = f.WriteAt([]byte("JPSTB001"), 0)
			must(t, err)
			must(t, f.Close())
			if _, _, err = q.CaptureExclusiveCensus(t.Context()); !errors.Is(err, ErrUnsupported) {
				t.Fatal("recognized prior carrier accepted", err)
			}
		})
	}
	q, path := fixture(t)
	s, _, err := q.CaptureExclusiveCensus(t.Context())
	must(t, err)
	selected, err := os.ReadFile(filepath.Join(path, "checkpoint.a"))
	must(t, err)
	// Identical valid peer replication changes bank selection but no selected fact.
	must(t, os.WriteFile(filepath.Join(path, "checkpoint.b"), selected, 0600))
	must(t, q.PublishExclusiveCensus(t.Context(), s, fixtureCensus()))
}
