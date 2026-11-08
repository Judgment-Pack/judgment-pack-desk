package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageStatusTestID(n uint64) string { return fmt.Sprintf("%032x", n) }
func storageStatusTestLimits() (storageStatusNumbers, storageStatusCategories) {
	total := storageStatusNumbers{1000, 1000, 1000}
	var categories storageStatusCategories
	for i := range categories {
		categories[i] = storageStatusNumbers{900, 900, 900}
	}
	return total, categories
}
func storageStatusTestProjection() storageStatusProjection {
	total, limits := storageStatusTestLimits()
	global := storageStatusCachedScope{ID: storageStatusTestID(1), Known: true, Total: total, Limits: limits}
	global.Usage[0] = storageStatusNumbers{400, 4, 2}
	global.Peak[0] = storageStatusNumbers{30, 3, 1}
	global.Settlement[0] = storageStatusNumbers{20, 2, 1}
	workspace := global
	workspace.ID = storageStatusTestID(2)
	workspace.Generation = 1
	workspace.Usage[0] = storageStatusNumbers{800, 8, 4} // attribution is larger than installation charge
	second := workspace
	second.ID = storageStatusTestID(3)
	second.Usage[0] = storageStatusNumbers{600, 6, 3}
	return storageStatusProjection{Installation: global.ID, Revision: ^uint64(0), Epoch: ^uint64(0), Global: global, Workspaces: []storageStatusCachedScope{workspace, second},
		Roots:       []storageStatusCachedRoot{{ID: storageStatusTestID(4), Filesystem: storageStatusTestID(5), Generation: 1, Known: true, Usage: storageStatusNumbers{400, 4, 2}, Peak: storageStatusNumbers{30, 3, 1}, Settlement: storageStatusNumbers{20, 2, 1}}},
		Filesystems: []storageStatusCachedFilesystem{{ID: storageStatusTestID(5), Generation: 1, Constraints: []string{storageStatusTestID(6)}}},
		Constraints: []storageStatusCachedConstraint{{ID: storageStatusTestID(6), Generation: ^uint64(0), Resources: 3, Headroom: [2]uint64{100, 10}, Peak: [2]uint64{50, 5}}}, Active: 3, Uncertain: 2, Terminal: 1}
}
func storageStatusTestPublic(t *testing.T, p storageStatusProjection, workspace string) *storageStatusSnapshot {
	t.Helper()
	out, err := p.public(workspace, storageStatusTestID(99), ^uint64(0), time.Date(2026, 10, 31, 1, 2, 3, 4, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStorageStatusDecimalUnknownAndIntersections(t *testing.T) {
	p := storageStatusTestProjection()
	out := storageStatusTestPublic(t, p, p.Workspaces[0].ID)
	if out.Installation.ChargedLogical.Bytes != "400" || out.CurrentWorkspace.ChargedLogical.Bytes != "800" {
		t.Fatal("summed shared attribution into installation")
	}
	if out.Installation.LogicalRemaining.Bytes != "550" || out.CurrentWorkspace.LogicalRemaining.Bytes != "150" || out.CurrentWorkspace.EffectiveLogicalRemaining.Bytes != "150" {
		t.Fatal("total residuals", out.Installation, out.CurrentWorkspace)
	}
	if out.CurrentWorkspaceCategories[0].LogicalRemaining.Bytes != "50" || out.CurrentWorkspaceCategories[0].EffectiveLogicalRemaining.Bytes != "50" {
		t.Fatal("category intersection", out.CurrentWorkspaceCategories)
	}
	if out.Roots[0].Limit != nil || out.Roots[0].LogicalRemaining != nil || out.Roots[0].OverBudget != nil {
		t.Fatal("invented root cap")
	}
	if out.PhysicalAllocated.Value != nil || out.RetainedLogical.Value != nil || out.Reclaimable.AllocatedEstimate != nil || out.Recovery.PendingRecoveryCount != nil || out.Measurement.At != nil || out.Constraints[0].Available != nil {
		t.Fatal("fabricated measurement")
	}
	if out.Filesystems[0].Completeness != "complete" || out.Filesystems[0].PhysicalAllocated != nil {
		t.Fatal("logical filesystem grouping", out.Filesystems)
	}
	p.Global.Usage[0][0] = 9007199254740993
	p.Global.Total[0] = 1 << 60
	p.Global.Limits[0][0] = 1 << 60
	p.Roots[0].Usage[0] = 9007199254740993
	out = storageStatusTestPublic(t, p, p.Workspaces[0].ID)
	encoded, err := storageStatusEncode(out, storageStatusBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, exact := range []string{`"bytes":"9007199254740993"`, `"policyRevision":"18446744073709551615"`, `"observationSequence":"18446744073709551615"`, `"generation":"18446744073709551615"`} {
		if !bytes.Contains(encoded, []byte(exact)) {
			t.Fatal("lost precision", exact)
		}
	}
	p.Global.Known = false
	p.Roots[0].Known = false
	p.Workspaces[0].Known = false
	p.Maintenance = true
	out = storageStatusTestPublic(t, p, p.Workspaces[0].ID)
	if out.Installation.ChargedLogical != nil || out.Installation.OverBudget != nil || out.Installation.LogicalRemaining != nil || out.CurrentWorkspace.EffectiveLogicalRemaining != nil || out.Categories[0].ChargedLogical != nil {
		t.Fatal("unknown became measured zero")
	}
	if out.Installation.PromisedPeak.Bytes != "30" || out.Measurement.Freshness != "maintenance" || out.Filesystems[0].Completeness != "unknown" {
		t.Fatal("unknown promises/maintenance")
	}
	// A known workspace cannot claim an effective residual while installation is unknown.
	if out.Workspaces[1].LogicalRemaining == nil || out.Workspaces[1].EffectiveLogicalRemaining != nil {
		t.Fatal("unknown installation intersection")
	}
	p = storageStatusTestProjection()
	p.Global.Usage[0][0] = 951
	out = storageStatusTestPublic(t, p, "")
	if !*out.Installation.OverBudget || out.Installation.LogicalRemaining.Bytes != "0" || !*out.Categories[0].OverBudget {
		t.Fatal("clamp hid over-budget")
	}
	p = storageStatusTestProjection()
	p.Global.Usage[0][0] = 950
	p.Global.Limits[0][0] = 1000
	out = storageStatusTestPublic(t, p, "")
	if *out.Installation.OverBudget || out.Installation.LogicalRemaining.Bytes != "0" {
		t.Fatal("exact boundary is not exceeded")
	}
}

func TestStorageStatusProjectionRefusesMalformedAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*storageStatusProjection)
	}{
		{"source-bytes", func(p *storageStatusProjection) { p.Global.Usage[0][0] = (1 << 60) + 1 }},
		{"source-files", func(p *storageStatusProjection) { p.Global.Usage[0][1] = (1 << 40) + 1 }},
		{"source-directories", func(p *storageStatusProjection) { p.Global.Limits[0][2] = (1 << 40) + 1 }},
		{"combined-promises", func(p *storageStatusProjection) { p.Global.Peak[0][0] = 1 << 60; p.Global.Settlement[0][0] = 1 }},
		{"sum-categories", func(p *storageStatusProjection) { p.Global.Usage[0][0] = 1 << 60; p.Global.Usage[1][0] = 1 }},
		{"used-plus-promises", func(p *storageStatusProjection) { p.Global.Usage[0][0] = 1 << 60 }},
		{"duplicate-root", func(p *storageStatusProjection) { p.Roots = append(p.Roots, p.Roots[0]) }},
		{"foreign-filesystem", func(p *storageStatusProjection) { p.Roots[0].Filesystem = storageStatusTestID(100) }},
		{"unknown-axis", func(p *storageStatusProjection) { p.Constraints[0].Resources = 4 }},
		{"undeclared-axis", func(p *storageStatusProjection) { p.Constraints[0].Resources = 1 }},
		{"capacity-max", func(p *storageStatusProjection) { p.Constraints[0].Peak[1] = (1 << 40) + 1 }},
		{"unsafe-id", func(p *storageStatusProjection) { p.Roots[0].ID = "/private/secret" }},
		{"unknown-as-known", func(p *storageStatusProjection) { p.Workspaces[0].Known = false }},
		{"ticket-bound", func(p *storageStatusProjection) { p.Active = 64; p.Uncertain = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := storageStatusTestProjection()
			tc.change(&p)
			if out, err := p.public("", "", 0, time.Time{}); err == nil || out != nil {
				t.Fatal("malformed projection escaped", out, err)
			}
		})
	}
	p := storageStatusTestProjection()
	if out, err := p.public(storageStatusTestID(200), "", 0, time.Time{}); !errors.Is(err, storagebudget.ErrAuthority) || out != nil {
		t.Fatal("foreign current workspace", out, err)
	}
}

func TestStorageStatusSingleEncodedCacheChildIsolationAndBounds(t *testing.T) {
	p := storageStatusTestProjection()
	// Full registered cardinalities, maximum-width IDs/generations/limits and
	// capacity demand. Each workspace may carry a full attributed charge; the
	// installation/root logical total remains independently authoritative.
	p.Global.Usage = storageStatusCategories{}
	p.Global.Peak = storageStatusCategories{}
	p.Global.Settlement = storageStatusCategories{}
	p.Global.Total = storageStatusNumbers{1 << 60, 1 << 40, 1 << 40}
	wide := storageStatusNumbers{(1 << 60) / 27, (1 << 40) / 27, (1 << 40) / 27}
	for i := range p.Global.Limits {
		p.Global.Limits[i] = p.Global.Total
		p.Global.Usage[i], p.Global.Peak[i], p.Global.Settlement[i] = wide, wide, wide
	}
	p.Roots = nil
	p.Workspaces = nil
	p.Filesystems = nil
	p.Constraints = nil
	for i := 0; i < 512; i++ {
		p.Constraints = append(p.Constraints, storageStatusCachedConstraint{ID: storageStatusTestID(uint64(10000 + i)), Generation: ^uint64(0), Resources: 3, Headroom: [2]uint64{1 << 60, 1 << 40}, Peak: [2]uint64{1 << 60, 1 << 40}})
	}
	for i := 0; i < 128; i++ {
		p.Filesystems = append(p.Filesystems, storageStatusCachedFilesystem{ID: storageStatusTestID(uint64(20000 + i)), Generation: ^uint64(0), Constraints: []string{p.Constraints[i*2].ID, p.Constraints[i*2+1].ID}})
	}
	rootWide := storageStatusNumbers{(1 << 60) / (3 * 4096), (1 << 40) / (3 * 4096), (1 << 40) / (3 * 4096)}
	for i := 0; i < 4096; i++ {
		p.Roots = append(p.Roots, storageStatusCachedRoot{ID: storageStatusTestID(uint64(30000 + i)), Filesystem: p.Filesystems[i%128].ID, Generation: ^uint64(0), Known: true, Usage: rootWide, Peak: rootWide, Settlement: rootWide})
	}
	for i := 0; i < 1024; i++ {
		row := p.Global
		row.ID = storageStatusTestID(uint64(40000 + i))
		row.Generation = ^uint64(0)
		// Every category carries wide counters; all 27 charge/promise rows
		// together remain below each source maximum.
		row.Usage[0][0] = 9007199254740993
		p.Workspaces = append(p.Workspaces, row)
	}
	encoded, err := storageStatusEncode(p, storageStatusBytes)
	if err != nil {
		t.Fatal("valid maximum private cache", err)
	}
	var decoded storageStatusProjection
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), encoded...)
	first := storageStatusTestPublic(t, decoded, p.Workspaces[0].ID)
	last := storageStatusTestPublic(t, decoded, p.Workspaces[1023].ID)
	if first.CurrentWorkspace.ID == last.CurrentWorkspace.ID || len(first.CurrentWorkspaceCategories) != 9 || len(first.Categories) != 9 {
		t.Fatal("child selected another workspace")
	}
	if first.CurrentWorkspaceCategories[0].ChargedLogical.Bytes != "9007199254740993" {
		t.Fatal("private numeric cache lost precision")
	}
	response := storageStatusResponse{Version: 1, Availability: "available", ServedAt: storageStatusTime(time.Now()), Pressure: storageStatusPressure{Codes: []string{}, DurableState: "unconfirmed"}, Snapshot: last}
	// All128 filesystem observations plus the fixed measurement envelope fit
	// the unchanged per-row1024/envelope262144 derived bound.
	measured := &storageOwnedMeasurement{Completed: time.Date(2026, 10, 31, 1, 2, 3, 0, time.UTC), Epoch: p.Epoch, LogicalKnown: true, AllocationKnown: true, SharedKnown: true, Roots: map[storagebudget.ID]storagebudget.RegisteredRoot{}, WorkspaceGenerations: map[storagebudget.ID]uint64{}, FilesystemGenerations: map[storagebudget.ID]uint64{}, Filesystems: map[storagebudget.ID]storageOwnedFilesystem{}}
	for _, row := range p.Roots {
		id, _ := storageStatusParseID(row.ID)
		fs, _ := storageStatusParseID(row.Filesystem)
		measured.Roots[id] = storagebudget.RegisteredRoot{ID: id, Generation: row.Generation, Filesystem: fs}
	}
	for _, row := range p.Workspaces {
		id, _ := storageStatusParseID(row.ID)
		measured.WorkspaceGenerations[id] = row.Generation
	}
	for _, row := range p.Filesystems {
		id, _ := storageStatusParseID(row.ID)
		measured.FilesystemGenerations[id] = row.Generation
		measured.Filesystems[id] = storageOwnedFilesystem{LogicalKnown: true, AllocationKnown: true}
	}
	measured.Logical = storagebudget.Vector{Bytes: ^uint64(0), Files: ^uint64(0), Directories: ^uint64(0)}
	measured.Allocation = measured.Logical
	measured.Shared = measured.Logical
	for id, row := range measured.Filesystems {
		row.Logical = measured.Logical
		row.Allocation = measured.Allocation
		measured.Filesystems[id] = row
	}
	p.applyOwnedMeasurement(last, measured, false, time.Now(), false, true)
	if last.Measurement.Logical.Value == nil || len(last.Filesystems) != 128 || last.Filesystems[127].PhysicalAllocated == nil {
		t.Fatal("maximum measurement fixture incomplete")
	}
	public, err := storageStatusEncode(response, storageStatusDerivedBytes)
	if err != nil || len(public) > 6160384 {
		t.Fatal("valid maximum public bound", len(public), err)
	}
	// Mutation of one decoded response never changes shared encoded data.
	first.CurrentWorkspaceCategories[0].ChargedLogical.Bytes = "0"
	if !bytes.Equal(before, encoded) || last.CurrentWorkspaceCategories[0].ChargedLogical.Bytes != "9007199254740993" {
		t.Fatal("response aliases shared cache")
	}
	t.Logf("private encoded bytes=%d; public encoded bytes=%d; decoding/projecting is per response, with no exact heap claim", len(encoded), len(public))
	p.Roots = append(p.Roots, p.Roots[0])
	if out, err := p.public("", "", 0, time.Time{}); out != nil || err == nil {
		t.Fatal("cardinality overflow")
	}
	if _, err := storageStatusEncode(strings.Repeat("x", storageStatusBytes), storageStatusBytes); !errors.Is(err, storagebudget.ErrBounds) {
		t.Fatal("encoder failed cap", err)
	}
	oversized := &storageStatusSnapshot{Roots: []storageStatusScope{{ID: strings.Repeat("x", 1024)}}}
	if !errors.Is(storageStatusCheckRows(oversized), storagebudget.ErrBounds) {
		t.Fatal("row cap not enforced")
	}
}

