package desk

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Hold the real response at transport delivery, after its immutable body has
// been copied. This barrier does not modify application state or admission.
type storageStatusTransportBarrier struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (w *storageStatusTransportBarrier) Write(raw []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(raw)
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return n, err
}

func TestStorageStatusTransportDoesNotRetainFirstUseExclusion(t *testing.T) {
	cfg := ownerActualConfig(t)
	cfg.JpackBin = standInRuntime(t)
	cfg.Logger = log.New(io.Discard, "", 0)
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("actual startup refused: class=%s", storageFirstUseErrorClass(err))
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("actual close refused: class=%s", storageFirstUseErrorClass(err))
		}
	})
	if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
		t.Fatal("actual startup did not enroll")
	}
	c, owner := s.storageConstruction, s.storageConstruction.owner
	origin := "http://127.0.0.1:" + strconv.Itoa(s.cfg.Port)
	request := func(method, path, bearer, body string) *http.Request {
		var input io.Reader
		if body != "" {
			input = strings.NewReader(body)
		}
		r := httptest.NewRequest(method, origin+path, input)
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		return r
	}
	setup := httptest.NewRecorder()
	s.ServeHTTP(setup, request(http.MethodPost, "/api/auth/setup", "", `{"code":"`+testToken+`"}`))
	var session struct {
		ID string `json:"id"`
	}
	if setup.Code != http.StatusOK || json.Unmarshal(setup.Body.Bytes(), &session) != nil || len(session.ID) != 48 {
		t.Fatal("normal owner setup failed")
	}

	// An actual in-progress diagnostic read must still exclude enrollment.
	// The change only shortens the completed read's transport tail.
	_, heldOwner, use, err := s.storageStatusLifetime(t.Context())
	if err != nil {
		t.Fatalf("diagnostic lifetime refused: class=%s", storageFirstUseErrorClass(err))
	}
	checkErr := func() error {
		defer heldOwner.drop()
		defer use.Close()
		_, selection, err := c.historySettings(t.Context(), s)
		if err != nil {
			return err
		}
		if selection.explicit {
			t.Error("history was enrolled before actual first use")
		}
		p, err := c.beginEnrollmentEpoch(t.Context())
		if p != nil {
			_ = p.finish(nil)
			t.Error("enrollment bypassed the active diagnostic read")
		}
		if !errors.Is(err, storagebudget.ErrBusy) {
			t.Error("active diagnostic read did not preserve nonblocking busy refusal")
		}
		return nil
	}()
	if checkErr != nil {
		t.Fatalf("original history selection refused: class=%s", storageFirstUseErrorClass(checkErr))
	}

	beforeConfig := storageTestTree(t, s.configDir)
	owner.mu.Lock()
	beforeEpoch := owner.censusEpoch
	owner.mu.Unlock()
	release := make(chan struct{})
	barrier := &storageStatusTransportBarrier{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: release}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		s.ServeHTTP(barrier, request(http.MethodGet, "/api/storage/status", session.ID, ""))
	}()
	var unblock sync.Once
	defer func() {
		unblock.Do(func() { close(release) })
		<-finished
	}()
	select {
	case <-barrier.entered:
	case <-finished:
		t.Fatal("status did not reach the transport barrier")
	case <-t.Context().Done():
		t.Fatal("status transport barrier canceled")
	}
	var observed storageStatusResponse
	if barrier.Code != http.StatusOK || json.Unmarshal(barrier.Body.Bytes(), &observed) != nil || observed.Snapshot == nil {
		t.Fatal("actual diagnostic did not deliver a bounded snapshot")
	}
	if !reflect.DeepEqual(beforeConfig, storageTestTree(t, s.configDir)) {
		t.Fatal("diagnostic persisted or pre-enrolled history")
	}
	owner.mu.Lock()
	active := owner.active
	owner.mu.Unlock()
	if active != 0 {
		t.Errorf("completed diagnostic retained owner during transport: active=%d", active)
	}

	// The first real conversation demand runs while delivery is still blocked.
	// This is one request, not an automatic retry of an uncertain enrollment.
	first := httptest.NewRecorder()
	s.ServeHTTP(first, request(http.MethodGet, "/api/conversations", session.ID, ""))
	if first.Code != http.StatusOK {
		t.Fatalf("first conversation with blocked status delivery refused: http=%d", first.Code)
	}
	var initial conversationReply
	if json.Unmarshal(first.Body.Bytes(), &initial) != nil || initial.SHA256 == "" {
		t.Fatal("first conversation did not return its actual revision")
	}
	owner.mu.Lock()
	enrolledEpoch := owner.censusEpoch
	owner.mu.Unlock()
	if enrolledEpoch <= beforeEpoch || observed.Snapshot.Epoch != storageStatusUInt(beforeEpoch) {
		t.Fatal("first use did not advance beyond the detached diagnostic observation")
	}
	second := httptest.NewRecorder()
	s.ServeHTTP(second, request(http.MethodGet, "/api/conversations", session.ID, ""))
	var retained conversationReply
	if second.Code != http.StatusOK || json.Unmarshal(second.Body.Bytes(), &retained) != nil || retained.SHA256 != initial.SHA256 {
		t.Fatal("ordinary subsequent read lost the first-use revision")
	}
	owner.mu.Lock()
	afterEpoch := owner.censusEpoch
	owner.mu.Unlock()
	if afterEpoch != enrolledEpoch {
		t.Fatal("ordinary subsequent read repeated enrollment")
	}
	unblock.Do(func() { close(release) })
	<-finished
}
