package desk

// This inactive codec validates archive controls and tables only. It does not
// validate saved slots, raw payloads, receipts, complete reads or authorization,
// and cannot admit a restore or confer access to any source owner.
import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

type dialectBackupManifest struct {
	Version        int                    `json:"version"`
	CreatedAt      string                 `json:"createdAt"`
	Files          []backupEntry          `json:"files"`
	Objects        []backupEntry          `json:"objects"`
	DialectClosure []dialectBackupClosure `json:"dialectClosure"`
}

type dialectBackupClosure struct {
	Ledger          string `json:"ledger"`
	RunID           string `json:"runId"`
	HostOperationID string `json:"hostOperationId"`
	Stage           string `json:"stage"`
	Availability    string `json:"availability"`
}

type dialectBackupClosureKey struct {
	ledger, run, host, stage string
}

var errDialectBackupManifest = errors.New("invalid retained Dialect backup metadata")

// Every declared object is closed and every member is required. Decoding keys
// before recording them catches repeated keys even with escaped spellings;
// checking exact membership prevents encoding/json's case-insensitive aliases.
func dialectBackupControlObject(raw []byte, names ...string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return nil, errDialectBackupManifest
	}
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	fields := make(map[string]json.RawMessage, len(names))
	for d.More() {
		token, err := d.Token()
		name, ok := token.(string)
		if err != nil || !ok || !wanted[name] || fields[name] != nil {
			return nil, errDialectBackupManifest
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, errDialectBackupManifest
		}
		fields[name] = value
	}
	last, err := d.Token()
	if err != nil || last != json.Delim('}') || len(fields) != len(names) {
		return nil, errDialectBackupManifest
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errDialectBackupManifest
	}
	return fields, nil
}

func dialectBackupControlArray(raw []byte) ([]json.RawMessage, error) {
	var rows []json.RawMessage
	if json.Unmarshal(raw, &rows) != nil || rows == nil {
		return nil, errDialectBackupManifest
	}
	return rows, nil
}

