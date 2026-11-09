package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Metadata only: never retain response bodies, request bodies, credentials,
// source names, raw errors or filesystem paths in the server trace.
type storageBrowserTraceRow struct {
	Route          string   `json:"route"`
	Method         string   `json:"method"`
	Status         int      `json:"status"`
	Bytes          int      `json:"bytes"`
	FailureClasses []string `json:"failureClasses,omitempty"`
}
type storageBrowserResponse struct {
	http.ResponseWriter
	status, bytes   int
	captureFailure  bool
	failureBody     []byte // Volatile, capped at 1024 bytes; never serialized.
	failureOverflow bool
}

func (w *storageBrowserResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *storageBrowserResponse) Write(raw []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(raw)
	w.bytes += n
	if w.captureFailure && w.status >= 400 {
		if w.bytes > 1024 {
			w.failureBody = nil
			w.failureOverflow = true
		} else if !w.failureOverflow {
			if w.failureBody == nil {
				w.failureBody = make([]byte, 0, 1024)
			}
			w.failureBody = append(w.failureBody, raw[:n]...)
		}
	}
	return n, err
}

// Classify the actual fixed error envelope without exposing arbitrary prose.
// Invalid/oversized envelopes and unrecognized constituents emit only "other".
func (w *storageBrowserResponse) failureClasses() []string {
	defer func() { w.failureBody = nil }()
	if !w.captureFailure || w.status < 400 {
		return nil
	}
	other := []string{"other"}
	if w.failureOverflow {
		return other
	}
	var value struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	decoder := json.NewDecoder(bytes.NewReader(w.failureBody))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.Code != CodeInternal || !strings.HasPrefix(value.Error, "Chat storage: ") {
		return other
	}
	// writeJSONCoded emits this exact canonical envelope. Requiring it also
	// excludes duplicate members, missing members and non-string values.
	canonical, err := json.Marshal(codedBody(value.Code, value.Error))
	if err != nil || !bytes.Equal(w.failureBody, append(canonical, '\n')) {
		return other
	}
	allowed := map[string]string{
		storagebudget.ErrBusy.Error():             "busy",
		storagebudget.ErrAuthority.Error():        "authority",
		storagebudget.ErrUnknown.Error():          "unknown-outcome",
		storagebudget.ErrBounds.Error():           "bounds",
		storagebudget.ErrStale.Error():            "stale",
		storagebudget.ErrUnsupported.Error():      "unsupported",
		storagebudget.ErrLimit.Error():            "limit",
		storagebudget.ErrHeadroom.Error():         "headroom",
		errStorageCustody.Error():                 "custody",
		errStorageEnrollmentUnresolved.Error():    "enrollment-unresolved",
		errStorageEnrollmentQualification.Error(): "native-unqualified",
		errStorageEnrollmentPeakMigration.Error(): "enrollment-migration",
		errStorageOwnerRecovery.Error():           "owner-recovery",
		errStorageOwnerTransitionRequired.Error(): "owner-transition",
		context.Canceled.Error():                  "canceled",
		context.DeadlineExceeded.Error():          "deadline",
		os.ErrNotExist.Error():                    "missing",
		os.ErrClosed.Error():                      "closed",
	}
	parts := strings.Split(strings.TrimPrefix(value.Error, "Chat storage: "), "\n")
	if len(parts) > 8 {
		return other
	}
	classes := []string{}
	seen := map[string]bool{}
	for _, part := range parts {
		class, ok := allowed[part]
		if !ok {
			return other
		}
		if !seen[class] {
			classes = append(classes, class)
			seen[class] = true
		}
	}
	return classes
}

type storageBrowserLog struct{ bytes atomic.Int64 }

func (l *storageBrowserLog) Write(raw []byte) (int, error) {
	l.bytes.Add(int64(len(raw)))
	return len(raw), nil
}

// Bound subprocess diagnostics even if a browser unexpectedly floods stdout.
type storageBrowserOutput struct {
	raw      []byte
	overflow bool
}

func (o *storageBrowserOutput) Write(raw []byte) (int, error) {
	n := len(raw)
	room := 65536 - len(o.raw)
	if n > room {
		o.raw = append(o.raw, raw[:room]...)
		o.overflow = true
	} else {
		o.raw = append(o.raw, raw...)
	}
	return n, nil
}

