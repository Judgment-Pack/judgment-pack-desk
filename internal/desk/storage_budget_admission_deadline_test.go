package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Actual positive Q results plus an already-expired internal context model the
// observed deadline crossing deterministically. No sleep, retry, test hook,
// fabricated qualified record or application effect supplies this coverage.
func TestStorageAdmissionCompletedQDeadlineRecording(t *testing.T) {
	for _, point := range []string{"reserve", "advance"} {
		for _, callerCancelled := range []bool{false, true} {
			name := point + "-internal-expiry"
			if callerCancelled {
				name = point + "-caller-cancellation"
			}
			t.Run(name, func(t *testing.T) {
				o, root, config := storageWriterFixture(t, true)
				plan := storageWriterPlan(root)
				base := storageTestMutationContext(t, o, t.Context())
				parent, cancelParent := context.WithCancel(base)
				defer cancelParent()
				components, err := o.writerComponents(parent, plan, root.root)
				storageTestMust(t, err)
				observations, err := o.writerObservations(parent, components)
				storageTestMust(t, err)
				var original storageIntentRecord
				var completed storagebudget.Token
				err = o.withControl(parent, func(coordination context.Context, table *storageIntentTable) error {
					status, e := o.ref.cell.q.InspectHostState(coordination)
					if e != nil {
						return e
					}
					original = storageIntentRecord{Slot: 0, Generation: 1, Phase: storageIntentPrepared, OperationID: storagebudget.ID{91}, CASBinding: storagebudget.Digest{92}, AdmissionEpoch: status.Epoch, NewLength: uint64(len(plan.Bytes)), Target: plan.Target, Stage: plan.Stage, Workflow: plan.Workflow, Operation: plan.Operation, Components: components, Changes: make([]storagebudget.Change, len(components))}
					for _, component := range components {
						r := o.roots[component.Root]
						original.RootIdentities = append(original.RootIdentities, r.row.Identity)
						original.FilesystemIdentities = append(original.FilesystemIdentities, o.filesystems[component.Filesystem].Identity)
						original.Locators = append(original.Locators, r.row.Locator)
					}
					if e = table.save(coordination, original); e != nil {
						return e
					}
					request, e := o.originalIntentRequest(original)
					if e != nil {
						return e
					}
					completed, e = o.ref.cell.q.Reserve(coordination, request, observations)
					if e != nil {
						return e
					}
					if point == "advance" {
						if e = o.recordSingleFileReserved(parent, coordination, table, original, completed); e != nil {
							return e
						}
						completed, e = o.ref.cell.q.Advance(coordination, completed, o.ref.cell.binding, storagebudget.Staging, original.CASBinding)
						if e != nil {
							return e
						}
					}
					// Expiry is forced only AFTER the successful original Q return. This is
					// not a retry or a new reservation after a cancelled original operation.
					expired, cancelExpired := context.WithDeadline(parent, time.Unix(1, 0))
					defer cancelExpired()
					if !errors.Is(expired.Err(), context.DeadlineExceeded) {
						return errors.New("expired coordination fixture not expired")
					}
					if callerCancelled {
						cancelParent()
					}
					if point == "reserve" {
						return o.recordSingleFileReserved(parent, expired, table, original, completed)
					}
					reserved := table.slots[0].record.Token
					_, e = o.recordSingleFileStaging(parent, expired, table, original, reserved, completed)
					return e
				})
				wanted := error(context.DeadlineExceeded)
				if callerCancelled {
					wanted = context.Canceled
				}
				if !errors.Is(err, wanted) {
					t.Fatalf("completed result hid caller/coordination expiry: %v", err)
				}
				records := storageWriterRecords(t, o)
				if len(records) != 1 {
					t.Fatal("original record lost")
				}
				expected := storageIntentReserved
				qstate := storagebudget.Reserved
				if point == "advance" {
					expected = storageIntentStaging
					qstate = storagebudget.Staging
				}
				if callerCancelled {
					if point == "reserve" {
						expected = storageIntentPrepared
					} else {
						expected = storageIntentReserved
					}
				}
				if records[0].Phase != expected {
					t.Fatal("wrong original completed/cancelled metadata phase", records[0].Phase, expected)
				}
				status, e := o.ref.cell.q.InspectHostState(t.Context())
				storageTestMust(t, e)
				ticket := status.Slots[completed.Slot].Ticket
				if ticket.Token != completed || ticket.State != qstate {
					t.Fatal("positive original Q result changed, advanced again or forgiven")
				}
				for _, path := range []string{plan.Target, plan.Stage} {
					if _, e = os.Stat(filepath.Join(config, path)); !errors.Is(e, os.ErrNotExist) {
						t.Fatal("deadline recording issued application effect")
					}
				}
				// Neither recorded completion nor original caller cancellation reopens
				// admission. No permit, replacement ticket or compensation may follow.
				tree := storageTestTree(t, config)
				// This is an otherwise-valid new caller under the same original root and
				// plan. The existing ExactNewReplaceAndZero positive fixture exercises
				// this ordinary authority route without retained debt.
				freshCaller := storageTestMutationContext(t, o, t.Context())
				if permit, e := o.admitSingleFile(freshCaller, plan); !errors.Is(e, errStorageOwnerRecovery) || permit != nil {
					t.Fatalf("expected retained original debt refusal, got permit=%t error=%v", permit != nil, e)
				}
				after, e := o.ref.cell.q.InspectHostState(t.Context())
				storageTestMust(t, e)
				if !reflect.DeepEqual(status, after) || !reflect.DeepEqual(tree, storageTestTree(t, config)) {
					t.Fatal("retry replayed/rewrote original debt")
				}
			})
		}
	}
}
