package desk

// v0.13.2's project move recovery, as these tests read it before the storage
// line's move: with the storage line off, a move recovers this way.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestProjectMoveCrashRecoveryStorageLineOff(t *testing.T) {
	if storageLineOn() {
		t.Skip("v0.13.2 behaviour, with the storage line off; TestProjectMoveCrashRecovery covers it on")
	}
	for _, step := range []string{"before-rename", "after-rename"} {
		t.Run(step, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			if code, reply := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
				t.Fatal("save", code, reply)
			}
			destination := filepath.Join(t.TempDir(), "new")
			request := movePreview(t, ts, destination)
			testProjectMoveStep = func(at string) error {
				if at == step {
					return errors.New("simulated stop")
				}
				return nil
			}
			defer func() { testProjectMoveStep = nil }()
			code, data := moveRequest(t, ts, "move", request)
			testProjectMoveStep = nil
			next := s.projectDir
			if step == "after-rename" {
				if code != 200 {
					t.Fatal(code, string(data))
				}
				next = destination
				selected, err := ResolveProjectDir("", s.configDir)
				if err != nil || selected != next {
					t.Fatal(selected, err)
				}
			} else if code != 409 {
				t.Fatal(code, string(data))
			}
			again, ats, _ := startedAt(t, s, next)
			if again.locationStatus().Problem != "" {
				t.Fatal(again.locationStatus())
			}
			if _, err := os.Stat(filepath.Join(s.configDir, projectMoveName)); !os.IsNotExist(err) {
				t.Fatal("intent not cleared", err)
			}
			if code, reply := chatRequest(t, ats, "GET", "", "", true, ""); code != 200 || !sameJSONDocument(reply.Content, []byte(storageChat)) {
				t.Fatal("history lost", code, string(reply.Content))
			}
		})
	}
}

func TestProjectMoveRecoveryWillNotOverwriteLaterSettingsStorageLineOff(t *testing.T) {
	if storageLineOn() {
		t.Skip("v0.13.2 behaviour, with the storage line off; TestProjectMoveRecoveryWillNotOverwriteLaterSettings covers it on")
	}
	s, ts, _ := assistantServer(t)
	destination := filepath.Join(t.TempDir(), "new")
	request := movePreview(t, ts, destination)
	testProjectMoveStep = func(at string) error {
		if at == "after-rename" {
			return errors.New("stop")
		}
		return nil
	}
	defer func() { testProjectMoveStep = nil }()
	moveRequest(t, ts, "move", request)
	testProjectMoveStep = nil
	replacement := `{"project":{"file":null}}`
	privateFixture(t, filepath.Join(s.configDir, deskConfigName), replacement)
	if _, err := OpenProject("", s.configDir); err == nil {
		t.Fatal("ignored later startup edit")
	}
	again, _, _ := startedAt(t, s, destination)
	if again.locationStatus().Problem == "" {
		t.Fatal("unreported settings conflict")
	}
	mustBytes(t, filepath.Join(s.configDir, deskConfigName), replacement)
}

// sameJSONDocument compares two JSON documents by value: this release saves
// history with its members in its own order.
func sameJSONDocument(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}
