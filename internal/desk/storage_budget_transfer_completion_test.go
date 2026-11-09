package desk

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Handwritten pointer predicates exercise no effect/qualification member.
// The actual same-owner completion journey is a separate qualified test below.
func TestStorageTransferSelectionPointerStateRefusals(t *testing.T) {
	old := []byte(`{"version":1,"path":"/before"}`)
	next := []byte(`{"version":1,"path":"/after","previous":"/before"}`)
	foreign := []byte(`{"version":1,"path":"/foreign"}`)
	for _, tc := range []struct {
		name     string
		actual   []byte
		pointer  byte
		owned    bool
		ordinary bool
		want     error
	}{
		{"old-before-attempt", old, 0, true, false, nil},
		{"new-before-attempt", next, 0, true, false, errStorageEnrollmentUnresolved},
		{"new-after-confirmed", next, 2, true, false, nil},
		{"old-after-confirmed", old, 2, true, false, errStorageEnrollmentUnresolved},
		{"foreign-after-confirmed", foreign, 2, true, false, errStorageEnrollmentUnresolved},
		{"other-operation-new", next, 2, false, false, errStorageOwnerTransitionRequired},
		{"ordinary-unchanged", old, 0, false, true, nil},
		{"ordinary-changed", next, 0, false, true, errStorageOwnerTransitionRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir()
			storageTestMust(t, os.WriteFile(filepath.Join(path, dataLocationName), tc.actual, 0600))
			root, err := os.OpenRoot(path)
			storageTestMust(t, err)
			t.Cleanup(func() { storageTestMust(t, root.Close()) })
			other, err := os.OpenRoot(t.TempDir())
			storageTestMust(t, err)
			t.Cleanup(func() { storageTestMust(t, other.Close()) })
			selection := storageOriginalHistorySelection{settings: root, path: path, location: dataLocation{Version: 1, Path: "/before"}, explicit: true}
			if tc.ordinary {
				c := &storageBudgetConstruction{sources: &storageOriginalSources{selections: []storageOriginalHistorySelection{selection}}}
				c.self = c
				source := &storageOriginalSourceRoot{root: root, role: 1, path: path}
				err = source.current(t.Context(), c)
			} else {
				op := &chatTransferOperation{settings: other, record: storageTransferRecord{OldPointer: old, NewPointer: next, PointerState: tc.pointer}}
				if tc.owned {
					op.settings = root
				}
				err = op.originalTransferSelectionCurrent(t.Context(), selection)
			}
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatal("pointer predicate changed its closed old/new or ordinary selection boundary", err)
			}
			got, err := os.ReadFile(filepath.Join(path, dataLocationName))
			if err != nil || !reflect.DeepEqual(got, tc.actual) {
				t.Fatal("selection observation changed original bytes", err)
			}
		})
	}
}

