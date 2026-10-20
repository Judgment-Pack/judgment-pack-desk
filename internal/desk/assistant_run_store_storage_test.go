package desk

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func assistantRunHTTPFixture(t *testing.T) (*Server, *httptest.Server, assistantRunStore, assistantRunBinding, assistantRunRecord, *assistantRunLease, *assistantRunWriter) {
	t.Helper()
	server, ts, _ := assistantServer(t)
	_, binding, cp := assistantRunFixture(t)
	store := server.assistantRuns()
	binding.Project = store.project
	lease, err := store.Acquire(context.Background())
	assistantRunMust(t, err)
	t.Cleanup(func() { lease.Close() })
	record, _, err := lease.Admit(context.Background(), binding, "http-admission", digestOf([]byte("http body")), func() error { return nil })
	assistantRunMust(t, err)
	writer, err := lease.Writer(context.Background(), binding, record.ID)
	assistantRunMust(t, err)
	assistantRunMust(t, writer.Checkpoint(context.Background(), cp))
	return server, ts, store, binding, record, lease, writer
}
func assistantRunRaw(t *testing.T, store assistantRunStore) []byte {
	t.Helper()
	data, err := store.transaction(context.Background(), func(*assistantRunLedger) (bool, error) { return false, nil })
	assistantRunMust(t, err)
	raw, err := json.Marshal(data)
	assistantRunMust(t, err)
	return raw
}
func assistantRunArchive(t *testing.T, name string, raw []byte, mode os.FileMode) []byte {
	t.Helper()
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	if mode != 0 {
		header.SetMode(mode)
	}
	item, err := archive.CreateHeader(header)
	assistantRunMust(t, err)
	_, err = item.Write(raw)
	assistantRunMust(t, err)
	manifest := backupManifest{Version: 1, Files: []backupEntry{{Name: name, Bytes: int64(len(raw)), SHA256: digestOf(raw)}}}
	item, err = archive.Create("manifest.json")
	assistantRunMust(t, err)
	encoded, err := json.Marshal(manifest)
	assistantRunMust(t, err)
	_, err = item.Write(encoded)
	assistantRunMust(t, err)
	assistantRunMust(t, archive.Close())
	return out.Bytes()
}
func assistantRunBackupCode(t *testing.T, ts *httptest.Server, page bool) int {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+"/api/storage/backup", nil)
	if page {
		pageBearer(privateWorkspaceTestSession(t, ts))(req)
	} else {
		bearer(req)
	}
	res, err := ts.Client().Do(req)
	assistantRunMust(t, err)
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

