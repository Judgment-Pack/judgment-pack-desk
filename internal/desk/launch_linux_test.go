package desk

import (
	"strings"
	"testing"
)

func TestAMissingShellIsRefusedByNameAtTheSpawn(t *testing.T) {
	// The trampoline's one dependency, said out loud. A host without a POSIX
	// shell gets this desk's own sentence rather than an exec failure nobody
	// can read.
	dir, _ := aProject(t)
	pinned, err := OpenProjectRoot(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	s, err := New(Config{Root: pinned, JpackBin: "jpack", Token: testToken, Port: testPort,
		DeskConfigDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()
	// So that the refusal under test is the shell's absence and not a runtime
	// this case never runs.
	withRuntimeBinary(t, s, "exit 0")

	was := runtimeShellName
	runtimeShellName = "no-such-shell-on-this-machine"
	t.Cleanup(func() { runtimeShellName = was })
	_, err = s.runtimeCommand(t.Context())
	if err == nil {
		t.Fatal("a command was built with no shell to run it")
	}
	for _, says := range []string{"no-such-shell-on-this-machine", "no runtime was started"} {
		if !strings.Contains(err.Error(), says) {
			t.Errorf("the refusal does not say %q: %v", says, err)
		}
	}
}