func TestStorageTransferQualifiedCompletionSelectionCurrent(t *testing.T) {
	requireStorageLine(t)
	records, err := storageEnrollmentNativeRecords(storageEnrollmentInstalledNativeRecords)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Skip("requires actual installed Record3 matching this exact corrected source; no qualification is fabricated")
	}
	s := ownerActualNew(t, ownerActualConfig(t))
	server := httptest.NewServer(s)
	defer server.Close()
	before := storageTransferNativeSelection(t, s, server)
	request := moveChatData{Path: filepath.Join(t.TempDir(), "external", "nested"), Revision: before.Revision}
	r := httptest.NewRequest("POST", server.URL+"/api/storage/move", nil)
	pageBearer(privateWorkspaceTestSession(t, server))(r)
	storageTestMust(t, s.originalTransferRequestPath(request))
	storageTestMust(t, s.ensureDefaultHistoryEnrollment(r.Context()))
	p, err := s.prepareOriginalChatTransfer(r, storageTransferMove)
	storageTestMust(t, err)
	p.targetPath = request.Path
	var original *chatTransferOperation
	err = s.withOriginalChatTransfer(p, func(op *chatTransferOperation) error {
		original = op
		if err := op.captureOriginalTransferSource(p.ctx); err != nil {
			return err
		}
		plan, err := planChatData(p.ctx, op.source)
		if err != nil {
			return err
		}
		if err = op.planOriginalTransferWorkload(p.ctx, request, plan); err != nil {
			return err
		}
		if err = op.enrollOriginalTransferDestination(p.ctx); err != nil {
			return err
		}
		if err = op.reserveWhole(p.ctx); err != nil {
			return err
		}
		settings, selected, err := p.epoch.c.originalHistorySettings(p.ctx, s)
		if err != nil {
			return err
		}
		retained := *selected
		if settings.root != op.settings || settings.source == nil || settings.source.scope != nil || settings.source.parent != nil {
			return errors.New("fixture requires the exact original plain settings source")
		}
		if err = op.writeOriginalTransferTarget(p.ctx); err != nil {
			return err
		}
		if op.returned.PointerState != 2 || op.record.InitialToken == (storagebudget.Token{}) {
			return errors.New("fixture did not actually publish on its one original reservation")
		}
		q := p.registry.owner.ref.cell.q
		beforeReads, err := q.InspectHostState(p.ctx)
		if err != nil {
			return err
		}
		token, nextOrdinal := op.record.InitialToken, op.next
		results := append([]byte(nil), op.results...)
		roots := append([]*storageRootBinding(nil), p.epoch.originalRoots...)
		// The source dictionary deliberately remains old until final P installs.
		actual, err := readPrivateData(op.settings, dataLocationName, storageTransferPointerBytes)
		if err != nil || !reflect.DeepEqual(actual, op.record.NewPointer) {
			return errors.Join(errors.New("actual publication differs from original new pointer"), err)
		}
		location, err := storageTransferResolvedLocation(actual, retained.path)
		if err != nil || location == retained.location {
			return errors.Join(errors.New("fixture did not retain the pre-publication selection"), err)
		}
		if err = settings.source.current(p.ctx, p.epoch.c); err != nil {
			return errors.Join(errors.New("original source rejected its own published pointer before completion"), err)
		}
		if err = settings.current(p.ctx); err != nil {
			return errors.Join(errors.New("ordinary root census entry rejected original publication"), err)
		}
		// Equal native directory through a different description is not this
		// operation's settings selection and must retain the old strict refusal.
		alias, err := op.settings.OpenRoot(".")
		if err != nil {
			return err
		}
		other := retained
		other.settings = alias
		refusal := op.originalTransferSelectionCurrent(p.ctx, other)
		closeErr := alias.Close()
		if !errors.Is(refusal, errStorageOwnerTransitionRequired) || closeErr != nil {
			return errors.Join(errors.New("different settings description borrowed this operation's transition"), refusal, closeErr)
		}
		// A live original operation must not skip the ordinary restricted-project
		// source's scope.Project join. This detached read predicate grants nothing.
		project := p.epoch.originalProjectRoot()
		native, err := storageOwnerPersistentRoot(p.ctx, project)
		if err != nil {
			return err
		}
		scope := storageEnrollmentPeakSource{Namespace: []byte(".desk-private"), Project: native}
		projectSource := storageOriginalSourceRoot{root: project, role: 7, relative: ".desk-private", persistent: native, scope: &scope}
		if err = projectSource.current(p.ctx, p.epoch.c); err != nil {
			return errors.Join(errors.New("valid original restricted-project read refused"), err)
		}
		scope.Project.Object++
		if err = projectSource.current(p.ctx, p.epoch.c); !errors.Is(err, errStorageCustody) {
			return errors.Join(errors.New("active transfer bypassed corrupt restricted-project scope"), err)
		}
		afterReads, err := q.InspectHostState(p.ctx)
		if err != nil || !reflect.DeepEqual(beforeReads, afterReads) || op.record.InitialToken != token || op.next != nextOrdinal || !reflect.DeepEqual(results, op.results) || !reflect.DeepEqual(roots, p.epoch.originalRoots) || !reflect.DeepEqual(retained, *selected) {
			return errors.Join(errors.New("source read checks changed original Q, token, ordinal, roots or retained selection"), err)
		}
		return nil // Original lexical completion alone performs census/settlement/P.
	})
	storageTestMust(t, err)
	storageTransferNativeFinal(t, s, original, true)
	if got := storageTransferNativeSelection(t, s, server); got.Path != request.Path {
		t.Fatal("completed original selection was not exposed")
	}
}

