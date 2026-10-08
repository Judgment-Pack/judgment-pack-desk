package desk

// The second ADR-0010 line audit's key findings (issues #309, #310, #311):
// a copy started after its original moved away takes no custody; a sidecar
// that merely lacks a rotation removes no next key; a made desk moved out of
// the desks folder after publication keeps its key through any start's
// sweep; and a rotation the runtime answered is finished only on the trail it
// was answered for, held through the rename. A stand-in runtime, by absolute
// path, answers each command; the test that drives the published runtime
// skips without one.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// plantedAs is what a test plants as file in Desk's signing folder: for a
// made desk's creation marker, the record a creation stopped before its
// manifest was about to be written leaves (issue #310); otherwise a line no
// reader takes for a key.
func plantedAs(file string) []byte {
	if id, ok := strings.CutSuffix(file, creatingSuffix); ok && deskIDPattern.MatchString(id) {
		return deskCreation{ID: id, Folder: "1:1"}.line()
	}
	return []byte("planted\n")
}

// folderOf is the folder at dir, by device and inode, as the identity file
// records it.
func folderOf(t *testing.T, dir string) string {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	return identityKey(info)
}

// seenOf is the sidecar at path as a rotation's journal records it, read now.
func seenOf(t *testing.T, path string) *sidecarSeen {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return &sidecarSeen{File: identityKey(info), Size: int64(len(data)), Digest: "sha256:" + hex.EncodeToString(sum[:])}
}
