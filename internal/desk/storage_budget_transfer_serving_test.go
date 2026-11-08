package desk

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Component evidence only: use the actual registered preparation's cancellation
// without constructing an admitted operation, archive or serving permission.
// The deliberately unsealed operation cannot pass serveOriginalChatTransferExport.
func TestStorageTransferExportViewsRefuseRevokedPreparation(t *testing.T) {
	requireStorageLine(t)
	for _, byServer := range []bool{false, true} {
		name := "request-cancel"
		if byServer {
			name = "server-stop-derived-context"
		}
		t.Run(name, func(t *testing.T) {
			s, err := New(ownerActualConfig(t))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r := httptest.NewRequest("GET", "http://localhost/api/storage/backup", nil).WithContext(ctx)
			p, err := s.prepareOriginalChatTransfer(r, storageTransferExport)
			if err != nil {
				_ = s.Close()
				t.Fatal(err)
			}
			if byServer {
				s.stopOriginalChatTransfers()
				if r.Context().Err() != nil {
					t.Error("fixture did not preserve the uncanceled original request")
				}
			} else {
				cancel()
			}
			<-p.ctx.Done()
			operation := &chatTransferOperation{preparation: p}
			recorder := httptest.NewRecorder()
			binding := &chatTransferExportResponse{operation: operation, request: r, writer: recorder}
			writer := &chatTransferExportWriter{binding: binding}
			writer.Header().Set("X-Revoked-Output", "must remain detached")
			if recorder.Header().Get("X-Revoked-Output") != "" {
				t.Error("revoked Header exposed the original response")
			}
			writer.WriteHeader(http.StatusCreated)
			if n, e := writer.Write([]byte("must not be emitted")); n != 0 || !errors.Is(e, context.Canceled) {
				t.Error("revoked response Write was forwarded", n, e)
			}
			if binding.started || recorder.Code != http.StatusOK || recorder.Body.Len() != 0 || !errors.Is(binding.err, context.Canceled) {
				t.Error("revocation started headers/body or lost the response error")
			}
			// No file is supplied: cancellation must refuse before touching any
			// description, and the unsealed view cannot acquire one itself.
			archive := &chatTransferExportArchive{operation: operation}
			if n, e := archive.Read(make([]byte, 1)); n != 0 || !errors.Is(e, context.Canceled) {
				t.Error("revoked archive Read was entered", n, e)
			}
			if n, e := archive.Seek(0, io.SeekStart); n != 0 || !errors.Is(e, context.Canceled) {
				t.Error("revoked archive Seek was entered", n, e)
			}
			if e := serveOriginalChatTransferExport(operation); !errors.Is(e, storagebudget.ErrAuthority) {
				t.Error("component view acquired a serving entry", e)
			}
			result := p.finish(errors.Join(binding.err, archive.err))
			if !errors.Is(result, context.Canceled) || len(chatTransferProcessPermit) != 0 {
				t.Error("original refusal finalization did not retain cancellation/drain permit", result)
			}
			if e := s.Close(); !errors.Is(e, context.Canceled) {
				t.Error("Close lost original cancellation evidence", e)
			}
		})
	}
}

type storageTransferPausedResponse struct {
	mu               sync.Mutex
	header           http.Header
	statuses         []int
	writes           int
	body             bytes.Buffer
	entered, release chan struct{}
	once             sync.Once
	writeErr         error
	beforeFirstWrite func()
}