// Source-only authoring: this fixture requires the later approved launcher,
// exact integrated web/dist, offline Runtime/native fixture and supplied tools.
// No fabricated enrollment, fake pressure slot, provider call or disk exhaustion.
func TestStorageBudgetStatusBrowser(t *testing.T) {
	script := os.Getenv("JPS_STORAGE_STATUS_BROWSER")
	if script == "" {
		t.Skip("set JPS_STORAGE_STATUS_BROWSER after the integrated web build")
	}
	node, chrome := os.Getenv("JPS_STORAGE_STATUS_BROWSER_NODE"), os.Getenv("JPS_STORAGE_STATUS_BROWSER_CHROME")
	if !filepath.IsAbs(script) || !filepath.IsAbs(node) || !filepath.IsAbs(chrome) {
		t.Fatal("supply explicit absolute fixture script, node and browser paths")
	}
	cfg := ownerActualConfig(t)
	// Always provide the repository stand-in, regardless of ambient JPACK_BIN.
	cfg.JpackBin = standInRuntime(t)
	cfg.Static = os.DirFS("../../web/dist")
	logs := &storageBrowserLog{}
	cfg.Logger = log.New(logs, "", 0)
	updateRoot, updateRun := researchRunFixture(t)
	createRoot, createRun := researchRunFixture(t)
	if err := createRoot.Remove("record.json"); err != nil {
		t.Fatal(err)
	}
	oldJournal, err := readPrivateData(updateRoot, "record.json", inputResearchRecordLimit)
	if err != nil {
		t.Fatal(err)
	}
	var refused atomic.Bool
	var workspaceCalls, createCalls, updateCalls, forbidden atomic.Int64
	var traceMu sync.Mutex
	trace := []storageBrowserTraceRow{}
	traceOverflow := false
	var s *Server
	var baselineConfig, baselineData, baselineUpdate, baselineCreate map[string]string
	var baselineCalls, baselineOccurrences uint64
	var baselineLog int64
	var frozen bool
	// Controls are exclusive test-harness actions, outside the application router.
	// The launch secret authorizes only this disposable wrapper; never log it.
	wrap := func(next http.Handler) http.Handler {
		actual, ok := next.(*Server)
		if !ok {
			t.Fatal("actual Server required")
		}
		s = actual
		s.testBeforeStoragePersistence = func(target storagePersistenceTarget) error {
			if !refused.Load() {
				return nil
			}
			switch target.operation {
			case storagePersistenceWorkspace:
				workspaceCalls.Add(1)
			case storagePersistenceResearchCreate:
				createCalls.Add(1)
			case storagePersistenceResearchUpdate:
				updateCalls.Add(1)
			default:
				return nil
			}
			return &os.PathError{Op: "stage", Path: target.name, Err: syscall.ENOSPC}
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/__storage_fixture/") {
				if r.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(w, "unauthorized", 401)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/__storage_fixture/arm":
					if r.Method != "POST" {
						w.WriteHeader(405)
						return
					}
					refused.Store(true)
				case "/__storage_fixture/journal-fail":
					if r.Method != "POST" || !refused.Load() {
						w.WriteHeader(409)
						return
					}
					if err := s.researchJournal(createRoot, createRun.ID).Create(r.Context(), createRun); !errors.Is(err, syscall.ENOSPC) {
						t.Error("expected default file Create ENOSPC")
						w.WriteHeader(500)
						return
					}
					var callbacks int
					saved, err := s.researchJournal(updateRoot, updateRun.ID).Update(r.Context(), func(next *inputResearchRun) error { callbacks++; next.Step++; return nil })
					current, readErr := readPrivateData(updateRoot, "record.json", inputResearchRecordLimit)
					_, missingErr := createRoot.Lstat("record.json")
					if !errors.Is(err, syscall.ENOSPC) || saved.ID != "" || callbacks != 1 || readErr != nil || !bytes.Equal(oldJournal, current) || !errors.Is(missingErr, os.ErrNotExist) {
						t.Error("default journal failure changed bytes/snapshot or replayed callback")
						w.WriteHeader(500)
						return
					}
				case "/__storage_fixture/freeze":
					if r.Method != "POST" {
						w.WriteHeader(405)
						return
					}
					// Fixture-only owned-tree comparison, never a status-handler operation.
					baselineConfig = storageTestTree(t, s.configDir)
					baselineData = storageTestTree(t, s.defaultChatDataPath())
					baselineUpdate = storageTestTree(t, updateRoot.Name())
					baselineCreate = storageTestTree(t, createRoot.Name())
					baselineCalls = uint64(workspaceCalls.Load() + createCalls.Load() + updateCalls.Load())
					baselineOccurrences = runtimePressureOccurrences(&s.storageConstruction.owner.statusPressure)
					baselineLog = logs.bytes.Load()
					frozen = true
				case "/__storage_fixture/verify":
					if r.Method != "POST" || !frozen {
						w.WriteHeader(409)
						return
					}
					same := reflect.DeepEqual(baselineConfig, storageTestTree(t, s.configDir)) && reflect.DeepEqual(baselineData, storageTestTree(t, s.defaultChatDataPath())) && reflect.DeepEqual(baselineUpdate, storageTestTree(t, updateRoot.Name())) && reflect.DeepEqual(baselineCreate, storageTestTree(t, createRoot.Name()))
					current, readErr := readPrivateData(updateRoot, "record.json", inputResearchRecordLimit)
					if !same || readErr != nil || !bytes.Equal(oldJournal, current) || uint64(workspaceCalls.Load()+createCalls.Load()+updateCalls.Load()) != baselineCalls || runtimePressureOccurrences(&s.storageConstruction.owner.statusPressure) != baselineOccurrences || logs.bytes.Load() != baselineLog {
						t.Error("diagnostic refresh persisted, logged, retried or changed retained bytes")
						w.WriteHeader(500)
						return
					}
				case "/__storage_fixture/trace":
					if r.Method != "GET" {
						w.WriteHeader(405)
						return
					}
					traceMu.Lock()
					defer traceMu.Unlock()
					_ = json.NewEncoder(w).Encode(map[string]any{"rows": trace, "overflow": traceOverflow, "workspaceRefusals": workspaceCalls.Load(), "createRefusals": createCalls.Load(), "updateRefusals": updateCalls.Load(), "forbiddenDispatches": forbidden.Load()})
					return
				default:
					w.WriteHeader(404)
					return
				}
				_, _ = io.WriteString(w, `{"ok":true}`)
				return
			}
			// Refuse before any accidental execution/effect request in this offline
			// journey. This is a fixture interlock, not an application security gate.
			if r.Method != "GET" && r.Method != "HEAD" && r.URL.Path != "/api/conversations" && r.URL.Path != "/api/session" && r.URL.Path != "/api/auth/setup" {
				forbidden.Add(1)
				http.Error(w, "offline fixture forbids dispatch", 503)
				return
			}
			route := ""
			if r.URL.Path == "/api/storage/status" {
				route = "storage-status"
			}
			if r.URL.Path == "/api/conversations" {
				route = "conversations"
			}
			if route == "" {
				next.ServeHTTP(w, r)
				return
			}
			out := &storageBrowserResponse{ResponseWriter: w, captureFailure: route == "conversations"}
			next.ServeHTTP(out, r)
			classes := out.failureClasses()
			traceMu.Lock()
			defer traceMu.Unlock()
			if len(trace) >= 256 {
				traceOverflow = true
				return
			}
			trace = append(trace, storageBrowserTraceRow{Route: route, Method: r.Method, Status: out.status, Bytes: out.bytes, FailureClasses: classes})
		})
	}
	_, ts := startDeskWithHandler(t, cfg, wrap)
	defer ts.Close()
	defer s.Close()
	if s.storageReadOnly || s.storageConstruction == nil || s.storageEnrollment == nil {
		t.Fatal("actual supported startup did not enroll")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, script, ts.URL, testToken, chrome)
	output := &storageBrowserOutput{}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Run(); err != nil {
		// Preserve the bounded metadata trace even when the browser stops early.
		// The trace contains no bodies, credentials, paths or raw server errors.
		traceMu.Lock()
		encoded, traceErr := json.Marshal(map[string]any{"kind": "SERVER", "instrumentation": "fixture-metadata-only", "rows": trace, "overflow": traceOverflow, "forbiddenDispatches": forbidden.Load()})
		traceMu.Unlock()
		if traceErr == nil {
			t.Log(string(encoded))
		}
		t.Fatalf("browser failed: %v; bounded diagnostics: %s", err, output.raw)
	}
	if output.overflow {
		t.Fatal("browser output exceeded 64 KiB")
	}
	if workspaceCalls.Load() != 1 || createCalls.Load() != 1 || updateCalls.Load() != 1 || forbidden.Load() != 0 || traceOverflow {
		t.Fatal("unexpected writer retries, missing boundaries, dispatch or trace overflow")
	}
	t.Log(string(output.raw))
	traceMu.Lock()
	defer traceMu.Unlock()
	encoded, err := json.Marshal(map[string]any{"kind": "SERVER", "instrumentation": "fixture-metadata-only", "rows": trace})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(encoded))
}
