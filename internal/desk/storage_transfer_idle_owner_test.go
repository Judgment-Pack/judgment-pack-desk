package desk

import "testing"

// **An idle Desk holds no owner retain a transfer counts against it.** A Desk
// started with chat history attaches its dialect read-recovery writer at New.
// That writer retained the storage owner for the Desk's life, and a chat
// transfer (backup, restore, move) admits only with the owner's one retain,
// its own: every transfer on a restarted Desk with history was "busy". The
// writer now holds a pin, counted apart from retains, so a backup runs.
func TestAnIdleRecoveryWriterLeavesTransfersAdmissible(t *testing.T) {
	requireStorageLine(t)
	config := t.TempDir()
	first, ts, _ := assistantServerIn(t, config)
	if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
		t.Fatal("first history", code)
	}
	ts.Close()
	assistantRunMust(t, first.Close())
	s, ts, _ := assistantServerAt(t, first.cfg.ProjectDir, config)
	if s.storageReadOnly || s.recoveryStorage == nil {
		t.Fatal("the restarted Desk attached no read-recovery writer", s.storageReadOnly, s.storageProblem)
	}
	o := s.storageConstruction.owner
	o.mu.Lock()
	active := o.active
	o.mu.Unlock()
	if active != 0 {
		t.Fatalf("an idle Desk holds %d owner retain(s)", active)
	}
	if backup := backupBytes(t, ts); len(backup) == 0 {
		t.Fatal("no backup")
	}
}
