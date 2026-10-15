package desk

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

// Browser/API integration invokes a real subprocess with a controlled choice,
// never an unattended dialog on the user's desktop.
func fakeNativeFolderPicker(t *testing.T, selected string) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("native project moves require Linux or macOS")
	}
	if runtime.GOOS == "darwin" {
		t.Skip("Linux chooser fixture; macOS chooser needs a desktop smoke test")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "zenity"), []byte("#!/bin/sh\nprintf '%s\\n' \"$JPS_TEST_SELECTED_FOLDER\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("DISPLAY", ":test")
	t.Setenv("JPS_TEST_SELECTED_FOLDER", selected)
}

func TestProjectFolderPickerKeepsStartupScopeAndRequiresReview(t *testing.T) {
	parent := t.TempDir()
	fakeNativeFolderPicker(t, parent)
	s, ts, _ := assistantServer(t)
	managed := createTestDesk(t, ts, "Managed research")
	code, data := deskCall(t, ts, "POST", "/api/project-location/choose-folder", managed.ID, "", true)
	var result struct{ Destination string }
	if code != 200 || json.Unmarshal(data, &result) != nil || result.Destination != filepath.Join(parent, filepath.Base(s.projectDir)) {
		t.Fatal(code, string(data))
	}
	if _, err := os.Stat(result.Destination); !os.IsNotExist(err) {
		t.Fatal("selection moved the project", err)
	}
	if _, err := os.Stat(s.projectDir); err != nil {
		t.Fatal(err)
	}
	request := movePreview(t, ts, result.Destination)
	if code, data := moveRequest(t, ts, "move", request); code != 200 {
		t.Fatal(code, string(data))
	}
	if _, err := os.Stat(managed.Folder); err != nil {
		t.Fatal(err)
	}
}

func TestProjectFolderPickerCancelAndAccessGuards(t *testing.T) {
	fakeNativeFolderPicker(t, "")
	s, ts, _ := assistantServer(t)
	code, data := deskCall(t, ts, "POST", "/api/project-location/choose-folder", "", "", true)
	if code != 200 || !strings.Contains(string(data), `"canceled":true`) {
		t.Fatal(code, string(data))
	}
	if code, _ = deskCall(t, ts, "POST", "/api/project-location/choose-folder", "", "", false); code != 401 {
		t.Fatal(code)
	}
	for _, remote := range []string{"192.0.2.1:1234", "malformed"} {
		r := httptest.NewRequest("POST", "/api/project-location/choose-folder", nil)
		r.RemoteAddr = remote
		bearer(r)
		w := httptest.NewRecorder()
		s.handleProjectFolderPicker(w, r)
		if w.Code != 403 {
			t.Fatal("remote native dialog", w.Code)
		}
	}
	s.location.picking.Store(true)
	if code, _ = deskCall(t, ts, "POST", "/api/project-location/choose-folder", "", "", true); code != 409 {
		t.Fatal(code)
	}
	s.location.picking.Store(false)
	s.markProjectMoved("/new/location")
	if code, _ = deskCall(t, ts, "POST", "/api/project-location/choose-folder", "", "", true); code != 409 {
		t.Fatal(code)
	}
}

func TestProjectFolderPickerRefusesInvalidSelections(t *testing.T) {
	for _, value := range []string{"relative/folder", "/does-not-exist/desk-folder", "a\nb"} {
		t.Run(value, func(t *testing.T) {
			fakeNativeFolderPicker(t, value)
			_, ts, _ := assistantServer(t)
			if code, data := deskCall(t, ts, "POST", "/api/project-location/choose-folder", "", "", true); code != 400 {
				t.Fatal(code, string(data))
			}
		})
	}
}

func TestWindowsPickerTreatsPathsAsData(t *testing.T) {
	path := `\\wsl.localhost\Ubuntu\home\O'Brien\$(Write-Output injected)\公司`
	script := windowsPickerScript(path)
	if strings.Contains(script, path) || strings.Contains(script, "Write-Output injected") {
		t.Fatal("path interpolated as executable script")
	}
	raw, err := base64.StdEncoding.DecodeString(encodedPowerShell(script))
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	if string(utf16.Decode(units)) != script {
		t.Fatal("PowerShell encoding changed the script")
	}
}

// On this WSL development machine, validate the real PowerShell/.NET bridge
// without opening an interactive chooser or reading any user files.
func TestWindowsFolderPickerBridge(t *testing.T) {
	if os.Getenv("JPS_NATIVE_PICKER_CHECK") != "1" {
		t.Skip("opt-in native bridge check")
	}
	program, err := exec.LookPath("pwsh.exe")
	if err != nil {
		t.Skip("Windows PowerShell unavailable")
	}
	initial := `\\wsl.localhost\Ubuntu\home\O'Brien\$(throw 'injected')\公司`
	script := windowsPickerScript(initial)
	script = strings.Replace(script, "if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK)", "if ($true)", 1)
	output, _, err := pickerCommand(context.Background(), program, "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-EncodedCommand", encodedPowerShell(script))
	var result struct{ Path string }
	if err != nil || json.Unmarshal(output, &result) != nil || result.Path != initial {
		t.Fatal("native bridge", err, string(output))
	}
	// Exercise both real WSL conversions around a controlled native selection.
	selected := filepath.Join(t.TempDir(), "O'Brien $() 公司")
	if err := os.Mkdir(selected, 0700); err != nil {
		t.Fatal(err)
	}
	windows, _, err := pickerCommand(context.Background(), "wslpath", "-w", selected)
	if err != nil {
		t.Fatal(err)
	}
	response, _ := json.Marshal(map[string]string{"path": strings.TrimSpace(string(windows))})
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "pwsh.exe"), []byte("#!/bin/sh\nprintf '%s\\n' \"$JPS_TEST_NATIVE_RESPONSE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("JPS_TEST_NATIVE_RESPONSE", string(response))
	picked, err := wslProjectFolder(context.Background(), filepath.Dir(selected))
	if err != nil || picked != selected {
		t.Fatal("WSL path roundtrip", picked, err)
	}
	t.Setenv("JPS_TEST_NATIVE_RESPONSE", `{"canceled":true}`)
	if picked, err = wslProjectFolder(context.Background(), selected); err != nil || picked != "" {
		t.Fatal("native cancellation", picked, err)
	}
	t.Setenv("JPS_TEST_NATIVE_RESPONSE", `{"path":"-bad-option"}`)
	if _, err = wslProjectFolder(context.Background(), selected); err == nil {
		t.Fatal("accepted invalid native path")
	}

}
