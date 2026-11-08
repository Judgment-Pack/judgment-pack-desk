package desk

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Actual original host custody is necessary: a detached tuple cannot prove
// that the SH launch's registered epoch may read its original J/Q snapshot.
func TestStorageMoveRegisteredControlOriginal(t *testing.T) {
	requireStorageLine(t)
	project := t.TempDir()
	storageTestMust(t, os.Mkdir(filepath.Join(project, startupIdentityDir), 0700))
	privateFixture(t, filepath.Join(project, startupIdentityDir, startupIdentityName), identityLine(strings.Repeat("f", 64), project, ""))
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: standInRuntime(t), Token: testToken, Logger: log.New(&bytes.Buffer{}, "", 0), DeskConfigDir: t.TempDir()})
	t.Cleanup(func() {
		ts.Close()
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal("initial history", code)
	}
	owner := s.storageConstruction.owner
	before, err := owner.ref.cell.q.InspectAction(t.Context())
	storageTestMust(t, err)
	var original *storageMoveOperation
	calls := 0
	err = s.withDialectArtifactProjectMoveHostOwned(t.Context(), func(_ *os.File, _ *dialectArtifactSelectedProviderPins, lease *storageCreatorLease) (err error) {
		if lease == nil || lease.move == nil {
			return storagebudget.ErrAuthority
		}
		m := lease.move
		original = m
		lock, err := m.privateDataLock(t.Context(), s)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, lock.Close()) }()
		if err = m.borrowPrivate(t.Context(), lock); err != nil {
			return err
		}
		defer func() { err = errors.Join(err, m.closePrivate()) }()
		inspect := func(ctx context.Context, table *storageIntentTable, state *storagePolicyOriginal) error {
			calls++
			status, e := owner.ref.cell.q.InspectHostState(ctx)
			if e != nil {
				return e
			}
			return owner.validateIntentMappingsOriginal(ctx, table, status, state)
		}
		if err = owner.withRegisteredMoveControl(t.Context(), m, inspect); err != nil {
			return err
		}
		if calls != 1 {
			t.Fatal("original registered control callback count", calls)
		}
		if e := owner.withControlOriginal(t.Context(), nil, m, func(context.Context, *storageIntentTable) error {
			calls++
			return nil
		}); e != storagebudget.ErrAuthority || calls != 1 {
			t.Fatal("registered control accepted missing original images", e, calls)
		}
		if e := owner.withStartupMoveControl(t.Context(), inspect); e != storagebudget.ErrAuthority || calls != 1 {
			t.Fatal("finished startup authority accepted for SH epoch", e, calls)
		}
		if e := owner.withRegisteredMoveControl(t.Context(), nil, inspect); e != storagebudget.ErrAuthority || calls != 1 {
			t.Fatal("missing original move accepted", e, calls)
		}
		copied := *m
		copied.self = &copied
		if e := owner.withRegisteredMoveControl(t.Context(), &copied, inspect); e != storagebudget.ErrAuthority || calls != 1 {
			t.Fatal("unregistered move copy accepted", e, calls)
		}
		foreign := &storageBudgetOwner{ref: owner.ref}
		foreign.self = foreign
		if e := foreign.withRegisteredMoveControl(t.Context(), m, inspect); e != storagebudget.ErrAuthority || calls != 1 {
			t.Fatal("same installation foreign owner accepted", e, calls)
		}
		e := func() error {
			saved := m.lease
			defer func() { m.lease = saved }()
			m.lease = &storageCreatorLease{}
			return owner.withRegisteredMoveControl(t.Context(), m, inspect)
		}()
		if e != storagebudget.ErrAuthority || calls != 1 {
			t.Fatal("wrong original lease accepted", e, calls)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if e = owner.withRegisteredMoveControl(ctx, m, inspect); !errors.Is(e, context.Canceled) || calls != 1 {
			t.Fatal("cancelled registered comparison entered callback", e, calls)
		}
		refused := errors.New("read-only original comparison refused")
		if e = owner.withRegisteredMoveControl(t.Context(), m, func(context.Context, *storageIntentTable, *storagePolicyOriginal) error {
			calls++
			return refused
		}); !errors.Is(e, refused) || calls != 2 {
			t.Fatal("callback refusal lost", e, calls)
		}
		return nil
	})
	storageTestMust(t, err)
	if original == nil || !owner.admissionOpen() {
		t.Fatal("original epoch did not drain")
	}
	if e := owner.withRegisteredMoveControl(t.Context(), original, func(context.Context, *storageIntentTable, *storagePolicyOriginal) error {
		calls++
		return nil
	}); e != storagebudget.ErrAuthority || calls != 2 {
		t.Fatal("finished epoch reused original comparison", e, calls)
	}
	after, err := owner.ref.cell.q.InspectAction(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("read-only comparison repeated Q effects")
	}
}