// These are actual one-ticket transfers. Only the existing synchronous
// successor seam pauses the owner; it grants no effect or qualification.
func TestStorageTransferQualifiedCompletionCallerLifecycle(t *testing.T) {
	requireStorageLine(t)
	records, err := storageEnrollmentNativeRecords(storageEnrollmentInstalledNativeRecords)
	storageTestMust(t, err)
	if len(records) == 0 {
		t.Skip("requires actual installed Record3 matching this exact lifecycle source; no qualification is fabricated")
	}
	for _, tc := range []struct {
		name        string
		serverClose bool
	}{
		{name: "caller-cancel-during-successor"},
		{name: "server-close-during-successor", serverClose: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := New(ownerActualConfig(t))
			storageTestMust(t, err)
			server := httptest.NewServer(s)
			defer server.Close()
			defer func() { _ = s.Close() }()
			before := storageTransferNativeSelection(t, s, server)
			request := moveChatData{Path: filepath.Join(t.TempDir(), "external", "nested"), Revision: before.Revision}
			requestCtx, cancelRequest := context.WithCancel(t.Context())
			defer cancelRequest()
			if deadline, ok := t.Deadline(); ok {
				var cancelDeadline context.CancelFunc
				requestCtx, cancelDeadline = context.WithDeadline(requestCtx, deadline)
				defer cancelDeadline()
			}
			r := httptest.NewRequest("POST", server.URL+"/api/storage/move", nil).WithContext(requestCtx)
			pageBearer(privateWorkspaceTestSession(t, server))(r)
			storageTestMust(t, s.originalTransferRequestPath(request))
			storageTestMust(t, s.ensureDefaultHistoryEnrollment(r.Context()))
			p, err := s.prepareOriginalChatTransfer(r, storageTransferMove)
			storageTestMust(t, err)
			p.targetPath = request.Path
			entered, release := make(chan struct{}), make(chan struct{})
			fault := &storageEnrollmentFault{point: "successor-entered", entered: entered, release: release}
			construction := s.storageConstruction
			previousFault := construction.enrollmentFault
			done, closed := make(chan struct{}), make(chan error, 1)
			result := make(chan error, 1)
			released, closeStarted, closeJoined := false, false, false
			releaseOwner := func() {
				if !released {
					close(release)
					released = true
				}
			}
			// Never restore the fixture seam or close its originals over a live
			// owner, including on an assertion failure. Runner bounds remain.
			defer func() {
				cancelRequest()
				releaseOwner()
				<-done
				construction.enrollmentFault = previousFault
				if closeStarted && !closeJoined {
					<-closed
				}
			}()
			var original *chatTransferOperation
			var heldFiles []*os.File
			var returnedParent *os.Root
			go func() {
				defer close(done)
				result <- s.withOriginalChatTransfer(p, func(op *chatTransferOperation) error {
					original = op
					if err := op.captureOriginalTransferSource(p.ctx); err != nil {
						return err
					}
					plan, err := planChatData(p.ctx, op.source)
					if err != nil {
						return err
					}
					if err = op.planOriginalTransferWorkload(p.ctx, request, plan); err != nil {
						return err
					}
					if err = op.enrollOriginalTransferDestination(p.ctx); err != nil {
						return err
					}
					if err = op.reserveWhole(p.ctx); err != nil {
						return err
					}
					if err = op.writeOriginalTransferTarget(p.ctx); err != nil {
						return err
					}
					if op.returned.PointerState != 2 || op.path == nil || op.private == nil || p.epoch.project == nil || len(op.created) == 0 {
						return errors.New("actual lifecycle fixture lacks published pointer and original owned descriptions")
					}
					heldFiles = []*os.File{op.private.file, p.epoch.project, op.path.domainOwner}
					returnedParent = op.created[0].root
					// Destination enrollment is already complete. This seam can
					// therefore pause only the original completion's successor.
					construction.enrollmentFault = fault
					return nil
				})
			}()
			select {
			case <-entered:
			case <-done:
				t.Fatal("original completion returned before its successor boundary", <-result)
			case <-requestCtx.Done():
				t.Fatal("original caller ended before the successor boundary", requestCtx.Err())
			}
			op := storageTransferNativeCurrent(t, s)
			if op != original || !op.completing || op.completion.finished || op.completionContext != p.ctx || p.ctx.Err() != nil || !op.tailConsumed || p.completed {
				t.Fatal("normal completion detached from its live original owner/caller or discharged Tail early")
			}
			deadline, hasDeadline := op.completionContext.Deadline()
			callerDeadline, callerHasDeadline := requestCtx.Deadline()
			if hasDeadline != callerHasDeadline || hasDeadline && !deadline.Equal(callerDeadline) {
				t.Fatal("normal completion replaced the original caller deadline")
			}
			qPath := filepath.Join(s.configDir, storageAnchorName)
			qBefore := storageTransferQFiles(t, qPath)
			token, tail, cursor, ordinal := op.record.InitialToken, op.record.Tail, p.epoch.planCursor, op.next
			controls, completionContext := op.controlConsumed, op.completionContext
			for _, file := range heldFiles {
				if file == nil {
					t.Fatal("fixture failed to retain an original descriptor")
				}
				if _, err = file.Stat(); err != nil {
					t.Fatal("original descriptor closed before completion returned", err)
				}
			}
			if tc.serverClose {
				closeStarted = true
				go func() { closed <- s.Close() }()
				select {
				case <-p.ctx.Done():
				case err := <-closed:
					closeJoined = true
					t.Fatal("Server.Close returned before the paused owner drained", err)
				case <-requestCtx.Done():
					t.Fatal("raw caller ended before Server.Close revoked the preparation", requestCtx.Err())
				}
				if requestCtx.Err() != nil {
					t.Fatal("Server.Close changed the raw original caller context")
				}
			} else {
				cancelRequest()
				<-p.ctx.Done()
			}
			if !errors.Is(completionContext.Err(), context.Canceled) {
				t.Fatal("live completion did not observe original caller/Close cancellation", completionContext.Err())
			}
			select {
			case err := <-closed:
				closeJoined = true
				t.Fatal("Close released originals over the paused completion", err)
			default:
			}
			releaseOwner()
			<-done
			finalErr := <-result
			if !errors.Is(finalErr, context.Canceled) || !errors.Is(finalErr, errStorageEnrollmentUnresolved) {
				t.Fatal("cancelled normal completion lost its original error/debt", finalErr)
			}
			if op.completionContext != completionContext || !op.completion.finished || !op.finished || !p.finished || p.operation != nil || p.completed || op.tailCompleted || p.epoch.reconciled || len(chatTransferProcessPermit) != 0 || p.epoch.running || p.epoch.entered || p.epoch.use != nil || p.epoch.project != nil || p.epoch.work != nil || len(p.epoch.holds) != 0 {
				t.Error("cancelled owner retried, discharged unresolved Tail or failed mandatory drain")
			}
			if op.record.InitialToken != token || !reflect.DeepEqual(op.record.Tail, tail) || op.next != ordinal || p.epoch.planCursor != cursor || op.controlConsumed != controls || !reflect.DeepEqual(qBefore, storageTransferQFiles(t, qPath)) {
				t.Error("cancellation dispatched another group/Q effect or changed the original token/Tail")
			}
			if op.appliedProof == nil || !storageTransferApplied(op.record, op.catalog, *op.appliedProof) || p.registry.owner.admissionOpen() {
				t.Error("original applied transfer/debt was lost or incomplete successor reopened admission")
			}
			for _, file := range heldFiles {
				if _, err = file.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Error("mandatory original descriptor Close did not finish", err)
				}
			}
			if _, err = returnedParent.Stat("."); !errors.Is(err, os.ErrClosed) {
				t.Error("original returned parent outlived completion drain", err)
			}
			if err = op.completeObserved(nil); !errors.Is(err, storagebudget.ErrAuthority) || !reflect.DeepEqual(qBefore, storageTransferQFiles(t, qPath)) {
				t.Error("finished original completion accepted a fallback/replay", err)
			}
			if closeStarted {
				finalErr = <-closed
				closeJoined = true
			} else {
				finalErr = s.Close()
			}
			if !errors.Is(finalErr, context.Canceled) || !errors.Is(finalErr, errStorageEnrollmentUnresolved) {
				t.Error("Close discarded the original cancelled completion result", finalErr)
			}
			if repeated := s.Close(); !reflect.DeepEqual(repeated, finalErr) {
				t.Error("repeated Close changed the original joined result", repeated, finalErr)
			}
		})
	}
}
