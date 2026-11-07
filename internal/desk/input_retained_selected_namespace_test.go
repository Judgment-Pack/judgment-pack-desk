package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Handwritten released027 report envelope; digest bytes are fixture authority,
// not a signature or independent compatibility claim.
func retainedSelectedTestReport(t *testing.T, profiles []byte) []byte {
	t.Helper()
	if !json.Valid(profiles) {
		t.Fatal("bad selected report fixture")
	}
	return []byte(`{"contract":"selected-profile-preparation/1","bound":{"profiles":16,"profilesBytes":81920},"profiles":` + string(profiles) + `}`)
}

func TestRetainedSelectedNamespaceFullOrdinaryAndSameOwner(t *testing.T) {
	s, _ := newTestServer(t, false)
	s.localGateway = &localGateway{}
	configured := make([]map[string]string, 15)
	for i := range configured {
		configured[i] = map[string]string{"id": "ordinary-" + strconv.Itoa(i), "source": "ordinary-" + strconv.Itoa(i)}
	}
	s.cfg.RunnerInputProfiles = retainedRuntimeTestJSON(t, configured)
	ordinary, err := s.reviewedInputProfiles()
	if err != nil {
		t.Fatal(err)
	}
	var ordinaryRows []json.RawMessage
	if json.Unmarshal(ordinary, &ordinaryRows) != nil || len(ordinaryRows) != 16 {
		t.Fatal("fixture must hold all16 ordinary profiles")
	}
	originalConfig := bytes.Clone(s.cfg.RunnerInputProfiles)
	profile, _, err := retainedPrivateProfile(s)
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	listing := retainedSelectedTestReport(t, []byte(`[{"profile":`+string(profile)+`,"digest":"`+digest+`"}]`))
	var mu sync.Mutex
	var calls []string
	admitted := false
	heldLease := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer selected-private" || r.Header.Get("Origin") != "" {
			t.Error("private caller authority lost")
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/v1/status" {
			w.Write(retainedD3bRunnerStatus(t))
			return
		}
		if r.URL.Path != retainedRunnerPrefix+"selected-profiles" {
			t.Error("ordinary or unrelated route called", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		if _, err := readRetainedRunnerSelection(http.MethodPost, retainedRunnerPrefix+"selected-inputs/next", r.Header, true); err != nil {
			t.Error("exact four-role selector absent", err)
			w.WriteHeader(422)
			return
		}
		switch r.Method {
		case http.MethodPost:
			body, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Equal(body, profile) {
				t.Error("unapproved profile admitted", err)
				w.WriteHeader(400)
				return
			}
			if heldLease {
				t.Error("dispatch after owner held")
			}
			if !admitted {
				w.WriteHeader(201)
				admitted = true
			}
			w.Write([]byte(`{"profile":` + string(profile) + `,"digest":"` + digest + `"}`))
		case http.MethodGet:
			w.Write(listing)
		default:
			t.Error("unexpected method", r.Method)
			w.WriteHeader(405)
		}
	}))
	defer server.Close()
	done := make(chan struct{})
	jobs := &jobsCompanion{url: server.URL, token: "selected-private", done: done, stop: make(chan struct{})}
	s.jobs = jobs
	t.Cleanup(func() { close(done) })
	for attempt := 0; attempt < 2; attempt++ {
		approval, err := s.retainedPrivateApprove(t.Context(), nil)
		if err != nil || approval.profileDigest != digest {
			t.Fatal("new/identical selected admission failed", attempt, err)
		}
		if s.jobs != jobs || s.jobs.url != server.URL || s.jobs.token != "selected-private" {
			t.Fatal("selected admission replaced executor owner")
		}
	}
	after, err := s.reviewedInputProfiles()
	if err != nil || !bytes.Equal(after, ordinary) || !bytes.Equal(s.cfg.RunnerInputProfiles, originalConfig) {
		t.Fatal("ordinary registry changed", err)
	}
	mu.Lock()
	before := len(calls)
	heldLease = true
	mu.Unlock()
	jobs.mu.Lock()
	jobs.closed = true
	jobs.mu.Unlock()
	if approval, err := s.retainedPrivateApprove(t.Context(), nil); err == nil || approval.admission != nil {
		t.Fatal("held current owner admitted selected profile")
	}
	jobs.mu.Lock()
	jobs.closed = false
	jobs.mu.Unlock()
	mu.Lock()
	if len(calls) != before {
		t.Error("held owner reached network")
	}
	mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if approval, err := s.retainedPrivateApprove(ctx, nil); !errors.Is(err, context.Canceled) || approval.admission != nil {
		t.Fatal("cancelled selected admission dispatched", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != before {
		t.Fatal("cancelled operation reached network")
	}
	expected := []string{"GET /v1/status", "POST " + retainedRunnerPrefix + "selected-profiles", "GET " + retainedRunnerPrefix + "selected-profiles"}
	for i, call := range calls {
		if call != expected[i%3] {
			t.Fatal("selected call sequence changed", calls)
		}
	}
	entries, err := os.ReadDir(s.inputHandoffPath())
	if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
		t.Fatal("profile admission acquired or created grant", err)
	}
}

