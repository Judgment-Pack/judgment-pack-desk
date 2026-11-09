package desk

import (
	"errors"
	"io"
	"log"
	"testing"
)

// **Another project in the same installation is refused at startup in words.**
// In an installation without a Desk home, the storage catalog is sealed to the
// project Desk was first started on. Starting Desk on another project with the
// same settings and data folders, after the first Desk stopped, started
// read-only behind the generic "new work is paused" ("original owner
// reconciliation required", "custody unavailable"). It is now refused at
// startup, naming the cause and what to do, before and after the first
// project's first-use history enrollment; the first project still starts.
func TestStorageAnotherStartupProjectIsRefusedInWords(t *testing.T) {
	requireStorageLine(t)
	for _, enrolled := range []bool{false, true} {
		name := "before first-use enrollment"
		if enrolled {
			name = "after first-use enrollment"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			t.Setenv("JPACK_DESK_HOME", "")
			config := t.TempDir()
			start := func(project string) (*Server, error) {
				return New(Config{ProjectDir: project, DeskConfigDir: config, JpackBin: jpackBinary(), Token: testToken, Port: 8791, Logger: log.New(io.Discard, "", 0)})
			}
			first, other := t.TempDir(), t.TempDir()
			s, err := start(first)
			if err != nil {
				t.Fatal(err)
			}
			if s.storageReadOnly {
				t.Fatalf("first start read-only: %v", s.storageProblem)
			}
			if enrolled {
				if err = s.ensureDefaultHistoryEnrollment(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			storageTestMust(t, s.Close())
			refused, err := start(other)
			if err == nil {
				refused.Close()
				t.Fatalf("another project started (read-only=%v, problem=%v)", refused.storageReadOnly, refused.storageProblem)
			}
			if !errors.Is(err, errDeskAnotherStartupProject) {
				t.Fatalf("another project was refused without saying why: %v", err)
			}
			again, err := start(first)
			if err != nil {
				t.Fatal(err)
			}
			defer again.Close()
			if again.storageReadOnly {
				t.Fatalf("the first project no longer starts after the refusal: %v", again.storageProblem)
			}
		})
	}
}
