package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// **An identity written later into the same folder restarts writable, with a
// warning** (Desk #685: the same folder is the same project). Storage was set
// up for a project before it had an identity, keyed by its folder. Desk then
// wrote an identity into the same folder outside storage's identity producer,
// as a signing upgrade, a key rotation or a repair does. Its storage key
// changed from the folder's to the identity's, and the first-project seal
// refused the same folder at the next start. The start now keeps the project
// under the key storage registered: it is writable before and after the
// first-use history enrollment, keeps that key, and says in the log and in
// status that the project was given an identity later. The seal still refuses
// another folder, and a copy of the project that carries the same identity.
func TestStorageIdentityAddedLaterToTheSameFolderRestartsWritableWithAWarning(t *testing.T) {
	requireStorageLine(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("JPACK_DESK_HOME", "")
	config := t.TempDir()
	logged := &bytes.Buffer{}
	start := func(project string) (*Server, error) {
		logged.Reset()
		return New(Config{ProjectDir: project, DeskConfigDir: config, JpackBin: jpackBinary(), Token: testToken, Port: 8792, Logger: log.New(logged, "", 0)})
	}
	project := t.TempDir()
	s, err := start(project)
	if err != nil {
		t.Fatal(err)
	}
	if s.storageReadOnly || s.storageRegistrationStatus() != nil {
		t.Fatalf("the first start was read only (%v) or warned (%+v)", s.storageProblem, s.storageRegistrationStatus())
	}
	folder := digestOf([]byte(s.projectDir))
	storageTestMust(t, s.Close())

	// The identity, written into the same folder outside the producer.
	id := strings.Repeat("d", 64)
	identity := identityLine(id, s.projectDir, "")
	writeBare(t, filepath.Join(project, ".desk-private", "project.json"), identity)
	if err = os.Chmod(filepath.Join(project, ".desk-private"), 0o700); err != nil {
		t.Fatal(err)
	}

	// The seal still refuses a genuinely different project: another folder,
	// and a copy of the project that carries the same identity.
	copied := t.TempDir()
	writeBare(t, filepath.Join(copied, ".desk-private", "project.json"), identity)
	if err = os.Chmod(filepath.Join(copied, ".desk-private"), 0o700); err != nil {
		t.Fatal(err)
	}
	refusesOthers := func(round string) {
		t.Helper()
		for name, dir := range map[string]string{"another folder": t.TempDir(), "a copy with the same identity": copied} {
			refused, err := start(dir)
			if err == nil {
				refused.Close()
				t.Fatalf("%s: %s started on an installation sealed to the first project", round, name)
			}
			if !errors.Is(err, errDeskAnotherStartupProject) {
				t.Fatalf("%s: %s was refused without saying why: %v", round, name, err)
			}
		}
	}

	for _, round := range []string{"after the identity was written", "after the first-use history enrollment", "after the refusals"} {
		again, err := start(project)
		if err != nil {
			t.Fatalf("%s: the same folder was refused: %v", round, err)
		}
		if again.storageReadOnly {
			again.Close()
			t.Fatalf("%s: the same folder started read only: %v\n%s", round, again.storageProblem, logged)
		}
		if got := again.storageConstruction.sources.workspaces; len(got) != 1 || string(got[0].OwnerKey) != folder {
			again.Close()
			t.Fatalf("%s: storage keyed the project %+v, want its folder's %q", round, got, folder)
		}
		warning := again.storageRegistrationStatus()
		encoded, _ := json.Marshal(warning)
		if warning == nil || warning.Enforcement != "warning-only" || !slices.Equal(warning.Identities, []string{again.projectDir}) || !strings.Contains(string(encoded), `"identities":[`) {
			again.Close()
			t.Fatalf("%s: status did not warn about the later identity: %s", round, encoded)
		}
		if !strings.Contains(logged.String(), "was given a project identity after storage was set up for it") {
			again.Close()
			t.Fatalf("%s: the start did not say so: %s", round, logged)
		}
		if round == "after the identity was written" {
			// The ordinary start's seal, then the first-use enrollment.
			storageTestMust(t, again.Close())
			refusesOthers("before the enrollment")
			if again, err = start(project); err != nil {
				t.Fatal(err)
			}
			if err = again.ensureDefaultHistoryEnrollment(t.Context()); err != nil {
				again.Close()
				t.Fatalf("the history enrollment of the same folder failed: %v", err)
			}
		}
		storageTestMust(t, again.Close())
		if round == "after the first-use history enrollment" {
			// The seal of a start that resumes the enrollment.
			refusesOthers("after the enrollment")
		}
	}
	if got := readFile(t, filepath.Join(project, ".desk-private", "project.json")); got != identity {
		t.Fatalf("the later identity was changed: %q", got)
	}
}