func TestStorageStatusCacheCoalescesCancellationFailureAndSequence(t *testing.T) {
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	c := &storageStatusCache{session: storageStatusTestID(1)}
	var wg sync.WaitGroup
	winners := make(chan bool, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, yes := c.begin(now); winners <- yes }()
	}
	wg.Wait()
	close(winners)
	count := 0
	for yes := range winners {
		if yes {
			count++
		}
	}
	if count != 1 {
		t.Fatal("multiple refreshes", count)
	}
	for i := 0; i < 20; i++ {
		if _, yes := c.begin(now.Add(time.Hour)); yes {
			t.Fatal("replacement while OS read remains in flight")
		}
	}
	view := c.finish([]byte(`{"safe":true}`), now, nil)
	if view.sequence != 1 || view.failed {
		t.Fatal("first publication")
	}
	if view.stale(now.Add(15*time.Second), false) || !view.stale(now.Add(15*time.Second+time.Nanosecond), false) || !view.stale(now, true) {
		t.Fatal("as-of/maintenance freshness boundary")
	}
	if !view.observedAt.Equal(now) {
		t.Fatal("serving cached data refreshed observedAt")
	}

	for _, delay := range []time.Duration{0, time.Second, 5*time.Second - time.Nanosecond} {
		if _, yes := c.begin(now.Add(delay)); yes {
			t.Fatal("manual/focus burst bypassed minimum", delay)
		}
	}
	if _, yes := c.begin(now.Add(5 * time.Second)); !yes {
		t.Fatal("boundary refresh denied")
	}
	view = c.finish(nil, now.Add(5*time.Second), context.Canceled)
	if !view.failed || !view.stale(now, false) || view.sequence != 1 || !view.observedAt.Equal(now) || string(view.encoded) != `{"safe":true}` {
		t.Fatal("cancelled refresh destroyed another caller's prior observation", view)
	}
	c.sequence = ^uint64(0)
	if _, yes := c.begin(now.Add(10 * time.Second)); !yes {
		t.Fatal("overflow fixture")
	}
	view = c.finish([]byte(`{"replacement":true}`), now.Add(10*time.Second), nil)
	if !view.failed || view.sequence != ^uint64(0) || string(view.encoded) != `{"safe":true}` {
		t.Fatal("sequence wrapped or published")
	}
	c.retire()
	if view, yes := c.begin(now.Add(time.Hour)); yes || len(view.encoded) != 0 {
		t.Fatal("owner end retained counters")
	}
}

