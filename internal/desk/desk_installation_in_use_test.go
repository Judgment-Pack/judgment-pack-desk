package desk

import (
	"errors"
	"path/filepath"
	"testing"
)

// **A second Desk on an installation another Desk holds is refused in words.**
// A running Desk keeps its installation's home lease for its whole life, and a
// start must take it exclusively, so a second start on the same installation
// exited with the lock's raw errno ("resource temporarily unavailable"). It now
// says what holds it and what to do, for the same project and for another. A
// start after the first Desk closes is unaffected on the same project; on
// another project it is the first-project seal's refusal, in its own words.
func TestASecondDeskOnAHeldInstallationIsRefusedInWords(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config")
	project := t.TempDir()
	start := func(dir string, port int) (*Server, error) {
		return New(Config{ProjectDir: dir, JpackBin: standInRuntime(t), Token: testToken, Port: port, DeskConfigDir: config})
	}
	first, err := start(project, 18101)
	if err != nil {
		t.Fatal("the first Desk:", err)
	}
	other := t.TempDir()
	for name, dir := range map[string]string{"the same project": project, "another project": other} {
		second, err := start(dir, 18102)
		if err == nil {
			second.Close()
			first.Close()
			t.Fatalf("a second Desk started on %s", name)
		}
		if !errors.Is(err, errDeskInstallationInUse) || err.Error() != errDeskInstallationInUse.Error() {
			first.Close()
			t.Fatalf("a second Desk on %s was refused with %q", name, err)
		}
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	// The installation's storage is sealed to the first project: another
	// project, after the first Desk closed, is refused in words.
	if second, err := start(other, 18103); err == nil {
		second.Close()
		t.Fatal("another project started on an installation sealed to the first")
	} else if !errors.Is(err, errDeskAnotherStartupProject) || errors.Is(err, errDeskInstallationInUse) {
		t.Fatalf("another project after the first closed was refused with %q", err)
	}
	again, err := start(project, 18104)
	if err != nil {
		t.Fatalf("the first project after its Desk closed: %v", err)
	}
	if err = again.Close(); err != nil {
		t.Fatal(err)
	}
}
