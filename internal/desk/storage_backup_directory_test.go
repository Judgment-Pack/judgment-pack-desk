package desk

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func TestBackupZIP64LocatorClassification(t *testing.T) {
	// Isolated locator fields only: these headers are never part of an archive
	// and are never passed to a ZIP reader.
	for _, tc := range []struct {
		name    string
		locator [20]byte
		want    bool
	}{
		{"zero-offset", [20]byte{0x50, 0x4b, 0x06, 0x07, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0}, true},
		{"positive-offset", [20]byte{0x50, 0x4b, 0x06, 0x07, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0}, true},
		{"largest-signed-offset", [20]byte{0x50, 0x4b, 0x06, 0x07, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f, 1, 0, 0, 0}, true},
		{"signature", [20]byte{0x50, 0x4b, 0x06, 0x08, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0}, false},
		{"start-disk", [20]byte{0x50, 0x4b, 0x06, 0x07, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0}, false},
		{"total-disks-zero", [20]byte{0x50, 0x4b, 0x06, 0x07, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, false},
		{"total-disks-two", [20]byte{0x50, 0x4b, 0x06, 0x07, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0}, false},
		{"negative-converted-offset", [20]byte{0x50, 0x4b, 0x06, 0x07, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x80, 1, 0, 0, 0}, false},
		{"all-ones-offset", [20]byte{0x50, 0x4b, 0x06, 0x07, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1, 0, 0, 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := backupZIP64Locator(tc.locator[:]); got != tc.want {
				t.Fatalf("locator classification = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBackupOrdinaryDirectorySizeCompatibility(t *testing.T) {
	for _, version := range []string{"v1-store", "v1-deflate", "v2-store"} {
		for _, directorySize := range []int{65534, 65535, 65536} {
			for _, commentSize := range []int{0, 65535} {
				t.Run(fmt.Sprintf("%s/directory-%d/comment-%d", version, directorySize, commentSize), func(t *testing.T) {
					entries := dialectBackupTestZIPEntries(t)
					if version != "v2-store" {
						method := uint16(zip.Store)
						if version == "v1-deflate" {
							method = zip.Deflate
						}
						name := "conversations-" + strings.Repeat("a", 64) + ".json"
						raw := []byte(storageChat)
						manifest := backupManifest{Version: 1, CreatedAt: "2026-10-29T12:00:00Z", Files: []backupEntry{{Name: name, Bytes: int64(len(raw)), SHA256: digestOf(raw)}}}
						entries = []dialectBackupTestZIPEntry{{name: name, raw: raw, method: method}, {name: "manifest.json", raw: dialectBackupTestJSON(t, manifest), method: method}}
					}
					// A standard writer creates one ordinary directory. Pad only a
					// legal file comment to reach the requested directory size.
					baseSize := 0
					for _, entry := range entries {
						baseSize += 46 + len(entry.name) + len(entry.extra)
					}
					var body bytes.Buffer
					writer := zip.NewWriter(&body)
					if err := writer.SetComment(strings.Repeat("z", commentSize)); err != nil {
						t.Fatal(err)
					}
					for i, entry := range entries {
						header := &zip.FileHeader{Name: entry.name, Method: entry.method, Extra: entry.extra}
						if i == len(entries)-1 {
							header.Comment = strings.Repeat("c", directorySize-baseSize)
						}
						item, err := writer.CreateHeader(header)
						if err != nil {
							t.Fatal(err)
						}
						if _, err := item.Write(entry.raw); err != nil {
							t.Fatal(err)
						}
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
					raw := body.Bytes()
					end := len(raw) - commentSize - 22
					if got := binary.LittleEndian.Uint32(raw[end+12 : end+16]); got != uint32(directorySize) {
						t.Fatalf("ordinary directory size = %d, want %d", got, directorySize)
					}
					file := dialectBackupTestZIPFile(t, raw)
					if err := checkBackupDirectory(file, int64(len(raw))); err != nil {
						t.Fatal("ordinary directory refused", err)
					}
					reader, err := zip.NewReader(file, int64(len(raw)))
					if err != nil || len(reader.File) != len(entries) || reader.Comment != strings.Repeat("z", commentSize) {
						t.Fatal("ordinary archive did not retain its directory and comment", err)
					}
					if version == "v2-store" {
						if _, _, err := validateDialectBackupArchive(file); err != nil {
							t.Fatal("ordinary v2 backup refused", err)
						}
					} else if _, manifest, err := validateChatBackup(file); err != nil || manifest.Version != 1 {
						t.Fatal("ordinary v1 backup refused", err)
					}
				})
			}
		}
	}
}