func TestRetainedSelectedNamespaceRefusals(t *testing.T) {
	for _, name := range []string{"missing-capability", "duplicate-capability", "conflict", "ordinary-id", "limit", "wrong-contract", "wrong-bound", "duplicate-id", "set-aside", "changed-profile", "different-digest", "oversized-report"} {
		t.Run(name, func(t *testing.T) {
			s, _ := newTestServer(t, false)
			s.localGateway = &localGateway{}
			profile, _, err := retainedPrivateProfile(s)
			if err != nil {
				t.Fatal(err)
			}
			digest := "sha256:" + strings.Repeat("a", 64)
			row := []byte(`{"profile":` + string(profile) + `,"digest":"` + digest + `"}`)
			report := retainedSelectedTestReport(t, []byte(`[`+string(row)+`]`))
			status := retainedD3bRunnerStatus(t)
			if name == "missing-capability" {
				status = bytes.Replace(status, []byte(`"selected-profile-preparation"`), []byte(`"ordinary"`), 1)
			}
			if name == "duplicate-capability" {
				status = bytes.Replace(status, []byte(`"selected-profile-preparation"`), []byte(`"selected-profile-preparation","selected-profile-preparation"`), 1)
			}
			switch name {
			case "wrong-contract":
				report = bytes.Replace(report, []byte("selected-profile-preparation/1"), []byte("selected-profile-preparation/2"), 1)
			case "wrong-bound":
				report = bytes.Replace(report, []byte(`"profiles":16`), []byte(`"profiles":17`), 1)
			case "duplicate-id":
				report = retainedSelectedTestReport(t, []byte(`[`+string(row)+`,`+string(row)+`]`))
			case "set-aside":
				report = bytes.Replace(report, []byte(`"digest":"`+digest+`"`), []byte(`"digest":"`+digest+`","state":"set-aside","reason":"old"`), 1)
			case "changed-profile":
				report = bytes.Replace(report, []byte(`"class":"generated"`), []byte(`"class":"record"`), 1)
			case "different-digest":
				report = bytes.Replace(report, []byte(digest), []byte("sha256:"+strings.Repeat("b", 64)), 1)
			case "oversized-report":
				report = append(report, bytes.Repeat([]byte(" "), int(retainedSelectedControlBytes)+1)...)
			}
			calls := 0
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path == "/v1/status" {
					w.Write(status)
					return
				}
				if r.URL.Path != retainedRunnerPrefix+"selected-profiles" {
					t.Error("fallback escaped selected namespace")
					w.WriteHeader(404)
					return
				}
				if r.Method == http.MethodPost {
					if name == "conflict" || name == "ordinary-id" {
						w.WriteHeader(409)
						return
					}
					if name == "limit" {
						w.WriteHeader(422)
						return
					}
					w.Write(row)
					return
				}
				w.Write(report)
			}))
			defer fixture.Close()
			done := make(chan struct{})
			s.jobs = &jobsCompanion{url: fixture.URL, token: "private", done: done, stop: make(chan struct{})}
			t.Cleanup(func() { close(done) })
			approval, err := s.retainedPrivateApprove(t.Context(), nil)
			if err == nil || approval.admission != nil {
				t.Fatal("unsupported selected namespace produced approval", name)
			}
			if strings.Contains(name, "capability") && calls != 1 {
				t.Fatal("capability refusal reached namespace", calls)
			}
			entries, err := os.ReadDir(s.inputHandoffPath())
			if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
				t.Fatal("refusal wrote grant", err)
			}
		})
	}
}

func TestRetainedSelectedNamespacePlanningBoundary(t *testing.T) {
	admission := retainedD4aAdmission(t, nil)
	path, header, err := retainedRunnerTransport(http.MethodPost, "/v1/selected-inputs/next", admission, true)
	if err != nil || path != retainedRunnerPrefix+"selected-inputs/next" || header == "" {
		t.Fatal("selected planning route unavailable", err)
	}
	if _, err := readRetainedRunnerSelection(http.MethodPost, path, http.Header{retainedSelectorHeaderName: []string{header}}, true); err != nil {
		t.Fatal("selected selector did not roundtrip", err)
	}
	for _, route := range []string{"/v1/selected-inputs/next/jobs", "/v1/selected-inputs/next?job=x", "/v1/selected-profiles", "/v1/selected-jobs", "/v1/selected-inputs/preview"} {
		if _, _, err := retainedRunnerTransport(http.MethodPost, route, admission, true); err == nil {
			t.Fatal("unapproved selected surface exposed", route)
		}
	}
	if _, _, err := retainedRunnerTransport(http.MethodPost, "/v1/selected-inputs/next", retainedD2Admission(t, false), false); err == nil {
		t.Fatal("ordinary admission selected generated namespace")
	}
}