func TestStorageStatusVolatilePressureNoPollOccurrences(t *testing.T) {
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	var slot storageStatusPressureSlot
	slot.record(storageStatusDiskFull, now)
	slot.record(storageStatusDiskFull, now.Add(time.Second))
	slot.record(storageStatusUncertain, now.Add(2*time.Second))
	before := slot.view()
	slot.occurrences = ^uint64(0)
	slot.record(storageStatusUnknown, now.Add(3*time.Second))
	slot.record("raw /private/credential error", now.Add(time.Hour))
	for i := 0; i < 1000; i++ {
		_ = slot.view()
	}
	after := slot.view()
	if len(after.Codes) != 3 || before.FirstSeenAt == nil || *before.FirstSeenAt != *after.FirstSeenAt || slot.occurrences != ^uint64(0) || *after.LastSeenAt != storageStatusTime(now.Add(3*time.Second)) {
		t.Fatal("slot unbounded, reset or poll-generated", after)
	}
	if storageStatusReason(fmt.Errorf("/secret: %w", syscall.ENOSPC)) != storageStatusDiskFull || storageStatusReason(storagebudget.ErrHeadroom) != storageStatusHeadroom || storageStatusReason(errors.New("inode full")) != storageStatusUnknown {
		t.Fatal("raw prose or headroom inferred an inode cause")
	}
}

