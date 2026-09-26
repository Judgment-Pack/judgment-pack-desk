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
	return briefFileName.MatchString(name) || sourceReviewsFileName.MatchString(name) || packTestsFileName.MatchString(name) || draftPackFileName.MatchString(name) || conversationFileName.MatchString(name) || attachmentFileName.MatchString(name) || name == projectBindingsName
}
func validateStorageFile(name string, data []byte) error {
	if !chatStorageFile(name) {
		return fmt.Errorf("unsupported chat data file %q", name)
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
	w.Header().Set("Cache-Control", "no-store")
	transfer, err := s.privateDataFileLock(".data-transfer.lock", true)
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer transfer.Close()
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
		return buildChatBackup(r.Context(), store, file)
	}()
	if err != nil {
		storageFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="jpack-chat-backup.zip"`)
	http.ServeContent(w, r, "jpack-chat-backup.zip", time.Time{}, file)
}

func buildChatBackup(ctx context.Context, store *chatDataStore, out *os.File) error {
	if err := validateStoredBindings(store.root); err != nil {
		return err
	}
	entries, err := storageEntries(store.root)
	if err != nil {
		return err
	}
	manifest := backupManifest{Version: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339), Files: []backupEntry{}}
	archive := zip.NewWriter(out)
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
	if err = out.Sync(); err != nil {
		return err
	}
	_, err = out.Seek(0, io.SeekStart)
	return err
}

func validateChatBackup(file *os.File) (*zip.Reader, backupManifest, error) {
	var manifest backupManifest
	stat, err := file.Stat()
	if err != nil {
		return nil, manifest, err
	}
	if stat.Size() > maxBackupArchive {
		return nil, manifest, withCode(CodeTooLarge, errors.New("chat backup exceeds its archive limit"))
	}
	if err = checkBackupDirectory(file, stat.Size()); err != nil {
		return nil, manifest, err
	}
	archive, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return nil, manifest, err
	}
	if len(archive.File) > maxStorageFiles {
		return nil, manifest, errors.New("backup contains too many files")
	}
	var record *zip.File
	seen := map[string]bool{}
	for _, entry := range archive.File {
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

func copyChatBackup(ctx context.Context, file *os.File, target *chatDataStore, copied *[]string) error {
	archive, manifest, err := validateChatBackup(file)
	if err != nil {
		return err
	}
	files := map[string]*zip.File{}
	for _, entry := range archive.File {
		files[entry.Name] = entry
	}
	for _, entry := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := readBackupFile(files[entry.Name], storageFileLimit(entry.Name))
		if err != nil {
			return err
		}
		if int64(len(data)) != entry.Bytes || digestOf(data) != entry.SHA256 {
			return fmt.Errorf("backup checksum or size mismatch: %s", entry.Name)
		}
		if err = validateStorageFile(entry.Name, data); err != nil {
			return err
		}
		*copied = append(*copied, entry.Name)
		if err = writePrivateData(target.root, entry.Name, data); err != nil {
			return err
		}
	}
	return validateStoredBindings(target.root)
}

func (s *Server) handleStorageRestore(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
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
		return copyChatBackup(ctx, file, target, copied)
	})
}

// Bound the central directory before archive/zip allocates its file table.
// Desk backups fit ordinary ZIP; ZIP64 and multi-disk archives are unnecessary.
func checkBackupDirectory(file *os.File, size int64) error {
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
		if bytes.Equal(tail[i:i+4], []byte{0x50, 0x4b, 0x05, 0x06}) && i+22+int(binary.LittleEndian.Uint16(tail[i+20:i+22])) == len(tail) {
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
	if binary.LittleEndian.Uint32(record[4:8]) != 0 || int(binary.LittleEndian.Uint16(record[8:10])) != count || count > maxStorageFiles || directorySize > 2<<20 || offset+directorySize != size-length+int64(end) {
		return errors.New("backup directory exceeds supported limits")
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
		entrySize := 46 + int(binary.LittleEndian.Uint16(directory[28:30])) + int(binary.LittleEndian.Uint16(directory[30:32])) + int(binary.LittleEndian.Uint16(directory[32:34]))
		actual++
		if actual > maxStorageFiles || entrySize > len(directory) {
			return errors.New("backup directory exceeds supported limits")
		}
		directory = directory[entrySize:]
	}
	if actual != count {
		return errors.New("backup directory count mismatch")
	}
	return nil
}