func TestAssistantRunStorageBackupRestoreHistorical(t *testing.T) {
	server, ts, store, binding, record, lease, writer := assistantRunHTTPFixture(t)
	ctx := context.Background()
	saved, err := store.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	cp := *saved.Checkpoint
	cp.Operations = append(cp.Operations, contract.CheckpointOperation{ID: "3f71c7b0-7df4-4eb6-96f3-f3a1390dc3a0", Name: "read_exact", Args: json.RawMessage(`{"Fact":false,"fact":null}`), Status: "running"})
	assistantRunMust(t, writer.Checkpoint(ctx, cp))
	exact := []byte(" {\"null\":null,\"false\":false,\"number\":\"24.00\"} \n")
	assistantRunMust(t, writer.Intent(ctx, assistantRunOp("completed")))
	assistantRunMust(t, writer.Dispatch(ctx, "completed"))
	assistantRunMust(t, writer.Result(ctx, "completed", "completed", exact))
	assistantRunMust(t, writer.Intent(ctx, assistantRunOp("empty")))
	assistantRunMust(t, writer.Result(ctx, "empty", "refused", []byte{}))
	pending := assistantRunOp("pending-approval")
	pending.Kind = "action"
	pending.Intent = []byte(`{"reviewed":true,"grant":"historical-not-authority"}`)
	assistantRunMust(t, writer.Intent(ctx, pending))
	assistantRunMust(t, writer.Dispatch(ctx, pending.ID))
	if code := assistantRunBackupCode(t, ts, true); code != 409 {
		t.Fatalf("active backup %d", code)
	}
	assistantRunMust(t, lease.Close())
	if code := assistantRunBackupCode(t, ts, false); code != 401 {
		t.Fatalf("ledger backup without Policy session %d", code)
	}
	backup := backupBytes(t, ts)
	backupHash := digestOf(backup)
	original := assistantRunRaw(t, store)
	before := storageGet(t, ts)
	target := filepath.Join(t.TempDir(), "restore")
	code, body := restoreBackup(t, ts, moveChatData{Path: target, Revision: before.Revision}, backup)
	if code != 200 {
		t.Fatalf("restore %d %s", code, body)
	}
	if digestOf(backup) != backupHash {
		t.Fatal("source archive rewritten")
	}
	// Original store is untouched, including its exact pending state.
	mustBytes(t, filepath.Join(before.Path, store.name), string(original))
	restored := server.assistantRuns()
	held, err := restored.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	var facts map[string]any
	assistantRunMust(t, json.Unmarshal(held.Checkpoint.Operations[0].Args, &facts))
	if len(facts) != 2 || facts["Fact"] != false || facts["fact"] != nil {
		t.Fatal("canonical case-sensitive false/null facts changed")
	}
	if !held.Historical || held.State != "interrupted" || held.Checkpoint.Status != "stopped" || held.Operations[2].State != "uncertain" {
		t.Fatalf("restored work actionable: %+v", held)
	}
	if !bytes.Equal(held.Operations[0].Result, exact) || held.Operations[1].Result == nil || len(held.Operations[1].Result) != 0 || !bytes.Equal(held.Operations[2].Intent, pending.Intent) {
		t.Fatal("restore changed exact receipts")
	}
	imported, err := restored.Acquire(ctx)
	assistantRunMust(t, err)
	defer imported.Close()
	stale, err := imported.Writer(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if err = stale.Dispatch(ctx, pending.ID); !errors.Is(err, errAssistantRunDispatch) {
		t.Fatalf("historical grant dispatched: %v", err)
	}
	retry, created, err := imported.Admit(ctx, binding, "http-admission", digestOf([]byte("http body")), func() error { t.Error("import retry reached fresh owner CAS"); return nil })
	assistantRunMust(t, err)
	if created || retry.ID != record.ID || !retry.Historical {
		t.Fatal("restore converted retry to fresh work")
	}
}

func TestAssistantRunStorageArchiveRefusalRollback(t *testing.T) {
	for _, kind := range []string{"namespace", "unknown-field", "duplicate-run", "duplicate-member", "checkpoint", "checkpoint-missing", "envelope-alias", "state-alias", "checkpoint-alias", "unsafe", "oversize", "readback"} {
		t.Run(kind, func(t *testing.T) {
			server, ts, store, _, _, lease, _ := assistantRunHTTPFixture(t)
			assistantRunMust(t, lease.Close())
			before := storageGet(t, ts)
			raw := assistantRunRaw(t, store)
			name := store.name
			mode := os.FileMode(0)
			var ledger assistantRunLedger
			assistantRunMust(t, json.Unmarshal(raw, &ledger))
			switch kind {
			case "namespace":
				name = "assistant-runs-" + digestOf([]byte("other project")) + ".json"
			case "unknown-field":
				raw = append([]byte(`{"unexpected":true,`), raw[1:]...)
			case "duplicate-run":
				ledger.Runs = append(ledger.Runs, ledger.Runs[0])
				raw, _ = json.Marshal(ledger)
			case "duplicate-member":
				raw = append([]byte(`{"version":1,`), raw[1:]...)
			case "checkpoint":
				ledger.Runs[0].Checkpoint.Status = "uncertain"
				raw, _ = json.Marshal(ledger)
			case "checkpoint-missing":
				raw = bytes.Replace(raw, []byte(`"used":0,`), nil, 1)
			case "envelope-alias":
				raw = append([]byte(`{"Project":"`+ledger.Project+`",`), raw[1:]...)
			case "state-alias":
				raw = bytes.Replace(raw, []byte(`"state":"running"`), []byte(`"state":"running","State":"admitted"`), 1)
			case "checkpoint-alias":
				raw = bytes.Replace(raw, []byte(`"used":0`), []byte(`"used":0,"Used":1`), 1)
			case "unsafe":
				mode = os.ModeSymlink | 0600
			case "oversize":
				raw = bytes.Repeat([]byte("x"), assistantRunRecordLimit+1)
			}
			archive := assistantRunArchive(t, name, raw, mode)
			digest := digestOf(archive)
			target := filepath.Join(t.TempDir(), "refused")
			if kind == "readback" {
				testHookPrivateRead = func(n string) error {
					if n == store.name {
						return errors.New("target write readback unavailable")
					}
					return nil
				}
				defer func() { testHookPrivateRead = nil }()
			}
			code, body := restoreBackup(t, ts, moveChatData{Path: target, Revision: before.Revision}, archive)
			testHookPrivateRead = nil
			if code == 200 {
				t.Fatalf("unsafe restore committed: %s", body)
			}
			if after := storageGet(t, ts); after.Path != before.Path || after.Revision != before.Revision {
				t.Fatal("failed restore switched pointer")
			}
			if _, err := os.Lstat(filepath.Join(target, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed restore retained staged ledger: %v", err)
			}
			if digestOf(archive) != digest {
				t.Fatal("refused archive mutated")
			}
			if kind != "readback" {
				if _, err := server.assistantRuns().Read(context.Background(), ledger.Runs[0].Binding, ledger.Runs[0].ID); err != nil {
					t.Fatal("refusal damaged original ledger", err)
				}
			}
		})
	}
}

func assistantRunStorageMove(t *testing.T, ts *httptest.Server, request moveChatData) (int, string) {
	t.Helper()
	raw, _ := json.Marshal(request)
	req, _ := http.NewRequest("POST", ts.URL+"/api/storage/move", bytes.NewReader(raw))
	pageBearer(privateWorkspaceTestSession(t, ts))(req)
	res, err := ts.Client().Do(req)
	assistantRunMust(t, err)
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}

func TestAssistantRunStorageActiveMoveRestoreAndRelease(t *testing.T) {
	server, ts, store, binding, record, lease, _ := assistantRunHTTPFixture(t)
	ctx := context.Background()
	before := storageGet(t, ts)
	archive := assistantRunArchive(t, store.name, assistantRunRaw(t, store), 0)
	if code, body := restoreBackup(t, ts, moveChatData{Path: filepath.Join(t.TempDir(), "restore"), Revision: before.Revision}, archive); code != 409 {
		t.Fatalf("active restore %d %s", code, body)
	}
	target := filepath.Join(t.TempDir(), "move")
	if code, body := assistantRunStorageMove(t, ts, moveChatData{Path: target, Revision: before.Revision}); code != 409 {
		t.Fatalf("active move %d %s", code, body)
	}
	assistantRunMust(t, lease.Close())
	if code, body := assistantRunStorageMove(t, ts, moveChatData{Path: target, Revision: before.Revision}); code != 200 {
		t.Fatalf("released move %d %s", code, body)
	}
	moved := server.assistantRuns()
	if moved.project != store.project || moved.name != store.name {
		t.Fatal("storage move changed namespace")
	}
	held, err := moved.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if held.State != "running" || held.Historical {
		t.Fatal("copy treated original state as restored authority")
	}
	acquired, err := moved.Acquire(ctx)
	assistantRunMust(t, err)
	defer acquired.Close()
	held, err = moved.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if held.State != "interrupted" {
		t.Fatal("reacquire did not recover moved pending work")
	}
}

func TestAssistantRunStorageNamespaceRelocationAndStaleStore(t *testing.T) {
	store, binding, _, record, lease, writer := assistantRunAdmitted(t)
	ctx := context.Background()
	assistantRunMust(t, writer.Intent(ctx, assistantRunOp("retained")))
	assistantRunMust(t, writer.Result(ctx, "retained", "refused", []byte("false\nnull")))
	assistantRunMust(t, lease.Close())
	server := store.server
	previous := server.projectDir
	next := filepath.Join(filepath.Dir(previous), "relocated")
	root, err := server.openChatData()
	assistantRunMust(t, err)
	// Ledger is the only sibling; linkMovedHistory creates the OLD empty anchor.
	assistantRunMust(t, linkMovedHistory(root.root, previous, next))
	root.root.Close()
	assistantRunMust(t, os.Rename(previous, next))
	server.projectDir = next
	moved := server.assistantRuns()
	if moved.project != binding.Project || moved.name != store.name {
		t.Fatal("relocation orphaned sibling-only ledger")
	}
	held, err := moved.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if string(held.Operations[0].Result) != "false\nnull" {
		t.Fatal("relocation rewrote receipt")
	}
	root, err = server.openChatData()
	assistantRunMust(t, err)
	defer root.root.Close()
	// A store captured before an explicit history rebind may not keep reading
	// that stale namespace after the data lock is reacquired.
	fresh := "conversations-" + digestOf([]byte("different history")) + ".json"
	assistantRunMust(t, writePrivateData(root.root, fresh, []byte(`{"version":1,"chats":[]}`)))
	bindings, _, err := readProjectBindings(root.root)
	assistantRunMust(t, err)
	bindings.Projects[next] = fresh
	encoded, _ := json.Marshal(bindings)
	assistantRunMust(t, writePrivateData(root.root, projectBindingsName, encoded))
	if _, err = moved.Read(ctx, binding, record.ID); !errors.Is(err, errAssistantRunScope) {
		t.Fatalf("stale namespace read %v", err)
	}
	if owned, err := moved.Acquire(ctx); err == nil {
		owned.Close()
		t.Fatal("stale namespace acquired dispatch")
	}
}

func TestAssistantRunStorageStartupAndRecoveryFailure(t *testing.T) {
	server, ts, store, binding, record, lease, writer := assistantRunHTTPFixture(t)
	ctx := context.Background()
	assistantRunMust(t, writer.Intent(ctx, assistantRunOp("uncertain")))
	assistantRunMust(t, writer.Dispatch(ctx, "uncertain"))
	// Another startup recovery cannot disturb this live owner.
	server.recoverAssistantRuns()
	held, err := store.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if held.State != "running" {
		t.Fatal("startup interrupted another live owner")
	}
	assistantRunMust(t, lease.Close())
	config := server.cfg
	ts.Close()
	assistantRunMust(t, server.Close())
	reopened, rts := startDesk(t, config)
	defer reopened.Close()
	defer rts.Close()
	recovered := reopened.assistantRuns()
	held, err = recovered.Read(ctx, binding, record.ID)
	assistantRunMust(t, err)
	if held.State != "interrupted" || held.Operations[0].State != "uncertain" {
		t.Fatal("actual startup did not recover")
	}
	before := assistantRunRaw(t, recovered)
	readsBefore := 0
	testHookPrivateRead = func(n string) error {
		if n == recovered.name {
			readsBefore++
		}
		return nil
	}
	reopened.recoverAssistantRuns()
	testHookPrivateRead = nil
	if readsBefore != 1 || !bytes.Equal(before, assistantRunRaw(t, recovered)) {
		t.Fatal("repeat recovery rewrote state")
	}
	// Start a second run, then fail the recovery read-back after its commit.
	active, err := recovered.Acquire(ctx)
	assistantRunMust(t, err)
	binding.Owner.ID = "recovery-failure"
	second, _, err := active.Admit(ctx, binding, "failure", digestOf([]byte("failure")), func() error { return nil })
	assistantRunMust(t, err)
	assistantRunMust(t, active.Close())
	reads := 0
	injected := errors.New("recovery readback failed")
	testHookPrivateRead = func(n string) error {
		if n == recovered.name {
			reads++
			if reads == 2 {
				return injected
			}
		}
		return nil
	}
	failed, err := recovered.Acquire(ctx)
	testHookPrivateRead = nil
	if err == nil {
		failed.Close()
		t.Fatal("failed recovery returned dispatch lease")
	}
	if reopened.assistantRunRecoveryFailure() == nil {
		t.Fatal("recovery error not latched")
	}
	if status := storageGet(t, rts); status.Problem != errAssistantRunRecovery.Error() {
		t.Fatal("recovery failure not surfaced safely", status.Problem)
	}
	if _, _, err = active.Admit(ctx, binding, "blocked", digestOf([]byte("body")), func() error { t.Error("failed recovery reached owner CAS"); return nil }); !errors.Is(err, errAssistantRunRecovery) {
		t.Fatalf("failed recovery admitted work %v", err)
	}
	retry, err := recovered.Acquire(ctx)
	assistantRunMust(t, err)
	defer retry.Close()
	if reopened.assistantRunRecoveryFailure() != nil {
		t.Fatal("verified reacquisition did not clear recovery error")
	}
	saved, err := recovered.Read(ctx, binding, second.ID)
	assistantRunMust(t, err)
	if saved.State != "interrupted" {
		t.Fatal("uncertain recovery lost retained admission")
	}
}

func TestAssistantRunStorageBlankStartupNoLedger(t *testing.T) {
	server, _, _ := assistantServer(t)
	root, err := server.storageSettingsRoot()
	assistantRunMust(t, err)
	defer root.Close()
	if _, err = root.Lstat(dataLocationName); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blank startup materialized data pointer %v", err)
	}
	entries, err := storageEntries(root)
	assistantRunMust(t, err)
	for _, entry := range entries {
		if assistantRunFileName.MatchString(entry.Name()) {
			t.Fatal("blank startup materialized run ledger")
		}
	}
}

func TestAssistantRunStoragePendingRestoreAndCustody(t *testing.T) {
	for _, state := range []string{"awaiting-input", "awaiting-client", "paused-budget"} {
		t.Run(state, func(t *testing.T) {
			server, ts, store, binding, record, lease, writer := assistantRunHTTPFixture(t)
			ctx := context.Background()
			assistantRunMust(t, writer.Intent(ctx, assistantRunOp("pending")))
			saved, err := store.Read(ctx, binding, record.ID)
			assistantRunMust(t, err)
			cp := *saved.Checkpoint
			cp.Status = "stopped"
			if state == "paused-budget" {
				cp.Status = "paused"
			}
			assistantRunMust(t, writer.Checkpoint(ctx, cp))
			assistantRunMust(t, writer.State(ctx, state))
			assistantRunMust(t, lease.Close())
			backup := backupBytes(t, ts)
			before := storageGet(t, ts)
			code, body := restoreBackup(t, ts, moveChatData{Path: filepath.Join(t.TempDir(), "historical"), Revision: before.Revision}, backup)
			if code != 200 {
				t.Fatalf("restore %d %s", code, body)
			}
			held, err := server.assistantRuns().Read(ctx, binding, record.ID)
			assistantRunMust(t, err)
			if !held.Historical || held.State != "interrupted" || held.Checkpoint.Status != "stopped" || held.Operations[0].State != "not-started" {
				t.Fatal("paused imported run remained actionable")
			}
		})
	}
	for _, kind := range []string{"symlink", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			server, ts, store, _, _, lease, _ := assistantRunHTTPFixture(t)
			assistantRunMust(t, lease.Close())
			root, err := server.openChatData()
			assistantRunMust(t, err)
			path := filepath.Join(root.location.Path, store.name)
			root.root.Close()
			if kind == "symlink" {
				original, err := os.ReadFile(path)
				assistantRunMust(t, err)
				target := filepath.Join(t.TempDir(), "outside")
				assistantRunMust(t, os.WriteFile(target, original, 0600))
				assistantRunMust(t, os.Remove(path))
				assistantRunMust(t, os.Symlink(target, path))
			} else {
				assistantRunMust(t, os.Chmod(path, 0644))
			}
			if code := assistantRunBackupCode(t, ts, true); code == 200 {
				t.Fatal("unsafe ledger entered backup")
			}
		})
	}
}

