package desk

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// These fixtures enter the actual registered preparation and Server Close
// paths. They issue no native qualification or capacity and never substitute a
// serialized comparator fixture for the actual effect route's Record3 gate.
type storageTransferCloseBody struct {
	io.Reader
	mu          sync.Mutex
	closes      int
	err         error
	closeReader io.Closer
}

func (b *storageTransferCloseBody) Close() error {
	b.mu.Lock()
	b.closes++
	b.mu.Unlock()
	if b.closeReader != nil {
		return errors.Join(b.err, b.closeReader.Close())
	}
	return b.err
}
func (b *storageTransferCloseBody) count() int { b.mu.Lock(); defer b.mu.Unlock(); return b.closes }

func TestStorageTransferRestorePreparationChecksBodyClose(t *testing.T) {
	requireStorageLine(t)
	s, err := New(ownerActualConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	closeFault := errors.New("handwritten body close failure")
	var data bytes.Buffer
	writer := multipart.NewWriter(&data)
	part, err := writer.CreateFormField("backup")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte("unexpected first field")); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	body := &storageTransferCloseBody{Reader: bytes.NewReader(data.Bytes()), err: closeFault}
	request := httptest.NewRequest("POST", "http://localhost/api/storage/restore", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	p, err := s.prepareOriginalChatTransfer(request, storageTransferRestore)
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	_, _, _, err = s.readOriginalChatRestore(p)
	if err == nil {
		t.Fatal("backup preceding settings was accepted")
	}
	err = p.finish(err)
	if !errors.Is(err, closeFault) || body.count() != 1 {
		t.Fatal("actual body Close result was lost or repeated", err, body.count())
	}
	if len(chatTransferProcessPermit) != 0 {
		t.Fatal("process permit survived actual finalization")
	}
	if err = s.Close(); !errors.Is(err, closeFault) {
		t.Fatal("Server Close did not retain body Close failure", err)
	}
}

func TestStorageTransferRestoreCancellationClosesOriginalBodyAndDrains(t *testing.T) {
	requireStorageLine(t)
	s, err := New(ownerActualConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	defer writer.Close()
	body := &storageTransferCloseBody{Reader: reader, closeReader: reader}
	request := httptest.NewRequest("POST", "http://localhost/api/storage/restore", body)
	request.Header.Set("Content-Type", "multipart/form-data; boundary=held-body")
	p, err := s.prepareOriginalChatTransfer(request, storageTransferRestore)
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, _, _, e := s.readOriginalChatRestore(p); result <- e }()
	// Close is a revoker/joiner. The lexical preparation remains responsible
	// for finalization and returning its one process permit after body read exits.
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case e := <-result:
		if e == nil {
			t.Fatal("canceled body read succeeded")
		}
		err = e
	case <-time.After(5 * time.Second):
		t.Fatal("original body was not interrupted by Close")
	}
	select {
	case e := <-closed:
		t.Fatal("Close returned before lexical finalization", e)
	default:
	}
	err = p.finish(err)
	select {
	case e := <-closed:
		if e == nil {
			t.Fatal("Close discarded canceled request result")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close failed to drain original preparation")
	}
	if body.count() != 1 || len(chatTransferProcessPermit) != 0 {
		t.Fatal("cancellation lost exact body/permit finalization", body.count())
	}
}

func TestStorageTransferRestorePreparationPermitRefusesWithoutQueue(t *testing.T) {
	requireStorageLine(t)
	first, err := New(ownerActualConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(ownerActualConfig(t))
	if err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "http://localhost/api/storage/restore", strings.NewReader("unretained"))
	p, err := first.prepareOriginalChatTransfer(request, storageTransferRestore)
	if err != nil {
		_ = first.Close()
		_ = second.Close()
		t.Fatal(err)
	}
	if _, err = second.prepareOriginalChatTransfer(httptest.NewRequest("POST", "http://localhost/api/storage/restore", strings.NewReader("unretained")), storageTransferRestore); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("second owner queued restore retention", err)
	}
	if err = p.finish(nil); err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	if err = second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStorageTransferQualifiedMoveRestoreExportRoutes(t *testing.T) {
	requireStorageLine(t)
	records, err := storageEnrollmentNativeRecords(storageEnrollmentInstalledNativeRecords)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Skip("requires actual installed Record3 matching this exact source candidate; no qualification is fabricated")
	}
	cfg := ownerActualConfig(t)
	s := ownerActualNew(t, cfg)
	server := httptest.NewServer(s)
	defer server.Close()
	before := storageGet(t, server)
	target := filepath.Join(t.TempDir(), "external", "nested")
	code, after, body := storageRequest(t, server, "/api/storage/move", moveChatData{Path: target, Revision: before.Revision})
	if code != 200 || after.Path != target {
		t.Fatalf("actual qualified move: %d %+v %s", code, after, body)
	}
	archive := backupBytes(t, server)
	destination := filepath.Join(t.TempDir(), "restored", "nested")
	code, body = restoreBackup(t, server, moveChatData{Path: destination, Revision: after.Revision}, archive)
	if code != 200 {
		t.Fatalf("actual qualified restore: %d %s", code, body)
	}
	if got := storageGet(t, server); got.Path != destination {
		t.Fatal("final durable selection was not exposed after completion", got)
	}
	// Export's response bytes are archive integrity evidence, never proof of
	// client receipt or permission to remove the preserved source.
	if len(archive) == 0 {
		t.Fatal("qualified synchronous export emitted no archive")
	}
}