func storageStatusTestRequest(t *testing.T, s *Server, method, path string, body *strings.Reader) (*httptest.ResponseRecorder, storageStatusResponse) {
	t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, body)
	}
	r.Header.Set("Authorization", "Bearer "+policyTestSession(t, s))
	w := httptest.NewRecorder()
	s.handleStorageStatus(w, r)
	var out storageStatusResponse
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return w, out
}
func TestStorageStatusGuardExactReadAndStartupFallback(t *testing.T) {
	sessions, err := newSessionStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sessions.close)
	s := &Server{cfg: Config{Token: testToken}, sessions: sessions, storageReadOnly: true}
	token := policyTestSession(t, s)
	s.recordStorageStatusProblem(fmt.Errorf("/private/path: %w", syscall.ENOSPC))
	for _, tc := range []struct {
		name, auth, origin string
		status             int
	}{{"no-session", "", "", 401}, {"foreign-origin", "Bearer " + token, "https://evil.test", 403}} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://desk.test/api/storage/status", nil)
			r.Header.Set("Authorization", tc.auth)
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			s.handleStorageStatus(w, r)
			if w.Code != tc.status || w.Header().Get(RefusalHeader) == "" {
				t.Fatal("guard", w.Code, w.Header())
			}
			if bytes.Contains(w.Body.Bytes(), []byte(storageStatusDiskFull)) {
				t.Fatal("pressure leaked before guard")
			}
		})
	}
	w, out := storageStatusTestRequest(t, s, "GET", "/api/storage/status", nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" || out.Snapshot != nil || out.Availability != "unavailable" || !out.ReadOnly || !out.Pressure.Active {
		t.Fatal("fallback", w.Code, out)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("/private/path")) {
		t.Fatal("raw error escaped")
	}
	for _, tc := range []struct {
		method, path string
		body         *strings.Reader
		status       int
	}{{"HEAD", "/api/storage/status", nil, 405}, {"POST", "/api/storage/status", nil, 405}, {"GET", "/api/storage/status?workspace=foreign", nil, 400}, {"GET", "/api/storage/status?", nil, 400}, {"GET", "/api/storage/status", strings.NewReader("x"), 400}, {"GET", "/api/storage/status/", nil, 405}, {"GET", "/api/storage/%73tatus", nil, 405}} {
		w, _ := storageStatusTestRequest(t, s, tc.method, tc.path, tc.body)
		if w.Code != tc.status || w.Header().Get(RefusalHeader) == "" {
			t.Fatal("invalid shape", tc.method, tc.path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/storage/status", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	if !s.serveStorageReadOnly(w, r) || w.Code != 200 {
		t.Fatal("exact diagnostic not allowed during startup refusal")
	}
	r = httptest.NewRequest("GET", "/api/storage", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	s.serveStorageReadOnly(w, r)
	if w.Code != 503 || bytes.Contains(w.Body.Bytes(), []byte(`"usage"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"projectBytes"`)) {
		t.Fatal("legacy location fallback fabricated data", w.Body.String())
	}
}

// These integration fixtures deliberately require actual supported New. They do
// not mint an enrollment or bypass the existing startup custody failure. Until
// that unrelated failure is repaired they are expected to stop at its evidence.
func TestStorageStatusActualReadNoWriteAndCopiedEnrollment(t *testing.T) {
	requireStorageLine(t)
	s := ownerActualNew(t, ownerActualConfig(t))
	beforeConfig := storageTestTree(t, s.configDir)
	beforeProject := storageTestTree(t, s.projectDir)
	w, out := storageStatusTestRequest(t, s, "GET", "/api/storage/status", nil)
	if w.Code != 200 || out.Snapshot == nil || out.Snapshot.CurrentWorkspace == nil {
		t.Fatal("actual status unavailable", w.Body.String())
	}
	again, next := storageStatusTestRequest(t, s, "GET", "/api/storage/status", nil)
	if again.Code != 200 || next.Snapshot.ObservationSequence != out.Snapshot.ObservationSequence || next.Snapshot.ObservedAt != out.Snapshot.ObservedAt {
		t.Fatal("cached read performed another observation")
	}
	if !reflect.DeepEqual(beforeConfig, storageTestTree(t, s.configDir)) || !reflect.DeepEqual(beforeProject, storageTestTree(t, s.projectDir)) {
		t.Fatal("diagnostic mutated persistent source bytes/entries")
	}
	original := s.storageEnrollment
	// Explicit fields avoid copying a mutex and intentionally retain the original
	// address seal. A serialized/copy wrapper cannot borrow original authority.
	copied := &storageServerEnrollment{self: original, server: s, construction: original.construction, ref: original.ref, workspace: original.workspace}
	s.storageEnrollment = copied
	_, refused := storageStatusTestRequest(t, s, "GET", "/api/storage/status", nil)
	s.storageEnrollment = original
	if refused.Snapshot != nil || refused.Availability != "unavailable" {
		t.Fatal("copied enrollment lent cache authority")
	}
}

func TestStorageStatusActualLifetimeDrainsAndRejectsClosing(t *testing.T) {
	requireStorageLine(t)
	s := ownerActualNew(t, ownerActualConfig(t))
	e, o, use, err := s.storageStatusLifetime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.storageConstruction.Close() }()
	<-o.closingStarted
	select {
	case err := <-done:
		t.Fatal("closed retained status lifetime", err)
	default:
	}
	if err := e.statusCurrent(t.Context(), s, o); err == nil {
		t.Fatal("closing owner permitted publication")
	}
	if err := use.Current(); err != nil {
		t.Fatal("live use invalidated before drain", err)
	}
	if err := use.Close(); err != nil {
		t.Fatal(err)
	}
	if err := o.drop(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.storageStatusLifetime(t.Context()); err == nil {
		t.Fatal("new read after close")
	}
}
func TestStorageStatusActualUseReplacementAndEXRefusal(t *testing.T) {
	requireStorageLine(t)
	s := ownerActualNew(t, ownerActualConfig(t))
	path := filepath.Join(s.configDir, storageUseName)
	gate, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = lockPrivateData(gate, true); err != nil {
		t.Fatal(err)
	}
	_, out := storageStatusTestRequest(t, s, "GET", "/api/storage/status", nil)
	if out.Snapshot != nil || out.Reason == nil || *out.Reason != storageStatusBusy {
		t.Fatal("borrowed startup/maintenance EX or hid busy", out)
	}
	if err = gate.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, path+".retained-fixture"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, out = storageStatusTestRequest(t, s, "GET", "/api/storage/status", nil)
	if out.Snapshot != nil || out.Availability != "unavailable" {
		t.Fatal("replacement lock adopted")
	}
}

func TestStorageStatusSanitizesInternalProjectionAndCapacity(t *testing.T) {
	id := func(n byte) (out storagebudget.ID) { out[15] = n; return }
	limits := storagebudget.Limits{Total: storagebudget.Vector{Bytes: 1000, Files: 1000, Directories: 1000}}
	for i := range limits.Category {
		limits.Category[i] = limits.Total
	}
	v := storagebudget.StatusSnapshot{Revision: ^uint64(0), Epoch: 3, Policy: storagebudget.Policy{Installation: limits, Workspace: limits, ObservationMaxAge: time.Minute}}
	v.Installation[0] = storagebudget.Vector{Bytes: 400, Files: 4}
	root := storagebudget.StatusRoot{Root: storagebudget.Root{ID: id(4), Filesystem: id(5), Generation: 1, Known: true, Locator: "secret original /private/workflow"}}
	root.Owner[0] = 77
	root.Identity[0] = 78
	root.Usage = v.Installation
	root.Promises.Peak[0] = storagebudget.Vector{Bytes: 30}
	root.Promises.Settlement[0] = storagebudget.Vector{Bytes: 20}
	v.Roots = []storagebudget.StatusRoot{root}
	v.Promises = root.Promises
	workspace := storagebudget.StatusWorkspace{Workspace: storagebudget.Workspace{ID: id(2), Generation: 1, Known: true, Limits: limits}, Promises: root.Promises}
	workspace.Usage[0] = storagebudget.Vector{Bytes: 800, Files: 8}
	v.Workspaces = []storagebudget.StatusWorkspace{workspace}
	v.Filesystems = []storagebudget.Filesystem{{ID: id(5), Generation: 1, ByteConstraint: id(6), InodeConstraint: id(6)}}
	v.CapacityConstraints = []storagebudget.CapacityConstraint{{ID: id(6), Generation: 1, Resources: 3, Headroom: storagebudget.CapacityVector{Bytes: 100, Inodes: 10}}}
	v.CapacityPromised = []storagebudget.CapacityPromise{{Constraint: id(6), Generation: 1, Peak: storagebudget.CapacityVector{Bytes: 50, Inodes: 5}}}
	v.Tickets.Active = 3
	v.Tickets.Uncertain = 2
	v.Tickets.TerminalUnapplied = 1
	p, err := storageStatusSanitize(v, id(1))
	if err != nil {
		t.Fatal(err)
	}
	cached, err := storageStatusEncode(p, storageStatusBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret", "private", "workflow", "Locator", "Owner", "Identity", "Components", "Tickets"} {
		if bytes.Contains(cached, []byte(forbidden)) {
			t.Fatal("internal payload retained", forbidden)
		}
	}
	out := storageStatusTestPublic(t, p, storageHexID(id(2)))
	if out.Constraints[0].PromisedPeak.Bytes != "50" || out.Installation.PromisedPeak.Bytes != "30" || out.Installation.PromisedSettlement.Bytes != "20" {
		t.Fatal("capacity or settlement double-counted")
	}
	if out.Recovery.ActiveTicketCount != 3 || out.Recovery.UncertainTicketCount != 2 || out.Recovery.TerminalAwaitingCheckpointCount != 1 {
		t.Fatal("lost exact count partition")
	}
	// Protected conservative control charges are not assigned to a filesystem.
	v.Installation[storagebudget.Control].Bytes = 1
	p, err = storageStatusSanitize(v, id(1))
	if err != nil {
		t.Fatal(err)
	}
	out = storageStatusTestPublic(t, p, "")
	if out.Filesystems[0].Completeness != "partial" {
		t.Fatal("unmatched control charge claimed complete", out.Filesystems)
	}
	v.CapacityPromised[0].Generation = 2
	if _, err = storageStatusSanitize(v, id(1)); err == nil {
		t.Fatal("capacity generation join accepted")
	}
}

func TestStorageStatusEncodingFailureReturnsSmallUnavailable(t *testing.T) {
	s := &Server{}
	// Even a malformed in-memory envelope must not yield an empty or partial 200.
	reason := strings.Repeat("x", storageStatusBytes)
	w := httptest.NewRecorder()
	s.writeStorageStatus(w, storageStatusResponse{Version: 1, Availability: "stale", Reason: &reason, Pressure: storageStatusPressure{Codes: []string{reason}}, Snapshot: &storageStatusSnapshot{}})
	var out storageStatusResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(w.Body.Bytes()) > storageStatusEnvelopeBytes || out.Availability != "unavailable" || out.Snapshot != nil || out.Reason == nil || *out.Reason != storageStatusUnknown {
		t.Fatal("unbounded/partial fallback", w.Code, w.Body.Len())
	}
}

// The read-only gate names an unusable private store's refusal, and keeps the
// generic pause for a Server that has no store at all (one New did not build),
// rather than reading the absent store.
func TestStorageReadOnlyGateWithAndWithoutAStore(t *testing.T) {
	sessions, err := newSessionStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sessions.close)
	for _, tc := range []struct {
		name   string
		store  *assistantStore
		status int
		code   string
	}{{"no-store", nil, 503, ""}, {"unusable-store", &assistantStore{problem: errors.New("unsafe directory")}, 409, CodeAssistantUnusableStore}} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{cfg: Config{Token: testToken}, sessions: sessions, storageReadOnly: true, assistant: tc.store}
			token := policyTestSession(t, s)
			r := httptest.NewRequest("GET", "/api/storage", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			if !s.serveStorageReadOnly(w, r) || w.Code != tc.status || tc.code != "" && !strings.Contains(w.Body.String(), tc.code) {
				t.Fatal("read-only gate", w.Code, w.Body.String())
			}
		})
	}
}
