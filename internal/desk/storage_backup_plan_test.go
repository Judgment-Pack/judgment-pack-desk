package desk

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func transferPlanArchive(t *testing.T, names []string, payloads [][]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	manifest := backupManifest{Version: 1, CreatedAt: "2026-10-31T00:00:00Z", Files: []backupEntry{}}
	for i, name := range names {
		item, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = item.Write(payloads[i]); err != nil {
			t.Fatal(err)
		}
		manifest.Files = append(manifest.Files, backupEntry{Name: name, Bytes: int64(len(payloads[i])), SHA256: digestOf(payloads[i])})
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	item, err := archive.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = item.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestTransferPlanReaderAtFileParity(t *testing.T) {
	name := "conversations-" + strings.Repeat("a", 64) + ".json"
	good := transferPlanArchive(t, []string{name}, [][]byte{[]byte(`{"version":1,"chats":[]}`)})
	duplicate := transferPlanArchive(t, []string{name, name}, [][]byte{[]byte(`{"version":1,"chats":[]}`), []byte(`{"version":1,"chats":[]}`)})
	badDirectory := bytes.Clone(good)
	binary.LittleEndian.PutUint32(badDirectory[len(badDirectory)-10:len(badDirectory)-6], 3<<20)
	for _, tc := range []struct {
		name  string
		raw   []byte
		valid bool
	}{
		{"valid", good, true}, {"incomplete", []byte("bad ZIP"), false}, {"directory", badDirectory, false}, {"duplicate", duplicate, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.zip")
			if err := os.WriteFile(path, tc.raw, 0600); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			fa, fm, fe := validateChatBackup(file)
			ra, rm, re := validateChatBackupReader(t.Context(), bytes.NewReader(tc.raw), int64(len(tc.raw)))
			if (fe == nil) != tc.valid || (re == nil) != tc.valid {
				t.Fatalf("file=%v ReaderAt=%v", fe, re)
			}
			if tc.valid && (!reflect.DeepEqual(fm, rm) || len(fa.File) != len(ra.File)) {
				t.Fatal("wrapper/core manifest differs")
			}
			if !tc.valid && fe.Error() != re.Error() {
				t.Fatalf("refusal differs: %v / %v", fe, re)
			}
		})
	}
}

// Fixture values are authored here; expected output is not captured from a
// transformation run. Catalog declarations supply the existing valid schema.
func transferPlanHistoricalFixtures(t *testing.T) (string, []byte, []byte, string, []byte, []byte) {
	t.Helper()
	project := strings.Repeat("a", 64)
	var snapshot assistantSourceSnapshot
	for _, row := range assistantSourceBaseCatalog() {
		if row.AdapterID == "desk-research-workflow" {
			snapshot = row
		}
	}
	snapshot.Status = "review-required"
	snapshot.Configuration = assistantSourceConfiguration{Target: "Desk private research workflow", DeskDigest: project, OwnerDigest: project}
	snapshot.ConfigurationDigest = assistantSourceConfigurationDigest(snapshot, project)
	identity, _ := json.Marshal(map[string]string{"kind": "connection", "label": "Desk research workflow " + snapshot.ConfigurationDigest})
	if err := json.Unmarshal(identity, &snapshot.Identity); err != nil {
		t.Fatal(err)
	}
	snapshot.IdentityDigest = runcontext.IdentityDigest(*snapshot.Identity)
	scope := assistantSourceScope{Project: project, ChatID: "chat_one", SessionScope: project, Principal: project, PolicyDigest: project}
	sources := assistantSourceLedger{Version: 1, Project: project, Reviews: []assistantSourceReview{{Version: 1, ID: strings.Repeat("b", 32), Revision: 1, Scope: scope, Snapshot: snapshot}}, Approvals: []assistantSourceApproval{}}
	sourceRaw, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	sourceWant := bytes.Replace(sourceRaw, []byte(`"consumed":false}`), []byte(`"consumed":false,"historical":true}`), 1)
	binding := assistantRunBinding{Principal: project, Session: project, Policy: project, Project: project, Owner: assistantRunOwner{Kind: "chat", ID: "chat_one", Revision: project}, RequestID: "request-one", RequestDigest: project, ConnectionID: "legacy-api", ConnectionRevision: project, ProfileRevision: project, Model: "fixture", Phase: "work", Unit: "model-requests", ExecutionKey: project}
	ledger := assistantRunLedger{Version: 1, Project: project, Runs: []assistantRunRecord{{ID: "arun_" + strings.Repeat("c", 32), Revision: 1, Binding: binding, AdmissionID: "admission-one", BodyDigest: project, State: "running", Operations: []assistantRunOperation{}, Events: []assistantRunEvent{}}}}
	runRaw, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	ledger.Runs[0].Historical = true
	ledger.Runs[0].Revision = 2
	ledger.Runs[0].State = "interrupted"
	runWant, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	return assistantSourceLedgerName(project), sourceRaw, sourceWant, "assistant-runs-" + project + ".json", runRaw, runWant
}

func TestTransferPlanExactHistoricalSizing(t *testing.T) {
	sourceName, source, sourceWant, runName, run, runWant := transferPlanHistoricalFixtures(t)
	raw := transferPlanArchive(t, []string{sourceName, runName}, [][]byte{source, run})
	calls := 0
	plan, err := planChatBackup(t.Context(), bytes.NewReader(raw), int64(len(raw)), func(name string, data []byte) error {
		calls++
		want := source
		if name == runName {
			want = run
		}
		if !bytes.Equal(data, want) {
			t.Fatal("authorization saw transformed bytes")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := chatTransferPlan{Files: []chatTransferOutput{{Name: sourceName, Bytes: int64(len(sourceWant)), SHA256: digestOf(sourceWant)}, {Name: runName, Bytes: int64(len(runWant)), SHA256: digestOf(runWant)}}, Bytes: int64(len(sourceWant) + len(runWant))}
	if calls != 2 || !reflect.DeepEqual(plan, want) {
		t.Fatalf("plan=%+v want=%+v calls=%d", plan, want, calls)
	}
	for _, tc := range []struct {
		name      string
		raw, want []byte
	}{{sourceName, source, sourceWant}, {runName, run, run}} {
		got, err := transformChatTransfer(tc.name, tc.raw, chatTransferMove)
		if err != nil || !bytes.Equal(got, tc.want) {
			t.Fatalf("move historical semantics: %s %v", tc.name, err)
		}
	}
	sentinel := errors.New("authorization refused")
	if _, err := planChatBackup(t.Context(), bytes.NewReader(raw), int64(len(raw)), func(string, []byte) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal("lost authorization refusal", err)
	}
	corrupt := bytes.Clone(source)
	corrupt[len(corrupt)-1] = '!'
	bad := transferPlanArchive(t, []string{sourceName}, [][]byte{corrupt})
	if _, err := planChatBackup(t.Context(), bytes.NewReader(bad), int64(len(bad))); err == nil {
		t.Fatal("invalid object accepted")
	}
}

type transferPlanReadProbe struct {
	input  *bytes.Reader
	offset int64
	reads  int
	cancel context.CancelFunc
}

func (p *transferPlanReadProbe) ReadAt(b []byte, off int64) (int, error) {
	if off == p.offset {
		p.reads++
		if p.cancel != nil {
			p.cancel()
		}
	}
	return p.input.ReadAt(b, off)
}

func TestTransferPlanOneEntryAndCancellation(t *testing.T) {
	names := []string{"conversations-" + strings.Repeat("a", 64) + ".json", "conversations-" + strings.Repeat("b", 64) + ".json"}
	payload := []byte(`{"version":1,"chats":[]}`)
	raw := transferPlanArchive(t, names, [][]byte{payload, payload})
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	second, err := archive.File[1].DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	probe := &transferPlanReadProbe{input: bytes.NewReader(raw), offset: second}
	visited := 0
	err = walkChatBackup(t.Context(), probe, int64(len(raw)), func(name string, data []byte) error {
		visited++
		if visited == 1 && probe.reads != 0 {
			t.Fatal("second payload read before first consumed")
		}
		if !bytes.Equal(data, payload) {
			t.Fatal("changed payload")
		}
		return nil
	})
	if err != nil || visited != 2 || probe.reads == 0 {
		t.Fatal("entry walk", visited, probe.reads, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	probe = &transferPlanReadProbe{input: bytes.NewReader(raw), offset: second, cancel: cancel}
	if _, err := planChatBackup(ctx, probe, int64(len(raw))); !errors.Is(err, context.Canceled) {
		t.Fatal("read cancellation lost", err)
	}
	cancel()
	ctx, cancel = context.WithCancel(t.Context())
	visited = 0
	err = walkChatBackup(ctx, bytes.NewReader(raw), int64(len(raw)), func(string, []byte) error { visited++; cancel(); return nil })
	if !errors.Is(err, context.Canceled) || visited != 1 {
		t.Fatal("iteration cancellation lost", visited, err)
	}
	cancel()
	if _, err := planChatBackup(ctx, bytes.NewReader(raw), int64(len(raw))); !errors.Is(err, context.Canceled) {
		t.Fatal("pre-cancel lost", err)
	}
}

func TestTransferPlanArchiveFinalByteCap(t *testing.T) {
	// An empty ZIP emits its 22-byte EOCD only at Close. Seed the count to
	// exercise the actual archive cap without allocating a 130 MiB fixture.
	for _, spare := range []int64{22, 21} {
		var sink bytes.Buffer
		bounded := &backupArchiveWriter{ctx: t.Context(), out: &sink, limit: maxBackupArchive, bytes: maxBackupArchive - spare}
		archive := zip.NewWriter(bounded)
		err := archive.Close()
		if spare == 22 && (err != nil || bounded.bytes != maxBackupArchive || sink.Len() != 22) {
			t.Fatal("exact archive cap", bounded.bytes, err)
		}
		if spare == 21 && (err == nil || sink.Len() != 0 || bounded.bytes != maxBackupArchive-spare) {
			t.Fatal("directory crossed cap", bounded.bytes, err)
		}
	}
	short := &backupArchiveWriter{ctx: t.Context(), out: transferPlanShortWriter{}, limit: maxBackupArchive}
	if _, err := short.Write([]byte("two")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("short write lost", err)
	}
}

type transferPlanShortWriter struct{}

func (transferPlanShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestTransferPlanFrozenArchiveCountAndFileWrapper(t *testing.T) {
	dir := t.TempDir()
	name := "conversations-" + strings.Repeat("a", 64) + ".json"
	payload := []byte(`{"version":1,"chats":[]}`)
	if err := os.WriteFile(filepath.Join(dir, name), payload, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	store := &chatDataStore{root: root, legacy: true}
	const created = "2026-10-31T00:00:00Z"
	count, err := countChatBackup(t.Context(), store, created)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = writeChatBackup(t.Context(), store, &out, created); err != nil {
		t.Fatal(err)
	}
	if count != int64(out.Len()) {
		t.Fatal("count differs", count, out.Len())
	}
	_, manifest, err := validateChatBackupReader(t.Context(), bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil || manifest.CreatedAt != created {
		t.Fatal("timestamp changed", manifest.CreatedAt, err)
	}
	file, err := os.CreateTemp(t.TempDir(), "wrapper-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err = buildChatBackup(t.Context(), store, file); err != nil {
		t.Fatal(err)
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != 0 {
		t.Fatal("file wrapper did not rewind", position, err)
	}
	if _, _, err = validateChatBackup(file); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != name {
		t.Fatal("planning created source objects", entries, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("planning changed source", err)
	}
}

func TestTransferPlanMoveExactSizingAndNoEffects(t *testing.T) {
	sourceName, source, sourceWant, runName, run, _ := transferPlanHistoricalFixtures(t)
	dir := t.TempDir()
	for name, data := range map[string][]byte{sourceName: source, runName: run} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	plan, err := planChatData(t.Context(), &chatDataStore{root: root, legacy: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{sourceName: sourceWant, runName: run}
	if len(plan.Files) != 2 || plan.Bytes != int64(len(sourceWant)+len(run)) {
		t.Fatal("move size", plan)
	}
	for _, file := range plan.Files {
		expected, ok := want[file.Name]
		if !ok || file.Bytes != int64(len(expected)) || file.SHA256 != digestOf(expected) {
			t.Fatal("move output", file)
		}
	}
	for name, data := range map[string][]byte{sourceName: source, runName: run} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("source mutated", name, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatal("planning created objects", entries, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := planChatData(ctx, &chatDataStore{root: root, legacy: true}); !errors.Is(err, context.Canceled) {
		t.Fatal("move cancellation", err)
	}
}