// This matrix is component evidence for the real registered multipart
// preparation, not evidence that a qualified restore reserved or wrote data.
func storageTransferMultipart(t *testing.T, settings string, archive []byte, extra bool) ([]byte, string) {
	t.Helper()
	var out bytes.Buffer
	form := multipart.NewWriter(&out)
	if err := form.SetBoundary("handwritten-transfer-upload"); err != nil {
		t.Fatal(err)
	}
	if err := form.WriteField("settings", settings); err != nil {
		t.Fatal(err)
	}
	part, err := form.CreateFormFile("backup", "backup.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(archive); err != nil {
		t.Fatal(err)
	}
	if extra {
		if err = form.WriteField("extra", "unadmitted tail"); err != nil {
			t.Fatal(err)
		}
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes(), form.FormDataContentType()
}

func storageTransferUploadArchive(t *testing.T, variant string) []byte {
	t.Helper()
	name := "conversations-" + strings.Repeat("a", 64) + ".json"
	payload := []byte(`{"version":1,"chats":[]}`)
	manifest := backupManifest{Version: 1, CreatedAt: "2026-10-31T00:00:00Z", Files: []backupEntry{}}
	if variant != "empty" {
		manifest.Files = append(manifest.Files, backupEntry{Name: name, Bytes: int64(len(payload)), SHA256: digestOf(payload)})
	}
	if variant == "sha" {
		manifest.Files[0].SHA256 = strings.Repeat("0", 64)
	}
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	if variant != "empty" {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		if variant == "type" {
			header.SetMode(os.ModeSymlink | 0600)
		}
		part, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	part, err := archive.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

type storageTransferTailError struct{ err error }

func (r storageTransferTailError) Read([]byte) (int, error) { return 0, r.err }

type storageTransferRepeatedByte struct{}

func (storageTransferRepeatedByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestStorageTransferRestorePreparationUploadMatrix(t *testing.T) {
	requireStorageLine(t)
	cfg := ownerActualConfig(t)
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		// Invalid uploads are deliberately retained by the original registry.
		if e := s.Close(); e == nil {
			t.Error("Close discarded the preparation matrix's failures")
		}
	}()
	settings, err := json.Marshal(moveChatData{Path: filepath.Join(cfg.ProjectDir, ".desk-private", "restored"), Revision: "frozen-request"})
	if err != nil {
		t.Fatal(err)
	}
	good := storageTransferUploadArchive(t, "empty")
	valid, contentType := storageTransferMultipart(t, string(settings), good, false)
	tailFault := errors.New("handwritten multipart tail read failure")
	// Replacing the final boundary with a non-final boundary forces NextPart
	// to consume the actual tail; a valid final boundary may legally hide epilogue.
	openTail := append([]byte(nil), bytes.TrimSuffix(valid, []byte("--handwritten-transfer-upload--\r\n"))...)
	openTail = append(openTail, []byte("--handwritten-transfer-upload\r\n")...)
	oversizePrefix, _ := storageTransferMultipart(t, string(settings), nil, false)
	oversizePrefix = bytes.TrimSuffix(oversizePrefix, []byte("\r\n--handwritten-transfer-upload--\r\n"))
	var oversizedRead int64
	cases := []struct {
		name  string
		body  func() io.Reader
		valid bool
		want  error
	}{
		{"valid-sealed-empty-archive", func() io.Reader { return bytes.NewReader(valid) }, true, nil},
		{"settings-over-16KiB", func() io.Reader {
			raw, _ := storageTransferMultipart(t, strings.Repeat(" ", (16<<10)+1), good, false)
			return bytes.NewReader(raw)
		}, false, nil},
		{"settings-json-depth", func() io.Reader {
			raw, _ := storageTransferMultipart(t, `{"path":`+strings.Repeat("[", 65)+`0`+strings.Repeat("]", 65)+`,"revision":"frozen-request"}`, good, false)
			return bytes.NewReader(raw)
		}, false, nil},
		{"backup-sha-mismatch", func() io.Reader {
			raw, _ := storageTransferMultipart(t, string(settings), storageTransferUploadArchive(t, "sha"), false)
			return bytes.NewReader(raw)
		}, false, nil},
		{"backup-unsafe-file-type", func() io.Reader {
			raw, _ := storageTransferMultipart(t, string(settings), storageTransferUploadArchive(t, "type"), false)
			return bytes.NewReader(raw)
		}, false, nil},
		{"backup-over-archive-cap", func() io.Reader {
			return io.MultiReader(bytes.NewReader(oversizePrefix), &storageTransferCountReader{reader: io.LimitReader(storageTransferRepeatedByte{}, maxBackupArchive+1), count: &oversizedRead})
		}, false, nil},
		{"unexpected-extra-part", func() io.Reader {
			raw, _ := storageTransferMultipart(t, string(settings), good, true)
			return bytes.NewReader(raw)
		}, false, nil},
		{"truncated-final-boundary", func() io.Reader { return bytes.NewReader(valid[:len(valid)-8]) }, false, nil},
		{"tail-read-failure", func() io.Reader {
			return io.MultiReader(bytes.NewReader(openTail), storageTransferTailError{tailFault})
		}, false, tailFault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			beforeConfig, beforeProject := storageTestTree(t, cfg.DeskConfigDir), storageTestTree(t, cfg.ProjectDir)
			beforeControl := ownerReviewDurableState(t, s.storageConstruction.original.config)
			body := &storageTransferCloseBody{Reader: tc.body()}
			request := httptest.NewRequest("POST", "http://localhost/api/storage/restore", body)
			request.Header.Set("Content-Type", contentType)
			p, e := s.prepareOriginalChatTransfer(request, storageTransferRestore)
			if e != nil {
				t.Fatal(e)
			}
			_, input, plan, e := s.readOriginalChatRestore(p)
			if (e == nil) != tc.valid || tc.want != nil && !errors.Is(e, tc.want) {
				t.Errorf("sealed upload acceptance: valid=%v error=%v", tc.valid, e)
			}
			if tc.valid && (!bytes.Equal(input, good) || len(plan.Files) != 0 || plan.Bytes != 0) {
				t.Error("valid sealed archive was not returned exactly")
			}
			if !tc.valid && (input != nil || len(plan.Files) != 0) {
				t.Error("invalid upload escaped a retained input or output plan")
			}
			if p.epoch != nil || p.operation != nil || p.admitted || !p.permit {
				t.Error("multipart preparation acquired effect authority or lost its original permit")
			}
			_ = p.finish(e)
			if body.count() != 1 || len(chatTransferProcessPermit) != 0 {
				t.Error("checked body Close/process permit was not drained exactly once")
			}
			if !reflect.DeepEqual(beforeControl, ownerReviewDurableState(t, s.storageConstruction.original.config)) || !reflect.DeepEqual(beforeConfig, storageTestTree(t, cfg.DeskConfigDir)) || !reflect.DeepEqual(beforeProject, storageTestTree(t, cfg.ProjectDir)) {
				t.Error("RAM-only multipart preparation changed the original filesystem/Q controls")
			}
		})
	}
	if oversizedRead != maxBackupArchive+1 {
		t.Fatal("archive cap fixture did not cross the real byte limit", oversizedRead)
	}
}

type storageTransferCountReader struct {
	reader io.Reader
	count  *int64
}

func (r *storageTransferCountReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	*r.count += int64(n)
	return n, err
}

// This body pauses after a complete valid ZIP but before its multipart seal.
// Its Close unblocks the exact entered Read without substituting another body.
type storageTransferPausedBody struct {
	prefix              *bytes.Reader
	entered, closed     chan struct{}
	readOnce, closeOnce sync.Once
	mu                  sync.Mutex
	closes              int
}

func (b *storageTransferPausedBody) Read(p []byte) (int, error) {
	if b.prefix.Len() != 0 {
		return b.prefix.Read(p)
	}
	b.readOnce.Do(func() { close(b.entered) })
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *storageTransferPausedBody) Close() error {
	b.mu.Lock()
	b.closes++
	b.mu.Unlock()
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

func TestStorageTransferRestoreCanceledValidUploadKeepsOriginalPreparation(t *testing.T) {
	requireStorageLine(t)
	cfg := ownerActualConfig(t)
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := json.Marshal(moveChatData{Path: filepath.Join(cfg.ProjectDir, ".desk-private", "restored"), Revision: "frozen-request"})
	if err != nil {
		t.Fatal(err)
	}
	raw, contentType := storageTransferMultipart(t, string(settings), storageTransferUploadArchive(t, "empty"), false)
	prefix := bytes.TrimSuffix(raw, []byte("\r\n--handwritten-transfer-upload--\r\n"))
	body := &storageTransferPausedBody{prefix: bytes.NewReader(prefix), entered: make(chan struct{}), closed: make(chan struct{})}
	request := httptest.NewRequest("POST", "http://localhost/api/storage/restore", body)
	request.Header.Set("Content-Type", contentType)
	p, err := s.prepareOriginalChatTransfer(request, storageTransferRestore)
	if err != nil {
		_ = s.Close()
		t.Fatal(err)
	}
	beforeConfig, beforeProject := storageTestTree(t, cfg.DeskConfigDir), storageTestTree(t, cfg.ProjectDir)
	result := make(chan error, 1)
	go func() { _, _, _, e := s.readOriginalChatRestore(p); result <- e }()
	select {
	case <-body.entered:
	case <-time.After(5 * time.Second):
		_ = body.Close()
		t.Fatal("valid upload never entered the original seal-waiting body Read")
	}
	if p.epoch != nil || p.operation != nil || p.admitted || len(chatTransferProcessPermit) != 1 {
		t.Error("network wait acquired effect lifetime or lost the one original permit")
	}
	// Exercise the actual existing private/writes boundaries while body I/O is
	// paused. The preparation cannot hold either one during a network wait.
	if !s.writes.TryLock() {
		t.Error("network wait retained writes mutex")
	} else {
		s.writes.Unlock()
	}
	// These existing lock primitives establish availability only. A whole
	// ordinary checkpoint additionally needs native history enrollment and is
	// deliberately not claimed by this preparation component fixture.
	release, e := s.privateDataTurns.tryAcquire(t.Context())
	if e != nil {
		t.Error("network wait retained the private FIFO turn", e)
	} else {
		for _, name := range []string{".data-work.lock", ".data.lock"} {
			lock, lockErr := s.privateDataFileLock(name, true)
			if lockErr != nil {
				t.Error("network wait retained original lock", name, lockErr)
			} else if lockErr = lock.Close(); lockErr != nil {
				t.Error(lockErr)
			}
		}
		release()
	}
	if _, e = s.prepareOriginalChatTransfer(httptest.NewRequest("GET", "http://localhost/api/storage/backup", nil), storageTransferExport); !errors.Is(e, storagebudget.ErrBusy) {
		t.Error("second transfer did not promptly refuse the retained valid upload", e)
	}
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case e = <-result:
		if e == nil {
			t.Error("canceled unsealed valid upload was accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not interrupt the original body borrower")
	}
	select {
	case e := <-closed:
		t.Fatal("Close returned before original lexical finalization", e)
	default:
	}
	if !reflect.DeepEqual(beforeConfig, storageTestTree(t, cfg.DeskConfigDir)) || !reflect.DeepEqual(beforeProject, storageTestTree(t, cfg.ProjectDir)) {
		t.Error("canceled valid upload created filesystem growth")
	}
	_ = p.finish(e)
	select {
	case e = <-closed:
		if e == nil {
			t.Error("Close lost original canceled upload failure")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not drain the finalized original preparation")
	}
	body.mu.Lock()
	closes := body.closes
	body.mu.Unlock()
	if closes != 1 || len(chatTransferProcessPermit) != 0 {
		t.Fatal("valid-upload cancellation did not Close/drain once", closes)
	}
}

// These helpers enter the existing authenticated handlers or the original
// lexical operation callback. They install no catalog, qualification or permit.
func storageTransferNativeRequest(t *testing.T, s *Server, server *httptest.Server, method, endpoint string, raw []byte, contentType string, contexts ...context.Context) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, server.URL+endpoint, bytes.NewReader(raw)).WithContext(t.Context())
	if len(contexts) != 0 {
		r = r.WithContext(contexts[0])
	}
	pageBearer(privateWorkspaceTestSession(t, server))(r)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("qualified %s %s: %d %s", method, endpoint, w.Code, w.Body.String())
	}
	return w
}

func storageTransferNativeSelection(t *testing.T, s *Server, server *httptest.Server, contexts ...context.Context) storageStatus {
	t.Helper()
	w := storageTransferNativeRequest(t, s, server, "GET", "/api/storage", nil, "", contexts...)
	var out storageStatus
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func storageTransferNativeMove(t *testing.T, s *Server, server *httptest.Server, path string, contexts ...context.Context) {
	t.Helper()
	before := storageTransferNativeSelection(t, s, server, contexts...)
	raw, err := json.Marshal(moveChatData{Path: path, Revision: before.Revision})
	if err != nil {
		t.Fatal(err)
	}
	storageTransferNativeRequest(t, s, server, "POST", "/api/storage/move", raw, "application/json", contexts...)
}

func storageTransferNativeRestore(t *testing.T, s *Server, server *httptest.Server, path string, archive []byte) {
	t.Helper()
	before := storageTransferNativeSelection(t, s, server)
	raw, err := json.Marshal(moveChatData{Path: path, Revision: before.Revision})
	if err != nil {
		t.Fatal(err)
	}
	body, contentType := storageTransferMultipart(t, string(raw), archive, false)
	storageTransferNativeRequest(t, s, server, "POST", "/api/storage/restore", body, contentType)
}

func storageTransferNativeCurrent(t *testing.T, s *Server) *chatTransferOperation {
	t.Helper()
	registry := s.storageConstruction.owner.transfers
	registry.mu.Lock()
	p := registry.current
	registry.mu.Unlock()
	if p == nil {
		t.Fatal("no original registered preparation")
	}
	p.mu.Lock()
	op := p.operation
	p.mu.Unlock()
	if op == nil || op.self != op || op.preparation != p || !p.admitted || op.completion == nil {
		t.Fatal("no original admitted transfer/completion")
	}
	return op
}

func storageTransferNativeFinal(t *testing.T, s *Server, op *chatTransferOperation, pointer bool) {
	t.Helper()
	if op == nil || !op.reserveReturned || op.reserveError != nil || op.record.InitialToken == (storagebudget.Token{}) || !op.finished || !op.preparation.finished || !op.preparation.completed || op.record.Phase != storageTransferRetired || op.appliedProof == nil || !storageTransferApplied(op.record, op.catalog, *op.appliedProof) || op.next != len(op.board) || len(chatTransferProcessPermit) != 0 {
		t.Fatal("original one-ticket transfer did not finish through applied Q and retirement")
	}
	pointers := 0
	for i, step := range op.board {
		if step.kind == storageTransferPointerOrdinal {
			pointers++
		}
		if op.results[i] != 2 {
			t.Error("normal original effect did not return exactly once", i, op.results[i])
		}
	}
	want := 0
	if pointer {
		want = 1
		if op.record.PointerState != 2 || !op.tailCompleted || op.preparation.epoch.completedEvidence == nil || storageTransferSelectionRelation(op.record, op.catalog, op.preparation.epoch.completedEvidence.Inner.Catalog) != nil {
			t.Error("pointer transfer lacked its original final P selection")
		}
	}
	if pointers != want {
		t.Error("unexpected pointer ordinal count", pointers, want)
	}
	if op.stage != nil || op.path != nil || len(op.created) != 0 || op.observation != nil || !op.completion.finished || op.oldPointerErr != nil {
		t.Error("checked completion left an original data/pass description or Close error")
	}
	qPath := filepath.Join(s.configDir, storageAnchorName)
	before := storageTransferQFiles(t, qPath)
	next := op.next
	if _, err := op.enterOriginalTransferOrdinal(t.Context(), storageTransferPointerOrdinal, 0); err == nil {
		t.Error("finished original operation replayed an effect")
	}
	if !pointer {
		if err := serveOriginalChatTransferExport(op); err == nil {
			t.Error("finished original export replayed serving")
		}
	}
	if op.next != next || !reflect.DeepEqual(before, storageTransferQFiles(t, qPath)) {
		t.Error("refused replay changed Q or the original ordinal")
	}
	matches := 0
	for _, slot := range op.appliedProof.Slots {
		if slot.Ticket.Workflow == storageTransferWorkflow && slot.Ticket.Operation == op.request.Operation {
			matches++
			if slot.Ticket.Token.ID != op.record.InitialToken.ID || slot.Ticket.Token.Sequence != op.record.InitialToken.Sequence {
				t.Error("transfer used another original reservation")
			}
		}
	}
	if matches != 1 {
		t.Error("applied Q did not retain exactly one original transfer ticket", matches)
	}
}

func storageTransferNativeRetiredView(t *testing.T, s *Server, anchor, next string) storagebudget.ID {
	t.Helper()
	cell := s.storageConstruction.ref.cell
	cell.mu.Lock()
	endpoint, catalog := cell.enrollmentEndpoint, cell.ownerCatalog
	cell.mu.Unlock()
	if endpoint == nil || !storageTransferNamespaceProof(*endpoint) || !reflect.DeepEqual(endpoint.Inner.Catalog, catalog) {
		t.Fatal("retired view lacks original inherited native retirement evidence")
	}
	var view storagebudget.ID
	var owning storageOwnerPersistentObject
	for _, row := range catalog.Roots {
		if !storageTransferCatalogView(row) && string(row.Source.Path) == anchor {
			owning = catalog.Objects[row.DirectoryObject].Persistent
		}
	}
	if !owning.valid(1) {
		t.Fatal("fixture anchor is not an original owning root")
	}
	for _, row := range catalog.Roots {
		if !storageTransferCatalogView(row) || row.Source.ControlSelection != 4 || catalog.Objects[row.DirectoryObject].Persistent != owning {
			continue
		}
		source, err := storageEnrollmentPeakDecodeSource(row.Source.Relative)
		if err != nil {
			t.Fatal(err)
		}
		if string(source.Default.SelectedPath) != anchor || string(source.Default.Next) != next {
			continue
		}
		binding := s.storageConstruction.owner.roots[row.Definition.ID]
		if binding == nil || !binding.transferInactive || binding.transferInactiveCurrent(t.Context()) != nil {
			t.Fatal("retained retired binding lost original native retirement/absence")
		}
		if _, err = binding.root.Lstat(next); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("retired namespace Next was not positively absent", err)
		}
		if view != (storagebudget.ID{}) {
			t.Fatal("duplicate retired fixture view")
		}
		view = row.Definition.ID
	}
	if view == (storagebudget.ID{}) {
		t.Fatal("actual operations did not retain the required retired view")
	}
	return view
}

// Return before the first data effect, letting this original admitted operation
// perform its own checked cleanup and native namespace retirement in finalization.
func storageTransferNativeRetireAbsent(t *testing.T, s *Server, server *httptest.Server, anchor, next string) storagebudget.ID {
	t.Helper()
	before := storageTransferNativeSelection(t, s, server)
	r := httptest.NewRequest("POST", server.URL+"/api/storage/move", nil)
	pageBearer(privateWorkspaceTestSession(t, server))(r)
	p, err := s.prepareOriginalChatTransfer(r, storageTransferMove)
	if err != nil {
		t.Fatal(err)
	}
	p.targetPath = filepath.Join(anchor, next)
	var retired *chatTransferOperation
	err = s.withOriginalChatTransfer(p, func(op *chatTransferOperation) error {
		retired = op
		if err := op.captureOriginalTransferSource(p.ctx); err != nil {
			return err
		}
		plan, err := planChatData(p.ctx, op.source)
		if err != nil {
			return err
		}
		if err = op.planOriginalTransferWorkload(p.ctx, moveChatData{Path: p.targetPath, Revision: before.Revision}, plan); err != nil {
			return err
		}
		if err = op.enrollOriginalTransferDestination(p.ctx); err != nil {
			return err
		}
		return op.reserveWhole(p.ctx) // No data ordinal is entered.
	})
	if err != nil || retired == nil || !p.completed || !retired.tailCompleted || retired.record.PointerState != 0 || !retired.record.Cleanup.complete() || retired.appliedProof == nil || !storageTransferApplied(retired.record, retired.catalog, *retired.appliedProof) {
		t.Fatal("original admitted abort did not retire absent native view", err)
	}
	if !retired.reserveReturned || retired.reserveError != nil || retired.record.InitialToken == (storagebudget.Token{}) || !retired.finished || !p.finished || len(chatTransferProcessPermit) != 0 {
		t.Fatal("original no-data transfer did not drain its one reservation")
	}
	for _, step := range retired.board {
		if step.kind == storageTransferPointerOrdinal && retired.record.PointerState != 0 {
			t.Fatal("no-data retirement published a pointer")
		}
	}
	return storageTransferNativeRetiredView(t, s, anchor, next)
}

// R2: owning source A and the independent retired namespace view use the same
// actual native anchor. Existing E has its own retained view and remains an
// original empty owning root. The no-data continuation below returns normally:
// original cleanup/Settle/applied-Q/final-P retire the absent Next themselves.
// Both roots originate from this one requester's admitted cohort. This native
// fixture checks unchanged B union A; a differing-cohort native route remains a
// separate gate (the changing-union comparator vectors are retained unchanged).
func TestStorageTransferQualifiedRetiredViewExportAndExistingTarget(t *testing.T) {
	requireStorageLine(t)
	records, err := storageEnrollmentNativeRecords(storageEnrollmentInstalledNativeRecords)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Skip("requires actual installed Record3 matching this exact source candidate; no qualification is fabricated")
	}
	for _, restore := range []bool{false, true} {
		name := "move-to-existing-E"
		if restore {
			name = "restore-to-existing-E"
		}
		t.Run(name, func(t *testing.T) {
			s, err := New(ownerActualConfig(t))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if e := s.Close(); e != nil {
					t.Error(e)
				}
			}()
			server := httptest.NewServer(s)
			defer server.Close()
			privateWorkspaceTestSession(t, server)
			e := filepath.Join(t.TempDir(), "E")
			a := filepath.Join(t.TempDir(), "A")
			storageTransferNativeMove(t, s, server, a)
			storageTransferNativeMove(t, s, server, e) // Both original A and E are empty.
			view := storageTransferNativeRetireAbsent(t, s, server, a, "retired-next")
			input := storageTransferUploadArchive(t, "valid")
			storageTransferNativeRestore(t, s, server, a, input)
			payloadName := "conversations-" + strings.Repeat("a", 64) + ".json"
			payload := `{"version":1,"chats":[]}`
			mustBytes(t, filepath.Join(a, payloadName), payload)
			sourceBytes := storageTransferExportSourceState(t, a)
			if storageTransferNativeRetiredView(t, s, a, "retired-next") != view {
				t.Fatal("original restore to A lost inherited retired view")
			}
			targetView := storageTransferNativeRetireAbsent(t, s, server, e, "retired-target-next")
			emptyE := storageTransferExportSourceState(t, e)
			if len(emptyE) != 1 || emptyE[dataMarkerName] == "" {
				t.Fatal("existing original E is not empty apart from its original marker")
			}
			cell := s.storageConstruction.ref.cell
			cell.mu.Lock()
			originalCatalog := cell.ownerCatalog
			cell.mu.Unlock()
			var originalE storageOwnerRoot
			for _, row := range originalCatalog.Roots {
				if !storageTransferCatalogView(row) && string(row.Source.Path) == e {
					originalE = row
				}
			}
			if originalE.Definition.ID == (storagebudget.ID{}) {
				t.Fatal("original E owning definition missing")
			}
			var exported *chatTransferOperation
			oldExport := testOriginalChatTransferExportCut
			testOriginalChatTransferExportCut = func(cut byte) error {
				if cut == storageTransferExportReadyCut {
					exported = storageTransferNativeCurrent(t, s)
				}
				return nil
			}
			defer func() { testOriginalChatTransferExportCut = oldExport }()
			w := storageTransferNativeRequest(t, s, server, "GET", "/api/storage/backup", nil, "")
			if w.Header().Get("Content-Type") != "application/zip" {
				t.Fatal("original export did not serve ZIP")
			}
			storageTransferNativeFinal(t, s, exported, false)
			archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, file := range archive.File {
				if file.Name == payloadName {
					reader, err := file.Open()
					if err != nil {
						t.Fatal(err)
					}
					raw, readErr := io.ReadAll(reader)
					closeErr := reader.Close()
					if readErr != nil || closeErr != nil || string(raw) != payload {
						t.Fatal("original export bytes/Close", readErr, closeErr)
					}
					found = true
				}
			}
			if !found {
				t.Fatal("export omitted handwritten original payload")
			}
			var final *chatTransferOperation
			oldPointer := testBeforeDataMoveCommit
			testBeforeDataMoveCommit = func() error { final = storageTransferNativeCurrent(t, s); return nil }
			defer func() { testBeforeDataMoveCommit = oldPointer }()
			if restore {
				storageTransferNativeRestore(t, s, server, e, input)
			} else {
				storageTransferNativeMove(t, s, server, e)
			}
			storageTransferNativeFinal(t, s, final, true)
			finalCatalog := final.preparation.epoch.completedEvidence.Inner.Catalog
			finalE, ok := storageTransferCatalogRoot(finalCatalog, originalE.Definition.ID)
			if !ok || finalE.Definition != originalE.Definition || !reflect.DeepEqual(finalE.Source, originalE.Source) || finalE.DirectoryObject != originalE.DirectoryObject {
				t.Fatal("final P changed original E owning definition/source")
			}
			wantRefs := map[storagebudget.ID]bool{}
			for _, id := range originalE.References {
				wantRefs[id] = true
			}
			for _, row := range originalCatalog.Roots {
				if !storageTransferCatalogView(row) && string(row.Source.Path) == a {
					for _, id := range row.References {
						wantRefs[id] = true
					}
				}
			}
			if len(finalE.References) != len(wantRefs) {
				t.Fatal("final E reference count differs from original B union A")
			}
			for _, id := range finalE.References {
				if !wantRefs[id] {
					t.Fatal("final E gained foreign owning reference")
				}
				delete(wantRefs, id)
			}
			if len(wantRefs) != 0 {
				t.Fatal("final E omitted original owning reference")
			}
			if storageTransferNativeRetiredView(t, s, e, "retired-target-next") != targetView {
				t.Fatal("final P lost original target-side retired view")
			}
			if storageTransferNativeSelection(t, s, server).Path != e || storageTransferNativeRetiredView(t, s, a, "retired-next") != view {
				t.Fatal("final P lost E selection or inherited retired view")
			}
			mustBytes(t, filepath.Join(a, payloadName), payload)
			mustBytes(t, filepath.Join(e, payloadName), payload)
			if !reflect.DeepEqual(sourceBytes, storageTransferExportSourceState(t, a)) {
				t.Fatal("export/transfer changed original source bytes")
			}
			if err = s.Close(); err != nil {
				t.Fatal("checked original Close", err)
			}
		})
	}
}