func (w *storageTransferPausedResponse) Header() http.Header { return w.header }
func (w *storageTransferPausedResponse) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.statuses = append(w.statuses, status)
}
func (w *storageTransferPausedResponse) Write(data []byte) (int, error) {
	w.mu.Lock()
	w.writes++
	first := w.writes == 1
	w.mu.Unlock()
	if first {
		if w.beforeFirstWrite != nil {
			w.beforeFirstWrite()
		}
		w.once.Do(func() { close(w.entered) })
		<-w.release
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.writeErr != nil {
		n := len(data) / 2
		_, _ = w.body.Write(data[:n])
		return n, w.writeErr
	}
	return w.body.Write(data)
}

type storageTransferHeldExport struct {
	preparation *chatTransferPreparation
	operation   *chatTransferOperation
	archive     *os.File
	files       map[*os.File]os.FileInfo
	sourcePath  string
	source      map[string]string
	beforeQ     storagebudget.HostState
	qPath       string
	qFiles      map[string]string
}

// Read-only fingerprints of the existing Q banks, including durable promises.
// Reopening them for comparison supplies no owner, Q API or transfer authority.
func storageTransferQFiles(t *testing.T, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range []string{"checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
		raw, err := os.ReadFile(filepath.Join(path, name))
		if err != nil {
			t.Fatal(err)
		}
		out[name] = digestOf(raw)
	}
	return out
}

func storageTransferExportSourceState(t *testing.T, path string) map[string]string {
	t.Helper()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	entries, err := storageEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, entry := range entries {
		if !chatStorageFile(entry.Name()) && entry.Name() != dataMarkerName {
			continue
		}
		raw, err := root.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		out[entry.Name()] = digestOf(raw)
	}
	return out
}

func storageTransferCaptureHeldExport(t *testing.T, s *Server) storageTransferHeldExport {
	t.Helper()
	registry := s.storageConstruction.owner.transfers
	registry.mu.Lock()
	p := registry.current
	registry.mu.Unlock()
	if p == nil {
		t.Fatal("ExportReady has no original registered preparation")
	}
	p.mu.Lock()
	op := p.operation
	p.mu.Unlock()
	if op == nil || op.self != op || op.preparation != p || !p.admitted || op.completion == nil || op.stage == nil || op.returned.ArchiveState != 2 || op.source == nil {
		t.Fatal("ExportReady lacked the real admitted archive/operation/completion")
	}
	if err := op.stage.current(); err != nil {
		t.Fatal(err)
	}
	epoch := p.epoch
	if epoch == nil || epoch.use == nil || epoch.work == nil || epoch.transferLock == nil || op.private == nil || epoch.c.home == nil {
		t.Fatal("ExportReady lost original private/domain/use/home custody")
	}
	files := map[*os.File]os.FileInfo{}
	for _, f := range []*os.File{op.stage.file, op.private.file, epoch.use.gate, epoch.work.file, epoch.transferLock.file, epoch.project, epoch.c.home.lease} {
		info, err := f.Stat()
		if err != nil {
			t.Fatal("original export descriptor not held", err)
		}
		files[f] = info
	}
	for _, hold := range epoch.holds {
		if hold.file != nil {
			info, err := hold.file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			files[hold.file] = info
		}
	}
	beforeQ, err := epoch.c.ref.cell.q.InspectHostState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	qPath := filepath.Join(s.configDir, storageAnchorName)
	return storageTransferHeldExport{preparation: p, operation: op, archive: op.stage.file, files: files, sourcePath: op.source.location.Path, source: storageTransferExportSourceState(t, op.source.location.Path), beforeQ: beforeQ, qPath: qPath, qFiles: storageTransferQFiles(t, qPath)}
}

func storageTransferAssertExportHeld(t *testing.T, held storageTransferHeldExport) {
	t.Helper()
	p, op := held.preparation, held.operation
	// These acquisitions must return while the original Write is paused.
	// They permit legitimate short state sections; a lock-held drain instead
	// blocks this assertion and is caught by the existing guarded test timeout.
	p.registry.mu.Lock()
	current := p.registry.current
	p.registry.mu.Unlock()
	p.mu.Lock()
	registered := p.operation == op && !p.finishing && !p.finished && p.permit
	p.mu.Unlock()
	op.mu.Lock()
	uncompleted := !op.completing && !op.finished && op.stage != nil && op.stage.file == held.archive
	op.mu.Unlock()
	if current != p || !registered || !uncompleted || len(chatTransferProcessPermit) != 1 {
		t.Error("original operation/archive/permit released before serving borrower returned")
	}
	select {
	case <-p.done:
		t.Error("preparation done preceded actual serving return")
	default:
	}
	for file, original := range held.files {
		info, err := file.Stat()
		if err != nil || !os.SameFile(info, original) {
			t.Error("original export descriptor changed/closed while serving paused", err)
		}
	}
	if err := op.private.current(); err != nil {
		t.Error("original private FIFO/EX custody was released", err)
	}
	owner := p.registry.owner
	owner.mu.Lock()
	active, closed := owner.active, owner.closed
	owner.mu.Unlock()
	owner.admission.mu.Lock()
	owner.admission.mu.Unlock()
	owner.controlMu.Lock()
	owner.controlMu.Unlock()
	if active == 0 || closed {
		t.Error("original owner retain dropped before serving borrower returned")
	}
	now, err := p.epoch.c.ref.cell.q.InspectHostState(t.Context())
	if err != nil || !reflect.DeepEqual(now, held.beforeQ) {
		t.Error("Q settlement/checkpoint began before serving borrower returned", err)
	}
}

// These are actual canonical, authenticated export routes. They remain gated
// until real Record3 qualification matches this exact candidate and filesystem.
// Synthetic response/after-real-Close errors are induced fault evidence only;
// they do not certify native durability, ENOSPC, or external client receipt.
func TestStorageTransferQualifiedExportServingLifetime(t *testing.T) {
	requireStorageLine(t)
	records, err := storageEnrollmentNativeRecords(storageEnrollmentInstalledNativeRecords)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Skip("requires actual installed Record3 matching this exact source candidate; no qualification is fabricated")
	}
	for _, tc := range []struct {
		name                                                       string
		disconnect, closeDuringWrite, closeBeforeEntry, closeFault bool
	}{
		{name: "S15a-normal-final-write-paused"},
		{name: "S15b-disconnect", disconnect: true},
		{name: "S15b-disconnect-and-archive-close-error", disconnect: true, closeFault: true},
		{name: "S15c-close-during-write", closeDuringWrite: true},
		{name: "S15c-close-during-write-and-archive-close-error", closeDuringWrite: true, closeFault: true},
		{name: "S15d-close-before-serving-entry", closeBeforeEntry: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := New(ownerActualConfig(t))
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(s)
			defer server.Close()
			defer func() { _ = s.Close() }()
			session := privateWorkspaceTestSession(t, server)
			if err = s.ensureDefaultHistoryEnrollment(t.Context()); err != nil {
				t.Fatal("actual qualified export history prerequisite", err)
			}
			writeFault := errors.New("handwritten export client disconnect")
			closeFault := errors.New("synthetic error joined after real archive Close")
			writer := &storageTransferPausedResponse{header: make(http.Header), entered: make(chan struct{}), release: make(chan struct{})}
			if tc.disconnect {
				writer.writeErr = writeFault
			}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(writer.release) }) }
			defer release()
			ready, resume := make(chan struct{}), make(chan struct{})
			var resumeOnce sync.Once
			resumeReady := func() { resumeOnce.Do(func() { close(resume) }) }
			defer resumeReady()
			var held storageTransferHeldExport
			archiveCloses := 0
			returned := make(chan struct{})
			previousCut := testOriginalChatTransferExportCut
			testOriginalChatTransferExportCut = func(cut byte) error {
				switch cut {
				case storageTransferExportReadyCut:
					held = storageTransferCaptureHeldExport(t, s)
					if writer.header.Get("Content-Type") != "" || len(writer.statuses) != 0 || writer.writes != 0 {
						t.Error("export emitted response output before ExportReady")
					}
					close(ready)
					if tc.closeBeforeEntry {
						<-resume
					}
				case storageTransferExportArchiveClosedCut:
					archiveCloses++
					if held.archive == nil {
						t.Error("archive Close ran without a captured ExportReady descriptor")
						return storagebudget.ErrAuthority
					}
					if _, e := held.archive.Stat(); !errors.Is(e, os.ErrClosed) {
						t.Error("archive Close cut ran before actual original Close", e)
					}
					if held.operation.entered || held.operation.response != nil && !held.operation.response.returned && !tc.closeBeforeEntry {
						t.Error("archive closed before actual serving borrower returned")
					}
					if tc.closeFault {
						return closeFault
					}
				}
				return nil
			}
			defer func() {
				resumeReady()
				release()
				<-returned
				testOriginalChatTransferExportCut = previousCut
			}()
			r := httptest.NewRequest("GET", server.URL+"/api/storage/backup", nil)
			pageBearer(session)(r)
			go func() { defer close(returned); s.handleStorageBackup(writer, r) }()
			select {
			case <-ready:
			case <-returned:
				t.Fatal("actual export failed before ExportReady", writer.statuses, writer.body.String())
			case <-time.After(10 * time.Second):
				t.Fatal("actual qualified export did not reach ExportReady")
			}
			var closeResult chan error
			if !tc.closeBeforeEntry {
				select {
				case <-writer.entered:
				case <-returned:
					t.Fatal("actual export returned without entering response Write")
				case <-time.After(5 * time.Second):
					t.Fatal("actual serving did not enter response Write")
				}
				if !held.operation.entered || held.operation.response.returned {
					t.Error("paused response Write was not the original entered serving borrower")
				}
			}
			storageTransferAssertExportHeld(t, held)
			if tc.closeBeforeEntry || tc.closeDuringWrite {
				closeResult = make(chan error, 1)
				go func() { closeResult <- s.Close() }()
				select {
				case <-held.preparation.ctx.Done():
				case <-time.After(5 * time.Second):
					t.Fatal("Close failed to revoke original preparation")
				}
				select {
				case e := <-closeResult:
					t.Fatal("Close released originals while serving boundary was paused", e)
				default:
				}
				storageTransferAssertExportHeld(t, held)
				// The original request remains live: this specifically exercises
				// Server.Close's derived preparation cancellation.
				if r.Context().Err() != nil {
					t.Error("fixture canceled the raw request")
				}
				if tc.closeDuringWrite {
					view := &chatTransferExportArchive{operation: held.operation, file: held.archive}
					if n, e := view.Read(make([]byte, 1)); n != 0 || !errors.Is(e, context.Canceled) {
						t.Error("post-revocation archive Read entered", n, e)
					}
					if n, e := view.Seek(0, io.SeekStart); n != 0 || !errors.Is(e, context.Canceled) {
						t.Error("post-revocation archive Seek entered", n, e)
					}
					response := &chatTransferExportWriter{binding: held.operation.response}
					if n, e := response.Write([]byte("unadmitted output")); n != 0 || !errors.Is(e, context.Canceled) {
						t.Error("post-revocation response Write entered", n, e)
					}
				}
			}
			if tc.closeBeforeEntry {
				writer.beforeFirstWrite = func() {
					select {
					case <-held.preparation.done:
					default:
						t.Error("ordinary error output preceded original finalization")
					}
				}
				release()
				resumeReady()
			} else {
				release()
			}
			select {
			case <-returned:
			case <-time.After(10 * time.Second):
				t.Fatal("original export serving/finalization failed to drain")
			}
			var finalErr error
			if closeResult != nil {
				select {
				case finalErr = <-closeResult:
				case <-time.After(5 * time.Second):
					t.Fatal("Close did not join actual export completion")
				}
			} else {
				finalErr = s.Close()
			}
			if tc.disconnect && !errors.Is(finalErr, writeFault) {
				t.Error("Close discarded client disconnect", finalErr)
			}
			if tc.closeFault && !errors.Is(finalErr, closeFault) {
				t.Error("Close discarded joined archive Close error", finalErr)
			}
			if tc.closeDuringWrite && !errors.Is(finalErr, context.Canceled) {
				t.Error("Close discarded derived serving cancellation", finalErr)
			}
			if !tc.disconnect && !tc.closeBeforeEntry && !tc.closeDuringWrite && finalErr != nil {
				t.Error("normal original export failed", finalErr)
			}
			if repeated := s.Close(); !reflect.DeepEqual(repeated, finalErr) {
				t.Error("repeated Close changed the joined result", repeated, finalErr)
			}
			if archiveCloses != 1 || len(chatTransferProcessPermit) != 0 || held.operation.stage != nil || !held.operation.finished {
				t.Error("archive/operation/permit did not drain once", archiveCloses)
			}
			if _, e := held.archive.Stat(); !errors.Is(e, os.ErrClosed) {
				t.Error("final original archive remained open", e)
			}
			if !reflect.DeepEqual(held.source, storageTransferExportSourceState(t, held.sourcePath)) {
				t.Error("export removed/changed original source")
			}
			writer.mu.Lock()
			statuses, writes, raw := append([]int(nil), writer.statuses...), writer.writes, bytes.Clone(writer.body.Bytes())
			writer.mu.Unlock()
			if tc.closeBeforeEntry {
				if held.operation.response.started || held.operation.response.returned || writes != 1 || len(statuses) != 1 || statuses[0] == http.StatusOK || writer.header.Get("Content-Type") == "application/zip" {
					t.Error("Close-before-entry streamed or failed to emit its one post-finalization error", statuses, writes)
				}
			} else {
				if !held.operation.response.returned || len(statuses) != 1 || statuses[0] != http.StatusOK || writes != 1 || writer.header.Get("Content-Type") != "application/zip" {
					t.Error("started export appended/replaced a response or re-served", statuses, writes)
				}
				if !tc.disconnect {
					if _, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw))); e != nil {
						t.Error("normal serving return did not contain the prepared exact ZIP", e)
					}
				}
			}
			wantNext := 2
			if tc.closeBeforeEntry {
				wantNext = 1
			}
			if len(held.operation.board) != 2 || held.operation.next != wantNext {
				t.Error("export consumed other than its one stage/serving ordinal", held.operation.board, held.operation.next)
			}
			beforeReplay := storageTransferQFiles(t, held.qPath)
			if e := serveOriginalChatTransferExport(held.operation); e == nil {
				t.Error("finished operation replayed serving")
			}
			if held.operation.next != wantNext || !reflect.DeepEqual(beforeReplay, storageTransferQFiles(t, held.qPath)) {
				t.Error("refused replay changed original ordinal or Q banks")
			}
			for _, change := range held.operation.record.Changes {
				if change != (storagebudget.Change{}) {
					t.Error("export inferred durable receipt/source delta from serving", change)
				}
			}
			stageName, stageErr := storageTransferStageName(held.operation.record)
			if stageErr != nil {
				t.Fatal(stageErr)
			}
			stageInfo, stageErr := os.Stat(filepath.Join(s.configDir, stageName))
			if tc.closeFault {
				if stageErr != nil || !os.SameFile(stageInfo, held.files[held.archive]) {
					t.Error("uncertain archive Close lost its exact charged residue", stageErr)
				}
				if !reflect.DeepEqual(held.qFiles, storageTransferQFiles(t, held.qPath)) || held.operation.controlConsumed[13] || held.operation.controlConsumed[14] || held.operation.controlConsumed[15] {
					t.Error("uncertain archive Close changed Q promise/settlement/checkpoint or replayed a control effect")
				}
				if held.preparation.completed || held.operation.returned.ArchiveState != 2 || held.operation.record.Phase == storageTransferRetired {
					t.Error("uncertain archive Close earned removal credit/retirement")
				}
			} else {
				if !errors.Is(stageErr, os.ErrNotExist) {
					t.Error("completed archive was not cleaned once", stageErr)
				}
				if !held.preparation.completed || held.operation.record.Phase != storageTransferRetired {
					t.Error("actual completed export failed settlement/application/retirement", held.operation.record.Phase, finalErr)
				}
			}
		})
	}
}