func dialectBackupEntries(raw []byte) ([]backupEntry, error) {
	rows, err := dialectBackupControlArray(raw)
	if err != nil {
		return nil, err
	}
	entries := make([]backupEntry, 0, len(rows))
	for _, row := range rows {
		if _, err = dialectBackupControlObject(row, "name", "bytes", "sha256"); err != nil {
			return nil, err
		}
		var entry backupEntry
		if json.Unmarshal(row, &entry) != nil {
			return nil, errDialectBackupManifest
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func readDialectBackupManifest(raw []byte) (dialectBackupManifest, error) {
	var manifest dialectBackupManifest
	if len(raw) > maxBackupManifest {
		return manifest, withCode(CodeTooLarge, errors.New("backup manifest exceeds its limit"))
	}
	if !validUTF8(raw) || jsonscalar.ValidateEscapes(raw) != nil {
		return manifest, errDialectBackupManifest
	}
	fields, err := dialectBackupControlObject(raw, "version", "createdAt", "files", "objects", "dialectClosure")
	if err != nil {
		return manifest, err
	}
	if json.Unmarshal(fields["version"], &manifest.Version) != nil || json.Unmarshal(fields["createdAt"], &manifest.CreatedAt) != nil {
		return dialectBackupManifest{}, errDialectBackupManifest
	}
	manifest.Files, err = dialectBackupEntries(fields["files"])
	if err != nil {
		return dialectBackupManifest{}, err
	}
	manifest.Objects, err = dialectBackupEntries(fields["objects"])
	if err != nil {
		return dialectBackupManifest{}, err
	}
	rows, err := dialectBackupControlArray(fields["dialectClosure"])
	if err != nil {
		return dialectBackupManifest{}, err
	}
	manifest.DialectClosure = make([]dialectBackupClosure, 0, len(rows))
	for _, row := range rows {
		if _, err = dialectBackupControlObject(row, "ledger", "runId", "hostOperationId", "stage", "availability"); err != nil {
			return dialectBackupManifest{}, err
		}
		var entry dialectBackupClosure
		if json.Unmarshal(row, &entry) != nil {
			return dialectBackupManifest{}, errDialectBackupManifest
		}
		manifest.DialectClosure = append(manifest.DialectClosure, entry)
	}
	if err = validateDialectBackupManifest(manifest); err != nil {
		return dialectBackupManifest{}, err
	}
	return manifest, nil
}

// Subtraction after checking both operands avoids signed addition overflow.
func dialectBackupAddBytes(total, next, limit int64) (int64, error) {
	if total < 0 || next < 0 || limit < 0 {
		return 0, errDialectBackupManifest
	}
	if total > limit || next > limit-total {
		return 0, withCode(CodeTooLarge, errors.New("expanded backup exceeds its byte limit"))
	}
	return total + next, nil
}

func dialectBackupFlatName(name string) bool {
	return !strings.ContainsAny(name, "/\\") && chatStorageFile(name)
}

func dialectBackupObjectName(name string) bool {
	return strings.HasPrefix(name, "objects/") && assistantRunDigest(strings.TrimPrefix(name, "objects/"))
}

func validateDialectBackupManifest(manifest dialectBackupManifest) error {
	if manifest.Version != 2 || manifest.Files == nil || manifest.Objects == nil || manifest.DialectClosure == nil {
		return errDialectBackupManifest
	}
	stamp, err := time.Parse(time.RFC3339, manifest.CreatedAt)
	_, offset := stamp.Zone()
	if err != nil || offset != 0 {
		return errDialectBackupManifest
	}
	// The manifest itself shares the existing total ZIP file budget.
	if len(manifest.Files) > maxStorageFiles-1 || len(manifest.Objects) > maxStorageFiles-1-len(manifest.Files) {
		return withCode(CodeTooLarge, errors.New("backup contains too many files"))
	}
	files := make(map[string]bool, len(manifest.Files))
	objects := make(map[string]bool, len(manifest.Objects))
	sizes := make(map[string]int64, len(manifest.Files)+len(manifest.Objects))
	var total int64
	add := func(entry backupEntry) error {
		if !assistantRunDigest(entry.SHA256) {
			return errDialectBackupManifest
		}
		if previous, ok := sizes[entry.SHA256]; ok && previous != entry.Bytes {
			return errDialectBackupManifest
		}
		sizes[entry.SHA256] = entry.Bytes
		var err error
		total, err = dialectBackupAddBytes(total, entry.Bytes, maxBackupData)
		return err
	}
	for _, entry := range manifest.Files {
		if !dialectBackupFlatName(entry.Name) || files[entry.Name] || entry.Bytes < 0 || entry.Bytes > int64(storageFileLimit(entry.Name)) {
			return errDialectBackupManifest
		}
		files[entry.Name] = true
		if err = add(entry); err != nil {
			return err
		}
	}
	for _, entry := range manifest.Objects {
		if !dialectBackupObjectName(entry.Name) || objects[entry.Name] || entry.Name != "objects/"+entry.SHA256 || entry.Bytes < 1 || entry.Bytes > assistantDialectDraftByteLimit {
			return errDialectBackupManifest
		}
		objects[entry.Name] = true
		if err = add(entry); err != nil {
			return err
		}
	}
	seen := make(map[dialectBackupClosureKey]bool, len(manifest.DialectClosure))
	for _, row := range manifest.DialectClosure {
		if !assistantRunFileName.MatchString(row.Ledger) || !files[row.Ledger] || len(row.RunID) != 37 || !strings.HasPrefix(row.RunID, "arun_") || !assistantRunText(row.HostOperationID, 128) {
			return errDialectBackupManifest
		}
		// Match the saved ledger's existing ID syntax, without normalizing it.
		if _, err = hex.DecodeString(row.RunID[5:]); err != nil {
			return errDialectBackupManifest
		}
		if !contains([]string{"candidate", "checked", "final"}, row.Stage) || !contains([]string{"present", "missing"}, row.Availability) {
			return errDialectBackupManifest
		}
		key := dialectBackupClosureKey{row.Ledger, row.RunID, row.HostOperationID, row.Stage}
		if seen[key] {
			return errDialectBackupManifest
		}
		seen[key] = true
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return errDialectBackupManifest
	}
	if len(raw) > maxBackupManifest {
		return withCode(CodeTooLarge, errors.New("backup manifest exceeds its limit"))
	}
	// Object reachability, actual slot availability and the final-stage 4 MiB
	// limit require the original saved-slot join; these tables cannot prove it.
	return nil
}

// Copy before sorting: a writer must not mutate its caller's selected plan.
func marshalDialectBackupManifest(manifest dialectBackupManifest) ([]byte, error) {
	if err := validateDialectBackupManifest(manifest); err != nil {
		return nil, err
	}
	manifest.Files = append([]backupEntry{}, manifest.Files...)
	manifest.Objects = append([]backupEntry{}, manifest.Objects...)
	manifest.DialectClosure = append([]dialectBackupClosure{}, manifest.DialectClosure...)
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Name < manifest.Files[j].Name })
	sort.Slice(manifest.Objects, func(i, j int) bool { return manifest.Objects[i].Name < manifest.Objects[j].Name })
	rank := map[string]int{"candidate": 0, "checked": 1, "final": 2}
	sort.Slice(manifest.DialectClosure, func(i, j int) bool {
		a, b := manifest.DialectClosure[i], manifest.DialectClosure[j]
		if a.Ledger != b.Ledger {
			return a.Ledger < b.Ledger
		}
		if a.RunID != b.RunID {
			return a.RunID < b.RunID
		}
		if a.HostOperationID != b.HostOperationID {
			return a.HostOperationID < b.HostOperationID
		}
		return rank[a.Stage] < rank[b.Stage]
	})
	return json.Marshal(manifest)
}

func dialectBackupZIPEntry(entry *zip.File) error {
	if entry == nil || entry.Method != zip.Store || entry.Mode()&os.ModeType != 0 || entry.CompressedSize64 != entry.UncompressedSize64 {
		return errDialectBackupManifest
	}
	if entry.Name != "manifest.json" && !dialectBackupFlatName(entry.Name) && !dialectBackupObjectName(entry.Name) {
		return errDialectBackupManifest
	}
	// ZIP64 per-entry metadata is unnecessary even when its values are small.
	for extra := entry.Extra; len(extra) > 0; {
		if len(extra) < 4 {
			return errDialectBackupManifest
		}
		tag := uint16(extra[0]) | uint16(extra[1])<<8
		size := int(extra[2]) | int(extra[3])<<8
		if tag == 1 || size > len(extra)-4 {
			return errDialectBackupManifest
		}
		extra = extra[4+size:]
	}
	return nil
}

func validateDialectBackupZIPTable(archive *zip.Reader, manifest dialectBackupManifest, manifestBytes int) error {
	if err := validateDialectBackupManifest(manifest); err != nil {
		return err
	}
	if archive == nil || manifestBytes < 1 || manifestBytes > maxBackupManifest || len(archive.File) != len(manifest.Files)+len(manifest.Objects)+1 {
		return errDialectBackupManifest
	}
	expected := make(map[string]uint64, len(archive.File))
	expected["manifest.json"] = uint64(manifestBytes)
	for _, entry := range manifest.Files {
		expected[entry.Name] = uint64(entry.Bytes)
	}
	for _, entry := range manifest.Objects {
		expected[entry.Name] = uint64(entry.Bytes)
	}
	seen := make(map[string]bool, len(archive.File))
	for _, entry := range archive.File {
		if err := dialectBackupZIPEntry(entry); err != nil {
			return err
		}
		size, ok := expected[entry.Name]
		if !ok || seen[entry.Name] || entry.UncompressedSize64 != size {
			return errDialectBackupManifest
		}
		seen[entry.Name] = true
	}
	return nil
}

// Keep the existing complete-file and ordinary non-ZIP64/single-disk directory
// guard BEFORE archive/zip allocates a table. The v1 reader is unchanged.
func validateDialectBackupArchive(file *os.File) (*zip.Reader, dialectBackupManifest, error) {
	var empty dialectBackupManifest
	stat, err := file.Stat()
	if err != nil {
		return nil, empty, err
	}
	if stat.Size() > maxBackupArchive {
		return nil, empty, withCode(CodeTooLarge, errors.New("chat backup exceeds its archive limit"))
	}
	if err = checkBackupDirectory(file, stat.Size()); err != nil {
		return nil, empty, err
	}
	archive, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return nil, empty, err
	}
	var record *zip.File
	seen := make(map[string]bool, len(archive.File))
	for _, entry := range archive.File {
		if err = dialectBackupZIPEntry(entry); err != nil {
			return nil, empty, err
		}
		if seen[entry.Name] {
			return nil, empty, errDialectBackupManifest
		}
		seen[entry.Name] = true
		if entry.Name == "manifest.json" {
			record = entry
		}
	}
	if record == nil {
		return nil, empty, errDialectBackupManifest
	}
	if record.UncompressedSize64 > maxBackupManifest {
		return nil, empty, withCode(CodeTooLarge, errors.New("backup manifest exceeds its limit"))
	}
	reader, err := record.Open()
	if err != nil {
		return nil, empty, err
	}
	raw, readErr := readBounded(reader, maxBackupManifest)
	err = errors.Join(readErr, reader.Close())
	if err != nil {
		return nil, empty, err
	}
	manifest, err := readDialectBackupManifest(raw)
	if err != nil {
		return nil, empty, err
	}
	if err = validateDialectBackupZIPTable(archive, manifest, len(raw)); err != nil {
		return nil, empty, err
	}
	return archive, manifest, nil
}
