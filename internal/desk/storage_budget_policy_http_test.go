package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func policyHTTPFixture() storagePolicySnapshot {
	l := storagebudget.Limits{Total: storagebudget.Vector{Bytes: 9007199254740993, Files: 100, Directories: 100}}
	for i := range l.Category {
		l.Category[i] = l.Total
	}
	return storagePolicySnapshot{Installation: storagebudget.ID{1}, Sequence: ^uint64(0), Revision: ^uint64(0), Epoch: ^uint64(0), Workspace: storagebudget.RegisteredWorkspace{ID: storagebudget.ID{2}, Generation: ^uint64(0), Limits: l}, Policy: storagebudget.Policy{Installation: l, Workspace: l, ObservationMaxAge: time.Second}}
}
func policyHTTPRaw(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	storageTestMust(t, err)
	return string(raw)
}
func policyHTTPServer(t *testing.T) *Server {
	t.Helper()
	sessions, err := newSessionStore()
	storageTestMust(t, err)
	t.Cleanup(sessions.close)
	return &Server{cfg: Config{Token: testToken}, sessions: sessions}
}
func policyHTTPCall(t *testing.T, s *Server, method, path, body string, routed bool) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	r.Header.Set("Authorization", "Bearer "+policyTestSession(t, s))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	if routed {
		s.mux.ServeHTTP(w, r)
	} else {
		s.handleStoragePolicy(w, r)
	}
	return w
}
func TestStoragePolicyHTTPDecimalSnapshot(t *testing.T) {
	old := policyHTTPFixture()
	dto := storagePolicyHTTPFormat(old)
	raw := policyHTTPRaw(t, dto)
	if len(raw) > storagePolicyHTTPBytes || !strings.Contains(raw, `"bytes":"9007199254740993"`) || !strings.Contains(raw, `"revision":"18446744073709551615"`) {
		t.Fatal("decimal precision lost")
	}
	var decoded storagePolicyHTTPSnapshot
	storageTestMust(t, storagePolicyHTTPDecode([]byte(raw), &decoded))
	got, err := decoded.value()
	storageTestMust(t, err)
	if !reflect.DeepEqual(old, got) {
		t.Fatal("detached complete snapshot changed")
	}
}
func TestStoragePolicyHTTPStrictRequest(t *testing.T) {
	s := policyHTTPServer(t)
	snapshot := storagePolicyHTTPFormat(policyHTTPFixture())
	limits := snapshot.Workspace.Limits
	request := storagePolicyHTTPRequest{Expected: &snapshot, Scope: "workspace", Limits: &limits}
	valid := policyHTTPRaw(t, request)
	// The nil original host has no clean-refusal proof and returns503 after decoding;
	// malformed inputs must stop with400 before any host operation is reachable.
	for _, tc := range []struct{ name, raw string }{
		{"empty-object", `{}`}, {"null", `null`}, {"array", `[]`}, {"oversized", strings.Repeat(" ", storagePolicyHTTPBytes) + valid},
		{"concatenated", valid + `{}`}, {"trailing-value", valid + ` true`},
		{"duplicate", strings.Replace(valid, `"scope":"workspace"`, `"scope":"workspace","scope":"default"`, 1)},
		{"escaped-duplicate", strings.Replace(valid, `"scope":"workspace"`, `"scope":"workspace","sc\u006fpe":"default"`, 1)},
		{"unknown", strings.Replace(valid, `"scope":"workspace"`, `"scope":"workspace","path":"/foreign"`, 1)},
		{"case-alias", strings.Replace(valid, `"scope":`, `"Scope":`, 1)},
		{"unknown-scope", strings.Replace(valid, `"scope":"workspace"`, `"scope":"all"`, 1)},
		{"missing-expected", strings.Replace(valid, `"expected":`+policyHTTPRaw(t, snapshot)+`,`, "", 1)},
		{"null-expected", strings.Replace(valid, policyHTTPRaw(t, snapshot), `null`, 1)},
		{"missing-limits", `{"expected":` + policyHTTPRaw(t, snapshot) + `,"scope":"workspace"}`},
		{"null-limits", `{"expected":` + policyHTTPRaw(t, snapshot) + `,"scope":"workspace","limits":null}`},
		{"missing-total", strings.Replace(valid, `"total":`+policyHTTPRaw(t, limits.Total)+`,`, "", 1)},
		{"null-total", strings.Replace(valid, `"total":`+policyHTTPRaw(t, limits.Total), `"total":null`, 1)},
		{"missing-nested-field", strings.Replace(valid, `"directories":"100"`, `"other":"100"`, 1)},
		{"null-nested-field", strings.Replace(valid, `"files":"100"`, `"files":null`, 1)},
		{"duplicate-nested", strings.Replace(valid, `"files":"100"`, `"files":"100","files":"1"`, 1)},
		{"eight-categories", strings.Replace(valid, policyHTTPRaw(t, limits.Categories), policyHTTPRaw(t, limits.Categories[:8]), 1)},
		{"ten-categories", strings.Replace(valid, policyHTTPRaw(t, limits.Categories), policyHTTPRaw(t, append(append([]storageStatusVector{}, limits.Categories...), limits.Total)), 1)},
		{"null-categories", strings.Replace(valid, policyHTTPRaw(t, limits.Categories), `null`, 1)},
		{"number", strings.Replace(valid, `"9007199254740993"`, `9007199254740993`, 1)},
		{"leading-zero", strings.Replace(valid, `"9007199254740993"`, `"01"`, 1)},
		{"negative", strings.Replace(valid, `"9007199254740993"`, `"-1"`, 1)},
		{"plus", strings.Replace(valid, `"9007199254740993"`, `"+1"`, 1)},
		{"fraction", strings.Replace(valid, `"9007199254740993"`, `"1.5"`, 1)},
		{"zero", strings.Replace(valid, `"9007199254740993"`, `"0"`, 1)},
		{"uint64-overflow", strings.Replace(valid, `"revision":"18446744073709551615"`, `"revision":"18446744073709551616"`, 1)},
		{"finite-vector-overflow", strings.Replace(valid, `"9007199254740993"`, `"1152921504606846977"`, 1)},
		{"zero-identity", strings.Replace(valid, snapshot.Installation, strings.Repeat("0", 32), 1)},
		{"uppercase-identity", strings.Replace(valid, snapshot.Installation, "ABCDEF00000000000000000000000000", 1)},
		{"zero-generation", strings.Replace(valid, `"generation":"18446744073709551615"`, `"generation":"0"`, 1)},
		{"age-overflow", strings.Replace(valid, `"observationMaxAgeNanos":"1000000000"`, `"observationMaxAgeNanos":"9223372036854775808"`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := policyHTTPCall(t, s, "POST", "/api/storage/policy", tc.raw, false)
			if w.Code != 400 || w.Header().Get(RefusalHeader) == "" {
				t.Fatal("invalid input reached host", w.Code, w.Body.String())
			}
		})
	}
}
func TestStoragePolicyHTTPGuardAndMethods(t *testing.T) {
	s := policyHTTPServer(t)
	token := policyTestSession(t, s)
	for _, tc := range []struct {
		name, method, path, body, auth, origin, contentType string
		readOnly                                            bool
		status                                              int
	}{
		{"session-first", "POST", "/api/storage/policy", "{", "", "https://evil.test", "application/json", false, 401},
		{"origin", "POST", "/api/storage/policy/prepare", "{}", "Bearer " + token, "https://evil.test", "application/json", false, 403},
		{"method", "PUT", "/api/storage/policy", "", "Bearer " + token, "", "", false, 405},
		{"prepare-get", "GET", "/api/storage/policy/prepare", "", "Bearer " + token, "", "", false, 405},
		{"escaped-path", "GET", "/api/storage/%70olicy", "", "Bearer " + token, "", "", false, 405},
		{"trailing-path", "GET", "/api/storage/policy/", "", "Bearer " + token, "", "", false, 405},
		{"query", "GET", "/api/storage/policy?scope=foreign", "", "Bearer " + token, "", "", false, 400},
		{"empty-query", "GET", "/api/storage/policy?", "", "Bearer " + token, "", "", false, 400},
		{"get-body", "GET", "/api/storage/policy", "{}", "Bearer " + token, "", "application/json", false, 400},
		{"media", "POST", "/api/storage/policy/prepare", "{}", "Bearer " + token, "", "text/plain", false, 400},
		{"prepare-input", "POST", "/api/storage/policy/prepare", `{"path":"/foreign"}`, "Bearer " + token, "", "application/json", false, 400},
		{"read-only", "POST", "/api/storage/policy/prepare", "{}", "Bearer " + token, "", "application/json", true, 503},
		{"read-only-edit", "POST", "/api/storage/policy", "{}", "Bearer " + token, "", "application/json", true, 503},
		{"prepare-without-owned-invocation", "POST", "/api/storage/policy/prepare", "{}", "Bearer " + token, "", "application/json", false, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.storageReadOnly = tc.readOnly
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Authorization", tc.auth)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			s.handleStoragePolicy(w, r)
			if w.Code != tc.status || w.Header().Get(RefusalHeader) == "" {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	s.storageReadOnly = false
	w := policyHTTPCall(t, s, "GET", "/api/storage/policy", "", false)
	if w.Code != 503 || w.Header().Get("Cache-Control") != "no-store" || s.storageEnrollment != nil {
		t.Fatal("GET created preparation or leaked unavailable state", w.Code)
	}
}
func TestStoragePolicyHTTPJoinedUnknown(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		noEffect storagePolicyNoEffect
		status   int
	}{
		{"proven-pre-effect-stale", storagebudget.ErrStale, true, 409},
		{"proven-pre-effect-busy", storagebudget.ErrBusy, true, 409},
		{"proven-pre-effect-foreign", storagebudget.ErrAuthority, true, 409},
		{"proven-wrapped-busy", fmt.Errorf("opaque: %w", storagebudget.ErrBusy), true, 409},
		{"proven-joined-conflict", errors.Join(storagebudget.ErrStale, storagebudget.ErrBusy), true, 409},
		{"possible-effect-stale", storagebudget.ErrStale, false, 503},
		{"possible-effect-busy", storagebudget.ErrBusy, false, 503},
		{"possible-effect-authority", storagebudget.ErrAuthority, false, 503},
		{"postprepared-joined-conflict", errors.Join(storagebudget.ErrStale, storagebudget.ErrBusy), false, 503},
		{"unowned-refusal", storagebudget.ErrStale, false, 503},
		{"cleanup-io", errors.Join(storagebudget.ErrStale, io.ErrUnexpectedEOF), true, 503},
		{"cleanup-authority", errors.Join(storagebudget.ErrStale, storagebudget.ErrAuthority), false, 503},
		{"cancellation", errors.Join(storagebudget.ErrStale, context.Canceled), true, 503},
		{"unknown", errors.Join(storagebudget.ErrAuthority, storagebudget.ErrUnknown), true, 503},
		{"preparation-unproved", storagebudget.ErrAuthority, false, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			storagePolicyHTTPError(w, tc.err, true, tc.noEffect)
			if w.Code != tc.status || strings.Contains(w.Body.String(), "opaque") || strings.Contains(w.Body.String(), "EOF") {
				t.Fatal("invocation uncertainty/raw error lost", w.Code, w.Body.String())
			}
		})
	}
}

// Actual host/API equivalence remains proof-dependent. An empty or stale record
// is not replaced by a fake host callback; parent must require this node to run.
func TestStoragePolicyHTTPActualPrepareEditRestart(t *testing.T) {
	storagePolicyHostRequireRecord(t)
	cfg := ownerActualConfig(t)
	s := ownerActualNew(t, cfg)
	before := ownerReviewDurableState(t, s.storageConstruction.original.config)
	if w := policyHTTPCall(t, s, "GET", "/api/storage/policy", "", true); w.Code != 503 {
		t.Fatal("first-use GET unexpectedly writable", w.Code)
	}
	if !reflect.DeepEqual(before, ownerReviewDurableState(t, s.storageConstruction.original.config)) {
		t.Fatal("GET mutated originals")
	}
	if w := policyHTTPCall(t, s, "POST", "/api/storage/policy/prepare", "{}", true); w.Code != 204 {
		t.Fatal("prepare", w.Code, w.Body.String())
	}
	read := func(s *Server) storagePolicyHTTPSnapshot {
		w := policyHTTPCall(t, s, "GET", "/api/storage/policy", "", true)
		if w.Code != 200 {
			t.Fatal("snapshot", w.Code, w.Body.String())
		}
		var v storagePolicyHTTPSnapshot
		storageTestMust(t, storagePolicyHTTPDecode(w.Body.Bytes(), &v))
		return v
	}
	first := read(s)
	frozen := first
	var limits storagePolicyHTTPLimits
	// Use an admitted existing finite value minus one, independently of defaults.
	value, err := storagePolicyHTTPLimitsValue(first.DefaultLimits)
	storageTestMust(t, err)
	value.Total.Bytes--
	limits = storagePolicyHTTPFormatLimits(value)
	edit := storagePolicyHTTPRequest{Expected: &first, Scope: "default", Limits: &limits}
	if w := policyHTTPCall(t, s, "POST", "/api/storage/policy", policyHTTPRaw(t, edit), true); w.Code != 200 {
		t.Fatal("edit", w.Code, w.Body.String())
	}
	second := read(s)
	if second.DefaultLimits.Total.Bytes != limits.Total.Bytes || !reflect.DeepEqual(second.Workspace.Limits, frozen.Workspace.Limits) {
		t.Fatal("sticky values lost")
	}
	if w := policyHTTPCall(t, s, "POST", "/api/storage/policy", policyHTTPRaw(t, edit), true); w.Code != 409 {
		t.Fatal("stale edit not refused", w.Code, w.Body.String())
	}
	for _, mutation := range []func(*storagePolicyHTTPSnapshot){func(v *storagePolicyHTTPSnapshot) { v.Installation = first.Workspace.ID }, func(v *storagePolicyHTTPSnapshot) { v.Workspace.ID = first.Installation }, func(v *storagePolicyHTTPSnapshot) { v.Workspace.Generation = "2" }, func(v *storagePolicyHTTPSnapshot) { v.Sequence = "1" }, func(v *storagePolicyHTTPSnapshot) { v.Revision = "1" }, func(v *storagePolicyHTTPSnapshot) { v.Epoch = "1" }} {
		bad := second
		mutation(&bad)
		request := storagePolicyHTTPRequest{Expected: &bad, Scope: "workspace", Limits: &limits}
		if w := policyHTTPCall(t, s, "POST", "/api/storage/policy", policyHTTPRaw(t, request), true); w.Code != 409 {
			t.Fatal("foreign/stale coordinate admitted", w.Code, w.Body.String())
		}
	}
	copyOnce := second.DefaultLimits
	edit = storagePolicyHTTPRequest{Expected: &second, Scope: "workspace", Limits: &copyOnce}
	if w := policyHTTPCall(t, s, "POST", "/api/storage/policy", policyHTTPRaw(t, edit), true); w.Code != 200 {
		t.Fatal("one-time copy", w.Code, w.Body.String())
	}
	got := read(s)
	host, err := s.readStoragePolicySnapshot(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(got, storagePolicyHTTPFormat(host)) || !reflect.DeepEqual(got.Workspace.Limits, copyOnce) {
		t.Fatal("API differs from original host")
	}
	storageTestMust(t, s.Close())
	restarted := ownerActualNew(t, cfg)
	again := read(restarted)
	if !bytes.Equal([]byte(policyHTTPRaw(t, again)), []byte(policyHTTPRaw(t, got))) {
		t.Fatal("restart changed committed policy snapshot")
	}
}

// The seam is reached only after actual checked original publications. Capture
// its invocation while paused; no alternate error producer or fake result is used.
func TestStoragePolicyHTTPPostPublicationStaleOutcome(t *testing.T) {
	storagePolicyHostRequireRecord(t)
	for _, tc := range []struct {
		name, point string
		phase       storagebudget.PolicyPhase
		committed   bool
	}{
		{"durable-prepared", "policy-begin-before", storagebudget.PolicyQ0, false},
		{"later-q2-final", "policy-finish-before", storagebudget.PolicyQ2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			s, err := New(cfg)
			storageTestMust(t, err)
			t.Cleanup(func() {
				if s != nil {
					e := s.Close()
					if e != nil && !errors.Is(e, storagebudget.ErrStale) {
						t.Error(e)
					}
				}
			})
			if s.storageReadOnly || s.storageConstruction == nil || s.storageEnrollment == nil {
				t.Fatal("actual original startup unavailable", s.storageProblem)
			}
			if w := policyHTTPCall(t, s, "POST", "/api/storage/policy/prepare", "{}", true); w.Code != 204 {
				t.Fatal("prepare", w.Code, w.Body.String())
			}
			first, err := s.readStoragePolicySnapshot(t.Context())
			storageTestMust(t, err)
			// A real clean expected-snapshot refusal must still yield409 after its
			// original finish. It cannot write or poison the next original invocation.
			limits := first.Policy.Workspace
			limits.Total.Bytes--
			wanted := storagePolicyHTTPFormatLimits(limits)
			stale := first
			stale.Revision--
			expected := storagePolicyHTTPFormat(stale)
			request := storagePolicyHTTPRequest{Expected: &expected, Scope: "default", Limits: &wanted}
			before := ownerReviewDurableState(t, s.storageConstruction.original.config)
			if w := policyHTTPCall(t, s, "POST", "/api/storage/policy", policyHTTPRaw(t, request), true); w.Code != 409 {
				t.Fatal("clean comparison was not409", w.Code, w.Body.String())
			}
			if !reflect.DeepEqual(before, ownerReviewDurableState(t, s.storageConstruction.original.config)) {
				t.Fatal("clean refusal changed original bytes")
			}
			expected = storagePolicyHTTPFormat(first)
			entered, release := make(chan struct{}), make(chan struct{})
			s.storageConstruction.enrollmentFault = &storageEnrollmentFault{point: tc.point, entered: entered, release: release, err: storagebudget.ErrStale}
			raw := policyHTTPRaw(t, request)
			r := httptest.NewRequest("POST", "/api/storage/policy", strings.NewReader(raw)).WithContext(t.Context())
			r.Header.Set("Authorization", "Bearer "+policyTestSession(t, s))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { s.mux.ServeHTTP(w, r); close(done) }()
			released := false
			defer func() {
				if !released {
					close(release)
					<-done
				}
			}()
			select {
			case <-entered:
			case <-done:
				t.Fatal("actual policy returned before selected seam", w.Code, w.Body.String())
			}
			o := s.storageConstruction.owner
			g := &o.admission
			g.mu.Lock()
			epoch := g.enrollment
			g.mu.Unlock()
			if epoch == nil || epoch.policy == nil || !epoch.policy.started || !epoch.policy.consumed[0] {
				close(release)
				released = true
				<-done
				t.Fatal("seam did not belong to actual consumed Prepared invocation")
			}
			// No Q gate is held by these before-step seams. Compare the actual strict
			// original Q prefix, rather than inferring it from a label or marker.
			qErr := s.storageConstruction.ref.cell.q.CheckPolicyTransition(t.Context(), epoch.policy.transition, tc.phase)
			cut := ownerReviewDurableState(t, s.storageConstruction.original.config)
			close(release)
			released = true
			<-done
			storageTestMust(t, qErr)
			if w.Code != 503 || !strings.Contains(w.Body.String(), storageStatusUncertain) {
				t.Fatal("persisted stale became a clean conflict", w.Code, w.Body.String())
			}
			epoch.mu.Lock()
			drained := !epoch.running && !epoch.finishing && !epoch.entered && epoch.use == nil && epoch.project == nil && epoch.work == nil && len(epoch.holds) == 0 && len(epoch.sourceFiles) == 0
			noEffect, cause := epoch.policy.noEffect, epoch.err
			epoch.mu.Unlock()
			select {
			case <-epoch.done:
			default:
				t.Fatal("HTTP returned before original drain")
			}
			if !drained || bool(noEffect) || !errors.Is(cause, storagebudget.ErrStale) {
				t.Fatal("invocation provenance/error/drain lost", drained, noEffect, cause)
			}
			g.mu.Lock()
			failed := g.phase == storageAdmissionFailed && g.enrollment == nil
			g.mu.Unlock()
			if !failed {
				t.Fatal("uncertain publication did not require original recovery")
			}
			if !reflect.DeepEqual(cut, ownerReviewDurableState(t, s.storageConstruction.original.config)) {
				t.Fatal("error completion wrote/replayed a policy effect")
			}
			if answer := policyHTTPCall(t, s, "GET", "/api/storage/policy", "", true); answer.Code != 503 {
				t.Fatal("unrecovered GET became writable", answer.Code)
			}
			if !reflect.DeepEqual(cut, ownerReviewDurableState(t, s.storageConstruction.original.config)) {
				t.Fatal("GET retried policy recovery")
			}
			closeErr := s.Close()
			s = nil
			if closeErr != nil && !errors.Is(closeErr, storagebudget.ErrStale) {
				t.Fatal("original Close changed cause", closeErr)
			}
			s, err = New(cfg)
			storageTestMust(t, err)
			if s.storageReadOnly {
				t.Fatal("original restart did not recover", s.storageProblem)
			}
			got, err := s.readStoragePolicySnapshot(t.Context())
			storageTestMust(t, err)
			if tc.committed && got.Policy.Workspace != limits || !tc.committed && got.Policy != first.Policy || got.Workspace != first.Workspace {
				t.Fatal("recovery selected wrong already-published outcome")
			}
		})
	}
}
