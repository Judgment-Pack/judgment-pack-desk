//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrivateDataLockWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	held, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err = lockPrivateData(held, true); err != nil {
		t.Fatal(err)
	}
	next, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- waitPrivateDataLock(ctx, next, false) }()
	select {
	case err := <-result:
		t.Fatalf("did not wait: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	if err = <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err = waitPrivateDataLock(ctx, next, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
	held.Close()
	if err = waitPrivateDataLock(context.Background(), next, true); err != nil {
		t.Fatal(err)
	}
	next.Close()
	if err = waitPrivateDataLock(context.Background(), next, false); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("invalid file must fail immediately: %v", err)
	}
}

func TestDialectRefreshWaitsForStorageTransaction(t *testing.T) {
	s, _ := newTestServer(t, false)
	call := func(method, path, body, revision string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("If-Match", revision)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	created := call("POST", "/api/input-preparations", `{"name":"RFPs","recipe":{"intent":"Find opportunities"}}`, "")
	var saved inputPreparationReply
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &saved) != nil {
		t.Fatal(created.Code, created.Body.String())
	}
	root, err := s.openInputPreparations(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = lockPrivateData(lock, true); err != nil {
		t.Fatal(err)
	}
	path := "/api/input-preparations/" + saved.Document.ID
	type result struct {
		path     string
		response *httptest.ResponseRecorder
	}
	done := make(chan result, 3)
	for _, p := range []string{"/api/input-preparations", path} {
		go func() { done <- result{p, call("GET", p, "", "")} }()
	}
	// A concurrent save must still check its revision AFTER acquiring the lock.
	go func() {
		done <- result{"save", call("PUT", path, `{"name":"Old edit","recipe":{"intent":"Old"}}`, saved.SHA256)}
	}()
	select {
	case got := <-done:
		t.Fatalf("read/save failed instead of waiting: %s %d", got.path, got.response.Code)
	case <-time.After(40 * time.Millisecond):
	}
	saved.Document.Name = "Saved during refresh"
	if _, err = writeInputPreparation(root, saved.Document); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	for range 3 {
		select {
		case got := <-done:
			want := 200
			if got.path == "save" {
				want = 409
			}
			if got.response.Code != want {
				t.Fatal(got.path, got.response.Code, got.response.Body.String())
			}
			if want == 200 && !strings.Contains(got.response.Body.String(), "Saved during refresh") {
				t.Fatal("stale snapshot", got.path)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("request did not resume")
		}
	}
}

func TestPrivateWorkspaceReadsWaitForStorageTransaction(t *testing.T) {
	s, _, _ := assistantServer(t)
	held, err := s.privateDataLock(true)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	paths := []string{"/api/conversations", "/api/draft-packs", "/api/pack-tests", "/api/briefs"}
	results := make(chan *httptest.ResponseRecorder, len(paths))
	for _, path := range paths {
		go func() {
			r := httptest.NewRequest("GET", path, nil)
			r.Header.Set("Authorization", "Bearer "+testToken)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			results <- w
		}()
	}
	select {
	case w := <-results:
		t.Fatal("read failed before storage became available", w.Code, w.Body.String())
	case <-time.After(40 * time.Millisecond):
	}
	held.Close()
	for range paths {
		select {
		case w := <-results:
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
		case <-time.After(2 * time.Second):
			t.Fatal("read did not resume")
		}
	}
}
