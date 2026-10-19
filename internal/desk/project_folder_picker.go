package desk

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
)

const folderPickerUnavailable = "The folder chooser could not open. Use default location or open Desk in a desktop session."

// Native selection supplies a parent, never a file upload or a browser-guessed
// local path. The existing preview/confirmation still authorizes the move.
func (s *Server) handleProjectFolderPicker(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() {
		writeJSONCoded(w, 403, CodeForbidden, "Open Desk on this computer to choose a folder.")
		return
	}
	if s.projectHasMoved() {
		writeJSONCoded(w, 409, CodeStale, projectRestartWords)
		return
	}
	if !s.location.picking.CompareAndSwap(false, true) {
		writeJSONCoded(w, 409, CodeStale, "A folder chooser is already open. Finish or cancel it first.")
		return
	}
	defer s.location.picking.Store(false)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	selected, err := nativeProjectFolder(ctx, filepath.Dir(s.projectDir))
	if ctx.Err() != nil {
		writeJSONCoded(w, 409, CodeStale, "Folder selection ended. Choose a folder again when ready.")
		return
	}
	if err != nil {
		writeJSONCoded(w, 503, CodeInternal, folderPickerUnavailable)
		return
	}
	if selected == "" {
		writeJSON(w, 200, map[string]bool{"canceled": true})
		return
	}
	if !validProjectHistoryPath(selected) || len(selected) > 2048 {
		writeJSONCoded(w, 400, CodeBadRequest, "The selected folder could not be used. Choose another folder.")
		return
	}
	parent, err := OpenProjectRoot(selected)
	if err != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "The selected folder is unavailable to Desk. Choose another folder.")
		return
	}
	defer parent.Close()
	if s.projectHasMoved() {
		writeJSONCoded(w, 409, CodeStale, projectRestartWords)
		return
	}
	writeJSON(w, 200, map[string]string{"destination": filepath.Join(parent.dir, filepath.Base(s.projectDir))})
}

func pickerCommand(ctx context.Context, program string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	stdout, stderr := &cappedBuffer{limit: 16 << 10}, &cappedBuffer{limit: 4 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if stdout.exceeded() {
		return nil, nil, errors.New("folder chooser response too large")
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func nativeProjectFolder(ctx context.Context, initial string) (string, error) {
	if runtime.GOOS == "linux" && os.Getenv("WSL_DISTRO_NAME") != "" {
		return wslProjectFolder(ctx, initial)
	}
	var program string
	var args []string
	if runtime.GOOS == "darwin" {
		program = "/usr/bin/osascript"
		args = []string{"-e", `on run argv
try
return POSIX path of (choose folder with prompt "Choose a containing folder" default location (POSIX file (item 1 of argv)))
on error number -128
return ""
end try
end run`, initial}
	} else if runtime.GOOS == "linux" && (os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "") {
		if bin, err := exec.LookPath("zenity"); err == nil {
			program, args = bin, []string{"--file-selection", "--directory", "--title=Choose a containing folder", "--filename=" + initial + string(filepath.Separator)}
		} else if bin, err := exec.LookPath("kdialog"); err == nil {
			program, args = bin, []string{"--getexistingdirectory", initial, "--title", "Choose a containing folder"}
		}
	}
	if program == "" {
		return "", errors.New(folderPickerUnavailable)
	}
	output, stderr, err := pickerCommand(ctx, program, args...)
	var exited *exec.ExitError
	if errors.As(err, &exited) && exited.ExitCode() == 1 && len(output) == 0 && len(stderr) == 0 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	selected := strings.TrimSuffix(strings.TrimSuffix(string(output), "\n"), "\r")
	if selected != "" {
		selected = filepath.Clean(selected)
	}
	return selected, nil
}

// Encode data separately from code: a folder containing quotes or PowerShell
// syntax never becomes executable script text. Prefer modern Windows dialogs.
func windowsPickerScript(initial string) string {
	payload, _ := json.Marshal(map[string]string{"initial": initial})
	return `$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false)
$inputData=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('` + base64.StdEncoding.EncodeToString(payload) + `')) | ConvertFrom-Json
Add-Type -AssemblyName System.Windows.Forms
$dialog=[System.Windows.Forms.FolderBrowserDialog]::new()
try {
 $dialog.Description='Choose a containing folder'
 if ($dialog.PSObject.Properties['UseDescriptionForTitle']) { $dialog.UseDescriptionForTitle=$true }
 $dialog.ShowNewFolderButton=$false
 $dialog.SelectedPath=$inputData.initial
 if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
  @{path=$dialog.SelectedPath} | ConvertTo-Json -Compress
 } else { @{canceled=$true} | ConvertTo-Json -Compress }
} finally { $dialog.Dispose() }
`
}

func encodedPowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	data := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(data[i*2:], unit)
	}
	return base64.StdEncoding.EncodeToString(data)
}

func wslProjectFolder(ctx context.Context, initial string) (string, error) {
	program, err := exec.LookPath("pwsh.exe")
	if err != nil {
		program, err = exec.LookPath("powershell.exe")
	}
	if err != nil {
		return "", err
	}
	converted, _, err := pickerCommand(ctx, "wslpath", "-w", initial)
	if err != nil {
		return "", err
	}
	initialWindows := strings.TrimSuffix(strings.TrimSuffix(string(converted), "\n"), "\r")
	output, _, err := pickerCommand(ctx, program, "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-EncodedCommand", encodedPowerShell(windowsPickerScript(initialWindows)))
	if err != nil {
		return "", err
	}
	var result struct {
		Path     string `json:"path"`
		Canceled bool   `json:"canceled"`
	}
	if json.Unmarshal(output, &result) != nil || result.Canceled && result.Path != "" {
		return "", errors.New("invalid folder selection")
	}
	if result.Canceled {
		return "", nil
	}
	// Only absolute drive or UNC paths can enter wslpath. No command shell or
	// user-supplied options are involved in either direction of this conversion.
	if strings.ContainsAny(result.Path, "\x00\r\n") || !(strings.HasPrefix(result.Path, `\\`) || len(result.Path) > 2 && result.Path[1] == ':' && result.Path[2] == '\\') {
		return "", errors.New("invalid Windows folder path")
	}
	converted, _, err = pickerCommand(ctx, "wslpath", "-u", result.Path)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(converted), "\n"), "\r"), nil
}
