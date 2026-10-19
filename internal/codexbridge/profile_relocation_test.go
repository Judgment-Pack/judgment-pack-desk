package codexbridge

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestRelocatedProfileConfigPreservesSandboxAndRejectsEdits(t *testing.T) {
	oldDir, newDir := filepath.Join(t.TempDir(), "old"), filepath.Join(t.TempDir(), "new")
	before := &profile{dir: oldDir, codex: filepath.Join(oldDir, "profile"), work: filepath.Join(oldDir, "work")}
	after := &profile{dir: newDir, codex: filepath.Join(newDir, "profile"), work: filepath.Join(newDir, "work")}
	got, err := RelocatedProfileConfig(before.config(), oldDir, newDir)
	if err != nil || !bytes.Equal(got, after.config()) {
		t.Fatal("profile sandbox changed incorrectly", err)
	}
	edited := bytes.Replace(before.config(), []byte("enabled = false"), []byte("enabled = true"), 1)
	if _, err = RelocatedProfileConfig(edited, oldDir, newDir); err == nil {
		t.Fatal("accepted altered sandbox")
	}
}
