package desk

import (
	"sync"
	"time"
)

// These targets select existing writers only. They are not configuration or a
// public test endpoint. A nil hook is the production path. Test hooks are fixed
// before serving; mutable callback state must be synchronized by the test.
type storagePersistenceTarget struct {
	operation string
	name      string
}

const (
	storagePersistenceWorkspace      = "workspace-record"
	storagePersistenceResearchCreate = "research-create"
	storagePersistenceResearchUpdate = "research-update"
)

// The original enrollment installs one immutable gate before serving. No
// descriptor is retained: only the volatile scope and slots have a lifetime here.
// Gate retirement precedes server draining and enrollment release; owner-slot
// retirement prevents late children from updating an already closed owner.
type storageRuntimePressureScope struct {
	mu           sync.Mutex
	closed       bool
	server       *Server
	enrollment   *storageServerEnrollment
	owner        *storageBudgetOwner
	construction *storageBudgetConstruction
}

type runtimeStoragePressureReporter struct {
	scope      *storageRuntimePressureScope
	server     *Server
	enrollment *storageServerEnrollment
}

func (s *Server) runtimeStoragePressureReporter() runtimeStoragePressureReporter {
	if e := s.storageEnrollment; e != nil {
		return runtimeStoragePressureReporter{scope: e.runtimePressure, server: s, enrollment: e}
	}
	return runtimeStoragePressureReporter{}
}
func (r runtimeStoragePressureReporter) retire() {
	if r.scope == nil || r.enrollment == nil {
		return
	}
	r.scope.mu.Lock()
	if r.scope.server == r.server && r.scope.enrollment == r.enrollment && r.enrollment.self == r.enrollment {
		r.scope.closed = true
	}
	r.scope.mu.Unlock()
}

// This may run under a caller's admission/worker lease. It acquires only the
// dedicated volatile gate and fixed slot mutexes: no Server/enrollment/owner/Q
// mutex, OS lock, currentness check, retain, persistence, notification or logging.
// It grants no write/read/retry/recovery authority. Pointer/seal checks use only
// immutable startup bindings; mutable closure state lives exclusively in gate
// and slot mutexes. Replaced/copied enrollments cannot redirect an old reporter.
func (r runtimeStoragePressureReporter) report(err error, uncertain bool) {
	g := r.scope
	if err == nil || g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	s, e, o := g.server, g.enrollment, g.owner
	if g.closed || s != r.server || e != r.enrollment || s == nil || e == nil || o == nil || s.storageEnrollment != e || e.self != e || e.server != s || e.runtimePressure != g || e.construction == nil || o.self != o {
		return
	}
	c := e.construction
	if c != g.construction || c.self != c || c.owner != o || c.server != s.deskOwner() || !e.ref.valid() || !c.ref.valid() || e.ref.cell != c.ref.cell || s.configDir != c.server.configDir {
		return
	}
	code := storageStatusReason(err)
	if uncertain {
		code = storageStatusUncertain
	}
	o.statusPressure.recordRuntime(code, time.Now(), &s.storageStatusPressure)
}