// R3: ordinary config Control and the active destination namespace coexist at
// the exact settings anchor. The first real returned descendant remains held
// while the original finite observation actually expires; no timestamp, policy,
// receipt, binding or qualification is rewritten by this fixture.
func TestStorageTransferQualifiedExpiredSharedAnchorRefresh(t *testing.T) {
	requireStorageLine(t)
	records, err := storageEnrollmentNativeRecords(storageEnrollmentInstalledNativeRecords)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Skip("requires actual installed Record3 matching this exact source candidate; no qualification is fabricated")
	}
	// Match the existing external native ceiling, or the earlier Go test
	// deadline. This fixture never grants additional time to its worker.
	deadline := time.Now().Add(120 * time.Second)
	if testDeadline, ok := t.Deadline(); ok && testDeadline.Before(deadline) {
		deadline = testDeadline
	}
	requestCtx, cancelRequest := context.WithDeadline(t.Context(), deadline)
	defer cancelRequest()
	s, err := New(ownerActualConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := s.Close(); e != nil {
			t.Error(e)
		}
	}()
	server := httptest.NewServer(s)
	defer server.Close()
	privateWorkspaceTestSession(t, server)
	a := filepath.Join(t.TempDir(), "A")
	storageTransferNativeRestore(t, s, server, a, storageTransferUploadArchive(t, "valid"))
	payloadName := "conversations-" + strings.Repeat("a", 64) + ".json"
	payload := `{"version":1,"chats":[]}`
	mustBytes(t, filepath.Join(a, payloadName), payload)
	target := filepath.Join(s.configDir, "r3-parent", "r3-target")
	ready, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(resume) }) }
	defer release()
	var op *chatTransferOperation
	var originalAt time.Time
	var originalToken storagebudget.Token
	var originalBinding *storageRootBinding
	var originalParent storageOwnerPersistentObject
	var returnedParent *os.Root
	var originalRoots []*storageRootBinding
	var pausedQ storagebudget.HostState
	var custodyFiles map[*os.File]os.FileInfo
	fresh, returnedDirectories := 0, 0
	oldCut := testOriginalChatTransferNativeCut
	testOriginalChatTransferNativeCut = func(cut byte) error {
		switch cut {
		case storageTransferDirectoryReturnedCut:
			returnedDirectories++
			if returnedDirectories != 1 {
				return nil
			}
			op = storageTransferNativeCurrent(t, s)
			if len(op.created) != 1 || op.returned.Descendants[0].State != 2 || op.next != 1 || op.results[0] != 2 || op.entered || op.path.native != op.record.Settings || op.path.native != op.record.Parent || len(op.returned.Descendants) != 2 {
				return errors.New("first original directory did not return at exact shared settings anchor")
			}
			binding, err := op.originalTransferDescendantBinding()
			if err != nil {
				return err
			}
			originalBinding = binding
			row, ok := storageTransferCatalogRoot(op.catalog, binding.row.ID)
			if !ok || !storageTransferCatalogView(row) || row.Source.ControlSelection != 0 || !reflect.DeepEqual(row.References, binding.references) || binding.source.scope == nil || binding.source.scope.Default == nil || string(binding.source.scope.Default.Next) != "r3-parent" || string(binding.source.scope.Default.SelectedPath) != s.configDir || binding.source.scope.Default.Settings != op.record.Settings || binding.source.scope.Default.SourceNative != op.record.Source || binding.source.scope.Default.Selected != op.record.Parent || binding.source.persistent != op.record.Parent {
				return errors.New("fresh parent did not select the original active destination namespace")
			}
			control := false
			for _, root := range op.preparation.epoch.originalRoots {
				control = control || root.configControl && root.source.persistent == op.record.Parent && root != binding
			}
			if !control {
				return errors.New("ordinary original config Control binding missing at shared anchor")
			}
			originalAt, originalToken = op.capacityAt, op.record.InitialToken
			if originalAt.IsZero() || time.Duration(op.catalog.Policy.ObservationMaxAgeNanos) != 5*time.Second {
				return errors.New("fixture requires unchanged original finite five-second policy")
			}
			originalRoots = append([]*storageRootBinding{}, op.preparation.epoch.originalRoots...)
			returnedParent = op.created[0].root
			originalParent, err = storageOwnerPersistentRoot(op.preparation.ctx, op.created[0].root)
			if err != nil {
				return err
			}
			pausedQ, err = s.storageConstruction.ref.cell.q.InspectHostState(op.preparation.ctx)
			if err != nil {
				return err
			}
			if err = op.originalTransferCustody(op.preparation.ctx); err != nil {
				return err
			}
			epoch := op.preparation.epoch
			custodyFiles = map[*os.File]os.FileInfo{}
			files := []*os.File{op.private.file, epoch.use.gate, epoch.work.file, epoch.transferLock.file, epoch.project, epoch.c.home.lease, op.path.domainOwner, epoch.transferExtraDomain}
			for _, hold := range epoch.transferDomainOwners {
				files = append(files, hold.file)
			}
			for _, file := range files {
				if file == nil {
					continue
				}
				info, err := file.Stat()
				if err != nil {
					return err
				}
				custodyFiles[file] = info
			}
			close(ready)
			select {
			case <-resume:
			case <-requestCtx.Done():
				return requestCtx.Err()
			}
			if err := requestCtx.Err(); err != nil {
				return err
			}
			if time.Since(originalAt) <= time.Duration(op.catalog.Policy.ObservationMaxAgeNanos) {
				return errors.New("original capacity observation did not actually expire")
			}
			// Still inside the paused original callback, before any next
			// effect or finalization. Inspect on its lexical worker so request
			// cancellation cannot race a parent inspection with native drain.
			if err := op.originalTransferCustody(op.preparation.ctx); err != nil {
				return errors.Join(errors.New("full original transfer custody was not held at expired pause"), err)
			}
			for file, original := range custodyFiles {
				info, err := file.Stat()
				if err != nil || !os.SameFile(info, original) {
					return errors.Join(errors.New("original private/use/home/project/domain description changed while paused"), err)
				}
			}
			if err := op.created[0].current(); err != nil {
				return errors.Join(errors.New("original returned descendant lost custody while paused"), err)
			}
			if err := originalBinding.currentForOriginalTransfer(op.preparation.ctx, op); err != nil {
				return errors.Join(errors.New("original namespace lost currentness while paused"), err)
			}
		case storageTransferCapacityObservedCut:
			fresh++
			if op == nil {
				return errors.New("fresh pass preceded the paused original descendant")
			}
			pass := op.observation
			if fresh != 1 || pass == nil || pass.operation != op || pass.closed || pass.completed.IsZero() || !pass.completed.After(originalAt) || pass.current() != nil || len(pass.parents) != 1 || len(pass.parents) > storageCapacityPassParents || !reflect.DeepEqual(pass.roots, originalRoots) {
				return errors.New("refresh did not hold exactly one original fresh pass and bounded returned-parent window")
			}
			parent := pass.parents[0]
			if parent.binding != originalBinding || parent.descendant != 0 || parent.root != op.created[0].root || parent.native != originalParent || op.returned.Descendants[0].Object != originalParent.Object || op.returned.Descendants[0].Incarnation != originalParent.Incarnation || op.returned.Descendants[0].State != 2 || op.record.InitialToken != originalToken || op.next != 1 {
				return errors.New("fresh pass changed original namespace/returned descendant/ticket or entered next effect early")
			}
			actual, err := storageOwnerPersistentRoot(op.preparation.ctx, parent.root)
			if err != nil || actual != originalParent {
				return errors.Join(errStorageCustody, err)
			}
			now, err := s.storageConstruction.ref.cell.q.InspectHostState(op.preparation.ctx)
			if err != nil || !reflect.DeepEqual(now, pausedQ) {
				return errors.Join(errors.New("capacity refresh changed Q instead of retaining the one original promise"), err)
			}
		}
		return nil
	}
	defer func() {
		// Cancellation releases fixture-owned waits; only the lexical worker
		// can drain originals. Never restore a global cut over a live borrower.
		cancelRequest()
		release()
		timer := time.NewTimer(max(time.Duration(0), time.Until(deadline)))
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			t.Error("original native worker did not drain within the remaining existing deadline; mandatory custody join remains contained by the unchanged external 120s failure bound")
			// An entered native call can stall despite cancellation. Keep its
			// originals and hook alive for the existing runner timeout/survivor
			// evidence; do not abandon it or claim a 15s total drain bound.
			<-done
		}
		testOriginalChatTransferNativeCut = oldCut
	}()
	go func() { defer close(done); storageTransferNativeMove(t, s, server, target, requestCtx) }()
	select {
	case <-ready:
	case <-done:
		t.Fatal("qualified move returned before first original descendant pause")
	case <-requestCtx.Done():
		cancelRequest()
		t.Fatal("qualified move exceeded the remaining original request deadline before first descendant", requestCtx.Err())
	case <-time.After(min(15*time.Second, max(time.Duration(0), time.Until(deadline)))):
		cancelRequest()
		t.Fatal("qualified move did not reach first original descendant; request canceled before mandatory native drain")
	}
	// This is elapsed real time against the original returned observation. The
	// policy is finite and unchanged, comfortably inside the 120s native group.
	wait := time.Until(originalAt.Add(time.Duration(op.catalog.Policy.ObservationMaxAgeNanos) + 100*time.Millisecond))
	if wait > 0 {
		select {
		case <-time.After(wait):
		case <-done:
			cancelRequest()
			t.Fatal("original move escaped its held descendant pause")
		case <-requestCtx.Done():
			cancelRequest()
			t.Fatal("remaining original request deadline expired during fixture pause", requestCtx.Err())
		}
	}
	release()
	select {
	case <-done:
	case <-requestCtx.Done():
		cancelRequest()
		t.Fatal("qualified expired refresh exceeded remaining original request deadline", requestCtx.Err())
	case <-time.After(min(15*time.Second, max(time.Duration(0), time.Until(deadline)))):
		cancelRequest()
		t.Fatal("qualified expired refresh/next effect did not return within 15s diagnostic wait; request canceled before mandatory native drain")
	}
	if fresh != 1 || returnedDirectories != 2 || op.capacityAt == originalAt || op.observation != nil || op.record.InitialToken != originalToken {
		t.Fatal("real expiry did not yield one closed fresh pass followed by the next original effect", fresh, returnedDirectories)
	}
	storageTransferNativeFinal(t, s, op, true)
	if _, err := returnedParent.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Error("original returned parent was not closed after normal completion", err)
	}
	if storageTransferNativeSelection(t, s, server).Path != target {
		t.Fatal("normal final P did not expose original target")
	}
	mustBytes(t, filepath.Join(a, payloadName), payload)
	mustBytes(t, filepath.Join(target, payloadName), payload)
	if err = s.Close(); err != nil {
		t.Fatal("checked original Close", err)
	}
}
