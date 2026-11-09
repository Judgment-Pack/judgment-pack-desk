package desk

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const maxBackupManifest = 1 << 20
const maxBackupData = 128 << 20
const maxBackupArchive = maxBackupData + (2 << 20)

type backupEntry struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type backupManifest struct {
	Version   int           `json:"version"`
	CreatedAt string        `json:"createdAt"`
	Files     []backupEntry `json:"files"`
}

// Only recognized chat records enter a backup. Credentials, server preferences,
// temporary files and project artifacts cannot be selected by the caller.
func chatStorageFile(name string) bool {
	return assistantSourceFileName.MatchString(name) || assistantRunFileName.MatchString(name) || assistantDialectReadRecoveryFileName.MatchString(name) || graphDraftFileName.MatchString(name) || memoryFileName.MatchString(name) || briefFileName.MatchString(name) || sourceReviewsFileName.MatchString(name) || packTestsFileName.MatchString(name) || draftPackFileName.MatchString(name) || conversationFileName.MatchString(name) || attachmentFileName.MatchString(name) || name == projectBindingsName
}
func validateStorageFile(name string, data []byte) error {
	if assistantDialectReadRecoveryFileName.MatchString(name) {
		_, err := decodeAssistantDialectReadRecovery(name, data)
		return err
	}
	if !chatStorageFile(name) {
		return fmt.Errorf("unsupported chat data file %q", name)
	}
	if assistantSourceFileName.MatchString(name) {
		_, err := decodeAssistantSourceStorage(name, data)
		return err
	}
	if assistantRunFileName.MatchString(name) {
		_, err := decodeAssistantRunStorage(name, data)
		return err
	}
	if graphDraftFileName.MatchString(name) {
		return validateGraphDrafts(data)
	}
	if memoryFileName.MatchString(name) {
		return validateMemoryStorage(data)
	}
	if attachmentFileName.MatchString(name) {
		return validateAttachmentObject(data)
	}
	if name == projectBindingsName {
		_, err := decodeProjectBindings(data)
		return err
	}
	if briefFileName.MatchString(name) {
		return validateBriefs(data)
	}
	if sourceReviewsFileName.MatchString(name) {
		return validateSourceReviews(data)
	}
	if packTestsFileName.MatchString(name) {
		return validatePackTests(data)
	}
	if draftPackFileName.MatchString(name) {
		return validateDraftPacks(data)
	}
	return validateConversations(data)
}