func TestAssistantRunStorageSiblingRelinkAndConflict(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "sibling only", true: "destination ledger"}[conflict], func(t *testing.T) {
			old, _, store, binding, record, lease, _ := assistantRunHTTPFixture(t)
			assistantRunMust(t, lease.Close())
			moved, mts, _ := assistantServerIn(t, old.configDir)
			stale := moved.assistantRuns()
			if conflict {
				raw := assistantRunRaw(t, store)
				ledger, err := decodeAssistantRunStorage(store.name, raw)
				assistantRunMust(t, err)
				ledger.Project = stale.project
				ledger.Runs[0].Binding.Project = stale.project
				raw, _ = json.Marshal(ledger)
				root, err := moved.openChatData()
				assistantRunMust(t, err)
				assistantRunMust(t, writePrivateData(root.root, stale.name, raw))
				root.root.Close()
			}
			request := projectHistoryRequest{PreviousProject: old.projectDir}
			code, preview, body := historyRequest(t, mts, "preview", request)
			if conflict {
				if code != 409 {
					t.Fatalf("ledger merged during relink %d %s", code, body)
				}
				return
			}
			if code != 200 || preview.ChatCount != 0 || preview.SourceRevision != "absent" {
				t.Fatalf("sibling preview %d %s", code, body)
			}
			request.SourceRevision = preview.SourceRevision
			request.BindingsRevision = preview.BindingsRevision
			if code, _, body = historyRequest(t, mts, "relink", request); code != 200 {
				t.Fatalf("sibling relink %d %s", code, body)
			}
			linked := moved.assistantRuns()
			if linked.name != store.name {
				t.Fatal("relink orphaned original namespace")
			}
			if _, err := linked.Read(context.Background(), binding, record.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := stale.Read(context.Background(), binding, record.ID); !errors.Is(err, errAssistantRunScope) {
				t.Fatalf("pre-relink store trusted stale namespace %v", err)
			}
		})
	}
}