func (s *Server) handleStorageBackup(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	if err := s.ensureDefaultHistoryEnrollment(r.Context()); err != nil {
		storageFailure(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if s.storageEnrollment != nil {
		started, err := s.withOriginalChatExport(w, r)
		if err != nil && !started {
			storageFailure(w, err)
		}
		return
	}
	transfer, err := s.privateDataFileLock(".data-transfer.lock", true)
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer transfer.Close()
	work, err := s.privateDataFileLock(".data-work.lock", true)
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer work.Close()
	file, name, err := newDataStage(s.assistant.root)
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer file.Close()
	defer s.assistant.root.Remove(name)
	// Prepare a complete archive before starting the HTTP response. A reader
	// therefore receives either a valid ZIP or an ordinary actionable error.
	err = func() error {
		s.writes.Lock()
		defer s.writes.Unlock()
		lock, err := s.privateDataLock(true)
		if err != nil {
			return err
		}
		defer lock.Close()
		store, err := s.openChatData()
		if err != nil {
			return err
		}
		defer store.root.Close()
		return buildChatBackup(r.Context(), store, file, func(name string, data []byte) error {
			if assistantSourceFileName.MatchString(name) {
				return s.authorizeAssistantSourceStorage(r)
			}
			if assistantRunFileName.MatchString(name) || assistantDialectReadRecoveryFileName.MatchString(name) {
				return s.authorizeAssistantRunStorage(r)
			}
			if privateWorkspaceStorageFile(name) {
				return s.authorizePrivateWorkspace(r)
			}
			if conversationFileName.MatchString(name) {
				return s.authorizePolicyConversations(r, data)
			}
			if draftPackFileName.MatchString(name) {
				return s.authorizePolicyWorkspace(r, data, true)
			}
			return nil
		})
	}()
	if err != nil {
		storageFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="jpack-chat-backup.zip"`)
	http.ServeContent(w, r, "jpack-chat-backup.zip", time.Time{}, file)
}

func buildChatBackup(ctx context.Context, store *chatDataStore, out *os.File, admit ...func(string, []byte) error) error {
	if err := writeChatBackup(ctx, store, out, time.Now().UTC().Format(time.RFC3339), admit...); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	_, err := out.Seek(0, io.SeekStart)
	return err
}

// writeChatBackup writes v1 bytes to an already supplied sink. createdAt must be
// captured once by the caller and reused when counting and emitting an archive.
// The sink includes local headers, manifest, central directory and EOCD in its cap.
func writeChatBackup(ctx context.Context, store *chatDataStore, out io.Writer, createdAt string, admit ...func(string, []byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateStoredBindings(store.root); err != nil {
		return err
	}
	entries, err := storageEntries(store.root)
	if err != nil {
		return err
	}
	manifest := backupManifest{Version: 1, CreatedAt: createdAt, Files: []backupEntry{}}
	bounded := &backupArchiveWriter{ctx: ctx, out: out, limit: maxBackupArchive}
	archive := zip.NewWriter(bounded)
	var total int64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		if !chatStorageFile(name) {
			if store.legacy || name == dataMarkerName || strings.HasPrefix(name, ".data-") || strings.HasPrefix(name, configStagingPrefix) {
				continue
			}
			return fmt.Errorf("unrecognized item %s in chat storage; backup was not created", name)
		}
		data, err := readPrivateData(store.root, name, storageFileLimit(name))
		if err != nil {
			return err
		}
		if err = validateStorageFile(name, data); err != nil {
			return err
		}
		for _, authorize := range admit {
			if err = authorize(name, data); err != nil {
				return err
			}
		}
		total += int64(len(data))
		if total > maxBackupData {
			return withCode(CodeTooLarge, errors.New("chat backup exceeds the 128 MiB limit"))
		}
		item, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			return err
		}
		if _, err = item.Write(data); err != nil {
			return err
		}
		manifest.Files = append(manifest.Files, backupEntry{Name: name, Bytes: int64(len(data)), SHA256: digestOf(data)})
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if len(data) > maxBackupManifest {
		return withCode(CodeTooLarge, errors.New("backup manifest exceeds its limit"))
	}
	item, err := archive.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: zip.Store})
	if err != nil {
		return err
	}
	if _, err = item.Write(data); err != nil {
		return err
	}
	if err = archive.Close(); err != nil {
		return err
	}
	return nil
}

func validateChatBackup(file *os.File) (*zip.Reader, backupManifest, error) {
	var manifest backupManifest
	stat, err := file.Stat()
	if err != nil {
		return nil, manifest, err
	}
	return validateChatBackupReader(context.Background(), file, stat.Size())
}

// validateChatBackupReader retains the bounded raw-directory pass before zip's
// file table allocation. Entry bytes/checksums and authorization are checked by
// walkChatBackup, as in the existing restore copy path.
func validateChatBackupReader(ctx context.Context, input io.ReaderAt, size int64) (*zip.Reader, backupManifest, error) {
	var manifest backupManifest
	file := backupContextReaderAt{ctx: ctx, input: input}
	if err := ctx.Err(); err != nil {
		return nil, manifest, err
	}
	if size > maxBackupArchive {
		return nil, manifest, withCode(CodeTooLarge, errors.New("chat backup exceeds its archive limit"))
	}
	if err := checkArchiveDirectory(file, size, maxStorageFiles); err != nil {
		return nil, manifest, err
	}
	archive, err := zip.NewReader(file, size)
	if err != nil {
		return nil, manifest, err
	}
	if len(archive.File) > maxStorageFiles {
		return nil, manifest, errors.New("backup contains too many files")
	}
	var record *zip.File
	seen := map[string]bool{}
	for _, entry := range archive.File {
		if err := ctx.Err(); err != nil {
			return nil, manifest, err
		}
		if seen[entry.Name] || (entry.Name != "manifest.json" && !chatStorageFile(entry.Name)) || entry.Mode()&os.ModeType != 0 {
			return nil, manifest, errors.New("backup contains duplicate, unsupported or unsafe files")
		}
		seen[entry.Name] = true
		if entry.Name == "manifest.json" {
			record = entry
		}
	}
	if record == nil {
		return nil, manifest, errors.New("backup manifest is missing")
	}
	data, err := readBackupFile(record, maxBackupManifest)
	if canceled := ctx.Err(); canceled != nil {
		return nil, manifest, canceled
	}
	if err != nil {
		return nil, manifest, err
	}
	if err = decodeDataJSON(data, &manifest); err != nil {
		return nil, manifest, err
	}
	if manifest.Version != 1 || manifest.Files == nil || len(manifest.Files) != len(archive.File)-1 {
		return nil, manifest, errors.New("unsupported or incomplete backup manifest")
	}
	checked := map[string]bool{}
	var total int64
	for _, entry := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return nil, manifest, err
		}
		if !chatStorageFile(entry.Name) || !seen[entry.Name] || checked[entry.Name] || entry.Bytes < 0 || entry.Bytes > int64(storageFileLimit(entry.Name)) || len(entry.SHA256) != 64 {
			return nil, manifest, errors.New("backup manifest has an invalid file entry")
		}
		checked[entry.Name] = true
		total += entry.Bytes
		if total > maxBackupData {
			return nil, manifest, withCode(CodeTooLarge, errors.New("expanded backup exceeds the 128 MiB limit"))
		}
	}
	return archive, manifest, nil
}
func readBackupFile(file *zip.File, limit int) ([]byte, error) {
	if file.UncompressedSize64 > uint64(limit) {
		return nil, withCode(CodeTooLarge, errors.New("backup entry exceeds its limit"))
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := readBounded(reader, limit)
	if err != nil {
		return nil, errors.New("backup entry is corrupt or exceeds its limit")
	}
	return data, nil
}

func copyChatBackup(ctx context.Context, file *os.File, target *chatDataStore, copied *[]string, admit ...func(string, []byte) error) error {
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	err = walkChatBackup(ctx, file, stat.Size(), func(name string, data []byte) error {
		data, err := transformChatTransfer(name, data, chatTransferRestore)
		if err != nil {
			return err
		}
		*copied = append(*copied, name)
		if err := writePrivateData(target.root, name, data); err != nil {
			return err
		}
		if assistantSourceFileName.MatchString(name) {
			if err := readbackAssistantSourceStorage(target.root, name, data); err != nil {
				return err
			}
		}
		return nil
	}, admit...)
	if err != nil {
		return err
	}
	return validateStoredBindings(target.root)
}

func (s *Server) handleStorageRestore(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if s.storageEnrollment != nil {
		status, e := s.withOriginalChatRestore(w, r)
		if e != nil {
			storageFailure(w, e)
		} else {
			writeJSON(w, http.StatusOK, status)
		}
		return
	}

	transfer, err := s.privateDataFileLock(".data-transfer.lock", true)
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer transfer.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupArchive+(32<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Choose a chat backup and an empty destination folder.")
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "settings" {
		writeJSONCoded(w, 400, CodeBadRequest, "Restore settings must precede the backup.")
		return
	}
	data, err := readBounded(part, 16<<10)
	part.Close()
	var request moveChatData
	if err != nil || decodeDataJSON(data, &request) != nil || !validMoveRequest(request) {
		writeJSONCoded(w, 400, CodeBadRequest, "Choose an absolute destination and reload storage settings.")
		return
	}
	part, err = reader.NextPart()
	if err != nil || part.FormName() != "backup" {
		writeJSONCoded(w, 400, CodeBadRequest, "A chat backup is required.")
		return
	}
	file, name, err := newDataStage(s.assistant.root)
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer file.Close()
	defer s.assistant.root.Remove(name)
	size, err := io.Copy(file, io.LimitReader(part, maxBackupArchive+1))
	part.Close()
	if err != nil || size > maxBackupArchive {
		writeJSONCoded(w, 413, CodeTooLarge, "The backup exceeds its limit or the upload was interrupted.")
		return
	}
	if _, err = reader.NextPart(); !errors.Is(err, io.EOF) {
		writeJSONCoded(w, 400, CodeBadRequest, "Unexpected data after the backup.")
		return
	}
	if _, _, err = validateChatBackup(file); err != nil {
		writeJSONCoded(w, 400, CodeBadRequest, err.Error())
		return
	}
	s.changeChatStorage(w, r, request, func(ctx context.Context, source, target *chatDataStore, copied *[]string) error {
		return copyChatBackup(ctx, file, target, copied, func(name string, data []byte) error {
			if assistantSourceFileName.MatchString(name) {
				return s.authorizeAssistantSourceStorage(r)
			}
			if assistantRunFileName.MatchString(name) || assistantDialectReadRecoveryFileName.MatchString(name) {
				return s.authorizeAssistantRunStorage(r)
			}
			if privateWorkspaceStorageFile(name) {
				if err := s.authorizePrivateWorkspace(r); err != nil {
					return err
				}
				if graphDraftFileName.MatchString(name) {
					return s.admitGraphDraftUpdate(r, []byte(`{"version":1,"drafts":[],"deleted":[]}`), data)
				}
				return nil
			}
			drafts := draftPackFileName.MatchString(name)
			if !drafts && !conversationFileName.MatchString(name) {
				return nil
			}
			if err := s.authorizePolicyWorkspace(r, data, drafts); err != nil {
				return err
			}
			empty := []byte(`{"version":1,"chats":[]}`)
			if drafts {
				empty = []byte(`{"version":1,"drafts":[]}`)
			}
			return s.admitPolicyWorkspaceUpdate(r, empty, data, drafts)
		})
	})
}

// Bound the central directory before archive/zip allocates its file table.
// Desk backups fit ordinary ZIP; ZIP64 and multi-disk archives are unnecessary.
func checkBackupDirectory(file *os.File, size int64) error {
	return checkArchiveDirectory(file, size, maxStorageFiles)
}

func checkArchiveDirectory(file io.ReaderAt, size int64, entryLimit int) error {
	if size < 22 {
		return errors.New("backup is not a complete ZIP archive")
	}
	length := min(size, 65535+22)
	tail := make([]byte, length)
	if _, err := file.ReadAt(tail, size-length); err != nil {
		return err
	}
	end := -1
	for i := len(tail) - 22; i >= 0; i-- {
		if bytes.Equal(tail[i:i+4], []byte{0x50, 0x4b, 0x05, 0x06}) {
			// archive/zip selects the last complete EOCD header signature,
			// including one embedded in a comment. It does not skip that
			// signature to find an earlier record when its comment is
			// truncated. Select the same record, then retain our stricter
			// requirement that its declared comment reaches exact EOF.
			// Otherwise the bounded directory here could differ from the
			// directory archive/zip allocates after this guard returns.
			if i+22+int(binary.LittleEndian.Uint16(tail[i+20:i+22])) != len(tail) {
				return errors.New("backup directory comment does not complete the archive")
			}
			end = i
			break
		}
	}
	if end < 0 {
		return errors.New("backup directory is missing")
	}
	record := tail[end:]
	count := int(binary.LittleEndian.Uint16(record[10:12]))
	directorySize := int64(binary.LittleEndian.Uint32(record[12:16]))
	offset := int64(binary.LittleEndian.Uint32(record[16:20]))
	if binary.LittleEndian.Uint32(record[4:8]) != 0 || int(binary.LittleEndian.Uint16(record[8:10])) != count || count > entryLimit || directorySize > 2<<20 || offset+directorySize != size-length+int64(end) {
		return errors.New("backup directory exceeds supported limits")
	}
	// Match archive/zip's ZIP64 selection before it can replace the bounded
	// directory. A 65535-byte directory is also legal ordinary ZIP, so only
	// refuse it when Go recognizes the immediately preceding ZIP64 locator.
	if count == 0xffff || directorySize == 0xffff || offset == 0xffffffff {
		var locator [20]byte
		endOffset := size - length + int64(end)
		if endOffset >= int64(len(locator)) {
			if _, err := file.ReadAt(locator[:], endOffset-int64(len(locator))); err != nil {
				return err
			}
			if backupZIP64Locator(locator[:]) {
				return errors.New("backup ZIP64 directory is unsupported")
			}
		}
	}
	directory := make([]byte, directorySize)
	if _, err := file.ReadAt(directory, offset); err != nil {
		return err
	}
	actual := 0
	for len(directory) > 0 {
		if len(directory) < 46 || !bytes.Equal(directory[:4], []byte{0x50, 0x4b, 0x01, 0x02}) {
			return errors.New("invalid backup directory")
		}
		// archive/zip discards this field. Check single-disk custody in
		// the bounded raw pass, before it allocates any file table.
		if binary.LittleEndian.Uint16(directory[34:36]) != 0 {
			return errors.New("backup directory contains an entry on another disk")
		}
		entrySize := 46 + int(binary.LittleEndian.Uint16(directory[28:30])) + int(binary.LittleEndian.Uint16(directory[30:32])) + int(binary.LittleEndian.Uint16(directory[32:34]))
		actual++
		if actual > entryLimit || entrySize > len(directory) {
			return errors.New("backup directory exceeds supported limits")
		}
		directory = directory[entrySize:]
	}
	if actual != count {
		return errors.New("backup directory count mismatch")
	}
	return nil
}

// Match the locator fields archive/zip uses to select a ZIP64 directory. An
// offset whose int64 conversion is negative does not select that directory.
func backupZIP64Locator(locator []byte) bool {
	return len(locator) == 20 &&
		binary.LittleEndian.Uint32(locator[:4]) == 0x07064b50 &&
		binary.LittleEndian.Uint32(locator[4:8]) == 0 &&
		binary.LittleEndian.Uint64(locator[8:16]) <= 1<<63-1 &&
		binary.LittleEndian.Uint32(locator[16:20]) == 1
}

// Dependency bodies collocated here so the existing native source pin covers
// the complete original control/planning implementation. Algorithms unchanged.

// These plans contain size/digest facts only. They confer no write admission,
// root identity, custody, or permission to reuse facts after the input changes.
type chatTransferOutput struct {
	Name   string
	Bytes  int64
	SHA256 string
}
type chatTransferPlan struct {
	Files []chatTransferOutput
	Bytes int64
}
type chatTransferKind uint8

const (
	chatTransferRestore chatTransferKind = iota
	chatTransferMove
)

// backupContextReaderAt observes cancellation during both the raw directory pass
// and zip entry reads, without changing the ReaderAt offset/EOF contract.
type backupContextReaderAt struct {
	ctx   context.Context
	input io.ReaderAt
}

func (r backupContextReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.input.ReadAt(p, off)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}

// backupArchiveWriter bounds all emitted archive bytes, including zip.Close's
// directory, before forwarding them. It never owns or closes the supplied sink.
type backupArchiveWriter struct {
	ctx   context.Context
	out   io.Writer
	limit int64
	bytes int64
}

func (w *backupArchiveWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.limit-w.bytes {
		return 0, withCode(CodeTooLarge, errors.New("chat backup exceeds its archive limit"))
	}
	n, err := w.out.Write(p)
	w.bytes += int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

// countChatBackup uses exactly the emitting core and a frozen timestamp. The
// caller must retain the same immutable source through counting and emission.
func countChatBackup(ctx context.Context, store *chatDataStore, createdAt string, admit ...func(string, []byte) error) (int64, error) {
	count := &backupArchiveWriter{ctx: ctx, out: io.Discard, limit: maxBackupArchive}
	if err := writeChatBackup(ctx, store, count, createdAt, admit...); err != nil {
		return 0, err
	}
	return count.bytes, nil
}

// walkChatBackup keeps only the current decoded entry. It preserves the restore
// checks and invokes authorization on original bytes before any transformation.
// visit must consume the current entry synchronously; planning stores metadata.
func walkChatBackup(ctx context.Context, input io.ReaderAt, size int64, visit func(string, []byte) error, admit ...func(string, []byte) error) error {
	archive, manifest, err := validateChatBackupReader(ctx, input, size)
	if err != nil {
		return err
	}
	files := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		files[file.Name] = file
	}
	for _, entry := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := readBackupFile(files[entry.Name], storageFileLimit(entry.Name))
		if err != nil {
			if canceled := ctx.Err(); canceled != nil {
				return canceled
			}
			return err
		}
		if int64(len(data)) != entry.Bytes || digestOf(data) != entry.SHA256 {
			return fmt.Errorf("backup checksum or size mismatch: %s", entry.Name)
		}
		if err = validateStorageFile(entry.Name, data); err != nil {
			return err
		}
		for _, authorize := range admit {
			if err = authorize(entry.Name, data); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err = visit(entry.Name, data); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func transformChatTransfer(name string, data []byte, kind chatTransferKind) ([]byte, error) {
	if kind != chatTransferRestore && kind != chatTransferMove {
		return nil, errors.New("unsupported chat transfer kind")
	}
	var err error
	if assistantSourceFileName.MatchString(name) {
		data, err = historicalAssistantSourceStorage(name, data)
	}
	if err == nil && kind == chatTransferRestore && assistantRunFileName.MatchString(name) {
		data, err = historicalAssistantRunStorage(name, data)
	}
	return data, err
}

func (plan *chatTransferPlan) add(ctx context.Context, name string, raw []byte, kind chatTransferKind) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := transformChatTransfer(name, raw, kind)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	plan.Bytes += int64(len(data))
	if kind == chatTransferMove && plan.Bytes > maxMoveBytes {
		return withCode(CodeTooLarge, errors.New("chat data grew beyond the move limit"))
	}
	plan.Files = append(plan.Files, chatTransferOutput{Name: name, Bytes: int64(len(data)), SHA256: digestOf(data)})
	return nil
}

// planChatBackup counts exact restored serialization, including historical source
// and run rewrites. It creates no destination or staging object. Binding readback
// and original owner/pointer admission remain mandatory at integration/copy time.
func planChatBackup(ctx context.Context, input io.ReaderAt, size int64, admit ...func(string, []byte) error) (chatTransferPlan, error) {
	plan := chatTransferPlan{Files: []chatTransferOutput{}}
	err := walkChatBackup(ctx, input, size, func(name string, data []byte) error { return plan.add(ctx, name, data, chatTransferRestore) }, admit...)
	if err != nil {
		return chatTransferPlan{}, err
	}
	return plan, nil
}

// planChatData mirrors move selection and its transformed-output cap. Moves
// historicalize source approvals only; existing run state remains unchanged.
func planChatData(ctx context.Context, source *chatDataStore) (chatTransferPlan, error) {
	plan := chatTransferPlan{Files: []chatTransferOutput{}}
	if err := ctx.Err(); err != nil {
		return chatTransferPlan{}, err
	}
	entries, err := storageEntries(source.root)
	if err != nil {
		return chatTransferPlan{}, err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return chatTransferPlan{}, err
		}
		name := entry.Name()
		if !chatStorageFile(name) {
			if source.legacy || name == dataMarkerName || strings.HasPrefix(name, ".data-") || strings.HasPrefix(name, configStagingPrefix) {
				continue
			}
			return chatTransferPlan{}, fmt.Errorf("unrecognized item %s in chat storage; it was not moved", name)
		}
		data, err := readPrivateData(source.root, name, storageFileLimit(name))
		if err != nil {
			return chatTransferPlan{}, err
		}
		if err = validateStorageFile(name, data); err != nil {
			return chatTransferPlan{}, err
		}
		if err = plan.add(ctx, name, data, chatTransferMove); err != nil {
			return chatTransferPlan{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return chatTransferPlan{}, err
	}
	return plan, nil
}
